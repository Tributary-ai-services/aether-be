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

// WorkflowHandler handles workflow-related HTTP requests
type WorkflowHandler struct {
	workflowService *services.WorkflowService
	logger          *logger.Logger
}

// NewWorkflowHandler creates a new workflow handler
func NewWorkflowHandler(workflowService *services.WorkflowService, log *logger.Logger) *WorkflowHandler {
	return &WorkflowHandler{
		workflowService: workflowService,
		logger:          log.WithService("workflow_handler"),
	}
}

// CreateWorkflow creates a new workflow
// @Summary Create workflow
// @Description Create a new automated workflow
// @Tags workflows
// @Accept json
// @Produce json
// @Security Bearer
// @Param workflow body models.CreateWorkflowRequest true "Workflow creation request"
// @Success 201 {object} models.Workflow
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/workflows [post]
func (h *WorkflowHandler) CreateWorkflow(c *gin.Context) {
	var req models.CreateWorkflowRequest
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

	h.logger.Info("Creating workflow", 
		zap.String("name", req.Name), 
		zap.String("type", req.Type),
		zap.String("user_id", userID))

	workflow, err := h.workflowService.CreateWorkflow(c.Request.Context(), req, userID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to create workflow", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to create workflow"))
		return
	}

	c.JSON(http.StatusCreated, workflow)
}

// GetWorkflows retrieves workflows
// @Summary Get workflows
// @Description Get a list of workflows for the current tenant
// @Tags workflows
// @Produce json
// @Security Bearer
// @Param limit query int false "Number of workflows to return" default(10)
// @Param offset query int false "Number of workflows to skip" default(0)
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/workflows [get]
func (h *WorkflowHandler) GetWorkflows(c *gin.Context) {
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

	h.logger.Info("Getting workflows", 
		zap.String("user_id", userID), 
		zap.Int("limit", limit), 
		zap.Int("offset", offset))

	workflows, total, err := h.workflowService.GetWorkflows(c.Request.Context(), spaceContext, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get workflows", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get workflows"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflows": workflows,
		"pagination": gin.H{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// GetWorkflow retrieves a specific workflow
// @Summary Get workflow
// @Description Get a specific workflow by ID
// @Tags workflows
// @Produce json
// @Security Bearer
// @Param id path string true "Workflow ID"
// @Success 200 {object} models.Workflow
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/workflows/{id} [get]
func (h *WorkflowHandler) GetWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Workflow ID is required", nil))
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

	h.logger.Info("Getting workflow", zap.String("workflow_id", workflowID), zap.String("user_id", userID))

	workflow, err := h.workflowService.GetWorkflowByID(c.Request.Context(), workflowID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to get workflow", zap.String("workflow_id", workflowID), zap.Error(err))
		c.JSON(http.StatusNotFound, errors.NotFound("Workflow not found"))
		return
	}

	c.JSON(http.StatusOK, workflow)
}

// UpdateWorkflow updates an existing workflow
// @Summary Update workflow
// @Description Update an existing workflow
// @Tags workflows
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Workflow ID"
// @Param workflow body models.UpdateWorkflowRequest true "Workflow update request"
// @Success 200 {object} models.Workflow
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/workflows/{id} [put]
func (h *WorkflowHandler) UpdateWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Workflow ID is required", nil))
		return
	}

	var req models.UpdateWorkflowRequest
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

	h.logger.Info("Updating workflow", zap.String("workflow_id", workflowID), zap.String("user_id", userID))

	workflow, err := h.workflowService.UpdateWorkflow(c.Request.Context(), workflowID, req, spaceContext)
	if err != nil {
		h.logger.Error("Failed to update workflow", zap.String("workflow_id", workflowID), zap.Error(err))
		if err.Error() == "workflow not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("Workflow not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to update workflow"))
		}
		return
	}

	c.JSON(http.StatusOK, workflow)
}

// DeleteWorkflow deletes a workflow
// @Summary Delete workflow
// @Description Delete a workflow
// @Tags workflows
// @Security Bearer
// @Param id path string true "Workflow ID"
// @Success 204
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/workflows/{id} [delete]
func (h *WorkflowHandler) DeleteWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Workflow ID is required", nil))
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

	h.logger.Info("Deleting workflow", zap.String("workflow_id", workflowID), zap.String("user_id", userID))

	err = h.workflowService.DeleteWorkflow(c.Request.Context(), workflowID, spaceContext)
	if err != nil {
		h.logger.Error("Failed to delete workflow", zap.String("workflow_id", workflowID), zap.Error(err))
		if err.Error() == "workflow not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("Workflow not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to delete workflow"))
		}
		return
	}

	c.Status(http.StatusNoContent)
}

// ExecuteWorkflow manually executes a workflow
// @Summary Execute workflow
// @Description Manually execute a workflow
// @Tags workflows
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Workflow ID"
// @Param execution body models.ExecuteWorkflowRequest true "Workflow execution request"
// @Success 201 {object} models.WorkflowExecution
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/workflows/{id}/execute [post]
func (h *WorkflowHandler) ExecuteWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Workflow ID is required", nil))
		return
	}

	var req models.ExecuteWorkflowRequest
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

	h.logger.Info("Executing workflow", 
		zap.String("workflow_id", workflowID), 
		zap.String("trigger_id", req.TriggerID),
		zap.String("user_id", userID))

	execution, err := h.workflowService.ExecuteWorkflow(c.Request.Context(), workflowID, req, spaceContext)
	if err != nil {
		h.logger.Error("Failed to execute workflow", zap.String("workflow_id", workflowID), zap.Error(err))
		if err.Error() == "workflow not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("Workflow not found"))
		} else if err.Error() == "workflow is not active" {
			c.JSON(http.StatusBadRequest, errors.BadRequest("Workflow is not active"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to execute workflow"))
		}
		return
	}

	c.JSON(http.StatusCreated, execution)
}

// GetWorkflowExecutions retrieves workflow executions
// @Summary Get workflow executions
// @Description Get a list of executions for a specific workflow
// @Tags workflows
// @Produce json
// @Security Bearer
// @Param id path string true "Workflow ID"
// @Param limit query int false "Number of executions to return" default(10)
// @Param offset query int false "Number of executions to skip" default(0)
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/workflows/{id}/executions [get]
func (h *WorkflowHandler) GetWorkflowExecutions(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Workflow ID is required", nil))
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

	h.logger.Info("Getting workflow executions", 
		zap.String("workflow_id", workflowID),
		zap.String("user_id", userID), 
		zap.Int("limit", limit), 
		zap.Int("offset", offset))

	executions, total, err := h.workflowService.GetWorkflowExecutions(c.Request.Context(), workflowID, spaceContext, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get workflow executions", zap.String("workflow_id", workflowID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get workflow executions"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"executions": executions,
		"pagination": gin.H{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// UpdateWorkflowStatus updates workflow status (activate/pause)
// @Summary Update workflow status
// @Description Update workflow status to active, paused, or disabled
// @Tags workflows
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Workflow ID"
// @Param status body map[string]string true "Status update request" example({"status": "active"})
// @Success 200 {object} models.Workflow
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/workflows/{id}/status [put]
func (h *WorkflowHandler) UpdateWorkflowStatus(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Workflow ID is required", nil))
		return
	}

	var statusReq struct {
		Status string `json:"status" binding:"required,oneof=active paused disabled"`
	}
	if err := c.ShouldBindJSON(&statusReq); err != nil {
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

	h.logger.Info("Updating workflow status", 
		zap.String("workflow_id", workflowID), 
		zap.String("status", statusReq.Status),
		zap.String("user_id", userID))

	updateReq := models.UpdateWorkflowRequest{
		Status: statusReq.Status,
	}

	workflow, err := h.workflowService.UpdateWorkflow(c.Request.Context(), workflowID, updateReq, spaceContext)
	if err != nil {
		h.logger.Error("Failed to update workflow status", zap.String("workflow_id", workflowID), zap.Error(err))
		if err.Error() == "workflow not found" {
			c.JSON(http.StatusNotFound, errors.NotFound("Workflow not found"))
		} else {
			c.JSON(http.StatusInternalServerError, errors.Internal("Failed to update workflow status"))
		}
		return
	}

	c.JSON(http.StatusOK, workflow)
}

// GetWorkflowAnalytics retrieves workflow performance analytics
// @Summary Get workflow analytics
// @Description Get workflow performance analytics for the current tenant
// @Tags workflows
// @Produce json
// @Security Bearer
// @Param period query string false "Analytics period" default(monthly) Enums(daily, weekly, monthly)
// @Success 200 {object} models.WorkflowAnalytics
// @Failure 401 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/workflows/analytics [get]
func (h *WorkflowHandler) GetWorkflowAnalytics(c *gin.Context) {
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

	h.logger.Info("Getting workflow analytics", zap.String("user_id", userID), zap.String("period", period))

	analytics, err := h.workflowService.GetWorkflowAnalytics(c.Request.Context(), spaceContext, period)
	if err != nil {
		h.logger.Error("Failed to get workflow analytics", zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to get workflow analytics"))
		return
	}

	c.JSON(http.StatusOK, analytics)
}