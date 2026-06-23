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
	Status      string // "active" (currently selected), "ready" (key found), "login_required"
	OnSelect    tea.Cmd
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

// checkProviderAuth checks if a provider has an API key configured (env var or keyring)
func checkProviderAuth(provider string) bool {
	// Check environment variable
	switch provider {
	case "anthropic":
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			return true
		}
	case "openai":
		if os.Getenv("OPENAI_API_KEY") != "" {
			return true
		}
	case "googleai":
		if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
			return true
		}
	case "openrouter":
		if os.Getenv("OPENROUTER_API_KEY") != "" {
			return true
		}
	}

	// Check keyring
	apiKey, err := GetAPIKeyFromKeyring(provider)
	if err == nil && apiKey != "" {
		return true
	}

	return false
}

// providerEnvVar returns the primary environment variable name for a provider
func providerEnvVar(provider string) string {
	switch provider {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "googleai":
		return "GEMINI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	default:
		return strings.ToUpper(provider) + "_API_KEY"
	}
}

// providerDisplayName returns a human-readable provider name
func providerDisplayName(provider string) string {
	switch provider {
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	case "googleai":
		return "Google AI"
	case "openrouter":
		return "OpenRouter"
	default:
		return provider
	}
}

// fetchAllModels aggregates models from all providers that have API keys configured
func fetchAllModels(config *Config) []Model {
	var allModels []Model
	currentProvider := config.LLM.Provider
	currentModel := config.LLM.Model
	ollamaAvailable := checkOllamaAvailable()

	// Fetch Anthropic models if key is configured
	if checkProviderAuth("anthropic") {
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
			slog.Warn("failed to fetch Anthropic models", "error", err)
			allModels = append(allModels, Model{
				Provider:    "anthropic",
				Status:      "error",
				Description: err.Error(),
			})
		}
	}

	// Fetch OpenAI models if key is configured
	if checkProviderAuth("openai") {
		openaiModels, err := fetchOpenAIModels(config)
		if err == nil && len(openaiModels) > 0 {
			for _, m := range openaiModels {
				status := "ready"
				if currentProvider == "openai" && m.ID == currentModel {
					status = "active"
				}
				allModels = append(allModels, Model{
					ID:          m.ID,
					DisplayName: m.ID,
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

	// Fetch Google AI models if key is configured
	if checkProviderAuth("googleai") {
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

	// Fetch OpenRouter models if key is configured
	if checkProviderAuth("openrouter") {
		openrouterModels, err := fetchOpenRouterModels(config)
		if err == nil && len(openrouterModels) > 0 {
			for _, m := range openrouterModels {
				status := "ready"
				if currentProvider == "openrouter" && m.ID == currentModel {
					status = "active"
				}
				allModels = append(allModels, Model{
					ID:          m.ID,
					DisplayName: m.ID,
					Provider:    "openrouter",
					Status:      status,
				})
			}
		} else if err != nil {
			slog.Warn("failed to fetch OpenRouter models", "error", err)
			allModels = append(allModels, Model{
				Provider:    "openrouter",
				Status:      "error",
				Description: err.Error(),
			})
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

	// Emit login_required entries for providers without auth
	knownProviders := []string{"openai", "anthropic", "googleai", "openrouter"}
	for _, p := range knownProviders {
		if checkProviderAuth(p) {
			continue
		}
		if p == "openai" {
			allModels = append(allModels, Model{
				ID:          "codex-login",
				DisplayName: "Login with OpenAI (Codex OAuth)",
				Provider:    "openai",
				Status:      "login_required",
			})
		} else {
			allModels = append(allModels, Model{
				ID:          p + "-apikey",
				DisplayName: "Set API key for " + providerDisplayName(p) + " (env var)",
				Provider:    p,
				Status:      "login_required",
			})
		}
	}

	// Sort models: active first, then ready, then error, then login_required
	sort.Slice(allModels, func(i, j int) bool {
		statusPriority := map[string]int{"active": 0, "ready": 1, "error": 2, "login_required": 3}
		if statusPriority[allModels[i].Status] != statusPriority[allModels[j].Status] {
			return statusPriority[allModels[i].Status] < statusPriority[allModels[j].Status]
		}
		if allModels[i].Provider != allModels[j].Provider {
			return allModels[i].Provider < allModels[j].Provider
		}
		return allModels[i].DisplayName < allModels[j].DisplayName
	})

	return allModels
}

// fetchAnthropicModels fetches available models from the Anthropic API
func fetchAnthropicModels(config *Config) ([]AnthropicModel, error) {
	var apiKey string

	// Try API key from keyring first
	apiKey, err := GetAPIKeyFromKeyring("anthropic")
	if err != nil {
		apiKey = ""
	}
	if apiKey != "" {
		slog.Debug("Using API key from keyring for Anthropic")
	}

	// If still no credentials, try environment variable
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if apiKey != "" {
			slog.Debug("Using API key from environment for Anthropic")
		}
	}

	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured for anthropic provider")
	}

	// Create HTTP client with API key authentication
	client := &http.Client{
		Transport: &apiKeyTransport{base: http.DefaultTransport},
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

// fetchOpenRouterModels fetches available models from the OpenRouter API
func fetchOpenRouterModels(config *Config) ([]OpenAIModel, error) {
	var apiKey string

	// Try environment variable first
	apiKey = os.Getenv("OPENROUTER_API_KEY")
	if apiKey != "" {
		slog.Debug("Using API key from environment for OpenRouter")
	}

	// If no env var, try keyring
	if apiKey == "" {
		var err error
		apiKey, err = GetAPIKeyFromKeyring("openrouter")
		if err != nil || apiKey == "" {
			return nil, fmt.Errorf("no API key configured for OpenRouter")
		}
		slog.Debug("Using API key from keyring for OpenRouter")
	}

	// Determine base URL
	baseURL := "https://openrouter.ai/api/v1"
	if envBaseURL := os.Getenv("OPENROUTER_BASE_URL"); envBaseURL != "" {
		baseURL = strings.TrimSuffix(envBaseURL, "/")
	}

	req, err := http.NewRequest("GET", baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/afittestide/asimi")
	req.Header.Set("X-Title", "asimi")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var modelsResponse OpenAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Filter to chat models — skip embedding/image models
	var chatModels []OpenAIModel
	for _, m := range modelsResponse.Data {
		if strings.Contains(m.ID, "embedding") || strings.Contains(m.ID, "tts") || strings.Contains(m.ID, "dall-e") {
			continue
		}
		chatModels = append(chatModels, m)
	}

	sort.Slice(chatModels, func(i, j int) bool {
		return chatModels[i].ID < chatModels[j].ID
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
		Timeout: 2 * time.Second,
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

	req, err := http.NewRequest("GET", baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var modelsResponse OllamaModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	sort.Slice(modelsResponse.Models, func(i, j int) bool {
		return modelsResponse.Models[i].Name < modelsResponse.Models[j].Name
	})

	return modelsResponse.Models, nil
}

// ModelsWindow is a component for displaying unified model selection across all providers
type ModelsWindow struct {
	SelectWindow[Model]
	currentModel string

	// Search state
	searchPattern  string // current search pattern (empty = no active search)
	searchDirection int   // 1 = forward (/), -1 = backward (?)
	matchIndices   []int  // indices into Items that match the pattern
	matchCursor    int    // current position in matchIndices
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
	if err == "" {
		m.SelectWindow.SetError(nil)
	} else {
		m.SelectWindow.SetError(fmt.Errorf("%s", err))
	}
}

// GetInitialSelection returns the index of the current model (or first selectable)
func (m *ModelsWindow) GetInitialSelection() int {
	for i, model := range m.Items {
		if model.Status == "active" {
			return i
		}
	}
	return m.FirstSelectableIndex(IsModelSelectable)
}

// GetSelectedModel returns the model at the given index
func (m *ModelsWindow) GetSelectedModel(index int) *Model {
	return m.GetSelectedItem(index)
}

// Search computes match indices for the given pattern and returns the index to jump to.
// direction: 1 = forward (/), -1 = backward (?)
// currentItem: the currently selected item index
// Returns -1 if no matches found.
func (m *ModelsWindow) Search(pattern string, direction int, currentItem int) int {
	if pattern == "" {
		m.searchPattern = ""
		m.searchDirection = 0
		m.matchIndices = nil
		m.matchCursor = 0
		return currentItem
	}

	m.searchPattern = pattern
	m.searchDirection = direction
	lowerPattern := strings.ToLower(pattern)
	m.matchIndices = m.matchIndices[:0]

	for i, model := range m.Items {
		if strings.Contains(strings.ToLower(model.ID), lowerPattern) ||
			strings.Contains(strings.ToLower(model.DisplayName), lowerPattern) {
			m.matchIndices = append(m.matchIndices, i)
		}
	}

	if len(m.matchIndices) == 0 {
		m.matchCursor = 0
		return -1
	}

	// Find the first match in the desired direction, with wrap-around
	if direction > 0 {
		// Forward: first match at or after currentItem+1, wrap around
		for _, idx := range m.matchIndices {
			if idx > currentItem {
				m.matchCursor = findMatchCursor(m.matchIndices, idx)
				return idx
			}
		}
		// Wrap around to first match
		m.matchCursor = 0
		return m.matchIndices[0]
	}

	// Backward: first match at or before currentItem-1, wrap around
	for i := len(m.matchIndices) - 1; i >= 0; i-- {
		if m.matchIndices[i] < currentItem {
			m.matchCursor = i
			return m.matchIndices[i]
		}
	}
	// Wrap around to last match
	m.matchCursor = len(m.matchIndices) - 1
	return m.matchIndices[len(m.matchIndices)-1]
}

// NextMatch moves through existing matchIndices and returns the next/previous match.
// direction: 1 = next match in search direction, -1 = opposite direction
// Returns -1 if no matches exist.
func (m *ModelsWindow) NextMatch(currentItem int, direction int) int {
	if len(m.matchIndices) == 0 {
		return -1
	}

	// Find the current cursor position based on currentItem
	m.matchCursor = findMatchCursor(m.matchIndices, currentItem)

	if direction > 0 {
		m.matchCursor++
		if m.matchCursor >= len(m.matchIndices) {
			m.matchCursor = 0
		}
	} else {
		m.matchCursor--
		if m.matchCursor < 0 {
			m.matchCursor = len(m.matchIndices) - 1
		}
	}

	return m.matchIndices[m.matchCursor]
}

// findMatchCursor returns the position of idx in matchIndices, or the nearest
// lower position if idx is not in the list.
func findMatchCursor(matchIndices []int, idx int) int {
	for i, mi := range matchIndices {
		if mi == idx {
			return i
		}
	}
	// Not found — find the largest index < idx
	result := 0
	for i, mi := range matchIndices {
		if mi < idx {
			result = i
		} else {
			break
		}
	}
	return result
}

// MatchCount returns the number of matches from the last search
func (m *ModelsWindow) MatchCount() int {
	return len(m.matchIndices)
}

// CurrentMatchNumber returns the 1-based position of matchCursor in matchIndices
func (m *ModelsWindow) CurrentMatchNumber() int {
	if len(m.matchIndices) == 0 {
		return 0
	}
	return m.matchCursor + 1
}

// HasSearch returns true if there's an active search pattern
func (m *ModelsWindow) HasSearch() bool {
	return m.searchPattern != ""
}

// ClearSearch resets search state
func (m *ModelsWindow) ClearSearch() {
	m.searchPattern = ""
	m.searchDirection = 0
	m.matchIndices = nil
	m.matchCursor = 0
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
	case "openrouter":
		return "🔀"
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
		return ""
	case "login_required":
		return "🔒"
	case "error":
		return "⚠"
	default:
		return ""
	}
}

// IsModelSelectable returns whether a model can be selected
func IsModelSelectable(model Model) bool {
	return model.Status != "error"
}

// RenderList renders the models list with the given selection
func (m *ModelsWindow) RenderList(selectedIndex, scrollOffset, visibleSlots int) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F952F9")).
		Background(lipgloss.Color("#000000")).
		Padding(0, 1)

	isFirst := true
	lastProvider := ""

	config := RenderConfig[Model]{
		ConstructTitle: func(selectedIndex, totalItems int) string {
			if m.HasSearch() {
				return titleStyle.Render(fmt.Sprintf("Select a model [%3d/%3d]  search %q [%d/%d]:", selectedIndex+1, totalItems, m.searchPattern, m.CurrentMatchNumber(), m.MatchCount()))
			}
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
			sb.WriteString("Select a provider below to login\n")
		},
		RenderItem: func(i int, model Model, isSelected bool, sb *strings.Builder) {
			if !isFirst && model.Provider != lastProvider {
				sb.WriteString("\n")
			}
			isFirst = false
			lastProvider = model.Provider

			prefix := "  "
			if model.Status == "error" {
				providerIcon := getProviderIcon(model.Provider)
				statusIcon := getStatusIcon(model.Status)
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
				line := fmt.Sprintf("%s%s %s %s", prefix, providerIcon, statusIcon, model.Description)
				sb.WriteString(style.Render(line) + "\n")
				return
			}

			if isSelected {
				prefix = "▶ "
			}

			providerIcon := getProviderIcon(model.Provider)
			statusIcon := getStatusIcon(model.Status)

			displayText := model.DisplayName
			if displayText == "" {
				displayText = model.ID
			}

			style := lipgloss.NewStyle()
			if isSelected {
				style = style.Foreground(lipgloss.Color("62")).Bold(true)
			} else if model.Status == "login_required" {
				style = style.Foreground(lipgloss.Color("240"))
			}

			line := fmt.Sprintf("%s%s %s", prefix, providerIcon, displayText)
			if statusIcon != "" {
				line = fmt.Sprintf("%s%s %s %s", prefix, providerIcon, statusIcon, displayText)
			}
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

			judge := "  "
			if edict.HasJudgeSeal {
				judge = "刑"
			}

			sage := "  "
			if edict.HasSageSeal {
				sage = "聖"
			}

			linePrefix := fmt.Sprintf("%s[%3d] %s %s ", prefix, edict.ID, judge, sage)
			intentWidth := s.Width - lipgloss.Width(linePrefix)
			if intentWidth < 0 {
				intentWidth = 0
			}

			intent := lipgloss.NewStyle().Inline(true).MaxWidth(intentWidth).Render(" " + edict.Intent)

			style := lipgloss.NewStyle().Inline(true).MaxWidth(s.Width)
			if isSelected {
				style = style.Foreground(lipgloss.Color("62")).Bold(true)
			}
			sb.WriteString(style.Render(linePrefix+intent) + "\n")
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
	showModelsCmd := model.tabs.Content().ShowUnifiedModels([]Model{}, model.config.LLM.Model)
	model.tabs.Content().models.SetLoading(true)

	loadCmd := func() tea.Msg {
		models := fetchAllModels(model.config)

		// Set OnSelect for login_required entries
		for i := range models {
			m := &models[i]
			if m.Status != "login_required" {
				continue
			}
			if m.Provider == "openai" {
				m.OnSelect = model.performCodexLogin()
			} else {
				envVar := providerEnvVar(m.Provider)
				name := providerDisplayName(m.Provider)
				msg := fmt.Sprintf("Set %s environment variable to use %s models", envVar, name)
				m.OnSelect = func() tea.Msg { return showSystemMsg(msg) }
			}
		}

		return modelsLoadedMsg{models: models}
	}

	return tea.Batch(showModelsCmd, loadCmd)
}
