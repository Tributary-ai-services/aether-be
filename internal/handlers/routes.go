package handlers

import (
	"os"
	
	"github.com/gin-gonic/gin"

	"github.com/Tributary-ai-services/aether-be/internal/auth"
	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/metrics"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// APIServer represents the API server with all dependencies
type APIServer struct {
	Router               *gin.Engine
	UserHandler          *UserHandler
	NotebookHandler      *NotebookHandler
	DocumentHandler      *DocumentHandler
	ChunkHandler         *ChunkHandler
	JobHandler           *JobHandler
	WebSocketHandler     *WebSocketHandler
	MLHandler            *MLHandler
	WorkflowHandler      *WorkflowHandler
	TeamHandler          *TeamHandler
	OrganizationHandler  *OrganizationHandler
	SpaceHandler         *SpaceHandler
	AgentHandler         *AgentHandler
	HealthHandler        *HealthHandler
	StreamHandler        *StreamHandler
	RouterHandler        *RouterHandler
	LoggingHandler       *LoggingHandler
	VectorSearchHandler  *VectorSearchHandler
	SpaceService         *services.SpaceContextService
	Metrics              *metrics.Metrics
	logger               *logger.Logger
}

// NewAPIServer creates a new API server with all routes configured
func NewAPIServer(
	cfg *config.Config,
	neo4j *database.Neo4jClient,
	keycloakClient *auth.KeycloakClient,
	storageService *services.S3StorageService,
	kafkaService *services.KafkaService,
	audiModalClient *services.AudiModalService,
	metricsInstance *metrics.Metrics,
	log *logger.Logger,
) *APIServer {
	// Initialize services
	userService := services.NewUserService(neo4j, audiModalClient, log)
	organizationService := services.NewOrganizationService(neo4j, audiModalClient, log)
	spaceContextService := services.NewSpaceContextService(userService, organizationService, audiModalClient, log)
	spaceService := services.NewSpaceService(neo4j, log)
	notebookService := services.NewNotebookService(neo4j, log)
	documentService := services.NewDocumentService(neo4j, notebookService, log)
	chunkService := services.NewChunkService(neo4j, log)
	mlService := services.NewMLService(neo4j, log)
	workflowService := services.NewWorkflowService(neo4j, log)
	teamService := services.NewTeamService(neo4j, log)
	streamService := services.NewStreamService(neo4j, log)

	// Agent service with agent-builder URL configuration
	agentBuilderURL := os.Getenv("AGENT_BUILDER_URL")
	if agentBuilderURL == "" {
		// For now, disable agent-builder proxy if not configured
		// This will cause agent endpoints to return errors but won't crash the server
		agentBuilderURL = "http://agent-builder-not-configured:8080"
		log.Warn("AGENT_BUILDER_URL not configured - agent endpoints will not work")
	}
	agentService := services.NewAgentService(neo4j, userService, notebookService, teamService, agentBuilderURL, log)

	// Onboarding service for automatic new user setup
	onboardingService := services.NewOnboardingService(
		userService,
		spaceContextService,
		notebookService,
		agentService,
		documentService,
		log,
	)

	// Set dependencies for document service
	documentService.SetStorageService(storageService)
	documentService.SetProcessingService(audiModalClient)

	// Initialize processing event handler for Kafka events from audimodal
	if kafkaService != nil {
		processingEventHandler := services.NewProcessingEventHandler(documentService, kafkaService, log)
		if err := processingEventHandler.Start(); err != nil {
			log.WithError(err).Error("Failed to start processing event handler - document sync from audimodal will not work")
		} else {
			log.Info("Processing event handler started - listening for processing.complete events")
		}
	}

	// Initialize handlers
	userHandler := NewUserHandler(userService, spaceContextService, onboardingService, log)
	notebookHandler := NewNotebookHandler(notebookService, userService, log)
	documentHandler := NewDocumentHandler(documentService, audiModalClient, log)
	chunkHandler := NewChunkHandler(neo4j, chunkService, audiModalClient, log)
	jobHandler := NewJobHandler(documentService, audiModalClient, log)
	webSocketHandler := NewWebSocketHandler(documentService, audiModalClient, log)
	mlHandler := NewMLHandler(mlService, log)
	workflowHandler := NewWorkflowHandler(workflowService, log)
	teamHandler := NewTeamHandler(teamService, userService, log)
	organizationHandler := NewOrganizationHandler(organizationService, userService, log)
	spaceHandler := NewSpaceHandler(spaceContextService, spaceService, userService, organizationService, log)
	agentHandler := NewAgentHandler(agentService, userService, teamService, log)
	streamHandler := NewStreamHandler(streamService, log)
	healthHandler := NewHealthHandler(neo4j, storageService, kafkaService, log)
	loggingHandler := NewLoggingHandler(log)
	vectorSearchHandler := NewVectorSearchHandler(notebookService, documentService, userService, &cfg.DeepLake, log)

	// Initialize router handler (may be nil if disabled)
	routerHandler, err := NewRouterHandler(&cfg.Router, log)
	if err != nil {
		log.WithError(err).Error("Failed to initialize router handler")
		// Continue without router handler - it will be nil
	}

	// Create Gin router
	gin.SetMode(gin.ReleaseMode) // Set to DebugMode for development
	router := gin.New()

	// Global middleware
	router.Use(debugRequestMiddleware(log))
	router.Use(customRecoveryMiddleware(log))
	router.Use(requestLoggingMiddleware())
	router.Use(corsMiddleware())
	router.Use(middleware.RequestIDMiddleware())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RequestSizeLimit(10 << 20)) // 10MB limit
	router.Use(middleware.ValidationMiddleware(log))
	router.Use(metrics.HTTPMetricsMiddleware(metricsInstance, log))

	server := &APIServer{
		Router:               router,
		UserHandler:          userHandler,
		NotebookHandler:      notebookHandler,
		DocumentHandler:      documentHandler,
		ChunkHandler:         chunkHandler,
		JobHandler:           jobHandler,
		WebSocketHandler:     webSocketHandler,
		MLHandler:            mlHandler,
		WorkflowHandler:      workflowHandler,
		TeamHandler:          teamHandler,
		OrganizationHandler:  organizationHandler,
		SpaceHandler:         spaceHandler,
		AgentHandler:         agentHandler,
		HealthHandler:        healthHandler,
		StreamHandler:        streamHandler,
		RouterHandler:        routerHandler,
		LoggingHandler:       loggingHandler,
		VectorSearchHandler:  vectorSearchHandler,
		SpaceService:         spaceContextService,
		Metrics:              metricsInstance,
		logger:               log.WithService("api_server"),
	}

	// Setup routes
	server.setupRoutes(keycloakClient)

	return server
}

// setupRoutes configures all API routes
func (s *APIServer) setupRoutes(keycloakClient *auth.KeycloakClient) {
	// Health check routes (no auth required)
	s.Router.GET("/health", s.HealthHandler.HealthCheck)
	s.Router.GET("/health/live", s.HealthHandler.LivenessCheck)
	s.Router.GET("/health/ready", s.HealthHandler.ReadinessCheck)

	// Webhook routes (no auth required)
	s.Router.POST("/webhooks/audimodal/processing-complete", s.DocumentHandler.AudiModalProcessingWebhook)

	// API routes with authentication
	api := s.Router.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(keycloakClient, s.logger))

	// Logging routes - frontend logs sent to backend
	api.POST("/logs", s.LoggingHandler.SubmitFrontendLogs)

	// User routes
	users := api.Group("/users")
	{
		users.GET("/me", s.UserHandler.GetCurrentUser)
		users.PUT("/me", s.UserHandler.UpdateCurrentUser)
		users.DELETE("/me", s.UserHandler.DeleteCurrentUser)
		users.GET("/me/preferences", s.UserHandler.UpdateUserPreferences) // TODO: Change to GET handler
		users.PUT("/me/preferences", s.UserHandler.UpdateUserPreferences)
		users.GET("/me/stats", s.UserHandler.GetUserStats)
		users.GET("/me/spaces", s.UserHandler.GetUserSpaces)
		users.GET("/me/onboarding", s.UserHandler.GetOnboardingStatus)
		users.POST("/me/onboarding", s.UserHandler.MarkTutorialComplete)
		users.DELETE("/me/onboarding", s.UserHandler.ResetTutorial)
		users.GET("/search", s.UserHandler.SearchUsers)
		users.GET("/:id", s.UserHandler.GetUserByID)
	}

	// Notebook routes
	notebooks := api.Group("/notebooks")
	notebooks.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	notebooks.Use(middleware.RequireSpaceContext(s.logger))
	{
		notebooks.POST("", s.NotebookHandler.CreateNotebook)
		notebooks.GET("", s.NotebookHandler.ListNotebooks)
		notebooks.GET("/search", s.NotebookHandler.SearchNotebooks)
		notebooks.GET("/:id", s.NotebookHandler.GetNotebook)
		notebooks.PUT("/:id", s.NotebookHandler.UpdateNotebook)
		notebooks.DELETE("/:id", s.NotebookHandler.DeleteNotebook)
		notebooks.POST("/:id/share", s.NotebookHandler.ShareNotebook)

		// Documents within notebooks - use same parameter name to avoid conflict
		notebooks.GET("/:id/documents", s.DocumentHandler.ListDocumentsByNotebook)

		// Vector search routes for RAG-only lookup
		notebooks.POST("/:id/vector-search/text", s.VectorSearchHandler.TextSearch)
		notebooks.POST("/:id/vector-search/hybrid", s.VectorSearchHandler.HybridSearch)
		notebooks.GET("/:id/vector-search/info", s.VectorSearchHandler.GetVectorSearchInfo)
	}

	// Document routes
	documents := api.Group("/documents")
	documents.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	documents.Use(middleware.RequireSpaceContext(s.logger))
	{
		documents.POST("", s.DocumentHandler.CreateDocument)
		documents.POST("/upload", s.DocumentHandler.UploadDocument)
		documents.POST("/upload-base64", s.DocumentHandler.UploadDocumentBase64)
		documents.GET("/search", s.DocumentHandler.SearchDocuments)
		documents.GET("/:id", s.DocumentHandler.GetDocument)
		documents.GET("/:id/status", s.DocumentHandler.GetDocumentStatus)
		documents.GET("/:id/stream", s.WebSocketHandler.StreamDocumentStatus)
		documents.PUT("/:id", s.DocumentHandler.UpdateDocument)
		documents.DELETE("/:id", s.DocumentHandler.DeleteDocument)
		documents.POST("/:id/reprocess", s.DocumentHandler.ReprocessDocument)
		documents.POST("/refresh-processing", s.DocumentHandler.RefreshProcessingResults)
		documents.GET("/:id/download", s.DocumentHandler.DownloadDocument)
		documents.GET("/:id/url", s.DocumentHandler.GetDocumentURL)
		documents.GET("/:id/analysis", s.DocumentHandler.GetDocumentAnalysis)
		documents.GET("/:id/text", s.DocumentHandler.GetDocumentExtractedText)
	}

	// Chunk routes - file-specific chunks
	files := api.Group("/files")
	files.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	files.Use(middleware.RequireSpaceContext(s.logger))
	{
		files.GET("/:file_id/chunks", s.ChunkHandler.GetFileChunks)
		files.GET("/:file_id/chunks/:chunk_id", s.ChunkHandler.GetChunk)
		files.POST("/:file_id/reprocess", s.ChunkHandler.ReprocessFileWithStrategy)
	}

	// Chunk search routes
	chunks := api.Group("/chunks")
	chunks.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	chunks.Use(middleware.RequireSpaceContext(s.logger))
	{
		chunks.POST("/search", s.ChunkHandler.SearchChunks)
	}

	// Strategy routes - no space context required (global)
	strategies := api.Group("/strategies")
	{
		strategies.GET("", s.ChunkHandler.GetAvailableStrategies)
		strategies.POST("/recommend", s.ChunkHandler.GetOptimalStrategy)
	}

	// Job tracking routes
	jobs := api.Group("/jobs")
	jobs.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	jobs.Use(middleware.RequireSpaceContext(s.logger))
	{
		jobs.GET("/:id", s.JobHandler.GetJobStatus)
		jobs.GET("/:id/stream", s.WebSocketHandler.StreamJobStatus)
	}

	// Team routes
	teams := api.Group("/teams")
	{
		teams.POST("", s.TeamHandler.CreateTeam)
		teams.GET("", s.TeamHandler.GetTeams)
		teams.GET("/:id", s.TeamHandler.GetTeam)
		teams.PUT("/:id", s.TeamHandler.UpdateTeam)
		teams.DELETE("/:id", s.TeamHandler.DeleteTeam)
		
		// Team member routes
		teams.GET("/:id/members", s.TeamHandler.GetTeamMembers)
		teams.POST("/:id/members", s.TeamHandler.InviteTeamMember)
		teams.PUT("/:id/members/:user_id", s.TeamHandler.UpdateTeamMemberRole)
		teams.DELETE("/:id/members/:user_id", s.TeamHandler.RemoveTeamMember)
	}

	// Organization routes
	organizations := api.Group("/organizations")
	{
		organizations.POST("", s.OrganizationHandler.CreateOrganization)
		organizations.GET("", s.OrganizationHandler.GetOrganizations)
		organizations.GET("/:id", s.OrganizationHandler.GetOrganization)
		organizations.PUT("/:id", s.OrganizationHandler.UpdateOrganization)
		organizations.DELETE("/:id", s.OrganizationHandler.DeleteOrganization)
		
		// Organization member routes
		organizations.GET("/:id/members", s.OrganizationHandler.GetOrganizationMembers)
		organizations.POST("/:id/members", s.OrganizationHandler.InviteOrganizationMember)
		organizations.PUT("/:id/members/:user_id", s.OrganizationHandler.UpdateOrganizationMemberRole)
		organizations.DELETE("/:id/members/:user_id", s.OrganizationHandler.RemoveOrganizationMember)
	}

	// Agent routes - with space context for multi-tenancy
	agents := api.Group("/agents")
	agents.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	agents.Use(middleware.RequireSpaceContext(s.logger))
	{
		agents.POST("", s.AgentHandler.CreateAgent)
		agents.GET("", s.AgentHandler.ListAgents)
		agents.GET("/:id", s.AgentHandler.GetAgent)
		agents.PUT("/:id", s.AgentHandler.UpdateAgent)
		agents.DELETE("/:id", s.AgentHandler.DeleteAgent)
		
		// Agent knowledge source management
		agents.POST("/:id/knowledge-sources", s.AgentHandler.AddKnowledgeSource)
		agents.GET("/:id/knowledge-sources", s.AgentHandler.GetKnowledgeSources)
		agents.DELETE("/:id/knowledge-sources/:notebook_id", s.AgentHandler.RemoveKnowledgeSource)
		
		// Agent execution
		agents.POST("/:id/execute", s.AgentHandler.ExecuteAgent)
	}

	// Execution history routes
	executions := api.Group("/executions")
	executions.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	executions.Use(middleware.RequireSpaceContext(s.logger))
	{
		executions.GET("", s.AgentHandler.ListExecutions)
	}

	// Stats routes
	stats := api.Group("/stats")
	stats.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	stats.Use(middleware.RequireSpaceContext(s.logger))
	{
		stats.GET("/agents/:id", s.AgentHandler.GetAgentStats)
	}

	// Space routes
	spaces := api.Group("/spaces")
	{
		spaces.POST("", s.SpaceHandler.CreateSpace)
		spaces.GET("", s.SpaceHandler.GetSpaces)
		spaces.GET("/:id", s.SpaceHandler.GetSpace)
		spaces.PUT("/:id", s.SpaceHandler.UpdateSpace)
		spaces.DELETE("/:id", s.SpaceHandler.DeleteSpace)

		// Space member management routes
		spaces.GET("/:id/members", s.SpaceHandler.ListSpaceMembers)
		spaces.POST("/:id/members", s.SpaceHandler.AddSpaceMember)
		spaces.PATCH("/:id/members/:userId", s.SpaceHandler.UpdateSpaceMember)
		spaces.DELETE("/:id/members/:userId", s.SpaceHandler.RemoveSpaceMember)
	}

	// ML/Analytics routes
	ml := api.Group("/ml")
	ml.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	ml.Use(middleware.RequireSpaceContext(s.logger))
	{
		// Model management
		ml.POST("/models", s.MLHandler.CreateModel)
		ml.GET("/models", s.MLHandler.GetModels)
		ml.GET("/models/:id", s.MLHandler.GetModel)
		ml.PUT("/models/:id", s.MLHandler.UpdateModel)
		ml.DELETE("/models/:id", s.MLHandler.DeleteModel)
		ml.POST("/models/:id/deploy", s.MLHandler.DeployModel)

		// Experiment management
		ml.POST("/experiments", s.MLHandler.CreateExperiment)
		ml.GET("/experiments", s.MLHandler.GetExperiments)
		ml.GET("/experiments/:id", s.MLHandler.GetExperiment)
		ml.PUT("/experiments/:id", s.MLHandler.UpdateExperiment)
		ml.DELETE("/experiments/:id", s.MLHandler.DeleteExperiment)

		// Analytics
		ml.GET("/analytics", s.MLHandler.GetAnalytics)
	}

	// Workflow automation routes
	workflows := api.Group("/workflows")
	workflows.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	workflows.Use(middleware.RequireSpaceContext(s.logger))
	{
		// Workflow management
		workflows.POST("", s.WorkflowHandler.CreateWorkflow)
		workflows.GET("", s.WorkflowHandler.GetWorkflows)
		workflows.GET("/analytics", s.WorkflowHandler.GetWorkflowAnalytics)
		workflows.GET("/:id", s.WorkflowHandler.GetWorkflow)
		workflows.PUT("/:id", s.WorkflowHandler.UpdateWorkflow)
		workflows.DELETE("/:id", s.WorkflowHandler.DeleteWorkflow)
		
		// Workflow execution
		workflows.POST("/:id/execute", s.WorkflowHandler.ExecuteWorkflow)
		workflows.PUT("/:id/status", s.WorkflowHandler.UpdateWorkflowStatus)
		workflows.GET("/:id/executions", s.WorkflowHandler.GetWorkflowExecutions)
	}

	// Live streaming routes
	streams := api.Group("/streams")
	streams.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	streams.Use(middleware.RequireSpaceContext(s.logger))
	{
		// Stream source management
		streams.POST("/sources", s.StreamHandler.CreateStreamSource)
		streams.GET("/sources", s.StreamHandler.GetStreamSources)
		streams.GET("/sources/:id", s.StreamHandler.GetStreamSource)
		streams.PUT("/sources/:id", s.StreamHandler.UpdateStreamSource)
		streams.DELETE("/sources/:id", s.StreamHandler.DeleteStreamSource)
		streams.PUT("/sources/:id/status", s.StreamHandler.UpdateStreamSourceStatus)
		
		// Live event management
		streams.POST("/sources/:id/events", s.StreamHandler.IngestEvent)
		streams.GET("/events", s.StreamHandler.GetLiveEvents)
		streams.GET("/events/:id", s.StreamHandler.GetLiveEvent)
		
		// Real-time WebSocket streaming
		streams.GET("/live", s.StreamHandler.StreamEvents)
		
		// Stream analytics
		streams.GET("/analytics", s.StreamHandler.GetStreamAnalytics)
		streams.GET("/analytics/realtime", s.StreamHandler.GetRealtimeAnalytics)
	}

	// Router proxy routes with flexible authentication
	if s.RouterHandler != nil {
		// Tier 1: Public router endpoints (no authentication required)
		// These provide informational data and don't need user context
		publicRouter := s.Router.Group("/api/v1/router")
		{
			publicRouter.GET("/health", s.RouterHandler.GetHealth)
			publicRouter.GET("/providers", s.RouterHandler.GetProviders)
			publicRouter.GET("/providers/:name", s.RouterHandler.GetProvider) // Provider details are informational
			publicRouter.GET("/capabilities", s.RouterHandler.GetCapabilities)
		}

		// Tier 2: Authenticated router endpoints (user context required)
		// These perform operations that need user tracking and billing
		authRouter := api.Group("/router")
		{
			authRouter.POST("/chat/completions", s.RouterHandler.ChatCompletions)
			authRouter.POST("/completions", s.RouterHandler.Completions)
			authRouter.POST("/messages", s.RouterHandler.Messages)
		}
		
		// Note: When UseServiceAuth=true, the RouterHandler will automatically
		// use service authentication regardless of which tier the request comes from
	}

	// Admin routes (require admin role)
	admin := api.Group("/admin")
	admin.Use(middleware.RequireRole("admin"))
	{
		// TODO: Add admin-specific routes
		// admin.GET("/users", s.UserHandler.ListAllUsers)
		// admin.GET("/stats", s.AdminHandler.GetSystemStats)
		// admin.POST("/maintenance", s.AdminHandler.MaintenanceMode)
	}

	// Metrics and monitoring routes (can be separate from main API)
	metricsGroup := s.Router.Group("/metrics")
	{
		metricsGroup.GET("/prometheus", s.Metrics.GinHandler())
		metricsGroup.GET("", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "Prometheus metrics available at /metrics/prometheus"})
		})
	}
}

// Start starts the HTTP server
func (s *APIServer) Start(addr string) error {
	s.logger.Info("Starting API server")
	return s.Router.Run(addr)
}

// Shutdown gracefully shuts down the server
func (s *APIServer) Shutdown() error {
	s.logger.Info("Shutting down API server")
	// TODO: Implement graceful shutdown
	// This would typically involve:
	// 1. Stop accepting new requests
	// 2. Wait for existing requests to complete
	// 3. Close database connections
	// 4. Close other resources
	return nil
}
