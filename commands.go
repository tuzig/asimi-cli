package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Command represents a slash command
type Command struct {
	Name        string
	Description string
	Handler     func(*TUIModel, []string) tea.Cmd
}

// CommandRegistry holds all available commands
type CommandRegistry struct {
	Commands map[string]Command
	order    []string
}

// NewCommandRegistry creates a new command registry
func NewCommandRegistry() CommandRegistry {
	registry := CommandRegistry{
		Commands: make(map[string]Command),
	}

	// Register built-in commands
	registry.RegisterCommand("/help", "Show help information", handleHelpCommand)
	registry.RegisterCommand("/new", "Start a new session", handleNewSessionCommand)
	registry.RegisterCommand("/quit", "Quit the application", handleQuitCommand)
	registry.RegisterCommand("/login", "Login with OAuth provider selection", handleLoginCommand)
	registry.RegisterCommand("/models", "Select AI model", handleModelsCommand)
	registry.RegisterCommand("/context", "Show context usage details", handleContextCommand)
	registry.RegisterCommand("/vi", "Toggle vi mode (use : for commands)", handleViCommand)
	registry.RegisterCommand("/clear-history", "Clear all prompt history", handleClearHistoryCommand)
	registry.RegisterCommand("/resume", "Resume a previous session", handleResumeCommand)
	registry.RegisterCommand("/export", "Export conversation to file and open in $EDITOR (usage: /export [full|conversation])", handleExportCommand)

	return registry
}

// RegisterCommand registers a new command
func (cr *CommandRegistry) RegisterCommand(name, description string, handler func(*TUIModel, []string) tea.Cmd) {
	if _, exists := cr.Commands[name]; !exists {
		cr.order = append(cr.order, name)
	}
	cr.Commands[name] = Command{
		Name:        name,
		Description: description,
		Handler:     handler,
	}
}

// GetCommand gets a command by name
func (cr CommandRegistry) GetCommand(name string) (Command, bool) {
	cmd, exists := cr.Commands[name]
	return cmd, exists
}

// GetAllCommands returns all registered commands
func (cr CommandRegistry) GetAllCommands() []Command {
	var commands []Command
	for _, name := range cr.order {
		if cmd, ok := cr.Commands[name]; ok {
			commands = append(commands, cmd)
		}
	}
	return commands
}

// Command handlers

type showHelpMsg struct {
	leader string
}
type showContextMsg struct{ content string }

func handleHelpCommand(model *TUIModel, args []string) tea.Cmd {
	return func() tea.Msg {
		leader := "/"
		if model != nil && model.prompt.ViMode {
			leader = ":"
		}
		return showHelpMsg{leader: leader}
	}
}

func handleNewSessionCommand(model *TUIModel, args []string) tea.Cmd {
	model.saveSession()
	model.sessionActive = true
	model.chat = NewChatComponent(model.chat.Width, model.chat.Height)

	model.rawSessionHistory = make([]string, 0)

	model.toolCallMessageIndex = make(map[string]int)

	// Reset prompt history and waiting state
	model.initHistory()
	model.cancelStreaming()
	model.stopStreaming()

	// If we have an active session, reset its conversation history
	if model.session != nil {
		model.session.ClearHistory()
	}
	return nil
}

func handleQuitCommand(model *TUIModel, args []string) tea.Cmd {
	// Save the session before quitting
	model.saveSession()
	// Quit the application
	return tea.Quit
}

func handleContextCommand(model *TUIModel, args []string) tea.Cmd {
	return func() tea.Msg {
		if model.session == nil {
			return showContextMsg{content: "No active session. Use /login to configure a provider and start chatting."}
		}
		info := model.session.GetContextInfo()
		return showContextMsg{content: renderContextInfo(info)}
	}
}

func handleViCommand(model *TUIModel, args []string) tea.Cmd {
	// Toggle vi mode
	model.prompt.SetViMode(!model.prompt.ViMode)

	var message string
	if model.prompt.ViMode {
		message = "Vi mode enabled. Press 'i' to insert, 'Esc' to return to normal mode. Use : for commands."
	} else {
		message = "Vi mode disabled. Use / for commands."
	}

	model.toastManager.AddToast(message, "info", 4000)
	return nil
}

func handleClearHistoryCommand(model *TUIModel, args []string) tea.Cmd {
	// Clear persistent history
	if model.historyStore != nil {
		if err := model.historyStore.Clear(); err != nil {
			model.toastManager.AddToast("Failed to clear history", "error", 3000)
			return nil
		}
	}

	// Clear in-memory history
	model.promptHistory = make([]promptHistoryEntry, 0)
	model.historyCursor = 0
	model.historySaved = false
	model.historyPendingPrompt = ""

	model.toastManager.AddToast("Prompt history cleared", "success", 3000)
	return nil
}

func handleResumeCommand(model *TUIModel, args []string) tea.Cmd {
	return func() tea.Msg {
		config, err := LoadConfig()
		if err != nil {
			return sessionResumeErrorMsg{err: err}
		}

		if !config.Session.Enabled {
			return showContextMsg{content: "Session resume is disabled in configuration."}
		}

		maxSessions := 50
		maxAgeDays := 30
		listLimit := 10

		if config.Session.MaxSessions > 0 {
			maxSessions = config.Session.MaxSessions
		}
		if config.Session.MaxAgeDays > 0 {
			maxAgeDays = config.Session.MaxAgeDays
		}
		if config.Session.ListLimit > 0 {
			listLimit = config.Session.ListLimit
		}

		store, err := NewSessionStore(maxSessions, maxAgeDays)
		if err != nil {
			return sessionResumeErrorMsg{err: err}
		}

		sessions, err := store.ListSessions(listLimit)
		if err != nil {
			return sessionResumeErrorMsg{err: err}
		}

		return sessionsLoadedMsg{sessions: sessions}
	}
}

func handleExportCommand(model *TUIModel, args []string) tea.Cmd {
	if model.session == nil {
		return func() tea.Msg {
			return showContextMsg{content: "No active session to export. Start a conversation first."}
		}
	}

	// Determine export type from args, default to conversation
	exportType := ExportTypeConversation
	if len(args) > 0 {
		switch args[0] {
		case "full":
			exportType = ExportTypeFull
		case "conversation":
			exportType = ExportTypeConversation
		default:
			model.toastManager.AddToast(fmt.Sprintf("Unknown export type '%s'. Use 'full' or 'conversation'", args[0]), "error", 3000)
			return nil
		}
	}

	// Export the session to a file
	filepath, err := exportSession(model.session, exportType)
	if err != nil {
		return func() tea.Msg {
			return showContextMsg{content: fmt.Sprintf("Export failed: %v", err)}
		}
	}

	// Open the file in the editor using ExecProcess
	cmd := openInEditor(filepath)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return showContextMsg{content: fmt.Sprintf("Editor exited with error: %v", err)}
		}
		model.toastManager.AddToast(fmt.Sprintf("Conversation exported successfully (%s).", exportType), "success", 3000)
		return nil
	})
}
