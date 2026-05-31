package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"

	"go.uber.org/zap"

	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
)

// OAuthService manages OAuth2 flows for cloud drive providers
type OAuthService struct {
	neo4j         *database.Neo4jClient
	redis         *redis.Client
	googleConfig  *oauth2.Config
	msConfig      *oauth2.Config
	encryptionKey []byte
	redirectBase  string
	logger        *logger.Logger
}

// OAuthCredential represents stored OAuth tokens
type OAuthCredential struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	UserID       string    `json:"user_id"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// OAuthCredentialMeta is the public metadata (no secrets)
type OAuthCredentialMeta struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// NewOAuthService creates a new OAuthService
func NewOAuthService(
	neo4j *database.Neo4jClient,
	redisClient *redis.Client,
	cfg *OAuthConfig,
	log *logger.Logger,
) *OAuthService {
	// Parse encryption key
	var encKey []byte
	if cfg.EncryptionKey != "" {
		decoded, err := hex.DecodeString(cfg.EncryptionKey)
		if err != nil {
			log.WithError(err).Error("Failed to decode OAuth encryption key, using random key")
			encKey = make([]byte, 32)
			rand.Read(encKey)
		} else {
			encKey = decoded
		}
	} else {
		log.Warn("OAUTH_ENCRYPTION_KEY not set, generating random key (tokens won't survive restarts)")
		encKey = make([]byte, 32)
		rand.Read(encKey)
	}

	redirectBase := cfg.RedirectBaseURL
	if redirectBase == "" {
		redirectBase = os.Getenv("OAUTH_REDIRECT_BASE_URL")
	}

	svc := &OAuthService{
		neo4j:         neo4j,
		redis:         redisClient,
		encryptionKey: encKey,
		redirectBase:  redirectBase,
		logger:        log.WithService("oauth_service"),
	}

	// Google OAuth config
	if cfg.GoogleClientID != "" {
		svc.googleConfig = &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL: "https://oauth2.googleapis.com/token",
			},
			Scopes:      []string{"https://www.googleapis.com/auth/drive.readonly"},
			RedirectURL: redirectBase + "/oauth/callback/google",
		}
	}

	// Microsoft OAuth config
	if cfg.MicrosoftClientID != "" {
		tenantID := cfg.MicrosoftTenantID
		if tenantID == "" {
			tenantID = "common"
		}
		svc.msConfig = &oauth2.Config{
			ClientID:     cfg.MicrosoftClientID,
			ClientSecret: cfg.MicrosoftClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:   fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize", tenantID),
				TokenURL:  fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenantID),
				AuthStyle: oauth2.AuthStyleInParams,
			},
			Scopes:      []string{"Files.Read.All", "Sites.Read.All", "User.Read", "offline_access"},
			RedirectURL: redirectBase + "/oauth/callback/microsoft",
		}
	}

	return svc
}

// generatePKCE creates a code_verifier and code_challenge for PKCE
func generatePKCE() (verifier, challenge string) {
	// Generate 32 random bytes for the verifier
	buf := make([]byte, 32)
	rand.Read(buf)
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	// SHA256 hash → base64url encode for S256 challenge
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

// GetAuthorizationURL generates an OAuth authorization URL with CSRF state
func (s *OAuthService) GetAuthorizationURL(provider string, loginHint ...string) (string, string, error) {
	cfg := s.getProviderConfig(provider)
	if cfg == nil {
		return "", "", fmt.Errorf("unsupported OAuth provider: %s", provider)
	}

	// Generate CSRF state token
	state := uuid.New().String()

	// Build provider-specific auth options
	var opts []oauth2.AuthCodeOption
	switch provider {
	case "google":
		// Google uses access_type=offline to get refresh tokens
		opts = append(opts, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	case "microsoft":
		// Microsoft v2.0 requires PKCE (code_challenge + code_challenge_method)
		// consent ensures file permissions are granted
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "consent"))
		// login_hint pre-selects the account so user doesn't have to pick
		if len(loginHint) > 0 && loginHint[0] != "" {
			opts = append(opts, oauth2.SetAuthURLParam("login_hint", loginHint[0]))
		}
		verifier, challenge := generatePKCE()
		opts = append(opts,
			oauth2.SetAuthURLParam("code_challenge", challenge),
			oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		)
		// Store verifier in Redis so ExchangeCode can use it
		if s.redis != nil {
			ctx := context.Background()
			vKey := fmt.Sprintf("oauth_pkce:%s", state)
			s.redis.Set(ctx, vKey, verifier, 10*time.Minute)
		}
	}

	// Store state in Redis with 10-minute TTL
	if s.redis != nil {
		ctx := context.Background()
		key := fmt.Sprintf("oauth_state:%s", state)
		s.redis.Set(ctx, key, provider, 10*time.Minute)
	}

	authURL := cfg.AuthCodeURL(state, opts...)
	return authURL, state, nil
}

// ExchangeCode exchanges an authorization code for tokens
func (s *OAuthService) ExchangeCode(ctx context.Context, provider, code, state, userID string) (*OAuthCredentialMeta, error) {
	// Validate state token
	if s.redis != nil {
		key := fmt.Sprintf("oauth_state:%s", state)
		storedProvider, err := s.redis.Get(ctx, key).Result()
		if err != nil {
			return nil, fmt.Errorf("invalid or expired state token")
		}
		if storedProvider != provider {
			return nil, fmt.Errorf("state token provider mismatch")
		}
		// Delete used state token
		s.redis.Del(ctx, key)
	}

	cfg := s.getProviderConfig(provider)
	if cfg == nil {
		return nil, fmt.Errorf("unsupported OAuth provider: %s", provider)
	}

	fmt.Printf("[OAUTH DEBUG] provider=%s redirect_url=%s token_url=%s code_length=%d\n",
		provider, cfg.RedirectURL, cfg.Endpoint.TokenURL, len(code))

	// Retrieve PKCE code_verifier for Microsoft
	var codeVerifier string
	if provider == "microsoft" && s.redis != nil {
		vKey := fmt.Sprintf("oauth_pkce:%s", state)
		codeVerifier, _ = s.redis.Get(ctx, vKey).Result()
		s.redis.Del(ctx, vKey)
	}

	var token *oauth2.Token
	var exchangeErr error
	if provider == "microsoft" {
		// Manual token exchange for Microsoft — full control over the POST body
		token, exchangeErr = s.microsoftTokenExchange(cfg, code, codeVerifier)
	} else {
		token, exchangeErr = cfg.Exchange(ctx, code)
	}
	if exchangeErr != nil {
		fmt.Printf("[OAUTH DEBUG] exchange FAILED: %v\n", exchangeErr)
		return nil, fmt.Errorf("token exchange failed: %w", exchangeErr)
	}
	fmt.Printf("[OAUTH DEBUG] exchange SUCCESS\n")

	// Encrypt tokens before storage
	encAccessToken, encErr := s.encrypt(token.AccessToken)
	if encErr != nil {
		return nil, fmt.Errorf("failed to encrypt access token: %w", encErr)
	}

	encRefreshToken := ""
	if token.RefreshToken != "" {
		encRefreshToken, encErr = s.encrypt(token.RefreshToken)
		if encErr != nil {
			return nil, fmt.Errorf("failed to encrypt refresh token: %w", encErr)
		}
	}

	// Determine credential name
	name := fmt.Sprintf("%s Cloud Drive", providerDisplayName(provider))

	// Store credential in Neo4j
	credID := uuid.New().String()
	now := time.Now()

	cred := OAuthCredential{
		ID:           credID,
		Provider:     provider,
		UserID:       userID,
		Name:         name,
		Status:       "active",
		AccessToken:  encAccessToken,
		RefreshToken: encRefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.storeCredential(ctx, cred); err != nil {
		return nil, fmt.Errorf("failed to store credential: %w", err)
	}

	return &OAuthCredentialMeta{
		ID:        credID,
		Provider:  provider,
		Name:      name,
		Status:    "active",
		CreatedAt: now,
	}, nil
}

// ListCredentials returns metadata for all OAuth credentials belonging to a user
func (s *OAuthService) ListCredentials(ctx context.Context, userID string) ([]*OAuthCredentialMeta, error) {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (c:OAuthCredential {user_id: $userId})
		RETURN c.id AS id, c.provider AS provider, c.name AS name,
		       c.status AS status, c.created_at AS createdAt
		ORDER BY c.created_at DESC
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"userId": userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query credentials: %w", err)
	}

	var credentials []*OAuthCredentialMeta
	for result.Next(ctx) {
		record := result.Record()
		cred := &OAuthCredentialMeta{}
		cred.ID, _ = getStringFromRecord(record, "id")
		cred.Provider, _ = getStringFromRecord(record, "provider")
		cred.Name, _ = getStringFromRecord(record, "name")
		cred.Status, _ = getStringFromRecord(record, "status")
		if createdStr, ok := getStringFromRecord(record, "createdAt"); ok {
			cred.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		}
		credentials = append(credentials, cred)
	}

	if credentials == nil {
		credentials = []*OAuthCredentialMeta{}
	}

	return credentials, nil
}

// DeleteCredential removes an OAuth credential belonging to a user
func (s *OAuthService) DeleteCredential(ctx context.Context, credentialID, userID string) error {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (c:OAuthCredential {id: $id, user_id: $userId})
		DELETE c
		RETURN count(c) AS deleted
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"id":     credentialID,
		"userId": userID,
	})
	if err != nil {
		return fmt.Errorf("failed to delete credential: %w", err)
	}

	if result.Next(ctx) {
		record := result.Record()
		deleted, ok := record.Get("deleted")
		if ok {
			if count, ok := deleted.(int64); ok && count == 0 {
				return fmt.Errorf("credential not found")
			}
		}
	}

	return nil
}

// GetDecryptedToken retrieves and decrypts tokens for a credential
func (s *OAuthService) GetDecryptedToken(ctx context.Context, credentialID, userID string) (*oauth2.Token, error) {
	cred, err := s.getCredential(ctx, credentialID, userID)
	if err != nil {
		return nil, err
	}

	accessToken, err := s.decrypt(cred.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt access token: %w", err)
	}

	refreshToken := ""
	if cred.RefreshToken != "" {
		refreshToken, err = s.decrypt(cred.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt refresh token: %w", err)
		}
	}

	token := &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    cred.TokenType,
		Expiry:       cred.Expiry,
	}

	// Auto-refresh if expired or expiring within 5 minutes
	if token.Expiry.Before(time.Now().Add(5*time.Minute)) && token.RefreshToken != "" {
		cfg := s.getProviderConfig(cred.Provider)
		if cfg != nil {
			src := cfg.TokenSource(ctx, token)
			newToken, err := src.Token()
			if err != nil {
				return nil, fmt.Errorf("token refresh failed: %w", err)
			}
			// Store updated tokens
			if newToken.AccessToken != token.AccessToken {
				s.updateStoredToken(ctx, credentialID, newToken)
			}
			return newToken, nil
		}
	}

	return token, nil
}

// GetTokenSource returns an auto-refreshing token source that persists refreshed tokens
func (s *OAuthService) GetTokenSource(ctx context.Context, credentialID, userID, provider string) (oauth2.TokenSource, error) {
	token, err := s.GetDecryptedToken(ctx, credentialID, userID)
	if err != nil {
		return nil, err
	}

	cfg := s.getProviderConfig(provider)
	if cfg == nil {
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	// Wrap in a persisting token source that saves refreshed tokens back to Neo4j
	inner := cfg.TokenSource(ctx, token)
	return &persistingTokenSource{
		inner:        inner,
		current:      token,
		credentialID: credentialID,
		oauthService: s,
		ctx:          ctx,
		logger:       s.logger,
	}, nil
}

// persistingTokenSource wraps an oauth2.TokenSource and persists refreshed tokens to the database.
// This ensures that when the underlying ReuseTokenSource refreshes an expired token in memory,
// the new token (and potentially rotated refresh token) is saved back to Neo4j.
type persistingTokenSource struct {
	inner        oauth2.TokenSource
	current      *oauth2.Token
	credentialID string
	oauthService *OAuthService
	ctx          context.Context
	logger       *logger.Logger
	mu           sync.Mutex
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	token, err := p.inner.Token()
	if err != nil {
		return nil, err
	}

	// If the token changed (was refreshed), persist it
	if token.AccessToken != p.current.AccessToken {
		p.logger.Info("Token was refreshed, persisting to database",
			zap.String("credential_id", p.credentialID),
		)
		p.oauthService.updateStoredToken(p.ctx, p.credentialID, token)
		p.current = token
	}

	return token, nil
}

// --- Internal helpers ---

func (s *OAuthService) getProviderConfig(provider string) *oauth2.Config {
	switch provider {
	case "google":
		return s.googleConfig
	case "microsoft":
		return s.msConfig
	default:
		return nil
	}
}

func (s *OAuthService) storeCredential(ctx context.Context, cred OAuthCredential) error {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		CREATE (c:OAuthCredential {
			id: $id,
			provider: $provider,
			user_id: $userId,
			name: $name,
			status: $status,
			access_token: $accessToken,
			refresh_token: $refreshToken,
			token_type: $tokenType,
			expiry: $expiry,
			created_at: $createdAt,
			updated_at: $updatedAt
		})
		RETURN c.id AS id
	`

	_, err := session.Run(ctx, query, map[string]interface{}{
		"id":           cred.ID,
		"provider":     cred.Provider,
		"userId":       cred.UserID,
		"name":         cred.Name,
		"status":       cred.Status,
		"accessToken":  cred.AccessToken,
		"refreshToken": cred.RefreshToken,
		"tokenType":    cred.TokenType,
		"expiry":       cred.Expiry.Format(time.RFC3339),
		"createdAt":    cred.CreatedAt.Format(time.RFC3339),
		"updatedAt":    cred.UpdatedAt.Format(time.RFC3339),
	})
	return err
}

func (s *OAuthService) getCredential(ctx context.Context, credentialID, userID string) (*OAuthCredential, error) {
	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (c:OAuthCredential {id: $id, user_id: $userId})
		RETURN c.id AS id, c.provider AS provider, c.user_id AS userId,
		       c.name AS name, c.status AS status,
		       c.access_token AS accessToken, c.refresh_token AS refreshToken,
		       c.token_type AS tokenType, c.expiry AS expiry,
		       c.created_at AS createdAt, c.updated_at AS updatedAt
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"id":     credentialID,
		"userId": userID,
	})
	if err != nil {
		return nil, err
	}

	if result.Next(ctx) {
		record := result.Record()
		cred := &OAuthCredential{}
		cred.ID, _ = getStringFromRecord(record, "id")
		cred.Provider, _ = getStringFromRecord(record, "provider")
		cred.UserID, _ = getStringFromRecord(record, "userId")
		cred.Name, _ = getStringFromRecord(record, "name")
		cred.Status, _ = getStringFromRecord(record, "status")
		cred.AccessToken, _ = getStringFromRecord(record, "accessToken")
		cred.RefreshToken, _ = getStringFromRecord(record, "refreshToken")
		cred.TokenType, _ = getStringFromRecord(record, "tokenType")

		if expiryStr, ok := getStringFromRecord(record, "expiry"); ok {
			cred.Expiry, _ = time.Parse(time.RFC3339, expiryStr)
		}
		return cred, nil
	}

	return nil, fmt.Errorf("credential not found")
}

func getStringFromRecord(record interface {
	Get(string) (interface{}, bool)
}, key string) (string, bool) {
	val, ok := record.Get(key)
	if !ok || val == nil {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

func (s *OAuthService) updateStoredToken(ctx context.Context, credentialID string, token *oauth2.Token) {
	encAccess, err := s.encrypt(token.AccessToken)
	if err != nil {
		s.logger.WithError(err).Error("Failed to encrypt updated access token")
		return
	}

	encRefresh := ""
	if token.RefreshToken != "" {
		encRefresh, err = s.encrypt(token.RefreshToken)
		if err != nil {
			s.logger.WithError(err).Error("Failed to encrypt updated refresh token")
			return
		}
	}

	session := s.neo4j.Session(ctx)
	defer session.Close(ctx)

	query := `
		MATCH (c:OAuthCredential {id: $id})
		SET c.access_token = $accessToken,
		    c.refresh_token = CASE WHEN $refreshToken <> '' THEN $refreshToken ELSE c.refresh_token END,
		    c.expiry = $expiry,
		    c.updated_at = $updatedAt
	`

	session.Run(ctx, query, map[string]interface{}{
		"id":           credentialID,
		"accessToken":  encAccess,
		"refreshToken": encRefresh,
		"expiry":       token.Expiry.Format(time.RFC3339),
		"updatedAt":    time.Now().Format(time.RFC3339),
	})
}

// AES-256-GCM encryption
func (s *OAuthService) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func (s *OAuthService) decrypt(cipherHex string) (string, error) {
	ciphertext, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// microsoftTokenExchange performs a manual token exchange for Microsoft OAuth
// to have full control over the request parameters and debugging
func (s *OAuthService) microsoftTokenExchange(cfg *oauth2.Config, code, codeVerifier string) (*oauth2.Token, error) {
	data := url.Values{
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"code":          {code},
		"redirect_uri":  {cfg.RedirectURL},
		"grant_type":    {"authorization_code"},
		"scope":         {strings.Join(cfg.Scopes, " ")},
	}
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}

	fmt.Printf("[OAUTH DEBUG] POST %s\n", cfg.Endpoint.TokenURL)
	fmt.Printf("[OAUTH DEBUG] redirect_uri=%s\n", cfg.RedirectURL)
	fmt.Printf("[OAUTH DEBUG] scope=%s\n", strings.Join(cfg.Scopes, " "))
	fmt.Printf("[OAUTH DEBUG] code_length=%d code_prefix=%s\n", len(code), code[:min(20, len(code))])
	fmt.Printf("[OAUTH DEBUG] has_pkce=%v client_id_length=%d client_secret_length=%d\n",
		codeVerifier != "", len(cfg.ClientID), len(cfg.ClientSecret))
	fmt.Printf("[OAUTH DEBUG] full form body (redacted secrets): %s\n", redactFormData(data))

	resp, err := http.PostForm(cfg.Endpoint.TokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("Microsoft token exchange HTTP error",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(body)),
		)
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &oauth2.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}

func redactFormData(data url.Values) string {
	redacted := url.Values{}
	for k, v := range data {
		switch k {
		case "client_secret":
			redacted.Set(k, fmt.Sprintf("[REDACTED len=%d]", len(v[0])))
		case "code":
			redacted.Set(k, fmt.Sprintf("[len=%d prefix=%s]", len(v[0]), v[0][:min(20, len(v[0]))]))
		default:
			redacted[k] = v
		}
	}
	return redacted.Encode()
}

func providerDisplayName(provider string) string {
	switch provider {
	case "google":
		return "Google"
	case "microsoft":
		return "Microsoft"
	default:
		return provider
	}
}

// OAuthConfig holds OAuth2 configuration for cloud drive providers
type OAuthConfig struct {
	GoogleClientID        string
	GoogleClientSecret    string
	MicrosoftClientID     string
	MicrosoftClientSecret string
	MicrosoftTenantID     string
	EncryptionKey         string
	RedirectBaseURL       string
}
