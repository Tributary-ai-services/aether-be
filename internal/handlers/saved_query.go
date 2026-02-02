package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// SavedQueryHandler handles saved query HTTP requests
type SavedQueryHandler struct {
	savedQueryService *services.SavedQueryService
	userService       *services.UserService
	teamService       *services.TeamService
	logger            *logger.Logger
}

// NewSavedQueryHandler creates a new saved query handler
func NewSavedQueryHandler(
	savedQueryService *services.SavedQueryService,
	userService *services.UserService,
	teamService *services.TeamService,
	log *logger.Logger,
) *SavedQueryHandler {
	return &SavedQueryHandler{
		savedQueryService: savedQueryService,
		userService:       userService,
		teamService:       teamService,
		logger:            log.WithService("saved_query_handler"),
	}
}

// CreateSavedQuery creates a new saved query
// @Summary Create saved query
// @Description Create a new saved query
// @Tags saved-queries
// @Accept json
// @Produce json
// @Security Bearer
// @Param query body models.SavedQueryCreateRequest true "Saved query data"
// @Success 201 {object} models.SavedQueryResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/saved-queries [post]
func (h *SavedQueryHandler) CreateSavedQuery(c *gin.Context) {
	// Get user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", map[string]any{
			"hint": "Set X-Space-ID header with your space ID",
		}))
		return
	}

	tenantID := spaceContext.TenantID
	spaceIDStr := spaceContext.SpaceID

	var req models.SavedQueryCreateRequest
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

	h.logger.Info("Creating saved query",
		zap.String("user_id", userID),
		zap.String("tenant_id", tenantID),
		zap.String("space_id", spaceIDStr),
		zap.String("name", req.Name),
	)

	query, err := h.savedQueryService.CreateSavedQuery(c.Request.Context(), userID, tenantID, spaceIDStr, req)
	if err != nil {
		h.logger.Error("Failed to create saved query",
			zap.String("user_id", userID),
			zap.Error(err),
		)
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Saved query created successfully",
		zap.String("query_id", query.ID),
		zap.String("name", query.Name),
	)

	c.JSON(http.StatusCreated, query.ToResponse())
}

// GetSavedQuery gets a saved query by ID
// @Summary Get saved query
// @Description Get a saved query by ID
// @Tags saved-queries
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Saved Query ID"
// @Success 200 {object} models.SavedQueryResponse
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/saved-queries/{id} [get]
func (h *SavedQueryHandler) GetSavedQuery(c *gin.Context) {
	queryID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	query, err := h.savedQueryService.GetSavedQuery(c.Request.Context(), queryID, spaceContext.TenantID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	// Check access
	userTeams := h.getUserTeams(c, userID)
	if !query.IsAccessibleBy(userID, spaceContext.SpaceID, userTeams) {
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("Not authorized to access this query", map[string]any{
			"query_id": queryID,
		}))
		return
	}

	c.JSON(http.StatusOK, query.ToResponse())
}

// ListSavedQueries lists saved queries with filtering
// @Summary List saved queries
// @Description List saved queries accessible to the user with optional filtering
// @Tags saved-queries
// @Accept json
// @Produce json
// @Security Bearer
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(20)
// @Param database_id query string false "Filter by database ID"
// @Param visibility query string false "Filter by visibility (private, shared, public)"
// @Param folder query string false "Filter by folder"
// @Param search query string false "Search in name and description"
// @Param sort_by query string false "Sort by field (name, created_at, updated_at, execution_count, last_executed_at)"
// @Param sort_order query string false "Sort order (asc, desc)"
// @Success 200 {object} models.SavedQueryListResponse
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/saved-queries [get]
func (h *SavedQueryHandler) ListSavedQueries(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	// Parse query parameters into search request
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	searchReq := models.SavedQuerySearchRequest{
		Search:     c.Query("search"),
		DatabaseID: c.Query("database_id"),
		Visibility: models.QueryVisibility(c.Query("visibility")),
		Folder:     c.Query("folder"),
		OwnerID:    c.Query("owner_id"),
		Page:       page,
		PageSize:   pageSize,
		SortBy:     c.Query("sort_by"),
		SortOrder:  c.Query("sort_order"),
	}

	// Get user's teams for access control
	userTeams := h.getUserTeams(c, userID)

	result, err := h.savedQueryService.ListSavedQueries(
		c.Request.Context(),
		spaceContext.TenantID,
		spaceContext.SpaceID,
		userID,
		userTeams,
		searchReq,
	)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// UpdateSavedQuery updates a saved query
// @Summary Update saved query
// @Description Update a saved query (owner only)
// @Tags saved-queries
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Saved Query ID"
// @Param query body models.SavedQueryUpdateRequest true "Update data"
// @Success 200 {object} models.SavedQueryResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/saved-queries/{id} [put]
func (h *SavedQueryHandler) UpdateSavedQuery(c *gin.Context) {
	queryID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	var req models.SavedQueryUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	query, err := h.savedQueryService.UpdateSavedQuery(c.Request.Context(), queryID, spaceContext.TenantID, userID, req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, query.ToResponse())
}

// DeleteSavedQuery deletes a saved query
// @Summary Delete saved query
// @Description Delete a saved query (owner only)
// @Tags saved-queries
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Saved Query ID"
// @Success 204 "No Content"
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/saved-queries/{id} [delete]
func (h *SavedQueryHandler) DeleteSavedQuery(c *gin.Context) {
	queryID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	if err := h.savedQueryService.DeleteSavedQuery(c.Request.Context(), queryID, spaceContext.TenantID, userID); err != nil {
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// ExecuteSavedQuery executes a saved query
// @Summary Execute saved query
// @Description Execute a saved query and return results
// @Tags saved-queries
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Saved Query ID"
// @Param request body models.SavedQueryExecuteRequest false "Execution parameters"
// @Success 200 {object} models.SavedQueryExecuteResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/saved-queries/{id}/execute [post]
func (h *SavedQueryHandler) ExecuteSavedQuery(c *gin.Context) {
	queryID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	var execReq models.SavedQueryExecuteRequest
	// Allow empty body (no parameters)
	_ = c.ShouldBindJSON(&execReq)

	h.logger.Info("Executing saved query",
		zap.String("query_id", queryID),
		zap.String("user_id", userID),
	)

	userTeams := h.getUserTeams(c, userID)

	result, err := h.savedQueryService.ExecuteSavedQuery(
		c.Request.Context(),
		queryID,
		spaceContext.TenantID,
		userID,
		spaceContext.SpaceID,
		userTeams,
		execReq,
	)
	if err != nil {
		h.logger.Error("Failed to execute saved query",
			zap.String("query_id", queryID),
			zap.Error(err),
		)
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// DuplicateSavedQuery creates a copy of a saved query
// @Summary Duplicate saved query
// @Description Create a copy of a saved query as a new private query
// @Tags saved-queries
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Saved Query ID to duplicate"
// @Param request body object{name string} true "New query name"
// @Success 201 {object} models.SavedQueryResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/saved-queries/{id}/duplicate [post]
func (h *SavedQueryHandler) DuplicateSavedQuery(c *gin.Context) {
	queryID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Name is required", err))
		return
	}

	h.logger.Info("Duplicating saved query",
		zap.String("query_id", queryID),
		zap.String("user_id", userID),
		zap.String("new_name", req.Name),
	)

	userTeams := h.getUserTeams(c, userID)

	newQuery, err := h.savedQueryService.DuplicateSavedQuery(
		c.Request.Context(),
		queryID,
		spaceContext.TenantID,
		userID,
		spaceContext.SpaceID,
		req.Name,
		userTeams,
	)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Saved query duplicated",
		zap.String("source_id", queryID),
		zap.String("new_id", newQuery.ID),
	)

	c.JSON(http.StatusCreated, newQuery.ToResponse())
}

// getUserTeams is a helper to get user's team IDs for access control
func (h *SavedQueryHandler) getUserTeams(c *gin.Context, userID string) []string {
	// Get teams from team service (passing empty org to get all teams for user)
	teams, err := h.teamService.GetTeams(c.Request.Context(), userID, "")
	if err != nil {
		h.logger.Warn("Failed to get user teams for access control",
			zap.String("user_id", userID),
			zap.Error(err),
		)
		return []string{}
	}

	teamIDs := make([]string, len(teams))
	for i, team := range teams {
		teamIDs[i] = team.ID
	}
	return teamIDs
}
