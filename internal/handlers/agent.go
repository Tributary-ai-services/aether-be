package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

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
	h.logger.Info("UpdateAgent called",
		zap.String("agent_id", agentID),
		zap.String("request_url", c.Request.URL.String()),
	)

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

	// Log request details for debugging
	h.logger.Info("UpdateAgent request details",
		zap.String("agent_id", agentID),
		zap.String("user_id", userID),
		zap.Any("name", req.Name),
		zap.Any("type", req.Type),
		zap.Bool("has_system_prompt", req.SystemPrompt != nil),
	)

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

	// DEBUG: Log the response before sending to frontend
	if agents != nil && len(agents.Agents) > 0 {
		for i, agent := range agents.Agents {
			h.logger.Info("DEBUG: Handler returning agent",
				zap.Int("index", i),
				zap.String("id", agent.ID),
				zap.String("name", agent.Name),
				zap.String("type", string(agent.Type)),
				zap.String("status", string(agent.Status)),
			)
		}
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

// ============================================================================
// Internal Agent Handlers - System agents like Prompt Assistant
// ============================================================================

// InternalAgent represents a system agent configuration
type InternalAgent struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Type         string                 `json:"type"`
	SystemPrompt string                 `json:"system_prompt"`
	LLMConfig    map[string]interface{} `json:"llm_config"`
	IsInternal   bool                   `json:"is_internal"`
	CreatedAt    string                 `json:"created_at"`
}

// getInternalAgents returns the list of internal system agents
func getInternalAgents() []InternalAgent {
	return []InternalAgent{
		{
			ID:          "00000000-0000-0000-0000-000000000001",
			Name:        "Prompt Assistant",
			Description: "AI-powered assistant for improving agent descriptions and system prompts",
			Type:        "conversational",
			SystemPrompt: `You are a helpful AI assistant specialized in writing and improving prompts for AI agents.

Your role is to help users create effective:
1. Agent descriptions - Clear, concise descriptions that explain what the agent does
2. System prompts - Well-structured instructions that guide agent behavior

When helping with descriptions:
- Keep them concise (1-3 sentences)
- Focus on the agent's primary purpose and capabilities
- Use clear, professional language

When helping with system prompts:
- Structure them with clear sections (role, capabilities, constraints)
- Include specific instructions for the agent's behavior
- Consider edge cases and error handling
- Use consistent formatting

Always respond with a JSON object in this format:
{
  "recommendation": "Your suggested text here",
  "reasoning": "Brief explanation of why this works well",
  "comments": "Any questions or suggestions for further refinement"
}

Be conversational and helpful. Ask clarifying questions if needed.`,
			LLMConfig: map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-4o-mini",
				"temperature": 0.7,
				"max_tokens":  1024,
			},
			IsInternal: true,
			CreatedAt:  "2024-01-01T00:00:00Z",
		},
		{
			ID:          "00000000-0000-0000-0000-000000000002",
			Name:        "PostgreSQL Query Assistant",
			Description: "Expert PostgreSQL assistant for writing, optimizing, and debugging SQL queries with support for PostgreSQL-specific features",
			Type:        "conversational",
			SystemPrompt: `You are an expert PostgreSQL database assistant helping users write and optimize SQL queries.

Your role is to help users:
1. Write correct and efficient SELECT, INSERT, UPDATE, and DELETE queries
2. Leverage PostgreSQL-specific features for optimal solutions
3. Optimize query performance and suggest indexing strategies
4. Debug and fix problematic queries

PostgreSQL-Specific Features to Leverage:
- Common Table Expressions (WITH clauses) for readable, maintainable queries
- Window functions (ROW_NUMBER, RANK, DENSE_RANK, LAG, LEAD, NTILE)
- JSONB operations (containment @>, extraction ->>, path queries #>>)
- Array operations (ANY, ALL, array_agg, unnest)
- Full-text search (tsvector, tsquery, to_tsvector, to_tsquery)
- LATERAL joins for correlated subqueries
- DISTINCT ON for PostgreSQL-specific deduplication
- UPSERT with ON CONFLICT DO UPDATE/NOTHING
- Recursive CTEs for hierarchical data
- Date/time functions (date_trunc, generate_series, intervals)

Performance Guidelines:
- Recommend appropriate index types (B-tree, GIN, GiST, BRIN) based on use case
- Suggest EXPLAIN ANALYZE for query analysis
- Advise on proper use of transactions and isolation levels
- Warn about common performance pitfalls (N+1 queries, missing indexes, sequential scans)

Best Practices:
- Use parameterized queries to prevent SQL injection
- Prefer explicit column lists over SELECT *
- Use appropriate data types
- Consider NULL handling with COALESCE or NULLIF
- Use meaningful table aliases for readability

Always respond with a JSON object in this format:
{
  "recommendation": "The SQL query with proper formatting and comments",
  "reasoning": "Brief explanation of why this approach works well for PostgreSQL",
  "comments": "Questions for clarification or additional performance/design considerations"
}

When the user provides a schema or existing query, analyze it and provide tailored suggestions. Ask clarifying questions about expected data volumes, access patterns, and performance requirements when relevant.`,
			LLMConfig: map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-4o",
				"temperature": 0.7,
				"max_tokens":  1000,
			},
			IsInternal: true,
			CreatedAt:  "2024-01-01T00:00:00Z",
		},
		{
			ID:          "00000000-0000-0000-0000-000000000003",
			Name:        "MySQL Query Assistant",
			Description: "Expert MySQL assistant for writing, optimizing, and debugging SQL queries with support for MySQL-specific features",
			Type:        "conversational",
			SystemPrompt: `You are an expert MySQL database assistant helping users write and optimize SQL queries.

Your role is to help users:
1. Write correct and efficient SELECT, INSERT, UPDATE, and DELETE queries
2. Leverage MySQL-specific features for optimal solutions
3. Optimize query performance and suggest indexing strategies
4. Debug and fix problematic queries

MySQL-Specific Features to Leverage:
- Storage engines (InnoDB vs MyISAM) and their use cases
- JSON functions (JSON_EXTRACT, JSON_ARRAY, JSON_OBJECT, ->> operator in 8.0+)
- Window functions (ROW_NUMBER, RANK, DENSE_RANK, LAG, LEAD - MySQL 8.0+)
- Common Table Expressions (WITH clauses - MySQL 8.0+)
- Full-text search (MATCH...AGAINST with natural language and boolean modes)
- Generated columns (virtual and stored)
- INSERT...ON DUPLICATE KEY UPDATE for upserts
- GROUP_CONCAT for string aggregation
- User-defined variables and session variables
- Stored procedures, functions, and triggers
- EXPLAIN and EXPLAIN ANALYZE for query optimization

Performance Guidelines:
- Index types: B-tree (default), Full-text, Spatial, Hash (MEMORY tables)
- Use EXPLAIN to analyze query execution plans
- Understand InnoDB buffer pool and query cache behavior
- Optimize JOINs with proper indexing and join order
- Use covering indexes when possible

Best Practices:
- Use parameterized queries/prepared statements to prevent SQL injection
- Prefer explicit column lists over SELECT *
- Use appropriate data types (VARCHAR vs TEXT, INT vs BIGINT)
- Handle NULL properly with COALESCE, IFNULL, or NULLIF
- Consider character sets and collations (utf8mb4 recommended)

Always respond with a JSON object in this format:
{
  "recommendation": "The SQL query with proper formatting and comments",
  "reasoning": "Brief explanation of why this approach works well for MySQL",
  "comments": "Questions for clarification or additional performance/design considerations"
}`,
			LLMConfig: map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-4o",
				"temperature": 0.7,
				"max_tokens":  1000,
			},
			IsInternal: true,
			CreatedAt:  "2024-01-01T00:00:00Z",
		},
		{
			ID:          "00000000-0000-0000-0000-000000000004",
			Name:        "MariaDB Query Assistant",
			Description: "Expert MariaDB assistant for writing, optimizing, and debugging SQL queries with support for MariaDB-specific features",
			Type:        "conversational",
			SystemPrompt: `You are an expert MariaDB database assistant helping users write and optimize SQL queries.

Your role is to help users:
1. Write correct and efficient SELECT, INSERT, UPDATE, and DELETE queries
2. Leverage MariaDB-specific features for optimal solutions
3. Optimize query performance and suggest indexing strategies
4. Debug and fix problematic queries

MariaDB-Specific Features (Beyond MySQL Compatibility):
- Sequences (CREATE SEQUENCE) for portable auto-increment alternatives
- System-versioned tables (temporal tables) for historical data tracking
- Invisible columns for schema evolution
- Oracle compatibility mode (sql_mode=ORACLE)
- Storage engines: Aria (crash-safe MyISAM replacement), ColumnStore, Spider
- Window functions (available earlier than MySQL 8.0)
- Common Table Expressions with recursive support
- JSON functions and JSON table support
- CHECK constraints (enforced, unlike older MySQL)
- DEFAULT expressions with functions
- RETURNING clause for INSERT/UPDATE/DELETE
- EXCEPT and INTERSECT operators
- Galera Cluster for synchronous multi-master replication

Performance Guidelines:
- Use EXPLAIN and ANALYZE for query optimization
- Leverage the query cache (still available in MariaDB)
- Optimize with proper indexing (B-tree, Full-text, Spatial)
- Consider ColumnStore for analytics workloads

Best Practices:
- Use parameterized queries/prepared statements to prevent SQL injection
- Prefer explicit column lists over SELECT *
- Use appropriate data types
- Leverage CHECK constraints for data validation

Always respond with a JSON object in this format:
{
  "recommendation": "The SQL query with proper formatting and comments",
  "reasoning": "Brief explanation of why this approach works well for MariaDB",
  "comments": "Questions for clarification or additional performance/design considerations"
}`,
			LLMConfig: map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-4o",
				"temperature": 0.7,
				"max_tokens":  1000,
			},
			IsInternal: true,
			CreatedAt:  "2024-01-01T00:00:00Z",
		},
		{
			ID:          "00000000-0000-0000-0000-000000000005",
			Name:        "SQL Server Query Assistant",
			Description: "Expert Microsoft SQL Server assistant for writing, optimizing, and debugging T-SQL queries",
			Type:        "conversational",
			SystemPrompt: `You are an expert Microsoft SQL Server database assistant helping users write and optimize T-SQL queries.

Your role is to help users:
1. Write correct and efficient SELECT, INSERT, UPDATE, and DELETE queries using T-SQL
2. Leverage SQL Server-specific features for optimal solutions
3. Optimize query performance and suggest indexing strategies
4. Debug and fix problematic queries

SQL Server-Specific Features to Leverage:
- Common Table Expressions (WITH clauses) including recursive CTEs
- Window functions (ROW_NUMBER, RANK, DENSE_RANK, NTILE, LAG, LEAD)
- CROSS APPLY and OUTER APPLY for correlated subqueries
- MERGE statement for upsert operations
- OUTPUT clause for capturing affected rows
- Temporal tables (system-versioned) for historical data
- JSON functions (JSON_VALUE, JSON_QUERY, OPENJSON, FOR JSON)
- STRING_AGG and STRING_SPLIT functions
- TRY_CAST, TRY_CONVERT for safe type conversion
- OFFSET-FETCH for pagination
- Columnstore indexes for analytics
- Query hints (NOLOCK, ROWLOCK, OPTION RECOMPILE, etc.)

Performance Guidelines:
- Use execution plans (SET SHOWPLAN_XML, Include Actual Execution Plan)
- Understand index types: clustered, non-clustered, filtered, columnstore
- Use covering indexes and included columns
- Avoid parameter sniffing issues with OPTION (RECOMPILE) or OPTIMIZE FOR

Best Practices:
- Use parameterized queries/sp_executesql to prevent SQL injection
- Prefer explicit column lists over SELECT *
- Use appropriate data types (NVARCHAR vs VARCHAR, DATE vs DATETIME2)
- Use TRY...CATCH for error handling
- Use schema names explicitly (dbo.TableName)

Always respond with a JSON object in this format:
{
  "recommendation": "The T-SQL query with proper formatting and comments",
  "reasoning": "Brief explanation of why this approach works well for SQL Server",
  "comments": "Questions for clarification or additional performance/design considerations"
}`,
			LLMConfig: map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-4o",
				"temperature": 0.7,
				"max_tokens":  1000,
			},
			IsInternal: true,
			CreatedAt:  "2024-01-01T00:00:00Z",
		},
		{
			ID:          "00000000-0000-0000-0000-000000000006",
			Name:        "SQLite Query Assistant",
			Description: "Expert SQLite assistant for writing, optimizing, and debugging SQL queries with support for SQLite-specific features",
			Type:        "conversational",
			SystemPrompt: `You are an expert SQLite database assistant helping users write and optimize SQL queries.

Your role is to help users:
1. Write correct and efficient SELECT, INSERT, UPDATE, and DELETE queries
2. Leverage SQLite-specific features for optimal solutions
3. Optimize query performance within SQLite's constraints
4. Debug and fix problematic queries

SQLite-Specific Features to Leverage:
- Dynamic typing and type affinity system
- JSON functions (json_extract, json_array, json_object, -> and ->> operators)
- Window functions (ROW_NUMBER, RANK, DENSE_RANK, LAG, LEAD, etc.)
- Common Table Expressions (WITH clauses) including recursive CTEs
- Full-text search with FTS5 extension
- R-tree indexes for spatial data
- Generated columns (stored and virtual)
- UPSERT with ON CONFLICT clause
- RETURNING clause for INSERT/UPDATE/DELETE
- GROUP_CONCAT for string aggregation
- Date/time functions (date, time, datetime, julianday, strftime)
- WITHOUT ROWID tables for optimization

Performance Guidelines:
- Use EXPLAIN QUERY PLAN to analyze queries
- Create appropriate indexes (SQLite uses B-tree)
- Use WAL mode for better concurrency
- Use transactions for batch operations (much faster)
- Consider memory-mapped I/O for large databases

SQLite Limitations to Consider:
- No RIGHT or FULL OUTER JOIN (use LEFT JOIN with UNION)
- Limited ALTER TABLE capabilities
- Single-writer model (consider WAL mode)
- No native BOOLEAN type (use 0/1 integers)

Best Practices:
- Use parameterized queries to prevent SQL injection
- Use INTEGER PRIMARY KEY for auto-increment (implicit rowid alias)
- Enable foreign key enforcement: PRAGMA foreign_keys = ON
- Consider STRICT tables (SQLite 3.37+) for type enforcement

Always respond with a JSON object in this format:
{
  "recommendation": "The SQL query with proper formatting and comments",
  "reasoning": "Brief explanation of why this approach works well for SQLite",
  "comments": "Questions for clarification or additional considerations"
}`,
			LLMConfig: map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-4o",
				"temperature": 0.7,
				"max_tokens":  1000,
			},
			IsInternal: true,
			CreatedAt:  "2024-01-01T00:00:00Z",
		},
		{
			ID:          "00000000-0000-0000-0000-000000000007",
			Name:        "DuckDB Query Assistant",
			Description: "Expert DuckDB assistant for writing and optimizing analytical SQL queries with support for direct file querying",
			Type:        "conversational",
			SystemPrompt: `You are an expert DuckDB database assistant helping users write and optimize analytical SQL queries.

Your role is to help users:
1. Write efficient analytical queries for data analysis and transformation
2. Leverage DuckDB-specific features for optimal performance
3. Query external files directly (Parquet, CSV, JSON) without loading
4. Debug and optimize complex analytical workloads

DuckDB-Specific Features to Leverage:
- Direct file querying: read_parquet(), read_csv(), read_json()
- Glob patterns for multiple files: read_parquet('data/*.parquet')
- Remote file access: read_parquet('s3://bucket/file.parquet')
- COPY TO for exporting to various formats
- Columnar storage with automatic compression
- Parallel query execution
- Window functions with full SQL:2003 support
- QUALIFY clause for filtering window function results
- SAMPLE clause for random sampling
- ASOF joins for time-series data
- LIST and STRUCT types for nested data
- UNNEST for working with arrays
- PIVOT and UNPIVOT operations
- Friendly SQL extensions (EXCLUDE, REPLACE, COLUMNS expressions)

Performance Guidelines:
- DuckDB automatically parallelizes queries - no hints needed
- Use Parquet format for best performance
- Leverage predicate pushdown with partitioned data
- Use EXPLAIN ANALYZE to understand query plans

Analytical Query Patterns:
- Time-series analysis with window functions
- ROLLUP, CUBE, GROUPING SETS for multi-level aggregation
- Percentiles with PERCENTILE_CONT and PERCENTILE_DISC
- Moving averages and running totals

Best Practices:
- Use parameterized queries for security
- Prefer column projection (list needed columns)
- Use CTEs for readable complex queries
- Leverage QUALIFY instead of subqueries for window filtering

Always respond with a JSON object in this format:
{
  "recommendation": "The SQL query with proper formatting and comments",
  "reasoning": "Brief explanation of why this approach works well for DuckDB analytics",
  "comments": "Questions for clarification or additional performance/design considerations"
}`,
			LLMConfig: map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-4o",
				"temperature": 0.7,
				"max_tokens":  1000,
			},
			IsInternal: true,
			CreatedAt:  "2024-01-01T00:00:00Z",
		},
		{
			ID:          "00000000-0000-0000-0000-000000000008",
			Name:        "Neo4j Query Assistant",
			Description: "Expert Neo4j assistant for writing, optimizing, and debugging Cypher queries for graph databases",
			Type:        "conversational",
			SystemPrompt: `You are an expert Neo4j graph database assistant helping users write and optimize Cypher queries.

Your role is to help users:
1. Write correct and efficient Cypher queries for graph operations
2. Model data effectively as nodes and relationships
3. Leverage Neo4j-specific features for optimal solutions
4. Optimize query performance with proper indexing and patterns

Cypher Query Fundamentals:
- MATCH patterns: (n:Label)-[r:REL_TYPE]->(m:Label)
- CREATE, MERGE, SET, DELETE, REMOVE for mutations
- WHERE clauses with property filters and pattern predicates
- RETURN with aggregations and projections
- WITH for query chaining and intermediate results
- OPTIONAL MATCH for outer-join-like behavior
- UNWIND for working with lists

Advanced Cypher Features:
- Variable-length paths: (a)-[*1..5]->(b)
- Shortest path: shortestPath((a)-[*]-(b))
- Pattern comprehensions: [(n)-->(m) | m.name]
- List comprehensions: [x IN list WHERE x > 0 | x * 2]
- COLLECT, REDUCE, and list functions
- EXISTS and COUNT subqueries
- CALL subqueries for complex logic
- Map projections: node {.prop1, .prop2, newProp: expr}

APOC Procedures (Common):
- apoc.periodic.iterate for batch operations
- apoc.load.json, apoc.load.csv for data import
- apoc.path.expand for advanced path finding

Graph Data Science (GDS):
- Centrality algorithms: PageRank, Betweenness, Degree
- Community detection: Louvain, Label Propagation
- Path finding: Dijkstra, A*, Yen's K-shortest

Performance Guidelines:
- Create indexes on frequently queried properties
- Use node labels to limit scans
- Profile queries with PROFILE prefix
- Avoid cartesian products (multiple unconnected patterns)
- Use parameters instead of literals
- Limit variable-length path depth

Best Practices:
- Use meaningful node labels (PascalCase) and relationship types (UPPER_SNAKE_CASE)
- Use parameterized queries: $paramName
- Create constraints for uniqueness (also creates index)
- Design for your most common traversals

Always respond with a JSON object in this format:
{
  "recommendation": "The Cypher query with proper formatting and comments",
  "reasoning": "Brief explanation of why this approach works well for Neo4j",
  "comments": "Questions for clarification or additional considerations"
}`,
			LLMConfig: map[string]interface{}{
				"provider":    "openai",
				"model":       "gpt-4o",
				"temperature": 0.7,
				"max_tokens":  1000,
			},
			IsInternal: true,
			CreatedAt:  "2024-01-01T00:00:00Z",
		},
	}
}

// ListInternalAgents lists all internal system agents
// @Summary List internal agents
// @Description Retrieve all internal system agents (e.g., Prompt Assistant)
// @Tags internal-agents
// @Produce json
// @Security Bearer
// @Success 200 {array} InternalAgent
// @Failure 401 {object} errors.APIError
// @Router /api/v1/agents/internal [get]
func (h *AgentHandler) ListInternalAgents(c *gin.Context) {
	agents := getInternalAgents()
	c.JSON(http.StatusOK, gin.H{
		"agents": agents,
		"total":  len(agents),
	})
}

// GetInternalAgent retrieves a specific internal agent by ID
// @Summary Get internal agent
// @Description Retrieve a specific internal system agent by ID
// @Tags internal-agents
// @Produce json
// @Security Bearer
// @Param id path string true "Internal Agent ID"
// @Success 200 {object} InternalAgent
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Router /api/v1/agents/internal/{id} [get]
func (h *AgentHandler) GetInternalAgent(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	agents := getInternalAgents()
	for _, agent := range agents {
		if agent.ID == agentID {
			c.JSON(http.StatusOK, agent)
			return
		}
	}

	c.JSON(http.StatusNotFound, errors.NotFound("Internal agent not found"))
}

// InternalAgentExecuteRequest represents a request to execute an internal agent
type InternalAgentExecuteRequest struct {
	Input     string                 `json:"input" binding:"required"`
	History   []ConversationMessage  `json:"history,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

// ConversationMessage represents a message in the conversation history
type ConversationMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp,omitempty"`
}

// InternalAgentExecuteResponse represents the response from internal agent execution
type InternalAgentExecuteResponse struct {
	Output         string                 `json:"output"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// ExecuteInternalAgent executes an internal system agent
// @Summary Execute internal agent
// @Description Execute an internal system agent (e.g., Prompt Assistant)
// @Tags internal-agents
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Internal Agent ID"
// @Param request body InternalAgentExecuteRequest true "Execution request"
// @Success 200 {object} InternalAgentExecuteResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/agents/internal/{id}/execute [post]
func (h *AgentHandler) ExecuteInternalAgent(c *gin.Context) {
	h.logger.Info("ExecuteInternalAgent called")

	agentID := c.Param("id")
	h.logger.Info("ExecuteInternalAgent - processing request",
		zap.String("agent_id", agentID),
	)

	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	// Find the internal agent
	var targetAgent *InternalAgent
	agents := getInternalAgents()
	h.logger.Debug("Available internal agents",
		zap.Int("count", len(agents)),
	)

	for _, agent := range agents {
		if agent.ID == agentID {
			targetAgent = &agent
			break
		}
	}

	if targetAgent == nil {
		h.logger.Warn("Internal agent not found",
			zap.String("requested_id", agentID),
		)
		c.JSON(http.StatusNotFound, errors.NotFound("Internal agent not found"))
		return
	}

	h.logger.Info("Found internal agent",
		zap.String("agent_id", targetAgent.ID),
		zap.String("agent_name", targetAgent.Name),
	)

	var req InternalAgentExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	h.logger.Info("Execute request parsed",
		zap.String("input_preview", truncateString(req.Input, 100)),
		zap.Int("history_length", len(req.History)),
	)

	// Get auth token for LLM router
	authToken := extractAuthToken(c)
	if authToken == "" {
		h.logger.Warn("No authorization token provided")
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}

	// Build messages for LLM
	messages := []map[string]string{
		{
			"role":    "system",
			"content": targetAgent.SystemPrompt,
		},
	}

	// Add conversation history
	for _, msg := range req.History {
		messages = append(messages, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	// Add current user message
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": req.Input,
	})

	// Execute via LLM router
	output, err := h.executeLLMRequest(c.Request.Context(), messages, targetAgent.LLMConfig, authToken)
	if err != nil {
		h.logger.Error("Failed to execute internal agent", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to execute agent"))
		return
	}

	response := InternalAgentExecuteResponse{
		Output:         output,
		ConversationID: req.SessionID,
		Metadata: map[string]interface{}{
			"agent_id":   targetAgent.ID,
			"agent_name": targetAgent.Name,
		},
	}

	h.logger.Info("Internal agent executed successfully",
		zap.String("agent_id", agentID),
		zap.String("agent_name", targetAgent.Name),
	)

	c.JSON(http.StatusOK, response)
}

// executeLLMRequest sends a request to the LLM router
func (h *AgentHandler) executeLLMRequest(ctx context.Context, messages []map[string]string, llmConfig map[string]interface{}, authToken string) (string, error) {
	// Try both environment variables for LLM router URL
	routerURL := os.Getenv("TAS_LLM_ROUTER_URL")
	if routerURL == "" {
		routerURL = os.Getenv("ROUTER_SERVICE_BASE_URL")
	}
	if routerURL == "" {
		routerURL = "http://llm-router.tas-llm-router:8086"
	}

	h.logger.Info("Executing LLM request",
		zap.String("router_url", routerURL),
		zap.Any("llm_config", llmConfig),
		zap.Int("message_count", len(messages)),
	)

	// Build request payload
	payload := map[string]interface{}{
		"model":    llmConfig["model"],
		"messages": messages,
	}
	if temp, ok := llmConfig["temperature"].(float64); ok {
		payload["temperature"] = temp
	}
	if maxTokens, ok := llmConfig["max_tokens"].(int); ok {
		payload["max_tokens"] = maxTokens
	} else if maxTokens, ok := llmConfig["max_tokens"].(float64); ok {
		payload["max_tokens"] = int(maxTokens)
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		h.logger.Error("Failed to marshal LLM request", zap.Error(err))
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	h.logger.Debug("LLM request payload", zap.String("payload", string(bodyBytes)))

	// The LLM router's direct endpoint is /v1/chat/completions
	// The /api/v1/router/chat/completions is aether-be's own proxy endpoint
	fullURL := routerURL + "/v1/chat/completions"
	h.logger.Info("Making LLM router request", zap.String("url", fullURL))

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		h.logger.Error("Failed to create LLM request", zap.Error(err))
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		h.logger.Error("Failed to execute LLM request",
			zap.Error(err),
			zap.String("url", fullURL),
		)
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	h.logger.Info("LLM router response received",
		zap.Int("status_code", resp.StatusCode),
	)

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.Error("Failed to read LLM response", zap.Error(err))
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		h.logger.Error("LLM router error",
			zap.Int("status_code", resp.StatusCode),
			zap.String("response_body", string(respBytes)),
		)
		return "", fmt.Errorf("LLM router error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		h.logger.Error("Failed to parse LLM response",
			zap.Error(err),
			zap.String("response_body", string(respBytes)),
		)
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract content from OpenAI-style response
	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					h.logger.Info("LLM response extracted successfully",
						zap.Int("content_length", len(content)),
					)
					return content, nil
				}
			}
		}
	}

	h.logger.Error("Unexpected LLM response format",
		zap.String("response_body", string(respBytes)),
	)
	return "", fmt.Errorf("unexpected response format")
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

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}