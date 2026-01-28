package middleware

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/validation"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// threatDetectionState holds the state for threat detection during a request
type threatDetectionState struct {
	threats []validation.DetectedThreat
	mu      sync.Mutex
}

// ValidationMiddleware creates a middleware that validates and sanitizes request bodies
func ValidationMiddleware(log *logger.Logger) gin.HandlerFunc {
	// Endpoints that need special base64 content handling
	base64UploadPaths := []string{
		"/api/v1/documents/upload-base64",
	}

	// Endpoints that use multipart form (skip JSON processing but could add form field scanning)
	// NOTE: Must be checked with exact match or suffix to avoid matching upload-base64
	multipartUploadPaths := []string{
		"/api/v1/documents/upload",
	}

	// isExactMultipartPath checks if a path exactly matches a multipart upload path
	// (not just starts with it, to avoid matching upload-base64)
	isExactMultipartPath := func(path string) bool {
		for _, mp := range multipartUploadPaths {
			// Exact match or path ends right after (no additional segments like -base64)
			if path == mp || (strings.HasPrefix(path, mp) && (len(path) == len(mp) || path[len(mp)] == '/')) {
				return true
			}
		}
		return false
	}

	// Create threat detector
	threatDetector := validation.NewThreatDetector()

	return func(c *gin.Context) {
		// Debug logging for upload requests
		if strings.Contains(c.Request.URL.Path, "upload") {
			log.Info("ValidationMiddleware processing upload request",
				zap.String("path", c.Request.URL.Path),
				zap.String("method", c.Request.Method),
				zap.String("content_type", c.GetHeader("Content-Type")),
			)
		}

		// Skip multipart form uploads (file content is binary, would need different handling)
		if isExactMultipartPath(c.Request.URL.Path) {
			log.Info("Skipping multipart upload path (exact match)", zap.String("path", c.Request.URL.Path))
			c.Next()
			return
		}

		// Check if this is a base64 upload endpoint (needs special content decoding)
		isBase64Upload := false
		for _, path := range base64UploadPaths {
			if strings.HasPrefix(c.Request.URL.Path, path) {
				isBase64Upload = true
				log.Info("Detected base64 upload endpoint", zap.String("path", c.Request.URL.Path))
				break
			}
		}

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

		// Get request context for logging
		requestID := GetRequestID(c)
		requestPath := c.Request.URL.Path
		requestMethod := c.Request.Method
		clientIP := c.ClientIP()
		userAgent := c.GetHeader("User-Agent")

		// Collect all detected threats
		var allThreats []validation.DetectedThreat

		// For base64 uploads, also scan the decoded content if it's text
		if isBase64Upload {
			log.Info("Processing base64 upload - checking for text content")
			if jsonMap, ok := jsonData.(map[string]interface{}); ok {
				// Check mime type to determine if content is text
				if mimeType, ok := jsonMap["mime_type"].(string); ok {
					log.Info("Base64 upload mime type", zap.String("mime_type", mimeType))
					isTextContent := strings.HasPrefix(mimeType, "text/") ||
						mimeType == "application/json" ||
						mimeType == "application/xml" ||
						mimeType == "application/javascript"

					if isTextContent {
						log.Info("Base64 upload is text content - will scan for threats")
						// Decode and scan the base64 content
						if fileContent, ok := jsonMap["file_content"].(string); ok && fileContent != "" {
							log.Info("Decoding base64 content", zap.Int("base64_length", len(fileContent)))
							decodedBytes, err := base64.StdEncoding.DecodeString(fileContent)
							if err == nil {
								decodedContent := string(decodedBytes)
								log.Info("Decoded content preview",
									zap.Int("decoded_length", len(decodedContent)),
									zap.String("content_preview", truncateForLog(decodedContent, 200)),
								)
								// Detect threats in the decoded file content
								contentThreats := threatDetector.DetectThreats(decodedContent, "file_content_decoded")
								allThreats = append(allThreats, contentThreats...)

								if len(contentThreats) > 0 {
									log.Warn("Threats detected in decoded base64 content",
										zap.String("mime_type", mimeType),
										zap.Int("threat_count", len(contentThreats)),
									)
									for _, threat := range contentThreats {
										log.Warn("Base64 content threat detail",
											zap.String("type", threat.Type),
											zap.String("severity", threat.Severity),
											zap.String("pattern", threat.Pattern),
											zap.String("matched", threat.MatchedContent),
										)
									}
								} else {
									log.Info("No threats detected in decoded base64 content")
								}
							} else {
								log.Error("Failed to decode base64 content", zap.Error(err))
							}
						} else {
							log.Info("No file_content field found in request")
						}
					} else {
						log.Info("Base64 upload is not text content - skipping threat scan",
							zap.String("mime_type", mimeType))
					}
				} else {
					log.Info("No mime_type found in base64 upload request")
				}
			}
		}

		// Detect threats and sanitize the JSON data (metadata fields)
		sanitizedData := sanitizeJSONValueWithThreatDetection(jsonData, "", threatDetector, &allThreats)

		// Check if we should reject the request (critical severity)
		highestSeverity := validation.GetHighestSeverity(allThreats)

		if len(allThreats) > 0 {
			// Collect unique threat types for logging/response
			threatTypeSet := make(map[string]bool)
			for _, threat := range allThreats {
				threatTypeSet[threat.Type] = true
			}
			threatTypes := make([]string, 0, len(threatTypeSet))
			for threatType := range threatTypeSet {
				threatTypes = append(threatTypes, threatType)
			}

			if highestSeverity == "critical" {
				// ERROR: Content blocked - request rejected
				log.Error("Security threat BLOCKED - request rejected",
					zap.String("event_type", "security_blocked"),
					zap.String("severity", highestSeverity),
					zap.String("request_id", requestID),
					zap.String("request_path", requestPath),
					zap.String("request_method", requestMethod),
					zap.String("client_ip", clientIP),
					zap.String("user_agent", userAgent),
					zap.Int("threat_count", len(allThreats)),
					zap.Strings("threat_types", threatTypes),
					zap.String("action", "rejected"),
				)

				// Log each individual threat at error level for detailed analysis
				for _, threat := range allThreats {
					log.Error("Security threat detail",
						zap.String("event_type", threat.Type),
						zap.String("severity", threat.Severity),
						zap.String("request_id", requestID),
						zap.String("field_name", threat.FieldName),
						zap.String("threat_pattern", threat.Pattern),
						zap.String("matched_content", threat.MatchedContent),
					)
				}

				c.JSON(http.StatusForbidden, errors.SecurityBlocked(
					"Your input contains potentially unsafe content that has been blocked",
					threatTypes,
					highestSeverity,
				))
				c.Abort()
				return
			}

			// WARNING: Threats detected but content allowed (sanitized)
			log.Warn("Security threat SANITIZED - request allowed",
				zap.String("event_type", "security_sanitized"),
				zap.String("severity", highestSeverity),
				zap.String("request_id", requestID),
				zap.String("request_path", requestPath),
				zap.String("request_method", requestMethod),
				zap.String("client_ip", clientIP),
				zap.String("user_agent", userAgent),
				zap.Int("threat_count", len(allThreats)),
				zap.Strings("threat_types", threatTypes),
				zap.String("action", "sanitized"),
			)

			// Log each individual threat at warn level for detailed analysis
			for _, threat := range allThreats {
				log.Warn("Security threat detail",
					zap.String("event_type", threat.Type),
					zap.String("severity", threat.Severity),
					zap.String("request_id", requestID),
					zap.String("field_name", threat.FieldName),
					zap.String("threat_pattern", threat.Pattern),
					zap.String("matched_content", threat.MatchedContent),
				)
			}

			// Store detected threats in context for downstream handlers
			c.Set("detected_threats", allThreats)
			c.Set("highest_threat_severity", highestSeverity)
		} else {
			// INFO: No threats detected - security check passed
			log.Info("Security check PASSED - no threats detected",
				zap.String("event_type", "security_passed"),
				zap.String("request_id", requestID),
				zap.String("request_path", requestPath),
				zap.String("request_method", requestMethod),
				zap.String("client_ip", clientIP),
			)
		}

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
	return sanitizeJSONValueWithKey(value, "")
}

// sanitizeJSONValueWithKey recursively sanitizes JSON values, tracking the field key
func sanitizeJSONValueWithKey(value interface{}, key string) interface{} {
	switch v := value.(type) {
	case string:
		return sanitizeStringValueForKey(v, key)
	case map[string]interface{}:
		sanitized := make(map[string]interface{})
		for k, val := range v {
			sanitizedKey := validation.SanitizeString(k, validation.StrictSanitizationOptions())
			sanitized[sanitizedKey] = sanitizeJSONValueWithKey(val, k)
		}
		return sanitized
	case []interface{}:
		sanitized := make([]interface{}, len(v))
		for i, val := range v {
			sanitized[i] = sanitizeJSONValueWithKey(val, key)
		}
		return sanitized
	default:
		return v
	}
}

// sanitizeJSONValueWithThreatDetection recursively sanitizes JSON values while detecting threats
func sanitizeJSONValueWithThreatDetection(value interface{}, key string, detector *validation.ThreatDetector, threats *[]validation.DetectedThreat) interface{} {
	switch v := value.(type) {
	case string:
		// Skip threat detection and sanitization for base64 encoded file content
		// (we decode and scan it separately, and sanitizing would corrupt the base64)
		if key == "file_content" {
			return v
		}

		// Detect threats before sanitizing
		if detector != nil && threats != nil {
			fieldName := key
			if fieldName == "" {
				fieldName = "root"
			}
			detectedThreats := detector.DetectThreats(v, fieldName)
			*threats = append(*threats, detectedThreats...)
		}
		return sanitizeStringValueForKey(v, key)
	case map[string]interface{}:
		sanitized := make(map[string]interface{})
		for k, val := range v {
			// Detect threats in keys as well
			if detector != nil && threats != nil {
				keyThreats := detector.DetectThreats(k, "object_key")
				*threats = append(*threats, keyThreats...)
			}
			sanitizedKey := validation.SanitizeString(k, validation.StrictSanitizationOptions())
			sanitized[sanitizedKey] = sanitizeJSONValueWithThreatDetection(val, k, detector, threats)
		}
		return sanitized
	case []interface{}:
		sanitized := make([]interface{}, len(v))
		for i, val := range v {
			sanitized[i] = sanitizeJSONValueWithThreatDetection(val, key, detector, threats)
		}
		return sanitized
	default:
		return v
	}
}

// Fields that can contain large content (e.g., document body, markdown from web scraping)
var largeContentFields = map[string]bool{
	"content":  true,
	"markdown": true,
	"html":     true,
	"body":     true,
	"text":     true,
}

// sanitizeStringValueForKey sanitizes string values based on field context
func sanitizeStringValueForKey(value string, key string) string {
	// For large content fields, use permissive options with very high max length
	// Still applies XSS, SQL injection, and HTML stripping for security
	if largeContentFields[key] {
		options := validation.SanitizationOptions{
			StripHTML:          false, // Keep HTML - it's markdown content
			StripSQLInjection:  true,  // Still protect against SQL injection
			StripXSS:           true,  // Still protect against XSS
			StripControlChars:  true,  // Remove control chars
			TrimWhitespace:     false, // Preserve whitespace in content
			CollapseWhitespace: false, // Preserve formatting
			MaxLength:          10 * 1024 * 1024, // 10MB max for content
			PreserveCase:       true,
		}
		return validation.SanitizeString(value, options)
	}

	// Default sanitization for other fields
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

// truncateForLog truncates a string for logging purposes
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RequestSizeLimit creates middleware to limit request body size
func RequestSizeLimit(maxSize int64) gin.HandlerFunc {
	log, _ := logger.New(logger.Config{Level: "debug"})
	return func(c *gin.Context) {
		log.Info("=== REQUEST SIZE LIMIT MIDDLEWARE ===", 
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int64("content_length", c.Request.ContentLength),
			zap.Int64("max_size", maxSize))
			
		if c.Request.ContentLength > maxSize {
			log.Error("Request body too large", 
				zap.Int64("content_length", c.Request.ContentLength),
				zap.Int64("max_size", maxSize))
			c.JSON(http.StatusRequestEntityTooLarge, errors.BadRequest("Request body too large"))
			c.Abort()
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		log.Info("Request size check passed, proceeding to next middleware")
		c.Next()
		log.Info("Request size middleware completed")
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
