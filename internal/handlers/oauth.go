package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/middleware"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// OAuthHandler handles OAuth2 flows for cloud drive providers
type OAuthHandler struct {
	oauthService *services.OAuthService
	logger       *logger.Logger
}

// NewOAuthHandler creates a new OAuthHandler
func NewOAuthHandler(oauthService *services.OAuthService, log *logger.Logger) *OAuthHandler {
	return &OAuthHandler{
		oauthService: oauthService,
		logger:       log.WithService("oauth_handler"),
	}
}

// Authorize initiates an OAuth flow for a provider
// POST /api/v1/oauth/:provider/authorize
func (h *OAuthHandler) Authorize(c *gin.Context) {
	provider := c.Param("provider")
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider is required"})
		return
	}

	// Accept optional login_hint from request body or query param
	var loginHint string
	var body struct {
		LoginHint string `json:"login_hint"`
	}
	if err := c.ShouldBindJSON(&body); err == nil {
		loginHint = body.LoginHint
	}
	if loginHint == "" {
		loginHint = c.Query("login_hint")
	}

	authURL, state, err := h.oauthService.GetAuthorizationURL(provider, loginHint)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"auth_url": authURL,
		"state":    state,
	})
}

// ListCredentials returns all OAuth credentials for the authenticated user
// GET /api/v1/credentials
func (h *OAuthHandler) ListCredentials(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	credentials, err := h.oauthService.ListCredentials(c.Request.Context(), userID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list credentials")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list credentials"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"credentials": credentials})
}

// DeleteCredential removes an OAuth credential
// DELETE /api/v1/credentials/:id
func (h *OAuthHandler) DeleteCredential(c *gin.Context) {
	credentialID := c.Param("id")
	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential id is required"})
		return
	}

	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	if err := h.oauthService.DeleteCredential(c.Request.Context(), credentialID, userID); err != nil {
		h.logger.WithError(err).Error("Failed to delete credential")
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "credential deleted"})
}

// Callback handles the OAuth callback after user authorization
// POST /api/v1/oauth/:provider/callback
func (h *OAuthHandler) Callback(c *gin.Context) {
	provider := c.Param("provider")
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider is required"})
		return
	}

	var req struct {
		Code  string `json:"code" binding:"required"`
		State string `json:"state" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code and state are required"})
		return
	}

	// Get user ID from auth context
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	fmt.Printf("[OAUTH HANDLER] Callback called: provider=%s code_len=%d state=%s user=%s\n", provider, len(req.Code), req.State, userID)
	h.logger.Info("OAuth callback starting",
		zap.String("provider", provider),
		zap.Int("code_length", len(req.Code)),
		zap.String("state", req.State),
		zap.String("user_id", userID),
	)

	credential, err := h.oauthService.ExchangeCode(c.Request.Context(), provider, req.Code, req.State, userID)
	if err != nil {
		fmt.Printf("[OAUTH HANDLER] ExchangeCode FAILED: %v\n", err)
		h.logger.WithError(err).Error("OAuth callback failed for provider: " + provider)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	fmt.Printf("[OAUTH HANDLER] ExchangeCode SUCCESS\n")

	c.JSON(http.StatusOK, gin.H{
		"credential": credential,
	})
}
