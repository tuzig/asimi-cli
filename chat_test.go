package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ===== Debounced Chat Render Tests =====

func TestAddAIChunk_SetsDirtyWithoutImmediateUpdate(t *testing.T) {
	chat := NewChatComponent(80, 20, false)
	// Baseline: viewport should have the initial session message
	baseline := chat.Viewport.View()

	chat.AddAIChunk("hello from AI")

	// contentDirty must be true
	assert.True(t, chat.contentDirty, "AddAIChunk should set contentDirty=true")

	// Viewport content should be stale (unchanged from baseline)
	assert.Equal(t, baseline, chat.Viewport.View(),
		"AddAIChunk should NOT immediately update the viewport")

	// After UpdateContent, viewport reflects the new message
	chat.UpdateContent()
	assert.Contains(t, chat.Viewport.View(), "hello from AI",
		"viewport should contain the AI chunk after UpdateContent")
}

func TestAddThinkingChunk_SetsDirtyWithoutImmediateUpdate(t *testing.T) {
	chat := NewChatComponent(80, 20, false)
	baseline := chat.Viewport.View()

	chat.AddThinkingChunk("pondering deeply")

	assert.True(t, chat.contentDirty, "AddThinkingChunk should set contentDirty=true")
	assert.Equal(t, baseline, chat.Viewport.View(),
		"AddThinkingChunk should NOT immediately update the viewport")

	chat.UpdateContent()
	assert.Contains(t, chat.Viewport.View(), "pondering deeply",
		"viewport should contain the thinking chunk after UpdateContent")
}

func TestAddAIChunk_AccumulatesContent(t *testing.T) {
	chat := NewChatComponent(80, 20, false)

	chat.AddAIChunk("first ")
	chat.AddAIChunk("second ")
	chat.AddAIChunk("third")

	// All chunks should be accumulated in the last AI message
	assert.Len(t, chat.Messages, 1, "chunks should append to the same AI message")
	assert.Equal(t, "first second third", chat.Messages[0].Content,
		"chunks should accumulate in order")
	assert.Equal(t, MessageTypeAI, chat.Messages[0].Type)
}

func TestAddAIChunk_CreatesNewMessageAfterDifferentType(t *testing.T) {
	chat := NewChatComponent(80, 20, false)

	chat.AddAIChunk("ai part one")
	chat.AddUserMessage("user interrupt")
	chat.AddAIChunk("ai part two")

	// Should have 3 messages: AI, User, AI
	assert.Len(t, chat.Messages, 3)
	assert.Equal(t, MessageTypeAI, chat.Messages[0].Type)
	assert.Equal(t, MessageTypeUser, chat.Messages[1].Type)
	assert.Equal(t, MessageTypeAI, chat.Messages[2].Type)
	assert.Equal(t, "ai part one", chat.Messages[0].Content)
	assert.Equal(t, "ai part two", chat.Messages[2].Content)
}

func TestUpdateContent_ViewportMatchesMessages(t *testing.T) {
	chat := NewChatComponent(80, 20, false)

	chat.AddAIChunk("chunk1")
	chat.AddAIChunk(" chunk2")

	// Before UpdateContent, viewport is stale
	assert.False(t, strings.Contains(chat.Viewport.View(), "chunk1"),
		"viewport should be stale before UpdateContent")

	chat.UpdateContent()

	// After UpdateContent, viewport contains the accumulated content
	view := chat.Viewport.View()
	assert.True(t, strings.Contains(view, "chunk1"), "viewport should contain 'chunk1'")
	assert.True(t, strings.Contains(view, "chunk2"), "viewport should contain 'chunk2'")
}

func TestContentDirty_FalseAfterUpdateContent(t *testing.T) {
	chat := NewChatComponent(80, 20, false)

	chat.AddAIChunk("dirty data")
	assert.True(t, chat.contentDirty, "should be dirty after AddAIChunk")

	chat.UpdateContent()
	assert.False(t, chat.contentDirty, "contentDirty should be false after UpdateContent")
}

func TestContentDirty_FalseAfterFlushDirty(t *testing.T) {
	chat := NewChatComponent(80, 20, false)

	chat.AddAIChunk("dirty data")
	assert.True(t, chat.contentDirty, "should be dirty after AddAIChunk")

	chat.FlushDirty()
	assert.False(t, chat.contentDirty, "contentDirty should be false after FlushDirty")
	assert.Contains(t, chat.Viewport.View(), "dirty data",
		"viewport should reflect content after FlushDirty")
}

func TestAddMessage_SynchronousUpdate_BackwardCompatibility(t *testing.T) {
	chat := NewChatComponent(80, 20, false)

	chat.AddMessage("sync message")

	// AddMessage calls UpdateContent synchronously, so contentDirty should remain false
	assert.False(t, chat.contentDirty,
		"AddMessage should not leave contentDirty=true")

	// Viewport should already reflect the message (no deferred render needed)
	assert.Contains(t, chat.Viewport.View(), "sync message",
		"AddMessage should synchronously update the viewport")
}

func TestAddUserMessage_SynchronousUpdate_BackwardCompatibility(t *testing.T) {
	chat := NewChatComponent(80, 20, false)

	chat.AddUserMessage("user says hi")

	assert.False(t, chat.contentDirty,
		"AddUserMessage should not leave contentDirty=true")
	assert.Contains(t, chat.Viewport.View(), "user says hi",
		"AddUserMessage should synchronously update the viewport")
}

func TestAddThinkingChunk_SkipsEmptyChunks(t *testing.T) {
	chat := NewChatComponent(80, 20, false)
	msgCountBefore := len(chat.Messages)

	chat.AddThinkingChunk("   ")
	assert.False(t, chat.contentDirty,
		"whitespace-only thinking chunks should not mark content dirty")
	assert.Len(t, chat.Messages, msgCountBefore,
		"no new message should be added for empty thinking chunks")
}

// ===== Clear() Tests =====

func TestClear_ResetsMessagesAndState(t *testing.T) {
	chat := NewChatComponent(80, 20, false)
	chat.AddUserMessage("some content")
	chat.AddMessage("another message")
	chat.AutoScroll = false
	chat.UserScrolled = true
	chat.ScrollLocked = true

	chat.Clear()

	assert.Empty(t, chat.Messages, "Clear should empty Messages")
	assert.True(t, chat.AutoScroll, "Clear should reset AutoScroll to true")
	assert.False(t, chat.UserScrolled, "Clear should reset UserScrolled")
	assert.False(t, chat.ScrollLocked, "Clear should reset ScrollLocked")
	assert.Empty(t, chat.rawSessionHistory, "Clear should clear rawSessionHistory")
	assert.Empty(t, chat.toolCallMessageIndex, "Clear should clear toolCallMessageIndex")
}

func TestClear_SynchronouslyUpdatesViewport(t *testing.T) {
	chat := NewChatComponent(80, 20, false)
	chat.AddUserMessage("this should disappear after Clear")

	chat.Clear()

	// Clear() now calls UpdateContent(), so viewport should reflect the empty state
	assert.False(t, chat.contentDirty,
		"Clear should not leave contentDirty=true — it calls UpdateContent()")
	assert.False(t, strings.Contains(chat.Viewport.View(), "this should disappear after Clear"),
		"viewport should not contain old messages after Clear")
}

func TestClear_ViewportAtTop(t *testing.T) {
	chat := NewChatComponent(80, 20, false)
	// Add enough messages to scroll
	for i := 0; i < 50; i++ {
		chat.AddMessage("line " + string(rune('A'+i%26)))
	}
	chat.Viewport.GotoBottom()

	chat.Clear()

	assert.True(t, chat.Viewport.AtTop(),
		"Clear should scroll viewport to top via GotoTop")
}

func TestClear_AllowsSubsequentMessages(t *testing.T) {
	chat := NewChatComponent(80, 20, false)
	chat.AddUserMessage("before clear")
	chat.Clear()
	chat.AddUserMessage("after clear")

	assert.Len(t, chat.Messages, 1, "should have exactly one message after Clear + AddUserMessage")
	assert.Equal(t, "after clear", chat.Messages[0].Content)
	assert.Contains(t, chat.Viewport.View(), "after clear",
		"viewport should show new messages after Clear")
}

// ===== Original Tests =====

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
