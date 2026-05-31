package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// AIPlaygroundHandler handles AI playground HTTP requests
type AIPlaygroundHandler struct {
	aiPlaygroundService *services.AIPlaygroundService
	userService         *services.UserService
	teamService         *services.TeamService
	logger              *logger.Logger
}

// NewAIPlaygroundHandler creates a new AI playground handler
func NewAIPlaygroundHandler(
	aiPlaygroundService *services.AIPlaygroundService,
	userService *services.UserService,
	teamService *services.TeamService,
	log *logger.Logger,
) *AIPlaygroundHandler {
	return &AIPlaygroundHandler{
		aiPlaygroundService: aiPlaygroundService,
		userService:         userService,
		teamService:         teamService,
		logger:              log.WithService("ai_playground_handler"),
	}
}

// ======================================================================
// LLM Provider & Model Endpoints
// ======================================================================

// ListProviders returns available LLM providers
// @Summary List LLM providers
// @Description Get available LLM providers configured in the system
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {object} []models.ProviderInfo
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/providers [get]
func (h *AIPlaygroundHandler) ListProviders(c *gin.Context) {
	providers, err := h.aiPlaygroundService.GetProviders(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"providers": providers})
}

// ListModels returns available models for a provider
// @Summary List provider models
// @Description Get available models for a specific LLM provider
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param provider path string true "Provider ID"
// @Success 200 {object} []models.ModelInfo
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/providers/{provider}/models [get]
func (h *AIPlaygroundHandler) ListModels(c *gin.Context) {
	provider := c.Param("provider")
	if provider == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Provider is required", nil))
		return
	}

	models, err := h.aiPlaygroundService.GetModels(c.Request.Context(), provider)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"models": models})
}

// ======================================================================
// LLM Completion Endpoints
// ======================================================================

// CreateCompletion creates an LLM completion
// @Summary Create LLM completion
// @Description Send a prompt to an LLM and get a completion response
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body models.LLMCompletionRequest true "Completion request"
// @Success 200 {object} models.LLMCompletionResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/llm/completions [post]
func (h *AIPlaygroundHandler) CreateCompletion(c *gin.Context) {
	var req models.LLMCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	h.logger.Info("Creating LLM completion",
		zap.String("provider", req.Provider),
		zap.String("model", req.Model),
	)

	result, err := h.aiPlaygroundService.CreateCompletion(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// CompareCompletions compares completions across multiple models
// @Summary Compare LLM completions
// @Description Send a prompt to multiple LLMs and compare their responses
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body models.LLMCompareRequest true "Compare request"
// @Success 200 {object} models.LLMCompareResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/llm/completions/compare [post]
func (h *AIPlaygroundHandler) CompareCompletions(c *gin.Context) {
	var req models.LLMCompareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	h.logger.Info("Comparing LLM completions",
		zap.Int("model_count", len(req.Models)),
	)

	result, err := h.aiPlaygroundService.CompareCompletions(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// ======================================================================
// Agent Testing Endpoints
// ======================================================================

// ListAgents returns agents available for testing
// @Summary List testable agents
// @Description Get agents available for testing in the AI playground
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {object} []models.TestableAgent
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/agents [get]
func (h *AIPlaygroundHandler) ListAgents(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	userTeams := h.getUserTeams(c, userID)

	agents, err := h.aiPlaygroundService.GetTestableAgents(
		c.Request.Context(),
		spaceContext.TenantID,
		spaceContext.SpaceID,
		userID,
		userTeams,
	)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"agents": agents})
}

// TestAgent executes a test against an agent
// @Summary Test agent
// @Description Execute a test message against an agent and view execution trace
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Agent ID"
// @Param request body models.AgentTestRequest true "Test request"
// @Success 200 {object} models.AgentTestResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/agents/{id}/test [post]
func (h *AIPlaygroundHandler) TestAgent(c *gin.Context) {
	agentID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	var req models.AgentTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	// Override agent ID from path
	req.AgentID = agentID

	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	h.logger.Info("Testing agent",
		zap.String("agent_id", agentID),
		zap.String("user_id", userID),
	)

	userTeams := h.getUserTeams(c, userID)
	authToken := c.GetHeader("Authorization")

	result, err := h.aiPlaygroundService.TestAgent(
		c.Request.Context(),
		req,
		spaceContext.TenantID,
		spaceContext.SpaceID,
		userID,
		userTeams,
		authToken,
	)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// ======================================================================
// Workflow Testing Endpoints
// ======================================================================

// ListWorkflows returns workflows available for testing
// @Summary List testable workflows
// @Description Get workflows available for testing in the AI playground
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Success 200 {object} []models.TestableWorkflow
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/workflows [get]
func (h *AIPlaygroundHandler) ListWorkflows(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	workflows, err := h.aiPlaygroundService.GetTestableWorkflows(
		c.Request.Context(),
		spaceContext.TenantID,
		spaceContext.SpaceID,
		userID,
	)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"workflows": workflows})
}

// TestWorkflow executes a test against a workflow
// @Summary Test workflow
// @Description Execute a test against a workflow and view execution steps
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Workflow ID"
// @Param request body models.WorkflowTestRequest true "Test request"
// @Success 200 {object} models.WorkflowTestResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/workflows/{id}/test [post]
func (h *AIPlaygroundHandler) TestWorkflow(c *gin.Context) {
	workflowID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	var req models.WorkflowTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body for workflows without parameters
		req = models.WorkflowTestRequest{}
	}

	// Override workflow ID from path
	req.WorkflowID = workflowID

	h.logger.Info("Testing workflow",
		zap.String("workflow_id", workflowID),
		zap.String("user_id", userID),
	)

	result, err := h.aiPlaygroundService.TestWorkflow(
		c.Request.Context(),
		req,
		spaceContext.TenantID,
		spaceContext.SpaceID,
		userID,
	)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// ======================================================================
// Saved Prompt Endpoints
// ======================================================================

// ListSavedPrompts returns saved prompts accessible to the user
// @Summary List saved prompts
// @Description Get saved prompts accessible to the current user
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(20)
// @Param search query string false "Search in name and description"
// @Param folder query string false "Filter by folder"
// @Param visibility query string false "Filter by visibility"
// @Success 200 {object} models.SavedPromptListResponse
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/prompts [get]
func (h *AIPlaygroundHandler) ListSavedPrompts(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	searchReq := models.SavedPromptSearchRequest{
		Search:     c.Query("search"),
		Visibility: models.PromptVisibility(c.Query("visibility")),
		Folder:     c.Query("folder"),
		Page:       page,
		PageSize:   pageSize,
		SortBy:     c.Query("sort_by"),
		SortOrder:  c.Query("sort_order"),
	}

	userTeams := h.getUserTeams(c, userID)

	result, err := h.aiPlaygroundService.ListSavedPrompts(
		c.Request.Context(),
		spaceContext.TenantID,
		spaceContext.SpaceID,
		userID,
		userTeams,
		searchReq,
	)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// CreateSavedPrompt creates a new saved prompt
// @Summary Create saved prompt
// @Description Create a new saved prompt for the AI playground
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body models.SavedPromptCreateRequest true "Prompt data"
// @Success 201 {object} models.SavedPromptResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/prompts [post]
func (h *AIPlaygroundHandler) CreateSavedPrompt(c *gin.Context) {
	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	var req models.SavedPromptCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	if err := validateStruct(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Validation failed", err))
		return
	}

	h.logger.Info("Creating saved prompt",
		zap.String("user_id", userID),
		zap.String("name", req.Name),
	)

	prompt, err := h.aiPlaygroundService.CreateSavedPrompt(
		c.Request.Context(),
		userID,
		spaceContext.TenantID,
		spaceContext.SpaceID,
		req,
	)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, prompt.ToResponse())
}

// GetSavedPrompt gets a saved prompt by ID
// @Summary Get saved prompt
// @Description Get a saved prompt by ID
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Prompt ID"
// @Success 200 {object} models.SavedPromptResponse
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/prompts/{id} [get]
func (h *AIPlaygroundHandler) GetSavedPrompt(c *gin.Context) {
	promptID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	prompt, err := h.aiPlaygroundService.GetSavedPrompt(c.Request.Context(), promptID, spaceContext.TenantID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	// Check access
	userTeams := h.getUserTeams(c, userID)
	if !prompt.IsAccessibleBy(userID, spaceContext.SpaceID, userTeams) {
		c.JSON(http.StatusForbidden, errors.ForbiddenWithDetails("Not authorized to access this prompt", nil))
		return
	}

	c.JSON(http.StatusOK, prompt.ToResponse())
}

// UpdateSavedPrompt updates a saved prompt
// @Summary Update saved prompt
// @Description Update a saved prompt (owner only)
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Prompt ID"
// @Param request body models.SavedPromptUpdateRequest true "Update data"
// @Success 200 {object} models.SavedPromptResponse
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/prompts/{id} [put]
func (h *AIPlaygroundHandler) UpdateSavedPrompt(c *gin.Context) {
	promptID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	var req models.SavedPromptUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request payload", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request payload", err))
		return
	}

	prompt, err := h.aiPlaygroundService.UpdateSavedPrompt(
		c.Request.Context(),
		promptID,
		spaceContext.TenantID,
		userID,
		req,
	)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, prompt.ToResponse())
}

// DeleteSavedPrompt deletes a saved prompt
// @Summary Delete saved prompt
// @Description Delete a saved prompt (owner only)
// @Tags ai-playground
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Prompt ID"
// @Success 204 "No Content"
// @Failure 401 {object} errors.APIError
// @Failure 403 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/developer-tools/ai/prompts/{id} [delete]
func (h *AIPlaygroundHandler) DeleteSavedPrompt(c *gin.Context) {
	promptID := c.Param("id")

	userID, err := ensureUserExists(c, h.userService, h.logger)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space context required", nil))
		return
	}

	if err := h.aiPlaygroundService.DeleteSavedPrompt(
		c.Request.Context(),
		promptID,
		spaceContext.TenantID,
		userID,
	); err != nil {
		handleServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// ======================================================================
// Helper Methods
// ======================================================================

// getUserTeams is a helper to get user's team IDs for access control
func (h *AIPlaygroundHandler) getUserTeams(c *gin.Context, userID string) []string {
	teams, err := h.teamService.GetTeams(c.Request.Context(), userID, "")
	if err != nil {
		h.logger.Warn("Failed to get user teams for access control",
			zap.String("user_id", userID),
			zap.Error(err),
		)
		return []string{}
	}

	teamIDs := make([]string, len(teams))
	for i, team := range teams {
		teamIDs[i] = team.ID
	}
	return teamIDs
}
