package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusComponent represents the status bar component
type StatusComponent struct {
	Provider  string
	Model     string
	Connected bool
	Width     int
	Style     lipgloss.Style
	Session   *Session // Reference to session for token/time tracking
	// Vi mode status
	ViModeEnabled bool
	ViCurrentMode string
	ViPendingOp   string

	// Waiting indicator
	waitingForResponse bool
	waitingSince       time.Time
}

// NewStatusComponent creates a new status component
func NewStatusComponent(width int) StatusComponent {
	return StatusComponent{
		Width: width,
		Style: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#01FAFA")). // Terminal7 text color
			Padding(0),
	}
}

// SetProvider sets the current provider and model
func (s *StatusComponent) SetProvider(provider, model string, connected bool) {
	s.Provider = provider
	s.Model = model
	s.Connected = connected
}

// SetSession sets the session reference for tracking
func (s *StatusComponent) SetSession(session *Session) {
	s.Session = session
}

// SetViMode updates vi mode status for display
func (s *StatusComponent) SetViMode(enabled bool, mode, pending string) {
	s.ViModeEnabled = enabled
	s.ViCurrentMode = mode
	s.ViPendingOp = pending
}

// StartWaiting marks the status component as waiting for a model response
func (s *StatusComponent) StartWaiting() {
	s.waitingForResponse = true
	s.waitingSince = time.Now()
}

// StopWaiting clears the waiting indicator
func (s *StatusComponent) StopWaiting() {
	s.waitingForResponse = false
}

// SetAgent sets the current agent (legacy method for compatibility)
func (s *StatusComponent) SetAgent(agent string) {
	// Parse the agent string to extract provider and model info
	if strings.Contains(agent, "✅") {
		s.Connected = true
	} else {
		s.Connected = false
	}

	// Extract provider and model from agent string
	// Format is usually "✅ provider (model)" or "🔌 provider (model)"
	parts := strings.Split(agent, " ")
	if len(parts) >= 2 {
		s.Provider = parts[1]
		if len(parts) >= 3 && strings.HasPrefix(parts[2], "(") && strings.HasSuffix(parts[len(parts)-1], ")") {
			// Join all parts between parentheses
			modelParts := strings.Join(parts[2:], " ")
			s.Model = strings.Trim(modelParts, "()")
		}
	}
}

// SetWorkingDir sets the current working directory (legacy method)
func (s *StatusComponent) SetWorkingDir(dir string) {
	// This is now handled internally by getting current directory
}

// SetWidth updates the width of the status component
func (s *StatusComponent) SetWidth(width int) {
	s.Width = width
}

// View renders the status component
func (s StatusComponent) View() string {
	// Left section: 🪾<branch_name>
	leftSection := s.renderLeftSection()

	// Middle section: <git status>
	middleSection := s.renderMiddleSection()

	// Right section: <provider status icon><provider-model>
	rightSection := s.renderRightSection()

	// Calculate available space
	leftWidth := lipgloss.Width(leftSection)
	rightWidth := lipgloss.Width(rightSection)
	middleWidth := lipgloss.Width(middleSection)

	// Calculate spacing
	// The style has Width() set, so lipgloss will handle padding internally
	// We need to account for the horizontal padding (1 left + 1 right = 2 chars)
	totalContentWidth := leftWidth + middleWidth + rightWidth
	availableSpace := s.Width - 2 // Account for horizontal padding

	if totalContentWidth > availableSpace {
		// Truncate if content is too long
		if leftWidth+rightWidth > availableSpace {
			// Truncate right section first
			maxRightWidth := availableSpace - leftWidth - 3 // Leave space for "..."
			if maxRightWidth > 0 {
				rightSection = s.truncateString(rightSection, maxRightWidth)
			} else {
				rightSection = ""
			}
		}
		middleSection = "" // Remove middle section if still too long
	}

	// Recalculate after potential truncation
	leftWidth = lipgloss.Width(leftSection)
	rightWidth = lipgloss.Width(rightSection)
	middleWidth = lipgloss.Width(middleSection)

	// Create the final status line
	var statusLine string
	if middleSection != "" {
		// Calculate spacing to center middle section
		totalContentWidth = leftWidth + middleWidth + rightWidth
		if totalContentWidth < availableSpace {
			leftSpacing := (availableSpace - totalContentWidth) / 2
			rightSpacing := availableSpace - totalContentWidth - leftSpacing
			statusLine = leftSection + strings.Repeat(" ", leftSpacing) + middleSection + strings.Repeat(" ", rightSpacing) + rightSection
		} else {
			statusLine = leftSection + " " + middleSection + " " + rightSection
		}
	} else {
		// Just left and right sections
		spacing := availableSpace - leftWidth - rightWidth
		if spacing < 0 {
			spacing = 0
		}
		statusLine = leftSection + strings.Repeat(" ", spacing) + rightSection
	}

	return s.Style.Render(statusLine)
}

// renderLeftSection renders the left section with branch info
func (s StatusComponent) renderLeftSection() string {
	branch := getCurrentGitBranch()
	if branch == "" {
		return "🪾no-git"
	}

	// Color branch name: yellow for main, green for others
	var bs lipgloss.Style
	if branch == "main" || branch == "master" {
		bs = lipgloss.NewStyle().Foreground(lipgloss.Color("#F4DB53")) // Terminal7 warning/yellow
	} else {
		bs = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")) // Green
	}

	var parts []string
	parts = append(parts, "🌴 "+bs.Render(branch))
	if gitStatus := getGitStatus(); gitStatus != "" {
		parts = append(parts, gitStatus)
	}
	if viIndicator := s.renderViModeIndicator(); viIndicator != "" {
		parts = append(parts, viIndicator)
	}
	return strings.Join(parts, " ")
}

// renderMiddleSection renders the middle section with token usage andsession age
func (s StatusComponent) renderMiddleSection() string {
	// Return token usage and session age e.g, `🪣 63%   1h23:45 ⏱`
	if s.Session == nil {
		return ""
	}

	// Get context usage percentage
	usagePercent := s.Session.GetContextUsagePercent()

	// Get session duration
	duration := s.Session.GetSessionDuration()

	// Format duration as h:mm:ss or mm:ss
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	var durationStr string
	if hours > 0 {
		durationStr = fmt.Sprintf("%dh%02d:%02d", hours, minutes, seconds)
	} else {
		durationStr = fmt.Sprintf("%02d:%02d", minutes, seconds)
	}

	// Format the output with icons
	statusStr := fmt.Sprintf("🪣 %.0f%%   %s ⏱", usagePercent, durationStr)
	if s.waitingForResponse && !s.waitingSince.IsZero() {
		waitSeconds := int(time.Since(s.waitingSince).Seconds())
		if waitSeconds >= 3 {
			statusStr += fmt.Sprintf("  ⏳ %ds", waitSeconds)
		}
	}

	// Style with Terminal7 text color
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#01FAFA"))
	return statusStyle.Render(statusStr)
}

// renderRightSection renders the right section with provider info
func (s StatusComponent) renderRightSection() string {
	icon := getProviderStatusIcon(s.Connected)
	providerModel := shortenProviderModel(s.Provider, s.Model)

	// Style provider info
	providerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#01FAFA")) // Terminal7 text color

	return providerStyle.Render(providerModel) + " " + icon
}

// renderViModeIndicator renders the vi mode indicator string
func (s StatusComponent) renderViModeIndicator() string {
	if !s.ViModeEnabled {
		return ""
	}

	var text string
	var style lipgloss.Style

	switch s.ViCurrentMode {
	case ViModeInsert:
		text = "<INSERT>"
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true)
	case ViModeNormal:
		text = "<NORMAL>"
		if s.ViPendingOp != "" {
			text += " (" + s.ViPendingOp + ")"
		}
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#F4DB53")).Bold(true)
	case ViModeVisual:
		text = "<VISUAL>"
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#01FAFA")).Bold(true)
	case ViModeCommandLine:
		text = "<COMMAND>"
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#F952F9")).Bold(true)
	default:
		text = "<VI>"
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#01FAFA")).Bold(true)
	}

	return style.Render(text)
}

// truncateString truncates a string to fit within maxWidth, adding "..." if needed
func (s StatusComponent) truncateString(str string, maxWidth int) string {
	if lipgloss.Width(str) <= maxWidth {
		return str
	}

	if maxWidth <= 3 {
		return "..."
	}

	// Binary search to find the right length
	left, right := 0, len(str)
	for left < right {
		mid := (left + right + 1) / 2
		candidate := str[:mid] + "..."
		if lipgloss.Width(candidate) <= maxWidth {
			left = mid
		} else {
			right = mid - 1
		}
	}

	if left == 0 {
		return "..."
	}

	return str[:left] + "..."
}
