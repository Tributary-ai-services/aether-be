package models

import "time"

// ChatConversation represents a chat conversation within a notebook
type ChatConversation struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	NotebookID   string    `json:"notebookId"`
	UserID       string    `json:"userId"`
	TenantID     string    `json:"tenantId"`
	SpaceID      string    `json:"spaceId"`
	SpaceType    string    `json:"spaceType"`
	MessageCount int       `json:"messageCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ChatMessage represents a message in a chat conversation
type ChatMessage struct {
	ID             string                 `json:"id"`
	ConversationID string                 `json:"conversationId"`
	Role           string                 `json:"role"` // "user", "assistant", "system"
	Content        string                 `json:"content"`
	IsError        bool                   `json:"isError"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`
}

// CreateConversationRequest represents a request to create a conversation
type CreateConversationRequest struct {
	Name string `json:"name,omitempty"`
}

// UpdateConversationRequest represents a request to update a conversation
type UpdateConversationRequest struct {
	Name string `json:"name" validate:"required,min=1,max=200"`
}

// ConversationResponse represents a conversation in API responses
type ConversationResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	NotebookID   string    `json:"notebookId"`
	UserID       string    `json:"userId"`
	MessageCount int       `json:"messageCount"`
	LastMessage  string    `json:"lastMessage,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ConversationListResponse represents a paginated list of conversations
type ConversationListResponse struct {
	Conversations []*ConversationResponse `json:"conversations"`
	Total         int                     `json:"total"`
}

// ChatMessageResponse represents a message in API responses
type ChatMessageResponse struct {
	ID             string                 `json:"id"`
	ConversationID string                 `json:"conversationId"`
	Role           string                 `json:"role"`
	Content        string                 `json:"content"`
	IsError        bool                   `json:"isError"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`
}

// ChatMessageListResponse represents a paginated list of messages
type ChatMessageListResponse struct {
	Messages []*ChatMessageResponse `json:"messages"`
	Total    int                    `json:"total"`
}
