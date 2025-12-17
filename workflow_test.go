package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
)

func TestWorkflowBasicExecution(t *testing.T) {
	// Create a simple workflow without persistence
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-workflow", nil, repoInfo)

	// Track step execution
	executedSteps := []string{}

	// Add simple steps
	w.AddStep(Step{
		Name: "step-1",
		Prepare: func(w *Workflow) (map[string]interface{}, error) {
			executedSteps = append(executedSteps, "step-1-prepare")
			return map[string]interface{}{"key1": "value1"}, nil
		},
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-1-verify")
			return StepResult{NextOffset: 1, Message: "step 1 done"}
		},
	})

	w.AddStep(Step{
		Name: "step-2",
		Prepare: func(w *Workflow) (map[string]interface{}, error) {
			executedSteps = append(executedSteps, "step-2-prepare")
			// Verify data from step 1 is available
			if w.Get("key1") != "value1" {
				t.Error("Expected key1 to be value1")
			}
			return nil, nil
		},
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-2-verify")
			return StepResult{NextOffset: 1, Message: "step 2 done"}
		},
	})

	// Run workflow
	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Verify all steps executed
	expectedSteps := []string{
		"step-1-prepare", "step-1-verify",
		"step-2-prepare", "step-2-verify",
	}

	if len(executedSteps) != len(expectedSteps) {
		t.Errorf("Expected %d steps, got %d", len(expectedSteps), len(executedSteps))
	}

	for i, step := range expectedSteps {
		if i >= len(executedSteps) || executedSteps[i] != step {
			t.Errorf("Step %d: expected %s, got %s", i, step, executedSteps[i])
		}
	}

	// Verify workflow state
	if w.State != storage.WorkflowStateCompleted {
		t.Errorf("Expected workflow state to be completed, got %s", w.State)
	}
}

func TestWorkflowRetry(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-retry", nil, repoInfo, WithMaxRetries(3))

	retryCount := 0

	w.AddStep(Step{
		Name: "retry-step",
		Verify: func(w *Workflow, response string) StepResult {
			retryCount++
			if retryCount < 3 {
				return StepResult{NextOffset: 0, Message: "retry needed"}
			}
			return StepResult{NextOffset: 1, Message: "success after retries"}
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if retryCount != 3 {
		t.Errorf("Expected 3 retries, got %d", retryCount)
	}

	// Check step state
	states := w.GetStepStates()
	if len(states) != 1 {
		t.Fatalf("Expected 1 step state, got %d", len(states))
	}

	if states[0].RetryCount != 2 { // 2 retries before success
		t.Errorf("Expected retry count 2, got %d", states[0].RetryCount)
	}
}

func TestWorkflowMaxRetriesExceeded(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-max-retries", nil, repoInfo, WithMaxRetries(2))

	w.AddStep(Step{
		Name: "always-fail",
		Verify: func(w *Workflow, response string) StepResult {
			return StepResult{NextOffset: 0, Message: "always fails"}
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)

	if err == nil {
		t.Error("Expected workflow to fail due to max retries")
	}

	if w.State != storage.WorkflowStateFailed {
		t.Errorf("Expected workflow state to be failed, got %s", w.State)
	}
}

func TestWorkflowSkipSteps(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-skip", nil, repoInfo)

	executedSteps := []string{}

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-1")
			return StepResult{NextOffset: 2, Message: "skip step 2"} // Skip next step
		},
	})

	w.AddStep(Step{
		Name: "step-2-skipped",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-2")
			return StepResult{NextOffset: 1, Message: "should not run"}
		},
	})

	w.AddStep(Step{
		Name: "step-3",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-3")
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Step 2 should be skipped
	if len(executedSteps) != 2 {
		t.Errorf("Expected 2 steps executed, got %d: %v", len(executedSteps), executedSteps)
	}

	if executedSteps[0] != "step-1" || executedSteps[1] != "step-3" {
		t.Errorf("Unexpected step execution order: %v", executedSteps)
	}
}

func TestWorkflowGoBack(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-goback", nil, repoInfo)

	step2Runs := 0

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	w.AddStep(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			step2Runs++
			if step2Runs == 1 {
				return StepResult{NextOffset: -1, Message: "go back to step 1"} // Go back
			}
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Step 2 should have run twice (once before going back, once after)
	if step2Runs != 2 {
		t.Errorf("Expected step 2 to run 2 times, got %d", step2Runs)
	}
}

func TestWorkflowAbort(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-abort", nil, repoInfo)

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			w.Abort() // Abort during execution
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	w.AddStep(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			t.Error("Step 2 should not run after abort")
			return StepResult{NextOffset: 1, Message: "should not run"}
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)

	if err == nil {
		t.Error("Expected workflow to return error on abort")
	}

	if w.State != storage.WorkflowStateAborted {
		t.Errorf("Expected workflow state to be aborted, got %s", w.State)
	}
}

func TestWorkflowContextCancellation(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-cancel", nil, repoInfo)

	step1Done := make(chan struct{})

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			close(step1Done)
			// Wait a bit to allow cancellation
			time.Sleep(100 * time.Millisecond)
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	w.AddStep(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			t.Error("Step 2 should not run after cancellation")
			return StepResult{NextOffset: 1, Message: "should not run"}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after step 1 starts
	go func() {
		<-step1Done
		cancel()
	}()

	err := w.Run(ctx)

	if err == nil {
		t.Error("Expected workflow to return error on cancellation")
	}

	if w.State != storage.WorkflowStateAborted {
		t.Errorf("Expected workflow state to be aborted, got %s", w.State)
	}
}

func TestWorkflowDataPersistence(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-data", nil, repoInfo)

	w.AddStep(Step{
		Name: "set-data",
		Prepare: func(w *Workflow) (map[string]interface{}, error) {
			w.Set("manual-key", "manual-value")
			return map[string]interface{}{
				"prepare-key": "prepare-value",
			}, nil
		},
		Verify: func(w *Workflow, response string) StepResult {
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	w.AddStep(Step{
		Name: "check-data",
		Verify: func(w *Workflow, response string) StepResult {
			if w.Get("manual-key") != "manual-value" {
				t.Error("manual-key not persisted")
			}
			if w.Get("prepare-key") != "prepare-value" {
				t.Error("prepare-key not persisted")
			}
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}
}

func TestWorkflowProgress(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-progress", nil, repoInfo)

	progressUpdates := []struct {
		stepIndex int
		status    storage.StepStatus
	}{}

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	w.AddStep(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	// Set progress callback
	w.onProgress = func(stepIndex int, stepState StepState, message string) {
		progressUpdates = append(progressUpdates, struct {
			stepIndex int
			status    storage.StepStatus
		}{stepIndex, stepState.Status})
	}

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Should have progress updates for each step (running + completed)
	if len(progressUpdates) < 2 {
		t.Errorf("Expected at least 2 progress updates, got %d", len(progressUpdates))
	}
}

func TestWorkflowGetProgress(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-get-progress", nil, repoInfo)

	for i := 0; i < 4; i++ {
		w.AddStep(Step{
			Name: "step",
			Verify: func(w *Workflow, response string) StepResult {
				return StepResult{NextOffset: 1, Message: "done"}
			},
		})
	}

	// Initial progress should be 0
	if w.GetProgress() != 0 {
		t.Errorf("Expected initial progress 0, got %f", w.GetProgress())
	}

	// Manually advance and check progress
	w.CurrentStep = 2
	if w.GetProgress() != 0.5 {
		t.Errorf("Expected progress 0.5, got %f", w.GetProgress())
	}

	w.CurrentStep = 4
	if w.GetProgress() != 1.0 {
		t.Errorf("Expected progress 1.0, got %f", w.GetProgress())
	}
}

// Tests for StepResult helper functions

func TestStepResultHelpers(t *testing.T) {
	tests := []struct {
		name     string
		result   StepResult
		wantOff  int
		wantStep string
		wantMsg  string
	}{
		{"Next", Next("done"), 1, "", "done"},
		{"Retry", Retry("retrying"), 0, "", "retrying"},
		{"GoTo", GoTo("target-step", "jumping"), 0, "target-step", "jumping"},
		{"Back", Back("going back"), -1, "", "going back"},
		{"BackN(2)", BackN(2, "back 2"), -2, "", "back 2"},
		{"BackN(0) clamps to 1", BackN(0, "back"), -1, "", "back"},
		{"Skip(2)", Skip(2, "skip"), 2, "", "skip"},
		{"Skip(0) clamps to 1", Skip(0, "skip"), 1, "", "skip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result.NextOffset != tt.wantOff {
				t.Errorf("NextOffset = %d, want %d", tt.result.NextOffset, tt.wantOff)
			}
			if tt.result.NextStep != tt.wantStep {
				t.Errorf("NextStep = %q, want %q", tt.result.NextStep, tt.wantStep)
			}
			if tt.result.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", tt.result.Message, tt.wantMsg)
			}
		})
	}
}

// Tests for common step helpers

func TestAddPrompt(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-prompt-step", nil, repoInfo)

	promptSent := ""
	w.sendPrompt = func(ctx context.Context, prompt string) <-chan string {
		promptSent = prompt
		ch := make(chan string, 1)
		ch <- "AI response"
		close(ch)
		return ch
	}

	w.AddPrompt("analyze", "Analyze the code")

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if promptSent != "Analyze the code" {
		t.Errorf("Expected prompt 'Analyze the code', got %q", promptSent)
	}

	if w.State != storage.WorkflowStateCompleted {
		t.Errorf("Expected completed state, got %s", w.State)
	}
}

func TestAddCmd(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-shell-step", nil, repoInfo)

	// Test successful command with output storage
	w.AddCmd("echo-test", "echo hello", "output")

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	output := w.Get("output")
	if output != "hello\n" {
		t.Errorf("Expected output 'hello\\n', got %q", output)
	}
}

func TestAddCmdFailure(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-shell-fail", nil, repoInfo, WithMaxRetries(1))

	// Test failing command
	w.AddCmd("fail-test", "exit 1", "")

	ctx := context.Background()
	err := w.Run(ctx)

	if err == nil {
		t.Error("Expected workflow to fail on shell error")
	}

	if w.State != storage.WorkflowStateFailed {
		t.Errorf("Expected failed state, got %s", w.State)
	}
}

func TestAddGate(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-gate-step", nil, repoInfo, WithMaxRetries(5))

	checkCount := 0
	w.AddGate("wait-condition", func(w *Workflow) bool {
		checkCount++
		return checkCount >= 3 // Pass on 3rd check
	}, "Waiting...")

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if checkCount != 3 {
		t.Errorf("Expected 3 checks, got %d", checkCount)
	}
}

func TestAddConfirm(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	// Test approval granted
	t.Run("approved", func(t *testing.T) {
		w := NewWorkflow("test-approval", nil, repoInfo,
			WithRequestYesNo(func(question string) <-chan bool {
				ch := make(chan bool, 1)
				ch <- true
				close(ch)
				return ch
			}),
		)

		w.AddConfirm("confirm", "Proceed?")

		ctx := context.Background()
		err := w.Run(ctx)
		if err != nil {
			t.Fatalf("Workflow failed: %v", err)
		}

		states := w.GetStepStates()
		if states[0].Message != "✓ Approved" {
			t.Errorf("Expected '✓ Approved', got %q", states[0].Message)
		}
	})

	// Test approval denied
	t.Run("denied", func(t *testing.T) {
		w := NewWorkflow("test-approval-denied", nil, repoInfo,
			WithRequestYesNo(func(question string) <-chan bool {
				ch := make(chan bool, 1)
				ch <- false
				close(ch)
				return ch
			}),
		)

		w.AddConfirm("confirm", "Proceed?")

		ctx := context.Background()
		err := w.Run(ctx)
		if err != nil {
			t.Fatalf("Workflow failed: %v", err)
		}

		states := w.GetStepStates()
		if states[0].Message != "⊘ Skipped by user" {
			t.Errorf("Expected '⊘ Skipped by user', got %q", states[0].Message)
		}
	})
}

func TestAddIf(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	// Test condition true - step executes
	t.Run("condition true", func(t *testing.T) {
		w := NewWorkflow("test-conditional-true", nil, repoInfo)

		executed := false
		w.AddIf(
			func(w *Workflow) bool { return true },
			Step{
				Name: "conditional",
				Verify: func(w *Workflow, response string) StepResult {
					executed = true
					return Next("done")
				},
			},
		)

		ctx := context.Background()
		err := w.Run(ctx)
		if err != nil {
			t.Fatalf("Workflow failed: %v", err)
		}

		if !executed {
			t.Error("Expected step to execute when condition is true")
		}
	})

	// Test condition false - step skipped
	t.Run("condition false", func(t *testing.T) {
		w := NewWorkflow("test-conditional-false", nil, repoInfo)

		executed := false
		w.AddIf(
			func(w *Workflow) bool { return false },
			Step{
				Name: "conditional",
				Verify: func(w *Workflow, response string) StepResult {
					executed = true
					return Next("done")
				},
			},
		)

		ctx := context.Background()
		err := w.Run(ctx)
		if err != nil {
			t.Fatalf("Workflow failed: %v", err)
		}

		if executed {
			t.Error("Expected step to be skipped when condition is false")
		}

		states := w.GetStepStates()
		if states[0].Message != "⊘ Skipped (condition not met)" {
			t.Errorf("Expected skip message, got %q", states[0].Message)
		}
	})
}

func TestStepHelpersWithWorkflowData(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-helpers-data", nil, repoInfo)

	// Set initial data
	w.Set("runTests", "true")

	// Use conditional step based on workflow data
	w.AddIf(
		func(w *Workflow) bool { return w.Get("runTests") == "true" },
		Step{
			Name: "run-tests",
			Verify: func(w *Workflow, response string) StepResult {
				w.Set("testsRan", "yes")
				return Next("tests done")
			},
		},
	)

	// Gate step that checks workflow data
	w.AddGate("check-tests", func(w *Workflow) bool {
		return w.Get("testsRan") == "yes"
	}, "Waiting for tests")

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if w.Get("testsRan") != "yes" {
		t.Error("Expected testsRan to be 'yes'")
	}
}

func TestMethodChaining(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	executedSteps := []string{}

	w := NewWorkflow("test-chaining", nil, repoInfo).
		Add(Step{
			Name: "step1",
			Verify: func(w *Workflow, response string) StepResult {
				executedSteps = append(executedSteps, "step1")
				return Next("done")
			},
		}).
		Add(Step{
			Name: "step2",
			Verify: func(w *Workflow, response string) StepResult {
				executedSteps = append(executedSteps, "step2")
				return Next("done")
			},
		}).
		AddGate("gate", func(w *Workflow) bool {
			executedSteps = append(executedSteps, "gate")
			return true
		}, "waiting")

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if len(executedSteps) != 3 {
		t.Errorf("Expected 3 steps executed, got %d: %v", len(executedSteps), executedSteps)
	}

	expected := []string{"step1", "step2", "gate"}
	for i, step := range expected {
		if executedSteps[i] != step {
			t.Errorf("Step %d: expected %s, got %s", i, step, executedSteps[i])
		}
	}
}

func TestAddRun(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	t.Run("success", func(t *testing.T) {
		w := NewWorkflow("test-addrun", nil, repoInfo)
		executed := false

		w.AddRun("do-something", func(w *Workflow) error {
			executed = true
			w.Set("result", "done")
			return nil
		})

		ctx := context.Background()
		err := w.Run(ctx)
		if err != nil {
			t.Fatalf("Workflow failed: %v", err)
		}

		if !executed {
			t.Error("Expected function to be executed")
		}

		if w.Get("result") != "done" {
			t.Errorf("Expected result 'done', got %q", w.Get("result"))
		}
	})

	t.Run("failure", func(t *testing.T) {
		w := NewWorkflow("test-addrun-fail", nil, repoInfo, WithMaxRetries(1))

		w.AddRun("fail", func(w *Workflow) error {
			return fmt.Errorf("something went wrong")
		})

		ctx := context.Background()
		err := w.Run(ctx)

		if err == nil {
			t.Error("Expected workflow to fail")
		}

		if w.State != storage.WorkflowStateFailed {
			t.Errorf("Expected failed state, got %s", w.State)
		}
	})
}

func TestAddCheck(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-addcheck", nil, repoInfo)

	checkCount := 0
	w.AddCheck("verify-something", func(w *Workflow) StepResult {
		checkCount++
		if checkCount < 2 {
			return Retry("not ready yet")
		}
		return Next("ready!")
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if checkCount != 2 {
		t.Errorf("Expected 2 checks, got %d", checkCount)
	}
}

// Tests for GoTo (named step navigation)

func TestGoToSkipForward(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-goto-forward", nil, repoInfo)

	executedSteps := []string{}

	w.Add(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-1")
			return GoTo("step-3", "skipping to step-3")
		},
	})

	w.Add(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-2")
			return Next("done")
		},
	})

	w.Add(Step{
		Name: "step-3",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-3")
			return Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// step-2 should be skipped
	expected := []string{"step-1", "step-3"}
	if len(executedSteps) != len(expected) {
		t.Errorf("Expected %d steps, got %d: %v", len(expected), len(executedSteps), executedSteps)
	}
	for i, step := range expected {
		if i >= len(executedSteps) || executedSteps[i] != step {
			t.Errorf("Step %d: expected %s, got %v", i, step, executedSteps)
		}
	}
}

func TestGoToGoBack(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-goto-back", nil, repoInfo)

	step2Runs := 0

	w.Add(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return Next("done")
		},
	})

	w.Add(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			step2Runs++
			if step2Runs == 1 {
				return GoTo("step-1", "going back to step-1")
			}
			return Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// step-2 should have run twice
	if step2Runs != 2 {
		t.Errorf("Expected step-2 to run 2 times, got %d", step2Runs)
	}
}

func TestGoToUnknownStep(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-goto-unknown", nil, repoInfo)

	w.Add(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return GoTo("nonexistent-step", "trying to go to unknown step")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)

	if err == nil {
		t.Error("Expected workflow to fail when GoTo references unknown step")
	}

	if w.State != storage.WorkflowStateFailed {
		t.Errorf("Expected failed state, got %s", w.State)
	}
}

func TestGoToSameStep(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-goto-same", nil, repoInfo, WithMaxRetries(3))

	runCount := 0

	w.Add(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			runCount++
			if runCount < 3 {
				// GoTo same step should act like Retry
				return GoTo("step-1", "retrying via GoTo")
			}
			return Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if runCount != 3 {
		t.Errorf("Expected 3 runs, got %d", runCount)
	}
}

func TestGoToHelper(t *testing.T) {
	result := GoTo("target-step", "jumping to target")
	if result.NextStep != "target-step" {
		t.Errorf("NextStep = %q, want %q", result.NextStep, "target-step")
	}
	if result.Message != "jumping to target" {
		t.Errorf("Message = %q, want %q", result.Message, "jumping to target")
	}
	if result.NextOffset != 0 {
		t.Errorf("NextOffset = %d, want 0", result.NextOffset)
	}
}

func TestWorkflowGoToForward(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-goto-forward", nil, repoInfo)

	executedSteps := []string{}

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-1")
			return GoTo("step-3", "skip to step-3")
		},
	})

	w.AddStep(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-2")
			return Next("done")
		},
	})

	w.AddStep(Step{
		Name: "step-3",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-3")
			return Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Step 2 should be skipped
	expected := []string{"step-1", "step-3"}
	if len(executedSteps) != len(expected) {
		t.Errorf("Expected %d steps, got %d: %v", len(expected), len(executedSteps), executedSteps)
	}
	for i, step := range expected {
		if i >= len(executedSteps) || executedSteps[i] != step {
			t.Errorf("Step %d: expected %s, got %s", i, step, executedSteps[i])
		}
	}
}

func TestWorkflowGoToBackward(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-goto-backward", nil, repoInfo)

	step2Runs := 0

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return Next("done")
		},
	})

	w.AddStep(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			step2Runs++
			if step2Runs == 1 {
				return GoTo("step-1", "go back to step-1")
			}
			return Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Step 2 should have run twice
	if step2Runs != 2 {
		t.Errorf("Expected step 2 to run 2 times, got %d", step2Runs)
	}
}

func TestWorkflowGoToUnknownStep(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-goto-unknown", nil, repoInfo)

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return GoTo("nonexistent-step", "this should fail")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)

	if err == nil {
		t.Error("Expected workflow to fail when GoTo targets unknown step")
	}

	if w.State != storage.WorkflowStateFailed {
		t.Errorf("Expected failed state, got %s", w.State)
	}
}

func TestWorkflowGoToSameStep(t *testing.T) {
	repoInfo := RepoInfo{
		ProjectRoot: "/tmp/test",
		Branch:      "main",
		Slug:        "test/project",
	}

	w := NewWorkflow("test-goto-same", nil, repoInfo, WithMaxRetries(3))

	runCount := 0

	w.AddStep(Step{
		Name: "retry-step",
		Verify: func(w *Workflow, response string) StepResult {
			runCount++
			if runCount < 3 {
				return GoTo("retry-step", "retry via GoTo")
			}
			return Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if runCount != 3 {
		t.Errorf("Expected 3 runs, got %d", runCount)
	}
}
