// Package config provides configuration loading and management for asimi.
package config

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	koanftoml "github.com/knadh/koanf/parsers/toml/v2"
	koanfenv "github.com/knadh/koanf/providers/env/v2"
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
			MaxToolOutput: 51200, // Default: 50KB
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
			slog.Warn("Failed to load user config", "path", userConfigPath, "error", err)
		}
	}

	projectConfigPath := filepath.Join(".agents", "asimi.conf")
	if _, err := os.Stat(projectConfigPath); err == nil {
		if err := k.Load(file.Provider(projectConfigPath), koanftoml.Parser()); err != nil {
			slog.Warn("Failed to load project config", "path", projectConfigPath, "error", err)
		}
	} else if !os.IsNotExist(err) {
		slog.Warn("Unable to stat project config", "path", projectConfigPath, "error", err)
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
		slog.Warn("Failed to load environment variables", "error", err)
	}

	// Special handling for API keys from standard environment variables
	// Check for OPENAI_API_KEY if using OpenAI
	if k.String("llm.provider") == "openai" && k.String("llm.api_key") == "" {
		if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
			if err := k.Set("llm.api_key", openaiKey); err != nil {
				slog.Warn("Failed to set OpenAI API key from environment", "error", err)
			}
		}
	}

	// Check for ANTHROPIC_API_KEY if using Anthropic
	if k.String("llm.provider") == "anthropic" && k.String("llm.api_key") == "" {
		if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
			if err := k.Set("llm.api_key", anthropicKey); err != nil {
				slog.Warn("Failed to set Anthropic API key from environment", "error", err)
			}
		}
	}

	// Unmarshal the configuration into our struct
	config := DefaultConfig()
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
			slog.Info("Auto-configured provider", "provider", "anthropic", "source", "ANTHROPIC_API_KEY")
		} else if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
			config.LLM.Provider = "openai"
			config.LLM.Model = "gpt-4o"
			config.LLM.APIKey = openaiKey
			slog.Info("Auto-configured provider", "provider", "openai", "source", "OPENAI_API_KEY")
		} else if geminiKey := os.Getenv("GEMINI_API_KEY"); geminiKey != "" {
			config.LLM.Provider = "googleai"
			config.LLM.Model = "gemini-2.5-flash"
			config.LLM.APIKey = geminiKey
			slog.Info("Auto-configured provider", "provider", "googleai", "source", "GEMINI_API_KEY")
		} else if googleKey := os.Getenv("GOOGLE_API_KEY"); googleKey != "" {
			config.LLM.Provider = "googleai"
			config.LLM.Model = "gemini-2.5-flash"
			config.LLM.APIKey = googleKey
			slog.Info("Auto-configured provider", "provider", "googleai", "source", "GOOGLE_API_KEY")
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
