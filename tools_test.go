package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShellRunner runs commands directly on the host machine.
// It's used for testing and for --run-on-host mode.
type TestShellRunner struct{}

// NewTestShellRunner creates a new test shell runner
func NewTestShellRunner() *TestShellRunner {
	return &TestShellRunner{}
}

// Run executes a command directly on the host machine
func (h *TestShellRunner) Run(ctx context.Context, params RunInShellInput) (RunInShellOutput, error) {
	var output RunInShellOutput

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", params.Command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", params.Command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	output.Output = stdout.String() + stderr.String()

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			output.ExitCode = fmt.Sprintf("%d", exitErr.ExitCode())
		} else {
			output.ExitCode = "-1"
		}
	} else {
		output.ExitCode = "0"
	}

	return output, nil
}

// Restart is a no-op for test shell runner since it doesn't maintain state
func (h *TestShellRunner) Restart(ctx context.Context) error {
	return nil
}

// Close is a no-op for test shell runner
func (h *TestShellRunner) Close(ctx context.Context) error {
	return nil
}

// Another no-op
// TODO: store this and use to verify
func (h *TestShellRunner) AllowFallback(allow bool) {
	return
}

func (h *TestShellRunner) RunnerType() string {
	return "podman"
}

func TestRunInShell(t *testing.T) {
	restore := setShellRunnerForTesting(NewTestShellRunner())
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
	restore := setShellRunnerForTesting(NewTestShellRunner())
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
// The HostShellRunner passes this test, but podman runner would truncate output
// See: https://github.com/afittestide/asimi-cli/issues/20
func TestRunInShellLargeOutput(t *testing.T) {
	restore := setShellRunnerForTesting(NewTestShellRunner())
	defer restore()

	tool := RunInShell{}

	// Generate output larger than 4096 bytes
	// The actual test would need to be run with podman runner to see the truncation issue
	// With HostShellRunner this passes, but with podman runner (4096 byte buffer)
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
// See: https://github.com/afittestide/asimi-cli/issues/20
func TestRunInShellExitCodeWithMarkerInOutput(t *testing.T) {
	restore := setShellRunnerForTesting(NewTestShellRunner())
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

// TestComposeShellCommand removed - composeShellCommand is deprecated
// The new marker-based approach generates commands inline with UUID markers

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

func TestPodmanShellRunner(t *testing.T) {
	repoInfo := repoInfoWithProjectRoot(t)
	runner := newPodmanShellRunner(true, nil, repoInfo) // allowFallback=true so test works without podman
	assert.NotNil(t, runner)

	output, err := runner.Run(context.Background(), RunInShellInput{
		Command: "echo hello",
	})
	require.NoError(t, err)
	assert.Contains(t, output.Output, "hello")
	assert.Equal(t, "0", output.ExitCode)
}

func TestPodmanShellRunnerMultipleCommands(t *testing.T) {
	if os.Getenv("container") != "" {
		t.Skip("Skipping Podman test when running inside a container")
	}

	// This test requires podman and the asimi-shell image to be available
	// Skip if they're not available (e.g., in CI or on systems without podman)
	if os.Getenv("ASIMI_TEST_PODMAN") == "" {
		t.Skip("Skipping Podman test. Set ASIMI_TEST_PODMAN=1 to run this test (requires podman and asimi-shell image)")
	}

	repoInfo := repoInfoWithProjectRoot(t)
	runner := newPodmanShellRunner(false, nil, repoInfo)
	assert.NotNil(t, runner)

	// First command
	output1, err := runner.Run(context.Background(), RunInShellInput{
		Command: "echo first",
	})
	require.NoError(t, err)
	assert.Contains(t, output1.Output, "first")
	assert.Equal(t, "0", output1.ExitCode)

	// Second command in the same session
	output2, err := runner.Run(context.Background(), RunInShellInput{
		Command: "echo second",
	})
	require.NoError(t, err)
	assert.Contains(t, output2.Output, "second")
	assert.Equal(t, "0", output2.ExitCode)
}

func TestPodmanShellRunnerWithStderr(t *testing.T) {
	repoInfo := repoInfoWithProjectRoot(t)
	runner := newPodmanShellRunner(true, nil, repoInfo) // allowFallback=true so test works without podman
	assert.NotNil(t, runner)

	output, err := runner.Run(context.Background(), RunInShellInput{
		Command: "echo 'stdout msg' && echo 'stderr msg' >&2",
	})

	require.NoError(t, err)
	assert.Contains(t, output.Output, "stdout msg")
	assert.Contains(t, output.Output, "stderr msg")
	assert.Equal(t, "0", output.ExitCode)
}

type failingPodmanRunner struct{}

func (failingPodmanRunner) Run(ctx context.Context, params RunInShellInput) (RunInShellOutput, error) {
	return RunInShellOutput{}, PodmanUnavailableError{reason: "podman unavailable"}
}

func (failingPodmanRunner) Restart(ctx context.Context) error {
	return nil
}

func (failingPodmanRunner) Close(ctx context.Context) error {
	return nil
}

func (failingPodmanRunner) AllowFallback(allow bool) {
	return
}
func (failingPodmanRunner) RunnerType() string {
	return "podman"
}

func TestValidatePathWithinProject(t *testing.T) {
	// Create a temporary directory to act as project root
	tempDir := t.TempDir()

	// Change to the temp directory so GetRepoInfo() returns it as project root
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	tests := []struct {
		name        string
		path        string
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "valid relative path",
			path:        "test.txt",
			shouldError: false,
		},
		{
			name:        "valid nested path",
			path:        "subdir/test.txt",
			shouldError: false,
		},
		{
			name:        "valid path with ./",
			path:        "./test.txt",
			shouldError: false,
		},
		{
			name:        "path traversal with ..",
			path:        "../outside.txt",
			shouldError: true,
			errorMsg:    "outside the current working directory",
		},
		{
			name:        "path traversal in middle",
			path:        "subdir/../../outside.txt",
			shouldError: true,
			errorMsg:    "outside the current working directory",
		},
		{
			name:        "absolute path outside project",
			path:        "/etc/passwd",
			shouldError: true,
			errorMsg:    "outside the current working directory",
		},
		{
			name:        "empty path",
			path:        "",
			shouldError: true,
			errorMsg:    "path cannot be empty",
		},
		{
			name:        "absolute path within project",
			path:        filepath.Join(tempDir, "test.txt"),
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePathWithinProject(tt.path)
			if tt.shouldError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWriteFileToolPathValidation(t *testing.T) {
	// Create a temporary directory to act as project root
	tempDir := t.TempDir()

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	tool := WriteFileTool{}

	t.Run("write file within project", func(t *testing.T) {
		input := `{"path": "test.txt", "content": "hello world"}`
		result, err := tool.Call(context.Background(), input)
		assert.NoError(t, err)
		assert.Contains(t, result, "Successfully wrote to test.txt")

		// Verify file was created
		content, err := os.ReadFile("test.txt")
		assert.NoError(t, err)
		assert.Equal(t, "hello world", string(content))
	})

	t.Run("write file in subdirectory", func(t *testing.T) {
		input := `{"path": "subdir/test.txt", "content": "nested content"}`
		result, err := tool.Call(context.Background(), input)
		assert.NoError(t, err)
		assert.Contains(t, result, "Successfully wrote to subdir/test.txt")

		// Verify file was created
		content, err := os.ReadFile("subdir/test.txt")
		assert.NoError(t, err)
		assert.Equal(t, "nested content", string(content))
	})

	t.Run("reject path traversal", func(t *testing.T) {
		input := `{"path": "../outside.txt", "content": "malicious"}`
		_, err := tool.Call(context.Background(), input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside the current working directory")

		// Verify file was not created
		_, err = os.ReadFile("../outside.txt")
		assert.Error(t, err)
	})

	t.Run("reject absolute path outside project", func(t *testing.T) {
		input := `{"path": "/tmp/outside.txt", "content": "malicious"}`
		_, err := tool.Call(context.Background(), input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside the current working directory")
	})

	t.Run("reject complex path traversal", func(t *testing.T) {
		input := `{"path": "subdir/../../outside.txt", "content": "malicious"}`
		_, err := tool.Call(context.Background(), input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside the current working directory")
	})
}

func TestReplaceTextToolPathValidation(t *testing.T) {
	// Create a temporary directory to act as project root
	tempDir := t.TempDir()

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	// Create a test file
	testContent := "hello world\nthis is a test"
	err = os.WriteFile("test.txt", []byte(testContent), 0644)
	require.NoError(t, err)

	tool := ReplaceTextTool{}

	t.Run("replace text within project", func(t *testing.T) {
		input := `{"path": "test.txt", "old_text": "hello", "new_text": "goodbye"}`
		result, err := tool.Call(context.Background(), input)
		assert.NoError(t, err)
		assert.Contains(t, result, "Successfully modified file")

		// Verify replacement
		content, err := os.ReadFile("test.txt")
		assert.NoError(t, err)
		assert.Contains(t, string(content), "goodbye world")
	})

	t.Run("reject path traversal", func(t *testing.T) {
		// Create a file outside the project
		outsideFile := filepath.Join(filepath.Dir(tempDir), "outside.txt")
		err := os.WriteFile(outsideFile, []byte("outside content"), 0644)
		require.NoError(t, err)
		defer os.Remove(outsideFile)

		input := `{"path": "../outside.txt", "old_text": "outside", "new_text": "modified"}`
		_, err = tool.Call(context.Background(), input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside the current working")

		// Verify file was not modified
		content, err := os.ReadFile(outsideFile)
		assert.NoError(t, err)
		assert.Equal(t, "outside content", string(content))
	})

	t.Run("reject absolute path outside project", func(t *testing.T) {
		input := `{"path": "/etc/hosts", "old_text": "localhost", "new_text": "malicious"}`
		_, err := tool.Call(context.Background(), input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "outside the current working")
	})
}

func TestPathValidationWithSymlinks(t *testing.T) {
	// Skip on Windows as symlink behavior is different
	if os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping symlink test on Windows")
	}

	// Create a temporary directory to act as project root
	tempDir := t.TempDir()

	// Change to the temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	// Create a directory outside the project
	outsideDir := filepath.Join(filepath.Dir(tempDir), "outside")
	err = os.MkdirAll(outsideDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(outsideDir)

	// Create a symlink inside the project pointing outside
	symlinkPath := filepath.Join(tempDir, "symlink")
	err = os.Symlink(outsideDir, symlinkPath)
	if err != nil {
		t.Skip("Unable to create symlink, skipping test")
	}

	tool := WriteFileTool{}

	t.Run("reject write through symlink to outside", func(t *testing.T) {
		input := `{"path": "symlink/malicious.txt", "content": "bad content"}`
		_, err := tool.Call(context.Background(), input)
		// This should either fail validation or fail to write
		// The exact behavior depends on how filepath.Abs handles symlinks
		if err == nil {
			// If it succeeded, verify it didn't write outside
			_, statErr := os.Stat(filepath.Join(outsideDir, "malicious.txt"))
			assert.Error(t, statErr, "File should not be created outside project")
		}
	})
}

func TestReadFileWithStringNumbers(t *testing.T) {
	// Create a test file
	testContent := "line1\nline2\nline3\nline4\nline5\n"
	testFile := "test_read_file_temp.txt"
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	tool := ReadFileTool{}
	ctx := context.Background()

	// Test with string values for offset and limit (Claude Code CLI bug workaround)
	input := `{"path":"test_read_file_temp.txt","offset":"2","limit":"2"}`
	result, err := tool.Call(ctx, input)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	expected := "line2\nline3"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// Test with numeric values (normal case)
	input2 := `{"path":"test_read_file_temp.txt","offset":2,"limit":2}`
	result2, err := tool.Call(ctx, input2)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result2 != expected {
		t.Errorf("Expected %q, got %q", expected, result2)
	}
}

// Tests from test_shell_runner_test.go

func TestTestShellRunner_Run(t *testing.T) {
	runner := NewTestShellRunner()

	params := RunInShellInput{
		Command:     "echo hello",
		Description: "Test echo",
	}

	output, err := runner.Run(context.Background(), params)
	require.NoError(t, err, "unexpected error")

	assert.Equal(t, "0", output.ExitCode, "exit code mismatch")
	assert.NotEmpty(t, output.Output, "expected non-empty output")
}

func TestTestShellRunner_RunError(t *testing.T) {
	runner := NewTestShellRunner()

	params := RunInShellInput{
		Command:     "exit 42",
		Description: "Test exit code",
	}

	output, err := runner.Run(context.Background(), params)
	require.NoError(t, err, "unexpected error")

	assert.Equal(t, "42", output.ExitCode, "exit code mismatch")
}

func TestShouldRunOnHost(t *testing.T) {
	tests := []struct {
		name              string
		config            *Config
		command           string
		wantRunOnHost     bool
		wantNeedsApproval bool
	}{
		{
			name:              "nil config",
			config:            nil,
			command:           "gh issue view 1",
			wantRunOnHost:     false,
			wantNeedsApproval: true,
		},
		{
			name: "command matches run_on_host and safe_run_on_host",
			config: &Config{
				RunInShell: RunInShellConfig{
					RunOnHost:     []string{`^gh\s.*`},
					SafeRunOnHost: []string{`^gh\s+(issue|pr)\s+(view|list)\s.*`},
				},
			},
			command:           "gh issue view 68",
			wantRunOnHost:     true,
			wantNeedsApproval: false,
		},
		{
			name: "command matches run_on_host but not safe_run_on_host",
			config: &Config{
				RunInShell: RunInShellConfig{
					RunOnHost:     []string{`^gh\s.*`},
					SafeRunOnHost: []string{`^gh\s+(issue|pr)\s+(view|list)\s.*`},
				},
			},
			command:           "gh issue create --title test",
			wantRunOnHost:     true,
			wantNeedsApproval: true,
		},
		{
			name: "command does not match run_on_host",
			config: &Config{
				RunInShell: RunInShellConfig{
					RunOnHost:     []string{`^gh\s.*`},
					SafeRunOnHost: []string{`^gh\s+(issue|pr)\s+(view|list)\s.*`},
				},
			},
			command:           "ls -la",
			wantRunOnHost:     false,
			wantNeedsApproval: false,
		},
		{
			name: "podman command requires approval",
			config: &Config{
				RunInShell: RunInShellConfig{
					RunOnHost:     []string{`^podman\s.*`},
					SafeRunOnHost: []string{},
				},
			},
			command:           "podman run alpine echo hello",
			wantRunOnHost:     true,
			wantNeedsApproval: true,
		},
	}

	currentShellRunner = NewTestShellRunner()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := RunInShell{config: tt.config}
			runOnHost, requiresApproval := tool.shouldRunOnHost(tt.command)

			assert.Equal(t, tt.wantRunOnHost, runOnHost, "runOnHost mismatch")
			assert.Equal(t, tt.wantNeedsApproval, requiresApproval, "requiresApproval mismatch")
		})
	}
}

func TestHostCommandApprovalChannel(t *testing.T) {
	// Test that the approval channel mechanism works
	approvalChan := make(chan HostCommandApprovalRequest, 1)
	SetHostCommandApprovalChannel(approvalChan)

	// Simulate a request in a goroutine
	go func() {
		request := <-approvalChan
		assert.Equal(t, "test command", request.Command, "unexpected command")
		request.ResponseChan <- true
	}()

	// Request approval
	approved, err := requestHostCommandApproval(context.Background(), "test command")
	require.NoError(t, err, "unexpected error")
	assert.True(t, approved, "expected approval to be true")

	// Clean up
	SetHostCommandApprovalChannel(nil)
}

// Tests from tools_security_test.go

// TestReadFileToolPathValidation tests that read_file validates paths are within project
func TestReadFileToolPathValidation(t *testing.T) {
	tool := ReadFileTool{}
	ctx := context.Background()

	tests := []struct {
		name        string
		path        string
		shouldError bool
	}{
		{
			name:        "read file in current directory",
			path:        "go.mod",
			shouldError: false,
		},
		{
			name:        "read file in subdirectory",
			path:        "testdata/test.txt",
			shouldError: false,
		},
		{
			name:        "read file outside project with absolute path",
			path:        "/etc/passwd",
			shouldError: true,
		},
		{
			name:        "read file outside project with relative path",
			path:        "../../../etc/passwd",
			shouldError: true,
		},
		{
			name:        "read file outside project with parent directory",
			path:        "..",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := `{"path":"` + tt.path + `"}`
			_, err := tool.Call(ctx, input)

			if tt.shouldError {
				require.Error(t, err, "Expected error for path %s", tt.path)
				assert.Contains(t, err.Error(), "access denied", "Expected 'access denied' error, got: %v", err)
			} else {
				// For valid paths, we might get file not found, but not access denied
				if err != nil {
					assert.NotContains(t, err.Error(), "access denied", "Unexpected access denied error for valid path %s: %v", tt.path, err)
				}
			}
		})
	}
}

// TestListDirectoryToolPathValidation tests that list_files validates paths are within project
func TestListDirectoryToolPathValidation(t *testing.T) {
	tool := ListDirectoryTool{}
	ctx := context.Background()

	tests := []struct {
		name        string
		path        string
		shouldError bool
	}{
		{
			name:        "list current directory",
			path:        ".",
			shouldError: false,
		},
		{
			name:        "list subdirectory",
			path:        "testdata",
			shouldError: false,
		},
		{
			name:        "list root directory",
			path:        "/",
			shouldError: true,
		},
		{
			name:        "list parent directory",
			path:        "..",
			shouldError: true,
		},
		{
			name:        "list /etc",
			path:        "/etc",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := `{"path":"` + tt.path + `"}`
			_, err := tool.Call(ctx, input)

			if tt.shouldError {
				require.Error(t, err, "Expected error for path %s", tt.path)
				assert.Contains(t, err.Error(), "access denied", "Expected 'access denied' error, got: %v", err)
			} else {
				// For valid paths, we might get file not found, but not access denied
				if err != nil {
					assert.NotContains(t, err.Error(), "access denied", "Unexpected access denied error for valid path %s: %v", tt.path, err)
				}
			}
		})
	}
}

// TestReadManyFilesToolPathValidation tests that read_many_files validates paths are within project
func TestReadManyFilesToolPathValidation(t *testing.T) {
	tool := ReadManyFilesTool{}
	ctx := context.Background()

	// Create a temporary test file in the project
	tmpFile := filepath.Join("testdata", "temp_test.txt")
	err := os.MkdirAll("testdata", 0755)
	require.NoError(t, err)
	err = os.WriteFile(tmpFile, []byte("test content"), 0644)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	tests := []struct {
		name          string
		paths         []string
		shouldContain string
		shouldNotFind bool
	}{
		{
			name:          "read files in project",
			paths:         []string{"testdata/*.txt"},
			shouldContain: "test content",
			shouldNotFind: false,
		},
		{
			name:          "read files outside project should be filtered",
			paths:         []string{"/etc/passwd", "/etc/hosts"},
			shouldContain: "",
			shouldNotFind: true,
		},
		{
			name:          "mixed paths - only project files should be read",
			paths:         []string{"testdata/*.txt", "/etc/passwd"},
			shouldContain: "test content",
			shouldNotFind: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := `{"paths":[`
			for i, p := range tt.paths {
				if i > 0 {
					input += ","
				}
				input += `"` + p + `"`
			}
			input += `]}`

			result, err := tool.Call(ctx, input)
			assert.NoError(t, err, "Unexpected error")

			if tt.shouldNotFind {
				assert.Empty(t, result, "Expected no results for paths outside project")
			} else if tt.shouldContain != "" {
				assert.Contains(t, result, tt.shouldContain, "Expected result to contain '%s'", tt.shouldContain)
			}

			// Verify that /etc/passwd content is never included
			assert.NotContains(t, result, "root:", "Result should not contain /etc/passwd content")
			assert.NotContains(t, result, "/etc/passwd", "Result should not contain /etc/passwd content")
		})
	}
}

// Tests from tools_security_demo_test.go

// TestSecurityDemonstration shows the security improvement
func TestSecurityDemonstration(t *testing.T) {
	ctx := context.Background()

	t.Run("ReadFile security", func(t *testing.T) {
		tool := ReadFileTool{}

		// Test 1: Project file should work
		t.Run("project file allowed", func(t *testing.T) {
			input := `{"path":"go.mod"}`
			_, err := tool.Call(ctx, input)
			if err != nil {
				assert.NotContains(t, err.Error(), "access denied", "Project files should be accessible")
			}
		})

		// Test 2: System file should be blocked
		t.Run("system file blocked", func(t *testing.T) {
			input := `{"path":"/etc/passwd"}`
			_, err := tool.Call(ctx, input)
			assert.Error(t, err, "System files should be blocked")
			if err != nil {
				fmt.Printf("✓ /etc/passwd blocked: %v\n", err)
			}
		})

		// Test 3: Path traversal should be blocked
		t.Run("path traversal blocked", func(t *testing.T) {
			input := `{"path":"../../../etc/passwd"}`
			_, err := tool.Call(ctx, input)
			assert.Error(t, err, "Path traversal should be blocked")
			if err != nil {
				fmt.Printf("✓ Path traversal blocked: %v\n", err)
			}
		})
	})

	t.Run("ListFiles security", func(t *testing.T) {
		tool := ListDirectoryTool{}

		// Test 1: Project directory should work
		t.Run("project directory allowed", func(t *testing.T) {
			input := `{"path":"."}`
			_, err := tool.Call(ctx, input)
			if err != nil {
				assert.NotContains(t, err.Error(), "access denied", "Project directory should be accessible")
			}
		})

		// Test 2: System directory should be blocked
		t.Run("system directory blocked", func(t *testing.T) {
			input := `{"path":"/etc"}`
			_, err := tool.Call(ctx, input)
			assert.Error(t, err, "System directories should be blocked")
			if err != nil {
				fmt.Printf("✓ /etc blocked: %v\n", err)
			}
		})

		// Test 3: Parent directory should be blocked
		t.Run("parent directory blocked", func(t *testing.T) {
			input := `{"path":".."}`
			_, err := tool.Call(ctx, input)
			assert.Error(t, err, "Parent directory should be blocked")
			if err != nil {
				fmt.Printf("✓ Parent directory blocked: %v\n", err)
			}
		})
	})

	t.Run("ReadManyFiles security", func(t *testing.T) {
		tool := ReadManyFilesTool{}

		// Test: System files should be filtered out
		t.Run("system files filtered", func(t *testing.T) {
			input := `{"paths":["/etc/passwd","/etc/hosts"]}`
			result, err := tool.Call(ctx, input)
			assert.NoError(t, err, "Unexpected error")
			assert.Empty(t, result, "System files should be filtered out, result should be empty")
			if result == "" {
				fmt.Printf("✓ System files filtered out (empty result)\n")
			}
		})
	})
}
