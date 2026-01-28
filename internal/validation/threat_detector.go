package validation

import (
	"regexp"
	"strings"
)

// DetectedThreat represents a security threat found in input
type DetectedThreat struct {
	Type           string `json:"type"`            // sql_injection, xss, html_injection, control_chars
	Severity       string `json:"severity"`        // low, medium, high, critical
	FieldName      string `json:"field_name"`
	Pattern        string `json:"pattern"`
	MatchedContent string `json:"matched_content"`
	Action         string `json:"action"`          // sanitized, isolated, rejected
}

// ThreatDetector provides threat detection capabilities
type ThreatDetector struct {
	// SQL injection patterns with severity levels
	sqlPatterns []threatPattern

	// XSS patterns with severity levels
	xssPatterns []threatPattern

	// HTML tag pattern
	htmlTagPattern *regexp.Regexp

	// Control character pattern
	controlCharPattern *regexp.Regexp
}

// threatPattern represents a detection pattern with its severity
type threatPattern struct {
	pattern  *regexp.Regexp
	severity string
	name     string
}

// NewThreatDetector creates a new threat detector instance
func NewThreatDetector() *ThreatDetector {
	return &ThreatDetector{
		sqlPatterns: []threatPattern{
			// Critical: Direct data exfiltration or table manipulation
			{regexp.MustCompile(`(?i)\bUNION\s+(ALL\s+)?SELECT\b`), "critical", "UNION SELECT"},
			{regexp.MustCompile(`(?i)\bDROP\s+(TABLE|DATABASE|INDEX)\b`), "critical", "DROP statement"},
			{regexp.MustCompile(`(?i)\bTRUNCATE\s+TABLE\b`), "critical", "TRUNCATE TABLE"},
			{regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`), "high", "DELETE FROM"},
			{regexp.MustCompile(`(?i)\bINSERT\s+INTO\b`), "high", "INSERT INTO"},
			{regexp.MustCompile(`(?i)\bUPDATE\s+\w+\s+SET\b`), "high", "UPDATE SET"},

			// High: Potential data access
			{regexp.MustCompile(`(?i)\bSELECT\s+.+\s+FROM\b`), "high", "SELECT FROM"},
			{regexp.MustCompile(`(?i)\bEXEC(UTE)?\s*\(`), "high", "EXEC function"},
			{regexp.MustCompile(`(?i)\bxp_cmdshell\b`), "critical", "xp_cmdshell"},
			{regexp.MustCompile(`(?i);\s*--`), "medium", "SQL comment injection"},

			// Medium: SQL syntax that could be part of injection
			{regexp.MustCompile(`(?i)\bOR\s+['"]?\d+['"]?\s*=\s*['"]?\d+['"]?`), "medium", "OR 1=1 pattern"},
			{regexp.MustCompile(`(?i)\bAND\s+['"]?\d+['"]?\s*=\s*['"]?\d+['"]?`), "medium", "AND 1=1 pattern"},
			{regexp.MustCompile(`(?i)\bHAVING\b`), "medium", "HAVING clause"},
			{regexp.MustCompile(`(?i)\bGROUP\s+BY\b`), "low", "GROUP BY clause"},
			{regexp.MustCompile(`(?i)\bORDER\s+BY\b`), "low", "ORDER BY clause"},

			// Low: Suspicious but could be legitimate
			{regexp.MustCompile(`/\*.*\*/`), "medium", "SQL block comment"},
		},
		xssPatterns: []threatPattern{
			// Critical: Direct script execution
			{regexp.MustCompile(`(?i)<script[^>]*>`), "critical", "script tag"},
			{regexp.MustCompile(`(?i)</script>`), "critical", "script close tag"},
			{regexp.MustCompile(`(?i)javascript:`), "critical", "javascript: protocol"},
			{regexp.MustCompile(`(?i)vbscript:`), "critical", "vbscript: protocol"},
			{regexp.MustCompile(`(?i)data:\s*text/html`), "critical", "data:text/html"},

			// High: Event handlers
			{regexp.MustCompile(`(?i)\bon\w+\s*=`), "high", "event handler"},
			{regexp.MustCompile(`(?i)onerror\s*=`), "high", "onerror handler"},
			{regexp.MustCompile(`(?i)onload\s*=`), "high", "onload handler"},
			{regexp.MustCompile(`(?i)onclick\s*=`), "high", "onclick handler"},
			{regexp.MustCompile(`(?i)onmouseover\s*=`), "high", "onmouseover handler"},

			// High: Dangerous tags
			{regexp.MustCompile(`(?i)<iframe[^>]*>`), "high", "iframe tag"},
			{regexp.MustCompile(`(?i)<object[^>]*>`), "high", "object tag"},
			{regexp.MustCompile(`(?i)<embed[^>]*>`), "high", "embed tag"},
			{regexp.MustCompile(`(?i)<form[^>]*>`), "medium", "form tag"},
			{regexp.MustCompile(`(?i)<input[^>]*>`), "medium", "input tag"},

			// Medium: Style-based XSS
			{regexp.MustCompile(`(?i)expression\s*\(`), "medium", "CSS expression"},
			{regexp.MustCompile(`(?i)url\s*\(\s*['"]?javascript:`), "high", "CSS javascript URL"},
		},
		htmlTagPattern:     regexp.MustCompile(`<[^>]+>`),
		controlCharPattern: regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`),
	}
}

// DetectThreats scans input and returns all detected threats
func (td *ThreatDetector) DetectThreats(input string, fieldName string) []DetectedThreat {
	var threats []DetectedThreat

	// Detect SQL injection
	sqlThreats := td.detectSQLInjection(input, fieldName)
	threats = append(threats, sqlThreats...)

	// Detect XSS
	xssThreats := td.detectXSS(input, fieldName)
	threats = append(threats, xssThreats...)

	// Detect HTML injection
	htmlThreats := td.detectHTMLInjection(input, fieldName)
	threats = append(threats, htmlThreats...)

	// Detect control characters
	controlThreats := td.detectControlChars(input, fieldName)
	threats = append(threats, controlThreats...)

	return threats
}

// detectSQLInjection checks for SQL injection patterns
func (td *ThreatDetector) detectSQLInjection(input string, fieldName string) []DetectedThreat {
	var threats []DetectedThreat

	for _, pattern := range td.sqlPatterns {
		matches := pattern.pattern.FindAllString(input, -1)
		for _, match := range matches {
			threats = append(threats, DetectedThreat{
				Type:           "sql_injection",
				Severity:       pattern.severity,
				FieldName:      fieldName,
				Pattern:        pattern.name,
				MatchedContent: truncateMatch(match, 100),
				Action:         DetermineAction(pattern.severity),
			})
		}
	}

	return threats
}

// detectXSS checks for XSS patterns
func (td *ThreatDetector) detectXSS(input string, fieldName string) []DetectedThreat {
	var threats []DetectedThreat

	for _, pattern := range td.xssPatterns {
		matches := pattern.pattern.FindAllString(input, -1)
		for _, match := range matches {
			threats = append(threats, DetectedThreat{
				Type:           "xss",
				Severity:       pattern.severity,
				FieldName:      fieldName,
				Pattern:        pattern.name,
				MatchedContent: truncateMatch(match, 100),
				Action:         DetermineAction(pattern.severity),
			})
		}
	}

	return threats
}

// detectHTMLInjection checks for HTML tag injection
func (td *ThreatDetector) detectHTMLInjection(input string, fieldName string) []DetectedThreat {
	var threats []DetectedThreat

	matches := td.htmlTagPattern.FindAllString(input, -1)
	for _, match := range matches {
		// Skip if already detected as XSS (script, iframe, etc.)
		lowerMatch := strings.ToLower(match)
		if strings.Contains(lowerMatch, "<script") ||
			strings.Contains(lowerMatch, "<iframe") ||
			strings.Contains(lowerMatch, "<object") ||
			strings.Contains(lowerMatch, "<embed") {
			continue
		}

		severity := "low"
		// Some HTML tags are more concerning
		if strings.Contains(lowerMatch, "<a ") ||
			strings.Contains(lowerMatch, "<img ") ||
			strings.Contains(lowerMatch, "<link ") {
			severity = "medium"
		}

		threats = append(threats, DetectedThreat{
			Type:           "html_injection",
			Severity:       severity,
			FieldName:      fieldName,
			Pattern:        "HTML tag",
			MatchedContent: truncateMatch(match, 100),
			Action:         DetermineAction(severity),
		})
	}

	return threats
}

// detectControlChars checks for control characters
func (td *ThreatDetector) detectControlChars(input string, fieldName string) []DetectedThreat {
	var threats []DetectedThreat

	matches := td.controlCharPattern.FindAllString(input, -1)
	if len(matches) > 0 {
		threats = append(threats, DetectedThreat{
			Type:           "control_chars",
			Severity:       "low",
			FieldName:      fieldName,
			Pattern:        "control characters",
			MatchedContent: "[control characters]",
			Action:         "sanitized",
		})
	}

	return threats
}

// GetHighestSeverity returns the highest severity from a list of threats
func GetHighestSeverity(threats []DetectedThreat) string {
	if len(threats) == 0 {
		return ""
	}

	severityOrder := map[string]int{
		"low":      1,
		"medium":   2,
		"high":     3,
		"critical": 4,
	}

	highest := "low"
	for _, threat := range threats {
		if severityOrder[threat.Severity] > severityOrder[highest] {
			highest = threat.Severity
		}
	}

	return highest
}

// DetermineAction determines the action to take based on threat severity
func DetermineAction(severity string) string {
	switch severity {
	case "critical":
		return "rejected"
	case "high":
		return "isolated"
	case "medium":
		return "isolated"
	default:
		return "sanitized"
	}
}

// truncateMatch truncates a matched string to a maximum length
func truncateMatch(match string, maxLen int) string {
	if len(match) <= maxLen {
		return match
	}
	return match[:maxLen] + "..."
}

// DefaultThreatDetector is the global threat detector instance
var DefaultThreatDetector = NewThreatDetector()
