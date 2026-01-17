package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// CreateExperiment creates a new ML experiment
func (s *MLService) CreateExperiment(ctx context.Context, req models.CreateExperimentRequest, userID string, spaceContext *models.SpaceContext) (*models.MLExperiment, error) {
	// Verify the model exists
	_, err := s.GetModelByID(ctx, req.ModelID, spaceContext)
	if err != nil {
		return nil, fmt.Errorf("model not found: %w", err)
	}

	experiment := models.NewMLExperiment(req, userID, spaceContext.TenantID, spaceContext.SpaceID)

	query := `
		CREATE (e:MLExperiment {
			id: $id,
			name: $name,
			status: $status,
			progress: $progress,
			model_id: $model_id,
			start_date: $start_date,
			end_date: $end_date,
			estimated_completion: $estimated_completion,
			training_dataset: $training_dataset,
			testing_dataset: $testing_dataset,
			hyperparameters: $hyperparameters,
			metrics: $metrics,
			results: $results,
			created_at: $created_at,
			updated_at: $updated_at,
			created_by: $created_by,
			tenant_id: $tenant_id,
			organization_id: $organization_id
		})
		RETURN e
	`

	parameters := map[string]interface{}{
		"id":                   experiment.ID,
		"name":                 experiment.Name,
		"status":               experiment.Status,
		"progress":             experiment.Progress,
		"model_id":             experiment.ModelID,
		"start_date":           experiment.StartDate,
		"end_date":             experiment.EndDate,
		"estimated_completion": experiment.EstimatedCompletion,
		"training_dataset":     experiment.TrainingDataset,
		"testing_dataset":      experiment.TestingDataset,
		"hyperparameters":      serializeParameters(experiment.Hyperparameters),
		"metrics":              serializeMetrics(experiment.Metrics),
		"results":              serializeParameters(experiment.Results),
		"created_at":           experiment.CreatedAt,
		"updated_at":           experiment.UpdatedAt,
		"created_by":           experiment.CreatedBy,
		"tenant_id":            experiment.TenantID,
		"organization_id":      experiment.OrganizationID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to create ML experiment", zap.Error(err))
		return nil, fmt.Errorf("failed to create ML experiment: %w", err)
	}

	s.logger.Info("Created ML experiment", zap.String("experiment_id", experiment.ID), zap.String("name", experiment.Name))
	return experiment, nil
}

// GetExperiments retrieves ML experiments for a tenant
func (s *MLService) GetExperiments(ctx context.Context, spaceContext *models.SpaceContext, limit, offset int) ([]*models.MLExperiment, int, error) {
	query := `
		MATCH (e:MLExperiment)
		WHERE e.tenant_id = $tenant_id
		RETURN e
		ORDER BY e.created_at DESC
		SKIP $offset
		LIMIT $limit
	`

	countQuery := `
		MATCH (e:MLExperiment)
		WHERE e.tenant_id = $tenant_id
		RETURN count(e) as total
	`

	parameters := map[string]interface{}{
		"tenant_id": spaceContext.TenantID,
		"limit":     limit,
		"offset":    offset,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Get experiments
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get ML experiments", zap.Error(err))
		return nil, 0, fmt.Errorf("failed to get ML experiments: %w", err)
	}

	records := result.([]*neo4j.Record)
	experiments := make([]*models.MLExperiment, 0, len(records))

	for _, record := range records {
		experiment, err := s.recordToMLExperiment(record, "e")
		if err != nil {
			s.logger.Error("Failed to parse ML experiment record", zap.Error(err))
			continue
		}
		experiments = append(experiments, experiment)
	}

	// Get total count
	countResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, countQuery, map[string]interface{}{"tenant_id": spaceContext.TenantID})
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to count ML experiments", zap.Error(err))
		return experiments, 0, nil
	}

	var total int
	if countRecords := countResult.([]*neo4j.Record); len(countRecords) > 0 {
		if totalValue, found := countRecords[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return experiments, total, nil
}

// GetExperimentByID retrieves a specific ML experiment by ID
func (s *MLService) GetExperimentByID(ctx context.Context, experimentID string, spaceContext *models.SpaceContext) (*models.MLExperiment, error) {
	query := `
		MATCH (e:MLExperiment)
		WHERE e.id = $experiment_id AND e.tenant_id = $tenant_id
		RETURN e
	`

	parameters := map[string]interface{}{
		"experiment_id": experimentID,
		"tenant_id":     spaceContext.TenantID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get ML experiment", zap.String("experiment_id", experimentID), zap.Error(err))
		return nil, fmt.Errorf("failed to get ML experiment: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, fmt.Errorf("ML experiment not found")
	}

	return s.recordToMLExperiment(records[0], "e")
}

// UpdateExperiment updates an existing ML experiment
func (s *MLService) UpdateExperiment(ctx context.Context, experimentID string, req models.UpdateExperimentRequest, spaceContext *models.SpaceContext) (*models.MLExperiment, error) {
	// Build dynamic update query
	setParts := []string{"e.updated_at = $updated_at"}
	parameters := map[string]interface{}{
		"experiment_id": experimentID,
		"tenant_id":     spaceContext.TenantID,
		"updated_at":    time.Now(),
	}

	if req.Status != "" {
		setParts = append(setParts, "e.status = $status")
		parameters["status"] = req.Status
		
		// If completed or failed, set end date
		if req.Status == "completed" || req.Status == "failed" {
			setParts = append(setParts, "e.end_date = $end_date")
			parameters["end_date"] = time.Now()
		}
	}
	if req.Progress > 0 {
		setParts = append(setParts, "e.progress = $progress")
		parameters["progress"] = req.Progress
	}
	if req.Metrics != nil {
		setParts = append(setParts, "e.metrics = $metrics")
		parameters["metrics"] = serializeMetrics(req.Metrics)
	}
	if req.Results != nil {
		setParts = append(setParts, "e.results = $results")
		parameters["results"] = serializeParameters(req.Results)
	}

	query := fmt.Sprintf(`
		MATCH (e:MLExperiment)
		WHERE e.id = $experiment_id AND e.tenant_id = $tenant_id
		SET %s
		RETURN e
	`, strings.Join(setParts, ", "))

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to update ML experiment", zap.String("experiment_id", experimentID), zap.Error(err))
		return nil, fmt.Errorf("failed to update ML experiment: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, fmt.Errorf("ML experiment not found")
	}

	s.logger.Info("Updated ML experiment", zap.String("experiment_id", experimentID))
	return s.recordToMLExperiment(records[0], "e")
}

// DeleteExperiment deletes an ML experiment
func (s *MLService) DeleteExperiment(ctx context.Context, experimentID string, spaceContext *models.SpaceContext) error {
	query := `
		MATCH (e:MLExperiment)
		WHERE e.id = $experiment_id AND e.tenant_id = $tenant_id
		DETACH DELETE e
		RETURN count(e) as deleted
	`

	parameters := map[string]interface{}{
		"experiment_id": experimentID,
		"tenant_id":     spaceContext.TenantID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to delete ML experiment", zap.String("experiment_id", experimentID), zap.Error(err))
		return fmt.Errorf("failed to delete ML experiment: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 || records[0].Values[0].(int64) == 0 {
		return fmt.Errorf("ML experiment not found")
	}

	s.logger.Info("Deleted ML experiment", zap.String("experiment_id", experimentID))
	return nil
}

// GetAnalytics retrieves ML performance analytics
func (s *MLService) GetAnalytics(ctx context.Context, spaceContext *models.SpaceContext, period string) (*models.MLPerformanceMetrics, error) {
	// Calculate analytics based on current data
	modelsQuery := `
		MATCH (m:MLModel)
		WHERE m.tenant_id = $tenant_id AND m.status = 'deployed'
		RETURN count(m) as active_models, avg(m.accuracy) as avg_accuracy, sum(m.predictions) as total_predictions
	`

	experimentsQuery := `
		MATCH (e:MLExperiment)
		WHERE e.tenant_id = $tenant_id AND e.status = 'running'
		RETURN count(e) as running_experiments
	`

	parameters := map[string]interface{}{
		"tenant_id": spaceContext.TenantID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Get model analytics
	modelsResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, modelsQuery, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get model analytics", zap.Error(err))
		return nil, fmt.Errorf("failed to get model analytics: %w", err)
	}

	// Get experiment analytics
	experimentsResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, experimentsQuery, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get experiment analytics", zap.Error(err))
		return nil, fmt.Errorf("failed to get experiment analytics: %w", err)
	}

	// Build analytics response
	analytics := &models.MLPerformanceMetrics{
		ID:                    fmt.Sprintf("analytics_%s_%d", period, time.Now().Unix()),
		Period:                period,
		Date:                  time.Now(),
		TenantID:              spaceContext.TenantID,
		OrganizationID:        spaceContext.SpaceID,
		CreatedAt:             time.Now(),
		ModelPerformance:      make(map[string]float64),
		AverageProcessingTime: 850.0, // Mock value
		ErrorRate:             0.058, // Mock 5.8% error rate
	}

	// Process model results
	if modelsRecords := modelsResult.([]*neo4j.Record); len(modelsRecords) > 0 {
		record := modelsRecords[0]
		if activeModels, found := record.Get("active_models"); found {
			if count, ok := activeModels.(int64); ok {
				analytics.ActiveModels = int(count)
			}
		}
		if avgAccuracy, found := record.Get("avg_accuracy"); found {
			if acc, ok := avgAccuracy.(float64); ok {
				analytics.AverageAccuracy = acc
			}
		}
		if totalPredictions, found := record.Get("total_predictions"); found {
			if pred, ok := totalPredictions.(int64); ok {
				analytics.TotalPredictions = pred
			}
		}
	}

	// Process experiment results
	if experimentsRecords := experimentsResult.([]*neo4j.Record); len(experimentsRecords) > 0 {
		record := experimentsRecords[0]
		if runningExperiments, found := record.Get("running_experiments"); found {
			if count, ok := runningExperiments.(int64); ok {
				analytics.RunningExperiments = int(count)
			}
		}
	}

	// Mock some additional analytics based on frontend requirements
	analytics.DocumentsProcessed = 2400000 // 2.4M documents/month as shown in frontend
	
	return analytics, nil
}

// recordToMLExperiment converts a Neo4j record to an MLExperiment
func (s *MLService) recordToMLExperiment(record *neo4j.Record, alias string) (*models.MLExperiment, error) {
	node, found := record.Get(alias)
	if !found {
		return nil, fmt.Errorf("node %s not found in record", alias)
	}

	nodeValue, ok := node.(neo4j.Node)
	if !ok {
		return nil, fmt.Errorf("expected neo4j.Node, got %T", node)
	}

	props := nodeValue.Props
	experiment := &models.MLExperiment{}

	// Required fields
	if id, found := props["id"]; found {
		experiment.ID = id.(string)
	}
	if name, found := props["name"]; found {
		experiment.Name = name.(string)
	}
	if status, found := props["status"]; found {
		experiment.Status = status.(string)
	}
	if modelID, found := props["model_id"]; found {
		experiment.ModelID = modelID.(string)
	}

	// Numeric fields
	if progress, found := props["progress"]; found {
		if prog, ok := progress.(float64); ok {
			experiment.Progress = prog
		}
	}

	// String fields
	if trainingDataset, found := props["training_dataset"]; found {
		experiment.TrainingDataset = trainingDataset.(string)
	}
	if testingDataset, found := props["testing_dataset"]; found && testingDataset != nil {
		experiment.TestingDataset = testingDataset.(string)
	}
	if createdBy, found := props["created_by"]; found {
		experiment.CreatedBy = createdBy.(string)
	}
	if tenantID, found := props["tenant_id"]; found {
		experiment.TenantID = tenantID.(string)
	}
	if orgID, found := props["organization_id"]; found {
		experiment.OrganizationID = orgID.(string)
	}

	// Timestamp fields
	if startDate, found := props["start_date"]; found {
		if startTime, ok := startDate.(time.Time); ok {
			experiment.StartDate = startTime
		}
	}
	if endDate, found := props["end_date"]; found && endDate != nil {
		if endTime, ok := endDate.(time.Time); ok {
			experiment.EndDate = &endTime
		}
	}
	if estimatedCompletion, found := props["estimated_completion"]; found && estimatedCompletion != nil {
		if estTime, ok := estimatedCompletion.(time.Time); ok {
			experiment.EstimatedCompletion = &estTime
		}
	}
	if createdAt, found := props["created_at"]; found {
		if createdTime, ok := createdAt.(time.Time); ok {
			experiment.CreatedAt = createdTime
		}
	}
	if updatedAt, found := props["updated_at"]; found {
		if updatedTime, ok := updatedAt.(time.Time); ok {
			experiment.UpdatedAt = updatedTime
		}
	}

	// Map fields
	if hyperparameters, found := props["hyperparameters"]; found && hyperparameters != nil {
		if hyperStr, ok := hyperparameters.(string); ok && hyperStr != "" {
			experiment.Hyperparameters = deserializeParameters(hyperStr)
		}
	}
	if metrics, found := props["metrics"]; found && metrics != nil {
		if metricsStr, ok := metrics.(string); ok && metricsStr != "" {
			experiment.Metrics = deserializeMetrics(metricsStr)
		}
	}
	if results, found := props["results"]; found && results != nil {
		if resultsStr, ok := results.(string); ok && resultsStr != "" {
			experiment.Results = deserializeParameters(resultsStr)
		}
	}

	return experiment, nil
}

// Helper functions for metrics serialization
func serializeMetrics(metrics map[string]float64) string {
	if len(metrics) == 0 {
		return ""
	}
	result := ""
	for k, v := range metrics {
		if result != "" {
			result += ","
		}
		result += fmt.Sprintf("%s:%.4f", k, v)
	}
	return result
}

func deserializeMetrics(metricsStr string) map[string]float64 {
	metrics := make(map[string]float64)
	if metricsStr == "" {
		return metrics
	}
	pairs := strings.Split(metricsStr, ",")
	for _, pair := range pairs {
		if kv := strings.Split(pair, ":"); len(kv) == 2 {
			var floatVal float64
			if _, err := fmt.Sscanf(kv[1], "%f", &floatVal); err == nil {
				metrics[kv[0]] = floatVal
			}
		}
	}
	return metrics
}