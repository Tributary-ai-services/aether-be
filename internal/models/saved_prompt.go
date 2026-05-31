package models

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// PromptVisibility represents the visibility level of a saved prompt
type PromptVisibility string

const (
	PromptVisibilityPrivate PromptVisibility = "private"
	PromptVisibilityShared  PromptVisibility = "shared"
	PromptVisibilityPublic  PromptVisibility = "public"
)

// SavedPrompt represents a saved prompt configuration for the AI Playground
type SavedPrompt struct {
	// Core identity
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	UserPrompt   string `json:"user_prompt"`

	// Default model configuration
	DefaultModels []ModelConfig `json:"default_models,omitempty"`

	// Multi-tenancy
	TenantID string `json:"tenant_id"`
	SpaceID  string `json:"space_id"`
	OwnerID  string `json:"owner_id"`

	// Sharing
	Visibility      PromptVisibility `json:"visibility"`
	SharedWithTeams []string         `json:"shared_with_teams,omitempty"`

	// Organization
	Tags   []string `json:"tags,omitempty"`
	Folder string   `json:"folder,omitempty"`

	// Usage stats
	UseCount   int        `json:"use_count"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	LastUsedBy string     `json:"last_used_by,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SavedPromptCreateRequest represents the request to create a saved prompt
type SavedPromptCreateRequest struct {
	Name          string           `json:"name" validate:"required,min=1,max=255"`
	Description   string           `json:"description,omitempty" validate:"max=2000"`
	SystemPrompt  string           `json:"system_prompt,omitempty" validate:"max=50000"`
	UserPrompt    string           `json:"user_prompt" validate:"required,min=1,max=50000"`
	DefaultModels []ModelConfig    `json:"default_models,omitempty" validate:"max=4"`
	Visibility    PromptVisibility `json:"visibility,omitempty" validate:"omitempty,oneof=private shared public"`
	Tags          []string         `json:"tags,omitempty" validate:"max=20"`
	Folder        string           `json:"folder,omitempty" validate:"max=255"`
}

// SavedPromptUpdateRequest represents the request to update a saved prompt
type SavedPromptUpdateRequest struct {
	Name            *string           `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Description     *string           `json:"description,omitempty" validate:"omitempty,max=2000"`
	SystemPrompt    *string           `json:"system_prompt,omitempty" validate:"omitempty,max=50000"`
	UserPrompt      *string           `json:"user_prompt,omitempty" validate:"omitempty,min=1,max=50000"`
	DefaultModels   []ModelConfig     `json:"default_models,omitempty" validate:"max=4"`
	Visibility      *PromptVisibility `json:"visibility,omitempty" validate:"omitempty,oneof=private shared public"`
	SharedWithTeams []string          `json:"shared_with_teams,omitempty"`
	Tags            []string          `json:"tags,omitempty" validate:"max=20"`
	Folder          *string           `json:"folder,omitempty" validate:"omitempty,max=255"`
}

// SavedPromptResponse represents the API response for a saved prompt
type SavedPromptResponse struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Description     string           `json:"description,omitempty"`
	SystemPrompt    string           `json:"system_prompt,omitempty"`
	UserPrompt      string           `json:"user_prompt"`
	DefaultModels   []ModelConfig    `json:"default_models,omitempty"`
	TenantID        string           `json:"tenant_id"`
	SpaceID         string           `json:"space_id"`
	OwnerID         string           `json:"owner_id"`
	Visibility      PromptVisibility `json:"visibility"`
	SharedWithTeams []string         `json:"shared_with_teams,omitempty"`
	Tags            []string         `json:"tags,omitempty"`
	Folder          string           `json:"folder,omitempty"`
	UseCount        int              `json:"use_count"`
	LastUsedAt      *time.Time       `json:"last_used_at,omitempty"`
	LastUsedBy      string           `json:"last_used_by,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// SavedPromptListResponse represents a paginated list of saved prompts
type SavedPromptListResponse struct {
	Prompts    []SavedPromptResponse `json:"prompts"`
	TotalCount int                   `json:"total_count"`
	Page       int                   `json:"page"`
	PageSize   int                   `json:"page_size"`
	TotalPages int                   `json:"total_pages"`
}

// SavedPromptSearchRequest represents search/filter parameters for saved prompts
type SavedPromptSearchRequest struct {
	Search     string           `json:"search,omitempty"`
	Visibility PromptVisibility `json:"visibility,omitempty"`
	Folder     string           `json:"folder,omitempty"`
	Tags       []string         `json:"tags,omitempty"`
	OwnerID    string           `json:"owner_id,omitempty"`
	Page       int              `json:"page,omitempty"`
	PageSize   int              `json:"page_size,omitempty"`
	SortBy     string           `json:"sort_by,omitempty"`
	SortOrder  string           `json:"sort_order,omitempty"`
}

// NewSavedPrompt creates a new SavedPrompt with defaults
func NewSavedPrompt(ownerID, tenantID, spaceID string, req SavedPromptCreateRequest) *SavedPrompt {
	now := time.Now()
	visibility := req.Visibility
	if visibility == "" {
		visibility = PromptVisibilityPrivate
	}

	return &SavedPrompt{
		ID:            uuid.New().String(),
		Name:          req.Name,
		Description:   req.Description,
		SystemPrompt:  req.SystemPrompt,
		UserPrompt:    req.UserPrompt,
		DefaultModels: req.DefaultModels,
		TenantID:      tenantID,
		SpaceID:       spaceID,
		OwnerID:       ownerID,
		Visibility:    visibility,
		Tags:          req.Tags,
		Folder:        req.Folder,
		UseCount:      0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// ToResponse converts a SavedPrompt to its API response format
func (p *SavedPrompt) ToResponse() *SavedPromptResponse {
	return &SavedPromptResponse{
		ID:              p.ID,
		Name:            p.Name,
		Description:     p.Description,
		SystemPrompt:    p.SystemPrompt,
		UserPrompt:      p.UserPrompt,
		DefaultModels:   p.DefaultModels,
		TenantID:        p.TenantID,
		SpaceID:         p.SpaceID,
		OwnerID:         p.OwnerID,
		Visibility:      p.Visibility,
		SharedWithTeams: p.SharedWithTeams,
		Tags:            p.Tags,
		Folder:          p.Folder,
		UseCount:        p.UseCount,
		LastUsedAt:      p.LastUsedAt,
		LastUsedBy:      p.LastUsedBy,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

// Update applies updates from an UpdateRequest to the SavedPrompt
func (p *SavedPrompt) Update(req SavedPromptUpdateRequest) {
	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if req.SystemPrompt != nil {
		p.SystemPrompt = *req.SystemPrompt
	}
	if req.UserPrompt != nil {
		p.UserPrompt = *req.UserPrompt
	}
	if req.DefaultModels != nil {
		p.DefaultModels = req.DefaultModels
	}
	if req.Visibility != nil {
		p.Visibility = *req.Visibility
	}
	if req.SharedWithTeams != nil {
		p.SharedWithTeams = req.SharedWithTeams
	}
	if req.Tags != nil {
		p.Tags = req.Tags
	}
	if req.Folder != nil {
		p.Folder = *req.Folder
	}
	p.UpdatedAt = time.Now()
}

// RecordUsage records a usage of the prompt
func (p *SavedPrompt) RecordUsage(userID string) {
	p.UseCount++
	now := time.Now()
	p.LastUsedAt = &now
	p.LastUsedBy = userID
}

// IsAccessibleBy checks if a user can access this prompt
func (p *SavedPrompt) IsAccessibleBy(userID, spaceID string, userTeams []string) bool {
	// Owner always has access
	if p.OwnerID == userID {
		return true
	}

	// Must be in the same space
	if p.SpaceID != spaceID {
		return false
	}

	// Check visibility
	switch p.Visibility {
	case PromptVisibilityPublic:
		return true
	case PromptVisibilityShared:
		// Check if user is in any of the shared teams
		for _, teamID := range p.SharedWithTeams {
			if slices.Contains(userTeams, teamID) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// CanBeModifiedBy checks if a user can modify this prompt
func (p *SavedPrompt) CanBeModifiedBy(userID string) bool {
	return p.OwnerID == userID
}
