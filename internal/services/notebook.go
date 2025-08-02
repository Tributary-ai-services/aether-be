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

// NotebookService handles notebook-related business logic
type NotebookService struct {
	neo4j  *database.Neo4jClient
	redis  *database.RedisClient
	logger *logger.Logger
}

// NewNotebookService creates a new notebook service
func NewNotebookService(neo4j *database.Neo4jClient, redis *database.RedisClient, log *logger.Logger) *NotebookService {
	return &NotebookService{
		neo4j:  neo4j,
		redis:  redis,
		logger: log.WithService("notebook_service"),
	}
}

// CreateNotebook creates a new notebook
func (s *NotebookService) CreateNotebook(ctx context.Context, req models.NotebookCreateRequest, ownerID string) (*models.Notebook, error) {
	// Create new notebook
	notebook := models.NewNotebook(req, ownerID)

	// Check if parent exists (if specified)
	if req.ParentID != "" {
		parentExists, err := s.notebookExists(ctx, req.ParentID)
		if err != nil {
			return nil, err
		}
		if !parentExists {
			return nil, errors.NotFoundWithDetails("Parent notebook not found", map[string]interface{}{
				"parent_id": req.ParentID,
			})
		}
	}

	// Create notebook in Neo4j
	query := `
		CREATE (n:Notebook {
			id: $id,
			name: $name,
			description: $description,
			visibility: $visibility,
			status: $status,
			owner_id: $owner_id,
			compliance_settings: $compliance_settings,
			document_count: $document_count,
			total_size_bytes: $total_size_bytes,
			tags: $tags,
			search_text: $search_text,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN n
	`

	params := map[string]interface{}{
		"id":                  notebook.ID,
		"name":                notebook.Name,
		"description":         notebook.Description,
		"visibility":          notebook.Visibility,
		"status":              notebook.Status,
		"owner_id":            notebook.OwnerID,
		"compliance_settings": notebook.ComplianceSettings,
		"document_count":      notebook.DocumentCount,
		"total_size_bytes":    notebook.TotalSizeBytes,
		"tags":                notebook.Tags,
		"search_text":         notebook.SearchText,
		"created_at":          notebook.CreatedAt.Format(time.RFC3339),
		"updated_at":          notebook.UpdatedAt.Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to create notebook", zap.Error(err))
		return nil, errors.Database("Failed to create notebook", err)
	}

	// Create parent-child relationship if parent specified
	if req.ParentID != "" {
		if err := s.createParentChildRelationship(ctx, req.ParentID, notebook.ID); err != nil {
			s.logger.Error("Failed to create parent-child relationship", zap.Error(err))
			// Don't fail the entire operation, just log the error
		}
	}

	// Create owner relationship
	if err := s.createOwnerRelationship(ctx, ownerID, notebook.ID); err != nil {
		s.logger.Error("Failed to create owner relationship", zap.Error(err))
		// Don't fail the entire operation, but this is more critical
	}

	s.logger.Info("Notebook created successfully",
		zap.String("notebook_id", notebook.ID),
		zap.String("name", notebook.Name),
		zap.String("owner_id", ownerID),
	)

	return notebook, nil
}

// GetNotebookByID retrieves a notebook by ID
func (s *NotebookService) GetNotebookByID(ctx context.Context, notebookID string, userID string) (*models.Notebook, error) {
	query := `
		MATCH (n:Notebook {id: $notebook_id})
		OPTIONAL MATCH (n)-[:OWNED_BY]->(owner:User)
		RETURN n.id, n.name, n.description, n.visibility, n.status, n.owner_id,
		       n.compliance_settings, n.document_count, n.total_size_bytes,
		       n.tags, n.search_text, n.created_at, n.updated_at,
		       owner.username, owner.full_name, owner.avatar_url
	`

	params := map[string]interface{}{
		"notebook_id": notebookID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get notebook by ID", zap.String("notebook_id", notebookID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve notebook", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Notebook not found", map[string]interface{}{
			"notebook_id": notebookID,
		})
	}

	notebook, err := s.recordToNotebook(result.Records[0])
	if err != nil {
		return nil, err
	}

	// Check access permissions
	if !s.canUserAccessNotebook(ctx, notebook, userID) {
		return nil, errors.Forbidden("Access denied to notebook")
	}

	return notebook, nil
}

// UpdateNotebook updates a notebook
func (s *NotebookService) UpdateNotebook(ctx context.Context, notebookID string, req models.NotebookUpdateRequest, userID string) (*models.Notebook, error) {
	// Get current notebook and check permissions
	notebook, err := s.GetNotebookByID(ctx, notebookID, userID)
	if err != nil {
		return nil, err
	}

	// Check if user can write to notebook
	if !s.canUserWriteNotebook(ctx, notebook, userID) {
		return nil, errors.Forbidden("Write access denied to notebook")
	}

	// Update notebook fields
	notebook.Update(req)

	// Update in Neo4j
	query := `
		MATCH (n:Notebook {id: $notebook_id})
		SET n.name = $name,
		    n.description = $description,
		    n.visibility = $visibility,
		    n.status = $status,
		    n.compliance_settings = $compliance_settings,
		    n.tags = $tags,
		    n.search_text = $search_text,
		    n.updated_at = datetime($updated_at)
		RETURN n
	`

	params := map[string]interface{}{
		"notebook_id":         notebookID,
		"name":                notebook.Name,
		"description":         notebook.Description,
		"visibility":          notebook.Visibility,
		"status":              notebook.Status,
		"compliance_settings": notebook.ComplianceSettings,
		"tags":                notebook.Tags,
		"search_text":         notebook.SearchText,
		"updated_at":          notebook.UpdatedAt.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update notebook", zap.String("notebook_id", notebookID), zap.Error(err))
		return nil, errors.Database("Failed to update notebook", err)
	}

	s.logger.Info("Notebook updated successfully",
		zap.String("notebook_id", notebookID),
		zap.String("name", notebook.Name),
	)

	return notebook, nil
}

// DeleteNotebook deletes a notebook (soft delete)
func (s *NotebookService) DeleteNotebook(ctx context.Context, notebookID string, userID string) error {
	// Get notebook and check permissions
	notebook, err := s.GetNotebookByID(ctx, notebookID, userID)
	if err != nil {
		return err
	}

	// Check if user can delete notebook (must be owner or admin)
	if notebook.OwnerID != userID {
		return errors.Forbidden("Only notebook owner can delete notebook")
	}

	// Soft delete: update status to deleted
	query := `
		MATCH (n:Notebook {id: $notebook_id})
		SET n.status = 'deleted',
		    n.updated_at = datetime($updated_at)
		RETURN n
	`

	params := map[string]interface{}{
		"notebook_id": notebookID,
		"updated_at":  time.Now().Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to delete notebook", zap.String("notebook_id", notebookID), zap.Error(err))
		return errors.Database("Failed to delete notebook", err)
	}

	s.logger.Info("Notebook deleted successfully",
		zap.String("notebook_id", notebookID),
		zap.String("name", notebook.Name),
	)

	return nil
}

// ListNotebooks lists notebooks for a user
func (s *NotebookService) ListNotebooks(ctx context.Context, userID string, limit, offset int) (*models.NotebookListResponse, error) {
	// Set defaults
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		MATCH (n:Notebook)
		WHERE n.status = 'active' AND (
			n.visibility = 'public' OR
			n.owner_id = $user_id OR
			EXISTS((n)-[:SHARED_WITH]->(:User {id: $user_id}))
		)
		OPTIONAL MATCH (n)-[:OWNED_BY]->(owner:User)
		RETURN n.id, n.name, n.description, n.visibility, n.status, n.owner_id,
		       n.compliance_settings, n.document_count, n.total_size_bytes,
		       n.tags, n.created_at, n.updated_at,
		       owner.username, owner.full_name, owner.avatar_url
		ORDER BY n.updated_at DESC
		SKIP $offset
		LIMIT $limit
	`

	params := map[string]interface{}{
		"user_id": userID,
		"limit":   limit + 1, // Get one extra to check if there are more
		"offset":  offset,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to list notebooks", zap.Error(err))
		return nil, errors.Database("Failed to list notebooks", err)
	}

	notebooks := make([]*models.NotebookResponse, 0, len(result.Records))
	hasMore := false

	for i, record := range result.Records {
		if i >= limit {
			hasMore = true
			break
		}

		notebook, err := s.recordToNotebookResponse(record)
		if err != nil {
			s.logger.Error("Failed to parse notebook record", zap.Error(err))
			continue
		}

		notebooks = append(notebooks, notebook)
	}

	// Get total count
	countQuery := `
		MATCH (n:Notebook)
		WHERE n.status = 'active' AND (
			n.visibility = 'public' OR
			n.owner_id = $user_id OR
			EXISTS((n)-[:SHARED_WITH]->(:User {id: $user_id}))
		)
		RETURN count(n) as total
	`

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, map[string]interface{}{"user_id": userID})
	if err != nil {
		s.logger.Error("Failed to get notebook count", zap.Error(err))
		return nil, errors.Database("Failed to get notebook count", err)
	}

	total := 0
	if len(countResult.Records) > 0 {
		if totalValue, found := countResult.Records[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return &models.NotebookListResponse{
		Notebooks: notebooks,
		Total:     total,
		Limit:     limit,
		Offset:    offset,
		HasMore:   hasMore,
	}, nil
}

// SearchNotebooks searches for notebooks
func (s *NotebookService) SearchNotebooks(ctx context.Context, req models.NotebookSearchRequest, userID string) (*models.NotebookListResponse, error) {
	// Set defaults
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	// Build query conditions
	whereConditions := []string{"n.status = 'active'"}
	params := map[string]interface{}{
		"user_id": userID,
		"limit":   req.Limit + 1,
		"offset":  req.Offset,
	}

	// Add access control
	whereConditions = append(whereConditions,
		"(n.visibility = 'public' OR n.owner_id = $user_id OR EXISTS((n)-[:SHARED_WITH]->(:User {id: $user_id})))")

	if req.Query != "" {
		whereConditions = append(whereConditions, "n.search_text CONTAINS $query")
		params["query"] = req.Query
	}

	if req.OwnerID != "" {
		whereConditions = append(whereConditions, "n.owner_id = $owner_id")
		params["owner_id"] = req.OwnerID
	}

	if req.Visibility != "" {
		whereConditions = append(whereConditions, "n.visibility = $visibility")
		params["visibility"] = req.Visibility
	}

	if req.Status != "" {
		whereConditions = append(whereConditions, "n.status = $status")
		params["status"] = req.Status
	}

	if len(req.Tags) > 0 {
		whereConditions = append(whereConditions, "ANY(tag IN $tags WHERE tag IN n.tags)")
		params["tags"] = req.Tags
	}

	whereClause := "WHERE " + fmt.Sprintf("(%s)", whereConditions[0])
	for i := 1; i < len(whereConditions); i++ {
		whereClause += " AND " + fmt.Sprintf("(%s)", whereConditions[i])
	}

	query := fmt.Sprintf(`
		MATCH (n:Notebook)
		%s
		OPTIONAL MATCH (n)-[:OWNED_BY]->(owner:User)
		RETURN n.id, n.name, n.description, n.visibility, n.status, n.owner_id,
		       n.document_count, n.total_size_bytes, n.tags, n.created_at, n.updated_at,
		       owner.username, owner.full_name, owner.avatar_url
		ORDER BY n.updated_at DESC
		SKIP $offset
		LIMIT $limit
	`, whereClause)

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to search notebooks", zap.Error(err))
		return nil, errors.Database("Failed to search notebooks", err)
	}

	notebooks := make([]*models.NotebookResponse, 0, len(result.Records))
	hasMore := false

	for i, record := range result.Records {
		if i >= req.Limit {
			hasMore = true
			break
		}

		notebook, err := s.recordToNotebookResponse(record)
		if err != nil {
			s.logger.Error("Failed to parse notebook record", zap.Error(err))
			continue
		}

		notebooks = append(notebooks, notebook)
	}

	return &models.NotebookListResponse{
		Notebooks: notebooks,
		Total:     len(notebooks), // For search, we don't compute exact total
		Limit:     req.Limit,
		Offset:    req.Offset,
		HasMore:   hasMore,
	}, nil
}

// ShareNotebook shares a notebook with users or groups
func (s *NotebookService) ShareNotebook(ctx context.Context, notebookID string, req models.NotebookShareRequest, userID string) error {
	// Get notebook and check permissions
	notebook, err := s.GetNotebookByID(ctx, notebookID, userID)
	if err != nil {
		return err
	}

	// Check if user can share notebook (must be owner or have admin permission)
	if notebook.OwnerID != userID {
		return errors.Forbidden("Only notebook owner can share notebook")
	}

	// Create sharing relationships
	for _, sharedUserID := range req.UserIDs {
		for _, permission := range req.Permissions {
			if err := s.createSharingRelationship(ctx, notebookID, sharedUserID, "", permission, userID); err != nil {
				s.logger.Error("Failed to create sharing relationship",
					zap.String("notebook_id", notebookID),
					zap.String("shared_user_id", sharedUserID),
					zap.String("permission", permission),
					zap.Error(err),
				)
				return err
			}
		}
	}

	for _, groupID := range req.GroupIDs {
		for _, permission := range req.Permissions {
			if err := s.createSharingRelationship(ctx, notebookID, "", groupID, permission, userID); err != nil {
				s.logger.Error("Failed to create group sharing relationship",
					zap.String("notebook_id", notebookID),
					zap.String("group_id", groupID),
					zap.String("permission", permission),
					zap.Error(err),
				)
				return err
			}
		}
	}

	s.logger.Info("Notebook shared successfully",
		zap.String("notebook_id", notebookID),
		zap.Strings("user_ids", req.UserIDs),
		zap.Strings("group_ids", req.GroupIDs),
		zap.Strings("permissions", req.Permissions),
	)

	return nil
}

// Helper methods (simplified implementations)

func (s *NotebookService) notebookExists(ctx context.Context, notebookID string) (bool, error) {
	query := "MATCH (n:Notebook {id: $notebook_id}) RETURN count(n) > 0 as exists"
	params := map[string]interface{}{"notebook_id": notebookID}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return false, err
	}

	if len(result.Records) > 0 {
		if exists, found := result.Records[0].Get("exists"); found {
			if existsBool, ok := exists.(bool); ok {
				return existsBool, nil
			}
		}
	}

	return false, nil
}

func (s *NotebookService) createParentChildRelationship(ctx context.Context, parentID, childID string) error {
	query := `
		MATCH (parent:Notebook {id: $parent_id}), (child:Notebook {id: $child_id})
		CREATE (parent)-[:CONTAINS]->(child)
	`
	params := map[string]interface{}{
		"parent_id": parentID,
		"child_id":  childID,
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	return err
}

func (s *NotebookService) createOwnerRelationship(ctx context.Context, userID, notebookID string) error {
	query := `
		MATCH (user:User {id: $user_id}), (notebook:Notebook {id: $notebook_id})
		CREATE (notebook)-[:OWNED_BY]->(user)
	`
	params := map[string]interface{}{
		"user_id":     userID,
		"notebook_id": notebookID,
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	return err
}

func (s *NotebookService) createSharingRelationship(ctx context.Context, notebookID, userID, groupID, permission, grantedBy string) error {
	// Implementation would create sharing relationships in Neo4j
	return nil
}

func (s *NotebookService) canUserAccessNotebook(ctx context.Context, notebook *models.Notebook, userID string) bool {
	return notebook.CanBeAccessedBy(userID)
}

func (s *NotebookService) canUserWriteNotebook(ctx context.Context, notebook *models.Notebook, userID string) bool {
	// Owner can always write
	if notebook.OwnerID == userID {
		return true
	}

	// TODO: Check write permissions from sharing relationships
	return false
}

func (s *NotebookService) recordToNotebook(record interface{}) (*models.Notebook, error) {
	// Implementation would convert Neo4j record to Notebook model
	return &models.Notebook{}, nil
}

func (s *NotebookService) recordToNotebookResponse(record interface{}) (*models.NotebookResponse, error) {
	// Implementation would convert Neo4j record to NotebookResponse model
	return &models.NotebookResponse{}, nil
}
