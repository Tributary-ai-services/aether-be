package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// SpaceContextKey is the key used to store space context in gin context
const SpaceContextKey = "space_context"

// SpaceContextMiddleware creates a middleware that resolves space context from request
func SpaceContextMiddleware(spaceService *services.SpaceContextService, log *logger.Logger) gin.HandlerFunc {
	logger := log.WithService("space_context_middleware")

	return func(c *gin.Context) {
		// Get authenticated user ID from context (set by auth middleware)
		userID, exists := c.Get("user_id")
		if !exists {
			logger.Error("User ID not found in context")
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Authentication required",
			})
			c.Abort()
			return
		}

		// Extract space information from request
		spaceType, spaceID, err := extractSpaceInfo(c)
		if err != nil {
			logger.Error("Failed to extract space info",
				zap.Error(err),
				zap.String("path", c.Request.URL.Path),
			)
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			c.Abort()
			return
		}

		// If no space info in request, skip (some endpoints don't require space context)
		if spaceType == "" || spaceID == "" {
			c.Next()
			return
		}

		// Resolve space context
		logger.Info("=== SPACE CONTEXT MIDDLEWARE ===",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("user_id", userID.(string)),
			zap.String("space_type", spaceType),
			zap.String("space_id", spaceID))
			
		req := models.SpaceContextRequest{
			SpaceType: models.SpaceType(spaceType),
			SpaceID:   spaceID,
		}

		logger.Info("About to resolve space context")
		spaceContext, err := spaceService.ResolveSpaceContext(c.Request.Context(), userID.(string), req)
		logger.Info("Space context resolution completed", zap.Bool("has_error", err != nil))
		if err != nil {
			logger.Error("Failed to resolve space context",
				zap.Error(err),
				zap.String("user_id", userID.(string)),
				zap.String("space_type", spaceType),
				zap.String("space_id", spaceID),
			)

			// Return appropriate error response
			switch {
			case errors.IsNotFound(err):
				c.JSON(http.StatusNotFound, gin.H{
					"error": "Space not found",
				})
			case errors.IsForbidden(err):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "Access to space denied",
				})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Failed to resolve space context",
				})
			}
			c.Abort()
			return
		}

		// Store space context in gin context
		c.Set(SpaceContextKey, spaceContext)

		logger.Debug("Space context resolved",
			zap.String("user_id", userID.(string)),
			zap.String("space_type", string(spaceContext.SpaceType)),
			zap.String("space_id", spaceContext.SpaceID),
			zap.String("tenant_id", spaceContext.TenantID),
		)

		logger.Info("Space context middleware proceeding to next")
		c.Next()
		logger.Info("Space context middleware completed")
	}
}

// RequireSpaceContext creates a middleware that ensures space context is present
func RequireSpaceContext(log *logger.Logger) gin.HandlerFunc {
	logger := log.WithService("require_space_context")

	return func(c *gin.Context) {
		logger.Info("=== REQUIRE SPACE CONTEXT CHECK ===",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("content_type", c.Request.Header.Get("Content-Type")))
		
		_, exists := c.Get(SpaceContextKey)
		if !exists {
			logger.Error("Space context required but not found",
				zap.String("path", c.Request.URL.Path),
			)
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Space context required",
			})
			c.Abort()
			return
		}

		logger.Info("Space context check passed")
		
		logger.Info("RequireSpaceContext proceeding to next middleware")
		c.Next()
		logger.Info("RequireSpaceContext middleware completed")
	}
}

// RequireSpacePermission creates a middleware that checks for specific permissions
func RequireSpacePermission(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		spaceContext, exists := c.Get(SpaceContextKey)
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Space context required",
			})
			c.Abort()
			return
		}

		ctx := spaceContext.(*models.SpaceContext)

		// Check if user has any of the required permissions
		hasPermission := false
		for _, permission := range permissions {
			if ctx.HasPermission(permission) {
				hasPermission = true
				break
			}
		}

		if !hasPermission {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Insufficient permissions",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// extractSpaceInfo extracts space type and ID from the request
func extractSpaceInfo(c *gin.Context) (spaceType, spaceID string, err error) {
	// 1. Check headers first (highest priority)
	if headerType := c.GetHeader("X-Space-Type"); headerType != "" {
		spaceType = headerType
		spaceID = c.GetHeader("X-Space-ID")
		if spaceID == "" {
			return "", "", errors.BadRequest("X-Space-ID header required when X-Space-Type is provided")
		}
		return spaceType, spaceID, nil
	}

	// 2. Check URL path parameters
	// Example: /api/v1/spaces/:space_type/:space_id/notebooks
	if pathType := c.Param("space_type"); pathType != "" {
		spaceType = pathType
		spaceID = c.Param("space_id")
		if spaceID == "" {
			return "", "", errors.BadRequest("space_id parameter required in URL")
		}
		return spaceType, spaceID, nil
	}

	// 3. Check query parameters
	if queryType := c.Query("space_type"); queryType != "" {
		spaceType = queryType
		spaceID = c.Query("space_id")
		if spaceID == "" {
			return "", "", errors.BadRequest("space_id query parameter required when space_type is provided")
		}
		return spaceType, spaceID, nil
	}

	// 4. Special handling for certain endpoints
	// Example: /api/v1/personal/notebooks implies personal space for current user
	if strings.Contains(c.Request.URL.Path, "/personal/") {
		userID, exists := c.Get("user_id")
		if exists {
			return "personal", userID.(string), nil
		}
	}

	// No space information found (this is OK for some endpoints)
	return "", "", nil
}

// GetSpaceContext retrieves the space context from gin context
func GetSpaceContext(c *gin.Context) (*models.SpaceContext, error) {
	value, exists := c.Get(SpaceContextKey)
	if !exists {
		return nil, errors.Internal("Space context not found in request context")
	}

	spaceContext, ok := value.(*models.SpaceContext)
	if !ok {
		return nil, errors.Internal("Invalid space context type in request context")
	}

	return spaceContext, nil
}

// MustGetSpaceContext retrieves the space context from gin context and panics if not found
func MustGetSpaceContext(c *gin.Context) *models.SpaceContext {
	spaceContext, err := GetSpaceContext(c)
	if err != nil {
		panic(err)
	}
	return spaceContext
}