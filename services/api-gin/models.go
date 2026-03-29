package main

type JobItem struct {
	JobID       string  `json:"jobId" dynamodbav:"jobId"`
	UserID      string  `json:"userId" dynamodbav:"userId"`
	FileName    string  `json:"fileName" dynamodbav:"fileName"`
	S3Key       string  `json:"-" dynamodbav:"s3Key"`
	PrinterName *string `json:"printerName" dynamodbav:"printerName,omitempty"`
	Status      string  `json:"status" dynamodbav:"status"`
	CreatedAt   string  `json:"createdAt" dynamodbav:"createdAt"`
	UpdatedAt   string  `json:"updatedAt" dynamodbav:"updatedAt"`
	ExpiresAt   int64   `json:"-" dynamodbav:"expiresAt"`
	Version     int64   `json:"-" dynamodbav:"version"`
}

type ReleaseRequest struct {
	PrinterName string `json:"printerName"`
}
