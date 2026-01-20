package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/auth"
	"github.com/afittestide/asimi/internal/config"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/fake"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// GetModelClient creates and returns an LLM client based on the configuration
func GetModelClient(llmConfig *config.LLMConfig) (llms.Model, error) {
	// First try to load tokens from keyring if not already in config
	if llmConfig.AuthToken == "" && llmConfig.APIKey == "" {
		// Try OAuth tokens first
		token, err := auth.GetOauthToken(llmConfig.Provider)
		if err == nil && token != nil {
			if !auth.IsTokenExpired(token) {
				// Token is still valid - use it
				llmConfig.AuthToken = token.AccessToken
				llmConfig.RefreshToken = token.RefreshToken
			} else {
				// Token exists but expired - try to refresh it
				slog.Info("Token expired, attempting refresh", "provider", llmConfig.Provider)

				// Try to refresh the token
				if !auth.RefreshOAuthToken(llmConfig) {
					// Refresh failed - fall back to API key
					slog.Warn("Token refresh failed, falling back to API key", "provider", llmConfig.Provider)
					apiKey, err := auth.GetAPIKeyFromKeyring(llmConfig.Provider)
					if err == nil && apiKey != "" {
						llmConfig.APIKey = apiKey
					}
				} else {
					slog.Info("Token refresh successful", "provider", llmConfig.Provider)
				}
			}
		} else {
			// No token data found - try API key from keyring
			apiKey, err := auth.GetAPIKeyFromKeyring(llmConfig.Provider)
			if err == nil && apiKey != "" {
				llmConfig.APIKey = apiKey
			}
		}
	}

	switch llmConfig.Provider {
	case "fake":
		llm := fake.NewFakeLLM([]string{})
		return llm, nil
	case "ollama":
		if err := EnsureOllamaConfigured(llmConfig.BaseURL); err != nil {
			return nil, err
		}
		// For Ollama, we can use default options or customize based on config
		opts := []ollama.Option{
			ollama.WithModel(llmConfig.Model),
		}

		if llmConfig.BaseURL != "" {
			opts = append(opts, ollama.WithServerURL(llmConfig.BaseURL))
		}

		return ollama.New(opts...)
	case "openai":
		// For OpenAI, we need to set the API key
		opts := []openai.Option{
			openai.WithModel(llmConfig.Model),
		}

		if llmConfig.APIKey != "" {
			opts = append(opts, openai.WithToken(llmConfig.APIKey))
		}

		if llmConfig.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(llmConfig.BaseURL))
		}

		return openai.New(opts...)
	case "anthropic":
		// For Anthropic, we can use either OAuth tokens or API key
		opts := []anthropic.Option{
			anthropic.WithModel(llmConfig.Model),
		}

		// Prefer OAuth access token over API key
		if llmConfig.AuthToken != "" {
			// Use the token we already have (either valid or freshly refreshed from above)
			accessToken := llmConfig.AuthToken

			// Pass placeholder to SDK to bypass API key validation
			// The real authentication happens in the HTTP transport
			// We can't use empty string as the SDK validates for non-empty token
			opts = append(opts, anthropic.WithToken("oauth-placeholder"))

			// Create custom HTTP client with OAuth transport
			httpClient := &http.Client{
				Transport: &AnthropicOAuthTransport{
					Token:     accessToken,
					LLMConfig: llmConfig,
					Base:      http.DefaultTransport,
				},
			}
			opts = append(opts, anthropic.WithHTTPClient(httpClient))
		} else if llmConfig.APIKey != "" {
			opts = append(opts, anthropic.WithToken(llmConfig.APIKey))
		}

		if llmConfig.BaseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(llmConfig.BaseURL))
		}

		return anthropic.New(opts...)
	case "googleai":
		// For GoogleAI, we need to set the API key
		apiKey := llmConfig.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
			if apiKey == "" {
				return nil, fmt.Errorf("missing Google AI API key. Set it in the config file or via GEMINI_API_KEY environment variable")
			}
		}

		opts := []googleai.Option{
			googleai.WithDefaultModel(llmConfig.Model),
			googleai.WithAPIKey(apiKey),
		}

		return googleai.New(context.Background(), opts...)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", llmConfig.Provider)
	}
}

// EnsureOllamaConfigured checks if Ollama is properly configured and reachable
func EnsureOllamaConfigured(rawBaseURL string) error {
	baseURL := rawBaseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	} else if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid ollama base URL %q: %w", rawBaseURL, err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid ollama base URL %q: host is empty", rawBaseURL)
	}

	host := strings.ToLower(parsed.Hostname())
	isLocalHost := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if isLocalHost {
		if _, err := exec.LookPath("ollama"); err != nil {
			installHint := "Install Ollama from https://ollama.com/download."
			if runtime.GOOS == "darwin" {
				installHint = "Install Ollama on macOS via https://ollama.com/download or Homebrew (`brew install ollama`)."
			}
			return fmt.Errorf("ollama CLI not found in PATH: %w. %s", err, installHint)
		}
	}

	versionURL := parsed.ResolveReference(&url.URL{Path: "/api/version"})

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(versionURL.String())
	if err != nil {
		startHint := fmt.Sprintf("Ensure the Ollama service is reachable at %s.", parsed.Host)
		if isLocalHost {
			startHint = "Ensure the Ollama service is running (start it with `ollama serve`)."
			if runtime.GOOS == "darwin" {
				startHint = "Launch the Ollama app or run `ollama serve` to start the background service."
			}
		}
		return fmt.Errorf("unable to reach ollama at %s: %w. %s", versionURL.String(), err, startHint)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("ollama at %s returned status %d", versionURL.String(), resp.StatusCode)
	}

	return nil
}

// AnthropicOAuthTransport adds OAuth headers for Anthropic API
type AnthropicOAuthTransport struct {
	Token     string
	LLMConfig *config.LLMConfig
	Base      http.RoundTripper
}

func (t *AnthropicOAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check if token needs refresh before making the request
	if t.LLMConfig != nil && auth.RefreshOAuthToken(t.LLMConfig) {
		// Token was refreshed, update transport token
		t.Token = t.LLMConfig.AuthToken
	}

	// Clone request to avoid mutating caller's request
	r := req.Clone(req.Context())

	// Add OAuth Bearer token (overwrite any existing authorization)
	if t.Token != "" {
		r.Header.Set("Authorization", "Bearer "+t.Token)
	}

	// Add required beta headers exactly as specified
	// Order matters: oauth-2025-04-20 must come first for OAuth mode
	r.Header.Set("anthropic-beta",
		"oauth-2025-04-20,claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14")

	// Remove x-api-key header - critical for OAuth to work
	r.Header.Del("x-api-key")
	r.Header.Del("X-Api-Key") // Remove all case variations

	// Override URL based on ANTHROPIC_BASE_URL environment variable
	if baseURL := os.Getenv("ANTHROPIC_BASE_URL"); baseURL != "" {
		if parsedURL, err := url.Parse(baseURL + "/v1/messages"); err == nil {
			r.URL = parsedURL
		}
	}

	if t.Base == nil {
		t.Base = http.DefaultTransport
	}
	return t.Base.RoundTrip(r)
}

// AnthropicAPIKeyTransport adds beta headers for API key authentication
type AnthropicAPIKeyTransport struct {
	Base http.RoundTripper
}

func (t *AnthropicAPIKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone request to avoid mutating caller's request
	r := req.Clone(req.Context())

	// Add beta headers for API key mode (no oauth header)
	r.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14")

	if t.Base == nil {
		t.Base = http.DefaultTransport
	}
	return t.Base.RoundTrip(r)
}
