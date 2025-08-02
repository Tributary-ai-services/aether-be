package metrics

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// HTTPMetricsMiddleware creates middleware for recording HTTP metrics
func HTTPMetricsMiddleware(metrics *Metrics, log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Increment in-flight requests
		metrics.IncHTTPRequestsInFlight()

		// Get request size
		requestSize := c.Request.ContentLength
		if requestSize < 0 {
			requestSize = 0
		}

		// Process request
		c.Next()

		// Calculate duration
		duration := time.Since(start)

		// Decrement in-flight requests
		metrics.DecHTTPRequestsInFlight()

		// Get response size from Content-Length header or writer
		responseSize := int64(0)
		if c.Writer.Size() >= 0 {
			responseSize = int64(c.Writer.Size())
		}

		// Record the metrics
		path := getCleanPath(c.FullPath(), c.Request.URL.Path)
		metrics.RecordHTTPRequest(
			c.Request.Method,
			path,
			c.Writer.Status(),
			duration,
			requestSize,
			responseSize,
		)

		// Log slow requests
		if duration > 5*time.Second {
			log.Warn("Slow HTTP request",
				zap.String("method", c.Request.Method),
				zap.String("path", path),
				zap.Int("status", c.Writer.Status()),
				zap.Duration("duration", duration),
				zap.String("user_agent", c.Request.UserAgent()),
				zap.String("remote_addr", c.ClientIP()),
			)
		}
	}
}

// getCleanPath returns a clean path for metrics (removes IDs and other variable parts)
func getCleanPath(routePath, requestPath string) string {
	// If we have a route path from Gin, use it (it contains parameter placeholders)
	if routePath != "" {
		return routePath
	}

	// Fallback to request path, but clean it up for common patterns
	return cleanPathForMetrics(requestPath)
}

// cleanPathForMetrics cleans up the path for metrics by replacing common variable parts
func cleanPathForMetrics(path string) string {
	// This is a simple implementation - could be enhanced with regex
	// Replace common UUID patterns with placeholders
	cleaned := path

	// Replace UUIDs in path with :id placeholder
	// This is a simple approach - in production, you might want more sophisticated path normalization

	return cleaned
}

// Note: SystemMetricsCollector and BusinessMetricsCollector are now in collector.go
// This file focuses on HTTP middleware and database wrappers

// DatabaseMetricsWrapper wraps database operations to record metrics
type DatabaseMetricsWrapper struct {
	metrics  *Metrics
	database string // "neo4j" or "redis"
}

// NewDatabaseMetricsWrapper creates a new database metrics wrapper
func NewDatabaseMetricsWrapper(metrics *Metrics, database string) *DatabaseMetricsWrapper {
	return &DatabaseMetricsWrapper{
		metrics:  metrics,
		database: database,
	}
}

// RecordQuery records a database query with metrics
func (dmw *DatabaseMetricsWrapper) RecordQuery(operation string, fn func() error) error {
	start := time.Now()
	err := fn()
	duration := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
	}

	dmw.metrics.RecordDBQuery(dmw.database, operation, status, duration)
	return err
}

// RecordRedisOperation records a Redis operation with metrics
func (dmw *DatabaseMetricsWrapper) RecordRedisOperation(operation string, fn func() error) error {
	start := time.Now()
	err := fn()
	duration := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
	}

	dmw.metrics.RecordRedisOperation(operation, status, duration)
	return err
}
