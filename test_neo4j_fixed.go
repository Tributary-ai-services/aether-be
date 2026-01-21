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
	keepData := flag.Bool("keep", false, "Keep test data in Neo4j for inspection")
	flag.Parse()

	fmt.Println("=== Neo4j Fixed Diagnostic Tests ===")
	fmt.Println()

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

	fmt.Println("1. Testing Neo4j connectivity")
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

	testNotebookID := "test_notebook_fixed_123"
	testUserID := "test_user_fixed_456"

	fmt.Printf("\n2. Creating notebook with ALL required attributes: %s\n", testNotebookID)
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		notebookName := "Fixed Diagnostic Test Notebook"
		notebookDesc := "Testing Neo4j with all proper attributes"
		
		query := `
			CREATE (n:Notebook {
				id: $notebook_id,
				name: $name,
				title: $title,
				description: $description,
				type: $type,
				owner_id: $owner_id,
				space_type: $space_type,
				space_id: $space_id,
				tenant_id: $tenant_id,
				created_at: datetime(),
				updated_at: datetime(),
				document_count: $document_count,
				search_text: $search_text,
				visibility: $visibility,
				status: $status,
				total_size_bytes: $total_size_bytes,
				compliance_settings: $compliance_settings,
				metadata: $metadata,
				tags: $tags
			})
			RETURN n
		`
		
		metadata := map[string]interface{}{
			"compliance": map[string]interface{}{
				"hipaa_compliant": true,
				"pii_detection":   true,
				"audit_level":     "high",
				"retention_years": 7,
			},
		}
		jsonBytes, _ := json.Marshal(metadata)
		
		params := map[string]interface{}{
			"notebook_id":         testNotebookID,
			"name":               notebookName,
			"title":              notebookName,
			"description":        notebookDesc,
			"type":               "research",
			"owner_id":           testUserID,
			"space_type":         "personal",
			"space_id":           "space_fixed_test",
			"tenant_id":          "tenant_fixed_test",
			"document_count":     0,
			"search_text":        fmt.Sprintf("%s %s", notebookName, notebookDesc),
			"visibility":         "private",
			"status":            "active",
			"total_size_bytes":   0,
			"compliance_settings": "{}",
			"metadata":          string(jsonBytes),
			"tags":              []string{"diagnostic", "test", "fixed"},
		}
		_, err := tx.Run(ctx, query, params)
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to create notebook: %v", err)
	}
	fmt.Println("   ✓ Test notebook created with ALL required attributes")

	testDocID1 := "test_doc_fixed_001"
	fmt.Printf("\n3. Creating first document and updating notebook count: %s\n", testDocID1)
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
				tags: $tags,
				status: "active"
			})
			CREATE (n)-[:CONTAINS]->(d)
			WITH n, d
			SET n.document_count = n.document_count + 1,
				n.total_size_bytes = n.total_size_bytes + d.size_bytes,
				n.updated_at = datetime()
			RETURN d, n.document_count as new_count
		`
		params := map[string]interface{}{
			"notebook_id":   testNotebookID,
			"document_id":   testDocID1,
			"name":          "First Fixed Document",
			"description":   "Testing document count with proper attributes",
			"type":          "pdf",
			"owner_id":      testUserID,
			"space_type":    "personal",
			"space_id":      "space_fixed_test",
			"tenant_id":     "tenant_fixed_test",
			"original_name": "fixed1.pdf",
			"mime_type":     "application/pdf",
			"size_bytes":    2048,
			"tags":          []string{"test", "diagnostic", "first"},
		}
		_, err := tx.Run(ctx, query, params)
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to create first document: %v", err)
	}
	fmt.Println("   ✓ First document created and notebook count updated")

	fmt.Printf("\n4. Verifying notebook attributes after first document\n")
	notebookData, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})
			RETURN n.document_count as doc_count, n.total_size_bytes as total_size, n.search_text as search_text
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"notebook_id": testNotebookID,
		})
		if err != nil {
			return nil, err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"doc_count":   record.Values[0],
			"total_size":  record.Values[1],
			"search_text": record.Values[2],
		}, nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to get notebook data: %v", err)
	}

	data := notebookData.(map[string]interface{})
	fmt.Printf("   ✓ Document count: %v\n", data["doc_count"])
	fmt.Printf("   ✓ Total size: %v bytes\n", data["total_size"])
	fmt.Printf("   ✓ Search text: %v\n", data["search_text"])

	testDocID2 := "test_doc_fixed_002"
	fmt.Printf("\n5. Creating second document: %s\n", testDocID2)
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
				tags: $tags,
				status: "active"
			})
			CREATE (n)-[:CONTAINS]->(d)
			WITH n, d
			SET n.document_count = n.document_count + 1,
				n.total_size_bytes = n.total_size_bytes + d.size_bytes,
				n.updated_at = datetime()
			RETURN d, n.document_count as new_count
		`
		params := map[string]interface{}{
			"notebook_id":   testNotebookID,
			"document_id":   testDocID2,
			"name":          "Second Fixed Document",
			"description":   "Testing with proper count updates",
			"type":          "docx",
			"owner_id":      testUserID,
			"space_type":    "personal",
			"space_id":      "space_fixed_test",
			"tenant_id":     "tenant_fixed_test",
			"original_name": "fixed2.docx",
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
	fmt.Println("   ✓ Second document created and notebook count updated")

	fmt.Printf("\n6. Verifying counts after second document\n")
	notebookData, err = session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})
			RETURN n.document_count as doc_count, n.total_size_bytes as total_size
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"notebook_id": testNotebookID,
		})
		if err != nil {
			return nil, err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"doc_count":  record.Values[0],
			"total_size": record.Values[1],
		}, nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to get notebook data: %v", err)
	}

	data = notebookData.(map[string]interface{})
	fmt.Printf("   ✓ Document count: %v (should be 2)\n", data["doc_count"])
	fmt.Printf("   ✓ Total size: %v bytes (should be 6144)\n", data["total_size"])

	fmt.Printf("\n7. Soft deleting first document and updating count\n")
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})-[:CONTAINS]->(d:Document {id: $document_id})
			SET d.deleted_at = datetime(),
				d.updated_at = datetime(),
				d.status = "deleted"
			WITH n, d
			SET n.document_count = n.document_count - 1,
				n.total_size_bytes = n.total_size_bytes - d.size_bytes,
				n.updated_at = datetime()
			RETURN d
		`
		_, err := tx.Run(ctx, query, map[string]interface{}{
			"notebook_id": testNotebookID,
			"document_id": testDocID1,
		})
		return nil, err
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to delete document: %v", err)
	}
	fmt.Println("   ✓ First document soft-deleted and counts updated")

	fmt.Printf("\n8. Final verification of counts after deletion\n")
	notebookData, err = session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook {id: $notebook_id})
			RETURN n.document_count as doc_count, n.total_size_bytes as total_size
		`
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"notebook_id": testNotebookID,
		})
		if err != nil {
			return nil, err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"doc_count":  record.Values[0],
			"total_size": record.Values[1],
		}, nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to get final notebook data: %v", err)
	}

	data = notebookData.(map[string]interface{})
	fmt.Printf("   ✓ Final document count: %v (should be 1)\n", data["doc_count"])
	fmt.Printf("   ✓ Final total size: %v bytes (should be 4096)\n", data["total_size"])

	fmt.Printf("\n9. Comparing with existing notebooks\n")
	comparison, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		query := `
			MATCH (n:Notebook) WHERE n.name IN ['test2', 'test_notebook_fixed_123']
			RETURN n.name, 
				   CASE WHEN n.document_count IS NULL THEN 'NULL' ELSE toString(n.document_count) END as doc_count,
				   CASE WHEN n.search_text IS NULL THEN 'NULL' ELSE n.search_text END as search_text,
				   CASE WHEN n.total_size_bytes IS NULL THEN 'NULL' ELSE toString(n.total_size_bytes) END as total_size
			ORDER BY n.name
		`
		result, err := tx.Run(ctx, query, nil)
		if err != nil {
			return nil, err
		}
		
		records, err := result.Collect(ctx)
		if err != nil {
			return nil, err
		}
		
		return records, nil
	})
	if err != nil {
		log.Fatalf("   ✗ Failed to compare notebooks: %v", err)
	}

	records := comparison.([]*neo4j.Record)
	for _, record := range records {
		name := record.Values[0].(string)
		docCount := record.Values[1].(string)
		searchText := record.Values[2].(string)
		totalSize := record.Values[3].(string)
		
		fmt.Printf("   📓 %s: doc_count=%s, search_text='%s', total_size=%s\n", 
			name, docCount, searchText, totalSize)
	}

	if !*keepData {
		fmt.Printf("\n10. Cleaning up test data\n")
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
		fmt.Printf("\n10. Preserving test data for inspection\n")
		fmt.Printf("   📓 Notebook ID: %s\n", testNotebookID)
		fmt.Printf("   🔍 Compare query: MATCH (n:Notebook) WHERE n.name IN ['test2', '%s'] RETURN n.name, n.document_count, n.search_text, n.total_size_bytes\n", testNotebookID)
	}

	fmt.Println("\n=== Neo4j Fixed Tests completed! ===")
}