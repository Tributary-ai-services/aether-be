package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// StreamHandler handles live streaming HTTP requests and WebSocket connections
type StreamHandler struct {
	streamService *services.StreamService
	logger        *logger.Logger
	upgrader      websocket.Upgrader
}

// NewStreamHandler creates a new stream handler
func NewStreamHandler(streamService *services.StreamService, log *logger.Logger) *StreamHandler {
	return &StreamHandler{
		streamService: streamService,
		logger:        log.WithService("stream_handler"),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// In production, implement proper origin checking
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
}

// CreateStreamSource creates a new stream source
// @Summary Create stream source
// @Description Create a new live data stream source
// @Tags streams
// @Accept json
// @Produce json
// @Security Bearer
// @Param source body models.CreateStreamSourceRequest true "Stream source creation request"
// @Success 201 {object} models.StreamSource
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/streams/sources [post]
func (h *StreamHandler) CreateStreamSource(c *gin.Context) {
	var req models.CreateStreamSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request body", err))
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

	h.logger.Info("Creating stream source", 
		zap.String("name", req.Name), 
		zap.String("type", req.Type),
		zap.String("provider", req.Provider),
		zap.String("user_id", userID))

	source, err := h.streamService.CreateStreamSource(c.Request.Context(), req, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to create stream source", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to create stream source"))
		return
	}

	c.JSON(http.StatusCreated, source)
}

// GetStreamSources retrieves stream sources
// @Summary Get stream sources
// @Description Get a list of stream sources for the current tenant
// @Tags streams
// @Produce json
// @Security Bearer
// @Param limit query int false "Number of sources to return" default(10)
// @Param offset query int false "Number of sources to skip" default(0)
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/streams/sources [get]
func (h *StreamHandler) GetStreamSources(c *gin.Context) {
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

	// Parse pagination parameters
	limit := 10
	offset := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	h.logger.Info("Getting stream sources", 
		zap.String("user_id", userID), 
		zap.Int("limit", limit), 
		zap.Int("offset", offset))

	sources, total, err := h.streamService.GetStreamSources(c.Request.Context(), spaceContext, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get stream sources", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get stream sources"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"sources": sources,
		"pagination": gin.H{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// GetStreamSource retrieves a specific stream source
// @Summary Get stream source
// @Description Get a specific stream source by ID
// @Tags streams
// @Produce json
// @Security Bearer
// @Param id path string true "Stream Source ID"
// @Success 200 {object} models.StreamSource
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/streams/sources/{id} [get]
func (h *StreamHandler) GetStreamSource(c *gin.Context) {
	sourceID := c.Param("id")
	if sourceID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Stream source ID is required", nil))
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

	h.logger.Info("Getting stream source", zap.String("source_id", sourceID), zap.String("user_id", userID))

	source, err := h.streamService.GetStreamSourceByID(c.Request.Context(), sourceID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get stream source", zap.String("source_id", sourceID), zap.Error(err))
		c.JSON(http.StatusNotFound, errors.NotFound("Stream source not found"))
		return
	}

	c.JSON(http.StatusOK, source)
}

// UpdateStreamSource updates an existing stream source
// @Summary Update stream source
// @Description Update an existing stream source
// @Tags streams
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Stream Source ID"
// @Param source body models.UpdateStreamSourceRequest true "Stream source update request"
// @Success 200 {object} models.StreamSource
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/streams/sources/{id} [put]
func (h *StreamHandler) UpdateStreamSource(c *gin.Context) {
	sourceID := c.Param("id")
	if sourceID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Stream source ID is required", nil))
		return
	}

	var req models.UpdateStreamSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request body", err))
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

	h.logger.Info("Updating stream source", zap.String("source_id", sourceID), zap.String("user_id", userID))

	source, err := h.streamService.UpdateStreamSource(c.Request.Context(), sourceID, req, spaceContext)
	if err != nil {
		h.logger.Error("Failed to update stream source", zap.String("source_id", sourceID), zap.Error(err))
		if err.Error() == "stream source not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("Stream source not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to update stream source"))
		}
		return
	}

	c.JSON(http.StatusOK, source)
}

// DeleteStreamSource deletes a stream source
// @Summary Delete stream source
// @Description Delete a stream source
// @Tags streams
// @Security Bearer
// @Param id path string true "Stream Source ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/streams/sources/{id} [delete]
func (h *StreamHandler) DeleteStreamSource(c *gin.Context) {
	sourceID := c.Param("id")
	if sourceID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Stream source ID is required", nil))
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

	h.logger.Info("Deleting stream source", zap.String("source_id", sourceID), zap.String("user_id", userID))

	err = h.streamService.DeleteStreamSource(c.Request.Context(), sourceID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to delete stream source", zap.String("source_id", sourceID), zap.Error(err))
		if err.Error() == "stream source not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("Stream source not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to delete stream source"))
		}
		return
	}

	c.Status(http.StatusNoContent)
}

// GetLiveEvents retrieves live events
// @Summary Get live events
// @Description Get a list of live events with optional filtering
// @Tags streams
// @Produce json
// @Security Bearer
// @Param limit query int false "Number of events to return" default(50)
// @Param offset query int false "Number of events to skip" default(0)
// @Param source_ids query string false "Comma-separated list of source IDs to filter by"
// @Param event_types query string false "Comma-separated list of event types to filter by"
// @Param media_types query string false "Comma-separated list of media types to filter by"
// @Param sentiments query string false "Comma-separated list of sentiments to filter by"
// @Param min_confidence query number false "Minimum confidence score to filter by"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/streams/events [get]
func (h *StreamHandler) GetLiveEvents(c *gin.Context) {
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

	// Parse pagination parameters
	limit := 50
	offset := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Parse filters
	filters := models.StreamFilters{}
	
	if sourceIDs := c.Query("source_ids"); sourceIDs != "" {
		filters.SourceIDs = parseCommaSeparated(sourceIDs)
	}
	if eventTypes := c.Query("event_types"); eventTypes != "" {
		filters.EventTypes = parseCommaSeparated(eventTypes)
	}
	if mediaTypes := c.Query("media_types"); mediaTypes != "" {
		filters.MediaTypes = parseCommaSeparated(mediaTypes)
	}
	if sentiments := c.Query("sentiments"); sentiments != "" {
		filters.Sentiments = parseCommaSeparated(sentiments)
	}
	if minConfStr := c.Query("min_confidence"); minConfStr != "" {
		if minConf, err := strconv.ParseFloat(minConfStr, 64); err == nil {
			filters.MinConfidence = minConf
		}
	}

	h.logger.Info("Getting live events", 
		zap.String("user_id", userID), 
		zap.Int("limit", limit), 
		zap.Int("offset", offset),
		zap.Any("filters", filters))

	events, total, err := h.streamService.GetLiveEvents(c.Request.Context(), spaceContext, filters, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get live events", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get live events"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"pagination": gin.H{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
		"filters": filters,
	})
}

// IngestEvent ingests a new live event (for testing/simulation)
// @Summary Ingest live event
// @Description Ingest a new live event into the system (for testing/simulation)
// @Tags streams
// @Accept json
// @Produce json
// @Security Bearer
// @Param event body map[string]interface{} true "Event ingestion request"
// @Success 201 {object} models.LiveEvent
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/streams/events [post]
func (h *StreamHandler) IngestEvent(c *gin.Context) {
	var req struct {
		SourceID   string                 `json:"source_id" binding:"required"`
		EventType  string                 `json:"event_type" binding:"required"`
		Content    string                 `json:"content" binding:"required"`
		MediaType  string                 `json:"media_type" binding:"required"`
		MediaURL   string                 `json:"media_url,omitempty"`
		Metadata   map[string]interface{} `json:"metadata"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request body", err))
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

	h.logger.Info("Ingesting live event", 
		zap.String("source_id", req.SourceID),
		zap.String("event_type", req.EventType),
		zap.String("media_type", req.MediaType),
		zap.String("user_id", userID))

	event, err := h.streamService.IngestEvent(c.Request.Context(), req.SourceID, req.EventType, req.Content, req.MediaType, req.Metadata, spaceContext)
	if err != nil {
		h.logger.Error("Failed to ingest event", zap.Error(err))
		if err.Error() == "stream source not found" {
			c.JSON(http.StatusBadRequest, errors.BadRequest("Stream source not found"))
		} else if err.Error() == "stream source is not active" {
			c.JSON(http.StatusBadRequest, errors.BadRequest("Stream source is not active"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to ingest event"))
		}
		return
	}

	c.JSON(http.StatusCreated, event)
}

// GetStreamAnalytics retrieves stream performance analytics
// @Summary Get stream analytics
// @Description Get stream performance analytics for the current tenant
// @Tags streams
// @Produce json
// @Security Bearer
// @Param period query string false "Analytics period" default(realtime) Enums(realtime, hourly, daily)
// @Success 200 {object} models.StreamAnalytics
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/streams/analytics [get]
func (h *StreamHandler) GetStreamAnalytics(c *gin.Context) {
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

	period := c.DefaultQuery("period", "realtime")
	if period != "realtime" && period != "hourly" && period != "daily" {
		period = "realtime"
	}

	h.logger.Info("Getting stream analytics", zap.String("user_id", userID), zap.String("period", period))

	analytics, err := h.streamService.GetStreamAnalytics(c.Request.Context(), spaceContext, period)
	if err != nil {
		h.logger.Error("Failed to get stream analytics", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get stream analytics"))
		return
	}

	c.JSON(http.StatusOK, analytics)
}

// StreamEvents handles WebSocket connections for real-time event streaming
// @Summary Stream live events
// @Description Get real-time live events via WebSocket
// @Tags streams
// @Security Bearer
// @Param source_ids query string false "Comma-separated list of source IDs to filter by"
// @Param event_types query string false "Comma-separated list of event types to filter by"
// @Param media_types query string false "Comma-separated list of media types to filter by"
// @Param sentiments query string false "Comma-separated list of sentiments to filter by"
// @Param min_confidence query number false "Minimum confidence score to filter by"
// @Router /api/v1/streams/events/stream [get]
func (h *StreamHandler) StreamEvents(c *gin.Context) {
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

	// Parse filters from query parameters
	filters := models.StreamFilters{}
	
	if sourceIDs := c.Query("source_ids"); sourceIDs != "" {
		filters.SourceIDs = parseCommaSeparated(sourceIDs)
	}
	if eventTypes := c.Query("event_types"); eventTypes != "" {
		filters.EventTypes = parseCommaSeparated(eventTypes)
	}
	if mediaTypes := c.Query("media_types"); mediaTypes != "" {
		filters.MediaTypes = parseCommaSeparated(mediaTypes)
	}
	if sentiments := c.Query("sentiments"); sentiments != "" {
		filters.Sentiments = parseCommaSeparated(sentiments)
	}
	if minConfStr := c.Query("min_confidence"); minConfStr != "" {
		if minConf, err := strconv.ParseFloat(minConfStr, 64); err == nil {
			filters.MinConfidence = minConf
		}
	}

	h.logger.Info("Starting WebSocket event stream", 
		zap.String("user_id", userID),
		zap.String("tenant_id", spaceContext.TenantID),
		zap.Any("filters", filters))

	// Upgrade HTTP connection to WebSocket
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("Failed to upgrade WebSocket connection", zap.Error(err))
		return
	}
	defer conn.Close()

	// Create stream connection
	streamConn := models.NewStreamConnection(userID, spaceContext.TenantID, filters)
	
	// Register connection with stream service
	h.streamService.AddStreamConnection(streamConn)
	defer h.streamService.RemoveStreamConnection(streamConn.ID)

	// Send initial connection confirmation
	confirmationMsg := models.StreamEventWebSocketMessage{
		Type:      "connection_established",
		Timestamp: time.Now(),
	}
	
	if err := conn.WriteJSON(confirmationMsg); err != nil {
		h.logger.Error("Failed to send connection confirmation", zap.Error(err))
		return
	}

	// Handle WebSocket connection
	h.handleWebSocketConnection(conn, streamConn)
}

// handleWebSocketConnection handles an active WebSocket connection
func (h *StreamHandler) handleWebSocketConnection(conn *websocket.Conn, streamConn *models.StreamConnection) {
	// Set up ping/pong handlers for connection health
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start ping ticker
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	// Start analytics ticker (send analytics every 10 seconds)
	analyticsTicker := time.NewTicker(10 * time.Second)
	defer analyticsTicker.Stop()

	for {
		select {
		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.logger.Debug("Failed to send ping", zap.String("connection_id", streamConn.ID), zap.Error(err))
				return
			}

		case <-analyticsTicker.C:
			// Send periodic analytics updates
			spaceContext := &models.SpaceContext{TenantID: streamConn.TenantID}
			analytics, err := h.streamService.GetStreamAnalytics(context.Background(), spaceContext, "realtime")
			if err != nil {
				h.logger.Error("Failed to get analytics for WebSocket", zap.Error(err))
				continue
			}

			analyticsMsg := models.StreamEventWebSocketMessage{
				Type:      "analytics_update",
				Analytics: analytics,
				Timestamp: time.Now(),
			}

			if err := conn.WriteJSON(analyticsMsg); err != nil {
				h.logger.Debug("Failed to send analytics update", zap.String("connection_id", streamConn.ID), zap.Error(err))
				return
			}

		default:
			// Read messages from client (for potential commands or heartbeat)
			messageType, _, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					h.logger.Error("WebSocket error", zap.String("connection_id", streamConn.ID), zap.Error(err))
				}
				return
			}

			if messageType == websocket.CloseMessage {
				return
			}

			// For now, we don't handle specific client messages
			// In a full implementation, clients could send filter updates, etc.
		}
	}
}

// UpdateStreamSourceStatus updates the status of a stream source
// @Summary Update stream source status
// @Description Update the status of a specific stream source (activate/pause/disconnect)
// @Tags streams
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Stream Source ID"
// @Param request body object true "Status update request"
// @Success 200 {object} models.StreamSource
// @Failure 400 {object} object
// @Failure 404 {object} object
// @Failure 500 {object} object
// @Router /api/v1/streams/sources/{id}/status [put]
func (h *StreamHandler) UpdateStreamSourceStatus(c *gin.Context) {
	sourceID := c.Param("id")
	spaceContext, _ := middleware.GetSpaceContext(c)

	var req struct {
		Status string `json:"status" binding:"required,oneof=active paused disconnected"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	source, err := h.streamService.UpdateStreamSourceStatus(c.Request.Context(), sourceID, req.Status, spaceContext)
	if err != nil {
		h.logger.Error("Failed to update stream source status", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stream source status"})
		return
	}

	if source == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stream source not found"})
		return
	}

	c.JSON(http.StatusOK, source)
}

// GetLiveEvent gets a specific live event by ID
// @Summary Get live event
// @Description Get a specific live event by ID
// @Tags streams
// @Produce json
// @Security Bearer
// @Param id path string true "Live Event ID"
// @Success 200 {object} models.LiveEvent
// @Failure 404 {object} object
// @Failure 500 {object} object
// @Router /api/v1/streams/events/{id} [get]
func (h *StreamHandler) GetLiveEvent(c *gin.Context) {
	eventID := c.Param("id")
	spaceContext, _ := middleware.GetSpaceContext(c)

	event, err := h.streamService.GetLiveEventByID(c.Request.Context(), eventID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get live event", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get live event"})
		return
	}

	if event == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Live event not found"})
		return
	}

	c.JSON(http.StatusOK, event)
}

// GetRealtimeAnalytics gets real-time analytics snapshot
// @Summary Get real-time analytics
// @Description Get current real-time analytics and performance metrics
// @Tags streams
// @Produce json
// @Security Bearer
// @Success 200 {object} models.StreamAnalytics
// @Failure 500 {object} object
// @Router /api/v1/streams/analytics/realtime [get]
func (h *StreamHandler) GetRealtimeAnalytics(c *gin.Context) {
	spaceContext, _ := middleware.GetSpaceContext(c)

	analytics, err := h.streamService.GetRealtimeAnalytics(c.Request.Context(), spaceContext)
	if err != nil {
		h.logger.Error("Failed to get real-time analytics", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get real-time analytics"})
		return
	}

	c.JSON(http.StatusOK, analytics)
}

// parseCommaSeparated parses a comma-separated string into a slice
func parseCommaSeparated(input string) []string {
	if input == "" {
		return nil
	}
	
	parts := make([]string, 0)
	for _, part := range strings.Split(input, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	
	return parts
}