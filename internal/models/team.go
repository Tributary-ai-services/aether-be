package models

import (
	"time"

	"github.com/google/uuid"
)

// Team represents a team in the system
type Team struct {
	ID             string `json:"id" validate:"required,uuid"`
	Name           string `json:"name" validate:"required,min=2,max=100"`
	Description    string `json:"description" validate:"omitempty,max=500"`
	OrganizationID string `json:"organization_id" validate:"required,uuid"`
	Visibility     string `json:"visibility" validate:"required,oneof=private organization public"`
	Icon           string `json:"icon,omitempty" validate:"omitempty,url,max=500"`

	// Settings
	Settings map[string]interface{} `json:"settings,omitempty" validate:"omitempty,neo4j_compatible"`

	// Metadata
	CreatedBy   string    `json:"created_by" validate:"required,uuid"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	MemberCount int       `json:"member_count"`

	// Computed fields (not stored in database)
	UserRole      string `json:"user_role,omitempty"`      // Current user's role in this team
	NotebookCount int    `json:"notebook_count,omitempty"` // Number of notebooks owned by team
	OwnerName     string `json:"owner_name,omitempty"`     // Full name of the team owner
}

// TeamCreateRequest represents a request to create a team
type TeamCreateRequest struct {
	Name           string                 `json:"name" validate:"required,safe_string,min=2,max=100"`
	Description    string                 `json:"description,omitempty" validate:"omitempty,safe_string,max=500"`
	OrganizationID string                 `json:"organization_id" validate:"required,uuid"`
	Visibility     string                 `json:"visibility" validate:"required,oneof=private organization public"`
	Icon           string                 `json:"icon,omitempty" validate:"omitempty,url,max=500"`
	Settings       map[string]interface{} `json:"settings,omitempty" validate:"omitempty,neo4j_compatible"`
}

// TeamUpdateRequest represents a request to update a team
type TeamUpdateRequest struct {
	Name        *string                `json:"name,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	Description *string                `json:"description,omitempty" validate:"omitempty,safe_string,max=500"`
	Visibility  *string                `json:"visibility,omitempty" validate:"omitempty,oneof=private organization public"`
	Icon        *string                `json:"icon,omitempty" validate:"omitempty,url,max=500"`
	Settings    map[string]interface{} `json:"settings,omitempty" validate:"omitempty,neo4j_compatible"`
}

// TeamResponse represents a team response
type TeamResponse struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	OrganizationID string                 `json:"organizationId"` // camelCase for frontend compatibility
	Visibility     string                 `json:"visibility"`
	Icon           string                 `json:"icon,omitempty"`
	Settings       map[string]interface{} `json:"settings,omitempty"`
	CreatedBy      string                 `json:"createdBy"` // camelCase for frontend compatibility
	CreatedAt      time.Time              `json:"createdAt"`
	UpdatedAt      time.Time              `json:"updatedAt"`
	MemberCount    int                    `json:"memberCount"`
	UserRole       string                 `json:"userRole,omitempty"`
	NotebookCount  int                    `json:"notebookCount,omitempty"`
}

// TeamMember represents a team member relationship
type TeamMember struct {
	UserID    string    `json:"user_id" validate:"required,uuid"`
	TeamID    string    `json:"team_id" validate:"required,uuid"`
	Role      string    `json:"role" validate:"required,oneof=owner admin member viewer"`
	JoinedAt  time.Time `json:"joined_at"`
	InvitedBy string    `json:"invited_by" validate:"omitempty,uuid"`

	// Computed user data (joined from User model)
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

// TeamMemberResponse represents a team member response with camelCase fields
type TeamMemberResponse struct {
	UserID    string    `json:"userId"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	JoinedAt  time.Time `json:"joinedAt"`
	InvitedBy string    `json:"invitedBy,omitempty"`
}

// TeamInviteRequest represents a request to invite a team member
type TeamInviteRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
	Role  string `json:"role" validate:"required,oneof=admin member viewer"`
}

// TeamMemberRoleUpdateRequest represents a request to update a team member's role
type TeamMemberRoleUpdateRequest struct {
	Role string `json:"role" validate:"required,oneof=admin member viewer"`
}

// NewTeam creates a new team with default values
func NewTeam(req TeamCreateRequest, createdBy string) *Team {
	now := time.Now()
	return &Team{
		ID:             uuid.New().String(),
		Name:           req.Name,
		Description:    req.Description,
		OrganizationID: req.OrganizationID,
		Visibility:     req.Visibility,
		Icon:           req.Icon,
		Settings:       req.Settings,
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
		MemberCount:    1, // Creator is first member
	}
}

// ToResponse converts a Team to TeamResponse with camelCase fields
func (t *Team) ToResponse() *TeamResponse {
	return &TeamResponse{
		ID:             t.ID,
		Name:           t.Name,
		Description:    t.Description,
		OrganizationID: t.OrganizationID,
		Visibility:     t.Visibility,
		Icon:           t.Icon,
		Settings:       t.Settings,
		CreatedBy:      t.CreatedBy,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
		MemberCount:    t.MemberCount,
		UserRole:       t.UserRole,
		NotebookCount:  t.NotebookCount,
	}
}

// ToMemberResponse converts a TeamMember to TeamMemberResponse with camelCase fields
func (tm *TeamMember) ToMemberResponse() *TeamMemberResponse {
	return &TeamMemberResponse{
		UserID:    tm.UserID,
		Name:      tm.Name,
		Email:     tm.Email,
		Role:      tm.Role,
		JoinedAt:  tm.JoinedAt,
		InvitedBy: tm.InvitedBy,
	}
}

// Update updates team fields from an update request
func (t *Team) Update(req TeamUpdateRequest) {
	if req.Name != nil {
		t.Name = *req.Name
	}
	if req.Description != nil {
		t.Description = *req.Description
	}
	if req.Visibility != nil {
		t.Visibility = *req.Visibility
	}
	if req.Icon != nil {
		t.Icon = *req.Icon
	}
	if req.Settings != nil {
		t.Settings = req.Settings
	}
	t.UpdatedAt = time.Now()
}

// CanUserModify checks if a user can modify the team based on their role
func (t *Team) CanUserModify(userRole string) bool {
	return userRole == "owner" || userRole == "admin"
}

// CanUserDelete checks if a user can delete the team
func (t *Team) CanUserDelete(userRole string) bool {
	return userRole == "owner"
}

// CanUserInvite checks if a user can invite members to the team
func (t *Team) CanUserInvite(userRole string) bool {
	return userRole == "owner" || userRole == "admin"
}

// DefaultTeamSettings returns default settings for a new team
func DefaultTeamSettings() map[string]interface{} {
	return map[string]interface{}{
		"allowExternalSharing":         false,
		"requireApprovalForJoining":    true,
		"defaultNotebookVisibility":    "team",
		"allowMemberInvites":           false,
		"allowMemberNotebookCreation":  true,
		"notificationsEnabled":         true,
	}
}

// TeamSearchRequest represents a team search request
type TeamSearchRequest struct {
	Query          string `json:"query,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	OrganizationID string `json:"organization_id,omitempty" validate:"omitempty,uuid"`
	Visibility     string `json:"visibility,omitempty" validate:"omitempty,oneof=private organization public"`
	Limit          int    `json:"limit,omitempty" validate:"omitempty,min=1,max=100"`
	Offset         int    `json:"offset,omitempty" validate:"omitempty,min=0"`
}

// TeamSearchResponse represents a team search response
type TeamSearchResponse struct {
	Teams   []*TeamResponse `json:"teams"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	HasMore bool            `json:"hasMore"`
}

// TeamListResponse represents a paginated list of teams
type TeamListResponse struct {
	Teams   []*TeamResponse `json:"teams"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	HasMore bool            `json:"hasMore"`
}

// TeamStatsResponse represents team statistics
type TeamStatsResponse struct {
	TotalTeams      int `json:"totalTeams"`
	PublicTeams     int `json:"publicTeams"`
	OrganizationTeams int `json:"organizationTeams"`
	PrivateTeams    int `json:"privateTeams"`
}