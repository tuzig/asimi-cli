package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

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

	// Set log level to DEBUG to see debug messages
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, opts)))
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
	if err != nil {
		// Log the error but continue without LLM support
		slog.Warn("Failed to get LLM client, running without AI capabilities", "error", err)
		fmt.Fprintf(os.Stderr, "Warning: Running without AI capabilities: %v\n", err)
	} else {
		sess, sessErr := NewSession(llm, config, func(m any) {
			program.Send(m)
		})
		if sessErr != nil {
			return fmt.Errorf("Failed to create a new session: %w", sessErr)
		}
		tuiModel.SetSession(sess)
	}

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
			fmt.Printf("Please configure authentication by running the program in interactive mode and using '/login'\n")
			os.Exit(1)
		}
		// Set up streaming for non-interactive mode
		done := make(chan struct{})
		var finalResponse strings.Builder
		var mu sync.Mutex

		sess, err := NewSession(llm, config, consoleStreamingNotify(done, &finalResponse, &mu))
		if err != nil {
			fmt.Printf("Error creating session: %v\n", err)
			os.Exit(1)
		}

		// Start streaming
		sess.AskStream(context.Background(), cli.Prompt)

		// Wait for streaming to complete
		<-done

		os.Exit(0)
	}

	// Interactive mode
	err := ctx.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// consoleStreamingNotify handles streaming and tool messages for non-interactive mode
func consoleStreamingNotify(done chan struct{}, finalResponse *strings.Builder, mu *sync.Mutex) func(any) {
	return func(m any) {
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
		case streamStartMsg:
			slog.Debug("console streaming started")
		case streamChunkMsg:
			chunk := string(v)
			slog.Debug("console streaming chunk", "chunk", chunk)
			fmt.Print(chunk)
			mu.Lock()
			finalResponse.WriteString(chunk)
			mu.Unlock()
		case streamCompleteMsg:
			fmt.Println() // Add newline after streaming
			slog.Debug("console streaming completed")
			close(done)
		case streamInterruptedMsg:
			slog.Debug("console streaming interrupted", "partial_content", v.partialContent)
			fmt.Printf("\n[Interrupted] %s\n", v.partialContent)
			mu.Lock()
			finalResponse.WriteString(v.partialContent)
			mu.Unlock()
			close(done)
		case streamErrorMsg:
			slog.Debug("console streaming error", "error", v.err)
			fmt.Printf("\nError: %v\n", v.err)
			close(done)
		}
	}
}

// getLLMClient creates and returns an LLM client based on the configuration
func getLLMClient(config *Config) (llms.Model, error) {
	// First try to load tokens from keyring if not already in config
	if config.LLM.AuthToken == "" && config.LLM.APIKey == "" {
		// Try OAuth tokens first
		tokenData, err := GetTokenFromKeyring(config.LLM.Provider)
		if err == nil && tokenData != nil && !IsTokenExpired(tokenData) {
			config.LLM.AuthToken = tokenData.AccessToken
			config.LLM.RefreshToken = tokenData.RefreshToken
		} else {
			// Try API key from keyring
			apiKey, err := GetAPIKeyFromKeyring(config.LLM.Provider)
			if err == nil && apiKey != "" {
				config.LLM.APIKey = apiKey
			}
		}
	}

	// Check if we have any authentication
	if config.LLM.AuthToken == "" && config.LLM.APIKey == "" && config.LLM.Provider != "fake" {
		return nil, fmt.Errorf("no authentication configured for %s provider. Use '/login' in interactive mode to authenticate", config.LLM.Provider)
	}
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
		// For Anthropic, we can use either OAuth tokens or API key
		opts := []anthropic.Option{
			anthropic.WithModel(config.LLM.Model),
		}

		// Prefer OAuth access token over API key
		if config.LLM.AuthToken != "" {
			// For OAuth, we need to refresh the token if expired
			auth := &AuthAnthropic{}
			accessToken, err := auth.access()
			if err != nil {
				// If refresh fails, try using the stored token directly
				accessToken = config.LLM.AuthToken
			}

			// Pass placeholder to SDK to bypass API key validation
			// The real authentication happens in the HTTP transport
			// We can't use empty string as the SDK validates for non-empty token
			opts = append(opts, anthropic.WithToken("oauth-placeholder"))

			// Create custom HTTP client with OAuth transport
			httpClient := &http.Client{
				Transport: &anthropicOAuthTransport{
					token: accessToken,
					base:  http.DefaultTransport,
				},
			}
			opts = append(opts, anthropic.WithHTTPClient(httpClient))
		} else if config.LLM.APIKey != "" {
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

		return googleai.New(context.Background(), opts...)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.LLM.Provider)
	}
}

// anthropicOAuthTransport adds OAuth headers for Anthropic API
type anthropicOAuthTransport struct {
	token string
	base  http.RoundTripper
}

func (t *anthropicOAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone request to avoid mutating caller's request
	r := req.Clone(req.Context())

	// Add OAuth Bearer token (overwrite any existing authorization)
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	}

	// Add required beta headers exactly as specified
	// Order matters: oauth-2025-04-20 must come first for OAuth mode
	r.Header.Set("anthropic-beta",
		"oauth-2025-04-20,claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14")

	// Remove x-api-key header - critical for OAuth to work
	r.Header.Del("x-api-key")
	r.Header.Del("X-Api-Key") // Remove all case variations
	
	// Override URL based on ANTHROPIC_BASE_URL environment variable
	if baseURL := os.Getenv("ANTHROPIC_BASE_URL"); baseURL != "" {
		if parsedURL, err := url.Parse(baseURL + "/v1/messages"); err == nil {
			r.URL = parsedURL
		}
	}

	if t.base == nil {
		t.base = http.DefaultTransport
	}
	return t.base.RoundTrip(r)
}

// anthropicAPIKeyTransport adds beta headers for API key authentication
type anthropicAPIKeyTransport struct {
	base http.RoundTripper
}

func (t *anthropicAPIKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone request to avoid mutating caller's request
	r := req.Clone(req.Context())

	// Add beta headers for API key mode (no oauth header)
	r.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14")

	if t.base == nil {
		t.base = http.DefaultTransport
	}
	return t.base.RoundTrip(r)
}
