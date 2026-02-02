package errors

import (
	"fmt"
	"net/http"
)

// Error codes
const (
	// Client errors (4xx)
	ErrBadRequest          = "BAD_REQUEST"
	ErrUnauthorized        = "UNAUTHORIZED"
	ErrForbidden           = "FORBIDDEN"
	ErrNotFound            = "NOT_FOUND"
	ErrMethodNotAllowed    = "METHOD_NOT_ALLOWED"
	ErrConflict            = "CONFLICT"
	ErrUnprocessableEntity = "UNPROCESSABLE_ENTITY"
	ErrTooManyRequests     = "TOO_MANY_REQUESTS"

	// Server errors (5xx)
	ErrInternal           = "INTERNAL_SERVER_ERROR"
	ErrBadGateway         = "BAD_GATEWAY"
	ErrServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrGatewayTimeout     = "GATEWAY_TIMEOUT"

	// Business logic errors
	ErrValidation       = "VALIDATION_ERROR"
	ErrAuthentication   = "AUTHENTICATION_ERROR"
	ErrAuthorization    = "AUTHORIZATION_ERROR"
	ErrResourceExists   = "RESOURCE_EXISTS"
	ErrResourceNotFound = "RESOURCE_NOT_FOUND"
	ErrDatabaseError    = "DATABASE_ERROR"
	ErrExternalService  = "EXTERNAL_SERVICE_ERROR"

	// Security errors
	ErrSecurityBlocked = "SECURITY_BLOCKED"

	// Chunk processing errors
	ErrChunkNotFound        = "CHUNK_NOT_FOUND"
	ErrChunkProcessing      = "CHUNK_PROCESSING_ERROR"
	ErrChunkEmbedding       = "CHUNK_EMBEDDING_ERROR"
	ErrChunkValidation      = "CHUNK_VALIDATION_ERROR"
	ErrStrategy             = "STRATEGY_ERROR"
	ErrStrategyNotFound     = "STRATEGY_NOT_FOUND"
	ErrStrategyValidation   = "STRATEGY_VALIDATION_ERROR"
	ErrFileNotProcessed     = "FILE_NOT_PROCESSED"
	ErrProcessingInProgress = "PROCESSING_IN_PROGRESS"
)

// APIError represents a structured API error
type APIError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
	StatusCode int                    `json:"-"`
	Cause      error                  `json:"-"`
}

// Error implements the error interface
func (e *APIError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause
func (e *APIError) Unwrap() error {
	return e.Cause
}

// NewAPIError creates a new API error
func NewAPIError(code, message string, details map[string]interface{}) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		StatusCode: GetHTTPStatusCodeFromErrorCode(code),
	}
}

// NewAPIErrorWithCause creates a new API error with a cause
func NewAPIErrorWithCause(code, message string, cause error, details map[string]interface{}) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		Details:    details,
		Cause:      cause,
		StatusCode: GetHTTPStatusCodeFromErrorCode(code),
	}
}

// GetHTTPStatusCode returns the appropriate HTTP status code for an error
func GetHTTPStatusCode(err error) int {
	if apiErr, ok := err.(*APIError); ok {
		return GetHTTPStatusCodeFromErrorCode(apiErr.Code)
	}
	return http.StatusInternalServerError
}

// GetHTTPStatusCodeFromErrorCode maps error codes to HTTP status codes
func GetHTTPStatusCodeFromErrorCode(code string) int {
	switch code {
	case ErrBadRequest, ErrValidation, ErrChunkValidation, ErrStrategyValidation:
		return http.StatusBadRequest
	case ErrUnauthorized, ErrAuthentication:
		return http.StatusUnauthorized
	case ErrForbidden, ErrAuthorization, ErrSecurityBlocked:
		return http.StatusForbidden
	case ErrNotFound, ErrResourceNotFound, ErrChunkNotFound, ErrStrategyNotFound, ErrFileNotProcessed:
		return http.StatusNotFound
	case ErrMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case ErrConflict, ErrResourceExists, ErrProcessingInProgress:
		return http.StatusConflict
	case ErrUnprocessableEntity, ErrChunkProcessing, ErrChunkEmbedding, ErrStrategy:
		return http.StatusUnprocessableEntity
	case ErrTooManyRequests:
		return http.StatusTooManyRequests
	case ErrBadGateway, ErrExternalService:
		return http.StatusBadGateway
	case ErrServiceUnavailable:
		return http.StatusServiceUnavailable
	case ErrGatewayTimeout:
		return http.StatusGatewayTimeout
	case ErrInternal, ErrDatabaseError:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// ValidationError represents validation errors
type ValidationError struct {
	Field   string      `json:"field"`
	Message string      `json:"message"`
	Value   interface{} `json:"value,omitempty"`
}

// ForbiddenWithDetails creates a forbidden error with details
func ForbiddenWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrForbidden, message, details)
}

// DatabaseWithDetails creates a database error with details
func DatabaseWithDetails(message string, cause error, details map[string]interface{}) *APIError {
	return NewAPIErrorWithCause(ErrDatabaseError, message, cause, details)
}

// ValidationWithDetails creates a validation error with details
func ValidationWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrValidation, message, details)
}

// NewValidationError creates a validation error with field details
func NewValidationError(message string, validationErrors []ValidationError) *APIError {
	details := map[string]interface{}{
		"validation_errors": validationErrors,
	}
	return NewAPIError(ErrValidation, message, details)
}

// Predefined error constructors for common cases

// BadRequest creates a bad request error
func BadRequest(message string) *APIError {
	return NewAPIError(ErrBadRequest, message, nil)
}

// BadRequestWithDetails creates a bad request error with details
func BadRequestWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrBadRequest, message, details)
}

// Unauthorized creates an unauthorized error
func Unauthorized(message string) *APIError {
	return NewAPIError(ErrUnauthorized, message, nil)
}

// Forbidden creates a forbidden error
func Forbidden(message string) *APIError {
	return NewAPIError(ErrForbidden, message, nil)
}

// SecurityBlocked creates a security blocked error for threat detection
func SecurityBlocked(message string, threatTypes []string, severity string) *APIError {
	details := map[string]interface{}{
		"threat_types": threatTypes,
		"severity":     severity,
	}
	return NewAPIError(ErrSecurityBlocked, message, details)
}

// NotFound creates a not found error
func NotFound(message string) *APIError {
	return NewAPIError(ErrNotFound, message, nil)
}

// NotFoundWithDetails creates a not found error with details
func NotFoundWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrNotFound, message, details)
}

// Conflict creates a conflict error
func Conflict(message string) *APIError {
	return NewAPIError(ErrConflict, message, nil)
}

// ConflictWithDetails creates a conflict error with details
func ConflictWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrConflict, message, details)
}

// Internal creates an internal server error
func Internal(message string) *APIError {
	return NewAPIError(ErrInternal, message, nil)
}

// InternalWithCause creates an internal server error with cause
func InternalWithCause(message string, cause error) *APIError {
	return NewAPIErrorWithCause(ErrInternal, message, cause, nil)
}

// Database creates a database error
func Database(message string, cause error) *APIError {
	return NewAPIErrorWithCause(ErrDatabaseError, message, cause, nil)
}

// ExternalService creates an external service error
func ExternalService(message string, cause error) *APIError {
	return NewAPIErrorWithCause(ErrExternalService, message, cause, nil)
}

// ServiceUnavailable creates a service unavailable error
func ServiceUnavailable(message string) *APIError {
	return NewAPIError(ErrServiceUnavailable, message, nil)
}

// TooManyRequests creates a too many requests error
func TooManyRequests(message string) *APIError {
	return NewAPIError(ErrTooManyRequests, message, nil)
}

// Validation creates a validation error
func Validation(message string, cause error) *APIError {
	return NewAPIErrorWithCause(ErrValidation, message, cause, nil)
}

// IsAPIError checks if an error is an APIError
func IsAPIError(err error) bool {
	_, ok := err.(*APIError)
	return ok
}

// AsAPIError converts an error to APIError if possible
func AsAPIError(err error) (*APIError, bool) {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr, true
	}
	return nil, false
}

// Error type checking functions
func IsNotFound(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrNotFound || apiErr.Code == ErrResourceNotFound
	}
	return false
}

func IsForbidden(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrForbidden || apiErr.Code == ErrAuthorization
	}
	return false
}

func IsUnauthorized(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrUnauthorized || apiErr.Code == ErrAuthentication
	}
	return false
}

func IsValidation(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrValidation || apiErr.Code == ErrBadRequest
	}
	return false
}

func IsConflict(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrConflict || apiErr.Code == ErrResourceExists
	}
	return false
}

func IsDatabase(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrDatabaseError
	}
	return false
}

func IsExternalService(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrExternalService || apiErr.Code == ErrBadGateway
	}
	return false
}

// Chunk-specific error constructors

// ChunkNotFound creates a chunk not found error
func ChunkNotFound(message string) *APIError {
	return NewAPIError(ErrChunkNotFound, message, nil)
}

// ChunkNotFoundWithDetails creates a chunk not found error with details
func ChunkNotFoundWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrChunkNotFound, message, details)
}

// ChunkProcessing creates a chunk processing error
func ChunkProcessing(message string, cause error) *APIError {
	return NewAPIErrorWithCause(ErrChunkProcessing, message, cause, nil)
}

// ChunkProcessingWithDetails creates a chunk processing error with details
func ChunkProcessingWithDetails(message string, cause error, details map[string]interface{}) *APIError {
	return NewAPIErrorWithCause(ErrChunkProcessing, message, cause, details)
}

// ChunkEmbedding creates a chunk embedding error
func ChunkEmbedding(message string, cause error) *APIError {
	return NewAPIErrorWithCause(ErrChunkEmbedding, message, cause, nil)
}

// ChunkValidation creates a chunk validation error
func ChunkValidation(message string) *APIError {
	return NewAPIError(ErrChunkValidation, message, nil)
}

// ChunkValidationWithDetails creates a chunk validation error with details
func ChunkValidationWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrChunkValidation, message, details)
}

// StrategyNotFound creates a strategy not found error
func StrategyNotFound(message string) *APIError {
	return NewAPIError(ErrStrategyNotFound, message, nil)
}

// StrategyNotFoundWithDetails creates a strategy not found error with details
func StrategyNotFoundWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrStrategyNotFound, message, details)
}

// StrategyError creates a strategy error
func StrategyError(message string, cause error) *APIError {
	return NewAPIErrorWithCause(ErrStrategy, message, cause, nil)
}

// StrategyValidation creates a strategy validation error
func StrategyValidation(message string) *APIError {
	return NewAPIError(ErrStrategyValidation, message, nil)
}

// FileNotProcessed creates a file not processed error
func FileNotProcessed(message string) *APIError {
	return NewAPIError(ErrFileNotProcessed, message, nil)
}

// FileNotProcessedWithDetails creates a file not processed error with details
func FileNotProcessedWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrFileNotProcessed, message, details)
}

// ProcessingInProgress creates a processing in progress error
func ProcessingInProgress(message string) *APIError {
	return NewAPIError(ErrProcessingInProgress, message, nil)
}

// ProcessingInProgressWithDetails creates a processing in progress error with details
func ProcessingInProgressWithDetails(message string, details map[string]interface{}) *APIError {
	return NewAPIError(ErrProcessingInProgress, message, details)
}

// Chunk error type checking functions

func IsChunkNotFound(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrChunkNotFound
	}
	return false
}

func IsChunkProcessing(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrChunkProcessing
	}
	return false
}

func IsChunkEmbedding(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrChunkEmbedding
	}
	return false
}

func IsChunkValidation(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrChunkValidation
	}
	return false
}

func IsStrategyError(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrStrategy || apiErr.Code == ErrStrategyNotFound || apiErr.Code == ErrStrategyValidation
	}
	return false
}

func IsProcessingInProgress(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code == ErrProcessingInProgress
	}
	return false
}
