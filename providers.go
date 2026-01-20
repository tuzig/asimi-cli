package main

import (
	"context"
	"fmt"
	"log/slog"

	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/fx"
)

// LoggerResult holds the configured logger
type LoggerResult struct {
	fx.Out
	Logger *slog.Logger
}

// ProvideLogger creates and returns a logger instance
func ProvideLogger() (LoggerResult, error) {
	initLogger()
	return LoggerResult{
		Logger: slog.Default(),
	}, nil
}

// ProvideConfig loads and returns the application configuration
func ProvideConfig(logger *slog.Logger) (*Config, error) {
	logger.Info("loading configuration")

	// Ensure user config file exists (creates it on first run)
	created, err := EnsureUserConfigExists()
	if err != nil {
		logger.Warn("failed to ensure user config exists", "error", err)
	} else if created {
		logger.Info("created user config file on first run")
		ConfigCreated = true
	}

	config, err := LoadConfig()
	if err != nil {
		logger.Info("using default configuration due to load failure")
		logger.Debug("Warning: Using defaults due to config load failure", "error", err)
		// Continue with default config
		config = &Config{
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
	// Override from CLI flag
	if cli.NoCleanup {
		config.RunShellCommand.NoCleanup = true
	}
	logger.Info("configuration loaded")
	return config, nil
}

// StorageParams holds parameters for storage initialization
type StorageParams struct {
	fx.In
	Lifecycle fx.Lifecycle
	Config    *Config
	Logger    *slog.Logger
}

// StorageResult holds the storage initialization result
type StorageResult struct {
	fx.Out
	DB *storage.DB
}

// ProvideStorage initializes the SQLite storage database
func ProvideStorage(params StorageParams) (StorageResult, error) {
	params.Logger.Info("initializing storage", "database_path", params.Config.Storage.DatabasePath)
	db, err := storage.InitDB(params.Config.Storage.DatabasePath)
	if err != nil {
		params.Logger.Error("failed to initialize storage", "error", err)
		return StorageResult{}, fmt.Errorf("failed to initialize storage: %w", err)
	}
	params.Logger.Info("storage initialized successfully")

	// Register cleanup on shutdown
	params.Lifecycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			params.Logger.Info("closing storage")
			if err := db.Close(); err != nil {
				params.Logger.Error("failed to close storage", "error", err)
				return err
			}
			params.Logger.Info("storage closed successfully")
			return nil
		},
	})

	return StorageResult{DB: db}, nil
}

// ProvideRepoInfo returns information about the git repository
func ProvideRepoInfo(config *Config, logger *slog.Logger) RepoInfo {
	logger.Info("detecting git repository")
	repoInfo := GetRepoInfo()
	if repoInfo.ProjectRoot != "" {
		logger.Info("git repository detected", "root", repoInfo.ProjectRoot, "branch", repoInfo.Branch)
	} else {
		logger.Info("no git repository found")
	}
	return repoInfo
}

// ProvideScheduler creates the tool scheduler for dependency injection
func ProvideScheduler() *shogunate.CoreToolScheduler {
	return shogunate.NewCoreToolScheduler(nil)
}

// ShellRunnerParams holds parameters for shell runner initialization
type ShellRunnerParams struct {
	fx.In
	Lifecycle fx.Lifecycle
	Config    *Config
	RepoInfo  RepoInfo
	Scheduler *shogunate.CoreToolScheduler
	Logger    *slog.Logger
}

// ProvideShellRunner creates and returns a shell runner with proper lifecycle management
func ProvideShellRunner(params ShellRunnerParams) shellRunner {
	params.Logger.Info("initializing shell runner")

	// Use auto-detection to select the appropriate shell runner
	initShellRunner(params.Config, params.Scheduler)
	runner := getShellRunner()

	params.Logger.Info("shell runner initialized", "type", runner.RunnerType())

	// Register cleanup hook to close the shell runner when app stops
	params.Lifecycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			params.Logger.Info("shutting down shell runner")
			return runner.Close(ctx)
		},
	})

	return runner
}

// ProvideLLMConfig extracts the LLMConfig from the main Config for use by shogunate
func ProvideLLMConfig(config *Config) *internalconfig.LLMConfig {
	return &config.LLM
}

// PromptHistoryResult holds the prompt history store
type PromptHistoryResult struct {
	fx.Out
	History *PromptHistory `name:"prompt"`
}

// CommandHistoryResult holds the command history store
type CommandHistoryResult struct {
	fx.Out
	History *CommandHistory `name:"command"`
}

// ProvidePromptHistory creates and returns the prompt history store
func ProvidePromptHistory(db *storage.DB, repoInfo RepoInfo, logger *slog.Logger) (PromptHistoryResult, error) {
	logger.Info("loading prompt history")
	historyStore, err := NewPromptHistoryStore(db, repoInfo)
	if err != nil {
		logger.Warn("failed to initialize prompt history store", "error", err)
		return PromptHistoryResult{History: nil}, nil // Don't fail, just return nil
	}
	return PromptHistoryResult{History: historyStore}, nil
}

// ProvideCommandHistory creates and returns the command history store
func ProvideCommandHistory(db *storage.DB, repoInfo RepoInfo, logger *slog.Logger) (CommandHistoryResult, error) {
	logger.Info("loading command history")
	historyStore, err := NewCommandHistoryStore(db, repoInfo)
	if err != nil {
		logger.Warn("failed to initialize command history store", "error", err)
		return CommandHistoryResult{History: nil}, nil // Don't fail, just return nil
	}
	return CommandHistoryResult{History: historyStore}, nil
}

// SessionHistoryParams holds parameters for session history initialization
type SessionHistoryParams struct {
	fx.In
	Lifecycle fx.Lifecycle
	DB        *storage.DB
	Config    *Config
	RepoInfo  RepoInfo
	Logger    *slog.Logger
}

// ProvideSessionHistory creates and returns the session history store
func ProvideSessionHistory(params SessionHistoryParams) (*SessionStore, error) {
	if !params.Config.Session.Enabled {
		return nil, nil // Session storage is disabled
	}

	params.Logger.Info("loading session history")
	maxSessions := 50
	maxAgeDays := 30
	if params.Config.Session.MaxSessions > 0 {
		maxSessions = params.Config.Session.MaxSessions
	}
	if params.Config.Session.MaxAgeDays > 0 {
		maxAgeDays = params.Config.Session.MaxAgeDays
	}

	store, err := NewSessionStore(params.DB, params.RepoInfo, maxSessions, maxAgeDays)
	if err != nil {
		params.Logger.Error("failed to create session store", "error", err)
		return nil, nil // Don't fail startup
	}

	// Register cleanup on shutdown
	params.Lifecycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			params.Logger.Info("closing session store")
			store.Close()
			return nil
		},
	})

	return store, nil
}

// TUIModelParams holds parameters for TUI model creation
type TUIModelParams struct {
	fx.In
	Config         *Config
	RepoInfo       RepoInfo
	PromptHistory  *PromptHistory  `name:"prompt"`
	CommandHistory *CommandHistory `name:"command"`
	SessionStore   *SessionStore
	DB             *storage.DB
	Scheduler      *shogunate.CoreToolScheduler
	Shogunate      *shogunate.Shogunate
	Logger         *slog.Logger
}

// ProvideTUIModel creates and returns the TUI model
func ProvideTUIModel(params TUIModelParams) *TUIModel {
	return NewTUIModel(params.Config, &params.RepoInfo, params.PromptHistory, params.CommandHistory, params.SessionStore, params.DB, params.Scheduler, params.Shogunate)
}

// TUIProgramParams holds parameters for TUI program initialization
type TUIProgramParams struct {
	fx.In
	Model     *TUIModel
	Lifecycle fx.Lifecycle
	Logger    *slog.Logger
}

// StartTUI creates the TUI program
func StartTUI(params TUIProgramParams) *tea.Program {
	params.Logger.Info("creating TUI program")

	// Create the bubbletea program with alt screen and mouse support
	prog := tea.NewProgram(params.Model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Set global program reference so async operations can send messages
	program = prog

	return prog
}
