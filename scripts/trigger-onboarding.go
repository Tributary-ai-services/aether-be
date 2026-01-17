package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Get onboarding status
func getOnboardingStatus() error {
	// Call the backend API to check onboarding status
	resp, err := http.Get("http://aether-backend.aether-be:8080/api/v1/onboarding/status")
	if err != nil {
		return fmt.Errorf("failed to get onboarding status: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("Onboarding status response: %s", string(body))

	return nil
}

// Trigger manual onboarding by creating sample documents
func triggerManualOnboarding() error {
	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		uri = "bolt+ssc://neo4j.aether-be.svc.cluster.local:7687"
	}
	username := "neo4j"
	password := "password"

	// Create driver with TLS insecure (same as backend)
	driver, err := neo4j.NewDriverWithContext(
		uri,
		neo4j.BasicAuth(username, password, ""),
		func(config *neo4j.Config) {
			config.TLSInsecure = true
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create driver: %w", err)
	}
	defer driver.Close(context.Background())

	// Verify connectivity
	err = driver.VerifyConnectivity(context.Background())
	if err != nil {
		return fmt.Errorf("failed to verify connectivity: %w", err)
	}
	log.Println("Successfully connected to Neo4j")

	// Find the user and notebook
	ctx := context.Background()
	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	email := "john@scharber.com"
	log.Printf("Looking for user with email: %s", email)

	// Get user details
	result, err := session.Run(ctx,
		`MATCH (u:User {email: $email})
		RETURN u.id as id, u.keycloak_id as keycloak_id, u.email as email,
		       u.personal_space_id as personal_space_id, u.personal_tenant_id as personal_tenant_id`,
		map[string]interface{}{"email": email},
	)
	if err != nil {
		return fmt.Errorf("failed to query user: %w", err)
	}

	if !result.Next(ctx) {
		return fmt.Errorf("user not found")
	}

	record := result.Record()
	userID, _ := record.Get("id")
	keycloakID, _ := record.Get("keycloak_id")
	personalSpaceID, _ := record.Get("personal_space_id")
	personalTenantID, _ := record.Get("personal_tenant_id")

	log.Printf("Found user:")
	log.Printf("  ID: %v", userID)
	log.Printf("  Keycloak ID: %v", keycloakID)
	log.Printf("  Personal Space ID: %v", personalSpaceID)
	log.Printf("  Personal Tenant ID: %v", personalTenantID)

	// Find the "Getting Started" notebook
	result2, err := session.Run(ctx,
		`MATCH (n:Notebook {name: "Getting Started", owner_id: $user_id})
		RETURN n.id as id, n.space_id as space_id, n.tenant_id as tenant_id`,
		map[string]interface{}{"user_id": userID},
	)
	if err != nil {
		return fmt.Errorf("failed to query notebook: %w", err)
	}

	if !result2.Next(ctx) {
		return fmt.Errorf("Getting Started notebook not found")
	}

	record2 := result2.Record()
	notebookID, _ := record2.Get("id")
	spaceID, _ := record2.Get("space_id")
	tenantID, _ := record2.Get("tenant_id")

	log.Printf("Found notebook:")
	log.Printf("  ID: %v", notebookID)
	log.Printf("  Space ID: %v", spaceID)
	log.Printf("  Tenant ID: %v", tenantID)

	// Create sample documents via backend API
	sampleDocs := []struct {
		Name        string
		Description string
		Content     string
	}{
		{
			Name:        "Welcome to Aether.txt",
			Description: "Introduction to the Aether AI Platform",
			Content:     "Welcome to Aether! This is a sample document to help you get started.",
		},
		{
			Name:        "Quick Start Guide.txt",
			Description: "Step-by-step guide to using Aether",
			Content:     "Quick Start Guide: 1. Upload documents 2. Search your content 3. Create AI agents",
		},
		{
			Name:        "Sample FAQ.txt",
			Description: "Frequently Asked Questions about Aether",
			Content:     "FAQ: Q: What is Aether? A: An AI-powered document intelligence platform.",
		},
	}

	// Upload documents via API
	client := &http.Client{Timeout: 30 * time.Second}

	for _, doc := range sampleDocs {
		payload := map[string]interface{}{
			"name":         doc.Name,
			"description":  doc.Description,
			"notebook_id":  notebookID,
			"content":      doc.Content,
			"tags":         []string{"sample", "onboarding"},
		}

		payloadBytes, _ := json.Marshal(payload)

		req, err := http.NewRequest("POST", "http://aether-backend.aether-be:8080/api/v1/documents", bytes.NewBuffer(payloadBytes))
		if err != nil {
			log.Printf("Failed to create request for %s: %v", doc.Name, err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		// TODO: Add authentication token

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Failed to upload %s: %v", doc.Name, err)
			continue
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		log.Printf("Uploaded %s: status=%d, response=%s", doc.Name, resp.StatusCode, string(body))
	}

	log.Println("\nManual onboarding completed!")
	return nil
}

func main() {
	log.Println("Checking onboarding status...")
	err := getOnboardingStatus()
	if err != nil {
		log.Printf("Error: %v", err)
	}

	log.Println("\nAttempting to trigger manual onboarding...")
	err = triggerManualOnboarding()
	if err != nil {
		log.Fatalf("Failed to trigger onboarding: %v", err)
	}
}
