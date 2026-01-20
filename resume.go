package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tmc/langchaingo/llms"
)

type sessionsLoadedMsg struct {
	sessions []shogunate.Session
}

type sessionSelectedMsg struct {
	session *shogunate.Session
}

type sessionResumeErrorMsg struct {
	err error
}

// ResumeWindow is a simplified component for displaying session selection
// Navigation is handled by ContentComponent
type ResumeWindow struct {
	SelectWindow[shogunate.Session]
	loadingSession bool
}

func NewResumeWindow() ResumeWindow {
	sw := NewSelectWindow[shogunate.Session]()
	sw.Height = 15 // Default height
	sw.SetSize(70, 15)

	return ResumeWindow{
		SelectWindow:   sw,
		loadingSession: false,
	}
}

func (r *ResumeWindow) SetSessions(sessions []shogunate.Session) {
	r.SetItems(sessions)
	r.loadingSession = false
}

func (r *ResumeWindow) SetError(err error) {
	r.SelectWindow.SetError(err)
	r.loadingSession = false
}

func (r *ResumeWindow) GetSelectedSession(index int) *shogunate.Session {
	return r.GetSelectedItem(index)
}

func sessionTitlePreview(session shogunate.Session) string {

	snippet := session.FirstPrompt
	if len(session.Messages) > 0 {
		snippet = lastHumanMessage(session.Messages)
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

// RenderList renders the session list with the given selection
// Always renders exactly visibleSlots lines to maintain consistent height
func (r *ResumeWindow) RenderList(selectedIndex, scrollOffset, visibleSlots int) string {
	// Update maxVisible to match requested visibleSlots if needed,
	// although SelectWindow usually manages this via SetSize.
	// But RenderList takes visibleSlots arg which comes from the caller (who might have calculated it differently).
	// The caller (ContentComponent) usually calls SetSize, then RenderList.
	// But let's just use what SelectWindow has or respect the arg?
	// In the original code: `lr := lineRenderer{targetLines: visibleSlots}`
	// So we should pass visibleSlots to SetSize or trust it matches?
	// Actually SelectWindow.Render uses s.MaxVisible.
	// We should probably ensure s.MaxVisible is synced or just rely on s.MaxVisible.

	// Wait, RenderList signature in ResumeWindow takes `visibleSlots`.
	// SelectWindow.Render uses s.MaxVisible.
	// If we want to support dynamic visibleSlots per render, SelectWindow.Render should maybe take it?
	// But `SetSize` updates `MaxVisible`.
	// Let's assume `SetSize` was called correctly.

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F952F9")).
		Background(lipgloss.Color("#000000")).
		Padding(0, 1)

	config := RenderConfig[shogunate.Session]{
		ConstructTitle: func(selectedIndex, totalItems int) string {
			return titleStyle.Render(fmt.Sprintf("Choose a session to resume [%3d/%3d]:", selectedIndex+1, totalItems))
		},
		OnLoading: func(sb *strings.Builder) {
			sb.WriteString("Loading sessions...\n")
			sb.WriteString("\n")
			sb.WriteString("⏳ Fetching previous sessions...\n")
		},
		CustomState: func(sb *strings.Builder) bool {
			if r.loadingSession {
				sb.WriteString("Loading selected session...\n")
				sb.WriteString("Please wait...\n")
				return true
			}
			return false
		},
		OnError: func(sb *strings.Builder, err error) {
			sb.WriteString(fmt.Sprintf("Error loading sessions: %v\n", err))
			sb.WriteString("\n")
		},
		OnEmpty: func(sb *strings.Builder) {
			sb.WriteString("No previous sessions found.\n")
			sb.WriteString("Start chatting to create a new session!\n")
			sb.WriteString("\n")
		},
		RenderItem: func(i int, session shogunate.Session, isSelected bool, sb *strings.Builder) {
			prefix := "  "
			if isSelected {
				prefix = "▶ "
			}

			timeStr := formatRelativeTime(session.LastUpdated)
			sessionTitle := sessionTitlePreview(session)

			var line strings.Builder
			line.WriteString(prefix)
			line.WriteString(fmt.Sprintf("[%s] %4d %s", timeStr, session.MessageCount, sessionTitle))

			lineStyle := lipgloss.NewStyle()
			if isSelected {
				lineStyle = lineStyle.Foreground(lipgloss.Color("62")).Bold(true)
			}

			sb.WriteString(lineStyle.Render(line.String()) + "\n")
		},
	}

	return r.Render(selectedIndex, scrollOffset, config)
}

// LoadSession loads a session by ID
func (r *ResumeWindow) LoadSession(sessionID string) tea.Cmd {
	r.loadingSession = true

	return func() tea.Msg {
		config, err := LoadConfig()
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to load config: %w", err)}
		}

		// Initialize storage
		db, err := storage.InitDB(config.Storage.DatabasePath)
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to initialize storage: %w", err)}
		}
		defer db.Close()

		maxSessions := 50
		maxAgeDays := 30
		if config.Session.MaxSessions > 0 {
			maxSessions = config.Session.MaxSessions
		}
		if config.Session.MaxAgeDays > 0 {
			maxAgeDays = config.Session.MaxAgeDays
		}

		repoInfo := GetRepoInfo()
		store, err := NewSessionStore(db, repoInfo, maxSessions, maxAgeDays)
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to create session store: %w", err)}
		}
		// No defer store.Close() needed as main.SessionStore does not have it.

		// Load the session
		mainSession, err := store.LoadSession(sessionID) // Load main.Session directly
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to load session: %w", err)}
		}

		if mainSession == nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("session %s not found", sessionID)}
		}

		return sessionSelectedMsg{session: mainSession}
	}
}

func formatRelativeTime(t time.Time) string {
	now := time.Now()

	// Normalize to midnight for calendar day comparison
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tMidnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

	daysDiff := int(todayMidnight.Sub(tMidnight).Hours() / 24)

	// Today
	if daysDiff == 0 {
		return fmt.Sprintf("Today %s", t.Format("15:04"))
	}

	// Yesterday
	if daysDiff == 1 {
		return fmt.Sprintf("Yesterday %s", t.Format("15:04"))
	}

	// This year - show month and day
	if t.Year() == now.Year() {
		return t.Format("Jan 2 15:04")
	}

	// Older - show full date
	return t.Format("Jan 2, 2006")
}

// handleSessionSelected processes a resumed session and updates the TUI model.
// It copies session data, rebuilds the chat UI from messages, and resets prompt history state.
func (m *TUIModel) handleSessionSelected(loaded *shogunate.Session) {
	if loaded == nil {
		return
	}

	if m.shogunate.Session() != nil {
		// Copy all persisted fields from loaded session to existing session
		m.shogunate.SetSessionFromResumed(loaded)
	} else {
		slog.Warn("Resumed session without active LLM - some features may be limited")
	}

	// Clear and rebuild chat UI from messages (reuses existing markdown renderer)
	m.content.Chat.Clear()

	// Build a map of tool call IDs to their responses for matching
	toolResults := make(map[string]llms.ToolCallResponse)
	for _, msgContent := range loaded.Messages {
		if msgContent.Role == llms.ChatMessageTypeTool {
			for _, part := range msgContent.Parts {
				if resp, ok := part.(llms.ToolCallResponse); ok {
					toolResults[resp.ToolCallID] = resp
				}
			}
		}
	}

	for _, msgContent := range loaded.Messages {
		// Skip system messages
		if msgContent.Role == llms.ChatMessageTypeSystem {
			continue
		}

		switch msgContent.Role {
		case llms.ChatMessageTypeHuman:
			for _, part := range msgContent.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					m.content.Chat.AddUserMessage(textPart.Text)
				}
			}

		case llms.ChatMessageTypeAI:
			// First check for thinking/reasoning content
			for _, part := range msgContent.Parts {
				if thinkingPart, ok := part.(llms.ThinkingContent); ok {
					text := strings.TrimSpace(thinkingPart.Thinking)
					if text != "" {
						m.content.Chat.AddThinkingChunk(text)
					}
				}
			}

			// Then collect all text content and add as a single message
			var textContent strings.Builder
			for _, part := range msgContent.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					text := strings.TrimSpace(textPart.Text)
					if text != "" {
						if textContent.Len() > 0 {
							textContent.WriteString("\n")
						}
						textContent.WriteString(text)
					}
				}
			}
			// Add as a single AI message if there's any non-empty text content
			if textContent.Len() > 0 {
				m.content.Chat.AddAIChunk(textContent.String())
				m.content.Chat.FinalizeLastAIMessage()
			}
			// Then add tool calls with their results
			for _, part := range msgContent.Parts {
				if tc, ok := part.(llms.ToolCall); ok && tc.FunctionCall != nil {
					// Find the corresponding result
					var result string
					var toolErr error
					if resp, exists := toolResults[tc.ID]; exists {
						if strings.HasPrefix(resp.Content, "Error:") || strings.HasPrefix(resp.Content, "error:") {
							toolErr = fmt.Errorf("%s", resp.Content)
						} else {
							result = resp.Content
						}
					}
					// Format the tool call with its result
					formatted := formatToolCall(tc.FunctionCall.Name, checkPrefix, tc.FunctionCall.Arguments, result, toolErr)
					m.content.Chat.AddMessage(formatted)
				}
			}

		// Skip tool messages as they're already incorporated into tool call display
		case llms.ChatMessageTypeTool:
			continue
		}
	}
	m.shogunate.UpdateTokenCounts(nil)
	m.sessionActive = true

	// Reset in-session prompt history state to prevent rollback issues
	// when the user enters a new prompt after resuming.
	// We keep the persistent history (loaded from disk) but clear the
	// session-specific rollback state.
	m.sessionPromptHistory = make([]promptHistoryEntry, 0)
	m.historyCursor = 0
	m.historySaved = false
	m.historyPendingPrompt = ""
	m.historyPresentSessionSnapshot = 0
	m.historyPresentChatSnapshot = 0

	timeStr := formatRelativeTime(loaded.LastUpdated)
	m.commandLine.AddToast(fmt.Sprintf("Resumed session from %s", timeStr), "success", 3000)
}
