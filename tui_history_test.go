package main

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// TestHistoryNavigation_EmptyHistory tests navigation with no history
func TestHistoryNavigation_EmptyHistory(t *testing.T) {
	model, _ := newTestModel(t)

	// Press up arrow with empty history
	handled, cmd := model.handleHistoryNavigation(-1)
	require.False(t, handled, "Should not handle navigation with empty history")
	require.Nil(t, cmd)

	// Press down arrow with empty history
	handled, cmd = model.handleHistoryNavigation(1)
	require.False(t, handled, "Should not handle navigation with empty history")
	require.Nil(t, cmd)
}

// TestHistoryNavigation_SingleEntry tests navigation with one history entry
func TestHistoryNavigation_SingleEntry(t *testing.T) {
	model, _ := newTestModel(t)

	// Add one history entry
	model.promptHistory = []promptHistoryEntry{
		{Prompt: "first prompt", SessionSnapshot: 1, ChatSnapshot: 0},
	}
	model.historyCursor = 1 // At present

	// Navigate up (to first entry)
	handled, cmd := model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 0, model.historyCursor)
	require.Equal(t, "first prompt", model.prompt.Value())
	require.True(t, model.historySaved, "Should save present state")

	// Try to navigate up again (should stay at first entry)
	handled, cmd = model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 0, model.historyCursor)

	// Navigate down (back to present)
	handled, cmd = model.handleHistoryNavigation(1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 1, model.historyCursor)
	require.False(t, model.historySaved, "Should clear saved state when returning to present")
}

// TestHistoryNavigation_MultipleEntries tests navigation through multiple entries
func TestHistoryNavigation_MultipleEntries(t *testing.T) {
	model, _ := newTestModel(t)
	model.prompt.SetValue("current input")

	// Add multiple history entries
	model.promptHistory = []promptHistoryEntry{
		{Prompt: "first prompt", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second prompt", SessionSnapshot: 3, ChatSnapshot: 2},
		{Prompt: "third prompt", SessionSnapshot: 5, ChatSnapshot: 4},
	}
	model.historyCursor = 3 // At present

	// Navigate up once
	handled, cmd := model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 2, model.historyCursor)
	require.Equal(t, "third prompt", model.prompt.Value())
	require.True(t, model.historySaved)
	require.Equal(t, "current input", model.historyPendingPrompt)

	// Navigate up again
	handled, cmd = model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 1, model.historyCursor)
	require.Equal(t, "second prompt", model.prompt.Value())

	// Navigate up to first
	handled, cmd = model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 0, model.historyCursor)
	require.Equal(t, "first prompt", model.prompt.Value())

	// Try to navigate up past first (should stay at first)
	handled, cmd = model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 0, model.historyCursor)
	require.Equal(t, "first prompt", model.prompt.Value())

	// Navigate down
	handled, cmd = model.handleHistoryNavigation(1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 1, model.historyCursor)
	require.Equal(t, "second prompt", model.prompt.Value())

	// Navigate down to third
	handled, cmd = model.handleHistoryNavigation(1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 2, model.historyCursor)
	require.Equal(t, "third prompt", model.prompt.Value())

	// Navigate down to present
	handled, cmd = model.handleHistoryNavigation(1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 3, model.historyCursor)
	require.Equal(t, "current input", model.prompt.Value())
	require.False(t, model.historySaved)
}

// TestHistoryNavigation_DownWithoutSavedState tests down navigation without saved state
func TestHistoryNavigation_DownWithoutSavedState(t *testing.T) {
	model, _ := newTestModel(t)

	model.promptHistory = []promptHistoryEntry{
		{Prompt: "first prompt", SessionSnapshot: 1, ChatSnapshot: 0},
	}
	model.historyCursor = 1 // At present
	model.historySaved = false

	// Try to navigate down when already at present
	handled, cmd := model.handleHistoryNavigation(1)
	require.False(t, handled, "Should not handle down when not in history")
	require.Nil(t, cmd)
}

// TestHistoryNavigation_CursorInitialization tests cursor initialization from present
func TestHistoryNavigation_CursorInitialization(t *testing.T) {
	model, _ := newTestModel(t)
	model.prompt.SetValue("current")

	model.promptHistory = []promptHistoryEntry{
		{Prompt: "first", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second", SessionSnapshot: 3, ChatSnapshot: 2},
	}
	model.historyCursor = len(model.promptHistory) // At present

	// First up navigation should go to last entry
	handled, cmd := model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, 1, model.historyCursor)
	require.Equal(t, "second", model.prompt.Value())
}

// TestWaitingIndicator_StartStop tests the waiting indicator lifecycle
func TestWaitingIndicator_StartStop(t *testing.T) {
	model, _ := newTestModel(t)

	// Initially not waiting
	require.False(t, model.waitingForResponse)
	require.True(t, model.waitingStart.IsZero())

	// Start waiting
	cmd := model.startWaitingForResponse()
	require.True(t, model.waitingForResponse)
	require.False(t, model.waitingStart.IsZero())
	require.NotNil(t, cmd, "Should return tick command")

	// Verify status component was updated
	require.True(t, model.status.waitingForResponse)

	// Stop waiting
	model.stopStreaming()
	require.False(t, model.waitingForResponse)
	require.False(t, model.status.waitingForResponse)
}

// TestWaitingIndicator_DoubleStart tests starting waiting when already waiting
func TestWaitingIndicator_DoubleStart(t *testing.T) {
	model, _ := newTestModel(t)

	// Start waiting
	cmd1 := model.startWaitingForResponse()
	require.NotNil(t, cmd1)
	startTime := model.waitingStart

	// Try to start again
	cmd2 := model.startWaitingForResponse()
	require.Nil(t, cmd2, "Should not return command when already waiting")
	require.Equal(t, startTime, model.waitingStart, "Start time should not change")
}

// TestWaitingIndicator_DoubleStop tests stopping when not waiting
func TestWaitingIndicator_DoubleStop(t *testing.T) {
	model, _ := newTestModel(t)

	// Stop when not waiting (should not panic)
	model.stopStreaming()
	require.False(t, model.waitingForResponse)
}

// TestWaitingTickMsg_WhileWaiting tests waiting tick message handling
func TestWaitingTickMsg_WhileWaiting(t *testing.T) {
	model, _ := newTestModel(t)

	// Start waiting
	model.startWaitingForResponse()

	// Handle tick message
	newModel, cmd := model.handleCustomMessages(waitingTickMsg{})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.NotNil(t, cmd, "Should return next tick command")

	// Verify still waiting
	require.True(t, updatedModel.waitingForResponse)
}

// TestWaitingTickMsg_NotWaiting tests waiting tick when not waiting
func TestWaitingTickMsg_NotWaiting(t *testing.T) {
	model, _ := newTestModel(t)

	// Handle tick message when not waiting
	newModel, cmd := model.handleCustomMessages(waitingTickMsg{})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Nil(t, cmd, "Should not return tick command when not waiting")
	require.False(t, updatedModel.waitingForResponse)
}

// TestHistoryRollback_OnSubmit tests that submitting a historical prompt rolls back state
func TestHistoryRollback_OnSubmit(t *testing.T) {
	model, _ := newTestModel(t)

	// Clear the welcome message for cleaner testing
	model.chat.Messages = []string{}
	model.chat.UpdateContent()

	// Simulate a conversation
	model.chat.AddMessage("You: first")
	model.chat.AddMessage("Asimi: response1")
	model.promptHistory = append(model.promptHistory, promptHistoryEntry{
		Prompt:          "first",
		SessionSnapshot: 1,
		ChatSnapshot:    0, // Before adding messages
	})

	model.chat.AddMessage("You: second")
	model.chat.AddMessage("Asimi: response2")
	model.promptHistory = append(model.promptHistory, promptHistoryEntry{
		Prompt:          "second",
		SessionSnapshot: 1, // Session hasn't changed (no actual LLM calls)
		ChatSnapshot:    2, // After first conversation
	})

	model.historyCursor = len(model.promptHistory)

	// Navigate to first prompt
	model.handleHistoryNavigation(-1) // to "second"
	model.handleHistoryNavigation(-1) // to "first"

	require.Equal(t, 0, model.historyCursor)
	require.Equal(t, "first", model.prompt.Value())
	require.True(t, model.historySaved)

	// Simulate submitting the historical prompt
	chatLenBefore := len(model.chat.Messages)
	sessionLenBefore := len(model.session.messages)

	// The handleEnterKey function should detect historySaved and roll back
	// We'll test the rollback logic directly
	if model.historySaved && model.historyCursor < len(model.promptHistory) {
		entry := model.promptHistory[model.historyCursor]
		model.session.RollbackTo(entry.SessionSnapshot)
		model.chat.TruncateTo(entry.ChatSnapshot)
	}

	// Verify rollback occurred
	require.Equal(t, 1, len(model.session.messages), "Session should be rolled back to system message")
	require.Equal(t, 0, len(model.chat.Messages), "Chat should be rolled back to empty")
	require.Less(t, len(model.chat.Messages), chatLenBefore)
	require.Equal(t, len(model.session.messages), sessionLenBefore) // Session didn't change in this test
}

// TestNewSessionCommand_ResetsHistory tests that /new command resets history
func TestNewSessionCommand_ResetsHistory(t *testing.T) {
	model, _ := newTestModel(t)

	// Add some history
	model.promptHistory = []promptHistoryEntry{
		{Prompt: "first", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second", SessionSnapshot: 3, ChatSnapshot: 2},
	}
	model.historyCursor = 1
	model.historySaved = true
	model.historyPendingPrompt = "pending"

	// Start waiting
	model.startWaitingForResponse()
	require.True(t, model.waitingForResponse)

	// Execute /new command
	handleNewSessionCommand(model, []string{})

	// Verify history was reset
	require.Empty(t, model.promptHistory)
	require.Equal(t, 0, model.historyCursor)
	require.False(t, model.historySaved)
	require.Empty(t, model.historyPendingPrompt)
	require.False(t, model.waitingForResponse)
}

// TestHistoryNavigation_WithArrowKeys tests arrow key handling
func TestHistoryNavigation_WithArrowKeys(t *testing.T) {
	model, _ := newTestModel(t)

	// Add history
	model.promptHistory = []promptHistoryEntry{
		{Prompt: "first", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second", SessionSnapshot: 3, ChatSnapshot: 2},
	}
	model.historyCursor = 2
	model.prompt.SetValue("current")

	// Simulate up arrow on first line (cursor at start)
	model.prompt.TextArea.CursorStart()
	newModel, cmd := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyUp})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Nil(t, cmd)
	require.Equal(t, 1, updatedModel.historyCursor)
	require.Equal(t, "second", updatedModel.prompt.Value())

	// Simulate down arrow on last line (cursor at end)
	updatedModel.prompt.TextArea.CursorEnd()
	newModel, cmd = updatedModel.handleKeyMsg(tea.KeyMsg{Type: tea.KeyDown})
	updatedModel, ok = newModel.(TUIModel)
	require.True(t, ok)
	require.Nil(t, cmd)
	require.Equal(t, 2, updatedModel.historyCursor)
	require.Equal(t, "current", updatedModel.prompt.Value())
}

// TestCancelActiveStreaming tests the streaming cancellation helper
func TestCancelActiveStreaming(t *testing.T) {
	model, _ := newTestModel(t)

	// Set up active streaming
	model.streamingActive = true
	cancelCalled := false
	model.streamingCancel = func() {
		cancelCalled = true
	}

	// Cancel streaming
	model.cancelStreaming()

	require.True(t, cancelCalled, "Cancel function should be called")
	require.False(t, model.streamingActive)
	require.Nil(t, model.streamingCancel)
}

// TestCancelActiveStreaming_NotActive tests cancellation when not streaming
func TestCancelActiveStreaming_NotActive(t *testing.T) {
	model, _ := newTestModel(t)

	// Not streaming
	model.streamingActive = false
	model.streamingCancel = nil

	// Should not panic
	model.cancelStreaming()

	require.False(t, model.streamingActive)
	require.Nil(t, model.streamingCancel)
}

// TestSaveHistoryPresentState tests saving the present state
func TestSaveHistoryPresentState(t *testing.T) {
	model, _ := newTestModel(t)
	model.prompt.SetValue("current prompt")
	model.chat.AddMessage("message 1")
	model.chat.AddMessage("message 2")

	// Save present state
	model.saveHistoryPresentState()

	require.True(t, model.historySaved)
	require.Equal(t, "current prompt", model.historyPendingPrompt)
	// Chat has welcome message + 2 added messages = 3 total
	require.Equal(t, 3, model.historyPresentChatSnapshot)
	require.Equal(t, 1, model.historyPresentSessionSnapshot) // System message only

	// Try to save again (should not change)
	model.prompt.SetValue("different")
	model.saveHistoryPresentState()
	require.Equal(t, "current prompt", model.historyPendingPrompt, "Should not update when already saved")
}

// TestRestoreHistoryPresent tests restoring the present state
func TestRestoreHistoryPresent(t *testing.T) {
	model, _ := newTestModel(t)
	model.prompt.SetValue("current")
	model.historyPendingPrompt = "pending"
	model.historySaved = true

	// Restore present
	model.restoreHistoryPresent()

	require.Equal(t, "pending", model.prompt.Value())
	require.False(t, model.historySaved)
}

// TestApplyHistoryEntry tests applying a history entry
func TestApplyHistoryEntry(t *testing.T) {
	model, _ := newTestModel(t)
	model.prompt.SetValue("current")

	entry := promptHistoryEntry{
		Prompt:          "historical prompt",
		SessionSnapshot: 5,
		ChatSnapshot:    3,
	}

	// Apply entry
	model.applyHistoryEntry(entry)

	require.Equal(t, "historical prompt", model.prompt.Value())
}

// TestStatusComponent_WaitingIndicator tests the status component waiting indicator
func TestStatusComponent_WaitingIndicator(t *testing.T) {
	status := NewStatusComponent(80)

	// Initially not waiting
	require.False(t, status.waitingForResponse)

	// Start waiting
	status.StartWaiting()
	require.True(t, status.waitingForResponse)
	require.False(t, status.waitingSince.IsZero())

	// Stop waiting
	status.StopWaiting()
	require.False(t, status.waitingForResponse)
}

// TestStatusComponent_WaitingIndicatorView tests the waiting indicator in the view
func TestStatusComponent_WaitingIndicatorView(t *testing.T) {
	status := NewStatusComponent(200) // Use very wide width to avoid truncation
	status.SetProvider("test", "model", true)

	// Create a mock session to provide usage data
	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	require.NoError(t, err)
	status.SetSession(sess)

	// View without waiting
	middleSection := status.renderMiddleSection()
	require.NotContains(t, middleSection, "⏳")

	// Start waiting (less than 3 seconds ago)
	status.StartWaiting()
	status.waitingSince = time.Now().Add(-2 * time.Second)
	middleSection = status.renderMiddleSection()
	require.NotContains(t, middleSection, "⏳", "Should not show indicator before 3 seconds")

	// Waiting for more than 3 seconds - check middle section directly
	status.StartWaiting()
	status.waitingSince = time.Now().Add(-5 * time.Second)
	middleSection = status.renderMiddleSection()
	require.Contains(t, middleSection, "⏳", "Middle section should contain waiting indicator")
	require.Contains(t, middleSection, "5s", "Middle section should show elapsed time")
}

// TestEscapeDuringStreaming_StopsWaiting tests that ESC during streaming stops waiting
func TestEscapeDuringStreaming_StopsWaiting(t *testing.T) {
	model, _ := newTestModel(t)

	// Set up streaming
	model.streamingActive = true
	cancelCalled := false
	model.streamingCancel = func() {
		cancelCalled = true
	}

	// Start waiting
	model.startWaitingForResponse()
	require.True(t, model.waitingForResponse)

	// Press escape
	newModel, _ := model.handleEscape()
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)

	require.True(t, cancelCalled)
	require.False(t, updatedModel.waitingForResponse)
}

// TestStreamChunkMsg_StopsWaiting tests that receiving a stream chunk resets the quiet time timer
func TestStreamChunkMsg_StopsWaiting(t *testing.T) {
	model, _ := newTestModel(t)

	// Start waiting and mark as streaming
	model.startWaitingForResponse()
	model.streamingActive = true
	require.True(t, model.waitingForResponse)

	// Record the initial wait start time
	initialWaitStart := model.waitingStart

	// Wait a bit to ensure time passes
	time.Sleep(10 * time.Millisecond)

	// Receive stream chunk - should reset the waiting timer
	newModel, _ := model.handleCustomMessages(streamChunkMsg("chunk"))
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)

	// Waiting should still be active (for tracking quiet time)
	require.True(t, updatedModel.waitingForResponse)
	// But the timer should have been reset (waitingStart should be newer)
	require.True(t, updatedModel.waitingStart.After(initialWaitStart), "Waiting timer should be reset when chunk arrives")
}

// TestStreamCompleteMsg_StopsWaiting tests that stream completion stops waiting
func TestStreamCompleteMsg_StopsWaiting(t *testing.T) {
	model, _ := newTestModel(t)

	// Start waiting
	model.startWaitingForResponse()
	require.True(t, model.waitingForResponse)

	// Stream completes
	newModel, _ := model.handleCustomMessages(streamCompleteMsg{})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)

	require.False(t, updatedModel.waitingForResponse)
}

// TestStreamErrorMsg_StopsWaiting tests that stream error stops waiting
func TestStreamErrorMsg_StopsWaiting(t *testing.T) {
	model, _ := newTestModel(t)

	// Start waiting
	model.startWaitingForResponse()
	require.True(t, model.waitingForResponse)

	// Stream error
	testErr := errors.New("test error")
	newModel, _ := model.handleCustomMessages(streamErrorMsg{err: testErr})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)

	require.False(t, updatedModel.waitingForResponse)
}

// TestHistoryNavigation_RapidNavigation tests rapid navigation through history
func TestHistoryNavigation_RapidNavigation(t *testing.T) {
	model, _ := newTestModel(t)

	// Add many history entries
	for i := 0; i < 10; i++ {
		model.promptHistory = append(model.promptHistory, promptHistoryEntry{
			Prompt:          "prompt " + string(rune('0'+i)),
			SessionSnapshot: i*2 + 1,
			ChatSnapshot:    i * 2,
		})
	}
	model.historyCursor = len(model.promptHistory)
	model.prompt.SetValue("current")

	// Rapidly navigate up
	for i := 0; i < 10; i++ {
		model.handleHistoryNavigation(-1)
	}
	require.Equal(t, 0, model.historyCursor)
	require.Equal(t, "prompt 0", model.prompt.Value())

	// Rapidly navigate down
	for i := 0; i < 10; i++ {
		model.handleHistoryNavigation(1)
	}
	require.Equal(t, 10, model.historyCursor)
	require.Equal(t, "current", model.prompt.Value())
	require.False(t, model.historySaved)
}
