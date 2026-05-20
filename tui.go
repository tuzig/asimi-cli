package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/internal/shogunateapi"
	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/shogunate/tools"
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
	prompts        map[string]*PromptComponent
	tabs           TabManager
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

	streamCompleteCallback func(*TUIModel) tea.Cmd // Optional callback to run after stream completes

	// Command registry
	commandRegistry CommandRegistry

	// Application services (passed in, not owned)
	sessionStore *SessionStore
	db           *storage.DB
	scheduler    *runners.CoreToolScheduler
	shogunate    shogunateapi.Client

	// Shogunate integration
	currentEdictKey storage.EdictKey // Tracks current edict for multi-turn conversations

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

	ctrlCLastPress time.Time // Time of last Ctrl-C press for double-press detection

	// Host command approval state
	pendingHostApproval *runners.ApprovalRequestMsg
	// Seal override confirmation state
	pendingSealOverride *pendingSealOverride
	repoInfo            *repo.RepoInfo

	// Debounced render tick: prevents stacking multiple 50ms tick commands
	renderTickPending bool
}

// prompt returns the PromptComponent for the active tab, creating one if needed.
func (m *TUIModel) prompt() *PromptComponent {
	label := m.tabs.ActiveTab().Label
	p, ok := m.prompts[label]
	if !ok {
		np := NewPromptComponent(m.width, 5)
		if m.config != nil && m.config.UI.PromptExpandedHeight > 0 {
			np.SetExpandedHeight(m.config.UI.PromptExpandedHeight)
		}
		p = &np
		m.prompts[label] = p
	}
	return p
}

type promptHistoryEntry struct {
	Prompt          string
	SessionSnapshot int
	ChatSnapshot    int
}

type tickMsg struct{}

// chatRenderTickMsg is produced by a debounce tick to flush dirty chat content.
// Only one tick is pending at a time (guarded by TUIModel.renderTickPending).
type chatRenderTickMsg struct{}

type shellCommandResultMsg struct {
	command  string
	output   string
	exitCode string
	err      error
}

// editorResultMsg is sent after tea.ExecProcess finishes running the editor.
type editorResultMsg struct {
	ResultChan chan tools.EditorResult
	TmpPath    string
	OrigBytes  []byte
	OrigMtime  time.Time
	Err        error
}

// pendingSealOverride tracks state when waiting for Ruler confirmation to seal without prerequisites
type pendingSealOverride struct {
	edictID uint
	notes   string
}

// NewTUIModel creates a new TUI model
// NewTUIModelWithStores creates a new TUI model with provided stores (for fx injection)
func NewTUIModel(cfg *Config, repoInfo *repo.RepoInfo, promptHistory *PromptHistory, commandHistory *CommandHistory, sessionStore *SessionStore, db *storage.DB, scheduler *runners.CoreToolScheduler, shog shogunateapi.Client) *TUIModel {

	registry := NewCommandRegistry()
	theme := NewTheme()

	prompts := map[string]*PromptComponent{}

	// Create status component and set repo info
	status := NewStatusComponent(80)
	status.SetRepoInfo(repoInfo)

	markdownEnabled := false
	if cfg != nil {
		markdownEnabled = cfg.UI.MarkdownEnabled
	}

	model := &TUIModel{
		config: cfg,
		theme:  theme,

		repoInfo: repoInfo,
		// Initialize components
		status:         status,
		prompts:        prompts,
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

	// Initialize tab system with default Chancellor tab
	model.tabs = NewTabManager(80, 18, markdownEnabled, func() string { return model.Mode })
	// Set up tab switch callback to update context percent in status
	model.tabs.onTabSwitch = func() {
		if state, ok := model.currentSessionState(); ok {
			model.status.ContextPercent = state.ContextUsagePercent
		} else {
			// No active session for this tab - context usage is 0%
			model.status.ContextPercent = 0
		}
	}

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

// getCurrentSession returns the current shogunate session based on active tab's target minister.
func (m *TUIModel) getCurrentSession() *shogunate.Session {
	if m.shogunate == nil {
		return nil
	}
	tab := m.tabs.ActiveTab()
	minister := m.shogunate.GetMinister(tab.Target)
	if minister == nil {
		return nil
	}
	return minister.GetSession()
}

// currentTabTarget returns the target identifier of the active tab, or "".
func (m *TUIModel) currentTabTarget() string {
	if m.tabs.ActiveTab() == nil {
		return ""
	}
	return m.tabs.ActiveTab().Target
}

// currentSessionState returns a snapshot of the active tab's session.
// The bool reports whether a session actually exists for that tab.
func (m *TUIModel) currentSessionState() (shogunate.SessionState, bool) {
	if m.shogunate == nil {
		return shogunate.SessionState{}, false
	}
	target := m.currentTabTarget()
	if target == "" {
		return shogunate.SessionState{}, false
	}
	state := m.shogunate.SessionState(target)
	return state, state.Exists
}

// autoCompactIfNeeded triggers a synchronous compaction pass when free
// context tokens drop below 10% and the conversation has more than two
// messages. Chat status messages are written directly to the active chat.
func (m *TUIModel) autoCompactIfNeeded(state shogunate.SessionState) {
	info := state.ContextInfo
	threshold := float64(info.TotalTokens) * 0.10
	if float64(info.FreeTokens) >= threshold || state.MessageCount <= 2 {
		return
	}
	slog.Info("auto-compacting conversation", "free_tokens", info.FreeTokens, "threshold", threshold)
	m.tabs.Content().Chat.AddMessage("🗜️  Auto-compacting conversation history (low on context)...")

	ctx := context.Background()
	if _, err := m.shogunate.CompactSession(ctx, m.currentTabTarget(), compactPrompt); err != nil {
		slog.Warn("auto-compaction failed", "error", err)
		m.tabs.Content().Chat.AddMessage(fmt.Sprintf("⚠️  Auto-compaction failed: %v", err))
		return
	}
	newState, ok := m.currentSessionState()
	if !ok {
		return
	}
	newInfo := newState.ContextInfo
	m.tabs.Content().Chat.AddMessage(fmt.Sprintf("✅ Conversation compacted! Context usage: %s/%s tokens (%.1f%%)",
		formatTokenCount(newInfo.UsedTokens),
		formatTokenCount(newInfo.TotalTokens),
		percentage(newInfo.UsedTokens, newInfo.TotalTokens)))
	slog.Info("auto-compaction completed", "old_used", info.UsedTokens, "new_used", newInfo.UsedTokens, "saved", info.UsedTokens-newInfo.UsedTokens)
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

// switchModel asks the Shogunate to rebuild its Bifrost client with the
// current config. In daemon mode this happens over the wire; in
// single-process mode it happens inline.
func (m *TUIModel) switchModel() tea.Cmd {
	return func() tea.Msg {
		if m.shogunate == nil {
			return llmInitErrorMsg{err: fmt.Errorf("shogunate not initialised")}
		}
		slog.Info("switching LLM model", "provider", m.config.LLM.Provider, "model", m.config.LLM.Model)
		if err := m.shogunate.ConfigureLLM(context.Background(), m.llmRequest()); err != nil {
			return llmInitErrorMsg{err: err}
		}
		slog.Info("LLM model switched successfully")
		return llmInitSuccessMsg{}
	}
}

// llmRequest builds the wire-safe config used by ConfigureLLM calls.
func (m *TUIModel) llmRequest() shogunate.ConfigureLLMRequest {
	return shogunate.ConfigureLLMRequest{
		Provider:                 m.config.LLM.Provider,
		Model:                    m.config.LLM.Model,
		BaseURL:                  m.config.LLM.BaseURL,
		MaxTurns:                 m.config.LLM.MaxTurns,
		MaxThinkingTokens:        m.config.LLM.MaxThinkingTokens,
		RequestTimeoutSeconds:    m.config.LLM.RequestTimeoutSeconds,
		StreamIdleTimeoutSeconds: m.config.LLM.StreamIdleTimeoutSeconds,
		MaxRetries:               m.config.LLM.MaxRetries,
		ProjectRoot:              m.config.Storage.DatabasePath,
		AgentsFile:               m.config.Session.AgentsFile,
	}
}

func (m *TUIModel) saveSession() {
	if m.sessionStore == nil || !m.config.Session.Enabled || !m.config.Session.AutoSave {
		return
	}
	if m.shogunate == nil {
		return
	}

	tab := m.tabs.ActiveTab()
	if tab == nil {
		return
	}

	minister := m.shogunate.GetMinister(tab.Target)
	if minister == nil {
		return
	}

	session := minister.GetSession()
	if session == nil {
		return
	}

	m.sessionStore.SaveSession(session)
	slog.Debug("session auto-saved", "tab", tab.Target)
}

// shutdown performs graceful shutdown of the TUI, ensuring all pending saves complete
func (m *TUIModel) shutdown() {
	// Save the current session before closing
	m.saveSession()

	if m.sessionStore != nil {
		m.sessionStore.Close()
	}
}

// Init implements bubbletea.Model. It asks the shogunate to build its
// Bifrost client (daemon-side in daemon mode, inline otherwise).
func (m TUIModel) Init() tea.Cmd {
	tick := tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
	return tea.Batch(func() tea.Msg {
		if m.shogunate == nil {
			return llmInitErrorMsg{err: fmt.Errorf("shogunate not initialised")}
		}
		slog.Info("connecting to LLM", "provider", m.config.LLM.Provider)
		if err := m.shogunate.ConfigureLLM(context.Background(), m.llmRequest()); err != nil {
			return llmInitErrorMsg{err: err}
		}
		slog.Info("LLM client connected")
		return llmInitSuccessMsg{}
	}, tick)
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
		if msg.Type == tea.MouseWheelUp && m.Mode != "scroll" && m.tabs.Content().GetActiveView() == ViewChat {
			// Only enter scroll mode if we're not already at the top
			if !m.tabs.Content().Chat.Viewport.AtTop() {
				// First, let the content handle the scroll
				contentCmd := m.tabs.UpdateContent(msg)
				// Then enter scroll mode
				enterScrollCmd := func() tea.Msg { return ChangeModeMsg{NewMode: "scroll"} }
				return m, tea.Batch(contentCmd, enterScrollCmd)
			}
		}
		contentCmd := m.tabs.UpdateContent(msg)
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

	// Handle Ctrl+Z for background mode
	if keyStr == "ctrl+z" {
		return m.handleCtrlZ()
	}

	// Handle command line input when in command mode or yes/no mode - MUST be before other handlers
	if m.commandLine.IsInCommandMode() || m.commandLine.IsInYesNoMode() || m.commandLine.IsInInputMode() {
		cmd, handled := m.commandLine.HandleKey(msg)
		if handled {
			// Component handled the key
			return m, cmd
		}
		return m, nil
	}

	// Handle non-chat views (help, models, resume)
	if m.tabs.Content().GetActiveView() != ViewChat {
		// Allow `:` to enter command line mode even in non-chat views
		if keyStr == ":" {
			m.prompt().Blur()
			return m, m.commandLine.EnterCommandMode("")
		}
		cmd = m.tabs.UpdateContent(msg)
		// If view switched back to chat, restore focus to prompt
		if m.tabs.Content().GetActiveView() == ViewChat {
			m.prompt().Focus()
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
		m.prompt().SetValue("")
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
	if keyStr == "esc" && m.prompt().IsViNormalMode() {
		m.prompt().EnterViInsertMode()
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
		if m.prompt().Value() == "" {
			return m.handleColonKey(msg)
		}
		// Otherwise, just insert the colon character
		var cmd tea.Cmd
		*m.prompt(), cmd = m.prompt().Update(msg)
		return m, cmd
	case "@":
		return m.handleAtKey(msg)
	default:
		var cmd tea.Cmd
		*m.prompt(), cmd = m.prompt().Update(msg)
		return m, cmd
	}

}

// handleToggleRawMode toggles between chat and raw session view
func (m TUIModel) handleToggleRawMode() (tea.Model, tea.Cmd) {
	m.rawMode = !m.rawMode
	return m, nil
}

func (m TUIModel) enterScrollMode() (tea.Model, tea.Cmd) {
	if m.Mode == "scroll" || m.tabs.Content().GetActiveView() != ViewChat {
		return m, nil
	}
	chat := m.tabs.Content().Chat
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
	m.prompt().Focus()
	return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
}

func (m TUIModel) handleScrollModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.Mode != "scroll" {
		return m, nil, false
	}
	chat := m.tabs.Content().Chat

	// Handle pending 'g' prefix in scroll mode
	if m.tabs.IsPendingG() {
		m.status.ViPendingOp = ""
		m.tabs.HandlePendingG(msg.String(), func() { chat.ScrollToTop() })
		return m, nil, true
	}

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
	case "g":
		// Start pending g-prefix for gt/gT/gg
		m.tabs.SetPendingG()
		m.status.ViPendingOp = "g"
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

	// Handle pending 'g' prefix for tab navigation (gt/gT)
	if m.tabs.IsPendingG() {
		m.status.ViPendingOp = ""
		scrollToTop := func() {
			if m.tabs.Content().GetActiveView() == ViewChat {
				m.tabs.Content().Chat.ScrollToTop()
			}
		}
		m.tabs.HandlePendingG(key, scrollToTop)
		return m, nil
	}

	// Handle history navigation with arrow keys first
	// When prompt is empty, always try history navigation
	promptEmpty := m.prompt().Value() == ""
	switch key {
	case "up", "k":
		// Handle history navigation if prompt is empty or we're on the first line
		if promptEmpty || m.prompt().TextArea.Line() == 0 {
			if handled := m.handleHistoryNavigation(-1); handled {
				return m, nil
			}
		}
		// If not handled by history, pass to textarea for navigation
		var cmd tea.Cmd
		*m.prompt(), cmd = m.prompt().Update(msg)
		return m, cmd
	case "down", "j":
		// Handle history navigation if prompt is empty or we're on the last line
		lineCount := m.prompt().TextArea.LineCount()
		if promptEmpty || lineCount == 0 || m.prompt().TextArea.Line() == lineCount-1 {
			if handled := m.handleHistoryNavigation(1); handled {
				return m, nil
			}
		}
		// If not handled by history, pass to textarea for navigation
		var cmd tea.Cmd
		*m.prompt(), cmd = m.prompt().Update(msg)
		return m, cmd
	case "enter":
		// Only submit from actual normal mode to avoid interfering with visual selections
		if m.Mode != "normal" {
			break
		}
		if m.prompt().Value() == "" {
			return m, nil
		}
		return m.handleEnterKey()
	}

	// Handle mode switching keys
	switch key {
	case "i":
		// Enter insert mode at cursor
		m.prompt().EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "I":
		// Enter insert mode at beginning of line
		m.prompt().TextArea.CursorStart()
		m.prompt().EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "a":
		// Enter insert mode after cursor (move cursor forward first)
		// Note: In vi, 'a' appends after the current character
		m.prompt().EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "A":
		// Enter insert mode at end of line
		m.prompt().TextArea.CursorEnd()
		m.prompt().EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "o":
		// Open new line below and enter insert mode
		m.prompt().TextArea.CursorEnd()
		m.prompt().TextArea.InsertString("\n")
		m.prompt().EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case "O":
		// Open new line above and enter insert mode
		m.prompt().TextArea.CursorStart()
		m.prompt().TextArea.InsertString("\n")
		m.prompt().TextArea.CursorUp()
		m.prompt().EnterViInsertMode()
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
	case ":":
		// Enter command mode in the command line (bottom of screen)
		enterCmd := m.commandLine.EnterCommandMode("")
		m.prompt().Blur()
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
		helpText += "  gt      - Next tab\n"
		helpText += "  gT      - Previous tab\n"
		helpText += "  gg      - Scroll to top\n"
		helpText += "  ?       - Show this help\n\n"
		m.modal = NewBaseModal("Shortcuts Help", helpText, 80, 30)
		return m, nil
	case "#":
		// Enter learning mode
		m.prompt().EnterViLearningMode()
		m.prompt().SetValue("#")
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "learning"} }
	case "r":
		// Enter replace mode - next character will replace char under cursor
		if m.prompt().Value() != "" {
			return m, func() tea.Msg { return ChangeModeMsg{NewMode: "replace"} }
		}
		return m, nil
	case "g":
		// Start pending g-prefix for tab navigation (gt/gT) and gg
		m.tabs.SetPendingG()
		m.status.ViPendingOp = "g"
		return m, nil
	default:
		// Pass other keys to the textarea for navigation
		var cmd tea.Cmd
		*m.prompt(), cmd = m.prompt().Update(msg)
		return m, cmd
	}
}

// handleViReplaceMode handles the replace mode where next character replaces char under cursor
func (m TUIModel) handleViReplaceMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Only handle printable runes
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		replacement := msg.Runes[0]

		// Get current value and cursor position
		value := m.prompt().Value()
		lineInfo := m.prompt().TextArea.LineInfo()
		row := m.prompt().TextArea.Line()
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
				m.prompt().SetValue(newValue)

				// Restore cursor position (SetValue resets it)
				m.prompt().TextArea.SetCursor(0)
				currentRow := m.prompt().TextArea.Line()
				for currentRow < row {
					m.prompt().TextArea.CursorDown()
					currentRow = m.prompt().TextArea.Line()
				}
				m.prompt().TextArea.SetCursor(col)
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
		content := m.prompt().Value()
		if strings.HasPrefix(content, ":") {
			// Parse the command (keep the : prefix for display)
			parts := strings.Fields(content)
			if len(parts) > 0 {
				cmdName := parts[0]
				m.tabs.Content().Chat.AddToRawHistory("COMMAND", content)
				cmd, exists := m.commandRegistry.GetCommand(cmdName)
				if exists {
					command := cmd.Handler(&m, parts[1:])
					m.prompt().SetValue("")
					m.prompt().EnterViInsertMode() // Return to insert mode after command
					// Hide completion dialog
					m.showCompletionDialog = false
					m.completions.Hide()
					m.completionMode = ""
					return m, command
				}
				m.commandLine.AddToast(fmt.Sprintf("Unknown command: %s", cmdName), "error", time.Second*3)
				m.prompt().SetValue("")
				m.prompt().EnterViInsertMode()
				// Hide completion dialog
				m.showCompletionDialog = false
				m.completions.Hide()
				m.completionMode = ""
				return m, nil
			}
		}
		// If no command, just return to insert mode
		m.prompt().SetValue("")
		m.prompt().EnterViInsertMode()
		// Hide completion dialog
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""
		return m, nil
	default:
		// Pass other keys to the textarea for editing the command
		var cmd tea.Cmd
		*m.prompt(), cmd = m.prompt().Update(msg)
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
//
// handleCtrlC: single press stops streaming, double press quits.
// Debounce ignores duplicate events arriving within CtrlCDebounceTime (terminals
// and iOS sometimes send multiple KeyCtrlC for a single physical press).
func (m TUIModel) handleCtrlC() (tea.Model, tea.Cmd) {
	now := time.Now()
	debounce := m.config.UI.CtrlCDebounceTime
	windowTime := m.config.UI.CtrlCWindowTime

	// Ignore duplicate events within debounce window
	if !m.ctrlCLastPress.IsZero() && now.Sub(m.ctrlCLastPress) < debounce {
		return m, nil
	}

	// Double press within window — quit
	if !m.ctrlCLastPress.IsZero() && now.Sub(m.ctrlCLastPress) <= windowTime {
		slog.Info("CTRL-C: double press, quitting")
		m.stopStreaming()
		m.shutdown()
		return m, tea.Quit
	}

	// Single press — cancel streaming on active tab only.
	// The daemon emits the ABORTED chat line on StreamInterruptedMsg once
	// the session actually stops; the toast below confirms the keystroke.
	m.ctrlCLastPress = now
	activeTab := m.tabs.ActiveTab()
	if activeTab.Streaming {
		slog.Info("ctrl_c_during_streaming", "cancelling_active_tab", activeTab.Target)
		m.stopStreamingTab(activeTab.Target)
	}

	m.commandLine.AddToast(
		fmt.Sprintf("Press CTRL-C again within %.1fs to exit", windowTime.Seconds()),
		"info",
		windowTime,
	)
	return m, nil
}

// handleEscape handles the escape key and the first ctrl-c
func (m TUIModel) handleEscape() (tea.Model, tea.Cmd) {
	activeTab := m.tabs.ActiveTab()
	if activeTab.Streaming {
		slog.Info("escape_during_streaming", "cancelling_active_tab", activeTab.Target)
		m.stopStreamingTab(activeTab.Target)
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
		*m.prompt(), cmd = m.prompt().Update(msg)
		if m.completionMode == "file" {
			files, err := utils.GetFileTree(".")
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
			} else if m.shogunate != nil {
				if err := m.shogunate.AddSessionContextFile(m.currentTabTarget(), filePath, string(content)); err == nil {
					m.tabs.Content().Chat.AddMessage(fmt.Sprintf("Loaded file: %s", filePath))
				}
			}
			currentValue := m.prompt().Value()
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
				m.prompt().SetValue(strings.TrimSpace(newValue) + " ")
			} else {
				// Fallback, though we should always find an @
				m.prompt().SetValue("@" + selected + " ")
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
			m.prompt().SetValue("")
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
	return nil
}

func (m *TUIModel) stopWaitingForResponse() {
	if !m.waitingForResponse {
		return
	}
	m.waitingForResponse = false
	m.status.StopWaiting()
	m.status.ResetStreamRate()
}

// submitToShogunate sends a prompt to the appropriate minister based on active tab
// and returns a command that listens for streaming responses.
func (m *TUIModel) submitToShogunate(ctx context.Context, prompt string, contextFiles map[string]string) tea.Cmd {
	if m.shogunate == nil {
		return func() tea.Msg {
			return shogunate.StreamErrorMsg{Err: fmt.Errorf("Shogunate not initialized")}
		}
	}

	tab := m.tabs.ActiveTab()

	p := &shogunate.Prompt{
		Ctx:          ctx,
		Message:      prompt,
		EdictKey:     m.currentEdictKey,
		ChannelID:    tab.Target,
		ContextFiles: contextFiles,
	}

	if err := m.shogunate.SubmitPrompt(tab.Target, p); err != nil {
		return func() tea.Msg {
			return shogunate.StreamErrorMsg{Err: err}
		}
	}

	// Mark the target tab as streaming after successful submission
	m.tabs.SetStreamingTabByTab(tab.Target)

	return nil
}

func (m *TUIModel) saveHistoryPresentState() {
	if m.historySaved {
		return
	}
	m.historyPendingPrompt = m.prompt().Value()
	if state, ok := m.currentSessionState(); ok {
		m.historyPresentSessionSnapshot = state.MessageSnapshot
	} else {
		m.historyPresentSessionSnapshot = 0
	}
	m.historyPresentChatSnapshot = len(m.tabs.Content().Chat.Messages)
	m.historySaved = true
}

func (m *TUIModel) applyHistoryEntry(entry promptHistoryEntry) {
	// Only set the prompt value, don't rollback session/chat yet
	// That will happen when user presses Enter
	m.prompt().SetValue(entry.Prompt)
	m.prompt().TextArea.CursorEnd()
}

func (m *TUIModel) restoreHistoryPresent() {
	// Only restore the prompt value, don't rollback session/chat yet
	// That will happen when user presses Enter
	if m.historySaved {
		m.prompt().SetValue(m.historyPendingPrompt)
		m.prompt().TextArea.CursorEnd()
		m.historySaved = false
		return
	}

	m.prompt().SetValue(m.historyPendingPrompt)
	m.prompt().TextArea.CursorEnd()
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

	content := m.prompt().Value()
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
					m.tabs.Content().Chat.AddMessage(fmt.Sprintf("📝 Learning added: %s", learningNote))
					m.sessionActive = true
				}
			}
		}
		// Return to normal mode
		m.prompt().EnterViNormalMode()
		m.prompt().SetValue("")
		return m, func() tea.Msg { return ChangeModeMsg{NewMode: "normal"} }
	}

	if strings.HasPrefix(content, ":") {
		// Parse the command (keep the : prefix for display)
		parts := strings.Fields(content)
		if len(parts) > 0 {
			cmdName := parts[0]
			m.tabs.Content().Chat.AddToRawHistory("COMMAND", content)
			cmd, exists := m.commandRegistry.GetCommand(cmdName)
			if exists {
				command := cmd.Handler(&m, parts[1:])
				cmds = append(cmds, command)
				m.prompt().SetValue("")
				m.prompt().EnterViInsertMode()
				cmds = append(cmds, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} })
				// Ensure prompt has focus after command
				m.prompt().Focus()
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
			m.stopStreamingTab(m.tabs.ActiveTab().Target)
			if m.shogunate != nil {
				_ = m.shogunate.RollbackSession(m.currentTabTarget(), entry.SessionSnapshot)
			}
			m.tabs.Content().Chat.TruncateTo(entry.ChatSnapshot)
			m.tabs.Content().Chat.ClearToolCallMessageIndex()

			// Now continue with the normal flow from this rolled-back state
			m.historySaved = false
		}

		// Add user input to raw history
		m.tabs.Content().Chat.AddToRawHistory("USER", content)
		chatSnapshot := len(m.tabs.Content().Chat.Messages)
		state, hasSession := m.currentSessionState()
		var sessionSnapshot int
		if hasSession {
			sessionSnapshot = state.MessageSnapshot
		}
		if m.historyCursor < len(m.sessionPromptHistory) {
			m.sessionPromptHistory = m.sessionPromptHistory[:m.historyCursor]
		}
		m.tabs.Content().Chat.AddUserMessage(content)
		if m.shogunate != nil {
			// Check if we need to auto-compact before sending the prompt (#54)
			if hasSession {
				m.autoCompactIfNeeded(state)
			}

			m.sessionActive = true
			m.prompt().SetValue("")
			// In vi mode, stay in insert mode for continued conversation
			if waitCmd := m.startWaitingForResponse(); waitCmd != nil {
				cmds = append(cmds, waitCmd)
			}
			ctx := m.tabs.ActiveTab().Ctx

			// Get context files from session (populated via @ references)
			var contextFiles map[string]string
			if hasSession {
				contextFiles = state.ContextFiles
			}
			shogunateCmd := m.submitToShogunate(ctx, content, contextFiles)
			cmds = append(cmds, shogunateCmd)
		} else {
			m.commandLine.AddToast("No model configured, use :models to configure a model", "error", time.Second*5)
			m.prompt().SetValue("")
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
	if m.prompt().Value() == "" {
		*m.prompt(), _ = m.prompt().Update(msg)
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
		*m.prompt(), _ = m.prompt().Update(msg)
	}
	return m, nil
}

// handleColonKey handles the colon key - enters command mode in command line
func (m TUIModel) handleColonKey(_ tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Enter command mode in the command line
	enterCmd := m.commandLine.EnterCommandMode("")
	m.prompt().Blur()

	// Show command completions immediately
	m.updateCommandLineCompletions()

	return m, enterCmd
}

// handleAtKey handles the @ key for file completion
func (m TUIModel) handleAtKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	*m.prompt(), _ = m.prompt().Update(msg)
	// Show completion dialog with files
	m.showCompletionDialog = true
	m.completionMode = "file"
	files, err := utils.GetFileTree(".")
	if err != nil {
		m.tabs.Content().Chat.AddMessage(fmt.Sprintf("Error scanning files: %v", err))
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
		m.prompt().Focus()
		return m, nil
	}

	m.tabs.Content().Chat.AddToRawHistory("SHELL_COMMAND", shellCmd)

	// Make session active so chat is visible (not welcome screen)
	m.sessionActive = true

	// Display the command in chat similar to a shell prompt
	m.tabs.Content().Chat.AddShellCommandInput(shellCmd)

	// Execute the shell command using the current shell runner (sandbox or host)
	return m, func() tea.Msg {
		ctx := context.Background()

		// User-initiated commands never need approval
		params := runners.Input{
			Command:        shellCmd,
			Description:    "User shell command",
			BypassApproval: true, // User explicitly requested this command
		}

		output, err := m.shogunate.RunShellCommand(ctx, params)

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
			m.stopStreamingTab(m.tabs.ActiveTab().Target)
			if m.shogunate != nil {
				_ = m.shogunate.RollbackSession(m.currentTabTarget(), entry.SessionSnapshot)
			}
			m.tabs.Content().Chat.TruncateTo(entry.ChatSnapshot)
			m.tabs.Content().Chat.ClearToolCallMessageIndex()
			m.historySaved = false
		}

		m.tabs.Content().Chat.AddToRawHistory("USER", content)
		chatSnapshot := len(m.tabs.Content().Chat.Messages)
		state, hasSession := m.currentSessionState()
		var sessionSnapshot int
		if hasSession {
			sessionSnapshot = state.MessageSnapshot
		}
		if m.historyCursor < len(m.sessionPromptHistory) {
			m.sessionPromptHistory = m.sessionPromptHistory[:m.historyCursor]
		}
		m.tabs.Content().Chat.AddUserMessage(content)
		if m.shogunate != nil {
			if hasSession {
				m.autoCompactIfNeeded(state)
			}

			m.sessionActive = true
			if waitCmd := m.startWaitingForResponse(); waitCmd != nil {
				cmds = append(cmds, waitCmd)
			}
			ctx := m.tabs.ActiveTab().Ctx

			// Get context files from session (populated via @ references)
			var contextFiles map[string]string
			if hasSession {
				contextFiles = state.ContextFiles
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

	case runners.ToolCallScheduledMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("TOOL_SCHEDULED", fmt.Sprintf("%s with input: %s", msg.ToolName, msg.Input))
		chat.HandleToolCallScheduled(msg)

	case runners.ToolCallExecutingMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("TOOL_EXECUTING", fmt.Sprintf("%s with input: %s", msg.ToolName, msg.Input))
		chat.HandleToolCallExecuting(msg)

	case runners.ToolCallSuccessMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("TOOL_SUCCESS", fmt.Sprintf("%s\nInput: %s\nOutput: %s", msg.ToolName, msg.Input, msg.Result))
		chat.HandleToolCallSuccess(msg)
		m.repoInfo.RefreshDiff()

	case runners.ToolCallErrorMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("TOOL_ERROR", fmt.Sprintf("%s\nInput: %s\nError: %v", msg.ToolName, msg.Input, msg.Error))
		chat.HandleToolCallError(msg)

	case runners.ToolCallAbortedMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("TOOL_ABORTED", fmt.Sprintf("%s\nInput: %s\nReason: %s", msg.ToolName, msg.Input, msg.Reason))
		chat.HandleToolCallAborted(msg)

	case errMsg:
		chat := m.tabs.Content().Chat
		chat.AddToRawHistory("ERROR", fmt.Sprintf("%v", msg.err))
		chat.AddMessage(fmt.Sprintf("Error: %v", msg.err))

	case shogunate.StreamStartMsg:
		// Streaming has started — capture edict ID for multi-turn
		m.tabs.SetStreamingTabByTab(msg.ChannelID)
		if msg.EdictID != 0 {
			m.currentEdictKey = m.shogunate.EdictKey(msg.EdictID)
			m.tabs.SetActiveEdictID(msg.EdictID)
		}
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("STREAM_START", "AI streaming response started")
		slog.Debug("streamStartMsg", "starting_stream", true, "edict_id", msg.EdictID)
		m.status.ClearError() // Clear any previous error state

	case shogunate.StreamCompleteMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("STREAM_COMPLETE", "AI streaming response completed")
		slog.Debug("streamCompleteMsg", "messages_count", len(chat.Messages))
		m.tabs.ClearStreamingByTab(msg.ChannelID)
		if !m.tabs.AnyStreaming() {
			m.stopWaitingForResponse()
		}

		// Finalize the last AI message with success/failure prefix
		isFailure := chat.FinalizeLastAIMessage()
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
		// Session already stopped — just reset UI state, don't re-cancel ctx.
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("STREAM_INTERRUPTED", fmt.Sprintf("AI streaming interrupted, partial content: %s", msg.PartialContent))
		chat.AddMessage(systemPrefix + "ABORTED")
		slog.Debug("streamInterruptedMsg", "partial_content_length", len(msg.PartialContent))
		m.tabs.ClearStreamingByTab(msg.ChannelID)
		if !m.tabs.AnyStreaming() {
			m.stopWaitingForResponse()
		}
		m.streamCompleteCallback = nil // Clear callback on interrupt
		m.repoInfo.RefreshDiff()

	case shogunate.StreamErrorMsg:
		fullError := fmt.Sprintf("Model Error: %v", msg.Err)
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("STREAM_ERROR", fmt.Sprintf("AI streaming error: %v", msg.Err))
		slog.Error("shogunate.StreamErrorMsg", "error", msg.Err)
		// Add full error message to chat for visibility
		chat.AddMessage(fmt.Sprintf("\n%s❌ %s", systemPrefix, fullError))
		// Toast will be automatically truncated by commandline component if needed
		m.commandLine.AddToast(fullError, "error", time.Second*5)
		m.status.SetError() // Update status icon to show error
		// Session already stopped — just reset UI state, don't re-cancel ctx.
		m.clearStreamingTab(msg.ChannelID)
		m.repoInfo.RefreshDiff()

		/* TODO: Add the message bellow
		case streamMaxTurnsExceededMsg:
			// Max turns exceeded, mark session as inactive and show warning
			m.tabs.Content().Chat.AddToRawHistory("STREAM_MAX_TURNS_EXCEEDED", fmt.Sprintf("AI streaming ended after reaching max turns limit: %d", msg.maxTurns))
			slog.Warn("streamMaxTurnsExceededMsg", "max_turns", msg.maxTurns)
			m.tabs.Content().Chat.AddMessage(fmt.Sprintf("\n⚠️  Conversation ended after reaching maximum turn limit (%d turns)", msg.maxTurns))
			m.stopStreaming()
			m.streamCompleteCallback = nil // Clear callback on max turns
			m.repoInfo.RefreshDiff()
		*/
	case shogunate.StreamMaxTokensReachedMsg:
		// Max tokens reached, mark session as inactive and show warning
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("STREAM_MAX_TOKENS_REACHED", fmt.Sprintf("AI response truncated due to length limit: %s", msg.Content))
		slog.Warn("streamMaxTokensReachedMsg", "content_length", len(msg.Content))
		chat.AddMessage("\n\n⚠️  Response truncated due to length limit")
		// Session already stopped — just reset UI state, don't re-cancel ctx.
		m.clearStreamingTab(msg.ChannelID)
		m.streamCompleteCallback = nil // Clear callback on max tokens
		m.repoInfo.RefreshDiff()

	// Shogunate streaming message handlers
	case shogunate.StreamChunkMsg:
		// Drop chunks for tabs the user already cancelled; daemon may still
		// emit a few in-flight chunks before its ctx-cancel takes effect.
		if tab := m.tabs.TabByTarget(msg.ChannelID); tab != nil && !tab.Streaming {
			return m, nil
		}
		// Handle text chunks from Shogunate — route to correct tab
		chat := m.tabs.ChatByTab(msg.ChannelID)
		if msg.Reasoning != "" {
			chat.AddToRawHistory("SHOGUNATE_REASONING", msg.Reasoning)
			chat.AddThinkingChunk(msg.Reasoning)
		}
		if msg.Text != "" {
			chat.AddToRawHistory("SHOGUNATE_TEXT", msg.Text)
			chat.AddAIChunk(msg.Text)
		}
		m.status.AddStreamChars(len(msg.Text) + len(msg.Reasoning))
		m.status.ContextPercent = msg.PercentContextUsed
		var cmds []tea.Cmd
		// Schedule the debounce tick if dirty and none pending
		if chat.contentDirty && !m.renderTickPending {
			m.renderTickPending = true
			cmds = append(cmds, tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return chatRenderTickMsg{} }))
		}
		if m.tabs.AnyStreaming() {
			m.waitingStart = time.Now()
			if !m.waitingForResponse {
				cmds = append(cmds, m.startWaitingForResponse())
			}
		}
		return m, tea.Batch(cmds...)

	case chatRenderTickMsg:
		// Debounce tick fired — flush every dirty chat. Chunks can land on a
		// non-active tab, so flushing only Content().Chat misses them.
		m.renderTickPending = false
		m.tabs.FlushDirtyChats()

	case shogunate.MinisterInvokingMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("MINISTER_INVOKING",
			fmt.Sprintf("Minister %s invoked for edict %d", msg.MinisterID, msg.EdictKey.ID))
		taskPreview := msg.Task
		if len(taskPreview) > 60 {
			taskPreview = taskPreview[:57] + "..."
		}
		chat.AddMessage(fmt.Sprintf("%s:  %s", msg.MinisterID, taskPreview))
		chat.Indent++
		return m, nil

	case shogunate.MinisterCompletedMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		if msg.Error != nil {
			chat.AddToRawHistory("MINISTER_FAILED",
				fmt.Sprintf("Minister %s failed: %v", msg.MinisterID, msg.Error))
			chat.AddMessage(fmt.Sprintf("%s%s %s failed: %v", systemPrefix, completeFailurePrefix, msg.MinisterID, msg.Error))
		} else {
			chat.AddToRawHistory("MINISTER_COMPLETED",
				fmt.Sprintf("Minister %s completed", msg.MinisterID))
			chat.AddMessage(fmt.Sprintf("%s%s %s completed", systemPrefix, checkPrefix, msg.MinisterID))
		}
		if chat.Indent > 0 {
			chat.Indent--
		}
		return m, nil

	case shogunate.RitualStepMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("RITUAL_STEP",
			fmt.Sprintf("Ritual %s step %s [%d/%d] %s",
				msg.RitualName, msg.StepName, msg.StepIndex+1, msg.TotalSteps, msg.Status))
		switch msg.Status {
		// TODO: remove this and use a different Msg type
		case "info":
			chat.AddMessage(fmt.Sprintf("  %s", msg.Message))
		case "started":
			text := ""
			if msg.StepName == "" {
				text = fmt.Sprintf("%sRitual %s started",
					ritualPrefix, msg.RitualName)
				if msg.EdictID != 0 {
					text = fmt.Sprintf("%s for edict %d",
						text, msg.EdictID)
				}
				// Mark the tab as streaming so CTRL-C can cancel the ritual
				m.tabs.SetStreamingTabByTab(msg.ChannelID)
			} else {
				if msg.TotalSteps == 1 {
					text = fmt.Sprintf("single step: %s", msg.StepName)
				} else {
					text = fmt.Sprintf("step: %d/%d: %s",
						msg.StepIndex+1, msg.TotalSteps, msg.StepName)
					if msg.Message != "" {
						text = text + " - " + msg.Message
					}
				}
				// Re-set streaming flag for each step, since a prior step's
				// StreamCompleteMsg clears it and would cause chunks to drop.
				m.tabs.SetStreamingTabByTab(msg.ChannelID)
			}
			chat.AddMessage(text)
			if msg.StepName == "" {
				chat.Indent++
			}
		/* TODO: move completed to the beigning of the last message
		case "completed":
			chat.AppendToLastMessage(" - " + checkPrefix)
		*/
		case "failed":
			chat.AppendToLastMessage(" - X")
		case "aborted":
			chat.AppendToLastMessage(" ABORTED")
		case "retrying":
			chat.AppendToLastMessage(" retrying")
		case "cmd_running":
			chat.AddMessage(fmt.Sprintf("%s Running: %s", cmdRunningPrefix, msg.Message))
		case "cmd_done":
			chat.AddMessage(fmt.Sprintf("%s %s done", cmdDonePrefix, msg.Message))
		case "ritual_completed":
			text := fmt.Sprintf("%sRitual %s for edict %d completed in %s",
				ritualPrefix, msg.RitualName, msg.EdictID, msg.Message)
			if msg.EdictID == 1 {
				text = fmt.Sprintf("%sRitual %s for the court completed in %s",
					ritualPrefix, msg.RitualName, msg.Message)
			}
			chat.AddMessage(text)
			chat.Indent--
		case "ritual_failed":
			chat.AddMessage(fmt.Sprintf("%s Ritual %s failed: %s", completeFailurePrefix, msg.RitualName, msg.Message))
			if chat.Indent > 0 {
				chat.Indent--
			}
		}
		return m, nil

	case shogunate.EventNotificationMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddToRawHistory("EVENT_NOTIFICATION",
			fmt.Sprintf("Event %s for edict %d: %s", msg.EventType, msg.EdictKey.ID, msg.Message))

		// Use appropriate icon based on event type
		icon := "📋"   // Default
		message := "" // What about msg.Message ?
		switch msg.EventType {
		case storage.EventShogunateStarted:
			icon = courtPrefix
			message = "Shogunate dawn at " + time.Now().Format("2 January, 3:04 PM MST")
		case storage.EventShogunateReady:
			icon = courtPrefix
			message = "READY at " + time.Now().Format("2 January, 3:04 PM MST")
		case storage.EventEdictCreated:
			icon = "📜"
			intent, _ := msg.Payload["intent"].(string)
			id := msg.EdictKey.ID
			if intent == "" {
				intent = "New edict"
			}
			// Truncate long intents for display
			// TODO: Replace it with and edict's summary
			if len(intent) > 60 {
				intent = intent[:57] + "..."
			}
			message = fmt.Sprintf("Edict %d Created:\n    %s", id, intent)
		case storage.EventEdictSealed:
			icon = "✅"
			message = fmt.Sprintf("Edict %d sealed and ascended to Heaven", msg.EdictKey.ID)
		case storage.EventSealGranted:
			icon = sealPrefix
			minister, _ := msg.Payload["minister_id"].(string)
			if minister == "" {
				minister = "Unknown"
			}
			if minister == "ruler" {
				message = fmt.Sprintf("Ruler sealed edict %d", msg.EdictKey.ID)
			} else {
				message = fmt.Sprintf("Minister %s sealed edict %d", minister, msg.EdictKey.ID)
			}
			// Re-query seals to show fresh seal chain with Ruler's seal
			updatedSeals, err := m.shogunate.GetEdictSeals(msg.EdictKey)
			if err != nil {
				message += fmt.Sprintf("\n  (failed to refresh seal chain: %v)", err)
			} else {
				message += fmt.Sprintf("\n  %s", renderSealChain(updatedSeals, 60))
			}
		case storage.EventManifestCommitted:
			icon = "🔨"
			message = fmt.Sprintf("Forge committed manifest for edict %d", msg.EdictKey.ID)
		case storage.EventZhengmingNeeded:
			icon = "❓"
			summary, _ := msg.Payload["summary"].(string)
			if summary == "" {
				summary = "clarification needed"
			}
			message = fmt.Sprintf("Zhengming requested for edict %d: %s", msg.EdictKey.ID, summary)
		case storage.EventZhengmingAnswered:
			icon = "💬"
			if msg.EdictKey.ID == 0 {
				message = "Zhengming answered for the court"
			} else {
				message = fmt.Sprintf("Zhengming answered for edict %d", msg.EdictKey.ID)
			}
		case storage.EventEdictCancelled:
			icon = "⛔"
			message = fmt.Sprintf("Edict %d cancelled", msg.EdictKey.ID)
		}

		if message != "" {
			// TODO: refactor to the bubbletea way and return a function with the next line
			chat.AddMessage(fmt.Sprintf("%s %s", icon, message))
		}
		return m, nil

	case shogunate.StreamDoneMsg:
		return m, nil

	case shogunate.EventsDrainedMsg:
		chat := m.tabs.ChatByTab(msg.ChannelID)
		chat.AddMessage(fmt.Sprintf("%sRecovered %d event(s) from previous session:", systemPrefix, len(msg.Events)))
		for _, ev := range msg.Events {
			detail := fmt.Sprintf("%s  %s", systemPrefix, ev.EventType)
			if ev.EdictKey.ID != 0 {
				detail += fmt.Sprintf(" [edict:%d]", ev.EdictKey.ID)
			}
			chat.AddMessage(detail)
		}
		return m, nil

	case shogunate.ZhengmingPendingMsg:
		// Route to the correct tab's prompt by matching MinisterID to tab target
		if tab := m.tabs.TabByTarget(msg.MinisterID); tab != nil {
			if p, ok := m.prompts[tab.Label]; ok {
				p.HandleZhengmingPending(msg)
			}
		}
		return m, nil

	case AnsweredMsg:
		m.prompt().ExitAnsweringMode()
		go m.handleAnsweringComplete(msg)
		return m, nil

	case AnsweringCancelMsg:
		m.prompt().ExitAnsweringMode()
		go m.handleAnsweringComplete(AnsweredMsg{RequestID: msg.RequestID, Answers: []string{"[chat]"}})
		return m, nil

	case AnsweringEditMsg:
		// Open the question text in $EDITOR and update it if modified
		originalText := msg.Question
		return m, func() tea.Msg {
			// Create temp file with question content
			tmpFile, err := os.CreateTemp("", "zhengming_edit_*.md")
			if err != nil {
				slog.Error("failed to create temp file for editing", "error", err)
				return AnsweringEditDoneMsg{RequestID: msg.RequestID, Question: originalText, OriginalQuestion: originalText}
			}
			tmpPath := tmpFile.Name()
			tmpFile.Close()

			if err := os.WriteFile(tmpPath, []byte(originalText), 0644); err != nil {
				slog.Error("failed to write temp file for editing", "error", err)
				os.Remove(tmpPath)
				return AnsweringEditDoneMsg{RequestID: msg.RequestID, Question: originalText, OriginalQuestion: originalText}
			}

			// Get editor
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}

			// Run the editor via tea.ExecProcess
			cmd := exec.Command(editor, tmpPath)
			return tea.ExecProcess(cmd, func(err error) tea.Msg {
				defer os.Remove(tmpPath)
				if err != nil {
					slog.Error("editor failed", "error", err)
					return AnsweringEditDoneMsg{RequestID: msg.RequestID, Question: originalText, OriginalQuestion: originalText}
				}

				// Read modified content
				modifiedContent, err := os.ReadFile(tmpPath)
				if err != nil {
					slog.Error("failed to read modified file", "error", err)
					return AnsweringEditDoneMsg{RequestID: msg.RequestID, Question: originalText, OriginalQuestion: originalText}
				}

				// Trim trailing whitespace for comparison
				modified := strings.TrimRight(string(modifiedContent), " \t\n\r")
				original := strings.TrimRight(originalText, " \t\n\r")
				if modified == original {
					// No changes, keep original
					return AnsweringEditDoneMsg{RequestID: msg.RequestID, Question: originalText, OriginalQuestion: originalText}
				}

				// Update the question with modified content
				return AnsweringEditDoneMsg{RequestID: msg.RequestID, Question: modified, OriginalQuestion: originalText}
			})
		}

	case AnsweringEditDoneMsg:
		// Exit answering mode
		m.prompt().ExitAnsweringMode()

		// Check if content was modified
		modified := strings.TrimRight(msg.Question, " \t\n\r")
		original := strings.TrimRight(msg.OriginalQuestion, " \t\n\r")

		if modified != original {
			// Content was modified - send it back to the Sage as a chat message
			if m.shogunate != nil {
				_ = m.shogunate.AddSessionMessage(m.currentTabTarget(), "human", msg.Question)
			}
			// Cancel the pending zhengming so the Sage can re-suggest with modified content
			if m.shogunate != nil {
				m.shogunate.CancelZhengming(msg.RequestID)
			}
		}
		return m, nil

	case tools.EditorRequest:
		// A tool wants to open content in $EDITOR — TUI owns the temp-file
		// lifecycle so the host running the editor doesn't need to share a
		// filesystem with whoever generated the content (daemon case).
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		ext := ".md"
		if msg.Filename != "" {
			if i := strings.LastIndex(msg.Filename, "."); i >= 0 {
				ext = msg.Filename[i:]
			}
		}
		f, err := os.CreateTemp("", "approve_doc_*"+ext)
		if err != nil {
			msg.ResultChan <- tools.EditorResult{Err: fmt.Errorf("create temp file: %w", err)}
			return m, nil
		}
		tmpPath := f.Name()
		f.Close()
		orig := []byte(msg.Content)
		if err := os.WriteFile(tmpPath, orig, 0644); err != nil {
			os.Remove(tmpPath)
			msg.ResultChan <- tools.EditorResult{Err: fmt.Errorf("write temp file: %w", err)}
			return m, nil
		}
		stat, err := os.Stat(tmpPath)
		if err != nil {
			os.Remove(tmpPath)
			msg.ResultChan <- tools.EditorResult{Err: fmt.Errorf("stat temp file: %w", err)}
			return m, nil
		}
		cmd := exec.Command(editor, tmpPath)
		resultChan := msg.ResultChan
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return editorResultMsg{
				ResultChan: resultChan,
				TmpPath:    tmpPath,
				OrigBytes:  orig,
				OrigMtime:  stat.ModTime(),
				Err:        err,
			}
		})

	case editorResultMsg:
		defer os.Remove(msg.TmpPath)
		if msg.Err != nil {
			msg.ResultChan <- tools.EditorResult{Err: msg.Err}
			return m, nil
		}
		stat, err := os.Stat(msg.TmpPath)
		if err != nil {
			msg.ResultChan <- tools.EditorResult{Err: fmt.Errorf("stat after edit: %w", err)}
			return m, nil
		}
		// mtime unchanged → user quit without saving (vi :q!).
		if stat.ModTime().Equal(msg.OrigMtime) {
			msg.ResultChan <- tools.EditorResult{Saved: false}
			return m, nil
		}
		content, err := os.ReadFile(msg.TmpPath)
		if err != nil {
			msg.ResultChan <- tools.EditorResult{Err: fmt.Errorf("read after edit: %w", err)}
			return m, nil
		}
		msg.ResultChan <- tools.EditorResult{Content: string(content), Saved: true}
		return m, nil

	case showHelpMsg:
		// Show the help viewer with the requested topic
		return m, m.tabs.Content().ShowHelp(msg.topic)

	case showContextMsg:
		m.tabs.Content().Chat.AddToRawHistory("CONTEXT", msg.content)
		m.tabs.Content().Chat.AddMessage(msg.content)
		m.sessionActive = true

	case updateCheckMsg:
		if msg.err != nil {
			m.tabs.Content().Chat.AddMessage(fmt.Sprintf("%s❌ Failed to check for updates: %v", systemPrefix, msg.err))
			return m, nil
		}

		if !msg.hasUpdate {
			m.tabs.Content().Chat.AddMessage(fmt.Sprintf("%s✓ You're running the latest version (%s)", systemPrefix, msg.latest))
			return m, nil
		}

		// Update available - ask for confirmation
		question := fmt.Sprintf("%sUpdate available: %s → %s. Do you want to update now?", systemPrefix, utils.AsimiVersion, msg.latest)
		return m, m.commandLine.EnterYesNoMode(question)

	case updateAvailableMsg:
		// Background update check found a new version - just set the flag
		// The home view will display the notification
		m.updateAvailable = true
		return m, nil

	case yesNoResponseMsg:
		// Check if this is a response to a seal override request
		if m.pendingSealOverride != nil {
			if msg.answer {
				// User confirmed - proceed with sealing
				return m, grantRulerSealCmd(&m, m.pendingSealOverride.edictID, m.pendingSealOverride.notes)
			} else {
				// User declined
				cancelMsg := NewChatMsgBuilder(systemPrefix)
				cancelMsg.WriteLn("Seal cancelled.")
				m.tabs.Content().Chat.AddMessage(cancelMsg.String())
			}
			m.pendingSealOverride = nil
			return m, nil
		}

		// Check if this is a response to a host command approval request
		if m.pendingHostApproval != nil {
			// Send the response back to the waiting goroutine
			m.pendingHostApproval.ResponseChan <- msg.answer
			if msg.answer {
				// User approved - update the emoji to ⚙️ (executing)
				m.tabs.Content().Chat.UpdateLastToolCallEmoji(m.pendingHostApproval.Command, "⚙️")
			} else {
				// User denied - update the emoji to ⛔︎
				m.tabs.Content().Chat.UpdateLastToolCallEmoji(m.pendingHostApproval.Command, "⛔︎")
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
		m.tabs.Content().Chat.AddMessage(cancelMsg.String())
		return m, nil

	case inputResponseMsg:
		// Handle free text input responses
		return m, handleProjectNameInput(&m, msg.text)

	case runners.ApprovalRequestMsg:
		// Store the pending approval request
		m.pendingHostApproval = &msg
		m.tabs.Content().Chat.UpdateLastToolCallEmoji(msg.Command, approvalPrefix)
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
			m.tabs.Content().Chat.AddMessage(errMsg.String())
			return m, nil
		}

		successMsg := NewChatMsgBuilder(systemPrefix)
		successMsg.WriteLn("✓ Update successful!")
		successMsg.WriteLn("Please restart asimi to use the new version.")
		m.tabs.Content().Chat.AddMessage(successMsg.String())
		return m, nil

	case tickMsg:
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })

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
		m.tabs.Content().Chat.AddToRawHistory("OAUTH_ERROR", msg.err)
		errToast := fmt.Sprintf("OAuth failed: %s", msg.err)
		m.commandLine.AddToast(errToast, "error", 4000)
		m.tabs.Content().Chat.AddMessage(errToast)
		m.sessionActive = false

	case modalCancelledMsg:
		m.providerModal = nil
		m.codeInputModal = nil
		// Return to chat view
		m.commandLine.AddToast("Cancelled", "info", 2000)
		return m, m.tabs.Content().ShowChat()

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
		return m, m.tabs.Content().ShowUnifiedModels(msg.models, m.config.LLM.Model)

	case showModelSelectionMsg:
		// Trigger model selection after login completes
		return m, handleModelsCommand(&m, nil)

	case modelsLoadErrorMsg:
		m.tabs.Content().SetModelsError(msg.error)

	case sessionsLoadedMsg:
		return m, m.tabs.Content().ShowResume(msg.sessions)

	case sealedEdictsLoadedMsg:
		return m, m.tabs.Content().ShowSealSelection(msg.edicts)

	case sealSelectedMsg:
		// Grant the ruler's seal to the selected edict
		return m, grantRulerSealCmd(&m, msg.edictID, "")

	case ChangeModeMsg:
		// Centralized mode change handling
		oldMode := m.Mode
		newMode := msg.NewMode

		// Update mode
		m.Mode = newMode
		m.status.SetMode(newMode)

		if newMode != "command" && newMode != "yesno" && newMode != "input" {
			m.commandLine.Blur()
		}

		// Handle scroll lock state changes
		if oldMode == "scroll" && newMode != "scroll" {
			m.tabs.Content().Chat.SetScrollLock(false)
		} else if oldMode != "scroll" && newMode == "scroll" {
			m.tabs.Content().Chat.SetScrollLock(true)
		}

		// Update prompt component based on new mode
		switch newMode {
		case "insert":
			m.prompt().EnterViInsertMode()
		case "normal":
			m.prompt().EnterViNormalMode()
		case "visual":
			// Visual mode uses normal keymap but different styling
			m.prompt().ViCurrentMode = ViModeVisual
			m.prompt().TextArea.KeyMap = m.prompt().viNormalKeyMap
			m.prompt().TextArea.Placeholder = "Visual selection mode"
			// Trigger style update by calling the private method via a public interface
			// For now, we'll just set it directly since we're in the same package
			if globalTheme != nil {
				m.prompt().Style = m.prompt().Style.BorderForeground(globalTheme.PromptOffBorder)
			}
		case "scroll":
			m.prompt().Blur()
			m.prompt().EnterViScrollMode()
		case "command":
			m.prompt().EnterViCommandLineMode()
		case "yesno":
			m.prompt().Style = m.prompt().Style.BorderForeground(globalTheme.PromptOffBorder)
		case "input":
			// Input mode - hide prompt to show command line input
			m.prompt().Blur()
		case "learning":
			m.prompt().EnterViLearningMode()
		case "select", "resume", "models", "help":
			// These modes don't need prompt updates, just placeholder changes
			m.prompt().Blur()
			m.prompt().TextArea.Placeholder = "j/k, CTRL-D/U to navigate | Enter to select | ESC to abort"
			m.prompt().Style = m.prompt().Style.BorderForeground(globalTheme.PromptOffBorder)
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
			m.tabs.Content().Chat.AddToRawHistory("COMMAND", ":"+msg.command)

			// Use FindCommand for vim-style partial matching
			cmd, matches, found := m.commandRegistry.FindCommand(cmdName)
			if found {
				c := cmd.Handler(&m, parts[1:])
				m.prompt().Focus()
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
		m.prompt().Focus()
		return m, nil

	case commandCancelledMsg:
		// Command cancelled - hide completions and restore focus
		m.showCompletionDialog = false
		m.completions.Hide()
		m.completionMode = ""
		m.prompt().Focus()
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
		return m, m.tabs.Content().ShowChat()

	case llmInitSuccessMsg:
		// LLM is configured daemon-side by now; just reflect readiness
		// in the status bar.
		m.status.SetProvider(m.config.LLM.Provider, m.config.LLM.Model, true)

		// Fire shogunate_started event to trigger wakeup ritual and health checks
		latest, hasUpdate, _ := utils.CheckForUpdates()
		m.raiseShogunateEvent(storage.EventShogunateStarted, storage.JSON{
			"latest_version":  latest.Version,
			"has_update":      hasUpdate,
			"current_version": utils.AsimiVersion})

	case llmInitErrorMsg:
		// LLM initialization failed - show persistent message in Chancellor tab
		m.tabs.Chancellor().AddMessage(fmt.Sprintf("%s%s LLM initialization failed: %v\n\nUse `:help models` to learn more.",
			systemPrefix, completeFailurePrefix, msg.err))
		slog.Warn("LLM initialization failed", "error", msg.err)
		m.commandLine.AddToast("Running without a model, use `:help models` to configure", "warning", 5000)

	case startConversationMsg:
		// Handle starting a new conversation (used by init, new, and other commands)
		slog.Debug("got startConversationMsg", "RunOnHost", msg.RunOnHost, "tryUpgradeToSandbox", msg.tryUpgradeToSandbox)

		// Clear history if requested - this can happen even without shogunate
		if msg.clearHistory {
			m.sessionActive = true
			// Clear the chat instead of creating a new component to avoid re-initializing the markdown renderer
			m.tabs.Content().Chat.Clear()

			// Reset prompt history and waiting state
			m.initHistory()
			m.stopStreamingTab(m.tabs.ActiveTab().Target)

			// Reset session conversation history
			if m.shogunate != nil {
				_ = m.shogunate.ClearSessionHistory(m.currentTabTarget())
			}
		}

		// Display initial messages after clearing history (before streaming starts)
		for _, initialMsg := range msg.initialMessages {
			m.tabs.Content().Chat.AddMessage(initialMsg)
		}

		// The rest of the operations require a shogunate
		if m.shogunate == nil {
			m.commandLine.AddToast("No LLM session available", "error", 4000)
			return m, nil
		}

		// Set the shell runner based on RunOnHost flag
		if msg.RunOnHost {
			slog.Debug("using host shell runner for this conversation")
			m.shogunate.AllowRunnerFallback(true)

			// Wrap the caller's func with code to restore the previous runner
			originalCallback := msg.onStreamComplete
			msg.onStreamComplete = func(model *TUIModel) tea.Cmd {
				model.shogunate.AllowRunnerFallback(false)

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
			ctx := m.tabs.ActiveTab().Ctx
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
			/* TODO: Remove
			if len(m.tabs.Content().Chat.Messages) > 0 {
				m.tabs.Content().Chat.Messages[0] = ChatMessage{Content: newSessionMessage(), Indent: 0}
				m.tabs.Content().Chat.UpdateContent()
			}
			*/
		}
		return m, nil

	case compactConversationMsg:
		// Handle conversation compaction
		slog.Debug("got compactConversationMsg")

		target := m.currentTabTarget()
		if _, ok := m.currentSessionState(); !ok {
			m.commandLine.AddToast("No LLM session available for compaction", "error", 4000)
			return m, nil
		}

		// Add a message to show we're compacting
		compactMsg := NewChatMsgBuilder("🗜️  Compacting conversation history...")
		compactMsg.WriteLn("This may take a moment as the model summarizes the chat.")
		m.tabs.Content().Chat.AddMessage(compactMsg.String())

		// Perform the compaction in a goroutine
		go func() {
			ctx := context.Background()
			summary, err := m.shogunate.CompactSession(ctx, target, compactPrompt)
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

		var info ContextInfo

		if state, ok := m.currentSessionState(); ok {
			si := state.ContextInfo
			info = ContextInfo{
				Model:              si.Model,
				TotalTokens:        si.TotalTokens,
				UsedTokens:         si.UsedTokens,
				SystemPromptTokens: si.SystemPromptTokens,
				SystemToolsTokens:  si.SystemToolsTokens,
				MemoryFilesTokens:  si.MemoryFilesTokens,
				MessagesTokens:     si.MessagesTokens,
				FreeTokens:         si.FreeTokens,
				AutocompactBuffer:  si.AutocompactBuffer,
			}
		}

		// Add success message
		m.tabs.Content().Chat.AddMessage(fmt.Sprintf("✅ Conversation compacted successfully!\n\nContext usage: %s/%s tokens (%.1f%%)",
			formatTokenCount(info.UsedTokens),
			formatTokenCount(info.TotalTokens),
			percentage(info.UsedTokens, info.TotalTokens)))

		m.commandLine.AddToast("Conversation history compacted", "success", 3000)

	case compactErrorMsg:
		// Compaction failed
		slog.Warn("compaction failed", "error", msg.err)
		m.tabs.Content().Chat.AddMessage(fmt.Sprintf("❌ Failed to compact conversation: %v\n\nYour conversation context was left unchanged.", msg.err))
		m.commandLine.AddToast("Compaction failed - context unchanged", "error", 3000)

	case runners.ContainerLaunchedMsg:
		// Container launch notification
		m.commandLine.AddToast(msg.Message, "info", 3*time.Second)
		// Update shell runner info in status bar
		m.status.ContainerID = msg.ContainerID
		return m, nil

	case shellCommandResultMsg:
		// Shell command execution completed
		m.tabs.Content().Chat.AddShellCommandResult(msg)
		m.repoInfo.RefreshDiff()
		m.prompt().Focus()
		return m, nil

	}

	// Restore focus to prompt if no modals are active and view is chat
	if m.providerModal == nil && m.codeInputModal == nil &&
		!m.commandLine.IsInCommandMode() &&
		m.tabs.Content().GetActiveView() == ViewChat {
		m.prompt().Focus()
	}

	// Update content (which handles chat updates)
	contentCmd := m.tabs.UpdateContent(msg)
	return m, contentCmd
}

func (m *TUIModel) updateFileCompletions(files []string) {
	inputValue := m.prompt().Value()

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
	inputValue := m.prompt().Value()

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
	m.prompt().SetScreenHeight(m.height)

	// Calculate desired prompt height based on content
	promptHeight := m.prompt().CalculateDesiredHeight()

	// Account for borders (2 lines for top and bottom border)
	promptWithBorder := promptHeight + 2

	// Calculate chat height (account for tab bar when multiple tabs)
	tabBarHeight := m.tabs.TabBarHeight()
	contentHeight := m.height - commandLineHeight - statusHeight - promptWithBorder - tabBarHeight + 1
	if contentHeight < 0 {
		contentHeight = 0
	}

	// Update component widths
	m.status.SetWidth(width + 2)
	m.commandLine.SetWidth(width + 2)

	// Full width layout - content handles chat and other views
	m.tabs.SetSize(width, contentHeight)

	m.prompt().SetWidth(width)
	m.prompt().SetHeight(promptHeight)

	// Update status info
	// TODO: move this to a proper place and drop the currentEdictKey
	if m.shogunate != nil && m.currentEdictKey.ID != 0 {
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
	// SetWidth ensures the active tab's prompt matches the terminal width
	// (prompts are per-tab, but only the active one is sized on WindowSizeMsg)
	m.prompt().SetScreenHeight(m.height)
	m.prompt().SetWidth(m.width - 2)
	promptHeight := m.prompt().CalculateDesiredHeight()
	m.prompt().SetHeight(promptHeight)
	promptWithBorder := promptHeight + 2

	// Recalculate content height based on new prompt height
	commandLineHeight := 1
	statusHeight := 1
	tabBarHeight := m.tabs.TabBarHeight()
	contentHeight := m.height - commandLineHeight - statusHeight - promptWithBorder - tabBarHeight + 1
	if contentHeight < 0 {
		contentHeight = 0
	}
	m.tabs.SetSize(m.width-2, contentHeight)

	modalHeight := 0
	if m.modal != nil {
		modalHeight = lipgloss.Height(m.modal.Render())
	}
	mainContent := m.renderMainContent(modalHeight)
	promptView := m.prompt().View()
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
	promptWithBorder := m.prompt().Height + 2
	tabBarHeight := m.tabs.TabBarHeight()
	contentHeight := m.height - commandLineHeight - statusHeight - promptWithBorder - tabBarHeight + 1 - modalHeight
	if contentHeight < 0 {
		contentHeight = 0
	}

	// First check if we're viewing help/models/resume - these take precedence
	if m.tabs.Content().GetActiveView() != ViewChat {
		return m.tabs.Content().View()
	}

	// Then check for special modes
	switch {
	case m.rawMode:
		return m.renderRawSessionView(m.width, contentHeight)
		/* TOOO: get some data, like version and update available from renderHomeView
		case !m.sessionActive:
			return m.renderHomeView(m.width, contentHeight)
		*/
	default:
		// Use content component which handles chat view
		return m.tabs.Content().View()
	}
}

func (m TUIModel) composeBaseView(mainContent, promptView, commandLineView string) string {
	tabBar := m.tabs.RenderTabBar(m.width)
	statusView := m.status.View()

	// Build layout parts
	var layoutParts []string
	if tabBar != "" {
		layoutParts = append(layoutParts, tabBar)
	}
	layoutParts = append(layoutParts, mainContent)

	if m.modal != nil {
		modalRender := m.modal.Render()
		layoutParts = append(layoutParts, modalRender, "")
	}

	if promptView != "" {
		layoutParts = append(layoutParts, promptView)
	}
	layoutParts = append(layoutParts, statusView, commandLineView)

	return lipgloss.JoinVertical(lipgloss.Left, layoutParts...)
}

func (m TUIModel) overlayCompletionDialog(baseView, promptView, commandLineView string) string {
	dialog := m.completions.View()
	if dialog == "" {
		return baseView
	}

	// 1 is for the status line
	// TODO: bring it down and cover part of the prompt. Need to wait for bubbletea 2.0
	bottomOffset := 2 + lipgloss.Height(promptView)

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

	// Create a version display in muted color
	versionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")). // Muted gray color
		Align(lipgloss.Center).
		Width(width)

	versionDisplay := versionStyle.Render("Version: " + utils.AsimiVersion)

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

	// Add title, subtitle, and version at the top (prepend)
	contentParts = append([]string{title, "", subtitle, "", versionDisplay, ""}, contentParts...)

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
	rawHistory := m.tabs.Content().Chat.GetRawHistory()
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

// stopStreamingTab cancels streaming on a single tab by target ID.
// Local tab context is cancelled for TUI-side state; CancelTab is
// forwarded so the daemon's ritual/stream context (registered under
// the same channelID via CancellableStreamCtx) also tears down.
func (m *TUIModel) stopStreamingTab(tabTarget string) {
	m.tabs.CancelTabByID(tabTarget)
	if m.shogunate != nil {
		m.shogunate.CancelTab(tabTarget)
	}
	if !m.tabs.AnyStreaming() {
		m.stopWaitingForResponse()
	}
}

// clearStreamingTab resets UI streaming state for a tab without touching its
// context. Use this when the session has already stopped itself (interrupted,
// errored, max-tokens) — cancelling again only creates feedback loops.
func (m *TUIModel) clearStreamingTab(tabTarget string) {
	m.tabs.ClearStreamingByTab(tabTarget)
	if !m.tabs.AnyStreaming() {
		m.stopWaitingForResponse()
	}
}

// stopStreaming cancels all streaming globally (used for shutdown).
func (m *TUIModel) stopStreaming() {
	m.tabs.CancelAllTabs()
	m.stopWaitingForResponse()
}

// handleAnsweringComplete closes the zhengming waiter and updates the DB.
// Runs in a goroutine from the Update loop.
func (m *TUIModel) handleAnsweringComplete(msg AnsweredMsg) {
	if m.shogunate == nil {
		return
	}
	// Join answers into a single string for the response
	answer := strings.Join(msg.Answers, "; ")
	if answer == "[chat]" {
		return
	}
	// Handle DB updates and emit zhengming_answered event
	if err := m.shogunate.HandleZhengmingResponse(context.Background(), msg.RequestID, answer); err != nil {
		slog.Error("failed to handle zhengming response", "error", err)
	}
}

func (m *TUIModel) raiseShogunateEvent(event storage.ShogunateEvent, params storage.JSON) {
	if m.shogunate == nil {
		slog.Warn("Failed to raise event as shogunate is nil", "event", event)
		return
	}
	var key storage.EdictKey
	switch event {
	case storage.EventShogunateStarted, storage.EventShogunateReady:
		key = m.shogunate.CourtEdictKey()
	default:
		// Derive edict key from payload instead of stale currentEdictKey
		if edictID, ok := params["edict_id"].(uint); ok {
			key = m.shogunate.EdictKey(edictID)
		} else {
			key = m.currentEdictKey // fallback for backward compatibility
		}
	}
	m.shogunate.PublishEvent(key, event, params)
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
