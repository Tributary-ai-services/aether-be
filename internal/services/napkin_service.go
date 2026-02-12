package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"go.uber.org/zap"
)

// NapkinService handles communication with Napkin AI MCP server
type NapkinService struct {
	baseURL string
	enabled bool
	client  *http.Client
	logger  *zap.Logger
}

// NapkinToolInfo represents a tool exposed by the Napkin MCP server
type NapkinToolInfo struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// NapkinToolsResponse represents the response from /mcp/tools/list
type NapkinToolsResponse struct {
	Tools []NapkinToolInfo `json:"tools"`
}

// NapkinToolCallRequest represents a tool invocation request
type NapkinToolCallRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// NapkinToolCallResponse represents a tool invocation response
type NapkinToolCallResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// NewNapkinService creates a new Napkin MCP service client
func NewNapkinService(cfg *config.NapkinConfig, log *logger.Logger) *NapkinService {
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 120
	}

	return &NapkinService{
		baseURL: cfg.BaseURL,
		enabled: cfg.Enabled,
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
		logger: log.Logger,
	}
}

// IsEnabled returns whether the Napkin service is enabled
func (s *NapkinService) IsEnabled() bool {
	return s.enabled
}

// GetBaseURL returns the base URL
func (s *NapkinService) GetBaseURL() string {
	return s.baseURL
}

// ListTools returns the list of tools from the Napkin MCP server
func (s *NapkinService) ListTools(ctx context.Context) ([]NapkinToolInfo, error) {
	if !s.enabled {
		return nil, fmt.Errorf("napkin service is not enabled")
	}

	url := s.baseURL + "/mcp/tools/list"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("Failed to list Napkin tools", zap.Error(err))
		return nil, fmt.Errorf("napkin service unavailable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("napkin returned status %d: %s", resp.StatusCode, string(body))
	}

	var toolsResp NapkinToolsResponse
	if err := json.Unmarshal(body, &toolsResp); err != nil {
		return nil, fmt.Errorf("failed to parse tools response: %w", err)
	}

	return toolsResp.Tools, nil
}

// InvokeTool calls a tool on the Napkin MCP server
func (s *NapkinService) InvokeTool(ctx context.Context, toolName string, args map[string]interface{}) (*NapkinToolCallResponse, error) {
	if !s.enabled {
		return nil, fmt.Errorf("napkin service is not enabled")
	}

	callReq := NapkinToolCallRequest{
		Name:      toolName,
		Arguments: args,
	}

	reqBody, err := json.Marshal(callReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := s.baseURL + "/mcp/tools/call"
	s.logger.Debug("Invoking Napkin tool",
		zap.String("tool", toolName),
		zap.String("url", url),
	)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("Failed to invoke Napkin tool", zap.Error(err), zap.String("tool", toolName))
		return nil, fmt.Errorf("napkin service unavailable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("napkin returned status %d: %s", resp.StatusCode, string(body))
	}

	var callResp NapkinToolCallResponse
	if err := json.Unmarshal(body, &callResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &callResp, nil
}

// HealthCheck checks if the Napkin MCP service is healthy
func (s *NapkinService) HealthCheck(ctx context.Context) error {
	if !s.enabled {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}

	return nil
}
