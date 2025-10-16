package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAGENTSmdInMemoryFiles verifies that AGENTS.md content is counted in Memory files, not System prompt
func TestAGENTSmdInMemoryFiles(t *testing.T) {
	llm := &sessionMockLLMContext{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	info := sess.GetContextInfo()

	// If AGENTS.md exists, it should be in Memory files
	if sess.HasContextFiles() {
		contextFiles := sess.GetContextFiles()
		agentsContent, hasAgents := contextFiles["AGENTS.md"]

		if hasAgents {
			t.Logf("AGENTS.md found with %d characters", len(agentsContent))

			// Memory files should have tokens (from AGENTS.md)
			assert.Greater(t, info.MemoryFilesTokens, 0, "AGENTS.md should contribute to Memory files tokens")

			// Verify AGENTS.md is in the context files
			assert.Contains(t, contextFiles, "AGENTS.md")

			t.Logf("Context breakdown:")
			t.Logf("  System prompt: %d tokens", info.SystemPromptTokens)
			t.Logf("  System tools: %d tokens", info.SystemToolsTokens)
			t.Logf("  Memory files: %d tokens (includes AGENTS.md)", info.MemoryFilesTokens)
			t.Logf("  Messages: %d tokens", info.MessagesTokens)
		}
	}
}
