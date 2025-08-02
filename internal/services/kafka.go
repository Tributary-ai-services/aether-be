package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// KafkaService handles Kafka operations
type KafkaService struct {
	writer  *kafka.Writer
	readers map[string]*kafka.Reader
	logger  *logger.Logger
	config  config.KafkaConfig
	brokers []string
}

// Message represents a Kafka message
type Message struct {
	Topic     string                 `json:"topic"`
	Key       string                 `json:"key,omitempty"`
	Value     interface{}            `json:"value"`
	Headers   map[string]string      `json:"headers,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// EventType represents different types of events
type EventType string

const (
	// User events
	EventUserCreated  EventType = "user.created"
	EventUserUpdated  EventType = "user.updated"
	EventUserDeleted  EventType = "user.deleted"
	EventUserLoggedIn EventType = "user.logged_in"

	// Notebook events
	EventNotebookCreated EventType = "notebook.created"
	EventNotebookUpdated EventType = "notebook.updated"
	EventNotebookDeleted EventType = "notebook.deleted"
	EventNotebookShared  EventType = "notebook.shared"

	// Document events
	EventDocumentUploaded  EventType = "document.uploaded"
	EventDocumentProcessed EventType = "document.processed"
	EventDocumentFailed    EventType = "document.failed"
	EventDocumentDeleted   EventType = "document.deleted"

	// Processing events
	EventProcessingStarted   EventType = "processing.started"
	EventProcessingCompleted EventType = "processing.completed"
	EventProcessingFailed    EventType = "processing.failed"
)

// Event represents a domain event
type Event struct {
	ID        string                 `json:"id"`
	Type      EventType              `json:"type"`
	Source    string                 `json:"source"`
	Subject   string                 `json:"subject"`
	Data      map[string]interface{} `json:"data"`
	UserID    string                 `json:"user_id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Version   string                 `json:"version"`
}

// NewKafkaService creates a new Kafka service
func NewKafkaService(cfg config.KafkaConfig, log *logger.Logger) (*KafkaService, error) {
	service := &KafkaService{
		readers: make(map[string]*kafka.Reader),
		logger:  log.WithService("kafka"),
		config:  cfg,
		brokers: cfg.Brokers,
	}

	// Create writer with default configuration
	service.writer = &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		BatchSize:    100,
		MaxAttempts:  3,
		WriteTimeout: 10 * time.Second,
		ReadTimeout:  10 * time.Second,
		RequiredAcks: kafka.RequireOne,
		Async:        false,
		ErrorLogger:  kafka.LoggerFunc(service.logError),
		Logger:       kafka.LoggerFunc(service.logInfo),
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := service.testConnection(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to Kafka: %w", err)
	}

	service.logger.Info("Kafka service initialized",
		zap.Strings("brokers", cfg.Brokers),
		zap.String("topic_prefix", cfg.TopicPrefix),
	)

	return service, nil
}

// PublishEvent publishes a domain event to Kafka
func (k *KafkaService) PublishEvent(ctx context.Context, event Event) error {
	// Set default values
	if event.ID == "" {
		event.ID = generateEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.Version == "" {
		event.Version = "1.0"
	}
	if event.Source == "" {
		event.Source = "aether-backend"
	}

	// Determine topic based on event type
	topic := k.getTopicForEvent(event.Type)

	// Serialize event
	eventData, err := json.Marshal(event)
	if err != nil {
		k.logger.Error("Failed to serialize event",
			zap.String("event_id", event.ID),
			zap.String("event_type", string(event.Type)),
			zap.Error(err),
		)
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// Create Kafka message
	message := kafka.Message{
		Topic: topic,
		Key:   []byte(event.Subject),
		Value: eventData,
		Headers: []kafka.Header{
			{Key: "event-type", Value: []byte(event.Type)},
			{Key: "event-id", Value: []byte(event.ID)},
			{Key: "source", Value: []byte(event.Source)},
			{Key: "version", Value: []byte(event.Version)},
		},
		Time: event.Timestamp,
	}

	// Add user ID header if present
	if event.UserID != "" {
		message.Headers = append(message.Headers, kafka.Header{
			Key: "user-id", Value: []byte(event.UserID),
		})
	}

	// Publish message
	start := time.Now()
	err = k.writer.WriteMessages(ctx, message)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		k.logger.Error("Failed to publish event",
			zap.String("event_id", event.ID),
			zap.String("event_type", string(event.Type)),
			zap.String("topic", topic),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return fmt.Errorf("failed to publish event: %w", err)
	}

	k.logger.Info("Event published successfully",
		zap.String("event_id", event.ID),
		zap.String("event_type", string(event.Type)),
		zap.String("topic", topic),
		zap.String("subject", event.Subject),
		zap.Float64("duration_ms", duration),
	)

	return nil
}

// PublishMessage publishes a generic message to Kafka
func (k *KafkaService) PublishMessage(ctx context.Context, msg Message) error {
	// Set timestamp if not provided
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	// Serialize message value
	var valueBytes []byte
	var err error

	if msg.Value != nil {
		valueBytes, err = json.Marshal(msg.Value)
		if err != nil {
			k.logger.Error("Failed to serialize message value",
				zap.String("topic", msg.Topic),
				zap.String("key", msg.Key),
				zap.Error(err),
			)
			return fmt.Errorf("failed to serialize message value: %w", err)
		}
	}

	// Create Kafka message
	kafkaMsg := kafka.Message{
		Topic: msg.Topic,
		Key:   []byte(msg.Key),
		Value: valueBytes,
		Time:  msg.Timestamp,
	}

	// Add headers
	for key, value := range msg.Headers {
		kafkaMsg.Headers = append(kafkaMsg.Headers, kafka.Header{
			Key: key, Value: []byte(value),
		})
	}

	// Publish message
	start := time.Now()
	err = k.writer.WriteMessages(ctx, kafkaMsg)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		k.logger.Error("Failed to publish message",
			zap.String("topic", msg.Topic),
			zap.String("key", msg.Key),
			zap.Float64("duration_ms", duration),
			zap.Error(err),
		)
		return fmt.Errorf("failed to publish message: %w", err)
	}

	k.logger.Debug("Message published successfully",
		zap.String("topic", msg.Topic),
		zap.String("key", msg.Key),
		zap.Float64("duration_ms", duration),
	)

	return nil
}

// Subscribe creates a consumer for a topic
func (k *KafkaService) Subscribe(topic string, groupID string, handler MessageHandler) error {
	readerKey := fmt.Sprintf("%s-%s", topic, groupID)

	// Check if reader already exists
	if _, exists := k.readers[readerKey]; exists {
		return fmt.Errorf("reader for topic %s and group %s already exists", topic, groupID)
	}

	// Create reader
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        k.brokers,
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       10e3, // 10KB
		MaxBytes:       10e6, // 10MB
		CommitInterval: time.Second,
		StartOffset:    kafka.LastOffset,
		ErrorLogger:    kafka.LoggerFunc(k.logError),
		Logger:         kafka.LoggerFunc(k.logInfo),
	})

	k.readers[readerKey] = reader

	// Start consuming in a goroutine
	go k.consume(reader, handler, topic, groupID)

	k.logger.Info("Subscribed to topic",
		zap.String("topic", topic),
		zap.String("group_id", groupID),
	)

	return nil
}

// MessageHandler is a function type for handling messages
type MessageHandler func(ctx context.Context, message kafka.Message) error

// consume consumes messages from a Kafka topic
func (k *KafkaService) consume(reader *kafka.Reader, handler MessageHandler, topic, groupID string) {
	for {
		ctx := context.Background()
		message, err := reader.ReadMessage(ctx)
		if err != nil {
			k.logger.Error("Failed to read message",
				zap.String("topic", topic),
				zap.String("group_id", groupID),
				zap.Error(err),
			)
			continue
		}

		start := time.Now()
		err = handler(ctx, message)
		duration := time.Since(start).Seconds() * 1000

		if err != nil {
			k.logger.Error("Message handler failed",
				zap.String("topic", topic),
				zap.String("group_id", groupID),
				zap.String("key", string(message.Key)),
				zap.Float64("duration_ms", duration),
				zap.Error(err),
			)
			// TODO: Implement dead letter queue or retry mechanism
		} else {
			k.logger.Debug("Message processed successfully",
				zap.String("topic", topic),
				zap.String("group_id", groupID),
				zap.String("key", string(message.Key)),
				zap.Float64("duration_ms", duration),
			)
		}
	}
}

// Unsubscribe stops consuming from a topic
func (k *KafkaService) Unsubscribe(topic string, groupID string) error {
	readerKey := fmt.Sprintf("%s-%s", topic, groupID)

	reader, exists := k.readers[readerKey]
	if !exists {
		return fmt.Errorf("no reader found for topic %s and group %s", topic, groupID)
	}

	err := reader.Close()
	if err != nil {
		k.logger.Error("Failed to close reader",
			zap.String("topic", topic),
			zap.String("group_id", groupID),
			zap.Error(err),
		)
		return fmt.Errorf("failed to close reader: %w", err)
	}

	delete(k.readers, readerKey)

	k.logger.Info("Unsubscribed from topic",
		zap.String("topic", topic),
		zap.String("group_id", groupID),
	)

	return nil
}

// Close closes the Kafka service
func (k *KafkaService) Close() error {
	// Close writer
	if err := k.writer.Close(); err != nil {
		k.logger.Error("Failed to close Kafka writer", zap.Error(err))
	}

	// Close all readers
	for key, reader := range k.readers {
		if err := reader.Close(); err != nil {
			k.logger.Error("Failed to close Kafka reader",
				zap.String("reader", key),
				zap.Error(err),
			)
		}
	}

	k.logger.Info("Kafka service closed")
	return nil
}

// HealthCheck performs a health check on the Kafka service
func (k *KafkaService) HealthCheck(ctx context.Context) error {
	return k.testConnection(ctx)
}

// Helper methods

func (k *KafkaService) getTopicForEvent(eventType EventType) string {
	// Map event types to topics
	topicMap := map[EventType]string{
		EventUserCreated:         "users",
		EventUserUpdated:         "users",
		EventUserDeleted:         "users",
		EventUserLoggedIn:        "users",
		EventNotebookCreated:     "notebooks",
		EventNotebookUpdated:     "notebooks",
		EventNotebookDeleted:     "notebooks",
		EventNotebookShared:      "notebooks",
		EventDocumentUploaded:    "documents",
		EventDocumentProcessed:   "documents",
		EventDocumentFailed:      "documents",
		EventDocumentDeleted:     "documents",
		EventProcessingStarted:   "processing",
		EventProcessingCompleted: "processing",
		EventProcessingFailed:    "processing",
	}

	baseTopic, exists := topicMap[eventType]
	if !exists {
		baseTopic = "events"
	}

	// Add prefix if configured
	if k.config.TopicPrefix != "" {
		return fmt.Sprintf("%s.%s", k.config.TopicPrefix, baseTopic)
	}

	return baseTopic
}

func (k *KafkaService) testConnection(ctx context.Context) error {
	// Try to get metadata from brokers
	conn, err := kafka.DialContext(ctx, "tcp", k.brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to Kafka broker: %w", err)
	}
	defer conn.Close()

	brokers, err := conn.Brokers()
	if err != nil {
		return fmt.Errorf("failed to get broker metadata: %w", err)
	}

	if len(brokers) == 0 {
		return fmt.Errorf("no brokers available")
	}

	k.logger.Debug("Kafka connection test successful",
		zap.Int("broker_count", len(brokers)),
	)

	return nil
}

func (k *KafkaService) logError(msg string, args ...interface{}) {
	k.logger.Error("Kafka error", zap.String("message", fmt.Sprintf(msg, args...)))
}

func (k *KafkaService) logInfo(msg string, args ...interface{}) {
	k.logger.Debug("Kafka info", zap.String("message", fmt.Sprintf(msg, args...)))
}

func generateEventID() string {
	// Simple event ID generation - in production, use UUID or similar
	return fmt.Sprintf("evt_%d", time.Now().UnixNano())
}

// Event convenience methods

// NewUserEvent creates a new user-related event
func NewUserEvent(eventType EventType, userID string, data map[string]interface{}) Event {
	return Event{
		Type:    eventType,
		Subject: userID,
		Data:    data,
		UserID:  userID,
	}
}

// NewNotebookEvent creates a new notebook-related event
func NewNotebookEvent(eventType EventType, notebookID, userID string, data map[string]interface{}) Event {
	return Event{
		Type:    eventType,
		Subject: notebookID,
		Data:    data,
		UserID:  userID,
	}
}

// NewDocumentEvent creates a new document-related event
func NewDocumentEvent(eventType EventType, documentID, userID string, data map[string]interface{}) Event {
	return Event{
		Type:    eventType,
		Subject: documentID,
		Data:    data,
		UserID:  userID,
	}
}

// NewProcessingEvent creates a new processing-related event
func NewProcessingEvent(eventType EventType, jobID, documentID, userID string, data map[string]interface{}) Event {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["document_id"] = documentID

	return Event{
		Type:    eventType,
		Subject: jobID,
		Data:    data,
		UserID:  userID,
	}
}
