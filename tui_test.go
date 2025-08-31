package main

import (
	"testing"
	time "time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// mockLLM is a mock LLM implementation for testing
type mockLLM struct{}

// mockConfig returns a mock configuration for testing
func mockConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "localhost",
			Port: 3000,
		},
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "asimi",
			Password: "asimi",
			Name:     "asimi_dev",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		LLM: LLMConfig{
			Provider: "fake",
			Model:    "mock-model",
			APIKey:   "",
			BaseURL:  "",
		},
	}
}

// TestTUIModelInit tests the initialization of the TUI model
func TestTUIModelInit(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)
	cmd := model.Init()

	// Init should return nil as there's no initial command
	require.Nil(t, cmd)
}

// TestTUIModelWindowSizeMsg tests handling of window size messages
func TestTUIModelWindowSizeMsg(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)

	// Send a window size message
	newModel, cmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Equal(t, 100, updatedModel.width)
	require.Equal(t, 50, updatedModel.height)
	require.Nil(t, cmd)
}

// TestTUIModelKeyMsgQuit tests quitting the application with 'q'
func TestTUIModelKeyMsgQuit(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)

	// Send a quit key message
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

	// Should return quit command
	require.NotNil(t, cmd)

	// Execute the command to verify it's a quit command
	result := cmd()
	_, ok := result.(tea.QuitMsg)
	require.True(t, ok)

	// Model should be unchanged
	_, ok = newModel.(TUIModel)
	require.True(t, ok)
}

// TestTUIModelKeyMsgCtrlC tests quitting the application with Ctrl+C
func TestTUIModelKeyMsgCtrlC(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)

	// Send a Ctrl+C key message
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Should return quit command
	require.NotNil(t, cmd)

	// Execute the command to verify it's a quit command
	result := cmd()
	_, ok := result.(tea.QuitMsg)
	require.True(t, ok)

	// Model should be unchanged
	_, ok = newModel.(TUIModel)
	require.True(t, ok)
}

// TestTUIModelKeyMsgEnterEmpty tests submitting an empty message
func TestTUIModelKeyMsgEnterEmpty(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)

	// Ensure editor is empty
	model.editor.SetValue("")

	// Send an enter key message
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	require.Nil(t, cmd)

	// Messages should remain the same since no content was submitted
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Equal(t, 1, len(updatedModel.messages.Messages))
	require.Equal(t, "Welcome to Asimi CLI! Send a message to start chatting.", updatedModel.messages.Messages[0])
	// Editor should be cleared (might have a newline)
	require.Contains(t, []string{"", "\n"}, updatedModel.editor.Value())
}

// TestTUIModelKeyMsgEnterWithText tests submitting a message with text
func TestTUIModelKeyMsgEnterWithText(t *testing.T) {
	// Create a mock agent for testing
	agent, err := getAgent(&Config{
		LLM: LLMConfig{
			Provider: "fake",
		},
	}, nil)
	require.NoError(t, err)

	model := NewTUIModel(mockConfig(), &toolCallbackHandler{})
	model.agent = agent

	// Set some text in the editor
	testMessage := "Hello, Asimi!"
	model.editor.SetValue(testMessage)

	// Send an enter key message
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)

	// Simulate the command execution
	msg := cmd()
	newModel, cmd = newModel.Update(msg)
	require.Nil(t, cmd)

	// Should have added the user message and AI response
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Equal(t, 3, len(updatedModel.messages.Messages))
	require.Equal(t, "You: "+testMessage, updatedModel.messages.Messages[1])
	require.Equal(t, "AI: I am a large language model, trained by Google.", updatedModel.messages.Messages[2])
}

// TestTUIModelKeyMsgCommand tests submitting a command
func TestTUIModelKeyMsgCommand(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)

	// Set a command in the editor
	command := "/help"
	model.editor.SetValue(command)

	// Send an enter key message
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	require.NotNil(t, cmd)

	// Simulate the command execution
	msg := cmd()
	newModel, cmd = newModel.Update(msg)
	require.Nil(t, cmd)

	// Should have added the help message
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Contains(t, updatedModel.messages.Messages[len(updatedModel.messages.Messages)-1], "Available commands:")
}

// TestTUIModelKeyMsgEsc tests the escape key functionality
func TestTUIModelKeyMsgEsc(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)

	// Set up modal to be active
	model.modal = NewBaseModal("Test", "Test content", 30, 10)

	// Show completion dialog
	model.showCompletionDialog = true

	// Activate file viewer
	if model.fileViewer != nil {
		model.fileViewer.Active = true
	}

	// Send escape key message
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	require.Nil(t, cmd)

	// Check updated model
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)

	// Modal should be closed
	require.Nil(t, updatedModel.modal)

	// Completion dialog should be hidden
	require.False(t, updatedModel.showCompletionDialog)

	// File viewer should be closed
	if updatedModel.fileViewer != nil {
		require.False(t, updatedModel.fileViewer.Active)
	}

	// Should still be a TUIModel
	_, ok = newModel.(TUIModel)
	require.True(t, ok)
}

// TestTUIModelKeyMsgTab tests tab navigation in completion dialog
func TestTUIModelKeyMsgTab(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)

	// Show completion dialog with options
	model.showCompletionDialog = true
	model.completions.SetOptions([]string{"option1", "option2", "option3"})
	model.completions.Show()

	// Initial selection should be 0
	require.Equal(t, 0, model.completions.Selected)

	// Send tab key message
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyTab})

	require.Nil(t, cmd)

	// Selection should move to next item
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Equal(t, 1, updatedModel.completions.Selected)
}

// TestTUIModelKeyMsgShiftTab tests shift+tab navigation in completion dialog
func TestTUIModelKeyMsgShiftTab(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)

	// Show completion dialog with options
	model.showCompletionDialog = true
	model.completions.SetOptions([]string{"option1", "option2", "option3"})
	model.completions.Show()

	// Set selection to middle item
	model.completions.Selected = 1

	// Send shift+tab key message
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})

	require.Nil(t, cmd)

	// Selection should move to previous item
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Equal(t, 0, updatedModel.completions.Selected)
}

// TestTUIModelView tests the view rendering
func TestTUIModelView(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil)

	// Test view rendering with default dimensions (should not show initializing)
	view := model.View()
	require.NotEmpty(t, view)
	require.NotContains(t, view, "Initializing...")

	// Create a model with zero dimensions to test initializing message
	model.width = 0
	model.height = 0
	view = model.View()
	require.NotEmpty(t, view)
	require.Contains(t, view, "Initializing...")

	// Set dimensions and test again
	model.width = 80
	model.height = 24
	view = model.View()
	require.NotEmpty(t, view)
	require.Contains(t, view, "Asimi CLI - Interactive Coding Agent")

	// Activate session and test chat view
	model.sessionActive = true
	view = model.View()
	require.NotEmpty(t, view)
	require.Contains(t, view, "Asimi CLI - Chat Session")
}

// TestEditorComponent tests the editor component
func TestEditorComponent(t *testing.T) {
	editor := NewEditorComponent(50, 10)

	// Test setting and getting value
	testValue := "Test content"
	editor.SetValue(testValue)
	require.Equal(t, testValue, editor.Value())

	// Test dimensions
	editor.SetWidth(60)
	require.Equal(t, 60, editor.Width)

	editor.SetHeight(15)
	require.Equal(t, 15, editor.Height)
}

// TestMessagesComponent tests the messages component
func TestMessagesComponent(t *testing.T) {
	messages := NewMessagesComponent(50, 10)

	// Should have initial welcome message
	require.Equal(t, 1, len(messages.Messages))
	require.Equal(t, "Welcome to Asimi CLI! Send a message to start chatting.", messages.Messages[0])

	// Test adding a message
	testMessage := "Test message"
	messages.AddMessage(testMessage)
	require.Equal(t, 2, len(messages.Messages))
	require.Equal(t, testMessage, messages.Messages[1])

	// Test dimensions
	messages.SetWidth(60)
	require.Equal(t, 60, messages.Width)

	messages.SetHeight(15)
	require.Equal(t, 15, messages.Height)
}

// TestCompletionDialog tests the completion dialog
func TestCompletionDialog(t *testing.T) {
	dialog := NewCompletionDialog()

	// Initially should be invisible
	require.False(t, dialog.Visible)
	require.Empty(t, dialog.Options)
	require.Equal(t, 0, dialog.Selected)

	// Test setting options
	options := []string{"option1", "option2", "option3"}
	dialog.SetOptions(options)
	require.Equal(t, options, dialog.Options)

	// Test showing and hiding
	dialog.Show()
	require.True(t, dialog.Visible)

	dialog.Hide()
	require.False(t, dialog.Visible)

	// Test selection navigation
	dialog.Show()
	dialog.SetOptions(options)

	dialog.SelectNext()
	require.Equal(t, 1, dialog.Selected)

	dialog.SelectNext()
	require.Equal(t, 2, dialog.Selected)

	dialog.SelectNext()
	require.Equal(t, 0, dialog.Selected) // Should wrap around

	dialog.SelectPrev()
	require.Equal(t, 2, dialog.Selected) // Should wrap around

	// Test getting selected option
	dialog.Selected = 1
	require.Equal(t, "option2", dialog.GetSelected())

	// Test view rendering
	// When not visible, should be empty
	dialog.Hide()
	view := dialog.View()
	require.Empty(t, view)

	// When visible but no options, should be empty
	dialog.Show()
	dialog.SetOptions([]string{})
	view = dialog.View()
	require.Empty(t, view)

	// When visible with options, should contain the options
	dialog.SetOptions(options)
	view = dialog.View()
	require.NotEmpty(t, view)
	// When visible, should contain the options
	for _, option := range options {
		require.Contains(t, view, option)
	}
}

// TestStatusComponent tests the status component
func TestStatusComponent(t *testing.T) {
	status := NewStatusComponent(50)

	// Test setting properties
	status.SetAgent("test-agent")
	status.SetWorkingDir("/test/dir")
	status.SetGitBranch("main")

	// Test width
	status.SetWidth(60)
	require.Equal(t, 60, status.Width)

	// Test view rendering
	view := status.View()
	require.NotEmpty(t, view)
	require.Contains(t, view, "test-agent")
	require.Contains(t, view, "/test/dir")
	require.Contains(t, view, "main")
}

// TestFileViewer tests the file viewer component
func TestFileViewer(t *testing.T) {
	viewer := NewFileViewer(50, 10)

	// Initially should not be active
	require.False(t, viewer.Active)
	require.Empty(t, viewer.FilePath)
	require.Empty(t, viewer.Content)

	// Test loading a file
	testPath := "test.txt"
	testContent := "This is test content"
	viewer.LoadFile(testPath, testContent)

	require.True(t, viewer.Active)
	require.Equal(t, testPath, viewer.FilePath)
	require.Equal(t, testContent, viewer.Content)

	// Test dimensions
	viewer.SetWidth(60)
	require.Equal(t, 60, viewer.Width)

	viewer.SetHeight(15)
	require.Equal(t, 15, viewer.Height)

	// Test closing
	viewer.Close()
	require.False(t, viewer.Active)
	require.Empty(t, viewer.FilePath)
	require.Empty(t, viewer.Content)
}

// TestBaseModal tests the base modal component
func TestBaseModal(t *testing.T) {
	title := "Test Modal"
	content := "This is a test modal"
	modal := NewBaseModal(title, content, 30, 10)

	require.Equal(t, title, modal.Title)
	require.Equal(t, content, modal.Content)
	require.Equal(t, 30, modal.Width)
	require.Equal(t, 10, modal.Height)

	// Test rendering
	view := modal.Render()
	require.NotEmpty(t, view)
	require.Contains(t, view, title)
	require.Contains(t, view, content)
}

// TestToastManager tests the toast manager
func TestToastManager(t *testing.T) {
	toastManager := NewToastManager()

	// Initially should have no toasts
	require.Empty(t, toastManager.Toasts)

	// Test adding a toast
	message := "Test toast message"
	toastType := "info"
	timeout := 5 * time.Second

	toastManager.AddToast(message, toastType, timeout)
	require.Equal(t, 1, len(toastManager.Toasts))

	// Test view rendering
	view := toastManager.View()
	require.NotEmpty(t, view)
	require.Contains(t, view, message)

	// Test removing a toast
	toastID := toastManager.Toasts[0].ID
	toastManager.RemoveToast(toastID)
	require.Empty(t, toastManager.Toasts)

	// Test updating (removing expired toasts)
	toastManager.AddToast(message, toastType, 1*time.Millisecond)
	time.Sleep(2 * time.Millisecond) // Wait for toast to expire
	updatedManager := toastManager.Update()
	require.Empty(t, updatedManager.Toasts)
}

// TestRenderHomeView tests the home view rendering
func TestRenderHomeView(t *testing.T) {
	view := RenderHomeView(80, 24)
	require.NotEmpty(t, view)
	require.Contains(t, view, "Asimi CLI - Interactive Coding Agent")
	require.Contains(t, view, "Your AI-powered coding assistant")
}

// TestRenderChatView tests the chat view rendering
func TestRenderChatView(t *testing.T) {
	messages := []string{"Message 1", "Message 2", "Message 3"}
	view := RenderChatView(messages, 80, 24)
	require.NotEmpty(t, view)
	require.Contains(t, view, "Asimi CLI - Chat Session")

	// Check that messages are included
	for _, msg := range messages {
		require.Contains(t, view, msg)
	}
}
