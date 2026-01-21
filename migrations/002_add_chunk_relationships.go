//go:build ignore
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// Migration to add Chunk nodes and establish relationships with Documents
func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Initialize logger
	appLogger, err := logger.NewDefault()
	if err != nil {
		log.Fatal("Failed to initialize logger:", err)
	}

	// Connect to Neo4j
	neo4jClient, err := database.NewNeo4jClient(cfg.Neo4j, appLogger)
	if err != nil {
		log.Fatal("Failed to connect to Neo4j:", err)
	}
	defer neo4jClient.Close(context.Background())

	ctx := context.Background()
	
	// Run migrations
	if err := createChunkConstraints(ctx, neo4jClient); err != nil {
		log.Fatal("Failed to create chunk constraints:", err)
	}

	if err := createChunkIndexes(ctx, neo4jClient); err != nil {
		log.Fatal("Failed to create chunk indexes:", err)
	}

	if err := updateDocumentChunkFields(ctx, neo4jClient); err != nil {
		log.Fatal("Failed to update document chunk fields:", err)
	}

	log.Println("Chunk relationships migration completed successfully!")
}

// createChunkConstraints creates uniqueness constraints for Chunk nodes
func createChunkConstraints(ctx context.Context, client *database.Neo4jClient) error {
	log.Println("Creating chunk constraints...")

	constraints := []string{
		// Unique constraint on Chunk.id
		"CREATE CONSTRAINT chunk_id_unique IF NOT EXISTS FOR (c:Chunk) REQUIRE c.id IS UNIQUE",
		// Unique constraint on combination of file_id and chunk_id
		"CREATE CONSTRAINT chunk_file_chunk_unique IF NOT EXISTS FOR (c:Chunk) REQUIRE (c.file_id, c.chunk_id) IS UNIQUE",
	}

	for _, constraint := range constraints {
		log.Printf("Creating constraint: %s", constraint)
		_, err := client.ExecuteQuery(ctx, constraint, map[string]interface{}{})
		if err != nil {
			return fmt.Errorf("failed to create constraint: %w", err)
		}
	}

	log.Println("Chunk constraints created successfully")
	return nil
}

// createChunkIndexes creates indexes for efficient chunk queries
func createChunkIndexes(ctx context.Context, client *database.Neo4jClient) error {
	log.Println("Creating chunk indexes...")

	indexes := []string{
		// Index on tenant_id for multi-tenant queries
		"CREATE INDEX chunk_tenant_id_index IF NOT EXISTS FOR (c:Chunk) ON (c.tenant_id)",
		// Index on file_id for file-specific chunk queries
		"CREATE INDEX chunk_file_id_index IF NOT EXISTS FOR (c:Chunk) ON (c.file_id)",
		// Index on chunk_type for type-based filtering
		"CREATE INDEX chunk_type_index IF NOT EXISTS FOR (c:Chunk) ON (c.chunk_type)",
		// Index on content for full-text search
		"CREATE FULLTEXT INDEX chunk_content_fulltext IF NOT EXISTS FOR (c:Chunk) ON EACH [c.content]",
		// Index on language for language-based filtering
		"CREATE INDEX chunk_language_index IF NOT EXISTS FOR (c:Chunk) ON (c.language)",
		// Index on embedding_status for embedding workflow
		"CREATE INDEX chunk_embedding_status_index IF NOT EXISTS FOR (c:Chunk) ON (c.embedding_status)",
		// Index on dlp_scan_status for compliance queries
		"CREATE INDEX chunk_dlp_scan_status_index IF NOT EXISTS FOR (c:Chunk) ON (c.dlp_scan_status)",
		// Index on pii_detected for privacy compliance
		"CREATE INDEX chunk_pii_detected_index IF NOT EXISTS FOR (c:Chunk) ON (c.pii_detected)",
		// Index on created_at for temporal queries
		"CREATE INDEX chunk_created_at_index IF NOT EXISTS FOR (c:Chunk) ON (c.created_at)",
		// Composite index on tenant_id and file_id for common query pattern
		"CREATE INDEX chunk_tenant_file_composite_index IF NOT EXISTS FOR (c:Chunk) ON (c.tenant_id, c.file_id)",
	}

	for _, index := range indexes {
		log.Printf("Creating index: %s", index)
		_, err := client.ExecuteQuery(ctx, index, map[string]interface{}{})
		if err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
		time.Sleep(100 * time.Millisecond) // Small delay between index creations
	}

	log.Println("Chunk indexes created successfully")
	return nil
}

// updateDocumentChunkFields adds chunk-related fields to existing documents
func updateDocumentChunkFields(ctx context.Context, client *database.Neo4jClient) error {
	log.Println("Updating document chunk fields...")

	// Add chunk-related fields to documents that don't have them
	query := `
		MATCH (d:Document)
		WHERE d.chunking_strategy IS NULL OR d.chunk_count IS NULL
		SET d.chunking_strategy = CASE 
			WHEN d.chunking_strategy IS NULL THEN 'semantic' 
			ELSE d.chunking_strategy 
		END,
		d.chunk_count = CASE 
			WHEN d.chunk_count IS NULL THEN 0 
			ELSE d.chunk_count 
		END,
		d.average_chunk_size = CASE 
			WHEN d.average_chunk_size IS NULL THEN 0 
			ELSE d.average_chunk_size 
		END,
		d.chunk_quality_score = CASE 
			WHEN d.chunk_quality_score IS NULL THEN NULL 
			ELSE d.chunk_quality_score 
		END,
		d.updated_at = datetime()
		RETURN count(d) as updated_documents
	`

	result, err := client.ExecuteQuery(ctx, query, map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to update document chunk fields: %w", err)
	}

	if len(result.Records) > 0 {
		if updatedCount, found := result.Records[0].Get("updated_documents"); found {
			log.Printf("Updated %v documents with chunk fields", updatedCount)
		}
	}

	log.Println("Document chunk fields updated successfully")
	return nil
}