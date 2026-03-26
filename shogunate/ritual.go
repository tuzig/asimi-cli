package shogunate

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	cucumberexpressions "github.com/cucumber/cucumber-expressions/go/v19"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

//go:embed dotagents/Justfile
var dotagentsJustfile string

//go:embed dotagents/asimi.conf
var dotagentsAsimiConf string

//go:embed dotagents/sandbox/Dockerfile
var dotagentsDockerfile string

//go:embed dotagents/sandbox/bashrc
var dotagentsBashrc string

// RitualStepMsg notifies the UI of ritual step progress
type RitualStepMsg struct {
	TabID       string
	RitualName  string
	ExecutionID string
	EdictID     uint
	StepName    string
	StepIndex   int
	TotalSteps  int
	Status      string
	Message     string
}

// RitualState represents the current state of a ritual execution
type RitualState string

const (
	RitualStatePending   RitualState = "pending" // maybe replace with the number of seconds till activation
	RitualStateRunning   RitualState = "running"
	RitualStateStopped   RitualState = "stopped"
	RitualStateCompleted RitualState = "completed"
	RitualStateFailed    RitualState = "failed"
	RitualStateAborted   RitualState = "aborted"
)

// OnFailureAction defines what happens when a step fails
type OnFailureAction string

const (
	OnFailureRetry     OnFailureAction = "retry"
	OnFailureZhengming OnFailureAction = "zhengming"
	OnFailureGoto      OnFailureAction = "goto"
	OnFailureAbort     OnFailureAction = "abort"
)

// ErrZhengmingPending is returned by runBuiltinThen when a zhengming request
// has been created and the ritual must block until the ruler answers.
var ErrZhengmingPending = errors.New("zhengming pending")

// ZhengmingAnswer carries the ruler's response to a pending zhengming request.
type ZhengmingAnswer struct {
	RequestID string
	Answer    string
	EdictID   uint
}

// getRulersError returns a user-friendly error message.
// Context cancellation errors are translated to "cancelled by ruler".
func getRulersError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled by ruler"
	}
	return err.Error()
}

// RitualDef represents a YAML-defined ritual
type RitualDef struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Triggers    []RitualTrigger     `yaml:"triggers,omitempty"`
	Inputs      map[string]InputDef `yaml:"inputs,omitempty"`
	Background  []string            `yaml:"background,omitempty"`  // Shared given steps that run before every execution
	OnFailure   string              `yaml:"on_failure,omitempty"`  // Default on_failure for all steps
	MaxRetries  int                 `yaml:"max_retries,omitempty"` // Default max_retries for all steps
	Steps       []RitualStep        `yaml:"steps"`
	Then        []string            `yaml:"then,omitempty"` // Ritual-level then steps (run after all steps succeed)
}

// RitualTrigger defines when a ritual can be invoked
type RitualTrigger struct {
	Event  string `yaml:"event,omitempty"`  // Event type that triggers this ritual
	Manual bool   `yaml:"manual,omitempty"` // Can be manually invoked
}

// InputDef defines an input parameter for a ritual
type InputDef struct {
	Type        string `yaml:"type"` // string, int, bool
	Required    bool   `yaml:"required,omitempty"`
	Default     string `yaml:"default,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// RitualStep defines a single step in a ritual (Given → Act → Then)
type RitualStep struct {
	Name            string            `yaml:"name"`
	Minister        string            `yaml:"minister,omitempty"`          // Minister to dispatch to
	Given           []string          `yaml:"given,omitempty"`             // Given steps: "!" prefix = bash, else matched via step registry
	Act             string            `yaml:"act,omitempty"`               // The action: task text, command, or prompt
	Then            []string          `yaml:"then,omitempty"`              // Then steps: "!" prefix = bash, else matched via step registry
	Task            string            `yaml:"task,omitempty"`              // Alias for Act (backward compat)
	DependsOn       []string          `yaml:"depends_on,omitempty"`        // Steps that must complete first
	OnFailure       string            `yaml:"on_failure,omitempty"`        // retry, zhengming, goto, abort
	OnFailureTarget string            `yaml:"on_failure_target,omitempty"` // Target step for goto
	MaxRetries      int               `yaml:"max_retries,omitempty"`       // Override default retries
	Scope           string            `yaml:"scope,omitempty"`             // Execution scope (e.g., "edict", "global")
	Model           string            `yaml:"model,omitempty"`             // LLM model override for this step
	Temperature     float64           `yaml:"temperature,omitempty"`       // LLM temperature override
	Env             map[string]string `yaml:"env,omitempty"`               // Environment variables for this step
	Fork            *ForkDef          `yaml:"fork,omitempty"`              // Fork/join parallel execution
	Work            []RitualStep      `yaml:"work,omitempty"`              // Steps to execute for each fork item
}

// ForkDef defines fork/join parallel execution
type ForkDef struct {
	Over      string `yaml:"over"`       // Output key to iterate over
	BatchSize int    `yaml:"batch_size"` // Number of concurrent workers: 0=default parallel, 1=sequential, >1=parallel
	Limit     string `yaml:"limit"`      // Max items to process (0 = unlimited)
}

// StepDefKind distinguishes bash commands from builtin handlers
type StepDefKind int

const (
	StepDefBash    StepDefKind = iota // "!" prefix — inline bash command
	StepDefBuiltin                    // matched via cucumber-expressions registry
)

// StepDefEntry is a resolved step definition ready for execution
type StepDefEntry struct {
	Kind    StepDefKind
	Key     string // output key for given context (e.g. "edict", "manifests", or sanitized command)
	Command string // for bash: the raw command; for builtin: the handler name
}

// StepDef maps a cucumber expression pattern to a handler
type StepDef struct {
	Pattern    string // cucumber expression pattern
	HandlerKey string // key used to dispatch to runBuiltinGiven/runBuiltinThen
	OutputKey  string // key stored in given_context
	expression cucumberexpressions.Expression
}

// StepDefRegistry holds registered step definitions matched via cucumber-expressions
type StepDefRegistry struct {
	paramRegistry *cucumberexpressions.ParameterTypeRegistry
	defs          []StepDef
}

// NewStepDefRegistry creates a registry with built-in given step definitions
func NewStepDefRegistry() *StepDefRegistry {
	r := &StepDefRegistry{
		paramRegistry: cucumberexpressions.NewParameterTypeRegistry(),
	}
	// Register built-in given steps
	builtins := []struct {
		pattern    string
		handlerKey string
		outputKey  string
	}{
		{"the edict details", "get_edict", "edict"},
		{"the court status", "get_court_status", "court_status"},
		{"the manifests", "get_manifests", "manifests"},
		{"the verdicts", "get_verdicts", "verdicts"},
		{"the precedents", "get_precedents", "precedents"},
		{"the earth status", "get_earth_status", "earth_status"},
		{"the borderlands", "get_borderlands", "borderlands"},
		{"the edict is sealed", "seal_edict", "sealed"},
		{"the edict is blocked", "block_edict", "blocked"},
		{"the edict is unblocked", "unblock_edict", "unblocked"},
		{"the ruler approves", "request_zhengming", "approved"},
		{"a clear working directory", "check_clean_working_directory", "working_directory_clean"},
		{"the infrastructure templates", "get_infrastructure_templates", "infrastructure_templates"},
		{"build the sandbox", "build_sandbox", "sandbox_build"},
		{"the sandbox is ready", "verify_sandbox_ready", "sandbox_ready"},
		{"the project metadata", "get_project_metadata", "project_metadata"},
		{"the edict awaits ruler's seal", "await_ruler_seal", "awaiting_seal"},
		{"the infrastructure is staged", "stage_infrastructure", "infrastructure_staged"},
		{"record the judge's seal", "record_judge_seal", ""},
		{"record the sage's seal", "record_sage_seal", ""},
	}
	for _, b := range builtins {
		_ = r.Register(b.pattern, b.handlerKey, b.outputKey) // builtin patterns are known-good
	}
	return r
}

// Register adds a step definition to the registry
func (r *StepDefRegistry) Register(pattern, handlerKey, outputKey string) error {
	expr, err := cucumberexpressions.NewCucumberExpression(pattern, r.paramRegistry)
	if err != nil {
		return fmt.Errorf("invalid cucumber expression %q: %w", pattern, err)
	}
	r.defs = append(r.defs, StepDef{
		Pattern:    pattern,
		HandlerKey: handlerKey,
		OutputKey:  outputKey,
		expression: expr,
	})
	return nil
}

// Match finds the first step definition that matches the given text
func (r *StepDefRegistry) Match(text string) (*StepDef, error) {
	for i := range r.defs {
		args, err := r.defs[i].expression.Match(text)
		if err != nil {
			return nil, err
		}
		if args != nil {
			return &r.defs[i], nil
		}
	}
	return nil, nil
}

// LoadStepDefsFromFile loads user-defined step definitions from a YAML file
func (r *StepDefRegistry) LoadStepDefsFromFile(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading step definitions: %w", err)
	}
	var defs []struct {
		Pattern string `yaml:"pattern"`
		Command string `yaml:"command"`
		Key     string `yaml:"key"`
	}
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return fmt.Errorf("parsing step definitions: %w", err)
	}
	for _, d := range defs {
		if err := r.Register(d.Pattern, d.Command, d.Key); err != nil {
			return err
		}
	}
	return nil
}

// RitualRegistry stores loaded rituals
type RitualRegistry struct {
	mu      sync.RWMutex
	rituals map[string]*RitualDef
	byEvent map[string][]*RitualDef
}

// NewRitualRegistry creates a new ritual registry
func NewRitualRegistry() *RitualRegistry {
	return &RitualRegistry{
		rituals: make(map[string]*RitualDef),
		byEvent: make(map[string][]*RitualDef),
	}
}

// Register adds a ritual to the registry
func (r *RitualRegistry) Register(ritual *RitualDef) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ritual.Name == "" {
		return fmt.Errorf("ritual name is required")
	}

	r.rituals[ritual.Name] = ritual

	// Index by event triggers
	for _, trigger := range ritual.Triggers {
		if trigger.Event != "" {
			r.byEvent[trigger.Event] = append(r.byEvent[trigger.Event], ritual)
		}
	}

	return nil
}

// Get retrieves a ritual by name
func (r *RitualRegistry) Get(name string) *RitualDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rituals[name]
}

// GetByEvent retrieves all rituals triggered by an event
func (r *RitualRegistry) GetByEvent(event string) []*RitualDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byEvent[event]
}

// Clear removes all registered rituals.
func (r *RitualRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rituals = make(map[string]*RitualDef)
	r.byEvent = make(map[string][]*RitualDef)
}

// List returns all registered ritual names
func (r *RitualRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.rituals))
	for name := range r.rituals {
		names = append(names, name)
	}
	return names
}

// ParseRitual parses YAML content into a RitualDef
func ParseRitual(content []byte) (*RitualDef, error) {
	var ritual RitualDef
	if err := yaml.Unmarshal(content, &ritual); err != nil {
		return nil, fmt.Errorf("failed to parse ritual YAML: %w", err)
	}
	return &ritual, nil
}

// ValidateRitual validates a ritual definition
func ValidateRitual(def *RitualDef) error {
	if def.Name == "" {
		return fmt.Errorf("ritual name is required")
	}

	if len(def.Steps) == 0 {
		return fmt.Errorf("ritual %q has no steps", def.Name)
	}

	// Build step name set for validation
	stepNames := make(map[string]bool)
	for _, step := range def.Steps {
		if step.Name == "" {
			return fmt.Errorf("ritual %q: step name is required", def.Name)
		}
		if stepNames[step.Name] {
			return fmt.Errorf("ritual %q: duplicate step name %q", def.Name, step.Name)
		}
		stepNames[step.Name] = true
	}

	// Validate step references and dependencies
	for _, step := range def.Steps {
		// Validate depends_on references
		for _, dep := range step.DependsOn {
			if !stepNames[dep] {
				return fmt.Errorf("ritual %q: step %q depends on unknown step %q", def.Name, step.Name, dep)
			}
		}

		// Validate on_failure_target
		onFailure := step.OnFailure
		if onFailure == "" {
			onFailure = def.OnFailure
		}
		if onFailure == "goto" && step.OnFailureTarget != "" {
			if !stepNames[step.OnFailureTarget] {
				return fmt.Errorf("ritual %q: step %q on_failure_target references unknown step %q", def.Name, step.Name, step.OnFailureTarget)
			}
		}

		// Validate fork steps vs regular steps
		if step.Fork != nil {
			// Fork step: requires work steps, no minister/act
			if len(step.Work) == 0 {
				return fmt.Errorf("ritual %q: fork step %q requires work steps", def.Name, step.Name)
			}
			if step.Fork.Over == "" {
				return fmt.Errorf("ritual %q: fork step %q requires 'over' field", def.Name, step.Name)
			}
			// Validate work steps recursively
			for i, workStep := range step.Work {
				if workStep.Name == "" {
					return fmt.Errorf("ritual %q: fork step %q work[%d] requires name", def.Name, step.Name, i)
				}
				if workStep.Minister == "" {
					return fmt.Errorf("ritual %q: fork step %q work[%d] requires minister", def.Name, step.Name, i)
				}
				if workStep.Act == "" && workStep.Task == "" {
					return fmt.Errorf("ritual %q: fork step %q work[%d] requires act or task", def.Name, step.Name, i)
				}
			}
		} else {
			// Regular step: requires minister and act
			if step.Minister == "" {
				return fmt.Errorf("ritual %q: step %q requires minister", def.Name, step.Name)
			}
			if step.Act == "" && step.Task == "" {
				return fmt.Errorf("ritual %q: step %q requires act or task", def.Name, step.Name)
			}
		}
	}

	// Check for circular dependencies
	if err := checkCircularDeps(def); err != nil {
		return err
	}

	return nil
}

// checkCircularDeps detects circular dependencies in step depends_on
func checkCircularDeps(def *RitualDef) error {
	// Build dependency graph
	deps := make(map[string][]string)
	for _, step := range def.Steps {
		deps[step.Name] = step.DependsOn
	}

	// DFS to detect cycles
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var hasCycle func(name string) bool
	hasCycle = func(name string) bool {
		visited[name] = true
		recStack[name] = true

		for _, dep := range deps[name] {
			if !visited[dep] {
				if hasCycle(dep) {
					return true
				}
			} else if recStack[dep] {
				return true
			}
		}

		recStack[name] = false
		return false
	}

	for _, step := range def.Steps {
		if !visited[step.Name] {
			if hasCycle(step.Name) {
				return fmt.Errorf("ritual %q: circular dependency detected involving step %q", def.Name, step.Name)
			}
		}
	}

	return nil
}

// LoadRitualsFromDir loads all .yaml/.yml files from a directory
func LoadRitualsFromDir(dir string) ([]*RitualDef, error) {
	var rituals []*RitualDef

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return rituals, nil // No rituals directory is OK
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read rituals directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read ritual file %s: %w", path, err)
		}

		ritual, err := ParseRitual(content)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ritual file %s: %w", path, err)
		}

		if err := ValidateRitual(ritual); err != nil {
			return nil, fmt.Errorf("invalid ritual file %s: %w", path, err)
		}

		rituals = append(rituals, ritual)
	}

	return rituals, nil
}

// RitualRunner executes rituals
type RitualRunner struct {
	registry        *RitualRegistry
	stepDefs        *StepDefRegistry
	getMinister     func(id string) Minister
	publishEvent    func(edictID uint, eventType storage.ShogunateEvent, payload storage.JSON) uint
	onRunnerUpgrade func(runners.Runner) // propagates runner changes back to shogunate
	db              *gorm.DB
	runner          runners.Runner
	logger          *slog.Logger
	maxRetries      int

	pendingZhengming   map[string]chan ZhengmingAnswer
	pendingZhengmingMu sync.Mutex
}

// NewRitualRunner creates a new ritual runner
func NewRitualRunner(
	registry *RitualRegistry,
	getMinister func(id string) Minister,
	publishEvent func(edictID uint, eventType storage.ShogunateEvent, payload storage.JSON) uint,
	db *gorm.DB,
	runner runners.Runner,
	logger *slog.Logger,
) *RitualRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &RitualRunner{
		registry:         registry,
		stepDefs:         NewStepDefRegistry(),
		getMinister:      getMinister,
		publishEvent:     publishEvent,
		db:               db,
		runner:           runner,
		logger:           logger,
		maxRetries:       3,
		pendingZhengming: make(map[string]chan ZhengmingAnswer),
	}
}

// registerPendingZhengming creates a channel for a zhengming request and returns it.
func (r *RitualRunner) registerPendingZhengming(requestID string) chan ZhengmingAnswer {
	r.pendingZhengmingMu.Lock()
	defer r.pendingZhengmingMu.Unlock()
	ch := make(chan ZhengmingAnswer, 1)
	r.pendingZhengming[requestID] = ch
	return ch
}

// removePendingZhengming removes a pending zhengming channel.
func (r *RitualRunner) removePendingZhengming(requestID string) {
	r.pendingZhengmingMu.Lock()
	defer r.pendingZhengmingMu.Unlock()
	delete(r.pendingZhengming, requestID)
}

// DeliverZhengmingAnswer delivers a zhengming answer to a waiting ritual.
// Returns true if the answer was delivered to a pending ritual request.
func (r *RitualRunner) DeliverZhengmingAnswer(answer ZhengmingAnswer) bool {
	r.pendingZhengmingMu.Lock()
	ch, ok := r.pendingZhengming[answer.RequestID]
	r.pendingZhengmingMu.Unlock()
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

// HasPendingZhengming returns true if the given request ID has a pending ritual zhengming.
func (r *RitualRunner) HasPendingZhengming(requestID string) bool {
	r.pendingZhengmingMu.Lock()
	defer r.pendingZhengmingMu.Unlock()
	_, ok := r.pendingZhengming[requestID]
	return ok
}

// waitForZhengming registers a channel, sets ritual state to stopped,
// blocks until the answer arrives or ctx is cancelled, then restores state.
func (r *RitualRunner) waitForZhengming(ctx context.Context, exec *RitualExecution, requestID string) (ZhengmingAnswer, error) {
	ch := r.registerPendingZhengming(requestID)
	defer r.removePendingZhengming(requestID)

	exec.State = RitualStateStopped
	r.saveExecution(exec)
	r.logger.Info("ritual paused waiting for zhengming",
		"ritual", exec.RitualName,
		"step", exec.CurrentStep,
		"execution_id", exec.ID,
		"request_id", requestID)

	select {
	case answer := <-ch:
		exec.State = RitualStateRunning
		r.saveExecution(exec)
		r.logger.Info("ritual resumed after zhengming",
			"ritual", exec.RitualName,
			"execution_id", exec.ID,
			"request_id", requestID,
			"answer", answer.Answer)
		return answer, nil
	case <-ctx.Done():
		exec.State = RitualStateRunning
		r.saveExecution(exec)
		return ZhengmingAnswer{}, ctx.Err()
	}
}

// RitualExecution tracks a running ritual instance
type RitualExecution struct {
	ID          string            `gorm:"primaryKey;column:id"`
	RitualName  string            `gorm:"column:ritual_name"`
	EdictID     uint              `gorm:"column:edict_id;index"`
	SessionID   string            `gorm:"column:session_id"`
	CurrentStep int               `gorm:"column:current_step"`
	State       RitualState       `gorm:"column:state"`
	Data        storage.JSON      `gorm:"column:data;type:json"`
	CreatedAt   time.Time         `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time         `gorm:"column:updated_at;autoUpdateTime"`

	// Runtime (not persisted)
	def        *RitualDef
	stepStates []RitualStepState
	notify     internal.NotifyFunc

	// Recovery tracking (not persisted)
	PreviousExecutionID string `gorm:"-"` // ID of the aborted execution being recovered
	RecoveryMode        bool   `gorm:"-"` // True if this execution is a recovery
}

// TableName returns the table name for RitualExecution
func (RitualExecution) TableName() string {
	return "ritual_executions"
}

// RitualStepState tracks the state of a step within an execution
type RitualStepState struct {
	ID          uint   `gorm:"primaryKey;autoIncrement"`
	ExecutionID string `gorm:"column:execution_id;index"`
	StepIndex   int    `gorm:"column:step_index"`
	Name        string `gorm:"column:name"`
	SessionID   string `gorm:"column:session_id"`
	RetryCount  int    `gorm:"column:retry_count"`
	Message     string `gorm:"column:message"`

	// Runtime only (not persisted)
	Session *Session `gorm:"-"` // LLM session for multi-turn
	Output  string   `gorm:"-"` // Step output (preserved on failure)
}

// TableName returns the table name for RitualStepState
func (RitualStepState) TableName() string {
	return "ritual_step_states"
}

// Start begins execution of a ritual
func (r *RitualRunner) Start(ctx context.Context, ritualName string, edictID uint, inputs map[string]string, notify internal.NotifyFunc) (*RitualExecution, error) {
	def := r.registry.Get(ritualName)
	// Validate required inputs
	if def == nil {
		return nil, fmt.Errorf("failed to get ritual %s", ritualName)
	}
	for name, inputDef := range def.Inputs {
		if _, ok := inputs[name]; !ok {
			if inputDef.Default != "" {
				inputs[name] = inputDef.Default
			} else if inputDef.Required {
				return nil, fmt.Errorf("required input %q not provided", name)
			}
		}
	}

	// Check for existing aborted/paused ritual execution to recover from
	var previousExec *RitualExecution
	var recoveryData storage.JSON
	var recoveryFirstIncompleteStep int = -1
	if err := r.db.Where("edict_id = ? AND ritual_name = ? AND state IN (?, ?)", edictID, ritualName, RitualStateAborted, RitualStateStopped).
		Order("updated_at DESC").
		First(&previousExec).Error; err == nil {
		// Found aborted execution - attempt recovery
		r.logger.Info("found aborted ritual execution for recovery",
			"ritual", ritualName,
			"edict_id", edictID,
			"previous_execution_id", previousExec.ID,
			"state", previousExec.State)

		// Load step states to determine which steps completed
		var stepStates []RitualStepState
		if err := r.db.Where("execution_id = ?", previousExec.ID).Order("step_index").Find(&stepStates).Error; err == nil {
			// Find first incomplete step (message is empty, has error, or step was not reached)
			firstIncompleteStep := len(def.Steps) // Default to end if all complete
			for i := range def.Steps {
				if i < len(stepStates) {
					ss := stepStates[i]
					// Step is incomplete if:
					// - No message (never executed)
					// - Has retries (failed and retried)
					// - Message indicates error/cancellation (context canceled, timeout, etc.)
					isIncomplete := ss.Message == "" || ss.RetryCount > 0
					if !isIncomplete && ss.Message != "" {
						// Check for error patterns that indicate incomplete execution
						if strings.Contains(ss.Message, "context canceled") ||
							strings.Contains(ss.Message, "timeout") ||
							strings.Contains(ss.Message, "aborted") {
							isIncomplete = true
						}
					}
					if isIncomplete {
						firstIncompleteStep = i
						break
					}
				} else {
					firstIncompleteStep = i
					break
				}
			}

			// Only recover if there are incomplete steps
			if firstIncompleteStep > 0 && firstIncompleteStep < len(def.Steps) {
				r.logger.Info("resuming ritual from incomplete step",
					"ritual", ritualName,
					"from_step", firstIncompleteStep,
					"previous_execution_id", previousExec.ID)
				recoveryData = previousExec.Data
				recoveryFirstIncompleteStep = firstIncompleteStep
			}
		}
	}

	// Create execution record
	exec := &RitualExecution{
		ID:          GenerateID("ritual", ritualName, fmt.Sprint(edictID), time.Now().String()),
		RitualName:  ritualName,
		EdictID:     edictID,
		CurrentStep: 0,
		State:       RitualStatePending,
		Data:        storage.JSON{"inputs": inputs},
		def:         def,
		notify:      notify,
	}

	// If recovering from aborted execution, request zhengming confirmation (if getMinister is available)
	if previousExec != nil && recoveryData != nil && recoveryFirstIncompleteStep >= 0 {
		if r.getMinister != nil {
			// Request zhengming for recovery confirmation
			minister := r.getMinister("chancellor")
			if minister != nil {
				type zhengmingGate interface {
					RequestZhengming(uint, storage.ZhengmingQuestions, storage.ZhengmingPriority) (string, error)
				}
				gate, ok := minister.(zhengmingGate)
				if ok {
					questions := storage.ZhengmingQuestions{{
						Text:    fmt.Sprintf("Ritual '%s' was previously aborted at step %d/%d. Recover from step %d (preserving %d completed steps)?", ritualName, recoveryFirstIncompleteStep, len(def.Steps), recoveryFirstIncompleteStep, recoveryFirstIncompleteStep),
						Options: []string{"Recover from step " + strconv.Itoa(recoveryFirstIncompleteStep), "Start fresh from step 0"},
					}}
					requestID, err := gate.RequestZhengming(edictID, questions, storage.PriorityUrgent)
					if err == nil {
						// Notify UI of zhengming request
						if exec.notify != nil {
							exec.notify(ZhengmingPendingMsg{
								RequestID:  requestID,
								EdictID:    edictID,
								MinisterID: "chancellor",
								Questions:  questions,
								Priority:   storage.PriorityUrgent,
							})
						}
						// Wait for zhengming answer
						answer, waitErr := r.waitForZhengming(ctx, exec, requestID)
						if waitErr != nil {
							r.logger.Warn("recovery zhengming wait failed, starting fresh", "error", waitErr)
							// Clear recovery data on wait failure
							previousExec = nil
							recoveryData = nil
							recoveryFirstIncompleteStep = -1
						} else if answer.Answer == "Start fresh from step 0" {
							r.logger.Info("user declined recovery, starting fresh",
								"ritual", ritualName,
								"previous_execution_id", previousExec.ID)
							// Mark the zhengming-saved execution as completed (cleanup)
							exec.State = RitualStateCompleted
							r.saveExecution(exec)
							// Generate a fresh execution ID and reset state
							exec.ID = GenerateID("ritual", ritualName, fmt.Sprint(edictID), time.Now().String())
							exec.State = RitualStatePending
							// Clear recovery data to start fresh
							previousExec = nil
							recoveryData = nil
							recoveryFirstIncompleteStep = -1
						} else {
							r.logger.Info("user approved recovery",
								"ritual", ritualName,
								"from_step", recoveryFirstIncompleteStep,
								"previous_execution_id", previousExec.ID)
						}
					} else {
						r.logger.Warn("recovery zhengming request failed, proceeding with recovery", "error", err)
					}
				}
			}
		}
		// If getMinister is nil or zhengming was not requested/answered with "Start fresh",
		// proceed with recovery initialization
		if previousExec != nil && recoveryData != nil && recoveryFirstIncompleteStep >= 0 {
			exec.CurrentStep = recoveryFirstIncompleteStep
			exec.Data = recoveryData
			exec.PreviousExecutionID = previousExec.ID
			exec.RecoveryMode = true
			r.logger.Info("ritual recovery initialized",
				"ritual", ritualName,
				"execution_id", exec.ID,
				"resuming_from_step", exec.CurrentStep,
				"previous_execution_id", previousExec.ID)
		}
	}

	// Initialize step states
	exec.stepStates = make([]RitualStepState, len(def.Steps))
	for i, step := range def.Steps {
		exec.stepStates[i] = RitualStepState{
			ExecutionID: exec.ID,
			StepIndex:   i,
			Name:        step.Name,
		}
	}

	// Save to database
	if err := r.db.Save(exec).Error; err != nil {
		return nil, fmt.Errorf("failed to create ritual execution: %w", err)
	}

	for i := range exec.stepStates {
		if err := r.db.Save(&exec.stepStates[i]).Error; err != nil {
			return nil, fmt.Errorf("failed to create step state: %w", err)
		}
	}

	if exec.RecoveryMode {
		r.logger.Info("ritual started (recovery mode)",
			"ritual", ritualName,
			"execution_id", exec.ID,
			"edict_id", edictID,
			"from_step", exec.CurrentStep)
	} else {
		r.logger.Info("ritual started",
			"ritual", ritualName,
			"execution_id", exec.ID,
			"edict_id", edictID)
	}

	steps := 0
	if exec.def != nil {
		steps = len(exec.def.Steps)
	}

	if exec.notify != nil {
		exec.notify(RitualStepMsg{
			TabID:       "chancellor",
			RitualName:  exec.RitualName,
			ExecutionID: exec.ID,
			EdictID:     exec.EdictID,
			TotalSteps:  steps,
			Status:      "started",
		})
	} else {
		r.logger.Warn("Can't notify a ritual has started", "name", exec.RitualName)
	}
	return exec, nil
}

// Run executes a ritual to completion
func (r *RitualRunner) Run(ctx context.Context, exec *RitualExecution) error {
	if exec.def == nil {
		exec.def = r.registry.Get(exec.RitualName)
		if exec.def == nil {
			return fmt.Errorf("ritual definition not found: %s", exec.RitualName)
		}
	}

	exec.State = RitualStateRunning
	r.saveExecution(exec)

	// Emit ritual_started Tian event
	r.emitEvent(exec.EdictID, storage.EventRitualStarted, storage.JSON{
		"ritual":       exec.RitualName,
		"execution_id": exec.ID,
	})

	// === BACKGROUND ===
	for _, raw := range exec.def.Background {
		entry, err := r.resolveStepDef(raw)
		if err != nil {
			exec.State = RitualStateFailed
			r.saveExecution(exec)
			return fmt.Errorf("background given %q failed: %w", raw, err)
		}
		if exec.notify != nil {
			exec.notify(RitualStepMsg{
				TabID:       "chancellor",
				RitualName:  exec.RitualName,
				ExecutionID: exec.ID,
				EdictID:     exec.EdictID,
				StepName:    entry.Key,
				Status:      "cmd_running",
				Message:     raw,
			})
		}
		result, err := r.runGivenStep(ctx, exec, entry)
		if err != nil {
			exec.State = RitualStateFailed
			r.saveExecution(exec)
			return fmt.Errorf("background given %q failed: %w", raw, err)
		}
		if exec.notify != nil {
			exec.notify(RitualStepMsg{
				TabID:       "chancellor",
				RitualName:  exec.RitualName,
				ExecutionID: exec.ID,
				EdictID:     exec.EdictID,
				StepName:    entry.Key,
				Status:      "cmd_done",
				Message:     raw,
			})
		}
		storeGivenResult(exec, entry.Key, result)
	}

	for exec.CurrentStep < len(exec.def.Steps) {
		select {
		case <-ctx.Done():
			exec.State = RitualStateAborted
			r.saveExecution(exec)
			if exec.notify != nil {
				exec.notify(RitualStepMsg{
					TabID:       "chancellor",
					RitualName:  exec.RitualName,
					ExecutionID: exec.ID,
					EdictID:     exec.EdictID,
					Status:      "ritual_failed",
					Message:     "aborted by user",
				})
			}
			return ctx.Err()
		default:
		}

		step := exec.def.Steps[exec.CurrentStep]
		result, err := r.executeStep(ctx, exec, step)
		if err != nil {
			exec.stepStates[exec.CurrentStep].Message = err.Error()
			exec.stepStates[exec.CurrentStep].Output = result

			// Context cancelled (user interrupt) — abort without cascading events
			if ctx.Err() != nil {
				if exec.notify != nil {
					exec.notify(RitualStepMsg{
						TabID:       "chancellor",
						RitualName:  exec.RitualName,
						ExecutionID: exec.ID,
						EdictID:     exec.EdictID,
						StepName:    step.Name,
						StepIndex:   exec.CurrentStep,
						TotalSteps:  len(exec.def.Steps),
						Status:      "aborted",
						Message:     "aborted by user",
					})
				}
				exec.State = RitualStateAborted
				r.saveExecution(exec)
				return err
			}

			// Notify: step failed
			if exec.notify != nil {
				exec.notify(RitualStepMsg{
					TabID:       "chancellor",
					RitualName:  exec.RitualName,
					ExecutionID: exec.ID,
					EdictID:     exec.EdictID,
					StepName:    step.Name,
					StepIndex:   exec.CurrentStep,
					TotalSteps:  len(exec.def.Steps),
					Status:      "failed",
					Message:     getRulersError(err),
				})
			}

			// Emit step_failed Tian event
			r.emitEvent(exec.EdictID, storage.EventStepFailed, storage.JSON{
				"ritual":       exec.RitualName,
				"execution_id": exec.ID,
				"step":         step.Name,
				"step_index":   exec.CurrentStep,
				"error":        err.Error(),
			})

			// Handle failure action
			if !r.handleFailure(ctx, exec, step, err) {
				exec.State = RitualStateFailed
				r.saveExecution(exec)
				// Notify UI so Indent is decremented
				if exec.notify != nil {
					exec.notify(RitualStepMsg{
						TabID:       "chancellor",
						RitualName:  exec.RitualName,
						ExecutionID: exec.ID,
						EdictID:     exec.EdictID,
						StepName:    step.Name,
						StepIndex:   exec.CurrentStep,
						TotalSteps:  len(exec.def.Steps),
						Status:      "ritual_failed",
						Message:     getRulersError(err),
					})
				}
				// Emit ritual_failed Tian event
				lastStepOutput := getLastStepOutput(exec)
				r.emitEvent(exec.EdictID, storage.EventRitualFailed, storage.JSON{
					"ritual":           exec.RitualName,
					"execution_id":     exec.ID,
					"step":             step.Name,
					"error":            err.Error(),
					"last_step_output": lastStepOutput,
				})
				return err
			}
			continue
		}

		// Notify: step completed
		if exec.notify != nil {
			exec.notify(RitualStepMsg{
				TabID:       "chancellor",
				RitualName:  exec.RitualName,
				ExecutionID: exec.ID,
				EdictID:     exec.EdictID,
				StepName:    step.Name,
				StepIndex:   exec.CurrentStep,
				TotalSteps:  len(exec.def.Steps),
				Status:      "completed",
				Message:     result,
			})
		}
		// Emit step_completed Tian event
		r.emitEvent(exec.EdictID, storage.EventStepCompleted, storage.JSON{
			"ritual":       exec.RitualName,
			"execution_id": exec.ID,
			"step":         step.Name,
			"step_index":   exec.CurrentStep,
		})

		// Update step state
		exec.stepStates[exec.CurrentStep].Message = result

		// Move to next step (considering dependencies)
		exec.CurrentStep++
		r.saveExecution(exec)
	}

	// === RITUAL-LEVEL THEN ===
	for _, raw := range exec.def.Then {
		entry, err := r.resolveStepDef(raw)
		if err != nil {
			r.logger.Warn("ritual-level then failed to resolve", "raw", raw, "error", err)
			continue
		}
		if err := r.runThenStep(ctx, exec, entry); errors.Is(err, ErrZhengmingPending) {
			requestID, ok := exec.Data["pending_zhengming"].(string)
			if !ok || requestID == "" {
				r.logger.Error("zhengming pending but no request_id in execution data")
				continue
			}
			answer, waitErr := r.waitForZhengming(ctx, exec, requestID)
			if waitErr != nil {
				r.logger.Warn("ritual-level then zhengming wait failed", "raw", raw, "error", waitErr)
				continue
			}
			if answer.Answer == "Reject" {
				r.logger.Warn("ritual-level then zhengming rejected", "raw", raw)
			}
		} else if err != nil {
			r.logger.Warn("ritual-level then step failed", "raw", raw, "error", err)
		}
	}

	exec.State = RitualStateCompleted
	r.saveExecution(exec)

	// Notify: ritual completed
	if exec.notify != nil {
		exec.notify(RitualStepMsg{
			TabID:       "chancellor",
			RitualName:  exec.RitualName,
			ExecutionID: exec.ID,
			EdictID:     exec.EdictID,
			StepName:    "",
			StepIndex:   len(exec.def.Steps),
			TotalSteps:  len(exec.def.Steps),
			Status:      "ritual_completed",
		})
	}
	// Emit ritual_completed Tian event
	lastStepOutput := getLastStepOutput(exec)
	r.emitEvent(exec.EdictID, storage.EventRitualCompleted, storage.JSON{
		"ritual":           exec.RitualName,
		"execution_id":     exec.ID,
		"last_step_output": lastStepOutput,
	})

	r.logger.Info("ritual completed",
		"ritual", exec.RitualName,
		"execution_id", exec.ID,
		"edict_id", exec.EdictID)

	return nil
}

// resolveOnFailure returns the step's on_failure or the ritual-level default
func (step *RitualStep) resolveOnFailure(def *RitualDef) string {
	if step.OnFailure != "" {
		return step.OnFailure
	}
	return def.OnFailure
}

// resolveMaxRetries returns the step's max_retries or the ritual-level default
func (step *RitualStep) resolveMaxRetries(def *RitualDef) int {
	if step.MaxRetries > 0 {
		return step.MaxRetries
	}
	if def.MaxRetries > 0 {
		return def.MaxRetries
	}
	return 0
}

// executeStep runs a single ritual step using the Given → Act → Then model
func (r *RitualRunner) executeStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	// Check if this is a fork step
	if step.Fork != nil {
		return r.executeForkStep(ctx, exec, step)
	}

	// Check edict status before executing step - abort if sealed or cancelled
	if exec.EdictID != 0 {
		sealService := storage.NewSealService(r.db)
		status, err := sealService.GetEdictStatus(exec.EdictID)
		if err == nil {
			if status == storage.EdictSealed || status == storage.EdictCancelled {
				r.logger.Info("aborting ritual step due to edict state change",
					"ritual", exec.RitualName,
					"step", step.Name,
					"edict_id", exec.EdictID,
					"edict_status", status)
				return "", fmt.Errorf("ritual aborted: edict %d is %s", exec.EdictID, status)
			}
		}
	}

	r.saveExecution(exec)

	r.logger.Debug("executing ritual step",
		"ritual", exec.RitualName,
		"step", step.Name,
		"minister", step.Minister)

	// Notify: step started
	if exec.notify != nil {
		exec.notify(RitualStepMsg{
			TabID:       "chancellor",
			RitualName:  exec.RitualName,
			ExecutionID: exec.ID,
			EdictID:     exec.EdictID,
			StepName:    step.Name,
			StepIndex:   exec.CurrentStep,
			TotalSteps:  len(exec.def.Steps),
			Status:      "started",
		})
	}
	// Emit step_started Tian event
	r.emitEvent(exec.EdictID, storage.EventStepStarted, storage.JSON{
		"ritual":       exec.RitualName,
		"execution_id": exec.ID,
		"step":         step.Name,
		"step_index":   exec.CurrentStep,
	})

	// Check dependencies are complete (dependency step index must be less than current)
	for _, dep := range step.DependsOn {
		depIdx := r.stepIndex(exec.def, dep)
		if depIdx == -1 {
			return "", fmt.Errorf("dependency %q not found", dep)
		}
		if depIdx >= exec.CurrentStep {
			return "", fmt.Errorf("dependency %q not completed", dep)
		}
	}

	// === GIVEN ===
	if len(step.Given) > 0 {
		for _, raw := range step.Given {
			entry, err := r.resolveStepDef(raw)
			if err != nil {
				return "", fmt.Errorf("given %q failed: %w", raw, err)
			}
			result, err := r.runGivenStep(ctx, exec, entry)
			if err != nil {
				return "", fmt.Errorf("given %q failed: %w", raw, err)
			}
			storeGivenResult(exec, entry.Key, result)
		}
	}

	// === ACT ===
	actResult, err := r.executeMinisterStep(ctx, exec, step)
	if err != nil {
		return actResult, err
	}
	if exec.Data == nil {
		exec.Data = storage.JSON{}
	}
	exec.Data["act_result"] = actResult

	// === THEN ===
	for _, raw := range step.Then {
		entry, err := r.resolveStepDef(raw)
		if err != nil {
			return "", fmt.Errorf("then %q failed: %w", raw, err)
		}
		if exec.notify != nil {
			exec.notify(RitualStepMsg{
				TabID:       "chancellor",
				RitualName:  exec.RitualName,
				ExecutionID: exec.ID,
				EdictID:     exec.EdictID,
				StepName:    entry.Key,
				Status:      "cmd_running",
				Message:     raw,
			})
		}
		if err := r.runThenStep(ctx, exec, entry); errors.Is(err, ErrZhengmingPending) {
			// Block until the ruler answers the zhengming
			requestID, ok := exec.Data["pending_zhengming"].(string)
			if !ok || requestID == "" {
				r.logger.Error("zhengming pending but no request_id in execution data")
				continue
			}
			answer, waitErr := r.waitForZhengming(ctx, exec, requestID)
			if waitErr != nil {
				return "", fmt.Errorf("then %q zhengming wait failed: %w", raw, waitErr)
			}
			if answer.Answer == "Reject" {
				return "", fmt.Errorf("then %q zhengming rejected by ruler", raw)
			}
		} else if err != nil {
			return "", fmt.Errorf("then %q failed: %w", raw, err)
		}
		if exec.notify != nil {
			exec.notify(RitualStepMsg{
				TabID:       "chancellor",
				RitualName:  exec.RitualName,
				ExecutionID: exec.ID,
				EdictID:     exec.EdictID,
				StepName:    entry.Key,
				Status:      "cmd_done",
				Message:     raw,
			})
		}
	}

	return actResult, nil
}

// ForkResult holds the result of a single fork item execution
type ForkResult struct {
	Item   interface{}
	Output string
	Error  error
}

// executeForkStep executes a fork/join parallel step
func (r *RitualRunner) executeForkStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	r.saveExecution(exec)

	r.logger.Info("executing fork step",
		"ritual", exec.RitualName,
		"step", step.Name,
		"over", step.Fork.Over)

	// Notify: fork started
	if exec.notify != nil {
		exec.notify(RitualStepMsg{
			TabID:       "chancellor",
			RitualName:  exec.RitualName,
			ExecutionID: exec.ID,
			EdictID:     exec.EdictID,
			StepName:    step.Name,
			StepIndex:   exec.CurrentStep,
			TotalSteps:  len(exec.def.Steps),
			Status:      "started",
			Message:     fmt.Sprintf("fork over %s", step.Fork.Over),
		})
	}

	// Emit step_started Tian event
	r.emitEvent(exec.EdictID, storage.EventStepStarted, storage.JSON{
		"ritual":       exec.RitualName,
		"execution_id": exec.ID,
		"step":         step.Name,
		"step_index":   exec.CurrentStep,
		"fork":         true,
	})

	// Get the work units to iterate over
	workUnits, err := r.getForkWorkUnits(exec, step.Fork.Over)
	if err != nil {
		return "", fmt.Errorf("failed to get fork work units: %w", err)
	}

	// Apply limit if specified
	limit := 0
	if step.Fork.Limit != "" {
		limitStr := r.expandTemplate(step.Fork.Limit, exec)
		if limitStr != "" {
			limit, _ = strconv.Atoi(limitStr)
		}
	}
	if limit > 0 && len(workUnits) > limit {
		workUnits = workUnits[:limit]
	}

	r.logger.Info("fork work units",
		"total", len(workUnits),
		"limit", limit,
		"batch_size", step.Fork.BatchSize)

	// Determine execution mode based on BatchSize:
	// BatchSize==0 → default parallel (5 workers)
	// BatchSize==1 → sequential
	// BatchSize>1 → parallel with specified batch size
	var results []ForkResult
	if step.Fork.BatchSize == 1 {
		results = r.executeForkSequential(ctx, exec, step, workUnits)
	} else {
		batchSize := step.Fork.BatchSize
		if batchSize == 0 {
			batchSize = 5 // default batch size
		}
		results = r.executeForkParallel(ctx, exec, step, workUnits, batchSize)
	}

	// Store fork results in execution context
	forkOut := []ForkResult{}
	forkErr := []ForkResult{}
	for _, res := range results {
		if res.Error != nil {
			forkErr = append(forkErr, res)
		} else {
			forkOut = append(forkOut, res)
		}
	}

	if exec.Data == nil {
		exec.Data = storage.JSON{}
	}
	exec.Data["fork"] = map[string]interface{}{
		"out": forkOut,
		"err": forkErr,
	}

	// Build summary result
	summary := fmt.Sprintf("Fork completed: %d successful, %d failed", len(forkOut), len(forkErr))

	// Notify: fork completed
	if exec.notify != nil {
		exec.notify(RitualStepMsg{
			TabID:       "chancellor",
			RitualName:  exec.RitualName,
			ExecutionID: exec.ID,
			EdictID:     exec.EdictID,
			StepName:    step.Name,
			StepIndex:   exec.CurrentStep,
			TotalSteps:  len(exec.def.Steps),
			Status:      "completed",
			Message:     summary,
		})
	}

	// Emit step_completed Tian event
	r.emitEvent(exec.EdictID, storage.EventStepCompleted, storage.JSON{
		"ritual":       exec.RitualName,
		"execution_id": exec.ID,
		"step":         step.Name,
		"step_index":   exec.CurrentStep,
		"fork":         true,
		"success":      len(forkOut),
		"failed":       len(forkErr),
	})

	return summary, nil
}

// getForkWorkUnits retrieves the work units from execution context
func (r *RitualRunner) getForkWorkUnits(exec *RitualExecution, over string) ([]interface{}, error) {
	// Look in given_context first
	if exec.Data != nil {
		if given, ok := exec.Data["given_context"].(map[string]interface{}); ok {
			if val, ok := given[over]; ok {
				return r.toInterfaceSlice(val)
			}
		}
		// Also check step_results
		if stepResults, ok := exec.Data["step_results"].(map[string]interface{}); ok {
			if val, ok := stepResults[over]; ok {
				return r.toInterfaceSlice(val)
			}
		}
	}
	return nil, fmt.Errorf("work units key %q not found in context", over)
}

// toInterfaceSlice converts various slice types to []interface{}
func (r *RitualRunner) toInterfaceSlice(val interface{}) ([]interface{}, error) {
	switch v := val.(type) {
	case []interface{}:
		return v, nil
	case []map[string]interface{}:
		result := make([]interface{}, len(v))
		for i, m := range v {
			result[i] = m
		}
		return result, nil
	default:
		// Try to convert via JSON marshaling
		data, err := json.Marshal(val)
		if err != nil {
			return nil, fmt.Errorf("cannot convert work units: %w", err)
		}
		var result []interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("cannot unmarshal work units: %w", err)
		}
		return result, nil
	}
}

// executeForkSequential executes work units one at a time
func (r *RitualRunner) executeForkSequential(ctx context.Context, exec *RitualExecution, step RitualStep, workUnits []interface{}) []ForkResult {
	results := make([]ForkResult, 0, len(workUnits))
	for i, item := range workUnits {
		select {
		case <-ctx.Done():
			r.logger.Warn("fork cancelled", "completed", i, "total", len(workUnits))
			return results
		default:
		}

		r.logger.Debug("executing fork item", "index", i, "total", len(workUnits))
		result := r.executeForkItem(ctx, exec, step, item)
		results = append(results, result)
	}
	return results
}

// executeForkParallel executes work units in parallel with batching
func (r *RitualRunner) executeForkParallel(ctx context.Context, exec *RitualExecution, step RitualStep, workUnits []interface{}, batchSize int) []ForkResult {
	results := make([]ForkResult, 0, len(workUnits))
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, batchSize)

	for i, item := range workUnits {
		select {
		case <-ctx.Done():
			r.logger.Warn("fork cancelled", "completed", len(results), "total", len(workUnits))
			return results
		default:
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(idx int, it interface{}) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			r.logger.Debug("executing fork item", "index", idx, "total", len(workUnits))
			result := r.executeForkItem(ctx, exec, step, it)

			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(i, item)
	}

	wg.Wait()
	return results
}

// executeForkItem executes a single fork item
func (r *RitualRunner) executeForkItem(ctx context.Context, exec *RitualExecution, step RitualStep, item interface{}) ForkResult {
	// Create a fork-specific execution context
	forkExec := &RitualExecution{
		ID:         exec.ID,
		RitualName: exec.RitualName,
		EdictID:    exec.EdictID,
		Data:       storage.JSON{},
		def:        exec.def,
		stepStates: exec.stepStates,
		notify:     exec.notify,
	}

	// Copy existing context
	if exec.Data != nil {
		forkExec.Data = storage.JSON{}
		for k, v := range exec.Data {
			forkExec.Data[k] = v
		}
	}

	// Add .item to the context
	if forkExec.Data == nil {
		forkExec.Data = storage.JSON{}
	}
	forkExec.Data["item"] = item

	result := ForkResult{Item: item}

	// Execute each work step
	for _, workStep := range step.Work {
		// Create a temporary step state for this work item
		workStepState := RitualStepState{
			ExecutionID: exec.ID,
			StepIndex:   exec.CurrentStep,
			Name:        workStep.Name,
		}

		// Execute the work step
		output, err := r.executeStep(ctx, forkExec, workStep)
		workStepState.Message = output
		workStepState.Output = output

		if err != nil {
			result.Error = err
			result.Output = output
			r.logger.Warn("fork item failed",
				"item", item,
				"step", workStep.Name,
				"error", err)
			break
		}
		result.Output = output
	}

	return result
}

// executeMinisterStep invokes a minister for a task
func (r *RitualRunner) executeMinisterStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	minister := r.getMinister(step.Minister)
	if minister == nil {
		return "", fmt.Errorf("minister not found: %s", step.Minister)
	}

	act := r.expandTemplate(step.Act, exec)

	// Dynamic work prompt — rebuilt every invocation (fresh context)
	work := r.buildWorkPrompt(exec, act)

	// Reuse session if step was already invoked (e.g., goto re-invocation)
	session := exec.stepStates[exec.CurrentStep].Session

	// Immutable scratchpad — only for session creation
	scratchpad := ""
	if session == nil {
		scratchpad = r.buildEnhancedScratchpad(ctx, exec, step)
	}

	doneChan := make(chan Result, 1)
	t := &Task{
		Ctx:        ctx,
		EdictID:    exec.EdictID,
		Work:       work,
		Scratchpad: scratchpad,
		Session:    session,
		Done:       doneChan,
		Notify:     exec.notify, // Route minister output to Ruling tab
	}

	// Set up zhengming signal so we can pause the timeout
	zhengmingSig := make(chan struct{}, 1)
	if setter, ok := minister.(interface{ SetOnZhengmingRaised(func()) }); ok {
		setter.SetOnZhengmingRaised(func() {
			select {
			case zhengmingSig <- struct{}{}:
			default:
			}
		})
		defer setter.SetOnZhengmingRaised(nil)
	}

	// Send task to minister
	select {
	case minister.Tasks() <- t:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Wait for result with pausable timeout
	stepIdx := exec.CurrentStep
	timer := time.NewTimer(15 * time.Minute)
	defer timer.Stop()

	for {
		select {
		case result := <-doneChan:
			// Store session for potential reuse and capture session_id
			if result.Session != nil {
				exec.stepStates[stepIdx].Session = result.Session
				exec.stepStates[stepIdx].SessionID = result.Session.ID
				// Set ritual execution session_id if not already set (primary session)
				if exec.SessionID == "" {
					exec.SessionID = result.Session.ID
				}
			}
			if result.Err != nil {
				return result.Output, result.Err
			}
			if result.Failure != "" {
				return result.Output, fmt.Errorf("%s", result.Failure)
			}
			return result.Output, nil
		case <-zhengmingSig:
			// Zhengming raised — pause timeout, wait only for completion or cancellation
			timer.Stop()
			r.logger.Debug("zhengming raised, pausing step timeout", "step", step.Name)
			select {
			case result := <-doneChan:
				// Store session for potential reuse and capture session_id
				if result.Session != nil {
					exec.stepStates[stepIdx].Session = result.Session
					exec.stepStates[stepIdx].SessionID = result.Session.ID
					// Set ritual execution session_id if not already set (primary session)
					if exec.SessionID == "" {
						exec.SessionID = result.Session.ID
					}
				}
				if result.Err != nil {
					return result.Output, result.Err
				}
				if result.Failure != "" {
					return result.Output, fmt.Errorf("%s", result.Failure)
				}
				return result.Output, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		case <-timer.C:
			return "", fmt.Errorf("minister %s timeout", step.Minister)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// buildEnhancedScratchpad creates a unified scratchpad with ritual context, edict details, and previous step results
func (r *RitualRunner) buildEnhancedScratchpad(ctx context.Context, exec *RitualExecution, step RitualStep) string {
	var buf bytes.Buffer

	// 1. Ritual context
	stepNum := exec.CurrentStep + 1
	totalSteps := len(exec.def.Steps)
	fmt.Fprintf(&buf, "# Ritual Context\n\n")
	fmt.Fprintf(&buf, "**Ritual:** %s\n", exec.RitualName)
	fmt.Fprintf(&buf, "**Step:** %s (%d/%d)\n\n", step.Name, stepNum, totalSteps)

	// 2. Full edict details
	edict, clarifications, err := r.getEdictDetails(ctx, exec.EdictID)
	if err == nil && edict != nil {
		sealService := storage.NewSealService(r.db)
		status, _ := sealService.GetEdictStatus(exec.EdictID)
		fmt.Fprintf(&buf, "# Edict\n\n")
		fmt.Fprintf(&buf, "```json\n")
		fmt.Fprintf(&buf, "{\n")
		fmt.Fprintf(&buf, "  \"edict_id\": %d,\n", edict.EdictID)
		fmt.Fprintf(&buf, "  \"status\": %q\n", status)
		fmt.Fprintf(&buf, "}\n")
		fmt.Fprintf(&buf, "```\n\n")
		fmt.Fprintf(&buf, "## Intent\n\n%s\n\n", edict.Intent)

		// Include clarification history
		if len(clarifications) > 0 {
			fmt.Fprintf(&buf, "## Clarification History\n\n")
			for i, c := range clarifications {
				fmt.Fprintf(&buf, "### Clarification %d\n\n", i+1)
				for _, q := range c.Questions {
					fmt.Fprintf(&buf, "**Q:** %s\n\n", q.Text)
				}
				if c.Answer != "" {
					fmt.Fprintf(&buf, "**A:** %s\n\n", c.Answer)
				}
			}
		}
	}

	return buf.String()
}

// workPromptTmpl is the template for building dynamic work messages.
// Task comes first so the model reads the instruction before the data.
var workPromptTmpl = template.Must(template.New("work").Parse(
	`# Task
{{ .Act }}
{{ if or .step_results .given_context }}
---
# Reference Data
The following information has already been gathered for you. Use it directly — do NOT call tools to re-fetch this data.
{{ if .step_results }}
## Previous Step Results
{{ range $name, $result := .step_results }}
### {{ $name }}
{{ $result }}
{{ end }}{{ end }}{{ if .given_context }}
## Given Context
{{ range $key, $val := .given_context }}
### {{ $key }}
{{ $val }}
{{ end }}{{ end }}{{ end }}`))

// buildWorkPrompt builds the dynamic work message from step results, given context, and the act.
// This is rebuilt on every invocation so goto re-invocations get fresh context.
func (r *RitualRunner) buildWorkPrompt(exec *RitualExecution, act string) string {
	data := map[string]interface{}{
		"Act": act,
	}
	// Previous step results (use != to include steps after CurrentStep that ran before a goto)
	for i, ss := range exec.stepStates {
		if ss.Message != "" && i != exec.CurrentStep {
			if data["step_results"] == nil {
				data["step_results"] = map[string]string{}
			}
			data["step_results"].(map[string]string)[ss.Name] = ss.Message
		}
	}
	// Given context
	if exec.Data != nil {
		if given, ok := exec.Data["given_context"].(map[string]interface{}); ok {
			data["given_context"] = given
		}
	}
	var buf bytes.Buffer
	if err := workPromptTmpl.Execute(&buf, data); err != nil {
		r.logger.Warn("failed to execute work prompt template", "error", err)
		return act
	}
	return buf.String()
}

// getEdictDetails retrieves full edict information including clarification history
func (r *RitualRunner) getEdictDetails(ctx context.Context, edictID uint) (*storage.Edict, []storage.Zhengming, error) {
	var edict storage.Edict
	if err := r.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		return nil, nil, err
	}

	// Get clarification history
	var clarifications []storage.Zhengming
	r.db.Where("edict_id = ? AND status = ?", edictID, storage.ZhengmingAnswered).
		Order("created_at ASC").
		Find(&clarifications)

	return &edict, clarifications, nil
}

// runBuiltinGiven runs a builtin given function and returns the result
func (r *RitualRunner) runBuiltinGiven(ctx context.Context, exec *RitualExecution, fn string) (interface{}, error) {
	switch fn {
	case "get_edict":
		if exec.EdictID == 0 {
			return map[string]string{"status": "no edict (system event)"}, nil
		}
		return r.arrangeGetEdict(exec.EdictID)
	case "get_court_status":
		return r.getCourtStatus(exec.EdictID)
	case "get_manifests":
		return r.arrangeGetManifests(exec.EdictID)
	case "get_verdicts":
		return r.arrangeGetVerdicts(exec.EdictID)
	case "get_precedents":
		return r.arrangeGetPrecedents(exec.EdictID)
	case "get_earth_status":
		return r.getEarthStatus(ctx)
	case "get_borderlands":
		return r.getBorderlands(ctx)
	case "check_clean_working_directory":
		return r.checkCleanWorkingDirectory(ctx)
	case "get_infrastructure_templates":
		return r.getInfrastructureTemplates(ctx)
	case "build_sandbox":
		return r.buildSandbox(ctx)
	case "verify_sandbox_ready":
		return r.verifySandboxReady(ctx)
	case "get_project_metadata":
		return r.getProjectMetadata(ctx)
	default:
		return nil, fmt.Errorf("unknown given function: %s", fn)
	}
}

func (r *RitualRunner) arrangeGetEdict(edictID uint) (interface{}, error) {
	var edict storage.Edict
	if err := r.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		return nil, err
	}
	sealService := storage.NewSealService(r.db)
	status, err := sealService.GetEdictStatus(edictID)
	if err != nil {
		status = storage.EdictActive // default if error
	}
	return map[string]interface{}{
		"edict_id": edict.EdictID,
		"intent":   edict.Intent,
		"status":   string(status),
	}, nil
}

func (r *RitualRunner) getCourtStatus(edictID uint) (interface{}, error) {
	// Use a single SQL query to fetch and filter edicts by derived status
	var result []map[string]interface{}
	query := `
SELECT 
    e.edict_id, e.session_id, e.issue_ref, e.intent, e.created_at, e.updated_at,
    CASE 
        WHEN EXISTS (SELECT 1 FROM seals s WHERE s.edict_id = e.edict_id AND s.minister_id = 'ruler') THEN 'sealed'
        WHEN EXISTS (SELECT 1 FROM zhengming_requests z WHERE z.edict_id = e.edict_id AND z.status = 'pending') THEN 'blocked'
        WHEN EXISTS (SELECT 1 FROM seals s WHERE s.edict_id = e.edict_id AND s.minister_id = 'confucius') THEN 'active'
        WHEN EXISTS (SELECT 1 FROM seals s WHERE s.edict_id = e.edict_id AND s.minister_id = 'judge') THEN 'active'
        ELSE 'active'
    END as status
FROM edicts e
WHERE NOT EXISTS (SELECT 1 FROM seals s WHERE s.edict_id = e.edict_id AND s.minister_id = 'ruler')
ORDER BY e.updated_at DESC
`
	if err := r.db.Raw(query).Scan(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (r *RitualRunner) arrangeGetManifests(edictID uint) (interface{}, error) {
	var manifests []storage.ForgeManifest
	if err := r.db.Where("edict_id = ?", edictID).Find(&manifests).Error; err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, len(manifests))
	for i, m := range manifests {
		result[i] = map[string]interface{}{
			"manifest_id": m.ManifestID,
			"edict_id":    m.EdictID,
			"ling_id":     m.LingID,
			"file_path":   m.FilePath,
			"func_name":   m.FuncName,
			"content_sha": m.ContentSHA,
			"commit_hash": m.CommitHash,
			"status":      string(m.Status),
			"verdict_id":  m.VerdictID,
			"created_at":  m.CreatedAt,
			"updated_at":  m.UpdatedAt,
		}
	}
	return result, nil
}

func (r *RitualRunner) arrangeGetVerdicts(edictID uint) (interface{}, error) {
	var verdicts []storage.JudgeVerdict
	err := r.db.Joins("JOIN forge_manifests ON forge_manifests.manifest_id = judge_verdicts.manifest_id").
		Where("forge_manifests.edict_id = ?", edictID).
		Find(&verdicts).Error
	if err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, len(verdicts))
	for i, v := range verdicts {
		result[i] = map[string]interface{}{
			"verdict_id":  v.VerdictID,
			"manifest_id": v.ManifestID,
			"outcome":     string(v.Outcome),
		}
	}
	return result, nil
}

func (r *RitualRunner) arrangeGetPrecedents(edictID uint) (interface{}, error) {
	var precedents []storage.CensorPrecedent
	err := r.db.Joins("JOIN forge_manifests ON forge_manifests.manifest_id = censor_precedents.manifest_id").
		Where("forge_manifests.edict_id = ?", edictID).
		Find(&precedents).Error
	if err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, len(precedents))
	for i, p := range precedents {
		result[i] = map[string]interface{}{
			"precedent_id": p.PrecedentID,
			"manifest_id":  p.ManifestID,
			"ruling":       string(p.Ruling),
			"principle":    p.Principle,
		}
	}
	return result, nil
}

// getEarthStatus captures the three parts of the Earth realm:
// the capital (git log), the middle kingdom (git diff --staged), and the borderlands (git diff).
func (r *RitualRunner) getEarthStatus(ctx context.Context) (interface{}, error) {
	result := map[string]string{
		"earth_status:capital":        "",
		"earth_status:middle_kingdom": "",
		"earth_status:borderlands":    "",
	}

	// Git operations always run on host (not in sandbox)
	gitRun := func(cmd, desc string) string {
		output, err := runners.HostRun(ctx, runners.Input{
			Command:        cmd,
			Description:    desc,
			BypassApproval: true,
		})
		if err == nil {
			return output.Output
		}
		return ""
	}

	result["earth_status:capital"] = gitRun("git log --oneline -20", "get capital status (git log)")
	result["earth_status:middle_kingdom"] = gitRun("git diff --staged", "get middle kingdom (git diff --staged)")
	result["earth_status:borderlands"] = gitRun("git diff", "get earth status: borderlands (git diff)")

	return result, nil
}

// getBorderlands captures unstaged changes and untracked files.
func (r *RitualRunner) getBorderlands(ctx context.Context) (interface{}, error) {
	result := map[string]string{
		"borderlands:changes":   "",
		"borderlands:untracked": "",
	}
	// Git operations always run on host
	diff, err := runners.HostRun(ctx, runners.Input{
		Command:        "git diff",
		Description:    "get borderlands (git diff)",
		BypassApproval: true,
	})
	if err == nil {
		result["borderlands:changes"] = diff.Output
	}
	untracked, err := runners.HostRun(ctx, runners.Input{
		Command:        "git ls-files --others --exclude-standard",
		Description:    "get borderlands (untracked files)",
		BypassApproval: true,
	})
	if err == nil {
		result["borderlands:untracked"] = untracked.Output
	}
	return result, nil
}

// checkCleanWorkingDirectory verifies the working directory is clean (no unstaged changes)
func (r *RitualRunner) checkCleanWorkingDirectory(ctx context.Context) (interface{}, error) {
	repoInfo := repo.GetRepoInfo()
	if !repoInfo.IsClean() {
		return nil, fmt.Errorf("working directory is not clean: %v", repoInfo)
	}
	return map[string]string{"status": "clean"}, nil
}

// getInfrastructureTemplates creates infrastructure files from embedded templates and returns their paths
func (r *RitualRunner) getInfrastructureTemplates(ctx context.Context) (interface{}, error) {
	// Ensure directory structure exists using host runner
	if _, err := runners.HostRun(ctx, runners.Input{
		Command:        "mkdir -p .agents/sandbox",
		Description:    "create .agents/sandbox directory structure",
		BypassApproval: true,
	}); err != nil {
		return nil, fmt.Errorf("failed to create .agents/sandbox directory: %w", err)
	}

	// Write embedded templates to project root
	files := map[string]string{
		"Justfile":                   dotagentsJustfile,
		".agents/asimi.conf":         dotagentsAsimiConf,
		".agents/sandbox/Dockerfile": dotagentsDockerfile,
		".agents/sandbox/bashrc":     dotagentsBashrc,
	}

	// Write files that don't already exist and track created paths
	createdFiles := []string{}
	for destPath, content := range files {
		if _, err := os.Stat(destPath); err == nil {
			// File already exists (e.g. from a previous attempt) — don't overwrite LLM customizations
			continue
		}
		if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", destPath, err)
		}
		createdFiles = append(createdFiles, destPath)
	}

	return map[string]interface{}{
		"template_files": createdFiles,
		"directories":    []string{".agents", ".agents/sandbox"},
	}, nil
}

// checkBuiltSandbox verifies the sandbox container image exists
func (r *RitualRunner) buildSandbox(ctx context.Context) (interface{}, error) {
	output, err := runners.HostRun(ctx, runners.Input{
		Command:        "just build-sandbox",
		Description:    "bulid the sandbox",
		BypassApproval: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build the sandbox image: %w", err)
	}
	return map[string]string{"status": "built", "output": output.Output}, nil
}

// verifySandboxReady builds the sandbox and appends RCA guidance on failure
func (r *RitualRunner) verifySandboxReady(ctx context.Context) (interface{}, error) {
	output, err := runners.HostRun(ctx, runners.Input{
		Command:        "just build-sandbox",
		Description:    "build the sandbox",
		BypassApproval: true,
	})
	if err == nil && output.ExitCode != "0" {
		r.logger.Warn("build-sandbox failed", "exit_code", output.ExitCode, "output", output.Output)
		err = fmt.Errorf("build-sandbox exited with code %s: %s", output.ExitCode, output.Output)
	}
	if err == nil {
		// Reload the runner to pick up the newly built sandbox image
		// TODO: Find a better way then reloading the config
		cfg, loadErr := config.LoadConfig()
		if loadErr != nil {
			output.Output = "Failed to load configuration " + loadErr.Error()
			goto fail
		}
		repoInfo := repo.GetRepoInfo()
		r.runner = runners.InitShellRunner(&cfg.Sandbox, repoInfo)
		if r.runner != nil && r.onRunnerUpgrade != nil {
			r.onRunnerUpgrade(r.runner)
		}
		if r.runner == nil {
			output.Output = "container runner not available"
			goto fail
		}
		if r.runner.RunnerType() != "podman" {
			output.Output = "failed to bring the container up"
			goto fail
		}

		// Run `just build` inside the sandbox to verify it works
		output, err = r.runner.Run(ctx, runners.Input{
			Command:        "just build",
			Description:    "verify sandbox by building inside container",
			BypassApproval: true,
		})
		if err == nil {
			// Run `just test` as non-blocking smoke test
			testOutput, testErr := r.runner.Run(ctx, runners.Input{
				Command:        "just test",
				Description:    "smoke test: verify tests run in sandbox",
				BypassApproval: true,
			})
			if testErr == nil && testOutput.ExitCode != "0" {
				r.logger.Warn("just test failed (non-blocking during init)",
					"exit_code", testOutput.ExitCode,
					"output", testOutput.Output)
				// Don't return error - tests are optional during project init
				// They may fail due to missing dependencies or incomplete setup
			}
			return map[string]string{
				"status": "ready",
				"output": output.Output,
			}, nil
		}

	}
fail:
	return map[string]string{
		"status": "failed",
		"output": `sandbox verification failed.
			Start RCA with .agents/sandbox/Dockerfile and verification output:` +
			output.Output,
	}, fmt.Errorf("sandbox verification failed: %w", err)
}

// getProjectMetadata captures repository information for use in ritual templates
func (r *RitualRunner) getProjectMetadata(ctx context.Context) (interface{}, error) {
	repoInfo := repo.GetRepoInfo()

	// Use ProjectRoot if available, fall back to cwd for remote URL lookup
	root := repoInfo.ProjectRoot
	if root == "" {
		root, _ = os.Getwd()
	}

	// Parse host, org, project from remote URL
	host, org, project := "local", "local", "unknown"
	if remote, err := repo.GitRemoteOriginURL(root); err == nil && remote != "" {
		host, org, project = parseHostOrgProject(remote)
	}

	// Derive slug: prefer repoInfo.Slug, fall back to org-project from remote
	slug := repoInfo.Slug
	if slug == "" && org != "local" && project != "unknown" {
		slug = org + "-" + project
	}

	// Extract project name from slug
	projectName := slug
	if idx := strings.LastIndex(slug, "-"); idx >= 0 {
		projectName = slug[idx+1:]
	}
	if projectName == "" {
		projectName = "unknown"
	}

	return map[string]string{
		"project_slug": slug,
		"project_name": projectName,
		"branch":       repoInfo.Branch,
		"host":         host,
		"org":          org,
		"project":      project,
	}, nil
}

// parseHostOrgProject extracts host, organization, and project from a git remote URL
func parseHostOrgProject(remote string) (host, org, project string) {
	host = "github.com" // default
	if strings.Contains(remote, "://") {
		if u, err := url.Parse(remote); err == nil {
			host = u.Host
		}
	} else if strings.Contains(remote, "@") {
		// SSH format: git@github.com:owner/repo.git
		parts := strings.SplitN(remote, "@", 2)
		if len(parts) == 2 {
			hostPart := strings.SplitN(parts[1], ":", 2)
			if len(hostPart) >= 1 {
				host = hostPart[0]
			}
		}
	}

	owner, repoName := repo.ParseGitRemote(remote)
	if owner == "" || repoName == "" {
		return host, "unknown", "unknown"
	}

	return host, repo.SanitizeSegment(owner), repo.SanitizeSegment(repoName)
}

// runBuiltinThen runs a builtin then function (extensible via step registry)
func (r *RitualRunner) runBuiltinThen(ctx context.Context, exec *RitualExecution, fn string) error {
	// Non-edict operations run regardless of EdictID
	switch fn {
	case "verify_sandbox_ready":
		_, err := r.verifySandboxReady(ctx)
		return err
	case "stage_infrastructure":
		// Stage infrastructure files on the host (git runs on host, not in sandbox)
		output, err := runners.HostRun(ctx, runners.Input{
			Command:        "git add AGENTS.md Justfile .agents/",
			Description:    "stage infrastructure files",
			BypassApproval: true,
		})
		if err != nil {
			return fmt.Errorf("failed to stage infrastructure: %w", err)
		}
		if output.ExitCode != "0" {
			return fmt.Errorf("git add failed (exit %s): %s", output.ExitCode, output.Output)
		}
		return nil
	}

	// Edict-specific operations require an active edict
	if exec.EdictID == 0 {
		r.logger.Debug("skipping edict operation for system ritual", "fn", fn)
		return nil
	}
	switch fn {
	case "seal_edict":
		// Sealing is now done via the seal chain - grant ruler seal
		sealService := storage.NewSealService(r.db)
		return sealService.GrantSeal(exec.EdictID, "ruler", storage.JSON{"ritual": exec.RitualName})
	case "block_edict":
		// Blocking is now done via zhengming - create a pending zhengming request
		// This is handled by the request_zhengming case below
		return nil
	case "unblock_edict":
		// Unblocking happens automatically when zhengming is answered
		// No action needed here
		return nil
	case "request_zhengming":
		// Use the chancellor for zhengming requests, as it's the minister that interacts with the ruler
		// and has a corresponding tab for displaying zhengming questions
		minister := r.getMinister("chancellor")
		if minister == nil {
			return fmt.Errorf("minister not found: chancellor")
		}
		type zhengmingGate interface {
			RequestZhengming(uint, storage.ZhengmingQuestions, storage.ZhengmingPriority) (string, error)
		}
		gate, ok := minister.(zhengmingGate)
		if !ok {
			return fmt.Errorf("minister chancellor does not support zhengming")
		}
		// Get the step that just completed to include in the question
		stepName := ""
		if exec.CurrentStep >= 0 && exec.CurrentStep < len(exec.def.Steps) {
			stepName = exec.def.Steps[exec.CurrentStep].Name
		}
		questions := storage.ZhengmingQuestions{{
			Text:    fmt.Sprintf("The %s has completed work on edict %d. Do you approve?", stepName, exec.EdictID),
			Options: []string{"Approve and proceed", "Let me clarify", "Reject"},
		}}
		requestID, err := gate.RequestZhengming(exec.EdictID, questions, storage.PriorityUrgent)
		if err != nil {
			return fmt.Errorf("failed to request zhengming: %w", err)
		}
		if exec.notify != nil {
			exec.notify(ZhengmingPendingMsg{
				RequestID:  requestID,
				EdictID:    exec.EdictID,
				MinisterID: "chancellor",
				Questions:  questions,
				Priority:   storage.PriorityUrgent,
			})
		}
		// Store request_id in execution data
		if exec.Data == nil {
			exec.Data = storage.JSON{}
		}
		exec.Data["pending_zhengming"] = requestID
		// Return sentinel so the caller can block until the ruler answers
		return ErrZhengmingPending
	case "the changes are staged":
		// Stage all changes in the working directory (Borderlands → Middle Kingdom)
		if r.runner == nil {
			return fmt.Errorf("no runner configured for staging changes")
		}
		output, err := r.runner.Run(ctx, runners.Input{
			Command:        "git add -A",
			Description:    "stage all changes (Borderlands → Middle Kingdom)",
			BypassApproval: true,
		})
		if err != nil {
			return fmt.Errorf("failed to stage changes: %w", err)
		}
		if output.ExitCode != "0" {
			return fmt.Errorf("git add failed (exit %s): %s", output.ExitCode, output.Output)
		}
		return nil
	case "await_ruler_seal":
		// Stage only files from manifests (not git add -A)
		var manifests []storage.ForgeManifest
		if err := r.db.Where("edict_id = ?", exec.EdictID).Find(&manifests).Error; err != nil {
			return fmt.Errorf("failed to query manifests: %w", err)
		}

		if len(manifests) > 0 {
			// Extract file paths
			files := make([]string, len(manifests))
			for i, m := range manifests {
				files[i] = m.FilePath
			}

			// Stage specific files
			cmd := "git add " + strings.Join(files, " ")
			output, err := r.runner.Run(ctx, runners.Input{
				Command:        cmd,
				Description:    "stage manifest files",
				BypassApproval: true,
			})
			if err != nil {
				return fmt.Errorf("failed to stage manifests: %w", err)
			}
			if output.ExitCode != "0" {
				return fmt.Errorf("git add failed (exit %s): %s", output.ExitCode, output.Output)
			}
		}

		// TODO: Raise event - awaiting ruler's seat
		return nil
	case "record the judge's seal":
		// Record the judge's seal on the edict
		sealService := storage.NewSealService(r.db)
		if err := sealService.GrantSeal(exec.EdictID, "judge", storage.JSON{"ritual": exec.RitualName}); err != nil {
			return fmt.Errorf("failed to record judge's seal: %w", err)
		}
		return nil
	case "record the sage's seal":
		// Record the sage's seal on the edict
		sealService := storage.NewSealService(r.db)
		if err := sealService.GrantSeal(exec.EdictID, "sage", storage.JSON{"ritual": exec.RitualName}); err != nil {
			return fmt.Errorf("failed to record sage's seal: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown then function: %s", fn)
	}
}

// resolveStepDef resolves a raw step string into a StepDefEntry.
// "!" prefix → bash command, else matched via cucumber-expressions registry.
func (r *RitualRunner) resolveStepDef(raw string) (StepDefEntry, error) {
	if strings.HasPrefix(raw, "!") {
		cmd := strings.TrimPrefix(raw, "!")
		// Sanitize command into a key: take first word
		key := strings.Fields(cmd)[0]
		key = strings.ReplaceAll(key, "/", "_")
		return StepDefEntry{
			Kind:    StepDefBash,
			Key:     key,
			Command: cmd,
		}, nil
	}

	def, err := r.stepDefs.Match(raw)
	if err != nil {
		return StepDefEntry{}, fmt.Errorf("step matching error: %w", err)
	}
	if def == nil {
		return StepDefEntry{}, fmt.Errorf("no step definition matches %q", raw)
	}
	return StepDefEntry{
		Kind:    StepDefBuiltin,
		Key:     def.OutputKey,
		Command: def.HandlerKey,
	}, nil
}

// storeGivenResult stores a given step result into exec.Data["given_context"].
// map[string]string results are flattened into separate colon-delimited keys.
func storeGivenResult(exec *RitualExecution, key string, result interface{}) {
	if exec.Data == nil {
		exec.Data = storage.JSON{}
	}
	given, _ := exec.Data["given_context"].(map[string]interface{})
	if given == nil {
		given = make(map[string]interface{})
	}
	if m, ok := result.(map[string]string); ok {
		for k, v := range m {
			given[k] = v
		}
	} else {
		given[key] = result
	}
	exec.Data["given_context"] = given
}

// runGivenStep executes a single given step and returns its result
func (r *RitualRunner) runGivenStep(ctx context.Context, exec *RitualExecution, entry StepDefEntry) (interface{}, error) {
	switch entry.Kind {
	case StepDefBash:
		if r.runner == nil {
			return nil, fmt.Errorf("no runner configured for bash given step")
		}
		cmd := r.expandTemplate(entry.Command, exec)
		output, err := r.runner.Run(ctx, runners.Input{
			Command:        cmd,
			Description:    fmt.Sprintf("given: %s", entry.Command),
			BypassApproval: true,
		})
		if err != nil {
			return nil, err
		}
		return output.Output, nil
	case StepDefBuiltin:
		return r.runBuiltinGiven(ctx, exec, entry.Command)
	default:
		return nil, fmt.Errorf("unknown step kind: %d", entry.Kind)
	}
}

// runThenStep executes a single then step and returns an error if it fails
func (r *RitualRunner) runThenStep(ctx context.Context, exec *RitualExecution, entry StepDefEntry) error {
	switch entry.Kind {
	case StepDefBash:
		if r.runner == nil {
			return fmt.Errorf("no runner configured for bash then step")
		}
		cmd := r.expandTemplate(entry.Command, exec)
		output, err := r.runner.Run(ctx, runners.Input{
			Command:        cmd,
			Description:    fmt.Sprintf("then: %s", entry.Command),
			BypassApproval: true,
		})
		if err != nil {
			return err
		}
		if output.ExitCode != "0" {
			return fmt.Errorf("then failed (exit %s): %s", output.ExitCode, output.Output)
		}
		return nil
	case StepDefBuiltin:
		return r.runBuiltinThen(ctx, exec, entry.Command)
	default:
		return fmt.Errorf("unknown step kind: %d", entry.Kind)
	}
}

// handleFailure handles step failure based on on_failure action
func (r *RitualRunner) handleFailure(ctx context.Context, exec *RitualExecution, step RitualStep, err error) bool {
	action := OnFailureAction(step.resolveOnFailure(exec.def))
	if action == "" {
		action = OnFailureAbort
	}

	// Don't retry if context was cancelled (e.g. user pressed CTRL-C)
	if ctx.Err() != nil {
		return false
	}

	switch action {
	case OnFailureRetry:
		maxRetries := step.resolveMaxRetries(exec.def)
		if maxRetries == 0 {
			maxRetries = r.maxRetries
		}
		if exec.stepStates[exec.CurrentStep].RetryCount < maxRetries {
			exec.stepStates[exec.CurrentStep].RetryCount++
			// Notify: retrying
			if exec.notify != nil {
				exec.notify(RitualStepMsg{
					TabID:       "chancellor",
					RitualName:  exec.RitualName,
					ExecutionID: exec.ID,
					EdictID:     exec.EdictID,
					StepName:    step.Name,
					StepIndex:   exec.CurrentStep,
					TotalSteps:  len(exec.def.Steps),
					Status:      "retrying",
					Message:     fmt.Sprintf("attempt %d/%d", exec.stepStates[exec.CurrentStep].RetryCount, maxRetries),
				})
			}
			r.logger.Info("retrying step",
				"step", step.Name,
				"attempt", exec.stepStates[exec.CurrentStep].RetryCount)
			return true
		}
		// Retries exhausted — invoke report_failure ritual
		r.invokeReportFailure(ctx, exec, step, err)
		return false

	case OnFailureZhengming:
		// Request clarification
		r.logger.Info("requesting zhengming for failed step", "step", step.Name)
		// This would create a zhengming request
		return false

	case OnFailureGoto:
		if step.OnFailureTarget != "" {
			targetIdx := r.stepIndex(exec.def, step.OnFailureTarget)
			if targetIdx != -1 {
				state := &exec.stepStates[exec.CurrentStep]
				if state.Output != "" {
					state.Message = fmt.Sprintf("Step '%s' failed.\n\nOutput:\n%s\n\nError: %s",
						step.Name, state.Output, err.Error())
				} else {
					state.Message = fmt.Sprintf("Step '%s' failed with error: %s",
						step.Name, err.Error())
				}
				exec.CurrentStep = targetIdx
				r.logger.Info("jumping to step on failure",
					"from", step.Name,
					"to", step.OnFailureTarget)
				return true
			}
		}
		return false

	case OnFailureAbort:
		return false

	default:
		return false
	}
}

// emitEvent records a Tian event from the ritual runner.
// Routes through publishEvent for channel delivery when available.
func (r *RitualRunner) emitEvent(edictID uint, eventType storage.ShogunateEvent, payload storage.JSON) {
	if r.publishEvent != nil {
		r.publishEvent(edictID, eventType, payload)
		return
	}
	// Fallback: DB-only (for tests without shogunate)
	event := storage.TianEvent{
		EdictID:   edictID,
		EventType: eventType,
		Payload:   payload,
	}
	if err := r.db.Create(&event).Error; err != nil {
		r.logger.Warn("failed to emit ritual event", "type", eventType, "error", err)
	}
}

// invokeReportFailure triggers the report_failure ritual when retries are exhausted
func (r *RitualRunner) invokeReportFailure(ctx context.Context, exec *RitualExecution, step RitualStep, err error) {
	if r.registry.Get("report_failure") == nil {
		return
	}
	inputs := map[string]string{
		"edict_id":    fmt.Sprint(exec.EdictID),
		"ritual_name": exec.RitualName,
		"step_name":   step.Name,
		"error":       err.Error(),
	}
	go func() {
		failExec, startErr := r.Start(ctx, "report_failure", exec.EdictID, inputs, nil)
		if startErr != nil {
			r.logger.Warn("failed to start report_failure ritual", "error", startErr)
			return
		}
		if runErr := r.Run(ctx, failExec); runErr != nil {
			r.logger.Warn("report_failure ritual failed", "error", runErr)
		}
	}()
}

// stepIndex returns the index of a step by name
func (r *RitualRunner) stepIndex(def *RitualDef, name string) int {
	for i, step := range def.Steps {
		if step.Name == name {
			return i
		}
	}
	return -1
}

// expandTemplate expands Go text/template syntax in a string
func (r *RitualRunner) expandTemplate(text string, exec *RitualExecution) string {
	data := map[string]interface{}{
		"edict_id": exec.EdictID,
	}
	// Merge inputs into template data (map[string]string from Start(), map[string]interface{} after DB round-trip)
	if exec.Data != nil {
		switch inputs := exec.Data["inputs"].(type) {
		case map[string]interface{}:
			for k, v := range inputs {
				data[k] = v
			}
		case map[string]string:
			for k, v := range inputs {
				data[k] = v
			}
		}
	}
	// Merge given context into template data
	if exec.Data != nil {
		if given, ok := exec.Data["given_context"].(map[string]interface{}); ok {
			for k, v := range given {
				data[k] = v
			}
		}
	}
	// Merge act result
	if exec.Data != nil {
		if ar, ok := exec.Data["act_result"]; ok {
			data["act_result"] = ar
		}
	}
	// Merge fork context (.fork.out, .fork.err, .item)
	if exec.Data != nil {
		if fork, ok := exec.Data["fork"]; ok {
			data["fork"] = fork
		}
		if item, ok := exec.Data["item"]; ok {
			data["item"] = item
		}
	}
	// Build step_results from completed steps
	stepResults := map[string]interface{}{}
	for i, ss := range exec.stepStates {
		if ss.Message != "" && i != exec.CurrentStep {
			stepResults[ss.Name] = ss.Message
		}
	}
	if len(stepResults) > 0 {
		data["step_results"] = stepResults
	}

	tmpl, err := template.New("ritual").Parse(text)
	if err != nil {
		r.logger.Warn("failed to parse template", "error", err, "text", text)
		return text
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		r.logger.Warn("failed to execute template", "error", err, "text", text)
		return text
	}
	return buf.String()
}

// saveExecution persists execution state to database
func (r *RitualRunner) saveExecution(exec *RitualExecution) {
	exec.UpdatedAt = time.Now()
	if err := r.db.Save(exec).Error; err != nil {
		r.logger.Warn("failed to save ritual execution", "error", err)
	}

	for i := range exec.stepStates {
		if err := r.db.Save(&exec.stepStates[i]).Error; err != nil {
			r.logger.Warn("failed to save step state", "error", err)
		}
	}
}

// GetExecution retrieves an execution by ID
func (r *RitualRunner) GetExecution(executionID string) (*RitualExecution, error) {
	var exec RitualExecution
	if err := r.db.First(&exec, "id = ?", executionID).Error; err != nil {
		return nil, err
	}

	// Load step states
	var states []RitualStepState
	if err := r.db.Where("execution_id = ?", executionID).Order("step_index").Find(&states).Error; err != nil {
		return nil, err
	}
	exec.stepStates = states

	// Load definition
	exec.def = r.registry.Get(exec.RitualName)

	return &exec, nil
}

// ListExecutions lists executions for an edict
func (r *RitualRunner) ListExecutions(edictID uint) ([]RitualExecution, error) {
	var executions []RitualExecution
	query := r.db.Order("created_at DESC")
	if edictID != 0 {
		query = query.Where("edict_id = ?", edictID)
	}
	if err := query.Find(&executions).Error; err != nil {
		return nil, err
	}
	return executions, nil
}
