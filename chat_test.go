package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChatComponent_StartBlock(t *testing.T) {
	t.Run("starts block with height limit", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		// Start with one message
		chat.AddMessage("header message")

		// Start a block with height limit of 5
		// Block records len(messages)-1 = 0
		chat.StartBlock(5)

		assert.Len(t, chat.blockLines, 1)
		assert.Equal(t, 0, chat.blockLines[0][0]) // Starting line index
		assert.Equal(t, 5, chat.blockLines[0][1]) // Height limit
	})

	t.Run("starts block with unlimited height (0)", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		chat.AddMessage("first")

		// Start an unlimited block
		chat.StartBlock(0)

		assert.Len(t, chat.blockLines, 1)
		assert.Equal(t, 0, chat.blockLines[0][0])
		assert.Equal(t, 0, chat.blockLines[0][1]) // Unlimited
	})

	t.Run("multiple blocks with different heights", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)

		// First block: after 2 messages, block starts at index 1
		chat.AddMessage("block1-msg1")
		chat.AddMessage("block1-msg2")
		chat.StartBlock(3)

		// Second block: after 4 total messages, block starts at index 3
		chat.AddMessage("block2-msg1")
		chat.AddMessage("block2-msg2")
		chat.StartBlock(5)

		assert.Len(t, chat.blockLines, 2)
		// First block starts after 2 messages (at index 1)
		assert.Equal(t, 1, chat.blockLines[0][0])
		assert.Equal(t, 3, chat.blockLines[0][1])
		// Second block starts after 4 messages (at index 3)
		assert.Equal(t, 3, chat.blockLines[1][0])
		assert.Equal(t, 5, chat.blockLines[1][1])
	})

	t.Run("start block on empty chat", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		// No messages added yet
		chat.StartBlock(10)

		assert.Len(t, chat.blockLines, 1)
		assert.Equal(t, -1, chat.blockLines[0][0]) // No messages yet, so -1
		assert.Equal(t, 10, chat.blockLines[0][1])
	})
}
