package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// AgentHandler handles agent-related HTTP requests as a proxy to agent-builder
// while managing Neo4j relationships for agents, users, teams, and knowledge sources
type AgentHandler struct {
	agentService *services.AgentService
	userService  *services.UserService
	teamService  *services.TeamService
	logger       *logger.Logger
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(
	agentService *services.AgentService,
	userService *services.UserService,
	teamService *services.TeamService,
	log *logger.Logger,
) *AgentHandler {
	return &AgentHandler{
		agentService: agentService,
		userService:  userService,
		teamService:  teamService,
		logger:       log.WithService("agent_handler"),
	}
}

// CreateAgent creates a new agent
// @Summary Create agent
// @Description Create a new agent with Neo4j relationship management and agent-builder proxy
// @Tags agents
// @Accept json
// @Produce json
// @Security Bearer
// @Param agent body models.AgentCreateRequest true "Agent data"
// @Success 201 {object} models.AgentResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents [post]
func (h *AgentHandler) CreateAgent(c *gin.Context) {
	// Resolve Keycloak ID to internal user ID
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.AgentCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Get auth token for agent-builder proxy
	authToken := extractAuthToken(c)
	if authToken == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}

	agent, err := h.agentService.CreateAgent(c.Request.Context(), req, spaceContext, authToken)
	if err != nil {
		h.logger.Error("Failed to create agent", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Agent created successfully",
		zap.String("agent_id", agent.ID),
		zap.String("agent_builder_id", agent.AgentBuilderID),
		zap.String("user_id", userID),
		zap.String("space_id", spaceContext.SpaceID),
	)

	c.JSON(http.StatusCreated, agent)
}

// GetAgent retrieves an agent by ID
// @Summary Get agent
// @Description Retrieve an agent by ID with access control
// @Tags agents
// @Produce json
// @Security Bearer
// @Param id path string true "Agent ID"
// @Success 200 {object} models.AgentResponse
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents/{id} [get]
func (h *AgentHandler) GetAgent(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get user's teams for access control
	userTeams, err := h.getUserTeams(c, userID)
	if err != nil {
		h.logger.Error("Failed to get user teams", zap.Error(err))
		userTeams = []string{} // Continue with empty teams
	}

	agent, err := h.agentService.GetAgent(c.Request.Context(), agentID, userID, userTeams)
	if err != nil {
		h.logger.Error("Failed to get agent", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, agent)
}

// UpdateAgent updates an agent
// @Summary Update agent
// @Description Update an agent with Neo4j and agent-builder sync
// @Tags agents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Agent ID"
// @Param agent body models.AgentUpdateRequest true "Agent update data"
// @Success 200 {object} models.AgentResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents/{id} [put]
func (h *AgentHandler) UpdateAgent(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.AgentUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	// Get auth token for agent-builder proxy
	authToken := extractAuthToken(c)
	if authToken == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}

	agent, err := h.agentService.UpdateAgent(c.Request.Context(), agentID, req, userID, authToken)
	if err != nil {
		h.logger.Error("Failed to update agent", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Agent updated successfully",
		zap.String("agent_id", agentID),
		zap.String("user_id", userID),
	)

	c.JSON(http.StatusOK, agent)
}

// DeleteAgent deletes an agent
// @Summary Delete agent
// @Description Delete an agent from both Neo4j and agent-builder
// @Tags agents
// @Security Bearer
// @Param id path string true "Agent ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents/{id} [delete]
func (h *AgentHandler) DeleteAgent(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Get auth token for agent-builder proxy
	authToken := extractAuthToken(c)
	if authToken == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}

	err = h.agentService.DeleteAgent(c.Request.Context(), agentID, userID, authToken)
	if err != nil {
		h.logger.Error("Failed to delete agent", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Agent deleted successfully",
		zap.String("agent_id", agentID),
		zap.String("user_id", userID),
	)

	c.Status(http.StatusNoContent)
}

// ListAgents lists agents with filtering and pagination
// @Summary List agents
// @Description List agents with filtering, pagination, and access control
// @Tags agents
// @Produce json
// @Security Bearer
// @Param query query string false "Search query"
// @Param space_id query string false "Space ID filter"
// @Param team_id query string false "Team ID filter"
// @Param status query string false "Status filter (draft, published, disabled)"
// @Param space_type query string false "Space type filter (personal, organization)"
// @Param is_public query boolean false "Public filter"
// @Param is_template query boolean false "Template filter"
// @Param tags query string false "Tags filter (comma-separated)"
// @Param limit query integer false "Limit (default 20, max 100)"
// @Param offset query integer false "Offset (default 0)"
// @Success 200 {object} models.AgentListResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents [get]
func (h *AgentHandler) ListAgents(c *gin.Context) {
	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	// Parse query parameters
	req := models.AgentSearchRequest{
		Query:     c.Query("query"),
		SpaceID:   c.Query("space_id"),
		TeamID:    c.Query("team_id"),
		SpaceType: models.SpaceType(c.Query("space_type")),
		Limit:     20, // Default limit
		Offset:    0,  // Default offset
	}

	// Parse status
	if status := c.Query("status"); status != "" {
		req.Status = models.AgentStatus(status)
	}

	// Parse boolean parameters
	if isPublic := c.Query("is_public"); isPublic != "" {
		if val, err := strconv.ParseBool(isPublic); err == nil {
			req.IsPublic = &val
		}
	}

	if isTemplate := c.Query("is_template"); isTemplate != "" {
		if val, err := strconv.ParseBool(isTemplate); err == nil {
			req.IsTemplate = &val
		}
	}

	// Parse tags
	if tags := c.Query("tags"); tags != "" {
		req.Tags = strings.Split(tags, ",")
		// Trim whitespace
		for i, tag := range req.Tags {
			req.Tags[i] = strings.TrimSpace(tag)
		}
	}

	// Parse pagination
	if limit := c.Query("limit"); limit != "" {
		if val, err := strconv.Atoi(limit); err == nil && val > 0 && val <= 100 {
			req.Limit = val
		}
	}

	if offset := c.Query("offset"); offset != "" {
		if val, err := strconv.Atoi(offset); err == nil && val >= 0 {
			req.Offset = val
		}
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid query parameters", err))
		return
	}

	// Get user's teams for access control
	userTeams, err := h.getUserTeams(c, userID)
	if err != nil {
		h.logger.Error("Failed to get user teams", zap.Error(err))
		userTeams = []string{} // Continue with empty teams
	}

	// Get auth token for agent-builder proxy
	authToken := extractAuthToken(c)
	if authToken == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}

	agents, err := h.agentService.ListAgents(c.Request.Context(), req, userID, userTeams, authToken)
	if err != nil {
		h.logger.Error("Failed to list agents", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, agents)
}

// AddKnowledgeSource adds a knowledge source (notebook) to an agent
// @Summary Add knowledge source
// @Description Link a notebook as a knowledge source for an agent
// @Tags agents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Agent ID"
// @Param knowledge_source body models.VectorSearchSpace true "Knowledge source configuration"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents/{id}/knowledge-sources [post]
func (h *AgentHandler) AddKnowledgeSource(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.VectorSearchSpace
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	err = h.agentService.AddKnowledgeSource(c.Request.Context(), agentID, req.NotebookID, userID, req)
	if err != nil {
		h.logger.Error("Failed to add knowledge source", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Knowledge source added to agent",
		zap.String("agent_id", agentID),
		zap.String("notebook_id", req.NotebookID),
		zap.String("user_id", userID),
	)

	c.JSON(http.StatusOK, gin.H{
		"message":     "Knowledge source added successfully",
		"agent_id":    agentID,
		"notebook_id": req.NotebookID,
	})
}

// RemoveKnowledgeSource removes a knowledge source from an agent
// @Summary Remove knowledge source
// @Description Unlink a notebook from an agent's knowledge sources
// @Tags agents
// @Security Bearer
// @Param id path string true "Agent ID"
// @Param notebook_id path string true "Notebook ID"
// @Success 204
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents/{id}/knowledge-sources/{notebook_id} [delete]
func (h *AgentHandler) RemoveKnowledgeSource(c *gin.Context) {
	agentID := c.Param("id")
	notebookID := c.Param("notebook_id")

	if agentID == "" || notebookID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID and Notebook ID are required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	err = h.agentService.RemoveKnowledgeSource(c.Request.Context(), agentID, notebookID, userID)
	if err != nil {
		h.logger.Error("Failed to remove knowledge source", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Knowledge source removed from agent",
		zap.String("agent_id", agentID),
		zap.String("notebook_id", notebookID),
		zap.String("user_id", userID),
	)

	c.Status(http.StatusNoContent)
}

// GetKnowledgeSources lists all knowledge sources for an agent
// @Summary Get agent knowledge sources
// @Description Retrieve all knowledge sources configured for an agent
// @Tags agents
// @Produce json
// @Security Bearer
// @Param id path string true "Agent ID"
// @Success 200 {array} models.VectorSearchSpace
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents/{id}/knowledge-sources [get]
func (h *AgentHandler) GetKnowledgeSources(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	sources, err := h.agentService.GetAgentKnowledgeSources(c.Request.Context(), agentID, userID)
	if err != nil {
		h.logger.Error("Failed to get agent knowledge sources", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, sources)
}

// ExecuteAgent executes an agent with the provided input
// @Summary Execute agent
// @Description Execute an agent with type-specific processing (Q&A, Conversational, Producer)
// @Tags agents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Agent ID"
// @Param request body models.AgentExecuteRequest true "Execution request"
// @Success 200 {object} models.AgentExecuteResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents/{id}/execute [post]
func (h *AgentHandler) ExecuteAgent(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	// Resolve user
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		h.logger.Error("Failed to resolve user", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	var req models.AgentExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	// Get auth token for agent-builder proxy
	authToken := extractAuthToken(c)
	if authToken == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}

	// Get user's teams for access control
	userTeams, err := h.getUserTeams(c, userID)
	if err != nil {
		h.logger.Error("Failed to get user teams", zap.Error(err))
		userTeams = []string{} // Continue with empty teams
	}

	response, err := h.agentService.ExecuteAgent(c.Request.Context(), agentID, req, userID, userTeams, authToken)
	if err != nil {
		h.logger.Error("Failed to execute agent", zap.Error(err))
		handleServiceError(c, err)
		return
	}

	h.logger.Info("Agent executed successfully",
		zap.String("agent_id", agentID),
		zap.String("execution_id", response.ExecutionID),
		zap.String("user_id", userID),
		zap.Int("response_time_ms", response.ResponseTimeMs),
	)

	c.JSON(http.StatusOK, response)
}

// Helper methods

// extractAuthToken extracts the Bearer token from the Authorization header
func extractAuthToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}

	// Expected format: "Bearer <token>"
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}

	return parts[1]
}

// getUserTeams retrieves team IDs for a user for access control purposes
func (h *AgentHandler) getUserTeams(c *gin.Context, userID string) ([]string, error) {
	teamIDs, err := h.teamService.GetUserTeamIDs(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get user team IDs", 
			zap.String("user_id", userID),
			zap.Error(err))
		// Return empty slice rather than failing the request - user may not be in any teams
		return []string{}, nil
	}
	
	h.logger.Debug("Retrieved user teams for agent access control",
		zap.String("user_id", userID),
		zap.Int("team_count", len(teamIDs)))
	
	return teamIDs, nil
}

// ListExecutions lists execution history for agents
// @Summary Get execution history
// @Description Retrieve execution history with optional filtering by agent_id
// @Tags Executions
// @Accept json
// @Produce json
// @Param agent_id query string false "Filter by agent ID"
// @Param limit query int false "Number of results to return" default(20)
// @Param offset query int false "Number of results to skip" default(0)
// @Success 200 {object} ExecutionListResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /executions [get]
func (h *AgentHandler) ListExecutions(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Get query parameters
	agentID := c.Query("agent_id")
	limit := 20
	offset := 0
	
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 100 {
			limit = parsedLimit
		}
	}
	
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	// For now, return empty list since we don't have execution history storage yet
	// TODO: Implement actual execution history retrieval from Neo4j or agent-builder
	response := gin.H{
		"executions": []interface{}{},
		"total":      0,
		"limit":      limit,
		"offset":     offset,
	}

	if agentID != "" {
		h.logger.Debug("Listing executions for specific agent",
			zap.String("agent_id", agentID),
			zap.String("user_id", userID.(string)),
			zap.Int("limit", limit),
			zap.Int("offset", offset))
	} else {
		h.logger.Debug("Listing all executions for user",
			zap.String("user_id", userID.(string)),
			zap.Int("limit", limit),
			zap.Int("offset", offset))
	}

	c.JSON(http.StatusOK, response)
}

// GetAgentStats returns statistics for a specific agent
// @Summary Get agent statistics
// @Description Retrieve statistics and metrics for a specific agent
// @Tags Stats
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {object} AgentStatsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /stats/agents/{id} [get]
func (h *AgentHandler) GetAgentStats(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}
	
	// Get auth token for agent-builder proxy
	authToken := extractAuthToken(c)
	if authToken == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}
	
	// For now, return basic stats structure
	// In the future, this should query agent-builder or Neo4j for actual stats
	stats := gin.H{
		"agent_id":              agentID,
		"total_executions":      0,
		"successful_executions": 0,
		"failed_executions":     0,
		"avg_response_time_ms":  0,
		"total_cost_usd":        0.0,
		"last_executed_at":      nil,
		"execution_trend": []interface{}{},  // Array of execution counts per day
		"performance_metrics": gin.H{
			"p50_response_time_ms": 0,
			"p95_response_time_ms": 0,
			"p99_response_time_ms": 0,
		},
	}
	
	h.logger.Info("Agent stats retrieved",
		zap.String("agent_id", agentID),
	)
	
	c.JSON(http.StatusOK, stats)
}