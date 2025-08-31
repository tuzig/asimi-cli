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

	// Get file list to find main.go
	files, err := getFileTree(".")
	require.NoError(t, err)
	mainGoIndex := -1
	for i, f := range files {
		if f == "main.go" {
			mainGoIndex = i
			break
		}
	}
	require.NotEqual(t, -1, mainGoIndex, "main.go not found in file tree")

	// Simulate typing "@"
	tm.Type("@")

	// Wait for the completion dialog to appear
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return strings.Contains(string(bts), "@main.go")
	}, teatest.WithCheckInterval(time.Millisecond*100), teatest.WithDuration(time.Second*3))

	// Simulate pressing tab to select "@main.go"
	for i := 0; i < mainGoIndex; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	}

	// Simulate pressing enter to select the file
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
	require.Contains(t, tuiModel.filesContentToSend["main.go"], "package main")

	// Assert that the prompt was not sent and the editor is still focused
	require.NotEmpty(t, tuiModel.messages.Messages)
	require.Contains(t, tuiModel.messages.Messages[len(tuiModel.messages.Messages)-1], "Loaded file: main.go")
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
