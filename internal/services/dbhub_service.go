package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
	"go.uber.org/zap"
)

// DBHubService handles communication with DBHub MCP server
type DBHubService struct {
	baseURL string
	enabled bool
	client  *http.Client
	logger  *zap.Logger
}

// NewDBHubService creates a new DBHub service client
func NewDBHubService(cfg *config.DBHubConfig, log *logger.Logger) *DBHubService {
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}

	return &DBHubService{
		baseURL: cfg.BaseURL,
		enabled: cfg.Enabled,
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
		logger: log.Logger,
	}
}

// MCPRequest represents an MCP tool call request
type MCPRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      int                    `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params"`
}

// MCPResponse represents an MCP tool call response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  *MCPResult  `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPResult represents the result of an MCP call
type MCPResult struct {
	Content []MCPContent `json:"content"`
}

// MCPContent represents content in an MCP response
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MCPError represents an MCP error
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// IsEnabled returns whether DBHub service is enabled
func (s *DBHubService) IsEnabled() bool {
	return s.enabled
}

// ExecuteQuery executes a SQL query via DBHub MCP server
func (s *DBHubService) ExecuteQuery(ctx context.Context, db *models.Database, query string, params []any) (*models.QueryResponse, error) {
	if !s.enabled {
		return nil, errors.ServiceUnavailable("DBHub service is not enabled")
	}

	start := time.Now()

	// Build MCP request for execute_sql tool
	mcpReq := MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "execute_sql",
			"arguments": map[string]interface{}{
				"source": db.CRDName,
				"sql":    query,
			},
		},
	}

	s.logger.Debug("Executing SQL query via DBHub",
		zap.String("database_id", db.ID),
		zap.String("source", db.CRDName),
		zap.String("query_preview", truncateQuery(query, 100)),
	)

	resp, err := s.callMCP(ctx, mcpReq)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, errors.Internal(fmt.Sprintf("DBHub error: %s", resp.Error.Message))
	}

	// Parse result from MCP response
	result := &models.QueryResponse{
		Duration: time.Since(start).Milliseconds(),
	}

	if resp.Result != nil && len(resp.Result.Content) > 0 && resp.Result.Content[0].Type == "text" {
		// Parse the JSON result from text content
		textContent := resp.Result.Content[0].Text
		s.logger.Debug("DBHub query result text content",
			zap.String("text", textContent),
		)

		// Try DBHub format first: {success: true, data: {rows: [...], count: N}}
		var dbhubResult struct {
			Success bool `json:"success"`
			Data    struct {
				Rows     []map[string]any `json:"rows"`
				Count    int              `json:"count"`
				SourceID string           `json:"source_id"`
			} `json:"data"`
		}

		if err := json.Unmarshal([]byte(textContent), &dbhubResult); err == nil && dbhubResult.Success {
			// DBHub format parsed successfully
			result.Rows = dbhubResult.Data.Rows
			// Extract columns from first row
			if len(dbhubResult.Data.Rows) > 0 {
				for col := range dbhubResult.Data.Rows[0] {
					result.Columns = append(result.Columns, col)
				}
			}
		} else {
			// Try legacy format: {columns: [], rows: [[]]}
			var queryResult struct {
				Columns []string        `json:"columns"`
				Rows    [][]interface{} `json:"rows"`
			}

			if err := json.Unmarshal([]byte(textContent), &queryResult); err != nil {
				// Try alternate format (array of objects)
				var altResult []map[string]interface{}
				if altErr := json.Unmarshal([]byte(textContent), &altResult); altErr != nil {
					s.logger.Error("Failed to parse query result",
						zap.String("text", textContent),
						zap.Error(err),
						zap.Error(altErr),
					)
					return nil, errors.InternalWithCause("Failed to parse query result", err)
				}

				// Extract columns from first row
				if len(altResult) > 0 {
					for col := range altResult[0] {
						result.Columns = append(result.Columns, col)
					}
				}
				result.Rows = altResult
			} else {
				result.Columns = queryResult.Columns
				result.Rows = make([]map[string]any, len(queryResult.Rows))

				for i, row := range queryResult.Rows {
					rowMap := make(map[string]any)
					for j, col := range queryResult.Columns {
						if j < len(row) {
							rowMap[col] = row[j]
						}
					}
					result.Rows[i] = rowMap
				}
			}
		}

		result.RowCount = len(result.Rows)
		result.Truncated = result.RowCount >= db.MaxRows
	}

	s.logger.Info("SQL query executed",
		zap.String("database_id", db.ID),
		zap.Int("row_count", result.RowCount),
		zap.Int64("duration_ms", result.Duration),
	)

	return result, nil
}

// TestConnection tests database connectivity via DBHub
func (s *DBHubService) TestConnection(ctx context.Context, db *models.Database) error {
	if !s.enabled {
		return errors.ServiceUnavailable("DBHub service is not enabled")
	}

	// Execute a simple query to test connection
	testQuery := "SELECT 1"
	switch db.Type {
	case models.DatabaseTypeSQLServer:
		testQuery = "SELECT 1 AS test"
	case models.DatabaseTypeMySQL, models.DatabaseTypeMariaDB:
		testQuery = "SELECT 1"
	}

	_, err := s.ExecuteQuery(ctx, db, testQuery, nil)
	return err
}

// GetSchema retrieves schema information via DBHub
func (s *DBHubService) GetSchema(ctx context.Context, db *models.Database, schemaType string) (*models.SchemaResponse, error) {
	if !s.enabled {
		return nil, errors.ServiceUnavailable("DBHub service is not enabled")
	}

	// Map schema type to DBHub object_type
	// DBHub supports: schema, table, column, procedure, index
	objectType := schemaType
	if schemaType == "database" {
		objectType = "schema" // databases map to schemas in DBHub
	}

	// Build MCP request for search_objects tool
	mcpReq := MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "search_objects",
			"arguments": map[string]interface{}{
				"object_type":  objectType,
				"detail_level": "summary",
				"limit":        1000,
			},
		},
	}

	s.logger.Debug("Fetching schema via DBHub",
		zap.String("database_id", db.ID),
		zap.String("schema_type", schemaType),
		zap.String("object_type", objectType),
	)

	resp, err := s.callMCP(ctx, mcpReq)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, errors.Internal(fmt.Sprintf("DBHub error: %s", resp.Error.Message))
	}

	result := &models.SchemaResponse{}

	if resp.Result != nil && len(resp.Result.Content) > 0 && resp.Result.Content[0].Type == "text" {
		textContent := resp.Result.Content[0].Text
		s.logger.Debug("DBHub schema result text content",
			zap.String("text", textContent),
		)

		// Parse DBHub format: {success: true, data: {object_type, results: [...]}}
		var dbhubResult struct {
			Success bool `json:"success"`
			Data    struct {
				ObjectType  string `json:"object_type"`
				Count       int    `json:"count"`
				DetailLevel string `json:"detail_level"`
				Results     []struct {
					Name        string `json:"name"`
					Schema      string `json:"schema"`
					ColumnCount int    `json:"column_count"`
					RowCount    int64  `json:"row_count"`
				} `json:"results"`
			} `json:"data"`
		}

		if err := json.Unmarshal([]byte(textContent), &dbhubResult); err != nil {
			s.logger.Error("Failed to parse schema result",
				zap.String("text", textContent),
				zap.Error(err),
			)
			return nil, errors.InternalWithCause("Failed to parse schema result", err)
		}

		if dbhubResult.Success {
			// Convert DBHub results to our SchemaResponse format
			switch dbhubResult.Data.ObjectType {
			case "table":
				result.Tables = make([]models.TableInfo, len(dbhubResult.Data.Results))
				for i, r := range dbhubResult.Data.Results {
					result.Tables[i] = models.TableInfo{
						Name:     r.Name,
						Schema:   r.Schema,
						RowCount: r.RowCount,
					}
				}
			case "schema":
				result.Schemas = make([]string, len(dbhubResult.Data.Results))
				for i, r := range dbhubResult.Data.Results {
					result.Schemas[i] = r.Name
				}
			}
		}
	}

	return result, nil
}

// GetTables retrieves table information for a database
func (s *DBHubService) GetTables(ctx context.Context, db *models.Database) ([]models.TableInfo, error) {
	schema, err := s.GetSchema(ctx, db, "table")
	if err != nil {
		return nil, err
	}
	return schema.Tables, nil
}

// GetTableColumns retrieves column information for a specific table
func (s *DBHubService) GetTableColumns(ctx context.Context, db *models.Database, tableName string) ([]models.ColumnInfo, error) {
	if !s.enabled {
		return nil, errors.ServiceUnavailable("DBHub service is not enabled")
	}

	// Parse schema and table name (format: "schema.table" or just "table")
	schemaName := "public"
	tblName := tableName
	if parts := strings.Split(tableName, "."); len(parts) == 2 {
		schemaName = parts[0]
		tblName = parts[1]
	}

	// Build MCP request for search_objects tool to get columns
	mcpReq := MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "search_objects",
			"arguments": map[string]interface{}{
				"object_type":  "column",
				"schema":       schemaName,
				"table":        tblName,
				"detail_level": "full",
				"limit":        1000,
			},
		},
	}

	s.logger.Debug("Fetching table columns via DBHub",
		zap.String("database_id", db.ID),
		zap.String("schema", schemaName),
		zap.String("table", tblName),
	)

	resp, err := s.callMCP(ctx, mcpReq)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, errors.Internal(fmt.Sprintf("DBHub error: %s", resp.Error.Message))
	}

	columns := []models.ColumnInfo{}

	if resp.Result != nil && len(resp.Result.Content) > 0 && resp.Result.Content[0].Type == "text" {
		textContent := resp.Result.Content[0].Text

		// Parse DBHub format
		var dbhubResult struct {
			Success bool `json:"success"`
			Data    struct {
				Results []struct {
					Name     string  `json:"name"`
					Type     string  `json:"type"`
					Nullable bool    `json:"nullable"`
					Default  *string `json:"default"`
				} `json:"results"`
			} `json:"data"`
		}

		if err := json.Unmarshal([]byte(textContent), &dbhubResult); err != nil {
			s.logger.Error("Failed to parse columns result",
				zap.String("text", textContent),
				zap.Error(err),
			)
			return nil, errors.InternalWithCause("Failed to parse columns result", err)
		}

		if dbhubResult.Success {
			columns = make([]models.ColumnInfo, len(dbhubResult.Data.Results))
			for i, r := range dbhubResult.Data.Results {
				col := models.ColumnInfo{
					Name:     r.Name,
					Type:     r.Type,
					Nullable: r.Nullable,
				}
				// Check if column is primary key (id column with uuid type is typically PK)
				if r.Name == "id" {
					col.PrimaryKey = true
				}
				if r.Default != nil {
					col.Default = *r.Default
				}
				columns[i] = col
			}
		}
	}

	return columns, nil
}

// GetTableColumnsLegacy retrieves column information using SQL (kept for reference)
func (s *DBHubService) GetTableColumnsLegacy(ctx context.Context, db *models.Database, tableName string) ([]models.ColumnInfo, error) {
	if !s.enabled {
		return nil, errors.ServiceUnavailable("DBHub service is not enabled")
	}

	// Build query based on database type
	var query string
	switch db.Type {
	case models.DatabaseTypePostgres:
		query = fmt.Sprintf(`
			SELECT column_name, data_type, is_nullable,
				   CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END as is_primary
			FROM information_schema.columns c
			LEFT JOIN (
				SELECT kcu.column_name
				FROM information_schema.table_constraints tc
				JOIN information_schema.key_column_usage kcu
					ON tc.constraint_name = kcu.constraint_name
				WHERE tc.constraint_type = 'PRIMARY KEY'
					AND tc.table_name = '%s'
			) pk ON c.column_name = pk.column_name
			WHERE c.table_name = '%s'
			ORDER BY c.ordinal_position
		`, tableName, tableName)
	case models.DatabaseTypeMySQL, models.DatabaseTypeMariaDB:
		query = fmt.Sprintf(`
			SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_KEY
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_NAME = '%s'
			ORDER BY ORDINAL_POSITION
		`, tableName)
	default:
		query = fmt.Sprintf(`
			SELECT column_name, data_type, is_nullable
			FROM information_schema.columns
			WHERE table_name = '%s'
		`, tableName)
	}

	result, err := s.ExecuteQuery(ctx, db, query, nil)
	if err != nil {
		return nil, err
	}

	columns := make([]models.ColumnInfo, 0, len(result.Rows))
	for _, row := range result.Rows {
		col := models.ColumnInfo{}
		if name, ok := row["column_name"].(string); ok {
			col.Name = name
		} else if name, ok := row["COLUMN_NAME"].(string); ok {
			col.Name = name
		}
		if dataType, ok := row["data_type"].(string); ok {
			col.Type = dataType
		} else if dataType, ok := row["DATA_TYPE"].(string); ok {
			col.Type = dataType
		}
		if nullable, ok := row["is_nullable"].(string); ok {
			col.Nullable = nullable == "YES"
		} else if nullable, ok := row["IS_NULLABLE"].(string); ok {
			col.Nullable = nullable == "YES"
		}
		if isPrimary, ok := row["is_primary"].(bool); ok {
			col.PrimaryKey = isPrimary
		} else if columnKey, ok := row["COLUMN_KEY"].(string); ok {
			col.PrimaryKey = columnKey == "PRI"
		}
		columns = append(columns, col)
	}

	return columns, nil
}

// callMCP sends a request to the DBHub MCP server
func (s *DBHubService) callMCP(ctx context.Context, req MCPRequest) (*MCPResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, errors.InternalWithCause("Failed to marshal MCP request", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, errors.InternalWithCause("Failed to create HTTP request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	s.logger.Debug("Calling DBHub MCP",
		zap.String("url", s.baseURL+"/mcp"),
		zap.String("method", req.Method),
	)

	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		s.logger.Error("DBHub MCP call failed",
			zap.Error(err),
			zap.String("url", s.baseURL),
		)
		return nil, errors.ExternalService("DBHub service unavailable", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, errors.InternalWithCause("Failed to read response", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		s.logger.Error("DBHub MCP returned error status",
			zap.Int("status", httpResp.StatusCode),
			zap.String("body", string(respBody)),
		)
		return nil, errors.Internal(fmt.Sprintf("DBHub returned status %d: %s", httpResp.StatusCode, string(respBody)))
	}

	s.logger.Debug("DBHub MCP raw response",
		zap.String("body", string(respBody)),
	)

	var resp MCPResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, errors.InternalWithCause("Failed to parse MCP response", err)
	}

	return &resp, nil
}

// HealthCheck checks if the DBHub service is healthy
func (s *DBHubService) HealthCheck(ctx context.Context) error {
	if !s.enabled {
		return nil // Not enabled is not an error
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}

	return nil
}

// truncateQuery truncates a query for logging purposes
func truncateQuery(query string, maxLen int) string {
	if len(query) <= maxLen {
		return query
	}
	return query[:maxLen] + "..."
}
