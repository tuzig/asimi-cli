package main

import (
	"embed"
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/muesli/reflow/wordwrap"
)

//go:embed help/*.md
var helpFS embed.FS

// HelpWindow is a simplified component for displaying help documentation
// Navigation is handled by ContentComponent
type HelpWindow struct {
	width    int
	height   int
	topic    string
	renderer *glamour.TermRenderer
}

// NewHelpWindow creates a new help window
func NewHelpWindow() HelpWindow {
	hw := HelpWindow{
		width:  80,
		height: 20,
		topic:  "index",
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0), // 0 disables glamour's word wrapping
	)
	if err == nil {
		hw.renderer = r
	}
	return hw
}

// SetSize updates the dimensions of the help window
func (h *HelpWindow) SetSize(width, height int) {
	h.width = width
	h.height = height
}

// SetTopic sets the help topic to display
func (h *HelpWindow) SetTopic(topic string) {
	if topic == "" {
		topic = "index"
	}
	h.topic = topic
}

// GetTopic returns the current topic
func (h *HelpWindow) GetTopic() string {
	return h.topic
}

// RenderContent generates the styled help content for the current topic
func (h *HelpWindow) RenderContent() string {
	content := h.getHelpTopic(h.topic)

	if h.renderer == nil {
		return h.wrap(content)
	}

	rendered, err := h.renderer.Render(content)
	if err != nil {
		return h.wrap(content)
	}

	return strings.TrimSpace(h.wrap(rendered))
}

// wrap applies word wrapping to the given content using the window width
func (h *HelpWindow) wrap(content string) string {
	width := h.width - 2 // -2 for padding
	if width < 1 {
		width = 1
	}
	return wordwrap.String(content, width)
}

// getHelpTopic returns the help content for a specific topic
func (h *HelpWindow) getHelpTopic(topic string) string {
	if topic == "" {
		topic = "index"
	}
	topic = strings.ToLower(topic)

	data, err := helpFS.ReadFile("help/" + topic + ".md")
	if err != nil {
		return fmt.Sprintf(`# Help Topic Not Found

The help topic '%s' was not found.

## Available Topics

Type :help followed by one of these topics:

  :help index       - Main help index
  :help modes       - Vi modes (INSERT, NORMAL, COMMAND)
  :help commands    - Available commands
  :help navigation  - Navigation keys
  :help editing     - Editing commands
  :help files       - File operations
  :help sessions    - Session management
  :help context     - Context and token usage
  :help config      - Configuration options
  :help models      - Model selection and LLM configuration
  :help login       - Provider authentication
  :help quickref    - Quick reference guide
  :help tutorial    - Tutorial: getting started as a Ruler

Press 'q' or ESC to close this help window.
`, topic)
	}

	return string(data)
}
