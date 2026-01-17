package services_test

import (
	"bytes"
	"context"
	"fmt"
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

// TestDiagnosticChecklist runs all diagnostic tests from diag_tests.todo
func TestDiagnosticChecklist(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping comprehensive integration test")
	}

	// Setup logging
	log := logger.NewLogger(logger.Config{Level: "debug", Format: "json"})
	ctx := context.Background()

	// Setup storage service
	storageCfg := &config.Config{
		Storage: config.StorageConfig{
			Provider:         "s3",
			S3Region:         "us-east-1",
			S3Endpoint:       "http://localhost:9000",
			S3AccessKey:      "minioadmin",
			S3SecretKey:      "minioadmin123", 
			S3Bucket:         "aether-storage",
			S3UseSSL:         false,
			S3ForcePathStyle: true,
		},
	}
	
	storageService, err := services.NewS3StorageService(storageCfg, log)
	require.NoError(t, err, "Failed to create storage service")

	// Test data
	tenantID := "tenant_diagnostic_test"
	spaceType := "personal"
	notebookID := "notebook_diag_123"
	documentID := "document_diag_456"
	fileName := "diagnostic-test.pdf"

	// Track what we've tested
	testResults := make(map[string]bool)

	t.Run("☐ Creating folders in MinIO using intended templates", func(t *testing.T) {
		// The folder structure is created automatically when uploading files
		storageKey := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s",
			spaceType, notebookID, documentID, fileName)
		
		fileContent := []byte("Test content for diagnostic folder creation")
		
		storagePath, err := storageService.UploadFileToTenantBucket(ctx, tenantID, storageKey, fileContent, "application/pdf")
		assert.NoError(t, err, "Failed to create folder structure and upload file")
		assert.NotEmpty(t, storagePath, "Storage path should not be empty")
		
		t.Logf("✓ Created folder structure: %s", storagePath)
		testResults["create_folders"] = true
	})

	t.Run("☐ Uploading file to the folder", func(t *testing.T) {
		// Already done in previous test, let's upload another file
		anotherFile := "diagnostic-test2.txt"
		storageKey := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s",
			spaceType, notebookID, documentID, anotherFile)
		
		fileContent := []byte("Another test file for upload verification")
		
		storagePath, err := storageService.UploadFileToTenantBucket(ctx, tenantID, storageKey, fileContent, "text/plain")
		assert.NoError(t, err, "Failed to upload additional file")
		
		// Verify file exists
		exists, err := storageService.FileExists(ctx, storagePath)
		assert.NoError(t, err, "Failed to check file existence")
		assert.True(t, exists, "Uploaded file should exist")
		
		t.Logf("✓ Uploaded file successfully: %s", anotherFile)
		testResults["upload_file"] = true
	})

	t.Run("☐ Updating the same file in the folder", func(t *testing.T) {
		// Update the first file with new content
		storageKey := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s",
			spaceType, notebookID, documentID, fileName)
		
		updatedContent := []byte("UPDATED: This file has been modified for testing")
		
		storagePath, err := storageService.UploadFileToTenantBucket(ctx, tenantID, storageKey, updatedContent, "application/pdf")
		assert.NoError(t, err, "Failed to update file")
		
		// Download and verify the update
		downloaded, err := storageService.DownloadFile(ctx, storagePath)
		assert.NoError(t, err, "Failed to download updated file")
		assert.Equal(t, updatedContent, downloaded, "File content should be updated")
		
		t.Logf("✓ File updated successfully with new content")
		testResults["update_file"] = true
	})

	var fileToDelete string

	t.Run("☐ Delete the file in the same folder", func(t *testing.T) {
		// Delete the first file
		bucketName := fmt.Sprintf("aether-%s", extractTenantSuffix(tenantID))
		storageKey := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s",
			spaceType, notebookID, documentID, fileName)
		fileToDelete = fmt.Sprintf("%s/%s", bucketName, storageKey)
		
		err := storageService.DeleteFile(ctx, fileToDelete)
		assert.NoError(t, err, "Failed to delete file")
		
		// Verify deletion
		exists, err := storageService.FileExists(ctx, fileToDelete)
		assert.NoError(t, err, "Failed to check existence after deletion")
		assert.False(t, exists, "File should not exist after deletion")
		
		t.Logf("✓ File deleted successfully: %s", fileName)
		testResults["delete_file"] = true
	})

	t.Run("☐ Remove the folder", func(t *testing.T) {
		// In S3/MinIO, folders are virtual - they exist only as long as files exist
		// Delete remaining files to "remove" the folder
		bucketName := fmt.Sprintf("aether-%s", extractTenantSuffix(tenantID))
		
		// Delete the second file
		storageKey2 := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/diagnostic-test2.txt",
			spaceType, notebookID, documentID)
		fullPath2 := fmt.Sprintf("%s/%s", bucketName, storageKey2)
		
		err := storageService.DeleteFile(ctx, fullPath2)
		assert.NoError(t, err, "Failed to delete second file")
		
		// Verify folder is "empty" (no files with prefix)
		prefix := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/",
			spaceType, notebookID, documentID)
		files, err := storageService.ListFiles(ctx, bucketName, prefix, 100)
		assert.NoError(t, err, "Failed to list files")
		assert.Empty(t, files, "Folder should be empty (no files)")
		
		t.Logf("✓ Folder removed (all files deleted)")
		testResults["remove_folder"] = true
	})

	// For Neo4j tests, we need the database connection
	if testing.Short() {
		t.Log("Skipping Neo4j tests in short mode")
		return
	}

	// Setup Neo4j
	neo4jCfg := &config.DatabaseConfig{
		Neo4jURI:      "bolt://localhost:7687",
		Neo4jUsername: "neo4j", 
		Neo4jPassword: "password",
		Neo4jDatabase: "neo4j",
	}

	neo4jClient, err := database.NewNeo4jClient(neo4jCfg, log)
	if err != nil {
		t.Log("Neo4j not available, skipping database tests")
		return
	}
	defer neo4jClient.Close()

	// Setup services
	redisClient := &mockRedisClient{}
	notebookService := services.NewNotebookService(neo4jClient, redisClient, log)
	documentService := services.NewDocumentService(neo4jClient, redisClient, notebookService, log)
	documentService.SetStorageService(storageService)

	// Test data for Neo4j tests
	userID := "test_user_diag"
	spaceContext := &models.SpaceContext{
		SpaceType:   models.SpaceTypePersonal,
		SpaceID:     "space_diag_test",
		TenantID:    tenantID,
		UserRole:    "owner",
		Permissions: []string{"read", "write", "delete"},
	}

	var testNotebook *models.Notebook

	t.Run("☐ Test creating a Notebook with compliance options", func(t *testing.T) {
		notebookReq := models.NotebookCreateRequest{
			Name:        "Diagnostic Test Notebook",
			Description: "Notebook for diagnostic testing with compliance",
			Type:        "research",
			Tags:        []string{"diagnostic", "test"},
			Metadata: map[string]interface{}{
				"compliance": map[string]interface{}{
					"hipaa_compliant": true,
					"pii_detection":   true,
					"audit_level":     "high",
					"retention_years": 7,
				},
			},
		}

		notebook, err := notebookService.CreateNotebook(ctx, notebookReq, userID, spaceContext)
		assert.NoError(t, err, "Failed to create notebook with compliance")
		assert.NotNil(t, notebook.Metadata["compliance"], "Compliance settings should be saved")
		
		testNotebook = notebook
		t.Logf("✓ Created notebook with compliance options: %s", notebook.ID)
		testResults["create_notebook_compliance"] = true
	})

	t.Run("☐ Test changing that notebook's compliance options", func(t *testing.T) {
		updateReq := models.NotebookUpdateRequest{
			Metadata: map[string]interface{}{
				"compliance": map[string]interface{}{
					"hipaa_compliant": true,
					"pii_detection":   false, // Changed
					"audit_level":     "medium", // Changed
					"retention_years": 10, // Changed
					"gdpr_compliant":  true, // Added
				},
			},
		}

		updated, err := notebookService.UpdateNotebook(ctx, testNotebook.ID, updateReq, userID, spaceContext)
		assert.NoError(t, err, "Failed to update notebook compliance")
		
		compliance := updated.Metadata["compliance"].(map[string]interface{})
		assert.Equal(t, false, compliance["pii_detection"], "PII detection should be updated")
		assert.Equal(t, "medium", compliance["audit_level"], "Audit level should be updated")
		assert.Equal(t, float64(10), compliance["retention_years"], "Retention should be updated")
		assert.Equal(t, true, compliance["gdpr_compliant"], "GDPR compliance should be added")
		
		t.Logf("✓ Updated notebook compliance options successfully")
		testResults["update_notebook_compliance"] = true
	})

	var firstDocID string

	t.Run("☐ Test creating a document", func(t *testing.T) {
		docReq := models.DocumentCreateRequest{
			Name:        "Diagnostic Test Document",
			Description: "First document for count testing",
			NotebookID:  testNotebook.ID,
			Type:        "pdf",
			Tags:        []string{"test", "diagnostic"},
		}

		fileInfo := models.FileInfo{
			OriginalName: "diag-test1.pdf",
			MimeType:     "application/pdf",
			SizeBytes:    2048,
		}

		doc, err := documentService.CreateDocument(ctx, docReq, userID, spaceContext, fileInfo)
		assert.NoError(t, err, "Failed to create document")
		firstDocID = doc.ID
		
		t.Logf("✓ Created document: %s", doc.ID)
		testResults["create_document"] = true
	})

	t.Run("☐ Test that notebook contains the correct count of documents", func(t *testing.T) {
		// Get actual count from Neo4j
		count := getDocumentCount(t, ctx, neo4jClient, testNotebook.ID)
		assert.Equal(t, 1, count, "Document count should be exactly 1")
		
		// Also verify through API
		listResp, err := documentService.ListDocumentsByNotebook(ctx, testNotebook.ID, userID, spaceContext, 0, 100)
		assert.NoError(t, err, "Failed to list documents")
		assert.Equal(t, 1, listResp.Total, "API should report 1 document")
		assert.Len(t, listResp.Documents, 1, "Should have exactly 1 document in list")
		
		t.Logf("✓ Document count is accurate: %d", count)
		testResults["verify_count_accuracy"] = true
	})

	var secondDocID string

	t.Run("☐ Test creating a second document", func(t *testing.T) {
		docReq := models.DocumentCreateRequest{
			Name:        "Second Diagnostic Document", 
			Description: "Second document for count testing",
			NotebookID:  testNotebook.ID,
			Type:        "docx",
			Tags:        []string{"test", "diagnostic", "second"},
		}

		fileInfo := models.FileInfo{
			OriginalName: "diag-test2.docx",
			MimeType:     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			SizeBytes:    4096,
		}

		doc, err := documentService.CreateDocument(ctx, docReq, userID, spaceContext, fileInfo)
		assert.NoError(t, err, "Failed to create second document")
		secondDocID = doc.ID
		
		t.Logf("✓ Created second document: %s", doc.ID)
		testResults["create_second_document"] = true
	})

	t.Run("☐ Repeat count testing", func(t *testing.T) {
		// Get actual count from Neo4j
		count := getDocumentCount(t, ctx, neo4jClient, testNotebook.ID)
		assert.Equal(t, 2, count, "Document count should be exactly 2")
		
		// Verify through API
		listResp, err := documentService.ListDocumentsByNotebook(ctx, testNotebook.ID, userID, spaceContext, 0, 100)
		assert.NoError(t, err, "Failed to list documents")
		assert.Equal(t, 2, listResp.Total, "API should report 2 documents")
		assert.Len(t, listResp.Documents, 2, "Should have exactly 2 documents in list")
		
		// Verify both documents are present
		foundFirst, foundSecond := false, false
		for _, doc := range listResp.Documents {
			if doc.ID == firstDocID {
				foundFirst = true
			}
			if doc.ID == secondDocID {
				foundSecond = true
			}
		}
		assert.True(t, foundFirst, "First document should be in list")
		assert.True(t, foundSecond, "Second document should be in list")
		
		t.Logf("✓ Document count after second creation is accurate: %d", count)
		testResults["repeat_count_test"] = true
	})

	t.Run("☐ Delete a document", func(t *testing.T) {
		err := documentService.DeleteDocument(ctx, firstDocID, userID, spaceContext)
		assert.NoError(t, err, "Failed to delete document")
		
		t.Logf("✓ Deleted document: %s", firstDocID)
		testResults["delete_document"] = true
	})

	t.Run("☐ Confirm delete counts are accurate", func(t *testing.T) {
		// Get actual count from Neo4j (should exclude soft-deleted)
		count := getDocumentCount(t, ctx, neo4jClient, testNotebook.ID)
		assert.Equal(t, 1, count, "Document count should be 1 after deletion")
		
		// Verify through API
		listResp, err := documentService.ListDocumentsByNotebook(ctx, testNotebook.ID, userID, spaceContext, 0, 100)
		assert.NoError(t, err, "Failed to list documents after deletion")
		assert.Equal(t, 1, listResp.Total, "API should report 1 document after deletion")
		assert.Len(t, listResp.Documents, 1, "Should have exactly 1 document in list")
		assert.Equal(t, secondDocID, listResp.Documents[0].ID, "Remaining document should be the second one")
		
		t.Logf("✓ Document count after deletion is accurate: %d", count)
		testResults["confirm_delete_count"] = true
	})

	// Print test summary
	t.Run("Test Summary", func(t *testing.T) {
		t.Log("\n=== DIAGNOSTIC TEST SUMMARY ===")
		allPassed := true
		for test, passed := range testResults {
			status := "✓"
			if !passed {
				status = "✗"
				allPassed = false
			}
			t.Logf("%s %s", status, test)
		}
		
		if allPassed {
			t.Log("\n✅ All diagnostic tests passed!")
		} else {
			t.Log("\n❌ Some tests failed")
		}
	})

	// Cleanup
	t.Cleanup(func() {
		if neo4jClient != nil && testNotebook != nil {
			// Clean up test data
			_, _ = neo4jClient.Session().ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
				query := `
					MATCH (n:Notebook {id: $notebook_id})
					OPTIONAL MATCH (n)-[:CONTAINS]->(d:Document)
					DETACH DELETE d, n
				`
				_, err := tx.Run(ctx, query, map[string]interface{}{
					"notebook_id": testNotebook.ID,
				})
				return nil, err
			})
		}
	})
}