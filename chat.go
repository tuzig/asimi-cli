package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

// ChatComponent represents the chat view
type ChatComponent struct {
	Viewport viewport.Model
	Messages []string
	Width    int
	Height   int
	Style    lipgloss.Style
}

// NewChatComponent creates a new chat component
func NewChatComponent(width, height int) ChatComponent {
	vp := viewport.New(width-2, height-2) // Account for borders
	vp.SetContent("Welcome to Asimi CLI! Send a message to start chatting.")

	return ChatComponent{
		Viewport: vp,
		Messages: []string{"Welcome to Asimi CLI! Send a message to start chatting."},
		Width:    width,
		Height:   height,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Width(width).
			Height(height),
	}
}

// SetWidth updates the width of the chat component
func (c *ChatComponent) SetWidth(width int) {
	c.Width = width
	c.Style = c.Style.Width(width)
	c.Viewport.Width = width - 2
	c.UpdateContent()
}

// SetHeight updates the height of the chat component
func (c *ChatComponent) SetHeight(height int) {
	c.Height = height
	c.Style = c.Style.Height(height)
	c.Viewport.Height = height
	c.UpdateContent()
}

// AddMessage adds a new message to the chat component
func (c *ChatComponent) AddMessage(message string) {
	c.Messages = append(c.Messages, message)
	c.UpdateContent()
}

// Replace last message
func (c *ChatComponent) ReplaceLastMessage(message string) {
	c.Messages[len(c.Messages)-1] = message
	c.UpdateContent()
}

// AppendToLastMessage appends text to the last message (for streaming)
func (c *ChatComponent) AppendToLastMessage(text string) {
	if len(c.Messages) == 0 {
		c.AddMessage(text)
		return
	}
	c.Messages[len(c.Messages)-1] += text
	c.UpdateContent()
}

// UpdateContent updates the viewport content based on the messages
func (c *ChatComponent) UpdateContent() {
	var messageViews []string
	for _, message := range c.Messages {
		var messageStyle lipgloss.Style
		if strings.HasPrefix(message, "You:") {
			messageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("230")).
				Padding(0, 1)
		} else {
			messageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#b0b0b0")).
				Padding(0, 1)
		}

		messageViews = append(messageViews,
			messageStyle.Render(wordwrap.String(message, c.Width)))
	}
	content := lipgloss.JoinVertical(lipgloss.Left, messageViews...)
	c.Viewport.SetContent(content)
	c.Viewport.GotoBottom()
}

// Update handles messages for the chat component
func (c ChatComponent) Update(msg interface{}) (ChatComponent, interface{}) {
	var cmd interface{}
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			c.Viewport.LineUp(1)
		case tea.MouseWheelDown:
			c.Viewport.LineDown(1)
		}
	}
	c.Viewport, cmd = c.Viewport.Update(msg)
	return c, cmd
}

// View renders the chat component
func (c ChatComponent) View() string {
	content := lipgloss.JoinVertical(lipgloss.Left, c.Viewport.View())

	// Adjust height for the header
	c.Style = c.Style.Height(c.Height)
	c.Viewport.Height = c.Height - 3 // Account for border and header

	return c.Style.Render(content)
}
