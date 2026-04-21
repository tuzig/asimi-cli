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
	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

//go:embed context/realm.md
var realm string

//go:embed context/system_base.tmpl
var systemBase string
var ministerTmpl = template.Must(template.New("minister").Parse(systemBase))

// ZhengmingConn provides clarification request capabilities (behavioral interface)
type ZhengmingConn interface {
	RequestZhengming(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (requestID string, err error)
	IsZhengmingPending(key storage.EdictKey) (bool, error)
}

// EventEmitter emits events to the Ritual Guard's ledger (behavioral interface)
type EventEmitter interface {
	EmitEvent(key storage.EdictKey, eventType storage.ShogunateEvent, payload storage.JSON) error
}

// === UNIFIED TYPES ===

// Prompt carries the user's message to the Chancellor
type Prompt struct {
	Ctx          context.Context   // Per-prompt context for cancellation (CTRL-C)
	Message      string            // The Ruler's words
	EdictKey     storage.EdictKey  // Zero = new edict, set = continue existing
	ChannelID    string            // Channel target for stream routing
	ContextFiles map[string]string // Files loaded via @ references
}

// Task carries work from Chancellor to a Minister
type Task struct {
	Ctx        context.Context     // Per-task cancellation (e.g. CTRL-C)
	EdictKey   storage.EdictKey    // The edict this task belongs to
	Work       string              // Specific instructions for the minister (renamed from Task to avoid Task.Task)
	Scratchpad string              // Pre-formatted markdown added to the context
	Session    *Session            // Existing session for multi-turn (nil = create new)
	Done       chan<- Result       // For completion signal
	Notify     internal.NotifyFunc // Routing-aware notify override (nil = use minister's default)
	ChannelID  string              // Routing target for stream messages (set by caller)
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
	// PromptsChan returns the channel for prompts via SubmitPrompt
	PromptsChan() <-chan *Prompt
	// SubmitPrompt sends a prompt to the minister
	SubmitPrompt(p *Prompt)
	// RepoInfo returns the repository information
	RepoInfo() repo.RepoInfo
	// Model returns the minister's LLM client
	Model() LLMProvider
	// GetConfig returns the minister's LLM configuration
	GetConfig() internalconfig.LLMConfig
	// Run starts the minister's processing loop (blocks until context cancelled)
	Run(ctx context.Context)
	// Get the interactive sessions
	GetSession() *Session
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
	Summary  string
	EdictKey storage.EdictKey
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
	EdictKey   storage.EdictKey
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
type StreamDoneMsg struct{ ChannelID string }

// MinisterBase provides shared functionality for all ministers.
// Ministers embed this struct to gain database access and session creation capabilities.
type MinisterBase struct {
	db         *gorm.DB
	ministerID string
	client LLMProvider // LLM client for chat completions
	config     *SessionConfig
	repoInfo   repo.RepoInfo
	runner     runners.Runner
	logger     *slog.Logger
	notify     internal.NotifyFunc
	prompts    chan *Prompt
	tasks      chan *Task
	publish    func(key storage.EdictKey, eventType storage.ShogunateEvent, payload storage.JSON) uint // routes events through Shogunate when set

	zhengmingMu       sync.Mutex
	onZhengmingRaised func()

	pendingZhengming   map[string]chan ZhengmingAnswer
	pendingZhengmingMu sync.Mutex

	username string
	project  string
	session  *Session // Embedded session for interactive use cases
}

// NewMinisterBase creates a base for all ministers with shared dependencies.
func NewMinisterBase(db *gorm.DB, runner runners.Runner, logger *slog.Logger, username string, project string) *MinisterBase {
	if logger == nil {
		logger = slog.Default()
	}
	return &MinisterBase{
		db:               db,
		runner:           runner,
		logger:           logger,
		prompts:          make(chan *Prompt),
		tasks:            make(chan *Task, 10),
		username:         username,
		project:          project,
		pendingZhengming: make(map[string]chan ZhengmingAnswer),
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

// Tasks returns the channel for submitting Tasks.
func (m *MinisterBase) Tasks() chan<- *Task {
	return m.tasks
}

// RunLoop is the shared processing loop for ministers. It listens on both
// prompts (via SubmitPrompt) and tasks (from Chancellor).
// The minister parameter is the concrete Minister, used by the default prompt
// handler to create sessions. processPrompt may be nil — in that case RunLoop
// uses ProcessPrompt(minister, ...) as the default. processTask may also be nil.
func (m *MinisterBase) RunLoop(
	ctx context.Context,
	minister Minister,
	processPrompt func(context.Context, *Prompt),
	processTask func(context.Context, *Task),
) {
	if processPrompt == nil {
		processPrompt = func(ctx context.Context, p *Prompt) {
			m.ProcessPrompt(ctx, minister, p)
		}
	}
	m.logger.Info("minister started", "minister_id", m.ministerID)
	for {
		select {
		case <-ctx.Done():
			m.logger.Info("minister stopped", "minister_id", m.ministerID)
			return
		case prompt := <-m.PromptsChan():
			merged, mergedCancel := context.WithCancel(ctx)
			if prompt.Ctx != nil {
				context.AfterFunc(prompt.Ctx, func() { mergedCancel() })
			}
			processPrompt(merged, prompt)
			mergedCancel()
		case task := <-m.tasks:
			if processTask == nil {
				m.logger.Warn("received task but no handler", "minister_id", m.ministerID)
				continue
			}
			merged, mergedCancel := context.WithCancel(ctx)
			if task.Ctx != nil {
				context.AfterFunc(task.Ctx, func() { mergedCancel() })
			}
			processTask(merged, task)
			mergedCancel()
		}
	}
}

// ProcessPrompt is the shared prompt handler for all ministers.
// It creates a session if needed and streams the LLM response.
func (m *MinisterBase) ProcessPrompt(ctx context.Context, minister Minister, prompt *Prompt) {
	if m.client == nil {
		m.notify(StreamErrorMsg{ChannelID: m.ministerID, Err: fmt.Errorf("LLM not configured for %s", m.ministerID)})
		return
	}

	if m.session == nil {
		var err error
		m.session, err = CreateSession(minister, m.client, m.config, m.notify, m.ministerID)
		if err != nil {
			m.notify(StreamErrorMsg{ChannelID: m.ministerID, Err: fmt.Errorf("failed to create session: %w", err)})
			return
		}
		m.session.TabType = m.ministerID
		m.logger.Info("created interactive session", "minister_id", m.ministerID)
	}

	m.notify(StreamStartMsg{ChannelID: m.ministerID, EdictID: prompt.EdictKey.ID})

	_, err := m.session.AskWithStreaming(ctx, prompt.Message, prompt.ContextFiles)
	if err != nil && ctx.Err() == nil {
		m.notify(StreamErrorMsg{ChannelID: m.ministerID, Err: err})
		return
	}
	m.notify(StreamDoneMsg{ChannelID: m.ministerID})
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

// Username returns the minister's username.
func (m *MinisterBase) Username() string {
	return m.username
}

// Project returns the minister's project name.
func (m *MinisterBase) Project() string {
	return m.project
}

// Scratchpad returns dynamic per-minister context. Default is empty.
// Ministers can override this to provide context like available rituals, rules, etc.
func (m *MinisterBase) Scratchpad() string {
	return ""
}

// SetRunner updates the shell runner
func (m *MinisterBase) SetRunner(r runners.Runner) {
	m.runner = r
}

// RepoInfo returns the repository information
func (m *MinisterBase) RepoInfo() repo.RepoInfo {
	return m.repoInfo
}

// CreateSessionOpts holds optional parameters for CreateSession.
type CreateSessionOpts struct {
	EdictKey   storage.EdictKey
	ChannelID  string
	Scratchpad string // Pre-formatted markdown context from ritual
}

// WithChannelID wraps a notify function to auto-set Session's ChannelID on first invocation.
// This allows the Session's routing target to be synchronized with whoever is driving it
// (e.g., Chancellor invoking Forge should route to Chancellor's tab).
func WithChannelID(notify internal.NotifyFunc, session *Session, channelID string) internal.NotifyFunc {
	mu := sync.Once{}
	return func(msg any) {
		mu.Do(func() {
			if session != nil && session.ChannelID() == "" {
				session.SetChannelID(channelID)
			}
		})
		notify(msg)
	}
}

// CreateSession creates a session for a minister with composed system prompt.
func CreateSession(minister Minister, client LLMProvider, config *SessionConfig, notify internal.NotifyFunc, channelID string, keys ...storage.EdictKey) (*Session, error) {
	key := storage.EdictKey{}
	if len(keys) > 0 {
		key = keys[0]
	}
	systemPrompt := buildSystemPrompt(minister, config, key)
	return NewSession(client, config, minister.Tools(), nil, notify, systemPrompt, channelID)
}

// CreateSessionWithOpts creates a session with extended options including given context.
func CreateSessionWithOpts(minister Minister, client LLMProvider, config *SessionConfig, notify internal.NotifyFunc, opts CreateSessionOpts) (*Session, error) {
	systemPrompt := buildSystemPrompt(minister, config, opts.EdictKey, opts.Scratchpad)
	return NewSession(client, config, minister.Tools(), nil, notify, systemPrompt, opts.ChannelID)
}

// buildSystemPrompt composes the system prompt by rendering the shared template
// with the minister's Role, Scratchpad, and project context from AGENTS.md.
// If id is non-empty, it's prepended to the scratchpad.
// Optional args, when provided, is appended to the scratchpad.
func buildSystemPrompt(minister Minister, config *SessionConfig, key storage.EdictKey, args ...string) string {
	agentsFile := "AGENTS.md"
	if config != nil && config.AgentsFile != "" {
		agentsFile = config.AgentsFile
	}

	scratchpad := minister.Scratchpad()
	if key.ID != 0 {
		scratchpad = fmt.Sprintf("# Current Edict: %d\n\n%s", key.ID, scratchpad)
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
// This allows ministers to be configured with an LLM client after initialization.
func (m *MinisterBase) SetMinisterConfig(client LLMProvider, config *SessionConfig, repoInfo repo.RepoInfo) {
	m.client = client
	m.config = config
	m.repoInfo = repoInfo
}

// SetNotify sets the notification callback.
func (m *MinisterBase) SetNotify(notify internal.NotifyFunc) {
	m.notify = notify
}

// Model returns the minister's LLM client.
func (m *MinisterBase) Model() LLMProvider {
	return m.client
}

// GetConfig returns the minister's LLM configuration.
func (m *MinisterBase) GetConfig() internalconfig.LLMConfig {
	if m.config != nil {
		return m.config.LLM
	}
	return internalconfig.LLMConfig{}
}

// Session returns the minister's embedded session (may be nil).
func (m *MinisterBase) Session() *Session {
	return m.session
}

// SetSession sets the minister's embedded session.
func (m *MinisterBase) SetSession(s *Session) {
	m.session = s
}

// ResetSession clears the minister's embedded session.
func (m *MinisterBase) ResetSession() {
	m.session = nil
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
func (m *MinisterBase) RequestZhengming(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
	requestID := GenerateID("zhengming", fmt.Sprintf("%d", key.ID), m.ministerID, fmt.Sprintf("%v", questions), time.Now().String())

	req := storage.Zhengming{
		RequestID:  requestID,
		EdictID:    key.ID,
		Username:   key.Username,
		Project:    key.Project,
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

	// Notify UI of pending zhengming
	if m.notify != nil {
		m.notify(ZhengmingPendingMsg{
			RequestID:  requestID,
			EdictKey:   key,
			MinisterID: m.ministerID,
			Questions:  questions,
			Priority:   priority,
		})
	}

	// Emit zhengming_requested event
	m.EmitEvent(key, "zhengming_requested", storage.JSON{
		"request_id":  requestID,
		"minister_id": m.ministerID,
		"questions":   questions,
		"priority":    string(priority),
	})

	return requestID, nil
}

// WaitForZhengming blocks until the zhengming answer arrives or ctx is cancelled.
func (m *MinisterBase) WaitForZhengming(ctx context.Context, requestID string) (string, error) {
	m.pendingZhengmingMu.Lock()
	ch := make(chan ZhengmingAnswer, 1)
	m.pendingZhengming[requestID] = ch
	m.pendingZhengmingMu.Unlock()

	defer func() {
		m.pendingZhengmingMu.Lock()
		delete(m.pendingZhengming, requestID)
		m.pendingZhengmingMu.Unlock()
	}()

	select {
	case answer := <-ch:
		return answer.Answer, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// DeliverZhengmingAnswer delivers a zhengming answer to a waiting caller.
// Returns true if the answer was delivered.
func (m *MinisterBase) DeliverZhengmingAnswer(answer ZhengmingAnswer) bool {
	m.pendingZhengmingMu.Lock()
	ch, ok := m.pendingZhengming[answer.RequestID]
	m.pendingZhengmingMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- answer:
		return true
	default:
		return false
	}
}

// CancelZhengming cancels a pending zhengming request, unblocking WaitForZhengming with an error.
func (m *MinisterBase) CancelZhengming(requestID string) {
	m.pendingZhengmingMu.Lock()
	defer m.pendingZhengmingMu.Unlock()
	delete(m.pendingZhengming, requestID)
}

// IsZhengmingPending checks if there are pending clarification requests for an edict
func (m *MinisterBase) IsZhengmingPending(key storage.EdictKey) (bool, error) {
	var count int64
	err := m.db.Model(&storage.Zhengming{}).
		Where("edict_id = ? AND username = ? AND project = ? AND status = ?", key.ID, key.Username, key.Project, storage.ZhengmingPending).
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
func (m *MinisterBase) AppendToIntent(key storage.EdictKey, clarification string) error {
	var edict storage.Edict
	if err := m.db.First(&edict, "id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).Error; err != nil {
		return fmt.Errorf("failed to get edict: %w", err)
	}

	newIntent := edict.Intent + "\n\n---\n**Clarification:**\n" + clarification

	result := m.db.Model(&storage.Edict{}).
		Where("id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).
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

	reqKey := storage.EdictKey{ID: req.EdictID, Username: req.Username, Project: req.Project}
	if req.EdictID != 0 {
		if err := m.AppendToIntent(reqKey, answer); err != nil {
			slog.Warn("failed to append clarification to edict", "edict_id", req.EdictID, "error", err)
		}
	}

	// Emit zhengming_answered event
	m.EmitEvent(reqKey, "zhengming_answered", storage.JSON{
		"request_id": requestID,
		"answer":     answer,
		"edict_id":   req.EdictID,
	})

	return nil
}

// EmitEvent records an event in the Tian ledger and delivers it via channel when available.
func (m *MinisterBase) EmitEvent(key storage.EdictKey, eventType storage.ShogunateEvent, payload storage.JSON) error {
	slog.Debug("Emitting event", "type", eventType, "edict", key.ID)
	if m.publish != nil {
		_ = m.publish(key, eventType, payload)
		return nil
	}
	// Fallback: DB-only (for tests/standalone)
	event := storage.TianEvent{
		EdictID:   key.ID,
		Username:  key.Username,
		Project:   key.Project,
		EventType: eventType,
		Payload:   payload,
	}
	if err := m.db.Create(&event).Error; err != nil {
		return fmt.Errorf("failed to emit event: %w", err)
	}
	return nil
}

// grantSeal records the minister's seal on an edict
func (m *MinisterBase) grantSeal(key storage.EdictKey, metadata storage.JSON) error {
	hasSeal, err := m.hasSeal(key)
	if err != nil {
		return fmt.Errorf("check existing seal: %w", err)
	}
	if hasSeal {
		m.logger.Debug("seal already granted", "edict_id", key.ID, "minister_id", m.ministerID)
		return nil
	}

	sealID := GenerateID("seal", fmt.Sprintf("%d", key.ID), m.ministerID)
	seal := storage.Seal{
		SealID:     sealID,
		EdictID:    key.ID,
		Username:   key.Username,
		Project:    key.Project,
		MinisterID: m.ministerID,
		SealedAt:   time.Now(),
		Metadata:   metadata,
	}

	if err := m.db.Create(&seal).Error; err != nil {
		return fmt.Errorf("failed to grant seal: %w", err)
	}

	m.EmitEvent(key, storage.EventSealGranted, storage.JSON{
		"minister_id": m.ministerID,
		"seal_id":     sealID,
	})

	m.logger.Info("seal granted", "edict_id", key.ID, "seal_id", sealID)
	return nil
}

// hasSeal checks if the minister has already sealed this edict
func (m *MinisterBase) hasSeal(key storage.EdictKey) (bool, error) {
	var count int64
	err := m.db.Model(&storage.Seal{}).
		Where("edict_id = ? AND username = ? AND project = ? AND minister_id = ?", key.ID, key.Username, key.Project, m.ministerID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check seal: %w", err)
	}
	return count > 0, nil
}

// GetEdict retrieves an edict by composite key
func (m *MinisterBase) GetEdict(key storage.EdictKey) (*storage.Edict, error) {
	var edict storage.Edict
	if err := m.db.First(&edict, "id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("edict not found: %d", key.ID)
		}
		return nil, fmt.Errorf("failed to get edict: %w", err)
	}
	return &edict, nil
}
func (m *MinisterBase) GetSession() *Session {
	return m.session
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
