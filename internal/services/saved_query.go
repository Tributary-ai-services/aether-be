package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// SavedQueryService handles saved query management
type SavedQueryService struct {
	neo4j           *database.Neo4jClient
	databaseService *DatabaseService
	logger          *logger.Logger
}

// NewSavedQueryService creates a new saved query service
func NewSavedQueryService(neo4j *database.Neo4jClient, databaseService *DatabaseService, log *logger.Logger) *SavedQueryService {
	return &SavedQueryService{
		neo4j:           neo4j,
		databaseService: databaseService,
		logger:          log.WithService("saved_query_service"),
	}
}

// CreateSavedQuery creates a new saved query
func (s *SavedQueryService) CreateSavedQuery(ctx context.Context, userID, tenantID, spaceID string, req models.SavedQueryCreateRequest) (*models.SavedQuery, error) {
	s.logger.Info("Creating saved query",
		zap.String("user_id", userID),
		zap.String("tenant_id", tenantID),
		zap.String("space_id", spaceID),
		zap.String("name", req.Name),
		zap.String("database_id", req.DatabaseID),
	)

	// Verify the database exists and user has access
	db, err := s.databaseService.GetDatabase(ctx, req.DatabaseID, tenantID)
	if err != nil {
		s.logger.Error("Failed to verify database",
			zap.String("database_id", req.DatabaseID),
			zap.Error(err),
		)
		return nil, errors.ValidationWithDetails("Invalid database reference", map[string]any{
			"database_id": req.DatabaseID,
			"error":       err.Error(),
		})
	}

	// Create the saved query model
	query := models.NewSavedQuery(req, userID, tenantID, spaceID)
	query.DatabaseName = db.Name // Denormalize for display

	// Create the SavedQuery node in Neo4j
	createQuery := `
		CREATE (sq:SavedQuery {
			id: $id,
			name: $name,
			description: $description,
			query: $query,
			database_id: $database_id,
			database_name: $database_name,
			tenant_id: $tenant_id,
			space_id: $space_id,
			owner_id: $owner_id,
			visibility: $visibility,
			shared_with_teams: $shared_with_teams,
			tags: $tags,
			folder: $folder,
			execution_count: $execution_count,
			avg_duration_ms: $avg_duration_ms,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN sq.id
	`

	params := map[string]any{
		"id":                query.ID,
		"name":              query.Name,
		"description":       query.Description,
		"query":             query.Query,
		"database_id":       query.DatabaseID,
		"database_name":     query.DatabaseName,
		"tenant_id":         query.TenantID,
		"space_id":          query.SpaceID,
		"owner_id":          query.OwnerID,
		"visibility":        string(query.Visibility),
		"shared_with_teams": query.GetSharedWithTeamsJSON(),
		"tags":              query.GetTagsJSON(),
		"folder":            query.Folder,
		"execution_count":   query.ExecutionCount,
		"avg_duration_ms":   query.AvgDurationMs,
		"created_at":        query.CreatedAt.Format(time.RFC3339),
		"updated_at":        query.UpdatedAt.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, createQuery, params)
	if err != nil {
		s.logger.Error("Failed to create saved query node",
			zap.String("query_id", query.ID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to create saved query", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.Internal("Failed to create saved query: no record returned")
	}

	// Link to space
	linkQuery := `
		MATCH (sq:SavedQuery {id: $query_id})
		MATCH (sp:Space {id: $space_id})
		CREATE (sq)-[:BELONGS_TO {created_at: datetime()}]->(sp)
	`
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, linkQuery, map[string]any{
		"query_id": query.ID,
		"space_id": spaceID,
	})
	if err != nil {
		s.logger.Warn("Failed to link saved query to space",
			zap.String("query_id", query.ID),
			zap.String("space_id", spaceID),
			zap.Error(err),
		)
	}

	// Link to owner
	ownerQuery := `
		MATCH (sq:SavedQuery {id: $query_id})
		MATCH (u:User {id: $user_id})
		CREATE (u)-[:OWNS {created_at: datetime()}]->(sq)
	`
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, ownerQuery, map[string]any{
		"query_id": query.ID,
		"user_id":  userID,
	})
	if err != nil {
		s.logger.Warn("Failed to link saved query to owner",
			zap.String("query_id", query.ID),
			zap.String("user_id", userID),
			zap.Error(err),
		)
	}

	// Link to database
	dbLinkQuery := `
		MATCH (sq:SavedQuery {id: $query_id})
		MATCH (d:Database {id: $database_id})
		CREATE (sq)-[:USES_DATABASE {created_at: datetime()}]->(d)
	`
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, dbLinkQuery, map[string]any{
		"query_id":    query.ID,
		"database_id": req.DatabaseID,
	})
	if err != nil {
		s.logger.Warn("Failed to link saved query to database",
			zap.String("query_id", query.ID),
			zap.String("database_id", req.DatabaseID),
			zap.Error(err),
		)
	}

	s.logger.Info("Saved query created",
		zap.String("query_id", query.ID),
		zap.String("name", query.Name),
	)

	return query, nil
}

// GetSavedQuery retrieves a saved query by ID
func (s *SavedQueryService) GetSavedQuery(ctx context.Context, queryID, tenantID string) (*models.SavedQuery, error) {
	s.logger.Debug("Getting saved query by ID",
		zap.String("query_id", queryID),
		zap.String("tenant_id", tenantID),
	)

	query := `
		MATCH (sq:SavedQuery {id: $query_id})
		WHERE sq.tenant_id = $tenant_id
		RETURN sq.id, sq.name, sq.description, sq.query,
		       sq.database_id, sq.database_name,
		       sq.tenant_id, sq.space_id, sq.owner_id,
		       sq.visibility, sq.shared_with_teams, sq.tags, sq.folder,
		       sq.last_executed_at, sq.last_executed_by,
		       sq.execution_count, sq.avg_duration_ms, sq.last_row_count,
		       sq.created_at, sq.updated_at
	`

	params := map[string]any{
		"query_id":  queryID,
		"tenant_id": tenantID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get saved query",
			zap.String("query_id", queryID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to retrieve saved query", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Saved query not found", map[string]any{
			"query_id": queryID,
		})
	}

	return s.recordToSavedQuery(result.Records[0])
}

// ListSavedQueries lists saved queries with filtering and pagination
func (s *SavedQueryService) ListSavedQueries(ctx context.Context, tenantID, spaceID, userID string, userTeams []string, searchReq models.SavedQuerySearchRequest) (*models.SavedQueryListResponse, error) {
	s.logger.Debug("Listing saved queries",
		zap.String("tenant_id", tenantID),
		zap.String("space_id", spaceID),
		zap.String("user_id", userID),
	)

	// Default pagination
	page := searchReq.Page
	if page < 1 {
		page = 1
	}
	pageSize := searchReq.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// Build dynamic query based on filters
	queryBase := `
		MATCH (sq:SavedQuery)
		WHERE sq.tenant_id = $tenant_id AND sq.space_id = $space_id
		AND (
			sq.owner_id = $user_id
			OR sq.visibility = 'public'
			OR (sq.visibility = 'shared' AND ANY(team IN $user_teams WHERE team IN sq.shared_with_teams))
		)
	`

	params := map[string]any{
		"tenant_id":  tenantID,
		"space_id":   spaceID,
		"user_id":    userID,
		"user_teams": userTeams,
		"skip":       (page - 1) * pageSize,
		"limit":      pageSize,
	}

	// Add optional filters
	filters := ""
	if searchReq.DatabaseID != "" {
		filters += " AND sq.database_id = $database_id"
		params["database_id"] = searchReq.DatabaseID
	}
	if searchReq.Visibility != "" {
		filters += " AND sq.visibility = $visibility"
		params["visibility"] = string(searchReq.Visibility)
	}
	if searchReq.Folder != "" {
		filters += " AND sq.folder = $folder"
		params["folder"] = searchReq.Folder
	}
	if searchReq.OwnerID != "" {
		filters += " AND sq.owner_id = $owner_id_filter"
		params["owner_id_filter"] = searchReq.OwnerID
	}
	if searchReq.Search != "" {
		filters += " AND (toLower(sq.name) CONTAINS toLower($search) OR toLower(sq.description) CONTAINS toLower($search))"
		params["search"] = searchReq.Search
	}

	// Determine sort order
	sortBy := "sq.created_at"
	switch searchReq.SortBy {
	case "name":
		sortBy = "sq.name"
	case "updated_at":
		sortBy = "sq.updated_at"
	case "execution_count":
		sortBy = "sq.execution_count"
	case "last_executed_at":
		sortBy = "sq.last_executed_at"
	}

	sortOrder := "DESC"
	if searchReq.SortOrder == "asc" {
		sortOrder = "ASC"
	}

	// Build full query
	fullQuery := queryBase + filters + fmt.Sprintf(`
		RETURN sq.id, sq.name, sq.description, sq.query,
		       sq.database_id, sq.database_name,
		       sq.tenant_id, sq.space_id, sq.owner_id,
		       sq.visibility, sq.shared_with_teams, sq.tags, sq.folder,
		       sq.last_executed_at, sq.last_executed_by,
		       sq.execution_count, sq.avg_duration_ms, sq.last_row_count,
		       sq.created_at, sq.updated_at
		ORDER BY %s %s
		SKIP $skip LIMIT $limit
	`, sortBy, sortOrder)

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, fullQuery, params)
	if err != nil {
		s.logger.Error("Failed to list saved queries",
			zap.String("tenant_id", tenantID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to list saved queries", err)
	}

	queries := make([]models.SavedQueryResponse, 0, len(result.Records))
	for _, record := range result.Records {
		sq, err := s.recordToSavedQuery(record)
		if err != nil {
			s.logger.Warn("Failed to parse saved query record", zap.Error(err))
			continue
		}
		queries = append(queries, *sq.ToResponse())
	}

	// Get total count
	countQuery := queryBase + filters + `
		RETURN count(sq) as total
	`

	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, params)
	if err != nil {
		s.logger.Warn("Failed to get saved query count", zap.Error(err))
	}

	total := 0
	if len(countResult.Records) > 0 {
		if t, ok := countResult.Records[0].Values[0].(int64); ok {
			total = int(t)
		}
	}

	return &models.SavedQueryListResponse{
		Queries:  queries,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// UpdateSavedQuery updates a saved query
func (s *SavedQueryService) UpdateSavedQuery(ctx context.Context, queryID, tenantID, userID string, req models.SavedQueryUpdateRequest) (*models.SavedQuery, error) {
	s.logger.Info("Updating saved query",
		zap.String("query_id", queryID),
		zap.String("tenant_id", tenantID),
		zap.String("user_id", userID),
	)

	// Get existing query
	sq, err := s.GetSavedQuery(ctx, queryID, tenantID)
	if err != nil {
		return nil, err
	}

	// Check ownership
	if !sq.CanBeModifiedBy(userID) {
		return nil, errors.ForbiddenWithDetails("Not authorized to modify this query", map[string]any{
			"query_id": queryID,
			"owner_id": sq.OwnerID,
		})
	}

	// If database is being changed, verify access
	if req.DatabaseID != nil && *req.DatabaseID != sq.DatabaseID {
		db, err := s.databaseService.GetDatabase(ctx, *req.DatabaseID, tenantID)
		if err != nil {
			return nil, errors.ValidationWithDetails("Invalid database reference", map[string]any{
				"database_id": *req.DatabaseID,
			})
		}
		sq.DatabaseName = db.Name
	}

	// Apply updates
	sq.Update(req)

	// Update in Neo4j
	updateQuery := `
		MATCH (sq:SavedQuery {id: $query_id})
		WHERE sq.tenant_id = $tenant_id
		SET sq.name = $name,
		    sq.description = $description,
		    sq.query = $query,
		    sq.database_id = $database_id,
		    sq.database_name = $database_name,
		    sq.visibility = $visibility,
		    sq.shared_with_teams = $shared_with_teams,
		    sq.tags = $tags,
		    sq.folder = $folder,
		    sq.updated_at = datetime($updated_at)
		RETURN sq.id
	`

	params := map[string]any{
		"query_id":          queryID,
		"tenant_id":         tenantID,
		"name":              sq.Name,
		"description":       sq.Description,
		"query":             sq.Query,
		"database_id":       sq.DatabaseID,
		"database_name":     sq.DatabaseName,
		"visibility":        string(sq.Visibility),
		"shared_with_teams": sq.GetSharedWithTeamsJSON(),
		"tags":              sq.GetTagsJSON(),
		"folder":            sq.Folder,
		"updated_at":        sq.UpdatedAt.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, updateQuery, params)
	if err != nil {
		s.logger.Error("Failed to update saved query",
			zap.String("query_id", queryID),
			zap.Error(err),
		)
		return nil, errors.Database("Failed to update saved query", err)
	}

	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Saved query not found", map[string]any{
			"query_id": queryID,
		})
	}

	s.logger.Info("Saved query updated",
		zap.String("query_id", queryID),
	)

	return sq, nil
}

// DeleteSavedQuery deletes a saved query
func (s *SavedQueryService) DeleteSavedQuery(ctx context.Context, queryID, tenantID, userID string) error {
	s.logger.Info("Deleting saved query",
		zap.String("query_id", queryID),
		zap.String("tenant_id", tenantID),
		zap.String("user_id", userID),
	)

	// Get existing query to check ownership
	sq, err := s.GetSavedQuery(ctx, queryID, tenantID)
	if err != nil {
		return err
	}

	// Check ownership
	if !sq.CanBeModifiedBy(userID) {
		return errors.ForbiddenWithDetails("Not authorized to delete this query", map[string]any{
			"query_id": queryID,
			"owner_id": sq.OwnerID,
		})
	}

	// Delete all relationships and the node
	deleteQuery := `
		MATCH (sq:SavedQuery {id: $query_id})
		WHERE sq.tenant_id = $tenant_id
		DETACH DELETE sq
		RETURN count(sq) as deleted
	`

	params := map[string]any{
		"query_id":  queryID,
		"tenant_id": tenantID,
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, deleteQuery, params)
	if err != nil {
		s.logger.Error("Failed to delete saved query",
			zap.String("query_id", queryID),
			zap.Error(err),
		)
		return errors.Database("Failed to delete saved query", err)
	}

	if len(result.Records) == 0 {
		return errors.NotFoundWithDetails("Saved query not found", map[string]any{
			"query_id": queryID,
		})
	}

	s.logger.Info("Saved query deleted",
		zap.String("query_id", queryID),
	)

	return nil
}

// ExecuteSavedQuery executes a saved query and records stats
func (s *SavedQueryService) ExecuteSavedQuery(ctx context.Context, queryID, tenantID, userID, spaceID string, userTeams []string, execReq models.SavedQueryExecuteRequest) (*models.SavedQueryExecuteResponse, error) {
	s.logger.Info("Executing saved query",
		zap.String("query_id", queryID),
		zap.String("user_id", userID),
	)

	// Get the saved query
	sq, err := s.GetSavedQuery(ctx, queryID, tenantID)
	if err != nil {
		return nil, err
	}

	// Check access
	if !sq.IsAccessibleBy(userID, spaceID, userTeams) {
		return nil, errors.ForbiddenWithDetails("Not authorized to execute this query", map[string]any{
			"query_id": queryID,
		})
	}

	// Execute the query via database service
	startTime := time.Now()
	result, err := s.databaseService.ExecuteQuery(ctx, sq.DatabaseID, tenantID, sq.Query, execReq.Parameters)
	duration := time.Since(startTime).Milliseconds()

	if err != nil {
		s.logger.Error("Failed to execute saved query",
			zap.String("query_id", queryID),
			zap.Error(err),
		)
		return nil, err
	}

	// Record execution stats
	sq.RecordExecution(userID, duration, result.RowCount)

	// Update stats in Neo4j (fire and forget)
	go func() {
		updateQuery := `
			MATCH (sq:SavedQuery {id: $query_id})
			SET sq.last_executed_at = datetime($last_executed_at),
			    sq.last_executed_by = $last_executed_by,
			    sq.execution_count = $execution_count,
			    sq.avg_duration_ms = $avg_duration_ms,
			    sq.last_row_count = $last_row_count,
			    sq.updated_at = datetime($updated_at)
		`
		params := map[string]any{
			"query_id":         queryID,
			"last_executed_at": sq.LastExecutedAt.Format(time.RFC3339),
			"last_executed_by": sq.LastExecutedBy,
			"execution_count":  sq.ExecutionCount,
			"avg_duration_ms":  sq.AvgDurationMs,
			"last_row_count":   sq.LastRowCount,
			"updated_at":       sq.UpdatedAt.Format(time.RFC3339),
		}
		_, err := s.neo4j.ExecuteQueryWithLogging(context.Background(), updateQuery, params)
		if err != nil {
			s.logger.Warn("Failed to update execution stats",
				zap.String("query_id", queryID),
				zap.Error(err),
			)
		}
	}()

	s.logger.Info("Saved query executed",
		zap.String("query_id", queryID),
		zap.Int("row_count", result.RowCount),
		zap.Int64("duration_ms", duration),
	)

	return &models.SavedQueryExecuteResponse{
		QueryID:    queryID,
		QueryName:  sq.Name,
		Columns:    result.Columns,
		Rows:       result.Rows,
		RowCount:   result.RowCount,
		Truncated:  result.Truncated,
		Duration:   duration,
		ExecutedAt: time.Now(),
	}, nil
}

// DuplicateSavedQuery creates a copy of a saved query
func (s *SavedQueryService) DuplicateSavedQuery(ctx context.Context, queryID, tenantID, userID, spaceID, newName string, userTeams []string) (*models.SavedQuery, error) {
	s.logger.Info("Duplicating saved query",
		zap.String("query_id", queryID),
		zap.String("user_id", userID),
		zap.String("new_name", newName),
	)

	// Get the source query
	source, err := s.GetSavedQuery(ctx, queryID, tenantID)
	if err != nil {
		return nil, err
	}

	// Check access
	if !source.IsAccessibleBy(userID, spaceID, userTeams) {
		return nil, errors.ForbiddenWithDetails("Not authorized to duplicate this query", map[string]any{
			"query_id": queryID,
		})
	}

	// Create duplicate
	duplicate := source.Duplicate(userID, newName)

	// Create the duplicate in Neo4j (reuse create logic)
	createReq := models.SavedQueryCreateRequest{
		Name:        duplicate.Name,
		Description: duplicate.Description,
		Query:       duplicate.Query,
		DatabaseID:  duplicate.DatabaseID,
		Visibility:  duplicate.Visibility,
		Tags:        duplicate.Tags,
		Folder:      duplicate.Folder,
	}

	return s.CreateSavedQuery(ctx, userID, tenantID, spaceID, createReq)
}

// recordToSavedQuery converts a Neo4j record to a SavedQuery model
func (s *SavedQueryService) recordToSavedQuery(record any) (*models.SavedQuery, error) {
	r, ok := record.(interface {
		Values() []any
		Keys() []string
	})
	if !ok {
		type recordWithGet interface {
			Get(key string) (any, bool)
		}
		if rg, ok := record.(recordWithGet); ok {
			return s.recordToSavedQuery2(rg)
		}
		return nil, fmt.Errorf("invalid record type: %T", record)
	}

	keys := r.Keys()
	values := r.Values()

	sq := &models.SavedQuery{}
	for i, key := range keys {
		if i >= len(values) {
			break
		}
		val := values[i]
		if val == nil {
			continue
		}

		switch key {
		case "sq.id":
			sq.ID = toString(val)
		case "sq.name":
			sq.Name = toString(val)
		case "sq.description":
			sq.Description = toString(val)
		case "sq.query":
			sq.Query = toString(val)
		case "sq.database_id":
			sq.DatabaseID = toString(val)
		case "sq.database_name":
			sq.DatabaseName = toString(val)
		case "sq.tenant_id":
			sq.TenantID = toString(val)
		case "sq.space_id":
			sq.SpaceID = toString(val)
		case "sq.owner_id":
			sq.OwnerID = toString(val)
		case "sq.visibility":
			sq.Visibility = models.QueryVisibility(toString(val))
		case "sq.shared_with_teams":
			sq.SetSharedWithTeamsFromJSON(toString(val))
		case "sq.tags":
			sq.SetTagsFromJSON(toString(val))
		case "sq.folder":
			sq.Folder = toString(val)
		case "sq.last_executed_at":
			sq.LastExecutedAt = toTime(val)
		case "sq.last_executed_by":
			sq.LastExecutedBy = toString(val)
		case "sq.execution_count":
			sq.ExecutionCount = toInt(val)
		case "sq.avg_duration_ms":
			sq.AvgDurationMs = toInt64(val)
		case "sq.last_row_count":
			sq.LastRowCount = toInt(val)
		case "sq.created_at":
			if t := toTime(val); t != nil {
				sq.CreatedAt = *t
			}
		case "sq.updated_at":
			if t := toTime(val); t != nil {
				sq.UpdatedAt = *t
			}
		}
	}

	return sq, nil
}

// recordToSavedQuery2 handles records with Get method
func (s *SavedQueryService) recordToSavedQuery2(r interface{ Get(key string) (any, bool) }) (*models.SavedQuery, error) {
	sq := &models.SavedQuery{}

	if v, ok := r.Get("sq.id"); ok && v != nil {
		sq.ID = toString(v)
	}
	if v, ok := r.Get("sq.name"); ok && v != nil {
		sq.Name = toString(v)
	}
	if v, ok := r.Get("sq.description"); ok && v != nil {
		sq.Description = toString(v)
	}
	if v, ok := r.Get("sq.query"); ok && v != nil {
		sq.Query = toString(v)
	}
	if v, ok := r.Get("sq.database_id"); ok && v != nil {
		sq.DatabaseID = toString(v)
	}
	if v, ok := r.Get("sq.database_name"); ok && v != nil {
		sq.DatabaseName = toString(v)
	}
	if v, ok := r.Get("sq.tenant_id"); ok && v != nil {
		sq.TenantID = toString(v)
	}
	if v, ok := r.Get("sq.space_id"); ok && v != nil {
		sq.SpaceID = toString(v)
	}
	if v, ok := r.Get("sq.owner_id"); ok && v != nil {
		sq.OwnerID = toString(v)
	}
	if v, ok := r.Get("sq.visibility"); ok && v != nil {
		sq.Visibility = models.QueryVisibility(toString(v))
	}
	if v, ok := r.Get("sq.shared_with_teams"); ok && v != nil {
		sq.SetSharedWithTeamsFromJSON(toString(v))
	}
	if v, ok := r.Get("sq.tags"); ok && v != nil {
		sq.SetTagsFromJSON(toString(v))
	}
	if v, ok := r.Get("sq.folder"); ok && v != nil {
		sq.Folder = toString(v)
	}
	if v, ok := r.Get("sq.last_executed_at"); ok && v != nil {
		sq.LastExecutedAt = toTime(v)
	}
	if v, ok := r.Get("sq.last_executed_by"); ok && v != nil {
		sq.LastExecutedBy = toString(v)
	}
	if v, ok := r.Get("sq.execution_count"); ok && v != nil {
		sq.ExecutionCount = toInt(v)
	}
	if v, ok := r.Get("sq.avg_duration_ms"); ok && v != nil {
		sq.AvgDurationMs = toInt64(v)
	}
	if v, ok := r.Get("sq.last_row_count"); ok && v != nil {
		sq.LastRowCount = toInt(v)
	}
	if v, ok := r.Get("sq.created_at"); ok && v != nil {
		if t := toTime(v); t != nil {
			sq.CreatedAt = *t
		}
	}
	if v, ok := r.Get("sq.updated_at"); ok && v != nil {
		if t := toTime(v); t != nil {
			sq.UpdatedAt = *t
		}
	}

	return sq, nil
}

// toInt64 converts any value to int64
func toInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return int64(val)
	case int64:
		return val
	case float64:
		return int64(val)
	default:
		return 0
	}
}
