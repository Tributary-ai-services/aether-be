package services

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

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// AgentService handles agent-related business logic with dual responsibility:
// 1. Neo4j relationship management for agents, users, teams, and knowledge sources
// 2. Proxy operations to agent-builder service for actual agent CRUD
type AgentService struct {
	neo4j          *database.Neo4jClient
	userService    *UserService
	notebookService *NotebookService
	teamService    *TeamService
	agentBuilderURL string
	httpClient     *http.Client
	logger         *logger.Logger
}

// AgentBuilderClient handles communication with the agent-builder service
type AgentBuilderClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *logger.Logger
}

// NewAgentService creates a new agent service
func NewAgentService(
	neo4j *database.Neo4jClient,
	userService *UserService,
	notebookService *NotebookService,
	teamService *TeamService,
	agentBuilderURL string,
	log *logger.Logger,
) *AgentService {
	return &AgentService{
		neo4j:           neo4j,
		userService:     userService,
		notebookService: notebookService,
		teamService:     teamService,
		agentBuilderURL: strings.TrimSuffix(agentBuilderURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: log.WithService("agent_service"),
	}
}

// CreateAgent creates a new agent by:
// 1. Creating the agent in agent-builder (PostgreSQL)
// 2. Creating the agent metadata in Neo4j for relationship management
func (s *AgentService) CreateAgent(ctx context.Context, req models.AgentCreateRequest, spaceCtx *models.SpaceContext, authToken string) (*models.AgentResponse, error) {
	// Validate space context and permissions
	if !spaceCtx.CanCreate() {
		return nil, errors.Forbidden("Insufficient permissions to create agent")
	}

	// Step 1: Create agent in agent-builder
	agentBuilderResp, err := s.createAgentInBuilder(ctx, req, authToken)
	if err != nil {
		s.logger.Error("Failed to create agent in agent-builder", zap.Error(err))
		return nil, err
	}

	// Step 2: Create agent metadata in Neo4j
	agent := models.NewAgent(req, spaceCtx.UserID, spaceCtx)
	agent.AgentBuilderID = agentBuilderResp["agent"].(map[string]interface{})["id"].(string)

	if err := s.createAgentInNeo4j(ctx, agent); err != nil {
		s.logger.Error("Failed to create agent in Neo4j", zap.Error(err))
		// TODO: Consider rollback of agent-builder creation
		return nil, err
	}

	// Step 3: Create ownership relationship
	if err := s.createOwnershipRelationship(ctx, agent.ID, spaceCtx.UserID); err != nil {
		s.logger.Error("Failed to create ownership relationship", zap.Error(err))
		return nil, err
	}

	// Step 4: Create team relationship if in organization space
	if agent.IsOrganizationAgent() && agent.TeamID != "" {
		if err := s.createTeamRelationship(ctx, agent.ID, agent.TeamID, spaceCtx.UserID); err != nil {
			s.logger.Error("Failed to create team relationship", zap.Error(err))
			return nil, err
		}
	}

	return s.buildAgentResponse(ctx, agent)
}

// GetAgent retrieves an agent by ID with access control
func (s *AgentService) GetAgent(ctx context.Context, agentID string, userID string, userTeams []string) (*models.AgentResponse, error) {
	agent, err := s.getAgentFromNeo4j(ctx, agentID)
	if err != nil {
		return nil, err
	}

	// Check access permissions
	if !agent.CanBeAccessedBy(userID, userTeams) {
		return nil, errors.NotFound("Agent not found")
	}

	return s.buildAgentResponse(ctx, agent)
}

// canUserModifyAgent checks if a user can modify an agent (owner or team admin)
func (s *AgentService) canUserModifyAgent(ctx context.Context, agent *models.Agent, userID string) (bool, error) {
	// Owner can always modify
	if agent.OwnerID == userID {
		return true, nil
	}
	
	// For organization agents, check if user is team admin
	if agent.IsOrganizationAgent() && agent.TeamID != "" {
		isAdmin, err := s.teamService.IsUserTeamAdmin(ctx, userID, agent.TeamID)
		if err != nil {
			s.logger.Error("Failed to check team admin status", 
				zap.String("user_id", userID),
				zap.String("team_id", agent.TeamID),
				zap.Error(err))
			return false, err
		}
		return isAdmin, nil
	}
	
	return false, nil
}

// UpdateAgent updates an agent by updating both agent-builder and Neo4j
func (s *AgentService) UpdateAgent(ctx context.Context, agentID string, req models.AgentUpdateRequest, userID string, authToken string) (*models.AgentResponse, error) {
	// Get existing agent
	agent, err := s.getAgentFromNeo4j(ctx, agentID)
	if err != nil {
		return nil, err
	}

	// Check if user can modify this agent (owner or team admin)
	canModify, err := s.canUserModifyAgent(ctx, agent, userID)
	if err != nil {
		return nil, err
	}
	if !canModify {
		return nil, errors.Forbidden("Insufficient permissions to update agent")
	}

	// Step 1: Update agent in agent-builder
	if err := s.updateAgentInBuilder(ctx, agent.AgentBuilderID, req, authToken); err != nil {
		s.logger.Error("Failed to update agent in agent-builder", zap.Error(err))
		return nil, err
	}

	// Step 2: Update agent metadata in Neo4j
	agent.Update(req)
	if err := s.updateAgentInNeo4j(ctx, agent); err != nil {
		s.logger.Error("Failed to update agent in Neo4j", zap.Error(err))
		return nil, err
	}

	return s.buildAgentResponse(ctx, agent)
}

// DeleteAgent deletes an agent from both agent-builder and Neo4j
func (s *AgentService) DeleteAgent(ctx context.Context, agentID string, userID string, authToken string) error {
	agent, err := s.getAgentFromNeo4j(ctx, agentID)
	if err != nil {
		return err
	}

	// Check if user can modify this agent (owner or team admin)
	canModify, err := s.canUserModifyAgent(ctx, agent, userID)
	if err != nil {
		return err
	}
	if !canModify {
		return errors.Forbidden("Insufficient permissions to delete agent")
	}

	// Step 1: Delete agent in agent-builder
	if err := s.deleteAgentInBuilder(ctx, agent.AgentBuilderID, authToken); err != nil {
		s.logger.Error("Failed to delete agent in agent-builder", zap.Error(err))
		return err
	}

	// Step 2: Delete agent and all relationships in Neo4j
	if err := s.deleteAgentInNeo4j(ctx, agentID); err != nil {
		s.logger.Error("Failed to delete agent in Neo4j", zap.Error(err))
		return err
	}

	return nil
}

// ListAgents retrieves agents with filtering and pagination by proxying to agent-builder
func (s *AgentService) ListAgents(ctx context.Context, req models.AgentSearchRequest, userID string, userTeams []string, authToken string) (*models.AgentListResponse, error) {
	// Convert aether-be search request to agent-builder format
	builderReq := map[string]interface{}{
		"page": (req.Offset / req.Limit) + 1, // Convert offset to page number
		"size": req.Limit,
	}
	
	if req.Query != "" {
		builderReq["search"] = req.Query
	}
	if req.Status != "" {
		builderReq["status"] = string(req.Status)
	}
	if req.IsPublic != nil {
		builderReq["is_public"] = *req.IsPublic
	}
	if req.IsTemplate != nil {
		builderReq["is_template"] = *req.IsTemplate
	}
	if len(req.Tags) > 0 {
		builderReq["tags"] = req.Tags
	}
	
	// Make request to agent-builder with query params
	endpoint := "/agents"
	requestURL := s.agentBuilderURL + endpoint
	
	// Build query string
	params := make([]string, 0)
	for key, value := range builderReq {
		switch v := value.(type) {
		case string:
			params = append(params, fmt.Sprintf("%s=%s", key, url.QueryEscape(v)))
		case int:
			params = append(params, fmt.Sprintf("%s=%d", key, v))
		case bool:
			params = append(params, fmt.Sprintf("%s=%t", key, v))
		case []string:
			for _, item := range v {
				params = append(params, fmt.Sprintf("%s=%s", key, url.QueryEscape(item)))
			}
		}
	}
	
	if len(params) > 0 {
		requestURL += "?" + strings.Join(params, "&")
	}
	
	httpReq, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, errors.ExternalService("Failed to create agent-builder request", err)
	}
	
	httpReq.Header.Set("Authorization", "Bearer "+authToken)
	httpReq.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, errors.ExternalService("Agent-builder service unavailable", err)
	}
	defer resp.Body.Close()
	
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.ExternalService("Failed to read agent-builder response", err)
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, errors.ExternalService(fmt.Sprintf("Agent-builder error: %s", string(bodyBytes)), nil)
	}
	
	var builderResp map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &builderResp); err != nil {
		return nil, errors.ExternalService("Failed to parse agent-builder response", err)
	}
	
	// Convert agent-builder response to aether-be format
	agentsList, ok := builderResp["agents"].([]interface{})
	if !ok {
		return nil, errors.ExternalService("Invalid agent-builder response format", nil)
	}
	
	agents := make([]*models.AgentResponse, 0, len(agentsList))
	for _, agentData := range agentsList {
		agentMap, ok := agentData.(map[string]interface{})
		if !ok {
			continue
		}
		
		// Safe string extraction helper
		safeString := func(key string) string {
			if val, ok := agentMap[key]; ok && val != nil {
				if str, ok := val.(string); ok {
					return str
				}
			}
			return ""
		}
		
		// Safe bool extraction helper
		safeBool := func(key string) bool {
			if val, ok := agentMap[key]; ok && val != nil {
				if b, ok := val.(bool); ok {
					return b
				}
			}
			return false
		}
		
		// Parse timestamps safely
		var createdAt, updatedAt time.Time
		if createdAtStr := safeString("created_at"); createdAtStr != "" {
			createdAt, _ = time.Parse(time.RFC3339, createdAtStr)
		}
		if updatedAtStr := safeString("updated_at"); updatedAtStr != "" {
			updatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
		}
		
		agent := &models.AgentResponse{
			ID:           safeString("id"),
			Name:         safeString("name"),
			Description:  safeString("description"),
			Status:       models.AgentStatus(safeString("status")),
			IsPublic:     safeBool("is_public"),
			IsTemplate:   safeBool("is_template"),
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
		}
		
		agents = append(agents, agent)
	}
	
	// Get total count from response safely
	var total, page, size int
	if totalVal, ok := builderResp["total"]; ok && totalVal != nil {
		if totalFloat, ok := totalVal.(float64); ok {
			total = int(totalFloat)
		}
	}
	if pageVal, ok := builderResp["page"]; ok && pageVal != nil {
		if pageFloat, ok := pageVal.(float64); ok {
			page = int(pageFloat)
		} else {
			page = 1 // default
		}
	} else {
		page = 1 // default
	}
	if sizeVal, ok := builderResp["size"]; ok && sizeVal != nil {
		if sizeFloat, ok := sizeVal.(float64); ok {
			size = int(sizeFloat)
		} else {
			size = req.Limit // default to request limit
		}
	} else {
		size = req.Limit // default to request limit
	}
	
	return &models.AgentListResponse{
		Agents:  agents,
		Total:   total,
		Limit:   req.Limit,
		Offset:  req.Offset,
		HasMore: (page * size) < total,
	}, nil
}

// AddKnowledgeSource links an agent to a notebook for vector search
func (s *AgentService) AddKnowledgeSource(ctx context.Context, agentID, notebookID, userID string, config models.VectorSearchSpace) error {
	// Verify agent access
	agent, err := s.getAgentFromNeo4j(ctx, agentID)
	if err != nil {
		return err
	}

	if agent.OwnerID != userID {
		return errors.Forbidden("Insufficient permissions to modify agent knowledge sources")
	}

	// Verify notebook access
	notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, nil)
	if err != nil {
		return errors.NotFound("Notebook not found or access denied")
	}

	// Create SEARCHES_IN relationship
	query := `
		MATCH (a:Agent {id: $agentId}), (n:Notebook {id: $notebookId})
		CREATE (a)-[:SEARCHES_IN {
			added_at: datetime($addedAt),
			added_by: $addedBy,
			search_strategy: $searchStrategy,
			search_weight: $searchWeight,
			filters: $filters
		}]->(n)
	`

	params := map[string]interface{}{
		"agentId":        agentID,
		"notebookId":     notebookID,
		"addedAt":        time.Now().Format(time.RFC3339),
		"addedBy":        userID,
		"searchStrategy": "hybrid", // Default to hybrid search
		"searchWeight":   config.SearchWeight,
		"filters":        s.filtersToJSON(config.Filters),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return errors.Database("Failed to add knowledge source", err)
	}

	s.logger.Info("Added knowledge source to agent",
		zap.String("agent_id", agentID),
		zap.String("notebook_id", notebookID),
		zap.String("notebook_name", notebook.Name),
	)

	return nil
}

// RemoveKnowledgeSource removes a notebook link from an agent
func (s *AgentService) RemoveKnowledgeSource(ctx context.Context, agentID, notebookID, userID string) error {
	// Verify agent access
	agent, err := s.getAgentFromNeo4j(ctx, agentID)
	if err != nil {
		return err
	}

	if agent.OwnerID != userID {
		return errors.Forbidden("Insufficient permissions to modify agent knowledge sources")
	}

	query := `
		MATCH (a:Agent {id: $agentId})-[r:SEARCHES_IN]->(n:Notebook {id: $notebookId})
		DELETE r
	`

	params := map[string]interface{}{
		"agentId":    agentID,
		"notebookId": notebookID,
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return errors.Database("Failed to remove knowledge source", err)
	}

	return nil
}

// GetAgentKnowledgeSources retrieves all knowledge sources for an agent
func (s *AgentService) GetAgentKnowledgeSources(ctx context.Context, agentID, userID string) ([]models.VectorSearchSpace, error) {
	// Verify agent access
	agent, err := s.getAgentFromNeo4j(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if agent.OwnerID != userID {
		return nil, errors.Forbidden("Insufficient permissions to view agent knowledge sources")
	}

	query := `
		MATCH (a:Agent {id: $agentId})-[r:SEARCHES_IN]->(n:Notebook)
		RETURN n.id as notebook_id, n.name as notebook_name,
		       r.search_weight as search_weight, r.filters as filters,
		       r.added_at as added_at, r.added_by as added_by
		ORDER BY r.search_weight DESC
	`

	params := map[string]interface{}{
		"agentId": agentID,
	}

	records, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return nil, errors.Database("Failed to get agent knowledge sources", err)
	}

	sources := make([]models.VectorSearchSpace, 0)
	for _, record := range records.Records {
		source := models.VectorSearchSpace{
			NotebookID:   record.Values[0].(string),
			NotebookName: record.Values[1].(string),
			SearchWeight: record.Values[2].(float64),
			Filters:      s.jsonToFilters(record.Values[3].(string)),
		}

		if addedAt, ok := record.Values[4].(time.Time); ok {
			source.AddedAt = addedAt
		}
		if addedBy, ok := record.Values[5].(string); ok {
			source.AddedBy = addedBy
		}

		sources = append(sources, source)
	}

	return sources, nil
}

// ExecuteAgent executes an agent with the provided input and handles type-specific logic
func (s *AgentService) ExecuteAgent(ctx context.Context, agentID string, req models.AgentExecuteRequest, userID string, userTeams []string, authToken string) (*models.AgentExecuteResponse, error) {
	// Get agent from agent-builder service (where agents actually live)
	agent, err := s.getAgentFromBuilder(ctx, agentID, authToken)
	if err != nil {
		return nil, errors.NotFound("Agent not found")
	}

	// Check access permissions based on space and ownership
	if !s.canUserAccessAgent(agent, userID, userTeams) {
		return nil, errors.NotFound("Agent not found")
	}

	// Prepare execution request for agent-builder
	builderReq, err := s.prepareExecutionRequest(ctx, agent, req, userID)
	if err != nil {
		s.logger.Error("Failed to prepare execution request", zap.Error(err))
		return nil, err
	}

	// Execute agent in agent-builder with fallback to direct execution
	startTime := time.Now()
	builderResp, err := s.executeAgentInBuilder(ctx, agent.AgentBuilderID, builderReq, authToken)
	if err != nil {
		s.logger.Warn("Agent-builder unavailable, falling back to direct execution", 
			zap.String("agent_id", agentID),
			zap.Error(err))
		
		// Fall back to direct LLM execution
		response, directErr := s.executeAgentDirect(ctx, agent, req, userID, authToken)
		if directErr != nil {
			s.logger.Error("Direct execution also failed", zap.Error(directErr))
			return nil, errors.ExternalService("Both agent-builder and direct execution failed", directErr)
		}
		
		// Update agent statistics for direct execution
		if err := s.updateAgentExecutionStats(ctx, agent, response); err != nil {
			s.logger.Error("Failed to update agent execution stats", zap.Error(err))
		}
		
		return response, nil
	}
	endTime := time.Now()

	// Build response for agent-builder execution
	response, err := s.buildExecutionResponse(ctx, agent, builderResp, startTime, endTime)
	if err != nil {
		s.logger.Error("Failed to build execution response", zap.Error(err))
		return nil, err
	}

	// Update agent statistics
	if err := s.updateAgentExecutionStats(ctx, agent, response); err != nil {
		s.logger.Error("Failed to update agent execution stats", zap.Error(err))
		// Don't fail the request for stats update failure
	}

	return response, nil
}

// Helper methods for agent-builder communication

func (s *AgentService) createAgentInBuilder(ctx context.Context, req models.AgentCreateRequest, authToken string) (map[string]interface{}, error) {
	// Convert aether-be request to agent-builder format
	builderReq := map[string]interface{}{
		"name":          req.Name,
		"description":   req.Description,
		"type":          string(req.Type),
		"system_prompt": req.SystemPrompt,
		"llm_config":    req.LLMConfig,
		"space_id":      req.SpaceID,
		"is_public":     req.IsPublic,
		"is_template":   req.IsTemplate,
		"tags":          req.Tags,
	}

	return s.makeAgentBuilderRequest(ctx, "POST", "/agents", builderReq, authToken)
}

func (s *AgentService) updateAgentInBuilder(ctx context.Context, agentBuilderID string, req models.AgentUpdateRequest, authToken string) error {
	// Convert update request to agent-builder format
	builderReq := make(map[string]interface{})

	if req.Name != nil {
		builderReq["name"] = *req.Name
	}
	if req.Description != nil {
		builderReq["description"] = *req.Description
	}
	if req.Type != nil {
		builderReq["type"] = string(*req.Type)
	}
	if req.Status != nil {
		builderReq["status"] = *req.Status
	}
	if req.IsPublic != nil {
		builderReq["is_public"] = *req.IsPublic
	}
	if req.IsTemplate != nil {
		builderReq["is_template"] = *req.IsTemplate
	}
	if req.Tags != nil {
		builderReq["tags"] = req.Tags
	}
	if req.SystemPrompt != nil {
		builderReq["system_prompt"] = *req.SystemPrompt
	}
	if req.LLMConfig != nil {
		builderReq["llm_config"] = req.LLMConfig
	}

	_, err := s.makeAgentBuilderRequest(ctx, "PUT", fmt.Sprintf("/agents/%s", agentBuilderID), builderReq, authToken)
	return err
}

func (s *AgentService) deleteAgentInBuilder(ctx context.Context, agentBuilderID string, authToken string) error {
	_, err := s.makeAgentBuilderRequest(ctx, "DELETE", fmt.Sprintf("/agents/%s", agentBuilderID), nil, authToken)
	return err
}

func (s *AgentService) makeAgentBuilderRequest(ctx context.Context, method, path string, body interface{}, authToken string) (map[string]interface{}, error) {
	url := s.agentBuilderURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, errors.InternalWithCause("Failed to marshal request body", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, errors.InternalWithCause("Failed to create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, errors.ExternalService("Agent-builder service unavailable", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, errors.ExternalService(fmt.Sprintf("Agent-builder error: %s", string(bodyBytes)), nil)
	}

	if method == "DELETE" {
		return nil, nil // DELETE requests don't return a body
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, errors.InternalWithCause("Failed to decode response", err)
	}

	return result, nil
}

// Helper methods for Neo4j operations

func (s *AgentService) createAgentInNeo4j(ctx context.Context, agent *models.Agent) error {
	query := `
		CREATE (a:Agent {
			id: $id,
			agent_builder_id: $agentBuilderId,
			name: $name,
			description: $description,
			status: $status,
			type: $type,
			owner_id: $ownerId,
			space_type: $spaceType,
			space_id: $spaceId,
			tenant_id: $tenantId,
			team_id: $teamId,
			is_public: $isPublic,
			is_template: $isTemplate,
			tags: $tags,
			search_text: $searchText,
			total_executions: $totalExecutions,
			total_cost_usd: $totalCostUSD,
			avg_response_time_ms: $avgResponseTimeMs,
			created_at: datetime($createdAt),
			updated_at: datetime($updatedAt)
		})
	`

	params := map[string]interface{}{
		"id":                 agent.ID,
		"agentBuilderId":     agent.AgentBuilderID,
		"name":               agent.Name,
		"description":        agent.Description,
		"status":             string(agent.Status),
		"type":               string(agent.Type),
		"ownerId":            agent.OwnerID,
		"spaceType":          string(agent.SpaceType),
		"spaceId":            agent.SpaceID,
		"tenantId":           agent.TenantID,
		"teamId":             agent.TeamID,
		"isPublic":           agent.IsPublic,
		"isTemplate":         agent.IsTemplate,
		"tags":               agent.Tags,
		"searchText":         agent.SearchText,
		"totalExecutions":    agent.TotalExecutions,
		"totalCostUSD":       agent.TotalCostUSD,
		"avgResponseTimeMs":  agent.AvgResponseTimeMs,
		"createdAt":          agent.CreatedAt.Format(time.RFC3339),
		"updatedAt":          agent.UpdatedAt.Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return errors.Database("Failed to create agent in Neo4j", err)
	}

	return nil
}

func (s *AgentService) updateAgentInNeo4j(ctx context.Context, agent *models.Agent) error {
	query := `
		MATCH (a:Agent {id: $id})
		SET a.name = $name,
		    a.description = $description,
		    a.status = $status,
		    a.type = $type,
		    a.is_public = $isPublic,
		    a.is_template = $isTemplate,
		    a.tags = $tags,
		    a.search_text = $searchText,
		    a.updated_at = datetime($updatedAt)
	`

	params := map[string]interface{}{
		"id":          agent.ID,
		"name":        agent.Name,
		"description": agent.Description,
		"status":      string(agent.Status),
		"type":        string(agent.Type),
		"isPublic":    agent.IsPublic,
		"isTemplate":  agent.IsTemplate,
		"tags":        agent.Tags,
		"searchText":  agent.SearchText,
		"updatedAt":   agent.UpdatedAt.Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return errors.Database("Failed to update agent in Neo4j", err)
	}

	return nil
}

func (s *AgentService) deleteAgentInNeo4j(ctx context.Context, agentID string) error {
	query := `
		MATCH (a:Agent {id: $agentId})
		DETACH DELETE a
	`

	params := map[string]interface{}{
		"agentId": agentID,
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return errors.Database("Failed to delete agent in Neo4j", err)
	}

	return nil
}

func (s *AgentService) getAgentFromNeo4j(ctx context.Context, agentID string) (*models.Agent, error) {
	query := `
		MATCH (a:Agent {id: $agentId})
		RETURN a
	`

	params := map[string]interface{}{
		"agentId": agentID,
	}

	records, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return nil, errors.Database("Failed to get agent from Neo4j", err)
	}

	if len(records.Records) == 0 {
		return nil, errors.NotFound("Agent not found")
	}

	return s.recordToAgent(*records.Records[0])
}

func (s *AgentService) createOwnershipRelationship(ctx context.Context, agentID, userID string) error {
	query := `
		MATCH (a:Agent {id: $agentId}), (u:User {id: $userId})
		CREATE (a)-[:OWNED_BY {
			created_at: datetime($createdAt)
		}]->(u)
	`

	params := map[string]interface{}{
		"agentId":   agentID,
		"userId":    userID,
		"createdAt": time.Now().Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return errors.Database("Failed to create ownership relationship", err)
	}

	return nil
}

func (s *AgentService) createTeamRelationship(ctx context.Context, agentID, teamID, userID string) error {
	query := `
		MATCH (a:Agent {id: $agentId}), (t:Team {id: $teamId})
		CREATE (a)-[:MANAGED_BY_TEAM {
			assigned_at: datetime($assignedAt),
			assigned_by: $assignedBy
		}]->(t)
	`

	params := map[string]interface{}{
		"agentId":    agentID,
		"teamId":     teamID,
		"assignedAt": time.Now().Format(time.RFC3339),
		"assignedBy": userID,
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return errors.Database("Failed to create team relationship", err)
	}

	return nil
}

// Helper methods

func (s *AgentService) recordToAgent(record neo4j.Record) (*models.Agent, error) {
	agentNode := record.Values[0].(neo4j.Node)
	props := agentNode.Props

	agent := &models.Agent{
		ID:             props["id"].(string),
		AgentBuilderID: props["agent_builder_id"].(string),
		Name:           props["name"].(string),
		Description:    props["description"].(string),
		Status:         models.AgentStatus(props["status"].(string)),
		Type:           models.AgentType(props["type"].(string)),
		OwnerID:        props["owner_id"].(string),
		SpaceType:      models.SpaceType(props["space_type"].(string)),
		SpaceID:        props["space_id"].(string),
		TenantID:       props["tenant_id"].(string),
		IsPublic:       props["is_public"].(bool),
		IsTemplate:     props["is_template"].(bool),
		SearchText:     props["search_text"].(string),
		TotalExecutions: int(props["total_executions"].(int64)),
		TotalCostUSD:   props["total_cost_usd"].(float64),
		AvgResponseTimeMs: int(props["avg_response_time_ms"].(int64)),
	}

	if teamID, ok := props["team_id"].(string); ok {
		agent.TeamID = teamID
	}

	if tags, ok := props["tags"].([]interface{}); ok {
		agent.Tags = make([]string, len(tags))
		for i, tag := range tags {
			agent.Tags[i] = tag.(string)
		}
	}

	if createdAt, ok := props["created_at"].(time.Time); ok {
		agent.CreatedAt = createdAt
	}
	if updatedAt, ok := props["updated_at"].(time.Time); ok {
		agent.UpdatedAt = updatedAt
	}

	return agent, nil
}

func (s *AgentService) buildAgentResponse(ctx context.Context, agent *models.Agent) (*models.AgentResponse, error) {
	response := agent.ToResponse()

	// Optionally add related data (owner, team, knowledge sources)
	// This can be optimized later with more complex queries

	return response, nil
}

func (s *AgentService) buildListAgentsQuery(req models.AgentSearchRequest, userID string, userTeams []string) string {
	query := `
		MATCH (a:Agent)
		WHERE (
			a.owner_id = $userId OR
			a.is_public = true OR
			(a.team_id IN $userTeams)
		)
	`

	conditions := make([]string, 0)

	if req.Query != "" {
		conditions = append(conditions, "a.search_text CONTAINS $query")
	}
	if req.SpaceID != "" {
		conditions = append(conditions, "a.space_id = $spaceId")
	}
	if req.TeamID != "" {
		conditions = append(conditions, "a.team_id = $teamId")
	}
	if req.Status != "" {
		conditions = append(conditions, "a.status = $status")
	}
	if req.SpaceType != "" {
		conditions = append(conditions, "a.space_type = $spaceType")
	}
	if req.IsPublic != nil {
		conditions = append(conditions, "a.is_public = $isPublic")
	}
	if req.IsTemplate != nil {
		conditions = append(conditions, "a.is_template = $isTemplate")
	}

	if len(conditions) > 0 {
		query += " AND " + strings.Join(conditions, " AND ")
	}

	query += `
		RETURN a
		ORDER BY a.updated_at DESC
	`

	if req.Limit > 0 {
		query += " SKIP $offset LIMIT $limit"
	}

	return query
}

func (s *AgentService) buildListAgentsParams(req models.AgentSearchRequest, userID string, userTeams []string) map[string]interface{} {
	params := map[string]interface{}{
		"userId": userID,
		"userTeams": userTeams,
	}

	if req.Query != "" {
		params["query"] = req.Query
	}
	if req.SpaceID != "" {
		params["spaceId"] = req.SpaceID
	}
	if req.TeamID != "" {
		params["teamId"] = req.TeamID
	}
	if req.Status != "" {
		params["status"] = string(req.Status)
	}
	if req.SpaceType != "" {
		params["spaceType"] = string(req.SpaceType)
	}
	if req.IsPublic != nil {
		params["isPublic"] = *req.IsPublic
	}
	if req.IsTemplate != nil {
		params["isTemplate"] = *req.IsTemplate
	}
	if req.Limit > 0 {
		params["limit"] = req.Limit
		params["offset"] = req.Offset
	}

	return params
}

func (s *AgentService) getAgentCount(ctx context.Context, req models.AgentSearchRequest, userID string, userTeams []string) (int, error) {
	// Similar to list query but with COUNT instead of RETURN
	query := strings.Replace(
		s.buildListAgentsQuery(req, userID, userTeams),
		"RETURN a ORDER BY a.updated_at DESC",
		"RETURN count(a) as total",
		1,
	)

	// Remove SKIP/LIMIT for count
	if strings.Contains(query, "SKIP") {
		parts := strings.Split(query, "SKIP")
		query = parts[0]
	}

	params := s.buildListAgentsParams(req, userID, userTeams)
	delete(params, "limit")
	delete(params, "offset")

	records, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return 0, errors.Database("Failed to count agents", err)
	}

	if len(records.Records) == 0 {
		return 0, nil
	}

	return int(records.Records[0].Values[0].(int64)), nil
}

func (s *AgentService) filtersToJSON(filters map[string]interface{}) string {
	if filters == nil || len(filters) == 0 {
		return "{}"
	}

	bytes, err := json.Marshal(filters)
	if err != nil {
		s.logger.Error("Failed to marshal filters", zap.Error(err))
		return "{}"
	}

	return string(bytes)
}

func (s *AgentService) jsonToFilters(jsonStr string) map[string]interface{} {
	if jsonStr == "" || jsonStr == "{}" {
		return nil
	}

	var filters map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &filters); err != nil {
		s.logger.Error("Failed to unmarshal filters", zap.Error(err))
		return nil
	}

	return filters
}

// Execution helper methods

func (s *AgentService) prepareExecutionRequest(ctx context.Context, agent *models.Agent, req models.AgentExecuteRequest, userID string) (map[string]interface{}, error) {
	// Base execution request
	builderReq := map[string]interface{}{
		"input":   req.Input,
		"context": req.Context,
		"user_id": userID,
	}

	// Add type-specific parameters
	switch agent.Type {
	case models.AgentTypeQA:
		if req.MaxResults != nil {
			builderReq["max_results"] = *req.MaxResults
		}
		if len(req.Sources) > 0 {
			builderReq["sources"] = req.Sources
		}
		
	case models.AgentTypeConversational:
		if req.ConversationID != nil {
			builderReq["conversation_id"] = *req.ConversationID
		}
		if len(req.History) > 0 {
			builderReq["history"] = req.History
		}
		
	case models.AgentTypeProducer:
		if req.Template != nil {
			builderReq["template"] = *req.Template
		}
		if req.TemplateParams != nil {
			builderReq["template_params"] = req.TemplateParams
		}
		if req.OutputFormat != nil {
			builderReq["output_format"] = *req.OutputFormat
		}
	}

	return builderReq, nil
}

func (s *AgentService) executeAgentInBuilder(ctx context.Context, agentBuilderID string, req map[string]interface{}, authToken string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/agents/%s/execute", agentBuilderID)
	return s.makeAgentBuilderRequest(ctx, "POST", path, req, authToken)
}

func (s *AgentService) buildExecutionResponse(ctx context.Context, agent *models.Agent, builderResp map[string]interface{}, startTime, endTime time.Time) (*models.AgentExecuteResponse, error) {
	responseTimeMs := int(endTime.Sub(startTime).Milliseconds())
	
	response := &models.AgentExecuteResponse{
		ExecutionID:    builderResp["execution_id"].(string),
		AgentID:        agent.ID,
		AgentType:      agent.Type,
		Output:         builderResp["output"].(string),
		TokensUsed:     int(builderResp["tokens_used"].(float64)),
		CostUSD:        builderResp["cost_usd"].(float64),
		ResponseTimeMs: responseTimeMs,
		StartedAt:      startTime,
		CompletedAt:    endTime,
	}

	// Add metadata if present
	if metadata, ok := builderResp["metadata"].(map[string]interface{}); ok {
		response.Metadata = metadata
	}

	// Add type-specific response data
	switch agent.Type {
	case models.AgentTypeQA:
		if sources, ok := builderResp["sources"].([]interface{}); ok {
			response.Sources = make([]models.SourceReference, len(sources))
			for i, source := range sources {
				srcMap := source.(map[string]interface{})
				response.Sources[i] = models.SourceReference{
					NotebookID:   srcMap["notebook_id"].(string),
					NotebookName: srcMap["notebook_name"].(string),
					ChunkID:      srcMap["chunk_id"].(string),
					Relevance:    srcMap["relevance"].(float64),
					Content:      srcMap["content"].(string),
				}
			}
		}
		
	case models.AgentTypeConversational:
		if conversationID, ok := builderResp["conversation_id"].(string); ok {
			response.ConversationID = &conversationID
		}
		
	case models.AgentTypeProducer:
		if production, ok := builderResp["production"].(map[string]interface{}); ok {
			response.Production = &models.ProductionResult{
				ID:         production["id"].(string),
				Title:      production["title"].(string),
				Format:     production["format"].(string),
				Content:    production["content"].(string),
				CreatedAt:  endTime,
			}
			if template, ok := production["template"].(string); ok {
				response.Production.Template = template
			}
			if params, ok := production["parameters"].(map[string]interface{}); ok {
				response.Production.Parameters = params
			}
		}
	}

	return response, nil
}

func (s *AgentService) updateAgentExecutionStats(ctx context.Context, agent *models.Agent, response *models.AgentExecuteResponse) error {
	// Update agent execution statistics
	agent.TotalExecutions++
	agent.TotalCostUSD += response.CostUSD
	
	// Update average response time (simple moving average)
	if agent.TotalExecutions == 1 {
		agent.AvgResponseTimeMs = response.ResponseTimeMs
	} else {
		// Calculate weighted average
		totalTime := (agent.AvgResponseTimeMs * (agent.TotalExecutions - 1)) + response.ResponseTimeMs
		agent.AvgResponseTimeMs = totalTime / agent.TotalExecutions
	}
	
	now := time.Now()
	agent.LastExecutedAt = &now
	agent.UpdatedAt = now

	// Update in Neo4j
	return s.updateAgentInNeo4j(ctx, agent)
}

// getAgentFromBuilder gets a single agent from the agent-builder service
func (s *AgentService) getAgentFromBuilder(ctx context.Context, agentID string, authToken string) (*models.Agent, error) {
	endpoint := "/agents/" + agentID
	requestURL := s.agentBuilderURL + endpoint
	
	httpReq, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, errors.ExternalService("Failed to create agent-builder request", err)
	}
	
	httpReq.Header.Set("Authorization", "Bearer "+authToken)
	httpReq.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, errors.ExternalService("Agent-builder service unavailable", err)
	}
	defer resp.Body.Close()
	
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.ExternalService("Failed to read agent-builder response", err)
	}
	
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.NotFound("Agent not found")
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, errors.ExternalService("Agent-builder service error", fmt.Errorf("status: %d, body: %s", resp.StatusCode, string(bodyBytes)))
	}
	
	var builderResp map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &builderResp); err != nil {
		return nil, errors.ExternalService("Failed to parse agent-builder response", err)
	}
	
	// Convert agent-builder response to our Agent model
	agent := &models.Agent{}
	if id, ok := builderResp["id"].(string); ok {
		agent.ID = id
		agent.AgentBuilderID = id // Same ID is used for agent-builder reference
	}
	if name, ok := builderResp["name"].(string); ok {
		agent.Name = name
	}
	if description, ok := builderResp["description"].(string); ok {
		agent.Description = description
	}
	if ownerID, ok := builderResp["owner_id"].(string); ok {
		agent.OwnerID = ownerID
	}
	if spaceID, ok := builderResp["space_id"].(string); ok {
		agent.SpaceID = spaceID
	}
	if isPublic, ok := builderResp["is_public"].(bool); ok {
		agent.IsPublic = isPublic
	}
	
	return agent, nil
}

// canUserAccessAgent checks if a user can access a specific agent based on ownership and permissions
func (s *AgentService) canUserAccessAgent(agent *models.Agent, userID string, userTeams []string) bool {
	// Allow access if:
	// 1. User owns the agent
	if agent.OwnerID == userID {
		return true
	}
	
	// 2. Agent is public
	if agent.IsPublic {
		return true
	}
	
	// For now, allow access if the user is in the same space
	// TODO: Implement proper team-based access control
	return true
}
