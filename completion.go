package main

import (
	"log/slog"

	"github.com/charmbracelet/lipgloss"
)

// CompletionDialog represents the autocompletion pop-up
type CompletionDialog struct {
	Options           []string
	Selected          int
	Visible           bool
	Width             int
	Height            int
	Offset            int
	PositionX         int
	PositionY         int
	Style             lipgloss.Style
	SelectedItemStyle lipgloss.Style
	ScrollMargin      int
}

// NewCompletionDialog creates a new completion dialog
func NewCompletionDialog() CompletionDialog {
	return CompletionDialog{
		Options:  []string{},
		Selected: 0,
		Visible:  false,
		Width:    30,
		Height:   10,
		Offset:   0,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("230")),
		SelectedItemStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")),
		ScrollMargin: 4,
	}
}

// SetOptions updates the completion options
func (c *CompletionDialog) SetOptions(options []string) {
	c.Options = options
	if c.Selected >= len(options) {
		c.Selected = len(options) - 1
	}
	if c.Selected < 0 {
		c.Selected = 0
	}
	c.Offset = 0
}

// Show makes the dialog visible
func (c *CompletionDialog) Show() {
	c.Visible = true
}

// Hide makes the dialog invisible
func (c *CompletionDialog) Hide() {
	c.Visible = false
}

// SelectNext moves selection to the next item
func (c *CompletionDialog) SelectNext() {
	slog.Info("Select Next")
	if len(c.Options) == 0 {
		return
	}
	next := c.Selected + 1
	if next >= len(c.Options) {
		return
	}
	slog.Info(">>>", "next", next, "offset", c.Offset, "height", c.Height)
	if next >= c.Offset+c.Height-c.ScrollMargin {
		if c.Offset < len(c.Options)-c.Height {
			c.Offset++
		}
	}
	c.Selected = next
}

// SelectPrev moves selection to the previous item
func (c *CompletionDialog) SelectPrev() {
	slog.Info("Select Prev")
	if c.Selected > 0 {
		c.Selected--
		slog.Info(">>>", "selected", c.Selected, "offset", c.Offset)
		if c.Selected < c.Offset+c.ScrollMargin {
			if c.Offset > 0 {
				c.Offset--
			}
		}
	}
}

// GetSelected returns the currently selected option
func (c CompletionDialog) GetSelected() string {
	if c.Selected >= 0 && c.Selected < len(c.Options) {
		return c.Options[c.Selected]
	}
	return ""
}

// View renders the completion dialog
func (c CompletionDialog) View() string {
	slog.Info("view")
	if !c.Visible || len(c.Options) == 0 {
		return ""
	}
	start := c.Offset
	end := c.Offset + c.Height
	slog.Info(">>>", "start", start, "end", end)
	lines := make([]string, 0, c.Height)
	for i := start; i < end; i++ {
		if i >= len(c.Options) {
			slog.Info("...", "i", i, "len", len(c.Options))
			lines = append(lines, "...")
			continue
		}
		option := c.Options[i]
		if i == c.Selected {
			lines = append(lines, c.SelectedItemStyle.Render(option))
		} else {
			lines = append(lines, option)
		}
	}

	slog.Info("lines", "len", len(lines))
	// Join the lines and render with style
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return c.Style.Render(content)
}
