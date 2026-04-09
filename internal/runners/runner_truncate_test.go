package runners

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// largeOutputTool is a mock tool that returns a large amount of output
type largeOutputTool struct {
	outputSize int
}

func (t *largeOutputTool) Name() string        { return "large_output_tool" }
func (t *largeOutputTool) Description() string { return "A tool that returns large output" }

func (t *largeOutputTool) Call(ctx context.Context, input string) (string, error) {
	// Generate output larger than DefaultMaxOutputSize (50KB)
	return strings.Repeat("x", t.outputSize), nil
}

func (t *largeOutputTool) Format(input, result string, err error) string {
	return "large output truncated"
}

func (t *largeOutputTool) ParameterSchema() map[string]any {
	return nil
}

// TestTruncateOutput_Basic verifies basic truncation behavior
func TestTruncateOutput_Basic(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxBytes  int
		wantTrunc bool
	}{
		{
			name:      "under limit - no truncation",
			input:     "short output",
			maxBytes:  100,
			wantTrunc: false,
		},
		{
			name:      "at limit - no truncation",
			input:     "exactly 10",
			maxBytes:  10,
			wantTrunc: false,
		},
		{
			name:      "over limit - truncation",
			input:     strings.Repeat("x", 200),
			maxBytes:  100,
			wantTrunc: true,
		},
		{
			name:      "zero maxBytes - no truncation",
			input:     strings.Repeat("x", 200),
			maxBytes:  0,
			wantTrunc: false,
		},
		{
			name:      "negative maxBytes - no truncation",
			input:     strings.Repeat("x", 200),
			maxBytes:  -1,
			wantTrunc: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateOutput(tt.input, tt.maxBytes)

			if tt.wantTrunc {
				assert.Less(t, len(result), tt.maxBytes+50, "truncated output should be smaller than maxBytes + overhead")
			} else {
				assert.Equal(t, len(tt.input), len(result), "output length should match input when not truncated")
			}
		})
	}
}

// TestTruncateOutput_MultiLine verifies truncation preserves line structure
func TestTruncateOutput_MultiLine(t *testing.T) {
	// Create a multi-line input with 100 lines
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line content here")
	}
	input := strings.Join(lines, "\n")

	maxBytes := 500 // Small enough to trigger truncation

	result := TruncateOutput(input, maxBytes)

	// Should contain the marker
	assert.Contains(t, result, "... +", "truncated output should contain marker")
	assert.Contains(t, result, "lines ...", "truncated output should contain 'lines' marker")

	// Result should be smaller than input
	assert.Less(t, len(result), len(input), "truncated result should be smaller than input")

	// Result should be smaller than maxBytes
	assert.LessOrEqual(t, len(result), maxBytes+300, "truncated result should be close to maxBytes")
}

// TestTruncateOutput_HeadAndTailPreserved verifies head and tail lines are preserved
func TestTruncateOutput_HeadAndTailPreserved(t *testing.T) {
	// Create input with identifiable head and tail
	head := "HEAD_LINE_1\nHEAD_LINE_2\nHEAD_LINE_3"
	tail := "TAIL_LINE_1\nTAIL_LINE_2\nTAIL_LINE_3"
	padding := strings.Repeat("PADDING_LINE\n", 100)
	input := head + "\n" + padding + tail

	maxBytes := 300
	result := TruncateOutput(input, maxBytes)

	// Head lines should be preserved
	assert.Contains(t, result, "HEAD_LINE_1", "head should be preserved")
	assert.Contains(t, result, "HEAD_LINE_2", "head should be preserved")

	// Tail lines should be preserved
	assert.Contains(t, result, "TAIL_LINE_1", "tail should be preserved")
	assert.Contains(t, result, "TAIL_LINE_2", "tail should be preserved")
}

// TestTruncateOutput_LargeOutputTool verifies that large tool output is truncated
// when used through the CoreToolScheduler and Session.executeToolCall flow.
// This is the bug scenario: asimisql tool output that exceeds DefaultMaxOutputSize
// should be truncated to prevent context explosion.
func TestTruncateOutput_LargeOutputTool(t *testing.T) {
	// The output size to generate (100KB - well over DefaultMaxOutputSize of 50KB)
	largeSize := 100 * 1024

	tool := &largeOutputTool{outputSize: largeSize}
	scheduler := NewCoreToolScheduler(func(any) {})

	// Schedule the large output tool
	ch := scheduler.Schedule(context.Background(), tool, "{}")
	result := <-ch

	assert.NoError(t, result.Error)

	// The raw output from the tool is NOT truncated by the scheduler
	// (this is the bug - the scheduler returns the raw output)
	// The truncation should happen in Session.executeToolCall, not here
	rawOutputLen := len(result.Output)
	assert.Equal(t, largeSize, rawOutputLen, "scheduler should return raw untruncated output")

	// Now apply truncation as Session.executeToolCall does
	truncated := TruncateOutput(result.Output, DefaultMaxOutputSize)

	// Verify the output is now truncated
	assert.Less(t, len(truncated), DefaultMaxOutputSize+200,
		"output should be truncated to roughly DefaultMaxOutputSize")

	// Verify it contains the truncation marker
	assert.Contains(t, truncated, "... +", "truncated output should contain marker")
}

// TestTruncateOutput_ExceedsMaxOutputSize verifies that output exceeding
// DefaultMaxOutputSize (50KB) is properly truncated. This is critical
// for preventing context explosion from tools like asimisql that may
// return large query results.
func TestTruncateOutput_ExceedsMaxOutputSize(t *testing.T) {
	// Generate output that exceeds DefaultMaxOutputSize (50KB)
	excessiveOutput := strings.Repeat("column1|column2|column3|column4|column5\n", 2000) // ~140KB

	// Verify it exceeds the limit
	assert.Greater(t, len(excessiveOutput), DefaultMaxOutputSize,
		"test input should exceed DefaultMaxOutputSize")

	// Apply truncation
	result := TruncateOutput(excessiveOutput, DefaultMaxOutputSize)

	// The result should be significantly smaller
	assert.Less(t, len(result), DefaultMaxOutputSize,
		"truncated output must be less than maxBytes")

	// Should have truncation markers
	assert.Contains(t, result, "... +", "should show bytes/lines skipped")
}
