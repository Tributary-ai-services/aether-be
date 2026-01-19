package models

import (
	"fmt"
	"time"
)

// SpaceStatus represents the status of a space
type SpaceStatus string

const (
	SpaceStatusActive    SpaceStatus = "active"
	SpaceStatusSuspended SpaceStatus = "suspended"
	SpaceStatusDeleted   SpaceStatus = "deleted"
)

// SpaceOwnerType represents the type of owner of a space
type SpaceOwnerType string

const (
	SpaceOwnerTypeUser         SpaceOwnerType = "user"
	SpaceOwnerTypeOrganization SpaceOwnerType = "organization"
)

// Space represents a top-level isolation boundary (Neo4j node)
type Space struct {
	// Identity
	ID       string `json:"id" validate:"required"`       // "space_<timestamp>"
	TenantID string `json:"tenant_id" validate:"required"` // "tenant_<timestamp>" - cross-service identifier

	// Cross-Service Mapping
	AudimodalTenantID string `json:"audimodal_tenant_id,omitempty"` // UUID returned by AudiModal on tenant creation
	DeeplakeNamespace string `json:"deeplake_namespace,omitempty"`  // Same as TenantID (for clarity)
	DeeplakeAPIKey    string `json:"-"`                              // API key with tenant_id embedded (not serialized)

	// Display
	Name        string `json:"name" validate:"required,min=1,max=100"`
	Description string `json:"description,omitempty" validate:"max=500"`

	// Type & Ownership
	Type       SpaceType      `json:"type" validate:"required,oneof=personal organization"`
	Visibility string         `json:"visibility" validate:"required,oneof=private team organization public"`
	OwnerID    string         `json:"owner_id" validate:"required"`
	OwnerType  SpaceOwnerType `json:"owner_type" validate:"required,oneof=user organization"`

	// Status
	Status    SpaceStatus `json:"status" validate:"required,oneof=active suspended deleted"`
	DeletedAt *time.Time  `json:"deleted_at,omitempty"`
	DeletedBy string      `json:"deleted_by,omitempty"`

	// Settings
	Quotas   *SpaceQuotas           `json:"quotas,omitempty"`
	Settings map[string]interface{} `json:"settings,omitempty" validate:"omitempty,neo4j_compatible"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SpaceQuotas represents resource quotas for a space
type SpaceQuotas struct {
	MaxNotebooks     int   `json:"max_notebooks"`      // Maximum number of notebooks
	MaxDocuments     int   `json:"max_documents"`      // Maximum number of documents
	MaxStorageBytes  int64 `json:"max_storage_bytes"`  // Maximum storage in bytes
	MaxMembersCount  int   `json:"max_members_count"`  // Maximum number of members (for org spaces)
	MaxTeamsCount    int   `json:"max_teams_count"`    // Maximum number of teams (for org spaces)
	UsedNotebooks    int   `json:"used_notebooks"`     // Current notebook count
	UsedDocuments    int   `json:"used_documents"`     // Current document count
	UsedStorageBytes int64 `json:"used_storage_bytes"` // Current storage usage
}

// SpaceMemberDTO represents space membership for API responses
// Note: This is a view/DTO for API responses, not a stored model.
// The actual data lives on the [:MEMBER_OF] relationship properties.
type SpaceMemberDTO struct {
	UserID      string    `json:"user_id"`
	SpaceID     string    `json:"space_id"`
	Role        string    `json:"role"`                   // "owner", "admin", "member", "viewer"
	Permissions []string  `json:"permissions,omitempty"`  // Explicit permissions
	JoinedAt    time.Time `json:"joined_at"`
	InvitedBy   string    `json:"invited_by,omitempty"`

	// User details (populated from User node)
	UserName  string `json:"user_name,omitempty"`
	UserEmail string `json:"user_email,omitempty"`
	Avatar    string `json:"avatar,omitempty"`
}

// SpaceMemberResponse represents a space member response with camelCase fields
type SpaceMemberResponse struct {
	UserID      string    `json:"userId"`
	SpaceID     string    `json:"spaceId"`
	Role        string    `json:"role"`
	Permissions []string  `json:"permissions,omitempty"`
	JoinedAt    time.Time `json:"joinedAt"`
	InvitedBy   string    `json:"invitedBy,omitempty"`
	UserName    string    `json:"userName,omitempty"`
	UserEmail   string    `json:"userEmail,omitempty"`
	Avatar      string    `json:"avatar,omitempty"`
}

// SpaceFullResponse represents a complete space response with camelCase fields
type SpaceFullResponse struct {
	ID                string                 `json:"id"`
	TenantID          string                 `json:"tenantId"`
	AudimodalTenantID string                 `json:"audimodalTenantId,omitempty"`
	DeeplakeNamespace string                 `json:"deeplakeNamespace,omitempty"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description,omitempty"`
	Type              SpaceType              `json:"type"`
	Visibility        string                 `json:"visibility"`
	OwnerID           string                 `json:"ownerId"`
	OwnerType         SpaceOwnerType         `json:"ownerType"`
	Status            SpaceStatus            `json:"status"`
	Quotas            *SpaceQuotas           `json:"quotas,omitempty"`
	Settings          map[string]interface{} `json:"settings,omitempty"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
	DeletedAt         *time.Time             `json:"deletedAt,omitempty"`

	// Computed fields
	MemberCount   int    `json:"memberCount,omitempty"`
	NotebookCount int    `json:"notebookCount,omitempty"`
	UserRole      string `json:"userRole,omitempty"` // Current user's role in this space
}

// AddMemberRequest represents a request to add a member to a space
type AddMemberRequest struct {
	UserID string `json:"user_id" validate:"required,uuid"`
	Role   string `json:"role" validate:"required,oneof=admin member viewer"`
}

// UpdateMemberRoleRequest represents a request to update a member's role
type UpdateMemberRoleRequest struct {
	Role string `json:"role" validate:"required,oneof=admin member viewer"`
}

// SpaceMembersListResponse represents a paginated list of space members
type SpaceMembersListResponse struct {
	Members []*SpaceMemberResponse `json:"members"`
	Total   int                    `json:"total"`
	Limit   int                    `json:"limit"`
	Offset  int                    `json:"offset"`
	HasMore bool                   `json:"hasMore"`
}

// NewSpace creates a new Space with default values
func NewSpace(name, description string, spaceType SpaceType, ownerID string, ownerType SpaceOwnerType) *Space {
	now := time.Now()
	timestamp := now.Unix()

	space := &Space{
		ID:       fmt.Sprintf("space_%d", timestamp),
		TenantID: fmt.Sprintf("tenant_%d", timestamp),
		Name:     name,
		Description: description,
		Type:       spaceType,
		Visibility: "private",
		OwnerID:    ownerID,
		OwnerType:  ownerType,
		Status:     SpaceStatusActive,
		Quotas:     DefaultSpaceQuotas(spaceType),
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Set DeeplakeNamespace to match TenantID
	space.DeeplakeNamespace = space.TenantID

	return space
}

// NewPersonalSpace creates a new personal space for a user
func NewPersonalSpace(userID, userName string) *Space {
	spaceName := fmt.Sprintf("%s's Space", userName)
	if userName == "" {
		spaceName = "Personal Space"
	}
	return NewSpace(spaceName, "Personal workspace", SpaceTypePersonal, userID, SpaceOwnerTypeUser)
}

// NewOrganizationSpace creates a new organization space
func NewOrganizationSpace(orgID, orgName string) *Space {
	return NewSpace(orgName, fmt.Sprintf("Organization workspace for %s", orgName), SpaceTypeOrganization, orgID, SpaceOwnerTypeOrganization)
}

// DefaultSpaceQuotas returns default quotas based on space type
func DefaultSpaceQuotas(spaceType SpaceType) *SpaceQuotas {
	if spaceType == SpaceTypePersonal {
		return &SpaceQuotas{
			MaxNotebooks:    100,
			MaxDocuments:    1000,
			MaxStorageBytes: 5 * 1024 * 1024 * 1024, // 5 GB
			MaxMembersCount: 1,                      // Personal space is single-user
			MaxTeamsCount:   0,
		}
	}
	// Organization space defaults
	return &SpaceQuotas{
		MaxNotebooks:    1000,
		MaxDocuments:    10000,
		MaxStorageBytes: 50 * 1024 * 1024 * 1024, // 50 GB
		MaxMembersCount: 100,
		MaxTeamsCount:   50,
	}
}

// ToFullResponse converts a Space to SpaceFullResponse with camelCase fields
func (s *Space) ToFullResponse() *SpaceFullResponse {
	return &SpaceFullResponse{
		ID:                s.ID,
		TenantID:          s.TenantID,
		AudimodalTenantID: s.AudimodalTenantID,
		DeeplakeNamespace: s.DeeplakeNamespace,
		Name:              s.Name,
		Description:       s.Description,
		Type:              s.Type,
		Visibility:        s.Visibility,
		OwnerID:           s.OwnerID,
		OwnerType:         s.OwnerType,
		Status:            s.Status,
		Quotas:            s.Quotas,
		Settings:          s.Settings,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
		DeletedAt:         s.DeletedAt,
	}
}

// ToSpaceInfo converts a Space to SpaceInfo for listings
func (s *Space) ToSpaceInfo() *SpaceInfo {
	return &SpaceInfo{
		SpaceType: s.Type,
		SpaceID:   s.ID,
		SpaceName: s.Name,
		TenantID:  s.TenantID,
	}
}

// Update updates space fields from an update request
func (s *Space) Update(req SpaceUpdateRequest) {
	if req.Name != nil {
		s.Name = *req.Name
	}
	if req.Description != nil {
		s.Description = *req.Description
	}
	if req.Visibility != nil {
		s.Visibility = *req.Visibility
	}
	s.UpdatedAt = time.Now()
}

// SoftDelete marks the space as deleted
func (s *Space) SoftDelete(deletedBy string) {
	now := time.Now()
	s.Status = SpaceStatusDeleted
	s.DeletedAt = &now
	s.DeletedBy = deletedBy
	s.UpdatedAt = now
}

// Restore restores a soft-deleted space
func (s *Space) Restore() {
	s.Status = SpaceStatusActive
	s.DeletedAt = nil
	s.DeletedBy = ""
	s.UpdatedAt = time.Now()
}

// Suspend suspends the space
func (s *Space) Suspend() {
	s.Status = SpaceStatusSuspended
	s.UpdatedAt = time.Now()
}

// IsActive returns true if the space is active
func (s *Space) IsActive() bool {
	return s.Status == SpaceStatusActive
}

// IsDeleted returns true if the space is soft-deleted
func (s *Space) IsDeleted() bool {
	return s.Status == SpaceStatusDeleted
}

// IsSuspended returns true if the space is suspended
func (s *Space) IsSuspended() bool {
	return s.Status == SpaceStatusSuspended
}

// IsPersonal returns true if this is a personal space
func (s *Space) IsPersonal() bool {
	return s.Type == SpaceTypePersonal
}

// IsOrganization returns true if this is an organization space
func (s *Space) IsOrganization() bool {
	return s.Type == SpaceTypeOrganization
}

// CanUserModify checks if a user role can modify the space
func (s *Space) CanUserModify(userRole string) bool {
	return userRole == "owner" || userRole == "admin"
}

// CanUserDelete checks if a user role can delete the space
func (s *Space) CanUserDelete(userRole string) bool {
	return userRole == "owner"
}

// CanUserInvite checks if a user role can invite members to the space
func (s *Space) CanUserInvite(userRole string) bool {
	return userRole == "owner" || userRole == "admin"
}

// HasTenant checks if the space has cross-service tenant configuration
func (s *Space) HasTenant() bool {
	return s.TenantID != ""
}

// HasAudimodalTenant checks if AudiModal tenant is configured
func (s *Space) HasAudimodalTenant() bool {
	return s.AudimodalTenantID != ""
}

// SetAudimodalTenant sets the AudiModal tenant ID
func (s *Space) SetAudimodalTenant(tenantID string) {
	s.AudimodalTenantID = tenantID
	s.UpdatedAt = time.Now()
}

// SetDeeplakeCredentials sets DeepLake credentials
func (s *Space) SetDeeplakeCredentials(namespace, apiKey string) {
	s.DeeplakeNamespace = namespace
	s.DeeplakeAPIKey = apiKey
	s.UpdatedAt = time.Now()
}

// GetTenantInfo returns tenant information for cross-service integration
func (s *Space) GetTenantInfo() map[string]interface{} {
	return map[string]interface{}{
		"tenant_id":           s.TenantID,
		"audimodal_tenant_id": s.AudimodalTenantID,
		"deeplake_namespace":  s.DeeplakeNamespace,
		"space_type":          s.Type,
		"space_name":          s.Name,
	}
}

// UpdateQuotaUsage updates the quota usage statistics
func (s *Space) UpdateQuotaUsage(notebooks, documents int, storageBytes int64) {
	if s.Quotas == nil {
		s.Quotas = DefaultSpaceQuotas(s.Type)
	}
	s.Quotas.UsedNotebooks = notebooks
	s.Quotas.UsedDocuments = documents
	s.Quotas.UsedStorageBytes = storageBytes
	s.UpdatedAt = time.Now()
}

// IsWithinQuota checks if the space is within its resource quotas
func (s *Space) IsWithinQuota() bool {
	if s.Quotas == nil {
		return true
	}
	return s.Quotas.UsedNotebooks < s.Quotas.MaxNotebooks &&
		s.Quotas.UsedDocuments < s.Quotas.MaxDocuments &&
		s.Quotas.UsedStorageBytes < s.Quotas.MaxStorageBytes
}

// CanAddNotebook checks if a notebook can be added within quota
func (s *Space) CanAddNotebook() bool {
	if s.Quotas == nil {
		return true
	}
	return s.Quotas.UsedNotebooks < s.Quotas.MaxNotebooks
}

// CanAddDocument checks if a document can be added within quota
func (s *Space) CanAddDocument() bool {
	if s.Quotas == nil {
		return true
	}
	return s.Quotas.UsedDocuments < s.Quotas.MaxDocuments
}

// CanAddStorage checks if storage can be added within quota
func (s *Space) CanAddStorage(additionalBytes int64) bool {
	if s.Quotas == nil {
		return true
	}
	return s.Quotas.UsedStorageBytes+additionalBytes <= s.Quotas.MaxStorageBytes
}

// ToMemberResponse converts SpaceMemberDTO to SpaceMemberResponse
func (m *SpaceMemberDTO) ToMemberResponse() *SpaceMemberResponse {
	return &SpaceMemberResponse{
		UserID:      m.UserID,
		SpaceID:     m.SpaceID,
		Role:        m.Role,
		Permissions: m.Permissions,
		JoinedAt:    m.JoinedAt,
		InvitedBy:   m.InvitedBy,
		UserName:    m.UserName,
		UserEmail:   m.UserEmail,
		Avatar:      m.Avatar,
	}
}

// Permission hierarchy for role-based access
var PermissionLevels = map[string]int{
	"viewer": 1,
	"member": 2,
	"admin":  3,
	"owner":  4,
}

// HasPermissionLevel checks if a role has at least the required permission level
func HasPermissionLevel(granted, required string) bool {
	grantedLevel, grantedOK := PermissionLevels[granted]
	requiredLevel, requiredOK := PermissionLevels[required]
	if !grantedOK || !requiredOK {
		return false
	}
	return grantedLevel >= requiredLevel
}

// DefaultSpaceSettings returns default settings for a new space
func DefaultSpaceSettings(spaceType SpaceType) map[string]interface{} {
	if spaceType == SpaceTypePersonal {
		return map[string]interface{}{
			"allowSharing":     true,
			"defaultNotebookVisibility": "private",
		}
	}
	// Organization space defaults
	return map[string]interface{}{
		"membersCanCreateNotebooks":    true,
		"membersCanInvite":             false,
		"defaultNotebookVisibility":    "private",
		"allowExternalSharing":         false,
		"requireApprovalForJoining":    true,
		"twoFactorRequired":            false,
	}
}
