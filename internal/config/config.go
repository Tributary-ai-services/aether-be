package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application
type Config struct {
	Server     ServerConfig
	Neo4j      DatabaseConfig
	Redis      RedisConfig
	Keycloak   KeycloakConfig
	Storage    StorageConfig
	Kafka      KafkaConfig
	Monitoring MonitoringConfig
	Logger     LoggingConfig
	AudiModal  AudiModalConfig
}

// ServerConfig holds server-specific configuration
type ServerConfig struct {
	Host         string
	Port         string
	Version      string
	Environment  string
	GinMode      string
	ReadTimeout  int
	WriteTimeout int
	IdleTimeout  int
}

// DatabaseConfig holds Neo4j database configuration
type DatabaseConfig struct {
	URI      string
	Username string
	Password string
	Database string
	MaxConns int
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	PoolSize int
}

// KeycloakConfig holds Keycloak OIDC configuration
type KeycloakConfig struct {
	URL          string
	Realm        string
	ClientID     string
	ClientSecret string
}

// StorageConfig holds S3/MinIO configuration
type StorageConfig struct {
	Enabled         bool
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Endpoint        string
	UseSSL          bool
}

// KafkaConfig holds Kafka configuration
type KafkaConfig struct {
	Enabled     bool
	Brokers     []string
	TopicPrefix string
}

// MonitoringConfig holds monitoring configuration
type MonitoringConfig struct {
	PrometheusEnabled bool
	OTELEndpoint      string
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string
	Format string
}

// AudiModalConfig holds AudiModal API configuration
type AudiModalConfig struct {
	BaseURL string
	APIKey  string
	Enabled bool
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file if it exists (ignore error if file doesn't exist)
	_ = godotenv.Load()

	config := &Config{
		Server: ServerConfig{
			Host:         getEnv("HOST", "0.0.0.0"),
			Port:         getEnv("PORT", "8080"),
			Version:      getEnv("VERSION", "0.1.0"),
			Environment:  getEnv("ENVIRONMENT", "development"),
			GinMode:      getEnv("GIN_MODE", "release"),
			ReadTimeout:  getEnvInt("READ_TIMEOUT", 10),
			WriteTimeout: getEnvInt("WRITE_TIMEOUT", 10),
			IdleTimeout:  getEnvInt("IDLE_TIMEOUT", 60),
		},
		Neo4j: DatabaseConfig{
			URI:      getEnv("NEO4J_URI", "bolt://localhost:7687"),
			Username: getEnv("NEO4J_USERNAME", "neo4j"),
			Password: getEnv("NEO4J_PASSWORD", "password"),
			Database: getEnv("NEO4J_DATABASE", "aether"),
			MaxConns: getEnvInt("NEO4J_MAX_CONNS", 50),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
			PoolSize: getEnvInt("REDIS_POOL_SIZE", 10),
		},
		Keycloak: KeycloakConfig{
			URL:          getEnv("KEYCLOAK_URL", "http://localhost:8081"),
			Realm:        getEnv("KEYCLOAK_REALM", "master"),
			ClientID:     getEnv("KEYCLOAK_CLIENT_ID", "aether-backend"),
			ClientSecret: getEnv("KEYCLOAK_CLIENT_SECRET", ""),
		},
		Storage: StorageConfig{
			Enabled:         getEnvBool("STORAGE_ENABLED", false),
			Region:          getEnv("AWS_REGION", "us-east-1"),
			AccessKeyID:     getEnv("AWS_ACCESS_KEY_ID", ""),
			SecretAccessKey: getEnv("AWS_SECRET_ACCESS_KEY", ""),
			Bucket:          getEnv("S3_BUCKET", "aether-storage"),
			Endpoint:        getEnv("S3_ENDPOINT", ""),
			UseSSL:          getEnvBool("S3_USE_SSL", true),
		},
		Kafka: KafkaConfig{
			Enabled:     getEnvBool("KAFKA_ENABLED", false),
			Brokers:     getEnvSlice("KAFKA_BROKERS", []string{"localhost:9092"}),
			TopicPrefix: getEnv("KAFKA_TOPIC_PREFIX", "aether"),
		},
		Monitoring: MonitoringConfig{
			PrometheusEnabled: getEnvBool("PROMETHEUS_ENABLED", true),
			OTELEndpoint:      getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		},
		Logger: LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		AudiModal: AudiModalConfig{
			BaseURL: getEnv("AUDIMODAL_BASE_URL", "http://audimodal:8080"),
			APIKey:  getEnv("AUDIMODAL_API_KEY", ""),
			Enabled: getEnvBool("AUDIMODAL_ENABLED", true),
		},
	}

	// Validate required configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Neo4j.Password == "" {
		return fmt.Errorf("NEO4J_PASSWORD is required")
	}

	if c.Keycloak.ClientSecret == "" && c.Keycloak.URL != "" {
		return fmt.Errorf("KEYCLOAK_CLIENT_SECRET is required when Keycloak is configured")
	}

	if c.Storage.Enabled && (c.Storage.AccessKeyID == "" || c.Storage.SecretAccessKey == "") {
		return fmt.Errorf("AWS credentials (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY) are required when storage is enabled")
	}

	if c.Kafka.Enabled && len(c.Kafka.Brokers) == 0 {
		return fmt.Errorf("at least one Kafka broker is required when Kafka is enabled")
	}

	return nil
}

// IsDevelopment returns true if running in development mode
func (c *Config) IsDevelopment() bool {
	return c.Server.GinMode == "debug" || c.Server.GinMode == "dev"
}

// IsProduction returns true if running in production mode
func (c *Config) IsProduction() bool {
	return c.Server.GinMode == "release"
}

// Helper functions for environment variables

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
