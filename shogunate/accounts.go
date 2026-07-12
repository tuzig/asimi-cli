// Package shogunate contains the core ministers and orchestration logic.
package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/afittestide/asimi/internal/keyring"
	"github.com/afittestide/asimi/internal/utils"
	"github.com/maximhq/bifrost/core/schemas"
)

// BifrostLogger wraps slog.Logger to implement schemas.Logger
type BifrostLogger struct {
	logger *slog.Logger
}

func (l *BifrostLogger) log(level slog.Level, msg string, args ...any) {
	if len(args) > 0 {
		l.logger.Log(context.Background(), level, fmt.Sprintf(msg, args...))
	} else {
		l.logger.Log(context.Background(), level, msg)
	}
}
func (l *BifrostLogger) Debug(msg string, args ...any)          { l.log(slog.LevelDebug, msg, args...) }
func (l *BifrostLogger) Info(msg string, args ...any)           { l.log(slog.LevelInfo, msg, args...) }
func (l *BifrostLogger) Warn(msg string, args ...any)           { l.log(slog.LevelWarn, msg, args...) }
func (l *BifrostLogger) Error(msg string, args ...any)          { l.log(slog.LevelError, msg, args...) }
func (l *BifrostLogger) Fatal(msg string, args ...any)          { l.log(slog.LevelError, msg, args...) }
func (l *BifrostLogger) SetLevel(schemas.LogLevel)              {}
func (l *BifrostLogger) SetOutputType(schemas.LoggerOutputType) {}
func (l *BifrostLogger) LogHTTPRequest(schemas.LogLevel, string) schemas.LogEventBuilder {
	return schemas.NoopLogEvent
}

// NewBifrostLogger creates a BifrostLogger wrapping the provided slog.Logger
func NewBifrostLogger(logger *slog.Logger) *BifrostLogger {
	return &BifrostLogger{logger: logger}
}

// Account implements schemas.Account using the OS keyring for credential storage.
// When apiKeys is populated (sandbox/daemon mode), keys are read from the map
// first; providers absent from the map fall through to keyring for backward
// compatibility with in-process mode.
type Account struct {
	requestTimeout    int
	streamIdleTimeout int
	maxRetries        int
	baseURL           string
	apiKeys           map[string]string
	codexAccountID    string
}

// NewAccount creates a new Account implementation backed by the OS keyring.
func NewAccount(requestTimeout, streamIdleTimeout, maxRetries int, baseURL string) schemas.Account {
	return &Account{requestTimeout: requestTimeout, streamIdleTimeout: streamIdleTimeout, maxRetries: maxRetries, baseURL: baseURL}
}

// NewAccountWithKeys creates a new Account that reads API keys from the given
// map first, falling through to keyring for providers not present in the map.
// Environment-variable reads are removed from the primary path; the caller
// (typically the daemon) is responsible for populating the map from env vars
// or any other source.
func NewAccountWithKeys(requestTimeout, streamIdleTimeout, maxRetries int, baseURL string, apiKeys map[string]string, codexAccountID string) schemas.Account {
	return &Account{requestTimeout: requestTimeout, streamIdleTimeout: streamIdleTimeout, maxRetries: maxRetries, baseURL: baseURL, apiKeys: apiKeys, codexAccountID: codexAccountID}
}

// GetConfiguredProviders returns providers that have credentials configured.
// When apiKeys is populated, only providers present in the map are reported.
// Otherwise (in-process mode), all standard providers plus Bedrock (if AWS
// env vars are present) are returned for backward compatibility.
func (a *Account) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	providers := []schemas.ModelProvider{
		schemas.OpenAI,
		schemas.Anthropic,
		schemas.Azure,
		schemas.Gemini,
		schemas.OpenRouter,
	}

	// Check for Bedrock credentials
	if a.apiKeys != nil {
		// Daemon mode: check the apiKeys map for AWS credentials
		if a.apiKeys["AWS_ACCESS_KEY_ID"] != "" && a.apiKeys["AWS_SECRET_ACCESS_KEY"] != "" {
			providers = append(providers, schemas.Bedrock)
		}
	} else {
		// In-process mode: check environment variables for AWS credentials
		if hasAWSEnvCredentials() {
			providers = append(providers, schemas.Bedrock)
		}
	}

	return providers, nil
}

// hasAWSEnvCredentials checks if standard AWS environment variables are set.
// This enables Bedrock provider support without explicit key storage.
func hasAWSEnvCredentials() bool {
	return os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != ""
}

// GetKeysForProvider returns API keys or OAuth tokens for a given provider
func (a *Account) GetKeysForProvider(ctx context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {
	providerStr := string(provider)

	// Primary path: read from the injected apiKeys map (sandbox/daemon mode).
	if a.apiKeys != nil {
		// Special handling for Bedrock: check for AWS credentials in the map
		if providerStr == "bedrock" {
			accessKey := a.apiKeys["AWS_ACCESS_KEY_ID"]
			secretKey := a.apiKeys["AWS_SECRET_ACCESS_KEY"]
			if accessKey != "" && secretKey != "" {
				enabled := true
				key := schemas.Key{
					ID:      "bedrock_aws",
					Name:    "Bedrock AWS Credentials",
					Models:  []string{"*"},
					Weight:  1.0,
					Enabled: &enabled,
					BedrockKeyConfig: &schemas.BedrockKeyConfig{
						AccessKey: schemas.EnvVar{Val: accessKey},
						SecretKey: schemas.EnvVar{Val: secretKey},
					},
				}
				if sessionToken := a.apiKeys["AWS_SESSION_TOKEN"]; sessionToken != "" {
					key.BedrockKeyConfig.SessionToken = &schemas.EnvVar{Val: sessionToken}
				}
				return []schemas.Key{key}, nil
			}
			// No AWS credentials in map — return empty, don't fall through to keyring
			return []schemas.Key{}, nil
		}

		if apiKey, ok := a.apiKeys[providerStr]; ok && apiKey != "" {
			enabled := true
			return []schemas.Key{
				{ID: providerStr + "_apikey", Name: providerStr + " API Key", Value: schemas.EnvVar{Val: apiKey}, Models: []string{"*"}, Weight: 1.0, Enabled: &enabled},
			}, nil
		}
		// Provider is in the map but empty — skip keyring fallback; the
		// caller explicitly provided no key for this provider.
		if _, exists := a.apiKeys[providerStr]; exists {
			return []schemas.Key{}, nil
		}
	}

	// Fallback: keyring-backed path for in-process mode (providers not in the map).

	// Bedrock via keyring: only if AWS keys were injected into the map or
	// discovered through keyring. Env-var-only Bedrock is the client's
	// responsibility when using NewAccountWithKeys.
	if providerStr == "bedrock" {
		// When apiKeys map is in use and "bedrock" wasn't provided, check
		// keyring as a last resort.
		apiKey, err := keyring.GetAPIKey(providerStr)
		if err == nil && apiKey != "" {
			enabled := true
			return []schemas.Key{
				{ID: providerStr + "_apikey", Name: providerStr + " API Key", Value: schemas.EnvVar{Val: apiKey}, Models: []string{}, Weight: 1.0, Enabled: &enabled},
			}, nil
		}
		return []schemas.Key{}, nil
	}

	// Resolve the Bifrost provider string to the keyring key name.
	// The keyring stores Google AI keys under "googleai", but Bifrost uses "gemini".
	krProvider := providerStr
	if providerStr == "gemini" {
		krProvider = "googleai"
	}

	apiKey, err := keyring.GetAPIKey(krProvider)
	if err == nil && apiKey != "" {
		enabled := true
		return []schemas.Key{
			{ID: providerStr + "_apikey", Name: providerStr + " API Key", Value: schemas.EnvVar{Val: apiKey}, Models: []string{}, Weight: 1.0, Enabled: &enabled},
		}, nil
	}

	return []schemas.Key{}, nil
}

// getBaseURLFromEnv returns the base URL from provider-specific environment variable
func getBaseURLFromEnv(provider string) string {
	envVarNames := map[string]string{
		"openai":     "OPENAI_BASE_URL",
		"anthropic":  "ANTHROPIC_BASE_URL",
		"azure":      "AZURE_OPENAI_BASE_URL",
		"gemini":     "GEMINI_BASE_URL",
		"openrouter": "OPENROUTER_BASE_URL",
		"bedrock":    "AWS_BEDROCK_BASE_URL",
	}
	if envName, ok := envVarNames[provider]; ok {
		if url := os.Getenv(envName); url != "" {
			return url
		}
	}
	return ""
}

// GetConfigForProvider returns network configuration for a provider
func (a *Account) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	networkConfig := schemas.DefaultNetworkConfig
	if a.requestTimeout > 0 {
		networkConfig.DefaultRequestTimeoutInSeconds = a.requestTimeout
	}
	if a.streamIdleTimeout > 0 {
		networkConfig.StreamIdleTimeoutInSeconds = a.streamIdleTimeout
	}
	if a.maxRetries > 0 {
		networkConfig.MaxRetries = a.maxRetries
	}
	if a.baseURL != "" {
		networkConfig.BaseURL = a.baseURL
	} else if baseURL := getBaseURLFromEnv(string(provider)); baseURL != "" {
		networkConfig.BaseURL = baseURL
		slog.Debug("base URL from env", "provider", provider, "base_url", networkConfig.BaseURL)
	} else {
		slog.Debug("base URL using provider default", "provider", provider)
	}

	// Identify asimi to every provider. Bifrost's SetExtraHeaders only sets
	// a header when it's absent, so these can't clobber Authorization etc.
	headers := map[string]string{
		"User-Agent": "asimi-cli/" + utils.AsimiVersion,
		"originator": "asimi",
	}
	// Codex OAuth: set the chatgpt-account-id header so the backend routes
	// requests to the correct organization.
	if provider == schemas.OpenAI && a.codexAccountID != "" {
		headers["chatgpt-account-id"] = a.codexAccountID
	}
	// OpenRouter app attribution: HTTP-Referer + X-Title surface asimi on
	// openrouter.ai/rankings and in users' dashboards.
	if provider == schemas.OpenRouter {
		headers["HTTP-Referer"] = "https://github.com/afittestide/asimi-cli"
		headers["X-Title"] = "Asimi"
	}
	networkConfig.ExtraHeaders = headers

	return &schemas.ProviderConfig{
		NetworkConfig: networkConfig,
		Logger:        NewBifrostLogger(slog.Default()),
	}, nil
}
