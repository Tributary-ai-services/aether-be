package handlers

import (
	"database/sql"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

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
	ComplianceHandler    *ComplianceHandler
	DataSourceHandler    *DataSourceHandler
	SecurityHandler      *SecurityHandler
	DatabaseHandler      *DatabaseHandler
	SavedQueryHandler    *SavedQueryHandler
	AIPlaygroundHandler  *AIPlaygroundHandler
	ProductionHandler    *ProductionHandler
	MCPHandler           *MCPHandler
	ArgoHandler          *ArgoHandler
	NotificationHandler  *NotificationHandler
	OAuthHandler         *OAuthHandler
	CloudDriveHandler    *CloudDriveHandler
	InvitationHandler    *InvitationHandler
	CommentHandler       *CommentHandler
	ConversationHandler  *ConversationHandler
	RegistrationHandler  *RegistrationHandler
	SpaceService              *services.SpaceContextService
	ProductionCleanupWorker  *services.ProductionCleanupWorker
	Metrics                  *metrics.Metrics
	logger                   *logger.Logger
}

// NewAPIServer creates a new API server with all routes configured
func NewAPIServer(
	cfg *config.Config,
	neo4j *database.Neo4jClient,
	postgresDB *sql.DB,
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
	spaceService := services.NewSpaceService(neo4j, log)
	spaceContextService := services.NewSpaceContextService(userService, organizationService, spaceService, audiModalClient, log)
	notebookService := services.NewNotebookService(neo4j, log)
	documentService := services.NewDocumentService(neo4j, notebookService, log)
	chunkService := services.NewChunkService(neo4j, log)
	mlService := services.NewMLService(neo4j, log)
	argoGenerator := services.NewArgoGenerator(log)
	workflowService := services.NewWorkflowService(neo4j, kafkaService, argoGenerator, log)
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
		processingEventHandler := services.NewProcessingEventHandler(documentService, workflowService, kafkaService, log)
		if err := processingEventHandler.Start(); err != nil {
			log.WithError(err).Error("Failed to start processing event handler - document sync from audimodal will not work")
		} else {
			log.Info("Processing event handler started - listening for processing.complete events")
		}
	}

	// Initialize handlers
	userHandler := NewUserHandler(userService, spaceContextService, onboardingService, log)
	notebookHandler := NewNotebookHandler(notebookService, userService, log)
	documentHandler := NewDocumentHandler(documentService, audiModalClient, userService, log)
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
	complianceHandler := NewComplianceHandler(audiModalClient, log)

	// Initialize Crawl4AI service and data source handler
	crawl4aiService := services.NewCrawl4AIService(&cfg.Crawl4AI, log)
	dataSourceHandler := NewDataSourceHandler(crawl4aiService, log)

	// Initialize security event service and handler
	securityEventService := services.NewSecurityEventService(kafkaService, postgresDB, log)
	securityHandler := NewSecurityHandler(securityEventService, log)

	// Initialize K8s client for database credential management (in-cluster)
	var k8sClient kubernetes.Interface
	if k8sConfig, err := rest.InClusterConfig(); err != nil {
		log.Warn("Not running in K8s cluster — K8s Secret/CRD management disabled for databases")
	} else {
		if client, err := kubernetes.NewForConfig(k8sConfig); err != nil {
			log.WithError(err).Error("Failed to create K8s client — K8s Secret/CRD management disabled")
		} else {
			k8sClient = client
			log.Info("K8s client initialized for database credential management")
		}
	}

	// Initialize DBHub and Database service and handler
	dbhubService := services.NewDBHubService(&cfg.DBHub, log)
	neo4jQueryService := services.NewNeo4jQueryService(k8sClient, log)
	databaseService := services.NewDatabaseService(neo4j, dbhubService, neo4jQueryService, log)
	databaseHandler := NewDatabaseHandler(databaseService, userService, log)

	// Initialize SavedQuery service and handler
	savedQueryService := services.NewSavedQueryService(neo4j, databaseService, log)
	savedQueryHandler := NewSavedQueryHandler(savedQueryService, userService, teamService, log)

	// Initialize AI Playground service and handler
	aiPlaygroundService := services.NewAIPlaygroundService(neo4j, &cfg.Router, agentService, workflowService, log)
	aiPlaygroundHandler := NewAIPlaygroundHandler(aiPlaygroundService, userService, teamService, log)

	// Initialize conversation service (needed by production handler)
	conversationService := services.NewConversationService(neo4j, notebookService, log)

	// Initialize Podcast MCP client and Production service
	podcastMCPClient := services.NewMCPClientService("podcast-mcp", &cfg.PodcastMCP, log)
	productionService := services.NewProductionService(neo4j, storageService, agentService, notebookService, teamService, spaceService, audiModalClient, podcastMCPClient, kafkaService, log)
	productionCleanupWorker := services.NewProductionCleanupWorker(productionService, log)

	// Initialize Redis client for podcast progress tracking (optional — graceful if unavailable)
	var podcastProgressService *services.PodcastProgressService
	if cfg.Redis.Addr != "" {
		redisClient, redisErr := database.NewRedisClient(cfg.Redis, log)
		if redisErr != nil {
			log.Warn("Redis unavailable — podcast progress tracking disabled", zap.Error(redisErr))
		} else {
			podcastProgressService = services.NewPodcastProgressService(redisClient, neo4j, productionService, log)
			// Subscribe to Kafka podcast.progress topic for progress events
			if kafkaService != nil {
				progressTopic := kafkaService.GetTopicForEvent(services.EventPodcastProgressUpdate)
				if subErr := kafkaService.Subscribe(progressTopic, "podcast-progress-handler", podcastProgressService.HandleProgressMessage); subErr != nil {
					log.Warn("Failed to subscribe to podcast progress topic", zap.String("topic", progressTopic), zap.Error(subErr))
				} else {
					log.Info("Subscribed to podcast progress Kafka topic", zap.String("topic", progressTopic))
				}
			}
		}
	}
	productionHandler := NewProductionHandler(productionService, podcastProgressService, conversationService, userService, teamService, log)

	// Initialize Argo Events service and handler
	argoService := services.NewArgoService(log)
	argoHandler := NewArgoHandler(argoService, log)

	// Initialize notification service and handler
	notificationService := services.NewNotificationService(neo4j, kafkaService, log)
	// Wire execution status updater to close the feedback loop:
	// when Argo completes, the webhook updates the WorkflowExecution node
	notificationService.SetExecutionUpdater(workflowService)
	notificationHandler := NewNotificationHandler(notificationService, log)

	// Initialize OAuth and Cloud Drive services
	oauthCfg := &services.OAuthConfig{
		GoogleClientID:        cfg.OAuth.GoogleClientID,
		GoogleClientSecret:    cfg.OAuth.GoogleClientSecret,
		MicrosoftClientID:     cfg.OAuth.MicrosoftClientID,
		MicrosoftClientSecret: cfg.OAuth.MicrosoftClientSecret,
		MicrosoftTenantID:     cfg.OAuth.MicrosoftTenantID,
		EncryptionKey:         cfg.OAuth.EncryptionKey,
		RedirectBaseURL:       cfg.OAuth.RedirectBaseURL,
	}
	var redisClientForOAuth *redis.Client
	if cfg.Redis.Addr != "" {
		redisClientForOAuth = redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
	}
	oauthService := services.NewOAuthService(neo4j, redisClientForOAuth, oauthCfg, log)
	cloudDriveService := services.NewCloudDriveService(oauthService, storageService, audiModalClient, documentService, log)
	oauthHandler := NewOAuthHandler(oauthService, log)
	cloudDriveHandler := NewCloudDriveHandler(cloudDriveService, oauthService, log)

	// Initialize email and invitation services
	emailService := services.NewEmailService(&cfg.SMTP, log)
	invitationService := services.NewInvitationService(neo4j, notebookService, userService, emailService, log)
	invitationHandler := NewInvitationHandler(invitationService, userService, log)

	// Initialize comment service and handler
	commentService := services.NewCommentService(neo4j, notebookService, log)
	commentHandler := NewCommentHandler(commentService, notebookService, userService, log)

	// Initialize conversation handler
	conversationHandler := NewConversationHandler(conversationService, notebookService, userService, log)

	// Initialize registration handler (public, no auth required)
	registrationHandler := NewRegistrationHandler(keycloakClient, log)

	// Wire invitation processing into onboarding
	onboardingService.SetInvitationService(invitationService)

	// Initialize Napkin MCP service and MCP handler
	napkinService := services.NewNapkinService(&cfg.Napkin, log)
	mcpHandler := NewMCPHandler(napkinService, databaseService, neo4jQueryService, cfg, log)

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
		ComplianceHandler:    complianceHandler,
		DataSourceHandler:    dataSourceHandler,
		SecurityHandler:      securityHandler,
		DatabaseHandler:      databaseHandler,
		SavedQueryHandler:    savedQueryHandler,
		AIPlaygroundHandler:  aiPlaygroundHandler,
		ProductionHandler:    productionHandler,
		MCPHandler:           mcpHandler,
		ArgoHandler:          argoHandler,
		NotificationHandler:  notificationHandler,
		OAuthHandler:         oauthHandler,
		CloudDriveHandler:    cloudDriveHandler,
		InvitationHandler:    invitationHandler,
		CommentHandler:       commentHandler,
		ConversationHandler:  conversationHandler,
		RegistrationHandler:  registrationHandler,
		SpaceService:              spaceContextService,
		ProductionCleanupWorker:  productionCleanupWorker,
		Metrics:                  metricsInstance,
		logger:                   log.WithService("api_server"),
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
	s.Router.POST("/webhooks/workflow-complete", s.NotificationHandler.WorkflowCompleteWebhook)

	// Public auth routes (no authentication required)
	publicAuth := s.Router.Group("/api/v1/auth")
	{
		publicAuth.POST("/register", s.RegistrationHandler.Register)
	}

	// API routes with authentication
	api := s.Router.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(keycloakClient, s.logger))

	// Logging routes - frontend logs sent to backend
	api.POST("/logs", s.LoggingHandler.SubmitFrontendLogs)

	// Invitation routes (no space context needed)
	invitations := api.Group("/invitations")
	{
		invitations.POST("/accept", s.InvitationHandler.AcceptInvitation)
		invitations.GET("/pending", s.InvitationHandler.GetPendingInvitations)
	}

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

		// Producer preferences
		users.GET("/me/preferences/producers", s.ProductionHandler.GetProducerPreferences)
		users.PATCH("/me/preferences/producers", s.ProductionHandler.UpdateProducerPreferences)
	}

	// Notebook routes
	notebooks := api.Group("/notebooks")
	notebooks.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	notebooks.Use(middleware.RequireSpaceContext(s.logger))
	{
		notebooks.POST("", s.NotebookHandler.CreateNotebook)
		notebooks.GET("", s.NotebookHandler.ListNotebooks)
		notebooks.GET("/search", s.NotebookHandler.SearchNotebooks)
		notebooks.GET("/shared-with-me", s.NotebookHandler.GetSharedWithMe)
		notebooks.GET("/:id", s.NotebookHandler.GetNotebook)
		notebooks.PUT("/:id", s.NotebookHandler.UpdateNotebook)
		notebooks.DELETE("/:id", s.NotebookHandler.DeleteNotebook)
		notebooks.POST("/:id/share", s.NotebookHandler.ShareNotebook)
		notebooks.GET("/:id/shares", s.NotebookHandler.GetNotebookShares)
		notebooks.DELETE("/:id/share/:userId", s.NotebookHandler.RevokeNotebookShare)

		// Invitation routes
		notebooks.POST("/:id/invite", s.InvitationHandler.SendInvitation)
		notebooks.GET("/:id/invitations", s.InvitationHandler.GetNotebookInvitations)
		notebooks.DELETE("/:id/invitations/:invitationId", s.InvitationHandler.CancelInvitation)

		// Comment routes (stream must be before :commentId to avoid param conflict)
		notebooks.GET("/:id/comments/stream", s.CommentHandler.StreamComments)
		notebooks.POST("/:id/comments", s.CommentHandler.CreateComment)
		notebooks.GET("/:id/comments", s.CommentHandler.GetComments)
		notebooks.PUT("/:id/comments/:commentId", s.CommentHandler.UpdateComment)
		notebooks.DELETE("/:id/comments/:commentId", s.CommentHandler.DeleteComment)

		// Conversation routes
		notebooks.GET("/:id/conversations", s.ConversationHandler.ListConversations)
		notebooks.POST("/:id/conversations", s.ConversationHandler.CreateConversation)
		notebooks.GET("/:id/conversations/:convId", s.ConversationHandler.GetConversation)
		notebooks.PUT("/:id/conversations/:convId", s.ConversationHandler.UpdateConversation)
		notebooks.DELETE("/:id/conversations/:convId", s.ConversationHandler.DeleteConversation)
		notebooks.GET("/:id/conversations/:convId/messages", s.ConversationHandler.GetMessages)

		// Documents within notebooks - use same parameter name to avoid conflict
		notebooks.GET("/:id/documents", s.DocumentHandler.ListDocumentsByNotebook)

		// Vector search routes for RAG-only lookup
		notebooks.POST("/:id/vector-search/text", s.VectorSearchHandler.TextSearch)
		notebooks.POST("/:id/vector-search/hybrid", s.VectorSearchHandler.HybridSearch)
		notebooks.GET("/:id/vector-search/info", s.VectorSearchHandler.GetVectorSearchInfo)

		// Producer agents for notebook
		notebooks.GET("/:id/producers", s.ProductionHandler.GetNotebookProducers)
		notebooks.POST("/:id/producers/:agent_id/execute", s.ProductionHandler.ExecuteProducer)

		// Notebook chat - uses internal Notebook Chat Assistant agent
		notebooks.POST("/:id/chat", s.ProductionHandler.NotebookChat)

		// Productions (artifacts) within notebook
		notebooks.GET("/:id/productions", s.ProductionHandler.ListNotebookProductions)
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

	// Production routes - standalone CRUD for productions
	productions := api.Group("/productions")
	productions.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	productions.Use(middleware.RequireSpaceContext(s.logger))
	{
		productions.GET("/:id", s.ProductionHandler.GetProduction)
		productions.GET("/:id/content", s.ProductionHandler.GetProductionContent)
		productions.GET("/:id/progress", s.ProductionHandler.GetProductionProgress)
		productions.GET("/:id/progress/stream", s.ProductionHandler.StreamProductionProgress)
		productions.POST("/:id/retry", s.ProductionHandler.RetryProduction)
		productions.DELETE("/:id", s.ProductionHandler.DeleteProduction)
		productions.POST("/bulk-delete", s.ProductionHandler.BulkDeleteProductions)
	}

	// Renderer routes - list available renderer workflows
	api.GET("/renderers", s.ProductionHandler.ListRenderers)

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

	// Internal agent routes - system agents like Prompt Assistant (no space context required)
	internalAgents := api.Group("/agents/internal")
	{
		internalAgents.GET("", s.AgentHandler.ListInternalAgents)
		internalAgents.GET("/:id", s.AgentHandler.GetInternalAgent)
		internalAgents.POST("/:id/execute", s.AgentHandler.ExecuteInternalAgent)
	}

	// Internal notebook routes - for service-to-service communication (agent-builder, etc.)
	// These routes bypass space context middleware for internal service calls
	internalNotebooks := api.Group("/internal/notebooks")
	{
		// Get notebook hierarchy with optional sub-notebooks
		internalNotebooks.GET("/:id/hierarchy", s.NotebookHandler.GetNotebookHierarchy)
		// Get all documents recursively from notebook and sub-notebooks
		internalNotebooks.GET("/:id/documents/recursive", s.NotebookHandler.GetDocumentsRecursive)
		// Get sub-notebook IDs for a parent notebook
		internalNotebooks.GET("/:id/sub-notebooks", s.NotebookHandler.GetSubNotebooks)
		// Get documents for specific notebook (flat, no recursion)
		internalNotebooks.GET("/:id/documents", s.NotebookHandler.GetNotebookDocuments)
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

	// Compliance routes - DLP violations from AudiModal
	compliance := api.Group("/compliance")
	compliance.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	compliance.Use(middleware.RequireSpaceContext(s.logger))
	{
		compliance.GET("/violations", s.ComplianceHandler.GetViolations)
		compliance.GET("/violations/:id", s.ComplianceHandler.GetViolation)
		compliance.GET("/summary", s.ComplianceHandler.GetSummary)
		compliance.POST("/violations/:id/acknowledge", s.ComplianceHandler.AcknowledgeViolation)
		compliance.POST("/violations/acknowledge-bulk", s.ComplianceHandler.BulkAcknowledgeViolations)
	}

	// Security routes - threat detection events and policies
	security := api.Group("/security")
	security.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	{
		// Security events
		security.GET("/events", s.SecurityHandler.GetSecurityEvents)
		security.GET("/events/:id", s.SecurityHandler.GetSecurityEvent)
		security.PUT("/events/:id/review", s.SecurityHandler.ReviewSecurityEvent)

		// Security summary/dashboard
		security.GET("/summary", s.SecurityHandler.GetSecuritySummary)

		// Security policies
		security.GET("/policies", s.SecurityHandler.GetSecurityPolicies)
		security.PUT("/policies/:id", s.SecurityHandler.UpdateSecurityPolicy)
	}

	// Data Sources routes - URL probing and web scraping via Crawl4AI
	dataSources := api.Group("/data-sources")
	{
		dataSources.GET("/health", s.DataSourceHandler.GetCrawl4AIHealth)
		dataSources.GET("/scrapers", s.DataSourceHandler.GetScraperTypes)
		dataSources.POST("/probe-url", s.DataSourceHandler.ProbeURL)
		dataSources.POST("/scrape-url", s.DataSourceHandler.ScrapeURL)
	}

	// Database connection management routes
	databases := api.Group("/databases")
	databases.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	databases.Use(middleware.RequireSpaceContext(s.logger))
	{
		// Database connection CRUD
		databases.POST("", s.DatabaseHandler.CreateDatabase)
		databases.GET("", s.DatabaseHandler.ListDatabases)
		databases.GET("/:id", s.DatabaseHandler.GetDatabase)
		databases.PUT("/:id", s.DatabaseHandler.UpdateDatabase)
		databases.DELETE("/:id", s.DatabaseHandler.DeleteDatabase)

		// Connection testing
		databases.POST("/:id/test", s.DatabaseHandler.TestConnection)

		// Query execution
		databases.POST("/:id/query", s.DatabaseHandler.ExecuteQuery)

		// Schema introspection
		databases.GET("/:id/schema", s.DatabaseHandler.GetSchema)
		databases.GET("/:id/tables", s.DatabaseHandler.GetTables)
		databases.GET("/:id/tables/:table/columns", s.DatabaseHandler.GetTableColumns)
	}

	// Saved Queries routes - for developer tools
	savedQueries := api.Group("/saved-queries")
	savedQueries.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	savedQueries.Use(middleware.RequireSpaceContext(s.logger))
	{
		savedQueries.POST("", s.SavedQueryHandler.CreateSavedQuery)
		savedQueries.GET("", s.SavedQueryHandler.ListSavedQueries)
		savedQueries.GET("/:id", s.SavedQueryHandler.GetSavedQuery)
		savedQueries.PUT("/:id", s.SavedQueryHandler.UpdateSavedQuery)
		savedQueries.DELETE("/:id", s.SavedQueryHandler.DeleteSavedQuery)
		savedQueries.POST("/:id/execute", s.SavedQueryHandler.ExecuteSavedQuery)
		savedQueries.POST("/:id/duplicate", s.SavedQueryHandler.DuplicateSavedQuery)
	}

	// AI Playground routes - for developer tools
	aiPlayground := api.Group("/developer-tools/ai")
	aiPlayground.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	aiPlayground.Use(middleware.RequireSpaceContext(s.logger))
	{
		// LLM endpoints
		aiPlayground.GET("/providers", s.AIPlaygroundHandler.ListProviders)
		aiPlayground.GET("/providers/:provider/models", s.AIPlaygroundHandler.ListModels)
		aiPlayground.POST("/llm/completions", s.AIPlaygroundHandler.CreateCompletion)
		aiPlayground.POST("/llm/completions/compare", s.AIPlaygroundHandler.CompareCompletions)

		// Agent testing endpoints
		aiPlayground.GET("/agents", s.AIPlaygroundHandler.ListAgents)
		aiPlayground.POST("/agents/:id/test", s.AIPlaygroundHandler.TestAgent)

		// Workflow testing endpoints
		aiPlayground.GET("/workflows", s.AIPlaygroundHandler.ListWorkflows)
		aiPlayground.POST("/workflows/:id/test", s.AIPlaygroundHandler.TestWorkflow)

		// Saved prompts endpoints
		aiPlayground.GET("/prompts", s.AIPlaygroundHandler.ListSavedPrompts)
		aiPlayground.POST("/prompts", s.AIPlaygroundHandler.CreateSavedPrompt)
		aiPlayground.GET("/prompts/:id", s.AIPlaygroundHandler.GetSavedPrompt)
		aiPlayground.PUT("/prompts/:id", s.AIPlaygroundHandler.UpdateSavedPrompt)
		aiPlayground.DELETE("/prompts/:id", s.AIPlaygroundHandler.DeleteSavedPrompt)
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
		workflows.GET("/:id/executions/:execId/status", s.WorkflowHandler.GetExecutionStatus)
		workflows.POST("/:id/upload", s.WorkflowHandler.UploadToWorkflow)
		workflows.POST("/:id/artifacts", s.WorkflowHandler.PublishArtifact)
		workflows.GET("/:id/versions", s.WorkflowHandler.ListWorkflowVersions)
	}

	// Notification routes
	notifications := api.Group("/notifications")
	notifications.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	notifications.Use(middleware.RequireSpaceContext(s.logger))
	{
		notifications.GET("", s.NotificationHandler.GetNotifications)
		notifications.GET("/unread-count", s.NotificationHandler.GetUnreadCount)
		notifications.PUT("/:id/read", s.NotificationHandler.MarkAsRead)
		notifications.PUT("/read-all", s.NotificationHandler.MarkAllAsRead)
		notifications.DELETE("/:id", s.NotificationHandler.DeleteNotification)
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

	// MCP server management routes
	mcpGroup := api.Group("/mcp")
	mcpGroup.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	{
		mcpGroup.GET("/servers", s.MCPHandler.ListServers)
		mcpGroup.GET("/servers/:id/tools", s.MCPHandler.ListTools)
		mcpGroup.POST("/servers/:id/test-connection", s.MCPHandler.TestConnection)
		mcpGroup.POST("/invoke", s.MCPHandler.InvokeTool)
	}

	// Skills routes - proxy to agent-builder
	api.GET("/skills", s.AgentHandler.ListSkills)

	// Credentials routes - list and manage OAuth credentials
	credentials := api.Group("/credentials")
	{
		credentials.GET("", s.OAuthHandler.ListCredentials)
		credentials.DELETE("/:id", s.OAuthHandler.DeleteCredential)
	}

	// OAuth routes - for cloud drive authentication
	oauth := api.Group("/oauth")
	{
		oauth.POST("/:provider/authorize", s.OAuthHandler.Authorize)
		oauth.POST("/:provider/callback", s.OAuthHandler.Callback)
	}

	// Cloud Drives routes - file browsing and import from Google Drive, OneDrive, SharePoint
	// IMPORTANT: Specific routes (sharepoint/*) MUST come before generic :provider routes
	// because Gin's `:provider` param would otherwise match "sharepoint" first.
	cloudDrives := api.Group("/cloud-drives")
	cloudDrives.Use(middleware.SpaceContextMiddleware(s.SpaceService, s.logger))
	{
		cloudDrives.GET("/sharepoint/sites", s.CloudDriveHandler.ListSharePointSites)
		cloudDrives.GET("/sharepoint/sites/:siteId/libraries", s.CloudDriveHandler.ListSharePointLibraries)
		cloudDrives.GET("/:provider/files", s.CloudDriveHandler.ListFiles)
		cloudDrives.GET("/:provider/search", s.CloudDriveHandler.SearchFiles)
		cloudDrives.POST("/:provider/import", s.CloudDriveHandler.ImportFiles)
	}

	// Argo Events routes - reads CRDs from K8s API (graceful degradation if unavailable)
	argoGroup := api.Group("/argo")
	{
		argoGroup.GET("/event-sources", s.ArgoHandler.ListEventSources)
		argoGroup.GET("/sensors", s.ArgoHandler.ListSensors)
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
