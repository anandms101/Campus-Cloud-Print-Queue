package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type App struct {
	config Config
	dynamo *dynamodb.Client
	s3     *s3.Client
	sqs    *sqs.Client
	router *gin.Engine
}

var uploadSemaphore = make(chan struct{}, 4)

func NewApp(cfg Config, awsCfg aws.Config) *App {
	dynamo, s3client, sqsclient := NewAWSClients(awsCfg)
	r := gin.New()
	r.Use(requestIDMiddleware(), jsonAccessLogger())
	app := &App{
		config: cfg,
		dynamo: dynamo,
		s3:     s3client,
		sqs:    sqsclient,
		router: r,
	}

	app.router.GET("/health", app.healthHandler)
	app.router.POST("/jobs", app.createJobHandler)
	app.router.GET("/jobs", app.listJobsHandler)
	app.router.GET("/jobs/:id", app.getJobHandler)
	app.router.POST("/jobs/:id/release", app.releaseJobHandler)
	app.router.DELETE("/jobs/:id", app.cancelJobHandler)

	return app
}

func (a *App) Run() error {
	return a.router.Run(":8000")
}

func (a *App) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (a *App) createJobHandler(c *gin.Context) {
	select {
	case uploadSemaphore <- struct{}{}:
		defer func() { <-uploadSemaphore }()
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{"detail": "too many active uploads, please try again later"})
		return
	}

	ctx := c.Request.Context()

	userID := c.PostForm("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "userId is required"})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "file is required"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to open uploaded file"})
		return
	}
	defer file.Close()

	jobID := uuid.NewString()
	fileName := filepath.Base(fileHeader.Filename)
	s3Key := fmt.Sprintf("uploads/%s/%s", jobID, fileName)

	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(a.config.S3Bucket),
		Key:           aws.String(s3Key),
		Body:          file,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(fileHeader.Size),
	})
	if err != nil {
		log.Printf("failed to upload to S3: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to upload file"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	item := JobItem{
		JobID:     jobID,
		UserID:    userID,
		FileName:  fileName,
		S3Key:     s3Key,
		Status:    "HELD",
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour).Unix(),
		Version:   1,
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		log.Printf("failed to marshal job item: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to save job"})
		return
	}

	_, err = a.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(a.config.DynamoTable),
		Item:      av,
	})
	if err != nil {
		log.Printf("failed to save item to DynamoDB: %v", err)

		// Best-effort cleanup of the uploaded S3 object to avoid orphans.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, delErr := a.s3.DeleteObject(cleanupCtx, &s3.DeleteObjectInput{
			Bucket: aws.String(a.config.S3Bucket),
			Key:    aws.String(s3Key),
		}); delErr != nil {
			log.Printf("failed to delete orphaned S3 object %s: %v", s3Key, delErr)
		}

		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to save job"})
		return
	}

	c.JSON(http.StatusCreated, item)
}

func (a *App) getJobHandler(c *gin.Context) {
	jobID := c.Param("id")
	item, err := a.fetchJob(c.Request.Context(), jobID)
	if err != nil {
		if err == errNotFound {
			c.JSON(http.StatusNotFound, gin.H{"detail": "Job not found"})
			return
		}
		log.Printf("failed to load job: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load job"})
		return
	}

	c.JSON(http.StatusOK, item)
}

func (a *App) listJobsHandler(c *gin.Context) {
	userID := c.Query("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "userId query parameter is required"})
		return
	}

	status := c.Query("status")

	keyCond := expression.Key("userId").Equal(expression.Value(userID))
	builder := expression.NewBuilder().WithKeyCondition(keyCond)

	if status != "" {
		filter := expression.Name("status").Equal(expression.Value(status))
		builder = builder.WithFilter(filter)
	}

	expr, err := builder.Build()
	if err != nil {
		log.Printf("failed to build expression: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query jobs"})
		return
	}

	resp, err := a.dynamo.Query(c.Request.Context(), &dynamodb.QueryInput{
		TableName:                 aws.String(a.config.DynamoTable),
		IndexName:                 aws.String("userId-createdAt-index"),
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ScanIndexForward:          aws.Bool(false),
	})
	if err != nil {
		log.Printf("failed to query DynamoDB: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query jobs"})
		return
	}

	jobs := make([]JobItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		var job JobItem
		if err := attributevalue.UnmarshalMap(item, &job); err != nil {
			log.Printf("failed to unmarshal job item: %v", err)
			continue
		}
		jobs = append(jobs, job)
	}

	c.JSON(http.StatusOK, jobs)
}

func (a *App) releaseJobHandler(c *gin.Context) {
	jobID := c.Param("id")
	var body ReleaseRequest
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}

	if body.PrinterName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "printerName is required"})
		return
	}

	if _, ok := a.config.SQSQueueURLs[body.PrinterName]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("Invalid printer. Choose from: %v", a.config.ValidPrinters)})
		return
	}

	ctx := c.Request.Context()

	_, err := a.fetchJob(ctx, jobID)
	if err != nil {
		if err == errNotFound {
			c.JSON(http.StatusNotFound, gin.H{"detail": "Job not found"})
			return
		}
		log.Printf("load job failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load job"})
		return
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	resp, err := a.dynamo.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(a.config.DynamoTable),
		Key: map[string]types.AttributeValue{
			"jobId": &types.AttributeValueMemberS{Value: jobID},
		},
		UpdateExpression:    aws.String("SET #s = :new_status, printerName = :printer, updatedAt = :now, version = version + :inc"),
		ConditionExpression: aws.String("#s = :held"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":new_status": &types.AttributeValueMemberS{Value: "RELEASED"},
			":printer":    &types.AttributeValueMemberS{Value: body.PrinterName},
			":now":        &types.AttributeValueMemberS{Value: updatedAt},
			":held":       &types.AttributeValueMemberS{Value: "HELD"},
			":inc":        &types.AttributeValueMemberN{Value: "1"},
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		var cce *types.ConditionalCheckFailedException
		if errors.As(err, &cce) {
			// Re-fetch to report the actual current status in case it changed concurrently.
			currentItem, fetchErr := a.fetchJob(ctx, jobID)
			status := "unknown"
			if fetchErr == nil {
				status = currentItem.Status
			}
			c.JSON(http.StatusConflict, gin.H{"detail": fmt.Sprintf("Job cannot be released. Current status: %s", status)})
			return
		}
		log.Printf("failed to release job: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to release job"})
		return
	}

	var updated JobItem
	if err := attributevalue.UnmarshalMap(resp.Attributes, &updated); err != nil {
		log.Printf("failed to unmarshal updated job: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to release job"})
		return
	}

	if err := a.sendJobToPrinter(ctx, &updated); err != nil {
		log.Printf("failed to send job to SQS: %v", err)
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if rollbackErr := a.rollbackRelease(rollbackCtx, jobID); rollbackErr != nil {
			log.Printf("rollback to HELD failed for job %s: %v — job stuck in RELEASED", jobID, rollbackErr)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to enqueue job"})
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (a *App) cancelJobHandler(c *gin.Context) {
	jobID := c.Param("id")
	ctx := c.Request.Context()

	_, err := a.fetchJob(ctx, jobID)
	if err != nil {
		if err == errNotFound {
			c.JSON(http.StatusNotFound, gin.H{"detail": "Job not found"})
			return
		}
		log.Printf("failed to load job: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to cancel job"})
		return
	}

	resp, err := a.dynamo.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(a.config.DynamoTable),
		Key: map[string]types.AttributeValue{
			"jobId": &types.AttributeValueMemberS{Value: jobID},
		},
		UpdateExpression:    aws.String("SET #s = :new_status, updatedAt = :now, version = version + :inc"),
		ConditionExpression: aws.String("#s = :held"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":new_status": &types.AttributeValueMemberS{Value: "CANCELLED"},
			":now":        &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)},
			":held":       &types.AttributeValueMemberS{Value: "HELD"},
			":inc":        &types.AttributeValueMemberN{Value: "1"},
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		var cce *types.ConditionalCheckFailedException
		if errors.As(err, &cce) {
			// Re-fetch to obtain the actual current status in case it changed concurrently.
			refreshed, fetchErr := a.fetchJob(ctx, jobID)
			if fetchErr == nil {
				c.JSON(http.StatusConflict, gin.H{"detail": fmt.Sprintf("Job cannot be cancelled. Current status: %s", refreshed.Status)})
				return
			}
			log.Printf("failed to reload job after conditional check failure for %s: %v", jobID, fetchErr)
			c.JSON(http.StatusConflict, gin.H{"detail": "Job cannot be cancelled because its status changed; please retry"})
			return
		}
		log.Printf("failed to cancel job: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to cancel job"})
		return
	}

	var updated JobItem
	if err := attributevalue.UnmarshalMap(resp.Attributes, &updated); err != nil {
		log.Printf("failed to unmarshal cancelled job: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to cancel job"})
		return
	}

	if updated.S3Key != "" {
		if _, err := a.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(a.config.S3Bucket),
			Key:    aws.String(updated.S3Key),
		}); err != nil {
			log.Printf("failed to delete S3 object for cancelled job %s: %v", jobID, err)
		}
	}

	c.JSON(http.StatusOK, updated)
}

func (a *App) fetchJob(ctx context.Context, jobID string) (*JobItem, error) {
	resp, err := a.dynamo.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(a.config.DynamoTable),
		Key: map[string]types.AttributeValue{
			"jobId": &types.AttributeValueMemberS{Value: jobID},
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.Item == nil {
		return nil, errNotFound
	}

	var item JobItem
	if err := attributevalue.UnmarshalMap(resp.Item, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

var errNotFound = fmt.Errorf("not found")

func (a *App) rollbackRelease(ctx context.Context, jobID string) error {
	_, err := a.dynamo.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(a.config.DynamoTable),
		Key: map[string]types.AttributeValue{
			"jobId": &types.AttributeValueMemberS{Value: jobID},
		},
		UpdateExpression:    aws.String("SET #s = :held, updatedAt = :now, version = version + :inc REMOVE printerName"),
		ConditionExpression: aws.String("#s = :released"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":held":     &types.AttributeValueMemberS{Value: "HELD"},
			":released": &types.AttributeValueMemberS{Value: "RELEASED"},
			":now":      &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)},
			":inc":      &types.AttributeValueMemberN{Value: "1"},
		},
	})
	return err
}

func (a *App) sendJobToPrinter(ctx context.Context, item *JobItem) error {
	if item.PrinterName == nil {
		return fmt.Errorf("job %s has no printer name assigned", item.JobID)
	}
	queueURL := a.config.SQSQueueURLs[*item.PrinterName]
	body, err := json.Marshal(map[string]string{
		"jobId": item.JobID,
		"s3Key": item.S3Key,
	})
	if err != nil {
		return err
	}

	_, err = a.sqs.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(body)),
	})
	return err
}
