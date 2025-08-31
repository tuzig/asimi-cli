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
	
	return registry
}

// RegisterCommand registers a new command
func (cr *CommandRegistry) RegisterCommand(name, description string, handler func(*TUIModel, []string) tea.Cmd) {
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
	for _, cmd := range cr.Commands {
		commands = append(commands, cmd)
	}
	return commands
}

// Command handlers

type showHelpMsg struct{}

func handleHelpCommand(model *TUIModel, args []string) tea.Cmd {
	return func() tea.Msg {
		return showHelpMsg{}
	}
}

func handleNewSessionCommand(model *TUIModel, args []string) tea.Cmd {
	// Start a new session
	model.sessionActive = false
	model.messages = NewMessagesComponent(model.messages.Width, model.messages.Height)
	model.messages.AddMessage("Started a new session. How can I help you today?")
	return nil
}

func handleQuitCommand(model *TUIModel, args []string) tea.Cmd {
	// Quit the application
	return tea.Quit
}