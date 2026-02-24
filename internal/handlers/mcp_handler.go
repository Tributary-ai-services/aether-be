package handlers

import (
	"fmt"
	"net/http"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
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
}

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

// infrastructureServerTypes maps MCP server IDs to their database types for connection resolution
var infrastructureServerTypes = map[string]string{
	"mcp-neo4j":    "neo4j",
	"mcp-postgres": "postgres",
	"mcp-minio":    "minio",
	"mcp-kafka":    "kafka",
	"mcp-grafana":  "grafana",
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
	}

	// Static servers (always present)
	h.serverInfos = []MCPServerInfo{
		{ID: "mcp-postgres", Name: "PostgreSQL MCP", Description: "PostgreSQL database tools", Status: "connected", Type: "database", Version: "1.0.0", SupportsConnection: true, ConnectionType: "postgres"},
		{ID: "mcp-filesystem", Name: "Filesystem MCP", Description: "File system operations", Status: "connected", Type: "filesystem", Version: "1.0.0"},
		{ID: "mcp-memory", Name: "Memory MCP", Description: "Knowledge graph storage", Status: "connected", Type: "memory", Version: "1.0.0"},
	}

	// Dynamic MCP servers registered via config
	defs := []mcpServerDef{
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
					Status:      "connected",
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

// ListServers returns all registered MCP servers
func (h *MCPHandler) ListServers(c *gin.Context) {
	servers := make([]MCPServerInfo, len(h.serverInfos))
	copy(servers, h.serverInfos)

	// Add Napkin server if enabled (uses its own service type)
	if h.napkinService != nil && h.napkinService.IsEnabled() {
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

	// Check generic MCP clients
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

	// Static server tool definitions
	switch serverID {
	case "mcp-postgres":
		c.JSON(http.StatusOK, gin.H{
			"tools": []map[string]interface{}{
				{"name": "query", "description": "Execute a SQL query", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"sql": map[string]interface{}{"type": "string", "description": "SQL query to execute"}}, "required": []string{"sql"}}},
				{"name": "list_tables", "description": "List all tables", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
			},
		})
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

// injectConnectionParams resolves a saved connection and injects credentials into the tool params.
func (h *MCPHandler) injectConnectionParams(c *gin.Context, req *MCPInvokeRequest) error {
	if h.databaseService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database service not available for connection resolution"})
		return fmt.Errorf("database service nil")
	}

	// Extract tenant ID from context (set by auth middleware)
	tenantID, _ := c.Get("tenant_id")
	tenantStr, _ := tenantID.(string)
	if tenantStr == "" {
		tenantStr = "default"
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
