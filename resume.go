package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	content.WriteString(instructionStyle.Render("↑/↓: Navigate • 1-9: Quick select • Enter: Load • Esc/Q: Cancel"))
	content.WriteString("\n\n")

	start := m.scrollOffset
	end := m.scrollOffset + m.maxVisible
	if end > len(m.sessions) {
		end = len(m.sessions)
	}

	for i := start; i < end; i++ {
		session := m.sessions[i]
		isSelected := i == m.selected

		prefix := fmt.Sprintf(" %d. ", i+1)
		if isSelected {
			prefix = fmt.Sprintf("▶%d. ", i+1)
		}

		timeStr := formatRelativeTime(session.LastUpdated)
		
		var line strings.Builder
		line.WriteString(prefix)
		line.WriteString(fmt.Sprintf("[%s] %s", timeStr, session.FirstPrompt))

		var details strings.Builder
		details.WriteString(fmt.Sprintf("    %d messages • %s", len(session.Messages), session.Model))
		
		currentDir, _ := os.Getwd()
		if session.WorkingDir != "" && session.WorkingDir != currentDir {
			shortPath := session.WorkingDir
			homeDir, _ := os.UserHomeDir()
			if homeDir != "" {
				shortPath = strings.Replace(shortPath, homeDir, "~", 1)
			}
			details.WriteString(fmt.Sprintf(" • %s", shortPath))
		}

		lineStyle := lipgloss.NewStyle()
		detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		
		if isSelected {
			lineStyle = lineStyle.Foreground(lipgloss.Color("62")).Bold(true)
			detailStyle = detailStyle.Foreground(lipgloss.Color("62"))
		}

		content.WriteString(lineStyle.Render(line.String()))
		content.WriteString("\n")
		content.WriteString(detailStyle.Render(details.String()))
		content.WriteString("\n")
		
		if i < end-1 {
			content.WriteString("\n")
		}
	}

	if len(m.sessions) > m.maxVisible {
		scrollInfo := fmt.Sprintf("\n%d-%d of %d sessions", start+1, end, len(m.sessions))
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
			if m.selected < len(m.sessions)-1 {
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
