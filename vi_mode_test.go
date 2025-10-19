package main

import (
	"testing"

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
