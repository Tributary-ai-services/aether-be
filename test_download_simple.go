//go:build ignore
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	// Test downloading a document from an existing user
	// From Neo4j: document "d1438e89-e088-45a9-a749-0654e45b7db5" belongs to user "d8f32f6d-f49e-4ce9-a873-c337172b1a29" 
	documentID := "d1438e89-e088-45a9-a749-0654e45b7db5" // ticket.pdf
	userID := "d8f32f6d-f49e-4ce9-a873-c337172b1a29"     // john.scharber
	
	url := fmt.Sprintf("http://localhost:8080/api/v1/documents/%s/download", documentID)
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	
	// Try with a simple test approach - no auth middleware by using health endpoint pattern
	// Let's see if we can access the endpoint directly
	
	fmt.Printf("Testing document download (direct test)...\n")
	fmt.Printf("Document ID: %s\n", documentID)
	fmt.Printf("User ID: %s\n", userID)
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
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response body: %v", err)
	}
	
	if resp.StatusCode == 200 {
		fmt.Printf("Downloaded file size: %d bytes\n", len(body))
		if len(body) > 100 {
			fmt.Printf("File content preview: %s...\n", string(body[:100]))
		} else {
			fmt.Printf("File content: %s\n", string(body))
		}
		fmt.Println("✓ Download test successful!")
	} else {
		fmt.Printf("Error response body: %s\n", string(body))
		fmt.Printf("✗ Download test failed with status: %s\n", resp.Status)
	}
}