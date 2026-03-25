package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// Neo4jQueryService handles Neo4j database queries via the neo4j-go-driver.
// Credentials are read from K8s Secrets referenced by the Database model.
type Neo4jQueryService struct {
	k8s    kubernetes.Interface
	logger *logger.Logger
}

// NewNeo4jQueryService creates a new Neo4j query service.
func NewNeo4jQueryService(k8s kubernetes.Interface, log *logger.Logger) *Neo4jQueryService {
	return &Neo4jQueryService{
		k8s:    k8s,
		logger: log.WithService("neo4j_query_service"),
	}
}

// GetCredentials reads username/password from the K8s Secret referenced by the Database model.
func (s *Neo4jQueryService) GetCredentials(ctx context.Context, db *models.Database) (string, string, error) {
	if s.k8s == nil {
		return "", "", fmt.Errorf("kubernetes client not configured")
	}

	ns := db.SecretNamespace
	if ns == "" {
		ns = "tas-mcp-servers"
	}

	secret, err := s.k8s.CoreV1().Secrets(ns).Get(ctx, db.SecretName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get secret %s/%s: %w", ns, db.SecretName, err)
	}

	username := string(secret.Data["username"])
	password := string(secret.Data["password"])
	if username == "" || password == "" {
		return "", "", fmt.Errorf("secret %s/%s missing username or password", ns, db.SecretName)
	}

	return username, password, nil
}

// buildURI constructs the Neo4j bolt URI from the Database model.
func (s *Neo4jQueryService) buildURI(db *models.Database) string {
	protocol := db.Protocol
	if protocol == "" {
		protocol = "bolt"
	}
	return fmt.Sprintf("%s://%s:%d", protocol, db.Host, db.Port)
}

// newDriver creates a temporary Neo4j driver for a query operation.
func (s *Neo4jQueryService) newDriver(ctx context.Context, db *models.Database) (neo4j.DriverWithContext, error) {
	username, password, err := s.GetCredentials(ctx, db)
	if err != nil {
		return nil, err
	}

	uri := s.buildURI(db)
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, fmt.Errorf("failed to create neo4j driver: %w", err)
	}

	return driver, nil
}

// TestConnection tests connectivity to a Neo4j database.
func (s *Neo4jQueryService) TestConnection(ctx context.Context, db *models.Database) error {
	driver, err := s.newDriver(ctx, db)
	if err != nil {
		return err
	}
	defer driver.Close(ctx)

	if err := driver.VerifyConnectivity(ctx); err != nil {
		return fmt.Errorf("neo4j connectivity check failed: %w", err)
	}

	// Run a trivial query to verify the database is accessible
	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: db.Database,
	})
	defer session.Close(ctx)

	_, err = session.Run(ctx, "RETURN 1", nil)
	if err != nil {
		return fmt.Errorf("neo4j test query failed: %w", err)
	}

	return nil
}

// ExecuteQuery runs a Cypher query and returns the results.
func (s *Neo4jQueryService) ExecuteQuery(ctx context.Context, db *models.Database, query string, params []any) (*models.QueryResponse, error) {
	start := time.Now()

	s.logger.Info("Executing user Cypher query",
		zap.String("database_id", db.ID),
		zap.String("host", db.Host),
		zap.Int("port", db.Port),
		zap.String("database", db.Database),
		zap.String("secret_name", db.SecretName),
		zap.String("secret_namespace", db.SecretNamespace),
		zap.String("query_preview", truncateQuery(query, 100)),
	)

	driver, err := s.newDriver(ctx, db)
	if err != nil {
		s.logger.Error("Failed to create Neo4j driver",
			zap.String("database_id", db.ID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to connect to Neo4j", err)
	}
	defer driver.Close(ctx)

	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: db.Database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, nil)
	if err != nil {
		s.logger.Error("Failed to execute Cypher query",
			zap.String("database_id", db.ID),
			zap.String("query_preview", truncateQuery(query, 200)),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to execute Cypher query", err)
	}

	var columns []string
	var rows []map[string]any
	maxRows := db.MaxRows
	if maxRows <= 0 {
		maxRows = 1000
	}

	truncated := false
	for result.Next(ctx) {
		record := result.Record()

		if columns == nil {
			columns = record.Keys
		}

		if len(rows) >= maxRows {
			truncated = true
			break
		}

		row := make(map[string]any, len(columns))
		for _, key := range columns {
			val, _ := record.Get(key)
			row[key] = convertNeo4jValue(val)
		}
		rows = append(rows, row)
	}

	if err := result.Err(); err != nil {
		return nil, errors.Database("Neo4j query error", err)
	}

	if columns == nil {
		columns = []string{}
	}
	if rows == nil {
		rows = []map[string]any{}
	}

	duration := time.Since(start).Milliseconds()

	s.logger.Debug("Neo4j query executed",
		zap.Int("row_count", len(rows)),
		zap.Int64("duration_ms", duration),
		zap.Bool("truncated", truncated),
	)

	return &models.QueryResponse{
		Columns:   columns,
		Rows:      rows,
		RowCount:  len(rows),
		Truncated: truncated,
		Duration:  duration,
	}, nil
}

// GetSchema returns node labels and relationship types.
func (s *Neo4jQueryService) GetSchema(ctx context.Context, db *models.Database) (*models.SchemaResponse, error) {
	driver, err := s.newDriver(ctx, db)
	if err != nil {
		return nil, errors.Database("Failed to connect to Neo4j", err)
	}
	defer driver.Close(ctx)

	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: db.Database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	// Get labels
	labelsResult, err := session.Run(ctx, "CALL db.labels()", nil)
	if err != nil {
		return nil, errors.Database("Failed to get labels", err)
	}

	var labels []string
	for labelsResult.Next(ctx) {
		record := labelsResult.Record()
		if val, ok := record.Get("label"); ok {
			if s, ok := val.(string); ok {
				labels = append(labels, s)
			}
		}
	}

	// Get relationship types
	relResult, err := session.Run(ctx, "CALL db.relationshipTypes()", nil)
	if err != nil {
		return nil, errors.Database("Failed to get relationship types", err)
	}

	var relTypes []string
	for relResult.Next(ctx) {
		record := relResult.Record()
		if val, ok := record.Get("relationshipType"); ok {
			if s, ok := val.(string); ok {
				relTypes = append(relTypes, s)
			}
		}
	}

	// Return labels as schemas and relationship types as tables (conceptual mapping)
	tables := make([]models.TableInfo, 0, len(labels))
	for _, label := range labels {
		tables = append(tables, models.TableInfo{
			Name:   label,
			Schema: "nodes",
		})
	}
	for _, rel := range relTypes {
		tables = append(tables, models.TableInfo{
			Name:   rel,
			Schema: "relationships",
		})
	}

	return &models.SchemaResponse{
		Schemas: []string{"nodes", "relationships"},
		Tables:  tables,
	}, nil
}

// GetTables returns node labels as table-like entries.
func (s *Neo4jQueryService) GetTables(ctx context.Context, db *models.Database) ([]models.TableInfo, error) {
	driver, err := s.newDriver(ctx, db)
	if err != nil {
		return nil, errors.Database("Failed to connect to Neo4j", err)
	}
	defer driver.Close(ctx)

	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: db.Database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "CALL db.labels()", nil)
	if err != nil {
		return nil, errors.Database("Failed to get labels", err)
	}

	var tables []models.TableInfo
	for result.Next(ctx) {
		record := result.Record()
		if val, ok := record.Get("label"); ok {
			if label, ok := val.(string); ok {
				tables = append(tables, models.TableInfo{
					Name:   label,
					Schema: "nodes",
				})
			}
		}
	}

	return tables, nil
}

// GetTableColumns returns property keys for a given node label.
func (s *Neo4jQueryService) GetTableColumns(ctx context.Context, db *models.Database, label string) ([]models.ColumnInfo, error) {
	driver, err := s.newDriver(ctx, db)
	if err != nil {
		return nil, errors.Database("Failed to connect to Neo4j", err)
	}
	defer driver.Close(ctx)

	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: db.Database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	// Sample up to 100 nodes of this label and collect unique property keys
	query := fmt.Sprintf("MATCH (n:`%s`) RETURN keys(n) AS props LIMIT 100", strings.ReplaceAll(label, "`", "``"))
	result, err := session.Run(ctx, query, nil)
	if err != nil {
		return nil, errors.Database("Failed to get property keys", err)
	}

	propSet := make(map[string]bool)
	for result.Next(ctx) {
		record := result.Record()
		if val, ok := record.Get("props"); ok {
			if keys, ok := val.([]any); ok {
				for _, k := range keys {
					if s, ok := k.(string); ok {
						propSet[s] = true
					}
				}
			}
		}
	}

	columns := make([]models.ColumnInfo, 0, len(propSet))
	for prop := range propSet {
		columns = append(columns, models.ColumnInfo{
			Name:     prop,
			Type:     "any", // Neo4j properties are schemaless
			Nullable: true,
		})
	}

	return columns, nil
}

// CreateSecret creates a K8s Secret with database credentials.
func (s *Neo4jQueryService) CreateSecret(ctx context.Context, name, namespace, username, password string) error {
	if s.k8s == nil {
		return fmt.Errorf("kubernetes client not configured")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "aether-be",
				"app.kubernetes.io/component":  "database-credentials",
			},
		},
		Data: map[string][]byte{
			"username": []byte(username),
			"password": []byte(password),
		},
	}

	_, err := s.k8s.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create secret %s/%s: %w", namespace, name, err)
	}

	return nil
}

// UpdateSecret updates an existing K8s Secret with new credentials.
func (s *Neo4jQueryService) UpdateSecret(ctx context.Context, name, namespace, username, password string) error {
	if s.k8s == nil {
		return fmt.Errorf("kubernetes client not configured")
	}

	secret, err := s.k8s.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get secret %s/%s: %w", namespace, name, err)
	}

	secret.Data["username"] = []byte(username)
	secret.Data["password"] = []byte(password)

	_, err = s.k8s.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update secret %s/%s: %w", namespace, name, err)
	}

	return nil
}

// DeleteSecret deletes a K8s Secret.
func (s *Neo4jQueryService) DeleteSecret(ctx context.Context, name, namespace string) error {
	if s.k8s == nil {
		return fmt.Errorf("kubernetes client not configured")
	}

	err := s.k8s.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete secret %s/%s: %w", namespace, name, err)
	}

	return nil
}

// convertNeo4jValue converts neo4j driver types to JSON-safe Go types.
func convertNeo4jValue(val any) any {
	if val == nil {
		return nil
	}

	switch v := val.(type) {
	case neo4j.Node:
		result := map[string]any{
			"_id":     v.ElementId,
			"_labels": v.Labels,
		}
		for k, pv := range v.Props {
			result[k] = convertNeo4jValue(pv)
		}
		return result
	case neo4j.Relationship:
		return map[string]any{
			"_id":         v.ElementId,
			"_type":       v.Type,
			"_startNodeId": v.StartElementId,
			"_endNodeId":   v.EndElementId,
			"properties":  v.Props,
		}
	case neo4j.Path:
		nodes := make([]any, len(v.Nodes))
		for i, n := range v.Nodes {
			nodes[i] = convertNeo4jValue(n)
		}
		rels := make([]any, len(v.Relationships))
		for i, r := range v.Relationships {
			rels[i] = convertNeo4jValue(r)
		}
		return map[string]any{
			"nodes":         nodes,
			"relationships": rels,
		}
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = convertNeo4jValue(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, mv := range v {
			result[k] = convertNeo4jValue(mv)
		}
		return result
	default:
		return v
	}
}
