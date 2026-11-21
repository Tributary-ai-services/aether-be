package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

// InternalAuthRequired gates /internal/v1/* on a static shared secret in the
// Internal-Auth header, for in-cluster callers with no Keycloak user (Plan #22
// 2.1/2.2: aiqg-dashboard-be's org link check, first sync and nightly
// reconcile). Same contract as aiqg-dashboard-be's middleware of the same name.
// The comparison is constant-time. An empty expected token is a wiring bug:
// the group must not be mounted without one, so this fails closed.
func InternalAuthRequired(expected string) gin.HandlerFunc {
	if expected == "" {
		panic("middleware.InternalAuthRequired: empty token (route should not have mounted)")
	}
	want := []byte(expected)
	return func(c *gin.Context) {
		got := c.GetHeader("Internal-Auth")
		if got == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "internal_auth_missing",
				"message": "Internal-Auth header required for /internal/v1/* endpoints"}})
			return
		}
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "internal_auth_invalid",
				"message": "Internal-Auth header did not match"}})
			return
		}
		c.Next()
	}
}
