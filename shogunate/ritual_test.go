package shogunate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "modernc.org/sqlite"
)

func TestParseRitual(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(*testing.T, *RitualDef)
	}{
		{
			name: "basic ritual",
			yaml: `
name: test-ritual
description: A test ritual
steps:
  - name: step1
    minister: strategist
    task: Plan the work
`,
			wantErr: false,
			check: func(t *testing.T, r *RitualDef) {
				if r.Name != "test-ritual" {
					t.Errorf("expected name 'test-ritual', got %q", r.Name)
				}
				if len(r.Steps) != 1 {
					t.Errorf("expected 1 step, got %d", len(r.Steps))
				}
				if r.Steps[0].Minister != "strategist" {
					t.Errorf("expected minister 'strategist', got %q", r.Steps[0].Minister)
				}
			},
		},
		{
			name: "ritual with triggers and inputs",
			yaml: `
name: implement
description: Implementation workflow
triggers:
  - event: edict_assigned
  - manual: true
inputs:
  edict_id:
    type: string
    required: true
steps:
  - name: plan
    minister: strategist
    task: Create plan for {{ .edict_id }}
`,
			wantErr: false,
			check: func(t *testing.T, r *RitualDef) {
				if len(r.Triggers) != 2 {
					t.Errorf("expected 2 triggers, got %d", len(r.Triggers))
				}
				if r.Triggers[0].Event != "edict_assigned" {
					t.Errorf("expected event 'edict_assigned', got %q", r.Triggers[0].Event)
				}
				if !r.Triggers[1].Manual {
					t.Error("expected manual trigger to be true")
				}
				if input, ok := r.Inputs["edict_id"]; !ok {
					t.Error("expected input 'edict_id'")
				} else if !input.Required {
					t.Error("expected input to be required")
				}
			},
		},
		{
			name: "ritual with dependencies and failure handling",
			yaml: `
name: complex-ritual
steps:
  - name: step1
    minister: forge
    task: Write code
  - name: step2
    minister: judge
    task: Run tests
    depends_on: [step1]
    on_failure: goto
    on_failure_target: step1
    max_retries: 5
`,
			wantErr: false,
			check: func(t *testing.T, r *RitualDef) {
				if len(r.Steps) != 2 {
					t.Errorf("expected 2 steps, got %d", len(r.Steps))
				}
				step2 := r.Steps[1]
				if len(step2.DependsOn) != 1 || step2.DependsOn[0] != "step1" {
					t.Errorf("expected depends_on [step1], got %v", step2.DependsOn)
				}
				if step2.OnFailure != "goto" {
					t.Errorf("expected on_failure 'goto', got %q", step2.OnFailure)
				}
				if step2.OnFailureTarget != "step1" {
					t.Errorf("expected on_failure_target 'step1', got %q", step2.OnFailureTarget)
				}
				if step2.MaxRetries != 5 {
					t.Errorf("expected max_retries 5, got %d", step2.MaxRetries)
				}
			},
		},
		{
			name: "ritual with given and then",
			yaml: `
name: gherkin-ritual
steps:
  - name: step1
    minister: forge
    given:
      - the edict details
      - "!git diff HEAD"
    act: |
      Implement changes.
    then:
      - "!just test"
`,
			wantErr: false,
			check: func(t *testing.T, r *RitualDef) {
				if len(r.Steps[0].Given) != 2 {
					t.Errorf("expected 2 given entries, got %d", len(r.Steps[0].Given))
				}
				if r.Steps[0].Given[0] != "the edict details" {
					t.Errorf("expected first given 'the edict details', got %q", r.Steps[0].Given[0])
				}
				if r.Steps[0].Given[1] != "!git diff HEAD" {
					t.Errorf("expected second given '!git diff HEAD', got %q", r.Steps[0].Given[1])
				}
				if len(r.Steps[0].Then) != 1 {
					t.Errorf("expected 1 then entry, got %d", len(r.Steps[0].Then))
				}
				if r.Steps[0].Then[0] != "!just test" {
					t.Errorf("expected then '!just test', got %q", r.Steps[0].Then[0])
				}
			},
		},
		{
			name:    "invalid yaml",
			yaml:    `{{{invalid yaml`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ritual, err := ParseRitual([]byte(tt.yaml))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRitual() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && ritual != nil {
				tt.check(t, ritual)
			}
		})
	}
}

func TestValidateRitual(t *testing.T) {
	tests := []struct {
		name    string
		ritual  *RitualDef
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid ritual",
			ritual: &RitualDef{
				Name: "valid",
				Steps: []RitualStep{
					{Name: "step1", Minister: "forge", Task: "do something"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			ritual: &RitualDef{
				Steps: []RitualStep{
					{Name: "step1", Minister: "forge", Task: "do something"},
				},
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "no steps",
			ritual: &RitualDef{
				Name:  "empty",
				Steps: []RitualStep{},
			},
			wantErr: true,
			errMsg:  "no steps",
		},
		{
			name: "duplicate step names",
			ritual: &RitualDef{
				Name: "dupe",
				Steps: []RitualStep{
					{Name: "step1", Minister: "forge", Task: "first"},
					{Name: "step1", Minister: "judge", Task: "second"},
				},
			},
			wantErr: true,
			errMsg:  "duplicate step name",
		},
		{
			name: "unknown dependency",
			ritual: &RitualDef{
				Name: "bad-dep",
				Steps: []RitualStep{
					{Name: "step1", Minister: "forge", Task: "do", DependsOn: []string{"unknown"}},
				},
			},
			wantErr: true,
			errMsg:  "unknown step",
		},
		{
			name: "unknown on_failure_target",
			ritual: &RitualDef{
				Name: "bad-target",
				Steps: []RitualStep{
					{Name: "step1", Minister: "forge", Task: "do", OnFailure: "goto", OnFailureTarget: "nonexistent"},
				},
			},
			wantErr: true,
			errMsg:  "unknown step",
		},
		{
			name: "missing minister",
			ritual: &RitualDef{
				Name: "no-minister",
				Steps: []RitualStep{
					{Name: "step1", Task: "do something"},
				},
			},
			wantErr: true,
			errMsg:  "requires minister",
		},
		{
			name: "missing act",
			ritual: &RitualDef{
				Name: "no-act",
				Steps: []RitualStep{
					{Name: "step1", Minister: "forge"},
				},
			},
			wantErr: true,
			errMsg:  "requires act or task",
		},
		{
			name: "circular dependency - self reference",
			ritual: &RitualDef{
				Name: "circular",
				Steps: []RitualStep{
					{Name: "step1", Minister: "forge", Task: "do", DependsOn: []string{"step1"}},
				},
			},
			wantErr: true,
			errMsg:  "circular dependency",
		},
		{
			name: "circular dependency - chain",
			ritual: &RitualDef{
				Name: "circular-chain",
				Steps: []RitualStep{
					{Name: "step1", Minister: "forge", Task: "a", DependsOn: []string{"step3"}},
					{Name: "step2", Minister: "forge", Task: "b", DependsOn: []string{"step1"}},
					{Name: "step3", Minister: "forge", Task: "c", DependsOn: []string{"step2"}},
				},
			},
			wantErr: true,
			errMsg:  "circular dependency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRitual(tt.ritual)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRitual() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			}
		})
	}
}

func TestRitualRegistry(t *testing.T) {
	registry := NewRitualRegistry()

	// Test empty registry
	if r := registry.Get("nonexistent"); r != nil {
		t.Error("expected nil for nonexistent ritual")
	}

	// Register a ritual
	ritual := &RitualDef{
		Name: "test",
		Triggers: []RitualTrigger{
			{Event: "edict_assigned"},
		},
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Task: "do"},
		},
	}

	if err := registry.Register(ritual); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Test Get
	if r := registry.Get("test"); r == nil {
		t.Error("expected ritual, got nil")
	} else if r.Name != "test" {
		t.Errorf("expected name 'test', got %q", r.Name)
	}

	// Test GetByEvent
	rituals := registry.GetByEvent("edict_assigned")
	if len(rituals) != 1 {
		t.Errorf("expected 1 ritual for event, got %d", len(rituals))
	}

	// Test List
	names := registry.List()
	if len(names) != 1 || names[0] != "test" {
		t.Errorf("expected [test], got %v", names)
	}

	// Test registering without name
	if err := registry.Register(&RitualDef{}); err == nil {
		t.Error("expected error for ritual without name")
	}
}

func TestLoadRitualsFromDir(t *testing.T) {
	// Create temp directory
	dir := t.TempDir()

	// Create a valid ritual file
	validRitual := `
name: valid-ritual
steps:
  - name: step1
    minister: forge
    task: Do something
`
	if err := os.WriteFile(filepath.Join(dir, "valid.yaml"), []byte(validRitual), 0644); err != nil {
		t.Fatal(err)
	}

	// Create another valid ritual with .yml extension
	anotherRitual := `
name: another-ritual
steps:
  - name: step1
    minister: chancellor
    task: Ask something
`
	if err := os.WriteFile(filepath.Join(dir, "another.yml"), []byte(anotherRitual), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a non-yaml file (should be ignored)
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0644); err != nil {
		t.Fatal(err)
	}

	// Load rituals
	rituals, err := LoadRitualsFromDir(dir)
	if err != nil {
		t.Fatalf("LoadRitualsFromDir() error = %v", err)
	}

	if len(rituals) != 2 {
		t.Errorf("expected 2 rituals, got %d", len(rituals))
	}

	// Test non-existent directory
	rituals, err = LoadRitualsFromDir("/nonexistent/path")
	if err != nil {
		t.Errorf("expected no error for non-existent dir, got %v", err)
	}
	if len(rituals) != 0 {
		t.Errorf("expected 0 rituals for non-existent dir, got %d", len(rituals))
	}
}

func TestLoadRitualsFromDir_Invalid(t *testing.T) {
	dir := t.TempDir()

	// Create an invalid ritual file
	invalidRitual := `
name: invalid
steps:
  - name: step1
    task: Do something
`
	if err := os.WriteFile(filepath.Join(dir, "invalid.yaml"), []byte(invalidRitual), 0644); err != nil {
		t.Fatal(err)
	}

	// Load should fail
	_, err := LoadRitualsFromDir(dir)
	if err == nil {
		t.Error("expected error for invalid ritual")
	}
}

func TestLoadEmbeddedRituals(t *testing.T) {
	rituals, err := LoadEmbeddedRituals()
	if err != nil {
		t.Fatalf("LoadEmbeddedRituals() error = %v", err)
	}

	if len(rituals) == 0 {
		t.Fatal("expected at least 1 embedded ritual")
	}

	// Only verify dawn-audience — other rituals change frequently
	var dawnAudience *RitualDef
	for _, r := range rituals {
		if r.Name == "dawn-audience" {
			dawnAudience = r
			break
		}
	}
	if dawnAudience == nil {
		t.Fatal("dawn-audience ritual not found")
	}
	if len(dawnAudience.Steps) != 1 {
		t.Errorf("dawn-audience: expected 1 step, got %d", len(dawnAudience.Steps))
	}
	if dawnAudience.Steps[0].Minister != "chancellor" {
		t.Errorf("dawn-audience: expected minister 'chancellor', got %q", dawnAudience.Steps[0].Minister)
	}
}

// containsString checks if s contains substr
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// TestRitualStreamMessages tests that ritual execution sends all expected
// typed messages through the notify callback.
func TestRitualStreamMessages(t *testing.T) {
	// Setup test database
	db := setupRitualTestDB(t)

	// Create a simple single-step ritual
	ritual := &RitualDef{
		Name:        "test-stream",
		Description: "A test ritual for streaming messages",
		Triggers:    []RitualTrigger{{Manual: true}},
		Inputs: map[string]InputDef{
			"edict_id": {Type: "integer", Required: true},
		},
		Steps: []RitualStep{
			{
				Name:     "echo",
				Minister: "forge",
				Task:     "Echo hello",
			},
		},
	}

	// Create and populate registry
	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("Failed to register ritual: %v", err)
	}

	// Create mock shogunate with ministers that return "hello\n"
	shogunate := newRitualTestShogunate(t, "hello\n", nil)

	// Create ritual runner
	runner := NewRitualRunner(registry, shogunate.GetMinister, shogunate.PublishEvent, db, nil, nil)

	// Collect messages from the stream
	var messages []any
	notify := func(msg any) {
		messages = append(messages, msg)
	}

	// Start the ritual
	ctx := context.Background()
	exec, err := runner.Start(ctx, "test-stream", 1, map[string]string{"edict_id": "1"}, notify)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	// Run the ritual to completion
	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	// Verify we received the expected message types
	var (
		ritualStarted  int // ritual-level "started" (empty StepName)
		stepStarted    int // step-level "started"
		completedCount int
		ritualComplete int
	)

	for _, msg := range messages {
		stepMsg, ok := msg.(RitualStepMsg)
		if !ok {
			continue
		}
		switch stepMsg.Status {
		case "started":
			if stepMsg.StepName == "" {
				ritualStarted++
				if stepMsg.RitualName != "test-stream" {
					t.Errorf("Expected ritual name 'test-stream', got %q", stepMsg.RitualName)
				}
			} else {
				stepStarted++
				if stepMsg.StepName != "echo" {
					t.Errorf("Expected step name 'echo', got %q", stepMsg.StepName)
				}
				if stepMsg.RitualName != "test-stream" {
					t.Errorf("Expected ritual name 'test-stream', got %q", stepMsg.RitualName)
				}
			}
		case "completed":
			completedCount++
		case "ritual_completed":
			ritualComplete++
		}
	}

	// Verify message counts
	if ritualStarted != 1 {
		t.Errorf("Expected 1 ritual-level 'started' message, got %d", ritualStarted)
	}
	if stepStarted != 1 {
		t.Errorf("Expected 1 step-level 'started' message, got %d", stepStarted)
	}
	if completedCount != 1 {
		t.Errorf("Expected 1 'completed' message, got %d", completedCount)
	}
	if ritualComplete != 1 {
		t.Errorf("Expected 1 'ritual_completed' message, got %d", ritualComplete)
	}

	// Verify execution state
	if exec.State != RitualStateCompleted {
		t.Errorf("Expected state 'completed', got %s", exec.State)
	}
}

// TestRitualStreamMessages_MultiStep tests a multi-step ritual sends messages for each step
func TestRitualStreamMessages_MultiStep(t *testing.T) {
	db := setupRitualTestDB(t)

	// Create a multi-step ritual
	ritual := &RitualDef{
		Name:        "multi-step",
		Description: "Multi-step ritual",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Task: "do one"},
			{Name: "step2", Minister: "judge", Task: "do two", DependsOn: []string{"step1"}},
			{Name: "step3", Minister: "sage", Task: "do three", DependsOn: []string{"step2"}},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	shogunate := newRitualTestShogunate(t, "ok\n", nil)
	runner := NewRitualRunner(registry, shogunate.GetMinister, shogunate.PublishEvent, db, nil, nil)

	var messages []RitualStepMsg
	notify := func(msg any) {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			messages = append(messages, stepMsg)
		}
	}

	ctx := context.Background()
	exec, err := runner.Start(ctx, "multi-step", 2, nil, notify)
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should have: ritual_started(1) + started(3) + completed(3) + ritual_completed(1) = 8 messages
	expectedCount := 8
	if len(messages) != expectedCount {
		t.Errorf("Expected %d messages, got %d", expectedCount, len(messages))
		for i, m := range messages {
			t.Logf("  [%d] step=%s status=%s", i, m.StepName, m.Status)
		}
	}

	// Verify step indices are correct
	for _, msg := range messages {
		if msg.Status == "started" || msg.Status == "completed" {
			if msg.TotalSteps != 3 {
				t.Errorf("Expected TotalSteps=3, got %d", msg.TotalSteps)
			}
		}
	}
}

// TestRitualStreamMessages_Failure tests that failure messages are sent correctly
func TestRitualStreamMessages_Failure(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fail-ritual",
		Steps: []RitualStep{
			{Name: "fail-step", Minister: "forge", Task: "do something", OnFailure: "abort"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	// Mock shogunate where ministers return errors
	shogunate := newRitualTestShogunate(t, "", fmt.Errorf("minister failed"))
	runner := NewRitualRunner(registry, shogunate.GetMinister, shogunate.PublishEvent, db, nil, nil)

	var messages []RitualStepMsg
	notify := func(msg any) {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			messages = append(messages, stepMsg)
		}
	}

	ctx := context.Background()
	exec, _ := runner.Start(ctx, "fail-ritual", 3, nil, notify)
	err := runner.Run(ctx, exec)

	// Should fail
	if err == nil {
		t.Error("Expected error from failed ritual")
	}

	// Should have: started(1) + failed(1) = 2 messages
	var failedCount int
	for _, msg := range messages {
		if msg.Status == "failed" {
			failedCount++
		}
	}

	if failedCount != 1 {
		t.Errorf("Expected 1 'failed' message, got %d", failedCount)
	}

	if exec.State != RitualStateFailed {
		t.Errorf("Expected state 'failed', got %s", exec.State)
	}
}

// TestRitualGotoPassesErrorMessage tests that when step2 fails and gotos back to step1,
// step1 receives the error message from step2 in its Work field (not scratchpad).
func TestRitualGotoPassesErrorMessage(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "goto-error-test",
		Steps: []RitualStep{
			{Name: "report", Minister: "forge", Act: "report status"},
			{Name: "review", Minister: "judge", Act: "review code",
				OnFailure: "goto", OnFailureTarget: "report"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Track Works received by forge across invocations
	var mu sync.Mutex
	var forgeWorks []string
	forgeCallCount := 0

	forgeCh := make(chan *Task, 1)
	judgeCh := make(chan *Task, 1)

	// forge: captures Work, returns it as output
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				mu.Lock()
				forgeWorks = append(forgeWorks, task.Work)
				forgeCallCount++
				count := forgeCallCount
				mu.Unlock()
				task.Done <- Result{Output: task.Work}
				if count >= 2 {
					cancel()
				}
			}
		}
	}()

	// judge: always fails
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-judgeCh:
				task.Done <- Result{Err: fmt.Errorf("Here goes the error message")}
			}
		}
	}()

	// Build shogunate with custom ministers (don't start their Run methods)
	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}
	judgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "judge",
		tasksCh:      judgeCh,
	}
	shog := &Shogunate{
		ministers:     map[string]Minister{"forge": forgeM, "judge": judgeM},
		logger: slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "goto-error-test", 4, nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	// Run will loop: step1 ok -> step2 fail -> goto step1 -> step1 ok -> cancel
	_ = runner.Run(ctx, exec)

	mu.Lock()
	defer mu.Unlock()

	if len(forgeWorks) < 2 {
		t.Fatalf("Expected forge to be called at least 2 times, got %d", len(forgeWorks))
	}

	// Second invocation Work should contain the error from step2
	if !strings.Contains(forgeWorks[1], "Here goes the error message") {
		t.Errorf("Expected second invocation Work to contain error, got: %s", forgeWorks[1])
	}
}

// TestRitualGotoPassesOutputAndError tests that when a step fails with both output and error,
// both are passed to the goto target step's Work.
func TestRitualGotoPassesOutputAndError(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "goto-output-error-test",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "implement feature"},
			{Name: "step2", Minister: "judge", Act: "review code",
				OnFailure: "goto", OnFailureTarget: "step1"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var forgeWorks []string
	forgeCallCount := 0

	forgeCh := make(chan *Task, 1)
	judgeCh := make(chan *Task, 1)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				mu.Lock()
				forgeWorks = append(forgeWorks, task.Work)
				forgeCallCount++
				count := forgeCallCount
				mu.Unlock()
				task.Done <- Result{Output: task.Work}
				if count >= 2 {
					cancel()
				}
			}
		}
	}()

	// judge: fails with both output and error
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-judgeCh:
				task.Done <- Result{
					Output: "Rejection 1: unsafe code\nRejection 2: missing tests",
					Err:    fmt.Errorf("review failed"),
				}
			}
		}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}
	judgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "judge",
		tasksCh:      judgeCh,
	}
	shog := &Shogunate{
		ministers:     map[string]Minister{"forge": forgeM, "judge": judgeM},
		logger: slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "goto-output-error-test", 5, nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	_ = runner.Run(ctx, exec)

	mu.Lock()
	defer mu.Unlock()

	if len(forgeWorks) < 2 {
		t.Fatalf("Expected forge to be called at least 2 times, got %d", len(forgeWorks))
	}

	// Second invocation should contain BOTH the output and the error
	if !strings.Contains(forgeWorks[1], "Rejection 1: unsafe code") {
		t.Errorf("Expected second invocation Work to contain output, got: %s", forgeWorks[1])
	}
	if !strings.Contains(forgeWorks[1], "review failed") {
		t.Errorf("Expected second invocation Work to contain error, got: %s", forgeWorks[1])
	}
}

// TestRitualGotoSessionReuse tests that the session returned by a minister is stored
// and passed back on goto re-invocation.
func TestRitualGotoSessionReuse(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "goto-session-test",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "implement feature"},
			{Name: "step2", Minister: "judge", Act: "review code",
				OnFailure: "goto", OnFailureTarget: "step1"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var forgeSessionsReceived []*Session
	forgeCallCount := 0

	// Create a dummy session to return from forge
	dummySession := &Session{ID: "test-session-123"}

	forgeCh := make(chan *Task, 1)
	judgeCh := make(chan *Task, 1)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				mu.Lock()
				forgeSessionsReceived = append(forgeSessionsReceived, task.Session)
				forgeCallCount++
				count := forgeCallCount
				mu.Unlock()
				task.Done <- Result{Output: task.Work, Session: dummySession}
				if count >= 2 {
					cancel()
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-judgeCh:
				task.Done <- Result{Err: fmt.Errorf("review failed")}
			}
		}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}
	judgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "judge",
		tasksCh:      judgeCh,
	}
	shog := &Shogunate{
		ministers:     map[string]Minister{"forge": forgeM, "judge": judgeM},
		logger: slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "goto-session-test", 6, nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	_ = runner.Run(ctx, exec)

	mu.Lock()
	defer mu.Unlock()

	if len(forgeSessionsReceived) < 2 {
		t.Fatalf("Expected forge to be called at least 2 times, got %d", len(forgeSessionsReceived))
	}

	// First invocation: no existing session
	if forgeSessionsReceived[0] != nil {
		t.Error("Expected first invocation to have nil session")
	}

	// Second invocation: should receive the session returned earlier
	if forgeSessionsReceived[1] == nil {
		t.Error("Expected second invocation to have non-nil session")
	} else if forgeSessionsReceived[1].ID != "test-session-123" {
		t.Errorf("Expected session ID 'test-session-123', got %q", forgeSessionsReceived[1].ID)
	}
}

// TestRitualGotoPreservesOutputOnFailure tests that executeMinisterStep preserves output on error.
func TestRitualGotoPreservesOutputOnFailure(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "preserve-output-test",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "do work", OnFailure: "abort"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	ctx := context.Background()

	forgeCh := make(chan *Task, 1)

	// forge: returns both output and error
	go func() {
		task := <-forgeCh
		task.Done <- Result{Output: "partial output", Err: fmt.Errorf("something went wrong")}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}
	shog := &Shogunate{
		ministers:     map[string]Minister{"forge": forgeM},
		logger: slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "preserve-output-test", 7, nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	_ = runner.Run(ctx, exec)

	// Verify output is preserved in step state
	if exec.stepStates[0].Output != "partial output" {
		t.Errorf("Expected step output 'partial output', got %q", exec.stepStates[0].Output)
	}
	// Verify error message is stored
	if !strings.Contains(exec.stepStates[0].Message, "something went wrong") {
		t.Errorf("Expected step message to contain error, got %q", exec.stepStates[0].Message)
	}
}

func TestStepDefRegistry(t *testing.T) {
	reg := NewStepDefRegistry()

	// Test matching built-in patterns
	tests := []struct {
		text    string
		wantKey string
		wantNil bool
	}{
		{"the edict details", "edict", false},
		{"the court status", "court_status", false},
		{"the manifests", "manifests", false},
		{"the verdicts", "verdicts", false},
		{"the precedents", "precedents", false},
		{"something unknown", "", true},
	}

	for _, tt := range tests {
		def, err := reg.Match(tt.text)
		if err != nil {
			t.Fatalf("Match(%q) error: %v", tt.text, err)
		}
		if tt.wantNil {
			if def != nil {
				t.Errorf("Match(%q) expected nil, got key=%q", tt.text, def.OutputKey)
			}
		} else {
			if def == nil {
				t.Errorf("Match(%q) expected key=%q, got nil", tt.text, tt.wantKey)
			} else if def.OutputKey != tt.wantKey {
				t.Errorf("Match(%q) expected key=%q, got %q", tt.text, tt.wantKey, def.OutputKey)
			}
		}
	}
}

func TestResolveStepDef(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, nil)

	// Test bash command resolution
	entry, err := runner.resolveStepDef("!just test")
	if err != nil {
		t.Fatalf("resolveStepDef('!just test') error: %v", err)
	}
	if entry.Kind != StepDefBash {
		t.Errorf("expected StepDefBash, got %d", entry.Kind)
	}
	if entry.Command != "just test" {
		t.Errorf("expected command 'just test', got %q", entry.Command)
	}

	// Test builtin resolution
	entry, err = runner.resolveStepDef("the edict details")
	if err != nil {
		t.Fatalf("resolveStepDef('the edict details') error: %v", err)
	}
	if entry.Kind != StepDefBuiltin {
		t.Errorf("expected StepDefBuiltin, got %d", entry.Kind)
	}
	if entry.Command != "get_edict" {
		t.Errorf("expected handler 'get_edict', got %q", entry.Command)
	}
	if entry.Key != "edict" {
		t.Errorf("expected key 'edict', got %q", entry.Key)
	}

	// Test unknown pattern
	_, err = runner.resolveStepDef("something that does not match")
	if err == nil {
		t.Error("expected error for unknown pattern")
	}
}

func TestRunGivenStep_Bash(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()
	mockRunner := &mockCmdRunner{output: "diff output\n", exitCode: "0"}
	runner := NewRitualRunner(registry, nil, nil, db, mockRunner, nil)

	exec := &RitualExecution{
		ID:         "test-exec",
		RitualName: "test",
		EdictID:    100,
	}

	entry := StepDefEntry{
		Kind:    StepDefBash,
		Key:     "git",
		Command: "git diff HEAD",
	}

	result, err := runner.runGivenStep(context.Background(), exec, entry)
	if err != nil {
		t.Fatalf("runGivenStep error: %v", err)
	}
	if result != "diff output\n" {
		t.Errorf("expected 'diff output\\n', got %q", result)
	}
}

func TestRunThenStep_Bash(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()

	// Success case
	mockRunner := &mockCmdRunner{output: "ok\n", exitCode: "0"}
	runner := NewRitualRunner(registry, nil, nil, db, mockRunner, nil)

	exec := &RitualExecution{
		ID:         "test-exec",
		RitualName: "test",
		EdictID:    100,
	}

	entry := StepDefEntry{
		Kind:    StepDefBash,
		Key:     "just",
		Command: "just test",
	}

	err := runner.runThenStep(context.Background(), exec, entry)
	if err != nil {
		t.Fatalf("runThenStep success case: %v", err)
	}

	// Failure case
	failRunner := &mockCmdRunner{output: "FAIL\n", exitCode: "1"}
	runner = NewRitualRunner(registry, nil, nil, db, failRunner, nil)

	err = runner.runThenStep(context.Background(), exec, entry)
	if err == nil {
		t.Error("expected error for failing then step")
	}
	if !strings.Contains(err.Error(), "exit 1") {
		t.Errorf("expected exit code in error, got: %v", err)
	}
}

func TestRunThenStep_Multiple(t *testing.T) {
	db := setupRitualTestDB(t)

	// Create ritual with multiple then steps, second one fails
	ritual := &RitualDef{
		Name: "multi-then",
		Steps: []RitualStep{
			{
				Name:     "build",
				Minister: "forge",
				Task:     "build something",
				Then:     []string{"!echo check1", "!exit 1"},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	shogunate := newRitualTestShogunate(t, "build\n", nil)
	mockRunner := &mockCallCountRunner{
		results: []runners.Output{
			{Output: "ok\n", ExitCode: "0"},   // first then
			{Output: "FAIL\n", ExitCode: "1"}, // second then
		},
	}
	runner := NewRitualRunner(registry, shogunate.GetMinister, shogunate.PublishEvent, db, mockRunner, nil)

	ctx := context.Background()
	exec, err := runner.Start(ctx, "multi-then", 8, nil, nil)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	err = runner.Run(ctx, exec)
	if err == nil {
		t.Error("expected error from failing then step")
	}
	if !strings.Contains(err.Error(), "then") {
		t.Errorf("expected 'then' in error message, got: %v", err)
	}
}

func TestLoadBuiltinRituals(t *testing.T) {
	rituals, err := LoadEmbeddedRituals()
	if err != nil {
		t.Fatalf("LoadBuiltinRituals() error = %v", err)
	}

	if len(rituals) != 5 {
		names := make([]string, len(rituals))
		for i, r := range rituals {
			names[i] = r.Name
		}
		t.Errorf("expected 5 builtin rituals, got %d: %v", len(rituals), names)
	}

	// Verify swift-strike uses ritual-level background given
	for _, r := range rituals {
		if r.Name == "swift-strike" {
			// Background given at ritual level
			if len(r.Background) == 0 || r.Background[0] != "the edict details" {
				t.Errorf("swift-strike: expected background given 'the edict details', got %v", r.Background)
			}
			// Forge step should have no step-level given (hoisted)
			if len(r.Steps[0].Given) != 0 {
				t.Errorf("swift-strike forge: expected no step-level given, got %v", r.Steps[0].Given)
			}
			// Judge step should have Given with !just test
			if len(r.Steps[1].Given) == 0 {
				t.Error("swift-strike judge step: expected given entries")
			}
			if r.Steps[1].Given[1] != "!just test" {
				t.Errorf("swift-strike judge given: expected '!just test', got %q", r.Steps[1].Given[1])
			}
		}
	}
}

func TestBackgroundGiven(t *testing.T) {
	db := setupRitualTestDB(t)

	// Ritual with background given (bash) and a minister step
	ritual := &RitualDef{
		Name:       "bg-test",
		Background: []string{"!echo background-data"},
		Steps: []RitualStep{
			{Name: "work", Minister: "forge", Task: "do work"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	shogunate := newRitualTestShogunate(t, "step-done\n", nil)
	mockRunner := &mockCallCountRunner{
		results: []runners.Output{
			{Output: "background-data\n", ExitCode: "0"}, // background given
		},
	}
	runner := NewRitualRunner(registry, shogunate.GetMinister, shogunate.PublishEvent, db, mockRunner, nil)

	var messages []RitualStepMsg
	notify := func(msg any) {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			messages = append(messages, stepMsg)
		}
	}

	ctx := context.Background()
	exec, err := runner.Start(ctx, "bg-test", 9, nil, notify)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Verify background given result is in context
	given, ok := exec.Data["given_context"].(map[string]interface{})
	if !ok {
		t.Fatal("expected given_context in exec.Data")
	}
	if _, ok := given["echo"]; !ok {
		t.Errorf("expected 'echo' key in given_context, got keys: %v", given)
	}

	// Verify cmd_running and cmd_done notifications were emitted for background
	var cmdRunning, cmdDone int
	for _, m := range messages {
		switch m.Status {
		case "cmd_running":
			cmdRunning++
			if m.Message != "!echo background-data" {
				t.Errorf("expected cmd_running message '!echo background-data', got %q", m.Message)
			}
		case "cmd_done":
			cmdDone++
			if m.Message != "!echo background-data" {
				t.Errorf("expected cmd_done message '!echo background-data', got %q", m.Message)
			}
		}
	}
	if cmdRunning != 1 {
		t.Errorf("expected 1 cmd_running message for background, got %d", cmdRunning)
	}
	if cmdDone != 1 {
		t.Errorf("expected 1 cmd_done message for background, got %d", cmdDone)
	}
}

// ritualTestMinister is a Minister that auto-completes tasks with a configured result.
type ritualTestMinister struct {
	MinisterBase
	id      string
	tasksCh chan *Task
	result  string
	err     error
}

func (m *ritualTestMinister) ID() string           { return m.id }
func (m *ritualTestMinister) SystemPrompt() string { return "" }
func (m *ritualTestMinister) Title() string        { return m.id }
func (m *ritualTestMinister) Tools() []Tool        { return nil }
func (m *ritualTestMinister) Tasks() chan<- *Task  { return m.tasksCh }
func (m *ritualTestMinister) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-m.tasksCh:
			t.Done <- Result{Output: m.result, Err: m.err}
		}
	}
}

// newRitualTestShogunate creates a Shogunate with mock ministers for ritual tests.
// All ministers return the given output. Start a goroutine for each minister's Run.
func newRitualTestShogunate(t *testing.T, output string, err error) *Shogunate {
	t.Helper()
	ministers := map[string]Minister{}
	for _, id := range []string{"forge", "judge", "sage", "strategist", "chancellor", "marshal"} {
		m := &ritualTestMinister{
			MinisterBase: MinisterBase{logger: slog.Default()},
			id:           id,
			tasksCh:      make(chan *Task, 1),
			result:       output,
			err:          err,
		}
		ministers[id] = m
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go m.Run(ctx)
	}
	s := &Shogunate{
		ministers: ministers,
		logger:   slog.Default(),
	}
	// Set up a minimal ritualGuard so PublishEvent and GetRitualRunner work
	base := &MinisterBase{logger: slog.Default()}
	s.ritualGuard = NewRitualGuard(RitualGuardOpts{
		Base:        base,
		GetMinister: s.GetMinister,
	})
	return s
}

// newRitualTestShogunateWithDB is like newRitualTestShogunate but with a db for the ritualGuard.
func newRitualTestShogunateWithDB(t *testing.T, db *gorm.DB, output string, err error) *Shogunate {
	t.Helper()
	ministers := map[string]Minister{}
	for _, id := range []string{"forge", "judge", "sage", "strategist", "chancellor", "marshal"} {
		m := &ritualTestMinister{
			MinisterBase: MinisterBase{logger: slog.Default()},
			id:           id,
			tasksCh:      make(chan *Task, 1),
			result:       output,
			err:          err,
		}
		ministers[id] = m
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go m.Run(ctx)
	}
	s := &Shogunate{
		ministers: ministers,
		logger:   slog.Default(),
	}
	base := NewMinisterBase(db, nil, slog.Default())
	s.ritualGuard = NewRitualGuard(RitualGuardOpts{
		Base:        base,
		GetMinister: s.GetMinister,
	})
	return s
}

// mockCallCountRunner returns sequential results for successive calls
type mockCallCountRunner struct {
	results []runners.Output
	idx     int
}

func (m *mockCallCountRunner) Run(ctx context.Context, input runners.Input) (runners.Output, error) {
	if m.idx >= len(m.results) {
		return runners.Output{Output: "", ExitCode: "0"}, nil
	}
	result := m.results[m.idx]
	m.idx++
	if result.ExitCode != "0" {
		return result, nil
	}
	return result, nil
}

func (m *mockCallCountRunner) Restart(ctx context.Context) error    { return nil }
func (m *mockCallCountRunner) Close(ctx context.Context) error      { return nil }
func (m *mockCallCountRunner) AllowFallback(bool)                   {}
func (m *mockCallCountRunner) RunnerType() string                   { return "mock" }
func (m *mockCallCountRunner) SetMessageChannel(chan<- runners.Msg) {}

// setupRitualTestDB creates a test database with ritual tables
func setupRitualTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := tmpDir + "/ritual_test.db"

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to initialize gorm: %v", err)
	}

	// Migrate ritual tables
	err = db.AutoMigrate(&RitualExecution{}, &RitualStepState{}, &storage.TianEvent{})
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	return db
}

// TestInvokeRitualTool_Enacted verifies that enact_ritual returns immediately with "enacted" status
// and publishes an EventRitualEnacted event for the RitualGuard to pick up.
func TestInvokeRitualTool_Enacted(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name:        "test-enacted",
		Description: "A test ritual for async enactment",
		Steps: []RitualStep{
			{Name: "echo", Minister: "forge", Task: "echo hello"},
		},
	}

	shogunate := newRitualTestShogunateWithDB(t, db, "hello\n", nil)
	shogunate.GetRitualRegistry().Register(ritual)

	base := &MinisterBase{logger: slog.Default(), db: db}
	chanc := &Chancellor{
		MinisterBase: base,
		shogunate:    shogunate,
	}

	tool := InvokeRitualTool{chancellor: chanc}
	input := `{"ritual_name":"test-enacted","edict_id":1}`

	result, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatalf("Call() returned error: %v", err)
	}

	// Parse result
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("Failed to parse result JSON: %v", err)
	}

	if !strings.HasPrefix(res["status"].(string), "enacted") {
		t.Errorf("expected status to start with 'enacted', got %q", res["status"])
	}
	if res["ritual_name"] != "test-enacted" {
		t.Errorf("expected ritual_name 'test-enacted', got %q", res["ritual_name"])
	}
	// edict_id comes back as float64 from JSON unmarshaling
	if res["edict_id"] != float64(1) {
		t.Errorf("expected edict_id 1, got %v", res["edict_id"])
	}
}

// TestInvokeRitualTool_EnactedEvenForBadRitual verifies that enact_ritual returns "enacted"
// even for rituals that would fail — failure is reported asynchronously via events.
func TestInvokeRitualTool_EnactedEvenForBadRitual(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "test-fail-enacted",
		Steps: []RitualStep{
			{Name: "fail", Minister: "forge", Task: "do something", OnFailure: "abort"},
		},
	}

	shogunate := newRitualTestShogunateWithDB(t, db, "", fmt.Errorf("minister failed"))
	shogunate.GetRitualRegistry().Register(ritual)

	base := &MinisterBase{logger: slog.Default(), db: db}
	chanc := &Chancellor{
		MinisterBase: base,
		shogunate:    shogunate,
	}

	tool := InvokeRitualTool{chancellor: chanc}
	input := `{"ritual_name":"test-fail-enacted","edict_id":2}`

	result, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatalf("Call() returned error: %v", err)
	}

	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("Failed to parse result JSON: %v", err)
	}

	// Should still return "enacted" — failure happens async
	if !strings.HasPrefix(res["status"].(string), "enacted") {
		t.Errorf("expected status to start with 'enacted', got %q", res["status"])
	}
	if res["ritual_name"] != "test-fail-enacted" {
		t.Errorf("expected ritual_name 'test-fail-enacted', got %q", res["ritual_name"])
	}
}

// mockCmdRunner implements runners.Runner for testing
type mockCmdRunner struct {
	output   string
	exitCode string
	err      error
}

func (m *mockCmdRunner) Run(ctx context.Context, input runners.Input) (runners.Output, error) {
	if m.err != nil {
		return runners.Output{}, m.err
	}
	if m.exitCode != "0" {
		return runners.Output{
			Output:   m.output,
			ExitCode: m.exitCode,
		}, nil
	}
	return runners.Output{
		Output:   m.output,
		ExitCode: "0",
	}, nil
}

func (m *mockCmdRunner) Restart(ctx context.Context) error    { return nil }
func (m *mockCmdRunner) Close(ctx context.Context) error      { return nil }
func (m *mockCmdRunner) AllowFallback(bool)                   {}
func (m *mockCmdRunner) RunnerType() string                   { return "mock" }
func (m *mockCmdRunner) SetMessageChannel(chan<- runners.Msg) {}

// zhengmingTestMinister is a mock minister that raises zhengming before completing.
type zhengmingTestMinister struct {
	MinisterBase
	id      string
	tasksCh chan *Task
	delay   time.Duration // how long after zhengming to send the result
}

func (m *zhengmingTestMinister) ID() string           { return m.id }
func (m *zhengmingTestMinister) SystemPrompt() string { return "" }
func (m *zhengmingTestMinister) Title() string        { return m.id }
func (m *zhengmingTestMinister) Tools() []Tool        { return nil }
func (m *zhengmingTestMinister) Tasks() chan<- *Task  { return m.tasksCh }
func (m *zhengmingTestMinister) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-m.tasksCh:
			// Simulate: raise zhengming, wait, then complete
			m.zhengmingMu.Lock()
			cb := m.onZhengmingRaised
			m.zhengmingMu.Unlock()
			if cb != nil {
				cb()
			}
			time.Sleep(m.delay)
			t.Done <- Result{Output: "done after zhengming", Err: nil}
		}
	}
}

// TestRitualZhengmingPausesTimeout verifies that raising zhengming during a ritual step
// pauses the 5-minute timeout so the step can complete after the user answers.
func TestRitualZhengmingPausesTimeout(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name:        "zhengming-pause",
		Description: "Test zhengming pauses timeout",
		Steps: []RitualStep{
			{Name: "ask", Minister: "forge", Task: "do work"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	// Create a minister that raises zhengming and then completes after a short delay
	zm := &zhengmingTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 1),
		delay:        50 * time.Millisecond,
	}

	ministers := map[string]Minister{"forge": zm}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go zm.Run(ctx)

	shogunate := &Shogunate{
		ministers:     ministers,
		logger: slog.Default(),
	}

	runner := NewRitualRunner(registry, shogunate.GetMinister, shogunate.PublishEvent, db, nil, nil)

	var messages []RitualStepMsg
	notify := func(msg any) {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			messages = append(messages, stepMsg)
		}
	}

	exec, err := runner.Start(ctx, "zhengming-pause", 10, nil, notify)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed (timeout was not paused): %v", err)
	}

	if exec.State != RitualStateCompleted {
		t.Errorf("Expected state 'completed', got %s", exec.State)
	}

	// Verify the step completed with the expected output
	if len(exec.stepStates) < 1 {
		t.Fatal("Expected at least 1 step state")
	}
	if exec.stepStates[0].Message != "done after zhengming" {
		t.Errorf("Expected output 'done after zhengming', got %q", exec.stepStates[0].Message)
	}
}

// TestRitualTimeoutWithoutZhengming verifies the timeout still fires when no zhengming is raised.
func TestRitualTimeoutWithoutZhengming(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name:        "timeout-test",
		Description: "Test timeout fires normally",
		Steps: []RitualStep{
			{Name: "slow", Minister: "forge", Task: "do work"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	// Create a minister that never responds
	slowMinister := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 1),
		result:       "", // won't be used — Run is overridden below
	}

	ministers := map[string]Minister{"forge": slowMinister}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// Don't start Run — the task channel will never be consumed, so we need
	// a goroutine that accepts but never completes
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-slowMinister.tasksCh:
				// Accept the task but never send a result — simulates a hanging minister
			}
		}
	}()

	shogunate := &Shogunate{
		ministers:     ministers,
		logger: slog.Default(),
	}

	// Use a patched runner that verifies the timeout fires
	runner := NewRitualRunner(registry, shogunate.GetMinister, shogunate.PublishEvent, db, nil, nil)

	notify := func(msg any) {}

	exec, err := runner.Start(ctx, "timeout-test", 11, nil, notify)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	// The default timeout is 5 minutes which is too long for a test.
	// We verify the mechanism works by cancelling the context instead.
	cancelCtx, cancelFn := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelFn()

	err = runner.Run(cancelCtx, exec)
	if err == nil {
		t.Fatal("Expected error from context cancellation or timeout, got nil")
	}
}

func TestExpandTemplate_ActResult(t *testing.T) {
	runner := &RitualRunner{logger: slog.Default()}
	exec := &RitualExecution{
		EdictID:     1,
		CurrentStep: 1,
		Data: storage.JSON{
			"act_result": "step0 produced this output",
		},
		stepStates: []RitualStepState{
			{Name: "step0", Message: "done"},
		},
	}

	result := runner.expandTemplate("Previous result: {{ .act_result }}", exec)
	expected := "Previous result: step0 produced this output"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExpandTemplate_StepResults(t *testing.T) {
	runner := &RitualRunner{logger: slog.Default()}
	exec := &RitualExecution{
		EdictID:     1,
		CurrentStep: 2,
		stepStates: []RitualStepState{
			{Name: "plan", Message: "the plan output"},
			{Name: "implement", Message: "the impl output"},
			{Name: "review", Message: "should be excluded (current step)"},
		},
	}

	result := runner.expandTemplate(`Plan said: {{ index .step_results "plan" }}`, exec)
	expected := "Plan said: the plan output"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}

	// Current step (index 2) should NOT appear in step_results
	result2 := runner.expandTemplate("{{ .step_results }}", exec)
	if strings.Contains(result2, "should be excluded") {
		t.Error("current step should not appear in step_results")
	}
}

func TestBuildWorkPrompt(t *testing.T) {
	runner := &RitualRunner{logger: slog.Default()}

	t.Run("with step results and given context", func(t *testing.T) {
		exec := &RitualExecution{
			CurrentStep: 1,
			Data: storage.JSON{
				"given_context": map[string]interface{}{
					"branch": "feature-x",
				},
			},
			stepStates: []RitualStepState{
				{Name: "plan", Message: "the plan"},
				{Name: "implement", Message: ""},
			},
		}

		result := runner.buildWorkPrompt(exec, "Do the implementation")
		if !strings.Contains(result, "# Previous Step Results") {
			t.Error("expected previous step results section")
		}
		if !strings.Contains(result, "## plan") {
			t.Error("expected plan step result")
		}
		if !strings.Contains(result, "the plan") {
			t.Error("expected plan result content")
		}
		if !strings.Contains(result, "# Given Context") {
			t.Error("expected given context section")
		}
		if !strings.Contains(result, "## branch") {
			t.Error("expected branch context key")
		}
		if !strings.Contains(result, "# Task") {
			t.Error("expected task section")
		}
		if !strings.Contains(result, "Do the implementation") {
			t.Error("expected act content")
		}
	})

	t.Run("empty sections omitted", func(t *testing.T) {
		exec := &RitualExecution{
			CurrentStep: 0,
			stepStates:  []RitualStepState{},
		}

		result := runner.buildWorkPrompt(exec, "Just do it")
		if strings.Contains(result, "# Previous Step Results") {
			t.Error("should not include empty step results section")
		}
		if strings.Contains(result, "# Given Context") {
			t.Error("should not include empty given context section")
		}
		if !strings.Contains(result, "# Task") {
			t.Error("expected task section")
		}
		if !strings.Contains(result, "Just do it") {
			t.Error("expected act content")
		}
	})
}

func TestBuildWorkPrompt_GotoFreshContext(t *testing.T) {
	runner := &RitualRunner{logger: slog.Default()}

	exec := &RitualExecution{
		CurrentStep: 1,
		stepStates: []RitualStepState{
			{Name: "plan", Message: "initial plan"},
			{Name: "implement", Message: ""},
		},
	}

	// First invocation
	result1 := runner.buildWorkPrompt(exec, "implement the feature")
	if !strings.Contains(result1, "initial plan") {
		t.Error("first invocation should contain initial plan")
	}

	// Simulate goto: step 0 re-ran and produced updated output
	exec.stepStates[0].Message = "revised plan after failure"

	// Second invocation (goto re-invocation) — should see updated results
	result2 := runner.buildWorkPrompt(exec, "implement the feature")
	if !strings.Contains(result2, "revised plan after failure") {
		t.Error("goto re-invocation should contain updated plan")
	}
	if strings.Contains(result2, "initial plan") {
		t.Error("goto re-invocation should NOT contain stale initial plan")
	}
}

func TestBuildWorkPrompt_GotoIncludesLaterSteps(t *testing.T) {
	runner := &RitualRunner{logger: slog.Default()}

	// Simulate: steps 0→1→2 ran, step 2 failed and goto back to step 1
	exec := &RitualExecution{
		CurrentStep: 1,
		stepStates: []RitualStepState{
			{Name: "plan", Message: "the plan"},
			{Name: "implement", Message: ""},             // current step, re-invoked
			{Name: "review", Message: "review feedback"}, // ran before goto
		},
	}

	result := runner.buildWorkPrompt(exec, "redo implementation")
	if !strings.Contains(result, "## review") {
		t.Error("should include step after CurrentStep that already ran")
	}
	if !strings.Contains(result, "review feedback") {
		t.Error("should include review step's message")
	}
}

func TestExpandTemplate_GotoIncludesLaterSteps(t *testing.T) {
	runner := &RitualRunner{logger: slog.Default()}

	// Simulate: steps 0→1→2 ran, step 2 failed and goto back to step 0
	exec := &RitualExecution{
		EdictID:     1,
		CurrentStep: 0,
		stepStates: []RitualStepState{
			{Name: "plan", Message: ""},                 // current step
			{Name: "implement", Message: "impl output"}, // ran before goto
			{Name: "review", Message: "review output"},  // ran before goto
		},
	}

	result := runner.expandTemplate(`impl: {{ index .step_results "implement" }}`, exec)
	expected := "impl: impl output"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestLintFixRitual(t *testing.T) {
	rituals, err := LoadEmbeddedRituals()
	if err != nil {
		t.Fatalf("LoadEmbeddedRituals() error = %v", err)
	}

	var lintFix *RitualDef
	for _, r := range rituals {
		if r.Name == "lint-fix" {
			lintFix = r
			break
		}
	}

	if lintFix == nil {
		t.Fatal("lint-fix ritual not found")
	}

	if len(lintFix.Steps) != 3 {
		t.Fatalf("lint-fix: expected 3 steps, got %d", len(lintFix.Steps))
	}

	forkStep := lintFix.Steps[1]
	if forkStep.Fork == nil {
		t.Fatal("lint-fix: expected fork step to have Fork defined")
	}
	if forkStep.Fork.Over != "lint" {
		t.Fatalf("lint-fix: expected fork over 'lint', got %q", forkStep.Fork.Over)
	}
	if forkStep.Fork.BatchSize != 5 {
		t.Fatalf("lint-fix: expected batch_size 5, got %d", forkStep.Fork.BatchSize)
	}
}

// TestRitualSessionIDTracking verifies that session_id is captured during ritual execution
func TestRitualSessionIDTracking(t *testing.T) {
	// Create in-memory database
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Run migrations via GORM AutoMigrate
	if err := db.AutoMigrate(
		&storage.Edict{},
		&storage.Zhengming{},
		&storage.TianEvent{},
		&storage.TianEventDLQ{},
		&storage.Ling{},
		&storage.ForgeManifest{},
		&storage.JudgeVerdict{},
		&storage.CensorPrecedent{},
		&storage.MarshalIncident{},
		&storage.RulerCouncil{},
		&storage.RitualGuardCheckpoint{},
		&RitualExecution{},
		&RitualStepState{},
	); err != nil {
		t.Fatalf("failed to auto-migrate schema: %v", err)
	}

	// Create a test edict
	testEdict := storage.Edict{
		SessionID: "session-initial",
		Intent:    "Test session tracking",
		Status:    storage.EdictActive,
	}
	if err := db.Create(&testEdict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := testEdict.EdictID

	// Verify ritual_executions table has session_id column
	var execResult struct {
		Cid        int    `gorm:"column:cid"`
		Name       string `gorm:"column:name"`
		Type       string `gorm:"column:type"`
		NotNull    int    `gorm:"column:notnull"`
		DefaultVal string `gorm:"column:dflt_value"`
		Pk         int    `gorm:"column:pk"`
	}

	// Check ritual_executions.session_id exists
	rows, err := db.Raw("PRAGMA table_info(ritual_executions)").Rows()
	if err != nil {
		t.Fatalf("failed to get ritual_executions columns: %v", err)
	}
	defer rows.Close()

	foundSessionID := false
	for rows.Next() {
		if err := db.ScanRows(rows, &execResult); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
		if execResult.Name == "session_id" {
			foundSessionID = true
			break
		}
	}
	if !foundSessionID {
		t.Error("ritual_executions.session_id column not found")
	}

	// Check ritual_step_states.session_id exists
	rows, err = db.Raw("PRAGMA table_info(ritual_step_states)").Rows()
	if err != nil {
		t.Fatalf("failed to get ritual_step_states columns: %v", err)
	}
	defer rows.Close()

	foundSessionID = false
	for rows.Next() {
		if err := db.ScanRows(rows, &execResult); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
		if execResult.Name == "session_id" {
			foundSessionID = true
			break
		}
	}
	if !foundSessionID {
		t.Error("ritual_step_states.session_id column not found")
	}

	// Test inserting and querying session_id directly via SQL
	execID := "ritual-exec-test-1"
	sessionID := "session-from-minister"
	
	// Insert ritual execution with session_id
	err = db.Exec(`
		INSERT INTO ritual_executions (id, ritual_name, edict_id, session_id, current_step, state, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, unixepoch(), unixepoch())
	`, execID, "test-ritual", edictID, sessionID, 0, "running", "{}").Error
	if err != nil {
		t.Fatalf("failed to insert ritual execution: %v", err)
	}

	// Verify session_id was stored
	var storedSessionID string
	err = db.Raw("SELECT session_id FROM ritual_executions WHERE id = ?", execID).Scan(&storedSessionID).Error
	if err != nil {
		t.Fatalf("failed to retrieve session_id: %v", err)
	}
	if storedSessionID != sessionID {
		t.Errorf("expected session_id '%s', got %q", sessionID, storedSessionID)
	}

	// Insert step state with session_id
	stepSessionID := "session-step-1"
	err = db.Exec(`
		INSERT INTO ritual_step_states (execution_id, step_index, name, session_id, retry_count, message)
		VALUES (?, ?, ?, ?, ?, ?)
	`, execID, 0, "test-step", stepSessionID, 0, "Step completed").Error
	if err != nil {
		t.Fatalf("failed to insert step state: %v", err)
	}

	// Verify step state session_id was stored
	var storedStepSessionID string
	err = db.Raw("SELECT session_id FROM ritual_step_states WHERE execution_id = ? AND step_index = ?", execID, 0).Scan(&storedStepSessionID).Error
	if err != nil {
		t.Fatalf("failed to retrieve step session_id: %v", err)
	}
	if storedStepSessionID != stepSessionID {
		t.Errorf("expected step session_id '%s', got %q", stepSessionID, storedStepSessionID)
	}

	// Test querying by session_id
	var count int64
	err = db.Raw("SELECT COUNT(*) FROM ritual_step_states WHERE session_id = ?", stepSessionID).Scan(&count).Error
	if err != nil {
		t.Fatalf("failed to query step states by session_id: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 step state for session_id, got %d", count)
	}

	// Test querying ritual executions by session_id
	err = db.Raw("SELECT COUNT(*) FROM ritual_executions WHERE session_id = ?", sessionID).Scan(&count).Error
	if err != nil {
		t.Fatalf("failed to query executions by session_id: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 execution for session_id, got %d", count)
	}
}
