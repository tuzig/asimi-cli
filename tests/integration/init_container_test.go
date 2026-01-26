// Package integration contains tests that require real container infrastructure.
// These tests are slower and require podman to be available.
//
// Run with: go test -tags=integration ./tests/integration/...
// Or: just test-integration
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInitRealContainer tests the full :init workflow with actual container builds.
// This test requires podman to be installed and running.
func TestInitRealContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if podman is available
	if !isPodmanAvailable() {
		t.Skip("podman is not available, skipping container test")
	}

	// Use a test project
	projectDir := setupIntegrationProject(t)
	defer os.RemoveAll(projectDir)

	// Change to project directory
	originalDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(originalDir)

	t.Run("build-sandbox creates container", func(t *testing.T) {
		// Create a minimal Dockerfile
		dockerfileContent := `FROM alpine:latest
RUN apk add --no-cache bash git
CMD ["bash"]
`
		os.MkdirAll(".agents/sandbox", 0755)
		os.WriteFile(".agents/sandbox/Dockerfile", []byte(dockerfileContent), 0644)

		// Create a minimal Justfile
		justfileContent := "default:\n\t@echo \"default\"\n\nbuild-sandbox:\n\tpodman build -t localhost/asimi-sandbox-test-integration:latest -f .agents/sandbox/Dockerfile .\n\ntest:\n\t@echo \"tests pass\"\n"
		os.WriteFile("Justfile", []byte(justfileContent), 0644)

		// Run build-sandbox
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "just", "build-sandbox")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build-sandbox failed: %v\nOutput: %s", err, output)
		}

		// Verify image was created
		cmd = exec.Command("podman", "image", "exists", "localhost/asimi-sandbox-test-integration:latest")
		if err := cmd.Run(); err != nil {
			t.Error("Container image was not created")
		}

		// Clean up the image
		defer func() {
			exec.Command("podman", "rmi", "localhost/asimi-sandbox-test-integration:latest").Run()
		}()
	})
}

// TestContainerSmoke tests basic container functionality
func TestContainerSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPodmanAvailable() {
		t.Skip("podman is not available, skipping container test")
	}

	t.Run("can run commands in container", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Run a simple command in a throwaway container
		cmd := exec.CommandContext(ctx, "podman", "run", "--rm", "alpine:latest", "echo", "hello")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("podman run failed: %v\nOutput: %s", err, output)
		}

		if string(output) != "hello\n" {
			t.Errorf("Expected 'hello\\n', got %q", output)
		}
	})

	t.Run("can mount volumes", func(t *testing.T) {
		// Create temp directory in current working directory for podman accessibility
		// Using t.TempDir() fails with podman machine where /tmp isn't shared
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get working directory: %v", err)
		}
		tmpDir, err := os.MkdirTemp(cwd, ".asimi-smoke-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		testFile := filepath.Join(tmpDir, "test.txt")
		os.WriteFile(testFile, []byte("test content"), 0644)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "podman", "run", "--rm",
			"-v", tmpDir+":/mnt:ro",
			"alpine:latest", "cat", "/mnt/test.txt")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("podman run with volume failed: %v\nOutput: %s", err, output)
		}

		if string(output) != "test content" {
			t.Errorf("Expected 'test content', got %q", output)
		}
	})
}

// TestFullInitWorkflow tests the complete init workflow including container build
func TestFullInitWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if !isPodmanAvailable() {
		t.Skip("podman is not available, skipping container test")
	}

	projectDir := setupIntegrationProject(t)
	imageName := "localhost/asimi-sandbox-integration-full:latest"

	// Clean up
	defer func() {
		exec.Command("podman", "rmi", imageName).Run()
		os.RemoveAll(projectDir)
	}()

	originalDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(originalDir)

	// Create all necessary files
	os.MkdirAll(".agents/sandbox", 0755)

	// Create Dockerfile
	dockerfile := `FROM node:20-slim
WORKDIR /workspace
RUN apt-get update && apt-get install -y git bash curl && rm -rf /var/lib/apt/lists/*
# Install just
RUN curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/bin
CMD ["bash"]
`
	os.WriteFile(".agents/sandbox/Dockerfile", []byte(dockerfile), 0644)

	// Create bashrc
	bashrc := `# Asimi sandbox bashrc
export PS1='$ '
`
	os.WriteFile(".agents/sandbox/bashrc", []byte(bashrc), 0644)

	// Create Justfile
	justfile := "default:\n\t@echo \"default\"\n\nbuild-sandbox:\n\tpodman build -t " + imageName + " -f .agents/sandbox/Dockerfile .\n\ntest:\n\t@echo \"All tests passed!\"\n"
	os.WriteFile("Justfile", []byte(justfile), 0644)

	// Create AGENTS.md
	agents := `# Project Guidelines
This is a test project for integration testing.
`
	os.WriteFile("AGENTS.md", []byte(agents), 0644)

	t.Run("just build-sandbox succeeds", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "just", "build-sandbox")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build-sandbox failed: %v\nOutput: %s", err, output)
		}
	})

	t.Run("just test succeeds on host", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "just", "test")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("just test on host failed: %v\nOutput: %s", err, output)
		}
	})

	t.Run("just test succeeds in container", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Run test in container - mount to /workspace which is the WORKDIR in the Dockerfile
		cmd := exec.CommandContext(ctx, "podman", "run", "--rm",
			"-v", projectDir+":/workspace:Z",
			"-w", "/workspace",
			imageName,
			"just", "test")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("just test in container failed: %v\nOutput: %s", err, output)
		}
	})
}

// Helper functions

func isPodmanAvailable() bool {
	cmd := exec.Command("podman", "version")
	return cmd.Run() == nil
}

// cleanGitEnv returns a copy of os.Environ() with git hook variables removed.
// This is necessary when running git commands from within a git hook (e.g., pre-commit),
// where GIT_DIR, GIT_INDEX_FILE, etc. would interfere with creating a new repo.
func cleanGitEnv() []string {
	gitVars := map[string]bool{
		"GIT_DIR":                          true,
		"GIT_WORK_TREE":                    true,
		"GIT_INDEX_FILE":                   true,
		"GIT_OBJECT_DIRECTORY":             true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
		"GIT_QUARANTINE_PATH":              true,
	}

	var env []string
	for _, e := range os.Environ() {
		name := e
		if idx := strings.Index(e, "="); idx != -1 {
			name = e[:idx]
		}
		if !gitVars[name] {
			env = append(env, e)
		}
	}
	return env
}

func setupIntegrationProject(t *testing.T) string {
	t.Helper()

	// Create temp directory in a location that is:
	// 1. NOT inside the current git repo (prevents accidental repo corruption)
	// 2. Accessible to podman (on macOS, $HOME is shared but /tmp is not)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	// Use ~/.cache/asimi-test as base directory for test temps
	baseDir := filepath.Join(homeDir, ".cache", "asimi-test")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("Failed to create base temp dir: %v", err)
	}

	tmpDir, err := os.MkdirTemp(baseDir, "integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Clean environment to remove git hook variables (GIT_DIR, etc.)
	cleanEnv := cleanGitEnv()

	// Initialize git repo - all commands check errors and verify directory
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "--local", "user.email", "test@asimi.dev"},
		{"git", "config", "--local", "user.name", "Asimi Integration Test"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		cmd.Env = cleanEnv
		if output, err := cmd.CombinedOutput(); err != nil {
			os.RemoveAll(tmpDir) // Clean up on failure
			t.Fatalf("git command %v failed: %v\nOutput: %s", args, err, output)
		}
	}

	// Verify .git directory was created (safety check)
	gitDir := filepath.Join(tmpDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		os.RemoveAll(tmpDir)
		t.Fatalf("git init did not create .git directory in %s", tmpDir)
	}

	// Create initial file and commit - with error checking
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test Project\n"), 0644); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to write README.md: %v", err)
	}

	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = tmpDir
	cmd.Env = cleanEnv
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("git add failed: %v\nOutput: %s", err, output)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tmpDir
	cmd.Env = append(cleanEnv,
		"GIT_AUTHOR_DATE=2024-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2024-01-01T00:00:00Z",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("git commit failed: %v\nOutput: %s", err, output)
	}

	return tmpDir
}
