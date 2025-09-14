package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/fake"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

type runCmd struct{}

type versionCmd struct{}

var program *tea.Program

var cli struct {
	Version versionCmd `cmd:"version" help:"Print version information"`
	Prompt  string     `short:"p" help:"Prompt to send to the agent"`
	Run     runCmd     `cmd:"" default:"1" help:"Run the interactive application"`
}

func initLogger() {
	logFile, err := os.OpenFile(".asimi/asimi.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))
}

func (v versionCmd) Run() error {
	fmt.Println("Asimi CLI v0.1.0")
	return nil
}

func (r *runCmd) Run() error {
	// This command will only be run when no prompt is provided.
	// The logic in main() will handle the non-interactive case.

	// Check if we are running in a terminal
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsTerminal(os.Stdin.Fd()) {
		fmt.Println("This program requires a terminal to run.")
		fmt.Println("Please run it in a terminal emulator.")
		return nil
	}

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

	// Create the TUI model
	tuiModel := NewTUIModel(config)

	program = tea.NewProgram(tuiModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	llm, err := getLLMClient(config)
	if err != nil { // if it fails, just skip native session setup
		return fmt.Errorf("Failed to get the LLM client: %w", err)
	}
	sess, sessErr := NewSession(llm, config, func(m any) {
		program.Send(m)
	})
	if sessErr != nil {
		return fmt.Errorf("Failed to create a new session: %w", err)
	}
	tuiModel.SetSession(sess)

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("alas, there's been an error: %w", err)
	}
	return nil
}

type responseMsg string
type errMsg struct{ err error }

func main() {
	initLogger()
	ctx := kong.Parse(&cli)

	if cli.Prompt != "" {
		// Non-interactive mode via native Session path
		config, err := LoadConfig()
		if err != nil {
			fmt.Printf("Error loading configuration: %v\n", err)
			os.Exit(1)
		}

		llm, err := getLLMClient(config)
		if err != nil {
			fmt.Printf("Error creating LLM client: %v\n", err)
			os.Exit(1)
		}
		sess, err := NewSession(llm, config, consoleToolNotify)
		if err != nil {
			fmt.Printf("Error creating session: %v\n", err)
			os.Exit(1)
		}
		out, err := sess.Ask(context.Background(), cli.Prompt)
		if err != nil {
			fmt.Printf("Error asking session: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(out)
		os.Exit(0)
	}

	// Interactive mode
	err := ctx.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// consoleToolNotify prints tool lifecycle messages to stdout for non-interactive mode.
func consoleToolNotify(m any) {
	switch v := m.(type) {
	case ToolCallScheduledMsg:
		fmt.Printf("Tool Scheduled: %s with input: %s\n", v.Call.Tool.Name(), v.Call.Input)
		slog.Info("tool.scheduled", "tool", v.Call.Tool.Name(), "input", v.Call.Input)
	case ToolCallExecutingMsg:
		fmt.Printf("Tool Executing: %s with input: %s\n", v.Call.Tool.Name(), v.Call.Input)
		slog.Info("tool.executing", "tool", v.Call.Tool.Name(), "input", v.Call.Input)
	case ToolCallSuccessMsg:
		fmt.Printf("Tool Succeeded: %s\nInput: %s\nOutput: %s\n", v.Call.Tool.Name(), v.Call.Input, v.Call.Result)
		slog.Info("tool.success", "tool", v.Call.Tool.Name(), "input", v.Call.Input, "output", v.Call.Result)
	case ToolCallErrorMsg:
		fmt.Printf("Tool Errored: %s\nInput: %s\nError: %v\n", v.Call.Tool.Name(), v.Call.Input, v.Call.Error)
		slog.Error("tool.error", "tool", v.Call.Tool.Name(), "input", v.Call.Input, "error", v.Call.Error)
	}
}

// getLLMClient creates and returns an LLM client based on the configuration
func getLLMClient(config *Config) (llms.Model, error) {
	switch config.LLM.Provider {
	case "fake":
		llm := fake.NewFakeLLM([]string{})
		return llm, nil
	case "ollama":
		// For Ollama, we can use default options or customize based on config
		opts := []ollama.Option{
			ollama.WithModel(config.LLM.Model),
		}

		if config.LLM.BaseURL != "" {
			opts = append(opts, ollama.WithServerURL(config.LLM.BaseURL))
		}

		return ollama.New(opts...)
	case "openai":
		// For OpenAI, we need to set the API key
		opts := []openai.Option{
			openai.WithModel(config.LLM.Model),
		}

		if config.LLM.APIKey != "" {
			opts = append(opts, openai.WithToken(config.LLM.APIKey))
		}

		if config.LLM.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(config.LLM.BaseURL))
		}

		return openai.New(opts...)
	case "anthropic":
		// For Anthropic, we need to set the API key
		opts := []anthropic.Option{
			anthropic.WithModel(config.LLM.Model),
		}

		if config.LLM.APIKey != "" {
			opts = append(opts, anthropic.WithToken(config.LLM.APIKey))
		}

		if config.LLM.BaseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(config.LLM.BaseURL))
		}

		return anthropic.New(opts...)
	case "googleai":
		// For GoogleAI, we need to set the API key
		apiKey := config.LLM.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
			if apiKey == "" {
				return nil, fmt.Errorf("missing Google AI API key. Set it in the config file or via GEMINI_API_KEY environment variable")
			}
		}

		opts := []googleai.Option{
			googleai.WithDefaultModel(config.LLM.Model),
			googleai.WithAPIKey(apiKey),
		}

		// TODO: Add WithBaseURL to langchaingo's googleai implementation and send a PR
		// if config.LLM.BaseURL != "" {
		//     opts = append(opts, googleai.WithAPIEndpoint(config.LLM.BaseURL))
		// }

		return googleai.New(context.Background(), opts...)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.LLM.Provider)
	}
}
