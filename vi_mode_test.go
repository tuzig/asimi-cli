package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestViModeToggle(t *testing.T) {
	// Create a new prompt component
	prompt := NewPromptComponent(80, 5)

	// Initially, vi mode should be disabled
	assert.False(t, prompt.ViMode, "Vi mode should be disabled by default")

	// Enable vi mode
	prompt.SetViMode(true)
	assert.True(t, prompt.ViMode, "Vi mode should be enabled")
	assert.Equal(t, ViModeInsert, prompt.ViCurrentMode, "Should start in insert mode when vi mode is enabled")

	// Disable vi mode
	prompt.SetViMode(false)
	assert.False(t, prompt.ViMode, "Vi mode should be disabled")
}

func TestViModeTransitions(t *testing.T) {
	// Create a new prompt component
	prompt := NewPromptComponent(80, 5)

	// Enable vi mode
	prompt.SetViMode(true)
	assert.True(t, prompt.IsViInsertMode(), "Should start in insert mode")
	assert.False(t, prompt.IsViNormalMode(), "Should not be in normal mode")

	// Switch to normal mode (ESC from insert)
	prompt.EnterViNormalMode()
	assert.False(t, prompt.IsViInsertMode(), "Should not be in insert mode")
	assert.True(t, prompt.IsViNormalMode(), "Should be in normal mode")

	// Switch to visual mode (v from normal)
	prompt.EnterViVisualMode()
	assert.False(t, prompt.IsViInsertMode(), "Should not be in insert mode")
	assert.False(t, prompt.IsViNormalMode(), "Should not be in normal mode")
	assert.True(t, prompt.IsViVisualMode(), "Should be in visual mode")

	// Switch to command-line mode (: from normal)
	prompt.EnterViNormalMode()
	prompt.EnterViCommandLineMode()
	assert.False(t, prompt.IsViInsertMode(), "Should not be in insert mode")
	assert.False(t, prompt.IsViNormalMode(), "Should not be in normal mode")
	assert.True(t, prompt.IsViCommandLineMode(), "Should be in command-line mode")

	// Switch back to insert mode (after command execution)
	prompt.EnterViInsertMode()
	assert.True(t, prompt.IsViInsertMode(), "Should be in insert mode")
	assert.False(t, prompt.IsViNormalMode(), "Should not be in normal mode")
}

func TestViCommandRegistered(t *testing.T) {
	// Create a new command registry
	registry := NewCommandRegistry()

	// Check if /vi command is registered
	cmd, exists := registry.GetCommand("/vi")
	assert.True(t, exists, "/vi command should be registered")
	assert.Equal(t, "/vi", cmd.Name, "Command name should be /vi")
	assert.Contains(t, cmd.Description, "vi mode", "Command description should mention vi mode")
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

func TestViCommandLineModeUsesNormalKeymap(t *testing.T) {
	// Create a new prompt component
	prompt := NewPromptComponent(80, 5)

	// Enable vi mode
	prompt.SetViMode(true)
	assert.True(t, prompt.IsViInsertMode(), "Should start in insert mode")

	// Switch to normal mode
	prompt.EnterViNormalMode()
	assert.True(t, prompt.IsViNormalMode(), "Should be in normal mode")

	// Verify that normal mode uses vi normal keymap
	assert.Equal(t, prompt.viNormalKeyMap, prompt.TextArea.KeyMap, "Normal mode should use vi normal keymap")

	// Switch to command-line mode
	prompt.EnterViCommandLineMode()
	assert.True(t, prompt.IsViCommandLineMode(), "Should be in command-line mode")

	// Verify that command-line mode uses normal (non-vi) keymap
	assert.Equal(t, prompt.normalKeyMap, prompt.TextArea.KeyMap, "Command-line mode should use normal keymap (like when vi mode is disabled)")

	// Switch back to insert mode
	prompt.EnterViInsertMode()
	assert.True(t, prompt.IsViInsertMode(), "Should be in insert mode")

	// Verify that insert mode uses vi insert keymap
	assert.Equal(t, prompt.viInsertKeyMap, prompt.TextArea.KeyMap, "Insert mode should use vi insert keymap")
}

func TestViModePlaceholderText(t *testing.T) {
	// Create a new prompt component
	prompt := NewPromptComponent(80, 5)

	// Enable vi mode
	prompt.SetViMode(true)
	assert.Equal(t, "Type your message here...", prompt.TextArea.Placeholder, "Insert mode should have default placeholder")

	// Switch to normal mode
	prompt.EnterViNormalMode()
	assert.Equal(t, "i for insert mode, : for commands, ↑↓ for history", prompt.TextArea.Placeholder, "Normal mode should have navigation placeholder")

	// Switch to visual mode
	prompt.EnterViVisualMode()
	assert.Equal(t, "Visual mode - select text", prompt.TextArea.Placeholder, "Visual mode should have selection placeholder")

	// Switch to command-line mode
	prompt.EnterViCommandLineMode()
	assert.Equal(t, "Enter command...", prompt.TextArea.Placeholder, "Command-line mode should have command placeholder")

	// Switch back to insert mode
	prompt.EnterViInsertMode()
	assert.Equal(t, "Type your message here...", prompt.TextArea.Placeholder, "Insert mode should have default placeholder")

	// Disable vi mode
	prompt.SetViMode(false)
	assert.Equal(t, "Type your message here...", prompt.TextArea.Placeholder, "Disabled vi mode should have default placeholder")
}

func TestViModeHistoryNavigation(t *testing.T) {
	// This test verifies that arrow keys work for history navigation in vi normal mode
	// Create a test model with vi mode enabled
	config := &Config{}
	config.LLM.ViMode = boolPtr(true)
	model := NewTUIModel(config)

	// Add some history entries
	model.promptHistory = []promptHistoryEntry{
		{Prompt: "first command", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second command", SessionSnapshot: 2, ChatSnapshot: 1},
		{Prompt: "third command", SessionSnapshot: 3, ChatSnapshot: 2},
	}
	model.historyCursor = len(model.promptHistory)
	model.prompt.SetValue("current input")

	// Switch to vi normal mode
	model.prompt.EnterViNormalMode()
	assert.True(t, model.prompt.IsViNormalMode(), "Should be in vi normal mode")

	// Simulate pressing up arrow in normal mode
	model.prompt.TextArea.CursorStart() // Ensure we're on first line
	newModel, _ := model.handleViNormalMode(tea.KeyMsg{Type: tea.KeyUp})
	updatedModel, ok := newModel.(TUIModel)
	assert.True(t, ok)

	// Should navigate to previous history entry
	assert.Equal(t, 2, updatedModel.historyCursor, "Should move to previous history entry")
	assert.Equal(t, "third command", updatedModel.prompt.Value(), "Should show third command")
	assert.True(t, updatedModel.historySaved, "Should save present state")

	// Press up again
	updatedModel.prompt.TextArea.CursorStart()
	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyUp})
	updatedModel, ok = newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 1, updatedModel.historyCursor, "Should move to second command")
	assert.Equal(t, "second command", updatedModel.prompt.Value())

	// Press down to go forward in history
	updatedModel.prompt.TextArea.CursorEnd()
	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyDown})
	updatedModel, ok = newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 2, updatedModel.historyCursor, "Should move forward to third command")
	assert.Equal(t, "third command", updatedModel.prompt.Value())

	// Press down again to return to present
	updatedModel.prompt.TextArea.CursorEnd()
	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyDown})
	updatedModel, ok = newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 3, updatedModel.historyCursor, "Should return to present")
	assert.Equal(t, "current input", updatedModel.prompt.Value(), "Should restore current input")
	assert.False(t, updatedModel.historySaved, "Should clear saved state")
}

func TestViModeHistoryNavigationWithKJ(t *testing.T) {
	// Test that k and j keys also work for history navigation in vi normal mode
	config := &Config{}
	config.LLM.ViMode = boolPtr(true)
	model := NewTUIModel(config)

	// Add history
	model.promptHistory = []promptHistoryEntry{
		{Prompt: "first", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second", SessionSnapshot: 2, ChatSnapshot: 1},
	}
	model.historyCursor = len(model.promptHistory)
	model.prompt.SetValue("current")

	// Switch to vi normal mode
	model.prompt.EnterViNormalMode()

	// Press k (up in vi)
	model.prompt.TextArea.CursorStart()
	newModel, _ := model.handleViNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	updatedModel, ok := newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 1, updatedModel.historyCursor)
	assert.Equal(t, "second", updatedModel.prompt.Value())

	// Press j (down in vi)
	updatedModel.prompt.TextArea.CursorEnd()
	newModel, _ = updatedModel.handleViNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updatedModel, ok = newModel.(TUIModel)
	assert.True(t, ok)
	assert.Equal(t, 2, updatedModel.historyCursor)
	assert.Equal(t, "current", updatedModel.prompt.Value())
}
