package main

import (
	"github.com/charmbracelet/lipgloss"
)

// RenderChatView renders the chat view when a session is active
func RenderChatView(messages []string, width, height int) string {
	// Create a header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("62")).
		Padding(0, 1)

	header := headerStyle.Render("Asimi CLI - Chat Session")

	// Create message views
	var messageViews []string
	for _, message := range messages {
		// Style messages based on sender
		var messageStyle lipgloss.Style
		if len(message) > 3 && message[:3] == "You" {
			messageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("230")).
				Padding(0, 1)
		} else {
			messageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Padding(0, 1)
		}

		messageViews = append(messageViews, messageStyle.Render(message))
	}

	// Join messages
	messagesView := lipgloss.JoinVertical(lipgloss.Left, messageViews...)

	// Create a container for messages with scrolling
	messagesContainer := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(width - 2).
		Height(height - 4). // Account for header and padding
		Padding(1, 1).
		Render(messagesView)

	// Combine everything
	content := lipgloss.JoinVertical(lipgloss.Left, header, messagesContainer)

	return content
}

// RenderHomeView renders the home view when no session is active
func RenderHomeView(width, height int) string {
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
		"▶ Press Ctrl+L to toggle layout",
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