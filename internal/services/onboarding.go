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
// This runs asynchronously after user creation from JWT token
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
	spaceCtx, err := s.spaceService.ResolveSpaceContext(ctx, user.ID, spaceReq)
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
	// TODO: Implement sample document upload
	// This would:
	// 1. Load sample PDF files from embedded resources or ConfigMap
	// 2. Upload to MinIO via storage service
	// 3. Create Document records in Neo4j
	// 4. Trigger AudiModal processing
	//
	// For now, return 0 as placeholder
	s.logger.Warn("Sample document upload not yet implemented",
		zap.String("user_id", user.ID),
		zap.String("notebook_id", notebook.ID),
	)

	return 0, nil
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
