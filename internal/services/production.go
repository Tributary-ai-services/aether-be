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

// ProductionService handles production-related business logic
type ProductionService struct {
	neo4j           *database.Neo4jClient
	storageService  *S3StorageService
	agentService    *AgentService
	notebookService *NotebookService
	teamService     *TeamService
	spaceService    *SpaceService
	audiModal       *AudiModalService
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
		logger:          log.WithService("production_service"),
	}
}

// GetNotebookProducers returns available producer agents for a notebook
// This includes both internal (system) producer agents and user-created producer agents
func (s *ProductionService) GetNotebookProducers(ctx context.Context, notebookID, userID, authToken string, spaceCtx *models.SpaceContext) ([]*models.AgentResponse, error) {
	// Verify notebook exists and user has access
	_, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
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
	// Filter agents by type=producer and accessible within the space
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
		"tenant_id": spaceCtx.TenantID,
		"space_id":  spaceCtx.SpaceID,
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
		agent, err = s.agentService.GetAgent(ctx, req.AgentID, userID, userTeams)
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
	production := models.NewProduction(req, agent.ID, agent.AgentBuilderID, notebookID, userID, spaceCtx)

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

	// Update production with completion details
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
	// Build the user message based on production type and notebook context
	// Note: We don't include document content here since agent-builder will inject it
	userMessage := s.buildProducerUserMessage(notebook, req, "")

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

	default:
		message = "IMPORTANT: You MUST use ONLY the document content provided above in the '--- RELEVANT CONTEXT ---' section. Do NOT make up or infer content.\n\n"
		message += fmt.Sprintf("Analyze the documents in the notebook \"%s\" and generate content of type \"%s\".\n\n", notebook.Name, req.Type)
		message += "Base your response EXCLUSIVELY on the actual document content provided above."
	}

	// Add format instruction if specified
	if req.Format != "" {
		message += fmt.Sprintf("\n\nPlease format the output as %s.", req.Format)
	}

	// Add custom title if provided
	if req.Title != "" {
		message += fmt.Sprintf("\n\nTitle the output: \"%s\"", req.Title)
	}

	// Include document content if explicitly provided (for direct LLM execution fallback)
	// When using agent-builder, document context is injected into the system prompt,
	// so this will typically be empty
	if documentContent != "" {
		message += "\n\nBelow is the content from the documents in this notebook. Use this content to generate your response:\n\n"
		message += documentContent
	}

	return message
}

// GetProductionByID retrieves a production by ID
func (s *ProductionService) GetProductionByID(ctx context.Context, productionID, userID string, spaceCtx *models.SpaceContext) (*models.Production, error) {
	query := `
		MATCH (p:Production {id: $production_id, tenant_id: $tenant_id})
		RETURN p.id, p.title, p.type, p.format, p.status,
		       p.agent_id, p.agent_builder_id, p.notebook_id, p.user_id,
		       p.space_type, p.space_id, p.tenant_id,
		       p.storage_bucket, p.storage_key, p.size_bytes,
		       p.execution_id, p.tokens_used, p.cost_usd, p.response_time_ms,
		       p.error_message, p.source_documents, p.search_text,
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

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Production not found", map[string]interface{}{
			"production_id": productionID,
		})
	}

	production, err := s.recordToProduction(result.Records[0])
	if err != nil {
		return nil, err
	}

	// Verify production belongs to the correct space
	if production.SpaceID != spaceCtx.SpaceID || production.TenantID != spaceCtx.TenantID {
		return nil, errors.ForbiddenWithDetails("Production not accessible in this space", map[string]interface{}{
			"production_id": productionID,
			"space_id":      spaceCtx.SpaceID,
		})
	}

	// Check if user has read permissions
	if !spaceCtx.CanRead() {
		return nil, errors.Forbidden("Insufficient permissions to read production")
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

	// Download content from S3
	content, err := s.storageService.DownloadFileFromTenantBucket(ctx, spaceCtx.TenantID, production.StorageKey)
	if err != nil {
		s.logger.Error("Failed to download production content", zap.Error(err))
		return "", errors.InternalWithCause("Failed to retrieve production content", err)
	}

	return string(content), nil
}

// ListNotebookProductions lists productions for a notebook
func (s *ProductionService) ListNotebookProductions(ctx context.Context, notebookID, userID string, spaceCtx *models.SpaceContext, limit, offset int) (*models.ProductionListResponse, error) {
	// Verify notebook exists and user has access
	_, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
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
		       p.error_message, p.source_documents, p.created_at, p.updated_at
		ORDER BY p.created_at DESC
		SKIP $offset
		LIMIT $limit
	`

	params := map[string]interface{}{
		"notebook_id": notebookID,
		"tenant_id":   spaceCtx.TenantID,
		"space_id":    spaceCtx.SpaceID,
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

	// Get total count
	countQuery := `
		MATCH (p:Production)
		WHERE p.notebook_id = $notebook_id
			AND p.tenant_id = $tenant_id
			AND p.space_id = $space_id
		RETURN count(p) as total
	`

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, map[string]interface{}{
		"notebook_id": notebookID,
		"tenant_id":   spaceCtx.TenantID,
		"space_id":    spaceCtx.SpaceID,
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
		"source_documents": production.SourceDocuments,
		"search_text":      production.SearchText,
		"created_at":       production.CreatedAt.Format(time.RFC3339),
		"updated_at":       production.UpdatedAt.Format(time.RFC3339),
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to create production", zap.Error(err))
		return errors.Database("Failed to create production", err)
	}

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
