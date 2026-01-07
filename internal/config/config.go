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
	Embedding  EmbeddingConfig
	DeepLake   DeepLakeConfig
	OpenAI     OpenAIConfig
	Compliance ComplianceConfig
	Router     RouterConfig
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
	URI         string
	Username    string
	Password    string
	Database    string
	MaxConns    int
	TLSInsecure bool
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
	BaseURL              string
	APIKey               string
	Enabled              bool
	DefaultStrategy      string
	TenantManagement     bool
	ProcessingTimeout    int
	RetryAttempts        int
	EnableWebhooks       bool
	WebhookSecret        string
	MaxConcurrentFiles   int
	ChunkSizeLimit       int
}

// EmbeddingConfig holds embedding service configuration
type EmbeddingConfig struct {
	Provider           string
	BatchSize          int
	MaxRetries         int
	ProcessingInterval int
	Enabled            bool
}

// DeepLakeConfig holds DeepLake vector storage configuration
type DeepLakeConfig struct {
	BaseURL          string
	APIKey           string
	CollectionName   string
	VectorDimensions int
	TimeoutSeconds   int
	Enabled          bool
}

// OpenAIConfig holds OpenAI API configuration
type OpenAIConfig struct {
	APIKey         string
	Model          string
	BaseURL        string
	Dimensions     int
	TimeoutSeconds int
}

// ComplianceConfig holds compliance scanning configuration
type ComplianceConfig struct {
	Enabled             bool
	GDPREnabled         bool
	HIPAAEnabled        bool
	CCPAEnabled         bool
	PIIDetectionEnabled bool
	DataClassificationEnabled bool
	BatchSize           int
	ScanInterval        int
	RetentionDays       int
	MaskPII             bool
	EncryptSensitive    bool
}

// RouterConfig holds LLM router proxy configuration
type RouterConfig struct {
	Enabled     bool                 `json:"enabled"`
	Service     RouterServiceConfig  `json:"service"`
	Endpoints   RouterEndpoints      `json:"endpoints"`
	ProxyRoutes []ProxyRoute         `json:"proxy_routes"`
}

// RouterServiceConfig holds router service connection configuration
type RouterServiceConfig struct {
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	UseServiceAuth bool   `json:"use_service_auth"`
	Timeout        string `json:"timeout"`
	MaxRetries     int    `json:"max_retries"`
	ConnectTimeout string `json:"connect_timeout"`
}

// RouterEndpoints holds router endpoint paths
type RouterEndpoints struct {
	Providers      string `json:"providers"`
	ProviderDetail string `json:"provider_detail"`
	Health         string `json:"health"`
	Capabilities   string `json:"capabilities"`
	ChatCompletions string `json:"chat_completions"`
	Completions    string `json:"completions"`
	Messages       string `json:"messages"`
}

// ProxyRoute defines a proxy route mapping
type ProxyRoute struct {
	Path    string   `json:"path"`
	Target  string   `json:"target"`
	Methods []string `json:"methods"`
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
			URI:         getEnv("NEO4J_URI", "bolt://localhost:7687"),
			Username:    getEnv("NEO4J_USERNAME", "neo4j"),
			Password:    getEnv("NEO4J_PASSWORD", "password"),
			Database:    getEnv("NEO4J_DATABASE", "aether"),
			MaxConns:    getEnvInt("NEO4J_MAX_CONNS", 50),
			TLSInsecure: getEnvBool("NEO4J_TLS_INSECURE", false),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
			PoolSize: getEnvInt("REDIS_POOL_SIZE", 10),
		},
		Keycloak: KeycloakConfig{
			URL:          getEnv("KEYCLOAK_URL", "http://localhost:8081"),
			Realm:        getEnv("KEYCLOAK_REALM", "aether"),
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
			BaseURL:              getEnv("AUDIMODAL_BASE_URL", "http://audimodal:8080"),
			APIKey:               getEnv("AUDIMODAL_API_KEY", ""),
			Enabled:              getEnvBool("AUDIMODAL_ENABLED", true),
			DefaultStrategy:      getEnv("AUDIMODAL_DEFAULT_STRATEGY", "semantic"),
			TenantManagement:     getEnvBool("AUDIMODAL_TENANT_MANAGEMENT", true),
			ProcessingTimeout:    getEnvInt("AUDIMODAL_PROCESSING_TIMEOUT", 300),
			RetryAttempts:        getEnvInt("AUDIMODAL_RETRY_ATTEMPTS", 3),
			EnableWebhooks:       getEnvBool("AUDIMODAL_ENABLE_WEBHOOKS", true),
			WebhookSecret:        getEnv("AUDIMODAL_WEBHOOK_SECRET", ""),
			MaxConcurrentFiles:   getEnvInt("AUDIMODAL_MAX_CONCURRENT_FILES", 5),
			ChunkSizeLimit:       getEnvInt("AUDIMODAL_CHUNK_SIZE_LIMIT", 4096),
		},
		Embedding: EmbeddingConfig{
			Provider:           getEnv("EMBEDDING_PROVIDER", "openai"),
			BatchSize:          getEnvInt("EMBEDDING_BATCH_SIZE", 50),
			MaxRetries:         getEnvInt("EMBEDDING_MAX_RETRIES", 3),
			ProcessingInterval: getEnvInt("EMBEDDING_PROCESSING_INTERVAL", 30),
			Enabled:            getEnvBool("EMBEDDING_ENABLED", true),
		},
		DeepLake: DeepLakeConfig{
			BaseURL:          getEnv("DEEPLAKE_BASE_URL", "http://localhost:8000"),
			APIKey:           getEnv("DEEPLAKE_API_KEY", ""),
			CollectionName:   getEnv("DEEPLAKE_COLLECTION_NAME", "aether_embeddings"),
			VectorDimensions: getEnvInt("DEEPLAKE_VECTOR_DIMENSIONS", 1536),
			TimeoutSeconds:   getEnvInt("DEEPLAKE_TIMEOUT_SECONDS", 30),
			Enabled:          getEnvBool("DEEPLAKE_ENABLED", true),
		},
		OpenAI: OpenAIConfig{
			APIKey:         getEnv("OPENAI_API_KEY", ""),
			Model:          getEnv("OPENAI_EMBEDDING_MODEL", "text-embedding-ada-002"),
			BaseURL:        getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			Dimensions:     getEnvInt("OPENAI_EMBEDDING_DIMENSIONS", 1536),
			TimeoutSeconds: getEnvInt("OPENAI_TIMEOUT_SECONDS", 30),
		},
		Compliance: ComplianceConfig{
			Enabled:                   getEnvBool("COMPLIANCE_ENABLED", true),
			GDPREnabled:               getEnvBool("COMPLIANCE_GDPR_ENABLED", true),
			HIPAAEnabled:              getEnvBool("COMPLIANCE_HIPAA_ENABLED", true),
			CCPAEnabled:               getEnvBool("COMPLIANCE_CCPA_ENABLED", false),
			PIIDetectionEnabled:       getEnvBool("COMPLIANCE_PII_DETECTION_ENABLED", true),
			DataClassificationEnabled: getEnvBool("COMPLIANCE_DATA_CLASSIFICATION_ENABLED", true),
			BatchSize:                 getEnvInt("COMPLIANCE_BATCH_SIZE", 20),
			ScanInterval:              getEnvInt("COMPLIANCE_SCAN_INTERVAL", 60),
			RetentionDays:             getEnvInt("COMPLIANCE_RETENTION_DAYS", 365),
			MaskPII:                   getEnvBool("COMPLIANCE_MASK_PII", true),
			EncryptSensitive:          getEnvBool("COMPLIANCE_ENCRYPT_SENSITIVE", true),
		},
		Router: RouterConfig{
			Enabled: getEnvBool("ROUTER_ENABLED", true),
			Service: RouterServiceConfig{
				BaseURL:        getEnv("ROUTER_SERVICE_BASE_URL", "http://localhost:8086"),
				APIKey:         getEnv("ROUTER_API_KEY", ""),
				UseServiceAuth: getEnvBool("ROUTER_USE_SERVICE_AUTH", false),
				Timeout:        getEnv("ROUTER_SERVICE_TIMEOUT", "30s"),
				MaxRetries:     getEnvInt("ROUTER_SERVICE_MAX_RETRIES", 3),
				ConnectTimeout: getEnv("ROUTER_SERVICE_CONNECT_TIMEOUT", "10s"),
			},
			Endpoints: RouterEndpoints{
				Providers:       getEnv("ROUTER_ENDPOINT_PROVIDERS", "/v1/providers"),
				ProviderDetail:  getEnv("ROUTER_ENDPOINT_PROVIDER_DETAIL", "/v1/providers/{name}"),
				Health:          getEnv("ROUTER_ENDPOINT_HEALTH", "/v1/health"),
				Capabilities:    getEnv("ROUTER_ENDPOINT_CAPABILITIES", "/v1/capabilities"),
				ChatCompletions: getEnv("ROUTER_ENDPOINT_CHAT_COMPLETIONS", "/v1/chat/completions"),
				Completions:     getEnv("ROUTER_ENDPOINT_COMPLETIONS", "/v1/completions"),
				Messages:        getEnv("ROUTER_ENDPOINT_MESSAGES", "/v1/messages"),
			},
			ProxyRoutes: getDefaultProxyRoutes(),
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

	if c.Router.Enabled {
		if c.Router.Service.BaseURL == "" {
			return fmt.Errorf("ROUTER_SERVICE_BASE_URL is required when router is enabled")
		}
		if c.Router.Service.Timeout == "" {
			return fmt.Errorf("ROUTER_SERVICE_TIMEOUT is required when router is enabled")
		}
		if c.Router.Service.ConnectTimeout == "" {
			return fmt.Errorf("ROUTER_SERVICE_CONNECT_TIMEOUT is required when router is enabled")
		}
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

// getDefaultProxyRoutes returns the default proxy route configuration
func getDefaultProxyRoutes() []ProxyRoute {
	return []ProxyRoute{
		{
			Path:    "/api/v1/router/providers",
			Target:  "/v1/providers",
			Methods: []string{"GET"},
		},
		{
			Path:    "/api/v1/router/providers/{name}",
			Target:  "/v1/providers/{name}",
			Methods: []string{"GET"},
		},
		{
			Path:    "/api/v1/router/health",
			Target:  "/v1/health",
			Methods: []string{"GET"},
		},
		{
			Path:    "/api/v1/router/capabilities",
			Target:  "/v1/capabilities",
			Methods: []string{"GET"},
		},
		{
			Path:    "/api/v1/router/chat/completions",
			Target:  "/v1/chat/completions",
			Methods: []string{"POST"},
		},
		{
			Path:    "/api/v1/router/completions",
			Target:  "/v1/completions",
			Methods: []string{"POST"},
		},
		{
			Path:    "/api/v1/router/messages",
			Target:  "/v1/messages",
			Methods: []string{"POST"},
		},
	}
}
