package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

func main() {
	// Initialize logger
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatal("Failed to initialize logger:", err)
	}
	defer zapLogger.Sync()

	logger := &logger.Logger{Logger: zapLogger}

	// Initialize Neo4j connection
	neo4jClient, err := database.NewNeo4jClient(config.DatabaseConfig{
		URI:      "bolt://localhost:7687",
		Username: "neo4j",
		Password: "password",
		Database: "neo4j",
		MaxConns: 50,
	}, logger)
	if err != nil {
		log.Fatal("Failed to connect to Neo4j:", err)
	}
	defer neo4jClient.Close(context.Background())

	ctx := context.Background()

	logger.Info("Starting document cleanup and reprocessing script")

	// Step 1: Find and analyze dangling documents
	fmt.Println("=== STEP 1: Finding Dangling Documents ===")
	if err := findDanglingDocuments(ctx, neo4jClient, logger); err != nil {
		logger.Error("Failed to find dangling documents", zap.Error(err))
	}

	// Step 2: Remove dangling documents
	fmt.Println("\n=== STEP 2: Removing Dangling Documents ===")
	if err := removeDanglingDocuments(ctx, neo4jClient, logger); err != nil {
		logger.Error("Failed to remove dangling documents", zap.Error(err))
	}

	// Step 3: Find documents with placeholder text
	fmt.Println("\n=== STEP 3: Finding Documents with Placeholder Text ===")
	placeholderDocs, err := findDocumentsWithPlaceholderText(ctx, neo4jClient, logger)
	if err != nil {
		logger.Error("Failed to find documents with placeholder text", zap.Error(err))
		return
	}

	// Step 4: Trigger reprocessing
	if len(placeholderDocs) > 0 {
		fmt.Printf("\n=== STEP 4: Triggering Reprocessing for %d Documents ===\n", len(placeholderDocs))
		if err := triggerReprocessing(ctx, neo4jClient, placeholderDocs, logger); err != nil {
			logger.Error("Failed to trigger reprocessing", zap.Error(err))
		}
	}

	logger.Info("Document cleanup and reprocessing script completed")
}

// findDanglingDocuments identifies documents that may be orphaned
func findDanglingDocuments(ctx context.Context, neo4j *database.Neo4jClient, logger *logger.Logger) error {
	// Find documents without notebooks
	query1 := `
		MATCH (d:Document)
		WHERE NOT (d)-[:BELONGS_TO]->(:Notebook)
		RETURN d.id, d.original_name, d.tenant_id, d.storage_path, d.created_at
		ORDER BY d.created_at DESC
	`
	
	result1, err := neo4j.ExecuteQuery(ctx, query1, nil)
	if err != nil {
		return fmt.Errorf("failed to find documents without notebooks: %w", err)
	}

	fmt.Printf("Documents without notebooks: %d\n", len(result1.Records))
	for _, record := range result1.Records {
		id, _ := record.Get("d.id")
		name, _ := record.Get("d.original_name")
		tenantID, _ := record.Get("d.tenant_id")
		storagePath, _ := record.Get("d.storage_path")
		createdAt, _ := record.Get("d.created_at")
		fmt.Printf("  - ID: %s, Name: %s, TenantID: %s, Storage: %s, Created: %s\n", 
			id, name, tenantID, storagePath, createdAt)
	}

	// Find documents with invalid notebook references
	query2 := `
		MATCH (d:Document)-[:BELONGS_TO]->(n:Notebook)
		WHERE n IS NULL OR n.id IS NULL
		RETURN d.id, d.original_name, d.notebook_id
	`
	
	result2, err := neo4j.ExecuteQuery(ctx, query2, nil)
	if err != nil {
		return fmt.Errorf("failed to find documents with invalid notebook references: %w", err)
	}

	fmt.Printf("Documents with invalid notebook references: %d\n", len(result2.Records))
	for _, record := range result2.Records {
		id, _ := record.Get("d.id")
		name, _ := record.Get("d.original_name")
		notebookID, _ := record.Get("d.notebook_id")
		fmt.Printf("  - ID: %s, Name: %s, NotebookID: %s\n", id, name, notebookID)
	}

	// Find documents without owners
	query3 := `
		MATCH (d:Document)
		WHERE NOT (d)-[:OWNED_BY]->(:User)
		RETURN d.id, d.original_name, d.owner_id
	`
	
	result3, err := neo4j.ExecuteQuery(ctx, query3, nil)
	if err != nil {
		return fmt.Errorf("failed to find documents without owners: %w", err)
	}

	fmt.Printf("Documents without owners: %d\n", len(result3.Records))
	for _, record := range result3.Records {
		id, _ := record.Get("d.id")
		name, _ := record.Get("d.original_name")
		ownerID, _ := record.Get("d.owner_id")
		fmt.Printf("  - ID: %s, Name: %s, OwnerID: %s\n", id, name, ownerID)
	}

	return nil
}

// removeDanglingDocuments removes orphaned documents
func removeDanglingDocuments(ctx context.Context, neo4j *database.Neo4jClient, logger *logger.Logger) error {
	// Remove documents without notebooks
	query := `
		MATCH (d:Document)
		WHERE NOT (d)-[:BELONGS_TO]->(:Notebook)
		DELETE d
		RETURN count(d) as danglingCount
	`
	
	result, err := neo4j.ExecuteQuery(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to remove dangling documents: %w", err)
	}

	if len(result.Records) > 0 {
		if count, ok := result.Records[0].Get("danglingCount"); ok {
			fmt.Printf("Removed %v dangling documents\n", count)
			logger.Info("Removed dangling documents", zap.Any("count", count))
		}
	}

	return nil
}

// DocumentInfo holds basic document information
type DocumentInfo struct {
	ID           string
	Name         string
	TenantID     string
	OwnerID      string
	NotebookID   string
	StoragePath  string
	ExtractedText string
}

// findDocumentsWithPlaceholderText finds documents containing the specific placeholder text
func findDocumentsWithPlaceholderText(ctx context.Context, neo4j *database.Neo4jClient, logger *logger.Logger) ([]DocumentInfo, error) {
	placeholderText := "This is a sample PDF document processed by AudiModal ML service. The document contains important information that has been extracted and analyzed."
	
	query := `
		MATCH (d:Document)
		WHERE d.extracted_text CONTAINS $placeholder_text
		RETURN d.id as id, 
		       d.original_name as name,
		       d.tenant_id as tenant_id,
		       d.owner_id as owner_id,
		       d.notebook_id as notebook_id,
		       d.storage_path as storage_path,
		       d.extracted_text as extracted_text
		ORDER BY d.created_at DESC
	`
	
	params := map[string]interface{}{
		"placeholder_text": placeholderText,
	}
	
	result, err := neo4j.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to find documents with placeholder text: %w", err)
	}

	var documents []DocumentInfo
	fmt.Printf("Found %d documents with placeholder text:\n", len(result.Records))
	
	for i, record := range result.Records {
		doc := DocumentInfo{}
		
		if val, ok := record.Get("id"); ok {
			doc.ID = val.(string)
		}
		if val, ok := record.Get("name"); ok {
			doc.Name = val.(string)
		}
		if val, ok := record.Get("tenant_id"); ok {
			doc.TenantID = val.(string)
		}
		if val, ok := record.Get("owner_id"); ok {
			doc.OwnerID = val.(string)
		}
		if val, ok := record.Get("notebook_id"); ok {
			doc.NotebookID = val.(string)
		}
		if val, ok := record.Get("storage_path"); ok {
			doc.StoragePath = val.(string)
		}
		if val, ok := record.Get("extracted_text"); ok {
			doc.ExtractedText = val.(string)
		}
		
		documents = append(documents, doc)
		
		fmt.Printf("  %d. ID: %s\n", i+1, doc.ID)
		fmt.Printf("     Name: %s\n", doc.Name)
		fmt.Printf("     TenantID: %s\n", doc.TenantID)
		fmt.Printf("     StoragePath: %s\n", doc.StoragePath)
		fmt.Printf("     Text preview: %s...\n", doc.ExtractedText[:min(100, len(doc.ExtractedText))])
		fmt.Println()
	}
	
	return documents, nil
}

// triggerReprocessing creates reprocessing jobs for documents with placeholder text
func triggerReprocessing(ctx context.Context, neo4j *database.Neo4jClient, documents []DocumentInfo, logger *logger.Logger) error {
	successCount := 0
	errorCount := 0
	
	for i, doc := range documents {
		fmt.Printf("Processing document %d/%d: %s\n", i+1, len(documents), doc.Name)
		
		// Create a processing job for reprocessing
		jobID := uuid.New().String()
		
		// Create retry job in database
		query := `
			CREATE (j:ProcessingRetryJob {
				id: $job_id,
				document_id: $document_id,
				tenant_id: $tenant_id,
				retry_attempt: 1,
				status: 'manual_reprocess',
				retry_at: $retry_at,
				created_at: $created_at,
				updated_at: $created_at,
				reason: 'placeholder_text_cleanup'
			})
			RETURN j.id as job_id
		`
		
		params := map[string]interface{}{
			"job_id": jobID,
			"document_id": doc.ID,
			"tenant_id": doc.TenantID,
			"retry_at": time.Now().UTC().Format(time.RFC3339),
			"created_at": time.Now().UTC().Format(time.RFC3339),
		}

		_, err := neo4j.ExecuteQuery(ctx, query, params)
		if err != nil {
			logger.Error("Failed to create reprocessing job",
				zap.String("document_id", doc.ID),
				zap.String("document_name", doc.Name),
				zap.Error(err),
			)
			errorCount++
			continue
		}

		// Update document status to processing
		statusQuery := `
			MATCH (d:Document {id: $document_id})
			SET d.status = 'processing',
			    d.updated_at = $updated_at,
			    d.extracted_text = NULL
			RETURN d.id
		`
		
		statusParams := map[string]interface{}{
			"document_id": doc.ID,
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		}
		
		_, err = neo4j.ExecuteQuery(ctx, statusQuery, statusParams)
		if err != nil {
			logger.Error("Failed to update document status",
				zap.String("document_id", doc.ID),
				zap.Error(err),
			)
			errorCount++
			continue
		}
		
		logger.Info("Created reprocessing job for document",
			zap.String("document_id", doc.ID),
			zap.String("document_name", doc.Name),
			zap.String("job_id", jobID),
		)
		
		successCount++
		
		// Add small delay to avoid overwhelming the system
		time.Sleep(100 * time.Millisecond)
	}
	
	fmt.Printf("\nReprocessing Summary:\n")
	fmt.Printf("  ✅ Successfully queued: %d documents\n", successCount)
	fmt.Printf("  ❌ Failed: %d documents\n", errorCount)
	
	if successCount > 0 {
		fmt.Printf("\nNote: The documents have been marked for reprocessing.\n")
		fmt.Printf("The actual text extraction will be performed by the document processing service.\n")
		fmt.Printf("Check the processing job status and document status to monitor progress.\n")
	}
	
	return nil
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}