// Package main provides the main entry point and CLI for asimi.
package main

import (
	"os"
	"time"

	"github.com/afittestide/asimi/internal/keyring"
)

// keyringService is the service name used for keyring operations
// Tests can override this to use a test-specific service
var keyringService = getKeyringService()

func getKeyringService() string {
	if os.Getenv("ASIMI_KEYRING_SERVICE") != "" {
		return os.Getenv("ASIMI_KEYRING_SERVICE")
	}
	return "dev.asimi.asimi-cli"
}

// TokenData is an alias for backward compatibility
type TokenData = keyring.TokenData

// SaveTokenToKeyring delegates to internal/keyring package
func SaveTokenToKeyring(provider, accessToken, refreshToken string, expiry time.Time) error {
	return keyring.SaveToken(provider, accessToken, refreshToken, expiry)
}

// GetOauthToken delegates to internal/keyring package
func GetOauthToken(provider string) (*TokenData, error) {
	return keyring.GetOauthToken(provider)
}

// GetTokenFromKeyring is an alias for GetOauthToken
func GetTokenFromKeyring(provider string) (*TokenData, error) {
	return keyring.GetOauthToken(provider)
}

// DeleteTokenFromKeyring delegates to internal/keyring package
func DeleteTokenFromKeyring(provider string) error {
	return keyring.DeleteToken(provider)
}

// SaveAPIKeyToKeyring delegates to internal/keyring package
func SaveAPIKeyToKeyring(provider, apiKey string) error {
	return keyring.SaveAPIKey(provider, apiKey)
}

// GetAPIKeyFromKeyring delegates to internal/keyring package
func GetAPIKeyFromKeyring(provider string) (string, error) {
	return keyring.GetAPIKey(provider)
}

// DeleteAPIKeyFromKeyring delegates to internal/keyring package
func DeleteAPIKeyFromKeyring(provider string) error {
	return keyring.DeleteAPIKey(provider)
}

// IsTokenExpired checks if the token has expired
func IsTokenExpired(data *TokenData) bool {
	if data == nil {
		return true
	}
	return time.Now().After(data.Expiry.Add(-5 * time.Minute))
}
