package services

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

const (
	// DefaultCleanupInterval is how often the cleanup worker runs
	DefaultCleanupInterval = 5 * time.Minute

	// DefaultStaleThreshold is the age after which a processing/rendering
	// production is considered stale and marked as failed
	DefaultStaleThreshold = 30 * time.Minute
)

// ProductionCleanupWorker periodically checks for stale productions
// that are stuck in processing/rendering state (e.g. due to pod restarts)
// and marks them as failed.
type ProductionCleanupWorker struct {
	productionService *ProductionService
	interval          time.Duration
	staleThreshold    time.Duration
	logger            *logger.Logger
	done              chan struct{}
}

// NewProductionCleanupWorker creates a new cleanup worker
func NewProductionCleanupWorker(
	productionService *ProductionService,
	log *logger.Logger,
) *ProductionCleanupWorker {
	return &ProductionCleanupWorker{
		productionService: productionService,
		interval:          DefaultCleanupInterval,
		staleThreshold:    DefaultStaleThreshold,
		logger:            log.WithService("production_cleanup"),
		done:              make(chan struct{}),
	}
}

// Start begins the periodic cleanup loop. It runs until the context is
// cancelled or Stop is called. It also runs one cleanup immediately on start
// to catch any productions orphaned before this pod came up.
func (w *ProductionCleanupWorker) Start(ctx context.Context) {
	w.logger.Info("Starting stale production cleanup worker",
		zap.Duration("interval", w.interval),
		zap.Duration("stale_threshold", w.staleThreshold),
	)

	// Run once immediately on startup to clean up anything orphaned
	// before this pod started
	w.runCleanup(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.runCleanup(ctx)
		case <-ctx.Done():
			w.logger.Info("Production cleanup worker stopped by context")
			return
		case <-w.done:
			w.logger.Info("Production cleanup worker stopped")
			return
		}
	}
}

// Stop signals the worker to stop
func (w *ProductionCleanupWorker) Stop() {
	close(w.done)
}

func (w *ProductionCleanupWorker) runCleanup(ctx context.Context) {
	count, err := w.productionService.CleanupStaleProductions(ctx, w.staleThreshold)
	if err != nil {
		w.logger.Error("Stale production cleanup failed", zap.Error(err))
		return
	}
	if count > 0 {
		w.logger.Info("Stale production cleanup run completed",
			zap.Int("productions_cleaned", count),
		)
	}
}
