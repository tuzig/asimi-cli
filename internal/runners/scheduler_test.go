package runners

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

type mockTool struct {
	name        string
	description string
	callFunc    func(ctx context.Context, input string) (string, error)
}

func (t *mockTool) Name() string {
	return t.name
}

func (t *mockTool) Description() string {
	return t.description
}

func (t *mockTool) Call(ctx context.Context, input string) (string, error) {
	if t.callFunc != nil {
		return t.callFunc(ctx, input)
	}
	return "", nil
}

func (t *mockTool) Format(input, result string, err error) string {
	return t.name + ": " + result
}

func (t *mockTool) ParameterSchema() map[string]any {
	return nil
}

type mockModel struct {
	messages []tea.Msg
}

func (m *mockModel) Init() tea.Cmd {
	return nil
}

func (m *mockModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.messages = append(m.messages, msg)
	if _, ok := msg.(tea.QuitMsg); ok {
		return m, tea.Quit
	}
	return m, nil
}

func (m *mockModel) View() string {
	return ""
}

func TestCoreToolScheduler(t *testing.T) {
	var msgs []any
	scheduler := NewCoreToolScheduler(func(msg any) {
		msgs = append(msgs, msg)
	})

	tool := &mockTool{
		name:        "test-tool",
		description: "A tool for testing",
		callFunc: func(ctx context.Context, input string) (string, error) {
			time.Sleep(10 * time.Millisecond)
			return "output for " + input, nil
		},
	}

	resultChan := scheduler.Schedule(tool, "test-input")

	// Wait for the result
	result := <-resultChan

	// Assertions
	assert.NoError(t, result.Error)
	assert.Equal(t, "output for test-input", result.Output)

	// Check messages sent to the model
	assert.GreaterOrEqual(t, len(msgs), 3)
	_, ok := msgs[0].(ToolCallScheduledMsg)
	assert.True(t, ok)
	_, ok = msgs[1].(ToolCallExecutingMsg)
	assert.True(t, ok)
	_, ok = msgs[2].(ToolCallSuccessMsg)
	assert.True(t, ok)
}

func TestCoreToolScheduler_ClearQueue(t *testing.T) {
	var abortedCalls []ToolCallAbortedMsg

	scheduler := NewCoreToolScheduler(func(msg any) {
		if aborted, ok := msg.(ToolCallAbortedMsg); ok {
			abortedCalls = append(abortedCalls, aborted)
		}
	})

	// Create a slow tool that will block the scheduler
	slowTool := &mockTool{
		name:        "slow-tool",
		description: "A slow tool",
		callFunc: func(ctx context.Context, input string) (string, error) {
			time.Sleep(500 * time.Millisecond)
			return "done", nil
		},
	}

	fastTool := &mockTool{
		name:        "fast-tool",
		description: "A fast tool",
		callFunc: func(ctx context.Context, input string) (string, error) {
			return "fast", nil
		},
	}

	// Schedule the slow tool first (it will start executing)
	slowResult := scheduler.Schedule(slowTool, "slow-input")

	// Give it a moment to start executing
	time.Sleep(10 * time.Millisecond)

	// Schedule multiple fast tools (they will be queued)
	fastResult1 := scheduler.Schedule(fastTool, "fast-input-1")
	fastResult2 := scheduler.Schedule(fastTool, "fast-input-2")
	fastResult3 := scheduler.Schedule(fastTool, "fast-input-3")

	// Clear the queue - this should abort the queued tool calls but not the executing one
	abortedCount := scheduler.ClearQueue()

	// Verify that 3 calls were aborted (the queued ones)
	assert.Equal(t, 3, abortedCount, "should have aborted 3 queued tool calls")

	// Check that aborted calls received the SandboxRestartedError
	for _, result := range []<-chan ToolCallResult{fastResult1, fastResult2, fastResult3} {
		res := <-result
		assert.Error(t, res.Error)
		_, ok := res.Error.(SandboxRestartedError)
		assert.True(t, ok, "error should be SandboxRestartedError")
	}

	// Verify notifications were sent for each aborted call
	assert.Equal(t, 3, len(abortedCalls), "should have received 3 aborted notifications")
	for _, aborted := range abortedCalls {
		assert.Equal(t, "fast-tool", aborted.Call.Tool.Name())
		assert.Equal(t, StatusAborted, aborted.Call.Status)
	}

	// The slow tool should still complete normally
	slowRes := <-slowResult
	assert.NoError(t, slowRes.Error)
	assert.Equal(t, "done", slowRes.Output)
}

func TestCoreToolScheduler_ClearQueue_EmptyQueue(t *testing.T) {
	scheduler := NewCoreToolScheduler(nil)

	// Clear an empty queue - should return 0 and not panic
	abortedCount := scheduler.ClearQueue()
	assert.Equal(t, 0, abortedCount, "clearing empty queue should return 0")
}
