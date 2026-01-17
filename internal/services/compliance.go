package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// ComplianceService handles data compliance scanning and validation
type ComplianceService struct {
	log              *logger.Logger
	config           *config.ComplianceConfig
	piiDetector      *PIIDetector
	gdprScanner      *GDPRScanner
	hipaaScanner     *HIPAAScanner
	classificationEngine *DataClassificationEngine
}

// ComplianceResult represents the result of compliance scanning
type ComplianceResult struct {
	ChunkID           string                 `json:"chunk_id"`
	PIIDetected       bool                   `json:"pii_detected"`
	PIIDetails        []PIIMatch             `json:"pii_details,omitempty"`
	ComplianceFlags   []string               `json:"compliance_flags"`
	DataClassification DataClassification     `json:"data_classification"`
	RiskLevel         string                 `json:"risk_level"`
	RequiredActions   []string               `json:"required_actions,omitempty"`
	ScanTimestamp     time.Time              `json:"scan_timestamp"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// PIIMatch represents a detected PII instance
type PIIMatch struct {
	Type       string  `json:"type"`        // email, ssn, phone, etc.
	Value      string  `json:"value"`       // Masked/redacted value
	Position   int     `json:"position"`    // Position in text
	Confidence float64 `json:"confidence"`  // Detection confidence (0-1)
	Context    string  `json:"context"`     // Surrounding context
}

// DataClassification represents data classification results
type DataClassification struct {
	Level         string   `json:"level"`         // public, internal, confidential, restricted
	Categories    []string `json:"categories"`    // personal, financial, health, etc.
	Regulations   []string `json:"regulations"`   // GDPR, HIPAA, CCPA, etc.
	RetentionDays int      `json:"retention_days"` // Data retention requirement
}

// PIIDetector handles PII detection using regex patterns and ML
type PIIDetector struct {
	patterns map[string]*regexp.Regexp
	log      *logger.Logger
}

// GDPRScanner handles GDPR compliance checks
type GDPRScanner struct {
	personalDataPatterns []string
	sensitiveCategories  []string
	log                  *logger.Logger
}

// HIPAAScanner handles HIPAA compliance checks
type HIPAAScanner struct {
	phiPatterns     []string
	medicalTerms    []string
	identifierTypes []string
	log             *logger.Logger
}

// DataClassificationEngine classifies data based on content and context
type DataClassificationEngine struct {
	classificationRules map[string][]ClassificationRule
	log                 *logger.Logger
}

// ClassificationRule defines rules for data classification
type ClassificationRule struct {
	Pattern     string   `json:"pattern"`
	Keywords    []string `json:"keywords"`
	Level       string   `json:"level"`
	Categories  []string `json:"categories"`
	Regulations []string `json:"regulations"`
}

// NewComplianceService creates a new compliance service
func NewComplianceService(config *config.ComplianceConfig, log *logger.Logger) *ComplianceService {
	return &ComplianceService{
		log:                  log,
		config:               config,
		piiDetector:          NewPIIDetector(log),
		gdprScanner:          NewGDPRScanner(log),
		hipaaScanner:         NewHIPAAScanner(log),
		classificationEngine: NewDataClassificationEngine(log),
	}
}

// ScanChunk performs comprehensive compliance scanning on a chunk
func (s *ComplianceService) ScanChunk(ctx context.Context, chunk *models.Chunk) (*ComplianceResult, error) {
	start := time.Now()

	result := &ComplianceResult{
		ChunkID:         chunk.ID,
		PIIDetected:     false,
		PIIDetails:      []PIIMatch{},
		ComplianceFlags: []string{},
		ScanTimestamp:   time.Now(),
		Metadata:        make(map[string]interface{}),
	}

	// 1. PII Detection
	piiMatches, err := s.piiDetector.DetectPII(chunk.Content)
	if err != nil {
		s.log.Error("PII detection failed", zap.String("chunk_id", chunk.ID), zap.Error(err))
		return nil, fmt.Errorf("PII detection failed: %w", err)
	}

	if len(piiMatches) > 0 {
		result.PIIDetected = true
		result.PIIDetails = piiMatches
		result.ComplianceFlags = append(result.ComplianceFlags, "PII_DETECTED")
	}

	// 2. GDPR Compliance Check
	if s.config.GDPREnabled {
		gdprFlags := s.gdprScanner.ScanForGDPRData(chunk.Content)
		result.ComplianceFlags = append(result.ComplianceFlags, gdprFlags...)
	}

	// 3. HIPAA Compliance Check
	if s.config.HIPAAEnabled {
		hipaaFlags := s.hipaaScanner.ScanForPHI(chunk.Content)
		result.ComplianceFlags = append(result.ComplianceFlags, hipaaFlags...)
	}

	// 4. Data Classification
	classification := s.classificationEngine.ClassifyData(chunk.Content, chunk.Metadata)
	result.DataClassification = classification

	// 5. Risk Assessment
	result.RiskLevel = s.assessRiskLevel(result)

	// 6. Required Actions
	result.RequiredActions = s.determineRequiredActions(result)

	// Add performance metadata
	result.Metadata["scan_duration_ms"] = time.Since(start).Milliseconds()
	result.Metadata["scanner_version"] = "1.0.0"

	s.log.Info("Compliance scan completed",
		zap.String("chunk_id", chunk.ID),
		zap.Bool("pii_detected", result.PIIDetected),
		zap.Strings("compliance_flags", result.ComplianceFlags),
		zap.String("risk_level", result.RiskLevel),
		zap.Duration("duration", time.Since(start)),
	)

	return result, nil
}

// BatchScanChunks performs compliance scanning on multiple chunks
func (s *ComplianceService) BatchScanChunks(ctx context.Context, chunks []*models.Chunk) ([]*ComplianceResult, error) {
	results := make([]*ComplianceResult, len(chunks))
	
	for i, chunk := range chunks {
		result, err := s.ScanChunk(ctx, chunk)
		if err != nil {
			s.log.Error("Batch scan failed for chunk",
				zap.String("chunk_id", chunk.ID),
				zap.Error(err),
			)
			// Continue with other chunks, don't fail entire batch
			results[i] = &ComplianceResult{
				ChunkID:       chunk.ID,
				RiskLevel:     "unknown",
				ScanTimestamp: time.Now(),
				Metadata: map[string]interface{}{
					"scan_error": err.Error(),
				},
			}
			continue
		}
		results[i] = result
	}

	return results, nil
}

// assessRiskLevel determines the overall risk level based on scan results
func (s *ComplianceService) assessRiskLevel(result *ComplianceResult) string {
	if len(result.ComplianceFlags) == 0 {
		return "low"
	}

	highRiskFlags := []string{"PII_DETECTED", "PHI_DETECTED", "GDPR_PERSONAL_DATA", "FINANCIAL_DATA"}
	for _, flag := range result.ComplianceFlags {
		for _, highRisk := range highRiskFlags {
			if flag == highRisk {
				return "high"
			}
		}
	}

	mediumRiskFlags := []string{"SENSITIVE_DATA", "PERSONAL_IDENTIFIERS"}
	for _, flag := range result.ComplianceFlags {
		for _, mediumRisk := range mediumRiskFlags {
			if flag == mediumRisk {
				return "medium"
			}
		}
	}

	return "low"
}

// determineRequiredActions suggests actions based on compliance findings
func (s *ComplianceService) determineRequiredActions(result *ComplianceResult) []string {
	actions := []string{}

	if result.PIIDetected {
		actions = append(actions, "MASK_PII", "REVIEW_RETENTION_POLICY")
	}

	for _, flag := range result.ComplianceFlags {
		switch flag {
		case "GDPR_PERSONAL_DATA":
			actions = append(actions, "ENSURE_CONSENT", "DATA_MINIMIZATION")
		case "PHI_DETECTED":
			actions = append(actions, "HIPAA_SAFEGUARDS", "ACCESS_CONTROLS")
		case "FINANCIAL_DATA":
			actions = append(actions, "ENCRYPTION_REQUIRED", "AUDIT_TRAIL")
		}
	}

	return actions
}

// NewPIIDetector creates a new PII detector
func NewPIIDetector(log *logger.Logger) *PIIDetector {
	patterns := map[string]*regexp.Regexp{
		"email":        regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),
		"ssn":          regexp.MustCompile(`\b\d{3}-?\d{2}-?\d{4}\b`),
		"phone":        regexp.MustCompile(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`),
		"credit_card":  regexp.MustCompile(`\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`),
		"ip_address":   regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
		"passport":     regexp.MustCompile(`\b[A-Z]{1,2}\d{6,9}\b`),
		"driver_license": regexp.MustCompile(`\b[A-Z]{1,2}\d{6,8}\b`),
	}

	return &PIIDetector{
		patterns: patterns,
		log:      log,
	}
}

// DetectPII detects PII in the given text
func (d *PIIDetector) DetectPII(text string) ([]PIIMatch, error) {
	matches := []PIIMatch{}

	for piiType, pattern := range d.patterns {
		found := pattern.FindAllStringIndex(text, -1)
		for _, match := range found {
			start, end := match[0], match[1]
			value := text[start:end]
			
			// Mask the value for logging/storage
			maskedValue := d.maskValue(value, piiType)
			
			// Get context (10 characters before and after)
			contextStart := maxInt(0, start-10)
			contextEnd := minInt(len(text), end+10)
			context := text[contextStart:contextEnd]

			matches = append(matches, PIIMatch{
				Type:       piiType,
				Value:      maskedValue,
				Position:   start,
				Confidence: d.calculateConfidence(value, piiType),
				Context:    context,
			})
		}
	}

	return matches, nil
}

// maskValue masks PII values for safe storage/logging
func (d *PIIDetector) maskValue(value, piiType string) string {
	switch piiType {
	case "email":
		parts := strings.Split(value, "@")
		if len(parts) == 2 {
			return parts[0][:1] + "***@" + parts[1]
		}
	case "ssn":
		if len(value) >= 4 {
			return "***-**-" + value[len(value)-4:]
		}
	case "phone":
		if len(value) >= 4 {
			return "***-***-" + value[len(value)-4:]
		}
	case "credit_card":
		if len(value) >= 4 {
			return "****-****-****-" + value[len(value)-4:]
		}
	}
	return "***"
}

// calculateConfidence calculates detection confidence
func (d *PIIDetector) calculateConfidence(value, piiType string) float64 {
	// Basic confidence calculation - can be enhanced with ML models
	switch piiType {
	case "email":
		if strings.Contains(value, "@") && strings.Contains(value, ".") {
			return 0.95
		}
	case "ssn":
		if len(strings.ReplaceAll(value, "-", "")) == 9 {
			return 0.90
		}
	case "phone":
		digits := strings.ReplaceAll(strings.ReplaceAll(value, "-", ""), ".", "")
		if len(digits) == 10 {
			return 0.85
		}
	}
	return 0.70
}

// NewGDPRScanner creates a new GDPR scanner
func NewGDPRScanner(log *logger.Logger) *GDPRScanner {
	return &GDPRScanner{
		personalDataPatterns: []string{
			"name", "address", "email", "phone", "birth", "nationality",
			"identification", "location", "online identifier", "IP address",
		},
		sensitiveCategories: []string{
			"racial", "ethnic", "political", "religious", "trade union",
			"genetic", "biometric", "health", "sex life", "sexual orientation",
		},
		log: log,
	}
}

// ScanForGDPRData scans text for GDPR-relevant personal data
func (g *GDPRScanner) ScanForGDPRData(text string) []string {
	flags := []string{}
	lowerText := strings.ToLower(text)

	// Check for personal data indicators
	for _, pattern := range g.personalDataPatterns {
		if strings.Contains(lowerText, pattern) {
			flags = append(flags, "GDPR_PERSONAL_DATA")
			break
		}
	}

	// Check for sensitive categories
	for _, category := range g.sensitiveCategories {
		if strings.Contains(lowerText, category) {
			flags = append(flags, "GDPR_SENSITIVE_DATA")
			break
		}
	}

	return flags
}

// NewHIPAAScanner creates a new HIPAA scanner
func NewHIPAAScanner(log *logger.Logger) *HIPAAScanner {
	return &HIPAAScanner{
		phiPatterns: []string{
			"patient", "medical", "health", "diagnosis", "treatment",
			"medication", "prescription", "doctor", "physician", "hospital",
		},
		medicalTerms: []string{
			"blood pressure", "diabetes", "cancer", "surgery", "therapy",
			"MRI", "CT scan", "x-ray", "lab results", "medical record",
		},
		identifierTypes: []string{
			"medical record number", "health plan", "account number",
			"certificate number", "device identifier", "biometric identifier",
		},
		log: log,
	}
}

// ScanForPHI scans text for Protected Health Information
func (h *HIPAAScanner) ScanForPHI(text string) []string {
	flags := []string{}
	lowerText := strings.ToLower(text)

	// Check for PHI indicators
	for _, pattern := range h.phiPatterns {
		if strings.Contains(lowerText, pattern) {
			flags = append(flags, "PHI_DETECTED")
			break
		}
	}

	// Check for medical terms
	for _, term := range h.medicalTerms {
		if strings.Contains(lowerText, term) {
			flags = append(flags, "MEDICAL_DATA")
			break
		}
	}

	// Check for HIPAA identifiers
	for _, identifier := range h.identifierTypes {
		if strings.Contains(lowerText, identifier) {
			flags = append(flags, "HIPAA_IDENTIFIER")
			break
		}
	}

	return flags
}

// NewDataClassificationEngine creates a new data classification engine
func NewDataClassificationEngine(log *logger.Logger) *DataClassificationEngine {
	rules := map[string][]ClassificationRule{
		"financial": {
			{
				Keywords:    []string{"credit card", "bank account", "payment", "invoice"},
				Level:       "confidential",
				Categories:  []string{"financial"},
				Regulations: []string{"PCI-DSS", "SOX"},
			},
		},
		"health": {
			{
				Keywords:    []string{"medical", "health", "patient", "diagnosis"},
				Level:       "restricted",
				Categories:  []string{"health", "personal"},
				Regulations: []string{"HIPAA", "GDPR"},
			},
		},
		"personal": {
			{
				Keywords:    []string{"name", "address", "email", "phone"},
				Level:       "internal",
				Categories:  []string{"personal"},
				Regulations: []string{"GDPR", "CCPA"},
			},
		},
	}

	return &DataClassificationEngine{
		classificationRules: rules,
		log:                 log,
	}
}

// ClassifyData classifies data based on content and context
func (e *DataClassificationEngine) ClassifyData(content string, metadata map[string]interface{}) DataClassification {
	lowerContent := strings.ToLower(content)
	
	classification := DataClassification{
		Level:         "public",
		Categories:    []string{},
		Regulations:   []string{},
		RetentionDays: 365, // Default retention
	}

	// Apply classification rules
	for category, rules := range e.classificationRules {
		for _, rule := range rules {
			matched := false
			for _, keyword := range rule.Keywords {
				if strings.Contains(lowerContent, keyword) {
					matched = true
					break
				}
			}

			if matched {
				// Upgrade classification level if needed
				if e.isHigherLevel(rule.Level, classification.Level) {
					classification.Level = rule.Level
				}
				
				// Add categories and regulations
				classification.Categories = append(classification.Categories, rule.Categories...)
				classification.Regulations = append(classification.Regulations, rule.Regulations...)
				
				// Adjust retention based on category
				if category == "health" {
					classification.RetentionDays = 2555 // 7 years for health data
				} else if category == "financial" {
					classification.RetentionDays = 2190 // 6 years for financial data
				}
			}
		}
	}

	// Remove duplicates
	classification.Categories = removeDuplicates(classification.Categories)
	classification.Regulations = removeDuplicates(classification.Regulations)

	return classification
}

// isHigherLevel checks if one classification level is higher than another
func (e *DataClassificationEngine) isHigherLevel(level1, level2 string) bool {
	levels := map[string]int{
		"public":       0,
		"internal":     1,
		"confidential": 2,
		"restricted":   3,
	}
	return levels[level1] > levels[level2]
}

// Helper functions
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func removeDuplicates(slice []string) []string {
	keys := make(map[string]bool)
	result := []string{}
	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}
	return result
}