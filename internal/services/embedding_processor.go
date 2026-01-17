package services

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// EmbeddingProcessor handles background processing of embeddings
type EmbeddingProcessor struct {
	embeddingService *EmbeddingService
	chunkService     ChunkServiceInterface
	config           *config.EmbeddingConfig
	log              *logger.Logger
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	mu               sync.RWMutex
	isRunning        bool
}

// ChunkServiceInterface defines the interface for chunk operations
type ChunkServiceInterface interface {
	GetPendingEmbeddingChunks(ctx context.Context, tenantID string, limit int) ([]*models.Chunk, error)
	UpdateChunkEmbeddingStatus(ctx context.Context, chunkID, status string) error
	GetChunksByStatus(ctx context.Context, tenantID, status string, limit int) ([]*models.Chunk, error)
}

// ProcessingStats holds statistics about embedding processing
type ProcessingStats struct {
	TotalProcessed     int64     `json:"total_processed"`
	TotalFailed        int64     `json:"total_failed"`
	LastProcessedAt    time.Time `json:"last_processed_at"`
	CurrentBatchSize   int       `json:"current_batch_size"`
	AverageProcessTime float64   `json:"average_process_time_ms"`
	IsRunning          bool      `json:"is_running"`
}

// NewEmbeddingProcessor creates a new embedding processor
func NewEmbeddingProcessor(
	embeddingService *EmbeddingService,
	chunkService ChunkServiceInterface,
	config *config.EmbeddingConfig,
	log *logger.Logger,
) *EmbeddingProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	return &EmbeddingProcessor{
		embeddingService: embeddingService,
		chunkService:     chunkService,
		config:           config,
		log:              log,
		ctx:              ctx,
		cancel:           cancel,
		isRunning:        false,
	}
}

// Start begins the background embedding processing
func (p *EmbeddingProcessor) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isRunning {
		return nil // Already running
	}

	if !p.config.Enabled {
		p.log.Info("Embedding processing is disabled")
		return nil
	}

	p.isRunning = true
	p.wg.Add(1)

	go p.processingLoop()

	p.log.Info("Embedding processor started",
		zap.Int("batch_size", p.config.BatchSize),
		zap.Int("processing_interval", p.config.ProcessingInterval),
	)

	return nil
}

// Stop gracefully stops the embedding processor
func (p *EmbeddingProcessor) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isRunning {
		return nil // Already stopped
	}

	p.log.Info("Stopping embedding processor...")
	p.cancel()
	p.isRunning = false

	// Wait for processing to complete with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.log.Info("Embedding processor stopped successfully")
	case <-time.After(30 * time.Second):
		p.log.Warn("Embedding processor stop timeout - some operations may still be running")
	}

	return nil
}

// IsRunning returns whether the processor is currently running
func (p *EmbeddingProcessor) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.isRunning
}

// ProcessTenant processes pending embeddings for a specific tenant
func (p *EmbeddingProcessor) ProcessTenant(ctx context.Context, tenantID string) (*BatchEmbeddingResult, error) {
	chunks, err := p.chunkService.GetPendingEmbeddingChunks(ctx, tenantID, p.config.BatchSize)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		return &BatchEmbeddingResult{
			Successful: []EmbeddingResponse{},
			Failed:     []EmbeddingError{},
			TotalCount: 0,
		}, nil
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
				"processed_by": chunk.ProcessedBy,
			},
		}
	}

	// Process embeddings
	result, err := p.embeddingService.GenerateBatchEmbeddings(ctx, requests)
	if err != nil {
		p.log.Error("Failed to process embeddings for tenant",
			zap.String("tenant_id", tenantID),
			zap.Error(err),
		)
		return nil, err
	}

	// Update chunk statuses
	for _, response := range result.Successful {
		if err := p.chunkService.UpdateChunkEmbeddingStatus(ctx, response.ChunkID, "completed"); err != nil {
			p.log.Error("Failed to update chunk embedding status",
				zap.String("chunk_id", response.ChunkID),
				zap.Error(err),
			)
		}
	}

	for _, failed := range result.Failed {
		if err := p.chunkService.UpdateChunkEmbeddingStatus(ctx, failed.ChunkID, "failed"); err != nil {
			p.log.Error("Failed to update failed chunk embedding status",
				zap.String("chunk_id", failed.ChunkID),
				zap.Error(err),
			)
		}
	}

	p.log.Info("Processed embeddings for tenant",
		zap.String("tenant_id", tenantID),
		zap.Int("successful", len(result.Successful)),
		zap.Int("failed", len(result.Failed)),
		zap.Duration("duration", result.Duration),
	)

	return result, nil
}

// RetryFailedEmbeddings retries processing failed embeddings
func (p *EmbeddingProcessor) RetryFailedEmbeddings(ctx context.Context, tenantID string) (*BatchEmbeddingResult, error) {
	chunks, err := p.chunkService.GetChunksByStatus(ctx, tenantID, "failed", p.config.BatchSize)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		return &BatchEmbeddingResult{
			Successful: []EmbeddingResponse{},
			Failed:     []EmbeddingError{},
			TotalCount: 0,
		}, nil
	}

	// Reset status to pending before retry
	for _, chunk := range chunks {
		if err := p.chunkService.UpdateChunkEmbeddingStatus(ctx, chunk.ID, "pending"); err != nil {
			p.log.Error("Failed to reset chunk status for retry",
				zap.String("chunk_id", chunk.ID),
				zap.Error(err),
			)
		}
	}

	// Process as normal
	return p.ProcessTenant(ctx, tenantID)
}

// GetStats returns processing statistics
func (p *EmbeddingProcessor) GetStats() ProcessingStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// This is a basic implementation - in production you'd want to track these metrics
	return ProcessingStats{
		IsRunning:        p.isRunning,
		CurrentBatchSize: p.config.BatchSize,
	}
}

// processingLoop runs the main processing loop
func (p *EmbeddingProcessor) processingLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(time.Duration(p.config.ProcessingInterval) * time.Second)
	defer ticker.Stop()

	p.log.Info("Embedding processing loop started")

	for {
		select {
		case <-p.ctx.Done():
			p.log.Info("Embedding processing loop stopped")
			return

		case <-ticker.C:
			p.processAllTenants()
		}
	}
}

// processAllTenants processes pending embeddings for all tenants
func (p *EmbeddingProcessor) processAllTenants() {
	// In a real implementation, you'd get a list of active tenants
	// For now, we'll use a placeholder approach
	tenants := p.getActiveTenants()

	for _, tenantID := range tenants {
		select {
		case <-p.ctx.Done():
			return
		default:
			if err := p.processTenantwithRetry(tenantID); err != nil {
				p.log.Error("Failed to process tenant embeddings",
					zap.String("tenant_id", tenantID),
					zap.Error(err),
				)
			}
		}
	}
}

// processTenantwithRetry processes a tenant with retry logic
func (p *EmbeddingProcessor) processTenantwithRetry(tenantID string) error {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Minute)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt < p.config.MaxRetries; attempt++ {
		result, err := p.ProcessTenant(ctx, tenantID)
		if err != nil {
			lastErr = err
			p.log.Warn("Embedding processing attempt failed",
				zap.String("tenant_id", tenantID),
				zap.Int("attempt", attempt+1),
				zap.Error(err),
			)

			// Exponential backoff
			backoff := time.Duration(attempt+1) * time.Second
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Success
		if result.TotalCount > 0 {
			p.log.Debug("Successfully processed tenant embeddings",
				zap.String("tenant_id", tenantID),
				zap.Int("processed", len(result.Successful)),
			)
		}
		return nil
	}

	return lastErr
}

// getActiveTenants returns a list of active tenant IDs
// This is a placeholder - in production, this would query the database
func (p *EmbeddingProcessor) getActiveTenants() []string {
	// For now, return a hardcoded list - this should be replaced with actual tenant discovery
	return []string{
		"9855e094-36a6-4d3a-a4f5-d77da4614439", // Example tenant from AudiModal
	}
}

// ProcessChunksImmediately processes specific chunks immediately without waiting for the batch processor
func (p *EmbeddingProcessor) ProcessChunksImmediately(ctx context.Context, chunkIDs []string) (*BatchEmbeddingResult, error) {
	if len(chunkIDs) == 0 {
		return &BatchEmbeddingResult{
			Successful: []EmbeddingResponse{},
			Failed:     []EmbeddingError{},
			TotalCount: 0,
		}, nil
	}

	// This would need to be implemented to fetch chunks by IDs
	// For now, return a placeholder
	p.log.Info("Immediate processing requested for chunks",
		zap.Strings("chunk_ids", chunkIDs),
	)

	return &BatchEmbeddingResult{
		Successful: []EmbeddingResponse{},
		Failed:     []EmbeddingError{},
		TotalCount: len(chunkIDs),
	}, nil
}