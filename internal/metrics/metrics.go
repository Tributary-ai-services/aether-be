package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// Metrics contains all Prometheus metrics for the application
type Metrics struct {
	// HTTP metrics
	httpRequestsTotal    *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	httpRequestsInFlight prometheus.Gauge
	httpRequestSize      *prometheus.HistogramVec
	httpResponseSize     *prometheus.HistogramVec

	// Database metrics
	dbConnectionsActive prometheus.Gauge
	dbConnectionsIdle   prometheus.Gauge
	dbQueriesTotal      *prometheus.CounterVec
	dbQueryDuration     *prometheus.HistogramVec

	// Redis metrics
	redisConnectionsActive prometheus.Gauge
	redisOperationsTotal   *prometheus.CounterVec
	redisOperationDuration *prometheus.HistogramVec

	// Application metrics
	documentsTotal         *prometheus.CounterVec
	documentsProcessing    prometheus.Gauge
	documentProcessingTime *prometheus.HistogramVec
	usersTotal             *prometheus.CounterVec
	notebooksTotal         *prometheus.CounterVec

	// Storage metrics
	storageOperationsTotal   *prometheus.CounterVec
	storageOperationDuration *prometheus.HistogramVec
	storageBytesTotal        *prometheus.CounterVec

	// External service metrics
	externalRequestsTotal   *prometheus.CounterVec
	externalRequestDuration *prometheus.HistogramVec

	// System metrics
	goroutinesActive prometheus.Gauge
	memoryUsage      prometheus.Gauge

	logger *logger.Logger
}

// NewMetrics creates a new metrics instance with all Prometheus metrics
func NewMetrics(log *logger.Logger) *Metrics {
	m := &Metrics{
		logger: log.WithService("metrics"),

		// HTTP metrics
		httpRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status_code"},
		),
		httpRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
			},
			[]string{"method", "endpoint"},
		),
		httpRequestsInFlight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "http_requests_in_flight",
				Help: "Current number of HTTP requests being processed",
			},
		),
		httpRequestSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_size_bytes",
				Help:    "HTTP request size in bytes",
				Buckets: []float64{100, 1000, 10000, 100000, 1000000, 10000000},
			},
			[]string{"method", "endpoint"},
		),
		httpResponseSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: []float64{100, 1000, 10000, 100000, 1000000, 10000000},
			},
			[]string{"method", "endpoint"},
		),

		// Database metrics
		dbConnectionsActive: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "database_connections_active",
				Help: "Number of active database connections",
			},
		),
		dbConnectionsIdle: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "database_connections_idle",
				Help: "Number of idle database connections",
			},
		),
		dbQueriesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "database_queries_total",
				Help: "Total number of database queries",
			},
			[]string{"database", "operation", "status"},
		),
		dbQueryDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "database_query_duration_seconds",
				Help:    "Database query duration in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
			},
			[]string{"database", "operation"},
		),

		// Redis metrics
		redisConnectionsActive: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "redis_connections_active",
				Help: "Number of active Redis connections",
			},
		),
		redisOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "redis_operations_total",
				Help: "Total number of Redis operations",
			},
			[]string{"operation", "status"},
		),
		redisOperationDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "redis_operation_duration_seconds",
				Help:    "Redis operation duration in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
			},
			[]string{"operation"},
		),

		// Application metrics
		documentsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "documents_total",
				Help: "Total number of documents",
			},
			[]string{"status", "type"},
		),
		documentsProcessing: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "documents_processing",
				Help: "Number of documents currently being processed",
			},
		),
		documentProcessingTime: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "document_processing_duration_seconds",
				Help:    "Document processing duration in seconds",
				Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
			},
			[]string{"type", "status"},
		),
		usersTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "users_total",
				Help: "Total number of users",
			},
			[]string{"status"},
		),
		notebooksTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "notebooks_total",
				Help: "Total number of notebooks",
			},
			[]string{"visibility"},
		),

		// Storage metrics
		storageOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "storage_operations_total",
				Help: "Total number of storage operations",
			},
			[]string{"operation", "status"},
		),
		storageOperationDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "storage_operation_duration_seconds",
				Help:    "Storage operation duration in seconds",
				Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60},
			},
			[]string{"operation"},
		),
		storageBytesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "storage_bytes_total",
				Help: "Total bytes stored",
			},
			[]string{"operation", "bucket"},
		),

		// External service metrics
		externalRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "external_requests_total",
				Help: "Total number of external service requests",
			},
			[]string{"service", "operation", "status"},
		),
		externalRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "external_request_duration_seconds",
				Help:    "External service request duration in seconds",
				Buckets: []float64{0.1, 0.5, 1, 5, 10, 30},
			},
			[]string{"service", "operation"},
		),

		// System metrics
		goroutinesActive: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "goroutines_active",
				Help: "Number of active goroutines",
			},
		),
		memoryUsage: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "memory_usage_bytes",
				Help: "Memory usage in bytes",
			},
		),
	}

	// Register all metrics
	prometheus.MustRegister(
		m.httpRequestsTotal,
		m.httpRequestDuration,
		m.httpRequestsInFlight,
		m.httpRequestSize,
		m.httpResponseSize,
		m.dbConnectionsActive,
		m.dbConnectionsIdle,
		m.dbQueriesTotal,
		m.dbQueryDuration,
		m.redisConnectionsActive,
		m.redisOperationsTotal,
		m.redisOperationDuration,
		m.documentsTotal,
		m.documentsProcessing,
		m.documentProcessingTime,
		m.usersTotal,
		m.notebooksTotal,
		m.storageOperationsTotal,
		m.storageOperationDuration,
		m.storageBytesTotal,
		m.externalRequestsTotal,
		m.externalRequestDuration,
		m.goroutinesActive,
		m.memoryUsage,
	)

	m.logger.Info("Prometheus metrics initialized")
	return m
}

// HTTP Metrics methods

// RecordHTTPRequest records an HTTP request metric
func (m *Metrics) RecordHTTPRequest(method, path string, statusCode int, duration time.Duration, requestSize, responseSize int64) {
	statusStr := strconv.Itoa(statusCode)

	m.httpRequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	m.httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
	m.httpRequestSize.WithLabelValues(method, path).Observe(float64(requestSize))
	m.httpResponseSize.WithLabelValues(method, path).Observe(float64(responseSize))
}

// IncHTTPRequestsInFlight increments the in-flight requests counter
func (m *Metrics) IncHTTPRequestsInFlight() {
	m.httpRequestsInFlight.Inc()
}

// DecHTTPRequestsInFlight decrements the in-flight requests counter
func (m *Metrics) DecHTTPRequestsInFlight() {
	m.httpRequestsInFlight.Dec()
}

// Database Metrics methods

// SetDBConnections sets the number of active and idle database connections
func (m *Metrics) SetDBConnections(active, idle int) {
	m.dbConnectionsActive.Set(float64(active))
	m.dbConnectionsIdle.Set(float64(idle))
}

// RecordDBQuery records a database query metric
func (m *Metrics) RecordDBQuery(database, operation, status string, duration time.Duration) {
	m.dbQueriesTotal.WithLabelValues(database, operation, status).Inc()
	m.dbQueryDuration.WithLabelValues(database, operation).Observe(duration.Seconds())
}

// Redis Metrics methods

// SetRedisConnections sets the number of active Redis connections
func (m *Metrics) SetRedisConnections(active int) {
	m.redisConnectionsActive.Set(float64(active))
}

// RecordRedisOperation records a Redis operation metric
func (m *Metrics) RecordRedisOperation(operation, status string, duration time.Duration) {
	m.redisOperationsTotal.WithLabelValues(operation, status).Inc()
	m.redisOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// Application Metrics methods

// IncDocumentsTotal increments the total documents counter
func (m *Metrics) IncDocumentsTotal(status, docType string) {
	m.documentsTotal.WithLabelValues(status, docType).Inc()
}

// SetDocumentsProcessing sets the number of documents being processed
func (m *Metrics) SetDocumentsProcessing(count int) {
	m.documentsProcessing.Set(float64(count))
}

// RecordDocumentProcessing records document processing duration
func (m *Metrics) RecordDocumentProcessing(docType, status string, duration time.Duration) {
	m.documentProcessingTime.WithLabelValues(docType, status).Observe(duration.Seconds())
}

// IncUsersTotal increments the total users counter
func (m *Metrics) IncUsersTotal(status string) {
	m.usersTotal.WithLabelValues(status).Inc()
}

// IncNotebooksTotal increments the total notebooks counter
func (m *Metrics) IncNotebooksTotal(visibility string) {
	m.notebooksTotal.WithLabelValues(visibility).Inc()
}

// Storage Metrics methods

// RecordStorageOperation records a storage operation metric
func (m *Metrics) RecordStorageOperation(operation, status string, duration time.Duration, bytes int64, bucket string) {
	m.storageOperationsTotal.WithLabelValues(operation, status).Inc()
	m.storageOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
	if bytes > 0 {
		m.storageBytesTotal.WithLabelValues(operation, bucket).Add(float64(bytes))
	}
}

// External Service Metrics methods

// RecordExternalRequest records an external service request metric
func (m *Metrics) RecordExternalRequest(service, operation, status string, duration time.Duration) {
	m.externalRequestsTotal.WithLabelValues(service, operation, status).Inc()
	m.externalRequestDuration.WithLabelValues(service, operation).Observe(duration.Seconds())
}

// System Metrics methods

// SetGoroutines sets the number of active goroutines
func (m *Metrics) SetGoroutines(count int) {
	m.goroutinesActive.Set(float64(count))
}

// SetMemoryUsage sets the memory usage in bytes
func (m *Metrics) SetMemoryUsage(bytes int64) {
	m.memoryUsage.Set(float64(bytes))
}

// Handler returns the Prometheus metrics HTTP handler
func (m *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}

// GinHandler returns a Gin handler for Prometheus metrics
func (m *Metrics) GinHandler() gin.HandlerFunc {
	handler := promhttp.Handler()
	return gin.WrapH(handler)
}

// Shutdown gracefully shuts down the metrics system
func (m *Metrics) Shutdown(ctx context.Context) error {
	m.logger.Info("Shutting down metrics system")
	// Prometheus metrics don't need explicit shutdown
	return nil
}
