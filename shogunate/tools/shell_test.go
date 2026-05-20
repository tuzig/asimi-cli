package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunShellCommandUsesLocalRunner(t *testing.T) {
	// mockRunner is a per-shogunate runner that records calls
	type call struct {
		cmd string
	}
	var calls []call
	mockRunner := &mockRunner{
		runFn: func(ctx context.Context, input runners.Input) (runners.Output, error) {
			calls = append(calls, call{cmd: input.Command})
			return runners.Output{Output: "mock output", ExitCode: "0"}, nil
		},
	}

	tool := NewRunShellCommand(nil, mockRunner)
	require.NotNil(t, tool)

	result, err := tool.Call(context.Background(), `{"command":"echo hello","description":"test"}`)
	require.NoError(t, err)

	// The mock runner was called with the right command
	require.Len(t, calls, 1)
	assert.Equal(t, "echo hello", calls[0].cmd)

	// The output comes from the mock, not from a global runner
	var output runners.Output
	require.NoError(t, json.Unmarshal([]byte(result), &output))
	assert.Equal(t, "mock output", output.Output)
	assert.Equal(t, "0", output.ExitCode)
}

func TestRunShellCommandNoRunner(t *testing.T) {
	tool := NewRunShellCommand(nil, nil)

	_, err := tool.Call(context.Background(), `{"command":"echo hello","description":"test"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no runner configured")
}

func TestRunShellCommandHostOverride(t *testing.T) {
	// When shouldRunOnHost returns true, we use an ephemeral HostRunner
	// regardless of the per-shogunate runner
	var runnerCalls int
	mockRunner := &mockRunner{
		runFn: func(ctx context.Context, input runners.Input) (runners.Output, error) {
			runnerCalls++
			return runners.Output{Output: "should not be called", ExitCode: "0"}, nil
		},
	}

	// hostChecker returns (runOnHost=true, needsApproval=false)
	hostChecker := func(cmd string) (bool, bool) { return true, false }

	tool := NewRunShellCommand(hostChecker, mockRunner)
	result, err := tool.Call(context.Background(), `{"command":"echo hello","description":"test"}`)
	require.NoError(t, err)

	// The per-shogunate runner should NOT be called
	assert.Equal(t, 0, runnerCalls)

	// Output comes from the real host runner
	var output runners.Output
	require.NoError(t, json.Unmarshal([]byte(result), &output))
	assert.Contains(t, output.Output, "hello")
	assert.Equal(t, "0", output.ExitCode)
}

// mockRunner implements runners.Runner for testing
type mockRunner struct {
	runFn       func(ctx context.Context, input runners.Input) (runners.Output, error)
	restartFn   func(ctx context.Context) error
	closeFn     func(ctx context.Context) error
	runnerType  string
	allowFallbackCalled bool
	msgChan     chan<- runners.Msg
}

func (m *mockRunner) Run(ctx context.Context, input runners.Input) (runners.Output, error) {
	if m.runFn != nil {
		return m.runFn(ctx, input)
	}
	return runners.Output{}, nil
}

func (m *mockRunner) Restart(ctx context.Context) error {
	if m.restartFn != nil {
		return m.restartFn(ctx)
	}
	return nil
}

func (m *mockRunner) Close(ctx context.Context) error {
	if m.closeFn != nil {
		return m.closeFn(ctx)
	}
	return nil
}

func (m *mockRunner) AllowFallback(allow bool) {
	m.allowFallbackCalled = allow
}

func (m *mockRunner) RunnerType() string {
	if m.runnerType != "" {
		return m.runnerType
	}
	return "mock"
}

func (m *mockRunner) SetMessageChannel(msgChan chan<- runners.Msg) {
	m.msgChan = msgChan
}
