package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model represents a unified model across all providers
type Model struct {
	ID          string // Model identifier (e.g., "claude-3-5-sonnet-latest", "gpt-4o")
	DisplayName string // Human-readable name
	Provider    string // Provider key (e.g., "anthropic", "openai", "googleai")
	Description string // Optional description
	Status      string // "active" (currently selected), "ready" (key found), "login_required", "login"
	// TODO: if not nil, it should be called on selecting the model
	OnSelect tea.Cmd
}

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
	FirstID string           `json:"first_id,omitempty"`
	LastID  string           `json:"last_id,omitempty"`
	HasMore bool             `json:"has_more"`
}

// OpenAIModel represents a model from the OpenAI API
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OpenAIModelsResponse represents the response from OpenAI /v1/models endpoint
type OpenAIModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// GoogleModel represents a model from the Google AI API
type GoogleModel struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	Description                string   `json:"description"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

// GoogleModelsResponse represents the response from Google AI models endpoint
type GoogleModelsResponse struct {
	Models        []GoogleModel `json:"models"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

// OllamaModel represents a model from the Ollama API
type OllamaModel struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	ModifiedAt string `json:"modified_at"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest"`
}

// OllamaModelsResponse represents the response from Ollama /api/tags endpoint
type OllamaModelsResponse struct {
	Models []OllamaModel `json:"models"`
}

// ProviderInfo holds information about a provider's authentication status
type ProviderInfo struct {
	Name      string
	HasAPIKey bool
	HasOAuth  bool
}

// checkProviderAuth checks if a provider has valid authentication
func checkProviderAuth(provider string) ProviderInfo {
	info := ProviderInfo{Name: provider}

	// Check for API key in environment
	switch provider {
	case "anthropic":
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			info.HasAPIKey = true
		}
	case "openai":
		if os.Getenv("OPENAI_API_KEY") != "" {
			info.HasAPIKey = true
		}
	case "googleai":
		if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
			info.HasAPIKey = true
		}
	}

	// Check for API key in keyring
	if !info.HasAPIKey {
		apiKey, err := GetAPIKeyFromKeyring(provider)
		if err == nil && apiKey != "" {
			info.HasAPIKey = true
		}
	}

	// Check for OAuth token
	tokenData, err := GetOauthToken(provider)
	if err == nil && tokenData != nil && !shogunate.IsTokenExpired(tokenData) {
		info.HasOAuth = true
	}

	return info
}

// fetchAllModels aggregates models from all providers
func fetchAllModels(config *Config) []Model {
	var allModels []Model
	currentProvider := config.LLM.Provider
	currentModel := config.LLM.Model

	// Check auth status for each provider
	anthropicAuth := checkProviderAuth("anthropic")
	ollamaAvailable := checkOllamaAvailable()

	// Fetch Anthropic models & login option
	addLogin := false
	if anthropicAuth.HasAPIKey || anthropicAuth.HasOAuth {
		anthropicModels, err := fetchAnthropicModels(config)
		if err == nil && len(anthropicModels) > 0 {
			for _, m := range anthropicModels {
				status := "ready"
				if currentProvider == "anthropic" && m.ID == currentModel {
					status = "active"
				}
				displayName := m.DisplayName
				if displayName == "" {
					displayName = m.ID
				}
				allModels = append(allModels, Model{
					ID:          m.ID,
					DisplayName: displayName,
					Provider:    "anthropic",
					Status:      status,
				})
			}
		} else if err != nil {
			// TODO:
			slog.Warn("failed to fetch Anthropic models", "error", err)
			allModels = append(allModels, Model{
				Provider:    "anthropic",
				Status:      "error",
				Description: err.Error(),
			})
			addLogin = true
		}
	} else {
		addLogin = true
	}
	if addLogin {
		// Add login option for Anthropic
		slog.Debug("Addign login option")
		allModels = append(allModels, Model{
			ID:          "login",
			DisplayName: "Login to Anthropic",
			Provider:    "anthropic",
			Description: "Use Claude Pro/Max account",
			Status:      "login",
			OnSelect: func() tea.Msg {
				slog.Debug("Login selected line")
				return providerSelectedMsg{provider: &Provider{
					Name:        "Anthropic (Claude)",
					Description: "Claude Pro/Max",
					Key:         "anthropic",
				}}
			},
		})
		// Add help option for learning about model configuration
		// TODO: ensure this is the right place for this.
		allModels = append(allModels, Model{
			ID:          "help",
			DisplayName: "Learn about model configuration",
			Provider:    "help",
			Description: "API keys, environment variables, and more",
			Status:      "login",
			OnSelect: func() tea.Msg {
				slog.Debug("Help models selected")
				return showHelpMsg{topic: "models"}
			},
		})
	}

	if config.LLM.ExperimentalModels {
		openaiAuth := checkProviderAuth("openai")
		googleAuth := checkProviderAuth("googleai")
		if openaiAuth.HasAPIKey || openaiAuth.HasOAuth {
			openaiModels, err := fetchOpenAIModels(config)
			if err == nil && len(openaiModels) > 0 {
				for _, m := range openaiModels {
					status := "ready"
					if currentProvider == "openai" && m.ID == currentModel {
						status = "active"
					}
					allModels = append(allModels, Model{
						ID:          m.ID,
						DisplayName: m.ID, // OpenAI API doesn't provide display names
						Provider:    "openai",
						Status:      status,
					})
				}
			} else if err != nil {
				slog.Warn("failed to fetch OpenAI models", "error", err)
				allModels = append(allModels, Model{
					Provider:    "openai",
					Status:      "error",
					Description: err.Error(),
				})
			}
		}

		// Fetch Google AI models
		if googleAuth.HasAPIKey || googleAuth.HasOAuth {
			googleModels, err := fetchGoogleModels(config)
			if err == nil && len(googleModels) > 0 {
				for _, m := range googleModels {
					status := "ready"
					if currentProvider == "googleai" && m.Name == currentModel {
						status = "active"
					}
					displayName := m.DisplayName
					if displayName == "" {
						displayName = m.Name
					}
					allModels = append(allModels, Model{
						ID:          m.Name,
						DisplayName: displayName,
						Provider:    "googleai",
						Description: m.Description,
						Status:      status,
					})
				}
			} else if err != nil {
				slog.Warn("failed to fetch Google AI models", "error", err)
				allModels = append(allModels, Model{
					Provider:    "googleai",
					Status:      "error",
					Description: err.Error(),
				})
			}
		}
	}

	// Fetch Ollama models (local, no auth required)
	if ollamaAvailable {
		ollamaModels, err := fetchOllamaModels(config)
		if err == nil && len(ollamaModels) > 0 {
			for _, m := range ollamaModels {
				status := "ready"
				if currentProvider == "ollama" && m.Name == currentModel {
					status = "active"
				}
				allModels = append(allModels, Model{
					ID:          m.Name,
					DisplayName: m.Name,
					Provider:    "ollama",
					Status:      status,
				})
			}
		} else if err != nil {
			slog.Warn("failed to fetch Ollama models", "error", err)
			allModels = append(allModels, Model{
				Provider:    "ollama",
				Status:      "error",
				Description: err.Error(),
			})
		}
	}

	// Sort models: active first, then ready, then error, then login_required
	// Within each status, sort by provider then by display name
	sort.Slice(allModels, func(i, j int) bool {
		statusPriority := map[string]int{"active": 0, "login": 1, "ready": 2, "help": 3, "error": 4, "login_required": 5}
		if statusPriority[allModels[i].Status] != statusPriority[allModels[j].Status] {
			return statusPriority[allModels[i].Status] < statusPriority[allModels[j].Status]
		}
		// Then by provider
		if allModels[i].Provider != allModels[j].Provider {
			return allModels[i].Provider < allModels[j].Provider
		}
		// Then by display name
		return allModels[i].DisplayName < allModels[j].DisplayName
	})

	return allModels
}

// fetchAnthropicModels fetches available models from the Anthropic API
func fetchAnthropicModels(config *Config) ([]AnthropicModel, error) {
	// Don't use config.LLM.AuthToken or config.LLM.APIKey as they might be for a different provider
	// Instead, fetch Anthropic-specific credentials directly
	var authToken, apiKey string

	// Try OAuth tokens first
	slog.Debug("Fetching Anthropic credentials")
	tokenData, err := GetOauthToken("anthropic")
	if err == nil && tokenData != nil {
		if !shogunate.IsTokenExpired(tokenData) {
			// Token is still valid - use it
			authToken = tokenData.AccessToken
			slog.Debug("Using valid OAuth token for Anthropic")
		} else {
			// Token expired - try to refresh for Anthropic
			auth := &AuthAnthropic{}
			newAccessToken, refreshErr := auth.access()
			if refreshErr == nil {
				authToken = newAccessToken
				slog.Debug("Refreshed OAuth token for Anthropic")
			}
		}
	}

	// If no OAuth token, try API key from keyring
	if authToken == "" {
		apiKey, err = GetAPIKeyFromKeyring("anthropic")
		if err != nil {
			apiKey = ""
		}
		if apiKey != "" {
			slog.Debug("Using API key from keyring for Anthropic")
		}
	}

	// If still no credentials, try environment variable
	if authToken == "" && apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if apiKey != "" {
			slog.Debug("Using API key from environment for Anthropic")
		}
	}

	if authToken == "" && apiKey == "" {
		return nil, fmt.Errorf("no authentication configured for anthropic provider")
	}

	// Create HTTP client with appropriate authentication
	client := &http.Client{}
	if authToken != "" {
		// Use OAuth authentication
		client.Transport = &authTransport{
			token:  authToken,
			config: config,
			base:   http.DefaultTransport,
		}
	} else {
		// Use API key authentication
		client.Transport = &apiKeyTransport{
			base: http.DefaultTransport,
		}
	}

	// Determine base URL
	baseURL := "https://api.anthropic.com"
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
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
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

// fetchOpenAIModels fetches available models from the OpenAI API
func fetchOpenAIModels(config *Config) ([]OpenAIModel, error) {
	// Don't use config.LLM.APIKey as it might be for a different provider
	// Fetch OpenAI-specific credentials directly
	var apiKey string

	// Try environment variable first
	apiKey = os.Getenv("OPENAI_API_KEY")
	if apiKey != "" {
		slog.Debug("Using API key from environment for OpenAI")
	}

	// If no env var, try keyring
	if apiKey == "" {
		var err error
		apiKey, err = GetAPIKeyFromKeyring("openai")
		if err != nil || apiKey == "" {
			return nil, fmt.Errorf("no API key configured for OpenAI")
		}
		slog.Debug("Using API key from keyring for OpenAI")
	}

	// Create request
	baseURL := "https://api.openai.com"
	if envBaseURL := os.Getenv("OPENAI_BASE_URL"); envBaseURL != "" {
		baseURL = strings.TrimSuffix(envBaseURL, "/")
	}

	req, err := http.NewRequest("GET", baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Parse response
	var modelsResponse OpenAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Filter to only include chat-capable models. When OPENAI_BASE_URL points
	// at a non-OpenAI gateway (e.g. AWS Bedrock's bedrock-mantle endpoint),
	// keep all listed models — the codex filter is OpenAI-specific.
	customBaseURL := os.Getenv("OPENAI_BASE_URL") != ""
	var chatModels []OpenAIModel
	for _, m := range modelsResponse.Data {
		if customBaseURL || strings.Contains(m.ID, "codex") {
			chatModels = append(chatModels, m)
		}
	}

	// Sort by ID for consistent ordering
	sort.Slice(chatModels, func(i, j int) bool {
		return chatModels[i].ID < chatModels[j].ID
	})

	return chatModels, nil
}

// fetchGoogleModels fetches available models from the Google AI API
func fetchGoogleModels(config *Config) ([]GoogleModel, error) {
	// Don't use config.LLM.APIKey as it might be for a different provider
	// Fetch Google AI-specific credentials directly
	var apiKey string

	// Try environment variables first (both GEMINI_API_KEY and GOOGLE_API_KEY)
	apiKey = os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey != "" {
		slog.Debug("Using API key from environment for Google AI")
	}

	// If no env var, try keyring
	if apiKey == "" {
		var err error
		apiKey, err = GetAPIKeyFromKeyring("googleai")
		if err != nil || apiKey == "" {
			return nil, fmt.Errorf("no API key configured for Google AI")
		}
		slog.Debug("Using API key from keyring for Google AI")
	}

	// Create request - Google AI uses query parameter for API key
	baseURL := "https://generativelanguage.googleapis.com/v1beta/models"
	req, err := http.NewRequest("GET", baseURL+"?key="+apiKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Parse response
	var modelsResponse GoogleModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Filter to only include models that support generateContent (chat)
	var chatModels []GoogleModel
	for _, m := range modelsResponse.Models {
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" {
				// Extract model name from full path (e.g., "models/gemini-pro" -> "gemini-pro")
				name := m.Name
				if strings.HasPrefix(name, "models/") {
					name = strings.TrimPrefix(name, "models/")
				}
				m.Name = name
				chatModels = append(chatModels, m)
				break
			}
		}
	}

	// Sort by name for consistent ordering
	sort.Slice(chatModels, func(i, j int) bool {
		return chatModels[i].Name < chatModels[j].Name
	})

	return chatModels, nil
}

// getOllamaBaseURL returns the Ollama API base URL
func getOllamaBaseURL() string {
	if envURL := os.Getenv("OLLAMA_HOST"); envURL != "" {
		return strings.TrimSuffix(envURL, "/")
	}
	return "http://localhost:11434"
}

// checkOllamaAvailable checks if Ollama is running and accessible
func checkOllamaAvailable() bool {
	baseURL := getOllamaBaseURL()
	client := &http.Client{
		Timeout: 2 * time.Second, // Quick timeout for local service check
	}

	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// fetchOllamaModels fetches available models from the local Ollama instance
func fetchOllamaModels(config *Config) ([]OllamaModel, error) {
	baseURL := getOllamaBaseURL()

	// Create request
	req, err := http.NewRequest("GET", baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Parse response
	var modelsResponse OllamaModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Sort by name for consistent ordering
	sort.Slice(modelsResponse.Models, func(i, j int) bool {
		return modelsResponse.Models[i].Name < modelsResponse.Models[j].Name
	})

	return modelsResponse.Models, nil
}

// ModelsWindow is a component for displaying unified model selection across all providers
// Navigation is handled by ContentComponent
type ModelsWindow struct {
	SelectWindow[Model]
	currentModel string
}

// NewModelsWindow creates a new models window
func NewModelsWindow() ModelsWindow {
	sw := NewSelectWindow[Model]()
	sw.Height = 15
	sw.SetSize(70, 15)
	sw.SetSelectable(IsModelSelectable)

	return ModelsWindow{
		SelectWindow: sw,
		currentModel: "",
	}
}

// SetModels updates the models list (unified Model type)
func (m *ModelsWindow) SetModels(models []Model, currentModel string) {
	m.SetItems(models)
	m.currentModel = currentModel
}

// SetError sets error state
func (m *ModelsWindow) SetError(err string) {
	// SelectWindow expects error interface
	if err == "" {
		m.SelectWindow.SetError(nil)
	} else {
		m.SelectWindow.SetError(fmt.Errorf("%s", err))
	}
}

// GetInitialSelection returns the index of the current model (or first selectable)
func (m *ModelsWindow) GetInitialSelection() int {
	// First, try to find the active model
	for i, model := range m.Items {
		if model.Status == "active" {
			return i
		}
	}
	// Fall back to first selectable item
	return m.FirstSelectableIndex(IsModelSelectable)
}

// GetSelectedModel returns the model at the given index
func (m *ModelsWindow) GetSelectedModel(index int) *Model {
	return m.GetSelectedItem(index)
}

// getProviderIcon returns an icon for the provider
func getProviderIcon(provider string) string {
	switch provider {
	case "anthropic":
		return "🅰️ "
	case "openai":
		return "🤖"
	case "googleai":
		return "🔷"
	case "ollama":
		return "🦙"
	case "help":
		return "📖"
	default:
		return "  "
	}
}

// getStatusIcon returns an icon for the status
func getStatusIcon(status string) string {
	switch status {
	case "active":
		return "✓"
	case "ready":
		return "●"
	case "login":
		return "🔑"
	case "login_required":
		return "🔒"
	case "error":
		return "⚠"
	default:
		return " "
	}
}

// IsModelSelectable returns whether a model can be selected
// Error items are not selectable
func IsModelSelectable(model Model) bool {
	return model.Status != "error"
}

// RenderList renders the models list with the given selection
// Always renders exactly visibleSlots lines to maintain consistent height
func (m *ModelsWindow) RenderList(selectedIndex, scrollOffset, visibleSlots int) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		// TODO: use globalTheme
		Foreground(lipgloss.Color("#F952F9")).
		Background(lipgloss.Color("#000000")).
		Padding(0, 1)

	// Helper for grouping
	isFirst := true
	lastProvider := ""

	config := RenderConfig[Model]{
		ConstructTitle: func(selectedIndex, totalItems int) string {
			return titleStyle.Render(fmt.Sprintf("Select a model [%3d/%3d]:", selectedIndex+1, totalItems))
		},
		OnLoading: func(sb *strings.Builder) {
			sb.WriteString("Loading models...\n")
			sb.WriteString("\n")
			sb.WriteString("⏳ Scanning available models across all providers...\n")
		},
		OnError: func(sb *strings.Builder, err error) {
			sb.WriteString("Error loading models:\n")
			sb.WriteString("\n")
			sb.WriteString(err.Error() + "\n")
		},
		OnEmpty: func(sb *strings.Builder) {
			sb.WriteString("No models available\n")
			sb.WriteString("\n")
			sb.WriteString("Configure API keys via environment variables or :login\n")
		},
		RenderItem: func(i int, model Model, isSelected bool, sb *strings.Builder) {
			// Add provider header if provider changed
			if !isFirst {
				if model.Provider != lastProvider {
					sb.WriteString("\n")
				}
			}

			isFirst = false
			lastProvider = model.Provider

			// Error items are not selectable - no selection prefix
			prefix := "  "
			if model.Status == "error" {
				// Error items show error message, not selectable
				providerIcon := getProviderIcon(model.Provider)
				statusIcon := getStatusIcon(model.Status)
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("203")) // Red/orange for errors
				line := fmt.Sprintf("%s%s %s %s", prefix, providerIcon, statusIcon, model.Description)
				sb.WriteString(style.Render(line) + "\n")
				return
			}

			if isSelected {
				prefix = "▶ "
			}

			// Build the line with provider icon, status, and model name
			providerIcon := getProviderIcon(model.Provider)
			statusIcon := getStatusIcon(model.Status)

			displayText := model.DisplayName
			if displayText == "" {
				displayText = model.ID
			}

			// Style based on selection and status
			style := lipgloss.NewStyle()
			if isSelected {
				style = style.Foreground(lipgloss.Color("62")).Bold(true)
			} else if model.Status == "login_required" {
				style = style.Foreground(lipgloss.Color("240")) // Dimmed
			}

			// Format: "▶ 🅰️  ✓ Claude 3.5 Sonnet"
			line := fmt.Sprintf("%s%s %s %s", prefix, providerIcon, statusIcon, displayText)
			sb.WriteString(style.Render(line) + "\n")
		},
		IsSelectable: func(model Model) bool {
			return IsModelSelectable(model)
		},
	}

	return m.Render(selectedIndex, scrollOffset, config)
}

// Message types for model selection
type modelSelectedMsg struct {
	model    *Model
	onSelect tea.Cmd
}

// Message types for seal selection
type sealSelectedMsg struct {
	edictID uint
}

type sealedEdictsLoadedMsg struct {
	edicts []storage.ActiveEdict
}

// SealSelectWindow displays a selectable list of edicts pending the Ruler's seal
type SealSelectWindow struct {
	SelectWindow[storage.ActiveEdict]
}

// NewSealSelectWindow creates a new seal selection window
func NewSealSelectWindow() SealSelectWindow {
	sw := NewSelectWindow[storage.ActiveEdict]()
	sw.Height = 15
	sw.SetSize(70, 15)
	return SealSelectWindow{SelectWindow: sw}
}

// RenderList renders the seal selection list
func (s *SealSelectWindow) RenderList(selectedIndex, scrollOffset, visibleSlots int) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F952F9")).
		Background(lipgloss.Color("#000000")).
		Padding(0, 1)

	config := RenderConfig[storage.ActiveEdict]{
		ConstructTitle: func(selectedIndex, totalItems int) string {
			return titleStyle.Render(fmt.Sprintf("Select edict to seal [%3d/%3d]:", selectedIndex+1, totalItems))
		},
		OnEmpty: func(sb *strings.Builder) {
			sb.WriteString("No edicts awaiting seal\n")
		},
		RenderItem: func(i int, edict storage.ActiveEdict, isSelected bool, sb *strings.Builder) {
			prefix := "  "
			if isSelected {
				prefix = "▶ "
			}

			// Truncate intent for display (first line only, max 50 chars)
			intent := edict.Intent
			if nl := strings.Index(intent, "\n"); nl != -1 {
				intent = intent[:nl]
			}
			if len(intent) > 50 {
				intent = intent[:47] + "..."
			}

			// Judge seal: 刑 when present, spaces when absent
			judge := "  "
			if edict.HasJudgeSeal {
				judge = "刑"
			}

			// Sage seal: 聖 when present, spaces when absent
			sage := "  "
			if edict.HasSageSeal {
				sage = "聖"
			}

			// Format: "▶ [  3] 刑 聖 Fix the login bug..."
			line := fmt.Sprintf("%s[%3d] %s %s %s",
				prefix, edict.ID,
				judge,
				sage,
				intent)

			style := lipgloss.NewStyle()
			if isSelected {
				style = style.Foreground(lipgloss.Color("62")).Bold(true)
			}
			sb.WriteString(style.Render(line) + "\n")
		},
	}

	return s.Render(selectedIndex, scrollOffset, config)
}

// Message types for model loading
type modelsLoadedMsg struct {
	models []Model
}

type modelsLoadErrorMsg struct {
	error string
}

type showModelSelectionMsg struct{}

// Command handler - now works with all providers
func handleModelsCommand(model *TUIModel, args []string) tea.Cmd {
	// Immediately show the models view with loading state
	showModelsCmd := model.tabs.Content().ShowUnifiedModels([]Model{}, model.config.LLM.Model)
	model.tabs.Content().models.SetLoading(true)

	// Fetch models in the background
	loadCmd := func() tea.Msg {
		models := fetchAllModels(model.config)
		return modelsLoadedMsg{models: models}
	}

	// Return both commands - show view immediately, then load data
	return tea.Batch(showModelsCmd, loadCmd)
}
