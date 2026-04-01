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

// ConversationHandler handles conversation-related HTTP requests
type ConversationHandler struct {
	conversationService *services.ConversationService
	notebookService     *services.NotebookService
	userService         *services.UserService
	logger              *logger.Logger
}

// NewConversationHandler creates a new conversation handler
func NewConversationHandler(conversationService *services.ConversationService, notebookService *services.NotebookService, userService *services.UserService, log *logger.Logger) *ConversationHandler {
	return &ConversationHandler{
		conversationService: conversationService,
		notebookService:     notebookService,
		userService:         userService,
		logger:              log.WithService("conversation_handler"),
	}
}

// CreateConversation creates a new conversation for a notebook
func (h *ConversationHandler) CreateConversation(c *gin.Context) {
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

	var req models.CreateConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body for creating conversation without name
		req = models.CreateConversationRequest{}
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Validate user has access to the notebook
	_, err = h.notebookService.GetNotebookByID(c.Request.Context(), notebookID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to validate notebook access for conversation", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	conversation, err := h.conversationService.CreateConversation(c.Request.Context(), notebookID, req, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to create conversation", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, conversation)
}

// ListConversations returns conversations for a notebook
func (h *ConversationHandler) ListConversations(c *gin.Context) {
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

	// Validate user has access to the notebook
	notebook, err := h.notebookService.GetNotebookByID(c.Request.Context(), notebookID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to validate notebook access for conversations", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	conversations, err := h.conversationService.ListConversations(c.Request.Context(), notebookID, notebook.TenantID)
	if err != nil {
		h.logger.Error("Failed to list conversations", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, conversations)
}

// GetConversation returns a conversation with details
func (h *ConversationHandler) GetConversation(c *gin.Context) {
	notebookID := c.Param("id")
	conversationID := c.Param("convId")
	if notebookID == "" || conversationID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID and Conversation ID are required", nil))
		return
	}

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	conversation, err := h.conversationService.GetConversation(c.Request.Context(), conversationID, userID)
	if err != nil {
		h.logger.Error("Failed to get conversation", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, conversation)
}

// UpdateConversation renames a conversation
func (h *ConversationHandler) UpdateConversation(c *gin.Context) {
	notebookID := c.Param("id")
	conversationID := c.Param("convId")
	if notebookID == "" || conversationID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID and Conversation ID are required", nil))
		return
	}

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	var req models.UpdateConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	conversation, err := h.conversationService.UpdateConversation(c.Request.Context(), conversationID, req, userID)
	if err != nil {
		h.logger.Error("Failed to update conversation", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, conversation)
}

// DeleteConversation deletes a conversation and all its messages
func (h *ConversationHandler) DeleteConversation(c *gin.Context) {
	notebookID := c.Param("id")
	conversationID := c.Param("convId")
	if notebookID == "" || conversationID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID and Conversation ID are required", nil))
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

	if err := h.conversationService.DeleteConversation(c.Request.Context(), conversationID, userID, spaceContext); err != nil {
		h.logger.Error("Failed to delete conversation", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetMessages returns paginated messages for a conversation
func (h *ConversationHandler) GetMessages(c *gin.Context) {
	notebookID := c.Param("id")
	conversationID := c.Param("convId")
	if notebookID == "" || conversationID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID and Conversation ID are required", nil))
		return
	}

	_, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	limit := 50
	offset := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 && val <= 200 {
			limit = val
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if val, err := strconv.Atoi(offsetStr); err == nil && val >= 0 {
			offset = val
		}
	}

	messages, err := h.conversationService.GetMessages(c.Request.Context(), conversationID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get messages", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, messages)
}
