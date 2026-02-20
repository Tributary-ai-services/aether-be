package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// CloudDriveHandler handles cloud drive API endpoints
type CloudDriveHandler struct {
	cloudDriveService *services.CloudDriveService
	oauthService      *services.OAuthService
	logger            *logger.Logger
}

// NewCloudDriveHandler creates a new CloudDriveHandler
func NewCloudDriveHandler(cloudDriveService *services.CloudDriveService, oauthService *services.OAuthService, log *logger.Logger) *CloudDriveHandler {
	return &CloudDriveHandler{
		cloudDriveService: cloudDriveService,
		oauthService:      oauthService,
		logger:            log.WithService("cloud_drive_handler"),
	}
}

// ListFiles lists files from a cloud drive provider
// GET /api/v1/cloud-drives/:provider/files
func (h *CloudDriveHandler) ListFiles(c *gin.Context) {
	provider := c.Param("provider")
	credentialID := c.Query("credential_id")
	path := c.DefaultQuery("path", "root")

	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id is required"})
		return
	}

	opts := &services.ListFilesOptions{
		SiteID:    c.Query("site_id"),
		LibraryID: c.Query("library_id"),
		PageToken: c.Query("page_token"),
	}

	result, err := h.cloudDriveService.ListFiles(c.Request.Context(), provider, credentialID, userID, path, opts)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list cloud drive files")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list files"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// SearchFiles searches for files in a cloud drive
// GET /api/v1/cloud-drives/:provider/search
func (h *CloudDriveHandler) SearchFiles(c *gin.Context) {
	provider := c.Param("provider")
	credentialID := c.Query("credential_id")
	query := c.Query("q")

	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	if credentialID == "" || query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id and q are required"})
		return
	}

	result, err := h.cloudDriveService.SearchFiles(c.Request.Context(), provider, credentialID, userID, query)
	if err != nil {
		h.logger.WithError(err).Error("Failed to search cloud drive files")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search files"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ImportFiles imports files from a cloud drive into a notebook
// POST /api/v1/cloud-drives/:provider/import
func (h *CloudDriveHandler) ImportFiles(c *gin.Context) {
	provider := c.Param("provider")

	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	spaceCtx, err := middleware.GetSpaceContext(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "space context required for import"})
		return
	}

	var req struct {
		CredentialID string   `json:"credential_id" binding:"required"`
		NotebookID   string   `json:"notebook_id" binding:"required"`
		FileIDs      []string `json:"file_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id, notebook_id, and file_ids are required"})
		return
	}

	result, err := h.cloudDriveService.ImportFiles(c.Request.Context(), provider, req.CredentialID, userID, req.NotebookID, req.FileIDs, spaceCtx)
	if err != nil {
		h.logger.WithError(err).Error("Failed to import cloud drive files")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to import files"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ListSharePointSites lists SharePoint sites for the user
// GET /api/v1/cloud-drives/sharepoint/sites
func (h *CloudDriveHandler) ListSharePointSites(c *gin.Context) {
	credentialID := c.Query("credential_id")

	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id is required"})
		return
	}

	// Get token source for Microsoft provider
	tokenSource, err := h.oauthService.GetTokenSource(c.Request.Context(), credentialID, userID, "microsoft")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get authentication token"})
		return
	}

	provider := services.NewMicrosoftGraphProvider(tokenSource, h.logger)
	sites, err := provider.ListSites(c.Request.Context())
	if err != nil {
		h.logger.WithError(err).Error("Failed to list SharePoint sites")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list sites"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"sites": sites})
}

// ListSharePointLibraries lists document libraries for a SharePoint site
// GET /api/v1/cloud-drives/sharepoint/sites/:siteId/libraries
func (h *CloudDriveHandler) ListSharePointLibraries(c *gin.Context) {
	siteID := c.Param("siteId")
	credentialID := c.Query("credential_id")

	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id is required"})
		return
	}

	tokenSource, err := h.oauthService.GetTokenSource(c.Request.Context(), credentialID, userID, "microsoft")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get authentication token"})
		return
	}

	provider := services.NewMicrosoftGraphProvider(tokenSource, h.logger)
	libraries, err := provider.ListDocumentLibraries(c.Request.Context(), siteID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list document libraries")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list libraries"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"libraries": libraries})
}
