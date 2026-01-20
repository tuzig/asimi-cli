package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/llm"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// ResponseType identifies the kind of response sent on the channel
type ResponseType int

const (
	ResponseStream       ResponseType = iota // streaming text chunk
	ResponseComplete                         // stream finished
	ResponseError                            // error occurred
	ResponseEdictCreated                     // new edict started
	ResponseZhengming                        // clarification needed
	ResponseToolResult                       // tool execution result
)

// Response is sent on the Responses channel to notify the TUI of events
type Response struct {
	Type    ResponseType
	EdictID string
	Content string // for streaming text
	Error   error  // for errors
	Data    any    // for structured data (tool results, etc.)
}

// ShogunateConfig holds configuration for the Shogunate
type ShogunateConfig struct {
	PollInterval time.Duration
	AgentsFile   string
	DBPath       string // Path to SQLite database for Chancellor SQL access
}

// DefaultShogunateConfig returns the default configuration
func DefaultShogunateConfig() *ShogunateConfig {
	return &ShogunateConfig{
		PollInterval: 5 * time.Second,
		AgentsFile:   "AGENTS.md",
	}
}

// Shogunate coordinates all ministers and manages sessions per-edict.
// It is the long-lived service that owns the workflow orchestration.
type Shogunate struct {
	// Response channel (TUI reads from this)
	Responses chan Response

	db     *gorm.DB
	logger *slog.Logger
	config *ShogunateConfig

	// LLM client (set when connected)
	llmClient llms.Model
	llmMu     sync.RWMutex

	// All ministers
	Chancellor  *Chancellor
	Strategist  *Strategist
	Forge       *Forge
	Judge       *Judge
	Censor      *Censor
	Marshal     *Marshal
	RitualGuard *RitualGuard

	// Active edict for UI display
	activeEdictID string
	activeEdictMu sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// NewShogunate creates a new Shogunate coordinator
func NewShogunate(db *gorm.DB, config *ShogunateConfig, logger *slog.Logger) *Shogunate {
	if logger == nil {
		logger = slog.Default()
	}
	if config == nil {
		config = DefaultShogunateConfig()
	}
	return &Shogunate{
		db:        db,
		logger:    logger,
		config:    config,
		Responses: make(chan Response, 100),
	}
}

// start initializes all ministers and starts their goroutines
func (s *Shogunate) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Create all connections
	strategistConn := NewStrategistConn(s.db)
	judgeConn := NewJudgeConn(s.db)
	censorConn := NewCensorConn(s.db)
	marshalConn := NewMarshalConn(s.db)
	ritualGuardConn := NewRitualGuardConn(s.db)

	// Create ministers
	s.Strategist = NewStrategist(strategistConn, nil, s.logger)
	s.Forge = NewForge(s.db, nil, s.logger) // tools set later via SetTools
	s.Judge = NewJudge(judgeConn, nil, s.logger)
	s.Censor = NewCensor(censorConn, nil, s.logger)
	s.Marshal = NewMarshal(marshalConn, nil, s.logger)

	// Create Chancellor
	s.Chancellor = NewChancellor(s.db, s.llmClient, nil, repo.RepoInfo{}, s.logger)
	s.Chancellor.SetShogunate(s)

	// Create RitualGuard with Chancellor reference
	s.RitualGuard = NewRitualGuard(ritualGuardConn, s.Chancellor, s.logger)

	// Wire Chancellor to Forge for tool routing
	// TODO: remove the forge
	s.Chancellor.SetForge(s.Forge)
	s.Chancellor.SetDBPath(s.config.DBPath)

	// Build and set Chancellor's tool definitions for LLM calls
	chancellorTools := s.Chancellor.Tools(func(msg any) {
		// Forward zhengming notifications to the response channel
		if zm, ok := msg.(ZhengmingPendingMsg); ok {
			s.sendResponse(Response{
				Type:    ResponseZhengming,
				EdictID: zm.EdictID,
				Content: zm.Question,
				Data:    zm,
			})
		}
	})

	// Build tool catalog for direct execution
	toolCatalog := make(map[string]Tool)
	for _, tool := range chancellorTools {
		toolCatalog[tool.Name()] = tool
	}
	s.Chancellor.SetToolCatalog(toolCatalog)
	s.logger.Info("chancellor tools configured", "catalog", len(toolCatalog))

	// Start all minister goroutines
	go s.Chancellor.Run(s.ctx) // Chancellor listens for prompts from the Ruler
	go s.Strategist.Run(s.ctx, s.config.PollInterval)
	go s.Forge.Run(s.ctx)
	go s.Judge.Run(s.ctx, s.config.PollInterval)
	go s.Censor.Run(s.ctx, s.config.PollInterval)
	go s.Marshal.Run(s.ctx, s.config.PollInterval)
	go s.runRitualGuard()

	s.logger.Info("shogunate started",
		"poll_interval", s.config.PollInterval,
		"ministers", []string{"chancellor", "strategist", "forge", "judge", "censor", "marshal", "ritual_guard"})

	return nil
}

// runRitualGuard runs the RitualGuard polling loop
func (s *Shogunate) runRitualGuard() {
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	s.logger.Info("ritual guard started", "poll_interval", s.config.PollInterval)

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("ritual guard stopped")
			return
		case <-ticker.C:
			runCtx, runCancel := context.WithTimeout(s.ctx, 30*time.Second)
			if err := s.RitualGuard.Run(runCtx); err != nil {
				s.logger.Error("ritual guard cycle failed", "error", err)
			}
			runCancel()
		}
	}
}

// Stop gracefully shuts down all ministers
func (s *Shogunate) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	close(s.Responses)
	s.logger.Info("shogunate stopped")
	return nil
}

// routeToEdict routes a prompt to an existing edict's session
func (s *Shogunate) routeToEdict(ctx context.Context, edictID, prompt string) error {
	// Append to the edict's intent if this is additional context
	if err := s.Chancellor.AppendToRenIntent(edictID, prompt); err != nil {
		s.sendError(err)
		return err
	}
	return nil
}

// SetTools updates the Forge's tool registry
func (s *Shogunate) SetTools(tools map[string]Tool) {
	if s.Forge != nil {
		s.Forge.SetTools(tools)
	}
}

// SetLLMClient sets the LLM client for ministers to use
func (s *Shogunate) SetLLMClient(llm llms.Model) {
	s.llmMu.Lock()
	defer s.llmMu.Unlock()
	s.llmClient = llm
	// Also set on Chancellor's MinisterBase so CreateSession works
	if s.Chancellor != nil {
		s.Chancellor.llm = llm
	}
	s.logger.Info("LLM client set for shogunate")
}

// SetChancellorConfig configures the Chancellor for session creation.
// This should be called after SetLLMClient with the session configuration.
func (s *Shogunate) SetChancellorConfig(llm llms.Model, config *SessionConfig, repoInfo repo.RepoInfo) {
	if s.Chancellor != nil {
		s.Chancellor.SetMinisterConfig(llm, config, repoInfo)
		s.logger.Info("chancellor configured for session creation")
	}
}

// IsReady returns true if the Shogunate has an LLM client configured
// TODO: remove this
func (s *Shogunate) IsReady() bool {
	s.llmMu.RLock()
	defer s.llmMu.RUnlock()
	return s.llmClient != nil
}

// GetActiveEdictID returns the currently active edict ID
func (s *Shogunate) GetActiveEdictID() string {
	s.activeEdictMu.RLock()
	defer s.activeEdictMu.RUnlock()
	return s.activeEdictID
}

// GetMinister returns a minister by ID
func (s *Shogunate) GetMinister(id string) Minister {
	switch id {
	case "chancellor":
		return s.Chancellor
	case "strategist":
		return s.Strategist
	case "forge":
		return s.Forge
	case "judge":
		return s.Judge
	case "censor":
		return s.Censor
	case "marshal":
		return s.Marshal
	default:
		return nil
	}
}

// setActiveEdict sets the active edict ID
func (s *Shogunate) setActiveEdict(edictID string) {
	s.activeEdictMu.Lock()
	defer s.activeEdictMu.Unlock()
	s.activeEdictID = edictID
}

// sendResponse sends a response to the TUI
func (s *Shogunate) sendResponse(r Response) {
	select {
	case s.Responses <- r:
	default:
		s.logger.Warn("response channel full, dropping response", "type", r.Type)
	}
}

// sendError sends an error response to the TUI
func (s *Shogunate) sendError(err error) {
	s.sendResponse(Response{
		Type:  ResponseError,
		Error: err,
	})
}

// executeChancellorSQL finds and executes sqlite3 commands in the Chancellor's response
func (s *Shogunate) executeChancellorSQL(ctx context.Context, response, edictID string) {
	// Look for sqlite3 command pattern
	// Pattern: sqlite3 <path> "<sql>"
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "sqlite3 ") {
			continue
		}

		// Replace EDICT_ID placeholder with actual ID
		cmd := strings.ReplaceAll(line, "EDICT_ID", edictID)

		s.logger.Info("chancellor executing SQL", "command", cmd)

		// Execute via shell (sqlite3 is safe for this use case)
		// Use os/exec directly for simplicity
		output, err := s.runSQLCommand(ctx, cmd)
		if err != nil {
			s.logger.Error("chancellor SQL failed", "error", err, "output", output)
		} else {
			s.logger.Info("chancellor SQL executed", "output", output)
		}
	}
}

// runSQLCommand executes a sqlite3 command
func (s *Shogunate) runSQLCommand(ctx context.Context, cmd string) (string, error) {
	// Parse the command to extract parts
	// Expected format: sqlite3 /path/to/db "SQL STATEMENT"
	parts := strings.SplitN(cmd, " ", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid sqlite3 command format")
	}

	dbPath := parts[1]
	sql := strings.Trim(parts[2], "\"'")

	execCmd := exec.CommandContext(ctx, "sqlite3", dbPath, sql)
	output, err := execCmd.CombinedOutput()
	return string(output), err
}

// --- Session Wrapper Methods ---
// These methods provide access to the Chancellor's session for TUI operations.
// All methods handle nil receiver case for safe use when Shogunate is not initialized.

// Session returns the current session from Chancellor
func (s *Shogunate) Session() *Session {
	if s == nil || s.Chancellor == nil {
		return nil
	}
	return s.Chancellor.session
}

// SaveSession saves the current session
func (s *Shogunate) SaveSession() {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return
	}
	s.Chancellor.session.Save()
}

// GetContextInfo returns context usage information from the current session
func (s *Shogunate) GetContextInfo() ContextInfo {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return ContextInfo{}
	}
	return s.Chancellor.session.GetContextInfo()
}

// GetContextUsagePercent returns the context usage percentage from the current session
func (s *Shogunate) GetContextUsagePercent() float64 {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return 0
	}
	return s.Chancellor.session.GetContextUsagePercent()
}

// ClearHistory clears the session's conversation history
func (s *Shogunate) ClearHistory() {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return
	}
	s.Chancellor.session.ClearHistory()
}

// RollbackTo rolls back the session to a previous message snapshot
func (s *Shogunate) RollbackTo(snapshot int) {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return
	}
	s.Chancellor.session.RollbackTo(snapshot)
}

// GetMessageSnapshot returns the current message count for rollback purposes
func (s *Shogunate) GetMessageSnapshot() int {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return 0
	}
	return s.Chancellor.session.GetMessageSnapshot()
}

// CompactHistory compacts the conversation history using the provided prompt
func (s *Shogunate) CompactHistory(ctx context.Context, compactPrompt string) (string, error) {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return "", fmt.Errorf("no active session")
	}
	return s.Chancellor.session.CompactHistory(ctx, compactPrompt)
}

// UpdateTokenCounts updates the token counts with context files
func (s *Shogunate) UpdateTokenCounts(contextFiles map[string]string) {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return
	}
	s.Chancellor.session.UpdateTokenCounts(contextFiles)
}

// AskWithStreaming sends a prompt to the LLM with streaming response
func (s *Shogunate) AskWithStreaming(ctx context.Context, prompt string, contextFiles map[string]string) (string, error) {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return "", fmt.Errorf("no active session")
	}
	return s.Chancellor.session.AskWithStreaming(ctx, prompt, contextFiles)
}

// SetSessionStore sets the session store and auto-save setting
func (s *Shogunate) SetSessionStore(store SessionStore, autoSave bool) {
	if s == nil || s.Chancellor == nil || s.Chancellor.session == nil {
		return
	}
	s.Chancellor.session.SetStore(store, autoSave)
}

// SetSessionFromResumed copies fields from a loaded session to the active session
func (s *Shogunate) SetSessionFromResumed(loaded *Session) {
	if s == nil || s.Chancellor == nil || loaded == nil {
		return
	}
	sess := s.Chancellor.session
	if sess == nil {
		return
	}
	sess.ID = loaded.ID
	sess.CreatedAt = loaded.CreatedAt
	sess.LastUpdated = loaded.LastUpdated
	sess.FirstPrompt = loaded.FirstPrompt
	sess.Provider = loaded.Provider
	sess.Model = loaded.Model
	sess.WorkingDir = loaded.WorkingDir
	sess.ProjectSlug = loaded.ProjectSlug
	sess.Messages = make([]llms.MessageContent, len(loaded.Messages))
	copy(sess.Messages, loaded.Messages)
}

// SetTestSession sets a session directly for testing purposes.
// This should only be used in tests.
func (s *Shogunate) SetTestSession(sess *Session) {
	if s == nil {
		return
	}
	if s.Chancellor == nil {
		s.Chancellor = &Chancellor{}
	}
	s.Chancellor.session = sess
}

// ShogunateParams holds parameters for Shogunate initialization
type ShogunateParams struct {
	fx.In
	Lifecycle fx.Lifecycle
	DB        *storage.DB
	LLMConfig *config.LLMConfig
	Logger    *slog.Logger
}

// ShogunateResult holds the Shogunate instance
type ShogunateResult struct {
	fx.Out
	Shogunate *Shogunate
}

// ProvideShogunate creates the Shogunate coordinator with fx lifecycle
func ProvideShogunate(params ShogunateParams) (ShogunateResult, error) {
	shogunateConfig := DefaultShogunateConfig()
	shogunateConfig.DBPath = params.DB.Path() // For Chancellor SQL access
	s := NewShogunate(params.DB.Conn(), shogunateConfig, params.Logger)

	// Initialize LLM client and set on Shogunate
	llmClient, err := llm.GetModelClient(params.LLMConfig)
	if err != nil {
		// Log warning but don't fail - user can configure model later via :models
		params.Logger.Warn("LLM client initialization failed", "error", err)
	} else {
		s.SetLLMClient(llmClient)
	}

	// Register lifecycle hooks
	params.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return s.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return s.Stop()
		},
	})

	return ShogunateResult{Shogunate: s}, nil
}
