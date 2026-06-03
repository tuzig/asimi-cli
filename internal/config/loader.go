// Package config provides configuration loading and management for asimi.
package config

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	koanftoml "github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	koanf "github.com/knadh/koanf/v2"
)

//go:embed default.conf
var defaultConfContent string

// DefaultConfContent returns the embedded default configuration file content.
func DefaultConfContent() string {
	return defaultConfContent
}

// ConfigCreated tracks whether the config file was created on this run.
// TODO: find a better way and remove this global
var ConfigCreated bool

// DefaultConfig returns the configuration populated with sensible defaults.
func DefaultConfig() Config {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("Failed to get user home directory", "error", err)
	}
	dbPath := filepath.Join(homeDir, ".local", "share", "asimi", "asimi.sqlite")

	return Config{
		Storage: StorageConfig{
			DatabasePath: dbPath,
		},
		LLM: LLMConfig{
			MaxToolOutput:            51200, // Default: 50KB
			RequestTimeoutSeconds:    300,
			StreamIdleTimeoutSeconds: 600,
			MaxRetries:               3,
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
			MarkdownEnabled:      true,
			CtrlCDebounceTime:    200 * time.Millisecond, // Debounce for duplicate CTRL-C events from terminals
			CtrlCWindowTime:      2000 * time.Millisecond,
			PromptExpandedHeight: 10,
		},
		Session: SessionConfig{
			Enabled:      true,
			MaxSessions:  50,
			MaxAgeDays:   30,
			ListLimit:    0,
			AutoSave:     true,
			SaveInterval: 300,
		},
		Sandbox: SandboxConfig{
			RunOnHost:     []string{`^gh\s`, `^podman\s`},
			SafeRunOnHost: []string{`^gh\s+(issue|pr)\s+(view|list)`},
		},
	}
}

// UserConfigPath returns the path to the user config directory and file.
// Returns (cfgDir, cfgPath, error).
func UserConfigPath() (string, string, error) {
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
	cfgDir, cfgPath, err := UserConfigPath()
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

	slog.Info("Created user config file", "path", cfgPath)
	return true, nil
}

// resolveAPIKeys fills in provider-specific API keys from well-known
// environment variables. It is used by LoadConfig so that initial boot
// still auto-discovers credentials, but is deliberately excluded from
// LoadProjectConfig (daemon receives keys via APIKeys).
func resolveAPIKeys(cfg *Config) {
	if cfg.LLM.Provider != "" && cfg.LLM.APIKey == "" {
		switch cfg.LLM.Provider {
		case "anthropic":
			if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
				cfg.LLM.APIKey = key
			}
		case "openai":
			if key := os.Getenv("OPENAI_API_KEY"); key != "" {
				cfg.LLM.APIKey = key
			}
		case "openrouter":
			if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
				cfg.LLM.APIKey = key
			}
		case "googleai":
			if key := os.Getenv("GEMINI_API_KEY"); key != "" {
				cfg.LLM.APIKey = key
			} else if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
				cfg.LLM.APIKey = key
			}
		}
	}
}

// LoadConfig loads user-level defaults plus ~/.config/asimi/asimi.conf
// and resolves API keys from environment variables. It does NOT load
// project-level config or ASIMI_ prefix env vars — the daemon loads
// per-client via LoadProjectConfig, and the TUI overlays project config
// after RepoInfo is available.
//
// Layer order (later wins):
//  1. Built-in defaults (DefaultConfig)
//  2. User-level config: ~/.config/asimi/asimi.conf
//  3. Env-var credential resolution (API keys for already-configured providers)
func LoadConfig() (*Config, error) {
	k := koanf.New(".")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	userConfigPath := filepath.Join(homeDir, ".config", "asimi", "asimi.conf")
	if err := k.Load(file.Provider(userConfigPath), koanftoml.Parser()); err != nil {
		// Missing user config is common on first run; downgrade to Debug
		// so it doesn't pollute normal startup output.
		slog.Debug("Failed to load user config", "path", userConfigPath, "error", err)
	}

	// Unmarshal onto defaults so every field has a value.
	cfg := DefaultConfig()
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Set default values for session config if not explicitly configured
	if !k.Exists("session.enabled") {
		cfg.Session.Enabled = true // Default to enabled
	}

	// Resolve API keys from environment variables
	resolveAPIKeys(&cfg)

	return &cfg, nil
}

// OverlayProjectConfig reads project-level config from
// {projectRoot}/.agents/asimi.conf and unmarshals it onto the existing
// cfg, overwriting any fields set in the project config. Fields not
// present in the project config are left unchanged on cfg.
//
// This is the initial-load counterpart to Config.ReloadProjectConf:
// it uses the same koanf-based approach but is a standalone function so
// the TUI boot path can call it after fx provides both Config and RepoInfo.
func OverlayProjectConfig(cfg *Config, projectRoot string) error {
	if projectRoot == "" {
		return nil // No project root, nothing to overlay
	}

	projectConfigPath := filepath.Join(projectRoot, ".agents", "asimi.conf")

	if _, err := os.Stat(projectConfigPath); os.IsNotExist(err) {
		return nil // No project config to overlay
	} else if err != nil {
		slog.Warn("Unable to stat project config", "path", projectConfigPath, "error", err)
		return nil
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(projectConfigPath), koanftoml.Parser()); err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	if err := k.Unmarshal("", cfg); err != nil {
		return fmt.Errorf("failed to unmarshal project config: %w", err)
	}

	return nil
}

// LoadProjectConfig loads configuration for a specific project root without
// relying on the current working directory or environment-variable credentials.
//
// Layer order (later wins):
//  1. Built-in defaults (DefaultConfig)
//  2. User-level config: ~/.config/asimi/asimi.conf
//  3. Project-level config: {projectRoot}/.agents/asimi.conf
//
// The daemon receives API keys via its APIKeys mechanism, so this function
// intentionally skips all env-var credential resolution.
func LoadProjectConfig(projectRoot string) (*Config, error) {
	k := koanf.New(".")

	// 1. User-level config
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	userConfigPath := filepath.Join(homeDir, ".config", "asimi", "asimi.conf")
	if err := k.Load(file.Provider(userConfigPath), koanftoml.Parser()); err != nil {
		// Missing user config is common on first run; downgrade to Debug
		// so it doesn't pollute normal startup output.
		slog.Debug("Failed to load user config", "path", userConfigPath, "error", err)
	}

	// 2. Project-level config
	projectConfigPath := filepath.Join(projectRoot, ".agents", "asimi.conf")
	if _, statErr := os.Stat(projectConfigPath); statErr == nil {
		if err := k.Load(file.Provider(projectConfigPath), koanftoml.Parser()); err != nil {
			slog.Debug("Failed to load project config", "path", projectConfigPath, "error", err)
		}
	} else if !os.IsNotExist(statErr) {
		slog.Warn("Unable to stat project config", "path", projectConfigPath, "error", statErr)
	}

	// Unmarshal onto defaults so every field has a value.
	config := DefaultConfig()
	if err := k.Unmarshal("", &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

// SaveConfig saves the current config to the user-level config file (~/.config/asimi/asimi.conf).
// It preserves all comments in the existing file.
func SaveConfig(cfg *Config) error {
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
	content = UpdateOrInsertTOMLValue(content, "llm", "provider", cfg.LLM.Provider)
	content = UpdateOrInsertTOMLValue(content, "llm", "model", cfg.LLM.Model)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(userConfigPath), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(userConfigPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// SetProjectConfig updates keys in the project config file under the given
// projectRoot directory. It accepts a section name followed by key-value pairs,
// similar to slog.Info().
// Example: SetProjectConfig("/path/to/project", "session", "agents_file", "CLAUDE.md")
// It preserves all comments in the existing file.
func SetProjectConfig(projectRoot, section string, keyValues ...string) error {
	if len(keyValues)%2 != 0 {
		return fmt.Errorf("SetProjectConfig requires an even number of key-value arguments")
	}

	projectConfigPath := filepath.Join(projectRoot, ".agents", "asimi.conf")
	agentsDir := filepath.Join(projectRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
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
		content = UpdateOrInsertTOMLValue(content, section, key, value)
	}

	if err := os.WriteFile(projectConfigPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetEnv returns the environment variable value or fallback if not set.
func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// EnsureProjectConfig seeds .agents/asimi.conf from the embedded default
// template if the file does not already exist. It creates .agents/ as needed.
// This lets callers modify keys in place (via config.SetProjectConfig) without
// the first write producing a stub file that overrides the full default set.
func EnsureProjectConfig(projectRoot string) error {
	path := filepath.Join(projectRoot, ".agents", "asimi.conf")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	agentsDir := filepath.Join(projectRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .agents directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(defaultConfContent), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

