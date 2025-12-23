package main

import (
	"context"
	"os"
	"testing"

	"github.com/afittestide/asimi/storage"
)

// getStepByName finds a step by name in the workflow
func getStepByName(w *Workflow, name string) *Step {
	for i := range w.Steps {
		if w.Steps[i].Name == name {
			return &w.Steps[i]
		}
	}
	return nil
}

func TestInitWorkflowRetryBehavior(t *testing.T) {
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}
	defer func() {
		os.Chdir(originalWd)
	}()

	t.Run("Workflow retries failed steps up to MaxRetries", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-retry", nil, repoCtx, WithMaxRetries(3))

		attemptCount := 0
		w.Add(Step{
			Name: "always-fail",
			Verify: func(w *Workflow, response string) StepResult {
				attemptCount++
				return w.Retry("always fails")
			},
		})

		ctx := context.Background()
		err := w.Run(ctx)

		// Should fail after max retries
		if err == nil {
			t.Error("Expected workflow to fail after max retries")
		}

		// Should have attempted MaxRetries times
		if attemptCount != 3 {
			t.Errorf("Expected 3 attempts, got %d", attemptCount)
		}

		// Workflow state should be failed
		if w.State != storage.WorkflowStateFailed {
			t.Errorf("Expected state Failed, got %s", w.State)
		}
	})

	t.Run("Workflow proceeds after successful retry", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-retry-success", nil, repoCtx, WithMaxRetries(3))

		attemptCount := 0
		w.Add(Step{
			Name: "fail-then-succeed",
			Verify: func(w *Workflow, response string) StepResult {
				attemptCount++
				if attemptCount < 2 {
					return w.Retry("retry")
				}
				return w.Next("success")
			},
		}).Add(Step{
			Name: "second-step",
			Verify: func(w *Workflow, response string) StepResult {
				return w.Next("done")
			},
		})

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Expected workflow to succeed, got error: %v", err)
		}

		if attemptCount != 2 {
			t.Errorf("Expected 2 attempts, got %d", attemptCount)
		}

		if w.State != storage.WorkflowStateCompleted {
			t.Errorf("Expected state Completed, got %s", w.State)
		}
	})
}

// Helper function to check if string contains any of the substrings
func containsAny(s string, substrings []string) bool {
	for _, sub := range substrings {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
