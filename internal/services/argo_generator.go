package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// Default LLM Router URL for Argo pods calling the TAS LLM Router service
const defaultLLMRouterURL = "http://llm-router.tas-llm-router.svc.cluster.local:8086/v1/chat/completions"

// ArgoGenerator converts workflow definitions into Argo Workflow CRDs and submits them
type ArgoGenerator struct {
	namespace          string
	serviceAccountName string
	enabled            bool
	llmRouterURL       string
	deeplakeURL        string
	client             *http.Client
	logger             *zap.Logger
}

// NewArgoGenerator creates a new Argo Workflow generator
func NewArgoGenerator(log *logger.Logger) *ArgoGenerator {
	namespace := os.Getenv("ARGO_WORKFLOWS_NAMESPACE")
	if namespace == "" {
		namespace = "argo"
	}

	saName := os.Getenv("ARGO_SERVICE_ACCOUNT")
	if saName == "" {
		saName = "argo-workflow-runner"
	}

	enabled := os.Getenv("ARGO_WORKFLOWS_ENABLED") != "false"

	llmRouterURL := os.Getenv("LLM_ROUTER_URL")
	if llmRouterURL == "" {
		llmRouterURL = defaultLLMRouterURL
	}

	deeplakeURL := os.Getenv("DEEPLAKE_BASE_URL")
	if deeplakeURL == "" {
		deeplakeURL = "http://deeplake-api.aether-be.svc.cluster.local:8000"
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &ArgoGenerator{
		namespace:          namespace,
		serviceAccountName: saName,
		enabled:            enabled,
		llmRouterURL:       llmRouterURL,
		deeplakeURL:        deeplakeURL,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		logger: log.Logger,
	}
}

// IsEnabled returns whether Argo Workflows integration is enabled
func (g *ArgoGenerator) IsEnabled() bool {
	return g.enabled
}

// GenerateWorkflowCRD converts a workflow with steps into an Argo Workflow CRD map
func (g *ArgoGenerator) GenerateWorkflowCRD(workflow *models.Workflow) (map[string]interface{}, error) {
	// All steps go in the DAG (including sync steps) so they can access
	// upstream task outputs via DAG arguments
	mainSteps := workflow.Steps

	// Separate error handler steps to use as onExit handlers
	var regularSteps []models.WorkflowStep
	var errorHandlerSteps []models.WorkflowStep
	for _, step := range mainSteps {
		if step.Type == "errorHandler" {
			errorHandlerSteps = append(errorHandlerSteps, step)
		} else {
			regularSteps = append(regularSteps, step)
		}
	}

	templates := g.buildTemplates(regularSteps)
	// Add error handler templates
	for _, step := range errorHandlerSteps {
		tmpl := g.buildStepTemplate(step)
		if tmpl != nil {
			templates = append(templates, tmpl)
		}
	}

	spec := map[string]interface{}{
		"entrypoint":         "main",
		"serviceAccountName": g.serviceAccountName,
		"templates":          templates,
		"arguments": map[string]interface{}{
			"parameters": []map[string]interface{}{
				{"name": "workflow_id", "value": workflow.ID},
				{"name": "workflow_name", "value": workflow.Name},
				{"name": "user_id", "value": workflow.CreatedBy},
				{"name": "tenant_id", "value": workflow.TenantID},
			},
		},
	}

	// Set onExit handler if error handler steps exist
	if len(errorHandlerSteps) > 0 {
		spec["onExit"] = sanitizeName(errorHandlerSteps[0].Name) + "-tmpl"
	}

	crd := map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Workflow",
		"metadata": map[string]interface{}{
			"generateName": sanitizeName(workflow.Name) + "-",
			"namespace":    g.namespace,
			"labels": map[string]interface{}{
				"app.kubernetes.io/managed-by": "aether",
				"aether/workflow-id":           workflow.ID,
			},
		},
		"spec": spec,
	}

	return crd, nil
}

// buildTemplates creates the DAG template and individual step templates
func (g *ArgoGenerator) buildTemplates(steps []models.WorkflowStep) []interface{} {
	templates := []interface{}{}

	// Build DAG tasks
	dagTasks := []interface{}{}
	for _, step := range steps {
		task := map[string]interface{}{
			"name":     sanitizeName(step.Name),
			"template": sanitizeName(step.Name) + "-tmpl",
		}

		// Add dependencies
		if len(step.Dependencies) > 0 {
			task["dependencies"] = step.Dependencies
		}

		// Add when condition
		if step.When != "" {
			task["when"] = step.When
		}

		// Add arguments from inputs
		params := []interface{}{}
		if step.Inputs != nil && len(step.Inputs.Parameters) > 0 {
			for _, p := range step.Inputs.Parameters {
				param := map[string]interface{}{"name": p.Name}
				if p.Value != "" {
					param["value"] = p.Value
				} else if p.ValueFrom != nil && p.ValueFrom.Expression != "" {
					param["expression"] = p.ValueFrom.Expression
				}
				params = append(params, param)
			}
		}

		// Auto-inject upstream task results as parameters for ALL steps with dependencies.
		// This enables data flow between connected steps via Argo parameter passing.
		if len(step.Dependencies) > 0 {
			for i, dep := range step.Dependencies {
				params = append(params, map[string]interface{}{
					"name":  fmt.Sprintf("input-%d", i),
					"value": fmt.Sprintf("{{tasks.%s.outputs.result}}", sanitizeName(dep)),
				})
			}
			// For sync steps, also pass the last dependency's output as "upstream-result"
			// for backward compatibility with the webhook payload
			if step.Type == "sync" {
				lastDep := step.Dependencies[len(step.Dependencies)-1]
				params = append(params, map[string]interface{}{
					"name":  "upstream-result",
					"value": fmt.Sprintf("{{tasks.%s.outputs.result}}", sanitizeName(lastDep)),
				})
			}
		}

		if len(params) > 0 {
			task["arguments"] = map[string]interface{}{
				"parameters": params,
			}
		}

		dagTasks = append(dagTasks, task)
	}

	// Main DAG template
	mainTemplate := map[string]interface{}{
		"name": "main",
		"dag": map[string]interface{}{
			"tasks": dagTasks,
		},
	}
	templates = append(templates, mainTemplate)

	// Individual step templates
	for _, step := range steps {
		tmpl := g.buildStepTemplate(step)
		if tmpl != nil {
			templates = append(templates, tmpl)
		}
	}

	return templates
}

// buildStepTemplate creates an Argo template for a single step based on its template_type
func (g *ArgoGenerator) buildStepTemplate(step models.WorkflowStep) map[string]interface{} {
	tmpl := map[string]interface{}{
		"name": sanitizeName(step.Name) + "-tmpl",
	}

	// Add inputs/outputs if defined
	if step.Inputs != nil {
		tmpl["inputs"] = buildIOSpec(step.Inputs)
	}
	if step.Outputs != nil {
		tmpl["outputs"] = buildIOSpec(step.Outputs)
	}

	// Auto-declare input parameters for steps with dependencies (data flow)
	// This ensures templates accept the upstream results passed by DAG task arguments
	if len(step.Dependencies) > 0 && step.Type != "sync" {
		inputParams := []map[string]interface{}{}
		for i := range step.Dependencies {
			inputParams = append(inputParams, map[string]interface{}{
				"name":    fmt.Sprintf("input-%d", i),
				"default": "",
			})
		}
		// Merge with existing inputs if present
		if existing, ok := tmpl["inputs"].(map[string]interface{}); ok {
			if existingParams, ok := existing["parameters"].([]interface{}); ok {
				for _, p := range inputParams {
					existingParams = append(existingParams, p)
				}
				existing["parameters"] = existingParams
			} else {
				params := make([]interface{}, len(inputParams))
				for i, p := range inputParams {
					params[i] = p
				}
				existing["parameters"] = params
			}
		} else {
			params := make([]interface{}, len(inputParams))
			for i, p := range inputParams {
				params[i] = p
			}
			tmpl["inputs"] = map[string]interface{}{
				"parameters": params,
			}
		}
	}

	// Add retry strategy
	if step.RetryStrategy != nil && step.RetryStrategy.Limit > 0 {
		retry := map[string]interface{}{
			"limit": step.RetryStrategy.Limit,
		}
		if step.RetryStrategy.Duration != "" || step.RetryStrategy.Factor > 0 {
			backoff := map[string]interface{}{}
			if step.RetryStrategy.Duration != "" {
				backoff["duration"] = step.RetryStrategy.Duration
			}
			if step.RetryStrategy.Factor > 0 {
				backoff["factor"] = step.RetryStrategy.Factor
			}
			if step.RetryStrategy.MaxDuration != "" {
				backoff["maxDuration"] = step.RetryStrategy.MaxDuration
			}
			retry["backoff"] = backoff
		}
		if step.RetryStrategy.RetryPolicy != "" {
			retry["retryPolicy"] = step.RetryStrategy.RetryPolicy
		}
		tmpl["retryStrategy"] = retry
	}

	// Data pinning: if step has pinned data, skip real execution and echo the pinned output
	if pinnedData, ok := step.Configuration["pinned_data"].(string); ok && pinnedData != "" {
		tmpl["script"] = map[string]interface{}{
			"image":   "alpine:3.19",
			"command": []string{"sh", "-c"},
			"source":  fmt.Sprintf("echo '%s'", strings.ReplaceAll(pinnedData, "'", "'\\''")),
		}
		return tmpl
	}

	templateType := step.TemplateType
	if templateType == "" {
		templateType = inferTemplateType(step.Type)
	}

	// Special handling: if this is an aiTask with LLM config, build an LLM container
	if step.Type == "aiTask" {
		aiTaskType, _ := step.Configuration["aiTaskType"].(string)
		if aiTaskType == "llm" {
			scriptSpec := g.buildAITaskLLMScript(step.Configuration)
			// Inject env vars for upstream inputs so AI tasks can use them
			if len(step.Dependencies) > 0 {
				env := []map[string]interface{}{}
				for i := range step.Dependencies {
					env = append(env, map[string]interface{}{
						"name":  fmt.Sprintf("INPUT_%d", i),
						"value": fmt.Sprintf("{{inputs.parameters.input-%d}}", i),
					})
				}
				scriptSpec["env"] = env
			}
			tmpl["script"] = scriptSpec
			return tmpl
		}
	}

	// Special handling: assembler step → script that assembles upstream outputs
	if step.Type == "assembler" {
		scriptSpec := g.buildAssemblerScript(step.Configuration)
		// Inject env vars: INPUT_N for upstream outputs, plus WORKFLOW_NAME and LLM_ROUTER_URL
		env := []map[string]interface{}{
			{"name": "WORKFLOW_NAME", "value": "{{workflow.parameters.workflow_name}}"},
			{"name": "LLM_ROUTER_URL", "value": g.llmRouterURL},
		}
		for i := range step.Dependencies {
			env = append(env, map[string]interface{}{
				"name":  fmt.Sprintf("INPUT_%d", i),
				"value": fmt.Sprintf("{{inputs.parameters.input-%d}}", i),
			})
		}
		scriptSpec["env"] = env
		tmpl["script"] = scriptSpec
		return tmpl
	}

	// Special handling: merge step → script that combines multiple upstream outputs
	if step.Type == "merge" {
		scriptSpec := buildMergeScript(step.Configuration)
		if len(step.Dependencies) > 0 {
			env := []map[string]interface{}{}
			for i := range step.Dependencies {
				env = append(env, map[string]interface{}{
					"name":  fmt.Sprintf("INPUT_%d", i),
					"value": fmt.Sprintf("{{inputs.parameters.input-%d}}", i),
				})
			}
			scriptSpec["env"] = env
		}
		tmpl["script"] = scriptSpec
		return tmpl
	}

	// Special handling: loop step → uses Argo withParam for fan-out
	if step.Type == "loop" {
		tmpl["script"] = buildLoopScript(step.Configuration)
		return tmpl
	}

	// Special handling: subworkflow step → Argo templateRef
	if step.Type == "subworkflow" {
		workflowRef, _ := step.Configuration["workflow_ref"].(string)
		if workflowRef != "" {
			tmpl["templateRef"] = map[string]interface{}{
				"name":     workflowRef,
				"template": "main",
			}
		} else {
			tmpl["script"] = map[string]interface{}{
				"image":   "alpine:3.19",
				"command": []string{"sh", "-c"},
				"source":  "echo 'sub-workflow placeholder'",
			}
		}
		return tmpl
	}

	// Special handling: errorHandler step → generates onExit template
	if step.Type == "errorHandler" {
		tmpl["script"] = buildErrorHandlerScript(step.Configuration)
		return tmpl
	}

	// Special handling: sync step → script template with curl to deliver results
	// Using script (curl) instead of HTTP template because HTTP templates require
	// additional Argo controller configuration that may not be present
	if step.Type == "sync" {
		// Declare input parameter so DAG task arguments can pass upstream results
		tmpl["inputs"] = map[string]interface{}{
			"parameters": []map[string]interface{}{
				{"name": "upstream-result", "default": ""},
			},
		}
		tmpl["script"] = buildSyncScript(step.Configuration)
		return tmpl
	}

	switch templateType {
	case "container":
		tmpl["container"] = buildContainerSpec(step.Configuration)
	case "script":
		tmpl["script"] = buildScriptSpec(step.Configuration)
	case "http":
		tmpl["http"] = buildHTTPSpec(step.Configuration)
	case "suspend":
		tmpl["suspend"] = buildSuspendSpec(step.Configuration)
	case "data":
		// Transform nodes don't create separate templates — they inject expressions
		// into downstream parameter references. Return a simple script that outputs
		// the expression result for now.
		tmpl["script"] = buildTransformScript(step.Configuration)
	case "condition":
		// Condition nodes evaluate an expression and output "true" or "false".
		// Downstream tasks use `when: {{tasks.condition.outputs.result}} == true|false`
		// to implement branching.
		tmpl["script"] = buildConditionScript(step.Configuration)
	default:
		// Fallback: treat as container
		tmpl["container"] = buildContainerSpec(step.Configuration)
	}

	return tmpl
}

// buildContainerSpec creates an Argo container template spec
func buildContainerSpec(config map[string]interface{}) map[string]interface{} {
	spec := map[string]interface{}{}

	if image, ok := config["image"].(string); ok && image != "" {
		spec["image"] = image
	} else {
		spec["image"] = "alpine:3.19"
	}

	if cmd, ok := config["command"]; ok {
		spec["command"] = cmd
	}
	if args, ok := config["args"]; ok {
		spec["args"] = args
	}

	// Environment variables
	if envList, ok := config["env"].([]interface{}); ok && len(envList) > 0 {
		spec["env"] = envList
	}

	// Resources
	if resources, ok := config["resources"].(map[string]interface{}); ok {
		spec["resources"] = resources
	}

	return spec
}

// buildAITaskLLMScript creates a script template that calls the TAS LLM Router
// via curl from inside the Argo pod. The prompt, model, and parameters come from
// the step configuration. Supports chain types: completion, summarization, qa,
// classification, extraction. When mcpEnabled is true, calls Agent Builder instead.
func (g *ArgoGenerator) buildAITaskLLMScript(config map[string]interface{}) map[string]interface{} {
	model, _ := config["model"].(string)
	if model == "" {
		model = "gpt-4o-mini"
	}

	prompt, _ := config["prompt"].(string)
	if prompt == "" {
		prompt = "Hello, World!"
	}

	maxTokens := 2000
	if mt, ok := config["maxTokens"].(float64); ok && mt > 0 {
		maxTokens = int(mt)
	}

	temperature := 0.7
	if t, ok := config["temperature"].(float64); ok {
		temperature = t
	}

	systemPrompt, _ := config["systemPrompt"].(string)
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant."
	}

	chainType, _ := config["chainType"].(string)
	if chainType == "" {
		chainType = "completion"
	}

	mcpEnabled, _ := config["mcpEnabled"].(bool)
	ragEnabled, _ := config["ragEnabled"].(bool)

	// If MCP is enabled, delegate to Agent Builder which handles the MCP tool loop
	if mcpEnabled {
		return g.buildMCPAgentScript(config, model, prompt, systemPrompt, maxTokens)
	}

	// If RAG is enabled, query DeepLake for context before LLM call
	if ragEnabled {
		dataset, _ := config["ragDataset"].(string)
		topK := 5
		if tk, ok := config["ragTopK"].(float64); ok && tk > 0 {
			topK = int(tk)
		}
		return g.buildRAGScript(model, prompt, systemPrompt, maxTokens, temperature, dataset, topK)
	}

	// Build chain-type-specific script
	switch chainType {
	case "summarization":
		return g.buildSummarizationScript(model, prompt, systemPrompt, maxTokens, temperature)
	case "qa":
		return g.buildQAScript(model, prompt, systemPrompt, maxTokens, temperature)
	case "classification":
		return g.buildClassificationScript(model, prompt, systemPrompt, maxTokens, temperature)
	case "extraction":
		return g.buildExtractionScript(model, prompt, systemPrompt, maxTokens, temperature)
	default:
		return g.buildCompletionScript(model, prompt, systemPrompt, maxTokens, temperature)
	}
}

// buildLLMCallFunction returns a Python function string for calling the LLM Router
func (g *ArgoGenerator) buildLLMCallFunction() string {
	return fmt.Sprintf(`
def call_llm(model, system_prompt, user_prompt, max_tokens, temperature=0.7):
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt}
        ],
        "max_tokens": max_tokens,
        "temperature": temperature
    }
    url = %q
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as resp:
        result = json.loads(resp.read().decode("utf-8"))
        return result.get("choices", [{}])[0].get("message", {}).get("content", "")
`, g.llmRouterURL)
}

// collectUpstreamInputs returns Python code to gather upstream step outputs
func collectUpstreamInputs() string {
	return `
upstream_inputs = []
for i in range(20):
    val = os.environ.get(f'INPUT_{i}', '')
    if val:
        upstream_inputs.append(val)
`
}

// buildCompletionScript generates a standard LLM completion script (default chain type)
func (g *ArgoGenerator) buildCompletionScript(model, prompt, systemPrompt string, maxTokens int, temperature float64) map[string]interface{} {
	source := fmt.Sprintf(`import json, urllib.request, sys, os
%s
%s
user_prompt = %q
if upstream_inputs:
    context = '\n---\n'.join(upstream_inputs)
    user_prompt = user_prompt + '\n\nContext from previous steps:\n' + context

try:
    result = call_llm(%q, %q, user_prompt, %d, %g)
    print(result)
except Exception as e:
    print(f"LLM call failed: {e}", file=sys.stderr)
    sys.exit(1)
`, g.buildLLMCallFunction(), collectUpstreamInputs(), prompt, model, systemPrompt, maxTokens, temperature)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildSummarizationScript generates a map-reduce summarization script.
// Chunks the input, summarizes each chunk, then combines summaries.
func (g *ArgoGenerator) buildSummarizationScript(model, prompt, systemPrompt string, maxTokens int, temperature float64) map[string]interface{} {
	source := fmt.Sprintf(`import json, urllib.request, sys, os
%s
%s
user_instructions = %q
system_prompt = %q

# Combine all upstream inputs
full_text = '\n\n'.join(upstream_inputs) if upstream_inputs else user_instructions

# Chunk the text for map-reduce (approx 3000 chars per chunk)
CHUNK_SIZE = 3000
chunks = [full_text[i:i+CHUNK_SIZE] for i in range(0, len(full_text), CHUNK_SIZE)]

try:
    if len(chunks) <= 1:
        # Single chunk — direct summarization
        result = call_llm(%q, system_prompt, f"Summarize the following:\n\n{full_text}\n\n{user_instructions}", %d, %g)
        print(result)
    else:
        # Map phase: summarize each chunk
        chunk_summaries = []
        for i, chunk in enumerate(chunks):
            summary = call_llm(%q, "You are a summarization assistant. Produce a concise summary of the given text.",
                f"Summarize this section (part {i+1} of {len(chunks)}):\n\n{chunk}", %d, %g)
            chunk_summaries.append(summary)

        # Reduce phase: combine chunk summaries into final summary
        combined = '\n\n---\n\n'.join(chunk_summaries)
        result = call_llm(%q, system_prompt,
            f"Combine these section summaries into a single coherent summary:\n\n{combined}\n\n{user_instructions}", %d, %g)
        print(result)
except Exception as e:
    print(f"Summarization failed: {e}", file=sys.stderr)
    sys.exit(1)
`, g.buildLLMCallFunction(), collectUpstreamInputs(), prompt, systemPrompt,
		model, maxTokens, temperature,
		model, maxTokens, temperature,
		model, maxTokens, temperature)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildQAScript generates a question-answering script that injects context + question format
func (g *ArgoGenerator) buildQAScript(model, prompt, systemPrompt string, maxTokens int, temperature float64) map[string]interface{} {
	source := fmt.Sprintf(`import json, urllib.request, sys, os
%s
%s
question = %q

# Build context from upstream inputs
context = '\n\n'.join(upstream_inputs) if upstream_inputs else ''

qa_system = %q + """

You are a question-answering assistant. Answer the question based ONLY on the provided context.
If the context does not contain enough information to answer, say so clearly.
Always cite which part of the context supports your answer."""

if context:
    qa_prompt = f"Context:\n{context}\n\nQuestion: {question}\n\nAnswer:"
else:
    qa_prompt = f"Question: {question}\n\nAnswer:"

try:
    result = call_llm(%q, qa_system, qa_prompt, %d, %g)
    print(result)
except Exception as e:
    print(f"QA failed: {e}", file=sys.stderr)
    sys.exit(1)
`, g.buildLLMCallFunction(), collectUpstreamInputs(), prompt, systemPrompt,
		model, maxTokens, temperature)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildClassificationScript generates a classification script with structured output
func (g *ArgoGenerator) buildClassificationScript(model, prompt, systemPrompt string, maxTokens int, temperature float64) map[string]interface{} {
	source := fmt.Sprintf(`import json, urllib.request, sys, os
%s
%s
user_instructions = %q

# Build input from upstream
input_text = '\n\n'.join(upstream_inputs) if upstream_inputs else user_instructions

classification_system = %q + """

You are a classification assistant. Classify the input according to the given instructions.
Respond with ONLY a valid JSON object in this format:
{"label": "<classification label>", "confidence": <0.0-1.0>, "reasoning": "<brief explanation>"}
Do not include any other text outside the JSON."""

try:
    result = call_llm(%q, classification_system,
        f"Classify the following:\n\n{input_text}\n\nClassification instructions: {user_instructions}", %d, %g)
    # Validate JSON output
    try:
        parsed = json.loads(result)
        print(json.dumps(parsed))
    except json.JSONDecodeError:
        # If LLM didn't return valid JSON, wrap it
        print(json.dumps({"label": result.strip(), "confidence": 0.5, "reasoning": "Raw LLM output"}))
except Exception as e:
    print(f"Classification failed: {e}", file=sys.stderr)
    sys.exit(1)
`, g.buildLLMCallFunction(), collectUpstreamInputs(), prompt, systemPrompt,
		model, maxTokens, temperature)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildExtractionScript generates a structured data extraction script
func (g *ArgoGenerator) buildExtractionScript(model, prompt, systemPrompt string, maxTokens int, temperature float64) map[string]interface{} {
	source := fmt.Sprintf(`import json, urllib.request, sys, os
%s
%s
user_instructions = %q

# Build input from upstream
input_text = '\n\n'.join(upstream_inputs) if upstream_inputs else user_instructions

extraction_system = %q + """

You are a data extraction assistant. Extract structured data from the input text.
Respond with ONLY a valid JSON object containing the extracted fields.
Follow the extraction instructions precisely for which fields to extract.
Do not include any other text outside the JSON."""

try:
    result = call_llm(%q, extraction_system,
        f"Extract structured data from:\n\n{input_text}\n\nExtraction instructions: {user_instructions}", %d, %g)
    # Validate JSON output
    try:
        parsed = json.loads(result)
        print(json.dumps(parsed))
    except json.JSONDecodeError:
        # Wrap raw text as extracted content
        print(json.dumps({"extracted_text": result.strip()}))
except Exception as e:
    print(f"Extraction failed: {e}", file=sys.stderr)
    sys.exit(1)
`, g.buildLLMCallFunction(), collectUpstreamInputs(), prompt, systemPrompt,
		model, maxTokens, temperature)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildMCPAgentScript generates a script that calls Agent Builder for MCP tool-enabled execution.
// When mcpConnectionId is provided in the config, it is passed to Agent Builder so that
// MCP tool invocations use the specified saved connection (resolved via aether-be proxy).
func (g *ArgoGenerator) buildMCPAgentScript(config map[string]interface{}, model, prompt, systemPrompt string, maxTokens int) map[string]interface{} {
	agentBuilderURL := "http://agent-builder.tas-agent-builder.svc.cluster.local:8083"

	// Extract optional connection context from workflow node config
	mcpConnectionID, _ := config["mcpConnectionId"].(string)
	mcpServerID, _ := config["mcpServerId"].(string)

	// Build the payload JSON with optional connection fields
	connectionPayloadFragment := ""
	if mcpConnectionID != "" && mcpServerID != "" {
		connectionPayloadFragment = fmt.Sprintf(`
    "mcp_connection_id": %q,
    "mcp_server_id": %q,`, mcpConnectionID, mcpServerID)
	}

	source := fmt.Sprintf(`import json, urllib.request, sys, os
# Collect upstream inputs
upstream_inputs = []
for i in range(20):
    val = os.environ.get(f'INPUT_{i}', '')
    if val:
        upstream_inputs.append(val)

user_prompt = %q
if upstream_inputs:
    context = '\n---\n'.join(upstream_inputs)
    user_prompt = user_prompt + '\n\nContext from previous steps:\n' + context

# Call Agent Builder execute endpoint with MCP tools enabled
payload = {
    "prompt": user_prompt,
    "system_prompt": %q,
    "model": %q,
    "max_tokens": %d,%s
    "document_context": {
        "strategy": "mcp"
    }
}

url = %q + "/api/v1/agents/execute"
data = json.dumps(payload).encode("utf-8")
req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})

try:
    with urllib.request.urlopen(req, timeout=300) as resp:
        result = json.loads(resp.read().decode("utf-8"))
        content = result.get("response", result.get("content", ""))
        print(content)
except Exception as e:
    print(f"MCP Agent call failed: {e}", file=sys.stderr)
    sys.exit(1)
`, prompt, systemPrompt, model, maxTokens, connectionPayloadFragment, agentBuilderURL)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildRAGScript generates a script that queries DeepLake for relevant context
// before calling the LLM with the augmented prompt (Retrieval-Augmented Generation)
func (g *ArgoGenerator) buildRAGScript(model, prompt, systemPrompt string, maxTokens int, temperature float64, dataset string, topK int) map[string]interface{} {
	if dataset == "" {
		dataset = "default"
	}

	source := fmt.Sprintf(`import json, urllib.request, sys, os
%s
%s
user_query = %q
if upstream_inputs:
    user_query = user_query + '\n' + '\n'.join(upstream_inputs)

# Step 1: Query DeepLake for relevant context
deeplake_url = %q
dataset = %q
top_k = %d

search_payload = {
    "query": user_query,
    "dataset": dataset,
    "top_k": top_k
}

retrieved_context = ""
try:
    search_data = json.dumps(search_payload).encode("utf-8")
    search_req = urllib.request.Request(
        deeplake_url + "/api/v1/search",
        data=search_data,
        headers={"Content-Type": "application/json"}
    )
    with urllib.request.urlopen(search_req, timeout=30) as resp:
        search_result = json.loads(resp.read().decode("utf-8"))
        chunks = search_result.get("results", [])
        if chunks:
            retrieved_context = '\n\n---\n\n'.join(
                c.get("text", c.get("content", "")) for c in chunks
            )
except Exception as e:
    print(f"DeepLake search warning (continuing without RAG): {e}", file=sys.stderr)

# Step 2: Build augmented prompt with retrieved context
if retrieved_context:
    augmented_prompt = f"Use the following retrieved context to answer the query.\n\nContext:\n{retrieved_context}\n\nQuery: {user_query}"
else:
    augmented_prompt = user_query

# Step 3: Call LLM with augmented prompt
try:
    result = call_llm(%q, %q, augmented_prompt, %d, %g)
    print(result)
except Exception as e:
    print(f"RAG LLM call failed: {e}", file=sys.stderr)
    sys.exit(1)
`, g.buildLLMCallFunction(), collectUpstreamInputs(), prompt,
		g.deeplakeURL, dataset, topK,
		model, systemPrompt, maxTokens, temperature)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildScriptSpec creates an Argo script template spec
func buildScriptSpec(config map[string]interface{}) map[string]interface{} {
	spec := map[string]interface{}{}

	language, _ := config["language"].(string)
	image, _ := config["image"].(string)
	source, _ := config["source"].(string)

	// Set image based on language if not specified
	if image == "" {
		switch language {
		case "python":
			image = "python:3.11-slim"
		case "bash":
			image = "alpine:3.19"
		case "node":
			image = "node:20-slim"
		default:
			image = "python:3.11-slim"
		}
	}
	spec["image"] = image

	// Set command based on language
	switch language {
	case "python":
		spec["command"] = []string{"python"}
	case "bash":
		spec["command"] = []string{"sh"}
	case "node":
		spec["command"] = []string{"node"}
	default:
		spec["command"] = []string{"python"}
	}

	spec["source"] = source

	return spec
}

// buildHTTPSpec creates an Argo HTTP template spec
func buildHTTPSpec(config map[string]interface{}) map[string]interface{} {
	spec := map[string]interface{}{}

	if url, ok := config["url"].(string); ok {
		spec["url"] = url
	}
	if method, ok := config["method"].(string); ok {
		spec["method"] = method
	} else {
		spec["method"] = "GET"
	}

	if headers, ok := config["headers"].([]interface{}); ok && len(headers) > 0 {
		httpHeaders := []interface{}{}
		for _, h := range headers {
			if hMap, ok := h.(map[string]interface{}); ok {
				httpHeaders = append(httpHeaders, map[string]interface{}{
					"name":  hMap["name"],
					"value": hMap["value"],
				})
			}
		}
		spec["headers"] = httpHeaders
	}

	if body, ok := config["body"].(string); ok && body != "" {
		spec["body"] = body
	}

	if sc, ok := config["successCondition"].(string); ok && sc != "" {
		spec["successCondition"] = sc
	}

	return spec
}

// buildSuspendSpec creates an Argo suspend template spec
func buildSuspendSpec(config map[string]interface{}) map[string]interface{} {
	spec := map[string]interface{}{}

	if duration, ok := config["duration"].(string); ok && duration != "" {
		spec["duration"] = duration
	}

	return spec
}

// buildConditionScript creates a script that evaluates a condition expression
// and outputs "true" or "false". Downstream tasks reference this output via
// `when: {{tasks.condition-name.outputs.result}} == true`.
func buildConditionScript(config map[string]interface{}) map[string]interface{} {
	expression, _ := config["expression"].(string)
	condType, _ := config["conditionType"].(string)

	var source string
	switch condType {
	case "expression", "":
		// Argo-style expression: evaluate with Python for numeric/string comparisons
		if expression != "" {
			source = fmt.Sprintf(`import os, json, sys
expr = """%s"""
# Replace Argo template refs with env var values (injected by upstream params)
for k, v in os.environ.items():
    if k.startswith("INPUT_"):
        expr = expr.replace("{{tasks." + k.lower() + ".outputs.result}}", v)
try:
    result = eval(expr)
    print("true" if result else "false")
except Exception as e:
    print("false", file=sys.stderr)
    print("false")
`, expression)
		} else {
			source = `print("true")`
		}
		return map[string]interface{}{
			"image":   "python:3.11-slim",
			"command": []string{"python", "-c"},
			"source":  source,
		}
	default:
		// Legacy field/value comparison
		field, _ := config["field"].(string)
		value, _ := config["value"].(string)
		source = fmt.Sprintf(`import os, json, sys
field = "%s"
expected = "%s"
cond_type = "%s"
# Read upstream input from env vars
input_val = os.environ.get("INPUT_0", "")
try:
    data = json.loads(input_val)
    actual = str(data.get(field, ""))
except:
    actual = input_val
if cond_type == "equals":
    result = actual == expected
elif cond_type == "contains":
    result = expected in actual
elif cond_type == "greater_than":
    result = float(actual) > float(expected)
elif cond_type == "less_than":
    result = float(actual) < float(expected)
else:
    result = actual == expected
print("true" if result else "false")
`, field, value, condType)
		return map[string]interface{}{
			"image":   "python:3.11-slim",
			"command": []string{"python", "-c"},
			"source":  source,
		}
	}
}

// buildTransformScript creates a lightweight script template for transform nodes
func buildTransformScript(config map[string]interface{}) map[string]interface{} {
	expression, _ := config["expression"].(string)
	transformType, _ := config["transformType"].(string)

	var source string
	switch transformType {
	case "jsonpath":
		query, _ := config["jsonpathQuery"].(string)
		source = fmt.Sprintf("import json, sys\nfrom jsonpath_ng.ext import parse\nexpr = parse('%s')\ndata = json.load(sys.stdin)\nresult = [m.value for m in expr.find(data)]\nprint(json.dumps(result))", query)
	case "lua":
		luaScript, _ := config["luaScript"].(string)
		source = fmt.Sprintf("echo '%s' | lua -", luaScript)
		return map[string]interface{}{
			"image":   "alpine:3.19",
			"command": []string{"sh", "-c"},
			"source":  source,
		}
	default:
		// Expression or sprig — output expression tag for Argo controller
		if expression != "" {
			source = fmt.Sprintf("echo '%s'", expression)
		} else {
			source = "echo 'no-transform-configured'"
		}
		return map[string]interface{}{
			"image":   "alpine:3.19",
			"command": []string{"sh", "-c"},
			"source":  source,
		}
	}

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildAssemblerScript creates a Python script that assembles upstream step outputs.
// Supports assembly_mode: concat, merge, ai_summarize and output_format: json, text, markdown
func (g *ArgoGenerator) buildAssemblerScript(config map[string]interface{}) map[string]interface{} {
	assemblyMode, _ := config["assembly_mode"].(string)
	outputFormat, _ := config["output_format"].(string)
	instructions, _ := config["instructions"].(string)
	templateName, _ := config["template_name"].(string)

	if assemblyMode == "" {
		assemblyMode = "concat"
	}
	if outputFormat == "" {
		outputFormat = "markdown"
	}

	assemblerMCPURL := os.Getenv("ASSEMBLER_MCP_URL")
	if assemblerMCPURL == "" {
		assemblerMCPURL = "http://assembler-mcp.tas-mcp-servers.svc.cluster.local:8091"
	}

	// Build Python script that assembles upstream outputs into a document.
	// Strategy:
	//   1. If template_name is set → call Assembler MCP to render the template
	//   2. If mode is ai_summarize → call LLM Router to synthesize a coherent document
	//   3. Otherwise (concat/merge) → inline assembly
	source := fmt.Sprintf(`import json, urllib.request, os, sys

# Read upstream step outputs from Argo input parameters (injected as env vars)
inputs = []
for i in range(20):
    val = os.environ.get(f'INPUT_{i}', '')
    if val:
        inputs.append(val)

if not inputs:
    inputs = ['No upstream outputs found']

mode = %q
output_format = %q
instructions = %q
template_name = %q
assembler_url = %q
router_url = os.environ.get('LLM_ROUTER_URL', %q)

# If a specific template is requested, call the Assembler MCP Server
if template_name:
    try:
        params = {}
        for i, inp in enumerate(inputs):
            params[f'Section{i+1}'] = inp
        if instructions:
            params['Instructions'] = instructions
        params['Title'] = os.environ.get('WORKFLOW_NAME', 'Assembled Document')

        call_payload = {
            "name": "assemble_document",
            "arguments": {
                "template_name": template_name,
                "params": params,
                "output_format": output_format,
            }
        }
        data = json.dumps(call_payload).encode("utf-8")
        req = urllib.request.Request(
            assembler_url + "/mcp/tools/call",
            data=data,
            headers={"Content-Type": "application/json"}
        )
        with urllib.request.urlopen(req, timeout=60) as resp:
            result = json.loads(resp.read().decode("utf-8"))
            content = result.get("content", [{}])
            if content and not result.get("isError"):
                print(content[0].get("text", ""))
                sys.exit(0)
            else:
                print(f"Assembler MCP error: {content[0].get('text', 'unknown')}", file=sys.stderr)
    except Exception as e:
        print(f"Assembler MCP unavailable ({e}), falling back to inline", file=sys.stderr)

# Assembly logic based on mode
if mode == 'ai_summarize':
    # Use LLM to synthesize all upstream inputs into a cohesive document
    combined = '\n\n--- Section ---\n\n'.join(inputs)
    system_msg = instructions or 'Combine the following sections into a well-structured, cohesive document.'
    system_msg = f"""You are a professional document assembler. Create a polished, well-formatted {output_format} document.

{system_msg}

Output the document directly with proper headings, sections, and formatting. Do NOT include meta-commentary about the assembly process."""

    payload = json.dumps({
        "model": "gpt-4o-mini",
        "messages": [
            {"role": "system", "content": system_msg},
            {"role": "user", "content": f"Here are the sections to assemble:\n\n{combined[:12000]}"}
        ],
        "max_tokens": 4000,
        "temperature": 0.3
    })
    req = urllib.request.Request(router_url,
        data=payload.encode(), headers={'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            data = json.loads(resp.read())
            result = data.get('choices', [{}])[0].get('message', {}).get('content', 'Assembly failed: no LLM response')
    except Exception as e:
        result = f'AI assembly failed: {e}\n\nRaw sections:\n\n' + combined
elif mode == 'concat':
    result = '\n---\n'.join(inputs)
elif mode == 'merge':
    merged = {}
    for inp in inputs:
        try:
            merged.update(json.loads(inp))
        except (json.JSONDecodeError, TypeError):
            merged[f'input_{inputs.index(inp)}'] = inp
    result = json.dumps(merged, indent=2)
else:
    result = '\n'.join(inputs)

print(result)
`, assemblyMode, outputFormat, instructions, templateName, assemblerMCPURL, g.llmRouterURL)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildSyncScript creates a script template that uses curl to deliver workflow results.
// Uses script+curl instead of Argo HTTP templates because HTTP templates require
// additional controller configuration. Supports target types: in_app, webhook, slack, email
func buildSyncScript(config map[string]interface{}) map[string]interface{} {
	targets, _ := config["targets"].([]interface{})

	// Default target: in_app notification
	targetType := "in_app"
	url := "http://aether-backend.aether-be.svc.cluster.local:8080/webhooks/workflow-complete"
	messageTemplate := ""

	if len(targets) > 0 {
		if firstTarget, ok := targets[0].(map[string]interface{}); ok {
			if t, ok := firstTarget["type"].(string); ok && t != "" {
				targetType = t
			}
			if u, ok := firstTarget["url"].(string); ok && u != "" {
				url = u
			}
			if m, ok := firstTarget["message_template"].(string); ok {
				messageTemplate = m
			}
		}
	}

	// Resolve target URL based on type
	curlURL := url
	switch targetType {
	case "in_app":
		curlURL = "http://aether-backend.aether-be.svc.cluster.local:8080/webhooks/workflow-complete"
	case "slack":
		if curlURL == "" {
			curlURL = "https://hooks.slack.com/services/PLACEHOLDER"
		}
	case "email":
		if curlURL == "" {
			curlURL = "http://email-service.tas-shared.svc.cluster.local:8080/send"
		}
	}

	// Build a Python script for robust JSON handling. Shell-based JSON escaping
	// breaks when LLM output contains special characters (pipes, quotes, ampersands).
	// Python's json.dumps() handles all edge cases correctly.
	source := fmt.Sprintf(`import json, os, sys, urllib.request

wf_id = os.environ.get("ARGO_WF_ID", "unknown")
wf_name = os.environ.get("ARGO_WF_NAME", "unknown")
exec_id = os.environ.get("ARGO_EXEC_ID", "unknown")
user_id = os.environ.get("ARGO_USER_ID", "unknown")
tenant_id = os.environ.get("ARGO_TENANT_ID", "unknown")
status = os.environ.get("ARGO_STATUS", "Succeeded")
upstream_result = os.environ.get("ARGO_UPSTREAM_RESULT", "")

# workflow.status is only resolved in onExit handlers
if not status or status == "Running" or "{{" in status:
    status = "Succeeded"

target_type = %q
url = %q

body = {
    "workflow_id": wf_id,
    "workflow_name": wf_name,
    "execution_id": exec_id,
    "user_id": user_id,
    "tenant_id": tenant_id,
    "status": status,
}

if target_type == "in_app":
    if upstream_result:
        body["result"] = upstream_result
elif target_type == "slack":
    msg = %q if %q else "Workflow *" + wf_name + "* completed with status: " + status
    body = {"text": msg}
elif target_type == "email":
    body = {"subject": "Workflow " + wf_name + " - " + status, "workflow_id": wf_id}

data = json.dumps(body).encode("utf-8")
print("Sending notification to " + target_type)
print("Body length: " + str(len(data)) + " bytes")

req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
try:
    with urllib.request.urlopen(req, timeout=30) as resp:
        resp_body = resp.read().decode("utf-8")
        print("Response (" + str(resp.status) + "): " + resp_body)
        print("Notification delivered successfully")
except urllib.error.HTTPError as e:
    print("Response (" + str(e.code) + "): " + e.read().decode("utf-8"), file=sys.stderr)
    print("Notification delivery failed with HTTP " + str(e.code), file=sys.stderr)
    sys.exit(1)
except Exception as e:
    print("Notification delivery failed: " + str(e), file=sys.stderr)
    sys.exit(1)
`, targetType, curlURL, messageTemplate, messageTemplate)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
		"env": []map[string]interface{}{
			{"name": "ARGO_WF_ID", "value": "{{workflow.parameters.workflow_id}}"},
			{"name": "ARGO_WF_NAME", "value": "{{workflow.parameters.workflow_name}}"},
			{"name": "ARGO_EXEC_ID", "value": "{{workflow.name}}"},
			{"name": "ARGO_USER_ID", "value": "{{workflow.parameters.user_id}}"},
			{"name": "ARGO_TENANT_ID", "value": "{{workflow.parameters.tenant_id}}"},
			{"name": "ARGO_STATUS", "value": "{{workflow.status}}"},
			{"name": "ARGO_UPSTREAM_RESULT", "value": "{{inputs.parameters.upstream-result}}"},
		},
	}
}

// buildIOSpec converts TemplateIO to Argo inputs/outputs spec
func buildIOSpec(io *models.TemplateIO) map[string]interface{} {
	spec := map[string]interface{}{}

	if len(io.Parameters) > 0 {
		params := []interface{}{}
		for _, p := range io.Parameters {
			param := map[string]interface{}{"name": p.Name}
			if p.Value != "" {
				param["value"] = p.Value
			}
			if p.Default != "" {
				param["default"] = p.Default
			}
			if p.ValueFrom != nil {
				vf := map[string]interface{}{}
				if p.ValueFrom.Path != "" {
					vf["path"] = p.ValueFrom.Path
				}
				if p.ValueFrom.Expression != "" {
					vf["expression"] = p.ValueFrom.Expression
				}
				if p.ValueFrom.Parameter != "" {
					vf["parameter"] = p.ValueFrom.Parameter
				}
				param["valueFrom"] = vf
			}
			params = append(params, param)
		}
		spec["parameters"] = params
	}

	if len(io.Artifacts) > 0 {
		artifacts := []interface{}{}
		for _, a := range io.Artifacts {
			artifact := map[string]interface{}{
				"name": a.Name,
				"path": a.Path,
			}
			if a.From != "" {
				artifact["from"] = a.From
			}
			artifacts = append(artifacts, artifact)
		}
		spec["artifacts"] = artifacts
	}

	return spec
}

// SubmitWorkflow submits an Argo Workflow CRD to the cluster
func (g *ArgoGenerator) SubmitWorkflow(ctx context.Context, crd map[string]interface{}) (string, error) {
	if !g.enabled {
		return "", fmt.Errorf("argo workflows integration is disabled")
	}

	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return "", fmt.Errorf("not running in K8s cluster: %w", err)
	}

	body, err := json.Marshal(crd)
	if err != nil {
		return "", fmt.Errorf("failed to marshal workflow CRD: %w", err)
	}

	url := fmt.Sprintf(
		"https://kubernetes.default.svc/apis/argoproj.io/v1alpha1/namespaces/%s/workflows",
		g.namespace,
	)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to submit workflow: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		g.logger.Error("Argo workflow submission failed",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(respBody)),
		)
		return "", fmt.Errorf("argo API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response to get workflow name
	var result struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse submission response: %w", err)
	}

	g.logger.Info("Argo workflow submitted",
		zap.String("name", result.Metadata.Name),
		zap.String("namespace", g.namespace),
	)

	return result.Metadata.Name, nil
}

// GenerateAndSubmit is a convenience method that generates and submits a workflow CRD
func (g *ArgoGenerator) GenerateAndSubmit(ctx context.Context, workflow *models.Workflow) (string, error) {
	crd, err := g.GenerateWorkflowCRD(workflow)
	if err != nil {
		return "", fmt.Errorf("failed to generate CRD: %w", err)
	}

	name, err := g.SubmitWorkflow(ctx, crd)
	if err != nil {
		return "", fmt.Errorf("failed to submit workflow: %w", err)
	}

	return name, nil
}

// ArgoNodeStatus represents the status of a single node/task in an Argo workflow
type ArgoNodeStatus struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Phase       string `json:"phase"` // Pending, Running, Succeeded, Failed, Skipped, Error, Omitted
	StartedAt   string `json:"startedAt,omitempty"`
	FinishedAt  string `json:"finishedAt,omitempty"`
	Message     string `json:"message,omitempty"`
	Type        string `json:"type"` // Pod, DAG, Steps, etc.
}

// ArgoWorkflowStatus represents the overall status of an Argo workflow
type ArgoWorkflowStatus struct {
	Phase      string                      `json:"phase"` // Pending, Running, Succeeded, Failed, Error
	StartedAt  string                      `json:"startedAt,omitempty"`
	FinishedAt string                      `json:"finishedAt,omitempty"`
	Message    string                      `json:"message,omitempty"`
	Nodes      map[string]ArgoNodeStatus   `json:"nodes,omitempty"`
}

// GetArgoWorkflowStatus queries the K8s API for the live status of an Argo workflow
func (g *ArgoGenerator) GetArgoWorkflowStatus(ctx context.Context, workflowName string) (*ArgoWorkflowStatus, error) {
	if !g.enabled {
		return nil, fmt.Errorf("argo workflows integration is disabled")
	}

	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, fmt.Errorf("not running in K8s cluster: %w", err)
	}

	url := fmt.Sprintf(
		"https://kubernetes.default.svc/apis/argoproj.io/v1alpha1/namespaces/%s/workflows/%s",
		g.namespace, workflowName,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+string(token))

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query workflow status: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("argo API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response — extract .status
	var raw struct {
		Status struct {
			Phase      string `json:"phase"`
			StartedAt  string `json:"startedAt"`
			FinishedAt string `json:"finishedAt"`
			Message    string `json:"message"`
			Nodes      map[string]struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Phase       string `json:"phase"`
				StartedAt   string `json:"startedAt"`
				FinishedAt  string `json:"finishedAt"`
				Message     string `json:"message"`
				Type        string `json:"type"`
			} `json:"nodes"`
		} `json:"status"`
	}

	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse workflow status: %w", err)
	}

	result := &ArgoWorkflowStatus{
		Phase:      raw.Status.Phase,
		StartedAt:  raw.Status.StartedAt,
		FinishedAt: raw.Status.FinishedAt,
		Message:    raw.Status.Message,
		Nodes:      make(map[string]ArgoNodeStatus),
	}

	for k, v := range raw.Status.Nodes {
		result.Nodes[k] = ArgoNodeStatus{
			Name:        v.Name,
			DisplayName: v.DisplayName,
			Phase:       v.Phase,
			StartedAt:   v.StartedAt,
			FinishedAt:  v.FinishedAt,
			Message:     v.Message,
			Type:        v.Type,
		}
	}

	return result, nil
}

// buildMergeScript creates a Python script that combines multiple upstream outputs.
// Supports modes: append (concat all), combine-by-position (zip), combine-by-field (join), choose-branch
func buildMergeScript(config map[string]interface{}) map[string]interface{} {
	mergeMode, _ := config["merge_mode"].(string)
	if mergeMode == "" {
		mergeMode = "append"
	}

	combineField, _ := config["combine_field"].(string)
	chooseBranch := 0
	if cb, ok := config["choose_branch"].(float64); ok {
		chooseBranch = int(cb)
	}

	source := fmt.Sprintf(`import json, os, sys

# Collect upstream inputs
inputs = []
for i in range(20):
    val = os.environ.get(f'INPUT_{i}', '')
    if val:
        inputs.append(val)

if not inputs:
    print('No inputs to merge')
    sys.exit(0)

mode = %q
combine_field = %q
choose_branch = %d

if mode == 'append':
    # Concatenate all inputs
    result = '\n---\n'.join(inputs)
elif mode == 'combine-by-position':
    # Zip arrays from inputs into combined items
    arrays = []
    for inp in inputs:
        try:
            arr = json.loads(inp)
            if isinstance(arr, list):
                arrays.append(arr)
            else:
                arrays.append([arr])
        except (json.JSONDecodeError, TypeError):
            arrays.append([inp])
    max_len = max(len(a) for a in arrays) if arrays else 0
    combined = []
    for i in range(max_len):
        item = {}
        for j, arr in enumerate(arrays):
            if i < len(arr):
                if isinstance(arr[i], dict):
                    item.update(arr[i])
                else:
                    item[f'input_{j}'] = arr[i]
        combined.append(item)
    result = json.dumps(combined, indent=2)
elif mode == 'combine-by-field':
    # Join objects by a common field
    groups = {}
    for inp in inputs:
        try:
            items = json.loads(inp)
            if not isinstance(items, list):
                items = [items]
            for item in items:
                key = str(item.get(combine_field, 'unknown'))
                if key not in groups:
                    groups[key] = {}
                groups[key].update(item)
        except (json.JSONDecodeError, TypeError):
            pass
    result = json.dumps(list(groups.values()), indent=2)
elif mode == 'choose-branch':
    # Select output from a specific branch
    idx = min(choose_branch, len(inputs) - 1)
    result = inputs[idx]
else:
    result = '\n'.join(inputs)

print(result)
`, mergeMode, combineField, chooseBranch)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildLoopScript creates a script template for loop iteration.
// The loop body is handled by Argo's native withParam fan-out.
// This script parses the input array and outputs it as JSON for withParam.
func buildLoopScript(config map[string]interface{}) map[string]interface{} {
	batchSize := 0
	if bs, ok := config["batch_size"].(float64); ok && bs > 0 {
		batchSize = int(bs)
	}

	source := fmt.Sprintf(`import json, os, sys

# Read input array from upstream
input_data = os.environ.get('INPUT_0', '[]')
try:
    items = json.loads(input_data)
    if not isinstance(items, list):
        items = [items]
except (json.JSONDecodeError, TypeError):
    items = [input_data]

batch_size = %d
if batch_size > 0:
    # Create batches
    batches = [items[i:i+batch_size] for i in range(0, len(items), batch_size)]
    print(json.dumps(batches))
else:
    print(json.dumps(items))
`, batchSize)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
	}
}

// buildErrorHandlerScript creates a script for error handling nodes.
// This runs as an onExit handler or error recovery path.
func buildErrorHandlerScript(config map[string]interface{}) map[string]interface{} {
	action, _ := config["action"].(string)
	if action == "" {
		action = "log"
	}

	notifyURL, _ := config["notify_url"].(string)
	if notifyURL == "" {
		notifyURL = "http://aether-backend.aether-be.svc.cluster.local:8080/webhooks/workflow-complete"
	}

	source := fmt.Sprintf(`import json, os, sys, urllib.request

action = %q
notify_url = %q

wf_id = os.environ.get("ARGO_WF_ID", "unknown")
wf_name = os.environ.get("ARGO_WF_NAME", "unknown")
status = os.environ.get("ARGO_STATUS", "Failed")
error_msg = os.environ.get("ARGO_ERROR", "Unknown error")

print(f"Error handler triggered for workflow {wf_name}: {status}")
print(f"Error: {error_msg}")

if action == 'notify':
    body = json.dumps({
        "workflow_id": wf_id,
        "workflow_name": wf_name,
        "status": "Failed",
        "message": f"Error handler: {error_msg}",
        "user_id": os.environ.get("ARGO_USER_ID", ""),
        "tenant_id": os.environ.get("ARGO_TENANT_ID", ""),
    }).encode("utf-8")
    req = urllib.request.Request(notify_url, data=body, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            print(f"Error notification sent: {resp.status}")
    except Exception as e:
        print(f"Failed to send error notification: {e}", file=sys.stderr)
elif action == 'retry':
    print("Retry action requested (handled by Argo retryStrategy)")
else:
    print("Error logged. No additional action taken.")
`, action, notifyURL)

	return map[string]interface{}{
		"image":   "python:3.11-slim",
		"command": []string{"python"},
		"source":  source,
		"env": []map[string]interface{}{
			{"name": "ARGO_WF_ID", "value": "{{workflow.parameters.workflow_id}}"},
			{"name": "ARGO_WF_NAME", "value": "{{workflow.parameters.workflow_name}}"},
			{"name": "ARGO_USER_ID", "value": "{{workflow.parameters.user_id}}"},
			{"name": "ARGO_TENANT_ID", "value": "{{workflow.parameters.tenant_id}}"},
			{"name": "ARGO_STATUS", "value": "{{workflow.status}}"},
		},
	}
}

// inferTemplateType maps legacy step types to Argo template types
func inferTemplateType(stepType string) string {
	switch stepType {
	case "container":
		return "container"
	case "script":
		return "script"
	case "http":
		return "http"
	case "suspend":
		return "suspend"
	case "transform":
		return "data"
	case "condition":
		return "condition"
	case "aiTask":
		return "container"
	// Legacy types
	case "process_document", "ai_analysis", "custom", "assemble_output":
		return "container"
	case "compliance_check":
		return "container"
	case "approval":
		return "suspend"
	case "notification":
		return "http"
	case "assembler":
		return "script"
	case "sync":
		return "http"
	case "merge":
		return "script"
	case "loop":
		return "script"
	case "subworkflow":
		return "container"
	case "errorHandler":
		return "script"
	default:
		return "container"
	}
}

// sanitizeName converts a human-readable name to a valid Argo task/template name
func sanitizeName(name string) string {
	result := strings.ToLower(name)
	result = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		return -1
	}, result)

	// Remove leading/trailing hyphens and collapse consecutive hyphens
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")

	if result == "" {
		result = "step"
	}

	return result
}
