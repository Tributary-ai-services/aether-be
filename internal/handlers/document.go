package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// DocumentHandler handles document-related HTTP requests
type DocumentHandler struct {
	documentService   *services.DocumentService
	audiModalService  *services.AudiModalService
	logger            *logger.Logger
}

// NewDocumentHandler creates a new document handler
func NewDocumentHandler(documentService *services.DocumentService, audiModalService *services.AudiModalService, log *logger.Logger) *DocumentHandler {
	return &DocumentHandler{
		documentService:  documentService,
		audiModalService: audiModalService,
		logger:           log.WithService("document_handler"),
	}
}

// CreateDocument creates a new document record in Neo4j (without file upload)
// @Summary Create a new document record
// @Description Create a document record in Neo4j, typically after external upload to AudiModal
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param document body models.DocumentCreateRequest true "Document data"
// @Success 201 {object} models.DocumentResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents [post]
func (h *DocumentHandler) CreateDocument(c *gin.Context) {
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

	// DEBUG: Read and log raw body to investigate truncation issue
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Error("Failed to read request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.BadRequest("Failed to read request body"))
		return
	}

	// Log raw body size and look for NUL bytes
	h.logger.Debug("CreateDocument raw body received",
		zap.Int("raw_body_length", len(bodyBytes)),
		zap.Int("nul_byte_count", countNulBytes(bodyBytes)),
		zap.String("first_100_chars", safeString(bodyBytes, 100)),
		zap.String("last_100_chars", safeStringEnd(bodyBytes, 100)))

	// Restore the body for JSON binding
	c.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))

	var req models.DocumentCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("JSON binding failed",
			zap.Error(err),
			zap.Int("body_length", len(bodyBytes)))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request body", err))
		return
	}

	// Use Title as Name if Name is empty (for web scraping compatibility)
	if req.Name == "" && req.Title != "" {
		req.Name = req.Title
	}

	// Ensure we have a name
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Document name or title is required", nil))
		return
	}

	// Determine mime type - prefer ContentType field, then metadata
	var mimeType string
	if req.ContentType != "" {
		mimeType = req.ContentType
	} else if mt, ok := req.Metadata["mime_type"].(string); ok {
		mimeType = mt
	}

	// Get size from content or metadata
	var sizeBytes int64
	if req.Content != "" {
		sizeBytes = int64(len(req.Content))
	} else if sb, ok := req.Metadata["size_bytes"].(float64); ok {
		sizeBytes = int64(sb)
	} else if sb, ok := req.Metadata["size_bytes"].(int64); ok {
		sizeBytes = sb
	}

	// Debug logging for content size tracking
	h.logger.Info("CreateDocument request details",
		zap.String("name", req.Name),
		zap.String("title", req.Title),
		zap.String("source_type", req.SourceType),
		zap.Int("content_length", len(req.Content)),
		zap.Int64("calculated_size_bytes", sizeBytes),
		zap.String("content_type", req.ContentType),
		zap.String("notebook_id", req.NotebookID))

	fileInfo := models.FileInfo{
		OriginalName: req.Name,
		MimeType:     mimeType,
		SizeBytes:    sizeBytes,
		Checksum:     "", // Not available for inline content
	}

	// Create the document
	document, err := h.documentService.CreateDocument(c.Request.Context(), req, userID, spaceContext, fileInfo)
	if err != nil {
		h.logger.Error("Failed to create document", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// For inline content (web scraping, text input), submit to AudiModal for compliance and ML processing
	if req.Content != "" && h.audiModalService != nil {
		h.logger.Info("Submitting inline content to AudiModal for processing",
			zap.String("document_id", document.ID),
			zap.String("source_type", req.SourceType),
			zap.Int("content_length", len(req.Content)))

		// Determine filename for processing
		filename := req.Name
		if !strings.Contains(filename, ".") {
			// Add extension based on content type
			if mimeType == "text/markdown" {
				filename += ".md"
			} else {
				filename += ".txt"
			}
		}

		// Use text/markdown or text/plain for inline content
		processingMimeType := mimeType
		if processingMimeType == "" {
			processingMimeType = "text/plain"
		}

		// Build processing config
		processingConfig := map[string]interface{}{
			"extract_text":     true,
			"extract_metadata": true,
			"file_data":        []byte(req.Content),
			"filename":         filename,
			"mime_type":        processingMimeType,
			"source_type":      req.SourceType,
			"source_url":       req.SourceURL,
		}

		// Capture values for the goroutine (request context will be cancelled after response)
		docID := document.ID
		tenantID := spaceContext.TenantID
		docService := h.documentService
		audiService := h.audiModalService
		log := h.logger

		// Submit processing job asynchronously with a background context
		go func() {
			// Use a background context with timeout since the request context will be cancelled
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			log.Info("Goroutine started - submitting to AudiModal",
				zap.String("document_id", docID),
				zap.String("tenant_id", tenantID))

			job, err := audiService.SubmitProcessingJob(ctx, tenantID, docID, "extract", processingConfig)
			if err != nil {
				log.Error("Failed to submit inline content for processing",
					zap.String("document_id", docID),
					zap.Error(err))
				// Update document status to failed
				if updateErr := docService.UpdateProcessingResult(ctx, docID, "failed", nil, "Processing submission failed: "+err.Error()); updateErr != nil {
					log.Error("Failed to update document status after processing failure",
						zap.String("document_id", docID),
						zap.Error(updateErr))
				}
				return
			}

			log.Info("Inline content submitted for processing",
				zap.String("document_id", docID),
				zap.String("job_id", job.ID),
				zap.String("job_status", job.Status))
		}()
	}

	c.JSON(http.StatusCreated, document.ToResponse())
}

// UploadDocument uploads a new document
// @Summary Upload document
// @Description Upload a new document to a notebook
// @Tags documents
// @Accept multipart/form-data
// @Produce json
// @Security Bearer
// @Param notebook_id formData string true "Notebook ID"
// @Param name formData string false "Document name (optional, will use filename if not provided)"
// @Param description formData string false "Document description"
// @Param tags formData []string false "Document tags"
// @Param file formData file true "Document file"
// @Success 201 {object} models.DocumentResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 413 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/upload [post]
func (h *DocumentHandler) UploadDocument(c *gin.Context) {
	h.logger.Info("=== UPLOAD HANDLER START ===", 
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.String("content_type", c.Request.Header.Get("Content-Type")),
		zap.String("content_length", c.Request.Header.Get("Content-Length")),
		zap.Any("headers", c.Request.Header))
	
	userID := getUserID(c)
	if userID == "" {
		h.logger.Error("Upload failed: User not authenticated")
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}
	
	h.logger.Info("Processing upload for user", zap.String("user_id", userID))

	// Parse multipart form
	h.logger.Info("About to parse multipart form")
	err := c.Request.ParseMultipartForm(128 << 20) // 128MB max memory
	if err != nil {
		h.logger.Error("Failed to parse multipart form", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid multipart form", err))
		return
	}
	h.logger.Info("Multipart form parsed successfully")

	// Get form values
	notebookID := c.PostForm("notebook_id")
	name := c.PostForm("name")
	description := c.PostForm("description")
	tagsStr := c.PostForm("tags")
	
	h.logger.Info("Form values extracted", 
		zap.String("notebook_id", notebookID),
		zap.String("name", name),
		zap.String("description", description),
		zap.String("tags", tagsStr))
	
	if notebookID == "" {
		h.logger.Error("Validation failed: notebook_id is required")
		c.JSON(http.StatusBadRequest, errors.Validation("notebook_id is required", nil))
		return
	}

	var tags []string
	if tagsStr != "" {
		tags = strings.Split(tagsStr, ",")
		// Trim whitespace from tags
		for i, tag := range tags {
			tags[i] = strings.TrimSpace(tag)
		}
	}

	// Get uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		h.logger.Error("Failed to get uploaded file", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("File is required", err))
		return
	}
	defer file.Close()

	// Use filename as name if not provided
	if name == "" {
		name = header.Filename
	}

	// Read file data
	fileData, err := io.ReadAll(file)
	if err != nil {
		h.logger.Error("Failed to read file data", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to read file"))
		return
	}

	// Check file size (100MB limit)
	const maxFileSize = 100 << 20 // 100MB
	if len(fileData) > maxFileSize {
		c.JSON(http.StatusRequestEntityTooLarge, errors.Validation("File too large (max 100MB)", nil))
		return
	}

	// Create upload request
	req := models.DocumentUploadRequest{
		DocumentCreateRequest: models.DocumentCreateRequest{
			Name:        name,
			Description: description,
			NotebookID:  notebookID,
			Tags:        tags,
		},
		FileData: fileData,
	}
	
	// Create file info with proper MIME type from multipart form
	fileInfo := models.FileInfo{
		OriginalName: header.Filename,
		MimeType:     header.Header.Get("Content-Type"),
		SizeBytes:    int64(len(fileData)),
		Checksum:     "", // TODO: Calculate checksum if needed
	}

	// Validate request
	h.logger.Info("About to validate request struct", zap.Any("request", req.DocumentCreateRequest))
	if err := validateStruct(&req.DocumentCreateRequest); err != nil {
		h.logger.Error("Struct validation failed", zap.Error(err), zap.Any("request", req.DocumentCreateRequest))
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}
	h.logger.Info("Request struct validation passed")

	// Get space context
	h.logger.Info("Getting space context...")
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		h.logger.Error("Failed to get space context", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}
	h.logger.Info("Space context retrieved", 
		zap.String("space_type", string(spaceContext.SpaceType)),
		zap.String("space_id", spaceContext.SpaceID),
		zap.String("tenant_id", spaceContext.TenantID))

	h.logger.Info("Starting document upload", 
		zap.String("user_id", userID),
		zap.String("notebook_id", req.NotebookID),
		zap.String("filename", req.Name))
	
	h.logger.Info("About to call documentService.UploadDocument")
	document, err := h.documentService.UploadDocument(c.Request.Context(), req, userID, spaceContext, fileInfo)
	h.logger.Info("documentService.UploadDocument call completed", zap.Bool("has_error", err != nil))
	if err != nil {
		h.logger.Error("Failed to upload document", zap.Error(err))
		handleServiceError(c, err)
		return
	}
	
	h.logger.Info("Document upload completed successfully", 
		zap.String("document_id", document.ID))

	c.JSON(http.StatusCreated, document.ToResponse())
}

// UploadDocumentBase64 uploads a document using base64 encoded content
// @Summary Upload document (base64)
// @Description Upload a new document using base64 encoded content
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param document body models.DocumentBase64UploadRequest true "Document upload data"
// @Success 201 {object} models.DocumentResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 413 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/upload-base64 [post]
func (h *DocumentHandler) UploadDocumentBase64(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	var req models.DocumentBase64UploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Failed to bind JSON request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request format", err))
		return
	}

	h.logger.Info("Base64 upload request received",
		zap.String("user_id", userID),
		zap.String("notebook_id", req.NotebookID),
		zap.String("file_name", req.FileName),
		zap.String("mime_type", req.MimeType),
		zap.Int("base64_length", len(req.FileContent)))

	// Decode base64 content
	fileData, err := base64.StdEncoding.DecodeString(req.FileContent)
	if err != nil {
		h.logger.Error("Failed to decode base64 content", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid base64 content", err))
		return
	}

	// Check file size (100MB limit)
	const maxFileSize = 100 << 20 // 100MB
	if len(fileData) > maxFileSize {
		c.JSON(http.StatusRequestEntityTooLarge, errors.Validation("File too large (max 100MB)", nil))
		return
	}

	// Use filename as name if not provided
	name := req.Name
	if name == "" {
		name = req.FileName
	}

	// Create upload request with proper file info
	uploadReq := models.DocumentUploadRequest{
		DocumentCreateRequest: models.DocumentCreateRequest{
			Name:        name,
			Description: req.Description,
			NotebookID:  req.NotebookID,
			Tags:        req.Tags,
		},
		FileData:           fileData,
		ComplianceSettings: req.ComplianceSettings,
	}

	// Log compliance settings if provided
	if req.ComplianceSettings != nil {
		h.logger.Info("Document upload includes compliance settings",
			zap.Any("compliance_settings", req.ComplianceSettings))
	}

	// Create file info with proper MIME type from frontend
	fileInfo := models.FileInfo{
		OriginalName: req.FileName,
		MimeType:     req.MimeType,
		SizeBytes:    int64(len(fileData)),
		Checksum:     "", // TODO: Calculate checksum if needed
	}

	// Validate request
	if err := validateStruct(&uploadReq.DocumentCreateRequest); err != nil {
		h.logger.Error("Validation failed", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		h.logger.Error("Failed to get space context", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	document, err := h.documentService.UploadDocument(c.Request.Context(), uploadReq, userID, spaceContext, fileInfo)
	if err != nil {
		h.logger.Error("Failed to upload document", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Document uploaded successfully",
		zap.String("document_id", document.ID),
		zap.String("file_name", req.FileName),
		zap.Int64("size_bytes", int64(len(fileData))))

	c.JSON(http.StatusCreated, document.ToResponse())
}

// GetDocument gets document by ID
// @Summary Get document by ID
// @Description Get document details by ID
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Document ID"
// @Success 200 {object} models.DocumentResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/{id} [get]
func (h *DocumentHandler) GetDocument(c *gin.Context) {
	documentID := c.Param("id")
	if documentID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Document ID is required", nil))
		return
	}

	userID := getUserID(c)
	
	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}
	
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get document", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, document.ToResponse())
}

// GetDocumentStatus gets the processing status of a document
// @Summary Get document processing status
// @Description Get real-time processing status and progress for a document
// @Tags documents
// @Produce json
// @Security Bearer
// @Param id path string true "Document ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/{id}/status [get]
func (h *DocumentHandler) GetDocumentStatus(c *gin.Context) {
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

	// Get the document first to verify access
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get document for status check", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Create status response
	status := map[string]interface{}{
		"document_id":         document.ID,
		"status":             document.Status,
		"processing_job_id":  document.ProcessingJobID,
		"processed_at":       document.ProcessedAt,
		"chunk_count":        document.ChunkCount,
		"processing_time":    document.ProcessingTime,
		"confidence_score":   document.ConfidenceScore,
		"chunking_strategy":  document.ChunkingStrategy,
		"average_chunk_size": document.AverageChunkSize,
		"chunk_quality_score": document.ChunkQualityScore,
		"progress":           calculateProgress(document.Status),
		"last_updated":       document.UpdatedAt,
	}

	// Add processing details if available
	if document.ProcessingResult != nil {
		status["processing_result"] = document.ProcessingResult
	}

	c.JSON(http.StatusOK, status)
}

// calculateProgress returns a progress percentage based on document status
func calculateProgress(status string) float64 {
	switch status {
	case "uploading":
		return 10.0
	case "processing":
		return 50.0
	case "processed":
		return 100.0
	case "failed":
		return 0.0
	case "archived":
		return 100.0
	case "deleted":
		return 100.0
	default:
		return 0.0
	}
}

// UpdateDocument updates a document
// @Summary Update document
// @Description Update document metadata
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Document ID"
// @Param document body models.DocumentUpdateRequest true "Document update data"
// @Success 200 {object} models.DocumentResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/{id} [put]
func (h *DocumentHandler) UpdateDocument(c *gin.Context) {
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

	var req models.DocumentUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	document, err := h.documentService.UpdateDocument(c.Request.Context(), documentID, req, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to update document", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, document.ToResponse())
}

// DeleteDocument deletes a document
// @Summary Delete document
// @Description Delete a document (soft delete)
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Document ID"
// @Success 204
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/{id} [delete]
func (h *DocumentHandler) DeleteDocument(c *gin.Context) {
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

	err = h.documentService.DeleteDocument(c.Request.Context(), documentID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to delete document", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// ReprocessDocument reprocesses a document to extract text again
// @Summary Reprocess document
// @Description Re-run text extraction and processing for a document
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Document ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/{id}/reprocess [post]
func (h *DocumentHandler) ReprocessDocument(c *gin.Context) {
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

	// Get the document first to verify access and get details
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get document for reprocessing", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Submit reprocessing job
	job, err := h.documentService.ReprocessDocument(c.Request.Context(), document, spaceContext)
	if err != nil {
		h.logger.Error("Failed to submit document reprocessing job", 
			zap.String("document_id", documentID), 
			zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Document reprocessing job submitted", 
		zap.String("document_id", documentID),
		zap.String("job_id", job.ID),
		zap.String("user_id", userID))

	c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Document reprocessing started",
		"document_id": documentID,
		"job_id": job.ID,
		"status": "processing",
	})
}

// ListDocumentsByNotebook lists documents in a notebook
// @Summary List documents by notebook
// @Description List documents in a specific notebook
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Param limit query int false "Results limit (max 100)" default(20)
// @Param offset query int false "Results offset" default(0)
// @Success 200 {object} models.DocumentListResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id}/documents [get]
func (h *DocumentHandler) ListDocumentsByNotebook(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID is required", nil))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Parse query parameters
	limit := 20
	offset := 0

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

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	response, err := h.documentService.ListDocumentsByNotebook(c.Request.Context(), notebookID, userID, spaceContext, limit, offset)
	if err != nil {
		h.logger.Error("Failed to list documents", zap.String("notebook_id", notebookID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// SearchDocuments searches documents
// @Summary Search documents
// @Description Search documents by query, notebook, owner, etc.
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param query query string false "Search query"
// @Param notebook_id query string false "Notebook ID filter"
// @Param owner_id query string false "Owner ID filter"
// @Param type query string false "Document type filter"
// @Param status query string false "Status filter"
// @Param mime_type query string false "MIME type filter"
// @Param tags query []string false "Tags filter"
// @Param limit query int false "Results limit (max 100)" default(20)
// @Param offset query int false "Results offset" default(0)
// @Success 200 {object} models.DocumentListResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/search [get]
func (h *DocumentHandler) SearchDocuments(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	var req models.DocumentSearchRequest

	// Parse query parameters
	req.Query = c.Query("query")
	req.NotebookID = c.Query("notebook_id")
	req.OwnerID = c.Query("owner_id")
	req.Type = c.Query("type")
	req.Status = c.Query("status")
	req.MimeType = c.Query("mime_type")
	req.Tags = c.QueryArray("tags")

	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			req.Limit = limit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			req.Offset = offset
		}
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	response, err := h.documentService.SearchDocuments(c.Request.Context(), req, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to search documents", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// DownloadDocument downloads document content
// @Summary Download document
// @Description Download document file content
// @Tags documents
// @Accept json
// @Produce application/octet-stream
// @Security Bearer
// @Param id path string true "Document ID"
// @Success 200 {file} binary
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/{id}/download [get]
func (h *DocumentHandler) DownloadDocument(c *gin.Context) {
	documentID := c.Param("id")
	if documentID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Document ID is required", nil))
		return
	}

	userID := getUserID(c)
	
	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}
	
	fileData, document, err := h.documentService.DownloadDocumentFile(c.Request.Context(), documentID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to download document file", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Set appropriate headers for file download
	c.Header("Content-Disposition", "attachment; filename=\""+document.OriginalName+"\"")
	c.Header("Content-Type", document.MimeType)
	c.Header("Content-Length", fmt.Sprintf("%d", len(fileData)))
	
	// Stream the file data
	c.Data(http.StatusOK, document.MimeType, fileData)
}

// GetDocumentURL gets a presigned URL for document access
// @Summary Get document URL
// @Description Get a presigned URL for direct document access
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Document ID"
// @Param expires query int false "URL expiration in seconds" default(3600)
// @Success 200 {object} map[string]string
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/{id}/url [get]
func (h *DocumentHandler) GetDocumentURL(c *gin.Context) {
	documentID := c.Param("id")
	if documentID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Document ID is required", nil))
		return
	}

	userID := getUserID(c)
	
	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}
	
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get document", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Parse expiration parameter
	expires := 3600 // Default 1 hour
	if expiresStr := c.Query("expires"); expiresStr != "" {
		if e, err := strconv.Atoi(expiresStr); err == nil && e > 0 && e <= 86400 { // Max 24 hours
			expires = e
		}
	}

	// TODO: Implement presigned URL generation from storage service
	// This would typically involve calling the storage service to generate a presigned URL

	c.JSON(http.StatusNotImplemented, gin.H{
		"message":     "Presigned URL generation not yet implemented",
		"document_id": document.ID,
		"expires":     expires,
	})
}

// RefreshProcessingResults refreshes processing results from AudiModal
// @Summary Refresh processing results
// @Description Check AudiModal for updated processing results and update documents accordingly
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/refresh-processing [post]
func (h *DocumentHandler) RefreshProcessingResults(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User ID is required"))
		return
	}

	h.logger.Info("Refreshing processing results from AudiModal", zap.String("user_id", userID))

	err := h.documentService.RefreshProcessingResults(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to refresh processing results", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Processing results refreshed successfully",
	})
}

// AudiModalProcessingWebhook handles webhook notifications from AudiModal when processing completes
// @Summary AudiModal processing webhook
// @Description Webhook endpoint for AudiModal to notify when document processing is complete
// @Tags webhooks
// @Accept json
// @Produce json
// @Param payload body object true "Webhook payload from AudiModal"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /webhooks/audimodal/processing-complete [post]
func (h *DocumentHandler) AudiModalProcessingWebhook(c *gin.Context) {
	var payload struct {
		FileID      string `json:"file_id" binding:"required"`
		TenantID    string `json:"tenant_id" binding:"required"`
		Status      string `json:"status" binding:"required"`
		Event       string `json:"event" binding:"required"`
		ProcessedAt string `json:"processed_at,omitempty"`
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		h.logger.Error("Invalid webhook payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	h.logger.Info("Received AudiModal webhook", 
		zap.String("file_id", payload.FileID),
		zap.String("tenant_id", payload.TenantID),
		zap.String("status", payload.Status),
		zap.String("event", payload.Event))

	// Only process completion events
	if payload.Event != "processing_complete" || payload.Status != "processed" {
		h.logger.Info("Ignoring non-completion webhook", 
			zap.String("event", payload.Event),
			zap.String("status", payload.Status))
		c.JSON(http.StatusOK, gin.H{"message": "Webhook received but not processed"})
		return
	}

	// Trigger refresh of processing results
	err := h.documentService.RefreshProcessingResults(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to refresh processing results from webhook", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refresh processing results"})
		return
	}

	h.logger.Info("Successfully processed AudiModal webhook",
		zap.String("file_id", payload.FileID))

	c.JSON(http.StatusOK, gin.H{
		"message": "Webhook processed successfully",
		"file_id": payload.FileID,
	})
}

// GetDocumentAnalysis retrieves ML analysis summary for a document from AudiModal
// @Summary Get document ML analysis
// @Description Get ML analysis summary (entities, sentiment, topics) for a document
// @Tags documents
// @Produce json
// @Security Bearer
// @Param id path string true "Document ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/documents/{id}/analysis [get]
func (h *DocumentHandler) GetDocumentAnalysis(c *gin.Context) {
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

	// Verify document exists and user has access
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get document for analysis", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Check if audiModal service is available
	if h.audiModalService == nil {
		h.logger.Warn("AudiModal service not available for ML analysis")
		c.JSON(http.StatusServiceUnavailable, errors.ExternalService("ML analysis service not available", nil))
		return
	}

	// Get audimodal file ID - try multiple sources in order of preference:
	// 1. processing_result["audimodal_file_id"] (synced via Kafka after processing)
	// 2. processing_job_id (set during initial upload to audimodal)
	// 3. document ID (fallback - won't work but gives clear error)
	fileID := documentID

	// Debug: log what we have from the document
	h.logger.Debug("Document processing info",
		zap.String("document_id", documentID),
		zap.String("processing_job_id", document.ProcessingJobID),
		zap.Bool("has_processing_result", document.ProcessingResult != nil),
		zap.Int("processing_result_len", len(document.ProcessingResult)))

	if document.ProcessingResult != nil {
		if audimodalFileID, ok := document.ProcessingResult["audimodal_file_id"].(string); ok && audimodalFileID != "" {
			fileID = audimodalFileID
			h.logger.Debug("Using audimodal_file_id from processing result",
				zap.String("document_id", documentID),
				zap.String("audimodal_file_id", fileID))
		} else {
			h.logger.Debug("audimodal_file_id not found in processing_result",
				zap.String("document_id", documentID),
				zap.Any("processing_result_keys", getMapKeys(document.ProcessingResult)))
		}
	}
	// If not found in processing_result, try processing_job_id (set during upload)
	if fileID == documentID && document.ProcessingJobID != "" {
		fileID = document.ProcessingJobID
		h.logger.Debug("Using processing_job_id as audimodal file ID",
			zap.String("document_id", documentID),
			zap.String("audimodal_file_id", fileID))
	}

	h.logger.Info("Fetching ML analysis for document",
		zap.String("document_id", documentID),
		zap.String("file_id", fileID),
		zap.String("tenant_id", spaceContext.TenantID))

	// Fetch ML analysis summary from AudiModal
	analysis, err := h.audiModalService.GetMLAnalysisSummary(c.Request.Context(), spaceContext.TenantID, fileID)
	if err != nil {
		h.logger.Error("Failed to get ML analysis from AudiModal",
			zap.String("document_id", documentID),
			zap.String("file_id", fileID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to retrieve ML analysis"))
		return
	}

	h.logger.Info("Successfully retrieved ML analysis",
		zap.String("document_id", documentID),
		zap.Int("total_chunks", analysis.TotalChunks),
		zap.Float64("avg_confidence", analysis.AvgConfidence))

	c.JSON(http.StatusOK, gin.H{
		"document_id": documentID,
		"analysis":    analysis,
	})
}

// getMapKeys returns the keys of a map for debugging purposes
func getMapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// GetDocumentExtractedText fetches the extracted text content from audimodal
// @Summary Get extracted text for a document
// @Description Fetches the extracted text content from audimodal's processed chunks
// @Tags documents
// @Accept json
// @Produce json
// @Param id path string true "Document ID"
// @Success 200 {object} map[string]interface{} "Extracted text"
// @Failure 404 {object} errors.Error "Document not found"
// @Failure 500 {object} errors.Error "Internal server error"
// @Router /documents/{id}/text [get]
func (h *DocumentHandler) GetDocumentExtractedText(c *gin.Context) {
	documentID := c.Param("id")

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		h.logger.Error("Failed to get space context", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get document to verify access and get audimodal file ID
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID.(string), spaceContext)
	if err != nil {
		h.logger.Error("Failed to get document",
			zap.String("document_id", documentID),
			zap.Error(err))
		c.JSON(http.StatusNotFound, errors.NotFound("Document not found"))
		return
	}

	// Check if audimodal service is available
	if h.audiModalService == nil {
		h.logger.Warn("AudiModal service not configured")
		c.JSON(http.StatusServiceUnavailable, errors.ExternalService("Text extraction service not available", nil))
		return
	}

	// Get audimodal file ID from processing_result or processing_job_id
	fileID := documentID
	if document.ProcessingResult != nil {
		if audimodalFileID, ok := document.ProcessingResult["audimodal_file_id"].(string); ok && audimodalFileID != "" {
			fileID = audimodalFileID
		}
	}
	if fileID == documentID && document.ProcessingJobID != "" {
		fileID = document.ProcessingJobID
	}

	h.logger.Info("Fetching extracted text for document",
		zap.String("document_id", documentID),
		zap.String("file_id", fileID),
		zap.String("tenant_id", spaceContext.TenantID))

	// Fetch extracted text from AudiModal
	extractedText, err := h.audiModalService.GetFileContent(c.Request.Context(), spaceContext.TenantID, fileID)
	if err != nil {
		h.logger.Error("Failed to get extracted text from AudiModal",
			zap.String("document_id", documentID),
			zap.String("file_id", fileID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to retrieve extracted text"))
		return
	}

	h.logger.Info("Successfully retrieved extracted text",
		zap.String("document_id", documentID),
		zap.Int("text_length", len(extractedText)))

	c.JSON(http.StatusOK, gin.H{
		"document_id":    documentID,
		"extracted_text": extractedText,
		"text_length":    len(extractedText),
	})
}

// Helper functions for debugging content truncation
func countNulBytes(data []byte) int {
	count := 0
	for _, b := range data {
		if b == 0 {
			count++
		}
	}
	return count
}

func safeString(data []byte, maxLen int) string {
	if len(data) < maxLen {
		maxLen = len(data)
	}
	// Replace non-printable characters for safe logging
	result := make([]byte, maxLen)
	for i := 0; i < maxLen; i++ {
		if data[i] >= 32 && data[i] < 127 {
			result[i] = data[i]
		} else {
			result[i] = '.'
		}
	}
	return string(result)
}

func safeStringEnd(data []byte, maxLen int) string {
	if len(data) < maxLen {
		return safeString(data, len(data))
	}
	return safeString(data[len(data)-maxLen:], maxLen)
}
