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

// OrganizationHandler handles organization-related HTTP requests
type OrganizationHandler struct {
	orgService *services.OrganizationService
	logger     *logger.Logger
}

// NewOrganizationHandler creates a new organization handler
func NewOrganizationHandler(orgService *services.OrganizationService, log *logger.Logger) *OrganizationHandler {
	return &OrganizationHandler{
		orgService: orgService,
		logger:     log.WithService("organization_handler"),
	}
}

// CreateOrganization creates a new organization
// @Summary Create a new organization
// @Description Create a new organization with the provided information
// @Tags organizations
// @Accept json
// @Produce json
// @Security Bearer
// @Param org body models.OrganizationCreateRequest true "Organization creation data"
// @Success 201 {object} models.OrganizationResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 409 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/organizations [post]
func (h *OrganizationHandler) CreateOrganization(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	var req models.OrganizationCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid organization creation request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}

	org, err := h.orgService.CreateOrganization(c.Request.Context(), req, userID)
	if err != nil {
		h.logger.Error("Failed to create organization", zap.Error(err), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Organization created successfully", zap.String("org_id", org.ID), zap.String("user_id", userID))
	c.JSON(http.StatusCreated, org.ToResponse())
}

// GetOrganizations gets organizations for the current user
// @Summary Get user organizations
// @Description Get all organizations the current user is a member of
// @Tags organizations
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {array} models.OrganizationResponse
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/organizations [get]
func (h *OrganizationHandler) GetOrganizations(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	organizations, err := h.orgService.GetOrganizations(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get organizations", zap.Error(err), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	// Convert to responses
	responses := make([]*models.OrganizationResponse, len(organizations))
	for i, org := range organizations {
		responses[i] = org.ToResponse()
	}

	c.JSON(http.StatusOK, responses)
}

// GetOrganization gets a specific organization by ID
// @Summary Get organization by ID
// @Description Get a specific organization by its ID
// @Tags organizations
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Organization ID"
// @Success 200 {object} models.OrganizationResponse
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/organizations/{id} [get]
func (h *OrganizationHandler) GetOrganization(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	orgID := c.Param("id")
	if orgID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Organization ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	org, err := h.orgService.GetOrganization(c.Request.Context(), orgID, userID)
	if err != nil {
		h.logger.Error("Failed to get organization", zap.Error(err), zap.String("org_id", orgID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, org.ToResponse())
}

// UpdateOrganization updates an organization
// @Summary Update organization
// @Description Update organization information
// @Tags organizations
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Organization ID"
// @Param org body models.OrganizationUpdateRequest true "Organization update data"
// @Success 200 {object} models.OrganizationResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 409 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/organizations/{id} [put]
func (h *OrganizationHandler) UpdateOrganization(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	orgID := c.Param("id")
	if orgID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Organization ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	var req models.OrganizationUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid organization update request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}

	org, err := h.orgService.UpdateOrganization(c.Request.Context(), orgID, req, userID)
	if err != nil {
		h.logger.Error("Failed to update organization", zap.Error(err), zap.String("org_id", orgID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Organization updated successfully", zap.String("org_id", orgID), zap.String("user_id", userID))
	c.JSON(http.StatusOK, org.ToResponse())
}

// DeleteOrganization deletes an organization
// @Summary Delete organization
// @Description Delete an organization (only organization owners can delete organizations)
// @Tags organizations
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Organization ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/organizations/{id} [delete]
func (h *OrganizationHandler) DeleteOrganization(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	orgID := c.Param("id")
	if orgID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Organization ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	err := h.orgService.DeleteOrganization(c.Request.Context(), orgID, userID)
	if err != nil {
		h.logger.Error("Failed to delete organization", zap.Error(err), zap.String("org_id", orgID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Organization deleted successfully", zap.String("org_id", orgID), zap.String("user_id", userID))
	c.Status(http.StatusNoContent)
}

// GetOrganizationMembers gets all members of an organization
// @Summary Get organization members
// @Description Get all members of a specific organization
// @Tags organizations
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Organization ID"
// @Success 200 {array} models.OrganizationMemberResponse
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/organizations/{id}/members [get]
func (h *OrganizationHandler) GetOrganizationMembers(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	orgID := c.Param("id")
	if orgID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Organization ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	members, err := h.orgService.GetOrganizationMembers(c.Request.Context(), orgID, userID)
	if err != nil {
		h.logger.Error("Failed to get organization members", zap.Error(err), zap.String("org_id", orgID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	// Convert to responses
	responses := make([]*models.OrganizationMemberResponse, len(members))
	for i, member := range members {
		responses[i] = member.ToMemberResponse()
	}

	c.JSON(http.StatusOK, responses)
}

// InviteOrganizationMember invites a new member to the organization
// @Summary Invite organization member
// @Description Invite a new member to the organization by email
// @Tags organizations
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Organization ID"
// @Param invite body models.OrganizationInviteRequest true "Member invitation data"
// @Success 201 {object} models.OrganizationMemberResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 409 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/organizations/{id}/members [post]
func (h *OrganizationHandler) InviteOrganizationMember(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	orgID := c.Param("id")
	if orgID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Organization ID is required", map[string]interface{}{
			"param": "id",
		}))
		return
	}

	var req models.OrganizationInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid member invitation request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}

	member, err := h.orgService.InviteOrganizationMember(c.Request.Context(), orgID, req, userID)
	if err != nil {
		h.logger.Error("Failed to invite organization member", zap.Error(err), 
			zap.String("org_id", orgID), zap.String("user_id", userID), zap.String("email", req.Email))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Organization member invited successfully", 
		zap.String("org_id", orgID), zap.String("invited_user_id", member.UserID), zap.String("invited_by", userID))
	c.JSON(http.StatusCreated, member.ToMemberResponse())
}

// UpdateOrganizationMemberRole updates an organization member's role
// @Summary Update member role
// @Description Update an organization member's role, title, and department
// @Tags organizations
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Organization ID"
// @Param user_id path string true "User ID"
// @Param role body models.OrganizationMemberRoleUpdateRequest true "Role update data"
// @Success 204
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/organizations/{id}/members/{user_id} [put]
func (h *OrganizationHandler) UpdateOrganizationMemberRole(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	orgID := c.Param("id")
	targetUserID := c.Param("user_id")
	if orgID == "" || targetUserID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Organization ID and User ID are required", map[string]interface{}{
			"org_id":  orgID,
			"user_id": targetUserID,
		}))
		return
	}

	var req models.OrganizationMemberRoleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid role update request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid request data", map[string]interface{}{
			"error": err.Error(),
		}))
		return
	}

	err := h.orgService.UpdateOrganizationMemberRole(c.Request.Context(), orgID, targetUserID, req, userID)
	if err != nil {
		h.logger.Error("Failed to update organization member role", zap.Error(err), 
			zap.String("org_id", orgID), zap.String("target_user_id", targetUserID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Organization member role updated successfully", 
		zap.String("org_id", orgID), zap.String("target_user_id", targetUserID), zap.String("updated_by", userID))
	c.Status(http.StatusNoContent)
}

// RemoveOrganizationMember removes a member from the organization
// @Summary Remove organization member
// @Description Remove a member from the organization
// @Tags organizations
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Organization ID"
// @Param user_id path string true "User ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/organizations/{id}/members/{user_id} [delete]
func (h *OrganizationHandler) RemoveOrganizationMember(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	orgID := c.Param("id")
	targetUserID := c.Param("user_id")
	if orgID == "" || targetUserID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Organization ID and User ID are required", map[string]interface{}{
			"org_id":  orgID,
			"user_id": targetUserID,
		}))
		return
	}

	err := h.orgService.RemoveOrganizationMember(c.Request.Context(), orgID, targetUserID, userID)
	if err != nil {
		h.logger.Error("Failed to remove organization member", zap.Error(err), 
			zap.String("org_id", orgID), zap.String("target_user_id", targetUserID), zap.String("user_id", userID))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Organization member removed successfully", 
		zap.String("org_id", orgID), zap.String("target_user_id", targetUserID), zap.String("removed_by", userID))
	c.Status(http.StatusNoContent)
}