package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeString(t *testing.T) {
	t.Run("remove HTML tags", func(t *testing.T) {
		input := "<script>alert('xss')</script>Hello World<b>Bold</b>"
		expected := "alert(xss)Hello WorldBold" // SQL injection removes quotes

		result := SanitizeString(input, DefaultSanitizationOptions())
		assert.Equal(t, expected, result)
	})

	t.Run("remove SQL injection patterns", func(t *testing.T) {
		input := "'; DROP TABLE users; SELECT * FROM passwords WHERE 1=1 --"

		result := SanitizeString(input, DefaultSanitizationOptions())
		// Should not contain dangerous SQL keywords
		assert.NotContains(t, strings.ToLower(result), "drop table")
		assert.NotContains(t, result, "'")
		assert.NotContains(t, result, "--")
	})

	t.Run("empty string", func(t *testing.T) {
		result := SanitizeString("", DefaultSanitizationOptions())
		assert.Equal(t, "", result)
	})

	t.Run("strict sanitization", func(t *testing.T) {
		input := "Hello<script>alert('xss')</script> & goodbye! @user #hashtag"
		options := StrictSanitizationOptions()

		result := SanitizeString(input, options)
		// Should be very restrictive
		assert.NotContains(t, result, "<script>")
		assert.NotContains(t, result, "'")
	})
}

func TestSanitizeEmail(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"  USER@EXAMPLE.COM  ", "user@example.com"},
		{"user+tag@example.com", "user+tag@example.com"},
		{"user@sub.example.com", "user@sub.example.com"},
		{"", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := SanitizeEmail(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSanitizeUsername(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"  Username123  ", "Username123"},
		{"user_name", "user_name"},
		{"user-name", "user-name"},
		{"user.name", "user.name"},
		{"", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := SanitizeUsername(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	testCases := []struct {
		input       string
		shouldClean bool
	}{
		{"document.pdf", false},
		{"My Document (1).pdf", false},
		{"../../../etc/passwd", true},
		{"file<script>.pdf", true},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := SanitizeFilename(tc.input)
			if tc.shouldClean {
				assert.NotEqual(t, tc.input, result, "Input should have been sanitized")
				assert.NotContains(t, result, "../")
				assert.NotContains(t, result, "<script>")
			} else if tc.input != "" {
				assert.Equal(t, tc.input, result)
			}
		})
	}
}

func TestSanitizeTitle(t *testing.T) {
	testCases := []struct {
		input       string
		shouldClean bool
	}{
		{"My Research Paper", false},
		{"Title with <b>HTML</b>", true},
		{"Title'; DROP TABLE;", true},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := SanitizeTitle(tc.input)
			if tc.shouldClean {
				assert.NotEqual(t, tc.input, result, "Input should have been sanitized")
				assert.NotContains(t, result, "<b>")
				assert.NotContains(t, result, "';")
			} else if tc.input != "" {
				assert.Equal(t, tc.input, result)
			}
		})
	}
}

func TestSanitizeDescription(t *testing.T) {
	testCases := []struct {
		input       string
		shouldClean bool
	}{
		{"This is a safe description.", false},
		{"Description with <script>alert('xss')</script>", true},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := SanitizeDescription(tc.input)
			if tc.shouldClean {
				assert.NotEqual(t, tc.input, result, "Input should have been sanitized")
				assert.NotContains(t, result, "<script>")
			} else if tc.input != "" {
				assert.Equal(t, tc.input, result)
			}
		})
	}
}

func TestSanitizeTag(t *testing.T) {
	testCases := []struct {
		input       string
		shouldClean bool
	}{
		{"research", false},
		{"machine-learning", false},
		{"data_science", false},
		{"<script>tag</script>", true},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := SanitizeTag(tc.input)
			if tc.shouldClean {
				assert.NotEqual(t, tc.input, result, "Input should have been sanitized")
				assert.NotContains(t, result, "<script>")
			} else if tc.input != "" {
				assert.Equal(t, tc.input, result)
			}
		})
	}
}

func TestSanitizeURL(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com/path", "http://example.com/path"},
		{"javascript:alert('xss')", ""},
		{"", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := SanitizeURL(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestStripSQLInjection(t *testing.T) {
	testCases := []struct {
		input       string
		shouldClean bool
	}{
		{"Normal text", false},
		{"'; DROP TABLE users; --", true},
		{"Text with 'single quotes'", true},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := StripSQLInjection(tc.input)
			if tc.shouldClean {
				assert.NotEqual(t, tc.input, result, "Input should have been sanitized")
				assert.NotContains(t, strings.ToLower(result), "drop table")
			} else if tc.input != "" {
				assert.Equal(t, tc.input, result)
			}
		})
	}
}

func TestStripHTML(t *testing.T) {
	testCases := []struct {
		input       string
		shouldClean bool
	}{
		{"Normal text", false},
		{"<p>Hello World</p>", true},
		{"<script>alert('xss')</script>", true},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := StripHTML(tc.input)
			if tc.shouldClean {
				assert.NotEqual(t, tc.input, result, "Input should have been sanitized")
				assert.NotContains(t, result, "<p>")
				assert.NotContains(t, result, "<script>")
			} else if tc.input != "" {
				assert.Equal(t, tc.input, result)
			}
		})
	}
}

func TestSanitizationOptions(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		options := DefaultSanitizationOptions()
		assert.True(t, options.StripHTML)
		assert.True(t, options.StripSQLInjection)
		assert.True(t, options.StripControlChars)
		assert.Equal(t, 1000, options.MaxLength)
	})

	t.Run("strict options", func(t *testing.T) {
		options := StrictSanitizationOptions()
		assert.True(t, options.StripHTML)
		assert.True(t, options.StripSQLInjection)
		assert.True(t, options.StripControlChars)
		assert.Equal(t, 500, options.MaxLength)
		assert.NotNil(t, options.AllowedChars)
	})

	t.Run("permissive options", func(t *testing.T) {
		options := PermissiveSanitizationOptions()
		assert.True(t, options.StripHTML)
		assert.True(t, options.StripSQLInjection)
		assert.True(t, options.StripControlChars)
		assert.Equal(t, 5000, options.MaxLength)
		assert.False(t, options.CollapseWhitespace)
	})
}
