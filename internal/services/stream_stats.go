package services

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"go.uber.org/zap"
)

// StreamStatsService queries TimescaleDB continuous aggregates to back the
// Live Streams summary cards. It runs against tas-timescaledb-shared.events_1m
// and falls back to a 0-value response when the DB pool is nil so the API
// stays available if Timescale is down.
type StreamStatsService struct {
	db     *sql.DB
	logger *logger.Logger
}

// StreamStats is the response shape returned by GetStats.
type StreamStats struct {
	EventsPerMin   int64           `json:"events_per_min"`
	ActiveSources  int             `json:"active_sources"`
	BySeverity     map[string]int64 `json:"by_severity"`
	WindowMinutes  int              `json:"window_minutes"`
	Source         string           `json:"source"` // "timescaledb" or "unavailable"
}

// NewStreamStatsService returns a stats service. Pass nil db to disable —
// the service will return zero-value stats on every call, which keeps the
// frontend rendering stable even when Timescale is down.
func NewStreamStatsService(db *sql.DB, log *logger.Logger) *StreamStatsService {
	return &StreamStatsService{
		db:     db,
		logger: log.WithService("stream_stats_service"),
	}
}

// GetStats returns events-per-minute and active source count for tenantID
// over the last 5 minutes by summing event_count from events_1m.
func (s *StreamStatsService) GetStats(ctx context.Context, tenantID string) (*StreamStats, error) {
	stats := &StreamStats{
		BySeverity:    map[string]int64{},
		WindowMinutes: 5,
		Source:        "timescaledb",
	}

	if s.db == nil {
		stats.Source = "unavailable"
		return stats, nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	const totalsQuery = `
SELECT
    COALESCE(SUM(event_count), 0)::bigint AS total,
    COUNT(DISTINCT source) AS active_sources
FROM events_1m
WHERE bucket >= NOW() - INTERVAL '5 minutes'
  AND ($1 = '' OR tenant_id = $1)`

	if err := s.db.QueryRowContext(queryCtx, totalsQuery, tenantID).Scan(&stats.EventsPerMin, &stats.ActiveSources); err != nil {
		s.logger.Warn("stream stats: totals query failed", zap.Error(err))
		stats.Source = "unavailable"
		return stats, fmt.Errorf("query totals: %w", err)
	}

	const severityQuery = `
SELECT severity, COALESCE(SUM(event_count), 0)::bigint
FROM events_1m
WHERE bucket >= NOW() - INTERVAL '5 minutes'
  AND ($1 = '' OR tenant_id = $1)
GROUP BY severity`

	rows, err := s.db.QueryContext(queryCtx, severityQuery, tenantID)
	if err != nil {
		s.logger.Warn("stream stats: severity query failed", zap.Error(err))
		return stats, nil // totals are still valid
	}
	defer rows.Close()

	for rows.Next() {
		var sev string
		var n int64
		if err := rows.Scan(&sev, &n); err != nil {
			continue
		}
		stats.BySeverity[sev] = n
	}

	return stats, nil
}

// Close releases the underlying database connection pool.
func (s *StreamStatsService) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
