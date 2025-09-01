package main

import (
	"context"
	"encoding/json"
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
