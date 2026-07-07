// Package main provides the main entry point and CLI for asimi.
package main

import (
	"github.com/afittestide/asimi/internal/keyring"
)

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
