package main

import (
	"os"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

// skipIfNotCI skips tests that alter git state or change working directories.
// These tests can corrupt the local git index when run outside CI.
// Set CI=true environment variable to run these tests.
func skipIfNotCI(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") == "" {
		t.Skip("Skipping git-altering test (set CI=true to run)")
	}
}

func strPtr(s string) *string { return &s }

// textMessage creates a dummy text message for testing
func textMessage(role schemas.ChatMessageRole, text string) schemas.ChatMessage {
	return schemas.ChatMessage{
		Role:    role,
		Content: &schemas.ChatMessageContent{ContentStr: &text},
	}
}
