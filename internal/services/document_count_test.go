package services_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// TestDocumentCountAccuracyWithNeo4j tests document counting accuracy
func TestDocumentCountAccuracyWithNeo4j(t *testing.T) {
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
	redisClient := &mockRedisClient{} // Mock redis for this test
	notebookService := services.NewNotebookService(neo4jClient, redisClient, log)
	documentService := services.NewDocumentService(neo4jClient, redisClient, notebookService, log)

	// Test data
	userID := "test_user_count_123"
	spaceContext := &models.SpaceContext{
		SpaceType:   models.SpaceTypePersonal,
		SpaceID:     "space_count_test",
		TenantID:    "tenant_count_test",
		UserRole:    "owner",
		Permissions: []string{"read", "write", "delete"},
	}

	// Create a test notebook
	notebookReq := models.NotebookCreateRequest{
		Name:        "Test Document Count Notebook",
		Description: "Notebook for testing document counts",
		Type:        "research",
		Tags:        []string{"test", "count"},
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
	require.NoError(t, err, "Failed to create test notebook")

	t.Run("Initial document count should be zero", func(t *testing.ot) {
		count := getDocumentCount(t, ctx, neo4jClient, notebook.ID)
		assert.Equal(t, 0, count, "Initial document count should be 0")
		
		// Also verify through document list
		listResp, err := documentService.ListDocumentsByNotebook(ctx, notebook.ID, userID, spaceContext, 0, 100)
		assert.NoError(t, err)
		assert.Equal(t, 0, listResp.Total, "List response total should be 0")
		assert.Empty(t, listResp.Documents, "Document list should be empty")
	})

	var firstDocID string

	t.Run("Create first document and verify count", func(t *testing.T) {
		// Create document
		docReq := models.DocumentCreateRequest{
			Name:        "First Test Document",
			Description: "Testing document count accuracy",
			NotebookID:  notebook.ID,
			Type:        "pdf",
			Tags:        []string{"test"},
		}

		fileInfo := models.FileInfo{
			OriginalName: "test1.pdf",
			MimeType:     "application/pdf",
			SizeBytes:    1024,
		}

		doc, err := documentService.CreateDocument(ctx, docReq, userID, spaceContext, fileInfo)
		assert.NoError(t, err, "Failed to create first document")
		firstDocID = doc.ID

		// Verify count increased by exactly 1
		count := getDocumentCount(t, ctx, neo4jClient, notebook.ID)
		assert.Equal(t, 1, count, "Document count should be 1 after first creation")

		// Verify through list API
		listResp, err := documentService.ListDocumentsByNotebook(ctx, notebook.ID, userID, spaceContext, 0, 100)
		assert.NoError(t, err)
		assert.Equal(t, 1, listResp.Total, "List response total should be 1")
		assert.Len(t, listResp.Documents, 1, "Should have exactly 1 document in list")
		assert.Equal(t, doc.ID, listResp.Documents[0].ID, "Document ID should match")
	})

	var secondDocID string

	t.Run("Create second document and verify count", func(t *testing.T) {
		// Create second document
		docReq := models.DocumentCreateRequest{
			Name:        "Second Test Document",
			Description: "Testing document count accuracy again",
			NotebookID:  notebook.ID,
			Type:        "docx",
			Tags:        []string{"test", "second"},
		}

		fileInfo := models.FileInfo{
			OriginalName: "test2.docx",
			MimeType:     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			SizeBytes:    2048,
		}

		doc, err := documentService.CreateDocument(ctx, docReq, userID, spaceContext, fileInfo)
		assert.NoError(t, err, "Failed to create second document")
		secondDocID = doc.ID

		// Verify count is exactly 2
		count := getDocumentCount(t, ctx, neo4jClient, notebook.ID)
		assert.Equal(t, 2, count, "Document count should be 2 after second creation")

		// Verify through list API
		listResp, err := documentService.ListDocumentsByNotebook(ctx, notebook.ID, userID, spaceContext, 0, 100)
		assert.NoError(t, err)
		assert.Equal(t, 2, listResp.Total, "List response total should be 2")
		assert.Len(t, listResp.Documents, 2, "Should have exactly 2 documents in list")
		
		// Verify both documents are present
		docIDs := make(map[string]bool)
		for _, d := range listResp.Documents {
			docIDs[d.ID] = true
		}
		assert.True(t, docIDs[firstDocID], "First document should be in list")
		assert.True(t, docIDs[secondDocID], "Second document should be in list")
	})

	t.Run("Delete one document and verify count", func(t *testing.T) {
		// Delete the first document
		err := documentService.DeleteDocument(ctx, firstDocID, userID, spaceContext)
		assert.NoError(t, err, "Failed to delete first document")

		// Verify count decreased by exactly 1
		count := getDocumentCount(t, ctx, neo4jClient, notebook.ID)
		assert.Equal(t, 1, count, "Document count should be 1 after deletion")

		// Verify through list API
		listResp, err := documentService.ListDocumentsByNotebook(ctx, notebook.ID, userID, spaceContext, 0, 100)
		assert.NoError(t, err)
		assert.Equal(t, 1, listResp.Total, "List response total should be 1")
		assert.Len(t, listResp.Documents, 1, "Should have exactly 1 document in list")
		assert.Equal(t, secondDocID, listResp.Documents[0].ID, "Remaining document should be the second one")

		// Verify deleted document is marked as deleted (soft delete)
		deletedDoc := getDocument(t, ctx, neo4jClient, firstDocID)
		assert.NotNil(t, deletedDoc.DeletedAt, "Deleted document should have DeletedAt timestamp")
	})

	t.Run("Create third document after deletion", func(t *testing.T) {
		// Create third document
		docReq := models.DocumentCreateRequest{
			Name:        "Third Test Document",
			Description: "Testing count after deletion",
			NotebookID:  notebook.ID,
			Type:        "txt",
			Tags:        []string{"test", "third"},
		}

		fileInfo := models.FileInfo{
			OriginalName: "test3.txt",
			MimeType:     "text/plain",
			SizeBytes:    512,
		}

		doc, err := documentService.CreateDocument(ctx, docReq, userID, spaceContext, fileInfo)
		assert.NoError(t, err, "Failed to create third document")

		// Verify count is 2 (one deleted, two active)
		count := getDocumentCount(t, ctx, neo4jClient, notebook.ID)
		assert.Equal(t, 2, count, "Document count should be 2 (excluding deleted)")

		// Verify through list API
		listResp, err := documentService.ListDocumentsByNotebook(ctx, notebook.ID, userID, spaceContext, 0, 100)
		assert.NoError(t, err)
		assert.Equal(t, 2, listResp.Total, "List response total should be 2")
		assert.Len(t, listResp.Documents, 2, "Should have exactly 2 active documents")
	})

	// Cleanup
	t.Cleanup(func() {
		// Delete test data
		_, err := neo4jClient.Session().ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			// Delete all test documents and notebook
			query := `
				MATCH (n:Notebook {id: $notebook_id})
				OPTIONAL MATCH (n)-[:CONTAINS]->(d:Document)
				DETACH DELETE d, n
			`
			_, err := tx.Run(ctx, query, map[string]interface{}{
				"notebook_id": notebook.ID,
			})
			return nil, err
		})
		if err != nil {
			t.Logf("Cleanup failed: %v", err)
		}
	})
}

// Helper function to get actual document count from Neo4j
func getDocumentCount(t *testing.T, ctx context.Context, neo4j *database.Neo4jClient, notebookID string) int {
	session := neo4j.Session()
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})-[:CONTAINS]->(d:Document)
			WHERE d.deleted_at IS NULL
			RETURN count(d) as count
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"notebook_id": notebookID,
		})
		if err != nil {
			return 0, err
		}

		record, err := result.Single(ctx)
		if err != nil {
			return 0, err
		}

		count, _ := record.Get("count")
		return count.(int64), nil
	})

	require.NoError(t, err, "Failed to get document count")
	return int(result.(int64))
}

// Helper function to get a document by ID
func getDocument(t *testing.T, ctx context.Context, neo4j *database.Neo4jClient, documentID string) *models.Document {
	session := neo4j.Session()
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (d:Document {id: $document_id})
			RETURN d
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"document_id": documentID,
		})
		if err != nil {
			return nil, err
		}

		record, err := result.Single(ctx)
		if err != nil {
			return nil, err
		}

		node, _ := record.Get("d")
		props := node.(neo4j.Node).Props

		doc := &models.Document{
			ID: props["id"].(string),
		}

		if deletedAt, ok := props["deleted_at"]; ok && deletedAt != nil {
			t := deletedAt.(time.Time)
			doc.DeletedAt = &t
		}

		return doc, nil
	})

	require.NoError(t, err, "Failed to get document")
	return result.(*models.Document)
}

// Mock Redis client for testing
type mockRedisClient struct{}

func (m *mockRedisClient) Get(ctx context.Context, key string) (string, error) {
	return "", nil
}

func (m *mockRedisClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return nil
}

func (m *mockRedisClient) Delete(ctx context.Context, keys ...string) error {
	return nil
}

func (m *mockRedisClient) Exists(ctx context.Context, keys ...string) (int64, error) {
	return 0, nil
}

func (m *mockRedisClient) Ping(ctx context.Context) error {
	return nil
}

func (m *mockRedisClient) Close() error {
	return nil
}