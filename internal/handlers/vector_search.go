package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// VectorSearchHandler handles vector search HTTP requests
type VectorSearchHandler struct {
	notebookService *services.NotebookService
	documentService *services.DocumentService
	userService     *services.UserService
	deeplakeConfig  *config.DeepLakeConfig
	httpClient      *http.Client
	logger          *logger.Logger
}

// NewVectorSearchHandler creates a new vector search handler
func NewVectorSearchHandler(
	notebookService *services.NotebookService,
	documentService *services.DocumentService,
	userService *services.UserService,
	deeplakeConfig *config.DeepLakeConfig,
	log *logger.Logger,
) *VectorSearchHandler {
	return &VectorSearchHandler{
		notebookService: notebookService,
		documentService: documentService,
		userService:     userService,
		deeplakeConfig:  deeplakeConfig,
		httpClient: &http.Client{
			Timeout: time.Duration(deeplakeConfig.TimeoutSeconds) * time.Second,
		},
		logger: log.WithService("vector_search_handler"),
	}
}

// TextSearchRequest represents a text-based vector search request
type TextSearchRequest struct {
	QueryText string        `json:"query_text" binding:"required"`
	Options   SearchOptions `json:"options"`
}

// HybridSearchRequest represents a hybrid vector search request
type HybridSearchRequest struct {
	QueryText    string        `json:"query_text"`
	QueryVector  []float64     `json:"query_vector,omitempty"`
	Options      SearchOptions `json:"options"`
	VectorWeight float64       `json:"vector_weight"`
	TextWeight   float64       `json:"text_weight"`
	FusionMethod string        `json:"fusion_method"`
}

// SearchOptions represents search options
type SearchOptions struct {
	TopK            int                    `json:"top_k"`
	MinScore        float64                `json:"min_score"`
	Threshold       *float64               `json:"threshold,omitempty"`
	MaxDistance     *float64               `json:"max_distance,omitempty"`
	Deduplicate     bool                   `json:"deduplicate"`
	GroupByDocument bool                   `json:"group_by_document"`
	Rerank          bool                   `json:"rerank"`
	IncludeContent  bool                   `json:"include_content"`
	IncludeMetadata bool                   `json:"include_metadata"`
	Filters         map[string]interface{} `json:"filters,omitempty"`
}

// VectorSearchInfo represents vector store info for a notebook
type VectorSearchInfo struct {
	DatasetID       string `json:"dataset_id"`
	VectorCount     int64  `json:"vector_count"`
	Dimensions      int    `json:"dimensions"`
	IndexType       string `json:"index_type"`
	LastUpdated     string `json:"last_updated,omitempty"`
	DocumentCount   int    `json:"document_count"`
	StorageSize     int64  `json:"storage_size"`
	NotebookID      string `json:"notebook_id"`
	SpaceID         string `json:"space_id"`
	TenantID        string `json:"tenant_id"`
	IndexingEnabled bool   `json:"indexing_enabled"`
}

// TextSearch performs text-based vector search on a notebook's indexed documents
// @Summary Text-based vector search
// @Description Search indexed documents using text query (converts to embeddings automatically)
// @Tags vector-search
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Param request body TextSearchRequest true "Search request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id}/vector-search/text [post]
func (h *VectorSearchHandler) TextSearch(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Notebook ID is required"))
		return
	}

	// Get space context for tenant ID
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Verify notebook exists and user has access
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	notebook, err := h.notebookService.GetNotebookByID(c.Request.Context(), notebookID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get notebook", zap.Error(err), zap.String("notebook_id", notebookID))
		handleServiceError(c, err)
		return
	}

	var req TextSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	if req.QueryText == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("query_text is required"))
		return
	}

	// Set default options if not provided
	if req.Options.TopK == 0 {
		req.Options.TopK = 10
	}
	if req.Options.TopK > 100 {
		req.Options.TopK = 100
	}

	// Construct dataset ID from notebook ID
	// Dataset naming convention: notebook_<notebook_id>
	datasetID := h.constructDatasetID(notebookID, spaceContext.SpaceID)

	// TEMPORARILY DISABLED: document_id filter doesn't work because Neo4j document IDs
	// don't match AudiModal file IDs stored in vectors. See GitHub issue for ID mapping fix.
	// When using the default dataset (temporary fix), add document_id filter to scope
	// search results to only documents belonging to this notebook
	if false && h.deeplakeConfig.UseDefaultDataset { // DISABLED FOR TESTING
		documentIDs, err := h.getDocumentIDsForNotebook(c.Request.Context(), notebookID, userID, spaceContext)
		if err != nil {
			h.logger.Warn("Failed to get document IDs for notebook, search may return results from other notebooks",
				zap.Error(err),
				zap.String("notebook_id", notebookID),
			)
		} else if len(documentIDs) > 0 {
			// Add document_id filter to the request options
			if req.Options.Filters == nil {
				req.Options.Filters = make(map[string]interface{})
			}
			req.Options.Filters["document_id"] = documentIDs
			h.logger.Debug("Added document_id filter for default dataset search",
				zap.String("notebook_id", notebookID),
				zap.Int("document_count", len(documentIDs)),
			)
		} else {
			// No processed documents in notebook - return empty result
			h.logger.Info("No processed documents found in notebook",
				zap.String("notebook_id", notebookID),
			)
			c.JSON(http.StatusOK, map[string]interface{}{
				"results":       []interface{}{},
				"total":         0,
				"query_time_ms": 0,
				"message":       "No documents have been indexed for this notebook yet. Upload and process documents to enable vector search.",
			})
			return
		}
	}

	// Proxy request to DeepLake API
	result, err := h.proxyTextSearch(c.Request.Context(), datasetID, spaceContext.SpaceID, req)
	if err != nil {
		// Check if this is a "dataset not found" error (404 from DeepLake)
		// In this case, return an empty result set rather than an error
		if isDatasetNotFoundError(err) {
			h.logger.Info("Dataset not found, returning empty results",
				zap.String("notebook_id", notebookID),
				zap.String("dataset_id", datasetID),
			)
			c.JSON(http.StatusOK, map[string]interface{}{
				"results":       []interface{}{},
				"total":         0,
				"query_time_ms": 0,
				"message":       "No documents have been indexed for this notebook yet. Upload and process documents to enable vector search.",
			})
			return
		}

		h.logger.Error("Vector search failed",
			zap.Error(err),
			zap.String("notebook_id", notebookID),
			zap.String("dataset_id", datasetID),
		)
		c.JSON(http.StatusInternalServerError, errors.InternalWithCause("Vector search failed", err))
		return
	}

	h.logger.Info("Vector text search completed",
		zap.String("notebook_id", notebookID),
		zap.String("notebook_name", notebook.Name),
		zap.String("query", req.QueryText[:minInt(len(req.QueryText), 50)]),
		zap.Int("top_k", req.Options.TopK),
	)

	c.JSON(http.StatusOK, result)
}

// HybridSearch performs hybrid vector+text search on a notebook's indexed documents
// @Summary Hybrid vector search
// @Description Search indexed documents using both vector and text search
// @Tags vector-search
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Param request body HybridSearchRequest true "Search request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id}/vector-search/hybrid [post]
func (h *VectorSearchHandler) HybridSearch(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Notebook ID is required"))
		return
	}

	// Get space context for tenant ID
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Verify notebook exists and user has access
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	_, err = h.notebookService.GetNotebookByID(c.Request.Context(), notebookID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get notebook", zap.Error(err), zap.String("notebook_id", notebookID))
		handleServiceError(c, err)
		return
	}

	var req HybridSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	if req.QueryText == "" && len(req.QueryVector) == 0 {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Either query_text or query_vector is required"))
		return
	}

	// Set defaults
	if req.Options.TopK == 0 {
		req.Options.TopK = 10
	}
	if req.VectorWeight == 0 && req.TextWeight == 0 {
		req.VectorWeight = 0.5
		req.TextWeight = 0.5
	}
	if req.FusionMethod == "" {
		req.FusionMethod = "weighted_sum"
	}

	// Construct dataset ID
	datasetID := h.constructDatasetID(notebookID, spaceContext.SpaceID)

	// TEMPORARILY DISABLED: document_id filter doesn't work because Neo4j document IDs
	// don't match AudiModal file IDs stored in vectors. See GitHub issue for ID mapping fix.
	// When using the default dataset (temporary fix), add document_id filter to scope
	// search results to only documents belonging to this notebook
	if false && h.deeplakeConfig.UseDefaultDataset { // DISABLED FOR TESTING
		documentIDs, err := h.getDocumentIDsForNotebook(c.Request.Context(), notebookID, userID, spaceContext)
		if err != nil {
			h.logger.Warn("Failed to get document IDs for notebook, search may return results from other notebooks",
				zap.Error(err),
				zap.String("notebook_id", notebookID),
			)
		} else if len(documentIDs) > 0 {
			// Add document_id filter to the request options
			if req.Options.Filters == nil {
				req.Options.Filters = make(map[string]interface{})
			}
			req.Options.Filters["document_id"] = documentIDs
			h.logger.Debug("Added document_id filter for default dataset hybrid search",
				zap.String("notebook_id", notebookID),
				zap.Int("document_count", len(documentIDs)),
			)
		} else {
			// No processed documents in notebook - return empty result
			h.logger.Info("No processed documents found in notebook",
				zap.String("notebook_id", notebookID),
			)
			c.JSON(http.StatusOK, map[string]interface{}{
				"results":       []interface{}{},
				"total":         0,
				"query_time_ms": 0,
				"message":       "No documents have been indexed for this notebook yet. Upload and process documents to enable vector search.",
			})
			return
		}
	}

	// Proxy request to DeepLake API
	result, err := h.proxyHybridSearch(c.Request.Context(), datasetID, spaceContext.SpaceID, req)
	if err != nil {
		// Check if this is a "dataset not found" error (404 from DeepLake)
		// In this case, return an empty result set rather than an error
		if isDatasetNotFoundError(err) {
			h.logger.Info("Dataset not found, returning empty results",
				zap.String("notebook_id", notebookID),
				zap.String("dataset_id", datasetID),
			)
			c.JSON(http.StatusOK, map[string]interface{}{
				"results":       []interface{}{},
				"total":         0,
				"query_time_ms": 0,
				"message":       "No documents have been indexed for this notebook yet. Upload and process documents to enable vector search.",
			})
			return
		}

		h.logger.Error("Hybrid search failed",
			zap.Error(err),
			zap.String("notebook_id", notebookID),
			zap.String("dataset_id", datasetID),
		)
		c.JSON(http.StatusInternalServerError, errors.InternalWithCause("Hybrid search failed", err))
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetVectorSearchInfo returns vector store info for a notebook
// @Summary Get vector store info
// @Description Get information about the vector store for a notebook
// @Tags vector-search
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Success 200 {object} VectorSearchInfo
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id}/vector-search/info [get]
func (h *VectorSearchHandler) GetVectorSearchInfo(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Notebook ID is required"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Verify notebook exists and user has access
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	notebook, err := h.notebookService.GetNotebookByID(c.Request.Context(), notebookID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get notebook", zap.Error(err), zap.String("notebook_id", notebookID))
		handleServiceError(c, err)
		return
	}

	// Construct dataset ID
	datasetID := h.constructDatasetID(notebookID, spaceContext.SpaceID)

	// Get dataset info from DeepLake API
	info, err := h.getDatasetInfo(c.Request.Context(), datasetID, spaceContext.SpaceID)
	if err != nil {
		// If dataset doesn't exist, return empty info
		h.logger.Warn("Dataset not found, returning empty info",
			zap.Error(err),
			zap.String("dataset_id", datasetID),
		)
		info = &VectorSearchInfo{
			DatasetID:       datasetID,
			VectorCount:     0,
			Dimensions:      h.deeplakeConfig.VectorDimensions,
			IndexType:       "default",
			DocumentCount:   notebook.DocumentCount,
			NotebookID:      notebookID,
			SpaceID:         spaceContext.SpaceID,
			TenantID:        spaceContext.SpaceID,
			IndexingEnabled: h.deeplakeConfig.Enabled,
		}
	}

	c.JSON(http.StatusOK, info)
}

// getDocumentIDsForNotebook retrieves the IDs of all processed documents in a notebook.
// This is used when UseDefaultDataset is enabled to filter search results to only
// documents belonging to this notebook (since all documents are in the shared "default" dataset).
func (h *VectorSearchHandler) getDocumentIDsForNotebook(ctx context.Context, notebookID, userID string, spaceContext *models.SpaceContext) ([]string, error) {
	// Get documents from Neo4j with a high limit to capture all document IDs
	// We only need IDs, but the service returns full documents
	docs, err := h.documentService.ListDocumentsByNotebook(ctx, notebookID, userID, spaceContext, 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get documents for notebook: %w", err)
	}

	// Extract document IDs - only include processed documents that would have vectors
	documentIDs := make([]string, 0, len(docs.Documents))
	for _, doc := range docs.Documents {
		// Only include documents that have been processed and would have vectors indexed
		if doc.Status == "processed" || doc.Status == "completed" || doc.Status == "indexed" {
			documentIDs = append(documentIDs, doc.ID)
		}
	}

	return documentIDs, nil
}

// constructDatasetID creates the dataset ID for a notebook
// Format: notebook_{notebook_id} or {space_id}_notebook_{notebook_id}
// When UseDefaultDataset is enabled (temporary fix), returns "default" to query
// the shared dataset where documents are currently being indexed by AudiModal.
func (h *VectorSearchHandler) constructDatasetID(notebookID, spaceID string) string {
	// Check feature flag - use default dataset temporarily while indexing pipeline is fixed
	// When DEEPLAKE_USE_DEFAULT_DATASET=true, query the "documents" dataset where AudiModal
	// indexes documents, and filter by document_id to scope to notebook
	if h.deeplakeConfig.UseDefaultDataset {
		return "documents"
	}

	// Original notebook-specific dataset logic (use when pipeline is fixed)
	// Using space_id prefix for multi-tenant isolation
	if spaceID != "" {
		return fmt.Sprintf("%s_notebook_%s", spaceID, notebookID)
	}
	return fmt.Sprintf("notebook_%s", notebookID)
}

// proxyTextSearch proxies the text search request to DeepLake API
func (h *VectorSearchHandler) proxyTextSearch(ctx context.Context, datasetID, tenantID string, req TextSearchRequest) (map[string]interface{}, error) {
	// Construct the request payload for DeepLake API
	payload := map[string]interface{}{
		"query_text": req.QueryText,
		"options":    req.Options,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/datasets/%s/search/text", h.deeplakeConfig.BaseURL, datasetID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if h.deeplakeConfig.APIKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("ApiKey %s", h.deeplakeConfig.APIKey))
	}
	// Pass tenant ID for multi-tenant support
	if tenantID != "" {
		httpReq.Header.Set("X-Tenant-ID", tenantID)
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("DeepLake API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result, nil
}

// proxyHybridSearch proxies the hybrid search request to DeepLake API
func (h *VectorSearchHandler) proxyHybridSearch(ctx context.Context, datasetID, tenantID string, req HybridSearchRequest) (map[string]interface{}, error) {
	// Construct the request payload for DeepLake API
	payload := map[string]interface{}{
		"query_text":    req.QueryText,
		"options":       req.Options,
		"vector_weight": req.VectorWeight,
		"text_weight":   req.TextWeight,
		"fusion_method": req.FusionMethod,
	}
	if len(req.QueryVector) > 0 {
		payload["query_vector"] = req.QueryVector
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/datasets/%s/search/hybrid", h.deeplakeConfig.BaseURL, datasetID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if h.deeplakeConfig.APIKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("ApiKey %s", h.deeplakeConfig.APIKey))
	}
	if tenantID != "" {
		httpReq.Header.Set("X-Tenant-ID", tenantID)
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("DeepLake API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result, nil
}

// getDatasetInfo gets dataset information from DeepLake API
func (h *VectorSearchHandler) getDatasetInfo(ctx context.Context, datasetID, tenantID string) (*VectorSearchInfo, error) {
	url := fmt.Sprintf("%s/api/v1/datasets/%s", h.deeplakeConfig.BaseURL, datasetID)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if h.deeplakeConfig.APIKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("ApiKey %s", h.deeplakeConfig.APIKey))
	}
	if tenantID != "" {
		httpReq.Header.Set("X-Tenant-ID", tenantID)
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("dataset not found")
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DeepLake API error (status %d): %s", resp.StatusCode, string(body))
	}

	var dataset struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Dimensions    int    `json:"dimensions"`
		IndexType     string `json:"index_type"`
		VectorCount   int64  `json:"vector_count"`
		StorageSize   int64  `json:"storage_size"`
		LastUpdated   string `json:"updated_at"`
		DocumentCount int    `json:"document_count,omitempty"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(body, &dataset); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &VectorSearchInfo{
		DatasetID:       dataset.ID,
		VectorCount:     dataset.VectorCount,
		Dimensions:      dataset.Dimensions,
		IndexType:       dataset.IndexType,
		LastUpdated:     dataset.LastUpdated,
		StorageSize:     dataset.StorageSize,
		IndexingEnabled: h.deeplakeConfig.Enabled,
	}, nil
}

// minInt returns the minimum of two integers
// Using a different name to avoid conflicts with built-in min in Go 1.21+
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isDatasetNotFoundError checks if an error indicates the dataset doesn't exist
// This happens when no documents have been indexed for a notebook yet
func isDatasetNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for 404 status code or "not found" message from DeepLake API
	return strings.Contains(errStr, "status 404") ||
		strings.Contains(errStr, "dataset not found") ||
		strings.Contains(errStr, "Dataset not found") ||
		strings.Contains(errStr, "not found")
}
