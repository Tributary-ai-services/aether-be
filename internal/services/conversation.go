package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// ConversationService handles conversation-related business logic
type ConversationService struct {
	neo4j           *database.Neo4jClient
	notebookService *NotebookService
	logger          *logger.Logger
}

// NewConversationService creates a new conversation service
func NewConversationService(neo4j *database.Neo4jClient, notebookService *NotebookService, log *logger.Logger) *ConversationService {
	return &ConversationService{
		neo4j:           neo4j,
		notebookService: notebookService,
		logger:          log.WithService("conversation_service"),
	}
}

// generateConversationName generates a name from the first message
func generateConversationName(message string) string {
	name := message
	if len(name) > 50 {
		name = name[:50]
	}
	suffix := time.Now().Format("Jan 2")
	return fmt.Sprintf("%s - %s", name, suffix)
}

// CreateConversation creates a new chat conversation for a notebook
func (s *ConversationService) CreateConversation(ctx context.Context, notebookID string, req models.CreateConversationRequest, userID string, spaceCtx *models.SpaceContext) (*models.ConversationResponse, error) {
	conversationID := uuid.New().String()
	now := time.Now()

	name := req.Name
	if name == "" {
		name = fmt.Sprintf("New Conversation - %s", now.Format("Jan 2"))
	}

	spaceID := ""
	spaceType := ""
	tenantID := ""
	if spaceCtx != nil {
		spaceID = spaceCtx.SpaceID
		spaceType = string(spaceCtx.SpaceType)
		tenantID = spaceCtx.TenantID
	}

	query := `
		MATCH (n:Notebook {id: $notebook_id}), (u:User {id: $user_id})
		CREATE (conv:ChatConversation {
			id: $id,
			name: $name,
			notebook_id: $notebook_id,
			user_id: $user_id,
			tenant_id: $tenant_id,
			space_id: $space_id,
			space_type: $space_type,
			message_count: 0,
			created_at: datetime($created_at),
			updated_at: datetime($created_at)
		})
		CREATE (conv)-[:BELONGS_TO]->(n)
		CREATE (conv)-[:CREATED_BY]->(u)
		RETURN conv.id, conv.name, conv.notebook_id, conv.user_id,
		       conv.message_count, conv.created_at, conv.updated_at
	`

	params := map[string]interface{}{
		"id":          conversationID,
		"name":        name,
		"notebook_id": notebookID,
		"user_id":     userID,
		"tenant_id":   tenantID,
		"space_id":    spaceID,
		"space_type":  spaceType,
		"created_at":  now.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return nil, errors.Database("Failed to create conversation", err)
	}
	if len(result.Records) == 0 {
		return nil, errors.NotFound("Notebook or user not found")
	}

	return s.recordToConversationResponse(result.Records[0]), nil
}

// ListConversations returns conversations for a notebook, ordered by updated_at DESC
func (s *ConversationService) ListConversations(ctx context.Context, notebookID, tenantID string) (*models.ConversationListResponse, error) {
	query := `
		MATCH (conv:ChatConversation {notebook_id: $notebook_id})
		OPTIONAL MATCH (msg:ChatMessage)-[:PART_OF]->(conv)
		WITH conv, msg
		ORDER BY msg.created_at DESC
		WITH conv, COLLECT(msg.content)[0] as last_message
		RETURN conv.id, conv.name, conv.notebook_id, conv.user_id,
		       conv.message_count, conv.created_at, conv.updated_at,
		       last_message
		ORDER BY conv.updated_at DESC
	`

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"notebook_id": notebookID,
	})
	if err != nil {
		return nil, errors.Database("Failed to list conversations", err)
	}

	conversations := make([]*models.ConversationResponse, 0, len(result.Records))
	for _, record := range result.Records {
		conv := s.recordToConversationResponse(record)
		if v, ok := record.Get("last_message"); ok && v != nil {
			conv.LastMessage = v.(string)
		}
		conversations = append(conversations, conv)
	}

	return &models.ConversationListResponse{
		Conversations: conversations,
		Total:         len(conversations),
	}, nil
}

// GetConversation returns a conversation with all its messages
func (s *ConversationService) GetConversation(ctx context.Context, conversationID, userID string) (*models.ConversationResponse, error) {
	query := `
		MATCH (conv:ChatConversation {id: $conversation_id})
		OPTIONAL MATCH (msg:ChatMessage)-[:PART_OF]->(conv)
		WITH conv, msg
		ORDER BY msg.created_at DESC
		WITH conv, COLLECT(msg.content)[0] as last_message
		RETURN conv.id, conv.name, conv.notebook_id, conv.user_id,
		       conv.message_count, conv.created_at, conv.updated_at,
		       last_message
	`

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"conversation_id": conversationID,
	})
	if err != nil {
		return nil, errors.Database("Failed to get conversation", err)
	}
	if len(result.Records) == 0 {
		return nil, errors.NotFound("Conversation not found")
	}

	conv := s.recordToConversationResponse(result.Records[0])
	if v, ok := result.Records[0].Get("last_message"); ok && v != nil {
		conv.LastMessage = v.(string)
	}
	return conv, nil
}

// UpdateConversation renames a conversation
func (s *ConversationService) UpdateConversation(ctx context.Context, conversationID string, req models.UpdateConversationRequest, userID string) (*models.ConversationResponse, error) {
	query := `
		MATCH (conv:ChatConversation {id: $conversation_id, user_id: $user_id})
		SET conv.name = $name, conv.updated_at = datetime()
		RETURN conv.id, conv.name, conv.notebook_id, conv.user_id,
		       conv.message_count, conv.created_at, conv.updated_at
	`

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"conversation_id": conversationID,
		"user_id":         userID,
		"name":            req.Name,
	})
	if err != nil {
		return nil, errors.Database("Failed to update conversation", err)
	}
	if len(result.Records) == 0 {
		return nil, errors.NotFound("Conversation not found or you don't have permission to edit it")
	}

	return s.recordToConversationResponse(result.Records[0]), nil
}

// DeleteConversation deletes a conversation and all its messages and associated comments
func (s *ConversationService) DeleteConversation(ctx context.Context, conversationID, userID string, spaceCtx *models.SpaceContext) error {
	// Verify ownership
	checkQuery := `
		MATCH (conv:ChatConversation {id: $conversation_id})
		RETURN conv.user_id, conv.notebook_id
	`
	checkResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, checkQuery, map[string]interface{}{
		"conversation_id": conversationID,
	})
	if err != nil {
		return errors.Database("Failed to check conversation", err)
	}
	if len(checkResult.Records) == 0 {
		return errors.NotFound("Conversation not found")
	}

	var ownerID string
	if v, ok := checkResult.Records[0].Get("conv.user_id"); ok && v != nil {
		ownerID = v.(string)
	}

	if ownerID != userID {
		var notebookID string
		if v, ok := checkResult.Records[0].Get("conv.notebook_id"); ok && v != nil {
			notebookID = v.(string)
		}
		notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
		if err != nil || notebook.OwnerID != userID {
			return errors.Forbidden("Only conversation owner or notebook owner can delete conversations")
		}
	}

	// Delete messages, associated comments, and the conversation
	query := `
		MATCH (conv:ChatConversation {id: $conversation_id})
		OPTIONAL MATCH (msg:ChatMessage)-[:PART_OF]->(conv)
		OPTIONAL MATCH (c:Comment {resource_id: $conversation_id, resource_type: 'conversation'})
		OPTIONAL MATCH (reply:Comment)-[:REPLY_TO]->(c)
		DETACH DELETE reply, c, msg, conv
		RETURN count(conv) as deleted
	`
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"conversation_id": conversationID,
	})
	if err != nil {
		return errors.Database("Failed to delete conversation", err)
	}

	return nil
}

// AddMessage adds a message to a conversation
func (s *ConversationService) AddMessage(ctx context.Context, conversationID, role, content string, isError bool, metadata map[string]interface{}) (*models.ChatMessageResponse, error) {
	messageID := uuid.New().String()
	now := time.Now()

	if metadata == nil {
		metadata = map[string]interface{}{}
	}

	query := `
		MATCH (conv:ChatConversation {id: $conversation_id})
		CREATE (msg:ChatMessage {
			id: $id,
			conversation_id: $conversation_id,
			role: $role,
			content: $content,
			is_error: $is_error,
			metadata: $metadata,
			created_at: datetime($created_at)
		})
		CREATE (msg)-[:PART_OF]->(conv)
		SET conv.message_count = conv.message_count + 1,
		    conv.updated_at = datetime($created_at)
		RETURN msg.id, msg.conversation_id, msg.role, msg.content,
		       msg.is_error, msg.created_at
	`

	params := map[string]interface{}{
		"id":              messageID,
		"conversation_id": conversationID,
		"role":            role,
		"content":         content,
		"is_error":        isError,
		"metadata":        fmt.Sprintf("%v", metadata), // Neo4j doesn't support nested maps well
		"created_at":      now.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return nil, errors.Database("Failed to add message", err)
	}
	if len(result.Records) == 0 {
		return nil, errors.NotFound("Conversation not found")
	}

	return s.recordToMessageResponse(result.Records[0]), nil
}

// GetMessages returns paginated messages for a conversation
func (s *ConversationService) GetMessages(ctx context.Context, conversationID string, limit, offset int) (*models.ChatMessageListResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Get total count
	countQuery := `
		MATCH (msg:ChatMessage {conversation_id: $conversation_id})
		RETURN count(msg) as total
	`
	countResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, countQuery, map[string]interface{}{
		"conversation_id": conversationID,
	})
	if err != nil {
		return nil, errors.Database("Failed to count messages", err)
	}

	total := 0
	if len(countResult.Records) > 0 {
		if v, ok := countResult.Records[0].Get("total"); ok && v != nil {
			total = int(v.(int64))
		}
	}

	// Get messages
	query := `
		MATCH (msg:ChatMessage {conversation_id: $conversation_id})
		RETURN msg.id, msg.conversation_id, msg.role, msg.content,
		       msg.is_error, msg.created_at
		ORDER BY msg.created_at ASC
		SKIP $offset
		LIMIT $limit
	`

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"conversation_id": conversationID,
		"offset":          int64(offset),
		"limit":           int64(limit),
	})
	if err != nil {
		return nil, errors.Database("Failed to get messages", err)
	}

	messages := make([]*models.ChatMessageResponse, 0, len(result.Records))
	for _, record := range result.Records {
		messages = append(messages, s.recordToMessageResponse(record))
	}

	return &models.ChatMessageListResponse{
		Messages: messages,
		Total:    total,
	}, nil
}

// GetAllMessages returns all messages for a conversation (for building LLM context)
func (s *ConversationService) GetAllMessages(ctx context.Context, conversationID string) ([]*models.ChatMessageResponse, error) {
	query := `
		MATCH (msg:ChatMessage {conversation_id: $conversation_id})
		RETURN msg.id, msg.conversation_id, msg.role, msg.content,
		       msg.is_error, msg.created_at
		ORDER BY msg.created_at ASC
	`

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"conversation_id": conversationID,
	})
	if err != nil {
		return nil, errors.Database("Failed to get messages", err)
	}

	messages := make([]*models.ChatMessageResponse, 0, len(result.Records))
	for _, record := range result.Records {
		messages = append(messages, s.recordToMessageResponse(record))
	}

	return messages, nil
}

// UpdateConversationName updates name from first message if still default
func (s *ConversationService) UpdateConversationNameFromMessage(ctx context.Context, conversationID, message string) error {
	name := generateConversationName(message)
	query := `
		MATCH (conv:ChatConversation {id: $conversation_id})
		WHERE conv.name STARTS WITH 'New Conversation'
		SET conv.name = $name
	`
	_, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"conversation_id": conversationID,
		"name":            name,
	})
	return err
}

func (s *ConversationService) recordToConversationResponse(record interface {
	Get(string) (interface{}, bool)
}) *models.ConversationResponse {
	conv := &models.ConversationResponse{}
	if v, ok := record.Get("conv.id"); ok && v != nil {
		conv.ID = v.(string)
	}
	if v, ok := record.Get("conv.name"); ok && v != nil {
		conv.Name = v.(string)
	}
	if v, ok := record.Get("conv.notebook_id"); ok && v != nil {
		conv.NotebookID = v.(string)
	}
	if v, ok := record.Get("conv.user_id"); ok && v != nil {
		conv.UserID = v.(string)
	}
	if v, ok := record.Get("conv.message_count"); ok && v != nil {
		if count, ok := v.(int64); ok {
			conv.MessageCount = int(count)
		}
	}
	if v, ok := record.Get("conv.created_at"); ok && v != nil {
		if t, ok := v.(time.Time); ok {
			conv.CreatedAt = t
		}
	}
	if v, ok := record.Get("conv.updated_at"); ok && v != nil {
		if t, ok := v.(time.Time); ok {
			conv.UpdatedAt = t
		}
	}
	return conv
}

func (s *ConversationService) recordToMessageResponse(record interface {
	Get(string) (interface{}, bool)
}) *models.ChatMessageResponse {
	msg := &models.ChatMessageResponse{}
	if v, ok := record.Get("msg.id"); ok && v != nil {
		msg.ID = v.(string)
	}
	if v, ok := record.Get("msg.conversation_id"); ok && v != nil {
		msg.ConversationID = v.(string)
	}
	if v, ok := record.Get("msg.role"); ok && v != nil {
		msg.Role = v.(string)
	}
	if v, ok := record.Get("msg.content"); ok && v != nil {
		msg.Content = v.(string)
	}
	if v, ok := record.Get("msg.is_error"); ok && v != nil {
		if isError, ok := v.(bool); ok {
			msg.IsError = isError
		}
	}
	if v, ok := record.Get("msg.created_at"); ok && v != nil {
		if t, ok := v.(time.Time); ok {
			msg.CreatedAt = t
		}
	}
	return msg
}
