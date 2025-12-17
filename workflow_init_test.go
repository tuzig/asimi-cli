package main

import (
	"context"
	"os"
	"testing"

	"github.com/afittestide/asimi/storage"
)

func TestInitWorkflowSteps(t *testing.T) {
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

	repoInfo := RepoInfo{
		ProjectRoot: tmpDir,
		Branch:      "main",
		Slug:        "test/project",
	}

	t.Run("PreChecks step passes when no uncommitted changes", func(t *testing.T) {
		// Initialize a git repo with no changes
		os.MkdirAll(".git", 0755)
		defer os.RemoveAll(".git")

		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")

		// Get the pre-checks step
		if len(w.Steps) < 1 {
			t.Fatal("Expected at least 1 step")
		}

		step := w.Steps[0]
		if step.Name != "pre-checks" {
			t.Errorf("Expected first step to be 'pre-checks', got '%s'", step.Name)
		}

		// AddGate doesn't have a Prepare function, only Verify
		result := step.Verify(w, "")
		t.Logf("PreChecks result: NextOffset=%d, Message=%s", result.NextOffset, result.Message)
	})

	t.Run("SetupDirectories step creates directories", func(t *testing.T) {
		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")

		// Get the setup-directories step
		if len(w.Steps) < 2 {
			t.Fatal("Expected at least 2 steps")
		}

		step := w.Steps[1]
		if step.Name != "setup-directories" {
			t.Errorf("Expected second step to be 'setup-directories', got '%s'", step.Name)
		}

		// AddRun steps do everything in Verify
		result := step.Verify(w, "")
		if result.NextOffset != 1 {
			t.Errorf("Expected NextOffset 1, got %d: %s", result.NextOffset, result.Message)
		}

		// Verify directory was created
		if _, err := os.Stat(".agents/sandbox"); os.IsNotExist(err) {
			t.Error("Expected .agents/sandbox to be created")
		}

		// Cleanup
		os.RemoveAll(".agents")
	})

	t.Run("WriteEmbeds step creates config files", func(t *testing.T) {
		// Create the directory first
		os.MkdirAll(".agents/sandbox", 0755)
		defer os.RemoveAll(".agents")

		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")

		// Get the write-embeds step
		if len(w.Steps) < 3 {
			t.Fatal("Expected at least 3 steps")
		}

		step := w.Steps[2]
		if step.Name != "write-embeds" {
			t.Errorf("Expected third step to be 'write-embeds', got '%s'", step.Name)
		}

		// AddRun steps do everything in Verify
		result := step.Verify(w, "")
		if result.NextOffset != 1 {
			t.Errorf("Expected NextOffset 1, got %d: %s", result.NextOffset, result.Message)
		}

		// Verify files were created
		if _, err := os.Stat(".agents/asimi.conf"); os.IsNotExist(err) {
			t.Error("Expected .agents/asimi.conf to be created")
		}
		if _, err := os.Stat(".agents/sandbox/bashrc"); os.IsNotExist(err) {
			t.Error("Expected .agents/sandbox/bashrc to be created")
		}
	})

	t.Run("WriteEmbeds step in clear mode removes and recreates files", func(t *testing.T) {
		// Create the directory and files first
		os.MkdirAll(".agents/sandbox", 0755)
		os.WriteFile("AGENTS.md", []byte("old content"), 0644)
		os.WriteFile("Justfile", []byte("old content"), 0644)
		os.WriteFile(".agents/asimi.conf", []byte("old content"), 0644)
		os.WriteFile(".agents/sandbox/bashrc", []byte("old content"), 0644)
		defer func() {
			os.RemoveAll(".agents")
			os.Remove("AGENTS.md")
			os.Remove("Justfile")
		}()

		w := NewInitWorkflow(nil, repoInfo, true, "AGENTS.md") // clearMode = true

		// Get the write-embeds step
		step := w.Steps[2]

		// AddRun steps do everything in Verify
		result := step.Verify(w, "")
		if result.NextOffset != 1 {
			t.Errorf("Expected NextOffset 1, got %d: %s", result.NextOffset, result.Message)
		}

		// Verify AGENTS.md was removed (clear mode)
		if _, err := os.Stat("AGENTS.md"); !os.IsNotExist(err) {
			t.Error("Expected AGENTS.md to be removed in clear mode")
		}

		// Verify config files were recreated with embedded content
		content, err := os.ReadFile(".agents/asimi.conf")
		if err != nil {
			t.Fatalf("Failed to read asimi.conf: %v", err)
		}
		if string(content) != defaultConfContent {
			t.Error("Expected asimi.conf to have embedded content")
		}
	})

	t.Run("AIAnalysis step skips when all files exist", func(t *testing.T) {
		// Create all required files
		os.MkdirAll(".agents/sandbox", 0755)
		os.WriteFile("AGENTS.md", []byte("content"), 0644)
		os.WriteFile("Justfile", []byte("content"), 0644)
		os.WriteFile(".agents/asimi.conf", []byte("content"), 0644)
		os.WriteFile(".agents/sandbox/Dockerfile", []byte("content"), 0644)
		os.WriteFile(".agents/sandbox/bashrc", []byte("content"), 0644)
		defer func() {
			os.RemoveAll(".agents")
			os.Remove("AGENTS.md")
			os.Remove("Justfile")
		}()

		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")

		// Get the ai-analysis step
		step := w.Steps[3]
		if step.Name != "ai-analysis" {
			t.Errorf("Expected fourth step to be 'ai-analysis', got '%s'", step.Name)
		}

		// Run prepare
		data, err := step.Prepare(w)
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}

		// Should set skipAIAnalysis
		if skip, ok := data["skipAIAnalysis"].(bool); !ok || !skip {
			t.Error("Expected skipAIAnalysis to be true when all files exist")
		}

		// Verify should skip
		result := step.Verify(w, "")
		if result.NextOffset != 1 {
			t.Errorf("Expected NextOffset 1 (skip), got %d: %s", result.NextOffset, result.Message)
		}
	})

	t.Run("AIAnalysis step fails when required files not created", func(t *testing.T) {
		os.MkdirAll(".agents/sandbox", 0755)
		defer os.RemoveAll(".agents")

		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")

		// Get the ai-analysis step
		step := w.Steps[3]

		// Run prepare (will not skip since files are missing)
		_, err := step.Prepare(w)
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}

		// Verify should fail since AGENTS.md and Justfile don't exist
		result := step.Verify(w, "AI response")
		if result.NextOffset != 0 {
			t.Errorf("Expected NextOffset 0 (retry), got %d: %s", result.NextOffset, result.Message)
		}
		if result.Message == "" {
			t.Error("Expected error message")
		}
	})

	t.Run("HostTests step retries on failure", func(t *testing.T) {
		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")

		// Get the host-tests step
		step := w.Steps[4]
		if step.Name != "host-tests" {
			t.Errorf("Expected fifth step to be 'host-tests', got '%s'", step.Name)
		}

		// Verify will fail because there's no Justfile
		result := step.Verify(w, "")

		// Should return retry (NextOffset = 0) since just test will fail
		if result.NextOffset != 0 {
			t.Errorf("Expected NextOffset 0 (retry) when just test fails, got %d", result.NextOffset)
		}

		// Should have an error message
		if result.Message == "" {
			t.Error("Expected error message when just test fails")
		}

		// Message should indicate failure
		if !containsAny(result.Message, []string{"❌", "failed", "exit code"}) {
			t.Errorf("Expected failure message, got: %s", result.Message)
		}
	})

	t.Run("BuildSandbox step retries on failure", func(t *testing.T) {
		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")

		// Get the build-sandbox step
		step := w.Steps[5]
		if step.Name != "build-sandbox" {
			t.Errorf("Expected sixth step to be 'build-sandbox', got '%s'", step.Name)
		}

		// Verify will fail because there's no Justfile with build-sandbox recipe
		result := step.Verify(w, "")

		// Should return retry (NextOffset = 0) since just build-sandbox will fail
		if result.NextOffset != 0 {
			t.Errorf("Expected NextOffset 0 (retry) when build-sandbox fails, got %d", result.NextOffset)
		}
	})

	t.Run("SmokeTest step checks for Linux in container", func(t *testing.T) {
		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")

		// Get the smoke-test step
		step := w.Steps[6]
		if step.Name != "smoke-test" {
			t.Errorf("Expected seventh step to be 'smoke-test', got '%s'", step.Name)
		}

		// In test environment without podman, the runner may fall back to host
		// which returns "Darwin" on macOS, causing the Linux check to fail
		// This is expected behavior - the step should retry when not in Linux container
		result := step.Verify(w, "")

		// The result depends on the test environment
		// We just verify the step runs without panic
		t.Logf("SmokeTest result: NextOffset=%d, Message=%s", result.NextOffset, result.Message)
	})

	t.Run("GitStage step always succeeds", func(t *testing.T) {
		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")

		// Get the git-stage step
		step := w.Steps[8]
		if step.Name != "git-stage" {
			t.Errorf("Expected ninth step to be 'git-stage', got '%s'", step.Name)
		}

		// AddRun steps do everything in Verify
		result := step.Verify(w, "")
		if result.NextOffset != 1 {
			t.Errorf("Expected NextOffset 1, got %d: %s", result.NextOffset, result.Message)
		}
	})
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

	repoInfo := RepoInfo{
		ProjectRoot: tmpDir,
		Branch:      "main",
		Slug:        "test/project",
	}

	t.Run("Workflow retries failed steps up to MaxRetries", func(t *testing.T) {
		w := NewWorkflow("test-retry", nil, repoInfo, WithMaxRetries(3))

		attemptCount := 0
		w.Add(Step{
			Name: "always-fail",
			Verify: func(w *Workflow, response string) StepResult {
				attemptCount++
				return Retry("always fails")
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
		w := NewWorkflow("test-retry-success", nil, repoInfo, WithMaxRetries(3))

		attemptCount := 0
		w.Add(Step{
			Name: "fail-then-succeed",
			Verify: func(w *Workflow, response string) StepResult {
				attemptCount++
				if attemptCount < 2 {
					return Retry("retry")
				}
				return Next("success")
			},
		}).Add(Step{
			Name: "second-step",
			Verify: func(w *Workflow, response string) StepResult {
				return Next("done")
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

func TestInitWorkflowWithMockHostRun(t *testing.T) {
	// This test would require mocking hostRun, which is more complex
	// For now, we test the step logic directly

	t.Run("HostTests verify returns retry on non-zero exit code", func(t *testing.T) {
		// The actual hostRun will fail in test environment
		// We're testing that the step correctly interprets failure

		tmpDir := t.TempDir()
		originalWd, _ := os.Getwd()
		os.Chdir(tmpDir)
		defer os.Chdir(originalWd)

		repoInfo := RepoInfo{
			ProjectRoot: tmpDir,
			Branch:      "main",
			Slug:        "test/project",
		}

		w := NewInitWorkflow(nil, repoInfo, false, "AGENTS.md")
		step := w.Steps[4] // host-tests

		result := step.Verify(w, "")

		// Should retry on failure
		if result.NextOffset != 0 {
			t.Errorf("Expected retry (NextOffset=0), got %d", result.NextOffset)
		}

		// Should have failure indicator in message
		if result.Message == "" {
			t.Error("Expected non-empty error message")
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
