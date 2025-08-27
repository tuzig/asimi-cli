package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// CompletionDialog represents the autocompletion pop-up
type CompletionDialog struct {
	Options           []string
	Selected          int
	Visible           bool
	Width             int
	Height            int
	PositionX         int
	PositionY         int
	Style             lipgloss.Style
	SelectedItemStyle lipgloss.Style
}

// NewCompletionDialog creates a new completion dialog
func NewCompletionDialog() CompletionDialog {
	return CompletionDialog{
		Options:   []string{},
		Selected:  0,
		Visible:   false,
		Width:     30,
		Height:    10,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("230")),
		SelectedItemStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")),
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
	if len(c.Options) > 0 {
		c.Selected = (c.Selected + 1) % len(c.Options)
	}
}

// SelectPrev moves selection to the previous item
func (c *CompletionDialog) SelectPrev() {
	if len(c.Options) > 0 {
		c.Selected--
		if c.Selected < 0 {
			c.Selected = len(c.Options) - 1
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
	if !c.Visible || len(c.Options) == 0 {
		return ""
	}

	// Create the content
	lines := make([]string, 0, len(c.Options))
	for i, option := range c.Options {
		if i == c.Selected {
			lines = append(lines, c.SelectedItemStyle.Render(option))
		} else {
			lines = append(lines, option)
		}
	}

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
	ta.Focus()

	// Set the dimensions
	ta.SetWidth(width - 2) // Account for borders
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

// FileViewer represents a file content viewer
type FileViewer struct {
	Viewport viewport.Model
	FilePath string
	Content  string
	Width    int
	Height   int
	Active   bool
	Style    lipgloss.Style
}

// NewFileViewer creates a new file viewer
func NewFileViewer(width, height int) *FileViewer {
	vp := viewport.New(width-2, height-2) // Account for borders

	return &FileViewer{
		Viewport: vp,
		Width:    width,
		Height:   height,
		Active:   false,
		Style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Width(width).
			Height(height),
	}
}

// SetWidth updates the width of the file viewer
func (fv *FileViewer) SetWidth(width int) {
	fv.Width = width
	fv.Style = fv.Style.Width(width)
	fv.Viewport.Width = width - 2
}

// SetHeight updates the height of the file viewer
func (fv *FileViewer) SetHeight(height int) {
	fv.Height = height
	fv.Style = fv.Style.Height(height)
	fv.Viewport.Height = height - 2
}

// LoadFile loads a file's content into the viewer
func (fv *FileViewer) LoadFile(filePath, content string) {
	fv.FilePath = filePath
	fv.Content = content
	fv.Viewport.SetContent(content)
	fv.Active = true
}

// Close closes the file viewer
func (fv *FileViewer) Close() {
	fv.FilePath = ""
	fv.Content = ""
	fv.Viewport.SetContent("")
	fv.Active = false
}

// Update handles messages for the file viewer
func (fv *FileViewer) Update(msg interface{}) (*FileViewer, interface{}) {
	var cmd interface{}
	fv.Viewport, cmd = fv.Viewport.Update(msg)
	return fv, cmd
}

// View renders the file viewer
func (fv *FileViewer) View() string {
	if !fv.Active {
		return ""
	}

	title := fv.FilePath
	if title == "" {
		title = "File Viewer"
	}

	// Create a header with the file path
	header := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1).
		Render(title)

	// Render the viewport with the header
	content := fv.Style.Render(fv.Viewport.View())

	return lipgloss.JoinVertical(lipgloss.Left, header, content)
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
		Width(m.Width - 2).
		Height(m.Height - 4). // Account for title and borders
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