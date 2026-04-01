package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
)

type Config struct {
	DynamoTable    string
	S3Bucket       string
	AWSRegion      string
	SQSQueueURLs   map[string]string
	ValidPrinters  []string
	MaxUploadBytes int64
}

func LoadConfig() (Config, error) {
	maxUpload := int64(50 * 1024 * 1024) // 50 MB default
	if raw := os.Getenv("MAX_UPLOAD_BYTES"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid MAX_UPLOAD_BYTES: %w", err)
		}
		maxUpload = parsed
	}

	cfg := Config{
		DynamoTable:    getenv("DYNAMODB_TABLE", "campus-print-jobs"),
		S3Bucket:       getenv("S3_BUCKET", "campus-print-docs"),
		AWSRegion:      getenv("AWS_DEFAULT_REGION", "us-west-2"),
		SQSQueueURLs:   make(map[string]string),
		MaxUploadBytes: maxUpload,
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
		sort.Strings(cfg.ValidPrinters) // deterministic order for error messages
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
