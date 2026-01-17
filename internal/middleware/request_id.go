package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDMiddleware adds a unique request ID to each request
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if request ID is already set (e.g., from load balancer)
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			// Generate new UUID for request ID
			requestID = uuid.New().String()
		}

		// Set request ID in context for use by handlers and other middleware
		c.Set("request_id", requestID)

		// Add request ID to response headers
		c.Header("X-Request-ID", requestID)

		c.Next()
	}
}
