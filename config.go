package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/config"
)

// Type aliases - use types from internal/config as the single source of truth
type (
	Config        = config.Config
	StorageConfig = config.StorageConfig
	LoggingConfig = config.LoggingConfig
	LLMConfig     = config.LLMConfig
	HistoryConfig = config.HistoryConfig
	UIConfig      = config.UIConfig
	SessionConfig = config.SessionConfig
	SandboxConfig = config.SandboxConfig
	Mount         = config.Mount
)

// Re-export functions from internal/config for backwards compatibility
var (
	LoadConfig             = config.LoadConfig
	SaveConfig             = config.SaveConfig
	SetProjectConfig       = config.SetProjectConfig
	EnsureUserConfigExists = config.EnsureUserConfigExists
)

// ConfigCreated returns the value of the config.ConfigCreated flag
func GetConfigCreated() bool {
	return config.ConfigCreated
}

// SetConfigCreated sets the value of the config.ConfigCreated flag
func SetConfigCreated(value bool) {
	config.ConfigCreated = value
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
