//go:build ignore
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func main() {
	// Parse command line flags
	keepData := flag.Bool("keep", false, "Keep test data in Neo4j for inspection")
	flag.Parse()

	fmt.Println("=== Neo4j Standalone Tests ===")
	fmt.Println()

	// Neo4j connection settings
	uri := "bolt://localhost:7687"
	username := "neo4j"
	password := "password"

	// Create driver
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		log.Fatalf("Failed to create Neo4j driver: %v", err)
	}
	defer driver.Close(context.Background())

	ctx := context.Background()
	session := driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	fmt.Println("1. Testing Neo4j connectivity")
	// Test connection
	_, err = session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := "RETURN 1 as test"
		result, err := tx.Run(ctx, query, nil)
		if err != nil {
			return nil, err
		}
		return result.Single(ctx)
	})
	if err != nil {
		log.Fatalf("   ✗ Neo4j connection test failed: %v", err)
	}
	fmt.Println("   ✓ Neo4j connection successful")

	// Test 2: Create a test notebook with compliance
	testNotebookID := "test_notebook_diag_123"
	testUserID := "test_user_diag_456"

	fmt.Printf("\n2. Creating test notebook: %s\n", testNotebookID)
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MERGE (n:Notebook {id: $notebook_id})
			SET n.name = $name,
				n.title = $title,
				n.description = $description,
				n.type = $type,
				n.owner_id = $owner_id,
				n.space_type = $space_type,
				n.space_id = $space_id,
				n.tenant_id = $tenant_id,
				n.created_at = datetime(),
				n.updated_at = datetime(),
				n.metadata = $metadata,
				n.tags = $tags,
				n.document_count = $document_count,
				n.search_text = $search_text,
				n.visibility = $visibility,
				n.status = $status,
				n.total_size_bytes = $total_size_bytes,
				n.compliance_settings = $compliance_settings
			RETURN n
		`
		notebookName := "Diagnostic Test Notebook"
		notebookDesc := "Testing Neo4j notebook operations"
		params := map[string]interface{}{
			"notebook_id":    testNotebookID,
			"name":           notebookName,
			"title":          notebookName,
			"description":    notebookDesc,
			"type":           "research",
			"owner_id":       testUserID,
			"space_type":     "personal",
			"space_id":       "space_diag_test",
			"tenant_id":      "tenant_diag_test", 
			"document_count": 0,
			"search_text":    fmt.Sprintf("%s %s", notebookName, notebookDesc),
			"visibility":     "private",
			"status":         "active",
			"total_size_bytes": 0,
			"compliance_settings": "{}",
			"metadata": func() string {
				metadata := map[string]interface{}{
					"compliance": map[string]interface{}{
						"hipaa_compliant": true,
						"pii_detection":   true,
						"audit_level":     "high",
						"retention_years": 7,
					},
				}
				jsonBytes, _ := json.Marshal(metadata)
				return string(jsonBytes)
			}(),
			"tags": []string{"diagnostic", "test"},
		}
		_, err := tx.Run(ctx, query, params)
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to create notebook: %v", err)
	}
	fmt.Println("   ✓ Test notebook created with compliance options")

	// Test 3: Update compliance options
	fmt.Printf("\n3. Updating notebook compliance options\n")
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})
			SET n.metadata = $metadata,
				n.updated_at = datetime()
			RETURN n
		`
		params := map[string]interface{}{
			"notebook_id": testNotebookID,
			"metadata": func() string {
				metadata := map[string]interface{}{
					"compliance": map[string]interface{}{
						"hipaa_compliant": true,
						"pii_detection":   false, // Changed
						"audit_level":     "medium", // Changed 
						"retention_years": 10, // Changed
						"gdpr_compliant":  true, // Added
					},
				}
				jsonBytes, _ := json.Marshal(metadata)
				return string(jsonBytes)
			}(),
		}
		_, err := tx.Run(ctx, query, params)
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to update compliance: %v", err)
	}
	fmt.Println("   ✓ Compliance options updated successfully")

	// Test 4: Create first document
	testDocID1 := "test_doc_diag_001"
	fmt.Printf("\n4. Creating first test document: %s\n", testDocID1)
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})
			CREATE (d:Document {
				id: $document_id,
				name: $name,
				description: $description,
				type: $type,
				notebook_id: $notebook_id,
				owner_id: $owner_id,
				space_type: $space_type,
				space_id: $space_id,
				tenant_id: $tenant_id,
				original_name: $original_name,
				mime_type: $mime_type,
				size_bytes: $size_bytes,
				created_at: datetime(),
				updated_at: datetime(),
				tags: $tags
			})
			CREATE (n)-[:CONTAINS]->(d)
			RETURN d
		`
		params := map[string]interface{}{
			"notebook_id":   testNotebookID,
			"document_id":   testDocID1,
			"name":          "First Diagnostic Document",
			"description":   "Testing document count accuracy",
			"type":          "pdf",
			"owner_id":      testUserID,
			"space_type":    "personal",
			"space_id":      "space_diag_test",
			"tenant_id":     "tenant_diag_test",
			"original_name": "diag1.pdf",
			"mime_type":     "application/pdf",
			"size_bytes":    2048,
			"tags":          []string{"test", "diagnostic"},
		}
		_, err := tx.Run(ctx, query, params)
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to create first document: %v", err)
	}
	fmt.Println("   ✓ First document created successfully")

	// Test 5: Count documents (should be 1)
	fmt.Printf("\n5. Verifying document count (should be 1)\n")
	count, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})-[:CONTAINS]->(d:Document)
			WHERE d.deleted_at IS NULL
			RETURN count(d) as count
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"notebook_id": testNotebookID,
		})
		if err != nil {
			return 0, err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return 0, err
		}
		count, _ := record.Get("count")
		return count.(int64), nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to count documents: %v", err)
	}
	actualCount := int(count.(int64))
	if actualCount != 1 {
		log.Fatalf("   ✗ Expected 1 document, got %d", actualCount)
	}
	fmt.Printf("   ✓ Document count is accurate: %d\n", actualCount)

	// Test 6: Create second document
	testDocID2 := "test_doc_diag_002"
	fmt.Printf("\n6. Creating second test document: %s\n", testDocID2)
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})
			CREATE (d:Document {
				id: $document_id,
				name: $name,
				description: $description,
				type: $type,
				notebook_id: $notebook_id,
				owner_id: $owner_id,
				space_type: $space_type,
				space_id: $space_id,
				tenant_id: $tenant_id,
				original_name: $original_name,
				mime_type: $mime_type,
				size_bytes: $size_bytes,
				created_at: datetime(),
				updated_at: datetime(),
				tags: $tags
			})
			CREATE (n)-[:CONTAINS]->(d)
			RETURN d
		`
		params := map[string]interface{}{
			"notebook_id":   testNotebookID,
			"document_id":   testDocID2,
			"name":          "Second Diagnostic Document",
			"description":   "Testing document count with multiple docs",
			"type":          "docx",
			"owner_id":      testUserID,
			"space_type":    "personal",
			"space_id":      "space_diag_test",
			"tenant_id":     "tenant_diag_test",
			"original_name": "diag2.docx",
			"mime_type":     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"size_bytes":    4096,
			"tags":          []string{"test", "diagnostic", "second"},
		}
		_, err := tx.Run(ctx, query, params)
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to create second document: %v", err)
	}
	fmt.Println("   ✓ Second document created successfully")

	// Test 7: Count documents again (should be 2)
	fmt.Printf("\n7. Verifying document count after second creation (should be 2)\n")
	count, err = session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})-[:CONTAINS]->(d:Document)
			WHERE d.deleted_at IS NULL
			RETURN count(d) as count
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"notebook_id": testNotebookID,
		})
		if err != nil {
			return 0, err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return 0, err
		}
		count, _ := record.Get("count")
		return count.(int64), nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to count documents: %v", err)
	}
	actualCount = int(count.(int64))
	if actualCount != 2 {
		log.Fatalf("   ✗ Expected 2 documents, got %d", actualCount)
	}
	fmt.Printf("   ✓ Document count is accurate: %d\n", actualCount)

	// Test 8: Delete first document (soft delete)
	fmt.Printf("\n8. Deleting first document (soft delete)\n")
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (d:Document {id: $document_id})
			SET d.deleted_at = datetime(),
				d.updated_at = datetime()
			RETURN d
		`
		_, err := tx.Run(ctx, query, map[string]interface{}{
			"document_id": testDocID1,
		})
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to delete document: %v", err)
	}
	fmt.Println("   ✓ First document soft-deleted successfully")

	// Test 9: Count documents after deletion (should be 1)
	fmt.Printf("\n9. Verifying document count after deletion (should be 1)\n")
	count, err = session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})-[:CONTAINS]->(d:Document)
			WHERE d.deleted_at IS NULL
			RETURN count(d) as count
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"notebook_id": testNotebookID,
		})
		if err != nil {
			return 0, err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return 0, err
		}
		count, _ := record.Get("count")
		return count.(int64), nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to count documents: %v", err)
	}
	actualCount = int(count.(int64))
	if actualCount != 1 {
		log.Fatalf("   ✗ Expected 1 document after deletion, got %d", actualCount)
	}
	fmt.Printf("   ✓ Document count after deletion is accurate: %d\n", actualCount)

	// Test 10: List all documents to verify counts
	fmt.Printf("\n10. Listing all documents to verify count accuracy\n")
	documents, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})-[:CONTAINS]->(d:Document)
			WHERE d.deleted_at IS NULL
			RETURN d.id as id, d.name as name, d.type as type
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"notebook_id": testNotebookID,
		})
		if err != nil {
			return nil, err
		}
		
		var docs []map[string]string
		for result.Next(ctx) {
			record := result.Record()
			doc := map[string]string{
				"id":   record.Values[0].(string),
				"name": record.Values[1].(string),
				"type": record.Values[2].(string),
			}
			docs = append(docs, doc)
		}
		return docs, nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to list documents: %v", err)
	}

	docs := documents.([]map[string]string)
	fmt.Printf("   Found %d active documents:\n", len(docs))
	for _, doc := range docs {
		fmt.Printf("     - %s (%s) - %s\n", doc["id"], doc["type"], doc["name"])
	}
	
	if len(docs) == 1 && docs[0]["id"] == testDocID2 {
		fmt.Println("   ✓ Document list matches expected count and content")
	} else {
		log.Fatalf("   ✗ Document list doesn't match expectations")
	}

	// Test 11: Verify deleted document still exists but is marked as deleted
	fmt.Printf("\n11. Verifying soft delete worked correctly\n")
	deletedDoc, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (d:Document {id: $document_id})
			RETURN d.id as id, d.deleted_at as deleted_at
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"document_id": testDocID1,
		})
		if err != nil {
			return nil, err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"id":         record.Values[0].(string),
			"deleted_at": record.Values[1],
		}, nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to check deleted document: %v", err)
	}

	delDoc := deletedDoc.(map[string]interface{})
	if delDoc["deleted_at"] != nil {
		fmt.Printf("   ✓ Deleted document %s has deleted_at timestamp\n", delDoc["id"])
	} else {
		log.Fatalf("   ✗ Deleted document doesn't have deleted_at timestamp")
	}

	// Cleanup
	if !*keepData {
		fmt.Printf("\n12. Cleaning up test data\n")
		_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MATCH (n:Notebook {id: $notebook_id})
				OPTIONAL MATCH (n)-[:CONTAINS]->(d:Document)
				DETACH DELETE d, n
			`
			_, err := tx.Run(ctx, query, map[string]interface{}{
				"notebook_id": testNotebookID,
			})
			return nil, err
		})
		if err != nil {
			log.Printf("   ⚠ Failed to cleanup test data: %v", err)
		} else {
			fmt.Println("   ✓ Test data cleaned up successfully")
		}
	} else {
		fmt.Printf("\n12. Skipping cleanup (--keep flag used)\n")
		fmt.Printf("   📊 Test data preserved in Neo4j:\n")
		fmt.Printf("   📓 Notebook ID: %s\n", testNotebookID)
		fmt.Printf("   📄 Document 1 ID: %s (soft deleted)\n", testDocID1)
		fmt.Printf("   📄 Document 2 ID: %s (active)\n", testDocID2)
		fmt.Printf("   👤 User ID: %s\n", testUserID)
		fmt.Printf("\n   🔍 Inspect with Neo4j Browser: http://localhost:7474\n")
		fmt.Printf("   💾 Query to see notebook: MATCH (n:Notebook {id: '%s'}) RETURN n\n", testNotebookID)
		fmt.Printf("   📋 Query to see docs: MATCH (n:Notebook {id: '%s'})-[:CONTAINS]->(d:Document) RETURN d\n", testNotebookID)
		fmt.Printf("   🗑️  Query active docs: MATCH (n:Notebook {id: '%s'})-[:CONTAINS]->(d:Document) WHERE d.deleted_at IS NULL RETURN d\n", testNotebookID)
	}

	fmt.Println("\n=== All Neo4j tests completed successfully! ===")
}