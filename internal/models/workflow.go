package models

import (
	"time"

	"github.com/google/uuid"
)

// Workflow represents an automated workflow in the system
type Workflow struct {
	ID              string                 `json:"id" neo4j:"id"`
	Name            string                 `json:"name" neo4j:"name"`
	Description     string                 `json:"description" neo4j:"description"`
	Status          string                 `json:"status" neo4j:"status"` // active, paused, disabled
	Type            string                 `json:"type" neo4j:"type"`     // document_processing, compliance_check, approval_chain
	Version         string                 `json:"version" neo4j:"version"`
	Configuration   map[string]interface{} `json:"configuration" neo4j:"configuration"`
	Steps           []WorkflowStep         `json:"steps" neo4j:"-"` // Stored as separate nodes
	Triggers        []WorkflowTrigger      `json:"triggers" neo4j:"-"` // Stored as separate nodes
	ExecutionCount  int64                  `json:"execution_count" neo4j:"execution_count"`
	LastExecuted    *time.Time             `json:"last_executed,omitempty" neo4j:"last_executed"`
	AverageRuntime  float64                `json:"average_runtime" neo4j:"average_runtime"` // milliseconds
	SuccessRate     float64                `json:"success_rate" neo4j:"success_rate"`       // percentage
	CreatedAt       time.Time              `json:"created_at" neo4j:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at" neo4j:"updated_at"`
	CreatedBy       string                 `json:"created_by" neo4j:"created_by"`
	TenantID        string                 `json:"tenant_id" neo4j:"tenant_id"`
	OrganizationID  string                 `json:"organization_id" neo4j:"organization_id"`
}

// WorkflowStep represents a single step in a workflow
type WorkflowStep struct {
	ID            string                 `json:"id" neo4j:"id"`
	WorkflowID    string                 `json:"workflow_id" neo4j:"workflow_id"`
	Name          string                 `json:"name" neo4j:"name"`
	Type          string                 `json:"type" neo4j:"type"` // container, script, http, aiTask, condition, suspend, transform, merge, loop, subworkflow, errorHandler + legacy types
	Order         int                    `json:"order" neo4j:"order"` // Legacy — deprecated, use Dependencies
	Configuration map[string]interface{} `json:"configuration" neo4j:"configuration"`
	Conditions    map[string]interface{} `json:"conditions" neo4j:"conditions"` // Legacy conditional execution rules
	Timeout       int                    `json:"timeout" neo4j:"timeout"`       // seconds
	RetryCount    int                    `json:"retry_count" neo4j:"retry_count"` // Legacy — use RetryStrategy
	OnSuccess     string                 `json:"on_success" neo4j:"on_success"` // Legacy
	OnFailure     string                 `json:"on_failure" neo4j:"on_failure"` // Legacy
	CreatedAt     time.Time              `json:"created_at" neo4j:"created_at"`

	// New Argo-aligned fields
	Dependencies  []string       `json:"dependencies,omitempty" neo4j:"dependencies"`
	TemplateName  string         `json:"template_name,omitempty" neo4j:"template_name"`
	TemplateType  string         `json:"template_type,omitempty" neo4j:"template_type"` // container, script, http, suspend, data, condition
	When          string         `json:"when,omitempty" neo4j:"when"`
	Depends       string         `json:"depends,omitempty" neo4j:"depends"`
	RetryStrategy *RetryStrategy `json:"retry_strategy,omitempty" neo4j:"-"`
	Inputs        *TemplateIO    `json:"inputs,omitempty" neo4j:"-"`
	Outputs       *TemplateIO    `json:"outputs,omitempty" neo4j:"-"`
}

// RetryStrategy defines Argo-compatible retry configuration
type RetryStrategy struct {
	Limit       int    `json:"limit"`
	Duration    string `json:"duration,omitempty"`
	Factor      int    `json:"factor,omitempty"`
	MaxDuration string `json:"max_duration,omitempty"`
	RetryPolicy string `json:"retry_policy,omitempty"` // Always, OnFailure, OnError, OnTransientError
}

// TemplateIO defines inputs/outputs for a workflow step
type TemplateIO struct {
	Parameters []ParameterDef `json:"parameters,omitempty"`
	Artifacts  []ArtifactDef  `json:"artifacts,omitempty"`
}

// ParameterDef defines a single parameter for a step template
type ParameterDef struct {
	Name      string     `json:"name"`
	Value     string     `json:"value,omitempty"`
	Default   string     `json:"default,omitempty"`
	ValueFrom *ValueFrom `json:"value_from,omitempty"`
}

// ValueFrom defines where a parameter value is sourced from
type ValueFrom struct {
	Path       string `json:"path,omitempty"`
	Expression string `json:"expression,omitempty"`
	Parameter  string `json:"parameter,omitempty"`
}

// ArtifactDef defines an artifact (file) input or output
type ArtifactDef struct {
	Name string `json:"name"`
	Path string `json:"path"`
	From string `json:"from,omitempty"`
}

// WorkflowTrigger represents a trigger that can start a workflow
type WorkflowTrigger struct {
	ID            string                 `json:"id" neo4j:"id"`
	WorkflowID    string                 `json:"workflow_id" neo4j:"workflow_id"`
	Type          string                 `json:"type" neo4j:"type"` // upload, schedule, api, webhook, manual
	Name          string                 `json:"name" neo4j:"name"`
	Configuration map[string]interface{} `json:"configuration" neo4j:"configuration"`
	IsActive      bool                   `json:"is_active" neo4j:"is_active"`
	LastTriggered *time.Time             `json:"last_triggered,omitempty" neo4j:"last_triggered"`
	TriggerCount  int64                  `json:"trigger_count" neo4j:"trigger_count"`
	CreatedAt     time.Time              `json:"created_at" neo4j:"created_at"`
}

// WorkflowExecution represents a single execution of a workflow
type WorkflowExecution struct {
	ID              string                 `json:"id" neo4j:"id"`
	WorkflowID      string                 `json:"workflow_id" neo4j:"workflow_id"`
	TriggerID       string                 `json:"trigger_id" neo4j:"trigger_id"`
	Status          string                 `json:"status" neo4j:"status"` // running, completed, failed, cancelled
	Progress        float64                `json:"progress" neo4j:"progress"` // 0-100%
	CurrentStep     string                 `json:"current_step" neo4j:"current_step"`
	StartedAt       time.Time              `json:"started_at" neo4j:"started_at"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty" neo4j:"completed_at"`
	Runtime         float64                `json:"runtime" neo4j:"runtime"` // milliseconds
	Input           map[string]interface{} `json:"input" neo4j:"input"`
	Output          map[string]interface{} `json:"output" neo4j:"output"`
	StepResults     []StepResult           `json:"step_results" neo4j:"-"` // Stored as separate nodes
	ErrorMessage    string                 `json:"error_message,omitempty" neo4j:"error_message"`
	TenantID        string                 `json:"tenant_id" neo4j:"tenant_id"`
	OrganizationID  string                 `json:"organization_id" neo4j:"organization_id"`
}

// StepResult represents the result of executing a workflow step
type StepResult struct {
	ID            string                 `json:"id" neo4j:"id"`
	ExecutionID   string                 `json:"execution_id" neo4j:"execution_id"`
	StepID        string                 `json:"step_id" neo4j:"step_id"`
	Status        string                 `json:"status" neo4j:"status"` // pending, running, completed, failed, skipped
	StartedAt     time.Time              `json:"started_at" neo4j:"started_at"`
	CompletedAt   *time.Time             `json:"completed_at,omitempty" neo4j:"completed_at"`
	Runtime       float64                `json:"runtime" neo4j:"runtime"` // milliseconds
	Input         map[string]interface{} `json:"input" neo4j:"input"`
	Output        map[string]interface{} `json:"output" neo4j:"output"`
	ErrorMessage  string                 `json:"error_message,omitempty" neo4j:"error_message"`
	RetryAttempt  int                    `json:"retry_attempt" neo4j:"retry_attempt"`
}

// WorkflowVersion represents a snapshot of a workflow at a point in time
type WorkflowVersion struct {
	ID            string                 `json:"id" neo4j:"id"`
	WorkflowID    string                 `json:"workflow_id" neo4j:"workflow_id"`
	Version       int                    `json:"version" neo4j:"version"`
	Label         string                 `json:"label" neo4j:"label"` // e.g. "1.0.0", "1.1.0"
	Description   string                 `json:"description" neo4j:"description"`
	Snapshot      map[string]interface{} `json:"snapshot" neo4j:"snapshot"` // Full workflow definition at this version
	CreatedAt     time.Time              `json:"created_at" neo4j:"created_at"`
	CreatedBy     string                 `json:"created_by" neo4j:"created_by"`
}

// Request models for API endpoints

// CreateWorkflowRequest represents the request to create a new workflow
type CreateWorkflowRequest struct {
	Name          string                 `json:"name" binding:"required"`
	Description   string                 `json:"description"`
	Type          string                 `json:"type" binding:"required,oneof=document_processing compliance_check approval_chain custom"`
	Configuration map[string]interface{} `json:"configuration"`
	Steps         []CreateStepRequest    `json:"steps" binding:"required,min=1"`
	Triggers      []CreateTriggerRequest `json:"triggers" binding:"required,min=1"`
}

// CreateStepRequest represents a step in the workflow creation request
type CreateStepRequest struct {
	Name          string                 `json:"name" binding:"required"`
	Type          string                 `json:"type" binding:"required,oneof=process_document compliance_check approval notification ai_analysis assemble_output custom container script http aiTask condition suspend transform assembler sync merge loop subworkflow errorHandler"`
	Order         int                    `json:"order"` // Legacy — optional when Dependencies provided
	Configuration map[string]interface{} `json:"configuration"`
	Conditions    map[string]interface{} `json:"conditions"`
	Timeout       int                    `json:"timeout"` // seconds, default 300
	RetryCount    int                    `json:"retry_count" binding:"min=0,max=10"` // Legacy
	OnSuccess     string                 `json:"on_success"` // Legacy
	OnFailure     string                 `json:"on_failure"` // Legacy

	// New Argo-aligned fields
	Dependencies  []string       `json:"dependencies,omitempty"`
	TemplateName  string         `json:"template_name,omitempty"`
	TemplateType  string         `json:"template_type,omitempty"`
	When          string         `json:"when,omitempty"`
	RetryStrategy *RetryStrategy `json:"retry_strategy,omitempty"`
	Inputs        *TemplateIO    `json:"inputs,omitempty"`
	Outputs       *TemplateIO    `json:"outputs,omitempty"`
}

// CreateTriggerRequest represents a trigger in the workflow creation request
type CreateTriggerRequest struct {
	Type          string                 `json:"type" binding:"required,oneof=upload schedule api webhook manual document_event"`
	Name          string                 `json:"name" binding:"required"`
	Configuration map[string]interface{} `json:"configuration"`
}

// UpdateWorkflowRequest represents the request to update a workflow
type UpdateWorkflowRequest struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Status        string                 `json:"status" binding:"omitempty,oneof=active paused disabled"`
	Configuration map[string]interface{} `json:"configuration"`
}

// ExecuteWorkflowRequest represents the request to manually execute a workflow
type ExecuteWorkflowRequest struct {
	TriggerID string                 `json:"trigger_id"`
	Input     map[string]interface{} `json:"input"`
}

// PublishArtifactRequest represents a request to store a workflow output artifact
type PublishArtifactRequest struct {
	Content     string `json:"content" binding:"required"`
	Format      string `json:"format" binding:"required,oneof=text json markdown docx pdf"`
	Filename    string `json:"filename" binding:"required"`
	NotebookID  string `json:"notebook_id"`
	ExecutionID string `json:"execution_id"`
}

// WorkflowAnalytics represents workflow performance analytics
type WorkflowAnalytics struct {
	ID                    string             `json:"id"`
	Period                string             `json:"period"` // daily, weekly, monthly
	Date                  time.Time          `json:"date"`
	TotalWorkflows        int                `json:"total_workflows"`
	ActiveWorkflows       int                `json:"active_workflows"`
	TotalExecutions       int64              `json:"total_executions"`
	SuccessfulExecutions  int64              `json:"successful_executions"`
	FailedExecutions      int64              `json:"failed_executions"`
	AverageRuntime        float64            `json:"average_runtime"`       // milliseconds
	TotalProcessingTime   float64            `json:"total_processing_time"` // milliseconds
	WorkflowPerformance   map[string]float64 `json:"workflow_performance"`  // workflow_id -> success_rate
	PopularTriggerTypes   map[string]int64   `json:"popular_trigger_types"` // trigger_type -> count
	TenantID              string             `json:"tenant_id"`
	OrganizationID        string             `json:"organization_id"`
	CreatedAt             time.Time          `json:"created_at"`
}

// NewWorkflow creates a new workflow with default values
func NewWorkflow(req CreateWorkflowRequest, userID, tenantID, orgID string) *Workflow {
	now := time.Now()
	return &Workflow{
		ID:              uuid.New().String(),
		Name:            req.Name,
		Description:     req.Description,
		Status:          "paused", // Start paused for safety
		Type:            req.Type,
		Version:         "1.0.0",
		Configuration:   req.Configuration,
		ExecutionCount:  0,
		AverageRuntime:  0.0,
		SuccessRate:     0.0,
		CreatedAt:       now,
		UpdatedAt:       now,
		CreatedBy:       userID,
		TenantID:        tenantID,
		OrganizationID:  orgID,
	}
}

// NewWorkflowStep creates a new workflow step
func NewWorkflowStep(req CreateStepRequest, workflowID string) *WorkflowStep {
	// Set defaults
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 300 // 5 minutes default
	}

	onSuccess := req.OnSuccess
	if onSuccess == "" {
		onSuccess = "next"
	}

	onFailure := req.OnFailure
	if onFailure == "" {
		onFailure = "abort"
	}

	return &WorkflowStep{
		ID:            uuid.New().String(),
		WorkflowID:    workflowID,
		Name:          req.Name,
		Type:          req.Type,
		Order:         req.Order,
		Configuration: req.Configuration,
		Conditions:    req.Conditions,
		Timeout:       timeout,
		RetryCount:    req.RetryCount,
		OnSuccess:     onSuccess,
		OnFailure:     onFailure,
		CreatedAt:     time.Now(),
		// New Argo-aligned fields
		Dependencies:  req.Dependencies,
		TemplateName:  req.TemplateName,
		TemplateType:  req.TemplateType,
		When:          req.When,
		RetryStrategy: req.RetryStrategy,
		Inputs:        req.Inputs,
		Outputs:       req.Outputs,
	}
}

// NewWorkflowTrigger creates a new workflow trigger
func NewWorkflowTrigger(req CreateTriggerRequest, workflowID string) *WorkflowTrigger {
	return &WorkflowTrigger{
		ID:            uuid.New().String(),
		WorkflowID:    workflowID,
		Type:          req.Type,
		Name:          req.Name,
		Configuration: req.Configuration,
		IsActive:      true,
		TriggerCount:  0,
		CreatedAt:     time.Now(),
	}
}

// NewWorkflowExecution creates a new workflow execution
func NewWorkflowExecution(workflowID, triggerID, tenantID, orgID string, input map[string]interface{}) *WorkflowExecution {
	return &WorkflowExecution{
		ID:             uuid.New().String(),
		WorkflowID:     workflowID,
		TriggerID:      triggerID,
		Status:         "running",
		Progress:       0.0,
		StartedAt:      time.Now(),
		Input:          input,
		Output:         make(map[string]interface{}),
		TenantID:       tenantID,
		OrganizationID: orgID,
	}
}