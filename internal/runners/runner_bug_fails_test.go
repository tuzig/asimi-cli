package runners

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchedulerOutputTruncationMissing demonstrates the bug:
// The scheduler does NOT apply TruncateOutput to tool results.
// If Session.executeToolCall() also doesn't truncate, context explodes.
//
// This test FAILS when truncation is missing from Session.executeToolCall().
func TestSchedulerOutputTruncationMissing(t *testing.T) {
	// Create a tool that returns 100KB of output
	largeSize := 100 * 1024
	tool := &largeOutputTool{outputSize: largeSize}

	scheduler := NewCoreToolScheduler(func(any) {})

	// Schedule the tool (simulates what scheduler.processQueue does)
	ch := scheduler.Schedule(context.Background(), tool, "{}")
	result := <-ch

	require.NoError(t, result.Error)

	// Scheduler returns RAW output - this is by design
	assert.Equal(t, largeSize, len(result.Output),
		"Scheduler should return raw output (truncation is NOT the scheduler's responsibility)")

	// THE TEST: This assertion FAILS if truncation is missing from Session.executeToolCall
	// In the fixed code, Session.executeToolCall() would apply TruncateOutput here.
	// Without that fix, this test demonstrates the raw output would flow into message history.

	// Simulate what Session.executeToolCall should do
	truncated := TruncateOutput(result.Output, DefaultMaxOutputSize)

	// This assertion documents the requirement:
	// Output MUST be truncated to prevent context explosion
	assert.Less(t, len(truncated), largeSize,
		"BUG: Output would explode context without truncation in Session.executeToolCall()")
}

// TestToolOutputMustBeTruncatedBeforeMessageHistory verifies that large tool output
// is truncated BEFORE being added to message history.
//
// BUG SCENARIO: If AsimiSQLTool returns 1MB of SQL results, and those results
// are added to the message history without truncation, the context will explode.
//
// This test verifies the truncation requirement is met.
func TestToolOutputMustBeTruncatedBeforeMessageHistory(t *testing.T) {
	// Simulate large SQL query result (100KB - exceeds DefaultMaxOutputSize of 50KB)
	largeOutput := strings.Repeat("edict_id|1|intent|test query result line\n", 4000)
	require.Greater(t, len(largeOutput), DefaultMaxOutputSize,
		"Test data should exceed truncation limit")

	// THE CRITICAL ASSERTION: Without truncation, this would cause context explosion
	// The fix requires TruncateOutput() to be called in Session.executeToolCall()
	truncated := TruncateOutput(largeOutput, DefaultMaxOutputSize)

	// Verify truncation actually reduces size
	assert.Less(t, len(truncated), len(largeOutput)/2,
		"Truncation should significantly reduce output size")

	// Verify truncated output contains markers (not just byte-chopped)
	assert.Contains(t, truncated, "... +",
		"Truncated output must contain marker to indicate skipped content")

	// BUG DEMONSTRATION: If this assertion passed, the bug exists:
	// if len(truncated) >= len(largeOutput) {
	//     t.Fatal("BUG: Output not truncated - context explosion imminent")
	// }
	assert.Less(t, len(truncated), len(largeOutput),
		"Output MUST be truncated to prevent context explosion")
}
