// Package main provides the main entry point and CLI for asimi.
package main

import (
	"os"

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
