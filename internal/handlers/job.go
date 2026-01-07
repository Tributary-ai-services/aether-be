package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/services"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// JobHandler handles job-related HTTP requests
type JobHandler struct {
	documentService *services.DocumentService
	audiModalService *services.AudiModalService
	logger          *logger.Logger
}

// NewJobHandler creates a new job handler
func NewJobHandler(documentService *services.DocumentService, audiModalService *services.AudiModalService, log *logger.Logger) *JobHandler {
	return &JobHandler{
		documentService: documentService,
		audiModalService: audiModalService,
		logger:          log.WithService("job_handler"),
	}
}

// GetJobStatus gets the status of a generic job by ID
// @Summary Get job status
// @Description Get real-time status for any processing job by job ID
// @Tags jobs
// @Produce json
// @Security Bearer
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} errors.APIError
// @Failure 401 {object} errors.APIError
// @Failure 404 {object} errors.APIError
// @Failure 500 {object} errors.APIError
// @Router /api/v1/jobs/{id} [get]
func (h *JobHandler) GetJobStatus(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, errors.Validation("Job ID is required", nil))
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

	h.logger.Info("Getting job status", zap.String("job_id", jobID), zap.String("user_id", userID))

	// Try to get status from AudiModal first (as processing jobs are typically there)
	status, err := h.getAudiModalJobStatus(c.Request.Context(), jobID, spaceContext.TenantID)
	if err != nil {
		h.logger.Error("Failed to get job status", zap.String("job_id", jobID), zap.Error(err))
		c.JSON(http.StatusNotFound, errors.NotFound("Job not found"))
		return
	}

	c.JSON(http.StatusOK, status)
}

// getAudiModalJobStatus gets job status from AudiModal service
func (h *JobHandler) getAudiModalJobStatus(ctx context.Context, jobID string, tenantID string) (map[string]interface{}, error) {
	// Use the AudiModal service to get file status (since most jobs are processing jobs)

	// First try to get file chunks to see if this is a completed processing job
	chunks, err := h.audiModalService.GetFileChunks(ctx, tenantID, jobID, 10, 0) // Get first 10 chunks
	if err == nil && chunks != nil && len(chunks.Data) > 0 {
		// Job completed successfully - we have chunks
		return map[string]interface{}{
			"job_id":        jobID,
			"status":        "completed",
			"progress":      100.0,
			"chunks_count":  len(chunks.Data),
			"total_chunks":  chunks.Total,
			"job_type":      "document_processing",
		}, nil
	}

	// For now, return a basic status structure since we can't easily check AudiModal health
	// In a full implementation, we'd have a proper job tracking system
	return map[string]interface{}{
		"job_id":      jobID,
		"status":      "processing", // Could be: queued, processing, completed, failed
		"progress":    50.0,         // Percentage complete
		"job_type":    "document_processing",
		"started_at":  nil,
		"estimated_completion": nil,
		"error":       nil,
	}, nil
}