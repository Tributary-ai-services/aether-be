package handlers

import (
	"github.com/gin-gonic/gin"

	"github.com/Tributary-ai-services/aether-be/internal/auth"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/metrics"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// APIServer represents the API server with all dependencies
type APIServer struct {
	Router              *gin.Engine
	UserHandler         *UserHandler
	NotebookHandler     *NotebookHandler
	DocumentHandler     *DocumentHandler
	TeamHandler         *TeamHandler
	OrganizationHandler *OrganizationHandler
	SpaceHandler        *SpaceHandler
	HealthHandler       *HealthHandler
	SpaceService        *services.SpaceContextService
	Metrics             *metrics.Metrics
	logger              *logger.Logger
}

// NewAPIServer creates a new API server with all routes configured
func NewAPIServer(
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
	spaceService := services.NewSpaceContextService(userService, organizationService, audiModalClient, log)
	notebookService := services.NewNotebookService(neo4j, log)
	documentService := services.NewDocumentService(neo4j, notebookService, log)
	teamService := services.NewTeamService(neo4j, log)

	// Set dependencies for document service
	documentService.SetStorageService(storageService)
	documentService.SetProcessingService(audiModalClient)

	// Initialize handlers
	userHandler := NewUserHandler(userService, spaceService, log)
	notebookHandler := NewNotebookHandler(notebookService, userService, log)
	documentHandler := NewDocumentHandler(documentService, log)
	teamHandler := NewTeamHandler(teamService, userService, log)
	organizationHandler := NewOrganizationHandler(organizationService, userService, log)
	spaceHandler := NewSpaceHandler(spaceService, userService, organizationService, log)
	healthHandler := NewHealthHandler(neo4j, storageService, kafkaService, log)

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
		Router:              router,
		UserHandler:         userHandler,
		NotebookHandler:     notebookHandler,
		DocumentHandler:     documentHandler,
		TeamHandler:         teamHandler,
		OrganizationHandler: organizationHandler,
		SpaceHandler:        spaceHandler,
		HealthHandler:       healthHandler,
		SpaceService:        spaceService,
		Metrics:             metricsInstance,
		logger:              log.WithService("api_server"),
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

	// API routes with authentication
	api := s.Router.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(keycloakClient, s.logger))

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
		documents.PUT("/:id", s.DocumentHandler.UpdateDocument)
		documents.DELETE("/:id", s.DocumentHandler.DeleteDocument)
		documents.POST("/:id/reprocess", s.DocumentHandler.ReprocessDocument)
		documents.GET("/:id/download", s.DocumentHandler.DownloadDocument)
		documents.GET("/:id/url", s.DocumentHandler.GetDocumentURL)
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

	// Space routes
	spaces := api.Group("/spaces")
	{
		spaces.POST("", s.SpaceHandler.CreateSpace)
		spaces.GET("", s.SpaceHandler.GetSpaces)
		spaces.GET("/:id", s.SpaceHandler.GetSpace)
		spaces.PUT("/:id", s.SpaceHandler.UpdateSpace)
		spaces.DELETE("/:id", s.SpaceHandler.DeleteSpace)
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
