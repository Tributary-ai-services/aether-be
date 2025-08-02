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
	Router          *gin.Engine
	UserHandler     *UserHandler
	NotebookHandler *NotebookHandler
	DocumentHandler *DocumentHandler
	HealthHandler   *HealthHandler
	Metrics         *metrics.Metrics
	logger          *logger.Logger
}

// NewAPIServer creates a new API server with all routes configured
func NewAPIServer(
	neo4j *database.Neo4jClient,
	redis *database.RedisClient,
	keycloakClient *auth.KeycloakClient,
	storageService *services.S3StorageService,
	kafkaService *services.KafkaService,
	metricsInstance *metrics.Metrics,
	log *logger.Logger,
) *APIServer {
	// Initialize services
	userService := services.NewUserService(neo4j, redis, log)
	notebookService := services.NewNotebookService(neo4j, redis, log)
	documentService := services.NewDocumentService(neo4j, redis, log)

	// Set dependencies for document service
	documentService.SetStorageService(storageService)
	// TODO: Set processing service when implemented

	// Initialize handlers
	userHandler := NewUserHandler(userService, log)
	notebookHandler := NewNotebookHandler(notebookService, log)
	documentHandler := NewDocumentHandler(documentService, log)
	healthHandler := NewHealthHandler(neo4j, redis, storageService, kafkaService, log)

	// Create Gin router
	gin.SetMode(gin.ReleaseMode) // Set to DebugMode for development
	router := gin.New()

	// Global middleware
	router.Use(gin.Recovery())
	router.Use(requestLoggingMiddleware())
	router.Use(corsMiddleware())
	router.Use(middleware.RequestIDMiddleware())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RequestSizeLimit(10 << 20)) // 10MB limit
	router.Use(middleware.ValidationMiddleware(log))
	router.Use(metrics.HTTPMetricsMiddleware(metricsInstance, log))

	server := &APIServer{
		Router:          router,
		UserHandler:     userHandler,
		NotebookHandler: notebookHandler,
		DocumentHandler: documentHandler,
		HealthHandler:   healthHandler,
		Metrics:         metricsInstance,
		logger:          log.WithService("api_server"),
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
		users.GET("/search", s.UserHandler.SearchUsers)
		users.GET("/:id", s.UserHandler.GetUserByID)
	}

	// Notebook routes
	notebooks := api.Group("/notebooks")
	{
		notebooks.POST("", s.NotebookHandler.CreateNotebook)
		notebooks.GET("", s.NotebookHandler.ListNotebooks)
		notebooks.GET("/search", s.NotebookHandler.SearchNotebooks)
		notebooks.GET("/:id", s.NotebookHandler.GetNotebook)
		notebooks.PUT("/:id", s.NotebookHandler.UpdateNotebook)
		notebooks.DELETE("/:id", s.NotebookHandler.DeleteNotebook)
		notebooks.POST("/:id/share", s.NotebookHandler.ShareNotebook)

		// Documents within notebooks
		notebooks.GET("/:notebook_id/documents", s.DocumentHandler.ListDocumentsByNotebook)
	}

	// Document routes
	documents := api.Group("/documents")
	{
		documents.POST("/upload", s.DocumentHandler.UploadDocument)
		documents.GET("/search", s.DocumentHandler.SearchDocuments)
		documents.GET("/:id", s.DocumentHandler.GetDocument)
		documents.PUT("/:id", s.DocumentHandler.UpdateDocument)
		documents.DELETE("/:id", s.DocumentHandler.DeleteDocument)
		documents.GET("/:id/download", s.DocumentHandler.DownloadDocument)
		documents.GET("/:id/url", s.DocumentHandler.GetDocumentURL)
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
