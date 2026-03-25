package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// InvitationService handles invitation-related business logic
type InvitationService struct {
	neo4j           *database.Neo4jClient
	notebookService *NotebookService
	userService     *UserService
	emailService    *EmailService
	logger          *logger.Logger
}

// NewInvitationService creates a new invitation service
func NewInvitationService(
	neo4j *database.Neo4jClient,
	notebookService *NotebookService,
	userService *UserService,
	emailService *EmailService,
	log *logger.Logger,
) *InvitationService {
	return &InvitationService{
		neo4j:           neo4j,
		notebookService: notebookService,
		userService:     userService,
		emailService:    emailService,
		logger:          log.WithService("invitation_service"),
	}
}

// CreateInvitation creates an invitation to share a notebook
func (s *InvitationService) CreateInvitation(ctx context.Context, notebookID string, req models.CreateInvitationRequest, inviterID string, spaceCtx *models.SpaceContext) (*models.InvitationResponse, error) {
	// Normalize permission (view->read, edit->write)
	req.Permission = models.NormalizePermission(req.Permission)

	// Get notebook and verify ownership
	notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, inviterID, spaceCtx)
	if err != nil {
		return nil, err
	}
	if notebook.OwnerID != inviterID {
		return nil, errors.Forbidden("Only notebook owner can send invitations")
	}

	// Check if invitee already has an account
	existingUser := s.findUserByEmail(ctx, req.Email)
	if existingUser != nil {
		// User exists - create SHARED_WITH directly
		if err := s.notebookService.createSharingRelationship(ctx, notebookID, existingUser.ID, "", req.Permission, inviterID); err != nil {
			return nil, err
		}

		// Get inviter info for email
		inviterName := s.getInviterName(ctx, inviterID)

		// Send share notification email
		if s.emailService != nil {
			_ = s.emailService.SendShareNotification(req.Email, inviterName, notebook.Name, req.Permission)
		}

		return &models.InvitationResponse{
			ID:           uuid.New().String(),
			InviterID:    inviterID,
			InviterName:  inviterName,
			InviteeEmail: req.Email,
			ResourceID:   notebookID,
			ResourceType: "notebook",
			ResourceName: notebook.Name,
			Permission:   req.Permission,
			Status:       "accepted",
			CreatedAt:    time.Now(),
			AcceptedAt:   time.Now(),
		}, nil
	}

	// User doesn't exist - create invitation node
	token, err := generateSecureToken()
	if err != nil {
		return nil, errors.Internal("Failed to generate invitation token")
	}

	invitationID := uuid.New().String()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	query := `
		CREATE (inv:Invitation {
			id: $id,
			type: 'notebook_share',
			inviter_id: $inviter_id,
			invitee_email: $invitee_email,
			resource_id: $resource_id,
			resource_type: 'notebook',
			permission: $permission,
			token: $token,
			status: 'pending',
			message: $message,
			tenant_id: $tenant_id,
			space_id: $space_id,
			expires_at: datetime($expires_at),
			created_at: datetime()
		})
		WITH inv
		MATCH (u:User {id: $inviter_id})
		CREATE (u)-[:INVITED]->(inv)
		WITH inv
		MATCH (n:Notebook {id: $resource_id})
		CREATE (inv)-[:FOR_RESOURCE]->(n)
		RETURN inv.id
	`
	params := map[string]interface{}{
		"id":            invitationID,
		"inviter_id":    inviterID,
		"invitee_email": req.Email,
		"resource_id":   notebookID,
		"permission":    req.Permission,
		"token":         token,
		"message":       req.Message,
		"tenant_id":     spaceCtx.TenantID,
		"space_id":      spaceCtx.SpaceID,
		"expires_at":    expiresAt.Format(time.RFC3339),
	}

	_, err = s.neo4j.ExecuteQueryWithLogging(ctx, query, params)
	if err != nil {
		return nil, errors.Database("Failed to create invitation", err)
	}

	// Send invitation email
	inviterName := s.getInviterName(ctx, inviterID)
	if s.emailService != nil {
		_ = s.emailService.SendInvitationEmail(req.Email, inviterName, notebook.Name, req.Permission, req.Message, token)
	}

	s.logger.Info("Invitation created",
		zap.String("invitation_id", invitationID),
		zap.String("invitee_email", req.Email),
		zap.String("notebook_id", notebookID),
	)

	return &models.InvitationResponse{
		ID:           invitationID,
		InviterID:    inviterID,
		InviterName:  inviterName,
		InviteeEmail: req.Email,
		ResourceID:   notebookID,
		ResourceType: "notebook",
		ResourceName: notebook.Name,
		Permission:   req.Permission,
		Status:       "pending",
		Message:      req.Message,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now(),
	}, nil
}

// AcceptInvitation accepts an invitation by token
func (s *InvitationService) AcceptInvitation(ctx context.Context, token, userID string) (*models.InvitationResponse, error) {
	query := `
		MATCH (inv:Invitation {token: $token, status: 'pending'})
		WHERE inv.expires_at > datetime()
		SET inv.status = 'accepted', inv.accepted_at = datetime()
		RETURN inv.id, inv.inviter_id, inv.invitee_email, inv.resource_id,
		       inv.resource_type, inv.permission, inv.message, inv.expires_at, inv.created_at
	`
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"token": token,
	})
	if err != nil {
		return nil, errors.Database("Failed to accept invitation", err)
	}
	if len(result.Records) == 0 {
		return nil, errors.NotFound("Invitation not found, expired, or already accepted")
	}

	record := result.Records[0]
	var resourceID, permission string
	if v, ok := record.Get("inv.resource_id"); ok && v != nil {
		resourceID = v.(string)
	}
	if v, ok := record.Get("inv.permission"); ok && v != nil {
		permission = v.(string)
	}

	var inviterID string
	if v, ok := record.Get("inv.inviter_id"); ok && v != nil {
		inviterID = v.(string)
	}

	// Create the SHARED_WITH relationship
	if err := s.notebookService.createSharingRelationship(ctx, resourceID, userID, "", permission, inviterID); err != nil {
		s.logger.Error("Failed to create sharing from accepted invitation", zap.Error(err))
		return nil, err
	}

	resp := &models.InvitationResponse{
		ResourceID:   resourceID,
		ResourceType: "notebook",
		Permission:   permission,
		Status:       "accepted",
		AcceptedAt:   time.Now(),
	}
	if v, ok := record.Get("inv.id"); ok && v != nil {
		resp.ID = v.(string)
	}
	if v, ok := record.Get("inv.invitee_email"); ok && v != nil {
		resp.InviteeEmail = v.(string)
	}
	resp.InviterID = inviterID

	s.logger.Info("Invitation accepted",
		zap.String("invitation_id", resp.ID),
		zap.String("user_id", userID),
		zap.String("resource_id", resourceID),
	)

	return resp, nil
}

// CancelInvitation cancels a pending invitation
func (s *InvitationService) CancelInvitation(ctx context.Context, notebookID, invitationID, userID string, spaceCtx *models.SpaceContext) error {
	// Verify user owns the notebook
	notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
	if err != nil {
		return err
	}
	if notebook.OwnerID != userID {
		return errors.Forbidden("Only notebook owner can cancel invitations")
	}

	query := `
		MATCH (inv:Invitation {id: $invitation_id, resource_id: $notebook_id, status: 'pending'})
		SET inv.status = 'cancelled'
		RETURN inv.id
	`
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"invitation_id": invitationID,
		"notebook_id":   notebookID,
	})
	if err != nil {
		return errors.Database("Failed to cancel invitation", err)
	}
	if len(result.Records) == 0 {
		return errors.NotFound("Invitation not found or already processed")
	}

	return nil
}

// GetInvitationsForNotebook returns pending invitations for a notebook
func (s *InvitationService) GetInvitationsForNotebook(ctx context.Context, notebookID, userID string, spaceCtx *models.SpaceContext) (*models.InvitationListResponse, error) {
	// Verify user has access
	notebook, err := s.notebookService.GetNotebookByID(ctx, notebookID, userID, spaceCtx)
	if err != nil {
		return nil, err
	}
	if notebook.OwnerID != userID {
		return nil, errors.Forbidden("Only notebook owner can view invitations")
	}

	query := `
		MATCH (inv:Invitation {resource_id: $notebook_id})
		WHERE inv.status IN ['pending']
		OPTIONAL MATCH (inviter:User {id: inv.inviter_id})
		RETURN inv.id, inv.inviter_id, inv.invitee_email, inv.resource_id,
		       inv.resource_type, inv.permission, inv.status, inv.message,
		       inv.expires_at, inv.created_at,
		       inviter.full_name as inviter_name
		ORDER BY inv.created_at DESC
	`
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"notebook_id": notebookID,
	})
	if err != nil {
		return nil, errors.Database("Failed to get invitations", err)
	}

	invitations := make([]*models.InvitationResponse, 0, len(result.Records))
	for _, record := range result.Records {
		inv := &models.InvitationResponse{
			ResourceType: "notebook",
			ResourceName: notebook.Name,
		}
		if v, ok := record.Get("inv.id"); ok && v != nil {
			inv.ID = v.(string)
		}
		if v, ok := record.Get("inv.inviter_id"); ok && v != nil {
			inv.InviterID = v.(string)
		}
		if v, ok := record.Get("inv.invitee_email"); ok && v != nil {
			inv.InviteeEmail = v.(string)
		}
		if v, ok := record.Get("inv.resource_id"); ok && v != nil {
			inv.ResourceID = v.(string)
		}
		if v, ok := record.Get("inv.permission"); ok && v != nil {
			inv.Permission = v.(string)
		}
		if v, ok := record.Get("inv.status"); ok && v != nil {
			inv.Status = v.(string)
		}
		if v, ok := record.Get("inv.message"); ok && v != nil {
			inv.Message = v.(string)
		}
		if v, ok := record.Get("inviter_name"); ok && v != nil {
			inv.InviterName = v.(string)
		}
		if v, ok := record.Get("inv.expires_at"); ok && v != nil {
			if t, ok := v.(time.Time); ok {
				inv.ExpiresAt = t
			}
		}
		if v, ok := record.Get("inv.created_at"); ok && v != nil {
			if t, ok := v.(time.Time); ok {
				inv.CreatedAt = t
			}
		}
		invitations = append(invitations, inv)
	}

	return &models.InvitationListResponse{
		Invitations: invitations,
		Total:       len(invitations),
	}, nil
}

// GetPendingInvitations returns pending invitations for the current user's email
func (s *InvitationService) GetPendingInvitations(ctx context.Context, userID string) (*models.InvitationListResponse, error) {
	// Get user's email
	userQuery := `MATCH (u:User {id: $user_id}) RETURN u.email`
	userResult, err := s.neo4j.ExecuteQueryWithLogging(ctx, userQuery, map[string]interface{}{
		"user_id": userID,
	})
	if err != nil || len(userResult.Records) == 0 {
		return &models.InvitationListResponse{Invitations: []*models.InvitationResponse{}, Total: 0}, nil
	}

	var email string
	if v, ok := userResult.Records[0].Get("u.email"); ok && v != nil {
		email = v.(string)
	}
	if email == "" {
		return &models.InvitationListResponse{Invitations: []*models.InvitationResponse{}, Total: 0}, nil
	}

	query := `
		MATCH (inv:Invitation {invitee_email: $email, status: 'pending'})
		WHERE inv.expires_at > datetime()
		OPTIONAL MATCH (inviter:User {id: inv.inviter_id})
		OPTIONAL MATCH (inv)-[:FOR_RESOURCE]->(n:Notebook)
		RETURN inv.id, inv.inviter_id, inv.invitee_email, inv.resource_id,
		       inv.resource_type, inv.permission, inv.status, inv.message,
		       inv.expires_at, inv.created_at,
		       inviter.full_name as inviter_name,
		       n.name as resource_name
		ORDER BY inv.created_at DESC
	`
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"email": email,
	})
	if err != nil {
		return nil, errors.Database("Failed to get pending invitations", err)
	}

	invitations := make([]*models.InvitationResponse, 0, len(result.Records))
	for _, record := range result.Records {
		inv := &models.InvitationResponse{}
		if v, ok := record.Get("inv.id"); ok && v != nil {
			inv.ID = v.(string)
		}
		if v, ok := record.Get("inv.inviter_id"); ok && v != nil {
			inv.InviterID = v.(string)
		}
		if v, ok := record.Get("inv.invitee_email"); ok && v != nil {
			inv.InviteeEmail = v.(string)
		}
		if v, ok := record.Get("inv.resource_id"); ok && v != nil {
			inv.ResourceID = v.(string)
		}
		if v, ok := record.Get("inv.resource_type"); ok && v != nil {
			inv.ResourceType = v.(string)
		}
		if v, ok := record.Get("inv.permission"); ok && v != nil {
			inv.Permission = v.(string)
		}
		if v, ok := record.Get("inv.status"); ok && v != nil {
			inv.Status = v.(string)
		}
		if v, ok := record.Get("inv.message"); ok && v != nil {
			inv.Message = v.(string)
		}
		if v, ok := record.Get("inviter_name"); ok && v != nil {
			inv.InviterName = v.(string)
		}
		if v, ok := record.Get("resource_name"); ok && v != nil {
			inv.ResourceName = v.(string)
		}
		invitations = append(invitations, inv)
	}

	return &models.InvitationListResponse{
		Invitations: invitations,
		Total:       len(invitations),
	}, nil
}

// ProcessPendingInvitations auto-accepts pending invitations for a newly registered user
func (s *InvitationService) ProcessPendingInvitations(ctx context.Context, userID, email string) {
	query := `
		MATCH (inv:Invitation {invitee_email: $email, status: 'pending'})
		WHERE inv.expires_at > datetime()
		SET inv.status = 'accepted', inv.accepted_at = datetime()
		RETURN inv.id, inv.resource_id, inv.permission, inv.inviter_id
	`
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"email": email,
	})
	if err != nil {
		s.logger.Error("Failed to process pending invitations", zap.String("email", email), zap.Error(err))
		return
	}

	for _, record := range result.Records {
		var resourceID, permission, inviterID string
		if v, ok := record.Get("inv.resource_id"); ok && v != nil {
			resourceID = v.(string)
		}
		if v, ok := record.Get("inv.permission"); ok && v != nil {
			permission = v.(string)
		}
		if v, ok := record.Get("inv.inviter_id"); ok && v != nil {
			inviterID = v.(string)
		}

		if err := s.notebookService.createSharingRelationship(ctx, resourceID, userID, "", permission, inviterID); err != nil {
			s.logger.Error("Failed to create sharing from pending invitation",
				zap.String("resource_id", resourceID),
				zap.Error(err),
			)
			continue
		}

		s.logger.Info("Auto-accepted pending invitation",
			zap.String("user_id", userID),
			zap.String("resource_id", resourceID),
		)
	}
}

// Helper functions

func (s *InvitationService) findUserByEmail(ctx context.Context, email string) *models.User {
	query := `MATCH (u:User {email: $email}) RETURN u.id, u.email, u.username`
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"email": email,
	})
	if err != nil || len(result.Records) == 0 {
		return nil
	}
	user := &models.User{}
	if v, ok := result.Records[0].Get("u.id"); ok && v != nil {
		user.ID = v.(string)
	}
	if v, ok := result.Records[0].Get("u.email"); ok && v != nil {
		user.Email = v.(string)
	}
	return user
}

func (s *InvitationService) getInviterName(ctx context.Context, inviterID string) string {
	query := `MATCH (u:User {id: $user_id}) RETURN u.full_name, u.username`
	result, err := s.neo4j.ExecuteQueryWithLogging(ctx, query, map[string]interface{}{
		"user_id": inviterID,
	})
	if err != nil || len(result.Records) == 0 {
		return "Someone"
	}
	if v, ok := result.Records[0].Get("u.full_name"); ok && v != nil {
		if name := v.(string); name != "" {
			return name
		}
	}
	if v, ok := result.Records[0].Get("u.username"); ok && v != nil {
		return v.(string)
	}
	return "Someone"
}

func generateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
