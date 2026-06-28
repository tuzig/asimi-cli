// Package keyring provides OS keyring access for credential storage.
package keyring

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	gokeyring "github.com/zalando/go-keyring"
)

const prefix = "oauth_"

var service = getService()

func getService() string {
	if os.Getenv("ASIMI_KEYRING_SERVICE") != "" {
		return os.Getenv("ASIMI_KEYRING_SERVICE")
	}
	return "dev.asimi.asimi-cli"
}

// TokenData holds OAuth token information
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	Provider     string    `json:"provider"`
}

// MockKeyring replaces the OS keyring with an in-memory mock.
// This should only be called from tests to avoid triggering OS keychain prompts.
func MockKeyring() {
	gokeyring.MockInit()
}

// SaveToken stores OAuth tokens in the OS keyring
func SaveToken(provider, accessToken, refreshToken string, expiry time.Time) error {
	if accessToken == "" {
		return fmt.Errorf("cannot save empty access token for provider %s", provider)
	}

	data := TokenData{AccessToken: accessToken, RefreshToken: refreshToken, Expiry: expiry, Provider: provider}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal token data: %w", err)
	}

	gErr := gokeyring.Set(service, prefix+provider, string(jsonData))
	if gErr != nil {
		return fmt.Errorf("failed to store token in keyring: %w", gErr)
	}
	return nil
}

// GetOauthToken retrieves OAuth tokens from environment variable or OS keyring
func GetOauthToken(provider string) (*TokenData, error) {
	envVarName := strings.ToUpper(provider) + "_OAUTH_TOKEN"
	rawData := os.Getenv(envVarName)

	if rawData == "" {
		key := prefix + provider
		kData, kErr := gokeyring.Get(service, key)
		if kErr != nil {
			if kErr == gokeyring.ErrNotFound {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to retrieve token from keyring: %w", kErr)
		}
		rawData = kData
	}

	var data TokenData
	if uErr := json.Unmarshal([]byte(rawData), &data); uErr == nil {
		return &data, nil
	}

	decoded, dErr := base64.StdEncoding.DecodeString(rawData)
	if dErr == nil {
		if uErr := json.Unmarshal(decoded, &data); uErr == nil {
			return &data, nil
		}
	}

	return &TokenData{AccessToken: rawData, Provider: provider, Expiry: time.Now().Add(24 * time.Hour)}, nil
}

// DeleteToken removes OAuth tokens from the OS keyring
func DeleteToken(provider string) error {
	dErr := gokeyring.Delete(service, prefix+provider)
	if dErr != nil && dErr != gokeyring.ErrNotFound {
		return fmt.Errorf("failed to delete token from keyring: %w", dErr)
	}
	return nil
}

// SaveAPIKey stores API keys in the OS keyring
func SaveAPIKey(provider, apiKey string) error {
	sErr := gokeyring.Set(service, "apikey_"+provider, apiKey)
	if sErr != nil {
		return fmt.Errorf("failed to store API key in keyring: %w", sErr)
	}
	return nil
}

// GetAPIKey retrieves API keys from the OS keyring
func GetAPIKey(provider string) (string, error) {
	key := "apikey_" + provider
	kData, kErr := gokeyring.Get(service, key)
	if kErr != nil {
		if kErr == gokeyring.ErrNotFound {
			return "", nil
		}
		return "", fmt.Errorf("failed to retrieve API key from keyring: %w", kErr)
	}
	return kData, nil
}

// DeleteAPIKey removes API keys from the OS keyring
func DeleteAPIKey(provider string) error {
	dErr := gokeyring.Delete(service, "apikey_"+provider)
	if dErr != nil && dErr != gokeyring.ErrNotFound {
		return fmt.Errorf("failed to delete API key from keyring: %w", dErr)
	}
	return nil
}
