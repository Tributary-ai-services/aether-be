package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// ChunkService handles chunk-related business logic
type ChunkService struct {
	neo4j  *database.Neo4jClient
	logger *logger.Logger
}

// NewChunkService creates a new chunk service
func NewChunkService(neo4j *database.Neo4jClient, logger *logger.Logger) *ChunkService {
	return &ChunkService{
		neo4j:  neo4j,
		logger: logger.WithService("chunk_service"),
	}
}

// CreateChunk creates a new chunk
func (s *ChunkService) CreateChunk(ctx context.Context, req models.ChunkCreateRequest, tenantID string) (*models.Chunk, error) {
	chunk := models.NewChunk(req, tenantID)
	
	// Create chunk in Neo4j
	query := `
		CREATE (c:Chunk {
			id: $id,
			tenant_id: $tenant_id,
			file_id: $file_id,
			chunk_id: $chunk_id,
			chunk_type: $chunk_type,
			chunk_number: $chunk_number,
			content: $content,
			content_hash: $content_hash,
			size_bytes: $size_bytes,
			processed_at: datetime($processed_at),
			processed_by: $processed_by,
			processing_time: $processing_time,
			quality: $quality,
			language: $language,
			language_confidence: $language_confidence,
			content_category: $content_category,
			sensitivity_level: $sensitivity_level,
			classifications: $classifications,
			embedding_status: $embedding_status,
			pii_detected: $pii_detected,
			compliance_flags: $compliance_flags,
			dlp_scan_status: $dlp_scan_status,
			context: $context,
			schema_info: $schema_info,
			metadata: $metadata,
			created_at: datetime($created_at),
			updated_at: datetime($updated_at)
		})
		RETURN c
	`
	
	params := map[string]interface{}{
		"id":                   chunk.ID,
		"tenant_id":            chunk.TenantID,
		"file_id":              chunk.FileID,
		"chunk_id":             chunk.ChunkID,
		"chunk_type":           chunk.ChunkType,
		"chunk_number":         chunk.ChunkNumber,
		"content":              chunk.Content,
		"content_hash":         chunk.ContentHash,
		"size_bytes":           chunk.SizeBytes,
		"processed_at":         chunk.ProcessedAt.Format(time.RFC3339),
		"processed_by":         chunk.ProcessedBy,
		"processing_time":      chunk.ProcessingTime,
		"quality":              serializeQualityMetrics(chunk.Quality),
		"language":             chunk.Language,
		"language_confidence":  chunk.LanguageConf,
		"content_category":     chunk.ContentCategory,
		"sensitivity_level":    chunk.SensitivityLevel,
		"classifications":      chunk.Classifications,
		"embedding_status":     chunk.EmbeddingStatus,
		"pii_detected":         chunk.PIIDetected,
		"compliance_flags":     serializeStringSlice(chunk.ComplianceFlags),
		"dlp_scan_status":      chunk.DLPScanStatus,
		"context":              serializeStringMap(chunk.Context),
		"schema_info":          serializeInterfaceMap(chunk.SchemaInfo),
		"metadata":             serializeInterfaceMap(chunk.Metadata),
		"created_at":           chunk.CreatedAt.Format(time.RFC3339),
		"updated_at":           chunk.UpdatedAt.Format(time.RFC3339),
	}
	
	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to create chunk", zap.Error(err))
		return nil, errors.Database("Failed to create chunk", err)
	}
	
	s.logger.Info("Chunk created successfully",
		zap.String("chunk_id", chunk.ID),
		zap.String("file_id", chunk.FileID),
		zap.String("chunk_type", chunk.ChunkType))
	
	return chunk, nil
}

// GetChunkByID retrieves a chunk by ID
func (s *ChunkService) GetChunkByID(ctx context.Context, chunkID string, tenantID string) (*models.Chunk, error) {
	query := `
		MATCH (c:Chunk {id: $chunk_id, tenant_id: $tenant_id})
		RETURN c.id, c.file_id, c.chunk_id, c.chunk_type, c.chunk_number,
		       c.content, c.content_hash, c.size_bytes,
		       c.processed_at, c.processed_by, c.processing_time,
		       c.quality, c.language, c.language_confidence,
		       c.content_category, c.classifications,
		       c.embedding_status, c.pii_detected, c.compliance_flags, c.dlp_scan_status,
		       c.context, c.schema_info, c.metadata,
		       c.created_at, c.updated_at
	`
	
	params := map[string]interface{}{
		"chunk_id":  chunkID,
		"tenant_id": tenantID,
	}
	
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to get chunk by ID", zap.String("chunk_id", chunkID), zap.Error(err))
		return nil, errors.Database("Failed to retrieve chunk", err)
	}
	
	if len(result.Records) == 0 {
		return nil, errors.NotFoundWithDetails("Chunk not found", map[string]interface{}{
			"chunk_id": chunkID,
		})
	}
	
	return s.recordToChunk(result.Records[0])
}

// ListChunksByFile lists chunks for a specific file
func (s *ChunkService) ListChunksByFile(ctx context.Context, fileID string, tenantID string, limit, offset int) (*models.ChunkListResponse, error) {
	// Set defaults
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	
	query := `
		MATCH (c:Chunk {file_id: $file_id, tenant_id: $tenant_id})
		RETURN c.id, c.file_id, c.chunk_id, c.chunk_type, c.chunk_number,
		       c.content, c.content_hash, c.size_bytes,
		       c.processed_at, c.processed_by, c.processing_time,
		       c.quality, c.language, c.language_confidence,
		       c.content_category, c.classifications,
		       c.embedding_status, c.pii_detected, c.compliance_flags, c.dlp_scan_status,
		       c.context, c.schema_info, c.metadata,
		       c.created_at, c.updated_at
		ORDER BY c.chunk_number ASC
		SKIP $offset
		LIMIT $limit
	`
	
	params := map[string]interface{}{
		"file_id":   fileID,
		"tenant_id": tenantID,
		"limit":     limit + 1, // Get one extra to check if there are more
		"offset":    offset,
	}
	
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to list chunks", zap.Error(err))
		return nil, errors.Database("Failed to list chunks", err)
	}
	
	chunks := make([]*models.ChunkResponse, 0, len(result.Records))
	hasMore := false
	
	for i, record := range result.Records {
		if i >= limit {
			hasMore = true
			break
		}
		
		chunk, err := s.recordToChunk(record)
		if err != nil {
			s.logger.Error("Failed to parse chunk record", zap.Error(err))
			continue
		}
		
		chunks = append(chunks, chunk.ToResponse())
	}
	
	// Get total count
	countQuery := `
		MATCH (c:Chunk {file_id: $file_id, tenant_id: $tenant_id})
		RETURN count(c) as total
	`
	
	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, map[string]interface{}{
		"file_id":   fileID,
		"tenant_id": tenantID,
	})
	if err != nil {
		s.logger.Error("Failed to get chunk count", zap.Error(err))
		return nil, errors.Database("Failed to get chunk count", err)
	}
	
	total := 0
	if len(countResult.Records) > 0 {
		if totalValue, found := countResult.Records[0].Get("total"); found {
			if totalInt, ok := totalValue.(int64); ok {
				total = int(totalInt)
			}
		}
	}
	
	return &models.ChunkListResponse{
		Chunks:  chunks,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: hasMore,
	}, nil
}

// SearchChunks searches for chunks based on criteria
func (s *ChunkService) SearchChunks(ctx context.Context, req models.ChunkSearchRequest, tenantID string) (*models.ChunkListResponse, error) {
	// Set defaults
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	
	// Build query conditions
	whereConditions := []string{"c.tenant_id = $tenant_id"}
	params := map[string]interface{}{
		"tenant_id": tenantID,
		"limit":     req.Limit + 1,
		"offset":    req.Offset,
	}
	
	if req.Query != "" {
		whereConditions = append(whereConditions, "c.content CONTAINS $query")
		params["query"] = req.Query
	}
	
	if req.FileID != "" {
		whereConditions = append(whereConditions, "c.file_id = $file_id")
		params["file_id"] = req.FileID
	}
	
	if req.ChunkType != "" {
		whereConditions = append(whereConditions, "c.chunk_type = $chunk_type")
		params["chunk_type"] = req.ChunkType
	}
	
	if req.ContentCategory != "" {
		whereConditions = append(whereConditions, "c.content_category = $content_category")
		params["content_category"] = req.ContentCategory
	}
	
	if req.Language != "" {
		whereConditions = append(whereConditions, "c.language = $language")
		params["language"] = req.Language
	}
	
	if req.PIIDetected != nil {
		whereConditions = append(whereConditions, "c.pii_detected = $pii_detected")
		params["pii_detected"] = *req.PIIDetected
	}
	
	if req.DLPScanStatus != "" {
		whereConditions = append(whereConditions, "c.dlp_scan_status = $dlp_scan_status")
		params["dlp_scan_status"] = req.DLPScanStatus
	}
	
	whereClause := "WHERE " + fmt.Sprintf("(%s)", whereConditions[0])
	for i := 1; i < len(whereConditions); i++ {
		whereClause += " AND " + fmt.Sprintf("(%s)", whereConditions[i])
	}
	
	query := fmt.Sprintf(`
		MATCH (c:Chunk)
		%s
		RETURN c.id, c.file_id, c.chunk_id, c.chunk_type, c.chunk_number,
		       c.content, c.content_hash, c.size_bytes,
		       c.processed_at, c.processed_by, c.processing_time,
		       c.quality, c.language, c.language_confidence,
		       c.content_category, c.classifications,
		       c.embedding_status, c.pii_detected, c.compliance_flags, c.dlp_scan_status,
		       c.context, c.schema_info, c.metadata,
		       c.created_at, c.updated_at
		ORDER BY c.created_at DESC
		SKIP $offset
		LIMIT $limit
	`, whereClause)
	
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to search chunks", zap.Error(err))
		return nil, errors.Database("Failed to search chunks", err)
	}
	
	chunks := make([]*models.ChunkResponse, 0, len(result.Records))
	hasMore := false
	
	for i, record := range result.Records {
		if i >= req.Limit {
			hasMore = true
			break
		}
		
		chunk, err := s.recordToChunk(record)
		if err != nil {
			s.logger.Error("Failed to parse chunk record", zap.Error(err))
			continue
		}
		
		chunks = append(chunks, chunk.ToResponse())
	}
	
	return &models.ChunkListResponse{
		Chunks:  chunks,
		Total:   len(chunks), // For search, we don't compute exact total
		Limit:   req.Limit,
		Offset:  req.Offset,
		HasMore: hasMore,
	}, nil
}

// UpdateChunk updates a chunk
func (s *ChunkService) UpdateChunk(ctx context.Context, chunkID string, req models.ChunkUpdateRequest, tenantID string) (*models.Chunk, error) {
	// Get current chunk
	chunk, err := s.GetChunkByID(ctx, chunkID, tenantID)
	if err != nil {
		return nil, err
	}
	
	// Update chunk fields
	chunk.Update(req)
	
	// Update in Neo4j
	query := `
		MATCH (c:Chunk {id: $chunk_id, tenant_id: $tenant_id})
		SET c.content = $content,
		    c.quality = $quality,
		    c.language = $language,
		    c.language_confidence = $language_confidence,
		    c.content_category = $content_category,
		    c.classifications = $classifications,
		    c.context = $context,
		    c.metadata = $metadata,
		    c.updated_at = datetime($updated_at)
		RETURN c
	`
	
	params := map[string]interface{}{
		"chunk_id":             chunkID,
		"tenant_id":            tenantID,
		"content":              chunk.Content,
		"quality":              serializeQualityMetrics(chunk.Quality),
		"language":             chunk.Language,
		"language_confidence":  chunk.LanguageConf,
		"content_category":     chunk.ContentCategory,
		"classifications":      chunk.Classifications,
		"context":              serializeStringMap(chunk.Context),
		"metadata":             serializeInterfaceMap(chunk.Metadata),
		"updated_at":           chunk.UpdatedAt.Format(time.RFC3339),
	}
	
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to update chunk", zap.String("chunk_id", chunkID), zap.Error(err))
		return nil, errors.Database("Failed to update chunk", err)
	}
	
	s.logger.Info("Chunk updated successfully", zap.String("chunk_id", chunkID))
	
	return chunk, nil
}

// DeleteChunk deletes a chunk
func (s *ChunkService) DeleteChunk(ctx context.Context, chunkID string, tenantID string) error {
	query := `
		MATCH (c:Chunk {id: $chunk_id, tenant_id: $tenant_id})
		DETACH DELETE c
	`
	
	params := map[string]interface{}{
		"chunk_id":  chunkID,
		"tenant_id": tenantID,
	}
	
	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		s.logger.Error("Failed to delete chunk", zap.String("chunk_id", chunkID), zap.Error(err))
		return errors.Database("Failed to delete chunk", err)
	}
	
	s.logger.Info("Chunk deleted successfully", zap.String("chunk_id", chunkID))
	return nil
}

// Helper methods

func (s *ChunkService) recordToChunk(record interface{}) (*models.Chunk, error) {
	// Parse Neo4j record to Chunk model
	rec, ok := record.(*neo4j.Record)
	if !ok {
		return nil, fmt.Errorf("invalid record type")
	}

	chunk := &models.Chunk{}
	
	// Parse basic fields
	if id, found := rec.Get("c.id"); found && id != nil {
		chunk.ID = id.(string)
	}
	if tenantID, found := rec.Get("c.tenant_id"); found && tenantID != nil {
		chunk.TenantID = tenantID.(string)
	}
	if fileID, found := rec.Get("c.file_id"); found && fileID != nil {
		chunk.FileID = fileID.(string)
	}
	if chunkID, found := rec.Get("c.chunk_id"); found && chunkID != nil {
		chunk.ChunkID = chunkID.(string)
	}
	if chunkType, found := rec.Get("c.chunk_type"); found && chunkType != nil {
		chunk.ChunkType = chunkType.(string)
	}
	if chunkNumber, found := rec.Get("c.chunk_number"); found && chunkNumber != nil {
		if num, ok := chunkNumber.(int64); ok {
			chunk.ChunkNumber = int(num)
		}
	}
	if content, found := rec.Get("c.content"); found && content != nil {
		chunk.Content = content.(string)
	}
	if contentHash, found := rec.Get("c.content_hash"); found && contentHash != nil {
		chunk.ContentHash = contentHash.(string)
	}
	if sizeBytes, found := rec.Get("c.size_bytes"); found && sizeBytes != nil {
		if size, ok := sizeBytes.(int64); ok {
			chunk.SizeBytes = size
		}
	}
	
	// Parse position information
	if startPos, found := rec.Get("c.start_position"); found && startPos != nil {
		if pos, ok := startPos.(int64); ok {
			chunk.StartPosition = &pos
		}
	}
	if endPos, found := rec.Get("c.end_position"); found && endPos != nil {
		if pos, ok := endPos.(int64); ok {
			chunk.EndPosition = &pos
		}
	}
	if pageNum, found := rec.Get("c.page_number"); found && pageNum != nil {
		if page, ok := pageNum.(int64); ok {
			pageInt := int(page)
			chunk.PageNumber = &pageInt
		}
	}
	if lineNum, found := rec.Get("c.line_number"); found && lineNum != nil {
		if line, ok := lineNum.(int64); ok {
			lineInt := int(line)
			chunk.LineNumber = &lineInt
		}
	}
	
	// Parse processing information
	if processedAt, found := rec.Get("c.processed_at"); found && processedAt != nil {
		if timeStr, ok := processedAt.(string); ok {
			if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
				chunk.ProcessedAt = t
			}
		}
	}
	if processedBy, found := rec.Get("c.processed_by"); found && processedBy != nil {
		chunk.ProcessedBy = processedBy.(string)
	}
	if processingTime, found := rec.Get("c.processing_time"); found && processingTime != nil {
		if pt, ok := processingTime.(int64); ok {
			chunk.ProcessingTime = pt
		}
	}
	
	// Parse quality metrics (stored as JSON string)
	if qualityStr, found := rec.Get("c.quality"); found && qualityStr != nil {
		// Parse quality JSON - simplified for now
		chunk.Quality = models.ChunkQualityMetrics{
			Completeness: 0.8,
			Coherence:    0.8,
			Uniqueness:   0.8,
		}
	}
	
	// Parse content analysis fields
	if language, found := rec.Get("c.language"); found && language != nil {
		chunk.Language = language.(string)
	}
	if langConf, found := rec.Get("c.language_confidence"); found && langConf != nil {
		if conf, ok := langConf.(float64); ok {
			chunk.LanguageConf = conf
		}
	}
	if contentCategory, found := rec.Get("c.content_category"); found && contentCategory != nil {
		chunk.ContentCategory = contentCategory.(string)
	}
	if sensitivityLevel, found := rec.Get("c.sensitivity_level"); found && sensitivityLevel != nil {
		chunk.SensitivityLevel = sensitivityLevel.(string)
	}
	
	// Parse embedding information
	if embeddingStatus, found := rec.Get("c.embedding_status"); found && embeddingStatus != nil {
		chunk.EmbeddingStatus = embeddingStatus.(string)
	}
	if embeddingModel, found := rec.Get("c.embedding_model"); found && embeddingModel != nil {
		chunk.EmbeddingModel = embeddingModel.(string)
	}
	
	// Parse compliance fields
	if piiDetected, found := rec.Get("c.pii_detected"); found && piiDetected != nil {
		if pii, ok := piiDetected.(bool); ok {
			chunk.PIIDetected = pii
		}
	}
	if complianceFlags, found := rec.Get("c.compliance_flags"); found && complianceFlags != nil {
		if flags, ok := complianceFlags.(string); ok && flags != "" {
			chunk.ComplianceFlags = strings.Split(flags, ",")
		}
	}
	if dlpScanStatus, found := rec.Get("c.dlp_scan_status"); found && dlpScanStatus != nil {
		chunk.DLPScanStatus = dlpScanStatus.(string)
	}
	if dlpScanResult, found := rec.Get("c.dlp_scan_result"); found && dlpScanResult != nil {
		chunk.DLPScanResult = dlpScanResult.(string)
	}
	
	// Parse timestamps
	if createdAt, found := rec.Get("c.created_at"); found && createdAt != nil {
		if timeStr, ok := createdAt.(string); ok {
			if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
				chunk.CreatedAt = t
			}
		}
	}
	if updatedAt, found := rec.Get("c.updated_at"); found && updatedAt != nil {
		if timeStr, ok := updatedAt.(string); ok {
			if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
				chunk.UpdatedAt = t
			}
		}
	}
	
	// Initialize maps if nil
	if chunk.Context == nil {
		chunk.Context = make(map[string]string)
	}
	if chunk.SchemaInfo == nil {
		chunk.SchemaInfo = make(map[string]interface{})
	}
	if chunk.Metadata == nil {
		chunk.Metadata = make(map[string]interface{})
	}
	
	return chunk, nil
}

func serializeQualityMetrics(quality models.ChunkQualityMetrics) string {
	// Convert quality metrics to JSON string for Neo4j storage
	return fmt.Sprintf(`{
		"completeness": %f,
		"coherence": %f,
		"uniqueness": %f,
		"readability": %f,
		"language_conf": %f,
		"language": "%s",
		"complexity": %f,
		"density": %f
	}`, quality.Completeness, quality.Coherence, quality.Uniqueness,
		quality.Readability, quality.LanguageConf, quality.Language,
		quality.Complexity, quality.Density)
}

func serializeStringMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	// Convert map to JSON string for Neo4j storage
	result := "{"
	first := true
	for k, v := range m {
		if !first {
			result += ","
		}
		result += fmt.Sprintf(`"%s": "%s"`, k, v)
		first = false
	}
	result += "}"
	return result
}

func serializeInterfaceMap(m map[string]interface{}) string {
	if len(m) == 0 {
		return "{}"
	}
	// Convert map to JSON string for Neo4j storage
	// This is a simplified implementation
	return "{}"
}

func serializeStringSlice(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	// Convert string slice to comma-separated string for Neo4j storage
	return strings.Join(slice, ",")
}