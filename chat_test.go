package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChatComponent_UpdateLastToolCallEmoji(t *testing.T) {
	t.Run("update emoji for matching command", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		// Add a tool call message (format: "<emoji> <description>\n │ $ <command>")
		chat.AddMessage("⚙️ Running command\n │ $ echo hello")

		updated := chat.UpdateLastToolCallEmoji("echo hello", "🙋")

		assert.True(t, updated)
		assert.Contains(t, chat.Messages[len(chat.Messages)-1].Content, "🙋")
		assert.Contains(t, chat.Messages[len(chat.Messages)-1].Content, "$ echo hello")
		assert.NotContains(t, chat.Messages[len(chat.Messages)-1].Content, "⚙️")
	})

	t.Run("update emoji for last matching command when multiple exist", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		// Add multiple tool call messages
		chat.AddMessage("✓ Running command\n │ $ echo first")
		chat.AddMessage("⚙️ Running command\n │ $ echo second")

		updated := chat.UpdateLastToolCallEmoji("echo second", "🙋")

		assert.True(t, updated)
		// First message should be unchanged
		assert.Contains(t, chat.Messages[1].Content, "✓")
		// Last message should be updated
		assert.Contains(t, chat.Messages[2].Content, "🙋")
	})

	t.Run("no update when command not found", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		chat.AddMessage("⚙️ Running command\n │ $ echo hello")

		updated := chat.UpdateLastToolCallEmoji("echo world", "🙋")

		assert.False(t, updated)
		// Original message should be unchanged
		assert.Contains(t, chat.Messages[len(chat.Messages)-1].Content, "⚙️")
	})

	t.Run("no update when messages are empty", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		chat.Messages = []ChatMessage{} // Clear all messages

		updated := chat.UpdateLastToolCallEmoji("echo hello", "🙋")

		assert.False(t, updated)
	})

	t.Run("update to denied emoji", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		chat.AddMessage("🙋 Running command\n │ $ rm -rf /")

		updated := chat.UpdateLastToolCallEmoji("rm -rf /", "⛔︎")

		assert.True(t, updated)
		assert.Contains(t, chat.Messages[len(chat.Messages)-1].Content, "⛔︎")
		assert.NotContains(t, chat.Messages[len(chat.Messages)-1].Content, "🙋")
	})
}
