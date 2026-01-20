package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	"github.com/afittestide/asimi/internal/llm"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
	isatty "github.com/mattn/go-isatty"
	"github.com/tmc/langchaingo/llms"
	"go.uber.org/fx"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// Update the version as part of the version release process
var version = "0.4.2"

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
			ProvideScheduler,
			ProvideShellRunner,
			ProvideLLMConfig,
			shogunate.ProvideShogunate,
			ProvidePromptHistory,
			ProvideCommandHistory,
			ProvideSessionHistory,
			ProvideTUIModel,
			StartTUI,
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

		// Initialize shell runner with config (no scheduler registry for non-interactive mode)
		initShellRunner(config, nil)

		// Initialize storage for Shogunate
		db, err := storage.InitDB(config.Storage.DatabasePath)
		if err != nil {
			fmt.Printf("Error initializing storage: %v\n", err)
			os.Exit(1)
		}
		defer db.Close()

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

		// Create and start Shogunate for non-interactive mode
		sgConfig := shogunate.DefaultShogunateConfig()
		sg := shogunate.NewShogunate(db.Conn(), sgConfig, slog.Default())
		if err := sg.Start(context.Background()); err != nil {
			fmt.Printf("Error starting shogunate: %v\n", err)
			os.Exit(1)
		}
		sg.SetLLMClient(llm)
		defer sg.Stop()

		// Build tools for the session
		tools := getAvailableTools(config)

		// Create session config
		sessionCfg := &shogunate.SessionConfig{
			LLM:        config.LLM,
			AgentsFile: config.Session.AgentsFile,
		}

		// Create session with Chancellor's brewing prompt
		sess, err := shogunate.NewSession(llm, sessionCfg, repoInfo.RepoInfo, tools, nil, consoleStreamingNotify(done, &finalResponse, &mu), sg.Chancellor.Role())
		if err != nil {
			fmt.Printf("Error creating session: %v\n", err)
			os.Exit(1)
		}

		// Connect the Shogunate Forge to the Session for envelope-based tool execution
		sess.SetForge(sg.Forge)

		// Start streaming (blocking call that uses notify callback)
		_, err = sess.AskWithStreaming(context.Background(), cli.Prompt, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		}
		close(done)

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
		case shogunate.ToolCallScheduledMsg:
			// Create initial display with hollow circle
			display := &toolCallDisplay{
				toolName: v.Call.Tool.Name(),
				input:    v.Call.Input,
				status:   "scheduled",
			}
			activeToolCalls[v.Call.ID] = display
			display.show()
			slog.Debug("tool.scheduled", "tool", v.Call.Tool.Name(), "input", v.Call.Input)
		case shogunate.ToolCallExecutingMsg:
			// Update to half-filled circle
			if display, exists := activeToolCalls[v.Call.ID]; exists {
				display.status = "executing"
				display.update()
			}
			slog.Debug("tool.executing", "tool", v.Call.Tool.Name(), "input", v.Call.Input)
		case shogunate.ToolCallSuccessMsg:
			// Update to full circle and show result
			if display, exists := activeToolCalls[v.Call.ID]; exists {
				display.status = "success"
				display.result = v.Call.Result
				display.complete()
				delete(activeToolCalls, v.Call.ID)
			}
			slog.Debug("tool.success", "tool", v.Call.Tool.Name(), "input", v.Call.Input, "output", v.Call.Result)
		case shogunate.ToolCallErrorMsg:
			// Update to X and show error
			if display, exists := activeToolCalls[v.Call.ID]; exists {
				display.status = "error"
				display.err = v.Call.Error
				display.complete()
				delete(activeToolCalls, v.Call.ID)
			}
			slog.Error("tool.error", "tool", v.Call.Tool.Name(), "input", v.Call.Input, "error", v.Call.Error)
		case shogunate.StreamStartMsg:
			slog.Debug("console streaming started")
		case shogunate.StreamChunkMsg:
			chunk := string(v)
			slog.Debug("console streaming chunk", "chunk", chunk)
			fmt.Print(chunk)
			mu.Lock()
			finalResponse.WriteString(chunk)
			mu.Unlock()
		case shogunate.StreamCompleteMsg:
			fmt.Println() // Add newline after streaming
			slog.Debug("console streaming completed")
		case shogunate.StreamInterruptedMsg:
			slog.Debug("console streaming interrupted", "partial_content", v.PartialContent)
			fmt.Printf("\n[Interrupted] %s\n", v.PartialContent)
			mu.Lock()
			finalResponse.WriteString(v.PartialContent)
			mu.Unlock()
		case shogunate.StreamErrorMsg:
			slog.Debug("console streaming error", "error", v.Err)
			fmt.Printf("\nError: %v\n", v.Err)
		case shogunate.StreamMaxTokensReachedMsg:
			slog.Debug("console streaming max tokens reached", "content", v.Content)
			fmt.Printf("\n\n[Response truncated due to length limit]\n")
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

// getModelClient wraps internal/llm.GetModelClient for use in main package
func getModelClient(config *Config) (llms.Model, error) {
	return llm.GetModelClient(&config.LLM)
}
