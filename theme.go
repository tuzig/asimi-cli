package main

import "github.com/charmbracelet/lipgloss"

// Theme defines the colors and styles for the UI.
type Theme struct {
	// Terminal7 color scheme
	PromptBorder     lipgloss.Color
	ChatBorder       lipgloss.Color
	TextColor        lipgloss.Color
	Warning          lipgloss.Color
	Error            lipgloss.Color
	PromptBackground lipgloss.Color
	ChatBackground   lipgloss.Color
	TextError        lipgloss.Color
	PaneBackground   lipgloss.Color
	DarkBorder       lipgloss.Color

	// Legacy colors for compatibility
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
}

// NewTheme creates and returns a new Theme with Terminal7 colors.
func NewTheme() *Theme {
	// Terminal7 color scheme
	promptBorder := lipgloss.Color("#F952F9")
	chatBorder := lipgloss.Color("#F4DB53")
	textColor := lipgloss.Color("#01FAFA")
	warning := lipgloss.Color("#F4DB53")
	errorColor := lipgloss.Color("#F54545")
	promptBackground := lipgloss.Color("#271D30")
	chatBackground := lipgloss.Color("#11051E")
	textError := lipgloss.Color("#004444")
	paneBackground := lipgloss.Color("#000000")
	darkBorder := lipgloss.Color("#373702")

	return &Theme{
		// Terminal7 colors
		PromptBorder:     promptBorder,
		ChatBorder:       chatBorder,
		TextColor:        textColor,
		Warning:          warning,
		Error:            errorColor,
		PromptBackground: promptBackground,
		ChatBackground:   chatBackground,
		TextError:        textError,
		PaneBackground:   paneBackground,
		DarkBorder:       darkBorder,

		// Legacy colors for compatibility
		PrimaryColor:   promptBorder,
		SecondaryColor: chatBorder,
		AccentColor:    textColor,

		RenderAI: func(text string) lipgloss.Style {
			return lipgloss.NewStyle().Foreground(textColor).SetString(text)
		},
		RenderUser: func(text string) lipgloss.Style {
			return lipgloss.NewStyle().Foreground(promptBorder).SetString(text)
		},
		RenderTool: func(text string) lipgloss.Style {
			return lipgloss.NewStyle().Foreground(chatBorder).SetString(text)
		},

		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(chatBorder),

		Highlight: lipgloss.NewStyle().
			Foreground(textColor).
			Background(promptBackground),
	}
}
