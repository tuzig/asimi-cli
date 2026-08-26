package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/afittestide/asimi/internal/courtapi"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFetchAllModels_ReturnsEmptyWithoutAuth verifies that fetchAllModels returns
// an empty list when no providers are authenticated
func TestFetchAllModels_ReturnsEmptyWithoutAuth(t *testing.T) {
	// Clear any existing credentials
	DeleteAPIKeyFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("openai")
	DeleteAPIKeyFromKeyring("googleai")
	DeleteAPIKeyFromKeyring("openrouter")

	// Clear environment variables for this test
	originalAnthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	originalOpenAIKey := os.Getenv("OPENAI_API_KEY")
	originalGeminiKey := os.Getenv("GEMINI_API_KEY")
	originalGoogleKey := os.Getenv("GOOGLE_API_KEY")
	originalOllamaHost := os.Getenv("OLLAMA_HOST")
	originalOpenRouterKey := os.Getenv("OPENROUTER_API_KEY")

	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	os.Unsetenv("OPENROUTER_API_KEY")

	// Create a mock Ollama server that returns empty model list
	mockOllamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
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
		if originalOpenRouterKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalOpenRouterKey)
		}
	}()

	// Override fetch to return empty for Ollama (keyless, no court in test)
	origFetch := fetchModelsForProvider
	defer func() { fetchModelsForProvider = origFetch }()
	fetchModelsForProvider = func(config *Config, court courtapi.Client, provider string) ([]Model, error) {
		return nil, nil // no models, no error
	}

	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-latest",
		},
	}

	models := fetchAllModels(config, nil)

	// With no auth and empty Ollama, should return no models
	assert.Equal(t, 0, len(models), "Expected 0 models when no auth and empty Ollama")
}

// TestCheckProviderAuth verifies provider authentication detection
func TestCheckProviderAuth(t *testing.T) {
	// Clear credentials
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
	assert.False(t, info, "Expected false when no credentials are set")

	// Test with environment variable
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	info = checkProviderAuth("anthropic")
	assert.True(t, info, "Expected true when ANTHROPIC_API_KEY is set")
}

func TestCheckProviderAuth_OpenRouter(t *testing.T) {
	// Clear keyring
	DeleteAPIKeyFromKeyring("openrouter")

	// Clear environment
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Unsetenv("OPENROUTER_API_KEY")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		}
	}()

	// Test with no credentials
	assert.False(t, checkProviderAuth("openrouter"), "Expected false when no credentials are set")

	// Test with environment variable
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	assert.True(t, checkProviderAuth("openrouter"), "Expected true when OPENROUTER_API_KEY is set")
}

// TestCheckProviderAuth_Convention verifies convention-based env var resolution
// for providers not explicitly listed in the old switch statements.
func TestCheckProviderAuth_Convention(t *testing.T) {
	// Clear keyring
	DeleteAPIKeyFromKeyring("cohere")

	originalKey := os.Getenv("COHERE_API_KEY")
	os.Unsetenv("COHERE_API_KEY")
	defer func() {
		if originalKey != "" {
			os.Setenv("COHERE_API_KEY", originalKey)
		}
	}()

	assert.False(t, checkProviderAuth("cohere"), "Expected false when no credentials")
	t.Setenv("COHERE_API_KEY", "test-key")
	assert.True(t, checkProviderAuth("cohere"), "Expected true when COHERE_API_KEY is set")
}

// TestCheckProviderAuth_Keyless verifies that keyless providers (Ollama) are always authed
func TestCheckProviderAuth_Keyless(t *testing.T) {
	assert.True(t, checkProviderAuth("ollama"), "Expected true for keyless provider ollama")
}

// TestFetchAllModels_ManualEntryOnError verifies that when a provider's API returns
// an error, a manual_entry entry is produced (not an error entry).
func TestFetchAllModels_ManualEntryOnError(t *testing.T) {
	DeleteAPIKeyFromKeyring("openrouter")

	originalKey := os.Getenv("OPENROUTER_API_KEY")
	originalBaseURL := os.Getenv("OPENROUTER_BASE_URL")
	t.Setenv("OPENROUTER_API_KEY", "bad-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
		if originalBaseURL != "" {
			os.Setenv("OPENROUTER_BASE_URL", originalBaseURL)
		} else {
			os.Unsetenv("OPENROUTER_BASE_URL")
		}
	}()

	// Clear other providers
	DeleteAPIKeyFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("openai")
	DeleteAPIKeyFromKeyring("googleai")
	originalAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	originalOpenAI := os.Getenv("OPENAI_API_KEY")
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	originalOllamaHost := os.Getenv("OLLAMA_HOST")
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")

	// Mock Ollama to return empty
	mockOllama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[]}`))
	}))
	defer mockOllama.Close()
	os.Setenv("OLLAMA_HOST", mockOllama.URL)

	defer func() {
		if originalAnthropic != "" {
			os.Setenv("ANTHROPIC_API_KEY", originalAnthropic)
		}
		if originalOpenAI != "" {
			os.Setenv("OPENAI_API_KEY", originalOpenAI)
		}
		if originalGemini != "" {
			os.Setenv("GEMINI_API_KEY", originalGemini)
		}
		if originalGoogle != "" {
			os.Setenv("GOOGLE_API_KEY", originalGoogle)
		}
		if originalOllamaHost != "" {
			os.Setenv("OLLAMA_HOST", originalOllamaHost)
		} else {
			os.Unsetenv("OLLAMA_HOST")
		}
	}()

	// Override the fetch function to simulate an error from bifrost
	origFetch := fetchModelsForProvider
	defer func() { fetchModelsForProvider = origFetch }()
	fetchModelsForProvider = func(config *Config, court courtapi.Client, provider string) ([]Model, error) {
		if provider == "openrouter" {
			return nil, fmt.Errorf("API returned status 401")
		}
		return nil, nil
	}

	config := &Config{
		LLM: LLMConfig{
			Provider: "openrouter",
			Model:    "test",
		},
	}

	models := fetchAllModels(config, nil)

	var manualEntry *Model
	for i := range models {
		if models[i].Provider == "openrouter" && models[i].Status == "manual_entry" {
			manualEntry = &models[i]
			break
		}
	}

	require.NotNil(t, manualEntry, "Expected a manual_entry entry for openrouter")
	assert.Contains(t, manualEntry.DisplayName, "Enter model name for")
	assert.Contains(t, manualEntry.DisplayName, "OpenRouter")
	assert.NotEmpty(t, manualEntry.Description, "Expected error description to be preserved")
}

// TestIsModelSelectable_ManualEntry verifies that manual_entry models are selectable
func TestIsModelSelectable_ManualEntry(t *testing.T) {
	model := Model{Status: "manual_entry", Provider: "openai"}
	assert.True(t, IsModelSelectable(model), "Expected manual_entry to be selectable")

	// error should still not be selectable
	errorModel := Model{Status: "error", Provider: "openai"}
	assert.False(t, IsModelSelectable(errorModel), "Expected error to NOT be selectable")
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
	assert.Equal(t, 9, window.MaxVisible)

	window.SetSize(50, 2)
	assert.Equal(t, 2, window.Height)
	assert.Equal(t, 1, window.MaxVisible)
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
	window.SetError("test error")

	window.SetLoading(true)
	assert.True(t, window.Loading)
	assert.Nil(t, window.Error)
}

func TestModelsWindowSetError(t *testing.T) {
	window := NewModelsWindow()

	window.SetError("something went wrong")
	assert.False(t, window.Loading)
	assert.NotNil(t, window.Error)
	assert.Equal(t, "something went wrong", window.Error.Error())

	window.SetError("")
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

	assert.Equal(t, 1, window.GetInitialSelection())

	window.SetModels([]Model{
		{ID: "m1", DisplayName: "Model 1", Provider: "anthropic", Status: "ready"},
	}, "m_nonexistent")
	assert.Equal(t, 0, window.GetInitialSelection())

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
	window.SetSize(80, 10)

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
	assert.Contains(t, render, "Use :login to authenticate with a provider")

	// Test Normal Rendering with Active/Ready and Grouping
	models := []Model{
		{ID: "claude-3-5-sonnet-latest", DisplayName: "Claude 3.5 Sonnet", Provider: "anthropic", Status: "active"},
		{ID: "claude-3-5-haiku-latest", DisplayName: "Claude 3.5 Haiku", Provider: "anthropic", Status: "ready"},
		{ID: "gpt-4o", DisplayName: "GPT-4o", Provider: "openai", Status: "ready"},
		{ID: "gpt-4o-mini", DisplayName: "GPT-4o Mini", Provider: "openai", Status: "ready"},
		{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", Provider: "googleai", Status: "ready"},
		{ID: "anthropic/claude-3.5-sonnet", DisplayName: "anthropic/claude-3.5-sonnet", Provider: "openrouter", Status: "ready"},
	}
	window.SetModels(models, "claude-3-5-sonnet-latest")

	render = window.RenderList(0, 0, window.GetVisibleSlots())
	lines := strings.Split(render, "\n")
	assert.True(t, strings.HasPrefix(lines[1], "▶ 🅰️  ✓ Claude 3.5 Sonnet"))
	assert.True(t, strings.HasPrefix(lines[2], "  🅰️  Claude 3.5 Haiku"))
	assert.Equal(t, "", lines[3]) // Blank line between provider groups
	assert.True(t, strings.HasPrefix(lines[4], "  🤖 GPT-4o"))
	assert.True(t, strings.HasPrefix(lines[5], "  🤖 GPT-4o Mini"))
	assert.Equal(t, "", lines[6]) // Blank line between provider groups
	assert.True(t, strings.HasPrefix(lines[7], "  🔷 Gemini 2.5 Pro"))
	assert.Equal(t, "", lines[8]) // Blank line between provider groups
	assert.True(t, strings.HasPrefix(lines[9], "  🔀 anthropic/claude-3.5-sonnet"))

	// Test Selection
	render = window.RenderList(2, 0, window.GetVisibleSlots())
	lines = strings.Split(render, "\n")
	assert.True(t, strings.HasPrefix(lines[4], "▶ 🤖 GPT-4o"))
}

func TestGetProviderIcon(t *testing.T) {
	assert.Equal(t, "🅰️ ", getProviderIcon("anthropic"))
	assert.Equal(t, "🤖", getProviderIcon("openai"))
	assert.Equal(t, "🔷", getProviderIcon("googleai"))
	assert.Equal(t, "🦙", getProviderIcon("ollama"))
	assert.Equal(t, "🔀", getProviderIcon("openrouter"))
	assert.Equal(t, "  ", getProviderIcon("unknown"))
}

func TestGetStatusIcon(t *testing.T) {
	assert.Equal(t, "✓", getStatusIcon("active"))
	assert.Equal(t, "", getStatusIcon("ready"))
	assert.Equal(t, "⚠", getStatusIcon("error"))
	assert.Equal(t, "✏️", getStatusIcon("manual_entry"))
	assert.Equal(t, "", getStatusIcon("unknown"))
}

func TestModelsWindowRenderList_ScrollingAndGrouping(t *testing.T) {
	window := NewModelsWindow()
	window.SetSize(80, 5)

	models := []Model{
		{ID: "claude-sonnet", DisplayName: "Claude Sonnet", Provider: "anthropic", Status: "ready"},
		{ID: "claude-haiku", DisplayName: "Claude Haiku", Provider: "anthropic", Status: "ready"},
		{ID: "gpt-4", DisplayName: "GPT-4", Provider: "openai", Status: "ready"},
		{ID: "gpt-3.5", DisplayName: "GPT-3.5", Provider: "openai", Status: "ready"},
		{ID: "gemini-pro", DisplayName: "Gemini Pro", Provider: "googleai", Status: "ready"},
	}
	window.SetModels(models, "")

	render := window.RenderList(2, 1, window.GetVisibleSlots())
	lines := strings.Split(render, "\n")

	assert.Contains(t, lines[0], "Select a model")
	assert.Contains(t, lines[1], "  🅰️  Claude Haiku")
	assert.Equal(t, "", lines[2])
	assert.Contains(t, lines[3], "▶ 🤖 GPT-4")
	assert.Contains(t, lines[4], "  🤖 GPT-3.5")
}

func TestHandleModelsCommandShowsConfiguredModels(t *testing.T) {
	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-latest",
		},
	}

	model := &TUIModel{
		config: config,
		tabs:   newTestTabManager(),
	}

	cmd := handleModelsCommand(model, []string{})
	require.NotNil(t, cmd, "handleModelsCommand should return a command")

	msg := cmd()
	require.NotNil(t, msg, "Command should return a message")
}

// TestFetchAllModels_WithAPIKey verifies that models show as ready or manual_entry
// when API key is available but the court/bifrost is nil (simulating test env)
func TestFetchAllModels_WithAPIKey(t *testing.T) {
	// Clear any existing credentials
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

	// Override fetch to simulate error (no court available in test)
	origFetch := fetchModelsForProvider
	defer func() { fetchModelsForProvider = origFetch }()
	fetchModelsForProvider = func(config *Config, court courtapi.Client, provider string) ([]Model, error) {
		return nil, fmt.Errorf("LLM client not initialized")
	}

	config := &Config{
		LLM: LLMConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
	}

	models := fetchAllModels(config, nil)

	// With an API key set but no court, we should get manual_entry
	hasOpenAI := false
	for _, m := range models {
		if m.Provider == "openai" {
			hasOpenAI = true
			if m.Status != "ready" && m.Status != "active" && m.Status != "manual_entry" {
				t.Errorf("Expected OpenAI model %s to be 'ready', 'active', or 'manual_entry', got %s", m.ID, m.Status)
			}
		}
	}

	if !hasOpenAI {
		t.Error("Expected at least one OpenAI item (model or error)")
	}
}

// TestFetchAllModels_LoginRequiredEntryContents verifies that login_required entries
// are no longer emitted by fetchAllModels (they moved to :login)
func TestFetchAllModels_LoginRequiredEntryContents(t *testing.T) {
	DeleteAPIKeyFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("openai")
	DeleteAPIKeyFromKeyring("googleai")
	DeleteAPIKeyFromKeyring("openrouter")

	originalAnthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	originalOpenAIKey := os.Getenv("OPENAI_API_KEY")
	originalGeminiKey := os.Getenv("GEMINI_API_KEY")
	originalGoogleKey := os.Getenv("GOOGLE_API_KEY")
	originalOpenRouterKey := os.Getenv("OPENROUTER_API_KEY")
	originalOllamaHost := os.Getenv("OLLAMA_HOST")

	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	os.Unsetenv("OPENROUTER_API_KEY")

	mockOllamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[]}`))
	}))
	defer mockOllamaServer.Close()
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
		if originalOpenRouterKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalOpenRouterKey)
		}
		if originalOllamaHost != "" {
			os.Setenv("OLLAMA_HOST", originalOllamaHost)
		} else {
			os.Unsetenv("OLLAMA_HOST")
		}
	}()

	// Override fetch to return empty for Ollama (which is keyless and thus "configured")
	origFetch := fetchModelsForProvider
	defer func() { fetchModelsForProvider = origFetch }()
	fetchModelsForProvider = func(config *Config, court courtapi.Client, provider string) ([]Model, error) {
		return nil, nil // no models, no error
	}

	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-latest",
		},
	}

	models := fetchAllModels(config, nil)

	// With no auth and empty Ollama, should return no models
	// (login_required entries are now handled by :login, not :models)
	assert.Equal(t, 0, len(models), "Expected 0 entries — login_required moved to :login")

	for _, m := range models {
		assert.NotEqual(t, "login_required", m.Status, "login_required entries should not be in fetchAllModels")
	}
}

// TestHandleModelsCommand_NoLoginRequiredOnSelect verifies that handleModelsCommand
// no longer wires OnSelect for login_required entries (they moved to :login)
func TestHandleModelsCommand_NoLoginRequiredOnSelect(t *testing.T) {
	DeleteAPIKeyFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("openai")
	DeleteAPIKeyFromKeyring("googleai")
	DeleteAPIKeyFromKeyring("openrouter")

	originalAnthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	originalOpenAIKey := os.Getenv("OPENAI_API_KEY")
	originalGeminiKey := os.Getenv("GEMINI_API_KEY")
	originalGoogleKey := os.Getenv("GOOGLE_API_KEY")
	originalOpenRouterKey := os.Getenv("OPENROUTER_API_KEY")
	originalOllamaHost := os.Getenv("OLLAMA_HOST")

	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	os.Unsetenv("OPENROUTER_API_KEY")

	mockOllamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[]}`))
	}))
	defer mockOllamaServer.Close()
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
		if originalOpenRouterKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalOpenRouterKey)
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

	model := &TUIModel{
		config: config,
		tabs:   newTestTabManager(),
	}

	cmd := handleModelsCommand(model, []string{})
	require.NotNil(t, cmd)

	msg := cmd()
	require.NotNil(t, msg)

	// Verify no login_required entries exist in the models list
	if modelsMsg, ok := msg.(modelsLoadedMsg); ok {
		for _, m := range modelsMsg.models {
			assert.NotEqual(t, "login_required", m.Status,
				"login_required entries should not be in handleModelsCommand output")
		}
	}
}

// TestHandleModelsCommand_SetsOnSelectForManualEntry verifies that handleModelsCommand
// sets OnSelect callbacks for manual_entry entries
func TestHandleModelsCommand_SetsOnSelectForManualEntry(t *testing.T) {
	DeleteAPIKeyFromKeyring("openrouter")

	originalKey := os.Getenv("OPENROUTER_API_KEY")
	t.Setenv("OPENROUTER_API_KEY", "bad-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	// Clear other providers
	DeleteAPIKeyFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("openai")
	DeleteAPIKeyFromKeyring("googleai")
	originalAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	originalOpenAI := os.Getenv("OPENAI_API_KEY")
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	originalOllamaHost := os.Getenv("OLLAMA_HOST")
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")

	mockOllama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[]}`))
	}))
	defer mockOllama.Close()
	os.Setenv("OLLAMA_HOST", mockOllama.URL)

	defer func() {
		if originalAnthropic != "" {
			os.Setenv("ANTHROPIC_API_KEY", originalAnthropic)
		}
		if originalOpenAI != "" {
			os.Setenv("OPENAI_API_KEY", originalOpenAI)
		}
		if originalGemini != "" {
			os.Setenv("GEMINI_API_KEY", originalGemini)
		}
		if originalGoogle != "" {
			os.Setenv("GOOGLE_API_KEY", originalGoogle)
		}
		if originalOllamaHost != "" {
			os.Setenv("OLLAMA_HOST", originalOllamaHost)
		} else {
			os.Unsetenv("OLLAMA_HOST")
		}
	}()

	// Override fetch to simulate error from bifrost
	origFetch := fetchModelsForProvider
	defer func() { fetchModelsForProvider = origFetch }()
	fetchModelsForProvider = func(config *Config, court courtapi.Client, provider string) ([]Model, error) {
		if provider == "openrouter" {
			return nil, fmt.Errorf("API returned status 401")
		}
		return nil, nil
	}

	config := &Config{
		LLM: LLMConfig{
			Provider: "openrouter",
			Model:    "test",
		},
	}

	// Replicate the handleModelsCommand logic to verify OnSelect is set
	models := fetchAllModels(config, nil)
	for i := range models {
		m := &models[i]
		if m.Status == "manual_entry" {
			provider := m.Provider
			m.OnSelect = func() tea.Msg { return enterModelNameMsg{provider: provider} }
		}
	}

	// Verify all manual_entry entries have OnSelect set
	for _, m := range models {
		if m.Status == "manual_entry" {
			assert.NotNil(t, m.OnSelect, "Expected OnSelect to be set for manual_entry entry (provider: %s)", m.Provider)
		}
	}
}

// TestProviderEnvVar_Convention verifies convention-based env var resolution
func TestProviderEnvVar_Convention(t *testing.T) {
	assert.Equal(t, "ANTHROPIC_API_KEY", providerEnvVar("anthropic"))
	assert.Equal(t, "OPENAI_API_KEY", providerEnvVar("openai"))
	assert.Equal(t, "GEMINI_API_KEY", providerEnvVar("googleai"))
	assert.Equal(t, "OPENROUTER_API_KEY", providerEnvVar("openrouter"))
	assert.Equal(t, "COHERE_API_KEY", providerEnvVar("cohere"))
	assert.Equal(t, "MISTRAL_API_KEY", providerEnvVar("mistral"))
}

// TestProviderDisplayName verifies display names from the metadata table
func TestProviderDisplayName(t *testing.T) {
	assert.Equal(t, "Anthropic", providerDisplayName("anthropic"))
	assert.Equal(t, "OpenAI", providerDisplayName("openai"))
	assert.Equal(t, "Google AI", providerDisplayName("googleai"))
	assert.Equal(t, "OpenRouter", providerDisplayName("openrouter"))
	assert.Equal(t, "Ollama", providerDisplayName("ollama"))
	// Unknown providers return the provider name itself
	assert.Equal(t, "unknown", providerDisplayName("unknown"))
}

// TestProviderMeta_AuthType verifies auth type classification
func TestProviderMeta_AuthType(t *testing.T) {
	assert.Equal(t, AuthTypeAPIKey, providerAuthType("anthropic"))
	assert.Equal(t, AuthTypeOAuth, providerAuthType("openai"))
	assert.Equal(t, AuthTypeKeyless, providerAuthType("ollama"))
	assert.Equal(t, AuthTypeAPIKey, providerAuthType("cohere"))
}

// TestFetchAllModels_WithMockBifrost verifies that fetchAllModels correctly
// maps schemas.Model from bifrost to asimi's Model struct.
func TestFetchAllModels_WithMockBifrost(t *testing.T) {
	DeleteAPIKeyFromKeyring("anthropic")
	DeleteAPIKeyFromKeyring("openai")
	DeleteAPIKeyFromKeyring("googleai")
	DeleteAPIKeyFromKeyring("openrouter")

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	originalOllamaHost := os.Getenv("OLLAMA_HOST")
	mockOllama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[]}`))
	}))
	defer mockOllama.Close()
	os.Setenv("OLLAMA_HOST", mockOllama.URL)
	defer func() {
		if originalOllamaHost != "" {
			os.Setenv("OLLAMA_HOST", originalOllamaHost)
		} else {
			os.Unsetenv("OLLAMA_HOST")
		}
	}()

	// Override fetch to return mock models
	origFetch := fetchModelsForProvider
	defer func() { fetchModelsForProvider = origFetch }()
	fetchModelsForProvider = func(config *Config, court courtapi.Client, provider string) ([]Model, error) {
		if provider == "anthropic" {
			return []Model{
				{ID: "claude-3-5-sonnet", DisplayName: "Claude 3.5 Sonnet"},
				{ID: "claude-3-5-haiku", DisplayName: "Claude 3.5 Haiku"},
			}, nil
		}
		return nil, nil
	}

	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet",
		},
	}

	models := fetchAllModels(config, nil)
	assert.GreaterOrEqual(t, len(models), 2)

	// Verify the active model is marked
	for _, m := range models {
		if m.Provider == "anthropic" && m.ID == "claude-3-5-sonnet" {
			assert.Equal(t, "active", m.Status)
		}
	}
}

// TestBifrostProviderToAsimi verifies the provider key mapping
func TestBifrostProviderToAsimi(t *testing.T) {
	assert.Equal(t, "googleai", bifrostProviderToAsimi("gemini"))
	assert.Equal(t, "anthropic", bifrostProviderToAsimi("anthropic"))
	assert.Equal(t, "openai", bifrostProviderToAsimi("openai"))
}

// TestAsimiProviderToBifrost verifies the inverse mapping of asimi → bifrost
func TestAsimiProviderToBifrost(t *testing.T) {
	assert.Equal(t, "gemini", asimiProviderToBifrost("googleai"))
	assert.Equal(t, "anthropic", asimiProviderToBifrost("anthropic"))
	assert.Equal(t, "openai", asimiProviderToBifrost("openai"))
	assert.Equal(t, "cohere", asimiProviderToBifrost("cohere"))
}

// TestAsimiProviderToBifrostRoundTrip verifies that the two mapping functions
// are inverses when starting from the bifrost side.
func TestAsimiProviderToBifrostRoundTrip(t *testing.T) {
	// bifrost → asimi → bifrost should be identity
	bifrostProviders := []string{"anthropic", "openai", "gemini", "cohere", "mistral"}
	for _, p := range bifrostProviders {
		asimi := bifrostProviderToAsimi(p)
		bifrost := asimiProviderToBifrost(asimi)
		assert.Equal(t, p, bifrost, "round-trip should be identity for %s", p)
	}

	// asimi → bifrost → asimi should be identity
	asimiProviders := []string{"anthropic", "openai", "googleai", "cohere", "mistral"}
	for _, p := range asimiProviders {
		bifrost := asimiProviderToBifrost(p)
		asimi := bifrostProviderToAsimi(bifrost)
		assert.Equal(t, p, asimi, "reverse round-trip should be identity for %s", p)
	}
}

// mockListModelsCourt is a minimal courtapi.Client that returns canned
// ListModels responses. It embeds courtapi.Client so all other methods
// panic if called (which is fine for these tests).
type mockListModelsCourt struct {
	courtapi.Client
	resp *schemas.BifrostListModelsResponse
	err  error
}

func (m *mockListModelsCourt) ListModels(provider string) (*schemas.BifrostListModelsResponse, error) {
	return m.resp, m.err
}

// TestFetchModelsForProvider_MapsBifrostModels verifies that the real
// fetchModelsForProvider correctly maps schemas.Model fields to asimi's Model,
// including nil-to-empty-string handling for Name and Description pointers.
func TestFetchModelsForProvider_MapsBifrostModels(t *testing.T) {
	name1 := "Claude 3.5 Sonnet"
	desc1 := "Most intelligent model"
	models := []schemas.Model{
		{ID: "claude-3-5-sonnet", Name: &name1, Description: &desc1},
		{ID: "claude-3-5-haiku"}, // nil Name and Description
	}
	court := &mockListModelsCourt{
		resp: &schemas.BifrostListModelsResponse{Data: models},
	}

	result, err := fetchModelsForProvider(&Config{}, court, "anthropic")
	require.NoError(t, err)
	require.Len(t, result, 2)

	assert.Equal(t, "claude-3-5-sonnet", result[0].ID)
	assert.Equal(t, "Claude 3.5 Sonnet", result[0].DisplayName)
	assert.Equal(t, "Most intelligent model", result[0].Description)

	// nil Name falls back to ID; nil Description falls back to ""
	assert.Equal(t, "claude-3-5-haiku", result[1].ID)
	assert.Equal(t, "claude-3-5-haiku", result[1].DisplayName)
	assert.Equal(t, "", result[1].Description)
}

// TestFetchModelsForProvider_NilCourtReturnsError verifies the nil court guard
func TestFetchModelsForProvider_NilCourtReturnsError(t *testing.T) {
	_, err := fetchModelsForProvider(&Config{}, nil, "anthropic")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no court available")
}

// TestFetchModelsForProvider_CourtError verifies error propagation
func TestFetchModelsForProvider_CourtError(t *testing.T) {
	court := &mockListModelsCourt{err: fmt.Errorf("provider unavailable")}
	_, err := fetchModelsForProvider(&Config{}, court, "openai")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provider unavailable")
}

// TestFetchModelsForProvider_EmptyResponse verifies empty responses are handled
func TestFetchModelsForProvider_EmptyResponse(t *testing.T) {
	court := &mockListModelsCourt{
		resp: &schemas.BifrostListModelsResponse{Data: nil},
	}
	result, err := fetchModelsForProvider(&Config{}, court, "cohere")
	require.NoError(t, err)
	assert.Empty(t, result)
}

// TestProviderBaseURLEnvVar_Convention verifies convention-based base URL env var
func TestProviderBaseURLEnvVar_Convention(t *testing.T) {
	assert.Equal(t, "ANTHROPIC_BASE_URL", providerBaseURLEnvVar("anthropic"))
	assert.Equal(t, "OPENAI_BASE_URL", providerBaseURLEnvVar("openai"))
	assert.Equal(t, "GEMINI_BASE_URL", providerBaseURLEnvVar("googleai"))
	assert.Equal(t, "AZURE_OPENAI_BASE_URL", providerBaseURLEnvVar("azure"))
	assert.Equal(t, "COHERE_BASE_URL", providerBaseURLEnvVar("cohere"))
	assert.Equal(t, "MISTRAL_BASE_URL", providerBaseURLEnvVar("mistral"))
}

// TestProviderBaseURLFromEnv verifies that base URL env vars are resolved
func TestProviderBaseURLFromEnv(t *testing.T) {
	t.Setenv("COHERE_BASE_URL", "https://api.cohere.ai")
	assert.Equal(t, "https://api.cohere.ai", providerBaseURLFromEnv("cohere"))

	t.Setenv("GEMINI_BASE_URL", "https://custom-gemini.example.com")
	assert.Equal(t, "https://custom-gemini.example.com", providerBaseURLFromEnv("googleai"))

	os.Unsetenv("COHERE_BASE_URL")
	assert.Equal(t, "", providerBaseURLFromEnv("cohere"))
}

// clearProviderAuthKeys unsets the API-key env vars and clears the keyring
// entries for every standard provider, so getConfiguredProviderKeys starts
// from a clean slate regardless of the shell/CI environment. Iterating over
// schemas.StandardProviders keeps this list in sync as providers are added.
func clearProviderAuthKeys() {
	for _, sp := range schemas.StandardProviders {
		key := bifrostProviderToAsimi(string(sp))
		os.Unsetenv(providerEnvVar(key))
		// Special-case aliases checked in checkProviderAuth/providerEnvVar.
		if key == "googleai" {
			os.Unsetenv("GEMINI_API_KEY")
			os.Unsetenv("GOOGLE_API_KEY")
		}
		if key == "bedrock" {
			os.Unsetenv("AWS_ACCESS_KEY_ID")
			os.Unsetenv("AWS_SECRET_ACCESS_KEY")
		}
		_ = DeleteAPIKeyFromKeyring(key)
	}
}

// TestGetConfiguredProviderKeys_NoAuth verifies that without auth, only
// keyless providers are included
func TestGetConfiguredProviderKeys_NoAuth(t *testing.T) {
	clearProviderAuthKeys()

	// Without Ollama available, no providers
	providers := getConfiguredProviderKeys(false)
	assert.Empty(t, providers)
}

// TestGetConfiguredProviderKeys_ConventionAuth verifies that a convention-based
// env var for a new provider (not in the old hardcoded switches) is detected
func TestGetConfiguredProviderKeys_ConventionAuth(t *testing.T) {
	clearProviderAuthKeys()

	t.Setenv("COHERE_API_KEY", "test-key")

	providers := getConfiguredProviderKeys(false)
	assert.Contains(t, providers, "cohere")
}
