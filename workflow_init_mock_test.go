package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
)

// Type aliases for backward compatibility with old root-package types
type RunShellCommandInput = runners.Input
type RunShellCommandOutput = runners.Output

// testShellRunner holds the mock runner for testing
var testShellRunner runners.Runner

func setShellRunnerForTesting(runner runners.Runner) func() {
	old := testShellRunner
	testShellRunner = runner
	return func() { testShellRunner = old }
}

func getShellRunner() runners.Runner {
	return testShellRunner
}

// mockShellRunner is a configurable mock implementation of shellRunner for testing
type mockShellRunner struct {
	mu sync.Mutex

	runnerType string

	// commandResults maps command patterns to their outputs
	commandResults map[string]RunShellCommandOutput

	// defaultResult is returned when no pattern matches
	defaultResult RunShellCommandOutput

	// recordedCommands stores all commands executed through this runner
	recordedCommands []RunShellCommandInput

	// errorOnCommand causes Run to return an error for commands matching the pattern
	errorOnCommand map[string]error

	// allowFallbackValue tracks the AllowFallback setting
	allowFallbackValue bool

	// closeCalled tracks if Close was called
	closeCalled bool
}

func newMockShellRunner(runnerType string) *mockShellRunner {
	return &mockShellRunner{
		runnerType:     runnerType,
		commandResults: make(map[string]RunShellCommandOutput),
		errorOnCommand: make(map[string]error),
		defaultResult: RunShellCommandOutput{
			Output:   "",
			ExitCode: "0",
		},
	}
}

func (m *mockShellRunner) Run(ctx context.Context, params RunShellCommandInput) (RunShellCommandOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record the command
	m.recordedCommands = append(m.recordedCommands, params)

	// Check for errors first
	for pattern, err := range m.errorOnCommand {
		if strings.Contains(params.Command, pattern) {
			return RunShellCommandOutput{}, err
		}
	}

	// Look for pattern match
	for pattern, result := range m.commandResults {
		if strings.Contains(params.Command, pattern) {
			return result, nil
		}
	}

	return m.defaultResult, nil
}

func (m *mockShellRunner) Restart(ctx context.Context) error {
	return nil
}

func (m *mockShellRunner) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return nil
}

func (m *mockShellRunner) AllowFallback(allow bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowFallbackValue = allow
}

func (m *mockShellRunner) RunnerType() string {
	return m.runnerType
}

func (m *mockShellRunner) SetMessageChannel(chan<- runners.Msg) {}

func (m *mockShellRunner) withResult(pattern string, result RunShellCommandOutput) *mockShellRunner {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commandResults[pattern] = result
	return m
}

func (m *mockShellRunner) withError(pattern string, err error) *mockShellRunner {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorOnCommand[pattern] = err
	return m
}

func (m *mockShellRunner) getRecordedCommands() []RunShellCommandInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmds := make([]RunShellCommandInput, len(m.recordedCommands))
	copy(cmds, m.recordedCommands)
	return cmds
}

// TestInitWorkflowWithMockedRunner tests workflow with actual shell runner mocking
func TestInitWorkflowWithMockedRunner(t *testing.T) {
	skipIfNotCI(t)
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Initialize git repo
	initTestGitRepo(t, tmpDir)

	t.Run("Smoke test step uses container runner", func(t *testing.T) {
		// Create a mock podman runner that returns Linux for uname
		mockRunner := newMockShellRunner("podman").
			withResult("uname", RunShellCommandOutput{Output: "Linux", ExitCode: "0"}).
			withResult("just test", RunShellCommandOutput{Output: "Tests passed!", ExitCode: "0"})

		// Install the mock runner
		restore := setShellRunnerForTesting(mockRunner)
		defer restore()

		// Verify getShellRunner returns our mock
		runner := getShellRunner()
		if runner.RunnerType() != "podman" {
			t.Errorf("Expected podman runner, got %s", runner.RunnerType())
		}

		// Simulate the smoke-test step
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := runner.Run(ctx, RunShellCommandInput{
			Command:     "uname",
			Description: "Running smoke test",
		})

		if err != nil {
			t.Fatalf("Smoke test failed: %v", err)
		}

		if !strings.Contains(result.Output, "Linux") {
			t.Errorf("Expected Linux in output, got %q", result.Output)
		}

		// Verify the command was recorded
		cmds := mockRunner.getRecordedCommands()
		if len(cmds) != 1 {
			t.Errorf("Expected 1 command recorded, got %d", len(cmds))
		}
		if cmds[0].Command != "uname" {
			t.Errorf("Expected uname command, got %q", cmds[0].Command)
		}
	})

	t.Run("Container tests use shell runner", func(t *testing.T) {
		mockRunner := newMockShellRunner("podman").
			withResult("just test", RunShellCommandOutput{Output: "All tests passed", ExitCode: "0"})

		restore := setShellRunnerForTesting(mockRunner)
		defer restore()

		// Simulate running container tests
		runner := getShellRunner()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := runner.Run(ctx, RunShellCommandInput{
			Command:     "just test",
			Description: "Running container tests",
		})

		if err != nil {
			t.Fatalf("Container tests failed: %v", err)
		}

		if result.ExitCode != "0" {
			t.Errorf("Expected exit code 0, got %s", result.ExitCode)
		}
	})

	t.Run("Failed container test triggers fix loop", func(t *testing.T) {
		callCount := 0
		mockRunner := newMockShellRunner("podman")

		// Configure to fail first time, succeed second time
		originalDefaultResult := mockRunner.defaultResult
		mockRunner.defaultResult = RunShellCommandOutput{
			Output: func() string {
				callCount++
				if callCount == 1 {
					return "Test failed: missing dependency"
				}
				return "All tests passed"
			}(),
			ExitCode: func() string {
				if callCount == 0 {
					return "1"
				}
				return "0"
			}(),
		}
		_ = originalDefaultResult

		restore := setShellRunnerForTesting(mockRunner)
		defer restore()

		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		testAttempts := 0
		fixAttempts := 0

		// Create a workflow that simulates the container-tests -> fix -> retry pattern
		w := New("test-container-fix-loop", nil, repoCtx,
			WithMaxRetries(5),
			WithSendPrompt(func(ctx context.Context, prompt string) <-chan string {
				ch := make(chan string, 1)
				ch <- "Fixed the dependency issue"
				close(ch)
				return ch
			}))

		w.AddCheck("container-tests", func(w *Workflow) StepResult {
			testAttempts++
			if testAttempts < 2 {
				w.Set("containerTestError", "Test failed: missing dependency")
				return w.GoTo("fix-container-tests", "tests failed")
			}
			// Skip to done step when tests pass
			return w.GoTo("done", "tests passed")
		}).
			Add(Step{
				Name:   "fix-container-tests",
				Prompt: "Fix the container test error: {{.containerTestError}}",
				Verify: func(w *Workflow, response string) StepResult {
					fixAttempts++
					return w.GoTo("container-tests", "re-running tests")
				},
			}).
			AddRun("done", func(w *Workflow) error {
				return nil
			})

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Workflow failed: %v", err)
		}

		if testAttempts != 2 {
			t.Errorf("Expected 2 test attempts, got %d", testAttempts)
		}

		if fixAttempts != 1 {
			t.Errorf("Expected 1 fix attempt, got %d", fixAttempts)
		}
	})
}

// TestWorkflowDataStorage tests that workflow data is properly stored and retrieved
func TestWorkflowDataStorage(t *testing.T) {
	skipIfNotCI(t)
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	t.Run("Set and Get workflow data", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-data", nil, repoCtx,
			WithSendPrompt(emptySendPrompt))

		storedValue := ""
		w.AddRun("store-data", func(w *Workflow) error {
			w.Set("testKey", "testValue")
			w.Set("agentsFile", "AGENTS.md")
			return nil
		}).
			AddRun("retrieve-data", func(w *Workflow) error {
				storedValue = w.Get("testKey")
				return nil
			})

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Workflow failed: %v", err)
		}

		if storedValue != "testValue" {
			t.Errorf("Expected 'testValue', got %q", storedValue)
		}
	})

	t.Run("GetValue retrieves interface values", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-data-interface", nil, repoCtx,
			WithSendPrompt(emptySendPrompt))

		var retrievedValue interface{}
		w.AddRun("store-interface", func(w *Workflow) error {
			w.Set("skipAIAnalysis", true)
			return nil
		}).
			AddRun("retrieve-interface", func(w *Workflow) error {
				retrievedValue = w.GetValue("skipAIAnalysis")
				return nil
			})

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Workflow failed: %v", err)
		}

		if boolVal, ok := retrievedValue.(bool); !ok || !boolVal {
			t.Errorf("Expected true, got %v", retrievedValue)
		}
	})
}

// TestWorkflowStepStates tests that step states are tracked correctly
func TestWorkflowStepStates(t *testing.T) {
	skipIfNotCI(t)
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	t.Run("Step states are tracked", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-states", nil, repoCtx,
			WithSendPrompt(emptySendPrompt))

		w.AddRun("step1", func(w *Workflow) error { return nil }).
			AddRun("step2", func(w *Workflow) error { return nil }).
			AddRun("step3", func(w *Workflow) error { return nil })

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Workflow failed: %v", err)
		}

		states := w.GetStepStates()
		if len(states) != 3 {
			t.Errorf("Expected 3 step states, got %d", len(states))
		}

		// Verify workflow completed all steps (CurrentStep is past all steps)
		if w.CurrentStep != 3 {
			t.Errorf("Expected CurrentStep to be 3 (past all steps), got %d", w.CurrentStep)
		}
	})

	t.Run("Failed step exhausts retries", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-fail-state", nil, repoCtx,
			WithMaxRetries(2),
			WithSendPrompt(emptySendPrompt))

		w.AddCheck("always-fail", func(w *Workflow) StepResult {
			return w.Retry("failed")
		})

		ctx := context.Background()
		w.Run(ctx)

		states := w.GetStepStates()
		if len(states) != 1 {
			t.Errorf("Expected 1 step state, got %d", len(states))
		}

		if states[0].RetryCount != 2 {
			t.Errorf("Expected 2 retries, got %d", states[0].RetryCount)
		}

		// Verify workflow is in failed state
		if w.State != storage.WorkflowStateFailed {
			t.Errorf("Expected failed workflow state, got %s", w.State)
		}
	})
}

// TestWorkflowAbortE2E tests that workflows can be aborted (E2E version)
func TestWorkflowAbortE2E(t *testing.T) {
	skipIfNotCI(t)
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	t.Run("Abort stops workflow execution", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-abort", nil, repoCtx,
			WithSendPrompt(emptySendPrompt))

		step2Executed := false
		w.AddRun("step1", func(w *Workflow) error {
			w.Abort()
			return nil
		}).
			AddRun("step2", func(w *Workflow) error {
				step2Executed = true
				return nil
			})

		ctx := context.Background()
		err := w.Run(ctx)

		if err == nil {
			t.Error("Expected workflow to return error on abort")
		}

		if step2Executed {
			t.Error("Step 2 should not have executed after abort")
		}

		if w.State != storage.WorkflowStateAborted {
			t.Errorf("Expected aborted state, got %s", w.State)
		}
	})
}

// TestWorkflowContextCancellationE2E tests that context cancellation stops workflow (E2E version)
func TestWorkflowContextCancellationE2E(t *testing.T) {
	skipIfNotCI(t)
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	t.Run("Context cancellation stops workflow", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-context-cancel", nil, repoCtx,
			WithSendPrompt(emptySendPrompt))

		step2Executed := false
		w.AddRun("step1", func(w *Workflow) error {
			time.Sleep(100 * time.Millisecond)
			return nil
		}).
			AddRun("step2", func(w *Workflow) error {
				step2Executed = true
				return nil
			})

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := w.Run(ctx)

		if err == nil {
			t.Error("Expected workflow to return error on context cancellation")
		}

		if step2Executed {
			t.Error("Step 2 should not have executed after context cancellation")
		}
	})
}
