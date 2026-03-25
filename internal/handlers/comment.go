package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// CommentHandler handles comment-related HTTP requests
type CommentHandler struct {
	commentService  *services.CommentService
	notebookService *services.NotebookService
	userService     *services.UserService
	logger          *logger.Logger
}

// NewCommentHandler creates a new comment handler
func NewCommentHandler(commentService *services.CommentService, notebookService *services.NotebookService, userService *services.UserService, log *logger.Logger) *CommentHandler {
	return &CommentHandler{
		commentService:  commentService,
		notebookService: notebookService,
		userService:     userService,
		logger:          log.WithService("comment_handler"),
	}
}

// CreateComment creates a new comment on a notebook
func (h *CommentHandler) CreateComment(c *gin.Context) {
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

	var req models.CreateCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Validate user has access to the notebook (supports cross-space via SHARED_WITH)
	notebook, err := h.notebookService.GetNotebookByID(c.Request.Context(), notebookID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to validate notebook access for comment", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	comment, err := h.commentService.CreateComment(c.Request.Context(), notebookID, req, userID, notebook.TenantID)
	if err != nil {
		h.logger.Error("Failed to create comment", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, comment)
}

// GetComments returns threaded comments for a notebook
func (h *CommentHandler) GetComments(c *gin.Context) {
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

	// Validate user has access to the notebook (supports cross-space via SHARED_WITH)
	notebook, err := h.notebookService.GetNotebookByID(c.Request.Context(), notebookID, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to validate notebook access for comments", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	comments, err := h.commentService.GetComments(c.Request.Context(), notebookID, notebook.TenantID)
	if err != nil {
		h.logger.Error("Failed to get comments", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, comments)
}

// UpdateComment updates a comment
func (h *CommentHandler) UpdateComment(c *gin.Context) {
	notebookID := c.Param("id")
	commentID := c.Param("commentId")
	if notebookID == "" || commentID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID and Comment ID are required", nil))
		return
	}

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	var req models.UpdateCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	comment, err := h.commentService.UpdateComment(c.Request.Context(), notebookID, commentID, req, userID)
	if err != nil {
		h.logger.Error("Failed to update comment", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, comment)
}

// DeleteComment deletes a comment
func (h *CommentHandler) DeleteComment(c *gin.Context) {
	notebookID := c.Param("id")
	commentID := c.Param("commentId")
	if notebookID == "" || commentID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID and Comment ID are required", nil))
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

	if err := h.commentService.DeleteComment(c.Request.Context(), notebookID, commentID, userID, spaceContext); err != nil {
		h.logger.Error("Failed to delete comment", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// StreamComments handles SSE for real-time comment updates
func (h *CommentHandler) StreamComments(c *gin.Context) {
	notebookID := c.Param("id")
	if notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Notebook ID is required", nil))
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Subscribe to comment events
	eventCh := h.commentService.Subscribe(notebookID)
	defer h.commentService.Unsubscribe(notebookID, eventCh)

	// Heartbeat ticker
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	clientGone := c.Request.Context().Done()

	c.Stream(func(w io.Writer) bool {
		select {
		case <-clientGone:
			return false
		case event := <-eventCh:
			data, err := json.Marshal(event)
			if err != nil {
				h.logger.Error("Failed to marshal comment event", zap.Error(err))
				return true
			}
			c.SSEvent("message", string(data))
			return true
		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			c.Writer.Flush()
			return true
		}
	})
}
