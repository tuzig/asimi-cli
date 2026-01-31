package shogunate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
	"gorm.io/gorm"
)

// ZhengmingConn provides clarification request capabilities (behavioral interface)
type ZhengmingConn interface {
	RequestZhengming(edictID, question string, priority storage.ZhengmingPriority) (requestID string, err error)
	IsZhengmingPending(edictID string) (bool, error)
}

// EventEmitter emits events to the Ritual Guard's ledger (behavioral interface)
type EventEmitter interface {
	EmitEvent(edictID, eventType string, payload storage.JSON) error
}

// TaskEnvelope carries a task from Chancellor to a minister.
// Each envelope includes return address (ReplyChan) for results.
type TaskEnvelope struct {
	EdictID   string            // The edict this task belongs to
	Task      string            // Specific instructions for the minister
	ReplyChan chan<- *TaskReply // Return channel for results
}

// TaskReply is sent back by ministers when a task completes.
type TaskReply struct {
	EdictID    string // Echo back for correlation
	MinisterID string // Which minister completed this
	Task       string // Echo back the task for logging
	Sealed     bool   // True if phase is complete
	Output     string // Result/response text
	Error      error  // Any error that occurred
}

// Minister is the shared interface for all Shogunate ministers
type Minister interface {
	// ID returns the minister's unique identifier (e.g., "strategist", "forge")
	ID() string
	// Title returns the minister's honorific title.
	Title() string
	// Logger returns the minister's logger with scoped metadata.
	Logger() *slog.Logger
	// Role returns the minister's role identity text (injected into system prompt template)
	Role() string
	// Tools returns the minister's LLM tools for interactive sessions
	Tools(notify NotifyFunc) []Tool
	// Tasks returns the channel for submitting TaskEnvelopes
	Tasks() chan<- *TaskEnvelope
	// Events returns the Shogunate events the minister listens to.
	Events() []ShogunateEvent
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

// NotifyFunc is a callback for sending notifications to the UI
type NotifyFunc func(any)

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

// MinisterBase provides shared functionality for all ministers.
// Ministers embed this struct to gain database access and session creation capabilities.
type MinisterBase struct {
	db         *gorm.DB
	ministerID string
	llm        llms.Model
	config     *SessionConfig
	repoInfo   repo.RepoInfo
	logger     *slog.Logger
}

// NewMinisterBase creates a base for all ministers with shared dependencies.
func NewMinisterBase(db *gorm.DB, llm llms.Model, config *SessionConfig, repoInfo repo.RepoInfo, logger *slog.Logger) MinisterBase {
	if logger == nil {
		logger = slog.Default()
	}
	return MinisterBase{
		db:       db,
		llm:      llm,
		config:   config,
		repoInfo: repoInfo,
		logger:   logger,
	}
}

// Logger returns the minister's logger with scoped metadata.
func (m *MinisterBase) Logger() *slog.Logger {
	if m.logger == nil {
		return slog.Default()
	}
	return m.logger
}

// Events returns the Shogunate events the minister listens to.
func (m *MinisterBase) Events() []ShogunateEvent {
	return nil
}

// CreateSession creates a session for a minister with composed system prompt.
// The system prompt is built from the shared template with the minister's role injected.
func (m *MinisterBase) CreateSession(minister Minister, notify NotifyFunc) (*Session, error) {
	tools := minister.Tools(notify)
	systemPrompt := m.buildSystemPrompt(minister)

	return NewSession(m.llm, m.config, m.repoInfo, tools, nil, notify, systemPrompt)
}

// buildSystemPrompt composes the system prompt by combining the minister's role
// with any other context needed.
func (m *MinisterBase) buildSystemPrompt(minister Minister) string {
	// For now, just return the minister's role as the system prompt.
	// In the future, this could render a template with Role as a variable.
	return minister.Role()
}

// SetMinisterConfig updates the MinisterBase configuration for session creation.
// This allows ministers to be configured with an LLM client after initialization.
func (m *MinisterBase) SetMinisterConfig(llm llms.Model, config *SessionConfig, repoInfo repo.RepoInfo) {
	m.llm = llm
	m.config = config
	m.repoInfo = repoInfo
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

	req := storage.ZhengmingRequest{
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
	return requestID, nil
}

// IsZhengmingPending checks if there are pending clarification requests for an edict
func (m *MinisterBase) IsZhengmingPending(edictID string) (bool, error) {
	var count int64
	err := m.db.Model(&storage.ZhengmingRequest{}).
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
