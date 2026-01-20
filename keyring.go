package main

import (
	"time"

	"github.com/afittestide/asimi/internal/auth"
)

// TokenData holds OAuth token information
// This is an alias to the internal auth package type for backward compatibility
type TokenData = auth.TokenData

// SetTestingKeyringService sets a test-specific keyring service name
func SetTestingKeyringService(service string) {
	auth.SetTestingKeyringService(service)
}

// SaveTokenToKeyring securely stores OAuth tokens in the OS keyring
func SaveTokenToKeyring(provider, accessToken, refreshToken string, expiry time.Time) error {
	return auth.SaveTokenToKeyring(provider, accessToken, refreshToken, expiry)
}

// GetOauthToken retrieves OAuth tokens from environment variable or OS keyring
func GetOauthToken(provider string) (*TokenData, error) {
	return auth.GetOauthToken(provider)
}

// GetTokenFromKeyring is an alias for GetOauthToken for backward compatibility
func GetTokenFromKeyring(provider string) (*TokenData, error) {
	return auth.GetTokenFromKeyring(provider)
}

// DeleteTokenFromKeyring removes OAuth tokens from the OS keyring on logout
func DeleteTokenFromKeyring(provider string) error {
	return auth.DeleteTokenFromKeyring(provider)
}

// SaveAPIKeyToKeyring securely stores API keys in the OS keyring
func SaveAPIKeyToKeyring(provider, apiKey string) error {
	return auth.SaveAPIKeyToKeyring(provider, apiKey)
}

// GetAPIKeyFromKeyring retrieves API keys from the OS keyring
func GetAPIKeyFromKeyring(provider string) (string, error) {
	return auth.GetAPIKeyFromKeyring(provider)
}

// DeleteAPIKeyFromKeyring removes API keys from the OS keyring
func DeleteAPIKeyFromKeyring(provider string) error {
	return auth.DeleteAPIKeyFromKeyring(provider)
}

// IsTokenExpired checks if the token has expired
func IsTokenExpired(data *TokenData) bool {
	return auth.IsTokenExpired(data)
}
