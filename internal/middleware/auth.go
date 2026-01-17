package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/auth"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// AuthMiddleware handles JWT token validation
func AuthMiddleware(keycloakClient *auth.KeycloakClient, log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			log.Warn("Missing authorization header")
			c.JSON(http.StatusUnauthorized, errors.NewAPIError(
				errors.ErrUnauthorized,
				"Authorization header is required",
				nil,
			))
			c.Abort()
			return
		}

		// Check Bearer prefix
		tokenParts := strings.SplitN(authHeader, " ", 2)
		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
			log.Warn("Invalid authorization header format")
			c.JSON(http.StatusUnauthorized, errors.NewAPIError(
				errors.ErrUnauthorized,
				"Invalid authorization header format",
				nil,
			))
			c.Abort()
			return
		}

		idToken := tokenParts[1]

		// Verify and parse the ID token
		ctx := context.Background()
		claims, err := keycloakClient.VerifyIDToken(ctx, idToken)
		if err != nil {
			log.Warn("Token verification failed", zap.Error(err))
			c.JSON(http.StatusUnauthorized, errors.NewAPIError(
				errors.ErrUnauthorized,
				"Invalid or expired token",
				nil,
			))
			c.Abort()
			return
		}

		// Store user information in context - use Keycloak ID as user_id for now
		// This will be resolved to internal user ID by handlers that need it
		c.Set("user_id", claims.Sub)
		c.Set("keycloak_id", claims.Sub)
		c.Set("user_email", claims.Email)
		c.Set("user_name", claims.Name)
		c.Set("username", claims.PreferredUsername)
		c.Set("user_claims", claims)

		// Add user info to logger context
		requestLogger := log.WithUserID(claims.Sub)
		c.Set("logger", requestLogger)

		log.Debug("User authenticated successfully",
			zap.String("user_id", claims.Sub),
			zap.String("email", claims.Email),
			zap.String("username", claims.PreferredUsername),
		)

		c.Next()
	}
}

// NOTE: RequireRole and RequireGroup middleware functions are now in role.go

// RequireAdmin middleware ensures user has admin privileges
func RequireAdmin(keycloakClient *auth.KeycloakClient, log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, exists := c.Get("user_claims")
		if !exists {
			log.Error("User claims not found in context")
			c.JSON(http.StatusInternalServerError, errors.NewAPIError(
				errors.ErrInternal,
				"Authentication context not found",
				nil,
			))
			c.Abort()
			return
		}

		userClaims, ok := claims.(*auth.TokenClaims)
		if !ok {
			log.Error("Invalid user claims type in context")
			c.JSON(http.StatusInternalServerError, errors.NewAPIError(
				errors.ErrInternal,
				"Invalid authentication context",
				nil,
			))
			c.Abort()
			return
		}

		if !keycloakClient.IsAdmin(userClaims) {
			log.Warn("Admin access denied",
				zap.String("user_id", userClaims.Sub),
				zap.Strings("user_roles", userClaims.RealmAccess.Roles),
			)
			c.JSON(http.StatusForbidden, errors.NewAPIError(
				errors.ErrForbidden,
				"Admin privileges required",
				nil,
			))
			c.Abort()
			return
		}

		c.Next()
	}
}

// OptionalAuth middleware extracts user information if token is present but doesn't require it
func OptionalAuth(keycloakClient *auth.KeycloakClient, log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// No token provided, continue without authentication
			c.Next()
			return
		}

		// Check Bearer prefix
		tokenParts := strings.SplitN(authHeader, " ", 2)
		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
			// Invalid format, continue without authentication
			c.Next()
			return
		}

		idToken := tokenParts[1]

		// Try to verify and parse the ID token
		ctx := context.Background()
		claims, err := keycloakClient.VerifyIDToken(ctx, idToken)
		if err != nil {
			// Token verification failed, continue without authentication
			log.Debug("Optional token verification failed", zap.Error(err))
			c.Next()
			return
		}

		// Store user information in context
		c.Set("user_id", claims.Sub)
		c.Set("user_email", claims.Email)
		c.Set("user_name", claims.Name)
		c.Set("username", claims.PreferredUsername)
		c.Set("user_claims", claims)

		// Add user info to logger context
		requestLogger := log.WithUserID(claims.Sub)
		c.Set("logger", requestLogger)

		log.Debug("Optional authentication successful",
			zap.String("user_id", claims.Sub),
			zap.String("email", claims.Email),
		)

		c.Next()
	}
}

// RequestID middleware adds a unique request ID to each request
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if request ID is already set (e.g., from load balancer)
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Set request ID in context and response header
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)

		c.Next()
	}
}

// GetUserID extracts user ID from Gin context
func GetUserID(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return "", false
	}

	if id, ok := userID.(string); ok {
		return id, true
	}

	return "", false
}

// GetUserClaims extracts user claims from Gin context
func GetUserClaims(c *gin.Context) (*auth.TokenClaims, bool) {
	claims, exists := c.Get("user_claims")
	if !exists {
		return nil, false
	}

	if userClaims, ok := claims.(*auth.TokenClaims); ok {
		return userClaims, true
	}

	return nil, false
}

// GetRequestID extracts request ID from Gin context
func GetRequestID(c *gin.Context) string {
	requestID, exists := c.Get("request_id")
	if !exists {
		return ""
	}

	if id, ok := requestID.(string); ok {
		return id
	}

	return ""
}

// GetLogger extracts logger from Gin context
func GetLogger(c *gin.Context) *logger.Logger {
	log, exists := c.Get("logger")
	if !exists {
		// Return a default logger if not found
		defaultLogger, _ := logger.NewDefault()
		return defaultLogger
	}

	if contextLogger, ok := log.(*logger.Logger); ok {
		return contextLogger
	}

	// Return a default logger if type assertion fails
	defaultLogger, _ := logger.NewDefault()
	return defaultLogger
}
