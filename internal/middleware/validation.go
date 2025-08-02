package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/validation"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// ValidationMiddleware creates a middleware that validates and sanitizes request bodies
func ValidationMiddleware(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only process JSON requests
		if !strings.Contains(c.GetHeader("Content-Type"), "application/json") {
			c.Next()
			return
		}

		// Read the request body
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			log.Error("Failed to read request body", zap.Error(err))
			c.JSON(http.StatusBadRequest, errors.BadRequest("Failed to read request body"))
			c.Abort()
			return
		}

		// Restore the request body for downstream handlers
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		// Skip empty bodies
		if len(body) == 0 {
			c.Next()
			return
		}

		// Parse JSON to detect the structure
		var jsonData interface{}
		if err := json.Unmarshal(body, &jsonData); err != nil {
			log.Error("Invalid JSON in request body", zap.Error(err))
			c.JSON(http.StatusBadRequest, errors.BadRequest("Invalid JSON format"))
			c.Abort()
			return
		}

		// Sanitize the JSON data
		sanitizedData := sanitizeJSONValue(jsonData)

		// Re-encode the sanitized data
		sanitizedBody, err := json.Marshal(sanitizedData)
		if err != nil {
			log.Error("Failed to marshal sanitized data", zap.Error(err))
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to process request"))
			c.Abort()
			return
		}

		// Replace the request body with sanitized version
		c.Request.Body = io.NopCloser(bytes.NewBuffer(sanitizedBody))
		c.Request.ContentLength = int64(len(sanitizedBody))

		c.Next()
	}
}

// sanitizeJSONValue recursively sanitizes JSON values
func sanitizeJSONValue(value interface{}) interface{} {
	switch v := value.(type) {
	case string:
		return sanitizeStringValue(v)
	case map[string]interface{}:
		sanitized := make(map[string]interface{})
		for key, val := range v {
			sanitizedKey := validation.SanitizeString(key, validation.StrictSanitizationOptions())
			sanitized[sanitizedKey] = sanitizeJSONValue(val)
		}
		return sanitized
	case []interface{}:
		sanitized := make([]interface{}, len(v))
		for i, val := range v {
			sanitized[i] = sanitizeJSONValue(val)
		}
		return sanitized
	default:
		return v
	}
}

// sanitizeStringValue sanitizes string values based on field context
func sanitizeStringValue(value string) string {
	// Basic sanitization for all strings
	return validation.SanitizeString(value, validation.DefaultSanitizationOptions())
}

// StructValidationMiddleware creates middleware for validating specific struct types
func StructValidationMiddleware[T any](log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only validate POST, PUT, PATCH requests
		if c.Request.Method != "POST" && c.Request.Method != "PUT" && c.Request.Method != "PATCH" {
			c.Next()
			return
		}

		// Only process JSON requests
		if !strings.Contains(c.GetHeader("Content-Type"), "application/json") {
			c.Next()
			return
		}

		var req T
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Error("Failed to bind JSON", zap.Error(err))
			c.JSON(http.StatusBadRequest, errors.BadRequest("Invalid request format"))
			c.Abort()
			return
		}

		// Sanitize the struct
		sanitizedReq := sanitizeStruct(req)

		// Validate the sanitized struct
		if err := validation.Validate(sanitizedReq); err != nil {
			if validationErrors, ok := err.(validation.ValidationErrors); ok {
				c.JSON(http.StatusBadRequest, errors.NewValidationError("Validation failed", convertValidationErrors(validationErrors)))
				c.Abort()
				return
			}

			log.Error("Validation error", zap.Error(err))
			c.JSON(http.StatusBadRequest, errors.BadRequest("Validation failed"))
			c.Abort()
			return
		}

		// Store the sanitized and validated request in context
		c.Set("validated_request", sanitizedReq)
		c.Next()
	}
}

// sanitizeStruct sanitizes all string fields in a struct
func sanitizeStruct(s interface{}) interface{} {
	v := reflect.ValueOf(s)
	t := reflect.TypeOf(s)

	// Handle pointers
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return s
		}
		v = v.Elem()
		t = t.Elem()
	}

	// Only process structs
	if v.Kind() != reflect.Struct {
		return s
	}

	// Create a new instance of the same type
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
			sanitizedValue := sanitizeFieldBasedOnTag(field.String(), fieldType)
			newField.SetString(sanitizedValue)
		case reflect.Ptr:
			if !field.IsNil() {
				if field.Type().Elem().Kind() == reflect.String {
					original := field.Elem().String()
					sanitized := sanitizeFieldBasedOnTag(original, fieldType)
					newStr := reflect.New(field.Type().Elem())
					newStr.Elem().SetString(sanitized)
					newField.Set(newStr)
				} else {
					newField.Set(field)
				}
			}
		case reflect.Slice:
			if field.Type().Elem().Kind() == reflect.String {
				newSlice := reflect.MakeSlice(field.Type(), field.Len(), field.Cap())
				for j := 0; j < field.Len(); j++ {
					original := field.Index(j).String()
					sanitized := sanitizeFieldBasedOnTag(original, fieldType)
					newSlice.Index(j).SetString(sanitized)
				}
				newField.Set(newSlice)
			} else {
				newField.Set(field)
			}
		case reflect.Struct:
			sanitizedStruct := sanitizeStruct(field.Interface())
			newField.Set(reflect.ValueOf(sanitizedStruct))
		default:
			newField.Set(field)
		}
	}

	return newStruct.Interface()
}

// sanitizeFieldBasedOnTag sanitizes a field based on its struct tags
func sanitizeFieldBasedOnTag(value string, field reflect.StructField) string {
	jsonTag := field.Tag.Get("json")
	validateTag := field.Tag.Get("validate")

	// Determine sanitization strategy based on field name and tags
	fieldName := strings.ToLower(field.Name)
	if jsonTag != "" {
		fieldName = strings.ToLower(strings.Split(jsonTag, ",")[0])
	}

	switch {
	case strings.Contains(fieldName, "email"):
		return validation.SanitizeEmail(value)
	case strings.Contains(fieldName, "username"):
		return validation.SanitizeUsername(value)
	case strings.Contains(fieldName, "filename") || strings.Contains(fieldName, "name"):
		if strings.Contains(validateTag, "filename") {
			return validation.SanitizeFilename(value)
		}
		return validation.SanitizeTitle(value)
	case strings.Contains(fieldName, "tag"):
		return validation.SanitizeTag(value)
	case strings.Contains(fieldName, "description") || strings.Contains(fieldName, "content"):
		return validation.SanitizeDescription(value)
	case strings.Contains(fieldName, "url"):
		return validation.SanitizeURL(value)
	case strings.Contains(validateTag, "safe_string"):
		return validation.SanitizeString(value, validation.StrictSanitizationOptions())
	default:
		return validation.SanitizeString(value, validation.DefaultSanitizationOptions())
	}
}

// convertValidationErrors converts validation errors to API error format
func convertValidationErrors(validationErrors validation.ValidationErrors) []errors.ValidationError {
	var apiErrors []errors.ValidationError

	for _, err := range validationErrors {
		apiErrors = append(apiErrors, errors.ValidationError{
			Field:   err.Field,
			Message: err.Message,
			Value:   err.Value,
		})
	}

	return apiErrors
}

// RequestSizeLimit creates middleware to limit request body size
func RequestSizeLimit(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxSize {
			c.JSON(http.StatusRequestEntityTooLarge, errors.BadRequest("Request body too large"))
			c.Abort()
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		c.Next()
	}
}

// SecurityHeaders adds security headers to responses
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Prevent MIME type sniffing
		c.Header("X-Content-Type-Options", "nosniff")

		// Prevent clickjacking
		c.Header("X-Frame-Options", "DENY")

		// Enable XSS protection
		c.Header("X-XSS-Protection", "1; mode=block")

		// Enforce HTTPS (if in production)
		if gin.Mode() == gin.ReleaseMode {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// Prevent caching of sensitive data
		if strings.Contains(c.Request.URL.Path, "/api/") {
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}

		c.Next()
	}
}
