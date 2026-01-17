package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
)

// Migration to add space_id and ensure tenant_id for all notebooks and documents
func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Connect to Neo4j
	neo4jClient, err := database.NewNeo4jClient(cfg.Database.Neo4j)
	if err != nil {
		log.Fatal("Failed to connect to Neo4j:", err)
	}
	defer neo4jClient.Close(context.Background())

	ctx := context.Background()
	
	// Run migrations
	if err := migrateNotebooks(ctx, neo4jClient); err != nil {
		log.Fatal("Failed to migrate notebooks:", err)
	}

	if err := migrateDocuments(ctx, neo4jClient); err != nil {
		log.Fatal("Failed to migrate documents:", err)
	}

	if err := ensureUserPersonalSpaces(ctx, neo4jClient); err != nil {
		log.Fatal("Failed to ensure user personal spaces:", err)
	}

	log.Println("Migration completed successfully!")
}

// migrateNotebooks ensures all notebooks have tenant_id and space_id
func migrateNotebooks(ctx context.Context, client *database.Neo4jClient) error {
	log.Println("Starting notebook migration...")

	// First, find notebooks without tenant_id or space_id
	query := `
		MATCH (n:Notebook)
		WHERE n.tenant_id IS NULL OR n.space_id IS NULL
		OPTIONAL MATCH (n)-[:OWNED_BY]->(u:User)
		RETURN n.id, n.name, n.owner_id, u.personal_tenant_id
	`

	result, err := client.ExecuteQueryWithLogging(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to find notebooks to migrate: %w", err)
	}

	migrated := 0
	for _, record := range result.Records {
		notebookID, _ := record.Get("n.id")
		notebookName, _ := record.Get("n.name")
		ownerID, _ := record.Get("n.owner_id")
		personalTenantID, _ := record.Get("u.personal_tenant_id")

		if notebookID == nil || ownerID == nil {
			continue
		}

		// If we have a personal_tenant_id, use it; otherwise, generate one
		tenantID := ""
		spaceID := ""
		
		if personalTenantID != nil && personalTenantID.(string) != "" {
			tenantID = personalTenantID.(string)
			spaceID = strings.Replace(tenantID, "tenant_", "space_", 1)
		} else {
			// Generate tenant_id based on owner_id
			ownerIDStr := ownerID.(string)
			tenantID = fmt.Sprintf("tenant_%s", strings.ReplaceAll(ownerIDStr, "-", ""))
			spaceID = fmt.Sprintf("space_%s", strings.ReplaceAll(ownerIDStr, "-", ""))
		}

		// Update the notebook
		updateQuery := `
			MATCH (n:Notebook {id: $notebook_id})
			SET n.tenant_id = $tenant_id,
			    n.space_id = $space_id,
			    n.space_type = 'personal',
			    n.updated_at = datetime($updated_at)
			RETURN n
		`

		params := map[string]interface{}{
			"notebook_id": notebookID.(string),
			"tenant_id":   tenantID,
			"space_id":    spaceID,
			"updated_at":  time.Now().Format(time.RFC3339),
		}

		_, err := client.ExecuteQueryWithLogging(ctx, updateQuery, params)
		if err != nil {
			log.Printf("Failed to update notebook %s: %v", notebookID, err)
			continue
		}

		log.Printf("Migrated notebook: %s (%s) -> tenant: %s, space: %s", 
			notebookName, notebookID, tenantID, spaceID)
		migrated++
	}

	log.Printf("Migrated %d notebooks", migrated)
	return nil
}

// migrateDocuments ensures all documents have tenant_id and space_id matching their notebook
func migrateDocuments(ctx context.Context, client *database.Neo4jClient) error {
	log.Println("Starting document migration...")

	// Find documents without tenant_id or space_id
	query := `
		MATCH (d:Document)
		WHERE d.tenant_id IS NULL OR d.space_id IS NULL
		OPTIONAL MATCH (d)-[:BELONGS_TO]->(n:Notebook)
		RETURN d.id, d.name, d.notebook_id, n.tenant_id, n.space_id, n.space_type
	`

	result, err := client.ExecuteQueryWithLogging(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to find documents to migrate: %w", err)
	}

	migrated := 0
	orphaned := 0
	
	for _, record := range result.Records {
		documentID, _ := record.Get("d.id")
		documentName, _ := record.Get("d.name")
		notebookID, _ := record.Get("d.notebook_id")
		notebookTenantID, _ := record.Get("n.tenant_id")
		notebookSpaceID, _ := record.Get("n.space_id")
		notebookSpaceType, _ := record.Get("n.space_type")

		if documentID == nil {
			continue
		}

		// Skip if notebook doesn't have tenant/space info
		if notebookTenantID == nil || notebookSpaceID == nil {
			log.Printf("Document %s has notebook without space info, skipping", documentID)
			orphaned++
			continue
		}

		spaceType := "personal"
		if notebookSpaceType != nil {
			spaceType = notebookSpaceType.(string)
		}

		// Update the document with notebook's space info
		updateQuery := `
			MATCH (d:Document {id: $document_id})
			SET d.tenant_id = $tenant_id,
			    d.space_id = $space_id,
			    d.space_type = $space_type,
			    d.updated_at = datetime($updated_at)
			RETURN d
		`

		params := map[string]interface{}{
			"document_id": documentID.(string),
			"tenant_id":   notebookTenantID.(string),
			"space_id":    notebookSpaceID.(string),
			"space_type":  spaceType,
			"updated_at":  time.Now().Format(time.RFC3339),
		}

		_, err := client.ExecuteQueryWithLogging(ctx, updateQuery, params)
		if err != nil {
			log.Printf("Failed to update document %s: %v", documentID, err)
			continue
		}

		log.Printf("Migrated document: %s (%s) -> tenant: %s, space: %s", 
			documentName, documentID, notebookTenantID, notebookSpaceID)
		migrated++
	}

	log.Printf("Migrated %d documents, %d orphaned", migrated, orphaned)
	return nil
}

// ensureUserPersonalSpaces ensures all users have personal_tenant_id and personal_space_id
func ensureUserPersonalSpaces(ctx context.Context, client *database.Neo4jClient) error {
	log.Println("Ensuring all users have personal space IDs...")

	// Find users without personal_space_id
	query := `
		MATCH (u:User)
		WHERE u.personal_space_id IS NULL
		RETURN u.id, u.username, u.personal_tenant_id
	`

	result, err := client.ExecuteQueryWithLogging(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to find users to update: %w", err)
	}

	updated := 0
	for _, record := range result.Records {
		userID, _ := record.Get("u.id")
		username, _ := record.Get("u.username")
		personalTenantID, _ := record.Get("u.personal_tenant_id")

		if userID == nil {
			continue
		}

		// Derive personal_space_id from tenant_id
		spaceID := ""
		if personalTenantID != nil && personalTenantID.(string) != "" {
			spaceID = strings.Replace(personalTenantID.(string), "tenant_", "space_", 1)
		} else {
			// This shouldn't happen in production, but handle it
			userIDStr := userID.(string)
			tenantID := fmt.Sprintf("tenant_%s", strings.ReplaceAll(userIDStr, "-", ""))
			spaceID = fmt.Sprintf("space_%s", strings.ReplaceAll(userIDStr, "-", ""))
			
			// Also update tenant_id if missing
			updateTenantQuery := `
				MATCH (u:User {id: $user_id})
				SET u.personal_tenant_id = $tenant_id
				RETURN u
			`
			_, err := client.ExecuteQueryWithLogging(ctx, updateTenantQuery, map[string]interface{}{
				"user_id":   userID.(string),
				"tenant_id": tenantID,
			})
			if err != nil {
				log.Printf("Failed to update user tenant_id %s: %v", userID, err)
			}
		}

		// Update user with personal_space_id
		updateQuery := `
			MATCH (u:User {id: $user_id})
			SET u.personal_space_id = $space_id,
			    u.updated_at = datetime($updated_at)
			RETURN u
		`

		params := map[string]interface{}{
			"user_id":    userID.(string),
			"space_id":   spaceID,
			"updated_at": time.Now().Format(time.RFC3339),
		}

		_, err := client.ExecuteQueryWithLogging(ctx, updateQuery, params)
		if err != nil {
			log.Printf("Failed to update user %s: %v", userID, err)
			continue
		}

		log.Printf("Updated user: %s (%s) -> space_id: %s", username, userID, spaceID)
		updated++
	}

	log.Printf("Updated %d users with personal_space_id", updated)
	return nil
}