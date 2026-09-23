package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// HealthHandler handles health check requests.
//
// storageEnabled/kafkaEnabled record whether each optional dependency was
// *configured*, independently of whether its service object was successfully
// constructed. Without them a dependency that failed to initialize simply
// disappeared from the response and the endpoint reported "ready" — a health
// check that stops looking at a subsystem rather than reporting it down
// (OPS-39, same shape as OPS-19/OPS-23).
type HealthHandler struct {
	neo4j          *database.Neo4jClient
	storageService *services.S3StorageService
	kafkaService   *services.KafkaService
	storageEnabled bool
	kafkaEnabled   bool
	logger         *logger.Logger
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(
	neo4j *database.Neo4jClient,
	storageService *services.S3StorageService,
	kafkaService *services.KafkaService,
	storageEnabled bool,
	kafkaEnabled bool,
	log *logger.Logger,
) *HealthHandler {
	return &HealthHandler{
		neo4j:          neo4j,
		storageService: storageService,
		kafkaService:   kafkaService,
		storageEnabled: storageEnabled,
		kafkaEnabled:   kafkaEnabled,
		logger:         log.WithService("health_handler"),
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string                   `json:"status"`
	Timestamp time.Time                `json:"timestamp"`
	Version   string                   `json:"version,omitempty"`
	Services  map[string]ServiceHealth `json:"services"`
}

// ServiceHealth represents individual service health
type ServiceHealth struct {
	Status       string        `json:"status"`
	ResponseTime time.Duration `json:"response_time_ms"`
	Error        string        `json:"error,omitempty"`
}

// LivenessCheck handles liveness probe.
//
// It returns 200 unconditionally and MUST stay that way: a liveness probe
// answers "is this process wedged?", and restarting the pod cannot repair a
// dependency. Wiring livenessProbe to a dependency check turned every Kafka
// blip into an application restart (OPS-39). Dependency state belongs in
// ReadinessCheck.
// @Summary Liveness check
// @Description Check if the application is alive
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Router /health/live [get]
func (h *HealthHandler) LivenessCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "alive",
		"timestamp": time.Now(),
	})
}

// ReadinessCheck handles readiness probe
// @Summary Readiness check
// @Description Check if the application is ready to serve requests. Returns 200 with status "ready" or "degraded" (an optional dependency is down but traffic can still be served), and 503 only when Neo4j is unreachable.
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} HealthResponse
// @Failure 503 {object} HealthResponse
// @Router /health/ready [get]
func (h *HealthHandler) ReadinessCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	response := HealthResponse{
		Timestamp: time.Now(),
		Services:  make(map[string]ServiceHealth),
	}

	// Neo4j is the only dependency that gates readiness: it is the datastore
	// the API cannot answer a request without. Storage and Kafka degrade
	// specific features (file operations, event publishing) but the process
	// still serves traffic, and the app is built to start without them.
	// Failing readiness on those would pull every replica out of the Service
	// for the duration of a dependency blip — an API outage caused by an
	// optional subsystem (OPS-39).
	degraded := false

	neo4jHealth := h.checkNeo4j(ctx)
	response.Services["neo4j"] = neo4jHealth
	ready := neo4jHealth.Status == "healthy"

	// Optional dependencies are always reported when configured, whether or
	// not their service object exists, so a boot-time failure is visible.
	if h.storageEnabled {
		storageHealth := h.checkStorage(ctx)
		response.Services["storage"] = storageHealth
		if storageHealth.Status != "healthy" {
			degraded = true
		}
	}

	if h.kafkaEnabled {
		kafkaHealth := h.checkKafka(ctx)
		response.Services["kafka"] = kafkaHealth
		if kafkaHealth.Status != "healthy" {
			degraded = true
		}
	}

	status, code := readinessVerdict(ready, degraded)
	response.Status = status
	c.JSON(code, response)
}

// readinessVerdict encodes the readiness policy in one testable place.
//
// ready is the state of the gating dependency (Neo4j); degraded is true when
// any optional dependency is unhealthy. An optional dependency must never
// produce a 503, because a 503 removes every replica from the Service and
// turns a subsystem outage into a total API outage (OPS-39).
func readinessVerdict(ready, degraded bool) (status string, code int) {
	switch {
	case !ready:
		return "not_ready", http.StatusServiceUnavailable
	case degraded:
		return "degraded", http.StatusOK
	default:
		return "ready", http.StatusOK
	}
}

// HealthCheck handles comprehensive health check
// @Summary Health check
// @Description Comprehensive health check for all services
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} HealthResponse
// @Failure 503 {object} HealthResponse
// @Router /health [get]
func (h *HealthHandler) HealthCheck(c *gin.Context) {
	// Delegate to readiness check for now
	h.ReadinessCheck(c)
}

func (h *HealthHandler) checkNeo4j(ctx context.Context) ServiceHealth {
	start := time.Now()

	err := h.neo4j.HealthCheck(ctx)
	responseTime := time.Since(start)

	if err != nil {
		h.logger.Error("Neo4j health check failed", zap.Error(err))
		return ServiceHealth{
			Status:       "unhealthy",
			ResponseTime: responseTime,
			Error:        err.Error(),
		}
	}

	return ServiceHealth{
		Status:       "healthy",
		ResponseTime: responseTime,
	}
}

// Redis health check removed - no longer using Redis

func (h *HealthHandler) checkStorage(ctx context.Context) ServiceHealth {
	start := time.Now()

	// Configured but never constructed - report it, don't hide it.
	if h.storageService == nil {
		return ServiceHealth{
			Status: "unhealthy",
			Error:  "storage service is enabled but was not initialized at startup",
		}
	}

	err := h.storageService.HealthCheck(ctx)
	responseTime := time.Since(start)

	if err != nil {
		h.logger.Error("Storage health check failed", zap.Error(err))
		return ServiceHealth{
			Status:       "unhealthy",
			ResponseTime: responseTime,
			Error:        err.Error(),
		}
	}

	return ServiceHealth{
		Status:       "healthy",
		ResponseTime: responseTime,
	}
}

func (h *HealthHandler) checkKafka(ctx context.Context) ServiceHealth {
	start := time.Now()

	// Configured but never constructed - report it, don't hide it.
	if h.kafkaService == nil {
		return ServiceHealth{
			Status: "unhealthy",
			Error:  "kafka service is enabled but was not initialized at startup",
		}
	}

	err := h.kafkaService.HealthCheck(ctx)
	responseTime := time.Since(start)

	if err != nil {
		h.logger.Error("Kafka health check failed", zap.Error(err))
		return ServiceHealth{
			Status:       "unhealthy",
			ResponseTime: responseTime,
			Error:        err.Error(),
		}
	}

	return ServiceHealth{
		Status:       "healthy",
		ResponseTime: responseTime,
	}
}
