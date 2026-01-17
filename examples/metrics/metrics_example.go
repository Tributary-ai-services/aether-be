package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/metrics"
)

func main() {
	fmt.Print("=== Aether Backend Prometheus Metrics Example ===\n")

	// Initialize logger
	loggerConfig := logger.Config{
		Level:  "info",
		Format: "json",
	}
	appLogger, err := logger.New(loggerConfig)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer appLogger.Sync()

	// Initialize metrics
	fmt.Println("1. Initializing Prometheus metrics system...")
	metricsInstance := metrics.NewMetrics(appLogger)

	// Example 1: Recording HTTP Metrics
	fmt.Println("2. Recording HTTP request metrics...")

	// Simulate different HTTP requests
	httpRequests := []struct {
		method   string
		path     string
		status   int
		duration time.Duration
		reqSize  int64
		respSize int64
	}{
		{"GET", "/api/v1/users/me", 200, 50 * time.Millisecond, 0, 512},
		{"POST", "/api/v1/documents/upload", 201, 2 * time.Second, 1024 * 1024, 256},
		{"GET", "/api/v1/notebooks", 200, 30 * time.Millisecond, 0, 2048},
		{"PUT", "/api/v1/users/me", 200, 100 * time.Millisecond, 256, 512},
		{"GET", "/api/v1/documents/search", 500, 5 * time.Second, 128, 0},
	}

	for _, req := range httpRequests {
		metricsInstance.RecordHTTPRequest(
			req.method, req.path, req.status,
			req.duration, req.reqSize, req.respSize,
		)
		fmt.Printf("  Recorded: %s %s -> %d (%v)\n",
			req.method, req.path, req.status, req.duration)
	}

	// Example 2: Recording Database Metrics
	fmt.Println("\n3. Recording database operation metrics...")

	dbOperations := []struct {
		database  string
		operation string
		status    string
		duration  time.Duration
	}{
		{"neo4j", "CREATE_USER", "success", 25 * time.Millisecond},
		{"neo4j", "FIND_DOCUMENTS", "success", 45 * time.Millisecond},
		{"neo4j", "UPDATE_NOTEBOOK", "success", 15 * time.Millisecond},
		{"neo4j", "DELETE_DOCUMENT", "error", 100 * time.Millisecond},
		{"redis", "GET", "success", 2 * time.Millisecond},
		{"redis", "SET", "success", 3 * time.Millisecond},
		{"redis", "EXPIRE", "success", 1 * time.Millisecond},
	}

	for _, op := range dbOperations {
		if op.database == "redis" {
			metricsInstance.RecordRedisOperation(op.operation, op.status, op.duration)
		} else {
			metricsInstance.RecordDBQuery(op.database, op.operation, op.status, op.duration)
		}
		fmt.Printf("  Recorded: %s %s -> %s (%v)\n",
			op.database, op.operation, op.status, op.duration)
	}

	// Example 3: Recording Application Metrics
	fmt.Println("\n4. Recording application-specific metrics...")

	// Document metrics
	metricsInstance.IncDocumentsTotal("processed", "pdf")
	metricsInstance.IncDocumentsTotal("processed", "image")
	metricsInstance.IncDocumentsTotal("failed", "pdf")
	metricsInstance.SetDocumentsProcessing(5)
	metricsInstance.RecordDocumentProcessing("pdf", "success", 30*time.Second)
	metricsInstance.RecordDocumentProcessing("image", "success", 10*time.Second)

	fmt.Println("  Recorded document processing metrics")

	// User and notebook metrics
	metricsInstance.IncUsersTotal("active")
	metricsInstance.IncUsersTotal("active")
	metricsInstance.IncUsersTotal("inactive")
	metricsInstance.IncNotebooksTotal("private")
	metricsInstance.IncNotebooksTotal("shared")

	fmt.Println("  Recorded user and notebook metrics")

	// Example 4: Recording Storage Metrics
	fmt.Println("\n5. Recording storage operation metrics...")

	storageOps := []struct {
		operation string
		status    string
		duration  time.Duration
		bytes     int64
		bucket    string
	}{
		{"upload", "success", 2 * time.Second, 1024 * 1024, "documents"},
		{"download", "success", 500 * time.Millisecond, 2048 * 1024, "documents"},
		{"delete", "success", 100 * time.Millisecond, 0, "documents"},
		{"upload", "error", 5 * time.Second, 0, "documents"},
	}

	for _, op := range storageOps {
		metricsInstance.RecordStorageOperation(
			op.operation, op.status, op.duration, op.bytes, op.bucket,
		)
		fmt.Printf("  Recorded: %s -> %s (%v, %d bytes)\n",
			op.operation, op.status, op.duration, op.bytes)
	}

	// Example 5: Recording External Service Metrics
	fmt.Println("\n6. Recording external service metrics...")

	externalRequests := []struct {
		service   string
		operation string
		status    string
		duration  time.Duration
	}{
		{"keycloak", "validate_token", "success", 100 * time.Millisecond},
		{"keycloak", "get_user_info", "success", 150 * time.Millisecond},
		{"keycloak", "validate_token", "error", 5 * time.Second},
		{"s3", "put_object", "success", 800 * time.Millisecond},
		{"s3", "get_object", "success", 200 * time.Millisecond},
	}

	for _, req := range externalRequests {
		metricsInstance.RecordExternalRequest(
			req.service, req.operation, req.status, req.duration,
		)
		fmt.Printf("  Recorded: %s %s -> %s (%v)\n",
			req.service, req.operation, req.status, req.duration)
	}

	// Example 6: System Metrics
	fmt.Println("\n7. Recording system metrics...")
	metricsInstance.SetGoroutines(50)
	metricsInstance.SetMemoryUsage(128 * 1024 * 1024) // 128MB
	metricsInstance.SetDBConnections(5, 2)            // 5 active, 2 idle
	metricsInstance.SetRedisConnections(3)            // 3 active
	fmt.Println("  Recorded system metrics (goroutines, memory, connections)")

	// Example 7: Start a simple HTTP server to expose metrics
	fmt.Println("\n8. Starting HTTP server with metrics endpoint...")
	fmt.Println("   Metrics will be available at: http://localhost:8080/metrics")
	fmt.Println("   Example metrics endpoint: http://localhost:8080/info")
	fmt.Println("   Press Ctrl+C to stop the server")

	// Create a simple HTTP server
	mux := http.NewServeMux()

	// Add the Prometheus metrics endpoint
	mux.Handle("/metrics", metricsInstance.Handler())

	// Add an info endpoint
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
	"service": "aether-backend-metrics-example",
	"version": "1.0.0",
	"metrics_endpoint": "/metrics",
	"description": "Example Prometheus metrics for Aether Backend"
}`)
	})

	// Add root endpoint with instructions
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `
<html>
<head><title>Aether Backend Metrics Example</title></head>
<body>
<h1>Aether Backend Metrics Example</h1>
<p>This example demonstrates the Prometheus metrics collection system.</p>
<h2>Available Endpoints:</h2>
<ul>
<li><a href="/metrics">Prometheus Metrics</a> - Raw Prometheus metrics format</li>
<li><a href="/info">Service Info</a> - JSON service information</li>
</ul>
<h2>Sample Metrics Recorded:</h2>
<ul>
<li>HTTP request metrics (requests/second, duration, response sizes)</li>
<li>Database operation metrics (Neo4j and Redis)</li>
<li>Application metrics (documents, users, notebooks)</li>
<li>Storage operation metrics</li>
<li>External service metrics (Keycloak, S3)</li>
<li>System metrics (goroutines, memory, connections)</li>
</ul>
<p>You can use tools like Prometheus and Grafana to scrape and visualize these metrics.</p>
</body>
</html>`)
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Start server
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Simulate some ongoing metrics collection
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		counter := 0
		for {
			select {
			case <-ticker.C:
				counter++
				// Simulate ongoing HTTP requests
				metricsInstance.RecordHTTPRequest(
					"GET", "/api/v1/health", 200,
					time.Duration(10+counter*2)*time.Millisecond, 0, 64,
				)

				// Simulate ongoing database operations
				metricsInstance.RecordDBQuery(
					"neo4j", "HEALTH_CHECK", "success",
					time.Duration(5+counter)*time.Millisecond,
				)

				fmt.Printf("   [%d] Recorded periodic metrics\n", counter)

			case <-ctx.Done():
				return
			}
		}
	}()

	// Keep server running until interrupted
	select {}
}
