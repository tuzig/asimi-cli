package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	koanftoml "github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	koanf "github.com/knadh/koanf/v2"
)

// Config represents the application configuration structure
type Config struct {
	Storage   StorageConfig    `koanf:"storage"`
	Logging   LoggingConfig    `koanf:"logging"`
	UI        UIConfig         `koanf:"ui"`
	LLM       LLMConfig        `koanf:"llm"`
	History   HistoryConfig    `koanf:"history"`
	Session   SessionConfig    `koanf:"session"`
	Sandbox   SandboxConfig    `koanf:"sandbox"`
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

// LLMConfig holds LLM provider settings.
// This type is shared between main, storage, and shogunate packages.
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
	MaxFileSize                int    `koanf:"max_file_size"` // Maximum file size to read fully (bytes)
}

// HistoryConfig holds persistent history configuration
type HistoryConfig struct {
	Enabled      bool `koanf:"enabled"`
	MaxSessions  int  `koanf:"max_sessions"` // Used as max entries for history
	MaxAgeDays   int  `koanf:"max_age_days"`
	ListLimit    int  `koanf:"list_limit"`
	AutoSave     bool `koanf:"auto_save"`
	SaveInterval int  `koanf:"save_interval"`
}

// UIConfig holds UI-specific configuration
type UIConfig struct {
	MarkdownEnabled      bool          `koanf:"markdown_enabled"`
	CtrlCDebounceTime    time.Duration `koanf:"ctrl_c_debounce_time"`   // Quiet period for CTRL-C burst detection (handles iOS duplicate events)
	CtrlCWindowTime      time.Duration `koanf:"ctrl_c_window_time"`     // Window for second CTRL-C press to quit
	PromptExpandedHeight int           `koanf:"prompt_expanded_height"` // Height prompt grows to when multiline (default: 10)
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

// Mount represents a volume mount for the sandbox container
type Mount struct {
	Source      string `koanf:"source"`
	Destination string `koanf:"destination"`
}

// SandboxConfig holds configuration for the sandboxed execution environment
type SandboxConfig struct {
	// ImageName is the container image name (default: asimi-sandbox-<project>:latest)
	ImageName string `koanf:"image_name"`
	// TimeoutMinutes is the timeout for shell commands in minutes (default: 10)
	TimeoutMinutes int `koanf:"timeout_minutes"`
	// NoCleanup skips container removal on exit (for debugging)
	NoCleanup bool `koanf:"no_cleanup"`
	// AllowHostFallback falls back to host shell if container is unavailable
	AllowHostFallback bool `koanf:"allow_host_fallback"`
	// RunOnHost is a list of regex patterns for commands that should run on the host
	// instead of in the container. These commands require user approval before execution.
	RunOnHost []string `koanf:"run_on_host"`
	// SafeRunOnHost is a list of regex patterns for commands that can run on the host
	// without requiring user approval (e.g., read-only commands like `gh issue view`)
	SafeRunOnHost []string `koanf:"safe_run_on_host"`
	// AdditionalMounts are extra volume mounts for the container
	AdditionalMounts []Mount `koanf:"additional_mounts"`
	// PassthroughEnv is a list of host environment variable names to forward into the sandbox
	PassthroughEnv []string `koanf:"passthrough_env"`
}

// ShogunateConfig holds configuration for the Shogunate.
type ShogunateConfig struct {
	PollInterval  time.Duration `koanf:"poll_interval"`
	RitualTimeout time.Duration `koanf:"ritual_timeout"`
	Username      string        `koanf:"username"` // OS username for edict scoping
	Project       string        `koanf:"project"`  // project slug for edict scoping
}

// DefaultShogunateConfig returns the default configuration.
func DefaultShogunateConfig() *ShogunateConfig {
	username := "guest"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	return &ShogunateConfig{
		PollInterval:  5 * time.Second,
		RitualTimeout: 30 * time.Second,
		Username:      username,
	}
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
