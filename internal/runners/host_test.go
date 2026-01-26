package runners

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostRunner(t *testing.T) {
	msgChan := make(chan Msg, 10)
	runner := NewHostRunner(msgChan)
	require.NotNil(t, runner)
	assert.Equal(t, "host", runner.RunnerType())
}

func TestHostRunnerRunWithBypassApproval(t *testing.T) {
	msgChan := make(chan Msg, 10)
	runner := NewHostRunner(msgChan)

	output, err := runner.Run(context.Background(), Input{
		Command:        "echo hello",
		BypassApproval: true,
	})

	require.NoError(t, err)
	assert.Contains(t, output.Output, "hello")
	assert.Equal(t, "0", output.ExitCode)
}

func TestHostRunnerRunExitCode(t *testing.T) {
	msgChan := make(chan Msg, 10)
	runner := NewHostRunner(msgChan)

	output, err := runner.Run(context.Background(), Input{
		Command:        "exit 42",
		BypassApproval: true,
	})

	require.NoError(t, err)
	assert.Equal(t, "42", output.ExitCode)
}

func TestHostRunnerRunWithStderr(t *testing.T) {
	msgChan := make(chan Msg, 10)
	runner := NewHostRunner(msgChan)

	output, err := runner.Run(context.Background(), Input{
		Command:        "echo 'stdout' && echo 'stderr' >&2",
		BypassApproval: true,
	})

	require.NoError(t, err)
	assert.Contains(t, output.Output, "stdout")
	assert.Contains(t, output.Output, "stderr")
	assert.Equal(t, "0", output.ExitCode)
}

func TestHostRunnerApprovalRequest(t *testing.T) {
	msgChan := make(chan Msg, 10)
	runner := NewHostRunner(msgChan)

	// Start a goroutine to handle the approval request
	go func() {
		select {
		case msg := <-msgChan:
			approvalReq, ok := msg.(ApprovalRequestMsg)
			require.True(t, ok, "Expected ApprovalRequestMsg, got %T", msg)
			assert.Equal(t, "echo hello", approvalReq.Command)
			approvalReq.ResponseChan <- true
		case <-time.After(5 * time.Second):
			t.Error("Timeout waiting for approval request")
		}
	}()

	output, err := runner.Run(context.Background(), Input{
		Command:        "echo hello",
		BypassApproval: false, // Requires approval
	})

	require.NoError(t, err)
	assert.Contains(t, output.Output, "hello")
	assert.Equal(t, "0", output.ExitCode)
}

func TestHostRunnerApprovalDenied(t *testing.T) {
	msgChan := make(chan Msg, 10)
	runner := NewHostRunner(msgChan)

	// Start a goroutine to deny the approval request
	go func() {
		select {
		case msg := <-msgChan:
			approvalReq, ok := msg.(ApprovalRequestMsg)
			require.True(t, ok, "Expected ApprovalRequestMsg, got %T", msg)
			approvalReq.ResponseChan <- false // Deny
		case <-time.After(5 * time.Second):
			t.Error("Timeout waiting for approval request")
		}
	}()

	output, err := runner.Run(context.Background(), Input{
		Command:        "echo hello",
		BypassApproval: false,
	})

	require.Error(t, err)
	_, ok := err.(CommandDeniedError)
	assert.True(t, ok, "Expected CommandDeniedError, got %T", err)
	assert.Equal(t, "1", output.ExitCode)
}

func TestHostRunnerNoMsgChannel(t *testing.T) {
	runner := NewHostRunner(nil)

	// Without a message channel and requiring approval, it should fail
	output, err := runner.Run(context.Background(), Input{
		Command:        "echo hello",
		BypassApproval: false,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no approval mechanism configured")
	assert.Equal(t, "1", output.ExitCode)
}

func TestHostRunnerRestart(t *testing.T) {
	msgChan := make(chan Msg, 10)
	runner := NewHostRunner(msgChan)

	// Restart should be a no-op for host runner
	err := runner.Restart(context.Background())
	assert.NoError(t, err)
}

func TestHostRunnerClose(t *testing.T) {
	msgChan := make(chan Msg, 10)
	runner := NewHostRunner(msgChan)

	// Close should be a no-op for host runner
	err := runner.Close(context.Background())
	assert.NoError(t, err)
}

func TestHostRunnerContextCancellation(t *testing.T) {
	msgChan := make(chan Msg, 10)
	runner := NewHostRunner(msgChan)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context immediately
	cancel()

	// Without bypass, it needs approval but context is cancelled
	_, err := runner.Run(ctx, Input{
		Command:        "echo hello",
		BypassApproval: false,
	})

	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}
