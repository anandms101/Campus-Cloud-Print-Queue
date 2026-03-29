package main

import (
	"context"
	"log"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	awsCfg, err := NewAWSConfig(context.Background(), cfg.AWSRegion)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	app := NewApp(cfg, awsCfg)
	log.Println("Starting Go Gin API on :8000")
	if err := app.Run(); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
