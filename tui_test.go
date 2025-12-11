package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	gogit "github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/fake"
)

// mockConfig returns a mock configuration for testing
func mockConfig() *Config {
	return &Config{
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

// containsMessage checks if any message in the slice contains the given substring
func containsMessage(messages []string, substring string) bool {
	for _, msg := range messages {
		if strings.Contains(msg, substring) {
			return true
		}
	}
	return false
}

// TestTUIModelInit tests the initialization of the TUI model
func TestTUIModelInit(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)
	cmd := model.Init()

	// Init should return nil as there's no initial command
	require.Nil(t, cmd)
}

// TestTUIModelWindowSizeMsg tests handling of window size messages
func TestTUIModelWindowSizeMsg(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)

	// Send a window size message
	newModel, cmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Equal(t, 100, updatedModel.width)
	require.Equal(t, 50, updatedModel.height)
	require.Nil(t, cmd)
}

// newTestModel creates a new TUIModel for testing purposes.
func newTestModel(t *testing.T) *TUIModel {
	llm := fake.NewFakeLLM([]string{})
	model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)
	// Disable persistent history to keep tests hermetic.
	model.persistentPromptHistory = nil
	model.initHistory()
	// Use native session path for tests now that legacy agent is removed.
	sess, err := NewSession(llm, &Config{LLM: LLMConfig{Provider: "fake"}}, RepoInfo{}, func(any) {})
	require.NoError(t, err)
	model.SetSession(sess)
	return model
}

func TestCommandCompletionOrderDefaultsToHelp(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)
	model.prompt.SetValue(":")
	model.completionMode = "command"
	model.updateCommandCompletions()
	require.NotEmpty(t, model.completions.Options)
	require.Equal(t, ":help", model.completions.Options[0])
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
			expectCommand: true, // Textarea returns a command for text input
		},
		{
			name:          "First 'ctrl+c' does not quit",
			key:           tea.KeyMsg{Type: tea.KeyCtrlC},
			expectQuit:    false,
			expectCommand: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)

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
			} else if cmd != nil {
				// If we got a command but don't expect quit, verify it's NOT a quit command
				result := cmd()
				_, ok := result.(tea.QuitMsg)
				require.False(t, ok, "Should not be a quit command")
			}

			// Model should be unchanged
			_, ok := newModel.(TUIModel)
			require.True(t, ok)
		})
	}
}

func TestDoubleCtrlCToQuit(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)

	// First CTRL-C should not quit
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.Nil(t, cmd)
	tuiModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.False(t, tuiModel.ctrlCPressedTime.IsZero())

	// Second CTRL-C should quit (wait slightly longer than debounce time)
	time.Sleep(ctrlCDebounceTime + 10*time.Millisecond)
	newModel, cmd = tuiModel.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd)
	result := cmd()
	_, ok = result.(tea.QuitMsg)
	require.True(t, ok)
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
			model := newTestModel(t)

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

			chat := model.content.Chat
			require.Equal(t, tc.expectedMessageCount, len(chat.Messages))
			require.Contains(t, chat.Messages[len(chat.Messages)-1], tc.expectedLastMessage, "prompt", tc.name)
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
				model.completions.SetOptions([]string{":help", "option2", "option3"})
				model.completions.Show()
			},
			verify: func(t *testing.T, model *TUIModel, cmd tea.Cmd) {
				require.Nil(t, cmd)
				require.Equal(t, ViewHelp, model.content.GetActiveView())
				require.Equal(t, "index", model.content.help.GetTopic())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			model := newTestModel(t)
			if tc.setup != nil {
				tc.setup(model)
			}

			newModel, cmd := model.Update(tc.key)
			updatedModel, ok := newModel.(TUIModel)
			require.True(t, ok)

			for cmd != nil {
				msg := cmd()
				newModel, cmd = updatedModel.Update(msg)
				updatedModel, ok = newModel.(TUIModel)
				require.True(t, ok)
			}

			tc.verify(t, &updatedModel, cmd)
		})
	}
}

// TestTUIModelView tests the view rendering
func TestTUIModelView(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)

	// Test view rendering with zero dimensions (should show initializing)
	view := model.View()
	require.NotEmpty(t, view)
	require.Contains(t, view, "Initializing...")

	// Set dimensions and test proper rendering
	model.width = 80
	model.height = 24
	view = model.View()
	require.NotEmpty(t, view)
	require.NotContains(t, view, "Initializing...")

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
	chat := NewChatComponent(50, 10, false)

	// Should have initial title message
	require.Equal(t, 1, len(chat.Messages))
	require.Contains(t, chat.Messages[0], "New session")

	// Test adding a message
	testMessage := "Test message"
	chat.AddMessage(testMessage)
	require.Equal(t, 2, len(chat.Messages))
	require.Equal(t, testMessage, chat.Messages[1])

	// Test dimensions
	chat.SetSize(60, 15)
	require.Equal(t, 60, chat.Width)
	require.Equal(t, 15, chat.Height)
}

func TestChatComponentScrollLock(t *testing.T) {
	chat := NewChatComponent(50, 10, false)

	chat.SetScrollLock(true)
	require.True(t, chat.IsScrollLocked())
	require.True(t, chat.UserScrolled)
	require.False(t, chat.AutoScroll)

	chat.AddMessage("Asimi: hello")
	require.True(t, chat.UserScrolled, "user should remain scrolled when locked")
	require.False(t, chat.AutoScroll, "auto-scroll should stay disabled when locked")

	chat.ScrollToBottom()
	require.False(t, chat.AutoScroll, "still locked, auto-scroll stays disabled even at bottom")

	chat.SetScrollLock(false)
	require.False(t, chat.IsScrollLocked())
	require.True(t, chat.AutoScroll, "auto-scroll should resume when unlock at bottom")
	require.False(t, chat.UserScrolled, "unlock at bottom should mark user as not scrolled")
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

	// Test setting properties with new API
	status.SetProvider("test", "model", true)

	// Set repo info to test branch rendering
	repoInfo := &RepoInfo{
		Branch: "main",
	}
	status.SetRepoInfo(repoInfo)

	// Test width
	status.SetWidth(60)
	require.Equal(t, 60, status.Width)

	// Test view rendering
	view := status.View()
	require.NotEmpty(t, view)
	// The new status format includes git branch and provider info
	require.Contains(t, view, "main")       // Should contain branch name
	require.Contains(t, view, "test-model") // Should contain provider-model
	require.Contains(t, view, "✅")          // Should contain connected icon
}

func TestSummarizeStatus(t *testing.T) {
	cases := []struct {
		name     string
		status   gogit.Status
		expected string
	}{
		{
			name:     "empty status",
			status:   gogit.Status{},
			expected: "",
		},
		{
			name: "mixed indicators",
			status: gogit.Status{
				"modified.go": &gogit.FileStatus{
					Staging:  gogit.Modified,
					Worktree: gogit.Unmodified,
				},
				"staged_added.go": &gogit.FileStatus{
					Staging:  gogit.Added,
					Worktree: gogit.Unmodified,
				},
				"deleted.txt": &gogit.FileStatus{
					Staging:  gogit.Deleted,
					Worktree: gogit.Unmodified,
				},
				"renamed.txt": &gogit.FileStatus{
					Staging:  gogit.Renamed,
					Worktree: gogit.Unmodified,
				},
				"untracked.md": &gogit.FileStatus{
					Staging:  gogit.Untracked,
					Worktree: gogit.Untracked,
				},
				"worktree_modified.go": &gogit.FileStatus{
					Staging:  gogit.Unmodified,
					Worktree: gogit.Modified,
				},
			},
			expected: "[!+-→?]",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, summarizeStatus(tc.status))
		})
	}
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

// TestCommandLine tests the command line component (including toast functionality)
func TestCommandLine(t *testing.T) {
	commandLine := NewCommandLineComponent()

	// Initially should have no toasts
	require.Empty(t, commandLine.toasts)

	// Test adding a toast
	message := "Test toast message"
	toastType := "info"
	timeout := 5 * time.Second

	commandLine.AddToast(message, toastType, timeout)
	require.Equal(t, 1, len(commandLine.toasts))

	// Test view rendering with toast
	view := commandLine.View()
	require.NotEmpty(t, view)
	require.Contains(t, view, message)

	// Test clearing toasts
	commandLine.ClearToasts()
	require.Empty(t, commandLine.toasts)

	// Re-add toast to verify removal still works
	commandLine.AddToast(message, toastType, timeout)
	require.Equal(t, 1, len(commandLine.toasts))

	// Test removing a toast
	toastID := commandLine.toasts[0].ID
	commandLine.RemoveToast(toastID)
	require.Empty(t, commandLine.toasts)

	// Test updating (removing expired toasts)
	commandLine.AddToast(message, toastType, 1*time.Millisecond)
	time.Sleep(2 * time.Millisecond) // Wait for toast to expire
	commandLine.Update()
	require.Empty(t, commandLine.toasts)
}

func TestCommandLineBackspaceAtLineStartExitsCommandMode(t *testing.T) {
	commandLine := NewCommandLineComponent()
	commandLine.EnterCommandMode("")
	require.True(t, commandLine.IsInCommandMode())

	cmd, handled := commandLine.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	require.True(t, handled)
	require.NotNil(t, cmd)
	require.False(t, commandLine.IsInCommandMode())
	require.Equal(t, "", commandLine.GetCommand())
	require.Equal(t, 0, commandLine.cursorPos)
}

// TestTUIModelUpdateFileCompletions tests the file completion functionality with multiple files
func TestTUIModelUpdateFileCompletions(t *testing.T) {
	model := newTestModel(t)

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
	model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)
	model.width = 80
	model.height = 24

	view := model.renderHomeView(80, 24)
	require.NotEmpty(t, view)
	require.Contains(t, view, "INSERT")
	require.Contains(t, view, "Asimi")
}

// TestRenderHomeViewWithUpdateAvailable tests the home view shows update notification
func TestRenderHomeViewWithUpdateAvailable(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)
	model.width = 80
	model.height = 24
	model.updateAvailable = true

	view := model.renderHomeView(80, 24)
	require.NotEmpty(t, view)
	require.Contains(t, view, "Update available")
	require.Contains(t, view, ":update")
}

// TestColonCommandCompletion tests command completion with colon prefix in vi mode
func TestColonCommandCompletion(t *testing.T) {
	model := newTestModel(t)

	// Test initial colon shows all commands with colon prefix
	model.prompt.SetValue(":")
	model.completionMode = "command"
	model.updateCommandCompletions()
	require.NotEmpty(t, model.completions.Options)
	// All options should start with ":"
	for _, opt := range model.completions.Options {
		require.True(t, strings.HasPrefix(opt, ":"), "Command should start with : but got: %s", opt)
	}

	// Test filtering with partial command
	model.prompt.SetValue(":he")
	model.updateCommandCompletions()
	require.NotEmpty(t, model.completions.Options)
	require.Contains(t, model.completions.Options, ":help")

	// Test filtering with more specific command
	model.prompt.SetValue(":new")
	model.updateCommandCompletions()
	require.NotEmpty(t, model.completions.Options)
	require.Contains(t, model.completions.Options, ":new")
}

// TestColonInNormalModeShowsCompletion tests that pressing : in normal mode shows completion dialog
func TestColonInNormalModeActivatesCommandLine(t *testing.T) {
	model := newTestModel(t)

	// Start in insert mode
	require.Equal(t, "insert", model.Mode)

	// Press Esc to enter normal mode
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updatedModel := newModel.(TUIModel)
	// Process the ChangeModeMsg returned by the command
	if cmd != nil {
		msg := cmd()
		newModel, _ = updatedModel.Update(msg)
		updatedModel = newModel.(TUIModel)
	}
	require.Equal(t, "normal", updatedModel.Mode)

	// Press : to enter command-line mode
	newModel, cmd = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	updatedModel = newModel.(TUIModel)
	// Process the ChangeModeMsg returned by the command
	if cmd != nil {
		msg := cmd()
		newModel, _ = updatedModel.Update(msg)
		updatedModel = newModel.(TUIModel)
	}

	require.True(t, updatedModel.commandLine.IsInCommandMode(), "command line should enter command mode")
	require.False(t, updatedModel.showCompletionDialog, "completion dialog should not be shown automatically")
	require.Equal(t, "", updatedModel.commandLine.GetCommand(), "command buffer should be empty")
	require.False(t, updatedModel.prompt.TextArea.Focused(), "prompt should lose focus while command line active")
}

func TestShowHelpMsgDisplaysRequestedTopic(t *testing.T) {
	model := NewTUIModel(mockConfig(), nil, nil, nil, nil, nil)
	require.Equal(t, ViewChat, model.content.GetActiveView())

	newModel, _ := model.handleCustomMessages(showHelpMsg{topic: "modes"})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Equal(t, ViewHelp, updatedModel.content.GetActiveView())
	require.Equal(t, "modes", updatedModel.content.help.GetTopic())
}

// Tests from tui_history_test.go

// TestHistoryNavigation_EmptyHistory tests navigation with no history
func TestHistoryNavigation_EmptyHistory(t *testing.T) {
	model := newTestModel(t)

	// Press up arrow with empty history
	handled := model.handleHistoryNavigation(-1)
	require.False(t, handled, "Should not handle navigation with empty history")

	// Press down arrow with empty history
	handled = model.handleHistoryNavigation(1)
	require.False(t, handled, "Should not handle navigation with empty history")
}

// TestHistoryNavigation_SingleEntry tests navigation with one history entry
func TestHistoryNavigation_SingleEntry(t *testing.T) {
	model := newTestModel(t)

	// Add one history entry
	model.sessionPromptHistory = []promptHistoryEntry{
		{Prompt: "first prompt", SessionSnapshot: 1, ChatSnapshot: 0},
	}
	model.historyCursor = 1 // At present

	// Navigate up (to first entry)
	handled := model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Equal(t, 0, model.historyCursor)
	require.Equal(t, "first prompt", model.prompt.Value())
	require.True(t, model.historySaved, "Should save present state")

	// Try to navigate up again (should stay at first entry)
	handled = model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Equal(t, 0, model.historyCursor)

	// Navigate down (back to present)
	handled = model.handleHistoryNavigation(1)
	require.True(t, handled)
	require.Equal(t, 1, model.historyCursor)
	require.False(t, model.historySaved, "Should clear saved state when returning to present")
}

// TestHistoryNavigation_MultipleEntries tests navigation through multiple entries
func TestHistoryNavigation_MultipleEntries(t *testing.T) {
	model := newTestModel(t)
	model.prompt.SetValue("current input")

	// Add multiple history entries
	model.sessionPromptHistory = []promptHistoryEntry{
		{Prompt: "first prompt", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second prompt", SessionSnapshot: 3, ChatSnapshot: 2},
		{Prompt: "third prompt", SessionSnapshot: 5, ChatSnapshot: 4},
	}
	model.historyCursor = 3 // At present

	// Navigate up once
	handled := model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Equal(t, 2, model.historyCursor)
	require.Equal(t, "third prompt", model.prompt.Value())
	require.True(t, model.historySaved)
	require.Equal(t, "current input", model.historyPendingPrompt)

	// Navigate up again
	handled = model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Equal(t, 1, model.historyCursor)
	require.Equal(t, "second prompt", model.prompt.Value())

	// Navigate up to first
	handled = model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Equal(t, 0, model.historyCursor)
	require.Equal(t, "first prompt", model.prompt.Value())

	// Try to navigate up past first (should stay at first)
	handled = model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Equal(t, 0, model.historyCursor)
	require.Equal(t, "first prompt", model.prompt.Value())

	// Navigate down
	handled = model.handleHistoryNavigation(1)
	require.True(t, handled)
	require.Equal(t, 1, model.historyCursor)
	require.Equal(t, "second prompt", model.prompt.Value())

	// Navigate down to third
	handled = model.handleHistoryNavigation(1)
	require.True(t, handled)
	require.Equal(t, 2, model.historyCursor)
	require.Equal(t, "third prompt", model.prompt.Value())

	// Navigate down to present
	handled = model.handleHistoryNavigation(1)
	require.True(t, handled)
	require.Equal(t, 3, model.historyCursor)
	require.Equal(t, "current input", model.prompt.Value())
	require.False(t, model.historySaved)
}

// TestHistoryNavigation_DownWithoutSavedState tests down navigation without saved state
func TestHistoryNavigation_DownWithoutSavedState(t *testing.T) {
	model := newTestModel(t)

	model.sessionPromptHistory = []promptHistoryEntry{
		{Prompt: "first prompt", SessionSnapshot: 1, ChatSnapshot: 0},
	}
	model.historyCursor = 1 // At present
	model.historySaved = false

	// Try to navigate down when already at present
	handled := model.handleHistoryNavigation(1)
	require.False(t, handled, "Should not handle down when not in history")
}

// TestHistoryNavigation_CursorInitialization tests cursor initialization from present
func TestHistoryNavigation_CursorInitialization(t *testing.T) {
	model := newTestModel(t)
	model.prompt.SetValue("current")

	model.sessionPromptHistory = []promptHistoryEntry{
		{Prompt: "first", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "second", SessionSnapshot: 3, ChatSnapshot: 2},
	}
	model.historyCursor = len(model.sessionPromptHistory) // At present

	// First up navigation should go to last entry
	handled := model.handleHistoryNavigation(-1)
	require.True(t, handled)
	require.Equal(t, 1, model.historyCursor)
	require.Equal(t, "second", model.prompt.Value())
}

// TestWaitingIndicator_StartStop tests the waiting indicator lifecycle
func TestWaitingIndicator_StartStop(t *testing.T) {
	model := newTestModel(t)

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
	model := newTestModel(t)

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
	model := newTestModel(t)

	// Stop when not waiting (should not panic)
	model.stopStreaming()
	require.False(t, model.waitingForResponse)
}

// TestWaitingTickMsg_WhileWaiting tests waiting tick message handling
func TestWaitingTickMsg_WhileWaiting(t *testing.T) {
	model := newTestModel(t)

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
	model := newTestModel(t)

	// Handle tick message when not waiting
	newModel, cmd := model.handleCustomMessages(waitingTickMsg{})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)
	require.Nil(t, cmd, "Should not return tick command when not waiting")
	require.False(t, updatedModel.waitingForResponse)
}

// TestHistoryRollback_OnSubmit tests that submitting a historical prompt rolls back state
func TestHistoryRollback_OnSubmit(t *testing.T) {
	model := newTestModel(t)
	chat := model.content.Chat

	// Clear the welcome message for cleaner testing
	chat.Messages = []string{}
	chat.UpdateContent()

	// Simulate a conversation
	chat.AddMessage("You: first")
	chat.AddMessage("Asimi: response1")
	model.sessionPromptHistory = append(model.sessionPromptHistory, promptHistoryEntry{
		Prompt:          "first",
		SessionSnapshot: 1,
		ChatSnapshot:    0, // Before adding messages
	})

	chat.AddMessage("You: second")
	chat.AddMessage("Asimi: response2")
	model.sessionPromptHistory = append(model.sessionPromptHistory, promptHistoryEntry{
		Prompt:          "second",
		SessionSnapshot: 1, // Session hasn't changed (no actual LLM calls)
		ChatSnapshot:    2, // After first conversation
	})

	model.historyCursor = len(model.sessionPromptHistory)

	// Navigate to first prompt
	model.handleHistoryNavigation(-1) // to "second"
	model.handleHistoryNavigation(-1) // to "first"

	require.Equal(t, 0, model.historyCursor)
	require.Equal(t, "first", model.prompt.Value())
	require.True(t, model.historySaved)

	// Simulate submitting the historical prompt
	chatLenBefore := len(chat.Messages)
	sessionLenBefore := len(model.session.Messages)

	// We'll test the rollback logic directly
	if model.historySaved && model.historyCursor < len(model.sessionPromptHistory) {
		entry := model.sessionPromptHistory[model.historyCursor]
		model.session.RollbackTo(entry.SessionSnapshot)
		chat.TruncateTo(entry.ChatSnapshot)
	}

	// Verify rollback occurred
	require.Equal(t, 1, len(model.session.Messages), "Session should be rolled back to system message")
	require.Equal(t, 0, len(chat.Messages), "Chat should be rolled back to empty")
	require.Less(t, len(chat.Messages), chatLenBefore)
	require.Equal(t, len(model.session.Messages), sessionLenBefore) // Session didn't change in this test
}

// TestNewSessionCommand_ResetsHistory tests that /new command resets history
func TestNewSessionCommand_ResetsHistory(t *testing.T) {
	model := newTestModel(t)

	// Add some history
	model.sessionPromptHistory = []promptHistoryEntry{
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
	cmd := handleNewSessionCommand(model, []string{})

	// Process the returned message
	msg := cmd()
	startMsg, ok := msg.(startConversationMsg)
	require.True(t, ok, "Expected startConversationMsg")
	require.True(t, startMsg.clearHistory)

	// Simulate the message being processed by Update
	updatedModel, _ := model.Update(startMsg)
	updatedModelValue, ok := updatedModel.(TUIModel)
	require.True(t, ok, "Expected TUIModel")

	// Verify history was reset
	require.Empty(t, updatedModelValue.sessionPromptHistory)
	require.Equal(t, 0, updatedModelValue.historyCursor)
	require.False(t, updatedModelValue.historySaved)
	require.Empty(t, updatedModelValue.historyPendingPrompt)
	require.False(t, updatedModelValue.waitingForResponse)
}

// TestStartConversationMsg_InitialMessages tests that initialMessages are displayed after clearing history
func TestStartConversationMsg_InitialMessages(t *testing.T) {
	model := newTestModel(t)

	// Add some messages to the chat
	model.content.Chat.AddMessage("existing message 1")
	model.content.Chat.AddMessage("existing message 2")
	require.Len(t, model.content.Chat.Messages, 3) // Welcome + 2 messages

	// Create a startConversationMsg with initialMessages
	msg := startConversationMsg{
		clearHistory: true,
		initialMessages: []string{
			"Initial message 1",
			"Initial message 2",
		},
	}

	// Process the message
	updatedModel, _ := model.Update(msg)
	updatedModelValue, ok := updatedModel.(TUIModel)
	require.True(t, ok, "Expected TUIModel")

	// Verify that the chat was cleared and initialMessages were added
	// The chat should have: Welcome message + 2 initial messages
	require.Len(t, updatedModelValue.content.Chat.Messages, 3)
	require.Contains(t, updatedModelValue.content.Chat.Messages[1], "Initial message 1")
	require.Contains(t, updatedModelValue.content.Chat.Messages[2], "Initial message 2")
}

// TestHistoryNavigation_WithArrowKeys tests arrow key handling
func TestHistoryNavigation_WithArrowKeys(t *testing.T) {
	model := newTestModel(t)

	// Add history
	model.sessionPromptHistory = []promptHistoryEntry{
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
	model := newTestModel(t)

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
	model := newTestModel(t)

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
	model := newTestModel(t)
	model.prompt.SetValue("current prompt")
	chat := model.content.Chat
	chat.AddMessage("message 1")
	chat.AddMessage("message 2")

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
	model := newTestModel(t)
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
	model := newTestModel(t)
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
	repoInfo := RepoInfo{}
	sess, err := NewSession(llm, &Config{}, repoInfo, func(any) {})
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
	model := newTestModel(t)

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
	model := newTestModel(t)

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
	model := newTestModel(t)

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
	model := newTestModel(t)

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

// TestSessionResume_ResetsHistoryState tests that resuming a session resets history state
// This prevents the bug where entering a prompt after resume would clear the chat
func TestSessionResume_ResetsHistoryState(t *testing.T) {
	model := newTestModel(t)

	// Simulate having some history state from a previous session
	model.sessionPromptHistory = []promptHistoryEntry{
		{Prompt: "old prompt 1", SessionSnapshot: 1, ChatSnapshot: 0},
		{Prompt: "old prompt 2", SessionSnapshot: 3, ChatSnapshot: 2},
	}
	model.historyCursor = 1
	model.historySaved = true
	model.historyPendingPrompt = "pending from old session"
	model.historyPresentSessionSnapshot = 5
	model.historyPresentChatSnapshot = 4

	// Create a mock resumed session
	resumedSession := &Session{
		ID:          "resumed-session-id",
		FirstPrompt: "resumed prompt",
		Messages: []llms.MessageContent{
			{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: "system"}}},
			{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "hello"}}},
			{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextContent{Text: "hi there"}}},
		},
	}

	// Process the sessionSelectedMsg
	newModel, _ := model.handleCustomMessages(sessionSelectedMsg{session: resumedSession})
	updatedModel, ok := newModel.(TUIModel)
	require.True(t, ok)

	// Verify history state was reset
	require.Empty(t, updatedModel.sessionPromptHistory, "sessionPromptHistory should be empty after resume")
	require.Equal(t, 0, updatedModel.historyCursor, "historyCursor should be 0 after resume")
	require.False(t, updatedModel.historySaved, "historySaved should be false after resume")
	require.Empty(t, updatedModel.historyPendingPrompt, "historyPendingPrompt should be empty after resume")
	require.Equal(t, 0, updatedModel.historyPresentSessionSnapshot, "historyPresentSessionSnapshot should be 0 after resume")
	require.Equal(t, 0, updatedModel.historyPresentChatSnapshot, "historyPresentChatSnapshot should be 0 after resume")

	// Verify session was properly set
	require.True(t, updatedModel.sessionActive)
	require.Equal(t, "resumed-session-id", updatedModel.session.ID)

	// Verify chat was rebuilt with resumed messages
	chat := updatedModel.content.Chat
	require.True(t, containsMessage(chat.Messages, "You: hello"), "Chat should contain resumed human message")
	require.True(t, containsMessage(chat.Messages, "Asimi: hi there"), "Chat should contain resumed AI message")
}

// TestHistoryNavigation_RapidNavigation tests rapid navigation through history
func TestHistoryNavigation_RapidNavigation(t *testing.T) {
	model := newTestModel(t)

	// Add many history entries
	for i := 0; i < 10; i++ {
		model.sessionPromptHistory = append(model.sessionPromptHistory, promptHistoryEntry{
			Prompt:          "prompt " + string(rune('0'+i)),
			SessionSnapshot: i*2 + 1,
			ChatSnapshot:    i * 2,
		})
	}
	model.historyCursor = len(model.sessionPromptHistory)
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

// Tests from tui_e2e_test.go

func TestFileCompletion(t *testing.T) {
	// Create a new TUI model for testing
	config := mockConfig()
	model := NewTUIModel(config, nil, nil, nil, nil, nil)

	// Set up a mock session for the test
	llm := fake.NewFakeLLM([]string{})
	sess, err := NewSession(llm, &Config{LLM: LLMConfig{Provider: "fake"}}, RepoInfo{}, func(any) {})
	require.NoError(t, err)
	model.SetSession(sess)

	// Create a new test model
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(200, 200))

	// Get file list and find the inex of main.go
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
	tm.Type("@main.")

	// Wait for the completion dialog to appear
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return strings.Contains(string(bts), "main.go")
	}, teatest.WithCheckInterval(time.Millisecond*100), teatest.WithDuration(time.Second*3))

	// Simulate pressing enter to select the file
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Wait for a bit to let the file be read
	time.Sleep(100 * time.Millisecond)

	// Quit the application (requires double CTRL-C)
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	time.Sleep(ctrlCDebounceTime + 10*time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Get the final model
	finalModel := tm.FinalModel(t)
	tuiModel, ok := finalModel.(TUIModel)
	require.True(t, ok)

	// Assert that the prompt was completed to @main.go
	require.Equal(t, "@main.go ", tuiModel.prompt.TextArea.Value())

	// Assert that the file viewer contains the file content
	contextFiles := tuiModel.session.GetContextFiles()
	require.Contains(t, contextFiles["main.go"], "package main")

	// Assert that the prompt was not sent and the editor is still focused
	chat := tuiModel.content.Chat
	require.NotEmpty(t, chat.Messages)
	require.True(t, containsMessage(chat.Messages, "Loaded file: main.go"),
		"messages", chat.Messages)
	require.True(t, tuiModel.prompt.TextArea.Focused(), "The editor should remain focused")
}

func TestColonCommandCompletionE2E(t *testing.T) {
	// Create a new TUI model for testing
	config := mockConfig()
	model := NewTUIModel(config, nil, nil, nil, nil, nil)

	// Set up a mock session for the test
	llm := fake.NewFakeLLM([]string{})
	sess, err := NewSession(llm, &Config{LLM: LLMConfig{Provider: "fake"}}, RepoInfo{}, func(any) {})
	require.NoError(t, err)
	model.SetSession(sess)

	// Create a new test model
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(200, 200))

	// Simulate typing ":" to enter command mode
	tm.Type(":")

	// Wait for the command line to show ":"
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return strings.Contains(string(bts), ":")
	}, teatest.WithCheckInterval(time.Millisecond*100), teatest.WithDuration(time.Second*3))

	// Type "help" command
	tm.Type("help")

	// Press enter to execute the command
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Wait for a bit to let the command be executed
	time.Sleep(100 * time.Millisecond)

	// Quit the application (requires double CTRL-C)
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	time.Sleep(ctrlCDebounceTime + 10*time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Get the final model
	finalModel := tm.FinalModel(t)
	tuiModel, ok := finalModel.(TUIModel)
	require.True(t, ok)

	require.Equal(t, ViewHelp, tuiModel.content.GetActiveView())
	require.Equal(t, "index", tuiModel.content.help.GetTopic())
}

func TestLiveAgentE2E(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set, skipping live agent test")
	}

	t.Skip("E2E test is skipped for now")
	cmd := exec.Command("go", "run", ".", "-p", "who are you?")
	output, err := cmd.CombinedOutput()
	// In case of error, report the output
	require.NoError(t, err, "output", string(output))

	// Assert the output
	require.Contains(t, string(output), "I am ")
	require.NotContains(t, string(output), "Error")
}

// Tests from commandline_test.go

func TestYesNoMode(t *testing.T) {
	cl := NewCommandLineComponent()

	// Test entering yes/no mode
	cmd := cl.EnterYesNoMode("Are you sure?")
	assert.True(t, cl.IsInYesNoMode(), "Expected to be in yes/no mode")
	assert.Equal(t, "Are you sure?", cl.yesNoQuestion, "Question mismatch")

	// Verify mode change message
	require.NotNil(t, cmd, "Expected mode change command")
	msg := cmd()
	modeMsg, ok := msg.(ChangeModeMsg)
	require.True(t, ok, "Expected ChangeModeMsg")
	assert.Equal(t, "yesno", modeMsg.NewMode, "Mode mismatch")

	// Test 'y' key - should set answer but not exit mode
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	_, handled := cl.HandleKey(keyMsg)
	assert.True(t, handled, "Expected 'y' key to be handled")
	assert.True(t, cl.IsInYesNoMode(), "Expected to remain in yes/no mode after 'y' (need Enter)")
	assert.Equal(t, "y", cl.input, "Expected input to be 'y'")

	// Test Enter key to confirm 'y'
	keyMsg = tea.KeyMsg{Type: tea.KeyEnter}
	cmd, handled = cl.HandleKey(keyMsg)
	assert.True(t, handled, "Expected 'enter' key to be handled")
	assert.False(t, cl.IsInYesNoMode(), "Expected to exit yes/no mode after Enter")
	require.NotNil(t, cmd, "Expected batch command")

	// Test entering again and pressing 'n' then Enter
	cl.EnterYesNoMode("Delete everything?")
	keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}
	_, handled = cl.HandleKey(keyMsg)
	assert.True(t, handled, "Expected 'n' key to be handled")
	assert.True(t, cl.IsInYesNoMode(), "Expected to remain in yes/no mode after 'n' (need Enter)")
	assert.Equal(t, "n", cl.input, "Expected input to be 'n'")

	keyMsg = tea.KeyMsg{Type: tea.KeyEnter}
	cmd, handled = cl.HandleKey(keyMsg)
	assert.True(t, handled, "Expected 'enter' key to be handled")
	assert.False(t, cl.IsInYesNoMode(), "Expected to exit yes/no mode after Enter")

	// Test entering again and pressing 'esc' (should exit immediately)
	cl.EnterYesNoMode("Continue?")
	keyMsg = tea.KeyMsg{Type: tea.KeyEsc}
	cmd, handled = cl.HandleKey(keyMsg)
	assert.True(t, handled, "Expected 'esc' key to be handled")
	assert.False(t, cl.IsInYesNoMode(), "Expected to exit yes/no mode after 'esc'")

	// Test that other keys are ignored in yes/no mode
	cl.EnterYesNoMode("Test?")
	keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	cmd, handled = cl.HandleKey(keyMsg)
	assert.True(t, handled, "Expected key to be handled (ignored) in yes/no mode")
	assert.True(t, cl.IsInYesNoMode(), "Expected to remain in yes/no mode after invalid key")

	// Test backspace clears the answer
	cl.input = "y"
	keyMsg = tea.KeyMsg{Type: tea.KeyBackspace}
	_, handled = cl.HandleKey(keyMsg)
	assert.True(t, handled, "Expected backspace to be handled")
	assert.Equal(t, "", cl.input, "Expected input to be cleared after backspace")

	// Test Enter without answer does nothing
	keyMsg = tea.KeyMsg{Type: tea.KeyEnter}
	_, handled = cl.HandleKey(keyMsg)
	assert.True(t, handled, "Expected enter to be handled")
	assert.True(t, cl.IsInYesNoMode(), "Expected to remain in yes/no mode when Enter pressed without answer")
}

func TestYesNoModeView(t *testing.T) {
	// Initialize the global theme for tests
	if globalTheme == nil {
		NewTheme()
	}

	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	// Test view in yes/no mode
	cl.EnterYesNoMode("Delete all files?")
	view := cl.View()
	require.Contains(t, view, "Delete all files?", "Expected view to contain question")
	require.Contains(t, view, "(y/n)", "Expected view to contain '(y/n)'")
}

func TestYesNoModePriority(t *testing.T) {
	// Initialize the global theme for tests
	if globalTheme == nil {
		NewTheme()
	}

	cl := NewCommandLineComponent()
	cl.SetWidth(80)

	// Add a toast
	cl.AddToast("Test toast", "info", 5000)

	// Enter yes/no mode
	cl.EnterYesNoMode("Confirm?")

	// Yes/no should have priority over toast
	view := cl.View()
	require.Contains(t, view, "Confirm?", "Expected yes/no prompt to have priority over toast")
	require.NotContains(t, view, "Test toast", "Expected toast to be hidden when in yes/no mode")
}

// Tests from main_branch_test.go

func TestIsMainBranch(t *testing.T) {
	tests := []struct {
		name     string
		branch   string
		expected bool
	}{
		{
			name:     "main branch",
			branch:   "main",
			expected: true,
		},
		{
			name:     "master branch",
			branch:   "master",
			expected: true,
		},
		{
			name:     "feature branch",
			branch:   "feature/test",
			expected: false,
		},
		{
			name:     "develop branch",
			branch:   "develop",
			expected: false,
		},
		{
			name:     "empty branch",
			branch:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMainBranch(tt.branch)
			require.Equal(t, tt.expected, result, "isMainBranch(%q)", tt.branch)
		})
	}
}

func TestGetRepoInfo(t *testing.T) {
	// Test that GetRepoInfo can be called
	repoInfo := GetRepoInfo()
	// Just verify the function can be called without panicking
	// The actual content depends on the test environment
	t.Logf("GetRepoInfo returned: %+v", repoInfo)
}

// Tests from select_window_test.go

func TestSelectWindowNavigationHelpers(t *testing.T) {
	// Create a window with mixed selectable/non-selectable items
	sw := NewSelectWindow[string]()
	sw.SetItems([]string{"a", "error1", "b", "c", "error2", "d"})

	// isSelectable returns false for items starting with "error"
	isSelectable := func(s string) bool {
		return len(s) < 5 || s[:5] != "error"
	}

	t.Run("NextSelectableIndex", func(t *testing.T) {
		// From "a" (0), next selectable is "b" (2), skipping "error1" (1)
		require.Equal(t, 2, sw.NextSelectableIndex(0, isSelectable))

		// From "b" (2), next selectable is "c" (3)
		require.Equal(t, 3, sw.NextSelectableIndex(2, isSelectable))

		// From "c" (3), next selectable is "d" (5), skipping "error2" (4)
		require.Equal(t, 5, sw.NextSelectableIndex(3, isSelectable))

		// From "d" (5), no next selectable, stay at 5
		require.Equal(t, 5, sw.NextSelectableIndex(5, isSelectable))

		// With nil isSelectable, all items are selectable
		require.Equal(t, 1, sw.NextSelectableIndex(0, nil))
	})

	t.Run("PrevSelectableIndex", func(t *testing.T) {
		// From "d" (5), prev selectable is "c" (3), skipping "error2" (4)
		require.Equal(t, 3, sw.PrevSelectableIndex(5, isSelectable))

		// From "c" (3), prev selectable is "b" (2)
		require.Equal(t, 2, sw.PrevSelectableIndex(3, isSelectable))

		// From "b" (2), prev selectable is "a" (0), skipping "error1" (1)
		require.Equal(t, 0, sw.PrevSelectableIndex(2, isSelectable))

		// From "a" (0), no prev selectable, stay at 0
		require.Equal(t, 0, sw.PrevSelectableIndex(0, isSelectable))

		// With nil isSelectable, all items are selectable
		require.Equal(t, 4, sw.PrevSelectableIndex(5, nil))
	})

	t.Run("FirstSelectableIndex", func(t *testing.T) {
		// First selectable is "a" (0)
		require.Equal(t, 0, sw.FirstSelectableIndex(isSelectable))

		// With nil isSelectable, first is 0
		require.Equal(t, 0, sw.FirstSelectableIndex(nil))

		// Test with items where first is not selectable
		sw2 := NewSelectWindow[string]()
		sw2.SetItems([]string{"error1", "error2", "a", "b"})
		require.Equal(t, 2, sw2.FirstSelectableIndex(isSelectable))
	})

	t.Run("LastSelectableIndex", func(t *testing.T) {
		// Last selectable is "d" (5)
		require.Equal(t, 5, sw.LastSelectableIndex(isSelectable))

		// With nil isSelectable, last is len-1
		require.Equal(t, 5, sw.LastSelectableIndex(nil))

		// Test with items where last is not selectable
		sw2 := NewSelectWindow[string]()
		sw2.SetItems([]string{"a", "b", "error1", "error2"})
		require.Equal(t, 1, sw2.LastSelectableIndex(isSelectable))
	})

	t.Run("CountSelectableItems", func(t *testing.T) {
		// 4 selectable items: a, b, c, d
		require.Equal(t, 4, sw.CountSelectableItems(isSelectable))

		// With nil isSelectable, all items are counted
		require.Equal(t, 6, sw.CountSelectableItems(nil))
	})

	t.Run("EmptyWindow", func(t *testing.T) {
		empty := NewSelectWindow[string]()
		empty.SetItems([]string{})

		require.Equal(t, 0, empty.FirstSelectableIndex(isSelectable))
		require.Equal(t, 0, empty.LastSelectableIndex(isSelectable))
		require.Equal(t, 0, empty.CountSelectableItems(isSelectable))
	})
}

func TestIsModelSelectable(t *testing.T) {
	tests := []struct {
		status   string
		expected bool
	}{
		{"active", true},
		{"ready", true},
		{"login_required", true},
		{"error", false},
		{"unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			model := Model{Status: tt.status}
			require.Equal(t, tt.expected, IsModelSelectable(model))
		})
	}
}
