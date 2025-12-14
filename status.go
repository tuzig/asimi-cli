package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusComponent represents the status bar component
type StatusComponent struct {
	Provider    string
	Model       string
	Connected   bool
	HasError    bool // Track if there's a model error
	Width       int
	Style       lipgloss.Style
	Session     *Session  // Reference to session for token/time tracking
	repoInfo    *RepoInfo // Git repository information
	mode        string
	ViPendingOp string

	// Waiting indicator
	waitingForResponse bool
	waitingSince       time.Time

	// Shell runner info
	shellRunnerInfo *ShellRunnerInfo
}

// NewStatusComponent creates a new status component
func NewStatusComponent(width int) StatusComponent {
	return StatusComponent{
		Width: width,
		Style: lipgloss.NewStyle().
			Foreground(globalTheme.TextColor),
		mode: "INSERT", // start in insert mode
	}
}

// SetShellRunnerInfo sets the shell runner information for display
func (s *StatusComponent) SetShellRunnerInfo(info *ShellRunnerInfo) {
	s.shellRunnerInfo = info
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

// SetRepoInfo sets the repository information
func (s *StatusComponent) SetRepoInfo(repoInfo *RepoInfo) {
	s.repoInfo = repoInfo
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

// SetError marks the status component as having an error
func (s *StatusComponent) SetError() {
	s.HasError = true
}

// ClearError clears the error state
func (s *StatusComponent) ClearError() {
	s.HasError = false
}

// getStatusIcon returns the appropriate status icon based on connection and error state
func (s StatusComponent) getStatusIcon() string {
	if s.HasError {
		return "❌"
	}
	if s.Connected {
		return "✅"
	}
	return "🔌"
}

// shortenProviderModel shortens provider and model names for display
func shortenProviderModel(provider, model string) string {
	// Shorten common provider names
	switch strings.ToLower(provider) {
	case "anthropic":
		provider = "Claude"
	case "openai":
		provider = "GPT"
	case "google", "googleai":
		provider = "Gemini"
	case "ollama":
		provider = "Ollama"
	}

	// Shorten common model names
	modelShort := model
	lowerModel := strings.ToLower(model)
	if strings.Contains(lowerModel, "claude") {
		// Handle models like "Claude-Haiku-4.5", "Claude 3.5 Sonnet", etc.
		// Extract the meaningful part after "claude"
		parts := strings.FieldsFunc(lowerModel, func(r rune) bool {
			return r == '-' || r == ' ' || r == '_'
		})

		// Skip "claude" prefix and build the short name
		if len(parts) > 1 {
			// For "claude-3-5-haiku-20240307" -> "3.5-Haiku"
			// For "claude-haiku-4.5" -> "Haiku-4.5"
			var shortParts []string
			for i := 1; i < len(parts); i++ {
				part := parts[i]
				// Skip date suffixes like "20240307"
				if len(part) == 8 && strings.ContainsAny(part, "0123456789") {
					continue
				}
				// Skip "latest" suffix
				if part == "latest" {
					continue
				}
				shortParts = append(shortParts, part)
			}

			if len(shortParts) > 0 {
				// Join parts and capitalize first letter of each word
				result := strings.Join(shortParts, "-")
				// Capitalize: "3-5-haiku" -> "3.5-Haiku"
				result = strings.ReplaceAll(result, "-5-", ".5-")
				// Capitalize model names
				result = strings.ReplaceAll(result, "haiku", "Haiku")
				result = strings.ReplaceAll(result, "sonnet", "Sonnet")
				result = strings.ReplaceAll(result, "opus", "Opus")
				modelShort = result
			}
		} else if strings.Contains(lowerModel, "instant") {
			modelShort = "Instant"
		}
	} else if strings.Contains(lowerModel, "gpt") {
		if strings.Contains(model, "4") {
			if strings.Contains(model, "turbo") {
				modelShort = "4T"
			} else {
				modelShort = "4"
			}
		} else if strings.Contains(model, "3.5") {
			modelShort = "3.5"
		}
	} else if strings.Contains(lowerModel, "gemini") {
		if strings.Contains(model, "pro") {
			modelShort = "Pro"
		} else if strings.Contains(model, "flash") {
			modelShort = "Flash"
		}
	}

	return fmt.Sprintf("%s-%s", provider, modelShort)
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
	// Left section: mode + branch info
	leftSection := s.renderLeftSection()

	// Middle section: token usage + wait time
	middleSection := s.renderMiddleSection()

	// Right section: provider/model with status color
	rightSection := s.renderRightSection()

	// Calculate visual widths using lipgloss (handles ANSI codes and unicode)
	leftWidth := lipgloss.Width(leftSection)
	middleWidth := lipgloss.Width(middleSection)
	rightWidth := lipgloss.Width(rightSection)

	// Calculate spacing
	// Layout: [left] ... [middle] ... [right]
	totalContentWidth := leftWidth + middleWidth + rightWidth
	totalSpacing := s.Width - totalContentWidth

	if totalSpacing < 2 {
		// Not enough space - just concatenate with single spaces
		return s.Style.Render(leftSection + " " + middleSection + " " + rightSection)
	}

	// Distribute spacing: try to center the middle section
	// Ideal middle position is at (s.Width / 2) - (middleWidth / 2)
	idealMiddleStart := (s.Width - middleWidth) / 2

	// Left padding (space between left section and middle)
	leftPadding := idealMiddleStart - leftWidth
	if leftPadding < 1 {
		leftPadding = 1
	}

	// Right padding (space between middle and right section)
	rightPadding := s.Width - leftWidth - leftPadding - middleWidth - rightWidth
	if rightPadding < 1 {
		rightPadding = 1
	}

	// Build the final string
	result := leftSection + strings.Repeat(" ", leftPadding) +
		middleSection + strings.Repeat(" ", rightPadding) +
		rightSection

	return s.Style.Render(result)
}

func (s *StatusComponent) SetMode(mode string) {
	s.mode = strings.ToUpper(mode)
}

// renderLeftSection renders the left section with vi mode and branch info
func (s StatusComponent) renderLeftSection() string {
	var parts []string

	// Add vi mode indicator first
	parts = append(parts, fmt.Sprintf(" %s", s.mode))

	// Get branch from RepoInfo - it always has a value (empty string if no git)
	var branch string
	if s.repoInfo != nil {
		branch = s.repoInfo.Branch
	}

	if branch == "" {
		parts = append(parts, "🪾no-git")
		return strings.Join(parts, " ")
	}

	// Color branch name: yellow for main, green for others
	var bs lipgloss.Style
	if s.repoInfo != nil && s.repoInfo.IsMain {
		bs = lipgloss.NewStyle().Foreground(globalTheme.Warning)
	} else {
		// Use a green color for non-main branches
		// TODO use globalTheme for the color
		bs = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	}

	parts = append(parts, "🌴 "+bs.Render(branch))

	// Add diff stats if available
	if s.repoInfo != nil {
		added := s.repoInfo.LinesAdded
		deleted := s.repoInfo.LinesDeleted
		if added > 0 || deleted > 0 {
			addedStyle := lipgloss.NewStyle().Foreground(globalTheme.Error)
			deletedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))

			var diffParts []string
			if added > 0 {
				diffParts = append(diffParts, addedStyle.Render(fmt.Sprintf("+%d", added)))
			}
			if deleted > 0 {
				diffParts = append(diffParts, deletedStyle.Render(fmt.Sprintf("-%d", deleted)))
			}
			parts = append(parts, "← "+strings.Join(diffParts, " "))
		}
	}

	// Add shell runner indicator
	parts = append(parts, s.renderShellRunnerIndicator())

	return strings.Join(parts, " ")
}

// renderShellRunnerIndicator renders the shell runner status indicator
func (s StatusComponent) renderShellRunnerIndicator() string {
	ret := "🏖️ "
	warnStyle := lipgloss.NewStyle().Foreground(globalTheme.Warning)

	if s.shellRunnerInfo != nil && s.shellRunnerInfo.Type == "podman" {
		// Show container indicator with container ID
		id := s.shellRunnerInfo.ContainerID
		if id == "" {
			ret += "TBD"
		} else {
			ret += id
		}
		return ret
	}

	return ret + warnStyle.Render("⚠ host")
}

// renderMiddleSection renders the middle section with token usage andsession age
func (s StatusComponent) renderMiddleSection() string {
	statusStyle := lipgloss.NewStyle().Foreground(globalTheme.TextColor)
	// Return token usage and session age e.g, `🪣 63%   1h23:45 ⏱`
	if s.Session == nil {
		return statusStyle.Render("🪣 0%")
	}

	// Get context usage percentage
	usagePercent := s.Session.GetContextUsagePercent()

	// Format the output with icons
	statusStr := fmt.Sprintf("🪣 %.0f%%", usagePercent)
	if s.waitingForResponse && !s.waitingSince.IsZero() {
		waitSeconds := int(time.Since(s.waitingSince).Seconds())
		if waitSeconds >= 3 {
			statusStr += fmt.Sprintf("  ⏳ %ds", waitSeconds)
		}
	}

	// Style with theme text color
	return statusStyle.Render(statusStr)
}

// renderRightSection renders the right section with provider info
func (s StatusComponent) renderRightSection() string {
	providerModel := shortenProviderModel(s.Provider, s.Model)

	// Color based on connection status: yellow on startup, green connected, red disconnected/error
	var color lipgloss.Color
	if s.HasError {
		color = globalTheme.Error
	} else if s.Connected {
		color = lipgloss.Color("#00FF00") // Green for connected
	} else {
		color = globalTheme.Warning // Yellow for startup/disconnected
	}

	providerStyle := lipgloss.NewStyle().Foreground(color)
	return providerStyle.Render(s.getStatusIcon() + " " + providerModel)
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
