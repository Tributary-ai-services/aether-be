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
)

// NotificationHandler handles notification-related HTTP endpoints
type NotificationHandler struct {
	service *services.NotificationService
	logger  *logger.Logger
}

// NewNotificationHandler creates a new NotificationHandler
func NewNotificationHandler(service *services.NotificationService, log *logger.Logger) *NotificationHandler {
	return &NotificationHandler{
		service: service,
		logger:  log.WithService("notification_handler"),
	}
}

// GetNotifications returns paginated notifications for the authenticated user
func (h *NotificationHandler) GetNotifications(c *gin.Context) {
	userID := c.GetString("user_id")
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Space context required"})
		return
	}
	tenantID := spaceCtx.TenantID

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	unreadOnly := c.DefaultQuery("unread_only", "false") == "true"

	notifications, total, err := h.service.GetNotifications(c.Request.Context(), userID, tenantID, limit, offset, unreadOnly)
	if err != nil {
		h.logger.Error("Failed to get notifications", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get notifications"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"notifications": notifications,
		"total":         total,
		"limit":         limit,
		"offset":        offset,
	})
}

// GetUnreadCount returns the count of unread notifications
func (h *NotificationHandler) GetUnreadCount(c *gin.Context) {
	userID := c.GetString("user_id")
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Space context required"})
		return
	}
	tenantID := spaceCtx.TenantID

	count, err := h.service.GetUnreadCount(c.Request.Context(), userID, tenantID)
	if err != nil {
		h.logger.Error("Failed to get unread count", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get unread count"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"unread_count": count})
}

// MarkAsRead marks a notification as read
func (h *NotificationHandler) MarkAsRead(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Space context required"})
		return
	}
	tenantID := spaceCtx.TenantID

	err = h.service.MarkAsRead(c.Request.Context(), id, userID, tenantID)
	if err != nil {
		h.logger.Error("Failed to mark as read", zap.Error(err), zap.String("id", id))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to mark notification as read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// MarkAllAsRead marks all notifications as read for the current user
func (h *NotificationHandler) MarkAllAsRead(c *gin.Context) {
	userID := c.GetString("user_id")
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Space context required"})
		return
	}
	tenantID := spaceCtx.TenantID

	err = h.service.MarkAllAsRead(c.Request.Context(), userID, tenantID)
	if err != nil {
		h.logger.Error("Failed to mark all as read", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to mark all as read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteNotification removes a notification
func (h *NotificationHandler) DeleteNotification(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("user_id")
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Space context required"})
		return
	}
	tenantID := spaceCtx.TenantID

	err = h.service.DeleteNotification(c.Request.Context(), id, userID, tenantID)
	if err != nil {
		h.logger.Error("Failed to delete notification", zap.Error(err), zap.String("id", id))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete notification"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// WorkflowCompleteWebhook handles POST /webhooks/workflow-complete (no auth)
func (h *NotificationHandler) WorkflowCompleteWebhook(c *gin.Context) {
	var payload models.WorkflowCompleteWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload: " + err.Error()})
		return
	}

	h.logger.Info("Received workflow complete webhook",
		zap.String("workflow_id", payload.WorkflowID),
		zap.String("status", payload.Status),
		zap.String("user_id", payload.UserID),
	)

	err := h.service.ProcessWorkflowComplete(c.Request.Context(), &payload)
	if err != nil {
		h.logger.Error("Failed to process workflow complete", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process webhook"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
