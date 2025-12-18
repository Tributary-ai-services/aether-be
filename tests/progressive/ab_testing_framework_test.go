package progressive

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestABFramework provides test utilities for the A/B testing framework
func TestABFramework(t *testing.T) {
	framework := NewABTestingFramework()

	// Test experiment creation
	experiment := &Experiment{
		ID:          "chunking_strategy_test",
		Name:        "Chunking Strategy Comparison",
		Description: "Compare semantic vs adaptive chunking strategies",
		Duration:    7 * 24 * time.Hour, // 7 days
		Variants: []Variant{
			{
				ID:             "control",
				Name:           "Semantic Chunking",
				TrafficPercent: 50,
				Configuration: map[string]interface{}{
					"default_strategy": "semantic",
				},
				FeatureFlags: map[string]bool{
					"new_chunking_algorithm": false,
				},
			},
			{
				ID:             "treatment",
				Name:           "Adaptive Chunking",
				TrafficPercent: 50,
				Configuration: map[string]interface{}{
					"default_strategy": "adaptive",
				},
				FeatureFlags: map[string]bool{
					"new_chunking_algorithm": true,
				},
			},
		},
		Metrics: []MetricDefinition{
			{
				Name:               "chunk_quality_score",
				Type:               MetricTypeGauge,
				Target:             MetricTargetHigherIsBetter,
				MinimumImprovement: 5.0,
			},
			{
				Name:              "processing_time",
				Type:              MetricTypeHistogram,
				Target:            MetricTargetLowerIsBetter,
				MaximumDegradation: 10.0,
			},
		},
		Config: ExperimentConfig{
			MinimumSampleSize:    1000,
			ConfidenceLevel:      0.95,
			PowerLevel:           0.80,
			AllowEarlyTermination: true,
			MaxDuration:          14 * 24 * time.Hour,
		},
	}

	// Test experiment lifecycle
	err := framework.CreateExperiment(experiment)
	require.NoError(t, err)

	err = framework.StartExperiment(experiment.ID)
	require.NoError(t, err)

	// Test variant assignment
	variant, err := framework.GetVariantForUser(experiment.ID, "user123")
	require.NoError(t, err)
	assert.Contains(t, []string{"control", "treatment"}, variant.ID)

	// Test metric recording
	err = framework.RecordMetric(experiment.ID, variant.ID, "chunk_quality_score", 0.85)
	require.NoError(t, err)

	err = framework.RecordMetric(experiment.ID, variant.ID, "processing_time", 1200.0)
	require.NoError(t, err)

	// Test experiment stopping
	err = framework.StopExperiment(experiment.ID)
	require.NoError(t, err)

	// Test analysis
	results, err := framework.AnalyzeExperiment(experiment.ID)
	require.NoError(t, err)
	assert.NotNil(t, results)
	assert.NotEmpty(t, results.Conclusion)
}

func TestTrafficSplitter(t *testing.T) {
	splitter := NewTrafficSplitter()
	
	experiment := &Experiment{
		ID:          "test_experiment",
		Name:        "Test Experiment",
		Description: "Test traffic splitting",
		Variants: []Variant{
			{
				ID:             "control",
				Name:           "Control",
				TrafficPercent: 70,
			},
			{
				ID:             "treatment",
				Name:           "Treatment",
				TrafficPercent: 30,
			},
		},
	}
	
	splitter.ConfigureExperiment(experiment)
	
	// Test variant assignment consistency
	userID := "test_user_123"
	variant1, err := splitter.GetVariant(experiment.ID, userID)
	require.NoError(t, err)
	
	variant2, err := splitter.GetVariant(experiment.ID, userID)
	require.NoError(t, err)
	
	// Same user should always get same variant
	assert.Equal(t, variant1.ID, variant2.ID)
	assert.Contains(t, []string{"control", "treatment"}, variant1.ID)
}

func TestMetricsCollector(t *testing.T) {
	collector := NewMetricsCollector()
	
	experimentID := "test_experiment"
	variantID := "test_variant"
	metricName := "test_metric"
	
	// Test metric recording
	err := collector.RecordMetric(experimentID, variantID, metricName, 100.0)
	require.NoError(t, err)
	
	err = collector.RecordMetric(experimentID, variantID, metricName, 200.0)
	require.NoError(t, err)
	
	// Verify metrics are stored
	key := fmt.Sprintf("%s_%s", variantID, metricName)
	values := collector.data[experimentID][key]
	
	assert.Len(t, values, 2)
	assert.Equal(t, 100.0, values[0])
	assert.Equal(t, 200.0, values[1])
}

func TestExperimentValidation(t *testing.T) {
	framework := NewABTestingFramework()
	
	// Test invalid experiment - no ID
	invalidExperiment := &Experiment{
		Name: "Test Experiment",
	}
	
	err := framework.CreateExperiment(invalidExperiment)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "experiment ID is required")
	
	// Test invalid experiment - traffic percentages don't sum to 100
	invalidTrafficExperiment := &Experiment{
		ID:   "test_experiment",
		Name: "Test Experiment",
		Variants: []Variant{
			{ID: "control", TrafficPercent: 60},
			{ID: "treatment", TrafficPercent: 30}, // Total = 90, not 100
		},
		Metrics:  []MetricDefinition{{Name: "test_metric"}},
		Duration: time.Hour,
	}
	
	err = framework.CreateExperiment(invalidTrafficExperiment)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must sum to 100")
}

func TestExperimentStates(t *testing.T) {
	framework := NewABTestingFramework()
	
	experiment := &Experiment{
		ID:       "state_test",
		Name:     "State Test",
		Duration: time.Hour,
		Variants: []Variant{
			{ID: "control", TrafficPercent: 50},
			{ID: "treatment", TrafficPercent: 50},
		},
		Metrics: []MetricDefinition{{Name: "test_metric"}},
	}
	
	// Create experiment
	err := framework.CreateExperiment(experiment)
	require.NoError(t, err)
	assert.Equal(t, ExperimentStatusDraft, experiment.Status)
	
	// Start experiment
	err = framework.StartExperiment(experiment.ID)
	require.NoError(t, err)
	assert.Equal(t, ExperimentStatusRunning, experiment.Status)
	
	// Try to start already running experiment
	err = framework.StartExperiment(experiment.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in draft status")
	
	// Stop experiment
	err = framework.StopExperiment(experiment.ID)
	require.NoError(t, err)
	assert.Equal(t, ExperimentStatusStopped, experiment.Status)
}