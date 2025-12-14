package main

import (
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
			expected: "- Read File test.txt\n ╰ Read 3 lines\n",
		},
		{
			name:     "read_file with offset and limit",
			toolName: "read_file",
			input:    `{"path": "test.txt", "offset": 2, "limit": 2}`,
			result:   "World\nTest",
			err:      nil,
			expected: "- Read File test.txt\n ╰ Read 2 lines\n",
		},
		{
			name:     "write_file success",
			toolName: "write_file",
			input:    `{"path": "output.txt", "content": "test content"}`,
			result:   "Successfully wrote to output.txt",
			err:      nil,
			expected: "- Write File output.txt\n ╰ File written successfully\n",
		},
		{
			name:     "list_files success",
			toolName: "list_files",
			input:    `{"path": "."}`,
			result:   "file1.txt\nfile2.txt\ndir1",
			err:      nil,
			expected: "- List Files .\n ╰ Found 3 items\n",
		},
		{
			name:     "run_shell_command success",
			toolName: "run_shell_command",
			input:    `{"command": "echo hello", "description": "test"}`,
			result:   `{"output":"hello\n","exitCode":"0"}`,
			err:      nil,
			expected: "- test\n ╰ $ echo hello\n",
		},
		{
			name:     "run_shell_command failure",
			toolName: "run_shell_command",
			input:    `{"command": "false", "description": "test"}`,
			result:   `{"output":"","exitCode":"1"}`,
			err:      nil,
			expected: "- test\n │ $ false\n ╰ 1\n",
		},
		{
			name:     "read_many_files success",
			toolName: "read_many_files",
			input:    `{"paths": ["*.txt", "*.go"]}`,
			result:   "---\tfile1.txt---\ncontent1\n---\tfile2.go---\ncontent2\n",
			err:      nil,
			expected: "- Read Many Files(2 files)\n ╰ Read 2 files\n",
		},
		{
			name:     "tool error",
			toolName: "read_file",
			input:    `{"path": "nonexistent.txt"}`,
			result:   "",
			err:      &testError{msg: "file not found"},
			expected: "- Read File nonexistent.txt\n ╰ Error: file not found\n",
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

// testError implements error interface for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
