package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

type runCmd struct{}

type promptCmd struct {
	Prompt string `arg:"" help:"Prompt to send to the LLM"`
}

type versionCmd struct{}

var cli struct {
	Version versionCmd `cmd:"" help:"Print version information"`

	// Add a default command to run the Bubble Tea app
	Run runCmd `cmd:"" default:"1" help:"Run the interactive application"`
	
	// Add prompt command
	Prompt promptCmd `cmd:"p" help:"Send a prompt to the configured LLM"`
}

func (v versionCmd) Run() error {
	fmt.Println("Asimi CLI v0.1.0")
	return nil
}

type model struct {
	choices  []string
	cursor   int
	selected map[int]struct{}
	config   *Config
}

func initialModel() model {
	// Load configuration
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

	return model{
		choices: []string{"Buy carrots", "Buy celery", "Buy kohlrabi"},

		// A map which indicates which choices are selected. We're using
		// the  map like a mathematical set. The keys refer to the indexes
		// of the `choices` slice, above.
		selected: make(map[int]struct{}),
		config:   config,
	}
}

func (m model) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// Is it a key press?
	case tea.KeyMsg:

		// Cool, what key was pressed?
		switch msg.String() {

		// These keys should exit the program.
		case "ctrl+c", "q":
			return m, tea.Quit

		// The "up" and "k" keys move the cursor up
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		// The "down" and "j" keys move the cursor down
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}

		// The "enter" key and the spacebar (a literal space) toggle
		// the selected state for the item that the cursor is pointing at.
		case "enter", " ":
			_, ok := m.selected[m.cursor]
			if ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}
		}
	}

	// Return the updated model to the Bubble Tea runtime for processing.
	// Note that we're not returning a command.
	return m, nil
}

func (m model) View() string {
	// The header
	s := "What should we buy at the market?\n\n"

	// Display config info
	s += fmt.Sprintf("Server: %s:%d\n", m.config.Server.Host, m.config.Server.Port)
	s += fmt.Sprintf("Database: %s@%s:%d/%s\n", m.config.Database.User, m.config.Database.Host, m.config.Database.Port, m.config.Database.Name)
	s += fmt.Sprintf("LLM: %s (%s)\n\n", m.config.LLM.Provider, m.config.LLM.Model)

	// Iterate over our choices
	for i, choice := range m.choices {

		// Is the cursor pointing at this choice?
		cursor := " " // no cursor
		if m.cursor == i {
			cursor = ">" // cursor!
		}

		// Is this choice selected?
		checked := " " // not selected
		if _, ok := m.selected[i]; ok {
			checked = "x" // selected!
		}

		// Render the row
		s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, choice)
	}

	// The footer
	s += "\nPress q to quit.\n"

	// Send the UI for rendering
	return s
}

func (r runCmd) Run() error {
	// Check if we are running in a terminal
	// If not, print a message and exit
	// This is to prevent the program from hanging when run in non-interactive environments
	// like when running in a CI/CD pipeline or when the output is redirected
	// to a file or another program.
	// It's a good practice to check for this and provide a helpful message.
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsTerminal(os.Stdin.Fd()) {
		fmt.Println("This program requires a terminal to run.")
		fmt.Println("Please run it in a terminal emulator.")
		return nil
	}

	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("alas, there's been an error: %w", err)
	}
	return nil
}

func (p promptCmd) Run() error {
	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	
	// Send the prompt to the LLM and print the response
	response, err := sendPrompt(config, p.Prompt)
	if err != nil {
		return fmt.Errorf("failed to send prompt to LLM: %w", err)
	}
	
	fmt.Println(response)
	return nil
}

func main() {
	ctx := kong.Parse(&cli)

	// Kong will automatically run the default command when no subcommand is specified
	// so we don't need to manually check for it
	err := ctx.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
