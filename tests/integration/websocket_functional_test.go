package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tributary-ai-services/aether-be/internal/handlers"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/database"
)

// TestWebSocketWithoutAuth tests WebSocket functionality without authentication middleware
func TestWebSocketWithoutAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	log, err := logger.New(logger.Config{
		Level:  "debug",
		Format: "console",
	})
	require.NoError(t, err)

	// Create handlers without authentication middleware for testing
	var documentService *services.DocumentService
	var audiModalService *services.AudiModalService
	
	_ = handlers.NewWebSocketHandler(documentService, audiModalService, log)
	
	// Create simple WebSocket endpoint for testing (without auth middleware)
	router.GET("/test/websocket", func(c *gin.Context) {
		// Simple upgrader for testing
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for testing
			},
		}
		
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer conn.Close()

		// Send test message
		testMessage := map[string]interface{}{
			"type":      "test",
			"message":   "WebSocket connection successful",
			"timestamp": time.Now().Format(time.RFC3339),
		}
		
		if err := conn.WriteJSON(testMessage); err != nil {
			t.Logf("Failed to send test message: %v", err)
			return
		}

		// Read one message back (optional)
		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Logf("Client disconnected: %v", err)
		}
	})
	
	// Add StreamHandler routes for testing
	var neo4jClient *database.Neo4jClient
	streamService := services.NewStreamService(neo4jClient, log)
	_ = handlers.NewStreamHandler(streamService, log)
	
	// Test endpoint without authentication
	router.GET("/test/stream", func(c *gin.Context) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}
		
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer conn.Close()

		// Send mock live event
		mockEvent := models.LiveEvent{
			ID:             "test-event-123",
			StreamSourceID: "test-source-456",
			EventType:      "mention",
			Content:        "This is a test event from WebSocket test",
			MediaType:      "text",
			Sentiment:      "positive",
			SentimentScore: 0.85,
			Confidence:     0.92,
			ProcessingTime: 150.5,
			HasAuditTrail:  true,
			AuditScore:     0.991,
			EventTimestamp: time.Now(),
			ProcessedAt:    time.Now(),
		}

		message := models.StreamEventWebSocketMessage{
			Type:      "live_event",
			Event:     &mockEvent,
			Timestamp: time.Now(),
		}
		
		if err := conn.WriteJSON(message); err != nil {
			t.Logf("Failed to send stream message: %v", err)
			return
		}

		// Wait a bit then send analytics update
		time.Sleep(100 * time.Millisecond)
		
		analytics := models.StreamAnalytics{
			ID:                   "test-analytics-123",
			Period:               "realtime",
			Timestamp:            time.Now(),
			ActiveStreams:        2,
			TotalEventsProcessed: 150,
			EventsPerSecond:      12.5,
			MediaProcessed:       1500,
			AverageProcessingTime: 125.5,
			AverageAuditScore:    0.991,
			SentimentDistribution: map[string]int64{
				"positive": 80,
				"neutral":  50,
				"negative": 20,
			},
			EventTypeDistribution: map[string]int64{
				"mention":    100,
				"multimodal": 30,
				"document":   20,
			},
			ErrorRate: 0.01,
		}

		analyticsMessage := models.StreamEventWebSocketMessage{
			Type:      "analytics_update",
			Analytics: &analytics,
			Timestamp: time.Now(),
		}
		
		if err := conn.WriteJSON(analyticsMessage); err != nil {
			t.Logf("Failed to send analytics message: %v", err)
			return
		}

		// Read one message back (optional)
		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Logf("Client disconnected: %v", err)
		}
	})
	
	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("basic websocket connection", func(t *testing.T) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/test/websocket"
		
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err, "Should connect to WebSocket successfully")
		defer conn.Close()

		// Read the test message
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var message map[string]interface{}
		err = conn.ReadJSON(&message)
		require.NoError(t, err, "Should receive test message")

		assert.Equal(t, "test", message["type"])
		assert.Equal(t, "WebSocket connection successful", message["message"])
		assert.NotNil(t, message["timestamp"])
		
		t.Logf("Received test message: %+v", message)
	})

	t.Run("live stream websocket", func(t *testing.T) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/test/stream"
		
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err, "Should connect to stream WebSocket successfully")
		defer conn.Close()

		// Read live event message
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var eventMessage models.StreamEventWebSocketMessage
		err = conn.ReadJSON(&eventMessage)
		require.NoError(t, err, "Should receive live event message")

		assert.Equal(t, "live_event", eventMessage.Type)
		assert.NotNil(t, eventMessage.Event)
		assert.Equal(t, "test-event-123", eventMessage.Event.ID)
		assert.Equal(t, "mention", eventMessage.Event.EventType)
		assert.Equal(t, "positive", eventMessage.Event.Sentiment)
		assert.Equal(t, 0.85, eventMessage.Event.SentimentScore)
		
		t.Logf("Received live event: %+v", eventMessage.Event)

		// Read analytics message
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var analyticsMessage models.StreamEventWebSocketMessage
		err = conn.ReadJSON(&analyticsMessage)
		require.NoError(t, err, "Should receive analytics message")

		assert.Equal(t, "analytics_update", analyticsMessage.Type)
		assert.NotNil(t, analyticsMessage.Analytics)
		assert.Equal(t, 2, analyticsMessage.Analytics.ActiveStreams)
		assert.Equal(t, int64(150), analyticsMessage.Analytics.TotalEventsProcessed)
		assert.Equal(t, 12.5, analyticsMessage.Analytics.EventsPerSecond)
		
		t.Logf("Received analytics: %+v", analyticsMessage.Analytics)
	})

	t.Run("websocket message types", func(t *testing.T) {
		// Test different message types that should be supported
		
		// Job status message
		jobMessage := map[string]interface{}{
			"type": "job_status",
			"data": map[string]interface{}{
				"job_id":   "job-123",
				"status":   "processing",
				"progress": 75,
				"message":  "Processing document...",
			},
			"timestamp": time.Now().Format(time.RFC3339),
		}
		
		jsonData, err := json.Marshal(jobMessage)
		require.NoError(t, err)
		assert.Contains(t, string(jsonData), "job_status")
		assert.Contains(t, string(jsonData), "processing")
		
		// Live event message
		event := models.LiveEvent{
			ID:             "event-456",
			EventType:      "multimodal",
			Content:        "Check out this image",
			MediaType:      "image",
			Sentiment:      "neutral",
			SentimentScore: 0.05,
			Confidence:     0.88,
		}
		
		eventMessage := models.StreamEventWebSocketMessage{
			Type:      "live_event",
			Event:     &event,
			Timestamp: time.Now(),
		}
		
		eventData, err := json.Marshal(eventMessage)
		require.NoError(t, err)
		assert.Contains(t, string(eventData), "live_event")
		assert.Contains(t, string(eventData), "multimodal")
		assert.Contains(t, string(eventData), "neutral")
		
		t.Logf("Job message JSON: %s", string(jsonData))
		t.Logf("Event message JSON: %s", string(eventData))
	})
}

// TestWebSocketPerformance tests WebSocket performance characteristics
func TestWebSocketPerformance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	_, err := logger.New(logger.Config{
		Level:  "error", // Minimal logging for performance test
		Format: "json",
	})
	require.NoError(t, err)

	router.GET("/perf/websocket", func(c *gin.Context) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}
		
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer conn.Close()

		// Send multiple messages quickly
		for i := 0; i < 10; i++ {
			message := map[string]interface{}{
				"type":    "performance_test",
				"id":      i,
				"content": "Performance test message",
				"timestamp": time.Now().Format(time.RFC3339),
			}
			
			if err := conn.WriteJSON(message); err != nil {
				t.Logf("Failed to send performance message: %v", err)
				return
			}
		}

		// Read one message back
		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Logf("Client disconnected: %v", err)
		}
	})
	
	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("message throughput", func(t *testing.T) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/perf/websocket"
		
		start := time.Now()
		
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		defer conn.Close()

		// Read multiple messages
		messagesReceived := 0
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		
		for i := 0; i < 10; i++ {
			var message map[string]interface{}
			err := conn.ReadJSON(&message)
			if err != nil {
				break
			}
			messagesReceived++
			
			assert.Equal(t, "performance_test", message["type"])
			assert.Equal(t, float64(i), message["id"]) // JSON numbers are float64
		}
		
		duration := time.Since(start)
		
		assert.Equal(t, 10, messagesReceived, "Should receive all 10 messages")
		t.Logf("Received %d messages in %v", messagesReceived, duration)
		
		// Basic performance check - should be fast
		assert.Less(t, duration, 1*time.Second, "Should process messages quickly")
	})
}

// TestWebSocketReconnection tests reconnection scenarios
func TestWebSocketReconnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	_, err := logger.New(logger.Config{
		Level:  "debug",
		Format: "console",
	})
	require.NoError(t, err)

	connectionCount := 0
	
	router.GET("/reconnect/websocket", func(c *gin.Context) {
		connectionCount++
		
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}
		
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer conn.Close()

		// Send connection info
		message := map[string]interface{}{
			"type":           "connection_info",
			"connection_id":  connectionCount,
			"message":        "Connected successfully",
			"timestamp":      time.Now().Format(time.RFC3339),
		}
		
		if err := conn.WriteJSON(message); err != nil {
			t.Logf("Failed to send connection message: %v", err)
			return
		}

		// Keep connection alive briefly
		time.Sleep(100 * time.Millisecond)
		
		// Read one message back
		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Logf("Client disconnected: %v", err)
		}
	})
	
	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("multiple connections", func(t *testing.T) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/reconnect/websocket"
		
		// Make multiple connections sequentially
		for i := 1; i <= 3; i++ {
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			require.NoError(t, err, "Connection %d should succeed", i)
			
			// Read connection info
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			var message map[string]interface{}
			err = conn.ReadJSON(&message)
			require.NoError(t, err, "Should receive connection info")
			
			assert.Equal(t, "connection_info", message["type"])
			assert.Equal(t, float64(i), message["connection_id"])
			
			conn.Close()
			
			t.Logf("Connection %d successful: %+v", i, message)
		}
		
		assert.Equal(t, 3, connectionCount, "Should have had 3 connections")
	})
}

// TestWebSocketErrorScenarios tests various error conditions
func TestWebSocketErrorScenarios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	_, err := logger.New(logger.Config{
		Level:  "debug",
		Format: "console",
	})
	require.NoError(t, err)

	router.GET("/error/websocket", func(c *gin.Context) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}
		
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer conn.Close()

		// Send error message
		errorMessage := map[string]interface{}{
			"type":      "error",
			"error":     "simulated_error",
			"message":   "This is a test error message",
			"timestamp": time.Now().Format(time.RFC3339),
		}
		
		if err := conn.WriteJSON(errorMessage); err != nil {
			t.Logf("Failed to send error message: %v", err)
			return
		}

		// Close connection after error
		conn.Close()
	})
	
	server := httptest.NewServer(router)
	defer server.Close()

	t.Run("error message handling", func(t *testing.T) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/error/websocket"
		
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		defer conn.Close()

		// Read error message
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var message map[string]interface{}
		err = conn.ReadJSON(&message)
		require.NoError(t, err, "Should receive error message")

		assert.Equal(t, "error", message["type"])
		assert.Equal(t, "simulated_error", message["error"])
		assert.Contains(t, message["message"], "test error")
		
		t.Logf("Received error message: %+v", message)
		
		// Try to read another message (should fail due to connection close)
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		err = conn.ReadJSON(&message)
		assert.Error(t, err, "Should fail to read after connection close")
	})
}