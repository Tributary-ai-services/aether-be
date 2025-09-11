package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// SpaceContextService handles space context resolution
type SpaceContextService struct {
	userService *UserService
	orgService  *OrganizationService
	audiModal   *AudiModalService
	logger      *logger.Logger
}

// NewSpaceContextService creates a new space context service
func NewSpaceContextService(userService *UserService, orgService *OrganizationService, audiModal *AudiModalService, log *logger.Logger) *SpaceContextService {
	return &SpaceContextService{
		userService: userService,
		orgService:  orgService,
		audiModal:   audiModal,
		logger:      log.WithService("space_context_service"),
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
	user, err := s.userService.GetUserByID(ctx, userID)
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
	// Personal space ID is derived from tenant ID: tenant_X -> space_X
	expectedSpaceID := strings.Replace(user.PersonalTenantID, "tenant_", "space_", 1)
	if spaceID != expectedSpaceID {
		return nil, errors.ForbiddenWithDetails("Cannot access another user's personal space", map[string]interface{}{
			"user_id":          userID,
			"space_id":         spaceID,
			"expected_space_id": expectedSpaceID,
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
func (s *SpaceContextService) resolveOrganizationSpace(ctx context.Context, userID, orgID string) (*models.SpaceContext, error) {
	// Get organization details
	org, err := s.orgService.GetOrganization(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}

	// Check if organization has tenant
	if !org.HasTenant() {
		return nil, errors.NotFoundWithDetails("Organization space not configured", map[string]interface{}{
			"org_id": orgID,
		})
	}

	// Check user membership
	members, err := s.orgService.GetOrganizationMembers(ctx, orgID, userID)
	if err != nil || len(members) == 0 {
		return nil, errors.ForbiddenWithDetails("User is not a member of this organization", map[string]interface{}{
			"user_id": userID,
			"org_id":  orgID,
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
			"org_id":  orgID,
		})
	}

	// Map member role to permissions
	permissions := s.getRolePermissions(member.Role)
	
	tenantID := org.TenantID
	apiKey := org.TenantAPIKey

	return &models.SpaceContext{
		SpaceType:   models.SpaceTypeOrganization,
		SpaceID:     orgID,
		TenantID:    tenantID,
		APIKey:      apiKey,
		UserID:      userID,
		UserRole:    member.Role,
		SpaceName:   org.Name,
		ResolvedAt:  time.Now(),
		Permissions: permissions,
	}, nil
}

// GetUserSpaces returns all available spaces for a user
func (s *SpaceContextService) GetUserSpaces(ctx context.Context, userID string) (*models.SpaceListResponse, error) {
	// Get user for personal space
	user, err := s.userService.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	response := &models.SpaceListResponse{
		OrganizationSpaces: make([]*models.SpaceInfo, 0),
	}

	// Add personal space if configured, or create one if missing
	if user.HasPersonalTenant() {
		// Force use the fallback for now to test
		spaceID := strings.Replace(user.PersonalTenantID, "tenant_", "space_", 1)
		response.PersonalSpace = &models.SpaceInfo{
			SpaceType:   models.SpaceTypePersonal,
			SpaceID:     spaceID,
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
			ContactEmail: user.Email,
			Quotas: map[string]interface{}{
				"max_data_sources":      10,
				"max_files":            1000,
				"max_storage_mb":        5120, // 5GB
				"max_vector_dimensions": 1536,
				"max_monthly_searches":  10000,
			},
			Compliance: map[string]interface{}{
				"data_retention_days":   365,
				"encryption_enabled":    true,
				"audit_logging_enabled": true,
				"gdpr_compliant":       true,
			},
			Settings: map[string]interface{}{
				"user_id":       user.ID,
				"user_email":    user.Email,
				"creation_type": "on_demand_setup",
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
				// Update the user object
				user.SetPersonalTenantInfo(tenant.TenantID, tenant.APIKey)
				s.logger.Info("Successfully created personal tenant for existing user",
					zap.String("user_id", user.ID),
					zap.String("tenant_id", tenant.TenantID),
				)
				
				// Add the personal space to the response
				spaceID := strings.Replace(tenant.TenantID, "tenant_", "space_", 1)
				response.PersonalSpace = &models.SpaceInfo{
					SpaceType:   models.SpaceTypePersonal,
					SpaceID:     spaceID,
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