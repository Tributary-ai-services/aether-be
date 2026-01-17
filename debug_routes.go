package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/auth"
	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/handlers"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/metrics"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	loggerConfig := logger.Config{
		Level:  cfg.Logger.Level,
		Format: cfg.Logger.Format,
	}
	appLogger, err := logger.New(loggerConfig)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	// Create a debug server
	fmt.Println("=== DEBUG: Creating Gin router ===")
	router := gin.New()
	
	// Add middlewares
	router.Use(gin.Recovery())
	
	// Add a simple test route
	fmt.Println("=== DEBUG: Adding test route ===")
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "test works"})
	})

	// Initialize databases (simplified)
	fmt.Println("=== DEBUG: Connecting to databases ===")
	neo4jClient, err := database.NewNeo4jClient(cfg.Neo4j, appLogger)
	if err != nil {
		appLogger.Fatal("Failed to initialize Neo4j client", zap.Error(err))
	}
	defer neo4jClient.Close(context.Background())

	redisClient, err := database.NewRedisClient(cfg.Redis, appLogger)
	if err != nil {
		appLogger.Fatal("Failed to initialize Redis client", zap.Error(err))
	}
	defer redisClient.Close()

	// Initialize Keycloak client
	fmt.Println("=== DEBUG: Initializing Keycloak client ===")
	keycloakClient, err := auth.NewKeycloakClient(cfg.Keycloak, appLogger)
	if err != nil {
		appLogger.Fatal("Failed to initialize Keycloak client", zap.Error(err))
	}

	// Initialize metrics
	metricsInstance := metrics.NewMetrics(appLogger)

	// Try to create the API server
	fmt.Println("=== DEBUG: Creating API server ===")
	apiServer := handlers.NewAPIServer(
		cfg,
		neo4jClient,
		keycloakClient,
		nil, // storage service
		nil, // kafka service
		nil, // audimodal service
		metricsInstance,
		appLogger,
	)

	fmt.Println("=== DEBUG: API Server routes ===")
	for _, route := range router.Routes() {
		fmt.Printf("Route: %s %s\n", route.Method, route.Path)
	}

	// Create HTTP server and test
	server := &http.Server{
		Addr:    ":3002",
		Handler: apiServer.Router,
	}

	fmt.Println("=== DEBUG: Starting server on :3002 ===")
	log.Fatal(server.ListenAndServe())
}