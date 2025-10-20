package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tmc/langchaingo/llms"
)

type sessionsLoadedMsg struct {
	sessions []Session
}

type sessionSelectedMsg struct {
	session *Session
}

type sessionResumeErrorMsg struct {
	err error
}

type SessionSelectionModal struct {
	*BaseModal
	sessions     []Session
	selected     int
	scrollOffset int
	maxVisible   int
	loading      bool
	err          error
}

func NewSessionSelectionModal() *SessionSelectionModal {
	baseModal := NewBaseModal("Resume Session", "", 70, 20)

	return &SessionSelectionModal{
		BaseModal:    baseModal,
		sessions:     []Session{},
		selected:     0,
		scrollOffset: 0,
		maxVisible:   10,
		loading:      true,
		err:          nil,
	}
}

func (m *SessionSelectionModal) SetSessions(sessions []Session) {
	m.sessions = sessions
	m.loading = false
	m.err = nil
}

func (m *SessionSelectionModal) SetError(err error) {
	m.err = err
	m.loading = false
}

func sessionTitlePreview(session Session) string {
	snippet := lastHumanMessage(session.Messages)
	if snippet == "" {
		snippet = session.FirstPrompt
	}
	snippet = cleanSnippet(snippet)
	if snippet == "" {
		return "Recent activity"
	}
	return truncateSnippet(snippet, 60)
}

func lastHumanMessage(messages []llms.MessageContent) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != llms.ChatMessageTypeHuman {
			continue
		}
		for _, part := range messages[i].Parts {
			if textPart, ok := part.(llms.TextContent); ok {
				text := strings.TrimSpace(textPart.Text)
				if text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func cleanSnippet(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "---") && strings.HasSuffix(trimmed, "---") {
			continue
		}
		if strings.HasPrefix(trimmed, "Context from:") {
			continue
		}
		return trimmed
	}

	return strings.TrimSpace(lines[0])
}

func truncateSnippet(text string, limit int) string {
	if limit <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	if limit <= 3 {
		return string(runes[:limit])
	}

	return string(runes[:limit-3]) + "..."
}

func formatMessageCount(messages []llms.MessageContent) string {
	count := 0
	for _, msg := range messages {
		if msg.Role == llms.ChatMessageTypeHuman || msg.Role == llms.ChatMessageTypeAI {
			count++
		}
	}

	if count == 0 {
		return ""
	}
	if count == 1 {
		return "1 msg"
	}
	return fmt.Sprintf("%d msgs", count)
}

func shortenModelName(model string) string {
	if model == "" {
		return ""
	}

	parts := strings.Split(model, "-")
	if len(parts) < 2 {
		return model
	}

	last := parts[len(parts)-1]
	isDateSuffix := len(last) == 8
	if isDateSuffix {
		for _, r := range last {
			if r < '0' || r > '9' {
				isDateSuffix = false
				break
			}
		}
	}

	if isDateSuffix {
		return strings.Join(parts[:len(parts)-1], "-")
	}

	return model
}

func (m *SessionSelectionModal) Render() string {
	var content strings.Builder

	if m.loading {
		content.WriteString("Loading sessions...\n")
		m.BaseModal.Content = content.String()
		return m.BaseModal.Render()
	}

	if m.err != nil {
		content.WriteString(fmt.Sprintf("Error loading sessions: %v\n\n", m.err))
		content.WriteString("Press Esc to close")
		m.BaseModal.Content = content.String()
		return m.BaseModal.Render()
	}

	if len(m.sessions) == 0 {
		content.WriteString("No previous sessions found.\n")
		content.WriteString("Start chatting to create a new session!\n\n")
		content.WriteString("Press Esc to close")
		m.BaseModal.Content = content.String()
		return m.BaseModal.Render()
	}

	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
	content.WriteString(instructionStyle.Render("↑/↓: Navigate • 1-9: Quick select • Enter: Select • Esc/Q: Cancel"))
	content.WriteString("\n\n")

	// Total items = sessions + cancel option
	totalItems := len(m.sessions) + 1

	start := m.scrollOffset
	end := m.scrollOffset + m.maxVisible
	if end > totalItems {
		end = totalItems
	}

	for i := start; i < end; i++ {
		isSelected := i == m.selected

		// Check if this is the cancel option (last item)
		if i == len(m.sessions) {
			prefix := "   "
			if isSelected {
				prefix = "▶ "
			}

			var line strings.Builder
			line.WriteString(prefix)
			line.WriteString("Cancel")

			lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
			if isSelected {
				lineStyle = lineStyle.Foreground(lipgloss.Color("62")).Bold(true)
			}

			content.WriteString("\n")
			content.WriteString(lineStyle.Render(line.String()))
			continue
		}

		session := m.sessions[i]

		prefix := fmt.Sprintf(" %d. ", i+1)
		if isSelected {
			prefix = fmt.Sprintf("▶%d. ", i+1)
		}

		timeStr := formatRelativeTime(session.LastUpdated)

		title := sessionTitlePreview(session)
		messageCount := formatMessageCount(session.Messages)
		modelName := shortenModelName(session.Model)

		var detailParts []string
		if messageCount != "" {
			detailParts = append(detailParts, messageCount)
		}
		if modelName != "" {
			detailParts = append(detailParts, modelName)
		}

		currentDir, _ := os.Getwd()
		if session.WorkingDir != "" && session.WorkingDir != currentDir {
			shortPath := session.WorkingDir
			homeDir, _ := os.UserHomeDir()
			if homeDir != "" {
				shortPath = strings.Replace(shortPath, homeDir, "~", 1)
			}
			detailParts = append(detailParts, shortPath)
		}

		var line strings.Builder
		line.WriteString(prefix)
		line.WriteString(fmt.Sprintf("[%s] %s", timeStr, title))

		detailLine := strings.Join(detailParts, " • ")

		lineStyle := lipgloss.NewStyle()
		detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		if isSelected {
			lineStyle = lineStyle.Foreground(lipgloss.Color("62")).Bold(true)
			detailStyle = detailStyle.Foreground(lipgloss.Color("62"))
		}

		content.WriteString(lineStyle.Render(line.String()))
		if detailLine != "" {
			content.WriteString("\n")
			content.WriteString(detailStyle.Render("    " + detailLine))
		}
		content.WriteString("\n")

		if i < end-1 {
			content.WriteString("\n")
		}
	}

	if totalItems > m.maxVisible {
		scrollInfo := fmt.Sprintf("\n%d-%d of %d items", start+1, end, totalItems)
		scrollStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
		content.WriteString(scrollStyle.Render(scrollInfo))
	}

	m.BaseModal.Content = content.String()
	return m.BaseModal.Render()
}

func (m *SessionSelectionModal) Update(msg tea.Msg) (*SessionSelectionModal, tea.Cmd) {
	if m.loading || m.err != nil || len(m.sessions) == 0 {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if keyMsg.String() == "esc" || keyMsg.String() == "q" {
				return m, func() tea.Msg { return modalCancelledMsg{} }
			}
		}
		return m, nil
	}

	// Total items = sessions + cancel option
	totalItems := len(m.sessions) + 1

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				if m.selected < m.scrollOffset {
					m.scrollOffset = m.selected
				}
			}
		case "down", "j":
			if m.selected < totalItems-1 {
				m.selected++
				if m.selected >= m.scrollOffset+m.maxVisible {
					m.scrollOffset = m.selected - m.maxVisible + 1
				}
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			num := int(msg.String()[0] - '1')
			if num < len(m.sessions) {
				m.selected = num
				return m, m.loadSelectedSession()
			}
		case "enter":
			// If cancel option is selected (last item)
			if m.selected == len(m.sessions) {
				return m, func() tea.Msg { return modalCancelledMsg{} }
			}
			return m, m.loadSelectedSession()
		case "esc", "q":
			return m, func() tea.Msg { return modalCancelledMsg{} }
		}
	}

	return m, nil
}

func (m *SessionSelectionModal) loadSelectedSession() tea.Cmd {
	sessionID := m.sessions[m.selected].ID

	return func() tea.Msg {
		config, err := LoadConfig()
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to load config: %w", err)}
		}

		maxSessions := 50
		maxAgeDays := 30
		if config.Session.MaxSessions > 0 {
			maxSessions = config.Session.MaxSessions
		}
		if config.Session.MaxAgeDays > 0 {
			maxAgeDays = config.Session.MaxAgeDays
		}

		store, err := NewSessionStore(maxSessions, maxAgeDays)
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to create session store: %w", err)}
		}

		session, err := store.LoadSession(sessionID)
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to load session: %w", err)}
		}

		return sessionSelectedMsg{session: session}
	}
}
