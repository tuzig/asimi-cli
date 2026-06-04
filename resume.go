package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maximhq/bifrost/core/schemas"
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
	msgs := session.GetMessages()
	if len(msgs) > 0 {
		snippet = lastHumanMessage(msgs)
	}

	snippet = cleanSnippet(snippet)
	if snippet == "" {
		return "Recent activity"
	}
	return truncateSnippet(snippet, 60)
}

func lastHumanMessage(messages []schemas.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != schemas.ChatMessageRoleUser {
			continue
		}
		if messages[i].Content != nil && messages[i].Content.ContentStr != nil {
			text := strings.TrimSpace(*messages[i].Content.ContentStr)
			if text != "" {
				return text
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
		cfg, err := config.LoadProjectConfig(repo.GetRepoInfo().ProjectRoot, true)
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to load config: %w", err)}
		}

		// Initialize storage
		db, err := storage.InitDB(cfg.Storage.DatabasePath)
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to initialize storage: %w", err)}
		}
		defer db.Close()

		maxSessions := 50
		maxAgeDays := 30
		if cfg.Session.MaxSessions > 0 {
			maxSessions = cfg.Session.MaxSessions
		}
		if cfg.Session.MaxAgeDays > 0 {
			maxAgeDays = cfg.Session.MaxAgeDays
		}

		repoInfo := repo.GetRepoInfo()
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
// It rebuilds the chat UI from messages, switches to the correct tab, and
// re-hydrates the minister session for full conversation continuity.
func (m *TUIModel) handleSessionSelected(session *shogunate.Session) {
	if session == nil {
		return
	}

	// Clear current edict ID (resumed sessions are edict-free)
	m.currentEdictKey = storage.EdictKey{}

	// Clear and rebuild chat UI from messages (reuses existing markdown renderer)
	m.tabs.Content().Chat.Clear()

	// Build a map of tool call IDs to their responses for matching
	allMessages := session.GetMessages()
	toolResults := make(map[string]string)
	for _, msgContent := range allMessages {
		if msgContent.Role == schemas.ChatMessageRoleTool {
			if msgContent.ChatToolMessage != nil && msgContent.ChatToolMessage.ToolCallID != nil {
				content := ""
				if msgContent.Content != nil && msgContent.Content.ContentStr != nil {
					content = *msgContent.Content.ContentStr
				}
				toolResults[*msgContent.ChatToolMessage.ToolCallID] = content
			}
		}
	}

	for _, msgContent := range allMessages {
		// Skip system messages
		if msgContent.Role == schemas.ChatMessageRoleSystem {
			continue
		}

		switch msgContent.Role {
		case schemas.ChatMessageRoleUser:
			if msgContent.Content != nil && msgContent.Content.ContentStr != nil {
				m.tabs.Content().Chat.AddUserMessage(*msgContent.Content.ContentStr)
			}

		case schemas.ChatMessageRoleAssistant:
			// First check for thinking/reasoning content
			if msgContent.ChatAssistantMessage != nil && msgContent.ChatAssistantMessage.Reasoning != nil {
				text := strings.TrimSpace(*msgContent.ChatAssistantMessage.Reasoning)
				if text != "" {
					m.tabs.Content().Chat.AddThinkingChunk(text)
				}
			}

			// Then collect all text content and add as a single message
			var textContent strings.Builder
			if msgContent.Content != nil && msgContent.Content.ContentStr != nil {
				text := strings.TrimSpace(*msgContent.Content.ContentStr)
				if text != "" {
					textContent.WriteString(text)
				}
			}
			// Add as a single AI message if there's any non-empty text content
			if textContent.Len() > 0 {
				m.tabs.Content().Chat.AddAIChunk(textContent.String())
				m.tabs.Content().Chat.FinalizeLastAIMessage()
			}
			// Then add tool calls with their results
			if msgContent.ChatAssistantMessage != nil {
				for _, tc := range msgContent.ChatAssistantMessage.ToolCalls {
					// Find the corresponding result
					var result string
					var toolErr error
					tcID := ""
					if tc.ID != nil {
						tcID = *tc.ID
					}
					if resp, exists := toolResults[tcID]; exists {
						if strings.HasPrefix(resp, "Error:") || strings.HasPrefix(resp, "error:") {
							toolErr = fmt.Errorf("%s", resp)
						} else {
							result = resp
						}
					}
					// Format the tool call with its result
					args := tc.Function.Arguments
					name := ""
					if tc.Function.Name != nil {
						name = *tc.Function.Name
					}
					formatted := formatToolCallByName(name, checkPrefix, args, result, toolErr)
					m.tabs.Content().Chat.AddMessage(formatted)
				}
			}

		// Skip tool messages as they're already incorporated into tool call display
		case schemas.ChatMessageRoleTool:
			continue
		}
	}
	m.sessionActive = true

	// Flush any debounced content from the rebuild loop above
	m.tabs.Content().Chat.FlushDirty()

	// Re-hydrate the minister session so follow-up prompts continue the
	// conversation. TabType holds the minister id (chancellor/sage/forge/judge);
	// legacy rows with no TabType predate per-minister persistence and are
	// treated as chancellor sessions.
	if m.shogunate != nil {
		tabType := session.TabType
		if tabType == "" {
			tabType = "chancellor"
		}
		if err := m.shogunate.RestoreMinisterSession(tabType, session.GetMessages()); err != nil {
			slog.Warn("failed to restore minister session", "tab_type", tabType, "error", err)
		}
	}

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

	timeStr := formatRelativeTime(session.LastUpdated)
	m.commandLine.AddToast(fmt.Sprintf("Resumed session from %s", timeStr), "success", 3000)
}
