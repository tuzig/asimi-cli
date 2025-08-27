package main

import (
	"github.com/tmc/langchaingo/llms"
)

// AppInfo contains static information about the application environment
type AppInfo struct {
	// Add fields for application information
}

// Agent represents an available agent
type Agent struct {
	ID   string
	Name string
	// Add other agent fields as needed
}

// Provider represents a model provider
type Provider struct {
	ID   string
	Name string
	// Add other provider fields as needed
}

// Permission represents a pending permission request
type Permission struct {
	ID   string
	Text string
	// Add other permission fields as needed
}

// Message represents a chat message
type Message struct {
	Role    string
	Content string
	// Add other message fields as needed
}

// Session represents a chat session
type Session struct {
	ID string
	// Add other session fields as needed
}

// State represents persistent TUI state
type State struct {
	// Add persistent state fields
}

// App represents the core application state
type App struct {
	Info        AppInfo
	Config      interface{} // Using interface{} for now, will replace with actual config type
	LLM         *llms.Model
	State       *State
	Session     *Session
	Messages    []Message
	Agents      []Agent
	Providers   []Provider
	AgentIndex  int
	Permissions []Permission
	Commands    CommandRegistry
}

// InterruptKeyState tracks the state for debouncing the interrupt command
type InterruptKeyState int

const (
	InterruptKeyStateNone InterruptKeyState = iota
	InterruptKeyStateFirstPress
	InterruptKeyStateSecondPress
)

// ExitKeyState tracks the state for debouncing the exit command
type ExitKeyState int

const (
	ExitKeyStateNone ExitKeyState = iota
	ExitKeyStateFirstPress
	ExitKeyStateSecondPress
)

// Modal represents a modal dialog
type Modal interface {
	Render() string
	Update(msg interface{}) (Modal, interface{}) // Returns new modal and command
}

// Model represents the UI state
type Model struct {
	App                   *App
	Width, Height         int
	ShowCompletionDialog  bool
	InterruptKeyState     InterruptKeyState
	ExitKeyState          ExitKeyState
	MessagesRight         bool
	Modal                 Modal
}