package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TUIModel represents the bubbletea model for the TUI
type TUIModel struct {
	config        *Config
	width, height int
	theme         *Theme // Add theme here

	// UI Components
	status         StatusComponent
	prompt         PromptComponent
	content        ContentComponent
	completions    CompletionDialog
	commandLine    *CommandLineComponent
	modal          *BaseModal
	providerModal  *ProviderSelectionModal
	codeInputModal *CodeInputModal

	// UI Flags & State
	Mode                 string // Current UI mode for status display
	showCompletionDialog bool
	completionMode       string // "file" or "command"
	sessionActive        bool
	rawMode              bool // Toggle between chat and raw session view
	updateAvailable      bool // True when a newer version is available
	configCreated        bool // True when config file was created on first run

	streamingActive        bool
	streamingCancel        context.CancelFunc
	streamCompleteCallback func(*TUIModel) tea.Cmd // Optional callback to run after stream completes

	// Command registry
	commandRegistry CommandRegistry

	// Application services (passed in, not owned)
	sessionStore *SessionStore
	db           *storage.DB
	scheduler    *runners.CoreToolScheduler
	shogunate    *shogunate.Shogunate

	// Shogunate integration
	currentEdictID string // Tracks current edict for multi-turn conversations

	// Prompt history and rollback management
	// sessionPromptHistory stores prompts with snapshots for current session rollback
	sessionPromptHistory          []promptHistoryEntry
	historyCursor                 int
	historySaved                  bool
	historyPendingPrompt          string
	historyPresentSessionSnapshot int
	historyPresentChatSnapshot    int

	// Persistent history stores (survive app restarts)
	persistentPromptHistory  *PromptHistory
	persistentCommandHistory *CommandHistory

	// Waiting indicator state
	waitingForResponse bool
	waitingStart       time.Time

	// CTRL-C state machine for handling duplicate events from iOS/terminal
	// See: https://github.com/anthropics/asimi/issues/115
	ctrlCState          ctrlCState // Current state in the CTRL-C state machine
	ctrlCLastEvent      time.Time  // Time of the last CTRL-C event received
	ctrlCBurstSeq       int        // Sequence number to identify which burst a timeout belongs to
	ctrlCFirstPressTime time.Time  // When the first press was completed (for window calculation)

	// Host command approval state
	pendingHostApproval *runners.ApprovalRequestMsg
	repoInfo            *repo.RepoInfo
}

// ctrlCState represents the state machine for CTRL-C handling
// This handles the "quiet period" detection to distinguish duplicate
// CTRL-C events (from iOS/terminal) from intentional double-presses
type ctrlCState int

const (
	ctrlCIdle          ctrlCState = iota // No pending CTRL-C
	ctrlCInBurst                         // Receiving CTRL-C events, waiting for quiet period
	ctrlCWaitingSecond                   // First press completed, waiting for second press
)

type promptHistoryEntry struct {
	Prompt          string
	SessionSnapshot int
	ChatSnapshot    int
}

type waitingTickMsg struct{}

// ctrlCBurstTimeoutMsg is sent after the quiet period to check if a burst has ended
type ctrlCBurstTimeoutMsg struct {
	seq int // Sequence number to match against current burst
}

// ctrlCWindowExpiredMsg is sent when the window for the second press expires
type ctrlCWindowExpiredMsg struct{}

type shellCommandResultMsg struct {
	command  string
	output   string
	exitCode string
	err      error
}

// NewTUIModel creates a new TUI model
// NewTUIModelWithStores creates a new TUI model with provided stores (for fx injection)
func NewTUIModel(cfg *Config, repoInfo *repo.RepoInfo, promptHistory *PromptHistory, commandHistory *CommandHistory, sessionStore *SessionStore, db *storage.DB, scheduler *runners.CoreToolScheduler, shog *shogunate.Shogunate) *TUIModel {

	registry := NewCommandRegistry()
	theme := NewTheme()

	prompt := NewPromptComponent(80, 5)

	// Create status component and set repo info
	status := NewStatusComponent(80)
	status.SetRepoInfo(repoInfo)

	markdownEnabled := false
	if cfg != nil {
		markdownEnabled = cfg.UI.MarkdownEnabled
		// Set prompt expanded height from config
		if cfg.UI.PromptExpandedHeight > 0 {
			prompt.SetExpandedHeight(cfg.UI.PromptExpandedHeight)
		}
	}

	model := &TUIModel{
		config: cfg,
		// width:  80, // Default width
		// height: 24, // Default height
		theme: theme,

		repoInfo: repoInfo,
		// Initialize components
		status:         status,
		prompt:         prompt,
		content:        NewContentComponent(80, 18, markdownEnabled),
		completions:    NewCompletionDialog(),
		commandLine:    NewCommandLineComponent(),
		modal:          nil,
		providerModal:  nil,
		codeInputModal: nil,

		// UI Flags
		Mode:                 ViModeInsert, // Start in insert mode
		showCompletionDialog: false,
		completionMode:       "",
		sessionActive:        false,
		rawMode:              false,
		configCreated:        config.ConfigCreated, // Set from global flag

		// Command registry
		commandRegistry: registry,

		// Application services (injected)
		sessionStore:             sessionStore,
		db:                       db,
		scheduler:                scheduler,
		shogunate:                shog,
		waitingForResponse:       false,
		persistentPromptHistory:  promptHistory,
		persistentCommandHistory: commandHistory,
	}

	// Set the GetStatus callback for the chat component
	model.content.Chat.GetStatus = func() string { return model.Mode }

	// Set initial status info - show disconnected state initially
	model.status.SetProvider(cfg.LLM.Provider, cfg.LLM.Model, false)
	model.initHistory()

	return model
}

// initHistory resets prompt history bookkeeping to its initial state and loads persistent history
func (m *TUIModel) initHistory() {
	m.sessionPromptHistory = make([]promptHistoryEntry, 0)
	m.historyCursor = 0
	m.historySaved = false
	m.historyPendingPrompt = ""
	m.historyPresentSessionSnapshot = 0
	m.historyPresentChatSnapshot = 0

	// Load persistent prompt history from disk
	if m.persistentPromptHistory != nil {
		entries, err := m.persistentPromptHistory.Load()
		if err != nil {
			slog.Warn("failed to load prompt history", "error", err)
		} else {
			// Convert persistent entries to in-memory format
			// Note: SessionSnapshot and ChatSnapshot are set to 0 for loaded entries
			// as they're only meaningful for the current session
			for _, entry := range entries {
				m.sessionPromptHistory = append(m.sessionPromptHistory, promptHistoryEntry{
					Prompt:          entry.Content,
					SessionSnapshot: 0,
					ChatSnapshot:    0,
				})
			}
			m.historyCursor = len(m.sessionPromptHistory)
			slog.Debug("loaded prompt history", "count", len(entries))
		}
	}

	// Load persistent command history from disk
	if m.persistentCommandHistory != nil {
		entries, err := m.persistentCommandHistory.Load()
		if err != nil {
			slog.Warn("failed to load command history", "error", err)
		} else {
			m.commandLine.LoadHistory(entries)
			slog.Debug("loaded command history", "count", len(entries))
		}
	}
}

// getCurrentSession returns the current shogunate session, or nil if not available
func (m *TUIModel) getCurrentSession() *shogunate.Session {
	if m.shogunate != nil && m.currentEdictID != "" {
		return m.shogunate.GetCurrentSession(m.currentEdictID)
	}
	return nil
}

// SetSession configures the Shogunate with an LLM model from a session.
func (m *TUIModel) SetSession(session *shogunate.Session) {
	if session != nil {
		m.status.SetProvider(m.config.LLM.Provider, m.config.LLM.Model, true)
		if m.shogunate != nil {
			model := session.GetModel()
			if model != nil {
				cfg := &shogunate.SessionConfig{
					LLM: config.LLMConfig{
						MaxTurns:          m.config.LLM.MaxTurns,
						MaxThinkingTokens: m.config.LLM.MaxThinkingTokens,
						Provider:          m.config.LLM.Provider,
						Model:             m.config.LLM.Model,
					},
				}
				repoInfo := repo.RepoInfo{
					ProjectRoot: m.config.Storage.DatabasePath,
				}
				m.shogunate.ConfigureModel(model, cfg, repoInfo)
			}
		}
	} else {
		m.status.SetProvider(m.config.LLM.Provider, m.config.LLM.Model, false)
	}
}

// switchModel recreates the LLM client with current config and reconfigures the Shogunate.
func (m *TUIModel) switchModel() tea.Cmd {
	return func() tea.Msg {
		slog.Info("switching LLM model", "provider", m.config.LLM.Provider, "model", m.config.LLM.Model)
		model, err := getModelClient(m.config)
		if err != nil {
			return llmInitErrorMsg{err: err}
		}
		slog.Info("LLM model switched successfully")
		return llmInitSuccessMsg{model: model}
	}
}

func (m *TUIModel) saveSession() {
	if m.sessionStore == nil {
		return
	}

	if !m.config.Session.Enabled || !m.config.Session.AutoSave {
		return
	}

	// Get the current shogunate session
	session := m.getCurrentSession()
	if session == nil {
		return
	}

	m.sessionStore.SaveSession(session)
	slog.Debug("session auto-save queued")
}

// shutdown performs graceful shutdown of the TUI, ensuring all pending saves complete
func (m *TUIModel) shutdown() {
	// Save the current session before closing
	m.saveSession()

	if m.sessionStore != nil {
		m.sessionStore.Close()
	}
}

// Init implements bubbletea.Model
func (m TUIModel) Init() tea.Cmd {
	// Async LLM initialization - getModelClient handles credentials/keyring
	return func() tea.Msg {
		slog.Info("connecting to LLM", "provider", m.config.LLM.Provider)
		model, err := getModelClient(m.config)
		if err != nil {
			return llmInitErrorMsg{err: err}
		}
		slog.Info("LLM client connected")
		return llmInitSuccessMsg{model: model}
	}
}

// Update implements bubbletea.Model
func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	start := time.Now()

	// Log all messages in debug mode

	defer func() {
		duration := time.Since(start)
		if duration > 100*time.Millisecond {
			slog.Warn("[bubbletea] Update() SLOW", "duration", duration, "msg_type", fmt.Sprintf("%T", msg))
		}
	}()

	// Update command line to remove expired toasts
	m.commandLine.Update()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.MouseMsg:
		// Handle mouse wheel scrolling - switch to SCROLL mode when scrolling up
		if msg.Type == tea.MouseWheelUp && m.Mode != "scroll" && m.content.GetActiveView() == ViewChat {
			// Only enter scroll mode if we're not already at the top
			if !m.content.Chat.Viewport.AtTop() {
				// First, let the content handle the scroll
				var contentCmd tea.Cmd
				m.content, contentCmd = m.content.Update(msg)
				// Then enter scroll mode
				enterScrollCmd := func() tea.Msg { return ChangeModeMsg{NewMode: "scroll"} }
				return m, tea.Batch(contentCmd, enterScrollCmd)
			}
		}
		var contentCmd tea.Cmd
		m.content, contentCmd = m.content.Update(msg)
		return m, contentCmd

	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)

	default:
		return m.handleCustomMessages(msg)
	}
}

// handleKeyMsg processes keyboard input filtering out escape sequences
func (m TUIModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// TODO: This is till not good enough. Not sure how sends them and why
	// Filter out terminal escape sequences that shouldn't be processed
	// These are responses to terminal queries (background color, cursor position, etc.)
	keyStr := msg.String()

	/*
		// Ignore OSC (Operating System Command) responses like ]11;rgb:...
		// These come from terminal background color queries
		if strings.HasPrefix(keyStr, "]") || strings.Contains(keyStr, "rgb:") || strings.Contains(keyStr, ";rgb") {
			return m, nil
		}

		// Ignore CSI (Control Sequence Introducer) responses like cursor position reports [1;1R
		// Check for pattern: starts with [ and ends with R and contains ;
		if (strings.HasPrefix(keyStr, "[") || strings.HasPrefix(keyStr, "\x1b[")) &&
			strings.HasSuffix(keyStr, "R") && strings.Contains(keyStr, ";") {
			return m, nil
		}

		// Ignore any key that looks like a terminal response (contains escape sequences)
		// This catches malformed or partial escape sequences
		if len(keyStr) > 3 && (strings.Contains(keyStr, "\x1b") || strings.Contains(keyStr, "\\")) {
			// But allow normal escape key
			if keyStr != "esc" && keyStr != "escape" {
				return m, nil
			}
		}
	*/

	// Always handle Ctrl+C first
	var cmd tea.Cmd

	if keyStr == "ctrl+c" {
		return m.handleCtrlC()
	}

	// Any other key resets the CTRL-C state machine
	if m.ctrlCState != ctrlCIdle {
		m.resetCtrlCState()
	}

	// Handle Ctrl+Z for background mode
	if keyStr == "ctrl+z" {
		return m.handleCtrlZ()
	}

	// Handle command line input when in command mode or yes/no mode - MUST be before other handlers
	if m.commandLine.IsInCommandMode() || m.commandLine.IsInYesNoMode() {
		cmd, handled := m.commandLine.HandleKey(msg)
		if handled {
			// Component handled the key
			return m, cmd
		}
		// Component didn't handle it - in YesNo mode, ignore unhandled keys
		if m.commandLine.IsInYesNoMode() {
			return m, nil
		}
		return m, nil
	}

	// Handle non-chat views (help, models, resume)
	if m.content.GetActiveView() != ViewChat {
		// Allow `:` to enter command line mode even in non-chat views
		if keyStr == ":" {
			m.prompt.Blur()
			return m, m.commandLine.EnterCommandMode("")
		}
		m.content, cmd = m.content.Update(msg)
		// If view switched back to chat, restore focus to prompt
		if m.content.GetActiveView() == ViewChat {
			m.prompt.Focus()
		}
		return m, cmd
	}
	if m.codeInputModal != nil {
		m.codeInputModal, cmd = m.codeInputModal.Update(msg)
		return m, cmd
	}
	if m.providerModal != nil {
		m.providerModal, cmd = m.providerModal.Update(msg)
		return m, cmd
	}

	// Handle modal close with 'q' or 'esc'
	if m.modal != nil && (keyStr == "q" || keyStr == "esc") {
		m.modal = nil
		// If esc was pressed, continue to handleEscape to clear completion dialog
		if keyStr == "esc" {
			// Don't return early - let handleEscape() also process this
		} else {
			// For 'q', return immediately
			return m, nil
		}
	}

	// Scroll mode activation and handling
	if keyStr == "ctrl+b" && m.Mode != "scroll" {
		return m.enterScrollMode()
	}
	if m.Mode == "scroll" {
		if newModel, cmd, handled := m.handleScrollModeKey(msg); handled {
			return newModel, cmd
		}
	}

	// Handle escape key for vi mode transitions BEFORE other escape handling
	// ESC in Insert mode -> Normal mode
	if keyStr == "esc" && m.Mode == "insert" {
		// Also clear completion dialog and modal if present
		m.modal = nil
		if m.showCompletionDialog {
			m.showCompletionDialog = false
			m.completions.Hide()
			m.completionMode = ""
		}
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "normal"} }
	}
	// ESC in Visual mode -> Normal mode
	if keyStr == "esc" && m.Mode == "visual" {
		// Also clear completion dialog and modal if present
		m.modal = nil
		if m.showCompletionDialog {
			m.showCompletionDialog = false
			m.completions.Hide()
			m.completionMode = ""
		}
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "normal"} }
	}
	// ESC in Command-line mode -> Normal mode
	if keyStr == "esc" && m.Mode == "command" {
		// Hide completion dialog
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "normal"} }
	}
	// ESC in Learning mode -> Normal mode
	if keyStr == "esc" && m.Mode == "learning" {
		m.prompt.SetValue("")
		// Also clear completion dialog and modal if present
		m.modal = nil
		if m.showCompletionDialog {
			m.showCompletionDialog = false
			m.completions.Hide()
			m.completionMode = ""
		}
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "normal"} }
	}
	// ESC in Replace mode -> Normal mode
	if keyStr == "esc" && m.Mode == "replace" {
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "normal"} }
	}
	// ESC in Normal mode -> Insert mode (issue #70)
	if keyStr == "esc" && m.prompt.IsViNormalMode() {
		m.prompt.EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	}

	// Handle escape key after modals have had a chance to process it
	if keyStr == "esc" {
		return m.handleEscape()
	}

	// Handle completion dialog
	if m.showCompletionDialog {
		return m.handleCompletionDialog(msg)
	}

	// Handle vi mode key bindings when in normal or visual mode
	if m.Mode == "normal" || m.Mode == "visual" {
		return m.handleViNormalMode(msg)
	}

	// Handle replace mode - replace character under cursor with next typed character
	if m.Mode == "replace" {
		return m.handleViReplaceMode(msg)
	}

	// Handle command-line mode
	if m.Mode == "command" {
		return m.handleViCommandLineMode(msg)
	}

	// Handle regular key input (when in insert mode)
	// Arrow keys only move cursor within the prompt (no history navigation in insert mode)
	switch keyStr {
	case "ctrl+o":
		m.rawMode = !m.rawMode
		return m, nil
	case ":":
		// Only enter command mode if at the beginning of input
		if m.prompt.Value() == "" {
			return m.handleColonKey(msg)
		}
		// Otherwise, just insert the colon character
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		return m, cmd
	case "@":
		return m.handleAtKey(msg)
	default:
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		return m, cmd
	}

}

// handleToggleRawMode toggles between chat and raw session view
func (m TUIModel) handleToggleRawMode() (tea.Model, tea.Cmd) {
	m.rawMode = !m.rawMode
	return m, nil
}

func (m TUIModel) enterScrollMode() (tea.Model, tea.Cmd) {
	if m.Mode == "scroll" || m.content.GetActiveView() != ViewChat {
		return m, nil
	}
	chat := m.content.Chat
	if chat.Viewport.AtBottom() {
		chat.ScrollHalfPageUp()
	}
	if m.showCompletionDialog {
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""
	}
	return m, func() tea.Msg { return ChangeModeMsg{NewMode: "scroll"} }
}

func (m TUIModel) exitScrollModeToInsert() (tea.Model, tea.Cmd) {
	if m.Mode != "scroll" {
		return m, nil
	}
	if m.showCompletionDialog {
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""
	}
	m.prompt.Focus()
	return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
}

func (m TUIModel) handleScrollModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.Mode != "scroll" {
		return m, nil, false
	}
	chat := m.content.Chat

	switch msg.String() {
	case "ctrl+f":
		chat.ScrollPageDown()
		return m, nil, true
	case "ctrl+b":
		chat.ScrollPageUp()
		return m, nil, true
	case "ctrl+d":
		chat.ScrollHalfPageDown()
		return m, nil, true
	case "ctrl+u":
		chat.ScrollHalfPageUp()
		return m, nil, true
	case "G":
		chat.ScrollToBottom()
		return m, nil, true
	case "j", "down":
		chat.ScrollDownOneLine()
		return m, nil, true
	case "k", "up":
		chat.ScrollUpOneLine()
		return m, nil, true
	case ":":
		// Exit scroll mode before entering command mode
		// The command mode will be set by handleColonKey
		newModel, cmd := m.handleColonKey(msg)
		return newModel, cmd, true
	case "esc", "escape", "i":
		newModel, cmd := m.exitScrollModeToInsert()
		return newModel, cmd, true
	}

	return m, nil, false
}

// handleViNormalMode handles key presses when in vi normal or visual mode
func (m TUIModel) handleViNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Handle history navigation with arrow keys first
	// When prompt is empty, always try history navigation
	promptEmpty := m.prompt.Value() == ""
	switch key {
	case "up", "k":
		// Handle history navigation if prompt is empty or we're on the first line
		if promptEmpty || m.prompt.TextArea.Line() == 0 {
			if handled := m.handleHistoryNavigation(-1); handled {
				return m, nil
			}
		}
		// If not handled by history, pass to textarea for navigation
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		return m, cmd
	case "down", "j":
		// Handle history navigation if prompt is empty or we're on the last line
		lineCount := m.prompt.TextArea.LineCount()
		if promptEmpty || lineCount == 0 || m.prompt.TextArea.Line() == lineCount-1 {
			if handled := m.handleHistoryNavigation(1); handled {
				return m, nil
			}
		}
		// If not handled by history, pass to textarea for navigation
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		return m, cmd
	case "enter":
		// Only submit from actual normal mode to avoid interfering with visual selections
		if m.Mode != "normal" {
			break
		}
		if m.prompt.Value() == "" {
			return m, nil
		}
		return m.handleEnterKey()
	}

	// Handle mode switching keys
	switch key {
	case "i":
		// Enter insert mode at cursor
		m.prompt.EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "I":
		// Enter insert mode at beginning of line
		m.prompt.TextArea.CursorStart()
		m.prompt.EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "a":
		// Enter insert mode after cursor (move cursor forward first)
		// Note: In vi, 'a' appends after the current character
		m.prompt.EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "A":
		// Enter insert mode at end of line
		m.prompt.TextArea.CursorEnd()
		m.prompt.EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "o":
		// Open new line below and enter insert mode
		m.prompt.TextArea.CursorEnd()
		m.prompt.TextArea.InsertString("\n")
		m.prompt.EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "O":
		// Open new line above and enter insert mode
		m.prompt.TextArea.CursorStart()
		m.prompt.TextArea.InsertString("\n")
		m.prompt.TextArea.CursorUp()
		m.prompt.EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case ":":
		// Enter command mode in the command line (bottom of screen)
		enterCmd := m.commandLine.EnterCommandMode("")
		m.prompt.Blur()
		return m, enterCmd
	case "?":
		// Show help modal
		helpText := "\n\n"
		helpText += "  j/↓     - Next history\n"
		helpText += "  k/↑     -  Previous history\n"
		helpText += "  :       - Enter command mode\n"
		helpText += "  i       - Insert mode at cursor\n"
		helpText += "  I       - Insert mode at line start\n"
		helpText += "  a       - Insert mode after cursor\n"
		helpText += "  A       - Insert mode at line end\n"
		helpText += "  o       - Open new line below\n"
		helpText += "  O       - Open new line above\n"
		helpText += "  ?       - Show this help\n\n"
		m.modal = NewBaseModal("Shortcuts Help", helpText, 80, 30)
		return m, nil
	case "#":
		// Enter learning mode
		m.prompt.EnterViLearningMode()
		m.prompt.SetValue("#")
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "learning"} }
	case "r":
		// Enter replace mode - next character will replace char under cursor
		if m.prompt.Value() != "" {
			return m, func() tea.Msg { return ChangeModeMsg{NewMode: "replace"} }
		}
		return m, nil
	default:
		// Pass other keys to the textarea for navigation
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		return m, cmd
	}
}

// handleViReplaceMode handles the replace mode where next character replaces char under cursor
func (m TUIModel) handleViReplaceMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Only handle printable runes
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		replacement := msg.Runes[0]

		// Get current value and cursor position
		value := m.prompt.Value()
		lineInfo := m.prompt.TextArea.LineInfo()
		row := m.prompt.TextArea.Line()
		col := lineInfo.StartColumn + lineInfo.ColumnOffset

		// Convert to absolute position in the string
		lines := strings.Split(value, "\n")
		if row >= 0 && row < len(lines) {
			lineRunes := []rune(lines[row])
			if col >= 0 && col < len(lineRunes) {
				// Replace the character at cursor position
				lineRunes[col] = replacement
				lines[row] = string(lineRunes)
				newValue := strings.Join(lines, "\n")
				m.prompt.SetValue(newValue)

				// Restore cursor position (SetValue resets it)
				m.prompt.TextArea.SetCursor(0)
				currentRow := m.prompt.TextArea.Line()
				for currentRow < row {
					m.prompt.TextArea.CursorDown()
					currentRow = m.prompt.TextArea.Line()
				}
				m.prompt.TextArea.SetCursor(col)
			}
		}
	}

	// Return to normal mode after any key (including non-printable)
	return m, func() tea.Msg { return ChangeModeMsg{NewMode: "normal"} }
}

// handleViCommandLineMode handles key presses when in vi command-line mode
func (m TUIModel) handleViCommandLineMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "enter":
		// Execute the command and return to insert mode
		content := m.prompt.Value()
		if strings.HasPrefix(content, ":") {
			// Parse the command (keep the : prefix for display)
			parts := strings.Fields(content)
			if len(parts) > 0 {
				cmdName := parts[0]
				m.content.Chat.AddToRawHistory("COMMAND", content)
				cmd, exists := m.commandRegistry.GetCommand(cmdName)
				if exists {
					command := cmd.Handler(&m, parts[1:])
					m.prompt.SetValue("")
					m.prompt.EnterViInsertMode() // Return to insert mode after command
					// Hide completion dialog
					m.showCompletionDialog = false
					m.completions.Hide()
					m.completionMode = ""
					return m, command
				}
				m.commandLine.AddToast(fmt.Sprintf("Unknown command: %s", cmdName), "error", time.Second*3)
				m.prompt.SetValue("")
				m.prompt.EnterViInsertMode()
				// Hide completion dialog
				m.showCompletionDialog = false
				m.completions.Hide()
				m.completionMode = ""
				return m, nil
			}
		}
		// If no command, just return to insert mode
		m.prompt.SetValue("")
		m.prompt.EnterViInsertMode()
		// Hide completion dialog
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""
		return m, nil
	default:
		// Pass other keys to the textarea for editing the command
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		// Update completion dialog if it's shown
		if m.showCompletionDialog && m.completionMode == "command" {
			m.updateCommandCompletions()
		}
		return m, cmd
	}
}

// handleCtrlZ handles Ctrl+Z to send the application to background
func (m TUIModel) handleCtrlZ() (tea.Model, tea.Cmd) {
	// TODO: Fix Ctrl+Z message not showing. tea.Println doesn't work here.
	// Need to investigate proper way to show message before suspension.
	return m, tea.Sequence(
		tea.Println("⏸️  Asimi is now in the background. Use `fg` to restore."),
		tea.Suspend,
	)
}

// handleCtrlC implements the "quiet period" detection for CTRL-C handling.
// iOS and some terminals send multiple CTRL-C events for a single keypress.
// This state machine waits for a quiet period to determine when a "burst" of
// events represents a single intentional press.
//
// State machine:
//
//	Idle -> InBurst: First CTRL-C received, start quiet period timer
//	InBurst -> InBurst: More CTRL-C events, reset quiet period timer
//	InBurst -> WaitingSecond: Quiet period elapsed, first press completed
//	WaitingSecond -> InBurst: Second burst started
//	InBurst (from WaitingSecond) -> Quit: Second press completed
//	WaitingSecond -> Idle: Window expired without second press
func (m TUIModel) handleCtrlC() (tea.Model, tea.Cmd) {
	now := time.Now()
	quietPeriod := m.config.UI.CtrlCDebounceTime // Reuse existing config field as quiet period
	windowTime := m.config.UI.CtrlCWindowTime

	slog.Debug("Got CTRL-C",
		"state", m.ctrlCState,
		"timeSinceLast", now.Sub(m.ctrlCLastEvent),
		"burstSeq", m.ctrlCBurstSeq)

	switch m.ctrlCState {
	case ctrlCIdle:
		// Start first burst
		m.ctrlCState = ctrlCInBurst
		m.ctrlCLastEvent = now
		m.ctrlCBurstSeq++
		seq := m.ctrlCBurstSeq

		// Perform immediate actions on first CTRL-C of first burst
		m.content.Chat.AddUserMessage("CTRL-C")
		m.handleEscape()

		slog.Debug("CTRL-C: Started first burst", "seq", seq)
		return m, tea.Tick(quietPeriod, func(time.Time) tea.Msg {
			return ctrlCBurstTimeoutMsg{seq: seq}
		})

	case ctrlCInBurst:
		// Continue current burst - just update timestamp and reset timer
		m.ctrlCLastEvent = now
		m.ctrlCBurstSeq++
		seq := m.ctrlCBurstSeq

		slog.Debug("CTRL-C: Continuing burst", "seq", seq)
		return m, tea.Tick(quietPeriod, func(time.Time) tea.Msg {
			return ctrlCBurstTimeoutMsg{seq: seq}
		})

	case ctrlCWaitingSecond:
		// Check if still within window
		if now.Sub(m.ctrlCFirstPressTime) > windowTime {
			// Window expired, treat as new first press
			slog.Debug("CTRL-C: Window expired, starting new first burst")
			m.ctrlCState = ctrlCInBurst
			m.ctrlCLastEvent = now
			m.ctrlCBurstSeq++
			seq := m.ctrlCBurstSeq

			m.content.Chat.AddUserMessage("CTRL-C")
			m.handleEscape()

			return m, tea.Tick(quietPeriod, func(time.Time) tea.Msg {
				return ctrlCBurstTimeoutMsg{seq: seq}
			})
		}

		// Start second burst (within window)
		slog.Debug("CTRL-C: Starting second burst (within window)")
		m.ctrlCState = ctrlCInBurst
		m.ctrlCLastEvent = now
		m.ctrlCBurstSeq++
		seq := m.ctrlCBurstSeq

		return m, tea.Tick(quietPeriod, func(time.Time) tea.Msg {
			return ctrlCBurstTimeoutMsg{seq: seq}
		})
	}

	return m, nil
}

// handleCtrlCBurstTimeout is called when the quiet period timer fires.
// It checks if the burst has actually ended (no new events) and transitions
// the state machine accordingly.
func (m TUIModel) handleCtrlCBurstTimeout(msg ctrlCBurstTimeoutMsg) (tea.Model, tea.Cmd) {
	// Ignore stale timeouts from previous bursts
	if msg.seq != m.ctrlCBurstSeq {
		slog.Debug("CTRL-C: Ignoring stale burst timeout", "msgSeq", msg.seq, "currentSeq", m.ctrlCBurstSeq)
		return m, nil
	}

	// Only process if we're in a burst
	if m.ctrlCState != ctrlCInBurst {
		slog.Debug("CTRL-C: Burst timeout but not in burst state", "state", m.ctrlCState)
		return m, nil
	}

	now := time.Now()
	quietPeriod := m.config.UI.CtrlCDebounceTime

	// Check if burst has actually ended (quiet period passed since last event)
	if now.Sub(m.ctrlCLastEvent) < quietPeriod {
		// Burst still active (more events came in), ignore this timeout
		slog.Debug("CTRL-C: Burst still active", "timeSinceLast", now.Sub(m.ctrlCLastEvent))
		return m, nil
	}

	// Burst has ended - determine if this was first or second press
	if m.ctrlCFirstPressTime.IsZero() {
		// This was the first press completing
		m.ctrlCState = ctrlCWaitingSecond
		m.ctrlCFirstPressTime = now
		windowTime := m.config.UI.CtrlCWindowTime

		slog.Debug("CTRL-C: First press completed, waiting for second")
		m.commandLine.AddToast(
			fmt.Sprintf("Press CTRL-C again within %.1fs to exit", windowTime.Seconds()),
			"info",
			windowTime,
		)

		// Schedule window expiry
		return m, tea.Tick(windowTime, func(time.Time) tea.Msg {
			return ctrlCWindowExpiredMsg{}
		})
	}

	// This was the second press completing - quit!
	slog.Info("CTRL-C: Second press completed, quitting")
	m.shutdown()
	return m, tea.Quit
}

// handleCtrlCWindowExpired is called when the window for the second press expires.
func (m TUIModel) handleCtrlCWindowExpired() (tea.Model, tea.Cmd) {
	if m.ctrlCState == ctrlCWaitingSecond {
		slog.Debug("CTRL-C: Window expired, resetting state")
		m.resetCtrlCState()
	}
	return m, nil
}

// resetCtrlCState resets the CTRL-C state machine to idle.
func (m *TUIModel) resetCtrlCState() {
	m.ctrlCState = ctrlCIdle
	m.ctrlCLastEvent = time.Time{}
	m.ctrlCFirstPressTime = time.Time{}
	// Don't reset ctrlCBurstSeq - it's used to invalidate pending timeouts
}

// handleEscape handles the escape key and the first ctrl-c
func (m TUIModel) handleEscape() (tea.Model, tea.Cmd) {
	if m.streamingActive && m.streamingCancel != nil {
		slog.Info("escape_during_streaming", "cancelling_context", true)
		m.streamingCancel()
		m.stopStreaming()
		return m, nil
	}

	m.modal = nil
	if m.showCompletionDialog {
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""
	}
	return m, nil
}

// handleCompletionDialog handles the completion dialog interactions
func (m TUIModel) handleCompletionDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "tab":
		return m.handleCompletionSelection()
	case "down":
		m.completions.SelectNext()
		return m, nil
	case "up":
		m.completions.SelectPrev()
		return m, nil
	default:
		// Any other key press updates the completion list
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		if m.completionMode == "file" {
			files, err := getFileTree(".")
			if err == nil {
				m.updateFileCompletions(files)
			}
		} else if m.completionMode == "command" {
			m.updateCommandCompletions()
		}
		return m, cmd
	}
}

// handleCompletionSelection handles when a completion is selected
func (m TUIModel) handleCompletionSelection() (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	selected := m.completions.GetSelected()
	if selected != "" {
		if m.completionMode == "file" {
			filePath := selected
			content, err := os.ReadFile(filePath)
			if err != nil {
				m.commandLine.AddToast(fmt.Sprintf("Error reading file: %v", err), "error", time.Second*3)
			} else if session := m.getCurrentSession(); session != nil {
				session.AddContextFile(filePath, string(content))
				m.content.Chat.AddMessage(fmt.Sprintf("Loaded file: %s", filePath))
			}
			currentValue := m.prompt.Value()
			lastAt := strings.LastIndex(currentValue, "@")
			if lastAt != -1 {
				// Ensure we correctly handle the text before the @
				prefix := currentValue[:lastAt]
				// Find the end of the word being completed
				wordEnd := -1
				for i := lastAt + 1; i < len(currentValue); i++ {
					if currentValue[i] == ' ' {
						wordEnd = i
						break
					}
				}
				if wordEnd == -1 {
					wordEnd = len(currentValue)
				}
				// Replace the partial file name with the full one
				newValue := prefix + "@" + selected + " " + currentValue[wordEnd:]
				m.prompt.SetValue(strings.TrimSpace(newValue) + " ")
			} else {
				// Fallback, though we should always find an @
				m.prompt.SetValue("@" + selected + " ")
			}
		} else if m.completionMode == "command" {
			// Get command name (already has : prefix)
			cmdName := selected

			// It's a command completion
			cmd, exists := m.commandRegistry.GetCommand(cmdName)
			if exists {
				// Execute command
				cmds = append(cmds, cmd.Handler(&m, []string{}))
			}
			m.prompt.SetValue("")
		}
	}
	m.showCompletionDialog = false
	m.completions.Hide()
	m.completionMode = ""
	return m, tea.Batch(cmds...)
}

func (m *TUIModel) startWaitingForResponse() tea.Cmd {
	if m.waitingForResponse {
		return nil
	}
	now := time.Now()
	m.waitingForResponse = true
	m.waitingStart = now
	m.status.StartWaiting()
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return waitingTickMsg{} })
}

func (m *TUIModel) stopWaitingForResponse() {
	if !m.waitingForResponse {
		return
	}
	m.waitingForResponse = false
	m.status.StopWaiting()
}

func (m *TUIModel) cancelStreaming() {
	if m.streamingActive && m.streamingCancel != nil {
		m.streamingCancel()
	}
	m.streamingActive = false
	m.streamingCancel = nil
}

// submitToShogunate sends a prompt to the Chancellor and returns a command that
// listens for streaming responses and dispatches them as TUI messages.
func (m *TUIModel) submitToShogunate(ctx context.Context, prompt string, contextFiles map[string]string) tea.Cmd {
	if m.shogunate == nil {
		return func() tea.Msg {
			return shogunate.StreamErrorMsg{Err: fmt.Errorf("Shogunate not initialized")}
		}
	}

	chancellor := m.shogunate.GetMinister("chancellor")
	if chancellor == nil {
		return func() tea.Msg {
			return shogunate.StreamErrorMsg{Err: fmt.Errorf("Chancellor not found")}
		}
	}

	// Type assert to get the Chancellor with Edicts channel
	ch, ok := chancellor.(*shogunate.Chancellor)
	if !ok {
		return func() tea.Msg {
			return shogunate.StreamErrorMsg{Err: fmt.Errorf("invalid Chancellor type")}
		}
	}

	// Create stream channel for streaming typed messages
	streamChan := make(chan any, 100)

	// Send edict to Chancellor
	edict := &shogunate.Edict{
		Prompt:       prompt,
		EdictID:      m.currentEdictID, // Empty for new edict
		ContextFiles: contextFiles,
		Stream:       streamChan,
	}

	// Non-blocking send to Chancellor
	go func() {
		ch.Edicts <- edict
	}()

	m.streamingActive = true
	return nil
}

func (m *TUIModel) saveHistoryPresentState() {
	if m.historySaved {
		return
	}
	m.historyPendingPrompt = m.prompt.Value()
	if session := m.getCurrentSession(); session != nil {
		m.historyPresentSessionSnapshot = session.GetMessageSnapshot()
	} else {
		m.historyPresentSessionSnapshot = 0
	}
	m.historyPresentChatSnapshot = len(m.content.Chat.Messages)
	m.historySaved = true
}

func (m *TUIModel) applyHistoryEntry(entry promptHistoryEntry) {
	// Only set the prompt value, don't rollback session/chat yet
	// That will happen when user presses Enter
	m.prompt.SetValue(entry.Prompt)
	m.prompt.TextArea.CursorEnd()
}

func (m *TUIModel) restoreHistoryPresent() {
	// Only restore the prompt value, don't rollback session/chat yet
	// That will happen when user presses Enter
	if m.historySaved {
		m.prompt.SetValue(m.historyPendingPrompt)
		m.prompt.TextArea.CursorEnd()
		m.historySaved = false
		return
	}

	m.prompt.SetValue(m.historyPendingPrompt)
	m.prompt.TextArea.CursorEnd()
}

func (m *TUIModel) handleHistoryNavigation(direction int) bool {
	if len(m.sessionPromptHistory) == 0 {
		return false
	}

	switch {
	case direction < 0:
		// Navigate backwards in history (older prompts)
		if !m.historySaved {
			m.saveHistoryPresentState()
		}
		if m.historyCursor == len(m.sessionPromptHistory) {
			m.historyCursor = len(m.sessionPromptHistory) - 1
		} else if m.historyCursor > 0 {
			m.historyCursor--
		}
		if m.historyCursor >= 0 && m.historyCursor < len(m.sessionPromptHistory) {
			m.applyHistoryEntry(m.sessionPromptHistory[m.historyCursor])
			return true
		}
	case direction > 0:
		// Navigate forwards in history (newer prompts)
		if !m.historySaved {
			return false
		}
		if m.historyCursor < len(m.sessionPromptHistory)-1 {
			m.historyCursor++
			m.applyHistoryEntry(m.sessionPromptHistory[m.historyCursor])
			return true
		}
		// Reached the end of history, restore the present state
		m.historyCursor = len(m.sessionPromptHistory)
		m.restoreHistoryPresent()
		return true
	}

	return false
}

// handleEnterKey handles the enter key press
func (m TUIModel) handleEnterKey() (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	content := m.prompt.Value()
	if content == "" {
		return m, nil
	}

	// Handle learning mode - append to agents file
	if m.Mode == "learning" {
		// Remove the leading "#" and trim whitespace
		learningNote := strings.TrimSpace(strings.TrimPrefix(content, "#"))
		if learningNote != "" {
			// Determine agents file from config
			agentsPath := "AGENTS.md"
			if m.config != nil && m.config.Session.AgentsFile != "" {
				agentsPath = m.config.Session.AgentsFile
			}
			f, err := os.OpenFile(agentsPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			if err != nil {
				m.commandLine.AddToast(fmt.Sprintf("Failed to open %s: %v", agentsPath, err), "error", time.Second*3)
			} else {
				defer f.Close()
				_, err = f.WriteString("\n" + learningNote + "\n")
				if err != nil {
					m.commandLine.AddToast(fmt.Sprintf("Failed to write to %s: %v", agentsPath, err), "error", time.Second*3)
				} else {
					m.commandLine.AddToast(fmt.Sprintf("Added to %s", agentsPath), "success", time.Second*2)
					m.content.Chat.AddMessage(fmt.Sprintf("📝 Learning added: %s", learningNote))
					m.sessionActive = true
				}
			}
		}
		// Return to normal mode
		m.prompt.EnterViNormalMode()
		m.prompt.SetValue("")
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "normal"} }
	}

	if strings.HasPrefix(content, ":") {
		// Parse the command (keep the : prefix for display)
		parts := strings.Fields(content)
		if len(parts) > 0 {
			cmdName := parts[0]
			m.content.Chat.AddToRawHistory("COMMAND", content)
			cmd, exists := m.commandRegistry.GetCommand(cmdName)
			if exists {
				command := cmd.Handler(&m, parts[1:])
				cmds = append(cmds, command)
				m.prompt.SetValue("")
				m.prompt.EnterViInsertMode()
				cmds = append(cmds, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} })
				// Ensure prompt has focus after command
				m.prompt.Focus()
			} else {
				m.commandLine.AddToast(fmt.Sprintf("Unknown command: %s", cmdName), "error", time.Second*3)
			}
		}
	} else {
		// Clear any lingering toast notifications before handling a new prompt
		m.commandLine.ClearToasts()
		m.repoInfo.RefreshDiff()

		// Check if we're submitting a historical prompt (user navigated history)
		if m.historySaved && m.historyCursor < len(m.sessionPromptHistory) {
			// User is submitting a historical prompt - rollback to that state
			entry := m.sessionPromptHistory[m.historyCursor]
			m.cancelStreaming()
			m.stopStreaming()
			if session := m.getCurrentSession(); session != nil {
				session.RollbackTo(entry.SessionSnapshot)
			}
			m.content.Chat.TruncateTo(entry.ChatSnapshot)
			m.content.Chat.ClearToolCallMessageIndex()

			// Now continue with the normal flow from this rolled-back state
			m.historySaved = false
		}

		// Add user input to raw history
		m.content.Chat.AddToRawHistory("USER", content)
		chatSnapshot := len(m.content.Chat.Messages)
		var sessionSnapshot int
		session := m.getCurrentSession()
		if session != nil {
			sessionSnapshot = session.GetMessageSnapshot()
		}
		if m.historyCursor < len(m.sessionPromptHistory) {
			m.sessionPromptHistory = m.sessionPromptHistory[:m.historyCursor]
		}
		m.content.Chat.AddUserMessage(content)
		if m.shogunate != nil {
			// Check if we need to auto-compact before sending the prompt (#54)
			if session != nil {
				info := session.GetContextInfo()
				// Auto-compact if free tokens are less than 10% of total
				autoCompactThreshold := float64(info.TotalTokens) * 0.10
				if float64(info.FreeTokens) < autoCompactThreshold && len(session.GetMessages()) > 2 {
					slog.Info("auto-compacting conversation", "free_tokens", info.FreeTokens, "threshold", autoCompactThreshold)
					m.content.Chat.AddMessage("🗜️  Auto-compacting conversation history (low on context)...")

					// Perform compaction synchronously before sending the prompt
					ctx := context.Background()
					// not using summary as this is an automatic workflow and
					// there's no reason to notfiy the user
					_, err := session.CompactHistory(ctx, compactPrompt)
					if err != nil {
						slog.Warn("auto-compaction failed", "error", err)
						m.content.Chat.AddMessage(fmt.Sprintf("⚠️  Auto-compaction failed: %v", err))
					} else {
						// Get updated context info
						newInfo := session.GetContextInfo()
						m.content.Chat.AddMessage(fmt.Sprintf("✅ Conversation compacted! Context usage: %s/%s tokens (%.1f%%)",
							formatTokenCount(newInfo.UsedTokens),
							formatTokenCount(newInfo.TotalTokens),
							percentage(newInfo.UsedTokens, newInfo.TotalTokens)))
						slog.Info("auto-compaction completed", "old_used", info.UsedTokens, "new_used", newInfo.UsedTokens, "saved", info.UsedTokens-newInfo.UsedTokens)
					}
				}
			}

			m.sessionActive = true
			m.prompt.SetValue("")
			// In vi mode, stay in insert mode for continued conversation
			if waitCmd := m.startWaitingForResponse(); waitCmd != nil {
				cmds = append(cmds, waitCmd)
			}
			ctx, cancel := context.WithCancel(context.Background())
			m.streamingCancel = cancel

			// Get context files from session (populated via @ references)
			var contextFiles map[string]string
			if session != nil {
				contextFiles = session.GetContextFiles()
			}
			shogunateCmd := m.submitToShogunate(ctx, content, contextFiles)
			cmds = append(cmds, shogunateCmd)
		} else {
			m.commandLine.AddToast("No model configured, use :models to configure a model", "error", time.Second*5)
			m.prompt.SetValue("")
		}
		m.sessionPromptHistory = append(m.sessionPromptHistory, promptHistoryEntry{
			Prompt:          content,
			SessionSnapshot: sessionSnapshot,
			ChatSnapshot:    chatSnapshot,
		})
		m.historyCursor = len(m.sessionPromptHistory)
		m.historySaved = false
		m.historyPendingPrompt = ""

		// Save to persistent history
		if m.persistentPromptHistory != nil {
			if err := m.persistentPromptHistory.Append(content); err != nil {
				slog.Warn("failed to save prompt to history", "error", err)
			}
		}
	}
	return m, tea.Batch(cmds...)
}

// handleSlashKey handles the slash key for command completion
func (m TUIModel) handleSlashKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Only show command completion if we're at the beginning of the input
	if m.prompt.Value() == "" {
		m.prompt, _ = m.prompt.Update(msg)
		// Show completion dialog with commands (add / prefix for display)
		m.showCompletionDialog = true
		m.completionMode = "command"
		var commandsWithPrefix []string
		for _, cmd := range m.commandRegistry.order {
			commandsWithPrefix = append(commandsWithPrefix, "/"+cmd)
		}
		m.completions.SetOptions(commandsWithPrefix)
		m.completions.Show()
	} else {
		m.prompt, _ = m.prompt.Update(msg)
	}
	return m, nil
}

// handleColonKey handles the colon key - enters command mode in command line
func (m TUIModel) handleColonKey(_ tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Enter command mode in the command line
	enterCmd := m.commandLine.EnterCommandMode("")
	m.prompt.Blur()

	// Show command completions immediately
	m.updateCommandLineCompletions()

	return m, enterCmd
}

// handleAtKey handles the @ key for file completion
func (m TUIModel) handleAtKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.prompt, _ = m.prompt.Update(msg)
	// Show completion dialog with files
	m.showCompletionDialog = true
	m.completionMode = "file"
	files, err := getFileTree(".")
	if err != nil {
		m.content.Chat.AddMessage(fmt.Sprintf("Error scanning files: %v", err))
	} else {
		m.updateFileCompletions(files)
	}
	m.completions.Show()
	return m, nil
}

// handleShellCommand executes a shell command using the run_shell_command tool
func (m TUIModel) handleShellCommand(command string) (tea.Model, tea.Cmd) {
	// Extract the shell command (everything after !)
	shellCmd := strings.TrimSpace(strings.TrimPrefix(command, "!"))
	if shellCmd == "" {
		m.commandLine.AddToast("No command specified after !", "error", time.Second*3)
		m.prompt.Focus()
		return m, nil
	}

	m.content.Chat.AddToRawHistory("SHELL_COMMAND", shellCmd)

	// Make session active so chat is visible (not welcome screen)
	m.sessionActive = true

	// Display the command in chat similar to a shell prompt
	m.content.Chat.AddShellCommandInput(shellCmd)

	// Execute the shell command using the current shell runner (sandbox or host)
	return m, func() tea.Msg {
		ctx := context.Background()

		// Get the current shell runner (podman sandbox or host)
		runner := m.shogunate.GetRunner()

		// User-initiated commands never need approval
		params := runners.Input{
			Command:        shellCmd,
			Description:    "User shell command",
			BypassApproval: true, // User explicitly requested this command
		}

		output, err := runner.Run(ctx, params)

		if err != nil {
			return shellCommandResultMsg{
				command:  shellCmd,
				output:   "",
				exitCode: "-1",
				err:      err,
			}
		}

		return shellCommandResultMsg{
			command:  shellCmd,
			output:   output.Output,
			exitCode: output.ExitCode,
			err:      nil,
		}
	}
}

// handleWindowSizeMsg handles window resize events
func (m TUIModel) handleWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.updateComponentDimensions()

	return m, nil
}

// handleCustomMessages handles all custom message types
func (m TUIModel) handleCustomMessages(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SubmitPromptMsg:
		var cmds []tea.Cmd
		content := msg.Prompt

		// This logic is adapted from handleEnterKey
		m.commandLine.ClearToasts()
		m.repoInfo.RefreshDiff()

		if m.historySaved && m.historyCursor < len(m.sessionPromptHistory) {
			entry := m.sessionPromptHistory[m.historyCursor]
			m.cancelStreaming()
			m.stopStreaming()
			if session := m.getCurrentSession(); session != nil {
				session.RollbackTo(entry.SessionSnapshot)
			}
			m.content.Chat.TruncateTo(entry.ChatSnapshot)
			m.content.Chat.ClearToolCallMessageIndex()
			m.historySaved = false
		}

		m.content.Chat.AddToRawHistory("USER", content)
		chatSnapshot := len(m.content.Chat.Messages)
		var sessionSnapshot int
		session := m.getCurrentSession()
		if session != nil {
			sessionSnapshot = session.GetMessageSnapshot()
		}
		if m.historyCursor < len(m.sessionPromptHistory) {
			m.sessionPromptHistory = m.sessionPromptHistory[:m.historyCursor]
		}
		m.content.Chat.AddUserMessage(content)
		if m.shogunate != nil {
			if session != nil {
				info := session.GetContextInfo()
				autoCompactThreshold := float64(info.TotalTokens) * 0.10
				if float64(info.FreeTokens) < autoCompactThreshold && len(session.GetMessages()) > 2 {
					slog.Info("auto-compacting conversation", "free_tokens", info.FreeTokens, "threshold", autoCompactThreshold)
					m.content.Chat.AddMessage("🗜️  Auto-compacting conversation history (low on context)...")
					ctx := context.Background()
					_, err := session.CompactHistory(ctx, compactPrompt)
					if err != nil {
						slog.Warn("auto-compaction failed", "error", err)
						m.content.Chat.AddMessage(fmt.Sprintf("⚠️  Auto-compaction failed: %v", err))
					} else {
						newInfo := session.GetContextInfo()
						m.content.Chat.AddMessage(fmt.Sprintf("✅ Conversation compacted! Context usage: %s/%s tokens (%.1f%%)",
							formatTokenCount(newInfo.UsedTokens),
							formatTokenCount(newInfo.TotalTokens),
							percentage(newInfo.UsedTokens, newInfo.TotalTokens)))
						slog.Info("auto-compaction completed", "old_used", info.UsedTokens, "new_used", newInfo.UsedTokens, "saved", info.UsedTokens-newInfo.UsedTokens)
					}
				}
			}

			m.sessionActive = true
			if waitCmd := m.startWaitingForResponse(); waitCmd != nil {
				cmds = append(cmds, waitCmd)
			}
			ctx, cancel := context.WithCancel(context.Background())
			m.streamingCancel = cancel

			// Get context files from session (populated via @ references)
			var contextFiles map[string]string
			if session != nil {
				contextFiles = session.GetContextFiles()
			}
			shogunateCmd := m.submitToShogunate(ctx, content, contextFiles)
			cmds = append(cmds, shogunateCmd)
		} else {
			m.commandLine.AddToast("No model configured. Use :models to select a model", "error", time.Second*5)
		}
		m.sessionPromptHistory = append(m.sessionPromptHistory, promptHistoryEntry{
			Prompt:          content,
			SessionSnapshot: sessionSnapshot,
			ChatSnapshot:    chatSnapshot,
		})
		m.historyCursor = len(m.sessionPromptHistory)
		m.historySaved = false
		m.historyPendingPrompt = ""

		if m.persistentPromptHistory != nil {
			if err := m.persistentPromptHistory.Append(content); err != nil {
				slog.Warn("failed to save prompt to history", "error", err)
			}
		}

		return m, tea.Batch(cmds...)

	case responseMsg:
		m.content.Chat.AddToRawHistory("AI_RESPONSE", string(msg))
		m.stopStreaming()
		// Use AddAIChunk for non-streaming AI responses
		m.content.Chat.AddAIChunk(string(msg))
		m.content.Chat.FinalizeLastAIMessage()
		m.repoInfo.RefreshDiff()

	case runners.ToolCallScheduledMsg:
		m.content.Chat.AddToRawHistory("TOOL_SCHEDULED", fmt.Sprintf("%s with input: %s", msg.Call.Tool.Name(), msg.Call.Input))
		m.content.Chat.HandleToolCallScheduled(msg)

	case runners.ToolCallExecutingMsg:
		m.content.Chat.AddToRawHistory("TOOL_EXECUTING", fmt.Sprintf("%s with input: %s", msg.Call.Tool.Name(), msg.Call.Input))
		m.content.Chat.HandleToolCallExecuting(msg)

	case runners.ToolCallSuccessMsg:
		m.content.Chat.AddToRawHistory("TOOL_SUCCESS", fmt.Sprintf("%s\nInput: %s\nOutput: %s", msg.Call.Tool.Name(), msg.Call.Input, msg.Call.Result))
		m.content.Chat.HandleToolCallSuccess(msg)
		m.repoInfo.RefreshDiff()

	case runners.ToolCallErrorMsg:
		m.content.Chat.AddToRawHistory("TOOL_ERROR", fmt.Sprintf("%s\nInput: %s\nError: %v", msg.Call.Tool.Name(), msg.Call.Input, msg.Call.Error))
		m.content.Chat.HandleToolCallError(msg)

	case runners.ToolCallAbortedMsg:
		m.content.Chat.AddToRawHistory("TOOL_ABORTED", fmt.Sprintf("%s\nInput: %s\nReason: sandbox restarted", msg.Call.Tool.Name(), msg.Call.Input))
		m.content.Chat.HandleToolCallAborted(msg)

	case errMsg:
		m.content.Chat.AddToRawHistory("ERROR", fmt.Sprintf("%v", msg.err))
		m.content.Chat.AddMessage(fmt.Sprintf("Error: %v", msg.err))

	case shogunate.StreamStartMsg:
		// Streaming has started
		m.content.Chat.AddToRawHistory("STREAM_START", "AI streaming response started")
		slog.Debug("streamStartMsg", "starting_stream", true)
		m.streamingActive = true
		m.status.ClearError() // Clear any previous error state

	case shogunate.StreamCompleteMsg:
		m.content.Chat.AddToRawHistory("STREAM_COMPLETE", "AI streaming response completed")
		slog.Debug("streamCompleteMsg", "messages_count", len(m.content.Chat.Messages))
		m.stopStreaming()

		// Finalize the last AI message with success/failure prefix
		isFailure := m.content.Chat.FinalizeLastAIMessage()
		if isFailure {
			slog.Debug("AI response marked as failure")
		}

		// Run guardrail callback if one was set
		var guardrailCmd tea.Cmd
		if m.streamCompleteCallback != nil {
			slog.Debug("running stream complete callback")
			guardrailCmd = m.streamCompleteCallback(&m)
			m.streamCompleteCallback = nil // Clear after running
		}

		m.saveSession()
		m.repoInfo.RefreshDiff()

		return m, guardrailCmd

	case shogunate.StreamInterruptedMsg:
		// Streaming was interrupted by user
		m.content.Chat.AddToRawHistory("STREAM_INTERRUPTED", fmt.Sprintf("AI streaming interrupted, partial content: %s", msg.PartialContent))
		slog.Debug("streamInterruptedMsg", "partial_content_length", len(msg.PartialContent))
		m.stopStreaming()
		m.streamCompleteCallback = nil // Clear callback on interrupt
		m.repoInfo.RefreshDiff()

	case shogunate.StreamErrorMsg:
		fullError := fmt.Sprintf("Model Error: %v", msg.Err)
		m.content.Chat.AddToRawHistory("STREAM_ERROR", fmt.Sprintf("AI streaming error: %v", msg.Err))
		slog.Error("shogunate.StreamErrorMsg", "error", msg.Err)
		// Add full error message to chat for visibility
		m.content.Chat.AddMessage(fmt.Sprintf("\n%s❌ %s", systemPrefix, fullError))
		// Toast will be automatically truncated by commandline component if needed
		m.commandLine.AddToast(fullError, "error", time.Second*5)
		m.status.SetError() // Update status icon to show error
		m.stopStreaming()
		m.repoInfo.RefreshDiff()

		/* TODO: Add the message bellow
		case streamMaxTurnsExceededMsg:
			// Max turns exceeded, mark session as inactive and show warning
			m.content.Chat.AddToRawHistory("STREAM_MAX_TURNS_EXCEEDED", fmt.Sprintf("AI streaming ended after reaching max turns limit: %d", msg.maxTurns))
			slog.Warn("streamMaxTurnsExceededMsg", "max_turns", msg.maxTurns)
			m.content.Chat.AddMessage(fmt.Sprintf("\n⚠️  Conversation ended after reaching maximum turn limit (%d turns)", msg.maxTurns))
			m.stopStreaming()
			m.streamCompleteCallback = nil // Clear callback on max turns
			m.repoInfo.RefreshDiff()
		*/
	case shogunate.StreamMaxTokensReachedMsg:
		// Max tokens reached, mark session as inactive and show warning
		m.content.Chat.AddToRawHistory("STREAM_MAX_TOKENS_REACHED", fmt.Sprintf("AI response truncated due to length limit: %s", msg.Content))
		slog.Warn("streamMaxTokensReachedMsg", "content_length", len(msg.Content))
		m.content.Chat.AddMessage("\n\n⚠️  Response truncated due to length limit")
		m.stopStreaming()
		m.streamCompleteCallback = nil // Clear callback on max tokens
		m.repoInfo.RefreshDiff()

	// Shogunate streaming message handlers
	case shogunate.StreamChunkMsg:
		// Handle text chunks from Shogunate
		m.content.Chat.AddToRawHistory("SHOGUNATE_TEXT", msg.Text)
		if m.streamingActive {
			m.waitingStart = time.Now()
			if !m.waitingForResponse {
				waitCmd := m.startWaitingForResponse()
				m.content.Chat.AddAIChunk(msg.Text)
				return m, waitCmd
			}
		}
		m.content.Chat.AddAIChunk(msg.Text)
		return m, nil

	case shogunate.StreamReasoningChunkMsg:
		// Handle thinking/reasoning chunks from Shogunate
		m.content.Chat.AddToRawHistory("SHOGUNATE_THOUGHT", msg.Text)
		m.content.Chat.AddThinkingChunk(msg.Text)
		return m, nil

	case showHelpMsg:
		// Show the help viewer with the requested topic
		return m, m.content.ShowHelp(msg.topic)

	case showContextMsg:
		m.content.Chat.AddToRawHistory("CONTEXT", msg.content)
		m.content.Chat.AddMessage(msg.content)
		m.sessionActive = true

	case updateCheckMsg:
		if msg.err != nil {
			m.content.Chat.AddMessage(fmt.Sprintf("%s❌ Failed to check for updates: %v", systemPrefix, msg.err))
			return m, nil
		}

		if !msg.hasUpdate {
			m.content.Chat.AddMessage(fmt.Sprintf("%s✓ You're running the latest version (%s)", systemPrefix, msg.latest))
			return m, nil
		}

		// Update available - ask for confirmation
		question := fmt.Sprintf("%sUpdate available: %s → %s. Do you want to update now?", systemPrefix, version, msg.latest)
		return m, m.commandLine.EnterYesNoMode(question)

	case updateAvailableMsg:
		// Background update check found a new version - just set the flag
		// The home view will display the notification
		m.updateAvailable = true
		return m, nil

	case yesNoResponseMsg:
		// Check if this is a response to a host command approval request
		if m.pendingHostApproval != nil {
			// Send the response back to the waiting goroutine
			m.pendingHostApproval.ResponseChan <- msg.answer
			if msg.answer {
				// User approved - update the emoji to ⚙️ (executing)
				m.content.Chat.UpdateLastToolCallEmoji(m.pendingHostApproval.Command, "⚙️")
			} else {
				// User denied - update the emoji to ⛔︎
				m.content.Chat.UpdateLastToolCallEmoji(m.pendingHostApproval.Command, "⛔︎")
			}
			m.pendingHostApproval = nil
			return m, nil
		}

		// Otherwise, this is an update confirmation
		if msg.answer {
			// User confirmed update
			return m, handleUpdateConfirm(&m)
		}
		// User declined
		cancelMsg := NewChatMsgBuilder(systemPrefix)
		cancelMsg.WriteLn("Update cancelled.")
		cancelMsg.WriteLn("Please run :update again when ready")
		m.content.Chat.AddMessage(cancelMsg.String())
		return m, nil

	case runners.ApprovalRequestMsg:
		// Store the pending approval request
		m.pendingHostApproval = &msg
		m.content.Chat.UpdateLastToolCallEmoji(msg.Command, approvalPrefix)
		displayCmd := msg.Command
		maxLen := 50
		// TODO: truncate the middle
		if len(displayCmd) > maxLen {
			displayCmd = displayCmd[:maxLen] + "..."
		}
		return m, m.commandLine.EnterYesNoMode(fmt.Sprintf("Allow `%s` to run?", displayCmd))

	case updateCompleteMsg:
		if msg.err != nil {
			errMsg := NewChatMsgBuilder(systemPrefix)
			errMsg.WriteLnf("❌ Update failed: %v", msg.err)
			errMsg.WriteLnf("Try updating manually with: %s", GetUpdateCommand())
			m.content.Chat.AddMessage(errMsg.String())
			return m, nil
		}

		successMsg := NewChatMsgBuilder(systemPrefix)
		successMsg.WriteLn("✓ Update successful!")
		successMsg.WriteLn("Please restart asimi to use the new version.")
		m.content.Chat.AddMessage(successMsg.String())
		return m, nil

	case waitingTickMsg:
		if m.waitingForResponse {
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return waitingTickMsg{} })
		}
		return m, nil

	case ctrlCBurstTimeoutMsg:
		return m.handleCtrlCBurstTimeout(msg)

	case ctrlCWindowExpiredMsg:
		return m.handleCtrlCWindowExpired()

	case providerSelectedMsg:
		m.providerModal = nil
		provider := msg.provider.Key

		// Handle Anthropic specially - show code input modal immediately
		// TODO: move to performOAuthLogin
		if provider == "anthropic" {
			auth := &AuthAnthropic{}
			authURL, verifier, err := auth.authorize()
			if err != nil {
				slog.Warn("Anthropic Auth failed", "error", err)
				m.commandLine.AddToast("Authorization failed", "error", 4000)
				return m, nil
			}

			// Open browser
			if err := openBrowser(authURL); err != nil {
				m.commandLine.AddToast("Failed to open browser", "warning", 3000)
			}

			// Show code input modal
			m.codeInputModal = NewCodeInputModal(authURL, verifier)
			m.config.LLM.Provider = provider
			m.config.LLM.Model = "claude-3-5-sonnet-latest"
			m.commandLine.AddToast("Logged in", "success", 3000)
		} else {
			// Other providers use the standard OAuth flow
			return m, m.performOAuthLogin(provider)
		}

	case showOauthFailed:
		m.content.Chat.AddToRawHistory("OAUTH_ERROR", msg.err)
		errToast := fmt.Sprintf("OAuth failed: %s", msg.err)
		m.commandLine.AddToast(errToast, "error", 4000)
		m.content.Chat.AddMessage(errToast)
		m.sessionActive = false

	case modalCancelledMsg:
		m.providerModal = nil
		m.codeInputModal = nil
		// Return to chat view
		m.commandLine.AddToast("Cancelled", "info", 2000)
		return m, m.content.ShowChat()

	case authCodeEnteredMsg:
		m.codeInputModal = nil
		return m, m.completeAnthropicOAuth(msg.code, msg.verifier)

	case urlCopiedToClipboardMsg:
		if msg.err != nil {
			m.commandLine.AddToast("Failed to copy URL to clipboard: "+msg.err.Error(), "error", 3000)
		} else {
			m.commandLine.AddToast("URL copied to clipboard", "success", 3000)
		}
		return m, m.codeInputModal.textInput.Focus()

	case modelSelectedMsg:
		if msg.onSelect != nil {
			return m, msg.onSelect
		}
		oldProvider := m.config.LLM.Provider
		oldModel := m.config.LLM.Model

		// Update provider and model
		m.config.LLM.Provider = msg.model.Provider
		m.config.LLM.Model = msg.model.ID

		// Load API key for the new provider if needed
		if msg.model.Provider != oldProvider {
			// Clear existing auth tokens when switching providers
			m.config.LLM.AuthToken = ""
			m.config.LLM.RefreshToken = ""
			m.config.LLM.APIKey = ""

			// Credentials will be loaded by getModelClient() from keyring
			// No need to load them here - getModelClient handles expiration and refresh
		}

		// Save config and reinitialize session
		if err := config.SaveConfig(m.config); err != nil {
			slog.Error("Failed to save config", "error", err)
			m.commandLine.AddToast("Failed to save config", "error", 4000)
			// Revert changes
			m.config.LLM.Provider = oldProvider
			m.config.LLM.Model = oldModel
		} else {
			modelName := msg.model.DisplayName
			if modelName == "" {
				modelName = msg.model.ID
			}
			providerChanged := ""
			if msg.model.Provider != oldProvider {
				providerChanged = fmt.Sprintf(" (switched to %s)", msg.model.Provider)
			}
			m.commandLine.AddToast(fmt.Sprintf("Model changed to %s%s", modelName, providerChanged), "success", 3000)
			return m, m.switchModel()
		}
		return m, nil

	case modelsLoadedMsg:
		return m, m.content.ShowUnifiedModels(msg.models, m.config.LLM.Model)

	case showModelSelectionMsg:
		// Trigger model selection after login completes
		return m, handleModelsCommand(&m, nil)

	case modelsLoadErrorMsg:
		m.content.SetModelsError(msg.error)

	case sessionsLoadedMsg:
		return m, m.content.ShowResume(msg.sessions)

	case ChangeModeMsg:
		// Centralized mode change handling
		oldMode := m.Mode
		newMode := msg.NewMode

		// Update mode
		m.Mode = newMode
		m.status.SetMode(newMode)

		if newMode != "command" && newMode != "yesno" {
			m.commandLine.Blur()
		}

		// Handle scroll lock state changes
		if oldMode == "scroll" && newMode != "scroll" {
			m.content.Chat.SetScrollLock(false)
		} else if oldMode != "scroll" && newMode == "scroll" {
			m.content.Chat.SetScrollLock(true)
		}

		// Update prompt component based on new mode
		switch newMode {
		case "insert":
			m.prompt.EnterViInsertMode()
		case "normal":
			m.prompt.EnterViNormalMode()
		case "visual":
			// Visual mode uses normal keymap but different styling
			m.prompt.ViCurrentMode = ViModeVisual
			m.prompt.TextArea.KeyMap = m.prompt.viNormalKeyMap
			m.prompt.TextArea.Placeholder = "Visual selection mode"
			// Trigger style update by calling the private method via a public interface
			// For now, we'll just set it directly since we're in the same package
			if globalTheme != nil {
				m.prompt.Style = m.prompt.Style.BorderForeground(globalTheme.PromptOffBorder)
			}
		case "scroll":
			m.prompt.Blur()
			m.prompt.EnterViScrollMode()
		case "command":
			m.prompt.EnterViCommandLineMode()
		case "yesno":
			m.prompt.Style = m.prompt.Style.BorderForeground(globalTheme.PromptOffBorder)
		case "learning":
			m.prompt.EnterViLearningMode()
		case "select", "resume", "models", "help":
			// These modes don't need prompt updates, just placeholder changes
			m.prompt.Blur()
			m.prompt.TextArea.Placeholder = "j/k, CTRL-D/U to navigate | Enter to select | ESC to abort"
			m.prompt.Style = m.prompt.Style.BorderForeground(globalTheme.PromptOffBorder)
		}

		return m, nil

	case commandReadyMsg:
		// Command ready from command line component
		// Hide completion dialog
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""

		// Save to persistent command history
		if m.persistentCommandHistory != nil {
			if err := m.persistentCommandHistory.Append(msg.command); err != nil {
				slog.Warn("failed to save command to history", "error", err)
			}
		}

		// Check if this is a shell command (starts with !)
		if strings.HasPrefix(msg.command, "!") {
			return m.handleShellCommand(msg.command)
		}

		// Parse and execute the command
		parts := strings.Fields(":" + msg.command)
		if len(parts) > 0 {
			cmdName := parts[0]
			m.content.Chat.AddToRawHistory("COMMAND", ":"+msg.command)

			// Use FindCommand for vim-style partial matching
			cmd, matches, found := m.commandRegistry.FindCommand(cmdName)
			if found {
				c := cmd.Handler(&m, parts[1:])
				m.prompt.Focus()
				return m, c
			} else if len(matches) > 1 {
				// Ambiguous command
				displayMatches := make([]string, len(matches))
				for i, match := range matches {
					displayMatches[i] = ":" + strings.TrimPrefix(match, "/")
				}
				errorMsg := fmt.Sprintf("Ambiguous command '%s'. Matches: %s",
					strings.TrimPrefix(cmdName, ":"),
					strings.Join(displayMatches, ", "))
				m.commandLine.AddToast(errorMsg, "error", time.Second*3)
			} else {
				// Unknown command
				m.commandLine.AddToast(fmt.Sprintf("Unknown command: %s", strings.TrimPrefix(cmdName, ":")), "error", time.Second*3)
			}
		}
		m.prompt.Focus()
		return m, nil

	case commandCancelledMsg:
		// Command cancelled - hide completions and restore focus
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""
		m.prompt.Focus()
		return m, nil

	case commandTextChangedMsg:
		// Command text changed - update completions
		m.updateCommandLineCompletions()
		return m, nil

	case navigateCompletionMsg:
		// Navigate completion dialog
		if msg.direction < 0 {
			m.completions.SelectPrev()
		} else {
			m.completions.SelectNext()
		}
		return m, nil

	case navigateHistoryMsg:
		// Navigate completion if visible, otherwise do nothing
		if m.showCompletionDialog {
			if msg.direction < 0 {
				m.completions.SelectPrev()
			} else {
				m.completions.SelectNext()
			}
		}
		return m, nil

	case acceptCompletionMsg:
		// Accept completion if visible
		if m.showCompletionDialog {
			selected := m.completions.GetSelected()
			if selected != "" {
				// Set the command text to the selected completion (without the : prefix)
				cmdText := strings.TrimPrefix(selected, ":")
				m.commandLine.SetCommand(cmdText)
			}
		}
		return m, nil

	case sessionSelectedMsg:
		m.handleSessionSelected(msg.session)
		return m, nil

	case sessionResumeErrorMsg:
		m.commandLine.AddToast(fmt.Sprintf("Failed to resume session: %v", msg.err), "error", 4000)
		return m, m.content.ShowChat()

	case llmInitSuccessMsg:
		// LLM initialization completed - configure Shogunate with the model
		m.status.SetProvider(m.config.LLM.Provider, m.config.LLM.Model, true)
		if m.shogunate != nil && msg.model != nil {
			cfg := &shogunate.SessionConfig{
				LLM: config.LLMConfig{
					MaxTurns:          m.config.LLM.MaxTurns,
					MaxThinkingTokens: m.config.LLM.MaxThinkingTokens,
					Provider:          m.config.LLM.Provider,
					Model:             m.config.LLM.Model,
				},
			}
			repoInfo := repo.RepoInfo{
				ProjectRoot: m.config.Storage.DatabasePath,
			}
			m.shogunate.ConfigureModel(msg.model, cfg, repoInfo)
			slog.Info("Shogunate configured with LLM model")
		}

	case llmInitErrorMsg:
		// LLM initialization failed
		slog.Warn("LLM initialization failed", "error", msg.err)
		m.commandLine.AddToast("Running without a model, use `:models` to set", "warning", 5000)

	case startConversationMsg:
		// Handle starting a new conversation (used by init, new, and other commands)
		slog.Debug("got startConversationMsg", "RunOnHost", msg.RunOnHost, "tryUpgradeToSandbox", msg.tryUpgradeToSandbox)

		// Clear history if requested - this can happen even without shogunate
		if msg.clearHistory {
			m.sessionActive = true
			// Clear the chat instead of creating a new component to avoid re-initializing the markdown renderer
			m.content.Chat.Clear()

			// Reset prompt history and waiting state
			m.initHistory()
			m.cancelStreaming()
			m.stopStreaming()

			// Reset session conversation history
			if session := m.getCurrentSession(); session != nil {
				session.ClearHistory()
			}
		}

		// Display initial messages after clearing history (before streaming starts)
		for _, initialMsg := range msg.initialMessages {
			m.content.Chat.AddMessage(initialMsg)
		}

		// The rest of the operations require a shogunate
		if m.shogunate == nil {
			m.commandLine.AddToast("No LLM session available", "error", 4000)
			return m, nil
		}

		// Set the shell runner based on RunOnHost flag
		if msg.RunOnHost {
			slog.Debug("using host shell runner for this conversation")
			m.shogunate.GetRunner().AllowFallback(true)

			// Wrap the caller's func with code to restore the previous runner
			originalCallback := msg.onStreamComplete
			msg.onStreamComplete = func(model *TUIModel) tea.Cmd {
				model.shogunate.GetRunner().AllowFallback(false)

				// Call the original callback if it exists
				if originalCallback != nil {
					return originalCallback(model)
				}
				return nil
			}
		}

		// Store the callback for later use
		m.streamCompleteCallback = msg.onStreamComplete

		// Try to upgrade from host to sandbox if requested (async)
		var upgradeCmd tea.Cmd
		if msg.tryUpgradeToSandbox {
			upgradeCmd = func() tea.Msg {
				upgraded := false // TODO: fix tryUpgradeToSandbox(m.config)
				return sandboxUpgradeMsg{upgraded: upgraded}
			}
		}

		// Add initialization message if this is an init command (has a prompt and callback)
		// If there's a prompt, send it to the AI
		if msg.prompt != "" {
			ctx, cancel := context.WithCancel(context.Background())
			m.streamingCancel = cancel
			m.sessionActive = true

			var streamCmd tea.Cmd
			// Route through Shogunate
			streamCmd = m.submitToShogunate(ctx, msg.prompt, nil)

			if waitCmd := m.startWaitingForResponse(); waitCmd != nil {
				return m, tea.Batch(waitCmd, streamCmd, upgradeCmd)
			} else if streamCmd != nil {
				return m, tea.Batch(streamCmd, upgradeCmd)
			}
		} else {
			// If RunOnHost is true and restoration callback is set, restore runner immediately
			if msg.RunOnHost && msg.onStreamComplete != nil {
				msg.onStreamComplete(&m)
			}
		}

		if upgradeCmd != nil {
			return m, upgradeCmd
		}
		return m, nil

	case sandboxUpgradeMsg:
		// Handle sandbox upgrade result
		if msg.upgraded {
			slog.Info("successfully upgraded from host to sandbox runner")
			m.commandLine.AddToast("🐳 Sandbox now available", "info", 3000)
			// Refresh the first message to show the updated sandbox status
			if len(m.content.Chat.Messages) > 0 {
				m.content.Chat.Messages[0] = ChatMessage{Content: newSessionMessage(), Indent: 0}
				m.content.Chat.UpdateContent()
			}
		}
		return m, nil

	case compactConversationMsg:
		// Handle conversation compaction
		slog.Debug("got compactConversationMsg")

		// Try Shogunate session first, then fall back to main session
		var compactFunc func(ctx context.Context, prompt string) (string, error)

		if m.shogunate != nil && m.currentEdictID != "" {
			if shogunateSession := m.shogunate.GetCurrentSession(m.currentEdictID); shogunateSession != nil {
				compactFunc = shogunateSession.CompactHistory
				slog.Debug("using Shogunate session for compaction", "edict_id", m.currentEdictID)
			}
		}

		if compactFunc == nil {
			m.commandLine.AddToast("No LLM session available for compaction", "error", 4000)
			return m, nil
		}

		// Add a message to show we're compacting
		compactMsg := NewChatMsgBuilder("🗜️  Compacting conversation history...")
		compactMsg.WriteLn("This may take a moment as the model summarizes the chat.")
		m.content.Chat.AddMessage(compactMsg.String())

		// Perform the compaction in a goroutine
		go func() {
			ctx := context.Background()
			summary, err := compactFunc(ctx, compactPrompt)
			if err != nil {
				if program != nil {
					program.Send(compactErrorMsg{err: err})
				}
				return
			}
			if program != nil {
				program.Send(compactCompleteMsg{summary: summary})
			}
		}()

	case compactCompleteMsg:
		// Compaction completed successfully
		slog.Debug("compaction completed")

		// Get context info from the appropriate session
		var info ContextInfo

		if m.shogunate != nil && m.currentEdictID != "" {
			if shogunateSession := m.shogunate.GetCurrentSession(m.currentEdictID); shogunateSession != nil {
				shogunateInfo := shogunateSession.GetContextInfo()
				info = ContextInfo{
					Model:              shogunateInfo.Model,
					TotalTokens:        shogunateInfo.TotalTokens,
					UsedTokens:         shogunateInfo.UsedTokens,
					SystemPromptTokens: shogunateInfo.SystemPromptTokens,
					SystemToolsTokens:  shogunateInfo.SystemToolsTokens,
					MemoryFilesTokens:  shogunateInfo.MemoryFilesTokens,
					MessagesTokens:     shogunateInfo.MessagesTokens,
					FreeTokens:         shogunateInfo.FreeTokens,
					AutocompactBuffer:  shogunateInfo.AutocompactBuffer,
				}
			}
		}

		// Info was obtained from shogunate session above; if TotalTokens is 0, we have no valid session
		// (the shogunate session check is already done above)

		// Add success message
		m.content.Chat.AddMessage(fmt.Sprintf("✅ Conversation compacted successfully!\n\nContext usage: %s/%s tokens (%.1f%%)",
			formatTokenCount(info.UsedTokens),
			formatTokenCount(info.TotalTokens),
			percentage(info.UsedTokens, info.TotalTokens)))

		m.commandLine.AddToast("Conversation history compacted", "success", 3000)

	case compactErrorMsg:
		// Compaction failed
		slog.Warn("compaction failed", "error", msg.err)
		m.content.Chat.AddMessage(fmt.Sprintf("❌ Failed to compact conversation: %v\n\nYour conversation context was left unchanged.", msg.err))
		m.commandLine.AddToast("Compaction failed - context unchanged", "error", 3000)

	case runners.ContainerLaunchedMsg:
		// Container launch notification
		m.commandLine.AddToast(msg.Message, "info", 3*time.Second)
		// Update shell runner info in status bar
		// TODO set the container ID
		m.status.ContainerID = m.status.ContainerID
		return m, nil

	case shellCommandResultMsg:
		// Shell command execution completed
		m.content.Chat.AddShellCommandResult(msg)
		m.repoInfo.RefreshDiff()
		m.prompt.Focus()
		return m, nil

	// Init workflow messages
	case startInitWorkflowMsg:
		// Start the init workflow asynchronously
		m.sessionActive = true
		return m, runInitWorkflowAsync(&m, msg.ClearMode, msg.AgentsFile)

	case initWorkflowProgressMsg:
		// Update UI with workflow progress
		m.content.Chat.AddMessage(msg.message)
		return m, nil

	case initWorkflowCompleteMsg:
		// Workflow completed
		if msg.success {
			m.content.Chat.AddMessage(msg.message)
			m.commandLine.AddToast("Initialization complete!", "success", 3*time.Second)

			// Start a fresh session without clearing the screen
			m.saveSession()
			m.sessionActive = true
			m.content.Chat.Indent = 0
			m.initHistory()
			m.cancelStreaming()
			m.stopStreaming()
			if session := m.getCurrentSession(); session != nil {
				session.ClearHistory()
			}

			// Try to upgrade to sandbox (async) in case it wasn't already done
			m.repoInfo.RefreshDiff()
			return m, func() tea.Msg {
				upgraded := false // TODO: we need to tryUpgradeToSandbox(m.config)
				return sandboxUpgradeMsg{upgraded: upgraded}
			}
		}
		m.repoInfo.RefreshDiff()
		return m, nil

	case initWorkflowErrorMsg:
		// Workflow failed
		slog.Error("Init workflow failed", "error", msg.err)
		m.content.Chat.AddMessage(fmt.Sprintf("%s❌ Initialization failed: %v", systemPrefix, msg.err))
		m.commandLine.AddToast("Initialization failed", "error", 5*time.Second)
		return m, nil

	}

	// Restore focus to prompt if no modals are active and view is chat
	if m.providerModal == nil && m.codeInputModal == nil &&
		!m.commandLine.IsInCommandMode() &&
		m.content.GetActiveView() == ViewChat {
		m.prompt.Focus()
	}

	// Update content (which handles chat updates)
	var contentCmd tea.Cmd
	m.content, contentCmd = m.content.Update(msg)
	return m, contentCmd
}

func (m *TUIModel) updateFileCompletions(files []string) {
	inputValue := m.prompt.Value()

	// Find the last @ character to determine what we're completing
	lastAt := strings.LastIndex(inputValue, "@")
	if lastAt == -1 {
		m.completions.SetOptions([]string{})
		return
	}

	// Extract the text after the last @ for completion
	searchQuery := inputValue[lastAt+1:]

	// If there's a space in the search query, we're likely starting a new file reference
	if spaceIndex := strings.Index(searchQuery, " "); spaceIndex != -1 {
		searchQuery = searchQuery[spaceIndex+1:]
	}

	var filteredFiles []string
	for _, file := range files {
		if strings.Contains(strings.ToLower(file), strings.ToLower(searchQuery)) {
			filteredFiles = append(filteredFiles, file)
		}
	}

	// Sort by the position of the search query
	sort.Slice(filteredFiles, func(i, j int) bool {
		s1 := filteredFiles[i]
		s2 := filteredFiles[j]
		lowerS1 := strings.ToLower(s1)
		lowerS2 := strings.ToLower(s2)
		lowerSearch := strings.ToLower(searchQuery)

		i1 := strings.Index(lowerS1, lowerSearch)
		i2 := strings.Index(lowerS2, lowerSearch)

		if i1 == i2 {
			return s1 < s2
		}

		return i1 < i2
	})

	var options []string
	for _, file := range filteredFiles {
		options = append(options, file)
	}
	m.completions.SetOptions(options)
}

// updateCommandCompletions filters commands based on current input
func (m *TUIModel) updateCommandCompletions() {
	inputValue := m.prompt.Value()

	// Determine if we're using vi mode colon commands or regular slash commands
	var prefix string
	var searchQuery string

	if strings.HasPrefix(inputValue, "/") {
		prefix = "/"
		searchQuery = strings.ToLower(inputValue[1:])
	} else if strings.HasPrefix(inputValue, ":") {
		prefix = ":"
		searchQuery = strings.ToLower(inputValue[1:])
	} else {
		// No command prefix found
		m.completions.SetOptions([]string{})
		return
	}

	// Get all command names and filter them
	var filteredCommands []string
	for _, name := range m.commandRegistry.order {
		// Commands are now stored without prefix
		// Check if the command starts with the search query
		if strings.HasPrefix(strings.ToLower(name), searchQuery) {
			// Format the command with the appropriate prefix for display
			if prefix == ":" {
				filteredCommands = append(filteredCommands, ":"+name)
			} else {
				filteredCommands = append(filteredCommands, "/"+name)
			}
		}
	}

	m.completions.SetOptions(filteredCommands)
}

// updateCommandLineCompletions filters commands based on command line input
func (m *TUIModel) updateCommandLineCompletions() {
	commandText := m.commandLine.GetCommand()
	searchQuery := strings.ToLower(commandText)

	// Get all command names and filter them
	var filteredCommands []string
	for _, name := range m.commandRegistry.order {
		// Commands are now stored without prefix
		// Check if the command starts with the search query
		if strings.HasPrefix(strings.ToLower(name), searchQuery) {
			// Format with : prefix for command line mode
			filteredCommands = append(filteredCommands, ":"+name)
		}
	}

	m.completions.SetOptions(filteredCommands)

	// Show completion dialog if we have matches
	if len(filteredCommands) > 0 {
		m.showCompletionDialog = true
		m.completionMode = "command"
		m.completions.Show()
	} else {
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""
	}
}

// updateComponentDimensions updates the dimensions of all components based on the window size
func (m *TUIModel) updateComponentDimensions() {
	// Calculate dimensions for vi-like layout (bottom to top):
	// - Command line: 1 line at bottom
	// - Status line: 1 line above command line
	// - Prompt: auto-growing (max 50% screen height)
	// - Empty line: 1 line above prompt
	// - Chat/File viewer: remaining space

	commandLineHeight := 1
	statusHeight := 1
	width := m.width - 2

	// Set screen height for prompt to calculate max height (50%)
	m.prompt.SetScreenHeight(m.height)

	// Calculate desired prompt height based on content
	promptHeight := m.prompt.CalculateDesiredHeight()

	// Account for borders (2 lines for top and bottom border)
	promptWithBorder := promptHeight + 2

	// Calculate chat height
	contentHeight := m.height - commandLineHeight - statusHeight - promptWithBorder + 1
	if contentHeight < 0 {
		contentHeight = 0
	}

	// Update component widths
	m.status.SetWidth(width + 2)
	m.commandLine.SetWidth(width + 2)

	// Full width layout - content handles chat and other views
	m.content.SetSize(width, contentHeight)

	m.prompt.SetWidth(width)
	m.prompt.SetHeight(promptHeight)

	// Update status info
	if m.shogunate != nil && m.currentEdictID != "" {
		m.status.SetProvider(m.config.LLM.Provider, m.config.LLM.Model, true)
	} else {
		m.status.SetProvider(m.config.LLM.Provider, m.config.LLM.Model, false)
	}
	slog.Debug("Updated dimensions", "m.height", m.height, "prompt height", promptHeight, "content height", contentHeight)
}

// View implements bubbletea.Model
func (m TUIModel) View() string {
	start := time.Now()

	defer func() {
		duration := time.Since(start)
		if duration > 100*time.Millisecond {
			slog.Warn("[bubbletea] View() SLOW", "duration", duration)
		}
	}()

	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Update prompt dimensions based on content before rendering
	// This ensures the prompt grows to 10 lines when multiline (#31)
	m.prompt.SetScreenHeight(m.height)
	promptHeight := m.prompt.CalculateDesiredHeight()
	m.prompt.SetHeight(promptHeight)

	// Recalculate content height based on new prompt height
	commandLineHeight := 1
	statusHeight := 1
	promptWithBorder := promptHeight + 2
	contentHeight := m.height - commandLineHeight - statusHeight - promptWithBorder + 1
	if contentHeight < 0 {
		contentHeight = 0
	}
	m.content.SetSize(m.width-2, contentHeight)

	modalHeight := 0
	if m.modal != nil {
		modalHeight = lipgloss.Height(m.modal.Render())
	}
	mainContent := m.renderMainContent(modalHeight)
	promptView := m.prompt.View()
	commandLineView := m.commandLine.View()
	view := m.composeBaseView(mainContent, promptView, commandLineView)
	if m.showCompletionDialog {
		view = m.overlayCompletionDialog(view, promptView, commandLineView)
	}

	// Note: m.modal (BaseModal) is now rendered in composeBaseView above the prompt
	// Only apply centered overlays for OAuth modals here

	if m.providerModal != nil {
		view = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.providerModal.Render())
	}

	if m.codeInputModal != nil {
		// Place the modal centered, leaving room for commandLineView at the bottom
		modalView := lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, m.codeInputModal.Render())
		view = lipgloss.JoinVertical(lipgloss.Left, modalView, commandLineView)
	}

	return view
}

func (m TUIModel) renderMainContent(modalHeight int) string {
	// Account for prompt, status, vi mode/toast line, and command line dynamically
	commandLineHeight := 1
	statusHeight := 1
	promptWithBorder := m.prompt.Height + 2
	contentHeight := m.height - commandLineHeight - statusHeight - promptWithBorder + 1 - modalHeight
	if contentHeight < 0 {
		contentHeight = 0
	}

	// First check if we're viewing help/models/resume - these take precedence
	if m.content.GetActiveView() != ViewChat {
		return m.content.View()
	}

	// Then check for special modes
	switch {
	case m.rawMode:
		return m.renderRawSessionView(m.width, contentHeight)
	case !m.sessionActive:
		return m.renderHomeView(m.width, contentHeight)
	default:
		// Use content component which handles chat view
		return m.content.View()
	}
}

func (m TUIModel) composeBaseView(mainContent, promptView, commandLineView string) string {
	// If help modal is active, insert it above the prompt
	// TODO: we can probably remove this as there are no more modals
	if m.modal != nil {
		modalRender := m.modal.Render()
		statusView := m.status.View()
		// Vi-like layout: Chat -> Modal -> Empty line -> Prompt -> Status -> Command line
		emptyLine := ""
		result := lipgloss.JoinVertical(
			lipgloss.Left,
			mainContent,
			modalRender,
			emptyLine,
			promptView,
			statusView,
			commandLineView,
		)
		return result
	}

	statusView := m.status.View()
	// Vi-like layout: Chat -> Empty line -> Prompt -> Status -> Command line
	result := lipgloss.JoinVertical(
		lipgloss.Left,
		mainContent,
		promptView,
		statusView,
		commandLineView,
	)
	return result
}

func (m TUIModel) overlayCompletionDialog(baseView, promptView, commandLineView string) string {
	dialog := m.completions.View()
	if dialog == "" {
		return baseView
	}

	// 2 is tor the command and status lines
	// TODO: bring it down and cover part of the prompt. Need to wait for bubbletea 2.0
	bottomOffset := 2 + lipgloss.Height(promptView)
	if m.completionMode == "file" {
		// File completion needs extra spacing
		bottomOffset++
	}

	dialogHeight := lipgloss.Height(dialog)
	yPos := m.height - bottomOffset - dialogHeight

	// TODO: fix the overlaying so it looks good
	dialogOverlay := lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, baseView)
	dialogPositioned := lipgloss.Place(m.width, dialogHeight, lipgloss.Left, lipgloss.Top, dialog)

	lines := strings.Split(dialogOverlay, "\n")
	dialogLines := strings.Split(dialogPositioned, "\n")

	if yPos >= 0 && yPos < len(lines) {
		for i, dialogLine := range dialogLines {
			if yPos+i < len(lines) {
				lines[yPos+i] = dialogLine
			}
		}
	}

	return strings.Join(lines, "\n")
}

// renderHomeView renders the home view when no session is active
func (m TUIModel) renderHomeView(width, height int) string {
	// Create a stylish welcome message
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F952F9")). // Terminal7 prompt border
		Align(lipgloss.Center).
		Width(width)

	title := titleStyle.Render("Asimi - Safe, Fast & Opinionated Coding Agent")

	// Create a subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#01FAFA")). // Terminal7 text color
		Align(lipgloss.Center).
		Width(width)

	subtitle := subtitleStyle.Render("🎂  Happy 50th Birthday to visual mode  🎂")

	// Create a list of helpful commands
	commands := []string{
		"▶ Mode base UI, starting in INSERT",
		"▶ Press `CTRL-B` for SCROLL mode",
		"▶ Press `CTRL-C` to stop the model, twice to exit",
		"▶ Press `ESC` to switch modes",
		"▶ Press `!` in COMMAND to run in the sandbox's shell",
		"▶ Type `:model` to setup the model",
		"▶ Type `:init` to generate project's infrastructure file",
		"     e.g, ⌨️ ESC:!uname -aENTER⌨️",
	}

	// Style for commands
	commandStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F4DB53")). // Terminal7 warning/chat border
		PaddingLeft(2)

	// Render commands
	var commandViews []string
	for _, command := range commands {
		commandViews = append(commandViews, commandStyle.Render(command))
	}

	// Build content parts in order: commands, notification, title, subtitle
	var contentParts []string
	contentParts = append(contentParts, lipgloss.JoinVertical(
		lipgloss.Left, commandViews...))

	// Add notification if available (centered)
	if m.updateAvailable {
		updateStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF00")). // Bright green for visibility
			Align(lipgloss.Center).
			Width(width)
		contentParts = append(contentParts, "",
			updateStyle.Render("🚀 Update available! Run :update to install the latest version"))
	}
	if m.configCreated {
		configStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00BFFF")). // Deep sky blue for visibility
			Align(lipgloss.Center).
			Width(width)
		contentParts = append(contentParts, "",
			configStyle.Render("📝 User's config file created at ~/.config/asimi/asimi.conf"))
	}

	// Add title and subtitle at the top (prepend)
	contentParts = append([]string{title, "", subtitle, ""}, contentParts...)

	content := lipgloss.JoinVertical(lipgloss.Center, contentParts...)

	// Create a container that centers the content
	container := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)

	return container
}

// renderRawSessionView renders the raw session view showing complete unfiltered history
func (m TUIModel) renderRawSessionView(width, height int) string {
	rawHistory := m.content.Chat.GetRawHistory()
	if len(rawHistory) == 0 {
		// Show empty state
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#004444")). // Terminal7 text-error
			Align(lipgloss.Center).
			Width(width)

		emptyContent := emptyStyle.Render("Raw session history is empty\nPress Ctrl+O to return to chat")

		container := lipgloss.NewStyle().
			Width(width).
			Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Render(emptyContent)

		return container
	}

	// Create title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F4DB53")). // Terminal7 warning/chat border
		Align(lipgloss.Center).
		Width(width)

	title := titleStyle.Render("Raw Session History (Press Ctrl+O to return to chat)")

	// Style for raw history entries
	entryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#01FAFA")). // Terminal7 text color
		PaddingLeft(1).
		Width(width - 2)

	// Render all history entries
	var historyViews []string
	for _, entry := range rawHistory {
		// Word wrap long entries to fit the width
		wrappedEntry := entry
		if len(entry) > width-4 {
			// Simple word wrap - in real implementation you might use wordwrap.String
			for len(wrappedEntry) > width-4 {
				breakPoint := width - 4
				// Try to break at a space
				for i := breakPoint; i > breakPoint-20 && i > 0; i-- {
					if wrappedEntry[i] == ' ' {
						breakPoint = i
						break
					}
				}
				historyViews = append(historyViews, entryStyle.Render(wrappedEntry[:breakPoint]))
				wrappedEntry = "    " + wrappedEntry[breakPoint:] // Indent continuation lines
			}
			if len(wrappedEntry) > 0 {
				historyViews = append(historyViews, entryStyle.Render(wrappedEntry))
			}
		} else {
			historyViews = append(historyViews, entryStyle.Render(wrappedEntry))
		}
		historyViews = append(historyViews, "") // Add spacing between entries
	}

	historyContent := lipgloss.JoinVertical(lipgloss.Left, historyViews...)

	// Combine title and content
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", historyContent)

	// Create scrollable container
	container := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Render(content)

	return container
}
func (m *TUIModel) stopStreaming() {
	m.streamingActive = false
	m.streamingCancel = nil
	m.stopWaitingForResponse()
}

// jsonEscape escapes a string for use in JSON
func jsonEscape(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// Fallback to simple quote escaping if marshal fails
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return string(b)
}
