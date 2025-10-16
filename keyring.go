package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "asimi-cli"
	keyringPrefix  = "oauth_"
)

// TokenData holds OAuth token information
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	Provider     string    `json:"provider"`
}

// SaveTokenToKeyring securely stores OAuth tokens in the OS keyring
func SaveTokenToKeyring(provider, accessToken, refreshToken string, expiry time.Time) error {
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
	err = keyring.Set(keyringService, key, string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to store token in keyring: %w", err)
	}

	return nil
}

// GetTokenFromKeyring retrieves OAuth tokens from the OS keyring
func GetTokenFromKeyring(provider string) (*TokenData, error) {
	key := keyringPrefix + provider
	jsonData, err := keyring.Get(keyringService, key)
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil, nil // Token not found is not an error
		}
		return nil, fmt.Errorf("failed to retrieve token from keyring: %w", err)
	}

	var data TokenData
	err = json.Unmarshal([]byte(jsonData), &data)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal token data: %w", err)
	}

	return &data, nil
}

// DeleteTokenFromKeyring removes OAuth tokens from the OS keyring
func DeleteTokenFromKeyring(provider string) error {
	key := keyringPrefix + provider
	err := keyring.Delete(keyringService, key)
	if err != nil && err != keyring.ErrNotFound {
		return fmt.Errorf("failed to delete token from keyring: %w", err)
	}
	return nil
}

// SaveAPIKeyToKeyring securely stores API keys in the OS keyring
func SaveAPIKeyToKeyring(provider, apiKey string) error {
	key := "apikey_" + provider
	err := keyring.Set(keyringService, key, apiKey)
	if err != nil {
		return fmt.Errorf("failed to store API key in keyring: %w", err)
	}
	return nil
}

// GetAPIKeyFromKeyring retrieves API keys from the OS keyring
func GetAPIKeyFromKeyring(provider string) (string, error) {
	key := "apikey_" + provider
	apiKey, err := keyring.Get(keyringService, key)
	if err != nil {
		if err == keyring.ErrNotFound {
			return "", nil // API key not found is not an error
		}
		return "", fmt.Errorf("failed to retrieve API key from keyring: %w", err)
	}
	return apiKey, nil
}

// DeleteAPIKeyFromKeyring removes API keys from the OS keyring
func DeleteAPIKeyFromKeyring(provider string) error {
	key := "apikey_" + provider
	err := keyring.Delete(keyringService, key)
	if err != nil && err != keyring.ErrNotFound {
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
