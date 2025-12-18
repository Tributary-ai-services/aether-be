package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/Tributary-ai-services/aether-be/tests/utils"
)

// StorageIntegrationTestSuite contains tests for MinIO and DeepLake storage integration
type StorageIntegrationTestSuite struct {
	suite.Suite
	config    *utils.TestConfig
	apiClient *utils.APIClient
	storage   *utils.StorageVerifier
}

// SetupSuite prepares the test suite
func (suite *StorageIntegrationTestSuite) SetupSuite() {
	suite.config = utils.SetupTestEnvironment(suite.T())
	suite.apiClient = utils.NewAPIClient(suite.config.ServerURL)
	
	var err error
	suite.storage, err = utils.NewStorageVerifier(
		suite.config.MinioEndpoint,
		suite.config.MinioAccessKey,
		suite.config.MinioSecretKey,
		"aether-test-storage",
		suite.config.DeepLakeURL,
	)
	require.NoError(suite.T(), err, "Should create storage verifier")
}

// TearDownSuite cleans up after the test suite
func (suite *StorageIntegrationTestSuite) TearDownSuite() {
	utils.TeardownTestEnvironment(suite.T(), suite.config)
}

// SetupTest prepares each individual test
func (suite *StorageIntegrationTestSuite) SetupTest() {
	ctx := context.Background()
	err := suite.apiClient.HealthCheck(ctx)
	require.NoError(suite.T(), err, "API should be healthy")
}

// TestMinIOFileStorage tests MinIO file storage functionality
func (suite *StorageIntegrationTestSuite) TestMinIOFileStorage() {
	ctx := context.Background()
	
	testCases := []struct {
		name         string
		filename     string
		contentType  string
		size         int
	}{
		{
			name:        "Small PDF File",
			filename:    "small_test.pdf",
			contentType: "application/pdf",
			size:        1024, // 1KB
		},
		{
			name:        "Medium Document",
			filename:    "medium_test.docx",
			contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			size:        51200, // 50KB
		},
		{
			name:        "Large Text File",
			filename:    "large_test.txt",
			contentType: "text/plain",
			size:        1048576, // 1MB
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			// Generate test content
			testContent := suite.generateTestContent(tc.filename, tc.size)
			
			// Upload document
			uploadResp, err := suite.apiClient.UploadMultipart(ctx, utils.UploadRequest{
				Name:     tc.filename,
				Content:  testContent,
				Strategy: "semantic",
			})
			require.NoError(suite.T(), err, "Should upload file to MinIO")
			
			// Wait for processing
			finalStatus, err := suite.apiClient.WaitForProcessingComplete(
				ctx, uploadResp.ID, 30*time.Second,
			)
			require.NoError(suite.T(), err, "Should complete processing")
			assert.Equal(suite.T(), "processed", finalStatus.Status)
			
			// Verify file exists in MinIO with correct metadata
			metadata := suite.storage.VerifyFileInMinIO(
				suite.T(), uploadResp.ID, tc.filename, testContent,
			)
			
			// Validate metadata
			assert.Equal(suite.T(), int64(len(testContent)), metadata.Size)
			assert.NotEmpty(suite.T(), metadata.ETag, "Should have ETag")
			assert.NotEmpty(suite.T(), metadata.Checksum, "Should have checksum")
			assert.False(suite.T(), metadata.LastModified.IsZero(), "Should have modification time")
			
			// Test file retrieval performance
			retrievalTime := utils.MeasurePerformance(suite.T(), "File Retrieval", func() {
				// Re-verify to test retrieval
				suite.storage.VerifyFileInMinIO(
					suite.T(), uploadResp.ID, tc.filename, testContent,
				)
			})
			
			// File retrieval should be fast
			assert.Less(suite.T(), retrievalTime, 2*time.Second, "File retrieval should be fast")
			
			suite.T().Logf("✅ MinIO storage test passed for %s (size: %d bytes, retrieval: %v)",
				tc.filename, len(testContent), retrievalTime)
		})
	}
}

// TestDeepLakeVectorStorage tests DeepLake vector storage functionality
func (suite *StorageIntegrationTestSuite) TestDeepLakeVectorStorage() {
	ctx := context.Background()
	
	testCases := []struct {
		name           string
		filename       string
		strategy       string
		expectedChunks int
		searchQueries  []string
	}{
		{
			name:           "Semantic Document Embeddings",
			filename:       "semantic_test.pdf",
			strategy:       "semantic",
			expectedChunks: 5,
			searchQueries:  []string{"natural language", "semantic meaning", "document content"},
		},
		{
			name:           "Technical Document Embeddings",
			filename:       "technical_test.txt",
			strategy:       "adaptive",
			expectedChunks: 8,
			searchQueries:  []string{"technical specifications", "implementation details", "system architecture"},
		},
		{
			name:           "Structured Data Embeddings",
			filename:       "structured_test.csv",
			strategy:       "row_based",
			expectedChunks: 6,
			searchQueries:  []string{"data analysis", "statistical information", "tabular data"},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			// Generate appropriate test content
			testContent := suite.generateSemanticTestContent(tc.filename, tc.strategy)
			
			// Upload document
			uploadResp, err := suite.apiClient.UploadBase64(ctx, utils.UploadRequest{
				Name:     tc.filename,
				Content:  testContent,
				Strategy: tc.strategy,
			})
			require.NoError(suite.T(), err, "Should upload document")
			
			// Wait for processing (including embedding generation)
			finalStatus, err := suite.apiClient.WaitForProcessingComplete(
				ctx, uploadResp.ID, 60*time.Second, // Longer timeout for embedding generation
			)
			require.NoError(suite.T(), err, "Should complete processing with embeddings")
			assert.Equal(suite.T(), "processed", finalStatus.Status)
			
			// Verify embeddings exist in DeepLake
			embeddingTime := utils.MeasurePerformance(suite.T(), "Embedding Verification", func() {
				embeddings := suite.storage.VerifyEmbeddingsInDeepLake(
					suite.T(), uploadResp.ID, tc.expectedChunks,
				)
				
				// Validate embedding structure
				for i, embedding := range embeddings {
					assert.NotEmpty(suite.T(), embedding.ID, "Embedding %d should have ID", i)
					assert.Equal(suite.T(), uploadResp.ID, embedding.DocumentID, 
						"Embedding %d should have correct document ID", i)
					assert.NotEmpty(suite.T(), embedding.ChunkID, "Embedding %d should have chunk ID", i)
					assert.Greater(suite.T(), len(embedding.Vector), 0, 
						"Embedding %d should have vector data", i)
					assert.NotNil(suite.T(), embedding.Metadata, "Embedding %d should have metadata", i)
				}
			})
			
			// Test semantic search functionality
			for _, query := range tc.searchQueries {
				suite.Run(fmt.Sprintf("Search_%s", query), func() {
					searchTime := utils.MeasurePerformance(suite.T(), "Semantic Search", func() {
						searchResults := suite.storage.VerifyEmbeddingSearch(
							suite.T(), query, uploadResp.ID, 2,
						)
						
						// Validate search results
						assert.GreaterOrEqual(suite.T(), len(searchResults), 1,
							"Should find relevant results for query: %s", query)
						
						// Verify similarity scores
						for i, result := range searchResults {
							similarity, exists := result.Metadata["similarity_score"]
							assert.True(suite.T(), exists, "Result %d should have similarity score", i)
							assert.IsType(suite.T(), float64(0), similarity, "Similarity should be float64")
							assert.GreaterOrEqual(suite.T(), similarity.(float64), 0.0,
								"Similarity should be non-negative")
							assert.LessOrEqual(suite.T(), similarity.(float64), 1.0,
								"Similarity should not exceed 1.0")
						}
					})
					
					// Search should be fast
					assert.Less(suite.T(), searchTime, 3*time.Second, 
						"Semantic search should be fast for query: %s", query)
				})
			}
			
			// Validate embedding generation performance
			assert.Less(suite.T(), embeddingTime, 10*time.Second, 
				"Embedding verification should be efficient")
			
			suite.T().Logf("✅ DeepLake storage test passed for %s (%d embeddings, verification: %v)",
				tc.filename, tc.expectedChunks, embeddingTime)
		})
	}
}

// TestStorageConsistency tests consistency between MinIO and DeepLake storage
func (suite *StorageIntegrationTestSuite) TestStorageConsistency() {
	ctx := context.Background()
	
	testDoc := "consistency_test.pdf"
	testContent := suite.generateTestContent(testDoc, 2048) // 2KB document
	
	// Upload document
	uploadResp, err := suite.apiClient.UploadMultipart(ctx, utils.UploadRequest{
		Name:     testDoc,
		Content:  testContent,
		Strategy: "semantic",
	})
	require.NoError(suite.T(), err, "Should upload document")
	
	// Wait for complete processing
	_, err = suite.apiClient.WaitForProcessingComplete(ctx, uploadResp.ID, 45*time.Second)
	require.NoError(suite.T(), err, "Should complete processing")
	
	// Get chunks from API
	chunksResp, err := suite.apiClient.GetFileChunks(ctx, uploadResp.ID, 50, 0)
	require.NoError(suite.T(), err, "Should retrieve chunks")
	
	suite.Run("Cross-Storage Consistency", func() {
		// Verify file exists in MinIO
		metadata := suite.storage.VerifyFileInMinIO(
			suite.T(), uploadResp.ID, testDoc, testContent,
		)
		assert.NotNil(suite.T(), metadata, "File should exist in MinIO")
		
		// Verify embeddings exist in DeepLake
		embeddings := suite.storage.VerifyEmbeddingsInDeepLake(
			suite.T(), uploadResp.ID, len(chunksResp.Chunks),
		)
		assert.Equal(suite.T(), len(chunksResp.Chunks), len(embeddings),
			"Number of embeddings should match number of chunks")
		
		// Verify consistency between chunks and embeddings
		suite.validateChunkEmbeddingConsistency(chunksResp.Chunks, embeddings)
	})
	
	suite.Run("Storage Integrity After Updates", func() {
		// Reprocess with different strategy
		reprocessResp, err := suite.apiClient.ReprocessFile(ctx, uploadResp.ID, "fixed")
		require.NoError(suite.T(), err, "Should initiate reprocessing")
		
		// Wait for reprocessing
		_, err = suite.apiClient.WaitForProcessingComplete(ctx, reprocessResp.ID, 45*time.Second)
		require.NoError(suite.T(), err, "Should complete reprocessing")
		
		// Verify file still exists with same content
		suite.storage.VerifyFileInMinIO(suite.T(), uploadResp.ID, testDoc, testContent)
		
		// Get new chunks
		newChunks, err := suite.apiClient.GetFileChunks(ctx, uploadResp.ID, 50, 0)
		require.NoError(suite.T(), err, "Should get updated chunks")
		
		// Verify new embeddings exist
		newEmbeddings := suite.storage.VerifyEmbeddingsInDeepLake(
			suite.T(), uploadResp.ID, len(newChunks.Chunks),
		)
		
		// Verify all new chunks use fixed strategy
		for _, chunk := range newChunks.Chunks {
			assert.Equal(suite.T(), "fixed", chunk.Strategy, "Should use fixed strategy")
		}
		
		// Verify embeddings were updated
		suite.validateChunkEmbeddingConsistency(newChunks.Chunks, newEmbeddings)
	})
}

// TestStorageFailureHandling tests error handling in storage operations
func (suite *StorageIntegrationTestSuite) TestStorageFailureHandling() {
	ctx := context.Background()
	
	suite.Run("Large File Handling", func() {
		// Test with file at size limit
		largeContent := suite.generateTestContent("large_file.pdf", 10*1024*1024) // 10MB
		
		uploadResp, err := suite.apiClient.UploadMultipart(ctx, utils.UploadRequest{
			Name:     "large_file.pdf",
			Content:  largeContent,
			Strategy: "fixed",
		})
		require.NoError(suite.T(), err, "Should handle large file upload")
		
		// Processing might take longer for large files
		_, err = suite.apiClient.WaitForProcessingComplete(ctx, uploadResp.ID, 120*time.Second)
		require.NoError(suite.T(), err, "Should process large file")
		
		// Verify storage
		suite.storage.VerifyFileInMinIO(suite.T(), uploadResp.ID, "large_file.pdf", largeContent)
	})
	
	suite.Run("Concurrent Upload Handling", func() {
		// Test concurrent uploads to verify storage consistency
		numConcurrent := 5
		results := make(chan error, numConcurrent)
		
		for i := 0; i < numConcurrent; i++ {
			go func(index int) {
				filename := fmt.Sprintf("concurrent_%d.txt", index)
				content := suite.generateTestContent(filename, 1024)
				
				uploadResp, err := suite.apiClient.UploadBase64(ctx, utils.UploadRequest{
					Name:     filename,
					Content:  content,
					Strategy: "semantic",
				})
				if err != nil {
					results <- err
					return
				}
				
				_, err = suite.apiClient.WaitForProcessingComplete(ctx, uploadResp.ID, 60*time.Second)
				if err != nil {
					results <- err
					return
				}
				
				// Verify storage
				suite.storage.VerifyFileInMinIO(suite.T(), uploadResp.ID, filename, content)
				results <- nil
			}(i)
		}
		
		// Wait for all concurrent operations
		for i := 0; i < numConcurrent; i++ {
			err := <-results
			assert.NoError(suite.T(), err, "Concurrent operation %d should succeed", i)
		}
	})
}

// TestStorageCleanup tests storage cleanup operations
func (suite *StorageIntegrationTestSuite) TestStorageCleanup() {
	ctx := context.Background()
	
	testDoc := "cleanup_test.pdf"
	testContent := suite.generateTestContent(testDoc, 1024)
	
	// Upload document
	uploadResp, err := suite.apiClient.UploadMultipart(ctx, utils.UploadRequest{
		Name:     testDoc,
		Content:  testContent,
		Strategy: "semantic",
	})
	require.NoError(suite.T(), err, "Should upload document")
	
	// Wait for processing
	_, err = suite.apiClient.WaitForProcessingComplete(ctx, uploadResp.ID, 30*time.Second)
	require.NoError(suite.T(), err, "Should complete processing")
	
	// Verify storage exists
	suite.storage.VerifyFileInMinIO(suite.T(), uploadResp.ID, testDoc, testContent)
	chunksResp, err := suite.apiClient.GetFileChunks(ctx, uploadResp.ID, 20, 0)
	require.NoError(suite.T(), err, "Should have chunks")
	suite.storage.VerifyEmbeddingsInDeepLake(suite.T(), uploadResp.ID, len(chunksResp.Chunks))
	
	// TODO: Implement document deletion endpoint and test cleanup
	// For now, verify that storage verification can detect missing files
	
	suite.Run("Cleanup Verification", func() {
		// Test verification of non-existent files
		nonExistentID := "non-existent-document-id"
		suite.storage.VerifyFileNotInMinIO(suite.T(), nonExistentID, "missing.pdf")
		suite.storage.VerifyNoEmbeddingsInDeepLake(suite.T(), nonExistentID)
	})
}

// Helper methods

func (suite *StorageIntegrationTestSuite) generateTestContent(filename string, size int) []byte {
	// Generate realistic test content based on file type and size
	content := make([]byte, size)
	
	// Fill with meaningful content based on file type
	baseContent := ""
	switch {
	case strings.HasSuffix(filename, ".pdf"):
		baseContent = "%PDF-1.4\nTest PDF content for storage integration testing. "
	case strings.HasSuffix(filename, ".txt"):
		baseContent = "This is test text content for storage integration testing. "
	case strings.HasSuffix(filename, ".csv"):
		baseContent = "id,name,value\n1,Test Item,Sample Value\n"
	case strings.HasSuffix(filename, ".json"):
		baseContent = `{"test": true, "content": "storage integration testing", `
	default:
		baseContent = "Test content for storage integration testing. "
	}
	
	// Repeat content to reach desired size
	for i := 0; i < size; i++ {
		content[i] = baseContent[i%len(baseContent)]
	}
	
	return content
}

func (suite *StorageIntegrationTestSuite) generateSemanticTestContent(filename, strategy string) []byte {
	// Generate content optimized for semantic processing and embedding generation
	templates := map[string]string{
		"semantic": `Natural language processing is a subfield of computer science and artificial intelligence. 
It focuses on the interaction between computers and human language. The goal is to enable computers 
to understand, interpret, and generate human language in a meaningful way. Applications include 
machine translation, sentiment analysis, and text summarization.`,
		
		"adaptive": `Technical documentation requires careful consideration of multiple factors. 
System architecture must be scalable and maintainable. Performance optimization involves 
analyzing bottlenecks and implementing efficient algorithms. Security considerations include 
authentication, authorization, and data protection measures.`,
		
		"row_based": `Product ID,Product Name,Category,Price,Stock
001,Laptop Computer,Electronics,999.99,50
002,Office Chair,Furniture,299.99,25
003,Coffee Mug,Kitchen,12.99,100
004,Notebook,Stationery,5.99,200`,
	}
	
	baseContent := templates[strategy]
	if baseContent == "" {
		baseContent = templates["semantic"] // Default fallback
	}
	
	// Expand content to ensure sufficient data for chunking
	expandedContent := ""
	for i := 0; i < 10; i++ {
		expandedContent += fmt.Sprintf("Section %d: %s\n\n", i+1, baseContent)
	}
	
	return []byte(expandedContent)
}

func (suite *StorageIntegrationTestSuite) validateChunkEmbeddingConsistency(chunks []utils.ChunkResponse, embeddings []utils.EmbeddingMetadata) {
	// Create maps for quick lookup
	chunkMap := make(map[string]utils.ChunkResponse)
	embeddingMap := make(map[string]utils.EmbeddingMetadata)
	
	for _, chunk := range chunks {
		chunkMap[chunk.ID] = chunk
	}
	
	for _, embedding := range embeddings {
		embeddingMap[embedding.ChunkID] = embedding
	}
	
	// Verify each chunk has corresponding embedding
	for _, chunk := range chunks {
		embedding, exists := embeddingMap[chunk.ID]
		assert.True(suite.T(), exists, "Chunk %s should have corresponding embedding", chunk.ID)
		
		if exists {
			assert.Equal(suite.T(), chunk.DocumentID, embedding.DocumentID,
				"Chunk and embedding should have same document ID")
			assert.Equal(suite.T(), chunk.ID, embedding.ChunkID,
				"Embedding should reference correct chunk ID")
			assert.NotEmpty(suite.T(), embedding.Vector,
				"Embedding should have vector data for chunk %s", chunk.ID)
		}
	}
	
	// Verify no orphaned embeddings
	for _, embedding := range embeddings {
		_, exists := chunkMap[embedding.ChunkID]
		assert.True(suite.T(), exists, "Embedding should reference existing chunk %s", embedding.ChunkID)
	}
}

// TestStorageIntegration runs the storage integration test suite
func TestStorageIntegration(t *testing.T) {
	suite.Run(t, new(StorageIntegrationTestSuite))
}