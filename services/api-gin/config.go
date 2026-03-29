package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	DynamoTable   string
	S3Bucket      string
	AWSRegion     string
	SQSQueueURLs  map[string]string
	ValidPrinters []string
}

func LoadConfig() (Config, error) {
	cfg := Config{
		DynamoTable:  getenv("DYNAMODB_TABLE", "campus-print-jobs"),
		S3Bucket:     getenv("S3_BUCKET", "campus-print-docs"),
		AWSRegion:    getenv("AWS_DEFAULT_REGION", "us-west-2"),
		SQSQueueURLs: make(map[string]string),
	}

	raw := os.Getenv("SQS_QUEUE_URLS")
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.SQSQueueURLs); err != nil {
			return Config{}, fmt.Errorf("invalid SQS_QUEUE_URLS: %w", err)
		}
	}

	if len(cfg.SQSQueueURLs) == 0 {
		cfg.ValidPrinters = []string{"printer-1", "printer-2", "printer-3"}
	} else {
		for printer := range cfg.SQSQueueURLs {
			cfg.ValidPrinters = append(cfg.ValidPrinters, printer)
		}
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
