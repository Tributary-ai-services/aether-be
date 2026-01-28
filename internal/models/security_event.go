package models

import (
	"time"

	"github.com/google/uuid"
)

// ThreatType represents the type of security threat detected
type ThreatType string

const (
	ThreatTypeSQLInjection  ThreatType = "sql_injection"
	ThreatTypeXSS           ThreatType = "xss"
	ThreatTypeHTMLInjection ThreatType = "html_injection"
	ThreatTypeControlChars  ThreatType = "control_chars"
)

// ThreatSeverity represents the severity level of a detected threat
type ThreatSeverity string

const (
	SeverityLow      ThreatSeverity = "low"
	SeverityMedium   ThreatSeverity = "medium"
	SeverityHigh     ThreatSeverity = "high"
	SeverityCritical ThreatSeverity = "critical"
)

// ThreatAction represents the action taken on a detected threat
type ThreatAction string

const (
	ActionSanitized ThreatAction = "sanitized"
	ActionIsolated  ThreatAction = "isolated"
	ActionRejected  ThreatAction = "rejected"
)

// SecurityEventStatus represents the review status of a security event
type SecurityEventStatus string

const (
	SecurityEventStatusNew          SecurityEventStatus = "new"
	SecurityEventStatusReviewed     SecurityEventStatus = "reviewed"
	SecurityEventStatusApproved     SecurityEventStatus = "approved"
	SecurityEventStatusRejected     SecurityEventStatus = "rejected"
	SecurityEventStatusFalsePositive SecurityEventStatus = "false_positive"
)

// SecurityEvent represents a security threat detection event
type SecurityEvent struct {
	ID             string              `json:"id"`
	TenantID       string              `json:"tenant_id,omitempty"`
	EventType      ThreatType          `json:"event_type"`
	Severity       ThreatSeverity      `json:"severity"`
	RequestID      string              `json:"request_id"`
	RequestPath    string              `json:"request_path"`
	RequestMethod  string              `json:"request_method"`
	ClientIP       string              `json:"client_ip"`
	UserAgent      string              `json:"user_agent"`
	UserID         string              `json:"user_id,omitempty"`
	FieldName      string              `json:"field_name"`
	ThreatPattern  string              `json:"threat_pattern"`
	MatchedContent string              `json:"matched_content"`
	Action         ThreatAction        `json:"action"`
	ResourceID     string              `json:"resource_id,omitempty"`
	ResourceType   string              `json:"resource_type,omitempty"`
	Status         SecurityEventStatus `json:"status"`
	ReviewedBy     string              `json:"reviewed_by,omitempty"`
	ReviewedAt     *time.Time          `json:"reviewed_at,omitempty"`
	ReviewNotes    string              `json:"review_notes,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
}

// NewSecurityEvent creates a new security event
func NewSecurityEvent(eventType ThreatType, severity ThreatSeverity, action ThreatAction, fieldName, pattern, matchedContent, requestID, requestPath, requestMethod, clientIP, userAgent string) *SecurityEvent {
	return &SecurityEvent{
		ID:             uuid.New().String(),
		EventType:      eventType,
		Severity:       severity,
		RequestID:      requestID,
		RequestPath:    requestPath,
		RequestMethod:  requestMethod,
		ClientIP:       clientIP,
		UserAgent:      userAgent,
		FieldName:      fieldName,
		ThreatPattern:  pattern,
		MatchedContent: matchedContent,
		Action:         action,
		Status:         SecurityEventStatusNew,
		CreatedAt:      time.Now(),
	}
}

// WithUserContext adds user context to the security event
func (e *SecurityEvent) WithUserContext(userID, tenantID string) *SecurityEvent {
	e.UserID = userID
	e.TenantID = tenantID
	return e
}

// WithResource associates the event with a created resource
func (e *SecurityEvent) WithResource(resourceID, resourceType string) *SecurityEvent {
	e.ResourceID = resourceID
	e.ResourceType = resourceType
	return e
}

// SecurityEventResponse is the API response for a security event
type SecurityEventResponse struct {
	ID             string              `json:"id"`
	TenantID       string              `json:"tenantId,omitempty"`
	EventType      ThreatType          `json:"eventType"`
	Severity       ThreatSeverity      `json:"severity"`
	RequestID      string              `json:"requestId"`
	RequestPath    string              `json:"requestPath"`
	RequestMethod  string              `json:"requestMethod"`
	ClientIP       string              `json:"clientIp"`
	UserAgent      string              `json:"userAgent"`
	UserID         string              `json:"userId,omitempty"`
	FieldName      string              `json:"fieldName"`
	ThreatPattern  string              `json:"threatPattern"`
	MatchedContent string              `json:"matchedContent"`
	Action         ThreatAction        `json:"action"`
	ResourceID     string              `json:"resourceId,omitempty"`
	ResourceType   string              `json:"resourceType,omitempty"`
	Status         SecurityEventStatus `json:"status"`
	ReviewedBy     string              `json:"reviewedBy,omitempty"`
	ReviewedAt     *time.Time          `json:"reviewedAt,omitempty"`
	ReviewNotes    string              `json:"reviewNotes,omitempty"`
	CreatedAt      time.Time           `json:"createdAt"`
}

// ToResponse converts a SecurityEvent to API response format
func (e *SecurityEvent) ToResponse() *SecurityEventResponse {
	return &SecurityEventResponse{
		ID:             e.ID,
		TenantID:       e.TenantID,
		EventType:      e.EventType,
		Severity:       e.Severity,
		RequestID:      e.RequestID,
		RequestPath:    e.RequestPath,
		RequestMethod:  e.RequestMethod,
		ClientIP:       e.ClientIP,
		UserAgent:      e.UserAgent,
		UserID:         e.UserID,
		FieldName:      e.FieldName,
		ThreatPattern:  e.ThreatPattern,
		MatchedContent: e.MatchedContent,
		Action:         e.Action,
		ResourceID:     e.ResourceID,
		ResourceType:   e.ResourceType,
		Status:         e.Status,
		ReviewedBy:     e.ReviewedBy,
		ReviewedAt:     e.ReviewedAt,
		ReviewNotes:    e.ReviewNotes,
		CreatedAt:      e.CreatedAt,
	}
}

// SecurityEventReviewRequest is the request to review a security event
type SecurityEventReviewRequest struct {
	Status      SecurityEventStatus `json:"status" validate:"required,oneof=approved rejected false_positive"`
	ReviewNotes string              `json:"review_notes" validate:"max=2000"`
}

// SecurityEventListRequest is the request parameters for listing security events
type SecurityEventListRequest struct {
	TenantID  string              `form:"tenant_id"`
	EventType ThreatType          `form:"event_type"`
	Severity  ThreatSeverity      `form:"severity"`
	Status    SecurityEventStatus `form:"status"`
	Action    ThreatAction        `form:"action"`
	StartDate string              `form:"start_date"`
	EndDate   string              `form:"end_date"`
	Limit     int                 `form:"limit" validate:"min=1,max=100"`
	Offset    int                 `form:"offset" validate:"min=0"`
}

// SecuritySummary provides aggregate security statistics
type SecuritySummary struct {
	TotalEvents         int                        `json:"totalEvents"`
	NewEvents           int                        `json:"newEvents"`
	PendingReview       int                        `json:"pendingReview"`
	EventsBySeverity    map[string]int             `json:"eventsBySeverity"`
	EventsByType        map[string]int             `json:"eventsByType"`
	EventsByAction      map[string]int             `json:"eventsByAction"`
	Last24Hours         int                        `json:"last24Hours"`
	Last7Days           int                        `json:"last7Days"`
	TopThreatenedPaths  []PathStats                `json:"topThreatenedPaths"`
	TopThreatenedFields []FieldStats               `json:"topThreatenedFields"`
}

// PathStats provides statistics for a specific request path
type PathStats struct {
	Path      string `json:"path"`
	Count     int    `json:"count"`
	Critical  int    `json:"critical"`
	High      int    `json:"high"`
}

// FieldStats provides statistics for a specific field name
type FieldStats struct {
	FieldName string `json:"fieldName"`
	Count     int    `json:"count"`
}

// SecurityPolicy defines how threats should be handled
type SecurityPolicy struct {
	ID        string         `json:"id"`
	TenantID  *string        `json:"tenant_id,omitempty"` // nil = global default
	EventType string         `json:"event_type"`          // '*' for all
	Severity  ThreatSeverity `json:"severity"`
	Action    ThreatAction   `json:"action"`
	Enabled   bool           `json:"enabled"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// SecurityPolicyUpdateRequest is the request to update a security policy
type SecurityPolicyUpdateRequest struct {
	Action  ThreatAction `json:"action" validate:"required,oneof=sanitized isolated rejected"`
	Enabled *bool        `json:"enabled,omitempty"`
}

// SecurityPolicyResponse is the API response for a security policy
type SecurityPolicyResponse struct {
	ID        string         `json:"id"`
	TenantID  *string        `json:"tenantId,omitempty"`
	EventType string         `json:"eventType"`
	Severity  ThreatSeverity `json:"severity"`
	Action    ThreatAction   `json:"action"`
	Enabled   bool           `json:"enabled"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// ToResponse converts a SecurityPolicy to API response format
func (p *SecurityPolicy) ToResponse() *SecurityPolicyResponse {
	return &SecurityPolicyResponse{
		ID:        p.ID,
		TenantID:  p.TenantID,
		EventType: p.EventType,
		Severity:  p.Severity,
		Action:    p.Action,
		Enabled:   p.Enabled,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

// DocumentSecurityStatus represents the security status of a document
type DocumentSecurityStatus string

const (
	DocumentSecurityStatusApproved       DocumentSecurityStatus = "approved"
	DocumentSecurityStatusPendingReview  DocumentSecurityStatus = "pending_security_review"
	DocumentSecurityStatusSecurityRejected DocumentSecurityStatus = "security_rejected"
)
