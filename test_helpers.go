package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
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

// testStorageSessionData creates a dummy storage.SessionData for testing
func testStorageSessionData(id, prompt string, updated time.Time, messageTexts ...string) *storage.SessionData {
	var messages []schemas.ChatMessage
	for _, text := range messageTexts {
		messages = append(messages, textMessage(schemas.ChatMessageRoleUser, text))
	}

	// Serialize messages to JSON for storage
	messagesJSON, _ := json.Marshal(messages)

	return &storage.SessionData{
		ID:           id,
		FirstPrompt:  prompt,
		LastUpdated:  updated,
		Messages:     messagesJSON,
		MessageCount: len(messages), // Set MessageCount for list views
		Model:        "test",
		CreatedAt:    updated,
		WorkingDir:   "/tmp",
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
