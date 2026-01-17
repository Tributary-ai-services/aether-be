package database

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// RedisClient wraps the Redis client with additional functionality
type RedisClient struct {
	client *redis.Client
	logger *logger.Logger
	config config.RedisConfig
}

// NewRedisClient creates a new Redis client
func NewRedisClient(cfg config.RedisConfig, log *logger.Logger) (*RedisClient, error) {
	// Configure Redis options
	options := &redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.PoolSize / 4,
		MaxRetries:   3,
		DialTimeout:  time.Second * 5,
		ReadTimeout:  time.Second * 3,
		WriteTimeout: time.Second * 3,
		PoolTimeout:  time.Second * 4,
	}

	// Create Redis client
	client := redis.NewClient(options)

	redisClient := &RedisClient{
		client: client,
		logger: log.WithService("redis"),
		config: cfg,
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	redisClient.logger.Info("Connected to Redis",
		zap.String("addr", cfg.Addr),
		zap.Int("db", cfg.DB),
		zap.Int("pool_size", cfg.PoolSize),
	)

	return redisClient, nil
}

// Ping tests the connection to Redis
func (r *RedisClient) Ping(ctx context.Context) error {
	start := time.Now()
	result := r.client.Ping(ctx)
	duration := time.Since(start).Seconds() * 1000

	err := result.Err()
	r.logger.LogServiceCall("redis", "ping", duration, err)

	return err
}

// Close closes the Redis connection
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// Get retrieves a value by key
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
	start := time.Now()
	result := r.client.Get(ctx, key)
	duration := time.Since(start).Seconds() * 1000

	value, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("get:%s", key), duration, err)

	if err == redis.Nil {
		return "", nil // Key doesn't exist, but this is not an error
	}

	return value, err
}

// Set stores a key-value pair with expiration
func (r *RedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	start := time.Now()
	result := r.client.Set(ctx, key, value, expiration)
	duration := time.Since(start).Seconds() * 1000

	err := result.Err()
	r.logger.LogServiceCall("redis", fmt.Sprintf("set:%s", key), duration, err)

	return err
}

// Delete removes a key
func (r *RedisClient) Delete(ctx context.Context, keys ...string) error {
	start := time.Now()
	result := r.client.Del(ctx, keys...)
	duration := time.Since(start).Seconds() * 1000

	err := result.Err()
	r.logger.LogServiceCall("redis", fmt.Sprintf("del:%v", keys), duration, err)

	return err
}

// Exists checks if a key exists
func (r *RedisClient) Exists(ctx context.Context, keys ...string) (int64, error) {
	start := time.Now()
	result := r.client.Exists(ctx, keys...)
	duration := time.Since(start).Seconds() * 1000

	count, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("exists:%v", keys), duration, err)

	return count, err
}

// SetNX sets a key only if it doesn't exist (atomic operation)
func (r *RedisClient) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	start := time.Now()
	result := r.client.SetNX(ctx, key, value, expiration)
	duration := time.Since(start).Seconds() * 1000

	success, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("setnx:%s", key), duration, err)

	return success, err
}

// Increment increments a numeric value
func (r *RedisClient) Increment(ctx context.Context, key string) (int64, error) {
	start := time.Now()
	result := r.client.Incr(ctx, key)
	duration := time.Since(start).Seconds() * 1000

	value, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("incr:%s", key), duration, err)

	return value, err
}

// IncrementBy increments a numeric value by a specific amount
func (r *RedisClient) IncrementBy(ctx context.Context, key string, value int64) (int64, error) {
	start := time.Now()
	result := r.client.IncrBy(ctx, key, value)
	duration := time.Since(start).Seconds() * 1000

	newValue, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("incrby:%s", key), duration, err)

	return newValue, err
}

// Expire sets an expiration time for a key
func (r *RedisClient) Expire(ctx context.Context, key string, expiration time.Duration) error {
	start := time.Now()
	result := r.client.Expire(ctx, key, expiration)
	duration := time.Since(start).Seconds() * 1000

	err := result.Err()
	r.logger.LogServiceCall("redis", fmt.Sprintf("expire:%s", key), duration, err)

	return err
}

// TTL returns the time to live for a key
func (r *RedisClient) TTL(ctx context.Context, key string) (time.Duration, error) {
	start := time.Now()
	result := r.client.TTL(ctx, key)
	duration := time.Since(start).Seconds() * 1000

	ttl, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("ttl:%s", key), duration, err)

	return ttl, err
}

// Hash operations

// HSet sets fields in a hash
func (r *RedisClient) HSet(ctx context.Context, key string, values ...interface{}) error {
	start := time.Now()
	result := r.client.HSet(ctx, key, values...)
	duration := time.Since(start).Seconds() * 1000

	err := result.Err()
	r.logger.LogServiceCall("redis", fmt.Sprintf("hset:%s", key), duration, err)

	return err
}

// HGet gets a field from a hash
func (r *RedisClient) HGet(ctx context.Context, key, field string) (string, error) {
	start := time.Now()
	result := r.client.HGet(ctx, key, field)
	duration := time.Since(start).Seconds() * 1000

	value, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("hget:%s:%s", key, field), duration, err)

	if err == redis.Nil {
		return "", nil // Field doesn't exist
	}

	return value, err
}

// HGetAll gets all fields from a hash
func (r *RedisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	start := time.Now()
	result := r.client.HGetAll(ctx, key)
	duration := time.Since(start).Seconds() * 1000

	values, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("hgetall:%s", key), duration, err)

	return values, err
}

// HDel deletes fields from a hash
func (r *RedisClient) HDel(ctx context.Context, key string, fields ...string) error {
	start := time.Now()
	result := r.client.HDel(ctx, key, fields...)
	duration := time.Since(start).Seconds() * 1000

	err := result.Err()
	r.logger.LogServiceCall("redis", fmt.Sprintf("hdel:%s", key), duration, err)

	return err
}

// List operations

// LPush pushes elements to the head of a list
func (r *RedisClient) LPush(ctx context.Context, key string, values ...interface{}) error {
	start := time.Now()
	result := r.client.LPush(ctx, key, values...)
	duration := time.Since(start).Seconds() * 1000

	err := result.Err()
	r.logger.LogServiceCall("redis", fmt.Sprintf("lpush:%s", key), duration, err)

	return err
}

// RPop pops an element from the tail of a list
func (r *RedisClient) RPop(ctx context.Context, key string) (string, error) {
	start := time.Now()
	result := r.client.RPop(ctx, key)
	duration := time.Since(start).Seconds() * 1000

	value, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("rpop:%s", key), duration, err)

	if err == redis.Nil {
		return "", nil // List is empty
	}

	return value, err
}

// LLen returns the length of a list
func (r *RedisClient) LLen(ctx context.Context, key string) (int64, error) {
	start := time.Now()
	result := r.client.LLen(ctx, key)
	duration := time.Since(start).Seconds() * 1000

	length, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("llen:%s", key), duration, err)

	return length, err
}

// Set operations

// SAdd adds members to a set
func (r *RedisClient) SAdd(ctx context.Context, key string, members ...interface{}) error {
	start := time.Now()
	result := r.client.SAdd(ctx, key, members...)
	duration := time.Since(start).Seconds() * 1000

	err := result.Err()
	r.logger.LogServiceCall("redis", fmt.Sprintf("sadd:%s", key), duration, err)

	return err
}

// SMembers returns all members of a set
func (r *RedisClient) SMembers(ctx context.Context, key string) ([]string, error) {
	start := time.Now()
	result := r.client.SMembers(ctx, key)
	duration := time.Since(start).Seconds() * 1000

	members, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("smembers:%s", key), duration, err)

	return members, err
}

// SIsMember checks if a member exists in a set
func (r *RedisClient) SIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	start := time.Now()
	result := r.client.SIsMember(ctx, key, member)
	duration := time.Since(start).Seconds() * 1000

	exists, err := result.Result()
	r.logger.LogServiceCall("redis", fmt.Sprintf("sismember:%s", key), duration, err)

	return exists, err
}

// Pipeline operations

// Pipeline creates a new pipeline
func (r *RedisClient) Pipeline() redis.Pipeliner {
	return r.client.Pipeline()
}

// TxPipeline creates a new transaction pipeline
func (r *RedisClient) TxPipeline() redis.Pipeliner {
	return r.client.TxPipeline()
}

// HealthCheck performs a health check on the Redis connection
func (r *RedisClient) HealthCheck(ctx context.Context) error {
	return r.Ping(ctx)
}

// GetStats returns Redis client statistics
func (r *RedisClient) GetStats() *redis.PoolStats {
	return r.client.PoolStats()
}

// FlushDB flushes the current database (use with caution!)
func (r *RedisClient) FlushDB(ctx context.Context) error {
	start := time.Now()
	result := r.client.FlushDB(ctx)
	duration := time.Since(start).Seconds() * 1000

	err := result.Err()
	r.logger.LogServiceCall("redis", "flushdb", duration, err)

	if err == nil {
		r.logger.Warn("Redis database flushed")
	}

	return err
}
