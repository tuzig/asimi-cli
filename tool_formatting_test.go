package main

import (
	"strings"
	"testing"
)

func TestFormatToolCall(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    string
		result   string
		err      error
		expected string
	}{
		{
			name:     "read_file success",
			toolName: "read_file",
			input:    `{"path": "test.txt"}`,
			result:   "Hello\nWorld\nTest",
			err:      nil,
			expected: "- Read File(test.txt)\n  ⎿  Read 3 lines",
		},
		{
			name:     "read_file with offset and limit",
			toolName: "read_file",
			input:    `{"path": "test.txt", "offset": 2, "limit": 2}`,
			result:   "World\nTest",
			err:      nil,
			expected: "- Read File(test.txt)\n  ⎿  Read 2 lines",
		},
		{
			name:     "write_file success",
			toolName: "write_file",
			input:    `{"path": "output.txt", "content": "test content"}`,
			result:   "Successfully wrote to output.txt",
			err:      nil,
			expected: "- Write File(output.txt)\n  ⎿  File written successfully",
		},
		{
			name:     "list_files success",
			toolName: "list_files",
			input:    `{"path": "."}`,
			result:   "file1.txt\nfile2.txt\ndir1",
			err:      nil,
			expected: "- List Files(.)\n  ⎿  Found 3 items",
		},
		{
			name:     "run_shell_command success",
			toolName: "run_shell_command",
			input:    `{"command": "echo hello", "description": "test"}`,
			result:   `{"stdout":"hello\n","stderr":"","exitCode":0}`,
			err:      nil,
			expected: "- Run Shell Command(echo hello)\n  ⎿  Command completed successfully",
		},
		{
			name:     "run_shell_command failure",
			toolName: "run_shell_command",
			input:    `{"command": "false", "description": "test"}`,
			result:   `{"stdout":"","stderr":"","exitCode":1}`,
			err:      nil,
			expected: "- Run Shell Command(false)\n  ⎿  Command failed (exit code 1)",
		},
		{
			name:     "read_many_files success",
			toolName: "read_many_files",
			input:    `{"paths": ["*.txt", "*.go"]}`,
			result:   "---\tfile1.txt---\ncontent1\n---\tfile2.go---\ncontent2\n",
			err:      nil,
			expected: "- Read Many Files(2 files)\n  ⎿  Read 2 files",
		},
		{
			name:     "tool error",
			toolName: "read_file",
			input:    `{"path": "nonexistent.txt"}`,
			result:   "",
			err:      &testError{msg: "file not found"},
			expected: "- Read File(nonexistent.txt)\n  ⎿  Error: file not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToolCall(tt.toolName, "-", tt.input, tt.result, tt.err)
			if result != tt.expected {
				t.Errorf("formatToolCall() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatToolCallLongCommand(t *testing.T) {
	longCommand := "this is a very long command that should be truncated because it exceeds the limit"
	input := `{"command": "` + longCommand + `", "description": "test"}`
	result := formatToolCall("run_shell_command", "-", input, `{"exitCode":0}`, nil)

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}

	// Check that the command was truncated
	if !strings.Contains(lines[0], "...") {
		t.Errorf("Expected command to be truncated with '...', got: %s", lines[0])
	}

	// Check that the truncated part is around 50 characters
	if len(lines[0]) > 80 { // Allow some buffer for the prefix and formatting
		t.Errorf("Command line too long after truncation: %s", lines[0])
	}
}

// testError implements error interface for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
