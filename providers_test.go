package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
)

// TestProvideConfig_ReturnsErrorOnMalformedConfig verifies that ProvideConfig
// propagates errors from LoadProjectConfig instead of silently falling back to
// a hardcoded default config. This is the core behavioral change of edict 523:
// the dead os.ErrNotExist fallback (which had contradictory openai/gpt-3.5-turbo
// defaults) was removed.
func TestProvideConfig_ReturnsErrorOnMalformedConfig(t *testing.T) {
	tempHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempHome)
	defer os.Setenv("HOME", originalHome)

	// Create a malformed user config (duplicate TOML key)
	userConfigDir := filepath.Join(tempHome, ".config", "asimi")
	require.NoError(t, os.MkdirAll(userConfigDir, 0o755))
	malformed := `[llm]
provider = "openai"
provider = "anthropic"
`
	require.NoError(t, os.WriteFile(filepath.Join(userConfigDir, "asimi.conf"), []byte(malformed), 0o644))

	logger := slog.Default()
	ri := repo.RepoInfo{ProjectRoot: ""}

	cfg, err := ProvideConfig(logger, ri)
	require.Error(t, err, "malformed config must return an error, not silently fall back")
	assert.Nil(t, cfg, "config should be nil on error")
	assert.Contains(t, err.Error(), "failed to load configuration")
}

// TestProvideConfig_SucceedsWithDefaults verifies that ProvideConfig succeeds
// and returns valid defaults when no config file exists (the normal first-run
// path). This confirms the dead fallback removal didn't break the happy path.
func TestProvideConfig_SucceedsWithDefaults(t *testing.T) {
	tempHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempHome)
	defer os.Setenv("HOME", originalHome)

	// EnsureUserConfigExists will create the config file from the embedded template
	_, err := config.EnsureUserConfigExists()
	require.NoError(t, err)

	logger := slog.Default()
	ri := repo.RepoInfo{ProjectRoot: ""}

	cfg, err := ProvideConfig(logger, ri)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Provider should be empty (matching DefaultConfig), NOT "openai"
	// (the old dead fallback hardcoded "openai")
	assert.Empty(t, cfg.LLM.Provider, "provider should default to empty, not a hardcoded value")
}

// TestProvideConfig_MaxTurnsOverride verifies that the --max-turns CLI flag
// overrides the LLMConfig.MaxTurns value from the config file.
func TestProvideConfig_MaxTurnsOverride(t *testing.T) {
	tempHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempHome)
	defer os.Setenv("HOME", originalHome)

	_, err := config.EnsureUserConfigExists()
	require.NoError(t, err)

	originalMaxTurns := cli.MaxTurns
	cli.MaxTurns = 42
	defer func() { cli.MaxTurns = originalMaxTurns }()

	logger := slog.Default()
	ri := repo.RepoInfo{ProjectRoot: ""}

	cfg, err := ProvideConfig(logger, ri)
	require.NoError(t, err)
	assert.Equal(t, 42, cfg.LLM.MaxTurns, "MaxTurns should be overridden by --max-turns CLI flag")
}

// TestProvideConfig_ModelOverride verifies that the --model and --provider
// CLI flags override the LLM config values.
func TestProvideConfig_ModelOverride(t *testing.T) {
	tempHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempHome)
	defer os.Setenv("HOME", originalHome)

	_, err := config.EnsureUserConfigExists()
	require.NoError(t, err)

	originalModel := cli.Model
	originalProvider := cli.Provider
	cli.Model = "gpt-4o"
	cli.Provider = "openai"
	defer func() {
		cli.Model = originalModel
		cli.Provider = originalProvider
	}()

	logger := slog.Default()
	ri := repo.RepoInfo{ProjectRoot: ""}

	cfg, err := ProvideConfig(logger, ri)
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", cfg.LLM.Model, "Model should be overridden by --model CLI flag")
	assert.Equal(t, "openai", cfg.LLM.Provider, "Provider should be overridden by --provider CLI flag")
}
