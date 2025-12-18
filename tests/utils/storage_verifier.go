package utils

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/require"
)

// StorageVerifier provides methods to verify file storage in MinIO and DeepLake
type StorageVerifier struct {
	s3Client      *s3.S3
	bucketName    string
	deeplakeURL   string
	httpClient    *http.Client
}

// NewStorageVerifier creates a new storage verifier
func NewStorageVerifier(minioEndpoint, accessKey, secretKey, bucketName, deeplakeURL string) (*StorageVerifier, error) {
	// Configure AWS session for MinIO
	sess, err := session.NewSession(&aws.Config{
		Region:           aws.String("us-east-1"),
		Endpoint:         aws.String(minioEndpoint),
		S3ForcePathStyle: aws.Bool(true),
		DisableSSL:       aws.Bool(true),
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &StorageVerifier{
		s3Client:    s3.New(sess),
		bucketName:  bucketName,
		deeplakeURL: deeplakeURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// FileMetadata represents file metadata in storage
type FileMetadata struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
	ContentType  string
	Checksum     string
}

// VerifyFileInMinIO verifies that a file exists in MinIO with correct metadata
func (sv *StorageVerifier) VerifyFileInMinIO(t *testing.T, documentID, fileName string, expectedContent []byte) *FileMetadata {
	t.Helper()

	// Construct expected S3 key
	key := fmt.Sprintf("documents/%s/%s", documentID, fileName)

	// Get object metadata
	headResp, err := sv.s3Client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(sv.bucketName),
		Key:    aws.String(key),
	})
	require.NoError(t, err, "File should exist in MinIO: %s", key)

	metadata := &FileMetadata{
		Key:          key,
		Size:         aws.Int64Value(headResp.ContentLength),
		ETag:         strings.Trim(aws.StringValue(headResp.ETag), "\""),
		LastModified: aws.TimeValue(headResp.LastModified),
		ContentType:  aws.StringValue(headResp.ContentType),
	}

	// Verify file size
	require.Equal(t, int64(len(expectedContent)), metadata.Size, "File size should match")

	// Get and verify file content
	getResp, err := sv.s3Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(sv.bucketName),
		Key:    aws.String(key),
	})
	require.NoError(t, err, "Should be able to retrieve file content")
	defer getResp.Body.Close()

	actualContent, err := io.ReadAll(getResp.Body)
	require.NoError(t, err, "Should be able to read file content")

	// Verify content integrity
	expectedChecksum := calculateMD5(expectedContent)
	actualChecksum := calculateMD5(actualContent)
	require.Equal(t, expectedChecksum, actualChecksum, "File content should match")

	metadata.Checksum = actualChecksum

	t.Logf("✅ MinIO verification passed for %s (size: %d bytes, checksum: %s)", key, metadata.Size, metadata.Checksum)
	return metadata
}

// VerifyFileNotInMinIO verifies that a file does not exist in MinIO
func (sv *StorageVerifier) VerifyFileNotInMinIO(t *testing.T, documentID, fileName string) {
	t.Helper()

	key := fmt.Sprintf("documents/%s/%s", documentID, fileName)

	_, err := sv.s3Client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(sv.bucketName),
		Key:    aws.String(key),
	})
	require.Error(t, err, "File should not exist in MinIO: %s", key)

	t.Logf("✅ MinIO verification passed - file does not exist: %s", key)
}

// EmbeddingMetadata represents embedding metadata in DeepLake
type EmbeddingMetadata struct {
	ID         string                 `json:"id"`
	DocumentID string                 `json:"document_id"`
	ChunkID    string                 `json:"chunk_id"`
	Vector     []float64              `json:"vector"`
	Metadata   map[string]interface{} `json:"metadata"`
	CreatedAt  time.Time              `json:"created_at"`
}

// VerifyEmbeddingsInDeepLake verifies that embeddings exist in DeepLake
func (sv *StorageVerifier) VerifyEmbeddingsInDeepLake(t *testing.T, documentID string, expectedChunks int) []EmbeddingMetadata {
	t.Helper()

	// Query DeepLake for embeddings
	url := fmt.Sprintf("%s/api/v1/embeddings?document_id=%s", sv.deeplakeURL, documentID)
	
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)

	resp, err := sv.httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "Should be able to query embeddings")

	// Parse response
	var embeddings []EmbeddingMetadata
	err = parseJSONResponse(resp.Body, &embeddings)
	require.NoError(t, err, "Should be able to parse embeddings response")

	// Verify embedding count
	require.GreaterOrEqual(t, len(embeddings), expectedChunks, 
		"Should have at least %d embeddings, got %d", expectedChunks, len(embeddings))

	// Verify embedding structure
	for i, embedding := range embeddings {
		require.NotEmpty(t, embedding.ID, "Embedding %d should have ID", i)
		require.Equal(t, documentID, embedding.DocumentID, "Embedding %d should have correct document ID", i)
		require.NotEmpty(t, embedding.ChunkID, "Embedding %d should have chunk ID", i)
		require.NotEmpty(t, embedding.Vector, "Embedding %d should have vector", i)
		require.Greater(t, len(embedding.Vector), 0, "Embedding %d vector should not be empty", i)
	}

	t.Logf("✅ DeepLake verification passed for document %s (%d embeddings)", documentID, len(embeddings))
	return embeddings
}

// VerifyEmbeddingSearch tests embedding similarity search
func (sv *StorageVerifier) VerifyEmbeddingSearch(t *testing.T, query string, documentID string, expectedResults int) []EmbeddingMetadata {
	t.Helper()

	// Perform similarity search
	url := fmt.Sprintf("%s/api/v1/embeddings/search", sv.deeplakeURL)
	
	searchPayload := map[string]interface{}{
		"query":       query,
		"document_id": documentID,
		"limit":       expectedResults * 2, // Request more to ensure we get expected minimum
		"threshold":   0.7,
	}

	resp, err := sv.makeJSONRequest("POST", url, searchPayload)
	require.NoError(t, err, "Should be able to perform similarity search")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "Search should succeed")

	// Parse search results
	var searchResults struct {
		Results []EmbeddingMetadata `json:"results"`
		Query   string              `json:"query"`
		Count   int                 `json:"count"`
	}
	
	err = parseJSONResponse(resp.Body, &searchResults)
	require.NoError(t, err, "Should be able to parse search results")

	// Verify search results
	require.GreaterOrEqual(t, len(searchResults.Results), expectedResults,
		"Should have at least %d search results, got %d", expectedResults, len(searchResults.Results))

	// Verify each result has similarity score
	for i, result := range searchResults.Results {
		require.NotEmpty(t, result.ID, "Search result %d should have ID", i)
		require.Equal(t, documentID, result.DocumentID, "Search result %d should match document ID", i)
		
		// Verify metadata contains similarity score
		similarity, ok := result.Metadata["similarity_score"]
		require.True(t, ok, "Search result %d should have similarity score", i)
		require.IsType(t, float64(0), similarity, "Similarity score should be float64")
	}

	t.Logf("✅ DeepLake search verification passed for query '%s' (%d results)", query, len(searchResults.Results))
	return searchResults.Results
}

// VerifyNoEmbeddingsInDeepLake verifies that no embeddings exist for a document
func (sv *StorageVerifier) VerifyNoEmbeddingsInDeepLake(t *testing.T, documentID string) {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/embeddings?document_id=%s", sv.deeplakeURL, documentID)
	
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)

	resp, err := sv.httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var embeddings []EmbeddingMetadata
		err = parseJSONResponse(resp.Body, &embeddings)
		require.NoError(t, err)
		require.Empty(t, embeddings, "Should have no embeddings for document %s", documentID)
	} else {
		// 404 is also acceptable - no embeddings found
		require.Equal(t, http.StatusNotFound, resp.StatusCode, 
			"Expected 200 (empty) or 404 for document %s embeddings", documentID)
	}

	t.Logf("✅ DeepLake verification passed - no embeddings exist for document %s", documentID)
}

// VerifyStorageCleanup verifies that files are properly cleaned up from storage
func (sv *StorageVerifier) VerifyStorageCleanup(t *testing.T, documentID string) {
	t.Helper()

	// List all objects with the document prefix
	prefix := fmt.Sprintf("documents/%s/", documentID)
	
	listResp, err := sv.s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(sv.bucketName),
		Prefix: aws.String(prefix),
	})
	require.NoError(t, err, "Should be able to list objects")

	require.Empty(t, listResp.Contents, "Should have no files remaining for document %s", documentID)

	// Also verify embeddings are cleaned up
	sv.VerifyNoEmbeddingsInDeepLake(t, documentID)

	t.Logf("✅ Storage cleanup verification passed for document %s", documentID)
}

// VerifyStorageIntegrity performs comprehensive storage integrity checks
func (sv *StorageVerifier) VerifyStorageIntegrity(t *testing.T, documentID, fileName string, originalContent []byte, expectedChunks int) {
	t.Helper()

	t.Run("MinIO File Storage", func(t *testing.T) {
		metadata := sv.VerifyFileInMinIO(t, documentID, fileName, originalContent)
		require.Greater(t, metadata.Size, int64(0), "File should have content")
		require.False(t, metadata.LastModified.IsZero(), "File should have modification time")
	})

	t.Run("DeepLake Embeddings", func(t *testing.T) {
		embeddings := sv.VerifyEmbeddingsInDeepLake(t, documentID, expectedChunks)
		require.Len(t, embeddings, expectedChunks, "Should have exact number of expected embeddings")
	})

	t.Run("Embedding Search", func(t *testing.T) {
		// Test search functionality
		searchResults := sv.VerifyEmbeddingSearch(t, "test query", documentID, 1)
		require.NotEmpty(t, searchResults, "Search should return results")
	})

	t.Logf("✅ Complete storage integrity verification passed for document %s", documentID)
}

// Helper functions

func calculateMD5(data []byte) string {
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

func parseJSONResponse(body io.Reader, target interface{}) error {
	decoder := json.NewDecoder(body)
	return decoder.Decode(target)
}

func (sv *StorageVerifier) makeJSONRequest(method, url string, payload interface{}) (*http.Response, error) {
	var body io.Reader
	
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = strings.NewReader(string(jsonData))
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return sv.httpClient.Do(req)
}