package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DatabaseType represents supported database types
type DatabaseType string

const (
	DatabaseTypePostgres  DatabaseType = "postgres"
	DatabaseTypeMySQL     DatabaseType = "mysql"
	DatabaseTypeMariaDB   DatabaseType = "mariadb"
	DatabaseTypeSQLServer DatabaseType = "sqlserver"
	DatabaseTypeSQLite    DatabaseType = "sqlite"
)

// DatabaseStatus represents the connection status
type DatabaseStatus string

const (
	DatabaseStatusPending   DatabaseStatus = "Pending"
	DatabaseStatusConnected DatabaseStatus = "Connected"
	DatabaseStatusFailed    DatabaseStatus = "Failed"
	DatabaseStatusDegraded  DatabaseStatus = "Degraded"
)

// Database represents a database connection configuration
type Database struct {
	// Core identity
	ID   string `json:"id"`
	Name string `json:"name"`

	// Multi-tenancy
	TenantID string `json:"tenant_id"`
	SpaceID  string `json:"space_id"`
	OwnerID  string `json:"owner_id"`

	// Connection details
	Type     DatabaseType `json:"type"`
	Host     string       `json:"host"`
	Port     int          `json:"port"`
	Database string       `json:"database"`
	SSLMode  string       `json:"ssl_mode,omitempty"`

	// Kubernetes references
	SecretName      string `json:"secret_name"`      // K8s Secret with credentials
	SecretNamespace string `json:"secret_namespace"`
	CRDName         string `json:"crd_name"`         // Database CR name
	CRDNamespace    string `json:"crd_namespace"`

	// Policy
	ReadOnly          bool `json:"readonly"`
	MaxRows           int  `json:"max_rows"`
	ConnectionTimeout int  `json:"connection_timeout"`
	QueryTimeout      int  `json:"query_timeout"`

	// Status (synced from K8s)
	Status        DatabaseStatus `json:"status"`
	StatusMessage string         `json:"status_message,omitempty"`
	LastChecked   *time.Time     `json:"last_checked,omitempty"`

	// Metadata
	Labels      map[string]string `json:"labels,omitempty"`
	Description string            `json:"description,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// DatabaseCreateRequest represents the request to create a database connection
type DatabaseCreateRequest struct {
	Name        string            `json:"name" validate:"required,min=1,max=255"`
	Type        DatabaseType      `json:"type" validate:"required,oneof=postgres mysql mariadb sqlserver sqlite"`
	Host        string            `json:"host" validate:"required"`
	Port        int               `json:"port" validate:"required,min=1,max=65535"`
	Database    string            `json:"database" validate:"required"`
	Username    string            `json:"username" validate:"required"`
	Password    string            `json:"password" validate:"required"`
	SSLMode     string            `json:"ssl_mode,omitempty" validate:"omitempty,oneof=disable allow prefer require verify-ca verify-full"`
	ReadOnly    bool              `json:"readonly"`
	MaxRows     int               `json:"max_rows,omitempty" validate:"omitempty,min=1,max=100000"`
	Labels      map[string]string `json:"labels,omitempty"`
	Description string            `json:"description,omitempty" validate:"omitempty,max=1000"`
}

// DatabaseUpdateRequest represents the request to update a database connection
type DatabaseUpdateRequest struct {
	Name        *string           `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Host        *string           `json:"host,omitempty"`
	Port        *int              `json:"port,omitempty" validate:"omitempty,min=1,max=65535"`
	Database    *string           `json:"database,omitempty"`
	Username    *string           `json:"username,omitempty"`
	Password    *string           `json:"password,omitempty"`
	SSLMode     *string           `json:"ssl_mode,omitempty" validate:"omitempty,oneof=disable allow prefer require verify-ca verify-full"`
	ReadOnly    *bool             `json:"readonly,omitempty"`
	MaxRows     *int              `json:"max_rows,omitempty" validate:"omitempty,min=1,max=100000"`
	Labels      map[string]string `json:"labels,omitempty"`
	Description *string           `json:"description,omitempty" validate:"omitempty,max=1000"`
}

// DatabaseResponse represents the API response for a database
type DatabaseResponse struct {
	Database
}

// DatabaseListResponse represents a paginated list of databases
type DatabaseListResponse struct {
	Databases []Database `json:"databases"`
	Total     int        `json:"total"`
	Page      int        `json:"page"`
	PageSize  int        `json:"page_size"`
}

// QueryRequest represents a SQL query execution request
type QueryRequest struct {
	DatabaseID string `json:"database_id" validate:"required,uuid"`
	Query      string `json:"query" validate:"required,min=1,max=100000"`
	Parameters []any  `json:"parameters,omitempty"`
}

// QueryResponse represents the SQL query execution result
type QueryResponse struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"row_count"`
	Truncated bool             `json:"truncated"`
	Duration  int64            `json:"duration_ms"`
}

// SchemaResponse represents database schema information
type SchemaResponse struct {
	Databases []string    `json:"databases,omitempty"`
	Schemas   []string    `json:"schemas,omitempty"`
	Tables    []TableInfo `json:"tables,omitempty"`
}

// TableInfo represents information about a database table
type TableInfo struct {
	Name     string       `json:"name"`
	Schema   string       `json:"schema,omitempty"`
	RowCount int64        `json:"row_count,omitempty"`
	Columns  []ColumnInfo `json:"columns,omitempty"`
}

// ColumnInfo represents information about a table column
type ColumnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primary_key"`
	Default    string `json:"default,omitempty"`
}

// NewDatabase creates a new Database from a create request
func NewDatabase(req DatabaseCreateRequest, userID, tenantID, spaceID string) *Database {
	now := time.Now()
	dbID := uuid.New().String()

	maxRows := req.MaxRows
	if maxRows == 0 {
		maxRows = 1000 // default
	}

	return &Database{
		ID:                dbID,
		Name:              req.Name,
		TenantID:          tenantID,
		SpaceID:           spaceID,
		OwnerID:           userID,
		Type:              req.Type,
		Host:              req.Host,
		Port:              req.Port,
		Database:          req.Database,
		SSLMode:           req.SSLMode,
		ReadOnly:          req.ReadOnly,
		MaxRows:           maxRows,
		ConnectionTimeout: 30, // default
		QueryTimeout:      60, // default
		Status:            DatabaseStatusPending,
		Labels:            req.Labels,
		Description:       req.Description,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// ToResponse converts a Database to DatabaseResponse
func (d *Database) ToResponse() *DatabaseResponse {
	return &DatabaseResponse{
		Database: *d,
	}
}

// Update applies updates from a DatabaseUpdateRequest
func (d *Database) Update(req DatabaseUpdateRequest) {
	if req.Name != nil {
		d.Name = *req.Name
	}
	if req.Host != nil {
		d.Host = *req.Host
	}
	if req.Port != nil {
		d.Port = *req.Port
	}
	if req.Database != nil {
		d.Database = *req.Database
	}
	if req.SSLMode != nil {
		d.SSLMode = *req.SSLMode
	}
	if req.ReadOnly != nil {
		d.ReadOnly = *req.ReadOnly
	}
	if req.MaxRows != nil {
		d.MaxRows = *req.MaxRows
	}
	if req.Description != nil {
		d.Description = *req.Description
	}
	if req.Labels != nil {
		d.Labels = req.Labels
	}
	d.UpdatedAt = time.Now()
}

// SetKubernetesRefs sets the Kubernetes resource references
func (d *Database) SetKubernetesRefs(secretName, secretNamespace, crdName, crdNamespace string) {
	d.SecretName = secretName
	d.SecretNamespace = secretNamespace
	d.CRDName = crdName
	d.CRDNamespace = crdNamespace
}

// UpdateStatus updates the database connection status
func (d *Database) UpdateStatus(status DatabaseStatus, message string) {
	d.Status = status
	d.StatusMessage = message
	now := time.Now()
	d.LastChecked = &now
	d.UpdatedAt = now
}

// IsConnected returns true if the database is connected
func (d *Database) IsConnected() bool {
	return d.Status == DatabaseStatusConnected
}

// IsFailed returns true if the database connection failed
func (d *Database) IsFailed() bool {
	return d.Status == DatabaseStatusFailed
}

// GetLabelsJSON returns labels as a JSON string for storage
func (d *Database) GetLabelsJSON() string {
	if d.Labels == nil {
		return "{}"
	}
	data, _ := json.Marshal(d.Labels)
	return string(data)
}

// SetLabelsFromJSON parses labels from a JSON string
func (d *Database) SetLabelsFromJSON(jsonStr string) {
	if jsonStr == "" || jsonStr == "{}" {
		d.Labels = nil
		return
	}
	json.Unmarshal([]byte(jsonStr), &d.Labels)
}

// GetDefaultPort returns the default port for a database type
func GetDefaultPort(dbType DatabaseType) int {
	switch dbType {
	case DatabaseTypePostgres:
		return 5432
	case DatabaseTypeMySQL, DatabaseTypeMariaDB:
		return 3306
	case DatabaseTypeSQLServer:
		return 1433
	case DatabaseTypeSQLite:
		return 0 // SQLite doesn't use ports
	default:
		return 0
	}
}

// GetDatabaseTypeIcon returns an emoji icon for the database type
func GetDatabaseTypeIcon(dbType DatabaseType) string {
	switch dbType {
	case DatabaseTypePostgres:
		return "🐘"
	case DatabaseTypeMySQL:
		return "🐬"
	case DatabaseTypeMariaDB:
		return "🦭"
	case DatabaseTypeSQLServer:
		return "🔷"
	case DatabaseTypeSQLite:
		return "📁"
	default:
		return "🗄️"
	}
}
