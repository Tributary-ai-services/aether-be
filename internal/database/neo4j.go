package database

import (
	"context"
	"crypto/tls"
	"fmt"
	"reflect"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jconfig "github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// Neo4jClient wraps the Neo4j driver with additional functionality
type Neo4jClient struct {
	driver neo4j.DriverWithContext
	logger *logger.Logger
	config config.DatabaseConfig
}

// NewNeo4jClient creates a new Neo4j client
func NewNeo4jClient(cfg config.DatabaseConfig, log *logger.Logger) (*Neo4jClient, error) {
	// Configure authentication
	auth := neo4j.BasicAuth(cfg.Username, cfg.Password, "")

	// Configure driver options
	driverConfig := func(conf *neo4jconfig.Config) {
		conf.MaxConnectionPoolSize = cfg.MaxConns
		conf.ConnectionAcquisitionTimeout = 30 * time.Second
		conf.SocketConnectTimeout = 5 * time.Second
		conf.SocketKeepalive = true

		// Configure TLS if using bolt+s:// or neo4j+s://
		if cfg.TLSInsecure {
			log.Info("Configuring Neo4j with TLS InsecureSkipVerify=true (development mode)")
			conf.TlsConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		} else {
			log.Info("Neo4j TLS verification enabled (TLSInsecure=false)")
		}
	}

	// Create driver
	driver, err := neo4j.NewDriverWithContext(cfg.URI, auth, driverConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	client := &Neo4jClient{
		driver: driver,
		logger: log.WithService("neo4j"),
		config: cfg,
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.VerifyConnectivity(ctx); err != nil {
		driver.Close(context.Background())
		return nil, fmt.Errorf("failed to verify Neo4j connectivity: %w", err)
	}

	client.logger.Info("Connected to Neo4j database",
		zap.String("uri", cfg.URI),
		zap.String("database", cfg.Database),
	)

	return client, nil
}

// VerifyConnectivity verifies the connection to Neo4j
func (c *Neo4jClient) VerifyConnectivity(ctx context.Context) error {
	return c.driver.VerifyConnectivity(ctx)
}

// Close closes the Neo4j driver
func (c *Neo4jClient) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

// Session creates a new session
func (c *Neo4jClient) Session(ctx context.Context, options ...func(*neo4j.SessionConfig)) neo4j.SessionWithContext {
	sessionConfig := neo4j.SessionConfig{
		DatabaseName: c.config.Database,
	}

	// Apply any custom options
	for _, option := range options {
		option(&sessionConfig)
	}

	return c.driver.NewSession(ctx, sessionConfig)
}

// ReadTransaction executes a read transaction
func (c *Neo4jClient) ReadTransaction(ctx context.Context, work neo4j.ManagedTransactionWork) (interface{}, error) {
	session := c.Session(ctx)
	defer session.Close(ctx)

	start := time.Now()
	result, err := session.ExecuteRead(ctx, work)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		c.logger.LogDatabaseQuery("read_transaction", duration, err)
		return nil, err
	}

	c.logger.LogDatabaseQuery("read_transaction", duration, nil)
	return result, nil
}

// WriteTransaction executes a write transaction
func (c *Neo4jClient) WriteTransaction(ctx context.Context, work neo4j.ManagedTransactionWork) (interface{}, error) {
	session := c.Session(ctx)
	defer session.Close(ctx)

	start := time.Now()
	result, err := session.ExecuteWrite(ctx, work)
	duration := time.Since(start).Seconds() * 1000

	if err != nil {
		c.logger.LogDatabaseQuery("write_transaction", duration, err)
		return nil, err
	}

	c.logger.LogDatabaseQuery("write_transaction", duration, nil)
	return result, nil
}

// validateNeo4jParameters validates that all parameters are Neo4j-compatible primitive types
func (c *Neo4jClient) validateNeo4jParameters(params map[string]interface{}) error {
	for key, value := range params {
		if err := c.validateNeo4jValue(key, value); err != nil {
			return err
		}
	}
	return nil
}

// validateNeo4jValue checks if a value is a valid Neo4j property type
func (c *Neo4jClient) validateNeo4jValue(key string, value interface{}) error {
	if value == nil {
		return nil
	}

	switch reflect.TypeOf(value).Kind() {
	case reflect.String, reflect.Bool:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return nil
	case reflect.Float32, reflect.Float64:
		return nil
	case reflect.Slice, reflect.Array:
		// Arrays/slices are allowed if they contain primitive types
		return c.validateNeo4jSlice(key, value)
	default:
		// Complex types like maps, structs are not allowed
		return fmt.Errorf("parameter '%s' contains invalid type %T (Neo4j only supports primitive types and arrays thereof)", key, value)
	}
}

// validateNeo4jSlice validates that slice/array elements are primitive types
func (c *Neo4jClient) validateNeo4jSlice(key string, value interface{}) error {
	v := reflect.ValueOf(value)
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i).Interface()
		if elem == nil {
			continue
		}
		
		switch reflect.TypeOf(elem).Kind() {
		case reflect.String, reflect.Bool:
			continue
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			continue
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			continue
		case reflect.Float32, reflect.Float64:
			continue
		default:
			return fmt.Errorf("parameter '%s' contains array element of invalid type %T (Neo4j arrays can only contain primitive types)", key, elem)
		}
	}
	return nil
}

// ExecuteQuery executes a query with parameters
func (c *Neo4jClient) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) (*neo4j.EagerResult, error) {
	// Validate parameters before execution
	if err := c.validateNeo4jParameters(params); err != nil {
		c.logger.Error("Invalid Neo4j parameters", zap.Error(err))
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	start := time.Now()
	result, err := neo4j.ExecuteQuery(ctx, c.driver, query, params,
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(c.config.Database))
	duration := time.Since(start).Seconds() * 1000

	c.logger.LogDatabaseQuery(query, duration, err)

	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	return result, nil
}

// ExecuteQueryWithLogging executes a query and logs performance metrics
func (c *Neo4jClient) ExecuteQueryWithLogging(ctx context.Context, query string, params map[string]interface{}) (*neo4j.EagerResult, error) {
	c.logger.Debug("Executing Neo4j query",
		zap.String("query", query),
		zap.Any("params", params),
	)

	result, err := c.ExecuteQuery(ctx, query, params)

	if err != nil {
		c.logger.Error("Neo4j query failed",
			zap.String("query", query),
			zap.Error(err),
		)
		return nil, err
	}

	c.logger.Debug("Neo4j query completed",
		zap.String("query", query),
		zap.Int("records", len(result.Records)),
	)

	return result, nil
}

// HealthCheck performs a health check on the Neo4j connection
func (c *Neo4jClient) HealthCheck(ctx context.Context) error {
	_, err := c.ExecuteQuery(ctx, "RETURN 1 as health", nil)
	return err
}

// GetDatabaseInfo returns information about the Neo4j database
func (c *Neo4jClient) GetDatabaseInfo(ctx context.Context) (map[string]interface{}, error) {
	result, err := c.ExecuteQuery(ctx, "CALL dbms.components() YIELD name, versions, edition", nil)
	if err != nil {
		return nil, err
	}

	info := make(map[string]interface{})
	for _, record := range result.Records {
		name, _ := record.Get("name")
		versions, _ := record.Get("versions")
		edition, _ := record.Get("edition")

		info[name.(string)] = map[string]interface{}{
			"versions": versions,
			"edition":  edition,
		}
	}

	return info, nil
}

// CreateConstraints creates database constraints
func (c *Neo4jClient) CreateConstraints(ctx context.Context) error {
	constraints := []string{
		// User constraints
		"CREATE CONSTRAINT user_id_unique IF NOT EXISTS FOR (u:User) REQUIRE u.id IS UNIQUE",
		"CREATE CONSTRAINT user_email_unique IF NOT EXISTS FOR (u:User) REQUIRE u.email IS UNIQUE",
		"CREATE CONSTRAINT user_keycloak_id_unique IF NOT EXISTS FOR (u:User) REQUIRE u.keycloak_id IS UNIQUE",

		// Notebook constraints
		"CREATE CONSTRAINT notebook_id_unique IF NOT EXISTS FOR (n:Notebook) REQUIRE n.id IS UNIQUE",

		// Document constraints
		"CREATE CONSTRAINT document_id_unique IF NOT EXISTS FOR (d:Document) REQUIRE d.id IS UNIQUE",

		// Entity constraints
		"CREATE CONSTRAINT entity_id_unique IF NOT EXISTS FOR (e:Entity) REQUIRE e.id IS UNIQUE",

		// Processing job constraints
		"CREATE CONSTRAINT job_id_unique IF NOT EXISTS FOR (j:ProcessingJob) REQUIRE j.id IS UNIQUE",
	}

	for _, constraint := range constraints {
		if _, err := c.ExecuteQuery(ctx, constraint, nil); err != nil {
			c.logger.Warn("Failed to create constraint",
				zap.String("constraint", constraint),
				zap.Error(err),
			)
			// Continue with other constraints even if one fails
		}
	}

	c.logger.Info("Database constraints created/verified")
	return nil
}

// CreateIndexes creates database indexes for performance
func (c *Neo4jClient) CreateIndexes(ctx context.Context) error {
	indexes := []string{
		// User indexes
		"CREATE INDEX user_email_idx IF NOT EXISTS FOR (u:User) ON (u.email)",
		"CREATE INDEX user_status_idx IF NOT EXISTS FOR (u:User) ON (u.status)",
		"CREATE INDEX user_created_at_idx IF NOT EXISTS FOR (u:User) ON (u.created_at)",

		// Notebook indexes
		"CREATE INDEX notebook_name_idx IF NOT EXISTS FOR (n:Notebook) ON (n.name)",
		"CREATE INDEX notebook_status_idx IF NOT EXISTS FOR (n:Notebook) ON (n.status)",
		"CREATE INDEX notebook_visibility_idx IF NOT EXISTS FOR (n:Notebook) ON (n.visibility)",
		"CREATE INDEX notebook_created_at_idx IF NOT EXISTS FOR (n:Notebook) ON (n.created_at)",

		// Document indexes
		"CREATE INDEX document_name_idx IF NOT EXISTS FOR (d:Document) ON (d.name)",
		"CREATE INDEX document_type_idx IF NOT EXISTS FOR (d:Document) ON (d.type)",
		"CREATE INDEX document_status_idx IF NOT EXISTS FOR (d:Document) ON (d.status)",
		"CREATE INDEX document_created_at_idx IF NOT EXISTS FOR (d:Document) ON (d.created_at)",

		// Full-text search indexes
		"CREATE FULLTEXT INDEX document_content_fulltext IF NOT EXISTS FOR (d:Document) ON EACH [d.content, d.extracted_text]",
		"CREATE FULLTEXT INDEX notebook_search_fulltext IF NOT EXISTS FOR (n:Notebook) ON EACH [n.name, n.description, n.search_text]",
	}

	for _, index := range indexes {
		if _, err := c.ExecuteQuery(ctx, index, nil); err != nil {
			c.logger.Warn("Failed to create index",
				zap.String("index", index),
				zap.Error(err),
			)
			// Continue with other indexes even if one fails
		}
	}

	c.logger.Info("Database indexes created/verified")
	return nil
}
