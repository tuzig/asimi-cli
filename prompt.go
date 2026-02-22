package main

import (
	"fmt"
	"strings"

	"github.com/afittestide/asimi/shogunate"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

// Vi mode constants
const (
	ViModeInsert      = "insert"
	ViModeNormal      = "normal"
	ViModeVisual      = "visual"
	ViModeScroll      = "scroll"
	ViModeCommandLine = "command"
	ViModeLearning    = "learning"
	ViModeAnswering   = "answering"
)

// Placeholder text constants
const (
	PlaceholderDefault = "Type your message here. Enter to send, ESC to change mode"
)

// PromptComponent represents the user input text area
type PromptComponent struct {
	TextArea       textarea.Model
	Placeholder    string
	Height         int
	Width          int
	MaxHeight      int // Maximum height (50% of screen height)
	ScreenHeight   int // Total screen height
	ExpandedHeight int // Height to grow to when multiline (default: 10)
	Style          lipgloss.Style
	ViCurrentMode  string // Current vi mode: insert, normal, or command
	viPendingOp    string // Track pending operation (e.g., "d" or "c")
	viNormalKeyMap textarea.KeyMap
	viInsertKeyMap textarea.KeyMap
	answering      *AnsweringState // non-nil when in answering mode
}

// SubmitPromptMsg is a message sent when the user submits a prompt
type SubmitPromptMsg struct {
	Prompt string
}

// AnsweredMsg is emitted when the user finishes answering all questions
type AnsweredMsg struct {
	RequestID string
	Answers   []string // option text or free-text per question
}

// AnsweringCancelMsg is emitted when the user cancels answering
type AnsweringCancelMsg struct {
	RequestID string
}

// AnsweringState holds state for the answering mode UI
type AnsweringState struct {
	RequestID string
	Title     string
	Questions []AnsweringQuestion
	Answers   []string // collected answers
	Current   int      // index of current question being answered
	Typing    bool     // true when user picked "Other..." and is typing
}

// AnsweringQuestion holds a single question with selectable options
type AnsweringQuestion struct {
	Text     string
	Options  []string // 2-4 options from LLM; "Other..." appended automatically
	Selected int      // cursor position (includes "Other..." entry)
}

// NewPromptComponent creates a new prompt component
func NewPromptComponent(width, height int) PromptComponent {
	ta := textarea.New()
	ta.Placeholder = PlaceholderDefault
	ta.ShowLineNumbers = false

	// Remove background color from textarea styles
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()

	ta.Focus()

	// Set the dimensions
	ta.SetWidth(width - 2) // Account for borders
	ta.SetHeight(height)   // Account for borders

	// Create a vi normal mode keymap (navigation only, no text input)
	viNormalKeyMap := textarea.KeyMap{
		CharacterBackward:          key.NewBinding(key.WithKeys("h", "left")),
		CharacterForward:           key.NewBinding(key.WithKeys("l", "right")),
		DeleteAfterCursor:          key.NewBinding(key.WithKeys("D")),
		DeleteBeforeCursor:         key.NewBinding(key.WithKeys("d0")),
		DeleteCharacterBackward:    key.NewBinding(key.WithKeys("X")),
		DeleteCharacterForward:     key.NewBinding(key.WithKeys("x")),
		DeleteWordBackward:         key.NewBinding(key.WithKeys("db")),
		DeleteWordForward:          key.NewBinding(key.WithKeys("dw")),
		InsertNewline:              key.NewBinding(key.WithKeys()), // Disabled in normal mode
		LineEnd:                    key.NewBinding(key.WithKeys("$", "end")),
		LineStart:                  key.NewBinding(key.WithKeys("0", "^", "home")),
		LineNext:                   key.NewBinding(key.WithKeys("j", "down")),
		LinePrevious:               key.NewBinding(key.WithKeys("k", "up")),
		Paste:                      key.NewBinding(key.WithKeys("p")),
		WordBackward:               key.NewBinding(key.WithKeys("b")),
		WordForward:                key.NewBinding(key.WithKeys("w")),
		InputBegin:                 key.NewBinding(key.WithKeys("gg")),
		InputEnd:                   key.NewBinding(key.WithKeys("G")),
		UppercaseWordForward:       key.NewBinding(key.WithKeys()), // Disabled
		LowercaseWordForward:       key.NewBinding(key.WithKeys()), // Disabled
		CapitalizeWordForward:      key.NewBinding(key.WithKeys()), // Disabled
		TransposeCharacterBackward: key.NewBinding(key.WithKeys()), // Disabled
	}

	// Create a vi insert mode keymap (similar to normal editing)
	viInsertKeyMap := textarea.KeyMap{
		CharacterBackward:          key.NewBinding(key.WithKeys("left")),
		CharacterForward:           key.NewBinding(key.WithKeys("right")),
		DeleteAfterCursor:          key.NewBinding(key.WithKeys("ctrl+k")),
		DeleteBeforeCursor:         key.NewBinding(key.WithKeys("ctrl+u")),
		DeleteCharacterBackward:    key.NewBinding(key.WithKeys("backspace", "ctrl+h")),
		DeleteCharacterForward:     key.NewBinding(key.WithKeys("delete")),
		DeleteWordBackward:         key.NewBinding(key.WithKeys("ctrl+w")),
		DeleteWordForward:          key.NewBinding(key.WithKeys("alt+d")),
		InsertNewline:              key.NewBinding(key.WithKeys("enter", "ctrl+m")),
		LineEnd:                    key.NewBinding(key.WithKeys("end")),
		LineStart:                  key.NewBinding(key.WithKeys("home")),
		LineNext:                   key.NewBinding(key.WithKeys("down")),
		LinePrevious:               key.NewBinding(key.WithKeys("up")),
		Paste:                      key.NewBinding(key.WithKeys("ctrl+v")),
		WordBackward:               key.NewBinding(key.WithKeys("alt+left")),
		WordForward:                key.NewBinding(key.WithKeys("alt+right")),
		InputBegin:                 key.NewBinding(key.WithKeys("ctrl+home")),
		InputEnd:                   key.NewBinding(key.WithKeys("ctrl+end")),
		UppercaseWordForward:       key.NewBinding(key.WithKeys("ctrl+alt+u")),
		LowercaseWordForward:       key.NewBinding(key.WithKeys("ctrl+alt+l")),
		CapitalizeWordForward:      key.NewBinding(key.WithKeys("ctrl+alt+c")),
		TransposeCharacterBackward: key.NewBinding(key.WithKeys("ctrl+t")),
	}

	// Ensure globalTheme is initialized (for tests)
	if globalTheme == nil {
		globalTheme = NewTheme()
	}

	prompt := PromptComponent{
		TextArea:       ta,
		Height:         height,
		Width:          width,
		ExpandedHeight: 10,           // Default expanded height, can be overridden by config
		ViCurrentMode:  ViModeInsert, // Start in insert mode
		viPendingOp:    "",           // No pending operation
		viNormalKeyMap: viNormalKeyMap,
		viInsertKeyMap: viInsertKeyMap,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(globalTheme.PromptOnBorder). // Use theme's on border for insert mode
			Width(width).
			Height(height),
	}

	// Set insert mode keymap
	prompt.TextArea.KeyMap = viInsertKeyMap

	return prompt
}

// SetWidth updates the width of the prompt component
func (p *PromptComponent) SetWidth(width int) {
	p.Width = width
	p.Style = p.Style.Width(width)
	p.TextArea.SetWidth(width - 2)
}

// SetHeight updates the height of the prompt component
// Height is constrained to MaxHeight (50% of screen)
func (p *PromptComponent) SetHeight(height int) {
	// Apply max height constraint if set
	if p.MaxHeight > 0 && height > p.MaxHeight {
		height = p.MaxHeight
	}
	p.Height = height
	p.Style = p.Style.Height(height)
	p.TextArea.SetHeight(height)
}

// SetScreenHeight updates the screen height and recalculates max height
func (p *PromptComponent) SetScreenHeight(screenHeight int) {
	p.ScreenHeight = screenHeight
	// Max height is 50% of screen height
	p.MaxHeight = screenHeight / 2
	// Re-apply height constraint
	if p.Height > p.MaxHeight {
		p.SetHeight(p.Height)
	}
}

// SetExpandedHeight sets the height the prompt grows to when content is multiline
func (p *PromptComponent) SetExpandedHeight(height int) {
	p.ExpandedHeight = height
}

// CalculateDesiredHeight returns the desired height based on content
// When user input is more than one line (including wrapped lines), grows to ExpandedHeight lines (if screen allows)
// Goes back to 2-line height when in scroll mode or when prompt is cleared
func (p *PromptComponent) CalculateDesiredHeight() int {
	// Scroll mode always minimizes to 2 lines
	if p.ViCurrentMode == ViModeScroll {
		return 2
	}

	// In answering mode, size for: title + question + options + Other... + padding
	if p.answering != nil && p.answering.Current < len(p.answering.Questions) {
		q := p.answering.Questions[p.answering.Current]
		// 1 title + 1 blank + question lines + option count + 1 (Other...) + 1 padding
		h := 3 + len(q.Options) + 1 + 1
		if p.MaxHeight > 0 && h > p.MaxHeight {
			return p.MaxHeight
		}
		return h
	}

	value := p.TextArea.Value()

	// Return to minimum height (2 lines) when prompt is cleared (empty)
	if value == "" {
		return 2
	}

	// Calculate the number of visual lines (accounting for word wrap)
	// The textarea width minus some padding for the cursor
	textWidth := p.Width - 4 // Account for borders and cursor
	if textWidth <= 0 {
		textWidth = 1
	}

	visualLines := 0
	for _, line := range strings.Split(value, "\n") {
		if len(line) == 0 {
			visualLines++
		} else {
			// Calculate how many visual rows this line takes
			visualLines += (len(line) + textWidth - 1) / textWidth
		}
	}

	// Return to minimum height (2 lines) when content fits in a single line
	if visualLines <= 1 {
		return 2
	}

	// When content is more than one line, grow to ExpandedHeight
	expandedHeight := p.ExpandedHeight
	if expandedHeight <= 0 {
		expandedHeight = 10 // Fallback default
	}

	// Apply max height constraint (50% of screen height)
	if p.MaxHeight > 0 && expandedHeight > p.MaxHeight {
		return p.MaxHeight
	}

	return expandedHeight
}

// SetValue sets the text value of the prompt
func (p *PromptComponent) SetValue(value string) {
	p.TextArea.SetValue(value)
}

// Value returns the current text value
func (p PromptComponent) Value() string {
	return p.TextArea.Value()
}

// Focus gives focus to the prompt
func (p *PromptComponent) Focus() {
	p.TextArea.Focus()
}

// Blur removes focus from the prompt
func (p *PromptComponent) Blur() {
	p.TextArea.Blur()
}

// EnterViNormalMode switches to vi normal mode (for navigation)
func (p *PromptComponent) EnterViNormalMode() {
	p.ViCurrentMode = ViModeNormal
	p.viPendingOp = ""
	p.TextArea.KeyMap = p.viNormalKeyMap
	p.TextArea.Placeholder = "i for insert mode, : for commands, ↑↓ for history"
	p.updateViModeStyle()
}

// EnterViInsertMode switches to vi insert mode
func (p *PromptComponent) EnterViInsertMode() {
	p.ViCurrentMode = ViModeInsert
	p.viPendingOp = ""
	p.TextArea.KeyMap = p.viInsertKeyMap
	p.TextArea.Placeholder = PlaceholderDefault
	p.updateViModeStyle()
}

// EnterViCommandLineMode switches to vi command-line mode
func (p *PromptComponent) EnterViCommandLineMode() {
	p.ViCurrentMode = ViModeCommandLine
	p.viPendingOp = ""
	// Use insert keymap for command-line editing
	p.TextArea.KeyMap = p.viInsertKeyMap
	p.TextArea.Placeholder = "Enter command below..."
	p.updateViModeStyle()
}

// IsViNormalMode returns true if in vi normal mode
func (p PromptComponent) IsViNormalMode() bool {
	return p.ViCurrentMode == ViModeNormal
}

// IsViInsertMode returns true if in vi insert mode
func (p PromptComponent) IsViInsertMode() bool {
	return p.ViCurrentMode == ViModeInsert
}

// IsViCommandLineMode returns true if in vi command-line mode
func (p PromptComponent) IsViCommandLineMode() bool {
	return p.ViCurrentMode == ViModeCommandLine
}

// EnterViLearningMode switches to vi learning mode
func (p *PromptComponent) EnterViLearningMode() {
	p.ViCurrentMode = ViModeLearning
	p.viPendingOp = ""
	// Use insert keymap for learning mode editing
	p.TextArea.KeyMap = p.viInsertKeyMap
	p.TextArea.Placeholder = "Enter learning note (will be appended to project's memory file)"
	p.updateViModeStyle()
}

// IsViLearningMode returns true if in vi learning mode
func (p PromptComponent) IsViLearningMode() bool {
	return p.ViCurrentMode == ViModeLearning
}

// EnterViScrollMode switches to vi scroll mode (for scrolling chat history)
func (p *PromptComponent) EnterViScrollMode() {
	p.ViCurrentMode = ViModeScroll
	p.viPendingOp = ""
	// Use normal keymap for scroll mode (no text editing)
	p.TextArea.KeyMap = p.viNormalKeyMap
	p.TextArea.Placeholder = "j/k to scroll | CTRL-f/b & d/u as in vi | i/:/ESC to exit"
	p.updateViModeStyle()
}

// IsViScrollMode returns true if in vi scroll mode
func (p PromptComponent) IsViScrollMode() bool {
	return p.ViCurrentMode == ViModeScroll
}

// ViModeStatus returns current vi mode status for display components
func (p PromptComponent) ViModeStatus() (enabled bool, mode string, pending string) {
	return true, p.ViCurrentMode, p.viPendingOp
}

// HandleZhengmingPending parses a ZhengmingPendingMsg and enters answering mode
func (p *PromptComponent) HandleZhengmingPending(msg shogunate.ZhengmingPendingMsg) {
	aq := make([]AnsweringQuestion, len(msg.Questions))
	for i, q := range msg.Questions {
		aq[i] = AnsweringQuestion{
			Text:    q.Text,
			Options: q.Options,
		}
	}

	state := &AnsweringState{
		RequestID: msg.RequestID,
		Title:     fmt.Sprintf("Zhengming: %s asks", msg.MinisterID),
		Questions: aq,
		Answers:   make([]string, len(aq)),
	}
	p.EnterAnsweringMode(state)
}

// EnterAnsweringMode switches to answering mode with the given state
func (p *PromptComponent) EnterAnsweringMode(state *AnsweringState) {
	p.answering = state
	p.ViCurrentMode = ViModeAnswering
	p.viPendingOp = ""
	p.TextArea.Blur()
	p.Style = p.Style.BorderForeground(globalTheme.PromptOnBorder)
}

// ExitAnsweringMode leaves answering mode and returns to insert mode
func (p *PromptComponent) ExitAnsweringMode() {
	p.answering = nil
	p.EnterViInsertMode()
	p.TextArea.Focus()
}

// updateViModeStyle updates the border color based on vi mode state
// Uses globalTheme.promptOnBorder when focused on prompt (INSERT/COMMAND/LEARNING)
// Uses globalTheme.promptOffBorder when focused away from prompt (NORMAL/VISUAL/SCROLL)
func (p *PromptComponent) updateViModeStyle() {
	switch p.ViCurrentMode {
	case ViModeInsert, ViModeNormal, ViModeLearning:
		// Focus on prompt input - use "on" border
		p.Style = p.Style.BorderForeground(globalTheme.PromptOnBorder)
	default:
		// Focus away from prompt (NORMAL/VISUAL/SCROLL/etc) - use "off" border
		p.Style = p.Style.BorderForeground(globalTheme.PromptOffBorder)
	}
}

// handleViCommand processes vi commands like dd, dw, cc, cw, etc.
func (p *PromptComponent) handleViCommand(key string) (bool, tea.Cmd) {
	// Handle pending operations
	if p.viPendingOp != "" {
		command := p.viPendingOp + key
		p.viPendingOp = "" // Clear pending operation

		switch command {
		case "dd":
			// Delete current line
			return p.deleteCurrentLine()
		case "dw":
			// Delete word forward
			return p.deleteWordForward()
		case "db":
			// Delete word backward
			var cmd tea.Cmd
			p.TextArea, cmd = p.TextArea.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
			return true, cmd
		case "d$", "dD":
			// Delete to end of line
			var cmd tea.Cmd
			p.TextArea, cmd = p.TextArea.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
			return true, cmd
		case "d0", "d^":
			// Delete to beginning of line
			var cmd tea.Cmd
			p.TextArea, cmd = p.TextArea.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
			return true, cmd
		case "cc":
			// Change current line (delete line and enter insert mode)
			handled, cmd := p.deleteCurrentLine()
			if handled {
				p.EnterViInsertMode()
			}
			return handled, cmd
		case "cw":
			// Change word forward (delete word and enter insert mode)
			handled, cmd := p.deleteWordForward()
			if handled {
				p.EnterViInsertMode()
			}
			return handled, cmd
		case "cb":
			// Change word backward (delete word and enter insert mode)
			var cmd tea.Cmd
			p.TextArea, cmd = p.TextArea.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
			p.EnterViInsertMode()
			return true, cmd
		case "c$", "cD":
			// Change to end of line
			var cmd tea.Cmd
			p.TextArea, cmd = p.TextArea.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
			p.EnterViInsertMode()
			return true, cmd
		case "c0", "c^":
			// Change to beginning of line
			var cmd tea.Cmd
			p.TextArea, cmd = p.TextArea.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
			p.EnterViInsertMode()
			return true, cmd
		default:
			// Unknown command, ignore
			return false, nil
		}
	}

	// Check if this is the start of a compound command
	if key == "d" || key == "c" {
		p.viPendingOp = key
		return true, nil
	}

	return false, nil
}

// deleteCurrentLine deletes the current line
func (p *PromptComponent) deleteCurrentLine() (bool, tea.Cmd) {
	lines := strings.Split(p.TextArea.Value(), "\n")
	row := p.TextArea.Line()
	lineInfo := p.TextArea.LineInfo()
	col := lineInfo.StartColumn + lineInfo.ColumnOffset

	if row >= 0 && row < len(lines) {
		// Delete the current line
		newLines := append(lines[:row], lines[row+1:]...)
		newValue := strings.Join(newLines, "\n")
		p.TextArea.SetValue(newValue)

		// Determine target row after deletion
		targetRow := row
		if targetRow >= len(newLines) {
			targetRow = len(newLines) - 1
		}
		if targetRow < 0 {
			targetRow = 0
		}

		// Determine target column, clamped to line length
		targetCol := 0
		if targetRow >= 0 && targetRow < len(newLines) {
			if col < len(newLines[targetRow]) {
				targetCol = col
			} else {
				targetCol = len(newLines[targetRow])
			}
		}

		// Move cursor to target row
		p.TextArea.SetCursor(0)
		currentRow := p.TextArea.Line()
		for currentRow > targetRow {
			p.TextArea.CursorUp()
			currentRow = p.TextArea.Line()
		}
		for currentRow < targetRow {
			p.TextArea.CursorDown()
			currentRow = p.TextArea.Line()
		}

		// Set cursor to target column
		p.TextArea.SetCursor(targetCol)

		return true, nil
	}

	return false, nil
}

// deleteWordForward deletes from cursor to the end of the current word
func (p *PromptComponent) deleteWordForward() (bool, tea.Cmd) {
	value := p.TextArea.Value()
	if len(value) == 0 {
		return false, nil
	}

	// Get cursor position
	lineInfo := p.TextArea.LineInfo()
	row := p.TextArea.Line()
	col := lineInfo.ColumnOffset

	// Split into lines
	lines := strings.Split(value, "\n")
	if row < 0 || row >= len(lines) {
		return false, nil
	}

	currentLine := lines[row]
	if col >= len(currentLine) {
		// At end of line, delete the newline if there's a next line
		if row < len(lines)-1 {
			lines[row] = currentLine + lines[row+1]
			lines = append(lines[:row+1], lines[row+2:]...)
			newValue := strings.Join(lines, "\n")
			p.TextArea.SetValue(newValue)

			// Restore cursor position
			p.TextArea.SetCursor(0)
			currentRow := p.TextArea.Line()
			for currentRow < row {
				p.TextArea.CursorDown()
				currentRow = p.TextArea.Line()
			}
			p.TextArea.SetCursor(col)
			return true, nil
		}
		return false, nil
	}

	// Find the end of the current word
	endCol := col

	// Skip any leading whitespace
	for endCol < len(currentLine) && (currentLine[endCol] == ' ' || currentLine[endCol] == '\t') {
		endCol++
	}

	// Now find the end of the word
	if endCol < len(currentLine) {
		for endCol < len(currentLine) && currentLine[endCol] != ' ' && currentLine[endCol] != '\t' {
			endCol++
		}
	}

	// If we didn't move at all, just delete one character
	if endCol == col {
		endCol = col + 1
	}

	// Delete from col to endCol
	newLine := currentLine[:col] + currentLine[endCol:]
	lines[row] = newLine
	newValue := strings.Join(lines, "\n")
	p.TextArea.SetValue(newValue)

	// Restore cursor position
	p.TextArea.SetCursor(0)
	currentRow := p.TextArea.Line()
	for currentRow < row {
		p.TextArea.CursorDown()
		currentRow = p.TextArea.Line()
	}
	// Set cursor to the column position
	targetCol := col
	if targetCol > len(lines[row]) {
		targetCol = len(lines[row])
	}
	p.TextArea.SetCursor(targetCol)

	return true, nil
}

// Update handles messages for the prompt component
func (p PromptComponent) Update(msg interface{}) (PromptComponent, tea.Cmd) {
	var cmd tea.Cmd

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// Answering mode: intercept all keys
		if p.answering != nil {
			return p.updateAnswering(keyMsg)
		}
		if p.IsViInsertMode() || p.IsViNormalMode() {
			// Shift+Enter inserts a newline instead of submitting
			if keyMsg.Type == tea.KeyEnter && keyMsg.Alt {
				p.TextArea.InsertString("\n")
				return p, nil
			}
			if keyMsg.Type == tea.KeyEnter && !keyMsg.Alt {
				promptContent := strings.TrimSpace(p.TextArea.Value())
				if promptContent != "" {
					p.TextArea.SetValue("")
					p.TextArea.SetCursor(0)
					return p, func() tea.Msg { return SubmitPromptMsg{Prompt: promptContent} }
				}
				// If prompt is empty, consume the Enter key
				return p, nil
			}
		}
		if p.IsViNormalMode() {
			keyStr := keyMsg.String()

			// Always allow arrow keys and navigation keys, regardless of mode
			navigationKeys := map[string]bool{
				"up": true, "down": true, "left": true, "right": true,
				"home": true, "end": true,
				"pgup": true, "pgdown": true,
			}

			// If it's a navigation key, pass it through immediately
			if navigationKeys[keyStr] {
				p.TextArea, cmd = p.TextArea.Update(msg)
				return p, cmd
			}

			// Handle vi commands (d, c, dd, dw, cc, cw, etc.)
			handled, viCmd := p.handleViCommand(keyStr)
			if handled {
				return p, viCmd
			}

			// Allow only specific navigation and command keys in normal mode
			allowedKeys := map[string]bool{
				// Navigation (vi keys)
				"h": true, "j": true, "k": true, "l": true,
				"w": true, "b": true, "e": true,
				"0": true, "^": true, "$": true,
				"gg": true, "G": true,
				// Deletion (single character)
				"x": true, "X": true,
				// Capital commands
				"D": true, // Delete to end of line
				// Other
				"p": true,                                                        // paste
				":": true,                                                        // command mode (handled in tui.go)
				"i": true, "I": true, "a": true, "A": true, "o": true, "O": true, // insert mode triggers
			}

			// If it's not an allowed key and it's a single character (potential text input),
			// ignore it to prevent text insertion in normal mode
			if !allowedKeys[keyStr] && len(keyStr) == 1 && p.viPendingOp == "" {
				// Ignore this key in normal mode
				return p, nil
			}
		}
	}

	p.TextArea, cmd = p.TextArea.Update(msg)
	return p, cmd
}

// View renders the prompt component
func (p PromptComponent) View() string {
	if p.answering != nil {
		return p.viewAnswering()
	}
	content := p.TextArea.View()
	return p.Style.Render(wordwrap.String(content, p.Width))
}

// viewAnswering renders the answering mode UI
func (p PromptComponent) viewAnswering() string {
	a := p.answering
	if a.Current >= len(a.Questions) {
		return p.Style.Render("Submitting answers...")
	}
	q := a.Questions[a.Current]

	var b strings.Builder
	// Title
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(a.Title))
	b.WriteByte('\n')
	// Question text
	b.WriteString(q.Text)
	b.WriteByte('\n')

	if a.Typing {
		// Show textarea for free-text input
		b.WriteByte('\n')
		b.WriteString(p.TextArea.View())
	} else {
		// Render options + "Other..."
		allOptions := append(q.Options, "Other...")
		for i, opt := range allOptions {
			if i == q.Selected {
				b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(globalTheme.PromptOnBorder).Render(fmt.Sprintf("  ▶ %s", opt)))
			} else {
				b.WriteString(fmt.Sprintf("    %s", opt))
			}
			if i < len(allOptions)-1 {
				b.WriteByte('\n')
			}
		}
	}

	return p.Style.Render(b.String())
}

// updateAnswering handles key input in answering mode
func (p PromptComponent) updateAnswering(keyMsg tea.KeyMsg) (PromptComponent, tea.Cmd) {
	a := p.answering
	if a == nil || a.Current >= len(a.Questions) {
		return p, nil
	}
	q := &a.Questions[a.Current]

	// When typing free-text ("Other..." selected)
	if a.Typing {
		switch keyMsg.Type {
		case tea.KeyEscape:
			// Go back to option selection
			a.Typing = false
			p.TextArea.SetValue("")
			p.TextArea.Blur()
			return p, nil
		case tea.KeyEnter:
			if !keyMsg.Alt {
				text := strings.TrimSpace(p.TextArea.Value())
				if text == "" {
					return p, nil
				}
				p.TextArea.SetValue("")
				p.TextArea.Blur()
				a.Typing = false
				return p.advanceAnswer(text)
			}
		}
		// Delegate to textarea for typing
		p.TextArea, _ = p.TextArea.Update(keyMsg)
		return p, nil
	}

	// Option selection mode
	totalOptions := len(q.Options) + 1 // +1 for "Other..."
	switch keyMsg.String() {
	case "j", "down":
		q.Selected = (q.Selected + 1) % totalOptions
	case "k", "up":
		q.Selected = (q.Selected - 1 + totalOptions) % totalOptions
	case "enter":
		if q.Selected == len(q.Options) {
			// "Other..." selected — switch to typing mode
			a.Typing = true
			p.TextArea.SetValue("")
			p.TextArea.Placeholder = "Type your answer..."
			p.TextArea.Focus()
		} else {
			return p.advanceAnswer(q.Options[q.Selected])
		}
	case "esc":
		return p, func() tea.Msg { return AnsweringCancelMsg{RequestID: a.RequestID} }
	}
	return p, nil
}

// advanceAnswer records the answer and moves to the next question or completes
func (p PromptComponent) advanceAnswer(answer string) (PromptComponent, tea.Cmd) {
	a := p.answering
	a.Answers[a.Current] = answer
	a.Current++

	if a.Current >= len(a.Questions) {
		answers := make([]string, len(a.Answers))
		copy(answers, a.Answers)
		requestID := a.RequestID
		return p, func() tea.Msg {
			return AnsweredMsg{RequestID: requestID, Answers: answers}
		}
	}
	return p, nil
}
