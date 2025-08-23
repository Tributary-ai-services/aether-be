package main

import (
	"context"
	"fmt"
	"log"


	"github.com/Tributary-ai-services/aether-be/internal/auth"
	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/handlers"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/metrics"
)

func main() {
	// Add defer recover to catch panics
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("PANIC CAUGHT: %v\n", r)
		}
	}()

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

	fmt.Println("=== STEP 1: Connecting to databases ===")
	neo4jClient, err := database.NewNeo4jClient(cfg.Neo4j, appLogger)
	if err != nil {
		log.Fatalf("Failed to initialize Neo4j client: %v", err)
	}
	defer neo4jClient.Close(context.Background())

	redisClient, err := database.NewRedisClient(cfg.Redis, appLogger)
	if err != nil {
		log.Fatalf("Failed to initialize Redis client: %v", err)
	}
	defer redisClient.Close()

	fmt.Println("=== STEP 2: Initializing Keycloak client ===")
	keycloakClient, err := auth.NewKeycloakClient(cfg.Keycloak, appLogger)
	if err != nil {
		log.Fatalf("Failed to initialize Keycloak client: %v", err)
	}

	fmt.Println("=== STEP 3: Initializing metrics ===")
	metricsInstance := metrics.NewMetrics(appLogger)

	fmt.Println("=== STEP 4: Creating API server ===")
	apiServer := handlers.NewAPIServer(
		neo4jClient,
		redisClient,
		keycloakClient,
		nil, // storage service
		nil, // kafka service
		metricsInstance,
		appLogger,
	)

	fmt.Println("=== STEP 5: Checking router routes ===")
	routes := apiServer.Router.Routes()
	fmt.Printf("Total routes registered: %d\n", len(routes))
	for _, route := range routes {
		fmt.Printf("Route: %s %s\n", route.Method, route.Path)
	}

	fmt.Println("=== SUCCESS: No panic occurred ===")
}