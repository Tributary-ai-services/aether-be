package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
)

// Rollback script to remove space_id fields if needed
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

	// Confirm rollback
	fmt.Println("WARNING: This will remove space_id fields from notebooks and documents.")
	fmt.Println("This action should only be performed if the migration needs to be reversed.")
	fmt.Print("Type 'ROLLBACK' to confirm: ")
	
	var confirm string
	fmt.Scanln(&confirm)
	
	if confirm != "ROLLBACK" {
		log.Println("Rollback cancelled")
		return
	}

	// Remove space fields from notebooks
	log.Println("Rolling back notebook space fields...")
	notebookQuery := `
		MATCH (n:Notebook)
		WHERE n.space_id IS NOT NULL OR n.space_type IS NOT NULL
		REMOVE n.space_id, n.space_type
		RETURN count(n) as updated
	`
	
	result, err := neo4jClient.ExecuteQueryWithLogging(ctx, notebookQuery, nil)
	if err != nil {
		log.Printf("Failed to rollback notebooks: %v", err)
	} else if len(result.Records) > 0 {
		count, _ := result.Records[0].Get("updated")
		log.Printf("Rolled back %v notebooks", count)
	}

	// Remove space fields from documents
	log.Println("Rolling back document space fields...")
	documentQuery := `
		MATCH (d:Document)
		WHERE d.space_id IS NOT NULL OR d.space_type IS NOT NULL
		REMOVE d.space_id, d.space_type
		RETURN count(d) as updated
	`
	
	result, err = neo4jClient.ExecuteQueryWithLogging(ctx, documentQuery, nil)
	if err != nil {
		log.Printf("Failed to rollback documents: %v", err)
	} else if len(result.Records) > 0 {
		count, _ := result.Records[0].Get("updated")
		log.Printf("Rolled back %v documents", count)
	}

	// Remove personal_space_id from users
	log.Println("Rolling back user personal_space_id fields...")
	userQuery := `
		MATCH (u:User)
		WHERE u.personal_space_id IS NOT NULL
		REMOVE u.personal_space_id
		RETURN count(u) as updated
	`
	
	result, err = neo4jClient.ExecuteQueryWithLogging(ctx, userQuery, nil)
	if err != nil {
		log.Printf("Failed to rollback users: %v", err)
	} else if len(result.Records) > 0 {
		count, _ := result.Records[0].Get("updated")
		log.Printf("Rolled back %v users", count)
	}

	log.Println("Rollback completed!")
}