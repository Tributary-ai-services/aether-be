package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// DocumentService handles document-related business logic
type DocumentService struct {
	neo4j  *database.Neo4jClient
	redis  *database.RedisClient
	logger *logger.Logger

	// External services (will be injected)
	storageService    StorageService
	processingService ProcessingService
}

// StorageService interface for file storage operations
type StorageService interface {
	UploadFile(ctx context.Context, key string, data []byte, contentType string) (string, error)
	DownloadFile(ctx context.Context, key string) ([]byte, error)
	DeleteFile(ctx context.Context, key string) error
	GetFileURL(ctx context.Context, key string, expiration time.Duration) (string, error)
}

// ProcessingService interface for document processing operations
type ProcessingService interface {
	SubmitProcessingJob(ctx context.Context, documentID string, jobType string, config map[string]interface{}) (*models.ProcessingJob, error)
	GetProcessingJob(ctx context.Context, jobID string) (*models.ProcessingJob, error)
	CancelProcessingJob(ctx context.Context, jobID string) error
}

// NewDocumentService creates a new document service
func NewDocumentService(neo4j *database.Neo4jClient, redis *database.RedisClient, log *logger.Logger) *DocumentService {
	return &DocumentService{
		neo4j:  neo4j,
		redis:  redis,
		logger: log.WithService("document_service"),
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
func (s *DocumentService) CreateDocument(ctx context.Context, req models.DocumentCreateRequest, ownerID string, fileInfo models.FileInfo) (*models.Document, error) {
	// Verify notebook exists and user has access
	notebookExists, err := s.verifyNotebookAccess(ctx, req.NotebookID, ownerID)
	if err != nil {
		return nil, err
	}
	if !notebookExists {
		return nil, errors.NotFoundWithDetails("Notebook not found", map[string]interface{}{
			"notebook_id": req.NotebookID,
		})
	}

	// Create new document
	document := models.NewDocument(req, ownerID, fileInfo)

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
			tags: $tags,
			search_text: $search_text,
			metadata: $metadata,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN d
	`

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
		"tags":           document.Tags,
		"search_text":    document.SearchText,
		"metadata":       document.Metadata,
		"created_at":     document.CreatedAt.Format(time.RFC3339),
		"updated_at":     document.UpdatedAt.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to create document", zap.Error(err))
		return nil, errors.Database("Failed to create document", err)
	}

	// Create relationships
	if err := s.createDocumentRelationships(ctx, document.ID, document.NotebookID, document.OwnerID); err != nil {
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
func (s *DocumentService) UploadDocument(ctx context.Context, req models.DocumentUploadRequest, ownerID string) (*models.Document, error) {
	if s.storageService == nil {
		return nil, errors.Internal("Storage service not configured")
	}

	// Create file info from request
	fileInfo := models.FileInfo{
		OriginalName: req.Name,                   // This should come from file upload
		MimeType:     "application/octet-stream", // This should be detected
		SizeBytes:    int64(len(req.FileData)),
		Checksum:     "", // Should be calculated
	}

	// Create document record
	document, err := s.CreateDocument(ctx, req.DocumentCreateRequest, ownerID, fileInfo)
	if err != nil {
		return nil, err
	}

	// Upload file to storage
	storageKey := fmt.Sprintf("documents/%s/%s", document.NotebookID, document.ID)
	storagePath, err := s.storageService.UploadFile(ctx, storageKey, req.FileData, document.MimeType)
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

	// Update document with storage information
	document.UpdateStorageInfo(storagePath, "default") // bucket name should come from config
	if err := s.updateDocumentStorage(ctx, document.ID, storagePath, "default"); err != nil {
		s.logger.Error("Failed to update document storage info", zap.Error(err))
	}

	// Submit for processing if processing service is available
	if s.processingService != nil {
		processingConfig := map[string]interface{}{
			"extract_text":     true,
			"extract_metadata": true,
		}

		job, err := s.processingService.SubmitProcessingJob(ctx, document.ID, "extract", processingConfig)
		if err != nil {
			s.logger.Error("Failed to submit processing job",
				zap.String("document_id", document.ID),
				zap.Error(err))
		} else {
			document.ProcessingJobID = job.ID
			document.Status = "processing"
			if statusErr := s.updateDocumentStatus(ctx, document.ID, "processing", nil, ""); statusErr != nil {
				s.logger.Error("Failed to update document status", zap.Error(statusErr))
			}
		}
	}

	s.logger.Info("Document uploaded successfully",
		zap.String("document_id", document.ID),
		zap.String("storage_path", storagePath),
	)

	return document, nil
}

// GetDocumentByID retrieves a document by ID
func (s *DocumentService) GetDocumentByID(ctx context.Context, documentID string, userID string) (*models.Document, error) {
	query := `
		MATCH (d:Document {id: $document_id})
		OPTIONAL MATCH (d)-[:BELONGS_TO]->(n:Notebook)
		OPTIONAL MATCH (d)-[:OWNED_BY]->(owner:User)
		RETURN d.id, d.name, d.description, d.type, d.status, d.original_name,
		       d.mime_type, d.size_bytes, d.checksum, d.storage_path, d.storage_bucket,
		       d.extracted_text, d.processing_result, d.metadata, d.notebook_id, d.owner_id,
		       d.tags, d.search_text, d.processing_job_id, d.processed_at,
		       d.created_at, d.updated_at,
		       n.name as notebook_name, n.visibility as notebook_visibility,
		       owner.username, owner.full_name, owner.avatar_url
	`

	params := map[string]interface{}{
		"document_id": documentID,
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

	// Check access permissions
	if !s.canUserAccessDocument(ctx, document, userID) {
		return nil, errors.Forbidden("Access denied to document")
	}

	return document, nil
}

// UpdateDocument updates a document
func (s *DocumentService) UpdateDocument(ctx context.Context, documentID string, req models.DocumentUpdateRequest, userID string) (*models.Document, error) {
	// Get current document and check permissions
	document, err := s.GetDocumentByID(ctx, documentID, userID)
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
		MATCH (d:Document {id: $document_id})
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
func (s *DocumentService) DeleteDocument(ctx context.Context, documentID string, userID string) error {
	// Get document and check permissions
	document, err := s.GetDocumentByID(ctx, documentID, userID)
	if err != nil {
		return err
	}

	// Check if user can delete document (must be owner)
	if document.OwnerID != userID {
		return errors.Forbidden("Only document owner can delete document")
	}

	// Soft delete: update status to deleted
	query := `
		MATCH (d:Document {id: $document_id})
		SET d.status = 'deleted',
		    d.updated_at = datetime($updated_at)
		RETURN d
	`

	params := map[string]interface{}{
		"document_id": documentID,
		"updated_at":  time.Now().Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to delete document", zap.String("document_id", documentID), zap.Error(err))
		return errors.Database("Failed to delete document", err)
	}

	// Cancel processing job if active
	if document.ProcessingJobID != "" && s.processingService != nil {
		if err := s.processingService.CancelProcessingJob(ctx, document.ProcessingJobID); err != nil {
			s.logger.Warn("Failed to cancel processing job",
				zap.String("document_id", documentID),
				zap.String("job_id", document.ProcessingJobID),
				zap.Error(err))
		}
	}

	s.logger.Info("Document deleted successfully",
		zap.String("document_id", documentID),
		zap.String("name", document.Name),
	)

	return nil
}

// ListDocumentsByNotebook lists documents in a notebook
func (s *DocumentService) ListDocumentsByNotebook(ctx context.Context, notebookID string, userID string, limit, offset int) (*models.DocumentListResponse, error) {
	// Verify user has access to notebook
	hasAccess, err := s.verifyNotebookAccess(ctx, notebookID, userID)
	if err != nil {
		return nil, err
	}
	if !hasAccess {
		return nil, errors.Forbidden("Access denied to notebook")
	}

	// Set defaults
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		MATCH (d:Document {notebook_id: $notebook_id})
		WHERE d.status <> 'deleted'
		OPTIONAL MATCH (d)-[:OWNED_BY]->(owner:User)
		RETURN d.id, d.name, d.description, d.type, d.status, d.original_name,
		       d.mime_type, d.size_bytes, d.notebook_id, d.owner_id, d.tags,
		       d.processed_at, d.created_at, d.updated_at,
		       owner.username, owner.full_name, owner.avatar_url
		ORDER BY d.created_at DESC
		SKIP $offset
		LIMIT $limit
	`

	params := map[string]interface{}{
		"notebook_id": notebookID,
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
		MATCH (d:Document {notebook_id: $notebook_id})
		WHERE d.status <> 'deleted'
		RETURN count(d) as total
	`

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, map[string]interface{}{"notebook_id": notebookID})
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

// SearchDocuments searches for documents
func (s *DocumentService) SearchDocuments(ctx context.Context, req models.DocumentSearchRequest, userID string) (*models.DocumentListResponse, error) {
	// Set defaults
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	// Build query conditions
	whereConditions := []string{"d.status <> 'deleted'"}
	params := map[string]interface{}{
		"user_id": userID,
		"limit":   req.Limit + 1,
		"offset":  req.Offset,
	}

	// Add access control - user can see documents in notebooks they have access to
	whereConditions = append(whereConditions, `
		EXISTS((d)-[:BELONGS_TO]->(n:Notebook) WHERE 
			n.visibility = 'public' OR 
			n.owner_id = $user_id OR 
			EXISTS((n)-[:SHARED_WITH]->(:User {id: $user_id}))
		)`)

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
func (s *DocumentService) UpdateProcessingResult(ctx context.Context, documentID string, status string, result map[string]interface{}, errorMsg string) error {
	query := `
		MATCH (d:Document {id: $document_id})
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
			extractedText = text
		}
	}

	searchText := ""
	if extractedText != "" {
		// Get current document to build search text
		doc, err := s.GetDocumentByID(ctx, documentID, "") // Empty userID for internal operation
		if err == nil {
			searchText = fmt.Sprintf("%s %s", doc.SearchText, extractedText)
		}
	}

	params := map[string]interface{}{
		"document_id":    documentID,
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

func (s *DocumentService) createDocumentRelationships(ctx context.Context, documentID, notebookID, ownerID string) error {
	query := `
		MATCH (d:Document {id: $document_id}), (n:Notebook {id: $notebook_id}), (u:User {id: $owner_id})
		CREATE (d)-[:BELONGS_TO]->(n), (d)-[:OWNED_BY]->(u)
	`

	params := map[string]interface{}{
		"document_id": documentID,
		"notebook_id": notebookID,
		"owner_id":    ownerID,
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	return err
}

func (s *DocumentService) updateDocumentStatus(ctx context.Context, documentID, status string, result map[string]interface{}, errorMsg string) error {
	query := `
		MATCH (d:Document {id: $document_id})
		SET d.status = $status,
		    d.updated_at = datetime($updated_at)
		RETURN d
	`

	params := map[string]interface{}{
		"document_id": documentID,
		"status":      status,
		"updated_at":  time.Now().Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	return err
}

func (s *DocumentService) updateDocumentStorage(ctx context.Context, documentID, storagePath, storageBucket string) error {
	query := `
		MATCH (d:Document {id: $document_id})
		SET d.storage_path = $storage_path,
		    d.storage_bucket = $storage_bucket,
		    d.updated_at = datetime($updated_at)
		RETURN d
	`

	params := map[string]interface{}{
		"document_id":    documentID,
		"storage_path":   storagePath,
		"storage_bucket": storageBucket,
		"updated_at":     time.Now().Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
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

func (s *DocumentService) recordToDocument(record interface{}) (*models.Document, error) {
	// Implementation would convert Neo4j record to Document model
	return &models.Document{}, nil
}

func (s *DocumentService) recordToDocumentResponse(record interface{}) (*models.DocumentResponse, error) {
	// Implementation would convert Neo4j record to DocumentResponse model
	return &models.DocumentResponse{}, nil
}
