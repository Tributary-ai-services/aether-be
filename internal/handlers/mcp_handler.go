package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/events"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-shared/go-events/payloads"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MCPHandler handles MCP server management endpoints
type MCPHandler struct {
	napkinService   *services.NapkinService
	databaseService *services.DatabaseService
	neo4jQuerySvc   *services.Neo4jQueryService
	mcpClients      map[string]*services.MCPClientService
	serverInfos     []MCPServerInfo
	cfg             *config.Config
	logger          *zap.Logger
	mcpPublisher    *events.MCPPublisher

	// Health cache with TTL
	healthMu    sync.RWMutex
	healthCache map[string]healthCacheEntry
}

// SetMCPPublisher wires the CloudEvents publisher used to emit
// com.tas.activity.mcp.tool_invoked. Pass nil to disable publishing —
// MCPPublisher methods no-op on a nil receiver.
func (h *MCPHandler) SetMCPPublisher(p *events.MCPPublisher) {
	h.mcpPublisher = p
}

// healthCacheEntry holds a cached health check result with expiry
type healthCacheEntry struct {
	status    string
	checkedAt time.Time
}

const healthCacheTTL = 30 * time.Second

// MCPServerInfo represents an MCP server in the API response
type MCPServerInfo struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	Status             string   `json:"status"`
	Type               string   `json:"type"`
	Version            string   `json:"version"`
	Endpoint           string   `json:"endpoint,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	SupportsConnection bool     `json:"supports_connection,omitempty"`
	ConnectionType     string   `json:"connection_type,omitempty"`
}

// MCPInvokeRequest represents a tool invocation request from the frontend
type MCPInvokeRequest struct {
	ServerID     string                 `json:"server_id"`
	Tool         string                 `json:"tool"`
	Params       map[string]interface{} `json:"params"`
	ConnectionID string                 `json:"connection_id,omitempty"`
}

// connectionInfo holds resolved credentials for an MCP server connection
type connectionInfo struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
	Protocol string `json:"protocol,omitempty"`
	SSLMode  string `json:"ssl_mode,omitempty"`
}

// TestConnectionRequest is the request body for test-connection endpoint
type TestConnectionRequest struct {
	ConnectionID string `json:"connection_id"`
}

// TestConnectionResponse is the response body for test-connection endpoint
type TestConnectionResponse struct {
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// infrastructureServerTypes maps MCP server IDs to their database types for connection resolution
var infrastructureServerTypes = map[string]string{
	"mcp-neo4j":    "neo4j",
	"mcp-postgres": "postgres",
	"mcp-minio":    "minio",
	"mcp-kafka":    "kafka",
	"mcp-grafana":  "grafana",
}

// testToolDefs maps server types to their lightweight test tool invocations
var testToolDefs = map[string]struct {
	tool   string
	params map[string]interface{}
}{
	"neo4j":    {tool: "execute_query", params: map[string]interface{}{"query": "RETURN 1 AS n"}},
	"postgres": {tool: "query", params: map[string]interface{}{"sql": "SELECT 1"}},
	"minio":    {tool: "list_buckets", params: map[string]interface{}{}},
	"kafka":    {tool: "list_topics", params: map[string]interface{}{}},
	"grafana":  {tool: "search_dashboards", params: map[string]interface{}{"query": ""}},
}

// mcpServerDef defines a server to register
type mcpServerDef struct {
	id          string
	name        string
	description string
	serverType  string
	tags        []string
	cfgGetter   func(*config.Config) *config.MCPServerConfig
}

// NewMCPHandler creates a new MCP handler with all configured MCP server clients
func NewMCPHandler(napkinService *services.NapkinService, databaseService *services.DatabaseService, neo4jQuerySvc *services.Neo4jQueryService, cfg *config.Config, log *logger.Logger) *MCPHandler {
	h := &MCPHandler{
		napkinService:   napkinService,
		databaseService: databaseService,
		neo4jQuerySvc:   neo4jQuerySvc,
		mcpClients:      make(map[string]*services.MCPClientService),
		cfg:             cfg,
		logger:          log.Logger,
		healthCache:     make(map[string]healthCacheEntry),
	}

	// Static servers (always present — these don't require bridge services)
	h.serverInfos = []MCPServerInfo{
		{ID: "mcp-filesystem", Name: "Filesystem MCP", Description: "File system operations", Status: "connected", Type: "filesystem", Version: "1.0.0"},
		{ID: "mcp-memory", Name: "Memory MCP", Description: "Knowledge graph storage", Status: "connected", Type: "memory", Version: "1.0.0"},
	}

	// Dynamic MCP servers registered via config
	defs := []mcpServerDef{
		{"mcp-postgres", "PostgreSQL MCP", "PostgreSQL database query and schema exploration", "database", []string{"postgres", "sql", "database"}, func(c *config.Config) *config.MCPServerConfig { return &c.PostgresMCP }},
		{"mcp-neo4j", "Neo4j MCP", "Neo4j graph database Cypher queries and schema exploration", "database", []string{"neo4j", "graph-database", "cypher"}, func(c *config.Config) *config.MCPServerConfig { return &c.Neo4jMCP }},
		{"mcp-minio", "MinIO MCP", "MinIO S3-compatible object storage management", "storage", []string{"minio", "s3", "object-storage"}, func(c *config.Config) *config.MCPServerConfig { return &c.MinIOMCP }},
		{"mcp-kafka", "Kafka MCP", "Apache Kafka message broker management", "messaging", []string{"kafka", "messaging", "streaming"}, func(c *config.Config) *config.MCPServerConfig { return &c.KafkaMCP }},
		{"mcp-grafana", "Grafana MCP", "Grafana dashboards and observability", "observability", []string{"grafana", "dashboards", "alerting"}, func(c *config.Config) *config.MCPServerConfig { return &c.GrafanaMCP }},
		{"mcp-brave-search", "Brave Search MCP", "Privacy-focused web search via Brave", "search", []string{"search", "web", "brave", "privacy"}, func(c *config.Config) *config.MCPServerConfig { return &c.BraveSearchMCP }},
		{"mcp-firecrawl", "Firecrawl MCP", "Web scraping and crawling via Firecrawl", "web-scraping", []string{"web-scraping", "firecrawl", "crawling"}, func(c *config.Config) *config.MCPServerConfig { return &c.FirecrawlMCP }},
		{"mcp-atlassian", "Atlassian MCP", "Jira and Confluence integration", "productivity", []string{"atlassian", "jira", "confluence"}, func(c *config.Config) *config.MCPServerConfig { return &c.AtlassianMCP }},
		{"mcp-context7", "Context7 MCP", "Library documentation and code examples", "documentation", []string{"documentation", "context7", "libraries"}, func(c *config.Config) *config.MCPServerConfig { return &c.Context7MCP }},
		{"mcp-sequential-thinking", "Sequential Thinking MCP", "Structured problem-solving and analysis", "reasoning", []string{"reasoning", "thinking", "analysis"}, func(c *config.Config) *config.MCPServerConfig { return &c.SequentialThinkingMCP }},
		{"mcp-perplexity", "Perplexity MCP", "AI-powered research and search", "search", []string{"search", "perplexity", "ai-search"}, func(c *config.Config) *config.MCPServerConfig { return &c.PerplexityMCP }},
		{"mcp-slack", "Slack MCP", "Slack workspace communication", "communication", []string{"slack", "messaging", "communication"}, func(c *config.Config) *config.MCPServerConfig { return &c.SlackMCP }},
		{"mcp-paper-search", "Paper Search MCP", "Academic paper search and retrieval", "research", []string{"research", "papers", "academic", "arxiv"}, func(c *config.Config) *config.MCPServerConfig { return &c.PaperSearchMCP }},
		{"mcp-assembler", "Assembler MCP", "Document assembly, template rendering, and format conversion (Markdown, DOCX, PDF)", "document-assembly", []string{"assembler", "templates", "documents", "reports", "formatting"}, func(c *config.Config) *config.MCPServerConfig { return &c.AssemblerMCP }},
		{"mcp-podcast", "Podcast MCP", "Podcast generation with multi-voice TTS, FFmpeg post-production, and music mixing", "media-generation", []string{"podcast", "tts", "audio", "speech", "media"}, func(c *config.Config) *config.MCPServerConfig { return &c.PodcastMCP }},
	}

	if cfg != nil {
		for _, def := range defs {
			mcpCfg := def.cfgGetter(cfg)
			if mcpCfg.Enabled {
				client := services.NewMCPClientService(def.id, mcpCfg, log)
				h.mcpClients[def.id] = client
				info := MCPServerInfo{
					ID:          def.id,
					Name:        def.name,
					Description: def.description,
					Status:      "unknown", // Will be resolved by health check
					Type:        def.serverType,
					Version:     "1.0.0",
					Endpoint:    mcpCfg.BaseURL,
					Tags:        def.tags,
				}
				if connType, ok := infrastructureServerTypes[def.id]; ok {
					info.SupportsConnection = true
					info.ConnectionType = connType
				}
				h.serverInfos = append(h.serverInfos, info)
			}
		}
	}

	return h
}

// getHealthStatus returns the cached health status for a server, or checks it if expired
func (h *MCPHandler) getHealthStatus(serverID string) string {
	h.healthMu.RLock()
	entry, ok := h.healthCache[serverID]
	h.healthMu.RUnlock()

	if ok && time.Since(entry.checkedAt) < healthCacheTTL {
		return entry.status
	}

	// Cache miss or expired — check health
	status := "disconnected"
	client, hasClient := h.mcpClients[serverID]
	if hasClient {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := client.HealthCheck(ctx); err == nil {
			status = "connected"
		}
	}

	h.healthMu.Lock()
	h.healthCache[serverID] = healthCacheEntry{status: status, checkedAt: time.Now()}
	h.healthMu.Unlock()

	return status
}

// ListServers returns all registered MCP servers
func (h *MCPHandler) ListServers(c *gin.Context) {
	typeFilter := c.Query("type")

	servers := make([]MCPServerInfo, 0, len(h.serverInfos)+1)
	for _, info := range h.serverInfos {
		if typeFilter != "" && info.Type != typeFilter {
			continue
		}
		srv := info
		// Only run health checks for servers that have MCP clients (dynamic/bridge servers)
		if _, hasClient := h.mcpClients[srv.ID]; hasClient {
			srv.Status = h.getHealthStatus(srv.ID)
		}
		servers = append(servers, srv)
	}

	// Add Napkin server if enabled (uses its own service type)
	if h.napkinService != nil && h.napkinService.IsEnabled() {
		if typeFilter == "" || typeFilter == "visual-generation" {
			status := "disconnected"
			if err := h.napkinService.HealthCheck(c.Request.Context()); err == nil {
				status = "connected"
			}
			servers = append(servers, MCPServerInfo{
				ID:          "mcp-napkin",
				Name:        "Napkin AI MCP",
				Description: "Visual generation from text using Napkin AI with MinIO storage",
				Status:      status,
				Type:        "visual-generation",
				Version:     "1.0.0",
				Tags:        []string{"napkin-ai", "visual-generation", "svg", "png", "minio"},
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"servers": servers,
	})
}

// ListTools returns tools for a specific MCP server
func (h *MCPHandler) ListTools(c *gin.Context) {
	serverID := c.Param("id")

	// Check Napkin first (special service)
	if serverID == "mcp-napkin" {
		if h.napkinService == nil || !h.napkinService.IsEnabled() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Napkin service is not available"})
			return
		}
		tools, err := h.napkinService.ListTools(c.Request.Context())
		if err != nil {
			h.logger.Error("Failed to list Napkin tools", zap.Error(err))
			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to get tools from Napkin MCP: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tools": tools})
		return
	}

	// Dynamic servers — tool discovery through MCPClientService.ListTools()
	if client, ok := h.mcpClients[serverID]; ok {
		tools, err := client.ListTools(c.Request.Context())
		if err != nil {
			h.logger.Error("Failed to list tools", zap.String("server", serverID), zap.Error(err))
			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to get tools: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tools": tools})
		return
	}

	// Static server tool definitions (these don't have bridge services)
	switch serverID {
	case "mcp-filesystem":
		c.JSON(http.StatusOK, gin.H{
			"tools": []map[string]interface{}{
				{"name": "read_file", "description": "Read file contents", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string", "description": "File path"}}, "required": []string{"path"}}},
				{"name": "list_directory", "description": "List directory contents", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string", "description": "Directory path"}}, "required": []string{"path"}}},
			},
		})
	case "mcp-memory":
		c.JSON(http.StatusOK, gin.H{
			"tools": []map[string]interface{}{
				{"name": "create_entities", "description": "Create entities in knowledge graph", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"entities": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}}}, "required": []string{"entities"}}},
				{"name": "search_nodes", "description": "Search knowledge graph nodes", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string", "description": "Search query"}}, "required": []string{"query"}}},
			},
		})
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found: " + serverID})
	}
}

// InvokeTool invokes a tool on an MCP server
func (h *MCPHandler) InvokeTool(c *gin.Context) {
	var req MCPInvokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	h.logger.Info("MCP tool invocation",
		zap.String("server_id", req.ServerID),
		zap.String("tool", req.Tool),
		zap.String("connection_id", req.ConnectionID),
	)

	tenantID := stringFromCtx(c, "tenant_id")
	userID := stringFromCtx(c, "user_id")
	requestID := c.GetHeader("X-Request-ID")
	start := time.Now()

	// If a connectionId was provided and this is an infrastructure server, resolve credentials
	if req.ConnectionID != "" {
		if _, isInfra := infrastructureServerTypes[req.ServerID]; isInfra {
			if err := h.injectConnectionParams(c, &req); err != nil {
				return // error response already sent
			}
		}
	}

	// Check Napkin first (special service)
	if req.ServerID == "mcp-napkin" {
		if h.napkinService == nil || !h.napkinService.IsEnabled() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Napkin service is not available"})
			return
		}
		result, err := h.napkinService.InvokeTool(c.Request.Context(), req.Tool, req.Params)
		h.publishToolInvoked(c.Request.Context(), tenantID, userID, requestID, req, start, err)
		if err != nil {
			h.logger.Error("Failed to invoke Napkin tool", zap.Error(err), zap.String("tool", req.Tool))
			c.JSON(http.StatusBadGateway, gin.H{"error": "Tool invocation failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
		return
	}

	// Check generic MCP clients
	if client, ok := h.mcpClients[req.ServerID]; ok {
		result, err := client.InvokeTool(c.Request.Context(), req.Tool, req.Params)
		h.publishToolInvoked(c.Request.Context(), tenantID, userID, requestID, req, start, err)
		if err != nil {
			h.logger.Error("Failed to invoke tool", zap.String("server", req.ServerID), zap.Error(err), zap.String("tool", req.Tool))
			c.JSON(http.StatusBadGateway, gin.H{"error": "Tool invocation failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
		return
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "Server not found or tool invocation not supported: " + req.ServerID})
}

// publishToolInvoked emits com.tas.activity.mcp.tool_invoked.
func (h *MCPHandler) publishToolInvoked(ctx context.Context, tenantID, userID, requestID string, req MCPInvokeRequest, start time.Time, err error) {
	payload := payloads.MCPToolInvoked{
		ServerID:   req.ServerID,
		ToolName:   req.Tool,
		DurationMS: time.Since(start).Milliseconds(),
		Success:    err == nil,
	}
	if err != nil {
		payload.Error = err.Error()
	}
	h.mcpPublisher.PublishToolInvoked(ctx, tenantID, userID, requestID, payload)
}

// stringFromCtx pulls a Gin context key as a string, returning "" if absent.
func stringFromCtx(c *gin.Context, key string) string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// TestConnection tests connectivity through an MCP server's bridge using a lightweight tool invocation
func (h *MCPHandler) TestConnection(c *gin.Context) {
	serverID := c.Param("id")

	var req TestConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	// Look up the infrastructure type for this server
	connType, isInfra := infrastructureServerTypes[serverID]
	if !isInfra {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Server does not support connection testing: " + serverID})
		return
	}

	// Look up the test tool definition
	testDef, ok := testToolDefs[connType]
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No test tool defined for type: " + connType})
		return
	}

	// Get the MCP client
	client, ok := h.mcpClients[serverID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found: " + serverID})
		return
	}

	// Build an invoke request to resolve credentials
	invokeReq := MCPInvokeRequest{
		ServerID:     serverID,
		Tool:         testDef.tool,
		Params:       make(map[string]interface{}),
		ConnectionID: req.ConnectionID,
	}

	// Copy test params
	for k, v := range testDef.params {
		invokeReq.Params[k] = v
	}

	// Inject connection credentials if provided
	if req.ConnectionID != "" {
		if err := h.injectConnectionParams(c, &invokeReq); err != nil {
			return // error response already sent
		}
	}

	// Invoke the test tool and measure latency
	start := time.Now()
	_, err := client.InvokeTool(c.Request.Context(), invokeReq.Tool, invokeReq.Params)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		h.logger.Warn("Connection test failed",
			zap.String("server", serverID),
			zap.String("connection_id", req.ConnectionID),
			zap.Error(err),
		)
		c.JSON(http.StatusOK, TestConnectionResponse{
			Success:   false,
			LatencyMs: latency,
			Error:     err.Error(),
		})
		return
	}

	h.logger.Info("Connection test succeeded",
		zap.String("server", serverID),
		zap.String("connection_id", req.ConnectionID),
		zap.Int64("latency_ms", latency),
	)
	c.JSON(http.StatusOK, TestConnectionResponse{
		Success:   true,
		LatencyMs: latency,
	})
}

// injectConnectionParams resolves a saved connection and injects credentials into the tool params.
func (h *MCPHandler) injectConnectionParams(c *gin.Context, req *MCPInvokeRequest) error {
	if h.databaseService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database service not available for connection resolution"})
		return fmt.Errorf("database service nil")
	}

	// Extract tenant ID from space context (set by SpaceContextMiddleware)
	tenantStr := "default"
	if sc, exists := c.Get(middleware.SpaceContextKey); exists {
		if spaceCtx, ok := sc.(*models.SpaceContext); ok && spaceCtx.TenantID != "" {
			tenantStr = spaceCtx.TenantID
		}
	}

	ctx := c.Request.Context()

	// Resolve the saved database connection
	db, err := h.databaseService.GetDatabase(ctx, req.ConnectionID, tenantStr)
	if err != nil {
		h.logger.Error("Failed to resolve connection",
			zap.String("connection_id", req.ConnectionID),
			zap.Error(err),
		)
		c.JSON(http.StatusNotFound, gin.H{"error": "Connection not found: " + err.Error()})
		return err
	}

	// Read credentials from K8s Secret
	username, password, err := h.neo4jQuerySvc.GetCredentials(ctx, db)
	if err != nil {
		h.logger.Error("Failed to read connection credentials",
			zap.String("connection_id", req.ConnectionID),
			zap.String("secret_name", db.SecretName),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read connection credentials"})
		return err
	}

	// Build connection info and inject into params
	conn := connectionInfo{
		Host:     db.Host,
		Port:     db.Port,
		Username: username,
		Password: password,
		Database: db.Database,
		Protocol: db.Protocol,
		SSLMode:  db.SSLMode,
	}

	if req.Params == nil {
		req.Params = make(map[string]interface{})
	}
	req.Params["_connection"] = conn

	h.logger.Info("Injected connection credentials",
		zap.String("connection_id", req.ConnectionID),
		zap.String("host", db.Host),
		zap.Int("port", db.Port),
		zap.String("database", db.Database),
	)

	return nil
}
