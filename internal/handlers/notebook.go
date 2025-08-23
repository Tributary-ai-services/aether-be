package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// NotebookHandler handles notebook-related HTTP requests
type NotebookHandler struct {
	notebookService *services.NotebookService
	userService     *services.UserService
	logger          *logger.Logger
}

// NewNotebookHandler creates a new notebook handler
func NewNotebookHandler(notebookService *services.NotebookService, userService *services.UserService, log *logger.Logger) *NotebookHandler {
	return &NotebookHandler{
		notebookService: notebookService,
		userService:     userService,
		logger:          log.WithService("notebook_handler"),
	}
}

// CreateNotebook creates a new notebook
// @Summary Create notebook
// @Description Create a new notebook
// @Tags notebooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param notebook body models.NotebookCreateRequest true "Notebook data"
// @Success 201 {object} models.NotebookResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks [post]
func (h *NotebookHandler) CreateNotebook(c *gin.Context) {
	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.NotebookCreateRequest
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

	notebook, err := h.notebookService.CreateNotebook(c.Request.Context(), req, userID)
	if err != nil {
		h.logger.Error("Failed to create notebook", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, notebook.ToResponse())
}

// GetNotebook gets notebook by ID
// @Summary Get notebook by ID
// @Description Get notebook details by ID
// @Tags notebooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Success 200 {object} models.NotebookResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id} [get]
func (h *NotebookHandler) GetNotebook(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID is required", nil))
		return
	}

	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	notebook, err := h.notebookService.GetNotebookByID(c.Request.Context(), notebookID, userID)
	if err != nil {
		h.logger.Error("Failed to get notebook", zap.String("notebook_id", notebookID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, notebook.ToResponse())
}

// UpdateNotebook updates a notebook
// @Summary Update notebook
// @Description Update notebook details
// @Tags notebooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Param notebook body models.NotebookUpdateRequest true "Notebook update data"
// @Success 200 {object} models.NotebookResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id} [put]
func (h *NotebookHandler) UpdateNotebook(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID is required", nil))
		return
	}

	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.NotebookUpdateRequest
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

	notebook, err := h.notebookService.UpdateNotebook(c.Request.Context(), notebookID, req, userID)
	if err != nil {
		h.logger.Error("Failed to update notebook", zap.String("notebook_id", notebookID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, notebook.ToResponse())
}

// DeleteNotebook deletes a notebook
// @Summary Delete notebook
// @Description Delete a notebook (soft delete)
// @Tags notebooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Success 204
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id} [delete]
func (h *NotebookHandler) DeleteNotebook(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID is required", nil))
		return
	}

	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	err = h.notebookService.DeleteNotebook(c.Request.Context(), notebookID, userID)
	if err != nil {
		h.logger.Error("Failed to delete notebook", zap.String("notebook_id", notebookID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// ListNotebooks lists notebooks for current user
// @Summary List notebooks
// @Description List notebooks accessible to the current user
// @Tags notebooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param limit query int false "Results limit (max 100)" default(20)
// @Param offset query int false "Results offset" default(0)
// @Success 200 {object} models.NotebookListResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks [get]
func (h *NotebookHandler) ListNotebooks(c *gin.Context) {
	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
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

	response, err := h.notebookService.ListNotebooks(c.Request.Context(), userID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to list notebooks", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// SearchNotebooks searches notebooks
// @Summary Search notebooks
// @Description Search notebooks by query, owner, visibility, etc.
// @Tags notebooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param query query string false "Search query"
// @Param owner_id query string false "Owner ID filter"
// @Param visibility query string false "Visibility filter"
// @Param status query string false "Status filter"
// @Param tags query []string false "Tags filter"
// @Param limit query int false "Results limit (max 100)" default(20)
// @Param offset query int false "Results offset" default(0)
// @Success 200 {object} models.NotebookListResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/search [get]
func (h *NotebookHandler) SearchNotebooks(c *gin.Context) {
	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.NotebookSearchRequest

	// Parse query parameters
	req.Query = c.Query("query")
	req.OwnerID = c.Query("owner_id")
	req.Visibility = c.Query("visibility")
	req.Status = c.Query("status")
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

	response, err := h.notebookService.SearchNotebooks(c.Request.Context(), req, userID)
	if err != nil {
		h.logger.Error("Failed to search notebooks", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// ShareNotebook shares a notebook with users or groups
// @Summary Share notebook
// @Description Share notebook with users or groups
// @Tags notebooks
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Notebook ID"
// @Param share body models.NotebookShareRequest true "Share data"
// @Success 200 {object} map[string]string
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/notebooks/{id}/share [post]
func (h *NotebookHandler) ShareNotebook(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID is required", nil))
		return
	}

	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.NotebookShareRequest
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

	err = h.notebookService.ShareNotebook(c.Request.Context(), notebookID, req, userID)
	if err != nil {
		h.logger.Error("Failed to share notebook", zap.String("notebook_id", notebookID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Notebook shared successfully"})
}
