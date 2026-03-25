package models

import "time"

// Comment represents a comment on a resource
type Comment struct {
	ID           string    `json:"id"`
	Content      string    `json:"content"`
	AuthorID     string    `json:"authorId"`
	ResourceID   string    `json:"resourceId"`
	ResourceType string    `json:"resourceType"` // "notebook"
	ParentID     string    `json:"parentId,omitempty"`
	TenantID     string    `json:"tenantId"`
	Mentions     []string  `json:"mentions,omitempty"`
	Edited       bool      `json:"edited"`
	EditedAt     time.Time `json:"editedAt,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// CreateCommentRequest represents a request to create a comment
type CreateCommentRequest struct {
	Content  string   `json:"content" validate:"required,min=1,max=5000"`
	ParentID string   `json:"parentId,omitempty" validate:"omitempty,uuid"`
	Mentions []string `json:"mentions,omitempty"`
}

// UpdateCommentRequest represents a request to update a comment
type UpdateCommentRequest struct {
	Content  string   `json:"content" validate:"required,min=1,max=5000"`
	Mentions []string `json:"mentions,omitempty"`
}

// CommentResponse represents a comment in API responses
type CommentResponse struct {
	ID           string             `json:"id"`
	Content      string             `json:"content"`
	AuthorID     string             `json:"authorId"`
	Author       *PublicUserResponse `json:"author,omitempty"`
	ResourceID   string             `json:"resourceId"`
	ResourceType string             `json:"resourceType"`
	ParentID     string             `json:"parentId,omitempty"`
	Mentions     []string           `json:"mentions,omitempty"`
	Edited       bool               `json:"edited"`
	Replies      []*CommentResponse `json:"replies,omitempty"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

// CommentListResponse represents a paginated list of comments
type CommentListResponse struct {
	Comments []*CommentResponse `json:"comments"`
	Total    int                `json:"total"`
}

// CommentEvent represents a real-time comment event for SSE
type CommentEvent struct {
	Type    string           `json:"type"` // "created", "updated", "deleted"
	Comment *CommentResponse `json:"comment"`
}
