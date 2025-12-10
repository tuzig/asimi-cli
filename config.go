package main

import (
	_ "embed"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	koanftoml "github.com/knadh/koanf/parsers/toml/v2"
	koanfenv "github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	koanf "github.com/knadh/koanf/v2"
)

//go:embed dotagents/asimi.conf
var defaultConfContent string

type oauthProviderConfig struct {
	AuthURL      string
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// Config represents the application configuration structure
type Config struct {
	Storage    StorageConfig    `koanf:"storage"`
	Logging    LoggingConfig    `koanf:"logging"`
	UI         UIConfig         `koanf:"ui"`
	LLM        LLMConfig        `koanf:"llm"`
	History    HistoryConfig    `koanf:"history"`
	Session    SessionConfig    `koanf:"session"`
	Container  ContainerConfig  `koanf:"container"`
	RunInShell RunInShellConfig `koanf:"run_in_shell"`
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	DatabasePath string `koanf:"database_path"` // Path to SQLite database
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

// LLMConfig holds LLM configuration
type LLMConfig struct {
	Provider                   string `koanf:"provider"`
	Model                      string `koanf:"model"`
	APIKey                     string `koanf:"api_key"`
	BaseURL                    string `koanf:"base_url"`
	MaxThinkingTokens          int    `koanf:"max_thinking_tokens"`
	MaxTurns                   int    `koanf:"max_turns"`
	DisableContextSanitization bool   `koanf:"disable_sanitization"`
	AuthToken                  string `koanf:"auth_token"`
	RefreshToken               string `koanf:"refresh_token"`
	ExperimentalModels         bool   `koanf:"experimental_models"`
}

// HistoryConfig holds persistent session history configuration
type HistoryConfig struct {
	Enabled      bool `koanf:"enabled"`
	MaxSessions  int  `koanf:"max_sessions"`
	MaxAgeDays   int  `koanf:"max_age_days"`
	ListLimit    int  `koanf:"list_limit"`
	AutoSave     bool `koanf:"auto_save"`
	SaveInterval int  `koanf:"save_interval"`
}

// UIConfig holds UI-specific configuration
type UIConfig struct {
	MarkdownEnabled bool `koanf:"markdown_enabled"`
}

// defaultConfig returns the configuration populated with sensible defaults.
func defaultConfig() Config {
	homeDir, _ := os.UserHomeDir()
	dbPath := filepath.Join(homeDir, ".local", "share", "asimi", "asimi.sqlite")

	return Config{
		Storage: StorageConfig{
			DatabasePath: dbPath,
		},
		History: HistoryConfig{
			Enabled:      true,
			MaxSessions:  50,
			MaxAgeDays:   30,
			ListLimit:    0,
			AutoSave:     false,
			SaveInterval: 300,
		},
		UI: UIConfig{
			MarkdownEnabled: true,
		},
		Session: SessionConfig{
			Enabled:      true,
			MaxSessions:  50,
			MaxAgeDays:   30,
			ListLimit:    0,
			AutoSave:     true,
			SaveInterval: 300,
		},
		RunInShell: RunInShellConfig{
			RunOnHost:     []string{`^gh\s`, `^podman\s`},
			SafeRunOnHost: []string{`^gh\s+(issue|pr)\s+(view|list)`},
		},
	}
}

// SessionConfig holds session persistence configuration
type SessionConfig struct {
	Enabled      bool   `koanf:"enabled"`
	MaxSessions  int    `koanf:"max_sessions"`
	MaxAgeDays   int    `koanf:"max_age_days"`
	ListLimit    int    `koanf:"list_limit"`
	AutoSave     bool   `koanf:"auto_save"`
	SaveInterval int    `koanf:"save_interval"`
	AgentsFile   string `koanf:"agents_file"` // Project context file name (default: AGENTS.md, can be CLAUDE.md)
}

// ContainerMount represents a mount point for the container
type ContainerMount struct {
	Source      string `koanf:"source"`
	Destination string `koanf:"destination"`
}

// ContainerConfig holds container configuration
type ContainerConfig struct {
	AdditionalMounts []ContainerMount `koanf:"additional_mounts"`
}

// RunInShellConfig holds configuration for the run_in_shell tool
type RunInShellConfig struct {
	// RunOnHost is a list of regex patterns for commands that should run on the host
	// instead of in the container. These commands require user approval before execution.
	RunOnHost []string `koanf:"run_on_host"`
	// SafeRunOnHost is a list of regex patterns for commands that can run on the host
	// without requiring user approval (e.g., read-only commands like `gh issue view`)
	SafeRunOnHost []string `koanf:"safe_run_on_host"`
	// TimeoutMinutes is the timeout for shell commands in minutes (default: 10)
	TimeoutMinutes    int    `koanf:"timeout_minutes"`
	AllowHostFallback bool   `koanf:"allow_host_fallback"`
	NoCleanup         bool   `koanf:"no_cleanup"`
	ImageName         string `koanf:"image_name"` // Container image name (default: asimi-sandbox-<project>:latest)
}

// TODO: find a better way and remove this global
// ConfigCreated tracks whether the config file was created on this run
var ConfigCreated bool

// userConfigPath returns the path to the user config directory and file.
// Returns (cfgDir, cfgPath, error).
func userConfigPath() (string, string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	cfgDir := filepath.Join(homeDir, ".config", "asimi")
	cfgPath := filepath.Join(cfgDir, "asimi.conf")
	return cfgDir, cfgPath, nil
}

// EnsureUserConfigExists checks if the user config file exists and creates it if not.
// Returns true if the config file was created (first run), false otherwise.
func EnsureUserConfigExists() (bool, error) {
	cfgDir, cfgPath, err := userConfigPath()
	if err != nil {
		return false, err
	}

	// Check if config file already exists
	if _, err := os.Stat(cfgPath); err == nil {
		return false, nil // Config exists, not first run
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to check config file: %w", err)
	}

	// Config doesn't exist - create it
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return false, fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(cfgPath, []byte(defaultConfContent), 0o644); err != nil {
		return false, fmt.Errorf("failed to create config file: %w", err)
	}

	log.Printf("Created user config file at %s", cfgPath)
	return true, nil
}

// LoadConfig loads configuration from multiple sources
func LoadConfig() (*Config, error) {
	// Create a new koanf instance
	k := koanf.New(".")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Error("Failed to get user home directory", "error", err)
	} else {
		userConfigPath := filepath.Join(homeDir, ".config", "asimi", "asimi.conf")
		if err := k.Load(file.Provider(userConfigPath), koanftoml.Parser()); err != nil {
			log.Printf("Failed to load user config from %s: %v", userConfigPath, err)
		}
	}

	projectConfigPath := filepath.Join(".agents", "asimi.conf")
	if _, err := os.Stat(projectConfigPath); err == nil {
		if err := k.Load(file.Provider(projectConfigPath), koanftoml.Parser()); err != nil {
			log.Printf("Failed to load project config from %s: %v", projectConfigPath, err)
		}
	} else if !os.IsNotExist(err) {
		log.Printf("Unable to stat project config at %s: %v", projectConfigPath, err)
	}

	// 3. Load environment variables
	// Environment variables with prefix "ASIMI_" will override config values
	// e.g., ASIMI_SERVER_PORT=8080 will override the server port
	if err := k.Load(koanfenv.Provider(".", koanfenv.Opt{
		Prefix: "ASIMI_",
		TransformFunc: func(key, value string) (string, any) {
			// Transform environment variable names to match config keys
			// ASIMI_SERVER_PORT becomes "server.port"
			key = strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(key, "ASIMI_")), "_", ".")
			return key, value
		},
	}), nil); err != nil {
		log.Printf("Failed to load environment variables: %v", err)
	}

	// Special handling for API keys from standard environment variables
	// Check for OPENAI_API_KEY if using OpenAI
	if k.String("llm.provider") == "openai" && k.String("llm.api_key") == "" {
		if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
			if err := k.Set("llm.api_key", openaiKey); err != nil {
				log.Printf("Failed to set OpenAI API key from environment: %v", err)
			}
		}
	}

	// Check for ANTHROPIC_API_KEY if using Anthropic
	if k.String("llm.provider") == "anthropic" && k.String("llm.api_key") == "" {
		if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
			if err := k.Set("llm.api_key", anthropicKey); err != nil {
				log.Printf("Failed to set Anthropic API key from environment: %v", err)
			}
		}
	}

	// Unmarshal the configuration into our struct
	config := defaultConfig()
	if err := k.Unmarshal("", &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Set default values for session config if not explicitly configured
	// Check if session.enabled was explicitly set in config or environment
	if !k.Exists("session.enabled") {
		config.Session.Enabled = true // Default to enabled
	}

	// Auto-discovery: If no provider is configured, detect from environment variables
	// Priority: Anthropic > OpenAI > Google AI
	if config.LLM.Provider == "" {
		if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
			config.LLM.Provider = "anthropic"
			config.LLM.Model = "claude-sonnet-4-20250514"
			config.LLM.APIKey = anthropicKey
			log.Printf("Auto-configured provider: anthropic (from ANTHROPIC_API_KEY)")
		} else if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
			config.LLM.Provider = "openai"
			config.LLM.Model = "gpt-4o"
			config.LLM.APIKey = openaiKey
			log.Printf("Auto-configured provider: openai (from OPENAI_API_KEY)")
		} else if geminiKey := os.Getenv("GEMINI_API_KEY"); geminiKey != "" {
			config.LLM.Provider = "googleai"
			config.LLM.Model = "gemini-2.5-flash"
			config.LLM.APIKey = geminiKey
			log.Printf("Auto-configured provider: googleai (from GEMINI_API_KEY)")
		} else if googleKey := os.Getenv("GOOGLE_API_KEY"); googleKey != "" {
			config.LLM.Provider = "googleai"
			config.LLM.Model = "gemini-2.5-flash"
			config.LLM.APIKey = googleKey
			log.Printf("Auto-configured provider: googleai (from GOOGLE_API_KEY)")
		}
	}

	// If provider is set but API key is not, try to load from environment
	if config.LLM.Provider != "" && config.LLM.APIKey == "" {
		switch config.LLM.Provider {
		case "anthropic":
			if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
				config.LLM.APIKey = key
			}
		case "openai":
			if key := os.Getenv("OPENAI_API_KEY"); key != "" {
				config.LLM.APIKey = key
			}
		case "googleai":
			if key := os.Getenv("GEMINI_API_KEY"); key != "" {
				config.LLM.APIKey = key
			} else if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
				config.LLM.APIKey = key
			}
		}
	}

	return &config, nil
}

// ReloadProjectConf reloads the project's configuration file
func (c *Config) ReloadProjectConf() error {
	projectConfigPath := filepath.Join(".agents", "asimi.conf")

	// Check if project config exists
	if _, err := os.Stat(projectConfigPath); os.IsNotExist(err) {
		return nil // No project config to reload
	}

	// Create a new koanf instance and load project config
	k := koanf.New(".")
	if err := k.Load(file.Provider(projectConfigPath), koanftoml.Parser()); err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	// Unmarshal into the current config, overwriting project-level settings
	if err := k.Unmarshal("", c); err != nil {
		return fmt.Errorf("failed to unmarshal project config: %w", err)
	}

	return nil
}

// SaveConfig saves the current config to the user-level config file (~/.config/asimi/asimi.conf).
// It preserves all comments in the existing file.
func SaveConfig(config *Config) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	userConfigPath := filepath.Join(homeDir, ".config", "asimi", "asimi.conf")
	// Read existing content or start with empty
	var content string
	if data, err := os.ReadFile(userConfigPath); err == nil {
		content = string(data)
	}
	// Update provider and model using comment-preserving helpers
	content = updateOrInsertTOMLValue(content, "llm", "provider", config.LLM.Provider)
	content = updateOrInsertTOMLValue(content, "llm", "model", config.LLM.Model)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(userConfigPath), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(userConfigPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// SetProjectConfig updates keys in the project config file (.agents/asimi.conf).
// It accepts a section name followed by key-value pairs, similar to slog.Info().
// Example: SetProjectConfig("session", "agents_file", "CLAUDE.md", "enabled", "true")
// It preserves all comments in the existing file.
func SetProjectConfig(section string, keyValues ...string) error {
	if len(keyValues)%2 != 0 {
		return fmt.Errorf("SetProjectConfig requires an even number of key-value arguments")
	}

	projectConfigPath := filepath.Join(".agents", "asimi.conf")
	if err := os.MkdirAll(".agents", 0o755); err != nil {
		return fmt.Errorf("failed to create .agents directory: %w", err)
	}

	// Read existing content or start with empty
	var content string
	if data, err := os.ReadFile(projectConfigPath); err == nil {
		content = string(data)
	}

	// Update each key-value pair
	for i := 0; i < len(keyValues); i += 2 {
		key := keyValues[i]
		value := keyValues[i+1]
		content = updateOrInsertTOMLValue(content, section, key, value)
	}

	if err := os.WriteFile(projectConfigPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// UpdateUserLLMAuth updates or creates ~/.config/asimi/asimi.conf with the given LLM auth settings.
// It saves API keys securely in the keyring and only stores provider/model in the config file.
// This function preserves all comments in the existing config file.
func UpdateUserLLMAuth(provider, apiKey, model string) error {
	// Save API key securely in keyring
	if err := SaveAPIKeyToKeyring(provider, apiKey); err != nil {
		// Fall back to file storage with warning
		log.Printf("Warning: Failed to save API key to keyring, falling back to file storage: %v", err)
		return updateAPIKeyInFile(provider, apiKey, model)
	}

	cfgDir, cfgPath, err := userConfigPath()
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
	content = updateOrInsertTOMLValue(content, "llm", "provider", provider)
	content = updateOrInsertTOMLValue(content, "llm", "model", model)
	content = updateOrInsertTOMLValue(content, "llm", "auth_method", "apikey_keyring")

	// Remove plaintext API key if it exists (we're using keyring now)
	content = removeTOMLKey(content, "llm", "api_key")

	return os.WriteFile(cfgPath, []byte(content), 0o600)
}

// updateAPIKeyInFile is the fallback method for storing API keys in file (less secure).
// This function preserves all comments in the existing config file.
func updateAPIKeyInFile(provider, apiKey, model string) error {
	cfgDir, cfgPath, err := userConfigPath()
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
	content = updateOrInsertTOMLValue(content, "llm", "provider", provider)
	content = updateOrInsertTOMLValue(content, "llm", "model", model)
	content = updateOrInsertTOMLValue(content, "llm", "api_key", apiKey)
	content = updateOrInsertTOMLValue(content, "llm", "auth_method", "apikey_file")

	return os.WriteFile(cfgPath, []byte(content), 0o600)
}

func escapeTOMLString(s string) string {
	// Basic escaping for quotes and backslashes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// =============================================================================
// TOML Comment-Preserving Helper Functions
// =============================================================================
// These functions use regex-based patching to modify TOML files while preserving
// all comments (both full-line comments and inline comments).

// findTOMLSectionBounds finds the start and end positions of a TOML section.
// Returns (sectionStart, sectionEnd, found) where:
// - sectionStart is the index of the line containing [section]
// - sectionEnd is the index of the first line of the next section (or len(lines))
// - found indicates whether the section was found
func findTOMLSectionBounds(lines []string, section string) (int, int, bool) {
	sectionStart := -1
	sectionEnd := len(lines)

	// Build regex to match the section header
	sectionPattern := regexp.MustCompile(`^\s*\[` + regexp.QuoteMeta(section) + `\]\s*$`)
	nextSectionPattern := regexp.MustCompile(`^\s*\[[^\]]+\]\s*$`)

	for i, line := range lines {
		if sectionStart == -1 {
			if sectionPattern.MatchString(line) {
				sectionStart = i
			}
		} else {
			// Look for the next section
			if nextSectionPattern.MatchString(line) {
				sectionEnd = i
				break
			}
		}
	}

	return sectionStart, sectionEnd, sectionStart != -1
}

// updateTOMLValue updates a single key's value in a TOML section, preserving comments.
// Returns the modified content and whether the key was found and updated.
func updateTOMLValue(content, section, key, newValue string) (string, bool) {
	lines := strings.Split(content, "\n")
	sectionStart, sectionEnd, found := findTOMLSectionBounds(lines, section)
	if !found {
		return content, false
	}

	// Build regex to match the key within the section
	// Matches: key = "value", key = 'value', key = value
	// Preserves inline comments
	keyPattern := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)("[^"]*"|'[^']*'|[^#\n]*)(.*)$`)

	for i := sectionStart + 1; i < sectionEnd; i++ {
		line := lines[i]
		if matches := keyPattern.FindStringSubmatch(line); matches != nil {
			// matches[1] = "key = " (with any leading whitespace)
			// matches[2] = the old value
			// matches[3] = inline comment (if any)
			newLine := matches[1] + `"` + escapeTOMLString(newValue) + `"` + matches[3]
			lines[i] = newLine
			return strings.Join(lines, "\n"), true
		}
	}

	return content, false
}

// insertTOMLValue inserts a new key=value in a section (at the end of the section).
// If the section doesn't exist, it returns the content unchanged.
func insertTOMLValue(content, section, key, value string) string {
	lines := strings.Split(content, "\n")
	sectionStart, sectionEnd, found := findTOMLSectionBounds(lines, section)
	if !found {
		return content
	}

	// Find the best insertion point (before the next section or at end of section content)
	insertAt := sectionEnd

	// Look for the last non-empty, non-comment line in the section to insert after it
	for i := sectionEnd - 1; i > sectionStart; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			insertAt = i + 1
			break
		}
	}

	// If section is empty (only header), insert right after header
	if insertAt == sectionEnd && sectionEnd == sectionStart+1 {
		insertAt = sectionStart + 1
	} else if insertAt == sectionEnd {
		// Check if we're at the section header still
		for i := sectionStart + 1; i < sectionEnd; i++ {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				break
			}
			if i == sectionEnd-1 {
				insertAt = sectionEnd
			}
		}
	}

	newLine := key + ` = "` + escapeTOMLString(value) + `"`

	// Insert the new line
	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[:insertAt]...)
	newLines = append(newLines, newLine)
	newLines = append(newLines, lines[insertAt:]...)

	return strings.Join(newLines, "\n")
}

// removeTOMLKey removes a key from a section, preserving surrounding comments.
// Returns the modified content.
func removeTOMLKey(content, section, key string) string {
	lines := strings.Split(content, "\n")
	sectionStart, sectionEnd, found := findTOMLSectionBounds(lines, section)
	if !found {
		return content
	}

	// Build regex to match the key
	keyPattern := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)

	for i := sectionStart + 1; i < sectionEnd; i++ {
		if keyPattern.MatchString(lines[i]) {
			// Remove this line
			newLines := make([]string, 0, len(lines)-1)
			newLines = append(newLines, lines[:i]...)
			newLines = append(newLines, lines[i+1:]...)
			return strings.Join(newLines, "\n")
		}
	}

	return content
}

// ensureTOMLSection ensures a section exists in the content.
// If the section doesn't exist, it appends it at the end.
// Returns the modified content.
func ensureTOMLSection(content, section string) string {
	lines := strings.Split(content, "\n")
	_, _, found := findTOMLSectionBounds(lines, section)
	if found {
		return content
	}

	// Append the section at the end
	var result strings.Builder
	result.WriteString(content)

	// Ensure there's a newline before the new section
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		result.WriteString("\n")
	}
	// Add blank line if content doesn't end with one
	if len(content) > 0 && !strings.HasSuffix(content, "\n\n") {
		result.WriteString("\n")
	}

	result.WriteString("[" + section + "]\n")
	return result.String()
}

// updateOrInsertTOMLValue updates a key if it exists, or inserts it if it doesn't.
// If the section doesn't exist, it creates the section first.
func updateOrInsertTOMLValue(content, section, key, value string) string {
	// Ensure section exists
	content = ensureTOMLSection(content, section)

	// Try to update existing key
	updated, found := updateTOMLValue(content, section, key, value)
	if found {
		return updated
	}

	// Key doesn't exist, insert it
	return insertTOMLValue(content, section, key, value)
}

// UpdateUserOAuthTokens saves OAuth tokens securely in the OS keyring and updates provider in config.
// This function preserves all comments in the existing config file.
func UpdateUserOAuthTokens(provider, accessToken, refreshToken string, expiry time.Time) error {
	// Save tokens securely in keyring
	if err := SaveTokenToKeyring(provider, accessToken, refreshToken, expiry); err != nil {
		// Fall back to file storage with warning
		log.Printf("Warning: Failed to save tokens to keyring, falling back to file storage: %v", err)
		return updateOAuthTokensInFile(provider, accessToken, refreshToken)
	}

	// Only save provider info in the config file (not the tokens)
	cfgDir, cfgPath, err := userConfigPath()
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
	content = updateOrInsertTOMLValue(content, "llm", "provider", provider)

	// Remove any plaintext tokens from config if they exist (we're using keyring now)
	content = removeTOMLKey(content, "llm", "auth_token")
	content = removeTOMLKey(content, "llm", "refresh_token")

	return os.WriteFile(cfgPath, []byte(content), 0o600)
}

// updateOAuthTokensInFile is the fallback method for storing tokens in file (less secure).
// This function preserves all comments in the existing config file.
func updateOAuthTokensInFile(provider, accessToken, refreshToken string) error {
	cfgDir, cfgPath, err := userConfigPath()
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
	content = updateOrInsertTOMLValue(content, "llm", "provider", provider)
	content = updateOrInsertTOMLValue(content, "llm", "auth_method", "oauth_file")
	content = updateOrInsertTOMLValue(content, "llm", "auth_token", accessToken)
	if refreshToken != "" {
		content = updateOrInsertTOMLValue(content, "llm", "refresh_token", refreshToken)
	}

	return os.WriteFile(cfgPath, []byte(content), 0o600)
}
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getOAuthConfig(provider string) (oauthProviderConfig, error) {
	p := oauthProviderConfig{}
	switch provider {
	case "googleai":
		// Use standard Google environment variable names
		p.AuthURL = getEnv("GOOGLE_AUTH_URL", "https://accounts.google.com/o/oauth2/v2/auth")
		p.TokenURL = getEnv("GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token")
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
