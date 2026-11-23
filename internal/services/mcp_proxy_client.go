package services

// MCPProxyClient calls the tas-mcp federation proxy (Plan #22 C.6).
//
//	POST {base}/api/v1/federation/servers/{id}/invoke
//	TAS-MCP-Delegate-Token:    <aether-be's delegate credential>
//	TAS-MCP-On-Behalf-Of-Org:  <Aether organization id>
//	TAS-MCP-On-Behalf-Of-User: <caller's Keycloak sub>
//	{"id": "...", "method": "tools/call", "params": {"name": ..., "arguments": {...}}}
//
// The proxy verifies the org's AIQG link and the user's membership with
// aiqg-dashboard-be (AIQG-88), applies that tenant's server registry and
// allowed tools (C.1), and injects the tenant's stored connection (C.4). This
// client never adds a database credential to the request.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// Delegation headers (tas-mcp internal/federation/delegation.go).
const (
	delegateTokenHeader  = "TAS-MCP-Delegate-Token" // #nosec G101 -- a header name, not a credential
	onBehalfOfOrgHeader  = "TAS-MCP-On-Behalf-Of-Org"
	onBehalfOfUserHeader = "TAS-MCP-On-Behalf-Of-User"
)

// MCPProxyClient is a client for the tas-mcp proxy's invoke route.
type MCPProxyClient struct {
	baseURL string
	client  *http.Client
	cred    DelegateCredential
	logger  *zap.Logger
}

// NewMCPProxyClient builds a client from config.
func NewMCPProxyClient(cfg *config.MCPProxyConfig, log *logger.Logger) *MCPProxyClient {
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	return &MCPProxyClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		client:  &http.Client{Timeout: time.Duration(timeout) * time.Second},
		cred:    NewDelegateCredential(cfg.DelegateTokenFile, cfg.DelegateToken),
		logger:  log.Logger,
	}
}

// NewMCPProxyClientWithHTTP builds a client over a given http.Client and
// credential (tests, tools).
func NewMCPProxyClientWithHTTP(baseURL string, hc *http.Client, cred DelegateCredential, log *zap.Logger) *MCPProxyClient {
	return &MCPProxyClient{baseURL: strings.TrimRight(baseURL, "/"), client: hc, cred: cred, logger: log}
}

// proxyRequest is the proxy's MCPRequest.
type proxyRequest struct {
	ID     string                 `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// proxyResponse is the proxy's MCPResponse.
type proxyResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// maxProxyErrorPreview bounds how much of a proxy error body reaches a log.
// The proxy already redacts every injected value from its error text (C.4);
// the bound keeps an upstream's verbose error from flooding Loki.
const maxProxyErrorPreview = 256

// Invoke sends one MCP method to serverID on behalf of caller and returns the
// raw result. A call without an org and a user is never sent: unattributed,
// it would be served as "(unattributed)" on platform servers, which is never
// what an Aether user's call is.
func (c *MCPProxyClient) Invoke(ctx context.Context, caller ProxyCaller, serverID, method string, params map[string]interface{}) (json.RawMessage, error) {
	if strings.TrimSpace(caller.AetherOrgID) == "" || strings.TrimSpace(caller.UserSub) == "" {
		return nil, errors.ForbiddenWithDetails("MCP proxy call without an org and a user", map[string]any{"code": "mcp_caller_required"})
	}
	if c.cred == nil {
		return nil, errors.ServiceUnavailable("MCP delegate credential is not configured")
	}
	tok, err := c.cred.Token()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(proxyRequest{ID: uuid.NewString(), Method: method, Params: params})
	if err != nil {
		return nil, errors.InternalWithCause("Failed to marshal MCP proxy request", err)
	}
	endpoint := c.baseURL + "/api/v1/federation/servers/" + url.PathEscape(serverID) + "/invoke"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.InternalWithCause("Failed to build MCP proxy request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(delegateTokenHeader, tok)
	req.Header.Set(onBehalfOfOrgHeader, caller.AetherOrgID)
	req.Header.Set(onBehalfOfUserHeader, caller.UserSub)
	if rid, ok := ctx.Value(requestIDKey{}).(string); ok && rid != "" {
		req.Header.Set("X-Request-ID", rid)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Warn("MCP proxy unreachable", zap.String("server_id", serverID), zap.String("method", method), zap.Error(err))
		return nil, errors.ExternalService("MCP proxy unavailable", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, errors.ExternalService("Failed to read MCP proxy response", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Warn("MCP proxy refused the call",
			zap.String("server_id", serverID),
			zap.String("method", method),
			zap.Int("status", resp.StatusCode),
			zap.String("body_preview", preview(raw, maxProxyErrorPreview)),
		)
		switch resp.StatusCode {
		case http.StatusForbidden:
			// The proxy's 403s: the org is not linked or the user is not a
			// member (AIQG-88), another tenant's server, a disallowed tool, a
			// suspended account, or no stored connection on a server that
			// requires one (SEC-38). The body is not echoed to the browser.
			return nil, errors.ForbiddenWithDetails("The MCP proxy refused this call for your organization", map[string]any{
				"code":      "mcp_proxy_forbidden",
				"server_id": serverID,
			})
		case http.StatusServiceUnavailable:
			return nil, errors.ServiceUnavailable("The MCP proxy could not resolve this organization's connection; retry shortly")
		case http.StatusBadRequest, http.StatusUnauthorized:
			// Our own request was malformed or our credential was refused:
			// misconfiguration on this side, not the user's fault.
			return nil, errors.ServiceUnavailable("The MCP proxy rejected aether-be's delegated call; check the delegate credential")
		case http.StatusNotFound:
			return nil, errors.NotFoundWithDetails("MCP server not found on the proxy", map[string]any{"server_id": serverID})
		default:
			return nil, errors.ExternalService(fmt.Sprintf("MCP proxy returned status %d", resp.StatusCode), nil)
		}
	}

	var pr proxyResponse
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, errors.ExternalService("MCP proxy returned an unreadable response", err)
	}
	if pr.Error != nil {
		return nil, errors.ExternalService("MCP server error: "+preview([]byte(pr.Error.Message), maxProxyErrorPreview), nil)
	}
	return pr.Result, nil
}

// requestIDKey carries a request id into proxy calls.
type requestIDKey struct{}

// WithRequestID returns ctx carrying a request id for proxy calls.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func preview(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// ProxyToolResult is a tools/call result.
type ProxyToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// CallTool runs tools/call and decodes the result.
func (c *MCPProxyClient) CallTool(ctx context.Context, caller ProxyCaller, serverID, tool string, args map[string]interface{}) (*ProxyToolResult, error) {
	if args == nil {
		args = map[string]interface{}{}
	}
	raw, err := c.Invoke(ctx, caller, serverID, "tools/call", map[string]interface{}{"name": tool, "arguments": args})
	if err != nil {
		return nil, err
	}
	var r ProxyToolResult
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, errors.ExternalService("MCP proxy returned an unreadable tool result", err)
		}
	}
	return &r, nil
}

// ListTools runs tools/list. The proxy has already removed tools outside the
// server's allowed_tools for this tenant.
func (c *MCPProxyClient) ListTools(ctx context.Context, caller ProxyCaller, serverID string) ([]MCPToolInfo, error) {
	raw, err := c.Invoke(ctx, caller, serverID, "tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var r MCPToolsResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, errors.ExternalService("MCP proxy returned an unreadable tools list", err)
		}
	}
	return r.Tools, nil
}
