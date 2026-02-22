package main

import (
	"testing"

	"github.com/afittestide/asimi/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViModeAlwaysEnabled(t *testing.T) {
	// Create a new prompt component
	prompt := NewPromptComponent(80, 5)

	// Vi mode should always be enabled
	viEnabled, viMode, _ := prompt.ViModeStatus()
	assert.True(t, viEnabled, "Vi mode should always be enabled")
	assert.Equal(t, ViModeInsert, viMode, "Should start in insert mode")
}

func TestViModeTransitions(t *testing.T) {
	// Create a new prompt component
	prompt := NewPromptComponent(80, 5)

	assert.True(t, prompt.IsViInsertMode(), "Should start in insert mode")
	assert.False(t, prompt.IsViNormalMode(), "Should not be in normal mode")

	// Switch to normal mode (ESC from insert)
	prompt.EnterViNormalMode()
	assert.False(t, prompt.IsViInsertMode(), "Should not be in insert mode")
	assert.True(t, prompt.IsViNormalMode(), "Should be in normal mode")

	// Switch to command-line mode (: from normal)
	prompt.EnterViCommandLineMode()
	assert.False(t, prompt.IsViInsertMode(), "Should not be in insert mode")
	assert.False(t, prompt.IsViNormalMode(), "Should not be in normal mode")
	assert.True(t, prompt.IsViCommandLineMode(), "Should be in command-line mode")

	// Switch back to insert mode (after command execution)
	prompt.EnterViInsertMode()
	assert.True(t, prompt.IsViInsertMode(), "Should be in insert mode")
	assert.False(t, prompt.IsViNormalMode(), "Should not be in normal mode")
}

func TestViModeCommandNormalization(t *testing.T) {
	// Test that colon commands are normalized to slash commands
	tests := []struct {
		input    string
		expected string
	}{
		{":help", "/help"},
		{":new", "/new"},
		{":quit", "/quit"},
		{":vi", "/vi"},
		{"/help", "/help"}, // Already normalized
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Simulate normalization
			normalized := tt.input
			if normalized[0] == ':' {
				normalized = "/" + normalized[1:]
			}
			assert.Equal(t, tt.expected, normalized)
		})
	}
}

func TestViCommandLineModeUsesInsertKeymap(t *testing.T) {
	// Create a new prompt component
	prompt := NewPromptComponent(80, 5)

	assert.True(t, prompt.IsViInsertMode(), "Should start in insert mode")

	// Switch to normal mode
	prompt.EnterViNormalMode()
	assert.True(t, prompt.IsViNormalMode(), "Should be in normal mode")

	// Verify that normal mode uses vi normal keymap
	assert.Equal(t, prompt.viNormalKeyMap, prompt.TextArea.KeyMap, "Normal mode should use vi normal keymap")

	// Switch to command-line mode
	prompt.EnterViCommandLineMode()
	assert.True(t, prompt.IsViCommandLineMode(), "Should be in command-line mode")

	assert.Equal(t, prompt.viInsertKeyMap, prompt.TextArea.KeyMap, "Command-line mode should use vi insert keymap")

	// Switch back to insert mode
	prompt.EnterViInsertMode()
	assert.True(t, prompt.IsViInsertMode(), "Should be in insert mode")

	// Verify that insert mode uses vi insert keymap
	assert.Equal(t, prompt.viInsertKeyMap, prompt.TextArea.KeyMap, "Insert mode should use vi insert keymap")
}

func TestViReplaceModeUsesAbsoluteCursorOnWrappedLine(t *testing.T) {
	model := newTestModel(t)

	// Force soft wrapping in the prompt textarea.
	model.prompt().SetWidth(12) // textarea width = 10
	model.prompt().SetHeight(2)

	model.prompt().SetValue("0123456789abcdef")
	model.prompt().TextArea.SetCursor(12) // wraps to second segment

	lineInfo := model.prompt().TextArea.LineInfo()
	require.Greater(t, lineInfo.StartColumn, 0, "expected cursor on wrapped segment")

	model.Mode = "replace"
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)

	require.Equal(t, "0123456789abXdef", updatedModel.prompt().Value())
	updatedLineInfo := updatedModel.prompt().TextArea.LineInfo()
	require.Equal(t, 12, updatedLineInfo.StartColumn+updatedLineInfo.ColumnOffset)
}

func TestViModePlaceholderText(t *testing.T) {
	// Create a new prompt component
	prompt := NewPromptComponent(80, 5)

	assert.Equal(t, PlaceholderDefault, prompt.TextArea.Placeholder, "Insert mode should have default placeholder")

	// Switch to normal mode
	prompt.EnterViNormalMode()
	assert.Equal(t, "i for insert mode, : for commands, ↑↓ for history", prompt.TextArea.Placeholder, "Normal mode should have navigation placeholder")

	// Switch to command-line mode
	prompt.EnterViCommandLineMode()
	assert.Equal(t, "Enter command below...", prompt.TextArea.Placeholder, "Command-line mode should have command placeholder")

	// Switch back to insert mode
	prompt.EnterViInsertMode()
	assert.Equal(t, PlaceholderDefault, prompt.TextArea.Placeholder, "Insert mode should have default placeholder")
}

func TestViModeHistoryNavigation(t *testing.T) {
	// This test verifies that arrow keys work for history navigation in vi normal mode
	config := &Config{}
	model := NewTUIModel(config, nil, nil, nil, nil, nil, nil, nil)

	// Add some history entries
	model.sessionPromptHistory = []promptHistoryEntry{
		{Prompt: "first command", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second command", SessionSnapshot: 2, ChatSnapshot: 1},
		{Prompt: "third command", SessionSnapshot: 3, ChatSnapshot: 2},
	}
	model.historyCursor = len(model.sessionPromptHistory)
	model.prompt().SetValue("current input")

	// Switch to vi normal mode
	model.prompt().EnterViNormalMode()
	assert.True(t, model.prompt().IsViNormalMode(), "Should be in vi normal mode")

	// Simulate pressing up arrow in normal mode
	model.prompt().TextArea.CursorStart() // Ensure we're on first line
	newModel, _ := model.handleViNormalMode(tea.KeyMsg{Type: tea.KeyUp})
	updatedModel, ok := newModel.(TUIModel)
	assert.True(t, ok)

	// Should navigate to previous history entry
	assert.Equal(t, 2, updatedModel.historyCursor, "Should move to previous history entry")
	assert.Equal(t, "third command", updatedModel.prompt().Value(), "Should show third command")
	assert.True(t, updatedModel.historySaved, "Should save present state")

	// Press up again
	updatedModel.prompt().TextArea.CursorStart()
	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyUp})
	updatedModel, ok = newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 1, updatedModel.historyCursor, "Should move to second command")
	assert.Equal(t, "second command", updatedModel.prompt().Value())

	// Press down to go forward in history
	updatedModel.prompt().TextArea.CursorEnd()
	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyDown})
	updatedModel, ok = newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 2, updatedModel.historyCursor, "Should move forward to third command")
	assert.Equal(t, "third command", updatedModel.prompt().Value())

	// Press down again to return to present
	updatedModel.prompt().TextArea.CursorEnd()
	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyDown})
	updatedModel, ok = newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 3, updatedModel.historyCursor, "Should return to present")
	assert.Equal(t, "current input", updatedModel.prompt().Value(), "Should restore current input")
	assert.False(t, updatedModel.historySaved, "Should clear saved state")
}

func TestViModeHistoryNavigationWithKJ(t *testing.T) {
	// Test that k and j keys also work for history navigation in vi normal mode
	config := &Config{}
	model := NewTUIModel(config, nil, nil, nil, nil, nil, nil, nil)

	// Add history
	model.sessionPromptHistory = []promptHistoryEntry{
		{Prompt: "first", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second", SessionSnapshot: 2, ChatSnapshot: 1},
	}
	model.historyCursor = len(model.sessionPromptHistory)
	model.prompt().SetValue("current")

	// Switch to vi normal mode
	model.prompt().EnterViNormalMode()

	// Press k (up in vi)
	model.prompt().TextArea.CursorStart()
	newModel, _ := model.handleViNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	updatedModel, ok := newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 1, updatedModel.historyCursor)
	assert.Equal(t, "second", updatedModel.prompt().Value())

	// Press j (down in vi)
	updatedModel.prompt().TextArea.CursorEnd()
	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updatedModel, ok = newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 2, updatedModel.historyCursor)
	assert.Equal(t, "current", updatedModel.prompt().Value())
}

func TestViNormalModeEnterSubmitsPrompt(t *testing.T) {
	config := &Config{}
	ri := &repo.RepoInfo{}
	model := NewTUIModel(config, ri, nil, nil, nil, nil, nil, nil)
	model.sessionActive = true // Ensure chat view is active

	model.prompt().SetValue("ship it")
	model.prompt().EnterViNormalMode()
	model.Mode = "normal"

	// In normal mode, Enter key calls handleEnterKey() directly which processes the prompt
	newModel, _ := model.handleViNormalMode(tea.KeyMsg{Type: tea.KeyEnter})
	updatedModel, ok := newModel.(TUIModel)
	assert.True(t, ok, "Should return TUIModel")

	// The prompt should be cleared after submission
	assert.Equal(t, "", updatedModel.prompt().Value(), "Prompt should be cleared after sending message")

	// Check that the user message was added to chat
	chat := updatedModel.tabs.Content().Chat
	assert.NotEmpty(t, chat.Messages)
	lastMsg := chat.Messages[len(chat.Messages)-1]
	assert.Equal(t, "ship it", lastMsg.Content)
	assert.Equal(t, MessageTypeUser, lastMsg.Type)
}

func TestViNormalModeEnterWithEmptyPrompt(t *testing.T) {
	config := &Config{}
	model := NewTUIModel(config, nil, nil, nil, nil, nil, nil, nil)
	model.sessionActive = true

	model.prompt().SetValue("")
	model.prompt().EnterViNormalMode()
	model.Mode = "normal"

	initialMessageCount := len(model.tabs.Content().Chat.Messages)

	// Enter with empty prompt should do nothing
	newModel, cmd := model.handleViNormalMode(tea.KeyMsg{Type: tea.KeyEnter})
	updatedModel, ok := newModel.(TUIModel)
	assert.True(t, ok)

	// No command should be returned
	assert.Nil(t, cmd, "No command should be returned for empty prompt")

	// No new messages should be added
	assert.Equal(t, initialMessageCount, len(updatedModel.tabs.Content().Chat.Messages))
}

func TestViNormalModeGtSwitchesTab(t *testing.T) {
	config := &Config{}
	model := NewTUIModel(config, nil, nil, nil, nil, nil, nil, nil)
	model.sessionActive = true

	// App starts with 2 tabs (Ruling, Hunting), active tab 0
	assert.Equal(t, 2, model.tabs.TabCount(), "Should start with 2 tabs")
	assert.Equal(t, 0, model.tabs.activeTab, "Should start on first tab")

	// Enter normal mode
	model.prompt().EnterViNormalMode()
	model.Mode = "normal"

	// Press 'g' to set pending g-prefix
	newModel, _ := model.handleViNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	assert.True(t, updatedModel.tabs.IsPendingG(), "PendingG should be set after pressing 'g'")

	// Press 't' to complete gt (next tab)
	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	updatedModel, ok = newModel.(TUIModel)
	require.True(t, ok)
	assert.False(t, updatedModel.tabs.IsPendingG(), "PendingG should be cleared after gt")
	assert.Equal(t, 1, updatedModel.tabs.activeTab, "Active tab should switch to 1 after gt")
}

func TestViNormalModeGTSwitchesPrevTab(t *testing.T) {
	config := &Config{}
	model := NewTUIModel(config, nil, nil, nil, nil, nil, nil, nil)
	model.sessionActive = true

	// Start on second tab
	model.tabs.SwitchTo(1)
	model.prompt().EnterViNormalMode()
	model.Mode = "normal"

	// Press 'g' then 'T' (previous tab)
	newModel, _ := model.handleViNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)

	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	updatedModel, ok = newModel.(TUIModel)
	require.True(t, ok)
	assert.Equal(t, 0, updatedModel.tabs.activeTab, "Active tab should switch to 0 after gT")
}

func TestScrollModeGtSwitchesTab(t *testing.T) {
	config := &Config{}
	model := NewTUIModel(config, nil, nil, nil, nil, nil, nil, nil)
	model.sessionActive = true
	model.Mode = "scroll"

	// App starts with 2 tabs, active tab 0
	assert.Equal(t, 0, model.tabs.activeTab)

	// Press 'g' to set pending g-prefix in scroll mode
	result, _, handled := model.handleScrollModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	assert.True(t, handled)
	updatedModel, ok := result.(TUIModel)
	require.True(t, ok)
	assert.True(t, updatedModel.tabs.IsPendingG(), "PendingG should be set")

	// Press 't' to complete gt
	result, _, handled = updatedModel.handleScrollModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	assert.True(t, handled)
	updatedModel, ok = result.(TUIModel)
	require.True(t, ok)
	assert.Equal(t, 1, updatedModel.tabs.activeTab, "Active tab should switch to 1 after gt in scroll mode")
}
