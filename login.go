package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Message types for login flow
type providerSelectedMsg struct {
	provider *Provider
}

type modalCancelledMsg struct{}
type showOauthFailed struct{ err string }

type authCodeEnteredMsg struct {
	code     string
	verifier string
}

type urlCopiedToClipboardMsg struct {
	url string
	err error
}

// Provider represents an authentication provider
type Provider struct {
	Name        string
	Description string
	Key         string
}

// BaseModal represents a base modal dialog
type BaseModal struct {
	Title   string
	Content string
	Width   int
	Height  int
	Style   lipgloss.Style
}

// NewBaseModal creates a new base modal
func NewBaseModal(title, content string, width, height int) *BaseModal {
	return &BaseModal{
		Title:   title,
		Content: content,
		Width:   width,
		Height:  height,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Width(width).
			Height(height).
			Align(lipgloss.Center, lipgloss.Center),
	}
}

// Render renders the modal
func (m *BaseModal) Render() string {
	// Create title style
	titleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1).
		Width(m.Width - 2) // Account for border

	title := titleStyle.Render(m.Title)
	content := lipgloss.NewStyle().
		Width(m.Width-2).
		Height(m.Height-4). // Account for title and borders
		Align(lipgloss.Left, lipgloss.Center).
		Render(m.Content)

	// Combine title and content
	body := lipgloss.JoinVertical(lipgloss.Center, title, content)

	return m.Style.Render(body)
}

// ProviderSelectionModal represents a modal for selecting authentication providers
type ProviderSelectionModal struct {
	*BaseModal
	providers        []Provider
	selected         int
	confirmed        bool
	selectedProvider *Provider
}

// NewProviderSelectionModal creates a new provider selection modal
func NewProviderSelectionModal() *ProviderSelectionModal {
	providers := []Provider{
		{Name: "Anthropic (Claude)", Description: "Claude Pro/Max", Key: "anthropic"},
		{Name: "OpenAI", Description: "GPT models", Key: "openai"},
		{Name: "Google AI", Description: "Gemini models", Key: "googleai"},
	}

	baseModal := NewBaseModal("Select Authentication Provider", "", 60, 12)

	return &ProviderSelectionModal{
		BaseModal:        baseModal,
		providers:        providers,
		selected:         0,
		confirmed:        false,
		selectedProvider: nil,
	}
}

// Render renders the provider selection modal
func (m *ProviderSelectionModal) Render() string {
	var content string

	content += "Use ↑/↓ arrows to navigate, Enter to select, Esc/Q to cancel\n\n"

	for i, provider := range m.providers {
		prefix := "  "
		if i == m.selected {
			prefix = "▶ "
		}

		style := lipgloss.NewStyle()
		if i == m.selected {
			style = style.Foreground(lipgloss.Color("62")).Bold(true)
		}

		line := fmt.Sprintf("%s%s", prefix, provider.Name)
		if provider.Description != "" {
			line += fmt.Sprintf(" - %s", provider.Description)
		}

		content += style.Render(line) + "\n"
	}

	// Update the base modal's content
	m.BaseModal.Content = content
	return m.BaseModal.Render()
}

// Update handles key events for provider selection
func (m *ProviderSelectionModal) Update(msg tea.Msg) (*ProviderSelectionModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.providers)-1 {
				m.selected++
			}
		case "enter":
			m.confirmed = true
			m.selectedProvider = &m.providers[m.selected]
			return m, func() tea.Msg { return providerSelectedMsg{provider: m.selectedProvider} }
		case "esc", "q":
			// Close the modal without selecting anything
			return m, func() tea.Msg { return modalCancelledMsg{} }
		}
	}
	return m, nil
}

// OAuthItemType represents the type of item in the OAuth window
type OAuthItemType int

const (
	OAuthItemToken OAuthItemType = iota
	OAuthItemCopyURL
)

// CodeInputModal represents a modal for inputting authorization codes
type CodeInputModal struct {
	*BaseModal
	textInput    textinput.Model
	authURL      string
	verifier     string
	confirmed    bool
	selectedItem OAuthItemType // Which item is currently selected (token input or copy URL)
}

// NewCodeInputModal creates a new code input modal
func NewCodeInputModal(authURL, verifier string) *CodeInputModal {
	baseModal := NewBaseModal("Enter Authorization Code", "", 90, 18)

	// Log the authorization URL for easy copying (especially useful for remote sessions)
	slog.Debug("Anthropic OAuth Authorization URL", "url", authURL)

	// Create text input using bubbles textinput
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "Paste authorization code here..."
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 60

	return &CodeInputModal{
		BaseModal:    baseModal,
		textInput:    ti,
		authURL:      authURL,
		verifier:     verifier,
		confirmed:    false,
		selectedItem: OAuthItemToken, // Start with token input selected
	}
}

// Render renders the code input modal
func (m *CodeInputModal) Render() string {
	content := "Browser opened for Anthropic OAuth.\n\n"
	content += "1. Authorize in the browser\n"
	content += "2. Copy the authorization code shown after redirect\n"
	content += "3. Paste it below\n\n"

	// Token input field
	tokenPrefix := "  "
	if m.selectedItem == OAuthItemToken {
		tokenPrefix = "▶ "
	}

	content += fmt.Sprintf("%sToken: %s", tokenPrefix, m.textInput.View()) + "\n"

	// Copy URL option
	copyPrefix := "  "
	if m.selectedItem == OAuthItemCopyURL {
		copyPrefix = "▶ "
	}
	content += copyPrefix + "Copy Anthropic's url to the clipboard\n\n"

	content += "j/k to navigate | Enter to select/submit | Esc to cancel"

	m.BaseModal.Content = content
	return m.BaseModal.Render()
}

// Update handles key events for code input
func (m *CodeInputModal) Update(msg tea.Msg) (*CodeInputModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		keyStr := msg.String()

		// Handle navigation between items (j/k or up/down)
		switch keyStr {
		case "j", "down":
			if m.selectedItem == OAuthItemToken {
				m.selectedItem = OAuthItemCopyURL
				m.textInput.Blur()
			}
			return m, nil
		case "k", "up":
			if m.selectedItem == OAuthItemCopyURL {
				m.selectedItem = OAuthItemToken
				m.textInput.Focus()
			}
			return m, nil
		case "esc", "ctrl+c":
			return m, func() tea.Msg { return modalCancelledMsg{} }
		case "enter", "ctrl+m":
			// Handle enter based on selected item
			if m.selectedItem == OAuthItemCopyURL {
				m.selectedItem = OAuthItemToken
				// Copy URL to clipboard - the actual copy happens in the command
				url := m.authURL
				return m, func() tea.Msg {
					err := clipboard.WriteAll(url)
					return urlCopiedToClipboardMsg{url: url, err: err}
				}
			}
			// Token input - submit if not empty
			if strings.TrimSpace(m.textInput.Value()) != "" {
				m.confirmed = true
				return m, func() tea.Msg {
					return authCodeEnteredMsg{
						code:     strings.TrimSpace(m.textInput.Value()),
						verifier: m.verifier,
					}
				}
			}
			return m, nil
		}

		// Only handle text input keys when token input is selected
		if m.selectedItem == OAuthItemToken {
			// Let textinput handle all text editing
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// Anthropic OAuth constants and types
const (
	anthropicClientID        = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	anthropicAuthURL         = "https://claude.ai/oauth/authorize"
	anthropicConsoleAuthURL  = "https://console.anthropic.com/oauth/authorize"
	anthropicTokenURL        = "https://console.anthropic.com/v1/oauth/token"
	anthropicRedirectURI     = "https://console.anthropic.com/oauth/code/callback"
	anthropicScope           = "org:create_api_key user:profile user:inference"
	anthropicAPIKeyCreateURL = "https://api.anthropic.com/api/oauth/claude_cli/create_api_key"
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

// generatePKCE generates PKCE code verifier and challenge
func (a *AuthAnthropic) generatePKCE() (verifier, challenge string, err error) {
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

// authorize generates the authorization URL and returns it along with the PKCE verifier
func (a *AuthAnthropic) authorize() (authURL, verifier string, err error) {
	verifier, challenge, err := a.generatePKCE()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate PKCE: %w", err)
	}

	// Build authorization URL
	params := url.Values{}
	params.Set("code", "true")
	params.Set("client_id", anthropicClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", anthropicRedirectURI)
	params.Set("scope", anthropicScope)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", verifier) // Using verifier as state for simplicity

	authURL = anthropicAuthURL + "?" + params.Encode()

	return authURL, verifier, nil
}

// exchange exchanges the authorization code for tokens
func (a *AuthAnthropic) exchange(authorizationCode, verifier string) (*AnthropicOAuthTokens, error) {
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
	data.Set("client_id", anthropicClientID)
	data.Set("redirect_uri", anthropicRedirectURI)
	data.Set("code_verifier", verifier)

	req, err := http.NewRequest("POST", anthropicTokenURL, strings.NewReader(data.Encode()))
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

// access retrieves or refreshes the access token
func (a *AuthAnthropic) access() (string, error) {
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
	refreshedTokens, err := a.refreshToken(tokenData.RefreshToken)
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

// refreshToken refreshes an access token using a refresh token
func (a *AuthAnthropic) refreshToken(refreshToken string) (*AnthropicOAuthTokens, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", anthropicClientID)

	req, err := http.NewRequest("POST", anthropicTokenURL, strings.NewReader(data.Encode()))
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

// refreshOAuthToken checks if the Anthropic OAuth token is expired
// and refreshes it if needed. Returns true if token was refreshed.
func refreshOAuthToken(config *Config) bool {
	if config.LLM.Provider != "anthropic" {
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
	newAccessToken, refreshErr := auth.access()
	if refreshErr == nil {
		// Successfully refreshed - update config with new token
		config.LLM.AuthToken = newAccessToken
		// Get updated token data from keyring (auth.access() should have saved it)
		token2, err := GetOauthToken("anthropic")
		if err == nil && token2 != nil {
			config.LLM.RefreshToken = token2.RefreshToken
		}
		slog.Debug("Refreshed OAuth token")
		return true
	}

	slog.Warn("Failed to refresh OAuth token", "error", refreshErr)
	return false
}

// forceRefreshOAuthToken forces a refresh of the Anthropic OAuth token,
// regardless of whether the local expiry time has passed.
// This is used when the API returns a 401 error, indicating the server
// has invalidated the token even if it hasn't locally expired.
// Returns the new access token and an error if refresh fails.
func forceRefreshOAuthToken(provider string) (string, error) {
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
	refreshedTokens, err := auth.refreshToken(tokenData.RefreshToken)
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

// performOAuthLogin performs OAuth login for non-Anthropic providers
func (m *TUIModel) performOAuthLogin(provider string) tea.Cmd {
	return func() tea.Msg {
		// Set default model based on provider
		var selModel string
		switch provider {
		case "openai":
			selModel = "gpt-4o-mini"
		case "googleai":
			selModel = "gemini-2.5-flash"
		default:
			selModel = "gpt-4o-mini"
		}

		// Update in-memory config
		m.config.LLM.Provider = provider
		m.config.LLM.Model = selModel

		// Run generic OAuth2 loopback flow for other providers
		token, refresh, expiry, err := runOAuthLoopback(provider)
		if err != nil {
			return showOauthFailed{err.Error()}
		}

		// Save tokens
		m.config.LLM.AuthToken = token
		m.config.LLM.RefreshToken = refresh
		slog.Debug("In performaOAuthLogin", "auth token", token, "refresh", refresh)
		if err := UpdateUserOAuthTokens(provider, token, refresh, expiry); err != nil {
			m.commandLine.AddToast("Authorized, but failed to persist token", "error", 4000)
		}

		// Initialize LLM with new credentials
		model, err := getModelClient(m.config)
		if err != nil {
			return showOauthFailed{"Failed to initialize AI session: " + err.Error()}
		}

		return llmInitSuccessMsg{model: model}
	}
}

// completeAnthropicOAuth completes the Anthropic OAuth flow with the authorization code
func (m *TUIModel) completeAnthropicOAuth(authCode, verifier string) tea.Cmd {
	return func() tea.Msg {
		auth := &AuthAnthropic{}

		// Exchange code for tokens
		m.commandLine.AddToast("Exchanging authorization code for tokens...", "success", 3000)
		m.content.Chat.AddMessage("")
		tokens, err := auth.exchange(authCode, verifier)
		if err != nil {
			return showOauthFailed{fmt.Sprintf("failed to exchange authorization code: %v", err)}
		}

		// Calculate expiry time
		expiry := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)

		// Store tokens securely in keyring and update config file
		slog.Debug("In compleseAnthropicOAuth", "token", tokens)
		if err := UpdateUserOAuthTokens("anthropic", tokens.AccessToken, tokens.RefreshToken, expiry); err != nil {
			return showOauthFailed{fmt.Sprintf("failed to save tokens: %v", err)}
		}

		// Update in-memory config with the new tokens
		m.config.LLM.Provider = "anthropic"
		m.config.LLM.AuthToken = tokens.AccessToken
		m.config.LLM.RefreshToken = tokens.RefreshToken
		if m.config.LLM.Model == "" {
			m.config.LLM.Model = "claude-3-5-sonnet-20241022"
		}

		// Initialize LLM with new credentials
		model, err := getModelClient(m.config)
		if err != nil {
			return showOauthFailed{"Failed to initialize AI session: " + err.Error()}
		}

		return llmInitSuccessMsg{model: model}
	}
}
func runOAuthLoopback(provider string) (accessToken, refreshToken string, expiry time.Time, err error) {
	cfg, err := getOAuthConfig(provider)
	if err != nil {
		return "", "", time.Time{}, err
	}

	// Start loopback server
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// PKCE
	codeVerifier := randomString(64)
	sum := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomString(32)

	// Build auth URL
	u, err := url.Parse(cfg.AuthURL)
	if err != nil {
		return "", "", time.Time{}, err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	if len(cfg.Scopes) > 0 {
		q.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()

	// Serve callback
	codeCh := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("<html><body><h2>Authorization complete. You can close this window.</h2></body></html>"))
		go func() { codeCh <- code }()
	})}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("oauth callback server error: %v", err)
		}
	}()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// Open browser
	_ = openBrowser(u.String())

	// Wait for code
	var code string
	select {
	case code = <-codeCh:
	case <-time.After(5 * time.Minute):
		return "", "", time.Time{}, fmt.Errorf("authorization timed out")
	}

	// Exchange code for token
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequest("POST", cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", time.Time{}, fmt.Errorf("token exchange failed: %s", resp.Status)
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		IdToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", "", time.Time{}, err
	}

	// Validate that we received valid tokens
	if tok.AccessToken == "" {
		return "", "", time.Time{}, fmt.Errorf("OAuth response did not contain an access token")
	}

	exp := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return tok.AccessToken, tok.RefreshToken, exp, nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported OS for auto-open browser")
	}
}
func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// fallback
		return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

type oauthProviderConfig struct {
	AuthURL      string
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// UpdateUserLLMAuth updates or creates ~/.config/asimi/asimi.conf with the given LLM auth settings.
// It saves API keys securely in the keyring and only stores provider/model in the config file.
// This function preserves all comments in the existing config file.
func UpdateUserLLMAuth(provider, apiKey, model string) error {
	// Save API key securely in keyring
	if err := SaveAPIKeyToKeyring(provider, apiKey); err != nil {
		// Fall back to file storage with warning
		slog.Warn("Failed to save API key to keyring, falling back to file storage", "error", err)
		return updateAPIKeyInFile(provider, apiKey, model)
	}

	cfgDir, cfgPath, err := config.UserConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Read existing content or start with empty
	var content string
	if data, err := os.ReadFile(cfgPath); err == nil {
		content = string(data)
	}

	// Update values using comment-preserving helpers
	content = config.UpdateOrInsertTOMLValue(content, "llm", "provider", provider)
	content = config.UpdateOrInsertTOMLValue(content, "llm", "model", model)
	content = config.UpdateOrInsertTOMLValue(content, "llm", "auth_method", "apikey_keyring")

	// Remove plaintext API key if it exists (we're using keyring now)
	content = config.RemoveTOMLKey(content, "llm", "api_key")

	return os.WriteFile(cfgPath, []byte(content), 0o600)
}

// updateAPIKeyInFile is the fallback method for storing API keys in file (less secure).
// This function preserves all comments in the existing config file.
func updateAPIKeyInFile(provider, apiKey, model string) error {
	cfgDir, cfgPath, err := config.UserConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Read existing content or start with empty
	var content string
	if data, err := os.ReadFile(cfgPath); err == nil {
		content = string(data)
	}

	// Update values using comment-preserving helpers
	content = config.UpdateOrInsertTOMLValue(content, "llm", "provider", provider)
	content = config.UpdateOrInsertTOMLValue(content, "llm", "model", model)
	content = config.UpdateOrInsertTOMLValue(content, "llm", "api_key", apiKey)
	content = config.UpdateOrInsertTOMLValue(content, "llm", "auth_method", "apikey_file")

	return os.WriteFile(cfgPath, []byte(content), 0o600)
}

// UpdateUserOAuthTokens saves OAuth tokens securely in the OS keyring and updates provider in config.
// This function preserves all comments in the existing config file.
func UpdateUserOAuthTokens(provider, accessToken, refreshToken string, expiry time.Time) error {
	// Save tokens securely in keyring
	if err := SaveTokenToKeyring(provider, accessToken, refreshToken, expiry); err != nil {
		// Fall back to file storage with warning
		slog.Warn("Failed to save tokens to keyring, falling back to file storage", "error", err)
		return updateOAuthTokensInFile(provider, accessToken, refreshToken)
	}

	// Only save provider info in the config file (not the tokens)
	cfgDir, cfgPath, err := config.UserConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Read existing content or start with empty
	var content string
	if data, err := os.ReadFile(cfgPath); err == nil {
		content = string(data)
	}

	// Update values using comment-preserving helpers
	content = config.UpdateOrInsertTOMLValue(content, "llm", "provider", provider)

	// Remove any plaintext tokens from config if they exist (we're using keyring now)
	content = config.RemoveTOMLKey(content, "llm", "auth_token")
	content = config.RemoveTOMLKey(content, "llm", "refresh_token")

	return os.WriteFile(cfgPath, []byte(content), 0o600)
}

// updateOAuthTokensInFile is the fallback method for storing tokens in file (less secure).
// This function preserves all comments in the existing config file.
func updateOAuthTokensInFile(provider, accessToken, refreshToken string) error {
	cfgDir, cfgPath, err := config.UserConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Read existing content or start with empty
	var content string
	if data, err := os.ReadFile(cfgPath); err == nil {
		content = string(data)
	}

	// Update values using comment-preserving helpers
	content = config.UpdateOrInsertTOMLValue(content, "llm", "provider", provider)
	content = config.UpdateOrInsertTOMLValue(content, "llm", "auth_method", "oauth_file")
	content = config.UpdateOrInsertTOMLValue(content, "llm", "auth_token", accessToken)
	if refreshToken != "" {
		content = config.UpdateOrInsertTOMLValue(content, "llm", "refresh_token", refreshToken)
	}

	return os.WriteFile(cfgPath, []byte(content), 0o600)
}

func getOAuthConfig(provider string) (oauthProviderConfig, error) {
	p := oauthProviderConfig{}
	switch provider {
	case "googleai":
		// Use standard Google environment variable names
		p.AuthURL = config.GetEnv("GOOGLE_AUTH_URL", "https://accounts.google.com/o/oauth2/v2/auth")
		p.TokenURL = config.GetEnv("GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token")
		p.ClientID = os.Getenv("GOOGLE_CLIENT_ID")
		p.ClientSecret = os.Getenv("GOOGLE_CLIENT_SECRET")
		scopes := os.Getenv("GOOGLE_OAUTH_SCOPES")
		if scopes == "" {
			// Default to the Generative Language scope
			p.Scopes = []string{"https://www.googleapis.com/auth/generative-language"}
		} else {
			p.Scopes = strings.Split(scopes, ",")
		}
	case "openai":
		// Use standard OpenAI environment variable names
		p.AuthURL = os.Getenv("OPENAI_AUTH_URL")
		p.TokenURL = os.Getenv("OPENAI_TOKEN_URL")
		p.ClientID = os.Getenv("OPENAI_CLIENT_ID")
		p.ClientSecret = os.Getenv("OPENAI_CLIENT_SECRET")
		scopes := os.Getenv("OPENAI_OAUTH_SCOPES")
		if scopes != "" {
			p.Scopes = strings.Split(scopes, ",")
		}
	case "anthropic":
		// Use standard Anthropic environment variable names
		p.AuthURL = os.Getenv("ANTHROPIC_AUTH_URL")
		p.TokenURL = os.Getenv("ANTHROPIC_TOKEN_URL")
		p.ClientID = os.Getenv("ANTHROPIC_CLIENT_ID")
		p.ClientSecret = os.Getenv("ANTHROPIC_CLIENT_SECRET")
		scopes := os.Getenv("ANTHROPIC_OAUTH_SCOPES")
		if scopes != "" {
			p.Scopes = strings.Split(scopes, ",")
		}
	default:
		return p, fmt.Errorf("unsupported provider for oauth: %s", provider)
	}
	if p.AuthURL == "" || p.TokenURL == "" || p.ClientID == "" {
		providerName := strings.ToUpper(provider)
		if provider == "googleai" {
			providerName = "GOOGLE"
		}
		return p, fmt.Errorf("OAuth not configured. Set %s_CLIENT_ID, %s_CLIENT_SECRET, %s_AUTH_URL, and %s_TOKEN_URL",
			providerName, providerName, providerName, providerName)
	}
	return p, nil
}

// handleLogoutCommand handles the :logout command
func handleLogoutCommand(model *TUIModel, args []string) tea.Cmd {
	return func() tea.Msg {
		provider := model.config.LLM.Provider
		if provider == "" {
			return showSystemMsg("No provider configured. Nothing to logout from.")
		}

		var errors []string

		// Delete OAuth tokens from keyring
		if err := DeleteTokenFromKeyring(provider); err != nil {
			slog.Warn("Failed to delete OAuth token from keyring", "provider", provider, "error", err)
			errors = append(errors, fmt.Sprintf("OAuth token: %v", err))
		} else {
			slog.Debug("Deleted OAuth token from keyring", "provider", provider)
		}

		// Delete API key from keyring
		if err := DeleteAPIKeyFromKeyring(provider); err != nil {
			slog.Warn("Failed to delete API key from keyring", "provider", provider, "error", err)
			errors = append(errors, fmt.Sprintf("API key: %v", err))
		} else {
			slog.Debug("Deleted API key from keyring", "provider", provider)
		}

		// Clear in-memory credentials
		model.config.LLM.AuthToken = ""
		model.config.LLM.RefreshToken = ""
		model.config.LLM.APIKey = ""

		// Clear the session state
		model.currentEdictID = ""
		model.sessionActive = false

		// Update status line
		model.status.SetAgent("not configured")

		// Build result message
		msg := NewChatMsgBuilder(systemPrefix)
		msg.WriteLnf("Logged out from %s", provider)

		if len(errors) > 0 {
			msg.WriteLn("")
			msg.WriteLn("Some credentials could not be removed:")
			for _, e := range errors {
				msg.WriteLnf("  • %s", e)
			}
		}

		msg.WriteLn("")
		msg.WriteLn("Use :models to authenticate with a new provider.")

		return showContextMsg{content: msg.String()}
	}
}
