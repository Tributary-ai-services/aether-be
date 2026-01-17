package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// EmbeddingProvider defines the interface for embedding generation
type EmbeddingProvider interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
	GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error)
	GetDimensions() int
	GetModelName() string
}

// EmbeddingService handles document chunk embedding generation
type EmbeddingService struct {
	provider    EmbeddingProvider
	vectorStore VectorStoreService
	log         *logger.Logger
	config      *config.EmbeddingConfig
}

// EmbeddingRequest represents a request for embedding generation
type EmbeddingRequest struct {
	ChunkID   string `json:"chunk_id"`
	Content   string `json:"content"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Priority  int    `json:"priority,omitempty"`
}

// EmbeddingResponse represents the response from embedding generation
type EmbeddingResponse struct {
	ChunkID    string    `json:"chunk_id"`
	Embedding  []float32 `json:"embedding"`
	Dimensions int       `json:"dimensions"`
	Model      string    `json:"model"`
	ProcessedAt time.Time `json:"processed_at"`
}

// BatchEmbeddingResult represents results from batch embedding generation
type BatchEmbeddingResult struct {
	Successful []EmbeddingResponse `json:"successful"`
	Failed     []EmbeddingError    `json:"failed"`
	TotalCount int                 `json:"total_count"`
	Duration   time.Duration       `json:"duration"`
}

// EmbeddingError represents an error during embedding generation
type EmbeddingError struct {
	ChunkID string `json:"chunk_id"`
	Error   string `json:"error"`
}

// NewEmbeddingService creates a new embedding service
func NewEmbeddingService(
	provider EmbeddingProvider,
	vectorStore VectorStoreService,
	log *logger.Logger,
	config *config.EmbeddingConfig,
) *EmbeddingService {
	return &EmbeddingService{
		provider:    provider,
		vectorStore: vectorStore,
		log:         log,
		config:      config,
	}
}

// GenerateEmbedding generates an embedding for a single chunk
func (s *EmbeddingService) GenerateEmbedding(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	start := time.Now()

	embedding, err := s.provider.GenerateEmbedding(ctx, req.Content)
	if err != nil {
		s.log.Error("Failed to generate embedding",
			zap.String("chunk_id", req.ChunkID),
			zap.Error(err),
		)
		return nil, errors.NewAPIError(
			errors.ErrInternal,
			"Failed to generate embedding",
			map[string]interface{}{"chunk_id": req.ChunkID},
		)
	}

	response := &EmbeddingResponse{
		ChunkID:     req.ChunkID,
		Embedding:   embedding,
		Dimensions:  s.provider.GetDimensions(),
		Model:       s.provider.GetModelName(),
		ProcessedAt: time.Now(),
	}

	// Store embedding in vector database
	if err := s.vectorStore.StoreEmbedding(ctx, req.ChunkID, embedding, req.Metadata); err != nil {
		s.log.Error("Failed to store embedding in vector database",
			zap.String("chunk_id", req.ChunkID),
			zap.Error(err),
		)
		// Don't fail the request if vector storage fails, but log it
	}

	s.log.Info("Successfully generated embedding",
		zap.String("chunk_id", req.ChunkID),
		zap.Int("dimensions", len(embedding)),
		zap.Duration("duration", time.Since(start)),
	)

	return response, nil
}

// GenerateBatchEmbeddings generates embeddings for multiple chunks
func (s *EmbeddingService) GenerateBatchEmbeddings(ctx context.Context, requests []EmbeddingRequest) (*BatchEmbeddingResult, error) {
	start := time.Now()

	if len(requests) == 0 {
		return &BatchEmbeddingResult{
			Successful: []EmbeddingResponse{},
			Failed:     []EmbeddingError{},
			TotalCount: 0,
			Duration:   time.Since(start),
		}, nil
	}

	// Extract texts for batch processing
	texts := make([]string, len(requests))
	for i, req := range requests {
		texts[i] = req.Content
	}

	// Generate embeddings in batch
	embeddings, err := s.provider.GenerateBatchEmbeddings(ctx, texts)
	if err != nil {
		s.log.Error("Batch embedding generation failed", zap.Error(err))
		return nil, errors.NewAPIError(
			errors.ErrInternal,
			"Batch embedding generation failed",
			nil,
		)
	}

	result := &BatchEmbeddingResult{
		Successful: make([]EmbeddingResponse, 0, len(requests)),
		Failed:     make([]EmbeddingError, 0),
		TotalCount: len(requests),
	}

	// Process results
	for i, req := range requests {
		if i >= len(embeddings) {
			result.Failed = append(result.Failed, EmbeddingError{
				ChunkID: req.ChunkID,
				Error:   "No embedding returned for chunk",
			})
			continue
		}

		embedding := embeddings[i]
		if len(embedding) == 0 {
			result.Failed = append(result.Failed, EmbeddingError{
				ChunkID: req.ChunkID,
				Error:   "Empty embedding returned",
			})
			continue
		}

		response := EmbeddingResponse{
			ChunkID:     req.ChunkID,
			Embedding:   embedding,
			Dimensions:  s.provider.GetDimensions(),
			Model:       s.provider.GetModelName(),
			ProcessedAt: time.Now(),
		}

		// Store in vector database
		if err := s.vectorStore.StoreEmbedding(ctx, req.ChunkID, embedding, req.Metadata); err != nil {
			s.log.Warn("Failed to store embedding in vector database",
				zap.String("chunk_id", req.ChunkID),
				zap.Error(err),
			)
		}

		result.Successful = append(result.Successful, response)
	}

	result.Duration = time.Since(start)

	s.log.Info("Batch embedding generation completed",
		zap.Int("total", result.TotalCount),
		zap.Int("successful", len(result.Successful)),
		zap.Int("failed", len(result.Failed)),
		zap.Duration("duration", result.Duration),
	)

	return result, nil
}

// ProcessPendingEmbeddings processes chunks with pending embedding status
func (s *EmbeddingService) ProcessPendingEmbeddings(ctx context.Context, tenantID string, limit int) error {
	chunks, err := s.getPendingChunks(ctx, tenantID, limit)
	if err != nil {
		return fmt.Errorf("failed to get pending chunks: %w", err)
	}

	if len(chunks) == 0 {
		s.log.Debug("No pending chunks found for embedding generation", zap.String("tenant_id", tenantID))
		return nil
	}

	// Convert chunks to embedding requests
	requests := make([]EmbeddingRequest, len(chunks))
	for i, chunk := range chunks {
		requests[i] = EmbeddingRequest{
			ChunkID:  chunk.ID,
			Content:  chunk.Content,
			Metadata: map[string]interface{}{
				"file_id":     chunk.FileID,
				"chunk_type":  chunk.ChunkType,
				"tenant_id":   chunk.TenantID,
			},
		}
	}

	// Process embeddings in batches
	batchSize := s.config.BatchSize
	if batchSize == 0 {
		batchSize = 50 // Default batch size
	}

	for i := 0; i < len(requests); i += batchSize {
		end := i + batchSize
		if end > len(requests) {
			end = len(requests)
		}

		batch := requests[i:end]
		result, err := s.GenerateBatchEmbeddings(ctx, batch)
		if err != nil {
			s.log.Error("Failed to process embedding batch",
				zap.Int("batch_start", i),
				zap.Int("batch_size", len(batch)),
				zap.Error(err),
			)
			continue
		}

		// Update chunk statuses
		for _, response := range result.Successful {
			if err := s.updateChunkEmbeddingStatus(ctx, response.ChunkID, "completed"); err != nil {
				s.log.Error("Failed to update chunk embedding status",
					zap.String("chunk_id", response.ChunkID),
					zap.Error(err),
				)
			}
		}

		for _, failed := range result.Failed {
			if err := s.updateChunkEmbeddingStatus(ctx, failed.ChunkID, "failed"); err != nil {
				s.log.Error("Failed to update failed chunk embedding status",
					zap.String("chunk_id", failed.ChunkID),
					zap.Error(err),
				)
			}
		}
	}

	return nil
}

// getPendingChunks retrieves chunks with pending embedding status
func (s *EmbeddingService) getPendingChunks(ctx context.Context, tenantID string, limit int) ([]*models.Chunk, error) {
	// This would typically query the database for chunks with embedding_status = "pending"
	// For now, return empty slice - this needs to be implemented with actual database queries
	return []*models.Chunk{}, nil
}

// updateChunkEmbeddingStatus updates the embedding status of a chunk
func (s *EmbeddingService) updateChunkEmbeddingStatus(ctx context.Context, chunkID, status string) error {
	// This would typically update the chunk's embedding_status in the database
	// For now, return nil - this needs to be implemented with actual database updates
	return nil
}

// VectorStoreService defines interface for vector storage operations
type VectorStoreService interface {
	StoreEmbedding(ctx context.Context, chunkID string, embedding []float32, metadata map[string]interface{}) error
	SearchSimilar(ctx context.Context, queryEmbedding []float32, limit int, threshold float64) ([]VectorSearchResult, error)
	DeleteEmbedding(ctx context.Context, chunkID string) error
}

// VectorSearchResult represents a result from vector similarity search
type VectorSearchResult struct {
	ChunkID    string                 `json:"chunk_id"`
	Score      float64                `json:"score"`
	Metadata   map[string]interface{} `json:"metadata"`
	Embedding  []float32              `json:"embedding,omitempty"`
}