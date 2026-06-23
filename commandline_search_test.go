package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestEnterSearchModeForward verifies EnterSearchMode sets up forward search
func TestEnterSearchModeForward(t *testing.T) {
	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	cmd := cl.EnterSearchMode(1)

	if !cl.IsInSearchMode() {
		t.Error("Should be in search mode after EnterSearchMode")
	}
	if cl.searchDirection != 1 {
		t.Errorf("Expected searchDirection=1 (forward), got %d", cl.searchDirection)
	}

	msg := cmd()
	modeMsg, ok := msg.(ChangeModeMsg)
	if !ok {
		t.Fatalf("Expected ChangeModeMsg, got %T", msg)
	}
	if modeMsg.NewMode != "search" {
		t.Errorf("Expected mode 'search', got %q", modeMsg.NewMode)
	}

	view := cl.View()
	if !strings.Contains(view, "/") {
		t.Error("View should contain '/' prefix for forward search")
	}
}

// TestEnterSearchModeBackward verifies EnterSearchMode sets up backward search
func TestEnterSearchModeBackward(t *testing.T) {
	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	cmd := cl.EnterSearchMode(-1)

	if !cl.IsInSearchMode() {
		t.Error("Should be in search mode after EnterSearchMode")
	}
	if cl.searchDirection != -1 {
		t.Errorf("Expected searchDirection=-1 (backward), got %d", cl.searchDirection)
	}

	msg := cmd()
	modeMsg, ok := msg.(ChangeModeMsg)
	if !ok {
		t.Fatalf("Expected ChangeModeMsg, got %T", msg)
	}
	if modeMsg.NewMode != "search" {
		t.Errorf("Expected mode 'search', got %q", modeMsg.NewMode)
	}

	view := cl.View()
	if !strings.Contains(view, "?") {
		t.Error("View should contain '?' prefix for backward search")
	}
}

// TestExitSearchMode verifies ExitSearchMode resets state and returns to models
func TestExitSearchMode(t *testing.T) {
	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	cl.EnterSearchMode(1)
	cl.searchTextInput.SetValue("gpt")

	cmd := cl.ExitSearchMode()

	if cl.IsInSearchMode() {
		t.Error("Should not be in search mode after ExitSearchMode")
	}
	if cl.searchTextInput.Value() != "" {
		t.Errorf("Expected empty search text, got %q", cl.searchTextInput.Value())
	}

	msg := cmd()
	modeMsg, ok := msg.(ChangeModeMsg)
	if !ok {
		t.Fatalf("Expected ChangeModeMsg, got %T", msg)
	}
	if modeMsg.NewMode != "models" {
		t.Errorf("Expected mode 'models', got %q", modeMsg.NewMode)
	}
}

// TestSearchModeTextEntry verifies text can be typed in search mode
func TestSearchModeTextEntry(t *testing.T) {
	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	cl.EnterSearchMode(1)

	charMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
	cl.HandleKey(charMsg)

	if cl.searchTextInput.Value() != "g" {
		t.Errorf("Expected 'g', got %q", cl.searchTextInput.Value())
	}

	charMsg2 := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: false}
	cl.HandleKey(charMsg2)

	if cl.searchTextInput.Value() != "gp" {
		t.Errorf("Expected 'gp', got %q", cl.searchTextInput.Value())
	}
}

// TestSearchModeEnterEmitsExecutedMsg verifies Enter emits searchExecutedMsg
func TestSearchModeEnterEmitsExecutedMsg(t *testing.T) {
	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	cl.EnterSearchMode(1)
	cl.searchTextInput.SetValue("gpt-4")

	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	cmd, handled := cl.HandleKey(enterMsg)
	if !handled {
		t.Fatal("Search mode should handle Enter")
	}

	msg := cmd()
	batchMsg, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Expected BatchMsg, got %T", msg)
	}

	found := false
	for _, c := range batchMsg {
		if c == nil {
			continue
		}
		m := c()
		if _, ok := m.(searchExecutedMsg); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected searchExecutedMsg in batch")
	}

	if cl.IsInSearchMode() {
		t.Error("Should not be in search mode after Enter")
	}
}

// TestSearchModeEscCancels verifies Escape cancels search
func TestSearchModeEscCancels(t *testing.T) {
	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	cl.EnterSearchMode(1)
	cl.searchTextInput.SetValue("test")

	escMsg := tea.KeyMsg{Type: tea.KeyEsc}
	cmd, handled := cl.HandleKey(escMsg)
	if !handled {
		t.Fatal("Search mode should handle Escape")
	}

	msg := cmd()
	batchMsg, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Expected BatchMsg, got %T", msg)
	}

	found := false
	for _, c := range batchMsg {
		if c == nil {
			continue
		}
		m := c()
		if _, ok := m.(searchCancelledMsg); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected searchCancelledMsg in batch")
	}

	if cl.IsInSearchMode() {
		t.Error("Should not be in search mode after Escape")
	}
}

// TestSearchModeBackspaceEmptyExits verifies backspace on empty input exits search
func TestSearchModeBackspaceEmptyExits(t *testing.T) {
	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	cl.EnterSearchMode(1)

	bsMsg := tea.KeyMsg{Type: tea.KeyBackspace}
	cmd, handled := cl.HandleKey(bsMsg)
	if !handled {
		t.Fatal("Search mode should handle backspace")
	}

	if cl.IsInSearchMode() {
		t.Error("Should exit search mode on backspace with empty input")
	}

	msg := cmd()
	batchMsg, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Expected BatchMsg, got %T", msg)
	}

	found := false
	for _, c := range batchMsg {
		if c == nil {
			continue
		}
		m := c()
		if _, ok := m.(searchCancelledMsg); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected searchCancelledMsg in batch")
	}
}

// TestSearchModeBackspaceWithTextDeletes verifies backspace with text deletes a char
func TestSearchModeBackspaceWithTextDeletes(t *testing.T) {
	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	cl.EnterSearchMode(1)
	cl.searchTextInput.SetValue("abc")

	bsMsg := tea.KeyMsg{Type: tea.KeyBackspace}
	cl.HandleKey(bsMsg)

	if cl.searchTextInput.Value() != "ab" {
		t.Errorf("Expected 'ab', got %q", cl.searchTextInput.Value())
	}

	if !cl.IsInSearchMode() {
		t.Error("Should still be in search mode with non-empty input")
	}
}

// TestIsInSearchModeNotActiveByDefault verifies search mode is not active by default
func TestIsInSearchModeNotActiveByDefault(t *testing.T) {
	cl := NewCommandLineComponent()
	if cl.IsInSearchMode() {
		t.Error("Search mode should not be active by default")
	}
}

// TestGetSearchPattern verifies GetSearchPattern returns the current value
func TestGetSearchPattern(t *testing.T) {
	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	cl.EnterSearchMode(1)
	cl.searchTextInput.SetValue("gemini")

	if cl.GetSearchPattern() != "gemini" {
		t.Errorf("Expected 'gemini', got %q", cl.GetSearchPattern())
	}
}

// TestGetSearchDirection verifies GetSearchDirection returns the current direction
func TestGetSearchDirection(t *testing.T) {
	cl := NewCommandLineComponent()

	cl.EnterSearchMode(1)
	if cl.GetSearchDirection() != 1 {
		t.Errorf("Expected direction 1, got %d", cl.GetSearchDirection())
	}

	cl.EnterSearchMode(-1)
	if cl.GetSearchDirection() != -1 {
		t.Errorf("Expected direction -1, got %d", cl.GetSearchDirection())
	}
}
