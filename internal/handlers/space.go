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
	spaceContextService *services.SpaceContextService // Context resolution (legacy)
	spaceService        *services.SpaceService        // CRUD and member management
	userService         *services.UserService
	organizationService *services.OrganizationService
	logger              *logger.Logger
}

// NewSpaceHandler creates a new space handler
func NewSpaceHandler(
	spaceContextService *services.SpaceContextService,
	spaceService *services.SpaceService,
	userService *services.UserService,
	organizationService *services.OrganizationService,
	log *logger.Logger,
) *SpaceHandler {
	return &SpaceHandler{
		spaceContextService: spaceContextService,
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

	// Use SpaceService for relationship-based queries
	spaces, err := h.spaceService.GetUserSpaces(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get user spaces", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Build response separating personal from organization spaces
	response := &models.SpaceListResponse{
		OrganizationSpaces: make([]*models.SpaceInfo, 0),
	}

	for _, space := range spaces {
		if space.SpaceType == models.SpaceTypePersonal {
			response.PersonalSpace = space
		} else {
			response.OrganizationSpaces = append(response.OrganizationSpaces, space)
		}
	}

	// Set current space to personal space if no current space is set
	if response.PersonalSpace != nil {
		response.CurrentSpace = response.PersonalSpace
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
// @Success 200 {object} models.SpaceFullResponse
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

	// Check user has access to this space
	role, err := h.spaceService.GetUserRoleInSpace(c.Request.Context(), spaceID, userID)
	if err != nil {
		h.logger.Error("Failed to check user role", zap.Error(err))
		handleServiceError(c, err)
		return
	}
	if role == "" {
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("You do not have access to this space", map[string]interface{}{
			"space_id": spaceID,
		}))
		return
	}

	// Get space details
	space, err := h.spaceService.GetSpaceByID(c.Request.Context(), spaceID)
	if err != nil {
		h.logger.Error("Failed to get space", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Convert to full response with user's role
	response := space.ToFullResponse()
	response.UserRole = role

	h.logger.Debug("Space details retrieved",
		zap.String("user_id", userID),
		zap.String("space_id", spaceID),
		zap.String("user_role", role))

	c.JSON(http.StatusOK, response)
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
// @Success 200 {object} models.SpaceFullResponse
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

	// Check user has permission to update the space (owner or admin)
	role, err := h.spaceService.GetUserRoleInSpace(c.Request.Context(), spaceID, userID)
	if err != nil {
		h.logger.Error("Failed to check user role", zap.Error(err))
		handleServiceError(c, err)
		return
	}
	if role == "" {
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("You do not have access to this space", map[string]interface{}{
			"space_id": spaceID,
		}))
		return
	}
	if !models.HasPermissionLevel(role, "admin") {
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("You do not have permission to update this space", map[string]interface{}{
			"space_id":      spaceID,
			"current_role":  role,
			"required_role": "admin",
		}))
		return
	}

	// Update the space
	space, err := h.spaceService.UpdateSpace(c.Request.Context(), spaceID, req)
	if err != nil {
		h.logger.Error("Failed to update space", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Convert to response
	response := space.ToFullResponse()
	response.UserRole = role

	h.logger.Info("Space updated successfully",
		zap.String("user_id", userID),
		zap.String("space_id", spaceID))

	c.JSON(http.StatusOK, response)
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

	// Check user has permission to delete the space (owner only)
	role, err := h.spaceService.GetUserRoleInSpace(c.Request.Context(), spaceID, userID)
	if err != nil {
		h.logger.Error("Failed to check user role", zap.Error(err))
		handleServiceError(c, err)
		return
	}
	if role == "" {
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("You do not have access to this space", map[string]interface{}{
			"space_id": spaceID,
		}))
		return
	}
	if role != "owner" {
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("Only the owner can delete a space", map[string]interface{}{
			"space_id":      spaceID,
			"current_role":  role,
			"required_role": "owner",
		}))
		return
	}

	// Get space to check if it's a personal space
	space, err := h.spaceService.GetSpaceByID(c.Request.Context(), spaceID)
	if err != nil {
		h.logger.Error("Failed to get space", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Personal spaces cannot be deleted
	if space.IsPersonal() {
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("Personal spaces cannot be deleted", map[string]interface{}{
			"space_id":   spaceID,
			"space_type": space.Type,
		}))
		return
	}

	// Soft delete the space
	err = h.spaceService.DeleteSpace(c.Request.Context(), spaceID, userID)
	if err != nil {
		h.logger.Error("Failed to delete space", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Space deleted successfully",
		zap.String("user_id", userID),
		zap.String("space_id", spaceID))

	c.Status(http.StatusNoContent)
}