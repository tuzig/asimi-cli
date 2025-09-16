package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestFetchAnthropicModels tests the API client function
func TestFetchAnthropicModels(t *testing.T) {
	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}
		if r.URL.Path != "/v1/models" {
			t.Errorf("Expected /v1/models path, got %s", r.URL.Path)
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("Expected anthropic-version header to be 2023-06-01, got %s", r.Header.Get("anthropic-version"))
		}

		// Return mock response
		response := AnthropicModelsResponse{
			Data: []AnthropicModel{
				{
					ID:          "claude-3-5-sonnet-20241022",
					DisplayName: "Claude 3.5 Sonnet",
					CreatedAt:   "2024-10-22T00:00:00Z",
					Type:        "model",
				},
				{
					ID:          "claude-3-haiku-20240307",
					DisplayName: "Claude 3 Haiku",
					CreatedAt:   "2024-03-07T00:00:00Z",
					Type:        "model",
				},
			},
			HasMore: false,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create config with API key and mock server URL
	config := &Config{
		LLM: LLMConfig{
			APIKey:  "test-api-key",
			BaseURL: server.URL,
		},
	}

	// Test the function
	models, err := fetchAnthropicModels(config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("Expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ID != "claude-3-5-sonnet-20241022" {
		t.Errorf("Expected first model ID to be claude-3-5-sonnet-20241022, got %s", models[0].ID)
	}
	if models[0].DisplayName != "Claude 3.5 Sonnet" {
		t.Errorf("Expected first model DisplayName to be Claude 3.5 Sonnet, got %s", models[0].DisplayName)
	}

	// Check second model
	if models[1].ID != "claude-3-haiku-20240307" {
		t.Errorf("Expected second model ID to be claude-3-haiku-20240307, got %s", models[1].ID)
	}
}

// TestFetchAnthropicModelsNoAuth tests error handling when no auth is provided
func TestFetchAnthropicModelsNoAuth(t *testing.T) {
	config := &Config{
		LLM: LLMConfig{
			// No APIKey or AuthToken
		},
	}

	_, err := fetchAnthropicModels(config)
	if err == nil {
		t.Error("Expected error when no authentication is provided")
	}
	if err.Error() != "no authentication configured for anthropic provider" {
		t.Errorf("Expected specific error message, got %s", err.Error())
	}
}

// TestFetchAnthropicModelsServerError tests error handling for server errors
func TestFetchAnthropicModelsServerError(t *testing.T) {
	// Create a mock HTTP server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := &Config{
		LLM: LLMConfig{
			APIKey:  "test-api-key",
			BaseURL: server.URL,
		},
	}

	_, err := fetchAnthropicModels(config)
	if err == nil {
		t.Error("Expected error when server returns 500")
	}
}

// TestNewModelSelectionModal tests modal creation
func TestNewModelSelectionModal(t *testing.T) {
	currentModel := "claude-3-5-sonnet-20241022"
	modal := NewModelSelectionModal(currentModel)

	if modal == nil {
		t.Fatal("Expected modal to be created")
	}
	if modal.currentModel != currentModel {
		t.Errorf("Expected currentModel to be %s, got %s", currentModel, modal.currentModel)
	}
	if !modal.loading {
		t.Error("Expected modal to be in loading state initially")
	}
	if modal.selected != 0 {
		t.Errorf("Expected selected to be 0, got %d", modal.selected)
	}
	if len(modal.models) != 0 {
		t.Errorf("Expected models to be empty initially, got %d", len(modal.models))
	}
}

// TestModelSelectionModalSetModels tests setting models
func TestModelSelectionModalSetModels(t *testing.T) {
	modal := NewModelSelectionModal("claude-3-haiku-20240307")

	models := []AnthropicModel{
		{
			ID:          "claude-3-5-sonnet-20241022",
			DisplayName: "Claude 3.5 Sonnet",
		},
		{
			ID:          "claude-3-haiku-20240307",
			DisplayName: "Claude 3 Haiku",
		},
	}

	modal.SetModels(models)

	if modal.loading {
		t.Error("Expected loading to be false after setting models")
	}
	if len(modal.models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(modal.models))
	}
	// Should select the current model (index 1)
	if modal.selected != 1 {
		t.Errorf("Expected selected to be 1 (current model), got %d", modal.selected)
	}
}

// TestModelSelectionModalSetError tests setting error
func TestModelSelectionModalSetError(t *testing.T) {
	modal := NewModelSelectionModal("claude-3-5-sonnet-20241022")

	errorMsg := "Failed to fetch models"
	modal.SetError(errorMsg)

	if modal.loading {
		t.Error("Expected loading to be false after setting error")
	}
	if modal.error != errorMsg {
		t.Errorf("Expected error to be %s, got %s", errorMsg, modal.error)
	}
}

// TestModelSelectionModalKeyHandling tests keyboard input
func TestModelSelectionModalKeyHandling(t *testing.T) {
	modal := NewModelSelectionModal("claude-3-5-sonnet-20241022")

	models := []AnthropicModel{
		{ID: "claude-3-5-sonnet-20241022", DisplayName: "Claude 3.5 Sonnet"},
		{ID: "claude-3-haiku-20240307", DisplayName: "Claude 3 Haiku"},
		{ID: "claude-3-opus-20240229", DisplayName: "Claude 3 Opus"},
	}
	modal.SetModels(models)

	// Test down arrow
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	keyMsg.Type = tea.KeyDown
	updatedModal, cmd := modal.Update(keyMsg)
	if updatedModal.selected != 1 {
		t.Errorf("Expected selected to be 1 after down arrow, got %d", updatedModal.selected)
	}
	if cmd != nil {
		t.Error("Expected no command for navigation")
	}

	// Test up arrow
	keyMsg.Type = tea.KeyUp
	updatedModal, cmd = updatedModal.Update(keyMsg)
	if updatedModal.selected != 0 {
		t.Errorf("Expected selected to be 0 after up arrow, got %d", updatedModal.selected)
	}

	// Test enter key - should return selection
	keyMsg.Type = tea.KeyEnter
	updatedModal, cmd = updatedModal.Update(keyMsg)
	if cmd == nil {
		t.Error("Expected command when pressing enter")
	}
	if !updatedModal.confirmed {
		t.Error("Expected modal to be confirmed after enter")
	}

	// Test escape key - should return cancel
	keyMsg = tea.KeyMsg{Type: tea.KeyEsc}
	_, cmd = modal.Update(keyMsg)
	if cmd == nil {
		t.Error("Expected command when pressing escape")
	}
}

// TestModelSelectionModalRender tests rendering
func TestModelSelectionModalRender(t *testing.T) {
	modal := NewModelSelectionModal("claude-3-5-sonnet-20241022")

	// Test loading state
	output := modal.Render()
	if output == "" {
		t.Error("Expected non-empty output")
	}

	// Test with models
	models := []AnthropicModel{
		{ID: "claude-3-5-sonnet-20241022", DisplayName: "Claude 3.5 Sonnet"},
		{ID: "claude-3-haiku-20240307", DisplayName: "Claude 3 Haiku"},
	}
	modal.SetModels(models)

	output = modal.Render()
	if output == "" {
		t.Error("Expected non-empty output with models")
	}

	// Test with error
	modal.SetError("Test error")
	output = modal.Render()
	if output == "" {
		t.Error("Expected non-empty output with error")
	}
}

// TestHandleModelsCommand tests the command handler
func TestHandleModelsCommand(t *testing.T) {
	// Test with Anthropic provider
	config := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
		},
	}
	model := &TUIModel{
		config:       config,
		toastManager: NewToastManager(),
	}

	cmd := handleModelsCommand(model, []string{})
	if cmd == nil {
		t.Error("Expected command for Anthropic provider")
	}

	// Execute the command to get the message
	msg := cmd()
	if _, ok := msg.(showModelSelectionMsg); !ok {
		t.Error("Expected showModelSelectionMsg")
	}

	// Test with non-Anthropic provider
	config.LLM.Provider = "openai"
	cmd = handleModelsCommand(model, []string{})
	if cmd != nil {
		t.Error("Expected no command for non-Anthropic provider")
	}
}

// TestMessageTypes tests the message type structs
func TestMessageTypes(t *testing.T) {
	// Test modelSelectedMsg
	model := &AnthropicModel{
		ID:          "claude-3-5-sonnet-20241022",
		DisplayName: "Claude 3.5 Sonnet",
	}
	msg := modelSelectedMsg{model: model}
	if msg.model.ID != "claude-3-5-sonnet-20241022" {
		t.Errorf("Expected model ID to be claude-3-5-sonnet-20241022, got %s", msg.model.ID)
	}

	// Test modelsLoadedMsg
	models := []AnthropicModel{{ID: "test"}}
	loadedMsg := modelsLoadedMsg{models: models}
	if len(loadedMsg.models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(loadedMsg.models))
	}

	// Test modelsLoadErrorMsg
	errorMsg := modelsLoadErrorMsg{error: "test error"}
	if errorMsg.error != "test error" {
		t.Errorf("Expected error to be 'test error', got %s", errorMsg.error)
	}

	// Test showModelSelectionMsg
	showMsg := showModelSelectionMsg{}
	_ = showMsg // Just test that it compiles
}
