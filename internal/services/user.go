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

// UserService handles user-related business logic
type UserService struct {
	neo4j  *database.Neo4jClient
	redis  *database.RedisClient
	logger *logger.Logger
}

// NewUserService creates a new user service
func NewUserService(neo4j *database.Neo4jClient, redis *database.RedisClient, log *logger.Logger) *UserService {
	return &UserService{
		neo4j:  neo4j,
		redis:  redis,
		logger: log.WithService("user_service"),
	}
}

// CreateUser creates a new user
func (s *UserService) CreateUser(ctx context.Context, req models.UserCreateRequest) (*models.User, error) {
	// Check if user already exists by email or Keycloak ID
	existingUser, err := s.GetUserByEmail(ctx, req.Email)
	if err == nil && existingUser != nil {
		return nil, errors.ConflictWithDetails("User already exists", map[string]interface{}{
			"email": req.Email,
		})
	}

	existingUser, err = s.GetUserByKeycloakID(ctx, req.KeycloakID)
	if err == nil && existingUser != nil {
		return nil, errors.ConflictWithDetails("User already exists", map[string]interface{}{
			"keycloak_id": req.KeycloakID,
		})
	}

	// Create new user
	user := models.NewUser(req)

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
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN u
	`

	params := map[string]interface{}{
		"id":          user.ID,
		"keycloak_id": user.KeycloakID,
		"email":       user.Email,
		"username":    user.Username,
		"full_name":   user.FullName,
		"avatar_url":  user.AvatarURL,
		"preferences": user.Preferences,
		"status":      user.Status,
		"created_at":  user.CreatedAt.Format(time.RFC3339),
		"updated_at":  user.UpdatedAt.Format(time.RFC3339),
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

// GetUserByID retrieves a user by ID
func (s *UserService) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	// Try cache first
	cacheKey := fmt.Sprintf("user:%s", userID)
	if cachedUser, err := s.getUserFromCache(ctx, cacheKey); err == nil && cachedUser != nil {
		return cachedUser, nil
	}

	query := `
		MATCH (u:User {id: $user_id})
		RETURN u.id, u.keycloak_id, u.email, u.username, u.full_name, u.avatar_url,
		       u.keycloak_roles, u.keycloak_groups, u.keycloak_attributes,
		       u.preferences, u.status, u.created_at, u.updated_at,
		       u.last_login_at, u.last_sync_at
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

	// Cache the user
	s.cacheUser(ctx, cacheKey, user)

	return user, nil
}

// GetUserByEmail retrieves a user by email
func (s *UserService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		MATCH (u:User {email: $email})
		RETURN u.id, u.keycloak_id, u.email, u.username, u.full_name, u.avatar_url,
		       u.keycloak_roles, u.keycloak_groups, u.keycloak_attributes,
		       u.preferences, u.status, u.created_at, u.updated_at,
		       u.last_login_at, u.last_sync_at
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
		       u.last_login_at, u.last_sync_at
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

	params := map[string]interface{}{
		"user_id":     userID,
		"full_name":   user.FullName,
		"avatar_url":  user.AvatarURL,
		"preferences": user.Preferences,
		"status":      user.Status,
		"updated_at":  user.UpdatedAt.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update user", zap.String("user_id", userID), zap.Error(err))
		return nil, errors.Database("Failed to update user", err)
	}

	// Invalidate cache
	cacheKey := fmt.Sprintf("user:%s", userID)
	if err := s.redis.Delete(ctx, cacheKey); err != nil {
		s.logger.Warn("Failed to invalidate user cache", zap.Error(err))
	}

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

	// Invalidate cache
	cacheKey := fmt.Sprintf("user:%s", userID)
	if err := s.redis.Delete(ctx, cacheKey); err != nil {
		s.logger.Warn("Failed to invalidate user cache", zap.Error(err))
	}

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

	// Invalidate cache
	cacheKey := fmt.Sprintf("user:%s", userID)
	if err := s.redis.Delete(ctx, cacheKey); err != nil {
		s.logger.Warn("Failed to invalidate user cache", zap.Error(err))
	}

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

	// Invalidate cache
	cacheKey := fmt.Sprintf("user:%s", userID)
	if err := s.redis.Delete(ctx, cacheKey); err != nil {
		s.logger.Warn("Failed to invalidate user cache", zap.Error(err))
	}

	s.logger.Debug("User synced with Keycloak",
		zap.String("user_id", userID),
		zap.Strings("roles", roles),
		zap.Strings("groups", groups),
	)

	return nil
}

// Helper methods

func (s *UserService) recordToUser(record interface{}) (*models.User, error) {
	// Implementation would convert Neo4j record to User model
	// This is a simplified version
	return &models.User{}, nil
}

func (s *UserService) recordToUserResponse(record interface{}) (*models.UserResponse, error) {
	// Implementation would convert Neo4j record to UserResponse model
	// This is a simplified version
	return &models.UserResponse{}, nil
}

func (s *UserService) getUserFromCache(ctx context.Context, key string) (*models.User, error) {
	// Implementation would deserialize user from Redis cache
	return nil, fmt.Errorf("not implemented")
}

func (s *UserService) cacheUser(ctx context.Context, key string, user *models.User) {
	// Implementation would serialize and cache user in Redis
	// Cache for 1 hour
	if err := s.redis.Set(ctx, key, user, time.Hour); err != nil {
		s.logger.Warn("Failed to cache user", zap.Error(err))
	}
}

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
