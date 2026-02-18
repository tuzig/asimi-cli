package shogunate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
	"gorm.io/gorm"
)

// ministerSystemPromptTemplate is the shared template for all minister system prompts.
// Ministers provide their Role (identity text) and optional Scratchpad (dynamic context).
const ministerSystemPromptTemplate = `You are a minister in the Shogunate.

{{.Role}}

{{.Scratchpad}}
{{- if .ProjectContext}}

--- Project specific directions from: {{.AgentsFile}} ---
{{.ProjectContext}}
--- End of Directions from: {{.AgentsFile}} ---
{{- end}}`

// ZhengmingConn provides clarification request capabilities (behavioral interface)
type ZhengmingConn interface {
	RequestZhengming(edictID, question string, priority storage.ZhengmingPriority) (requestID string, err error)
	IsZhengmingPending(edictID string) (bool, error)
}

// EventEmitter emits events to the Ritual Guard's ledger (behavioral interface)
type EventEmitter interface {
	EmitEvent(edictID, eventType string, payload storage.JSON) error
}

// === UNIFIED TYPES ===

// Prompt carries the user's message to the Chancellor
type Prompt struct {
	Ctx          context.Context   // Per-prompt context for cancellation (CTRL-C)
	Message      string            // The Ruler's words
	EdictID      string            // Empty = new edict, set = continue existing
	ContextFiles map[string]string // Files loaded via @ references
}

// Task carries work from Chancellor to a Minister
type Task struct {
	EdictID string       // The edict this task belongs to
	Work    string       // Specific instructions for the minister (renamed from Task to avoid Task.Task)
	Done    chan<- Result // For completion signal
}

// Result signals a Minister has completed a Task
type Result struct {
	MinisterID string
	Sealed     bool // phase complete
	Output     string
	Err        error
}

// Minister is the shared interface for all Shogunate ministers
type Minister interface {
	// ID returns the minister's unique identifier (e.g., "strategist", "forge")
	ID() string
	// Logger returns the minister's logger with scoped metadata.
	Logger() *slog.Logger
	// Role returns the minister's role identity text (injected into system prompt template)
	Role() string
	// Scratchpad returns dynamic per-minister context (e.g., available rituals, rules)
	Scratchpad() string
	// Tools returns the minister's LLM tools for interactive sessions
	Tools() []Tool
	// Tasks returns the channel for submitting Tasks
	Tasks() chan<- *Task
	// Run starts the minister's processing loop (blocks until context cancelled)
	Run(ctx context.Context)
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
	Question   string
	Priority   storage.ZhengmingPriority
}

// ZhengmingAnsweredMsg notifies the UI that a clarification was answered
type ZhengmingAnsweredMsg struct {
	RequestID string
	Answer    string
}

// StreamDoneMsg signals that streaming has completed
type StreamDoneMsg struct{}

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
}

// NewMinisterBase creates a base for all ministers with shared dependencies.
func NewMinisterBase(db *gorm.DB, model llms.Model, config *SessionConfig, repoInfo repo.RepoInfo, runner runners.Runner, logger *slog.Logger) MinisterBase {
	if logger == nil {
		logger = slog.Default()
	}
	return MinisterBase{
		db:       db,
		model:    model,
		config:   config,
		repoInfo: repoInfo,
		runner:   runner,
		logger:   logger,
	}
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

// CreateSession creates a session for a minister with composed system prompt.
// The system prompt is built from the shared template with the minister's role injected.
// edictID is optional — when provided, it's included in the scratchpad context.
func (m *MinisterBase) CreateSession(minister Minister, edictID ...string) (*Session, error) {
	tools := minister.Tools()
	eid := ""
	if len(edictID) > 0 {
		eid = edictID[0]
	}
	systemPrompt := m.buildSystemPrompt(minister, eid)

	return NewSession(m.model, m.config, m.repoInfo, tools, nil, m.notify, systemPrompt)
}

// buildSystemPrompt composes the system prompt by rendering the shared template
// with the minister's Role, Scratchpad, and project context from AGENTS.md.
// If edictID is non-empty, it's prepended to the scratchpad.
func (m *MinisterBase) buildSystemPrompt(minister Minister, edictID string) string {
	agentsFile := "AGENTS.md"
	if m.config != nil && m.config.AgentsFile != "" {
		agentsFile = m.config.AgentsFile
	}

	scratchpad := minister.Scratchpad()
	if edictID != "" {
		scratchpad = fmt.Sprintf("# Current Edict: %s\n\n%s", edictID, scratchpad)
	}

	tmpl := template.Must(template.New("minister").Parse(ministerSystemPromptTemplate))
	var buf bytes.Buffer
	tmpl.Execute(&buf, map[string]string{
		"Role":           minister.Role(),
		"Scratchpad":     scratchpad,
		"ProjectContext": readProjectContext(agentsFile),
		"AgentsFile":     agentsFile,
	})
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
func (m *MinisterBase) RequestZhengming(edictID, question string, priority storage.ZhengmingPriority) (string, error) {
	requestID := GenerateID("zhengming", edictID, m.ministerID, question, time.Now().String())

	req := storage.Zhengming{
		RequestID:  requestID,
		EdictID:    edictID,
		MinisterID: m.ministerID,
		Question:   question,
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

	// Halt the edict while zhengming is pending
	if edictID != "" {
		m.db.Model(&storage.Edict{}).
			Where("edict_id = ?", edictID).
			Update("halted", true)
	}

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

// EmitEvent records an event in the Tian ledger
func (m *MinisterBase) EmitEvent(edictID, eventType string, payload storage.JSON) error {
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

// UpdatePhase transitions an edict to a new phase
func (m *MinisterBase) UpdatePhase(edictID string, phase storage.EdictPhase) error {
	result := m.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("current_phase", phase)
	if result.Error != nil {
		return fmt.Errorf("failed to update phase: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
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
