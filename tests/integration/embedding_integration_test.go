package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/tests/utils"
)

// EmbeddingIntegrationTestSuite tests end-to-end embedding generation
type EmbeddingIntegrationTestSuite struct {
	suite.Suite
	config          *utils.TestConfig
	embeddingService *services.EmbeddingService
	deepLakeService  *services.DeepLakeService
	openAIProvider   *services.OpenAIEmbeddingProvider
	log             *logger.Logger
}

// SetupSuite prepares the test suite
func (suite *EmbeddingIntegrationTestSuite) SetupSuite() {
	suite.config = utils.SetupTestEnvironment(suite.T())
	
	// Initialize logger
	var err error
	suite.log, err = logger.NewDefault()
	require.NoError(suite.T(), err)

	// Check if OpenAI API key is available for testing
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		suite.T().Skip("Skipping embedding tests - OPENAI_API_KEY not set")
	}

	// Initialize OpenAI provider
	openAIConfig := &config.OpenAIConfig{
		APIKey:         openAIKey,
		Model:          "text-embedding-ada-002",
		BaseURL:        "https://api.openai.com/v1",
		Dimensions:     1536,
		TimeoutSeconds: 30,
	}

	suite.openAIProvider = services.NewOpenAIEmbeddingProvider(openAIConfig, suite.log)

	// Initialize DeepLake service
	deepLakeConfig := &config.DeepLakeConfig{
		BaseURL:          suite.config.DeepLakeURL,
		APIKey:           "",
		CollectionName:   "test_embeddings",
		VectorDimensions: 1536,
		TimeoutSeconds:   30,
		Enabled:          true,
	}

	suite.deepLakeService = services.NewDeepLakeService(deepLakeConfig, suite.log)

	// Initialize embedding service
	embeddingConfig := &config.EmbeddingConfig{
		Provider:           "openai",
		BatchSize:          5,
		MaxRetries:         3,
		ProcessingInterval: 10,
		Enabled:            true,
	}

	suite.embeddingService = services.NewEmbeddingService(
		suite.openAIProvider,
		suite.deepLakeService,
		suite.log,
		embeddingConfig,
	)
}

// TestOpenAIConnection tests connection to OpenAI API
func (suite *EmbeddingIntegrationTestSuite) TestOpenAIConnection() {
	ctx := context.Background()

	// Test connection
	err := suite.openAIProvider.TestConnection(ctx)
	assert.NoError(suite.T(), err, "Should connect to OpenAI API")

	// Validate configuration
	err = suite.openAIProvider.ValidateConfiguration()
	assert.NoError(suite.T(), err, "Configuration should be valid")

	// Check provider details
	assert.Equal(suite.T(), 1536, suite.openAIProvider.GetDimensions())
	assert.Equal(suite.T(), "text-embedding-ada-002", suite.openAIProvider.GetModelName())
}

// TestSingleEmbedding tests generating a single embedding
func (suite *EmbeddingIntegrationTestSuite) TestSingleEmbedding() {
	ctx := context.Background()

	testText := "This is a test document for embedding generation."

	// Generate embedding
	embedding, err := suite.openAIProvider.GenerateEmbedding(ctx, testText)
	require.NoError(suite.T(), err, "Should generate embedding successfully")

	// Validate embedding
	assert.Equal(suite.T(), 1536, len(embedding), "Embedding should have correct dimensions")
	assert.NotEmpty(suite.T(), embedding, "Embedding should not be empty")

	// Check that embedding values are reasonable
	for i, val := range embedding {
		assert.False(suite.T(), val != val, "Embedding value at index %d should not be NaN", i) // Check for NaN
		assert.True(suite.T(), val >= -1.0 && val <= 1.0, "Embedding value should be normalized")
	}
}

// TestBatchEmbeddings tests generating multiple embeddings
func (suite *EmbeddingIntegrationTestSuite) TestBatchEmbeddings() {
	ctx := context.Background()

	testTexts := []string{
		"This is the first test document.",
		"Here is another document with different content.",
		"Third document for batch processing test.",
		"", // Empty text to test handling
		"Final document in the batch.",
	}

	// Generate batch embeddings
	embeddings, err := suite.openAIProvider.GenerateBatchEmbeddings(ctx, testTexts)
	require.NoError(suite.T(), err, "Should generate batch embeddings successfully")

	// Validate results
	assert.Equal(suite.T(), len(testTexts), len(embeddings), "Should return embedding for each input")

	for i, embedding := range embeddings {
		if testTexts[i] == "" {
			// Empty text should result in nil/empty embedding
			assert.Empty(suite.T(), embedding, "Empty text should result in empty embedding")
		} else {
			assert.Equal(suite.T(), 1536, len(embedding), "Non-empty text should have correct embedding dimensions")
			assert.NotEmpty(suite.T(), embedding, "Non-empty text should have non-empty embedding")
		}
	}
}

// TestEmbeddingService tests the embedding service functionality
func (suite *EmbeddingIntegrationTestSuite) TestEmbeddingService() {
	ctx := context.Background()

	// Test single embedding request
	req := services.EmbeddingRequest{
		ChunkID: "test-chunk-001",
		Content: "This is test content for the embedding service.",
		Metadata: map[string]interface{}{
			"document_id": "test-doc-001",
			"chunk_type":  "text",
		},
	}

	response, err := suite.embeddingService.GenerateEmbedding(ctx, req)
	require.NoError(suite.T(), err, "Should generate embedding via service")

	// Validate response
	assert.Equal(suite.T(), req.ChunkID, response.ChunkID)
	assert.Equal(suite.T(), 1536, response.Dimensions)
	assert.Equal(suite.T(), "text-embedding-ada-002", response.Model)
	assert.NotEmpty(suite.T(), response.Embedding)
	assert.False(suite.T(), response.ProcessedAt.IsZero())
}

// TestBatchEmbeddingService tests batch processing via embedding service
func (suite *EmbeddingIntegrationTestSuite) TestBatchEmbeddingService() {
	ctx := context.Background()

	// Create batch requests
	requests := []services.EmbeddingRequest{
		{
			ChunkID: "batch-chunk-001",
			Content: "First chunk content for batch processing.",
			Metadata: map[string]interface{}{
				"document_id": "batch-doc-001",
				"chunk_type":  "text",
			},
		},
		{
			ChunkID: "batch-chunk-002",
			Content: "Second chunk with different content.",
			Metadata: map[string]interface{}{
				"document_id": "batch-doc-001", 
				"chunk_type":  "text",
			},
		},
		{
			ChunkID: "batch-chunk-003",
			Content: "", // Empty content to test error handling
			Metadata: map[string]interface{}{
				"document_id": "batch-doc-001",
				"chunk_type":  "text",
			},
		},
	}

	result, err := suite.embeddingService.GenerateBatchEmbeddings(ctx, requests)
	require.NoError(suite.T(), err, "Should process batch embeddings")

	// Validate results
	assert.Equal(suite.T(), len(requests), result.TotalCount)
	assert.True(suite.T(), len(result.Successful) >= 2, "Should have at least 2 successful embeddings")
	assert.True(suite.T(), len(result.Failed) >= 1, "Should have at least 1 failed embedding (empty content)")
	assert.True(suite.T(), result.Duration > 0, "Should track processing duration")

	// Check successful responses
	for _, response := range result.Successful {
		assert.NotEmpty(suite.T(), response.ChunkID)
		assert.Equal(suite.T(), 1536, response.Dimensions)
		assert.NotEmpty(suite.T(), response.Embedding)
	}

	// Check failed responses
	for _, failed := range result.Failed {
		assert.NotEmpty(suite.T(), failed.ChunkID)
		assert.NotEmpty(suite.T(), failed.Error)
	}
}

// TestDeepLakeIntegration tests DeepLake vector storage (if available)
func (suite *EmbeddingIntegrationTestSuite) TestDeepLakeIntegration() {
	ctx := context.Background()

	// Check if DeepLake is available
	err := suite.deepLakeService.HealthCheck(ctx)
	if err != nil {
		suite.T().Skip("Skipping DeepLake tests - service not available")
	}

	// Initialize collection
	err = suite.deepLakeService.Initialize(ctx)
	assert.NoError(suite.T(), err, "Should initialize DeepLake collection")

	// Generate test embedding
	testEmbedding, err := suite.openAIProvider.GenerateEmbedding(ctx, "Test content for vector storage")
	require.NoError(suite.T(), err)

	// Store embedding
	metadata := map[string]interface{}{
		"document_id": "test-doc-vector-001",
		"chunk_type":  "text",
		"content":     "Test content for vector storage",
	}

	err = suite.deepLakeService.StoreEmbedding(ctx, "test-vector-001", testEmbedding, metadata)
	assert.NoError(suite.T(), err, "Should store embedding in DeepLake")

	// Test similarity search
	results, err := suite.deepLakeService.SearchSimilar(ctx, testEmbedding, 5, 0.7)
	assert.NoError(suite.T(), err, "Should perform similarity search")
	
	if len(results) > 0 {
		// Should find the vector we just stored
		found := false
		for _, result := range results {
			if result.ChunkID == "test-vector-001" {
				found = true
				assert.True(suite.T(), result.Score >= 0.7, "Self-similarity should be high")
				break
			}
		}
		assert.True(suite.T(), found, "Should find the stored vector in search results")
	}

	// Clean up
	err = suite.deepLakeService.DeleteEmbedding(ctx, "test-vector-001")
	assert.NoError(suite.T(), err, "Should delete test embedding")
}

// TestEmbeddingPerformance tests embedding generation performance
func (suite *EmbeddingIntegrationTestSuite) TestEmbeddingPerformance() {
	ctx := context.Background()

	// Test single embedding performance
	start := time.Now()
	_, err := suite.openAIProvider.GenerateEmbedding(ctx, "Performance test content")
	singleDuration := time.Since(start)
	
	assert.NoError(suite.T(), err)
	assert.True(suite.T(), singleDuration < 10*time.Second, "Single embedding should complete within 10 seconds")

	// Test batch performance
	batchTexts := make([]string, 10)
	for i := range batchTexts {
		batchTexts[i] = "Batch performance test content item " + string(rune(i+'0'))
	}

	start = time.Now()
	embeddings, err := suite.openAIProvider.GenerateBatchEmbeddings(ctx, batchTexts)
	batchDuration := time.Since(start)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(batchTexts), len(embeddings))
	assert.True(suite.T(), batchDuration < 30*time.Second, "Batch embedding should complete within 30 seconds")

	// Batch should be more efficient than individual calls
	estimatedIndividualTime := singleDuration * time.Duration(len(batchTexts))
	suite.T().Logf("Single embedding time: %v", singleDuration)
	suite.T().Logf("Batch embedding time: %v", batchDuration)
	suite.T().Logf("Estimated individual time: %v", estimatedIndividualTime)
	
	if estimatedIndividualTime > batchDuration {
		suite.T().Logf("Batch processing is more efficient (%.2fx faster)",
			float64(estimatedIndividualTime)/float64(batchDuration))
	}
}

// TestEmbeddingIntegration runs the embedding integration test suite
func TestEmbeddingIntegration(t *testing.T) {
	suite.Run(t, new(EmbeddingIntegrationTestSuite))
}