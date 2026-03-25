package models

import "time"

// Invitation represents an invitation to share a resource
type Invitation struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`         // "notebook_share"
	InviterID    string    `json:"inviterId"`
	InviteeEmail string    `json:"inviteeEmail"`
	ResourceID   string    `json:"resourceId"`
	ResourceType string    `json:"resourceType"` // "notebook"
	Permission   string    `json:"permission"`   // "read", "write", "admin"
	Token        string    `json:"-"`            // Hidden from API responses
	Status       string    `json:"status"`       // "pending", "accepted", "expired", "cancelled"
	Message      string    `json:"message,omitempty"`
	TenantID     string    `json:"tenantId"`
	SpaceID      string    `json:"spaceId"`
	ExpiresAt    time.Time `json:"expiresAt"`
	CreatedAt    time.Time `json:"createdAt"`
	AcceptedAt   time.Time `json:"acceptedAt,omitempty"`
}

// CreateInvitationRequest represents a request to create an invitation
type CreateInvitationRequest struct {
	Email      string `json:"email" validate:"required,email"`
	Permission string `json:"permission" validate:"required,oneof=read write admin view edit"`
	Message    string `json:"message,omitempty" validate:"max=500"`
}

// NormalizePermission maps frontend permission names to backend canonical names
func NormalizePermission(p string) string {
	switch p {
	case "view":
		return "read"
	case "edit":
		return "write"
	default:
		return p
	}
}

// InvitationResponse represents an invitation in API responses
type InvitationResponse struct {
	ID           string    `json:"id"`
	InviterID    string    `json:"inviterId"`
	InviterName  string    `json:"inviterName,omitempty"`
	InviteeEmail string    `json:"inviteeEmail"`
	ResourceID   string    `json:"resourceId"`
	ResourceType string    `json:"resourceType"`
	ResourceName string    `json:"resourceName,omitempty"`
	Permission   string    `json:"permission"`
	Status       string    `json:"status"`
	Message      string    `json:"message,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt"`
	CreatedAt    time.Time `json:"createdAt"`
	AcceptedAt   time.Time `json:"acceptedAt,omitempty"`
}

// InvitationListResponse represents a list of invitations
type InvitationListResponse struct {
	Invitations []*InvitationResponse `json:"invitations"`
	Total       int                   `json:"total"`
}

// AcceptInvitationRequest represents a request to accept an invitation
type AcceptInvitationRequest struct {
	Token string `json:"token" validate:"required"`
}
