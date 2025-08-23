package models

import (
	"time"

	"github.com/google/uuid"
)

// Organization represents an organization in the system
type Organization struct {
	ID          string `json:"id" validate:"required,uuid"`
	Name        string `json:"name" validate:"required,min=2,max=100"`
	Slug        string `json:"slug" validate:"required,min=2,max=100"`
	Description string `json:"description" validate:"omitempty,max=500"`
	AvatarURL   string `json:"avatar_url,omitempty" validate:"omitempty,url,max=500"`
	Website     string `json:"website,omitempty" validate:"omitempty,url,max=500"`
	Location    string `json:"location,omitempty" validate:"omitempty,max=200"`
	Visibility  string `json:"visibility" validate:"required,oneof=public private"`

	// Billing information
	Billing map[string]interface{} `json:"billing,omitempty" validate:"omitempty,neo4j_compatible"`

	// Settings
	Settings map[string]interface{} `json:"settings,omitempty" validate:"omitempty,neo4j_compatible"`

	// Metadata
	CreatedBy    string    `json:"created_by" validate:"required,uuid"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MemberCount  int       `json:"member_count"`
	TeamCount    int       `json:"team_count,omitempty"`
	NotebookCount int      `json:"notebookCount,omitempty"` // Standardized on notebook naming

	// Computed fields (not stored in database)
	UserRole string `json:"user_role,omitempty"` // Current user's role in this organization
}

// OrganizationCreateRequest represents a request to create an organization
type OrganizationCreateRequest struct {
	Name         string                 `json:"name" validate:"required,safe_string,min=2,max=100"`
	Slug         string                 `json:"slug,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	Description  string                 `json:"description,omitempty" validate:"omitempty,safe_string,max=500"`
	AvatarURL    string                 `json:"avatar_url,omitempty" validate:"omitempty,url,max=500"`
	Website      string                 `json:"website,omitempty" validate:"omitempty,url,max=500"`
	Location     string                 `json:"location,omitempty" validate:"omitempty,safe_string,max=200"`
	Visibility   string                 `json:"visibility" validate:"required,oneof=public private"`
	BillingEmail string                 `json:"billing_email,omitempty" validate:"omitempty,email,max=254"`
	Billing      map[string]interface{} `json:"billing,omitempty" validate:"omitempty,neo4j_compatible"`
	Settings     map[string]interface{} `json:"settings,omitempty" validate:"omitempty,neo4j_compatible"`
}

// OrganizationUpdateRequest represents a request to update an organization
type OrganizationUpdateRequest struct {
	Name        *string                `json:"name,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	Slug        *string                `json:"slug,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	Description *string                `json:"description,omitempty" validate:"omitempty,safe_string,max=500"`
	AvatarURL   *string                `json:"avatar_url,omitempty" validate:"omitempty,url,max=500"`
	Website     *string                `json:"website,omitempty" validate:"omitempty,url,max=500"`
	Location    *string                `json:"location,omitempty" validate:"omitempty,safe_string,max=200"`
	Visibility  *string                `json:"visibility,omitempty" validate:"omitempty,oneof=public private"`
	Billing     map[string]interface{} `json:"billing,omitempty" validate:"omitempty,neo4j_compatible"`
	Settings    map[string]interface{} `json:"settings,omitempty" validate:"omitempty,neo4j_compatible"`
}

// OrganizationResponse represents an organization response with camelCase fields
type OrganizationResponse struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Slug           string                 `json:"slug"`
	Description    string                 `json:"description"`
	AvatarUrl      string                 `json:"avatarUrl,omitempty"` // camelCase for frontend
	Website        string                 `json:"website,omitempty"`
	Location       string                 `json:"location,omitempty"`
	Visibility     string                 `json:"visibility"`
	Billing        map[string]interface{} `json:"billing,omitempty"`
	Settings       map[string]interface{} `json:"settings,omitempty"`
	CreatedBy      string                 `json:"createdBy"` // camelCase for frontend
	CreatedAt      time.Time              `json:"createdAt"`
	UpdatedAt      time.Time              `json:"updatedAt"`
	MemberCount    int                    `json:"memberCount"`
	TeamCount      int                    `json:"teamCount,omitempty"`
	NotebookCount   int                   `json:"notebookCount,omitempty"` // Standardized on notebook naming
	UserRole       string                 `json:"userRole,omitempty"`
}

// OrganizationMember represents an organization member relationship
type OrganizationMember struct {
	UserID       string    `json:"user_id" validate:"required,uuid"`
	OrgID        string    `json:"org_id" validate:"required,uuid"`
	Role         string    `json:"role" validate:"required,oneof=owner admin member billing"`
	JoinedAt     time.Time `json:"joined_at"`
	InvitedBy    string    `json:"invited_by" validate:"omitempty,uuid"`
	Title        string    `json:"title,omitempty" validate:"omitempty,safe_string,max=100"`
	Department   string    `json:"department,omitempty" validate:"omitempty,safe_string,max=100"`

	// Computed user data (joined from User model)
	Name     string   `json:"name,omitempty"`
	Email    string   `json:"email,omitempty"`
	Username string   `json:"username,omitempty"`
	Avatar   string   `json:"avatar,omitempty"`
	Teams    []string `json:"teams,omitempty"` // Team IDs this user belongs to in this org
}

// OrganizationMemberResponse represents an organization member response with camelCase fields
type OrganizationMemberResponse struct {
	UserID     string   `json:"userId"`
	Name       string   `json:"name"`
	Email      string   `json:"email"`
	Role       string   `json:"role"`
	JoinedAt   time.Time `json:"joinedAt"`
	InvitedBy  string   `json:"invitedBy,omitempty"`
	Title      string   `json:"title,omitempty"`
	Department string   `json:"department,omitempty"`
	Teams      []string `json:"teams,omitempty"`
}

// OrganizationInviteRequest represents a request to invite an organization member
type OrganizationInviteRequest struct {
	Email      string `json:"email" validate:"required,email,max=254"`
	Role       string `json:"role" validate:"required,oneof=admin member billing"`
	Title      string `json:"title,omitempty" validate:"omitempty,safe_string,max=100"`
	Department string `json:"department,omitempty" validate:"omitempty,safe_string,max=100"`
}

// OrganizationMemberRoleUpdateRequest represents a request to update an organization member's role
type OrganizationMemberRoleUpdateRequest struct {
	Role       string `json:"role" validate:"required,oneof=admin member billing"`
	Title      string `json:"title,omitempty" validate:"omitempty,safe_string,max=100"`
	Department string `json:"department,omitempty" validate:"omitempty,safe_string,max=100"`
}

// NewOrganization creates a new organization with default values
func NewOrganization(req OrganizationCreateRequest, createdBy string) *Organization {
	now := time.Now()
	
	// Generate slug if not provided
	slug := req.Slug
	if slug == "" {
		// Simple slug generation - replace non-alphanumeric with hyphens and lowercase
		slug = generateSlug(req.Name)
	}

	return &Organization{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Slug:        slug,
		Description: req.Description,
		AvatarURL:   req.AvatarURL,
		Website:     req.Website,
		Location:    req.Location,
		Visibility:  req.Visibility,
		Billing:     req.Billing,
		Settings:    req.Settings,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
		MemberCount: 1, // Creator is first member
	}
}

// ToResponse converts an Organization to OrganizationResponse with camelCase fields
func (o *Organization) ToResponse() *OrganizationResponse {
	return &OrganizationResponse{
		ID:              o.ID,
		Name:            o.Name,
		Slug:            o.Slug,
		Description:     o.Description,
		AvatarUrl:       o.AvatarURL,
		Website:         o.Website,
		Location:        o.Location,
		Visibility:      o.Visibility,
		Billing:         o.Billing,
		Settings:        o.Settings,
		CreatedBy:       o.CreatedBy,
		CreatedAt:       o.CreatedAt,
		UpdatedAt:       o.UpdatedAt,
		MemberCount:     o.MemberCount,
		TeamCount:       o.TeamCount,
		NotebookCount:   o.NotebookCount, // Consistent notebook naming
		UserRole:        o.UserRole,
	}
}

// ToMemberResponse converts an OrganizationMember to OrganizationMemberResponse with camelCase fields
func (om *OrganizationMember) ToMemberResponse() *OrganizationMemberResponse {
	return &OrganizationMemberResponse{
		UserID:     om.UserID,
		Name:       om.Name,
		Email:      om.Email,
		Role:       om.Role,
		JoinedAt:   om.JoinedAt,
		InvitedBy:  om.InvitedBy,
		Title:      om.Title,
		Department: om.Department,
		Teams:      om.Teams,
	}
}

// Update updates organization fields from an update request
func (o *Organization) Update(req OrganizationUpdateRequest) {
	if req.Name != nil {
		o.Name = *req.Name
	}
	if req.Slug != nil {
		o.Slug = *req.Slug
	}
	if req.Description != nil {
		o.Description = *req.Description
	}
	if req.AvatarURL != nil {
		o.AvatarURL = *req.AvatarURL
	}
	if req.Website != nil {
		o.Website = *req.Website
	}
	if req.Location != nil {
		o.Location = *req.Location
	}
	if req.Visibility != nil {
		o.Visibility = *req.Visibility
	}
	if req.Billing != nil {
		o.Billing = req.Billing
	}
	if req.Settings != nil {
		o.Settings = req.Settings
	}
	o.UpdatedAt = time.Now()
}

// CanUserModify checks if a user can modify the organization based on their role
func (o *Organization) CanUserModify(userRole string) bool {
	return userRole == "owner" || userRole == "admin"
}

// CanUserDelete checks if a user can delete the organization
func (o *Organization) CanUserDelete(userRole string) bool {
	return userRole == "owner"
}

// CanUserInvite checks if a user can invite members to the organization
func (o *Organization) CanUserInvite(userRole string) bool {
	return userRole == "owner" || userRole == "admin"
}

// CanUserManageBilling checks if a user can manage billing for the organization
func (o *Organization) CanUserManageBilling(userRole string) bool {
	return userRole == "owner" || userRole == "billing"
}

// DefaultOrganizationSettings returns default settings for a new organization
func DefaultOrganizationSettings() map[string]interface{} {
	return map[string]interface{}{
		"membersCanCreateRepositories": true,
		"membersCanCreateTeams":        true,
		"membersCanFork":               true,
		"defaultMemberPermissions":     "read",
		"twoFactorRequired":            false,
		"allowExternalSharing":         false,
		"requireApprovalForJoining":    true,
	}
}

// DefaultOrganizationBilling returns default billing information for a new organization
func DefaultOrganizationBilling(billingEmail string) map[string]interface{} {
	return map[string]interface{}{
		"plan":         "free",
		"seats":        3,
		"billingEmail": billingEmail,
		"nextBillingDate": nil,
		"subscriptionStatus": "active",
	}
}

// Helper function to generate a slug from name
func generateSlug(name string) string {
	// Simple slug generation - in production you might want a more sophisticated approach
	slug := ""
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			slug += string(r)
		} else if len(slug) > 0 && slug[len(slug)-1] != '-' {
			slug += "-"
		}
	}
	// Remove trailing hyphens and convert to lowercase
	for len(slug) > 0 && slug[len(slug)-1] == '-' {
		slug = slug[:len(slug)-1]
	}
	
	// Convert to lowercase
	result := ""
	for _, r := range slug {
		if r >= 'A' && r <= 'Z' {
			result += string(r + 32) // Convert to lowercase
		} else {
			result += string(r)
		}
	}
	
	return result
}

// OrganizationSearchRequest represents an organization search request
type OrganizationSearchRequest struct {
	Query      string `json:"query,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	Visibility string `json:"visibility,omitempty" validate:"omitempty,oneof=public private"`
	Limit      int    `json:"limit,omitempty" validate:"omitempty,min=1,max=100"`
	Offset     int    `json:"offset,omitempty" validate:"omitempty,min=0"`
}

// OrganizationSearchResponse represents an organization search response
type OrganizationSearchResponse struct {
	Organizations []*OrganizationResponse `json:"organizations"`
	Total         int                     `json:"total"`
	Limit         int                     `json:"limit"`
	Offset        int                     `json:"offset"`
	HasMore       bool                    `json:"hasMore"`
}

// OrganizationListResponse represents a paginated list of organizations
type OrganizationListResponse struct {
	Organizations []*OrganizationResponse `json:"organizations"`
	Total         int                     `json:"total"`
	Limit         int                     `json:"limit"`
	Offset        int                     `json:"offset"`
	HasMore       bool                    `json:"hasMore"`
}

// OrganizationStatsResponse represents organization statistics
type OrganizationStatsResponse struct {
	TotalOrganizations   int `json:"totalOrganizations"`
	PublicOrganizations  int `json:"publicOrganizations"`
	PrivateOrganizations int `json:"privateOrganizations"`
}