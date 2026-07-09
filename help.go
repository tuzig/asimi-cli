package main

import (
	"embed"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

//go:embed help/*.md
var helpFS embed.FS

// HelpWindow is a simplified component for displaying help documentation
// Navigation is handled by ContentComponent
type HelpWindow struct {
	width  int
	height int
	topic  string
}

// NewHelpWindow creates a new help window
func NewHelpWindow() HelpWindow {
	return HelpWindow{
		width:  80,
		height: 20,
		topic:  "index",
	}
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
	return h.renderHelpContent(h.topic)
}

// renderHelpContent generates the help content for a given topic
func (h *HelpWindow) renderHelpContent(topic string) string {
	// Style definitions
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(globalTheme.ChatBorder).
		MarginTop(1).
		MarginBottom(1)

	subheaderStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(globalTheme.TextColor).
		MarginTop(1)

	codeStyle := lipgloss.NewStyle().
		Foreground(globalTheme.PromptBorder).
		Background(globalTheme.CodeBackground).
		Padding(0, 1)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(globalTheme.PromptBorder)

	// Get help content based on topic
	content := h.getHelpTopic(topic)

	// Apply styling to the content
	lines := strings.Split(content, "\n")
	var styledLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Headers (lines starting with #)
		if strings.HasPrefix(trimmed, "# ") {
			styledLines = append(styledLines, headerStyle.Render(strings.TrimPrefix(trimmed, "# ")))
		} else if strings.HasPrefix(trimmed, "## ") {
			styledLines = append(styledLines, subheaderStyle.Render(strings.TrimPrefix(trimmed, "## ")))
		} else if strings.HasPrefix(trimmed, "```") {
			// Code blocks - skip the markers
			continue
		} else if strings.HasPrefix(trimmed, "  ") && strings.Contains(trimmed, "-") {
			// Key bindings (indented lines with dashes)
			parts := strings.SplitN(trimmed, "-", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				desc := strings.TrimSpace(parts[1])
				styledLines = append(styledLines, "  "+keyStyle.Render(key)+" - "+desc)
			} else {
				styledLines = append(styledLines, line)
			}
		} else if strings.HasPrefix(trimmed, ":") || strings.HasPrefix(trimmed, "/") {
			// Commands
			styledLines = append(styledLines, "  "+codeStyle.Render(trimmed))
		} else {
			styledLines = append(styledLines, line)
		}
	}

	return strings.Join(styledLines, "\n")
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

Press 'q' or ESC to close this help window.
`, topic)
	}

	return string(data)
}
