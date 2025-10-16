package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// StatusComponent represents the status bar component
type StatusComponent struct {
	Agent      string
	WorkingDir string
	GitBranch  string
	Width      int
	Style      lipgloss.Style
}

// NewStatusComponent creates a new status component
func NewStatusComponent(width int) StatusComponent {
	return StatusComponent{
		Width: width,
		Style: lipgloss.NewStyle().
			Background(lipgloss.Color("#271D30")). // Terminal7 prompt background
			Foreground(lipgloss.Color("#01FAFA")). // Terminal7 text color
			Padding(0, 1).
			Width(width),
	}
}

// SetAgent sets the current agent
func (s *StatusComponent) SetAgent(agent string) {
	s.Agent = agent
}

// SetWorkingDir sets the current working directory
func (s *StatusComponent) SetWorkingDir(dir string) {
	s.WorkingDir = dir
}

// SetGitBranch sets the current git branch
func (s *StatusComponent) SetGitBranch(branch string) {
	s.GitBranch = branch
}

// SetWidth updates the width of the status component
func (s *StatusComponent) SetWidth(width int) {
	s.Width = width
	s.Style = s.Style.Width(width)
}

// View renders the status component
func (s StatusComponent) View() string {
	statusText := fmt.Sprintf("Agent: %s | Dir: %s | Branch: %s",
		s.Agent, s.WorkingDir, s.GitBranch)

	// Truncate or pad the status text to fit the width
	if len(statusText) > s.Width {
		// Truncate with ellipsis
		statusText = statusText[:s.Width-3] + "..."
	}

	return s.Style.Render(statusText)
}
