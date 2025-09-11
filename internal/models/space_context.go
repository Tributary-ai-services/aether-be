package models

import (
	"time"
)

// SpaceType represents the type of space
type SpaceType string

const (
	SpaceTypePersonal     SpaceType = "personal"
	SpaceTypeOrganization SpaceType = "organization"
)

// SpaceContext represents the current working space context
type SpaceContext struct {
	// Space identification
	SpaceType SpaceType `json:"space_type"`
	SpaceID   string    `json:"space_id"` // user ID for personal, org ID for organization

	// Tenant information
	TenantID string `json:"tenant_id"`
	APIKey   string `json:"-"` // Not serialized

	// User context within the space
	UserID   string `json:"user_id"`
	UserRole string `json:"user_role"` // "owner" for personal, org role for organization

	// Metadata
	SpaceName   string    `json:"space_name"`
	ResolvedAt  time.Time `json:"resolved_at"`
	Permissions []string  `json:"permissions"`
}

// SpaceContextRequest represents a request to resolve a space context
type SpaceContextRequest struct {
	SpaceType SpaceType `json:"space_type" validate:"required,oneof=personal organization"`
	SpaceID   string    `json:"space_id" validate:"required,uuid"`
}

// IsPersonalSpace returns true if this is a personal space
func (sc *SpaceContext) IsPersonalSpace() bool {
	return sc.SpaceType == SpaceTypePersonal
}

// IsOrganizationSpace returns true if this is an organization space
func (sc *SpaceContext) IsOrganizationSpace() bool {
	return sc.SpaceType == SpaceTypeOrganization
}

// HasPermission checks if the context has a specific permission
func (sc *SpaceContext) HasPermission(permission string) bool {
	for _, p := range sc.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// CanCreate checks if the context allows creating resources
func (sc *SpaceContext) CanCreate() bool {
	return sc.HasPermission("create") || sc.HasPermission("write") || sc.UserRole == "owner" || sc.UserRole == "admin"
}

// CanRead checks if the context allows reading resources
func (sc *SpaceContext) CanRead() bool {
	return sc.HasPermission("read") || sc.CanCreate()
}

// CanUpdate checks if the context allows updating resources
func (sc *SpaceContext) CanUpdate() bool {
	return sc.HasPermission("update") || sc.HasPermission("write") || sc.UserRole == "owner" || sc.UserRole == "admin"
}

// CanDelete checks if the context allows deleting resources
func (sc *SpaceContext) CanDelete() bool {
	return sc.HasPermission("delete") || sc.UserRole == "owner" || sc.UserRole == "admin"
}

// GetTenantInfo returns the tenant ID and API key for this space
func (sc *SpaceContext) GetTenantInfo() (tenantID, apiKey string) {
	return sc.TenantID, sc.APIKey
}

// SpaceInfo provides public information about a space
type SpaceInfo struct {
	SpaceType   SpaceType `json:"space_type"`
	SpaceID     string    `json:"space_id"`
	SpaceName   string    `json:"space_name"`
	TenantID    string    `json:"tenant_id"`
	UserRole    string    `json:"user_role"`
	Permissions []string  `json:"permissions"`
}

// ToSpaceInfo converts SpaceContext to SpaceInfo (excludes sensitive data)
func (sc *SpaceContext) ToSpaceInfo() *SpaceInfo {
	return &SpaceInfo{
		SpaceType:   sc.SpaceType,
		SpaceID:     sc.SpaceID,
		SpaceName:   sc.SpaceName,
		TenantID:    sc.TenantID,
		UserRole:    sc.UserRole,
		Permissions: sc.Permissions,
	}
}

// SpaceListResponse represents a list of available spaces for a user
type SpaceListResponse struct {
	PersonalSpace      *SpaceInfo   `json:"personal_space"`
	OrganizationSpaces []*SpaceInfo `json:"organization_spaces"`
	CurrentSpace       *SpaceInfo   `json:"current_space,omitempty"`
}

// SpaceCreateRequest represents a request to create a new space
type SpaceCreateRequest struct {
	Name           string `json:"name" validate:"required,min=1,max=100"`
	Description    string `json:"description,omitempty" validate:"max=500"`
	Visibility     string `json:"visibility,omitempty" validate:"oneof=private team organization public"`
	OrganizationID string `json:"organization_id,omitempty" validate:"omitempty,uuid"`
}

// SpaceUpdateRequest represents a request to update a space
type SpaceUpdateRequest struct {
	Name        *string `json:"name,omitempty" validate:"omitempty,min=1,max=100"`
	Description *string `json:"description,omitempty" validate:"omitempty,max=500"`
	Visibility  *string `json:"visibility,omitempty" validate:"omitempty,oneof=private team organization public"`
}

// SpaceResponse represents a space creation/update response
type SpaceResponse struct {
	SpaceInfo
	Description string    `json:"description,omitempty"`
	Visibility  string    `json:"visibility"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}