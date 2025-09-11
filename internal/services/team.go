package services

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// TeamService handles team-related business logic
type TeamService struct {
	neo4j  *database.Neo4jClient
	logger *logger.Logger
}

// NewTeamService creates a new team service
func NewTeamService(neo4j *database.Neo4jClient, log *logger.Logger) *TeamService {
	return &TeamService{
		neo4j:  neo4j,
		logger: log.WithService("team_service"),
	}
}

// CreateTeam creates a new team
func (s *TeamService) CreateTeam(ctx context.Context, req models.TeamCreateRequest, createdBy string) (*models.Team, error) {
	// Create new team
	team := models.NewTeam(req, createdBy)

	// Apply default settings if none provided
	if team.Settings == nil {
		team.Settings = models.DefaultTeamSettings()
	}

	// Validate organization exists (if we have organization service)
	// TODO: Add organization validation once OrganizationService is implemented

	// Create team in database
	query := `
		CREATE (t:Team {
			id: $id,
			name: $name,
			description: $description,
			organization_id: $organization_id,
			visibility: $visibility,
			icon: $icon,
			settings: $settings,
			created_by: $created_by,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN t`

	params := map[string]interface{}{
		"id":              team.ID,
		"name":            team.Name,
		"description":     team.Description,
		"organization_id": team.OrganizationID,
		"visibility":      team.Visibility,
		"icon":            team.Icon,
		"settings":        team.Settings,
		"created_by":      team.CreatedBy,
		"created_at":      team.CreatedAt.Format(time.RFC3339),
		"updated_at":      team.UpdatedAt.Format(time.RFC3339),
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
		s.logger.Error("Failed to create team", zap.Error(err), zap.String("team_id", team.ID))
		return nil, errors.DatabaseWithDetails("Failed to create team", err, map[string]interface{}{
			"team_id": team.ID,
		})
	}

	// Add creator as owner
	err = s.addTeamMember(ctx, team.ID, createdBy, "owner", createdBy)
	if err != nil {
		s.logger.Error("Failed to add creator as team owner", zap.Error(err), zap.String("team_id", team.ID))
		// Try to clean up the team if member addition fails
		_ = s.deleteTeamInternal(ctx, team.ID)
		return nil, err
	}

	s.logger.Info("Team created successfully", zap.String("team_id", team.ID), zap.String("created_by", createdBy))
	return team, nil
}

// GetTeam retrieves a team by ID
func (s *TeamService) GetTeam(ctx context.Context, teamID string, userID string) (*models.Team, error) {
	query := `
		MATCH (t:Team {id: $team_id})
		OPTIONAL MATCH (t)<-[r:MEMBER_OF]-(u:User {id: $user_id})
		OPTIONAL MATCH (t)<-[:MEMBER_OF]-()
		WITH t, r.role as user_role, count(*) as member_count
		OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(t)
		WITH t, user_role, member_count, count(n) as notebook_count
		RETURN t, user_role, member_count, notebook_count`

	params := map[string]interface{}{
		"team_id": teamID,
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
		s.logger.Error("Failed to get team", zap.Error(err), zap.String("team_id", teamID))
		return nil, errors.DatabaseWithDetails("Failed to retrieve team", err, map[string]interface{}{
			"team_id": teamID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, errors.NotFoundWithDetails("Team not found", map[string]interface{}{
			"team_id": teamID,
		})
	}

	record := records[0]
	team, err := s.recordToTeam(record)
	if err != nil {
		return nil, err
	}

	return team, nil
}

// GetTeams retrieves teams for a user
func (s *TeamService) GetTeams(ctx context.Context, userID string, organizationID string) ([]*models.Team, error) {
	var query string
	params := map[string]interface{}{
		"user_id": userID,
	}

	if organizationID != "" {
		query = `
			MATCH (t:Team {organization_id: $organization_id})
			OPTIONAL MATCH (t)<-[r:MEMBER_OF]-(u:User {id: $user_id})
			WHERE t.visibility = 'public' OR t.visibility = 'organization' OR r IS NOT NULL
			WITH t, r.role as user_role
			OPTIONAL MATCH (t)<-[:MEMBER_OF]-()
			WITH t, user_role, count(*) as member_count
			OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(t)
			WITH t, user_role, member_count, count(n) as notebook_count
			OPTIONAL MATCH (owner:User {id: t.created_by})
			RETURN t, user_role, member_count, notebook_count, owner.full_name as owner_name
			ORDER BY t.created_at DESC`
		params["organization_id"] = organizationID
	} else {
		query = `
			MATCH (t:Team)<-[r:MEMBER_OF]-(u:User {id: $user_id})
			WITH t, r.role as user_role
			OPTIONAL MATCH (t)<-[:MEMBER_OF]-()
			WITH t, user_role, count(*) as member_count
			OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(t)
			WITH t, user_role, member_count, count(n) as notebook_count
			OPTIONAL MATCH (owner:User {id: t.created_by})
			RETURN t, user_role, member_count, notebook_count, owner.full_name as owner_name
			ORDER BY t.created_at DESC`
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
		s.logger.Error("Failed to get teams", zap.Error(err), zap.String("user_id", userID))
		return nil, errors.DatabaseWithDetails("Failed to retrieve teams", err, map[string]interface{}{
			"user_id": userID,
		})
	}

	records := result.([]*neo4j.Record)
	teams := make([]*models.Team, 0, len(records))

	for _, record := range records {
		team, err := s.recordToTeam(record)
		if err != nil {
			s.logger.Error("Failed to parse team record", zap.Error(err))
			continue
		}
		teams = append(teams, team)
	}

	return teams, nil
}

// UpdateTeam updates a team
func (s *TeamService) UpdateTeam(ctx context.Context, teamID string, req models.TeamUpdateRequest, userID string) (*models.Team, error) {
	// Check if user has permission to update
	userRole, err := s.getUserRoleInTeam(ctx, teamID, userID)
	if err != nil {
		return nil, err
	}

	if userRole != "owner" && userRole != "admin" {
		return nil, errors.ForbiddenWithDetails("Insufficient permissions to update team", map[string]interface{}{
			"user_role": userRole,
			"team_id":   teamID,
		})
	}

	// Build update query dynamically
	setParts := []string{}
	params := map[string]interface{}{
		"team_id":    teamID,
		"updated_at": time.Now().Format(time.RFC3339),
	}

	if req.Name != nil {
		setParts = append(setParts, "t.name = $name")
		params["name"] = *req.Name
	}
	if req.Description != nil {
		setParts = append(setParts, "t.description = $description")
		params["description"] = *req.Description
	}
	if req.Visibility != nil {
		setParts = append(setParts, "t.visibility = $visibility")
		params["visibility"] = *req.Visibility
	}
	if req.Icon != nil {
		setParts = append(setParts, "t.icon = $icon")
		params["icon"] = *req.Icon
	}
	if req.Settings != nil {
		setParts = append(setParts, "t.settings = $settings")
		params["settings"] = req.Settings
	}

	if len(setParts) == 0 {
		// No updates provided, just return current team
		return s.GetTeam(ctx, teamID, userID)
	}

	setParts = append(setParts, "t.updated_at = datetime($updated_at)")

	query := fmt.Sprintf(`
		MATCH (t:Team {id: $team_id})
		SET %s
		WITH t
		OPTIONAL MATCH (t)<-[r:MEMBER_OF]-(u:User {id: $user_id})
		OPTIONAL MATCH (t)<-[:MEMBER_OF]-()
		WITH t, r.role as user_role, count(*) as member_count
		OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(t)
		WITH t, user_role, member_count, count(n) as notebook_count
		RETURN t, user_role, member_count, notebook_count`, 
		fmt.Sprintf("%s", setParts[0]))

	for i := 1; i < len(setParts); i++ {
		query = fmt.Sprintf(`
		MATCH (t:Team {id: $team_id})
		SET %s
		WITH t
		OPTIONAL MATCH (t)<-[r:MEMBER_OF]-(u:User {id: $user_id})
		OPTIONAL MATCH (t)<-[:MEMBER_OF]-()
		WITH t, r.role as user_role, count(*) as member_count
		OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(t)
		WITH t, user_role, member_count, count(n) as notebook_count
		RETURN t, user_role, member_count, notebook_count`, 
		fmt.Sprintf("%s, %s", setParts[0], setParts[i]))
	}

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
		s.logger.Error("Failed to update team", zap.Error(err), zap.String("team_id", teamID))
		return nil, errors.DatabaseWithDetails("Failed to update team", err, map[string]interface{}{
			"team_id": teamID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, errors.NotFoundWithDetails("Team not found", map[string]interface{}{
			"team_id": teamID,
		})
	}

	team, err := s.recordToTeam(records[0])
	if err != nil {
		return nil, err
	}

	s.logger.Info("Team updated successfully", zap.String("team_id", teamID), zap.String("updated_by", userID))
	return team, nil
}

// DeleteTeam deletes a team
func (s *TeamService) DeleteTeam(ctx context.Context, teamID string, userID string) error {
	// Check if user has permission to delete
	userRole, err := s.getUserRoleInTeam(ctx, teamID, userID)
	if err != nil {
		return err
	}

	if userRole != "owner" {
		return errors.ForbiddenWithDetails("Only team owners can delete teams", map[string]interface{}{
			"user_role": userRole,
			"team_id":   teamID,
		})
	}

	return s.deleteTeamInternal(ctx, teamID)
}

// Internal helper methods

func (s *TeamService) deleteTeamInternal(ctx context.Context, teamID string) error {
	query := `
		MATCH (t:Team {id: $team_id})
		OPTIONAL MATCH (t)<-[r:MEMBER_OF]-()
		OPTIONAL MATCH (n:Notebook)-[:OWNED_BY]->(t)
		DETACH DELETE t, r, n`

	params := map[string]interface{}{
		"team_id": teamID,
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
		s.logger.Error("Failed to delete team", zap.Error(err), zap.String("team_id", teamID))
		return errors.DatabaseWithDetails("Failed to delete team", err, map[string]interface{}{
			"team_id": teamID,
		})
	}

	s.logger.Info("Team deleted successfully", zap.String("team_id", teamID))
	return nil
}

func (s *TeamService) getUserRoleInTeam(ctx context.Context, teamID string, userID string) (string, error) {
	query := `
		MATCH (t:Team {id: $team_id})<-[r:MEMBER_OF]-(u:User {id: $user_id})
		RETURN r.role as role`

	params := map[string]interface{}{
		"team_id": teamID,
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
			"team_id": teamID,
			"user_id": userID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return "", errors.ForbiddenWithDetails("User is not a member of this team", map[string]interface{}{
			"team_id": teamID,
			"user_id": userID,
		})
	}

	role, _ := records[0].Get("role")
	return role.(string), nil
}

func (s *TeamService) addTeamMember(ctx context.Context, teamID string, userID string, role string, invitedBy string) error {
	query := `
		MATCH (t:Team {id: $team_id}), (u:User {id: $user_id})
		CREATE (u)-[r:MEMBER_OF {
			role: $role,
			joined_at: datetime($joined_at),
			invited_by: $invited_by
		}]->(t)
		RETURN r`

	params := map[string]interface{}{
		"team_id":    teamID,
		"user_id":    userID,
		"role":       role,
		"joined_at":  time.Now().Format(time.RFC3339),
		"invited_by": invitedBy,
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
		return errors.DatabaseWithDetails("Failed to add team member", err, map[string]interface{}{
			"team_id": teamID,
			"user_id": userID,
		})
	}

	return nil
}

// GetTeamMembers retrieves all members of a team
func (s *TeamService) GetTeamMembers(ctx context.Context, teamID string, userID string) ([]*models.TeamMember, error) {
	// Check if user has access to view team members
	userRole, err := s.getUserRoleInTeam(ctx, teamID, userID)
	if err != nil {
		return nil, err
	}

	// Members can view other members
	if userRole == "" {
		return nil, errors.ForbiddenWithDetails("User is not a member of this team", map[string]interface{}{
			"team_id": teamID,
			"user_id": userID,
		})
	}

	query := `
		MATCH (t:Team {id: $team_id})<-[r:MEMBER_OF]-(u:User)
		RETURN u.id as user_id, u.full_name as name, u.email as email, u.username as username, u.avatar_url as avatar,
			   r.role as role, r.joined_at as joined_at, r.invited_by as invited_by
		ORDER BY r.joined_at ASC`

	params := map[string]interface{}{
		"team_id": teamID,
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
		s.logger.Error("Failed to get team members", zap.Error(err), zap.String("team_id", teamID))
		return nil, errors.DatabaseWithDetails("Failed to retrieve team members", err, map[string]interface{}{
			"team_id": teamID,
		})
	}

	records := result.([]*neo4j.Record)
	members := make([]*models.TeamMember, 0, len(records))

	for _, record := range records {
		member := &models.TeamMember{
			TeamID: teamID,
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
		if joinedAt, ok := record.Get("joined_at"); ok {
			if t, ok := joinedAt.(time.Time); ok {
				member.JoinedAt = t
			}
		}
		if invitedBy, ok := record.Get("invited_by"); ok && invitedBy != nil {
			member.InvitedBy = invitedBy.(string)
		}

		members = append(members, member)
	}

	return members, nil
}

// InviteTeamMember invites a new member to the team
func (s *TeamService) InviteTeamMember(ctx context.Context, teamID string, req models.TeamInviteRequest, invitedBy string) (*models.TeamMember, error) {
	// Check if user has permission to invite
	userRole, err := s.getUserRoleInTeam(ctx, teamID, invitedBy)
	if err != nil {
		return nil, err
	}

	if userRole != "owner" && userRole != "admin" {
		return nil, errors.ForbiddenWithDetails("Insufficient permissions to invite team members", map[string]interface{}{
			"user_role": userRole,
			"team_id":   teamID,
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
	existingRole, err := s.getUserRoleInTeam(ctx, teamID, targetUserID.(string))
	if err == nil && existingRole != "" {
		return nil, errors.ConflictWithDetails("User is already a member of this team", map[string]interface{}{
			"user_id":      targetUserID,
			"current_role": existingRole,
		})
	}

	// Add member
	err = s.addTeamMember(ctx, teamID, targetUserID.(string), req.Role, invitedBy)
	if err != nil {
		return nil, err
	}

	// Return the new member
	member := &models.TeamMember{
		UserID:    targetUserID.(string),
		TeamID:    teamID,
		Role:      req.Role,
		JoinedAt:  time.Now(),
		InvitedBy: invitedBy,
	}

	if name, ok := userRecord.Get("name"); ok && name != nil {
		member.Name = name.(string)
	}
	if avatar, ok := userRecord.Get("avatar"); ok && avatar != nil {
		member.Avatar = avatar.(string)
	}

	member.Email = req.Email

	s.logger.Info("Team member invited successfully", 
		zap.String("team_id", teamID), 
		zap.String("user_id", targetUserID.(string)),
		zap.String("invited_by", invitedBy))

	return member, nil
}

// UpdateTeamMemberRole updates a team member's role
func (s *TeamService) UpdateTeamMemberRole(ctx context.Context, teamID string, targetUserID string, req models.TeamMemberRoleUpdateRequest, updatedBy string) error {
	// Check if user has permission to update roles
	userRole, err := s.getUserRoleInTeam(ctx, teamID, updatedBy)
	if err != nil {
		return err
	}

	if userRole != "owner" && userRole != "admin" {
		return errors.ForbiddenWithDetails("Insufficient permissions to update member roles", map[string]interface{}{
			"user_role": userRole,
			"team_id":   teamID,
		})
	}

	// Don't allow changing owner role or promoting to owner unless you are owner
	targetRole, err := s.getUserRoleInTeam(ctx, teamID, targetUserID)
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
		MATCH (t:Team {id: $team_id})<-[r:MEMBER_OF]-(u:User {id: $user_id})
		SET r.role = $role
		RETURN r`

	params := map[string]interface{}{
		"team_id": teamID,
		"user_id": targetUserID,
		"role":    req.Role,
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
		s.logger.Error("Failed to update team member role", zap.Error(err), 
			zap.String("team_id", teamID), zap.String("user_id", targetUserID))
		return errors.DatabaseWithDetails("Failed to update member role", err, map[string]interface{}{
			"team_id": teamID,
			"user_id": targetUserID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return errors.NotFoundWithDetails("Team member not found", map[string]interface{}{
			"team_id": teamID,
			"user_id": targetUserID,
		})
	}

	s.logger.Info("Team member role updated successfully", 
		zap.String("team_id", teamID), 
		zap.String("user_id", targetUserID),
		zap.String("new_role", req.Role),
		zap.String("updated_by", updatedBy))

	return nil
}

// RemoveTeamMember removes a member from the team
func (s *TeamService) RemoveTeamMember(ctx context.Context, teamID string, targetUserID string, removedBy string) error {
	// Check if user has permission to remove members
	userRole, err := s.getUserRoleInTeam(ctx, teamID, removedBy)
	if err != nil {
		return err
	}

	if userRole != "owner" && userRole != "admin" {
		return errors.ForbiddenWithDetails("Insufficient permissions to remove team members", map[string]interface{}{
			"user_role": userRole,
			"team_id":   teamID,
		})
	}

	// Check target user's role - can't remove owners unless you are owner
	targetRole, err := s.getUserRoleInTeam(ctx, teamID, targetUserID)
	if err != nil {
		return err
	}

	if targetRole == "owner" && userRole != "owner" {
		return errors.ForbiddenWithDetails("Only owners can remove other owners", map[string]interface{}{
			"user_role":   userRole,
			"target_role": targetRole,
		})
	}

	// Remove member
	query := `
		MATCH (t:Team {id: $team_id})<-[r:MEMBER_OF]-(u:User {id: $user_id})
		DELETE r
		RETURN count(*) as deleted`

	params := map[string]interface{}{
		"team_id": teamID,
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
		s.logger.Error("Failed to remove team member", zap.Error(err), 
			zap.String("team_id", teamID), zap.String("user_id", targetUserID))
		return errors.DatabaseWithDetails("Failed to remove member", err, map[string]interface{}{
			"team_id": teamID,
			"user_id": targetUserID,
		})
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 || records[0] == nil {
		return errors.NotFoundWithDetails("Team member not found", map[string]interface{}{
			"team_id": teamID,
			"user_id": targetUserID,
		})
	}

	s.logger.Info("Team member removed successfully", 
		zap.String("team_id", teamID), 
		zap.String("user_id", targetUserID),
		zap.String("removed_by", removedBy))

	return nil
}

func (s *TeamService) recordToTeam(record *neo4j.Record) (*models.Team, error) {
	node, ok := record.Get("t")
	if !ok {
		return nil, errors.ValidationWithDetails("Invalid team record", map[string]interface{}{
			"record": record.Keys,
		})
	}

	teamNode := node.(neo4j.Node)
	props := teamNode.Props

	team := &models.Team{
		ID:             props["id"].(string),
		Name:           props["name"].(string),
		Description:    props["description"].(string),
		OrganizationID: props["organization_id"].(string),
		Visibility:     props["visibility"].(string),
		CreatedBy:      props["created_by"].(string),
	}

	if icon, ok := props["icon"]; ok && icon != nil {
		team.Icon = icon.(string)
	}

	if settings, ok := props["settings"]; ok && settings != nil {
		team.Settings = settings.(map[string]interface{})
	}

	if createdAt, ok := props["created_at"]; ok {
		if t, ok := createdAt.(time.Time); ok {
			team.CreatedAt = t
		}
	}

	if updatedAt, ok := props["updated_at"]; ok {
		if t, ok := updatedAt.(time.Time); ok {
			team.UpdatedAt = t
		}
	}

	// Get computed fields
	if userRole, ok := record.Get("user_role"); ok && userRole != nil {
		team.UserRole = userRole.(string)
	}

	if memberCount, ok := record.Get("member_count"); ok && memberCount != nil {
		if count, ok := memberCount.(int64); ok {
			team.MemberCount = int(count)
		}
	}

	if notebookCount, ok := record.Get("notebook_count"); ok && notebookCount != nil {
		if count, ok := notebookCount.(int64); ok {
			team.NotebookCount = int(count)
		}
	}

	if ownerName, ok := record.Get("owner_name"); ok && ownerName != nil {
		team.OwnerName = ownerName.(string)
	}

	return team, nil
}