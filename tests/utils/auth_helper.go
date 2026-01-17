package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AuthHelper provides authentication utilities for testing
type AuthHelper struct {
	KeycloakURL  string
	Realm        string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
}

// NewAuthHelper creates a new authentication helper
func NewAuthHelper(keycloakURL, realm, clientID, clientSecret string) *AuthHelper {
	return &AuthHelper{
		KeycloakURL:  keycloakURL,
		Realm:        realm,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// TokenResponse represents an OAuth2 token response
type TokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	Scope            string `json:"scope"`
}

// UserInfo represents user information from Keycloak
type UserInfo struct {
	ID                string                 `json:"sub"`
	Username          string                 `json:"username"`
	Email             string                 `json:"email"`
	EmailVerified     bool                   `json:"email_verified"`
	Name              string                 `json:"name"`
	GivenName         string                 `json:"given_name"`
	FamilyName        string                 `json:"family_name"`
	Roles             []string               `json:"roles"`
	Groups            []string               `json:"groups"`
	RealmAccess       map[string]interface{} `json:"realm_access"`
	ResourceAccess    map[string]interface{} `json:"resource_access"`
	SessionState      string                 `json:"session_state"`
	PreferredUsername string                 `json:"preferred_username"`
}

// TestUser represents a test user for authentication
type TestUser struct {
	Username    string
	Password    string
	Email       string
	FirstName   string
	LastName    string
	Roles       []string
	AccessToken string
	UserInfo    *UserInfo
}

// GetTestUsers returns predefined test users for different scenarios
func GetTestUsers() map[string]TestUser {
	return map[string]TestUser{
		"admin": {
			Username:  "test_admin",
			Password:  "test_password_123",
			Email:     "test.admin@example.com",
			FirstName: "Test",
			LastName:  "Administrator",
			Roles:     []string{"admin", "user"},
		},
		"user": {
			Username:  "test_user",
			Password:  "test_password_123",
			Email:     "test.user@example.com",
			FirstName: "Test",
			LastName:  "User",
			Roles:     []string{"user"},
		},
		"readonly": {
			Username:  "test_readonly",
			Password:  "test_password_123",
			Email:     "test.readonly@example.com",
			FirstName: "Test",
			LastName:  "ReadOnly",
			Roles:     []string{"readonly"},
		},
	}
}

// GetClientCredentialsToken gets a token using client credentials flow
func (ah *AuthHelper) GetClientCredentialsToken(ctx context.Context) (*TokenResponse, error) {
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", ah.KeycloakURL, ah.Realm)
	
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", ah.ClientID)
	data.Set("client_secret", ah.ClientSecret)
	
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request failed with status: %d", resp.StatusCode)
	}
	
	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}
	
	return &token, nil
}

// GetPasswordToken gets a token using password flow (for testing only)
func (ah *AuthHelper) GetPasswordToken(ctx context.Context, username, password string) (*TokenResponse, error) {
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", ah.KeycloakURL, ah.Realm)
	
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("client_id", ah.ClientID)
	data.Set("client_secret", ah.ClientSecret)
	data.Set("username", username)
	data.Set("password", password)
	
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("password token request failed with status: %d", resp.StatusCode)
	}
	
	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}
	
	return &token, nil
}

// GetUserInfo retrieves user information using an access token
func (ah *AuthHelper) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	userInfoURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/userinfo", ah.KeycloakURL, ah.Realm)
	
	req, err := http.NewRequestWithContext(ctx, "GET", userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+accessToken)
	
	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo request failed with status: %d", resp.StatusCode)
	}
	
	var userInfo UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("failed to decode userinfo response: %w", err)
	}
	
	return &userInfo, nil
}

// CreateTestUser creates a test user in Keycloak (requires admin privileges)
func (ah *AuthHelper) CreateTestUser(ctx context.Context, adminToken string, user TestUser) error {
	usersURL := fmt.Sprintf("%s/admin/realms/%s/users", ah.KeycloakURL, ah.Realm)
	
	userData := map[string]interface{}{
		"username":      user.Username,
		"email":         user.Email,
		"firstName":     user.FirstName,
		"lastName":      user.LastName,
		"enabled":       true,
		"emailVerified": true,
		"credentials": []map[string]interface{}{
			{
				"type":      "password",
				"value":     user.Password,
				"temporary": false,
			},
		},
	}
	
	jsonData, err := json.Marshal(userData)
	if err != nil {
		return fmt.Errorf("failed to marshal user data: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", usersURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create user request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	
	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("create user request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create user failed with status: %d", resp.StatusCode)
	}
	
	return nil
}

// DeleteTestUser deletes a test user from Keycloak (requires admin privileges)
func (ah *AuthHelper) DeleteTestUser(ctx context.Context, adminToken, username string) error {
	// First, find the user ID
	userID, err := ah.findUserIDByUsername(ctx, adminToken, username)
	if err != nil {
		return fmt.Errorf("failed to find user %s: %w", username, err)
	}
	
	if userID == "" {
		return nil // User doesn't exist, nothing to delete
	}
	
	// Delete the user
	deleteURL := fmt.Sprintf("%s/admin/realms/%s/users/%s", ah.KeycloakURL, ah.Realm, userID)
	
	req, err := http.NewRequestWithContext(ctx, "DELETE", deleteURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+adminToken)
	
	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete user request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete user failed with status: %d", resp.StatusCode)
	}
	
	return nil
}

// AuthenticateTestUser authenticates a test user and returns token info
func (ah *AuthHelper) AuthenticateTestUser(ctx context.Context, userType string) (*TestUser, error) {
	testUsers := GetTestUsers()
	user, exists := testUsers[userType]
	if !exists {
		return nil, fmt.Errorf("unknown test user type: %s", userType)
	}
	
	// Get access token
	token, err := ah.GetPasswordToken(ctx, user.Username, user.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to get token for user %s: %w", user.Username, err)
	}
	
	user.AccessToken = token.AccessToken
	
	// Get user info
	userInfo, err := ah.GetUserInfo(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info for %s: %w", user.Username, err)
	}
	
	user.UserInfo = userInfo
	
	return &user, nil
}

// ValidateToken validates an access token and returns user info
func (ah *AuthHelper) ValidateToken(ctx context.Context, accessToken string) (*UserInfo, error) {
	return ah.GetUserInfo(ctx, accessToken)
}

// RefreshToken refreshes an access token using a refresh token
func (ah *AuthHelper) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", ah.KeycloakURL, ah.Realm)
	
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", ah.ClientID)
	data.Set("client_secret", ah.ClientSecret)
	data.Set("refresh_token", refreshToken)
	
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh token failed with status: %d", resp.StatusCode)
	}
	
	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode refresh token response: %w", err)
	}
	
	return &token, nil
}

// Helper function to find user ID by username
func (ah *AuthHelper) findUserIDByUsername(ctx context.Context, adminToken, username string) (string, error) {
	usersURL := fmt.Sprintf("%s/admin/realms/%s/users?username=%s", ah.KeycloakURL, ah.Realm, username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", usersURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create search request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+adminToken)
	
	resp, err := ah.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search user request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("search user failed with status: %d", resp.StatusCode)
	}
	
	var users []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return "", fmt.Errorf("failed to decode users response: %w", err)
	}
	
	if len(users) == 0 {
		return "", nil // User not found
	}
	
	userID, ok := users[0]["id"].(string)
	if !ok {
		return "", fmt.Errorf("user ID not found in response")
	}
	
	return userID, nil
}

// GenerateTestJWT generates a test JWT token for non-Keycloak scenarios
func GenerateTestJWT(userID, username, email string, roles []string) string {
	// This is a simplified JWT for testing purposes only
	// In real tests, you would use a proper JWT library
	payload := map[string]interface{}{
		"sub":       userID,
		"username":  username,
		"email":     email,
		"roles":     roles,
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(time.Hour).Unix(),
		"iss":       "aether-test",
	}
	
	// For testing purposes, return a base64 encoded JSON
	// Real implementation would use proper JWT signing
	jsonData, _ := json.Marshal(payload)
	return fmt.Sprintf("test.%s.signature", strings.ReplaceAll(string(jsonData), "=", ""))
}