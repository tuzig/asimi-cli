package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/tools"
)

type runCmd struct{}

type promptCmd struct {
	Prompt string `arg:"" help:"Prompt to send to the LLM"`
}

type agentCmd struct {
	Prompt string `arg:"" help:"Prompt to send to the agent"`
}

type versionCmd struct{}

var cli struct {
	Version versionCmd `cmd:"" help:"Print version information"`

	// Add a default command to run the Bubble Tea app
	Run runCmd `cmd:"" default:"1" help:"Run the interactive application"`
	
	// Add prompt command
	Prompt promptCmd `cmd:"p" help:"Send a prompt to the configured LLM"`

	// Add agent command
	Agent agentCmd `cmd:"a" help:"Run the agent with the given prompt"`
}

func (v versionCmd) Run() error {
	fmt.Println("Asimi CLI v0.1.0")
	return nil
}

func (r runCmd) Run() error {
	// Check if we are running in a terminal
	// If not, print a message and exit
	// This is to prevent the program from hanging when run in non-interactive environments
	// like when running in a CI/CD pipeline or when the output is redirected
	// to a file or another program.
	// It's a good practice to check for this and provide a helpful message.
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsTerminal(os.Stdin.Fd()) {
		fmt.Println("This program requires a terminal to run.")
		fmt.Println("Please run it in a terminal emulator.")
		return nil
	}

	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Using defaults due to config load failure: %v\n", err)
		// Continue with default config
		config = &Config{
			Server: ServerConfig{
				Host: "localhost",
				Port: 3000,
			},
			Database: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "asimi",
				Password: "asimi",
				Name:     "asimi_dev",
			},
			Logging: LoggingConfig{
				Level:  "info",
				Format: "text",
			},
			LLM: LLMConfig{
				Provider: "openai",
				Model:    "gpt-3.5-turbo",
				APIKey:   "",
				BaseURL:  "",
			},
		}
	}

	// Get the LLM client
	llm, err := getLLMClient(config)
	if err != nil {
		return fmt.Errorf("failed to create LLM client: %w", err)
	}

	// Create the TUI model
	tuiModel := NewTUIModel(config, &llm)

	p := tea.NewProgram(tuiModel)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("alas, there's been an error: %w", err)
	}
	return nil
}

func (p promptCmd) Run() error {
	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	
	// Send the prompt to the LLM and print the response
	response, err := sendPrompt(config, p.Prompt)
	if err != nil {
		return fmt.Errorf("failed to send prompt to LLM: %w", err)
	}
	
	fmt.Println(response)
	return nil
}

func (a agentCmd) Run() error {
	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Get the LLM client
	llm, err := getLLMClient(config)
	if err != nil {
		return fmt.Errorf("failed to create LLM client: %w", err)
	}

	// Create tools for the agent
	agentTools := []tools.Tool{
		ReadFileTool{},
		WriteFileTool{},
		ListDirectoryTool{},
		ReplaceTextTool{},
	}

	// Create the agent
	agent := CreateAgent(llm, agentTools)

	// Create the executor
	executor := CreateExecutor(agent)

	// Execute the agent with the given prompt
	ctx := context.Background()
	result, err := executor.Call(ctx, map[string]any{
		"input": a.Prompt,
	})
	if err != nil {
		return fmt.Errorf("failed to execute agent: %w", err)
	}

	// Print the result
	output, ok := result["output"]
	if ok {
		fmt.Println(output)
	} else {
		fmt.Println("Agent completed successfully")
		for key, value := range result {
			fmt.Printf("%s: %v\n", key, value)
		}
	}

	return nil
}

// TUIModel represents the bubbletea model for the TUI
type TUIModel struct {
	config        *Config
	llm           *llms.Model
	width, height int
	
	// UI Components
	status         StatusComponent
	editor         EditorComponent
	messages       MessagesComponent
	fileViewer     *FileViewer
	completions    CompletionDialog
	toastManager   ToastManager
	modal          *BaseModal
	
	// UI Flags & State
	showCompletionDialog bool
	messagesRight        bool
	
	// Session state
	sessionActive bool
	
	// Command registry
	commandRegistry CommandRegistry
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
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			// Close any open dialogs or viewers
			if m.modal != nil {
				m.modal = nil
			}
			if m.fileViewer != nil && m.fileViewer.Active {
				m.fileViewer.Close()
			}
			if m.showCompletionDialog {
				m.showCompletionDialog = false
			}
		case "enter":
			// Submit the editor content or select completion
			if m.showCompletionDialog {
				// Get selected completion
				selected := m.completions.GetSelected()
				if selected != "" {
					// Add to editor
					current := m.editor.Value()
					// Find the last space or start of line
					lastSpace := strings.LastIndex(current, " ")
					if lastSpace == -1 {
						// Replace the whole content
						m.editor.SetValue(selected + " ")
					} else {
						// Replace from last space
						m.editor.SetValue(current[:lastSpace+1] + selected + " ")
					}
				}
				m.showCompletionDialog = false
				m.completions.Hide()
			} else {
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
								cmd.Handler(&m, parts[1:])
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
						
						// Simulate AI response (in a real implementation, this would call the LLM)
						m.messages.AddMessage("AI: I received your message!")
					}
				}
			}
		case "/":
			// Show completion dialog with commands
			m.showCompletionDialog = true
			var commandNames []string
			for name := range m.commandRegistry.Commands {
				commandNames = append(commandNames, name)
			}
			m.completions.SetOptions(commandNames)
			m.completions.Show()
		case "@":
			// Show completion dialog with files
			m.showCompletionDialog = true
			// In a real implementation, you would get actual file names
			m.completions.SetOptions([]string{"@main.go", "@config.go", "@llm.go", "@agent.go", "@tui.go"})
			m.completions.Show()
		case "tab":
			if m.showCompletionDialog {
				m.completions.SelectNext()
			}
		case "shift+tab":
			if m.showCompletionDialog {
				m.completions.SelectPrev()
			}
		case "ctrl+l":
			// Toggle messages layout
			m.messagesRight = !m.messagesRight
		}
		
	case tea.WindowSizeMsg:
		// Handle window resize
		m.width = msg.Width
		m.height = msg.Height
		
		// Update component dimensions
		m.updateComponentDimensions()
	}
	
	// Update components
	m.editor, _ = m.editor.Update(msg)
	m.messages, _ = m.messages.Update(msg)
	if m.fileViewer != nil && m.fileViewer.Active {
		m.fileViewer, _ = m.fileViewer.Update(msg)
	}
	
	return m, tea.Batch(cmds...)
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
	
	if m.messagesRight && m.fileViewer != nil && m.fileViewer.Active {
		// Split screen layout
		messagesWidth := m.width / 2
		viewerWidth := m.width - messagesWidth
		
		m.messages.SetWidth(messagesWidth)
		m.messages.SetHeight(messagesHeight)
		
		m.fileViewer.SetWidth(viewerWidth)
		m.fileViewer.SetHeight(messagesHeight)
	} else {
		// Full width layout
		m.messages.SetWidth(m.width)
		m.messages.SetHeight(messagesHeight)
		
		if m.fileViewer != nil {
			m.fileViewer.SetWidth(m.width)
			m.fileViewer.SetHeight(messagesHeight)
		}
	}
	
	m.editor.SetWidth(m.width)
	m.editor.SetHeight(editorHeight)
	
	// Update status info
	m.status.SetAgent(fmt.Sprintf("%s (%s)", m.config.LLM.Provider, m.config.LLM.Model))
	m.status.SetWorkingDir(".") // In a real implementation, get current working directory
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
func NewTUIModel(config *Config, llm *llms.Model) TUIModel {
	registry := NewCommandRegistry()
	
	model := TUIModel{
		config:       config,
		llm:          llm,
		width:        80,  // Default width
		height:       24,  // Default height
		
		// Initialize components
		status:       NewStatusComponent(80),
		editor:       NewEditorComponent(80, 5),
		messages:     NewMessagesComponent(80, 18),
		fileViewer:   NewFileViewer(80, 18),
		completions:  NewCompletionDialog(),
		toastManager: NewToastManager(),
		modal:        nil,
		
		// UI Flags
		showCompletionDialog: false,
		messagesRight:        false,
		
		// Session state
		sessionActive: false,
		
		// Command registry
		commandRegistry: registry,
	}
	
	// Set initial status info
	model.status.SetAgent(fmt.Sprintf("%s (%s)", config.LLM.Provider, config.LLM.Model))
	model.status.SetWorkingDir(".") // In a real implementation, get current working directory
	model.status.SetGitBranch("main") // In a real implementation, get current git branch
	
	return model
}

func main() {
	ctx := kong.Parse(&cli)

	// Kong will automatically run the default command when no subcommand is specified
	// so we don't need to manually check for it
	err := ctx.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
