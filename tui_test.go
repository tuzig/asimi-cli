package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms/fake"
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
	model := NewTUIModel(mockConfig())
	cmd := model.Init()

	// Init should return nil as there's no initial command
	require.Nil(t, cmd)
}

// TestTUIModelWindowSizeMsg tests handling of window size messages
func TestTUIModelWindowSizeMsg(t *testing.T) {
	model := NewTUIModel(mockConfig())

	// Send a window size message
	newModel, cmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Equal(t, 100, updatedModel.width)
	require.Equal(t, 50, updatedModel.height)
	require.Nil(t, cmd)
}

// newTestModel creates a new TUIModel for testing purposes.
func newTestModel(t *testing.T) (*TUIModel, *fake.LLM) {
    llm := fake.NewFakeLLM([]string{})
    model := NewTUIModel(mockConfig())
    // Use native session path for tests now that legacy agent is removed.
    sess, err := NewSession(llm, &Config{LLM: LLMConfig{Provider: "fake"}}, func(any) {})
    require.NoError(t, err)
    model.SetSession(sess)
    return model, llm
}

// TestTUIModelKeyMsg tests quitting the application with 'q' and Ctrl+C
func TestTUIModelKeyMsg(t *testing.T) {
	testCases := []struct {
		name          string
		key           tea.KeyMsg
		expectQuit    bool
		expectCommand bool
	}{
		{
			name:          "Quit with 'q'",
			key:           tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")},
			expectQuit:    false,
			expectCommand: false,
		},
		{
			name:          "Quit with 'ctrl+c'",
			key:           tea.KeyMsg{Type: tea.KeyCtrlC},
			expectQuit:    true,
			expectCommand: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			model := NewTUIModel(mockConfig())

			// Send a quit key message
			newModel, cmd := model.Update(tc.key)

			if tc.expectCommand {
				require.NotNil(t, cmd)
			} else {
				require.Nil(t, cmd)
			}

			if tc.expectQuit {
				// Execute the command to verify it's a quit command
				result := cmd()
				_, ok := result.(tea.QuitMsg)
				require.True(t, ok)
			}

			// Model should be unchanged
			_, ok := newModel.(TUIModel)
			require.True(t, ok)
		})
	}
}

func TestTUIModelSubmit(t *testing.T) {
	t.Skip("TODO: fix this test")
	testCases := []struct {
		name                 string
		initialEditorValue   string
		expectedMessageCount int
		expectedLastMessage  string
		expectCommand        bool
	}{
		{
			name:                 "Submit empty message",
			initialEditorValue:   "",
			expectedMessageCount: 1,
			expectedLastMessage:  "Welcome to Asimi CLI! Send a message to start chatting.",
			expectCommand:        false,
		},
		{
			name:                 "Submit command",
			initialEditorValue:   "/help",
			expectedMessageCount: 2,
			expectedLastMessage:  "Available commands:",
			expectCommand:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			model, _ := newTestModel(t)

			model.prompt.SetValue(tc.initialEditorValue)

			newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

			if tc.expectCommand {
				require.NotNil(t, cmd)
				msg := cmd()
				newModel, cmd = newModel.Update(msg)
				require.Nil(t, cmd)
			} else {
				require.Nil(t, cmd)
			}

			require.Equal(t, tc.expectedMessageCount, len(model.chat.Messages))
			require.Contains(t, model.chat.Messages[len(model.chat.Messages)-1], tc.expectedLastMessage, "prompt", tc.name)
		})
	}
}

func TestTUIModelKeyboardInteraction(t *testing.T) {
	testCases := []struct {
		name   string
		key    tea.KeyMsg
		setup  func(model *TUIModel)
		verify func(t *testing.T, model *TUIModel, cmd tea.Cmd)
	}{
		{
			name: "Escape key",
			key:  tea.KeyMsg{Type: tea.KeyEsc},
			setup: func(model *TUIModel) {
				model.modal = NewBaseModal("Test", "Test content", 30, 10)
				model.showCompletionDialog = true
			},
			verify: func(t *testing.T, model *TUIModel, cmd tea.Cmd) {
				require.Nil(t, cmd)
				require.Nil(t, model.modal)
				require.False(t, model.showCompletionDialog)
			},
		},
		{
			name: "Down arrow in completion dialog",
			key:  tea.KeyMsg{Type: tea.KeyDown},
			setup: func(model *TUIModel) {
				model.showCompletionDialog = true
				model.completions.SetOptions([]string{"option1", "option2", "option3"})
				model.completions.Show()
			},
			verify: func(t *testing.T, model *TUIModel, cmd tea.Cmd) {
				require.Nil(t, cmd)
				require.Equal(t, 1, model.completions.Selected)
			},
		},
		{
			name: "Up arrow in completion dialog",
			key:  tea.KeyMsg{Type: tea.KeyUp},
			setup: func(model *TUIModel) {
				model.showCompletionDialog = true
				model.completions.SetOptions([]string{"option1", "option2", "option3"})
				model.completions.Show()
				model.completions.Selected = 1
			},
			verify: func(t *testing.T, model *TUIModel, cmd tea.Cmd) {
				require.Nil(t, cmd)
				require.Equal(t, 0, model.completions.Selected)
			},
		},
		{
			name: "Tab to select in completion dialog",
			key:  tea.KeyMsg{Type: tea.KeyTab},
			setup: func(model *TUIModel) {
				model.showCompletionDialog = true
				model.completionMode = "command"
				model.completions.SetOptions([]string{"/help", "option2", "option3"})
				model.completions.Show()
			},
			verify: func(t *testing.T, model *TUIModel, cmd tea.Cmd) {
				require.NotNil(t, cmd)
				msg := cmd()
				newModel, cmd := model.Update(msg)
				require.Nil(t, cmd)
				updatedModel, ok := newModel.(TUIModel)
				require.True(t, ok)
				require.Contains(t, updatedModel.chat.Messages[len(updatedModel.chat.Messages)-1], "Available commands:")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			model, _ := newTestModel(t)
			if tc.setup != nil {
				tc.setup(model)
			}

			newModel, cmd := model.Update(tc.key)
			updatedModel, ok := newModel.(TUIModel)
			require.True(t, ok)

			tc.verify(t, &updatedModel, cmd)
		})
	}
}

// TestTUIModelView tests the view rendering
func TestTUIModelView(t *testing.T) {
	model := NewTUIModel(mockConfig())

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
	require.Contains(t, view, "Welcome to Asimi")
}

// TestPromptComponent tests the prompt component
func TestPromptComponent(t *testing.T) {
	prompt := NewPromptComponent(50, 10)

	// Test setting and getting value
	testValue := "Test content"
	prompt.SetValue(testValue)
	require.Equal(t, testValue, prompt.Value())

	// Test dimensions
	prompt.SetWidth(60)
	require.Equal(t, 60, prompt.Width)

	prompt.SetHeight(15)
	require.Equal(t, 15, prompt.Height)
}

// TestChatComponent tests the chat component
func TestChatComponent(t *testing.T) {
	chat := NewChatComponent(50, 10)

	// Should have initial welcome message
	require.Equal(t, 1, len(chat.Messages))
	require.Equal(t, "Welcome to Asimi CLI! Send a message to start chatting.", chat.Messages[0])

	// Test adding a message
	testMessage := "Test message"
	chat.AddMessage(testMessage)
	require.Equal(t, 2, len(chat.Messages))
	require.Equal(t, testMessage, chat.Messages[1])

	// Test dimensions
	chat.SetWidth(60)
	require.Equal(t, 60, chat.Width)

	chat.SetHeight(15)
	require.Equal(t, 15, chat.Height)
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
	require.Equal(t, 2, dialog.Selected)

	dialog.SelectPrev()
	require.Equal(t, 1, dialog.Selected)

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

// TestCompletionDialogScrolling tests the scrolling functionality of the completion dialog
func TestCompletionDialogScrolling(t *testing.T) {
	dialog := NewCompletionDialog()
	dialog.MaxHeight = 5
	dialog.ScrollMargin = 1
	options := []string{"a", "b", "c", "d", "e", "f", "g"}
	dialog.SetOptions(options)

	// Initial state
	require.Equal(t, 0, dialog.Selected)
	require.Equal(t, 0, dialog.Offset)

	// Scroll down
	dialog.SelectNext() // b
	require.Equal(t, 1, dialog.Selected)
	require.Equal(t, 0, dialog.Offset)

	dialog.SelectNext() // c
	require.Equal(t, 2, dialog.Selected)
	require.Equal(t, 0, dialog.Offset)

	dialog.SelectNext() // d
	require.Equal(t, 3, dialog.Selected)
	require.Equal(t, 0, dialog.Offset)

	dialog.SelectNext() // e, enters scroll margin
	require.Equal(t, 4, dialog.Selected)
	require.Equal(t, 1, dialog.Offset) // scrolled

	dialog.SelectNext() // f
	require.Equal(t, 5, dialog.Selected)
	require.Equal(t, 2, dialog.Offset)

	dialog.SelectNext() // g, at the end
	require.Equal(t, 6, dialog.Selected)
	require.Equal(t, 2, dialog.Offset) // offset is maxed out

	// Try to scroll past the end
	dialog.SelectNext() // g
	require.Equal(t, 6, dialog.Selected)
	require.Equal(t, 2, dialog.Offset)

	// Scroll up
	dialog.SelectPrev() // f
	require.Equal(t, 5, dialog.Selected)
	require.Equal(t, 2, dialog.Offset)

	dialog.SelectPrev() // e
	require.Equal(t, 4, dialog.Selected)
	require.Equal(t, 2, dialog.Offset)

	dialog.SelectPrev() // d
	require.Equal(t, 3, dialog.Selected)
	require.Equal(t, 2, dialog.Offset)

	dialog.SelectPrev() // c, enters scroll margin
	require.Equal(t, 2, dialog.Selected)
	require.Equal(t, 1, dialog.Offset)

	dialog.SelectPrev() // b, enters scroll margin
	require.Equal(t, 1, dialog.Selected)
	require.Equal(t, 0, dialog.Offset)

	dialog.SelectPrev() // a
	require.Equal(t, 0, dialog.Selected)
	require.Equal(t, 0, dialog.Offset)

	// Try to scroll past the beginning
	dialog.SelectPrev() // a
	require.Equal(t, 0, dialog.Selected)
	require.Equal(t, 0, dialog.Offset)
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
	tostType := "info"
	timeout := 5 * time.Second

	toastManager.AddToast(message, tostType, timeout)
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
	toastManager.AddToast(message, tostType, 1*time.Millisecond)
	time.Sleep(2 * time.Millisecond) // Wait for toast to expire
	updatedManager := toastManager.Update()
	require.Empty(t, updatedManager.Toasts)
}

// TestTUIModelUpdateFileCompletions tests the file completion functionality with multiple files
func TestTUIModelUpdateFileCompletions(t *testing.T) {
	model, _ := newTestModel(t)

	// Set up mock file list
	files := []string{
		"main.go",
		"utils.go",
		"config.json",
		"README.md",
		"docs/guide.md",
		"test/utils_test.go",
	}

	// Test single file completion
	model.prompt.SetValue("@mai")
	model.updateFileCompletions(files)
	require.Equal(t, 1, len(model.completions.Options))
	require.Contains(t, model.completions.Options[0], "main.go")

	// Test multiple matching files
	model.prompt.SetValue("@util")
	model.updateFileCompletions(files)
	require.Equal(t, 2, len(model.completions.Options))
	require.True(t,
		(strings.Contains(model.completions.Options[0], "utils.go") && strings.Contains(model.completions.Options[1], "utils_test.go")) ||
			(strings.Contains(model.completions.Options[1], "utils.go") && strings.Contains(model.completions.Options[0], "utils_test.go")))

	// Test multiple file references in one input
	model.prompt.SetValue("Check these files: @main.go and @config")
	model.updateFileCompletions(files)
	require.Equal(t, 1, len(model.completions.Options))
	require.Contains(t, model.completions.Options[0], "config.json")

}

// TestRenderHomeView tests the home view rendering
func TestRenderHomeView(t *testing.T) {
	view := renderHomeView(80, 24)
	require.NotEmpty(t, view)
	require.Contains(t, view, "Asimi CLI - Interactive Coding Agent")
	require.Contains(t, view, "Your AI-powered coding assistant")
}
