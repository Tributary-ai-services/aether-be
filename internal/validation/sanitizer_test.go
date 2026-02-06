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

// ============================================================================
// Web Content Sanitization Tests (for Crawl4AI and web scraping)
// ============================================================================

func TestSanitizeWebContent(t *testing.T) {
	options := DefaultWebContentSanitizationOptions()

	t.Run("removes script tags with content", func(t *testing.T) {
		input := `<html><body>
			<h1>Hello World</h1>
			<script>alert('xss')</script>
			<script type="text/javascript">
				var x = 1;
				document.write("malicious");
			</script>
			<p>Safe content</p>
		</body></html>`

		result := SanitizeWebContent(input, options)
		assert.NotContains(t, result, "<script>")
		assert.NotContains(t, result, "</script>")
		assert.NotContains(t, result, "alert('xss')")
		assert.NotContains(t, result, "document.write")
		assert.Contains(t, result, "<h1>Hello World</h1>")
		assert.Contains(t, result, "<p>Safe content</p>")
	})

	t.Run("removes style tags with content", func(t *testing.T) {
		input := `<html><body>
			<style>body { background: red; }</style>
			<p>Content</p>
		</body></html>`

		result := SanitizeWebContent(input, options)
		assert.NotContains(t, result, "<style>")
		assert.NotContains(t, result, "</style>")
		assert.NotContains(t, result, "background: red")
		assert.Contains(t, result, "<p>Content</p>")
	})

	t.Run("removes event handlers", func(t *testing.T) {
		input := `<div onclick="alert('xss')" onmouseover="steal()" onerror="hack()">Content</div>
			<img src="x.jpg" onerror="alert('img xss')" alt="image">
			<a href="#" onload="malicious()">Link</a>`

		result := SanitizeWebContent(input, options)
		assert.NotContains(t, result, "onclick=")
		assert.NotContains(t, result, "onmouseover=")
		assert.NotContains(t, result, "onerror=")
		assert.NotContains(t, result, "onload=")
		assert.NotContains(t, result, "alert(")
		assert.Contains(t, result, "Content")
		assert.Contains(t, result, "Link")
	})

	t.Run("removes javascript: URLs", func(t *testing.T) {
		input := `<a href="javascript:alert('xss')">Click me</a>
			<a href="JAVASCRIPT:void(0)">Another link</a>`

		result := SanitizeWebContent(input, options)
		assert.NotContains(t, result, "javascript:")
		assert.NotContains(t, result, "JAVASCRIPT:")
		assert.Contains(t, result, "Click me")
	})

	t.Run("removes iframe tags", func(t *testing.T) {
		input := `<p>Safe</p>
			<iframe src="http://evil.com" width="100%" height="100%"></iframe>
			<p>Also safe</p>`

		result := SanitizeWebContent(input, options)
		assert.NotContains(t, result, "<iframe")
		assert.NotContains(t, result, "</iframe>")
		assert.NotContains(t, result, "evil.com")
		assert.Contains(t, result, "<p>Safe</p>")
		assert.Contains(t, result, "<p>Also safe</p>")
	})

	t.Run("removes object and embed tags", func(t *testing.T) {
		input := `<object data="malicious.swf"></object>
			<embed src="bad.swf" type="application/x-shockwave-flash">`

		result := SanitizeWebContent(input, options)
		assert.NotContains(t, result, "<object")
		assert.NotContains(t, result, "<embed")
	})

	t.Run("removes form elements", func(t *testing.T) {
		input := `<form action="http://evil.com/steal">
			<input type="text" name="password">
			<button type="submit">Submit</button>
		</form>`

		result := SanitizeWebContent(input, options)
		assert.NotContains(t, result, "<form")
		assert.NotContains(t, result, "<input")
		assert.NotContains(t, result, "<button")
	})

	t.Run("removes base tag", func(t *testing.T) {
		input := `<head><base href="http://evil.com/"></head><body><p>Content</p></body>`

		result := SanitizeWebContent(input, options)
		assert.NotContains(t, result, "<base")
	})

	t.Run("removes meta refresh", func(t *testing.T) {
		input := `<head><meta http-equiv="refresh" content="0;url=http://evil.com"></head><body><p>Content</p></body>`

		result := SanitizeWebContent(input, options)
		assert.NotContains(t, result, "http-equiv")
		assert.NotContains(t, result, "refresh")
	})

	t.Run("handles empty input", func(t *testing.T) {
		result := SanitizeWebContent("", options)
		assert.Equal(t, "", result)
	})

	t.Run("preserves safe HTML structure", func(t *testing.T) {
		input := `<html>
			<head><title>Safe Page</title></head>
			<body>
				<h1>Welcome</h1>
				<p>This is <strong>safe</strong> content.</p>
				<ul>
					<li>Item 1</li>
					<li>Item 2</li>
				</ul>
				<a href="https://example.com">Safe link</a>
				<img src="https://example.com/image.jpg" alt="Safe image">
			</body>
		</html>`

		result := SanitizeWebContent(input, options)
		assert.Contains(t, result, "<h1>Welcome</h1>")
		assert.Contains(t, result, "<strong>safe</strong>")
		assert.Contains(t, result, "<li>Item 1</li>")
		assert.Contains(t, result, `href="https://example.com"`)
		assert.Contains(t, result, `src="https://example.com/image.jpg"`)
	})

	t.Run("enforces max length when specified", func(t *testing.T) {
		options := DefaultWebContentSanitizationOptions()
		options.MaxContentLength = 100
		input := strings.Repeat("a", 200)

		result := SanitizeWebContent(input, options)
		assert.Equal(t, 100, len(result))
	})
}

func TestSanitizeMarkdownContent(t *testing.T) {
	options := DefaultWebContentSanitizationOptions()

	t.Run("removes embedded script tags", func(t *testing.T) {
		input := `# Heading

This is safe markdown.

<script>alert('xss')</script>

More safe content.`

		result := SanitizeMarkdownContent(input, options)
		assert.NotContains(t, result, "<script>")
		assert.NotContains(t, result, "alert('xss')")
		assert.Contains(t, result, "# Heading")
		assert.Contains(t, result, "This is safe markdown.")
		assert.Contains(t, result, "More safe content.")
	})

	t.Run("removes javascript URLs from markdown links", func(t *testing.T) {
		input := `[Click here](javascript:alert('xss'))
[Safe link](https://example.com)`

		result := SanitizeMarkdownContent(input, options)
		assert.NotContains(t, result, "javascript:")
		assert.Contains(t, result, "[Safe link](https://example.com)")
	})

	t.Run("preserves markdown formatting", func(t *testing.T) {
		input := "# Heading 1\n\n## Heading 2\n\n- List item 1\n- List item 2\n\n**Bold** and *italic* text.\n\n```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```\n"

		result := SanitizeMarkdownContent(input, options)
		assert.Contains(t, result, "# Heading 1")
		assert.Contains(t, result, "## Heading 2")
		assert.Contains(t, result, "- List item 1")
		assert.Contains(t, result, "**Bold**")
		assert.Contains(t, result, "*italic*")
		assert.Contains(t, result, "```go")
	})

	t.Run("handles empty input", func(t *testing.T) {
		result := SanitizeMarkdownContent("", options)
		assert.Equal(t, "", result)
	})
}

func TestSanitizeMediaURL(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid https URL", "https://example.com/image.jpg", "https://example.com/image.jpg"},
		{"valid http URL", "http://example.com/video.mp4", "http://example.com/video.mp4"},
		{"data URL for image", "data:image/png;base64,abc123", "data:image/png;base64,abc123"},
		{"javascript URL", "javascript:alert('xss')", ""},
		{"vbscript URL", "vbscript:msgbox('xss')", ""},
		{"data URL with text/html", "data:text/html,<script>alert('xss')</script>", ""},
		{"empty string", "", ""},
		{"URL with control chars", "https://example.com/image\x00.jpg", "https://example.com/image.jpg"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := SanitizeMediaURL(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestWebContentSanitizationOptions(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		options := DefaultWebContentSanitizationOptions()
		assert.True(t, options.RemoveScripts)
		assert.True(t, options.RemoveStyles)
		assert.True(t, options.RemoveEventHandlers)
		assert.True(t, options.RemoveJavaScriptURLs)
		assert.True(t, options.RemoveDangerousTags)
		assert.True(t, options.RemoveBaseTag)
		assert.True(t, options.RemoveMetaRefresh)
		assert.False(t, options.PreserveMarkdown)
		assert.Equal(t, 0, options.MaxContentLength)
	})

	t.Run("strict options", func(t *testing.T) {
		options := StrictWebContentSanitizationOptions()
		assert.True(t, options.RemoveScripts)
		assert.True(t, options.RemoveDangerousTags)
		assert.Equal(t, 10*1024*1024, options.MaxContentLength)
	})
}
