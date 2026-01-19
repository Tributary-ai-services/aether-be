package services

import (
	"context"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// SpaceService handles Space entity management (CRUD operations)
// This is separate from SpaceContextService which handles context resolution
type SpaceService struct {
	neo4j  *database.Neo4jClient
	logger *logger.Logger
}

// NewSpaceService creates a new space service
func NewSpaceService(neo4j *database.Neo4jClient, log *logger.Logger) *SpaceService {
	return &SpaceService{
		neo4j:  neo4j,
		logger: log.WithService("space_service"),
	}
}

// GetSpaceByID retrieves a Space by its ID
func (s *SpaceService) GetSpaceByID(ctx context.Context, spaceID string) (*models.Space, error) {
	s.logger.Debug("Getting space by ID", zap.String("space_id", spaceID))

	query := `
		MATCH (sp:Space {id: $space_id})
		OPTIONAL MATCH (sp)<-[:OWNS]-(owner:User)
		RETURN sp.id, sp.tenant_id, sp.audimodal_tenant_id, sp.deeplake_namespace,
		       sp.name, sp.description, sp.space_type as type, sp.visibility,
		       sp.owner_id, sp.status, sp.settings, sp.quotas,
		       sp.created_at, sp.updated_at, sp.deleted_at, sp.deleted_by,
		       owner.id as owner_user_id
	`

	params := map[string]interface{}{
		"space_id": spaceID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get space by ID", zap.String("space_id", spaceID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve space", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Space not found", map[string]interface{}{
			"space_id": spaceID,
		})
	}

	space, err := s.recordToSpace(result.Records[0])
	if err != nil {
		return nil, err
	}

	return space, nil
}

// GetUserSpaces retrieves all spaces a user has access to (via OWNS or MEMBER_OF relationships)
func (s *SpaceService) GetUserSpaces(ctx context.Context, userID string) ([]*models.SpaceInfo, error) {
	s.logger.Debug("Getting user spaces", zap.String("user_id", userID))

	// Query for spaces the user owns OR is a member of
	query := `
		MATCH (u:User {id: $user_id})
		OPTIONAL MATCH (u)-[:OWNS]->(owned:Space)
		OPTIONAL MATCH (u)-[m:MEMBER_OF]->(member:Space)
		WITH u,
		     COLLECT(DISTINCT {
		         space: owned,
		         role: 'owner',
		         joined_at: null
		     }) as owned_spaces,
		     COLLECT(DISTINCT {
		         space: member,
		         role: m.role,
		         joined_at: m.joined_at
		     }) as member_spaces
		UNWIND (owned_spaces + member_spaces) as space_info
		WHERE space_info.space IS NOT NULL
		RETURN space_info.space.id as id,
		       space_info.space.name as name,
		       space_info.space.space_type as type,
		       space_info.space.tenant_id as tenant_id,
		       space_info.space.visibility as visibility,
		       space_info.space.status as status,
		       space_info.role as role,
		       space_info.joined_at as joined_at
		ORDER BY space_info.space.created_at DESC
	`

	params := map[string]interface{}{
		"user_id": userID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get user spaces", zap.String("user_id", userID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve user spaces", err)
	}

	spaces := make([]*models.SpaceInfo, 0, len(result.Records))
	for _, record := range result.Records {
		spaceInfo, err := s.recordToSpaceInfo(record)
		if err != nil {
			s.logger.Warn("Failed to parse space info record", zap.Error(err))
			continue
		}
		spaces = append(spaces, spaceInfo)
	}

	return spaces, nil
}

// GetUserRoleInSpace returns the user's role in a specific space
// Returns "owner" for OWNS relationship, or the role property from MEMBER_OF relationship
// Returns empty string if user has no access
func (s *SpaceService) GetUserRoleInSpace(ctx context.Context, spaceID, userID string) (string, error) {
	s.logger.Debug("Getting user role in space",
		zap.String("space_id", spaceID),
		zap.String("user_id", userID),
	)

	query := `
		MATCH (u:User {id: $user_id})
		OPTIONAL MATCH (u)-[:OWNS]->(owned:Space {id: $space_id})
		OPTIONAL MATCH (u)-[m:MEMBER_OF]->(member:Space {id: $space_id})
		RETURN
		    CASE
		        WHEN owned IS NOT NULL THEN 'owner'
		        WHEN m IS NOT NULL THEN m.role
		        ELSE null
		    END as role
	`

	params := map[string]interface{}{
		"user_id":  userID,
		"space_id": spaceID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get user role in space",
			zap.String("space_id", spaceID),
			zap.String("user_id", userID),
			zap.Error(err),
		)
		return "", errors.Database("Failed to retrieve user role", err)
	}

	if len(result.Records) == 0 {
		return "", nil
	}

	roleValue, found := result.Records[0].Get("role")
	if !found || roleValue == nil {
		return "", nil
	}

	role, ok := roleValue.(string)
	if !ok {
		return "", nil
	}

	return role, nil
}

// UpdateSpace updates a Space's details
func (s *SpaceService) UpdateSpace(ctx context.Context, spaceID string, req models.SpaceUpdateRequest) (*models.Space, error) {
	s.logger.Info("Updating space", zap.String("space_id", spaceID))

	// Get current space to verify it exists
	space, err := s.GetSpaceByID(ctx, spaceID)
	if err != nil {
		return nil, err
	}

	// Apply updates
	space.Update(req)

	// Update in Neo4j
	query := `
		MATCH (sp:Space {id: $space_id})
		SET sp.name = $name,
		    sp.description = $description,
		    sp.visibility = $visibility,
		    sp.updated_at = datetime($updated_at)
		RETURN sp.id
	`

	params := map[string]interface{}{
		"space_id":    spaceID,
		"name":        space.Name,
		"description": space.Description,
		"visibility":  space.Visibility,
		"updated_at":  space.UpdatedAt.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update space", zap.String("space_id", spaceID), zap.Error(err))
		return nil, errors.Database("Failed to update space", err)
	}

	s.logger.Info("Space updated successfully",
		zap.String("space_id", spaceID),
		zap.String("name", space.Name),
	)

	return space, nil
}

// DeleteSpace performs a soft delete on a Space
func (s *SpaceService) DeleteSpace(ctx context.Context, spaceID, deletedBy string) error {
	s.logger.Info("Deleting space",
		zap.String("space_id", spaceID),
		zap.String("deleted_by", deletedBy),
	)

	// Verify space exists
	_, err := s.GetSpaceByID(ctx, spaceID)
	if err != nil {
		return err
	}

	now := time.Now()

	// Soft delete: update status and set deleted_at
	query := `
		MATCH (sp:Space {id: $space_id})
		SET sp.status = 'deleted',
		    sp.deleted_at = datetime($deleted_at),
		    sp.deleted_by = $deleted_by,
		    sp.updated_at = datetime($updated_at)
		RETURN sp.id
	`

	params := map[string]interface{}{
		"space_id":   spaceID,
		"deleted_at": now.Format(time.RFC3339),
		"deleted_by": deletedBy,
		"updated_at": now.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to delete space", zap.String("space_id", spaceID), zap.Error(err))
		return errors.Database("Failed to delete space", err)
	}

	s.logger.Info("Space deleted successfully",
		zap.String("space_id", spaceID),
		zap.String("deleted_by", deletedBy),
	)

	return nil
}

// GetSpaceNotebooks returns all notebooks that BELONG_TO a space
func (s *SpaceService) GetSpaceNotebooks(ctx context.Context, spaceID string, limit, offset int) ([]*models.NotebookResponse, int, error) {
	s.logger.Debug("Getting space notebooks",
		zap.String("space_id", spaceID),
		zap.Int("limit", limit),
		zap.Int("offset", offset),
	)

	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Get notebooks via BELONGS_TO relationship
	query := `
		MATCH (n:Notebook)-[:BELONGS_TO]->(sp:Space {id: $space_id})
		WHERE n.status = 'active'
		OPTIONAL MATCH (n)-[:OWNED_BY]->(owner:User)
		RETURN n.id, n.name, n.description, n.visibility, n.status, n.owner_id,
		       n.document_count, n.total_size_bytes, n.tags,
		       n.created_at, n.updated_at,
		       owner.username, owner.full_name, owner.avatar_url
		ORDER BY n.updated_at DESC
		SKIP $offset
		LIMIT $limit
	`

	params := map[string]interface{}{
		"space_id": spaceID,
		"limit":    limit,
		"offset":   offset,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get space notebooks", zap.String("space_id", spaceID), zap.Error(err))
		return nil, 0, errors.Database("Failed to retrieve space notebooks", err)
	}

	notebooks := make([]*models.NotebookResponse, 0, len(result.Records))
	for _, record := range result.Records {
		notebook, err := s.recordToNotebookResponse(record)
		if err != nil {
			s.logger.Warn("Failed to parse notebook record", zap.Error(err))
			continue
		}
		notebooks = append(notebooks, notebook)
	}

	// Get total count
	countQuery := `
		MATCH (n:Notebook)-[:BELONGS_TO]->(sp:Space {id: $space_id})
		WHERE n.status = 'active'
		RETURN count(n) as total
	`

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, map[string]interface{}{
		"space_id": spaceID,
	})
	if err != nil {
		s.logger.Error("Failed to get notebook count", zap.Error(err))
		return notebooks, len(notebooks), nil // Return what we have
	}

	total := 0
	if len(countResult.Records) > 0 {
		if totalValue, found := countResult.Records[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return notebooks, total, nil
}

// Helper methods

func (s *SpaceService) recordToSpace(record interface{}) (*models.Space, error) {
	r, ok := record.(*neo4j.Record)
	if !ok {
		return nil, errors.Internal("Invalid record type")
	}

	space := &models.Space{}

	// Extract basic fields
	if val, ok := r.Get("sp.id"); ok && val != nil {
		space.ID = val.(string)
	}
	if val, ok := r.Get("sp.tenant_id"); ok && val != nil {
		space.TenantID = val.(string)
	}
	if val, ok := r.Get("sp.audimodal_tenant_id"); ok && val != nil {
		space.AudimodalTenantID = val.(string)
	}
	if val, ok := r.Get("sp.deeplake_namespace"); ok && val != nil {
		space.DeeplakeNamespace = val.(string)
	}
	if val, ok := r.Get("sp.name"); ok && val != nil {
		space.Name = val.(string)
	}
	if val, ok := r.Get("sp.description"); ok && val != nil {
		space.Description = val.(string)
	}
	if val, ok := r.Get("type"); ok && val != nil {
		space.Type = models.SpaceType(val.(string))
	}
	if val, ok := r.Get("sp.visibility"); ok && val != nil {
		space.Visibility = val.(string)
	}
	if val, ok := r.Get("sp.owner_id"); ok && val != nil {
		space.OwnerID = val.(string)
	}
	if val, ok := r.Get("sp.status"); ok && val != nil {
		space.Status = models.SpaceStatus(val.(string))
	}
	if val, ok := r.Get("sp.deleted_by"); ok && val != nil {
		space.DeletedBy = val.(string)
	}

	// Parse timestamps
	if val, ok := r.Get("sp.created_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			space.CreatedAt = t
		}
	}
	if val, ok := r.Get("sp.updated_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			space.UpdatedAt = t
		}
	}
	if val, ok := r.Get("sp.deleted_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			space.DeletedAt = &t
		}
	}

	return space, nil
}

func (s *SpaceService) recordToSpaceInfo(record interface{}) (*models.SpaceInfo, error) {
	r, ok := record.(*neo4j.Record)
	if !ok {
		return nil, errors.Internal("Invalid record type")
	}

	spaceInfo := &models.SpaceInfo{}

	if val, ok := r.Get("id"); ok && val != nil {
		spaceInfo.SpaceID = val.(string)
	}
	if val, ok := r.Get("name"); ok && val != nil {
		spaceInfo.SpaceName = val.(string)
	}
	if val, ok := r.Get("type"); ok && val != nil {
		spaceInfo.SpaceType = models.SpaceType(val.(string))
	}
	if val, ok := r.Get("tenant_id"); ok && val != nil {
		spaceInfo.TenantID = val.(string)
	}
	if val, ok := r.Get("role"); ok && val != nil {
		spaceInfo.UserRole = val.(string)
	}

	// Map role to permissions
	spaceInfo.Permissions = s.getRolePermissions(spaceInfo.UserRole)

	return spaceInfo, nil
}

func (s *SpaceService) recordToNotebookResponse(record interface{}) (*models.NotebookResponse, error) {
	r, ok := record.(*neo4j.Record)
	if !ok {
		return nil, errors.Internal("Invalid record type")
	}

	notebook := &models.NotebookResponse{}

	// Extract basic fields
	if val, ok := r.Get("n.id"); ok && val != nil {
		notebook.ID = val.(string)
	}
	if val, ok := r.Get("n.name"); ok && val != nil {
		notebook.Name = val.(string)
	}
	if val, ok := r.Get("n.description"); ok && val != nil {
		notebook.Description = val.(string)
	}
	if val, ok := r.Get("n.visibility"); ok && val != nil {
		notebook.Visibility = val.(string)
	}
	if val, ok := r.Get("n.status"); ok && val != nil {
		notebook.Status = val.(string)
	}
	if val, ok := r.Get("n.owner_id"); ok && val != nil {
		notebook.OwnerID = val.(string)
	}
	if val, ok := r.Get("n.document_count"); ok && val != nil {
		switch v := val.(type) {
		case int64:
			notebook.DocumentCount = int(v)
		case int:
			notebook.DocumentCount = v
		}
	}
	if val, ok := r.Get("n.total_size_bytes"); ok && val != nil {
		switch v := val.(type) {
		case int64:
			notebook.TotalSizeBytes = v
		case int:
			notebook.TotalSizeBytes = int64(v)
		}
	}

	// Extract tags
	if val, ok := r.Get("n.tags"); ok && val != nil {
		if tagSlice, ok := val.([]interface{}); ok {
			tags := make([]string, 0, len(tagSlice))
			for _, tag := range tagSlice {
				if tagStr, ok := tag.(string); ok {
					tags = append(tags, tagStr)
				}
			}
			notebook.Tags = tags
		}
	}

	// Parse timestamps
	if val, ok := r.Get("n.created_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			notebook.CreatedAt = t
		}
	}
	if val, ok := r.Get("n.updated_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			notebook.UpdatedAt = t
		}
	}

	// Extract owner info
	var owner *models.PublicUserResponse
	ownerUsername, hasUsername := r.Get("owner.username")
	ownerFullName, hasFullName := r.Get("owner.full_name")
	ownerAvatarURL, hasAvatar := r.Get("owner.avatar_url")

	if hasUsername || hasFullName || hasAvatar {
		owner = &models.PublicUserResponse{
			ID: notebook.OwnerID,
		}
		if hasUsername && ownerUsername != nil {
			owner.Username = ownerUsername.(string)
		}
		if hasFullName && ownerFullName != nil {
			owner.FullName = ownerFullName.(string)
		}
		if hasAvatar && ownerAvatarURL != nil {
			owner.AvatarURL = ownerAvatarURL.(string)
		}
	}
	notebook.Owner = owner

	return notebook, nil
}

// getRolePermissions maps roles to permissions
func (s *SpaceService) getRolePermissions(role string) []string {
	switch role {
	case "owner":
		return []string{"read", "write", "create", "update", "delete", "admin"}
	case "admin":
		return []string{"read", "write", "create", "update", "delete"}
	case "member":
		return []string{"read", "write", "create", "update"}
	case "viewer":
		return []string{"read"}
	default:
		return []string{"read"}
	}
}
