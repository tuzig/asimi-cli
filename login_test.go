package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeInputModal_NewCodeInputModal(t *testing.T) {
	authURL := "https://example.com/oauth"
	verifier := "test-verifier"

	modal := NewCodeInputModal(authURL, verifier)

	if modal.authURL != authURL {
		t.Errorf("expected authURL %q, got %q", authURL, modal.authURL)
	}
	if modal.verifier != verifier {
		t.Errorf("expected verifier %q, got %q", verifier, modal.verifier)
	}
	if modal.selectedItem != OAuthItemToken {
		t.Errorf("expected selectedItem to be OAuthItemToken, got %v", modal.selectedItem)
	}
	if modal.textInput.Value() != "" {
		t.Errorf("expected empty input, got %q", modal.textInput.Value())
	}
}

func TestCodeInputModal_Navigation(t *testing.T) {
	modal := NewCodeInputModal("https://example.com", "verifier")

	// Initially on token input
	if modal.selectedItem != OAuthItemToken {
		t.Errorf("expected to start on OAuthItemToken")
	}

	// Navigate down to copy URL
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if modal.selectedItem != OAuthItemCopyURL {
		t.Errorf("expected OAuthItemCopyURL after j, got %v", modal.selectedItem)
	}

	// Navigate down again (should stay on copy URL)
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if modal.selectedItem != OAuthItemCopyURL {
		t.Errorf("expected to stay on OAuthItemCopyURL, got %v", modal.selectedItem)
	}

	// Navigate up to token input
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if modal.selectedItem != OAuthItemToken {
		t.Errorf("expected OAuthItemToken after k, got %v", modal.selectedItem)
	}

	// Navigate up again (should stay on token)
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if modal.selectedItem != OAuthItemToken {
		t.Errorf("expected to stay on OAuthItemToken, got %v", modal.selectedItem)
	}

	// Test with arrow keys
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyDown})
	if modal.selectedItem != OAuthItemCopyURL {
		t.Errorf("expected OAuthItemCopyURL after down arrow, got %v", modal.selectedItem)
	}

	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyUp})
	if modal.selectedItem != OAuthItemToken {
		t.Errorf("expected OAuthItemToken after up arrow, got %v", modal.selectedItem)
	}
}

func TestCodeInputModal_TextInput(t *testing.T) {
	modal := NewCodeInputModal("https://example.com", "verifier")

	// Type some text
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})

	if modal.textInput.Value() != "abc" {
		t.Errorf("expected input 'abc', got %q", modal.textInput.Value())
	}

	// Backspace
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if modal.textInput.Value() != "ab" {
		t.Errorf("expected input 'ab' after backspace, got %q", modal.textInput.Value())
	}

	// Move cursor left and insert
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyLeft})
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	if modal.textInput.Value() != "aXb" {
		t.Errorf("expected input 'aXb', got %q", modal.textInput.Value())
	}
}

func TestCodeInputModal_TextInputOnlyWhenTokenSelected(t *testing.T) {
	modal := NewCodeInputModal("https://example.com", "verifier")

	// Navigate to copy URL
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	// Try to type - should not work
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if modal.textInput.Value() != "" {
		t.Errorf("expected empty input when on copy URL, got %q", modal.textInput.Value())
	}

	// Navigate back to token
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})

	// Now typing should work
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if modal.textInput.Value() != "a" {
		t.Errorf("expected input 'a' when on token, got %q", modal.textInput.Value())
	}
}

func TestCodeInputModal_EnterOnToken(t *testing.T) {
	modal := NewCodeInputModal("https://example.com", "test-verifier")

	// Type a code using the textInput
	modal.textInput.SetValue("CODE123#STATE456")

	// Press enter
	_, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected a command to be returned")
	}

	// Execute the command to get the message
	msg := cmd()
	authMsg, ok := msg.(authCodeEnteredMsg)
	if !ok {
		t.Fatalf("expected authCodeEnteredMsg, got %T", msg)
	}

	if authMsg.code != "CODE123#STATE456" {
		t.Errorf("expected code 'CODE123#STATE456', got %q", authMsg.code)
	}
	if authMsg.verifier != "test-verifier" {
		t.Errorf("expected verifier 'test-verifier', got %q", authMsg.verifier)
	}
}

func TestCodeInputModal_EnterOnCopyURL(t *testing.T) {
	authURL := "https://example.com/oauth?code=123"
	modal := NewCodeInputModal(authURL, "verifier")

	// Navigate to copy URL
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	// Press enter
	_, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected a command to be returned")
	}

	// Execute the command to get the message
	msg := cmd()
	copyMsg, ok := msg.(urlCopiedToClipboardMsg)
	if !ok {
		t.Fatalf("expected urlCopiedToClipboardMsg, got %T", msg)
	}

	if copyMsg.url != authURL {
		t.Errorf("expected url %q, got %q", authURL, copyMsg.url)
	}
	// Note: err might be non-nil if clipboard is not available in test environment
}

func TestCodeInputModal_Escape(t *testing.T) {
	modal := NewCodeInputModal("https://example.com", "verifier")

	_, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if cmd == nil {
		t.Fatal("expected a command to be returned")
	}

	msg := cmd()
	_, ok := msg.(modalCancelledMsg)
	if !ok {
		t.Fatalf("expected modalCancelledMsg, got %T", msg)
	}
}

func TestCodeInputModal_Render(t *testing.T) {
	modal := NewCodeInputModal("https://example.com/oauth", "verifier")
	modal.textInput.SetValue("test-code")

	rendered := modal.Render()

	// Check that key elements are present
	if !strings.Contains(rendered, "Browser opened for Anthropic OAuth") {
		t.Error("expected render to contain 'Browser opened for Anthropic OAuth'")
	}
	if !strings.Contains(rendered, "Token") {
		t.Error("expected render to contain 'Token'")
	}
	assert.Contains(t, rendered, "Copy the auth")
	if !strings.Contains(rendered, "j/k to navigate") {
		t.Error("expected render to contain navigation instructions")
	}

	// Check that the selection indicator is on token (first item)
	if !strings.Contains(rendered, "▶ Token") {
		t.Error("expected token to be selected (▶ prefix)")
	}

	// Navigate to copy URL and check selection
	modal, _ = modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	rendered = modal.Render()

	assert.Contains(t, rendered, "▶ Copy Anthropic")
}

func TestGetOAuthConfig(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		setupEnv    func()
		cleanupEnv  func()
		expectError bool
		checkResult func(t *testing.T, cfg oauthProviderConfig)
	}{
		{
			name:     "googleai with defaults",
			provider: "googleai",
			setupEnv: func() {
				os.Setenv("GOOGLE_CLIENT_ID", "test-client-id")
				os.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
			},
			cleanupEnv: func() {
				os.Unsetenv("GOOGLE_CLIENT_ID")
				os.Unsetenv("GOOGLE_CLIENT_SECRET")
			},
			expectError: false,
			checkResult: func(t *testing.T, cfg oauthProviderConfig) {
				assert.Equal(t, "test-client-id", cfg.ClientID)
				assert.Equal(t, "test-secret", cfg.ClientSecret)
				assert.Contains(t, cfg.AuthURL, "accounts.google.com")
				assert.Contains(t, cfg.TokenURL, "oauth2.googleapis.com")
				assert.Contains(t, cfg.Scopes, "https://www.googleapis.com/auth/generative-language")
			},
		},
		{
			name:     "googleai with custom scopes",
			provider: "googleai",
			setupEnv: func() {
				os.Setenv("GOOGLE_CLIENT_ID", "test-client-id")
				os.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
				os.Setenv("GOOGLE_OAUTH_SCOPES", "scope1,scope2")
			},
			cleanupEnv: func() {
				os.Unsetenv("GOOGLE_CLIENT_ID")
				os.Unsetenv("GOOGLE_CLIENT_SECRET")
				os.Unsetenv("GOOGLE_OAUTH_SCOPES")
			},
			expectError: false,
			checkResult: func(t *testing.T, cfg oauthProviderConfig) {
				assert.Equal(t, []string{"scope1", "scope2"}, cfg.Scopes)
			},
		},
		{
			name:     "openai with configuration",
			provider: "openai",
			setupEnv: func() {
				os.Setenv("OPENAI_AUTH_URL", "https://auth.openai.com")
				os.Setenv("OPENAI_TOKEN_URL", "https://token.openai.com")
				os.Setenv("OPENAI_CLIENT_ID", "openai-client")
				os.Setenv("OPENAI_CLIENT_SECRET", "openai-secret")
			},
			cleanupEnv: func() {
				os.Unsetenv("OPENAI_AUTH_URL")
				os.Unsetenv("OPENAI_TOKEN_URL")
				os.Unsetenv("OPENAI_CLIENT_ID")
				os.Unsetenv("OPENAI_CLIENT_SECRET")
			},
			expectError: false,
			checkResult: func(t *testing.T, cfg oauthProviderConfig) {
				assert.Equal(t, "https://auth.openai.com", cfg.AuthURL)
				assert.Equal(t, "https://token.openai.com", cfg.TokenURL)
				assert.Equal(t, "openai-client", cfg.ClientID)
			},
		},
		{
			name:        "unsupported provider",
			provider:    "unsupported",
			setupEnv:    func() {},
			cleanupEnv:  func() {},
			expectError: true,
		},
		{
			name:     "missing client ID",
			provider: "googleai",
			setupEnv: func() {
				// Don't set CLIENT_ID
				os.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
			},
			cleanupEnv: func() {
				os.Unsetenv("GOOGLE_CLIENT_SECRET")
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv()
			defer tt.cleanupEnv()

			cfg, err := getOAuthConfig(tt.provider)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.checkResult != nil {
					tt.checkResult(t, cfg)
				}
			}
		})
	}
}

// NOTE: UpdateUserLLMAuth tests are disabled because they trigger system keyring dialogs.
// To test this function manually, set ASIMI_TEST_KEYRING=1 and run:
//
//	ASIMI_TEST_KEYRING=1 go test -v -run TestUpdateUserLLMAuthIntegration
func TestUpdateUserLLMAuthIntegration(t *testing.T) {
	// Skip unless explicitly enabled
	if os.Getenv("ASIMI_TEST_KEYRING") != "1" {
		t.Skip("Skipping UpdateUserLLMAuth test. Set ASIMI_TEST_KEYRING=1 to run this test manually.")
	}

	t.Log("WARNING: This test will trigger system keyring dialogs!")

	t.Run("creates config file if not exists", func(t *testing.T) {
		tempHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempHome)
		defer os.Setenv("HOME", originalHome)

		err := UpdateUserLLMAuth("openai", "test-api-key", "gpt-4")
		require.NoError(t, err)

		configDir := filepath.Join(tempHome, ".config", "asimi")
		_, err = os.Stat(configDir)
		require.NoError(t, err)

		configPath := filepath.Join(configDir, "asimi.conf")
		_, err = os.Stat(configPath)
		assert.NoError(t, err, "Config file should be created")
	})
}
