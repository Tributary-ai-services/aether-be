package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user in the system
type User struct {
	ID         string `json:"id" validate:"required,uuid"`
	KeycloakID string `json:"keycloak_id" validate:"required"`
	Email      string `json:"email" validate:"required,email"`
	Username   string `json:"username" validate:"required,min=3,max=50"`
	FullName   string `json:"full_name" validate:"required,min=2,max=100"`
	AvatarURL  string `json:"avatar_url,omitempty" validate:"omitempty,url"`

	// Keycloak sync data
	KeycloakRoles      []string               `json:"keycloak_roles,omitempty"`
	KeycloakGroups     []string               `json:"keycloak_groups,omitempty"`
	KeycloakAttributes map[string]interface{} `json:"keycloak_attributes,omitempty" validate:"omitempty,neo4j_compatible"`

	// Local app data
	Preferences map[string]interface{} `json:"preferences,omitempty" validate:"omitempty,neo4j_compatible"`
	Status      string                 `json:"status" validate:"required,oneof=active inactive suspended"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	LastSyncAt  *time.Time `json:"last_sync_at,omitempty"`
}

// UserCreateRequest represents a request to create a user
type UserCreateRequest struct {
	KeycloakID  string                 `json:"keycloak_id" validate:"required,uuid"`
	Email       string                 `json:"email" validate:"required,email,max=254"`
	Username    string                 `json:"username" validate:"required,username,min=3,max=50"`
	FullName    string                 `json:"full_name" validate:"required,safe_string,min=2,max=100"`
	AvatarURL   string                 `json:"avatar_url,omitempty" validate:"omitempty,url,max=500"`
	Preferences map[string]interface{} `json:"preferences,omitempty" validate:"omitempty,neo4j_compatible"`
}

// UserUpdateRequest represents a request to update a user
type UserUpdateRequest struct {
	FullName    *string                `json:"full_name,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	AvatarURL   *string                `json:"avatar_url,omitempty" validate:"omitempty,url,max=500"`
	Preferences map[string]interface{} `json:"preferences,omitempty" validate:"omitempty,neo4j_compatible"`
	Status      *string                `json:"status,omitempty" validate:"omitempty,user_status"`
}

// UserResponse represents a user response (may exclude sensitive data)
type UserResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	FullName  string    `json:"fullName"`
	AvatarURL string    `json:"avatarUrl,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PublicUserResponse represents a minimal user response for public contexts
type PublicUserResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	FullName  string `json:"fullName"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// NewUser creates a new user with default values
func NewUser(req UserCreateRequest) *User {
	now := time.Now()
	return &User{
		ID:          uuid.New().String(),
		KeycloakID:  req.KeycloakID,
		Email:       req.Email,
		Username:    req.Username,
		FullName:    req.FullName,
		AvatarURL:   req.AvatarURL,
		Preferences: req.Preferences,
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// ToResponse converts a User to UserResponse
func (u *User) ToResponse() *UserResponse {
	return &UserResponse{
		ID:        u.ID,
		Email:     u.Email,
		Username:  u.Username,
		FullName:  u.FullName,
		AvatarURL: u.AvatarURL,
		Status:    u.Status,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

// ToPublicResponse converts a User to PublicUserResponse
func (u *User) ToPublicResponse() *PublicUserResponse {
	return &PublicUserResponse{
		ID:        u.ID,
		Username:  u.Username,
		FullName:  u.FullName,
		AvatarURL: u.AvatarURL,
	}
}

// Update updates user fields from an update request
func (u *User) Update(req UserUpdateRequest) {
	if req.FullName != nil {
		u.FullName = *req.FullName
	}
	if req.AvatarURL != nil {
		u.AvatarURL = *req.AvatarURL
	}
	if req.Preferences != nil {
		u.Preferences = req.Preferences
	}
	if req.Status != nil {
		u.Status = *req.Status
	}
	u.UpdatedAt = time.Now()
}

// UpdateKeycloakData updates user data from Keycloak
func (u *User) UpdateKeycloakData(roles, groups []string, attributes map[string]interface{}) {
	u.KeycloakRoles = roles
	u.KeycloakGroups = groups
	u.KeycloakAttributes = attributes
	now := time.Now()
	u.LastSyncAt = &now
	u.UpdatedAt = now
}

// UpdateLastLogin updates the last login timestamp
func (u *User) UpdateLastLogin() {
	now := time.Now()
	u.LastLoginAt = &now
	u.UpdatedAt = now
}

// IsActive returns true if the user is active
func (u *User) IsActive() bool {
	return u.Status == "active"
}

// HasRole checks if user has a specific role
func (u *User) HasRole(role string) bool {
	for _, r := range u.KeycloakRoles {
		if r == role {
			return true
		}
	}
	return false
}

// HasGroup checks if user belongs to a specific group
func (u *User) HasGroup(group string) bool {
	for _, g := range u.KeycloakGroups {
		if g == group {
			return true
		}
	}
	return false
}

// UserSearchRequest represents a user search request
type UserSearchRequest struct {
	Query    string `json:"query,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	Email    string `json:"email,omitempty" validate:"omitempty,email,max=254"`
	Username string `json:"username,omitempty" validate:"omitempty,username,min=3,max=50"`
	Status   string `json:"status,omitempty" validate:"omitempty,user_status"`
	Limit    int    `json:"limit,omitempty" validate:"omitempty,min=1,max=100"`
	Offset   int    `json:"offset,omitempty" validate:"omitempty,min=0"`
}

// UserSearchResponse represents a user search response
type UserSearchResponse struct {
	Users   []*UserResponse `json:"users"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	HasMore bool            `json:"has_more"`
}

// UserPreferences represents user preferences
type UserPreferences struct {
	Language      string                 `json:"language,omitempty"`
	Timezone      string                 `json:"timezone,omitempty"`
	DateFormat    string                 `json:"date_format,omitempty"`
	Theme         string                 `json:"theme,omitempty"`
	Notifications map[string]bool        `json:"notifications,omitempty"`
	Settings      map[string]interface{} `json:"settings,omitempty" validate:"omitempty,neo4j_compatible"`
}

// UserStats represents user statistics
type UserStats struct {
	NotebookCount  int    `json:"notebook_count"`
	DocumentCount  int    `json:"document_count"`
	TotalSizeBytes int64  `json:"total_size_bytes"`
	ProcessedDocs  int    `json:"processed_documents"`
	FailedDocs     int    `json:"failed_documents"`
	CreatedAt      string `json:"member_since"`
}

// UserStatsResponse represents user statistics
type UserStatsResponse struct {
	TotalUsers     int `json:"total_users"`
	ActiveUsers    int `json:"active_users"`
	InactiveUsers  int `json:"inactive_users"`
	SuspendedUsers int `json:"suspended_users"`
}

// UserListResponse represents a paginated list of users
type UserListResponse struct {
	Users   []*PublicUserResponse `json:"users"`
	Total   int                   `json:"total"`
	Limit   int                   `json:"limit"`
	Offset  int                   `json:"offset"`
	HasMore bool                  `json:"has_more"`
}
