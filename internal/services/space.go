package services

import (
	"context"
	"encoding/json"
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

// CreateSpace creates a new organization space linked to an organization via HAS_SPACE relationship
// Organization ID is REQUIRED - spaces must belong to an organization
func (s *SpaceService) CreateSpace(ctx context.Context, userID string, req models.SpaceCreateRequest) (*models.Space, error) {
	// Organization ID is REQUIRED for space creation
	if req.OrganizationID == "" {
		return nil, errors.ValidationWithDetails("Organization ID is required", map[string]interface{}{
			"field": "organization_id",
		})
	}

	s.logger.Info("Creating organization space",
		zap.String("user_id", userID),
		zap.String("org_id", req.OrganizationID),
		zap.String("name", req.Name),
	)

	// Validate organization exists
	orgExistsQuery := `MATCH (o:Organization {id: $org_id}) RETURN o.id, o.name`
	orgResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, orgExistsQuery, map[string]interface{}{
		"org_id": req.OrganizationID,
	})
	if err != nil {
		s.logger.Error("Failed to check organization existence",
			zap.String("org_id", req.OrganizationID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to verify organization", err)
	}
	if len(orgResult.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Organization not found", map[string]interface{}{
			"organization_id": req.OrganizationID,
		})
	}

	// Create the space using the factory method
	spaceName := req.Name
	space := models.NewSpace(
		spaceName,
		req.Description,
		models.SpaceTypeOrganization,
		req.OrganizationID,
		models.SpaceOwnerTypeOrganization,
	)

	// Override visibility if provided
	if req.Visibility != "" {
		space.Visibility = req.Visibility
	}

	// Apply default settings for organization spaces
	space.Settings = models.DefaultSpaceSettings(models.SpaceTypeOrganization)

	// Serialize settings to JSON string for Neo4j (Neo4j doesn't support nested maps)
	settingsJSON := ""
	if space.Settings != nil {
		settingsBytes, err := json.Marshal(space.Settings)
		if err != nil {
			s.logger.Error("Failed to serialize space settings",
				zap.Error(err),
			)
			return nil, errors.Internal("Failed to serialize space settings")
		}
		settingsJSON = string(settingsBytes)
	}

	// Create the Space node in Neo4j AND link it to the Organization via HAS_SPACE relationship
	createQuery := `
		MATCH (o:Organization {id: $org_id})
		CREATE (sp:Space {
			id: $id,
			tenant_id: $tenant_id,
			audimodal_tenant_id: $audimodal_tenant_id,
			deeplake_namespace: $deeplake_namespace,
			name: $name,
			description: $description,
			space_type: $type,
			visibility: $visibility,
			owner_id: $owner_id,
			owner_type: $owner_type,
			status: $status,
			settings: $settings,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		CREATE (o)-[:HAS_SPACE {
			created_at: datetime(),
			is_default: false
		}]->(sp)
		RETURN sp.id
	`

	createParams := map[string]interface{}{
		"org_id":              req.OrganizationID,
		"id":                  space.ID,
		"tenant_id":           space.TenantID,
		"audimodal_tenant_id": space.AudimodalTenantID,
		"deeplake_namespace":  space.DeeplakeNamespace,
		"name":                space.Name,
		"description":         space.Description,
		"type":                string(space.Type),
		"visibility":          space.Visibility,
		"owner_id":            space.OwnerID,
		"owner_type":          string(space.OwnerType),
		"status":              string(space.Status),
		"settings":            settingsJSON, // Stored as JSON string
		"created_at":          space.CreatedAt.Format(time.RFC3339),
		"updated_at":          space.UpdatedAt.Format(time.RFC3339),
	}

	createResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, createQuery, createParams)
	if err != nil {
		s.logger.Error("Failed to create space node with HAS_SPACE relationship",
			zap.String("space_id", space.ID),
			zap.String("org_id", req.OrganizationID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to create space", err)
	}

	// Verify the space was created (org must exist for MATCH to return records)
	if len(createResult.Records) == 0 {
		s.logger.Error("Space not created - organization MATCH failed",
			zap.String("org_id", req.OrganizationID),
			zap.String("space_id", space.ID),
		)
		return nil, errors.NotFoundWithDetails("Organization not found during space creation", map[string]interface{}{
			"organization_id": req.OrganizationID,
		})
	}

	s.logger.Info("Organization space created successfully with HAS_SPACE relationship",
		zap.String("space_id", space.ID),
		zap.String("tenant_id", space.TenantID),
		zap.String("user_id", userID),
		zap.String("org_id", req.OrganizationID),
	)

	return space, nil
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

// GetUserSpaces retrieves all spaces a user has access to:
// - Personal spaces: via OWNS relationship
// - Organization spaces: via user's MEMBER_OF relationship to Organization which HAS_SPACE
// Note: userID parameter is the Keycloak ID from JWT
func (s *SpaceService) GetUserSpaces(ctx context.Context, userID string) ([]*models.SpaceInfo, error) {
	s.logger.Debug("Getting user spaces", zap.String("keycloak_id", userID))

	// Query for:
	// 1. Personal spaces the user owns (direct OWNS relationship)
	// 2. Organization spaces via organization membership (User -[:MEMBER_OF]-> Org -[:HAS_SPACE]-> Space)
	query := `
		MATCH (u:User {keycloak_id: $keycloak_id})

		// Personal spaces (direct ownership)
		OPTIONAL MATCH (u)-[:OWNS]->(personalSpace:Space)
		WHERE personalSpace.space_type = 'personal'

		// Organization spaces (via org membership)
		OPTIONAL MATCH (u)-[orgMembership:MEMBER_OF]->(org:Organization)-[:HAS_SPACE]->(orgSpace:Space)

		WITH u,
		     COLLECT(DISTINCT {
		         space: personalSpace,
		         role: 'owner',
		         org_id: null,
		         org_name: null
		     }) as personal_spaces,
		     COLLECT(DISTINCT {
		         space: orgSpace,
		         role: orgMembership.role,
		         org_id: org.id,
		         org_name: org.name
		     }) as org_spaces

		UNWIND (personal_spaces + org_spaces) as space_info
		WITH space_info
		WHERE space_info.space IS NOT NULL
		RETURN DISTINCT
		       space_info.space.id as id,
		       space_info.space.name as name,
		       space_info.space.space_type as type,
		       space_info.space.tenant_id as tenant_id,
		       space_info.space.visibility as visibility,
		       space_info.space.status as status,
		       space_info.role as role,
		       space_info.org_id as organization_id,
		       space_info.org_name as organization_name,
		       space_info.space.created_at as created_at
		ORDER BY created_at DESC
	`

	params := map[string]interface{}{
		"keycloak_id": userID,
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

	// Parse organization fields
	if val, ok := r.Get("organization_id"); ok && val != nil {
		spaceInfo.OrganizationID = val.(string)
	}
	if val, ok := r.Get("organization_name"); ok && val != nil {
		spaceInfo.OrganizationName = val.(string)
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

// =============================================================================
// MEMBER_OF Relationship Methods
// =============================================================================

// AddMember adds a user as a member of a space with the specified role
// Creates a MEMBER_OF relationship between User and Space
func (s *SpaceService) AddMember(ctx context.Context, spaceID, userID, role, invitedBy string) error {
	s.logger.Info("Adding member to space",
		zap.String("space_id", spaceID),
		zap.String("user_id", userID),
		zap.String("role", role),
		zap.String("invited_by", invitedBy),
	)

	// Validate role
	if !s.isValidRole(role) {
		return errors.ValidationWithDetails("Invalid role", map[string]interface{}{
			"role":        role,
			"valid_roles": []string{"admin", "member", "viewer"},
		})
	}

	// Check if user already has access (via OWNS or MEMBER_OF)
	existingRole, err := s.GetUserRoleInSpace(ctx, spaceID, userID)
	if err != nil {
		return err
	}
	if existingRole != "" {
		return errors.ConflictWithDetails("User already has access to this space", map[string]interface{}{
			"user_id":       userID,
			"space_id":      spaceID,
			"existing_role": existingRole,
		})
	}

	// Verify space exists
	_, err = s.GetSpaceByID(ctx, spaceID)
	if err != nil {
		return err
	}

	// Create MEMBER_OF relationship
	query := `
		MATCH (u:User {id: $user_id}), (sp:Space {id: $space_id})
		CREATE (u)-[r:MEMBER_OF {
			role: $role,
			permissions: $permissions,
			joined_at: datetime(),
			invited_by: $invited_by
		}]->(sp)
		RETURN r.role
	`

	params := map[string]interface{}{
		"user_id":     userID,
		"space_id":    spaceID,
		"role":        role,
		"permissions": s.getRolePermissions(role),
		"invited_by":  invitedBy,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to add member to space",
			zap.String("space_id", spaceID),
			zap.String("user_id", userID),
			zap.Error(err),
		)
		return errors.Database("Failed to add member to space", err)
	}

	if len(result.Records) == 0 {
		return errors.NotFoundWithDetails("User or Space not found", map[string]interface{}{
			"user_id":  userID,
			"space_id": spaceID,
		})
	}

	s.logger.Info("Member added to space successfully",
		zap.String("space_id", spaceID),
		zap.String("user_id", userID),
		zap.String("role", role),
	)

	return nil
}

// GetSpaceMembers returns all members of a space with their roles
func (s *SpaceService) GetSpaceMembers(ctx context.Context, spaceID string, limit, offset int) (*models.SpaceMembersListResponse, error) {
	s.logger.Debug("Getting space members",
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

	// Get all members including owner (via OWNS) and members (via MEMBER_OF)
	query := `
		MATCH (sp:Space {id: $space_id})
		// Get owner
		OPTIONAL MATCH (owner:User)-[:OWNS]->(sp)
		// Get members
		OPTIONAL MATCH (member:User)-[m:MEMBER_OF]->(sp)
		// Combine results
		WITH sp,
		     COLLECT(DISTINCT {
		         user: owner,
		         role: 'owner',
		         joined_at: sp.created_at,
		         invited_by: null
		     }) as owners,
		     COLLECT(DISTINCT {
		         user: member,
		         role: m.role,
		         joined_at: m.joined_at,
		         invited_by: m.invited_by
		     }) as members
		UNWIND (owners + members) as member_info
		WHERE member_info.user IS NOT NULL
		RETURN member_info.user.id as user_id,
		       member_info.user.username as username,
		       member_info.user.email as email,
		       member_info.user.full_name as full_name,
		       member_info.user.avatar_url as avatar_url,
		       member_info.role as role,
		       member_info.joined_at as joined_at,
		       member_info.invited_by as invited_by
		ORDER BY
		    CASE member_info.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'member' THEN 2 ELSE 3 END,
		    member_info.joined_at
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
		s.logger.Error("Failed to get space members", zap.String("space_id", spaceID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve space members", err)
	}

	members := make([]*models.SpaceMemberResponse, 0, len(result.Records))
	for _, record := range result.Records {
		member, err := s.recordToSpaceMember(record, spaceID)
		if err != nil {
			s.logger.Warn("Failed to parse member record", zap.Error(err))
			continue
		}
		members = append(members, member)
	}

	// Get total count
	countQuery := `
		MATCH (sp:Space {id: $space_id})
		OPTIONAL MATCH (owner:User)-[:OWNS]->(sp)
		OPTIONAL MATCH (member:User)-[:MEMBER_OF]->(sp)
		RETURN count(DISTINCT owner) + count(DISTINCT member) as total
	`

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, map[string]interface{}{
		"space_id": spaceID,
	})
	if err != nil {
		s.logger.Error("Failed to get member count", zap.Error(err))
		return &models.SpaceMembersListResponse{
			Members: members,
			Total:   len(members),
			Limit:   limit,
			Offset:  offset,
			HasMore: false,
		}, nil
	}

	total := 0
	if len(countResult.Records) > 0 {
		if totalValue, found := countResult.Records[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return &models.SpaceMembersListResponse{
		Members: members,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+len(members) < total,
	}, nil
}

// UpdateMemberRole updates a member's role in a space
func (s *SpaceService) UpdateMemberRole(ctx context.Context, spaceID, userID, newRole string) error {
	s.logger.Info("Updating member role",
		zap.String("space_id", spaceID),
		zap.String("user_id", userID),
		zap.String("new_role", newRole),
	)

	// Validate role
	if !s.isValidRole(newRole) {
		return errors.ValidationWithDetails("Invalid role", map[string]interface{}{
			"role":        newRole,
			"valid_roles": []string{"admin", "member", "viewer"},
		})
	}

	// Check if user is the owner - owner role cannot be changed via this method
	currentRole, err := s.GetUserRoleInSpace(ctx, spaceID, userID)
	if err != nil {
		return err
	}
	if currentRole == "" {
		return errors.NotFoundWithDetails("User is not a member of this space", map[string]interface{}{
			"user_id":  userID,
			"space_id": spaceID,
		})
	}
	if currentRole == "owner" {
		return errors.ForbiddenWithDetails("Cannot change owner's role", map[string]interface{}{
			"user_id":  userID,
			"space_id": spaceID,
		})
	}

	// Update MEMBER_OF relationship
	query := `
		MATCH (u:User {id: $user_id})-[r:MEMBER_OF]->(sp:Space {id: $space_id})
		SET r.role = $new_role,
		    r.permissions = $permissions,
		    r.updated_at = datetime()
		RETURN r.role
	`

	params := map[string]interface{}{
		"user_id":     userID,
		"space_id":    spaceID,
		"new_role":    newRole,
		"permissions": s.getRolePermissions(newRole),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update member role",
			zap.String("space_id", spaceID),
			zap.String("user_id", userID),
			zap.Error(err),
		)
		return errors.Database("Failed to update member role", err)
	}

	if len(result.Records) == 0 {
		return errors.NotFoundWithDetails("Membership not found", map[string]interface{}{
			"user_id":  userID,
			"space_id": spaceID,
		})
	}

	s.logger.Info("Member role updated successfully",
		zap.String("space_id", spaceID),
		zap.String("user_id", userID),
		zap.String("new_role", newRole),
	)

	return nil
}

// RemoveMember removes a member from a space
func (s *SpaceService) RemoveMember(ctx context.Context, spaceID, userID string) error {
	s.logger.Info("Removing member from space",
		zap.String("space_id", spaceID),
		zap.String("user_id", userID),
	)

	// Check if user is the owner - owner cannot be removed
	currentRole, err := s.GetUserRoleInSpace(ctx, spaceID, userID)
	if err != nil {
		return err
	}
	if currentRole == "" {
		return errors.NotFoundWithDetails("User is not a member of this space", map[string]interface{}{
			"user_id":  userID,
			"space_id": spaceID,
		})
	}
	if currentRole == "owner" {
		return errors.ForbiddenWithDetails("Cannot remove owner from space", map[string]interface{}{
			"user_id":  userID,
			"space_id": spaceID,
		})
	}

	// Delete MEMBER_OF relationship
	query := `
		MATCH (u:User {id: $user_id})-[r:MEMBER_OF]->(sp:Space {id: $space_id})
		DELETE r
		RETURN count(*) as deleted
	`

	params := map[string]interface{}{
		"user_id":  userID,
		"space_id": spaceID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to remove member from space",
			zap.String("space_id", spaceID),
			zap.String("user_id", userID),
			zap.Error(err),
		)
		return errors.Database("Failed to remove member from space", err)
	}

	// Check if deletion actually happened
	if len(result.Records) > 0 {
		if deleted, found := result.Records[0].Get("deleted"); found {
			if deletedInt, ok := deleted.(int64); ok && deletedInt == 0 {
				return errors.NotFoundWithDetails("Membership not found", map[string]interface{}{
					"user_id":  userID,
					"space_id": spaceID,
				})
			}
		}
	}

	s.logger.Info("Member removed from space successfully",
		zap.String("space_id", spaceID),
		zap.String("user_id", userID),
	)

	return nil
}

// isValidRole checks if a role is valid for MEMBER_OF relationships
// Note: "owner" is not valid here - ownership is via OWNS relationship
func (s *SpaceService) isValidRole(role string) bool {
	validRoles := map[string]bool{
		"admin":  true,
		"member": true,
		"viewer": true,
	}
	return validRoles[role]
}

// recordToSpaceMember converts a Neo4j record to SpaceMemberResponse
func (s *SpaceService) recordToSpaceMember(record interface{}, spaceID string) (*models.SpaceMemberResponse, error) {
	r, ok := record.(*neo4j.Record)
	if !ok {
		return nil, errors.Internal("Invalid record type")
	}

	member := &models.SpaceMemberResponse{
		SpaceID: spaceID,
	}

	if val, ok := r.Get("user_id"); ok && val != nil {
		member.UserID = val.(string)
	}
	if val, ok := r.Get("role"); ok && val != nil {
		member.Role = val.(string)
	}
	if val, ok := r.Get("username"); ok && val != nil {
		member.UserName = val.(string)
	}
	if val, ok := r.Get("email"); ok && val != nil {
		member.UserEmail = val.(string)
	}
	if val, ok := r.Get("full_name"); ok && val != nil {
		member.UserName = val.(string) // Use full_name if available
	}
	if val, ok := r.Get("avatar_url"); ok && val != nil {
		member.Avatar = val.(string)
	}
	if val, ok := r.Get("invited_by"); ok && val != nil {
		member.InvitedBy = val.(string)
	}

	// Parse joined_at timestamp
	if val, ok := r.Get("joined_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			member.JoinedAt = t
		}
	}

	// Set permissions based on role
	member.Permissions = s.getRolePermissions(member.Role)

	return member, nil
}

// =============================================================================
// Consistency Validation Methods
// =============================================================================

// InconsistencyReport represents a detected inconsistency between embedded fields and relationships
type InconsistencyReport struct {
	EntityType         string `json:"entity_type"`          // "notebook" or "user"
	EntityID           string `json:"entity_id"`            // ID of the entity
	EmbeddedSpaceID    string `json:"embedded_space_id"`    // Space ID from embedded field
	EmbeddedTenantID   string `json:"embedded_tenant_id"`   // Tenant ID from embedded field
	RelationshipSpace  string `json:"relationship_space"`   // Space ID from relationship
	RelationshipTenant string `json:"relationship_tenant"`  // Tenant ID from relationship Space
	Issue              string `json:"issue"`                // Description of the inconsistency
}

// ConsistencyCheckResult contains the results of a consistency check
type ConsistencyCheckResult struct {
	TotalNotebooks             int                    `json:"total_notebooks"`
	TotalUsers                 int                    `json:"total_users"`
	InconsistentNotebooks      int                    `json:"inconsistent_notebooks"`
	InconsistentUsers          int                    `json:"inconsistent_users"`
	OrphanedNotebooks          int                    `json:"orphaned_notebooks"`
	OrphanedSpaces             int                    `json:"orphaned_spaces"`
	UsersWithoutOwnsRelation   int                    `json:"users_without_owns_relation"`
	Inconsistencies            []*InconsistencyReport `json:"inconsistencies"`
	CheckedAt                  time.Time              `json:"checked_at"`
}

// CheckConsistency performs a consistency check between embedded fields and relationships
// This helps detect drift in the hybrid model (embedded fields + relationships)
func (s *SpaceService) CheckConsistency(ctx context.Context) (*ConsistencyCheckResult, error) {
	s.logger.Info("Running consistency check")

	result := &ConsistencyCheckResult{
		Inconsistencies: make([]*InconsistencyReport, 0),
		CheckedAt:       time.Now(),
	}

	// Check notebook consistency
	notebookInconsistencies, err := s.checkNotebookConsistency(ctx)
	if err != nil {
		s.logger.Error("Failed to check notebook consistency", zap.Error(err))
		return nil, err
	}
	result.Inconsistencies = append(result.Inconsistencies, notebookInconsistencies...)
	result.InconsistentNotebooks = len(notebookInconsistencies)

	// Check user-space consistency
	userInconsistencies, err := s.checkUserSpaceConsistency(ctx)
	if err != nil {
		s.logger.Error("Failed to check user-space consistency", zap.Error(err))
		return nil, err
	}
	result.Inconsistencies = append(result.Inconsistencies, userInconsistencies...)
	result.InconsistentUsers = len(userInconsistencies)

	// Count orphaned entities
	orphanCounts, err := s.countOrphanedEntities(ctx)
	if err != nil {
		s.logger.Error("Failed to count orphaned entities", zap.Error(err))
		return nil, err
	}
	result.OrphanedNotebooks = orphanCounts["notebooks"]
	result.OrphanedSpaces = orphanCounts["spaces"]
	result.UsersWithoutOwnsRelation = orphanCounts["users"]

	// Get totals
	totals, err := s.getTotals(ctx)
	if err != nil {
		s.logger.Warn("Failed to get totals", zap.Error(err))
	} else {
		result.TotalNotebooks = totals["notebooks"]
		result.TotalUsers = totals["users"]
	}

	s.logger.Info("Consistency check complete",
		zap.Int("inconsistent_notebooks", result.InconsistentNotebooks),
		zap.Int("inconsistent_users", result.InconsistentUsers),
		zap.Int("orphaned_notebooks", result.OrphanedNotebooks),
		zap.Int("orphaned_spaces", result.OrphanedSpaces),
		zap.Int("total_inconsistencies", len(result.Inconsistencies)),
	)

	return result, nil
}

// checkNotebookConsistency checks if notebook embedded fields match their BELONGS_TO relationship
func (s *SpaceService) checkNotebookConsistency(ctx context.Context) ([]*InconsistencyReport, error) {
	query := `
		MATCH (n:Notebook)
		WHERE n.space_id IS NOT NULL
		OPTIONAL MATCH (n)-[:BELONGS_TO]->(s:Space)
		WITH n, s
		WHERE s IS NULL
		   OR n.space_id <> s.id
		   OR (n.tenant_id IS NOT NULL AND s.tenant_id IS NOT NULL AND n.tenant_id <> s.tenant_id)
		RETURN n.id as notebook_id,
		       n.space_id as embedded_space_id,
		       n.tenant_id as embedded_tenant_id,
		       s.id as relationship_space_id,
		       s.tenant_id as relationship_tenant_id,
		       CASE
		           WHEN s IS NULL THEN 'No BELONGS_TO relationship exists'
		           WHEN n.space_id <> s.id THEN 'Space ID mismatch'
		           WHEN n.tenant_id <> s.tenant_id THEN 'Tenant ID mismatch'
		           ELSE 'Unknown'
		       END as issue
	`

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, nil)
	if err != nil {
		return nil, errors.Database("Failed to check notebook consistency", err)
	}

	reports := make([]*InconsistencyReport, 0, len(result.Records))
	for _, record := range result.Records {
		report := &InconsistencyReport{
			EntityType: "notebook",
		}

		if val, ok := record.Get("notebook_id"); ok && val != nil {
			report.EntityID = val.(string)
		}
		if val, ok := record.Get("embedded_space_id"); ok && val != nil {
			report.EmbeddedSpaceID = val.(string)
		}
		if val, ok := record.Get("embedded_tenant_id"); ok && val != nil {
			report.EmbeddedTenantID = val.(string)
		}
		if val, ok := record.Get("relationship_space_id"); ok && val != nil {
			report.RelationshipSpace = val.(string)
		}
		if val, ok := record.Get("relationship_tenant_id"); ok && val != nil {
			report.RelationshipTenant = val.(string)
		}
		if val, ok := record.Get("issue"); ok && val != nil {
			report.Issue = val.(string)
		}

		reports = append(reports, report)
	}

	return reports, nil
}

// checkUserSpaceConsistency checks if user personal_space_id matches their OWNS relationship
func (s *SpaceService) checkUserSpaceConsistency(ctx context.Context) ([]*InconsistencyReport, error) {
	query := `
		MATCH (u:User)
		WHERE u.personal_space_id IS NOT NULL
		OPTIONAL MATCH (u)-[:OWNS]->(s:Space)
		WITH u, s
		WHERE s IS NULL
		   OR u.personal_space_id <> s.id
		RETURN u.id as user_id,
		       u.personal_space_id as embedded_space_id,
		       s.id as relationship_space_id,
		       s.tenant_id as relationship_tenant_id,
		       CASE
		           WHEN s IS NULL THEN 'No OWNS relationship exists'
		           WHEN u.personal_space_id <> s.id THEN 'Personal space ID mismatch'
		           ELSE 'Unknown'
		       END as issue
	`

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, nil)
	if err != nil {
		return nil, errors.Database("Failed to check user-space consistency", err)
	}

	reports := make([]*InconsistencyReport, 0, len(result.Records))
	for _, record := range result.Records {
		report := &InconsistencyReport{
			EntityType: "user",
		}

		if val, ok := record.Get("user_id"); ok && val != nil {
			report.EntityID = val.(string)
		}
		if val, ok := record.Get("embedded_space_id"); ok && val != nil {
			report.EmbeddedSpaceID = val.(string)
		}
		if val, ok := record.Get("relationship_space_id"); ok && val != nil {
			report.RelationshipSpace = val.(string)
		}
		if val, ok := record.Get("relationship_tenant_id"); ok && val != nil {
			report.RelationshipTenant = val.(string)
		}
		if val, ok := record.Get("issue"); ok && val != nil {
			report.Issue = val.(string)
		}

		reports = append(reports, report)
	}

	return reports, nil
}

// countOrphanedEntities counts entities with missing relationships
func (s *SpaceService) countOrphanedEntities(ctx context.Context) (map[string]int, error) {
	counts := make(map[string]int)

	// Count orphaned notebooks (have space_id but no BELONGS_TO)
	notebookQuery := `
		MATCH (n:Notebook)
		WHERE n.space_id IS NOT NULL
		  AND NOT EXISTS { (n)-[:BELONGS_TO]->(:Space) }
		RETURN count(n) as count
	`
	notebookResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, notebookQuery, nil)
	if err != nil {
		return nil, err
	}
	if len(notebookResult.Records) > 0 {
		if val, ok := notebookResult.Records[0].Get("count"); ok {
			if countInt, ok := val.(int64); ok {
				counts["notebooks"] = int(countInt)
			}
		}
	}

	// Count orphaned personal spaces (no OWNS relationship)
	spaceQuery := `
		MATCH (s:Space {type: "personal"})
		WHERE NOT EXISTS { (:User)-[:OWNS]->(s) }
		RETURN count(s) as count
	`
	spaceResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, spaceQuery, nil)
	if err != nil {
		return nil, err
	}
	if len(spaceResult.Records) > 0 {
		if val, ok := spaceResult.Records[0].Get("count"); ok {
			if countInt, ok := val.(int64); ok {
				counts["spaces"] = int(countInt)
			}
		}
	}

	// Count users with personal_space_id but no OWNS relationship
	userQuery := `
		MATCH (u:User)
		WHERE u.personal_space_id IS NOT NULL
		  AND NOT EXISTS { (u)-[:OWNS]->(:Space {id: u.personal_space_id}) }
		RETURN count(u) as count
	`
	userResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, userQuery, nil)
	if err != nil {
		return nil, err
	}
	if len(userResult.Records) > 0 {
		if val, ok := userResult.Records[0].Get("count"); ok {
			if countInt, ok := val.(int64); ok {
				counts["users"] = int(countInt)
			}
		}
	}

	return counts, nil
}

// getTotals gets total counts of entities
func (s *SpaceService) getTotals(ctx context.Context) (map[string]int, error) {
	totals := make(map[string]int)

	query := `
		MATCH (n:Notebook)
		WITH count(n) as notebook_count
		MATCH (u:User)
		RETURN notebook_count, count(u) as user_count
	`

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	if len(result.Records) > 0 {
		if val, ok := result.Records[0].Get("notebook_count"); ok {
			if countInt, ok := val.(int64); ok {
				totals["notebooks"] = int(countInt)
			}
		}
		if val, ok := result.Records[0].Get("user_count"); ok {
			if countInt, ok := val.(int64); ok {
				totals["users"] = int(countInt)
			}
		}
	}

	return totals, nil
}

// CheckUserSpaceAccess checks if a user has access to a space and returns their role
// It checks for:
// 1. Direct OWNS relationship (owner)
// 2. MEMBER_OF relationship to the space
// 3. Membership in the owning organization (for organization-owned spaces)
// Returns: (hasAccess bool, role string, err error)
func (s *SpaceService) CheckUserSpaceAccess(ctx context.Context, userID, spaceID string) (bool, string, error) {
	s.logger.Debug("Checking user space access",
		zap.String("user_id", userID),
		zap.String("space_id", spaceID),
	)

	// Query checks multiple access paths:
	// 1. User directly owns the space (OWNS relationship)
	// 2. User is a direct member of the space (MEMBER_OF relationship)
	// 3. Space is owned by an organization that the user is a member of
	query := `
		MATCH (sp:Space {id: $space_id})
		MATCH (u:User)
		WHERE u.keycloak_id = $user_id OR u.id = $user_id

		// Check direct ownership
		OPTIONAL MATCH (u)-[:OWNS]->(sp)
		WITH sp, u, CASE WHEN (u)-[:OWNS]->(sp) THEN 'owner' ELSE null END as owner_role

		// Check direct membership
		OPTIONAL MATCH (u)-[direct_member:MEMBER_OF]->(sp)
		WITH sp, u, owner_role, direct_member.role as direct_role

		// Check organization membership (if space is owned by org)
		OPTIONAL MATCH (org:Organization {id: sp.owner_id})<-[org_member:MEMBER_OF]-(u)
		WHERE sp.owner_type = 'organization'

		RETURN owner_role,
		       direct_role,
		       org_member.role as org_role,
		       sp.owner_type as owner_type
	`

	params := map[string]interface{}{
		"space_id": spaceID,
		"user_id":  userID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to check user space access", zap.Error(err))
		return false, "", err
	}

	if len(result.Records) == 0 {
		return false, "", nil
	}

	record := result.Records[0]

	// Check roles in priority order: owner > direct member > org member
	if ownerRole, ok := record.Get("owner_role"); ok && ownerRole != nil {
		if roleStr, ok := ownerRole.(string); ok && roleStr != "" {
			s.logger.Debug("User has owner access to space",
				zap.String("user_id", userID),
				zap.String("space_id", spaceID),
			)
			return true, roleStr, nil
		}
	}

	if directRole, ok := record.Get("direct_role"); ok && directRole != nil {
		if roleStr, ok := directRole.(string); ok && roleStr != "" {
			s.logger.Debug("User has direct member access to space",
				zap.String("user_id", userID),
				zap.String("space_id", spaceID),
				zap.String("role", roleStr),
			)
			return true, roleStr, nil
		}
	}

	if orgRole, ok := record.Get("org_role"); ok && orgRole != nil {
		if roleStr, ok := orgRole.(string); ok && roleStr != "" {
			s.logger.Debug("User has organization member access to space",
				zap.String("user_id", userID),
				zap.String("space_id", spaceID),
				zap.String("role", roleStr),
			)
			return true, roleStr, nil
		}
	}

	s.logger.Debug("User does not have access to space",
		zap.String("user_id", userID),
		zap.String("space_id", spaceID),
	)
	return false, "", nil
}
