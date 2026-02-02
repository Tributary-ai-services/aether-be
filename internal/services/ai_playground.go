package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// AIPlaygroundService handles AI playground operations including:
// - LLM completions and comparisons via tas-llm-router
// - Agent testing via agent-builder
// - Workflow testing via workflow-builder
// - Saved prompt management via Neo4j
type AIPlaygroundService struct {
	neo4j           *database.Neo4jClient
	routerConfig    *config.RouterConfig
	agentService    *AgentService
	workflowService *WorkflowService
	httpClient      *http.Client
	logger          *logger.Logger
}

// NewAIPlaygroundService creates a new AI playground service
func NewAIPlaygroundService(
	neo4j *database.Neo4jClient,
	routerConfig *config.RouterConfig,
	agentService *AgentService,
	workflowService *WorkflowService,
	log *logger.Logger,
) *AIPlaygroundService {
	return &AIPlaygroundService{
		neo4j:           neo4j,
		routerConfig:    routerConfig,
		agentService:    agentService,
		workflowService: workflowService,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Longer timeout for LLM calls
		},
		logger: log.WithService("ai_playground_service"),
	}
}

// ======================================================================
// LLM Provider & Model Methods
// ======================================================================

// GetProviders returns available LLM providers
func (s *AIPlaygroundService) GetProviders(ctx context.Context) ([]models.ProviderInfo, error) {
	if s.routerConfig == nil || !s.routerConfig.Enabled {
		return nil, errors.BadRequest("LLM router is not configured")
	}

	url := strings.TrimRight(s.routerConfig.Service.BaseURL, "/") + s.routerConfig.Endpoints.Providers

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Internal("Failed to create request: " + err.Error())
	}

	if s.routerConfig.Service.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.routerConfig.Service.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Error("Failed to fetch providers from LLM router", zap.Error(err))
		return nil, errors.Internal("Failed to fetch providers: " + err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.BadRequest(fmt.Sprintf("LLM router error (%d): %s", resp.StatusCode, string(body)))
	}

	var result struct {
		Providers []models.ProviderInfo `json:"providers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, errors.Internal("Failed to decode providers: " + err.Error())
	}

	return result.Providers, nil
}

// GetModels returns available models for a provider
func (s *AIPlaygroundService) GetModels(ctx context.Context, provider string) ([]models.ModelInfo, error) {
	if s.routerConfig == nil || !s.routerConfig.Enabled {
		return nil, errors.BadRequest("LLM router is not configured")
	}

	endpoint := strings.Replace(s.routerConfig.Endpoints.ProviderDetail, ":provider", provider, 1)
	url := strings.TrimRight(s.routerConfig.Service.BaseURL, "/") + endpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Internal("Failed to create request: " + err.Error())
	}

	if s.routerConfig.Service.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.routerConfig.Service.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Error("Failed to fetch models from LLM router", zap.Error(err), zap.String("provider", provider))
		return nil, errors.Internal("Failed to fetch models: " + err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.BadRequest(fmt.Sprintf("LLM router error (%d): %s", resp.StatusCode, string(body)))
	}

	var result struct {
		Models []models.ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, errors.Internal("Failed to decode models: " + err.Error())
	}

	return result.Models, nil
}

// ======================================================================
// LLM Completion Methods
// ======================================================================

// CreateCompletion sends a completion request to the LLM router
func (s *AIPlaygroundService) CreateCompletion(ctx context.Context, req models.LLMCompletionRequest) (*models.LLMCompletionResponse, error) {
	if s.routerConfig == nil || !s.routerConfig.Enabled {
		return nil, errors.BadRequest("LLM router is not configured")
	}

	startTime := time.Now()

	// Build request body for LLM router
	routerReq := map[string]any{
		"model":    req.Model,
		"provider": req.Provider,
		"messages": []map[string]string{},
		"stream":   false, // Non-streaming for this endpoint
	}

	messages := []map[string]string{}
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": req.UserPrompt,
	})
	routerReq["messages"] = messages

	// Add any additional parameters
	for k, v := range req.Parameters {
		routerReq[k] = v
	}

	jsonBody, err := json.Marshal(routerReq)
	if err != nil {
		return nil, errors.Internal("Failed to marshal request: " + err.Error())
	}

	url := strings.TrimRight(s.routerConfig.Service.BaseURL, "/") + s.routerConfig.Endpoints.ChatCompletions
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, errors.Internal("Failed to create request: " + err.Error())
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if s.routerConfig.Service.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.routerConfig.Service.APIKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		s.logger.Error("LLM completion request failed", zap.Error(err))
		return nil, errors.Internal("LLM request failed: " + err.Error())
	}
	defer resp.Body.Close()

	latency := time.Since(startTime).Milliseconds()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return &models.LLMCompletionResponse{
			Provider:  req.Provider,
			Model:     req.Model,
			Error:     fmt.Sprintf("LLM router error (%d): %s", resp.StatusCode, string(body)),
			CreatedAt: time.Now(),
			Metrics:   &models.LLMMetrics{LatencyMs: latency},
		}, nil
	}

	// Parse the response
	var routerResp struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &routerResp); err != nil {
		return nil, errors.Internal("Failed to decode response: " + err.Error())
	}

	content := ""
	finishReason := ""
	if len(routerResp.Choices) > 0 {
		content = routerResp.Choices[0].Message.Content
		finishReason = routerResp.Choices[0].FinishReason
	}

	return &models.LLMCompletionResponse{
		ID:           routerResp.ID,
		Provider:     req.Provider,
		Model:        req.Model,
		Content:      content,
		FinishReason: finishReason,
		Usage: &models.LLMUsage{
			PromptTokens:     routerResp.Usage.PromptTokens,
			CompletionTokens: routerResp.Usage.CompletionTokens,
			TotalTokens:      routerResp.Usage.TotalTokens,
		},
		Metrics: &models.LLMMetrics{
			LatencyMs: latency,
			// TODO: Calculate estimated cost based on model pricing
		},
		CreatedAt: time.Now(),
	}, nil
}

// CompareCompletions runs the same prompt across multiple models for comparison
func (s *AIPlaygroundService) CompareCompletions(ctx context.Context, req models.LLMCompareRequest) (*models.LLMCompareResponse, error) {
	results := make(map[string]*models.LLMCompletionResponse)

	// Run completions for each model (could be parallelized in the future)
	for _, modelCfg := range req.Models {
		completionReq := models.LLMCompletionRequest{
			Provider:     modelCfg.Provider,
			Model:        modelCfg.Model,
			SystemPrompt: req.SystemPrompt,
			UserPrompt:   req.UserPrompt,
			Parameters:   modelCfg.Parameters,
			Stream:       false,
		}

		result, err := s.CreateCompletion(ctx, completionReq)
		if err != nil {
			// Store error but continue with other models
			results[modelCfg.Provider+":"+modelCfg.Model] = &models.LLMCompletionResponse{
				Provider:  modelCfg.Provider,
				Model:     modelCfg.Model,
				Error:     err.Error(),
				CreatedAt: time.Now(),
			}
		} else {
			results[modelCfg.Provider+":"+modelCfg.Model] = result
		}
	}

	return &models.LLMCompareResponse{
		Results:   results,
		CreatedAt: time.Now(),
	}, nil
}

// ======================================================================
// Agent Testing Methods
// ======================================================================

// GetTestableAgents returns agents available for testing
func (s *AIPlaygroundService) GetTestableAgents(ctx context.Context, tenantID, spaceID, userID string, userTeams []string) ([]models.TestableAgent, error) {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (a:Agent)
		WHERE a.tenant_id = $tenant_id
		  AND (
		    a.owner_id = $user_id
		    OR a.visibility = 'public'
		    OR (a.visibility = 'shared' AND a.space_id = $space_id)
		    OR a.type = 'system'
		  )
		RETURN a.id AS id, a.name AS name, a.description AS description,
		       COALESCE(a.type, 'user') AS type, a.owner_id AS owner_id,
		       a.model AS model, a.tools AS tools, a.system_prompt AS system_prompt,
		       a.created_at AS created_at
		ORDER BY a.name
	`

	result, err := session.Run(ctx, query, map[string]any{
		"tenant_id": tenantID,
		"space_id":  spaceID,
		"user_id":   userID,
	})
	if err != nil {
		return nil, errors.Internal("Failed to fetch agents: " + err.Error())
	}

	var agents []models.TestableAgent
	for result.Next(ctx) {
		record := result.Record()
		agent := models.TestableAgent{
			ID:          getStringValue(record, "id"),
			Name:        getStringValue(record, "name"),
			Description: getStringValue(record, "description"),
			Type:        getStringValue(record, "type"),
			OwnerID:     getStringValue(record, "owner_id"),
			Model:       getStringValue(record, "model"),
			SystemPrompt: getStringValue(record, "system_prompt"),
		}

		if tools, ok := record.Get("tools"); ok && tools != nil {
			if toolsArr, ok := tools.([]any); ok {
				for _, t := range toolsArr {
					if str, ok := t.(string); ok {
						agent.Tools = append(agent.Tools, str)
					}
				}
			}
		}

		if createdAt, ok := record.Get("created_at"); ok && createdAt != nil {
			if t, ok := createdAt.(time.Time); ok {
				agent.CreatedAt = t
			}
		}

		agents = append(agents, agent)
	}

	return agents, nil
}

// TestAgent executes a test against an agent
func (s *AIPlaygroundService) TestAgent(ctx context.Context, req models.AgentTestRequest, tenantID, spaceID, userID string, userTeams []string, authToken string) (*models.AgentTestResponse, error) {
	// For now, return a mock response indicating the agent test infrastructure is being built
	// This will be integrated with the actual agent-builder service

	// First, verify the agent exists and is accessible
	agents, err := s.GetTestableAgents(ctx, tenantID, spaceID, userID, userTeams)
	if err != nil {
		return nil, err
	}

	var targetAgent *models.TestableAgent
	for _, a := range agents {
		if a.ID == req.AgentID {
			targetAgent = &a
			break
		}
	}

	if targetAgent == nil {
		return nil, errors.NotFound("Agent not found or not accessible")
	}

	startTime := time.Now()

	// TODO: Integrate with actual agent-builder execution
	// For now, return a placeholder response
	return &models.AgentTestResponse{
		AgentID:   req.AgentID,
		AgentName: targetAgent.Name,
		ExecutionTrace: []models.ExecutionStep{
			{
				StepNumber: 1,
				Type:       "reasoning",
				Content:    "Agent testing infrastructure is being integrated...",
				TimingMs:   100,
				Timestamp:  time.Now(),
			},
		},
		FinalResponse: fmt.Sprintf("Agent '%s' received input: %s\n\nNote: Full agent execution will be available once integrated with agent-builder service.", targetAgent.Name, req.Input),
		Metrics: &models.AgentMetrics{
			TotalDurationMs: time.Since(startTime).Milliseconds(),
			StepCount:       1,
		},
		CreatedAt: time.Now(),
	}, nil
}

// ======================================================================
// Workflow Testing Methods
// ======================================================================

// GetTestableWorkflows returns workflows available for testing
func (s *AIPlaygroundService) GetTestableWorkflows(ctx context.Context, tenantID, spaceID, userID string) ([]models.TestableWorkflow, error) {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (w:Workflow)
		WHERE w.tenant_id = $tenant_id
		  AND (
		    w.created_by = $user_id
		    OR w.visibility = 'public'
		    OR (w.visibility = 'shared' AND w.organization_id = $space_id)
		    OR w.type = 'system'
		  )
		OPTIONAL MATCH (w)-[:HAS_STEP]->(s:WorkflowStep)
		WITH w, count(s) as step_count
		RETURN w.id AS id, w.name AS name, w.description AS description,
		       COALESCE(w.type, 'user') AS type, w.created_by AS owner_id,
		       step_count, w.created_at AS created_at
		ORDER BY w.name
	`

	result, err := session.Run(ctx, query, map[string]any{
		"tenant_id": tenantID,
		"space_id":  spaceID,
		"user_id":   userID,
	})
	if err != nil {
		return nil, errors.Internal("Failed to fetch workflows: " + err.Error())
	}

	var workflows []models.TestableWorkflow
	for result.Next(ctx) {
		record := result.Record()
		workflow := models.TestableWorkflow{
			ID:          getStringValue(record, "id"),
			Name:        getStringValue(record, "name"),
			Description: getStringValue(record, "description"),
			Type:        getStringValue(record, "type"),
			OwnerID:     getStringValue(record, "owner_id"),
		}

		if sc, ok := record.Get("step_count"); ok && sc != nil {
			if count, ok := sc.(int64); ok {
				workflow.StepCount = int(count)
			}
		}

		if createdAt, ok := record.Get("created_at"); ok && createdAt != nil {
			if t, ok := createdAt.(time.Time); ok {
				workflow.CreatedAt = t
			}
		}

		workflows = append(workflows, workflow)
	}

	return workflows, nil
}

// TestWorkflow executes a test against a workflow
func (s *AIPlaygroundService) TestWorkflow(ctx context.Context, req models.WorkflowTestRequest, tenantID, spaceID, userID string) (*models.WorkflowTestResponse, error) {
	// Verify workflow exists and is accessible
	workflows, err := s.GetTestableWorkflows(ctx, tenantID, spaceID, userID)
	if err != nil {
		return nil, err
	}

	var targetWorkflow *models.TestableWorkflow
	for _, w := range workflows {
		if w.ID == req.WorkflowID {
			targetWorkflow = &w
			break
		}
	}

	if targetWorkflow == nil {
		return nil, errors.NotFound("Workflow not found or not accessible")
	}

	startTime := time.Now()

	// TODO: Integrate with actual workflow-builder execution
	// For now, return a placeholder response
	return &models.WorkflowTestResponse{
		WorkflowID:   req.WorkflowID,
		WorkflowName: targetWorkflow.Name,
		Status:       "completed",
		CurrentStep:  targetWorkflow.StepCount,
		TotalSteps:   targetWorkflow.StepCount,
		ExecutionSteps: []models.WorkflowExecutionStep{
			{
				StepNumber: 1,
				StepName:   "Initialize",
				StepType:   "system",
				Status:     "completed",
				Input:      req.InputParams,
				Output:     map[string]any{"message": "Workflow testing infrastructure is being integrated..."},
				TimingMs:   50,
			},
		},
		FinalOutput: map[string]any{
			"message": fmt.Sprintf("Workflow '%s' received %d input parameters.\n\nNote: Full workflow execution will be available once integrated with workflow-builder service.", targetWorkflow.Name, len(req.InputParams)),
		},
		Metrics: &models.WorkflowMetrics{
			TotalDurationMs: time.Since(startTime).Milliseconds(),
			StepsCompleted:  1,
		},
		CreatedAt: time.Now(),
	}, nil
}

// ======================================================================
// Saved Prompt Methods
// ======================================================================

// CreateSavedPrompt creates a new saved prompt
func (s *AIPlaygroundService) CreateSavedPrompt(ctx context.Context, userID, tenantID, spaceID string, req models.SavedPromptCreateRequest) (*models.SavedPrompt, error) {
	prompt := models.NewSavedPrompt(userID, tenantID, spaceID, req)

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Serialize default models as JSON
	defaultModelsJSON, _ := json.Marshal(prompt.DefaultModels)
	tagsJSON, _ := json.Marshal(prompt.Tags)

	query := `
		CREATE (p:SavedPrompt {
			id: $id,
			name: $name,
			description: $description,
			system_prompt: $system_prompt,
			user_prompt: $user_prompt,
			default_models: $default_models,
			tenant_id: $tenant_id,
			space_id: $space_id,
			owner_id: $owner_id,
			visibility: $visibility,
			tags: $tags,
			folder: $folder,
			use_count: $use_count,
			created_at: $created_at,
			updated_at: $updated_at
		})
		RETURN p
	`

	_, err := session.Run(ctx, query, map[string]any{
		"id":             prompt.ID,
		"name":           prompt.Name,
		"description":    prompt.Description,
		"system_prompt":  prompt.SystemPrompt,
		"user_prompt":    prompt.UserPrompt,
		"default_models": string(defaultModelsJSON),
		"tenant_id":      prompt.TenantID,
		"space_id":       prompt.SpaceID,
		"owner_id":       prompt.OwnerID,
		"visibility":     string(prompt.Visibility),
		"tags":           string(tagsJSON),
		"folder":         prompt.Folder,
		"use_count":      prompt.UseCount,
		"created_at":     prompt.CreatedAt,
		"updated_at":     prompt.UpdatedAt,
	})
	if err != nil {
		s.logger.Error("Failed to create saved prompt", zap.Error(err))
		return nil, errors.Internal("Failed to create saved prompt: " + err.Error())
	}

	return prompt, nil
}

// ListSavedPrompts returns saved prompts accessible to the user
func (s *AIPlaygroundService) ListSavedPrompts(ctx context.Context, tenantID, spaceID, userID string, userTeams []string, searchReq models.SavedPromptSearchRequest) (*models.SavedPromptListResponse, error) {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Build dynamic query based on filters
	whereClause := "WHERE p.tenant_id = $tenant_id AND (p.owner_id = $user_id OR p.visibility = 'public' OR (p.visibility = 'shared' AND p.space_id = $space_id))"

	params := map[string]any{
		"tenant_id": tenantID,
		"space_id":  spaceID,
		"user_id":   userID,
	}

	if searchReq.Search != "" {
		whereClause += " AND (toLower(p.name) CONTAINS toLower($search) OR toLower(p.description) CONTAINS toLower($search))"
		params["search"] = searchReq.Search
	}

	if searchReq.Folder != "" {
		whereClause += " AND p.folder = $folder"
		params["folder"] = searchReq.Folder
	}

	if searchReq.Visibility != "" {
		whereClause += " AND p.visibility = $visibility"
		params["visibility"] = string(searchReq.Visibility)
	}

	// Count query
	countQuery := fmt.Sprintf(`MATCH (p:SavedPrompt) %s RETURN count(p) as total`, whereClause)
	countResult, err := session.Run(ctx, countQuery, params)
	if err != nil {
		return nil, errors.Internal("Failed to count prompts: " + err.Error())
	}

	var totalCount int
	if countResult.Next(ctx) {
		if total, ok := countResult.Record().Get("total"); ok {
			if t, ok := total.(int64); ok {
				totalCount = int(t)
			}
		}
	}

	// Pagination
	page := searchReq.Page
	if page < 1 {
		page = 1
	}
	pageSize := searchReq.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	skip := (page - 1) * pageSize

	// Sort
	sortBy := "created_at"
	if searchReq.SortBy != "" {
		sortBy = searchReq.SortBy
	}
	sortOrder := "DESC"
	if searchReq.SortOrder == "asc" {
		sortOrder = "ASC"
	}

	// Main query
	query := fmt.Sprintf(`
		MATCH (p:SavedPrompt)
		%s
		RETURN p
		ORDER BY p.%s %s
		SKIP $skip LIMIT $limit
	`, whereClause, sortBy, sortOrder)

	params["skip"] = skip
	params["limit"] = pageSize

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, errors.Internal("Failed to fetch prompts: " + err.Error())
	}

	var prompts []models.SavedPromptResponse
	for result.Next(ctx) {
		record := result.Record()
		node, ok := record.Get("p")
		if !ok {
			continue
		}

		prompt := s.recordToSavedPrompt(node.(neo4j.Node))
		prompts = append(prompts, *prompt.ToResponse())
	}

	totalPages := (totalCount + pageSize - 1) / pageSize

	return &models.SavedPromptListResponse{
		Prompts:    prompts,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

// GetSavedPrompt retrieves a saved prompt by ID
func (s *AIPlaygroundService) GetSavedPrompt(ctx context.Context, promptID, tenantID string) (*models.SavedPrompt, error) {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (p:SavedPrompt {id: $id, tenant_id: $tenant_id})
		RETURN p
	`

	result, err := session.Run(ctx, query, map[string]any{
		"id":        promptID,
		"tenant_id": tenantID,
	})
	if err != nil {
		return nil, errors.Internal("Failed to fetch prompt: " + err.Error())
	}

	if !result.Next(ctx) {
		return nil, errors.NotFound("Saved prompt not found")
	}

	node, ok := result.Record().Get("p")
	if !ok {
		return nil, errors.NotFound("Saved prompt not found")
	}

	return s.recordToSavedPrompt(node.(neo4j.Node)), nil
}

// UpdateSavedPrompt updates a saved prompt
func (s *AIPlaygroundService) UpdateSavedPrompt(ctx context.Context, promptID, tenantID, userID string, req models.SavedPromptUpdateRequest) (*models.SavedPrompt, error) {
	prompt, err := s.GetSavedPrompt(ctx, promptID, tenantID)
	if err != nil {
		return nil, err
	}

	if !prompt.CanBeModifiedBy(userID) {
		return nil, errors.Forbidden("Only the owner can modify this prompt")
	}

	prompt.Update(req)

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	defaultModelsJSON, _ := json.Marshal(prompt.DefaultModels)
	tagsJSON, _ := json.Marshal(prompt.Tags)
	sharedTeamsJSON, _ := json.Marshal(prompt.SharedWithTeams)

	query := `
		MATCH (p:SavedPrompt {id: $id, tenant_id: $tenant_id})
		SET p.name = $name,
		    p.description = $description,
		    p.system_prompt = $system_prompt,
		    p.user_prompt = $user_prompt,
		    p.default_models = $default_models,
		    p.visibility = $visibility,
		    p.shared_with_teams = $shared_with_teams,
		    p.tags = $tags,
		    p.folder = $folder,
		    p.updated_at = $updated_at
		RETURN p
	`

	_, err = session.Run(ctx, query, map[string]any{
		"id":                promptID,
		"tenant_id":         tenantID,
		"name":              prompt.Name,
		"description":       prompt.Description,
		"system_prompt":     prompt.SystemPrompt,
		"user_prompt":       prompt.UserPrompt,
		"default_models":    string(defaultModelsJSON),
		"visibility":        string(prompt.Visibility),
		"shared_with_teams": string(sharedTeamsJSON),
		"tags":              string(tagsJSON),
		"folder":            prompt.Folder,
		"updated_at":        prompt.UpdatedAt,
	})
	if err != nil {
		return nil, errors.Internal("Failed to update prompt: " + err.Error())
	}

	return prompt, nil
}

// DeleteSavedPrompt deletes a saved prompt
func (s *AIPlaygroundService) DeleteSavedPrompt(ctx context.Context, promptID, tenantID, userID string) error {
	prompt, err := s.GetSavedPrompt(ctx, promptID, tenantID)
	if err != nil {
		return err
	}

	if !prompt.CanBeModifiedBy(userID) {
		return errors.Forbidden("Only the owner can delete this prompt")
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (p:SavedPrompt {id: $id, tenant_id: $tenant_id})
		DETACH DELETE p
	`

	_, err = session.Run(ctx, query, map[string]any{
		"id":        promptID,
		"tenant_id": tenantID,
	})
	if err != nil {
		return errors.Internal("Failed to delete prompt: " + err.Error())
	}

	return nil
}

// RecordPromptUsage records usage of a saved prompt
func (s *AIPlaygroundService) RecordPromptUsage(ctx context.Context, promptID, tenantID, userID string) error {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (p:SavedPrompt {id: $id, tenant_id: $tenant_id})
		SET p.use_count = p.use_count + 1,
		    p.last_used_at = datetime(),
		    p.last_used_by = $user_id
		RETURN p
	`

	_, err := session.Run(ctx, query, map[string]any{
		"id":        promptID,
		"tenant_id": tenantID,
		"user_id":   userID,
	})
	if err != nil {
		s.logger.Warn("Failed to record prompt usage", zap.Error(err), zap.String("prompt_id", promptID))
	}

	return nil
}

// ======================================================================
// Helper Methods
// ======================================================================

func (s *AIPlaygroundService) recordToSavedPrompt(node neo4j.Node) *models.SavedPrompt {
	props := node.Props

	prompt := &models.SavedPrompt{
		ID:           getStringFromProps(props, "id"),
		Name:         getStringFromProps(props, "name"),
		Description:  getStringFromProps(props, "description"),
		SystemPrompt: getStringFromProps(props, "system_prompt"),
		UserPrompt:   getStringFromProps(props, "user_prompt"),
		TenantID:     getStringFromProps(props, "tenant_id"),
		SpaceID:      getStringFromProps(props, "space_id"),
		OwnerID:      getStringFromProps(props, "owner_id"),
		Visibility:   models.PromptVisibility(getStringFromProps(props, "visibility")),
		Folder:       getStringFromProps(props, "folder"),
		UseCount:     getIntFromProps(props, "use_count"),
		LastUsedBy:   getStringFromProps(props, "last_used_by"),
	}

	// Parse JSON fields
	if defaultModelsStr := getStringFromProps(props, "default_models"); defaultModelsStr != "" {
		_ = json.Unmarshal([]byte(defaultModelsStr), &prompt.DefaultModels)
	}
	if tagsStr := getStringFromProps(props, "tags"); tagsStr != "" {
		_ = json.Unmarshal([]byte(tagsStr), &prompt.Tags)
	}
	if sharedTeamsStr := getStringFromProps(props, "shared_with_teams"); sharedTeamsStr != "" {
		_ = json.Unmarshal([]byte(sharedTeamsStr), &prompt.SharedWithTeams)
	}

	// Parse timestamps
	if createdAt, ok := props["created_at"]; ok && createdAt != nil {
		if t, ok := createdAt.(time.Time); ok {
			prompt.CreatedAt = t
		}
	}
	if updatedAt, ok := props["updated_at"]; ok && updatedAt != nil {
		if t, ok := updatedAt.(time.Time); ok {
			prompt.UpdatedAt = t
		}
	}
	if lastUsedAt, ok := props["last_used_at"]; ok && lastUsedAt != nil {
		if t, ok := lastUsedAt.(time.Time); ok {
			prompt.LastUsedAt = &t
		}
	}

	return prompt
}

// getStringValue safely extracts a string value from a Neo4j record
func getStringValue(record *neo4j.Record, key string) string {
	if val, ok := record.Get(key); ok && val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// getStringFromProps safely extracts a string value from Neo4j props map
func getStringFromProps(props map[string]any, key string) string {
	if val, ok := props[key]; ok && val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// getIntFromProps safely extracts an int value from Neo4j props map
func getIntFromProps(props map[string]any, key string) int {
	if val, ok := props[key]; ok && val != nil {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return 0
}
