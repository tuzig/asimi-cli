package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNotCI(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") == "" && os.Getenv("ASIMI_TEST_CI") == "" {
		t.Skip("Skipping test that modifies HOME (set CI=1 or ASIMI_TEST_CI=1 to run)")
	}
}

func TestEscapeTOMLString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no escaping needed",
			input:    "simple string",
			expected: "simple string",
		},
		{
			name:     "escape quotes",
			input:    `string with "quotes"`,
			expected: `string with \"quotes\"`,
		},
		{
			name:     "escape backslashes",
			input:    `path\to\file`,
			expected: `path\\to\\file`,
		},
		{
			name:     "escape both quotes and backslashes",
			input:    `path\to\"file"`,
			expected: `path\\to\\\"file\"`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EscapeTOMLString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		fallback string
		envValue string
		setEnv   bool
		expected string
	}{
		{
			name:     "environment variable exists",
			key:      "TEST_VAR_EXISTS",
			fallback: "fallback",
			envValue: "actual_value",
			setEnv:   true,
			expected: "actual_value",
		},
		{
			name:     "environment variable does not exist",
			key:      "TEST_VAR_NOT_EXISTS",
			fallback: "fallback_value",
			envValue: "",
			setEnv:   false,
			expected: "fallback_value",
		},
		{
			name:     "empty environment variable",
			key:      "TEST_VAR_EMPTY",
			fallback: "fallback",
			envValue: "",
			setEnv:   true,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up before and after
			os.Unsetenv(tt.key)
			defer os.Unsetenv(tt.key)

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
			}

			result := GetEnv(tt.key, tt.fallback)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoadConfig(t *testing.T) {
	skipIfNotCI(t)
	tempDir := t.TempDir()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	t.Run("load with defaults", func(t *testing.T) {
		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.NotNil(t, cfg)
		// History should be enabled by default
		assert.True(t, cfg.History.Enabled)
	})

	t.Run("load with project config", func(t *testing.T) {
		err := os.MkdirAll(".agents", 0755)
		require.NoError(t, err)

		configContent := `[llm]
provider = "openai"
model = "gpt-4"
api_key = "test-key"

[history]
enabled = false
max_sessions = 100
`
		err = os.WriteFile(".agents/asimi.conf", []byte(configContent), 0644)
		require.NoError(t, err)
		defer os.RemoveAll(".agents")

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "openai", cfg.LLM.Provider)
		assert.Equal(t, "gpt-4", cfg.LLM.Model)
		assert.False(t, cfg.History.Enabled)
		assert.Equal(t, 100, cfg.History.MaxSessions)
	})

	t.Run("environment variables override config", func(t *testing.T) {
		os.Setenv("ASIMI_LLM_PROVIDER", "anthropic")
		os.Setenv("ASIMI_LLM_MODEL", "claude-3-opus")
		defer os.Unsetenv("ASIMI_LLM_PROVIDER")
		defer os.Unsetenv("ASIMI_LLM_MODEL")

		err := os.MkdirAll(".agents", 0755)
		require.NoError(t, err)
		defer os.RemoveAll(".agents")

		configContent := `[llm]
provider = "openai"
model = "gpt-4"
`
		err = os.WriteFile(".agents/asimi.conf", []byte(configContent), 0644)
		require.NoError(t, err)

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "anthropic", cfg.LLM.Provider)
		assert.Equal(t, "claude-3-opus", cfg.LLM.Model)
	})

	t.Run("load OPENAI_API_KEY from environment", func(t *testing.T) {
		os.Setenv("OPENAI_API_KEY", "sk-test-key")
		defer os.Unsetenv("OPENAI_API_KEY")

		err := os.MkdirAll(".agents", 0755)
		require.NoError(t, err)
		defer os.RemoveAll(".agents")

		configContent := `[llm]
provider = "openai"
model = "gpt-4"
`
		err = os.WriteFile(".agents/asimi.conf", []byte(configContent), 0644)
		require.NoError(t, err)

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "sk-test-key", cfg.LLM.APIKey)
	})

	t.Run("load ANTHROPIC_API_KEY from environment", func(t *testing.T) {
		os.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
		defer os.Unsetenv("ANTHROPIC_API_KEY")

		err := os.MkdirAll(".agents", 0755)
		require.NoError(t, err)
		defer os.RemoveAll(".agents")

		configContent := `[llm]
provider = "anthropic"
model = "claude-3-opus"
`
		err = os.WriteFile(".agents/asimi.conf", []byte(configContent), 0644)
		require.NoError(t, err)

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "sk-ant-test-key", cfg.LLM.APIKey)
	})
}

func TestSaveConfig(t *testing.T) {
	skipIfNotCI(t)
	tempDir := t.TempDir()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	t.Run("save config falls back to user config", func(t *testing.T) {
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempDir)
		defer os.Setenv("HOME", originalHome)

		cfg := &Config{
			LLM: LLMConfig{
				Model: "gpt-4",
			},
		}

		err := SaveConfig(cfg)
		require.NoError(t, err)

		_, err = os.Stat(".config/asimi")
		assert.NoError(t, err)

		_, err = os.Stat(".config/asimi/asimi.conf")
		assert.NoError(t, err)

		_, err = os.Stat(".agents")
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("save config updates existing file", func(t *testing.T) {
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempDir)
		defer os.Setenv("HOME", originalHome)

		configDir := filepath.Join(tempDir, ".config", "asimi")
		err := os.MkdirAll(configDir, 0755)
		require.NoError(t, err)

		initialContent := `[llm]
provider = "openai"
model = "gpt-3.5-turbo"
`
		err = os.WriteFile(filepath.Join(configDir, "asimi.conf"), []byte(initialContent), 0644)
		require.NoError(t, err)

		cfg := &Config{
			LLM: LLMConfig{
				Provider: "openai",
				Model:    "gpt-4",
			},
		}

		err = SaveConfig(cfg)
		require.NoError(t, err)

		loadedConfig, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", loadedConfig.LLM.Model)
	})

	t.Run("save config preserves other settings", func(t *testing.T) {
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempDir)
		defer os.Setenv("HOME", originalHome)

		envVars := []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"}
		originalEnvs := make(map[string]string)
		for _, env := range envVars {
			originalEnvs[env] = os.Getenv(env)
			os.Unsetenv(env)
		}
		defer func() {
			for env, val := range originalEnvs {
				if val != "" {
					os.Setenv(env, val)
				}
			}
		}()

		configDir := filepath.Join(tempDir, ".config", "asimi")
		err := os.MkdirAll(configDir, 0755)
		require.NoError(t, err)

		initialContent := `[llm]
provider = "openai"
model = "gpt-3.5-turbo"
api_key = "test-key"

[history]
enabled = true
max_sessions = 50
`
		err = os.WriteFile(filepath.Join(configDir, "asimi.conf"), []byte(initialContent), 0644)
		require.NoError(t, err)

		cfg := &Config{
			LLM: LLMConfig{
				Provider: "openai",
				Model:    "gpt-4",
			},
		}

		err = SaveConfig(cfg)
		require.NoError(t, err)

		loadedConfig, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", loadedConfig.LLM.Model)
		assert.Equal(t, "openai", loadedConfig.LLM.Provider)
		assert.True(t, loadedConfig.History.Enabled)
		assert.Equal(t, 50, loadedConfig.History.MaxSessions)
	})
}

func TestSetProjectConfig(t *testing.T) {
	skipIfNotCI(t)
	tempDir := t.TempDir()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	t.Run("creates directory and file if not exists", func(t *testing.T) {
		err := SetProjectConfig("session", "agents_file", "CLAUDE.md")
		require.NoError(t, err)

		_, err = os.Stat(".agents")
		assert.NoError(t, err)

		content, err := os.ReadFile(".agents/asimi.conf")
		require.NoError(t, err)
		assert.Contains(t, string(content), `agents_file = "CLAUDE.md"`)
	})

	t.Run("updates existing file preserving other settings", func(t *testing.T) {
		err := os.MkdirAll(".agents", 0755)
		require.NoError(t, err)

		initialContent := `[llm]
provider = "openai"
model = "gpt-4"

[session]
enabled = true
`
		err = os.WriteFile(".agents/asimi.conf", []byte(initialContent), 0644)
		require.NoError(t, err)

		err = SetProjectConfig("session", "agents_file", "CLAUDE.md")
		require.NoError(t, err)

		content, err := os.ReadFile(".agents/asimi.conf")
		require.NoError(t, err)
		contentStr := string(content)

		assert.Contains(t, contentStr, `agents_file = "CLAUDE.md"`)
		assert.Contains(t, contentStr, `provider = "openai"`)
		assert.Contains(t, contentStr, `model = "gpt-4"`)
		assert.Contains(t, contentStr, `enabled = true`)
	})

	t.Run("updates existing key value", func(t *testing.T) {
		err := os.MkdirAll(".agents", 0755)
		require.NoError(t, err)

		initialContent := `[session]
agents_file = "AGENTS.md"
`
		err = os.WriteFile(".agents/asimi.conf", []byte(initialContent), 0644)
		require.NoError(t, err)

		err = SetProjectConfig("session", "agents_file", "CLAUDE.md")
		require.NoError(t, err)

		content, err := os.ReadFile(".agents/asimi.conf")
		require.NoError(t, err)
		contentStr := string(content)

		assert.Contains(t, contentStr, `agents_file = "CLAUDE.md"`)
		assert.NotContains(t, contentStr, `agents_file = "AGENTS.md"`)
	})

	t.Run("sets multiple key-value pairs", func(t *testing.T) {
		os.RemoveAll(".agents")

		err := SetProjectConfig("session",
			"agents_file", "CLAUDE.md",
			"enabled", "true",
			"max_sessions", "100",
		)
		require.NoError(t, err)

		content, err := os.ReadFile(".agents/asimi.conf")
		require.NoError(t, err)
		contentStr := string(content)

		assert.Contains(t, contentStr, `agents_file = "CLAUDE.md"`)
		assert.Contains(t, contentStr, `enabled = "true"`)
		assert.Contains(t, contentStr, `max_sessions = "100"`)
	})

	t.Run("errors on odd number of key-value args", func(t *testing.T) {
		err := SetProjectConfig("session", "agents_file")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "even number")
	})
}

func TestEnsureUserConfigExists(t *testing.T) {
	t.Run("creates config file on first run", func(t *testing.T) {
		tempHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempHome)
		defer os.Setenv("HOME", originalHome)

		configPath := filepath.Join(tempHome, ".config", "asimi", "asimi.conf")
		_, err := os.Stat(configPath)
		require.True(t, os.IsNotExist(err), "Config should not exist before test")

		created, err := EnsureUserConfigExists()
		require.NoError(t, err)
		assert.True(t, created, "Should return true when config is created")

		_, err = os.Stat(configPath)
		require.NoError(t, err, "Config file should exist after EnsureUserConfigExists")

		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Contains(t, string(content), "# Asimi Default Configuration File")
		assert.Contains(t, string(content), "[llm]")
	})

	t.Run("returns false when config already exists", func(t *testing.T) {
		tempHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempHome)
		defer os.Setenv("HOME", originalHome)

		configDir := filepath.Join(tempHome, ".config", "asimi")
		err := os.MkdirAll(configDir, 0755)
		require.NoError(t, err)

		configPath := filepath.Join(configDir, "asimi.conf")
		existingContent := "[llm]\nprovider = \"anthropic\"\n"
		err = os.WriteFile(configPath, []byte(existingContent), 0644)
		require.NoError(t, err)

		created, err := EnsureUserConfigExists()
		require.NoError(t, err)
		assert.False(t, created, "Should return false when config already exists")

		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Equal(t, existingContent, string(content), "Existing config should not be modified")
	})

	t.Run("creates directory if it doesn't exist", func(t *testing.T) {
		tempHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempHome)
		defer os.Setenv("HOME", originalHome)

		configDir := filepath.Join(tempHome, ".config", "asimi")
		_, err := os.Stat(configDir)
		require.True(t, os.IsNotExist(err), "Config directory should not exist before test")

		created, err := EnsureUserConfigExists()
		require.NoError(t, err)
		assert.True(t, created)

		_, err = os.Stat(configDir)
		require.NoError(t, err, "Config directory should exist after EnsureUserConfigExists")
	})
}

// =============================================================================
// TOML Comment Preservation Tests
// =============================================================================

func TestFindTOMLSectionBounds(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		section     string
		expectStart int
		expectEnd   int
		expectFound bool
	}{
		{
			name: "find section in middle",
			content: `[storage]
path = "/data"

[llm]
provider = "openai"
model = "gpt-4"

[history]
enabled = true`,
			section:     "llm",
			expectStart: 3,
			expectEnd:   7,
			expectFound: true,
		},
		{
			name: "find first section",
			content: `[llm]
provider = "openai"

[history]
enabled = true`,
			section:     "llm",
			expectStart: 0,
			expectEnd:   3,
			expectFound: true,
		},
		{
			name: "find last section",
			content: `[llm]
provider = "openai"

[history]
enabled = true`,
			section:     "history",
			expectStart: 3,
			expectEnd:   5,
			expectFound: true,
		},
		{
			name: "section not found",
			content: `[llm]
provider = "openai"`,
			section:     "history",
			expectStart: -1,
			expectEnd:   2,
			expectFound: false,
		},
		{
			name:        "empty content",
			content:     "",
			section:     "llm",
			expectStart: -1,
			expectEnd:   1,
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.content, "\n")
			start, end, found := FindTOMLSectionBounds(lines, tt.section)
			assert.Equal(t, tt.expectFound, found, "found mismatch")
			if found {
				assert.Equal(t, tt.expectStart, start, "start mismatch")
				assert.Equal(t, tt.expectEnd, end, "end mismatch")
			}
		})
	}
}

func TestUpdateTOMLValue(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		section           string
		key               string
		newValue          string
		expectFound       bool
		expectContains    []string
		expectNotContains []string
	}{
		{
			name: "update existing value",
			content: `[llm]
provider = "openai"
model = "gpt-3.5"`,
			section:           "llm",
			key:               "model",
			newValue:          "gpt-4",
			expectFound:       true,
			expectContains:    []string{`model = "gpt-4"`},
			expectNotContains: []string{`model = "gpt-3.5"`},
		},
		{
			name: "preserve inline comment",
			content: `[llm]
provider = "openai" # the provider
model = "gpt-3.5"`,
			section:        "llm",
			key:            "provider",
			newValue:       "anthropic",
			expectFound:    true,
			expectContains: []string{`provider = "anthropic" # the provider`},
		},
		{
			name: "preserve full-line comments",
			content: `[llm]
# This is the provider setting
provider = "openai"
# This is the model setting
model = "gpt-3.5"`,
			section:     "llm",
			key:         "provider",
			newValue:    "anthropic",
			expectFound: true,
			expectContains: []string{
				"# This is the provider setting",
				`provider = "anthropic"`,
				"# This is the model setting",
			},
		},
		{
			name: "key not found in section",
			content: `[llm]
provider = "openai"`,
			section:     "llm",
			key:         "model",
			newValue:    "gpt-4",
			expectFound: false,
		},
		{
			name: "section not found",
			content: `[storage]
path = "/data"`,
			section:     "llm",
			key:         "provider",
			newValue:    "openai",
			expectFound: false,
		},
		{
			name: "update value with special characters",
			content: `[llm]
api_key = "old-key"`,
			section:        "llm",
			key:            "api_key",
			newValue:       `new-key-with-"quotes"`,
			expectFound:    true,
			expectContains: []string{`api_key = "new-key-with-\"quotes\""`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, found := UpdateTOMLValue(tt.content, tt.section, tt.key, tt.newValue)
			assert.Equal(t, tt.expectFound, found)
			for _, s := range tt.expectContains {
				assert.Contains(t, result, s)
			}
			for _, s := range tt.expectNotContains {
				assert.NotContains(t, result, s)
			}
		})
	}
}

func TestInsertTOMLValue(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		section        string
		key            string
		value          string
		expectContains []string
	}{
		{
			name: "insert into existing section",
			content: `[llm]
provider = "openai"`,
			section: "llm",
			key:     "model",
			value:   "gpt-4",
			expectContains: []string{
				`provider = "openai"`,
				`model = "gpt-4"`,
			},
		},
		{
			name: "insert preserves comments",
			content: `[llm]
# Provider setting
provider = "openai"

[history]
enabled = true`,
			section: "llm",
			key:     "model",
			value:   "gpt-4",
			expectContains: []string{
				"# Provider setting",
				`provider = "openai"`,
				`model = "gpt-4"`,
				"[history]",
			},
		},
		{
			name: "section not found returns unchanged",
			content: `[storage]
path = "/data"`,
			section: "llm",
			key:     "provider",
			value:   "openai",
			expectContains: []string{
				"[storage]",
				`path = "/data"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := InsertTOMLValue(tt.content, tt.section, tt.key, tt.value)
			for _, s := range tt.expectContains {
				assert.Contains(t, result, s)
			}
		})
	}
}

func TestRemoveTOMLKey(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		section           string
		key               string
		expectContains    []string
		expectNotContains []string
	}{
		{
			name: "remove existing key",
			content: `[llm]
provider = "openai"
api_key = "secret"
model = "gpt-4"`,
			section: "llm",
			key:     "api_key",
			expectContains: []string{
				`provider = "openai"`,
				`model = "gpt-4"`,
			},
			expectNotContains: []string{
				"api_key",
				"secret",
			},
		},
		{
			name: "remove preserves surrounding comments",
			content: `[llm]
# Provider setting
provider = "openai"
# API key (to be removed)
api_key = "secret"
# Model setting
model = "gpt-4"`,
			section: "llm",
			key:     "api_key",
			expectContains: []string{
				"# Provider setting",
				`provider = "openai"`,
				"# API key (to be removed)",
				"# Model setting",
				`model = "gpt-4"`,
			},
			expectNotContains: []string{
				`api_key = "secret"`,
			},
		},
		{
			name: "key not found returns unchanged",
			content: `[llm]
provider = "openai"`,
			section: "llm",
			key:     "api_key",
			expectContains: []string{
				`provider = "openai"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RemoveTOMLKey(tt.content, tt.section, tt.key)
			for _, s := range tt.expectContains {
				assert.Contains(t, result, s)
			}
			for _, s := range tt.expectNotContains {
				assert.NotContains(t, result, s)
			}
		})
	}
}

func TestEnsureTOMLSection(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		section        string
		expectContains []string
	}{
		{
			name: "section already exists",
			content: `[llm]
provider = "openai"`,
			section: "llm",
			expectContains: []string{
				"[llm]",
				`provider = "openai"`,
			},
		},
		{
			name:    "add new section to empty content",
			content: "",
			section: "llm",
			expectContains: []string{
				"[llm]",
			},
		},
		{
			name: "add new section to existing content",
			content: `[storage]
path = "/data"`,
			section: "llm",
			expectContains: []string{
				"[storage]",
				`path = "/data"`,
				"[llm]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EnsureTOMLSection(tt.content, tt.section)
			for _, s := range tt.expectContains {
				assert.Contains(t, result, s)
			}
		})
	}
}

func TestUpdateOrInsertTOMLValue(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		section        string
		key            string
		value          string
		expectContains []string
	}{
		{
			name: "update existing key",
			content: `[llm]
provider = "openai"`,
			section: "llm",
			key:     "provider",
			value:   "anthropic",
			expectContains: []string{
				`provider = "anthropic"`,
			},
		},
		{
			name: "insert new key in existing section",
			content: `[llm]
provider = "openai"`,
			section: "llm",
			key:     "model",
			value:   "gpt-4",
			expectContains: []string{
				`provider = "openai"`,
				`model = "gpt-4"`,
			},
		},
		{
			name:    "create section and insert key",
			content: "",
			section: "llm",
			key:     "provider",
			value:   "openai",
			expectContains: []string{
				"[llm]",
				`provider = "openai"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UpdateOrInsertTOMLValue(tt.content, tt.section, tt.key, tt.value)
			for _, s := range tt.expectContains {
				assert.Contains(t, result, s)
			}
		})
	}
}

func TestSaveConfigPreservesComments(t *testing.T) {
	skipIfNotCI(t)
	tempDir := t.TempDir()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	t.Run("preserves comments when updating", func(t *testing.T) {
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempDir)
		defer os.Setenv("HOME", originalHome)

		configDir := filepath.Join(tempDir, ".config", "asimi")
		err := os.MkdirAll(configDir, 0755)
		require.NoError(t, err)

		initialContent := `# Project configuration
# This file configures the LLM settings

[llm]
# The LLM provider to use
provider = "openai"
# The model name
model = "gpt-3.5-turbo" # default model

[history]
# Enable history tracking
enabled = true
`
		err = os.WriteFile(filepath.Join(configDir, "asimi.conf"), []byte(initialContent), 0644)
		require.NoError(t, err)

		cfg := &Config{
			LLM: LLMConfig{
				Provider: "anthropic",
				Model:    "claude-3-opus",
			},
		}

		err = SaveConfig(cfg)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(configDir, "asimi.conf"))
		require.NoError(t, err)
		contentStr := string(content)

		assert.Contains(t, contentStr, "# Project configuration")
		assert.Contains(t, contentStr, "# This file configures the LLM settings")
		assert.Contains(t, contentStr, "# The LLM provider to use")
		assert.Contains(t, contentStr, "# The model name")
		assert.Contains(t, contentStr, "# Enable history tracking")

		assert.Contains(t, contentStr, `provider = "anthropic"`)
		assert.Contains(t, contentStr, `model = "claude-3-opus"`)

		assert.Contains(t, contentStr, "[history]")
		assert.Contains(t, contentStr, "enabled = true")
	})

	t.Run("preserves inline comments", func(t *testing.T) {
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempDir)
		defer os.Setenv("HOME", originalHome)

		configDir := filepath.Join(tempDir, ".config", "asimi")
		err := os.MkdirAll(configDir, 0755)
		require.NoError(t, err)

		initialContent := `[llm]
provider = "openai" # cloud provider
model = "gpt-3.5" # fast model
`
		err = os.WriteFile(filepath.Join(configDir, "asimi.conf"), []byte(initialContent), 0644)
		require.NoError(t, err)

		cfg := &Config{
			LLM: LLMConfig{
				Provider: "anthropic",
				Model:    "claude-3",
			},
		}

		err = SaveConfig(cfg)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(configDir, "asimi.conf"))
		require.NoError(t, err)
		contentStr := string(content)

		assert.Contains(t, contentStr, `provider = "anthropic" # cloud provider`)
		assert.Contains(t, contentStr, `model = "claude-3" # fast model`)
	})
}
