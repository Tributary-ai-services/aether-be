package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// OrganizationService handles organization-related business logic
type OrganizationService struct {
	neo4j       *database.Neo4jClient
	audiModal   *AudiModalService
	logger      *logger.Logger
}

// NewOrganizationService creates a new organization service
func NewOrganizationService(neo4j *database.Neo4jClient, audiModal *AudiModalService, log *logger.Logger) *OrganizationService {
	return &OrganizationService{
		neo4j:     neo4j,
		audiModal: audiModal,
		logger:    log.WithService("organization_service"),
	}
}

// CreateOrganization creates a new organization
func (s *OrganizationService) CreateOrganization(ctx context.Context, req models.OrganizationCreateRequest, createdBy string) (*models.Organization, error) {
	// Check if slug is already taken
	if req.Slug != "" {
		exists, err := s.organizationSlugExists(ctx, req.Slug)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.ConflictWithDetails("Organization slug already exists", map[string]interface{}{
				"slug": req.Slug,
			})
		}
	}

	// Create new organization
	org := models.NewOrganization(req, createdBy)

	// Apply default settings if none provided
	if org.Settings == nil {
		org.Settings = models.DefaultOrganizationSettings()
	}

	// Apply default billing if none provided
	if org.Billing == nil {
		org.Billing = models.DefaultOrganizationBilling(req.BillingEmail)
	}

	// Create tenant in AudiModal if enabled
	if s.audiModal != nil {
		s.logger.Info("Creating tenant for organization", zap.String("org_name", org.Name))
		
		tenantReq := CreateTenantRequest{
			Name:         org.Slug,
			DisplayName:  org.Name,
			BillingPlan:  "organization",
			ContactEmail: req.BillingEmail,
			Quotas:       make(map[string]interface{}), // TODO: Set proper quotas
			Compliance:   make(map[string]interface{}), // TODO: Set compliance settings
			Settings:     make(map[string]interface{}), // TODO: Set organization settings
		}

		tenant, err := s.audiModal.CreateTenant(ctx, tenantReq)
		if err != nil {
			s.logger.Error("Failed to create tenant in AudiModal", zap.Error(err), zap.String("org_name", org.Name))
			return nil, errors.ExternalService("Failed to create organization tenant", err)
		}

		// API key is already returned from CreateTenant

		// Store tenant information in organization
		org.SetTenantInfo(tenant.TenantID, tenant.APIKey)
		s.logger.Info("Successfully created tenant for organization", 
			zap.String("org_id", org.ID), 
			zap.String("tenant_id", tenant.TenantID))
	}

	// Create organization in database
	query := `
		CREATE (o:Organization {
			id: $id,
			name: $name,
			slug: $slug,
			description: $description,
			avatar_url: $avatar_url,
			website: $website,
			location: $location,
			visibility: $visibility,
			tenant_id: $tenant_id,
			tenant_api_key: $tenant_api_key,
			billing: $billing,
			settings: $settings,
			created_by: $created_by,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN o`

	params := map[string]interface{}{
		"id":              org.ID,
		"name":            org.Name,
		"slug":            org.Slug,
		"description":     org.Description,
		"avatar_url":      org.AvatarURL,
		"website":         org.Website,
		"location":        org.Location,
		"visibility":      org.Visibility,
		"tenant_id":       org.TenantID,
		"tenant_api_key":  org.TenantAPIKey,
		"billing":         org.Billing,
		"settings":        org.Settings,
		"created_by":      org.CreatedBy,
		"created_at":      org.CreatedAt.Format(time.RFC3339),
		"updated_at":      org.UpdatedAt.Format(time.RFC3339),
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeWrite
	})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to create organization", zap.Error(err), zap.String("org_id", org.ID))
		return nil, errors.DatabaseWithDetails("Failed to create organization", err, map[string]interface{}{
			"org_id": org.ID,
		})
	}

	// Add creator as owner
	err = s.addOrganizationMember(ctx, org.ID, createdBy, "owner", createdBy, "", "")
	if err != nil {
		s.logger.Error("Failed to add creator as organization owner", zap.Error(err), zap.String("org_id", org.ID))
		// Try to clean up the organization if member addition fails
		_ = s.deleteOrganizationInternal(ctx, org.ID)
		return nil, err
	}

	s.logger.Info("Organization created successfully", zap.String("org_id", org.ID), zap.String("created_by", createdBy))
	return org, nil
}

// GetOrganization retrieves an organization by ID
func (s *OrganizationService) GetOrganization(ctx context.Context, orgID string, userID string) (*models.Organization, error) {
	query := `
		MATCH (o:Organization {id: $org_id})
		OPTIONAL MATCH (o)<-[r:MEMBER_OF]-(u:User {id: $user_id})
		OPTIONAL MATCH (o)<-[:MEMBER_OF]-()
		WITH o, r.role as user_role, count(*) as member_count
		OPTIONAL MATCH (t:Team {organization_id: o.id})
		WITH o, user_role, member_count, count(t) as team_count
		OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(o)
		WITH o, user_role, member_count, team_count, count(n) as notebook_count
		RETURN o, user_role, member_count, team_count, notebook_count`

	params := map[string]interface{}{
		"org_id":  orgID,
		"user_id": userID,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeRead
	})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get organization", zap.Error(err), zap.String("org_id", orgID))
		return nil, errors.DatabaseWithDetails("Failed to retrieve organization", err, map[string]interface{}{
			"org_id": orgID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, errors.NotFoundWithDetails("Organization not found", map[string]interface{}{
			"org_id": orgID,
		})
	}

	record := records[0]
	org, err := s.recordToOrganization(record)
	if err != nil {
		return nil, err
	}

	return org, nil
}

// GetOrganizations retrieves organizations for a user
func (s *OrganizationService) GetOrganizations(ctx context.Context, userID string) ([]*models.Organization, error) {
	query := `
		MATCH (o:Organization)<-[r:MEMBER_OF]-(u:User {id: $user_id})
		WITH o, r.role as user_role
		OPTIONAL MATCH (o)<-[:MEMBER_OF]-()
		WITH o, user_role, count(*) as member_count
		OPTIONAL MATCH (t:Team {organization_id: o.id})
		WITH o, user_role, member_count, count(t) as team_count
		OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(o)
		WITH o, user_role, member_count, team_count, count(n) as notebook_count
		RETURN o, user_role, member_count, team_count, notebook_count
		ORDER BY o.created_at DESC`

	params := map[string]interface{}{
		"user_id": userID,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeRead
	})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get organizations", zap.Error(err), zap.String("user_id", userID))
		return nil, errors.DatabaseWithDetails("Failed to retrieve organizations", err, map[string]interface{}{
			"user_id": userID,
		})
	}

	records := result.([]*neo4j.Record)
	organizations := make([]*models.Organization, 0, len(records))

	for _, record := range records {
		org, err := s.recordToOrganization(record)
		if err != nil {
			s.logger.Error("Failed to parse organization record", zap.Error(err))
			continue
		}
		organizations = append(organizations, org)
	}

	return organizations, nil
}

// UpdateOrganization updates an organization
func (s *OrganizationService) UpdateOrganization(ctx context.Context, orgID string, req models.OrganizationUpdateRequest, userID string) (*models.Organization, error) {
	// Check if user has permission to update
	userRole, err := s.getUserRoleInOrganization(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}

	if userRole != "owner" && userRole != "admin" {
		return nil, errors.ForbiddenWithDetails("Insufficient permissions to update organization", map[string]interface{}{
			"user_role": userRole,
			"org_id":    orgID,
		})
	}

	// Check slug uniqueness if being updated
	if req.Slug != nil {
		exists, err := s.organizationSlugExistsExcluding(ctx, *req.Slug, orgID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.ConflictWithDetails("Organization slug already exists", map[string]interface{}{
				"slug": *req.Slug,
			})
		}
	}

	// Build update query dynamically
	setParts := []string{}
	params := map[string]interface{}{
		"org_id":     orgID,
		"updated_at": time.Now().Format(time.RFC3339),
	}

	if req.Name != nil {
		setParts = append(setParts, "o.name = $name")
		params["name"] = *req.Name
	}
	if req.Slug != nil {
		setParts = append(setParts, "o.slug = $slug")
		params["slug"] = *req.Slug
	}
	if req.Description != nil {
		setParts = append(setParts, "o.description = $description")
		params["description"] = *req.Description
	}
	if req.AvatarURL != nil {
		setParts = append(setParts, "o.avatar_url = $avatar_url")
		params["avatar_url"] = *req.AvatarURL
	}
	if req.Website != nil {
		setParts = append(setParts, "o.website = $website")
		params["website"] = *req.Website
	}
	if req.Location != nil {
		setParts = append(setParts, "o.location = $location")
		params["location"] = *req.Location
	}
	if req.Visibility != nil {
		setParts = append(setParts, "o.visibility = $visibility")
		params["visibility"] = *req.Visibility
	}
	if req.Billing != nil {
		setParts = append(setParts, "o.billing = $billing")
		params["billing"] = req.Billing
	}
	if req.Settings != nil {
		setParts = append(setParts, "o.settings = $settings")
		params["settings"] = req.Settings
	}

	if len(setParts) == 0 {
		// No updates provided, just return current organization
		return s.GetOrganization(ctx, orgID, userID)
	}

	setParts = append(setParts, "o.updated_at = datetime($updated_at)")

	// Join all SET clauses with commas
	setClause := strings.Join(setParts, ", ")

	query := fmt.Sprintf(`
		MATCH (o:Organization {id: $org_id})
		SET %s
		WITH o
		OPTIONAL MATCH (o)<-[r:MEMBER_OF]-(u:User {id: $user_id})
		OPTIONAL MATCH (o)<-[:MEMBER_OF]-()
		WITH o, r.role as user_role, count(*) as member_count
		OPTIONAL MATCH (t:Team {organization_id: o.id})
		WITH o, user_role, member_count, count(t) as team_count
		OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(o)
		WITH o, user_role, member_count, team_count, count(n) as notebook_count
		RETURN o, user_role, member_count, team_count, notebook_count`, setClause)

	params["user_id"] = userID

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeWrite
	})
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to update organization", zap.Error(err), zap.String("org_id", orgID))
		return nil, errors.DatabaseWithDetails("Failed to update organization", err, map[string]interface{}{
			"org_id": orgID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, errors.NotFoundWithDetails("Organization not found", map[string]interface{}{
			"org_id": orgID,
		})
	}

	org, err := s.recordToOrganization(records[0])
	if err != nil {
		return nil, err
	}

	s.logger.Info("Organization updated successfully", zap.String("org_id", orgID), zap.String("updated_by", userID))
	return org, nil
}

// DeleteOrganization deletes an organization
func (s *OrganizationService) DeleteOrganization(ctx context.Context, orgID string, userID string) error {
	// Check if user has permission to delete
	userRole, err := s.getUserRoleInOrganization(ctx, orgID, userID)
	if err != nil {
		return err
	}

	if userRole != "owner" {
		return errors.ForbiddenWithDetails("Only organization owners can delete organizations", map[string]interface{}{
			"user_role": userRole,
			"org_id":    orgID,
		})
	}

	return s.deleteOrganizationInternal(ctx, orgID)
}

// Internal helper methods

func (s *OrganizationService) deleteOrganizationInternal(ctx context.Context, orgID string) error {
	query := `
		MATCH (o:Organization {id: $org_id})
		OPTIONAL MATCH (o)<-[r:MEMBER_OF]-()
		OPTIONAL MATCH (t:Team {organization_id: $org_id})
		OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(o)
		DETACH DELETE o, r, t, n`

	params := map[string]interface{}{
		"org_id": orgID,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeWrite
	})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to delete organization", zap.Error(err), zap.String("org_id", orgID))
		return errors.DatabaseWithDetails("Failed to delete organization", err, map[string]interface{}{
			"org_id": orgID,
		})
	}

	s.logger.Info("Organization deleted successfully", zap.String("org_id", orgID))
	return nil
}

func (s *OrganizationService) organizationSlugExists(ctx context.Context, slug string) (bool, error) {
	query := `MATCH (o:Organization {slug: $slug}) RETURN count(o) > 0 as exists`
	params := map[string]interface{}{
		"slug": slug,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeRead
	})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		return false, errors.DatabaseWithDetails("Failed to check slug existence", err, map[string]interface{}{
			"slug": slug,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return false, nil
	}

	exists, _ := records[0].Get("exists")
	return exists.(bool), nil
}

func (s *OrganizationService) organizationSlugExistsExcluding(ctx context.Context, slug string, excludeID string) (bool, error) {
	query := `MATCH (o:Organization {slug: $slug}) WHERE o.id <> $exclude_id RETURN count(o) > 0 as exists`
	params := map[string]interface{}{
		"slug":       slug,
		"exclude_id": excludeID,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeRead
	})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		return false, errors.DatabaseWithDetails("Failed to check slug existence", err, map[string]interface{}{
			"slug": slug,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return false, nil
	}

	exists, _ := records[0].Get("exists")
	return exists.(bool), nil
}

func (s *OrganizationService) getUserRoleInOrganization(ctx context.Context, orgID string, userID string) (string, error) {
	query := `
		MATCH (o:Organization {id: $org_id})<-[r:MEMBER_OF]-(u:User {id: $user_id})
		RETURN r.role as role`

	params := map[string]interface{}{
		"org_id":  orgID,
		"user_id": userID,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeRead
	})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		return "", errors.DatabaseWithDetails("Failed to get user role", err, map[string]interface{}{
			"org_id":  orgID,
			"user_id": userID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return "", errors.ForbiddenWithDetails("User is not a member of this organization", map[string]interface{}{
			"org_id":  orgID,
			"user_id": userID,
		})
	}

	role, _ := records[0].Get("role")
	return role.(string), nil
}

func (s *OrganizationService) addOrganizationMember(ctx context.Context, orgID string, userID string, role string, invitedBy string, title string, department string) error {
	query := `
		MATCH (o:Organization {id: $org_id}), (u:User {id: $user_id})
		CREATE (u)-[r:MEMBER_OF {
			role: $role,
			joined_at: datetime($joined_at),
			invited_by: $invited_by,
			title: $title,
			department: $department
		}]->(o)
		RETURN r`

	params := map[string]interface{}{
		"org_id":     orgID,
		"user_id":    userID,
		"role":       role,
		"joined_at":  time.Now().Format(time.RFC3339),
		"invited_by": invitedBy,
		"title":      title,
		"department": department,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeWrite
	})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		return errors.DatabaseWithDetails("Failed to add organization member", err, map[string]interface{}{
			"org_id":  orgID,
			"user_id": userID,
		})
	}

	return nil
}

func (s *OrganizationService) recordToOrganization(record *neo4j.Record) (*models.Organization, error) {
	node, ok := record.Get("o")
	if !ok {
		return nil, errors.ValidationWithDetails("Invalid organization record", map[string]interface{}{
			"record": record.Keys,
		})
	}

	orgNode := node.(neo4j.Node)
	props := orgNode.Props

	org := &models.Organization{
		ID:          props["id"].(string),
		Name:        props["name"].(string),
		Slug:        props["slug"].(string),
		Description: props["description"].(string),
		Visibility:  props["visibility"].(string),
		CreatedBy:   props["created_by"].(string),
	}

	if avatarURL, ok := props["avatar_url"]; ok && avatarURL != nil {
		org.AvatarURL = avatarURL.(string)
	}

	if website, ok := props["website"]; ok && website != nil {
		org.Website = website.(string)
	}

	if location, ok := props["location"]; ok && location != nil {
		org.Location = location.(string)
	}

	if billing, ok := props["billing"]; ok && billing != nil {
		org.Billing = billing.(map[string]interface{})
	}

	if settings, ok := props["settings"]; ok && settings != nil {
		org.Settings = settings.(map[string]interface{})
	}

	if createdAt, ok := props["created_at"]; ok {
		if t, ok := createdAt.(time.Time); ok {
			org.CreatedAt = t
		}
	}

	if updatedAt, ok := props["updated_at"]; ok {
		if t, ok := updatedAt.(time.Time); ok {
			org.UpdatedAt = t
		}
	}

	// Get computed fields
	if userRole, ok := record.Get("user_role"); ok && userRole != nil {
		org.UserRole = userRole.(string)
	}

	if memberCount, ok := record.Get("member_count"); ok && memberCount != nil {
		if count, ok := memberCount.(int64); ok {
			org.MemberCount = int(count)
		}
	}

	if teamCount, ok := record.Get("team_count"); ok && teamCount != nil {
		if count, ok := teamCount.(int64); ok {
			org.TeamCount = int(count)
		}
	}

	if notebookCount, ok := record.Get("notebook_count"); ok && notebookCount != nil {
		if count, ok := notebookCount.(int64); ok {
			org.NotebookCount = int(count)
		}
	}

	return org, nil
}

// GetOrganizationMembers retrieves all members of an organization
func (s *OrganizationService) GetOrganizationMembers(ctx context.Context, orgID string, userID string) ([]*models.OrganizationMember, error) {
	// Check if user has access to view organization members
	userRole, err := s.getUserRoleInOrganization(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}

	// Members can view other members
	if userRole == "" {
		return nil, errors.ForbiddenWithDetails("User is not a member of this organization", map[string]interface{}{
			"org_id":  orgID,
			"user_id": userID,
		})
	}

	query := `
		MATCH (o:Organization {id: $org_id})<-[r:MEMBER_OF]-(u:User)
		OPTIONAL MATCH (t:Team {organization_id: $org_id})<-[:MEMBER_OF]-(u)
		WITH u, r, collect(t.id) as team_ids
		RETURN u.id as user_id, u.full_name as name, u.email as email, u.username as username, u.avatar_url as avatar,
			   r.role as role, r.joined_at as joined_at, r.invited_by as invited_by, r.title as title, r.department as department,
			   team_ids
		ORDER BY r.joined_at ASC`

	params := map[string]interface{}{
		"org_id": orgID,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeRead
	})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get organization members", zap.Error(err), zap.String("org_id", orgID))
		return nil, errors.DatabaseWithDetails("Failed to retrieve organization members", err, map[string]interface{}{
			"org_id": orgID,
		})
	}

	records := result.([]*neo4j.Record)
	members := make([]*models.OrganizationMember, 0, len(records))

	for _, record := range records {
		member := &models.OrganizationMember{
			OrgID: orgID,
		}

		if userID, ok := record.Get("user_id"); ok {
			member.UserID = userID.(string)
		}
		if name, ok := record.Get("name"); ok && name != nil {
			member.Name = name.(string)
		}
		if email, ok := record.Get("email"); ok && email != nil {
			member.Email = email.(string)
		}
		if role, ok := record.Get("role"); ok {
			member.Role = role.(string)
		}
		if avatar, ok := record.Get("avatar"); ok && avatar != nil {
			member.Avatar = avatar.(string)
		}
		if title, ok := record.Get("title"); ok && title != nil {
			member.Title = title.(string)
		}
		if department, ok := record.Get("department"); ok && department != nil {
			member.Department = department.(string)
		}
		if joinedAt, ok := record.Get("joined_at"); ok {
			if t, ok := joinedAt.(time.Time); ok {
				member.JoinedAt = t
			}
		}
		if invitedBy, ok := record.Get("invited_by"); ok && invitedBy != nil {
			member.InvitedBy = invitedBy.(string)
		}
		if teamIds, ok := record.Get("team_ids"); ok && teamIds != nil {
			if ids, ok := teamIds.([]interface{}); ok {
				teams := make([]string, len(ids))
				for i, id := range ids {
					if teamId, ok := id.(string); ok {
						teams[i] = teamId
					}
				}
				member.Teams = teams
			}
		}

		members = append(members, member)
	}

	return members, nil
}

// InviteOrganizationMember invites a new member to the organization
func (s *OrganizationService) InviteOrganizationMember(ctx context.Context, orgID string, req models.OrganizationInviteRequest, invitedBy string) (*models.OrganizationMember, error) {
	// Check if user has permission to invite
	userRole, err := s.getUserRoleInOrganization(ctx, orgID, invitedBy)
	if err != nil {
		return nil, err
	}

	if userRole != "owner" && userRole != "admin" {
		return nil, errors.ForbiddenWithDetails("Insufficient permissions to invite organization members", map[string]interface{}{
			"user_role": userRole,
			"org_id":    orgID,
		})
	}

	// Find user by email
	userQuery := `MATCH (u:User {email: $email}) RETURN u.id as user_id, u.full_name as name, u.username as username, u.avatar_url as avatar`
	userParams := map[string]interface{}{
		"email": req.Email,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeRead
	})
	defer session.Close(ctx)

	userResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, userQuery, userParams)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		return nil, errors.DatabaseWithDetails("Failed to find user", err, map[string]interface{}{
			"email": req.Email,
		})
	}

	userRecords := userResult.([]*neo4j.Record)
	if len(userRecords) == 0 {
		return nil, errors.NotFoundWithDetails("User not found", map[string]interface{}{
			"email": req.Email,
		})
	}

	userRecord := userRecords[0]
	targetUserID, _ := userRecord.Get("user_id")

	// Check if user is already a member
	existingRole, err := s.getUserRoleInOrganization(ctx, orgID, targetUserID.(string))
	if err == nil && existingRole != "" {
		return nil, errors.ConflictWithDetails("User is already a member of this organization", map[string]interface{}{
			"user_id":      targetUserID,
			"current_role": existingRole,
		})
	}

	// Add member
	err = s.addOrganizationMember(ctx, orgID, targetUserID.(string), req.Role, invitedBy, req.Title, req.Department)
	if err != nil {
		return nil, err
	}

	// Return the new member
	member := &models.OrganizationMember{
		UserID:     targetUserID.(string),
		OrgID:      orgID,
		Role:       req.Role,
		JoinedAt:   time.Now(),
		InvitedBy:  invitedBy,
		Title:      req.Title,
		Department: req.Department,
		Teams:      []string{}, // New members start with no teams
	}

	if name, ok := userRecord.Get("name"); ok && name != nil {
		member.Name = name.(string)
	}
	if avatar, ok := userRecord.Get("avatar"); ok && avatar != nil {
		member.Avatar = avatar.(string)
	}

	member.Email = req.Email

	s.logger.Info("Organization member invited successfully", 
		zap.String("org_id", orgID), 
		zap.String("user_id", targetUserID.(string)),
		zap.String("invited_by", invitedBy))

	return member, nil
}

// UpdateOrganizationMemberRole updates an organization member's role
func (s *OrganizationService) UpdateOrganizationMemberRole(ctx context.Context, orgID string, targetUserID string, req models.OrganizationMemberRoleUpdateRequest, updatedBy string) error {
	// Check if user has permission to update roles
	userRole, err := s.getUserRoleInOrganization(ctx, orgID, updatedBy)
	if err != nil {
		return err
	}

	if userRole != "owner" && userRole != "admin" {
		return errors.ForbiddenWithDetails("Insufficient permissions to update member roles", map[string]interface{}{
			"user_role": userRole,
			"org_id":    orgID,
		})
	}

	// Don't allow changing owner role or promoting to owner unless you are owner
	targetRole, err := s.getUserRoleInOrganization(ctx, orgID, targetUserID)
	if err != nil {
		return err
	}

	if targetRole == "owner" || req.Role == "owner" {
		if userRole != "owner" {
			return errors.ForbiddenWithDetails("Only owners can change owner roles", map[string]interface{}{
				"user_role":   userRole,
				"target_role": targetRole,
			})
		}
	}

	// Update role
	query := `
		MATCH (o:Organization {id: $org_id})<-[r:MEMBER_OF]-(u:User {id: $user_id})
		SET r.role = $role, r.title = $title, r.department = $department
		RETURN r`

	params := map[string]interface{}{
		"org_id":     orgID,
		"user_id":    targetUserID,
		"role":       req.Role,
		"title":      req.Title,
		"department": req.Department,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeWrite
	})
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to update organization member role", zap.Error(err), 
			zap.String("org_id", orgID), zap.String("user_id", targetUserID))
		return errors.DatabaseWithDetails("Failed to update member role", err, map[string]interface{}{
			"org_id":  orgID,
			"user_id": targetUserID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return errors.NotFoundWithDetails("Organization member not found", map[string]interface{}{
			"org_id":  orgID,
			"user_id": targetUserID,
		})
	}

	s.logger.Info("Organization member role updated successfully", 
		zap.String("org_id", orgID), 
		zap.String("user_id", targetUserID),
		zap.String("new_role", req.Role),
		zap.String("updated_by", updatedBy))

	return nil
}

// RemoveOrganizationMember removes a member from the organization
func (s *OrganizationService) RemoveOrganizationMember(ctx context.Context, orgID string, targetUserID string, removedBy string) error {
	// Check if user has permission to remove members
	userRole, err := s.getUserRoleInOrganization(ctx, orgID, removedBy)
	if err != nil {
		return err
	}

	if userRole != "owner" && userRole != "admin" {
		return errors.ForbiddenWithDetails("Insufficient permissions to remove organization members", map[string]interface{}{
			"user_role": userRole,
			"org_id":    orgID,
		})
	}

	// Check target user's role - can't remove owners unless you are owner
	targetRole, err := s.getUserRoleInOrganization(ctx, orgID, targetUserID)
	if err != nil {
		return err
	}

	if targetRole == "owner" && userRole != "owner" {
		return errors.ForbiddenWithDetails("Only owners can remove other owners", map[string]interface{}{
			"user_role":   userRole,
			"target_role": targetRole,
		})
	}

	// Remove member (also removes from all teams in the organization)
	query := `
		MATCH (o:Organization {id: $org_id})<-[r:MEMBER_OF]-(u:User {id: $user_id})
		OPTIONAL MATCH (t:Team {organization_id: $org_id})<-[tr:MEMBER_OF]-(u)
		DELETE r, tr
		RETURN count(*) as deleted`

	params := map[string]interface{}{
		"org_id":  orgID,
		"user_id": targetUserID,
	}

	session := s.neo4j.Session(ctx, func(c *neo4j.SessionConfig) {
		c.AccessMode = neo4j.AccessModeWrite
	})
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to remove organization member", zap.Error(err), 
			zap.String("org_id", orgID), zap.String("user_id", targetUserID))
		return errors.DatabaseWithDetails("Failed to remove member", err, map[string]interface{}{
			"org_id":  orgID,
			"user_id": targetUserID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 || records[0] == nil {
		return errors.NotFoundWithDetails("Organization member not found", map[string]interface{}{
			"org_id":  orgID,
			"user_id": targetUserID,
		})
	}

	s.logger.Info("Organization member removed successfully", 
		zap.String("org_id", orgID), 
		zap.String("user_id", targetUserID),
		zap.String("removed_by", removedBy))

	return nil
}