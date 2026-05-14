// Asimi - AI coding agent governed by a 幕府 of six ministers and a sage
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
	isatty "github.com/mattn/go-isatty"
	"go.uber.org/fx"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

var program *tea.Program

// logBaseName names the log file (without extension). Default suits the TUI;
// runDaemonMode overrides it so daemon and TUI write to separate files and
// don't interleave when both run with --debug in the same cwd.
var logBaseName = "asimi"

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
		logPath = filepath.Join(logDir, logBaseName+".log")
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
		logPath = filepath.Join(logDir, logBaseName+".log")
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

	fmt.Printf("Asimi %s loading...\n", utils.AsimiVersion)

	var tuiModel *TUIModel
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
			ProvideGormDB,
			ProvideRepoInfo,
			ProvideScheduler,
			ProvideShellRunner,
			ProvidePromptHistory,
			ProvideCommandHistory,
			ProvideSessionHistory,
			ProvideTUIModel,
			StartTUI,
			ProvideShogunate,
		),
		fx.Populate(&tuiModel),
	)

	// Create fx app with all providers
	app := fx.New(fxOptions...)

	// Start the fx app (runs OnStart hooks for async initialization)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("failed to start fx app: %w", err)
	}
	// Opt-in wire modes. Precedence:
	//   ASIMI_DAEMON_SOCKET=/path — connect to a specific running daemon
	//   ASIMI_DAEMON=1            — autostart default daemon if needed
	//   ASIMI_LOOPBACK=1          — in-process net.Pipe loopback
	//   default                   — fully in-process, no RPC
	var onProgramReady func(*tea.Program)
	if sock := os.Getenv("ASIMI_DAEMON_SOCKET"); sock != "" {
		hook, err := installDaemonSocket(ctx, tuiModel, sock)
		if err != nil {
			return fmt.Errorf("daemon socket: %w", err)
		}
		onProgramReady = hook
	} else if os.Getenv("ASIMI_DAEMON") != "" {
		hook, err := installDaemonAutostart(ctx, tuiModel)
		if err != nil {
			return fmt.Errorf("daemon autostart: %w", err)
		}
		onProgramReady = hook
	} else if os.Getenv("ASIMI_LOOPBACK") != "" {
		hook, err := installRPCLoopback(ctx, tuiModel)
		if err != nil {
			return fmt.Errorf("loopback: %w", err)
		}
		onProgramReady = hook
	}

	tuiProgram := tea.NewProgram(tuiModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if onProgramReady != nil {
		onProgramReady(tuiProgram)
	}
	tuiModel.shogunate.SetRulingCtx(tuiModel.tabs.RulingCtx)

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	events := tuiModel.shogunate.Subscribe(subCtx)
	dispatcher := newNotificationDispatcher(tuiProgram)
	go func() {
		for {
			select {
			case <-subCtx.Done():
				dispatcher.close()
				return
			case msg, ok := <-events:
				if !ok {
					dispatcher.close()
					return
				}
				dispatcher.notify(msg)
			}
		}
	}()
	defer app.Stop(ctx)

	slog.Debug("[TIMING] fx app initialized", "duration", time.Since(startTime))

	// Check for updates in background (non-blocking)
	go func() {
		if AutoCheckForUpdates(utils.AsimiVersion) {
			tuiProgram.Send(updateAvailableMsg{})
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

	// fire an event to get the shogunate going
	// to avoid race condition where health check runs before model is ready
	_, runErr := tuiProgram.Run()

	if runErr != nil {
		return fmt.Errorf("alas, there's been an error: %w", runErr)
	}

	slog.Debug("[TIMING] Total Run() time", "duration", time.Since(startTime))
	return nil
}

type errMsg struct{ err error }

// llmInitSuccessMsg is sent when LLM initialization completes
// successfully. The bifrost client lives daemon-side now; callers use
// it only as a "we're ready, paint the provider" signal.
type llmInitSuccessMsg struct{}

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
	// Subcommand dispatch. A proper kong refactor can come later;
	// today a leading `daemon` arg is enough to branch cleanly.
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		os.Args = append(os.Args[:1], os.Args[2:]...)
		kong.Parse(&cli)
		if err := runDaemonMode(); err != nil {
			fmt.Fprintln(os.Stderr, "daemon:", err)
			os.Exit(1)
		}
		return
	}

	startTime := time.Now()
	kong.Parse(&cli)

	// Handle --version flag
	if cli.Version {
		fmt.Printf("Asimi CLI v%s\n", utils.AsimiVersion)
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
		fmt.Println("Not implmented yet")
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

// authTransport is used by models.go for list_models
type authTransport struct {
	token  string
	config *Config
	base   http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.config != nil && refreshOAuthToken(t.config) {
		t.token = t.config.LLM.AuthToken
	}
	r := req.Clone(req.Context())
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	}
	r.Header.Set("anthropic-beta", "oauth-2025-04-20,claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14")
	r.Header.Del("x-api-key")
	r.Header.Del("X-Api-Key")
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

// apiKeyTransport is used by models.go for list_models
type apiKeyTransport struct {
	base http.RoundTripper
}

func (t *apiKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14")
	if t.base == nil {
		t.base = http.DefaultTransport
	}
	return t.base.RoundTrip(r)
}

var _ = (http.RoundTripper)(&authTransport{})
var _ = (http.RoundTripper)(&apiKeyTransport{})
