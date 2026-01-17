package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestConfig holds test environment configuration
type TestConfig struct {
	ServerURL     string
	Neo4jURI      string
	Neo4jUsername string
	Neo4jPassword string
	RedisAddr     string
	MinioEndpoint string
	MinioAccessKey string
	MinioSecretKey string
	AudiModalURL  string
	DeepLakeURL   string
	FixturesPath  string
}

// NewTestConfig creates a new test configuration from environment variables
func NewTestConfig() *TestConfig {
	return &TestConfig{
		ServerURL:      getEnvOrDefault("SERVER_URL", "http://localhost:8080"),
		Neo4jURI:       getEnvOrDefault("NEO4J_URI", "bolt://localhost:7687"),
		Neo4jUsername:  getEnvOrDefault("NEO4J_USERNAME", "neo4j"),
		Neo4jPassword:  getEnvOrDefault("NEO4J_PASSWORD", "password"),
		RedisAddr:      getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
		MinioEndpoint:  getEnvOrDefault("MINIO_ENDPOINT", "http://localhost:9000"),
		MinioAccessKey: getEnvOrDefault("MINIO_ACCESS_KEY", "minioadmin"),
		MinioSecretKey: getEnvOrDefault("MINIO_SECRET_KEY", "minioadmin123"),
		AudiModalURL:   getEnvOrDefault("AUDIMODAL_API_URL", "http://localhost:8084"),
		DeepLakeURL:    getEnvOrDefault("DEEPLAKE_API_URL", "http://localhost:8000"),
		FixturesPath:   getEnvOrDefault("FIXTURES_PATH", "./tests/fixtures"),
	}
}

// getEnvOrDefault returns environment variable value or default if not set
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// WaitForService waits for a service to become available
func WaitForService(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	client := &http.Client{Timeout: 5 * time.Second}
	
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Service at %s not available after %v", url, timeout)
		default:
			resp, err := client.Get(url)
			if err == nil && resp.StatusCode < 500 {
				resp.Body.Close()
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(1 * time.Second)
		}
	}
}

// CleanupNeo4j removes test data from Neo4j database
func CleanupNeo4j(t *testing.T, config *TestConfig) {
	t.Helper()
	
	driver, err := neo4j.NewDriver(config.Neo4jURI, neo4j.BasicAuth(config.Neo4jUsername, config.Neo4jPassword, ""))
	require.NoError(t, err)
	defer driver.Close()
	
	session := driver.NewSession(neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close()
	
	// Clean up test data
	queries := []string{
		"MATCH (d:Document) WHERE d.tenant_id STARTS WITH 'test_' DETACH DELETE d",
		"MATCH (c:Chunk) WHERE c.tenant_id STARTS WITH 'test_' DETACH DELETE c",
		"MATCH (u:User) WHERE u.email STARTS WITH 'test@' DETACH DELETE u",
		"MATCH (n:Notebook) WHERE n.name STARTS WITH 'test_' DETACH DELETE n",
	}
	
	for _, query := range queries {
		_, err := session.WriteTransaction(func(tx neo4j.Transaction) (interface{}, error) {
			return tx.Run(query, nil)
		})
		if err != nil {
			t.Logf("Warning: Failed to clean up Neo4j with query %s: %v", query, err)
		}
	}
}

// CleanupRedis removes test data from Redis
func CleanupRedis(t *testing.T, config *TestConfig) {
	t.Helper()
	
	client := redis.NewClient(&redis.Options{
		Addr: config.RedisAddr,
		DB:   0,
	})
	defer client.Close()
	
	ctx := context.Background()
	
	// Remove test keys
	keys, err := client.Keys(ctx, "test_*").Result()
	if err != nil {
		t.Logf("Warning: Failed to get Redis test keys: %v", err)
		return
	}
	
	if len(keys) > 0 {
		err = client.Del(ctx, keys...).Err()
		if err != nil {
			t.Logf("Warning: Failed to delete Redis test keys: %v", err)
		}
	}
}

// SetupTestEnvironment prepares the test environment
func SetupTestEnvironment(t *testing.T) *TestConfig {
	t.Helper()
	
	config := NewTestConfig()
	
	// Wait for services to be available
	WaitForService(t, config.ServerURL+"/health", 30*time.Second)
	WaitForService(t, config.AudiModalURL+"/__admin/health", 30*time.Second)
	WaitForService(t, config.DeepLakeURL+"/__admin/health", 30*time.Second)
	
	// Clean up any existing test data
	CleanupNeo4j(t, config)
	CleanupRedis(t, config)
	
	return config
}

// TeardownTestEnvironment cleans up after tests
func TeardownTestEnvironment(t *testing.T, config *TestConfig) {
	t.Helper()
	
	CleanupNeo4j(t, config)
	CleanupRedis(t, config)
}

// LoadTestFixture loads a test fixture file
func LoadTestFixture(t *testing.T, config *TestConfig, filename string) []byte {
	t.Helper()
	
	fullPath := filepath.Join(config.FixturesPath, filename)
	data, err := os.ReadFile(fullPath)
	require.NoError(t, err, "Failed to load test fixture: %s", fullPath)
	
	return data
}

// CreateTestFile creates a temporary test file with given content
func CreateTestFile(t *testing.T, content []byte, filename string) string {
	t.Helper()
	
	tmpDir := t.TempDir()
	fullPath := filepath.Join(tmpDir, filename)
	
	err := os.WriteFile(fullPath, content, 0644)
	require.NoError(t, err)
	
	return fullPath
}

// AssertEventuallyTrue polls a condition until it becomes true or times out
func AssertEventuallyTrue(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	t.Helper()
	
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Condition not met within %v: %s", timeout, message)
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// MakeHTTPRequest makes an HTTP request and returns the response
func MakeHTTPRequest(t *testing.T, method, url string, body io.Reader, headers map[string]string) *http.Response {
	t.Helper()
	
	req, err := http.NewRequest(method, url, body)
	require.NoError(t, err)
	
	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	
	return resp
}

// ReadResponseBody reads and returns the response body as string
func ReadResponseBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	
	return string(body)
}

// GenerateTestTenantID generates a unique test tenant ID
func GenerateTestTenantID() string {
	return fmt.Sprintf("test_tenant_%d", time.Now().Unix())
}

// GenerateTestUserID generates a unique test user ID
func GenerateTestUserID() string {
	return fmt.Sprintf("test_user_%d", time.Now().Unix())
}

// GenerateTestNotebookID generates a unique test notebook ID
func GenerateTestNotebookID() string {
	return fmt.Sprintf("test_notebook_%d", time.Now().Unix())
}

// TestDocument represents a test document for uploading
type TestDocument struct {
	Name        string
	Content     []byte
	MimeType    string
	Strategy    string
	ExpectedChunks int
	ExpectedQuality float64
}

// GetTestDocuments returns a collection of test documents for different scenarios
func GetTestDocuments() map[string]TestDocument {
	return map[string]TestDocument{
		"pdf_simple": {
			Name:        "simple_text.pdf",
			MimeType:    "application/pdf",
			Strategy:    "semantic",
			ExpectedChunks: 5,
			ExpectedQuality: 0.8,
		},
		"pdf_complex": {
			Name:        "complex_layout.pdf",
			MimeType:    "application/pdf",
			Strategy:    "adaptive",
			ExpectedChunks: 8,
			ExpectedQuality: 0.75,
		},
		"docx_sample": {
			Name:        "sample_document.docx",
			MimeType:    "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			Strategy:    "semantic",
			ExpectedChunks: 6,
			ExpectedQuality: 0.85,
		},
		"csv_sample": {
			Name:        "sample_data.csv",
			MimeType:    "text/csv",
			Strategy:    "row_based",
			ExpectedChunks: 3,
			ExpectedQuality: 0.9,
		},
		"txt_sample": {
			Name:        "plain_text.txt",
			MimeType:    "text/plain",
			Strategy:    "fixed",
			ExpectedChunks: 4,
			ExpectedQuality: 0.85,
		},
	}
}

// PerformanceMetrics holds performance measurement data
type PerformanceMetrics struct {
	UploadTime    time.Duration
	ProcessingTime time.Duration
	ChunkTime     time.Duration
	StorageTime   time.Duration
	SearchTime    time.Duration
}

// MeasurePerformance measures execution time of a function
func MeasurePerformance(t *testing.T, operation string, fn func()) time.Duration {
	t.Helper()
	
	start := time.Now()
	fn()
	duration := time.Since(start)
	
	t.Logf("Performance: %s took %v", operation, duration)
	return duration
}

// ParseJSONResponse parses JSON response body into target interface
func ParseJSONResponse(body io.Reader, target interface{}) error {
	decoder := json.NewDecoder(body)
	return decoder.Decode(target)
}

// ValidatePerformanceBenchmarks validates that performance meets benchmarks
func ValidatePerformanceBenchmarks(t *testing.T, metrics PerformanceMetrics) {
	t.Helper()
	
	// Performance benchmarks from test config
	benchmarks := map[string]time.Duration{
		"upload":     2 * time.Second,
		"processing": 30 * time.Second,
		"chunking":   10 * time.Second,
		"storage":    5 * time.Second,
		"search":     1 * time.Second,
	}
	
	if metrics.UploadTime > benchmarks["upload"] {
		t.Errorf("Upload time %v exceeds benchmark %v", metrics.UploadTime, benchmarks["upload"])
	}
	
	if metrics.ProcessingTime > benchmarks["processing"] {
		t.Errorf("Processing time %v exceeds benchmark %v", metrics.ProcessingTime, benchmarks["processing"])
	}
	
	if metrics.ChunkTime > benchmarks["chunking"] {
		t.Errorf("Chunking time %v exceeds benchmark %v", metrics.ChunkTime, benchmarks["chunking"])
	}
	
	if metrics.StorageTime > benchmarks["storage"] {
		t.Errorf("Storage time %v exceeds benchmark %v", metrics.StorageTime, benchmarks["storage"])
	}
	
	if metrics.SearchTime > benchmarks["search"] {
		t.Errorf("Search time %v exceeds benchmark %v", metrics.SearchTime, benchmarks["search"])
	}
}