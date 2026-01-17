package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// DeepLakeService implements VectorStoreService for DeepLake integration
type DeepLakeService struct {
	baseURL    string
	httpClient *http.Client
	log        *logger.Logger
	config     *config.DeepLakeConfig
}

// DeepLakeCollection represents a DeepLake collection
type DeepLakeCollection struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"schema"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// DeepLakeVector represents a vector in DeepLake
type DeepLakeVector struct {
	ID        string                 `json:"id"`
	Vector    []float32              `json:"vector"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
}

// DeepLakeSearchRequest represents a search request to DeepLake
type DeepLakeSearchRequest struct {
	Vector     []float32              `json:"vector"`
	TopK       int                    `json:"top_k"`
	Filter     map[string]interface{} `json:"filter,omitempty"`
	Threshold  float64                `json:"threshold,omitempty"`
}

// DeepLakeSearchResponse represents search results from DeepLake
type DeepLakeSearchResponse struct {
	Results []DeepLakeSearchResult `json:"results"`
	Count   int                    `json:"count"`
	TimeTaken string               `json:"time_taken"`
}

// DeepLakeSearchResult represents a single search result
type DeepLakeSearchResult struct {
	ID       string                 `json:"id"`
	Score    float64                `json:"score"`
	Vector   []float32              `json:"vector,omitempty"`
	Metadata map[string]interface{} `json:"metadata"`
}

// NewDeepLakeService creates a new DeepLake service
func NewDeepLakeService(config *config.DeepLakeConfig, log *logger.Logger) *DeepLakeService {
	return &DeepLakeService{
		baseURL: config.BaseURL,
		httpClient: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
		log:    log,
		config: config,
	}
}

// Initialize creates the collection if it doesn't exist
func (s *DeepLakeService) Initialize(ctx context.Context) error {
	// Check if collection exists
	exists, err := s.collectionExists(ctx, s.config.CollectionName)
	if err != nil {
		return fmt.Errorf("failed to check collection existence: %w", err)
	}

	if !exists {
		// Create collection
		if err := s.createCollection(ctx, s.config.CollectionName); err != nil {
			return fmt.Errorf("failed to create collection: %w", err)
		}
		s.log.Info("Created DeepLake collection", zap.String("collection", s.config.CollectionName))
	}

	return nil
}

// StoreEmbedding stores an embedding in DeepLake
func (s *DeepLakeService) StoreEmbedding(ctx context.Context, chunkID string, embedding []float32, metadata map[string]interface{}) error {
	vector := DeepLakeVector{
		ID:       chunkID,
		Vector:   embedding,
		Metadata: metadata,
	}

	payload, err := json.Marshal(vector)
	if err != nil {
		return fmt.Errorf("failed to marshal vector: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/collections/%s/vectors", s.baseURL, s.config.CollectionName)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to store embedding: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		s.log.Error("Failed to store embedding in DeepLake",
			zap.String("chunk_id", chunkID),
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", string(body)),
		)
		return errors.NewAPIError(
			errors.ErrInternal,
			"Failed to store embedding in vector database",
			map[string]interface{}{
				"chunk_id":    chunkID,
				"status_code": resp.StatusCode,
			},
		)
	}

	s.log.Debug("Successfully stored embedding",
		zap.String("chunk_id", chunkID),
		zap.Int("dimensions", len(embedding)),
	)

	return nil
}

// SearchSimilar performs similarity search in DeepLake
func (s *DeepLakeService) SearchSimilar(ctx context.Context, queryEmbedding []float32, limit int, threshold float64) ([]VectorSearchResult, error) {
	searchReq := DeepLakeSearchRequest{
		Vector:    queryEmbedding,
		TopK:      limit,
		Threshold: threshold,
	}

	payload, err := json.Marshal(searchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/collections/%s/search", s.baseURL, s.config.CollectionName)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed with status %d: %s", resp.StatusCode, string(body))
	}

	var searchResp DeepLakeSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	// Convert to VectorSearchResult format
	results := make([]VectorSearchResult, len(searchResp.Results))
	for i, result := range searchResp.Results {
		results[i] = VectorSearchResult{
			ChunkID:   result.ID,
			Score:     result.Score,
			Metadata:  result.Metadata,
			Embedding: result.Vector,
		}
	}

	s.log.Debug("Similarity search completed",
		zap.Int("results_count", len(results)),
		zap.String("time_taken", searchResp.TimeTaken),
	)

	return results, nil
}

// DeleteEmbedding removes an embedding from DeepLake
func (s *DeepLakeService) DeleteEmbedding(ctx context.Context, chunkID string) error {
	url := fmt.Sprintf("%s/api/v1/collections/%s/vectors/%s", s.baseURL, s.config.CollectionName, chunkID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete embedding: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 && resp.StatusCode != 404 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed with status %d: %s", resp.StatusCode, string(body))
	}

	s.log.Debug("Successfully deleted embedding", zap.String("chunk_id", chunkID))
	return nil
}

// HealthCheck verifies DeepLake service health
func (s *DeepLakeService) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/__admin/health", s.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DeepLake health check failed with status: %d", resp.StatusCode)
	}

	return nil
}

// GetCollectionInfo retrieves information about the collection
func (s *DeepLakeService) GetCollectionInfo(ctx context.Context) (*DeepLakeCollection, error) {
	url := fmt.Sprintf("%s/api/v1/collections/%s", s.baseURL, s.config.CollectionName)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get collection info failed with status %d: %s", resp.StatusCode, string(body))
	}

	var collection DeepLakeCollection
	if err := json.NewDecoder(resp.Body).Decode(&collection); err != nil {
		return nil, fmt.Errorf("failed to decode collection info: %w", err)
	}

	return &collection, nil
}

// collectionExists checks if a collection exists
func (s *DeepLakeService) collectionExists(ctx context.Context, name string) (bool, error) {
	url := fmt.Sprintf("%s/api/v1/collections/%s", s.baseURL, name)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to check collection: %w", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// createCollection creates a new collection
func (s *DeepLakeService) createCollection(ctx context.Context, name string) error {
	collection := map[string]interface{}{
		"name":        name,
		"description": "Aether document chunks vector storage",
		"schema": map[string]interface{}{
			"vector": map[string]interface{}{
				"type":       "tensor",
				"dtype":      "float32",
				"dimensions": s.config.VectorDimensions,
			},
			"metadata": map[string]interface{}{
				"type": "json",
			},
		},
	}

	payload, err := json.Marshal(collection)
	if err != nil {
		return fmt.Errorf("failed to marshal collection: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/collections", s.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create collection failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}