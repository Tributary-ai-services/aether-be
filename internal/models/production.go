package models

import (
	"time"

	"github.com/google/uuid"
)

// ProductionStatus represents the status of a production
type ProductionStatus string

const (
	ProductionStatusProcessing ProductionStatus = "processing"
	ProductionStatusQueued     ProductionStatus = "queued"
	ProductionStatusRendering  ProductionStatus = "rendering"
	ProductionStatusCompleted  ProductionStatus = "completed"
	ProductionStatusFailed     ProductionStatus = "failed"
	ProductionStatusRetrying   ProductionStatus = "retrying"
)

// ProductionType represents the type of production
type ProductionType string

const (
	ProductionTypeSummary ProductionType = "summary"
	ProductionTypeQA      ProductionType = "qa"
	ProductionTypeOutline ProductionType = "outline"
	ProductionTypeInsight ProductionType = "insight"
	ProductionTypeCustom  ProductionType = "custom"
	ProductionTypePodcast ProductionType = "podcast"
)

// ProductionFormat represents the output format of a production
type ProductionFormat string

const (
	ProductionFormatMarkdown ProductionFormat = "markdown"
	ProductionFormatHTML     ProductionFormat = "html"
	ProductionFormatJSON     ProductionFormat = "json"
	ProductionFormatText     ProductionFormat = "text"
	ProductionFormatAudio    ProductionFormat = "audio"
)

// Production represents a generated artifact from a producer agent
type Production struct {
	// Identity
	ID    string `json:"id" validate:"required,uuid"`
	Title string `json:"title" validate:"required,min=1,max=255"`

	// Production type and format
	Type   ProductionType   `json:"type" validate:"required,oneof=summary qa outline insight custom podcast"`
	Format ProductionFormat `json:"format" validate:"required,oneof=markdown html json text audio"`
	Status ProductionStatus `json:"status" validate:"required,oneof=processing queued rendering completed failed retrying"`

	// Agent that created this production
	AgentID        string `json:"agent_id" validate:"required,uuid"`
	AgentBuilderID string `json:"agent_builder_id,omitempty" validate:"omitempty,uuid"`

	// Notebook association
	NotebookID string `json:"notebook_id" validate:"required,uuid"`

	// Owner and multi-tenancy
	UserID    string    `json:"user_id" validate:"required,uuid"`
	SpaceType SpaceType `json:"space_type" validate:"required,oneof=personal organization"`
	SpaceID   string    `json:"space_id" validate:"required"`
	TenantID  string    `json:"tenant_id" validate:"required"`

	// S3 storage
	StorageBucket string `json:"storage_bucket,omitempty"`
	StorageKey    string `json:"storage_key,omitempty"`
	SizeBytes     int64  `json:"size_bytes"`

	// Execution metrics
	ExecutionID    string  `json:"execution_id,omitempty"`
	TokensUsed     int     `json:"tokens_used"`
	CostUSD        float64 `json:"cost_usd"`
	ResponseTimeMs int     `json:"response_time_ms"`

	// Renderer (post-processing)
	MediaURL      string                 `json:"media_url,omitempty"`
	MediaMetadata map[string]interface{} `json:"media_metadata,omitempty"`
	RendererID    string                 `json:"renderer_id,omitempty"`

	// Progress tracking (Kafka job queue)
	ProgressPhase string `json:"progress_phase,omitempty"` // tts_generating, assembling, uploading
	RetryCount    int    `json:"retry_count"`
	MaxRetries    int    `json:"max_retries"`
	WorkerID      string `json:"worker_id,omitempty"`

	// Error handling
	ErrorMessage string `json:"error_message,omitempty"`

	// Source documents (document IDs used to generate this production)
	SourceDocuments []string `json:"source_documents,omitempty"`

	// Search text for full-text search
	SearchText string `json:"search_text,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProductionCreateRequest represents a request to create/execute a production
type ProductionCreateRequest struct {
	Title           string           `json:"title" validate:"required,safe_string,min=1,max=255"`
	Type            ProductionType   `json:"type" validate:"required,oneof=summary qa outline insight custom podcast"`
	Format          ProductionFormat `json:"format" validate:"required,oneof=markdown html json text audio"`
	SourceDocuments []string         `json:"source_documents,omitempty" validate:"dive,uuid"`

	// Optional context/parameters for the producer agent
	Context map[string]interface{} `json:"context,omitempty"`
}

// ProductionUpdateRequest represents a request to update a production
type ProductionUpdateRequest struct {
	Title  *string          `json:"title,omitempty" validate:"omitempty,safe_string,min=1,max=255"`
	Status *ProductionStatus `json:"status,omitempty" validate:"omitempty,oneof=processing queued rendering completed failed retrying"`
}

// ProductionResponse represents a production response
type ProductionResponse struct {
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	Type            ProductionType   `json:"type"`
	Format          ProductionFormat `json:"format"`
	Status          ProductionStatus `json:"status"`
	AgentID         string           `json:"agentId"`
	AgentBuilderID  string           `json:"agentBuilderId,omitempty"`
	NotebookID      string           `json:"notebookId"`
	UserID          string           `json:"userId"`
	SpaceType       SpaceType        `json:"spaceType"`
	SpaceID         string           `json:"spaceId"`
	SizeBytes       int64            `json:"sizeBytes"`
	ExecutionID     string           `json:"executionId,omitempty"`
	TokensUsed      int              `json:"tokensUsed"`
	CostUSD         float64          `json:"costUsd"`
	ResponseTimeMs  int              `json:"responseTimeMs"`
	ErrorMessage    string                 `json:"errorMessage,omitempty"`
	ProgressPhase   string                 `json:"progressPhase,omitempty"`
	RetryCount      int                    `json:"retryCount"`
	MediaURL        string                 `json:"mediaUrl,omitempty"`
	MediaMetadata   map[string]interface{} `json:"mediaMetadata,omitempty"`
	RendererID      string                 `json:"rendererId,omitempty"`
	SourceDocuments []string               `json:"sourceDocuments,omitempty"`
	CreatedAt       time.Time              `json:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt"`

	// Optional related data
	Agent    *AgentResponse    `json:"agent,omitempty"`
	Notebook *NotebookResponse `json:"notebook,omitempty"`
	Owner    *PublicUserResponse `json:"owner,omitempty"`

	// Content (populated when fetching single production with content)
	Content string `json:"content,omitempty"`
}

// ProductionListResponse represents a paginated list of productions
type ProductionListResponse struct {
	Productions []*ProductionResponse `json:"productions"`
	Total       int                   `json:"total"`
	Limit       int                   `json:"limit"`
	Offset      int                   `json:"offset"`
	HasMore     bool                  `json:"hasMore"`
}

// ProductionSearchRequest represents a production search request
type ProductionSearchRequest struct {
	Query      string           `json:"query,omitempty" validate:"omitempty,safe_string,min=2,max=100"`
	NotebookID string           `json:"notebook_id,omitempty" validate:"omitempty,uuid"`
	AgentID    string           `json:"agent_id,omitempty" validate:"omitempty,uuid"`
	Type       ProductionType   `json:"type,omitempty" validate:"omitempty,oneof=summary qa outline insight custom podcast"`
	Status     ProductionStatus `json:"status,omitempty" validate:"omitempty,oneof=processing queued rendering completed failed retrying"`
	Limit      int              `json:"limit,omitempty" validate:"omitempty,min=1,max=100"`
	Offset     int              `json:"offset,omitempty" validate:"omitempty,min=0"`
}

// ProducerExecuteRequest represents a request to execute a producer agent
type ProducerExecuteRequest struct {
	AgentID         string           `json:"agent_id" validate:"required,uuid"`
	Title           string           `json:"title" validate:"required,safe_string,min=1,max=255"`
	Type            ProductionType   `json:"type" validate:"required,oneof=summary qa outline insight custom podcast"`
	Format          ProductionFormat `json:"format" validate:"required,oneof=markdown html json text audio"`
	SourceDocuments []string         `json:"source_documents,omitempty" validate:"dive,uuid"`
	Context         map[string]interface{} `json:"context,omitempty"`
}

// ProducerExecuteResponse represents the response from executing a producer
type ProducerExecuteResponse struct {
	Production *ProductionResponse `json:"production"`
	Content    string              `json:"content,omitempty"`
}

// ProducerPreferences represents user preferences for producers
type ProducerPreferences struct {
	Pinned   *PinnedProducers         `json:"pinned,omitempty"`
	Order    *OrderedProducers        `json:"order,omitempty"`
	Recent   []string                 `json:"recent,omitempty"`
	Settings *ProducerSettingsPrefs   `json:"settings,omitempty"`
}

// PinnedProducers represents pinned producer agents at different levels
type PinnedProducers struct {
	Global     []string            `json:"global,omitempty"`      // Globally pinned agents
	BySpace    map[string][]string `json:"by_space,omitempty"`    // Pinned by space ID
	ByNotebook map[string][]string `json:"by_notebook,omitempty"` // Pinned by notebook ID
}

// OrderedProducers represents custom ordering of producer agents at different levels
type OrderedProducers struct {
	Global     []string            `json:"global,omitempty"`      // Global custom order of agent IDs
	BySpace    map[string][]string `json:"by_space,omitempty"`    // Custom order by space ID
	ByNotebook map[string][]string `json:"by_notebook,omitempty"` // Custom order by notebook ID
}

// ProducerSettingsPrefs represents producer settings preferences
type ProducerSettingsPrefs struct {
	DefaultFormat ProductionFormat `json:"default_format,omitempty"`
	DefaultType   ProductionType   `json:"default_type,omitempty"`
}

// UpdateProducerPreferencesRequest represents a request to update producer preferences
type UpdateProducerPreferencesRequest struct {
	// Pin/unpin actions
	PinGlobal      *string `json:"pin_global,omitempty" validate:"omitempty,uuid"`      // Agent ID to pin globally
	UnpinGlobal    *string `json:"unpin_global,omitempty" validate:"omitempty,uuid"`    // Agent ID to unpin globally
	PinToSpace     *string `json:"pin_to_space,omitempty" validate:"omitempty,uuid"`    // Agent ID to pin to current space
	UnpinFromSpace *string `json:"unpin_from_space,omitempty" validate:"omitempty,uuid"` // Agent ID to unpin from current space
	PinToNotebook  *string `json:"pin_to_notebook,omitempty" validate:"omitempty,uuid"` // Agent ID to pin to specified notebook
	UnpinFromNotebook *string `json:"unpin_from_notebook,omitempty" validate:"omitempty,uuid"` // Agent ID to unpin

	// Order actions (for drag-and-drop reordering)
	SetOrderGlobal     []string `json:"set_order_global,omitempty"`      // Set global custom order of agent IDs
	SetOrderForSpace   []string `json:"set_order_for_space,omitempty"`   // Set custom order for a specific space
	SetOrderForNotebook []string `json:"set_order_for_notebook,omitempty"` // Set custom order for a specific notebook

	// Context for pin/order operations
	SpaceID    string `json:"space_id,omitempty"`
	NotebookID string `json:"notebook_id,omitempty"`

	// Settings updates
	DefaultFormat *ProductionFormat `json:"default_format,omitempty" validate:"omitempty,oneof=markdown html json text audio"`
	DefaultType   *ProductionType   `json:"default_type,omitempty" validate:"omitempty,oneof=summary qa outline insight custom podcast"`
}

// NewProduction creates a new production with default values
func NewProduction(req ProducerExecuteRequest, agentID, agentBuilderID, notebookID, userID string, spaceCtx *SpaceContext) *Production {
	now := time.Now()
	return &Production{
		ID:              uuid.New().String(),
		Title:           req.Title,
		Type:            req.Type,
		Format:          req.Format,
		Status:          ProductionStatusProcessing,
		AgentID:         agentID,
		AgentBuilderID:  agentBuilderID,
		NotebookID:      notebookID,
		UserID:          userID,
		SpaceType:       spaceCtx.SpaceType,
		SpaceID:         spaceCtx.SpaceID,
		TenantID:        spaceCtx.TenantID,
		SourceDocuments: req.SourceDocuments,
		SearchText:      buildProductionSearchText(req.Title, string(req.Type)),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// ToResponse converts a Production to ProductionResponse
func (p *Production) ToResponse() *ProductionResponse {
	return &ProductionResponse{
		ID:              p.ID,
		Title:           p.Title,
		Type:            p.Type,
		Format:          p.Format,
		Status:          p.Status,
		AgentID:         p.AgentID,
		AgentBuilderID:  p.AgentBuilderID,
		NotebookID:      p.NotebookID,
		UserID:          p.UserID,
		SpaceType:       p.SpaceType,
		SpaceID:         p.SpaceID,
		SizeBytes:       p.SizeBytes,
		ExecutionID:     p.ExecutionID,
		TokensUsed:      p.TokensUsed,
		CostUSD:         p.CostUSD,
		ResponseTimeMs:  p.ResponseTimeMs,
		ErrorMessage:    p.ErrorMessage,
		ProgressPhase:   p.ProgressPhase,
		RetryCount:      p.RetryCount,
		MediaURL:        p.MediaURL,
		MediaMetadata:   p.MediaMetadata,
		RendererID:      p.RendererID,
		SourceDocuments: p.SourceDocuments,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

// Update updates production fields from an update request
func (p *Production) Update(req ProductionUpdateRequest) {
	if req.Title != nil {
		p.Title = *req.Title
	}
	if req.Status != nil {
		p.Status = *req.Status
	}
	p.SearchText = buildProductionSearchText(p.Title, string(p.Type))
	p.UpdatedAt = time.Now()
}

// MarkCompleted marks the production as completed with execution details
func (p *Production) MarkCompleted(storageKey, storageBucket string, sizeBytes int64, executionID string, tokensUsed int, costUSD float64, responseTimeMs int) {
	p.Status = ProductionStatusCompleted
	p.StorageKey = storageKey
	p.StorageBucket = storageBucket
	p.SizeBytes = sizeBytes
	p.ExecutionID = executionID
	p.TokensUsed = tokensUsed
	p.CostUSD = costUSD
	p.ResponseTimeMs = responseTimeMs
	p.UpdatedAt = time.Now()
}

// MarkFailed marks the production as failed with an error message
func (p *Production) MarkFailed(errorMessage string) {
	p.Status = ProductionStatusFailed
	p.ErrorMessage = errorMessage
	p.UpdatedAt = time.Now()
}

// IsCompleted returns true if the production is completed
func (p *Production) IsCompleted() bool {
	return p.Status == ProductionStatusCompleted
}

// IsFailed returns true if the production has failed
func (p *Production) IsFailed() bool {
	return p.Status == ProductionStatusFailed
}

// IsProcessing returns true if the production is still processing
func (p *Production) IsProcessing() bool {
	return p.Status == ProductionStatusProcessing
}

// CanBeAccessedBy checks if a user can access this production
func (p *Production) CanBeAccessedBy(userID string) bool {
	return p.UserID == userID
}

// GetStoragePath returns the full S3 path for this production
func (p *Production) GetStoragePath() string {
	if p.StorageKey == "" {
		return ""
	}
	return p.StorageKey
}

// buildProductionSearchText creates a searchable text field from title and type
func buildProductionSearchText(title, prodType string) string {
	return title + " " + prodType
}

// BulkDeleteProductionsRequest represents a request to delete multiple productions
type BulkDeleteProductionsRequest struct {
	ProductionIDs []string `json:"production_ids" validate:"required,min=1,max=100,dive,uuid"`
}

// BulkDeleteProductionsResponse represents the response from a bulk delete operation
type BulkDeleteProductionsResponse struct {
	Deleted []string             `json:"deleted"`
	Failed  []BulkDeleteFailure  `json:"failed,omitempty"`
}

// BulkDeleteFailure represents a failed deletion in a bulk operation
type BulkDeleteFailure struct {
	ProductionID string `json:"production_id"`
	Error        string `json:"error"`
}

// BuildProductionStorageKey constructs the S3 storage key for a production
// Pattern: spaces/{space_type}/notebooks/{notebook_id}/productions/{production_id}/{filename}
func BuildProductionStorageKey(spaceType, notebookID, productionID, filename string) string {
	return "spaces/" + spaceType + "/notebooks/" + notebookID + "/productions/" + productionID + "/" + filename
}
