package main

import "github.com/charmbracelet/lipgloss"

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
		Width(m.Width-2).
		Height(m.Height-4). // Account for title and borders
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
