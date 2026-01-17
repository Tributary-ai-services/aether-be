package models

import (
	"time"

	"github.com/google/uuid"
)

// AgentStatus represents the status of an agent
type AgentStatus string

const (
	AgentStatusDraft     AgentStatus = "draft"
	AgentStatusPublished AgentStatus = "published"
	AgentStatusDisabled  AgentStatus = "disabled"
)

// AgentType represents the type/behavior of an agent
type AgentType string

const (
	AgentTypeQA             AgentType = "qa"             // Question-Answer agent based on knowledge sources
	AgentTypeConversational AgentType = "conversational" // Chat-based agent with conversation memory
	AgentTypeProducer       AgentType = "producer"       // Content generation agent using templates
)

// Agent represents an agent in the Neo4j graph database
// This is metadata for relationship management - actual agent data lives in PostgreSQL via agent-builder
type Agent struct {
	// Neo4j node identifier (different from agent-builder ID)
	ID string `json:"id" validate:"required,uuid"`
	
	// Reference to agent-builder PostgreSQL record
	AgentBuilderID string `json:"agent_builder_id" validate:"required,uuid"`
	
	// Basic agent information (synced from agent-builder)
	Name        string      `json:"name" validate:"required,min=1,max=255"`
	Description string      `json:"description,omitempty" validate:"max=1000"`
	Status      AgentStatus `json:"status" validate:"required,oneof=draft published disabled"`
	Type        AgentType   `json:"type" validate:"required,oneof=qa conversational producer"`
	
	// Space and tenant context (follows existing aether-be patterns)
	OwnerID   string    `json:"owner_id" validate:"required,uuid"`
	SpaceType SpaceType `json:"space_type" validate:"required,oneof=personal organization"`
	SpaceID   string    `json:"space_id" validate:"required"`
	TenantID  string    `json:"tenant_id" validate:"required"`
	
	// Team assignment for organization spaces
	TeamID string `json:"team_id,omitempty" validate:"omitempty,uuid"`
	
	// Visibility and sharing
	IsPublic   bool `json:"is_public"`
	IsTemplate bool `json:"is_template"`
	
	// Metadata and search
	Tags       []string `json:"tags,omitempty"`
	SearchText string   `json:"search_text,omitempty"` // Combined searchable text
	
	// Statistics (synced from agent-builder)
	TotalExecutions    int        `json:"total_executions"`
	TotalCostUSD       float64    `json:"total_cost_usd"`
	AvgResponseTimeMs  int        `json:"avg_response_time_ms"`
	LastExecutedAt     *time.Time `json:"last_executed_at,omitempty"`
	
	// Audit trail
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	SyncedAt  *time.Time `json:"synced_at,omitempty"` // Last sync with agent-builder
}

// AgentCreateRequest represents a request to create an agent
type AgentCreateRequest struct {
	Name        string    `json:"name" validate:"required,safe_string,min=1,max=255"`
	Description string    `json:"description,omitempty" validate:"safe_string,max=1000"`
	Type        AgentType `json:"type" validate:"required,oneof=qa conversational producer"`
	SpaceID     string    `json:"space_id" validate:"required"`
	TeamID      string    `json:"team_id,omitempty" validate:"omitempty,uuid"`
	IsPublic    bool      `json:"is_public"`
	IsTemplate  bool      `json:"is_template"`
	Tags        []string  `json:"tags,omitempty" validate:"dive,tag,min=1,max=50"`
	
	// Agent-builder specific configuration (passed through)
	SystemPrompt string                 `json:"system_prompt" validate:"required,min=1"`
	LLMConfig    map[string]interface{} `json:"llm_config" validate:"required"`
}

// AgentUpdateRequest represents a request to update an agent
type AgentUpdateRequest struct {
	Name        *string    `json:"name,omitempty" validate:"omitempty,safe_string,min=1,max=255"`
	Description *string    `json:"description,omitempty" validate:"omitempty,safe_string,max=1000"`
	Type        *AgentType `json:"type,omitempty" validate:"omitempty,oneof=qa conversational producer"`
	Status      *string    `json:"status,omitempty" validate:"omitempty,oneof=draft published disabled"`
	IsPublic    *bool      `json:"is_public,omitempty"`
	IsTemplate  *bool      `json:"is_template,omitempty"`
	Tags        []string   `json:"tags,omitempty" validate:"dive,tag,min=1,max=50"`
	
	// Agent-builder specific updates (passed through)
	SystemPrompt *string                `json:"system_prompt,omitempty" validate:"omitempty,min=1"`
	LLMConfig    map[string]interface{} `json:"llm_config,omitempty"`
}

// AgentResponse represents an agent response with related data
type AgentResponse struct {
	ID             string      `json:"id"`
	AgentBuilderID string      `json:"agent_builder_id"`
	Name           string      `json:"name"`
	Description    string      `json:"description"`
	Status         AgentStatus `json:"status"`
	Type           AgentType   `json:"type"`
	OwnerID        string      `json:"owner_id"`
	SpaceType      SpaceType   `json:"space_type"`
	SpaceID        string      `json:"space_id"`
	TeamID         string      `json:"team_id,omitempty"`
	IsPublic       bool        `json:"is_public"`
	IsTemplate     bool        `json:"is_template"`
	Tags           []string    `json:"tags,omitempty"`
	
	// Statistics
	TotalExecutions   int        `json:"total_executions"`
	TotalCostUSD      float64    `json:"total_cost_usd"`
	AvgResponseTimeMs int        `json:"avg_response_time_ms"`
	LastExecutedAt    *time.Time `json:"last_executed_at,omitempty"`
	
	// Timestamps
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	SyncedAt  *time.Time `json:"synced_at,omitempty"`
	
	// Optional related data
	Owner            *PublicUserResponse `json:"owner,omitempty"`
	Team             *TeamResponse       `json:"team,omitempty"`
	KnowledgeSources []string            `json:"knowledge_sources,omitempty"` // Notebook IDs
	VectorSearchConfig *VectorSearchConfig `json:"vector_search_config,omitempty"`
}

// AgentListResponse represents a paginated list of agents
type AgentListResponse struct {
	Agents  []*AgentResponse `json:"agents"`
	Total   int              `json:"total"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
	HasMore bool             `json:"has_more"`
}

// AgentSearchRequest represents an agent search request
type AgentSearchRequest struct {
	Query      string      `json:"query,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	OwnerID    string      `json:"owner_id,omitempty" validate:"omitempty,uuid"`
	SpaceID    string      `json:"space_id,omitempty" validate:"omitempty"`
	TeamID     string      `json:"team_id,omitempty" validate:"omitempty,uuid"`
	Status     AgentStatus `json:"status,omitempty" validate:"omitempty,oneof=draft published disabled"`
	SpaceType  SpaceType   `json:"space_type,omitempty" validate:"omitempty,oneof=personal organization"`
	IsPublic   *bool       `json:"is_public,omitempty"`
	IsTemplate *bool       `json:"is_template,omitempty"`
	Tags       []string    `json:"tags,omitempty" validate:"dive,tag,min=1,max=50"`
	Limit      int         `json:"limit,omitempty" validate:"omitempty,min=1,max=100"`
	Offset     int         `json:"offset,omitempty" validate:"omitempty,min=0"`
}

// VectorSearchConfig represents agent's vector search configuration
type VectorSearchConfig struct {
	SearchSpaces []VectorSearchSpace `json:"search_spaces"`
	Strategy     string              `json:"strategy" validate:"oneof=semantic hybrid keyword"`
	MaxResults   int                 `json:"max_results" validate:"min=1,max=50"`
	Threshold    float64             `json:"threshold" validate:"min=0,max=1"`
}

// VectorSearchSpace represents a notebook/collection that an agent searches
type VectorSearchSpace struct {
	NotebookID       string                 `json:"notebook_id" validate:"required,uuid"`
	NotebookName     string                 `json:"notebook_name"`
	SearchWeight     float64                `json:"search_weight" validate:"min=0,max=1"`
	Filters          map[string]interface{} `json:"filters,omitempty"`
	AddedAt          time.Time              `json:"added_at"`
	AddedBy          string                 `json:"added_by"`
}

// AgentStats represents agent statistics
type AgentStats struct {
	TotalAgents      int `json:"total_agents"`
	PublishedAgents  int `json:"published_agents"`
	DraftAgents      int `json:"draft_agents"`
	DisabledAgents   int `json:"disabled_agents"`
	PublicAgents     int `json:"public_agents"`
	TemplateAgents   int `json:"template_agents"`
	PersonalAgents   int `json:"personal_agents"`
	OrganizationAgents int `json:"organization_agents"`
}

// NewAgent creates a new agent with default values
func NewAgent(req AgentCreateRequest, ownerID string, spaceCtx *SpaceContext) *Agent {
	now := time.Now()
	return &Agent{
		ID:             uuid.New().String(),
		AgentBuilderID: "", // Will be set after agent-builder creation
		Name:           req.Name,
		Description:    req.Description,
		Status:         AgentStatusDraft,
		Type:           req.Type,
		OwnerID:        ownerID,
		SpaceType:      spaceCtx.SpaceType,
		SpaceID:        spaceCtx.SpaceID,
		TenantID:       spaceCtx.TenantID,
		TeamID:         req.TeamID,
		IsPublic:       req.IsPublic,
		IsTemplate:     req.IsTemplate,
		Tags:           req.Tags,
		SearchText:     buildAgentSearchText(req.Name, req.Description, req.Tags),
		TotalExecutions: 0,
		TotalCostUSD:   0.0,
		AvgResponseTimeMs: 0,
		LastExecutedAt: nil,
		CreatedAt:      now,
		UpdatedAt:      now,
		SyncedAt:       nil,
	}
}

// ToResponse converts an Agent to AgentResponse
func (a *Agent) ToResponse() *AgentResponse {
	return &AgentResponse{
		ID:                a.ID,
		AgentBuilderID:    a.AgentBuilderID,
		Name:              a.Name,
		Description:       a.Description,
		Status:            a.Status,
		Type:              a.Type,
		OwnerID:           a.OwnerID,
		SpaceType:         a.SpaceType,
		SpaceID:           a.SpaceID,
		TeamID:            a.TeamID,
		IsPublic:          a.IsPublic,
		IsTemplate:        a.IsTemplate,
		Tags:              a.Tags,
		TotalExecutions:   a.TotalExecutions,
		TotalCostUSD:      a.TotalCostUSD,
		AvgResponseTimeMs: a.AvgResponseTimeMs,
		LastExecutedAt:    a.LastExecutedAt,
		CreatedAt:         a.CreatedAt,
		UpdatedAt:         a.UpdatedAt,
		SyncedAt:          a.SyncedAt,
	}
}

// Update updates agent fields from an update request
func (a *Agent) Update(req AgentUpdateRequest) {
	if req.Name != nil {
		a.Name = *req.Name
	}
	if req.Description != nil {
		a.Description = *req.Description
	}
	if req.Type != nil {
		a.Type = *req.Type
	}
	if req.Status != nil {
		a.Status = AgentStatus(*req.Status)
	}
	if req.IsPublic != nil {
		a.IsPublic = *req.IsPublic
	}
	if req.IsTemplate != nil {
		a.IsTemplate = *req.IsTemplate
	}
	if req.Tags != nil {
		a.Tags = req.Tags
	}
	
	// Update search text
	a.SearchText = buildAgentSearchText(a.Name, a.Description, a.Tags)
	a.UpdatedAt = time.Now()
}

// UpdateStats updates agent statistics from agent-builder data
func (a *Agent) UpdateStats(executions int, costUSD float64, avgResponseTime int, lastExecuted *time.Time) {
	a.TotalExecutions = executions
	a.TotalCostUSD = costUSD
	a.AvgResponseTimeMs = avgResponseTime
	a.LastExecutedAt = lastExecuted
	a.SyncedAt = &time.Time{}
	*a.SyncedAt = time.Now()
	a.UpdatedAt = time.Now()
}

// IsActive returns true if the agent is published
func (a *Agent) IsActive() bool {
	return a.Status == AgentStatusPublished
}

// IsDraft returns true if the agent is in draft status
func (a *Agent) IsDraft() bool {
	return a.Status == AgentStatusDraft
}

// IsDisabled returns true if the agent is disabled
func (a *Agent) IsDisabled() bool {
	return a.Status == AgentStatusDisabled
}

// IsPersonalAgent returns true if the agent belongs to a personal space
func (a *Agent) IsPersonalAgent() bool {
	return a.SpaceType == SpaceTypePersonal
}

// IsOrganizationAgent returns true if the agent belongs to an organization space
func (a *Agent) IsOrganizationAgent() bool {
	return a.SpaceType == SpaceTypeOrganization
}

// CanBeAccessedBy checks if a user can access this agent
func (a *Agent) CanBeAccessedBy(userID string, userTeams []string) bool {
	// Owner can always access
	if a.OwnerID == userID {
		return true
	}
	
	// Public agents can be accessed by anyone
	if a.IsPublic {
		return true
	}
	
	// For organization agents, check team membership
	if a.IsOrganizationAgent() && a.TeamID != "" {
		for _, teamID := range userTeams {
			if teamID == a.TeamID {
				return true
			}
		}
	}
	
	return false
}

// AddTag adds a tag to the agent
func (a *Agent) AddTag(tag string) {
	// Check if tag already exists
	for _, existingTag := range a.Tags {
		if existingTag == tag {
			return
		}
	}
	
	a.Tags = append(a.Tags, tag)
	a.SearchText = buildAgentSearchText(a.Name, a.Description, a.Tags)
	a.UpdatedAt = time.Now()
}

// RemoveTag removes a tag from the agent
func (a *Agent) RemoveTag(tag string) {
	for i, existingTag := range a.Tags {
		if existingTag == tag {
			a.Tags = append(a.Tags[:i], a.Tags[i+1:]...)
			break
		}
	}
	
	a.SearchText = buildAgentSearchText(a.Name, a.Description, a.Tags)
	a.UpdatedAt = time.Now()
}

// HasTag checks if the agent has a specific tag
func (a *Agent) HasTag(tag string) bool {
	for _, existingTag := range a.Tags {
		if existingTag == tag {
			return true
		}
	}
	return false
}

// buildAgentSearchText creates a searchable text field from name, description, and tags
func buildAgentSearchText(name, description string, tags []string) string {
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

// Agent Execution Models

// AgentExecuteRequest represents a request to execute an agent
type AgentExecuteRequest struct {
	// Common execution parameters
	Input   string                 `json:"input" validate:"required,min=1"`
	Context map[string]interface{} `json:"context,omitempty"`
	
	// Agent type-specific parameters
	// For Q&A agents
	MaxResults *int     `json:"max_results,omitempty" validate:"omitempty,min=1,max=50"`
	Sources    []string `json:"sources,omitempty" validate:"dive,uuid"` // Notebook IDs
	
	// For Conversational agents
	ConversationID *string                  `json:"conversation_id,omitempty" validate:"omitempty,uuid"`
	History        []ConversationMessage    `json:"history,omitempty"`
	
	// For Producer agents
	Template       *string                  `json:"template,omitempty"`
	TemplateParams map[string]interface{}   `json:"template_params,omitempty"`
	OutputFormat   *string                  `json:"output_format,omitempty" validate:"omitempty,oneof=text markdown html json"`
}

// ConversationMessage represents a message in a conversation
type ConversationMessage struct {
	Role      string                 `json:"role" validate:"required,oneof=user assistant system"`
	Content   string                 `json:"content" validate:"required"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// AgentExecuteResponse represents the response from agent execution
type AgentExecuteResponse struct {
	// Common response fields
	ExecutionID string                 `json:"execution_id"`
	AgentID     string                 `json:"agent_id"`
	AgentType   AgentType              `json:"agent_type"`
	Output      string                 `json:"output"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	
	// Execution statistics
	TokensUsed     int     `json:"tokens_used"`
	CostUSD        float64 `json:"cost_usd"`
	ResponseTimeMs int     `json:"response_time_ms"`
	
	// Agent type-specific response data
	// For Q&A agents
	Sources        []SourceReference      `json:"sources,omitempty"`
	
	// For Conversational agents
	ConversationID *string                `json:"conversation_id,omitempty"`
	
	// For Producer agents
	Production     *ProductionResult      `json:"production,omitempty"`
	
	// Timestamps
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// SourceReference represents a reference to a source used in agent execution
type SourceReference struct {
	NotebookID   string  `json:"notebook_id"`
	NotebookName string  `json:"notebook_name"`
	ChunkID      string  `json:"chunk_id,omitempty"`
	Relevance    float64 `json:"relevance"`
	Content      string  `json:"content"`
}

// ProductionResult represents the result of a producer agent execution
type ProductionResult struct {
	ID           string                 `json:"id"`
	Title        string                 `json:"title"`
	Format       string                 `json:"format"`
	Content      string                 `json:"content"`
	Template     string                 `json:"template,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

// AgentExecutionStats represents statistics for agent executions
type AgentExecutionStats struct {
	TotalExecutions int     `json:"total_executions"`
	SuccessfulExecutions int `json:"successful_executions"`
	FailedExecutions int     `json:"failed_executions"`
	AverageResponseTime int  `json:"average_response_time_ms"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	LastExecutedAt   *time.Time `json:"last_executed_at,omitempty"`
}