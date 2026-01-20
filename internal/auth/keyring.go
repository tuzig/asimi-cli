package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	gokeyring "github.com/zalando/go-keyring"
)

const (
	keyringPrefix = "oauth_"
)

// keyringService is the service name used for keyring operations
// Tests can override this to use a test-specific service
var keyringService = getKeyringService()

func getKeyringService() string {
	// Allow tests to use a separate keyring service to avoid polluting production credentials
	if os.Getenv("ASIMI_KEYRING_SERVICE") != "" {
		return os.Getenv("ASIMI_KEYRING_SERVICE")
	}
	return "dev.asimi.asimi-cli"
}

// SetTestingKeyringService sets a test-specific keyring service name
func SetTestingKeyringService(service string) {
	keyringService = service
}

// TokenData holds OAuth token information
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	Provider     string    `json:"provider"`
}

// SaveTokenToKeyring securely stores OAuth tokens in the OS keyring
func SaveTokenToKeyring(provider, accessToken, refreshToken string, expiry time.Time) error {
	// Validate that we're not saving empty tokens
	if accessToken == "" {
		return fmt.Errorf("cannot save empty access token for provider %s", provider)
	}

	data := TokenData{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       expiry,
		Provider:     provider,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal token data: %w", err)
	}

	key := keyringPrefix + provider
	err = gokeyring.Set(keyringService, key, string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to store token in keyring: %w", err)
	}

	return nil
}

// GetOauthToken retrieves OAuth tokens from environment variable or OS keyring
func GetOauthToken(provider string) (*TokenData, error) {
	var err error
	envVarName := strings.ToUpper(provider) + "_OAUTH_TOKEN"
	rawData := os.Getenv(envVarName)

	if rawData == "" {
		// Fall back to keyring
		key := keyringPrefix + provider
		rawData, err = gokeyring.Get(keyringService, key)
		if err != nil {
			if err == gokeyring.ErrNotFound {
				return nil, nil // Token not found is not an error
			}
			return nil, fmt.Errorf("failed to retrieve token from keyring: %w", err)
		}
	}

	// Try to parse as JSON first
	var data TokenData
	err = json.Unmarshal([]byte(rawData), &data)
	if err == nil {
		return &data, nil
	}

	// If that fails, try base64 decoding first, then parse as JSON
	decoded, err := base64.StdEncoding.DecodeString(rawData)
	if err == nil {
		err = json.Unmarshal(decoded, &data)
		if err == nil {
			return &data, nil
		}
	}

	// If still failing, treat it as a raw access token
	// This provides a fallback for simple usage
	return &TokenData{
		AccessToken: rawData,
		Provider:    provider,
		Expiry:      time.Now().Add(24 * time.Hour), // Give it a reasonable default expiry
	}, nil
}

// GetTokenFromKeyring is an alias for GetOauthToken for backward compatibility
func GetTokenFromKeyring(provider string) (*TokenData, error) {
	return GetOauthToken(provider)
}

// DeleteTokenFromKeyring removes OAuth tokens from the OS keyring on logout
func DeleteTokenFromKeyring(provider string) error {
	key := keyringPrefix + provider
	err := gokeyring.Delete(keyringService, key)
	if err != nil && err != gokeyring.ErrNotFound {
		return fmt.Errorf("failed to delete token from keyring: %w", err)
	}
	return nil
}

// SaveAPIKeyToKeyring securely stores API keys in the OS keyring
func SaveAPIKeyToKeyring(provider, apiKey string) error {
	key := "apikey_" + provider
	err := gokeyring.Set(keyringService, key, apiKey)
	if err != nil {
		return fmt.Errorf("failed to store API key in keyring: %w", err)
	}
	return nil
}

// GetAPIKeyFromKeyring retrieves API keys from the OS keyring
func GetAPIKeyFromKeyring(provider string) (string, error) {
	key := "apikey_" + provider
	apiKey, err := gokeyring.Get(keyringService, key)
	if err != nil {
		if err == gokeyring.ErrNotFound {
			return "", nil // API key not found is not an error
		}
		return "", fmt.Errorf("failed to retrieve API key from keyring: %w", err)
	}
	return apiKey, nil
}

// DeleteAPIKeyFromKeyring removes API keys from the OS keyring
func DeleteAPIKeyFromKeyring(provider string) error {
	key := "apikey_" + provider
	err := gokeyring.Delete(keyringService, key)
	if err != nil && err != gokeyring.ErrNotFound {
		return fmt.Errorf("failed to delete API key from keyring: %w", err)
	}
	return nil
}

// IsTokenExpired checks if the token has expired
func IsTokenExpired(data *TokenData) bool {
	if data == nil {
		return true
	}
	// Add a 5-minute buffer before actual expiry
	return time.Now().After(data.Expiry.Add(-5 * time.Minute))
}
