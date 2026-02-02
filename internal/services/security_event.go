package services

import (
	"context"
	"database/sql"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/validation"
)

// SecurityEventService handles security event operations
type SecurityEventService struct {
	kafkaService *KafkaService
	db           *sql.DB
	logger       *logger.Logger
}

// NewSecurityEventService creates a new security event service
func NewSecurityEventService(kafkaService *KafkaService, db *sql.DB, log *logger.Logger) *SecurityEventService {
	return &SecurityEventService{
		kafkaService: kafkaService,
		db:           db,
		logger:       log.WithService("security_event_service"),
	}
}

// PublishThreatDetected publishes a security threat detection event to Kafka
func (s *SecurityEventService) PublishThreatDetected(ctx context.Context, event *models.SecurityEvent) error {
	if s.kafkaService == nil {
		s.logger.Warn("Kafka service not available, skipping security event publish",
			zap.String("event_id", event.ID),
		)
		return nil
	}

	data := map[string]interface{}{
		"id":              event.ID,
		"tenant_id":       event.TenantID,
		"event_type":      string(event.EventType),
		"severity":        string(event.Severity),
		"request_id":      event.RequestID,
		"request_path":    event.RequestPath,
		"request_method":  event.RequestMethod,
		"client_ip":       event.ClientIP,
		"user_agent":      event.UserAgent,
		"user_id":         event.UserID,
		"field_name":      event.FieldName,
		"threat_pattern":  event.ThreatPattern,
		"matched_content": event.MatchedContent,
		"action":          string(event.Action),
		"resource_id":     event.ResourceID,
		"resource_type":   event.ResourceType,
		"status":          string(event.Status),
		"created_at":      event.CreatedAt.Format(time.RFC3339),
	}

	kafkaEvent := NewSecurityEvent(
		EventSecurityThreatDetected,
		event.ID,
		event.RequestID,
		event.UserID,
		data,
	)

	err := s.kafkaService.PublishEvent(ctx, kafkaEvent)
	if err != nil {
		s.logger.Error("Failed to publish security event to Kafka",
			zap.String("event_id", event.ID),
			zap.Error(err),
		)
		return err
	}

	s.logger.Info("Security event published to Kafka",
		zap.String("event_id", event.ID),
		zap.String("event_type", string(event.EventType)),
		zap.String("severity", string(event.Severity)),
	)

	return nil
}

// CreateSecurityEventsFromThreats creates security events from detected threats
func (s *SecurityEventService) CreateSecurityEventsFromThreats(
	threats []validation.DetectedThreat,
	requestID, requestPath, requestMethod, clientIP, userAgent string,
) []*models.SecurityEvent {
	var events []*models.SecurityEvent

	for _, threat := range threats {
		event := models.NewSecurityEvent(
			models.ThreatType(threat.Type),
			models.ThreatSeverity(threat.Severity),
			models.ThreatAction(threat.Action),
			threat.FieldName,
			threat.Pattern,
			threat.MatchedContent,
			requestID,
			requestPath,
			requestMethod,
			clientIP,
			userAgent,
		)
		events = append(events, event)
	}

	return events
}

// SaveSecurityEvent persists a security event to the database
func (s *SecurityEventService) SaveSecurityEvent(ctx context.Context, event *models.SecurityEvent) error {
	if s.db == nil {
		s.logger.Warn("Database not available, skipping security event persistence",
			zap.String("event_id", event.ID),
		)
		return nil
	}

	query := `
		INSERT INTO security_events (
			id, tenant_id, event_type, severity, request_id, request_path,
			request_method, client_ip, user_agent, user_id, field_name,
			threat_pattern, matched_content, action, resource_id, resource_type,
			status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

	_, err := s.db.ExecContext(ctx, query,
		event.ID,
		nullString(event.TenantID),
		string(event.EventType),
		string(event.Severity),
		event.RequestID,
		event.RequestPath,
		event.RequestMethod,
		event.ClientIP,
		event.UserAgent,
		nullString(event.UserID),
		event.FieldName,
		event.ThreatPattern,
		event.MatchedContent,
		string(event.Action),
		nullString(event.ResourceID),
		nullString(event.ResourceType),
		string(event.Status),
		event.CreatedAt,
	)

	if err != nil {
		s.logger.Error("Failed to save security event to database",
			zap.String("event_id", event.ID),
			zap.Error(err),
		)
		return err
	}

	s.logger.Debug("Security event saved to database",
		zap.String("event_id", event.ID),
	)

	return nil
}

// GetSecurityEvent retrieves a security event by ID
func (s *SecurityEventService) GetSecurityEvent(ctx context.Context, id string) (*models.SecurityEvent, error) {
	if s.db == nil {
		return nil, nil
	}

	query := `
		SELECT id, tenant_id, event_type, severity, request_id, request_path,
			request_method, client_ip, user_agent, user_id, field_name,
			threat_pattern, matched_content, action, resource_id, resource_type,
			status, reviewed_by, reviewed_at, review_notes, created_at
		FROM security_events
		WHERE id = $1
	`

	var event models.SecurityEvent
	var tenantID, userID, resourceID, resourceType, reviewedBy, reviewNotes sql.NullString
	var reviewedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&event.ID,
		&tenantID,
		&event.EventType,
		&event.Severity,
		&event.RequestID,
		&event.RequestPath,
		&event.RequestMethod,
		&event.ClientIP,
		&event.UserAgent,
		&userID,
		&event.FieldName,
		&event.ThreatPattern,
		&event.MatchedContent,
		&event.Action,
		&resourceID,
		&resourceType,
		&event.Status,
		&reviewedBy,
		&reviewedAt,
		&reviewNotes,
		&event.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Set nullable fields
	if tenantID.Valid {
		event.TenantID = tenantID.String
	}
	if userID.Valid {
		event.UserID = userID.String
	}
	if resourceID.Valid {
		event.ResourceID = resourceID.String
	}
	if resourceType.Valid {
		event.ResourceType = resourceType.String
	}
	if reviewedBy.Valid {
		event.ReviewedBy = reviewedBy.String
	}
	if reviewedAt.Valid {
		event.ReviewedAt = &reviewedAt.Time
	}
	if reviewNotes.Valid {
		event.ReviewNotes = reviewNotes.String
	}

	return &event, nil
}

// ListSecurityEvents retrieves security events with optional filtering
func (s *SecurityEventService) ListSecurityEvents(ctx context.Context, req *models.SecurityEventListRequest) ([]*models.SecurityEvent, int, error) {
	if s.db == nil {
		return nil, 0, nil
	}

	// Build query with filters
	baseQuery := `
		FROM security_events
		WHERE 1=1
	`
	args := []interface{}{}
	argPos := 1

	if req.TenantID != "" {
		baseQuery += ` AND tenant_id = $` + string(rune('0'+argPos))
		args = append(args, req.TenantID)
		argPos++
	}
	if req.EventType != "" {
		baseQuery += ` AND event_type = $` + string(rune('0'+argPos))
		args = append(args, string(req.EventType))
		argPos++
	}
	if req.Severity != "" {
		baseQuery += ` AND severity = $` + string(rune('0'+argPos))
		args = append(args, string(req.Severity))
		argPos++
	}
	if req.Status != "" {
		baseQuery += ` AND status = $` + string(rune('0'+argPos))
		args = append(args, string(req.Status))
		argPos++
	}
	if req.Action != "" {
		baseQuery += ` AND action = $` + string(rune('0'+argPos))
		args = append(args, string(req.Action))
		argPos++
	}

	// Count query
	countQuery := "SELECT COUNT(*) " + baseQuery
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Set default limit
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	// Data query with pagination
	dataQuery := `
		SELECT id, tenant_id, event_type, severity, request_id, request_path,
			request_method, client_ip, user_agent, user_id, field_name,
			threat_pattern, matched_content, action, resource_id, resource_type,
			status, reviewed_by, reviewed_at, review_notes, created_at
	` + baseQuery + ` ORDER BY created_at DESC LIMIT $` + string(rune('0'+argPos)) + ` OFFSET $` + string(rune('0'+argPos+1))
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []*models.SecurityEvent
	for rows.Next() {
		var event models.SecurityEvent
		var tenantID, userID, resourceID, resourceType, reviewedBy, reviewNotes sql.NullString
		var reviewedAt sql.NullTime

		err := rows.Scan(
			&event.ID,
			&tenantID,
			&event.EventType,
			&event.Severity,
			&event.RequestID,
			&event.RequestPath,
			&event.RequestMethod,
			&event.ClientIP,
			&event.UserAgent,
			&userID,
			&event.FieldName,
			&event.ThreatPattern,
			&event.MatchedContent,
			&event.Action,
			&resourceID,
			&resourceType,
			&event.Status,
			&reviewedBy,
			&reviewedAt,
			&reviewNotes,
			&event.CreatedAt,
		)
		if err != nil {
			return nil, 0, err
		}

		// Set nullable fields
		if tenantID.Valid {
			event.TenantID = tenantID.String
		}
		if userID.Valid {
			event.UserID = userID.String
		}
		if resourceID.Valid {
			event.ResourceID = resourceID.String
		}
		if resourceType.Valid {
			event.ResourceType = resourceType.String
		}
		if reviewedBy.Valid {
			event.ReviewedBy = reviewedBy.String
		}
		if reviewedAt.Valid {
			event.ReviewedAt = &reviewedAt.Time
		}
		if reviewNotes.Valid {
			event.ReviewNotes = reviewNotes.String
		}

		events = append(events, &event)
	}

	return events, total, nil
}

// ReviewSecurityEvent updates the review status of a security event
func (s *SecurityEventService) ReviewSecurityEvent(ctx context.Context, id, reviewerID string, req *models.SecurityEventReviewRequest) (*models.SecurityEvent, error) {
	if s.db == nil {
		return nil, nil
	}

	query := `
		UPDATE security_events
		SET status = $1, reviewed_by = $2, reviewed_at = $3, review_notes = $4
		WHERE id = $5
	`

	reviewedAt := time.Now()
	_, err := s.db.ExecContext(ctx, query,
		string(req.Status),
		reviewerID,
		reviewedAt,
		req.ReviewNotes,
		id,
	)
	if err != nil {
		return nil, err
	}

	return s.GetSecurityEvent(ctx, id)
}

// GetSecuritySummary returns aggregated security statistics
func (s *SecurityEventService) GetSecuritySummary(ctx context.Context, tenantID string) (*models.SecuritySummary, error) {
	if s.db == nil {
		return &models.SecuritySummary{
			EventsBySeverity: make(map[string]int),
			EventsByType:     make(map[string]int),
			EventsByAction:   make(map[string]int),
		}, nil
	}

	summary := &models.SecuritySummary{
		EventsBySeverity:    make(map[string]int),
		EventsByType:        make(map[string]int),
		EventsByAction:      make(map[string]int),
		TopThreatenedPaths:  []models.PathStats{},
		TopThreatenedFields: []models.FieldStats{},
	}

	// Total events
	var baseWhere string
	var args []interface{}
	if tenantID != "" {
		baseWhere = " WHERE tenant_id = $1"
		args = append(args, tenantID)
	}

	query := "SELECT COUNT(*) FROM security_events" + baseWhere
	s.db.QueryRowContext(ctx, query, args...).Scan(&summary.TotalEvents)

	// New events
	query = "SELECT COUNT(*) FROM security_events" + baseWhere
	if baseWhere == "" {
		query += " WHERE status = 'new'"
	} else {
		query += " AND status = 'new'"
	}
	s.db.QueryRowContext(ctx, query, args...).Scan(&summary.NewEvents)

	// Events by severity
	query = "SELECT severity, COUNT(*) FROM security_events" + baseWhere + " GROUP BY severity"
	rows, _ := s.db.QueryContext(ctx, query, args...)
	if rows != nil {
		for rows.Next() {
			var severity string
			var count int
			rows.Scan(&severity, &count)
			summary.EventsBySeverity[severity] = count
		}
		rows.Close()
	}

	// Events by type
	query = "SELECT event_type, COUNT(*) FROM security_events" + baseWhere + " GROUP BY event_type"
	rows, _ = s.db.QueryContext(ctx, query, args...)
	if rows != nil {
		for rows.Next() {
			var eventType string
			var count int
			rows.Scan(&eventType, &count)
			summary.EventsByType[eventType] = count
		}
		rows.Close()
	}

	// Events by action
	query = "SELECT action, COUNT(*) FROM security_events" + baseWhere + " GROUP BY action"
	rows, _ = s.db.QueryContext(ctx, query, args...)
	if rows != nil {
		for rows.Next() {
			var action string
			var count int
			rows.Scan(&action, &count)
			summary.EventsByAction[action] = count
		}
		rows.Close()
	}

	// Last 24 hours
	query = "SELECT COUNT(*) FROM security_events" + baseWhere
	if baseWhere == "" {
		query += " WHERE created_at > NOW() - INTERVAL '24 hours'"
	} else {
		query += " AND created_at > NOW() - INTERVAL '24 hours'"
	}
	s.db.QueryRowContext(ctx, query, args...).Scan(&summary.Last24Hours)

	// Last 7 days
	query = "SELECT COUNT(*) FROM security_events" + baseWhere
	if baseWhere == "" {
		query += " WHERE created_at > NOW() - INTERVAL '7 days'"
	} else {
		query += " AND created_at > NOW() - INTERVAL '7 days'"
	}
	s.db.QueryRowContext(ctx, query, args...).Scan(&summary.Last7Days)

	return summary, nil
}

// nullString converts an empty string to sql.NullString
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
