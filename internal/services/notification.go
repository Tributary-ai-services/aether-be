package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// NotificationService handles CRUD operations for notifications in Neo4j
type NotificationService struct {
	neo4j  *database.Neo4jClient
	kafka  *KafkaService
	logger *logger.Logger
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(neo4j *database.Neo4jClient, kafka *KafkaService, log *logger.Logger) *NotificationService {
	return &NotificationService{
		neo4j:  neo4j,
		kafka:  kafka,
		logger: log.WithService("notification_service"),
	}
}

// CreateNotification persists a new notification in Neo4j
func (s *NotificationService) CreateNotification(ctx context.Context, req *models.CreateNotificationRequest, tenantID string) (*models.Notification, error) {
	notification := &models.Notification{
		ID:        uuid.New().String(),
		UserID:    req.UserID,
		TenantID:  tenantID,
		Type:      req.Type,
		Title:     req.Title,
		Message:   req.Message,
		Source:    req.Source,
		Metadata:  req.Metadata,
		Read:      false,
		CreatedAt: time.Now(),
	}

	query := `
		CREATE (n:Notification {
			id: $id,
			user_id: $user_id,
			tenant_id: $tenant_id,
			type: $type,
			title: $title,
			message: $message,
			source: $source,
			read: $read,
			created_at: datetime($created_at)
		})
		RETURN n.id AS id
	`

	params := map[string]interface{}{
		"id":         notification.ID,
		"user_id":    notification.UserID,
		"tenant_id":  notification.TenantID,
		"type":       notification.Type,
		"title":      notification.Title,
		"message":    notification.Message,
		"source":     notification.Source,
		"read":       notification.Read,
		"created_at": notification.CreatedAt.Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification: %w", err)
	}

	// Publish Kafka event if available
	if s.kafka != nil {
		event := NewNotificationEvent(EventNotificationCreated, notification.ID, notification.UserID, map[string]interface{}{
			"type":  notification.Type,
			"title": notification.Title,
		})
		if pubErr := s.kafka.PublishEvent(ctx, event); pubErr != nil {
			s.logger.Warn("Failed to publish notification event", zap.Error(pubErr))
		}
	}

	return notification, nil
}

// GetNotifications returns paginated notifications for a user
func (s *NotificationService) GetNotifications(ctx context.Context, userID, tenantID string, limit, offset int, unreadOnly bool) ([]models.Notification, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	readFilter := ""
	if unreadOnly {
		readFilter = "AND n.read = false"
	}

	// Count total
	countQuery := fmt.Sprintf(`
		MATCH (n:Notification)
		WHERE n.user_id = $user_id AND n.tenant_id = $tenant_id %s
		RETURN count(n) AS total
	`, readFilter)

	baseParams := map[string]interface{}{
		"user_id":   userID,
		"tenant_id": tenantID,
	}

	var total int
	var notifications []models.Notification

	_, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		// Get count
		countResult, err := tx.Run(ctx, countQuery, baseParams)
		if err != nil {
			return nil, err
		}
		if countResult.Next(ctx) {
			v, _ := countResult.Record().Get("total")
			if iv, ok := v.(int64); ok {
				total = int(iv)
			}
		}

		// Get notifications
		fetchQuery := fmt.Sprintf(`
			MATCH (n:Notification)
			WHERE n.user_id = $user_id AND n.tenant_id = $tenant_id %s
			RETURN n.id AS id, n.user_id AS user_id, n.tenant_id AS tenant_id,
			       n.type AS type, n.title AS title, n.message AS message,
			       n.source AS source, n.read AS read, n.created_at AS created_at
			ORDER BY n.created_at DESC
			SKIP $offset LIMIT $limit
		`, readFilter)

		fetchParams := map[string]interface{}{
			"user_id":   userID,
			"tenant_id": tenantID,
			"offset":    int64(offset),
			"limit":     int64(limit),
		}

		result, err := tx.Run(ctx, fetchQuery, fetchParams)
		if err != nil {
			return nil, err
		}

		for result.Next(ctx) {
			record := result.Record()
			n := recordToNotification(record)
			notifications = append(notifications, n)
		}

		return nil, nil
	})

	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch notifications: %w", err)
	}

	if notifications == nil {
		notifications = []models.Notification{}
	}

	return notifications, total, nil
}

// MarkAsRead marks a single notification as read
func (s *NotificationService) MarkAsRead(ctx context.Context, id, userID, tenantID string) error {
	query := `
		MATCH (n:Notification {id: $id, user_id: $user_id, tenant_id: $tenant_id})
		SET n.read = true
		RETURN n.id AS id
	`
	result, err := s.neo4j.ExecuteQuery(ctx, query, map[string]interface{}{
		"id":        id,
		"user_id":   userID,
		"tenant_id": tenantID,
	})
	if err != nil {
		return fmt.Errorf("failed to mark notification as read: %w", err)
	}
	if len(result.Records) == 0 {
		return fmt.Errorf("notification not found")
	}
	return nil
}

// MarkAllAsRead marks all notifications for a user as read
func (s *NotificationService) MarkAllAsRead(ctx context.Context, userID, tenantID string) error {
	query := `
		MATCH (n:Notification {user_id: $user_id, tenant_id: $tenant_id, read: false})
		SET n.read = true
	`
	_, err := s.neo4j.ExecuteQuery(ctx, query, map[string]interface{}{
		"user_id":   userID,
		"tenant_id": tenantID,
	})
	if err != nil {
		return fmt.Errorf("failed to mark all as read: %w", err)
	}
	return nil
}

// DeleteNotification removes a notification
func (s *NotificationService) DeleteNotification(ctx context.Context, id, userID, tenantID string) error {
	query := `
		MATCH (n:Notification {id: $id, user_id: $user_id, tenant_id: $tenant_id})
		DELETE n
	`
	_, err := s.neo4j.ExecuteQuery(ctx, query, map[string]interface{}{
		"id":        id,
		"user_id":   userID,
		"tenant_id": tenantID,
	})
	if err != nil {
		return fmt.Errorf("failed to delete notification: %w", err)
	}
	return nil
}

// GetUnreadCount returns the count of unread notifications for a user
func (s *NotificationService) GetUnreadCount(ctx context.Context, userID, tenantID string) (int, error) {
	query := `
		MATCH (n:Notification {user_id: $user_id, tenant_id: $tenant_id, read: false})
		RETURN count(n) AS count
	`
	result, err := s.neo4j.ExecuteQuery(ctx, query, map[string]interface{}{
		"user_id":   userID,
		"tenant_id": tenantID,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get unread count: %w", err)
	}
	if len(result.Records) > 0 {
		v, _ := result.Records[0].Get("count")
		if iv, ok := v.(int64); ok {
			return int(iv), nil
		}
	}
	return 0, nil
}

// ProcessWorkflowComplete handles a workflow completion webhook and creates a notification
func (s *NotificationService) ProcessWorkflowComplete(ctx context.Context, payload *models.WorkflowCompleteWebhookPayload) error {
	notifType := "success"
	title := fmt.Sprintf("Workflow \"%s\" Succeeded", payload.WorkflowName)
	message := "Workflow completed successfully."

	switch payload.Status {
	case "Failed", "Error":
		notifType = "error"
		title = fmt.Sprintf("Workflow \"%s\" Failed", payload.WorkflowName)
		message = "Workflow execution failed."
		if payload.Message != "" {
			message = payload.Message
		}
	case "Succeeded":
		if payload.Result != "" {
			message = payload.Result
		}
	}

	metadata := map[string]interface{}{
		"workflow_id":  payload.WorkflowID,
		"execution_id": payload.ExecutionID,
		"status":       payload.Status,
	}

	req := &models.CreateNotificationRequest{
		UserID:   payload.UserID,
		Type:     notifType,
		Title:    title,
		Message:  message,
		Source:   "Workflow Engine",
		Metadata: metadata,
	}

	_, err := s.CreateNotification(ctx, req, payload.TenantID)
	return err
}

// recordToNotification converts a Neo4j record to a Notification
func recordToNotification(record *neo4j.Record) models.Notification {
	n := models.Notification{}
	if v, _ := record.Get("id"); v != nil {
		n.ID, _ = v.(string)
	}
	if v, _ := record.Get("user_id"); v != nil {
		n.UserID, _ = v.(string)
	}
	if v, _ := record.Get("tenant_id"); v != nil {
		n.TenantID, _ = v.(string)
	}
	if v, _ := record.Get("type"); v != nil {
		n.Type, _ = v.(string)
	}
	if v, _ := record.Get("title"); v != nil {
		n.Title, _ = v.(string)
	}
	if v, _ := record.Get("message"); v != nil {
		n.Message, _ = v.(string)
	}
	if v, _ := record.Get("source"); v != nil {
		n.Source, _ = v.(string)
	}
	if v, _ := record.Get("read"); v != nil {
		n.Read, _ = v.(bool)
	}
	if v, _ := record.Get("created_at"); v != nil {
		switch t := v.(type) {
		case time.Time:
			n.CreatedAt = t
		case neo4j.Date:
			n.CreatedAt = t.Time()
		}
	}
	return n
}

// NewNotificationEvent is a convenience constructor
func NewNotificationEvent(eventType EventType, notificationID, userID string, data map[string]interface{}) Event {
	return Event{
		ID:        uuid.New().String(),
		Type:      eventType,
		Source:    "notification-service",
		Subject:   notificationID,
		Data:      data,
		UserID:    userID,
		Timestamp: time.Now(),
		Version:   "1.0",
	}
}
