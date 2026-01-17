package services

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
)

// ComplianceProcessor handles background compliance scanning
type ComplianceProcessor struct {
	complianceService *ComplianceService
	chunkService      ChunkServiceInterface
	config            *config.ComplianceConfig
	log               *logger.Logger
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	mu                sync.RWMutex
	isRunning         bool
}

// ComplianceReport represents compliance scanning statistics and findings
type ComplianceReport struct {
	TenantID             string                 `json:"tenant_id"`
	ReportDate           time.Time              `json:"report_date"`
	TotalChunksScanned   int                    `json:"total_chunks_scanned"`
	PIIDetectedCount     int                    `json:"pii_detected_count"`
	ComplianceViolations []ComplianceViolation  `json:"compliance_violations"`
	RiskDistribution     map[string]int         `json:"risk_distribution"`
	DataClassification   map[string]int         `json:"data_classification"`
	RequiredActions      []string               `json:"required_actions"`
	LastScanDate         time.Time              `json:"last_scan_date"`
	ScanDuration         time.Duration          `json:"scan_duration"`
	Metadata             map[string]interface{} `json:"metadata"`
}

// ComplianceViolation represents a specific compliance violation
type ComplianceViolation struct {
	ChunkID         string    `json:"chunk_id"`
	ViolationType   string    `json:"violation_type"`
	Severity        string    `json:"severity"`
	Description     string    `json:"description"`
	Regulation      string    `json:"regulation"`
	RequiredAction  string    `json:"required_action"`
	DetectedAt      time.Time `json:"detected_at"`
	Status          string    `json:"status"` // new, acknowledged, resolved
}

// ComplianceMetrics holds performance and operational metrics
type ComplianceMetrics struct {
	TotalScansPerformed    int64         `json:"total_scans_performed"`
	TotalPIIDetected       int64         `json:"total_pii_detected"`
	TotalViolations        int64         `json:"total_violations"`
	AverageScanTime        time.Duration `json:"average_scan_time"`
	LastScanTime           time.Time     `json:"last_scan_time"`
	ComplianceScore        float64       `json:"compliance_score"`
	ProcessorStatus        string        `json:"processor_status"`
	ErrorRate              float64       `json:"error_rate"`
}

// NewComplianceProcessor creates a new compliance processor
func NewComplianceProcessor(
	complianceService *ComplianceService,
	chunkService ChunkServiceInterface,
	config *config.ComplianceConfig,
	log *logger.Logger,
) *ComplianceProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	return &ComplianceProcessor{
		complianceService: complianceService,
		chunkService:      chunkService,
		config:            config,
		log:               log,
		ctx:               ctx,
		cancel:            cancel,
		isRunning:         false,
	}
}

// Start begins background compliance scanning
func (p *ComplianceProcessor) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isRunning {
		return nil
	}

	if !p.config.Enabled {
		p.log.Info("Compliance scanning is disabled")
		return nil
	}

	p.isRunning = true
	p.wg.Add(1)

	go p.processingLoop()

	p.log.Info("Compliance processor started",
		zap.Int("batch_size", p.config.BatchSize),
		zap.Int("scan_interval", p.config.ScanInterval),
		zap.Bool("gdpr_enabled", p.config.GDPREnabled),
		zap.Bool("hipaa_enabled", p.config.HIPAAEnabled),
	)

	return nil
}

// Stop gracefully stops the compliance processor
func (p *ComplianceProcessor) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isRunning {
		return nil
	}

	p.log.Info("Stopping compliance processor...")
	p.cancel()
	p.isRunning = false

	// Wait for processing to complete with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.log.Info("Compliance processor stopped successfully")
	case <-time.After(30 * time.Second):
		p.log.Warn("Compliance processor stop timeout")
	}

	return nil
}

// ProcessTenant scans all pending chunks for a specific tenant
func (p *ComplianceProcessor) ProcessTenant(ctx context.Context, tenantID string) (*ComplianceReport, error) {
	start := time.Now()

	// Get chunks that need compliance scanning
	chunks, err := p.chunkService.GetChunksByStatus(ctx, tenantID, "pending", p.config.BatchSize)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		return &ComplianceReport{
			TenantID:           tenantID,
			ReportDate:         time.Now(),
			TotalChunksScanned: 0,
			LastScanDate:       time.Now(),
			ScanDuration:       time.Since(start),
		}, nil
	}

	// Perform compliance scanning
	results, err := p.complianceService.BatchScanChunks(ctx, chunks)
	if err != nil {
		return nil, err
	}

	// Generate compliance report
	report := p.generateReport(tenantID, chunks, results, start)

	// Update chunk compliance status
	for i, result := range results {
		chunk := chunks[i]
		status := "completed"
		if len(result.ComplianceFlags) > 0 {
			status = "violations_detected"
		}

		if err := p.chunkService.UpdateChunkEmbeddingStatus(ctx, chunk.ID, status); err != nil {
			p.log.Error("Failed to update chunk compliance status",
				zap.String("chunk_id", chunk.ID),
				zap.Error(err),
			)
		}
	}

	p.log.Info("Compliance scanning completed for tenant",
		zap.String("tenant_id", tenantID),
		zap.Int("chunks_scanned", report.TotalChunksScanned),
		zap.Int("pii_detected", report.PIIDetectedCount),
		zap.Int("violations", len(report.ComplianceViolations)),
		zap.Duration("duration", report.ScanDuration),
	)

	return report, nil
}

// GenerateComplianceReport generates a comprehensive compliance report for a tenant
func (p *ComplianceProcessor) GenerateComplianceReport(ctx context.Context, tenantID string) (*ComplianceReport, error) {
	// This would typically query all chunks for the tenant and generate a comprehensive report
	// For now, we'll use the ProcessTenant method
	return p.ProcessTenant(ctx, tenantID)
}

// GetMetrics returns compliance processor metrics
func (p *ComplianceProcessor) GetMetrics() ComplianceMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// This is a basic implementation - in production you'd track these metrics
	return ComplianceMetrics{
		ProcessorStatus: map[bool]string{true: "running", false: "stopped"}[p.isRunning],
		LastScanTime:    time.Now(),
	}
}

// processingLoop runs the main compliance scanning loop
func (p *ComplianceProcessor) processingLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(time.Duration(p.config.ScanInterval) * time.Second)
	defer ticker.Stop()

	p.log.Info("Compliance scanning loop started")

	for {
		select {
		case <-p.ctx.Done():
			p.log.Info("Compliance scanning loop stopped")
			return

		case <-ticker.C:
			p.scanAllTenants()
		}
	}
}

// scanAllTenants scans compliance for all active tenants
func (p *ComplianceProcessor) scanAllTenants() {
	// Get list of active tenants (placeholder)
	tenants := p.getActiveTenants()

	for _, tenantID := range tenants {
		select {
		case <-p.ctx.Done():
			return
		default:
			if err := p.scanTenantWithRetry(tenantID); err != nil {
				p.log.Error("Failed to scan tenant compliance",
					zap.String("tenant_id", tenantID),
					zap.Error(err),
				)
			}
		}
	}
}

// scanTenantWithRetry scans a tenant with retry logic
func (p *ComplianceProcessor) scanTenantWithRetry(tenantID string) error {
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Minute)
	defer cancel()

	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		_, err := p.ProcessTenant(ctx, tenantID)
		if err == nil {
			return nil
		}

		lastErr = err
		p.log.Warn("Compliance scan attempt failed",
			zap.String("tenant_id", tenantID),
			zap.Int("attempt", attempt+1),
			zap.Error(err),
		)

		// Exponential backoff
		backoff := time.Duration(attempt+1) * time.Second
		select {
		case <-time.After(backoff):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return lastErr
}

// getActiveTenants returns a list of active tenant IDs
func (p *ComplianceProcessor) getActiveTenants() []string {
	// Placeholder - this should query the database for active tenants
	return []string{
		"9855e094-36a6-4d3a-a4f5-d77da4614439", // Example tenant
	}
}

// generateReport creates a compliance report from scan results
func (p *ComplianceProcessor) generateReport(
	tenantID string,
	chunks []*models.Chunk,
	results []*ComplianceResult,
	startTime time.Time,
) *ComplianceReport {
	report := &ComplianceReport{
		TenantID:             tenantID,
		ReportDate:           time.Now(),
		TotalChunksScanned:   len(chunks),
		PIIDetectedCount:     0,
		ComplianceViolations: []ComplianceViolation{},
		RiskDistribution:     make(map[string]int),
		DataClassification:   make(map[string]int),
		RequiredActions:      []string{},
		LastScanDate:         time.Now(),
		ScanDuration:         time.Since(startTime),
		Metadata:             make(map[string]interface{}),
	}

	// Analyze results
	actionSet := make(map[string]bool)
	
	for i, result := range results {
		chunk := chunks[i]

		// Count PII detections
		if result.PIIDetected {
			report.PIIDetectedCount++
		}

		// Track risk distribution
		report.RiskDistribution[result.RiskLevel]++

		// Track data classification
		report.DataClassification[result.DataClassification.Level]++

		// Collect compliance violations
		for _, flag := range result.ComplianceFlags {
			violation := ComplianceViolation{
				ChunkID:        chunk.ID,
				ViolationType:  flag,
				Severity:       p.mapFlagToSeverity(flag),
				Description:    p.getViolationDescription(flag),
				Regulation:     p.mapFlagToRegulation(flag),
				RequiredAction: p.mapFlagToAction(flag),
				DetectedAt:     result.ScanTimestamp,
				Status:         "new",
			}
			report.ComplianceViolations = append(report.ComplianceViolations, violation)
		}

		// Collect unique required actions
		for _, action := range result.RequiredActions {
			actionSet[action] = true
		}
	}

	// Convert action set to slice
	for action := range actionSet {
		report.RequiredActions = append(report.RequiredActions, action)
	}

	// Add metadata
	report.Metadata["scan_version"] = "1.0.0"
	report.Metadata["regulations_checked"] = []string{}
	if p.config.GDPREnabled {
		report.Metadata["regulations_checked"] = append(
			report.Metadata["regulations_checked"].([]string), "GDPR")
	}
	if p.config.HIPAAEnabled {
		report.Metadata["regulations_checked"] = append(
			report.Metadata["regulations_checked"].([]string), "HIPAA")
	}

	return report
}

// Helper methods for mapping flags to violation details
func (p *ComplianceProcessor) mapFlagToSeverity(flag string) string {
	switch flag {
	case "PII_DETECTED", "PHI_DETECTED", "FINANCIAL_DATA":
		return "high"
	case "GDPR_PERSONAL_DATA", "SENSITIVE_DATA":
		return "medium"
	default:
		return "low"
	}
}

func (p *ComplianceProcessor) getViolationDescription(flag string) string {
	descriptions := map[string]string{
		"PII_DETECTED":        "Personally Identifiable Information detected in content",
		"PHI_DETECTED":        "Protected Health Information detected",
		"GDPR_PERSONAL_DATA":  "GDPR-regulated personal data detected",
		"GDPR_SENSITIVE_DATA": "GDPR-regulated sensitive data detected",
		"FINANCIAL_DATA":      "Financial data detected requiring protection",
		"MEDICAL_DATA":        "Medical information detected",
		"HIPAA_IDENTIFIER":    "HIPAA-regulated health identifier detected",
	}
	
	if desc, exists := descriptions[flag]; exists {
		return desc
	}
	return "Compliance flag detected: " + flag
}

func (p *ComplianceProcessor) mapFlagToRegulation(flag string) string {
	switch {
	case strings.Contains(flag, "GDPR"):
		return "GDPR"
	case strings.Contains(flag, "HIPAA") || strings.Contains(flag, "PHI"):
		return "HIPAA"
	case strings.Contains(flag, "FINANCIAL"):
		return "PCI-DSS"
	default:
		return "General"
	}
}

func (p *ComplianceProcessor) mapFlagToAction(flag string) string {
	actions := map[string]string{
		"PII_DETECTED":        "Review and mask PII data",
		"PHI_DETECTED":        "Apply HIPAA safeguards",
		"GDPR_PERSONAL_DATA":  "Ensure GDPR compliance measures",
		"GDPR_SENSITIVE_DATA": "Apply enhanced GDPR protections",
		"FINANCIAL_DATA":      "Implement PCI-DSS controls",
		"MEDICAL_DATA":        "Apply healthcare data protections",
		"HIPAA_IDENTIFIER":    "Secure HIPAA identifiers",
	}
	
	if action, exists := actions[flag]; exists {
		return action
	}
	return "Review compliance requirements"
}