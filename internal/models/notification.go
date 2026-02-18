package models

import "time"

// Notification represents an in-app notification stored in Neo4j
type Notification struct {
	ID        string                 `json:"id" neo4j:"id"`
	UserID    string                 `json:"user_id" neo4j:"user_id"`
	TenantID  string                 `json:"tenant_id" neo4j:"tenant_id"`
	Type      string                 `json:"type" neo4j:"type"`       // success, info, warning, error
	Title     string                 `json:"title" neo4j:"title"`
	Message   string                 `json:"message" neo4j:"message"`
	Source    string                 `json:"source" neo4j:"source"`
	Metadata  map[string]interface{} `json:"metadata,omitempty" neo4j:"metadata"`
	Read      bool                   `json:"read" neo4j:"read"`
	CreatedAt time.Time              `json:"created_at" neo4j:"created_at"`
}

// CreateNotificationRequest is the request body for creating a notification
type CreateNotificationRequest struct {
	UserID  string                 `json:"user_id" binding:"required"`
	Type    string                 `json:"type" binding:"required,oneof=success info warning error"`
	Title   string                 `json:"title" binding:"required"`
	Message string                 `json:"message" binding:"required"`
	Source  string                 `json:"source"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// WorkflowCompleteWebhookPayload is posted by Argo sync steps or onExit handlers
type WorkflowCompleteWebhookPayload struct {
	WorkflowID   string `json:"workflow_id" binding:"required"`
	WorkflowName string `json:"workflow_name"`
	ExecutionID  string `json:"execution_id"`
	UserID       string `json:"user_id" binding:"required"`
	TenantID     string `json:"tenant_id"`
	Status       string `json:"status" binding:"required"` // Succeeded, Failed, Error
	Result       string `json:"result,omitempty"`
	Message      string `json:"message,omitempty"`
}
