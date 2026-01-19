package models

import (
	"time"

	"github.com/google/uuid"
)

// Notebook represents a notebook in the system
type Notebook struct {
	ID          string `json:"id" validate:"required,uuid"`
	Name        string `json:"name" validate:"required,min=1,max=255"`
	Description string `json:"description,omitempty" validate:"max=1000"`
	Visibility  string `json:"visibility" validate:"required,oneof=private shared public"`
	Status      string `json:"status" validate:"required,oneof=active archived deleted"`

	// Owner information
	OwnerID string `json:"owner_id" validate:"required,uuid"`

	// Space and tenant information
	SpaceType SpaceType `json:"space_type" validate:"required,oneof=personal organization"`
	SpaceID   string    `json:"space_id" validate:"required"`
	TenantID  string    `json:"tenant_id" validate:"required"`

	// Parent notebook ID (for hierarchical structure)
	ParentID string `json:"parent_id,omitempty" validate:"omitempty,uuid"`

	// Team assignment (for organization spaces)
	TeamID string `json:"team_id,omitempty" validate:"omitempty,uuid"`

	// Compliance settings
	ComplianceSettings map[string]interface{} `json:"compliance_settings,omitempty" validate:"omitempty,neo4j_compatible"`

	// Metadata
	DocumentCount  int      `json:"document_count"`
	TotalSizeBytes int64    `json:"total_size_bytes"`
	Tags           []string `json:"tags,omitempty"`
	SearchText     string   `json:"search_text,omitempty"` // Combined searchable text

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NotebookCreateRequest represents a request to create a notebook
type NotebookCreateRequest struct {
	Name               string                 `json:"name" validate:"required,safe_string,min=1,max=255"`
	Description        string                 `json:"description,omitempty" validate:"safe_string,max=1000"`
	Visibility         string                 `json:"visibility" validate:"required,notebook_visibility"`
	ParentID           string                 `json:"parent_id,omitempty" validate:"omitempty,uuid"`
	TeamID             string                 `json:"team_id,omitempty" validate:"omitempty,uuid"`
	ComplianceSettings map[string]interface{} `json:"compliance_settings,omitempty" validate:"omitempty,neo4j_compatible"`
	Tags               []string               `json:"tags,omitempty" validate:"dive,tag,min=1,max=50"`
}

// NotebookUpdateRequest represents a request to update a notebook
type NotebookUpdateRequest struct {
	Name               *string                `json:"name,omitempty" validate:"omitempty,safe_string,min=1,max=255"`
	Description        *string                `json:"description,omitempty" validate:"omitempty,safe_string,max=1000"`
	Visibility         *string                `json:"visibility,omitempty" validate:"omitempty,notebook_visibility"`
	Status             *string                `json:"status,omitempty" validate:"omitempty,oneof=active archived deleted"`
	ComplianceSettings map[string]interface{} `json:"compliance_settings,omitempty" validate:"omitempty,neo4j_compatible"`
	Tags               []string               `json:"tags,omitempty" validate:"dive,tag,min=1,max=50"`
}

// NotebookResponse represents a notebook response
type NotebookResponse struct {
	ID                 string                 `json:"id"`
	Name               string                 `json:"name"`
	Description        string                 `json:"description,omitempty"`
	Visibility         string                 `json:"visibility"`
	Status             string                 `json:"status"`
	OwnerID            string                 `json:"ownerId"`
	ParentID           string                 `json:"parentId,omitempty"`
	ComplianceSettings map[string]interface{} `json:"complianceSettings,omitempty" validate:"omitempty,neo4j_compatible"`
	DocumentCount      int                    `json:"documentCount"`
	TotalSizeBytes     int64                  `json:"totalSizeBytes"`
	Tags               []string               `json:"tags,omitempty"`
	CreatedAt          time.Time              `json:"createdAt"`
	UpdatedAt          time.Time              `json:"updatedAt"`

	// Optional fields for detailed responses
	Owner    *PublicUserResponse `json:"owner,omitempty"`
	Children []*NotebookResponse `json:"children,omitempty"`
	Parent   *NotebookResponse   `json:"parent,omitempty"`
}

// NotebookListResponse represents a paginated list of notebooks
type NotebookListResponse struct {
	Notebooks []*NotebookResponse `json:"notebooks"`
	Total     int                 `json:"total"`
	Limit     int                 `json:"limit"`
	Offset    int                 `json:"offset"`
	HasMore   bool                `json:"hasMore"`
}

// NotebookSearchRequest represents a notebook search request
type NotebookSearchRequest struct {
	Query      string   `json:"query,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	OwnerID    string   `json:"owner_id,omitempty" validate:"omitempty,uuid"`
	Visibility string   `json:"visibility,omitempty" validate:"omitempty,notebook_visibility"`
	Status     string   `json:"status,omitempty" validate:"omitempty,oneof=active archived deleted"`
	Tags       []string `json:"tags,omitempty" validate:"dive,tag,min=1,max=50"`
	Limit      int      `json:"limit,omitempty" validate:"omitempty,min=1,max=100"`
	Offset     int      `json:"offset,omitempty" validate:"omitempty,min=0"`
}

// NotebookShareRequest represents a request to share a notebook
type NotebookShareRequest struct {
	UserIDs     []string `json:"user_ids,omitempty" validate:"dive,uuid"`
	GroupIDs    []string `json:"group_ids,omitempty"`
	Permissions []string `json:"permissions" validate:"required,dive,oneof=read write admin"`
}

// NotebookPermission represents notebook permissions
type NotebookPermission struct {
	NotebookID string    `json:"notebook_id"`
	UserID     string    `json:"user_id,omitempty"`
	GroupID    string    `json:"group_id,omitempty"`
	Permission string    `json:"permission" validate:"oneof=read write admin"`
	GrantedBy  string    `json:"granted_by"`
	GrantedAt  time.Time `json:"granted_at"`
}

// NotebookActivity represents notebook activity
type NotebookActivity struct {
	ID         string                 `json:"id"`
	NotebookID string                 `json:"notebook_id"`
	UserID     string                 `json:"user_id"`
	Action     string                 `json:"action"`
	Details    map[string]interface{} `json:"details,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
}

// NotebookStats represents notebook statistics
type NotebookStats struct {
	TotalNotebooks    int `json:"total_notebooks"`
	ActiveNotebooks   int `json:"active_notebooks"`
	ArchivedNotebooks int `json:"archived_notebooks"`
	PublicNotebooks   int `json:"public_notebooks"`
	SharedNotebooks   int `json:"shared_notebooks"`
	PrivateNotebooks  int `json:"private_notebooks"`
}

// NewNotebook creates a new notebook with default values
func NewNotebook(req NotebookCreateRequest, ownerID string, spaceCtx *SpaceContext) *Notebook {
	now := time.Now()
	return &Notebook{
		ID:                 uuid.New().String(),
		Name:               req.Name,
		Description:        req.Description,
		Visibility:         req.Visibility,
		Status:             "active",
		OwnerID:            ownerID,
		SpaceType:          spaceCtx.SpaceType,
		SpaceID:            spaceCtx.SpaceID,
		TenantID:           spaceCtx.TenantID,
		ParentID:           req.ParentID,
		TeamID:             req.TeamID,
		ComplianceSettings: req.ComplianceSettings,
		Tags:               req.Tags,
		SearchText:         buildNotebookSearchText(req.Name, req.Description, req.Tags),
		DocumentCount:      0,
		TotalSizeBytes:     0,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

// ToResponse converts a Notebook to NotebookResponse
func (n *Notebook) ToResponse() *NotebookResponse {
	return &NotebookResponse{
		ID:                 n.ID,
		Name:               n.Name,
		Description:        n.Description,
		Visibility:         n.Visibility,
		Status:             n.Status,
		OwnerID:            n.OwnerID,
		ParentID:           n.ParentID,
		ComplianceSettings: n.ComplianceSettings,
		DocumentCount:      n.DocumentCount,
		TotalSizeBytes:     n.TotalSizeBytes,
		Tags:               n.Tags,
		CreatedAt:          n.CreatedAt,
		UpdatedAt:          n.UpdatedAt,
	}
}

// Update updates notebook fields from an update request
func (n *Notebook) Update(req NotebookUpdateRequest) {
	if req.Name != nil {
		n.Name = *req.Name
	}
	if req.Description != nil {
		n.Description = *req.Description
	}
	if req.Visibility != nil {
		n.Visibility = *req.Visibility
	}
	if req.Status != nil {
		n.Status = *req.Status
	}
	if req.ComplianceSettings != nil {
		n.ComplianceSettings = req.ComplianceSettings
	}
	if req.Tags != nil {
		n.Tags = req.Tags
	}

	// Update search text
	n.SearchText = buildNotebookSearchText(n.Name, n.Description, n.Tags)
	n.UpdatedAt = time.Now()
}

// IsActive returns true if the notebook is active
func (n *Notebook) IsActive() bool {
	return n.Status == "active"
}

// IsPublic returns true if the notebook is public
func (n *Notebook) IsPublic() bool {
	return n.Visibility == "public"
}

// IsShared returns true if the notebook is shared
func (n *Notebook) IsShared() bool {
	return n.Visibility == "shared"
}

// IsPrivate returns true if the notebook is private
func (n *Notebook) IsPrivate() bool {
	return n.Visibility == "private"
}

// CanBeAccessedBy checks if a user can access this notebook
func (n *Notebook) CanBeAccessedBy(userID string) bool {
	// Owner can always access
	if n.OwnerID == userID {
		return true
	}

	// Public notebooks can be accessed by anyone
	if n.IsPublic() {
		return true
	}

	// For shared notebooks, need to check permissions separately
	return false
}

// UpdateDocumentCount updates the document count and total size
func (n *Notebook) UpdateDocumentCount(documentCount int, totalSizeBytes int64) {
	n.DocumentCount = documentCount
	n.TotalSizeBytes = totalSizeBytes
	n.UpdatedAt = time.Now()
}

// AddTag adds a tag to the notebook
func (n *Notebook) AddTag(tag string) {
	// Check if tag already exists
	for _, existingTag := range n.Tags {
		if existingTag == tag {
			return
		}
	}

	n.Tags = append(n.Tags, tag)
	n.SearchText = buildNotebookSearchText(n.Name, n.Description, n.Tags)
	n.UpdatedAt = time.Now()
}

// RemoveTag removes a tag from the notebook
func (n *Notebook) RemoveTag(tag string) {
	for i, existingTag := range n.Tags {
		if existingTag == tag {
			n.Tags = append(n.Tags[:i], n.Tags[i+1:]...)
			break
		}
	}

	n.SearchText = buildNotebookSearchText(n.Name, n.Description, n.Tags)
	n.UpdatedAt = time.Now()
}

// HasTag checks if the notebook has a specific tag
func (n *Notebook) HasTag(tag string) bool {
	for _, existingTag := range n.Tags {
		if existingTag == tag {
			return true
		}
	}
	return false
}

// buildNotebookSearchText creates a searchable text field from name, description, and tags
func buildNotebookSearchText(name, description string, tags []string) string {
	searchText := name
	if description != "" {
		searchText += " " + description
	}
	if len(tags) > 0 {
		for _, tag := range tags {
			searchText += " " + tag
		}
	}
	return searchText
}
