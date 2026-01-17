package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/config"
)

func main() {
	fmt.Println("=== Direct Storage Download Test ===")
	
	// Initialize logger
	appLogger, err := logger.New(logger.Config{Level: "debug", Format: "console"})
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	
	// Storage configuration matching the docker-compose.yml
	storageConfig := config.StorageConfig{
		Enabled:         true,
		Endpoint:        "http://localhost:9000", 
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin123",
		Bucket:          "aether-storage", // Default bucket (not used for tenant buckets)
		Region:          "us-east-1",
		UseSSL:          false,
	}
	
	// Create storage service
	storageService, err := services.NewS3StorageService(storageConfig, appLogger)
	if err != nil {
		log.Fatalf("Failed to create storage service: %v", err)
	}
	
	ctx := context.Background()
	
	// Test downloading a file we know exists in MinIO
	// From the MinIO listing: spaces/personal/notebooks/81600038-1262-407b-a45a-9aeac648ead2/documents/503edc70-3a5d-4fcc-ac5d-4493770c1c36/ticket.pdf
	tenantID := "tenant_1756217701"  // This maps to bucket aether-1756217701
	key := "spaces/personal/notebooks/689a0a7a-5038-4059-9e6f-c40ea517f402/documents/503edc70-3a5d-4fcc-ac5d-4493770c1c36/ticket.pdf"
	
	fmt.Printf("Testing direct download from tenant bucket...\n")
	fmt.Printf("Tenant ID: %s\n", tenantID)
	fmt.Printf("Key: %s\n", key)
	
	// Download the file
	fileData, err := storageService.DownloadFileFromTenantBucket(ctx, tenantID, key)
	if err != nil {
		log.Fatalf("Failed to download file: %v", err)
	}
	
	fmt.Printf("✓ Download successful!\n")
	fmt.Printf("File size: %d bytes\n", len(fileData))
	fmt.Printf("File content preview: %s...\n", string(fileData[:min(100, len(fileData))]))
	
	// Save to temporary file for inspection
	tempFile := "/tmp/direct_download_test.pdf"
	err = os.WriteFile(tempFile, fileData, 0644)
	if err != nil {
		log.Printf("Warning: Failed to save file: %v", err)
	} else {
		fmt.Printf("File saved to: %s\n", tempFile)
	}
	
	fmt.Println("\n=== Direct download test completed successfully! ===")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}