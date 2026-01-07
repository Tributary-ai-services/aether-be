package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// UserService handles user-related business logic
type UserService struct {
	neo4j     *database.Neo4jClient
	audiModal *AudiModalService
	logger    *logger.Logger
}

// NewUserService creates a new user service
func NewUserService(neo4j *database.Neo4jClient, audiModal *AudiModalService, log *logger.Logger) *UserService {
	return &UserService{
		neo4j:     neo4j,
		audiModal: audiModal,
		logger:    log.WithService("user_service"),
	}
}

// CreateUser creates a new user
func (s *UserService) CreateUser(ctx context.Context, req models.UserCreateRequest) (*models.User, error) {
	// Check if user already exists by Keycloak ID
	existingUser, err := s.GetUserByKeycloakID(ctx, req.KeycloakID)
	if err == nil && existingUser != nil {
		// User with this Keycloak ID already exists, return it
		s.logger.Info("User with Keycloak ID already exists, returning existing user",
			zap.String("keycloak_id", req.KeycloakID),
			zap.String("email", existingUser.Email),
		)
		return existingUser, nil
	}

	// Check if user exists by email but with different Keycloak ID
	existingUserByEmail, err := s.GetUserByEmail(ctx, req.Email)
	if err == nil && existingUserByEmail != nil {
		// User exists with same email but different Keycloak ID - update the Keycloak ID
		s.logger.Info("User exists with same email but different Keycloak ID, updating Keycloak ID",
			zap.String("old_keycloak_id", existingUserByEmail.KeycloakID),
			zap.String("new_keycloak_id", req.KeycloakID),
			zap.String("email", req.Email),
		)
		
		// Update the Keycloak ID in the database
		updateQuery := `
			MATCH (u:User {email: $email})
			SET u.keycloak_id = $new_keycloak_id,
			    u.updated_at = datetime($updated_at)
			RETURN u
		`
		
		updateParams := map[string]interface{}{
			"email":            req.Email,
			"new_keycloak_id":  req.KeycloakID,
			"updated_at":       time.Now().Format(time.RFC3339),
		}
		
		_, err = s.neo4j.ExecuteQueryWithLogging(ctx, updateQuery, updateParams)
		if err != nil {
			s.logger.Error("Failed to update user Keycloak ID", zap.Error(err))
			return nil, errors.Database("Failed to update user Keycloak ID", err)
		}
		
		// Cache invalidation removed - no longer using Redis
		
		// Check if user needs personal tenant setup
		if !existingUserByEmail.HasPersonalTenant() {
			s.logger.Info("User missing personal tenant, creating one",
				zap.String("user_id", existingUserByEmail.ID),
				zap.String("email", req.Email),
			)
			
			// Create personal tenant in AudiModal
			tenantReq := CreateTenantRequest{
				Name:         fmt.Sprintf("%s-personal", existingUserByEmail.Username),
				DisplayName:  fmt.Sprintf("%s's Personal Space", existingUserByEmail.FullName),
				BillingPlan:  "personal",
				ContactEmail: existingUserByEmail.Email,
				Quotas: map[string]interface{}{
					"max_data_sources":      10,
					"max_files":            1000,
					"max_storage_mb":        5120, // 5GB
					"max_vector_dimensions": 1536,
					"max_monthly_searches":  10000,
				},
				Compliance: map[string]interface{}{
					"data_retention_days":   365,
					"encryption_enabled":    true,
					"audit_logging_enabled": true,
					"gdpr_compliant":       true,
				},
				Settings: map[string]interface{}{
					"user_id":       existingUserByEmail.ID,
					"user_email":    existingUserByEmail.Email,
					"creation_type": "retroactive_setup",
				},
			}
			
			tenant, err := s.audiModal.CreateTenant(ctx, tenantReq)
			if err != nil {
				s.logger.Error("Failed to create personal tenant for existing user", zap.Error(err))
				// Don't fail the login - just log the error
			} else {
				// Update user with personal tenant info
				updateTenantQuery := `
					MATCH (u:User {email: $email})
					SET u.personal_tenant_id = $tenant_id,
					    u.personal_api_key = $api_key,
					    u.updated_at = datetime($updated_at)
					RETURN u
				`
				
				updateTenantParams := map[string]interface{}{
					"email":      req.Email,
					"tenant_id":  tenant.TenantID,
					"api_key":    tenant.APIKey,
					"updated_at": time.Now().Format(time.RFC3339),
				}
				
				_, err = s.neo4j.ExecuteQueryWithLogging(ctx, updateTenantQuery, updateTenantParams)
				if err != nil {
					s.logger.Error("Failed to update user with tenant info", zap.Error(err))
					// Don't fail the login - just log the error
				} else {
					// Update the user object
					existingUserByEmail.SetPersonalTenantInfo(tenant.TenantID, tenant.APIKey)
					s.logger.Info("Successfully created personal tenant for existing user",
						zap.String("user_id", existingUserByEmail.ID),
						zap.String("tenant_id", tenant.TenantID),
					)
				}
			}
		}
		
		// Update the existing user object and return it
		existingUserByEmail.KeycloakID = req.KeycloakID
		existingUserByEmail.UpdatedAt = time.Now()
		
		s.logger.Info("Successfully updated user Keycloak ID",
			zap.String("user_id", existingUserByEmail.ID),
			zap.String("email", req.Email),
			zap.String("new_keycloak_id", req.KeycloakID),
		)
		
		return existingUserByEmail, nil
	}

	// Create new user
	user := models.NewUser(req)

	// Create personal tenant in AudiModal
	tenantReq := CreateTenantRequest{
		Name:         fmt.Sprintf("%s-personal", user.Username),
		DisplayName:  fmt.Sprintf("%s's Personal Space", user.FullName),
		BillingPlan:  "personal",
		ContactEmail: user.Email,
		Quotas: map[string]interface{}{
			"max_data_sources":      10,
			"max_files":            1000,
			"max_storage_mb":        5120, // 5GB
			"max_vector_dimensions": 1536,
			"max_monthly_searches":  10000,
		},
		Compliance: map[string]interface{}{
			"data_retention_days":   365,
			"encryption_enabled":    true,
			"audit_logging_enabled": true,
			"gdpr_compliant":       true,
		},
		Settings: map[string]interface{}{
			"user_id":       user.ID,
			"user_email":    user.Email,
			"creation_type": "user_registration",
		},
	}

	tenant, err := s.audiModal.CreateTenant(ctx, tenantReq)
	if err != nil {
		s.logger.Error("Failed to create personal tenant", zap.Error(err))
		return nil, errors.InternalWithCause("Failed to create personal workspace", err)
	}

	// Set personal tenant info on user
	user.SetPersonalTenantInfo(tenant.TenantID, tenant.APIKey)

	// Create personal space node in Neo4j (must exist before user node for onboarding)
	if err := s.createPersonalSpace(ctx, user); err != nil {
		s.logger.Error("Failed to create personal space", zap.Error(err))
		return nil, errors.InternalWithCause("Failed to create personal space", err)
	}

	// Create user in Neo4j
	query := `
		CREATE (u:User {
			id: $id,
			keycloak_id: $keycloak_id,
			email: $email,
			username: $username,
			full_name: $full_name,
			avatar_url: $avatar_url,
			preferences: $preferences,
			status: $status,
			personal_tenant_id: $personal_tenant_id,
			personal_space_id: $personal_space_id,
			personal_api_key: $personal_api_key,
			tutorial_completed: false,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN u
	`

	// Serialize preferences to JSON string for Neo4j storage
	var preferencesJSON string
	if user.Preferences != nil {
		preferencesBytes, err := json.Marshal(user.Preferences)
		if err != nil {
			s.logger.Error("Failed to serialize user preferences", zap.Error(err))
			return nil, errors.InternalWithCause("Failed to serialize user preferences", err)
		}
		preferencesJSON = string(preferencesBytes)
	} else {
		preferencesJSON = "{}"
	}

	params := map[string]interface{}{
		"id":                 user.ID,
		"keycloak_id":        user.KeycloakID,
		"email":              user.Email,
		"username":           user.Username,
		"full_name":          user.FullName,
		"avatar_url":         user.AvatarURL,
		"preferences":        preferencesJSON,
		"status":             user.Status,
		"personal_tenant_id": user.PersonalTenantID,
		"personal_space_id":  user.PersonalSpaceID,
		"personal_api_key":   user.PersonalAPIKey,
		"created_at":         user.CreatedAt.Format(time.RFC3339),
		"updated_at":         user.UpdatedAt.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to create user", zap.Error(err))
		return nil, errors.Database("Failed to create user", err)
	}

	s.logger.Info("User created successfully",
		zap.String("user_id", user.ID),
		zap.String("email", user.Email),
		zap.String("username", user.Username),
	)

	return user, nil
}

// createPersonalSpace creates a personal Space node in Neo4j for the user
// This must be called BEFORE creating the User node to ensure the space exists for onboarding
func (s *UserService) createPersonalSpace(ctx context.Context, user *models.User) error {
	s.logger.Info("Creating personal space for user",
		zap.String("user_id", user.ID),
		zap.String("space_id", user.PersonalSpaceID),
		zap.String("tenant_id", user.PersonalTenantID),
	)

	// Create Space node with OWNED_BY relationship to be created User
	query := `
		CREATE (s:Space {
			id: $space_id,
			name: $space_name,
			description: $description,
			space_type: "personal",
			tenant_id: $tenant_id,
			visibility: "private",
			owner_id: $owner_id,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN s.id as space_id
	`

	params := map[string]interface{}{
		"space_id":    user.PersonalSpaceID,
		"space_name":  fmt.Sprintf("%s's Personal Space", user.FullName),
		"description": "Your private workspace for documents, notebooks, and AI agents",
		"tenant_id":   user.PersonalTenantID,
		"owner_id":    user.ID,
		"created_at":  user.CreatedAt.Format(time.RFC3339),
		"updated_at":  user.UpdatedAt.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to create personal space",
			zap.String("user_id", user.ID),
			zap.String("space_id", user.PersonalSpaceID),
			zap.Error(err),
		)
		return errors.Database("Failed to create personal space", err)
	}

	if len(result.Records) == 0 {
		return errors.Internal("Space creation returned no records")
	}

	s.logger.Info("Personal space created successfully",
		zap.String("user_id", user.ID),
		zap.String("space_id", user.PersonalSpaceID),
	)

	return nil
}

// GetUserByID retrieves a user by ID
func (s *UserService) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	query := `
		MATCH (u:User {id: $user_id})
		RETURN u.id, u.keycloak_id, u.email, u.username, u.full_name, u.avatar_url,
		       u.keycloak_roles, u.keycloak_groups, u.keycloak_attributes,
		       u.preferences, u.status, u.created_at, u.updated_at,
		       u.last_login_at, u.last_sync_at, u.personal_tenant_id, u.personal_space_id, u.personal_api_key,
		       COALESCE(u.tutorial_completed, false) AS tutorial_completed, u.tutorial_completed_at
	`

	params := map[string]interface{}{
		"user_id": userID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get user by ID", zap.String("user_id", userID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve user", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("User not found", map[string]interface{}{
			"user_id": userID,
		})
	}

	user, err := s.recordToUser(result.Records[0])
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByEmail retrieves a user by email
func (s *UserService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		MATCH (u:User {email: $email})
		RETURN u.id, u.keycloak_id, u.email, u.username, u.full_name, u.avatar_url,
		       u.keycloak_roles, u.keycloak_groups, u.keycloak_attributes,
		       u.preferences, u.status, u.created_at, u.updated_at,
		       u.last_login_at, u.last_sync_at, u.personal_tenant_id, u.personal_space_id, u.personal_api_key,
		       COALESCE(u.tutorial_completed, false) AS tutorial_completed, u.tutorial_completed_at
	`

	params := map[string]interface{}{
		"email": email,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get user by email", zap.String("email", email), zap.Error(err))
		return nil, errors.Database("Failed to retrieve user", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("User not found", map[string]interface{}{
			"email": email,
		})
	}

	return s.recordToUser(result.Records[0])
}

// GetUserByKeycloakID retrieves a user by Keycloak ID
func (s *UserService) GetUserByKeycloakID(ctx context.Context, keycloakID string) (*models.User, error) {
	query := `
		MATCH (u:User {keycloak_id: $keycloak_id})
		RETURN u.id, u.keycloak_id, u.email, u.username, u.full_name, u.avatar_url,
		       u.keycloak_roles, u.keycloak_groups, u.keycloak_attributes,
		       u.preferences, u.status, u.created_at, u.updated_at,
		       u.last_login_at, u.last_sync_at, u.personal_tenant_id, u.personal_space_id, u.personal_api_key,
		       COALESCE(u.tutorial_completed, false) AS tutorial_completed, u.tutorial_completed_at
	`

	params := map[string]interface{}{
		"keycloak_id": keycloakID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get user by Keycloak ID", zap.String("keycloak_id", keycloakID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve user", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("User not found", map[string]interface{}{
			"keycloak_id": keycloakID,
		})
	}

	return s.recordToUser(result.Records[0])
}

// UpdateUser updates a user
func (s *UserService) UpdateUser(ctx context.Context, userID string, req models.UserUpdateRequest) (*models.User, error) {
	// Get current user
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Update user fields
	user.Update(req)

	// Update in Neo4j
	query := `
		MATCH (u:User {id: $user_id})
		SET u.full_name = $full_name,
		    u.avatar_url = $avatar_url,
		    u.preferences = $preferences,
		    u.status = $status,
		    u.updated_at = datetime($updated_at)
		RETURN u
	`

	// Serialize preferences to JSON string for Neo4j storage
	var preferencesJSON string
	if user.Preferences != nil {
		preferencesBytes, err := json.Marshal(user.Preferences)
		if err != nil {
			s.logger.Error("Failed to serialize user preferences during update", zap.Error(err))
			return nil, errors.InternalWithCause("Failed to serialize user preferences", err)
		}
		preferencesJSON = string(preferencesBytes)
	} else {
		preferencesJSON = "{}"
	}

	params := map[string]interface{}{
		"user_id":     userID,
		"full_name":   user.FullName,
		"avatar_url":  user.AvatarURL,
		"preferences": preferencesJSON,
		"status":      user.Status,
		"updated_at":  user.UpdatedAt.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update user", zap.String("user_id", userID), zap.Error(err))
		return nil, errors.Database("Failed to update user", err)
	}

	// Cache invalidation removed - no longer using Redis

	s.logger.Info("User updated successfully",
		zap.String("user_id", userID),
		zap.String("email", user.Email),
	)

	return user, nil
}

// DeleteUser deletes a user
func (s *UserService) DeleteUser(ctx context.Context, userID string) error {
	// Check if user exists
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// Delete personal tenant if exists
	if user.HasPersonalTenant() {
		s.logger.Info("Deleting personal tenant for user",
			zap.String("user_id", userID),
			zap.String("tenant_id", user.PersonalTenantID),
		)
		
		if err := s.audiModal.DeleteTenant(ctx, user.PersonalTenantID); err != nil {
			s.logger.Error("Failed to delete personal tenant",
				zap.String("user_id", userID),
				zap.String("tenant_id", user.PersonalTenantID),
				zap.Error(err),
			)
			// Continue with user deletion even if tenant deletion fails
		}
	}

	// Soft delete: update status to inactive
	query := `
		MATCH (u:User {id: $user_id})
		SET u.status = 'inactive',
		    u.updated_at = datetime($updated_at)
		RETURN u
	`

	params := map[string]interface{}{
		"user_id":    userID,
		"updated_at": time.Now().Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to delete user", zap.String("user_id", userID), zap.Error(err))
		return errors.Database("Failed to delete user", err)
	}

	// Cache invalidation removed - no longer using Redis

	s.logger.Info("User deleted successfully",
		zap.String("user_id", userID),
		zap.String("email", user.Email),
	)

	return nil
}

// SearchUsers searches for users
func (s *UserService) SearchUsers(ctx context.Context, req models.UserSearchRequest) (*models.UserSearchResponse, error) {
	// Set defaults
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	// Build query
	whereConditions := []string{}
	params := map[string]interface{}{
		"limit":  req.Limit + 1, // Get one extra to check if there are more
		"offset": req.Offset,
	}

	if req.Query != "" {
		whereConditions = append(whereConditions, "(u.username CONTAINS $query OR u.full_name CONTAINS $query OR u.email CONTAINS $query)")
		params["query"] = req.Query
	}

	if req.Email != "" {
		whereConditions = append(whereConditions, "u.email = $email")
		params["email"] = req.Email
	}

	if req.Username != "" {
		whereConditions = append(whereConditions, "u.username = $username")
		params["username"] = req.Username
	}

	if req.Status != "" {
		whereConditions = append(whereConditions, "u.status = $status")
		params["status"] = req.Status
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + fmt.Sprintf("(%s)", whereConditions[0])
		for i := 1; i < len(whereConditions); i++ {
			whereClause += " AND " + fmt.Sprintf("(%s)", whereConditions[i])
		}
	}

	query := fmt.Sprintf(`
		MATCH (u:User)
		%s
		RETURN u.id, u.keycloak_id, u.email, u.username, u.full_name, u.avatar_url,
		       u.status, u.created_at, u.updated_at
		ORDER BY u.created_at DESC
		SKIP $offset
		LIMIT $limit
	`, whereClause)

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to search users", zap.Error(err))
		return nil, errors.Database("Failed to search users", err)
	}

	users := make([]*models.UserResponse, 0, len(result.Records))
	hasMore := false

	for i, record := range result.Records {
		if i >= req.Limit {
			hasMore = true
			break
		}

		user, err := s.recordToUserResponse(record)
		if err != nil {
			s.logger.Error("Failed to parse user record", zap.Error(err))
			continue
		}

		users = append(users, user)
	}

	// Get total count
	countQuery := fmt.Sprintf(`
		MATCH (u:User)
		%s
		RETURN count(u) as total
	`, whereClause)

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, params)
	if err != nil {
		s.logger.Error("Failed to get user count", zap.Error(err))
		return nil, errors.Database("Failed to get user count", err)
	}

	total := 0
	if len(countResult.Records) > 0 {
		if totalValue, found := countResult.Records[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return &models.UserSearchResponse{
		Users:   users,
		Total:   total,
		Limit:   req.Limit,
		Offset:  req.Offset,
		HasMore: hasMore,
	}, nil
}

// UpdateLastLogin updates the user's last login time
func (s *UserService) UpdateLastLogin(ctx context.Context, userID string) error {
	query := `
		MATCH (u:User {id: $user_id})
		SET u.last_login_at = datetime($last_login_at),
		    u.updated_at = datetime($updated_at)
		RETURN u
	`

	now := time.Now()
	params := map[string]interface{}{
		"user_id":       userID,
		"last_login_at": now.Format(time.RFC3339),
		"updated_at":    now.Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update last login", zap.String("user_id", userID), zap.Error(err))
		return errors.Database("Failed to update last login", err)
	}

	// Cache invalidation removed - no longer using Redis

	return nil
}

// SyncWithKeycloak synchronizes user data with Keycloak
func (s *UserService) SyncWithKeycloak(ctx context.Context, userID string, roles, groups []string, attributes map[string]interface{}) error {
	query := `
		MATCH (u:User {id: $user_id})
		SET u.keycloak_roles = $roles,
		    u.keycloak_groups = $groups,
		    u.keycloak_attributes = $attributes,
		    u.last_sync_at = datetime($last_sync_at),
		    u.updated_at = datetime($updated_at)
		RETURN u
	`

	now := time.Now()
	params := map[string]interface{}{
		"user_id":      userID,
		"roles":        roles,
		"groups":       groups,
		"attributes":   attributes,
		"last_sync_at": now.Format(time.RFC3339),
		"updated_at":   now.Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to sync user with Keycloak", zap.String("user_id", userID), zap.Error(err))
		return errors.Database("Failed to sync user with Keycloak", err)
	}

	// Cache invalidation removed - no longer using Redis

	s.logger.Debug("User synced with Keycloak",
		zap.String("user_id", userID),
		zap.Strings("roles", roles),
		zap.Strings("groups", groups),
	)

	return nil
}

// Helper methods

func (s *UserService) recordToUser(record interface{}) (*models.User, error) {
	r, ok := record.(*neo4j.Record)
	if !ok {
		return nil, errors.Internal("Invalid record type")
	}

	user := &models.User{}

	// Extract values from the record
	if val, ok := r.Get("u.id"); ok && val != nil {
		user.ID = val.(string)
	}
	if val, ok := r.Get("u.keycloak_id"); ok && val != nil {
		user.KeycloakID = val.(string)
	}
	if val, ok := r.Get("u.email"); ok && val != nil {
		user.Email = val.(string)
	}
	if val, ok := r.Get("u.username"); ok && val != nil {
		user.Username = val.(string)
	}
	if val, ok := r.Get("u.full_name"); ok && val != nil {
		user.FullName = val.(string)
	}
	if val, ok := r.Get("u.avatar_url"); ok && val != nil {
		user.AvatarURL = val.(string)
	}
	if val, ok := r.Get("u.status"); ok && val != nil {
		user.Status = val.(string)
	}

	// Parse preferences from JSON string
	if val, ok := r.Get("u.preferences"); ok && val != nil {
		preferencesStr := val.(string)
		if preferencesStr != "" && preferencesStr != "{}" {
			var preferences map[string]interface{}
			if err := json.Unmarshal([]byte(preferencesStr), &preferences); err != nil {
				s.logger.Warn("Failed to parse user preferences", zap.Error(err))
			} else {
				user.Preferences = preferences
			}
		}
	}

	// Parse timestamps
	if val, ok := r.Get("u.created_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			user.CreatedAt = t
		}
	}
	if val, ok := r.Get("u.updated_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			user.UpdatedAt = t
		}
	}
	if val, ok := r.Get("u.last_login_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			user.LastLoginAt = &t
		}
	}
	if val, ok := r.Get("u.last_sync_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			user.LastSyncAt = &t
		}
	}

	// Parse personal tenant fields
	if val, ok := r.Get("u.personal_tenant_id"); ok && val != nil {
		user.PersonalTenantID = val.(string)
	}
	if val, ok := r.Get("u.personal_space_id"); ok && val != nil {
		user.PersonalSpaceID = val.(string)
	}
	if val, ok := r.Get("u.personal_api_key"); ok && val != nil {
		user.PersonalAPIKey = val.(string)
	}

	// Parse arrays
	if val, ok := r.Get("u.keycloak_roles"); ok && val != nil {
		if roles, ok := val.([]interface{}); ok {
			user.KeycloakRoles = make([]string, len(roles))
			for i, role := range roles {
				user.KeycloakRoles[i] = role.(string)
			}
		}
	}
	if val, ok := r.Get("u.keycloak_groups"); ok && val != nil {
		if groups, ok := val.([]interface{}); ok {
			user.KeycloakGroups = make([]string, len(groups))
			for i, group := range groups {
				user.KeycloakGroups[i] = group.(string)
			}
		}
	}

	// Parse keycloak attributes from JSON string
	if val, ok := r.Get("u.keycloak_attributes"); ok && val != nil {
		attributesStr := val.(string)
		if attributesStr != "" && attributesStr != "{}" {
			var attributes map[string]interface{}
			if err := json.Unmarshal([]byte(attributesStr), &attributes); err != nil {
				s.logger.Warn("Failed to parse keycloak attributes", zap.Error(err))
			} else {
				user.KeycloakAttributes = attributes
			}
		}
	}

	// Parse tutorial fields
	if val, ok := r.Get("tutorial_completed"); ok && val != nil {
		if completed, ok := val.(bool); ok {
			user.TutorialCompleted = completed
		}
	}
	if val, ok := r.Get("u.tutorial_completed_at"); ok && val != nil {
		if t, ok := val.(time.Time); ok {
			user.TutorialCompletedAt = &t
		}
	}

	return user, nil
}

func (s *UserService) recordToUserResponse(record interface{}) (*models.UserResponse, error) {
	// Implementation would convert Neo4j record to UserResponse model
	// This is a simplified version
	return &models.UserResponse{}, nil
}

// Redis caching methods removed - no longer using Redis

// UpdateUserPreferences updates user preferences
func (s *UserService) UpdateUserPreferences(ctx context.Context, userID string, preferences models.UserPreferences) (*models.UserPreferences, error) {
	// Implementation would update user preferences in Neo4j
	s.logger.Info("Updating user preferences", zap.String("user_id", userID))
	return &preferences, nil
}

// GetUserStats gets user statistics
func (s *UserService) GetUserStats(ctx context.Context, userID string) (*models.UserStats, error) {
	// Implementation would calculate user statistics from Neo4j
	s.logger.Info("Getting user stats", zap.String("user_id", userID))

	stats := &models.UserStats{
		NotebookCount:  0,
		DocumentCount:  0,
		TotalSizeBytes: 0,
		ProcessedDocs:  0,
		FailedDocs:     0,
		CreatedAt:      "2024-01-01", // Would be actual user creation date
	}

	return stats, nil
}

// UpdatePersonalTenantInfo updates a user's personal tenant information
func (s *UserService) UpdatePersonalTenantInfo(ctx context.Context, userID, tenantID, apiKey string) error {
	query := `
		MATCH (u:User {id: $user_id})
		SET u.personal_tenant_id = $tenant_id,
		    u.personal_api_key = $api_key,
		    u.updated_at = datetime($updated_at)
		RETURN u
	`
	
	params := map[string]interface{}{
		"user_id":    userID,
		"tenant_id":  tenantID,
		"api_key":    apiKey,
		"updated_at": time.Now().Format(time.RFC3339),
	}
	
	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update user personal tenant info", 
			zap.String("user_id", userID), 
			zap.String("tenant_id", tenantID),
			zap.Error(err))
		return errors.Database("Failed to update user personal tenant info", err)
	}
	
	// Cache invalidation removed - no longer using Redis
	
	s.logger.Info("Updated user personal tenant info",
		zap.String("user_id", userID),
		zap.String("tenant_id", tenantID),
	)

	return nil
}

// GetOnboardingStatus returns the user's onboarding status
func (s *UserService) GetOnboardingStatus(ctx context.Context, userID string) (*models.OnboardingStatusResponse, error) {
	s.logger.Debug("Getting onboarding status",
		zap.String("user_id", userID),
	)

	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		s.logger.Error("Failed to get user for onboarding status",
			zap.String("user_id", userID),
			zap.Error(err))
		return nil, err
	}

	return user.ToOnboardingStatusResponse(), nil
}

// MarkTutorialComplete marks the tutorial as completed for a user
func (s *UserService) MarkTutorialComplete(ctx context.Context, userID string) error {
	s.logger.Info("Marking tutorial complete",
		zap.String("user_id", userID),
	)

	query := `
		MATCH (u:User {id: $user_id})
		SET u.tutorial_completed = true,
		    u.tutorial_completed_at = datetime($completed_at),
		    u.updated_at = datetime($updated_at)
		RETURN u
	`

	now := time.Now()
	params := map[string]interface{}{
		"user_id":      userID,
		"completed_at": now.Format(time.RFC3339),
		"updated_at":   now.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to mark tutorial complete",
			zap.String("user_id", userID),
			zap.Error(err))
		return errors.Database("Failed to mark tutorial complete", err)
	}

	if len(result.Records) == 0 {
		s.logger.Warn("User not found for tutorial completion",
			zap.String("user_id", userID))
		return errors.NotFound("User not found")
	}

	s.logger.Info("Tutorial marked complete",
		zap.String("user_id", userID),
	)

	return nil
}

// ResetTutorial resets the tutorial status for a user (testing/re-onboarding)
func (s *UserService) ResetTutorial(ctx context.Context, userID string) error {
	s.logger.Info("Resetting tutorial",
		zap.String("user_id", userID),
	)

	query := `
		MATCH (u:User {id: $user_id})
		SET u.tutorial_completed = false,
		    u.tutorial_completed_at = null,
		    u.updated_at = datetime($updated_at)
		RETURN u
	`

	now := time.Now()
	params := map[string]interface{}{
		"user_id":    userID,
		"updated_at": now.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to reset tutorial",
			zap.String("user_id", userID),
			zap.Error(err))
		return errors.Database("Failed to reset tutorial", err)
	}

	if len(result.Records) == 0 {
		s.logger.Warn("User not found for tutorial reset",
			zap.String("user_id", userID))
		return errors.NotFound("User not found")
	}

	s.logger.Info("Tutorial reset",
		zap.String("user_id", userID),
	)

	return nil
}
