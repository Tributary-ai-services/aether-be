package handlers

// MCP calls through the tas-mcp proxy (Plan #22 C.6). Wired only when
// AETHER_MCP_VIA_PROXY is on; otherwise MCPHandler keeps its direct paths.
//
// What goes through the proxy: tools/list and tools/call for every Aether
// server that the proxy federates (config.DefaultProxyServerIDs), sent with
// aether-be's delegate credential and the caller's org and Keycloak sub
// (AIQG-88); the proxy verifies both with AIQG.
//
// What does not, and why:
//   - Napkin keeps its own service client (it uploads to MinIO itself).
//   - Servers the proxy does not federate (Brave Search, Firecrawl, Atlassian,
//     Perplexity, Slack) keep their direct paths.
//   - A call that names a saved connection on any server but postgres keeps
//     the direct path. The proxy injects stored connections only for the
//     servers AIQG holds connections for (postgres-mcp today), and sending
//     Aether's credentials through the proxy as a tool argument would put
//     them in front of its scanner and its tool-call events, which redact
//     only what the proxy injected itself.
//
// A caller-supplied _connection is never forwarded on the proxy path: the
// database a proxied call reaches is the one the tenant stored, nothing else.

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// SetProxy turns on the proxy path. ids maps Aether server ids to proxy ids.
func (h *MCPHandler) SetProxy(p *services.MCPProxyClient, ids map[string]string) {
	h.proxy = p
	h.proxyIDs = ids
}

// proxyTarget returns the proxy id for an Aether server id, when the call
// should go through the proxy.
func (h *MCPHandler) proxyTarget(serverID string) (string, bool) {
	if h.proxy == nil {
		return "", false
	}
	id, ok := h.proxyIDs[serverID]
	return id, ok && id != ""
}

// usesProxy decides the path for one call: proxied servers go through the
// proxy, except a saved connection on a server whose connections the proxy
// does not hold (see the file comment).
func (h *MCPHandler) usesProxy(serverID, connectionID string) (string, bool) {
	target, ok := h.proxyTarget(serverID)
	if !ok {
		return "", false
	}
	if connectionID != "" && serverID != "mcp-postgres" {
		return "", false
	}
	return target, true
}

// spaceFrom returns the request's space context, or nil.
func spaceFrom(c *gin.Context) *models.SpaceContext {
	if v, ok := c.Get(middleware.SpaceContextKey); ok {
		if sc, ok := v.(*models.SpaceContext); ok {
			return sc
		}
	}
	return nil
}

// proxyCaller returns who this request's proxied call acts for, or writes
// the refusal.
func (h *MCPHandler) proxyCaller(c *gin.Context) (services.ProxyCaller, bool) {
	caller, err := services.CallerFor(spaceFrom(c))
	if err != nil {
		handleServiceError(c, err)
		return services.ProxyCaller{}, false
	}
	return caller, true
}

// checkSharedConnection confirms that a postgres call's saved connection is
// in the caller's tenant and bound to the proxy's postgres server, and
// returns its bound connection id, which the proxy checks against what it
// injects (LOW-2). Nothing else from the row is sent. A call to the postgres
// server without a connection_id is refused (LOW-1): it would otherwise run
// SQL on the tenant's stored DSN with no Database row behind it. Other
// servers take no connection here and return "".
func (h *MCPHandler) checkSharedConnection(c *gin.Context, serverID, target, connectionID string) (string, bool) {
	if serverID != "mcp-postgres" {
		return "", true
	}
	if connectionID == "" {
		handleServiceError(c, errors.ValidationWithDetails("Choose a database connection: connection_id is required for PostgreSQL", map[string]any{
			"code": "connection_id_required",
		}))
		return "", false
	}
	sc := spaceFrom(c)
	if sc == nil || h.databaseService == nil {
		handleServiceError(c, errors.ForbiddenWithDetails("Space context required", map[string]any{"code": "mcp_space_required"}))
		return "", false
	}
	db, err := h.databaseService.GetDatabase(c.Request.Context(), connectionID, sc.TenantID)
	if err != nil {
		handleServiceError(c, err)
		return "", false
	}
	if db.Type != models.DatabaseTypePostgres || db.MCPConnectionID == "" || db.MCPServerID != target {
		handleServiceError(c, errors.ConflictWithDetails("This connection has not been moved to the shared connection store yet", map[string]any{
			"code":        "connection_not_shared",
			"database_id": db.ID,
		}))
		return "", false
	}
	return db.MCPConnectionID, true
}

// requirePermission refuses a request whose space lacks perm (LOW-1). Tool
// calls can have side effects, so they need write; listing and testing need
// read.
func requirePermission(c *gin.Context, perm string) bool {
	sc := spaceFrom(c)
	ok := sc != nil && (sc.HasPermission(perm) || (perm == "read" && sc.CanRead()) || (perm == "write" && sc.CanUpdate()))
	if !ok {
		handleServiceError(c, errors.ForbiddenWithDetails("You do not have "+perm+" access in this space", map[string]any{
			"code": "space_permission_required", "permission": perm,
		}))
	}
	return ok
}

// proxyListTools serves GET /mcp/servers/:id/tools through the proxy.
func (h *MCPHandler) proxyListTools(c *gin.Context, serverID, target string) {
	if !requirePermission(c, "read") {
		return
	}
	tok, ok := h.proxyCaller(c)
	if !ok {
		return
	}
	tools, err := h.proxy.ListTools(c.Request.Context(), tok, target)
	if err != nil {
		h.logger.Warn("Proxy tools/list failed", zap.String("server", serverID), zap.String("proxy_server", target), zap.Error(err))
		handleServiceError(c, err)
		return
	}
	if tools == nil {
		tools = []services.MCPToolInfo{}
	}
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

// withoutConnection copies params without a caller-supplied _connection.
func withoutConnection(params map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(params))
	for k, v := range params {
		if k == "_connection" {
			continue
		}
		out[k] = v
	}
	return out
}

// proxyInvoke serves POST /mcp/invoke through the proxy.
func (h *MCPHandler) proxyInvoke(c *gin.Context, req MCPInvokeRequest, target string) {
	tenantID := stringFromCtx(c, "tenant_id")
	userID := stringFromCtx(c, "user_id")
	requestID := c.GetHeader("X-Request-ID")
	start := time.Now()

	if !requirePermission(c, "write") {
		return
	}
	expected, ok := h.checkSharedConnection(c, req.ServerID, target, req.ConnectionID)
	if !ok {
		return
	}
	tok, ok := h.proxyCaller(c)
	if !ok {
		return
	}
	tok.ExpectedConnectionID = expected
	ctx := c.Request.Context()
	if requestID != "" {
		ctx = services.WithRequestID(ctx, requestID)
	}
	result, err := h.proxy.CallTool(ctx, tok, target, req.Tool, withoutConnection(req.Params))
	h.publishToolInvoked(ctx, tenantID, userID, requestID, req, start, err)
	if err != nil {
		h.logger.Warn("Proxy tool invocation failed", zap.String("server", req.ServerID), zap.String("proxy_server", target), zap.String("tool", req.Tool), zap.Error(err))
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// proxyTestConnection serves POST /mcp/servers/:id/test-connection for a
// proxied server.
func (h *MCPHandler) proxyTestConnection(c *gin.Context, serverID, target, connectionID, tool string, params map[string]interface{}) {
	if !requirePermission(c, "read") {
		return
	}
	expected, ok := h.checkSharedConnection(c, serverID, target, connectionID)
	if !ok {
		return
	}
	tok, ok := h.proxyCaller(c)
	if !ok {
		return
	}
	tok.ExpectedConnectionID = expected
	start := time.Now()
	res, err := h.proxy.CallTool(c.Request.Context(), tok, target, tool, params)
	latency := time.Since(start).Milliseconds()
	if err == nil && res.IsError {
		err = errors.ValidationWithDetails("Connection test failed", nil)
	}
	if err != nil {
		h.logger.Warn("Connection test via proxy failed", zap.String("server", serverID), zap.String("connection_id", connectionID), zap.Error(err))
		c.JSON(http.StatusOK, TestConnectionResponse{Success: false, LatencyMs: latency, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, TestConnectionResponse{Success: true, LatencyMs: latency})
}
