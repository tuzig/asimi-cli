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
	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/schema"
	"github.com/tmc/langchaingo/tools"
)

type runCmd struct{}

type versionCmd struct{}

var cli struct {
	Version versionCmd `cmd:"" help:"Print version information"`
	Prompt  string     `short:"p" help:"Prompt to send to the agent"`

	// Add a default command to run the Bubble Tea app
	Run runCmd `cmd:"" default:"1" help:"Run the interactive application"`
}

func (v versionCmd) Run() error {
	fmt.Println("Asimi CLI v0.1.0")
	return nil
}

func (r *runCmd) Run() error {
	// This command will only be run when no prompt is provided.
	// The logic in main() will handle the non-interactive case.

	// Check if we are running in a terminal
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsTerminal(os.Stdin.Fd()) {
		fmt.Println("This program requires a terminal to run.")
		fmt.Println("Please run it in a terminal emulator.")
		return nil
	}

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

	// Create the TUI model
	handler := &toolCallbackHandler{}
	tuiModel := NewTUIModel(config, handler)

	p := tea.NewProgram(tuiModel)
	handler.p = p
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("alas, there's been an error: %w", err)
	}
	return nil
}

type responseMsg string
type errMsg struct{ err error }
type toolStartMsg struct{ name string }
type toolEndMsg struct{ result string }

type toolCallbackHandler struct {
	p *tea.Program
}

func (h *toolCallbackHandler) HandleToolStart(ctx context.Context, input string) {
	h.p.Send(toolStartMsg{name: "unknown"})
}
func (h *toolCallbackHandler) HandleToolEnd(ctx context.Context, output string) {
	if h.p != nil {
		h.p.Send(toolEndMsg{result: output})
	}
}
func (h *toolCallbackHandler) HandleLLMStart(ctx context.Context, prompts []string) {}
func (h *toolCallbackHandler) HandleLLMError(ctx context.Context, err error)         {}
func (h *toolCallbackHandler) HandleChainError(ctx context.Context, err error)       {}
func (h *toolCallbackHandler) HandleStreamingFunc(ctx context.Context, chunk []byte) {}
func (h *toolCallbackHandler) HandleLLMGenerateContentEnd(ctx context.Context, res *llms.ContentResponse) {
}
func (h *toolCallbackHandler) HandleLLMGenerateContentStart(ctx context.Context, ms []llms.MessageContent) {
}
func (h *toolCallbackHandler) HandleText(ctx context.Context, text string)            {}
func (h *toolCallbackHandler) HandleToolError(ctx context.Context, err error)         {}
func (h *toolCallbackHandler) HandleRetrieverStart(ctx context.Context, query string) {}
func (h *toolCallbackHandler) HandleRetrieverEnd(ctx context.Context, query string, documents []schema.Document) {
}
func (h *toolCallbackHandler) HandleChainEnd(ctx context.Context, outputs map[string]any)       {}
func (h *toolCallbackHandler) HandleChainStart(ctx context.Context, inputs map[string]any)      {}
func (h *toolCallbackHandler) HandleAgentAction(ctx context.Context, action schema.AgentAction) {}
func (h *toolCallbackHandler) HandleAgentFinish(ctx context.Context, finish schema.AgentFinish) {}

type toolWrapper struct {
	t       tools.Tool
	handler callbacks.Handler
}

func (tw *toolWrapper) Name() string {
	return tw.t.Name()
}

func (tw *toolWrapper) Description() string {
	return tw.t.Description()
}

func (tw *toolWrapper) Call(ctx context.Context, input string) (string, error) {
	if tw.handler != nil {
		tw.handler.HandleToolStart(ctx, input)
	}

	output, err := tw.t.Call(ctx, input)
	if err != nil {
		if tw.handler != nil {
			tw.handler.HandleToolError(ctx, err)
		}
		return "", err
	}

	if tw.handler != nil {
		tw.handler.HandleToolEnd(ctx, output)
	}

	return output, nil
}

var _ tools.Tool = &toolWrapper{}

// TUIModel represents the bubbletea model for the TUI
type TUIModel struct {
	config        *Config
	agent         *agents.Executor
	width, height int

	// UI Components
	status       StatusComponent
	editor       EditorComponent
	messages     MessagesComponent
	fileViewer   *FileViewer
	completions  CompletionDialog
	toastManager ToastManager
	modal        *BaseModal

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
		if msg.String() == "q" {
			if m.fileViewer != nil && m.fileViewer.Active {
				m.fileViewer.Close()
				return m, nil
			}
			return m, tea.Quit
		}
		switch msg.String() {
		case "ctrl+c":
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
					if strings.HasPrefix(selected, "@") {
						// It's a file completion
						filePath := strings.TrimPrefix(selected, "@")
						content, err := os.ReadFile(filePath)
						if err != nil {
							m.messages.AddMessage(fmt.Sprintf("Error reading file: %v", err))
						} else {
							m.editor.SetValue(string(content))
						}
					} else {
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

						// Send the prompt to the agent
						cmds = append(cmds, func() tea.Msg {
							response, err := chains.Run(context.Background(), m.agent, content)
							if err != nil {
								return errMsg{err}
							}
							return responseMsg(response)
						})
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

	case responseMsg:
		m.messages.AddMessage(fmt.Sprintf("AI: %s", string(msg)))

	case toolStartMsg:
		m.messages.AddMessage(fmt.Sprintf("Tool Used: %s", msg.name))

	case toolEndMsg:
		m.messages.AddMessage(fmt.Sprintf("Tool Result: %s", msg.result))

	case errMsg:
		m.messages.AddMessage(fmt.Sprintf("Error: %v", msg.err))
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
func NewTUIModel(config *Config, handler *toolCallbackHandler) TUIModel {

	registry := NewCommandRegistry()

	model := TUIModel{
		config: config,
		width:  80, // Default width
		height: 24, // Default height

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
	model.status.SetWorkingDir(".")   // In a real implementation, get current working directory
	model.status.SetGitBranch("main") // In a real implementation, get current git branch

	agent, err := getAgent(config, handler)
	if err != nil {
		// This is a critical error, so we'll panic
		panic(err)
	}
	model.agent = agent

	return model
}

func main() {
	ctx := kong.Parse(&cli)

	if cli.Prompt != "" {
		// Non-interactive mode
		config, err := LoadConfig()
		if err != nil {
			fmt.Printf("Error loading configuration: %v\n", err)
			os.Exit(1)
		}

		response, err := sendPromptToAgent(config, cli.Prompt)
		if err != nil {
			fmt.Printf("Error from agent: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(response)
		os.Exit(0)
	}

	// Interactive mode
	err := ctx.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
