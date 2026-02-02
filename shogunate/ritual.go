package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

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
steps:
  - name: forge
    minister: forge
    task: |
      Implement the changes for edict {{ .edict_id }}.
      Focus on minimal, targeted changes to fulfill the intent.
    on_failure: retry
    max_retries: 3

  - name: judge
    minister: judge
    task: |
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
steps:
  - name: strategist
    minister: strategist
    task: |
      Analyze edict {{ .edict_id }} and produce a technical Battle Plan.
      Break down the work into clear phases with dependencies.
      Identify risks and architectural decisions.
    on_failure: zhengming

  - name: forge
    minister: forge
    task: |
      Execute the Battle Plan for edict {{ .edict_id }}.
      Implement changes according to the Strategist's design.
    depends_on: [strategist]
    on_failure: retry
    max_retries: 3

  - name: judge
    minister: judge
    task: |
      Run the Trials for edict {{ .edict_id }}.
      Execute all tests and validate the Forge's work.
      If the Judge fails, the Forge must return to the anvil.
    depends_on: [forge]
    on_failure: goto
    on_failure_target: forge

  - name: censor
    minister: censor
    task: |
      Review the implemented code for edict {{ .edict_id }}.
      Verify it adheres to the Imperial Code (project standards).
      Veto if the code violates conventions or introduces risk.
    depends_on: [judge]
    on_failure: goto
    on_failure_target: strategist
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

// RitualStep defines a single step in a ritual
type RitualStep struct {
	Name            string   `yaml:"name"`
	Type            string   `yaml:"type,omitempty"`              // minister, prompt, cmd, gate, confirm (default: minister if minister is set)
	Minister        string   `yaml:"minister,omitempty"`          // For minister steps
	Task            string   `yaml:"task,omitempty"`              // Task description/prompt
	Command         string   `yaml:"command,omitempty"`           // For cmd steps
	Condition       string   `yaml:"condition,omitempty"`         // For gate steps
	DependsOn       []string `yaml:"depends_on,omitempty"`        // Steps that must complete first
	OnFailure       string   `yaml:"on_failure,omitempty"`        // retry, zhengming, goto, abort
	OnFailureTarget string   `yaml:"on_failure_target,omitempty"` // Target step for goto
	MaxRetries      int      `yaml:"max_retries,omitempty"`       // Override default retries
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
		if step.OnFailure == "goto" && step.OnFailureTarget != "" {
			if !stepNames[step.OnFailureTarget] {
				return fmt.Errorf("ritual %q: step %q on_failure_target references unknown step %q", def.Name, step.Name, step.OnFailureTarget)
			}
		}

		// Validate step type and required fields
		stepType := step.Type
		if stepType == "" && step.Minister != "" {
			stepType = "minister"
		}

		switch stepType {
		case "minister", "":
			if step.Minister == "" && step.Task == "" {
				return fmt.Errorf("ritual %q: step %q requires minister or task", def.Name, step.Name)
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
			if step.Task == "" {
				return fmt.Errorf("ritual %q: confirm step %q requires task (question)", def.Name, step.Name)
			}
		case "prompt":
			if step.Task == "" {
				return fmt.Errorf("ritual %q: prompt step %q requires task (prompt text)", def.Name, step.Name)
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
	notify     NotifyFunc
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
func (r *RitualRunner) Start(ctx context.Context, ritualName, edictID string, inputs map[string]string, notify NotifyFunc) (*RitualExecution, error) {
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

			// Handle failure action
			if !r.handleFailure(ctx, exec, step, err) {
				exec.State = RitualStateFailed
				r.saveExecution(exec)
				return err
			}
			continue
		}

		// Update step state
		exec.stepStates[exec.CurrentStep].Message = result

		// Move to next step (considering dependencies)
		exec.CurrentStep++
		r.saveExecution(exec)
	}

	exec.State = RitualStateCompleted
	r.saveExecution(exec)

	r.logger.Info("ritual completed",
		"ritual", exec.RitualName,
		"execution_id", exec.ID,
		"edict_id", exec.EdictID)

	return nil
}

// executeStep runs a single ritual step
func (r *RitualRunner) executeStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	r.saveExecution(exec)

	r.logger.Debug("executing ritual step",
		"ritual", exec.RitualName,
		"step", step.Name,
		"type", step.Type)

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

	// Determine step type
	stepType := step.Type
	if stepType == "" && step.Minister != "" {
		stepType = "minister"
	}

	switch stepType {
	case "minister", "":
		return r.executeMinisterStep(ctx, exec, step)
	case "prompt":
		return r.executePromptStep(ctx, exec, step)
	case "cmd":
		return r.executeCmdStep(ctx, exec, step)
	case "gate":
		return r.executeGateStep(ctx, exec, step)
	case "confirm":
		return r.executeConfirmStep(ctx, exec, step)
	default:
		return "", fmt.Errorf("unknown step type: %s", stepType)
	}
}

// executeMinisterStep invokes a minister for a task
func (r *RitualRunner) executeMinisterStep(ctx context.Context, exec *RitualExecution, step RitualStep) (string, error) {
	minister := r.shogunate.GetMinister(step.Minister)
	if minister == nil {
		return "", fmt.Errorf("minister not found: %s", step.Minister)
	}

	// Expand template in task
	task := r.expandTemplate(step.Task, exec)

	// Create task envelope
	replyChan := make(chan *TaskReply, 1)
	env := &TaskEnvelope{
		EdictID:   exec.EdictID,
		Task:      task,
		ReplyChan: replyChan,
	}

	// Send task to minister
	select {
	case minister.Tasks() <- env:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Wait for reply
	select {
	case reply := <-replyChan:
		if reply.Error != nil {
			return "", reply.Error
		}
		return reply.Output, nil
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

	prompt := r.expandTemplate(step.Task, exec)
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

// handleFailure handles step failure based on on_failure action
func (r *RitualRunner) handleFailure(ctx context.Context, exec *RitualExecution, step RitualStep, err error) bool {
	action := OnFailureAction(step.OnFailure)
	if action == "" {
		action = OnFailureAbort
	}

	switch action {
	case OnFailureRetry:
		maxRetries := step.MaxRetries
		if maxRetries == 0 {
			maxRetries = r.maxRetries
		}
		if exec.stepStates[exec.CurrentStep].RetryCount < maxRetries {
			exec.stepStates[exec.CurrentStep].RetryCount++
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

// expandTemplate expands Go template syntax in a string
func (r *RitualRunner) expandTemplate(text string, exec *RitualExecution) string {
	// Simple variable expansion for now (could use text/template)
	// Replace {{ .edict_id }} with actual value
	if exec.Data != nil {
		if inputs, ok := exec.Data["inputs"].(map[string]interface{}); ok {
			for k, v := range inputs {
				placeholder := fmt.Sprintf("{{ .%s }}", k)
				if str, ok := v.(string); ok {
					text = replaceAll(text, placeholder, str)
				}
			}
		}
	}
	text = replaceAll(text, "{{ .edict_id }}", exec.EdictID)
	return text
}

// replaceAll replaces all occurrences of old with new in s
func replaceAll(s, old, new string) string {
	for {
		idx := findString(s, old)
		if idx == -1 {
			return s
		}
		s = s[:idx] + new + s[idx+len(old):]
	}
}

// findString finds the first occurrence of substr in s
func findString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
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
