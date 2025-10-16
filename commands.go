package main

import (
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
	registry.RegisterCommand("/resume", "Resume a previous session", handleResumeCommand)

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

type showHelpMsg struct{}
type showContextMsg struct{ content string }

func handleHelpCommand(model *TUIModel, args []string) tea.Cmd {
	return func() tea.Msg {
		return showHelpMsg{}
	}
}

func handleNewSessionCommand(model *TUIModel, args []string) tea.Cmd {
	// Start a new session
	model.sessionActive = true
	model.chat = NewChatComponent(model.chat.Width, model.chat.Height)

	// Clear raw session history for the new session
	model.rawSessionHistory = make([]string, 0)

	// Clear tool call tracking
	model.toolCallMessageIndex = make(map[string]int)

	// If we have an active session, reset its conversation history
	if model.session != nil {
		model.session.ClearHistory()
	}

	return nil
}

func handleQuitCommand(model *TUIModel, args []string) tea.Cmd {
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
