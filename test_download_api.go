package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func main() {
	// Test downloading a document that we know exists
	// From the MinIO listing we saw: document ID 503edc70-3a5d-4fcc-ac5d-4493770c1c36 (ticket.pdf)
	documentID := "503edc70-3a5d-4fcc-ac5d-4493770c1c36"
	
	// Get the token first (assuming you have a valid token)
	token := os.Getenv("TEST_TOKEN")
	if token == "" {
		// Try to get a token using the existing script
		log.Println("No TEST_TOKEN environment variable found")
		log.Println("Please run: export TEST_TOKEN=$(./get-token.sh)")
		return
	}
	
	// Make the download request
	url := fmt.Sprintf("http://localhost:8080/api/v1/documents/%s/download", documentID)
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	
	// Add required headers
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("X-Space-Type", "personal")
	req.Header.Add("X-Space-ID", "2f8b65f7-00b8-4baf-bade-578572f64bce") // User ID from token
	
	fmt.Printf("Testing document download...\n")
	fmt.Printf("Document ID: %s\n", documentID)
	fmt.Printf("URL: %s\n", url)
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()
	
	fmt.Printf("Response Status: %s\n", resp.Status)
	fmt.Printf("Content-Type: %s\n", resp.Header.Get("Content-Type"))
	fmt.Printf("Content-Disposition: %s\n", resp.Header.Get("Content-Disposition"))
	fmt.Printf("Content-Length: %s\n", resp.Header.Get("Content-Length"))
	
	if resp.StatusCode == 200 {
		// Read the file content
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("Failed to read response body: %v", err)
		}
		
		fmt.Printf("Downloaded file size: %d bytes\n", len(body))
		fmt.Printf("File content preview: %s...\n", string(body[:min(100, len(body))]))
		
		// Save to temporary file
		err = os.WriteFile("/tmp/downloaded_document.pdf", body, 0644)
		if err != nil {
			log.Printf("Warning: Failed to save file: %v", err)
		} else {
			fmt.Printf("File saved to: /tmp/downloaded_document.pdf\n")
		}
		
		fmt.Println("✓ Download test successful!")
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Error response body: %s\n", string(body))
		fmt.Println("✗ Download test failed!")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}