package shogunate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	cucumberexpressions "github.com/cucumber/cucumber-expressions/go/v19"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// RitualStepMsg notifies the UI of ritual step progress
type RitualStepMsg struct {
	TabID       string
	RitualName  string
	ExecutionID string
	EdictID     string
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
		{"the edict is sealed", "seal_edict", "sealed"},
		{"the edict is blocked", "block_edict", "blocked"},
		{"the edict is unblocked", "unblock_edict", "unblocked"},
		{"the ruler approves", "request_zhengming", "approved"},
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
	registry     *RitualRegistry
	stepDefs     *StepDefRegistry
	getMinister  func(id string) Minister
	publishEvent func(edictID string, eventType storage.ShogunateEvent, payload storage.JSON) string
	db           *gorm.DB
	runner       runners.Runner
	logger       *slog.Logger
	maxRetries   int
}

// NewRitualRunner creates a new ritual runner
func NewRitualRunner(
	registry *RitualRegistry,
	getMinister func(id string) Minister,
	publishEvent func(edictID string, eventType storage.ShogunateEvent, payload storage.JSON) string,
	db *gorm.DB,
	runner runners.Runner,
	logger *slog.Logger,
) *RitualRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &RitualRunner{
		registry:     registry,
		stepDefs:     NewStepDefRegistry(),
		getMinister:  getMinister,
		publishEvent: publishEvent,
		db:           db,
		runner:       runner,
		logger:       logger,
		maxRetries:   3,
	}
}

// RitualExecution tracks a running ritual instance
type RitualExecution struct {
	ID          string       `gorm:"primaryKey;column:id"`
	RitualName  string       `gorm:"column:ritual_name"`
	EdictID     string       `gorm:"column:edict_id;index"`
	SessionID   string       `gorm:"column:session_id"`
	CurrentStep int          `gorm:"column:current_step"`
	State       RitualState  `gorm:"column:state"`
	Data        storage.JSON `gorm:"column:data;type:json"`
	CreatedAt   time.Time    `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time    `gorm:"column:updated_at;autoUpdateTime"`

	// Runtime (not persisted)
	def        *RitualDef
	stepStates []RitualStepState
	notify     internal.NotifyFunc
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
func (r *RitualRunner) Start(ctx context.Context, ritualName, edictID string, inputs map[string]string, notify internal.NotifyFunc) (*RitualExecution, error) {
	def := r.registry.Get(ritualName)
	if def == nil {
		return nil, fmt.Errorf("ritual not found: %s", ritualName)
	}

	// Validate required inputs
	for name, inputDef := range def.Inputs {
		if inputDef.Required {
			if _, ok := inputs[name]; !ok {
				if inputDef.Default == "" {
					return nil, fmt.Errorf("required input %q not provided", name)
				}
				inputs[name] = inputDef.Default
			}
		}
	}

	// Create execution record
	exec := &RitualExecution{
		ID:          GenerateID("ritual", ritualName, edictID, time.Now().String()),
		RitualName:  ritualName,
		EdictID:     edictID,
		CurrentStep: 0,
		State:       RitualStatePending,
		Data:        storage.JSON{"inputs": inputs},
		def:         def,
		notify:      notify,
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
	if err := r.db.Create(exec).Error; err != nil {
		return nil, fmt.Errorf("failed to create ritual execution: %w", err)
	}

	for _, state := range exec.stepStates {
		if err := r.db.Create(&state).Error; err != nil {
			return nil, fmt.Errorf("failed to create step state: %w", err)
		}
	}

	r.logger.Info("ritual started",
		"ritual", ritualName,
		"execution_id", exec.ID,
		"edict_id", edictID)

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
			return ctx.Err()
		default:
		}

		step := exec.def.Steps[exec.CurrentStep]
		result, err := r.executeStep(ctx, exec, step)
		if err != nil {
			exec.stepStates[exec.CurrentStep].Message = err.Error()
			exec.stepStates[exec.CurrentStep].Output = result

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
					Message:     err.Error(),
				})
			}
			// Context cancelled (user interrupt) — abort without cascading events
			if ctx.Err() != nil {
				exec.State = RitualStateAborted
				r.saveExecution(exec)
				return err
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
				// Emit ritual_failed Tian event
				r.emitEvent(exec.EdictID, storage.EventRitualFailed, storage.JSON{
					"ritual":       exec.RitualName,
					"execution_id": exec.ID,
					"step":         step.Name,
					"error":        err.Error(),
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

	exec.State = RitualStateCompleted
	r.saveExecution(exec)

	// === RITUAL-LEVEL THEN ===
	for _, raw := range exec.def.Then {
		entry, err := r.resolveStepDef(raw)
		if err != nil {
			r.logger.Warn("ritual-level then failed to resolve", "raw", raw, "error", err)
			continue
		}
		if err := r.runThenStep(ctx, exec, entry); err != nil {
			r.logger.Warn("ritual-level then step failed", "raw", raw, "error", err)
		}
	}

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
	r.emitEvent(exec.EdictID, storage.EventRitualCompleted, storage.JSON{
		"ritual":       exec.RitualName,
		"execution_id": exec.ID,
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
		if err := r.runThenStep(ctx, exec, entry); err != nil {
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
		ID:          exec.ID,
		RitualName:  exec.RitualName,
		EdictID:     exec.EdictID,
		Data:        storage.JSON{},
		def:         exec.def,
		stepStates:  exec.stepStates,
		notify:      exec.notify,
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
		fmt.Fprintf(&buf, "# Edict\n\n")
		fmt.Fprintf(&buf, "```json\n")
		fmt.Fprintf(&buf, "{\n")
		fmt.Fprintf(&buf, "  \"edict_id\": %q,\n", edict.EdictID)
		fmt.Fprintf(&buf, "  \"status\": %q\n", edict.Status)
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
func (r *RitualRunner) getEdictDetails(ctx context.Context, edictID string) (*storage.Edict, []storage.Zhengming, error) {
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
		if exec.EdictID == "" {
			return map[string]string{"status": "no edict (system event)"}, nil
		}
		return r.arrangeGetEdict(exec.EdictID)
	case "get_court_status":
		return r.arrangeGetCourtStatus(exec.EdictID)
	case "get_manifests":
		return r.arrangeGetManifests(exec.EdictID)
	case "get_verdicts":
		return r.arrangeGetVerdicts(exec.EdictID)
	case "get_precedents":
		return r.arrangeGetPrecedents(exec.EdictID)
	case "get_earth_status":
		return r.getEarthStatus(ctx)
	default:
		return nil, fmt.Errorf("unknown given function: %s", fn)
	}
}

func (r *RitualRunner) arrangeGetEdict(edictID string) (interface{}, error) {
	var edict storage.Edict
	if err := r.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"edict_id": edict.EdictID,
		"intent":   edict.Intent,
		"status":   string(edict.Status),
	}, nil
}

func (r *RitualRunner) arrangeGetCourtStatus(edictID string) (interface{}, error) {
	var edicts []storage.Edict
	if err := r.db.Where("status NOT IN ?", []string{"sealed", "cancelled"}).Find(&edicts).Error; err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, len(edicts))
	for i, e := range edicts {
		result[i] = map[string]interface{}{
			"edict_id":   e.EdictID,
			"session_id": e.SessionID,
			"issue_ref":  e.IssueRef,
			"intent":     e.Intent,
			"status":     string(e.Status),
			"created_at": e.CreatedAt,
			"updated_at": e.UpdatedAt,
		}
	}
	return result, nil
}

func (r *RitualRunner) arrangeGetManifests(edictID string) (interface{}, error) {
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

func (r *RitualRunner) arrangeGetVerdicts(edictID string) (interface{}, error) {
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

func (r *RitualRunner) arrangeGetPrecedents(edictID string) (interface{}, error) {
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

	if r.runner == nil {
		return result, nil // Return empty values if no runner available
	}

	// The capital: git log (recent commits)
	capitalOutput, err := r.runner.Run(ctx, runners.Input{
		Command:        "git log --oneline -20",
		Description:    "get capital status (git log)",
		BypassApproval: true,
	})
	if err == nil {
		result["earth_status:capital"] = capitalOutput.Output
	}

	// The middle kingdom: git diff --staged
	middleKingdomOutput, err := r.runner.Run(ctx, runners.Input{
		Command:        "git diff --staged",
		Description:    "get middle kingdom (git diff --staged)",
		BypassApproval: true,
	})
	if err == nil {
		result["earth_status:middle_kingdom"] = middleKingdomOutput.Output
	}

	// The borderlands: git diff
	borderlandsOutput, err := r.runner.Run(ctx, runners.Input{
		Command:        "git diff",
		Description:    "get earth status: borderlands (git diff)",
		BypassApproval: true,
	})
	if err == nil {
		result["earth_status:borderlands"] = borderlandsOutput.Output
	}

	return result, nil
}

// runBuiltinThen runs a builtin then function (extensible via step registry)
func (r *RitualRunner) runBuiltinThen(ctx context.Context, exec *RitualExecution, fn string) error {
	if exec.EdictID == "" {
		r.logger.Debug("skipping edict operation for system ritual", "fn", fn)
		return nil
	}
	switch fn {
	case "seal_edict":
		return r.db.Model(&storage.Edict{}).
			Where("edict_id = ? AND status = ?", exec.EdictID, storage.EdictActive).
			Update("status", storage.EdictSealed).Error
	case "block_edict":
		return r.db.Model(&storage.Edict{}).
			Where("edict_id = ? AND status = ?", exec.EdictID, storage.EdictActive).
			Update("status", storage.EdictBlocked).Error
	case "unblock_edict":
		return r.db.Model(&storage.Edict{}).
			Where("edict_id = ? AND status = ?", exec.EdictID, storage.EdictBlocked).
			Update("status", storage.EdictActive).Error
	case "request_zhengming":
		// Use the chancellor for zhengming requests, as it's the minister that interacts with the ruler
		// and has a corresponding tab for displaying zhengming questions
		minister := r.getMinister("chancellor")
		if minister == nil {
			return fmt.Errorf("minister not found: chancellor")
		}
		type zhengmingGate interface {
			RequestZhengming(string, storage.ZhengmingQuestions, storage.ZhengmingPriority) (string, error)
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
			Text:    fmt.Sprintf("The %s has completed work on edict %s. Do you approve?", stepName, exec.EdictID),
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
		// Return without blocking - ritual will resume on zhengming_answered event
		// Store request_id in execution data for event handler to check
		if exec.Data == nil {
			exec.Data = storage.JSON{}
		}
		exec.Data["pending_zhengming"] = requestID
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
func (r *RitualRunner) emitEvent(edictID string, eventType storage.ShogunateEvent, payload storage.JSON) {
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
		"edict_id":    exec.EdictID,
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
	// Merge inputs into template data
	if exec.Data != nil {
		if inputs, ok := exec.Data["inputs"].(map[string]interface{}); ok {
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
func (r *RitualRunner) ListExecutions(edictID string) ([]RitualExecution, error) {
	var executions []RitualExecution
	query := r.db.Order("created_at DESC")
	if edictID != "" {
		query = query.Where("edict_id = ?", edictID)
	}
	if err := query.Find(&executions).Error; err != nil {
		return nil, err
	}
	return executions, nil
}
