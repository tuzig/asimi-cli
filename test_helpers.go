package main

import (
	"os"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
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

// testMainSession creates a dummy main.Session for testing
func testMainSession(id, prompt string, updated time.Time, messageTexts ...string) *Session {
	var messages []llms.MessageContent
	for _, text := range messageTexts {
		messages = append(messages, textMessage(llms.ChatMessageTypeHuman, text))
	}

	return &Session{
		ID:           id,
		FirstPrompt:  prompt,
		LastUpdated:  updated,
		Messages:     messages,
		MessageCount: len(messages), // Set MessageCount for list views
		Model:        "test",
		CreatedAt:    updated,
		WorkingDir:   "/tmp",
	}
}

// testStorageSessionData creates a dummy storage.SessionData for testing
func testStorageSessionData(id, prompt string, updated time.Time, messageTexts ...string) *storage.SessionData {
	var messages []llms.MessageContent
	for _, text := range messageTexts {
		messages = append(messages, textMessage(llms.ChatMessageTypeHuman, text))
	}

	return &storage.SessionData{
		ID:           id,
		FirstPrompt:  prompt,
		LastUpdated:  updated,
		Messages:     messages,
		MessageCount: len(messages), // Set MessageCount for list views
		Model:        "test",
		CreatedAt:    updated,
		WorkingDir:   "/tmp",
	}
}

// textMessage creates a dummy text message for testing
func textMessage(role llms.ChatMessageType, text string) llms.MessageContent {
	return llms.MessageContent{
		Role: role,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: text},
		},
	}
}
