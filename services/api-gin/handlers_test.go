package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testConfig() Config {
	return Config{
		DynamoTable:    "test-table",
		S3Bucket:       "test-bucket",
		AWSRegion:      "us-west-2",
		MaxUploadBytes: 50 * 1024 * 1024,
		SQSQueueURLs: map[string]string{
			"printer-1": "http://localhost/q1",
			"printer-2": "http://localhost/q2",
			"printer-3": "http://localhost/q3",
		},
		ValidPrinters: []string{"printer-1", "printer-2", "printer-3"},
	}
}

func testApp(d DynamoAPI, s S3API, q SQSAPI) *App {
	logger, _ := zap.NewDevelopment()
	return NewAppWithClients(testConfig(), d, s, q, logger)
}

func newMultipartRequest(userId, fileName string, fileContent []byte) (*http.Request, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if userId != "" {
		_ = writer.WriteField("userId", userId)
	}
	if fileName != "" {
		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			return nil, err
		}
		_, _ = part.Write(fileContent)
	}
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/jobs", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

func parseBody(w *httptest.ResponseRecorder) map[string]interface{} {
	var result map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return result
}

// heldJobItem returns DynamoDB-marshalled attributes for a HELD job.
func heldJobItem(jobID string) map[string]dbtypes.AttributeValue {
	av, _ := attributevalue.MarshalMap(JobItem{
		JobID:     jobID,
		UserID:    "user1",
		FileName:  "test.pdf",
		S3Key:     "uploads/" + jobID + "/test.pdf",
		Status:    "HELD",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Version:   1,
	})
	return av
}

func releasedJobItem(jobID, printer string) map[string]dbtypes.AttributeValue {
	av, _ := attributevalue.MarshalMap(JobItem{
		JobID:       jobID,
		UserID:      "user1",
		FileName:    "test.pdf",
		S3Key:       "uploads/" + jobID + "/test.pdf",
		PrinterName: &printer,
		Status:      "RELEASED",
		CreatedAt:   "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-01T00:00:00Z",
		Version:     2,
	})
	return av
}

func cancelledJobItem(jobID string) map[string]dbtypes.AttributeValue {
	av, _ := attributevalue.MarshalMap(JobItem{
		JobID:     jobID,
		UserID:    "user1",
		FileName:  "test.pdf",
		S3Key:     "uploads/" + jobID + "/test.pdf",
		Status:    "CANCELLED",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Version:   2,
	})
	return av
}

// =========================================================================
// Health
// =========================================================================

func TestHealth_Returns200(t *testing.T) {
	app := testApp(&mockDynamo{}, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(w)
	if body["status"] != "healthy" {
		t.Fatalf("expected status=healthy, got %v", body["status"])
	}
	if body["timestamp"] == nil {
		t.Fatal("expected timestamp field")
	}
}

func TestHealthReady_AllHealthy(t *testing.T) {
	app := testApp(&mockDynamo{}, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(w)
	if body["status"] != "healthy" {
		t.Fatalf("expected healthy, got %v", body["status"])
	}
}

func TestHealthReady_DynamoDown(t *testing.T) {
	d := &mockDynamo{
		describeTableFn: func(ctx context.Context, params *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	body := parseBody(w)
	if body["status"] != "degraded" {
		t.Fatalf("expected degraded, got %v", body["status"])
	}
}

// =========================================================================
// Create Job
// =========================================================================

func TestCreateJob_Success(t *testing.T) {
	var putCalled bool
	d := &mockDynamo{
		putItemFn: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
			putCalled = true
			return &dynamodb.PutItemOutput{}, nil
		},
	}
	var s3Called bool
	s := &mockS3{
		putObjectFn: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			s3Called = true
			return &s3.PutObjectOutput{}, nil
		},
	}

	app := testApp(d, s, &mockSQS{})
	req, _ := newMultipartRequest("student1", "report.pdf", []byte("PDF content"))
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if !s3Called {
		t.Fatal("expected S3 PutObject to be called")
	}
	if !putCalled {
		t.Fatal("expected DynamoDB PutItem to be called")
	}
	body := parseBody(w)
	if body["status"] != "HELD" {
		t.Fatalf("expected HELD, got %v", body["status"])
	}
	if body["userId"] != "student1" {
		t.Fatalf("expected userId=student1, got %v", body["userId"])
	}
}

func TestCreateJob_MissingUserId(t *testing.T) {
	app := testApp(&mockDynamo{}, &mockS3{}, &mockSQS{})
	req, _ := newMultipartRequest("", "report.pdf", []byte("content"))
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateJob_MissingFile(t *testing.T) {
	app := testApp(&mockDynamo{}, &mockS3{}, &mockSQS{})
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("userId", "student1")
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/jobs", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateJob_FileTooLarge(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cfg := testConfig()
	cfg.MaxUploadBytes = 10 // 10 bytes limit
	app := NewAppWithClients(cfg, &mockDynamo{}, &mockS3{}, &mockSQS{}, logger)

	bigContent := make([]byte, 100) // 100 bytes > 10 limit
	req, _ := newMultipartRequest("student1", "big.pdf", bigContent)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != 413 {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateJob_SanitizesFilename(t *testing.T) {
	var capturedKey string
	s := &mockS3{
		putObjectFn: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			capturedKey = *params.Key
			return &s3.PutObjectOutput{}, nil
		},
	}
	app := testApp(&mockDynamo{}, s, &mockSQS{})
	req, _ := newMultipartRequest("student1", "../../etc/passwd", []byte("content"))
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(capturedKey, "..") {
		t.Fatalf("S3 key should not contain path traversal: %s", capturedKey)
	}
	if !strings.HasSuffix(capturedKey, "/passwd") {
		t.Fatalf("expected sanitized filename 'passwd', got key: %s", capturedKey)
	}
}

func TestCreateJob_DynamoFailure_CleansUpS3(t *testing.T) {
	var s3Deleted bool
	d := &mockDynamo{
		putItemFn: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
			return nil, fmt.Errorf("DynamoDB unavailable")
		},
	}
	s := &mockS3{
		putObjectFn: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return &s3.PutObjectOutput{}, nil
		},
		deleteObjectFn: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			s3Deleted = true
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	app := testApp(d, s, &mockSQS{})
	req, _ := newMultipartRequest("student1", "report.pdf", []byte("content"))
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if !s3Deleted {
		t.Fatal("expected S3 orphan cleanup (DeleteObject) to be called")
	}
}

// =========================================================================
// Get Job
// =========================================================================

func TestGetJob_Success(t *testing.T) {
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: heldJobItem("job-1")}, nil
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs/job-1", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(w)
	if body["jobId"] != "job-1" {
		t.Fatalf("expected jobId=job-1, got %v", body["jobId"])
	}
}

func TestGetJob_NotFound(t *testing.T) {
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: nil}, nil
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs/nonexistent", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// =========================================================================
// List Jobs
// =========================================================================

func TestListJobs_ByUser(t *testing.T) {
	d := &mockDynamo{
		queryFn: func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{
				Items: []map[string]dbtypes.AttributeValue{heldJobItem("job-1")},
			}, nil
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs?userId=user1", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var jobs []map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &jobs)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
}

func TestListJobs_MissingUserId(t *testing.T) {
	app := testApp(&mockDynamo{}, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListJobs_WithStatusFilter(t *testing.T) {
	var filterUsed bool
	d := &mockDynamo{
		queryFn: func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
			if params.FilterExpression != nil {
				filterUsed = true
			}
			return &dynamodb.QueryOutput{Items: []map[string]dbtypes.AttributeValue{}}, nil
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jobs?userId=user1&status=HELD", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !filterUsed {
		t.Fatal("expected filter expression to be used when status is provided")
	}
}

// =========================================================================
// Release Job
// =========================================================================

func TestReleaseJob_Success(t *testing.T) {
	fetchCount := 0
	var sqsCalled bool
	printer := "printer-1"
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			fetchCount++
			return &dynamodb.GetItemOutput{Item: heldJobItem("job-1")}, nil
		},
		updateItemFn: func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			return &dynamodb.UpdateItemOutput{Attributes: releasedJobItem("job-1", printer)}, nil
		},
	}
	q := &mockSQS{
		sendMessageFn: func(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
			sqsCalled = true
			return &sqs.SendMessageOutput{}, nil
		},
	}

	app := testApp(d, &mockS3{}, q)
	w := httptest.NewRecorder()
	reqBody := `{"printerName":"printer-1"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/release", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	app.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !sqsCalled {
		t.Fatal("expected SQS SendMessage to be called")
	}
	body := parseBody(w)
	if body["status"] != "RELEASED" {
		t.Fatalf("expected RELEASED, got %v", body["status"])
	}
}

func TestReleaseJob_InvalidPrinter(t *testing.T) {
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: heldJobItem("job-1")}, nil
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	reqBody := `{"printerName":"nonexistent-printer"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/release", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	app.router.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestReleaseJob_NotFound(t *testing.T) {
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: nil}, nil
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	reqBody := `{"printerName":"printer-1"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs/nonexistent/release", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	app.router.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestReleaseJob_Conflict(t *testing.T) {
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: heldJobItem("job-1")}, nil
		},
		updateItemFn: func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			return nil, &dbtypes.ConditionalCheckFailedException{Message: stringPtr("condition not met")}
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	reqBody := `{"printerName":"printer-1"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/release", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	app.router.ServeHTTP(w, req)

	if w.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReleaseJob_SQSFailure_Rollback(t *testing.T) {
	var rollbackCalled bool
	updateCallCount := 0
	printer := "printer-1"
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: heldJobItem("job-1")}, nil
		},
		updateItemFn: func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			updateCallCount++
			if updateCallCount == 1 {
				// First call: release succeeds
				return &dynamodb.UpdateItemOutput{Attributes: releasedJobItem("job-1", printer)}, nil
			}
			// Second call: rollback
			rollbackCalled = true
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	q := &mockSQS{
		sendMessageFn: func(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
			return nil, fmt.Errorf("SQS unavailable")
		},
	}

	app := testApp(d, &mockS3{}, q)
	w := httptest.NewRecorder()
	reqBody := `{"printerName":"printer-1"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/release", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	app.router.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if !rollbackCalled {
		t.Fatal("expected compensating rollback (UpdateItem) to be called")
	}
}

// =========================================================================
// Cancel Job
// =========================================================================

func TestCancelJob_Success(t *testing.T) {
	var s3Deleted bool
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: heldJobItem("job-1")}, nil
		},
		updateItemFn: func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			return &dynamodb.UpdateItemOutput{Attributes: cancelledJobItem("job-1")}, nil
		},
	}
	s := &mockS3{
		deleteObjectFn: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			s3Deleted = true
			return &s3.DeleteObjectOutput{}, nil
		},
	}
	app := testApp(d, s, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/jobs/job-1", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !s3Deleted {
		t.Fatal("expected S3 DeleteObject to be called for cancelled job")
	}
	body := parseBody(w)
	if body["status"] != "CANCELLED" {
		t.Fatalf("expected CANCELLED, got %v", body["status"])
	}
}

func TestCancelJob_NotFound(t *testing.T) {
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: nil}, nil
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/jobs/nonexistent", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCancelJob_Conflict(t *testing.T) {
	d := &mockDynamo{
		getItemFn: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: heldJobItem("job-1")}, nil
		},
		updateItemFn: func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			return nil, &dbtypes.ConditionalCheckFailedException{Message: stringPtr("condition not met")}
		},
	}
	app := testApp(d, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/jobs/job-1", nil)
	app.router.ServeHTTP(w, req)

	if w.Code != 409 {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// =========================================================================
// Bulkhead (concurrent upload limit)
// =========================================================================

func TestCreateJob_BulkheadRejects(t *testing.T) {
	app := testApp(&mockDynamo{}, &mockS3{}, &mockSQS{})

	// Fill the semaphore (capacity 4) manually.
	for i := 0; i < 4; i++ {
		app.uploadSemaphore <- struct{}{}
	}
	// The 5th upload should be rejected immediately.
	req, _ := newMultipartRequest("student1", "report.pdf", []byte("content"))
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != 429 {
		t.Fatalf("expected 429 (bulkhead full), got %d: %s", w.Code, w.Body.String())
	}

	// Drain the semaphore to avoid leaking goroutines.
	for i := 0; i < 4; i++ {
		<-app.uploadSemaphore
	}
}

// =========================================================================
// Circuit Breaker
// =========================================================================

func TestCreateJob_S3CircuitBreakerOpen(t *testing.T) {
	// Trip the S3 circuit breaker by causing 5 consecutive failures.
	s := &mockS3{
		putObjectFn: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, fmt.Errorf("S3 unavailable")
		},
	}
	app := testApp(&mockDynamo{}, s, &mockSQS{})

	// Fire 5 requests to trip the breaker (threshold = 5 consecutive failures).
	for i := 0; i < 5; i++ {
		req, _ := newMultipartRequest("student1", "report.pdf", []byte("content"))
		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)
		// These should all be 500 (S3 error, breaker still closed/half-open).
		if w.Code != 500 {
			t.Fatalf("iteration %d: expected 500, got %d", i, w.Code)
		}
	}

	// Now the breaker should be open — the next request should get 503.
	req, _ := newMultipartRequest("student1", "report.pdf", []byte("content"))
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503 (circuit breaker open), got %d: %s", w.Code, w.Body.String())
	}
}

// =========================================================================
// Rate Limiter
// =========================================================================

func TestRateLimit_PassesUnderNormalLoad(t *testing.T) {
	app := testApp(&mockDynamo{}, &mockS3{}, &mockSQS{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	app.router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200 under normal load, got %d", w.Code)
	}
}

// =========================================================================
// Helpers
// =========================================================================

func stringPtr(s string) *string {
	return &s
}
