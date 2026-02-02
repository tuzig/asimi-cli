package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowBasicExecution(t *testing.T) {
	// Create a simple workflow without persistence
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-workflow", nil, repoCtx)

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
			assert.Equal(t, "value1", w.Get("key1"), "Expected key1 to be value1")
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
	require.NoError(t, err, "Workflow failed")

	// Verify all steps executed
	expectedSteps := []string{
		"step-1-prepare", "step-1-verify",
		"step-2-prepare", "step-2-verify",
	}

	assert.Equal(t, len(expectedSteps), len(executedSteps), "Step count mismatch")

	for i, step := range expectedSteps {
		if i < len(executedSteps) {
			assert.Equal(t, step, executedSteps[i], "Step %d mismatch", i)
		}
	}

	// Verify workflow state
	assert.Equal(t, storage.WorkflowStateCompleted, w.State, "Expected workflow state to be completed")
}

func TestWorkflowRetry(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-retry", nil, repoCtx, WithMaxRetries(3))

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
	require.NoError(t, err, "Workflow failed")

	assert.Equal(t, 3, retryCount, "Expected 3 retries")

	// Check step state
	states := w.GetStepStates()
	require.Len(t, states, 1, "Expected 1 step state")
	assert.Equal(t, 2, states[0].RetryCount, "Expected retry count 2") // 2 retries before success
}

func TestWorkflowMaxRetriesExceeded(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-max-retries", nil, repoCtx, WithMaxRetries(2))

	w.AddStep(Step{
		Name: "always-fail",
		Verify: func(w *Workflow, response string) StepResult {
			return StepResult{NextOffset: 0, Message: "always fails"}
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)

	assert.Error(t, err, "Expected workflow to fail due to max retries")
	assert.Equal(t, storage.WorkflowStateFailed, w.State, "Expected workflow state to be failed")
}

func TestWorkflowSkipSteps(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-skip", nil, repoCtx)

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
	require.NoError(t, err, "Workflow failed")

	// Step 2 should be skipped
	assert.Len(t, executedSteps, 2, "Expected 2 steps executed")
	assert.Equal(t, "step-1", executedSteps[0])
	assert.Equal(t, "step-3", executedSteps[1])
}

func TestWorkflowGoBack(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-goback", nil, repoCtx)

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
	require.NoError(t, err, "Workflow failed")

	// Step 2 should have run twice (once before going back, once after)
	assert.Equal(t, 2, step2Runs, "Expected step 2 to run 2 times")
}

func TestWorkflowAbort(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-abort", nil, repoCtx)

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

	assert.Error(t, err, "Expected workflow to return error on abort")
	assert.Equal(t, storage.WorkflowStateAborted, w.State, "Expected workflow state to be aborted")
}

func TestWorkflowContextCancellation(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-cancel", nil, repoCtx)

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

	assert.Error(t, err, "Expected workflow to return error on cancellation")
	assert.Equal(t, storage.WorkflowStateAborted, w.State, "Expected workflow state to be aborted")
}

func TestWorkflowDataPersistence(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-data", nil, repoCtx)

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
			assert.Equal(t, "manual-value", w.Get("manual-key"), "manual-key not persisted")
			assert.Equal(t, "prepare-value", w.Get("prepare-key"), "prepare-key not persisted")
			return StepResult{NextOffset: 1, Message: "done"}
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")
}

func TestWorkflowProgress(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-progress", nil, repoCtx)

	progressUpdates := []int{}

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
		progressUpdates = append(progressUpdates, stepIndex)
	}

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	// Should have progress updates for each step
	assert.GreaterOrEqual(t, len(progressUpdates), 2, "Expected at least 2 progress updates")
}

func TestWorkflowGetProgress(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-get-progress", nil, repoCtx)

	for i := 0; i < 4; i++ {
		w.AddStep(Step{
			Name: "step",
			Verify: func(w *Workflow, response string) StepResult {
				return StepResult{NextOffset: 1, Message: "done"}
			},
		})
	}

	// Initial progress should be 0
	assert.Equal(t, float64(0), w.GetProgress(), "Expected initial progress 0")

	// Manually advance and check progress
	w.CurrentStep = 2
	assert.Equal(t, 0.5, w.GetProgress(), "Expected progress 0.5")

	w.CurrentStep = 4
	assert.Equal(t, 1.0, w.GetProgress(), "Expected progress 1.0")
}

// Tests for StepResult helper functions

func TestStepResultHelpers(t *testing.T) {
	// Create a workflow instance for testing methods that require one
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}
	w := New("test-helpers", nil, repoCtx)
	// Add two steps so Back/BackN can work from CurrentStep=1
	w.Add(Step{Name: "step1", Prompt: "test prompt", Verify: func(w *Workflow, response string) StepResult { return w.Next("ok") }})
	w.Add(Step{Name: "step2", Prompt: "test prompt", Verify: func(w *Workflow, response string) StepResult { return w.Next("ok") }})
	// Move to step 1 so Back/BackN tests work
	w.CurrentStep = 1

	tests := []struct {
		name     string
		result   StepResult
		wantOff  int
		wantStep string
		wantMsg  string
	}{
		{"Next", w.Next("done"), 1, "", "done"},
		{"Retry", w.Retry("retrying"), 0, "", "retrying"},
		{"GoTo", w.GoTo("target-step", "jumping"), 0, "target-step", "jumping"},
		{"Back", w.Back("going back"), -1, "", "going back"},
		{"BackN(2)", w.BackN(2, "back 2"), -2, "", "back 2"},
		{"BackN(0) clamps to 1", w.BackN(0, "back"), -1, "", "back"},
		{"Skip(2)", w.Skip(2, "skip"), 2, "", "skip"},
		{"Skip(0) clamps to 1", w.Skip(0, "skip"), 1, "", "skip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantOff, tt.result.NextOffset, "NextOffset mismatch")
			assert.Equal(t, tt.wantStep, tt.result.NextStep, "NextStep mismatch")
			assert.Equal(t, tt.wantMsg, tt.result.Message, "Message mismatch")
		})
	}
}

// TestBackNAtStep0 tests that workflow fails when trying to go back from step 0
func TestBackNAtStep0(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}
	w := New("test-back-from-zero", nil, repoCtx)
	w.Add(Step{
		Name: "step1",
		Verify: func(w *Workflow, response string) StepResult {
			return w.BackN(2, "trying to go back")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)

	assert.Error(t, err, "Expected workflow to fail when going back from step 0")
	assert.Contains(t, err.Error(), "cannot go back")
	assert.Equal(t, storage.WorkflowStateFailed, w.State)
}

// TestRetryAtStep0WithNoPrompt tests that Retry works correctly for steps without prompts
func TestRetryAtStep0WithNoPrompt(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}
	w := New("test-retry-at-zero", nil, repoCtx, WithMaxRetries(3))

	retryCount := 0
	w.Add(Step{
		Name:   "step1",
		Prompt: "",
		Verify: func(w *Workflow, response string) StepResult {
			retryCount++
			if retryCount < 3 {
				return w.Retry("retrying")
			}
			return w.Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)

	require.NoError(t, err, "Workflow should complete after retries")
	assert.Equal(t, 3, retryCount, "Expected 3 attempts")
	assert.Equal(t, storage.WorkflowStateCompleted, w.State)
}

// Tests for common step helpers

func TestAddPrompt(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-prompt-step", nil, repoCtx)

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
	require.NoError(t, err, "Workflow failed")

	assert.Equal(t, "Analyze the code", promptSent, "Expected prompt 'Analyze the code'")
	assert.Equal(t, storage.WorkflowStateCompleted, w.State, "Expected completed state")
}

func TestAddCmd(t *testing.T) {
	// Skip if running in CI or environment without shell runner
	// AddCmd uses the shell runner infrastructure which requires podman or host approval
	t.Skip("TestAddCmd requires shell runner infrastructure (podman or host approval)")
}

func TestAddCmdFailure(t *testing.T) {
	// Skip if running in CI or environment without shell runner
	t.Skip("TestAddCmdFailure requires shell runner infrastructure (podman or host approval)")
}

func TestAddGate(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-gate-step", nil, repoCtx, WithMaxRetries(5))

	checkCount := 0
	w.AddGate("wait-condition", func(w *Workflow) bool {
		checkCount++
		return checkCount >= 3 // Pass on 3rd check
	}, "Waiting...")

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	assert.Equal(t, 3, checkCount, "Expected 3 checks")
}

func TestAddConfirm(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	// Test approval granted
	t.Run("approved", func(t *testing.T) {
		w := New("test-approval", nil, repoCtx,
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
		require.NoError(t, err, "Workflow failed")

		states := w.GetStepStates()
		assert.Equal(t, "✓ Approved", states[0].Message)
	})

	// Test approval denied
	t.Run("denied", func(t *testing.T) {
		w := New("test-approval-denied", nil, repoCtx,
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
		require.NoError(t, err, "Workflow failed")

		states := w.GetStepStates()
		assert.Equal(t, "⊘ Skipped by user", states[0].Message)
	})
}

func TestAddIf(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	// Test condition true - step executes
	t.Run("condition true", func(t *testing.T) {
		w := New("test-conditional-true", nil, repoCtx)

		executed := false
		w.AddIf(
			func(w *Workflow) bool { return true },
			Step{
				Name: "conditional",
				Verify: func(w *Workflow, response string) StepResult {
					executed = true
					return w.Next("done")
				},
			},
		)

		ctx := context.Background()
		err := w.Run(ctx)
		require.NoError(t, err, "Workflow failed")

		assert.True(t, executed, "Expected step to execute when condition is true")
	})

	// Test condition false - step skipped
	t.Run("condition false", func(t *testing.T) {
		w := New("test-conditional-false", nil, repoCtx)

		executed := false
		w.AddIf(
			func(w *Workflow) bool { return false },
			Step{
				Name: "conditional",
				Verify: func(w *Workflow, response string) StepResult {
					executed = true
					return w.Next("done")
				},
			},
		)

		ctx := context.Background()
		err := w.Run(ctx)
		require.NoError(t, err, "Workflow failed")

		assert.False(t, executed, "Expected step to be skipped when condition is false")

		states := w.GetStepStates()
		assert.Equal(t, "⊘ Skipped (condition not met)", states[0].Message, "Expected skip message")
	})
}

func TestStepHelpersWithWorkflowData(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-helpers-data", nil, repoCtx)

	// Set initial data
	w.Set("runTests", "true")

	// Use conditional step based on workflow data
	w.AddIf(
		func(w *Workflow) bool { return w.Get("runTests") == "true" },
		Step{
			Name: "run-tests",
			Verify: func(w *Workflow, response string) StepResult {
				w.Set("testsRan", "yes")
				return w.Next("tests done")
			},
		},
	)

	// Gate step that checks workflow data
	w.AddGate("check-tests", func(w *Workflow) bool {
		return w.Get("testsRan") == "yes"
	}, "Waiting for tests")

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	assert.Equal(t, "yes", w.Get("testsRan"), "Expected testsRan to be 'yes'")
}

func TestMethodChaining(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	executedSteps := []string{}

	w := New("test-chaining", nil, repoCtx).
		Add(Step{
			Name: "step1",
			Verify: func(w *Workflow, response string) StepResult {
				executedSteps = append(executedSteps, "step1")
				return w.Next("done")
			},
		}).
		Add(Step{
			Name: "step2",
			Verify: func(w *Workflow, response string) StepResult {
				executedSteps = append(executedSteps, "step2")
				return w.Next("done")
			},
		}).
		AddGate("gate", func(w *Workflow) bool {
			executedSteps = append(executedSteps, "gate")
			return true
		}, "waiting")

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	assert.Len(t, executedSteps, 3, "Expected 3 steps executed")

	expected := []string{"step1", "step2", "gate"}
	for i, step := range expected {
		assert.Equal(t, step, executedSteps[i], "Step %d mismatch", i)
	}
}

func TestAddRun(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	t.Run("success", func(t *testing.T) {
		w := New("test-addrun", nil, repoCtx)
		executed := false

		w.AddRun("do-something", func(w *Workflow) error {
			executed = true
			w.Set("result", "done")
			return nil
		})

		ctx := context.Background()
		err := w.Run(ctx)
		require.NoError(t, err, "Workflow failed")

		assert.True(t, executed, "Expected function to be executed")
		assert.Equal(t, "done", w.Get("result"), "Expected result 'done'")
	})

	t.Run("failure", func(t *testing.T) {
		w := New("test-addrun-fail", nil, repoCtx, WithMaxRetries(1))

		w.AddRun("fail", func(w *Workflow) error {
			return fmt.Errorf("something went wrong")
		})

		ctx := context.Background()
		err := w.Run(ctx)

		assert.Error(t, err, "Expected workflow to fail")
		assert.Equal(t, storage.WorkflowStateFailed, w.State, "Expected failed state")
	})
}

func TestAddCheck(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-addcheck", nil, repoCtx)

	checkCount := 0
	w.AddCheck("verify-something", func(w *Workflow) StepResult {
		checkCount++
		if checkCount < 2 {
			// For polling checks, return NextOffset=0 directly instead of Retry()
			// (Retry() requires a prompt or previous step to go back to)
			return StepResult{NextOffset: 0, Message: "not ready yet"}
		}
		return w.Next("ready!")
	})

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	assert.Equal(t, 2, checkCount, "Expected 2 checks")
}

// Tests for GoTo (named step navigation)

func TestGoToSkipForward(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-goto-forward", nil, repoCtx)

	executedSteps := []string{}

	w.Add(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-1")
			return w.GoTo("step-3", "skipping to step-3")
		},
	})

	w.Add(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-2")
			return w.Next("done")
		},
	})

	w.Add(Step{
		Name: "step-3",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-3")
			return w.Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	// step-2 should be skipped
	expected := []string{"step-1", "step-3"}
	assert.Equal(t, expected, executedSteps, "Step execution order mismatch")
}

func TestGoToGoBack(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-goto-back", nil, repoCtx)

	step2Runs := 0

	w.Add(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return w.Next("done")
		},
	})

	w.Add(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			step2Runs++
			if step2Runs == 1 {
				return w.GoTo("step-1", "going back to step-1")
			}
			return w.Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	// step-2 should have run twice
	assert.Equal(t, 2, step2Runs, "Expected step-2 to run 2 times")
}

func TestGoToUnknownStep(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-goto-unknown", nil, repoCtx)

	w.Add(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return w.GoTo("nonexistent-step", "trying to go to unknown step")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)

	assert.Error(t, err, "Expected workflow to fail when GoTo references unknown step")
	assert.Equal(t, storage.WorkflowStateFailed, w.State, "Expected failed state")
}

func TestGoToSameStep(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-goto-same", nil, repoCtx, WithMaxRetries(3))

	runCount := 0

	w.Add(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			runCount++
			if runCount < 3 {
				// GoTo same step should act like Retry
				return w.GoTo("step-1", "retrying via GoTo")
			}
			return w.Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	assert.Equal(t, 3, runCount, "Expected 3 runs")
}

func TestGoToHelper(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}
	w := New("test-goto-helper", nil, repoCtx)
	w.Add(Step{Name: "dummy", Verify: func(w *Workflow, response string) StepResult { return w.Next("ok") }})

	result := w.GoTo("target-step", "jumping to target")
	assert.Equal(t, "target-step", result.NextStep, "NextStep mismatch")
	assert.Equal(t, "jumping to target", result.Message, "Message mismatch")
	assert.Equal(t, 0, result.NextOffset, "NextOffset should be 0")
}

func TestWorkflowGoToForward(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-goto-forward", nil, repoCtx)

	executedSteps := []string{}

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-1")
			return w.GoTo("step-3", "skip to step-3")
		},
	})

	w.AddStep(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-2")
			return w.Next("done")
		},
	})

	w.AddStep(Step{
		Name: "step-3",
		Verify: func(w *Workflow, response string) StepResult {
			executedSteps = append(executedSteps, "step-3")
			return w.Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	// Step 2 should be skipped
	expected := []string{"step-1", "step-3"}
	assert.Equal(t, expected, executedSteps, "Step execution order mismatch")
}

func TestWorkflowGoToBackward(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-goto-backward", nil, repoCtx)

	step2Runs := 0

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return w.Next("done")
		},
	})

	w.AddStep(Step{
		Name: "step-2",
		Verify: func(w *Workflow, response string) StepResult {
			step2Runs++
			if step2Runs == 1 {
				return w.GoTo("step-1", "go back to step-1")
			}
			return w.Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	// Step 2 should have run twice
	assert.Equal(t, 2, step2Runs, "Expected step 2 to run 2 times")
}

func TestWorkflowGoToUnknownStep(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-goto-unknown", nil, repoCtx)

	w.AddStep(Step{
		Name: "step-1",
		Verify: func(w *Workflow, response string) StepResult {
			return w.GoTo("nonexistent-step", "this should fail")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)

	assert.Error(t, err, "Expected workflow to fail when GoTo targets unknown step")
	assert.Equal(t, storage.WorkflowStateFailed, w.State, "Expected failed state")
}

func TestWorkflowGoToSameStep(t *testing.T) {
	repoCtx := RepoContext{
		Host:    "github.com",
		Org:     "test",
		Project: "project",
		Branch:  "main",
	}

	w := New("test-goto-same", nil, repoCtx, WithMaxRetries(3))

	runCount := 0

	w.AddStep(Step{
		Name: "retry-step",
		Verify: func(w *Workflow, response string) StepResult {
			runCount++
			if runCount < 3 {
				return w.GoTo("retry-step", "retry via GoTo")
			}
			return w.Next("done")
		},
	})

	ctx := context.Background()
	err := w.Run(ctx)
	require.NoError(t, err, "Workflow failed")

	assert.Equal(t, 3, runCount, "Expected 3 runs")
}
