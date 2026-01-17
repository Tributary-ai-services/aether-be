package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/Tributary-ai-services/aether-be/internal/models"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for demo
	},
}

func main() {
	fmt.Println("🚀 Starting Aether WebSocket Demo Server...")
	
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Serve demo HTML page
	router.GET("/", serveDemoPage)
	
	// WebSocket endpoints for demo
	router.GET("/demo/job-status", handleJobStatusDemo)
	router.GET("/demo/live-stream", handleLiveStreamDemo)
	router.GET("/demo/document-status", handleDocumentStatusDemo)

	fmt.Println("📡 WebSocket Demo Server running on: http://localhost:8888")
	fmt.Println("🌐 Open your browser to http://localhost:8888 to see the demo")
	fmt.Println("")
	fmt.Println("Available WebSocket endpoints:")
	fmt.Println("  • ws://localhost:8888/demo/job-status - Job progress tracking")
	fmt.Println("  • ws://localhost:8888/demo/live-stream - Live event streaming")
	fmt.Println("  • ws://localhost:8888/demo/document-status - Document processing")
	fmt.Println("")
	
	log.Fatal(http.ListenAndServe(":8888", router))
}

func serveDemoPage(c *gin.Context) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Aether WebSocket Demo</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; }
        .demo-section { background: white; margin: 20px 0; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .status { padding: 10px; margin: 10px 0; border-radius: 4px; }
        .connected { background: #d4edda; color: #155724; border: 1px solid #c3e6cb; }
        .disconnected { background: #f8d7da; color: #721c24; border: 1px solid #f5c6cb; }
        .message { background: #e2f3ff; padding: 10px; margin: 5px 0; border-left: 4px solid #007bff; font-family: monospace; }
        button { background: #007bff; color: white; border: none; padding: 10px 20px; margin: 5px; border-radius: 4px; cursor: pointer; }
        button:hover { background: #0056b3; }
        .metrics { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 15px; margin: 15px 0; }
        .metric { background: #f8f9fa; padding: 15px; border-radius: 6px; text-align: center; }
        .metric-value { font-size: 24px; font-weight: bold; color: #007bff; }
        .metric-label { font-size: 14px; color: #6c757d; margin-top: 5px; }
        .progress-bar { width: 100%; height: 20px; background: #e9ecef; border-radius: 10px; overflow: hidden; margin: 10px 0; }
        .progress-fill { height: 100%; background: linear-gradient(90deg, #28a745, #20c997); transition: width 0.3s ease; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚀 Aether Enterprise AI Platform - WebSocket Demo</h1>
        <p>This demo shows real-time WebSocket communication for job tracking, live streaming, and document processing.</p>

        <!-- Job Status Demo -->
        <div class="demo-section">
            <h2>📊 Job Status Tracking</h2>
            <p>Demonstrates real-time job progress updates with WebSocket streaming.</p>
            <div id="job-status" class="status disconnected">Disconnected</div>
            <div class="progress-bar">
                <div id="job-progress" class="progress-fill" style="width: 0%"></div>
            </div>
            <div id="job-progress-text">0% Complete</div>
            <button onclick="connectJobStatus()">Connect Job Stream</button>
            <button onclick="disconnectJobStatus()">Disconnect</button>
            <div id="job-messages"></div>
        </div>

        <!-- Live Stream Demo -->
        <div class="demo-section">
            <h2>📡 Live Event Streaming</h2>
            <p>Shows real-time social media monitoring and analytics with sentiment analysis.</p>
            <div id="stream-status" class="status disconnected">Disconnected</div>
            <div class="metrics">
                <div class="metric">
                    <div id="events-count" class="metric-value">0</div>
                    <div class="metric-label">Events Processed</div>
                </div>
                <div class="metric">
                    <div id="events-per-sec" class="metric-value">0.0</div>
                    <div class="metric-label">Events/Sec</div>
                </div>
                <div class="metric">
                    <div id="active-streams" class="metric-value">0</div>
                    <div class="metric-label">Active Streams</div>
                </div>
                <div class="metric">
                    <div id="audit-score" class="metric-value">99.1%</div>
                    <div class="metric-label">Audit Score</div>
                </div>
            </div>
            <button onclick="connectLiveStream()">Connect Live Stream</button>
            <button onclick="disconnectLiveStream()">Disconnect</button>
            <div id="stream-messages"></div>
        </div>

        <!-- Document Processing Demo -->
        <div class="demo-section">
            <h2>📄 Document Processing Status</h2>
            <p>Tracks document upload and AI processing progress in real-time.</p>
            <div id="doc-status" class="status disconnected">Disconnected</div>
            <div class="progress-bar">
                <div id="doc-progress" class="progress-fill" style="width: 0%"></div>
            </div>
            <div id="doc-progress-text">Waiting for document...</div>
            <button onclick="connectDocumentStatus()">Connect Document Stream</button>
            <button onclick="disconnectDocumentStatus()">Disconnect</button>
            <div id="doc-messages"></div>
        </div>
    </div>

    <script>
        let jobSocket = null;
        let streamSocket = null;
        let docSocket = null;
        let eventsCount = 0;

        function addMessage(containerId, message) {
            const container = document.getElementById(containerId);
            const div = document.createElement('div');
            div.className = 'message';
            div.innerHTML = '<strong>' + new Date().toLocaleTimeString() + '</strong>: ' + JSON.stringify(message, null, 2);
            container.appendChild(div);
            container.scrollTop = container.scrollHeight;
            
            // Keep only last 10 messages
            while (container.children.length > 10) {
                container.removeChild(container.firstChild);
            }
        }

        // Job Status WebSocket
        function connectJobStatus() {
            if (jobSocket) jobSocket.close();
            
            jobSocket = new WebSocket('ws://localhost:8888/demo/job-status');
            
            jobSocket.onopen = function() {
                document.getElementById('job-status').textContent = 'Connected - Monitoring job progress';
                document.getElementById('job-status').className = 'status connected';
            };
            
            jobSocket.onmessage = function(event) {
                const data = JSON.parse(event.data);
                addMessage('job-messages', data);
                
                if (data.type === 'job_status' && data.data) {
                    const progress = data.data.progress || 0;
                    document.getElementById('job-progress').style.width = progress + '%';
                    document.getElementById('job-progress-text').textContent = progress + '% Complete - ' + (data.data.message || 'Processing...');
                }
            };
            
            jobSocket.onclose = function() {
                document.getElementById('job-status').textContent = 'Disconnected';
                document.getElementById('job-status').className = 'status disconnected';
            };
        }

        function disconnectJobStatus() {
            if (jobSocket) {
                jobSocket.close();
                jobSocket = null;
            }
        }

        // Live Stream WebSocket
        function connectLiveStream() {
            if (streamSocket) streamSocket.close();
            
            streamSocket = new WebSocket('ws://localhost:8888/demo/live-stream');
            
            streamSocket.onopen = function() {
                document.getElementById('stream-status').textContent = 'Connected - Receiving live events';
                document.getElementById('stream-status').className = 'status connected';
            };
            
            streamSocket.onmessage = function(event) {
                const data = JSON.parse(event.data);
                addMessage('stream-messages', data);
                
                if (data.type === 'live_event' && data.event) {
                    eventsCount++;
                    document.getElementById('events-count').textContent = eventsCount;
                }
                
                if (data.type === 'analytics_update' && data.analytics) {
                    const analytics = data.analytics;
                    document.getElementById('events-per-sec').textContent = analytics.events_per_second.toFixed(1);
                    document.getElementById('active-streams').textContent = analytics.active_streams;
                    document.getElementById('audit-score').textContent = (analytics.average_audit_score * 100).toFixed(1) + '%';
                }
            };
            
            streamSocket.onclose = function() {
                document.getElementById('stream-status').textContent = 'Disconnected';
                document.getElementById('stream-status').className = 'status disconnected';
            };
        }

        function disconnectLiveStream() {
            if (streamSocket) {
                streamSocket.close();
                streamSocket = null;
            }
        }

        // Document Status WebSocket
        function connectDocumentStatus() {
            if (docSocket) docSocket.close();
            
            docSocket = new WebSocket('ws://localhost:8888/demo/document-status');
            
            docSocket.onopen = function() {
                document.getElementById('doc-status').textContent = 'Connected - Monitoring document processing';
                document.getElementById('doc-status').className = 'status connected';
            };
            
            docSocket.onmessage = function(event) {
                const data = JSON.parse(event.data);
                addMessage('doc-messages', data);
                
                if (data.type === 'document_status' && data.data) {
                    const progress = data.data.progress || 0;
                    document.getElementById('doc-progress').style.width = progress + '%';
                    document.getElementById('doc-progress-text').textContent = data.data.message || 'Processing...';
                }
            };
            
            docSocket.onclose = function() {
                document.getElementById('doc-status').textContent = 'Disconnected';
                document.getElementById('doc-status').className = 'status disconnected';
            };
        }

        function disconnectDocumentStatus() {
            if (docSocket) {
                docSocket.close();
                docSocket = null;
            }
        }

        // Auto-connect on page load for demo
        setTimeout(() => {
            connectJobStatus();
            connectLiveStream();
            connectDocumentStatus();
        }, 1000);
    </script>
</body>
</html>`
	
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, html)
}

func handleJobStatusDemo(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Println("Job status demo client connected")

	// Simulate job progress
	go func() {
		for progress := 0; progress <= 100; progress += 5 {
			status := "processing"
			message := "Processing document..."
			
			if progress == 100 {
				status = "completed"
				message = "Document processing completed successfully!"
			}
			
			jobUpdate := map[string]interface{}{
				"type": "job_status",
				"data": map[string]interface{}{
					"job_id":   "demo-job-123",
					"status":   status,
					"progress": progress,
					"message":  message,
				},
				"timestamp": time.Now().Format(time.RFC3339),
			}
			
			if err := conn.WriteJSON(jobUpdate); err != nil {
				log.Printf("Error sending job update: %v", err)
				return
			}
			
			time.Sleep(2 * time.Second)
		}
	}()

	// Keep connection alive
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Job status client disconnected: %v", err)
			break
		}
	}
}

func handleLiveStreamDemo(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Println("Live stream demo client connected")

	// Simulate live events and analytics
	go func() {
		eventTypes := []string{"mention", "multimodal", "document", "video"}
		sentiments := []string{"positive", "neutral", "negative"}
		sentimentScores := map[string]float64{"positive": 0.85, "neutral": 0.05, "negative": -0.75}
		
		eventsProcessed := 0
		
		for {
			// Send live event
			eventType := eventTypes[eventsProcessed%len(eventTypes)]
			sentiment := sentiments[eventsProcessed%len(sentiments)]
			
			event := models.LiveEvent{
				ID:             fmt.Sprintf("demo-event-%d", eventsProcessed+1),
				StreamSourceID: "demo-source-twitter",
				EventType:      eventType,
				Content:        fmt.Sprintf("Demo %s event #%d with %s sentiment", eventType, eventsProcessed+1, sentiment),
				MediaType:      "text",
				Sentiment:      sentiment,
				SentimentScore: sentimentScores[sentiment],
				Confidence:     0.92,
				ProcessingTime: 125.5,
				HasAuditTrail:  true,
				AuditScore:     0.991,
				EventTimestamp: time.Now(),
				ProcessedAt:    time.Now(),
			}

			eventMessage := models.StreamEventWebSocketMessage{
				Type:      "live_event",
				Event:     &event,
				Timestamp: time.Now(),
			}
			
			if err := conn.WriteJSON(eventMessage); err != nil {
				log.Printf("Error sending live event: %v", err)
				return
			}
			
			eventsProcessed++
			
			// Send analytics update every 5 events
			if eventsProcessed%5 == 0 {
				analytics := models.StreamAnalytics{
					ID:                   fmt.Sprintf("demo-analytics-%d", eventsProcessed),
					Period:               "realtime",
					Timestamp:            time.Now(),
					ActiveStreams:        3,
					TotalEventsProcessed: int64(eventsProcessed),
					EventsPerSecond:      float64(eventsProcessed) / 10.0, // Rough calculation
					MediaProcessed:       2400000 + int64(eventsProcessed*100),
					AverageProcessingTime: 125.5,
					AverageAuditScore:    0.991,
					SentimentDistribution: map[string]int64{
						"positive": int64(eventsProcessed / 3),
						"neutral":  int64(eventsProcessed / 3),
						"negative": int64(eventsProcessed / 3),
					},
					EventTypeDistribution: map[string]int64{
						"mention":    int64(eventsProcessed / 2),
						"multimodal": int64(eventsProcessed / 4),
						"document":   int64(eventsProcessed / 8),
					},
					ErrorRate: 0.01,
				}

				analyticsMessage := models.StreamEventWebSocketMessage{
					Type:      "analytics_update",
					Analytics: &analytics,
					Timestamp: time.Now(),
				}
				
				if err := conn.WriteJSON(analyticsMessage); err != nil {
					log.Printf("Error sending analytics: %v", err)
					return
				}
			}
			
			time.Sleep(3 * time.Second)
		}
	}()

	// Keep connection alive
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Live stream client disconnected: %v", err)
			break
		}
	}
}

func handleDocumentStatusDemo(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Println("Document status demo client connected")

	// Simulate document processing
	go func() {
		stages := []struct {
			progress int
			message  string
			delay    time.Duration
		}{
			{0, "Document uploaded successfully", 1 * time.Second},
			{10, "Analyzing document structure...", 2 * time.Second},
			{25, "Extracting text content...", 3 * time.Second},
			{45, "Running AI analysis...", 4 * time.Second},
			{65, "Processing with AudiModal service...", 3 * time.Second},
			{80, "Generating insights and metadata...", 2 * time.Second},
			{95, "Finalizing results...", 1 * time.Second},
			{100, "Document processing completed! Ready for search and analysis.", 0},
		}
		
		for _, stage := range stages {
			status := "processing"
			if stage.progress == 100 {
				status = "completed"
			}
			
			docUpdate := map[string]interface{}{
				"type": "document_status",
				"data": map[string]interface{}{
					"document_id": "demo-doc-456",
					"status":      status,
					"progress":    stage.progress,
					"message":     stage.message,
					"confidence":  0.95,
				},
				"timestamp": time.Now().Format(time.RFC3339),
			}
			
			if err := conn.WriteJSON(docUpdate); err != nil {
				log.Printf("Error sending document update: %v", err)
				return
			}
			
			if stage.delay > 0 {
				time.Sleep(stage.delay)
			}
		}
	}()

	// Keep connection alive
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Document status client disconnected: %v", err)
			break
		}
	}
}