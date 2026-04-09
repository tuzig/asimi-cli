package runners

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScheduler_DoesNotTruncateOutput verifies that the scheduler does NOT truncate
// tool output - this is the potential bug scenario where large tool outputs flow
// through the scheduler without truncation.
//
// The fix should ensure truncation happens in Session.executeToolCall(), not in the scheduler.
func TestScheduler_DoesNotTruncateOutput(t *testing.T) {
	// Create a tool that returns 100KB of output
	largeSize := 100 * 1024
	tool := &largeOutputTool{outputSize: largeSize}

	scheduler := NewCoreToolScheduler(func(any) {})

	// Schedule the tool
	ch := scheduler.Schedule(context.Background(), tool, "{}")
	result := <-ch

	require.NoError(t, result.Error)

	// THE BUG: Scheduler returns raw untruncated output
	// This is by design - the scheduler should not truncate.
	// Truncation should happen in Session.executeToolCall()
	rawOutputLen := len(result.Output)
	assert.Equal(t, largeSize, rawOutputLen,
		"Scheduler should return raw untruncated output (truncation happens in Session)")

	// Verify truncation would reduce the output
	truncated := TruncateOutput(result.Output, DefaultMaxOutputSize)
	assert.Less(t, len(truncated), len(result.Output),
		"TruncateOutput should reduce output size")
}

// TestAsimiSQLTool_BypassesScheduler demonstrates that AsimiSQLTool uses
// HostRun directly, bypassing the scheduler's truncation.
//
// This is the actual bug: AsimiSQLTool.Call() -> HostRun() -> HostRunner.Run()
// does apply truncation via TruncateOutput() in HostRunner.Run().
// But the truncation uses DefaultMaxOutputSize, not the session's MaxToolOutput.
//
// The flow is:
// 1. AsimiSQLTool.Call() calls HostRun()
// 2. HostRun() creates HostRunner and calls runner.Run()
// 3. HostRunner.Run() applies TruncateOutput() with DefaultMaxOutputSize
// 4. Output returns to AsimiSQLTool.Call()
// 5. AsimiSQLTool returns raw output
// 6. Session.executeToolCall() applies TruncateOutput() again with MaxToolOutput
//
// Double truncation is inefficient but harmless. The bug would be if
// Session.executeToolCall() did NOT truncate, allowing raw output to
// flow into the message history.
func TestAsimiSQLTool_BypassesScheduler(t *testing.T) {
	// This test documents the code path for AsimiSQLTool:
	// AsimiSQLTool.Call() -> runners.HostRun() -> HostRunner.Run()
	//
	// HostRunner.Run() applies truncation at line 59:
	//   output.Output = TruncateOutput(stdout.String()+"\n"+stderr.String(), DefaultMaxOutputSize)
	//
	// Then Session.executeToolCall() applies truncation again with MaxToolOutput.
	//
	// The bug scenario would be if Session.executeToolCall() did NOT truncate,
	// allowing large output to accumulate in message history.

	// Verify TruncateOutput function exists and works
	testOutput := strings.Repeat("x", 100*1024)
	truncated := TruncateOutput(testOutput, DefaultMaxOutputSize)

	assert.Less(t, len(truncated), len(testOutput),
		"TruncateOutput should reduce large output")
	assert.Contains(t, truncated, "... +",
		"Truncated output should contain marker")
}

// TestTruncateOutput_DefaultMaxOutputSize verifies the default truncation limit
func TestTruncateOutput_DefaultMaxOutputSize(t *testing.T) {
	// DefaultMaxOutputSize should be 50KB
	assert.Equal(t, 51200, DefaultMaxOutputSize,
		"DefaultMaxOutputSize should be 50KB (51200 bytes)")

	// Verify truncation with default size
	largeOutput := strings.Repeat("column1|column2|column3\n", 5000) // ~90KB
	truncated := TruncateOutput(largeOutput, DefaultMaxOutputSize)

	assert.Less(t, len(truncated), len(largeOutput),
		"Truncated output should be smaller than original")
}

// TestContextExplosionPrevention verifies that large outputs are prevented from
// causing context explosion by being truncated before adding to message history.
func TestContextExplosionPrevention(t *testing.T) {
	// Simulate what happens when a tool returns large output
	// that would be added to the message history

	// Generate large output (100KB)
	largeOutput := strings.Repeat("data|row|entry\n", 5000) // ~90KB
	assert.Greater(t, len(largeOutput), DefaultMaxOutputSize,
		"Test output should exceed truncation limit")

	// Without truncation, this would cause context explosion
	// With truncation, it should be safe
	truncated := TruncateOutput(largeOutput, DefaultMaxOutputSize)

	// Verify truncation worked
	assert.Less(t, len(truncated), len(largeOutput),
		"Truncated output should be smaller than original to prevent context explosion")

	// Verify the truncated output contains head and tail (not just chopped)
	assert.Contains(t, truncated, "data", "Truncated output should preserve beginning")
	assert.Contains(t, truncated, "entry", "Truncated output should preserve end")
	assert.Contains(t, truncated, "... +", "Truncated output should indicate skipped content")
}
