package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// DatabaseService handles database connection management
type DatabaseService struct {
	neo4j        *database.Neo4jClient
	dbhub        *DBHubService
	neo4jQuery   *Neo4jQueryService
	logger       *logger.Logger
	crdEnabled   bool   // Whether Kubernetes CRD management is enabled
	crdNamespace string // Namespace for Database CRDs and Secrets
}

// NewDatabaseService creates a new database service
func NewDatabaseService(neo4j *database.Neo4jClient, dbhub *DBHubService, neo4jQuery *Neo4jQueryService, log *logger.Logger) *DatabaseService {
	crdEnabled := neo4jQuery != nil && neo4jQuery.k8s != nil
	return &DatabaseService{
		neo4j:        neo4j,
		dbhub:        dbhub,
		neo4jQuery:   neo4jQuery,
		logger:       log.WithService("database_service"),
		crdEnabled:   crdEnabled,
		crdNamespace: "tas-mcp-servers",
	}
}

// CreateDatabase creates a new database connection configuration
func (s *DatabaseService) CreateDatabase(ctx context.Context, userID, tenantID, spaceID string, req models.DatabaseCreateRequest) (*models.Database, error) {
	s.logger.Info("Creating database connection",
		zap.String("user_id", userID),
		zap.String("tenant_id", tenantID),
		zap.String("space_id", spaceID),
		zap.String("name", req.Name),
		zap.String("type", string(req.Type)),
	)

	// Create database model
	db := models.NewDatabase(req, userID, tenantID, spaceID)

	// Generate CRD name and K8s Secret name
	db.CRDName = fmt.Sprintf("db-%s", db.ID[:8])
	db.CRDNamespace = s.crdNamespace
	db.SecretName = fmt.Sprintf("db-credentials-%s", db.ID[:8])
	db.SecretNamespace = s.crdNamespace

	// Create K8s Secret with credentials.
	// For neo4j type, always create the secret (Neo4jQueryService reads credentials from K8s Secrets).
	// For other types, only create if CRD management is enabled.
	shouldCreateSecret := (s.crdEnabled || db.Type == models.DatabaseTypeNeo4j) && s.neo4jQuery != nil && req.Username != ""
	if shouldCreateSecret {
		if err := s.neo4jQuery.CreateSecret(ctx, db.SecretName, db.SecretNamespace, req.Username, req.Password); err != nil {
			s.logger.Warn("Failed to create K8s secret for database (non-fatal)",
				zap.String("database_id", db.ID),
				zap.Error(err),
			)
		} else {
			s.logger.Info("K8s secret created for database",
				zap.String("secret", db.SecretName),
				zap.String("namespace", db.SecretNamespace),
			)
		}
	}

	// Create the Database node in Neo4j
	createQuery := `
		CREATE (d:Database {
			id: $id,
			name: $name,
			tenant_id: $tenant_id,
			space_id: $space_id,
			owner_id: $owner_id,
			type: $type,
			host: $host,
			port: $port,
			database: $database,
			ssl_mode: $ssl_mode,
			protocol: $protocol,
			secret_name: $secret_name,
			secret_namespace: $secret_namespace,
			crd_name: $crd_name,
			crd_namespace: $crd_namespace,
			readonly: $readonly,
			max_rows: $max_rows,
			connection_timeout: $connection_timeout,
			query_timeout: $query_timeout,
			status: $status,
			status_message: $status_message,
			labels: $labels,
			description: $description,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN d.id
	`

	params := map[string]any{
		"id":                 db.ID,
		"name":               db.Name,
		"tenant_id":          db.TenantID,
		"space_id":           db.SpaceID,
		"owner_id":           db.OwnerID,
		"type":               string(db.Type),
		"host":               db.Host,
		"port":               db.Port,
		"database":           db.Database,
		"ssl_mode":           db.SSLMode,
		"protocol":           db.Protocol,
		"secret_name":        db.SecretName,
		"secret_namespace":   db.SecretNamespace,
		"crd_name":           db.CRDName,
		"crd_namespace":      db.CRDNamespace,
		"readonly":           db.ReadOnly,
		"max_rows":           db.MaxRows,
		"connection_timeout": db.ConnectionTimeout,
		"query_timeout":      db.QueryTimeout,
		"status":             string(db.Status),
		"status_message":     db.StatusMessage,
		"labels":             db.GetLabelsJSON(),
		"description":        db.Description,
		"created_at":         db.CreatedAt.Format(time.RFC3339),
		"updated_at":         db.UpdatedAt.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, createQuery, params)
	if err != nil {
		s.logger.Error("Failed to create database node",
			zap.String("database_id", db.ID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to create database", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.Internal("Failed to create database: no record returned")
	}

	// Link to space
	linkQuery := `
		MATCH (d:Database {id: $db_id})
		MATCH (sp:Space {id: $space_id})
		CREATE (d)-[:BELONGS_TO {created_at: datetime()}]->(sp)
	`
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, linkQuery, map[string]any{
		"db_id":    db.ID,
		"space_id": spaceID,
	})
	if err != nil {
		s.logger.Warn("Failed to link database to space (space may not exist)",
			zap.String("database_id", db.ID),
			zap.String("space_id", spaceID),
			zap.Error(err),
		)
	}

	// Link to owner
	ownerQuery := `
		MATCH (d:Database {id: $db_id})
		MATCH (u:User {id: $user_id})
		CREATE (u)-[:OWNS {created_at: datetime()}]->(d)
	`
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, ownerQuery, map[string]any{
		"db_id":   db.ID,
		"user_id": userID,
	})
	if err != nil {
		s.logger.Warn("Failed to link database to owner",
			zap.String("database_id", db.ID),
			zap.String("user_id", userID),
			zap.Error(err),
		)
	}

	s.logger.Info("Database connection created",
		zap.String("database_id", db.ID),
		zap.String("name", db.Name),
		zap.String("type", string(db.Type)),
	)

	return db, nil
}

// GetDatabase retrieves a database connection by ID
func (s *DatabaseService) GetDatabase(ctx context.Context, databaseID, tenantID string) (*models.Database, error) {
	s.logger.Debug("Getting database by ID",
		zap.String("database_id", databaseID),
		zap.String("tenant_id", tenantID),
	)

	query := `
		MATCH (d:Database {id: $db_id})
		WHERE d.tenant_id = $tenant_id
		RETURN d.id, d.name, d.tenant_id, d.space_id, d.owner_id,
		       d.type, d.host, d.port, d.database, d.ssl_mode, d.protocol,
		       d.secret_name, d.secret_namespace, d.crd_name, d.crd_namespace,
		       d.readonly, d.max_rows, d.connection_timeout, d.query_timeout,
		       d.status, d.status_message, d.last_checked,
		       d.labels, d.description, d.created_at, d.updated_at
	`

	params := map[string]any{
		"db_id":     databaseID,
		"tenant_id": tenantID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get database",
			zap.String("database_id", databaseID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to retrieve database", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Database not found", map[string]any{
			"database_id": databaseID,
		})
	}

	return s.recordToDatabase(result.Records[0])
}

// ListDatabases lists all databases for a tenant/space with pagination
func (s *DatabaseService) ListDatabases(ctx context.Context, tenantID, spaceID string, page, pageSize int) (*models.DatabaseListResponse, error) {
	s.logger.Debug("Listing databases",
		zap.String("tenant_id", tenantID),
		zap.String("space_id", spaceID),
		zap.Int("page", page),
		zap.Int("page_size", pageSize),
	)

	// Build query based on whether space_id is provided
	var query string
	params := map[string]any{
		"tenant_id": tenantID,
		"skip":      (page - 1) * pageSize,
		"limit":     pageSize,
	}

	if spaceID != "" {
		query = `
			MATCH (d:Database)
			WHERE d.tenant_id = $tenant_id AND d.space_id = $space_id
			RETURN d.id, d.name, d.tenant_id, d.space_id, d.owner_id,
			       d.type, d.host, d.port, d.database, d.ssl_mode, d.protocol,
			       d.secret_name, d.secret_namespace, d.crd_name, d.crd_namespace,
			       d.readonly, d.max_rows, d.connection_timeout, d.query_timeout,
			       d.status, d.status_message, d.last_checked,
			       d.labels, d.description, d.created_at, d.updated_at
			ORDER BY d.created_at DESC
			SKIP $skip LIMIT $limit
		`
		params["space_id"] = spaceID
	} else {
		query = `
			MATCH (d:Database)
			WHERE d.tenant_id = $tenant_id
			RETURN d.id, d.name, d.tenant_id, d.space_id, d.owner_id,
			       d.type, d.host, d.port, d.database, d.ssl_mode, d.protocol,
			       d.secret_name, d.secret_namespace, d.crd_name, d.crd_namespace,
			       d.readonly, d.max_rows, d.connection_timeout, d.query_timeout,
			       d.status, d.status_message, d.last_checked,
			       d.labels, d.description, d.created_at, d.updated_at
			ORDER BY d.created_at DESC
			SKIP $skip LIMIT $limit
		`
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to list databases",
			zap.String("tenant_id", tenantID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to list databases", err)
	}

	databases := make([]models.DatabaseResponse, 0, len(result.Records))
	for _, record := range result.Records {
		db, err := s.recordToDatabase(record)
		if err != nil {
			s.logger.Warn("Failed to parse database record", zap.Error(err))
			continue
		}
		resp := s.enrichResponse(ctx, db)
		databases = append(databases, *resp)
	}

	// Get total count
	var countQuery string
	if spaceID != "" {
		countQuery = `
			MATCH (d:Database)
			WHERE d.tenant_id = $tenant_id AND d.space_id = $space_id
			RETURN count(d) as total
		`
	} else {
		countQuery = `
			MATCH (d:Database)
			WHERE d.tenant_id = $tenant_id
			RETURN count(d) as total
		`
	}

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, params)
	if err != nil {
		s.logger.Warn("Failed to get database count", zap.Error(err))
	}

	total := 0
	if len(countResult.Records) > 0 {
		if t, ok := countResult.Records[0].Values[0].(int64); ok {
			total = int(t)
		}
	}

	return &models.DatabaseListResponse{
		Databases: databases,
		Total:     total,
		Page:      page,
		PageSize:  pageSize,
	}, nil
}

// UpdateDatabase updates a database connection configuration
func (s *DatabaseService) UpdateDatabase(ctx context.Context, databaseID, tenantID string, req models.DatabaseUpdateRequest) (*models.Database, error) {
	s.logger.Info("Updating database",
		zap.String("database_id", databaseID),
		zap.String("tenant_id", tenantID),
	)

	// Get existing database
	db, err := s.GetDatabase(ctx, databaseID, tenantID)
	if err != nil {
		return nil, err
	}

	// Apply updates
	db.Update(req)

	// Update K8s Secret if credentials changed for neo4j type
	if (req.Username != nil || req.Password != nil) && db.Type == models.DatabaseTypeNeo4j && s.neo4jQuery != nil && db.SecretName != "" {
		username := ""
		password := ""
		if req.Username != nil {
			username = *req.Username
		}
		if req.Password != nil {
			password = *req.Password
		}
		// Only update if at least one credential field is provided
		if username != "" || password != "" {
			if err := s.neo4jQuery.UpdateSecret(ctx, db.SecretName, db.SecretNamespace, username, password); err != nil {
				s.logger.Warn("Failed to update K8s secret for database (non-fatal)",
					zap.String("database_id", databaseID),
					zap.Error(err),
				)
			}
		}
	}

	// Update in Neo4j
	updateQuery := `
		MATCH (d:Database {id: $db_id})
		WHERE d.tenant_id = $tenant_id
		SET d.name = $name,
		    d.host = $host,
		    d.port = $port,
		    d.database = $database,
		    d.ssl_mode = $ssl_mode,
		    d.protocol = $protocol,
		    d.readonly = $readonly,
		    d.max_rows = $max_rows,
		    d.labels = $labels,
		    d.description = $description,
		    d.updated_at = datetime($updated_at)
		RETURN d.id
	`

	params := map[string]any{
		"db_id":       databaseID,
		"tenant_id":   tenantID,
		"name":        db.Name,
		"host":        db.Host,
		"port":        db.Port,
		"database":    db.Database,
		"ssl_mode":    db.SSLMode,
		"protocol":    db.Protocol,
		"readonly":    db.ReadOnly,
		"max_rows":    db.MaxRows,
		"labels":      db.GetLabelsJSON(),
		"description": db.Description,
		"updated_at":  db.UpdatedAt.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, updateQuery, params)
	if err != nil {
		s.logger.Error("Failed to update database",
			zap.String("database_id", databaseID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to update database", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Database not found", map[string]any{
			"database_id": databaseID,
		})
	}

	s.logger.Info("Database updated",
		zap.String("database_id", databaseID),
	)

	return db, nil
}

// DeleteDatabase deletes a database connection configuration
func (s *DatabaseService) DeleteDatabase(ctx context.Context, databaseID, tenantID string) error {
	s.logger.Info("Deleting database",
		zap.String("database_id", databaseID),
		zap.String("tenant_id", tenantID),
	)

	// Clean up K8s Secret if CRD management is enabled or for neo4j type
	if s.neo4jQuery != nil {
		db, err := s.GetDatabase(ctx, databaseID, tenantID)
		if err == nil && db.SecretName != "" {
			if err := s.neo4jQuery.DeleteSecret(ctx, db.SecretName, db.SecretNamespace); err != nil {
				s.logger.Warn("Failed to delete K8s secret (non-fatal)",
					zap.String("secret", db.SecretName),
					zap.Error(err),
				)
			}
		}
	}

	// Delete all relationships and the node
	deleteQuery := `
		MATCH (d:Database {id: $db_id})
		WHERE d.tenant_id = $tenant_id
		DETACH DELETE d
		RETURN count(d) as deleted
	`

	params := map[string]any{
		"db_id":     databaseID,
		"tenant_id": tenantID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, deleteQuery, params)
	if err != nil {
		s.logger.Error("Failed to delete database",
			zap.String("database_id", databaseID),
			zap.Error(err),
		)
		return errors.Database("Failed to delete database", err)
	}

	if len(result.Records) == 0 {
		return errors.NotFoundWithDetails("Database not found", map[string]any{
			"database_id": databaseID,
		})
	}

	s.logger.Info("Database deleted",
		zap.String("database_id", databaseID),
	)

	return nil
}

// TestConnection tests a database connection via DBHub
func (s *DatabaseService) TestConnection(ctx context.Context, databaseID, tenantID string) error {
	s.logger.Info("Testing database connection",
		zap.String("database_id", databaseID),
	)

	db, err := s.GetDatabase(ctx, databaseID, tenantID)
	if err != nil {
		return err
	}

	// Route to appropriate service
	var testErr error
	if db.Type == models.DatabaseTypeNeo4j && s.neo4jQuery != nil {
		testErr = s.neo4jQuery.TestConnection(ctx, db)
	} else {
		testErr = s.dbhub.TestConnection(ctx, db)
	}

	if testErr != nil {
		s.updateStatus(ctx, databaseID, tenantID, models.DatabaseStatusFailed, testErr.Error())
		return testErr
	}

	s.updateStatus(ctx, databaseID, tenantID, models.DatabaseStatusConnected, "Connection successful")
	return nil
}

// ExecuteQuery executes a SQL query on a database
func (s *DatabaseService) ExecuteQuery(ctx context.Context, databaseID, tenantID string, query string, params []any) (*models.QueryResponse, error) {
	s.logger.Info("Executing query",
		zap.String("database_id", databaseID),
		zap.String("query_preview", truncateQuery(query, 50)),
	)

	db, err := s.GetDatabase(ctx, databaseID, tenantID)
	if err != nil {
		return nil, err
	}

	// Check if database allows queries (not pending)
	if db.Status == models.DatabaseStatusPending {
		return nil, errors.ValidationWithDetails("Database connection not yet established", map[string]any{
			"database_id": databaseID,
			"status":      string(db.Status),
		})
	}

	// Route to appropriate service
	if db.Type == models.DatabaseTypeNeo4j && s.neo4jQuery != nil {
		return s.neo4jQuery.ExecuteQuery(ctx, db, query, params)
	}

	return s.dbhub.ExecuteQuery(ctx, db, query, params)
}

// GetSchema retrieves schema information for a database
func (s *DatabaseService) GetSchema(ctx context.Context, databaseID, tenantID, schemaType string) (*models.SchemaResponse, error) {
	s.logger.Info("Getting schema",
		zap.String("database_id", databaseID),
		zap.String("schema_type", schemaType),
	)

	db, err := s.GetDatabase(ctx, databaseID, tenantID)
	if err != nil {
		return nil, err
	}

	if db.Type == models.DatabaseTypeNeo4j && s.neo4jQuery != nil {
		return s.neo4jQuery.GetSchema(ctx, db)
	}

	return s.dbhub.GetSchema(ctx, db, schemaType)
}

// GetTables retrieves table list for a database
func (s *DatabaseService) GetTables(ctx context.Context, databaseID, tenantID string) ([]models.TableInfo, error) {
	db, err := s.GetDatabase(ctx, databaseID, tenantID)
	if err != nil {
		return nil, err
	}

	if db.Type == models.DatabaseTypeNeo4j && s.neo4jQuery != nil {
		return s.neo4jQuery.GetTables(ctx, db)
	}

	return s.dbhub.GetTables(ctx, db)
}

// GetTableColumns retrieves column information for a table
func (s *DatabaseService) GetTableColumns(ctx context.Context, databaseID, tenantID, tableName string) ([]models.ColumnInfo, error) {
	db, err := s.GetDatabase(ctx, databaseID, tenantID)
	if err != nil {
		return nil, err
	}

	if db.Type == models.DatabaseTypeNeo4j && s.neo4jQuery != nil {
		return s.neo4jQuery.GetTableColumns(ctx, db, tableName)
	}

	return s.dbhub.GetTableColumns(ctx, db, tableName)
}

// enrichResponse populates a DatabaseResponse with credential info from K8s Secrets.
func (s *DatabaseService) enrichResponse(ctx context.Context, db *models.Database) *models.DatabaseResponse {
	resp := db.ToResponse()
	if db.SecretName != "" && s.neo4jQuery != nil {
		username, password, err := s.neo4jQuery.getCredentials(ctx, db)
		if err == nil {
			resp.Username = username
			resp.HasPassword = password != ""
		} else {
			s.logger.Debug("Could not read credentials for database",
				zap.String("database_id", db.ID),
				zap.Error(err),
			)
		}
	}
	return resp
}

// EnrichResponse is the exported version for use by handlers.
func (s *DatabaseService) EnrichResponse(ctx context.Context, db *models.Database) *models.DatabaseResponse {
	return s.enrichResponse(ctx, db)
}

// updateStatus updates the status of a database connection
func (s *DatabaseService) updateStatus(ctx context.Context, databaseID, tenantID string, status models.DatabaseStatus, message string) {
	query := `
		MATCH (d:Database {id: $db_id})
		WHERE d.tenant_id = $tenant_id
		SET d.status = $status,
		    d.status_message = $message,
		    d.last_checked = datetime(),
		    d.updated_at = datetime()
	`

	params := map[string]any{
		"db_id":     databaseID,
		"tenant_id": tenantID,
		"status":    string(status),
		"message":   message,
	}

	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Warn("Failed to update database status",
			zap.String("database_id", databaseID),
			zap.Error(err),
		)
	}
}

// recordToDatabase converts a Neo4j record to a Database model
func (s *DatabaseService) recordToDatabase(record any) (*models.Database, error) {
	// Get the record interface
	r, ok := record.(interface {
		Values() []any
		Keys() []string
	})
	if !ok {
		// Try alternative interface
		type recordWithGet interface {
			Get(key string) (any, bool)
		}
		if rg, ok := record.(recordWithGet); ok {
			return s.recordToDatabase2(rg)
		}
		return nil, fmt.Errorf("invalid record type: %T", record)
	}

	keys := r.Keys()
	values := r.Values()

	db := &models.Database{}
	for i, key := range keys {
		if i >= len(values) {
			break
		}
		val := values[i]
		if val == nil {
			continue
		}

		switch key {
		case "d.id":
			db.ID = toString(val)
		case "d.name":
			db.Name = toString(val)
		case "d.tenant_id":
			db.TenantID = toString(val)
		case "d.space_id":
			db.SpaceID = toString(val)
		case "d.owner_id":
			db.OwnerID = toString(val)
		case "d.type":
			db.Type = models.DatabaseType(toString(val))
		case "d.host":
			db.Host = toString(val)
		case "d.port":
			db.Port = toInt(val)
		case "d.database":
			db.Database = toString(val)
		case "d.ssl_mode":
			db.SSLMode = toString(val)
		case "d.protocol":
			db.Protocol = toString(val)
		case "d.secret_name":
			db.SecretName = toString(val)
		case "d.secret_namespace":
			db.SecretNamespace = toString(val)
		case "d.crd_name":
			db.CRDName = toString(val)
		case "d.crd_namespace":
			db.CRDNamespace = toString(val)
		case "d.readonly":
			db.ReadOnly = toBool(val)
		case "d.max_rows":
			db.MaxRows = toInt(val)
		case "d.connection_timeout":
			db.ConnectionTimeout = toInt(val)
		case "d.query_timeout":
			db.QueryTimeout = toInt(val)
		case "d.status":
			db.Status = models.DatabaseStatus(toString(val))
		case "d.status_message":
			db.StatusMessage = toString(val)
		case "d.last_checked":
			if t := toTime(val); t != nil {
				db.LastChecked = t
			}
		case "d.labels":
			db.SetLabelsFromJSON(toString(val))
		case "d.description":
			db.Description = toString(val)
		case "d.created_at":
			if t := toTime(val); t != nil {
				db.CreatedAt = *t
			}
		case "d.updated_at":
			if t := toTime(val); t != nil {
				db.UpdatedAt = *t
			}
		}
	}

	return db, nil
}

// recordToDatabase2 handles records with Get method
func (s *DatabaseService) recordToDatabase2(r interface{ Get(key string) (any, bool) }) (*models.Database, error) {
	db := &models.Database{
		ID: uuid.New().String(), // fallback
	}

	if v, ok := r.Get("d.id"); ok && v != nil {
		db.ID = toString(v)
	}
	if v, ok := r.Get("d.name"); ok && v != nil {
		db.Name = toString(v)
	}
	if v, ok := r.Get("d.tenant_id"); ok && v != nil {
		db.TenantID = toString(v)
	}
	if v, ok := r.Get("d.space_id"); ok && v != nil {
		db.SpaceID = toString(v)
	}
	if v, ok := r.Get("d.owner_id"); ok && v != nil {
		db.OwnerID = toString(v)
	}
	if v, ok := r.Get("d.type"); ok && v != nil {
		db.Type = models.DatabaseType(toString(v))
	}
	if v, ok := r.Get("d.host"); ok && v != nil {
		db.Host = toString(v)
	}
	if v, ok := r.Get("d.port"); ok && v != nil {
		db.Port = toInt(v)
	}
	if v, ok := r.Get("d.database"); ok && v != nil {
		db.Database = toString(v)
	}
	if v, ok := r.Get("d.ssl_mode"); ok && v != nil {
		db.SSLMode = toString(v)
	}
	if v, ok := r.Get("d.protocol"); ok && v != nil {
		db.Protocol = toString(v)
	}
	if v, ok := r.Get("d.secret_name"); ok && v != nil {
		db.SecretName = toString(v)
	}
	if v, ok := r.Get("d.secret_namespace"); ok && v != nil {
		db.SecretNamespace = toString(v)
	}
	if v, ok := r.Get("d.crd_name"); ok && v != nil {
		db.CRDName = toString(v)
	}
	if v, ok := r.Get("d.crd_namespace"); ok && v != nil {
		db.CRDNamespace = toString(v)
	}
	if v, ok := r.Get("d.readonly"); ok && v != nil {
		db.ReadOnly = toBool(v)
	}
	if v, ok := r.Get("d.max_rows"); ok && v != nil {
		db.MaxRows = toInt(v)
	}
	if v, ok := r.Get("d.connection_timeout"); ok && v != nil {
		db.ConnectionTimeout = toInt(v)
	}
	if v, ok := r.Get("d.query_timeout"); ok && v != nil {
		db.QueryTimeout = toInt(v)
	}
	if v, ok := r.Get("d.status"); ok && v != nil {
		db.Status = models.DatabaseStatus(toString(v))
	}
	if v, ok := r.Get("d.status_message"); ok && v != nil {
		db.StatusMessage = toString(v)
	}
	if v, ok := r.Get("d.last_checked"); ok && v != nil {
		db.LastChecked = toTime(v)
	}
	if v, ok := r.Get("d.labels"); ok && v != nil {
		db.SetLabelsFromJSON(toString(v))
	}
	if v, ok := r.Get("d.description"); ok && v != nil {
		db.Description = toString(v)
	}
	if v, ok := r.Get("d.created_at"); ok && v != nil {
		if t := toTime(v); t != nil {
			db.CreatedAt = *t
		}
	}
	if v, ok := r.Get("d.updated_at"); ok && v != nil {
		if t := toTime(v); t != nil {
			db.UpdatedAt = *t
		}
	}

	return db, nil
}

// Helper functions for type conversion
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func toInt(v any) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}

func toBool(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func toTime(v any) *time.Time {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case time.Time:
		return &val
	case string:
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			return nil
		}
		return &t
	default:
		// Neo4j LocalDateTime
		if dt, ok := v.(interface{ Time() time.Time }); ok {
			t := dt.Time()
			return &t
		}
		return nil
	}
}
