package validation

import (
	"testing"
)

func TestThreatDetector_DetectThreats(t *testing.T) {
	td := NewThreatDetector()

	tests := []struct {
		name           string
		input          string
		fieldName      string
		expectThreats  bool
		expectedTypes  []string
		minSeverity    string
	}{
		{
			name:          "clean input",
			input:         "This is a normal text",
			fieldName:     "description",
			expectThreats: false,
		},
		{
			name:          "SQL injection - UNION SELECT",
			input:         "test UNION SELECT * FROM users",
			fieldName:     "query",
			expectThreats: true,
			expectedTypes: []string{"sql_injection"},
			minSeverity:   "high",
		},
		{
			name:          "SQL injection - DROP TABLE",
			input:         "'; DROP TABLE users; --",
			fieldName:     "name",
			expectThreats: true,
			expectedTypes: []string{"sql_injection"},
			minSeverity:   "critical",
		},
		{
			name:          "XSS - script tag",
			input:         "<script>alert('xss')</script>",
			fieldName:     "content",
			expectThreats: true,
			expectedTypes: []string{"xss"},
			minSeverity:   "high",
		},
		{
			name:          "XSS - javascript protocol",
			input:         "javascript:alert('xss')",
			fieldName:     "url",
			expectThreats: true,
			expectedTypes: []string{"xss"},
			minSeverity:   "high",
		},
		{
			name:          "XSS - event handler",
			input:         "<img onerror=\"alert('xss')\" src=\"x\">",
			fieldName:     "html",
			expectThreats: true,
			expectedTypes: []string{"xss"},
			minSeverity:   "medium",
		},
		{
			name:          "HTML injection - iframe",
			input:         "<iframe src=\"http://evil.com\"></iframe>",
			fieldName:     "content",
			expectThreats: true,
			expectedTypes: []string{"html_injection"},
			minSeverity:   "medium",
		},
		{
			name:          "Control characters",
			input:         "text with null\x00 byte",
			fieldName:     "data",
			expectThreats: true,
			expectedTypes: []string{"control_chars"},
			minSeverity:   "low",
		},
		{
			name:          "Multiple threats",
			input:         "<script>'; DROP TABLE users; --</script>",
			fieldName:     "payload",
			expectThreats: true,
			expectedTypes: []string{"sql_injection", "xss"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			threats := td.DetectThreats(tt.input, tt.fieldName)

			if tt.expectThreats && len(threats) == 0 {
				t.Errorf("Expected threats but got none for input: %s", tt.input)
				return
			}

			if !tt.expectThreats && len(threats) > 0 {
				t.Errorf("Expected no threats but got %d for input: %s", len(threats), tt.input)
				return
			}

			if tt.expectedTypes != nil {
				for _, expectedType := range tt.expectedTypes {
					found := false
					for _, threat := range threats {
						if threat.Type == expectedType {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected threat type %s not found for input: %s", expectedType, tt.input)
					}
				}
			}

			// Check that field name is correctly set
			for _, threat := range threats {
				if threat.FieldName != tt.fieldName {
					t.Errorf("Expected field name %s but got %s", tt.fieldName, threat.FieldName)
				}
			}
		})
	}
}

func TestGetHighestSeverity(t *testing.T) {
	tests := []struct {
		name     string
		threats  []DetectedThreat
		expected string
	}{
		{
			name:     "empty threats",
			threats:  []DetectedThreat{},
			expected: "", // Returns empty string when no threats
		},
		{
			name: "single low threat",
			threats: []DetectedThreat{
				{Severity: "low"},
			},
			expected: "low",
		},
		{
			name: "single critical threat",
			threats: []DetectedThreat{
				{Severity: "critical"},
			},
			expected: "critical",
		},
		{
			name: "mixed severities - critical highest",
			threats: []DetectedThreat{
				{Severity: "low"},
				{Severity: "medium"},
				{Severity: "critical"},
				{Severity: "high"},
			},
			expected: "critical",
		},
		{
			name: "mixed severities - high highest",
			threats: []DetectedThreat{
				{Severity: "low"},
				{Severity: "high"},
				{Severity: "medium"},
			},
			expected: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetHighestSeverity(tt.threats)
			if result != tt.expected {
				t.Errorf("Expected severity %s but got %s", tt.expected, result)
			}
		})
	}
}

func TestDetermineAction(t *testing.T) {
	tests := []struct {
		severity       string
		expectedAction string
	}{
		{"low", "sanitized"},
		{"medium", "isolated"},
		{"high", "isolated"},
		{"critical", "rejected"},
		{"none", "sanitized"},
		{"unknown", "sanitized"},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			action := DetermineAction(tt.severity)
			if action != tt.expectedAction {
				t.Errorf("For severity %s: expected action %s but got %s",
					tt.severity, tt.expectedAction, action)
			}
		})
	}
}

func TestDetectedThreat_MatchedContent(t *testing.T) {
	td := NewThreatDetector()

	// Test that matched content is captured correctly
	input := "hello UNION SELECT password FROM users world"
	threats := td.DetectThreats(input, "test")

	if len(threats) == 0 {
		t.Fatal("Expected at least one threat")
	}

	// The matched content should contain the dangerous pattern
	found := false
	for _, threat := range threats {
		if threat.MatchedContent != "" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected matched content to be captured")
	}
}
