package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// RendererResult represents the result from a renderer post-processing step
type RendererResult struct {
	URL      string                 `json:"url"`
	Metadata map[string]interface{} `json:"metadata"`
}

// ProductionService handles production-related business logic
type ProductionService struct {
	neo4j           *database.Neo4jClient
	storageService  *S3StorageService
	agentService    *AgentService
	notebookService *NotebookService
	teamService     *TeamService
	spaceService    *SpaceService
	audiModal       *AudiModalService
	podcastMCP      *MCPClientService
	logger          *logger.Logger
}

// NewProductionService creates a new production service
func NewProductionService(
	neo4j *database.Neo4jClient,
	storageService *S3StorageService,
	agentService *AgentService,
	notebookService *NotebookService,
	teamService *TeamService,
	spaceService *SpaceService,
	audiModal *AudiModalService,
	podcastMCP *MCPClientService,
	log *logger.Logger,
) *ProductionService {
	return &ProductionService{
		neo4j:           neo4j,
		storageService:  storageService,
		agentService:    agentService,
		notebookService: notebookService,
		teamService:     teamService,
		spaceService:    spaceService,
		audiModal:       audiModal,
		podcastMCP:      podcastMCP,
		logger:          log.WithService("production_service"),
	}
}

// GetNotebookProducers returns available producer agents for a notebook
// This includes both internal (system) producer agents and user-created producer agents
func (s *ProductionService) GetNotebookProducers(ctx context.Context, notebookID, userID, authToken string, spaceCtx *models.SpaceContext) ([]*models.AgentResponse, error) {
	// Verify notebook exists and user has access (supports cross-space via SHARED_WITH)
	notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
	if err != nil {
		return nil, err
	}

	// Get internal producer agents from agent-builder (these are always available)
	internalProducers, err := s.agentService.GetInternalProducerAgents(ctx, authToken)
	if err != nil {
		s.logger.Error("Failed to get internal producer agents from agent-builder", zap.Error(err))
		// Don't fail completely - continue with user producers
		internalProducers = []*models.AgentResponse{}
	}

	s.logger.Debug("Retrieved internal producer agents",
		zap.Int("count", len(internalProducers)),
	)

	// Get all user-created producer agents accessible to this user/space
	// Use notebook's own tenant/space to find agents in the notebook owner's space
	query := `
		MATCH (a:Agent)
		WHERE a.type = 'producer'
			AND a.status = 'published'
			AND a.tenant_id = $tenant_id
			AND (
				a.space_id = $space_id
				OR a.is_public = true
			)
		OPTIONAL MATCH (a)-[:OWNED_BY]->(owner:User)
		RETURN a.id, a.agent_builder_id, a.name, a.description, a.status, a.type,
		       a.owner_id, a.space_type, a.space_id, a.tenant_id, a.team_id,
		       a.is_public, a.is_template, a.tags, a.created_at, a.updated_at,
		       owner.username, owner.full_name, owner.avatar_url
		ORDER BY a.name ASC
	`

	params := map[string]interface{}{
		"tenant_id": notebook.TenantID,
		"space_id":  notebook.SpaceID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get notebook producers from Neo4j", zap.Error(err))
		return nil, errors.Database("Failed to get notebook producers", err)
	}

	// Parse user-created producer agents from Neo4j
	userProducers := make([]*models.AgentResponse, 0, len(result.Records))
	for _, record := range result.Records {
		agent, err := s.recordToAgentResponse(record)
		if err != nil {
			s.logger.Error("Failed to parse agent record", zap.Error(err))
			continue
		}
		userProducers = append(userProducers, agent)
	}

	s.logger.Debug("Retrieved user-created producer agents",
		zap.Int("count", len(userProducers)),
	)

	// Merge: internal agents first, then user-created agents
	allProducers := make([]*models.AgentResponse, 0, len(internalProducers)+len(userProducers))
	allProducers = append(allProducers, internalProducers...)
	allProducers = append(allProducers, userProducers...)

	s.logger.Info("Returning all producer agents for notebook",
		zap.String("notebook_id", notebookID),
		zap.Int("internal_count", len(internalProducers)),
		zap.Int("user_count", len(userProducers)),
		zap.Int("total_count", len(allProducers)),
	)

	return allProducers, nil
}

// ExecuteProducer executes a producer agent on a notebook and creates a production
func (s *ProductionService) ExecuteProducer(ctx context.Context, notebookID string, req models.ProducerExecuteRequest, userID string, userTeams []string, authToken string, spaceCtx *models.SpaceContext) (*models.ProductionResponse, error) {
	// Verify notebook exists and user has access
	notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
	if err != nil {
		return nil, err
	}

	var agent *models.AgentResponse
	var isInternalAgent bool

	// Check if this is an internal producer agent first (from agent-builder)
	internalAgent, err := s.agentService.GetInternalAgentByID(ctx, req.AgentID, authToken)
	if err != nil {
		s.logger.Warn("Error checking for internal agent, trying user agent",
			zap.Error(err),
			zap.String("agent_id", req.AgentID),
		)
	}

	if internalAgent != nil {
		// Verify internal agent is a producer type
		if internalAgent.Type != models.AgentTypeProducer {
			return nil, errors.BadRequestWithDetails("Agent is not a producer type", map[string]interface{}{
				"agent_id":   req.AgentID,
				"agent_type": internalAgent.Type,
			})
		}
		agent = internalAgent
		isInternalAgent = true
		s.logger.Info("Using internal producer agent",
			zap.String("agent_id", agent.ID),
			zap.String("agent_name", agent.Name),
		)
	} else {
		// Get the agent using the agent service (from Neo4j)
		agent, err = s.agentService.GetAgent(ctx, req.AgentID, userID, userTeams, "")
		if err != nil {
			return nil, errors.NotFoundWithDetails("Agent not found", map[string]interface{}{
				"agent_id": req.AgentID,
			})
		}

		// Verify agent is a producer type
		if agent.Type != models.AgentTypeProducer {
			return nil, errors.BadRequestWithDetails("Agent is not a producer type", map[string]interface{}{
				"agent_id":   req.AgentID,
				"agent_type": agent.Type,
			})
		}

		// Verify agent is published
		if agent.Status != models.AgentStatusPublished {
			return nil, errors.BadRequestWithDetails("Agent is not published", map[string]interface{}{
				"agent_id":     req.AgentID,
				"agent_status": agent.Status,
			})
		}
		isInternalAgent = false
	}

	// Create production record in processing status
	// Use the notebook's space context so production is stored alongside notebook's other productions
	productionSpaceCtx := spaceCtx
	if notebook.TenantID != spaceCtx.TenantID || notebook.SpaceID != spaceCtx.SpaceID {
		productionSpaceCtx = &models.SpaceContext{
			SpaceType: notebook.SpaceType,
			SpaceID:   notebook.SpaceID,
			TenantID:  notebook.TenantID,
			UserID:    spaceCtx.UserID,
			UserRole:  spaceCtx.UserRole,
		}
	}
	production := models.NewProduction(req, agent.ID, agent.AgentBuilderID, notebookID, userID, productionSpaceCtx)

	// Store production in Neo4j
	if err := s.createProduction(ctx, production); err != nil {
		return nil, err
	}

	// Execute the agent asynchronously (in a goroutine)
	// For now, we do it synchronously for simplicity
	startTime := time.Now()

	var output string
	var tokensUsed int
	var costUSD float64
	var executionID string

	if isInternalAgent {
		// Execute internal agent directly via LLM router
		output, err = s.executeInternalProducerAgent(ctx, agent, notebook, req, authToken, spaceCtx)
		if err != nil {
			// Mark production as failed
			production.MarkFailed(err.Error())
			if updateErr := s.updateProduction(ctx, production); updateErr != nil {
				s.logger.Error("Failed to update production status to failed", zap.Error(updateErr))
			}
			return nil, errors.InternalWithCause("Internal agent execution failed", err)
		}
		executionID = production.ID // Use production ID as execution ID for internal agents
		// Note: tokensUsed and costUSD will be 0 for internal agents as we don't track them yet
	} else {
		// Build the execute request for user-created agents
		formatStr := string(req.Format)
		executeReq := models.AgentExecuteRequest{
			Input:        fmt.Sprintf("Generate a %s for notebook: %s", req.Type, notebook.Name),
			Context:      req.Context,
			Sources:      []string{notebookID},
			OutputFormat: &formatStr,
		}

		if len(req.SourceDocuments) > 0 {
			if executeReq.Context == nil {
				executeReq.Context = make(map[string]interface{})
			}
			executeReq.Context["document_ids"] = req.SourceDocuments
		}

		// Execute the agent via agent service
		executeResp, execErr := s.agentService.ExecuteAgent(ctx, agent.ID, executeReq, userID, userTeams, authToken)
		if execErr != nil {
			// Mark production as failed
			production.MarkFailed(execErr.Error())
			if updateErr := s.updateProduction(ctx, production); updateErr != nil {
				s.logger.Error("Failed to update production status to failed", zap.Error(updateErr))
			}
			return nil, errors.InternalWithCause("Agent execution failed", execErr)
		}
		output = executeResp.Output
		tokensUsed = executeResp.TokensUsed
		costUSD = executeResp.CostUSD
		executionID = executeResp.ExecutionID
	}

	responseTimeMs := int(time.Since(startTime).Milliseconds())

	// Generate content from agent output
	content := output
	contentBytes := []byte(content)

	// Generate filename based on type and format
	filename := fmt.Sprintf("%s_%s.%s", req.Type, time.Now().Format("2006-01-02"), getFileExtension(req.Format))
	storageKey := models.BuildProductionStorageKey(string(spaceCtx.SpaceType), notebookID, production.ID, filename)

	// Upload content to S3
	contentType := getContentType(req.Format)
	_, err = s.storageService.UploadFileToTenantBucket(ctx, spaceCtx.TenantID, storageKey, contentBytes, contentType)
	if err != nil {
		s.logger.Error("Failed to upload production content to S3", zap.Error(err))
		production.MarkFailed("Failed to store production content: " + err.Error())
		if updateErr := s.updateProduction(ctx, production); updateErr != nil {
			s.logger.Error("Failed to update production status", zap.Error(updateErr))
		}
		return nil, errors.InternalWithCause("Failed to store production content", err)
	}

	// Check if renderer is requested — if so, mark as "rendering" and run async
	rendererID, hasRenderer := req.Context["renderer_id"].(string)
	hasRenderer = hasRenderer && rendererID != ""

	if hasRenderer {
		// Mark production as completed for the text part, but note rendering is pending
		production.RendererID = rendererID
		production.MarkCompleted(
			storageKey,
			fmt.Sprintf("aether-%s", extractTenantSuffix(spaceCtx.TenantID)),
			int64(len(contentBytes)),
			executionID,
			tokensUsed,
			costUSD,
			responseTimeMs,
		)
		// Override status to "rendering" so frontend knows media is being generated
		production.Status = models.ProductionStatusRendering

		if err := s.updateProduction(ctx, production); err != nil {
			s.logger.Error("Failed to update production status", zap.Error(err))
			return nil, err
		}

		// Create relationships
		if err := s.createProductionRelationships(ctx, production, spaceCtx.TenantID); err != nil {
			s.logger.Error("Failed to create production relationships", zap.Error(err))
		}

		s.logger.Info("Production text saved, starting async renderer",
			zap.String("production_id", production.ID),
			zap.String("renderer_id", rendererID),
		)

		// Run renderer asynchronously
		go func() {
			bgCtx := context.Background()
			mediaResult, renderErr := s.executeRenderer(bgCtx, rendererID, output, req)
			if renderErr != nil {
				s.logger.Error("Async renderer failed",
					zap.String("production_id", production.ID),
					zap.String("renderer_id", rendererID),
					zap.Error(renderErr))
				production.Status = models.ProductionStatusCompleted
				production.ErrorMessage = "Renderer failed: " + renderErr.Error()
			} else {
				s.logger.Info("Async renderer completed successfully",
					zap.String("production_id", production.ID),
					zap.String("media_url", mediaResult.URL),
				)
				production.MediaURL = mediaResult.URL
				production.MediaMetadata = mediaResult.Metadata
				production.Status = models.ProductionStatusCompleted
			}
			if updateErr := s.updateProduction(bgCtx, production); updateErr != nil {
				s.logger.Error("Failed to update production after rendering",
					zap.String("production_id", production.ID),
					zap.Error(updateErr))
			}
		}()

		response := production.ToResponse()
		response.Content = content
		return response, nil
	}

	// No renderer — mark as completed immediately
	production.MarkCompleted(
		storageKey,
		fmt.Sprintf("aether-%s", extractTenantSuffix(spaceCtx.TenantID)),
		int64(len(contentBytes)),
		executionID,
		tokensUsed,
		costUSD,
		responseTimeMs,
	)

	// Update production in Neo4j
	if err := s.updateProduction(ctx, production); err != nil {
		s.logger.Error("Failed to update production status", zap.Error(err))
		return nil, err
	}

	// Create relationships
	if err := s.createProductionRelationships(ctx, production, spaceCtx.TenantID); err != nil {
		s.logger.Error("Failed to create production relationships", zap.Error(err))
		// Don't fail the whole operation for relationship errors
	}

	s.logger.Info("Production created successfully",
		zap.String("production_id", production.ID),
		zap.String("notebook_id", notebookID),
		zap.String("agent_id", agent.ID),
		zap.String("type", string(req.Type)),
	)

	response := production.ToResponse()
	response.Content = content // Include content in response
	return response, nil
}

// executeInternalProducerAgent executes an internal producer agent via agent-builder
// This leverages agent-builder's document context retrieval capabilities
func (s *ProductionService) executeInternalProducerAgent(
	ctx context.Context,
	agent *models.AgentResponse,
	notebook *models.Notebook,
	req models.ProducerExecuteRequest,
	authToken string,
	spaceCtx *models.SpaceContext,
) (string, error) {
	// Fetch document content directly from Neo4j extracted_text
	// This ensures the LLM always has the source material regardless of AudiModal chunk availability
	documentContent, docErr := s.getNotebookDocumentContent(ctx, notebook.ID, notebook.TenantID, req.SourceDocuments)
	if docErr != nil {
		s.logger.Warn("Failed to get document content from Neo4j, proceeding without it",
			zap.String("notebook_id", notebook.ID),
			zap.Error(docErr))
	}

	// Build the user message with document content included directly
	userMessage := s.buildProducerUserMessage(notebook, req, documentContent)

	// Resolve Aether tenant ID to AudiModal UUID
	// The notebook has an internal tenant ID (e.g., "tenant_1766596584") that needs to be
	// resolved to an AudiModal UUID. This may create a new AudiModal tenant if one doesn't exist.
	var audimodalTenantID string
	if s.audiModal != nil {
		resolvedUUID, err := s.audiModal.GetAudiModalTenantUUID(ctx, notebook.TenantID)
		if err != nil {
			s.logger.Warn("Failed to resolve AudiModal tenant UUID, falling back to extracting from tenant ID",
				zap.String("notebook_tenant_id", notebook.TenantID),
				zap.Error(err))
			// Fallback: extract UUID from tenant_<UUID> format if it's already a UUID
			audimodalTenantID = extractTenantSuffix(notebook.TenantID)
		} else {
			audimodalTenantID = resolvedUUID
			s.logger.Debug("Resolved AudiModal tenant UUID",
				zap.String("notebook_tenant_id", notebook.TenantID),
				zap.String("audimodal_tenant_uuid", audimodalTenantID))
		}
	} else {
		// No AudiModalService available - try to extract UUID from tenant ID
		audimodalTenantID = extractTenantSuffix(notebook.TenantID)
		s.logger.Warn("AudiModalService not available, using extracted tenant ID",
			zap.String("audimodal_tenant_id", audimodalTenantID))
	}

	// Get AudiModal file IDs for documents in the notebook
	// Agent-builder needs these file IDs to fetch chunks from AudiModal
	var audimodalFileIDs []string
	if len(req.SourceDocuments) > 0 {
		// User specified specific documents - get their AudiModal file IDs
		audimodalFileIDs, _ = s.getAudiModalFileIDs(ctx, notebook.TenantID, req.SourceDocuments)
	} else {
		// No specific documents - get all documents from the notebook
		audimodalFileIDs, _ = s.getNotebookAudiModalFileIDs(ctx, notebook.ID, notebook.TenantID)
	}

	s.logger.Debug("Retrieved AudiModal file IDs for producer execution",
		zap.String("notebook_id", notebook.ID),
		zap.Int("file_id_count", len(audimodalFileIDs)),
		zap.Strings("file_ids", audimodalFileIDs))

	// Build the execution request for agent-builder
	// This passes notebook IDs and file IDs so agent-builder can retrieve document context
	executeReq := InternalAgentExecuteRequest{
		Input:       userMessage,
		NotebookIDs: []string{notebook.ID},
		TenantID:    audimodalTenantID,
	}

	// Add AudiModal file IDs so agent-builder can fetch chunks
	if len(audimodalFileIDs) > 0 {
		executeReq.SelectedDocuments = audimodalFileIDs
	}

	s.logger.Info("Executing internal producer agent via agent-builder",
		zap.String("agent_id", agent.ID),
		zap.String("agent_name", agent.Name),
		zap.String("notebook_id", notebook.ID),
		zap.String("audimodal_tenant_id", audimodalTenantID),
		zap.String("internal_tenant_id", notebook.TenantID),
		zap.Int("source_documents", len(req.SourceDocuments)),
	)

	// Execute via agent-builder which will:
	// 1. Retrieve document context from DeepLake/AudiModal based on notebook IDs
	// 2. Inject the context into the system prompt
	// 3. Call the LLM with the enriched prompt
	response, err := s.agentService.ExecuteInternalAgent(ctx, agent.ID, executeReq, authToken)
	if err != nil {
		s.logger.Error("Failed to execute internal producer agent via agent-builder",
			zap.String("agent_id", agent.ID),
			zap.String("agent_name", agent.Name),
			zap.Error(err))
		return "", err
	}

	s.logger.Info("Internal producer agent executed successfully via agent-builder",
		zap.String("agent_id", agent.ID),
		zap.String("agent_name", agent.Name),
		zap.String("notebook_id", notebook.ID),
		zap.String("execution_id", response.ExecutionID),
		zap.Int("output_length", len(response.Output)),
		zap.Int("tokens_used", response.TokensUsed),
	)

	return response.Output, nil
}

// executeRenderer post-processes a producer's text output through a renderer
// Currently supports the podcast renderer via podcast-mcp
func (s *ProductionService) executeRenderer(ctx context.Context, rendererID, textOutput string, req models.ProducerExecuteRequest) (*RendererResult, error) {
	rendererType, _ := req.Context["renderer_type"].(string)

	switch rendererType {
	case "podcast":
		return s.executePodcastRenderer(ctx, textOutput, req)
	default:
		return nil, fmt.Errorf("unsupported renderer type: %s", rendererType)
	}
}

// executePodcastRenderer calls podcast-mcp to generate audio from a script
func (s *ProductionService) executePodcastRenderer(ctx context.Context, script string, req models.ProducerExecuteRequest) (*RendererResult, error) {
	if s.podcastMCP == nil || !s.podcastMCP.IsEnabled() {
		return nil, fmt.Errorf("podcast MCP service is not available")
	}

	// Extract renderer config from context
	ttsProvider, _ := req.Context["tts_provider"].(string)
	if ttsProvider == "" {
		ttsProvider = "kokoro"
	}

	speakers, _ := req.Context["speakers"].(string)
	if speakers == "" {
		speakers = "Alex, Sam"
	}

	voiceMapping, _ := req.Context["voice_mapping"].(map[string]interface{})

	// voice_mapping is required by the podcast MCP — build defaults from speakers if not provided
	if voiceMapping == nil || len(voiceMapping) == 0 {
		voiceMapping = map[string]interface{}{}
		// Assign default voices based on provider
		var defaultVoices []string
		if ttsProvider == "kokoro" {
			defaultVoices = []string{"af_heart", "am_adam", "af_bella", "am_michael"}
		} else {
			defaultVoices = []string{"Rachel", "Adam", "Domi", "Josh"}
		}
		for i, speaker := range strings.Split(speakers, ",") {
			name := strings.TrimSpace(speaker)
			if name == "" {
				continue
			}
			voiceIdx := i % len(defaultVoices)
			voiceMapping[name] = defaultVoices[voiceIdx]
		}
	}

	introMusicKey, _ := req.Context["intro_music_key"].(string)
	outroMusicKey, _ := req.Context["outro_music_key"].(string)
	ambientMusicKey, _ := req.Context["ambient_music_key"].(string)

	// Extract podcast duration (minutes) from context
	podcastDuration := 10 // default 10 minutes
	if dur, ok := req.Context["podcast_duration"].(float64); ok && dur > 0 {
		podcastDuration = int(dur)
	}

	// Build MCP tool arguments
	args := map[string]interface{}{
		"script":        script,
		"provider":      ttsProvider,
		"title":         req.Title,
		"voice_mapping": voiceMapping,
		"config": map[string]interface{}{
			"intro_music_key":   introMusicKey,
			"outro_music_key":   outroMusicKey,
			"ambient_music_key": ambientMusicKey,
			"silence_gap_ms":    500,
			"target_duration":   podcastDuration,
		},
	}

	s.logger.Info("Invoking podcast renderer via MCP",
		zap.String("provider", ttsProvider),
		zap.String("speakers", speakers),
		zap.String("title", req.Title),
	)

	// Call podcast-mcp generate_podcast tool
	resp, err := s.podcastMCP.InvokeTool(ctx, "generate_podcast", args)
	if err != nil {
		return nil, fmt.Errorf("podcast MCP call failed: %w", err)
	}

	if resp.IsError {
		errText := "unknown error"
		if len(resp.Content) > 0 {
			errText = resp.Content[0].Text
		}
		return nil, fmt.Errorf("podcast generation failed: %s", errText)
	}

	// Parse the response - expect JSON with podcast_url, duration, etc.
	if len(resp.Content) == 0 {
		return nil, fmt.Errorf("empty response from podcast MCP")
	}

	var podcastResult map[string]interface{}
	if err := json.Unmarshal([]byte(resp.Content[0].Text), &podcastResult); err != nil {
		// If not JSON, treat the text as a URL
		return &RendererResult{
			URL: resp.Content[0].Text,
			Metadata: map[string]interface{}{
				"provider": ttsProvider,
				"speakers": speakers,
			},
		}, nil
	}

	// Extract URL and metadata from the result
	podcastURL, _ := podcastResult["podcast_url"].(string)
	if podcastURL == "" {
		podcastURL, _ = podcastResult["url"].(string)
	}

	metadata := map[string]interface{}{
		"provider": ttsProvider,
		"speakers": speakers,
	}
	if duration, ok := podcastResult["duration"]; ok {
		metadata["duration"] = duration
	}
	if segments, ok := podcastResult["segments"]; ok {
		metadata["segments"] = segments
	}

	s.logger.Info("Podcast rendered successfully",
		zap.String("url", podcastURL),
		zap.String("provider", ttsProvider),
	)

	return &RendererResult{
		URL:      podcastURL,
		Metadata: metadata,
	}, nil
}

// ListRenderers returns available renderer workflows (workflows with type='renderer' and status='active')
func (s *ProductionService) ListRenderers(ctx context.Context) ([]map[string]interface{}, error) {
	query := `
		MATCH (w:Workflow {type: 'renderer', status: 'active'})
		RETURN w.id AS id, w.name AS name, w.description AS description,
			   w.type AS type, w.configuration AS configuration,
			   w.created_at AS created_at
		ORDER BY w.name
	`

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query renderer workflows: %w", err)
	}

	renderers := make([]map[string]interface{}, 0)
	for _, record := range result.Records {
		renderer := map[string]interface{}{}
		if v, ok := record.Get("id"); ok && v != nil {
			renderer["id"] = fmt.Sprintf("%v", v)
		}
		if v, ok := record.Get("name"); ok && v != nil {
			renderer["name"] = fmt.Sprintf("%v", v)
		}
		if v, ok := record.Get("description"); ok && v != nil {
			renderer["description"] = fmt.Sprintf("%v", v)
		}
		if v, ok := record.Get("type"); ok && v != nil {
			renderer["type"] = fmt.Sprintf("%v", v)
		}
		if config, ok := record.Get("configuration"); ok && config != nil {
			renderer["configuration"] = config
		}
		if createdAt, ok := record.Get("created_at"); ok {
			renderer["createdAt"] = createdAt
		}
		renderers = append(renderers, renderer)
	}

	return renderers, nil
}

// getNotebookDocumentContent retrieves the extracted text content from documents in a notebook
func (s *ProductionService) getNotebookDocumentContent(ctx context.Context, notebookID, tenantID string, sourceDocumentIDs []string) (string, error) {
	var query string
	params := map[string]interface{}{
		"notebook_id": notebookID,
		"tenant_id":   tenantID,
	}

	// If specific source documents are requested, only get those
	if len(sourceDocumentIDs) > 0 {
		query = `
			MATCH (d:Document {tenant_id: $tenant_id})
			WHERE d.id IN $document_ids AND d.extracted_text IS NOT NULL AND d.extracted_text <> ''
			RETURN d.id as id, d.name as name, d.extracted_text as content
			ORDER BY d.name
		`
		params["document_ids"] = sourceDocumentIDs
	} else {
		// Get all documents in the notebook
		query = `
			MATCH (n:Notebook {id: $notebook_id, tenant_id: $tenant_id})-[:CONTAINS]->(d:Document {tenant_id: $tenant_id})
			WHERE d.extracted_text IS NOT NULL AND d.extracted_text <> ''
			RETURN d.id as id, d.name as name, d.extracted_text as content
			ORDER BY d.name
		`
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return "", fmt.Errorf("failed to query documents: %w", err)
	}

	if len(result.Records) == 0 {
		s.logger.Info("No documents with extracted text found",
			zap.String("notebook_id", notebookID),
			zap.Int("source_docs_requested", len(sourceDocumentIDs)),
		)
		return "", nil
	}

	// Build combined content from all documents
	var contentBuilder strings.Builder
	contentBuilder.WriteString("=== DOCUMENT CONTENT ===\n\n")

	for i, record := range result.Records {
		name := ""
		content := ""

		if record.Values[1] != nil {
			name = fmt.Sprintf("%v", record.Values[1])
		}
		if record.Values[2] != nil {
			content = fmt.Sprintf("%v", record.Values[2])
		}

		if content != "" {
			contentBuilder.WriteString(fmt.Sprintf("--- Document %d: %s ---\n", i+1, name))
			contentBuilder.WriteString(content)
			contentBuilder.WriteString("\n\n")
		}
	}

	contentBuilder.WriteString("=== END OF DOCUMENTS ===\n")

	s.logger.Debug("Built document content",
		zap.Int("document_count", len(result.Records)),
		zap.Int("total_content_length", contentBuilder.Len()),
	)

	return contentBuilder.String(), nil
}

// processingResultJSON represents the structure of the processing_result JSON string
type processingResultJSON struct {
	AudiModalFileID string `json:"audimodal_file_id"`
}

// getNotebookAudiModalFileIDs retrieves AudiModal file IDs for all processed documents in a notebook
// Note: processing_result is stored as a JSON string in Neo4j, so we need to parse it in Go
// Note: Documents are linked to notebooks via notebook_id property, not CONTAINS relationship
func (s *ProductionService) getNotebookAudiModalFileIDs(ctx context.Context, notebookID, tenantID string) ([]string, error) {
	// Query for documents with processing_result (stored as JSON string)
	// Documents are linked via notebook_id property and filtered by status
	query := `
		MATCH (d:Document {notebook_id: $notebook_id, tenant_id: $tenant_id})
		WHERE d.processing_result IS NOT NULL
		  AND d.status IN ['processed', 'ready']
		RETURN d.id as id, d.name as name, d.processing_result as processing_result, d.status as status
		ORDER BY d.name
	`
	params := map[string]any{
		"notebook_id": notebookID,
		"tenant_id":   tenantID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to query documents for AudiModal file IDs",
			zap.String("notebook_id", notebookID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to query documents: %w", err)
	}

	s.logger.Info("Found documents in notebook",
		zap.String("notebook_id", notebookID),
		zap.Int("document_count", len(result.Records)))

	var fileIDs []string
	for _, record := range result.Records {
		docName := fmt.Sprintf("%v", record.Values[1])
		processingResultStr := fmt.Sprintf("%v", record.Values[2])
		status := fmt.Sprintf("%v", record.Values[3])

		// Parse the processing_result JSON string to extract audimodal_file_id
		var pr processingResultJSON
		if err := json.Unmarshal([]byte(processingResultStr), &pr); err != nil {
			s.logger.Debug("Failed to parse processing_result JSON",
				zap.String("document_name", docName),
				zap.String("status", status),
				zap.Error(err))
			continue
		}

		if pr.AudiModalFileID != "" {
			fileIDs = append(fileIDs, pr.AudiModalFileID)
			s.logger.Debug("Found AudiModal file ID for document",
				zap.String("document_name", docName),
				zap.String("audimodal_file_id", pr.AudiModalFileID),
				zap.String("status", status))
		} else {
			s.logger.Debug("Document has no audimodal_file_id in processing_result",
				zap.String("document_name", docName),
				zap.String("status", status))
		}
	}

	s.logger.Info("Retrieved AudiModal file IDs from notebook",
		zap.String("notebook_id", notebookID),
		zap.Int("total_documents", len(result.Records)),
		zap.Int("documents_with_file_ids", len(fileIDs)))

	return fileIDs, nil
}

// getAudiModalFileIDs retrieves AudiModal file IDs for specific document IDs
// Note: processing_result is stored as a JSON string in Neo4j, so we need to parse it in Go
func (s *ProductionService) getAudiModalFileIDs(ctx context.Context, tenantID string, documentIDs []string) ([]string, error) {
	query := `
		MATCH (d:Document {tenant_id: $tenant_id})
		WHERE d.id IN $document_ids
		  AND d.processing_result IS NOT NULL
		RETURN d.id as id, d.name as name, d.processing_result as processing_result, d.status as status
		ORDER BY d.name
	`
	params := map[string]any{
		"tenant_id":    tenantID,
		"document_ids": documentIDs,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to query specific documents for AudiModal file IDs",
			zap.Int("document_count", len(documentIDs)),
			zap.Error(err))
		return nil, fmt.Errorf("failed to query documents: %w", err)
	}

	var fileIDs []string
	for _, record := range result.Records {
		docName := fmt.Sprintf("%v", record.Values[1])
		processingResultStr := fmt.Sprintf("%v", record.Values[2])
		status := fmt.Sprintf("%v", record.Values[3])

		// Parse the processing_result JSON string to extract audimodal_file_id
		var pr processingResultJSON
		if err := json.Unmarshal([]byte(processingResultStr), &pr); err != nil {
			s.logger.Debug("Failed to parse processing_result JSON for document",
				zap.String("document_name", docName),
				zap.String("status", status),
				zap.Error(err))
			continue
		}

		if pr.AudiModalFileID != "" {
			fileIDs = append(fileIDs, pr.AudiModalFileID)
			s.logger.Debug("Found AudiModal file ID for specific document",
				zap.String("document_name", docName),
				zap.String("audimodal_file_id", pr.AudiModalFileID),
				zap.String("status", status))
		}
	}

	s.logger.Info("Retrieved AudiModal file IDs for specific documents",
		zap.Int("requested_documents", len(documentIDs)),
		zap.Int("found_documents", len(result.Records)),
		zap.Int("documents_with_file_ids", len(fileIDs)))

	return fileIDs, nil
}

// buildProducerUserMessage builds the user message for producer agents based on the request
// Note: documentContent parameter is kept for backwards compatibility but is typically empty
// when using agent-builder's document context injection (which happens at the system prompt level)
func (s *ProductionService) buildProducerUserMessage(notebook *models.Notebook, req models.ProducerExecuteRequest, documentContent string) string {
	var message string

	// Build message based on production type
	// Note: When using agent-builder, document content is injected into the system prompt,
	// so user messages must explicitly reference "the documents provided above in the context"
	// IMPORTANT: We must be very explicit that the LLM should use ONLY the document content,
	// not make up content based on the notebook name/description
	switch req.Type {
	case models.ProductionTypeSummary:
		message = "IMPORTANT: You MUST use ONLY the document content provided above in the '--- RELEVANT CONTEXT ---' section. Do NOT make up or infer content.\n\n"
		message += fmt.Sprintf("Generate a comprehensive summary of the documents in the notebook \"%s\".\n\n", notebook.Name)
		message += "Your summary must be based EXCLUSIVELY on the actual text content from the documents provided above. Include specific facts, figures, names, and details from those documents. Do not generate generic content."

	case models.ProductionTypeQA:
		message = "IMPORTANT: You MUST use ONLY the document content provided above in the '--- RELEVANT CONTEXT ---' section. Do NOT make up or infer content.\n\n"
		message += fmt.Sprintf("Generate question and answer pairs based on the documents in the notebook \"%s\".\n\n", notebook.Name)
		message += "All questions and answers must be derived EXCLUSIVELY from the actual text content in the documents provided above. Reference specific information from those documents."

	case models.ProductionTypeOutline:
		message = "IMPORTANT: You MUST use ONLY the document content provided above in the '--- RELEVANT CONTEXT ---' section. Do NOT make up or infer content.\n\n"
		message += fmt.Sprintf("Create a structured outline of the documents in the notebook \"%s\".\n\n", notebook.Name)
		message += "The outline must reflect the ACTUAL content and topics from the documents provided above. Organize the real information from those documents into a hierarchical structure with sections and key points."

	case models.ProductionTypeInsight:
		message = "IMPORTANT: You MUST use ONLY the document content provided above in the '--- RELEVANT CONTEXT ---' section. Do NOT make up or infer content.\n\n"
		message += fmt.Sprintf("Extract key insights and actionable takeaways from the documents in the notebook \"%s\".\n\n", notebook.Name)
		message += "All insights must be derived EXCLUSIVELY from the actual text content in the documents provided above. Cite specific evidence from those documents."

	case models.ProductionTypePodcast:
		// Extract requested duration from context (default 10 minutes)
		podcastDuration := 10
		if dur, ok := req.Context["podcast_duration"].(float64); ok && dur > 0 {
			podcastDuration = int(dur)
		}
		// TTS speech rate is ~130 words per minute; use 160 wpm target to account for
		// stage directions and pauses that don't produce audio
		targetWords := podcastDuration * 160

		message = "IMPORTANT: You MUST use ONLY the document content provided above in the '--- RELEVANT CONTEXT ---' section. Do NOT make up or infer content.\n\n"
		message += fmt.Sprintf("Generate a multi-speaker podcast script based on the documents in the notebook \"%s\".\n\n", notebook.Name)
		message += fmt.Sprintf("TARGET LENGTH: The script MUST be AT LEAST %d words long (for a %d-minute podcast). ", targetWords, podcastDuration)
		message += fmt.Sprintf("Count carefully — you need roughly %d lines of dialogue with 15-25 words each. ", targetWords/20)
		message += "This is CRITICAL — a script that is too short will produce an incomplete podcast. Err on the side of being LONGER rather than shorter.\n\n"
		message += "Use the screenplay format: 'SpeakerName: dialogue' on each line. Default speakers are Alex and Sam.\n"
		message += "Include stage directions in parentheses like (laughs), (excited), (thoughtful pause).\n"
		message += "Make the conversation natural with back-and-forth dialogue, reactions, questions, and insights.\n"
		message += "Reference specific facts, quotes, and data from the source documents.\n"
		message += "Structure: Introduction → Main discussion (multiple subtopics with deep exploration) → Key takeaways → Conclusion/sign-off.\n"
		message += "IMPORTANT: Explore each topic in depth with examples, analogies, and follow-up questions. Do NOT rush through topics.\n"
		message += "Output ONLY the screenplay text with no markdown headers or metadata."

	default:
		message = "IMPORTANT: You MUST use ONLY the document content provided above in the '--- RELEVANT CONTEXT ---' section. Do NOT make up or infer content.\n\n"
		message += fmt.Sprintf("Analyze the documents in the notebook \"%s\" and generate content of type \"%s\".\n\n", notebook.Name, req.Type)
		message += "Base your response EXCLUSIVELY on the actual document content provided above."
	}

	// Add format instruction if specified
	// Skip for podcast type — the podcast renderer requires screenplay format regardless
	if req.Format != "" && req.Type != models.ProductionTypePodcast {
		message += fmt.Sprintf("\n\nPlease format the output as %s.", req.Format)
	}

	// Add custom title if provided
	if req.Title != "" {
		message += fmt.Sprintf("\n\nTitle the output: \"%s\"", req.Title)
	}

	// Include document content from Neo4j extracted_text
	// This provides the source material the LLM must use to generate the production
	if documentContent != "" {
		message += "\n\n--- RELEVANT CONTEXT ---\n"
		message += "Below is the content from the documents in this notebook. You MUST use this content as the basis for your response:\n\n"
		message += documentContent
		message += "\n--- END CONTEXT ---\n"
	}

	return message
}

// GetProductionByID retrieves a production by ID
func (s *ProductionService) GetProductionByID(ctx context.Context, productionID, userID string, spaceCtx *models.SpaceContext) (*models.Production, error) {
	// First try: match production in the user's current space
	query := `
		MATCH (p:Production {id: $production_id, tenant_id: $tenant_id})
		RETURN p.id, p.title, p.type, p.format, p.status,
		       p.agent_id, p.agent_builder_id, p.notebook_id, p.user_id,
		       p.space_type, p.space_id, p.tenant_id,
		       p.storage_bucket, p.storage_key, p.size_bytes,
		       p.execution_id, p.tokens_used, p.cost_usd, p.response_time_ms,
		       p.error_message, p.media_url, p.media_metadata, p.renderer_id,
		       p.source_documents, p.search_text,
		       p.created_at, p.updated_at
	`

	params := map[string]interface{}{
		"production_id": productionID,
		"tenant_id":     spaceCtx.TenantID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get production by ID", zap.String("production_id", productionID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve production", err)
	}

	if len(result.Records) > 0 {
		production, err := s.recordToProduction(result.Records[0])
		if err != nil {
			return nil, err
		}

		if production.SpaceID == spaceCtx.SpaceID && production.TenantID == spaceCtx.TenantID {
			if !spaceCtx.CanRead() {
				return nil, errors.Forbidden("Insufficient permissions to read production")
			}
			return production, nil
		}
	}

	// Second try: check if production's notebook is shared with the user (cross-space access)
	sharedQuery := `
		MATCH (p:Production {id: $production_id})
		MATCH (n:Notebook {id: p.notebook_id})-[:SHARED_WITH]->(u:User {id: $user_id})
		WHERE n.status = 'active'
		RETURN p.id, p.title, p.type, p.format, p.status,
		       p.agent_id, p.agent_builder_id, p.notebook_id, p.user_id,
		       p.space_type, p.space_id, p.tenant_id,
		       p.storage_bucket, p.storage_key, p.size_bytes,
		       p.execution_id, p.tokens_used, p.cost_usd, p.response_time_ms,
		       p.error_message, p.media_url, p.media_metadata, p.renderer_id,
		       p.source_documents, p.search_text,
		       p.created_at, p.updated_at
	`

	sharedResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, sharedQuery, map[string]interface{}{
		"production_id": productionID,
		"user_id":       userID,
	})
	if err != nil {
		s.logger.Error("Failed to check shared production access", zap.String("production_id", productionID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve production", err)
	}

	if len(sharedResult.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Production not found", map[string]interface{}{
			"production_id": productionID,
		})
	}

	production, err := s.recordToProduction(sharedResult.Records[0])
	if err != nil {
		return nil, err
	}

	return production, nil
}

// GetProductionContent retrieves the content of a production from S3
func (s *ProductionService) GetProductionContent(ctx context.Context, productionID, userID string, spaceCtx *models.SpaceContext) (string, error) {
	production, err := s.GetProductionByID(ctx, productionID, userID, spaceCtx)
	if err != nil {
		return "", err
	}

	if production.StorageKey == "" {
		return "", errors.NotFoundWithDetails("Production content not available", map[string]interface{}{
			"production_id": productionID,
			"status":        production.Status,
		})
	}

	// Download content from S3 using the production's own tenant (supports cross-space access)
	content, err := s.storageService.DownloadFileFromTenantBucket(ctx, production.TenantID, production.StorageKey)
	if err != nil {
		s.logger.Error("Failed to download production content", zap.Error(err))
		return "", errors.InternalWithCause("Failed to retrieve production content", err)
	}

	return string(content), nil
}

// ListNotebookProductions lists productions for a notebook
func (s *ProductionService) ListNotebookProductions(ctx context.Context, notebookID, userID string, spaceCtx *models.SpaceContext, limit, offset int) (*models.ProductionListResponse, error) {
	// Verify notebook exists and user has access (supports cross-space via SHARED_WITH)
	notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
	if err != nil {
		return nil, err
	}

	// Set defaults
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Use notebook's own tenant/space to find productions
	query := `
		MATCH (p:Production)
		WHERE p.notebook_id = $notebook_id
			AND p.tenant_id = $tenant_id
			AND p.space_id = $space_id
		RETURN p.id, p.title, p.type, p.format, p.status,
		       p.agent_id, p.agent_builder_id, p.notebook_id, p.user_id,
		       p.space_type, p.space_id, p.tenant_id,
		       p.storage_bucket, p.storage_key, p.size_bytes,
		       p.execution_id, p.tokens_used, p.cost_usd, p.response_time_ms,
		       p.error_message, p.media_url, p.media_metadata, p.renderer_id,
		       p.source_documents, p.created_at, p.updated_at
		ORDER BY p.created_at DESC
		SKIP $offset
		LIMIT $limit
	`

	params := map[string]interface{}{
		"notebook_id": notebookID,
		"tenant_id":   notebook.TenantID,
		"space_id":    notebook.SpaceID,
		"limit":       limit + 1,
		"offset":      offset,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to list notebook productions", zap.Error(err))
		return nil, errors.Database("Failed to list productions", err)
	}

	productions := make([]*models.ProductionResponse, 0, len(result.Records))
	hasMore := false

	for i, record := range result.Records {
		if i >= limit {
			hasMore = true
			break
		}

		production, err := s.recordToProduction(record)
		if err != nil {
			s.logger.Error("Failed to parse production record", zap.Error(err))
			continue
		}
		productions = append(productions, production.ToResponse())
	}

	// Get total count using notebook's own tenant/space
	countQuery := `
		MATCH (p:Production)
		WHERE p.notebook_id = $notebook_id
			AND p.tenant_id = $tenant_id
			AND p.space_id = $space_id
		RETURN count(p) as total
	`

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, map[string]interface{}{
		"notebook_id": notebookID,
		"tenant_id":   notebook.TenantID,
		"space_id":    notebook.SpaceID,
	})
	if err != nil {
		s.logger.Error("Failed to get production count", zap.Error(err))
		return nil, errors.Database("Failed to get production count", err)
	}

	total := 0
	if len(countResult.Records) > 0 {
		if totalValue, found := countResult.Records[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return &models.ProductionListResponse{
		Productions: productions,
		Total:       total,
		Limit:       limit,
		Offset:      offset,
		HasMore:     hasMore,
	}, nil
}

// DeleteProduction deletes a production
func (s *ProductionService) DeleteProduction(ctx context.Context, productionID, userID string, spaceCtx *models.SpaceContext) error {
	production, err := s.GetProductionByID(ctx, productionID, userID, spaceCtx)
	if err != nil {
		return err
	}

	// Check if user can delete
	if !spaceCtx.CanDelete() {
		return errors.Forbidden("Insufficient permissions to delete production")
	}

	// Delete from S3 first
	if production.StorageKey != "" {
		if err := s.storageService.DeleteFileFromTenantBucket(ctx, spaceCtx.TenantID, production.StorageKey); err != nil {
			s.logger.Warn("Failed to delete production content from S3", zap.Error(err))
			// Continue with Neo4j deletion even if S3 deletion fails
		}
	}

	// Delete from Neo4j
	query := `
		MATCH (p:Production {id: $production_id, tenant_id: $tenant_id})
		DETACH DELETE p
	`

	params := map[string]interface{}{
		"production_id": productionID,
		"tenant_id":     spaceCtx.TenantID,
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to delete production", zap.String("production_id", productionID), zap.Error(err))
		return errors.Database("Failed to delete production", err)
	}

	s.logger.Info("Production deleted successfully",
		zap.String("production_id", productionID),
		zap.String("title", production.Title),
	)

	return nil
}

// Helper methods

func (s *ProductionService) createProduction(ctx context.Context, production *models.Production) error {
	query := `
		CREATE (p:Production {
			id: $id,
			title: $title,
			type: $type,
			format: $format,
			status: $status,
			agent_id: $agent_id,
			agent_builder_id: $agent_builder_id,
			notebook_id: $notebook_id,
			user_id: $user_id,
			space_type: $space_type,
			space_id: $space_id,
			tenant_id: $tenant_id,
			storage_bucket: $storage_bucket,
			storage_key: $storage_key,
			size_bytes: $size_bytes,
			execution_id: $execution_id,
			tokens_used: $tokens_used,
			cost_usd: $cost_usd,
			response_time_ms: $response_time_ms,
			error_message: $error_message,
			media_url: $media_url,
			media_metadata: $media_metadata,
			renderer_id: $renderer_id,
			source_documents: $source_documents,
			search_text: $search_text,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN p
	`

	params := map[string]interface{}{
		"id":               production.ID,
		"title":            production.Title,
		"type":             string(production.Type),
		"format":           string(production.Format),
		"status":           string(production.Status),
		"agent_id":         production.AgentID,
		"agent_builder_id": production.AgentBuilderID,
		"notebook_id":      production.NotebookID,
		"user_id":          production.UserID,
		"space_type":       string(production.SpaceType),
		"space_id":         production.SpaceID,
		"tenant_id":        production.TenantID,
		"storage_bucket":   production.StorageBucket,
		"storage_key":      production.StorageKey,
		"size_bytes":       production.SizeBytes,
		"execution_id":     production.ExecutionID,
		"tokens_used":      production.TokensUsed,
		"cost_usd":         production.CostUSD,
		"response_time_ms": production.ResponseTimeMs,
		"error_message":    production.ErrorMessage,
		"media_url":        production.MediaURL,
		"media_metadata":   marshalJSONOrEmpty(production.MediaMetadata),
		"renderer_id":      production.RendererID,
		"source_documents": production.SourceDocuments,
		"search_text":      production.SearchText,
		"created_at":       production.CreatedAt.Format(time.RFC3339),
		"updated_at":       production.UpdatedAt.Format(time.RFC3339),
	}

	s.logger.Info("Creating production in Neo4j",
		zap.String("production_id", production.ID),
		zap.String("type", string(production.Type)),
		zap.String("tenant_id", production.TenantID),
	)

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to create production in Neo4j",
			zap.String("production_id", production.ID),
			zap.Error(err),
		)
		return errors.Database("Failed to create production", err)
	}

	s.logger.Info("Production created in Neo4j successfully",
		zap.String("production_id", production.ID),
	)

	return nil
}

func (s *ProductionService) updateProduction(ctx context.Context, production *models.Production) error {
	query := `
		MATCH (p:Production {id: $id, tenant_id: $tenant_id})
		SET p.title = $title,
		    p.status = $status,
		    p.storage_bucket = $storage_bucket,
		    p.storage_key = $storage_key,
		    p.size_bytes = $size_bytes,
		    p.execution_id = $execution_id,
		    p.tokens_used = $tokens_used,
		    p.cost_usd = $cost_usd,
		    p.response_time_ms = $response_time_ms,
		    p.error_message = $error_message,
		    p.media_url = $media_url,
		    p.media_metadata = $media_metadata,
		    p.renderer_id = $renderer_id,
		    p.search_text = $search_text,
		    p.updated_at = datetime($updated_at)
		RETURN p
	`

	params := map[string]interface{}{
		"id":               production.ID,
		"tenant_id":        production.TenantID,
		"title":            production.Title,
		"status":           string(production.Status),
		"storage_bucket":   production.StorageBucket,
		"storage_key":      production.StorageKey,
		"size_bytes":       production.SizeBytes,
		"execution_id":     production.ExecutionID,
		"tokens_used":      production.TokensUsed,
		"cost_usd":         production.CostUSD,
		"response_time_ms": production.ResponseTimeMs,
		"error_message":    production.ErrorMessage,
		"media_url":        production.MediaURL,
		"media_metadata":   marshalJSONOrEmpty(production.MediaMetadata),
		"renderer_id":      production.RendererID,
		"search_text":      production.SearchText,
		"updated_at":       production.UpdatedAt.Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update production", zap.Error(err))
		return errors.Database("Failed to update production", err)
	}

	return nil
}

// CleanupStaleProductions finds productions stuck in processing/rendering states
// for longer than the given threshold and marks them as failed. This handles
// cases where async goroutines were lost due to pod restarts.
func (s *ProductionService) CleanupStaleProductions(ctx context.Context, staleThreshold time.Duration) (int, error) {
	query := `
		MATCH (p:Production)
		WHERE p.status IN ['processing', 'rendering']
		  AND p.updated_at < datetime($cutoff)
		SET p.status = 'failed',
		    p.error_message = 'Stale production cleanup: processing was interrupted (likely by a service restart)',
		    p.updated_at = datetime()
		RETURN p.id AS id, p.title AS title, p.tenant_id AS tenant_id
	`

	cutoff := time.Now().UTC().Add(-staleThreshold).Format(time.RFC3339)
	params := map[string]interface{}{
		"cutoff": cutoff,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to cleanup stale productions", zap.Error(err))
		return 0, err
	}

	count := 0
	for _, record := range result.Records {
		id, _ := record.Get("id")
		title, _ := record.Get("title")
		s.logger.Warn("Cleaned up stale production",
			zap.Any("production_id", id),
			zap.Any("title", title),
		)
		count++
	}

	if count > 0 {
		s.logger.Info("Stale production cleanup completed",
			zap.Int("cleaned_up", count),
			zap.Duration("threshold", staleThreshold),
		)
	}

	return count, nil
}

func (s *ProductionService) createProductionRelationships(ctx context.Context, production *models.Production, tenantID string) error {
	// Create BELONGS_TO relationship to Notebook
	notebookRelQuery := `
		MATCH (p:Production {id: $production_id, tenant_id: $tenant_id}),
		      (n:Notebook {id: $notebook_id, tenant_id: $tenant_id})
		MERGE (p)-[r:BELONGS_TO]->(n)
		ON CREATE SET r.created_at = datetime()
	`
	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, notebookRelQuery, map[string]interface{}{
		"production_id": production.ID,
		"notebook_id":   production.NotebookID,
		"tenant_id":     tenantID,
	})
	if err != nil {
		s.logger.Warn("Failed to create BELONGS_TO relationship", zap.Error(err))
	}

	// Create CREATED_BY relationship to Agent
	agentRelQuery := `
		MATCH (p:Production {id: $production_id, tenant_id: $tenant_id}),
		      (a:Agent {id: $agent_id, tenant_id: $tenant_id})
		MERGE (p)-[r:CREATED_BY]->(a)
		ON CREATE SET r.created_at = datetime()
	`
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, agentRelQuery, map[string]interface{}{
		"production_id": production.ID,
		"agent_id":      production.AgentID,
		"tenant_id":     tenantID,
	})
	if err != nil {
		s.logger.Warn("Failed to create CREATED_BY relationship", zap.Error(err))
	}

	// Create OWNED_BY relationship to User
	userRelQuery := `
		MATCH (p:Production {id: $production_id, tenant_id: $tenant_id}),
		      (u:User {id: $user_id})
		MERGE (p)-[r:OWNED_BY]->(u)
		ON CREATE SET r.created_at = datetime()
	`
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, userRelQuery, map[string]interface{}{
		"production_id": production.ID,
		"user_id":       production.UserID,
		"tenant_id":     tenantID,
	})
	if err != nil {
		s.logger.Warn("Failed to create OWNED_BY relationship", zap.Error(err))
	}

	// Create SOURCES relationships to Documents if specified
	for _, docID := range production.SourceDocuments {
		docRelQuery := `
			MATCH (p:Production {id: $production_id, tenant_id: $tenant_id}),
			      (d:Document {id: $document_id, tenant_id: $tenant_id})
			MERGE (p)-[r:SOURCES]->(d)
			ON CREATE SET r.created_at = datetime()
		`
		_, err = s.neo4j.ExecuteQueryWithLogging(ctx, docRelQuery, map[string]interface{}{
			"production_id": production.ID,
			"document_id":   docID,
			"tenant_id":     tenantID,
		})
		if err != nil {
			s.logger.Warn("Failed to create SOURCES relationship", zap.String("document_id", docID), zap.Error(err))
		}
	}

	return nil
}

func (s *ProductionService) recordToProduction(record interface{}) (*models.Production, error) {
	neo4jRecord, ok := record.(*neo4j.Record)
	if !ok {
		return nil, errors.Internal("Invalid record type")
	}

	production := &models.Production{}

	// Extract fields
	if id, found := neo4jRecord.Get("p.id"); found && id != nil {
		production.ID = id.(string)
	}
	if title, found := neo4jRecord.Get("p.title"); found && title != nil {
		production.Title = title.(string)
	}
	if pType, found := neo4jRecord.Get("p.type"); found && pType != nil {
		production.Type = models.ProductionType(pType.(string))
	}
	if format, found := neo4jRecord.Get("p.format"); found && format != nil {
		production.Format = models.ProductionFormat(format.(string))
	}
	if status, found := neo4jRecord.Get("p.status"); found && status != nil {
		production.Status = models.ProductionStatus(status.(string))
	}
	if agentID, found := neo4jRecord.Get("p.agent_id"); found && agentID != nil {
		production.AgentID = agentID.(string)
	}
	if agentBuilderID, found := neo4jRecord.Get("p.agent_builder_id"); found && agentBuilderID != nil {
		production.AgentBuilderID = agentBuilderID.(string)
	}
	if notebookID, found := neo4jRecord.Get("p.notebook_id"); found && notebookID != nil {
		production.NotebookID = notebookID.(string)
	}
	if userID, found := neo4jRecord.Get("p.user_id"); found && userID != nil {
		production.UserID = userID.(string)
	}
	if spaceType, found := neo4jRecord.Get("p.space_type"); found && spaceType != nil {
		production.SpaceType = models.SpaceType(spaceType.(string))
	}
	if spaceID, found := neo4jRecord.Get("p.space_id"); found && spaceID != nil {
		production.SpaceID = spaceID.(string)
	}
	if tenantID, found := neo4jRecord.Get("p.tenant_id"); found && tenantID != nil {
		production.TenantID = tenantID.(string)
	}
	if storageBucket, found := neo4jRecord.Get("p.storage_bucket"); found && storageBucket != nil {
		production.StorageBucket = storageBucket.(string)
	}
	if storageKey, found := neo4jRecord.Get("p.storage_key"); found && storageKey != nil {
		production.StorageKey = storageKey.(string)
	}
	if sizeBytes, found := neo4jRecord.Get("p.size_bytes"); found && sizeBytes != nil {
		switch v := sizeBytes.(type) {
		case int64:
			production.SizeBytes = v
		case float64:
			production.SizeBytes = int64(v)
		}
	}
	if executionID, found := neo4jRecord.Get("p.execution_id"); found && executionID != nil {
		production.ExecutionID = executionID.(string)
	}
	if tokensUsed, found := neo4jRecord.Get("p.tokens_used"); found && tokensUsed != nil {
		switch v := tokensUsed.(type) {
		case int64:
			production.TokensUsed = int(v)
		case float64:
			production.TokensUsed = int(v)
		}
	}
	if costUSD, found := neo4jRecord.Get("p.cost_usd"); found && costUSD != nil {
		switch v := costUSD.(type) {
		case float64:
			production.CostUSD = v
		case int64:
			production.CostUSD = float64(v)
		}
	}
	if responseTimeMs, found := neo4jRecord.Get("p.response_time_ms"); found && responseTimeMs != nil {
		switch v := responseTimeMs.(type) {
		case int64:
			production.ResponseTimeMs = int(v)
		case float64:
			production.ResponseTimeMs = int(v)
		}
	}
	if errorMessage, found := neo4jRecord.Get("p.error_message"); found && errorMessage != nil {
		production.ErrorMessage = errorMessage.(string)
	}
	if mediaURL, found := neo4jRecord.Get("p.media_url"); found && mediaURL != nil {
		production.MediaURL = mediaURL.(string)
	}
	if mediaMetadata, found := neo4jRecord.Get("p.media_metadata"); found && mediaMetadata != nil {
		if metaStr, ok := mediaMetadata.(string); ok && metaStr != "" {
			var meta map[string]interface{}
			if err := json.Unmarshal([]byte(metaStr), &meta); err == nil {
				production.MediaMetadata = meta
			}
		}
	}
	if rendererID, found := neo4jRecord.Get("p.renderer_id"); found && rendererID != nil {
		production.RendererID = rendererID.(string)
	}
	if sourceDocuments, found := neo4jRecord.Get("p.source_documents"); found && sourceDocuments != nil {
		if docs, ok := sourceDocuments.([]interface{}); ok {
			for _, doc := range docs {
				if docStr, ok := doc.(string); ok {
					production.SourceDocuments = append(production.SourceDocuments, docStr)
				}
			}
		}
	}

	// Parse timestamps
	if createdAt, found := neo4jRecord.Get("p.created_at"); found && createdAt != nil {
		if timeStr, ok := createdAt.(string); ok {
			if parsed, err := time.Parse(time.RFC3339, timeStr); err == nil {
				production.CreatedAt = parsed
			}
		} else if neo4jTime, ok := createdAt.(time.Time); ok {
			production.CreatedAt = neo4jTime
		}
	}
	if updatedAt, found := neo4jRecord.Get("p.updated_at"); found && updatedAt != nil {
		if timeStr, ok := updatedAt.(string); ok {
			if parsed, err := time.Parse(time.RFC3339, timeStr); err == nil {
				production.UpdatedAt = parsed
			}
		} else if neo4jTime, ok := updatedAt.(time.Time); ok {
			production.UpdatedAt = neo4jTime
		}
	}

	return production, nil
}

func (s *ProductionService) recordToAgentResponse(record interface{}) (*models.AgentResponse, error) {
	neo4jRecord, ok := record.(*neo4j.Record)
	if !ok {
		return nil, errors.Internal("Invalid record type")
	}

	agent := &models.AgentResponse{}

	if id, found := neo4jRecord.Get("a.id"); found && id != nil {
		agent.ID = id.(string)
	}
	if agentBuilderID, found := neo4jRecord.Get("a.agent_builder_id"); found && agentBuilderID != nil {
		agent.AgentBuilderID = agentBuilderID.(string)
	}
	if name, found := neo4jRecord.Get("a.name"); found && name != nil {
		agent.Name = name.(string)
	}
	if description, found := neo4jRecord.Get("a.description"); found && description != nil {
		agent.Description = description.(string)
	}
	if status, found := neo4jRecord.Get("a.status"); found && status != nil {
		agent.Status = models.AgentStatus(status.(string))
	}
	if agentType, found := neo4jRecord.Get("a.type"); found && agentType != nil {
		agent.Type = models.AgentType(agentType.(string))
	}
	if ownerID, found := neo4jRecord.Get("a.owner_id"); found && ownerID != nil {
		agent.OwnerID = ownerID.(string)
	}
	if spaceType, found := neo4jRecord.Get("a.space_type"); found && spaceType != nil {
		agent.SpaceType = models.SpaceType(spaceType.(string))
	}
	if spaceID, found := neo4jRecord.Get("a.space_id"); found && spaceID != nil {
		agent.SpaceID = spaceID.(string)
	}
	if teamID, found := neo4jRecord.Get("a.team_id"); found && teamID != nil {
		agent.TeamID = teamID.(string)
	}
	if isPublic, found := neo4jRecord.Get("a.is_public"); found && isPublic != nil {
		agent.IsPublic = isPublic.(bool)
	}
	if isTemplate, found := neo4jRecord.Get("a.is_template"); found && isTemplate != nil {
		agent.IsTemplate = isTemplate.(bool)
	}
	if tags, found := neo4jRecord.Get("a.tags"); found && tags != nil {
		if tagSlice, ok := tags.([]interface{}); ok {
			for _, tag := range tagSlice {
				if tagStr, ok := tag.(string); ok {
					agent.Tags = append(agent.Tags, tagStr)
				}
			}
		}
	}

	// Parse timestamps
	if createdAt, found := neo4jRecord.Get("a.created_at"); found && createdAt != nil {
		if timeStr, ok := createdAt.(string); ok {
			if parsed, err := time.Parse(time.RFC3339, timeStr); err == nil {
				agent.CreatedAt = parsed
			}
		} else if neo4jTime, ok := createdAt.(time.Time); ok {
			agent.CreatedAt = neo4jTime
		}
	}
	if updatedAt, found := neo4jRecord.Get("a.updated_at"); found && updatedAt != nil {
		if timeStr, ok := updatedAt.(string); ok {
			if parsed, err := time.Parse(time.RFC3339, timeStr); err == nil {
				agent.UpdatedAt = parsed
			}
		} else if neo4jTime, ok := updatedAt.(time.Time); ok {
			agent.UpdatedAt = neo4jTime
		}
	}

	// Extract owner information
	ownerUsername, hasOwnerUsername := neo4jRecord.Get("owner.username")
	ownerFullName, hasOwnerFullName := neo4jRecord.Get("owner.full_name")
	ownerAvatarURL, hasOwnerAvatarURL := neo4jRecord.Get("owner.avatar_url")

	if hasOwnerUsername || hasOwnerFullName || hasOwnerAvatarURL {
		agent.Owner = &models.PublicUserResponse{
			ID: agent.OwnerID,
		}
		if hasOwnerUsername && ownerUsername != nil {
			agent.Owner.Username = ownerUsername.(string)
		}
		if hasOwnerFullName && ownerFullName != nil {
			agent.Owner.FullName = ownerFullName.(string)
		}
		if hasOwnerAvatarURL && ownerAvatarURL != nil {
			agent.Owner.AvatarURL = ownerAvatarURL.(string)
		}
	}

	return agent, nil
}

// Utility functions

func getFileExtension(format models.ProductionFormat) string {
	switch format {
	case models.ProductionFormatMarkdown:
		return "md"
	case models.ProductionFormatHTML:
		return "html"
	case models.ProductionFormatJSON:
		return "json"
	case models.ProductionFormatText:
		return "txt"
	default:
		return "txt"
	}
}

func getContentType(format models.ProductionFormat) string {
	switch format {
	case models.ProductionFormatMarkdown:
		return "text/markdown"
	case models.ProductionFormatHTML:
		return "text/html"
	case models.ProductionFormatJSON:
		return "application/json"
	case models.ProductionFormatText:
		return "text/plain"
	default:
		return "text/plain"
	}
}

// marshalJSONOrEmpty marshals a map to JSON string, or returns empty string if nil
func marshalJSONOrEmpty(m map[string]interface{}) string {
	if m == nil {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// NotebookChatResponse represents a response from notebook chat
type NotebookChatResponse struct {
	Content string `json:"content"`
}

// NotebookChatAssistantID is the ID of the internal notebook chat agent
const NotebookChatAssistantID = "00000000-0000-0000-0000-000000000009"

// ExecuteNotebookChat executes the internal Notebook Chat Assistant agent for a notebook
// This uses agent-builder to retrieve document context and inject it into the prompt
func (s *ProductionService) ExecuteNotebookChat(
	ctx context.Context,
	notebookID string,
	message string,
	history []models.ConversationMessage,
	userID string,
	authToken string,
	spaceCtx *models.SpaceContext,
) (*NotebookChatResponse, error) {
	// Verify notebook exists and user has access
	notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
	if err != nil {
		return nil, err
	}

	// Resolve Aether tenant ID to AudiModal UUID
	// This is needed for agent-builder to fetch document chunks from AudiModal
	var audimodalTenantID string
	if s.audiModal != nil {
		resolvedUUID, err := s.audiModal.GetAudiModalTenantUUID(ctx, notebook.TenantID)
		if err != nil {
			s.logger.Warn("Failed to resolve AudiModal tenant UUID for notebook chat",
				zap.String("notebook_tenant_id", notebook.TenantID),
				zap.Error(err))
			audimodalTenantID = extractTenantSuffix(notebook.TenantID)
		} else {
			audimodalTenantID = resolvedUUID
			s.logger.Debug("Resolved AudiModal tenant UUID for notebook chat",
				zap.String("notebook_tenant_id", notebook.TenantID),
				zap.String("audimodal_tenant_uuid", audimodalTenantID))
		}
	} else {
		audimodalTenantID = extractTenantSuffix(notebook.TenantID)
		s.logger.Warn("AudiModalService not available for notebook chat",
			zap.String("audimodal_tenant_id", audimodalTenantID))
	}

	// Get AudiModal file IDs for all documents in the notebook
	// Agent-builder needs these to fetch document chunks for context injection
	audimodalFileIDs, _ := s.getNotebookAudiModalFileIDs(ctx, notebook.ID, notebook.TenantID)

	s.logger.Debug("Retrieved AudiModal file IDs for notebook chat",
		zap.String("notebook_id", notebook.ID),
		zap.Int("file_id_count", len(audimodalFileIDs)),
		zap.Strings("file_ids", audimodalFileIDs))

	// Convert conversation history to services.ConversationMessage
	var serviceHistory []ConversationMessage
	for _, msg := range history {
		serviceHistory = append(serviceHistory, ConversationMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	// Build the execution request for agent-builder
	// This passes notebook IDs and file IDs so agent-builder can retrieve document context
	executeReq := InternalAgentExecuteRequest{
		Input:       message,
		NotebookIDs: []string{notebook.ID},
		TenantID:    audimodalTenantID,
		History:     serviceHistory,
	}

	// Add AudiModal file IDs so agent-builder can fetch document chunks
	if len(audimodalFileIDs) > 0 {
		executeReq.SelectedDocuments = audimodalFileIDs
	}

	s.logger.Info("Executing notebook chat via agent-builder",
		zap.String("notebook_id", notebook.ID),
		zap.String("audimodal_tenant_id", audimodalTenantID),
		zap.Int("history_length", len(history)),
		zap.Int("file_ids", len(audimodalFileIDs)),
		zap.String("user_id", userID),
	)

	// Execute via agent-builder which will:
	// 1. Retrieve document context from AudiModal based on file IDs
	// 2. Inject the context into the system prompt
	// 3. Call the LLM with the enriched prompt and conversation history
	response, err := s.agentService.ExecuteInternalAgent(ctx, NotebookChatAssistantID, executeReq, authToken)
	if err != nil {
		s.logger.Error("Failed to execute notebook chat via agent-builder",
			zap.String("notebook_id", notebookID),
			zap.Error(err))
		return nil, errors.Internal("Failed to execute chat request")
	}

	s.logger.Info("Notebook chat executed successfully via agent-builder",
		zap.String("notebook_id", notebookID),
		zap.String("user_id", userID),
		zap.String("execution_id", response.ExecutionID),
		zap.Int("output_length", len(response.Output)),
	)

	return &NotebookChatResponse{
		Content: response.Output,
	}, nil
}
