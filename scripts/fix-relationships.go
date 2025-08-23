package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func main() {
	// Get Neo4j connection details from environment or use defaults
	uri := getEnv("NEO4J_URI", "bolt://localhost:7687")
	username := getEnv("NEO4J_USERNAME", "neo4j")
	password := getEnv("NEO4J_PASSWORD", "password")

	// Create driver
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		log.Fatalf("Failed to create Neo4j driver: %v", err)
	}
	defer driver.Close(context.Background())

	// Verify connection
	err = driver.VerifyConnectivity(context.Background())
	if err != nil {
		log.Fatalf("Failed to connect to Neo4j: %v", err)
	}

	fmt.Println("Connected to Neo4j successfully!")

	ctx := context.Background()
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	// Step 1: Check current state
	fmt.Println("\n=== Current State ===")
	result, err := session.Run(ctx, `
		MATCH (u:User) 
		WITH count(u) as users
		MATCH (t:Team)
		WITH users, count(t) as teams
		MATCH (o:Organization)
		WITH users, teams, count(o) as orgs
		MATCH ()-[r:MEMBER_OF]->()
		RETURN users, teams, orgs, count(r) as relationships
	`, nil)
	if err != nil {
		log.Fatalf("Failed to check current state: %v", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		users, _ := record.Get("users")
		teams, _ := record.Get("teams") 
		orgs, _ := record.Get("orgs")
		relationships, _ := record.Get("relationships")
		fmt.Printf("Users: %v, Teams: %v, Organizations: %v, Existing MEMBER_OF relationships: %v\n", 
			users, teams, orgs, relationships)
	}

	// Step 2: Fix team relationships by keycloak_id
	fmt.Println("\n=== Fixing Team Relationships (by keycloak_id) ===")
	result, err = session.Run(ctx, `
		MATCH (t:Team), (u:User)
		WHERE t.created_by = u.keycloak_id 
		  AND NOT EXISTS((u)-[:MEMBER_OF]->(t))
		CREATE (u)-[:MEMBER_OF {
		  role: 'owner',
		  joined_at: t.created_at,
		  invited_by: u.id
		}]->(t)
		RETURN count(*) as created, collect(t.name) as team_names
	`, nil)
	if err != nil {
		log.Printf("Error fixing team relationships by keycloak_id: %v", err)
	} else if result.Next(ctx) {
		record := result.Record()
		created, _ := record.Get("created")
		teamNames, _ := record.Get("team_names")
		fmt.Printf("Created %v team owner relationships: %v\n", created, teamNames)
	}

	// Step 3: Fix team relationships by user id  
	fmt.Println("\n=== Fixing Team Relationships (by user id) ===")
	result, err = session.Run(ctx, `
		MATCH (t:Team), (u:User)
		WHERE t.created_by = u.id 
		  AND NOT EXISTS((u)-[:MEMBER_OF]->(t))
		CREATE (u)-[:MEMBER_OF {
		  role: 'owner',
		  joined_at: t.created_at,
		  invited_by: u.id
		}]->(t)
		RETURN count(*) as created, collect(t.name) as team_names
	`, nil)
	if err != nil {
		log.Printf("Error fixing team relationships by user id: %v", err)
	} else if result.Next(ctx) {
		record := result.Record()
		created, _ := record.Get("created")
		teamNames, _ := record.Get("team_names")
		fmt.Printf("Created %v team owner relationships: %v\n", created, teamNames)
	}

	// Step 4: Fix organization relationships by keycloak_id
	fmt.Println("\n=== Fixing Organization Relationships (by keycloak_id) ===")
	result, err = session.Run(ctx, `
		MATCH (o:Organization), (u:User)
		WHERE o.created_by = u.keycloak_id 
		  AND NOT EXISTS((u)-[:MEMBER_OF]->(o))
		CREATE (u)-[:MEMBER_OF {
		  role: 'owner',
		  joined_at: o.created_at,
		  invited_by: u.id,
		  title: '',
		  department: ''
		}]->(o)
		RETURN count(*) as created, collect(o.name) as org_names
	`, nil)
	if err != nil {
		log.Printf("Error fixing organization relationships by keycloak_id: %v", err)
	} else if result.Next(ctx) {
		record := result.Record()
		created, _ := record.Get("created")
		orgNames, _ := record.Get("org_names")
		fmt.Printf("Created %v organization owner relationships: %v\n", created, orgNames)
	}

	// Step 5: Fix organization relationships by user id
	fmt.Println("\n=== Fixing Organization Relationships (by user id) ===")
	result, err = session.Run(ctx, `
		MATCH (o:Organization), (u:User)
		WHERE o.created_by = u.id 
		  AND NOT EXISTS((u)-[:MEMBER_OF]->(o))
		CREATE (u)-[:MEMBER_OF {
		  role: 'owner',
		  joined_at: o.created_at,
		  invited_by: u.id,
		  title: '',
		  department: ''
		}]->(o)
		RETURN count(*) as created, collect(o.name) as org_names
	`, nil)
	if err != nil {
		log.Printf("Error fixing organization relationships by user id: %v", err)
	} else if result.Next(ctx) {
		record := result.Record()
		created, _ := record.Get("created")
		orgNames, _ := record.Get("org_names")
		fmt.Printf("Created %v organization owner relationships: %v\n", created, orgNames)
	}

	// Step 6: Final verification
	fmt.Println("\n=== Final Verification ===")
	result, err = session.Run(ctx, `
		MATCH (u:User)-[r:MEMBER_OF]->(entity)
		RETURN u.email as user_email, r.role, labels(entity)[0] as entity_type, entity.name
		ORDER BY u.email, entity_type, entity.name
	`, nil)
	if err != nil {
		log.Printf("Error in final verification: %v", err)
	} else {
		fmt.Println("Current MEMBER_OF relationships:")
		for result.Next(ctx) {
			record := result.Record()
			email, _ := record.Get("user_email")
			role, _ := record.Get("role")
			entityType, _ := record.Get("entity_type")
			entityName, _ := record.Get("entity.name")
			fmt.Printf("  %v (%v) -> %v: %v\n", email, role, entityType, entityName)
		}
	}

	fmt.Println("\n=== Relationship Fix Complete! ===")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}