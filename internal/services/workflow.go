package services

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// WorkflowService handles workflow operations
type WorkflowService struct {
	neo4j         *database.Neo4jClient
	kafkaService  *KafkaService
	argoGenerator *ArgoGenerator
	logger        *logger.Logger
}

// NewWorkflowService creates a new workflow service
func NewWorkflowService(neo4j *database.Neo4jClient, kafkaService *KafkaService, argoGenerator *ArgoGenerator, log *logger.Logger) *WorkflowService {
	return &WorkflowService{
		neo4j:         neo4j,
		kafkaService:  kafkaService,
		argoGenerator: argoGenerator,
		logger:        log.WithService("workflow_service"),
	}
}

// CreateWorkflow creates a new workflow with steps and triggers
func (s *WorkflowService) CreateWorkflow(ctx context.Context, req models.CreateWorkflowRequest, userID string, spaceContext *models.SpaceContext) (*models.Workflow, error) {
	workflow := models.NewWorkflow(req, userID, spaceContext.TenantID, spaceContext.SpaceID)

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Create workflow, steps, and triggers in a single transaction
	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		// Create workflow node
		workflowQuery := `
			CREATE (w:Workflow {
				id: $id,
				name: $name,
				description: $description,
				status: $status,
				type: $type,
				version: $version,
				configuration: $configuration,
				execution_count: $execution_count,
				average_runtime: $average_runtime,
				success_rate: $success_rate,
				created_at: $created_at,
				updated_at: $updated_at,
				created_by: $created_by,
				tenant_id: $tenant_id,
				organization_id: $organization_id
			})
			RETURN w
		`

		workflowParams := map[string]interface{}{
			"id":               workflow.ID,
			"name":             workflow.Name,
			"description":      workflow.Description,
			"status":           workflow.Status,
			"type":             workflow.Type,
			"version":          workflow.Version,
			"configuration":    serializeParameters(workflow.Configuration),
			"execution_count":  workflow.ExecutionCount,
			"average_runtime":  workflow.AverageRuntime,
			"success_rate":     workflow.SuccessRate,
			"created_at":       workflow.CreatedAt,
			"updated_at":       workflow.UpdatedAt,
			"created_by":       workflow.CreatedBy,
			"tenant_id":        workflow.TenantID,
			"organization_id":  workflow.OrganizationID,
		}

		_, err := tx.Run(ctx, workflowQuery, workflowParams)
		if err != nil {
			return nil, fmt.Errorf("failed to create workflow: %w", err)
		}

		// Create workflow steps
		for _, stepReq := range req.Steps {
			step := models.NewWorkflowStep(stepReq, workflow.ID)
			stepQuery := `
				CREATE (s:WorkflowStep {
					id: $id,
					workflow_id: $workflow_id,
					name: $name,
					type: $type,
					order: $order,
					configuration: $configuration,
					conditions: $conditions,
					timeout: $timeout,
					retry_count: $retry_count,
					on_success: $on_success,
					on_failure: $on_failure,
					created_at: $created_at,
					dependencies: $dependencies,
					template_name: $template_name,
					template_type: $template_type,
					when_condition: $when_condition
				})
				WITH s
				MATCH (w:Workflow {id: $workflow_id})
				CREATE (w)-[:HAS_STEP]->(s)
				RETURN s
			`

			// Ensure dependencies is a non-nil slice for Neo4j
			deps := step.Dependencies
			if deps == nil {
				deps = []string{}
			}

			stepParams := map[string]interface{}{
				"id":             step.ID,
				"workflow_id":    step.WorkflowID,
				"name":           step.Name,
				"type":           step.Type,
				"order":          step.Order,
				"configuration":  serializeParameters(step.Configuration),
				"conditions":     serializeParameters(step.Conditions),
				"timeout":        step.Timeout,
				"retry_count":    step.RetryCount,
				"on_success":     step.OnSuccess,
				"on_failure":     step.OnFailure,
				"created_at":     step.CreatedAt,
				"dependencies":   deps,
				"template_name":  step.TemplateName,
				"template_type":  step.TemplateType,
				"when_condition": step.When,
			}

			_, err := tx.Run(ctx, stepQuery, stepParams)
			if err != nil {
				return nil, fmt.Errorf("failed to create workflow step: %w", err)
			}

			workflow.Steps = append(workflow.Steps, *step)
		}

		// Create workflow triggers
		for _, triggerReq := range req.Triggers {
			trigger := models.NewWorkflowTrigger(triggerReq, workflow.ID)
			triggerQuery := `
				CREATE (t:WorkflowTrigger {
					id: $id,
					workflow_id: $workflow_id,
					type: $type,
					name: $name,
					configuration: $configuration,
					is_active: $is_active,
					trigger_count: $trigger_count,
					created_at: $created_at
				})
				WITH t
				MATCH (w:Workflow {id: $workflow_id})
				CREATE (w)-[:HAS_TRIGGER]->(t)
				RETURN t
			`

			triggerParams := map[string]interface{}{
				"id":            trigger.ID,
				"workflow_id":   trigger.WorkflowID,
				"type":          trigger.Type,
				"name":          trigger.Name,
				"configuration": serializeParameters(trigger.Configuration),
				"is_active":     trigger.IsActive,
				"trigger_count": trigger.TriggerCount,
				"created_at":    trigger.CreatedAt,
			}

			_, err := tx.Run(ctx, triggerQuery, triggerParams)
			if err != nil {
				return nil, fmt.Errorf("failed to create workflow trigger: %w", err)
			}

			workflow.Triggers = append(workflow.Triggers, *trigger)
		}

		return workflow, nil
	})

	if err != nil {
		s.logger.Error("Failed to create workflow", zap.Error(err))
		return nil, err
	}

	s.logger.Info("Created workflow", zap.String("workflow_id", workflow.ID), zap.String("name", workflow.Name))
	return result.(*models.Workflow), nil
}

// GetWorkflows retrieves workflows for a tenant
func (s *WorkflowService) GetWorkflows(ctx context.Context, spaceContext *models.SpaceContext, limit, offset int) ([]*models.Workflow, int, error) {
	query := `
		MATCH (w:Workflow)
		WHERE w.tenant_id = $tenant_id
		OPTIONAL MATCH (w)-[:HAS_STEP]->(s:WorkflowStep)
		OPTIONAL MATCH (w)-[:HAS_TRIGGER]->(t:WorkflowTrigger)
		RETURN w, collect(DISTINCT s) as steps, collect(DISTINCT t) as triggers
		ORDER BY w.created_at DESC
		SKIP $offset
		LIMIT $limit
	`

	countQuery := `
		MATCH (w:Workflow)
		WHERE w.tenant_id = $tenant_id
		RETURN count(w) as total
	`

	parameters := map[string]interface{}{
		"tenant_id": spaceContext.TenantID,
		"limit":     limit,
		"offset":    offset,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Get workflows with steps and triggers
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get workflows", zap.Error(err))
		return nil, 0, fmt.Errorf("failed to get workflows: %w", err)
	}

	records := result.([]*neo4j.Record)
	workflows := make([]*models.Workflow, 0, len(records))

	for _, record := range records {
		workflow, err := s.recordToWorkflow(record)
		if err != nil {
			s.logger.Error("Failed to parse workflow record", zap.Error(err))
			continue
		}
		workflows = append(workflows, workflow)
	}

	// Get total count
	countResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, countQuery, map[string]interface{}{"tenant_id": spaceContext.TenantID})
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to count workflows", zap.Error(err))
		return workflows, 0, nil
	}

	var total int
	if countRecords := countResult.([]*neo4j.Record); len(countRecords) > 0 {
		if totalValue, found := countRecords[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return workflows, total, nil
}

// GetWorkflowByID retrieves a specific workflow by ID
func (s *WorkflowService) GetWorkflowByID(ctx context.Context, workflowID string, spaceContext *models.SpaceContext) (*models.Workflow, error) {
	query := `
		MATCH (w:Workflow)
		WHERE w.id = $workflow_id AND w.tenant_id = $tenant_id
		OPTIONAL MATCH (w)-[:HAS_STEP]->(s:WorkflowStep)
		OPTIONAL MATCH (w)-[:HAS_TRIGGER]->(t:WorkflowTrigger)
		RETURN w, collect(DISTINCT s) as steps, collect(DISTINCT t) as triggers
	`

	parameters := map[string]interface{}{
		"workflow_id": workflowID,
		"tenant_id":   spaceContext.TenantID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get workflow", zap.String("workflow_id", workflowID), zap.Error(err))
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, fmt.Errorf("workflow not found")
	}

	return s.recordToWorkflow(records[0])
}

// UpdateWorkflow updates an existing workflow
func (s *WorkflowService) UpdateWorkflow(ctx context.Context, workflowID string, req models.UpdateWorkflowRequest, spaceContext *models.SpaceContext) (*models.Workflow, error) {
	// Build dynamic update query
	setParts := []string{"w.updated_at = $updated_at"}
	parameters := map[string]interface{}{
		"workflow_id": workflowID,
		"tenant_id":   spaceContext.TenantID,
		"updated_at":  time.Now(),
	}

	if req.Name != "" {
		setParts = append(setParts, "w.name = $name")
		parameters["name"] = req.Name
	}
	if req.Description != "" {
		setParts = append(setParts, "w.description = $description")
		parameters["description"] = req.Description
	}
	if req.Status != "" {
		setParts = append(setParts, "w.status = $status")
		parameters["status"] = req.Status
	}
	if req.Configuration != nil {
		setParts = append(setParts, "w.configuration = $configuration")
		parameters["configuration"] = serializeParameters(req.Configuration)
	}

	query := fmt.Sprintf(`
		MATCH (w:Workflow)
		WHERE w.id = $workflow_id AND w.tenant_id = $tenant_id
		SET %s
		RETURN w
	`, strings.Join(setParts, ", "))

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to update workflow", zap.String("workflow_id", workflowID), zap.Error(err))
		return nil, fmt.Errorf("failed to update workflow: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, fmt.Errorf("workflow not found")
	}

	s.logger.Info("Updated workflow", zap.String("workflow_id", workflowID))
	
	// Get the full workflow with steps and triggers
	return s.GetWorkflowByID(ctx, workflowID, spaceContext)
}

// DeleteWorkflow deletes a workflow and all its related data
func (s *WorkflowService) DeleteWorkflow(ctx context.Context, workflowID string, spaceContext *models.SpaceContext) error {
	query := `
		MATCH (w:Workflow {id: $workflow_id, tenant_id: $tenant_id})
		OPTIONAL MATCH (w)-[:HAS_STEP]->(s:WorkflowStep)
		OPTIONAL MATCH (w)-[:HAS_TRIGGER]->(t:WorkflowTrigger)
		OPTIONAL MATCH (w)-[:HAS_EXECUTION]->(e:WorkflowExecution)
		DETACH DELETE w, s, t, e
		RETURN count(w) as deleted
	`

	parameters := map[string]interface{}{
		"workflow_id": workflowID,
		"tenant_id":   spaceContext.TenantID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to delete workflow", zap.String("workflow_id", workflowID), zap.Error(err))
		return fmt.Errorf("failed to delete workflow: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 || records[0].Values[0].(int64) == 0 {
		return fmt.Errorf("workflow not found")
	}

	s.logger.Info("Deleted workflow", zap.String("workflow_id", workflowID))
	return nil
}

// ExecuteWorkflow starts a new execution of a workflow
func (s *WorkflowService) ExecuteWorkflow(ctx context.Context, workflowID string, req models.ExecuteWorkflowRequest, spaceContext *models.SpaceContext) (*models.WorkflowExecution, error) {
	// Verify the workflow exists
	workflow, err := s.GetWorkflowByID(ctx, workflowID, spaceContext)
	if err != nil {
		return nil, err
	}

	// Auto-activate workflow on first manual execution
	if workflow.Status != "active" {
		s.logger.Info("Auto-activating workflow for execution",
			zap.String("workflow_id", workflowID),
			zap.String("previous_status", workflow.Status))
		updateReq := models.UpdateWorkflowRequest{Status: "active"}
		if _, err := s.UpdateWorkflow(ctx, workflowID, updateReq, spaceContext); err != nil {
			s.logger.Warn("Failed to auto-activate workflow", zap.Error(err))
		}
	}

	// Create new execution
	execution := models.NewWorkflowExecution(workflowID, req.TriggerID, spaceContext.TenantID, spaceContext.SpaceID, req.Input)

	// Build parameter map: start with trigger defaults, then overlay user-provided values
	paramMap := map[string]any{}

	// Extract defaults from trigger input_parameters
	for _, trigger := range workflow.Triggers {
		if trigger.Configuration == nil {
			continue
		}
		if ipList, ok := trigger.Configuration["input_parameters"]; ok {
			if ipSlice, ok := ipList.([]interface{}); ok {
				for _, ipRaw := range ipSlice {
					if ip, ok := ipRaw.(map[string]interface{}); ok {
						name, _ := ip["name"].(string)
						defVal, hasDef := ip["default"]
						if name != "" && hasDef {
							paramMap[name] = defVal
						}
					}
				}
			}
		}
	}

	// Overlay user-provided parameters (these take precedence over defaults)
	if req.Input != nil {
		if params, ok := req.Input["parameters"]; ok {
			if userParams, ok := params.(map[string]any); ok {
				for k, v := range userParams {
					paramMap[k] = v
				}
			}
		}
	}

	// Substitute parameters into workflow step configurations before submission
	if len(paramMap) > 0 {
		for i := range workflow.Steps {
			if workflow.Steps[i].Configuration != nil {
				for key, val := range workflow.Steps[i].Configuration {
					if strVal, ok := val.(string); ok {
						for pk, pv := range paramMap {
							strVal = strings.ReplaceAll(strVal, "{{"+pk+"}}", fmt.Sprintf("%v", pv))
						}
						workflow.Steps[i].Configuration[key] = strVal
					}
				}
			}
		}
	}

	// Submit workflow to Argo for real execution
	if s.argoGenerator != nil && s.argoGenerator.IsEnabled() {
		argoName, err := s.argoGenerator.GenerateAndSubmit(ctx, workflow)
		if err != nil {
			s.logger.Error("Argo workflow submission failed",
				zap.String("workflow_id", workflowID),
				zap.Error(err))
			execution.Status = "failed"
			execution.ErrorMessage = fmt.Sprintf("Argo submission failed: %v", err)
		} else {
			execution.Status = "submitted"
			execution.CurrentStep = "argo-pending"
			execution.Output = map[string]any{
				"argo_workflow_name": argoName,
				"argo_namespace":    s.argoGenerator.namespace,
				"message":           fmt.Sprintf("Workflow submitted to Argo as '%s'", argoName),
			}
			s.logger.Info("Workflow submitted to Argo",
				zap.String("workflow_id", workflowID),
				zap.String("argo_name", argoName))
		}
	} else {
		execution.Status = "failed"
		execution.ErrorMessage = "Argo Workflows integration is not enabled"
	}

	now := time.Now()
	elapsed := time.Since(execution.StartedAt)
	execution.Runtime = float64(elapsed.Milliseconds())
	if execution.Status == "failed" {
		execution.CompletedAt = &now
	}

	query := `
		CREATE (e:WorkflowExecution {
			id: $id,
			workflow_id: $workflow_id,
			trigger_id: $trigger_id,
			status: $status,
			progress: $progress,
			current_step: $current_step,
			started_at: $started_at,
			completed_at: $completed_at,
			runtime: $runtime,
			input: $input,
			output: $output,
			error_message: $error_message,
			tenant_id: $tenant_id,
			organization_id: $organization_id
		})
		WITH e
		MATCH (w:Workflow {id: $workflow_id})
		CREATE (w)-[:HAS_EXECUTION]->(e)
		SET w.execution_count = w.execution_count + 1,
		    w.last_executed = $started_at
		RETURN e
	`

	// For Neo4j driver compatibility: use now if completed, otherwise use started_at as placeholder
	var completedAtValue interface{}
	if execution.CompletedAt != nil {
		completedAtValue = *execution.CompletedAt
	} else {
		completedAtValue = nil
	}

	parameters := map[string]any{
		"id":              execution.ID,
		"workflow_id":     execution.WorkflowID,
		"trigger_id":      execution.TriggerID,
		"status":          execution.Status,
		"progress":        execution.Progress,
		"current_step":    execution.CurrentStep,
		"started_at":      execution.StartedAt,
		"completed_at":    completedAtValue,
		"runtime":         execution.Runtime,
		"input":           serializeParameters(execution.Input),
		"output":          serializeParameters(execution.Output),
		"error_message":   execution.ErrorMessage,
		"tenant_id":       execution.TenantID,
		"organization_id": execution.OrganizationID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to create workflow execution", zap.Error(err))
		return nil, fmt.Errorf("failed to create workflow execution: %w", err)
	}

	s.logger.Info("Workflow execution recorded",
		zap.String("execution_id", execution.ID),
		zap.String("workflow_id", workflowID),
		zap.String("status", execution.Status),
		zap.Int("steps", len(workflow.Steps)))

	return execution, nil
}

// GetWorkflowExecutions retrieves executions for a workflow
func (s *WorkflowService) GetWorkflowExecutions(ctx context.Context, workflowID string, spaceContext *models.SpaceContext, limit, offset int) ([]*models.WorkflowExecution, int, error) {
	query := `
		MATCH (e:WorkflowExecution)
		WHERE e.workflow_id = $workflow_id AND e.tenant_id = $tenant_id
		RETURN e
		ORDER BY e.started_at DESC
		SKIP $offset
		LIMIT $limit
	`

	countQuery := `
		MATCH (e:WorkflowExecution)
		WHERE e.workflow_id = $workflow_id AND e.tenant_id = $tenant_id
		RETURN count(e) as total
	`

	parameters := map[string]interface{}{
		"workflow_id": workflowID,
		"tenant_id":   spaceContext.TenantID,
		"limit":       limit,
		"offset":      offset,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Get executions
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get workflow executions", zap.Error(err))
		return nil, 0, fmt.Errorf("failed to get workflow executions: %w", err)
	}

	records := result.([]*neo4j.Record)
	executions := make([]*models.WorkflowExecution, 0, len(records))

	for _, record := range records {
		execution, err := s.recordToWorkflowExecution(record, "e")
		if err != nil {
			s.logger.Error("Failed to parse workflow execution record", zap.Error(err))
			continue
		}
		executions = append(executions, execution)
	}

	// Get total count
	countResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, countQuery, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to count workflow executions", zap.Error(err))
		return executions, 0, nil
	}

	var total int
	if countRecords := countResult.([]*neo4j.Record); len(countRecords) > 0 {
		if totalValue, found := countRecords[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return executions, total, nil
}

// GetWorkflowAnalytics retrieves workflow performance analytics
func (s *WorkflowService) GetWorkflowAnalytics(ctx context.Context, spaceContext *models.SpaceContext, period string) (*models.WorkflowAnalytics, error) {
	// Get workflow statistics
	workflowQuery := `
		MATCH (w:Workflow)
		WHERE w.tenant_id = $tenant_id
		RETURN count(w) as total_workflows, 
		       count(CASE WHEN w.status = 'active' THEN 1 END) as active_workflows,
		       avg(w.success_rate) as avg_success_rate,
		       avg(w.average_runtime) as avg_runtime
	`

	executionQuery := `
		MATCH (e:WorkflowExecution)
		WHERE e.tenant_id = $tenant_id
		RETURN count(e) as total_executions,
		       count(CASE WHEN e.status = 'completed' THEN 1 END) as successful_executions,
		       count(CASE WHEN e.status = 'failed' THEN 1 END) as failed_executions,
		       sum(e.runtime) as total_runtime
	`

	parameters := map[string]interface{}{
		"tenant_id": spaceContext.TenantID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Get workflow analytics
	workflowResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, workflowQuery, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get workflow analytics", zap.Error(err))
		return nil, fmt.Errorf("failed to get workflow analytics: %w", err)
	}

	// Get execution analytics
	executionResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, executionQuery, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get execution analytics", zap.Error(err))
		return nil, fmt.Errorf("failed to get execution analytics: %w", err)
	}

	// Build analytics response
	analytics := &models.WorkflowAnalytics{
		ID:                    fmt.Sprintf("workflow_analytics_%s_%d", period, time.Now().Unix()),
		Period:                period,
		Date:                  time.Now(),
		TenantID:              spaceContext.TenantID,
		OrganizationID:        spaceContext.SpaceID,
		CreatedAt:             time.Now(),
		WorkflowPerformance:   make(map[string]float64),
		PopularTriggerTypes:   make(map[string]int64),
	}

	// Process workflow results
	if workflowRecords := workflowResult.([]*neo4j.Record); len(workflowRecords) > 0 {
		record := workflowRecords[0]
		if totalWorkflows, found := record.Get("total_workflows"); found {
			if count, ok := totalWorkflows.(int64); ok {
				analytics.TotalWorkflows = int(count)
			}
		}
		if activeWorkflows, found := record.Get("active_workflows"); found {
			if count, ok := activeWorkflows.(int64); ok {
				analytics.ActiveWorkflows = int(count)
			}
		}
		if avgRuntime, found := record.Get("avg_runtime"); found {
			if runtime, ok := avgRuntime.(float64); ok {
				analytics.AverageRuntime = runtime
			}
		}
	}

	// Process execution results
	if executionRecords := executionResult.([]*neo4j.Record); len(executionRecords) > 0 {
		record := executionRecords[0]
		if totalExecutions, found := record.Get("total_executions"); found {
			if count, ok := totalExecutions.(int64); ok {
				analytics.TotalExecutions = count
			}
		}
		if successfulExecutions, found := record.Get("successful_executions"); found {
			if count, ok := successfulExecutions.(int64); ok {
				analytics.SuccessfulExecutions = count
			}
		}
		if failedExecutions, found := record.Get("failed_executions"); found {
			if count, ok := failedExecutions.(int64); ok {
				analytics.FailedExecutions = count
			}
		}
		if totalRuntime, found := record.Get("total_runtime"); found {
			if runtime, ok := totalRuntime.(float64); ok {
				analytics.TotalProcessingTime = runtime
			}
		}
	}

	// Mock some popular trigger types for demo
	analytics.PopularTriggerTypes = map[string]int64{
		"upload":   245,
		"schedule": 123,
		"api":      89,
		"webhook":  67,
		"manual":   34,
	}

	return analytics, nil
}

// recordToWorkflow converts a Neo4j record to a Workflow
func (s *WorkflowService) recordToWorkflow(record *neo4j.Record) (*models.Workflow, error) {
	workflowNode, found := record.Get("w")
	if !found {
		return nil, fmt.Errorf("workflow node not found in record")
	}

	nodeValue, ok := workflowNode.(neo4j.Node)
	if !ok {
		return nil, fmt.Errorf("expected neo4j.Node, got %T", workflowNode)
	}

	props := nodeValue.Props
	workflow := &models.Workflow{}

	// Parse workflow properties
	if id, found := props["id"]; found {
		workflow.ID = id.(string)
	}
	if name, found := props["name"]; found {
		workflow.Name = name.(string)
	}
	if description, found := props["description"]; found && description != nil {
		workflow.Description = description.(string)
	}
	if status, found := props["status"]; found {
		workflow.Status = status.(string)
	}
	if workflowType, found := props["type"]; found {
		workflow.Type = workflowType.(string)
	}
	if version, found := props["version"]; found {
		workflow.Version = version.(string)
	}
	if executionCount, found := props["execution_count"]; found {
		if count, ok := executionCount.(int64); ok {
			workflow.ExecutionCount = count
		}
	}
	if avgRuntime, found := props["average_runtime"]; found {
		if runtime, ok := avgRuntime.(float64); ok {
			workflow.AverageRuntime = runtime
		}
	}
	if successRate, found := props["success_rate"]; found {
		if rate, ok := successRate.(float64); ok {
			workflow.SuccessRate = rate
		}
	}
	if createdBy, found := props["created_by"]; found {
		workflow.CreatedBy = createdBy.(string)
	}
	if tenantID, found := props["tenant_id"]; found {
		workflow.TenantID = tenantID.(string)
	}
	if orgID, found := props["organization_id"]; found {
		workflow.OrganizationID = orgID.(string)
	}

	// Parse timestamps
	if createdAt, found := props["created_at"]; found {
		if createdTime, ok := createdAt.(time.Time); ok {
			workflow.CreatedAt = createdTime
		}
	}
	if updatedAt, found := props["updated_at"]; found {
		if updatedTime, ok := updatedAt.(time.Time); ok {
			workflow.UpdatedAt = updatedTime
		}
	}
	if lastExecuted, found := props["last_executed"]; found && lastExecuted != nil {
		if lastTime, ok := lastExecuted.(time.Time); ok {
			workflow.LastExecuted = &lastTime
		}
	}

	// Parse configuration
	if configuration, found := props["configuration"]; found && configuration != nil {
		if configStr, ok := configuration.(string); ok && configStr != "" {
			workflow.Configuration = deserializeParameters(configStr)
		}
	}

	// Parse steps and triggers from record
	if stepsValue, found := record.Get("steps"); found {
		if stepsList, ok := stepsValue.([]interface{}); ok {
			for _, stepInterface := range stepsList {
				if stepNode, ok := stepInterface.(neo4j.Node); ok {
					step := s.nodeToWorkflowStep(stepNode)
					workflow.Steps = append(workflow.Steps, *step)
				}
			}
		}
	}

	if triggersValue, found := record.Get("triggers"); found {
		if triggersList, ok := triggersValue.([]interface{}); ok {
			for _, triggerInterface := range triggersList {
				if triggerNode, ok := triggerInterface.(neo4j.Node); ok {
					trigger := s.nodeToWorkflowTrigger(triggerNode)
					workflow.Triggers = append(workflow.Triggers, *trigger)
				}
			}
		}
	}

	return workflow, nil
}

// nodeToWorkflowStep converts a Neo4j node to a WorkflowStep
func (s *WorkflowService) nodeToWorkflowStep(node neo4j.Node) *models.WorkflowStep {
	props := node.Props
	step := &models.WorkflowStep{}

	if id, found := props["id"]; found {
		step.ID = id.(string)
	}
	if workflowID, found := props["workflow_id"]; found {
		step.WorkflowID = workflowID.(string)
	}
	if name, found := props["name"]; found {
		step.Name = name.(string)
	}
	if stepType, found := props["type"]; found {
		step.Type = stepType.(string)
	}
	if order, found := props["order"]; found {
		if orderInt, ok := order.(int64); ok {
			step.Order = int(orderInt)
		}
	}
	if timeout, found := props["timeout"]; found {
		if timeoutInt, ok := timeout.(int64); ok {
			step.Timeout = int(timeoutInt)
		}
	}
	if retryCount, found := props["retry_count"]; found {
		if retryInt, ok := retryCount.(int64); ok {
			step.RetryCount = int(retryInt)
		}
	}
	if onSuccess, found := props["on_success"]; found {
		step.OnSuccess = onSuccess.(string)
	}
	if onFailure, found := props["on_failure"]; found {
		step.OnFailure = onFailure.(string)
	}
	if createdAt, found := props["created_at"]; found {
		if createdTime, ok := createdAt.(time.Time); ok {
			step.CreatedAt = createdTime
		}
	}

	// Parse configuration and conditions
	if configuration, found := props["configuration"]; found && configuration != nil {
		if configStr, ok := configuration.(string); ok && configStr != "" {
			step.Configuration = deserializeParameters(configStr)
		}
	}
	if conditions, found := props["conditions"]; found && conditions != nil {
		if condStr, ok := conditions.(string); ok && condStr != "" {
			step.Conditions = deserializeParameters(condStr)
		}
	}

	// Parse Argo-aligned fields
	if deps, found := props["dependencies"]; found && deps != nil {
		if depsList, ok := deps.([]interface{}); ok {
			for _, d := range depsList {
				if ds, ok := d.(string); ok {
					step.Dependencies = append(step.Dependencies, ds)
				}
			}
		}
	}
	if templateName, found := props["template_name"]; found && templateName != nil {
		if tn, ok := templateName.(string); ok {
			step.TemplateName = tn
		}
	}
	if templateType, found := props["template_type"]; found && templateType != nil {
		if tt, ok := templateType.(string); ok {
			step.TemplateType = tt
		}
	}
	if when, found := props["when_condition"]; found && when != nil {
		if w, ok := when.(string); ok {
			step.When = w
		}
	}

	return step
}

// nodeToWorkflowTrigger converts a Neo4j node to a WorkflowTrigger
func (s *WorkflowService) nodeToWorkflowTrigger(node neo4j.Node) *models.WorkflowTrigger {
	props := node.Props
	trigger := &models.WorkflowTrigger{}

	if id, found := props["id"]; found {
		trigger.ID = id.(string)
	}
	if workflowID, found := props["workflow_id"]; found {
		trigger.WorkflowID = workflowID.(string)
	}
	if triggerType, found := props["type"]; found {
		trigger.Type = triggerType.(string)
	}
	if name, found := props["name"]; found {
		trigger.Name = name.(string)
	}
	if isActive, found := props["is_active"]; found {
		if active, ok := isActive.(bool); ok {
			trigger.IsActive = active
		}
	}
	if triggerCount, found := props["trigger_count"]; found {
		if count, ok := triggerCount.(int64); ok {
			trigger.TriggerCount = count
		}
	}
	if createdAt, found := props["created_at"]; found {
		if createdTime, ok := createdAt.(time.Time); ok {
			trigger.CreatedAt = createdTime
		}
	}
	if lastTriggered, found := props["last_triggered"]; found && lastTriggered != nil {
		if lastTime, ok := lastTriggered.(time.Time); ok {
			trigger.LastTriggered = &lastTime
		}
	}

	// Parse configuration
	if configuration, found := props["configuration"]; found && configuration != nil {
		if configStr, ok := configuration.(string); ok && configStr != "" {
			trigger.Configuration = deserializeParameters(configStr)
		}
	}

	return trigger
}

// recordToWorkflowExecution converts a Neo4j record to a WorkflowExecution
func (s *WorkflowService) recordToWorkflowExecution(record *neo4j.Record, alias string) (*models.WorkflowExecution, error) {
	node, found := record.Get(alias)
	if !found {
		return nil, fmt.Errorf("node %s not found in record", alias)
	}

	nodeValue, ok := node.(neo4j.Node)
	if !ok {
		return nil, fmt.Errorf("expected neo4j.Node, got %T", node)
	}

	props := nodeValue.Props
	execution := &models.WorkflowExecution{}

	// Parse execution properties
	if id, found := props["id"]; found {
		execution.ID = id.(string)
	}
	if workflowID, found := props["workflow_id"]; found {
		execution.WorkflowID = workflowID.(string)
	}
	if triggerID, found := props["trigger_id"]; found {
		execution.TriggerID = triggerID.(string)
	}
	if status, found := props["status"]; found {
		execution.Status = status.(string)
	}
	if progress, found := props["progress"]; found {
		if prog, ok := progress.(float64); ok {
			execution.Progress = prog
		}
	}
	if currentStep, found := props["current_step"]; found && currentStep != nil {
		execution.CurrentStep = currentStep.(string)
	}
	if runtime, found := props["runtime"]; found {
		if rt, ok := runtime.(float64); ok {
			execution.Runtime = rt
		}
	}
	if errorMessage, found := props["error_message"]; found && errorMessage != nil {
		execution.ErrorMessage = errorMessage.(string)
	}
	if tenantID, found := props["tenant_id"]; found {
		execution.TenantID = tenantID.(string)
	}
	if orgID, found := props["organization_id"]; found {
		execution.OrganizationID = orgID.(string)
	}

	// Parse timestamps
	if startedAt, found := props["started_at"]; found {
		if startTime, ok := startedAt.(time.Time); ok {
			execution.StartedAt = startTime
		}
	}
	if completedAt, found := props["completed_at"]; found && completedAt != nil {
		if completeTime, ok := completedAt.(time.Time); ok {
			execution.CompletedAt = &completeTime
		}
	}

	// Parse input and output
	if input, found := props["input"]; found && input != nil {
		if inputStr, ok := input.(string); ok && inputStr != "" {
			execution.Input = deserializeParameters(inputStr)
		}
	}
	if output, found := props["output"]; found && output != nil {
		if outputStr, ok := output.(string); ok && outputStr != "" {
			execution.Output = deserializeParameters(outputStr)
		}
	}

	return execution, nil
}

// UploadTriggerConfig defines configuration for content-based upload routing
type UploadTriggerConfig struct {
	AcceptedExtensions []string `json:"accepted_extensions,omitempty"`
	AcceptedMimeTypes  []string `json:"accepted_mime_types,omitempty"`
	MaxFileSizeMB      int      `json:"max_file_size_mb,omitempty"`
	SourceFilter       string   `json:"source_filter,omitempty"`
	NotebookIDs        []string `json:"notebook_ids,omitempty"`
	TagFilters         []string `json:"tag_filters,omitempty"`
	UploadEndpoint     string   `json:"upload_endpoint,omitempty"`
	OutputNotebookID   string   `json:"output_notebook_id,omitempty"`
	OutputMode         string   `json:"output_mode,omitempty"`
}

// DocumentEventTriggerConfig defines configuration for document event triggers
type DocumentEventTriggerConfig struct {
	EventType      string   `json:"event_type"`
	NotebookIDs    []string `json:"notebook_ids,omitempty"`
	MimeTypeFilter []string `json:"mime_type_filter,omitempty"`
	TagFilters     []string `json:"tag_filters,omitempty"`
}

// EvaluateUploadTriggers checks active workflows for matching upload triggers and publishes events
func (s *WorkflowService) EvaluateUploadTriggers(ctx context.Context, document *models.Document, source string, spaceContext *models.SpaceContext) {
	if s.kafkaService == nil {
		s.logger.Debug("Kafka not available, skipping upload trigger evaluation")
		return
	}

	query := `
		MATCH (w:Workflow)-[:HAS_TRIGGER]->(t:WorkflowTrigger)
		WHERE w.tenant_id = $tenant_id
		  AND w.status = 'active'
		  AND t.type = 'upload'
		  AND t.is_active = true
		RETURN w.id as workflow_id, t.id as trigger_id, t.configuration as trigger_config
	`

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"tenant_id": spaceContext.TenantID,
		})
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to query upload triggers", zap.Error(err))
		return
	}

	records := result.([]*neo4j.Record)
	for _, record := range records {
		workflowID, _ := record.Get("workflow_id")
		triggerID, _ := record.Get("trigger_id")
		triggerConfigStr, _ := record.Get("trigger_config")

		var triggerConfig UploadTriggerConfig
		if configStr, ok := triggerConfigStr.(string); ok && configStr != "" {
			if err := json.Unmarshal([]byte(configStr), &triggerConfig); err != nil {
				s.logger.Warn("Failed to parse trigger config", zap.Error(err))
				continue
			}
		}

		if !s.matchesUploadTrigger(document, source, &triggerConfig) {
			continue
		}

		outputNotebookID := triggerConfig.OutputNotebookID
		if triggerConfig.OutputMode == "same_notebook" || outputNotebookID == "" {
			outputNotebookID = document.NotebookID
		}

		eventData := map[string]interface{}{
			"workflow_id":        workflowID,
			"trigger_id":        triggerID,
			"document_id":       document.ID,
			"file_name":         document.Name,
			"mime_type":         document.MimeType,
			"size_bytes":        document.SizeBytes,
			"notebook_id":       document.NotebookID,
			"tags":              document.Tags,
			"storage_path":      document.StoragePath,
			"output_notebook_id": outputNotebookID,
			"tenant_id":         spaceContext.TenantID,
			"space_id":          spaceContext.SpaceID,
		}

		event := NewWorkflowEvent(EventWorkflowFileUploaded, workflowID.(string), "", eventData)
		if err := s.kafkaService.PublishEvent(ctx, event); err != nil {
			s.logger.Error("Failed to publish workflow file uploaded event",
				zap.String("workflow_id", workflowID.(string)),
				zap.Error(err))
		} else {
			s.logger.Info("Published workflow upload trigger",
				zap.String("workflow_id", workflowID.(string)),
				zap.String("trigger_id", triggerID.(string)),
				zap.String("document_id", document.ID))
		}
	}
}

// matchesUploadTrigger checks if a document matches an upload trigger's filters
func (s *WorkflowService) matchesUploadTrigger(doc *models.Document, source string, config *UploadTriggerConfig) bool {
	// Source filter
	if config.SourceFilter != "" && config.SourceFilter != "any" && config.SourceFilter != source {
		return false
	}

	// Extension filter
	if len(config.AcceptedExtensions) > 0 {
		ext := strings.TrimPrefix(filepath.Ext(doc.Name), ".")
		found := false
		for _, accepted := range config.AcceptedExtensions {
			if strings.EqualFold(ext, accepted) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// MIME type filter
	if len(config.AcceptedMimeTypes) > 0 {
		found := false
		for _, accepted := range config.AcceptedMimeTypes {
			if strings.EqualFold(doc.MimeType, accepted) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// File size filter
	if config.MaxFileSizeMB > 0 && doc.SizeBytes > int64(config.MaxFileSizeMB)*1024*1024 {
		return false
	}

	// Notebook ID filter
	if len(config.NotebookIDs) > 0 {
		found := false
		for _, nbID := range config.NotebookIDs {
			if nbID == doc.NotebookID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Tag filter (match any)
	if len(config.TagFilters) > 0 && len(doc.Tags) > 0 {
		found := false
		for _, filterTag := range config.TagFilters {
			for _, docTag := range doc.Tags {
				if strings.EqualFold(filterTag, docTag) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// EvaluateDocumentEventTriggers checks active workflows for matching document event triggers
func (s *WorkflowService) EvaluateDocumentEventTriggers(ctx context.Context, document *models.Document, eventType string, spaceContext *models.SpaceContext) {
	if s.kafkaService == nil {
		s.logger.Debug("Kafka not available, skipping document event trigger evaluation")
		return
	}

	query := `
		MATCH (w:Workflow)-[:HAS_TRIGGER]->(t:WorkflowTrigger)
		WHERE w.tenant_id = $tenant_id
		  AND w.status = 'active'
		  AND t.type = 'document_event'
		  AND t.is_active = true
		RETURN w.id as workflow_id, t.id as trigger_id, t.configuration as trigger_config
	`

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, map[string]interface{}{
			"tenant_id": spaceContext.TenantID,
		})
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to query document event triggers", zap.Error(err))
		return
	}

	records := result.([]*neo4j.Record)
	for _, record := range records {
		workflowID, _ := record.Get("workflow_id")
		triggerID, _ := record.Get("trigger_id")
		triggerConfigStr, _ := record.Get("trigger_config")

		var triggerConfig DocumentEventTriggerConfig
		if configStr, ok := triggerConfigStr.(string); ok && configStr != "" {
			if err := json.Unmarshal([]byte(configStr), &triggerConfig); err != nil {
				s.logger.Warn("Failed to parse document event trigger config", zap.Error(err))
				continue
			}
		}

		if !s.matchesDocumentEventTrigger(document, eventType, &triggerConfig) {
			continue
		}

		eventData := map[string]interface{}{
			"workflow_id":   workflowID,
			"trigger_id":   triggerID,
			"document_id":  document.ID,
			"event_type":   eventType,
			"file_name":    document.Name,
			"mime_type":    document.MimeType,
			"notebook_id":  document.NotebookID,
			"tenant_id":    spaceContext.TenantID,
			"space_id":     spaceContext.SpaceID,
		}

		event := NewWorkflowEvent(EventWorkflowDocumentEvent, workflowID.(string), "", eventData)
		if err := s.kafkaService.PublishEvent(ctx, event); err != nil {
			s.logger.Error("Failed to publish workflow document event",
				zap.String("workflow_id", workflowID.(string)),
				zap.Error(err))
		} else {
			s.logger.Info("Published workflow document event trigger",
				zap.String("workflow_id", workflowID.(string)),
				zap.String("event_type", eventType),
				zap.String("document_id", document.ID))
		}
	}
}

// matchesDocumentEventTrigger checks if a document event matches a trigger's filters
func (s *WorkflowService) matchesDocumentEventTrigger(doc *models.Document, eventType string, config *DocumentEventTriggerConfig) bool {
	// Event type must match
	if config.EventType != "" && config.EventType != eventType {
		return false
	}

	// MIME type filter
	if len(config.MimeTypeFilter) > 0 {
		found := false
		for _, accepted := range config.MimeTypeFilter {
			if strings.EqualFold(doc.MimeType, accepted) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Notebook ID filter
	if len(config.NotebookIDs) > 0 {
		found := false
		for _, nbID := range config.NotebookIDs {
			if nbID == doc.NotebookID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Tag filter
	if len(config.TagFilters) > 0 && len(doc.Tags) > 0 {
		found := false
		for _, filterTag := range config.TagFilters {
			for _, docTag := range doc.Tags {
				if strings.EqualFold(filterTag, docTag) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// PublishProductionArtifact stores a production artifact and links it to a notebook
func (s *WorkflowService) PublishProductionArtifact(ctx context.Context, workflowID, executionID string, artifact map[string]interface{}, destinationNotebookID string, spaceContext *models.SpaceContext) error {
	artifactID := fmt.Sprintf("art_%d", time.Now().UnixNano())

	query := `
		CREATE (a:WorkflowArtifact {
			id: $id,
			workflow_id: $workflow_id,
			execution_id: $execution_id,
			tier: 'production',
			name: $name,
			format: $format,
			storage_path: $storage_path,
			notebook_id: $notebook_id,
			tenant_id: $tenant_id,
			created_at: $created_at
		})
		WITH a
		MATCH (w:Workflow {id: $workflow_id})
		CREATE (w)-[:HAS_ARTIFACT]->(a)
		RETURN a
	`

	name, _ := artifact["name"].(string)
	format, _ := artifact["format"].(string)
	storagePath, _ := artifact["storage_path"].(string)

	params := map[string]interface{}{
		"id":            artifactID,
		"workflow_id":   workflowID,
		"execution_id":  executionID,
		"name":          name,
		"format":        format,
		"storage_path":  storagePath,
		"notebook_id":   destinationNotebookID,
		"tenant_id":     spaceContext.TenantID,
		"created_at":    time.Now(),
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to publish production artifact",
			zap.String("workflow_id", workflowID),
			zap.String("execution_id", executionID),
			zap.Error(err))
		return fmt.Errorf("failed to publish production artifact: %w", err)
	}

	s.logger.Info("Published production artifact",
		zap.String("artifact_id", artifactID),
		zap.String("workflow_id", workflowID),
		zap.String("notebook_id", destinationNotebookID))

	return nil
}