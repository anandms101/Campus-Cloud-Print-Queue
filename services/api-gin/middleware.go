package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

// jsonAccessLogger logs a structured JSON access-log line after each request,
// mirroring the fields emitted by the Python API's RequestIdMiddleware.
func jsonAccessLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		requestID, _ := c.Get("request_id")
		entry := map[string]interface{}{
			"timestamp":   start.UTC().Format(time.RFC3339Nano),
			"method":      c.Request.Method,
			"path":        c.Request.URL.Path,
			"status":      c.Writer.Status(),
			"duration_ms": float64(time.Since(start).Nanoseconds()) / 1e6,
			"request_id":  requestID,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			log.Printf("access log marshal error: %v", err)
			return
		}
		log.Println(string(data))
	}
}
