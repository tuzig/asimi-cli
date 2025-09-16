package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AnthropicModel represents a model from the Anthropic API
type AnthropicModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
	Type        string `json:"type"`
}

// AnthropicModelsResponse represents the response from /v1/models endpoint
type AnthropicModelsResponse struct {
	Data    []AnthropicModel `json:"data"`
	FirstID string          `json:"first_id,omitempty"`
	LastID  string          `json:"last_id,omitempty"`
	HasMore bool            `json:"has_more"`
}

// fetchAnthropicModels fetches available models from the Anthropic API
func fetchAnthropicModels(config *Config) ([]AnthropicModel, error) {
	if config.LLM.AuthToken == "" && config.LLM.APIKey == "" {
		return nil, fmt.Errorf("no authentication configured for anthropic provider")
	}

	// Create HTTP client with appropriate authentication
	client := &http.Client{}
	if config.LLM.AuthToken != "" {
		// Use OAuth authentication
		client.Transport = &anthropicOAuthTransport{
			token: config.LLM.AuthToken,
			base:  http.DefaultTransport,
		}
	} else {
		// Use API key authentication
		client.Transport = &anthropicAPIKeyTransport{
			base: http.DefaultTransport,
		}
	}

	// Determine base URL
	baseURL := "https://api.anthropic.com"
	if config.LLM.BaseURL != "" {
		baseURL = strings.TrimSuffix(config.LLM.BaseURL, "/")
	}
	if envBaseURL := os.Getenv("ANTHROPIC_BASE_URL"); envBaseURL != "" {
		baseURL = strings.TrimSuffix(envBaseURL, "/")
	}

	// Create request
	req, err := http.NewRequest("GET", baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("anthropic-version", "2023-06-01")
	if config.LLM.APIKey != "" {
		req.Header.Set("x-api-key", config.LLM.APIKey)
	}

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Parse response
	var modelsResponse AnthropicModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return modelsResponse.Data, nil
}

// ModelSelectionModal represents a modal for selecting AI models
type ModelSelectionModal struct {
	*BaseModal
	models        []AnthropicModel
	selected      int
	confirmed     bool
	selectedModel *AnthropicModel
	currentModel  string
	loading       bool
	error         string
}

// NewModelSelectionModal creates a new model selection modal
func NewModelSelectionModal(currentModel string) *ModelSelectionModal {
	baseModal := NewBaseModal("Select Model", "", 70, 15)

	return &ModelSelectionModal{
		BaseModal:     baseModal,
		models:        []AnthropicModel{},
		selected:      0,
		confirmed:     false,
		selectedModel: nil,
		currentModel:  currentModel,
		loading:       true,
		error:         "",
	}
}

// Render renders the model selection modal
func (m *ModelSelectionModal) Render() string {
	var content string

	if m.loading {
		content += "Loading models...\n\n"
		// Show spinner or loading indicator
		content += "⏳ Fetching available models from Anthropic API..."
	} else if m.error != "" {
		content += "Error loading models:\n\n"
		content += m.error + "\n\n"
		content += "Press Esc to cancel"
	} else {
		content += "Use ↑/↓ arrows to navigate, Enter to select, Esc to cancel\n\n"

		if len(m.models) == 0 {
			content += "No models available"
		} else {
			for i, model := range m.models {
				prefix := "  "
				suffix := ""

				// Mark current model
				if model.ID == m.currentModel {
					suffix = " (current)"
				}

				// Mark selected model
				if i == m.selected {
					prefix = "▶ "
				}

				style := lipgloss.NewStyle()
				if i == m.selected {
					style = style.Foreground(lipgloss.Color("62")).Bold(true)
				}

				displayText := model.DisplayName
				if displayText == "" {
					displayText = model.ID
				}

				line := fmt.Sprintf("%s%s%s", prefix, displayText, suffix)
				content += style.Render(line) + "\n"
			}
		}
	}

	// Update the base modal's content
	m.BaseModal.Content = content
	return m.BaseModal.Render()
}

// Update handles key events for model selection
func (m *ModelSelectionModal) Update(msg tea.Msg) (*ModelSelectionModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't handle keys while loading or if there's an error
		if m.loading || m.error != "" {
			switch msg.String() {
			case "esc", "q":
				return m, func() tea.Msg { return modalCancelledMsg{} }
			}
			return m, nil
		}

		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.models)-1 {
				m.selected++
			}
		case "enter":
			if len(m.models) > 0 {
				m.confirmed = true
				m.selectedModel = &m.models[m.selected]
				return m, func() tea.Msg { return modelSelectedMsg{model: m.selectedModel} }
			}
		case "esc", "q":
			return m, func() tea.Msg { return modalCancelledMsg{} }
		}
	}
	return m, nil
}

// SetModels updates the models list and stops loading
func (m *ModelSelectionModal) SetModels(models []AnthropicModel) {
	m.models = models
	m.loading = false
	m.error = ""

	// Try to set selected to current model
	for i, model := range models {
		if model.ID == m.currentModel {
			m.selected = i
			break
		}
	}
}

// SetError sets an error message and stops loading
func (m *ModelSelectionModal) SetError(err string) {
	m.error = err
	m.loading = false
}

// Message types for model selection
type modelSelectedMsg struct {
	model *AnthropicModel
}

// Message types for model loading
type modelsLoadedMsg struct {
	models []AnthropicModel
}

type modelsLoadErrorMsg struct {
	error string
}

type showModelSelectionMsg struct{}

// Command handler
func handleModelsCommand(model *TUIModel, args []string) tea.Cmd {
	// Only allow model selection for Anthropic provider
	if model.config.LLM.Provider != "anthropic" {
		model.toastManager.AddToast("Model selection is only available for Anthropic provider", "error", 3000)
		return nil
	}

	return func() tea.Msg {
		return showModelSelectionMsg{}
	}
}

// TUI command to fetch models
func (m *TUIModel) fetchModelsCommand() tea.Cmd {
	return func() tea.Msg {
		models, err := fetchAnthropicModels(m.config)
		if err != nil {
			return modelsLoadErrorMsg{error: err.Error()}
		}
		return modelsLoadedMsg{models: models}
	}
}