package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"go.uber.org/zap"
)

// ArgoHandler handles Argo Events API endpoints
type ArgoHandler struct {
	argoService *services.ArgoService
	logger      *zap.Logger
}

// NewArgoHandler creates a new Argo handler
func NewArgoHandler(argoService *services.ArgoService, log *logger.Logger) *ArgoHandler {
	return &ArgoHandler{
		argoService: argoService,
		logger:      log.Logger,
	}
}

// ListEventSources returns available Argo event sources
func (h *ArgoHandler) ListEventSources(c *gin.Context) {
	sources, err := h.argoService.ListEventSources(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to list Argo event sources", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list event sources"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": sources})
}

// ListSensors returns available Argo sensors
func (h *ArgoHandler) ListSensors(c *gin.Context) {
	sensors, err := h.argoService.ListSensors(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to list Argo sensors", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list sensors"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": sensors})
}
