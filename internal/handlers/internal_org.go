package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// InternalOrgHandler serves /internal/v1/orgs* to in-cluster services
// (Plan #22 2.1/2.2). Mounted only when INTERNAL_AUTH_TOKEN is set.
type InternalOrgHandler struct {
	Snapshots *services.OrgSnapshotService
	Logger    *logger.Logger
}

// ListOrgs: GET /internal/v1/orgs
func (h *InternalOrgHandler) ListOrgs(c *gin.Context) {
	orgs, err := h.Snapshots.ListOrgs(c.Request.Context())
	if err != nil {
		h.Logger.Error("internal: list orgs failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "org_list_failed"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orgs": orgs})
}

// Snapshot: GET /internal/v1/orgs/:id/snapshot
func (h *InternalOrgHandler) Snapshot(c *gin.Context) {
	snap, err := h.Snapshots.Snapshot(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.Logger.Error("internal: org snapshot failed", zap.Error(err), zap.String("org_id", c.Param("id")))
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "org_snapshot_failed"}})
		return
	}
	if snap == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "org_not_found"}})
		return
	}
	c.JSON(http.StatusOK, snap)
}
