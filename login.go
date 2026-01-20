package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/auth"
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

// refreshOAuthToken wraps auth.RefreshOAuthToken for use with main.Config
func refreshOAuthToken(config *Config) bool {
	return auth.RefreshOAuthToken(&config.LLM)
}

// forceRefreshOAuthToken wraps auth.ForceRefreshOAuthToken
func forceRefreshOAuthToken(provider string) (string, error) {
	return auth.ForceRefreshOAuthToken(provider)
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

		// Reinitialize LLM and session with new credentials
		if err := m.reinitializeSession(); err != nil {
			m.commandLine.AddToast("Failed to initialize AI session: "+err.Error(), "error", 5000)
			return showOauthFailed{err.Error()}
		}

		// Update status line
		m.status.SetAgent(provider + " (" + m.config.LLM.Model + ")")
		m.content.Chat.AddMessage("Authenticated with " + provider + ", model: " + m.config.LLM.Model)
		m.commandLine.AddToast("Authentication saved", "info", 2500)
		m.sessionActive = true
		return nil
	}
}

// completeAnthropicOAuth completes the Anthropic OAuth flow with the authorization code
func (m *TUIModel) completeAnthropicOAuth(authCode, verifier string) tea.Cmd {
	return func() tea.Msg {
		authClient := &auth.AuthAnthropic{}

		// Exchange code for tokens
		m.commandLine.AddToast("Exchanging authorization code for tokens...", "success", 3000)
		m.content.Chat.AddMessage("")
		tokens, err := authClient.Exchange(authCode, verifier)
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

		// Reinitialize LLM and session with new credentials
		if err := m.reinitializeSession(); err != nil {
			m.commandLine.AddToast("Failed to initialize AI session: "+err.Error(), "error", 5000)
			return showOauthFailed{err.Error()}
		}

		// Update status and UI
		m.status.SetAgent("anthropic (" + m.config.LLM.Model + ")")
		m.commandLine.AddToast("✅ Anthropic Authenticated using Oauth", "info", 2500)
		m.sessionActive = true

		// Show model selection modal after successful authentication
		return showModelSelectionMsg{}
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

		// Clear the session
		model.session = nil
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
