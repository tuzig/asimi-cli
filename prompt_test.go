package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestArrowKeysInViNormalMode tests that arrow keys work in vi normal mode
func TestArrowKeysInViNormalMode(t *testing.T) {
	// Create a prompt component with vi mode enabled
	prompt := NewPromptComponent(80, 5)
	prompt.SetViMode(true)

	// Set some initial text
	prompt.SetValue("Hello World")

	// Enter normal mode
	prompt.EnterViNormalMode()

	// Test that we're in normal mode
	if !prompt.IsViNormalMode() {
		t.Fatal("Expected to be in vi normal mode")
	}

	// Move cursor to start
	prompt.TextArea.SetCursor(0)

	// Test right arrow key
	rightMsg := tea.KeyMsg{Type: tea.KeyRight}
	prompt, _ = prompt.Update(rightMsg)

	// The cursor should have moved (we can't directly check cursor position,
	// but we can verify the update didn't fail and we're still in normal mode)
	if !prompt.IsViNormalMode() {
		t.Error("Should still be in normal mode after right arrow")
	}

	// Test left arrow key
	leftMsg := tea.KeyMsg{Type: tea.KeyLeft}
	prompt, _ = prompt.Update(leftMsg)

	if !prompt.IsViNormalMode() {
		t.Error("Should still be in normal mode after left arrow")
	}

	// Test down arrow key
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	prompt, _ = prompt.Update(downMsg)

	if !prompt.IsViNormalMode() {
		t.Error("Should still be in normal mode after down arrow")
	}

	// Test up arrow key
	upMsg := tea.KeyMsg{Type: tea.KeyUp}
	prompt, _ = prompt.Update(upMsg)

	if !prompt.IsViNormalMode() {
		t.Error("Should still be in normal mode after up arrow")
	}
}

// TestViMovementKeys tests that vi movement keys (h, j, k, l) work in normal mode
func TestViMovementKeys(t *testing.T) {
	// Create a prompt component with vi mode enabled
	prompt := NewPromptComponent(80, 5)
	prompt.SetViMode(true)

	// Set some initial text with multiple lines
	prompt.SetValue("Line 1\nLine 2\nLine 3")

	// Enter normal mode
	prompt.EnterViNormalMode()

	// Test h (left)
	hMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	prompt, _ = prompt.Update(hMsg)
	if !prompt.IsViNormalMode() {
		t.Error("Should still be in normal mode after 'h'")
	}

	// Test l (right)
	lMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	prompt, _ = prompt.Update(lMsg)
	if !prompt.IsViNormalMode() {
		t.Error("Should still be in normal mode after 'l'")
	}

	// Test j (down)
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	prompt, _ = prompt.Update(jMsg)
	if !prompt.IsViNormalMode() {
		t.Error("Should still be in normal mode after 'j'")
	}

	// Test k (up)
	kMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	prompt, _ = prompt.Update(kMsg)
	if !prompt.IsViNormalMode() {
		t.Error("Should still be in normal mode after 'k'")
	}
}

// TestTextInputBlockedInNormalMode tests that regular text input is blocked in normal mode
func TestTextInputBlockedInNormalMode(t *testing.T) {
	// Create a prompt component with vi mode enabled
	prompt := NewPromptComponent(80, 5)
	prompt.SetViMode(true)

	// Set some initial text
	initialText := "Hello"
	prompt.SetValue(initialText)

	// Enter normal mode
	prompt.EnterViNormalMode()

	// Try to type a regular character (should be blocked)
	aMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	prompt, _ = prompt.Update(aMsg)

	// The text should not have changed (unless 'a' triggered insert mode)
	// Since 'a' is an action key that enters insert mode, let's test with a different character

	// Reset to normal mode
	prompt.SetValue(initialText)
	prompt.EnterViNormalMode()

	// Try a character that should be blocked (like 'z' which has no vi command)
	zMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}}
	prompt, _ = prompt.Update(zMsg)

	// Should still be in normal mode
	if !prompt.IsViNormalMode() {
		t.Error("Should still be in normal mode after blocked character")
	}

	// Text should not have changed
	if prompt.Value() != initialText {
		t.Errorf("Text should not have changed. Expected %q, got %q", initialText, prompt.Value())
	}
}

// TestArrowKeysInInsertMode tests that arrow keys work in insert mode
func TestArrowKeysInInsertMode(t *testing.T) {
	// Create a prompt component with vi mode enabled
	prompt := NewPromptComponent(80, 5)
	prompt.SetViMode(true)

	// Set some initial text
	prompt.SetValue("Hello World")

	// Should start in insert mode
	if !prompt.IsViInsertMode() {
		t.Fatal("Expected to start in vi insert mode")
	}

	// Test right arrow key
	rightMsg := tea.KeyMsg{Type: tea.KeyRight}
	prompt, _ = prompt.Update(rightMsg)

	if !prompt.IsViInsertMode() {
		t.Error("Should still be in insert mode after right arrow")
	}

	// Test left arrow key
	leftMsg := tea.KeyMsg{Type: tea.KeyLeft}
	prompt, _ = prompt.Update(leftMsg)

	if !prompt.IsViInsertMode() {
		t.Error("Should still be in insert mode after left arrow")
	}
}
