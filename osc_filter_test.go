package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== Cached Markdown Renderer Tests (Edict 697) =====

func TestCachedRenderer_ReusedAcrossComponents(t *testing.T) {
	// Reset the cache to ensure a clean state
	cachedMarkdownRenderer = nil

	// First call creates and caches the renderer
	chat1 := NewChatComponent(80, 20, true)
	require.NotNil(t, chat1.markdownRenderer, "first chat should have a renderer")
	require.NotNil(t, cachedMarkdownRenderer, "renderer should be cached after first creation")
	firstRenderer := cachedMarkdownRenderer

	// Second call should reuse the cached renderer, not create a new one
	chat2 := NewChatComponent(80, 20, true)
	assert.Same(t, firstRenderer, chat2.markdownRenderer,
		"second chat should reuse the cached renderer")
	assert.Same(t, firstRenderer, cachedMarkdownRenderer,
		"cached renderer should not change")

	// Clean up
	cachedMarkdownRenderer = nil
}

func TestCachedRenderer_NotUsedWhenMarkdownDisabled(t *testing.T) {
	cachedMarkdownRenderer = nil

	chat := NewChatComponent(80, 20, false)
	assert.Nil(t, chat.markdownRenderer, "renderer should be nil when markdown is disabled")
	assert.Nil(t, cachedMarkdownRenderer, "cache should remain nil when markdown is disabled")

	cachedMarkdownRenderer = nil
}

// ===== OSC Response Filter Tests (Edict 697) =====
// These verify that terminal escape-sequence responses (OSC 11 background
// color queries, CSI cursor position reports) are filtered out in
// handleKeyMsg so they don't appear as garbage in the prompt.

func TestHandleKeyMsg_FiltersOSCBackgroundResponse_BG(t *testing.T) {
	model := newTestModel(t)
	model.tabs.DismissWelcome()

	// Simulate the truncated OSC 11 response: "bg:1e1e/1e1e/1e1e"
	// (the leading 'r' from 'rgb:' is consumed by the escape parser)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bg:1e1e/1e1e/1e1e")}

	newModel, cmd := model.handleKeyMsg(msg)
	updated, ok := newModel.(TUIModel)
	require.True(t, ok)

	assert.NotContains(t, updated.prompt().Value(), "bg:1e1e",
		"OSC 11 response (bg:) should be filtered out")
	assert.Nil(t, cmd, "filtered key should produce no command")
}

func TestHandleKeyMsg_FiltersOSCFullRGBResponse(t *testing.T) {
	model := newTestModel(t)
	model.tabs.DismissWelcome()

	// Full OSC 11 response: "]11;rgb:1e1e/1e1e/1e1e"
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]11;rgb:1e1e/1e1e/1e1e")}

	newModel, cmd := model.handleKeyMsg(msg)
	updated, ok := newModel.(TUIModel)
	require.True(t, ok)

	assert.NotContains(t, updated.prompt().Value(), "rgb:",
		"OSC 11 response (rgb:) should be filtered out")
	assert.Nil(t, cmd, "filtered key should produce no command")
}

func TestHandleKeyMsg_FiltersCSICursorPositionResponse(t *testing.T) {
	model := newTestModel(t)
	model.tabs.DismissWelcome()

	// CSI cursor position response: [1;1R
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[1;1R")}

	newModel, cmd := model.handleKeyMsg(msg)
	updated, ok := newModel.(TUIModel)
	require.True(t, ok)

	assert.NotContains(t, updated.prompt().Value(), "1;1R",
		"CSI cursor position response should be filtered out")
	assert.Nil(t, cmd, "filtered key should produce no command")
}

func TestHandleKeyMsg_AllowsNormalEscapeKey(t *testing.T) {
	model := newTestModel(t)
	model.tabs.DismissWelcome()

	// Normal escape key should pass through the filter
	msg := tea.KeyMsg{Type: tea.KeyEsc}

	newModel, _ := model.handleKeyMsg(msg)
	_, ok := newModel.(TUIModel)
	require.True(t, ok, "normal escape key should be handled normally")
}

func TestHandleKeyMsg_AllowsNormalTyping(t *testing.T) {
	model := newTestModel(t)
	model.tabs.DismissWelcome()
	model.prompt().EnterViInsertMode()

	// Normal text input should pass through
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")}

	newModel, _ := model.handleKeyMsg(msg)
	updated, ok := newModel.(TUIModel)
	require.True(t, ok)

	assert.Contains(t, updated.prompt().Value(), "hello",
		"normal text input should not be filtered")
}

func TestHandleKeyMsg_FiltersEscapeSequenceResponse(t *testing.T) {
	model := newTestModel(t)
	model.tabs.DismissWelcome()

	// A key containing escape char \x1b that's longer than 3 chars
	// and is NOT "esc"/"escape" should be filtered
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b]11;rgb:aa/bb/cc")}

	newModel, cmd := model.handleKeyMsg(msg)
	updated, ok := newModel.(TUIModel)
	require.True(t, ok)

	assert.NotContains(t, updated.prompt().Value(), "rgb:",
		"escape sequence response should be filtered out")
	assert.Nil(t, cmd, "filtered key should produce no command")
}
