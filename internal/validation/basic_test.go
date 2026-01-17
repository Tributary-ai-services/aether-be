package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Tributary-ai-services/aether-be/internal/models"
)

func TestBasicValidation(t *testing.T) {
	t.Run("valid user create request", func(t *testing.T) {
		req := models.UserCreateRequest{
			KeycloakID: "550e8400-e29b-41d4-a716-446655440000",
			Email:      "test@example.com",
			Username:   "testuser123",
			FullName:   "Test User",
		}

		err := Validate(req)
		assert.NoError(t, err)
	})

	t.Run("invalid user create request - bad email", func(t *testing.T) {
		req := models.UserCreateRequest{
			KeycloakID: "550e8400-e29b-41d4-a716-446655440000",
			Email:      "invalid-email",
			Username:   "testuser123",
			FullName:   "Test User",
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "email")
	})

	t.Run("valid document create request", func(t *testing.T) {
		req := models.DocumentCreateRequest{
			Name:        "research-paper.pdf",
			Description: "My research paper",
			NotebookID:  "550e8400-e29b-41d4-a716-446655440000",
			Tags:        []string{"research", "ai"},
		}

		err := Validate(req)
		assert.NoError(t, err)
	})

	t.Run("valid notebook create request", func(t *testing.T) {
		req := models.NotebookCreateRequest{
			Name:        "My Research Notebook",
			Description: "A notebook for AI research",
			Visibility:  "private",
			Tags:        []string{"research", "ai"},
		}

		err := Validate(req)
		assert.NoError(t, err)
	})
}

func TestBasicSanitization(t *testing.T) {
	t.Run("sanitize HTML from string", func(t *testing.T) {
		input := "<p>Hello World</p>"
		result := StripHTML(input)
		assert.NotContains(t, result, "<p>")
		assert.Contains(t, result, "Hello World")
	})

	t.Run("sanitize email", func(t *testing.T) {
		input := "  TEST@EXAMPLE.COM  "
		result := SanitizeEmail(input)
		assert.Equal(t, "test@example.com", result)
	})

	t.Run("sanitize dangerous filename", func(t *testing.T) {
		input := "../../../etc/passwd"
		result := SanitizeFilename(input)
		assert.NotContains(t, result, "../")
	})

	t.Run("strip SQL injection", func(t *testing.T) {
		input := "'; DROP TABLE users; --"
		result := StripSQLInjection(input)
		assert.NotContains(t, result, "DROP TABLE")
		assert.NotContains(t, result, "'")
		assert.NotContains(t, result, "--")
	})
}
