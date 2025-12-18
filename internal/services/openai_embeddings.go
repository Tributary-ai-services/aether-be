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
)

// OpenAIEmbeddingProvider implements EmbeddingProvider for OpenAI embeddings
type OpenAIEmbeddingProvider struct {
	apiKey     string
	model      string
	dimensions int
	baseURL    string
	httpClient *http.Client
	log        *logger.Logger
}

// OpenAIEmbeddingRequest represents a request to OpenAI embeddings API
type OpenAIEmbeddingRequest struct {
	Input          interface{} `json:"input"`
	Model          string      `json:"model"`
	EncodingFormat string      `json:"encoding_format,omitempty"`
	Dimensions     int         `json:"dimensions,omitempty"`
}

// OpenAIEmbeddingResponse represents OpenAI embeddings API response
type OpenAIEmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// NewOpenAIEmbeddingProvider creates a new OpenAI embedding provider
func NewOpenAIEmbeddingProvider(config *config.OpenAIConfig, log *logger.Logger) *OpenAIEmbeddingProvider {
	dimensions := config.Dimensions
	if dimensions == 0 {
		// Default dimensions for text-embedding-ada-002
		dimensions = 1536
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	return &OpenAIEmbeddingProvider{
		apiKey:     config.APIKey,
		model:      config.Model,
		dimensions: dimensions,
		baseURL:    baseURL,
		httpClient: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
		log: log,
	}
}

// GenerateEmbedding generates an embedding for a single text
func (p *OpenAIEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, fmt.Errorf("empty text provided for embedding")
	}

	request := OpenAIEmbeddingRequest{
		Input:          text,
		Model:          p.model,
		EncodingFormat: "float",
	}

	// Set dimensions if supported by the model
	if p.supportsCustomDimensions() {
		request.Dimensions = p.dimensions
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/embeddings", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	start := time.Now()
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		p.log.Error("OpenAI embedding request failed",
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", string(body)),
		)
		return nil, fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response OpenAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned from OpenAI")
	}

	embedding := response.Data[0].Embedding
	
	p.log.Debug("Generated OpenAI embedding",
		zap.Int("dimensions", len(embedding)),
		zap.Int("prompt_tokens", response.Usage.PromptTokens),
		zap.Duration("duration", time.Since(start)),
	)

	return embedding, nil
}

// GenerateBatchEmbeddings generates embeddings for multiple texts
func (p *OpenAIEmbeddingProvider) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// Filter out empty texts
	nonEmptyTexts := make([]string, 0, len(texts))
	textIndices := make([]int, 0, len(texts))
	
	for i, text := range texts {
		if text != "" {
			nonEmptyTexts = append(nonEmptyTexts, text)
			textIndices = append(textIndices, i)
		}
	}

	if len(nonEmptyTexts) == 0 {
		// Return empty embeddings for all texts
		result := make([][]float32, len(texts))
		return result, nil
	}

	request := OpenAIEmbeddingRequest{
		Input:          nonEmptyTexts,
		Model:          p.model,
		EncodingFormat: "float",
	}

	// Set dimensions if supported by the model
	if p.supportsCustomDimensions() {
		request.Dimensions = p.dimensions
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal batch request: %w", err)
	}

	url := fmt.Sprintf("%s/embeddings", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create batch request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	start := time.Now()
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make batch request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		p.log.Error("OpenAI batch embedding request failed",
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", string(body)),
		)
		return nil, fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response OpenAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode batch response: %w", err)
	}

	if len(response.Data) != len(nonEmptyTexts) {
		return nil, fmt.Errorf("mismatch between input texts (%d) and returned embeddings (%d)",
			len(nonEmptyTexts), len(response.Data))
	}

	// Create result array with proper indexing
	result := make([][]float32, len(texts))
	
	for _, dataItem := range response.Data {
		if dataItem.Index < len(textIndices) {
			originalIndex := textIndices[dataItem.Index]
			result[originalIndex] = dataItem.Embedding
		}
	}

	p.log.Info("Generated batch embeddings",
		zap.Int("total_texts", len(texts)),
		zap.Int("processed_texts", len(nonEmptyTexts)),
		zap.Int("prompt_tokens", response.Usage.PromptTokens),
		zap.Duration("duration", time.Since(start)),
	)

	return result, nil
}

// GetDimensions returns the embedding dimensions
func (p *OpenAIEmbeddingProvider) GetDimensions() int {
	return p.dimensions
}

// GetModelName returns the model name
func (p *OpenAIEmbeddingProvider) GetModelName() string {
	return p.model
}

// supportsCustomDimensions checks if the model supports custom dimensions
func (p *OpenAIEmbeddingProvider) supportsCustomDimensions() bool {
	// Only newer embedding models support custom dimensions
	switch p.model {
	case "text-embedding-3-small", "text-embedding-3-large":
		return true
	default:
		return false
	}
}

// ValidateConfiguration validates the provider configuration
func (p *OpenAIEmbeddingProvider) ValidateConfiguration() error {
	if p.apiKey == "" {
		return fmt.Errorf("OpenAI API key is required")
	}

	if p.model == "" {
		return fmt.Errorf("OpenAI model is required")
	}

	if p.dimensions <= 0 {
		return fmt.Errorf("embedding dimensions must be positive")
	}

	return nil
}

// TestConnection tests the connection to OpenAI API
func (p *OpenAIEmbeddingProvider) TestConnection(ctx context.Context) error {
	// Test with a simple text
	_, err := p.GenerateEmbedding(ctx, "test connection")
	if err != nil {
		return fmt.Errorf("OpenAI connection test failed: %w", err)
	}

	p.log.Info("OpenAI embedding provider connection test successful")
	return nil
}