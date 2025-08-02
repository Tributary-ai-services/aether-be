package handlers

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// DocumentHandler handles document-related HTTP requests
type DocumentHandler struct {
	documentService *services.DocumentService
	logger          *logger.Logger
}

// NewDocumentHandler creates a new document handler
func NewDocumentHandler(documentService *services.DocumentService, log *logger.Logger) *DocumentHandler {
	return &DocumentHandler{
		documentService: documentService,
		logger:          log.WithService("document_handler"),
	}
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
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Parse multipart form
	err := c.Request.ParseMultipartForm(32 << 20) // 32MB max memory
	if err != nil {
		h.logger.Error("Failed to parse multipart form", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid multipart form", err))
		return
	}

	// Get form values
	notebookID := c.PostForm("notebook_id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("notebook_id is required", nil))
		return
	}

	name := c.PostForm("name")
	description := c.PostForm("description")
	tagsStr := c.PostForm("tags")

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

	// Validate request
	if err := validateStruct(&req.DocumentCreateRequest); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	document, err := h.documentService.UploadDocument(c.Request.Context(), req, userID)
	if err != nil {
		h.logger.Error("Failed to upload document", zap.Error(err))
		handleServiceError(c, err)
		return
	}

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
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID)
	if err != nil {
		h.logger.Error("Failed to get document", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, document.ToResponse())
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

	document, err := h.documentService.UpdateDocument(c.Request.Context(), documentID, req, userID)
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

	err := h.documentService.DeleteDocument(c.Request.Context(), documentID, userID)
	if err != nil {
		h.logger.Error("Failed to delete document", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// ListDocumentsByNotebook lists documents in a notebook
// @Summary List documents by notebook
// @Description List documents in a specific notebook
// @Tags documents
// @Accept json
// @Produce json
// @Security Bearer
// @Param notebook_id path string true "Notebook ID"
// @Param limit query int false "Results limit (max 100)" default(20)
// @Param offset query int false "Results offset" default(0)
// @Success 200 {object} models.DocumentListResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{notebook_id}/documents [get]
func (h *DocumentHandler) ListDocumentsByNotebook(c *gin.Context) {
	notebookID := c.Param("notebook_id")
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

	response, err := h.documentService.ListDocumentsByNotebook(c.Request.Context(), notebookID, userID, limit, offset)
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

	response, err := h.documentService.SearchDocuments(c.Request.Context(), req, userID)
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
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID)
	if err != nil {
		h.logger.Error("Failed to get document", zap.String("document_id", documentID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// TODO: Implement actual file download from storage service
	// This would typically involve:
	// 1. Getting file URL from storage service
	// 2. Streaming file content to response
	// 3. Setting appropriate headers

	c.Header("Content-Disposition", "attachment; filename="+document.OriginalName)
	c.Header("Content-Type", document.MimeType)
	c.JSON(http.StatusNotImplemented, gin.H{"message": "Download not yet implemented"})
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
	document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID)
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
