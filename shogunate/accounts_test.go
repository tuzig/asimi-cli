package shogunate

import (
	"context"
	"os"
	"testing"

	"github.com/afittestide/asimi/internal/keyring"
	"github.com/maximhq/bifrost/core/schemas"
)

func TestHasAWSEnvCredentials(t *testing.T) {
	tests := []struct {
		name         string
		accessKeyID  string
		secretAccess string
		expectTrue   bool
	}{
		{
			name:         "both credentials set",
			accessKeyID:  "AKIAIOSFODNN7EXAMPLE",
			secretAccess: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			expectTrue:   true,
		},
		{
			name:         "only access key set",
			accessKeyID:  "AKIAIOSFODNN7EXAMPLE",
			secretAccess: "",
			expectTrue:   false,
		},
		{
			name:         "only secret key set",
			accessKeyID:  "",
			secretAccess: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			expectTrue:   false,
		},
		{
			name:         "neither set",
			accessKeyID:  "",
			secretAccess: "",
			expectTrue:   false,
		},
		{
			name:         "both empty strings",
			accessKeyID:  "",
			secretAccess: "",
			expectTrue:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("AWS_ACCESS_KEY_ID", tt.accessKeyID)
			os.Setenv("AWS_SECRET_ACCESS_KEY", tt.secretAccess)
			defer os.Unsetenv("AWS_ACCESS_KEY_ID")
			defer os.Unsetenv("AWS_SECRET_ACCESS_KEY")

			result := hasAWSEnvCredentials()
			if result != tt.expectTrue {
				t.Errorf("hasAWSEnvCredentials() = %v, want %v", result, tt.expectTrue)
			}
		})
	}
}

func TestGetConfiguredProviders_WithAWSEnvCredentials(t *testing.T) {
	// Save original env vars
	origAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	origSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	defer func() {
		os.Setenv("AWS_ACCESS_KEY_ID", origAccessKey)
		os.Setenv("AWS_SECRET_ACCESS_KEY", origSecretKey)
	}()

	// Set AWS credentials
	os.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")

	account := &Account{}
	providers, err := account.GetConfiguredProviders()
	if err != nil {
		t.Fatalf("GetConfiguredProviders() error = %v", err)
	}

	// Should include Bedrock when AWS credentials are set (in-process mode)
	found := false
	for _, p := range providers {
		if p == schemas.Bedrock {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Bedrock provider when AWS credentials are set")
	}

	// Should still have other providers
	if len(providers) < 5 {
		t.Errorf("expected at least 5 providers, got %d", len(providers))
	}
}

func TestGetConfiguredProviders_WithKeysMap_NoBedrock(t *testing.T) {
	// When apiKeys map is provided, Bedrock should NOT be auto-added even if
	// AWS env vars are set — the caller controls provider availability.
	os.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	defer os.Unsetenv("AWS_ACCESS_KEY_ID")
	defer os.Unsetenv("AWS_SECRET_ACCESS_KEY")

	account := &Account{apiKeys: map[string]string{"openai": "sk-test"}}
	providers, err := account.GetConfiguredProviders()
	if err != nil {
		t.Fatalf("GetConfiguredProviders() error = %v", err)
	}
	for _, p := range providers {
		if p == schemas.Bedrock {
			t.Error("expected no Bedrock provider when apiKeys map is provided")
		}
	}
}

func TestGetConfiguredProviders_WithAWSKeysInMap(t *testing.T) {
	keys := map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"openai":                "sk-test",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	providers, err := account.GetConfiguredProviders()
	if err != nil {
		t.Fatalf("GetConfiguredProviders() error = %v", err)
	}

	// Should include Bedrock when AWS credentials are in the map
	found := false
	for _, p := range providers {
		if p == schemas.Bedrock {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Bedrock provider when AWS credentials are in apiKeys map")
	}

	// Should still have all 5 base providers
	if len(providers) < 6 {
		t.Errorf("expected at least 6 providers (5 base + Bedrock), got %d", len(providers))
	}
}

func TestGetConfiguredProviders_WithAWSKeysInMap_MissingSecret(t *testing.T) {
	keys := map[string]string{
		"AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE",
		"openai":            "sk-test",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	providers, err := account.GetConfiguredProviders()
	if err != nil {
		t.Fatalf("GetConfiguredProviders() error = %v", err)
	}

	// Should NOT include Bedrock when only access key is present (missing secret)
	for _, p := range providers {
		if p == schemas.Bedrock {
			t.Error("expected no Bedrock provider when AWS secret key is missing from map")
		}
	}
}

func TestGetConfiguredProviders_WithoutAWSEnvCredentials(t *testing.T) {
	// Ensure AWS credentials are not set
	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")

	account := &Account{}
	providers, err := account.GetConfiguredProviders()
	if err != nil {
		t.Fatalf("GetConfiguredProviders() error = %v", err)
	}

	// Should NOT include Bedrock when AWS credentials are not set
	for _, p := range providers {
		if p == schemas.Bedrock {
			t.Error("expected no Bedrock provider when AWS credentials are not set")
		}
	}
}

// --- NewAccountWithKeys tests ---

func TestNewAccountWithKeys_ReturnsKeyFromMap(t *testing.T) {
	keys := map[string]string{
		"openai":    "sk-test-openai",
		"anthropic": "sk-ant-test",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	result, err := account.GetKeysForProvider(context.Background(), schemas.OpenAI)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 key, got %d", len(result))
	}
	if result[0].Value.Val != "sk-test-openai" {
		t.Errorf("expected key value 'sk-test-openai', got %q", result[0].Value.Val)
	}
}

func TestNewAccountWithKeys_ProviderNotInMap_FallsThroughToKeyring(t *testing.T) {
	// Only openai is in the map; anthropic should fall through to keyring
	keys := map[string]string{
		"openai": "sk-test-openai",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	// anthropic is not in the map, so it goes to keyring fallback (which returns empty)
	result, err := account.GetKeysForProvider(context.Background(), schemas.Anthropic)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}
	// No keyring key set, so expect empty
	if len(result) != 0 {
		t.Errorf("expected 0 keys for anthropic (no keyring, not in map), got %d", len(result))
	}
}

func TestNewAccountWithKeys_EmptyValueInMap_ReturnsNoKeys(t *testing.T) {
	// An empty string in the map means "explicitly no key for this provider"
	keys := map[string]string{
		"openai": "",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	result, err := account.GetKeysForProvider(context.Background(), schemas.OpenAI)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 keys for openai with empty value in map, got %d", len(result))
	}
}

func TestNewAccountWithKeys_NilMap_FallsThroughToKeyring(t *testing.T) {
	// Use in-memory mock keyring so the test doesn't pick up real keys
	// stored on the developer's machine (env-dependent failure).
	keyring.MockKeyring()

	// Nil map should behave like NewAccount — full keyring fallback path
	account := NewAccountWithKeys(30, 60, 3, "", nil, "")

	// No keyring key set, so expect empty
	result, err := account.GetKeysForProvider(context.Background(), schemas.OpenAI)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 keys for OpenAI (nil map, no keyring), got %d", len(result))
	}
}

func TestNewAccountWithKeys_AllProviders(t *testing.T) {
	keys := map[string]string{
		"openai":     "sk-openai",
		"anthropic":  "sk-ant",
		"gemini":     "sk-gemini",
		"openrouter": "sk-or",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	providers := []schemas.ModelProvider{schemas.OpenAI, schemas.Anthropic, schemas.Gemini, schemas.OpenRouter}
	for _, provider := range providers {
		result, err := account.GetKeysForProvider(context.Background(), provider)
		if err != nil {
			t.Fatalf("GetKeysForProvider(%s) error = %v", provider, err)
		}
		if len(result) != 1 {
			t.Errorf("expected 1 key for %s, got %d", provider, len(result))
		}
	}
}

func TestNewAccountWithKeys_BedrockNotInMap_ReturnsEmpty(t *testing.T) {
	keys := map[string]string{
		"openai": "sk-test",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	result, err := account.GetKeysForProvider(context.Background(), schemas.Bedrock)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}
	// Bedrock not in map, not in keyring — expect empty
	if len(result) != 0 {
		t.Errorf("expected 0 keys for Bedrock (not in map, no keyring), got %d", len(result))
	}
}

func TestGetKeysForProvider_Bedrock_WithAWSKeysInMap(t *testing.T) {
	keys := map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	result, err := account.GetKeysForProvider(context.Background(), schemas.Bedrock)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 key for Bedrock, got %d", len(result))
	}

	key := result[0]
	if key.ID != "bedrock_aws" {
		t.Errorf("expected key ID 'bedrock_aws', got %q", key.ID)
	}
	if key.BedrockKeyConfig == nil {
		t.Fatal("expected BedrockKeyConfig to be set")
	}
	if key.BedrockKeyConfig.AccessKey.Val != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("expected access key 'AKIAIOSFODNN7EXAMPLE', got %q", key.BedrockKeyConfig.AccessKey.Val)
	}
	if key.BedrockKeyConfig.SecretKey.Val != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("expected secret key to match, got %q", key.BedrockKeyConfig.SecretKey.Val)
	}
	if key.BedrockKeyConfig.SessionToken != nil {
		t.Error("expected no session token")
	}
}

func TestGetKeysForProvider_Bedrock_WithSessionToken(t *testing.T) {
	keys := map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"AWS_SESSION_TOKEN":     "FwoGZXIvYXdzEGMaDNu5EXAMPLESESSION",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	result, err := account.GetKeysForProvider(context.Background(), schemas.Bedrock)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 key for Bedrock, got %d", len(result))
	}

	key := result[0]
	if key.BedrockKeyConfig == nil {
		t.Fatal("expected BedrockKeyConfig to be set")
	}
	if key.BedrockKeyConfig.SessionToken == nil {
		t.Fatal("expected session token to be set")
	}
	if key.BedrockKeyConfig.SessionToken.Val != "FwoGZXIvYXdzEGMaDNu5EXAMPLESESSION" {
		t.Errorf("expected session token to match, got %q", key.BedrockKeyConfig.SessionToken.Val)
	}
}

func TestGetKeysForProvider_Bedrock_OnlyAccessKeyInMap(t *testing.T) {
	keys := map[string]string{
		"AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE",
	}
	account := NewAccountWithKeys(30, 60, 3, "", keys, "")

	result, err := account.GetKeysForProvider(context.Background(), schemas.Bedrock)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}
	// Missing secret key — no credentials should be returned
	if len(result) != 0 {
		t.Errorf("expected 0 keys for Bedrock with only access key, got %d", len(result))
	}
}

// --- Legacy tests (in-process mode, no apiKeys map) ---

func TestGetKeysForProvider_Bedrock_WithoutCredentials(t *testing.T) {
	// Save original env vars
	origAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	origSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	defer func() {
		os.Setenv("AWS_ACCESS_KEY_ID", origAccessKey)
		os.Setenv("AWS_SECRET_ACCESS_KEY", origSecretKey)
	}()

	// Ensure AWS credentials are not set
	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")

	account := &Account{}
	keys, err := account.GetKeysForProvider(context.Background(), schemas.Bedrock)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}

	// Should return empty keys when no credentials are set
	if len(keys) != 0 {
		t.Errorf("expected 0 keys when no AWS credentials are set, got %d", len(keys))
	}
}

func TestGetKeysForProvider_Bedrock_OnlyAccessKeySet(t *testing.T) {
	// Save original env vars
	origAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	origSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	defer func() {
		os.Setenv("AWS_ACCESS_KEY_ID", origAccessKey)
		os.Setenv("AWS_SECRET_ACCESS_KEY", origSecretKey)
	}()

	// Set only access key (missing secret key means hasAWSEnvCredentials returns false)
	os.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")

	account := &Account{}
	keys, err := account.GetKeysForProvider(context.Background(), schemas.Bedrock)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}

	// Should return empty keys when only access key is set
	if len(keys) != 0 {
		t.Errorf("expected 0 keys when only access key is set, got %d", len(keys))
	}
}

// TestAccountImplementsSchemaAccount ensures the Account type implements schemas.Account
func TestAccountImplementsSchemaAccount(t *testing.T) {
	var _ schemas.Account = &Account{}
}

// TestGetConfigForProvider tests the config provider
func TestGetConfigForProvider(t *testing.T) {
	account := NewAccount(30, 60, 0, "")
	cfg, err := account.GetConfigForProvider(schemas.OpenAI)
	if err != nil {
		t.Fatalf("GetConfigForProvider() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.NetworkConfig.DefaultRequestTimeoutInSeconds != 30 {
		t.Errorf("expected timeout 30, got %d", cfg.NetworkConfig.DefaultRequestTimeoutInSeconds)
	}
	if cfg.NetworkConfig.StreamIdleTimeoutInSeconds != 60 {
		t.Errorf("expected stream timeout 60, got %d", cfg.NetworkConfig.StreamIdleTimeoutInSeconds)
	}
}

// TestGetConfigForProvider_WithKeysMap tests config works with NewAccountWithKeys
func TestGetConfigForProvider_WithKeysMap(t *testing.T) {
	account := NewAccountWithKeys(45, 90, 2, "https://custom.api.com", map[string]string{"openai": "sk-test"}, "")
	cfg, err := account.GetConfigForProvider(schemas.OpenAI)
	if err != nil {
		t.Fatalf("GetConfigForProvider() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.NetworkConfig.DefaultRequestTimeoutInSeconds != 45 {
		t.Errorf("expected timeout 45, got %d", cfg.NetworkConfig.DefaultRequestTimeoutInSeconds)
	}
	if cfg.NetworkConfig.StreamIdleTimeoutInSeconds != 90 {
		t.Errorf("expected stream timeout 90, got %d", cfg.NetworkConfig.StreamIdleTimeoutInSeconds)
	}
	if cfg.NetworkConfig.MaxRetries != 2 {
		t.Errorf("expected max retries 2, got %d", cfg.NetworkConfig.MaxRetries)
	}
	if cfg.NetworkConfig.BaseURL != "https://custom.api.com" {
		t.Errorf("expected base URL 'https://custom.api.com', got %q", cfg.NetworkConfig.BaseURL)
	}
}

// TestGetConfigForProvider_CodexAccountIDHeader verifies that when a
// codexAccountID is set, GetConfigForProvider includes the
// "chatgpt-account-id" header for OpenAI provider requests.
func TestGetConfigForProvider_CodexAccountIDHeader(t *testing.T) {
	account := NewAccountWithKeys(30, 60, 3, "https://chatgpt.com/backend-api",
		map[string]string{"openai": "sk-test"}, "org-codex-123")

	cfg, err := account.GetConfigForProvider(schemas.OpenAI)
	if err != nil {
		t.Fatalf("GetConfigForProvider() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	headers := cfg.NetworkConfig.ExtraHeaders
	if headers["chatgpt-account-id"] != "org-codex-123" {
		t.Errorf("expected chatgpt-account-id header 'org-codex-123', got %q", headers["chatgpt-account-id"])
	}
	if headers["originator"] != "asimi" {
		t.Errorf("expected originator header 'asimi', got %q", headers["originator"])
	}
	if cfg.NetworkConfig.BaseURL != "https://chatgpt.com/backend-api" {
		t.Errorf("expected base URL 'https://chatgpt.com/backend-api', got %q", cfg.NetworkConfig.BaseURL)
	}
}

// TestGetConfigForProvider_NoCodexAccountID verifies that the
// chatgpt-account-id header is absent when codexAccountID is empty.
func TestGetConfigForProvider_NoCodexAccountID(t *testing.T) {
	account := NewAccountWithKeys(30, 60, 3, "", map[string]string{"openai": "sk-test"}, "")

	cfg, err := account.GetConfigForProvider(schemas.OpenAI)
	if err != nil {
		t.Fatalf("GetConfigForProvider() error = %v", err)
	}

	headers := cfg.NetworkConfig.ExtraHeaders
	if _, ok := headers["chatgpt-account-id"]; ok {
		t.Error("expected no chatgpt-account-id header when codexAccountID is empty")
	}
	// originator should still be present for all providers
	if headers["originator"] != "asimi" {
		t.Errorf("expected originator header 'asimi', got %q", headers["originator"])
	}
}
