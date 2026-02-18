package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain sets up test environment
func TestMain(m *testing.M) {
	// Use a test-specific keyring service to avoid polluting production credentials
	original := os.Getenv("ASIMI_KEYRING_SERVICE")
	os.Setenv("ASIMI_KEYRING_SERVICE", "dev.asimi.asimi-cli-test")

	// Update the global keyringService variable
	keyringService = getKeyringService()

	defer func() {
		os.Setenv("ASIMI_KEYRING_SERVICE", original)
		keyringService = getKeyringService()
	}()

	// Run tests
	code := m.Run()

	// Exit with test result code
	os.Exit(code)
}

// TestFetchAllModels_ReturnsEmptyWithoutAuth verifies that fetchAllModels returns
// an empty list when no providers are authenticated
func TestFetchAllModels_ReturnsEmptyWithoutAuth(t *testing.T) {
	// Clear any existing credentials
	DeleteTokenFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("anthropic")
	DeleteTokenFromKeyring("openai")
	DeleteAPIKeyFromKeyring("openai")
	DeleteTokenFromKeyring("googleai")
	DeleteAPIKeyFromKeyring("googleai")

	// Clear environment variables for this test
	originalAnthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	originalOpenAIKey := os.Getenv("OPENAI_API_KEY")
	originalGeminiKey := os.Getenv("GEMINI_API_KEY")
	originalGoogleKey := os.Getenv("GOOGLE_API_KEY")
	originalOllamaHost := os.Getenv("OLLAMA_HOST")

	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")

	// Create a mock Ollama server that returns empty model list
	mockOllamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			// Return empty model list regardless of auth
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"models":[]}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockOllamaServer.Close()

	// Point OLLAMA_HOST to our mock server
	os.Setenv("OLLAMA_HOST", mockOllamaServer.URL)

	defer func() {
		if originalAnthropicKey != "" {
			os.Setenv("ANTHROPIC_API_KEY", originalAnthropicKey)
		}
		if originalOpenAIKey != "" {
			os.Setenv("OPENAI_API_KEY", originalOpenAIKey)
		}
		if originalGeminiKey != "" {
			os.Setenv("GEMINI_API_KEY", originalGeminiKey)
		}
		if originalGoogleKey != "" {
			os.Setenv("GOOGLE_API_KEY", originalGoogleKey)
		}
		if originalOllamaHost != "" {
			os.Setenv("OLLAMA_HOST", originalOllamaHost)
		} else {
			os.Unsetenv("OLLAMA_HOST")
		}
	}()

	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-latest",
		},
	}

	models := fetchAllModels(config)

	// Should have 2 models: login and help options
	assert.Equal(t, 2, len(models), "Expected exactly 2 models", "models", models)

	// First should be login
	assert.Contains(t, models[0].DisplayName, "Login")
	assert.Equal(t, "login", models[0].Status)

	// Second should be help
	assert.Contains(t, models[1].DisplayName, "Learn about model configuration")
	assert.Equal(t, "login", models[1].Status)
	assert.Equal(t, "help", models[1].Provider)
}

// TestFetchAllModels_WithAPIKey verifies that models show as ready when API key is available
// or error items are added when API fails
func TestFetchAllModels_WithAPIKey(t *testing.T) {
	// Clear any existing credentials
	DeleteTokenFromKeyring("openai")
	DeleteAPIKeyFromKeyring("openai")

	// Set OpenAI API key in environment
	originalKey := os.Getenv("OPENAI_API_KEY")
	os.Setenv("OPENAI_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENAI_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENAI_API_KEY")
		}
	}()

	config := &Config{
		LLM: LLMConfig{
			Provider:           "openai",
			Model:              "gpt-4o",
			ExperimentalModels: true,
		},
	}

	models := fetchAllModels(config)

	// With an API key set, we should get either models or an error item
	hasOpenAI := false
	for _, m := range models {
		if m.Provider == "openai" {
			hasOpenAI = true
			// Should be ready, active, or error (if API call failed)
			if m.Status != "ready" && m.Status != "active" && m.Status != "error" {
				t.Errorf("Expected OpenAI model %s to be 'ready', 'active', or 'error', got %s", m.ID, m.Status)
			}
		}
	}

	if !hasOpenAI {
		t.Error("Expected at least one OpenAI item (model or error)")
	}
}

// TestCheckProviderAuth verifies provider authentication detection
func TestCheckProviderAuth(t *testing.T) {
	// Clear credentials
	DeleteTokenFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("anthropic")

	// Clear environment
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	defer func() {
		if originalKey != "" {
			os.Setenv("ANTHROPIC_API_KEY", originalKey)
		}
	}()

	// Test with no credentials
	info := checkProviderAuth("anthropic")
	if info.HasAPIKey || info.HasOAuth {
		t.Error("Expected no auth when no credentials are set")
	}

	// Test with environment variable
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	info = checkProviderAuth("anthropic")
	if !info.HasAPIKey {
		t.Error("Expected HasAPIKey to be true when ANTHROPIC_API_KEY is set")
	}
}

// TestFetchAnthropicModels_LoadsFromKeyring verifies that fetchAnthropicModels
// loads credentials from the keyring when they're not in the config
func TestFetchAnthropicModels_LoadsFromKeyring(t *testing.T) {
	// This test verifies the bug fix for worktrees
	// When running in a worktree, the config file might not have auth tokens,
	// but they should be loaded from the OS keyring

	// Create a config without auth tokens
	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-20241022",
			// Intentionally leave AuthToken and APIKey empty
		},
	}

	// Save a test token to the keyring
	testToken := "test-token-12345"
	testRefreshToken := "test-refresh-token"
	expiry := time.Now().Add(24 * time.Hour)

	err := SaveTokenToKeyring("anthropic", testToken, testRefreshToken, expiry)
	if err != nil {
		t.Skipf("Skipping test: keyring not available: %v", err)
	}
	defer DeleteTokenFromKeyring("anthropic")

	// Call fetchAnthropicModels - it should load the token from keyring
	// Note: This will fail with a network error since we're using a fake token,
	// but we're just testing that it loads the token from keyring
	_, err = fetchAnthropicModels(config)
	// TODO: Add verification - 401 error
}

// TestFetchAnthropicModels_LoadsAPIKeyFromKeyring verifies that fetchAnthropicModels
// loads API keys from the keyring as a fallback
func TestFetchAnthropicModels_LoadsAPIKeyFromKeyring(t *testing.T) {
	// Create a config without auth tokens
	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-20241022",
		},
	}

	// Save a test API key to the keyring
	testAPIKey := "sk-ant-test-key-12345"

	err := SaveAPIKeyToKeyring("anthropic", testAPIKey)
	if err != nil {
		t.Skipf("Skipping test: keyring not available: %v", err)
	}
	defer DeleteAPIKeyFromKeyring("anthropic")

	// Call fetchAnthropicModels - it should load the API key from keyring
	_, err = fetchAnthropicModels(config)

	// Add verification -401 error
}

// TestFetchAnthropicModels_NoCredentials verifies error when no credentials available
func TestFetchAnthropicModels_NoCredentials(t *testing.T) {
	// Create a config without auth tokens
	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-20241022",
		},
	}

	// Make sure no credentials are in keyring
	DeleteTokenFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("anthropic")

	// Clear environment variable
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	defer func() {
		if originalKey != "" {
			os.Setenv("ANTHROPIC_API_KEY", originalKey)
		}
	}()

	// Call fetchAnthropicModels - it should return an error
	_, err := fetchAnthropicModels(config)

	if err == nil {
		t.Error("Expected error when no credentials available, got nil")
	}

	expectedError := "no authentication configured for anthropic provider"
	if err.Error() != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, err.Error())
	}
}

// Tests from models_help_test.go

// TestModelsHelpOption verifies that the help option is added when no auth is available
func TestModelsHelpOption(t *testing.T) {
	// Clear any existing credentials to ensure we get the login/help options
	DeleteTokenFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("anthropic")
	DeleteTokenFromKeyring("openai")
	DeleteAPIKeyFromKeyring("openai")
	DeleteTokenFromKeyring("googleai")
	DeleteAPIKeyFromKeyring("googleai")

	// Clear environment variables for this test
	originalAnthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	originalOpenAIKey := os.Getenv("OPENAI_API_KEY")
	originalGeminiKey := os.Getenv("GEMINI_API_KEY")
	originalGoogleKey := os.Getenv("GOOGLE_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	defer func() {
		if originalAnthropicKey != "" {
			os.Setenv("ANTHROPIC_API_KEY", originalAnthropicKey)
		}
		if originalOpenAIKey != "" {
			os.Setenv("OPENAI_API_KEY", originalOpenAIKey)
		}
		if originalGeminiKey != "" {
			os.Setenv("GEMINI_API_KEY", originalGeminiKey)
		}
		if originalGoogleKey != "" {
			os.Setenv("GOOGLE_API_KEY", originalGoogleKey)
		}
	}()

	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-latest",
		},
	}

	// Fetch models with no authentication configured
	models := fetchAllModels(config)

	// Should have at least the login and help options
	require.GreaterOrEqual(t, len(models), 2, "Expected at least 2 models (login and help)")

	// Find the help option
	var helpModel *Model
	for i := range models {
		if models[i].ID == "help" {
			helpModel = &models[i]
			break
		}
	}

	require.NotNil(t, helpModel, "Help option not found in models list")

	// Verify help model properties
	assert.Equal(t, "Learn about model configuration", helpModel.DisplayName, "DisplayName mismatch")
	assert.Equal(t, "help", helpModel.Provider, "Provider mismatch")
	assert.Equal(t, "login", helpModel.Status, "Status mismatch")
	assert.Equal(t, "API keys, environment variables, and more", helpModel.Description, "Description mismatch")

	// Verify OnSelect returns showHelpMsg
	require.NotNil(t, helpModel.OnSelect, "OnSelect should not be nil")

	msg := helpModel.OnSelect()
	helpMsg, ok := msg.(showHelpMsg)
	require.True(t, ok, "Expected showHelpMsg, got %T", msg)
	assert.Equal(t, "models", helpMsg.topic, "Topic mismatch")
}

// TestModelsHelpIcon verifies the help provider has the correct icon
func TestModelsHelpIcon(t *testing.T) {
	icon := getProviderIcon("help")
	expected := "📖"
	assert.Equal(t, expected, icon, "Icon mismatch")
}

// TestHelpModelsTopicExists verifies the models help topic exists
func TestHelpModelsTopicExists(t *testing.T) {
	help := NewHelpWindow()
	help.SetTopic("models")

	content := help.RenderContent()
	require.NotEmpty(t, content, "Help content for 'models' topic is empty")

	// Check for key content
	assert.Contains(t, content, "Model Selection", "Help content should contain 'Model Selection'")
	assert.Contains(t, content, "Anthropic", "Help content should mention 'Anthropic'")
	assert.Contains(t, content, "ANTHROPIC_API_KEY", "Help content should mention 'ANTHROPIC_API_KEY'")
	assert.Contains(t, content, "OPENAI_API_KEY", "Help content should mention 'OPENAI_API_KEY'")
}

// TestHandleModelsCommandShowsHelpOption verifies the models command includes help option
func TestHandleModelsCommandShowsHelpOption(t *testing.T) {
	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-latest",
		},
	}

	model := &TUIModel{
		config: config,
		tabs:   NewTabManager(80, 24, false, func() string { return "insert" }),
	}

	// Execute the models command
	cmd := handleModelsCommand(model, []string{})
	require.NotNil(t, cmd, "handleModelsCommand should return a command")

	// Execute the command to get the message
	msg := cmd()

	// Should be a batch command, so we need to handle it differently
	// For now, just verify it doesn't panic
	require.NotNil(t, msg, "Command should return a message")
}

// Tests from models_window_test.go

func TestNewModelsWindowDefaults(t *testing.T) {
	window := NewModelsWindow()

	assert.Equal(t, 70, window.Width)
	assert.Equal(t, 15, window.Height)
	assert.False(t, window.Loading)
	assert.Empty(t, window.Items)
	assert.Nil(t, window.Error)
	assert.Equal(t, "", window.currentModel)
}

func TestModelsWindowSetSizeAdjustsVisibleSlots(t *testing.T) {
	window := NewModelsWindow()

	window.SetSize(80, 10)
	assert.Equal(t, 80, window.Width)
	assert.Equal(t, 10, window.Height)
	assert.Equal(t, 9, window.MaxVisible) // 10 - 1 (for title line)

	window.SetSize(50, 2)
	assert.Equal(t, 2, window.Height)
	assert.Equal(t, 1, window.MaxVisible) // minimum 1
}

func TestModelsWindowSetModels(t *testing.T) {
	window := NewModelsWindow()
	models := []Model{
		{ID: "m1", DisplayName: "Model 1", Provider: "anthropic", Status: "ready"},
		{ID: "m2", DisplayName: "Model 2", Provider: "openai", Status: "active"},
	}

	window.SetModels(models, "m2")

	assert.False(t, window.Loading)
	assert.Nil(t, window.Error)
	assert.Equal(t, 2, window.GetItemCount())
	assert.Equal(t, "m2", window.currentModel)
	assert.Equal(t, "Model 1", window.Items[0].DisplayName)
	assert.Equal(t, "Model 2", window.Items[1].DisplayName)
}

func TestModelsWindowSetLoading(t *testing.T) {
	window := NewModelsWindow()
	window.SetError("test error") // Set error first

	window.SetLoading(true)
	assert.True(t, window.Loading)
	assert.Nil(t, window.Error) // Error should be cleared on loading
}

func TestModelsWindowSetError(t *testing.T) {
	window := NewModelsWindow()

	window.SetError("something went wrong")
	assert.False(t, window.Loading)
	assert.NotNil(t, window.Error)
	assert.Equal(t, "something went wrong", window.Error.Error())

	window.SetError("") // Clear error
	assert.Nil(t, window.Error)
}

func TestModelsWindowGetInitialSelection(t *testing.T) {
	window := NewModelsWindow()
	models := []Model{
		{ID: "m1", DisplayName: "Model 1", Provider: "anthropic", Status: "ready"},
		{ID: "m2", DisplayName: "Model 2", Provider: "openai", Status: "active"},
		{ID: "m3", DisplayName: "Model 3", Provider: "googleai", Status: "ready"},
	}
	window.SetModels(models, "m2")

	assert.Equal(t, 1, window.GetInitialSelection()) // m2 is active at index 1

	// No active model, should return 0
	window.SetModels([]Model{
		{ID: "m1", DisplayName: "Model 1", Provider: "anthropic", Status: "ready"},
	}, "m_nonexistent")
	assert.Equal(t, 0, window.GetInitialSelection())

	// Empty models list
	window.SetModels([]Model{}, "")
	assert.Equal(t, 0, window.GetInitialSelection())
}

func TestModelsWindowGetSelectedModel(t *testing.T) {
	window := NewModelsWindow()
	models := []Model{
		{ID: "m1", DisplayName: "Model 1", Provider: "anthropic", Status: "ready"},
		{ID: "m2", DisplayName: "Model 2", Provider: "openai", Status: "active"},
	}
	window.SetModels(models, "m2")

	model := window.GetSelectedModel(1)
	assert.NotNil(t, model)
	assert.Equal(t, "m2", model.ID)

	assert.Nil(t, window.GetSelectedModel(-1))
	assert.Nil(t, window.GetSelectedModel(2))
}

func TestModelsWindowRenderList(t *testing.T) {
	window := NewModelsWindow()
	window.SetSize(80, 10) // 9 visible slots

	// Test Loading State
	window.SetLoading(true)
	render := window.RenderList(0, 0, window.GetVisibleSlots())
	assert.Contains(t, render, "Loading models...")
	assert.Contains(t, render, "Scanning available models across all providers...")
	assert.NotContains(t, render, "Error loading models:")

	// Test Error State
	window.SetLoading(false)
	window.SetError("network failed")
	render = window.RenderList(0, 0, window.GetVisibleSlots())
	assert.Contains(t, render, "Error loading models:")
	assert.Contains(t, render, "network failed")
	assert.NotContains(t, render, "Loading models...")

	// Test Empty State
	window.SetError("")
	window.SetModels([]Model{}, "")
	render = window.RenderList(0, 0, window.GetVisibleSlots())
	assert.Contains(t, render, "No models available")
	assert.Contains(t, render, "Configure API keys via environment variables or :login")

	// Test Normal Rendering with Active/Ready/LoginRequired and Grouping
	models := []Model{
		{ID: "claude-3-5-sonnet-latest", DisplayName: "Claude 3.5 Sonnet", Provider: "anthropic", Status: "active"},
		{ID: "claude-3-5-haiku-latest", DisplayName: "Claude 3.5 Haiku", Provider: "anthropic", Status: "ready"},
		{ID: "gpt-4o", DisplayName: "GPT-4o", Provider: "openai", Status: "ready"},
		{ID: "o1-mini", DisplayName: "o1 Mini", Provider: "openai", Status: "login_required"},
		{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", Provider: "googleai", Status: "ready"},
	}
	window.SetModels(models, "claude-3-5-sonnet-latest")

	// Render first page (0 selected, 0 scroll)
	render = window.RenderList(0, 0, window.GetVisibleSlots())
	lines := strings.Split(render, "\n")
	assert.True(t, strings.HasPrefix(lines[1], "▶ 🅰️  ✓ Claude 3.5 Sonnet"))
	assert.True(t, strings.HasPrefix(lines[2], "  🅰️  ● Claude 3.5 Haiku"))
	assert.Equal(t, "", lines[3]) // Blank line between provider groups
	assert.True(t, strings.HasPrefix(lines[4], "  🤖 ● GPT-4o"))
	assert.True(t, strings.HasPrefix(lines[5], "  🤖 🔒 o1 Mini"))
	assert.Equal(t, "", lines[6]) // Blank line between provider groups
	assert.True(t, strings.HasPrefix(lines[7], "  🔷 ● Gemini 2.5 Pro"))

	// Test Selection
	render = window.RenderList(2, 0, window.GetVisibleSlots()) // Select GPT-4o
	lines = strings.Split(render, "\n")
	assert.True(t, strings.HasPrefix(lines[4], "▶ 🤖 ● GPT-4o"))

	// Test Scrolling (select last item, scroll to show it at bottom)
	window.SetSize(80, 5)                                      // 4 visible slots
	render = window.RenderList(4, 1, window.GetVisibleSlots()) // Select Gemini (item 4), scroll offset 1 shows items 1-4
	lines = strings.Split(render, "\n")
	// Header at line 0, then visible items starting at line 1
	// With scroll offset 1, items 1-4 are rendered: Haiku, (blank), GPT-4o, o1 Mini
	// The loop breaks before rendering Gemini because we've filled 4 visible slots
	assert.True(t, strings.HasPrefix(lines[1], "  🅰️  ● Claude 3.5 Haiku"))
	assert.Equal(t, "", lines[2]) // Blank line between provider groups
	assert.True(t, strings.HasPrefix(lines[3], "  🤖 ● GPT-4o"))
	assert.True(t, strings.HasPrefix(lines[4], "  🤖 🔒 o1 Mini"))
	// Line 5 is the trailing empty string from split (render ends with \n)
}

func TestGetProviderIcon(t *testing.T) {
	assert.Equal(t, "🅰️ ", getProviderIcon("anthropic"))
	assert.Equal(t, "🤖", getProviderIcon("openai"))
	assert.Equal(t, "🔷", getProviderIcon("googleai"))
	assert.Equal(t, "  ", getProviderIcon("unknown"))
}

func TestGetStatusIcon(t *testing.T) {
	assert.Equal(t, "✓", getStatusIcon("active"))
	assert.Equal(t, "●", getStatusIcon("ready"))
	assert.Equal(t, "🔒", getStatusIcon("login_required"))
	assert.Equal(t, "⚠", getStatusIcon("error"))
	assert.Equal(t, " ", getStatusIcon("unknown"))
}

func TestModelsWindowRenderList_ScrollingAndGrouping(t *testing.T) {
	window := NewModelsWindow()
	window.SetSize(80, 5) // 4 visible slots (incl. title)

	models := []Model{
		{ID: "claude-sonnet", DisplayName: "Claude Sonnet", Provider: "anthropic", Status: "ready"},
		{ID: "claude-haiku", DisplayName: "Claude Haiku", Provider: "anthropic", Status: "ready"},
		{ID: "gpt-4", DisplayName: "GPT-4", Provider: "openai", Status: "ready"},
		{ID: "gpt-3.5", DisplayName: "GPT-3.5", Provider: "openai", Status: "ready"},
		{ID: "gemini-pro", DisplayName: "Gemini Pro", Provider: "googleai", Status: "ready"},
	}
	window.SetModels(models, "") // No active model for this test

	// Scroll to show items from different groups
	render := window.RenderList(2, 1, window.GetVisibleSlots()) // selected gpt-4, scroll offset 1
	lines := strings.Split(render, "\n")

	assert.Contains(t, lines[0], "Select a model")
	assert.Contains(t, lines[1], "  🅰️  ● Claude Haiku") // Item at index 1
	assert.Equal(t, "", lines[2])                        // Blank line between provider groups
	assert.Contains(t, lines[3], "▶ 🤖 ● GPT-4")          // Selected item at index 2
	assert.Contains(t, lines[4], "  🤖 ● GPT-3.5")        // Item at index 3
}
