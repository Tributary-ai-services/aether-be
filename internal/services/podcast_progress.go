package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

const (
	// ProgressKeyPrefix is the Redis key prefix for podcast progress hashes
	ProgressKeyPrefix = "podcast:progress:"
	// ProgressTTL is how long progress data lives in Redis
	ProgressTTL = 2 * time.Hour
)

// ProgressState represents the current progress of a podcast production
type ProgressState struct {
	Phase          string `json:"phase"` // tts_generating, assembling, uploading, completed, failed
	ClipsTotal     int    `json:"clips_total"`
	ClipsCompleted int    `json:"clips_completed"`
	ClipsFailed    int    `json:"clips_failed"`
	Message        string `json:"message"`
	WorkerPod      string `json:"worker_pod"`
	UpdatedAt      string `json:"updated_at"`
	MediaURL       string `json:"media_url,omitempty"`
}

// ProgressEvent represents a progress event sent to SSE subscribers
type ProgressEvent struct {
	ProductionID string        `json:"production_id"`
	State        ProgressState `json:"state"`
}

// PodcastProgressService manages podcast production progress tracking via Redis and SSE
type PodcastProgressService struct {
	redis             *database.RedisClient
	neo4j             *database.Neo4jClient
	productionService *ProductionService
	logger            *logger.Logger

	// In-memory pub/sub for SSE (same pattern as CommentService)
	mu          sync.RWMutex
	subscribers map[string][]chan *ProgressEvent // productionID -> channels
}

// NewPodcastProgressService creates a new podcast progress service
func NewPodcastProgressService(
	redis *database.RedisClient,
	neo4j *database.Neo4jClient,
	productionService *ProductionService,
	log *logger.Logger,
) *PodcastProgressService {
	return &PodcastProgressService{
		redis:             redis,
		neo4j:             neo4j,
		productionService: productionService,
		logger:            log.WithService("podcast_progress"),
		subscribers:       make(map[string][]chan *ProgressEvent),
	}
}

// GetProgress reads the current progress from Redis hash
func (s *PodcastProgressService) GetProgress(ctx context.Context, productionID string) (*ProgressState, error) {
	if s.redis == nil {
		return nil, fmt.Errorf("redis not available")
	}

	key := ProgressKeyPrefix + productionID
	fields, err := s.redis.HGetAll(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to read progress from Redis: %w", err)
	}

	if len(fields) == 0 {
		return nil, nil // No progress data yet
	}

	state := &ProgressState{
		Phase:     fields["phase"],
		Message:   fields["message"],
		WorkerPod: fields["worker_pod"],
		UpdatedAt: fields["updated_at"],
		MediaURL:  fields["media_url"],
	}

	if v, err := strconv.Atoi(fields["clips_total"]); err == nil {
		state.ClipsTotal = v
	}
	if v, err := strconv.Atoi(fields["clips_completed"]); err == nil {
		state.ClipsCompleted = v
	}
	if v, err := strconv.Atoi(fields["clips_failed"]); err == nil {
		state.ClipsFailed = v
	}

	return state, nil
}

// Subscribe creates a new subscription channel for SSE streaming
func (s *PodcastProgressService) Subscribe(productionID string) chan *ProgressEvent {
	ch := make(chan *ProgressEvent, 10)
	s.mu.Lock()
	s.subscribers[productionID] = append(s.subscribers[productionID], ch)
	s.mu.Unlock()

	s.logger.Debug("SSE subscriber added",
		zap.String("production_id", productionID),
	)

	return ch
}

// Unsubscribe removes a subscription channel
func (s *PodcastProgressService) Unsubscribe(productionID string, ch chan *ProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subs := s.subscribers[productionID]
	for i, sub := range subs {
		if sub == ch {
			s.subscribers[productionID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
	if len(s.subscribers[productionID]) == 0 {
		delete(s.subscribers, productionID)
	}

	s.logger.Debug("SSE subscriber removed",
		zap.String("production_id", productionID),
	)
}

// publish sends a progress event to all SSE subscribers for a production
func (s *PodcastProgressService) publish(productionID string, event *ProgressEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.subscribers[productionID] {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}

// HandleProgressMessage is the Kafka consumer callback for podcast.progress topic
func (s *PodcastProgressService) HandleProgressMessage(ctx context.Context, message kafka.Message) error {
	var event Event
	if err := json.Unmarshal(message.Value, &event); err != nil {
		s.logger.Error("Failed to unmarshal progress event", zap.Error(err))
		return err
	}

	productionID := event.Subject
	eventType := event.Type

	s.logger.Info("Received podcast progress event",
		zap.String("production_id", productionID),
		zap.String("event_type", string(eventType)),
	)

	switch eventType {
	case EventPodcastProgressUpdate:
		return s.handleProgressUpdate(ctx, productionID, event.Data)
	case EventPodcastCompleted:
		return s.handleCompleted(ctx, productionID, event.Data)
	case EventPodcastFailed:
		return s.handleFailed(ctx, productionID, event.Data)
	default:
		s.logger.Warn("Unknown podcast event type", zap.String("type", string(eventType)))
		return nil
	}
}

func (s *PodcastProgressService) handleProgressUpdate(ctx context.Context, productionID string, data map[string]interface{}) error {
	state := ProgressState{
		Phase:     getStringFromMap(data, "phase"),
		Message:   getStringFromMap(data, "message"),
		WorkerPod: getStringFromMap(data, "worker_pod"),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if v, ok := data["clips_total"].(float64); ok {
		state.ClipsTotal = int(v)
	}
	if v, ok := data["clips_completed"].(float64); ok {
		state.ClipsCompleted = int(v)
	}
	if v, ok := data["clips_failed"].(float64); ok {
		state.ClipsFailed = int(v)
	}

	// Update Redis progress hash
	if s.redis != nil {
		key := ProgressKeyPrefix + productionID
		err := s.redis.HSet(ctx, key,
			"phase", state.Phase,
			"clips_total", strconv.Itoa(state.ClipsTotal),
			"clips_completed", strconv.Itoa(state.ClipsCompleted),
			"clips_failed", strconv.Itoa(state.ClipsFailed),
			"message", state.Message,
			"worker_pod", state.WorkerPod,
			"updated_at", state.UpdatedAt,
		)
		if err != nil {
			s.logger.Error("Failed to update Redis progress", zap.Error(err))
		}
		_ = s.redis.Expire(ctx, key, ProgressTTL)
	}

	// Update Neo4j production progress_phase
	s.updateNeo4jProgressPhase(ctx, productionID, state.Phase)

	// Dispatch to SSE subscribers
	s.publish(productionID, &ProgressEvent{
		ProductionID: productionID,
		State:        state,
	})

	return nil
}

func (s *PodcastProgressService) handleCompleted(ctx context.Context, productionID string, data map[string]interface{}) error {
	mediaURL := getStringFromMap(data, "media_url")

	// Update Redis with final state (include media_url for polling fallback)
	if s.redis != nil {
		key := ProgressKeyPrefix + productionID
		_ = s.redis.HSet(ctx, key,
			"phase", "completed",
			"message", "Podcast generation completed",
			"media_url", mediaURL,
			"updated_at", time.Now().UTC().Format(time.RFC3339),
		)
		_ = s.redis.Expire(ctx, key, ProgressTTL)
	}

	// Update Neo4j production to completed with media URL
	s.updateNeo4jCompleted(ctx, productionID, mediaURL, data)

	// Dispatch final event to SSE subscribers (include media_url so frontend can show player)
	s.publish(productionID, &ProgressEvent{
		ProductionID: productionID,
		State: ProgressState{
			Phase:    "completed",
			Message:  "Podcast generation completed",
			MediaURL: mediaURL,
		},
	})

	return nil
}

func (s *PodcastProgressService) handleFailed(ctx context.Context, productionID string, data map[string]interface{}) error {
	errorMessage := getStringFromMap(data, "error")
	retryCount := 0
	if v, ok := data["retry_count"].(float64); ok {
		retryCount = int(v)
	}

	// Update Redis with failure state
	if s.redis != nil {
		key := ProgressKeyPrefix + productionID
		_ = s.redis.HSet(ctx, key,
			"phase", "failed",
			"message", errorMessage,
			"updated_at", time.Now().UTC().Format(time.RFC3339),
		)
		_ = s.redis.Expire(ctx, key, ProgressTTL)
	}

	// Update Neo4j production to failed
	s.updateNeo4jFailed(ctx, productionID, errorMessage, retryCount)

	// Dispatch failure event to SSE subscribers
	s.publish(productionID, &ProgressEvent{
		ProductionID: productionID,
		State: ProgressState{
			Phase:   "failed",
			Message: errorMessage,
		},
	})

	return nil
}

// Neo4j update helpers

func (s *PodcastProgressService) updateNeo4jProgressPhase(ctx context.Context, productionID, phase string) {
	query := `
		MATCH (p:Production {id: $id})
		SET p.progress_phase = $phase, p.updated_at = datetime()
		RETURN p.id
	`
	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"id":    productionID,
		"phase": phase,
	})
	if err != nil {
		s.logger.Error("Failed to update Neo4j progress_phase",
			zap.String("production_id", productionID),
			zap.Error(err))
	}
}

func (s *PodcastProgressService) updateNeo4jCompleted(ctx context.Context, productionID, mediaURL string, data map[string]interface{}) {
	// Build metadata from data
	metadata := map[string]interface{}{}
	if v, ok := data["duration"]; ok {
		metadata["duration"] = v
	}
	if v, ok := data["segments"]; ok {
		metadata["segments"] = v
	}
	if v, ok := data["provider"]; ok {
		metadata["provider"] = v
	}

	metadataJSON, _ := json.Marshal(metadata)

	query := `
		MATCH (p:Production {id: $id})
		SET p.status = $status,
		    p.media_url = $media_url,
		    p.media_metadata = $media_metadata,
		    p.progress_phase = 'completed',
		    p.updated_at = datetime()
		RETURN p.id
	`
	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"id":             productionID,
		"status":         string(models.ProductionStatusCompleted),
		"media_url":      mediaURL,
		"media_metadata": string(metadataJSON),
	})
	if err != nil {
		s.logger.Error("Failed to update Neo4j production to completed",
			zap.String("production_id", productionID),
			zap.Error(err))
	}
}

func (s *PodcastProgressService) updateNeo4jFailed(ctx context.Context, productionID, errorMessage string, retryCount int) {
	query := `
		MATCH (p:Production {id: $id})
		SET p.status = $status,
		    p.error_message = $error_message,
		    p.retry_count = $retry_count,
		    p.progress_phase = 'failed',
		    p.updated_at = datetime()
		RETURN p.id
	`
	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"id":            productionID,
		"status":        string(models.ProductionStatusFailed),
		"error_message": errorMessage,
		"retry_count":   retryCount,
	})
	if err != nil {
		s.logger.Error("Failed to update Neo4j production to failed",
			zap.String("production_id", productionID),
			zap.Error(err))
	}
}

// GetRedisUpdatedAt reads the updated_at timestamp from Redis for stale checking
func (s *PodcastProgressService) GetRedisUpdatedAt(ctx context.Context, productionID string) (time.Time, error) {
	if s.redis == nil {
		return time.Time{}, fmt.Errorf("redis not available")
	}

	key := ProgressKeyPrefix + productionID
	val, err := s.redis.HGet(ctx, key, "updated_at")
	if err != nil {
		return time.Time{}, err
	}
	if val == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, val)
}

// helper
func getStringFromMap(data map[string]interface{}, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}
