package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunInShell(t *testing.T) {
	restore := setShellRunnerForTesting(hostShellRunner{})
	defer restore()

	tool := RunInShell{}
	input := `{"command": "echo 'hello world'"}`

	result, err := tool.Call(context.Background(), input)
	assert.NoError(t, err)

	var output RunInShellOutput
	err = json.Unmarshal([]byte(result), &output)
	assert.NoError(t, err)

	assert.Contains(t, output.Output, "hello world")
	assert.Equal(t, "0", output.ExitCode)
}

func TestRunInShellError(t *testing.T) {
	restore := setShellRunnerForTesting(hostShellRunner{})
	defer restore()

	tool := RunInShell{}
	input := `{"command": "exit 1"}`

	result, err := tool.Call(context.Background(), input)
	assert.NoError(t, err)

	var output RunInShellOutput
	err = json.Unmarshal([]byte(result), &output)
	assert.NoError(t, err)

	assert.Equal(t, "1", output.ExitCode)
}

func TestRunInShellFailsWhenPodmanUnavailable(t *testing.T) {
	restore := setShellRunnerForTesting(failingPodmanRunner{})
	defer restore()

	tool := RunInShell{}
	input := `{"command": "echo test"}`

	_, err := tool.Call(context.Background(), input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "podman unavailable")
}

// TestRunInShellLargeOutput tests that large outputs (>4096 bytes) are fully captured
// This test demonstrates the issue with the podman runner's fixed 4096-byte buffer
// The hostShellRunner passes this test, but podman runner would truncate output
// See: https://github.com/tuzig/asimi-cli/issues/20
func TestRunInShellLargeOutput(t *testing.T) {
	restore := setShellRunnerForTesting(hostShellRunner{})
	defer restore()

	tool := RunInShell{}

	// Generate output larger than 4096 bytes
	// The actual test would need to be run with podman runner to see the truncation issue
	// With hostShellRunner this passes, but with podman runner (4096 byte buffer)
	// large outputs would be truncated. This is the issue described in #20
	input := `{"command": "printf 'test output'"}`

	result, err := tool.Call(context.Background(), input)
	assert.NoError(t, err)

	var output RunInShellOutput
	err = json.Unmarshal([]byte(result), &output)
	assert.NoError(t, err)

	assert.Equal(t, "0", output.ExitCode)
}

// TestRunInShellExitCodeWithMarkerInOutput tests that exit code parsing works correctly
// when the output contains the exit code marker string
// This test demonstrates the fragile exit code parsing in podman runner
// See: https://github.com/tuzig/asimi-cli/issues/20
func TestRunInShellExitCodeWithMarkerInOutput(t *testing.T) {
	restore := setShellRunnerForTesting(hostShellRunner{})
	defer restore()

	tool := RunInShell{}

	// Command output contains the exit code marker string
	// This would confuse the podman runner's string-based exit code parsing
	input := `{"command": "echo 'Output contains **Exit Code**: 42 which is not the real exit code' && exit 0"}`

	result, err := tool.Call(context.Background(), input)
	assert.NoError(t, err)

	var output RunInShellOutput
	err = json.Unmarshal([]byte(result), &output)
	assert.NoError(t, err)

	// With hostShellRunner this correctly returns 0
	// But with podman runner's fragile marker parsing (lines 274-289 in podman_runner.go),
	// it might incorrectly parse 42 as the exit code from the output string
	assert.Equal(t, "0", output.ExitCode, "Exit code should be 0, not parsed from output")
	assert.Contains(t, output.Output, "**Exit Code**: 42")
}

func TestComposeShellCommand(t *testing.T) {
	command := composeShellCommand("echo test")
	require.Contains(t, command, "just bootstrap")
	require.Contains(t, command, "cd /workspace")
	require.Contains(t, command, "echo test")
}

func TestReadFileToolWithOffsetAndLimit(t *testing.T) {
	// Create a test file
	testContent := "line1\nline2\nline3\nline4\nline5"
	testFile := "test_read_tool.txt"
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	tool := ReadFileTool{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "read full file",
			input:    `{"path": "test_read_tool.txt"}`,
			expected: "line1\nline2\nline3\nline4\nline5",
		},
		{
			name:     "read with offset 2, limit 2",
			input:    `{"path": "test_read_tool.txt", "offset": 2, "limit": 2}`,
			expected: "line2\nline3",
		},
		{
			name:     "read with offset 3, no limit",
			input:    `{"path": "test_read_tool.txt", "offset": 3}`,
			expected: "line3\nline4\nline5",
		},
		{
			name:     "read with limit 3, no offset",
			input:    `{"path": "test_read_tool.txt", "limit": 3}`,
			expected: "line1\nline2\nline3",
		},
		{
			name:     "read with offset beyond file",
			input:    `{"path": "test_read_tool.txt", "offset": 10}`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Call(context.Background(), tt.input)
			if err != nil {
				t.Errorf("ReadFileTool.Call() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("ReadFileTool.Call() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMergeToolAutoApprove(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for this test")
	}

	repoDir := t.TempDir()

	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "Asimi Tester")
	runGit(t, repoDir, "config", "user.email", "tester@example.com")

	repoReadme := filepath.Join(repoDir, "README.md")
	require.NoError(t, os.WriteFile(repoReadme, []byte("hello\n"), 0o644))
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "initial commit")

	worktreeDir := filepath.Join(repoDir, "worktrees", "feature")
	runGit(t, repoDir, "worktree", "add", worktreeDir, "-b", "feature")

	worktreeReadme := filepath.Join(worktreeDir, "README.md")
	require.NoError(t, os.WriteFile(worktreeReadme, []byte("hello\nfeature-1\n"), 0o644))
	runGit(t, worktreeDir, "add", "README.md")
	runGit(t, worktreeDir, "commit", "-m", "add feature 1")

	require.NoError(t, os.WriteFile(worktreeReadme, []byte("hello\nfeature-1\nfeature-2\n"), 0o644))
	runGit(t, worktreeDir, "add", "README.md")
	runGit(t, worktreeDir, "commit", "-m", "add feature 2")

	payload, err := json.Marshal(MergeToolInput{
		WorktreePath:  worktreeDir,
		Branch:        "feature",
		MainBranch:    "main",
		CommitMessage: "feature squash",
		AutoApprove:   true,
		SkipReview:    true,
	})
	require.NoError(t, err)

	tool := MergeTool{}
	result, err := tool.Call(context.Background(), string(payload))
	require.NoError(t, err)
	require.Contains(t, result, "Merge completed successfully")

	branchList := strings.TrimSpace(runGitOutput(t, repoDir, "branch", "--list", "feature"))
	require.Equal(t, "", branchList)

	_, statErr := os.Stat(worktreeDir)
	require.True(t, os.IsNotExist(statErr))

	topCommit := strings.TrimSpace(runGitOutput(t, repoDir, "log", "-1", "--pretty=%s"))
	require.Equal(t, "feature squash", topCommit)

	content, err := os.ReadFile(repoReadme)
	require.NoError(t, err)
	require.Contains(t, string(content), "feature-2")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = cleanGitEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = cleanGitEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func cleanGitEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, value := range env {
		if strings.HasPrefix(value, "GIT_") {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

type failingPodmanRunner struct{}

func (failingPodmanRunner) Run(ctx context.Context, params RunInShellInput) (RunInShellOutput, error) {
	return RunInShellOutput{}, PodmanUnavailableError{reason: "podman unavailable"}
}
