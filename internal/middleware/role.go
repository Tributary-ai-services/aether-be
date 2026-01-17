package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// RequireRole middleware ensures user has required role
func RequireRole(requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user roles from context (set by auth middleware)
		roles, exists := c.Get("user_roles")
		if !exists {
			c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
			c.Abort()
			return
		}

		userRoles, ok := roles.([]string)
		if !ok {
			c.JSON(http.StatusInternalServerError, errors.Internal("Invalid user roles"))
			c.Abort()
			return
		}

		// Check if user has required role
		hasRole := false
		for _, role := range userRoles {
			if role == requiredRole {
				hasRole = true
				break
			}
		}

		if !hasRole {
			c.JSON(http.StatusForbidden, errors.Forbidden("Insufficient permissions"))
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAnyRole middleware ensures user has at least one of the required roles
func RequireAnyRole(requiredRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user roles from context (set by auth middleware)
		roles, exists := c.Get("user_roles")
		if !exists {
			c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
			c.Abort()
			return
		}

		userRoles, ok := roles.([]string)
		if !ok {
			c.JSON(http.StatusInternalServerError, errors.Internal("Invalid user roles"))
			c.Abort()
			return
		}

		// Check if user has any of the required roles
		hasRole := false
		for _, userRole := range userRoles {
			for _, requiredRole := range requiredRoles {
				if userRole == requiredRole {
					hasRole = true
					break
				}
			}
			if hasRole {
				break
			}
		}

		if !hasRole {
			c.JSON(http.StatusForbidden, errors.Forbidden("Insufficient permissions"))
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireGroup middleware ensures user belongs to required group
func RequireGroup(requiredGroup string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user groups from context (set by auth middleware)
		groups, exists := c.Get("user_groups")
		if !exists {
			c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
			c.Abort()
			return
		}

		userGroups, ok := groups.([]string)
		if !ok {
			c.JSON(http.StatusInternalServerError, errors.Internal("Invalid user groups"))
			c.Abort()
			return
		}

		// Check if user belongs to required group
		hasGroup := false
		for _, group := range userGroups {
			if group == requiredGroup {
				hasGroup = true
				break
			}
		}

		if !hasGroup {
			c.JSON(http.StatusForbidden, errors.Forbidden("Access denied - group membership required"))
			c.Abort()
			return
		}

		c.Next()
	}
}
