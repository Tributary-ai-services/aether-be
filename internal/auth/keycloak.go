package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// KeycloakClient handles Keycloak OIDC authentication
type KeycloakClient struct {
	provider      *oidc.Provider
	verifier      *oidc.IDTokenVerifier
	oauth2Config  *oauth2.Config
	logger        *logger.Logger
	config        config.KeycloakConfig
	allowedIssuers []string  // Support multiple issuer URLs for dev/prod environments
}

// UserInfo represents user information from Keycloak
type UserInfo struct {
	Sub               string                 `json:"sub"`
	Email             string                 `json:"email"`
	EmailVerified     bool                   `json:"email_verified"`
	PreferredUsername string                 `json:"preferred_username"`
	Name              string                 `json:"name"`
	GivenName         string                 `json:"given_name"`
	FamilyName        string                 `json:"family_name"`
	Roles             []string               `json:"realm_access.roles"`
	Groups            []string               `json:"groups"`
	Attributes        map[string]interface{} `json:"-"`
}

// TokenClaims represents JWT token claims
type TokenClaims struct {
	Sub               string                 `json:"sub"`
	Iss               string                 `json:"iss"`
	Aud               interface{}            `json:"aud"`
	Exp               int64                  `json:"exp"`
	Iat               int64                  `json:"iat"`
	Email             string                 `json:"email"`
	EmailVerified     bool                   `json:"email_verified"`
	PreferredUsername string                 `json:"preferred_username"`
	Name              string                 `json:"name"`
	GivenName         string                 `json:"given_name"`
	FamilyName        string                 `json:"family_name"`
	RealmAccess       RealmAccess            `json:"realm_access"`
	ResourceAccess    map[string]interface{} `json:"resource_access"`
	Groups            []string               `json:"groups"`
}

// RealmAccess represents realm access information
type RealmAccess struct {
	Roles []string `json:"roles"`
}

// NewKeycloakClient creates a new Keycloak client
func NewKeycloakClient(cfg config.KeycloakConfig, log *logger.Logger) (*KeycloakClient, error) {
	ctx := context.Background()

	// Construct provider URL
	providerURL := fmt.Sprintf("%s/realms/%s", cfg.URL, cfg.Realm)

	// Create OIDC provider
	provider, err := oidc.NewProvider(ctx, providerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Create ID token verifier with SkipIssuerCheck for multiple issuer support
	// Skip audience check entirely by not setting ClientID (admin-cli tokens don't have audience)
	verifier := provider.Verifier(&oidc.Config{
		SkipIssuerCheck:   true, // We'll manually validate issuers
		SkipClientIDCheck: true, // Skip client ID check since admin-cli tokens don't have proper audience
	})

	// Create OAuth2 config
	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "roles"},
	}

	// Set up allowed issuers for multi-environment support
	allowedIssuers := []string{
		providerURL, // Primary provider URL (from config)
	}

	log.Debug("Setting up allowed issuers", zap.String("keycloak_url", cfg.URL), zap.String("realm", cfg.Realm))

	// Add localhost issuer for development (when configured Keycloak is internal)
	if cfg.URL == "http://tas-keycloak-shared:8080" {
		log.Debug("Adding issuers for tas-keycloak-shared configuration")
		allowedIssuers = append(allowedIssuers, "http://localhost:8081/realms/"+cfg.Realm)
		allowedIssuers = append(allowedIssuers, "http://localhost/realms/"+cfg.Realm) // nginx proxy without port
	}

	// Add localhost issuer when Keycloak is at http://keycloak:8080 (docker service name)
	if cfg.URL == "http://keycloak:8080" {
		log.Debug("Adding issuers for keycloak:8080 configuration")
		allowedIssuers = append(allowedIssuers, "http://localhost:8081/realms/"+cfg.Realm)
		allowedIssuers = append(allowedIssuers, "http://localhost/realms/"+cfg.Realm) // nginx proxy without port
	}

	// Add internal issuer for production (when configured Keycloak is external)
	if cfg.URL == "http://localhost:8081" {
		log.Debug("Adding issuers for localhost:8081 configuration")
		allowedIssuers = append(allowedIssuers, "http://tas-keycloak-shared:8080/realms/"+cfg.Realm)
		allowedIssuers = append(allowedIssuers, "http://localhost/realms/"+cfg.Realm) // nginx proxy without port
	}

	// Add external issuer for K8s deployment (when configured Keycloak is internal but advertises external)
	if cfg.URL == "http://keycloak-shared.tas-shared:8080" {
		log.Debug("Adding issuers for K8s keycloak-shared.tas-shared configuration")
		allowedIssuers = append(allowedIssuers, "http://keycloak.tas.scharber.com/realms/"+cfg.Realm)
		allowedIssuers = append(allowedIssuers, "https://keycloak.tas.scharber.com/realms/"+cfg.Realm)
		log.Debug("Added K8s issuers", zap.Int("total_issuers", len(allowedIssuers)))
	}

	// Add external issuer for K8s deployment with full service DNS
	if cfg.URL == "http://keycloak-shared.tas-shared.svc.cluster.local:8080" {
		log.Debug("Adding issuers for full K8s DNS configuration")
		allowedIssuers = append(allowedIssuers, "http://keycloak.tas.scharber.com/realms/"+cfg.Realm)
		allowedIssuers = append(allowedIssuers, "https://keycloak.tas.scharber.com/realms/"+cfg.Realm)
	}

	client := &KeycloakClient{
		provider:       provider,
		verifier:       verifier,
		oauth2Config:   oauth2Config,
		logger:         log.WithService("keycloak"),
		config:         cfg,
		allowedIssuers: allowedIssuers,
	}

	client.logger.Info("Keycloak client initialized",
		zap.String("realm", cfg.Realm),
		zap.String("client_id", cfg.ClientID),
		zap.String("provider_url", providerURL),
		zap.Strings("allowed_issuers", allowedIssuers),
	)

	return client, nil
}

// VerifyIDToken verifies and parses an ID token
func (k *KeycloakClient) VerifyIDToken(ctx context.Context, rawIDToken string) (*TokenClaims, error) {
	// Verify the ID token
	idToken, err := k.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		k.logger.Error("Failed to verify ID token", zap.Error(err))
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	// Parse claims
	var claims TokenClaims
	if err := idToken.Claims(&claims); err != nil {
		k.logger.Error("Failed to parse token claims", zap.Error(err))
		return nil, fmt.Errorf("failed to parse token claims: %w", err)
	}

	// Manually validate issuer against allowed list
	issuerValid := false
	for _, allowedIssuer := range k.allowedIssuers {
		if claims.Iss == allowedIssuer {
			issuerValid = true
			break
		}
	}

	if !issuerValid {
		k.logger.Warn("Token issued by unauthorized issuer",
			zap.String("token_issuer", claims.Iss),
			zap.Strings("allowed_issuers", k.allowedIssuers),
		)
		return nil, fmt.Errorf("token issued by unauthorized issuer: %s", claims.Iss)
	}

	k.logger.Debug("ID token verified successfully",
		zap.String("subject", claims.Sub),
		zap.String("email", claims.Email),
		zap.String("username", claims.PreferredUsername),
		zap.String("issuer", claims.Iss),
	)

	return &claims, nil
}

// GetUserInfo retrieves user information from Keycloak
func (k *KeycloakClient) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	// Create OAuth2 token
	token := &oauth2.Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
	}

	// Get user info from provider
	userInfo, err := k.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		k.logger.Error("Failed to get user info", zap.Error(err))
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Parse user info
	var user UserInfo
	if err := userInfo.Claims(&user); err != nil {
		k.logger.Error("Failed to parse user info claims", zap.Error(err))
		return nil, fmt.Errorf("failed to parse user info claims: %w", err)
	}

	// Get additional attributes
	var allClaims map[string]interface{}
	if err := userInfo.Claims(&allClaims); err == nil {
		user.Attributes = allClaims
	}

	k.logger.Debug("User info retrieved successfully",
		zap.String("subject", user.Sub),
		zap.String("email", user.Email),
		zap.String("username", user.PreferredUsername),
	)

	return &user, nil
}

// ValidateToken validates an access token
func (k *KeycloakClient) ValidateToken(ctx context.Context, accessToken string) (bool, error) {
	// Try to get user info - if successful, token is valid
	_, err := k.GetUserInfo(ctx, accessToken)
	if err != nil {
		k.logger.Debug("Token validation failed", zap.Error(err))
		return false, nil
	}

	return true, nil
}

// GetAuthURL generates an authorization URL for OAuth2 flow
func (k *KeycloakClient) GetAuthURL(state string, redirectURL string) string {
	// Set redirect URL
	config := *k.oauth2Config
	config.RedirectURL = redirectURL

	return config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// ExchangeCode exchanges authorization code for tokens
func (k *KeycloakClient) ExchangeCode(ctx context.Context, code string, redirectURL string) (*oauth2.Token, error) {
	// Set redirect URL
	config := *k.oauth2Config
	config.RedirectURL = redirectURL

	// Exchange code for token
	token, err := config.Exchange(ctx, code)
	if err != nil {
		k.logger.Error("Failed to exchange authorization code", zap.Error(err))
		return nil, fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	k.logger.Debug("Authorization code exchanged successfully")
	return token, nil
}

// RefreshToken refreshes an access token using a refresh token
func (k *KeycloakClient) RefreshToken(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	// Create token source with refresh token
	token := &oauth2.Token{
		RefreshToken: refreshToken,
	}

	tokenSource := k.oauth2Config.TokenSource(ctx, token)

	// Get new token
	newToken, err := tokenSource.Token()
	if err != nil {
		k.logger.Error("Failed to refresh token", zap.Error(err))
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	k.logger.Debug("Token refreshed successfully")
	return newToken, nil
}

// CheckPermission checks if user has specific role or permission
func (k *KeycloakClient) CheckPermission(claims *TokenClaims, requiredRole string) bool {
	// Check realm roles
	for _, role := range claims.RealmAccess.Roles {
		if role == requiredRole {
			return true
		}
	}

	// Check client roles (resource access)
	if clientAccess, exists := claims.ResourceAccess[k.config.ClientID]; exists {
		if clientRoles, ok := clientAccess.(map[string]interface{})["roles"].([]interface{}); ok {
			for _, role := range clientRoles {
				if roleStr, ok := role.(string); ok && roleStr == requiredRole {
					return true
				}
			}
		}
	}

	return false
}

// CheckGroup checks if user belongs to a specific group
func (k *KeycloakClient) CheckGroup(claims *TokenClaims, requiredGroup string) bool {
	for _, group := range claims.Groups {
		if group == requiredGroup {
			return true
		}
	}
	return false
}

// IsAdmin checks if user has admin role
func (k *KeycloakClient) IsAdmin(claims *TokenClaims) bool {
	return k.CheckPermission(claims, "admin") || k.CheckPermission(claims, "realm-admin")
}

// GetProviderMetadata returns OIDC provider metadata
func (k *KeycloakClient) GetProviderMetadata() *oidc.Provider {
	return k.provider
}

// HealthCheck performs a health check on the Keycloak connection
func (k *KeycloakClient) HealthCheck(ctx context.Context) error {
	// Try to get provider configuration
	providerURL := fmt.Sprintf("%s/realms/%s", k.config.URL, k.config.Realm)

	_, err := oidc.NewProvider(ctx, providerURL)
	if err != nil {
		return fmt.Errorf("keycloak health check failed: %w", err)
	}

	return nil
}
