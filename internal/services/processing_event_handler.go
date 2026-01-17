package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// ProcessingCompleteEvent represents the event from audimodal when processing completes
type ProcessingCompleteEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Source    string    `json:"source"`
	TenantID  string    `json:"tenant_id"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
	Data      ProcessingCompleteData `json:"data"`
}

// ProcessingCompleteData contains the processing result data
type ProcessingCompleteData struct {
	FileID              string        `json:"file_id"`              // AudiModal file UUID
	URL                 string        `json:"url"`
	TotalProcessingTime time.Duration `json:"total_processing_time"`
	ChunksCreated       int           `json:"chunks_created"`
	EmbeddingsCreated   int           `json:"embeddings_created"`
	DLPViolationsFound  int           `json:"dlp_violations_found"`
	FinalDataClass      string        `json:"final_data_class"`
	StorageLocation     string        `json:"storage_location"`
	Success             bool          `json:"success"`
}

// ProcessingEventHandler handles processing-related events from Kafka
type ProcessingEventHandler struct {
	documentService *DocumentService
	kafkaService    *KafkaService
	logger          *logger.Logger
}

// NewProcessingEventHandler creates a new processing event handler
func NewProcessingEventHandler(documentService *DocumentService, kafkaService *KafkaService, log *logger.Logger) *ProcessingEventHandler {
	return &ProcessingEventHandler{
		documentService: documentService,
		kafkaService:    kafkaService,
		logger:          log.WithService("processing_event_handler"),
	}
}

// Start starts listening for processing events
func (h *ProcessingEventHandler) Start() error {
	topic := "processing.complete"
	groupID := "aether-be-processing-consumer"

	h.logger.Info("Starting processing event handler",
		zap.String("topic", topic),
		zap.String("group_id", groupID),
	)

	return h.kafkaService.Subscribe(topic, groupID, h.handleProcessingComplete)
}

// Stop stops the event handler
func (h *ProcessingEventHandler) Stop() error {
	return h.kafkaService.Unsubscribe("processing.complete", "aether-be-processing-consumer")
}

// handleProcessingComplete handles a processing.complete event
func (h *ProcessingEventHandler) handleProcessingComplete(ctx context.Context, message kafka.Message) error {
	var event ProcessingCompleteEvent
	if err := json.Unmarshal(message.Value, &event); err != nil {
		h.logger.Error("Failed to unmarshal processing complete event",
			zap.Error(err),
			zap.String("raw_value", string(message.Value)),
		)
		return err
	}

	h.logger.Info("Received processing complete event",
		zap.String("event_id", event.ID),
		zap.String("source", event.Source),
		zap.String("tenant_id", event.TenantID),
		zap.String("file_id", event.Data.FileID),
		zap.String("storage_location", event.Data.StorageLocation),
		zap.Int("chunks_created", event.Data.ChunksCreated),
		zap.Bool("success", event.Data.Success),
	)

	// First, try to find document by audimodal file ID (most reliable method)
	// This requires the processing_job_id to be set during document upload
	var documentID string
	if event.Data.FileID != "" {
		doc, err := h.documentService.FindDocumentByAudiModalFileID(ctx, event.Data.FileID, event.TenantID)
		if err != nil {
			h.logger.Warn("Error looking up document by audimodal file ID",
				zap.String("file_id", event.Data.FileID),
				zap.Error(err))
		} else if doc != nil {
			documentID = doc.ID
			h.logger.Info("Found document by audimodal file ID",
				zap.String("file_id", event.Data.FileID),
				zap.String("document_id", documentID))
		}
	}

	// Fallback: try to extract from path or find by URL/filename
	if documentID == "" {
		documentID = h.extractDocumentID(event.Data.URL, event.Data.StorageLocation)
	}

	if documentID == "" {
		h.logger.Warn("Could not extract document ID from event, trying URL lookup",
			zap.String("url", event.Data.URL),
			zap.String("storage_location", event.Data.StorageLocation),
		)
		// Try to find document by URL in Neo4j (includes filename fallback)
		doc, err := h.documentService.FindDocumentByURL(ctx, event.Data.URL, event.TenantID)
		if err != nil || doc == nil {
			h.logger.Error("Could not find document for processing event",
				zap.String("url", event.Data.URL),
				zap.String("file_id", event.Data.FileID),
				zap.Error(err),
			)
			return nil // Don't retry - document not found
		}
		documentID = doc.ID
	}

	// Determine status based on success
	status := "processed"
	errorMsg := ""
	if !event.Data.Success {
		status = "failed"
		errorMsg = "Processing failed in audimodal"
	}

	// Build result map
	result := map[string]interface{}{
		"audimodal_file_id":    event.Data.FileID, // Store AudiModal file ID for cross-service lookup
		"chunks_created":       event.Data.ChunksCreated,
		"embeddings_created":   event.Data.EmbeddingsCreated,
		"dlp_violations_found": event.Data.DLPViolationsFound,
		"final_data_class":     event.Data.FinalDataClass,
		"processing_time_ms":   event.Data.TotalProcessingTime.Milliseconds(),
	}

	// Update document in Neo4j
	err := h.documentService.UpdateProcessingResult(ctx, documentID, status, result, errorMsg)
	if err != nil {
		h.logger.Error("Failed to update document processing result",
			zap.String("document_id", documentID),
			zap.Error(err),
		)
		return err
	}

	h.logger.Info("Document processing result synced to Neo4j",
		zap.String("document_id", documentID),
		zap.String("status", status),
		zap.Int("chunks_created", event.Data.ChunksCreated),
	)

	return nil
}

// extractDocumentID attempts to extract document ID from URL or path
func (h *ProcessingEventHandler) extractDocumentID(url, storagePath string) string {
	// This is a simplified implementation
	// In practice, you may need to query the database to find the document
	// based on URL matching or path parsing
	return ""
}
