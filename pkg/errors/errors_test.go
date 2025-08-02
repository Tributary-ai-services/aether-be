package errors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPIError(t *testing.T) {
	t.Run("create API error", func(t *testing.T) {
		err := &APIError{
			StatusCode: 400,
			Code:       "TEST_ERROR",
			Message:    "Test error",
			Details:    map[string]interface{}{"field": "value"},
		}

		assert.Equal(t, 400, err.StatusCode)
		assert.Equal(t, "TEST_ERROR", err.Code)
		assert.Equal(t, "Test error", err.Message)
		assert.Contains(t, err.Error(), "TEST_ERROR")
		assert.Contains(t, err.Error(), "Test error")
	})
}

func TestErrorCreators(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		err := NotFound("Resource not found")

		assert.Error(t, err)
		assert.True(t, IsNotFound(err))
		assert.Contains(t, err.Error(), "Resource not found")
	})

	t.Run("Validation", func(t *testing.T) {
		originalErr := assert.AnError
		err := Validation("Invalid input", originalErr)

		assert.Error(t, err)
		assert.True(t, IsValidation(err))
		assert.Contains(t, err.Error(), "Invalid input")
	})

	t.Run("Unauthorized", func(t *testing.T) {
		err := Unauthorized("Access denied")

		assert.Error(t, err)
		assert.True(t, IsUnauthorized(err))
		assert.Contains(t, err.Error(), "Access denied")
	})

	t.Run("Internal", func(t *testing.T) {
		err := Internal("Server error")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Server error")
	})
}

func TestErrorCheckers(t *testing.T) {
	t.Run("IsNotFound", func(t *testing.T) {
		notFoundErr := NotFound("Not found")
		otherErr := Internal("Internal error")

		assert.True(t, IsNotFound(notFoundErr))
		assert.False(t, IsNotFound(otherErr))
		assert.False(t, IsNotFound(assert.AnError))
	})

	t.Run("IsValidation", func(t *testing.T) {
		validationErr := Validation("Invalid", nil)
		otherErr := NotFound("Not found")

		assert.True(t, IsValidation(validationErr))
		assert.False(t, IsValidation(otherErr))
		assert.False(t, IsValidation(assert.AnError))
	})

	t.Run("IsUnauthorized", func(t *testing.T) {
		unauthorizedErr := Unauthorized("Unauthorized")
		otherErr := Forbidden("Forbidden")

		assert.True(t, IsUnauthorized(unauthorizedErr))
		assert.False(t, IsUnauthorized(otherErr))
		assert.False(t, IsUnauthorized(assert.AnError))
	})
}
