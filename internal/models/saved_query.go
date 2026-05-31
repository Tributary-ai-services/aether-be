package models

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
)

// QueryVisibility represents the visibility level of a saved query
type QueryVisibility string

const (
	QueryVisibilityPrivate QueryVisibility = "private"
	QueryVisibilityShared  QueryVisibility = "shared"
	QueryVisibilityPublic  QueryVisibility = "public"
)

// SavedQuery represents a saved database query
type SavedQuery struct {
	// Core identity
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Query       string `json:"query"`

	// Database reference
	DatabaseID   string `json:"database_id"`
	DatabaseName string `json:"database_name,omitempty"` // Denormalized for display

	// Multi-tenancy
	TenantID string `json:"tenant_id"`
	SpaceID  string `json:"space_id"`
	OwnerID  string `json:"owner_id"`

	// Sharing
	Visibility      QueryVisibility `json:"visibility"`
	SharedWithTeams []string        `json:"shared_with_teams,omitempty"`

	// Organization
	Tags   []string `json:"tags,omitempty"`
	Folder string   `json:"folder,omitempty"`

	// Execution stats
	LastExecutedAt *time.Time `json:"last_executed_at,omitempty"`
	LastExecutedBy string     `json:"last_executed_by,omitempty"`
	ExecutionCount int        `json:"execution_count"`
	AvgDurationMs  int64      `json:"avg_duration_ms,omitempty"`
	LastRowCount   int        `json:"last_row_count,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SavedQueryCreateRequest represents the request to create a saved query
type SavedQueryCreateRequest struct {
	Name            string          `json:"name" validate:"required,min=1,max=255"`
	Description     string          `json:"description,omitempty" validate:"omitempty,max=2000"`
	Query           string          `json:"query" validate:"required,min=1,max=100000"`
	DatabaseID      string          `json:"database_id" validate:"required,uuid"`
	Visibility      QueryVisibility `json:"visibility,omitempty" validate:"omitempty,oneof=private shared public"`
	SharedWithTeams []string        `json:"shared_with_teams,omitempty"`
	Tags            []string        `json:"tags,omitempty"`
	Folder          string          `json:"folder,omitempty" validate:"omitempty,max=255"`
}

// SavedQueryUpdateRequest represents the request to update a saved query
type SavedQueryUpdateRequest struct {
	Name            *string          `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Description     *string          `json:"description,omitempty" validate:"omitempty,max=2000"`
	Query           *string          `json:"query,omitempty" validate:"omitempty,min=1,max=100000"`
	DatabaseID      *string          `json:"database_id,omitempty" validate:"omitempty,uuid"`
	Visibility      *QueryVisibility `json:"visibility,omitempty" validate:"omitempty,oneof=private shared public"`
	SharedWithTeams []string         `json:"shared_with_teams,omitempty"`
	Tags            []string         `json:"tags,omitempty"`
	Folder          *string          `json:"folder,omitempty" validate:"omitempty,max=255"`
}

// SavedQueryResponse represents the API response for a saved query
type SavedQueryResponse struct {
	SavedQuery
	OwnerName string `json:"owner_name,omitempty"`
}

// SavedQueryListResponse represents a paginated list of saved queries
type SavedQueryListResponse struct {
	Queries  []SavedQueryResponse `json:"queries"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

// SavedQueryExecuteRequest represents a request to execute a saved query
type SavedQueryExecuteRequest struct {
	Parameters []any `json:"parameters,omitempty"`
}

// SavedQueryExecuteResponse represents the result of executing a saved query
type SavedQueryExecuteResponse struct {
	QueryID    string           `json:"query_id"`
	QueryName  string           `json:"query_name"`
	Columns    []string         `json:"columns"`
	Rows       []map[string]any `json:"rows"`
	RowCount   int              `json:"row_count"`
	Truncated  bool             `json:"truncated"`
	Duration   int64            `json:"duration_ms"`
	ExecutedAt time.Time        `json:"executed_at"`
}

// SavedQuerySearchRequest represents search/filter criteria for queries
type SavedQuerySearchRequest struct {
	Search     string          `json:"search,omitempty"`
	DatabaseID string          `json:"database_id,omitempty"`
	Visibility QueryVisibility `json:"visibility,omitempty"`
	Tags       []string        `json:"tags,omitempty"`
	Folder     string          `json:"folder,omitempty"`
	OwnerID    string          `json:"owner_id,omitempty"`
	Page       int             `json:"page,omitempty"`
	PageSize   int             `json:"page_size,omitempty"`
	SortBy     string          `json:"sort_by,omitempty" validate:"omitempty,oneof=name created_at updated_at execution_count last_executed_at"`
	SortOrder  string          `json:"sort_order,omitempty" validate:"omitempty,oneof=asc desc"`
}

// NewSavedQuery creates a new SavedQuery from a create request
func NewSavedQuery(req SavedQueryCreateRequest, ownerID, tenantID, spaceID string) *SavedQuery {
	now := time.Now()
	queryID := uuid.New().String()

	visibility := req.Visibility
	if visibility == "" {
		visibility = QueryVisibilityPrivate
	}

	return &SavedQuery{
		ID:              queryID,
		Name:            req.Name,
		Description:     req.Description,
		Query:           req.Query,
		DatabaseID:      req.DatabaseID,
		TenantID:        tenantID,
		SpaceID:         spaceID,
		OwnerID:         ownerID,
		Visibility:      visibility,
		SharedWithTeams: req.SharedWithTeams,
		Tags:            req.Tags,
		Folder:          req.Folder,
		ExecutionCount:  0,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// ToResponse converts a SavedQuery to SavedQueryResponse
func (q *SavedQuery) ToResponse() *SavedQueryResponse {
	return &SavedQueryResponse{
		SavedQuery: *q,
	}
}

// ToResponseWithOwner converts a SavedQuery to SavedQueryResponse with owner name
func (q *SavedQuery) ToResponseWithOwner(ownerName string) *SavedQueryResponse {
	return &SavedQueryResponse{
		SavedQuery: *q,
		OwnerName:  ownerName,
	}
}

// Update applies updates from a SavedQueryUpdateRequest
func (q *SavedQuery) Update(req SavedQueryUpdateRequest) {
	if req.Name != nil {
		q.Name = *req.Name
	}
	if req.Description != nil {
		q.Description = *req.Description
	}
	if req.Query != nil {
		q.Query = *req.Query
	}
	if req.DatabaseID != nil {
		q.DatabaseID = *req.DatabaseID
	}
	if req.Visibility != nil {
		q.Visibility = *req.Visibility
	}
	if req.SharedWithTeams != nil {
		q.SharedWithTeams = req.SharedWithTeams
	}
	if req.Tags != nil {
		q.Tags = req.Tags
	}
	if req.Folder != nil {
		q.Folder = *req.Folder
	}
	q.UpdatedAt = time.Now()
}

// RecordExecution updates execution statistics
func (q *SavedQuery) RecordExecution(userID string, durationMs int64, rowCount int) {
	now := time.Now()
	q.LastExecutedAt = &now
	q.LastExecutedBy = userID
	q.ExecutionCount++
	q.LastRowCount = rowCount

	// Update average duration (weighted average)
	if q.AvgDurationMs == 0 {
		q.AvgDurationMs = durationMs
	} else {
		// Simple moving average
		q.AvgDurationMs = (q.AvgDurationMs*(int64(q.ExecutionCount)-1) + durationMs) / int64(q.ExecutionCount)
	}
	q.UpdatedAt = now
}

// IsAccessibleBy checks if a user can access this query
func (q *SavedQuery) IsAccessibleBy(userID, spaceID string, userTeams []string) bool {
	// Owner always has access
	if q.OwnerID == userID {
		return true
	}

	// Must be in same space
	if q.SpaceID != spaceID {
		return false
	}

	// Check visibility
	switch q.Visibility {
	case QueryVisibilityPublic:
		return true
	case QueryVisibilityShared:
		// Check if user is in any of the shared teams
		for _, teamID := range q.SharedWithTeams {
			if slices.Contains(userTeams, teamID) {
				return true
			}
		}
		return false
	case QueryVisibilityPrivate:
		return false
	default:
		return false
	}
}

// CanBeModifiedBy checks if a user can modify this query
func (q *SavedQuery) CanBeModifiedBy(userID string) bool {
	return q.OwnerID == userID
}

// GetTagsJSON returns tags as a JSON string for storage
func (q *SavedQuery) GetTagsJSON() string {
	if len(q.Tags) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(q.Tags)
	return string(data)
}

// SetTagsFromJSON parses tags from a JSON string
func (q *SavedQuery) SetTagsFromJSON(jsonStr string) {
	if jsonStr == "" || jsonStr == "[]" {
		q.Tags = nil
		return
	}
	json.Unmarshal([]byte(jsonStr), &q.Tags)
}

// GetSharedWithTeamsJSON returns shared teams as a JSON string for storage
func (q *SavedQuery) GetSharedWithTeamsJSON() string {
	if len(q.SharedWithTeams) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(q.SharedWithTeams)
	return string(data)
}

// SetSharedWithTeamsFromJSON parses shared teams from a JSON string
func (q *SavedQuery) SetSharedWithTeamsFromJSON(jsonStr string) {
	if jsonStr == "" || jsonStr == "[]" {
		q.SharedWithTeams = nil
		return
	}
	json.Unmarshal([]byte(jsonStr), &q.SharedWithTeams)
}

// Duplicate creates a copy of this query with a new ID and owner
func (q *SavedQuery) Duplicate(newOwnerID, newName string) *SavedQuery {
	now := time.Now()
	return &SavedQuery{
		ID:             uuid.New().String(),
		Name:           newName,
		Description:    q.Description,
		Query:          q.Query,
		DatabaseID:     q.DatabaseID,
		TenantID:       q.TenantID,
		SpaceID:        q.SpaceID,
		OwnerID:        newOwnerID,
		Visibility:     QueryVisibilityPrivate, // Always start as private
		Tags:           q.Tags,
		Folder:         q.Folder,
		ExecutionCount: 0,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}
