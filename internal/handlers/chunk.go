package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// ChunkHandler handles chunk-related HTTP requests
type ChunkHandler struct {
	db             *database.Neo4jClient
	chunkService   *services.ChunkService
	audiModalService *services.AudiModalService
	logger         *logger.Logger
}

// NewChunkHandler creates a new chunk handler
func NewChunkHandler(db *database.Neo4jClient, chunkService *services.ChunkService, audiModalService *services.AudiModalService, logger *logger.Logger) *ChunkHandler {
	return &ChunkHandler{
		db:             db,
		chunkService:   chunkService,
		audiModalService: audiModalService,
		logger:         logger.WithService("chunk_handler"),
	}
}

// GetFileChunks retrieves all chunks for a specific file
// GET /api/v1/tenants/:tenant_id/files/:file_id/chunks
func (h *ChunkHandler) GetFileChunks(c *gin.Context) {
	// Get space context
	spaceCtx, exists := c.Get("space_context")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Space context required"})
		return
	}
	spaceContext := spaceCtx.(*models.SpaceContext)

	// Get file ID from path
	fileID := c.Param("file_id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File ID is required"})
		return
	}

	// Get pagination parameters
	limit := 50 // default
	offset := 0 // default

	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User authentication required"})
		return
	}

	h.logger.Info("Fetching chunks for file",
		zap.String("file_id", fileID),
		zap.String("user_id", userID.(string)),
		zap.String("tenant_id", spaceContext.TenantID),
		zap.Int("limit", limit),
		zap.Int("offset", offset))

	// Get chunks from AudiModal service
	chunks, err := h.audiModalService.GetFileChunks(c.Request.Context(), spaceContext.TenantID, fileID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch chunks from AudiModal",
			zap.String("file_id", fileID),
			zap.Error(err))
		
		// Handle different types of errors appropriately
		if errors.IsNotFound(err) {
			apiErr := errors.FileNotProcessedWithDetails("File has not been processed or chunks not found", map[string]interface{}{
				"file_id": fileID,
			})
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		if errors.IsExternalService(err) {
			apiErr := errors.ExternalService("AudiModal service is currently unavailable", err)
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		apiErr := errors.ChunkProcessingWithDetails("Failed to retrieve chunks", err, map[string]interface{}{
			"file_id": fileID,
		})
		c.JSON(apiErr.StatusCode, apiErr)
		return
	}

	// Convert to response format
	chunkResponses := make([]*models.ChunkResponse, len(chunks.Data))
	for i, chunk := range chunks.Data {
		chunkResponses[i] = convertAudiModalChunkToResponse(chunk)
	}

	response := &models.ChunkListResponse{
		Chunks:  chunkResponses,
		Total:   chunks.Total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+limit < chunks.Total,
	}

	c.JSON(http.StatusOK, response)
}

// GetChunk retrieves a specific chunk by ID
// GET /api/v1/tenants/:tenant_id/files/:file_id/chunks/:chunk_id
func (h *ChunkHandler) GetChunk(c *gin.Context) {
	// Get space context
	spaceCtx, exists := c.Get("space_context")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Space context required"})
		return
	}
	spaceContext := spaceCtx.(*models.SpaceContext)

	// Get parameters from path
	fileID := c.Param("file_id")
	chunkID := c.Param("chunk_id")

	if fileID == "" || chunkID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File ID and chunk ID are required"})
		return
	}

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User authentication required"})
		return
	}

	h.logger.Info("Fetching specific chunk",
		zap.String("file_id", fileID),
		zap.String("chunk_id", chunkID),
		zap.String("user_id", userID.(string)),
		zap.String("tenant_id", spaceContext.TenantID))

	// Get chunk from AudiModal service
	chunk, err := h.audiModalService.GetChunk(c.Request.Context(), spaceContext.TenantID, fileID, chunkID)
	if err != nil {
		h.logger.Error("Failed to fetch chunk from AudiModal",
			zap.String("file_id", fileID),
			zap.String("chunk_id", chunkID),
			zap.Error(err))
		
		// Handle different types of errors appropriately
		if errors.IsNotFound(err) {
			apiErr := errors.ChunkNotFoundWithDetails("Chunk not found", map[string]interface{}{
				"file_id":  fileID,
				"chunk_id": chunkID,
			})
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		if errors.IsExternalService(err) {
			apiErr := errors.ExternalService("AudiModal service is currently unavailable", err)
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		apiErr := errors.ChunkProcessingWithDetails("Failed to retrieve chunk", err, map[string]interface{}{
			"file_id":  fileID,
			"chunk_id": chunkID,
		})
		c.JSON(apiErr.StatusCode, apiErr)
		return
	}

	// Convert to response format
	response := convertAudiModalChunkToResponse(*chunk)
	c.JSON(http.StatusOK, response)
}

// SearchChunks searches for chunks across files
// POST /api/v1/tenants/:tenant_id/chunks/search
func (h *ChunkHandler) SearchChunks(c *gin.Context) {
	// Get space context
	spaceCtx, exists := c.Get("space_context")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Space context required"})
		return
	}
	_ = spaceCtx.(*models.SpaceContext) // spaceContext unused in this function

	// Parse search request
	var searchReq models.ChunkSearchRequest
	if err := c.ShouldBindJSON(&searchReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid search request", "details": err.Error()})
		return
	}

	// Validate search request
	if searchReq.Limit <= 0 || searchReq.Limit > 100 {
		searchReq.Limit = 20
	}
	if searchReq.Offset < 0 {
		searchReq.Offset = 0
	}

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User authentication required"})
		return
	}

	h.logger.Info("Searching chunks",
		zap.String("query", searchReq.Query),
		zap.String("user_id", userID.(string)),
		zap.Int("limit", searchReq.Limit),
		zap.Int("offset", searchReq.Offset))

	// For now, return mock search results
	// In a full implementation, this would query Neo4j or a search engine
	response := &models.ChunkListResponse{
		Chunks:  []*models.ChunkResponse{},
		Total:   0,
		Limit:   searchReq.Limit,
		Offset:  searchReq.Offset,
		HasMore: false,
	}

	c.JSON(http.StatusOK, response)
}

// GetAvailableStrategies retrieves available chunking strategies
// GET /api/v1/strategies
func (h *ChunkHandler) GetAvailableStrategies(c *gin.Context) {
	h.logger.Info("Fetching available chunking strategies")

	// Get strategies from AudiModal service
	strategies, err := h.audiModalService.GetAvailableStrategies(c.Request.Context())
	if err != nil {
		h.logger.Warn("Failed to fetch strategies from AudiModal, falling back to defaults", zap.Error(err))
		
		// Return default strategies if AudiModal is unavailable
		defaultStrategies := []map[string]interface{}{
			{
				"name":        "semantic",
				"description": "Splits text based on semantic boundaries (paragraphs, sentences)",
				"best_for":    []string{"natural language", "documents", "articles"},
				"data_types":  []string{"text", "unstructured", "documents"},
				"complexity":  "medium",
				"performance": "medium",
				"memory_usage": "medium",
			},
			{
				"name":        "fixed",
				"description": "Splits text into fixed-size chunks with optional overlap",
				"best_for":    []string{"simple text processing", "consistent chunk sizes"},
				"data_types":  []string{"text", "unstructured"},
				"complexity":  "low",
				"performance": "high",
				"memory_usage": "low",
			},
			{
				"name":        "adaptive",
				"description": "Automatically adapts to different data types and structures",
				"best_for":    []string{"mixed content", "unknown data types", "JSON"},
				"data_types":  []string{"mixed", "semi_structured", "json", "unknown"},
				"complexity":  "high",
				"performance": "medium",
				"memory_usage": "medium",
			},
			{
				"name":        "row_based",
				"description": "Groups structured data rows into chunks",
				"best_for":    []string{"CSV files", "database tables", "spreadsheets"},
				"data_types":  []string{"structured", "csv", "table"},
				"complexity":  "low",
				"performance": "high",
				"memory_usage": "low",
			},
		}
		
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    defaultStrategies,
		})
		return
	}

	c.JSON(http.StatusOK, strategies)
}

// GetOptimalStrategy gets recommended strategy for file characteristics
// POST /api/v1/strategies/recommend
func (h *ChunkHandler) GetOptimalStrategy(c *gin.Context) {
	var request struct {
		ContentType string `json:"content_type" binding:"required"`
		FileSize    int64  `json:"file_size" binding:"required,min=0"`
		Complexity  string `json:"complexity,omitempty"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	h.logger.Info("Getting optimal strategy recommendation",
		zap.String("content_type", request.ContentType),
		zap.Int64("file_size", request.FileSize),
		zap.String("complexity", request.Complexity))

	// Get recommendation from AudiModal service
	strategy, config, err := h.audiModalService.GetOptimalStrategy(
		c.Request.Context(),
		request.ContentType,
		request.FileSize,
		request.Complexity,
	)
	if err != nil {
		h.logger.Error("Failed to get strategy recommendation", zap.Error(err))
		
		// Handle different types of errors appropriately
		if errors.IsValidation(err) {
			apiErr := errors.StrategyValidation("Invalid content type or file size for strategy recommendation")
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		if errors.IsExternalService(err) {
			apiErr := errors.ExternalService("AudiModal service is currently unavailable", err)
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		apiErr := errors.StrategyError("Failed to get strategy recommendation", err)
		c.JSON(apiErr.StatusCode, apiErr)
		return
	}

	response := gin.H{
		"success": true,
		"data": gin.H{
			"strategy":        strategy,
			"strategy_config": config,
			"reasoning":       "Recommended based on content type and file size",
		},
	}

	c.JSON(http.StatusOK, response)
}

// ReprocessFileWithStrategy reprocesses a file with a different chunking strategy
// POST /api/v1/tenants/:tenant_id/files/:file_id/reprocess
func (h *ChunkHandler) ReprocessFileWithStrategy(c *gin.Context) {
	// Get space context
	spaceCtx, exists := c.Get("space_context")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Space context required"})
		return
	}
	spaceContext := spaceCtx.(*models.SpaceContext)

	// Get file ID from path
	fileID := c.Param("file_id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File ID is required"})
		return
	}

	// Parse request
	var request struct {
		Strategy       string                 `json:"strategy" binding:"required"`
		StrategyConfig map[string]interface{} `json:"strategy_config,omitempty"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User authentication required"})
		return
	}

	h.logger.Info("Reprocessing file with new strategy",
		zap.String("file_id", fileID),
		zap.String("strategy", request.Strategy),
		zap.String("user_id", userID.(string)),
		zap.String("tenant_id", spaceContext.TenantID))

	// Submit reprocessing request to AudiModal
	err := h.audiModalService.ReprocessFileWithStrategy(
		c.Request.Context(),
		spaceContext.TenantID,
		fileID,
		request.Strategy,
		request.StrategyConfig,
	)
	if err != nil {
		h.logger.Error("Failed to reprocess file",
			zap.String("file_id", fileID),
			zap.String("strategy", request.Strategy),
			zap.Error(err))
		
		// Handle different types of errors appropriately
		if errors.IsNotFound(err) {
			apiErr := errors.FileNotProcessedWithDetails("File not found or not available for reprocessing", map[string]interface{}{
				"file_id": fileID,
			})
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		if errors.IsValidation(err) {
			apiErr := errors.ValidationWithDetails("Invalid strategy or configuration", map[string]interface{}{
				"strategy": request.Strategy,
				"config":   request.StrategyConfig,
			})
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		if errors.IsConflict(err) {
			apiErr := errors.ProcessingInProgressWithDetails("File is currently being processed", map[string]interface{}{
				"file_id": fileID,
			})
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		if errors.IsExternalService(err) {
			apiErr := errors.ExternalService("AudiModal service is currently unavailable", err)
			c.JSON(apiErr.StatusCode, apiErr)
			return
		}
		
		apiErr := errors.ChunkProcessingWithDetails("Failed to initiate file reprocessing", err, map[string]interface{}{
			"file_id":  fileID,
			"strategy": request.Strategy,
		})
		c.JSON(apiErr.StatusCode, apiErr)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "File reprocessing initiated",
		"file_id": fileID,
		"strategy": request.Strategy,
	})
}

// convertAudiModalChunkToResponse converts AudiModal chunk data to our response format
func convertAudiModalChunkToResponse(chunk services.ChunkData) *models.ChunkResponse {
	qualityMetrics := models.ChunkQualityMetrics{}
	if chunk.Quality != nil {
		// Convert quality map to structured metrics
		if completeness, ok := chunk.Quality["completeness"].(float64); ok {
			qualityMetrics.Completeness = completeness
		}
		if coherence, ok := chunk.Quality["coherence"].(float64); ok {
			qualityMetrics.Coherence = coherence
		}
		if uniqueness, ok := chunk.Quality["uniqueness"].(float64); ok {
			qualityMetrics.Uniqueness = uniqueness
		}
		if readability, ok := chunk.Quality["readability"].(float64); ok {
			qualityMetrics.Readability = readability
		}
		if langConf, ok := chunk.Quality["language_conf"].(float64); ok {
			qualityMetrics.LanguageConf = langConf
		}
		if language, ok := chunk.Quality["language"].(string); ok {
			qualityMetrics.Language = language
		}
		if complexity, ok := chunk.Quality["complexity"].(float64); ok {
			qualityMetrics.Complexity = complexity
		}
		if density, ok := chunk.Quality["density"].(float64); ok {
			qualityMetrics.Density = density
		}
	}

	// Parse timestamps
	processedAt, _ := parseAudiModalTimestamp(chunk.ProcessedAt)
	createdAt, _ := parseAudiModalTimestamp(chunk.CreatedAt)
	updatedAt, _ := parseAudiModalTimestamp(chunk.UpdatedAt)

	return &models.ChunkResponse{
		ID:              chunk.ID,
		FileID:          chunk.FileID,
		ChunkID:         chunk.ID, // Use ID as ChunkID for now
		ChunkType:       chunk.ChunkType,
		ChunkNumber:     chunk.ChunkNumber,
		Content:         chunk.Content,
		ContentHash:     chunk.ContentHash,
		SizeBytes:       chunk.SizeBytes,
		StartPosition:   chunk.StartPosition,
		EndPosition:     chunk.EndPosition,
		PageNumber:      chunk.PageNumber,
		LineNumber:      chunk.LineNumber,
		ProcessedAt:     processedAt,
		ProcessedBy:     chunk.ProcessedBy,
		ProcessingTime:  chunk.ProcessingTime,
		Quality:         qualityMetrics,
		Language:        chunk.Language,
		LanguageConf:    chunk.LanguageConf,
		ContentCategory: chunk.ContentCategory,
		Classifications: chunk.Classifications,
		PIIDetected:     chunk.PIIDetected,
		DLPScanStatus:   chunk.DLPScanStatus,
		DLPScanResult:   chunk.DLPScanResult,
		Context:         chunk.Context,
		SchemaInfo:      chunk.SchemaInfo,
		Metadata:        chunk.Metadata,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}
}

// parseAudiModalTimestamp parses AudiModal timestamp strings
func parseAudiModalTimestamp(timestamp string) (time.Time, error) {
	if timestamp == "" {
		return time.Time{}, nil
	}
	
	// Try multiple timestamp formats
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}
	
	for _, layout := range layouts {
		if t, err := time.Parse(layout, timestamp); err == nil {
			return t, nil
		}
	}
	
	return time.Time{}, fmt.Errorf("unable to parse timestamp")
}