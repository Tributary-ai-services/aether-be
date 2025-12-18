package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// MLService handles machine learning model and experiment operations
type MLService struct {
	neo4j  *database.Neo4jClient
	logger *logger.Logger
}

// NewMLService creates a new ML service
func NewMLService(neo4j *database.Neo4jClient, log *logger.Logger) *MLService {
	return &MLService{
		neo4j:  neo4j,
		logger: log.WithService("ml_service"),
	}
}

// CreateModel creates a new ML model
func (s *MLService) CreateModel(ctx context.Context, req models.CreateMLModelRequest, userID string, spaceContext *models.SpaceContext) (*models.MLModel, error) {
	model := models.NewMLModel(req, userID, spaceContext.TenantID, spaceContext.SpaceID)

	query := `
		CREATE (m:MLModel {
			id: $id,
			name: $name,
			type: $type,
			status: $status,
			version: $version,
			accuracy: $accuracy,
			training_data: $training_data,
			predictions: $predictions,
			media_types: $media_types,
			description: $description,
			parameters: $parameters,
			created_at: $created_at,
			updated_at: $updated_at,
			created_by: $created_by,
			tenant_id: $tenant_id,
			organization_id: $organization_id
		})
		RETURN m
	`

	parameters := map[string]interface{}{
		"id":              model.ID,
		"name":            model.Name,
		"type":            model.Type,
		"status":          model.Status,
		"version":         model.Version,
		"accuracy":        model.Accuracy,
		"training_data":   model.TrainingData,
		"predictions":     model.Predictions,
		"media_types":     serializeStringSlice(model.MediaTypes),
		"description":     model.Description,
		"parameters":      serializeParameters(model.Parameters),
		"created_at":      model.CreatedAt,
		"updated_at":      model.UpdatedAt,
		"created_by":      model.CreatedBy,
		"tenant_id":       model.TenantID,
		"organization_id": model.OrganizationID,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to create ML model", zap.Error(err))
		return nil, fmt.Errorf("failed to create ML model: %w", err)
	}

	s.logger.Info("Created ML model", zap.String("model_id", model.ID), zap.String("name", model.Name))
	return model, nil
}

// GetModels retrieves ML models for a tenant
func (s *MLService) GetModels(ctx context.Context, spaceContext *models.SpaceContext, limit, offset int) ([]*models.MLModel, int, error) {
	query := `
		MATCH (m:MLModel)
		WHERE m.tenant_id = $tenant_id
		RETURN m
		ORDER BY m.created_at DESC
		SKIP $offset
		LIMIT $limit
	`

	countQuery := `
		MATCH (m:MLModel)
		WHERE m.tenant_id = $tenant_id
		RETURN count(m) as total
	`

	parameters := map[string]interface{}{
		"tenant_id": spaceContext.TenantID,
		"limit":     limit,
		"offset":    offset,
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	// Get models
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, parameters)
		if err != nil {
			return nil, err
		}
		return result.Collect(ctx)
	})

	if err != nil {
		s.logger.Error("Failed to get ML models", zap.Error(err))
		return nil, 0, fmt.Errorf("failed to get ML models: %w", err)
	}

	records := result.([]*neo4j.Record)
	models := make([]*models.MLModel, 0, len(records))

	for _, record := range records {
		model, err := s.recordToMLModel(record, "m")
		if err != nil {
			s.logger.Error("Failed to parse ML model record", zap.Error(err))
			continue
		}
		models = append(models, model)
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
		s.logger.Error("Failed to count ML models", zap.Error(err))
		return models, 0, nil
	}

	var total int
	if countRecords := countResult.([]*neo4j.Record); len(countRecords) > 0 {
		if totalValue, found := countRecords[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}

	return models, total, nil
}

// GetModelByID retrieves a specific ML model by ID
func (s *MLService) GetModelByID(ctx context.Context, modelID string, spaceContext *models.SpaceContext) (*models.MLModel, error) {
	query := `
		MATCH (m:MLModel)
		WHERE m.id = $model_id AND m.tenant_id = $tenant_id
		RETURN m
	`

	parameters := map[string]interface{}{
		"model_id":  modelID,
		"tenant_id": spaceContext.TenantID,
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
		s.logger.Error("Failed to get ML model", zap.String("model_id", modelID), zap.Error(err))
		return nil, fmt.Errorf("failed to get ML model: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, fmt.Errorf("ML model not found")
	}

	return s.recordToMLModel(records[0], "m")
}

// UpdateModel updates an existing ML model
func (s *MLService) UpdateModel(ctx context.Context, modelID string, req models.UpdateMLModelRequest, spaceContext *models.SpaceContext) (*models.MLModel, error) {
	// Build dynamic update query
	setParts := []string{"m.updated_at = $updated_at"}
	parameters := map[string]interface{}{
		"model_id":   modelID,
		"tenant_id":  spaceContext.TenantID,
		"updated_at": time.Now(),
	}

	if req.Name != "" {
		setParts = append(setParts, "m.name = $name")
		parameters["name"] = req.Name
	}
	if req.Status != "" {
		setParts = append(setParts, "m.status = $status")
		parameters["status"] = req.Status
	}
	if req.Version != "" {
		setParts = append(setParts, "m.version = $version")
		parameters["version"] = req.Version
	}
	if req.Description != "" {
		setParts = append(setParts, "m.description = $description")
		parameters["description"] = req.Description
	}
	if req.Parameters != nil {
		setParts = append(setParts, "m.parameters = $parameters")
		parameters["parameters"] = serializeParameters(req.Parameters)
	}

	query := fmt.Sprintf(`
		MATCH (m:MLModel)
		WHERE m.id = $model_id AND m.tenant_id = $tenant_id
		SET %s
		RETURN m
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
		s.logger.Error("Failed to update ML model", zap.String("model_id", modelID), zap.Error(err))
		return nil, fmt.Errorf("failed to update ML model: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 {
		return nil, fmt.Errorf("ML model not found")
	}

	s.logger.Info("Updated ML model", zap.String("model_id", modelID))
	return s.recordToMLModel(records[0], "m")
}

// DeleteModel deletes an ML model
func (s *MLService) DeleteModel(ctx context.Context, modelID string, spaceContext *models.SpaceContext) error {
	query := `
		MATCH (m:MLModel)
		WHERE m.id = $model_id AND m.tenant_id = $tenant_id
		DETACH DELETE m
		RETURN count(m) as deleted
	`

	parameters := map[string]interface{}{
		"model_id":  modelID,
		"tenant_id": spaceContext.TenantID,
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
		s.logger.Error("Failed to delete ML model", zap.String("model_id", modelID), zap.Error(err))
		return fmt.Errorf("failed to delete ML model: %w", err)
	}

	records := result.([]*neo4j.Record)
	if len(records) == 0 || records[0].Values[0].(int64) == 0 {
		return fmt.Errorf("ML model not found")
	}

	s.logger.Info("Deleted ML model", zap.String("model_id", modelID))
	return nil
}

// DeployModel deploys an ML model to production
func (s *MLService) DeployModel(ctx context.Context, modelID string, spaceContext *models.SpaceContext) (*models.MLModel, error) {
	return s.UpdateModel(ctx, modelID, models.UpdateMLModelRequest{
		Status: "deployed",
	}, spaceContext)
}

// recordToMLModel converts a Neo4j record to an MLModel
func (s *MLService) recordToMLModel(record *neo4j.Record, alias string) (*models.MLModel, error) {
	node, found := record.Get(alias)
	if !found {
		return nil, fmt.Errorf("node %s not found in record", alias)
	}

	nodeValue, ok := node.(neo4j.Node)
	if !ok {
		return nil, fmt.Errorf("expected neo4j.Node, got %T", node)
	}

	props := nodeValue.Props
	model := &models.MLModel{}

	// Required fields
	if id, found := props["id"]; found {
		model.ID = id.(string)
	}
	if name, found := props["name"]; found {
		model.Name = name.(string)
	}
	if modelType, found := props["type"]; found {
		model.Type = modelType.(string)
	}
	if status, found := props["status"]; found {
		model.Status = status.(string)
	}
	if version, found := props["version"]; found {
		model.Version = version.(string)
	}

	// Numeric fields
	if accuracy, found := props["accuracy"]; found {
		if acc, ok := accuracy.(float64); ok {
			model.Accuracy = acc
		}
	}
	if predictions, found := props["predictions"]; found {
		if pred, ok := predictions.(int64); ok {
			model.Predictions = pred
		}
	}

	// String fields
	if trainingData, found := props["training_data"]; found && trainingData != nil {
		model.TrainingData = trainingData.(string)
	}
	if description, found := props["description"]; found && description != nil {
		model.Description = description.(string)
	}
	if createdBy, found := props["created_by"]; found {
		model.CreatedBy = createdBy.(string)
	}
	if tenantID, found := props["tenant_id"]; found {
		model.TenantID = tenantID.(string)
	}
	if orgID, found := props["organization_id"]; found {
		model.OrganizationID = orgID.(string)
	}

	// Array fields
	if mediaTypes, found := props["media_types"]; found && mediaTypes != nil {
		if mediaTypesStr, ok := mediaTypes.(string); ok && mediaTypesStr != "" {
			model.MediaTypes = strings.Split(mediaTypesStr, ",")
		}
	}

	// Timestamp fields
	if createdAt, found := props["created_at"]; found {
		if createdTime, ok := createdAt.(time.Time); ok {
			model.CreatedAt = createdTime
		}
	}
	if updatedAt, found := props["updated_at"]; found {
		if updatedTime, ok := updatedAt.(time.Time); ok {
			model.UpdatedAt = updatedTime
		}
	}

	// Parameters (JSON)
	if parameters, found := props["parameters"]; found && parameters != nil {
		if paramStr, ok := parameters.(string); ok && paramStr != "" {
			model.Parameters = deserializeParameters(paramStr)
		}
	}

	return model, nil
}

// Helper functions for serialization

func serializeParameters(params map[string]interface{}) string {
	if len(params) == 0 {
		return ""
	}
	// In a real implementation, you'd use JSON marshaling
	// For now, return a simple string representation
	result := ""
	for k, v := range params {
		if result != "" {
			result += ","
		}
		result += fmt.Sprintf("%s:%v", k, v)
	}
	return result
}

func deserializeParameters(paramStr string) map[string]interface{} {
	params := make(map[string]interface{})
	if paramStr == "" {
		return params
	}
	// In a real implementation, you'd use JSON unmarshaling
	// For now, parse the simple string representation
	pairs := strings.Split(paramStr, ",")
	for _, pair := range pairs {
		if kv := strings.Split(pair, ":"); len(kv) == 2 {
			params[kv[0]] = kv[1]
		}
	}
	return params
}