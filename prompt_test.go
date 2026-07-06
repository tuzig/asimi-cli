package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
)

// TestPromptHeightGrowsTo10LinesForMultilineInput tests that prompt grows to 10 lines
// when user input is more than one line (issue #31)
func TestPromptHeightGrowsTo10LinesForMultilineInput(t *testing.T) {
	prompt := NewPromptComponent(80, 5)
	prompt.SetScreenHeight(40) // Set screen height so MaxHeight = 20

	tests := []struct {
		name           string
		value          string
		mode           string
		expectedHeight int
	}{
		{
			name:           "empty prompt returns 2 lines",
			value:          "",
			mode:           ViModeInsert,
			expectedHeight: 2,
		},
		{
			name:           "single line content returns 2 lines",
			value:          "Hello World",
			mode:           ViModeInsert,
			expectedHeight: 2,
		},
		{
			name:           "two lines of content grows to 10 lines",
			value:          "Line 1\nLine 2",
			mode:           ViModeInsert,
			expectedHeight: 10,
		},
		{
			name:           "multiple lines of content grows to 10 lines",
			value:          "Line 1\nLine 2\nLine 3\nLine 4",
			mode:           ViModeInsert,
			expectedHeight: 10,
		},
		{
			name:           "scroll mode returns 2 lines regardless of content",
			value:          "Line 1\nLine 2\nLine 3",
			mode:           ViModeScroll,
			expectedHeight: 2,
		},
		{
			name:           "scroll mode with empty content returns 2 lines",
			value:          "",
			mode:           ViModeScroll,
			expectedHeight: 2,
		},
		{
			name:           "normal mode with multiline grows to 10 lines",
			value:          "Line 1\nLine 2",
			mode:           ViModeNormal,
			expectedHeight: 10,
		},
		{
			name:           "long text that wraps grows to 10 lines",
			value:          "This is a very long line of text that should wrap to multiple lines because it exceeds the width of the prompt component which is set to 80 characters",
			mode:           ViModeInsert,
			expectedHeight: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt.SetValue(tt.value)
			prompt.ViCurrentMode = tt.mode

			height := prompt.CalculateDesiredHeight()
			if height != tt.expectedHeight {
				t.Errorf("CalculateDesiredHeight() = %d, want %d", height, tt.expectedHeight)
			}
		})
	}
}

// TestPromptHeightRespectsMaxHeight tests that the 10-line expansion respects MaxHeight
func TestPromptHeightRespectsMaxHeight(t *testing.T) {
	prompt := NewPromptComponent(80, 5)
	// Set a small screen height so MaxHeight = 4 (50% of 8)
	prompt.SetScreenHeight(8)

	// Multi-line content should be capped at MaxHeight
	prompt.SetValue("Line 1\nLine 2\nLine 3")
	prompt.ViCurrentMode = ViModeInsert

	height := prompt.CalculateDesiredHeight()
	if height != 4 {
		t.Errorf("CalculateDesiredHeight() = %d, want %d (MaxHeight)", height, 4)
	}
}

// TestPromptHeightConfigurableExpandedHeight tests that ExpandedHeight can be configured
func TestPromptHeightConfigurableExpandedHeight(t *testing.T) {
	prompt := NewPromptComponent(80, 5)
	prompt.SetScreenHeight(40) // MaxHeight = 20

	// Set custom expanded height
	prompt.SetExpandedHeight(15)

	// Multi-line content should grow to configured expanded height
	prompt.SetValue("Line 1\nLine 2")
	prompt.ViCurrentMode = ViModeInsert

	height := prompt.CalculateDesiredHeight()
	if height != 15 {
		t.Errorf("CalculateDesiredHeight() = %d, want %d (custom ExpandedHeight)", height, 15)
	}
}

// TestPromptHeightClearedReturnsToMinimum tests that clearing the prompt returns height to 2
func TestPromptHeightClearedReturnsToMinimum(t *testing.T) {
	prompt := NewPromptComponent(80, 5)
	prompt.SetScreenHeight(40)

	// Start with multiline content
	prompt.SetValue("Line 1\nLine 2\nLine 3")
	prompt.ViCurrentMode = ViModeInsert

	height := prompt.CalculateDesiredHeight()
	if height != 10 {
		t.Errorf("CalculateDesiredHeight() with multiline = %d, want 10", height)
	}

	// Clear the prompt
	prompt.SetValue("")

	height = prompt.CalculateDesiredHeight()
	if height != 2 {
		t.Errorf("CalculateDesiredHeight() after clear = %d, want 2", height)
	}
}

// TestArrowKeysInViNormalMode tests that arrow keys work in vi normal mode
func TestArrowKeysInViNormalMode(t *testing.T) {
	prompt := NewPromptComponent(80, 5)

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
	prompt := NewPromptComponent(80, 5)

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
	prompt := NewPromptComponent(80, 5)

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
	prompt := NewPromptComponent(80, 5)

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

// TestAnsweringModeEditOption tests that the Edit option appears and emits the correct message
func TestAnsweringModeEditOption(t *testing.T) {
	prompt := NewPromptComponent(80, 10)

	// Create answering state with a question
	state := &AnsweringState{
		RequestID: "test-request-1",
		Title:     "Zhengming: Sage asks",
		Questions: []AnsweringQuestion{
			{
				Text:     "What would you like to do?",
				Summary:  "What to do?",
				Options:  []string{"Accept", tools.AnswerReject, "Edit", "Chat"},
				Selected: 0,
			},
		},
		Answers: make([]string, 1),
	}
	prompt.EnterAnsweringMode(state)

	// View should include "Edit" option
	view := prompt.View()
	if !strings.Contains(view, "Edit") {
		t.Error("View should contain 'Edit' option")
	}

	// Navigate down to Edit option (Options: Accept, Reject, Chat, Edit)
	// Current selected is 0 (Accept), down to 1 (Reject), down to 2 (Chat), down to 3 (Edit)
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	prompt, _ = prompt.Update(downMsg) // Selected = 1
	prompt, _ = prompt.Update(downMsg) // Selected = 2

	// Press Enter to select Edit
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := prompt.Update(enterMsg)

	// Should emit AnsweringEditMsg
	if cmd == nil {
		t.Fatal("Expected cmd to emit AnsweringEditMsg")
	}

	// Execute the command to get the message
	msg := cmd()
	if editMsg, ok := msg.(AnsweringEditMsg); ok {
		if editMsg.RequestID != "test-request-1" {
			t.Errorf("Expected RequestID 'test-request-1', got %q", editMsg.RequestID)
		}
		if editMsg.Question != "What to do?" {
			t.Errorf("Expected Question 'What to do?' (summary), got %q", editMsg.Question)
		}
	} else {
		t.Fatalf("Expected AnsweringEditMsg, got %T", msg)
	}
}

// TestAnsweringModeEditUpdatesQuestion tests that UpdateAnsweringQuestion updates the current question
func TestAnsweringModeEditUpdatesQuestion(t *testing.T) {
	prompt := NewPromptComponent(80, 10)

	// Create answering state
	state := &AnsweringState{
		RequestID: "test-request-2",
		Title:     "Zhengming: Sage asks",
		Questions: []AnsweringQuestion{
			{
				Text:     "Original question text",
				Summary:  "Original",
				Options:  []string{"Yes", "No"},
				Selected: 0,
			},
		},
		Answers: make([]string, 1),
	}
	prompt.EnterAnsweringMode(state)

	// Update the question
	modifiedText := "Modified question text"
	prompt.UpdateAnsweringQuestion(modifiedText)

	// Verify the question was updated
	if prompt.answering.Questions[0].Text != modifiedText {
		t.Errorf("Expected Text %q, got %q", modifiedText, prompt.answering.Questions[0].Text)
	}
	if prompt.answering.Questions[0].Summary != modifiedText {
		t.Errorf("Expected Summary %q, got %q", modifiedText, prompt.answering.Questions[0].Summary)
	}

	// Selection should be reset to 0
	if prompt.answering.Questions[0].Selected != 0 {
		t.Errorf("Expected Selected to be reset to 0, got %d", prompt.answering.Questions[0].Selected)
	}
}

// TestHandleZhengmingPendingAppendsEditChat verifies that HandleZhengmingPending
// appends "Edit" and "Chat" to each question's Options.
func TestHandleZhengmingPendingAppendsEditChat(t *testing.T) {
	prompt := NewPromptComponent(80, 10)

	msg := shogunate.ZhengmingPendingMsg{
		RequestID:  "zhengming-1",
		MinisterID: "sage",
		Questions: storage.ZhengmingQuestions{
			{Text: "Which approach?", Summary: "Approach?", Options: []string{"Option A", "Option B"}},
			{Text: "Confirm?", Summary: "Confirm?", Options: []string{"Yes", "No"}},
		},
	}
	prompt.HandleZhengmingPending(msg)

	if prompt.answering == nil {
		t.Fatal("Expected answering state to be set")
	}
	if prompt.answering.RequestID != "zhengming-1" {
		t.Errorf("Expected RequestID 'zhengming-1', got %q", prompt.answering.RequestID)
	}
	if len(prompt.answering.Questions) != 2 {
		t.Fatalf("Expected 2 questions, got %d", len(prompt.answering.Questions))
	}

	for i, q := range prompt.answering.Questions {
		expected := len(msg.Questions[i].Options) + 2 // +Edit +Chat
		if len(q.Options) != expected {
			t.Errorf("Question %d: expected %d options, got %d", i, expected, len(q.Options))
		}
		if q.Options[len(q.Options)-2] != "Edit" {
			t.Errorf("Question %d: expected second-to-last option 'Edit', got %q", i, q.Options[len(q.Options)-2])
		}
		if q.Options[len(q.Options)-1] != "Chat" {
			t.Errorf("Question %d: expected last option 'Chat', got %q", i, q.Options[len(q.Options)-1])
		}
	}
}

// TestAnsweringModeChatOption verifies that selecting "Chat" emits AnsweredMsg with AnswerChat.
func TestAnsweringModeChatOption(t *testing.T) {
	prompt := NewPromptComponent(80, 10)

	state := &AnsweringState{
		RequestID: "test-chat-1",
		Title:     "Zhengming: Sage asks",
		Questions: []AnsweringQuestion{
			{
				Text:     "Pick one",
				Summary:  "Pick",
				Options:  []string{"Accept", tools.AnswerReject, "Edit", "Chat"},
				Selected: 0,
			},
		},
		Answers: make([]string, 1),
	}
	prompt.EnterAnsweringMode(state)

	// Navigate to "Chat" (index 3)
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	prompt, _ = prompt.Update(downMsg) // Selected = 1
	prompt, _ = prompt.Update(downMsg) // Selected = 2
	prompt, _ = prompt.Update(downMsg) // Selected = 3 (Chat)

	// Press Enter to select Chat
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := prompt.Update(enterMsg)

	if cmd == nil {
		t.Fatal("Expected cmd to emit AnsweredMsg")
	}
	msg := cmd()
	answered, ok := msg.(AnsweredMsg)
	if !ok {
		t.Fatalf("Expected AnsweredMsg, got %T", msg)
	}
	if answered.RequestID != "test-chat-1" {
		t.Errorf("Expected RequestID 'test-chat-1', got %q", answered.RequestID)
	}
	if len(answered.Answers) != 1 || answered.Answers[0] != tools.AnswerChat {
		t.Errorf("Expected Answers [%q], got %v", tools.AnswerChat, answered.Answers)
	}
}

// TestCalculateDesiredHeightAnsweringMode verifies the height calculation in answering mode.
func TestCalculateDesiredHeightAnsweringMode(t *testing.T) {
	prompt := NewPromptComponent(80, 10)
	prompt.SetScreenHeight(40) // MaxHeight = 20

	state := &AnsweringState{
		RequestID: "test-height-1",
		Title:     "Zhengming",
		Questions: []AnsweringQuestion{
			{
				Text:    "Question?",
				Summary: "Q?",
				Options: []string{"A", "B", "C"},
			},
		},
		Answers: make([]string, 1),
	}
	prompt.EnterAnsweringMode(state)

	// Expected: 3 (title + blank + question) + 3 (options) = 6
	height := prompt.CalculateDesiredHeight()
	if height != 6 {
		t.Errorf("Expected height 6 (3 + 3 options), got %d", height)
	}
}
