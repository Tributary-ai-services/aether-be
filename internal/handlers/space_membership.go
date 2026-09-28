package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/pkg/errors"
)

// SpaceMembershipResponse is the answer to "is this caller in this space?".
//
// It is deliberately a subset of models.SpaceContext: the resolved context
// also carries the space's AudiModal API key, which must never leave this
// service. Only the fields another service needs to enforce isolation are
// returned.
type SpaceMembershipResponse struct {
	Member      bool             `json:"member"`
	SpaceType   models.SpaceType `json:"space_type"`
	SpaceID     string           `json:"space_id"`
	TenantID    string           `json:"tenant_id"`
	UserRole    string           `json:"user_role"`
	Permissions []string         `json:"permissions"`
}

// VerifySpaceMembership reports whether the authenticated caller is a member
// of the given space.
//
// It exists for service-to-service isolation checks: agent-builder stores
// agents keyed by space_id but has no membership data of its own, so before
// it scopes a query to a space it asks here, passing through the caller's own
// bearer token. Membership is decided by the same SpaceContextService the
// space middleware uses, so there is one definition of "in this space" rather
// than a second copy that can drift.
//
// A non-member gets 403, not 404: the space's existence is not the secret,
// and agent-builder needs to tell "you may not" apart from "no such space".
//
// GET /api/v1/spaces/:id/membership?space_type=personal|organization
func (h *SpaceHandler) VerifySpaceMembership(c *gin.Context) {
	spaceID := c.Param("id")
	if spaceID == "" {
		c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Space ID is required", nil))
		return
	}

	// The Keycloak subject, which is what ResolveSpaceContext expects —
	// it resolves the internal user itself.
	userID := getUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, errors.Unauthorized("User not authenticated"))
		return
	}

	// space_type is optional. Callers that know it should send it; callers
	// holding only an opaque space id get both types tried, personal first
	// because it is the common case.
	candidates := []models.SpaceType{models.SpaceTypePersonal, models.SpaceTypeOrganization}
	if requested := c.Query("space_type"); requested != "" {
		st := models.SpaceType(requested)
		if st != models.SpaceTypePersonal && st != models.SpaceTypeOrganization {
			c.JSON(http.StatusBadRequest, errors.ValidationWithDetails("Invalid space_type", map[string]interface{}{
				"space_type": requested,
			}))
			return
		}
		candidates = []models.SpaceType{st}
	}

	var lastErr error
	for _, spaceType := range candidates {
		spaceContext, err := h.spaceContextService.ResolveSpaceContext(c.Request.Context(), userID, models.SpaceContextRequest{
			SpaceType: spaceType,
			SpaceID:   spaceID,
		})
		if err != nil {
			lastErr = err
			continue
		}

		c.JSON(http.StatusOK, SpaceMembershipResponse{
			Member:      true,
			SpaceType:   spaceContext.SpaceType,
			SpaceID:     spaceContext.SpaceID,
			TenantID:    spaceContext.TenantID,
			UserRole:    spaceContext.UserRole,
			Permissions: spaceContext.Permissions,
		})
		return
	}

	h.logger.Info("Space membership denied",
		zap.String("user_id", userID),
		zap.String("space_id", spaceID),
		zap.Error(lastErr),
	)

	// Whatever the underlying reason (no such space, not a member, personal
	// space belonging to someone else), the caller gets one answer: not a
	// member. Anything else would let a caller enumerate spaces.
	c.JSON(http.StatusForbidden, SpaceMembershipResponse{
		Member:  false,
		SpaceID: spaceID,
	})
}
