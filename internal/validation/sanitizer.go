package validation

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

var (
	// HTML tag removal regex
	htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

	// SQL injection patterns
	sqlInjectionRegex = regexp.MustCompile(`(?i)\b(union|select|insert|update|delete|drop|create|alter|exec|execute|script|declare|cast|convert|having|where|order\s+by|group\s+by)\b`)

	// XSS patterns
	xssPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)javascript:`),
		regexp.MustCompile(`(?i)vbscript:`),
		regexp.MustCompile(`(?i)onload`),
		regexp.MustCompile(`(?i)onerror`),
		regexp.MustCompile(`(?i)onclick`),
		regexp.MustCompile(`(?i)onmouseover`),
		regexp.MustCompile(`(?i)<script`),
		regexp.MustCompile(`(?i)</script>`),
		regexp.MustCompile(`(?i)<iframe`),
		regexp.MustCompile(`(?i)<object`),
		regexp.MustCompile(`(?i)<embed`),
	}

	// Common whitespace and control characters
	controlCharsRegex = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)

	// Multiple whitespace regex
	multipleSpacesRegex = regexp.MustCompile(`\s+`)
)

// SanitizationOptions controls how strings are sanitized
type SanitizationOptions struct {
	StripHTML          bool
	StripSQLInjection  bool
	StripXSS           bool
	StripControlChars  bool
	TrimWhitespace     bool
	CollapseWhitespace bool
	MaxLength          int
	AllowedChars       *regexp.Regexp
	DisallowedChars    *regexp.Regexp
	PreserveCase       bool
}

// DefaultSanitizationOptions returns sensible defaults for most use cases
func DefaultSanitizationOptions() SanitizationOptions {
	return SanitizationOptions{
		StripHTML:          true,
		StripSQLInjection:  true,
		StripXSS:           true,
		StripControlChars:  true,
		TrimWhitespace:     true,
		CollapseWhitespace: true,
		MaxLength:          1000,
		PreserveCase:       true,
	}
}

// StrictSanitizationOptions returns very strict sanitization for security-critical fields
func StrictSanitizationOptions() SanitizationOptions {
	return SanitizationOptions{
		StripHTML:          true,
		StripSQLInjection:  true,
		StripXSS:           true,
		StripControlChars:  true,
		TrimWhitespace:     true,
		CollapseWhitespace: true,
		MaxLength:          500,
		AllowedChars:       regexp.MustCompile(`^[a-zA-Z0-9\s\-_\.@]+$`),
		PreserveCase:       true,
	}
}

// PermissiveSanitizationOptions allows more characters but still removes dangerous content
func PermissiveSanitizationOptions() SanitizationOptions {
	return SanitizationOptions{
		StripHTML:          true,
		StripSQLInjection:  true,
		StripXSS:           true,
		StripControlChars:  true,
		TrimWhitespace:     true,
		CollapseWhitespace: false, // Allow multiple spaces in content
		MaxLength:          5000,
		PreserveCase:       true,
	}
}

// SanitizeString sanitizes a string according to the given options
func SanitizeString(input string, options SanitizationOptions) string {
	if input == "" {
		return input
	}

	result := input

	// Remove control characters first
	if options.StripControlChars {
		result = controlCharsRegex.ReplaceAllString(result, "")
	}

	// Strip HTML tags
	if options.StripHTML {
		result = StripHTML(result)
	}

	// Strip XSS patterns
	if options.StripXSS {
		result = StripXSS(result)
	}

	// Strip SQL injection patterns
	if options.StripSQLInjection {
		result = StripSQLInjection(result)
	}

	// Remove disallowed characters
	if options.DisallowedChars != nil {
		result = options.DisallowedChars.ReplaceAllString(result, "")
	}

	// Keep only allowed characters
	if options.AllowedChars != nil {
		// Build new string with only allowed characters
		var sanitized strings.Builder
		for _, r := range result {
			if options.AllowedChars.MatchString(string(r)) {
				sanitized.WriteRune(r)
			}
		}
		result = sanitized.String()
	}

	// Collapse multiple whitespaces
	if options.CollapseWhitespace {
		result = multipleSpacesRegex.ReplaceAllString(result, " ")
	}

	// Trim whitespace
	if options.TrimWhitespace {
		result = strings.TrimSpace(result)
	}

	// Enforce max length
	if options.MaxLength > 0 && len(result) > options.MaxLength {
		result = result[:options.MaxLength]
		// Try to break at word boundary
		if lastSpace := strings.LastIndex(result, " "); lastSpace > options.MaxLength*3/4 {
			result = result[:lastSpace]
		}
		result = strings.TrimSpace(result)
	}

	return result
}

// StripHTML removes HTML tags from a string
func StripHTML(input string) string {
	// First pass: remove HTML tags
	result := htmlTagRegex.ReplaceAllString(input, "")

	// Second pass: decode HTML entities
	result = html.UnescapeString(result)

	return result
}

// StripXSS removes potential XSS attack vectors
func StripXSS(input string) string {
	result := input

	for _, pattern := range xssPatterns {
		result = pattern.ReplaceAllString(result, "")
	}

	return result
}

// StripSQLInjection removes potential SQL injection patterns
func StripSQLInjection(input string) string {
	// Remove SQL keywords in a context that could be dangerous
	result := sqlInjectionRegex.ReplaceAllStringFunc(input, func(match string) string {
		// If the keyword is surrounded by word boundaries, it's likely dangerous
		return ""
	})

	// Remove other dangerous SQL characters
	result = strings.ReplaceAll(result, "'", "")
	result = strings.ReplaceAll(result, "\"", "")
	result = strings.ReplaceAll(result, ";", "")
	result = strings.ReplaceAll(result, "--", "")
	result = strings.ReplaceAll(result, "/*", "")
	result = strings.ReplaceAll(result, "*/", "")

	return result
}

// SanitizeFilename sanitizes a filename to be safe for file system operations
func SanitizeFilename(filename string) string {
	if filename == "" {
		return "untitled"
	}

	// Remove path separators and dangerous characters
	unsafe := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|", "\x00"}
	result := filename

	for _, char := range unsafe {
		result = strings.ReplaceAll(result, char, "")
	}

	// Remove leading/trailing dots and spaces
	result = strings.Trim(result, ". ")

	// Handle reserved Windows filenames
	reserved := []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}
	upperResult := strings.ToUpper(result)
	for _, res := range reserved {
		if upperResult == res {
			result = result + "_file"
			break
		}
	}

	// Ensure filename is not empty and not too long
	if result == "" {
		result = "untitled"
	}

	if len(result) > 255 {
		result = result[:255]
	}

	return result
}

// SanitizeUsername sanitizes a username
func SanitizeUsername(username string) string {
	options := SanitizationOptions{
		StripHTML:          true,
		StripSQLInjection:  true,
		StripXSS:           true,
		StripControlChars:  true,
		TrimWhitespace:     true,
		CollapseWhitespace: true,
		MaxLength:          50,
		AllowedChars:       regexp.MustCompile(`[a-zA-Z0-9_\-\.]`),
		PreserveCase:       true,
	}

	result := SanitizeString(username, options)

	// Ensure it starts with alphanumeric
	if len(result) > 0 && !unicode.IsLetter(rune(result[0])) && !unicode.IsDigit(rune(result[0])) {
		result = "user_" + result
	}

	return result
}

// SanitizeTag sanitizes a tag
func SanitizeTag(tag string) string {
	options := SanitizationOptions{
		StripHTML:          true,
		StripSQLInjection:  true,
		StripXSS:           true,
		StripControlChars:  true,
		TrimWhitespace:     true,
		CollapseWhitespace: true,
		MaxLength:          50,
		AllowedChars:       regexp.MustCompile(`[a-zA-Z0-9_\-]`),
		PreserveCase:       false,
	}

	result := SanitizeString(strings.ToLower(tag), options)
	return result
}

// SanitizeDescription sanitizes longer text content like descriptions
func SanitizeDescription(description string) string {
	options := PermissiveSanitizationOptions()
	options.MaxLength = 2000

	return SanitizeString(description, options)
}

// SanitizeTitle sanitizes titles and names
func SanitizeTitle(title string) string {
	options := SanitizationOptions{
		StripHTML:          true,
		StripSQLInjection:  true,
		StripXSS:           true,
		StripControlChars:  true,
		TrimWhitespace:     true,
		CollapseWhitespace: true,
		MaxLength:          255,
		PreserveCase:       true,
	}

	return SanitizeString(title, options)
}

// SanitizeEmail sanitizes email addresses (basic sanitization, validation should be done separately)
func SanitizeEmail(email string) string {
	options := SanitizationOptions{
		StripHTML:          true,
		StripSQLInjection:  true,
		StripXSS:           true,
		StripControlChars:  true,
		TrimWhitespace:     true,
		CollapseWhitespace: true,
		MaxLength:          254, // RFC 5321 limit
		AllowedChars:       regexp.MustCompile(`[a-zA-Z0-9@._\-+]`),
		PreserveCase:       false,
	}

	return strings.ToLower(SanitizeString(email, options))
}

// SanitizeURL sanitizes URLs (basic sanitization, validation should be done separately)
func SanitizeURL(url string) string {
	// Check for dangerous protocols first
	lowercaseURL := strings.ToLower(strings.TrimSpace(url))
	if strings.HasPrefix(lowercaseURL, "javascript:") ||
		strings.HasPrefix(lowercaseURL, "vbscript:") ||
		strings.HasPrefix(lowercaseURL, "data:") {
		return "" // Return empty string for dangerous protocols
	}

	options := SanitizationOptions{
		StripHTML:          false, // URLs might contain encoded HTML
		StripSQLInjection:  true,
		StripXSS:           true,
		StripControlChars:  true,
		TrimWhitespace:     true,
		CollapseWhitespace: true,
		MaxLength:          2048,
		PreserveCase:       true,
	}

	return SanitizeString(url, options)
}
