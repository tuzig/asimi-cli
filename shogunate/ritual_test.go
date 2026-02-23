package shogunate

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afittestide/asimi/internal/runners"
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
					{Name: "step1", Task: "do something"},
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
			name: "cmd step without command",
			ritual: &RitualDef{
				Name: "bad-cmd",
				Steps: []RitualStep{
					{Name: "step1", Type: "cmd"},
				},
			},
			wantErr: true,
			errMsg:  "requires command",
		},
		{
			name: "gate step without condition",
			ritual: &RitualDef{
				Name: "bad-gate",
				Steps: []RitualStep{
					{Name: "step1", Type: "gate"},
				},
			},
			wantErr: true,
			errMsg:  "requires condition",
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
    type: prompt
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
    type: unknown_type
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

	if len(rituals) != 6 {
		names := make([]string, len(rituals))
		for i, r := range rituals {
			names[i] = r.Name
		}
		t.Errorf("expected 6 embedded rituals, got %d: %v", len(rituals), names)
	}

	// Check key rituals exist
	var foundSwift, foundGrand, foundWakeup, foundOrchestration, foundReport, foundReview bool
	for _, r := range rituals {
		switch r.Name {
		case "swift-strike":
			foundSwift = true
			if len(r.Steps) != 2 {
				t.Errorf("swift-strike: expected 2 steps, got %d", len(r.Steps))
			}
			if r.Steps[0].Minister != "forge" {
				t.Errorf("swift-strike: expected first step minister 'forge', got %q", r.Steps[0].Minister)
			}
			if r.Steps[1].Minister != "judge" {
				t.Errorf("swift-strike: expected second step minister 'judge', got %q", r.Steps[1].Minister)
			}
			// Verify Background
			if len(r.Background) == 0 {
				t.Error("swift-strike: expected background")
			}
			if r.Background[0] != "the edict details" {
				t.Errorf("swift-strike: expected background 'the edict details', got %q", r.Background[0])
			}
			// Forge step should have no step-level given (hoisted to background)
			if len(r.Steps[0].Given) != 0 {
				t.Errorf("swift-strike forge: expected no step-level given (hoisted), got %v", r.Steps[0].Given)
			}
			// Verify Act is used (not Task)
			if r.Steps[0].Act == "" {
				t.Error("swift-strike forge step: expected act field")
			}

		case "grand-campaign":
			foundGrand = true
			if len(r.Steps) != 4 {
				t.Errorf("grand-campaign: expected 4 steps, got %d", len(r.Steps))
			}
			expectedMinisters := []string{"strategist", "forge", "judge", "censor"}
			for i, expected := range expectedMinisters {
				if r.Steps[i].Minister != expected {
					t.Errorf("grand-campaign: step %d expected minister %q, got %q", i, expected, r.Steps[i].Minister)
				}
			}
			// Verify ritual-level defaults
			if r.MaxRetries != 3 {
				t.Errorf("grand-campaign: expected max_retries 3, got %d", r.MaxRetries)
			}

		case "review":
			foundReview = true
			if len(r.Steps) != 3 {
				t.Errorf("review: expected 3 steps, got %d", len(r.Steps))
			}
			// Background: git diff and just test at ritual level
			if len(r.Background) != 2 || r.Background[0] != "!git diff" || r.Background[1] != "!just test" {
				t.Errorf("review: expected background ['!git diff', '!just test'], got %v", r.Background)
			}
			// Judge step should have no then and no step-level given
			if len(r.Steps[0].Given) != 0 {
				t.Errorf("review judge: expected no step-level given, got %v", r.Steps[0].Given)
			}
			if len(r.Steps[0].Then) != 0 {
				t.Errorf("review judge: expected no then entries, got %v", r.Steps[0].Then)
			}
			// Censor depends on judge
			if len(r.Steps[1].DependsOn) != 1 || r.Steps[1].DependsOn[0] != "judge" {
				t.Errorf("review censor: expected depends_on [judge], got %v", r.Steps[1].DependsOn)
			}
			// Report step is a prompt
			if r.Steps[2].Type != "prompt" {
				t.Errorf("review report: expected type 'prompt', got %q", r.Steps[2].Type)
			}

		case "wakeup":
			foundWakeup = true
		case "grand-orchestration":
			foundOrchestration = true
		case "report_failure":
			foundReport = true
		}
	}

	if !foundSwift {
		t.Error("swift-strike ritual not found")
	}
	if !foundGrand {
		t.Error("grand-campaign ritual not found")
	}
	if !foundReview {
		t.Error("review ritual not found")
	}
	if !foundWakeup {
		t.Error("wakeup ritual not found")
	}
	if !foundOrchestration {
		t.Error("grand-orchestration ritual not found")
	}
	if !foundReport {
		t.Error("report_failure ritual not found")
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
			"edict_id": {Type: "string", Required: true},
		},
		Steps: []RitualStep{
			{
				Name:    "echo",
				Type:    "cmd",
				Command: "echo hello",
				Task:    "Echo hello",
			},
		},
	}

	// Create and populate registry
	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("Failed to register ritual: %v", err)
	}

	// Create a mock runner that succeeds
	mockRunner := &mockCmdRunner{output: "hello\n", exitCode: "0"}

	// Create ritual runner (no shogunate needed for cmd steps)
	runner := NewRitualRunner(registry, nil, db, mockRunner, nil)

	// Collect messages from the stream
	var messages []any
	notify := func(msg any) {
		messages = append(messages, msg)
	}

	// Start the ritual
	ctx := context.Background()
	exec, err := runner.Start(ctx, "test-stream", "test-edict-1", map[string]string{"edict_id": "test-edict-1"}, notify)
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
		startedCount   int
		completedCount int
		ritualComplete int
	)

	for _, msg := range messages {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			t.Logf("Received RitualStepMsg: ritual=%s step=%s status=%s", stepMsg.RitualName, stepMsg.StepName, stepMsg.Status)
			switch stepMsg.Status {
			case "started":
				startedCount++
				if stepMsg.StepName != "echo" {
					t.Errorf("Expected step name 'echo', got %q", stepMsg.StepName)
				}
				if stepMsg.RitualName != "test-stream" {
					t.Errorf("Expected ritual name 'test-stream', got %q", stepMsg.RitualName)
				}
			case "completed":
				completedCount++
			case "ritual_completed":
				ritualComplete++
			}
		}
	}

	// Verify message counts
	if startedCount != 1 {
		t.Errorf("Expected 1 'started' message, got %d", startedCount)
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
			{Name: "step1", Type: "cmd", Command: "echo one"},
			{Name: "step2", Type: "cmd", Command: "echo two", DependsOn: []string{"step1"}},
			{Name: "step3", Type: "cmd", Command: "echo three", DependsOn: []string{"step2"}},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	mockRunner := &mockCmdRunner{output: "ok\n", exitCode: "0"}
	runner := NewRitualRunner(registry, nil, db, mockRunner, nil)

	var messages []RitualStepMsg
	notify := func(msg any) {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			messages = append(messages, stepMsg)
		}
	}

	ctx := context.Background()
	exec, err := runner.Start(ctx, "multi-step", "edict-multi", nil, notify)
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should have: started(3) + completed(3) + ritual_completed(1) = 7 messages
	expectedCount := 7
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
			{Name: "fail-step", Type: "cmd", Command: "exit 1", OnFailure: "abort"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	// Mock runner that fails
	mockRunner := &mockCmdRunner{output: "error!", exitCode: "1", err: nil}
	runner := NewRitualRunner(registry, nil, db, mockRunner, nil)

	var messages []RitualStepMsg
	notify := func(msg any) {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			messages = append(messages, stepMsg)
		}
	}

	ctx := context.Background()
	exec, _ := runner.Start(ctx, "fail-ritual", "edict-fail", nil, notify)
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
	runner := NewRitualRunner(registry, nil, db, nil, nil)

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
	runner := NewRitualRunner(registry, nil, db, mockRunner, nil)

	exec := &RitualExecution{
		ID:         "test-exec",
		RitualName: "test",
		EdictID:    "test-edict",
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
	runner := NewRitualRunner(registry, nil, db, mockRunner, nil)

	exec := &RitualExecution{
		ID:         "test-exec",
		RitualName: "test",
		EdictID:    "test-edict",
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
	runner = NewRitualRunner(registry, nil, db, failRunner, nil)

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
				Name:    "build",
				Type:    "cmd",
				Command: "echo build",
				Then:    []string{"!echo check1", "!exit 1"},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	callCount := 0
	mockRunner := &mockCallCountRunner{
		results: []runners.Output{
			{Output: "build\n", ExitCode: "0"}, // cmd step
			{Output: "ok\n", ExitCode: "0"},    // first then
			{Output: "FAIL\n", ExitCode: "1"},  // second then
		},
	}
	runner := NewRitualRunner(registry, nil, db, mockRunner, nil)
	_ = callCount

	ctx := context.Background()
	exec, err := runner.Start(ctx, "multi-then", "edict-test", nil, nil)
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

	if len(rituals) != 6 {
		names := make([]string, len(rituals))
		for i, r := range rituals {
			names[i] = r.Name
		}
		t.Errorf("expected 6 builtin rituals, got %d: %v", len(rituals), names)
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
			// Judge step should have Then
			if len(r.Steps[1].Then) == 0 {
				t.Error("swift-strike judge step: expected then entries")
			}
			if r.Steps[1].Then[0] != "!just test" {
				t.Errorf("swift-strike judge then: expected '!just test', got %q", r.Steps[1].Then[0])
			}
		}
	}
}

func TestBackgroundGiven(t *testing.T) {
	db := setupRitualTestDB(t)

	// Ritual with background given (bash) and a cmd step
	ritual := &RitualDef{
		Name:       "bg-test",
		Background: []string{"!echo background-data"},
		Steps: []RitualStep{
			{Name: "work", Type: "cmd", Command: "echo step-done"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	mockRunner := &mockCallCountRunner{
		results: []runners.Output{
			{Output: "background-data\n", ExitCode: "0"}, // background given
			{Output: "step-done\n", ExitCode: "0"},       // cmd step
		},
	}
	runner := NewRitualRunner(registry, nil, db, mockRunner, nil)

	var messages []RitualStepMsg
	notify := func(msg any) {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			messages = append(messages, stepMsg)
		}
	}

	ctx := context.Background()
	exec, err := runner.Start(ctx, "bg-test", "edict-bg", nil, notify)
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
	err = db.AutoMigrate(&RitualExecution{}, &RitualStepState{})
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	return db
}

// TestInvokeRitualTool_Blocking verifies that enact_ritual blocks until the ritual completes
// and returns full results with step_results.
func TestInvokeRitualTool_Blocking(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name:        "test-blocking",
		Description: "A test ritual for blocking behavior",
		Steps: []RitualStep{
			{Name: "echo", Type: "cmd", Command: "echo hello"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	mockRunner := &mockCmdRunner{output: "hello\n", exitCode: "0"}
	ritualRunner := NewRitualRunner(registry, nil, db, mockRunner, nil)

	// Wire up a minimal Chancellor + Shogunate
	shogunate := &Shogunate{
		ritualRunner: ritualRunner,
	}
	chanc := &Chancellor{
		MinisterBase: MinisterBase{logger: slog.Default()},
		shogunate:    shogunate,
	}

	tool := InvokeRitualTool{chancellor: chanc}
	input := `{"ritual_name":"test-blocking","edict_id":"edict-block-1"}`

	result, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatalf("Call() returned error: %v", err)
	}

	// Parse result
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("Failed to parse result JSON: %v", err)
	}

	if res["status"] != "completed" {
		t.Errorf("expected status 'completed', got %q", res["status"])
	}
	if res["ritual_name"] != "test-blocking" {
		t.Errorf("expected ritual_name 'test-blocking', got %q", res["ritual_name"])
	}
	if res["execution_id"] == nil || res["execution_id"] == "" {
		t.Error("expected non-empty execution_id")
	}

	// Verify step_results are present
	stepResults, ok := res["step_results"].([]any)
	if !ok {
		t.Fatalf("expected step_results array, got %T", res["step_results"])
	}
	if len(stepResults) != 1 {
		t.Errorf("expected 1 step result, got %d", len(stepResults))
	}
}

// TestInvokeRitualTool_BlockingFailure verifies that a failed ritual returns failure details
// as tool output (not a Go error) so the LLM can reason about it.
func TestInvokeRitualTool_BlockingFailure(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "test-fail-blocking",
		Steps: []RitualStep{
			{Name: "fail", Type: "cmd", Command: "exit 1", OnFailure: "abort"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	mockRunner := &mockCmdRunner{output: "error!", exitCode: "1"}
	ritualRunner := NewRitualRunner(registry, nil, db, mockRunner, nil)

	shogunate := &Shogunate{
		ritualRunner: ritualRunner,
	}
	chanc := &Chancellor{
		MinisterBase: MinisterBase{logger: slog.Default()},
		shogunate:    shogunate,
	}

	tool := InvokeRitualTool{chancellor: chanc}
	input := `{"ritual_name":"test-fail-blocking","edict_id":"edict-fail-1"}`

	result, err := tool.Call(context.Background(), input)
	// Should NOT return a Go error — failure is returned as tool output
	if err != nil {
		t.Fatalf("Call() returned Go error (should return failure as tool output): %v", err)
	}

	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("Failed to parse result JSON: %v", err)
	}

	if res["status"] != "failed" {
		t.Errorf("expected status 'failed', got %q", res["status"])
	}
	if res["error"] == nil || res["error"] == "" {
		t.Error("expected non-empty error field")
	}

	// Verify step_results are present even on failure
	stepResults, ok := res["step_results"].([]any)
	if !ok {
		t.Fatalf("expected step_results array, got %T", res["step_results"])
	}
	if len(stepResults) != 1 {
		t.Errorf("expected 1 step result, got %d", len(stepResults))
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
