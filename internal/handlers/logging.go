package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// LoggingHandler handles frontend logging requests
type LoggingHandler struct {
	logger *logger.Logger
}

// NewLoggingHandler creates a new logging handler
func NewLoggingHandler(log *logger.Logger) *LoggingHandler {
	return &LoggingHandler{
		logger: log,
	}
}

// FrontendLogEntry represents a log entry from the frontend
type FrontendLogEntry struct {
	Level      string                 `json:"level" binding:"required"`      // error, warn, info, debug
	Message    string                 `json:"message" binding:"required"`    // Log message
	Timestamp  *time.Time             `json:"timestamp"`                     // Client timestamp (optional)
	URL        string                 `json:"url"`                           // Page URL
	UserAgent  string                 `json:"user_agent"`                    // Browser user agent
	SessionID  string                 `json:"session_id"`                    // Session identifier
	StackTrace string                 `json:"stack_trace,omitempty"`         // Error stack trace
	Extra      map[string]interface{} `json:"extra,omitempty"`               // Additional fields
}

// LogBatchRequest represents a batch of log entries from the frontend
type LogBatchRequest struct {
	Logs []FrontendLogEntry `json:"logs" binding:"required,dive"`
}

// @Summary Submit frontend logs
// @Description Receives log entries from the frontend (browser) and logs them server-side for collection by Loki
// @Tags logging
// @Accept json
// @Produce json
// @Param logs body LogBatchRequest true "Batch of frontend log entries"
// @Success 200 {object} map[string]interface{} "Logs received successfully"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Router /api/v1/logs [post]
func (h *LoggingHandler) SubmitFrontendLogs(c *gin.Context) {
	var req LogBatchRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request",
			"details": err.Error(),
		})
		return
	}

	// Get user context from auth middleware (if available)
	userID, _ := c.Get("user_id")
	userEmail, _ := c.Get("user_email")
	spaceID, _ := c.Get("space_id")
	tenantID, _ := c.Get("tenant_id")

	// Log each frontend entry to server logs
	for _, entry := range req.Logs {
		// Build zap fields
		fields := []zap.Field{
			zap.String("source", "frontend"),
			zap.String("client_url", entry.URL),
			zap.String("user_agent", entry.UserAgent),
			zap.String("session_id", entry.SessionID),
		}

		// Add client timestamp if provided
		if entry.Timestamp != nil {
			fields = append(fields, zap.Time("timestamp_client", *entry.Timestamp))
		}

		// Add user context if available
		if userID != nil {
			if uid, ok := userID.(string); ok {
				fields = append(fields, zap.String("user_id", uid))
			}
		}
		if userEmail != nil {
			if email, ok := userEmail.(string); ok {
				fields = append(fields, zap.String("user_email", email))
			}
		}
		if spaceID != nil {
			if sid, ok := spaceID.(string); ok {
				fields = append(fields, zap.String("space_id", sid))
			}
		}
		if tenantID != nil {
			if tid, ok := tenantID.(string); ok {
				fields = append(fields, zap.String("tenant_id", tid))
			}
		}

		// Add stack trace for errors
		if entry.StackTrace != "" {
			fields = append(fields, zap.String("stack_trace", entry.StackTrace))
		}

		// Add extra fields as JSON
		if entry.Extra != nil {
			fields = append(fields, zap.Any("extra", entry.Extra))
		}

		// Create logger with context
		contextLogger := h.logger.WithContext(fields...)

		// Log at appropriate level
		switch entry.Level {
		case "error":
			contextLogger.Error(entry.Message)
		case "warn", "warning":
			contextLogger.Warn(entry.Message)
		case "info":
			contextLogger.Info(entry.Message)
		case "debug":
			contextLogger.Debug(entry.Message)
		default:
			// Default to info level
			contextLogger.Info(entry.Message)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Logs received",
		"count":   len(req.Logs),
	})
}
