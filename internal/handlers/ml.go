package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// MLHandler handles machine learning model and experiment HTTP requests
type MLHandler struct {
	mlService *services.MLService
	logger    *logger.Logger
}

// NewMLHandler creates a new ML handler
func NewMLHandler(mlService *services.MLService, log *logger.Logger) *MLHandler {
	return &MLHandler{
		mlService: mlService,
		logger:    log.WithService("ml_handler"),
	}
}

// CreateModel creates a new ML model
// @Summary Create ML model
// @Description Create a new machine learning model
// @Tags ml
// @Accept json
// @Produce json
// @Security Bearer
// @Param model body models.CreateMLModelRequest true "Model creation request"
// @Success 201 {object} models.MLModel
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/models [post]
func (h *MLHandler) CreateModel(c *gin.Context) {
	var req models.CreateMLModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request body", err))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Creating ML model", 
		zap.String("name", req.Name), 
		zap.String("type", req.Type),
		zap.String("user_id", userID))

	model, err := h.mlService.CreateModel(c.Request.Context(), req, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to create ML model", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to create ML model"))
		return
	}

	c.JSON(http.StatusCreated, model)
}

// GetModels retrieves ML models
// @Summary Get ML models
// @Description Get a list of ML models for the current tenant
// @Tags ml
// @Produce json
// @Security Bearer
// @Param limit query int false "Number of models to return" default(10)
// @Param offset query int false "Number of models to skip" default(0)
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/models [get]
func (h *MLHandler) GetModels(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Parse pagination parameters
	limit := 10
	offset := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	h.logger.Info("Getting ML models", 
		zap.String("user_id", userID), 
		zap.Int("limit", limit), 
		zap.Int("offset", offset))

	models, total, err := h.mlService.GetModels(c.Request.Context(), spaceContext, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get ML models", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get ML models"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"models": models,
		"pagination": gin.H{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// GetModel retrieves a specific ML model
// @Summary Get ML model
// @Description Get a specific ML model by ID
// @Tags ml
// @Produce json
// @Security Bearer
// @Param id path string true "Model ID"
// @Success 200 {object} models.MLModel
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/models/{id} [get]
func (h *MLHandler) GetModel(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Model ID is required", nil))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Getting ML model", zap.String("model_id", modelID), zap.String("user_id", userID))

	model, err := h.mlService.GetModelByID(c.Request.Context(), modelID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get ML model", zap.String("model_id", modelID), zap.Error(err))
		c.JSON(http.StatusNotFound, errors.NotFound("ML model not found"))
		return
	}

	c.JSON(http.StatusOK, model)
}

// UpdateModel updates an existing ML model
// @Summary Update ML model
// @Description Update an existing ML model
// @Tags ml
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Model ID"
// @Param model body models.UpdateMLModelRequest true "Model update request"
// @Success 200 {object} models.MLModel
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/models/{id} [put]
func (h *MLHandler) UpdateModel(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Model ID is required", nil))
		return
	}

	var req models.UpdateMLModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request body", err))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Updating ML model", zap.String("model_id", modelID), zap.String("user_id", userID))

	model, err := h.mlService.UpdateModel(c.Request.Context(), modelID, req, spaceContext)
	if err != nil {
		h.logger.Error("Failed to update ML model", zap.String("model_id", modelID), zap.Error(err))
		if err.Error() == "ML model not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("ML model not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to update ML model"))
		}
		return
	}

	c.JSON(http.StatusOK, model)
}

// DeleteModel deletes an ML model
// @Summary Delete ML model
// @Description Delete an ML model
// @Tags ml
// @Security Bearer
// @Param id path string true "Model ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/models/{id} [delete]
func (h *MLHandler) DeleteModel(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Model ID is required", nil))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Deleting ML model", zap.String("model_id", modelID), zap.String("user_id", userID))

	err = h.mlService.DeleteModel(c.Request.Context(), modelID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to delete ML model", zap.String("model_id", modelID), zap.Error(err))
		if err.Error() == "ML model not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("ML model not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to delete ML model"))
		}
		return
	}

	c.Status(http.StatusNoContent)
}

// DeployModel deploys an ML model
// @Summary Deploy ML model
// @Description Deploy an ML model to production
// @Tags ml
// @Security Bearer
// @Param id path string true "Model ID"
// @Success 200 {object} models.MLModel
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/models/{id}/deploy [post]
func (h *MLHandler) DeployModel(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Model ID is required", nil))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Deploying ML model", zap.String("model_id", modelID), zap.String("user_id", userID))

	model, err := h.mlService.DeployModel(c.Request.Context(), modelID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to deploy ML model", zap.String("model_id", modelID), zap.Error(err))
		if err.Error() == "ML model not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("ML model not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to deploy ML model"))
		}
		return
	}

	c.JSON(http.StatusOK, model)
}

// CreateExperiment creates a new ML experiment
// @Summary Create ML experiment
// @Description Create a new machine learning experiment
// @Tags ml
// @Accept json
// @Produce json
// @Security Bearer
// @Param experiment body models.CreateExperimentRequest true "Experiment creation request"
// @Success 201 {object} models.MLExperiment
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/experiments [post]
func (h *MLHandler) CreateExperiment(c *gin.Context) {
	var req models.CreateExperimentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request body", err))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Creating ML experiment", 
		zap.String("name", req.Name), 
		zap.String("model_id", req.ModelID),
		zap.String("user_id", userID))

	experiment, err := h.mlService.CreateExperiment(c.Request.Context(), req, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to create ML experiment", zap.Error(err))
		if err.Error() == "model not found: ML model not found" {
			c.JSON(http.StatusBadRequest, errors.BadRequest("Model not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to create ML experiment"))
		}
		return
	}

	c.JSON(http.StatusCreated, experiment)
}

// GetExperiments retrieves ML experiments
// @Summary Get ML experiments
// @Description Get a list of ML experiments for the current tenant
// @Tags ml
// @Produce json
// @Security Bearer
// @Param limit query int false "Number of experiments to return" default(10)
// @Param offset query int false "Number of experiments to skip" default(0)
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/experiments [get]
func (h *MLHandler) GetExperiments(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	// Parse pagination parameters
	limit := 10
	offset := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	h.logger.Info("Getting ML experiments", 
		zap.String("user_id", userID), 
		zap.Int("limit", limit), 
		zap.Int("offset", offset))

	experiments, total, err := h.mlService.GetExperiments(c.Request.Context(), spaceContext, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get ML experiments", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get ML experiments"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"experiments": experiments,
		"pagination": gin.H{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// GetExperiment retrieves a specific ML experiment
// @Summary Get ML experiment
// @Description Get a specific ML experiment by ID
// @Tags ml
// @Produce json
// @Security Bearer
// @Param id path string true "Experiment ID"
// @Success 200 {object} models.MLExperiment
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/experiments/{id} [get]
func (h *MLHandler) GetExperiment(c *gin.Context) {
	experimentID := c.Param("id")
	if experimentID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Experiment ID is required", nil))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Getting ML experiment", zap.String("experiment_id", experimentID), zap.String("user_id", userID))

	experiment, err := h.mlService.GetExperimentByID(c.Request.Context(), experimentID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get ML experiment", zap.String("experiment_id", experimentID), zap.Error(err))
		c.JSON(http.StatusNotFound, errors.NotFound("ML experiment not found"))
		return
	}

	c.JSON(http.StatusOK, experiment)
}

// UpdateExperiment updates an existing ML experiment
// @Summary Update ML experiment
// @Description Update an existing ML experiment
// @Tags ml
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Experiment ID"
// @Param experiment body models.UpdateExperimentRequest true "Experiment update request"
// @Success 200 {object} models.MLExperiment
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/experiments/{id} [put]
func (h *MLHandler) UpdateExperiment(c *gin.Context) {
	experimentID := c.Param("id")
	if experimentID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Experiment ID is required", nil))
		return
	}

	var req models.UpdateExperimentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.Validation("Invalid request body", err))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Updating ML experiment", zap.String("experiment_id", experimentID), zap.String("user_id", userID))

	experiment, err := h.mlService.UpdateExperiment(c.Request.Context(), experimentID, req, spaceContext)
	if err != nil {
		h.logger.Error("Failed to update ML experiment", zap.String("experiment_id", experimentID), zap.Error(err))
		if err.Error() == "ML experiment not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("ML experiment not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to update ML experiment"))
		}
		return
	}

	c.JSON(http.StatusOK, experiment)
}

// DeleteExperiment deletes an ML experiment
// @Summary Delete ML experiment
// @Description Delete an ML experiment
// @Tags ml
// @Security Bearer
// @Param id path string true "Experiment ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/experiments/{id} [delete]
func (h *MLHandler) DeleteExperiment(c *gin.Context) {
	experimentID := c.Param("id")
	if experimentID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Experiment ID is required", nil))
		return
	}

	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	h.logger.Info("Deleting ML experiment", zap.String("experiment_id", experimentID), zap.String("user_id", userID))

	err = h.mlService.DeleteExperiment(c.Request.Context(), experimentID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to delete ML experiment", zap.String("experiment_id", experimentID), zap.Error(err))
		if err.Error() == "ML experiment not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("ML experiment not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to delete ML experiment"))
		}
		return
	}

	c.Status(http.StatusNoContent)
}

// GetAnalytics retrieves ML performance analytics
// @Summary Get ML analytics
// @Description Get ML performance analytics for the current tenant
// @Tags ml
// @Produce json
// @Security Bearer
// @Param period query string false "Analytics period" default(monthly) Enums(daily, weekly, monthly)
// @Success 200 {object} models.MLPerformanceMetrics
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/ml/analytics [get]
func (h *MLHandler) GetAnalytics(c *gin.Context) {
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// Get space context
	spaceContext, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required"))
		return
	}

	period := c.DefaultQuery("period", "monthly")
	if period != "daily" && period != "weekly" && period != "monthly" {
		period = "monthly"
	}

	h.logger.Info("Getting ML analytics", zap.String("user_id", userID), zap.String("period", period))

	analytics, err := h.mlService.GetAnalytics(c.Request.Context(), spaceContext, period)
	if err != nil {
		h.logger.Error("Failed to get ML analytics", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get ML analytics"))
		return
	}

	c.JSON(http.StatusOK, analytics)
}