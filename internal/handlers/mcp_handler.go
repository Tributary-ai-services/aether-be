package handlers

import (
	"net/http"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MCPHandler handles MCP server management endpoints
type MCPHandler struct {
	napkinService *services.NapkinService
	cfg           *config.Config
	logger        *zap.Logger
}

// MCPServerInfo represents an MCP server in the API response
type MCPServerInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Type        string   `json:"type"`
	Version     string   `json:"version"`
	Endpoint    string   `json:"endpoint,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// MCPInvokeRequest represents a tool invocation request from the frontend
type MCPInvokeRequest struct {
	ServerID string                 `json:"server_id"`
	Tool     string                 `json:"tool"`
	Params   map[string]interface{} `json:"params"`
}

// NewMCPHandler creates a new MCP handler
func NewMCPHandler(napkinService *services.NapkinService, cfg *config.Config, log *logger.Logger) *MCPHandler {
	return &MCPHandler{
		napkinService: napkinService,
		cfg:           cfg,
		logger:        log.Logger,
	}
}

// ListServers returns all registered MCP servers
func (h *MCPHandler) ListServers(c *gin.Context) {
	servers := []MCPServerInfo{
		{
			ID:          "mcp-postgres",
			Name:        "PostgreSQL MCP",
			Description: "PostgreSQL database tools",
			Status:      "connected",
			Type:        "database",
			Version:     "1.0.0",
		},
		{
			ID:          "mcp-filesystem",
			Name:        "Filesystem MCP",
			Description: "File system operations",
			Status:      "connected",
			Type:        "filesystem",
			Version:     "1.0.0",
		},
		{
			ID:          "mcp-memory",
			Name:        "Memory MCP",
			Description: "Knowledge graph storage",
			Status:      "connected",
			Type:        "memory",
			Version:     "1.0.0",
		},
	}

	// Add Napkin server if enabled
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

	// Add Wave 1 infrastructure-aligned MCP servers
	if h.cfg != nil {
		if h.cfg.Neo4jMCP.Enabled {
			servers = append(servers, MCPServerInfo{
				ID:          "mcp-neo4j",
				Name:        "Neo4j MCP",
				Description: "Neo4j graph database Cypher queries and schema exploration",
				Status:      "connected",
				Type:        "database",
				Version:     "1.0.0",
				Endpoint:    h.cfg.Neo4jMCP.BaseURL,
				Tags:        []string{"neo4j", "graph-database", "cypher", "knowledge-graph"},
			})
		}
		if h.cfg.MinIOMCP.Enabled {
			servers = append(servers, MCPServerInfo{
				ID:          "mcp-minio",
				Name:        "MinIO MCP",
				Description: "MinIO S3-compatible object storage management",
				Status:      "connected",
				Type:        "storage",
				Version:     "1.0.0",
				Endpoint:    h.cfg.MinIOMCP.BaseURL,
				Tags:        []string{"minio", "s3", "object-storage", "buckets"},
			})
		}
		if h.cfg.KafkaMCP.Enabled {
			servers = append(servers, MCPServerInfo{
				ID:          "mcp-kafka",
				Name:        "Kafka MCP",
				Description: "Apache Kafka message broker management and monitoring",
				Status:      "connected",
				Type:        "messaging",
				Version:     "1.0.0",
				Endpoint:    h.cfg.KafkaMCP.BaseURL,
				Tags:        []string{"kafka", "messaging", "streaming", "topics", "consumer-groups"},
			})
		}
		if h.cfg.GrafanaMCP.Enabled {
			servers = append(servers, MCPServerInfo{
				ID:          "mcp-grafana",
				Name:        "Grafana MCP",
				Description: "Grafana dashboards, alerting, and observability management",
				Status:      "connected",
				Type:        "observability",
				Version:     "1.0.0",
				Endpoint:    h.cfg.GrafanaMCP.BaseURL,
				Tags:        []string{"grafana", "dashboards", "alerting", "prometheus", "loki"},
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

	switch serverID {
	case "mcp-napkin":
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
	)

	switch req.ServerID {
	case "mcp-napkin":
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

	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "Server not found or tool invocation not supported: " + req.ServerID})
	}
}
