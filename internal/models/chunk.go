package models

import (
	"time"

	"github.com/google/uuid"
)

// Chunk represents a chunk of processed content from a document
type Chunk struct {
	ID       string `json:"id" validate:"required,uuid"`
	TenantID string `json:"tenant_id" validate:"required"`
	FileID   string `json:"file_id" validate:"required,uuid"`

	// Chunk identification
	ChunkID     string `json:"chunk_id" validate:"required"`     // Unique identifier within file
	ChunkType   string `json:"chunk_type" validate:"required"`   // text, table, image, etc.
	ChunkNumber int    `json:"chunk_number" validate:"min=0"`    // Sequential number within file

	// Content
	Content     string `json:"content" validate:"required"`
	ContentHash string `json:"content_hash,omitempty"`          // Hash of content for deduplication
	SizeBytes   int64  `json:"size_bytes" validate:"min=0"`

	// Position information
	StartPosition *int64 `json:"start_position,omitempty"`
	EndPosition   *int64 `json:"end_position,omitempty"`
	PageNumber    *int   `json:"page_number,omitempty"`
	LineNumber    *int   `json:"line_number,omitempty"`

	// Relationships to other chunks
	ParentChunkID *string  `json:"parent_chunk_id,omitempty" validate:"omitempty,uuid"`
	Relationships []string `json:"relationships,omitempty"`

	// Processing metadata
	ProcessedAt    time.Time `json:"processed_at"`
	ProcessedBy    string    `json:"processed_by"`                      // Strategy name
	ProcessingTime int64     `json:"processing_time" validate:"min=0"`  // Time in milliseconds

	// Quality metrics
	Quality ChunkQualityMetrics `json:"quality"`

	// Content analysis
	Language         string   `json:"language,omitempty"`
	LanguageConf     float64  `json:"language_confidence,omitempty" validate:"min=0,max=1"`
	ContentCategory  string   `json:"content_category,omitempty"`
	SensitivityLevel string   `json:"sensitivity_level,omitempty"`
	Classifications  []string `json:"classifications,omitempty"`

	// Embedding information
	EmbeddingStatus string    `json:"embedding_status" validate:"oneof=pending processing completed failed skipped"`
	EmbeddingModel  string    `json:"embedding_model,omitempty"`
	EmbeddingVector []float64 `json:"embedding_vector,omitempty"`
	EmbeddingDim    int       `json:"embedding_dimension,omitempty" validate:"min=0"`
	EmbeddedAt      *time.Time `json:"embedded_at,omitempty"`

	// Compliance and security
	PIIDetected     bool     `json:"pii_detected"`
	ComplianceFlags []string `json:"compliance_flags,omitempty"`
	DLPScanStatus   string   `json:"dlp_scan_status" validate:"oneof=pending processing completed failed skipped"`
	DLPScanResult   string   `json:"dlp_scan_result,omitempty"`

	// Context information
	Context map[string]string `json:"context,omitempty"`

	// Schema information for structured chunks
	SchemaInfo map[string]interface{} `json:"schema_info,omitempty"`

	// Metadata
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CustomFields map[string]interface{} `json:"custom_fields,omitempty"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// ChunkQualityMetrics represents quality metrics for a chunk
type ChunkQualityMetrics struct {
	Completeness float64 `json:"completeness" validate:"min=0,max=1"`  // How complete the content appears (0.0-1.0)
	Coherence    float64 `json:"coherence" validate:"min=0,max=1"`     // How semantically coherent the content is
	Uniqueness   float64 `json:"uniqueness" validate:"min=0,max=1"`    // How unique this chunk is vs others
	Readability  float64 `json:"readability" validate:"min=0,max=1"`   // Text readability score
	LanguageConf float64 `json:"language_conf" validate:"min=0,max=1"` // Confidence in detected language
	Language     string  `json:"language"`                             // Detected language code
	Complexity   float64 `json:"complexity" validate:"min=0,max=1"`    // Content complexity score
	Density      float64 `json:"density" validate:"min=0,max=1"`       // Information density score
}

// ChunkCreateRequest represents a request to create a chunk
type ChunkCreateRequest struct {
	FileID      string                 `json:"file_id" validate:"required,uuid"`
	ChunkID     string                 `json:"chunk_id" validate:"required"`
	ChunkType   string                 `json:"chunk_type" validate:"required"`
	ChunkNumber int                    `json:"chunk_number" validate:"min=0"`
	Content     string                 `json:"content" validate:"required"`
	ContentHash string                 `json:"content_hash,omitempty"`
	SizeBytes   int64                  `json:"size_bytes" validate:"min=0"`
	Strategy    string                 `json:"strategy,omitempty"`
	Quality     ChunkQualityMetrics    `json:"quality,omitempty"`
	Context     map[string]string      `json:"context,omitempty"`
	SchemaInfo  map[string]interface{} `json:"schema_info,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ChunkUpdateRequest represents a request to update a chunk
type ChunkUpdateRequest struct {
	Content         *string                `json:"content,omitempty"`
	Quality         *ChunkQualityMetrics   `json:"quality,omitempty"`
	Language        *string                `json:"language,omitempty"`
	LanguageConf    *float64               `json:"language_confidence,omitempty" validate:"omitempty,min=0,max=1"`
	ContentCategory *string                `json:"content_category,omitempty"`
	Classifications []string               `json:"classifications,omitempty"`
	Context         map[string]string      `json:"context,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// ChunkResponse represents a chunk in API responses
type ChunkResponse struct {
	ID              string                 `json:"id"`
	FileID          string                 `json:"file_id"`
	ChunkID         string                 `json:"chunk_id"`
	ChunkType       string                 `json:"chunk_type"`
	ChunkNumber     int                    `json:"chunk_number"`
	Content         string                 `json:"content"`
	ContentHash     string                 `json:"content_hash,omitempty"`
	SizeBytes       int64                  `json:"size_bytes"`
	StartPosition   *int64                 `json:"start_position,omitempty"`
	EndPosition     *int64                 `json:"end_position,omitempty"`
	PageNumber      *int                   `json:"page_number,omitempty"`
	LineNumber      *int                   `json:"line_number,omitempty"`
	ProcessedAt     time.Time              `json:"processed_at"`
	ProcessedBy     string                 `json:"processed_by"`
	ProcessingTime  int64                  `json:"processing_time"`
	Quality         ChunkQualityMetrics    `json:"quality"`
	Language        string                 `json:"language,omitempty"`
	LanguageConf    float64                `json:"language_confidence,omitempty"`
	ContentCategory string                 `json:"content_category,omitempty"`
	Classifications []string               `json:"classifications,omitempty"`
	PIIDetected     bool                   `json:"pii_detected"`
	DLPScanStatus   string                 `json:"dlp_scan_status"`
	DLPScanResult   string                 `json:"dlp_scan_result,omitempty"`
	Context         map[string]string      `json:"context,omitempty"`
	SchemaInfo      map[string]interface{} `json:"schema_info,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// ChunkListResponse represents a paginated list of chunks
type ChunkListResponse struct {
	Chunks  []*ChunkResponse `json:"chunks"`
	Total   int              `json:"total"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
	HasMore bool             `json:"has_more"`
}

// ChunkSearchRequest represents a request to search for chunks
type ChunkSearchRequest struct {
	Query           string `json:"query,omitempty"`
	FileID          string `json:"file_id,omitempty" validate:"omitempty,uuid"`
	ChunkType       string `json:"chunk_type,omitempty"`
	ContentCategory string `json:"content_category,omitempty"`
	Language        string `json:"language,omitempty"`
	PIIDetected     *bool  `json:"pii_detected,omitempty"`
	DLPScanStatus   string `json:"dlp_scan_status,omitempty"`
	MinQuality      float64 `json:"min_quality,omitempty" validate:"min=0,max=1"`
	Limit           int    `json:"limit,omitempty" validate:"min=1,max=100"`
	Offset          int    `json:"offset,omitempty" validate:"min=0"`
}

// ProcessingJob represents a document processing job
type ProcessingJob struct {
	ID          string                 `json:"id" validate:"required,uuid"`
	DocumentID  string                 `json:"document_id" validate:"required,uuid"`
	Type        string                 `json:"type" validate:"required"`
	Status      string                 `json:"status" validate:"required,oneof=pending processing completed failed cancelled"`
	Priority    int                    `json:"priority" validate:"min=0,max=10"`
	Progress    float64                `json:"progress" validate:"min=0,max=100"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Result      map[string]interface{} `json:"result,omitempty"`
	Error       string                 `json:"error,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}

// NewChunk creates a new chunk instance
func NewChunk(req ChunkCreateRequest, tenantID string) *Chunk {
	now := time.Now()
	
	chunk := &Chunk{
		ID:             uuid.New().String(),
		TenantID:       tenantID,
		FileID:         req.FileID,
		ChunkID:        req.ChunkID,
		ChunkType:      req.ChunkType,
		ChunkNumber:    req.ChunkNumber,
		Content:        req.Content,
		ContentHash:    req.ContentHash,
		SizeBytes:      req.SizeBytes,
		ProcessedAt:    now,
		ProcessedBy:    req.Strategy,
		ProcessingTime: 0, // Will be set during processing
		Quality:        req.Quality,
		EmbeddingStatus: "pending",
		PIIDetected:    false,
		DLPScanStatus:  "pending",
		Context:        req.Context,
		SchemaInfo:     req.SchemaInfo,
		Metadata:       req.Metadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if chunk.Context == nil {
		chunk.Context = make(map[string]string)
	}
	if chunk.SchemaInfo == nil {
		chunk.SchemaInfo = make(map[string]interface{})
	}
	if chunk.Metadata == nil {
		chunk.Metadata = make(map[string]interface{})
	}

	return chunk
}

// Update updates chunk fields from an update request
func (c *Chunk) Update(req ChunkUpdateRequest) {
	if req.Content != nil {
		c.Content = *req.Content
	}
	if req.Quality != nil {
		c.Quality = *req.Quality
	}
	if req.Language != nil {
		c.Language = *req.Language
	}
	if req.LanguageConf != nil {
		c.LanguageConf = *req.LanguageConf
	}
	if req.ContentCategory != nil {
		c.ContentCategory = *req.ContentCategory
	}
	if req.Classifications != nil {
		c.Classifications = req.Classifications
	}
	if req.Context != nil {
		c.Context = req.Context
	}
	if req.Metadata != nil {
		c.Metadata = req.Metadata
	}
	
	c.UpdatedAt = time.Now()
}

// IsEmbedded checks if the chunk has been embedded
func (c *Chunk) IsEmbedded() bool {
	return c.EmbeddingStatus == "completed" && len(c.EmbeddingVector) > 0
}

// HasPII checks if the chunk contains personally identifiable information
func (c *Chunk) HasPII() bool {
	return c.PIIDetected
}

// GetSensitivityLevel returns the sensitivity level of the chunk
func (c *Chunk) GetSensitivityLevel() string {
	if c.SensitivityLevel == "" {
		return "unknown"
	}
	return c.SensitivityLevel
}

// GetQualityScore returns an overall quality score for the chunk
func (c *Chunk) GetQualityScore() float64 {
	metrics := c.Quality
	
	// Weighted average of quality metrics
	weights := map[string]float64{
		"completeness":  0.25,
		"coherence":     0.25,
		"uniqueness":    0.20,
		"readability":   0.15,
		"language_conf": 0.10,
		"complexity":    0.05,
	}
	
	score := 0.0
	score += metrics.Completeness * weights["completeness"]
	score += metrics.Coherence * weights["coherence"]
	score += metrics.Uniqueness * weights["uniqueness"]
	score += metrics.Readability * weights["readability"]
	score += metrics.LanguageConf * weights["language_conf"]
	score += metrics.Complexity * weights["complexity"]
	
	return score
}

// IsHighQuality checks if the chunk meets high quality thresholds
func (c *Chunk) IsHighQuality() bool {
	return c.GetQualityScore() >= 0.8
}

// IsLowQuality checks if the chunk is below quality thresholds
func (c *Chunk) IsLowQuality() bool {
	return c.GetQualityScore() < 0.5
}

// GetContentPreview returns a preview of the content
func (c *Chunk) GetContentPreview(maxLength int) string {
	if len(c.Content) <= maxLength {
		return c.Content
	}
	return c.Content[:maxLength] + "..."
}

// SetEmbedding sets the embedding vector for the chunk
func (c *Chunk) SetEmbedding(vector []float64, model string) {
	c.EmbeddingVector = vector
	c.EmbeddingModel = model
	c.EmbeddingDim = len(vector)
	c.EmbeddingStatus = "completed"
	now := time.Now()
	c.EmbeddedAt = &now
	c.UpdatedAt = now
}

// MarkEmbeddingFailed marks the embedding as failed
func (c *Chunk) MarkEmbeddingFailed() {
	c.EmbeddingStatus = "failed"
	c.UpdatedAt = time.Now()
}

// SetDLPScanResult sets the DLP scan result for the chunk
func (c *Chunk) SetDLPScanResult(result string, hasPII bool) {
	c.DLPScanStatus = "completed"
	c.DLPScanResult = result
	c.PIIDetected = hasPII
	c.UpdatedAt = time.Now()
}

// MarkDLPScanFailed marks the DLP scan as failed
func (c *Chunk) MarkDLPScanFailed() {
	c.DLPScanStatus = "failed"
	c.UpdatedAt = time.Now()
}

// ToResponse converts a Chunk to a ChunkResponse
func (c *Chunk) ToResponse() *ChunkResponse {
	return &ChunkResponse{
		ID:              c.ID,
		FileID:          c.FileID,
		ChunkID:         c.ChunkID,
		ChunkType:       c.ChunkType,
		ChunkNumber:     c.ChunkNumber,
		Content:         c.Content,
		ContentHash:     c.ContentHash,
		SizeBytes:       c.SizeBytes,
		StartPosition:   c.StartPosition,
		EndPosition:     c.EndPosition,
		PageNumber:      c.PageNumber,
		LineNumber:      c.LineNumber,
		ProcessedAt:     c.ProcessedAt,
		ProcessedBy:     c.ProcessedBy,
		ProcessingTime:  c.ProcessingTime,
		Quality:         c.Quality,
		Language:        c.Language,
		LanguageConf:    c.LanguageConf,
		ContentCategory: c.ContentCategory,
		Classifications: c.Classifications,
		PIIDetected:     c.PIIDetected,
		DLPScanStatus:   c.DLPScanStatus,
		DLPScanResult:   c.DLPScanResult,
		Context:         c.Context,
		SchemaInfo:      c.SchemaInfo,
		Metadata:        c.Metadata,
		CreatedAt:       c.CreatedAt,
		UpdatedAt:       c.UpdatedAt,
	}
}