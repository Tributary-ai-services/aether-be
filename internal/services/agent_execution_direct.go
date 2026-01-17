package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Tributary-ai-services/aether-be/pkg/errors"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// executeAgentDirect handles agent execution directly using the router service
// This is a fallback when agent-builder is not available
func (s *AgentService) executeAgentDirect(ctx context.Context, agent *models.Agent, req models.AgentExecuteRequest, userID string, authToken string) (*models.AgentExecuteResponse, error) {
	s.logger.Info("Executing agent directly via router service",
		zap.String("agent_id", agent.ID),
		zap.String("agent_type", string(agent.Type)),
		zap.String("user_id", userID),
	)

	startTime := time.Now()

	// Build LLM request based on agent type
	llmRequest := s.buildLLMRequest(agent, req)

	// Call router service
	llmResponse, err := s.callRouterService(ctx, llmRequest, authToken)
	if err != nil {
		return nil, errors.ExternalService("Failed to call LLM service", err)
	}

	endTime := time.Now()

	// Build response
	response := &models.AgentExecuteResponse{
		ExecutionID:    uuid.New().String(),
		AgentID:        agent.ID,
		AgentType:      agent.Type,
		Output:         llmResponse.Content,
		TokensUsed:     llmResponse.TokensUsed,
		CostUSD:        calculateCost(llmResponse.TokensUsed, llmResponse.Model),
		ResponseTimeMs: int(endTime.Sub(startTime).Milliseconds()),
		StartedAt:      startTime,
		CompletedAt:    endTime,
		Metadata: map[string]interface{}{
			"model":    llmResponse.Model,
			"provider": llmResponse.Provider,
			"direct":   true, // Indicates this was a direct execution
		},
	}

	// Add type-specific response data
	switch agent.Type {
	case models.AgentTypeQA:
		// For Q&A, we could add vector search results here
		response.Sources = s.searchKnowledgeSources(ctx, req.Input, req.Sources)
	case models.AgentTypeConversational:
		// For conversational, preserve the conversation ID
		if req.ConversationID != nil {
			response.ConversationID = req.ConversationID
		} else {
			convID := uuid.New().String()
			response.ConversationID = &convID
		}
	case models.AgentTypeProducer:
		// For producer, create a production result
		response.Production = &models.ProductionResult{
			ID:        uuid.New().String(),
			Title:     fmt.Sprintf("Generated: %s", truncate(req.Input, 50)),
			Format:    *req.OutputFormat,
			Content:   response.Output,
			CreatedAt: time.Now(),
		}
	}

	return response, nil
}

func (s *AgentService) buildLLMRequest(agent *models.Agent, req models.AgentExecuteRequest) map[string]interface{} {
	// Start with system prompt
	systemPrompt := "You are a helpful AI assistant."
	
	// Customize based on agent type
	switch agent.Type {
	case models.AgentTypeQA:
		systemPrompt = "You are a Q&A assistant. Answer questions based on the provided context and sources. Be accurate and cite your sources when possible."
	case models.AgentTypeConversational:
		systemPrompt = "You are a conversational AI assistant. Maintain context throughout the conversation and provide helpful, friendly responses."
	case models.AgentTypeProducer:
		systemPrompt = "You are a content producer. Generate high-quality content based on the given instructions and parameters."
	}

	// Build messages array
	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
	}

	// Add conversation history if available
	if req.History != nil && len(req.History) > 0 {
		for _, msg := range req.History {
			messages = append(messages, map[string]string{
				"role":    msg.Role,
				"content": msg.Content,
			})
		}
	}

	// Add current user input
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": req.Input,
	})

	// Default to OpenAI for now (could be configurable)
	return map[string]interface{}{
		"model":       "gpt-3.5-turbo",
		"messages":    messages,
		"temperature": 0.7,
		"max_tokens":  500,
		"provider":    "openai",
	}
}

type LLMResponse struct {
	Content    string
	TokensUsed int
	Model      string
	Provider   string
}

func (s *AgentService) callRouterService(ctx context.Context, request map[string]interface{}, authToken string) (*LLMResponse, error) {
	// Use the router service URL from environment or default
	routerURL := s.getRouterServiceURL()

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", routerURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	// Pass through user's JWT token to router service
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("router service error: %s", string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Extract response from OpenAI-compatible format
	choices := result["choices"].([]interface{})
	if len(choices) == 0 {
		return nil, fmt.Errorf("no response from LLM")
	}

	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	content := message["content"].(string)

	usage := result["usage"].(map[string]interface{})
	totalTokens := int(usage["total_tokens"].(float64))

	return &LLMResponse{
		Content:    content,
		TokensUsed: totalTokens,
		Model:      request["model"].(string),
		Provider:   request["provider"].(string),
	}, nil
}

func (s *AgentService) searchKnowledgeSources(ctx context.Context, query string, sourceIDs []string) []models.SourceReference {
	sources := make([]models.SourceReference, 0)
	
	// If no specific sources provided, search all linked notebooks for the agent
	searchSources := sourceIDs
	if len(searchSources) == 0 {
		// Get agent's linked notebooks - this would require the agent ID
		// For now, return empty results for no sources
		return sources
	}
	
	// Search through each specified notebook
	for _, notebookID := range searchSources {
		// Get notebook info
		notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, "", nil)
		if err != nil {
			s.logger.Warn("Failed to get notebook for knowledge source search",
				zap.String("notebook_id", notebookID),
				zap.Error(err))
			continue
		}
		
		// Perform vector search on notebook content
		// This would integrate with the embedding service for actual vector search
		// For now, create a placeholder result
		source := models.SourceReference{
			NotebookID:   notebookID,
			NotebookName: notebook.Name,
			ChunkID:      "search-result-" + notebookID,
			Relevance:    0.75, // Placeholder relevance score
			Content:      fmt.Sprintf("Search results from %s related to: %s", notebook.Name, query),
		}
		sources = append(sources, source)
	}
	
	return sources
}

func calculateCost(tokens int, model string) float64 {
	// Enhanced cost calculation based on model
	costPer1000Tokens := 0.002 // Default for GPT-3.5-turbo
	
	switch model {
	case "gpt-4", "gpt-4-32k":
		costPer1000Tokens = 0.03
	case "gpt-4o", "gpt-4o-mini":
		costPer1000Tokens = 0.015
	case "gpt-3.5-turbo", "gpt-3.5-turbo-16k":
		costPer1000Tokens = 0.002
	case "claude-3-sonnet", "claude-3-5-sonnet":
		costPer1000Tokens = 0.003
	case "claude-3-haiku":
		costPer1000Tokens = 0.00025
	case "claude-3-opus":
		costPer1000Tokens = 0.015
	default:
		// Use default pricing for unknown models
		costPer1000Tokens = 0.002
	}
	
	return float64(tokens) / 1000.0 * costPer1000Tokens
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// getRouterServiceURL returns the router service URL from environment or default
func (s *AgentService) getRouterServiceURL() string {
	// Try to get from environment variables
	if url := os.Getenv("ROUTER_SERVICE_URL"); url != "" {
		return url + "/v1/chat/completions"
	}
	
	// Default to localhost for development
	return "http://localhost:8086/v1/chat/completions"
}