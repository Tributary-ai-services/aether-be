package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// OnboardingService handles new user setup and default resource creation
type OnboardingService struct {
	userService     *UserService
	spaceService    *SpaceContextService
	notebookService *NotebookService
	agentService    *AgentService
	documentService *DocumentService
	logger          *logger.Logger
}

// NewOnboardingService creates a new onboarding service
func NewOnboardingService(
	userService *UserService,
	spaceService *SpaceContextService,
	notebookService *NotebookService,
	agentService *AgentService,
	documentService *DocumentService,
	log *logger.Logger,
) *OnboardingService {
	return &OnboardingService{
		userService:     userService,
		spaceService:    spaceService,
		notebookService: notebookService,
		agentService:    agentService,
		documentService: documentService,
		logger:          log.WithService("onboarding_service"),
	}
}

// OnboardNewUser performs complete user onboarding with default resources
// This runs synchronously during user creation to ensure resources are ready before login completes
func (s *OnboardingService) OnboardNewUser(ctx context.Context, user *models.User) (*models.OnboardingResult, error) {
	s.logger.Info("Starting user onboarding",
		zap.String("user_id", user.ID),
		zap.String("email", user.Email),
		zap.String("personal_space_id", user.PersonalSpaceID),
	)

	result := &models.OnboardingResult{
		UserID:    user.ID,
		Steps:     make(map[string]bool),
		StartedAt: time.Now(),
	}

	// Step 1: Verify personal space exists (should be created during user creation)
	if !user.HasPersonalTenant() {
		errMsg := "User missing personal tenant - cannot proceed with onboarding"
		s.logger.Error(errMsg, zap.String("user_id", user.ID))
		result.Success = false
		result.ErrorMessage = errMsg
		result.CompletedAt = time.Now()
		result.DurationMs = result.CompletedAt.Sub(result.StartedAt).Milliseconds()
		return result, errors.Internal(errMsg)
	}
	result.Steps[string(models.StepPersonalSpace)] = true

	// Step 2: Get personal space context
	spaceReq := models.SpaceContextRequest{
		SpaceType: models.SpaceTypePersonal,
		SpaceID:   user.PersonalSpaceID,
	}

	s.logger.Info("Resolving personal space for onboarding",
		zap.String("user_id", user.ID),
		zap.String("keycloak_id", user.KeycloakID),
		zap.String("personal_space_id", user.PersonalSpaceID),
		zap.String("personal_tenant_id", user.PersonalTenantID),
	)

	// Pass Keycloak ID for space resolution (consistent with space_context.go fix)
	spaceCtx, err := s.spaceService.ResolveSpaceContext(ctx, user.KeycloakID, spaceReq)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to resolve personal space: %v", err)
		s.logger.Error(errMsg, zap.String("user_id", user.ID), zap.Error(err))
		result.Success = false
		result.ErrorMessage = errMsg
		result.CompletedAt = time.Now()
		result.DurationMs = result.CompletedAt.Sub(result.StartedAt).Milliseconds()
		return result, errors.InternalWithCause("Failed to initialize personal space", err)
	}

	s.logger.Info("Personal space context resolved",
		zap.String("user_id", user.ID),
		zap.String("space_id", spaceCtx.SpaceID),
		zap.String("tenant_id", spaceCtx.TenantID),
	)

	// Step 3: Create default "Getting Started" notebook
	defaultNotebook, err := s.createDefaultNotebook(ctx, user, spaceCtx)
	if err != nil {
		s.logger.Error("Failed to create default notebook",
			zap.String("user_id", user.ID),
			zap.Error(err),
		)
		// Don't fail onboarding - just log and continue
	} else {
		result.DefaultNotebookID = defaultNotebook.ID
		result.Steps[string(models.StepDefaultNotebook)] = true
		s.logger.Info("Default notebook created",
			zap.String("user_id", user.ID),
			zap.String("notebook_id", defaultNotebook.ID),
		)
	}

	// Step 4: Upload sample documents to the notebook
	if defaultNotebook != nil {
		sampleDocsCount, err := s.uploadSampleDocuments(ctx, user, spaceCtx, defaultNotebook)
		if err != nil {
			s.logger.Error("Failed to upload sample documents",
				zap.String("user_id", user.ID),
				zap.Error(err),
			)
			// Don't fail onboarding - just log
		} else {
			result.SampleDocsCount = sampleDocsCount
			result.Steps[string(models.StepSampleDocuments)] = true
			s.logger.Info("Sample documents uploaded",
				zap.String("user_id", user.ID),
				zap.Int("count", sampleDocsCount),
			)
		}
	}

	// Step 5: Create default "Personal Assistant" agent (GPT-4)
	if defaultNotebook != nil {
		defaultAgent, err := s.createDefaultAgent(ctx, user, spaceCtx, defaultNotebook)
		if err != nil {
			s.logger.Error("Failed to create default agent",
				zap.String("user_id", user.ID),
				zap.Error(err),
			)
			// Don't fail onboarding - just log
		} else {
			result.DefaultAgentID = defaultAgent.AgentBuilderID
			result.Steps[string(models.StepDefaultAgent)] = true
			s.logger.Info("Default agent created",
				zap.String("user_id", user.ID),
				zap.String("agent_id", defaultAgent.ID),
				zap.String("agent_builder_id", defaultAgent.AgentBuilderID),
			)
		}
	}

	// Mark onboarding as complete
	result.CompletedAt = time.Now()
	result.DurationMs = result.CompletedAt.Sub(result.StartedAt).Milliseconds()
	result.Success = true

	s.logger.Info("User onboarding completed successfully",
		zap.String("user_id", user.ID),
		zap.Duration("duration", result.CompletedAt.Sub(result.StartedAt)),
		zap.Int("steps_completed", len(result.Steps)),
	)

	return result, nil
}

// createDefaultNotebook creates a "Getting Started" notebook for new users
func (s *OnboardingService) createDefaultNotebook(
	ctx context.Context,
	user *models.User,
	spaceCtx *models.SpaceContext,
) (*models.Notebook, error) {
	req := models.NotebookCreateRequest{
		Name:        "Getting Started",
		Description: "Welcome to Aether! This is your first notebook. Use it to explore document processing, AI agents, and intelligent search.",
		Visibility:  "private",
		Tags:        []string{"welcome", "getting-started", "onboarding"},
	}

	notebook, err := s.notebookService.CreateNotebook(ctx, req, user.ID, spaceCtx)
	if err != nil {
		return nil, err
	}

	return notebook, nil
}

// uploadSampleDocuments uploads pre-configured sample documents to the notebook
func (s *OnboardingService) uploadSampleDocuments(
	ctx context.Context,
	user *models.User,
	spaceCtx *models.SpaceContext,
	notebook *models.Notebook,
) (int, error) {
	s.logger.Info("Starting sample document upload",
		zap.String("user_id", user.ID),
		zap.String("notebook_id", notebook.ID),
		zap.String("space_id", spaceCtx.SpaceID),
	)

	// Define sample documents with embedded content
	sampleDocs := []struct {
		Name        string
		Description string
		Content     string
		Tags        []string
	}{
		{
			Name:        "Welcome to Aether.txt",
			Description: "Introduction to the Aether AI Platform",
			Content: `Welcome to Aether - AI-Powered Document Intelligence Platform

Aether is your comprehensive platform for intelligent document processing and AI-powered knowledge extraction.

KEY FEATURES:
1. Multi-Modal Document Processing
   - Upload PDFs, images, audio, and video files
   - Automatic text extraction and OCR
   - Metadata extraction and indexing

2. Intelligent Search
   - Semantic search across all your documents
   - Vector-based similarity matching
   - Entity recognition and relationship mapping

3. AI Agents
   - Create custom AI assistants for your notebooks
   - Ask questions about your documents
   - Get intelligent summaries and insights

4. Collaborative Workspaces
   - Organize documents in notebooks
   - Share with team members
   - Role-based access control

GETTING STARTED:
1. Upload your first document using the "Upload" button
2. Wait for processing to complete
3. Use the search bar to find content
4. Create an AI agent to interact with your documents

For more information, visit our documentation or contact support.
`,
			Tags: []string{"welcome", "introduction", "getting-started"},
		},
		{
			Name:        "Quick Start Guide.txt",
			Description: "Step-by-step guide to using Aether",
			Content: `Aether Quick Start Guide

STEP 1: UPLOAD DOCUMENTS
- Click the "Upload" button in your notebook
- Select one or more files (PDF, DOCX, images, audio, video)
- Add a description and tags
- Click "Upload" to start processing

STEP 2: WAIT FOR PROCESSING
- Documents are automatically processed by our AI engine
- Text is extracted and indexed
- Metadata and entities are identified
- Processing typically takes 30-60 seconds per document

STEP 3: SEARCH YOUR DOCUMENTS
- Use the search bar to find content across all documents
- Search supports natural language queries
- Results are ranked by relevance
- Click any result to view the full document

STEP 4: CREATE AN AI AGENT
- Go to the "Agents" tab
- Click "Create Agent"
- Configure the agent with your preferences
- The agent will have access to all documents in selected notebooks

STEP 5: ASK QUESTIONS
- Chat with your AI agent
- Ask questions about your documents
- Get summaries and insights
- The agent will cite sources from your documents

ADVANCED FEATURES:
- Create hierarchical notebook structures
- Share notebooks with team members
- Export search results and insights
- Set up automated workflows

Need help? Check the FAQ or contact our support team.
`,
			Tags: []string{"guide", "tutorial", "quick-start"},
		},
		{
			Name:        "Sample FAQ.txt",
			Description: "Frequently Asked Questions about Aether",
			Content: `Aether Platform - Frequently Asked Questions

Q: What file types does Aether support?
A: Aether supports a wide range of file types including:
   - Documents: PDF, DOCX, TXT, MD
   - Images: JPG, PNG, GIF, TIFF
   - Audio: MP3, WAV, M4A
   - Video: MP4, MOV, AVI
   - Archives: ZIP (auto-extracted)

Q: How long does document processing take?
A: Processing time varies by document size and complexity:
   - Text documents (< 10 pages): 10-30 seconds
   - Large PDFs (100+ pages): 1-3 minutes
   - Images with OCR: 20-60 seconds
   - Audio/video transcription: ~30% of file duration

Q: Is my data secure?
A: Yes, Aether implements enterprise-grade security:
   - All data encrypted at rest and in transit
   - Role-based access control (RBAC)
   - Audit logging of all activities
   - SOC 2 and GDPR compliant
   - Regular security audits

Q: Can I share notebooks with my team?
A: Yes! Create an Organization space to:
   - Invite team members
   - Assign roles (owner, admin, member, viewer)
   - Share notebooks and documents
   - Collaborate on AI agents
   - Track team activity

Q: How does the AI agent work?
A: AI agents use advanced language models to:
   - Understand natural language questions
   - Search relevant documents in your notebooks
   - Synthesize information from multiple sources
   - Provide cited answers with source references
   - Learn from your feedback

Q: What are the storage limits?
A: Storage limits depend on your plan:
   - Personal Free: 5GB, 1,000 files
   - Professional: 100GB, 10,000 files
   - Enterprise: Unlimited storage

Q: Can I export my data?
A: Yes, you can export:
   - Individual documents (original format)
   - Search results (CSV, JSON)
   - Notebook metadata
   - Chat transcripts with AI agents
   - Full data export for migration

Q: How accurate is the text extraction?
A: Our AI-powered extraction achieves:
   - 99%+ accuracy on digital PDFs
   - 95%+ accuracy on scanned documents (OCR)
   - 90%+ accuracy on handwritten text
   - 95%+ accuracy on audio transcription

Q: Can I integrate Aether with other tools?
A: Yes, Aether provides:
   - REST API for programmatic access
   - Webhooks for event notifications
   - Zapier and Make integrations
   - Direct integrations with Slack, Teams, Google Drive

For more questions, contact support@aether.ai
`,
			Tags: []string{"faq", "help", "support"},
		},
	}

	successCount := 0

	for i, doc := range sampleDocs {
		// Create upload request
		fileData := []byte(doc.Content)
		uploadReq := models.DocumentUploadRequest{
			DocumentCreateRequest: models.DocumentCreateRequest{
				Name:        doc.Name,
				Description: doc.Description,
				NotebookID:  notebook.ID,
				Tags:        doc.Tags,
				Metadata: map[string]interface{}{
					"source":     "onboarding",
					"is_sample":  true,
					"order":      i + 1,
					"mime_type":  "text/plain",
					"size_bytes": len(fileData),
				},
			},
			FileData: fileData,
		}

		fileInfo := models.FileInfo{
			OriginalName: doc.Name,
			MimeType:     "text/plain",
			SizeBytes:    int64(len(fileData)),
			Checksum:     "",
		}

		// Upload the document
		_, err := s.documentService.UploadDocument(ctx, uploadReq, user.KeycloakID, spaceCtx, fileInfo)
		if err != nil {
			s.logger.Error("Failed to upload sample document",
				zap.String("user_id", user.ID),
				zap.String("notebook_id", notebook.ID),
				zap.String("document_name", doc.Name),
				zap.Error(err),
			)
			// Continue with other documents even if one fails
			continue
		}

		successCount++
		s.logger.Info("Successfully uploaded sample document",
			zap.String("user_id", user.ID),
			zap.String("document_name", doc.Name),
			zap.Int("bytes", len(fileData)),
		)
	}

	s.logger.Info("Sample document upload completed",
		zap.String("user_id", user.ID),
		zap.String("notebook_id", notebook.ID),
		zap.Int("success_count", successCount),
		zap.Int("total_count", len(sampleDocs)),
	)

	return successCount, nil
}

// createDefaultAgent creates a "Personal Assistant" agent for new users
func (s *OnboardingService) createDefaultAgent(
	ctx context.Context,
	user *models.User,
	spaceCtx *models.SpaceContext,
	notebook *models.Notebook,
) (*models.AgentResponse, error) {
	agentReq := models.AgentCreateRequest{
		Name:         "Personal Assistant",
		Description:  "Your personal AI assistant with access to your Getting Started notebook. Ask questions about your documents!",
		Type:         models.AgentTypeQA,
		SpaceID:      spaceCtx.SpaceID,
		IsPublic:     false,
		IsTemplate:   false,
		Tags:         []string{"default", "personal-assistant", "onboarding"},
		SystemPrompt: "You are a helpful AI assistant. Use the knowledge from the user's notebooks to provide accurate, contextual responses. When answering questions, cite the specific documents you reference.",
		LLMConfig: map[string]interface{}{
			"provider":    "openai",
			"model":       "gpt-4",
			"temperature": 0.7,
			"max_tokens":  2000,
		},
	}

	// Note: This requires authentication token for agent-builder API
	// The token would need to be obtained from the request context
	// For now, we'll attempt to create without token (may fail)
	agent, err := s.agentService.CreateAgent(ctx, agentReq, spaceCtx, "")
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// GetOnboardingStatus checks onboarding status for a user
func (s *OnboardingService) GetOnboardingStatus(ctx context.Context, user *models.User, spaceCtx *models.SpaceContext) (*models.OnboardingResult, error) {
	status := &models.OnboardingResult{
		UserID:  user.ID,
		Steps:   make(map[string]bool),
		Success: user.HasPersonalTenant(),
	}

	// Check personal space
	status.Steps[string(models.StepPersonalSpace)] = user.HasPersonalTenant()

	// Check for default notebook
	searchReq := models.NotebookSearchRequest{
		Query:  "Getting Started",
		Limit:  1,
		Offset: 0,
	}
	notebooks, err := s.notebookService.SearchNotebooks(ctx, searchReq, user.ID, spaceCtx)
	if err != nil {
		s.logger.Error("Failed to check for default notebook", zap.Error(err))
	} else if len(notebooks.Notebooks) > 0 {
		status.Steps[string(models.StepDefaultNotebook)] = true
		status.DefaultNotebookID = notebooks.Notebooks[0].ID

		// Check for documents in the notebook
		// TODO: Query document count for the notebook
		status.Steps[string(models.StepSampleDocuments)] = false
	}

	// Check for default agent
	// TODO: Query agents with tag "personal-assistant"
	status.Steps[string(models.StepDefaultAgent)] = false

	return status, nil
}
