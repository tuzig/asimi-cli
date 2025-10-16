package main

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

// PromptComponent represents the user input text area
type PromptComponent struct {
	TextArea    textarea.Model
	Placeholder string
	Height      int
	Width       int
	Style       lipgloss.Style
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

	return PromptComponent{
		TextArea: ta,
		Height:   height,
		Width:    width,
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

// Update handles messages for the prompt component
func (p PromptComponent) Update(msg interface{}) (PromptComponent, interface{}) {
	var cmd interface{}
	p.TextArea, cmd = p.TextArea.Update(msg)
	return p, cmd
}

// View renders the prompt component
func (p PromptComponent) View() string {
	return p.Style.Render(wordwrap.String(p.TextArea.View(), p.Width))
}
