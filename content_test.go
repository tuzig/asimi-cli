package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTabGreetingsAllPresent(t *testing.T) {
	for _, target := range []string{"chancellor", "sage", "forge", "judge"} {
		g, ok := tabGreetings[target]
		if !ok || g == "" {
			t.Errorf("missing greeting for tab %q", target)
		}
	}
}

func TestAddGreetingMessage_UsesGreetingType(t *testing.T) {
	chat := NewChatComponent(80, 20, true)

	chat.AddGreetingMessage("Welcome to the Court")

	assert.Len(t, chat.Messages, 1, "should have exactly one greeting message")
	assert.Equal(t, MessageTypeGreeting, chat.Messages[0].Type,
		"greeting message should have MessageTypeGreeting type")
	assert.Equal(t, "Welcome to the Court", chat.Messages[0].Content,
		"greeting content should be stored without prefix")
}

func TestAddGreetingMessage_RendersWithCastleEmoji(t *testing.T) {
	chat := NewChatComponent(80, 20, true)

	chat.AddGreetingMessage("Welcome, Ruler")

	view := chat.Viewport.View()
	assert.True(t, strings.Contains(view, "🏯"),
		"rendered greeting should contain the 🏯 prefix emoji")
}

func TestAddGreetingMessage_NoSystemPrefix(t *testing.T) {
	chat := NewChatComponent(80, 20, true)

	chat.AddGreetingMessage("Greetings, Ruler")

	view := chat.Viewport.View()
	// The old rendering used systemPrefix ("🛠️  ") — greetings should no longer include it
	assert.False(t, strings.Contains(view, "🛠️"),
		"greeting should not contain the system/tool prefix 🛠️")
}

func TestAddGreetingMessage_MarkdownRendered(t *testing.T) {
	chat := NewChatComponent(80, 20, true)

	// Use markdown bold in the greeting
	chat.AddGreetingMessage("Welcome **Ruler** to the Court")

	view := chat.Viewport.View()
	// Markdown rendering processes the content — the greeting type ensures
	// renderMarkdown is called, which strips raw markdown markers or applies
	// ANSI styling. The view should contain the text regardless.
	assert.True(t, strings.Contains(view, "Ruler"),
		"rendered greeting should contain 'Ruler' text")
	// The greeting should NOT appear as plain system-styled cyan text
	// (that's the default for MessageTypeSystem) — it should have its own style
	assert.True(t, chat.Messages[0].Type == MessageTypeGreeting,
		"greeting message type ensures markdown rendering path")
}

func TestAddGreetingMessage_SetsAutoScroll(t *testing.T) {
	chat := NewChatComponent(80, 20, true)
	chat.AutoScroll = false
	chat.UserScrolled = true

	chat.AddGreetingMessage("Welcome")

	assert.True(t, chat.AutoScroll, "AddGreetingMessage should enable AutoScroll")
	assert.False(t, chat.UserScrolled, "AddGreetingMessage should reset UserScrolled")
}

func TestAddGreetingMessage_RespectsScrollLock(t *testing.T) {
	chat := NewChatComponent(80, 20, true)
	chat.ScrollLocked = true
	chat.AutoScroll = false
	chat.UserScrolled = true

	chat.AddGreetingMessage("Welcome")

	assert.False(t, chat.AutoScroll, "AddGreetingMessage should not change AutoScroll when scroll-locked")
	assert.True(t, chat.UserScrolled, "AddGreetingMessage should not reset UserScrolled when scroll-locked")
}
