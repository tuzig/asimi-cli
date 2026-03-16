package shogunate

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
	"gorm.io/gorm"
)

//go:embed context/realm.md
var realm string

//go:embed context/system_base.tmpl
var systemBase string
var ministerTmpl = template.Must(template.New("minister").Parse(systemBase))

// ZhengmingConn provides clarification request capabilities (behavioral interface)
type ZhengmingConn interface {
	RequestZhengming(edictID string, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (requestID string, err error)
	IsZhengmingPending(edictID string) (bool, error)
}

// EventEmitter emits events to the Ritual Guard's ledger (behavioral interface)
type EventEmitter interface {
	EmitEvent(edictID string, eventType storage.ShogunateEvent, payload storage.JSON) error
}

// === UNIFIED TYPES ===

// Prompt carries the user's message to the Chancellor
type Prompt struct {
	Ctx          context.Context   // Per-prompt context for cancellation (CTRL-C)
	Message      string            // The Ruler's words
	EdictID      string            // Empty = new edict, set = continue existing
	TabID        string            // Tab target for stream routing
	ContextFiles map[string]string // Files loaded via @ references
}

// Task carries work from Chancellor to a Minister
type Task struct {
	Ctx        context.Context     // Per-task cancellation (e.g. CTRL-C)
	EdictID    string              // The edict this task belongs to
	Work       string              // Specific instructions for the minister (renamed from Task to avoid Task.Task)
	Scratchpad string              // Pre-formatted markdown added to the context
	Session    *Session            // Existing session for multi-turn (nil = create new)
	Done       chan<- Result       // For completion signal
	Notify     internal.NotifyFunc // Routing-aware notify override (nil = use minister's default)
}

// Result signals a Minister has completed a Task
type Result struct {
	MinisterID string
	Sealed     bool // phase complete
	Output     string
	Failure    string   // soft failure reason (tool completed but found problems)
	Session    *Session // Return session for reuse by ritual runner
	Err        error
}

// Minister is the shared interface for all Shogunate ministers
type Minister interface {
	// ID returns the minister's unique identifier (e.g., "strategist", "forge")
	ID() string
	// Logger returns the minister's logger with scoped metadata.
	Logger() *slog.Logger
	// SystemPrompt returns the minister's system prompt template string.
	SystemPrompt() string
	// Scratchpad returns dynamic per-minister context (e.g., available rituals, rules)
	Scratchpad() string
	// Tools returns the minister's LLM tools for interactive sessions
	Tools() []Tool
	// Tasks returns the channel for submitting Tasks
	Tasks() chan<- *Task
	// SubmitPrompt sends a prompt to the minister
	SubmitPrompt(p *Prompt)
	// Run starts the minister's processing loop (blocks until context cancelled)
	Run(ctx context.Context)
	// RepoInfo returns the repository information
	RepoInfo() repo.RepoInfo
}

// --- External Dependencies ---

// LLMClient generates text using a language model
type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// GitOps provides git operations
type GitOps interface {
	Commit(ctx context.Context, changes []FileChange, message string) (commitHash string, err error)
}

// FileOp represents a file operation type
type FileOp int

const (
	FileOpCreate FileOp = iota
	FileOpModify
	FileOpDelete
)

// FileChange represents a file modification
type FileChange struct {
	Path    string
	Content []byte
	Op      FileOp
}

// EdictLock provides distributed locking for edicts
type EdictLock interface {
	Lock(edictID string) error
	Unlock(edictID string) error
}

// CIRunner executes CI pipelines
type CIRunner interface {
	Run(ctx context.Context, commitHash string) (outcome storage.VerdictOutcome, evidence storage.JSON, err error)
	GetTestSuite() string
}

// Linter performs static code analysis
type Linter interface {
	Analyze(ctx context.Context, filePath string) (violations []EthicsViolation, err error)
}

// EthicsViolation represents a censor finding
type EthicsViolation struct {
	Principle     string
	Ruling        storage.PrecedentRuling
	Justification string
}

// RCAAnalyzer performs root cause analysis on incidents
type RCAAnalyzer interface {
	Analyze(ctx context.Context, incidentID string) (*RCAReport, error)
}

// RCAReport contains the results of root cause analysis
type RCAReport struct {
	Summary string
	EdictID string
}

// --- Tool Interface ---

// Tool defines a tool that can be invoked by ministers
type Tool interface {
	Name() string
	Description() string
	Call(ctx context.Context, input string) (string, error)
	Format(input, result string, err error) string
	ParameterSchema() map[string]any
}

// ZhengmingPendingMsg notifies the UI of a pending clarification request
type ZhengmingPendingMsg struct {
	RequestID  string
	EdictID    string
	MinisterID string
	Questions  storage.ZhengmingQuestions
	Priority   storage.ZhengmingPriority
}

// ZhengmingAnsweredMsg notifies the UI that a clarification was answered
type ZhengmingAnsweredMsg struct {
	RequestID string
	Answer    string
}

// StreamDoneMsg signals that streaming has completed
type StreamDoneMsg struct{ TabID string }

// MinisterBase provides shared functionality for all ministers.
// Ministers embed this struct to gain database access and session creation capabilities.
type MinisterBase struct {
	db         *gorm.DB
	ministerID string
	model      llms.Model
	config     *SessionConfig
	repoInfo   repo.RepoInfo
	runner     runners.Runner
	logger     *slog.Logger
	notify     internal.NotifyFunc
	prompts    chan *Prompt
	publish    func(edictID string, eventType storage.ShogunateEvent, payload storage.JSON) string // routes events through Shogunate when set

	zhengmingMu       sync.Mutex
	onZhengmingRaised func()
}

// NewMinisterBase creates a base for all ministers with shared dependencies.
func NewMinisterBase(db *gorm.DB, runner runners.Runner, logger *slog.Logger) *MinisterBase {
	if logger == nil {
		logger = slog.Default()
	}
	return &MinisterBase{
		db:      db,
		runner:  runner,
		logger:  logger,
		prompts: make(chan *Prompt),
	}
}

// SubmitPrompt sends a prompt to the minister's channel.
func (m *MinisterBase) SubmitPrompt(p *Prompt) {
	m.prompts <- p
}

// PromptsChan returns the receive end of the prompts channel for Run() loops.
func (m *MinisterBase) PromptsChan() <-chan *Prompt {
	return m.prompts
}

// Runner returns the shell runner (may be nil)
func (m *MinisterBase) Runner() runners.Runner {
	return m.runner
}

// Logger returns the minister's logger with scoped metadata.
func (m *MinisterBase) Logger() *slog.Logger {
	if m.logger == nil {
		return slog.Default()
	}
	return m.logger
}

// Scratchpad returns dynamic per-minister context. Default is empty.
// Ministers can override this to provide context like available rituals, rules, etc.
func (m *MinisterBase) Scratchpad() string {
	return ""
}

// RepoInfo returns the repository information
func (m *MinisterBase) RepoInfo() repo.RepoInfo {
	return m.repoInfo
}

// CreateSessionOpts holds optional parameters for CreateSession.
type CreateSessionOpts struct {
	EdictID    string
	TabID      string
	Scratchpad string // Pre-formatted markdown context from ritual
}

// CreateSession creates a session for a minister with composed system prompt.
// The system prompt is built from the shared template with the minister's role injected.
// edictID is optional — when provided, it's included in the scratchpad context.
func CreateSession(minister Minister, model llms.Model, config *SessionConfig, notify internal.NotifyFunc, tabID string, edictID ...string) (*Session, error) {
	eid := ""
	if len(edictID) > 0 {
		eid = edictID[0]
	}
	systemPrompt := buildSystemPrompt(minister, config, eid)
	return NewSession(model, config, minister.Tools(), nil, notify, systemPrompt, tabID)
}

// CreateSessionWithOpts creates a session with extended options including given context.
func CreateSessionWithOpts(minister Minister, model llms.Model, config *SessionConfig, notify internal.NotifyFunc, opts CreateSessionOpts) (*Session, error) {
	systemPrompt := buildSystemPrompt(minister, config, opts.EdictID, opts.Scratchpad)
	return NewSession(model, config, minister.Tools(), nil, notify, systemPrompt, opts.TabID)
}

// buildSystemPrompt composes the system prompt by rendering the shared template
// with the minister's Role, Scratchpad, and project context from AGENTS.md.
// If edictID is non-empty, it's prepended to the scratchpad.
// Optional args, when provided, is appended to the scratchpad.
func buildSystemPrompt(minister Minister, config *SessionConfig, edictID string, args ...string) string {
	agentsFile := "AGENTS.md"
	if config != nil && config.AgentsFile != "" {
		agentsFile = config.AgentsFile
	}

	scratchpad := minister.Scratchpad()
	if edictID != "" {
		scratchpad = fmt.Sprintf("# Current Edict: %s\n\n%s", edictID, scratchpad)
	}
	if len(args) > 0 && args[0] != "" {
		scratchpad += "\n\n" + args[0]
	}

	// Get repo info and build environment block
	envBlock := sessBuildEnvBlock(minister.RepoInfo())

	var buf bytes.Buffer
	ministerTmpl.Execute(&buf, map[string]string{
		"Realm":          realm,
		"Role":           minister.SystemPrompt(),
		"MinisterID":     minister.ID(),
		"Scratchpad":     scratchpad,
		"ProjectContext": readProjectContext(agentsFile),
		"AgentsFile":     agentsFile,
		"EnvBlock":       envBlock,
	})
	return buf.String()
}

// formatScratchpad renders a given context map as readable markdown sections.
func formatScratchpad(ctx map[string]interface{}) string {
	if len(ctx) == 0 {
		return ""
	}
	var buf bytes.Buffer
	buf.WriteString("# Given Context\n\n")

	// Sort keys for deterministic output
	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		buf.WriteString("## ")
		buf.WriteString(key)
		buf.WriteString("\n\n")
		switch v := ctx[key].(type) {
		case string:
			buf.WriteString(v)
		default:
			b, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				buf.WriteString(fmt.Sprintf("%v", v))
			} else {
				buf.WriteString("```json\n")
				buf.Write(b)
				buf.WriteString("\n```")
			}
		}
		buf.WriteString("\n\n")
	}
	return buf.String()
}

// readProjectContext reads the project context file (AGENTS.md or CLAUDE.md) from the working directory.
func readProjectContext(agentsFile string) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(wd, agentsFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// SetMinisterConfig updates the MinisterBase configuration for session creation.
// This allows ministers to be configured with a model client after initialization.
func (m *MinisterBase) SetMinisterConfig(model llms.Model, config *SessionConfig, repoInfo repo.RepoInfo) {
	m.model = model
	m.config = config
	m.repoInfo = repoInfo
}

// SetNotify sets the notification callback.
func (m *MinisterBase) SetNotify(notify internal.NotifyFunc) {
	m.notify = notify
}

// SetOnZhengmingRaised sets a callback invoked when RequestZhengming is called.
// The ritual runner uses this to pause the step timeout while waiting for an answer.
func (m *MinisterBase) SetOnZhengmingRaised(cb func()) {
	m.zhengmingMu.Lock()
	defer m.zhengmingMu.Unlock()
	m.onZhengmingRaised = cb
}

// GenerateID creates a unique ID using SHA256.
// Exported for use by session.go envelope pattern.
func GenerateID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// generateIdempotencyKey creates a deterministic key for deduplication
func generateIdempotencyKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// RequestZhengming creates a clarification request
func (m *MinisterBase) RequestZhengming(edictID string, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
	requestID := GenerateID("zhengming", edictID, m.ministerID, fmt.Sprintf("%v", questions), time.Now().String())

	req := storage.Zhengming{
		RequestID:  requestID,
		EdictID:    edictID,
		MinisterID: m.ministerID,
		Questions:  questions,
		Priority:   priority,
		Status:     storage.ZhengmingPending,
		TimeoutAt:  time.Now().Add(24 * time.Hour), // Default 24h timeout
	}

	if priority == storage.PriorityUrgent {
		req.TimeoutAt = time.Now().Add(1 * time.Hour)
	}

	if err := m.db.Create(&req).Error; err != nil {
		return "", fmt.Errorf("failed to create zhengming request: %w", err)
	}

	// Notify ritual runner so it can pause the step timeout
	m.zhengmingMu.Lock()
	cb := m.onZhengmingRaised
	m.zhengmingMu.Unlock()
	if cb != nil {
		cb()
	}

	// Block the edict while zhengming is pending
	if edictID != "" {
		m.db.Model(&storage.Edict{}).
			Where("edict_id = ? AND status = ?", edictID, storage.EdictActive).
			Update("status", storage.EdictBlocked)
	}

	// Emit zhengming_requested event
	m.EmitEvent(edictID, "zhengming_requested", storage.JSON{
		"request_id":  requestID,
		"minister_id": m.ministerID,
		"questions":   questions,
		"priority":    string(priority),
	})

	return requestID, nil
}

// IsZhengmingPending checks if there are pending clarification requests for an edict
func (m *MinisterBase) IsZhengmingPending(edictID string) (bool, error) {
	var count int64
	err := m.db.Model(&storage.Zhengming{}).
		Where("edict_id = ? AND status = ?", edictID, storage.ZhengmingPending).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check pending zhengming: %w", err)
	}
	return count > 0, nil
}

// AnswerZhengming records the answer for a zhengming request
func (m *MinisterBase) AnswerZhengming(requestID, answer string) error {
	now := time.Now()
	result := m.db.Model(&storage.Zhengming{}).
		Where("request_id = ?", requestID).
		Updates(map[string]interface{}{
			"answer":      answer,
			"status":      storage.ZhengmingAnswered,
			"answered_at": &now,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to answer zhengming: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("zhengming request not found: %s", requestID)
	}
	return nil
}

// AppendToIntent appends clarification to the edict's intent
func (m *MinisterBase) AppendToIntent(edictID, clarification string) error {
	var edict storage.Edict
	if err := m.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		return fmt.Errorf("failed to get edict: %w", err)
	}

	newIntent := edict.Intent + "\n\n---\n**Clarification:**\n" + clarification

	result := m.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("intent", newIntent)
	if result.Error != nil {
		return fmt.Errorf("failed to append to intent: %w", result.Error)
	}
	return nil
}

// HandleZhengmingResponse processes a clarification response
func (m *MinisterBase) HandleZhengmingResponse(ctx context.Context, requestID, answer string) error {
	if err := m.AnswerZhengming(requestID, answer); err != nil {
		return fmt.Errorf("answer zhengming: %w", err)
	}

	var req storage.Zhengming
	if err := m.db.First(&req, "request_id = ?", requestID).Error; err != nil {
		return fmt.Errorf("get request: %w", err)
	}

	if req.EdictID != "" {
		if err := m.AppendToIntent(req.EdictID, answer); err != nil {
			slog.Warn("failed to append clarification to edict", "edict_id", req.EdictID, "error", err)
		}

		pending, err := m.IsZhengmingPending(req.EdictID)
		if err == nil && !pending {
			m.db.Model(&storage.Edict{}).
				Where("edict_id = ? AND status = ?", req.EdictID, storage.EdictBlocked).
				Update("status", storage.EdictActive)
		}
	}

	// Emit zhengming_answered event
	m.EmitEvent(req.EdictID, "zhengming_answered", storage.JSON{
		"request_id": requestID,
		"answer":     answer,
		"edict_id":   req.EdictID,
	})

	return nil
}

// EmitEvent records an event in the Tian ledger and delivers it via channel when available.
func (m *MinisterBase) EmitEvent(edictID string, eventType storage.ShogunateEvent, payload storage.JSON) error {
	if m.publish != nil {
		_ = m.publish(edictID, eventType, payload)
		return nil
	}
	// TODO: remove the fallback, test should pass a publish stub
	// Fallback: DB-only (for tests/standalone)
	event := storage.TianEvent{
		EdictID:   edictID,
		EventType: eventType,
		Payload:   payload,
	}
	if err := m.db.Create(&event).Error; err != nil {
		return fmt.Errorf("failed to emit event: %w", err)
	}
	return nil
}

// grantSeal records the minister's seal on an edict
func (m *MinisterBase) grantSeal(edictID string, metadata storage.JSON) error {
	// Check if minister already sealed this edict
	hasSeal, err := m.hasSeal(edictID)
	if err != nil {
		return fmt.Errorf("check existing seal: %w", err)
	}
	if hasSeal {
		m.logger.Debug("seal already granted", "edict_id", edictID, "minister_id", m.ministerID)
		return nil
	}

	// Create seal with metadata
	sealID := GenerateID("seal", edictID, m.ministerID)
	seal := storage.Seal{
		SealID:     sealID,
		EdictID:    edictID,
		MinisterID: m.ministerID,
		SealedAt:   time.Now(),
		Metadata:   metadata,
	}

	if err := m.db.Create(&seal).Error; err != nil {
		return fmt.Errorf("failed to grant seal: %w", err)
	}

	// Emit event
	m.EmitEvent(edictID, storage.EventSealGranted, storage.JSON{
		"minister_id": m.ministerID,
		"seal_id":     sealID,
	})

	m.logger.Info("seal granted", "edict_id", edictID, "seal_id", sealID)
	return nil
}

// hasSeal checks if the minister has already sealed this edict
func (m *MinisterBase) hasSeal(edictID string) (bool, error) {
	var count int64
	err := m.db.Model(&storage.Seal{}).
		Where("edict_id = ? AND minister_id = ?", edictID, m.ministerID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check seal: %w", err)
	}
	return count > 0, nil
}

// GetEdict retrieves an edict by ID
func (m *MinisterBase) GetEdict(edictID string) (*storage.Edict, error) {
	var edict storage.Edict
	if err := m.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("edict not found: %s", edictID)
		}
		return nil, fmt.Errorf("failed to get edict: %w", err)
	}
	return &edict, nil
}

// sessBuildEnvBlock constructs a markdown summary of the OS, shell, and key paths.
func sessBuildEnvBlock(repoInfo repo.RepoInfo) string {
	var env strings.Builder

	env.WriteString(fmt.Sprintf("- **OS:** %s\n", runtime.GOOS))
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		env.WriteString(fmt.Sprintf("- **Working copy path:** %s\n", cwd))
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	env.WriteString(fmt.Sprintf("- **Shell:** %s\n", shell))

	if repoInfo.Branch != "" {
		env.WriteString(fmt.Sprintf("- **Branch:** %s\n", repoInfo.Branch))
	}

	if repoInfo.IsWorktree && repoInfo.Branch != "dev" {
		env.WriteString(
			`\n\n**IMPORTANT:** Working on worktree so commits will be quashed.
Feel free to commit whenever you can summarize the changes in a meaningful commit message.`)
	}

	return env.String()
}
