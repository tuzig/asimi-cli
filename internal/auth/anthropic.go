package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/config"
)

// Anthropic OAuth constants
const (
	AnthropicClientID        = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	AnthropicAuthURL         = "https://claude.ai/oauth/authorize"
	AnthropicConsoleAuthURL  = "https://console.anthropic.com/oauth/authorize"
	AnthropicTokenURL        = "https://console.anthropic.com/v1/oauth/token"
	AnthropicRedirectURI     = "https://console.anthropic.com/oauth/code/callback"
	AnthropicScope           = "org:create_api_key user:profile user:inference"
	AnthropicAPIKeyCreateURL = "https://api.anthropic.com/api/oauth/claude_cli/create_api_key"
)

// AnthropicOAuthTokens represents the token response from Anthropic
type AnthropicOAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// AnthropicAPIKeyResponse represents the response from API key creation
type AnthropicAPIKeyResponse struct {
	APIKey string `json:"api_key"`
}

// AuthAnthropic provides Anthropic OAuth 2.0 authentication methods
type AuthAnthropic struct{}

// GeneratePKCE generates PKCE code verifier and challenge
func (a *AuthAnthropic) GeneratePKCE() (verifier, challenge string, err error) {
	// Generate 32 random bytes for verifier
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	verifier = base64.RawURLEncoding.EncodeToString(bytes)

	// Create SHA256 hash of verifier
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])

	return verifier, challenge, nil
}

// Authorize generates the authorization URL and returns it along with the PKCE verifier
func (a *AuthAnthropic) Authorize() (authURL, verifier string, err error) {
	verifier, challenge, err := a.GeneratePKCE()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate PKCE: %w", err)
	}

	// Build authorization URL
	params := url.Values{}
	params.Set("code", "true")
	params.Set("client_id", AnthropicClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", AnthropicRedirectURI)
	params.Set("scope", AnthropicScope)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", verifier) // Using verifier as state for simplicity

	authURL = AnthropicAuthURL + "?" + params.Encode()

	return authURL, verifier, nil
}

// Exchange exchanges the authorization code for tokens
func (a *AuthAnthropic) Exchange(authorizationCode, verifier string) (*AnthropicOAuthTokens, error) {
	// Parse authorization code (format: code#state)
	parts := strings.Split(authorizationCode, "#")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid authorization code format")
	}

	code := parts[0]
	state := parts[1]

	// Verify state matches verifier
	if state != verifier {
		return nil, fmt.Errorf("state mismatch")
	}

	// Prepare token request
	data := url.Values{}
	data.Set("code", code)
	data.Set("state", state)
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", AnthropicClientID)
	data.Set("redirect_uri", AnthropicRedirectURI)
	data.Set("code_verifier", verifier)

	req, err := http.NewRequest("POST", AnthropicTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokens AnthropicOAuthTokens
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	// Validate that we received valid tokens
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("Anthropic OAuth response did not contain an access token")
	}

	return &tokens, nil
}

// Access retrieves or refreshes the access token
func (a *AuthAnthropic) Access() (string, error) {
	// Try to get stored credentials
	tokenData, err := GetTokenFromKeyring("anthropic")
	if err != nil {
		return "", fmt.Errorf("failed to get tokens from keyring: %w", err)
	}

	if tokenData == nil {
		return "", fmt.Errorf("no stored credentials found")
	}

	// Check if token is still valid (with 5 minute buffer)
	if time.Now().Before(tokenData.Expiry.Add(-5 * time.Minute)) {
		return tokenData.AccessToken, nil
	}

	// Token expired, refresh it
	refreshedTokens, err := a.RefreshToken(tokenData.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}

	// Calculate new expiry
	expiry := time.Now().Add(time.Duration(refreshedTokens.ExpiresIn) * time.Second)

	// Update stored credentials
	slog.Debug("Saving token in access", "tokens", refreshedTokens)
	if err := SaveTokenToKeyring("anthropic", refreshedTokens.AccessToken, refreshedTokens.RefreshToken, expiry); err != nil {
		return "", fmt.Errorf("failed to save refreshed tokens: %w", err)
	}

	return refreshedTokens.AccessToken, nil
}

// RefreshToken refreshes an access token using a refresh token
func (a *AuthAnthropic) RefreshToken(refreshToken string) (*AnthropicOAuthTokens, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", AnthropicClientID)

	req, err := http.NewRequest("POST", AnthropicTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokens AnthropicOAuthTokens
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("failed to decode refresh response: %w", err)
	}

	// Validate that we received valid tokens
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("token refresh response did not contain an access token")
	}

	return &tokens, nil
}

// RefreshOAuthToken checks if the Anthropic OAuth token is expired
// and refreshes it if needed. Returns true if token was refreshed.
func RefreshOAuthToken(llmConfig *config.LLMConfig) bool {
	if llmConfig.Provider != "anthropic" {
		return false
	}

	tokenData, err := GetOauthToken("anthropic")
	if err != nil || tokenData == nil {
		return false
	}

	// Check if token is expired
	if !IsTokenExpired(tokenData) {
		return false
	}

	// Token expired - refresh it
	auth := &AuthAnthropic{}
	newAccessToken, refreshErr := auth.Access()
	if refreshErr == nil {
		// Successfully refreshed - update config with new token
		llmConfig.AuthToken = newAccessToken
		// Get updated token data from keyring (auth.Access() should have saved it)
		token2, err := GetOauthToken("anthropic")
		if err == nil && token2 != nil {
			llmConfig.RefreshToken = token2.RefreshToken
		}
		slog.Debug("Refreshed OAuth token")
		return true
	}

	slog.Warn("Failed to refresh OAuth token", "error", refreshErr)
	return false
}

// ForceRefreshOAuthToken forces a refresh of the Anthropic OAuth token,
// regardless of whether the local expiry time has passed.
// This is used when the API returns a 401 error, indicating the server
// has invalidated the token even if it hasn't locally expired.
// Returns the new access token and an error if refresh fails.
func ForceRefreshOAuthToken(provider string) (string, error) {
	if provider != "anthropic" {
		return "", fmt.Errorf("force refresh only supported for anthropic provider")
	}

	tokenData, err := GetOauthToken("anthropic")
	if err != nil {
		return "", fmt.Errorf("failed to get token from keyring: %w", err)
	}
	if tokenData == nil {
		return "", fmt.Errorf("no stored credentials found")
	}

	if tokenData.RefreshToken == "" {
		return "", fmt.Errorf("no refresh token available")
	}

	// Force refresh the token using the refresh token
	auth := &AuthAnthropic{}
	refreshedTokens, err := auth.RefreshToken(tokenData.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}

	// Calculate new expiry
	expiry := time.Now().Add(time.Duration(refreshedTokens.ExpiresIn) * time.Second)

	// Save the new tokens to keyring
	if err := SaveTokenToKeyring("anthropic", refreshedTokens.AccessToken, refreshedTokens.RefreshToken, expiry); err != nil {
		return "", fmt.Errorf("failed to save refreshed tokens: %w", err)
	}

	slog.Info("Force refreshed OAuth token successfully")
	return refreshedTokens.AccessToken, nil
}
