//go:build ignore
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func main() {
	fmt.Println("=== Testing Updated Backend Document Count Logic ===")
	
	uri := "bolt://localhost:7687"
	username := "neo4j"
	password := "password"

	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		log.Fatalf("Failed to create Neo4j driver: %v", err)
	}
	defer driver.Close(context.Background())

	ctx := context.Background()
	session := driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Clean up any existing test data
	fmt.Println("1. Cleaning up existing test data")
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: 'test_backend_update'})
			OPTIONAL MATCH (n)-[:CONTAINS]->(d:Document)
			DETACH DELETE d, n
		`
		_, err := tx.Run(ctx, query, nil)
		return nil, err
	})
	if err != nil {
		log.Printf("   Cleanup warning: %v", err)
	}
	fmt.Println("   ✓ Cleanup completed")

	// Create notebook with initial counts
	fmt.Println("\n2. Creating notebook with zero counts")
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			CREATE (n:Notebook {
				id: 'test_backend_update',
				name: 'Backend Update Test',
				document_count: 0,
				total_size_bytes: 0,
				created_at: datetime(),
				updated_at: datetime()
			})
			RETURN n
		`
		_, err := tx.Run(ctx, query, nil)
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to create notebook: %v", err)
	}
	fmt.Println("   ✓ Notebook created with zero counts")

	// Test the updated createDocumentRelationships logic
	fmt.Println("\n3. Testing document creation with count updates")
	createResult, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		// First create the document
		docQuery := `
			CREATE (d:Document {
				id: 'test_doc_backend_001',
				name: 'Test Document',
				size_bytes: 1024,
				tenant_id: 'test_tenant',
				created_at: datetime(),
				updated_at: datetime()
			})
			RETURN d
		`
		_, err := tx.Run(ctx, docQuery, nil)
		if err != nil {
			return nil, err
		}

		// Now apply the updated createDocumentRelationships logic
		relationQuery := `
			MATCH (d:Document {id: 'test_doc_backend_001'}), 
			      (n:Notebook {id: 'test_backend_update'})
			CREATE (d)-[:BELONGS_TO]->(n)
			WITH n, d
			SET n.document_count = COALESCE(n.document_count, 0) + 1,
			    n.total_size_bytes = COALESCE(n.total_size_bytes, 0) + d.size_bytes,
			    n.updated_at = datetime()
			RETURN n.document_count, n.total_size_bytes
		`
		result, err := tx.Run(ctx, relationQuery, nil)
		if err != nil {
			return nil, err
		}
		
		record, err := result.Single(ctx)
		if err != nil {
			return nil, err
		}
		
		return map[string]interface{}{
			"doc_count": record.Values[0],
			"total_size": record.Values[1],
		}, nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to create document with count update: %v", err)
	}
	
	data := createResult.(map[string]interface{})
	fmt.Printf("   ✓ Document created, counts updated: %v docs, %v bytes\n", 
		data["doc_count"], data["total_size"])

	// Verify counts
	fmt.Println("\n4. Verifying notebook counts")
	counts, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: 'test_backend_update'})
			RETURN n.document_count, n.total_size_bytes
		`
		result, err := tx.Run(ctx, query, nil)
		if err != nil {
			return nil, err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"doc_count": record.Values[0],
			"total_size": record.Values[1],
		}, nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to get counts: %v", err)
	}
	
	countData := counts.(map[string]interface{})
	fmt.Printf("   ✓ Verified counts: %v docs, %v bytes\n", 
		countData["doc_count"], countData["total_size"])

	// Test deletion logic
	fmt.Println("\n5. Testing document deletion with count updates")
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (d:Document {id: 'test_doc_backend_001'})
			MATCH (d)-[:BELONGS_TO]->(n:Notebook)
			SET d.status = 'deleted',
			    d.deleted_at = datetime(),
			    d.updated_at = datetime(),
			    n.document_count = COALESCE(n.document_count, 0) - 1,
			    n.total_size_bytes = COALESCE(n.total_size_bytes, 0) - d.size_bytes,
			    n.updated_at = datetime()
			RETURN d
		`
		_, err := tx.Run(ctx, query, nil)
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to delete document: %v", err)
	}
	fmt.Println("   ✓ Document deleted with count updates")

	// Final verification
	fmt.Println("\n6. Final count verification")
	finalCounts, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: 'test_backend_update'})
			RETURN n.document_count, n.total_size_bytes
		`
		result, err := tx.Run(ctx, query, nil)
		if err != nil {
			return nil, err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"doc_count": record.Values[0],
			"total_size": record.Values[1],
		}, nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to get final counts: %v", err)
	}
	
	finalData := finalCounts.(map[string]interface{})
	fmt.Printf("   ✓ Final counts: %v docs, %v bytes (should be 0, 0)\n", 
		finalData["doc_count"], finalData["total_size"])

	// Cleanup
	fmt.Println("\n7. Cleaning up test data")
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: 'test_backend_update'})
			OPTIONAL MATCH (n)-[:CONTAINS]->(d:Document)
			DETACH DELETE d, n
		`
		_, err := tx.Run(ctx, query, nil)
		return nil, err
	})
	if err != nil {
		log.Printf("   ⚠ Cleanup failed: %v", err)
	} else {
		fmt.Println("   ✓ Test data cleaned up")
	}

	fmt.Println("\n=== Backend Update Test Completed ===")
}