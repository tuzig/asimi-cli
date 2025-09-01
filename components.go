package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/chains"
)

func getFileTree(root string) ([]string, error) {
	var files []string
	// Directories to ignore at any level
	ignoreDirs := map[string]bool{
		".git":    true,
		"vendor":  true,
		".asimi":  true,
		"archive": true,
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if ignoreDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// We only want files.
		// Let's make sure the path is relative to the root.
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, relPath)
		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

// CompletionDialog represents the autocompletion pop-up
type CompletionDialog struct {
	Options           []string
	Selected          int
	Visible           bool
	Width             int
	Height            int
	Offset            int
	PositionX         int
	PositionY         int
	Style             lipgloss.Style
	SelectedItemStyle lipgloss.Style
	ScrollMargin      int
}

// NewCompletionDialog creates a new completion dialog
func NewCompletionDialog() CompletionDialog {
	return CompletionDialog{
		Options:  []string{},
		Selected: 0,
		Visible:  false,
		Width:    30,
		Height:   10,
		Offset:   0,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("230")),
		SelectedItemStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")),
		ScrollMargin: 4,
	}
}

// SetOptions updates the completion options
func (c *CompletionDialog) SetOptions(options []string) {
	c.Options = options
	if c.Selected >= len(options) {
		c.Selected = len(options) - 1
	}
	if c.Selected < 0 {
		c.Selected = 0
	}
	c.Offset = 0
}

// Show makes the dialog visible
func (c *CompletionDialog) Show() {
	c.Visible = true
}

// Hide makes the dialog invisible
func (c *CompletionDialog) Hide() {
	c.Visible = false
}

// SelectNext moves selection to the next item
func (c *CompletionDialog) SelectNext() {
	slog.Info("Select Next")
	if len(c.Options) == 0 {
		return
	}
	next := c.Selected + 1
	if next >= len(c.Options) {
		return
	}
	slog.Info(">>>", "next", next, "offset", c.Offset, "height", c.Height)
	if next >= c.Offset+c.Height-c.ScrollMargin {
		if c.Offset < len(c.Options)-c.Height {
			c.Offset++
		}
	}
	c.Selected = next
}

// SelectPrev moves selection to the previous item
func (c *CompletionDialog) SelectPrev() {
	slog.Info("Select Prev")
	if c.Selected > 0 {
		c.Selected--
		slog.Info(">>>", "selected", c.Selected, "offset", c.Offset)
		if c.Selected < c.Offset+c.ScrollMargin {
			if c.Offset > 0 {
				c.Offset--
			}
		}
	}
}

// GetSelected returns the currently selected option
func (c CompletionDialog) GetSelected() string {
	if c.Selected >= 0 && c.Selected < len(c.Options) {
		return c.Options[c.Selected]
	}
	return ""
}

// View renders the completion dialog
func (c CompletionDialog) View() string {
	slog.Info("view")
	if !c.Visible || len(c.Options) == 0 {
		return ""
	}
	start := c.Offset
	end := c.Offset + c.Height
	slog.Info(">>>", "start", start, "end", end)
	lines := make([]string, 0, c.Height)
	for i := start; i < end; i++ {
		if i >= len(c.Options) {
			slog.Info("...", "i", i, "len", len(c.Options))
			lines = append(lines, "...")
			continue
		}
		option := c.Options[i]
		if i == c.Selected {
			lines = append(lines, c.SelectedItemStyle.Render(option))
		} else {
			lines = append(lines, option)
		}
	}

	slog.Info("lines", "len", len(lines))
	// Join the lines and render with style
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return c.Style.Render(content)
}

// EditorComponent represents the user input text area
type EditorComponent struct {
	TextArea    textarea.Model
	Placeholder string
	Height      int
	Width       int
	Style       lipgloss.Style
}

// NewEditorComponent creates a new editor component
func NewEditorComponent(width, height int) EditorComponent {
	ta := textarea.New()
	ta.Placeholder = "Type your message here..."
	ta.ShowLineNumbers = false
	ta.Focus()

	// Set the dimensions
	ta.SetWidth(width - 2)   // Account for borders
	ta.SetHeight(height - 2) // Account for borders

	return EditorComponent{
		TextArea: ta,
		Height:   height,
		Width:    width,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Width(width).
			Height(height),
	}
}

// SetWidth updates the width of the editor component
func (e *EditorComponent) SetWidth(width int) {
	e.Width = width
	e.Style = e.Style.Width(width)
	e.TextArea.SetWidth(width - 2)
}

// SetHeight updates the height of the editor component
func (e *EditorComponent) SetHeight(height int) {
	e.Height = height
	e.Style = e.Style.Height(height)
	e.TextArea.SetHeight(height - 2)
}

// SetValue sets the text value of the editor
func (e *EditorComponent) SetValue(value string) {
	e.TextArea.SetValue(value)
}

// Value returns the current text value
func (e EditorComponent) Value() string {
	return e.TextArea.Value()
}

// Focus gives focus to the editor
func (e *EditorComponent) Focus() {
	e.TextArea.Focus()
}

// Blur removes focus from the editor
func (e *EditorComponent) Blur() {
	e.TextArea.Blur()
}

// Update handles messages for the editor component
func (e EditorComponent) Update(msg interface{}) (EditorComponent, interface{}) {
	var cmd interface{}
	e.TextArea, cmd = e.TextArea.Update(msg)
	return e, cmd
}

// View renders the editor component
func (e EditorComponent) View() string {
	return e.Style.Render(e.TextArea.View())
}

// MessagesComponent represents the chat view
type MessagesComponent struct {
	Viewport viewport.Model
	Messages []string
	Width    int
	Height   int
	Style    lipgloss.Style
}

// NewMessagesComponent creates a new messages component
func NewMessagesComponent(width, height int) MessagesComponent {
	vp := viewport.New(width-2, height-2) // Account for borders
	vp.SetContent("Welcome to Asimi CLI! Send a message to start chatting.")

	return MessagesComponent{
		Viewport: vp,
		Messages: []string{"Welcome to Asimi CLI! Send a message to start chatting."},
		Width:    width,
		Height:   height,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Width(width).
			Height(height),
	}
}

// SetWidth updates the width of the messages component
func (m *MessagesComponent) SetWidth(width int) {
	m.Width = width
	m.Style = m.Style.Width(width)
	m.Viewport.Width = width - 2
	m.updateContent()
}

// SetHeight updates the height of the messages component
func (m *MessagesComponent) SetHeight(height int) {
	m.Height = height
	m.Style = m.Style.Height(height)
	m.Viewport.Height = height - 2
}

// AddMessage adds a new message to the messages component
func (m *MessagesComponent) AddMessage(message string) {
	m.Messages = append(m.Messages, message)
	m.updateContent()
}

// updateContent updates the viewport content based on the messages
func (m *MessagesComponent) updateContent() {
	content := strings.Join(m.Messages, "\n\n")
	m.Viewport.SetContent(content)
}

// Update handles messages for the messages component
func (m MessagesComponent) Update(msg interface{}) (MessagesComponent, interface{}) {
	var cmd interface{}
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}

// View renders the messages component
func (m MessagesComponent) View() string {
	return m.Style.Render(m.Viewport.View())
}

// BaseModal represents a base modal dialog
type BaseModal struct {
	Title   string
	Content string
	Width   int
	Height  int
	Style   lipgloss.Style
}

// NewBaseModal creates a new base modal
func NewBaseModal(title, content string, width, height int) *BaseModal {
	return &BaseModal{
		Title:   title,
		Content: content,
		Width:   width,
		Height:  height,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Width(width).
			Height(height).
			Align(lipgloss.Center, lipgloss.Center),
	}
}

// Render renders the modal
func (m *BaseModal) Render() string {
	// Create title style
	titleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1).
		Width(m.Width - 2) // Account for border

	title := titleStyle.Render(m.Title)
	content := lipgloss.NewStyle().
		Width(m.Width-2).
		Height(m.Height-4). // Account for title and borders
		Align(lipgloss.Center, lipgloss.Center).
		Render(m.Content)

	// Combine title and content
	body := lipgloss.JoinVertical(lipgloss.Center, title, content)

	return m.Style.Render(body)
}

// Update handles messages for the modal
func (m *BaseModal) Update(msg interface{}) (*BaseModal, interface{}) {
	// Base modal doesn't handle any messages
	return m, nil
}

// StatusComponent represents the status bar component
type StatusComponent struct {
	Agent      string
	WorkingDir string
	GitBranch  string
	Width      int
	Style      lipgloss.Style
}

// NewStatusComponent creates a new status component
func NewStatusComponent(width int) StatusComponent {
	return StatusComponent{
		Width: width,
		Style: lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1).
			Width(width),
	}
}

// SetAgent sets the current agent
func (s *StatusComponent) SetAgent(agent string) {
	s.Agent = agent
}

// SetWorkingDir sets the current working directory
func (s *StatusComponent) SetWorkingDir(dir string) {
	s.WorkingDir = dir
}

// SetGitBranch sets the current git branch
func (s *StatusComponent) SetGitBranch(branch string) {
	s.GitBranch = branch
}

// SetWidth updates the width of the status component
func (s *StatusComponent) SetWidth(width int) {
	s.Width = width
	s.Style = s.Style.Width(width)
}

// View renders the status component
func (s StatusComponent) View() string {
	statusText := fmt.Sprintf("Agent: %s | Dir: %s | Branch: %s",
		s.Agent, s.WorkingDir, s.GitBranch)

	// Truncate or pad the status text to fit the width
	if len(statusText) > s.Width {
		// Truncate with ellipsis
		statusText = statusText[:s.Width-3] + "..."
	}

	return s.Style.Render(statusText)
}

// Toast represents a single toast notification
type Toast struct {
	ID      string
	Message string
	Type    string // info, success, warning, error
	Created time.Time
	Timeout time.Duration
}

// ToastManager manages toast notifications
type ToastManager struct {
	Toasts []Toast
	Style  lipgloss.Style
}

// NewToastManager creates a new toast manager
func NewToastManager() ToastManager {
	return ToastManager{
		Toasts: make([]Toast, 0),
		Style: lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1).
			MaxWidth(50),
	}
}

// AddToast adds a new toast notification
func (tm *ToastManager) AddToast(message, toastType string, timeout time.Duration) {
	toast := Toast{
		ID:      time.Now().String(), // Simple ID generation
		Message: message,
		Type:    toastType,
		Created: time.Now(),
		Timeout: timeout,
	}

	tm.Toasts = append(tm.Toasts, toast)
}

// RemoveToast removes a toast by ID
func (tm *ToastManager) RemoveToast(id string) {
	for i, toast := range tm.Toasts {
		if toast.ID == id {
			// Remove the toast at index i
			tm.Toasts = append(tm.Toasts[:i], tm.Toasts[i+1:]...)
			break
		}
	}
}

// Update handles updating the toast manager (e.g., removing expired toasts)
func (tm ToastManager) Update() ToastManager {
	now := time.Now()
	activeToasts := make([]Toast, 0)

	for _, toast := range tm.Toasts {
		// Check if toast is still valid
		if now.Sub(toast.Created) < toast.Timeout {
			activeToasts = append(activeToasts, toast)
		}
	}

	tm.Toasts = activeToasts
	return tm
}

// View renders the active toasts
func (tm ToastManager) View() string {
	if len(tm.Toasts) == 0 {
		return ""
	}

	// For now, just show the most recent toast
	// In a full implementation, you might show multiple toasts
	toast := tm.Toasts[len(tm.Toasts)-1]

	// Apply different styles based on toast type
	style := tm.Style
	switch toast.Type {
	case "success":
		style = style.Background(lipgloss.Color("76")) // Green
	case "warning":
		style = style.Background(lipgloss.Color("11")) // Yellow
	case "error":
		style = style.Background(lipgloss.Color("124")) // Red
	}
	return style.Render(toast.Message)
}

// TUIModel represents the bubbletea model for the TUI
type TUIModel struct {
	config        *Config
	agent         *agents.Executor
	width, height int

	// UI Components
	status       StatusComponent
	editor       EditorComponent
	messages     MessagesComponent
	completions  CompletionDialog
	toastManager ToastManager
	modal        *BaseModal

	// UI Flags & State
	showCompletionDialog bool
	completionMode       string // "file" or "command"
	messagesRight        bool

	// Session state
	sessionActive      bool
	filesContentToSend map[string]string

	// Command registry
	commandRegistry CommandRegistry

	// Scheduler
	scheduler        *CoreToolScheduler
	userMessageQueue []string
	allFiles         []string
}

// Init implements bubbletea.Model
func (m TUIModel) Init() tea.Cmd {
	// Initialize the TUI
	return nil
}

// Update implements bubbletea.Model
func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Always handle Ctrl+C and Esc first
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.modal != nil {
				m.modal = nil
			}
			if m.showCompletionDialog {
				m.showCompletionDialog = false
				m.completions.Hide()
				m.completionMode = ""
			}
			return m, nil
		}

		if m.showCompletionDialog {
			switch msg.String() {
			case "enter", "tab":
				// Get selected completion
				selected := m.completions.GetSelected()
				if selected != "" {
					if m.completionMode == "file" {
						// It's a file completion
						filePath := selected
						content, err := os.ReadFile(filePath)
							if err != nil {
								m.messages.AddMessage(fmt.Sprintf("Error reading file: %v", err))
							} else {
								m.filesContentToSend[filePath] = string(content)
								m.messages.AddMessage(fmt.Sprintf("Loaded file: %s", filePath))
							}
						currentValue := m.editor.Value()
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
							m.editor.SetValue(strings.TrimSpace(newValue) + " ")
						} else {
							// Fallback, though we should always find an @
							m.editor.SetValue("@" + selected + " ")
						}
					} else if m.completionMode == "command" {
						// It's a command completion
						cmd, exists := m.commandRegistry.GetCommand(selected)
						if exists {
							// Execute command
							cmds = append(cmds, cmd.Handler(&m, []string{}))
						}
						m.editor.SetValue("")
					}
				}
				m.showCompletionDialog = false
				m.completions.Hide()
				m.completionMode = ""
				return m, tea.Batch(cmds...)
			case "down":
				m.completions.SelectNext()
				return m, nil
			case "up":
				m.completions.SelectPrev()
				return m, nil
			default:
				// Any other key press updates the completion list
				m.editor, _ = m.editor.Update(msg)
				if m.completionMode == "file" {
					m.updateFileCompletions()
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "q":
		case "enter":
			// Submit the editor content
			content := m.editor.Value()
			if content != "" {
				// Check if it's a command
				if content[0] == '/' {
					// Handle command
					parts := strings.Fields(content)
					if len(parts) > 0 {
						cmdName := parts[0]
						cmd, exists := m.commandRegistry.GetCommand(cmdName)
						if exists {
							// Execute command
							command := cmd.Handler(&m, parts[1:])
							cmds = append(cmds, command)
							m.editor.SetValue("")
						} else {
							m.messages.AddMessage(fmt.Sprintf("Unknown command: %s", cmdName))
						}
					}
				} else {
					// Add user message to messages
					m.messages.AddMessage(fmt.Sprintf("You: %s", content))

					// Mark session as active
					m.sessionActive = true

					// Clear editor
				m.editor.SetValue("")

					// Send the prompt to the agent
					cmds = append(cmds, func() tea.Msg {
						fullPrompt := content
						if len(m.filesContentToSend) > 0 {
							var fileContents []string
							for path, content := range m.filesContentToSend {
								fileContents = append(fileContents, fmt.Sprintf("---\n%s---\n%s", path, content))
							}
							fullPrompt = strings.Join(fileContents, "\n\n") + "\n" + content
							m.filesContentToSend = make(map[string]string)
						}
						response, err := chains.Run(context.Background(), m.agent, fullPrompt)
						if err != nil {
							return errMsg{err}
						}
						return responseMsg(response)
					})
				}
			}
		case "/":
			m.editor, _ = m.editor.Update(msg)
			// Show completion dialog with commands
			m.showCompletionDialog = true
			m.completionMode = "command"
			var commandNames []string
			for name := range m.commandRegistry.Commands {
				commandNames = append(commandNames, name)
			}
			sort.Strings(commandNames)
			m.completions.SetOptions(commandNames)
			m.completions.Show()
		case "@":
			m.editor, _ = m.editor.Update(msg)
			// Show completion dialog with files
			m.showCompletionDialog = true
			m.completionMode = "file"
			files, err := getFileTree(".")
			if err != nil {
				m.messages.AddMessage(fmt.Sprintf("Error scanning files: %v", err))
			} else {
				m.allFiles = files
				m.updateFileCompletions()
			}
			m.completions.Show()

		case "ctrl+l":
			// Toggle messages layout
			m.messagesRight = !m.messagesRight
		default:
			m.editor, _ = m.editor.Update(msg)
		}

	case tea.WindowSizeMsg:
		// Handle window resize
		m.width = msg.Width
		m.height = msg.Height

		// Update component dimensions
		m.updateComponentDimensions()

	case responseMsg:
		m.messages.AddMessage(fmt.Sprintf("AI: %s", string(msg)))

	case ToolCallScheduledMsg:
		m.messages.AddMessage(fmt.Sprintf("Tool Scheduled: %s", msg.Call.Tool.Name()))
	case ToolCallExecutingMsg:
		m.messages.AddMessage(fmt.Sprintf("Tool Executing: %s", msg.Call.Tool.Name()))
	case ToolCallSuccessMsg:
		m.messages.AddMessage(fmt.Sprintf("Tool Succeeded: %s", msg.Call.Tool.Name()))
	case ToolCallErrorMsg:
		m.messages.AddMessage(fmt.Sprintf("Tool Errored: %s: %v", msg.Call.Tool.Name(), msg.Call.Error))

	case errMsg:
		m.messages.AddMessage(fmt.Sprintf("Error: %v", msg.err))

	case showHelpMsg:
		helpText := "Available commands:\n"
		for _, cmd := range m.commandRegistry.GetAllCommands() {
			helpText += fmt.Sprintf("  %s - %s\n", cmd.Name, cmd.Description)
		}
		m.messages.AddMessage(helpText)
		m.sessionActive = true
	}

	m.messages, _ = m.messages.Update(msg)

	return m, tea.Batch(cmds...)
}


func (m *TUIModel) updateFileCompletions() {
	inputValue := m.editor.Value()
	parts := strings.Split(inputValue, "@")
	if len(parts) < 2 {
		m.completions.SetOptions([]string{})
		return
	}
	// TODO: ensure this works well by adding a test for two file inclusing in one input
	searchQuery := parts[len(parts)-1]

	var filteredFiles []string
	for _, file := range m.allFiles {
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
		// TODO: add a nerdfont emojie based on file type
		options = append(options, file)
	}
	m.completions.SetOptions(options)
}

// updateComponentDimensions updates the dimensions of all components based on the window size
func (m *TUIModel) updateComponentDimensions() {
	// Calculate dimensions for a typical layout:
	// - Status bar: 1 line at bottom
	// - Editor: 5 lines at bottom
	// - Messages/File viewer: remaining space

	statusHeight := 1
	editorHeight := 5
	messagesHeight := m.height - statusHeight - editorHeight

	// Update components
	m.status.SetWidth(m.width)

	// Full width layout
	m.messages.SetWidth(m.width)
	m.messages.SetHeight(messagesHeight)

	m.editor.SetWidth(m.width)
	m.editor.SetHeight(editorHeight)

	// Update status info
	m.status.SetAgent(fmt.Sprintf("%s (%s)", m.config.LLM.Provider, m.config.LLM.Model))
	m.status.SetWorkingDir(".")   // In a real implementation, get current working directory
	m.status.SetGitBranch("main") // In a real implementation, get current git branch
}

// View implements bubbletea.Model
func (m TUIModel) View() string {
	// If we don't have dimensions yet, return empty
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Render the appropriate view based on session state
	var mainContent string
	if !m.sessionActive {
		// Home view
		mainContent = RenderHomeView(m.width, m.height-6) // Account for editor and status
	} else {
		// Chat view
		// In a real implementation, you would pass the actual messages
		mainContent = RenderChatView(m.messages.Messages, m.width, m.height-6)
	}

	// Build the full view
	view := lipgloss.JoinVertical(
		lipgloss.Left,
		mainContent,
		m.editor.View(),
		m.status.View(),
	)

	// Add completion dialog if visible
	if m.showCompletionDialog {
		// Position the completion dialog above the editor
		// In a real implementation, you would calculate the exact position
		dialog := m.completions.View()
		if dialog != "" {
			view = lipgloss.JoinVertical(lipgloss.Left, view, dialog)
		}
	}

	// Add modal if active
	if m.modal != nil {
		modalView := m.modal.Render()
		// In a real implementation, you would overlay the modal on top of the main view
		view = lipgloss.JoinVertical(lipgloss.Left, view, modalView)
	}

	// Add toast notifications
	toastView := m.toastManager.View()
	if toastView != "" {
		// In a real implementation, you would position the toast appropriately
		view = lipgloss.JoinVertical(lipgloss.Left, view, toastView)
	}

	return view
}

// NewTUIModel creates a new TUI model
func NewTUIModel(config *Config, handler *toolCallbackHandler) *TUIModel {

	registry := NewCommandRegistry()

	model := &TUIModel{
		config: config,
		width:  80, // Default width
		height: 24, // Default height

		// Initialize components
		status:       NewStatusComponent(80),
		editor:       NewEditorComponent(80, 5),
		messages:     NewMessagesComponent(80, 18),
		completions:  NewCompletionDialog(),
		toastManager: NewToastManager(),
		modal:        nil,

		// UI Flags
		showCompletionDialog: false,
		completionMode:       "",
		messagesRight:        false,

		// Session state
		sessionActive:      false,
		filesContentToSend: make(map[string]string),

		// Command registry
		commandRegistry: registry,

		// Scheduler
		userMessageQueue: make([]string, 0),
	}

	// Set initial status info
	model.status.SetAgent(fmt.Sprintf("%s (%s)", config.LLM.Provider, config.LLM.Model))
	model.status.SetWorkingDir(".")   // In a real implementation, get current working directory
	model.status.SetGitBranch("main") // In a real implementation, get current git branch

	return model
}
