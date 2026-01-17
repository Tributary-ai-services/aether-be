package services_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// TestNotebookComplianceOptions tests creating and updating notebook compliance settings
func TestNotebookComplianceOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Neo4j integration test")
	}

	// Setup
	ctx := context.Background()
	log := logger.NewLogger(logger.Config{Level: "debug", Format: "json"})

	// Neo4j configuration
	neo4jCfg := &config.DatabaseConfig{
		Neo4jURI:      "bolt://localhost:7687",
		Neo4jUsername: "neo4j",
		Neo4jPassword: "password",
		Neo4jDatabase: "neo4j",
	}

	// Initialize Neo4j
	neo4jClient, err := database.NewNeo4jClient(neo4jCfg, log)
	require.NoError(t, err, "Failed to create Neo4j client")
	defer neo4jClient.Close()

	// Initialize services
	redisClient := &mockRedisClient{} // Using mock from document_count_test.go
	notebookService := services.NewNotebookService(neo4jClient, redisClient, log)

	// Test data
	userID := "test_user_compliance_123"
	spaceContext := &models.SpaceContext{
		SpaceType:   models.SpaceTypeOrganization,
		SpaceID:     "space_compliance_test",
		TenantID:    "tenant_compliance_test",
		UserRole:    "admin",
		Permissions: []string{"read", "write", "delete", "manage_compliance"},
	}

	var notebookID string

	t.Run("Create notebook with compliance options", func(t *testing.T) {
		// Define compliance settings
		complianceSettings := map[string]interface{}{
			"hipaa_compliant": true,
			"pii_detection": map[string]interface{}{
				"enabled": true,
				"scan_on_upload": true,
				"sensitivity_level": "high",
			},
			"audit_scoring": map[string]interface{}{
				"enabled": true,
				"level": "HIGH",
				"log_all_access": true,
			},
			"data_retention": map[string]interface{}{
				"years": 7,
				"auto_delete": false,
				"archive_after_days": 365,
			},
			"encryption": map[string]interface{}{
				"at_rest": true,
				"in_transit": true,
				"algorithm": "AES-256",
			},
			"access_control": map[string]interface{}{
				"require_mfa": true,
				"ip_whitelist": []string{"10.0.0.0/8", "192.168.0.0/16"},
			},
		}

		// Create notebook with compliance settings
		notebookReq := models.NotebookCreateRequest{
			Name:        "HIPAA Compliant Research Notebook",
			Description: "Notebook with strict compliance requirements",
			Type:        "medical_research",
			Tags:        []string{"hipaa", "compliance", "medical"},
			Metadata: map[string]interface{}{
				"compliance": complianceSettings,
				"department": "Medical Research",
				"project_id": "MED-2025-001",
			},
		}

		notebook, err := notebookService.CreateNotebook(ctx, notebookReq, userID, spaceContext)
		assert.NoError(t, err, "Failed to create notebook with compliance settings")
		notebookID = notebook.ID

		// Verify compliance settings were saved
		assert.NotNil(t, notebook.Metadata["compliance"], "Compliance settings should be present")
		
		compliance, ok := notebook.Metadata["compliance"].(map[string]interface{})
		assert.True(t, ok, "Compliance should be a map")
		assert.Equal(t, true, compliance["hipaa_compliant"], "HIPAA compliance should be enabled")
		
		// Check PII detection settings
		piiSettings, ok := compliance["pii_detection"].(map[string]interface{})
		assert.True(t, ok, "PII detection settings should be present")
		assert.Equal(t, true, piiSettings["enabled"], "PII detection should be enabled")
		assert.Equal(t, "high", piiSettings["sensitivity_level"], "Sensitivity level should be high")

		// Check audit settings
		auditSettings, ok := compliance["audit_scoring"].(map[string]interface{})
		assert.True(t, ok, "Audit settings should be present")
		assert.Equal(t, "HIGH", auditSettings["level"], "Audit level should be HIGH")

		t.Logf("Created notebook %s with compliance settings", notebookID)
	})

	t.Run("Update notebook compliance options", func(t *testing.T) {
		// Update compliance settings
		updatedCompliance := map[string]interface{}{
			"hipaa_compliant": true, // Keep HIPAA
			"pii_detection": map[string]interface{}{
				"enabled": true,
				"scan_on_upload": true,
				"sensitivity_level": "medium", // Changed from high
				"redact_in_preview": true, // New setting
			},
			"audit_scoring": map[string]interface{}{
				"enabled": true,
				"level": "MEDIUM", // Changed from HIGH
				"log_all_access": false, // Changed from true
			},
			"data_retention": map[string]interface{}{
				"years": 10, // Changed from 7
				"auto_delete": true, // Changed from false
				"archive_after_days": 730, // Changed from 365
			},
			"gdpr_compliant": true, // New compliance requirement
		}

		// Update notebook
		updateReq := models.NotebookUpdateRequest{
			Name:        "HIPAA & GDPR Compliant Research Notebook", // Updated name
			Description: "Notebook with updated compliance requirements",
			Metadata: map[string]interface{}{
				"compliance": updatedCompliance,
				"department": "Medical Research",
				"project_id": "MED-2025-001",
				"last_compliance_review": "2025-08-29",
			},
		}

		updatedNotebook, err := notebookService.UpdateNotebook(ctx, notebookID, updateReq, userID, spaceContext)
		assert.NoError(t, err, "Failed to update notebook compliance settings")

		// Verify updates were applied
		compliance, ok := updatedNotebook.Metadata["compliance"].(map[string]interface{})
		assert.True(t, ok, "Compliance should be a map")
		assert.Equal(t, true, compliance["hipaa_compliant"], "HIPAA compliance should still be enabled")
		assert.Equal(t, true, compliance["gdpr_compliant"], "GDPR compliance should be added")

		// Check updated PII settings
		piiSettings, ok := compliance["pii_detection"].(map[string]interface{})
		assert.True(t, ok, "PII detection settings should be present")
		assert.Equal(t, "medium", piiSettings["sensitivity_level"], "Sensitivity level should be updated to medium")
		assert.Equal(t, true, piiSettings["redact_in_preview"], "New PII setting should be present")

		// Check updated audit settings
		auditSettings, ok := compliance["audit_scoring"].(map[string]interface{})
		assert.True(t, ok, "Audit settings should be present")
		assert.Equal(t, "MEDIUM", auditSettings["level"], "Audit level should be updated to MEDIUM")
		assert.Equal(t, false, auditSettings["log_all_access"], "Log all access should be false")

		// Check updated retention
		retention, ok := compliance["data_retention"].(map[string]interface{})
		assert.True(t, ok, "Retention settings should be present")
		assert.Equal(t, float64(10), retention["years"], "Retention should be updated to 10 years")
		assert.Equal(t, true, retention["auto_delete"], "Auto delete should be enabled")

		t.Logf("Successfully updated notebook compliance settings")
	})

	t.Run("Verify compliance settings persist after retrieval", func(t *testing.T) {
		// Get notebook again
		notebook, err := notebookService.GetNotebookByID(ctx, notebookID, userID, spaceContext)
		assert.NoError(t, err, "Failed to retrieve notebook")

		// Verify all compliance settings are still present
		compliance, ok := notebook.Metadata["compliance"].(map[string]interface{})
		assert.True(t, ok, "Compliance should be a map")
		assert.Equal(t, true, compliance["hipaa_compliant"], "HIPAA compliance should persist")
		assert.Equal(t, true, compliance["gdpr_compliant"], "GDPR compliance should persist")

		// Verify complex nested settings
		piiSettings, ok := compliance["pii_detection"].(map[string]interface{})
		assert.True(t, ok, "PII detection settings should persist")
		assert.Equal(t, "medium", piiSettings["sensitivity_level"], "PII sensitivity should persist")

		t.Logf("Compliance settings successfully persisted")
	})

	// Cleanup
	t.Cleanup(func() {
		// Delete test notebook
		err := notebookService.DeleteNotebook(ctx, notebookID, userID, spaceContext)
		if err != nil {
			t.Logf("Cleanup failed: %v", err)
		}
	})
}

// TestComplianceValidation tests validation of compliance settings
func TestComplianceValidation(t *testing.T) {
	t.Run("Invalid compliance settings should be rejected", func(t *testing.T) {
		// Test various invalid compliance configurations
		invalidSettings := []map[string]interface{}{
			{
				// Invalid audit level
				"audit_scoring": map[string]interface{}{
					"level": "INVALID_LEVEL",
				},
			},
			{
				// Negative retention years
				"data_retention": map[string]interface{}{
					"years": -1,
				},
			},
			{
				// Invalid sensitivity level
				"pii_detection": map[string]interface{}{
					"sensitivity_level": "super-ultra-high",
				},
			},
		}

		// Each of these should fail validation when implemented
		for i, settings := range invalidSettings {
			t.Logf("Testing invalid setting %d: %v", i+1, settings)
			// TODO: Add validation logic in service layer
		}
	})
}