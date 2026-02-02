package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// ComplianceHandler handles compliance-related HTTP requests
type ComplianceHandler struct {
	audiModalService *services.AudiModalService
	logger           *logger.Logger
}

// NewComplianceHandler creates a new ComplianceHandler
func NewComplianceHandler(audiModalService *services.AudiModalService, log *logger.Logger) *ComplianceHandler {
	return &ComplianceHandler{
		audiModalService: audiModalService,
		logger:           log.WithService("compliance_handler"),
	}
}

// ViolationResponse represents a DLP violation for the frontend
type ViolationResponse struct {
	ID             string   `json:"id"`
	TenantID       string   `json:"tenant_id"`
	PolicyID       string   `json:"policy_id"`
	FileID         string   `json:"file_id"`
	ChunkID        *string  `json:"chunk_id,omitempty"`
	RuleName       string   `json:"rule_name"`
	Severity       string   `json:"severity"`
	Confidence     float64  `json:"confidence"`
	MatchedText    string   `json:"matched_text,omitempty"`
	Context        string   `json:"context,omitempty"`
	StartOffset    int64    `json:"start_offset,omitempty"`
	EndOffset      int64    `json:"end_offset,omitempty"`
	LineNumber     int      `json:"line_number,omitempty"`
	ActionsTaken   []string `json:"actions_taken"`
	Status         string   `json:"status"`
	Acknowledged   bool     `json:"acknowledged"`
	AcknowledgedBy string   `json:"acknowledged_by,omitempty"`
	AcknowledgedAt *string  `json:"acknowledged_at,omitempty"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	// Enhanced fields for frontend display
	FileName       string   `json:"file_name,omitempty"`
	ComplianceType string   `json:"compliance_type,omitempty"`
}

// ComplianceSummaryResponse represents aggregated compliance statistics
type ComplianceSummaryResponse struct {
	TotalViolations     int            `json:"total_violations"`
	UnacknowledgedCount int            `json:"unacknowledged_count"`
	ComplianceScore     int            `json:"compliance_score"`
	CriticalCount       int            `json:"critical_count"`
	HighCount           int            `json:"high_count"`
	MediumCount         int            `json:"medium_count"`
	LowCount            int            `json:"low_count"`
	PIIDetections       int            `json:"pii_detections"`
	BySeverity          map[string]int `json:"by_severity"`
	ByRuleType          map[string]int `json:"by_rule_type"`
}

// GetViolations handles GET /api/v1/compliance/violations
// @Summary List DLP violations
// @Description Get a paginated list of DLP violations from AudiModal
// @Tags compliance
// @Accept json
// @Produce json
// @Param severity query string false "Filter by severity (critical, high, medium, low)"
// @Param status query string false "Filter by status (detected, resolved, acknowledged)"
// @Param acknowledged query bool false "Filter by acknowledged status"
// @Param compliance_type query string false "Filter by compliance type (pii, hipaa, pci-dss, gdpr)"
// @Param from query string false "Time range start (ISO 8601 or relative like 'now-24h')"
// @Param to query string false "Time range end (ISO 8601 or 'now')"
// @Param page query int false "Page number (default: 1)"
// @Param page_size query int false "Page size (default: 20, max: 100)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/compliance/violations [get]
func (h *ComplianceHandler) GetViolations(c *gin.Context) {
	// Get space context for tenant isolation
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil || spaceCtx == nil || spaceCtx.TenantID == "" {
		h.logger.Error("No space context found", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Space context required",
		})
		return
	}

	// Parse query parameters
	severity := c.Query("severity")
	status := c.Query("status")
	acknowledgedStr := c.Query("acknowledged")
	complianceType := c.Query("compliance_type")
	from := c.Query("from")
	to := c.Query("to")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	h.logger.Info("Fetching compliance violations",
		zap.String("tenant_id", spaceCtx.TenantID),
		zap.String("severity", severity),
		zap.String("status", status),
		zap.String("compliance_type", complianceType),
		zap.String("from", from),
		zap.String("to", to),
		zap.Int("page", page),
		zap.Int("page_size", pageSize))

	// Call AudiModal to get violations
	violations, total, err := h.audiModalService.GetDLPViolations(c.Request.Context(), spaceCtx.TenantID, services.DLPViolationFilter{
		Severity:       severity,
		Status:         status,
		Acknowledged:   acknowledgedStr,
		ComplianceType: complianceType,
		From:           from,
		To:             to,
		Page:           page,
		PageSize:       pageSize,
	})
	if err != nil {
		h.logger.Error("Failed to fetch violations from AudiModal",
			zap.String("tenant_id", spaceCtx.TenantID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to fetch compliance violations",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    violations,
		"meta": gin.H{
			"page":       page,
			"page_size":  pageSize,
			"total":      total,
			"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
		},
	})
}

// GetViolation handles GET /api/v1/compliance/violations/:id
// @Summary Get a specific DLP violation
// @Description Get details of a specific DLP violation by ID
// @Tags compliance
// @Accept json
// @Produce json
// @Param id path string true "Violation ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/compliance/violations/{id} [get]
func (h *ComplianceHandler) GetViolation(c *gin.Context) {
	// Get space context for tenant isolation
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil || spaceCtx == nil || spaceCtx.TenantID == "" {
		h.logger.Error("No space context found", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Space context required",
		})
		return
	}

	violationID := c.Param("id")
	if violationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Violation ID is required",
		})
		return
	}

	h.logger.Info("Fetching compliance violation",
		zap.String("tenant_id", spaceCtx.TenantID),
		zap.String("violation_id", violationID))

	violation, err := h.audiModalService.GetDLPViolation(c.Request.Context(), spaceCtx.TenantID, violationID)
	if err != nil {
		h.logger.Error("Failed to fetch violation from AudiModal",
			zap.String("tenant_id", spaceCtx.TenantID),
			zap.String("violation_id", violationID),
			zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Violation not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    violation,
	})
}

// GetSummary handles GET /api/v1/compliance/summary
// @Summary Get compliance summary statistics
// @Description Get aggregated compliance statistics including violation counts and score
// @Tags compliance
// @Accept json
// @Produce json
// @Success 200 {object} ComplianceSummaryResponse
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/compliance/summary [get]
func (h *ComplianceHandler) GetSummary(c *gin.Context) {
	// Get space context for tenant isolation
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil || spaceCtx == nil || spaceCtx.TenantID == "" {
		h.logger.Error("No space context found", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Space context required",
		})
		return
	}

	h.logger.Info("Fetching compliance summary",
		zap.String("tenant_id", spaceCtx.TenantID))

	summary, err := h.audiModalService.GetDLPViolationSummary(c.Request.Context(), spaceCtx.TenantID)
	if err != nil {
		h.logger.Error("Failed to fetch compliance summary from AudiModal",
			zap.String("tenant_id", spaceCtx.TenantID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to fetch compliance summary",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    summary,
	})
}

// AcknowledgeViolation handles POST /api/v1/compliance/violations/:id/acknowledge
// @Summary Acknowledge a DLP violation
// @Description Mark a DLP violation as acknowledged by the current user
// @Tags compliance
// @Accept json
// @Produce json
// @Param id path string true "Violation ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/compliance/violations/{id}/acknowledge [post]
func (h *ComplianceHandler) AcknowledgeViolation(c *gin.Context) {
	// Get space context for tenant isolation
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil || spaceCtx == nil || spaceCtx.TenantID == "" {
		h.logger.Error("No space context found", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Space context required",
		})
		return
	}

	violationID := c.Param("id")
	if violationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Violation ID is required",
		})
		return
	}

	// Get user ID from context for audit trail
	userID, _ := middleware.GetUserID(c)

	h.logger.Info("Acknowledging compliance violation",
		zap.String("tenant_id", spaceCtx.TenantID),
		zap.String("violation_id", violationID),
		zap.String("acknowledged_by", userID))

	violation, err := h.audiModalService.AcknowledgeDLPViolation(c.Request.Context(), spaceCtx.TenantID, violationID, userID)
	if err != nil {
		h.logger.Error("Failed to acknowledge violation",
			zap.String("tenant_id", spaceCtx.TenantID),
			zap.String("violation_id", violationID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to acknowledge violation",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    violation,
	})
}

// BulkAcknowledgeRequest represents a request to acknowledge multiple violations
type BulkAcknowledgeRequest struct {
	ViolationIDs []string `json:"violation_ids" binding:"required,min=1"`
}

// BulkAcknowledgeViolations handles POST /api/v1/compliance/violations/acknowledge-bulk
// @Summary Bulk acknowledge DLP violations
// @Description Mark multiple DLP violations as acknowledged by the current user
// @Tags compliance
// @Accept json
// @Produce json
// @Param request body BulkAcknowledgeRequest true "Violation IDs to acknowledge"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/compliance/violations/acknowledge-bulk [post]
func (h *ComplianceHandler) BulkAcknowledgeViolations(c *gin.Context) {
	// Get space context for tenant isolation
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil || spaceCtx == nil || spaceCtx.TenantID == "" {
		h.logger.Error("No space context found", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Space context required",
		})
		return
	}

	// Parse request body
	var req BulkAcknowledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid bulk acknowledge request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request: violation_ids array is required",
		})
		return
	}

	if len(req.ViolationIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "At least one violation ID is required",
		})
		return
	}

	// Get user ID from context for audit trail
	userID, _ := middleware.GetUserID(c)

	h.logger.Info("Bulk acknowledging compliance violations",
		zap.String("tenant_id", spaceCtx.TenantID),
		zap.Int("violation_count", len(req.ViolationIDs)),
		zap.String("acknowledged_by", userID))

	result, err := h.audiModalService.BulkAcknowledgeDLPViolations(c.Request.Context(), spaceCtx.TenantID, req.ViolationIDs, userID)
	if err != nil {
		h.logger.Error("Failed to bulk acknowledge violations",
			zap.String("tenant_id", spaceCtx.TenantID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to acknowledge violations",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
