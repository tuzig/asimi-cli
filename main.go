package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
	isatty "github.com/mattn/go-isatty"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/fake"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"go.uber.org/fx"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// Update the version as part of the version release process
var version = "0.3.0"

var program *tea.Program

var cli struct {
	Version       bool   `help:"Print version information"`
	Prompt        string `short:"p" help:"Prompt to send to the agent"`
	Debug         bool   `help:"Enable debug logging"`
	NoCleanup     bool   `help:"Don't remove container on exit (for debugging)"`
	CPUProfile    string `help:"Write CPU profile to file"`
	MemProfile    string `help:"Write memory profile to file"`
	Trace         string `help:"Write execution trace to file"`
	ProfileExitMs int    `help:"Exit after N milliseconds (for profiling startup)"`
}

func initLogger() {
	var logDir string
	var logPath string

	// Determine log directory and path
	if cli.Debug {
		// In debug mode, log to current directory
		logDir = "."
		logPath = filepath.Join(logDir, "asimi.log")
		logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			panic(fmt.Errorf("failed to open log file %s: %w", logPath, err))
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug})))
	} else {
		// In production mode, log to user's data directory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			panic(fmt.Errorf("failed to get user home directory: %w", err))
		}
		logDir = filepath.Join(homeDir, ".local", "share", "asimi")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			panic(fmt.Errorf("failed to create log directory %s: %w", logDir, err))
		}
		logPath = filepath.Join(logDir, "asimi.log")
		logFile := &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    10, // megabytes
			MaxBackups: 3,
			MaxAge:     28, // days
			Compress:   true,
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}
}

func runInteractiveMode() error {
	startTime := time.Now()

	// Check if we are running in a terminal (skip check if profiling with auto-exit)
	if cli.ProfileExitMs == 0 && !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsTerminal(os.Stdin.Fd()) {
		fmt.Println("This program requires a terminal to run.")
		fmt.Println("Please run it in a terminal emulator.")
		return nil
	}

	if cli.Debug {
		slog.Debug("[TIMING] Terminal check completed", "duration", time.Since(startTime))
	}

	fmt.Printf("Asimi %s loading...\n", version)

	// Variables to hold populated dependencies
	var tuiProgram *tea.Program

	// Conditionally enable fx logging based on debug flag
	var fxOptions []fx.Option
	if !cli.Debug {
		fxOptions = append(fxOptions, fx.NopLogger)
	}

	// Add core providers
	fxOptions = append(fxOptions,
		fx.Provide(
			ProvideLogger,
			ProvideConfig,
			ProvideStorage,
			ProvideRepoInfo,
			ProvideShellRunner,
			ProvidePromptHistory,
			ProvideCommandHistory,
			ProvideSessionHistory,
			ProvideTUIModel,
			StartTUI,
		),
		fx.Invoke(
			ProvideModelClient,
		),
		fx.Populate(&currentShellRunner, &tuiProgram),
	)

	// Create fx app with all providers
	app := fx.New(fxOptions...)

	// Start the fx app (runs OnStart hooks for async initialization)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("failed to start fx app: %w", err)
	}
	defer app.Stop(ctx)

	slog.Debug("[TIMING] fx app initialized", "duration", time.Since(startTime))

	// Check for updates in background (non-blocking)
	go func() {
		if AutoCheckForUpdates(version) {
			program.Send(updateAvailableMsg{})
		}
	}()

	// If profile-exit-ms is set, schedule an exit after that duration
	if cli.ProfileExitMs > 0 {
		go func() {
			time.Sleep(time.Duration(cli.ProfileExitMs) * time.Millisecond)
			if cli.Debug {
				slog.Debug("[TIMING] Auto-exiting after N ms for profiling", "ms", cli.ProfileExitMs)
			}
			program.Send(tea.Quit())
		}()
	}

	_, runErr := tuiProgram.Run()

	if runErr != nil {
		return fmt.Errorf("alas, there's been an error: %w", runErr)
	}

	slog.Debug("[TIMING] Total Run() time", "duration", time.Since(startTime))
	return nil
}

type responseMsg string
type errMsg struct{ err error }

// llmInitSuccessMsg is sent when LLM initialization completes successfully
type llmInitSuccessMsg struct {
	session *Session
}

// llmInitErrorMsg is sent when LLM initialization fails
type llmInitErrorMsg struct {
	err error
}

// compactCompleteMsg is sent when conversation compaction completes successfully
type compactCompleteMsg struct {
	summary string
}

// compactErrorMsg is sent when conversation compaction fails
type compactErrorMsg struct {
	err error
}

// updateAvailableMsg is sent when a newer version is available
type updateAvailableMsg struct{}

func main() {
	startTime := time.Now()
	kong.Parse(&cli)

	// Handle --version flag
	if cli.Version {
		fmt.Printf("Asimi CLI v%s\n", version)
		os.Exit(0)
	}

	// Start profiling if requested
	if cli.CPUProfile != "" {
		f, err := os.Create(cli.CPUProfile)
		if err != nil {
			slog.Error("Could not create CPU profile", "error", err)
			os.Exit(1)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			slog.Error("Could not start CPU profile", "error", err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
		slog.Error("CPU profiling enabled", "file", cli.CPUProfile)
	}

	if cli.Trace != "" {
		f, err := os.Create(cli.Trace)
		if err != nil {
			slog.Error("Could not create trace file", "error", err)
			os.Exit(1)
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			slog.Error("Could not start trace", "error", err)
			os.Exit(1)
		}
		defer trace.Stop()
		slog.Info("Execution tracing enabled", "writing to", cli.Trace)
	}

	// Log startup timing
	if cli.Debug {
		slog.Debug("[TIMING] main() started", "time", startTime)
	}

	// Determine if we should run in non-interactive mode
	// Non-interactive mode is triggered by:
	// 1. Explicit -p flag: asimi -p "prompt here"
	// 2. Non-interactive stdin (pipe/redirect): echo "prompt" | asimi
	isStdinTerminal := isatty.IsTerminal(os.Stdin.Fd())
	hasPromptArg := cli.Prompt != ""

	// If no -p flag but stdin is not a terminal, read from stdin
	if !hasPromptArg && !isStdinTerminal {
		// Read prompt from stdin
		var builder strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				builder.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		cli.Prompt = strings.TrimSpace(builder.String())
		hasPromptArg = cli.Prompt != ""
	}

	// For non-interactive mode, initialize the old logger
	// For interactive mode, the fx-provided logger will be used
	if hasPromptArg {
		initLogger()
		if cli.Debug {
			slog.Debug("[TIMING] initLogger() completed", "duration", time.Since(startTime))
		}
	}

	if hasPromptArg {
		// Non-interactive mode via native Session path
		config, err := LoadConfig()
		if err != nil {
			fmt.Printf("Error loading configuration: %v\n", err)
			os.Exit(1)
		}

		// Initialize shell runner with config
		initShellRunner(config)

		llm, err := getModelClient(config)
		if err != nil {
			fmt.Printf("Error creating LLM client: %v\n", err)
			fmt.Printf("Please authenticate by running the program in interactive mode and ':models'\n")
			os.Exit(1)
		}
		// Set up streaming for non-interactive mode
		done := make(chan struct{})
		var finalResponse strings.Builder
		var mu sync.Mutex

		repoInfo := GetRepoInfo()
		sess, err := NewSession(llm, config, repoInfo, consoleStreamingNotify(done, &finalResponse, &mu))
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
	if err := runInteractiveMode(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Write memory profile if requested
	if cli.MemProfile != "" {
		f, err := os.Create(cli.MemProfile)
		if err != nil {
			slog.Error("Could not create memory profile", "error", err)
			os.Exit(1)
		}
		defer f.Close()
		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			slog.Error("Could not write memory profile", "error", err)
			os.Exit(1)
		}
		slog.Info("Memory profile written", "file", cli.MemProfile)
	}

	slog.Debug("[TIMING] Total execution time", "duration", time.Since(startTime))
}

// formatToolCall formats a tool call according to the spec: two lines with ⏺ and ⎿ symbols
func formatToolCall(toolName, icon string, input, result string, err error) string {
	// Parse input JSON to extract key parameters for the first line
	var params map[string]interface{}
	json.Unmarshal([]byte(input), &params)

	f := toolName
	for i := range availableTools {
		tool := availableTools[i]
		//nolint:typecheck // Tool interface is correctly defined in tools.go
		if tool.Name() == toolName {
			f = tool.Format(input, result, err)
		}
	}
	// Add a special err message type
	return fmt.Sprintf("%s %s", icon, f)

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
			slog.Debug("tool.scheduled", "tool", v.Call.Tool.Name(), "input", v.Call.Input)
		case ToolCallExecutingMsg:
			// Update to half-filled circle
			if display, exists := activeToolCalls[v.Call.ID]; exists {
				display.status = "executing"
				display.update()
			}
			slog.Debug("tool.executing", "tool", v.Call.Tool.Name(), "input", v.Call.Input)
		case ToolCallSuccessMsg:
			// Update to full circle and show result
			if display, exists := activeToolCalls[v.Call.ID]; exists {
				display.status = "success"
				display.result = v.Call.Result
				display.complete()
				delete(activeToolCalls, v.Call.ID)
			}
			slog.Debug("tool.success", "tool", v.Call.Tool.Name(), "input", v.Call.Input, "output", v.Call.Result)
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

// formatWithStatus formats the tool call for the UI
func (d *toolCallDisplay) formatWithStatus() string {
	// Get the base format from the tool
	var baseFormat string
	for i := range availableTools {
		tool := availableTools[i]
		//nolint:typecheck // Tool interface is correctly defined in tools.go
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

// getModelClient creates and returns an LLM client based on the configuration
func getModelClient(config *Config) (llms.Model, error) {
	// First try to load tokens from keyring if not already in config
	if config.LLM.AuthToken == "" && config.LLM.APIKey == "" {
		// Try OAuth tokens first
		token, err := GetOauthToken(config.LLM.Provider)
		if err == nil && token != nil {
			if !IsTokenExpired(token) {
				// Token is still valid - use it
				config.LLM.AuthToken = token.AccessToken
				config.LLM.RefreshToken = token.RefreshToken
			} else {
				// Token exists but expired - try to refresh it
				slog.Info("Token expired, attempting refresh", "provider", config.LLM.Provider)

				// Try to refresh the token
				if !refreshOAuthToken(config) {
					// Refresh failed - fall back to API key
					slog.Warn("Token refresh failed, falling back to API key", "provider", config.LLM.Provider)
					apiKey, err := GetAPIKeyFromKeyring(config.LLM.Provider)
					if err == nil && apiKey != "" {
						config.LLM.APIKey = apiKey
					}
				} else {
					slog.Info("Token refresh successful", "provider", config.LLM.Provider)
				}
			}
		} else {
			// No token data found - try API key from keyring
			apiKey, err := GetAPIKeyFromKeyring(config.LLM.Provider)
			if err == nil && apiKey != "" {
				config.LLM.APIKey = apiKey
			}
		}
	}

	switch config.LLM.Provider {
	case "fake":
		llm := fake.NewFakeLLM([]string{})
		return llm, nil
	case "ollama":
		if err := ensureOllamaConfigured(config.LLM.BaseURL); err != nil {
			return nil, err
		}
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
					token:  accessToken,
					config: config,
					base:   http.DefaultTransport,
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

func ensureOllamaConfigured(rawBaseURL string) error {
	baseURL := rawBaseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	} else if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid ollama base URL %q: %w", rawBaseURL, err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid ollama base URL %q: host is empty", rawBaseURL)
	}

	host := strings.ToLower(parsed.Hostname())
	isLocalHost := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if isLocalHost {
		if _, err := exec.LookPath("ollama"); err != nil {
			installHint := "Install Ollama from https://ollama.com/download."
			if runtime.GOOS == "darwin" {
				installHint = "Install Ollama on macOS via https://ollama.com/download or Homebrew (`brew install ollama`)."
			}
			return fmt.Errorf("ollama CLI not found in PATH: %w. %s", err, installHint)
		}
	}

	versionURL := parsed.ResolveReference(&url.URL{Path: "/api/version"})

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(versionURL.String())
	if err != nil {
		startHint := fmt.Sprintf("Ensure the Ollama service is reachable at %s.", parsed.Host)
		if isLocalHost {
			startHint = "Ensure the Ollama service is running (start it with `ollama serve`)."
			if runtime.GOOS == "darwin" {
				startHint = "Launch the Ollama app or run `ollama serve` to start the background service."
			}
		}
		return fmt.Errorf("unable to reach ollama at %s: %w. %s", versionURL.String(), err, startHint)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("ollama at %s returned status %d", versionURL.String(), resp.StatusCode)
	}

	return nil
}

// anthropicOAuthTransport adds OAuth headers for Anthropic API
type anthropicOAuthTransport struct {
	token  string
	config *Config
	base   http.RoundTripper
}

func (t *anthropicOAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check if token needs refresh before making the request
	if t.config != nil && refreshOAuthToken(t.config) {
		// Token was refreshed, update transport token
		t.token = t.config.LLM.AuthToken
	}

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
