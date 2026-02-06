package agents

import (
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/models"
)

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

// Well-known internal agent IDs
const (
	PromptAssistantID        = "00000000-0000-0000-0000-000000000001"
	PostgreSQLAssistantID    = "00000000-0000-0000-0000-000000000002"
	MySQLAssistantID         = "00000000-0000-0000-0000-000000000003"
	MariaDBAssistantID       = "00000000-0000-0000-0000-000000000004"
	SQLServerAssistantID     = "00000000-0000-0000-0000-000000000005"
	SQLiteAssistantID        = "00000000-0000-0000-0000-000000000006"
	DuckDBAssistantID        = "00000000-0000-0000-0000-000000000007"
	Neo4jAssistantID         = "00000000-0000-0000-0000-000000000008"
	NotebookChatAssistantID  = "00000000-0000-0000-0000-000000000009"
	DocumentSummarizerID     = "00000000-0000-0000-0000-000000000010"
	QAGeneratorID            = "00000000-0000-0000-0000-000000000011"
	OutlineCreatorID         = "00000000-0000-0000-0000-000000000012"
	InsightsExtractorID      = "00000000-0000-0000-0000-000000000013"
)

// GetInternalAgents returns the list of all internal system agents.
// DEPRECATED: Internal agents are now stored in agent-builder database.
// This function returns an empty list for backward compatibility.
// The agents are seeded via SQL migrations in tas-agent-builder/database/migrations/016_seed_producer_agents.sql
// To modify agent prompts, update the database directly or via migrations - no code deployment required.
func GetInternalAgents() []InternalAgent {
	// Return empty list - internal agents now come from agent-builder
	return []InternalAgent{}
}

// GetInternalProducerAgents returns only internal agents of type "producer"
func GetInternalProducerAgents() []InternalAgent {
	allAgents := GetInternalAgents()
	producers := make([]InternalAgent, 0)
	for _, agent := range allAgents {
		if agent.Type == "producer" {
			producers = append(producers, agent)
		}
	}
	return producers
}

// GetInternalConversationalAgents returns only internal agents of type "conversational"
func GetInternalConversationalAgents() []InternalAgent {
	allAgents := GetInternalAgents()
	conversational := make([]InternalAgent, 0)
	for _, agent := range allAgents {
		if agent.Type == "conversational" {
			conversational = append(conversational, agent)
		}
	}
	return conversational
}

// GetInternalAgentByID returns an internal agent by ID, or nil if not found
func GetInternalAgentByID(id string) *InternalAgent {
	for _, agent := range GetInternalAgents() {
		if agent.ID == id {
			return &agent
		}
	}
	return nil
}

// IsInternalAgentID checks if the given ID is an internal agent ID
func IsInternalAgentID(id string) bool {
	return GetInternalAgentByID(id) != nil
}

// ToAgentResponse converts an InternalAgent to a models.AgentResponse
func (a *InternalAgent) ToAgentResponse() *models.AgentResponse {
	createdAt, _ := time.Parse(time.RFC3339, a.CreatedAt)

	return &models.AgentResponse{
		ID:             a.ID,
		AgentBuilderID: a.ID, // Internal agents use same ID
		Name:           a.Name,
		Description:    a.Description,
		Status:         models.AgentStatusPublished, // Internal agents are always published
		Type:           models.AgentType(a.Type),
		OwnerID:        "system",
		SpaceType:      models.SpaceTypePersonal, // Internal agents are available in all spaces
		SpaceID:        "",                       // Internal agents are not space-scoped
		IsPublic:       true,                     // Internal agents are always public
		IsTemplate:     false,
		Tags:           []string{"internal", "system"},
		SystemPrompt:   a.SystemPrompt,
		LLMConfig:      a.LLMConfig,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
}

// GetInternalProducerAgentResponses returns internal producer agents as AgentResponse objects
func GetInternalProducerAgentResponses() []*models.AgentResponse {
	producers := GetInternalProducerAgents()
	responses := make([]*models.AgentResponse, 0, len(producers))
	for _, agent := range producers {
		responses = append(responses, agent.ToAgentResponse())
	}
	return responses
}
