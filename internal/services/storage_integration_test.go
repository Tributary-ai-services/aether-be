package services_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// Test MinIO folder operations
func TestMinIOFolderOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// Setup
	ctx := context.Background()
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Provider:        "s3",
			S3Region:        "us-east-1",
			S3Endpoint:      "http://localhost:9000",
			S3AccessKey:     "minioadmin",
			S3SecretKey:     "minioadmin123",
			S3Bucket:        "aether-storage",
			S3UseSSL:        false,
			S3ForcePathStyle: true,
		},
	}
	
	log := logger.NewLogger(logger.Config{Level: "debug", Format: "json"})
	storageService, err := services.NewS3StorageService(cfg, log)
	require.NoError(t, err, "Failed to create storage service")

	// Test data
	tenantID := "tenant_test_12345"
	spaceType := "personal"
	notebookID := "notebook_test_67890"
	documentID := "document_test_11111"
	fileName := "test-document.txt"
	fileContent := []byte("This is test content for MinIO storage")

	t.Run("Create folder structure and upload file", func(t *testing.T) {
		// Create the storage key following the pattern
		storageKey := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s", 
			spaceType, notebookID, documentID, fileName)
		
		// Upload file (which creates the folder structure automatically)
		storagePath, err := storageService.UploadFileToTenantBucket(ctx, tenantID, storageKey, fileContent, "text/plain")
		assert.NoError(t, err, "Failed to upload file")
		assert.NotEmpty(t, storagePath, "Storage path should not be empty")
		
		t.Logf("File uploaded to: %s", storagePath)

		// Verify file exists
		exists, err := storageService.FileExists(ctx, storagePath)
		assert.NoError(t, err, "Failed to check file existence")
		assert.True(t, exists, "File should exist after upload")
	})

	t.Run("Update the same file", func(t *testing.T) {
		// Update with new content
		updatedContent := []byte("This is UPDATED test content for MinIO storage")
		storageKey := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s", 
			spaceType, notebookID, documentID, fileName)
		
		storagePath, err := storageService.UploadFileToTenantBucket(ctx, tenantID, storageKey, updatedContent, "text/plain")
		assert.NoError(t, err, "Failed to update file")
		
		// Download and verify content
		downloadedContent, err := storageService.DownloadFile(ctx, storagePath)
		assert.NoError(t, err, "Failed to download updated file")
		assert.Equal(t, updatedContent, downloadedContent, "Downloaded content should match updated content")
		
		t.Logf("File successfully updated with new content")
	})

	t.Run("Delete file from folder", func(t *testing.T) {
		storageKey := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s", 
			spaceType, notebookID, documentID, fileName)
		
		// Get the full path for deletion
		bucketName := fmt.Sprintf("aether-%s", extractTenantSuffix(tenantID))
		fullPath := fmt.Sprintf("%s/%s", bucketName, storageKey)
		
		// Delete the file
		err := storageService.DeleteFile(ctx, fullPath)
		assert.NoError(t, err, "Failed to delete file")
		
		// Verify file no longer exists
		exists, err := storageService.FileExists(ctx, fullPath)
		assert.NoError(t, err, "Failed to check file existence after deletion")
		assert.False(t, exists, "File should not exist after deletion")
		
		t.Logf("File successfully deleted")
	})

	t.Run("Remove folder structure", func(t *testing.T) {
		// In S3/MinIO, folders don't really exist - they're just prefixes
		// When all files with a prefix are deleted, the "folder" disappears
		// Let's verify this by checking if any files exist with the document prefix
		
		prefix := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/", 
			spaceType, notebookID, documentID)
		bucketName := fmt.Sprintf("aether-%s", extractTenantSuffix(tenantID))
		
		// List files with prefix (should be empty)
		files, err := storageService.ListFiles(ctx, bucketName, prefix, 10)
		assert.NoError(t, err, "Failed to list files")
		assert.Empty(t, files, "No files should exist with the prefix after deletion")
		
		t.Logf("Folder structure confirmed removed (no files with prefix)")
	})
}

// Test notebook compliance options
func TestNotebookComplianceOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// This would need Neo4j setup - showing structure
	t.Run("Create notebook with compliance options", func(t *testing.T) {
		// TODO: Initialize Neo4j connection
		// TODO: Create notebook with compliance settings
		// Example compliance options:
		// - HIPAA compliance
		// - PII detection enabled
		// - Audit scoring level: HIGH
		// - Data retention: 7 years
		
		t.Skip("Neo4j integration needed")
	})

	t.Run("Update notebook compliance options", func(t *testing.T) {
		// TODO: Update existing notebook's compliance settings
		// TODO: Verify changes were persisted
		
		t.Skip("Neo4j integration needed")
	})
}

// Test document operations with accurate counting
func TestDocumentCountAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// This would need full service setup
	t.Run("Create document and verify count", func(t *testing.T) {
		// TODO: Create document service with Neo4j
		// TODO: Get initial document count for notebook
		// TODO: Create new document
		// TODO: Verify count increased by exactly 1
		// TODO: Query all documents and verify actual count matches
		
		t.Skip("Full service integration needed")
	})

	t.Run("Create second document and verify count", func(t *testing.T) {
		// TODO: Create another document
		// TODO: Verify count is exactly 2
		// TODO: List all documents and confirm count
		
		t.Skip("Full service integration needed")
	})

	t.Run("Delete document and verify count", func(t *testing.T) {
		// TODO: Delete one document
		// TODO: Verify count decreased by exactly 1
		// TODO: List remaining documents and confirm count
		
		t.Skip("Full service integration needed")
	})
}

// Helper function to extract tenant suffix
func extractTenantSuffix(tenantID string) string {
	if len(tenantID) > 10 {
		return tenantID[len(tenantID)-10:]
	}
	return tenantID
}

// Integration test that combines storage and document operations
func TestCompleteDocumentLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Provider:        "s3",
			S3Region:        "us-east-1", 
			S3Endpoint:      "http://localhost:9000",
			S3AccessKey:     "minioadmin",
			S3SecretKey:     "minioadmin123",
			S3Bucket:        "aether-storage",
			S3UseSSL:        false,
			S3ForcePathStyle: true,
		},
	}
	
	log := logger.NewLogger(logger.Config{Level: "debug", Format: "json"})
	storageService, err := services.NewS3StorageService(cfg, log)
	require.NoError(t, err)

	// Test creating multiple documents in a structured way
	tenantID := "tenant_lifecycle_test"
	spaceType := "organization"
	notebookID := "notebook_lifecycle_123"
	
	documents := []struct {
		id      string
		name    string
		content []byte
	}{
		{
			id:      "doc_001",
			name:    "requirements.pdf",
			content: []byte("Project requirements document"),
		},
		{
			id:      "doc_002", 
			name:    "design.pdf",
			content: []byte("System design document"),
		},
		{
			id:      "doc_003",
			name:    "test-plan.pdf", 
			content: []byte("Testing plan document"),
		},
	}

	uploadedPaths := make(map[string]string)

	t.Run("Upload multiple documents", func(t *testing.T) {
		for _, doc := range documents {
			storageKey := fmt.Sprintf("spaces/%s/notebooks/%s/documents/%s/%s",
				spaceType, notebookID, doc.id, doc.name)
			
			path, err := storageService.UploadFileToTenantBucket(ctx, tenantID, storageKey, doc.content, "application/pdf")
			assert.NoError(t, err, "Failed to upload document %s", doc.id)
			uploadedPaths[doc.id] = path
			
			t.Logf("Uploaded document %s to %s", doc.id, path)
		}
	})

	t.Run("Verify all documents exist", func(t *testing.T) {
		for id, path := range uploadedPaths {
			exists, err := storageService.FileExists(ctx, path)
			assert.NoError(t, err, "Failed to check existence for %s", id)
			assert.True(t, exists, "Document %s should exist", id)
		}
	})

	t.Run("List all documents in notebook", func(t *testing.T) {
		prefix := fmt.Sprintf("spaces/%s/notebooks/%s/documents/", spaceType, notebookID)
		bucketName := fmt.Sprintf("aether-%s", extractTenantSuffix(tenantID))
		
		files, err := storageService.ListFiles(ctx, bucketName, prefix, 100)
		assert.NoError(t, err, "Failed to list files")
		assert.Len(t, files, 3, "Should have exactly 3 documents")
		
		t.Logf("Found %d documents in notebook", len(files))
	})

	t.Run("Delete one document and verify count", func(t *testing.T) {
		// Delete the second document
		err := storageService.DeleteFile(ctx, uploadedPaths["doc_002"])
		assert.NoError(t, err, "Failed to delete document")
		
		// List again
		prefix := fmt.Sprintf("spaces/%s/notebooks/%s/documents/", spaceType, notebookID)
		bucketName := fmt.Sprintf("aether-%s", extractTenantSuffix(tenantID))
		
		files, err := storageService.ListFiles(ctx, bucketName, prefix, 100)
		assert.NoError(t, err, "Failed to list files after deletion")
		assert.Len(t, files, 2, "Should have exactly 2 documents after deletion")
	})
}