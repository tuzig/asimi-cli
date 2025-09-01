package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/schema"
	"github.com/tmc/langchaingo/tools"
)

type runCmd struct{}

type versionCmd struct{}

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
	handler := &toolCallbackHandler{}
	tuiModel := NewTUIModel(config, handler)

	p := tea.NewProgram(tuiModel)
	handler.p = p
	agent, err := NewAgent(config, WithCallbacks(handler), WithTea(p))
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	tuiModel.agent = agent.executor
	tuiModel.scheduler = agent.scheduler
	handler.scheduler = agent.scheduler

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("alas, there's been an error: %w", err)
	}
	return nil
}

type responseMsg string
type errMsg struct{ err error }

type toolCallbackHandler struct {
	p         *tea.Program
	scheduler *CoreToolScheduler
}

func (h *toolCallbackHandler) HandleToolStart(ctx context.Context, input string) {
	// Now handled by the CoreToolScheduler
}
func (h *toolCallbackHandler) HandleToolEnd(ctx context.Context, output string) {
	// Now handled by the CoreToolScheduler
}
func (h *toolCallbackHandler) HandleLLMStart(ctx context.Context, prompts []string)  {}
func (h *toolCallbackHandler) HandleLLMError(ctx context.Context, err error)         {}
func (h *toolCallbackHandler) HandleChainError(ctx context.Context, err error)       {}
func (h *toolCallbackHandler) HandleStreamingFunc(ctx context.Context, chunk []byte) {}
func (h *toolCallbackHandler) HandleLLMGenerateContentEnd(ctx context.Context, res *llms.ContentResponse) {
}
func (h *toolCallbackHandler) HandleLLMGenerateContentStart(ctx context.Context, ms []llms.MessageContent) {
}
func (h *toolCallbackHandler) HandleText(ctx context.Context, text string)            {}
func (h *toolCallbackHandler) HandleToolError(ctx context.Context, err error)         {}
func (h *toolCallbackHandler) HandleRetrieverStart(ctx context.Context, query string) {}
func (h *toolCallbackHandler) HandleRetrieverEnd(ctx context.Context, query string, documents []schema.Document) {
}
func (h *toolCallbackHandler) HandleChainEnd(ctx context.Context, outputs map[string]any)       {}
func (h *toolCallbackHandler) HandleChainStart(ctx context.Context, inputs map[string]any)      {}
func (h *toolCallbackHandler) HandleAgentAction(ctx context.Context, action schema.AgentAction) {}
func (h *toolCallbackHandler) HandleAgentFinish(ctx context.Context, finish schema.AgentFinish) {}

type toolWrapper struct {
	t       tools.Tool
	handler callbacks.Handler
}

func (tw *toolWrapper) Name() string {
	return tw.t.Name()
}

func (tw *toolWrapper) Description() string {
	return tw.t.Description()
}

func (tw *toolWrapper) Call(ctx context.Context, input string) (string, error) {
	slog.Info("Tool call", "tool", tw.t.Name(), "input", input)

	if h, ok := tw.handler.(*toolCallbackHandler); ok && h.scheduler != nil {
		resultChan := h.scheduler.Schedule(tw.t, input)
		result := <-resultChan
		return result.Output, result.Error
	}

	// Fallback for non-TUI mode
	if tw.handler != nil {
		tw.handler.HandleToolStart(ctx, input)
	}

	output, err := tw.t.Call(ctx, input)
	if err != nil {
		slog.Error("Tool retured an error", "error", err)
		if tw.handler != nil {
			tw.handler.HandleToolError(ctx, err)
		}
		return "", err
	}

	if tw.handler != nil {
		tw.handler.HandleToolEnd(ctx, output)
	}

	return output, nil
}

var _ tools.Tool = &toolWrapper{}

func main() {
	initLogger()
	ctx := kong.Parse(&cli)

	if cli.Prompt != "" {
		// Non-interactive mode
		config, err := LoadConfig()
		if err != nil {
			fmt.Printf("Error loading configuration: %v\n", err)
			os.Exit(1)
		}

		response, err := sendPromptToAgent(config, cli.Prompt)
		if err != nil {
			fmt.Printf("Error from agent: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(response)
		os.Exit(0)
	}

	// Interactive mode
	err := ctx.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
