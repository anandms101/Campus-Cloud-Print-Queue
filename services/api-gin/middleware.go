package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// requestIDMiddleware generates a UUID for every request, stores it in the
// Gin context, and echoes it back as the X-Request-ID response header.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.NewString()
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// jsonAccessLogger logs a structured JSON access-log line after each request
// using the zap logger for consistent structured output.
func jsonAccessLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		requestID, _ := c.Get("request_id")
		logger.Info("request completed",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Float64("duration_ms", float64(time.Since(start).Nanoseconds())/1e6),
			zap.Any("request_id", requestID),
		)
	}
}

// timeoutMiddleware applies a context deadline to each request so handlers
// that call AWS services cannot hang indefinitely. If the deadline is
// exceeded and no response body has been written yet, it returns HTTP 504.
func timeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()

		// If the context deadline was exceeded and the handler hasn't
		// written a response yet, return a clear 504 Gateway Timeout.
		if ctx.Err() == context.DeadlineExceeded && !c.Writer.Written() {
			c.AbortWithStatusJSON(http.StatusGatewayTimeout, gin.H{
				"detail": "request timed out",
			})
		}
	}
}

// rateLimitMiddleware uses a token-bucket rate limiter (golang.org/x/time/rate).
// Requests that exceed the limit receive HTTP 429.
func rateLimitMiddleware(limiter *rate.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"detail": "rate limit exceeded, please try again later",
			})
			return
		}
		c.Next()
	}
}
