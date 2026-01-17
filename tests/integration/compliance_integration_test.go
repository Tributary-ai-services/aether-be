package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/tests/utils"
)

// ComplianceIntegrationTestSuite tests compliance scanning functionality
type ComplianceIntegrationTestSuite struct {
	suite.Suite
	config            *utils.TestConfig
	complianceService *services.ComplianceService
	log               *logger.Logger
}

// SetupSuite prepares the test suite
func (suite *ComplianceIntegrationTestSuite) SetupSuite() {
	suite.config = utils.SetupTestEnvironment(suite.T())

	// Initialize logger
	var err error
	suite.log, err = logger.NewDefault()
	require.NoError(suite.T(), err)

	// Initialize compliance configuration
	complianceConfig := &config.ComplianceConfig{
		Enabled:                   true,
		GDPREnabled:               true,
		HIPAAEnabled:              true,
		CCPAEnabled:               false,
		PIIDetectionEnabled:       true,
		DataClassificationEnabled: true,
		BatchSize:                 10,
		ScanInterval:              30,
		RetentionDays:             365,
		MaskPII:                   true,
		EncryptSensitive:          true,
	}

	// Initialize compliance service
	suite.complianceService = services.NewComplianceService(complianceConfig, suite.log)
}

// TestPIIDetection tests PII detection functionality
func (suite *ComplianceIntegrationTestSuite) TestPIIDetection() {
	ctx := context.Background()

	testCases := []struct {
		name        string
		content     string
		expectPII   bool
		expectedTypes []string
	}{
		{
			name:        "Email detection",
			content:     "Please contact John Doe at john.doe@example.com for more information.",
			expectPII:   true,
			expectedTypes: []string{"email"},
		},
		{
			name:        "SSN detection",
			content:     "John's SSN is 123-45-6789 and he was born in 1985.",
			expectPII:   true,
			expectedTypes: []string{"ssn"},
		},
		{
			name:        "Phone number detection",
			content:     "Call me at 555-123-4567 or use the backup number 555.987.6543.",
			expectPII:   true,
			expectedTypes: []string{"phone"},
		},
		{
			name:        "Credit card detection",
			content:     "Credit card number: 4532-1234-5678-9012 expires 12/25.",
			expectPII:   true,
			expectedTypes: []string{"credit_card"},
		},
		{
			name:        "Multiple PII types",
			content:     "Customer: john.doe@example.com, SSN: 123-45-6789, Phone: 555-123-4567",
			expectPII:   true,
			expectedTypes: []string{"email", "ssn", "phone"},
		},
		{
			name:        "No PII content",
			content:     "This document contains general information about our products and services.",
			expectPII:   false,
			expectedTypes: []string{},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			// Create test chunk
			chunk := &models.Chunk{
				ID:      "test-chunk-" + tc.name,
				Content: tc.content,
				Metadata: map[string]interface{}{
					"test_case": tc.name,
				},
			}

			// Run compliance scan
			result, err := suite.complianceService.ScanChunk(ctx, chunk)
			require.NoError(suite.T(), err, "Compliance scan should not fail")

			// Validate PII detection
			assert.Equal(suite.T(), tc.expectPII, result.PIIDetected,
				"PII detection should match expectation for: %s", tc.name)

			if tc.expectPII {
				assert.NotEmpty(suite.T(), result.PIIDetails, "PII details should be provided when PII is detected")

				// Check detected PII types
				detectedTypes := make(map[string]bool)
				for _, match := range result.PIIDetails {
					detectedTypes[match.Type] = true
				}

				for _, expectedType := range tc.expectedTypes {
					assert.True(suite.T(), detectedTypes[expectedType],
						"Expected PII type '%s' should be detected", expectedType)
				}

				// Validate PII matches have required fields
				for _, match := range result.PIIDetails {
					assert.NotEmpty(suite.T(), match.Type, "PII match should have type")
					assert.NotEmpty(suite.T(), match.Value, "PII match should have masked value")
					assert.True(suite.T(), match.Confidence > 0, "PII match should have confidence > 0")
					assert.Contains(suite.T(), match.Value, "*", "PII value should be masked")
				}
			}
		})
	}
}

// TestGDPRCompliance tests GDPR compliance detection
func (suite *ComplianceIntegrationTestSuite) TestGDPRCompliance() {
	ctx := context.Background()

	testCases := []struct {
		name            string
		content         string
		expectedFlags   []string
		expectedLevel   string
	}{
		{
			name:            "Personal data indicators",
			content:         "User profile contains name, email address, and birth date information.",
			expectedFlags:   []string{"GDPR_PERSONAL_DATA"},
			expectedLevel:   "internal",
		},
		{
			name:            "Sensitive personal data",
			content:         "Medical records show patient has genetic predisposition to diabetes.",
			expectedFlags:   []string{"GDPR_PERSONAL_DATA", "GDPR_SENSITIVE_DATA"},
			expectedLevel:   "restricted",
		},
		{
			name:            "Political information",
			content:         "Voter registration shows political party affiliation and voting history.",
			expectedFlags:   []string{"GDPR_SENSITIVE_DATA"},
			expectedLevel:   "public",
		},
		{
			name:            "General business content",
			content:         "This quarterly report shows increased revenue and market expansion.",
			expectedFlags:   []string{},
			expectedLevel:   "public",
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			chunk := &models.Chunk{
				ID:      "test-gdpr-" + tc.name,
				Content: tc.content,
			}

			result, err := suite.complianceService.ScanChunk(ctx, chunk)
			require.NoError(suite.T(), err)

			// Check for expected GDPR flags
			flagsFound := make(map[string]bool)
			for _, flag := range result.ComplianceFlags {
				flagsFound[flag] = true
			}

			for _, expectedFlag := range tc.expectedFlags {
				assert.True(suite.T(), flagsFound[expectedFlag],
					"Expected GDPR flag '%s' should be detected", expectedFlag)
			}

			// Validate data classification level
			assert.NotEmpty(suite.T(), result.DataClassification.Level,
				"Data classification level should be set")
		})
	}
}

// TestHIPAACompliance tests HIPAA compliance detection
func (suite *ComplianceIntegrationTestSuite) TestHIPAACompliance() {
	ctx := context.Background()

	testCases := []struct {
		name          string
		content       string
		expectedFlags []string
	}{
		{
			name:          "Medical information",
			content:       "Patient John Smith diagnosed with diabetes, prescribed medication.",
			expectedFlags: []string{"PHI_DETECTED", "MEDICAL_DATA"},
		},
		{
			name:          "Health records",
			content:       "MRI scan results show no abnormalities in brain tissue.",
			expectedFlags: []string{"MEDICAL_DATA"},
		},
		{
			name:          "Medical record number",
			content:       "Please reference medical record number MRN123456 for lab results.",
			expectedFlags: []string{"HIPAA_IDENTIFIER"},
		},
		{
			name:          "General health discussion",
			content:       "The importance of regular exercise and healthy diet for wellness.",
			expectedFlags: []string{"MEDICAL_DATA"},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			chunk := &models.Chunk{
				ID:      "test-hipaa-" + tc.name,
				Content: tc.content,
			}

			result, err := suite.complianceService.ScanChunk(ctx, chunk)
			require.NoError(suite.T(), err)

			// Check for expected HIPAA flags
			flagsFound := make(map[string]bool)
			for _, flag := range result.ComplianceFlags {
				flagsFound[flag] = true
			}

			for _, expectedFlag := range tc.expectedFlags {
				assert.True(suite.T(), flagsFound[expectedFlag],
					"Expected HIPAA flag '%s' should be detected", expectedFlag)
			}

			// If PHI detected, should be classified as restricted
			if flagsFound["PHI_DETECTED"] {
				assert.Contains(suite.T(), result.DataClassification.Regulations, "HIPAA",
					"PHI should trigger HIPAA regulation classification")
			}
		})
	}
}

// TestDataClassification tests data classification functionality
func (suite *ComplianceIntegrationTestSuite) TestDataClassification() {
	ctx := context.Background()

	testCases := []struct {
		name               string
		content            string
		expectedLevel      string
		expectedCategories []string
		expectedRegulations []string
	}{
		{
			name:               "Financial data",
			content:            "Credit card payment processed for $1,250.00 to account 1234-5678-9012.",
			expectedLevel:      "confidential",
			expectedCategories: []string{"financial"},
			expectedRegulations: []string{"PCI-DSS"},
		},
		{
			name:               "Health information",
			content:            "Patient medical history shows diabetes diagnosis and treatment plan.",
			expectedLevel:      "restricted",
			expectedCategories: []string{"health", "personal"},
			expectedRegulations: []string{"HIPAA", "GDPR"},
		},
		{
			name:               "Personal information",
			content:            "Customer contact details: John Smith, email john@example.com, phone 555-1234.",
			expectedLevel:      "internal",
			expectedCategories: []string{"personal"},
			expectedRegulations: []string{"GDPR"},
		},
		{
			name:               "Public information",
			content:            "Company announces new product launch and market expansion plans.",
			expectedLevel:      "public",
			expectedCategories: []string{},
			expectedRegulations: []string{},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			chunk := &models.Chunk{
				ID:      "test-classification-" + tc.name,
				Content: tc.content,
			}

			result, err := suite.complianceService.ScanChunk(ctx, chunk)
			require.NoError(suite.T(), err)

			classification := result.DataClassification

			// Check classification level
			assert.Equal(suite.T(), tc.expectedLevel, classification.Level,
				"Data classification level should match expected")

			// Check categories
			if len(tc.expectedCategories) > 0 {
				for _, expectedCategory := range tc.expectedCategories {
					assert.Contains(suite.T(), classification.Categories, expectedCategory,
						"Classification should include category: %s", expectedCategory)
				}
			}

			// Check regulations
			if len(tc.expectedRegulations) > 0 {
				for _, expectedRegulation := range tc.expectedRegulations {
					assert.Contains(suite.T(), classification.Regulations, expectedRegulation,
						"Classification should include regulation: %s", expectedRegulation)
				}
			}

			// Validate retention days based on data type
			assert.True(suite.T(), classification.RetentionDays > 0,
				"Retention days should be set")
		})
	}
}

// TestBatchCompliance tests batch compliance scanning
func (suite *ComplianceIntegrationTestSuite) TestBatchCompliance() {
	ctx := context.Background()

	// Create test chunks with different compliance characteristics
	chunks := []*models.Chunk{
		{
			ID:      "batch-chunk-1",
			Content: "Customer email: john.doe@example.com with order details.",
		},
		{
			ID:      "batch-chunk-2", 
			Content: "Patient medical record shows diabetes treatment progress.",
		},
		{
			ID:      "batch-chunk-3",
			Content: "General product information and specifications.",
		},
		{
			ID:      "batch-chunk-4",
			Content: "Credit card transaction: 4532-1234-5678-9012 for $500.",
		},
	}

	// Run batch compliance scan
	results, err := suite.complianceService.BatchScanChunks(ctx, chunks)
	require.NoError(suite.T(), err, "Batch compliance scan should not fail")

	// Validate results
	assert.Equal(suite.T(), len(chunks), len(results), "Should return result for each chunk")

	// Check specific results
	piiDetectedCount := 0
	complianceViolations := 0

	for i, result := range results {
		chunk := chunks[i]

		// Validate basic result structure
		assert.Equal(suite.T(), chunk.ID, result.ChunkID, "Result should match chunk ID")
		assert.NotZero(suite.T(), result.ScanTimestamp, "Scan timestamp should be set")
		assert.NotEmpty(suite.T(), result.RiskLevel, "Risk level should be assigned")

		// Count PII detections and violations
		if result.PIIDetected {
			piiDetectedCount++
		}
		if len(result.ComplianceFlags) > 0 {
			complianceViolations++
		}
	}

	// Validate expected patterns
	assert.True(suite.T(), piiDetectedCount >= 2, "Should detect PII in email and credit card chunks")
	assert.True(suite.T(), complianceViolations >= 2, "Should detect compliance issues in multiple chunks")

	suite.T().Logf("Batch scan results: %d PII detections, %d compliance violations",
		piiDetectedCount, complianceViolations)
}

// TestCompliancePerformance tests compliance scanning performance
func (suite *ComplianceIntegrationTestSuite) TestCompliancePerformance() {
	ctx := context.Background()

	// Create test content
	testContent := "Customer information: John Smith, email john.smith@company.com, " +
		"SSN 123-45-6789, phone 555-123-4567. Medical history includes diabetes " +
		"treatment and recent blood test results. Credit card on file: 4532-1234-5678-9012."

	chunk := &models.Chunk{
		ID:      "performance-test-chunk",
		Content: testContent,
	}

	// Measure single scan performance
	start := time.Now()
	result, err := suite.complianceService.ScanChunk(ctx, chunk)
	singleScanDuration := time.Since(start)

	require.NoError(suite.T(), err)
	assert.True(suite.T(), singleScanDuration < 1*time.Second,
		"Single compliance scan should complete quickly")

	// Validate comprehensive scanning occurred
	assert.True(suite.T(), result.PIIDetected, "Should detect PII in test content")
	assert.True(suite.T(), len(result.PIIDetails) >= 3, "Should detect multiple PII types")
	assert.True(suite.T(), len(result.ComplianceFlags) > 0, "Should detect compliance issues")
	assert.NotEmpty(suite.T(), result.RequiredActions, "Should provide required actions")

	suite.T().Logf("Single compliance scan duration: %v", singleScanDuration)
	suite.T().Logf("PII detected: %d items", len(result.PIIDetails))
	suite.T().Logf("Compliance flags: %v", result.ComplianceFlags)
}

// TestComplianceIntegration runs the compliance integration test suite
func TestComplianceIntegration(t *testing.T) {
	suite.Run(t, new(ComplianceIntegrationTestSuite))
}