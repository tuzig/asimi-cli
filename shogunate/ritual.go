package shogunate

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"text/template"
	"time"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// Embedded basic rituals - loaded by default
var embeddedRituals = map[string]string{
	"swift-strike": `
name: swift-strike
description: "The Swift Strike (S) - A tight loop of creation and validation"
triggers:
  - manual: true
inputs:
  edict_id:
    type: string
    required: true
max_retries: 3
steps:
  - name: forge
    minister: forge
    arrange: [get_edict]
    act: |
      Implement the changes for edict {{ .edict_id }}.
      Focus on minimal, targeted changes to fulfill the intent.
    on_failure: retry

  - name: judge
    minister: judge
    arrange: [get_edict, get_manifests]
    act: |
      Run tests and validate the changes for edict {{ .edict_id }}.
      If tests fail, provide clear feedback for the Forge.
    depends_on: [forge]
    on_failure: goto
    on_failure_target: forge
`,

	"grand-campaign": `
name: grand-campaign
description: "The Grand Campaign (L) - Architecture-first with strict gatekeeping"
triggers:
  - manual: true
inputs:
  edict_id:
    type: string
    required: true
max_retries: 3
steps:
  - name: strategist
    minister: strategist
    arrange: [get_edict]
    act: |
      Analyze edict {{ .edict_id }} and produce a technical Battle Plan.
      Break down the work into clear phases with dependencies.
      Identify risks and architectural decisions.
    on_failure: zhengming

  - name: forge
    minister: forge
    arrange: [get_edict, get_manifests]
    act: |
      Execute the Battle Plan for edict {{ .edict_id }}.
      Implement changes according to the Strategist's design.
    depends_on: [strategist]
    on_failure: retry

  - name: judge
    minister: judge
    arrange: [get_edict, get_manifests]
    act: |
      Run the Trials for edict {{ .edict_id }}.
      Execute all tests and validate the Forge's work.
      If the Judge fails, the Forge must return to the anvil.
    depends_on: [forge]
    on_failure: goto
    on_failure_target: forge

  - name: censor
    minister: censor
    arrange: [get_edict, get_manifests, get_verdicts]
    act: |
      Review the implemented code for edict {{ .edict_id }}.
      Verify it adheres to the Imperial Code (project standards).
      Veto if the code violates conventions or introduces risk.
    depends_on: [judge]
    on_failure: goto
    on_failure_target: strategist
`,

	"wakeup": `
name: wakeup
description: "Startup ritual - report court status on Shogunate boot"
triggers:
  - event: shogunate_started
steps:
  - name: report
    type: prompt
    arrange: [get_court_status]
    act: |
      The Shogunate has awoken. Report the current court status.
      List any active edicts, their phases, and any pending zhengming.
`,

	"grand-orchestration": `
name: grand-orchestration
description: "Full lifecycle orchestration - strategize, forge, judge, censor, deploy"
triggers:
  - manual: true
inputs:
  edict_id:
    type: string
    required: true
max_retries: 3
steps:
  - name: strategist
    minister: strategist
    arrange: [get_edict]
    act: |
      Produce a comprehensive Battle Plan for edict {{ .edict_id }}.
      Decompose into lings with dependencies.
    on_failure: zhengming

  - name: forge
    minister: forge
    arrange: [get_edict, get_manifests]
    act: |
      Execute all lings for edict {{ .edict_id }}.
      Implement each change and stage manifests.
    depends_on: [strategist]
    on_failure: retry

  - name: judge
    minister: judge
    arrange: [get_edict, get_manifests]
    act: |
      Run the full trial suite for edict {{ .edict_id }}.
    depends_on: [forge]
    on_failure: goto
    on_failure_target: forge

  - name: censor
    minister: censor
    arrange: [get_edict, get_manifests, get_verdicts]
    act: |
      Full ethics and standards review for edict {{ .edict_id }}.
    depends_on: [judge]
    on_failure: goto
    on_failure_target: strategist

  - name: deploy
    minister: marshal
    arrange: [get_edict, get_manifests, get_verdicts, get_precedents]
    act: |
      Prepare deployment for edict {{ .edict_id }}.
      Verify all seals are in place before proceeding.
    depends_on: [censor]
    on_failure: zhengming
`,

	"report_failure": `
name: report_failure
description: "Report ritual failure after retry exhaustion"
triggers:
  - event: ritual_failed
steps:
  - name: report
    type: prompt
    arrange: [get_edict, get_court_status]
    act: |
      A ritual has failed after exhausting all retries for edict {{ .edict_id }}.
      Analyze the failure and suggest next steps.
      Consider requesting zhengming if the path forward is unclear.
`,
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
	Arrange     []string            `yaml:"arrange,omitempty"`     // Ritual-level arrange functions (run before first step)
	OnFailure   string              `yaml:"on_failure,omitempty"`  // Default on_failure for all steps
	MaxRetries  int                 `yaml:"max_retries,omitempty"` // Default max_retries for all steps
	Steps       []RitualStep        `yaml:"steps"`
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

// RitualStep defines a single step in a ritual (AAA model: Arrange → Act → Assert)
type RitualStep struct {
	Name            string   `yaml:"name"`
	Type            string   `yaml:"type,omitempty"`              // minister, prompt, cmd, gate, confirm (default: minister if minister is set)
	Minister        string   `yaml:"minister,omitempty"`          // For minister steps
	Arrange         []string `yaml:"arrange,omitempty"`           // Builtin arrange functions to run before act
	Act             string   `yaml:"act,omitempty"`               // The action: task text, command, or prompt
	Assert          string   `yaml:"assert,omitempty"`            // Post-act validation command
	Task            string   `yaml:"task,omitempty"`              // Alias for Act (backward compat)
	Command         string   `yaml:"command,omitempty"`           // For cmd steps
	Condition       string   `yaml:"condition,omitempty"`         // For gate steps
	DependsOn       []string `yaml:"depends_on,omitempty"`        // Steps that must complete first
	OnFailure       string   `yaml:"on_failure,omitempty"`        // retry, zhengming, goto, abort
	OnFailureTarget string   `yaml:"on_failure_target,omitempty"` // Target step for goto
	MaxRetries      int      `yaml:"max_retries,omitempty"`       // Override default retries
	Scope           string   `yaml:"scope,omitempty"`             // Execution scope (e.g., "edict", "global")
	Model           string   `yaml:"model,omitempty"`             // LLM model override for this step
	Temperature     float64  `yaml:"temperature,omitempty"`       // LLM temperature override
	Env             map[string]string `yaml:"env,omitempty"`      // Environment variables for this step
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

		// Validate step type and required fields
		stepType := step.Type
		if stepType == "" && step.Minister != "" {
			stepType = "minister"
		}

		// act resolves Act or Task (backward compat)
		hasAction := step.Act != "" || step.Task != ""

		switch stepType {
		case "minister", "":
			if step.Minister == "" && !hasAction {
				return fmt.Errorf("ritual %q: step %q requires minister or act/task", def.Name, step.Name)
			}
		case "cmd":
			if step.Command == "" {
				return fmt.Errorf("ritual %q: cmd step %q requires command", def.Name, step.Name)
			}
		case "gate":
			if step.Condition == "" {
				return fmt.Errorf("ritual %q: gate step %q requires condition", def.Name, step.Name)
			}
		case "confirm":
			if !hasAction {
				return fmt.Errorf("ritual %q: confirm step %q requires act/task (question)", def.Name, step.Name)
			}
		case "prompt":
			if !hasAction {
				return fmt.Errorf("ritual %q: prompt step %q requires act/task (prompt text)", def.Name, step.Name)
			}
		default:
			return fmt.Errorf("ritual %q: step %q has unknown type %q", def.Name, step.Name, stepType)
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

// LoadEmbeddedRituals loads the built-in default rituals
func LoadEmbeddedRituals() ([]*RitualDef, error) {
	var rituals []*RitualDef

	for name, content := range embeddedRituals {
		ritual, err := ParseRitual([]byte(content))
		if err != nil {
			return nil, fmt.Errorf("failed to parse embedded ritual %s: %w", name, err)
		}

		if err := ValidateRitual(ritual); err != nil {
			return nil, fmt.Errorf("invalid embedded ritual %s: %w", name, err)
		}

		rituals = append(rituals, ritual)
	}

	return rituals, nil
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
	registry   *RitualRegistry
	shogunate  *Shogunate
	db         *gorm.DB
	runner     runners.Runner
	logger     *slog.Logger
	maxRetries int
}

// NewRitualRunner creates a new ritual runner
func NewRitualRunner(registry *RitualRegistry, shogunate *Shogunate, db *gorm.DB, runner runners.Runner, logger *slog.Logger) *RitualRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &RitualRunner{
		registry:   registry,
		shogunate:  shogunate,
		db:         db,
		runner:     runner,
		logger:     logger,
		maxRetries: 3,
	}
}

// RitualExecution tracks a running ritual instance
type RitualExecution struct {
	ID          string       `gorm:"primaryKey;column:id"`
	RitualName  string       `gorm:"column:ritual_name"`
	EdictID     string       `gorm:"column:edict_id;index"`
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
	RetryCount  int    `gorm:"column:retry_count"`
	Message     string `gorm:"column:message"`
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

			// Notify: step failed
			if exec.notify != nil {
				exec.notify(RitualStepMsg{
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

			// Handle failure action
			if !r.handleFailure(ctx, exec, step, err) {
				exec.State = RitualStateFailed
				r.saveExecution(exec)
				return err
			}
			continue
		}

		// Notify: step completed
		if exec.notify != nil {
			exec.notify(RitualStepMsg{
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

		// Update step state
		exec.stepStates[exec.CurrentStep].Message = result

		// Move to next step (considering dependencies)
		exec.CurrentStep++
		r.saveExecution(exec)
	}

	exec.State = RitualStateCompleted
	r.saveExecution(exec)

	// Notify: ritual completed
	if exec.notify != nil {
		exec.notify(RitualStepMsg{
			RitualName:  exec.RitualName,
			ExecutionID: exec.ID,
			EdictID:     exec.EdictID,
			StepName:    "",
			StepIndex:   len(exec.def.Steps),
			TotalSteps:  len(exec.def.Steps),
			Status:      "ritual_completed",
		})
	}

	r.logger.Info("ritual completed",
		"ritual", exec.RitualName,
		"execution_id", exec.ID,
		"edict_id", exec.EdictID)

	return nil
}

// resolveAct returns the action text for a step, preferring Act over Task (backward compat)
func (step *RitualStep) resolveAct() string {
	if step.Act != "" {
		return step.Act
	}
	return step.Task
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

// executeStep runs a single ritual step using the AAA model: Arrange → Act → Assert
func (r *RitualRunner) executeStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	r.saveExecution(exec)

	r.logger.Debug("executing ritual step",
		"ritual", exec.RitualName,
		"step", step.Name,
		"type", step.Type)

	// Notify: step started
	if exec.notify != nil {
		exec.notify(RitualStepMsg{
			RitualName:  exec.RitualName,
			ExecutionID: exec.ID,
			EdictID:     exec.EdictID,
			StepName:    step.Name,
			StepIndex:   exec.CurrentStep,
			TotalSteps:  len(exec.def.Steps),
			Status:      "started",
		})
	}

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

	// === ARRANGE ===
	if len(step.Arrange) > 0 {
		for _, fn := range step.Arrange {
			result, err := r.runArrangeFunc(ctx, exec, fn)
			if err != nil {
				return "", fmt.Errorf("arrange %q failed: %w", fn, err)
			}
			// Store arrange result in execution data for template use
			if exec.Data == nil {
				exec.Data = storage.JSON{}
			}
			arrangeCtx, _ := exec.Data["arrange_context"].(map[string]interface{})
			if arrangeCtx == nil {
				arrangeCtx = make(map[string]interface{})
			}
			arrangeCtx[fn] = result
			exec.Data["arrange_context"] = arrangeCtx
		}
	}

	// === ACT ===
	stepType := step.Type
	if stepType == "" && step.Minister != "" {
		stepType = "minister"
	}

	var actResult string
	var err error
	switch stepType {
	case "minister", "":
		actResult, err = r.executeMinisterStep(ctx, exec, step)
	case "prompt":
		actResult, err = r.executePromptStep(ctx, exec, step)
	case "cmd":
		actResult, err = r.executeCmdStep(ctx, exec, step)
	case "gate":
		actResult, err = r.executeGateStep(ctx, exec, step)
	case "confirm":
		actResult, err = r.executeConfirmStep(ctx, exec, step)
	default:
		return "", fmt.Errorf("unknown step type: %s", stepType)
	}
	if err != nil {
		return "", err
	}

	// === ASSERT ===
	if step.Assert != "" {
		assertCmd := r.expandTemplate(step.Assert, exec)
		if err := r.runAssert(ctx, exec, assertCmd); err != nil {
			return "", fmt.Errorf("assert failed: %w", err)
		}
	}

	return actResult, nil
}

// executeMinisterStep invokes a minister for a task
func (r *RitualRunner) executeMinisterStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	minister := r.shogunate.GetMinister(step.Minister)
	if minister == nil {
		return "", fmt.Errorf("minister not found: %s", step.Minister)
	}

	// Expand template in work (Act or Task for backward compat)
	work := r.expandTemplate(step.resolveAct(), exec)

	// Create task
	doneChan := make(chan Result, 1)
	t := &Task{
		EdictID: exec.EdictID,
		Work:    work,
		Done:    doneChan,
	}

	// Send task to minister
	select {
	case minister.Tasks() <- t:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Wait for result
	select {
	case result := <-doneChan:
		if result.Err != nil {
			return "", result.Err
		}
		return result.Output, nil
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("minister %s timeout", step.Minister)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// executePromptStep sends a prompt to the LLM
func (r *RitualRunner) executePromptStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	// Get chancellor's session for LLM access
	chancellor := r.shogunate.GetMinister("chancellor")
	if chancellor == nil {
		return "", fmt.Errorf("chancellor not available")
	}

	chanc, ok := chancellor.(*Chancellor)
	if !ok {
		return "", fmt.Errorf("invalid chancellor type")
	}

	sess := chanc.GetSession(exec.EdictID)
	if sess == nil {
		return "", fmt.Errorf("no session for edict %s", exec.EdictID)
	}

	prompt := r.expandTemplate(step.resolveAct(), exec)
	response, err := sess.AskWithStreaming(ctx, prompt, nil)
	if err != nil {
		return "", err
	}

	return response, nil
}

// executeCmdStep runs a shell command
func (r *RitualRunner) executeCmdStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	if r.runner == nil {
		return "", fmt.Errorf("no runner configured for cmd steps")
	}

	command := r.expandTemplate(step.Command, exec)
	output, err := r.runner.Run(ctx, runners.Input{
		Command:        command,
		Description:    fmt.Sprintf("ritual %s step %s", exec.RitualName, step.Name),
		BypassApproval: true,
	})
	if err != nil {
		return "", err
	}
	if output.ExitCode != "0" {
		return "", fmt.Errorf("exit code %s: %s", output.ExitCode, output.Output)
	}
	return output.Output, nil
}

// executeGateStep waits for a condition
func (r *RitualRunner) executeGateStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	// Gate conditions would be evaluated here
	// For now, just pass through
	return "gate passed", nil
}

// executeConfirmStep requires user confirmation
func (r *RitualRunner) executeConfirmStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	// This would integrate with the TUI for user confirmation
	// For now, auto-approve
	r.logger.Info("confirm step auto-approved (not implemented)", "step", step.Name)
	return "confirmed", nil
}

// runArrangeFunc runs a builtin arrange function and returns the result
func (r *RitualRunner) runArrangeFunc(ctx context.Context, exec *RitualExecution, fn string) (interface{}, error) {
	switch fn {
	case "get_edict":
		return r.arrangeGetEdict(exec.EdictID)
	case "get_court_status":
		return r.arrangeGetCourtStatus(exec.EdictID)
	case "get_manifests":
		return r.arrangeGetManifests(exec.EdictID)
	case "get_verdicts":
		return r.arrangeGetVerdicts(exec.EdictID)
	case "get_precedents":
		return r.arrangeGetPrecedents(exec.EdictID)
	default:
		return nil, fmt.Errorf("unknown arrange function: %s", fn)
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
		"phase":    string(edict.CurrentPhase),
		"halted":   edict.Halted,
	}, nil
}

func (r *RitualRunner) arrangeGetCourtStatus(edictID string) (interface{}, error) {
	var edicts []storage.Edict
	if err := r.db.Where("current_phase NOT IN ?", []string{"sealed", "cancelled"}).Find(&edicts).Error; err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, len(edicts))
	for i, e := range edicts {
		result[i] = map[string]interface{}{
			"edict_id": e.EdictID,
			"phase":    string(e.CurrentPhase),
			"halted":   e.Halted,
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
			"file_path":   m.FilePath,
			"status":      string(m.Status),
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

// runAssert runs an assertion command and returns error if it fails
func (r *RitualRunner) runAssert(ctx context.Context, exec *RitualExecution, assertCmd string) error {
	if r.runner == nil {
		return fmt.Errorf("no runner configured for assert")
	}
	output, err := r.runner.Run(ctx, runners.Input{
		Command:        assertCmd,
		Description:    fmt.Sprintf("assert: ritual %s", exec.RitualName),
		BypassApproval: true,
	})
	if err != nil {
		return err
	}
	if output.ExitCode != "0" {
		return fmt.Errorf("assertion failed (exit %s): %s", output.ExitCode, output.Output)
	}
	return nil
}

// handleFailure handles step failure based on on_failure action
func (r *RitualRunner) handleFailure(ctx context.Context, exec *RitualExecution, step RitualStep, err error) bool {
	action := OnFailureAction(step.resolveOnFailure(exec.def))
	if action == "" {
		action = OnFailureAbort
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
	// Merge arrange context into template data
	if exec.Data != nil {
		if arrangeCtx, ok := exec.Data["arrange_context"].(map[string]interface{}); ok {
			for k, v := range arrangeCtx {
				data[k] = v
			}
		}
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
