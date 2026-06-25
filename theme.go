package main

import "github.com/charmbracelet/lipgloss"

// globalTheme is the application-wide theme instance
var globalTheme *Theme

// Theme defines the colors and styles for the UI.
type Theme struct {
	// Terminal7 color scheme
	PromptBorder     lipgloss.Color
	ChatBorder       lipgloss.Color
	TextColor        lipgloss.Color
	Warning          lipgloss.Color
	Error            lipgloss.Color
	PromptBackground lipgloss.Color
	StatusBackground lipgloss.Color
	TextError        lipgloss.Color
	PaneBackground   lipgloss.Color
	DarkBorder       lipgloss.Color

	// Prompt focus indicators
	PromptOnBorder  lipgloss.Color // Border color when focused on prompt (INSERT/COMMAND/LEARNING modes)
	PromptOffBorder lipgloss.Color // Border color when focused away from prompt (NORMAL/VISUAL modes)

	// Semantic colors
	SuccessColor    lipgloss.Color // Success / positive indicators (green)
	InfoColor       lipgloss.Color // Informational highlights (blue)
	ThinkingColor   lipgloss.Color // Thinking / reasoning content (muted green)
	DimTextColor    lipgloss.Color // Dimmed / secondary text
	TabFill         lipgloss.Color // Tab bar fill background
	CodeBackground  lipgloss.Color // Code block background
	ToastTextColor  lipgloss.Color // Text on toasts / colored backgrounds
	ModalBackground lipgloss.Color // Modal / completion menu background

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
// It also sets the global theme instance.
func NewTheme() *Theme {
	// Terminal7 color scheme
	promptBorder := lipgloss.Color("#F952F9")
	chatBorder := lipgloss.Color("#F4DB53")
	textColor := lipgloss.Color("#01FAFA")
	warning := lipgloss.Color("#F4DB53")
	errorColor := lipgloss.Color("#F54545")
	promptBackground := lipgloss.Color("#271D30")

	textError := lipgloss.Color("#004444")
	paneBackground := lipgloss.Color("#000000")
	darkBorder := lipgloss.Color("#373702")

	// Prompt focus indicators
	promptOnBorder := lipgloss.Color("#F952F9")  // Magent - focus on prompt (INSERT/other)
	promptOffBorder := lipgloss.Color("#373702") // Dark - focus away from prompt (NORMAL/VISUAL)

	theme := &Theme{
		// Terminal7 colors
		PromptBorder:     promptBorder,
		ChatBorder:       chatBorder,
		TextColor:        textColor,
		Warning:          warning,
		Error:            errorColor,
		PromptBackground: promptBackground,
		TextError:        textError,
		PaneBackground:   paneBackground,
		DarkBorder:       darkBorder,

		// Prompt focus indicators
		PromptOnBorder:  promptOnBorder,
		PromptOffBorder: promptOffBorder,

		// Semantic colors
		SuccessColor:    lipgloss.Color("#00FF00"),
		InfoColor:       lipgloss.Color("#00BFFF"),
		ThinkingColor:   lipgloss.Color("#6A9955"),
		DimTextColor:    lipgloss.Color("#666666"),
		TabFill:         lipgloss.Color("#333333"),
		CodeBackground:  lipgloss.Color("#1a1a1a"),
		ToastTextColor:  lipgloss.Color("230"),
		ModalBackground: lipgloss.Color("235"),

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

	// Set the global theme
	globalTheme = theme

	return theme
}
