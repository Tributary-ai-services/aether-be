package services

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// CommentService handles comment-related business logic
type CommentService struct {
	neo4j           *database.Neo4jClient
	notebookService *NotebookService
	logger          *logger.Logger

	// In-memory pub/sub for SSE
	mu          sync.RWMutex
	subscribers map[string][]chan *models.CommentEvent // notebookID -> channels
}

// NewCommentService creates a new comment service
func NewCommentService(neo4j *database.Neo4jClient, notebookService *NotebookService, log *logger.Logger) *CommentService {
	return &CommentService{
		neo4j:           neo4j,
		notebookService: notebookService,
		logger:          log.WithService("comment_service"),
		subscribers:     make(map[string][]chan *models.CommentEvent),
	}
}

// CreateComment creates a new comment on a notebook or conversation
func (s *CommentService) CreateComment(ctx context.Context, notebookID string, req models.CreateCommentRequest, userID, tenantID string) (*models.CommentResponse, error) {
	commentID := uuid.New().String()
	now := time.Now()

	// Determine resource type and ID based on conversationId
	resourceID := notebookID
	resourceType := "notebook"
	if req.ConversationID != "" {
		resourceID = req.ConversationID
		resourceType = "conversation"
	}

	// Build Cypher query - match the appropriate target node
	var query string
	if resourceType == "conversation" {
		query = `
			MATCH (target:ChatConversation {id: $resource_id}), (u:User {id: $user_id})
			CREATE (c:Comment {
				id: $id,
				content: $content,
				author_id: $user_id,
				resource_id: $resource_id,
				resource_type: $resource_type,
				parent_id: $parent_id,
				tenant_id: $tenant_id,
				mentions: $mentions,
				edited: false,
				created_at: datetime($created_at),
				updated_at: datetime($created_at)
			})
			CREATE (c)-[:COMMENTED_ON]->(target)
			CREATE (c)-[:AUTHORED_BY]->(u)
			RETURN c.id, c.content, c.author_id, c.resource_id, c.resource_type,
			       c.parent_id, c.mentions, c.edited, c.created_at, c.updated_at,
			       u.username as author_username, u.full_name as author_full_name,
			       u.avatar_url as author_avatar_url
		`
	} else {
		query = `
			MATCH (target:Notebook {id: $resource_id}), (u:User {id: $user_id})
			CREATE (c:Comment {
				id: $id,
				content: $content,
				author_id: $user_id,
				resource_id: $resource_id,
				resource_type: $resource_type,
				parent_id: $parent_id,
				tenant_id: $tenant_id,
				mentions: $mentions,
				edited: false,
				created_at: datetime($created_at),
				updated_at: datetime($created_at)
			})
			CREATE (c)-[:COMMENTED_ON]->(target)
			CREATE (c)-[:AUTHORED_BY]->(u)
			RETURN c.id, c.content, c.author_id, c.resource_id, c.resource_type,
			       c.parent_id, c.mentions, c.edited, c.created_at, c.updated_at,
			       u.username as author_username, u.full_name as author_full_name,
			       u.avatar_url as author_avatar_url
		`
	}

	mentions := req.Mentions
	if mentions == nil {
		mentions = []string{}
	}

	params := map[string]interface{}{
		"id":            commentID,
		"resource_id":   resourceID,
		"resource_type": resourceType,
		"user_id":       userID,
		"content":       req.Content,
		"parent_id":     req.ParentID,
		"tenant_id":     tenantID,
		"mentions":      mentions,
		"created_at":    now.Format(time.RFC3339),
	}

	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return nil, errors.Database("Failed to create comment", err)
	}
	if len(result.Records) == 0 {
		return nil, errors.NotFound("Notebook or user not found")
	}

	// If reply, create REPLY_TO relationship
	if req.ParentID != "" {
		replyQuery := `
			MATCH (child:Comment {id: $child_id}), (parent:Comment {id: $parent_id})
			CREATE (child)-[:REPLY_TO]->(parent)
		`
		_, _ = s.neo4j.ExecuteQueryWithLogging(ctx, replyQuery, map[string]interface{}{
			"child_id":  commentID,
			"parent_id": req.ParentID,
		})
	}

	comment := s.recordToCommentResponse(result.Records[0])

	// Publish event
	s.publish(notebookID, &models.CommentEvent{Type: "created", Comment: comment})

	return comment, nil
}

// GetComments returns threaded comments for a notebook or conversation
func (s *CommentService) GetComments(ctx context.Context, notebookID, tenantID, conversationID string) (*models.CommentListResponse, error) {
	// Determine resource filter
	resourceID := notebookID
	resourceType := "notebook"
	if conversationID != "" {
		resourceID = conversationID
		resourceType = "conversation"
	}

	// Get top-level comments with author info
	query := `
		MATCH (c:Comment {resource_id: $resource_id, resource_type: $resource_type})
		WHERE c.parent_id IS NULL OR c.parent_id = ''
		MATCH (c)-[:AUTHORED_BY]->(author:User)
		OPTIONAL MATCH (reply:Comment)-[:REPLY_TO]->(c)
		OPTIONAL MATCH (reply)-[:AUTHORED_BY]->(replyAuthor:User)
		WITH c, author,
		     COLLECT(DISTINCT {
		       id: reply.id, content: reply.content, author_id: reply.author_id,
		       resource_id: reply.resource_id, resource_type: reply.resource_type,
		       parent_id: reply.parent_id, mentions: reply.mentions,
		       edited: reply.edited, created_at: reply.created_at, updated_at: reply.updated_at,
		       author_username: replyAuthor.username, author_full_name: replyAuthor.full_name,
		       author_avatar_url: replyAuthor.avatar_url
		     }) as replies
		RETURN c.id, c.content, c.author_id, c.resource_id, c.resource_type,
		       c.parent_id, c.mentions, c.edited, c.created_at, c.updated_at,
		       author.username as author_username, author.full_name as author_full_name,
		       author.avatar_url as author_avatar_url,
		       replies
		ORDER BY c.created_at DESC
	`
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"resource_id":   resourceID,
		"resource_type": resourceType,
	})
	if err != nil {
		return nil, errors.Database("Failed to get comments", err)
	}

	comments := make([]*models.CommentResponse, 0, len(result.Records))
	for _, record := range result.Records {
		comment := s.recordToCommentResponse(record)

		// Parse replies
		if repliesVal, ok := record.Get("replies"); ok && repliesVal != nil {
			if repliesList, ok := repliesVal.([]interface{}); ok {
				for _, r := range repliesList {
					if replyMap, ok := r.(map[string]interface{}); ok {
						// Skip nil reply entries (from OPTIONAL MATCH with no matches)
						if replyMap["id"] == nil {
							continue
						}
						reply := &models.CommentResponse{}
						if v, ok := replyMap["id"].(string); ok {
							reply.ID = v
						}
						if v, ok := replyMap["content"].(string); ok {
							reply.Content = v
						}
						if v, ok := replyMap["author_id"].(string); ok {
							reply.AuthorID = v
						}
						if v, ok := replyMap["resource_id"].(string); ok {
							reply.ResourceID = v
						}
						if v, ok := replyMap["resource_type"].(string); ok {
							reply.ResourceType = v
						}
						if v, ok := replyMap["parent_id"].(string); ok {
							reply.ParentID = v
						}
						if v, ok := replyMap["edited"].(bool); ok {
							reply.Edited = v
						}
						if v, ok := replyMap["created_at"].(time.Time); ok {
							reply.CreatedAt = v
						}
						if v, ok := replyMap["updated_at"].(time.Time); ok {
							reply.UpdatedAt = v
						}
						reply.Author = &models.PublicUserResponse{}
						if v, ok := replyMap["author_username"].(string); ok {
							reply.Author.Username = v
						}
						if v, ok := replyMap["author_full_name"].(string); ok {
							reply.Author.FullName = v
						}
						if v, ok := replyMap["author_avatar_url"].(string); ok {
							reply.Author.AvatarURL = v
						}
						comment.Replies = append(comment.Replies, reply)
					}
				}
			}
		}

		comments = append(comments, comment)
	}

	return &models.CommentListResponse{
		Comments: comments,
		Total:    len(comments),
	}, nil
}

// UpdateComment updates a comment (only by author)
func (s *CommentService) UpdateComment(ctx context.Context, notebookID, commentID string, req models.UpdateCommentRequest, userID string) (*models.CommentResponse, error) {
	mentions := req.Mentions
	if mentions == nil {
		mentions = []string{}
	}

	query := `
		MATCH (c:Comment {id: $comment_id, resource_id: $notebook_id, author_id: $user_id})
		MATCH (c)-[:AUTHORED_BY]->(author:User)
		SET c.content = $content, c.mentions = $mentions, c.edited = true,
		    c.edited_at = datetime(), c.updated_at = datetime()
		RETURN c.id, c.content, c.author_id, c.resource_id, c.resource_type,
		       c.parent_id, c.mentions, c.edited, c.created_at, c.updated_at,
		       author.username as author_username, author.full_name as author_full_name,
		       author.avatar_url as author_avatar_url
	`
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"comment_id":  commentID,
		"notebook_id": notebookID,
		"user_id":     userID,
		"content":     req.Content,
		"mentions":    mentions,
	})
	if err != nil {
		return nil, errors.Database("Failed to update comment", err)
	}
	if len(result.Records) == 0 {
		return nil, errors.NotFound("Comment not found or you don't have permission to edit it")
	}

	comment := s.recordToCommentResponse(result.Records[0])
	s.publish(notebookID, &models.CommentEvent{Type: "updated", Comment: comment})

	return comment, nil
}

// DeleteComment deletes a comment (by author or notebook owner)
func (s *CommentService) DeleteComment(ctx context.Context, notebookID, commentID, userID string, spaceCtx *models.SpaceContext) error {
	// Check if user is the comment author
	checkQuery := `
		MATCH (c:Comment {id: $comment_id, resource_id: $notebook_id})
		RETURN c.author_id
	`
	checkResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, checkQuery, map[string]interface{}{
		"comment_id":  commentID,
		"notebook_id": notebookID,
	})
	if err != nil {
		return errors.Database("Failed to check comment", err)
	}
	if len(checkResult.Records) == 0 {
		return errors.NotFound("Comment not found")
	}

	var authorID string
	if v, ok := checkResult.Records[0].Get("c.author_id"); ok && v != nil {
		authorID = v.(string)
	}

	// Allow deletion by author or notebook owner
	if authorID != userID {
		notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
		if err != nil || notebook.OwnerID != userID {
			return errors.Forbidden("Only comment author or notebook owner can delete comments")
		}
	}

	// Delete comment and its relationships
	query := `
		MATCH (c:Comment {id: $comment_id, resource_id: $notebook_id})
		OPTIONAL MATCH (reply:Comment)-[:REPLY_TO]->(c)
		DETACH DELETE reply, c
		RETURN count(c) as deleted
	`
	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"comment_id":  commentID,
		"notebook_id": notebookID,
	})
	if err != nil {
		return errors.Database("Failed to delete comment", err)
	}

	// Publish delete event
	s.publish(notebookID, &models.CommentEvent{
		Type: "deleted",
		Comment: &models.CommentResponse{
			ID:         commentID,
			ResourceID: notebookID,
		},
	})

	return nil
}

// Subscribe creates a new subscription channel for SSE
func (s *CommentService) Subscribe(notebookID string) chan *models.CommentEvent {
	ch := make(chan *models.CommentEvent, 10)
	s.mu.Lock()
	s.subscribers[notebookID] = append(s.subscribers[notebookID], ch)
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscription channel
func (s *CommentService) Unsubscribe(notebookID string, ch chan *models.CommentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs := s.subscribers[notebookID]
	for i, sub := range subs {
		if sub == ch {
			s.subscribers[notebookID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
	if len(s.subscribers[notebookID]) == 0 {
		delete(s.subscribers, notebookID)
	}
}

func (s *CommentService) publish(notebookID string, event *models.CommentEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.subscribers[notebookID] {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}

func (s *CommentService) recordToCommentResponse(record interface{ Get(string) (interface{}, bool) }) *models.CommentResponse {
	comment := &models.CommentResponse{}
	if v, ok := record.Get("c.id"); ok && v != nil {
		comment.ID = v.(string)
	}
	if v, ok := record.Get("c.content"); ok && v != nil {
		comment.Content = v.(string)
	}
	if v, ok := record.Get("c.author_id"); ok && v != nil {
		comment.AuthorID = v.(string)
	}
	if v, ok := record.Get("c.resource_id"); ok && v != nil {
		comment.ResourceID = v.(string)
	}
	if v, ok := record.Get("c.resource_type"); ok && v != nil {
		comment.ResourceType = v.(string)
	}
	if v, ok := record.Get("c.parent_id"); ok && v != nil {
		comment.ParentID = v.(string)
	}
	if v, ok := record.Get("c.edited"); ok && v != nil {
		if edited, ok := v.(bool); ok {
			comment.Edited = edited
		}
	}
	if v, ok := record.Get("c.created_at"); ok && v != nil {
		if t, ok := v.(time.Time); ok {
			comment.CreatedAt = t
		}
	}
	if v, ok := record.Get("c.updated_at"); ok && v != nil {
		if t, ok := v.(time.Time); ok {
			comment.UpdatedAt = t
		}
	}

	// Author info
	comment.Author = &models.PublicUserResponse{}
	if v, ok := record.Get("author_username"); ok && v != nil {
		comment.Author.Username = v.(string)
	}
	if v, ok := record.Get("author_full_name"); ok && v != nil {
		comment.Author.FullName = v.(string)
	}
	if v, ok := record.Get("author_avatar_url"); ok && v != nil {
		comment.Author.AvatarURL = v.(string)
	}
	comment.Author.ID = comment.AuthorID

	return comment
}
