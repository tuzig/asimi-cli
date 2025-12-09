package main

import (
	"time"

	"github.com/afittestide/asimi/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Toast represents a single toast notification
type Toast struct {
	ID      string
	Message string
	Type    string // info, success, warning, error
	Created time.Time
	Timeout time.Duration
}

// CommandLine messages for TUI coordination
type (
	commandReadyMsg       struct{ command string }
	commandCancelledMsg   struct{}
	commandTextChangedMsg struct{}                // Signals completion update needed
	navigateCompletionMsg struct{ direction int } // -1 for up, +1 for down
	acceptCompletionMsg   struct{}                // Tab pressed
	navigateHistoryMsg    struct{ direction int } // For completion or history
	yesNoResponseMsg      struct{ answer bool }   // true for yes, false for no
)

// Mode management - single unified message for all mode changes
type ChangeModeMsg struct {
	NewMode string // "insert", "normal", "visual", "command", "help", "models", "resume", "scroll"
}

// CommandLineMode represents the state of the command line
type CommandLineMode int

const (
	CommandLineIdle CommandLineMode = iota
	CommandLineCommand
	CommandLineToast
	CommandLineYesNo
)

// CommandLineComponent manages the bottom command line
// Handles both : commands and toast notifications
type CommandLineComponent struct {
	mode       CommandLineMode
	toasts     []Toast
	command    string
	cursorPos  int // Cursor position in command string
	width      int
	style      lipgloss.Style
	showCursor bool

	// History support
	history        []string // Command history
	historyCursor  int      // Current position in history
	historySaved   bool     // Whether we've saved the current command
	historyPending string   // The command being typed before navigating history

	// Yes/No prompt support
	yesNoQuestion string // The question being asked
}

// NewCommandLineComponent creates a new command line component
func NewCommandLineComponent() *CommandLineComponent {
	return &CommandLineComponent{
		mode:           CommandLineIdle,
		toasts:         make([]Toast, 0),
		cursorPos:      0,
		showCursor:     true,
		history:        make([]string, 0),
		historyCursor:  0,
		historySaved:   false,
		historyPending: "",
		style: lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1).
			MaxWidth(50),
	}
}

// AddToast adds a new toast notification
func (cl *CommandLineComponent) AddToast(message, toastType string, timeout time.Duration) {
	toast := Toast{
		ID:      time.Now().String(),
		Message: message,
		Type:    toastType,
		Created: time.Now(),
		Timeout: timeout,
	}
	cl.toasts = append(cl.toasts, toast)
}

// RemoveToast removes a toast by ID
func (cl *CommandLineComponent) RemoveToast(id string) {
	for i, toast := range cl.toasts {
		if toast.ID == id {
			cl.toasts = append(cl.toasts[:i], cl.toasts[i+1:]...)
			break
		}
	}
}

// ClearToasts removes all existing toast notifications
func (cl *CommandLineComponent) ClearToasts() {
	cl.toasts = nil
}

// EnterCommandMode enters command mode with optional initial text
func (cl *CommandLineComponent) EnterCommandMode(initialText string) tea.Cmd {
	cl.mode = CommandLineCommand
	cl.command = initialText
	cl.cursorPos = len(initialText)
	cl.showCursor = true
	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "command"}
	}
}

// ExitCommandMode exits command mode and returns to idle
func (cl *CommandLineComponent) ExitCommandMode() tea.Cmd {
	cl.mode = CommandLineIdle
	cl.command = ""
	cl.cursorPos = 0
	cl.historySaved = false
	cl.historyPending = ""
	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "insert"}
	}
}

// IsInCommandMode returns true if in command mode
func (cl *CommandLineComponent) IsInCommandMode() bool {
	return cl.mode == CommandLineCommand
}

// Blur removes focus from the command line
func (cl *CommandLineComponent) Blur() {
	cl.showCursor = false
}

// EnterYesNoMode enters yes/no prompt mode with a question
func (cl *CommandLineComponent) EnterYesNoMode(question string) tea.Cmd {
	cl.mode = CommandLineYesNo
	cl.yesNoQuestion = question
	cl.showCursor = true
	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "yesno"}
	}
}

// ExitYesNoMode exits yes/no mode and returns to idle
func (cl *CommandLineComponent) ExitYesNoMode() tea.Cmd {
	cl.mode = CommandLineIdle
	cl.yesNoQuestion = ""
	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "insert"}
	}
}

// IsInYesNoMode returns true if in yes/no mode
func (cl *CommandLineComponent) IsInYesNoMode() bool {
	return cl.mode == CommandLineYesNo
}

// SetCommand sets the current command being entered
func (cl *CommandLineComponent) SetCommand(cmd string) {
	cl.command = cmd
	cl.cursorPos = len(cmd)
	if cmd != "" {
		cl.mode = CommandLineCommand
	} else {
		cl.mode = CommandLineIdle
	}
}

// InsertRune inserts a character at cursor position
func (cl *CommandLineComponent) InsertRune(r rune) {
	if cl.mode != CommandLineCommand {
		return
	}
	before := cl.command[:cl.cursorPos]
	after := cl.command[cl.cursorPos:]
	cl.command = before + string(r) + after
	cl.cursorPos++
}

// DeleteCharBackward deletes character before cursor (backspace)
func (cl *CommandLineComponent) DeleteCharBackward() {
	if cl.mode != CommandLineCommand || cl.cursorPos == 0 {
		return
	}
	before := cl.command[:cl.cursorPos-1]
	after := cl.command[cl.cursorPos:]
	cl.command = before + after
	cl.cursorPos--
}

// DeleteCharForward deletes character at cursor (delete key)
func (cl *CommandLineComponent) DeleteCharForward() {
	if cl.mode != CommandLineCommand || cl.cursorPos >= len(cl.command) {
		return
	}
	before := cl.command[:cl.cursorPos]
	after := cl.command[cl.cursorPos+1:]
	cl.command = before + after
}

// MoveCursorLeft moves cursor one position left
func (cl *CommandLineComponent) MoveCursorLeft() {
	if cl.cursorPos > 0 {
		cl.cursorPos--
	}
}

// MoveCursorRight moves cursor one position right
func (cl *CommandLineComponent) MoveCursorRight() {
	if cl.cursorPos < len(cl.command) {
		cl.cursorPos++
	}
}

// MoveCursorHome moves cursor to start
func (cl *CommandLineComponent) MoveCursorHome() {
	cl.cursorPos = 0
}

// MoveCursorEnd moves cursor to end
func (cl *CommandLineComponent) MoveCursorEnd() {
	cl.cursorPos = len(cl.command)
}

// GetCommand returns the current command
func (cl *CommandLineComponent) GetCommand() string {
	return cl.command
}

// ClearCommand clears the current command
func (cl *CommandLineComponent) ClearCommand() {
	cl.command = ""
	cl.mode = CommandLineIdle
}

// SetWidth sets the width for rendering
func (cl *CommandLineComponent) SetWidth(width int) {
	cl.width = width
}

// Update handles updating the command line (e.g., removing expired toasts)
func (cl *CommandLineComponent) Update() {
	now := time.Now()

	// Remove expired toasts
	activeToasts := make([]Toast, 0)
	for _, toast := range cl.toasts {
		if now.Sub(toast.Created) < toast.Timeout {
			activeToasts = append(activeToasts, toast)
		}
	}
	cl.toasts = activeToasts
}

// View renders the command line
func (cl *CommandLineComponent) View() string {
	// Priority 1: Show yes/no prompt if in yes/no mode
	if cl.mode == CommandLineYesNo {
		promptText := cl.yesNoQuestion + " (y/n) "
		var displayText string
		if cl.showCursor {
			cursorStyle := lipgloss.NewStyle().Reverse(true)
			displayText = promptText + cursorStyle.Render(" ")
		} else {
			displayText = promptText
		}
		promptStyle := lipgloss.NewStyle().
			Foreground(globalTheme.Warning).
			Width(cl.width)
		return promptStyle.Render(displayText)
	}

	// Priority 2: Show command if in command mode
	if cl.mode == CommandLineCommand {
		// Build command text with cursor
		cmdText := ":" + cl.command

		// Insert cursor at position (account for leading ":")
		displayPos := cl.cursorPos + 1
		var displayText string
		if cl.showCursor {
			if displayPos < len(cmdText) {
				// Cursor in middle of text
				before := cmdText[:displayPos]
				cursorChar := string(cmdText[displayPos])
				after := cmdText[displayPos+1:]
				cursorStyle := lipgloss.NewStyle().Reverse(true)
				displayText = before + cursorStyle.Render(cursorChar) + after
			} else {
				// Cursor at end
				cursorStyle := lipgloss.NewStyle().Reverse(true)
				displayText = cmdText + cursorStyle.Render(" ")
			}
		} else {
			displayText = cmdText
		}

		cmdStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Width(cl.width)
		return cmdStyle.Render(displayText)
	}

	// Priority 3: Show toast if active
	if len(cl.toasts) > 0 {
		toast := cl.toasts[len(cl.toasts)-1]
		style := cl.style

		contentWidth := lipgloss.Width(toast.Message)
		frameWidth, _ := style.GetFrameSize()
		maxWidth := style.GetMaxWidth()
		if maxWidth > 0 && contentWidth+frameWidth > maxWidth {
			style = style.Copy().MaxWidth(contentWidth + frameWidth)
		}

		switch toast.Type {
		case "info":
			style = style.Background(lipgloss.NoColor{}).Foreground(globalTheme.TextColor)
		case "success":
			style = style.Background(lipgloss.NoColor{}).Foreground(globalTheme.TextColor)
		case "warning":
			style = style.Background(lipgloss.NoColor{}).Foreground(globalTheme.Warning)
		case "error":
			style = style.Background(globalTheme.Error)
		}

		return style.Render(toast.Message)
	}

	// Default: Show blank line
	return ""
}

// LoadHistory loads command history from a history store
func (cl *CommandLineComponent) LoadHistory(entries []storage.HistoryEntry) {
	cl.history = make([]string, 0, len(entries))
	for _, entry := range entries {
		cl.history = append(cl.history, entry.Content)
	}
	cl.historyCursor = len(cl.history)
}

// AddToHistory adds a command to the history
func (cl *CommandLineComponent) AddToHistory(cmd string) {
	// Don't add empty commands or duplicates of the last command
	if cmd == "" {
		return
	}
	if len(cl.history) > 0 && cl.history[len(cl.history)-1] == cmd {
		return
	}
	cl.history = append(cl.history, cmd)
	cl.historyCursor = len(cl.history)
}

// NavigateHistory navigates through command history
// direction: -1 for previous (up), +1 for next (down)
// Returns true if navigation occurred
func (cl *CommandLineComponent) NavigateHistory(direction int) bool {
	if len(cl.history) == 0 {
		return false
	}

	switch {
	case direction < 0:
		// Navigate backwards (older commands)
		if !cl.historySaved {
			cl.historyPending = cl.command
			cl.historySaved = true
		}
		if cl.historyCursor == len(cl.history) {
			cl.historyCursor = len(cl.history) - 1
		} else if cl.historyCursor > 0 {
			cl.historyCursor--
		}
		if cl.historyCursor >= 0 && cl.historyCursor < len(cl.history) {
			cl.command = cl.history[cl.historyCursor]
			cl.cursorPos = len(cl.command)
			return true
		}
	case direction > 0:
		// Navigate forwards (newer commands)
		if !cl.historySaved {
			return false
		}
		if cl.historyCursor < len(cl.history)-1 {
			cl.historyCursor++
			cl.command = cl.history[cl.historyCursor]
			cl.cursorPos = len(cl.command)
			return true
		}
		// Reached the end, restore pending command
		cl.historyCursor = len(cl.history)
		cl.command = cl.historyPending
		cl.cursorPos = len(cl.command)
		cl.historySaved = false
		return true
	}

	return false
}

// HandleKey handles keyboard input for the command line component
func (cl *CommandLineComponent) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	// Handle yes/no mode
	if cl.IsInYesNoMode() {
		keyStr := msg.String()
		switch keyStr {
		case "y", "Y":
			exitCmd := cl.ExitYesNoMode()
			return tea.Batch(
				exitCmd,
				func() tea.Msg { return yesNoResponseMsg{answer: true} },
			), true
		case "n", "N", "esc":
			exitCmd := cl.ExitYesNoMode()
			return tea.Batch(
				exitCmd,
				func() tea.Msg { return yesNoResponseMsg{answer: false} },
			), true
		}
		// Ignore other keys in yes/no mode
		return nil, true
	}

	if !cl.IsInCommandMode() {
		return nil, false
	}

	keyStr := msg.String()

	switch keyStr {
	case "esc":
		// Cancel command mode
		exitCmd := cl.ExitCommandMode()
		return tea.Batch(
			exitCmd,
			func() tea.Msg { return commandCancelledMsg{} },
		), true

	case "enter":
		// Execute command
		cmdText := cl.GetCommand()
		exitCmd := cl.ExitCommandMode()
		if cmdText != "" {
			cl.AddToHistory(cmdText)
			return tea.Batch(
				exitCmd,
				func() tea.Msg { return commandReadyMsg{command: cmdText} },
			), true
		}
		return tea.Batch(
			exitCmd,
			func() tea.Msg { return commandCancelledMsg{} },
		), true

	case "backspace", "ctrl+h":
		if cl.cursorPos == 0 {
			exitCmd := cl.ExitCommandMode()
			return tea.Batch(
				exitCmd,
				func() tea.Msg { return commandCancelledMsg{} },
			), true
		}
		cl.DeleteCharBackward()
		return func() tea.Msg { return commandTextChangedMsg{} }, true

	case "delete":
		cl.DeleteCharForward()
		return func() tea.Msg { return commandTextChangedMsg{} }, true

	case "left":
		cl.MoveCursorLeft()
		return nil, true

	case "right":
		cl.MoveCursorRight()
		return nil, true

	case "home", "ctrl+a":
		cl.MoveCursorHome()
		return nil, true

	case "end", "ctrl+e":
		cl.MoveCursorEnd()
		return nil, true

	case "up":
		// Navigate history or completion (TUI decides based on completion state)
		direction := -1
		if cl.NavigateHistory(direction) {
			// History was navigated, signal text change
			return func() tea.Msg { return commandTextChangedMsg{} }, true
		}
		// No history or at beginning, send to TUI for completion
		return func() tea.Msg { return navigateHistoryMsg{direction: direction} }, true

	case "down":
		// Navigate history or completion (TUI decides based on completion state)
		direction := 1
		if cl.NavigateHistory(direction) {
			// History was navigated, signal text change
			return func() tea.Msg { return commandTextChangedMsg{} }, true
		}
		// No history or at end, send to TUI for completion
		return func() tea.Msg { return navigateHistoryMsg{direction: direction} }, true

	case "tab":
		// Accept completion
		return func() tea.Msg { return acceptCompletionMsg{} }, true

	case "ctrl+n":
		// Navigate completion down
		return func() tea.Msg { return navigateCompletionMsg{direction: 1} }, true

	case "shift+tab", "ctrl+p":
		// Navigate completion up
		return func() tea.Msg { return navigateCompletionMsg{direction: -1} }, true

	case "space", " ":
		cl.InsertRune(' ')
		return func() tea.Msg { return commandTextChangedMsg{} }, true

	default:
		// Insert character if it's a printable rune
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				cl.InsertRune(r)
			}
			return func() tea.Msg { return commandTextChangedMsg{} }, true
		}
		return nil, false
	}
}
