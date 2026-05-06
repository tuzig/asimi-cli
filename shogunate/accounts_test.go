package shogunate

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/keyring"
	"github.com/maximhq/bifrost/core/schemas"
)

func TestHasAWSEnvCredentials(t *testing.T) {
	tests := []struct {
		name          string
		accessKeyID   string
		secretAccess  string
		expectTrue    bool
	}{
		{
			name:        "both credentials set",
			accessKeyID: "AKIAIOSFODNN7EXAMPLE",
			secretAccess: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			expectTrue:  true,
		},
		{
			name:        "only access key set",
			accessKeyID: "AKIAIOSFODNN7EXAMPLE",
			secretAccess: "",
			expectTrue:  false,
		},
		{
			name:        "only secret key set",
			accessKeyID: "",
			secretAccess: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			expectTrue:  false,
		},
		{
			name:        "neither set",
			accessKeyID: "",
			secretAccess: "",
			expectTrue:  false,
		},
		{
			name:        "both empty strings",
			accessKeyID: "",
			secretAccess: "",
			expectTrue:  false,
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

	// Should include Bedrock when AWS credentials are set
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

func TestGetKeysForProvider_Bedrock_WithAllEnvVars(t *testing.T) {
	// Save original env vars
	origAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	origSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	origRegion := os.Getenv("AWS_REGION")
	origSessionToken := os.Getenv("AWS_SESSION_TOKEN")
	defer func() {
		os.Setenv("AWS_ACCESS_KEY_ID", origAccessKey)
		os.Setenv("AWS_SECRET_ACCESS_KEY", origSecretKey)
		os.Setenv("AWS_REGION", origRegion)
		os.Setenv("AWS_SESSION_TOKEN", origSessionToken)
	}()

	// Set all AWS credentials
	os.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	os.Setenv("AWS_REGION", "us-east-1")
	os.Setenv("AWS_SESSION_TOKEN", "FwoGRzQwMUISqD+pAEXAMPLE")

	account := &Account{}
	keys, err := account.GetKeysForProvider(context.Background(), schemas.Bedrock)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}

	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	key := keys[0]
	if key.ID != "bedrock_aws" {
		t.Errorf("expected key ID 'bedrock_aws', got %q", key.ID)
	}
	if key.Name != "AWS Credentials" {
		t.Errorf("expected key Name 'AWS Credentials', got %q", key.Name)
	}
	if key.Value.Val != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("expected access key in Value.Val, got %q", key.Value.Val)
	}
	if key.BedrockKeyConfig == nil {
		t.Fatal("expected BedrockKeyConfig to be set")
	}
	if key.BedrockKeyConfig.Region == nil || key.BedrockKeyConfig.Region.Val != "us-east-1" {
		t.Errorf("expected region 'us-east-1', got %v", key.BedrockKeyConfig.Region)
	}
	if key.BedrockKeyConfig.SecretKey.Val != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("expected secret key in BedrockKeyConfig.SecretKey")
	}
	if key.BedrockKeyConfig.SessionToken == nil || key.BedrockKeyConfig.SessionToken.Val != "FwoGRzQwMUISqD+pAEXAMPLE" {
		t.Errorf("expected session token in BedrockKeyConfig.SessionToken")
	}
	if key.Enabled == nil || !*key.Enabled {
		t.Error("expected Enabled to be true")
	}
}

func TestGetKeysForProvider_Bedrock_WithOnlyRequiredEnvVars(t *testing.T) {
	// Save original env vars
	origAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	origSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	origRegion := os.Getenv("AWS_REGION")
	origSessionToken := os.Getenv("AWS_SESSION_TOKEN")
	defer func() {
		os.Setenv("AWS_ACCESS_KEY_ID", origAccessKey)
		os.Setenv("AWS_SECRET_ACCESS_KEY", origSecretKey)
		os.Setenv("AWS_REGION", origRegion)
		os.Setenv("AWS_SESSION_TOKEN", origSessionToken)
	}()

	// Set only required credentials (no region, no session token)
	os.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	os.Unsetenv("AWS_REGION")
	os.Unsetenv("AWS_SESSION_TOKEN")

	account := &Account{}
	keys, err := account.GetKeysForProvider(context.Background(), schemas.Bedrock)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}

	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	key := keys[0]
	if key.BedrockKeyConfig == nil {
		t.Fatal("expected BedrockKeyConfig to be set")
	}
	if key.BedrockKeyConfig.Region != nil {
		t.Error("expected region to be nil when AWS_REGION is not set")
	}
	if key.BedrockKeyConfig.SessionToken != nil {
		t.Error("expected session token to be nil when AWS_SESSION_TOKEN is not set")
	}
}

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

func TestGetKeysForProvider_OtherProvidersStillWork(t *testing.T) {
	// Save original env vars
	origOpenAI := os.Getenv("OPENAI_API_KEY")
	defer func() {
		os.Setenv("OPENAI_API_KEY", origOpenAI)
	}()

	// Set OpenAI API key
	os.Setenv("OPENAI_API_KEY", "sk-test-openai-key")

	account := &Account{}
	keys, err := account.GetKeysForProvider(context.Background(), schemas.OpenAI)
	if err != nil {
		t.Fatalf("GetKeysForProvider() error = %v", err)
	}

	if len(keys) == 0 {
		t.Error("expected at least 1 key for OpenAI")
	}
}

// TestAccountImplementsSchemaAccount ensures the Account type implements schemas.Account
func TestAccountImplementsSchemaAccount(t *testing.T) {
	var _ schemas.Account = &Account{}
}

// TestGetConfigForProvider tests the config provider
func TestGetConfigForProvider(t *testing.T) {
	account := NewAccount(30, 60, "")
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

// TestIsTokenExpired tests token expiration logic
func TestIsTokenExpired(t *testing.T) {
	tests := []struct {
		name     string
		data     *keyring.TokenData
		expected bool
	}{
		{
			name:     "nil token",
			data:     nil,
			expected: true,
		},
		{
			name: "future token",
			data: &keyring.TokenData{
				Expiry: time.Now().Add(1 * time.Hour),
			},
			expected: false,
		},
		{
			name: "expired token",
			data: &keyring.TokenData{
				Expiry: time.Now().Add(-1 * time.Hour),
			},
			expected: true,
		},
		{
			name: "token expiring soon (within 5 min buffer)",
			data: &keyring.TokenData{
				Expiry: time.Now().Add(3 * time.Minute),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTokenExpired(tt.data)
			if result != tt.expected {
				t.Errorf("IsTokenExpired() = %v, want %v", result, tt.expected)
			}
		})
	}
}
