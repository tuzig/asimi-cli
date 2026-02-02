package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"text/template"
	"time"

	"github.com/afittestide/asimi/storage"
)

// StepResult represents the result of a step's verify function
type StepResult struct {
	NextStep   string // Target step name (empty = use NextOffset)
	NextOffset int    // +1 proceed, 0 retry, -n go back, +n skip (used when NextStep is empty)
	Message    string // Status message for the step
}

// StepState represents the runtime state of a step
type StepState struct {
	Name       string
	RetryCount int
	Message    string
}

// Step represents a single step in a workflow
type Step struct {
	Name    string
	Prepare func(w *Workflow) (map[string]interface{}, error) // Returns data for template
	Prompt  string                                            // Go template, empty = skip AI
	Verify  func(w *Workflow, response string) StepResult
}

// Workflow represents a multi-step AI-driven operation
type Workflow struct {
	ID          string
	Name        string
	Steps       []Step
	StepStates  []StepState
	CurrentStep int
	State       storage.WorkflowState
	MaxRetries  int                    // Per-step limit
	Data        map[string]interface{} // Accumulated state (flexible types)
	BranchID    int64
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// Runtime (not persisted)
	aborted      bool
	mu           sync.Mutex
	db           *storage.DB
	store        *storage.WorkflowStore
	host         string
	org          string
	project      string
	branch       string
	sendPrompt   func(ctx context.Context, prompt string) <-chan string
	onProgress   func(stepIndex int, stepState StepState, message string)
	requestYesNo func(question string) <-chan bool
}

// Option is a functional option for configuring a workflow
type Option func(*Workflow)

// WithMaxRetries sets the maximum retries per step
func WithMaxRetries(maxRetries int) Option {
	return func(w *Workflow) {
		w.MaxRetries = maxRetries
	}
}

// WithSendPrompt sets the function to send prompts to the AI
func WithSendPrompt(fn func(ctx context.Context, prompt string) <-chan string) Option {
	return func(w *Workflow) {
		w.sendPrompt = fn
	}
}

// WithOnProgress sets the progress callback for step state changes and ad-hoc messages
// The message parameter is non-empty for ad-hoc progress messages (via ReportProgress)
// and empty for step state change notifications
func WithOnProgress(fn func(stepIndex int, stepState StepState, message string)) Option {
	return func(w *Workflow) {
		w.onProgress = fn
	}
}

// WithRequestYesNo sets the function to request yes/no approval
func WithRequestYesNo(fn func(question string) <-chan bool) Option {
	return func(w *Workflow) {
		w.requestYesNo = fn
	}
}

// SetOnProgress sets the progress callback after workflow creation
func (w *Workflow) SetOnProgress(fn func(stepIndex int, stepState StepState, message string)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onProgress = fn
}

// SetSendPrompt sets the send prompt function after workflow creation
func (w *Workflow) SetSendPrompt(fn func(ctx context.Context, prompt string) <-chan string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sendPrompt = fn
}

// SetRequestYesNo sets the request yes/no function after workflow creation
func (w *Workflow) SetRequestYesNo(fn func(question string) <-chan bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.requestYesNo = fn
}

// RepoContext contains repository information needed by workflows
type RepoContext struct {
	Host    string
	Org     string
	Project string
	Branch  string
}

// IDGenerator is a function that generates unique IDs
type IDGenerator func() string

// defaultIDGenerator generates a simple timestamp-based ID
func defaultIDGenerator() string {
	return fmt.Sprintf("wf-%d", time.Now().UnixNano())
}

var idGenerator IDGenerator = defaultIDGenerator

// SetIDGenerator sets the ID generator function (for integration with main package)
func SetIDGenerator(gen IDGenerator) {
	idGenerator = gen
}

// New creates a new workflow
func New(name string, db *storage.DB, repoCtx RepoContext, opts ...Option) *Workflow {
	w := &Workflow{
		ID:          idGenerator(),
		Name:        name,
		Steps:       []Step{},
		StepStates:  []StepState{},
		CurrentStep: 0,
		State:       storage.WorkflowStatePending,
		MaxRetries:  3,
		Data:        make(map[string]interface{}),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		db:          db,
		host:        repoCtx.Host,
		org:         repoCtx.Org,
		project:     repoCtx.Project,
		branch:      repoCtx.Branch,
		onProgress:  func(int, StepState, string) {},
	}

	if db != nil {
		w.store = storage.NewWorkflowStore(db)
	}

	for _, opt := range opts {
		opt(w)
	}

	return w
}

// Add adds a step to the workflow and returns the workflow for chaining
func (w *Workflow) Add(step Step) *Workflow {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.Steps = append(w.Steps, step)
	w.StepStates = append(w.StepStates, StepState{
		Name:       step.Name,
		RetryCount: 0,
		Message:    "",
	})
	return w
}

// AddStep is an alias for Add (deprecated, use Add for chaining)
func (w *Workflow) AddStep(step Step) *Workflow {
	return w.Add(step)
}

// Common step helpers for reducing boilerplate
// All helpers return *Workflow for method chaining

// AddPrompt adds a simple step that sends a prompt and proceeds on any response
func (w *Workflow) AddPrompt(name, prompt string) *Workflow {
	return w.Add(Step{
		Name:   name,
		Prompt: prompt,
		Verify: func(w *Workflow, response string) StepResult {
			return w.Next("✓ Completed")
		},
	})
}

// AddCmd adds a step that runs a shell command and optionally stores the output
// If storeAs is non-empty, the command output is stored in workflow data under that key
func (w *Workflow) AddCmd(name, command, storeAs string) *Workflow {
	return w.Add(Step{
		Name: name,
		Verify: func(w *Workflow, response string) StepResult {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			var output string
			var exitCode string

			runner := getShellRunner()
			if runner != nil {
				result, err := runner.Run(ctx, RunShellCommandInput{Command: command, Description: name})
				if err != nil {
					return w.Retry(fmt.Sprintf("❌ Command failed: %v", err))
				}
				output = result.Output
				exitCode = result.ExitCode
			} else {
				// Fallback to hostRun for safe host execution
				result, err := hostRun(ctx, RunShellCommandInput{Command: command, Description: name})
				if err != nil {
					return w.Retry(fmt.Sprintf("❌ Command failed: %v", err))
				}
				output = result.Output
				exitCode = result.ExitCode
			}

			if exitCode != "0" {
				return w.Retry(fmt.Sprintf("❌ Command failed (exit code: %s): %s", exitCode, output))
			}
			if storeAs != "" {
				w.Set(storeAs, output)
			}
			return w.Next("✓ Command succeeded")
		},
	})
}

// AddGate adds a step that blocks until a condition is met
// The condition function is called repeatedly until it returns true
func (w *Workflow) AddGate(name string, condition func(w *Workflow) bool, waitMessage string) *Workflow {
	return w.Add(Step{
		Name: name,
		Verify: func(w *Workflow, response string) StepResult {
			if condition(w) {
				return w.Next("✓ Condition met")
			}
			return w.Retry(waitMessage)
		},
	})
}

// AddConfirm adds a step that requires user confirmation to proceed
// If the user declines, the step is marked as skipped and workflow continues
func (w *Workflow) AddConfirm(name, question string) *Workflow {
	return w.Add(Step{
		Name: name,
		Verify: func(w *Workflow, response string) StepResult {
			if w.RequestApproval(question) {
				return w.Next("✓ Approved")
			}
			return w.Next("⊘ Skipped by user")
		},
	})
}

// AddIf adds a step that only executes when the condition is true
// If the condition is false, the step is skipped
func (w *Workflow) AddIf(condition func(w *Workflow) bool, step Step) *Workflow {
	originalVerify := step.Verify
	step.Verify = func(w *Workflow, response string) StepResult {
		if !condition(w) {
			return w.Next("⊘ Skipped (condition not met)")
		}
		if originalVerify != nil {
			return originalVerify(w, response)
		}
		return w.Next("✓ Completed")
	}
	return w.Add(step)
}

// AddRun adds a step that executes a function and proceeds on success
// The function should return an error message on failure, or empty string on success
func (w *Workflow) AddRun(name string, fn func(w *Workflow) error) *Workflow {
	return w.Add(Step{
		Name: name,
		Verify: func(w *Workflow, response string) StepResult {
			if err := fn(w); err != nil {
				return w.Retry(fmt.Sprintf("❌ %v", err))
			}
			return w.Next("✓ Completed")
		},
	})
}

// AddCheck adds a step with only a verify function
func (w *Workflow) AddCheck(name string, check func(w *Workflow) StepResult) *Workflow {
	return w.Add(Step{
		Name:   name,
		Verify: func(w *Workflow, response string) StepResult { return check(w) },
	})
}

// stepIndex returns the index of a step by name, or -1 if not found
func (w *Workflow) stepIndex(name string) int {
	for i, step := range w.Steps {
		if step.Name == name {
			return i
		}
	}
	return -1
}

// Set stores a key-value pair in the workflow data
func (w *Workflow) Set(key string, value interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Data[key] = value
}

// Get retrieves a string value from the workflow data
func (w *Workflow) Get(key string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if v, ok := w.Data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetValue retrieves any value from the workflow data
func (w *Workflow) GetValue(key string) interface{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Data[key]
}

// ReportProgress sends an ad-hoc progress message during step execution
func (w *Workflow) ReportProgress(message string) {
	w.onProgress(w.CurrentStep, w.StepStates[w.CurrentStep], message)
}

// Retry retries the current step with the given message and notifies the user.
// For prompt steps, this re-sends the prompt. For non-prompt steps (gates, checks),
// this re-executes the verify function.
func (w *Workflow) Retry(message string) StepResult {
	// Check if CurrentStep is out of bounds
	if w.CurrentStep >= len(w.Steps) {
		w.Abort()
		return StepResult{NextOffset: 0, Message: "Workflow aborted: invalid step index"}
	}

	w.ReportProgress(message)
	return StepResult{NextOffset: 0, Message: message}
}

// Back goes back one step with the given message and notifies the user
// Deprecated: Use GoTo with explicit step name for clarity
func (w *Workflow) Back(message string) StepResult {
	w.ReportProgress(message)
	return StepResult{NextOffset: -1, Message: message}
}

// BackN goes back n steps with the given message and notifies the user
// Deprecated: Use GoTo with explicit step name for clarity
func (w *Workflow) BackN(n int, message string) StepResult {
	if n < 1 {
		n = 1
	}
	w.ReportProgress(message)
	return StepResult{NextOffset: -n, Message: message}
}

// Skip skips forward n steps with the given message and notifies the user
func (w *Workflow) Skip(n int, message string) StepResult {
	if n < 1 {
		n = 1
	}
	w.ReportProgress(message)
	return StepResult{NextOffset: n, Message: message}
}

// Next proceeds to the next step with the given message and notifies the user
func (w *Workflow) Next(message string) StepResult {
	w.ReportProgress(message)
	return StepResult{NextOffset: 1, Message: message}
}

// GoTo jumps to a specific step by name with the given message and notifies the user
func (w *Workflow) GoTo(stepName, message string) StepResult {
	w.ReportProgress(message)
	return StepResult{NextStep: stepName, Message: message}
}

// RequestApproval blocks and waits for user approval
func (w *Workflow) RequestApproval(question string) bool {
	if w.requestYesNo == nil {
		slog.Warn("RequestApproval called but requestYesNo is nil, defaulting to true")
		return true
	}

	responseChan := w.requestYesNo(question)
	return <-responseChan
}

// Abort signals the workflow to stop
func (w *Workflow) Abort() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.aborted = true
	w.State = storage.WorkflowStateAborted
}

// IsAborted returns whether the workflow has been aborted
func (w *Workflow) IsAborted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.aborted
}

// Save persists the workflow state to the database
func (w *Workflow) Save() error {
	if w.store == nil {
		return nil // No persistence configured
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Marshal data to JSON (interface{} values will be serialized)
	dataJSON, err := json.Marshal(w.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal workflow data: %w", err)
	}

	workflowData := &storage.WorkflowData{
		ID:          w.ID,
		BranchID:    w.BranchID,
		Name:        w.Name,
		CurrentStep: w.CurrentStep,
		State:       w.State,
		MaxRetries:  w.MaxRetries,
		Data:        string(dataJSON),
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   time.Now(),
	}

	if err := w.store.SaveWorkflow(workflowData, w.host, w.org, w.project, w.branch); err != nil {
		return err
	}

	// Save step states
	for i, state := range w.StepStates {
		prepareDataJSON := "{}"
		if i < len(w.Steps) && w.Steps[i].Prepare != nil {
			// We don't persist prepare data here, it's computed at runtime
		}

		stepData := &storage.WorkflowStepData{
			WorkflowID:     w.ID,
			StepIndex:      i,
			Name:           state.Name,
			RetryCount:     state.RetryCount,
			Message:        state.Message,
			PromptTemplate: "",
			PrepareData:    prepareDataJSON,
		}

		if i < len(w.Steps) {
			stepData.PromptTemplate = w.Steps[i].Prompt
		}

		if err := w.store.SaveWorkflowStep(stepData); err != nil {
			return err
		}
	}

	return nil
}

// Run executes the workflow
func (w *Workflow) Run(ctx context.Context) error {
	w.mu.Lock()
	w.State = storage.WorkflowStateRunning
	w.mu.Unlock()

	if err := w.Save(); err != nil {
		slog.Warn("Failed to save workflow state", "error", err)
	}

	for w.CurrentStep < len(w.Steps) {
		// Check for abort
		if w.IsAborted() {
			return fmt.Errorf("workflow aborted")
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			w.mu.Lock()
			w.State = storage.WorkflowStateAborted
			w.mu.Unlock()
			w.Save()
			return ctx.Err()
		default:
		}

		step := w.Steps[w.CurrentStep]
		result, err := w.executeStep(ctx, w.CurrentStep, step)
		if err != nil {
			w.mu.Lock()
			w.StepStates[w.CurrentStep].Message = err.Error()
			w.State = storage.WorkflowStateFailed
			w.mu.Unlock()
			w.Save()
			return err
		}

		// Handle step result
		w.mu.Lock()
		w.StepStates[w.CurrentStep].Message = result.Message

		// Resolve NextStep name to offset if specified
		nextOffset := result.NextOffset
		if result.NextStep != "" {
			targetIndex := w.stepIndex(result.NextStep)
			if targetIndex == -1 {
				w.State = storage.WorkflowStateFailed
				w.mu.Unlock()
				w.Save()
				return fmt.Errorf("step %q: target step %q not found", step.Name, result.NextStep)
			}
			nextOffset = targetIndex - w.CurrentStep
		}

		switch {
		case nextOffset > 0:
			// Proceed or skip
			w.CurrentStep += nextOffset
		case nextOffset == 0:
			// Retry
			w.StepStates[w.CurrentStep].RetryCount++
			if w.StepStates[w.CurrentStep].RetryCount >= w.MaxRetries {
				w.State = storage.WorkflowStateFailed
				w.mu.Unlock()
				w.Save()
				return fmt.Errorf("step %q exceeded max retries (%d)", step.Name, w.MaxRetries)
			}
			// Stay on current step for retry
		case nextOffset < 0:
			// Go back
			newStep := w.CurrentStep + nextOffset
			if newStep < 0 {
				w.State = storage.WorkflowStateFailed
				w.mu.Unlock()
				w.Save()
				return fmt.Errorf("step %q: cannot go back %d steps from step %d", step.Name, -nextOffset, w.CurrentStep)
			}
			w.CurrentStep = newStep
		}
		w.mu.Unlock()

		// Notify progress (only if we're still within bounds)
		if w.CurrentStep < len(w.StepStates) {
			w.onProgress(w.CurrentStep, w.StepStates[w.CurrentStep], "")
		}

		if err := w.Save(); err != nil {
			slog.Warn("Failed to save workflow state", "error", err)
		}
	}

	w.mu.Lock()
	w.State = storage.WorkflowStateCompleted
	w.mu.Unlock()
	w.Save()

	return nil
}

// executeStep runs a single step
func (w *Workflow) executeStep(ctx context.Context, stepIndex int, step Step) (StepResult, error) {
	slog.Debug("Executing workflow step", "step", step.Name, "index", stepIndex)

	// Notify progress
	w.onProgress(stepIndex, w.StepStates[stepIndex], "")

	// Run prepare function if present
	var prepareData map[string]interface{}
	if step.Prepare != nil {
		var err error
		prepareData, err = step.Prepare(w)
		if err != nil {
			return StepResult{NextOffset: 0, Message: fmt.Sprintf("prepare failed: %v", err)}, err
		}

		// Merge prepare data into workflow data
		for k, v := range prepareData {
			w.Set(k, v)
		}
	}

	// If there's a prompt, send it to the AI
	var response string
	if step.Prompt != "" {
		// Parse and execute the prompt template
		tmpl, err := template.New(step.Name).Parse(step.Prompt)
		if err != nil {
			return StepResult{NextOffset: 0, Message: fmt.Sprintf("template parse error: %v", err)}, err
		}

		// Build template data from workflow data and prepare data
		templateData := make(map[string]interface{})
		w.mu.Lock()
		for k, v := range w.Data {
			templateData[k] = v
		}
		w.mu.Unlock()
		for k, v := range prepareData {
			templateData[k] = v
		}

		var promptBuf bytes.Buffer
		if err := tmpl.Execute(&promptBuf, templateData); err != nil {
			return StepResult{NextOffset: 0, Message: fmt.Sprintf("template execute error: %v", err)}, err
		}

		prompt := promptBuf.String()

		// Send prompt to AI if sendPrompt is configured
		if w.sendPrompt != nil {
			responseChan := w.sendPrompt(ctx, prompt)
			response = <-responseChan
		} else {
			slog.Warn("No sendPrompt function configured, skipping AI interaction")
		}
	}

	// Run verify function if present
	if step.Verify != nil {
		return step.Verify(w, response), nil
	}

	// Default: proceed to next step
	return StepResult{NextOffset: 1, Message: "completed"}, nil
}

// GetStepStates returns a copy of the step states
func (w *Workflow) GetStepStates() []StepState {
	w.mu.Lock()
	defer w.mu.Unlock()

	states := make([]StepState, len(w.StepStates))
	copy(states, w.StepStates)
	return states
}

// GetCurrentStepName returns the name of the current step
func (w *Workflow) GetCurrentStepName() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.CurrentStep < len(w.Steps) {
		return w.Steps[w.CurrentStep].Name
	}
	return ""
}

// GetProgress returns the current progress as a fraction (0.0 to 1.0)
func (w *Workflow) GetProgress() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.Steps) == 0 {
		return 0
	}
	return float64(w.CurrentStep) / float64(len(w.Steps))
}

// Load loads a workflow from the database
func Load(db *storage.DB, workflowID string) (*Workflow, error) {
	store := storage.NewWorkflowStore(db)

	workflowData, err := store.LoadWorkflow(workflowID)
	if err != nil {
		return nil, err
	}
	if workflowData == nil {
		return nil, fmt.Errorf("workflow not found: %s", workflowID)
	}

	// Parse data JSON into map[string]interface{}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(workflowData.Data), &data); err != nil {
		data = make(map[string]interface{})
	}

	w := &Workflow{
		ID:          workflowData.ID,
		Name:        workflowData.Name,
		CurrentStep: workflowData.CurrentStep,
		State:       workflowData.State,
		MaxRetries:  workflowData.MaxRetries,
		Data:        data,
		BranchID:    workflowData.BranchID,
		CreatedAt:   workflowData.CreatedAt,
		UpdatedAt:   workflowData.UpdatedAt,
		db:          db,
		store:       store,
	}

	// Load step states
	stepData, err := store.LoadWorkflowSteps(workflowID)
	if err != nil {
		return nil, err
	}

	for _, sd := range stepData {
		w.StepStates = append(w.StepStates, StepState{
			Name:       sd.Name,
			RetryCount: sd.RetryCount,
			Message:    sd.Message,
		})
	}

	return w, nil
}

// ListActive returns all active (pending/running) workflows for the given repo context
func ListActive(db *storage.DB, repoCtx RepoContext) ([]*Workflow, error) {
	store := storage.NewWorkflowStore(db)

	workflowsData, err := store.ListActiveWorkflows(repoCtx.Host, repoCtx.Org, repoCtx.Project, repoCtx.Branch)
	if err != nil {
		return nil, err
	}

	var workflows []*Workflow
	for _, wd := range workflowsData {
		w, err := Load(db, wd.ID)
		if err != nil {
			slog.Warn("Failed to load workflow", "id", wd.ID, "error", err)
			continue
		}
		workflows = append(workflows, w)
	}

	return workflows, nil
}
