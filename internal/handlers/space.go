package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// SpaceHandler handles space-related HTTP requests
type SpaceHandler struct {
	spaceService        *services.SpaceContextService
	userService         *services.UserService
	organizationService *services.OrganizationService
	logger              *logger.Logger
}

// NewSpaceHandler creates a new space handler
func NewSpaceHandler(spaceService *services.SpaceContextService, userService *services.UserService, organizationService *services.OrganizationService, log *logger.Logger) *SpaceHandler {
	return &SpaceHandler{
		spaceService:        spaceService,
		userService:         userService,
		organizationService: organizationService,
		logger:              log.WithService("space_handler"),
	}
}

// CreateSpace creates a new space
// @Summary Create space
// @Description Create a new space (organization space)
// @Tags spaces
// @Accept json
// @Produce json
// @Security Bearer
// @Param space body models.SpaceCreateRequest true "Space data"
// @Success 201 {object} models.SpaceResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/spaces [post]
func (h *SpaceHandler) CreateSpace(c *gin.Context) {
	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.SpaceCreateRequest
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

	// For now, only support organization spaces via this endpoint
	// Personal spaces are auto-created
	if req.OrganizationID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Organization ID is required for space creation"))
		return
	}

	// TODO: Implement organization space creation
	// This would involve:
	// 1. Validate user has permission to create spaces in the organization
	// 2. Create tenant in AudiModal for the space
	// 3. Create space record in database
	// 4. Set up initial permissions

	h.logger.Info("Space creation requested", 
		zap.String("user_id", userID),
		zap.String("org_id", req.OrganizationID),
		zap.String("space_name", req.Name))

	c.JSON(http.StatusNotImplemented, errors.BadRequest("Organization space creation not yet implemented"))
}

// GetSpaces gets spaces available to current user
// @Summary Get user spaces
// @Description Get all spaces accessible to the current user
// @Tags spaces
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {object} models.SpaceListResponse
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/spaces [get]
func (h *SpaceHandler) GetSpaces(c *gin.Context) {
	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	response, err := h.spaceService.GetUserSpaces(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get user spaces", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetSpace gets space by ID
// @Summary Get space by ID
// @Description Get space details by ID
// @Tags spaces
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Space ID"
// @Success 200 {object} models.SpaceInfo
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/spaces/{id} [get]
func (h *SpaceHandler) GetSpace(c *gin.Context) {
	spaceID := c.Param("id")
	if spaceID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Space ID is required", nil))
		return
	}

	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// TODO: Implement space details retrieval
	// This would involve:
	// 1. Determine space type (personal/organization)
	// 2. Validate user has access to the space
	// 3. Return space details with permissions

	h.logger.Info("Space details requested", 
		zap.String("user_id", userID),
		zap.String("space_id", spaceID))

	c.JSON(http.StatusNotImplemented, errors.BadRequest("Space details retrieval not yet implemented"))
}

// UpdateSpace updates a space
// @Summary Update space
// @Description Update space details
// @Tags spaces
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Space ID"
// @Param space body models.SpaceUpdateRequest true "Space update data"
// @Success 200 {object} models.SpaceResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/spaces/{id} [put]
func (h *SpaceHandler) UpdateSpace(c *gin.Context) {
	spaceID := c.Param("id")
	if spaceID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Space ID is required", nil))
		return
	}

	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.SpaceUpdateRequest
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

	// TODO: Implement space updates
	// This would involve:
	// 1. Validate user has permission to update the space
	// 2. Update space metadata
	// 3. Return updated space details

	h.logger.Info("Space update requested", 
		zap.String("user_id", userID),
		zap.String("space_id", spaceID))

	c.JSON(http.StatusNotImplemented, errors.BadRequest("Space updates not yet implemented"))
}

// DeleteSpace deletes a space
// @Summary Delete space
// @Description Delete a space (soft delete)
// @Tags spaces
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Space ID"
// @Success 204
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/spaces/{id} [delete]
func (h *SpaceHandler) DeleteSpace(c *gin.Context) {
	spaceID := c.Param("id")
	if spaceID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Space ID is required", nil))
		return
	}

	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// TODO: Implement space deletion
	// This would involve:
	// 1. Validate user has permission to delete the space
	// 2. Check if space can be deleted (no active notebooks, etc.)
	// 3. Soft delete the space
	// 4. Clean up associated resources

	h.logger.Info("Space deletion requested", 
		zap.String("user_id", userID),
		zap.String("space_id", spaceID))

	c.JSON(http.StatusNotImplemented, errors.BadRequest("Space deletion not yet implemented"))
}