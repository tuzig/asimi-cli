package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/chains"
)

// TUIModel represents the bubbletea model for the TUI
type TUIModel struct {
	config        *Config
	agent         *agents.Executor
	width, height int

	// UI Components
	status       StatusComponent
	prompt       PromptComponent
	chat         ChatComponent
	completions  CompletionDialog
	toastManager ToastManager
	modal        *BaseModal

	// UI Flags & State
	showCompletionDialog bool
	completionMode       string // "file" or "command"

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

// NewTUIModel creates a new TUI model
func NewTUIModel(config *Config, handler *toolCallbackHandler) *TUIModel {

	registry := NewCommandRegistry()

	model := &TUIModel{
		config: config,
		width:  80, // Default width
		height: 24, // Default height

		// Initialize components
		status:       NewStatusComponent(80),
		prompt:       NewPromptComponent(80, 5),
		chat:         NewChatComponent(80, 18),
		completions:  NewCompletionDialog(),
		toastManager: NewToastManager(),
		modal:        nil,

		// UI Flags
		showCompletionDialog: false,
		completionMode:       "",

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
						filePath := selected
						content, err := os.ReadFile(filePath)
						if err != nil {
							m.toastManager.AddToast(fmt.Sprintf("Error reading file: %v", err), "error", time.Second*3)
						} else {
							m.filesContentToSend[filePath] = string(content)
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
					m.updateFileCompletions()
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "enter":
			// Submit the prompt content
			content := m.prompt.Value()
			if content != "" {
				// TODO: move the slash command update to m.chat.Update()
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
					// move the this block to m.prompt.Update()
					m.chat.AddMessage(fmt.Sprintf("You: %s", content))
					m.sessionActive = true

					m.prompt.SetValue("")

					// Send the prompt to the agent
					cmds = append(cmds, func() tea.Msg {
						fullPrompt := content
						if len(m.filesContentToSend) > 0 {
							var fileContents []string
							for path, content := range m.filesContentToSend {
								fileContents = append(fileContents, fmt.Sprintf("--- Context from: %s ---\n%s\n--- End of Context from: %s ---", path, content, path))
							}
							fullPrompt = strings.Join(fileContents, "\n\n") + "\n" + content
							m.filesContentToSend = make(map[string]string)
						}
						outputs, err := chains.Call(context.Background(), m.agent, map[string]any{"input": fullPrompt})
						if err != nil {
							return errMsg{err}
						}
						out, ok := outputs["output"].(string)
						if !ok {
							return errMsg{fmt.Errorf("invalid agent output type")}
						}
						return responseMsg(out)
					})
				}
			}
		// Only trigger command completion when slash is at the start of the prompt
		case "/":
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
		case "@":
			m.prompt, _ = m.prompt.Update(msg)
			// Show completion dialog with files
			m.showCompletionDialog = true
			m.completionMode = "file"
			files, err := getFileTree(".")
			if err != nil {
				m.chat.AddMessage(fmt.Sprintf("Error scanning files: %v", err))
			} else {
				m.allFiles = files
				m.updateFileCompletions()
			}
			m.completions.Show()

		default:
			m.prompt, _ = m.prompt.Update(msg)
		}

	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			m.chat.Viewport.LineUp(1)
		case tea.MouseWheelDown:
			m.chat.Viewport.LineDown(1)
		}

	case tea.WindowSizeMsg:
		// Handle window resize
		m.width = msg.Width
		m.height = msg.Height

		// Update component dimensions
		m.updateComponentDimensions()

	case responseMsg:
		m.chat.AddMessage(fmt.Sprintf("AI: %s", string(msg)))

	case ToolCallScheduledMsg:
		m.chat.AddMessage(fmt.Sprintf("Tool Scheduled: %s", msg.Call.Tool.Name()))
	case ToolCallExecutingMsg:
		m.chat.ReplaceLastMessage(fmt.Sprintf("Tool Executing: %s", msg.Call.Tool.Name()))
	case ToolCallSuccessMsg:
		m.chat.ReplaceLastMessage(fmt.Sprintf("Tool Succeeded: %s", msg.Call.Tool.Name()))
	case ToolCallErrorMsg:
		m.chat.ReplaceLastMessage(fmt.Sprintf("Tool Errored: %s: %v", msg.Call.Tool.Name(), msg.Call.Error))

	case errMsg:
		m.chat.AddMessage(fmt.Sprintf("Error: %v", msg.err))

	case showHelpMsg:
		helpText := "Available commands:\n"
		for _, cmd := range m.commandRegistry.GetAllCommands() {
			helpText += fmt.Sprintf("  %s - %s\n", cmd.Name, cmd.Description)
		}
		m.chat.AddMessage(helpText)
		m.sessionActive = true
	}

	m.chat, _ = m.chat.Update(msg)

	return m, tea.Batch(cmds...)
}

func (m *TUIModel) updateFileCompletions() {
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
		options = append(options, file)
	}
	m.completions.SetOptions(options)
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
