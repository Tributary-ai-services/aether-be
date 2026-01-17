package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/Tributary-ai-services/aether-be/internal/auth"
	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
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

	// Initialize Keycloak client
	keycloakClient, err := auth.NewKeycloakClient(cfg.Keycloak, appLogger)
	if err != nil {
		log.Fatalf("Failed to initialize Keycloak client: %v", err)
	}

	// Create router
	router := gin.New()
	router.Use(gin.Recovery())

	fmt.Println("=== TESTING: Basic route setup ===")
	
	// Test 1: Basic route without middleware
	router.GET("/basic", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "basic works"})
	})

	// Test 2: API group without auth middleware
	fmt.Println("=== TESTING: API group without auth ===")
	api := router.Group("/api/v1")
	api.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "api test works"})
	})

	// Test 3: API group with auth middleware
	fmt.Println("=== TESTING: API group with auth middleware ===")
	apiWithAuth := router.Group("/api/v1/auth")
	apiWithAuth.Use(middleware.AuthMiddleware(keycloakClient, appLogger))
	apiWithAuth.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "auth test works"})
	})

	fmt.Println("=== ROUTES REGISTERED ===")
	for _, route := range router.Routes() {
		fmt.Printf("Route: %s %s\n", route.Method, route.Path)
	}

	fmt.Println("=== Starting server on :3003 ===")
	router.Run(":3003")
}