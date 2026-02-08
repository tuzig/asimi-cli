package main

import (
	"fmt"

	"github.com/afittestide/asimi/shogunate"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ViewType represents the active view
type ViewType int

const (
	ViewChat ViewType = iota
	ViewHelp
	ViewModels
	ViewResume
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

	// Sub-components (now simplified - no navigation logic)
	Chat   *ChatComponent
	help   HelpWindow
	models ModelsWindow
	resume ResumeWindow

	// Unified navigation state
	navMode      NavigationMode
	viewport     viewport.Model // For text navigation
	selectedItem int            // For list navigation
	scrollOffset int            // For list navigation
}

// NewContentComponent creates a new content component
func NewContentComponent(width, height int, markdownEnabled bool) ContentComponent {
	return ContentComponent{
		activeView:   ViewChat,
		width:        width,
		height:       height,
		Chat:         NewChatComponent(width, height, markdownEnabled),
		help:         NewHelpWindow(),
		models:       NewModelsWindow(),
		resume:       NewResumeWindow(),
		navMode:      NavText,
		viewport:     viewport.New(width, height),
		selectedItem: 0,
		scrollOffset: 0,
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
	c.models.SetModels(models, currentModel)
	c.selectedItem = c.models.GetInitialSelection()
	c.scrollOffset = 0

	return func() tea.Msg {
		return ChangeModeMsg{NewMode: "select"}
	}
}

// ShowResume switches to resume view
func (c *ContentComponent) ShowResume(sessions []shogunate.Session) tea.Cmd {
	c.activeView = ViewResume
	c.navMode = NavList
	c.resume.SetSessions(sessions)
	c.selectedItem = 0
	c.scrollOffset = 0

	changeModeCmd := func() tea.Msg {
		return ChangeModeMsg{NewMode: "select"}
	}

	return changeModeCmd
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

// handleListNavigation handles navigation for list views (models, resume)
func (c *ContentComponent) handleListNavigation(msg tea.KeyMsg) tea.Cmd {
	var itemCount int
	var visibleSlots int

	switch c.activeView {
	case ViewModels:
		itemCount = c.models.GetItemCount()
		visibleSlots = c.models.GetVisibleSlots()
	case ViewResume:
		itemCount = c.resume.GetItemCount()
		visibleSlots = c.resume.GetVisibleSlots()
	default:
		return nil
	}

	var scrollInfoCmd tea.Cmd

	switch msg.String() {
	case "j", "down":
		var newIndex int
		if c.activeView == ViewModels {
			newIndex = c.models.NextSelectableIndex(c.selectedItem, IsModelSelectable)
		} else {
			if c.selectedItem < itemCount-1 {
				newIndex = c.selectedItem + 1
			} else {
				newIndex = c.selectedItem
			}
		}
		if newIndex != c.selectedItem {
			c.selectedItem = newIndex
			// Scroll if needed
			if c.selectedItem >= c.scrollOffset+visibleSlots {
				c.scrollOffset = c.selectedItem - visibleSlots + 1
			}
			scrollInfoCmd = c.getScrollInfoCmd()
		}
	case "k", "up":
		var newIndex int
		if c.activeView == ViewModels {
			newIndex = c.models.PrevSelectableIndex(c.selectedItem, IsModelSelectable)
		} else {
			if c.selectedItem > 0 {
				newIndex = c.selectedItem - 1
			} else {
				newIndex = c.selectedItem
			}
		}
		if newIndex != c.selectedItem {
			c.selectedItem = newIndex
			// Scroll if needed
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
		targetIndex := c.selectedItem + move
		if targetIndex >= itemCount {
			targetIndex = itemCount - 1
		}
		// For models, find the nearest selectable item at or after target
		if c.activeView == ViewModels {
			for i := targetIndex; i < itemCount; i++ {
				if IsModelSelectable(c.models.Items[i]) {
					targetIndex = i
					break
				}
			}
			// If no selectable item found forward, search backward
			if !IsModelSelectable(c.models.Items[targetIndex]) {
				for i := targetIndex; i >= 0; i-- {
					if IsModelSelectable(c.models.Items[i]) {
						targetIndex = i
						break
					}
				}
			}
		}
		c.selectedItem = targetIndex
		// Adjust scroll
		if c.selectedItem >= c.scrollOffset+visibleSlots {
			c.scrollOffset = c.selectedItem - visibleSlots + 1
		}
		scrollInfoCmd = c.getScrollInfoCmd()
	case "ctrl+u": // Half page up
		move := visibleSlots / 2
		if move < 1 {
			move = 1
		}
		targetIndex := c.selectedItem - move
		if targetIndex < 0 {
			targetIndex = 0
		}
		// For models, find the nearest selectable item at or before target
		if c.activeView == ViewModels {
			for i := targetIndex; i >= 0; i-- {
				if IsModelSelectable(c.models.Items[i]) {
					targetIndex = i
					break
				}
			}
			// If no selectable item found backward, search forward
			if !IsModelSelectable(c.models.Items[targetIndex]) {
				for i := targetIndex; i < itemCount; i++ {
					if IsModelSelectable(c.models.Items[i]) {
						targetIndex = i
						break
					}
				}
			}
		}
		c.selectedItem = targetIndex
		// Adjust scroll
		if c.selectedItem < c.scrollOffset {
			c.scrollOffset = c.selectedItem
		}
		scrollInfoCmd = c.getScrollInfoCmd()
	case "g", "home":
		if c.activeView == ViewModels {
			c.selectedItem = c.models.FirstSelectableIndex(IsModelSelectable)
		} else {
			c.selectedItem = 0
		}
		c.scrollOffset = 0
		scrollInfoCmd = c.getScrollInfoCmd()
	case "G", "end":
		if c.activeView == ViewModels {
			c.selectedItem = c.models.LastSelectableIndex(IsModelSelectable)
		} else {
			c.selectedItem = itemCount - 1
		}
		if c.selectedItem >= visibleSlots {
			c.scrollOffset = c.selectedItem - visibleSlots + 1
		}
		scrollInfoCmd = c.getScrollInfoCmd()
	case "enter":
		// Handle selection
		switch c.activeView {
		case ViewModels:
			if model := c.models.GetSelectedModel(c.selectedItem); model != nil && IsModelSelectable(*model) {
				showChatCmd := c.ShowChat()
				// If model has a custom OnSelect handler, use it instead of the default
				if model.OnSelect != nil {
					return tea.Batch(showChatCmd, model.OnSelect)
				}
				return tea.Batch(
					showChatCmd,
					func() tea.Msg {
						return modelSelectedMsg{model: model,
							onSelect: model.OnSelect}
					},
				)
			}
		case ViewResume:
			if session := c.resume.GetSelectedSession(c.selectedItem); session != nil {
				showChatCmd := c.ShowChat()
				return tea.Batch(
					showChatCmd,
					c.resume.LoadSession(session.ID),
				)
			}
		}
	}

	return scrollInfoCmd
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
