package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.Logger with additional functionality
type Logger struct {
	*zap.Logger
}

// Config holds logger configuration
type Config struct {
	Level  string
	Format string // "json" or "console"
}

// New creates a new logger instance
func New(config Config) (*Logger, error) {
	var zapConfig zap.Config

	// Set log level
	level, err := zapcore.ParseLevel(config.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}

	// Configure based on format
	if config.Format == "console" {
		zapConfig = zap.NewDevelopmentConfig()
		zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		zapConfig = zap.NewProductionConfig()
		zapConfig.EncoderConfig.TimeKey = "timestamp"
		zapConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	zapConfig.Level = zap.NewAtomicLevelAt(level)

	// Add caller information for development
	if config.Format == "console" {
		zapConfig.Development = true
		zapConfig.EncoderConfig.CallerKey = "caller"
		zapConfig.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	}

	logger, err := zapConfig.Build(
		zap.AddCallerSkip(1), // Skip one level to show actual caller
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	if err != nil {
		return nil, err
	}

	return &Logger{Logger: logger}, nil
}

// NewDefault creates a logger with default configuration
func NewDefault() (*Logger, error) {
	config := Config{
		Level:  "info",
		Format: "json",
	}

	// Use console format in development
	if os.Getenv("GIN_MODE") == "debug" {
		config.Format = "console"
		config.Level = "debug"
	}

	return New(config)
}

// WithContext adds context fields to the logger
func (l *Logger) WithContext(fields ...zap.Field) *Logger {
	return &Logger{Logger: l.Logger.With(fields...)}
}

// WithRequestID adds request ID to the logger
func (l *Logger) WithRequestID(requestID string) *Logger {
	return l.WithContext(zap.String("request_id", requestID))
}

// WithUserID adds user ID to the logger
func (l *Logger) WithUserID(userID string) *Logger {
	return l.WithContext(zap.String("user_id", userID))
}

// WithService adds service name to the logger
func (l *Logger) WithService(service string) *Logger {
	return l.WithContext(zap.String("service", service))
}

// WithError adds error to the logger
func (l *Logger) WithError(err error) *Logger {
	return l.WithContext(zap.Error(err))
}

// LogHTTPRequest logs HTTP request information
func (l *Logger) LogHTTPRequest(method, path, userAgent, clientIP string, statusCode int, duration float64) {
	l.Info("HTTP request",
		zap.String("method", method),
		zap.String("path", path),
		zap.String("user_agent", userAgent),
		zap.String("client_ip", clientIP),
		zap.Int("status_code", statusCode),
		zap.Float64("duration_ms", duration),
	)
}

// LogDatabaseQuery logs database query information
func (l *Logger) LogDatabaseQuery(query string, duration float64, err error) {
	fields := []zap.Field{
		zap.String("query", query),
		zap.Float64("duration_ms", duration),
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		l.Error("Database query failed", fields...)
	} else {
		l.Debug("Database query executed", fields...)
	}
}

// LogServiceCall logs service call information
func (l *Logger) LogServiceCall(service, operation string, duration float64, err error) {
	fields := []zap.Field{
		zap.String("service", service),
		zap.String("operation", operation),
		zap.Float64("duration_ms", duration),
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		l.Error("Service call failed", fields...)
	} else {
		l.Debug("Service call completed", fields...)
	}
}

// Global logger instance (can be used for convenience)
var globalLogger *Logger

// InitGlobal initializes the global logger
func InitGlobal(config Config) error {
	logger, err := New(config)
	if err != nil {
		return err
	}
	globalLogger = logger
	return nil
}

// Global logger convenience functions
func Info(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Info(msg, fields...)
	}
}

func Debug(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Debug(msg, fields...)
	}
}

func Warn(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Warn(msg, fields...)
	}
}

func Error(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Error(msg, fields...)
	}
}

func Fatal(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Fatal(msg, fields...)
	}
}

func Sync() {
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
}
