package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

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
	chat           ChatComponent
	completions    CompletionDialog
	toastManager   ToastManager
	modal          *BaseModal
	providerModal  *ProviderSelectionModal
	codeInputModal *CodeInputModal

	// UI Flags & State
	showCompletionDialog bool
	completionMode       string // "file" or "command"
	sessionActive        bool

	// Streaming state
	streamingActive bool
	streamingCancel context.CancelFunc

	// Command registry
	commandRegistry CommandRegistry

	// Application services (passed in, not owned)
	session *Session
}

// NewTUIModel creates a new TUI model
func NewTUIModel(config *Config) *TUIModel {

	registry := NewCommandRegistry()
	theme := NewTheme()

	model := &TUIModel{
		config: config,
		width:  80, // Default width
		height: 24, // Default height
		theme:  theme,

		// Initialize components
		status:         NewStatusComponent(80),
		prompt:         NewPromptComponent(80, 5),
		chat:           NewChatComponent(80, 18),
		completions:    NewCompletionDialog(),
		toastManager:   NewToastManager(),
		modal:          nil,
		providerModal:  nil,
		codeInputModal: nil,

		// UI Flags
		showCompletionDialog: false,
		completionMode:       "",
		sessionActive:        false,

		// Command registry
		commandRegistry: registry,

		// Application services (injected)
		session: nil,
	}

	// Set initial status info - show disconnected state initially
	model.status.SetAgent(fmt.Sprintf("🔌 %s (%s)", config.LLM.Provider, config.LLM.Model))
	model.status.SetWorkingDir(".")   // In a real implementation, get current working directory
	model.status.SetGitBranch("main") // In a real implementation, get current git branch

	return model
}

// SetSession sets the session for the TUI model
func (m *TUIModel) SetSession(session *Session) {
	m.session = session
	if session != nil {
		m.status.SetAgent(fmt.Sprintf("✅ %s (%s)", m.config.LLM.Provider, m.config.LLM.Model))
	} else {
		m.status.SetAgent(fmt.Sprintf("🔌 %s (%s)", m.config.LLM.Provider, m.config.LLM.Model))
	}
}

// reinitializeSession recreates the LLM client and session with current config
func (m *TUIModel) reinitializeSession() error {
	// Get the LLM client with the updated config
	llm, err := getLLMClient(m.config)
	if err != nil {
		return fmt.Errorf("failed to create LLM client: %w", err)
	}

	// Create a new session with the LLM
	sess, err := NewSession(llm, m.config, func(msg any) {
		if program != nil {
			program.Send(msg)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	// Set the new session
	m.SetSession(sess)
	return nil
}

// Init implements bubbletea.Model
func (m TUIModel) Init() tea.Cmd {
	// Initialize the TUI
	return nil
}

// Update implements bubbletea.Model
func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Update toast manager to remove expired toasts
	m.toastManager.Update()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)

	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)

	default:
		return m.handleCustomMessages(msg)
	}
}

// handleKeyMsg processes keyboard input
func (m TUIModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Always handle Ctrl+C first
	var cmd tea.Cmd

	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// Handle modals first (they need to handle their own escape keys)
	if m.codeInputModal != nil {
		m.codeInputModal, cmd = m.codeInputModal.Update(msg)
		return m, cmd
	}
	if m.providerModal != nil {
		m.providerModal, cmd = m.providerModal.Update(msg)
		return m, cmd
	}

	// Handle escape key after modals have had a chance to process it
	if msg.String() == "esc" {
		return m.handleEscape()
	}

	// Handle completion dialog
	if m.showCompletionDialog {
		return m.handleCompletionDialog(msg)
	}

	// Handle regular key input
	switch msg.String() {
	case "enter":
		return m.handleEnterKey()
	case "/":
		return m.handleSlashKey(msg)
	case "@":
		return m.handleAtKey(msg)
	default:
		m.prompt, _ = m.prompt.Update(msg)
		return m, nil
	}

}

// handleEscape handles the escape key
func (m TUIModel) handleEscape() (tea.Model, tea.Cmd) {
	// Check if streaming is active first - cancel streaming via context
	if m.streamingActive && m.streamingCancel != nil {
		slog.Info("escape_during_streaming", "cancelling_context", true)
		m.streamingCancel()
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
		m.prompt, _ = m.prompt.Update(msg)
		if m.completionMode == "file" {
			files, err := getFileTree(".")
			if err == nil {
				m.updateFileCompletions(files)
			}
		} else if m.completionMode == "command" {
			m.updateCommandCompletions()
		}
		return m, nil
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
				m.toastManager.AddToast(fmt.Sprintf("Error reading file: %v", err), "error", time.Second*3)
			} else if m.session != nil {
				m.session.AddContextFile(filePath, string(content))
				m.chat.AddMessage(fmt.Sprintf("Loaded file: %s", filePath))
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
			// It's a command completion
			cmd, exists := m.commandRegistry.GetCommand(selected)
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

// handleEnterKey handles the enter key press
func (m TUIModel) handleEnterKey() (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	content := m.prompt.Value()
	if content == "" {
		return m, nil
	}
	if strings.HasPrefix(content, "/") {
		parts := strings.Fields(content)
		if len(parts) > 0 {
			cmdName := parts[0]
			cmd, exists := m.commandRegistry.GetCommand(cmdName)
			if exists {
				command := cmd.Handler(&m, parts[1:])
				cmds = append(cmds, command)
				m.prompt.SetValue("")
			} else {
				m.toastManager.AddToast(fmt.Sprintf("Unknown command: %s", cmdName), "error", time.Second*3)
			}
		}
	} else {
		m.chat.AddMessage(fmt.Sprintf("You: %s", content))
		if m.session != nil {
			m.sessionActive = true
			m.prompt.SetValue("")
			ctx, cancel := context.WithCancel(context.Background())
			m.streamingCancel = cancel
			m.session.AskStream(ctx, content)
		} else {
			m.toastManager.AddToast("No LLM configured. Please use /login to configure an API key.", "error", time.Second*5)
			m.prompt.SetValue("")
		}
	}
	return m, tea.Batch(cmds...)
}

// handleSlashKey handles the slash key for command completion
func (m TUIModel) handleSlashKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Only show command completion if we're at the beginning of the input
	if m.prompt.Value() == "" {
		m.prompt, _ = m.prompt.Update(msg)
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
	} else {
		m.prompt, _ = m.prompt.Update(msg)
	}
	return m, nil
}

// handleAtKey handles the @ key for file completion
func (m TUIModel) handleAtKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.prompt, _ = m.prompt.Update(msg)
	// Show completion dialog with files
	m.showCompletionDialog = true
	m.completionMode = "file"
	files, err := getFileTree(".")
	if err != nil {
		m.chat.AddMessage(fmt.Sprintf("Error scanning files: %v", err))
	} else {
		m.updateFileCompletions(files)
	}
	m.completions.Show()
	return m, nil
}

// handleMouseMsg handles mouse events
func (m TUIModel) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.MouseWheelUp:
		m.chat.Viewport.LineUp(1)
	case tea.MouseWheelDown:
		m.chat.Viewport.LineDown(1)
	}
	return m, nil
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
	case responseMsg:
		m.chat.AddMessage(fmt.Sprintf("AI: %s", string(msg)))

	case ToolCallScheduledMsg:
		m.chat.AddMessage(fmt.Sprintf("Tool Scheduled: %s with input: %s", msg.Call.Tool.Name(), msg.Call.Input))
	case ToolCallExecutingMsg:
		m.chat.AddMessage(fmt.Sprintf("Tool Executing: %s with input: %s", msg.Call.Tool.Name(), msg.Call.Input))
	case ToolCallSuccessMsg:
		m.chat.AddMessage(fmt.Sprintf("Tool Succeeded: %s\nInput: %s\nOutput: %s", msg.Call.Tool.Name(), msg.Call.Input, msg.Call.Result))
	case ToolCallErrorMsg:
		m.chat.AddMessage(fmt.Sprintf("Tool Errored: %s\nInput: %s\nError: %v", msg.Call.Tool.Name(), msg.Call.Input, msg.Call.Error))

	case errMsg:
		m.chat.AddMessage(fmt.Sprintf("Error: %v", msg.err))

	case streamStartMsg:
		// Streaming has started
		slog.Debug("streamStartMsg", "starting_stream", true)
		m.streamingActive = true

	case streamChunkMsg:
		// For the first chunk, add a new AI message. For subsequent chunks, append to the last message.
		slog.Debug("streamChunkMsg", "messages_count", len(m.chat.Messages), "chunk", string(msg))
		if len(m.chat.Messages) == 0 || !strings.HasPrefix(m.chat.Messages[len(m.chat.Messages)-1], "AI:") {
			m.chat.AddMessage(fmt.Sprintf("AI: %s", string(msg)))
			slog.Info("added_new_message", "total_messages", len(m.chat.Messages))
		} else {
			m.chat.AppendToLastMessage(string(msg))
			slog.Info("appended_to_last_message", "total_messages", len(m.chat.Messages))
		}

	case streamCompleteMsg:
		// Streaming is complete, mark session as inactive
		slog.Debug("streamCompleteMsg", "messages_count", len(m.chat.Messages))
		m.streamingActive = false
		m.streamingCancel = nil

	case streamInterruptedMsg:
		// Streaming was interrupted by user
		slog.Debug("streamInterruptedMsg", "partial_content_length", len(msg.partialContent))
		m.streamingActive = false
		m.streamingCancel = nil
		m.chat.AddMessage("\nESC")

	case streamErrorMsg:
		slog.Error("streamErrorMsg", "error", msg.err)
		m.chat.AddMessage(fmt.Sprintf("LLM Error: %v", msg.err))
		m.streamingActive = false
		m.streamingCancel = nil

	case showHelpMsg:
		helpText := "Available commands:\n"
		for _, cmd := range m.commandRegistry.GetAllCommands() {
			helpText += fmt.Sprintf("  %s - %s\n", cmd.Name, cmd.Description)
		}
		m.chat.AddMessage(helpText)
		m.sessionActive = true

	case providerSelectedMsg:
		m.providerModal = nil
		provider := msg.provider.Key

		// Handle Anthropic specially - show code input modal immediately
		if provider == "anthropic" {
			auth := &AuthAnthropic{}
			authURL, verifier, err := auth.authorize()
			if err != nil {
				m.toastManager.AddToast(fmt.Sprintf("Auth failed: %v", err), "error", 4000)
				return m, nil
			}

			// Open browser
			if err := openBrowser(authURL); err != nil {
				m.toastManager.AddToast("Failed to open browser", "warning", 3000)
			}

			// Show code input modal
			m.codeInputModal = NewCodeInputModal(authURL, verifier)
			m.config.LLM.Provider = provider
			m.config.LLM.Model = "claude-3-5-sonnet-latest"
			m.toastManager.AddToast("Logged in", "success", 3000)
		} else {
			// Other providers use the standard OAuth flow
			return m, m.performOAuthLogin(provider)
		}

	case modalCancelledMsg:
		m.providerModal = nil
		m.codeInputModal = nil
		m.toastManager.AddToast("Login cancelled", "info", 2000)

	case authCodeEnteredMsg:
		m.codeInputModal = nil
		return m, m.completeAnthropicOAuth(msg.code, msg.verifier)
	}

	m.chat, _ = m.chat.Update(msg)

	return m, nil
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

	// Extract the command being typed (everything after the first "/")
	if !strings.HasPrefix(inputValue, "/") {
		m.completions.SetOptions([]string{})
		return
	}

	// Get the partial command name (without the leading "/")
	searchQuery := strings.ToLower(inputValue[1:])

	// Get all command names and filter them
	var filteredCommands []string
	for name := range m.commandRegistry.Commands {
		// Check if the command starts with the search query
		if strings.HasPrefix(strings.ToLower(name[1:]), searchQuery) { // name already includes "/"
			filteredCommands = append(filteredCommands, name)
		}
	}

	// Sort the filtered commands
	sort.Strings(filteredCommands)

	m.completions.SetOptions(filteredCommands)
}

// updateComponentDimensions updates the dimensions of all components based on the window size
func (m *TUIModel) updateComponentDimensions() {
	// Calculate dimensions for a typical layout:
	// - Status bar: 1 line at bottom
	// - prompt: 5 lines at bottom
	// - chat/File viewer: remaining space

	statusHeight := 1
	promptHeight := 2
	width := m.width - 2
	chatHeight := m.height - statusHeight - promptHeight - 4

	// Update components
	m.status.SetWidth(width + 1)

	// Full width layout
	m.chat.SetWidth(width)
	m.chat.SetHeight(chatHeight)

	m.prompt.SetWidth(width)
	m.prompt.SetHeight(promptHeight)

	// Update status info
	if m.session != nil {
		m.status.SetAgent(fmt.Sprintf("✅ %s (%s)", m.config.LLM.Provider, m.config.LLM.Model))
	} else {
		m.status.SetAgent(fmt.Sprintf("🔌 %s (%s)", m.config.LLM.Provider, m.config.LLM.Model))
	}
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
		mainContent = renderHomeView(m.width, m.height-6) // Account for prompt and status
	} else {
		// Chat view
		mainContent = m.chat.View()
	}

	// Build the full view
	view := lipgloss.JoinVertical(
		lipgloss.Left,
		mainContent,
		m.prompt.View(),
		m.status.View(),
	)

	// Add completion dialog if visible
	if m.showCompletionDialog {
		// Position the completion dialog above the prompt
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

	// Add provider modal if active (show modal over existing view)
	if m.providerModal != nil {
		modalView := m.providerModal.Render()
		view = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modalView)
	}

	// Add code input modal if active (takes priority over provider modal)
	if m.codeInputModal != nil {
		modalView := m.codeInputModal.Render()
		view = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modalView)
	}

	// Add toast notifications
	toastView := m.toastManager.View()
	if toastView != "" {
		// In a real implementation, you would position the toast appropriately
		view = lipgloss.JoinVertical(lipgloss.Left, view, toastView)
	}

	return view
}

// renderHomeView renders the home view when no session is active
func renderHomeView(width, height int) string {
	// Create a stylish welcome message
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("62")).
		Align(lipgloss.Center).
		Width(width)

	title := titleStyle.Render("Asimi CLI - Interactive Coding Agent")

	// Create a subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Align(lipgloss.Center).
		Width(width)

	subtitle := subtitleStyle.Render("Your AI-powered coding assistant")

	// Create a list of helpful commands
	commands := []string{
		"▶ Type a message and press Enter to chat",
		"▶ Use / to access commands (e.g., /help, /new)",
		"▶ Use @ to reference files (e.g., @main.go)",
		"▶ Press Ctrl+C or Q to quit",
	}

	// Style for commands
	commandStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		PaddingLeft(2)

	// Render commands
	var commandViews []string
	for _, command := range commands {
		commandViews = append(commandViews, commandStyle.Render(command))
	}

	commandsView := lipgloss.JoinVertical(lipgloss.Left, commandViews...)

	// Center the content vertically
	content := lipgloss.JoinVertical(lipgloss.Center, title, "", subtitle, "", commandsView)

	// Create a container that centers the content
	container := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)

	return container
}
