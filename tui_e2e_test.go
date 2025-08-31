package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/stretchr/testify/require"
)

func TestFileCompletion(t *testing.T) {
	// Create a new TUI model for testing
	config := mockConfig()
	handler := &toolCallbackHandler{}
	model := NewTUIModel(config, handler)

	// Create a new test model
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(200, 200))

	// Simulate typing "@"
	tm.Type("@")

	// Wait for the completion dialog to appear
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return strings.Contains(string(bts), "@main.go")
	}, teatest.WithCheckInterval(time.Millisecond*100), teatest.WithDuration(time.Second*3))

	// Simulate pressing enter to select the first file
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Wait for a bit to let the file be read
	time.Sleep(100 * time.Millisecond)

	// Quit the application
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Get the final model
	finalModel := tm.FinalModel(t)
	tuiModel, ok := finalModel.(TUIModel)
	require.True(t, ok)

	// Assert that the file viewer contains the file content
	require.Contains(t, tuiModel.fileContentViewer.Content, "package main")

	// Assert that the prompt was not sent and the editor is still focused
	require.Len(t, tuiModel.messages.Messages, 1, "A prompt should not have been sent to the LLM")
	require.True(t, tuiModel.editor.TextArea.Focused(), "The editor should remain focused")
}

func TestSlashCommandCompletion(t *testing.T) {
	// Create a new TUI model for testing
	config := mockConfig()
	handler := &toolCallbackHandler{}
	model := NewTUIModel(config, handler)

	// Create a new test model
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(200, 200))

	// Simulate typing "/"
	tm.Type("/")

	// Wait for the completion dialog to appear
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return strings.Contains(string(bts), "/help")
	}, teatest.WithCheckInterval(time.Millisecond*100), teatest.WithDuration(time.Second*3))

	// Simulate pressing enter to select the first command
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Wait for a bit to let the command be executed
	time.Sleep(100 * time.Millisecond)

	// Quit the application
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Get the final model
	finalModel := tm.FinalModel(t)
	tuiModel, ok := finalModel.(TUIModel)
	require.True(t, ok)

	// Assert that the messages contain the help text
	require.Contains(t, tuiModel.messages.Messages[len(tuiModel.messages.Messages)-1], "Available commands:")
}
