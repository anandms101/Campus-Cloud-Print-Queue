package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg, err := LoadConfig()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	awsCfg, err := NewAWSConfig(context.Background(), cfg.AWSRegion)
	if err != nil {
		logger.Fatal("failed to load AWS config", zap.Error(err))
	}

	app := NewApp(cfg, awsCfg, logger)

	srv := &http.Server{
		Addr:         ":8000",
		Handler:      app.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info("starting Go Gin API on :8000")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	// Graceful shutdown: wait for SIGINT or SIGTERM, then drain in-flight requests.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("received shutdown signal", zap.String("signal", sig.String()))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("forced shutdown", zap.Error(err))
	}
	logger.Info("server stopped gracefully")
}
