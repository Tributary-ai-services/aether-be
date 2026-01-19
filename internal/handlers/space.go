package handlers

import (
	"fmt"
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

// =============================================================================
// Space Member Management Endpoints
// =============================================================================

// ListSpaceMembers lists all members of a space
// @Summary List space members
// @Description Get all members of a space with their roles
// @Tags spaces
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Space ID"
// @Param limit query int false "Limit" default(20)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} models.SpaceMembersListResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/spaces/{id}/members [get]
func (h *SpaceHandler) ListSpaceMembers(c *gin.Context) {
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

	// Check user has access to view this space
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

	// Parse pagination
	limit := 20
	offset := 0
	if l := c.Query("limit"); l != "" {
		if parsed, err := parseInt(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := c.Query("offset"); o != "" {
		if parsed, err := parseInt(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Get members
	response, err := h.spaceService.GetSpaceMembers(c.Request.Context(), spaceID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get space members", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// AddSpaceMember adds a member to a space
// @Summary Add space member
// @Description Invite a user to a space with a specific role
// @Tags spaces
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Space ID"
// @Param member body models.AddMemberRequest true "Member data"
// @Success 201 {object} models.SpaceMemberResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 409 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/spaces/{id}/members [post]
func (h *SpaceHandler) AddSpaceMember(c *gin.Context) {
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

	// Check user has permission to invite members (owner or admin)
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
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("You do not have permission to invite members", map[string]interface{}{
			"space_id":      spaceID,
			"current_role":  role,
			"required_role": "admin",
		}))
		return
	}

	var req models.AddMemberRequest
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

	// Add member
	err = h.spaceService.AddMember(c.Request.Context(), spaceID, req.UserID, req.Role, userID)
	if err != nil {
		h.logger.Error("Failed to add member", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Member added to space",
		zap.String("space_id", spaceID),
		zap.String("user_id", req.UserID),
		zap.String("role", req.Role),
		zap.String("invited_by", userID))

	// Return the new member info
	response := &models.SpaceMemberResponse{
		UserID:    req.UserID,
		SpaceID:   spaceID,
		Role:      req.Role,
		InvitedBy: userID,
	}

	c.JSON(http.StatusCreated, response)
}

// UpdateSpaceMember updates a member's role in a space
// @Summary Update space member
// @Description Update a member's role in a space
// @Tags spaces
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Space ID"
// @Param userId path string true "User ID"
// @Param role body models.UpdateMemberRoleRequest true "Role data"
// @Success 200 {object} models.SpaceMemberResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/spaces/{id}/members/{userId} [patch]
func (h *SpaceHandler) UpdateSpaceMember(c *gin.Context) {
	spaceID := c.Param("id")
	targetUserID := c.Param("userId")
	if spaceID == "" || targetUserID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Space ID and User ID are required", nil))
		return
	}

	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Check user has permission to update member roles (owner or admin)
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
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("You do not have permission to update member roles", map[string]interface{}{
			"space_id":      spaceID,
			"current_role":  role,
			"required_role": "admin",
		}))
		return
	}

	var req models.UpdateMemberRoleRequest
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

	// Update member role
	err = h.spaceService.UpdateMemberRole(c.Request.Context(), spaceID, targetUserID, req.Role)
	if err != nil {
		h.logger.Error("Failed to update member role", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Member role updated",
		zap.String("space_id", spaceID),
		zap.String("target_user_id", targetUserID),
		zap.String("new_role", req.Role),
		zap.String("updated_by", userID))

	// Return updated member info
	response := &models.SpaceMemberResponse{
		UserID:  targetUserID,
		SpaceID: spaceID,
		Role:    req.Role,
	}

	c.JSON(http.StatusOK, response)
}

// RemoveSpaceMember removes a member from a space
// @Summary Remove space member
// @Description Remove a user from a space
// @Tags spaces
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Space ID"
// @Param userId path string true "User ID"
// @Success 204
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/spaces/{id}/members/{userId} [delete]
func (h *SpaceHandler) RemoveSpaceMember(c *gin.Context) {
	spaceID := c.Param("id")
	targetUserID := c.Param("userId")
	if spaceID == "" || targetUserID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Space ID and User ID are required", nil))
		return
	}

	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Check user has permission to remove members (owner or admin)
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
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("You do not have permission to remove members", map[string]interface{}{
			"space_id":      spaceID,
			"current_role":  role,
			"required_role": "admin",
		}))
		return
	}

	// Remove member
	err = h.spaceService.RemoveMember(c.Request.Context(), spaceID, targetUserID)
	if err != nil {
		h.logger.Error("Failed to remove member", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Member removed from space",
		zap.String("space_id", spaceID),
		zap.String("target_user_id", targetUserID),
		zap.String("removed_by", userID))

	c.Status(http.StatusNoContent)
}

// parseInt helper function
func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}