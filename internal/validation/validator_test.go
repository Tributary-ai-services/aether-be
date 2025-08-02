package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Tributary-ai-services/aether-be/internal/models"
)

func TestValidateUserCreateRequest(t *testing.T) {
	t.Run("valid user create request", func(t *testing.T) {
		req := models.UserCreateRequest{
			KeycloakID: "550e8400-e29b-41d4-a716-446655440000",
			Email:      "test@example.com",
			Username:   "testuser123",
			FullName:   "Test User",
			AvatarURL:  "https://example.com/avatar.jpg",
		}

		err := Validate(req)
		assert.NoError(t, err)
	})

	t.Run("invalid keycloak ID", func(t *testing.T) {
		req := models.UserCreateRequest{
			KeycloakID: "not-a-uuid",
			Email:      "test@example.com",
			Username:   "testuser123",
			FullName:   "Test User",
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "KeycloakID")
	})

	t.Run("invalid email", func(t *testing.T) {
		req := models.UserCreateRequest{
			KeycloakID: "550e8400-e29b-41d4-a716-446655440000",
			Email:      "invalid-email",
			Username:   "testuser123",
			FullName:   "Test User",
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Email")
	})

	t.Run("invalid username", func(t *testing.T) {
		req := models.UserCreateRequest{
			KeycloakID: "550e8400-e29b-41d4-a716-446655440000",
			Email:      "test@example.com",
			Username:   "ab", // too short
			FullName:   "Test User",
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Username")
	})

	t.Run("unsafe full name", func(t *testing.T) {
		req := models.UserCreateRequest{
			KeycloakID: "550e8400-e29b-41d4-a716-446655440000",
			Email:      "test@example.com",
			Username:   "testuser123",
			FullName:   "Test <script>alert('xss')</script> User",
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "FullName")
	})
}

func TestValidateDocumentCreateRequest(t *testing.T) {
	t.Run("valid document create request", func(t *testing.T) {
		req := models.DocumentCreateRequest{
			Name:        "research-paper.pdf",
			Description: "My research paper on AI",
			NotebookID:  "550e8400-e29b-41d4-a716-446655440000",
			Tags:        []string{"research", "ai", "machine-learning"},
		}

		err := Validate(req)
		assert.NoError(t, err)
	})

	t.Run("invalid filename", func(t *testing.T) {
		req := models.DocumentCreateRequest{
			Name:       "../../../etc/passwd",
			NotebookID: "550e8400-e29b-41d4-a716-446655440000",
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Name")
	})

	t.Run("unsafe description", func(t *testing.T) {
		req := models.DocumentCreateRequest{
			Name:        "document.pdf",
			Description: "Document with <script>alert('xss')</script> malicious content",
			NotebookID:  "550e8400-e29b-41d4-a716-446655440000",
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Description")
	})

	t.Run("invalid tags", func(t *testing.T) {
		req := models.DocumentCreateRequest{
			Name:       "document.pdf",
			NotebookID: "550e8400-e29b-41d4-a716-446655440000",
			Tags:       []string{"valid-tag", "<script>alert('xss')</script>", "another-valid-tag"},
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Tags")
	})

	t.Run("tag too long", func(t *testing.T) {
		longTag := make([]byte, 100)
		for i := range longTag {
			longTag[i] = 'a'
		}

		req := models.DocumentCreateRequest{
			Name:       "document.pdf",
			NotebookID: "550e8400-e29b-41d4-a716-446655440000",
			Tags:       []string{string(longTag)},
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Tags")
	})
}

func TestValidateNotebookCreateRequest(t *testing.T) {
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

	t.Run("invalid visibility", func(t *testing.T) {
		req := models.NotebookCreateRequest{
			Name:       "My Notebook",
			Visibility: "invalid_visibility",
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Visibility")
	})

	t.Run("name too long", func(t *testing.T) {
		longName := make([]byte, 300)
		for i := range longName {
			longName[i] = 'a'
		}

		req := models.NotebookCreateRequest{
			Name:       string(longName),
			Visibility: "private",
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Name")
	})
}

func TestValidateSearchRequests(t *testing.T) {
	t.Run("valid user search request", func(t *testing.T) {
		req := models.UserSearchRequest{
			Query:  "john",
			Email:  "john@example.com",
			Status: "active",
			Limit:  50,
			Offset: 10,
		}

		err := Validate(req)
		assert.NoError(t, err)
	})

	t.Run("invalid search limit", func(t *testing.T) {
		req := models.UserSearchRequest{
			Query: "john",
			Limit: 200, // exceeds max of 100
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Limit")
	})

	t.Run("query too short", func(t *testing.T) {
		req := models.UserSearchRequest{
			Query: "a", // too short, min is 2
			Limit: 10,
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Query")
	})

	t.Run("negative offset", func(t *testing.T) {
		req := models.UserSearchRequest{
			Query:  "john",
			Limit:  10,
			Offset: -1,
		}

		err := Validate(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Offset")
	})
}

func TestCustomValidators(t *testing.T) {
	t.Run("username validator", func(t *testing.T) {
		testCases := []struct {
			username string
			valid    bool
		}{
			{"validuser123", true},
			{"user_name", true},
			{"user-name", true},
			{"user.name", true},
			{"123user", true},
			{"ab", false},                          // too short
			{"a", false},                           // too short
			{"user name", false},                   // space not allowed
			{"user@name", false},                   // @ not allowed
			{"user#name", false},                   // # not allowed
			{"user'; DROP TABLE users; --", false}, // SQL injection attempt
		}

		for _, tc := range testCases {
			t.Run(tc.username, func(t *testing.T) {
				type TestStruct struct {
					Username string `validate:"username"`
				}

				testStruct := TestStruct{Username: tc.username}
				err := Validate(testStruct)

				if tc.valid {
					assert.NoError(t, err, "Expected username '%s' to be valid", tc.username)
				} else {
					assert.Error(t, err, "Expected username '%s' to be invalid", tc.username)
				}
			})
		}
	})

	t.Run("safe_string validator", func(t *testing.T) {
		testCases := []struct {
			input string
			valid bool
		}{
			{"Safe string", true},
			{"String with numbers 123", true},
			{"String with punctuation: hello, world!", true},
			{"String with 'single quotes'", true},
			{"String with \"double quotes\"", false}, // contains quotes
			{"<script>alert('xss')</script>", false}, // contains HTML
			{"'; DROP TABLE users; --", false},       // SQL injection
			{"String with < and >", false},           // contains angle brackets
		}

		for _, tc := range testCases {
			t.Run(tc.input, func(t *testing.T) {
				type TestStruct struct {
					Content string `validate:"safe_string"`
				}

				testStruct := TestStruct{Content: tc.input}
				err := Validate(testStruct)

				if tc.valid {
					assert.NoError(t, err, "Expected string '%s' to be valid", tc.input)
				} else {
					assert.Error(t, err, "Expected string '%s' to be invalid", tc.input)
				}
			})
		}
	})

	t.Run("tag validator", func(t *testing.T) {
		testCases := []struct {
			tag   string
			valid bool
		}{
			{"valid-tag", true},
			{"validtag123", true},
			{"tag_name", true},
			{"research", true},
			{"machine-learning", true},
			{"", false},                      // empty
			{" ", false},                     // whitespace only
			{"tag with spaces", false},       // spaces not allowed
			{"<script>", false},              // HTML not allowed
			{"'; DROP TABLE;", false},        // SQL injection
			{strings.Repeat("a", 60), false}, // too long (max 50)
		}

		for _, tc := range testCases {
			t.Run(tc.tag, func(t *testing.T) {
				type TestStruct struct {
					Tag string `validate:"tag"`
				}

				testStruct := TestStruct{Tag: tc.tag}
				err := Validate(testStruct)

				if tc.valid {
					assert.NoError(t, err, "Expected tag '%s' to be valid", tc.tag)
				} else {
					assert.Error(t, err, "Expected tag '%s' to be invalid", tc.tag)
				}
			})
		}
	})

	t.Run("notebook_visibility validator", func(t *testing.T) {
		validVisibilities := []string{"private", "shared", "public"}
		invalidVisibilities := []string{"", "invalid", "PRIVATE", "Secret", "internal"}

		for _, visibility := range validVisibilities {
			t.Run(visibility, func(t *testing.T) {
				type TestStruct struct {
					Visibility string `validate:"notebook_visibility"`
				}

				testStruct := TestStruct{Visibility: visibility}
				err := Validate(testStruct)
				assert.NoError(t, err, "Expected visibility '%s' to be valid", visibility)
			})
		}

		for _, visibility := range invalidVisibilities {
			t.Run(visibility, func(t *testing.T) {
				type TestStruct struct {
					Visibility string `validate:"notebook_visibility"`
				}

				testStruct := TestStruct{Visibility: visibility}
				err := Validate(testStruct)
				assert.Error(t, err, "Expected visibility '%s' to be invalid", visibility)
			})
		}
	})

	t.Run("filename validator", func(t *testing.T) {
		testCases := []struct {
			filename string
			valid    bool
		}{
			{"document.pdf", true},
			{"research-paper.docx", true},
			{"file_name.txt", true},
			{"My Document (1).pdf", true},
			{"../../../etc/passwd", false},    // path traversal
			{"file<script>.pdf", false},       // HTML injection
			{"file'; DROP TABLE;.pdf", false}, // SQL injection
			{"", false},                       // empty
			{" ", false},                      // whitespace only
		}

		for _, tc := range testCases {
			t.Run(tc.filename, func(t *testing.T) {
				type TestStruct struct {
					Filename string `validate:"filename"`
				}

				testStruct := TestStruct{Filename: tc.filename}
				err := Validate(testStruct)

				if tc.valid {
					assert.NoError(t, err, "Expected filename '%s' to be valid", tc.filename)
				} else {
					assert.Error(t, err, "Expected filename '%s' to be invalid", tc.filename)
				}
			})
		}
	})
}
