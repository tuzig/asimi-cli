package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

// ChatComponent represents the chat view
type ChatComponent struct {
	Viewport     viewport.Model
	Messages     []string
	Width        int
	Height       int
	Style        lipgloss.Style
	AutoScroll   bool // Track if auto-scrolling is enabled
	UserScrolled bool // Track if user has manually scrolled

	// Touch gesture support
	TouchStartY      int  // Y coordinate where touch/drag started
	TouchDragging    bool // Whether we're currently in a touch drag
	TouchScrollSpeed int  // Sensitivity for touch scrolling

	// Markdown rendering
	markdownRenderer *glamour.TermRenderer
}

// NewChatComponent creates a new chat component
func NewChatComponent(width, height int) ChatComponent {
	vp := viewport.New(width, height)
	vp.SetContent("Welcome to Asimi CLI! Send a message to start chatting.")

	return ChatComponent{
		Viewport:         vp,
		Messages:         []string{"Welcome to Asimi CLI! Send a message to start chatting."},
		Width:            width,
		Height:           height,
		AutoScroll:       true,  // Enable auto-scroll by default
		UserScrolled:     false, // User hasn't scrolled yet
		TouchStartY:      0,     // Initialize touch tracking
		TouchDragging:    false,
		TouchScrollSpeed: 3, // Lines to scroll per touch movement unit
		markdownRenderer: nil, // Will be initialized asynchronously via message
		Style: lipgloss.NewStyle().
			Background(lipgloss.Color("#11051E")). // Terminal7 chat background
			Width(width).
			Height(height),
	}
}

// SetWidth updates the width of the chat component
func (c *ChatComponent) SetWidth(width int) {
	c.Width = width
	c.Style = c.Style.Width(width)
	c.Viewport.Width = width

	// Don't recreate the renderer synchronously on resize - it's expensive
	// The renderer will be recreated asynchronously via handleWindowSizeMsg
	// For now, just update the content with current renderer
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

// TruncateTo keeps only the first count messages and refreshes the viewport
func (c *ChatComponent) TruncateTo(count int) {
	if count < 0 {
		count = 0
	}
	if count > len(c.Messages) {
		count = len(c.Messages)
	}
	c.Messages = append([]string(nil), c.Messages[:count]...)
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
				// Render AI messages with markdown
				messageViews = append(messageViews, c.renderMarkdown(regularContent))
			}
		} else {
			// Regular message styling
			if strings.HasPrefix(message, "You:") {
				messageStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#F952F9")) // Terminal7 prompt border

				userContent := strings.TrimSpace(strings.TrimPrefix(message, "You:"))

				wrapWidth := c.Width
				const indentSpaces = 8
				if wrapWidth > indentSpaces {
					wrapWidth -= indentSpaces
				}
				if wrapWidth < 1 {
					wrapWidth = 1
				}

				wrapped := wordwrap.String(userContent, wrapWidth)
				indent := strings.Repeat(" ", indentSpaces)
				lines := strings.Split(wrapped, "\n")
				for i := range lines {
					lines[i] = indent + lines[i]
				}

				messageViews = append(messageViews,
					messageStyle.Render(strings.Join(lines, "\n")))
			} else if strings.HasPrefix(message, "Asimi:") {
				// Render AI messages with markdown
				// Remove "Asimi: " prefix for markdown rendering
				content := strings.TrimPrefix(message, "Asimi: ")
				rendered := c.renderMarkdown(content)
				// Add "Asimi: " prefix back with styling
				asimiPrefix := lipgloss.NewStyle().
					Foreground(lipgloss.Color("#01FAFA")). // Terminal7 text color
					Bold(true).
					Render("Asimi: ")
				messageViews = append(messageViews, asimiPrefix+"\n"+rendered)
			} else {
				// Other messages (system, tool calls, etc.)
				messageStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#01FAFA")). // Terminal7 text color
					Padding(0, 1)
				messageViews = append(messageViews,
					messageStyle.Render(wordwrap.String(message, c.Width)))
			}
		}
	}
	content := lipgloss.JoinVertical(lipgloss.Left, messageViews...)
	c.Viewport.SetContent(content)

	// Only auto-scroll if user hasn't manually scrolled
	if c.AutoScroll && !c.UserScrolled {
		c.Viewport.GotoBottom()
	}
}

// renderMarkdown renders markdown content with glamour
func (c *ChatComponent) renderMarkdown(content string) string {
	if c.markdownRenderer == nil {
		// Fallback to plain text if renderer is not available
		return content
	}

	rendered, err := c.markdownRenderer.Render(content)
	if err != nil {
		// Fallback to plain text on error
		return content
	}

	return strings.TrimSpace(rendered)
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
func (c ChatComponent) Update(msg tea.Msg) (ChatComponent, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			c.Viewport.ScrollUp(1)
			c.UserScrolled = true // User manually scrolled
		case tea.MouseWheelDown:
			c.Viewport.ScrollDown(1)
			c.UserScrolled = true // User manually scrolled
		case tea.MouseLeft:
			// Start of touch/drag gesture
			if msg.Action == tea.MouseActionPress {
				c.TouchStartY = msg.Y
				c.TouchDragging = true
			} else if msg.Action == tea.MouseActionRelease {
				c.TouchDragging = false
			}
		case tea.MouseMotion:
			// Handle touch drag scrolling
			if c.TouchDragging {
				deltaY := c.TouchStartY - msg.Y
				if deltaY != 0 {
					// Calculate scroll amount based on delta
					scrollLines := deltaY / c.TouchScrollSpeed
					if scrollLines > 0 {
						// Scroll down
						for i := 0; i < scrollLines; i++ {
							c.Viewport.ScrollDown(1)
						}
						c.UserScrolled = true
					} else if scrollLines < 0 {
						// Scroll up
						for i := 0; i < -scrollLines; i++ {
							c.Viewport.ScrollUp(1)
						}
						c.UserScrolled = true
					}
					// Update start position for next motion event
					c.TouchStartY = msg.Y
				}
			}
		}
	case tea.KeyMsg:
		// Track keyboard scrolling as well
		switch msg.String() {
		case "up", "k":
			c.Viewport.ScrollUp(1)
			c.UserScrolled = true
		case "down", "j":
			c.Viewport.ScrollDown(1)
			c.UserScrolled = true
		case "pgup":
			c.Viewport.HalfPageUp()
			c.UserScrolled = true
		case "pgdown":
			c.Viewport.HalfPageDown()
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

	// Adjust height
	c.Style = c.Style.Height(c.Height)
	c.Viewport.Height = c.Height

	return c.Style.Render(content)
}
