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
	Viewport         viewport.Model
	Messages         []string
	Width            int
	Height           int
	Style            lipgloss.Style
	AutoScroll       bool // Track if auto-scrolling is enabled
	UserScrolled     bool // Track if user has manually scrolled
}

// NewChatComponent creates a new chat component
func NewChatComponent(width, height int) ChatComponent {
	vp := viewport.New(width-2, height-2) // Account for borders
	vp.SetContent("Welcome to Asimi CLI! Send a message to start chatting.")

	return ChatComponent{
		Viewport:     vp,
		Messages:     []string{"Welcome to Asimi CLI! Send a message to start chatting."},
		Width:        width,
		Height:       height,
		AutoScroll:   true,  // Enable auto-scroll by default
		UserScrolled: false, // User hasn't scrolled yet
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#F4DB53")). // Terminal7 chat border
			Background(lipgloss.Color("#11051E")).       // Terminal7 chat background
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
	// Reset auto-scroll when new message is added
	c.AutoScroll = true
	c.UserScrolled = false
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
		
		// Check if this is a thinking message
		if strings.Contains(message, "<thinking>") && strings.Contains(message, "</thinking>") {
			// Extract thinking content and regular content
			thinkingContent, regularContent := extractThinkingContent(message)
			
			// Style thinking content differently
			if thinkingContent != "" {
				thinkingStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("#004444")). // Terminal7 text-error color
					Italic(true).
					Padding(0, 1).
					Border(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("#373702")) // Terminal7 dark border
				
				wrappedThinking := wordwrap.String("💭 Thinking: "+thinkingContent, c.Width-4)
				messageViews = append(messageViews, thinkingStyle.Render(wrappedThinking))
			}
			
			// Style regular content normally if present
			if regularContent != "" {
				if strings.HasPrefix(regularContent, "You:") {
					messageStyle = lipgloss.NewStyle().
						Foreground(lipgloss.Color("#F952F9")). // Terminal7 prompt border
						Padding(0, 1)
				} else {
					messageStyle = lipgloss.NewStyle().
						Foreground(lipgloss.Color("#01FAFA")). // Terminal7 text color
						Padding(0, 1)
				}
				messageViews = append(messageViews,
					messageStyle.Render(wordwrap.String(regularContent, c.Width)))
			}
		} else {
			// Regular message styling
			if strings.HasPrefix(message, "You:") {
				messageStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#F952F9")). // Terminal7 prompt border
					Padding(0, 1)
			} else {
				messageStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#01FAFA")). // Terminal7 text color
					Padding(0, 1)
			}
			messageViews = append(messageViews,
				messageStyle.Render(wordwrap.String(message, c.Width)))
		}
	}
	content := lipgloss.JoinVertical(lipgloss.Left, messageViews...)
	c.Viewport.SetContent(content)
	
	// Only auto-scroll if user hasn't manually scrolled
	if c.AutoScroll && !c.UserScrolled {
		c.Viewport.GotoBottom()
	}
}

// extractThinkingContent separates thinking content from regular content
func extractThinkingContent(message string) (thinking, regular string) {
	// Find thinking tags
	startTag := "<thinking>"
	endTag := "</thinking>"
	
	startIdx := strings.Index(message, startTag)
	if startIdx == -1 {
		return "", message
	}
	
	endIdx := strings.Index(message, endTag)
	if endIdx == -1 {
		return "", message
	}
	
	// Extract thinking content
	thinkingStart := startIdx + len(startTag)
	thinking = strings.TrimSpace(message[thinkingStart:endIdx])
	
	// Extract regular content (before and after thinking)
	before := strings.TrimSpace(message[:startIdx])
	after := strings.TrimSpace(message[endIdx+len(endTag):])
	
	if before != "" && after != "" {
		regular = before + "\n\n" + after
	} else if before != "" {
		regular = before
	} else {
		regular = after
	}
	
	return thinking, regular
}

// Update handles messages for the chat component
func (c ChatComponent) Update(msg interface{}) (ChatComponent, interface{}) {
	var cmd interface{}
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			c.Viewport.LineUp(1)
			c.UserScrolled = true // User manually scrolled
		case tea.MouseWheelDown:
			c.Viewport.LineDown(1)
			c.UserScrolled = true // User manually scrolled
		}
	case tea.KeyMsg:
		// Track keyboard scrolling as well
		switch msg.String() {
		case "up", "k":
			c.Viewport.LineUp(1)
			c.UserScrolled = true
		case "down", "j":
			c.Viewport.LineDown(1)
			c.UserScrolled = true
		case "pgup":
			c.Viewport.HalfViewUp()
			c.UserScrolled = true
		case "pgdown":
			c.Viewport.HalfViewDown()
			c.UserScrolled = true
		case "home":
			c.Viewport.GotoTop()
			c.UserScrolled = true
		case "end":
			c.Viewport.GotoBottom()
			// If user scrolls to bottom, re-enable auto-scroll
			c.UserScrolled = false
			c.AutoScroll = true
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
