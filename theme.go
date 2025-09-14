package main

import "github.com/charmbracelet/lipgloss"

// Theme defines the colors and styles for the UI.
type Theme struct {
	PrimaryColor   lipgloss.Color
	SecondaryColor lipgloss.Color
	AccentColor    lipgloss.Color

	// Text rendering
	RenderAI   func(string) lipgloss.Style
	RenderUser func(string) lipgloss.Style
	RenderTool func(string) lipgloss.Style

	// Borders and highlights
	Border    lipgloss.Style
	Highlight lipgloss.Style
	// Add more as needed
}

// NewTheme creates and returns a new Theme with default styles.
func NewTheme() *Theme {
	// Define some default colors
	primary := lipgloss.Color("#7D56F4") // A nice purple
	secondary := lipgloss.Color("#6248FF") // A slightly different purple
	accent := lipgloss.Color("#FF5F87")    // A pink/red for accents

	return &Theme{
		PrimaryColor:   primary,
		SecondaryColor: secondary,
		AccentColor:    accent,

		RenderAI: func(text string) lipgloss.Style {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("86")).SetString(text)
		},
		RenderUser: func(text string) lipgloss.Style {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("205")).SetString(text)
		},
		RenderTool: func(text string) lipgloss.Style {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("39")).SetString(text)
		},

		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primary),

		Highlight: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("57")),
	}
}
