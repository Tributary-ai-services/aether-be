package services

// The SQL console through the tas-mcp proxy (Plan #22 C.6, AETHER-DBHUB-1).
//
// Before C.6 the console sent every query to dbhub, which is stock bytebase:
// it never reads a per-call connection, so whatever host a user had typed, the
// SQL ran against dbhub's single source. This path sends the query to
// postgres-mcp, the TAS bridge, through the proxy with the org's token. The
// proxy injects the tenant's stored connection (C.4), and postgres-mcp refuses
// any call without one (SEC-38).
//
// It fails closed at every step and never falls back to dbhub:
//   - no space context, a personal space, or no signed-in user: 403;
//   - the proxy refuses the org or user (unlinked org, not a member): 403;
//   - a connection that was never copied to the shared store
//     (no mcp_connection_id): 409, with the reason;
//   - a database type postgres-mcp cannot serve: 400;
//   - every proxy refusal is passed on as a refusal.
//
// Tenant isolation is the proxy's: it verifies with AIQG that the org is
// linked to a tenant and that the user is a member of it (AIQG-88), and the
// connection it injects is keyed by that tenant. aether-be only names the org
// of the space the caller is already authorized for, and the caller's sub.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// postgresMCPQueryTool is @modelcontextprotocol/server-postgres's only tool.
// It runs the SQL in a READ ONLY transaction and takes no bind parameters.
const postgresMCPQueryTool = "query"

// spaceContextKey carries the caller's space into the service layer.
type spaceContextKey struct{}

// WithSpaceContext returns ctx carrying sc, for the proxy SQL path.
func WithSpaceContext(ctx context.Context, sc *models.SpaceContext) context.Context {
	return context.WithValue(ctx, spaceContextKey{}, sc)
}

// spaceContextFrom returns the space a request carries, or nil.
func spaceContextFrom(ctx context.Context) *models.SpaceContext {
	sc, _ := ctx.Value(spaceContextKey{}).(*models.SpaceContext)
	return sc
}

// ProxySQLService runs console queries through the proxy.
type ProxySQLService struct {
	proxy    *MCPProxyClient
	serverID string
	logger   *zap.Logger
}

// NewProxySQLService builds the proxy SQL path.
func NewProxySQLService(proxy *MCPProxyClient, serverID string, log *zap.Logger) *ProxySQLService {
	if serverID == "" {
		serverID = "postgres-mcp"
	}
	return &ProxySQLService{proxy: proxy, serverID: serverID, logger: log}
}

// ServerID is the proxy id of the console's server.
func (s *ProxySQLService) ServerID() string { return s.serverID }

// ErrConnectionNotShared codes the 409 for a connection never migrated.
const codeConnectionNotShared = "connection_not_shared"

// guard checks everything that does not need the network and returns who
// the call acts for.
func (s *ProxySQLService) guard(ctx context.Context, db *models.Database) (ProxyCaller, error) {
	if db == nil {
		return ProxyCaller{}, errors.Internal("no database")
	}
	sc := spaceContextFrom(ctx)
	if sc == nil {
		return ProxyCaller{}, errors.ForbiddenWithDetails("Space context required", map[string]any{"code": "mcp_space_required"})
	}
	// The database was already fetched under sc.TenantID; this is a second,
	// local check so that a future caller cannot pair one space's token with
	// another tenant's row.
	if db.TenantID != sc.TenantID {
		return ProxyCaller{}, errors.ForbiddenWithDetails("This connection belongs to another space", map[string]any{"code": "mcp_cross_tenant"})
	}
	if db.Type != models.DatabaseTypePostgres {
		return ProxyCaller{}, errors.ValidationWithDetails("Only PostgreSQL connections run through the shared MCP connection store", map[string]any{
			"database_id": db.ID,
			"type":        string(db.Type),
			"supported":   []string{"postgres"},
		})
	}
	if db.MCPConnectionID == "" || (db.MCPServerID != "" && db.MCPServerID != s.serverID) {
		return ProxyCaller{}, errors.ConflictWithDetails("This connection has not been moved to the shared connection store yet; add it in AIQG Settings > Tools (MCP) or run the migration", map[string]any{
			"code":        codeConnectionNotShared,
			"database_id": db.ID,
		})
	}
	return CallerFor(sc)
}

// runSQL sends one statement and returns its rows.
func (s *ProxySQLService) runSQL(ctx context.Context, tok ProxyCaller, sql string) ([]map[string]any, error) {
	res, err := s.proxy.CallTool(ctx, tok, s.serverID, postgresMCPQueryTool, map[string]interface{}{"sql": sql})
	if err != nil {
		return nil, err
	}
	text := ""
	for _, c := range res.Content {
		if c.Type == "text" {
			text = c.Text
			break
		}
	}
	if res.IsError {
		// server-postgres puts the database error in the text. It names no
		// credential: the proxy redacts every injected value first (C.4).
		return nil, errors.ValidationWithDetails("Query failed", map[string]any{"error": preview([]byte(text), 512)})
	}
	if strings.TrimSpace(text) == "" {
		return []map[string]any{}, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(text), &rows); err != nil {
		return nil, errors.ExternalService("postgres-mcp returned an unreadable result", err)
	}
	return rows, nil
}

// ExecuteQuery runs a console query.
func (s *ProxySQLService) ExecuteQuery(ctx context.Context, db *models.Database, query string, params []any) (*models.QueryResponse, error) {
	tok, err := s.guard(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(params) > 0 {
		return nil, errors.ValidationWithDetails("Bind parameters are not supported for shared connections", map[string]any{"database_id": db.ID})
	}
	start := time.Now()
	rows, err := s.runSQL(ctx, tok, query)
	if err != nil {
		return nil, err
	}
	out := &models.QueryResponse{Rows: rows, RowCount: len(rows), Duration: time.Since(start).Milliseconds()}
	if len(rows) > 0 {
		for col := range rows[0] {
			out.Columns = append(out.Columns, col)
		}
	}
	if db.MaxRows > 0 && len(rows) > db.MaxRows {
		out.Rows = rows[:db.MaxRows]
		out.RowCount = db.MaxRows
		out.Truncated = true
	}
	s.logger.Info("SQL query executed via MCP proxy",
		zap.String("database_id", db.ID),
		zap.String("server_id", s.serverID),
		zap.Int("row_count", out.RowCount),
		zap.Int64("duration_ms", out.Duration),
	)
	return out, nil
}

// TestConnection runs SELECT 1 on the shared connection.
func (s *ProxySQLService) TestConnection(ctx context.Context, db *models.Database) error {
	tok, err := s.guard(ctx, db)
	if err != nil {
		return err
	}
	_, err = s.runSQL(ctx, tok, "SELECT 1 AS ok")
	return err
}

// tablesSQL lists user tables with an estimated row count.
const tablesSQL = `SELECT n.nspname AS schema, c.relname AS name, GREATEST(c.reltuples, 0)::bigint AS row_count
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r','p','v','m','f') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%'
ORDER BY 1, 2 LIMIT 1000`

const schemasSQL = `SELECT nspname AS name FROM pg_catalog.pg_namespace
WHERE nspname NOT IN ('pg_catalog','information_schema') AND nspname NOT LIKE 'pg_toast%' AND nspname NOT LIKE 'pg_temp%'
ORDER BY 1`

// GetSchema returns tables or schemas.
func (s *ProxySQLService) GetSchema(ctx context.Context, db *models.Database, schemaType string) (*models.SchemaResponse, error) {
	tok, err := s.guard(ctx, db)
	if err != nil {
		return nil, err
	}
	out := &models.SchemaResponse{}
	switch schemaType {
	case "schema", "database":
		rows, err := s.runSQL(ctx, tok, schemasSQL)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out.Schemas = append(out.Schemas, toString(r["name"]))
		}
	default:
		tables, err := s.tables(ctx, tok)
		if err != nil {
			return nil, err
		}
		out.Tables = tables
	}
	return out, nil
}

// GetTables lists tables.
func (s *ProxySQLService) GetTables(ctx context.Context, db *models.Database) ([]models.TableInfo, error) {
	tok, err := s.guard(ctx, db)
	if err != nil {
		return nil, err
	}
	return s.tables(ctx, tok)
}

func (s *ProxySQLService) tables(ctx context.Context, tok ProxyCaller) ([]models.TableInfo, error) {
	rows, err := s.runSQL(ctx, tok, tablesSQL)
	if err != nil {
		return nil, err
	}
	out := make([]models.TableInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.TableInfo{Name: toString(r["name"]), Schema: toString(r["schema"]), RowCount: jsonInt64(r["row_count"])})
	}
	return out, nil
}

// pgIdentRe is what a schema or table name may contain on this path. The
// name is placed in a SQL string literal (server-postgres takes no bind
// parameters), so it is checked rather than escaped alone.
var pgIdentRe = regexp.MustCompile(`^[A-Za-z0-9_$ -]{1,63}$`)

// GetTableColumns lists a table's columns. tableName is "table" or "schema.table".
func (s *ProxySQLService) GetTableColumns(ctx context.Context, db *models.Database, tableName string) ([]models.ColumnInfo, error) {
	tok, err := s.guard(ctx, db)
	if err != nil {
		return nil, err
	}
	schema, table := "public", tableName
	if parts := strings.Split(tableName, "."); len(parts) == 2 {
		schema, table = parts[0], parts[1]
	}
	if !pgIdentRe.MatchString(schema) || !pgIdentRe.MatchString(table) {
		return nil, errors.ValidationWithDetails("Invalid schema or table name", map[string]any{"table": tableName})
	}
	sql := fmt.Sprintf(`SELECT c.column_name AS name, c.data_type AS type, (c.is_nullable = 'YES') AS nullable, c.column_default AS "default",
  EXISTS (SELECT 1 FROM information_schema.table_constraints tc JOIN information_schema.key_column_usage k
    ON k.constraint_name = tc.constraint_name AND k.table_schema = tc.table_schema AND k.table_name = tc.table_name
    WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = c.table_schema AND tc.table_name = c.table_name AND k.column_name = c.column_name) AS primary_key
FROM information_schema.columns c WHERE c.table_schema = '%s' AND c.table_name = '%s' ORDER BY c.ordinal_position`, schema, table)
	rows, err := s.runSQL(ctx, tok, sql)
	if err != nil {
		return nil, err
	}
	out := make([]models.ColumnInfo, 0, len(rows))
	for _, r := range rows {
		col := models.ColumnInfo{Name: toString(r["name"]), Type: toString(r["type"]), Nullable: toBool(r["nullable"]), PrimaryKey: toBool(r["primary_key"])}
		if d, ok := r["default"]; ok && d != nil {
			col.Default = toString(d)
		}
		out = append(out, col)
	}
	return out, nil
}

// jsonInt64 reads a JSON number or a numeric string (node-postgres returns
// bigint as a string).
func jsonInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		var x int64
		_, _ = fmt.Sscan(n, &x)
		return x
	}
	return 0
}
