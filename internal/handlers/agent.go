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

	"github.com/Tributary-ai-services/aether-be/internal/agents"
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

	authToken := extractAuthToken(c)
	agent, err := h.agentService.GetAgent(c.Request.Context(), agentID, userID, userTeams, authToken)
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
		zap.Strings("tags", req.Tags),
		zap.Strings("skills", req.Skills),
	)

	// Strip empty tags before validation
	if len(req.Tags) > 0 {
		filtered := make([]string, 0, len(req.Tags))
		for _, t := range req.Tags {
			if t != "" {
				filtered = append(filtered, t)
			}
		}
		req.Tags = filtered
	}

	// Validate request
	if err := validateStruct(&req); err != nil {
		h.logger.Warn("Agent update validation failed",
			zap.String("agent_id", agentID),
			zap.Error(err),
		)
		c.JSON(http.StatusUnprocessableEntity, errors.Validation("Validation failed", err))
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

	if includeInternal := c.Query("include_internal"); includeInternal != "" {
		if val, err := strconv.ParseBool(includeInternal); err == nil {
			req.IncludeInternal = val
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

// InternalAgent is an alias for the shared internal agent type
type InternalAgent = agents.InternalAgent

// NotebookChatAssistantID is the ID of the internal notebook chat agent
// Using the constant from the shared agents package
const NotebookChatAssistantID = agents.NotebookChatAssistantID

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
	// Deprecated: Use GET /api/v1/agents?include_internal=true instead
	c.Header("Deprecation", "true")
	c.Header("Sunset", "2026-06-01")
	c.Header("Link", `</api/v1/agents?include_internal=true>; rel="successor-version"`)
	h.logger.Warn("Deprecated endpoint called: GET /api/v1/agents/internal — use GET /api/v1/agents?include_internal=true instead")

	authToken := extractAuthToken(c)
	if authToken == "" {
		h.logger.Warn("No authorization token provided for listing internal agents")
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}

	agents, err := h.agentService.GetInternalAgents(c.Request.Context(), authToken)
	if err != nil {
		h.logger.Error("Failed to fetch internal agents from agent-builder", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to fetch internal agents"))
		return
	}

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
	// Deprecated: Use GET /api/v1/agents?include_internal=true instead
	c.Header("Deprecation", "true")
	c.Header("Sunset", "2026-06-01")
	h.logger.Warn("Deprecated endpoint called: GET /api/v1/agents/internal/:id — use GET /api/v1/agents?include_internal=true instead")

	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Agent ID is required"))
		return
	}

	authToken := extractAuthToken(c)
	if authToken == "" {
		h.logger.Warn("No authorization token provided for getting internal agent")
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}

	agent, err := h.agentService.GetInternalAgentByID(c.Request.Context(), agentID, authToken)
	if err != nil {
		h.logger.Error("Failed to fetch internal agent from agent-builder",
			zap.Error(err),
			zap.String("agent_id", agentID),
		)
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to fetch internal agent"))
		return
	}

	if agent == nil {
		c.JSON(http.StatusNotFound, errors.NotFound("Internal agent not found"))
		return
	}

	c.JSON(http.StatusOK, agent)
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

	// Get auth token for agent-builder
	authToken := extractAuthToken(c)
	if authToken == "" {
		h.logger.Warn("No authorization token provided")
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("Authorization token required"))
		return
	}

	// Parse the request body
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

	// Build service request with conversation history support
	serviceReq := services.InternalAgentExecuteRequest{
		Input:     req.Input,
		SessionID: req.SessionID,
		Context:   req.Context,
	}

	// Convert conversation history
	for _, msg := range req.History {
		serviceReq.History = append(serviceReq.History, services.ConversationMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		})
	}

	// Execute via agent-builder service
	response, err := h.agentService.ExecuteInternalAgent(c.Request.Context(), agentID, serviceReq, authToken)
	if err != nil {
		h.logger.Error("Failed to execute internal agent via agent-builder",
			zap.Error(err),
			zap.String("agent_id", agentID),
		)
		// Check if it's a not found error
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, errors.NotFound("Internal agent not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to execute agent"))
		return
	}

	h.logger.Info("Internal agent executed successfully via agent-builder",
		zap.String("agent_id", agentID),
		zap.String("execution_id", response.ExecutionID),
	)

	// Return response in the format expected by the frontend
	c.JSON(http.StatusOK, InternalAgentExecuteResponse{
		Output:         response.Output,
		ConversationID: response.ConversationID,
		Metadata:       response.Metadata,
	})
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
		"execution_trend":       []interface{}{}, // Array of execution counts per day
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

// ListSkills proxies the skills list request to agent-builder
func (h *AgentHandler) ListSkills(c *gin.Context) {
	authToken := extractAuthToken(c)
	body, err := h.agentService.ProxySkillsList(c.Request.Context(), c.Request.URL.RawQuery, authToken)
	if err != nil {
		h.logger.Error("Failed to proxy skills list", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch skills from agent-builder"})
		return
	}
	c.Data(http.StatusOK, "application/json", body)
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
