package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// StreamService handles live streaming operations
type StreamService struct {
	neo4j             *database.Neo4jClient
	logger            *logger.Logger
	activeConnections map[string]*models.StreamConnection
	connectionsMux    sync.RWMutex
	eventChannel      chan *models.LiveEvent
	eventProcessors   map[string]EventProcessor
}

// EventProcessor interface for processing different types of events
type EventProcessor interface {
	ProcessEvent(ctx context.Context, event *models.LiveEvent) (*models.LiveEvent, error)
	GetProcessorType() string
}

// SentimentAnalysisProcessor processes events for sentiment analysis
type SentimentAnalysisProcessor struct {
	logger *logger.Logger
}

// NewStreamService creates a new stream service
func NewStreamService(neo4j *database.Neo4jClient, log *logger.Logger) *StreamService {
	service := &StreamService{
		neo4j:             neo4j,
		logger:            log.WithService("stream_service"),
		activeConnections: make(map[string]*models.StreamConnection),
		eventChannel:      make(chan *models.LiveEvent, 1000), // Buffered channel for events
		eventProcessors:   make(map[string]EventProcessor),
	}

	// Register default event processors
	service.eventProcessors["sentiment_analysis"] = &SentimentAnalysisProcessor{logger: log}

	// Start event processing goroutine
	go service.processEvents()

	return service
}

// CreateStreamSource creates a new stream source
func (s *StreamService) CreateStreamSource(ctx context.Context, req models.CreateStreamSourceRequest, userID string, spaceContext *models.SpaceContext) (*models.StreamSource, error) {
	source := models.NewStreamSource(req, userID, spaceContext.TenantID, spaceContext.SpaceID)

	query := `
		CREATE (s:StreamSource {
			id: $id,
			name: $name,
			type: $type,
			provider: $provider,
			status: $status,
			configuration: $configuration,
			events_processed: $events_processed,
			events_per_second: $events_per_second,
			error_count: $error_count,
			created_at: $created_at,
			updated_at: $updated_at,
			created_by: $created_by,
			tenant_id: $tenant_id,
			organization_id: $organization_id
		})
		RETURN s
	`

	parameters := map[string]interface{}{
		"id":               source.ID,
		"name":             source.Name,
		"type":             source.Type,
		"provider":         source.Provider,
		"status":           source.Status,
		"configuration":    serializeParameters(source.Configuration),
		"events_processed": source.EventsProcessed,
		"events_per_second": source.EventsPerSecond,
		"error_count":      source.ErrorCount,
		"created_at":       source.CreatedAt,
		"updated_at":       source.UpdatedAt,
		"created_by":       source.CreatedBy,
		"tenant_id":        source.TenantID,
		"organization_id":  source.OrganizationID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to create stream source", zap.Error(err))
		return nil, fmt.Errorf("failed to create stream source: %w", err)
	}

	s.logger.Info("Created stream source", zap.String("source_id", source.ID), zap.String("name", source.Name))
	return source, nil
}

// GetStreamSources retrieves stream sources for a tenant
func (s *StreamService) GetStreamSources(ctx context.Context, spaceContext *models.SpaceContext, limit, offset int) ([]*models.StreamSource, int, error) {
	query := `
		MATCH (s:StreamSource)
		WHERE s.tenant_id = $tenant_id
		RETURN s
		ORDER BY s.created_at DESC
		SKIP $offset
		LIMIT $limit
	`

	countQuery := `
		MATCH (s:StreamSource)
		WHERE s.tenant_id = $tenant_id
		RETURN count(s) as total
	`

	parameters := map[string]interface{}{
		"tenant_id": spaceContext.TenantID,
		"limit":     limit,
		"offset":    offset,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Get stream sources
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get stream sources", zap.Error(err))
		return nil, 0, fmt.Errorf("failed to get stream sources: %w", err)
	}

	records := result.([]*neo4j.Record)
	sources := make([]*models.StreamSource, 0, len(records))

	for _, record := range records {
		source, err := s.recordToStreamSource(record, "s")
		if err != nil {
			s.logger.Error("Failed to parse stream source record", zap.Error(err))
			continue
		}
		sources = append(sources, source)
	}

	// Get total count
	countResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, countQuery, map[string]interface{}{"tenant_id": spaceContext.TenantID})
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to count stream sources", zap.Error(err))
		return sources, 0, nil
	}

	var total int
	if countRecords := countResult.([]*neo4j.Record); len(countRecords) > 0 {
		if totalValue, found := countRecords[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return sources, total, nil
}

// GetStreamSourceByID retrieves a specific stream source by ID
func (s *StreamService) GetStreamSourceByID(ctx context.Context, sourceID string, spaceContext *models.SpaceContext) (*models.StreamSource, error) {
	query := `
		MATCH (s:StreamSource)
		WHERE s.id = $source_id AND s.tenant_id = $tenant_id
		RETURN s
	`

	parameters := map[string]interface{}{
		"source_id": sourceID,
		"tenant_id": spaceContext.TenantID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get stream source", zap.String("source_id", sourceID), zap.Error(err))
		return nil, fmt.Errorf("failed to get stream source: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, fmt.Errorf("stream source not found")
	}

	return s.recordToStreamSource(records[0], "s")
}

// UpdateStreamSource updates an existing stream source
func (s *StreamService) UpdateStreamSource(ctx context.Context, sourceID string, req models.UpdateStreamSourceRequest, spaceContext *models.SpaceContext) (*models.StreamSource, error) {
	// Build dynamic update query
	setParts := []string{"s.updated_at = $updated_at"}
	parameters := map[string]interface{}{
		"source_id":  sourceID,
		"tenant_id":  spaceContext.TenantID,
		"updated_at": time.Now(),
	}

	if req.Name != "" {
		setParts = append(setParts, "s.name = $name")
		parameters["name"] = req.Name
	}
	if req.Status != "" {
		setParts = append(setParts, "s.status = $status")
		parameters["status"] = req.Status
	}
	if req.Configuration != nil {
		setParts = append(setParts, "s.configuration = $configuration")
		parameters["configuration"] = serializeParameters(req.Configuration)
	}

	query := fmt.Sprintf(`
		MATCH (s:StreamSource)
		WHERE s.id = $source_id AND s.tenant_id = $tenant_id
		SET %s
		RETURN s
	`, strings.Join(setParts, ", "))

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to update stream source", zap.String("source_id", sourceID), zap.Error(err))
		return nil, fmt.Errorf("failed to update stream source: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, fmt.Errorf("stream source not found")
	}

	s.logger.Info("Updated stream source", zap.String("source_id", sourceID))
	return s.recordToStreamSource(records[0], "s")
}

// DeleteStreamSource deletes a stream source
func (s *StreamService) DeleteStreamSource(ctx context.Context, sourceID string, spaceContext *models.SpaceContext) error {
	query := `
		MATCH (s:StreamSource {id: $source_id, tenant_id: $tenant_id})
		OPTIONAL MATCH (s)-[:GENERATED]->(e:LiveEvent)
		DETACH DELETE s, e
		RETURN count(s) as deleted
	`

	parameters := map[string]interface{}{
		"source_id": sourceID,
		"tenant_id": spaceContext.TenantID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to delete stream source", zap.String("source_id", sourceID), zap.Error(err))
		return fmt.Errorf("failed to delete stream source: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 || records[0].Values[0].(int64) == 0 {
		return fmt.Errorf("stream source not found")
	}

	s.logger.Info("Deleted stream source", zap.String("source_id", sourceID))
	return nil
}

// IngestEvent ingests a new live event into the system
func (s *StreamService) IngestEvent(ctx context.Context, sourceID, eventType, content, mediaType string, metadata map[string]interface{}, spaceContext *models.SpaceContext) (*models.LiveEvent, error) {
	// Verify the stream source exists
	source, err := s.GetStreamSourceByID(ctx, sourceID, spaceContext)
	if err != nil {
		return nil, err
	}

	if source.Status != "active" {
		return nil, fmt.Errorf("stream source is not active")
	}

	event := models.NewLiveEvent(sourceID, eventType, content, mediaType, spaceContext.TenantID, spaceContext.SpaceID)
	event.Metadata = metadata

	// Queue event for asynchronous processing
	select {
	case s.eventChannel <- event:
		s.logger.Debug("Event queued for processing", zap.String("event_id", event.ID))
	default:
		s.logger.Warn("Event channel full, dropping event", zap.String("event_id", event.ID))
		return nil, fmt.Errorf("event processing queue is full")
	}

	return event, nil
}

// GetLiveEvents retrieves recent live events
func (s *StreamService) GetLiveEvents(ctx context.Context, spaceContext *models.SpaceContext, filters models.StreamFilters, limit, offset int) ([]*models.LiveEvent, int, error) {
	// Build dynamic query based on filters
	whereConditions := []string{"e.tenant_id = $tenant_id"}
	parameters := map[string]interface{}{
		"tenant_id": spaceContext.TenantID,
		"limit":     limit,
		"offset":    offset,
	}

	if len(filters.SourceIDs) > 0 {
		whereConditions = append(whereConditions, "e.stream_source_id IN $source_ids")
		parameters["source_ids"] = filters.SourceIDs
	}
	if len(filters.EventTypes) > 0 {
		whereConditions = append(whereConditions, "e.event_type IN $event_types")
		parameters["event_types"] = filters.EventTypes
	}
	if len(filters.MediaTypes) > 0 {
		whereConditions = append(whereConditions, "e.media_type IN $media_types")
		parameters["media_types"] = filters.MediaTypes
	}
	if len(filters.Sentiments) > 0 {
		whereConditions = append(whereConditions, "e.sentiment IN $sentiments")
		parameters["sentiments"] = filters.Sentiments
	}
	if filters.MinConfidence > 0 {
		whereConditions = append(whereConditions, "e.confidence >= $min_confidence")
		parameters["min_confidence"] = filters.MinConfidence
	}

	whereClause := strings.Join(whereConditions, " AND ")

	query := fmt.Sprintf(`
		MATCH (e:LiveEvent)
		WHERE %s
		RETURN e
		ORDER BY e.processed_at DESC
		SKIP $offset
		LIMIT $limit
	`, whereClause)

	countQuery := fmt.Sprintf(`
		MATCH (e:LiveEvent)
		WHERE %s
		RETURN count(e) as total
	`, whereClause)

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Get events
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get live events", zap.Error(err))
		return nil, 0, fmt.Errorf("failed to get live events: %w", err)
	}

	records := result.([]*neo4j.Record)
	events := make([]*models.LiveEvent, 0, len(records))

	for _, record := range records {
		event, err := s.recordToLiveEvent(record, "e")
		if err != nil {
			s.logger.Error("Failed to parse live event record", zap.Error(err))
			continue
		}
		events = append(events, event)
	}

	// Get total count
	countResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, countQuery, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to count live events", zap.Error(err))
		return events, 0, nil
	}

	var total int
	if countRecords := countResult.([]*neo4j.Record); len(countRecords) > 0 {
		if totalValue, found := countRecords[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return events, total, nil
}

// GetStreamAnalytics retrieves stream performance analytics
func (s *StreamService) GetStreamAnalytics(ctx context.Context, spaceContext *models.SpaceContext, period string) (*models.StreamAnalytics, error) {
	// Get current stream sources and recent events for real-time analytics
	sources, _, err := s.GetStreamSources(ctx, spaceContext, 100, 0)
	if err != nil {
		return nil, err
	}

	events, _, err := s.GetLiveEvents(ctx, spaceContext, models.StreamFilters{}, 1000, 0)
	if err != nil {
		return nil, err
	}

	analytics := models.CalculateRealTimeAnalytics(sources, events, spaceContext.TenantID, spaceContext.SpaceID)
	return analytics, nil
}

// AddStreamConnection adds a new WebSocket connection for real-time events
func (s *StreamService) AddStreamConnection(conn *models.StreamConnection) {
	s.connectionsMux.Lock()
	defer s.connectionsMux.Unlock()
	s.activeConnections[conn.ID] = conn
	s.logger.Info("Added stream connection", zap.String("connection_id", conn.ID), zap.String("user_id", conn.UserID))
}

// RemoveStreamConnection removes a WebSocket connection
func (s *StreamService) RemoveStreamConnection(connectionID string) {
	s.connectionsMux.Lock()
	defer s.connectionsMux.Unlock()
	delete(s.activeConnections, connectionID)
	s.logger.Info("Removed stream connection", zap.String("connection_id", connectionID))
}

// BroadcastEvent broadcasts an event to all connected WebSocket clients
func (s *StreamService) BroadcastEvent(event *models.LiveEvent) {
	s.connectionsMux.RLock()
	connections := make([]*models.StreamConnection, 0, len(s.activeConnections))
	for _, conn := range s.activeConnections {
		connections = append(connections, conn)
	}
	s.connectionsMux.RUnlock()

	for _, conn := range connections {
		// Check if event matches connection filters
		if event.TenantID == conn.TenantID && event.MatchesFilters(conn.Filters) {
			message := &models.StreamEventWebSocketMessage{
				Type:      "live_event",
				Event:     event,
				Timestamp: time.Now(),
			}
			
			// In a real implementation, you would send the message via WebSocket
			// For now, we'll just log it
			s.logger.Debug("Broadcasting event to connection", 
				zap.String("event_id", event.ID),
				zap.String("connection_id", conn.ID),
				zap.Any("message", message))
			
			conn.EventsDelivered++
			conn.LastEventSent = time.Now()
		}
	}
}

// processEvents processes events from the event channel
func (s *StreamService) processEvents() {
	for event := range s.eventChannel {
		start := time.Now()
		
		// Process the event with available processors
		processedEvent := event
		for _, processor := range s.eventProcessors {
			var err error
			processedEvent, err = processor.ProcessEvent(context.Background(), processedEvent)
			if err != nil {
				s.logger.Error("Failed to process event", 
					zap.String("event_id", event.ID),
					zap.String("processor", processor.GetProcessorType()),
					zap.Error(err))
				continue
			}
		}

		// Calculate processing time
		processingTime := float64(time.Since(start).Nanoseconds()) / 1000000.0 // Convert to milliseconds
		processedEvent.ProcessingTime = processingTime

		// Store the processed event
		err := s.storeEvent(context.Background(), processedEvent)
		if err != nil {
			s.logger.Error("Failed to store event", zap.String("event_id", event.ID), zap.Error(err))
			continue
		}

		// Broadcast to WebSocket connections
		s.BroadcastEvent(processedEvent)

		s.logger.Debug("Processed event", 
			zap.String("event_id", event.ID),
			zap.Float64("processing_time_ms", processingTime))
	}
}

// storeEvent stores an event in Neo4j
func (s *StreamService) storeEvent(ctx context.Context, event *models.LiveEvent) error {
	query := `
		CREATE (e:LiveEvent {
			id: $id,
			stream_source_id: $stream_source_id,
			event_type: $event_type,
			content: $content,
			media_type: $media_type,
			media_url: $media_url,
			sentiment: $sentiment,
			sentiment_score: $sentiment_score,
			confidence: $confidence,
			processing_time: $processing_time,
			has_audit_trail: $has_audit_trail,
			audit_score: $audit_score,
			metadata: $metadata,
			extracted_data: $extracted_data,
			processed_at: $processed_at,
			event_timestamp: $event_timestamp,
			tenant_id: $tenant_id,
			organization_id: $organization_id
		})
		WITH e
		MATCH (s:StreamSource {id: $stream_source_id})
		CREATE (s)-[:GENERATED]->(e)
		RETURN e
	`

	parameters := map[string]interface{}{
		"id":               event.ID,
		"stream_source_id": event.StreamSourceID,
		"event_type":       event.EventType,
		"content":          event.Content,
		"media_type":       event.MediaType,
		"media_url":        event.MediaURL,
		"sentiment":        event.Sentiment,
		"sentiment_score":  event.SentimentScore,
		"confidence":       event.Confidence,
		"processing_time":  event.ProcessingTime,
		"has_audit_trail":  event.HasAuditTrail,
		"audit_score":      event.AuditScore,
		"metadata":         serializeParameters(event.Metadata),
		"extracted_data":   serializeParameters(event.ExtractedData),
		"processed_at":     event.ProcessedAt,
		"event_timestamp":  event.EventTimestamp,
		"tenant_id":        event.TenantID,
		"organization_id":  event.OrganizationID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	return err
}

// recordToStreamSource converts a Neo4j record to a StreamSource
func (s *StreamService) recordToStreamSource(record *neo4j.Record, alias string) (*models.StreamSource, error) {
	node, found := record.Get(alias)
	if !found {
		return nil, fmt.Errorf("node %s not found in record", alias)
	}

	nodeValue, ok := node.(neo4j.Node)
	if !ok {
		return nil, fmt.Errorf("expected neo4j.Node, got %T", node)
	}

	props := nodeValue.Props
	source := &models.StreamSource{}

	// Parse source properties
	if id, found := props["id"]; found {
		source.ID = id.(string)
	}
	if name, found := props["name"]; found {
		source.Name = name.(string)
	}
	if sourceType, found := props["type"]; found {
		source.Type = sourceType.(string)
	}
	if provider, found := props["provider"]; found {
		source.Provider = provider.(string)
	}
	if status, found := props["status"]; found {
		source.Status = status.(string)
	}
	if eventsProcessed, found := props["events_processed"]; found {
		if count, ok := eventsProcessed.(int64); ok {
			source.EventsProcessed = count
		}
	}
	if eventsPerSecond, found := props["events_per_second"]; found {
		if rate, ok := eventsPerSecond.(float64); ok {
			source.EventsPerSecond = rate
		}
	}
	if errorCount, found := props["error_count"]; found {
		if count, ok := errorCount.(int64); ok {
			source.ErrorCount = count
		}
	}
	if errorMessage, found := props["error_message"]; found && errorMessage != nil {
		source.ErrorMessage = errorMessage.(string)
	}
	if createdBy, found := props["created_by"]; found {
		source.CreatedBy = createdBy.(string)
	}
	if tenantID, found := props["tenant_id"]; found {
		source.TenantID = tenantID.(string)
	}
	if orgID, found := props["organization_id"]; found {
		source.OrganizationID = orgID.(string)
	}

	// Parse timestamps
	if createdAt, found := props["created_at"]; found {
		if createdTime, ok := createdAt.(time.Time); ok {
			source.CreatedAt = createdTime
		}
	}
	if updatedAt, found := props["updated_at"]; found {
		if updatedTime, ok := updatedAt.(time.Time); ok {
			source.UpdatedAt = updatedTime
		}
	}
	if lastEventAt, found := props["last_event_at"]; found && lastEventAt != nil {
		if lastTime, ok := lastEventAt.(time.Time); ok {
			source.LastEventAt = &lastTime
		}
	}
	if connectedAt, found := props["connected_at"]; found && connectedAt != nil {
		if connTime, ok := connectedAt.(time.Time); ok {
			source.ConnectedAt = &connTime
		}
	}

	// Parse configuration
	if configuration, found := props["configuration"]; found && configuration != nil {
		if configStr, ok := configuration.(string); ok && configStr != "" {
			source.Configuration = deserializeParameters(configStr)
		}
	}

	return source, nil
}

// recordToLiveEvent converts a Neo4j record to a LiveEvent
func (s *StreamService) recordToLiveEvent(record *neo4j.Record, alias string) (*models.LiveEvent, error) {
	node, found := record.Get(alias)
	if !found {
		return nil, fmt.Errorf("node %s not found in record", alias)
	}

	nodeValue, ok := node.(neo4j.Node)
	if !ok {
		return nil, fmt.Errorf("expected neo4j.Node, got %T", node)
	}

	props := nodeValue.Props
	event := &models.LiveEvent{}

	// Parse event properties
	if id, found := props["id"]; found {
		event.ID = id.(string)
	}
	if streamSourceID, found := props["stream_source_id"]; found {
		event.StreamSourceID = streamSourceID.(string)
	}
	if eventType, found := props["event_type"]; found {
		event.EventType = eventType.(string)
	}
	if content, found := props["content"]; found {
		event.Content = content.(string)
	}
	if mediaType, found := props["media_type"]; found {
		event.MediaType = mediaType.(string)
	}
	if mediaURL, found := props["media_url"]; found && mediaURL != nil {
		event.MediaURL = mediaURL.(string)
	}
	if sentiment, found := props["sentiment"]; found {
		event.Sentiment = sentiment.(string)
	}
	if sentimentScore, found := props["sentiment_score"]; found {
		if score, ok := sentimentScore.(float64); ok {
			event.SentimentScore = score
		}
	}
	if confidence, found := props["confidence"]; found {
		if conf, ok := confidence.(float64); ok {
			event.Confidence = conf
		}
	}
	if processingTime, found := props["processing_time"]; found {
		if time, ok := processingTime.(float64); ok {
			event.ProcessingTime = time
		}
	}
	if hasAuditTrail, found := props["has_audit_trail"]; found {
		if audit, ok := hasAuditTrail.(bool); ok {
			event.HasAuditTrail = audit
		}
	}
	if auditScore, found := props["audit_score"]; found {
		if score, ok := auditScore.(float64); ok {
			event.AuditScore = score
		}
	}
	if tenantID, found := props["tenant_id"]; found {
		event.TenantID = tenantID.(string)
	}
	if orgID, found := props["organization_id"]; found {
		event.OrganizationID = orgID.(string)
	}

	// Parse timestamps
	if processedAt, found := props["processed_at"]; found {
		if processedTime, ok := processedAt.(time.Time); ok {
			event.ProcessedAt = processedTime
		}
	}
	if eventTimestamp, found := props["event_timestamp"]; found {
		if eventTime, ok := eventTimestamp.(time.Time); ok {
			event.EventTimestamp = eventTime
		}
	}

	// Parse metadata and extracted data
	if metadata, found := props["metadata"]; found && metadata != nil {
		if metaStr, ok := metadata.(string); ok && metaStr != "" {
			event.Metadata = deserializeParameters(metaStr)
		}
	}
	if extractedData, found := props["extracted_data"]; found && extractedData != nil {
		if dataStr, ok := extractedData.(string); ok && dataStr != "" {
			event.ExtractedData = deserializeParameters(dataStr)
		}
	}

	return event, nil
}

// ProcessEvent implements EventProcessor for sentiment analysis
func (p *SentimentAnalysisProcessor) ProcessEvent(ctx context.Context, event *models.LiveEvent) (*models.LiveEvent, error) {
	// Mock sentiment analysis - in real implementation, this would call an AI service
	content := strings.ToLower(event.Content)
	
	// Simple keyword-based sentiment analysis for demo
	positiveWords := []string{"good", "great", "excellent", "amazing", "wonderful", "fantastic", "love", "happy"}
	negativeWords := []string{"bad", "terrible", "awful", "hate", "sad", "angry", "disappointed", "worst"}
	
	positiveCount := 0
	negativeCount := 0
	
	for _, word := range positiveWords {
		if strings.Contains(content, word) {
			positiveCount++
		}
	}
	
	for _, word := range negativeWords {
		if strings.Contains(content, word) {
			negativeCount++
		}
	}
	
	if positiveCount > negativeCount {
		event.Sentiment = "positive"
		event.SentimentScore = 0.7 + (float64(positiveCount-negativeCount) * 0.1)
	} else if negativeCount > positiveCount {
		event.Sentiment = "negative" 
		event.SentimentScore = -0.7 - (float64(negativeCount-positiveCount) * 0.1)
	} else {
		event.Sentiment = "neutral"
		event.SentimentScore = 0.0
	}
	
	// Ensure score is within bounds
	if event.SentimentScore > 1.0 {
		event.SentimentScore = 1.0
	} else if event.SentimentScore < -1.0 {
		event.SentimentScore = -1.0
	}
	
	// Set confidence based on word count
	wordCount := len(strings.Fields(content))
	if wordCount > 10 {
		event.Confidence = 0.9
	} else if wordCount > 5 {
		event.Confidence = 0.7
	} else {
		event.Confidence = 0.5
	}
	
	p.logger.Debug("Processed sentiment", 
		zap.String("event_id", event.ID),
		zap.String("sentiment", event.Sentiment),
		zap.Float64("score", event.SentimentScore),
		zap.Float64("confidence", event.Confidence))
	
	return event, nil
}

// GetProcessorType returns the processor type
func (p *SentimentAnalysisProcessor) GetProcessorType() string {
	return "sentiment_analysis"
}

// UpdateStreamSourceStatus updates the status of a stream source
func (s *StreamService) UpdateStreamSourceStatus(ctx context.Context, sourceID, status string, spaceContext *models.SpaceContext) (*models.StreamSource, error) {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (s:StreamSource {id: $sourceID, tenant_id: $tenantID})
		SET s.status = $status, s.updated_at = datetime()
		RETURN s.id as id, s.name as name, s.type as type, s.provider as provider, s.status as status,
			   s.configuration as configuration, s.events_processed as events_processed, 
			   s.events_per_second as events_per_second, s.last_event_at as last_event_at,
			   s.connected_at as connected_at, s.error_count as error_count, 
			   s.error_message as error_message, s.created_at as created_at, 
			   s.updated_at as updated_at, s.created_by as created_by, 
			   s.tenant_id as tenant_id, s.organization_id as organization_id
	`

	params := map[string]interface{}{
		"sourceID": sourceID,
		"tenantID": spaceContext.TenantID,
		"status":   status,
	}

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to update stream source status: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		source := &models.StreamSource{}
		
		// Map fields from record to source
		if id, ok := record.Get("id"); ok {
			source.ID, _ = id.(string)
		}
		if name, ok := record.Get("name"); ok {
			source.Name, _ = name.(string)
		}
		if t, ok := record.Get("type"); ok {
			source.Type, _ = t.(string)
		}
		if provider, ok := record.Get("provider"); ok {
			source.Provider, _ = provider.(string)
		}
		if status, ok := record.Get("status"); ok {
			source.Status, _ = status.(string)
		}
		if tenantID, ok := record.Get("tenant_id"); ok {
			source.TenantID, _ = tenantID.(string)
		}
		if orgID, ok := record.Get("organization_id"); ok {
			source.OrganizationID, _ = orgID.(string)
		}
		
		return source, nil
	}

	return nil, nil // Not found
}

// GetLiveEventByID gets a live event by ID
func (s *StreamService) GetLiveEventByID(ctx context.Context, eventID string, spaceContext *models.SpaceContext) (*models.LiveEvent, error) {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (e:LiveEvent {id: $eventID, tenant_id: $tenantID})
		RETURN e.id as id, e.stream_source_id as stream_source_id, e.event_type as event_type,
			   e.content as content, e.media_type as media_type, e.media_url as media_url,
			   e.sentiment as sentiment, e.sentiment_score as sentiment_score,
			   e.confidence as confidence, e.processing_time as processing_time,
			   e.has_audit_trail as has_audit_trail, e.audit_score as audit_score,
			   e.metadata as metadata, e.extracted_data as extracted_data,
			   e.processed_at as processed_at, e.event_timestamp as event_timestamp,
			   e.tenant_id as tenant_id, e.organization_id as organization_id
	`

	params := map[string]interface{}{
		"eventID":  eventID,
		"tenantID": spaceContext.TenantID,
	}

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get live event: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		event := &models.LiveEvent{}
		
		// Map fields from record to event
		if id, ok := record.Get("id"); ok {
			event.ID, _ = id.(string)
		}
		if sourceID, ok := record.Get("stream_source_id"); ok {
			event.StreamSourceID, _ = sourceID.(string)
		}
		if eventType, ok := record.Get("event_type"); ok {
			event.EventType, _ = eventType.(string)
		}
		if content, ok := record.Get("content"); ok {
			event.Content, _ = content.(string)
		}
		if mediaType, ok := record.Get("media_type"); ok {
			event.MediaType, _ = mediaType.(string)
		}
		if sentiment, ok := record.Get("sentiment"); ok {
			event.Sentiment, _ = sentiment.(string)
		}
		if tenantID, ok := record.Get("tenant_id"); ok {
			event.TenantID, _ = tenantID.(string)
		}
		if orgID, ok := record.Get("organization_id"); ok {
			event.OrganizationID, _ = orgID.(string)
		}
		
		return event, nil
	}

	return nil, nil // Not found
}

// GetRealtimeAnalytics gets real-time analytics
func (s *StreamService) GetRealtimeAnalytics(ctx context.Context, spaceContext *models.SpaceContext) (*models.StreamAnalytics, error) {
	// Get current stream sources
	sources, _, err := s.GetStreamSources(ctx, spaceContext, 100, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream sources: %w", err)
	}

	// Get recent events (last hour)
	filters := models.StreamFilters{} // Empty filters to get all events
	events, _, err := s.GetLiveEvents(ctx, spaceContext, filters, 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get live events: %w", err)
	}

	// Calculate real-time analytics
	analytics := models.CalculateRealTimeAnalytics(sources, events, spaceContext.TenantID, spaceContext.SpaceID)
	
	return analytics, nil
}