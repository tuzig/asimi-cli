// Package shogunate contains the core ministers and orchestration logic.
package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/afittestide/asimi/internal/keyring"
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
func (l *BifrostLogger) Debug(msg string, args ...any) { l.log(slog.LevelDebug, msg, args...) }
func (l *BifrostLogger) Info(msg string, args ...any)  { l.log(slog.LevelInfo, msg, args...) }
func (l *BifrostLogger) Warn(msg string, args ...any)  { l.log(slog.LevelWarn, msg, args...) }
func (l *BifrostLogger) Error(msg string, args ...any) { l.log(slog.LevelError, msg, args...) }
func (l *BifrostLogger) Fatal(msg string, args ...any) { l.log(slog.LevelError, msg, args...) }
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
type Account struct {
	requestTimeout    int
	streamIdleTimeout int
}

// NewAccount creates a new Account implementation backed by the OS keyring.
func NewAccount(requestTimeout, streamIdleTimeout int) schemas.Account {
	return &Account{requestTimeout: requestTimeout, streamIdleTimeout: streamIdleTimeout}
}

// GetConfiguredProviders returns providers that have credentials configured
func (a *Account) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	providers := []schemas.ModelProvider{
		schemas.OpenAI,
		schemas.Anthropic,
		schemas.Azure,
		schemas.Gemini,
		schemas.OpenRouter,
	}
	// Add Bedrock if AWS credentials are available via environment
	if hasAWSEnvCredentials() {
		providers = append(providers, schemas.Bedrock)
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

	token, err := keyring.GetOauthToken(providerStr)
	if err == nil && token != nil && token.AccessToken != "" {
		enabled := true
		return []schemas.Key{
			{ID: providerStr + "_oauth", Name: providerStr + " OAuth Token", Value: schemas.EnvVar{Val: token.AccessToken}, Models: []string{}, Weight: 1.0, Enabled: &enabled},
		}, nil
	}

	envVarNames := map[string]string{
		"openrouter": "OPENROUTER_API_KEY",
		"openai":     "OPENAI_API_KEY",
		"anthropic":  "ANTHROPIC_API_KEY",
		"gemini":     "GEMINI_API_KEY",
		// Note: "bedrock" is NOT included here because it requires special handling
		// (both AWS_ACCESS_KEY_ID AND AWS_SECRET_ACCESS_KEY must be set)
	}
	if envName, ok := envVarNames[providerStr]; ok {
		if apiKey := os.Getenv(envName); apiKey != "" {
			enabled := true
			return []schemas.Key{
				{ID: providerStr + "_apikey", Name: providerStr + " API Key", Value: schemas.EnvVar{Val: apiKey}, Models: []string{"*"}, Weight: 1.0, Enabled: &enabled},
			}, nil
		}
	}

	// Handle Bedrock with AWS credentials
	if providerStr == "bedrock" && hasAWSEnvCredentials() {
		enabled := true
		region := os.Getenv("AWS_REGION")
		accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
		secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
		sessionToken := os.Getenv("AWS_SESSION_TOKEN")

		key := schemas.Key{
			ID:     "bedrock_aws",
			Name:   "AWS Credentials",
			Models: []string{"*"},
			Weight: 1.0,
			Enabled: &enabled,
		}

		if region != "" {
			regionVal := schemas.EnvVar{Val: region}
			key.BedrockKeyConfig = &schemas.BedrockKeyConfig{
				Region: &regionVal,
			}
		}
		if accessKey != "" {
			key.Value = schemas.EnvVar{Val: accessKey}
		}
		if secretKey != "" {
			if key.BedrockKeyConfig == nil {
				key.BedrockKeyConfig = &schemas.BedrockKeyConfig{}
			}
			key.BedrockKeyConfig.SecretKey = schemas.EnvVar{Val: secretKey}
		}
		if sessionToken != "" {
			tokenVal := schemas.EnvVar{Val: sessionToken}
			if key.BedrockKeyConfig == nil {
				key.BedrockKeyConfig = &schemas.BedrockKeyConfig{}
			}
			key.BedrockKeyConfig.SessionToken = &tokenVal
		}

		return []schemas.Key{key}, nil
	}

	apiKey, err := keyring.GetAPIKey(providerStr)
	if err == nil && apiKey != "" {
		enabled := true
		return []schemas.Key{
			{ID: providerStr + "_apikey", Name: providerStr + " API Key", Value: schemas.EnvVar{Val: apiKey}, Models: []string{}, Weight: 1.0, Enabled: &enabled},
		}, nil
	}

	return []schemas.Key{}, nil
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
	return &schemas.ProviderConfig{
		NetworkConfig: networkConfig,
		Logger:        NewBifrostLogger(slog.Default()),
	}, nil
}

// IsTokenExpired checks if the token has expired (with 5-minute buffer)
func IsTokenExpired(data *keyring.TokenData) bool {
	if data == nil {
		return true
	}
	return time.Now().After(data.Expiry.Add(-5 * time.Minute))
}
