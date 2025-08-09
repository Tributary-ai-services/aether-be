package validation

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

var (
	validate *validator.Validate

	// Common regex patterns for validation
	slugPattern     = regexp.MustCompile(`^[a-z0-9\-]+$`)
	usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)
	filenamePattern = regexp.MustCompile(`^[a-zA-Z0-9_\-\.\s\(\)]+$`)
	hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

func init() {
	validate = validator.New()

	// Register custom validations
	if err := validate.RegisterValidation("slug", validateSlug); err != nil {
		panic(fmt.Sprintf("Failed to register slug validation: %v", err))
	}
	if err := validate.RegisterValidation("username", validateUsername); err != nil {
		panic(fmt.Sprintf("Failed to register username validation: %v", err))
	}
	if err := validate.RegisterValidation("filename", validateFilename); err != nil {
		panic(fmt.Sprintf("Failed to register filename validation: %v", err))
	}
	if err := validate.RegisterValidation("hexcolor", validateHexColor); err != nil {
		panic(fmt.Sprintf("Failed to register hexcolor validation: %v", err))
	}
	if err := validate.RegisterValidation("no_html", validateNoHTML); err != nil {
		panic(fmt.Sprintf("Failed to register no_html validation: %v", err))
	}
	if err := validate.RegisterValidation("no_sql", validateNoSQL); err != nil {
		panic(fmt.Sprintf("Failed to register no_sql validation: %v", err))
	}
	if err := validate.RegisterValidation("safe_string", validateSafeString); err != nil {
		panic(fmt.Sprintf("Failed to register safe_string validation: %v", err))
	}
	if err := validate.RegisterValidation("tag", validateTag); err != nil {
		panic(fmt.Sprintf("Failed to register tag validation: %v", err))
	}
	if err := validate.RegisterValidation("notebook_visibility", validateNotebookVisibility); err != nil {
		panic(fmt.Sprintf("Failed to register notebook_visibility validation: %v", err))
	}
	if err := validate.RegisterValidation("user_status", validateUserStatus); err != nil {
		panic(fmt.Sprintf("Failed to register user_status validation: %v", err))
	}
	if err := validate.RegisterValidation("document_type", validateDocumentType); err != nil {
		panic(fmt.Sprintf("Failed to register document_type validation: %v", err))
	}
	if err := validate.RegisterValidation("neo4j_compatible", validateNeo4jCompatible); err != nil {
		panic(fmt.Sprintf("Failed to register neo4j_compatible validation: %v", err))
	}

	// Register function to get struct field names for better error messages
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		return fld.Name
	})
}

// ValidationError represents a field validation error with enhanced details
type ValidationError struct {
	Field     string      `json:"field"`
	Value     interface{} `json:"value,omitempty"`
	Tag       string      `json:"tag"`
	Message   string      `json:"message"`
	Namespace string      `json:"namespace,omitempty"`
}

// ValidationErrors represents multiple validation errors
type ValidationErrors []ValidationError

func (v ValidationErrors) Error() string {
	var messages []string
	for _, err := range v {
		messages = append(messages, err.Message)
	}
	return strings.Join(messages, "; ")
}

// Validate validates a struct and returns detailed validation errors
func Validate(s interface{}) error {
	err := validate.Struct(s)
	if err == nil {
		return nil
	}

	var validationErrors ValidationErrors

	if validatorErrors, ok := err.(validator.ValidationErrors); ok {
		for _, fieldError := range validatorErrors {
			validationError := ValidationError{
				Field:     fieldError.Field(),
				Value:     fieldError.Value(),
				Tag:       fieldError.Tag(),
				Namespace: fieldError.Namespace(),
				Message:   getValidationMessage(fieldError),
			}
			validationErrors = append(validationErrors, validationError)
		}
	}

	return validationErrors
}

// ValidateVar validates a single variable
func ValidateVar(field interface{}, tag string) error {
	return validate.Var(field, tag)
}

// Custom validation functions

func validateSlug(fl validator.FieldLevel) bool {
	return slugPattern.MatchString(fl.Field().String())
}

func validateUsername(fl validator.FieldLevel) bool {
	username := fl.Field().String()
	if len(username) < 3 || len(username) > 50 {
		return false
	}
	return usernamePattern.MatchString(username)
}

func validateFilename(fl validator.FieldLevel) bool {
	filename := fl.Field().String()
	if len(filename) == 0 || len(filename) > 255 {
		return false
	}
	// Check for whitespace-only filenames
	if strings.TrimSpace(filename) == "" {
		return false
	}
	// Check for dangerous characters
	dangerous := []string{"..", "/", "\\", "<", ">", ":", "\"", "|", "?", "*"}
	for _, char := range dangerous {
		if strings.Contains(filename, char) {
			return false
		}
	}
	return filenamePattern.MatchString(filename)
}

func validateHexColor(fl validator.FieldLevel) bool {
	return hexColorPattern.MatchString(fl.Field().String())
}

func validateNoHTML(fl validator.FieldLevel) bool {
	str := fl.Field().String()
	return !strings.Contains(str, "<") && !strings.Contains(str, ">")
}

func validateNoSQL(fl validator.FieldLevel) bool {
	str := strings.ToLower(fl.Field().String())
	sqlKeywords := []string{
		"select", "insert", "update", "delete", "drop", "create", "alter",
		"union", "script", "exec", "execute", "sp_", "xp_", "cmdshell",
	}

	for _, keyword := range sqlKeywords {
		if strings.Contains(str, keyword) {
			return false
		}
	}
	return true
}

func validateSafeString(fl validator.FieldLevel) bool {
	str := fl.Field().String()

	// Check for HTML
	if strings.Contains(str, "<") || strings.Contains(str, ">") {
		return false
	}

	// Check for SQL injection patterns (removed single quotes to allow them)
	dangerous := []string{
		"\"", ";", "--", "/*", "*/", "xp_", "sp_",
		"exec", "execute", "select", "insert", "update", "delete",
		"drop", "create", "alter", "union", "script",
	}

	lowerStr := strings.ToLower(str)
	for _, pattern := range dangerous {
		if strings.Contains(lowerStr, pattern) {
			return false
		}
	}

	return true
}

func validateTag(fl validator.FieldLevel) bool {
	tag := fl.Field().String()
	if len(tag) < 1 || len(tag) > 50 {
		return false
	}

	// Tags should be alphanumeric with hyphens and underscores
	return regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`).MatchString(tag)
}

func validateNotebookVisibility(fl validator.FieldLevel) bool {
	visibility := fl.Field().String()
	validValues := []string{"private", "public", "shared"}

	for _, valid := range validValues {
		if visibility == valid {
			return true
		}
	}
	return false
}

func validateUserStatus(fl validator.FieldLevel) bool {
	status := fl.Field().String()
	validValues := []string{"active", "inactive", "suspended", "pending"}

	for _, valid := range validValues {
		if status == valid {
			return true
		}
	}
	return false
}

func validateDocumentType(fl validator.FieldLevel) bool {
	docType := fl.Field().String()
	validTypes := []string{
		"pdf", "document", "spreadsheet", "presentation", "text",
		"csv", "json", "xml", "image", "video", "audio", "unknown",
	}

	for _, valid := range validTypes {
		if docType == valid {
			return true
		}
	}
	return false
}

func validateNeo4jCompatible(fl validator.FieldLevel) bool {
	field := fl.Field()
	return isNeo4jCompatibleValue(field)
}

// isNeo4jCompatibleValue checks if a value can be stored as a Neo4j property
func isNeo4jCompatibleValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true // nil values are allowed
	}

	switch v.Kind() {
	case reflect.String, reflect.Bool:
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	case reflect.Float32, reflect.Float64:
		return true
	case reflect.Slice, reflect.Array:
		// Arrays/slices are allowed if they contain primitive types
		for i := 0; i < v.Len(); i++ {
			if !isNeo4jCompatibleValue(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		// Maps are allowed if they contain only primitive values (depth 1 only)
		for _, key := range v.MapKeys() {
			if !isPrimitiveValue(key) {
				return false
			}
			mapValue := v.MapIndex(key)
			if !isPrimitiveValue(mapValue) && !isArrayOfPrimitives(mapValue) {
				return false
			}
		}
		return true
	case reflect.Interface:
		// Check the underlying value
		if v.IsNil() {
			return true
		}
		return isNeo4jCompatibleValue(v.Elem())
	default:
		// Complex types like structs, channels, functions are not allowed
		return false
	}
}

// isPrimitiveValue checks if a value is a primitive type (no nested maps/slices)
func isPrimitiveValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}

	switch v.Kind() {
	case reflect.String, reflect.Bool:
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	case reflect.Float32, reflect.Float64:
		return true
	case reflect.Interface:
		if v.IsNil() {
			return true
		}
		return isPrimitiveValue(v.Elem())
	default:
		return false
	}
}

// isArrayOfPrimitives checks if a value is an array/slice of primitive types
func isArrayOfPrimitives(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}

	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !isPrimitiveValue(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Interface:
		if v.IsNil() {
			return true
		}
		return isArrayOfPrimitives(v.Elem())
	default:
		return false
	}
}

// getValidationMessage returns a human-readable validation error message
func getValidationMessage(fe validator.FieldError) string {
	field := fe.Field()
	tag := fe.Tag()
	param := fe.Param()
	value := fe.Value()

	switch tag {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "email":
		return fmt.Sprintf("%s must be a valid email address", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s characters long", field, param)
	case "max":
		return fmt.Sprintf("%s must be at most %s characters long", field, param)
	case "len":
		return fmt.Sprintf("%s must be exactly %s characters long", field, param)
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, param)
	case "uuid":
		return fmt.Sprintf("%s must be a valid UUID", field)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "slug":
		return fmt.Sprintf("%s must be a valid slug (lowercase letters, numbers, and hyphens only)", field)
	case "username":
		return fmt.Sprintf("%s must be 3-50 characters and contain only letters, numbers, dots, hyphens, and underscores", field)
	case "filename":
		return fmt.Sprintf("%s contains invalid characters or is too long", field)
	case "hexcolor":
		return fmt.Sprintf("%s must be a valid hex color (e.g., #FF0000)", field)
	case "no_html":
		return fmt.Sprintf("%s cannot contain HTML tags", field)
	case "no_sql":
		return fmt.Sprintf("%s contains potentially dangerous SQL keywords", field)
	case "safe_string":
		return fmt.Sprintf("%s contains unsafe characters", field)
	case "tag":
		return fmt.Sprintf("%s must be 1-50 characters and contain only letters, numbers, hyphens, and underscores", field)
	case "notebook_visibility":
		return fmt.Sprintf("%s must be one of: private, public, shared", field)
	case "user_status":
		return fmt.Sprintf("%s must be one of: active, inactive, suspended, pending", field)
	case "document_type":
		return fmt.Sprintf("%s must be a valid document type", field)
	case "neo4j_compatible":
		return fmt.Sprintf("%s contains complex nested objects that cannot be stored in the database. Only primitive values and simple objects are allowed", field)
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", field, param)
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", field, param)
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", field, param)
	case "lt":
		return fmt.Sprintf("%s must be less than %s", field, param)
	default:
		return fmt.Sprintf("%s failed validation (tag: %s, value: %v)", field, tag, value)
	}
}
