package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// SpaceContextService handles space context resolution
type SpaceContextService struct {
	userService  *UserService
	orgService   *OrganizationService
	spaceService *SpaceService
	audiModal    *AudiModalService
	logger       *logger.Logger
}

// NewSpaceContextService creates a new space context service
func NewSpaceContextService(userService *UserService, orgService *OrganizationService, spaceService *SpaceService, audiModal *AudiModalService, log *logger.Logger) *SpaceContextService {
	return &SpaceContextService{
		userService:  userService,
		orgService:   orgService,
		spaceService: spaceService,
		audiModal:    audiModal,
		logger:       log.WithService("space_context_service"),
	}
}

// ResolveSpaceContext resolves a space context for a user
func (s *SpaceContextService) ResolveSpaceContext(ctx context.Context, userID string, req models.SpaceContextRequest) (*models.SpaceContext, error) {
	var spaceContext *models.SpaceContext
	var err error

	switch req.SpaceType {
	case models.SpaceTypePersonal:
		spaceContext, err = s.resolvePersonalSpace(ctx, userID, req.SpaceID)
	case models.SpaceTypeOrganization:
		spaceContext, err = s.resolveOrganizationSpace(ctx, userID, req.SpaceID)
	default:
		return nil, errors.BadRequestWithDetails("Invalid space type", map[string]interface{}{
			"space_type": req.SpaceType,
		})
	}

	if err != nil {
		return nil, err
	}

	s.logger.Info("Space context resolved",
		zap.String("user_id", userID),
		zap.String("space_type", string(spaceContext.SpaceType)),
		zap.String("space_id", spaceContext.SpaceID),
		zap.String("tenant_id", spaceContext.TenantID),
	)

	return spaceContext, nil
}

// resolvePersonalSpace resolves a personal space context
func (s *SpaceContextService) resolvePersonalSpace(ctx context.Context, userID, spaceID string) (*models.SpaceContext, error) {
	// Get user details first
	// userID here is the Keycloak ID from JWT, not the internal User ID
	user, err := s.userService.GetUserByKeycloakID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Check if user has personal tenant
	if !user.HasPersonalTenant() {
		return nil, errors.NotFoundWithDetails("Personal space not configured", map[string]interface{}{
			"user_id": userID,
		})
	}

	// Verify the user is accessing their own personal space
	// Use the pre-computed PersonalSpaceID from the user model (supports both tenant_X and UUID formats)
	if spaceID != user.PersonalSpaceID {
		s.logger.Error("Personal space ID mismatch",
			zap.String("keycloak_id", userID),
			zap.String("internal_user_id", user.ID),
			zap.String("requested_space_id", spaceID),
			zap.String("user_personal_space_id", user.PersonalSpaceID),
			zap.String("user_personal_tenant_id", user.PersonalTenantID),
		)
		return nil, errors.ForbiddenWithDetails("Cannot access another user's personal space", map[string]interface{}{
			"user_id":           userID,
			"space_id":          spaceID,
			"expected_space_id": user.PersonalSpaceID,
		})
	}

	tenantID, apiKey, _ := user.GetPersonalTenantInfo()

	return &models.SpaceContext{
		SpaceType:   models.SpaceTypePersonal,
		SpaceID:     spaceID,
		TenantID:    tenantID,
		APIKey:      apiKey,
		UserID:      userID,
		UserRole:    "owner",
		SpaceName:   fmt.Sprintf("%s's Personal Space", user.FullName),
		ResolvedAt:  time.Now(),
		Permissions: []string{"read", "write", "create", "update", "delete"},
	}, nil
}

// resolveOrganizationSpace resolves an organization space context
// It first tries to find an Organization node, then falls back to Space nodes
func (s *SpaceContextService) resolveOrganizationSpace(ctx context.Context, userID, spaceOrOrgID string) (*models.SpaceContext, error) {
	// First try to find an Organization with the given ID
	org, err := s.orgService.GetOrganization(ctx, spaceOrOrgID, userID)
	if err == nil {
		// Found an Organization - use the original Organization-based flow
		return s.resolveOrganizationSpaceFromOrg(ctx, userID, org)
	}

	// If Organization not found, try to find a Space node with organization type
	if errors.IsNotFound(err) && s.spaceService != nil {
		s.logger.Debug("Organization not found, trying Space node lookup",
			zap.String("space_or_org_id", spaceOrOrgID),
			zap.String("user_id", userID),
		)
		return s.resolveOrganizationSpaceFromSpaceNode(ctx, userID, spaceOrOrgID)
	}

	// Return the original error if it wasn't a not-found error
	return nil, err
}

// resolveOrganizationSpaceFromOrg resolves space context from an Organization node
func (s *SpaceContextService) resolveOrganizationSpaceFromOrg(ctx context.Context, userID string, org *models.Organization) (*models.SpaceContext, error) {
	// Check if organization has tenant
	if !org.HasTenant() {
		return nil, errors.NotFoundWithDetails("Organization space not configured", map[string]interface{}{
			"org_id": org.ID,
		})
	}

	// Check user membership
	members, err := s.orgService.GetOrganizationMembers(ctx, org.ID, userID)
	if err != nil || len(members) == 0 {
		return nil, errors.ForbiddenWithDetails("User is not a member of this organization", map[string]interface{}{
			"user_id": userID,
			"org_id":  org.ID,
		})
	}

	// Find the user's member record
	var member *models.OrganizationMember
	for _, m := range members {
		if m.UserID == userID {
			member = m
			break
		}
	}

	if member == nil {
		return nil, errors.ForbiddenWithDetails("User is not a member of this organization", map[string]interface{}{
			"user_id": userID,
			"org_id":  org.ID,
		})
	}

	// Map member role to permissions
	permissions := s.getRolePermissions(member.Role)

	return &models.SpaceContext{
		SpaceType:   models.SpaceTypeOrganization,
		SpaceID:     org.ID,
		TenantID:    org.TenantID,
		APIKey:      org.TenantAPIKey,
		UserID:      userID,
		UserRole:    member.Role,
		SpaceName:   org.Name,
		ResolvedAt:  time.Now(),
		Permissions: permissions,
	}, nil
}

// resolveOrganizationSpaceFromSpaceNode resolves space context from a Space node with organization type
func (s *SpaceContextService) resolveOrganizationSpaceFromSpaceNode(ctx context.Context, userID, spaceID string) (*models.SpaceContext, error) {
	// Get the Space node
	space, err := s.spaceService.GetSpaceByID(ctx, spaceID)
	if err != nil {
		return nil, err
	}

	// Verify this is an organization-type space
	if space.Type != models.SpaceTypeOrganization {
		return nil, errors.BadRequestWithDetails("Space is not an organization space", map[string]interface{}{
			"space_id":   spaceID,
			"space_type": space.Type,
		})
	}

	// Validate user access to this space
	// Check if user has access via:
	// 1. Direct OWNS relationship
	// 2. Organization membership (if space is owned by an org)
	hasAccess, role, err := s.spaceService.CheckUserSpaceAccess(ctx, userID, spaceID)
	if err != nil {
		s.logger.Error("Failed to check user space access",
			zap.String("user_id", userID),
			zap.String("space_id", spaceID),
			zap.Error(err),
		)
		return nil, errors.ForbiddenWithDetails("Failed to verify space access", map[string]interface{}{
			"user_id":  userID,
			"space_id": spaceID,
		})
	}

	if !hasAccess {
		return nil, errors.ForbiddenWithDetails("User does not have access to this space", map[string]interface{}{
			"user_id":  userID,
			"space_id": spaceID,
		})
	}

	// Map role to permissions
	permissions := s.getRolePermissions(role)

	return &models.SpaceContext{
		SpaceType:   models.SpaceTypeOrganization,
		SpaceID:     spaceID,
		TenantID:    space.TenantID,
		APIKey:      "", // Space nodes may not have API keys directly
		UserID:      userID,
		UserRole:    role,
		SpaceName:   space.Name,
		ResolvedAt:  time.Now(),
		Permissions: permissions,
	}, nil
}

// GetUserSpaces returns all available spaces for a user
func (s *SpaceContextService) GetUserSpaces(ctx context.Context, userID string) (*models.SpaceListResponse, error) {
	// Get user for personal space
	// userID here is the Keycloak ID from JWT, not the internal User ID
	user, err := s.userService.GetUserByKeycloakID(ctx, userID)
	if err != nil {
		return nil, err
	}

	response := &models.SpaceListResponse{
		OrganizationSpaces: make([]*models.SpaceInfo, 0),
	}

	// Add personal space if configured, or create one if missing
	if user.HasPersonalTenant() {
		// Use the pre-computed PersonalSpaceID from the user model (supports both tenant_X and UUID formats)
		response.PersonalSpace = &models.SpaceInfo{
			SpaceType:   models.SpaceTypePersonal,
			SpaceID:     user.PersonalSpaceID,
			SpaceName:   fmt.Sprintf("%s's Personal Space", user.FullName),
			TenantID:    user.PersonalTenantID,
			UserRole:    "owner",
			Permissions: []string{"read", "write", "create", "update", "delete"},
		}
	} else {
		// User missing personal tenant, create one
		s.logger.Info("User missing personal tenant, creating one",
			zap.String("user_id", user.ID),
			zap.String("email", user.Email),
		)
		
		// Create personal tenant in AudiModal
		tenantReq := CreateTenantRequest{
			Name:         fmt.Sprintf("%s-personal", user.Username),
			DisplayName:  fmt.Sprintf("%s's Personal Space", user.FullName),
			BillingPlan:  "personal",
			BillingEmail: user.Email,
			Quotas: TenantQuotas{
				FilesPerHour:         100,
				StorageGB:            5,
				ComputeHours:         10,
				APIRequestsPerMinute: 100,
				MaxConcurrentJobs:    2,
				MaxFileSize:          52428800, // 50MB
				MaxChunksPerFile:     500,
				VectorStorageGB:      5,
			},
			Compliance: TenantCompliance{
				GDPR:               true,
				HIPAA:              false,
				SOX:                false,
				PCI:                false,
				DataResidency:      []string{},
				RetentionDays:      365,
				EncryptionRequired: true,
			},
			ContactInfo: TenantContactInfo{
				AdminEmail:     user.Email,
				SecurityEmail:  user.Email,
				BillingEmail:   user.Email,
				TechnicalEmail: user.Email,
			},
		}
		
		tenant, err := s.audiModal.CreateTenant(ctx, tenantReq)
		if err != nil {
			s.logger.Error("Failed to create personal tenant for existing user", zap.Error(err))
			// Don't fail the request - just return without personal space
		} else {
			// Update user with personal tenant info
			err = s.userService.UpdatePersonalTenantInfo(ctx, user.ID, tenant.TenantID, tenant.APIKey)
			if err != nil {
				s.logger.Error("Failed to update user with tenant info", zap.Error(err))
				// Don't fail the request - just log the error
			} else {
				// Update the user object (this also sets PersonalSpaceID)
				user.SetPersonalTenantInfo(tenant.TenantID, tenant.APIKey)
				s.logger.Info("Successfully created personal tenant for existing user",
					zap.String("user_id", user.ID),
					zap.String("tenant_id", tenant.TenantID),
				)

				// Add the personal space to the response using the pre-computed PersonalSpaceID
				response.PersonalSpace = &models.SpaceInfo{
					SpaceType:   models.SpaceTypePersonal,
					SpaceID:     user.PersonalSpaceID,
					SpaceName:   fmt.Sprintf("%s's Personal Space", user.FullName),
					TenantID:    tenant.TenantID,
					UserRole:    "owner",
					Permissions: []string{"read", "write", "create", "update", "delete"},
				}
			}
		}
	}

	// Get user's organizations
	orgs, err := s.orgService.GetOrganizations(ctx, userID)
	if err != nil {
		s.logger.Error("Failed to get user organizations", zap.Error(err))
		// Continue without organizations rather than failing
		return response, nil
	}

	// Add organization spaces
	for _, org := range orgs {
		if org.HasTenant() {
			// Get the user's role in this organization
			members, err := s.orgService.GetOrganizationMembers(ctx, org.ID, userID)
			if err != nil || len(members) == 0 {
				continue
			}
			
			// Find user's role
			var userRole string
			for _, member := range members {
				if member.UserID == userID {
					userRole = member.Role
					break
				}
			}
			
			if userRole == "" {
				continue
			}
			
			permissions := s.getRolePermissions(userRole)
			response.OrganizationSpaces = append(response.OrganizationSpaces, &models.SpaceInfo{
				SpaceType:   models.SpaceTypeOrganization,
				SpaceID:     org.ID,
				SpaceName:   org.Name,
				TenantID:    org.TenantID,
				UserRole:    userRole,
				Permissions: permissions,
			})
		}
	}

	// Also query for Space nodes that the user owns or is a member of (via graph relationships)
	// This includes spaces created via the Space API (not just organization-based spaces)
	if s.spaceService != nil {
		spaceNodes, err := s.spaceService.GetUserSpaces(ctx, userID)
		if err != nil {
			s.logger.Warn("Failed to get user space nodes", zap.Error(err))
			// Continue without space nodes rather than failing
		} else {
			// Add space nodes that aren't already in the response
			existingSpaceIDs := make(map[string]bool)
			if response.PersonalSpace != nil {
				existingSpaceIDs[response.PersonalSpace.SpaceID] = true
			}
			for _, orgSpace := range response.OrganizationSpaces {
				existingSpaceIDs[orgSpace.SpaceID] = true
			}

			for _, spaceNode := range spaceNodes {
				if existingSpaceIDs[spaceNode.SpaceID] {
					continue // Skip spaces already in response
				}

				// Add based on space type
				if spaceNode.SpaceType == models.SpaceTypePersonal && response.PersonalSpace == nil {
					response.PersonalSpace = spaceNode
				} else if spaceNode.SpaceType == models.SpaceTypeOrganization {
					response.OrganizationSpaces = append(response.OrganizationSpaces, spaceNode)
				}
			}
		}
	}

	return response, nil
}

// ValidateSpaceAccess validates that a user has access to a specific space
func (s *SpaceContextService) ValidateSpaceAccess(ctx context.Context, userID string, spaceType models.SpaceType, spaceID string) error {
	req := models.SpaceContextRequest{
		SpaceType: spaceType,
		SpaceID:   spaceID,
	}

	_, err := s.ResolveSpaceContext(ctx, userID, req)
	return err
}

// getRolePermissions maps organization roles to permissions
func (s *SpaceContextService) getRolePermissions(role string) []string {
	switch role {
	case "owner":
		return []string{"read", "write", "create", "update", "delete", "admin"}
	case "admin":
		return []string{"read", "write", "create", "update", "delete"}
	case "member":
		return []string{"read", "write", "create", "update"}
	case "viewer":
		return []string{"read"}
	default:
		return []string{"read"}
	}
}

// Cache helpers

// Redis caching methods removed - no longer using Redis