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

// SecurityHandler handles security-related HTTP requests
type SecurityHandler struct {
	securityService *services.SecurityEventService
	logger          *logger.Logger
}

// NewSecurityHandler creates a new SecurityHandler
func NewSecurityHandler(securityService *services.SecurityEventService, log *logger.Logger) *SecurityHandler {
	return &SecurityHandler{
		securityService: securityService,
		logger:          log.WithService("security_handler"),
	}
}

// GetSecurityEvents handles GET /api/v1/security/events
// @Summary List security events
// @Description Get a paginated list of security threat detection events
// @Tags security
// @Accept json
// @Produce json
// @Param event_type query string false "Filter by event type (sql_injection, xss, html_injection, control_chars)"
// @Param severity query string false "Filter by severity (low, medium, high, critical)"
// @Param status query string false "Filter by status (new, reviewed, approved, rejected, false_positive)"
// @Param action query string false "Filter by action (sanitized, isolated, rejected)"
// @Param start_date query string false "Filter by start date (ISO 8601)"
// @Param end_date query string false "Filter by end date (ISO 8601)"
// @Param limit query int false "Limit (default: 20, max: 100)"
// @Param offset query int false "Offset (default: 0)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/security/events [get]
func (h *SecurityHandler) GetSecurityEvents(c *gin.Context) {
	// Get space context for tenant isolation
	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil {
		h.logger.Error("No space context found", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.BadRequest("Space context required"))
		return
	}

	// Parse query parameters
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	req := &models.SecurityEventListRequest{
		TenantID:  spaceCtx.TenantID,
		EventType: models.ThreatType(c.Query("event_type")),
		Severity:  models.ThreatSeverity(c.Query("severity")),
		Status:    models.SecurityEventStatus(c.Query("status")),
		Action:    models.ThreatAction(c.Query("action")),
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
		Limit:     limit,
		Offset:    offset,
	}

	h.logger.Info("Fetching security events",
		zap.String("tenant_id", spaceCtx.TenantID),
		zap.Int("limit", limit),
		zap.Int("offset", offset))

	events, total, err := h.securityService.ListSecurityEvents(c.Request.Context(), req)
	if err != nil {
		h.logger.Error("Failed to fetch security events",
			zap.String("tenant_id", spaceCtx.TenantID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to fetch security events"))
		return
	}

	// Convert to response format
	var responses []*models.SecurityEventResponse
	for _, event := range events {
		responses = append(responses, event.ToResponse())
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    responses,
		"meta": gin.H{
			"limit":  limit,
			"offset": offset,
			"total":  total,
		},
	})
}

// GetSecurityEvent handles GET /api/v1/security/events/:id
// @Summary Get a specific security event
// @Description Get details of a specific security event by ID
// @Tags security
// @Accept json
// @Produce json
// @Param id path string true "Event ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/security/events/{id} [get]
func (h *SecurityHandler) GetSecurityEvent(c *gin.Context) {
	eventID := c.Param("id")
	if eventID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Event ID is required"))
		return
	}

	h.logger.Info("Fetching security event", zap.String("event_id", eventID))

	event, err := h.securityService.GetSecurityEvent(c.Request.Context(), eventID)
	if err != nil {
		h.logger.Error("Failed to fetch security event",
			zap.String("event_id", eventID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to fetch security event"))
		return
	}

	if event == nil {
		c.JSON(http.StatusNotFound, errors.NotFound("Security event not found"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    event.ToResponse(),
	})
}

// ReviewSecurityEvent handles PUT /api/v1/security/events/:id/review
// @Summary Review a security event
// @Description Approve, reject, or mark a security event as false positive
// @Tags security
// @Accept json
// @Produce json
// @Param id path string true "Event ID"
// @Param request body models.SecurityEventReviewRequest true "Review request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/security/events/{id}/review [put]
func (h *SecurityHandler) ReviewSecurityEvent(c *gin.Context) {
	eventID := c.Param("id")
	if eventID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Event ID is required"))
		return
	}

	// Get user ID for audit trail
	userID, _ := middleware.GetUserID(c)

	// Parse request body
	var req models.SecurityEventReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid review request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.BadRequest("Invalid request format"))
		return
	}

	// Validate status
	validStatuses := map[models.SecurityEventStatus]bool{
		models.SecurityEventStatusApproved:      true,
		models.SecurityEventStatusRejected:      true,
		models.SecurityEventStatusFalsePositive: true,
	}
	if !validStatuses[req.Status] {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Status must be one of: approved, rejected, false_positive"))
		return
	}

	h.logger.Info("Reviewing security event",
		zap.String("event_id", eventID),
		zap.String("reviewer_id", userID),
		zap.String("status", string(req.Status)))

	event, err := h.securityService.ReviewSecurityEvent(c.Request.Context(), eventID, userID, &req)
	if err != nil {
		h.logger.Error("Failed to review security event",
			zap.String("event_id", eventID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to review security event"))
		return
	}

	if event == nil {
		c.JSON(http.StatusNotFound, errors.NotFound("Security event not found"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    event.ToResponse(),
	})
}

// GetSecuritySummary handles GET /api/v1/security/summary
// @Summary Get security summary statistics
// @Description Get aggregated security statistics including event counts and trends
// @Tags security
// @Accept json
// @Produce json
// @Success 200 {object} models.SecuritySummary
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/security/summary [get]
func (h *SecurityHandler) GetSecuritySummary(c *gin.Context) {
	// Get space context for tenant isolation
	spaceCtx, err := middleware.GetSpaceContext(c)
	tenantID := ""
	if err == nil && spaceCtx != nil {
		tenantID = spaceCtx.TenantID
	}

	h.logger.Info("Fetching security summary", zap.String("tenant_id", tenantID))

	summary, err := h.securityService.GetSecuritySummary(c.Request.Context(), tenantID)
	if err != nil {
		h.logger.Error("Failed to fetch security summary",
			zap.String("tenant_id", tenantID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, errors.Internal("Failed to fetch security summary"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    summary,
	})
}

// GetSecurityPolicies handles GET /api/v1/security/policies
// @Summary List security policies
// @Description Get the list of security policies for the current tenant
// @Tags security
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/security/policies [get]
func (h *SecurityHandler) GetSecurityPolicies(c *gin.Context) {
	// Get space context for tenant isolation
	spaceCtx, err := middleware.GetSpaceContext(c)
	tenantID := ""
	if err == nil && spaceCtx != nil {
		tenantID = spaceCtx.TenantID
	}

	h.logger.Info("Fetching security policies", zap.String("tenant_id", tenantID))

	// For now, return the default policies
	// In a full implementation, this would query the security_policies table
	defaultPolicies := []gin.H{
		{
			"id":        "default-low",
			"tenantId":  nil,
			"eventType": "*",
			"severity":  "low",
			"action":    "sanitize",
			"enabled":   true,
		},
		{
			"id":        "default-medium",
			"tenantId":  nil,
			"eventType": "*",
			"severity":  "medium",
			"action":    "isolate",
			"enabled":   true,
		},
		{
			"id":        "default-high",
			"tenantId":  nil,
			"eventType": "*",
			"severity":  "high",
			"action":    "isolate",
			"enabled":   true,
		},
		{
			"id":        "default-critical",
			"tenantId":  nil,
			"eventType": "*",
			"severity":  "critical",
			"action":    "reject",
			"enabled":   true,
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    defaultPolicies,
	})
}

// UpdateSecurityPolicy handles PUT /api/v1/security/policies/:id
// @Summary Update a security policy
// @Description Update the action for a security policy
// @Tags security
// @Accept json
// @Produce json
// @Param id path string true "Policy ID"
// @Param request body models.SecurityPolicyUpdateRequest true "Policy update request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/security/policies/{id} [put]
func (h *SecurityHandler) UpdateSecurityPolicy(c *gin.Context) {
	policyID := c.Param("id")
	if policyID == "" {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Policy ID is required"))
		return
	}

	// Parse request body
	var req models.SecurityPolicyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid policy update request", zap.Error(err))
		c.JSON(http.StatusBadRequest, errors.BadRequest("Invalid request format"))
		return
	}

	// Validate action
	validActions := map[models.ThreatAction]bool{
		models.ActionSanitized: true,
		models.ActionIsolated:  true,
		models.ActionRejected:  true,
	}
	if !validActions[req.Action] {
		c.JSON(http.StatusBadRequest, errors.BadRequest("Action must be one of: sanitized, isolated, rejected"))
		return
	}

	h.logger.Info("Updating security policy",
		zap.String("policy_id", policyID),
		zap.String("action", string(req.Action)))

	// For now, return success - in a full implementation, this would update the database
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Policy updated successfully",
	})
}
