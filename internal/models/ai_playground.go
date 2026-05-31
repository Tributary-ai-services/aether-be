package models

import (
	"time"
)

// ======================================================================
// LLM Completion Models
// ======================================================================

// LLMCompletionRequest represents a request to run an LLM completion
type LLMCompletionRequest struct {
	Provider     string                 `json:"provider" validate:"required"`
	Model        string                 `json:"model" validate:"required"`
	SystemPrompt string                 `json:"system_prompt,omitempty"`
	UserPrompt   string                 `json:"user_prompt" validate:"required"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	Stream       bool                   `json:"stream"`
}

// LLMCompletionResponse represents the response from an LLM completion
type LLMCompletionResponse struct {
	ID           string      `json:"id"`
	Provider     string      `json:"provider"`
	Model        string      `json:"model"`
	Content      string      `json:"content"`
	FinishReason string      `json:"finish_reason,omitempty"`
	Usage        *LLMUsage   `json:"usage,omitempty"`
	Metrics      *LLMMetrics `json:"metrics,omitempty"`
	Error        string      `json:"error,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

// LLMUsage represents token usage statistics
type LLMUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// LLMMetrics represents performance metrics for an LLM call
type LLMMetrics struct {
	LatencyMs     int64   `json:"latency_ms"`
	EstimatedCost float64 `json:"estimated_cost,omitempty"`
}

// LLMCompareRequest represents a request to compare multiple LLM completions
type LLMCompareRequest struct {
	SystemPrompt string        `json:"system_prompt,omitempty"`
	UserPrompt   string        `json:"user_prompt" validate:"required"`
	Models       []ModelConfig `json:"models" validate:"required,min=1,max=4"`
	Stream       bool          `json:"stream"`
}

// ModelConfig represents configuration for a single model in comparison
type ModelConfig struct {
	Provider   string                 `json:"provider" validate:"required"`
	Model      string                 `json:"model" validate:"required"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// LLMCompareResponse represents the response from comparing multiple LLMs
type LLMCompareResponse struct {
	Results   map[string]*LLMCompletionResponse `json:"results"` // keyed by "provider:model"
	CreatedAt time.Time                         `json:"created_at"`
}

// ProviderInfo represents information about an LLM provider
type ProviderInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Models      []string `json:"models,omitempty"`
	Enabled     bool     `json:"enabled"`
}

// ModelInfo represents information about a specific model
type ModelInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	Description     string   `json:"description,omitempty"`
	MaxTokens       int      `json:"max_tokens,omitempty"`
	InputCostPer1K  float64  `json:"input_cost_per_1k,omitempty"`
	OutputCostPer1K float64  `json:"output_cost_per_1k,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Enabled         bool     `json:"enabled"`
}

// ======================================================================
// Agent Testing Models
// ======================================================================

// AgentTestRequest represents a request to test an agent
type AgentTestRequest struct {
	AgentID          string   `json:"agent_id" validate:"required,uuid"`
	Input            string   `json:"input" validate:"required"`
	ContextDocuments []string `json:"context_documents,omitempty"` // Document IDs
	ContextNotebooks []string `json:"context_notebooks,omitempty"` // Notebook IDs
	Stream           bool     `json:"stream"`
}

// AgentTestResponse represents the response from testing an agent
type AgentTestResponse struct {
	AgentID        string          `json:"agent_id"`
	AgentName      string          `json:"agent_name"`
	ExecutionTrace []ExecutionStep `json:"execution_trace"`
	FinalResponse  string          `json:"final_response"`
	Metrics        *AgentMetrics   `json:"metrics"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// ExecutionStep represents a single step in an agent's execution
type ExecutionStep struct {
	StepNumber int       `json:"step_number"`
	Type       string    `json:"type"` // "reasoning", "tool_call", "tool_result", "response"
	Content    string    `json:"content"`
	ToolCall   *ToolCall `json:"tool_call,omitempty"`
	TimingMs   int64     `json:"timing_ms"`
	Timestamp  time.Time `json:"timestamp"`
}

// ToolCall represents a tool invocation by an agent
type ToolCall struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Input      map[string]interface{} `json:"input"`
	Output     interface{}            `json:"output,omitempty"`
	Error      string                 `json:"error,omitempty"`
	DurationMs int64                  `json:"duration_ms"`
}

// AgentMetrics represents performance metrics for an agent test
type AgentMetrics struct {
	TotalDurationMs  int64   `json:"total_duration_ms"`
	TotalTokens      int     `json:"total_tokens"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
	ToolCallCount    int     `json:"tool_call_count"`
	StepCount        int     `json:"step_count"`
}

// AgentCompareRequest represents a request to compare multiple agents
type AgentCompareRequest struct {
	AgentIDs         []string `json:"agent_ids" validate:"required,min=2,max=4"`
	Input            string   `json:"input" validate:"required"`
	ContextDocuments []string `json:"context_documents,omitempty"`
	ContextNotebooks []string `json:"context_notebooks,omitempty"`
	Stream           bool     `json:"stream"`
}

// AgentCompareResponse represents the response from comparing multiple agents
type AgentCompareResponse struct {
	Results   map[string]*AgentTestResponse `json:"results"` // keyed by agent ID
	CreatedAt time.Time                     `json:"created_at"`
}

// TestableAgent represents an agent available for testing
type TestableAgent struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	Type         string    `json:"type"` // "system", "user", "shared"
	OwnerID      string    `json:"owner_id,omitempty"`
	Model        string    `json:"model"`
	Tools        []string  `json:"tools,omitempty"`
	SystemPrompt string    `json:"system_prompt,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// ======================================================================
// Workflow Testing Models
// ======================================================================

// WorkflowTestRequest represents a request to test a workflow
type WorkflowTestRequest struct {
	WorkflowID  string                 `json:"workflow_id" validate:"required,uuid"`
	InputParams map[string]interface{} `json:"input_params"`
	StepByStep  bool                   `json:"step_by_step"`
	Stream      bool                   `json:"stream"`
}

// WorkflowTestResponse represents the response from testing a workflow
type WorkflowTestResponse struct {
	WorkflowID     string                  `json:"workflow_id"`
	WorkflowName   string                  `json:"workflow_name"`
	Status         string                  `json:"status"` // "running", "completed", "failed", "paused"
	CurrentStep    int                     `json:"current_step,omitempty"`
	TotalSteps     int                     `json:"total_steps"`
	ExecutionSteps []WorkflowExecutionStep `json:"execution_steps"`
	FinalOutput    interface{}             `json:"final_output,omitempty"`
	Metrics        *WorkflowMetrics        `json:"metrics,omitempty"`
	Error          string                  `json:"error,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	CompletedAt    *time.Time              `json:"completed_at,omitempty"`
}

// WorkflowExecutionStep represents a single step in workflow execution
type WorkflowExecutionStep struct {
	StepNumber  int         `json:"step_number"`
	StepName    string      `json:"step_name"`
	StepType    string      `json:"step_type"` // "transform", "llm", "condition", "loop", etc.
	Status      string      `json:"status"`    // "pending", "running", "completed", "failed", "skipped"
	Input       interface{} `json:"input,omitempty"`
	Output      interface{} `json:"output,omitempty"`
	Error       string      `json:"error,omitempty"`
	TimingMs    int64       `json:"timing_ms,omitempty"`
	StartedAt   *time.Time  `json:"started_at,omitempty"`
	CompletedAt *time.Time  `json:"completed_at,omitempty"`
}

// WorkflowMetrics represents performance metrics for a workflow execution
type WorkflowMetrics struct {
	TotalDurationMs int64   `json:"total_duration_ms"`
	StepsCompleted  int     `json:"steps_completed"`
	StepsFailed     int     `json:"steps_failed"`
	StepsSkipped    int     `json:"steps_skipped"`
	TotalTokens     int     `json:"total_tokens,omitempty"`
	EstimatedCost   float64 `json:"estimated_cost,omitempty"`
}

// WorkflowStepRequest represents a request to execute a single workflow step
type WorkflowStepRequest struct {
	WorkflowID  string                 `json:"workflow_id" validate:"required,uuid"`
	StepNumber  int                    `json:"step_number" validate:"required,min=1"`
	InputParams map[string]interface{} `json:"input_params,omitempty"`
}

// TestableWorkflow represents a workflow available for testing
type TestableWorkflow struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description,omitempty"`
	Type        string                   `json:"type"` // "system", "user", "shared"
	OwnerID     string                   `json:"owner_id,omitempty"`
	StepCount   int                      `json:"step_count"`
	Steps       []WorkflowStepDefinition `json:"steps,omitempty"`
	InputSchema map[string]interface{}   `json:"input_schema,omitempty"`
	CreatedAt   time.Time                `json:"created_at"`
}

// WorkflowStepDefinition represents the definition of a workflow step
type WorkflowStepDefinition struct {
	StepNumber  int    `json:"step_number"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}
