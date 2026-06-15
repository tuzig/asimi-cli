package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TabType represents the type of tab connection
type TabType string

// Tab represents a TUI tab with its own content buffer and stream target
type Tab struct {
	Label     string
	Type      TabType
	Target    string             // minister ID, edict ID, or ritual run ID
	Content   ContentComponent   // Own content buffer per tab
	EdictID   uint               // Current edict ID for this tab
	Streaming bool               // True when this tab is actively receiving stream data
	Ctx       context.Context    // per-tab context, flows to rituals for ruling tab
	Cancel    context.CancelFunc // per-tab streaming cancellation
}

// NewTab creates a Tab with its own context and cancellation.
// Every tab is an independent, cancellable unit of work.
func NewTab(label string, tabType TabType, target string, content ContentComponent) Tab {
	ctx, cancel := context.WithCancel(context.Background())
	return Tab{
		Label:   label,
		Type:    tabType,
		Target:  target,
		Content: content,
		Ctx:     ctx,
		Cancel:  cancel,
	}
}

// TabManager manages multiple tabs, each wrapping a ContentComponent
type TabManager struct {
	tabs            []Tab
	activeTab       int
	pendingG        bool
	width, height   int
	markdownEnabled bool
	getStatus       func() string
	onTabSwitch     func() // Called after tab switch to update TUI state
}

// bt is a backtick character, used inside raw string literals where ` cannot appear.
const bt = "`"

// tabGreetings maps each minister tab target to a welcome message.
// Messages are injected as system messages into each tab's ChatComponent
// at initialization — pure presentation, no daemon coupling.
var tabGreetings = map[string]string{
	"chancellor": "Greetings, Ruler and welcome to your Court\n" +
		"Your court's main function is to harmonize the three realms:\n\n" +
		"\t- **Intent** the ruler's will, TODO comments and docs\n" +
		"\t- **Earth** Production code\n" +
		"\t- **Heaven** Logs, CI, test results and tests\n\n" +
		"The UI follows neovim's. For example, " + bt + "gt" + bt + " in NORMAL mode to move tab\n" +
		"For system commands use the " + bt + ":" + bt + " prefix, as in " + bt + ":help" + bt + "\n" +
		"For shell commands in the sandbox use " + bt + ":!" + bt + " prefix, as in " + bt + ":!uname" + bt,
	"sage": `Greetings, Ruler and welcome to your hunting grounds
With the sage you can hunt bugs' root cause,
brew new features and forumlate edicts`,
	"forge": `Salutations, Ruler and welcome to your Forge 工部
The forge is where code is changed and the earth is shaped
It's best to run the forge as part of a ritual,
but if it's a quick-fix you're after, you're in the right tab`,
	"judge": `Greetings, Ruler and welcome to your tribunal
Here we weigh every change against the truth:
- **Tests** pass or fail — there is no ambiguity
- **Verdicts** seal the fate of every manifest
If quality bugs you, you're on the right tab`,
}

// initTabGreetings seeds each tab's ChatComponent with its minister welcome
// message. Called once during NewTabManager construction.
func initTabGreetings(tm *TabManager) {
	for i := range tm.tabs {
		if greeting := tabGreetings[tm.tabs[i].Target]; greeting != "" {
			tm.tabs[i].Content.Chat.AddMarkdownMessage(systemPrefix + greeting)
		}
	}
}

// NewTabManager creates a TabManager with 4 tabs per the Shogunate structure:
// 宰相 Chancellor (ruling/edicts), 聖人 Sage (exploration),
// 工部 Forge (builds), 刑部 Judge (tests)
func NewTabManager(w, h int, mdEnabled bool, getStatus func() string) TabManager {
	tm := TabManager{
		tabs: []Tab{
			NewTab("宰相 Chancellor", "chancellor", "chancellor",
				newContentComponent(w, h, mdEnabled, getStatus)),
			NewTab("聖人 Sage", "sage", "sage",
				newContentComponent(w, h, mdEnabled, getStatus)),
			NewTab("工部 Forge", "forge", "forge",
				newContentComponent(w, h, mdEnabled, getStatus)),
			NewTab("刑部 Judge", "judge", "judge",
				newContentComponent(w, h, mdEnabled, getStatus)),
		},
		activeTab:       0,
		width:           w,
		height:          h,
		markdownEnabled: mdEnabled,
		getStatus:       getStatus,
	}
	// Seed each tab with its minister welcome greeting
	return tm
}

// newContentComponent is a helper to create ContentComponent with status getter
func newContentComponent(w, h int, mdEnabled bool, getStatus func() string) ContentComponent {
	c := NewContentComponent(w, h, mdEnabled)
	c.Chat.GetStatus = getStatus
	return c
}

// Content returns a pointer to the active tab's ContentComponent
func (tm *TabManager) Content() *ContentComponent {
	return &tm.tabs[tm.activeTab].Content
}

// StreamingChat returns the ChatComponent receiving stream data.
// If the active tab is streaming, return it. Otherwise find any streaming tab.
// Falls back to the active tab if nothing is streaming.
func (tm *TabManager) StreamingChat() *ChatComponent {
	if tm.tabs[tm.activeTab].Streaming {
		return tm.tabs[tm.activeTab].Content.Chat
	}
	for i := range tm.tabs {
		if tm.tabs[i].Streaming {
			return tm.tabs[i].Content.Chat
		}
	}
	return tm.tabs[tm.activeTab].Content.Chat
}

// StreamingChatByTab returns the ChatComponent for a specific tab.
// Matches by tab.Target == tabID && tab.Streaming.
// Falls back to StreamingChat() if no match is found.
func (tm *TabManager) StreamingChatByTab(tabID string) *ChatComponent {
	if tabID != "" {
		for i := range tm.tabs {
			if tm.tabs[i].Target == tabID && tm.tabs[i].Streaming {
				return tm.tabs[i].Content.Chat
			}
		}
	}
	return tm.StreamingChat()
}

// ChatByTab returns the ChatComponent for the tab matching target.
// Falls back to active tab if no match.
func (tm *TabManager) ChatByTab(tabID string) *ChatComponent {
	if tabID != "" {
		for i := range tm.tabs {
			if tm.tabs[i].Target == tabID {
				return tm.tabs[i].Content.Chat
			}
		}
	}
	return tm.tabs[tm.activeTab].Content.Chat
}

// Chancellor returns the Chancellor tab's ChatComponent (always index 0)
func (tm *TabManager) Chancellor() *ChatComponent {
	return tm.tabs[0].Content.Chat
}

// FlushDirtyChats calls UpdateContent on every dirty tab chat. Used by the
// debounce tick to flush all streaming tabs, not just the active one —
// chunks can land on a non-active tab.
func (tm *TabManager) FlushDirtyChats() {
	for i := range tm.tabs {
		chat := tm.tabs[i].Content.Chat
		if chat != nil && chat.contentDirty {
			chat.UpdateContent()
		}
	}
}

// SetStreamingTabByTab marks the tab with matching Target as streaming
func (tm *TabManager) SetStreamingTabByTab(tabID string) {
	for i := range tm.tabs {
		if tm.tabs[i].Target == tabID {
			tm.tabs[i].Streaming = true
			return
		}
	}
	tm.tabs[tm.activeTab].Streaming = true // fallback
}

// ClearStreamingByTab clears the streaming flag on the tab with matching Target
func (tm *TabManager) ClearStreamingByTab(tabID string) {
	for i := range tm.tabs {
		if tm.tabs[i].Target == tabID {
			tm.tabs[i].Streaming = false
			return
		}
	}
}

// CancelTabByID cancels streaming on the tab matching the given target.
func (tm *TabManager) CancelTabByID(tabID string) {
	for i := range tm.tabs {
		tab := &tm.tabs[i]
		if tab.Target == tabID {
			if tab.Cancel != nil {
				tab.Cancel()
			}
			tab.Ctx, tab.Cancel = context.WithCancel(context.Background())
			slog.Debug("Created a new cancelable context", "tab", tabID)
			tab.Streaming = false
			return
		}
	}
	slog.Error("Failed to cancel tab streaming, tabID not found", "tabID", tabID)
}

// CancelAllTabs cancels streaming on all tabs.
// The Chancellor tab gets a fresh context for future rituals.
func (tm *TabManager) CancelAllTabs() {
	for i := range tm.tabs {
		if tm.tabs[i].Cancel != nil {
			tm.tabs[i].Cancel()
		}
		if tm.tabs[i].Target == "chancellor" {
			tm.tabs[i].Ctx, tm.tabs[i].Cancel = context.WithCancel(context.Background())
		} else {
			tm.tabs[i].Cancel = nil
		}
		tm.tabs[i].Streaming = false
	}
}

// SwitchTo saves current tab state and switches to the target index
func (tm *TabManager) SwitchTo(index int) {
	if index < 0 || index >= len(tm.tabs) || index == tm.activeTab {
		return
	}
	tm.activeTab = index
	// Notify TUI of tab switch so it can update context percent
	if tm.onTabSwitch != nil {
		tm.onTabSwitch()
	}
}

// Add creates a new tab and switches to it
func (tm *TabManager) Add(label string, tabType TabType, target string) {
	newContent := newContentComponent(tm.width, tm.height, tm.markdownEnabled, tm.getStatus)
	tm.tabs = append(tm.tabs, NewTab(label, tabType, target, newContent))
	tm.SwitchTo(len(tm.tabs) - 1)
}

// Close closes the active tab if safe to do so
func (tm *TabManager) Close() error {
	if len(tm.tabs) <= 1 {
		return errors.New("cannot close the last tab")
	}
	if tm.tabs[tm.activeTab].Streaming {
		return errors.New("cannot close tab while streaming")
	}

	closingIdx := tm.activeTab
	// Switch to adjacent tab before removing
	if closingIdx > 0 {
		tm.SwitchTo(closingIdx - 1)
	} else {
		tm.SwitchTo(closingIdx + 1)
	}

	// Remove the closed tab
	tm.tabs = append(tm.tabs[:closingIdx], tm.tabs[closingIdx+1:]...)

	// Adjust activeTab index if needed
	if tm.activeTab > closingIdx {
		tm.activeTab--
	}
	return nil
}

// NextTab switches to the next tab (wraps around)
func (tm *TabManager) NextTab() {
	if len(tm.tabs) <= 1 {
		return
	}
	next := (tm.activeTab + 1) % len(tm.tabs)
	tm.SwitchTo(next)
}

// PrevTab switches to the previous tab (wraps around)
func (tm *TabManager) PrevTab() {
	if len(tm.tabs) <= 1 {
		return
	}
	prev := tm.activeTab - 1
	if prev < 0 {
		prev = len(tm.tabs) - 1
	}
	tm.SwitchTo(prev)
}

// RenderTabBar renders the tab bar; returns "" for single tab
func (tm *TabManager) RenderTabBar(width int) string {
	if len(tm.tabs) <= 1 {
		return ""
	}
	fillBg := lipgloss.Color("#333333")
	var parts []string
	for i, tab := range tm.tabs {
		label := tab.Label
		if i == tm.activeTab {
			// Active tab: bold, bright foreground, stands out from the fill
			style := lipgloss.NewStyle().
				Bold(true).
				Foreground(globalTheme.TextColor).
				Background(fillBg).
				Padding(0, 1)
			parts = append(parts, style.Render(label))
		} else {
			// Inactive tab: dimmer text on the same grey fill
			style := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#999999")).
				Background(fillBg).
				Padding(0, 1)
			parts = append(parts, style.Render(label))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	// Fill the rest of the line with the same grey background
	return lipgloss.NewStyle().Width(width).Background(fillBg).Render(tabBar)
}

// TabBarHeight returns 1 if multiple tabs, 0 otherwise
func (tm *TabManager) TabBarHeight() int {
	if len(tm.tabs) > 1 {
		return 1
	}
	return 0
}

// SetSize updates dimensions for all tabs
func (tm *TabManager) SetSize(w, h int) {
	tm.width = w
	tm.height = h
	for i := range tm.tabs {
		tm.tabs[i].Content.SetSize(w, h)
	}
}

// ActiveTab returns a pointer to the active Tab
func (tm *TabManager) ActiveTab() *Tab {
	return &tm.tabs[tm.activeTab]
}

// TabByTarget returns the tab whose Target matches, or nil
func (tm *TabManager) TabByTarget(target string) *Tab {
	for i := range tm.tabs {
		if tm.tabs[i].Target == target {
			return &tm.tabs[i]
		}
	}
	return nil
}

// SwitchToTabType switches to the first tab matching the given type
func (tm *TabManager) SwitchToTabType(tabType TabType) {
	for i := range tm.tabs {
		if tm.tabs[i].Type == tabType {
			tm.SwitchTo(i)
			return
		}
	}
}

// TabCount returns the number of tabs
func (tm *TabManager) TabCount() int {
	return len(tm.tabs)
}

// ActiveEdictID returns the active tab's edict ID
func (tm *TabManager) ActiveEdictID() uint {
	return tm.tabs[tm.activeTab].EdictID
}

// SetActiveEdictID sets the edict ID on the active tab
func (tm *TabManager) SetActiveEdictID(id uint) {
	tm.tabs[tm.activeTab].EdictID = id
}

// SetStreamingTab marks the active tab as streaming
func (tm *TabManager) SetStreamingTab() {
	tm.tabs[tm.activeTab].Streaming = true
}

// ClearStreaming clears the streaming flag on all tabs
func (tm *TabManager) ClearStreaming() {
	for i := range tm.tabs {
		tm.tabs[i].Streaming = false
	}
}

// StreamingTabIndex returns the index of a streaming tab, or -1 if none
func (tm *TabManager) StreamingTabIndex() int {
	for i := range tm.tabs {
		if tm.tabs[i].Streaming {
			return i
		}
	}
	return -1
}

// AnyStreaming returns true if any tab is currently streaming
func (tm *TabManager) AnyStreaming() bool {
	for i := range tm.tabs {
		if tm.tabs[i].Streaming {
			return true
		}
	}
	return false
}

// HandlePendingG handles the g-prefix key sequence for gt/gT/gg
// Returns (handled bool, cmd tea.Cmd)
func (tm *TabManager) HandlePendingG(key string, scrollToTop func()) (bool, tea.Cmd) {
	if !tm.pendingG {
		return false, nil
	}
	tm.pendingG = false
	switch key {
	case "t":
		tm.NextTab()
		return true, nil
	case "T":
		tm.PrevTab()
		return true, nil
	case "g":
		if scrollToTop != nil {
			scrollToTop()
		}
		return true, nil
	}
	return true, nil // consumed the pending g, unknown follow-up
}

// SetPendingG sets the pending g-prefix flag
func (tm *TabManager) SetPendingG() {
	tm.pendingG = true
}

// IsPendingG returns whether g-prefix is pending
func (tm *TabManager) IsPendingG() bool {
	return tm.pendingG
}

// ClearPendingG clears the pending g-prefix flag
func (tm *TabManager) ClearPendingG() {
	tm.pendingG = false
}

// UpdateContent calls Update on the active tab's ContentComponent and writes back
func (tm *TabManager) UpdateContent(msg tea.Msg) tea.Cmd {
	updated, cmd := tm.tabs[tm.activeTab].Content.Update(msg)
	tm.tabs[tm.activeTab].Content = updated
	return cmd
}

// ViewType represents the active view
type ViewType int

const (
	ViewChat ViewType = iota
	ViewHelp
	ViewModels
	ViewResume
	ViewSeal
)

// NavigationMode represents how navigation works in the current view
type NavigationMode int

const (
	NavText NavigationMode = iota // Text scrolling (chat, help)
	NavList                       // List selection (models, resume)
)

// ContentComponent manages all main content views with unified navigation
type ContentComponent struct {
	activeView ViewType
	width      int
	height     int

	// Sub-components
	Chat       *ChatComponent
	help       HelpWindow
	models     ModelsWindow
	resume     ResumeWindow
	sealSelect SealSelectWindow

	// Unified navigation state
	navMode      NavigationMode
	activeList   ListNavigator           // current list view for NavList mode
	onSelect     func(index int) tea.Cmd // called on enter in list mode
	viewport     viewport.Model          // For text navigation
	selectedItem int                     // For list navigation
	scrollOffset int                     // For list navigation
}

// NewContentComponent creates a new content component
func NewContentComponent(width, height int, markdownEnabled bool) ContentComponent {
	return ContentComponent{
		activeView: ViewChat,
		width:      width,
		height:     height,
		Chat:       NewChatComponent(width, height, markdownEnabled),
		help:       NewHelpWindow(),
		models:     NewModelsWindow(),
		resume:     NewResumeWindow(),
		sealSelect: NewSealSelectWindow(),
		navMode:    NavText,
		viewport:   viewport.New(width, height),
	}
}

// SetSize updates the dimensions
func (c *ContentComponent) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.viewport.Width = width
	c.viewport.Height = height
	h := height - 1
	c.Chat.SetSize(width, h)
	c.help.SetSize(width, h)
	c.models.SetSize(width, h)
	c.resume.SetSize(width, h)
	c.sealSelect.SetSize(width, h)
}

// GetActiveView returns the current view type
func (c *ContentComponent) GetActiveView() ViewType {
	return c.activeView
}

// ShowChat switches to chat view
func (c *ContentComponent) ShowChat() tea.Cmd {
	c.activeView = ViewChat
	c.navMode = NavText
	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "insert"}
	}
}

// ShowHelp switches to help view
func (c *ContentComponent) ShowHelp(topic string) tea.Cmd {
	c.activeView = ViewHelp
	c.navMode = NavText
	c.help.SetTopic(topic)

	// Setup viewport for help text
	c.viewport.SetContent(c.help.RenderContent())
	c.viewport.GotoTop()

	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "help"}
	}
}

// ShowModels switches to models view (legacy - for backward compatibility)
func (c *ContentComponent) ShowModels(models []AnthropicModel, currentModel string) tea.Cmd {
	// Convert AnthropicModel to unified Model type
	unifiedModels := make([]Model, len(models))
	for i, m := range models {
		status := "ready"
		if m.ID == currentModel {
			status = "active"
		}
		displayName := m.DisplayName
		if displayName == "" {
			displayName = m.ID
		}
		unifiedModels[i] = Model{
			ID:          m.ID,
			DisplayName: displayName,
			Provider:    "anthropic",
			Status:      status,
		}
	}
	return c.ShowUnifiedModels(unifiedModels, currentModel)
}

// ShowUnifiedModels switches to models view with unified model list
func (c *ContentComponent) ShowUnifiedModels(models []Model, currentModel string) tea.Cmd {
	c.activeView = ViewModels
	c.navMode = NavList
	c.activeList = &c.models.SelectWindow
	c.models.SetModels(models, currentModel)
	c.selectedItem = c.models.GetInitialSelection()
	c.scrollOffset = 0
	c.onSelect = func(index int) tea.Cmd {
		model := c.models.GetSelectedModel(index)
		if model == nil || !IsModelSelectable(*model) {
			return nil
		}
		if model.OnSelect != nil {
			return model.OnSelect
		}
		return func() tea.Msg {
			return modelSelectedMsg{model: model, onSelect: model.OnSelect}
		}
	}

	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "select"}
	}
}

// ShowResume switches to resume view
func (c *ContentComponent) ShowResume(sessions []shogunate.Session) tea.Cmd {
	c.activeView = ViewResume
	c.navMode = NavList
	c.activeList = &c.resume.SelectWindow
	c.resume.SetSessions(sessions)
	c.selectedItem = 0
	c.scrollOffset = 0
	c.onSelect = func(index int) tea.Cmd {
		session := c.resume.GetSelectedSession(index)
		if session == nil {
			return nil
		}
		return c.resume.LoadSession(session.ID)
	}

	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "select"}
	}
}

// ShowSealSelection switches to seal selection view
func (c *ContentComponent) ShowSealSelection(edicts []storage.ActiveEdict) tea.Cmd {
	c.activeView = ViewSeal
	c.navMode = NavList
	c.activeList = &c.sealSelect.SelectWindow
	c.sealSelect.SetItems(edicts)
	c.selectedItem = 0
	c.scrollOffset = 0
	c.onSelect = func(index int) tea.Cmd {
		edict := c.sealSelect.GetSelectedItem(index)
		if edict == nil {
			return nil
		}
		return func() tea.Msg {
			return sealSelectedMsg{edictID: edict.ID}
		}
	}

	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "select"}
	}
}

// SetModelsLoading shows loading state for models
func (c *ContentComponent) SetModelsLoading() {
	c.models.SetLoading(true)
}

// SetModelsError shows error state for models
func (c *ContentComponent) SetModelsError(err string) {
	c.models.SetError(err)
}

// SetResumeLoading shows loading state for resume
func (c *ContentComponent) SetResumeLoading() {
	c.activeView = ViewResume
	c.navMode = NavList
	c.resume.SetLoading(true)
	c.selectedItem = 0
	c.scrollOffset = 0
}

// Update handles messages and navigation
func (c *ContentComponent) Update(msg tea.Msg) (ContentComponent, tea.Cmd) {
	var cmds []tea.Cmd

	// Delegate to active view first for view-specific updates
	switch c.activeView {
	case ViewChat:
		var cmd tea.Cmd
		newChat, cmd := c.Chat.Update(msg)
		c.Chat = &newChat
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Handle navigation based on mode
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Exit handling for non-chat views
		if c.activeView != ViewChat {
			cmd := c.handleExitKeys(msg)
			if cmd != nil {
				return *c, cmd
			}
		}

		// Navigation
		switch c.navMode {
		case NavText:
			cmd := c.handleTextNavigation(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case NavList:
			cmd := c.handleListNavigation(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.MouseMsg:
		if c.navMode == NavText && c.activeView == ViewChat {
			// Chat handles its own mouse events
			// Already handled above in chat.Update
		} else if c.navMode == NavText {
			// Handle mouse scrolling for help view
			switch msg.Type {
			case tea.MouseWheelUp:
				c.viewport.ScrollUp(1)
			case tea.MouseWheelDown:
				c.viewport.ScrollDown(1)
			}
		}
	}

	return *c, tea.Batch(cmds...)
}

// handleExitKeys handles Esc, Ctrl+C for exiting views
func (c *ContentComponent) handleExitKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		// Ctrl+C exits to chat
		return c.ShowChat()
	case "esc":
		// Single ESC exits to chat
		return c.ShowChat()
	}
	return nil
}

// handleTextNavigation handles navigation for text views (chat, help)
func (c *ContentComponent) handleTextNavigation(msg tea.KeyMsg) tea.Cmd {
	if c.activeView == ViewChat {
		// Chat handles its own navigation via Update
		return nil
	}

	// Help view navigation
	switch msg.String() {
	case "j", "down":
		c.viewport.LineDown(1)
	case "k", "up":
		c.viewport.LineUp(1)
	case "ctrl+d":
		c.viewport.HalfPageDown()
	case "ctrl+u":
		c.viewport.HalfPageUp()
	case "ctrl+f", "pgdown":
		c.viewport.PageDown()
	case "ctrl+b", "pgup":
		c.viewport.PageUp()
	case "g":
		// Check if this is gg (go to top)
		// For now, single 'g' goes to top
		c.viewport.GotoTop()
	case "G":
		c.viewport.GotoBottom()
	}

	return nil
}

// handleListNavigation handles navigation for list views (models, resume, seal)
func (c *ContentComponent) handleListNavigation(msg tea.KeyMsg) tea.Cmd {
	nav := c.activeList
	if nav == nil {
		return nil
	}

	itemCount := nav.GetItemCount()
	visibleSlots := nav.GetVisibleSlots()
	var scrollInfoCmd tea.Cmd

	switch msg.String() {
	case "j", "down":
		newIndex := nav.NavNext(c.selectedItem)
		if newIndex != c.selectedItem {
			c.selectedItem = newIndex
			if c.selectedItem >= c.scrollOffset+visibleSlots {
				c.scrollOffset = c.selectedItem - visibleSlots + 1
			}
			scrollInfoCmd = c.getScrollInfoCmd()
		}
	case "k", "up":
		newIndex := nav.NavPrev(c.selectedItem)
		if newIndex != c.selectedItem {
			c.selectedItem = newIndex
			if c.selectedItem < c.scrollOffset {
				c.scrollOffset = c.selectedItem
			}
			scrollInfoCmd = c.getScrollInfoCmd()
		}
	case "ctrl+d": // Half page down
		move := visibleSlots / 2
		if move < 1 {
			move = 1
		}
		target := c.selectedItem + move
		if target >= itemCount {
			target = itemCount - 1
		}
		c.selectedItem = nav.NavNearest(target)
		if c.selectedItem >= c.scrollOffset+visibleSlots {
			c.scrollOffset = c.selectedItem - visibleSlots + 1
		}
		scrollInfoCmd = c.getScrollInfoCmd()
	case "ctrl+u": // Half page up
		move := visibleSlots / 2
		if move < 1 {
			move = 1
		}
		target := c.selectedItem - move
		if target < 0 {
			target = 0
		}
		c.selectedItem = nav.NavNearest(target)
		if c.selectedItem < c.scrollOffset {
			c.scrollOffset = c.selectedItem
		}
		scrollInfoCmd = c.getScrollInfoCmd()
	case "g", "home":
		c.selectedItem = nav.NavFirst()
		c.scrollOffset = 0
		scrollInfoCmd = c.getScrollInfoCmd()
	case "G", "end":
		c.selectedItem = nav.NavLast()
		if c.selectedItem >= visibleSlots {
			c.scrollOffset = c.selectedItem - visibleSlots + 1
		}
		scrollInfoCmd = c.getScrollInfoCmd()
	case "enter":
		return c.handleListSelect()
	}

	return scrollInfoCmd
}

// handleListSelect handles enter/selection for the active list view
func (c *ContentComponent) handleListSelect() tea.Cmd {
	if c.onSelect == nil {
		return nil
	}
	cmd := c.onSelect(c.selectedItem)
	if cmd == nil {
		return nil
	}
	return tea.Batch(c.ShowChat(), cmd)
}

// getScrollInfoCmd returns a command that sends scroll info as a message
func (c *ContentComponent) getScrollInfoCmd() tea.Cmd {
	return nil
}

// View renders the active view
func (c *ContentComponent) View() string {
	switch c.activeView {
	case ViewChat:
		return c.Chat.View()
	case ViewHelp:
		return c.renderHelpView()
	case ViewModels:
		return c.renderModelsView()
	case ViewResume:
		return c.renderResumeView()
	case ViewSeal:
		return c.renderSealView()
	}
	return ""
}

// renderHelpView renders the help view
func (c *ContentComponent) renderHelpView() string {
	// Title bar
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F952F9")).
		Background(lipgloss.Color("#000000")).
		Padding(0, 1)

	title := titleStyle.Render(fmt.Sprintf(" Help: %s ", c.help.GetTopic()))

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		c.viewport.View(),
	)

	// Ensure the view fills the full height
	return lipgloss.NewStyle().
		// Height(c.height).
		Render(content)
}

// renderModelsView renders the models selection view
func (c *ContentComponent) renderModelsView() string {
	content := c.models.RenderList(c.selectedItem, c.scrollOffset, c.models.GetVisibleSlots())

	// Apply height constraint to prevent overflow clipping from the top
	// Use height-1 to account for the title line
	return lipgloss.NewStyle().
		Height(c.height - 1).
		MaxHeight(c.height - 1).
		Render(content)
}

// renderResumeView renders the session selection view
func (c *ContentComponent) renderResumeView() string {
	content := c.resume.RenderList(c.selectedItem, c.scrollOffset, c.resume.GetVisibleSlots())

	// Apply height constraint to prevent overflow clipping from the top
	// Use height-1 to account for the title line
	return lipgloss.NewStyle().
		Height(c.height - 1).
		MaxHeight(c.height - 1).
		Render(content)
}

// renderSealView renders the seal selection view
func (c *ContentComponent) renderSealView() string {
	content := c.sealSelect.RenderList(c.selectedItem, c.scrollOffset, c.sealSelect.GetVisibleSlots())

	// Apply height constraint to prevent overflow clipping from the top
	// Use height-1 to account for the title line
	return lipgloss.NewStyle().
		Height(c.height - 1).
		MaxHeight(c.height - 1).
		Render(content)
}
