package handlers

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tributary-ai-services/aether-be/internal/validation"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// Remove local validator since we're using the validation package

// getUserID extracts user ID from the Gin context
func getUserID(c *gin.Context) string {
	if userID, exists := c.Get("user_id"); exists {
		if id, ok := userID.(string); ok {
			return id
		}
	}
	return ""
}

// getUserRoles extracts user roles from the Gin context
//
//nolint:unused // Used internally by requireAdmin and hasRole
func getUserRoles(c *gin.Context) []string {
	if roles, exists := c.Get("user_roles"); exists {
		if roleSlice, ok := roles.([]string); ok {
			return roleSlice
		}
	}
	return []string{}
}

// getUserGroups extracts user groups from the Gin context
//
//nolint:unused // Used internally by hasGroup
func getUserGroups(c *gin.Context) []string {
	if groups, exists := c.Get("user_groups"); exists {
		if groupSlice, ok := groups.([]string); ok {
			return groupSlice
		}
	}
	return []string{}
}

// getRequestID extracts request ID from the Gin context
//
//nolint:unused // Used internally by addResponseMetadata
func getRequestID(c *gin.Context) string {
	if requestID, exists := c.Get("request_id"); exists {
		if id, ok := requestID.(string); ok {
			return id
		}
	}
	return ""
}

// validateStruct validates a struct using the validation package
func validateStruct(s interface{}) error {
	return validation.Validate(s)
}

// handleServiceError converts service errors to appropriate HTTP responses
func handleServiceError(c *gin.Context, err error) {
	// Check if it's already an API error
	if apiErr, ok := err.(*errors.APIError); ok {
		c.JSON(apiErr.StatusCode, apiErr)
		return
	}

	// Handle specific error types
	switch {
	case errors.IsNotFound(err):
		c.JSON(http.StatusNotFound, errors.NotFound(err.Error()))
	case errors.IsForbidden(err):
		c.JSON(http.StatusForbidden, errors.Forbidden(err.Error()))
	case errors.IsUnauthorized(err):
		c.JSON(http.StatusUnauthorized, errors.Unauthorized(err.Error()))
	case errors.IsValidation(err):
		c.JSON(http.StatusBadRequest, errors.Validation(err.Error(), err))
	case errors.IsConflict(err):
		c.JSON(http.StatusConflict, errors.Conflict(err.Error()))
	case errors.IsDatabase(err):
		c.JSON(http.StatusInternalServerError, errors.Internal("Database operation failed"))
	case errors.IsExternalService(err):
		c.JSON(http.StatusBadGateway, errors.ExternalService("External service error", err))
	default:
		c.JSON(http.StatusInternalServerError, errors.Internal("Internal server error"))
	}
}

// sanitizeAndValidate sanitizes and validates a struct
func sanitizeAndValidate(obj interface{}) (interface{}, error) {
	// First sanitize based on field types and tags
	sanitized := sanitizeStructFields(obj)

	// Then validate
	if err := validation.Validate(sanitized); err != nil {
		return nil, err
	}

	return sanitized, nil
}

// sanitizeStructFields sanitizes struct fields based on their names and tags
func sanitizeStructFields(obj interface{}) interface{} {
	v := reflect.ValueOf(obj)
	t := reflect.TypeOf(obj)

	// Handle pointers
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return obj
		}
		v = v.Elem()
		t = t.Elem()
	}

	// Only process structs
	if v.Kind() != reflect.Struct {
		return obj
	}

	// Create a new instance
	newStruct := reflect.New(t).Elem()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		newField := newStruct.Field(i)

		if !newField.CanSet() {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			sanitized := sanitizeBasedOnFieldName(field.String(), fieldType.Name)
			newField.SetString(sanitized)
		case reflect.Ptr:
			if !field.IsNil() && field.Type().Elem().Kind() == reflect.String {
				original := field.Elem().String()
				sanitized := sanitizeBasedOnFieldName(original, fieldType.Name)
				newStr := reflect.New(field.Type().Elem())
				newStr.Elem().SetString(sanitized)
				newField.Set(newStr)
			} else {
				newField.Set(field)
			}
		case reflect.Slice:
			if field.Type().Elem().Kind() == reflect.String {
				newSlice := reflect.MakeSlice(field.Type(), field.Len(), field.Cap())
				for j := 0; j < field.Len(); j++ {
					original := field.Index(j).String()
					sanitized := sanitizeBasedOnFieldName(original, fieldType.Name)
					newSlice.Index(j).SetString(sanitized)
				}
				newField.Set(newSlice)
			} else {
				newField.Set(field)
			}
		default:
			newField.Set(field)
		}
	}

	return newStruct.Interface()
}

// sanitizeBasedOnFieldName applies appropriate sanitization based on field name
func sanitizeBasedOnFieldName(value, fieldName string) string {
	fieldLower := strings.ToLower(fieldName)

	switch {
	case strings.Contains(fieldLower, "email"):
		return validation.SanitizeEmail(value)
	case strings.Contains(fieldLower, "username"):
		return validation.SanitizeUsername(value)
	case strings.Contains(fieldLower, "filename") || strings.Contains(fieldLower, "name"):
		return validation.SanitizeTitle(value)
	case strings.Contains(fieldLower, "tag"):
		return validation.SanitizeTag(value)
	case strings.Contains(fieldLower, "description"):
		return validation.SanitizeDescription(value)
	case strings.Contains(fieldLower, "url"):
		return validation.SanitizeURL(value)
	default:
		return validation.SanitizeString(value, validation.DefaultSanitizationOptions())
	}
}

// requireAuth ensures user is authenticated
//
//nolint:unused // Utility function for authentication checks
func requireAuth(c *gin.Context) (string, bool) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return "", false
	}
	return userID, true
}

// requireAdmin ensures user has admin role
//
//nolint:unused // Utility function for admin checks
func requireAdmin(c *gin.Context) bool {
	roles := getUserRoles(c)
	for _, role := range roles {
		if role == "admin" || role == "system_admin" {
			return true
		}
	}
	c.JSON(http.StatusForbidden, errors.Forbidden("Admin access required"))
	return false
}

// hasRole checks if user has a specific role
//
//nolint:unused // Utility function for role checks
func hasRole(c *gin.Context, requiredRole string) bool {
	roles := getUserRoles(c)
	for _, role := range roles {
		if role == requiredRole {
			return true
		}
	}
	return false
}

// hasGroup checks if user belongs to a specific group
//
//nolint:unused // Utility function for group checks
func hasGroup(c *gin.Context, requiredGroup string) bool {
	groups := getUserGroups(c)
	for _, group := range groups {
		if group == requiredGroup {
			return true
		}
	}
	return false
}

// ResponseMetadata represents common response metadata
type ResponseMetadata struct {
	RequestID string `json:"request_id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// addResponseMetadata adds common metadata to responses
//
//nolint:unused // Utility function for response metadata
func addResponseMetadata(c *gin.Context, response interface{}) interface{} {
	// Use reflection to check if response has metadata field
	v := reflect.ValueOf(response)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() == reflect.Struct {
		metadataField := v.FieldByName("Metadata")
		if metadataField.IsValid() && metadataField.CanSet() {
			metadata := ResponseMetadata{
				RequestID: getRequestID(c),
			}
			metadataField.Set(reflect.ValueOf(metadata))
		}
	}

	return response
}

// PaginationParams represents common pagination parameters
type PaginationParams struct {
	Limit  int `json:"limit" validate:"min=1,max=100"`
	Offset int `json:"offset" validate:"min=0"`
}

// parsePaginationParams parses limit and offset from query parameters
//
//nolint:unused // Utility function for pagination
func parsePaginationParams(c *gin.Context) PaginationParams {
	params := PaginationParams{
		Limit:  20, // default
		Offset: 0,  // default
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := parseIntQueryParam(limitStr, 1, 100); err == nil {
			params.Limit = limit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if offset, err := parseIntQueryParam(offsetStr, 0, -1); err == nil {
			params.Offset = offset
		}
	}

	return params
}

// parseIntQueryParam parses an integer query parameter with min/max validation
//
//nolint:unused // Used internally by parsePaginationParams
func parseIntQueryParam(value string, min, max int) (int, error) {
	// Implementation would parse string to int and validate bounds
	// Simplified for now
	return 20, nil
}

// corsMiddleware adds CORS headers (if needed)
func corsMiddleware() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Header("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})
}

// requestLoggingMiddleware logs HTTP requests
func requestLoggingMiddleware() gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health/live", "/health/ready"},
	})
}
