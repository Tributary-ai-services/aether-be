package progressive

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// ABTestingFramework provides A/B testing capabilities for progressive deployments
type ABTestingFramework struct {
	experiments map[string]*Experiment
	metrics     *MetricsCollector
	splitter    *TrafficSplitter
	mu          sync.RWMutex
}

// Experiment represents an A/B test configuration
type Experiment struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Status      ExperimentStatus       `json:"status"`
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time"`
	Duration    time.Duration          `json:"duration"`
	Variants    []Variant              `json:"variants"`
	Metrics     []MetricDefinition     `json:"metrics"`
	Config      ExperimentConfig       `json:"config"`
	Results     *ExperimentResults     `json:"results,omitempty"`
}

// ExperimentStatus represents the current status of an experiment
type ExperimentStatus string

const (
	ExperimentStatusDraft     ExperimentStatus = "draft"
	ExperimentStatusRunning   ExperimentStatus = "running"
	ExperimentStatusCompleted ExperimentStatus = "completed"
	ExperimentStatusStopped   ExperimentStatus = "stopped"
	ExperimentStatusAnalyzing ExperimentStatus = "analyzing"
)

// Variant represents a test variant in an A/B test
type Variant struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	TrafficPercent float64                `json:"traffic_percent"`
	Configuration  map[string]interface{} `json:"configuration"`
	FeatureFlags   map[string]bool        `json:"feature_flags"`
}

// MetricDefinition defines what to measure in an experiment
type MetricDefinition struct {
	Name                string      `json:"name"`
	Type                MetricType  `json:"type"`
	Target              MetricTarget `json:"target"`
	MinimumImprovement  float64     `json:"minimum_improvement"`
	MaximumDegradation  float64     `json:"maximum_degradation"`
	StatisticalSignificance float64 `json:"statistical_significance"`
}

// MetricType represents the type of metric
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeRate      MetricType = "rate"
)

// MetricTarget represents the desired direction for a metric
type MetricTarget string

const (
	MetricTargetHigherIsBetter MetricTarget = "higher_is_better"
	MetricTargetLowerIsBetter  MetricTarget = "lower_is_better"
	MetricTargetNoChange       MetricTarget = "no_change"
)

// ExperimentConfig contains experiment configuration
type ExperimentConfig struct {
	MinimumSampleSize    int     `json:"minimum_sample_size"`
	ConfidenceLevel      float64 `json:"confidence_level"`
	PowerLevel           float64 `json:"power_level"`
	AllowEarlyTermination bool    `json:"allow_early_termination"`
	MaxDuration          time.Duration `json:"max_duration"`
}

// ExperimentResults contains the results of an experiment
type ExperimentResults struct {
	TotalSamples     int                      `json:"total_samples"`
	VariantResults   map[string]VariantResult `json:"variant_results"`
	StatisticalTests map[string]StatResult    `json:"statistical_tests"`
	Conclusion       string                   `json:"conclusion"`
	Recommendation   string                   `json:"recommendation"`
	EffectSize       float64                  `json:"effect_size"`
	Significance     bool                     `json:"significance"`
}

// VariantResult contains results for a specific variant
type VariantResult struct {
	VariantID      string             `json:"variant_id"`
	SampleSize     int                `json:"sample_size"`
	Metrics        map[string]float64 `json:"metrics"`
	ConversionRate float64            `json:"conversion_rate"`
	ConfidenceInterval ConfidenceInterval `json:"confidence_interval"`
}

// ConfidenceInterval represents a confidence interval
type ConfidenceInterval struct {
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
	Level float64 `json:"level"`
}

// StatResult contains statistical test results
type StatResult struct {
	TestType   string  `json:"test_type"`
	PValue     float64 `json:"p_value"`
	ZScore     float64 `json:"z_score"`
	EffectSize float64 `json:"effect_size"`
	Significant bool   `json:"significant"`
}

// TrafficSplitter handles traffic allocation for experiments
type TrafficSplitter struct {
	experiments map[string]*Experiment
	mu          sync.RWMutex
}

// MetricsCollector collects and aggregates experiment metrics
type MetricsCollector struct {
	data map[string]map[string][]float64 // experiment -> variant -> metrics
	mu   sync.RWMutex
}

// NewABTestingFramework creates a new A/B testing framework
func NewABTestingFramework() *ABTestingFramework {
	return &ABTestingFramework{
		experiments: make(map[string]*Experiment),
		metrics:     NewMetricsCollector(),
		splitter:    NewTrafficSplitter(),
	}
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		data: make(map[string]map[string][]float64),
	}
}

// NewTrafficSplitter creates a new traffic splitter
func NewTrafficSplitter() *TrafficSplitter {
	return &TrafficSplitter{
		experiments: make(map[string]*Experiment),
	}
}

// CreateExperiment creates a new A/B test experiment
func (ab *ABTestingFramework) CreateExperiment(experiment *Experiment) error {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	// Validate experiment configuration
	if err := ab.validateExperiment(experiment); err != nil {
		return fmt.Errorf("invalid experiment configuration: %w", err)
	}

	// Set initial status
	experiment.Status = ExperimentStatusDraft
	experiment.StartTime = time.Now()
	experiment.EndTime = experiment.StartTime.Add(experiment.Duration)

	// Store experiment
	ab.experiments[experiment.ID] = experiment

	return nil
}

// StartExperiment starts an A/B test experiment
func (ab *ABTestingFramework) StartExperiment(experimentID string) error {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	experiment, exists := ab.experiments[experimentID]
	if !exists {
		return fmt.Errorf("experiment not found: %s", experimentID)
	}

	if experiment.Status != ExperimentStatusDraft {
		return fmt.Errorf("experiment not in draft status: %s", experiment.Status)
	}

	// Start the experiment
	experiment.Status = ExperimentStatusRunning
	experiment.StartTime = time.Now()
	experiment.EndTime = experiment.StartTime.Add(experiment.Duration)

	// Configure traffic splitting
	ab.splitter.ConfigureExperiment(experiment)

	return nil
}

// StopExperiment stops a running experiment
func (ab *ABTestingFramework) StopExperiment(experimentID string) error {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	experiment, exists := ab.experiments[experimentID]
	if !exists {
		return fmt.Errorf("experiment not found: %s", experimentID)
	}

	if experiment.Status != ExperimentStatusRunning {
		return fmt.Errorf("experiment not running: %s", experiment.Status)
	}

	// Stop the experiment
	experiment.Status = ExperimentStatusStopped
	experiment.EndTime = time.Now()

	return nil
}

// GetVariantForUser determines which variant a user should see
func (ab *ABTestingFramework) GetVariantForUser(experimentID, userID string) (*Variant, error) {
	ab.mu.RLock()
	defer ab.mu.RUnlock()

	experiment, exists := ab.experiments[experimentID]
	if !exists {
		return nil, fmt.Errorf("experiment not found: %s", experimentID)
	}

	if experiment.Status != ExperimentStatusRunning {
		return nil, fmt.Errorf("experiment not running: %s", experiment.Status)
	}

	return ab.splitter.GetVariant(experimentID, userID)
}

// RecordMetric records a metric value for an experiment variant
func (ab *ABTestingFramework) RecordMetric(experimentID, variantID, metricName string, value float64) error {
	return ab.metrics.RecordMetric(experimentID, variantID, metricName, value)
}

// AnalyzeExperiment analyzes the results of an experiment
func (ab *ABTestingFramework) AnalyzeExperiment(experimentID string) (*ExperimentResults, error) {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	experiment, exists := ab.experiments[experimentID]
	if !exists {
		return nil, fmt.Errorf("experiment not found: %s", experimentID)
	}

	// Set status to analyzing
	experiment.Status = ExperimentStatusAnalyzing

	// Collect metrics data
	results := &ExperimentResults{
		VariantResults:   make(map[string]VariantResult),
		StatisticalTests: make(map[string]StatResult),
	}

	// Analyze each variant
	for _, variant := range experiment.Variants {
		variantResult, err := ab.analyzeVariant(experimentID, variant.ID, experiment.Metrics)
		if err != nil {
			return nil, fmt.Errorf("failed to analyze variant %s: %w", variant.ID, err)
		}
		results.VariantResults[variant.ID] = *variantResult
		results.TotalSamples += variantResult.SampleSize
	}

	// Perform statistical tests
	ab.performStatisticalTests(results, experiment.Metrics)

	// Generate conclusion and recommendation
	ab.generateConclusion(results, experiment)

	// Store results
	experiment.Results = results
	experiment.Status = ExperimentStatusCompleted

	return results, nil
}

// validateExperiment validates experiment configuration
func (ab *ABTestingFramework) validateExperiment(experiment *Experiment) error {
	if experiment.ID == "" {
		return fmt.Errorf("experiment ID is required")
	}

	if experiment.Name == "" {
		return fmt.Errorf("experiment name is required")
	}

	if len(experiment.Variants) < 2 {
		return fmt.Errorf("at least 2 variants are required")
	}

	// Validate traffic percentages sum to 100
	totalTraffic := 0.0
	for _, variant := range experiment.Variants {
		totalTraffic += variant.TrafficPercent
	}
	if totalTraffic != 100.0 {
		return fmt.Errorf("variant traffic percentages must sum to 100, got %f", totalTraffic)
	}

	if len(experiment.Metrics) == 0 {
		return fmt.Errorf("at least one metric is required")
	}

	if experiment.Duration <= 0 {
		return fmt.Errorf("experiment duration must be positive")
	}

	return nil
}

// ConfigureExperiment configures traffic splitting for an experiment
func (ts *TrafficSplitter) ConfigureExperiment(experiment *Experiment) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.experiments[experiment.ID] = experiment
}

// GetVariant determines which variant a user should see
func (ts *TrafficSplitter) GetVariant(experimentID, userID string) (*Variant, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	experiment, exists := ts.experiments[experimentID]
	if !exists {
		return nil, fmt.Errorf("experiment not configured: %s", experimentID)
	}

	// Use consistent hashing to ensure user always gets same variant
	hash := ts.hashUser(userID, experimentID)
	threshold := 0.0

	for _, variant := range experiment.Variants {
		threshold += variant.TrafficPercent / 100.0
		if hash <= threshold {
			return &variant, nil
		}
	}

	// Fallback to first variant
	return &experiment.Variants[0], nil
}

// hashUser creates a consistent hash for user assignment
func (ts *TrafficSplitter) hashUser(userID, experimentID string) float64 {
	// Simple hash function for demonstration
	// In production, use a proper consistent hashing algorithm
	combined := userID + experimentID
	rand.Seed(int64(len(combined)))
	return rand.Float64()
}

// RecordMetric records a metric value
func (mc *MetricsCollector) RecordMetric(experimentID, variantID, metricName string, value float64) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.data[experimentID] == nil {
		mc.data[experimentID] = make(map[string][]float64)
	}

	key := fmt.Sprintf("%s_%s", variantID, metricName)
	mc.data[experimentID][key] = append(mc.data[experimentID][key], value)

	return nil
}

// analyzeVariant analyzes metrics for a specific variant
func (ab *ABTestingFramework) analyzeVariant(experimentID, variantID string, metrics []MetricDefinition) (*VariantResult, error) {
	result := &VariantResult{
		VariantID: variantID,
		Metrics:   make(map[string]float64),
	}

	for _, metric := range metrics {
		key := fmt.Sprintf("%s_%s", variantID, metric.Name)
		values := ab.metrics.data[experimentID][key]
		
		if len(values) == 0 {
			continue
		}

		result.SampleSize = len(values)

		// Calculate metric statistics
		switch metric.Type {
		case MetricTypeCounter:
			result.Metrics[metric.Name] = ab.sum(values)
		case MetricTypeRate:
			result.Metrics[metric.Name] = ab.mean(values)
		case MetricTypeHistogram, MetricTypeGauge:
			result.Metrics[metric.Name] = ab.mean(values)
		}
	}

	return result, nil
}

// performStatisticalTests performs statistical significance tests
func (ab *ABTestingFramework) performStatisticalTests(results *ExperimentResults, metrics []MetricDefinition) {
	// For each metric, compare variants
	for _, metric := range metrics {
		// Get control variant (first variant)
		var controlVariant, treatmentVariant *VariantResult
		for _, result := range results.VariantResults {
			if controlVariant == nil {
				controlVariant = &result
			} else {
				treatmentVariant = &result
				break
			}
		}

		if controlVariant == nil || treatmentVariant == nil {
			continue
		}

		// Perform t-test (simplified)
		controlValue := controlVariant.Metrics[metric.Name]
		treatmentValue := treatmentVariant.Metrics[metric.Name]
		
		// Calculate effect size
		effectSize := ab.calculateEffectSize(controlValue, treatmentValue)
		
		// Calculate statistical significance (simplified)
		zScore := ab.calculateZScore(controlValue, treatmentValue, controlVariant.SampleSize, treatmentVariant.SampleSize)
		pValue := ab.calculatePValue(zScore)
		
		results.StatisticalTests[metric.Name] = StatResult{
			TestType:   "t-test",
			PValue:     pValue,
			ZScore:     zScore,
			EffectSize: effectSize,
			Significant: pValue < 0.05,
		}
	}
}

// generateConclusion generates experiment conclusion and recommendation
func (ab *ABTestingFramework) generateConclusion(results *ExperimentResults, experiment *Experiment) {
	significantResults := 0
	positiveResults := 0

	for _, test := range results.StatisticalTests {
		if test.Significant {
			significantResults++
			if test.EffectSize > 0 {
				positiveResults++
			}
		}
	}

	if significantResults == 0 {
		results.Conclusion = "No statistically significant differences found between variants"
		results.Recommendation = "Continue with current implementation or extend experiment duration"
	} else if positiveResults > 0 {
		results.Conclusion = fmt.Sprintf("Found %d statistically significant improvements", positiveResults)
		results.Recommendation = "Implement the treatment variant"
		results.Significance = true
	} else {
		results.Conclusion = "Treatment variant shows negative impact"
		results.Recommendation = "Continue with control variant"
	}
}

// Helper mathematical functions
func (ab *ABTestingFramework) sum(values []float64) float64 {
	total := 0.0
	for _, v := range values {
		total += v
	}
	return total
}

func (ab *ABTestingFramework) mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return ab.sum(values) / float64(len(values))
}

func (ab *ABTestingFramework) calculateEffectSize(control, treatment float64) float64 {
	if control == 0 {
		return 0
	}
	return (treatment - control) / control
}

func (ab *ABTestingFramework) calculateZScore(control, treatment float64, controlN, treatmentN int) float64 {
	// Simplified z-score calculation
	// In production, use proper statistical libraries
	if controlN == 0 || treatmentN == 0 {
		return 0
	}
	pooledStd := 1.0 // Simplified assumption
	return (treatment - control) / pooledStd
}

func (ab *ABTestingFramework) calculatePValue(zScore float64) float64 {
	// Simplified p-value calculation
	// In production, use proper statistical libraries
	return 0.05 // Placeholder
}

