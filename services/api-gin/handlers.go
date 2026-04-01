package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// App holds the configuration, AWS service clients, resilience components,
// and the Gin router. All AWS clients are behind interfaces for testability.
type App struct {
	config          Config
	dynamo          DynamoAPI
	s3client        S3API
	sqsClient       SQSAPI
	router          *gin.Engine
	logger          *zap.Logger
	dynamoBreaker   *gobreaker.CircuitBreaker[[]byte]
	s3Breaker       *gobreaker.CircuitBreaker[[]byte]
	sqsBreaker      *gobreaker.CircuitBreaker[[]byte]
	uploadSemaphore chan struct{} // bulkhead: limits concurrent uploads
}

var errNotFound = fmt.Errorf("not found")

// isCircuitBreakerError returns true if the error is from an open or
// half-open circuit breaker (ErrOpenState or ErrTooManyRequests).
func isCircuitBreakerError(err error) bool {
	return errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests)
}

// NewApp constructs the application with all middleware and routes.
func NewApp(cfg Config, awsCfg aws.Config, logger *zap.Logger) *App {
	dynamo, s3client, sqsclient := NewAWSClients(awsCfg)
	return NewAppWithClients(cfg, dynamo, s3client, sqsclient, logger)
}

// NewAppWithClients is the constructor used in tests, accepting interface-typed clients.
func NewAppWithClients(cfg Config, dynamo DynamoAPI, s3client S3API, sqsclient SQSAPI, logger *zap.Logger) *App {
	r := gin.New()

	// --- Middleware stack (order matters) ---
	r.Use(gin.Recovery()) // panic recovery — returns 500 instead of crashing the process
	r.Use(requestIDMiddleware())
	r.Use(jsonAccessLogger(logger))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		AllowCredentials: false,
	}))

	// Global rate limiter: 100 requests/second, burst of 20.
	limiter := rate.NewLimiter(rate.Limit(100), 20)
	r.Use(rateLimitMiddleware(limiter))

	app := &App{
		config:          cfg,
		dynamo:          dynamo,
		s3client:        s3client,
		sqsClient:       sqsclient,
		router:          r,
		logger:          logger,
		dynamoBreaker:   newCircuitBreaker("dynamodb"),
		s3Breaker:       newCircuitBreaker("s3"),
		sqsBreaker:      newCircuitBreaker("sqs"),
		uploadSemaphore: make(chan struct{}, 4),
	}

	// --- Routes ---
	// Timeouts are applied per-route (not globally) to avoid child-context
	// capping issues where a shorter global timeout would override a longer
	// route-specific one.
	r.GET("/health", app.healthHandler)
	r.GET("/health/ready", timeoutMiddleware(5*time.Second), app.healthReadyHandler)
	r.POST("/jobs", timeoutMiddleware(60*time.Second), app.createJobHandler)
	r.GET("/jobs", timeoutMiddleware(30*time.Second), app.listJobsHandler)
	r.GET("/jobs/:id", timeoutMiddleware(30*time.Second), app.getJobHandler)
	r.POST("/jobs/:id/release", timeoutMiddleware(30*time.Second), app.releaseJobHandler)
	r.DELETE("/jobs/:id", timeoutMiddleware(30*time.Second), app.cancelJobHandler)

	return app
}

// ---------------------------------------------------------------------------
// Health endpoints
// ---------------------------------------------------------------------------

// healthHandler is lightweight (always 200) so the ALB keeps the task healthy.
func (a *App) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// healthReadyHandler verifies actual connectivity to DynamoDB, S3, and SQS.
// Returns 200 if all dependencies are reachable, 503 if any are degraded.
func (a *App) healthReadyHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{}
	healthy := true

	// DynamoDB
	_, err := a.dynamo.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(a.config.DynamoTable),
	})
	if err != nil {
		checks["dynamodb"] = "unhealthy"
		healthy = false
	} else {
		checks["dynamodb"] = "healthy"
	}

	// S3
	_, err = a.s3client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(a.config.S3Bucket),
	})
	if err != nil {
		checks["s3"] = "unhealthy"
		healthy = false
	} else {
		checks["s3"] = "healthy"
	}

	// SQS — ensure at least one queue is configured, then probe all queues
	if len(a.config.SQSQueueURLs) == 0 {
		checks["sqs"] = "unhealthy"
		healthy = false
	} else {
		sqsHealthy := true
		for _, url := range a.config.SQSQueueURLs {
			_, err = a.sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
				QueueUrl:       aws.String(url),
				AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
			})
			if err != nil {
				sqsHealthy = false
			}
		}
		if sqsHealthy {
			checks["sqs"] = "healthy"
		} else {
			checks["sqs"] = "unhealthy"
			healthy = false
		}
	}

	status := "healthy"
	httpCode := http.StatusOK
	if !healthy {
		status = "degraded"
		httpCode = http.StatusServiceUnavailable
	}

	c.JSON(httpCode, gin.H{
		"status":    status,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"checks":    checks,
	})
}

// ---------------------------------------------------------------------------
// Create job
// ---------------------------------------------------------------------------

func (a *App) createJobHandler(c *gin.Context) {
	// Bulkhead: reject immediately if 4 uploads are already in flight.
	select {
	case a.uploadSemaphore <- struct{}{}:
		defer func() { <-a.uploadSemaphore }()
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

	// File size validation.
	if fileHeader.Size > a.config.MaxUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"detail": fmt.Sprintf("File too large. Maximum size is %d MB", a.config.MaxUploadBytes/(1024*1024)),
		})
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

	// S3 upload via circuit breaker.
	_, cbErr := a.s3Breaker.Execute(func() ([]byte, error) {
		_, err := a.s3client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(a.config.S3Bucket),
			Key:           aws.String(s3Key),
			Body:          file,
			ContentType:   aws.String(contentType),
			ContentLength: aws.Int64(fileHeader.Size),
		})
		return nil, err
	})
	if cbErr != nil {
		a.logger.Error("failed to upload to S3", zap.Error(cbErr), zap.String("jobId", jobID))
		if isCircuitBreakerError(cbErr) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "storage service temporarily unavailable"})
			return
		}
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
		a.logger.Error("failed to marshal job item", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to save job"})
		return
	}

	// DynamoDB put via circuit breaker.
	_, cbErr = a.dynamoBreaker.Execute(func() ([]byte, error) {
		_, err := a.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(a.config.DynamoTable),
			Item:      av,
		})
		return nil, err
	})
	if cbErr != nil {
		a.logger.Error("failed to save item to DynamoDB", zap.Error(cbErr), zap.String("jobId", jobID))

		// Best-effort orphan cleanup: delete the S3 object we just uploaded.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, delErr := a.s3client.DeleteObject(cleanupCtx, &s3.DeleteObjectInput{
			Bucket: aws.String(a.config.S3Bucket),
			Key:    aws.String(s3Key),
		}); delErr != nil {
			a.logger.Error("failed to delete orphaned S3 object", zap.Error(delErr), zap.String("s3Key", s3Key))
		}

		if isCircuitBreakerError(cbErr) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "database service temporarily unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to save job"})
		return
	}

	c.JSON(http.StatusCreated, item)
}

// ---------------------------------------------------------------------------
// Get / List jobs
// ---------------------------------------------------------------------------

func (a *App) getJobHandler(c *gin.Context) {
	jobID := c.Param("id")
	item, err := a.fetchJob(c.Request.Context(), jobID)
	if err != nil {
		if err == errNotFound {
			c.JSON(http.StatusNotFound, gin.H{"detail": "Job not found"})
			return
		}
		a.logger.Error("failed to load job", zap.Error(err), zap.String("jobId", jobID))
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
		a.logger.Error("failed to build expression", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query jobs"})
		return
	}

	var resp *dynamodb.QueryOutput
	_, cbErr := a.dynamoBreaker.Execute(func() ([]byte, error) {
		var err error
		resp, err = a.dynamo.Query(c.Request.Context(), &dynamodb.QueryInput{
			TableName:                 aws.String(a.config.DynamoTable),
			IndexName:                 aws.String("userId-createdAt-index"),
			KeyConditionExpression:    expr.KeyCondition(),
			FilterExpression:          expr.Filter(),
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
			ScanIndexForward:          aws.Bool(false),
		})
		return nil, err
	})
	if cbErr != nil {
		a.logger.Error("failed to query DynamoDB", zap.Error(cbErr))
		if isCircuitBreakerError(cbErr) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "database service temporarily unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to query jobs"})
		return
	}

	jobs := make([]JobItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		var job JobItem
		if err := attributevalue.UnmarshalMap(item, &job); err != nil {
			a.logger.Error("failed to unmarshal job item", zap.Error(err))
			continue
		}
		jobs = append(jobs, job)
	}

	c.JSON(http.StatusOK, jobs)
}

// ---------------------------------------------------------------------------
// Release job
// ---------------------------------------------------------------------------

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
		a.logger.Error("load job failed", zap.Error(err), zap.String("jobId", jobID))
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load job"})
		return
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	var releaseResp *dynamodb.UpdateItemOutput
	_, cbErr := a.dynamoBreaker.Execute(func() ([]byte, error) {
		var err error
		releaseResp, err = a.dynamo.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName: aws.String(a.config.DynamoTable),
			Key: map[string]dbtypes.AttributeValue{
				"jobId": &dbtypes.AttributeValueMemberS{Value: jobID},
			},
			UpdateExpression:    aws.String("SET #s = :new_status, printerName = :printer, updatedAt = :now, version = version + :inc"),
			ConditionExpression: aws.String("#s = :held"),
			ExpressionAttributeNames: map[string]string{
				"#s": "status",
			},
			ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
				":new_status": &dbtypes.AttributeValueMemberS{Value: "RELEASED"},
				":printer":    &dbtypes.AttributeValueMemberS{Value: body.PrinterName},
				":now":        &dbtypes.AttributeValueMemberS{Value: updatedAt},
				":held":       &dbtypes.AttributeValueMemberS{Value: "HELD"},
				":inc":        &dbtypes.AttributeValueMemberN{Value: "1"},
			},
			ReturnValues: dbtypes.ReturnValueAllNew,
		})
		return nil, err
	})
	if cbErr != nil {
		var cce *dbtypes.ConditionalCheckFailedException
		if errors.As(cbErr, &cce) {
			currentItem, fetchErr := a.fetchJob(ctx, jobID)
			status := "unknown"
			if fetchErr == nil {
				status = currentItem.Status
			} else {
				a.logger.Error("failed to re-fetch job after conditional check failure",
					zap.Error(fetchErr), zap.String("jobId", jobID))
			}
			c.JSON(http.StatusConflict, gin.H{"detail": fmt.Sprintf("Job cannot be released. Current status: %s", status)})
			return
		}
		a.logger.Error("failed to release job", zap.Error(cbErr), zap.String("jobId", jobID))
		if isCircuitBreakerError(cbErr) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "database service temporarily unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to release job"})
		return
	}

	var updated JobItem
	if err := attributevalue.UnmarshalMap(releaseResp.Attributes, &updated); err != nil {
		a.logger.Error("failed to unmarshal updated job", zap.Error(err), zap.String("jobId", jobID))
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to release job"})
		return
	}

	// Enqueue to the printer's SQS queue via circuit breaker.
	if err := a.sendJobToPrinter(ctx, &updated); err != nil {
		a.logger.Error("failed to send job to SQS", zap.Error(err), zap.String("jobId", jobID))
		// Compensating transaction: roll back RELEASED → HELD.
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if rollbackErr := a.rollbackRelease(rollbackCtx, jobID); rollbackErr != nil {
			a.logger.Error("rollback to HELD failed — job stuck in RELEASED",
				zap.Error(rollbackErr), zap.String("jobId", jobID))
		}
		if isCircuitBreakerError(err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "queue service temporarily unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to enqueue job"})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// ---------------------------------------------------------------------------
// Cancel job
// ---------------------------------------------------------------------------

func (a *App) cancelJobHandler(c *gin.Context) {
	jobID := c.Param("id")
	ctx := c.Request.Context()

	_, err := a.fetchJob(ctx, jobID)
	if err != nil {
		if err == errNotFound {
			c.JSON(http.StatusNotFound, gin.H{"detail": "Job not found"})
			return
		}
		a.logger.Error("failed to load job", zap.Error(err), zap.String("jobId", jobID))
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to cancel job"})
		return
	}

	var cancelResp *dynamodb.UpdateItemOutput
	_, cbErr := a.dynamoBreaker.Execute(func() ([]byte, error) {
		var err error
		cancelResp, err = a.dynamo.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName: aws.String(a.config.DynamoTable),
			Key: map[string]dbtypes.AttributeValue{
				"jobId": &dbtypes.AttributeValueMemberS{Value: jobID},
			},
			UpdateExpression:    aws.String("SET #s = :new_status, updatedAt = :now, version = version + :inc"),
			ConditionExpression: aws.String("#s = :held"),
			ExpressionAttributeNames: map[string]string{
				"#s": "status",
			},
			ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
				":new_status": &dbtypes.AttributeValueMemberS{Value: "CANCELLED"},
				":now":        &dbtypes.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)},
				":held":       &dbtypes.AttributeValueMemberS{Value: "HELD"},
				":inc":        &dbtypes.AttributeValueMemberN{Value: "1"},
			},
			ReturnValues: dbtypes.ReturnValueAllNew,
		})
		return nil, err
	})
	if cbErr != nil {
		var cce *dbtypes.ConditionalCheckFailedException
		if errors.As(cbErr, &cce) {
			refreshed, fetchErr := a.fetchJob(ctx, jobID)
			if fetchErr == nil {
				c.JSON(http.StatusConflict, gin.H{"detail": fmt.Sprintf("Job cannot be cancelled. Current status: %s", refreshed.Status)})
				return
			}
			a.logger.Error("failed to reload job after conditional check failure", zap.Error(fetchErr), zap.String("jobId", jobID))
			c.JSON(http.StatusConflict, gin.H{"detail": "Job cannot be cancelled because its status changed; please retry"})
			return
		}
		a.logger.Error("failed to cancel job", zap.Error(cbErr), zap.String("jobId", jobID))
		if isCircuitBreakerError(cbErr) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "database service temporarily unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to cancel job"})
		return
	}

	var updated JobItem
	if err := attributevalue.UnmarshalMap(cancelResp.Attributes, &updated); err != nil {
		a.logger.Error("failed to unmarshal cancelled job", zap.Error(err), zap.String("jobId", jobID))
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to cancel job"})
		return
	}

	// Best-effort S3 cleanup.
	if updated.S3Key != "" {
		if _, err := a.s3client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(a.config.S3Bucket),
			Key:    aws.String(updated.S3Key),
		}); err != nil {
			a.logger.Error("failed to delete S3 object for cancelled job", zap.Error(err), zap.String("jobId", jobID))
		}
	}

	c.JSON(http.StatusOK, updated)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (a *App) fetchJob(ctx context.Context, jobID string) (*JobItem, error) {
	// Wrap through DynamoDB circuit breaker for consistent failure tracking.
	var resp *dynamodb.GetItemOutput
	_, cbErr := a.dynamoBreaker.Execute(func() ([]byte, error) {
		var err error
		resp, err = a.dynamo.GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String(a.config.DynamoTable),
			Key: map[string]dbtypes.AttributeValue{
				"jobId": &dbtypes.AttributeValueMemberS{Value: jobID},
			},
		})
		return nil, err
	})
	if cbErr != nil {
		return nil, cbErr
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

func (a *App) rollbackRelease(ctx context.Context, jobID string) error {
	_, err := a.dynamo.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(a.config.DynamoTable),
		Key: map[string]dbtypes.AttributeValue{
			"jobId": &dbtypes.AttributeValueMemberS{Value: jobID},
		},
		UpdateExpression:    aws.String("SET #s = :held, updatedAt = :now, version = version + :inc REMOVE printerName"),
		ConditionExpression: aws.String("#s = :released"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
			":held":     &dbtypes.AttributeValueMemberS{Value: "HELD"},
			":released": &dbtypes.AttributeValueMemberS{Value: "RELEASED"},
			":now":      &dbtypes.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)},
			":inc":      &dbtypes.AttributeValueMemberN{Value: "1"},
		},
	})
	return err
}

func (a *App) sendJobToPrinter(ctx context.Context, item *JobItem) error {
	if item.PrinterName == nil {
		return fmt.Errorf("job %s has no printer name assigned", item.JobID)
	}
	queueURL, ok := a.config.SQSQueueURLs[*item.PrinterName]
	if !ok {
		return fmt.Errorf("printer %s has no configured SQS queue URL; check SQS_QUEUE_URLS configuration", *item.PrinterName)
	}
	body, err := json.Marshal(map[string]string{
		"jobId": item.JobID,
		"s3Key": item.S3Key,
	})
	if err != nil {
		return err
	}

	// SQS send via circuit breaker.
	_, cbErr := a.sqsBreaker.Execute(func() ([]byte, error) {
		_, err := a.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    aws.String(queueURL),
			MessageBody: aws.String(string(body)),
		})
		return nil, err
	})
	return cbErr
}
