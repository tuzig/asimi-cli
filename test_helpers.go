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

// drainResumePager drives the progressive session-resume pager to completion
// synchronously. Tests that call handleSessionSelected directly and assert on
// the resulting chat/pending-prompt state must drain the pager so the whole
// history (and any deferred final-batch work) is applied.
func drainResumePager(model *TUIModel) {
	guard := 0
	for model.resumeRebuildActive && guard < 1000 {
		model.handleResumeRebuildBatch(resumeRebuildBatchMsg{
			messages: model.resumeRebuildMessages,
			cursor:   model.resumeRebuildCursor,
		})
		guard++
	}
}
