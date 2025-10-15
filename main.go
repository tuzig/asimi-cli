package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	"gopkg.in/natefinch/lumberjack.v2"
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
	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Errorf("failed to get user home directory: %w", err))
	}

	logDir := filepath.Join(homeDir, ".local", "share", "asimi")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		panic(fmt.Errorf("failed to create log directory %s: %w", logDir, err))
	}

	// Set up lumberjack for log rotation
	logFile := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "asimi.log"),
		MaxSize:    10, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   true,
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

// formatToolCall formats a tool call according to the spec: two lines with ⏺ and ⎿ symbols
func formatToolCall(toolName, input, result string, err error) string {
	// Parse input JSON to extract key parameters for the first line
	var params map[string]interface{}
	json.Unmarshal([]byte(input), &params)

	for _, tool := range availableTools {
		if tool.Name() == toolName {
			return tool.Format(input, result, err)
		}
	}

	// Add a special err message type
	return fmt.Sprintf("Unknown tool: %s", toolName)

}

// consoleStreamingNotify handles streaming and tool messages for non-interactive mode
func consoleStreamingNotify(done chan struct{}, finalResponse *strings.Builder, mu *sync.Mutex) func(any) {
	// Track active tool calls to update their status
	activeToolCalls := make(map[string]*toolCallDisplay)

	return func(m any) {
		switch v := m.(type) {
		case ToolCallScheduledMsg:
			// Create initial display with hollow circle
			display := &toolCallDisplay{
				toolName: v.Call.Tool.Name(),
				input:    v.Call.Input,
				status:   "scheduled",
			}
			activeToolCalls[v.Call.ID] = display
			display.show()
			slog.Info("tool.scheduled", "tool", v.Call.Tool.Name(), "input", v.Call.Input)
		case ToolCallExecutingMsg:
			// Update to half-filled circle
			if display, exists := activeToolCalls[v.Call.ID]; exists {
				display.status = "executing"
				display.update()
			}
			slog.Info("tool.executing", "tool", v.Call.Tool.Name(), "input", v.Call.Input)
		case ToolCallSuccessMsg:
			// Update to full circle and show result
			if display, exists := activeToolCalls[v.Call.ID]; exists {
				display.status = "success"
				display.result = v.Call.Result
				display.complete()
				delete(activeToolCalls, v.Call.ID)
			}
			slog.Info("tool.success", "tool", v.Call.Tool.Name(), "input", v.Call.Input, "output", v.Call.Result)
		case ToolCallErrorMsg:
			// Update to X and show error
			if display, exists := activeToolCalls[v.Call.ID]; exists {
				display.status = "error"
				display.err = v.Call.Error
				display.complete()
				delete(activeToolCalls, v.Call.ID)
			}
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
		case streamMaxTokensReachedMsg:
			slog.Debug("console streaming max tokens reached", "content", v.content)
			fmt.Printf("\n\n[Response truncated due to length limit]\n")
			close(done)
		}
	}
}

// toolCallDisplay manages the display of a tool call with dynamic status updates
type toolCallDisplay struct {
	toolName string
	input    string
	result   string
	err      error
	status   string // "scheduled", "executing", "success", "error"
	linePos  int    // Track cursor position for updates
}

// show displays the initial tool call with hollow circle
func (d *toolCallDisplay) show() {
	formatted := d.formatWithStatus()
	lines := strings.Split(formatted, "\n")

	// Print both lines and remember position
	fmt.Print(lines[0])
	if len(lines) > 1 {
		fmt.Printf("\n%s", lines[1])
	}
	fmt.Print("\n")

	// Store position for updates (2 lines up from current position)
	d.linePos = 2
}

// update modifies the existing display in place
func (d *toolCallDisplay) update() {
	formatted := d.formatWithStatus()
	lines := strings.Split(formatted, "\n")

	// Move cursor up to overwrite previous lines
	fmt.Printf("\033[%dA", d.linePos) // Move up
	fmt.Print("\033[2K")              // Clear line
	fmt.Print(lines[0])               // Print first line

	if len(lines) > 1 {
		fmt.Print("\n\033[2K") // Move down and clear line
		fmt.Print(lines[1])    // Print second line
	}
	fmt.Print("\n")
}

// complete finalizes the display and moves cursor to next line
func (d *toolCallDisplay) complete() {
	formatted := d.formatWithStatus()
	lines := strings.Split(formatted, "\n")

	// Move cursor up to overwrite previous lines
	fmt.Printf("\033[%dA", d.linePos) // Move up
	fmt.Print("\033[2K")              // Clear line
	fmt.Print(lines[0])               // Print first line

	if len(lines) > 1 {
		fmt.Print("\n\033[2K") // Move down and clear line
		fmt.Print(lines[1])    // Print second line
	}
	fmt.Print("\n")
}

// formatWithStatus formats the tool call with appropriate status indicator
func (d *toolCallDisplay) formatWithStatus() string {
	// Get the base format from the tool
	var baseFormat string
	for _, tool := range availableTools {
		if tool.Name() == d.toolName {
			baseFormat = tool.Format(d.input, d.result, d.err)
			break
		}
	}

	if baseFormat == "" {
		baseFormat = fmt.Sprintf("⏺ Unknown tool: %s\n  ⎿  Error: tool not found", d.toolName)
	}

	// Replace the circle based on status
	var statusCircle string
	switch d.status {
	case "scheduled":
		statusCircle = "○" // Hollow circle
	case "executing":
		statusCircle = "◐" // Half-filled circle
	case "success":
		statusCircle = "●" // Full circle
	case "error":
		statusCircle = "✗" // X mark
	default:
		statusCircle = "○"
	}

	// Replace the first ○ with the status circle
	return strings.Replace(baseFormat, "○", statusCircle, 1)
}

// getLLMClient creates and returns an LLM client based on the configuration
func getLLMClient(config *Config) (llms.Model, error) {
	// First try to load tokens from keyring if not already in config
	if config.LLM.AuthToken == "" && config.LLM.APIKey == "" {
		// Try OAuth tokens first
		tokenData, err := GetTokenFromKeyring(config.LLM.Provider)
		if err == nil && tokenData != nil {
			if IsTokenExpired(tokenData) {
				// Token exists but expired - try to refresh it
				slog.Info("Token expired, attempting refresh", "provider", config.LLM.Provider)

				// Only attempt refresh for providers that support OAuth
				if config.LLM.Provider == "anthropic" {
					auth := &AuthAnthropic{}
					newAccessToken, refreshErr := auth.access()
					if refreshErr == nil {
						// Successfully refreshed - update config with new token
						slog.Info("Token refresh successful", "provider", config.LLM.Provider)
						config.LLM.AuthToken = newAccessToken

						// Get updated token data from keyring (auth.access() should have saved it)
						updatedTokenData, _ := GetTokenFromKeyring(config.LLM.Provider)
						if updatedTokenData != nil {
							config.LLM.RefreshToken = updatedTokenData.RefreshToken
						}
					} else {
						// Refresh failed - log error and fall back to API key
						slog.Warn("Token refresh failed, falling back to API key",
							"provider", config.LLM.Provider, "error", refreshErr)
						apiKey, err := GetAPIKeyFromKeyring(config.LLM.Provider)
						if err == nil && apiKey != "" {
							config.LLM.APIKey = apiKey
						}
					}
				} else {
					// For non-Anthropic providers, just fall back to API key when token expired
					apiKey, err := GetAPIKeyFromKeyring(config.LLM.Provider)
					if err == nil && apiKey != "" {
						config.LLM.APIKey = apiKey
					}
				}
			} else {
				// Token is still valid - use it
				config.LLM.AuthToken = tokenData.AccessToken
				config.LLM.RefreshToken = tokenData.RefreshToken
			}
		} else {
			// No token data found - try API key from keyring
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
			// Use the token we already have (either valid or freshly refreshed from above)
			accessToken := config.LLM.AuthToken

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
