package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunShellCommand(t *testing.T) {
	tool := RunShellCommand{}
	input := `{"command": "echo 'hello world'"}`

	result, err := tool.Call(context.Background(), input)
	assert.NoError(t, err)

	var output RunShellCommandOutput
	err = json.Unmarshal([]byte(result), &output)
	assert.NoError(t, err)

	assert.Equal(t, "hello world\n", output.Stdout)
	assert.Equal(t, "", output.Stderr)
	assert.Equal(t, 0, output.ExitCode)
}

func TestRunShellCommandError(t *testing.T) {
	tool := RunShellCommand{}
	input := `{"command": "exit 1"}`

	result, err := tool.Call(context.Background(), input)
	assert.NoError(t, err)

	var output RunShellCommandOutput
	err = json.Unmarshal([]byte(result), &output)
	assert.NoError(t, err)

	assert.Equal(t, 1, output.ExitCode)
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
