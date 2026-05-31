package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// WorkflowMetrics contains all Prometheus metrics for workflow operations
type WorkflowMetrics struct {
	// Execution metrics
	ExecutionsTotal   *prometheus.CounterVec
	ExecutionDuration *prometheus.HistogramVec
	ActiveExecutions  prometheus.Gauge

	// Step metrics
	StepDuration      *prometheus.HistogramVec
	StepFailuresTotal *prometheus.CounterVec
	StepRetriesTotal  *prometheus.CounterVec

	// MCP/Agent/LLM metrics
	AgentCallsTotal *prometheus.CounterVec
	MCPCallsTotal   *prometheus.CounterVec
	LLMTokensTotal  *prometheus.CounterVec
	LLMCostTotal    *prometheus.CounterVec

	// Trigger/Sync metrics
	TriggerEventsTotal *prometheus.CounterVec
	SyncTotal          *prometheus.CounterVec
	SyncFailuresTotal  *prometheus.CounterVec
}

// NewWorkflowMetrics creates and registers all workflow-related Prometheus metrics
func NewWorkflowMetrics() *WorkflowMetrics {
	wm := &WorkflowMetrics{
		// Execution metrics
		ExecutionsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_executions_total",
				Help: "Total number of workflow executions",
			},
			[]string{"workflow_id", "status", "trigger_type", "space_id"},
		),
		ExecutionDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "workflow_execution_duration_seconds",
				Help:    "Workflow execution duration in seconds",
				Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800},
			},
			[]string{"workflow_id", "trigger_type"},
		),
		ActiveExecutions: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "workflow_active_executions",
				Help: "Number of currently active workflow executions",
			},
		),

		// Step metrics
		StepDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "workflow_step_duration_seconds",
				Help:    "Workflow step execution duration in seconds",
				Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 300},
			},
			[]string{"workflow_id", "step_name", "step_type"},
		),
		StepFailuresTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_step_failures_total",
				Help: "Total number of workflow step failures",
			},
			[]string{"workflow_id", "step_name", "step_type", "error_type"},
		),
		StepRetriesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_step_retries_total",
				Help: "Total number of workflow step retries",
			},
			[]string{"workflow_id", "step_name", "step_type"},
		),

		// MCP/Agent/LLM metrics
		AgentCallsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_agent_calls_total",
				Help: "Total number of agent calls from workflows",
			},
			[]string{"agent_id", "status"},
		),
		MCPCallsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_mcp_calls_total",
				Help: "Total number of MCP tool calls from workflows",
			},
			[]string{"server", "tool", "status"},
		),
		LLMTokensTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_llm_tokens_total",
				Help: "Total number of LLM tokens used by workflows",
			},
			[]string{"workflow_id", "step_name", "provider", "direction"},
		),
		LLMCostTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_llm_cost_usd_total",
				Help: "Total LLM cost in USD for workflow executions",
			},
			[]string{"workflow_id", "provider"},
		),

		// Trigger/Sync metrics
		TriggerEventsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_trigger_events_total",
				Help: "Total number of workflow trigger events",
			},
			[]string{"trigger_type", "event_source", "status"},
		),
		SyncTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_sync_total",
				Help: "Total number of workflow sync (exit handler) operations",
			},
			[]string{"sync_type", "destination", "status"},
		),
		SyncFailuresTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_sync_failures_total",
				Help: "Total number of workflow sync failures",
			},
			[]string{"sync_type", "destination", "error_type"},
		),
	}

	// Register all workflow metrics
	prometheus.MustRegister(
		wm.ExecutionsTotal,
		wm.ExecutionDuration,
		wm.ActiveExecutions,
		wm.StepDuration,
		wm.StepFailuresTotal,
		wm.StepRetriesTotal,
		wm.AgentCallsTotal,
		wm.MCPCallsTotal,
		wm.LLMTokensTotal,
		wm.LLMCostTotal,
		wm.TriggerEventsTotal,
		wm.SyncTotal,
		wm.SyncFailuresTotal,
	)

	return wm
}

// RecordExecution records a workflow execution metric
func (wm *WorkflowMetrics) RecordExecution(workflowID, status, triggerType, spaceID string, duration time.Duration) {
	wm.ExecutionsTotal.WithLabelValues(workflowID, status, triggerType, spaceID).Inc()
	wm.ExecutionDuration.WithLabelValues(workflowID, triggerType).Observe(duration.Seconds())
}

// IncActiveExecutions increments the active executions gauge
func (wm *WorkflowMetrics) IncActiveExecutions() {
	wm.ActiveExecutions.Inc()
}

// DecActiveExecutions decrements the active executions gauge
func (wm *WorkflowMetrics) DecActiveExecutions() {
	wm.ActiveExecutions.Dec()
}

// RecordStepExecution records a workflow step execution metric
func (wm *WorkflowMetrics) RecordStepExecution(workflowID, stepName, stepType string, duration time.Duration) {
	wm.StepDuration.WithLabelValues(workflowID, stepName, stepType).Observe(duration.Seconds())
}

// RecordStepFailure records a workflow step failure
func (wm *WorkflowMetrics) RecordStepFailure(workflowID, stepName, stepType, errorType string) {
	wm.StepFailuresTotal.WithLabelValues(workflowID, stepName, stepType, errorType).Inc()
}

// RecordStepRetry records a workflow step retry
func (wm *WorkflowMetrics) RecordStepRetry(workflowID, stepName, stepType string) {
	wm.StepRetriesTotal.WithLabelValues(workflowID, stepName, stepType).Inc()
}

// RecordAgentCall records an agent call from a workflow
func (wm *WorkflowMetrics) RecordAgentCall(agentID, status string) {
	wm.AgentCallsTotal.WithLabelValues(agentID, status).Inc()
}

// RecordMCPCall records an MCP tool call from a workflow
func (wm *WorkflowMetrics) RecordMCPCall(server, tool, status string) {
	wm.MCPCallsTotal.WithLabelValues(server, tool, status).Inc()
}

// RecordLLMTokens records LLM token usage from a workflow step
func (wm *WorkflowMetrics) RecordLLMTokens(workflowID, stepName, provider, direction string, count float64) {
	wm.LLMTokensTotal.WithLabelValues(workflowID, stepName, provider, direction).Add(count)
}

// RecordLLMCost records LLM cost in USD from a workflow
func (wm *WorkflowMetrics) RecordLLMCost(workflowID, provider string, costUSD float64) {
	wm.LLMCostTotal.WithLabelValues(workflowID, provider).Add(costUSD)
}

// RecordTriggerEvent records a workflow trigger event
func (wm *WorkflowMetrics) RecordTriggerEvent(triggerType, eventSource, status string) {
	wm.TriggerEventsTotal.WithLabelValues(triggerType, eventSource, status).Inc()
}

// RecordSync records a workflow sync (exit handler) operation
func (wm *WorkflowMetrics) RecordSync(syncType, destination, status string) {
	wm.SyncTotal.WithLabelValues(syncType, destination, status).Inc()
}

// RecordSyncFailure records a workflow sync failure
func (wm *WorkflowMetrics) RecordSyncFailure(syncType, destination, errorType string) {
	wm.SyncFailuresTotal.WithLabelValues(syncType, destination, errorType).Inc()
}
