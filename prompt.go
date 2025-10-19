package main

import (
	"strings"

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
	ViModeCommandLine = "command"
)

// PromptComponent represents the user input text area
type PromptComponent struct {
	TextArea       textarea.Model
	Placeholder    string
	Height         int
	Width          int
	Style          lipgloss.Style
	ViMode         bool   // Track if vi mode is enabled
	ViCurrentMode  string // Current vi mode: insert, normal, visual, or command
	viPendingOp    string // Track pending operation (e.g., "d" or "c")
	normalKeyMap   textarea.KeyMap
	viNormalKeyMap textarea.KeyMap
	viInsertKeyMap textarea.KeyMap
}

// NewPromptComponent creates a new prompt component
func NewPromptComponent(width, height int) PromptComponent {
	ta := textarea.New()
	ta.Placeholder = "Type your message here..."
	ta.ShowLineNumbers = false
	ta.Focus()

	// Set the dimensions
	ta.SetWidth(width - 2) // Account for borders
	ta.SetHeight(height)   // Account for borders

	// Store the default (normal) keymap
	normalKeyMap := ta.KeyMap

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

	return PromptComponent{
		TextArea:       ta,
		Height:         height,
		Width:          width,
		ViMode:         false,        // Default to normal mode
		ViCurrentMode:  ViModeInsert, // When vi mode is enabled, start in insert mode
		viPendingOp:    "",           // No pending operation
		normalKeyMap:   normalKeyMap,
		viNormalKeyMap: viNormalKeyMap,
		viInsertKeyMap: viInsertKeyMap,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#F952F9")). // Terminal7 prompt border
			Width(width).
			Height(height),
	}
}

// SetWidth updates the width of the prompt component
func (p *PromptComponent) SetWidth(width int) {
	p.Width = width
	p.Style = p.Style.Width(width)
	p.TextArea.SetWidth(width - 2)
}

// SetHeight updates the height of the prompt component
func (p *PromptComponent) SetHeight(height int) {
	p.Height = height
	p.Style = p.Style.Height(height)
	p.TextArea.SetHeight(height)
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

// SetViMode enables or disables vi mode
func (p *PromptComponent) SetViMode(enabled bool) {
	p.ViMode = enabled

	if enabled {
		// Start in insert mode when enabling vi mode
		p.ViCurrentMode = ViModeInsert
		p.TextArea.KeyMap = p.viInsertKeyMap
		p.viPendingOp = ""
		p.updateViModeStyle()
	} else {
		// Return to normal keymap
		p.ViCurrentMode = ""
		p.TextArea.KeyMap = p.normalKeyMap
		p.viPendingOp = ""
		p.Style = p.Style.BorderForeground(lipgloss.Color("#F952F9")) // Terminal7 prompt border (magenta)
	}
}

// EnterViNormalMode switches to vi normal mode (for navigation)
func (p *PromptComponent) EnterViNormalMode() {
	if !p.ViMode {
		return
	}
	p.ViCurrentMode = ViModeNormal
	p.viPendingOp = ""
	p.TextArea.KeyMap = p.viNormalKeyMap
	p.updateViModeStyle()
}

// EnterViVisualMode switches to vi visual mode (for text selection)
func (p *PromptComponent) EnterViVisualMode() {
	if !p.ViMode {
		return
	}
	p.ViCurrentMode = ViModeVisual
	p.viPendingOp = ""
	p.TextArea.KeyMap = p.viNormalKeyMap // Visual mode uses similar navigation
	p.updateViModeStyle()
}

// EnterViInsertMode switches to vi insert mode
func (p *PromptComponent) EnterViInsertMode() {
	if !p.ViMode {
		return
	}
	p.ViCurrentMode = ViModeInsert
	p.viPendingOp = ""
	p.TextArea.KeyMap = p.viInsertKeyMap
	p.updateViModeStyle()
}

// EnterViCommandLineMode switches to vi command-line mode
func (p *PromptComponent) EnterViCommandLineMode() {
	if !p.ViMode {
		return
	}
	p.ViCurrentMode = ViModeCommandLine
	p.viPendingOp = ""
	// Use normal keymap for command-line editing (like when vi mode is disabled)
	p.TextArea.KeyMap = p.normalKeyMap
	p.updateViModeStyle()
}

// IsViNormalMode returns true if in vi normal mode
func (p PromptComponent) IsViNormalMode() bool {
	return p.ViMode && p.ViCurrentMode == ViModeNormal
}

// IsViVisualMode returns true if in vi visual mode
func (p PromptComponent) IsViVisualMode() bool {
	return p.ViMode && p.ViCurrentMode == ViModeVisual
}

// IsViInsertMode returns true if in vi insert mode
func (p PromptComponent) IsViInsertMode() bool {
	return p.ViMode && p.ViCurrentMode == ViModeInsert
}

// IsViCommandLineMode returns true if in vi command-line mode
func (p PromptComponent) IsViCommandLineMode() bool {
	return p.ViMode && p.ViCurrentMode == ViModeCommandLine
}

// ViModeStatus returns current vi mode status for display components
func (p PromptComponent) ViModeStatus() (enabled bool, mode string, pending string) {
	return p.ViMode, p.ViCurrentMode, p.viPendingOp
}

// updateViModeStyle updates the border color based on vi mode state
func (p *PromptComponent) updateViModeStyle() {
	if !p.ViMode {
		p.Style = p.Style.BorderForeground(lipgloss.Color("#F952F9")) // Terminal7 prompt border (magenta)
		return
	}

	switch p.ViCurrentMode {
	case ViModeInsert:
		// Insert mode: green border
		p.Style = p.Style.BorderForeground(lipgloss.Color("#00FF00")) // Green
	case ViModeNormal:
		// Normal mode: yellow border
		p.Style = p.Style.BorderForeground(lipgloss.Color("#F4DB53")) // Terminal7 warning/chat border (yellow)
	case ViModeVisual:
		// Visual mode: blue border
		p.Style = p.Style.BorderForeground(lipgloss.Color("#01FAFA")) // Terminal7 text color (cyan)
	case ViModeCommandLine:
		// Command-line mode: magenta border
		p.Style = p.Style.BorderForeground(lipgloss.Color("#F952F9")) // Terminal7 prompt border (magenta)
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

	// In vi normal or visual mode, handle special vi commands
	if p.IsViNormalMode() || p.IsViVisualMode() {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			keyStr := keyMsg.String()

			// Handle vi commands (d, c, dd, dw, cc, cw, etc.)
			handled, viCmd := p.handleViCommand(keyStr)
			if handled {
				return p, viCmd
			}

			// Allow only specific navigation and command keys in normal/visual mode
			allowedKeys := map[string]bool{
				// Navigation
				"h": true, "j": true, "k": true, "l": true,
				"left": true, "right": true, "up": true, "down": true,
				"w": true, "b": true, "e": true,
				"0": true, "^": true, "$": true,
				"gg": true, "G": true,
				"home": true, "end": true,
				// Deletion (single character)
				"x": true, "X": true,
				// Capital commands
				"D": true, // Delete to end of line
				// Other
				"p": true,                                                        // paste
				":": true,                                                        // command mode (handled in tui.go)
				"i": true, "I": true, "a": true, "A": true, "o": true, "O": true, // insert mode triggers
				"v": true, "V": true, // visual mode triggers
			}

			// If it's not an allowed key and it's a single character (potential text input),
			// ignore it to prevent text insertion in normal/visual mode
			if !allowedKeys[keyStr] && len(keyStr) == 1 && p.viPendingOp == "" {
				// Ignore this key in normal/visual mode
				return p, nil
			}
		}
	}

	p.TextArea, cmd = p.TextArea.Update(msg)
	return p, cmd
}

// View renders the prompt component
func (p PromptComponent) View() string {
	content := p.TextArea.View()

	return p.Style.Render(wordwrap.String(content, p.Width))
}
