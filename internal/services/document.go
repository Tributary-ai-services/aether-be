package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// DocumentService handles document-related business logic
type DocumentService struct {
	neo4j           *database.Neo4jClient
	notebookService *NotebookService
	logger          *logger.Logger

	// External services (will be injected)
	storageService    StorageService
	processingService ProcessingService
}

// StorageService interface for file storage operations
type StorageService interface {
	UploadFile(ctx context.Context, key string, data []byte, contentType string) (string, error)
	UploadFileToTenantBucket(ctx context.Context, tenantID, key string, data []byte, contentType string) (string, error)
	DownloadFile(ctx context.Context, key string) ([]byte, error)
	DownloadFileFromTenantBucket(ctx context.Context, tenantID, key string) ([]byte, error)
	DeleteFile(ctx context.Context, key string) error
	DeleteFileFromTenantBucket(ctx context.Context, tenantID, key string) error
	GetFileURL(ctx context.Context, key string, expiration time.Duration) (string, error)
}

// ProcessingService interface for document processing operations
type ProcessingService interface {
	SubmitProcessingJob(ctx context.Context, documentID string, jobType string, config map[string]interface{}) (*models.ProcessingJob, error)
	GetProcessingJob(ctx context.Context, jobID string) (*models.ProcessingJob, error)
	CancelProcessingJob(ctx context.Context, jobID string) error
}

// NewDocumentService creates a new document service
func NewDocumentService(neo4j *database.Neo4jClient, notebookService *NotebookService, log *logger.Logger) *DocumentService {
	return &DocumentService{
		neo4j:           neo4j,
		notebookService: notebookService,
		logger:          log.WithService("document_service"),
	}
}

// SetStorageService sets the storage service dependency
func (s *DocumentService) SetStorageService(storageService StorageService) {
	s.storageService = storageService
}

// SetProcessingService sets the processing service dependency
func (s *DocumentService) SetProcessingService(processingService ProcessingService) {
	s.processingService = processingService
}

// CreateDocument creates a new document record (without file upload)
func (s *DocumentService) CreateDocument(ctx context.Context, req models.DocumentCreateRequest, ownerID string, spaceCtx *models.SpaceContext, fileInfo models.FileInfo) (*models.Document, error) {
	// Verify user can create documents in this space
	if !spaceCtx.CanCreate() {
		return nil, errors.ForbiddenWithDetails("Insufficient permissions to create document", map[string]interface{}{
			"space_id": spaceCtx.SpaceID,
			"user_id":  ownerID,
		})
	}

	// Verify notebook exists and belongs to the correct space
	notebook, err := s.notebookService.GetNotebookByID(ctx, req.NotebookID, ownerID, spaceCtx)
	if err != nil {
		return nil, err
	}

	// Ensure notebook belongs to the same space as the context
	if notebook.TenantID != spaceCtx.TenantID || notebook.SpaceID != spaceCtx.SpaceID {
		return nil, errors.ForbiddenWithDetails("Notebook not accessible in this space", map[string]interface{}{
			"notebook_id": req.NotebookID,
			"space_id":    spaceCtx.SpaceID,
		})
	}

	// Create new document
	document := models.NewDocument(req, ownerID, fileInfo, spaceCtx)

	// Create document in Neo4j
	query := `
		CREATE (d:Document {
			id: $id,
			name: $name,
			description: $description,
			type: $type,
			status: $status,
			original_name: $original_name,
			mime_type: $mime_type,
			size_bytes: $size_bytes,
			checksum: $checksum,
			storage_path: $storage_path,
			storage_bucket: $storage_bucket,
			notebook_id: $notebook_id,
			owner_id: $owner_id,
			space_type: $space_type,
			space_id: $space_id,
			tenant_id: $tenant_id,
			tags: $tags,
			search_text: $search_text,
			metadata: $metadata,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN d
	`

	// Serialize metadata to JSON string for Neo4j storage
	var metadataJSON string
	if document.Metadata != nil {
		metadataBytes, err := json.Marshal(document.Metadata)
		if err != nil {
			s.logger.Error("Failed to serialize document metadata", zap.Error(err))
			return nil, errors.InternalWithCause("Failed to serialize document metadata", err)
		}
		metadataJSON = string(metadataBytes)
	} else {
		metadataJSON = "{}"
	}

	params := map[string]interface{}{
		"id":             document.ID,
		"name":           document.Name,
		"description":    document.Description,
		"type":           document.Type,
		"status":         document.Status,
		"original_name":  document.OriginalName,
		"mime_type":      document.MimeType,
		"size_bytes":     document.SizeBytes,
		"checksum":       document.Checksum,
		"storage_path":   document.StoragePath,
		"storage_bucket": document.StorageBucket,
		"notebook_id":    document.NotebookID,
		"owner_id":       document.OwnerID,
		"space_type":     string(document.SpaceType),
		"space_id":       document.SpaceID,
		"tenant_id":      document.TenantID,
		"tags":           document.Tags,
		"search_text":    document.SearchText,
		"metadata":       metadataJSON,
		"created_at":     document.CreatedAt.Format(time.RFC3339),
		"updated_at":     document.UpdatedAt.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to create document", zap.Error(err))
		return nil, errors.Database("Failed to create document", err)
	}

	// Create relationships and update notebook counts
	if err := s.createDocumentRelationships(ctx, document.ID, document.NotebookID, document.OwnerID, spaceCtx.TenantID, document.SizeBytes); err != nil {
		s.logger.Error("Failed to create document relationships", zap.Error(err))
		// Don't fail the entire operation
	}

	s.logger.Info("Document created successfully",
		zap.String("document_id", document.ID),
		zap.String("name", document.Name),
		zap.String("notebook_id", document.NotebookID),
		zap.String("owner_id", ownerID),
	)

	return document, nil
}

// UploadDocument handles complete document upload including file storage
func (s *DocumentService) UploadDocument(ctx context.Context, req models.DocumentUploadRequest, ownerID string, spaceCtx *models.SpaceContext, fileInfo models.FileInfo) (*models.Document, error) {
	if s.storageService == nil {
		return nil, errors.Internal("Storage service not configured")
	}

	// Use provided file info (MIME type from frontend)

	// Create document record
	document, err := s.CreateDocument(ctx, req.DocumentCreateRequest, ownerID, spaceCtx, fileInfo)
	if err != nil {
		return nil, err
	}

	// Upload file to tenant-scoped storage
	// Build tenant storage key: spaces/{space_type}/notebooks/{notebook_id}/documents/{document_id}/{original_filename}
	storageKey := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s", 
		spaceCtx.SpaceType, document.NotebookID, document.ID, document.OriginalName)
	
	s.logger.Info("About to upload to storage", 
		zap.String("tenant_id", spaceCtx.TenantID),
		zap.String("storage_key", storageKey),
		zap.String("space_type", string(spaceCtx.SpaceType)),
		zap.String("notebook_id", document.NotebookID),
		zap.String("document_id", document.ID),
		zap.String("original_name", document.OriginalName),
		zap.String("mime_type", document.MimeType),
		zap.Int("file_size", len(req.FileData)))
	
	s.logger.Info("=== CALLING STORAGE SERVICE ===")
	storagePath, err := s.storageService.UploadFileToTenantBucket(ctx, spaceCtx.TenantID, storageKey, req.FileData, document.MimeType)
	s.logger.Info("=== STORAGE SERVICE CALL COMPLETED ===", zap.Bool("has_error", err != nil))
	if err != nil {
		s.logger.Error("Failed to upload file to storage",
			zap.String("document_id", document.ID),
			zap.Error(err))

		// Update document status to failed
		if statusErr := s.updateDocumentStatus(ctx, document.ID, "failed", nil, "File upload failed"); statusErr != nil {
			s.logger.Error("Failed to update document status", zap.Error(statusErr))
		}
		return nil, errors.ExternalService("Failed to upload file", err)
	}

	// Parse storage path (format: "bucketName:key") 
	parts := strings.SplitN(storagePath, ":", 2)
	var bucketName, keyPath string
	if len(parts) == 2 {
		bucketName = parts[0]
		keyPath = parts[1]
	} else {
		// Fallback if format is unexpected
		bucketName = fmt.Sprintf("aether-%s", extractTenantSuffix(spaceCtx.TenantID))
		keyPath = storagePath
	}

	// Update document with tenant-scoped storage information
	document.UpdateStorageInfo(keyPath, bucketName)
	if err := s.updateDocumentStorage(ctx, document.ID, keyPath, bucketName); err != nil {
		s.logger.Error("Failed to update document storage info", 
			zap.String("document_id", document.ID),
			zap.String("bucket", bucketName),
			zap.String("key", keyPath),
			zap.Error(err))
	}

	// Submit for processing if processing service is available
	if s.processingService != nil {
		processingConfig := map[string]interface{}{
			"extract_text":     true,
			"extract_metadata": true,
			"file_data":        req.FileData,
			"filename":         document.OriginalName,
			"mime_type":        document.MimeType,
		}

		job, err := s.processingService.SubmitProcessingJob(ctx, document.ID, "extract", processingConfig)
		if err != nil {
			s.logger.Error("Failed to submit processing job - cleaning up document",
				zap.String("document_id", document.ID),
				zap.Error(err))
			
			// Clean up: delete the uploaded file from storage
			if deleteErr := s.storageService.DeleteFileFromTenantBucket(ctx, spaceCtx.TenantID, keyPath); deleteErr != nil {
				s.logger.Error("Failed to clean up file after processing failure",
					zap.String("key", keyPath),
					zap.Error(deleteErr))
			}
			
			// Clean up: delete the document record from database
			if deleteErr := s.deleteDocumentRecord(ctx, document.ID); deleteErr != nil {
				s.logger.Error("Failed to clean up document record after processing failure",
					zap.String("document_id", document.ID),
					zap.Error(deleteErr))
			}
			
			return nil, errors.ServiceUnavailable("Document processing service is currently unavailable. Please try again later.")
		} else {
			document.ProcessingJobID = job.ID
			document.Status = "processing"
			if statusErr := s.updateDocumentStatus(ctx, document.ID, "processing", nil, ""); statusErr != nil {
				s.logger.Error("Failed to update document status", zap.Error(statusErr))
			}
			
			// Document submitted for processing - status will be updated via processing service callback
		}
	} else {
		// No processing service available - fail the upload
		s.logger.Error("No processing service configured - cleaning up document",
			zap.String("document_id", document.ID))
		
		// Clean up: delete the uploaded file from storage
		if deleteErr := s.storageService.DeleteFileFromTenantBucket(ctx, spaceCtx.TenantID, keyPath); deleteErr != nil {
			s.logger.Error("Failed to clean up file after processing service unavailable",
				zap.String("key", keyPath),
				zap.Error(deleteErr))
		}
		
		// Clean up: delete the document record from database
		if deleteErr := s.deleteDocumentRecord(ctx, document.ID); deleteErr != nil {
			s.logger.Error("Failed to clean up document record after processing service unavailable",
				zap.String("document_id", document.ID),
				zap.Error(deleteErr))
		}
		
		return nil, errors.ServiceUnavailable("Document processing service is not configured. Please contact support.")
	}

	s.logger.Info("Document uploaded successfully",
		zap.String("document_id", document.ID),
		zap.String("storage_path", storagePath),
	)

	return document, nil
}

// GetDocumentByID retrieves a document by ID
func (s *DocumentService) GetDocumentByID(ctx context.Context, documentID string, userID string, spaceCtx *models.SpaceContext) (*models.Document, error) {
	query := `
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id})
		OPTIONAL MATCH (d)-[:BELONGS_TO]->(n:Notebook)
		OPTIONAL MATCH (d)-[:OWNED_BY]->(owner:User)
		RETURN d.id, d.name, d.description, d.type, d.status, d.original_name,
		       d.mime_type, d.size_bytes, d.checksum, d.storage_path, d.storage_bucket,
		       d.extracted_text, d.processing_result, d.processing_time, d.confidence_score, d.metadata, d.notebook_id, d.owner_id,
		       d.space_type, d.space_id, d.tenant_id,
		       d.tags, d.search_text, d.processing_job_id, d.processed_at,
		       d.created_at, d.updated_at,
		       n.name as notebook_name, n.visibility as notebook_visibility,
		       owner.username, owner.full_name, owner.avatar_url
	`

	params := map[string]interface{}{
		"document_id": documentID,
		"tenant_id":   spaceCtx.TenantID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get document by ID", zap.String("document_id", documentID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve document", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Document not found", map[string]interface{}{
			"document_id": documentID,
		})
	}

	document, err := s.recordToDocument(result.Records[0])
	if err != nil {
		return nil, err
	}

	// Validate document belongs to the correct space
	if document.SpaceID != spaceCtx.SpaceID || document.TenantID != spaceCtx.TenantID {
		return nil, errors.ForbiddenWithDetails("Document not accessible in this space", map[string]interface{}{
			"document_id": documentID,
			"space_id":    spaceCtx.SpaceID,
		})
	}

	// Check if user has read permissions in the space
	if !spaceCtx.CanRead() {
		return nil, errors.Forbidden("Insufficient permissions to read document")
	}

	return document, nil
}

// UpdateDocument updates a document
func (s *DocumentService) UpdateDocument(ctx context.Context, documentID string, req models.DocumentUpdateRequest, userID string, spaceCtx *models.SpaceContext) (*models.Document, error) {
	// Get current document and check permissions
	document, err := s.GetDocumentByID(ctx, documentID, userID, spaceCtx)
	if err != nil {
		return nil, err
	}

	// Check if user can write to document
	if !s.canUserWriteDocument(ctx, document, userID) {
		return nil, errors.Forbidden("Write access denied to document")
	}

	// Update document fields
	document.Update(req)

	// Update in Neo4j
	query := `
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id})
		SET d.name = $name,
		    d.description = $description,
		    d.status = $status,
		    d.tags = $tags,
		    d.search_text = $search_text,
		    d.metadata = $metadata,
		    d.updated_at = datetime($updated_at)
		RETURN d
	`

	params := map[string]interface{}{
		"document_id": documentID,
		"tenant_id":   spaceCtx.TenantID,
		"name":        document.Name,
		"description": document.Description,
		"status":      document.Status,
		"tags":        document.Tags,
		"search_text": document.SearchText,
		"metadata":    document.Metadata,
		"updated_at":  document.UpdatedAt.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update document", zap.String("document_id", documentID), zap.Error(err))
		return nil, errors.Database("Failed to update document", err)
	}

	s.logger.Info("Document updated successfully",
		zap.String("document_id", documentID),
		zap.String("name", document.Name),
	)

	return document, nil
}

// DeleteDocument deletes a document (soft delete)
func (s *DocumentService) DeleteDocument(ctx context.Context, documentID string, userID string, spaceCtx *models.SpaceContext) error {
	// Get document and check permissions
	document, err := s.GetDocumentByID(ctx, documentID, userID, spaceCtx)
	if err != nil {
		return err
	}

	// Check if user can delete document (must be owner)
	if document.OwnerID != userID {
		return errors.Forbidden("Only document owner can delete document")
	}

	// Soft delete: update status to deleted and update notebook counts
	query := `
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id})
		MATCH (d)-[:BELONGS_TO]->(n:Notebook {tenant_id: $tenant_id})
		SET d.status = 'deleted',
		    d.deleted_at = datetime(),
		    d.updated_at = datetime($updated_at),
		    n.document_count = COALESCE(n.document_count, 0) - 1,
		    n.total_size_bytes = COALESCE(n.total_size_bytes, 0) - d.size_bytes,
		    n.updated_at = datetime()
		RETURN d
	`

	params := map[string]interface{}{
		"document_id": documentID,
		"tenant_id":   spaceCtx.TenantID,
		"updated_at":  time.Now().Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to delete document", zap.String("document_id", documentID), zap.Error(err))
		return errors.Database("Failed to delete document", err)
	}

	// Cancel processing job if active
	if document.ProcessingJobID != "" && s.processingService != nil {
		// First try to get the job to retrieve the AudiModal file ID
		job, jobErr := s.processingService.GetProcessingJob(ctx, document.ProcessingJobID)
		if jobErr == nil && job != nil && job.Config != nil {
			// Check if we have an AudiModal file ID to delete
			if fileID, ok := job.Config["audimodal_file_id"].(string); ok && fileID != "" {
				// Try to cast processing service to AudiModalService to access DeleteFile
				if audiModalService, ok := s.processingService.(*AudiModalService); ok {
					s.logger.Info("Deleting file from AudiModal",
						zap.String("document_id", documentID),
						zap.String("audimodal_file_id", fileID))
					
					if deleteErr := audiModalService.DeleteFile(ctx, fileID); deleteErr != nil {
						s.logger.Error("Failed to delete file from AudiModal",
							zap.String("document_id", documentID),
							zap.String("audimodal_file_id", fileID),
							zap.Error(deleteErr))
					}
				}
			}
		}
		
		// Cancel the processing job
		if err := s.processingService.CancelProcessingJob(ctx, document.ProcessingJobID); err != nil {
			s.logger.Warn("Failed to cancel processing job",
				zap.String("document_id", documentID),
				zap.String("job_id", document.ProcessingJobID),
				zap.Error(err))
		}
	}

	// Delete the actual file from storage if it exists and storage service is available
	if s.storageService != nil && document.StoragePath != "" {
		// Extract the key from storage path (supports both "bucket:key" and legacy "key" formats)
		var key string
		if strings.Contains(document.StoragePath, ":") {
			// New format: "bucket:key"
			parts := strings.SplitN(document.StoragePath, ":", 2)
			if len(parts) == 2 {
				key = parts[1]
			} else {
				s.logger.Error("Invalid storage path format during deletion", 
					zap.String("document_id", documentID),
					zap.String("storage_path", document.StoragePath))
				key = document.StoragePath // fallback to full path
			}
		} else {
			// Legacy format: just the key
			key = document.StoragePath
		}

		s.logger.Info("Deleting file from storage",
			zap.String("document_id", documentID),
			zap.String("tenant_id", spaceCtx.TenantID),
			zap.String("key", key))

		if err := s.storageService.DeleteFileFromTenantBucket(ctx, spaceCtx.TenantID, key); err != nil {
			// Log the error but don't fail the entire delete operation since the database record is already marked as deleted
			s.logger.Error("Failed to delete file from storage (database record already deleted)",
				zap.String("document_id", documentID),
				zap.String("tenant_id", spaceCtx.TenantID),
				zap.String("key", key),
				zap.Error(err))
		} else {
			s.logger.Info("File deleted from storage successfully",
				zap.String("document_id", documentID),
				zap.String("key", key))
		}
	} else if document.StoragePath != "" {
		s.logger.Warn("Storage service not available, cannot delete file from storage",
			zap.String("document_id", documentID),
			zap.String("storage_path", document.StoragePath))
	}

	s.logger.Info("Document deleted successfully",
		zap.String("document_id", documentID),
		zap.String("name", document.Name),
	)

	return nil
}

// ListDocumentsByNotebook lists documents in a notebook
func (s *DocumentService) ListDocumentsByNotebook(ctx context.Context, notebookID string, userID string, spaceCtx *models.SpaceContext, limit, offset int) (*models.DocumentListResponse, error) {
	// Check if user has read permissions in the space
	if !spaceCtx.CanRead() {
		return nil, errors.Forbidden("Insufficient permissions to list documents")
	}

	// Verify notebook exists and belongs to the correct space
	notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
	if err != nil {
		return nil, err
	}

	if notebook.TenantID != spaceCtx.TenantID || notebook.SpaceID != spaceCtx.SpaceID {
		return nil, errors.ForbiddenWithDetails("Notebook not accessible in this space", map[string]interface{}{
			"notebook_id": notebookID,
			"space_id":    spaceCtx.SpaceID,
		})
	}

	// Set defaults
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		MATCH (d:Document {notebook_id: $notebook_id, tenant_id: $tenant_id})
		WHERE d.status <> 'deleted'
		OPTIONAL MATCH (d)-[:OWNED_BY]->(owner:User)
		RETURN d.id, d.name, d.description, d.type, d.status, d.original_name,
		       d.mime_type, d.size_bytes, d.notebook_id, d.owner_id, 
		       d.space_type, d.space_id, d.tenant_id, d.tags,
		       d.processed_at, d.created_at, d.updated_at,
		       owner.username, owner.full_name, owner.avatar_url
		ORDER BY d.created_at DESC
		SKIP $offset
		LIMIT $limit
	`

	params := map[string]interface{}{
		"notebook_id": notebookID,
		"tenant_id":   spaceCtx.TenantID,
		"limit":       limit + 1, // Get one extra to check if there are more
		"offset":      offset,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to list documents", zap.Error(err))
		return nil, errors.Database("Failed to list documents", err)
	}

	documents := make([]*models.DocumentResponse, 0, len(result.Records))
	hasMore := false

	for i, record := range result.Records {
		if i >= limit {
			hasMore = true
			break
		}

		document, err := s.recordToDocumentResponse(record)
		if err != nil {
			s.logger.Error("Failed to parse document record", zap.Error(err))
			continue
		}

		documents = append(documents, document)
	}

	// Get total count
	countQuery := `
		MATCH (d:Document {notebook_id: $notebook_id, tenant_id: $tenant_id})
		WHERE d.status <> 'deleted'
		RETURN count(d) as total
	`

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, map[string]interface{}{
		"notebook_id": notebookID,
		"tenant_id":   spaceCtx.TenantID,
	})
	if err != nil {
		s.logger.Error("Failed to get document count", zap.Error(err))
		return nil, errors.Database("Failed to get document count", err)
	}

	total := 0
	if len(countResult.Records) > 0 {
		if totalValue, found := countResult.Records[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return &models.DocumentListResponse{
		Documents: documents,
		Total:     total,
		Limit:     limit,
		Offset:    offset,
		HasMore:   hasMore,
	}, nil
}

// SearchDocuments searches for documents within a space
func (s *DocumentService) SearchDocuments(ctx context.Context, req models.DocumentSearchRequest, userID string, spaceCtx *models.SpaceContext) (*models.DocumentListResponse, error) {
	// Set defaults
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	// Check if user has read permissions in the space
	if !spaceCtx.CanRead() {
		return nil, errors.Forbidden("Insufficient permissions to search documents")
	}

	// Build query conditions - filter by space
	whereConditions := []string{
		"d.status <> 'deleted'",
		"d.tenant_id = $tenant_id",
		"d.space_id = $space_id",
	}
	
	params := map[string]interface{}{
		"user_id":   userID,
		"tenant_id": spaceCtx.TenantID,
		"space_id":  spaceCtx.SpaceID,
		"limit":     req.Limit + 1,
		"offset":    req.Offset,
	}

	if req.Query != "" {
		whereConditions = append(whereConditions, "d.search_text CONTAINS $query")
		params["query"] = req.Query
	}

	if req.NotebookID != "" {
		whereConditions = append(whereConditions, "d.notebook_id = $notebook_id")
		params["notebook_id"] = req.NotebookID
	}

	if req.OwnerID != "" {
		whereConditions = append(whereConditions, "d.owner_id = $owner_id")
		params["owner_id"] = req.OwnerID
	}

	if req.Type != "" {
		whereConditions = append(whereConditions, "d.type = $type")
		params["type"] = req.Type
	}

	if req.Status != "" {
		whereConditions = append(whereConditions, "d.status = $status")
		params["status"] = req.Status
	}

	if req.MimeType != "" {
		whereConditions = append(whereConditions, "d.mime_type = $mime_type")
		params["mime_type"] = req.MimeType
	}

	if len(req.Tags) > 0 {
		whereConditions = append(whereConditions, "ANY(tag IN $tags WHERE tag IN d.tags)")
		params["tags"] = req.Tags
	}

	whereClause := "WHERE " + fmt.Sprintf("(%s)", whereConditions[0])
	for i := 1; i < len(whereConditions); i++ {
		whereClause += " AND " + fmt.Sprintf("(%s)", whereConditions[i])
	}

	query := fmt.Sprintf(`
		MATCH (d:Document)
		%s
		OPTIONAL MATCH (d)-[:OWNED_BY]->(owner:User)
		OPTIONAL MATCH (d)-[:BELONGS_TO]->(n:Notebook)
		RETURN d.id, d.name, d.description, d.type, d.status, d.original_name,
		       d.mime_type, d.size_bytes, d.notebook_id, d.owner_id, d.tags,
		       d.processed_at, d.created_at, d.updated_at,
		       owner.username, owner.full_name, owner.avatar_url,
		       n.name as notebook_name
		ORDER BY d.updated_at DESC
		SKIP $offset
		LIMIT $limit
	`, whereClause)

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to search documents", zap.Error(err))
		return nil, errors.Database("Failed to search documents", err)
	}

	documents := make([]*models.DocumentResponse, 0, len(result.Records))
	hasMore := false

	for i, record := range result.Records {
		if i >= req.Limit {
			hasMore = true
			break
		}

		document, err := s.recordToDocumentResponse(record)
		if err != nil {
			s.logger.Error("Failed to parse document record", zap.Error(err))
			continue
		}

		documents = append(documents, document)
	}

	return &models.DocumentListResponse{
		Documents: documents,
		Total:     len(documents), // For search, we don't compute exact total
		Limit:     req.Limit,
		Offset:    req.Offset,
		HasMore:   hasMore,
	}, nil
}

// UpdateProcessingResult updates document processing results
// NOTE: This is called by external services (e.g., processing workers) that don't have space context
// We first retrieve the document to get its tenant_id for proper isolation
func (s *DocumentService) UpdateProcessingResult(ctx context.Context, documentID string, status string, result map[string]interface{}, errorMsg string) error {
	// First get the document's tenant_id
	tenantQuery := `
		MATCH (d:Document {id: $document_id})
		RETURN d.tenant_id as tenant_id
	`
	
	tenantResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, tenantQuery, map[string]interface{}{
		"document_id": documentID,
	})
	if err != nil {
		return errors.Database("Failed to get document tenant", err)
	}
	
	if len(tenantResult.Records) == 0 {
		return errors.NotFound("Document not found")
	}
	
	tenantID := ""
	if val, ok := tenantResult.Records[0].Get("tenant_id"); ok && val != nil {
		tenantID = val.(string)
	}
	
	return s.updateProcessingResultWithTenant(ctx, documentID, tenantID, status, result, errorMsg)
}

// updateProcessingResultWithTenant is the internal version that includes tenant_id
func (s *DocumentService) updateProcessingResultWithTenant(ctx context.Context, documentID string, tenantID string, status string, result map[string]interface{}, errorMsg string) error {
	query := `
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id})
		SET d.status = $status,
		    d.processing_result = $result,
		    d.extracted_text = $extracted_text,
		    d.search_text = $search_text,
		    d.processed_at = CASE WHEN $status = 'processed' THEN datetime($processed_at) ELSE d.processed_at END,
		    d.updated_at = datetime($updated_at)
		RETURN d
	`

	extractedText := ""
	if result != nil && result["extracted_text"] != nil {
		if text, ok := result["extracted_text"].(string); ok {
			// Validate extracted text is not placeholder/sample content
			if s.isPlaceholderText(text) {
				s.logger.Warn("Detected placeholder text in processing result - rejecting update", 
					zap.String("document_id", documentID),
					zap.String("text_preview", text[:min(100, len(text))]),
				)
				return fmt.Errorf("extracted text appears to be placeholder content - processing may have failed")
			}
			extractedText = text
		}
	}

	searchText := ""
	if extractedText != "" {
		// Get current document to build search text
		doc, err := s.getDocumentByIDInternal(ctx, documentID, tenantID)
		if err == nil {
			searchText = fmt.Sprintf("%s %s", doc.SearchText, extractedText)
		}
	}

	params := map[string]interface{}{
		"document_id":    documentID,
		"tenant_id":      tenantID,
		"status":         status,
		"result":         result,
		"extracted_text": extractedText,
		"search_text":    searchText,
		"processed_at":   time.Now().Format(time.RFC3339),
		"updated_at":     time.Now().Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update processing result",
			zap.String("document_id", documentID),
			zap.Error(err))
		return errors.Database("Failed to update processing result", err)
	}

	// Monitor and log processing results for alerting/metrics
	s.monitorProcessingResult(ctx, documentID, tenantID, status, extractedText, errorMsg)

	s.logger.Info("Document processing result updated",
		zap.String("document_id", documentID),
		zap.String("status", status),
	)

	return nil
}

// Helper methods (simplified implementations)

func (s *DocumentService) verifyNotebookAccess(ctx context.Context, notebookID, userID string) (bool, error) {
	query := `
		MATCH (n:Notebook {id: $notebook_id})
		WHERE n.visibility = 'public' OR 
		      n.owner_id = $user_id OR 
		      EXISTS((n)-[:SHARED_WITH]->(:User {id: $user_id}))
		RETURN count(n) > 0 as has_access
	`

	params := map[string]interface{}{
		"notebook_id": notebookID,
		"user_id":     userID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return false, err
	}

	if len(result.Records) > 0 {
		if hasAccess, found := result.Records[0].Get("has_access"); found {
			if hasAccessBool, ok := hasAccess.(bool); ok {
				return hasAccessBool, nil
			}
		}
	}

	return false, nil
}

func (s *DocumentService) createDocumentRelationships(ctx context.Context, documentID, notebookID, ownerID string, tenantID string, sizeBytes int64) error {
	query := `
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id}), 
		      (n:Notebook {id: $notebook_id, tenant_id: $tenant_id}), 
		      (u:User {id: $owner_id})
		CREATE (d)-[:BELONGS_TO]->(n), (d)-[:OWNED_BY]->(u)
		WITH n, d
		SET n.document_count = COALESCE(n.document_count, 0) + 1,
		    n.total_size_bytes = COALESCE(n.total_size_bytes, 0) + d.size_bytes,
		    n.updated_at = datetime()
	`

	params := map[string]interface{}{
		"document_id": documentID,
		"notebook_id": notebookID,
		"owner_id":    ownerID,
		"tenant_id":   tenantID,
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	return err
}

func (s *DocumentService) updateDocumentStatus(ctx context.Context, documentID, status string, result map[string]interface{}, errorMsg string) error {
	// First get the document's tenant_id
	tenantQuery := `
		MATCH (d:Document {id: $document_id})
		RETURN d.tenant_id as tenant_id
	`
	
	tenantResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, tenantQuery, map[string]interface{}{
		"document_id": documentID,
	})
	if err != nil {
		return err
	}
	
	if len(tenantResult.Records) == 0 {
		return errors.NotFound("Document not found")
	}
	
	tenantID := ""
	if val, ok := tenantResult.Records[0].Get("tenant_id"); ok && val != nil {
		tenantID = val.(string)
	}

	// Build the SET clause based on what needs updating
	setClauses := []string{
		"d.status = $status",
		"d.updated_at = datetime($updated_at)",
	}
	
	params := map[string]interface{}{
		"document_id": documentID,
		"tenant_id":   tenantID,
		"status":      status,
		"updated_at":  time.Now().Format(time.RFC3339),
	}
	
	// Add processing result if provided
	if result != nil && len(result) > 0 {
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("failed to marshal processing result: %w", err)
		}
		setClauses = append(setClauses, "d.processing_result = $processing_result")
		params["processing_result"] = string(resultJSON)
		
		// Extract text if available in result
		if extractedText, ok := result["extracted_text"].(string); ok {
			setClauses = append(setClauses, "d.extracted_text = $extracted_text")
			params["extracted_text"] = extractedText
			
			// Update search text with extracted content
			setClauses = append(setClauses, "d.search_text = d.name + ' ' + COALESCE(d.description, '') + ' ' + $extracted_text")
		}
		
		// Set processed_at for processed status
		if status == "processed" {
			setClauses = append(setClauses, "d.processed_at = datetime($processed_at)")
			params["processed_at"] = time.Now().Format(time.RFC3339)
		}
	}
	
	// Add error message if provided
	if errorMsg != "" {
		setClauses = append(setClauses, "d.error = $error")
		params["error"] = errorMsg
	}
	
	query := fmt.Sprintf(`
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id})
		SET %s
		RETURN d
	`, strings.Join(setClauses, ", "))

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	return err
}

func (s *DocumentService) updateDocumentStorage(ctx context.Context, documentID, storagePath, storageBucket string) error {
	// First get the document's tenant_id
	tenantQuery := `
		MATCH (d:Document {id: $document_id})
		RETURN d.tenant_id as tenant_id
	`
	
	tenantResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, tenantQuery, map[string]interface{}{
		"document_id": documentID,
	})
	if err != nil {
		return err
	}
	
	if len(tenantResult.Records) == 0 {
		return errors.NotFound("Document not found")
	}
	
	tenantID := ""
	if val, ok := tenantResult.Records[0].Get("tenant_id"); ok && val != nil {
		tenantID = val.(string)
	}

	query := `
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id})
		SET d.storage_path = $storage_path,
		    d.storage_bucket = $storage_bucket,
		    d.updated_at = datetime($updated_at)
		RETURN d
	`

	params := map[string]interface{}{
		"document_id":    documentID,
		"tenant_id":      tenantID,
		"storage_path":   storagePath,
		"storage_bucket": storageBucket,
		"updated_at":     time.Now().Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	return err
}

func (s *DocumentService) canUserAccessDocument(ctx context.Context, document *models.Document, userID string) bool {
	// Owner can always access
	if document.OwnerID == userID {
		return true
	}

	// Check notebook access
	hasAccess, err := s.verifyNotebookAccess(ctx, document.NotebookID, userID)
	if err != nil {
		return false
	}

	return hasAccess
}

func (s *DocumentService) canUserWriteDocument(ctx context.Context, document *models.Document, userID string) bool {
	// Owner can always write
	if document.OwnerID == userID {
		return true
	}

	// TODO: Check write permissions from notebook sharing
	return false
}

// getDocumentByIDInternal is an internal helper that retrieves a document with tenant isolation
// Used by internal operations that don't have access to space context
func (s *DocumentService) getDocumentByIDInternal(ctx context.Context, documentID string, tenantID string) (*models.Document, error) {
	query := `
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id})
		RETURN d.id, d.name, d.description, d.type, d.status, d.original_name,
		       d.mime_type, d.size_bytes, d.checksum, d.storage_path, d.storage_bucket,
		       d.extracted_text, d.processing_result, d.processing_time, d.confidence_score, d.metadata, d.notebook_id, d.owner_id,
		       d.space_type, d.space_id, d.tenant_id,
		       d.tags, d.search_text, d.processing_job_id, d.processed_at,
		       d.created_at, d.updated_at
	`

	params := map[string]interface{}{
		"document_id": documentID,
		"tenant_id":   tenantID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return nil, errors.Database("Failed to retrieve document", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFound("Document not found")
	}

	return s.recordToDocument(result.Records[0])
}

func (s *DocumentService) recordToDocument(record interface{}) (*models.Document, error) {
	r, ok := record.(*neo4j.Record)
	if !ok {
		return nil, errors.Internal("Invalid record type")
	}

	document := &models.Document{}

	// Extract basic fields
	if val, ok := r.Get("d.id"); ok && val != nil {
		document.ID = val.(string)
	}
	if val, ok := r.Get("d.name"); ok && val != nil {
		document.Name = val.(string)
	}
	if val, ok := r.Get("d.description"); ok && val != nil {
		document.Description = val.(string)
	}
	if val, ok := r.Get("d.type"); ok && val != nil {
		document.Type = val.(string)
	}
	if val, ok := r.Get("d.status"); ok && val != nil {
		document.Status = val.(string)
	}
	if val, ok := r.Get("d.original_name"); ok && val != nil {
		document.OriginalName = val.(string)
	}
	if val, ok := r.Get("d.mime_type"); ok && val != nil {
		document.MimeType = val.(string)
	}
	if val, ok := r.Get("d.size_bytes"); ok && val != nil {
		if size, ok := val.(int64); ok {
			document.SizeBytes = size
		}
	}
	if val, ok := r.Get("d.checksum"); ok && val != nil {
		document.Checksum = val.(string)
	}
	if val, ok := r.Get("d.storage_path"); ok && val != nil {
		document.StoragePath = val.(string)
	}
	if val, ok := r.Get("d.storage_bucket"); ok && val != nil {
		document.StorageBucket = val.(string)
	}
	if val, ok := r.Get("d.extracted_text"); ok && val != nil {
		document.ExtractedText = val.(string)
	}
	if val, ok := r.Get("d.processing_time"); ok && val != nil {
		if processingTime, ok := val.(int64); ok {
			document.ProcessingTime = &processingTime
		}
	}
	if val, ok := r.Get("d.confidence_score"); ok && val != nil {
		if confidenceScore, ok := val.(float64); ok {
			document.ConfidenceScore = &confidenceScore
		}
	}
	if val, ok := r.Get("d.notebook_id"); ok && val != nil {
		document.NotebookID = val.(string)
	}
	if val, ok := r.Get("d.owner_id"); ok && val != nil {
		document.OwnerID = val.(string)
	}

	// Extract space fields
	if val, ok := r.Get("d.space_type"); ok && val != nil {
		document.SpaceType = models.SpaceType(val.(string))
	}
	if val, ok := r.Get("d.space_id"); ok && val != nil {
		document.SpaceID = val.(string)
	}
	if val, ok := r.Get("d.tenant_id"); ok && val != nil {
		document.TenantID = val.(string)
	}

	// Extract tags
	if val, ok := r.Get("d.tags"); ok && val != nil {
		if tags, ok := val.([]interface{}); ok {
			document.Tags = make([]string, len(tags))
			for i, tag := range tags {
				document.Tags[i] = tag.(string)
			}
		}
	}

	// Extract timestamps
	if val, ok := r.Get("d.created_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			document.CreatedAt = t
		} else if str, ok := val.(string); ok && str != "" {
			if t, err := time.Parse(time.RFC3339, str); err == nil {
				document.CreatedAt = t
			}
		}
	}
	if val, ok := r.Get("d.updated_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			document.UpdatedAt = t
		} else if str, ok := val.(string); ok && str != "" {
			if t, err := time.Parse(time.RFC3339, str); err == nil {
				document.UpdatedAt = t
			}
		}
	}
	if val, ok := r.Get("d.processed_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			document.ProcessedAt = &t
		}
	}

	return document, nil
}

// Helper function to check if an interface has a Get method like neo4j.Record
func hasGetMethod(record interface{}) bool {
	if record == nil {
		return false
	}
	recordValue := reflect.ValueOf(record)
	recordType := recordValue.Type()
	
	// Check if it has a Get method
	_, hasGet := recordType.MethodByName("Get")
	return hasGet
}

// Generic record processor that works with any type that has Get(string) method
func (s *DocumentService) recordToDocumentResponseGeneric(record interface{}) (*models.DocumentResponse, error) {
	recordValue := reflect.ValueOf(record)
	
	// Helper function to safely get values using reflection
	getValue := func(key string) interface{} {
		getMethod := recordValue.MethodByName("Get")
		if !getMethod.IsValid() {
			return nil
		}
		
		results := getMethod.Call([]reflect.Value{reflect.ValueOf(key)})
		if len(results) >= 2 {
			// Get method typically returns (value, found)
			if found := results[1]; found.Kind() == reflect.Bool && found.Bool() {
				return results[0].Interface()
			}
		} else if len(results) == 1 {
			// Some implementations might just return value
			return results[0].Interface()
		}
		return nil
	}
	
	// Helper function to safely get string values
	getString := func(key string) string {
		if val := getValue(key); val != nil {
			if str, ok := val.(string); ok {
				return str
			}
		}
		return ""
	}

	// Helper function to safely get int64 values
	getInt64 := func(key string) int64 {
		if val := getValue(key); val != nil {
			if i, ok := val.(int64); ok {
				return i
			}
		}
		return 0
	}

	// Helper function to safely get time values
	getTime := func(key string) time.Time {
		if val := getValue(key); val != nil {
			if str, ok := val.(string); ok && str != "" {
				if t, err := time.Parse(time.RFC3339, str); err == nil {
					return t
				}
			}
		}
		return time.Time{}
	}

	// Helper function to safely get time pointer values
	getTimePtr := func(key string) *time.Time {
		if val := getValue(key); val != nil {
			if str, ok := val.(string); ok && str != "" {
				if t, err := time.Parse(time.RFC3339, str); err == nil {
					return &t
				}
			}
		}
		return nil
	}

	// Extract fields using the generic approach
	response := &models.DocumentResponse{
		ID:           getString("d.id"),
		Name:         getString("d.name"),
		Description:  getString("d.description"),
		Type:         getString("d.type"),
		Status:       getString("d.status"),
		OriginalName: getString("d.original_name"),
		MimeType:     getString("d.mime_type"),
		SizeBytes:    getInt64("d.size_bytes"),
		NotebookID:   getString("d.notebook_id"),
		OwnerID:      getString("d.owner_id"),
		ProcessedAt:  getTimePtr("d.processed_at"),
		CreatedAt:    getTime("d.created_at"),
		UpdatedAt:    getTime("d.updated_at"),
	}

	// Extract tags if available
	if tagsVal := getValue("d.tags"); tagsVal != nil {
		if tagsList, ok := tagsVal.([]interface{}); ok {
			tags := make([]string, 0, len(tagsList))
			for _, tag := range tagsList {
				if tagStr, ok := tag.(string); ok {
					tags = append(tags, tagStr)
				}
			}
			response.Tags = tags
		}
	}

	// Add owner info if available
	if ownerUsername := getString("owner.username"); ownerUsername != "" {
		response.Owner = &models.PublicUserResponse{
			ID:       getString("owner.id"),
			Username: ownerUsername,
			FullName: getString("owner.full_name"),
		}
	}

	return response, nil
}

func (s *DocumentService) recordToDocumentResponse(record interface{}) (*models.DocumentResponse, error) {
	// Cast record to proper type - handle multiple possible record types
	var neo4jRecord neo4j.Record
	var ok bool
	
	// Try different possible types
	switch r := record.(type) {
	case neo4j.Record:
		neo4jRecord = r
		ok = true
	case *neo4j.Record:
		neo4jRecord = *r
		ok = true
	default:
		// Try reflection approach for wrapped records
		if hasGetMethod(record) {
			// If it has a Get method like neo4j.Record, we can work with it directly
			return s.recordToDocumentResponseGeneric(record)
		}
		s.logger.Error("Invalid record type in recordToDocumentResponse", 
			zap.String("type", fmt.Sprintf("%T", record)),
			zap.String("expected", "neo4j.Record"))
		return nil, fmt.Errorf("invalid record type: %T", record)
	}
	
	if !ok {
		return nil, fmt.Errorf("failed to convert record to neo4j.Record")
	}

	// Helper function to safely get string values
	getString := func(key string) string {
		if val, found := neo4jRecord.Get(key); found && val != nil {
			if str, ok := val.(string); ok {
				return str
			}
		}
		return ""
	}

	// Helper function to safely get int64 values
	getInt64 := func(key string) int64 {
		if val, found := neo4jRecord.Get(key); found && val != nil {
			if i, ok := val.(int64); ok {
				return i
			}
		}
		return 0
	}

	// Helper function to safely get time values
	getTime := func(key string) time.Time {
		if val, found := neo4jRecord.Get(key); found && val != nil {
			if str, ok := val.(string); ok && str != "" {
				if t, err := time.Parse(time.RFC3339, str); err == nil {
					return t
				}
			}
		}
		return time.Time{}
	}

	// Helper function to safely get time pointer values
	getTimePtr := func(key string) *time.Time {
		if val, found := neo4jRecord.Get(key); found && val != nil {
			if str, ok := val.(string); ok && str != "" {
				if t, err := time.Parse(time.RFC3339, str); err == nil {
					return &t
				}
			}
		}
		return nil
	}

	// Helper function to safely get string array values
	getStringArray := func(key string) []string {
		if val, found := neo4jRecord.Get(key); found && val != nil {
			if arr, ok := val.([]interface{}); ok {
				result := make([]string, 0, len(arr))
				for _, item := range arr {
					if str, ok := item.(string); ok {
						result = append(result, str)
					}
				}
				return result
			}
		}
		return []string{}
	}

	// Build the DocumentResponse
	doc := &models.DocumentResponse{
		ID:           getString("d.id"),
		Name:         getString("d.name"),
		Description:  getString("d.description"),
		Type:         getString("d.type"),
		Status:       getString("d.status"),
		OriginalName: getString("d.original_name"),
		MimeType:     getString("d.mime_type"),
		SizeBytes:    getInt64("d.size_bytes"),
		NotebookID:   getString("d.notebook_id"),
		OwnerID:      getString("d.owner_id"),
		Tags:         getStringArray("d.tags"),
		ProcessedAt:  getTimePtr("d.processed_at"),
		CreatedAt:    getTime("d.created_at"),
		UpdatedAt:    getTime("d.updated_at"),
	}

	// Add owner information if available
	ownerUsername := getString("owner.username")
	ownerFullName := getString("owner.full_name")
	ownerAvatarURL := getString("owner.avatar_url")
	
	if ownerUsername != "" || ownerFullName != "" {
		doc.Owner = &models.PublicUserResponse{
			ID:        doc.OwnerID,
			Username:  ownerUsername,
			FullName:  ownerFullName,
			AvatarURL: ownerAvatarURL,
		}
	}

	return doc, nil
}

// DownloadDocumentFile downloads the file content for a document
func (s *DocumentService) DownloadDocumentFile(ctx context.Context, documentID, userID string, spaceContext *models.SpaceContext) ([]byte, *models.Document, error) {
	s.logger.Info("Starting document file download", 
		zap.String("document_id", documentID),
		zap.String("user_id", userID),
	)

	// First, get the document to verify access and get storage info
	document, err := s.GetDocumentByID(ctx, documentID, userID, spaceContext)
	if err != nil {
		s.logger.Error("Failed to get document for download", 
			zap.String("document_id", documentID),
			zap.Error(err),
		)
		return nil, nil, fmt.Errorf("failed to get document: %w", err)
	}

	// Check if the document has storage path
	if document.StoragePath == "" {
		s.logger.Error("Document has no storage path", 
			zap.String("document_id", documentID),
		)
		return nil, nil, fmt.Errorf("document file not available for download")
	}

	// Extract the key from storage path (supports both "bucket:key" and legacy "key" formats)
	var key string
	if strings.Contains(document.StoragePath, ":") {
		// New format: "bucket:key"
		parts := strings.SplitN(document.StoragePath, ":", 2)
		if len(parts) == 2 {
			key = parts[1]
		} else {
			s.logger.Error("Invalid storage path format", 
				zap.String("document_id", documentID),
				zap.String("storage_path", document.StoragePath),
			)
			return nil, nil, fmt.Errorf("invalid storage path format")
		}
	} else {
		// Legacy format: just the key (backward compatibility)
		key = document.StoragePath
	}

	// Download the file using the storage service
	if s.storageService == nil {
		s.logger.Error("Storage service not available for download",
			zap.String("document_id", documentID),
		)
		return nil, nil, fmt.Errorf("storage service not available")
	}

	fileData, err := s.storageService.DownloadFileFromTenantBucket(ctx, spaceContext.TenantID, key)
	if err != nil {
		s.logger.Error("Failed to download file from storage", 
			zap.String("document_id", documentID),
			zap.String("key", key),
			zap.String("tenant_id", spaceContext.TenantID),
			zap.Error(err),
		)
		return nil, nil, fmt.Errorf("failed to download file: %w", err)
	}

	s.logger.Info("Document file downloaded successfully", 
		zap.String("document_id", documentID),
		zap.String("original_name", document.OriginalName),
		zap.Int("size_bytes", len(fileData)),
	)

	return fileData, document, nil
}

// ReprocessDocument resubmits a document for text extraction processing
func (s *DocumentService) ReprocessDocument(ctx context.Context, document *models.Document, spaceContext *models.SpaceContext) (*models.ProcessingJob, error) {
	s.logger.Info("Starting document reprocessing", 
		zap.String("document_id", document.ID),
		zap.String("original_name", document.OriginalName),
		zap.String("tenant_id", spaceContext.TenantID),
	)

	// Validate document can be reprocessed
	if document.StoragePath == "" {
		return nil, fmt.Errorf("document has no storage path - cannot reprocess")
	}

	// Set document status to processing
	err := s.updateDocumentStatus(ctx, document.ID, "processing", nil, "")
	if err != nil {
		s.logger.Error("Failed to update document status for reprocessing",
			zap.String("document_id", document.ID),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to update document status: %w", err)
	}

	// Clear previous extracted text and processing results
	err = s.clearDocumentProcessingData(ctx, document.ID, spaceContext.TenantID)
	if err != nil {
		s.logger.Error("Failed to clear previous processing data",
			zap.String("document_id", document.ID),
			zap.Error(err),
		)
		// Continue anyway - this is not critical
	}

	// Create processing job
	job := &models.ProcessingJob{
		ID:          uuid.New().String(),
		DocumentID:  document.ID,
		Type:        "reprocess_document",
		Status:      "pending",
		Priority:    1, // High priority for reprocessing
		Config: map[string]interface{}{
			"original_name": document.OriginalName,
			"mime_type": document.MimeType,
			"reprocessing": true,
			"reason": "manual_reprocess",
			"created_by": spaceContext.UserID,
			"tenant_id": spaceContext.TenantID,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Submit processing job
	if s.processingService != nil {
		submittedJob, err := s.processingService.SubmitProcessingJob(ctx, document.ID, "reprocess_document", job.Config)
		if err != nil {
			s.logger.Error("Failed to submit reprocessing job to processing service",
				zap.String("document_id", document.ID),
				zap.String("job_id", job.ID),
				zap.Error(err),
			)
			
			// Revert document status back to its previous state
			_ = s.updateDocumentStatus(ctx, document.ID, document.Status, nil, "")
			
			return nil, fmt.Errorf("failed to submit reprocessing job: %w", err)
		}
		job = submittedJob
	}

	s.logger.Info("Document reprocessing job created successfully",
		zap.String("document_id", document.ID),
		zap.String("job_id", job.ID),
		zap.String("status", job.Status),
	)

	return job, nil
}


// clearDocumentProcessingData clears extracted text and processing results to prepare for reprocessing
func (s *DocumentService) clearDocumentProcessingData(ctx context.Context, documentID, tenantID string) error {
	query := `
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id})
		SET d.extracted_text = null, 
		    d.processing_result = null,
		    d.processed_at = null,
		    d.updated_at = $updated_at
		RETURN d.id
	`
	
	params := map[string]interface{}{
		"document_id": documentID,
		"tenant_id": tenantID,
		"updated_at": time.Now().UTC(),
	}

	result, err := s.neo4j.ExecuteQuery(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to clear document processing data: %w", err)
	}
	if len(result.Records) == 0 {
		return fmt.Errorf("document not found: %s", documentID)
	}
	
	s.logger.Debug("Cleared document processing data for reprocessing",
		zap.String("document_id", documentID),
	)
	
	return nil
}

// isPlaceholderText detects if the extracted text is placeholder/sample content
func (s *DocumentService) isPlaceholderText(text string) bool {
	if text == "" {
		return false
	}
	
	// Convert to lowercase for case-insensitive matching
	lowerText := strings.ToLower(text)
	
	// Common placeholder/sample text patterns
	placeholderPatterns := []string{
		"this is a sample",
		"sample pdf document",
		"audimodal ml service",
		"the document contains important information",
		"has been extracted and analyzed",
		"lorem ipsum",
		"placeholder text",
		"sample document",
		"test document",
		"demo content",
		"example text",
	}
	
	// Check for placeholder patterns
	for _, pattern := range placeholderPatterns {
		if strings.Contains(lowerText, pattern) {
			return true
		}
	}
	
	// Check for suspiciously short generic text (less than 50 chars and contains common generic words)
	if len(text) < 50 {
		genericWords := []string{"document", "processed", "extracted", "analyzed", "sample", "test", "demo"}
		wordCount := 0
		for _, word := range genericWords {
			if strings.Contains(lowerText, word) {
				wordCount++
			}
		}
		// If more than 2 generic words in short text, likely placeholder
		if wordCount > 2 {
			return true
		}
	}
	
	// Check for exact matches to known placeholder text
	knownPlaceholders := []string{
		"This is a sample PDF document processed by AudiModal ML service. The document contains important information that has been extracted and analyzed.",
		"Sample document content for testing purposes.",
		"Default extracted text.",
	}
	
	for _, placeholder := range knownPlaceholders {
		if text == placeholder {
			return true
		}
	}
	
	return false
}

// monitorProcessingResult monitors processing results for failures and alerts
func (s *DocumentService) monitorProcessingResult(ctx context.Context, documentID, tenantID, status, extractedText, errorMsg string) {
	// Log processing metrics for external monitoring systems
	switch status {
	case "processed":
		if extractedText == "" {
			s.logger.Warn("Processing completed but no text extracted",
				zap.String("document_id", documentID),
				zap.String("tenant_id", tenantID),
				zap.String("alert", "empty_extraction"),
			)
			// Could trigger alert here for empty extractions
		} else {
			s.logger.Info("Document processing successful",
				zap.String("document_id", documentID),
				zap.String("tenant_id", tenantID),
				zap.Int("text_length", len(extractedText)),
				zap.String("metric", "processing_success"),
			)
		}
		
	case "failed":
		s.logger.Error("Document processing failed",
			zap.String("document_id", documentID),
			zap.String("tenant_id", tenantID),
			zap.String("error_message", errorMsg),
			zap.String("alert", "processing_failure"),
		)
		
		// Record failure metrics
		s.recordProcessingFailure(ctx, documentID, tenantID, errorMsg)
		
	case "error":
		s.logger.Error("Document processing error",
			zap.String("document_id", documentID),
			zap.String("tenant_id", tenantID),
			zap.String("error_message", errorMsg),
			zap.String("alert", "processing_error"),
		)
		
		// Record error metrics
		s.recordProcessingFailure(ctx, documentID, tenantID, errorMsg)
		
	default:
		s.logger.Debug("Document processing status updated",
			zap.String("document_id", documentID),
			zap.String("tenant_id", tenantID),
			zap.String("status", status),
		)
	}
}

// recordProcessingFailure records processing failure metrics and potentially triggers alerts
func (s *DocumentService) recordProcessingFailure(ctx context.Context, documentID, tenantID, errorMsg string) {
	// Create or update failure tracking record
	query := `
		MERGE (f:ProcessingFailure {document_id: $document_id})
		SET f.tenant_id = $tenant_id,
		    f.error_message = $error_message,
		    f.failure_count = COALESCE(f.failure_count, 0) + 1,
		    f.last_failure_at = $timestamp,
		    f.updated_at = $timestamp
		ON CREATE SET f.first_failure_at = $timestamp,
		              f.created_at = $timestamp
		RETURN f.failure_count as count
	`
	
	params := map[string]interface{}{
		"document_id": documentID,
		"tenant_id": tenantID,
		"error_message": errorMsg,
		"timestamp": time.Now().UTC(),
	}

	result, err := s.neo4j.ExecuteQuery(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to record processing failure",
			zap.String("document_id", documentID),
			zap.Error(err),
		)
		return
	}

	// Check failure count for retry logic and escalation
	if len(result.Records) > 0 {
		if countVal, ok := result.Records[0].Get("count"); ok {
			if count, ok := countVal.(int64); ok {
				if count <= 3 {
					// Attempt retry for first 3 failures
					s.scheduleRetryProcessing(ctx, documentID, tenantID, count)
				} else {
					s.logger.Error("Document processing has failed multiple times - maximum retries exceeded",
						zap.String("document_id", documentID),
						zap.String("tenant_id", tenantID),
						zap.Int64("failure_count", count),
						zap.String("alert", "repeated_processing_failure"),
					)
					// Here you could trigger alerts, webhooks, or other escalation mechanisms
				}
			}
		}
	}
}

// scheduleRetryProcessing schedules a retry for failed text extraction
func (s *DocumentService) scheduleRetryProcessing(ctx context.Context, documentID, tenantID string, retryCount int64) {
	// Calculate exponential backoff delay: 2^retryCount minutes
	delayMinutes := int(math.Pow(2, float64(retryCount))) // 2, 4, 8 minutes
	retryAt := time.Now().UTC().Add(time.Duration(delayMinutes) * time.Minute)
	
	s.logger.Info("Scheduling document processing retry",
		zap.String("document_id", documentID),
		zap.String("tenant_id", tenantID),
		zap.Int64("retry_attempt", retryCount),
		zap.Int("delay_minutes", delayMinutes),
		zap.Time("retry_at", retryAt),
	)

	// Create retry job in database
	query := `
		CREATE (j:ProcessingRetryJob {
			id: $job_id,
			document_id: $document_id,
			tenant_id: $tenant_id,
			retry_attempt: $retry_attempt,
			status: 'scheduled',
			retry_at: $retry_at,
			created_at: $created_at,
			updated_at: $created_at
		})
		RETURN j.id as job_id
	`
	
	jobID := uuid.New().String()
	params := map[string]interface{}{
		"job_id": jobID,
		"document_id": documentID,
		"tenant_id": tenantID,
		"retry_attempt": retryCount,
		"retry_at": retryAt,
		"created_at": time.Now().UTC(),
	}

	_, err := s.neo4j.ExecuteQuery(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to schedule retry job",
			zap.String("document_id", documentID),
			zap.String("job_id", jobID),
			zap.Error(err),
		)
		return
	}

	// Start goroutine to handle the retry after delay
	go s.handleScheduledRetry(documentID, tenantID, jobID, retryAt)
}

// handleScheduledRetry processes a scheduled retry after the delay period
func (s *DocumentService) handleScheduledRetry(documentID, tenantID, jobID string, retryAt time.Time) {
	// Wait for the scheduled time
	time.Sleep(time.Until(retryAt))
	
	// Create context for retry operation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	s.logger.Info("Starting scheduled document processing retry",
		zap.String("document_id", documentID),
		zap.String("job_id", jobID),
	)

	// Mark retry job as in progress
	err := s.updateRetryJobStatus(ctx, jobID, "in_progress")
	if err != nil {
		s.logger.Error("Failed to update retry job status",
			zap.String("job_id", jobID),
			zap.Error(err),
		)
		return
	}

	// Get document details
	document, err := s.getDocumentForRetry(ctx, documentID, tenantID)
	if err != nil {
		s.logger.Error("Failed to get document for retry",
			zap.String("document_id", documentID),
			zap.Error(err),
		)
		_ = s.updateRetryJobStatus(ctx, jobID, "failed")
		return
	}

	// Attempt reprocessing
	spaceContext := &models.SpaceContext{
		TenantID: tenantID,
		UserID: document.OwnerID, // Use document owner for retry context
	}

	_, err = s.ReprocessDocument(ctx, document, spaceContext)
	if err != nil {
		s.logger.Error("Document retry processing failed",
			zap.String("document_id", documentID),
			zap.String("job_id", jobID),
			zap.Error(err),
		)
		_ = s.updateRetryJobStatus(ctx, jobID, "failed")
		return
	}

	// Mark retry job as completed
	err = s.updateRetryJobStatus(ctx, jobID, "completed")
	if err != nil {
		s.logger.Error("Failed to mark retry job as completed",
			zap.String("job_id", jobID),
			zap.Error(err),
		)
	}

	s.logger.Info("Document processing retry completed successfully",
		zap.String("document_id", documentID),
		zap.String("job_id", jobID),
	)
}

// updateRetryJobStatus updates the status of a retry job
func (s *DocumentService) updateRetryJobStatus(ctx context.Context, jobID, status string) error {
	query := `
		MATCH (j:ProcessingRetryJob {id: $job_id})
		SET j.status = $status, j.updated_at = $updated_at
		RETURN j.id
	`
	
	params := map[string]interface{}{
		"job_id": jobID,
		"status": status,
		"updated_at": time.Now().UTC(),
	}

	_, err := s.neo4j.ExecuteQuery(ctx, query, params)
	return err
}

// getDocumentForRetry retrieves a document for retry processing
func (s *DocumentService) getDocumentForRetry(ctx context.Context, documentID, tenantID string) (*models.Document, error) {
	query := `
		MATCH (d:Document {id: $document_id, tenant_id: $tenant_id})
		RETURN d.id as id,
		       d.name as name,
		       d.original_name as original_name,
		       d.description as description,
		       d.type as type,
		       d.status as status,
		       d.mime_type as mime_type,
		       d.size_bytes as size_bytes,
		       d.checksum as checksum,
		       d.storage_path as storage_path,
		       d.storage_bucket as storage_bucket,
		       d.notebook_id as notebook_id,
		       d.owner_id as owner_id,
		       d.space_type as space_type,
		       d.space_id as space_id,
		       d.tenant_id as tenant_id
	`
	
	params := map[string]interface{}{
		"document_id": documentID,
		"tenant_id": tenantID,
	}

	result, err := s.neo4j.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	if len(result.Records) == 0 {
		return nil, fmt.Errorf("document not found")
	}

	record := result.Records[0]
	return s.recordToDocument(record)
}

// deleteDocumentRecord removes a document record from the database (used for cleanup)
func (s *DocumentService) deleteDocumentRecord(ctx context.Context, documentID string) error {
	query := `
		MATCH (d:Document {id: $document_id})
		DETACH DELETE d
	`
	
	params := map[string]interface{}{
		"document_id": documentID,
	}
	
	_, err := s.neo4j.ExecuteQuery(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to delete document record: %w", err)
	}
	
	s.logger.Info("Document record deleted from database",
		zap.String("document_id", documentID))
	
	return nil
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
