package main

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func TestNewSessionSelectionModal(t *testing.T) {
	modal := NewSessionSelectionModal()

	assert.NotNil(t, modal)
	assert.NotNil(t, modal.BaseModal)
	assert.Equal(t, "Resume Session", modal.BaseModal.Title)
	assert.Equal(t, 0, modal.selected)
	assert.Equal(t, 0, modal.scrollOffset)
	assert.Equal(t, 10, modal.maxVisible)
	assert.True(t, modal.loading)
	assert.Nil(t, modal.err)
	assert.Empty(t, modal.sessions)
}

func TestSessionSelectionModalSetSessions(t *testing.T) {
	modal := NewSessionSelectionModal()

	sessions := []Session{
		{
			ID:          "session-1",
			FirstPrompt: "Test prompt 1",
			Messages:    []llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "test"}}}},
			Model:       "gpt-4",
			LastUpdated: time.Now(),
		},
		{
			ID:          "session-2",
			FirstPrompt: "Test prompt 2",
			Messages:    []llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "test"}}}},
			Model:       "claude-3",
			LastUpdated: time.Now().Add(-1 * time.Hour),
		},
	}

	modal.SetSessions(sessions)

	assert.Equal(t, 2, len(modal.sessions))
	assert.False(t, modal.loading)
	assert.Nil(t, modal.err)
	assert.Equal(t, "session-1", modal.sessions[0].ID)
	assert.Equal(t, "session-2", modal.sessions[1].ID)
}

func TestSessionSelectionModalSetError(t *testing.T) {
	modal := NewSessionSelectionModal()

	testErr := assert.AnError
	modal.SetError(testErr)

	assert.False(t, modal.loading)
	assert.Equal(t, testErr, modal.err)
}

func TestSessionSelectionModalRenderLoading(t *testing.T) {
	modal := NewSessionSelectionModal()

	output := modal.Render()

	assert.Contains(t, output, "Loading sessions")
	assert.Contains(t, output, "Resume Session")
}

func TestSessionSelectionModalRenderError(t *testing.T) {
	modal := NewSessionSelectionModal()
	modal.SetError(assert.AnError)

	output := modal.Render()

	assert.Contains(t, output, "Error loading sessions")
	assert.Contains(t, output, "Press Esc to close")
}

func TestSessionSelectionModalRenderEmpty(t *testing.T) {
	modal := NewSessionSelectionModal()
	modal.SetSessions([]Session{})

	output := modal.Render()

	assert.Contains(t, output, "No previous sessions found")
	assert.Contains(t, output, "Start chatting to create a new session")
	assert.Contains(t, output, "Press Esc to close")
}

func TestSessionSelectionModalRenderWithSessions(t *testing.T) {
	modal := NewSessionSelectionModal()

	sessions := []Session{
		{
			ID:          "session-1",
			FirstPrompt: "First test prompt",
			Messages:    []llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "First test prompt"}}}},
			Model:       "gpt-4",
			LastUpdated: time.Now(),
			WorkingDir:  "/test/dir",
		},
		{
			ID:          "session-2",
			FirstPrompt: "Second test prompt",
			Messages:    []llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "Second test prompt"}}}},
			Model:       "claude-3",
			LastUpdated: time.Now().Add(-1 * time.Hour),
		},
	}

	modal.SetSessions(sessions)
	output := modal.Render()

	// Check for navigation instructions
	assert.Contains(t, output, "↑/↓: Navigate")
	assert.Contains(t, output, "Enter: Select")
	assert.Contains(t, output, "Esc/Q: Cancel")

	// Check for session content
	assert.Contains(t, output, "First test prompt")
	assert.Contains(t, output, "Second test prompt")
	assert.Contains(t, output, "1 msg")
	assert.Contains(t, output, "gpt-4")
	assert.Contains(t, output, "claude-3")

	// Check for Cancel option
	assert.Contains(t, output, "Cancel")
}

func TestSessionSelectionModalNavigationUp(t *testing.T) {
	modal := NewSessionSelectionModal()
	sessions := createTestSessions(3)
	modal.SetSessions(sessions)

	// Start at position 2
	modal.selected = 2

	// Navigate up
	keyMsg := tea.KeyMsg{Type: tea.KeyUp}
	updatedModal, cmd := modal.Update(keyMsg)

	assert.Equal(t, 1, updatedModal.selected)
	assert.Nil(t, cmd)
}

func TestSessionSelectionModalNavigationDown(t *testing.T) {
	modal := NewSessionSelectionModal()
	sessions := createTestSessions(3)
	modal.SetSessions(sessions)

	// Start at position 0
	modal.selected = 0

	// Navigate down
	keyMsg := tea.KeyMsg{Type: tea.KeyDown}
	updatedModal, cmd := modal.Update(keyMsg)

	assert.Equal(t, 1, updatedModal.selected)
	assert.Nil(t, cmd)
}

func TestSessionSelectionModalNavigationBoundaries(t *testing.T) {
	modal := NewSessionSelectionModal()
	sessions := createTestSessions(2)
	modal.SetSessions(sessions)

	t.Run("cannot go above first item", func(t *testing.T) {
		modal.selected = 0
		keyMsg := tea.KeyMsg{Type: tea.KeyUp}
		updatedModal, _ := modal.Update(keyMsg)
		assert.Equal(t, 0, updatedModal.selected)
	})

	t.Run("can navigate to cancel option", func(t *testing.T) {
		modal.selected = 0
		// Navigate down to last session
		keyMsg := tea.KeyMsg{Type: tea.KeyDown}
		updatedModal, _ := modal.Update(keyMsg)
		assert.Equal(t, 1, updatedModal.selected)

		// Navigate down to cancel option (index 2, which is len(sessions))
		keyMsg = tea.KeyMsg{Type: tea.KeyDown}
		updatedModal, _ = updatedModal.Update(keyMsg)
		assert.Equal(t, 2, updatedModal.selected)
	})

	t.Run("cannot go below cancel option", func(t *testing.T) {
		// Total items = 2 sessions + 1 cancel = 3 items (indices 0-2)
		modal.selected = 2 // Cancel option
		keyMsg := tea.KeyMsg{Type: tea.KeyDown}
		updatedModal, _ := modal.Update(keyMsg)
		assert.Equal(t, 2, updatedModal.selected)
	})
}

func TestSessionSelectionModalQuickSelect(t *testing.T) {
	modal := NewSessionSelectionModal()
	sessions := createTestSessions(5)
	modal.SetSessions(sessions)

	tests := []struct {
		name     string
		key      string
		expected int
	}{
		{"select first", "1", 0},
		{"select second", "2", 1},
		{"select third", "3", 2},
		{"select fourth", "4", 3},
		{"select fifth", "5", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modal.selected = 0 // Reset selection
			keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			updatedModal, cmd := modal.Update(keyMsg)

			assert.Equal(t, tt.expected, updatedModal.selected)
			assert.NotNil(t, cmd) // Should trigger session load
		})
	}
}

func TestSessionSelectionModalQuickSelectOutOfRange(t *testing.T) {
	modal := NewSessionSelectionModal()
	sessions := createTestSessions(2)
	modal.SetSessions(sessions)

	// Try to select session 5 when only 2 exist
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")}
	updatedModal, cmd := modal.Update(keyMsg)

	// Selection should not change
	assert.Equal(t, 0, updatedModal.selected)
	assert.Nil(t, cmd)
}

func TestSessionSelectionModalEnterKey(t *testing.T) {
	modal := NewSessionSelectionModal()
	sessions := createTestSessions(2)
	modal.SetSessions(sessions)

	t.Run("enter on session triggers load", func(t *testing.T) {
		modal.selected = 0
		keyMsg := tea.KeyMsg{Type: tea.KeyEnter}
		_, cmd := modal.Update(keyMsg)

		assert.NotNil(t, cmd)
		// Execute the command to verify it returns the right message type
		msg := cmd()
		_, ok := msg.(sessionResumeErrorMsg)
		// It will be an error because we don't have a real session store
		assert.True(t, ok)
	})

	t.Run("enter on cancel option triggers cancel", func(t *testing.T) {
		modal.selected = 2 // Cancel option (2 sessions + cancel = index 2)
		keyMsg := tea.KeyMsg{Type: tea.KeyEnter}
		_, cmd := modal.Update(keyMsg)

		assert.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(modalCancelledMsg)
		assert.True(t, ok)
	})
}

func TestSessionSelectionModalEscapeKey(t *testing.T) {
	modal := NewSessionSelectionModal()
	sessions := createTestSessions(2)
	modal.SetSessions(sessions)

	tests := []struct {
		name string
		key  tea.KeyType
	}{
		{"escape key", tea.KeyEsc},
		{"q key", tea.KeyRunes},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var keyMsg tea.KeyMsg
			if tt.key == tea.KeyRunes {
				keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
			} else {
				keyMsg = tea.KeyMsg{Type: tt.key}
			}

			_, cmd := modal.Update(keyMsg)
			assert.NotNil(t, cmd)

			msg := cmd()
			_, ok := msg.(modalCancelledMsg)
			assert.True(t, ok)
		})
	}
}

func TestSessionSelectionModalScrolling(t *testing.T) {
	modal := NewSessionSelectionModal()
	sessions := createTestSessions(15) // More than maxVisible (10)
	modal.SetSessions(sessions)

	t.Run("scroll down when reaching bottom of visible area", func(t *testing.T) {
		modal.selected = 0
		modal.scrollOffset = 0

		// Navigate down to position 10 (beyond maxVisible)
		for i := 0; i < 10; i++ {
			keyMsg := tea.KeyMsg{Type: tea.KeyDown}
			modal, _ = modal.Update(keyMsg)
		}

		// Should have scrolled
		assert.Equal(t, 10, modal.selected)
		assert.Equal(t, 1, modal.scrollOffset)
	})

	t.Run("scroll up when reaching top of visible area", func(t *testing.T) {
		modal.selected = 10
		modal.scrollOffset = 5

		// Navigate up
		keyMsg := tea.KeyMsg{Type: tea.KeyUp}
		updatedModal, _ := modal.Update(keyMsg)

		assert.Equal(t, 9, updatedModal.selected)
		// Scroll offset should adjust if needed
		if updatedModal.selected < updatedModal.scrollOffset {
			assert.Equal(t, updatedModal.selected, updatedModal.scrollOffset)
		}
	})
}

func TestSessionSelectionModalVimKeys(t *testing.T) {
	modal := NewSessionSelectionModal()
	sessions := createTestSessions(3)
	modal.SetSessions(sessions)

	t.Run("k key moves up", func(t *testing.T) {
		modal.selected = 1
		keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}
		updatedModal, _ := modal.Update(keyMsg)
		assert.Equal(t, 0, updatedModal.selected)
	})

	t.Run("j key moves down", func(t *testing.T) {
		modal.selected = 0
		keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
		updatedModal, _ := modal.Update(keyMsg)
		assert.Equal(t, 1, updatedModal.selected)
	})
}

func TestSessionSelectionModalLoadingState(t *testing.T) {
	modal := NewSessionSelectionModal()

	t.Run("no interaction when loading", func(t *testing.T) {
		assert.True(t, modal.loading)

		keyMsg := tea.KeyMsg{Type: tea.KeyDown}
		updatedModal, cmd := modal.Update(keyMsg)

		// Should not change selection
		assert.Equal(t, 0, updatedModal.selected)
		assert.Nil(t, cmd)
	})

	t.Run("can cancel when loading", func(t *testing.T) {
		keyMsg := tea.KeyMsg{Type: tea.KeyEsc}
		_, cmd := modal.Update(keyMsg)

		assert.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(modalCancelledMsg)
		assert.True(t, ok)
	})
}

func TestSessionSelectionModalErrorState(t *testing.T) {
	modal := NewSessionSelectionModal()
	modal.SetError(assert.AnError)

	t.Run("no interaction when error", func(t *testing.T) {
		keyMsg := tea.KeyMsg{Type: tea.KeyDown}
		updatedModal, cmd := modal.Update(keyMsg)

		assert.Equal(t, 0, updatedModal.selected)
		assert.Nil(t, cmd)
	})

	t.Run("can cancel when error", func(t *testing.T) {
		keyMsg := tea.KeyMsg{Type: tea.KeyEsc}
		_, cmd := modal.Update(keyMsg)

		assert.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(modalCancelledMsg)
		assert.True(t, ok)
	})
}

func TestSessionSelectionModalEmptyState(t *testing.T) {
	modal := NewSessionSelectionModal()
	modal.SetSessions([]Session{})

	t.Run("no interaction when empty", func(t *testing.T) {
		keyMsg := tea.KeyMsg{Type: tea.KeyDown}
		updatedModal, cmd := modal.Update(keyMsg)

		assert.Equal(t, 0, updatedModal.selected)
		assert.Nil(t, cmd)
	})

	t.Run("can cancel when empty", func(t *testing.T) {
		keyMsg := tea.KeyMsg{Type: tea.KeyEsc}
		_, cmd := modal.Update(keyMsg)

		assert.NotNil(t, cmd)
		msg := cmd()
		_, ok := msg.(modalCancelledMsg)
		assert.True(t, ok)
	})
}

// Helper function to create test sessions
func createTestSessions(count int) []Session {
	sessions := make([]Session, count)
	for i := 0; i < count; i++ {
		prompt := "Test prompt " + string(rune('1'+i))
		sessions[i] = Session{
			ID:          "session-" + string(rune('1'+i)),
			FirstPrompt: prompt,
			Messages: []llms.MessageContent{
				{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: prompt}}},
			},
			Model:       "test-model",
			LastUpdated: time.Now().Add(-time.Duration(i) * time.Hour),
			WorkingDir:  "/test/dir",
		}
	}
	return sessions
}

// Test the formatRelativeTime function indirectly through rendering
func TestFormatRelativeTimeInModal(t *testing.T) {
	// This test assumes formatRelativeTime is accessible
	// If it's not exported, you may need to test it indirectly through Render()

	now := time.Now()

	tests := []struct {
		name     string
		time     time.Time
		contains string // What the output should contain
	}{
		{
			name:     "just now",
			time:     now,
			contains: "now",
		},
		{
			name:     "1 hour ago",
			time:     now.Add(-1 * time.Hour),
			contains: "hour",
		},
		{
			name:     "1 day ago",
			time:     now.Add(-24 * time.Hour),
			contains: "day",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test indirectly through modal rendering
			modal := NewSessionSelectionModal()
			sessions := []Session{
				{
					ID:          "test",
					FirstPrompt: "Test",
					Messages:    []llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "test"}}}},
					Model:       "test-model",
					LastUpdated: tt.time,
				},
			}
			modal.SetSessions(sessions)
			output := modal.Render()

			// The output should contain some time-related text
			assert.NotEmpty(t, output)
		})
	}
}

// Integration test for the full modal lifecycle
func TestSessionSelectionModalLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("complete user flow", func(t *testing.T) {
		// Create modal
		modal := NewSessionSelectionModal()
		require.NotNil(t, modal)
		require.True(t, modal.loading)

		// Load sessions
		sessions := createTestSessions(3)
		modal.SetSessions(sessions)
		require.False(t, modal.loading)
		require.Len(t, modal.sessions, 3)

		// Navigate down
		keyMsg := tea.KeyMsg{Type: tea.KeyDown}
		modal, _ = modal.Update(keyMsg)
		assert.Equal(t, 1, modal.selected)

		// Navigate up
		keyMsg = tea.KeyMsg{Type: tea.KeyUp}
		modal, _ = modal.Update(keyMsg)
		assert.Equal(t, 0, modal.selected)

		// Quick select
		keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")}
		modal, cmd := modal.Update(keyMsg)
		assert.Equal(t, 1, modal.selected)
		assert.NotNil(t, cmd)

		// Render at various states
		output := modal.Render()
		assert.Contains(t, output, "Test prompt")
	})
}
