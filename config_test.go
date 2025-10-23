package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			result := escapeTOMLString(tt.input)
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

			result := getEnv(tt.key, tt.fallback)
			assert.Equal(t, tt.expected, result)
		})
	}
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
				os.Setenv("ASIMI_OAUTH_GOOGLE_CLIENT_ID", "test-client-id")
				os.Setenv("ASIMI_OAUTH_GOOGLE_CLIENT_SECRET", "test-secret")
			},
			cleanupEnv: func() {
				os.Unsetenv("ASIMI_OAUTH_GOOGLE_CLIENT_ID")
				os.Unsetenv("ASIMI_OAUTH_GOOGLE_CLIENT_SECRET")
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
				os.Setenv("ASIMI_OAUTH_GOOGLE_CLIENT_ID", "test-client-id")
				os.Setenv("ASIMI_OAUTH_GOOGLE_CLIENT_SECRET", "test-secret")
				os.Setenv("ASIMI_OAUTH_GOOGLE_SCOPES", "scope1,scope2")
			},
			cleanupEnv: func() {
				os.Unsetenv("ASIMI_OAUTH_GOOGLE_CLIENT_ID")
				os.Unsetenv("ASIMI_OAUTH_GOOGLE_CLIENT_SECRET")
				os.Unsetenv("ASIMI_OAUTH_GOOGLE_SCOPES")
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
				os.Setenv("ASIMI_OAUTH_OPENAI_AUTH_URL", "https://auth.openai.com")
				os.Setenv("ASIMI_OAUTH_OPENAI_TOKEN_URL", "https://token.openai.com")
				os.Setenv("ASIMI_OAUTH_OPENAI_CLIENT_ID", "openai-client")
				os.Setenv("ASIMI_OAUTH_OPENAI_CLIENT_SECRET", "openai-secret")
			},
			cleanupEnv: func() {
				os.Unsetenv("ASIMI_OAUTH_OPENAI_AUTH_URL")
				os.Unsetenv("ASIMI_OAUTH_OPENAI_TOKEN_URL")
				os.Unsetenv("ASIMI_OAUTH_OPENAI_CLIENT_ID")
				os.Unsetenv("ASIMI_OAUTH_OPENAI_CLIENT_SECRET")
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
				os.Setenv("ASIMI_OAUTH_GOOGLE_CLIENT_SECRET", "test-secret")
			},
			cleanupEnv: func() {
				os.Unsetenv("ASIMI_OAUTH_GOOGLE_CLIENT_SECRET")
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

func TestLoadConfig(t *testing.T) {
	// Create a temporary directory for test configs
	tempDir := t.TempDir()

	// Save current directory and change to temp
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	t.Run("load with defaults", func(t *testing.T) {
		config, err := LoadConfig()
		require.NoError(t, err)
		assert.NotNil(t, config)
		// History should be enabled by default
		assert.True(t, config.History.Enabled)
	})

	t.Run("load with project config", func(t *testing.T) {
		// Create .asimi directory and config
		err := os.MkdirAll(".asimi", 0755)
		require.NoError(t, err)

		configContent := `[llm]
provider = "openai"
model = "gpt-4"
api_key = "test-key"

[history]
enabled = false
max_sessions = 100
`
		err = os.WriteFile(".asimi/conf.toml", []byte(configContent), 0644)
		require.NoError(t, err)
		defer os.RemoveAll(".asimi")

		config, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "openai", config.LLM.Provider)
		assert.Equal(t, "gpt-4", config.LLM.Model)
		assert.False(t, config.History.Enabled)
		assert.Equal(t, 100, config.History.MaxSessions)
	})

	t.Run("environment variables override config", func(t *testing.T) {
		// Set environment variable
		os.Setenv("ASIMI_LLM_PROVIDER", "anthropic")
		os.Setenv("ASIMI_LLM_MODEL", "claude-3-opus")
		defer os.Unsetenv("ASIMI_LLM_PROVIDER")
		defer os.Unsetenv("ASIMI_LLM_MODEL")

		// Create project config with different values
		err := os.MkdirAll(".asimi", 0755)
		require.NoError(t, err)
		defer os.RemoveAll(".asimi")

		configContent := `[llm]
provider = "openai"
model = "gpt-4"
`
		err = os.WriteFile(".asimi/conf.toml", []byte(configContent), 0644)
		require.NoError(t, err)

		config, err := LoadConfig()
		require.NoError(t, err)
		// Environment variables should override file config
		assert.Equal(t, "anthropic", config.LLM.Provider)
		assert.Equal(t, "claude-3-opus", config.LLM.Model)
	})

	t.Run("load OPENAI_API_KEY from environment", func(t *testing.T) {
		os.Setenv("OPENAI_API_KEY", "sk-test-key")
		defer os.Unsetenv("OPENAI_API_KEY")

		// Create config with openai provider but no api_key
		err := os.MkdirAll(".asimi", 0755)
		require.NoError(t, err)
		defer os.RemoveAll(".asimi")

		configContent := `[llm]
provider = "openai"
model = "gpt-4"
`
		err = os.WriteFile(".asimi/conf.toml", []byte(configContent), 0644)
		require.NoError(t, err)

		config, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "sk-test-key", config.LLM.APIKey)
	})

	t.Run("load ANTHROPIC_API_KEY from environment", func(t *testing.T) {
		os.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
		defer os.Unsetenv("ANTHROPIC_API_KEY")

		// Create config with anthropic provider but no api_key
		err := os.MkdirAll(".asimi", 0755)
		require.NoError(t, err)
		defer os.RemoveAll(".asimi")

		configContent := `[llm]
provider = "anthropic"
model = "claude-3-opus"
`
		err = os.WriteFile(".asimi/conf.toml", []byte(configContent), 0644)
		require.NoError(t, err)

		config, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "sk-ant-test-key", config.LLM.APIKey)
	})
}

func TestSaveConfig(t *testing.T) {
	// Create a temporary directory for test
	tempDir := t.TempDir()

	// Save current directory and change to temp
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	t.Run("save config creates directory", func(t *testing.T) {
		config := &Config{
			LLM: LLMConfig{
				Model: "gpt-4",
			},
		}

		err := SaveConfig(config)
		require.NoError(t, err)

		// Check that .asimi directory was created
		_, err = os.Stat(".asimi")
		assert.NoError(t, err)

		// Check that config file was created
		_, err = os.Stat(".asimi/conf.toml")
		assert.NoError(t, err)
	})

	t.Run("save config updates existing file", func(t *testing.T) {
		// Create initial config
		err := os.MkdirAll(".asimi", 0755)
		require.NoError(t, err)
		defer os.RemoveAll(".asimi")

		initialContent := `[llm]
provider = "openai"
model = "gpt-3.5-turbo"
`
		err = os.WriteFile(".asimi/conf.toml", []byte(initialContent), 0644)
		require.NoError(t, err)

		// Update config
		config := &Config{
			LLM: LLMConfig{
				Model: "gpt-4",
			},
		}

		err = SaveConfig(config)
		require.NoError(t, err)

		// Load and verify
		loadedConfig, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", loadedConfig.LLM.Model)
	})

	t.Run("save config preserves other settings", func(t *testing.T) {
		// Create config with multiple settings
		err := os.MkdirAll(".asimi", 0755)
		require.NoError(t, err)
		defer os.RemoveAll(".asimi")

		initialContent := `[llm]
provider = "openai"
model = "gpt-3.5-turbo"
api_key = "test-key"

[history]
enabled = true
max_sessions = 50
`
		err = os.WriteFile(".asimi/conf.toml", []byte(initialContent), 0644)
		require.NoError(t, err)

		// Update only model
		config := &Config{
			LLM: LLMConfig{
				Model: "gpt-4",
			},
		}

		err = SaveConfig(config)
		require.NoError(t, err)

		// Load and verify other settings are preserved
		loadedConfig, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", loadedConfig.LLM.Model)
		assert.Equal(t, "openai", loadedConfig.LLM.Provider)
		assert.True(t, loadedConfig.History.Enabled)
		assert.Equal(t, 50, loadedConfig.History.MaxSessions)
	})
}

// NOTE: UpdateUserLLMAuth tests are disabled because they trigger system keyring dialogs.
// To test this function manually, set ASIMI_TEST_KEYRING=1 and run:
//   ASIMI_TEST_KEYRING=1 go test -v -run TestUpdateUserLLMAuthIntegration

func TestUpdateUserLLMAuthIntegration(t *testing.T) {
	// Skip unless explicitly enabled
	if os.Getenv("ASIMI_TEST_KEYRING") != "1" {
		t.Skip("Skipping UpdateUserLLMAuth test. Set ASIMI_TEST_KEYRING=1 to run this test manually.")
	}

	t.Log("⚠️  WARNING: This test will trigger system keyring dialogs!")

	t.Run("creates config file if not exists", func(t *testing.T) {
		// Create a temporary home directory
		tempHome := t.TempDir()
		originalHome := os.Getenv("HOME")
		os.Setenv("HOME", tempHome)
		defer os.Setenv("HOME", originalHome)

		err := UpdateUserLLMAuth("openai", "test-api-key", "gpt-4")
		require.NoError(t, err)

		// Check if config directory was created
		configDir := filepath.Join(tempHome, ".config", "asimi")
		_, err = os.Stat(configDir)
		require.NoError(t, err)

		// Check the file
		configPath := filepath.Join(configDir, "conf.toml")
		_, err = os.Stat(configPath)
		assert.NoError(t, err, "Config file should be created")
	})
}
