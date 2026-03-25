package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// InvitationHandler handles invitation-related HTTP requests
type InvitationHandler struct {
	invitationService *services.InvitationService
	userService       *services.UserService
	logger            *logger.Logger
}

// NewInvitationHandler creates a new invitation handler
func NewInvitationHandler(invitationService *services.InvitationService, userService *services.UserService, log *logger.Logger) *InvitationHandler {
	return &InvitationHandler{
		invitationService: invitationService,
		userService:       userService,
		logger:            log.WithService("invitation_handler"),
	}
}

// SendInvitation sends an email invitation to share a notebook
func (h *InvitationHandler) SendInvitation(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID is required", nil))
		return
	}

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	var req models.CreateInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	invitation, err := h.invitationService.CreateInvitation(c.Request.Context(), notebookID, req, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to create invitation", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, invitation)
}

// GetNotebookInvitations returns pending invitations for a notebook
func (h *InvitationHandler) GetNotebookInvitations(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID is required", nil))
		return
	}

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	invitations, err := h.invitationService.GetInvitationsForNotebook(c.Request.Context(), notebookID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get invitations", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, invitations)
}

// CancelInvitation cancels a pending invitation
func (h *InvitationHandler) CancelInvitation(c *gin.Context) {
	notebookID := c.Param("id")
	invitationID := c.Param("invitationId")
	if notebookID == "" || invitationID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID and Invitation ID are required", nil))
		return
	}

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	if err := h.invitationService.CancelInvitation(c.Request.Context(), notebookID, invitationID, userID, spaceContext); err != nil {
		h.logger.Error("Failed to cancel invitation", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Invitation cancelled"})
}

// AcceptInvitation accepts an invitation by token
func (h *InvitationHandler) AcceptInvitation(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	var req models.AcceptInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	invitation, err := h.invitationService.AcceptInvitation(c.Request.Context(), req.Token, userID)
	if err != nil {
		h.logger.Error("Failed to accept invitation", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, invitation)
}

// GetPendingInvitations returns pending invitations for the current user
func (h *InvitationHandler) GetPendingInvitations(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	invitations, err := h.invitationService.GetPendingInvitations(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get pending invitations", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, invitations)
}
