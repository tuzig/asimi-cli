package main

import (
	"context"
	"os"
	"testing"

	"github.com/afittestide/asimi/storage"
)

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

	// Use a custom sendPrompt function that returns empty responses
	// so steps with prompts don't hang waiting for AI responses
	emptySendPrompt := func(ctx context.Context, prompt string) <-chan string {
		ch := make(chan string, 1)
		ch <- ""
		close(ch)
		return ch
	}

	t.Run("Workflow retries failed steps up to MaxRetries", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-retry", nil, repoCtx,
			WithMaxRetries(3),
			WithSendPrompt(emptySendPrompt))

		attemptCount := 0
		w.Add(Step{
			Name:   "always-fail",
			Prompt: "test prompt", // Use a prompt so Retry doesn't go backwards
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

		w := New("test-retry-success", nil, repoCtx,
			WithMaxRetries(3),
			WithSendPrompt(emptySendPrompt))

		attemptCount := 0
		w.Add(Step{
			Name:   "fail-then-succeed",
			Prompt: "test prompt", // Add a prompt so Retry doesn't go backwards
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
