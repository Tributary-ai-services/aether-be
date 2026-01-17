package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// WebSocketHandler handles WebSocket connections for real-time updates
type WebSocketHandler struct {
	documentService   *services.DocumentService
	audiModalService  *services.AudiModalService
	logger           *logger.Logger
	upgrader         websocket.Upgrader
	connections      map[string]*WebSocketConnection // jobID -> connection
	connectionsMux   sync.RWMutex
}

// WebSocketConnection represents a WebSocket connection tracking a specific job
type WebSocketConnection struct {
	conn      *websocket.Conn
	jobID     string
	userID    string
	tenantID  string
	lastSent  time.Time
	done      chan bool
}

// WebSocketMessage represents a message sent over WebSocket
type WebSocketMessage struct {
	Type      string                 `json:"type"`
	JobID     string                 `json:"job_id"`
	Status    string                 `json:"status"`
	Progress  float64                `json:"progress"`
	Data      map[string]interface{} `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
	Error     string                 `json:"error,omitempty"`
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(documentService *services.DocumentService, audiModalService *services.AudiModalService, log *logger.Logger) *WebSocketHandler {
	return &WebSocketHandler{
		documentService:  documentService,
		audiModalService: audiModalService,
		logger:          log.WithService("websocket_handler"),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// In production, implement proper origin checking
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		connections: make(map[string]*WebSocketConnection),
	}
}

// StreamJobStatus handles WebSocket connections for real-time job status updates
// @Summary Stream job status updates
// @Description Get real-time status updates for a job via WebSocket
// @Tags websocket
// @Security Bearer
// @Param id path string true "Job ID"
// @Router /api/v1/jobs/{id}/stream [get]
func (h *WebSocketHandler) StreamJobStatus(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Job ID is required", nil))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Starting WebSocket job status stream", 
		zap.String("job_id", jobID), 
		zap.String("user_id", userID),
		zap.String("tenant_id", spaceContext.TenantID))

	// Upgrade HTTP connection to WebSocket
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("Failed to upgrade WebSocket connection", zap.Error(err))
		return
	}
	defer conn.Close()

	// Create connection tracking
	wsConn := &WebSocketConnection{
		conn:     conn,
		jobID:    jobID,
		userID:   userID,
		tenantID: spaceContext.TenantID,
		lastSent: time.Now(),
		done:     make(chan bool),
	}

	// Register connection
	h.connectionsMux.Lock()
	h.connections[jobID] = wsConn
	h.connectionsMux.Unlock()

	// Cleanup on disconnect
	defer func() {
		h.connectionsMux.Lock()
		delete(h.connections, jobID)
		h.connectionsMux.Unlock()
		close(wsConn.done)
		h.logger.Info("WebSocket connection closed", zap.String("job_id", jobID))
	}()

	// Start status monitoring
	h.startStatusMonitoring(c.Request.Context(), wsConn)
}

// startStatusMonitoring monitors job status and sends updates via WebSocket
func (h *WebSocketHandler) startStatusMonitoring(ctx context.Context, wsConn *WebSocketConnection) {
	ticker := time.NewTicker(2 * time.Second) // Update every 2 seconds
	defer ticker.Stop()

	// Send initial status
	h.sendJobStatusUpdate(ctx, wsConn)

	for {
		select {
		case <-wsConn.done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.sendJobStatusUpdate(ctx, wsConn); err != nil {
				h.logger.Error("Failed to send status update", 
					zap.String("job_id", wsConn.jobID), 
					zap.Error(err))
				return
			}
		}
	}
}

// sendJobStatusUpdate sends a job status update via WebSocket
func (h *WebSocketHandler) sendJobStatusUpdate(ctx context.Context, wsConn *WebSocketConnection) error {
	// Get current job status
	status, err := h.getJobStatus(ctx, wsConn.jobID, wsConn.tenantID)
	if err != nil {
		// Send error message
		errorMsg := WebSocketMessage{
			Type:      "error",
			JobID:     wsConn.jobID,
			Error:     "Failed to get job status",
			Timestamp: time.Now(),
		}
		return wsConn.conn.WriteJSON(errorMsg)
	}

	// Create WebSocket message
	message := WebSocketMessage{
		Type:      "job_status_update",
		JobID:     wsConn.jobID,
		Status:    status["status"].(string),
		Progress:  status["progress"].(float64),
		Data:      status,
		Timestamp: time.Now(),
	}

	// Send message
	if err := wsConn.conn.WriteJSON(message); err != nil {
		return err
	}

	wsConn.lastSent = time.Now()
	
	// If job is completed or failed, send final message and close
	if message.Status == "completed" || message.Status == "failed" {
		finalMsg := WebSocketMessage{
			Type:      "job_completed",
			JobID:     wsConn.jobID,
			Status:    message.Status,
			Progress:  message.Progress,
			Data:      status,
			Timestamp: time.Now(),
		}
		wsConn.conn.WriteJSON(finalMsg)
		return fmt.Errorf("job completed") // Signal to close connection
	}

	return nil
}

// getJobStatus gets job status (reusing logic from JobHandler)
func (h *WebSocketHandler) getJobStatus(ctx context.Context, jobID string, tenantID string) (map[string]interface{}, error) {
	// Try to get file chunks to see if this is a completed processing job
	chunks, err := h.audiModalService.GetFileChunks(ctx, tenantID, jobID, 10, 0)
	if err == nil && chunks != nil && len(chunks.Data) > 0 {
		// Job completed successfully
		return map[string]interface{}{
			"job_id":        jobID,
			"status":        "completed",
			"progress":      100.0,
			"chunks_count":  len(chunks.Data),
			"total_chunks":  chunks.Total,
			"job_type":      "document_processing",
			"completed_at":  time.Now(),
		}, nil
	}

	// Job is still processing or doesn't exist
	return map[string]interface{}{
		"job_id":      jobID,
		"status":      "processing",
		"progress":    50.0, // In real implementation, calculate based on actual progress
		"job_type":    "document_processing",
		"started_at":  time.Now().Add(-30 * time.Second), // Mock start time
		"estimated_completion": time.Now().Add(60 * time.Second), // Mock ETA
	}, nil
}

// StreamDocumentStatus handles WebSocket connections for document processing status
// @Summary Stream document status updates  
// @Description Get real-time status updates for document processing via WebSocket
// @Tags websocket
// @Security Bearer
// @Param id path string true "Document ID"
// @Router /api/v1/documents/{id}/stream [get]
func (h *WebSocketHandler) StreamDocumentStatus(c *gin.Context) {
	documentID := c.Param("id")
	if documentID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Document ID is required", nil))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Starting WebSocket document status stream", 
		zap.String("document_id", documentID), 
		zap.String("user_id", userID))

	// Get document to find processing job ID
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID, spaceContext)
	if err != nil {
		c.JSON(http.StatusNotFound, errors.NotFound("Document not found"))
		return
	}

	// If document has a processing job ID, stream that job's status
	if document.ProcessingJobID != "" {
		// Redirect to job status streaming
		c.Params = []gin.Param{{Key: "id", Value: document.ProcessingJobID}}
		h.StreamJobStatus(c)
		return
	}

	// Otherwise, stream document status directly
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("Failed to upgrade WebSocket connection", zap.Error(err))
		return
	}
	defer conn.Close()

	// Send document status updates
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			// Get fresh document status
			doc, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID, spaceContext)
			if err != nil {
				break
			}

			message := WebSocketMessage{
				Type:   "document_status_update",
				JobID:  documentID,
				Status: doc.Status,
				Progress: calculateProgress(doc.Status),
				Data: map[string]interface{}{
					"document_id":    doc.ID,
					"status":         doc.Status,
					"chunk_count":    doc.ChunkCount,
					"processing_time": doc.ProcessingTime,
					"confidence_score": doc.ConfidenceScore,
				},
				Timestamp: time.Now(),
			}

			if err := conn.WriteJSON(message); err != nil {
				return
			}

			// Close connection if processing is complete
			if doc.Status == "processed" || doc.Status == "failed" {
				return
			}
		}
	}
}