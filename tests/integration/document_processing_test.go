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

// DocumentProcessingTestSuite contains tests for document processing pipeline
type DocumentProcessingTestSuite struct {
	suite.Suite
	config    *utils.TestConfig
	apiClient *utils.APIClient
	storage   *utils.StorageVerifier
	authHelper *utils.AuthHelper
}

// SetupSuite prepares the test suite
func (suite *DocumentProcessingTestSuite) SetupSuite() {
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
	
	suite.authHelper = utils.NewAuthHelper(
		"http://localhost:8081",
		"master",
		"aether-backend",
		"test-client-secret",
	)
}

// TearDownSuite cleans up after the test suite
func (suite *DocumentProcessingTestSuite) TearDownSuite() {
	utils.TeardownTestEnvironment(suite.T(), suite.config)
}

// SetupTest prepares each individual test
func (suite *DocumentProcessingTestSuite) SetupTest() {
	// Verify services are available
	ctx := context.Background()
	err := suite.apiClient.HealthCheck(ctx)
	require.NoError(suite.T(), err, "API should be healthy")
}

// TestPDFProcessingPipeline tests the complete PDF processing workflow
func (suite *DocumentProcessingTestSuite) TestPDFProcessingPipeline() {
	testCases := []struct {
		name              string
		filename          string
		strategy          string
		expectedChunks    int
		expectedQuality   float64
		expectedProcessingTime time.Duration
	}{
		{
			name:              "Simple text PDF with semantic chunking",
			filename:          "simple_text.pdf",
			strategy:          "semantic",
			expectedChunks:    5,
			expectedQuality:   0.8,
			expectedProcessingTime: 15 * time.Second,
		},
		{
			name:              "Complex layout PDF with adaptive chunking",
			filename:          "complex_layout.pdf",
			strategy:          "adaptive",
			expectedChunks:    8,
			expectedQuality:   0.75,
			expectedProcessingTime: 20 * time.Second,
		},
		{
			name:              "Multi-page PDF with fixed chunking",
			filename:          "multipage_document.pdf",
			strategy:          "fixed",
			expectedChunks:    10,
			expectedQuality:   0.7,
			expectedProcessingTime: 25 * time.Second,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			ctx := context.Background()
			
			// Load test document
			documentContent := suite.loadTestDocument(tc.filename)
			
			// Measure upload performance
			var uploadResp *utils.UploadResponse
			uploadTime := utils.MeasurePerformance(suite.T(), "PDF Upload", func() {
				var err error
				uploadResp, err = suite.apiClient.UploadMultipart(ctx, utils.UploadRequest{
					Name:     tc.filename,
					Content:  documentContent,
					Strategy: tc.strategy,
				})
				require.NoError(suite.T(), err, "Should successfully upload PDF")
			})
			
			// Verify upload response
			assert.NotEmpty(suite.T(), uploadResp.ID, "Should have document ID")
			assert.Equal(suite.T(), "queued", uploadResp.Status, "Should be queued for processing")
			assert.NotEmpty(suite.T(), uploadResp.ProcessingJobID, "Should have processing job ID")
			
			// Wait for processing to complete and measure processing time
			var finalStatus *utils.DocumentStatus
			processingTime := utils.MeasurePerformance(suite.T(), "PDF Processing", func() {
				var err error
				finalStatus, err = suite.apiClient.WaitForProcessingComplete(
					ctx, uploadResp.ID, tc.expectedProcessingTime,
				)
				require.NoError(suite.T(), err, "Processing should complete successfully")
			})
			
			// Verify processing results
			assert.Equal(suite.T(), "processed", finalStatus.Status, "Document should be processed")
			assert.Empty(suite.T(), finalStatus.ErrorMessage, "Should have no error message")
			assert.Equal(suite.T(), 100.0, finalStatus.Progress, "Should be 100% complete")
			
			// Verify chunks were created
			var chunksResp *utils.ChunksResponse
			chunkTime := utils.MeasurePerformance(suite.T(), "Chunk Retrieval", func() {
				var err error
				chunksResp, err = suite.apiClient.GetFileChunks(ctx, uploadResp.ID, 20, 0)
				require.NoError(suite.T(), err, "Should retrieve chunks")
			})
			
			// Validate chunk count and quality
			assert.GreaterOrEqual(suite.T(), len(chunksResp.Chunks), tc.expectedChunks,
				"Should have at least %d chunks", tc.expectedChunks)
			assert.Equal(suite.T(), tc.expectedChunks, chunksResp.TotalCount,
				"Total count should match expected chunks")
			
			// Verify chunk quality and metadata
			suite.validateChunkQuality(chunksResp.Chunks, tc.strategy, tc.expectedQuality)
			
			// Verify storage integrity
			var storageTime time.Duration
			suite.Run("Storage Verification", func() {
				storageTime = utils.MeasurePerformance(suite.T(), "Storage Verification", func() {
					suite.storage.VerifyStorageIntegrity(
						suite.T(), uploadResp.ID, tc.filename, 
						documentContent, tc.expectedChunks,
					)
				})
			})
			
			// Test chunk search functionality
			suite.Run("Chunk Search", func() {
				searchTime := utils.MeasurePerformance(suite.T(), "Chunk Search", func() {
					searchResp, err := suite.apiClient.SearchChunks(ctx, utils.SearchRequest{
						Query: "test content",
						Filters: map[string]interface{}{
							"document_id": uploadResp.ID,
						},
						Limit: 5,
					})
					require.NoError(suite.T(), err, "Should perform search")
					assert.NotEmpty(suite.T(), searchResp.Results, "Should have search results")
				})
				
				// Validate search performance
				assert.Less(suite.T(), searchTime, 2*time.Second, "Search should be fast")
			})
			
			// Validate overall performance metrics
			metrics := utils.PerformanceMetrics{
				UploadTime:     uploadTime,
				ProcessingTime: processingTime,
				ChunkTime:      chunkTime,
				StorageTime:    storageTime,
			}
			utils.ValidatePerformanceBenchmarks(suite.T(), metrics)
		})
	}
}

// TestMultiFormatProcessing tests processing of various document formats
func (suite *DocumentProcessingTestSuite) TestMultiFormatProcessing() {
	testFormats := []struct {
		name           string
		filename       string
		mimeType       string
		strategy       string
		expectedChunks int
		expectedQuality float64
	}{
		{
			name:           "Word Document",
			filename:       "sample.docx",
			mimeType:       "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			strategy:       "semantic",
			expectedChunks: 6,
			expectedQuality: 0.85,
		},
		{
			name:           "Excel Spreadsheet",
			filename:       "sample.xlsx",
			mimeType:       "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			strategy:       "row_based",
			expectedChunks: 4,
			expectedQuality: 0.9,
		},
		{
			name:           "PowerPoint Presentation",
			filename:       "sample.pptx",
			mimeType:       "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			strategy:       "semantic",
			expectedChunks: 7,
			expectedQuality: 0.8,
		},
		{
			name:           "Plain Text File",
			filename:       "sample.txt",
			mimeType:       "text/plain",
			strategy:       "fixed",
			expectedChunks: 3,
			expectedQuality: 0.85,
		},
		{
			name:           "CSV Data File",
			filename:       "sample.csv",
			mimeType:       "text/csv",
			strategy:       "row_based",
			expectedChunks: 5,
			expectedQuality: 0.9,
		},
		{
			name:           "JSON Document",
			filename:       "sample.json",
			mimeType:       "application/json",
			strategy:       "adaptive",
			expectedChunks: 4,
			expectedQuality: 0.85,
		},
	}

	for _, format := range testFormats {
		suite.Run(format.name, func() {
			ctx := context.Background()
			
			// Load test document
			documentContent := suite.loadTestDocument(format.filename)
			
			// Upload document
			uploadResp, err := suite.apiClient.UploadBase64(ctx, utils.UploadRequest{
				Name:     format.filename,
				Content:  documentContent,
				Strategy: format.strategy,
			})
			require.NoError(suite.T(), err, "Should upload %s successfully", format.name)
			
			// Wait for processing
			finalStatus, err := suite.apiClient.WaitForProcessingComplete(
				ctx, uploadResp.ID, 30*time.Second,
			)
			require.NoError(suite.T(), err, "Should process %s successfully", format.name)
			assert.Equal(suite.T(), "processed", finalStatus.Status)
			
			// Verify chunks
			chunksResp, err := suite.apiClient.GetFileChunks(ctx, uploadResp.ID, 20, 0)
			require.NoError(suite.T(), err, "Should retrieve chunks for %s", format.name)
			
			assert.GreaterOrEqual(suite.T(), len(chunksResp.Chunks), format.expectedChunks,
				"Should have at least %d chunks for %s", format.expectedChunks, format.name)
			
			// Verify format-specific processing
			suite.validateFormatSpecificProcessing(format.mimeType, chunksResp.Chunks)
			
			// Verify storage
			suite.storage.VerifyStorageIntegrity(
				suite.T(), uploadResp.ID, format.filename,
				documentContent, format.expectedChunks,
			)
		})
	}
}

// TestChunkingStrategies tests all available chunking strategies
func (suite *DocumentProcessingTestSuite) TestChunkingStrategies() {
	ctx := context.Background()
	
	// First get available strategies
	strategiesResp, err := suite.apiClient.GetAvailableStrategies(ctx)
	require.NoError(suite.T(), err, "Should get available strategies")
	assert.NotEmpty(suite.T(), strategiesResp.Strategies, "Should have available strategies")
	
	// Test document for strategy testing
	testDoc := "sample_for_strategies.pdf"
	documentContent := suite.loadTestDocument(testDoc)
	
	for _, strategy := range strategiesResp.Strategies {
		suite.Run(fmt.Sprintf("Strategy_%s", strategy.Name), func() {
			// Upload with specific strategy
			uploadResp, err := suite.apiClient.UploadMultipart(ctx, utils.UploadRequest{
				Name:     fmt.Sprintf("%s_%s", strategy.Name, testDoc),
				Content:  documentContent,
				Strategy: strategy.Name,
			})
			require.NoError(suite.T(), err, "Should upload with %s strategy", strategy.Name)
			
			// Wait for processing
			finalStatus, err := suite.apiClient.WaitForProcessingComplete(
				ctx, uploadResp.ID, 30*time.Second,
			)
			require.NoError(suite.T(), err, "Should process with %s strategy", strategy.Name)
			assert.Equal(suite.T(), "processed", finalStatus.Status)
			
			// Get chunks and verify strategy was applied
			chunksResp, err := suite.apiClient.GetFileChunks(ctx, uploadResp.ID, 50, 0)
			require.NoError(suite.T(), err, "Should get chunks for %s strategy", strategy.Name)
			
			// Verify all chunks use the specified strategy
			for i, chunk := range chunksResp.Chunks {
				assert.Equal(suite.T(), strategy.Name, chunk.Strategy,
					"Chunk %d should use %s strategy", i, strategy.Name)
			}
			
			// Validate strategy-specific characteristics
			suite.validateStrategyCharacteristics(strategy.Name, chunksResp.Chunks)
		})
	}
}

// TestReprocessingWithDifferentStrategy tests reprocessing documents with different strategies
func (suite *DocumentProcessingTestSuite) TestReprocessingWithDifferentStrategy() {
	ctx := context.Background()
	
	testDoc := "reprocessing_test.pdf"
	documentContent := suite.loadTestDocument(testDoc)
	
	// Initial upload with semantic strategy
	uploadResp, err := suite.apiClient.UploadMultipart(ctx, utils.UploadRequest{
		Name:     testDoc,
		Content:  documentContent,
		Strategy: "semantic",
	})
	require.NoError(suite.T(), err, "Should upload document")
	
	// Wait for initial processing
	_, err = suite.apiClient.WaitForProcessingComplete(ctx, uploadResp.ID, 30*time.Second)
	require.NoError(suite.T(), err, "Should complete initial processing")
	
	// Get initial chunks
	initialChunks, err := suite.apiClient.GetFileChunks(ctx, uploadResp.ID, 50, 0)
	require.NoError(suite.T(), err, "Should get initial chunks")
	
	// Reprocess with different strategy
	reprocessResp, err := suite.apiClient.ReprocessFile(ctx, uploadResp.ID, "fixed")
	require.NoError(suite.T(), err, "Should initiate reprocessing")
	
	// Wait for reprocessing
	_, err = suite.apiClient.WaitForProcessingComplete(ctx, reprocessResp.ID, 30*time.Second)
	require.NoError(suite.T(), err, "Should complete reprocessing")
	
	// Get new chunks
	newChunks, err := suite.apiClient.GetFileChunks(ctx, uploadResp.ID, 50, 0)
	require.NoError(suite.T(), err, "Should get new chunks")
	
	// Verify chunks changed
	assert.NotEqual(suite.T(), len(initialChunks.Chunks), len(newChunks.Chunks),
		"Chunk count should change with different strategy")
	
	// Verify all new chunks use the new strategy
	for _, chunk := range newChunks.Chunks {
		assert.Equal(suite.T(), "fixed", chunk.Strategy, "New chunks should use fixed strategy")
	}
}

// Helper methods

func (suite *DocumentProcessingTestSuite) loadTestDocument(filename string) []byte {
	// For testing purposes, create mock document content
	// In real implementation, this would load actual test files
	content := suite.generateMockDocumentContent(filename)
	return content
}

func (suite *DocumentProcessingTestSuite) generateMockDocumentContent(filename string) []byte {
	// Generate appropriate mock content based on file type
	switch {
	case strings.HasSuffix(filename, ".pdf"):
		return []byte("%PDF-1.4\n1 0 obj\n<<\n/Type /Catalog\n/Pages 2 0 R\n>>\nendobj\n" +
			"Mock PDF content for testing document processing pipeline with " + filename)
	case strings.HasSuffix(filename, ".txt"):
		return []byte("This is a sample text document for testing.\n" +
			"It contains multiple lines and paragraphs.\n" +
			"Used for testing the document processing pipeline with " + filename)
	case strings.HasSuffix(filename, ".json"):
		return []byte(`{"title": "Test Document", "content": "Sample JSON content", ` +
			`"filename": "` + filename + `", "sections": ["intro", "body", "conclusion"]}`)
	case strings.HasSuffix(filename, ".csv"):
		return []byte("id,name,description,category\n" +
			"1,Test Item 1,Sample description 1,Category A\n" +
			"2,Test Item 2,Sample description 2,Category B\n" +
			"3,Test Item 3,Sample description 3,Category A")
	default:
		return []byte("Mock document content for " + filename + " - testing document processing pipeline")
	}
}

func (suite *DocumentProcessingTestSuite) validateChunkQuality(chunks []utils.ChunkResponse, strategy string, expectedQuality float64) {
	for i, chunk := range chunks {
		// Verify basic chunk structure
		assert.NotEmpty(suite.T(), chunk.ID, "Chunk %d should have ID", i)
		assert.NotEmpty(suite.T(), chunk.Content, "Chunk %d should have content", i)
		assert.Equal(suite.T(), strategy, chunk.Strategy, "Chunk %d should use correct strategy", i)
		assert.GreaterOrEqual(suite.T(), chunk.QualityScore, expectedQuality,
			"Chunk %d quality should meet threshold", i)
		assert.Greater(suite.T(), chunk.TokenCount, 0, "Chunk %d should have token count", i)
		assert.NotNil(suite.T(), chunk.Metadata, "Chunk %d should have metadata", i)
	}
}

func (suite *DocumentProcessingTestSuite) validateFormatSpecificProcessing(mimeType string, chunks []utils.ChunkResponse) {
	switch mimeType {
	case "text/csv":
		// CSV should have row-based chunking with structured metadata
		for _, chunk := range chunks {
			assert.Contains(suite.T(), chunk.Metadata, "row_count", "CSV chunk should have row count")
			assert.Contains(suite.T(), chunk.Metadata, "columns", "CSV chunk should have column info")
		}
	case "application/json":
		// JSON should preserve structure information
		for _, chunk := range chunks {
			assert.Contains(suite.T(), chunk.Metadata, "json_path", "JSON chunk should have path info")
		}
	case "application/pdf":
		// PDF should have page information
		for _, chunk := range chunks {
			assert.Contains(suite.T(), chunk.Metadata, "page_number", "PDF chunk should have page number")
		}
	}
}

func (suite *DocumentProcessingTestSuite) validateStrategyCharacteristics(strategy string, chunks []utils.ChunkResponse) {
	switch strategy {
	case "semantic":
		// Semantic chunks should have natural language boundaries
		for _, chunk := range chunks {
			assert.Contains(suite.T(), chunk.Metadata, "sentence_count", "Semantic chunk should have sentence count")
		}
	case "fixed":
		// Fixed chunks should have consistent size
		if len(chunks) > 1 {
			firstSize := len(chunks[0].Content)
			for i := 1; i < len(chunks)-1; i++ { // Skip last chunk as it may be smaller
				chunkSize := len(chunks[i].Content)
				tolerance := firstSize / 10 // 10% tolerance
				assert.InDelta(suite.T(), firstSize, chunkSize, float64(tolerance),
					"Fixed chunks should have similar sizes")
			}
		}
	case "row_based":
		// Row-based chunks should have row information
		for _, chunk := range chunks {
			assert.Contains(suite.T(), chunk.Metadata, "row_start", "Row-based chunk should have row start")
			assert.Contains(suite.T(), chunk.Metadata, "row_end", "Row-based chunk should have row end")
		}
	case "adaptive":
		// Adaptive chunks should have complexity metrics
		for _, chunk := range chunks {
			assert.Contains(suite.T(), chunk.Metadata, "complexity_score", "Adaptive chunk should have complexity score")
		}
	}
}

// TestDocumentProcessingPipeline runs the document processing test suite
func TestDocumentProcessingPipeline(t *testing.T) {
	suite.Run(t, new(DocumentProcessingTestSuite))
}