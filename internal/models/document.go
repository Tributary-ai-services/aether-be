package models

import (
	"time"

	"github.com/google/uuid"
)

// Document represents a document in the system
type Document struct {
	ID          string `json:"id" validate:"required,uuid"`
	Name        string `json:"name" validate:"required,min=1,max=255"`
	Description string `json:"description,omitempty" validate:"max=1000"`
	Type        string `json:"type" validate:"required"`
	Status      string `json:"status" validate:"required,oneof=uploading processing processed failed archived deleted"`

	// File information
	OriginalName string `json:"original_name" validate:"required"`
	MimeType     string `json:"mime_type" validate:"required"`
	SizeBytes    int64  `json:"size_bytes" validate:"min=0"`
	Checksum     string `json:"checksum,omitempty"`

	// Storage information
	StoragePath   string `json:"storage_path,omitempty"`
	StorageBucket string `json:"storage_bucket,omitempty"`

	// Content and processing
	ExtractedText    string                 `json:"extracted_text,omitempty"`
	ProcessingResult map[string]interface{} `json:"processing_result,omitempty" validate:"omitempty,neo4j_compatible"`
	ProcessingTime   *int64                 `json:"processingTime,omitempty"` // Processing duration in milliseconds
	ConfidenceScore  *float64               `json:"confidenceScore,omitempty"` // AI confidence score (0.0-1.0)
	Metadata         map[string]interface{} `json:"metadata,omitempty" validate:"omitempty,neo4j_compatible"`

	// Relationships
	NotebookID string `json:"notebook_id" validate:"required,uuid"`
	OwnerID    string `json:"owner_id" validate:"required,uuid"`

	// Space and tenant information (inherited from notebook)
	SpaceType SpaceType `json:"space_type" validate:"required,oneof=personal organization"`
	SpaceID   string    `json:"space_id" validate:"required,uuid"`
	TenantID  string    `json:"tenant_id" validate:"required"`

	// Search and indexing
	SearchText string   `json:"search_text,omitempty"`
	Tags       []string `json:"tags,omitempty"`

	// Processing information
	ProcessingJobID      string     `json:"processing_job_id,omitempty"`
	ProcessedAt          *time.Time `json:"processed_at,omitempty"`
	ChunkingStrategy     string     `json:"chunking_strategy,omitempty"`     // Strategy used for chunking
	ChunkCount           int        `json:"chunk_count" validate:"min=0"`    // Number of chunks created
	AverageChunkSize     int64      `json:"average_chunk_size,omitempty" validate:"min=0"` // Average chunk size in bytes
	ChunkQualityScore    *float64   `json:"chunk_quality_score,omitempty" validate:"omitempty,min=0,max=1"` // Average quality across all chunks

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DocumentCreateRequest represents a request to create a document
type DocumentCreateRequest struct {
	Name        string                 `json:"name" validate:"required,filename,min=1,max=255"`
	Description string                 `json:"description,omitempty" validate:"safe_string,max=1000"`
	NotebookID  string                 `json:"notebook_id" validate:"required,uuid"`
	Tags        []string               `json:"tags,omitempty" validate:"dive,tag,min=1,max=50"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// DocumentUpdateRequest represents a request to update a document
type DocumentUpdateRequest struct {
	Name        *string                `json:"name,omitempty" validate:"omitempty,filename,min=1,max=255"`
	Description *string                `json:"description,omitempty" validate:"omitempty,safe_string,max=1000"`
	Status      *string                `json:"status,omitempty" validate:"omitempty,oneof=uploading processing processed failed archived deleted"`
	Tags        []string               `json:"tags,omitempty" validate:"dive,tag,min=1,max=50"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// DocumentResponse represents a document response
type DocumentResponse struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description,omitempty"`
	Type             string                 `json:"type"`
	Status           string                 `json:"status"`
	OriginalName     string                 `json:"original_name"`
	MimeType         string                 `json:"mime_type"`
	SizeBytes        int64                  `json:"size_bytes"`
	ExtractedText    string                 `json:"extracted_text,omitempty"`
	ProcessingResult map[string]interface{} `json:"processing_result,omitempty" validate:"omitempty,neo4j_compatible"`
	ProcessingTime   *int64                 `json:"processingTime,omitempty"` // Processing duration in milliseconds
	ConfidenceScore  *float64               `json:"confidenceScore,omitempty"` // AI confidence score (0.0-1.0)
	Metadata         map[string]interface{} `json:"metadata,omitempty" validate:"omitempty,neo4j_compatible"`
	NotebookID           string                 `json:"notebook_id"`
	OwnerID              string                 `json:"owner_id"`
	Tags                 []string               `json:"tags,omitempty"`
	ProcessedAt          *time.Time             `json:"processed_at,omitempty"`
	ChunkingStrategy     string                 `json:"chunking_strategy,omitempty"`
	ChunkCount           int                    `json:"chunk_count"`
	AverageChunkSize     int64                  `json:"average_chunk_size,omitempty"`
	ChunkQualityScore    *float64               `json:"chunk_quality_score,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`

	// Optional fields for detailed responses
	Owner    *PublicUserResponse `json:"owner,omitempty"`
	Notebook *NotebookResponse   `json:"notebook,omitempty"`
}

// DocumentListResponse represents a paginated list of documents
type DocumentListResponse struct {
	Documents []*DocumentResponse `json:"documents"`
	Total     int                 `json:"total"`
	Limit     int                 `json:"limit"`
	Offset    int                 `json:"offset"`
	HasMore   bool                `json:"has_more"`
}

// DocumentSearchRequest represents a document search request
type DocumentSearchRequest struct {
	Query      string   `json:"query,omitempty" validate:"omitempty,min=2,max=100"`
	NotebookID string   `json:"notebook_id,omitempty" validate:"omitempty,uuid"`
	OwnerID    string   `json:"owner_id,omitempty" validate:"omitempty,uuid"`
	Type       string   `json:"type,omitempty"`
	Status     string   `json:"status,omitempty" validate:"omitempty,oneof=uploading processing processed failed archived deleted"`
	Tags       []string `json:"tags,omitempty" validate:"dive,min=1,max=50"`
	MimeType   string   `json:"mime_type,omitempty"`
	Limit      int      `json:"limit,omitempty" validate:"omitempty,min=1,max=100"`
	Offset     int      `json:"offset,omitempty" validate:"omitempty,min=0"`
}

// DocumentUploadRequest represents a document upload request
type DocumentUploadRequest struct {
	DocumentCreateRequest
	FileData []byte `json:"-"` // File content (not included in JSON)
}

// DocumentBase64UploadRequest represents a base64 encoded document upload request
type DocumentBase64UploadRequest struct {
	DocumentCreateRequest
	FileContent string `json:"file_content" validate:"required,base64"` // Base64 encoded file content
	FileName    string `json:"file_name" validate:"required,filename"`  // Original filename
	MimeType    string `json:"mime_type" validate:"required"`           // MIME type of the file
}


// DocumentStats represents document statistics
type DocumentStats struct {
	TotalDocuments      int   `json:"total_documents"`
	ProcessingDocuments int   `json:"processing_documents"`
	ProcessedDocuments  int   `json:"processed_documents"`
	FailedDocuments     int   `json:"failed_documents"`
	TotalSizeBytes      int64 `json:"total_size_bytes"`
}

// NewDocument creates a new document with default values
func NewDocument(req DocumentCreateRequest, ownerID string, fileInfo FileInfo, spaceCtx *SpaceContext) *Document {
	now := time.Now()
	return &Document{
		ID:           uuid.New().String(),
		Name:         req.Name,
		Description:  req.Description,
		Type:         determineDocumentType(fileInfo.MimeType),
		Status:       "uploading",
		OriginalName: fileInfo.OriginalName,
		MimeType:     fileInfo.MimeType,
		SizeBytes:    fileInfo.SizeBytes,
		Checksum:     fileInfo.Checksum,
		NotebookID:   req.NotebookID,
		OwnerID:      ownerID,
		SpaceType:    spaceCtx.SpaceType,
		SpaceID:      spaceCtx.SpaceID,
		TenantID:     spaceCtx.TenantID,
		Tags:         req.Tags,
		Metadata:     req.Metadata,
		SearchText:   buildSearchText(req.Name, req.Description, req.Tags),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// FileInfo represents file information for upload
type FileInfo struct {
	OriginalName string
	MimeType     string
	SizeBytes    int64
	Checksum     string
}

// ToResponse converts a Document to DocumentResponse
func (d *Document) ToResponse() *DocumentResponse {
	return &DocumentResponse{
		ID:               d.ID,
		Name:             d.Name,
		Description:      d.Description,
		Type:             d.Type,
		Status:           d.Status,
		OriginalName:     d.OriginalName,
		MimeType:         d.MimeType,
		SizeBytes:        d.SizeBytes,
		ExtractedText:    d.ExtractedText,
		ProcessingResult: d.ProcessingResult,
		ProcessingTime:   d.ProcessingTime,
		ConfidenceScore:  d.ConfidenceScore,
		Metadata:         d.Metadata,
		NotebookID:       d.NotebookID,
		OwnerID:          d.OwnerID,
		Tags:             d.Tags,
		ProcessedAt:      d.ProcessedAt,
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.UpdatedAt,
	}
}

// Update updates document fields from an update request
func (d *Document) Update(req DocumentUpdateRequest) {
	if req.Name != nil {
		d.Name = *req.Name
	}
	if req.Description != nil {
		d.Description = *req.Description
	}
	if req.Status != nil {
		d.Status = *req.Status
	}
	if req.Tags != nil {
		d.Tags = req.Tags
	}
	if req.Metadata != nil {
		d.Metadata = req.Metadata
	}

	// Update search text
	d.SearchText = buildSearchText(d.Name, d.Description, d.Tags)
	d.UpdatedAt = time.Now()
}

// UpdateProcessingStatus updates the processing status and related fields
func (d *Document) UpdateProcessingStatus(status string, result map[string]interface{}, errorMsg string) {
	d.Status = status
	if result != nil {
		d.ProcessingResult = result
	}

	// Extract text from processing result if available
	if status == "processed" && result != nil {
		if extractedText, ok := result["extracted_text"].(string); ok {
			d.ExtractedText = extractedText
			d.SearchText = buildSearchText(d.Name, d.Description, d.Tags) + " " + extractedText
		}
		now := time.Now()
		d.ProcessedAt = &now
	}

	d.UpdatedAt = time.Now()
}

// UpdateStorageInfo updates storage-related information
func (d *Document) UpdateStorageInfo(storagePath, storageBucket string) {
	d.StoragePath = storagePath
	d.StorageBucket = storageBucket
	d.UpdatedAt = time.Now()
}

// IsProcessed returns true if the document has been processed
func (d *Document) IsProcessed() bool {
	return d.Status == "processed"
}

// IsProcessing returns true if the document is currently being processed
func (d *Document) IsProcessing() bool {
	return d.Status == "processing"
}

// HasFailed returns true if document processing failed
func (d *Document) HasFailed() bool {
	return d.Status == "failed"
}

// AddTag adds a tag to the document
func (d *Document) AddTag(tag string) {
	// Check if tag already exists
	for _, existingTag := range d.Tags {
		if existingTag == tag {
			return
		}
	}

	d.Tags = append(d.Tags, tag)
	d.SearchText = buildSearchText(d.Name, d.Description, d.Tags)
	if d.ExtractedText != "" {
		d.SearchText += " " + d.ExtractedText
	}
	d.UpdatedAt = time.Now()
}

// RemoveTag removes a tag from the document
func (d *Document) RemoveTag(tag string) {
	for i, existingTag := range d.Tags {
		if existingTag == tag {
			d.Tags = append(d.Tags[:i], d.Tags[i+1:]...)
			break
		}
	}

	d.SearchText = buildSearchText(d.Name, d.Description, d.Tags)
	if d.ExtractedText != "" {
		d.SearchText += " " + d.ExtractedText
	}
	d.UpdatedAt = time.Now()
}

// HasTag checks if the document has a specific tag
func (d *Document) HasTag(tag string) bool {
	for _, existingTag := range d.Tags {
		if existingTag == tag {
			return true
		}
	}
	return false
}

// GetFileExtension returns the file extension based on original name
func (d *Document) GetFileExtension() string {
	if d.OriginalName == "" {
		return ""
	}

	for i := len(d.OriginalName) - 1; i >= 0; i-- {
		if d.OriginalName[i] == '.' {
			return d.OriginalName[i+1:]
		}
	}
	return ""
}

// Helper functions

// determineDocumentType determines document type based on MIME type
func determineDocumentType(mimeType string) string {
	switch {
	case mimeType == "application/pdf":
		return "pdf"
	case mimeType == "application/msword" || mimeType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "document"
	case mimeType == "application/vnd.ms-excel" || mimeType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "spreadsheet"
	case mimeType == "application/vnd.ms-powerpoint" || mimeType == "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return "presentation"
	case mimeType == "text/plain":
		return "text"
	case mimeType == "text/csv":
		return "csv"
	case mimeType == "application/json":
		return "json"
	case mimeType == "application/xml" || mimeType == "text/xml":
		return "xml"
	case mimeType[:5] == "image":
		return "image"
	case mimeType[:5] == "video":
		return "video"
	case mimeType[:5] == "audio":
		return "audio"
	default:
		return "unknown"
	}
}

// buildSearchText creates a searchable text field
func buildSearchText(name, description string, tags []string) string {
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
