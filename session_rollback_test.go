package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// TestSession_GetMessageSnapshot tests the snapshot functionality
func TestSession_GetMessageSnapshot(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	// Initial snapshot should be 1 (system message)
	snapshot := sess.GetMessageSnapshot()
	assert.Equal(t, 1, snapshot)

	// After first message, the session will:
	// 1. Add user message
	// 2. Get AI response (no tools)
	// 3. Give model another turn (adds another AI message with same content)
	// Total: system + user + ai + ai = 4
	_, err = sess.Ask(context.Background(), "hello")
	assert.NoError(t, err)
	snapshot = sess.GetMessageSnapshot()
	assert.Equal(t, 4, snapshot, "Should have system + user + ai + ai (second turn)")

	// After second message, adds 3 more: user + ai + ai = 7 total
	_, err = sess.Ask(context.Background(), "world")
	assert.NoError(t, err)
	snapshot = sess.GetMessageSnapshot()
	assert.Equal(t, 7, snapshot, "Should have 7 messages total")
}

// TestSession_RollbackTo tests the rollback functionality
func TestSession_RollbackTo(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	// Add some messages
	_, err = sess.Ask(context.Background(), "first message")
	assert.NoError(t, err)
	snapshot1 := sess.GetMessageSnapshot()

	_, err = sess.Ask(context.Background(), "second message")
	assert.NoError(t, err)

	_, err = sess.Ask(context.Background(), "third message")
	assert.NoError(t, err)

	// Rollback to after first message
	sess.RollbackTo(snapshot1)
	assert.Equal(t, snapshot1, len(sess.messages))

	// Verify we can continue from rolled back state
	_, err = sess.Ask(context.Background(), "new second message")
	assert.NoError(t, err)
	// Should add user + ai + ai = 3 more messages
	assert.Equal(t, snapshot1+3, len(sess.messages))
}

// TestSession_RollbackToZero tests rollback with invalid snapshot
func TestSession_RollbackToZero(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	_, err = sess.Ask(context.Background(), "test message")
	assert.NoError(t, err)

	// Rollback to 0 should preserve system message
	sess.RollbackTo(0)
	assert.Equal(t, 1, len(sess.messages), "Should preserve system message")

	// Rollback to negative should preserve system message
	sess.RollbackTo(-5)
	assert.Equal(t, 1, len(sess.messages), "Should preserve system message")
}

// TestSession_RollbackBeyondLength tests rollback with snapshot beyond current length
func TestSession_RollbackBeyondLength(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	_, err = sess.Ask(context.Background(), "test message")
	assert.NoError(t, err)
	currentLen := len(sess.messages)

	// Rollback to beyond current length should not change anything
	sess.RollbackTo(currentLen + 10)
	assert.Equal(t, currentLen, len(sess.messages))
}

// TestSession_RollbackResetsToolLoopDetection tests that rollback resets tool loop state
func TestSession_RollbackResetsToolLoopDetection(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	// Simulate tool loop detection state
	sess.lastToolCallKey = "some_tool_call"
	sess.toolCallRepetitionCount = 5

	snapshot := sess.GetMessageSnapshot()

	// Add a message
	_, err = sess.Ask(context.Background(), "test")
	assert.NoError(t, err)

	// Rollback
	sess.RollbackTo(snapshot)

	// Tool loop state should be reset
	assert.Equal(t, "", sess.lastToolCallKey)
	assert.Equal(t, 0, sess.toolCallRepetitionCount)
}

// mockLLMToolMessages is a mock that returns tool messages for testing
type mockLLMToolMessages struct{ llms.Model }

func (m *mockLLMToolMessages) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}

func (m *mockLLMToolMessages) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	last := messages[len(messages)-1]
	switch last.Role {
	case llms.ChatMessageTypeHuman:
		// Return a tool call
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID:   "tc1",
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"testdata/test.txt"}`,
				},
			}},
		}}}, nil
	case llms.ChatMessageTypeTool:
		// Return final response
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
			Content: "Tool executed successfully",
		}}}, nil
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}, nil
}

// TestSession_RollbackWithToolCalls tests rollback with tool calls in history
func TestSession_RollbackWithToolCalls(t *testing.T) {
	t.Parallel()

	llm := &mockLLMToolMessages{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	snapshot1 := sess.GetMessageSnapshot()

	// Execute a message that triggers tool calls
	_, err = sess.Ask(context.Background(), "read a file")
	assert.NoError(t, err)

	// Should have: system + user + assistant(tool call) + tool response + assistant(final)
	assert.Greater(t, len(sess.messages), snapshot1)

	// Rollback to before the tool call
	sess.RollbackTo(snapshot1)
	assert.Equal(t, snapshot1, len(sess.messages))

	// Verify we can execute a different command
	_, err = sess.Ask(context.Background(), "different command")
	assert.NoError(t, err)
	assert.Greater(t, len(sess.messages), snapshot1)
}

// TestSession_MultipleToolMessagesPerCall tests the new one-message-per-tool-call structure
func TestSession_MultipleToolMessagesPerCall(t *testing.T) {
	t.Parallel()

	llm := &sessionMockLLMMultiTools{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	initialLen := len(sess.messages)

	// Execute a request that triggers multiple tool calls
	_, err = sess.Ask(context.Background(), "read two files")
	assert.NoError(t, err)

	// Count tool messages
	toolMessageCount := 0
	for i := initialLen; i < len(sess.messages); i++ {
		if sess.messages[i].Role == llms.ChatMessageTypeTool {
			toolMessageCount++
			// Each tool message should have exactly one part
			assert.Equal(t, 1, len(sess.messages[i].Parts),
				"Each tool message should have exactly one part")
		}
	}

	// Should have 2 tool messages (one per tool call)
	assert.Equal(t, 2, toolMessageCount,
		"Should have one message per tool call")
}

// TestSession_RollbackPreservesSystemPrompt tests that system prompt is always preserved
func TestSession_RollbackPreservesSystemPrompt(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	// Get the system message
	assert.Equal(t, 1, len(sess.messages))
	systemMsg := sess.messages[0]
	assert.Equal(t, llms.ChatMessageTypeSystem, systemMsg.Role)

	// Add some messages
	_, err = sess.Ask(context.Background(), "test1")
	assert.NoError(t, err)
	_, err = sess.Ask(context.Background(), "test2")
	assert.NoError(t, err)

	// Rollback to 0 (should preserve system message)
	sess.RollbackTo(0)
	assert.Equal(t, 1, len(sess.messages))
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.messages[0].Role)
	assert.Equal(t, systemMsg, sess.messages[0])

	// Rollback to 1 (should preserve system message)
	_, err = sess.Ask(context.Background(), "test3")
	assert.NoError(t, err)
	sess.RollbackTo(1)
	assert.Equal(t, 1, len(sess.messages))
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.messages[0].Role)
}
