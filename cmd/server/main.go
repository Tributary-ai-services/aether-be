package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/auth"
	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/handlers"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/metrics"
	"github.com/Tributary-ai-services/aether-be/internal/services"
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
	defer func() {
		if err := appLogger.Sync(); err != nil {
			// Ignore broken pipe errors on sync, common during shutdown
			log.Printf("Logger sync warning: %v", err)
		}
	}()

	appLogger.Info("Starting Aether Backend Server",
		zap.String("version", cfg.Server.Version),
		zap.String("environment", cfg.Server.Environment),
		zap.String("port", cfg.Server.Port),
	)

	// Initialize databases
	appLogger.Info("Initializing database connections")

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

	// Initialize external services
	appLogger.Info("Initializing external services")

	keycloakClient, err := auth.NewKeycloakClient(cfg.Keycloak, appLogger)
	if err != nil {
		appLogger.Fatal("Failed to initialize Keycloak client", zap.Error(err))
	}

	var storageService *services.S3StorageService
	if cfg.Storage.Enabled {
		storageService, err = services.NewS3StorageService(cfg.Storage, appLogger)
		if err != nil {
			appLogger.Error("Failed to initialize storage service", zap.Error(err))
			// Don't fail startup, but log the error
		} else {
			appLogger.Info("Storage service initialized successfully")
		}
	}

	var kafkaService *services.KafkaService
	if cfg.Kafka.Enabled {
		kafkaService, err = services.NewKafkaService(cfg.Kafka, appLogger)
		if err != nil {
			appLogger.Error("Failed to initialize Kafka service", zap.Error(err))
			// Don't fail startup, but log the error
		} else {
			appLogger.Info("Kafka service initialized successfully")
		}
	}

	// Initialize metrics
	appLogger.Info("Initializing metrics system")
	metricsInstance := metrics.NewMetrics(appLogger)

	// Initialize metrics collector
	metricsCollector := metrics.NewMetricsCollector(
		metricsInstance,
		neo4jClient,
		redisClient,
		appLogger,
	)

	// Initialize API server
	appLogger.Info("Initializing API server")
	apiServer := handlers.NewAPIServer(
		neo4jClient,
		redisClient,
		keycloakClient,
		storageService,
		kafkaService,
		metricsInstance,
		appLogger,
	)

	// Create HTTP server
	server := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      apiServer.Router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeout) * time.Second,
	}

	// Start metrics collection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go metricsCollector.Start(ctx)
	appLogger.Info("Metrics collection started")

	// Start server in a goroutine
	go func() {
		appLogger.Info("Starting HTTP server",
			zap.String("address", server.Addr),
		)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Fatal("Failed to start HTTP server", zap.Error(err))
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	appLogger.Info("Shutting down server...")

	// Stop metrics collection
	cancel() // This stops the metrics collector context
	metricsCollector.Stop()
	appLogger.Info("Metrics collection stopped")

	// Give outstanding requests 30 seconds to complete
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shutdown HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		appLogger.Error("Server forced to shutdown", zap.Error(err))
	}

	// Shutdown API server (close external connections)
	if err := apiServer.Shutdown(); err != nil {
		appLogger.Error("Error during API server shutdown", zap.Error(err))
	}

	// Close Kafka service
	if kafkaService != nil {
		if err := kafkaService.Close(); err != nil {
			appLogger.Error("Error closing Kafka service", zap.Error(err))
		}
	}

	appLogger.Info("Server exited")
}

func init() {
	// Set timezone to UTC
	os.Setenv("TZ", "UTC")

	// Print startup banner
	fmt.Print(`
    ___       __  __              ____             __              __
   /   | ____/ /_/ /_  ___  _____/ __ )____ ______/ /_____  ____  / /
  / /| |/ __  / __/ __ \/ _ \/ ___/ __  / __ '/ ___/ //_/ _ \/ __ \/ / 
 / ___ / /_/ / /_/ / / /  __/ /  / /_/ / /_/ / /__/ ,< /  __/ / / / /  
/_/  |_\__,_/\__/_/ /_/\___/_/  /_____/\__,_/\___/_/|_|\___/_/ /_/_/   
                                                                       
Aether Backend - Document Processing & Knowledge Management Platform
`)
}
