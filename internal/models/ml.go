package models

import (
	"time"

	"github.com/google/uuid"
)

// MLModel represents a machine learning model in the system
type MLModel struct {
	ID              string                 `json:"id" neo4j:"id"`
	Name            string                 `json:"name" neo4j:"name"`
	Type            string                 `json:"type" neo4j:"type"` // Classification, NER, Sentiment, ComputerVision
	Status          string                 `json:"status" neo4j:"status"` // deployed, training, testing, inactive
	Version         string                 `json:"version" neo4j:"version"`
	Accuracy        float64                `json:"accuracy" neo4j:"accuracy"`
	TrainingData    string                 `json:"training_data" neo4j:"training_data"` // e.g., "45K documents"
	Predictions     int64                  `json:"predictions" neo4j:"predictions"`
	MediaTypes      []string               `json:"media_types" neo4j:"media_types"` // document, image, audio, video
	Description     string                 `json:"description" neo4j:"description"`
	Parameters      map[string]interface{} `json:"parameters" neo4j:"parameters"`
	CreatedAt       time.Time              `json:"created_at" neo4j:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at" neo4j:"updated_at"`
	CreatedBy       string                 `json:"created_by" neo4j:"created_by"`
	TenantID        string                 `json:"tenant_id" neo4j:"tenant_id"`
	OrganizationID  string                 `json:"organization_id" neo4j:"organization_id"`
}

// MLExperiment represents a machine learning experiment
type MLExperiment struct {
	ID                  string                 `json:"id" neo4j:"id"`
	Name                string                 `json:"name" neo4j:"name"`
	Status              string                 `json:"status" neo4j:"status"` // running, completed, failed, paused
	Progress            float64                `json:"progress" neo4j:"progress"` // 0-100%
	ModelID             string                 `json:"model_id" neo4j:"model_id"`
	StartDate           time.Time              `json:"start_date" neo4j:"start_date"`
	EndDate             *time.Time             `json:"end_date,omitempty" neo4j:"end_date"`
	EstimatedCompletion *time.Time             `json:"estimated_completion,omitempty" neo4j:"estimated_completion"`
	TrainingDataset     string                 `json:"training_dataset" neo4j:"training_dataset"`
	TestingDataset      string                 `json:"testing_dataset" neo4j:"testing_dataset"`
	Hyperparameters     map[string]interface{} `json:"hyperparameters" neo4j:"hyperparameters"`
	Metrics             map[string]float64     `json:"metrics" neo4j:"metrics"` // accuracy, precision, recall, f1_score
	Results             map[string]interface{} `json:"results" neo4j:"results"`
	CreatedAt           time.Time              `json:"created_at" neo4j:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at" neo4j:"updated_at"`
	CreatedBy           string                 `json:"created_by" neo4j:"created_by"`
	TenantID            string                 `json:"tenant_id" neo4j:"tenant_id"`
	OrganizationID      string                 `json:"organization_id" neo4j:"organization_id"`
}

// MLPerformanceMetrics represents system-wide ML performance analytics
type MLPerformanceMetrics struct {
	ID                    string             `json:"id" neo4j:"id"`
	Period                string             `json:"period" neo4j:"period"` // daily, weekly, monthly
	Date                  time.Time          `json:"date" neo4j:"date"`
	AverageAccuracy       float64            `json:"average_accuracy" neo4j:"average_accuracy"`
	TotalPredictions      int64              `json:"total_predictions" neo4j:"total_predictions"`
	DocumentsProcessed    int64              `json:"documents_processed" neo4j:"documents_processed"`
	ActiveModels          int                `json:"active_models" neo4j:"active_models"`
	RunningExperiments    int                `json:"running_experiments" neo4j:"running_experiments"`
	ModelPerformance      map[string]float64 `json:"model_performance" neo4j:"model_performance"`
	ErrorRate             float64            `json:"error_rate" neo4j:"error_rate"`
	AverageProcessingTime float64            `json:"average_processing_time" neo4j:"average_processing_time"` // milliseconds
	TenantID              string             `json:"tenant_id" neo4j:"tenant_id"`
	OrganizationID        string             `json:"organization_id" neo4j:"organization_id"`
	CreatedAt             time.Time          `json:"created_at" neo4j:"created_at"`
}

// CreateMLModelRequest represents the request to create a new ML model
type CreateMLModelRequest struct {
	Name         string                 `json:"name" binding:"required"`
	Type         string                 `json:"type" binding:"required,oneof=Classification NER Sentiment ComputerVision"`
	Version      string                 `json:"version" binding:"required"`
	MediaTypes   []string               `json:"media_types" binding:"required"`
	Description  string                 `json:"description"`
	Parameters   map[string]interface{} `json:"parameters"`
}

// UpdateMLModelRequest represents the request to update an ML model
type UpdateMLModelRequest struct {
	Name        string                 `json:"name"`
	Status      string                 `json:"status" binding:"omitempty,oneof=deployed training testing inactive"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// CreateExperimentRequest represents the request to create a new experiment
type CreateExperimentRequest struct {
	Name               string                 `json:"name" binding:"required"`
	ModelID            string                 `json:"model_id" binding:"required"`
	TrainingDataset    string                 `json:"training_dataset" binding:"required"`
	TestingDataset     string                 `json:"testing_dataset"`
	Hyperparameters    map[string]interface{} `json:"hyperparameters"`
	EstimatedDuration  int                    `json:"estimated_duration"` // minutes
}

// UpdateExperimentRequest represents the request to update an experiment
type UpdateExperimentRequest struct {
	Status   string             `json:"status" binding:"omitempty,oneof=running completed failed paused"`
	Progress float64            `json:"progress" binding:"omitempty,min=0,max=100"`
	Metrics  map[string]float64 `json:"metrics"`
	Results  map[string]interface{} `json:"results"`
}

// NewMLModel creates a new ML model with default values
func NewMLModel(req CreateMLModelRequest, userID, tenantID, orgID string) *MLModel {
	now := time.Now()
	return &MLModel{
		ID:             uuid.New().String(),
		Name:           req.Name,
		Type:           req.Type,
		Status:         "inactive",
		Version:        req.Version,
		Accuracy:       0.0,
		TrainingData:   "",
		Predictions:    0,
		MediaTypes:     req.MediaTypes,
		Description:    req.Description,
		Parameters:     req.Parameters,
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      userID,
		TenantID:       tenantID,
		OrganizationID: orgID,
	}
}

// NewMLExperiment creates a new ML experiment with default values
func NewMLExperiment(req CreateExperimentRequest, userID, tenantID, orgID string) *MLExperiment {
	now := time.Now()
	
	var estimatedCompletion *time.Time
	if req.EstimatedDuration > 0 {
		completion := now.Add(time.Duration(req.EstimatedDuration) * time.Minute)
		estimatedCompletion = &completion
	}

	return &MLExperiment{
		ID:                  uuid.New().String(),
		Name:                req.Name,
		Status:              "running",
		Progress:            0.0,
		ModelID:             req.ModelID,
		StartDate:           now,
		EstimatedCompletion: estimatedCompletion,
		TrainingDataset:     req.TrainingDataset,
		TestingDataset:      req.TestingDataset,
		Hyperparameters:     req.Hyperparameters,
		Metrics:             make(map[string]float64),
		Results:             make(map[string]interface{}),
		CreatedAt:           now,
		UpdatedAt:           now,
		CreatedBy:           userID,
		TenantID:            tenantID,
		OrganizationID:      orgID,
	}
}