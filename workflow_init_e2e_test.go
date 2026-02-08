package main

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
)

// hasUncommittedChanges checks if the current repo has uncommitted changes
func hasUncommittedChanges() bool {
	ri := repo.GetRepoInfo()
	return !ri.IsClean()
}

// TestInitWorkflowE2E contains end-to-end tests for the :init command workflow
// These tests use the mocking infrastructure from tests/e2e/

func TestInitWorkflowPreChecks(t *testing.T) {
	skipIfNotCI(t)
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

	// Initialize a git repo
	initTestGitRepo(t, tmpDir)

	t.Run("Workflow fails on uncommitted changes", func(t *testing.T) {
		// Create a staged (uncommitted) file - untracked files are ignored
		if err := os.WriteFile("uncommitted.txt", []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
		runTestGitCommand(t, tmpDir, "add", "uncommitted.txt")
		defer func() {
			runTestGitCommand(t, tmpDir, "reset", "HEAD", "uncommitted.txt")
			os.Remove("uncommitted.txt")
		}()

		// Create workflow with mock sendPrompt
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-precheck", nil, repoCtx,
			WithMaxRetries(1),
			WithSendPrompt(emptySendPrompt))

		// Add the same pre-checks gate as init workflow
		w.AddGate("pre-checks", func(w *Workflow) bool {
			return !hasUncommittedChanges()
		}, "Please commit or stash your changes")

		ctx := context.Background()
		err := w.Run(ctx)

		// Should fail because of uncommitted changes
		if err == nil {
			t.Error("Expected workflow to fail on uncommitted changes")
		}

		if w.State != storage.WorkflowStateFailed {
			t.Errorf("Expected state Failed, got %s", w.State)
		}
	})

	t.Run("Workflow passes pre-checks on clean repo", func(t *testing.T) {
		// Commit the test file from previous test if it exists
		runTestGitCommand(t, tmpDir, "add", "-A")
		runTestGitCommand(t, tmpDir, "commit", "-m", "cleanup", "--allow-empty")

		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-precheck-pass", nil, repoCtx,
			WithMaxRetries(1),
			WithSendPrompt(emptySendPrompt))

		passed := false
		w.AddGate("pre-checks", func(w *Workflow) bool {
			return !hasUncommittedChanges()
		}, "Please commit or stash your changes").
			AddRun("mark-passed", func(w *Workflow) error {
				passed = true
				return nil
			})

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Expected workflow to succeed, got error: %v", err)
		}

		if !passed {
			t.Error("mark-passed step should have run")
		}
	})
}

func TestInitWorkflowDirectorySetup(t *testing.T) {
	skipIfNotCI(t)
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

	t.Run("Creates .agents/sandbox directory", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-dirs", nil, repoCtx,
			WithSendPrompt(emptySendPrompt))

		w.AddRun("setup-directories", func(w *Workflow) error {
			if err := os.MkdirAll(".agents/sandbox", 0o755); err != nil {
				return err
			}
			return nil
		})

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Workflow failed: %v", err)
		}

		// Verify directory was created
		if _, err := os.Stat(".agents/sandbox"); os.IsNotExist(err) {
			t.Error(".agents/sandbox directory was not created")
		}
	})
}

func TestInitWorkflowGoToNavigation(t *testing.T) {
	skipIfNotCI(t)
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	t.Run("GoTo navigates to named step", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		executionOrder := []string{}

		w := New("test-goto", nil, repoCtx,
			WithSendPrompt(emptySendPrompt))

		w.AddCheck("step-a", func(w *Workflow) StepResult {
			executionOrder = append(executionOrder, "a")
			return w.GoTo("step-c", "skipping to c")
		}).
			AddCheck("step-b", func(w *Workflow) StepResult {
				executionOrder = append(executionOrder, "b")
				return w.Next("done b")
			}).
			AddCheck("step-c", func(w *Workflow) StepResult {
				executionOrder = append(executionOrder, "c")
				return w.Next("done c")
			})

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Workflow failed: %v", err)
		}

		// Should have skipped step-b
		expected := []string{"a", "c"}
		if len(executionOrder) != len(expected) {
			t.Errorf("Expected %d steps, got %d: %v", len(expected), len(executionOrder), executionOrder)
		}

		for i, step := range expected {
			if i >= len(executionOrder) || executionOrder[i] != step {
				t.Errorf("Step %d: expected %q, got %v", i, step, executionOrder)
			}
		}
	})

	t.Run("GoTo can go backwards", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		attempts := 0

		w := New("test-goto-back", nil, repoCtx,
			WithMaxRetries(5),
			WithSendPrompt(emptySendPrompt))

		w.AddCheck("step-a", func(w *Workflow) StepResult {
			attempts++
			if attempts < 3 {
				return w.GoTo("step-b", "go to b")
			}
			// Skip step-b by going to step-c directly
			return w.GoTo("step-c", "done, skip to end")
		}).
			AddCheck("step-b", func(w *Workflow) StepResult {
				// Go back to step-a
				return w.GoTo("step-a", "retry from a")
			}).
			AddRun("step-c", func(w *Workflow) error {
				// Final step
				return nil
			})

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Workflow failed: %v", err)
		}

		// step-a should have been attempted 3 times
		if attempts != 3 {
			t.Errorf("Expected 3 attempts, got %d", attempts)
		}
	})
}

func TestInitWorkflowRetryLoops(t *testing.T) {
	skipIfNotCI(t)
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	t.Run("Retry loop with eventual success", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		testFailCount := 0

		// Create a mock sendPrompt that simulates fixing issues
		mockSendPrompt := func(ctx context.Context, prompt string) <-chan string {
			ch := make(chan string, 1)
			ch <- "Fixed the issue"
			close(ch)
			return ch
		}

		w := New("test-retry-loop", nil, repoCtx,
			WithMaxRetries(5),
			WithSendPrompt(mockSendPrompt))

		// Simulates host-tests -> fix-host-tests -> host-tests pattern
		w.AddCheck("host-tests", func(w *Workflow) StepResult {
			testFailCount++
			if testFailCount < 3 {
				w.Set("testError", "test failed")
				return w.GoTo("fix-host-tests", "tests failed")
			}
			// Skip to done step when tests pass
			return w.GoTo("done", "tests passed")
		}).
			Add(Step{
				Name:   "fix-host-tests",
				Prompt: "Fix the test error: {{.testError}}",
				Verify: func(w *Workflow, response string) StepResult {
					return w.GoTo("host-tests", "re-running tests")
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

		// Tests should have run 3 times (2 failures + 1 success)
		if testFailCount != 3 {
			t.Errorf("Expected 3 test attempts, got %d", testFailCount)
		}
	})

	t.Run("Retry loop exceeds max retries", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		w := New("test-max-retries", nil, repoCtx,
			WithMaxRetries(3),
			WithSendPrompt(emptySendPrompt))

		w.AddCheck("always-fail", func(w *Workflow) StepResult {
			return w.Retry("always fails")
		})

		ctx := context.Background()
		err := w.Run(ctx)

		// Should fail after max retries
		if err == nil {
			t.Error("Expected workflow to fail after max retries")
		}

		if w.State != storage.WorkflowStateFailed {
			t.Errorf("Expected state Failed, got %s", w.State)
		}
	})
}

func TestInitWorkflowProgress(t *testing.T) {
	skipIfNotCI(t)
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	t.Run("Progress callback is invoked", func(t *testing.T) {
		repoCtx := RepoContext{
			Host:    "github.com",
			Org:     "test",
			Project: "project",
			Branch:  "main",
		}

		progressMessages := []string{}

		w := New("test-progress", nil, repoCtx,
			WithSendPrompt(emptySendPrompt),
			WithOnProgress(func(stepIndex int, state StepState, message string) {
				if message != "" {
					progressMessages = append(progressMessages, message)
				}
			}))

		w.AddRun("step1", func(w *Workflow) error {
			w.ReportProgress("Starting step 1")
			w.ReportProgress("Working on step 1")
			return nil
		}).
			AddRun("step2", func(w *Workflow) error {
				w.ReportProgress("Starting step 2")
				return nil
			})

		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			t.Errorf("Workflow failed: %v", err)
		}

		// Check that progress messages were captured
		if len(progressMessages) < 3 {
			t.Errorf("Expected at least 3 progress messages, got %d: %v", len(progressMessages), progressMessages)
		}
	})
}

func TestMissingInfraFiles(t *testing.T) {
	skipIfNotCI(t)
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	t.Run("Detects all missing files", func(t *testing.T) {
		missing := checkMissingInfraFiles("AGENTS.md")

		expectedMissing := []string{
			"AGENTS.md",
			"Justfile",
			".agents/asimi.conf",
			".agents/sandbox/Dockerfile",
			".agents/sandbox/bashrc",
		}

		if len(missing) != len(expectedMissing) {
			t.Errorf("Expected %d missing files, got %d: %v", len(expectedMissing), len(missing), missing)
		}

		for _, file := range expectedMissing {
			found := false
			for _, m := range missing {
				if m == file {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected %s to be in missing list", file)
			}
		}
	})

	t.Run("Detects when some files exist", func(t *testing.T) {
		// Create some files
		os.WriteFile("AGENTS.md", []byte("# Test"), 0644)
		os.MkdirAll(".agents/sandbox", 0755)
		os.WriteFile(".agents/asimi.conf", []byte("# config"), 0644)

		missing := checkMissingInfraFiles("AGENTS.md")

		// Should only be missing Justfile, Dockerfile, and bashrc
		expectedMissing := []string{
			"Justfile",
			".agents/sandbox/Dockerfile",
			".agents/sandbox/bashrc",
		}

		if len(missing) != len(expectedMissing) {
			t.Errorf("Expected %d missing files, got %d: %v", len(expectedMissing), len(missing), missing)
		}
	})

	t.Run("No missing files when all exist", func(t *testing.T) {
		// Create all files
		os.WriteFile("AGENTS.md", []byte("# Test"), 0644)
		os.WriteFile("Justfile", []byte("test:"), 0644)
		os.MkdirAll(".agents/sandbox", 0755)
		os.WriteFile(".agents/asimi.conf", []byte("# config"), 0644)
		os.WriteFile(".agents/sandbox/Dockerfile", []byte("FROM alpine"), 0644)
		os.WriteFile(".agents/sandbox/bashrc", []byte("# bashrc"), 0644)

		missing := checkMissingInfraFiles("AGENTS.md")

		if len(missing) != 0 {
			t.Errorf("Expected no missing files, got: %v", missing)
		}
	})
}

// Helper functions

func emptySendPrompt(ctx context.Context, prompt string) <-chan string {
	ch := make(chan string, 1)
	ch <- ""
	close(ch)
	return ch
}

func initTestGitRepo(t *testing.T, dir string) {
	t.Helper()
	runTestGitCommand(t, dir, "init")
	runTestGitCommand(t, dir, "config", "user.email", "test@test.com")
	runTestGitCommand(t, dir, "config", "user.name", "Test")

	// Create an initial file and commit
	if err := os.WriteFile(dir+"/initial.txt", []byte("initial"), 0644); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}
	runTestGitCommand(t, dir, "add", "-A")
	runTestGitCommand(t, dir, "commit", "-m", "initial")
}

func runTestGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2024-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2024-01-01T00:00:00Z",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, output)
	}
}
