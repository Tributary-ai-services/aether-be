package utils

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// APIClient provides methods to interact with the Aether-BE API
type APIClient struct {
	BaseURL    string
	HTTPClient *http.Client
	AuthToken  string
}

// NewAPIClient creates a new API client
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetAuthToken sets the authentication token for requests
func (c *APIClient) SetAuthToken(token string) {
	c.AuthToken = token
}

// UploadRequest represents a document upload request
type UploadRequest struct {
	Name        string `json:"name"`
	Content     []byte `json:"-"`
	Strategy    string `json:"strategy,omitempty"`
	NotebookID  string `json:"notebook_id,omitempty"`
	Description string `json:"description,omitempty"`
}

// UploadResponse represents the response from document upload
type UploadResponse struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	UploadedAt    time.Time `json:"uploaded_at"`
	ProcessingJobID string  `json:"processing_job_id,omitempty"`
	Message       string    `json:"message,omitempty"`
}

// DocumentStatus represents the status of a document
type DocumentStatus struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	ProcessingJobID string    `json:"processing_job_id"`
	Progress        float64   `json:"progress"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ChunkResponse represents a document chunk
type ChunkResponse struct {
	ID            string                 `json:"id"`
	DocumentID    string                 `json:"document_id"`
	ChunkIndex    int                    `json:"chunk_index"`
	Content       string                 `json:"content"`
	StartByte     int64                  `json:"start_byte"`
	EndByte       int64                  `json:"end_byte"`
	Strategy      string                 `json:"strategy"`
	QualityScore  float64                `json:"quality_score"`
	TokenCount    int                    `json:"token_count"`
	Metadata      map[string]interface{} `json:"metadata"`
	CreatedAt     time.Time              `json:"created_at"`
}

// ChunksResponse represents a collection of chunks
type ChunksResponse struct {
	Chunks     []ChunkResponse `json:"chunks"`
	TotalCount int             `json:"total_count"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	HasMore    bool            `json:"has_more"`
}

// Strategy represents a chunking strategy
type Strategy struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Supported   []string               `json:"supported_mime_types"`
}

// StrategiesResponse represents available strategies
type StrategiesResponse struct {
	Strategies []Strategy `json:"strategies"`
}

// StrategyRecommendation represents a strategy recommendation
type StrategyRecommendation struct {
	Strategy   string                 `json:"strategy"`
	Confidence float64                `json:"confidence"`
	Reasoning  string                 `json:"reasoning"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// SearchRequest represents a chunk search request
type SearchRequest struct {
	Query      string   `json:"query"`
	Filters    map[string]interface{} `json:"filters,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Offset     int      `json:"offset,omitempty"`
	Similarity float64  `json:"similarity_threshold,omitempty"`
}

// SearchResponse represents search results
type SearchResponse struct {
	Results    []ChunkResponse `json:"results"`
	TotalCount int             `json:"total_count"`
	Query      string          `json:"query"`
	TimeTaken  string          `json:"time_taken"`
}

// UploadMultipart uploads a document using multipart form data
func (c *APIClient) UploadMultipart(ctx context.Context, req UploadRequest) (*UploadResponse, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	
	// Add file content
	fileWriter, err := writer.CreateFormFile("file", req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	
	if _, err := fileWriter.Write(req.Content); err != nil {
		return nil, fmt.Errorf("failed to write file content: %w", err)
	}
	
	// Add optional fields
	if req.Strategy != "" {
		writer.WriteField("strategy", req.Strategy)
	}
	if req.NotebookID != "" {
		writer.WriteField("notebook_id", req.NotebookID)
	}
	if req.Description != "" {
		writer.WriteField("description", req.Description)
	}
	
	writer.Close()
	
	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/v1/documents/upload", &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	if c.AuthToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	
	// Execute request
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	// Parse response
	var uploadResp UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	if resp.StatusCode >= 400 {
		return &uploadResp, fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, uploadResp.Message)
	}
	
	return &uploadResp, nil
}

// UploadBase64 uploads a document using base64 encoding
func (c *APIClient) UploadBase64(ctx context.Context, req UploadRequest) (*UploadResponse, error) {
	payload := map[string]interface{}{
		"name":    req.Name,
		"content": base64.StdEncoding.EncodeToString(req.Content),
	}
	
	if req.Strategy != "" {
		payload["strategy"] = req.Strategy
	}
	if req.NotebookID != "" {
		payload["notebook_id"] = req.NotebookID
	}
	if req.Description != "" {
		payload["description"] = req.Description
	}
	
	var result UploadResponse
	err := c.makeJSONRequest(ctx, "POST", "/api/v1/documents/upload-base64", payload, &result)
	return &result, err
}

// GetDocumentStatus retrieves the processing status of a document
func (c *APIClient) GetDocumentStatus(ctx context.Context, documentID string) (*DocumentStatus, error) {
	var result DocumentStatus
	err := c.makeJSONRequest(ctx, "GET", fmt.Sprintf("/api/v1/documents/%s/status", documentID), nil, &result)
	return &result, err
}

// GetFileChunks retrieves chunks for a specific file
func (c *APIClient) GetFileChunks(ctx context.Context, fileID string, limit, offset int) (*ChunksResponse, error) {
	url := fmt.Sprintf("/api/v1/files/%s/chunks?limit=%d&offset=%d", fileID, limit, offset)
	var result ChunksResponse
	err := c.makeJSONRequest(ctx, "GET", url, nil, &result)
	return &result, err
}

// GetChunk retrieves a specific chunk
func (c *APIClient) GetChunk(ctx context.Context, fileID, chunkID string) (*ChunkResponse, error) {
	url := fmt.Sprintf("/api/v1/files/%s/chunks/%s", fileID, chunkID)
	var result ChunkResponse
	err := c.makeJSONRequest(ctx, "GET", url, nil, &result)
	return &result, err
}

// SearchChunks searches for chunks based on query
func (c *APIClient) SearchChunks(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	var result SearchResponse
	err := c.makeJSONRequest(ctx, "POST", "/api/v1/chunks/search", req, &result)
	return &result, err
}

// GetAvailableStrategies retrieves available chunking strategies
func (c *APIClient) GetAvailableStrategies(ctx context.Context) (*StrategiesResponse, error) {
	var result StrategiesResponse
	err := c.makeJSONRequest(ctx, "GET", "/api/v1/strategies", nil, &result)
	return &result, err
}

// GetOptimalStrategy gets a strategy recommendation for content
func (c *APIClient) GetOptimalStrategy(ctx context.Context, contentType string, fileSize int64, complexity string) (*StrategyRecommendation, error) {
	payload := map[string]interface{}{
		"content_type": contentType,
		"file_size":    fileSize,
		"complexity":   complexity,
	}
	
	var result StrategyRecommendation
	err := c.makeJSONRequest(ctx, "POST", "/api/v1/strategies/recommend", payload, &result)
	return &result, err
}

// ReprocessFile reprocesses a file with a different strategy
func (c *APIClient) ReprocessFile(ctx context.Context, fileID, strategy string) (*UploadResponse, error) {
	payload := map[string]interface{}{
		"strategy": strategy,
	}
	
	var result UploadResponse
	err := c.makeJSONRequest(ctx, "POST", fmt.Sprintf("/api/v1/files/%s/reprocess", fileID), payload, &result)
	return &result, err
}

// HealthCheck performs a health check
func (c *APIClient) HealthCheck(ctx context.Context) error {
	resp, err := c.makeRequest(ctx, "GET", "/health", nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}
	
	return nil
}

// makeJSONRequest makes a JSON request and unmarshals the response
func (c *APIClient) makeJSONRequest(ctx context.Context, method, path string, payload interface{}, result interface{}) error {
	var body io.Reader
	headers := map[string]string{}
	
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewReader(jsonData)
		headers["Content-Type"] = "application/json"
	}
	
	resp, err := c.makeRequest(ctx, method, path, body, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}
	
	return nil
}

// makeRequest makes an HTTP request with proper headers
func (c *APIClient) makeRequest(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	url := c.BaseURL + path
	
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	
	// Set auth token if available
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	
	// Execute request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	
	return resp, nil
}

// MakeRequest makes a public HTTP request with proper headers (for testing)
func (c *APIClient) MakeRequest(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	return c.makeRequest(ctx, method, path, body, headers)
}

// WaitForProcessingComplete polls until document processing is complete
func (c *APIClient) WaitForProcessingComplete(ctx context.Context, documentID string, timeout time.Duration) (*DocumentStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("processing timeout after %v", timeout)
		case <-ticker.C:
			status, err := c.GetDocumentStatus(ctx, documentID)
			if err != nil {
				return nil, fmt.Errorf("failed to get document status: %w", err)
			}
			
			switch status.Status {
			case "processed", "completed":
				return status, nil
			case "failed", "error":
				return status, fmt.Errorf("processing failed: %s", status.ErrorMessage)
			}
			// Continue polling for "processing", "queued", etc.
		}
	}
}