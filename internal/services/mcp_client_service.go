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

// MCPClientService is a generic HTTP client for any MCP server
// that follows the TAS MCP HTTP pattern:
//   GET  /health          - Health check
//   GET  /mcp/tools/list  - List tools
//   POST /mcp/tools/call  - Call a tool
type MCPClientService struct {
	name    string
	baseURL string
	enabled bool
	client  *http.Client
	logger  *zap.Logger
}

// MCPToolInfo represents a tool exposed by an MCP server
type MCPToolInfo struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// MCPToolsResponse represents the response from /mcp/tools/list
type MCPToolsResponse struct {
	Tools []MCPToolInfo `json:"tools"`
}

// MCPToolCallRequest represents a tool invocation request
type MCPToolCallRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// MCPToolCallResponse represents a tool invocation response
type MCPToolCallResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// NewMCPClientService creates a new generic MCP client service
func NewMCPClientService(name string, cfg *config.MCPServerConfig, log *logger.Logger) *MCPClientService {
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}

	return &MCPClientService{
		name:    name,
		baseURL: cfg.BaseURL,
		enabled: cfg.Enabled,
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
		logger: log.Logger,
	}
}

// IsEnabled returns whether the service is enabled
func (s *MCPClientService) IsEnabled() bool {
	return s.enabled
}

// GetBaseURL returns the base URL
func (s *MCPClientService) GetBaseURL() string {
	return s.baseURL
}

// GetName returns the service name
func (s *MCPClientService) GetName() string {
	return s.name
}

// ListTools returns the list of tools from the MCP server
func (s *MCPClientService) ListTools(ctx context.Context) ([]MCPToolInfo, error) {
	if !s.enabled {
		return nil, fmt.Errorf("%s service is not enabled", s.name)
	}

	url := s.baseURL + "/mcp/tools/list"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("Failed to list tools", zap.String("service", s.name), zap.Error(err))
		return nil, fmt.Errorf("%s service unavailable: %w", s.name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned status %d: %s", s.name, resp.StatusCode, string(body))
	}

	var toolsResp MCPToolsResponse
	if err := json.Unmarshal(body, &toolsResp); err != nil {
		return nil, fmt.Errorf("failed to parse tools response: %w", err)
	}

	return toolsResp.Tools, nil
}

// InvokeTool calls a tool on the MCP server
func (s *MCPClientService) InvokeTool(ctx context.Context, toolName string, args map[string]interface{}) (*MCPToolCallResponse, error) {
	if !s.enabled {
		return nil, fmt.Errorf("%s service is not enabled", s.name)
	}

	callReq := MCPToolCallRequest{
		Name:      toolName,
		Arguments: args,
	}

	reqBody, err := json.Marshal(callReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := s.baseURL + "/mcp/tools/call"
	s.logger.Debug("Invoking MCP tool",
		zap.String("service", s.name),
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
		s.logger.Error("Failed to invoke tool", zap.String("service", s.name), zap.Error(err), zap.String("tool", toolName))
		return nil, fmt.Errorf("%s service unavailable: %w", s.name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned status %d: %s", s.name, resp.StatusCode, string(body))
	}

	var callResp MCPToolCallResponse
	if err := json.Unmarshal(body, &callResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &callResp, nil
}

// HealthCheck checks if the MCP service is healthy
func (s *MCPClientService) HealthCheck(ctx context.Context) error {
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
