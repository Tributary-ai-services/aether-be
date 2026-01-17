package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// MockNeo4jClient is a mock implementation of Neo4jClient
type MockNeo4jClient struct {
	mock.Mock
}

func (m *MockNeo4jClient) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) (interface{}, error) {
	args := m.Called(ctx, query, params)
	return args.Get(0), args.Error(1)
}

func (m *MockNeo4jClient) ExecuteQueryWithLogging(ctx context.Context, query string, params map[string]interface{}) (interface{}, error) {
	args := m.Called(ctx, query, params)
	return args.Get(0), args.Error(1)
}

func (m *MockNeo4jClient) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockNeo4jClient) VerifyConnectivity(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// MockRedisClient is a mock implementation of RedisClient
type MockRedisClient struct {
	mock.Mock
}

func (m *MockRedisClient) Get(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}

func (m *MockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	args := m.Called(ctx, key, value, expiration)
	return args.Error(0)
}

func (m *MockRedisClient) Del(ctx context.Context, keys ...string) error {
	args := m.Called(ctx, keys)
	return args.Error(0)
}

func (m *MockRedisClient) Delete(ctx context.Context, keys ...string) error {
	args := m.Called(ctx, keys)
	return args.Error(0)
}

func (m *MockRedisClient) Exists(ctx context.Context, keys ...string) (int64, error) {
	args := m.Called(ctx, keys)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockRedisClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockRedisClient) Ping(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockRedisClient) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	args := m.Called(ctx, key, value, expiration)
	return args.Bool(0), args.Error(1)
}

func (m *MockRedisClient) Incr(ctx context.Context, key string) (int64, error) {
	args := m.Called(ctx, key)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockRedisClient) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	args := m.Called(ctx, key, expiration)
	return args.Bool(0), args.Error(1)
}

// MockStorageService is a mock implementation of StorageService
type MockStorageService struct {
	mock.Mock
}

func (m *MockStorageService) UploadFile(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	args := m.Called(ctx, key, data, contentType)
	return args.String(0), args.Error(1)
}

func (m *MockStorageService) DownloadFile(ctx context.Context, key string) ([]byte, error) {
	args := m.Called(ctx, key)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockStorageService) DeleteFile(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *MockStorageService) GetFileURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	args := m.Called(ctx, key, expiration)
	return args.String(0), args.Error(1)
}

// setupTestLogger creates a test logger with minimal output
func setupTestLogger(t *testing.T) *logger.Logger {
	loggerConfig := logger.Config{
		Level:  "error", // Reduce log noise in tests
		Format: "json",
	}
	testLogger, err := logger.New(loggerConfig)
	require.NoError(t, err)
	return testLogger
}

// Helper function to create string pointers for test data
func stringPtr(s string) *string {
	return &s
}
