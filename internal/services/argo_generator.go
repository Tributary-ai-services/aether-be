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

// ArgoGenerator converts workflow definitions into Argo Workflow CRDs and submits them
type ArgoGenerator struct {
	namespace          string
	serviceAccountName string
	enabled            bool
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

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &ArgoGenerator{
		namespace:          namespace,
		serviceAccountName: saName,
		enabled:            enabled,
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

	spec := map[string]interface{}{
		"entrypoint":         "main",
		"serviceAccountName": g.serviceAccountName,
		"templates":          g.buildTemplates(mainSteps),
		"arguments": map[string]interface{}{
			"parameters": []map[string]interface{}{
				{"name": "workflow_id", "value": workflow.ID},
				{"name": "workflow_name", "value": workflow.Name},
				{"name": "user_id", "value": workflow.CreatedBy},
				{"name": "tenant_id", "value": workflow.TenantID},
			},
		},
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

		// For sync steps, auto-inject upstream task results as a parameter
		// so the webhook can include the actual workflow output
		if step.Type == "sync" && len(step.Dependencies) > 0 {
			// Use the last dependency's output (typically the final processing step)
			lastDep := step.Dependencies[len(step.Dependencies)-1]
			params = append(params, map[string]interface{}{
				"name":  "upstream-result",
				"value": fmt.Sprintf("{{tasks.%s.outputs.result}}", lastDep),
			})
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

	templateType := step.TemplateType
	if templateType == "" {
		templateType = inferTemplateType(step.Type)
	}

	// Special handling: if this is an aiTask with LLM config, build an LLM container
	if step.Type == "aiTask" {
		aiTaskType, _ := step.Configuration["aiTaskType"].(string)
		if aiTaskType == "llm" {
			tmpl["script"] = buildAITaskLLMScript(step.Configuration)
			return tmpl
		}
	}

	// Special handling: assembler step → script that assembles upstream outputs
	if step.Type == "assembler" {
		tmpl["script"] = buildAssemblerScript(step.Configuration)
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
		// Conditions are handled via `when` on DAG tasks, not as templates.
		// Create a pass-through script that evaluates and outputs the condition.
		tmpl["script"] = map[string]interface{}{
			"image":   "alpine:3.19",
			"command": []string{"sh", "-c"},
			"source":  "echo 'condition-evaluated'",
		}
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
// the step configuration.
func buildAITaskLLMScript(config map[string]interface{}) map[string]interface{} {
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

	// Build a Python script that calls the LLM Router with proper JSON handling
	// Using Python avoids shell escaping issues with complex prompts
	source := fmt.Sprintf(`import json, urllib.request, sys

payload = {
    "model": %q,
    "messages": [
        {"role": "system", "content": %q},
        {"role": "user", "content": %q}
    ],
    "max_tokens": %d,
    "temperature": %g
}

url = "http://llm-router.tas-llm-router.svc.cluster.local:8086/v1/chat/completions"
data = json.dumps(payload).encode("utf-8")
req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})

try:
    with urllib.request.urlopen(req, timeout=120) as resp:
        result = json.loads(resp.read().decode("utf-8"))
        content = result.get("choices", [{}])[0].get("message", {}).get("content", "")
        print(content)
except Exception as e:
    print(f"LLM call failed: {e}", file=sys.stderr)
    sys.exit(1)
`, model, systemPrompt, prompt, maxTokens, temperature)

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
func buildAssemblerScript(config map[string]interface{}) map[string]interface{} {
	assemblyMode, _ := config["assembly_mode"].(string)
	outputFormat, _ := config["output_format"].(string)
	instructions, _ := config["instructions"].(string)

	if assemblyMode == "" {
		assemblyMode = "concat"
	}
	if outputFormat == "" {
		outputFormat = "text"
	}

	// Build Python script that reads upstream outputs via Argo parameter artifacts
	source := fmt.Sprintf(`import json, os, sys, glob

# Read all upstream step outputs from Argo parameter files
inputs = []
for f in sorted(glob.glob('/tmp/inputs/*.txt')):
    with open(f) as fh:
        inputs.append(fh.read().strip())

# Also check environment for upstream outputs
for key, val in sorted(os.environ.items()):
    if key.startswith('UPSTREAM_'):
        inputs.append(val)

if not inputs:
    inputs = ['No upstream outputs found']

mode = '%s'
fmt = '%s'

if mode == 'concat':
    result = '\n---\n'.join(inputs)
elif mode == 'merge':
    merged = {}
    for inp in inputs:
        try:
            merged.update(json.loads(inp))
        except (json.JSONDecodeError, TypeError):
            merged[f'input_{inputs.index(inp)}'] = inp
    result = json.dumps(merged, indent=2)
elif mode == 'ai_summarize':
    # Calls TAS LLM Router to summarize
    import urllib.request
    combined = '\n---\n'.join(inputs)
    router_url = os.environ.get('LLM_ROUTER_URL', 'http://tas-llm-router.tas-llm-router.svc.cluster.local:8085')
    payload = json.dumps({
        "model": "gpt-4o-mini",
        "messages": [
            {"role": "system", "content": "Summarize the following outputs into a cohesive result. %s"},
            {"role": "user", "content": combined[:8000]}
        ],
        "max_tokens": 2000
    })
    req = urllib.request.Request(f'{router_url}/v1/chat/completions',
        data=payload.encode(), headers={'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            data = json.loads(resp.read())
            result = data.get('choices', [{}])[0].get('message', {}).get('content', 'No summary')
    except Exception as e:
        result = f'AI summarize failed: {e}\n\n' + combined
else:
    result = '\n'.join(inputs)

if fmt == 'json':
    try:
        parsed = json.loads(result)
        result = json.dumps(parsed, indent=2)
    except (json.JSONDecodeError, TypeError):
        result = json.dumps({"result": result})

print(result)
`, assemblyMode, outputFormat, instructions)

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
