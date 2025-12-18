package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// UserHandler handles user-related HTTP requests
type UserHandler struct {
	userService       *services.UserService
	spaceService      *services.SpaceContextService
	onboardingService *services.OnboardingService
	logger            *logger.Logger
}

// NewUserHandler creates a new user handler
func NewUserHandler(
	userService *services.UserService,
	spaceService *services.SpaceContextService,
	onboardingService *services.OnboardingService,
	log *logger.Logger,
) *UserHandler {
	return &UserHandler{
		userService:       userService,
		spaceService:      spaceService,
		onboardingService: onboardingService,
		logger:            log.WithService("user_handler"),
	}
}

// GetCurrentUser gets current user profile
// @Summary Get current user profile
// @Description Get the profile of the currently authenticated user
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {object} models.UserResponse
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/me [get]
func (h *UserHandler) GetCurrentUser(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// First try to find user by Keycloak ID (from JWT sub claim)
	user, err := h.userService.GetUserByKeycloakID(c.Request.Context(), userID)
	if err != nil {
		// If user doesn't exist in Neo4j, try to create from JWT token
		if errors.IsNotFound(err) {
			h.logger.Info("User not found in database, creating from JWT token", zap.String("keycloak_id", userID))
			
			// Extract user info from JWT token context
			email, _ := c.Get("user_email")
			name, _ := c.Get("user_name")
			username, _ := c.Get("username")
			
			emailStr, _ := email.(string)
			nameStr, _ := name.(string)
			usernameStr, _ := username.(string)
			
			// If username is empty, use email as username
			if usernameStr == "" {
				usernameStr = emailStr
			}
			
			// Create user from JWT token data
			createReq := models.UserCreateRequest{
				KeycloakID: userID,
				Email:      emailStr,
				Username:   usernameStr,
				FullName:   nameStr,
			}
			
			user, err = h.userService.CreateUser(c.Request.Context(), createReq)
			if err != nil {
				// Check if it's a conflict error (user already exists)
				if errors.IsConflict(err) {
					h.logger.Warn("User creation conflict, attempting to fetch existing user", zap.String("keycloak_id", userID), zap.Error(err))
					// Try one more time to get the user
					user, err = h.userService.GetUserByKeycloakID(c.Request.Context(), userID)
					if err != nil {
						h.logger.Error("Failed to fetch existing user after conflict", zap.String("keycloak_id", userID), zap.Error(err))
						c.JSON(http.StatusInternalServerError, errors.Internal("Failed to retrieve user profile"))
						return
					}
				} else {
					h.logger.Error("Failed to create user from JWT token", zap.String("keycloak_id", userID), zap.Error(err))
					c.JSON(http.StatusInternalServerError, errors.Internal("Failed to create user profile"))
					return
				}
			} else {
				h.logger.Info("Successfully created user from JWT token", zap.String("keycloak_id", userID), zap.String("email", emailStr))

				// Trigger automatic onboarding for new user (async)
				go func(createdUser *models.User) {
					onboardCtx := context.Background()
					onboardingResult, err := h.onboardingService.OnboardNewUser(onboardCtx, createdUser)
					if err != nil {
						h.logger.Error("Failed to onboard new user",
							zap.String("user_id", createdUser.ID),
							zap.String("keycloak_id", userID),
							zap.Error(err),
						)
					} else {
						h.logger.Info("User onboarding completed",
							zap.String("user_id", createdUser.ID),
							zap.Bool("success", onboardingResult.Success),
							zap.Int64("duration_ms", onboardingResult.DurationMs),
							zap.Int("steps_completed", len(onboardingResult.Steps)),
						)
					}
				}(user)
			}
		} else {
			h.logger.Error("Failed to get current user", zap.String("user_id", userID), zap.Error(err))
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to retrieve user"))
			return
		}
	}

	c.JSON(http.StatusOK, user.ToResponse())
}

// UpdateCurrentUser updates current user profile
// @Summary Update current user profile
// @Description Update the profile of the currently authenticated user
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param user body models.UserUpdateRequest true "User update data"
// @Success 200 {object} models.UserResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/me [put]
func (h *UserHandler) UpdateCurrentUser(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	var req models.UserUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Sanitize and validate request
	sanitized, err := sanitizeAndValidate(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	sanitizedReq := sanitized.(models.UserUpdateRequest)

	user, err := h.userService.UpdateUser(c.Request.Context(), userID, sanitizedReq)
	if err != nil {
		h.logger.Error("Failed to update user", zap.String("user_id", userID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, user.ToResponse())
}

// GetUserByID gets user by ID
// @Summary Get user by ID
// @Description Get user profile by ID
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "User ID"
// @Success 200 {object} models.PublicUserResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/{id} [get]
func (h *UserHandler) GetUserByID(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("User ID is required", nil))
		return
	}

	user, err := h.userService.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get user by ID", zap.String("user_id", userID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Return public user response (limited fields)
	c.JSON(http.StatusOK, user.ToPublicResponse())
}

// SearchUsers searches for users
// @Summary Search users
// @Description Search for users by query, username, or email
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param query query string false "Search query"
// @Param username query string false "Username filter"
// @Param email query string false "Email filter"
// @Param status query string false "Status filter"
// @Param role query string false "Role filter"
// @Param limit query int false "Results limit (max 100)" default(20)
// @Param offset query int false "Results offset" default(0)
// @Success 200 {object} models.UserListResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/search [get]
func (h *UserHandler) SearchUsers(c *gin.Context) {
	var req models.UserSearchRequest

	// Parse query parameters
	req.Query = c.Query("query")
	req.Username = c.Query("username")
	req.Email = c.Query("email")
	req.Status = c.Query("status")
	// Note: Role field not available in UserSearchRequest

	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			req.Limit = limit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			req.Offset = offset
		}
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	response, err := h.userService.SearchUsers(c.Request.Context(), req)
	if err != nil {
		h.logger.Error("Failed to search users", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// UpdateUserPreferences updates user preferences
// @Summary Update user preferences
// @Description Update user preferences and settings
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param preferences body models.UserPreferences true "User preferences"
// @Success 200 {object} models.UserPreferences
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/me/preferences [put]
func (h *UserHandler) UpdateUserPreferences(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	var preferences models.UserPreferences
	if err := c.ShouldBindJSON(&preferences); err != nil {
		h.logger.Error("Invalid preferences payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid preferences payload", err))
		return
	}

	updatedPrefs, err := h.userService.UpdateUserPreferences(c.Request.Context(), userID, preferences)
	if err != nil {
		h.logger.Error("Failed to update user preferences", zap.String("user_id", userID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, updatedPrefs)
}

// GetUserStats gets user statistics
// @Summary Get user statistics
// @Description Get statistics for the current user
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {object} models.UserStats
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/me/stats [get]
func (h *UserHandler) GetUserStats(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	stats, err := h.userService.GetUserStats(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get user stats", zap.String("user_id", userID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, stats)
}

// DeleteCurrentUser deletes current user account
// @Summary Delete current user account
// @Description Delete the currently authenticated user account (soft delete)
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/me [delete]
func (h *UserHandler) DeleteCurrentUser(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	err := h.userService.DeleteUser(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to delete user", zap.String("user_id", userID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetUserSpaces gets available spaces for the current user
// @Summary Get user spaces
// @Description Get all spaces accessible to the current user
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {array} models.SpaceInfo
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/me/spaces [get]
func (h *UserHandler) GetUserSpaces(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get internal user ID from Keycloak ID
	user, err := h.userService.GetUserByKeycloakID(c.Request.Context(), userID)
	if err != nil {
		// If user doesn't exist in Neo4j, try to create from JWT token
		if errors.IsNotFound(err) {
			h.logger.Info("User not found in database, creating from JWT token", zap.String("keycloak_id", userID))
			
			// Extract user info from JWT token context
			email, _ := c.Get("user_email")
			name, _ := c.Get("user_name")
			username, _ := c.Get("username")
			
			emailStr, _ := email.(string)
			nameStr, _ := name.(string)
			usernameStr, _ := username.(string)
			
			// If username is empty, use email as username
			if usernameStr == "" {
				usernameStr = emailStr
			}
			
			// Create user from JWT token data
			createReq := models.UserCreateRequest{
				KeycloakID: userID,
				Email:      emailStr,
				Username:   usernameStr,
				FullName:   nameStr,
			}
			
			user, err = h.userService.CreateUser(c.Request.Context(), createReq)
			if err != nil {
				// Check if it's a conflict error (user already exists)
				if errors.IsConflict(err) {
					h.logger.Warn("User creation conflict, attempting to fetch existing user", zap.String("keycloak_id", userID), zap.Error(err))
					// Try one more time to get the user
					user, err = h.userService.GetUserByKeycloakID(c.Request.Context(), userID)
					if err != nil {
						h.logger.Error("Failed to fetch existing user after conflict", zap.String("keycloak_id", userID), zap.Error(err))
						c.JSON(http.StatusInternalServerError, errors.Internal("Failed to retrieve user profile"))
						return
					}
				} else {
					h.logger.Error("Failed to create user from JWT token", zap.String("keycloak_id", userID), zap.Error(err))
					c.JSON(http.StatusInternalServerError, errors.Internal("Failed to create user profile"))
					return
				}
			} else {
				h.logger.Info("Successfully created user from JWT token", zap.String("keycloak_id", userID), zap.String("email", emailStr))

				// Trigger automatic onboarding for new user (async)
				go func(createdUser *models.User) {
					onboardCtx := context.Background()
					onboardingResult, err := h.onboardingService.OnboardNewUser(onboardCtx, createdUser)
					if err != nil {
						h.logger.Error("Failed to onboard new user",
							zap.String("user_id", createdUser.ID),
							zap.String("keycloak_id", userID),
							zap.Error(err),
						)
					} else {
						h.logger.Info("User onboarding completed",
							zap.String("user_id", createdUser.ID),
							zap.Bool("success", onboardingResult.Success),
							zap.Int64("duration_ms", onboardingResult.DurationMs),
							zap.Int("steps_completed", len(onboardingResult.Steps)),
						)
					}
				}(user)
			}
		} else {
			h.logger.Error("Failed to get user", zap.String("keycloak_id", userID), zap.Error(err))
			handleServiceError(c, err)
			return
		}
	}

	spaces, err := h.spaceService.GetUserSpaces(c.Request.Context(), user.ID)
	if err != nil {
		h.logger.Error("Failed to get user spaces", zap.String("user_id", user.ID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, spaces)
}

// GetOnboardingStatus gets onboarding status for current user
// @Summary Get onboarding status
// @Description Check if user onboarding is complete and get default resources
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {object} models.OnboardingResult
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/users/me/onboarding [get]
func (h *UserHandler) GetOnboardingStatus(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get internal user ID from Keycloak ID
	user, err := h.userService.GetUserByKeycloakID(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get user for onboarding status", zap.String("keycloak_id", userID), zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get personal space context
	spaceReq := models.SpaceContextRequest{
		SpaceType: models.SpaceTypePersonal,
		SpaceID:   user.PersonalSpaceID,
	}
	spaceCtx, err := h.spaceService.ResolveSpaceContext(c.Request.Context(), user.ID, spaceReq)
	if err != nil {
		h.logger.Error("Failed to resolve personal space for onboarding status",
			zap.String("user_id", user.ID),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to resolve personal space"))
		return
	}

	// Get onboarding status
	status, err := h.onboardingService.GetOnboardingStatus(c.Request.Context(), user, spaceCtx)
	if err != nil {
		h.logger.Error("Failed to get onboarding status",
			zap.String("user_id", user.ID),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get onboarding status"))
		return
	}

	c.JSON(http.StatusOK, status)
}
