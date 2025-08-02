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

// HealthHandler handles health check requests
type HealthHandler struct {
	neo4j          *database.Neo4jClient
	redis          *database.RedisClient
	storageService *services.S3StorageService
	kafkaService   *services.KafkaService
	logger         *logger.Logger
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(
	neo4j *database.Neo4jClient,
	redis *database.RedisClient,
	storageService *services.S3StorageService,
	kafkaService *services.KafkaService,
	log *logger.Logger,
) *HealthHandler {
	return &HealthHandler{
		neo4j:          neo4j,
		redis:          redis,
		storageService: storageService,
		kafkaService:   kafkaService,
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

// LivenessCheck handles liveness probe
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
// @Description Check if the application is ready to serve requests
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

	allHealthy := true

	// Check Neo4j
	neo4jHealth := h.checkNeo4j(ctx)
	response.Services["neo4j"] = neo4jHealth
	if neo4jHealth.Status != "healthy" {
		allHealthy = false
	}

	// Check Redis
	redisHealth := h.checkRedis(ctx)
	response.Services["redis"] = redisHealth
	if redisHealth.Status != "healthy" {
		allHealthy = false
	}

	// Check Storage Service
	if h.storageService != nil {
		storageHealth := h.checkStorage(ctx)
		response.Services["storage"] = storageHealth
		if storageHealth.Status != "healthy" {
			allHealthy = false
		}
	}

	// Check Kafka Service
	if h.kafkaService != nil {
		kafkaHealth := h.checkKafka(ctx)
		response.Services["kafka"] = kafkaHealth
		if kafkaHealth.Status != "healthy" {
			allHealthy = false
		}
	}

	if allHealthy {
		response.Status = "ready"
		c.JSON(http.StatusOK, response)
	} else {
		response.Status = "not_ready"
		c.JSON(http.StatusServiceUnavailable, response)
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

func (h *HealthHandler) checkRedis(ctx context.Context) ServiceHealth {
	start := time.Now()

	err := h.redis.HealthCheck(ctx)
	responseTime := time.Since(start)

	if err != nil {
		h.logger.Error("Redis health check failed", zap.Error(err))
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

func (h *HealthHandler) checkStorage(ctx context.Context) ServiceHealth {
	start := time.Now()

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
