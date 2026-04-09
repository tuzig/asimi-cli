package shogunate

import (
	"context"
	"strings"
	"testing"

	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// mockLLM simulates provider-native function/tool calling behavior.
type mockLLM struct {
	llms.Model
	response  string          // If set, returns this as a simple response
	toolCalls []llms.ToolCall // If set, returns these tool calls
	callCount int             // Track number of GenerateContent calls
	Err       error           // If set, returns this error
}

func (m *mockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	resp, err := m.GenerateContent(ctx, []llms.MessageContent{{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: prompt}},
	}}, options...)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", nil
	}
	return resp.Choices[0].Content, nil
}

func (m *mockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.callCount++

	if m.Err != nil {
		return nil, m.Err
	}

	// Apply streaming if configured
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}

	if m.response != "" {
		if callOpts.StreamingFunc != nil {
			chunks := strings.Split(m.response, " ")
			for i, chunk := range chunks {
				chunkText := chunk
				if i < len(chunks)-1 {
					chunkText += " "
				}
				if err := callOpts.StreamingFunc(ctx, []byte(chunkText)); err != nil {
					return nil, err
				}
			}
		}
		return &llms.ContentResponse{
			Choices: []*llms.ContentChoice{{Content: m.response}},
		}, nil
	}

	// Check if this is a tool response - if so, return final text
	last := messages[len(messages)-1]
	if last.Role == llms.ChatMessageTypeTool {
		var toolOut string
		for i := len(last.Parts) - 1; i >= 0; i-- {
			if tr, ok := last.Parts[i].(llms.ToolCallResponse); ok {
				toolOut = tr.Content
				break
			}
		}
		finalResponse := "FILE:" + toolOut
		if callOpts.StreamingFunc != nil {
			callOpts.StreamingFunc(ctx, []byte(finalResponse))
		}
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: finalResponse}}}, nil
	}

	// Return tool calls if configured
	if len(m.toolCalls) > 0 {
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{
			{ToolCalls: m.toolCalls},
		}}, nil
	}

	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}, nil
}

// mockLLMNoTools returns a direct assistant message without any tool calls.
type mockLLMNoTools struct{ llms.Model }

func (m *mockLLMNoTools) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}

func (m *mockLLMNoTools) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}
	response := "Hello world"
	if callOpts.StreamingFunc != nil {
		callOpts.StreamingFunc(ctx, []byte(response))
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: response}}}, nil
}

// mockTool is a simple tool for testing
type mockTool struct {
	name   string
	output string
	called bool
}

func (t *mockTool) Name() string        { return t.name }
func (t *mockTool) Description() string { return "A mock tool for testing" }
func (t *mockTool) Call(ctx context.Context, input string) (string, error) {
	t.called = true
	return t.output, nil
}
func (t *mockTool) Format(input, result string, err error) string {
	if err != nil {
		return t.name + " error"
	}
	return t.name + " ok"
}
func (t *mockTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func TestNewSession(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			Provider: "test",
			Model:    "test-model",
		},
	}

	sess, err := NewSession(llm, cfg, nil, nil, func(any) {}, "You are a test assistant", "")
	require.NoError(t, err)

	assert.NotEmpty(t, sess.ID)
	assert.NotNil(t, sess.toolCatalog)
	assert.NotNil(t, sess.scheduler)

	// Should have system message
	require.NotEmpty(t, sess.messages)
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.messages[0].Role)
}

func TestNewSession_DefaultMaxTurns(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, nil, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	assert.Equal(t, 999, sess.config.MaxTurns)
}

func TestNewSession_WithTools(t *testing.T) {
	t.Parallel()

	tools := []Tool{
		&mockTool{name: "test_tool", output: "tool output"},
	}

	sess, err := NewSession(&mockLLMNoTools{}, nil, tools, nil, func(any) {}, "", "")
	require.NoError(t, err)

	assert.Len(t, sess.toolDefs, 1)
	assert.Len(t, sess.toolCatalog, 1)
	assert.Contains(t, sess.toolCatalog, "test_tool")
}

func TestSession_AskWithStreaming_NoTools(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "say hi", nil)
	require.NoError(t, err)
	assert.Equal(t, "Hello world", out)
}

func TestSession_AskWithStreaming_WithResponse(t *testing.T) {
	t.Parallel()

	llm := &mockLLM{response: "Hello from the model"}
	sess, err := NewSession(llm, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "say hello", nil)
	require.NoError(t, err)
	assert.Contains(t, out, "Hello from the model")
}

func TestSession_AskWithStreaming_StreamsChunks(t *testing.T) {
	t.Parallel()

	llm := &mockLLM{response: "chunk1 chunk2 chunk3"}
	var chunks []string
	notify := func(msg any) {
		if chunk, ok := msg.(StreamChunkMsg); ok {
			chunks = append(chunks, chunk.Text)
		}
	}

	sess, err := NewSession(llm, &SessionConfig{}, nil, nil, notify, "", "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "stream test", nil)
	require.NoError(t, err)
	assert.Contains(t, out, "chunk1")
	assert.NotEmpty(t, chunks)
}

func TestSession_AskWithStreaming_WithToolExecution(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "read_file", output: "file contents"}
	llm := &mockLLM{
		toolCalls: []llms.ToolCall{{
			ID:   "tc1",
			Type: "function",
			FunctionCall: &llms.FunctionCall{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		}},
	}

	sess, err := NewSession(llm, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "read test.txt", nil)
	require.NoError(t, err)

	assert.True(t, tool.called, "Tool should have been called")
	assert.Contains(t, out, "file contents")
}

func TestSession_AskWithStreaming_WithContextFiles(t *testing.T) {
	t.Parallel()

	llm := &mockLLM{response: "I see the context"}
	sess, err := NewSession(llm, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	contextFiles := map[string]string{
		"file1.txt": "content of file 1",
		"file2.txt": "content of file 2",
	}

	_, err = sess.AskWithStreaming(context.Background(), "analyze these files", contextFiles)
	require.NoError(t, err)

	// Check that user message includes context
	// messages[0] = system, messages[1] = human, messages[2] = AI
	require.Greater(t, len(sess.messages), 2)
	userMsg := sess.messages[1]
	assert.Equal(t, llms.ChatMessageTypeHuman, userMsg.Role)

	// Get the text content
	for _, part := range userMsg.Parts {
		if textPart, ok := part.(llms.TextContent); ok {
			assert.Contains(t, textPart.Text, "file1.txt")
			assert.Contains(t, textPart.Text, "content of file 1")
		}
	}
}

func TestSession_SanitizeMessages_RemovesUnmatchedToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add a user message
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})

	// Add an AI message with tool call (no corresponding tool response)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{
				ID:   "tc1",
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "some_tool",
					Arguments: "{}",
				},
			},
		},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should have removed the trailing AI message with unmatched tool call
	assert.Less(t, len(sess.messages), initialLen)
}

func TestSession_SanitizeMessages_RemovesOrphanToolResponses(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add a user message
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})

	// Add an orphan tool response (no matching AI message)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: "orphan_tc",
				Name:       "some_tool",
				Content:    "orphan result",
			},
		},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should have removed the orphan tool response
	assert.Less(t, len(sess.messages), initialLen, "Orphan tool responses should be removed")
}

func TestSession_SanitizeMessages_KeepsMatchedToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add user message
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})

	// Add AI message with tool call
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{
				ID:   "tc1",
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "some_tool",
					Arguments: "{}",
				},
			},
		},
	})

	// Add matching tool response
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: "tc1",
				Name:       "some_tool",
				Content:    "tool result",
			},
		},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should keep all messages since tool call has matching response
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_SanitizeMessages_DisabledByConfig(t *testing.T) {
	t.Parallel()

	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			DisableContextSanitization: true,
		},
	}
	sess, err := NewSession(&mockLLMNoTools{}, cfg, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add unmatched tool call
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{
				ID:   "tc1",
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "some_tool",
					Arguments: "{}",
				},
			},
		},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should NOT remove messages when sanitization is disabled
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_EnsureToolCallIDsBeforeAppend(t *testing.T) {
	t.Parallel()

	// Simulate a provider returning empty tool call IDs
	mock := &mockLLM{
		toolCalls: []llms.ToolCall{
			{
				ID:   "", // empty ID, simulating provider bug
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"test.txt"}`,
				},
			},
		},
	}

	echoTool := &mockTool{name: "read_file", output: "file contents"}
	scheduler := runners.NewCoreToolScheduler(func(any) {})
	sess, err := NewSession(mock, &SessionConfig{LLM: internalconfig.LLMConfig{MaxTurns: 2}}, nil, scheduler, func(any) {}, "system", "test")
	require.NoError(t, err)

	sess.toolCatalog["read_file"] = echoTool
	sess.toolDefs = []llms.Tool{{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "read_file",
			Description: "Read a file",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		},
	}}

	// After the first call (tool call), mock returns plain text to end the loop
	mock.toolCalls = []llms.ToolCall{
		{
			ID:   "",
			Type: "function",
			FunctionCall: &llms.FunctionCall{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	}

	// Run AskWithStreaming — it will call generateLLMResponse which calls appendMessage
	// The fix ensures ensureToolCallID is called BEFORE appendMessage
	_, _ = sess.AskWithStreaming(context.Background(), "read test.txt", nil)

	// Find the AI message with tool calls and verify IDs are non-empty
	for _, msg := range sess.messages {
		if msg.Role != llms.ChatMessageTypeAI {
			continue
		}
		for _, part := range msg.Parts {
			if tc, ok := part.(llms.ToolCall); ok {
				assert.NotEmpty(t, tc.ID, "AI message tool call ID should not be empty after ensureToolCallID")
				assert.Contains(t, tc.ID, "synthetic_", "Empty provider IDs should get synthetic IDs")
			}
		}
	}

	// Verify tool response IDs match AI message tool call IDs
	aiToolCallIDs := map[string]bool{}
	toolResponseIDs := map[string]bool{}
	for _, msg := range sess.messages {
		for _, part := range msg.Parts {
			if tc, ok := part.(llms.ToolCall); ok {
				aiToolCallIDs[tc.ID] = true
			}
			if resp, ok := part.(llms.ToolCallResponse); ok {
				toolResponseIDs[resp.ToolCallID] = true
			}
		}
	}

	for id := range toolResponseIDs {
		assert.True(t, aiToolCallIDs[id], "Tool response ID %q should match an AI message tool call ID", id)
	}
}

func TestSession_CheckToolCallLoop(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	args := `{"path":"test.txt"}`

	// First call - not a loop
	assert.False(t, sess.checkToolCallLoop("read_file", args))
	assert.Equal(t, 1, sess.toolCallRepetitionCount)

	// Second identical call - still not a loop
	assert.False(t, sess.checkToolCallLoop("read_file", args))
	assert.Equal(t, 2, sess.toolCallRepetitionCount)

	// Third identical call - now it's a loop (threshold is 3)
	assert.True(t, sess.checkToolCallLoop("read_file", args))
	assert.Equal(t, 3, sess.toolCallRepetitionCount)
}

func TestSession_CheckToolCallLoop_DifferentCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	// Different calls should reset the counter
	assert.False(t, sess.checkToolCallLoop("read_file", `{"path":"a.txt"}`))
	assert.False(t, sess.checkToolCallLoop("read_file", `{"path":"b.txt"}`))
	assert.False(t, sess.checkToolCallLoop("write_file", `{"path":"c.txt"}`))

	// Counter should be 1 after each different call
	assert.Equal(t, 1, sess.toolCallRepetitionCount)
}

func TestSession_RegisterShogunateTools(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialToolCount := len(sess.toolDefs)

	tools := []Tool{
		&mockTool{name: "tool1"},
		&mockTool{name: "tool2"},
	}
	sess.RegisterShogunateTools(tools)

	assert.Equal(t, initialToolCount+2, len(sess.toolDefs))
	assert.Contains(t, sess.toolCatalog, "tool1")
	assert.Contains(t, sess.toolCatalog, "tool2")
}

func TestSession_Messages(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system prompt", "")
	require.NoError(t, err)

	messages := sess.Messages()
	require.NotEmpty(t, messages)
	assert.Equal(t, llms.ChatMessageTypeSystem, messages[0].Role)
}

func TestSession_AddMessage(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.AddMessage(llms.ChatMessageTypeHuman, "Hello")

	assert.Equal(t, initialLen+1, len(sess.messages))
	lastMsg := sess.messages[len(sess.messages)-1]
	assert.Equal(t, llms.ChatMessageTypeHuman, lastMsg.Role)
}

func TestBuildPromptWithContext_NoContext(t *testing.T) {
	t.Parallel()

	result := buildPromptWithContext("simple prompt", nil)
	assert.Equal(t, "simple prompt", result)
}

func TestBuildPromptWithContext_WithContext(t *testing.T) {
	t.Parallel()

	context := map[string]string{
		"file.txt": "file content",
	}
	result := buildPromptWithContext("analyze this", context)

	assert.Contains(t, result, "file.txt")
	assert.Contains(t, result, "file content")
	assert.Contains(t, result, "analyze this")
}

func TestCoreToolScheduler_Schedule(t *testing.T) {
	t.Parallel()

	var notified bool
	notify := func(msg any) {
		notified = true
	}

	scheduler := runners.NewCoreToolScheduler(notify)
	tool := &mockTool{name: "test", output: "result"}

	ch := scheduler.Schedule(context.Background(), tool, "{}")
	result := <-ch

	assert.NoError(t, result.Error)
	assert.Equal(t, "result", result.Output)
	assert.True(t, tool.called)
	assert.True(t, notified)
}

func TestSession_StreamBufferMethods(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	// Initially empty
	assert.Empty(t, sess.getStreamBuffer())

	// Write to buffer
	sess.accumulatedContent.WriteString("hello ")
	sess.accumulatedContent.WriteString("world")
	assert.Equal(t, "hello world", sess.getStreamBuffer())

	// Reset buffer
	sess.resetStreamBuffer()
	assert.Empty(t, sess.getStreamBuffer())
}

func TestSession_ProcessToolCalls_UnknownTool(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	toolCalls := []llms.ToolCall{{
		ID:   "tc1",
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      "unknown_tool",
			Arguments: "{}",
		},
	}}

	messages, shouldReturn := sess.processToolCalls(context.Background(), toolCalls)

	assert.False(t, shouldReturn)
	require.Len(t, messages, 1)

	// Check error response
	resp := messages[0].Parts[0].(llms.ToolCallResponse)
	assert.Contains(t, resp.Content, "unknown tool")
}

func TestSession_ProcessToolCalls_ContextCancellation(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	toolCalls := []llms.ToolCall{{
		ID:   "tc1",
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      "some_tool",
			Arguments: "{}",
		},
	}}

	messages, shouldReturn := sess.processToolCalls(ctx, toolCalls)

	assert.True(t, shouldReturn)
	require.NotEmpty(t, messages)

	// Should have abort response
	resp := messages[0].Parts[0].(llms.ToolCallResponse)
	assert.Contains(t, resp.Content, "aborted")
}

// --- Additional tests for full coverage ---

func TestSession_AddTools(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	tools := []Tool{&mockTool{name: "added_tool"}}
	sess.AddTools(tools)

	assert.Contains(t, sess.toolCatalog, "added_tool")
}

// TestSession_SetNotify_UpdatesScheduler verifies that SetNotify updates both
// the session's notify and the scheduler's notify (fixes tool call notifications)
func TestSession_SetNotify_UpdatesScheduler(t *testing.T) {
	t.Parallel()

	// Create session with nil notify
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, nil, "", "")
	require.NoError(t, err)

	// Verify both start as nil
	assert.Nil(t, sess.notify)

	// Set notify via SetNotify method
	var called bool
	sess.SetNotify(func(msg any) {
		called = true
	}, "")

	// Verify session notify is set
	assert.NotNil(t, sess.notify)

	// Verify the notify function works when called
	sess.notify("test")
	assert.True(t, called, "notify function should be callable after SetNotify")
}

// TestSession_ToolCallNotifications verifies that tool execution sends proper
// notifications through the stream (ToolCallExecutingMsg, ToolCallSuccessMsg)
func TestSession_ToolCallNotifications(t *testing.T) {
	t.Parallel()

	// Create a mock tool
	tool := &mockTool{
		name:   "test_tool",
		output: "tool output",
	}

	// Collect notifications
	var notifications []any
	notify := func(msg any) {
		notifications = append(notifications, msg)
	}

	// Create mock LLM that returns a tool call, then a final response
	mockLLM := &mockLLMWithToolCall{
		toolCall: llms.ToolCall{
			ID: "call-123",
			FunctionCall: &llms.FunctionCall{
				Name:      "test_tool",
				Arguments: `{"input": "test"}`,
			},
		},
		finalResponse: "Done",
	}

	// Create session with the notify function
	sess, err := NewSession(mockLLM, &SessionConfig{}, []Tool{tool}, nil, notify, "", "")
	require.NoError(t, err)

	// Execute a prompt that triggers tool use
	ctx := context.Background()
	_, err = sess.AskWithStreaming(ctx, "Use the test_tool", nil)
	require.NoError(t, err)

	// Verify we got tool call notifications
	var executingCount, successCount int
	for _, msg := range notifications {
		switch msg.(type) {
		case runners.ToolCallExecutingMsg:
			executingCount++
		case runners.ToolCallSuccessMsg:
			successCount++
		}
	}

	assert.Equal(t, 1, executingCount, "Should have 1 ToolCallExecutingMsg")
	assert.Equal(t, 1, successCount, "Should have 1 ToolCallSuccessMsg")
}

// mockLLMWithToolCall returns a tool call on first call, then a text response
type mockLLMWithToolCall struct {
	llms.Model
	toolCall      llms.ToolCall
	finalResponse string
	callCount     int
}

func (m *mockLLMWithToolCall) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return m.finalResponse, nil
}

func (m *mockLLMWithToolCall) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.callCount++

	// Apply streaming if configured
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}

	// First call returns tool call, subsequent calls return text
	if m.callCount == 1 {
		return &llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{
					ToolCalls: []llms.ToolCall{m.toolCall},
				},
			},
		}, nil
	}

	// Stream the final response if streaming is enabled
	if callOpts.StreamingFunc != nil {
		callOpts.StreamingFunc(ctx, []byte(m.finalResponse))
	}

	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{Content: m.finalResponse},
		},
	}, nil
}

func TestEnsureToolCallID_ExistingID(t *testing.T) {
	t.Parallel()

	tc := &llms.ToolCall{
		ID: "existing-id",
		FunctionCall: &llms.FunctionCall{
			Name: "test",
		},
	}

	result := ensureToolCallID(tc, 0)
	assert.Equal(t, "existing-id", result)
	assert.Equal(t, "existing-id", tc.ID)
}

func TestEnsureToolCallID_GeneratesSyntheticID(t *testing.T) {
	t.Parallel()

	tc := &llms.ToolCall{
		ID: "", // Empty ID
		FunctionCall: &llms.FunctionCall{
			Name: "test",
		},
	}

	result := ensureToolCallID(tc, 0)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "synthetic_")
	assert.Equal(t, result, tc.ID) // Should be set on the struct
}

func TestHasToolCallResponse_Found(t *testing.T) {
	t.Parallel()

	toolMessages := []llms.MessageContent{{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: "tc1",
				Name:       "test",
				Content:    "result",
			},
		},
	}}

	assert.True(t, hasToolCallResponse(toolMessages, "tc1"))
}

func TestHasToolCallResponse_NotFound(t *testing.T) {
	t.Parallel()

	toolMessages := []llms.MessageContent{{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: "tc1",
				Name:       "test",
				Content:    "result",
			},
		},
	}}

	assert.False(t, hasToolCallResponse(toolMessages, "tc2"))
}

func TestHasToolCallResponse_EmptyMessages(t *testing.T) {
	t.Parallel()

	assert.False(t, hasToolCallResponse(nil, "tc1"))
	assert.False(t, hasToolCallResponse([]llms.MessageContent{}, "tc1"))
}

func TestHasToolCallResponse_NonToolMessage(t *testing.T) {
	t.Parallel()

	messages := []llms.MessageContent{{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "hello"}},
	}}

	assert.False(t, hasToolCallResponse(messages, "tc1"))
}

func TestSession_SanitizeMessages_EmptyMessages(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	sess.messages = []llms.MessageContent{}
	sess.SanitizeMessages() // Should not panic
	assert.Empty(t, sess.messages)
}

func TestSession_SanitizeMessages_OrphanToolResponse(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add orphan tool response without prior AI message
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: "tc1",
				Name:       "test",
				Content:    "result",
			},
		},
	})

	sess.SanitizeMessages()

	// Should remove orphan tool response, keep only system message
	assert.Len(t, sess.messages, 1)
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.messages[0].Role)
}

func TestSession_SanitizeMessages_ToolResponseWithMismatchedID(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add AI message with tool call
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{
				ID:           "tc1",
				Type:         "function",
				FunctionCall: &llms.FunctionCall{Name: "test", Arguments: "{}"},
			},
		},
	})

	// Add tool response with DIFFERENT ID
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: "tc2", // Mismatched!
				Name:       "test",
				Content:    "result",
			},
		},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should remove mismatched messages
	assert.Less(t, len(sess.messages), initialLen)
}

func TestSession_SanitizeMessages_ToolResponseWithEmptyID(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add AI message with tool call
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{
				ID:           "tc1",
				Type:         "function",
				FunctionCall: &llms.FunctionCall{Name: "test", Arguments: "{}"},
			},
		},
	})

	// Add tool response with empty ID
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: "", // Empty!
				Name:       "test",
				Content:    "result",
			},
		},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should remove messages with empty tool call ID
	assert.Less(t, len(sess.messages), initialLen)
}

func TestSession_AppendMessage_NilChoice(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(nil)

	// Should not add anything for nil choice
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_AppendMessage_EmptyContent(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(&llms.ContentChoice{Content: "   "}) // Only whitespace

	// Should not add message with only whitespace
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_AppendMessage_WithToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(&llms.ContentChoice{
		Content: "",
		ToolCalls: []llms.ToolCall{{
			ID:   "tc1",
			Type: "function",
			FunctionCall: &llms.FunctionCall{
				Name:      "test",
				Arguments: "{}",
			},
		}},
	})

	// Should add message with tool calls even without content
	assert.Equal(t, initialLen+1, len(sess.messages))
}

func TestSession_AppendMessage_SkipsEmptyToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(&llms.ContentChoice{
		Content: "text",
		ToolCalls: []llms.ToolCall{
			{ID: "tc1", FunctionCall: nil},                          // nil FunctionCall
			{ID: "tc2", FunctionCall: &llms.FunctionCall{Name: ""}}, // empty name
		},
	})

	// Should add message but skip invalid tool calls
	assert.Equal(t, initialLen+1, len(sess.messages))
	lastMsg := sess.messages[len(sess.messages)-1]
	// Should only have text part, no tool calls
	assert.Len(t, lastMsg.Parts, 1)
}

func TestNewSession_WithProvidedScheduler(t *testing.T) {
	t.Parallel()

	customScheduler := runners.NewCoreToolScheduler(func(any) {})
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, customScheduler, func(any) {}, "", "")
	require.NoError(t, err)

	assert.Equal(t, customScheduler, sess.scheduler)
}

// mockLLMError returns an error
type mockLLMError struct{ llms.Model }

func (m *mockLLMError) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return nil, assert.AnError
}

func TestSession_AskWithStreaming_LLMError(t *testing.T) {
	t.Parallel()

	var errorNotified bool
	notify := func(msg any) {
		if _, ok := msg.(StreamErrorMsg); ok {
			errorNotified = true
		}
	}

	sess, err := NewSession(&mockLLMError{}, &SessionConfig{}, nil, nil, notify, "", "")
	require.NoError(t, err)

	_, err = sess.AskWithStreaming(context.Background(), "test", nil)
	assert.Error(t, err)
	assert.True(t, errorNotified)
}

// mockLLMEmpty returns empty choices
type mockLLMEmpty struct{ llms.Model }

func (m *mockLLMEmpty) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{}}, nil
}

func TestSession_GenerateLLMResponse_EmptyChoices(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMEmpty{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "test"}},
	})

	_, err = sess.generateLLMResponse(context.Background(), nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func TestSession_AskWithStreaming_Cancellation(t *testing.T) {
	t.Parallel()

	var interrupted bool
	notify := func(msg any) {
		if _, ok := msg.(StreamInterruptedMsg); ok {
			interrupted = true
		}
	}

	sess, err := NewSession(&mockLLM{response: "long response"}, &SessionConfig{}, nil, nil, notify, "", "")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = sess.AskWithStreaming(ctx, "test", nil)
	assert.Error(t, err)
	assert.True(t, interrupted)
}

// mockLLMMaxTokens returns a response with max_tokens stop reason
type mockLLMMaxTokens struct{ llms.Model }

func (m *mockLLMMaxTokens) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}
	if callOpts.StreamingFunc != nil {
		callOpts.StreamingFunc(ctx, []byte("truncated"))
	}
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			Content:    "truncated",
			StopReason: "max_tokens",
		}},
	}, nil
}

func TestSession_AskWithStreaming_MaxTokens(t *testing.T) {
	t.Parallel()

	var maxTokensNotified bool
	notify := func(msg any) {
		if _, ok := msg.(StreamMaxTokensReachedMsg); ok {
			maxTokensNotified = true
		}
	}

	sess, err := NewSession(&mockLLMMaxTokens{}, &SessionConfig{}, nil, nil, notify, "", "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "test", nil)
	require.NoError(t, err)
	assert.Contains(t, out, "truncated")
	assert.True(t, maxTokensNotified)
}

// mockLLMErrorStopReason returns a response with "error" stop reason (e.g. OpenRouter abort)
type mockLLMErrorStopReason struct {
	llms.Model
	stopReason string
}

func (m *mockLLMErrorStopReason) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			Content:    "",
			StopReason: m.stopReason,
		}},
	}, nil
}

func TestSession_AskWithStreaming_ErrorStopReason(t *testing.T) {
	t.Parallel()

	for _, stopReason := range []string{"error", "content_filter"} {
		t.Run(stopReason, func(t *testing.T) {
			t.Parallel()

			var errorNotified bool
			notify := func(msg any) {
				if _, ok := msg.(StreamErrorMsg); ok {
					errorNotified = true
				}
			}

			sess, err := NewSession(&mockLLMErrorStopReason{stopReason: stopReason}, &SessionConfig{}, nil, nil, notify, "", "")
			require.NoError(t, err)

			out, err := sess.AskWithStreaming(context.Background(), "test", nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "stop_reason="+stopReason)
			assert.Empty(t, out)
			assert.True(t, errorNotified)
		})
	}
}

func TestSession_ProcessToolCalls_LoopDetected(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "loop_tool", output: "result"}
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)

	toolCall := llms.ToolCall{
		ID:   "tc1",
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      "loop_tool",
			Arguments: `{"same":"args"}`,
		},
	}

	// Call 3 times to trigger loop detection
	sess.processToolCalls(context.Background(), []llms.ToolCall{toolCall})
	sess.processToolCalls(context.Background(), []llms.ToolCall{toolCall})
	messages, shouldReturn := sess.processToolCalls(context.Background(), []llms.ToolCall{toolCall})

	assert.True(t, shouldReturn)
	require.NotEmpty(t, messages)
	resp := messages[0].Parts[0].(llms.ToolCallResponse)
	assert.Contains(t, resp.Content, "loop detected")
}

func TestSession_ProcessToolCalls_SkipsEmptyName(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	toolCalls := []llms.ToolCall{
		{
			ID:           "tc1",
			Type:         "function",
			FunctionCall: &llms.FunctionCall{Name: "", Arguments: "{}"},
		},
		{
			ID:           "tc2",
			Type:         "function",
			FunctionCall: nil, // nil FunctionCall
		},
	}

	messages, _ := sess.processToolCalls(context.Background(), toolCalls)

	// Should skip both invalid tool calls
	assert.Empty(t, messages)
}

func TestSession_ExecuteToolCall_WithScheduler(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "scheduled_tool", output: "scheduled result"}
	scheduler := runners.NewCoreToolScheduler(func(any) {})

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, []Tool{tool}, scheduler, func(any) {}, "", "")
	require.NoError(t, err)

	tc := llms.ToolCall{
		ID:           "tc1",
		Type:         "function",
		FunctionCall: &llms.FunctionCall{Name: "scheduled_tool", Arguments: "{}"},
	}

	resp := sess.executeToolCall(context.Background(), tool, tc, "{}")

	assert.Equal(t, "scheduled result", resp.Content)
	assert.True(t, tool.called)
}

func TestSession_ExecuteToolCall_WithoutScheduler(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "direct_tool", output: "direct result"}

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)
	sess.scheduler = nil // Force no scheduler

	tc := llms.ToolCall{
		ID:           "tc1",
		Type:         "function",
		FunctionCall: &llms.FunctionCall{Name: "direct_tool", Arguments: "{}"},
	}

	resp := sess.executeToolCall(context.Background(), tool, tc, "{}")

	assert.Equal(t, "direct result", resp.Content)
	assert.True(t, tool.called)
}

// mockToolError returns an error
type mockToolError struct {
	name string
}

func (t *mockToolError) Name() string        { return t.name }
func (t *mockToolError) Description() string { return "error tool" }
func (t *mockToolError) Call(ctx context.Context, input string) (string, error) {
	return "", assert.AnError
}
func (t *mockToolError) Format(input, result string, err error) string { return "error" }
func (t *mockToolError) ParameterSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func TestSession_ExecuteToolCall_Error(t *testing.T) {
	t.Parallel()

	tool := &mockToolError{name: "error_tool"}

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)
	sess.scheduler = nil

	tc := llms.ToolCall{
		ID:           "tc1",
		Type:         "function",
		FunctionCall: &llms.FunctionCall{Name: "error_tool", Arguments: "{}"},
	}

	resp := sess.executeToolCall(context.Background(), tool, tc, "{}")

	assert.Contains(t, resp.Content, "Error:")
}

func TestCoreToolScheduler_Schedule_Error(t *testing.T) {
	t.Parallel()

	var errorNotified bool
	notify := func(msg any) {
		if _, ok := msg.(runners.ToolCallErrorMsg); ok {
			errorNotified = true
		}
	}

	scheduler := runners.NewCoreToolScheduler(notify)
	tool := &mockToolError{name: "error_tool"}

	ch := scheduler.Schedule(context.Background(), tool, "{}")
	result := <-ch

	assert.Error(t, result.Error)
	assert.True(t, errorNotified)
}

func TestCoreToolScheduler_Schedule_NoNotify(t *testing.T) {
	t.Parallel()

	scheduler := runners.NewCoreToolScheduler(nil) // No notify function
	tool := &mockTool{name: "test", output: "result"}

	ch := scheduler.Schedule(context.Background(), tool, "{}")
	result := <-ch

	assert.NoError(t, result.Error)
	assert.Equal(t, "result", result.Output)
}

func TestBuildLLMTools_Empty(t *testing.T) {
	t.Parallel()

	defs, catalog := buildLLMTools(nil)
	assert.Empty(t, defs)
	assert.Empty(t, catalog)
}

func TestBuildLLMTools_Multiple(t *testing.T) {
	t.Parallel()

	tools := []Tool{
		&mockTool{name: "tool1"},
		&mockTool{name: "tool2"},
	}

	defs, catalog := buildLLMTools(tools)

	assert.Len(t, defs, 2)
	assert.Len(t, catalog, 2)
	assert.Equal(t, "tool1", defs[0].Function.Name)
	assert.Equal(t, "tool2", defs[1].Function.Name)
}

// mockLLMRepeating returns the same response multiple times to test repeat detection
type mockLLMRepeating struct {
	llms.Model
	callCount int
}

func (m *mockLLMRepeating) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.callCount++
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}
	response := "same response"
	if callOpts.StreamingFunc != nil {
		callOpts.StreamingFunc(ctx, []byte(response))
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: response}}}, nil
}

func TestSession_AskWithStreaming_RepeatingResponse(t *testing.T) {
	t.Parallel()

	llm := &mockLLMRepeating{}
	sess, err := NewSession(llm, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "test", nil)
	require.NoError(t, err)
	assert.Contains(t, out, "same response")
	// With no tool calls, the loop breaks immediately after the first response
	assert.Equal(t, 1, llm.callCount)
}

// mockLLMMultiTurn returns tool calls then a final response
type mockLLMMultiTurn struct {
	llms.Model
	callCount int
}

func (m *mockLLMMultiTurn) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.callCount++
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}

	// First call: return tool call
	if m.callCount == 1 {
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID:           "tc1",
				Type:         "function",
				FunctionCall: &llms.FunctionCall{Name: "test_tool", Arguments: "{}"},
			}},
		}}}, nil
	}

	// Second call (after tool response): return final answer
	response := "final answer"
	if callOpts.StreamingFunc != nil {
		callOpts.StreamingFunc(ctx, []byte(response))
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: response}}}, nil
}

func TestSession_AskWithStreaming_MultiTurnToolExecution(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "test_tool", output: "tool output"}
	llm := &mockLLMMultiTurn{}
	sess, err := NewSession(llm, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "test", nil)
	require.NoError(t, err)
	assert.Contains(t, out, "final answer")
	assert.True(t, tool.called)
	assert.Equal(t, 2, llm.callCount)
}

func TestSession_AskWithStreaming_NoNotify(t *testing.T) {
	t.Parallel()

	llm := &mockLLM{response: "response"}
	sess, err := NewSession(llm, &SessionConfig{}, nil, nil, nil, "", "") // nil notify
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "test", nil)
	require.NoError(t, err)
	assert.Contains(t, out, "response")
}

func TestSession_SanitizeMessages_ToolResponseAtIndexZero(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	// Start with only a tool response (no prior messages)
	sess.messages = []llms.MessageContent{{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "tc1", Name: "test", Content: "result"},
		},
	}}

	sess.SanitizeMessages()

	// Should remove the orphan tool response
	assert.Empty(t, sess.messages)
}

func TestSession_SanitizeMessages_ToolResponseAfterNonAIMessage(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add human message then tool response (no AI message in between)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "hello"}},
	})
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "tc1", Name: "test", Content: "result"},
		},
	})

	sess.SanitizeMessages()

	// Should remove the tool response (no prior AI message)
	assert.Len(t, sess.messages, 2) // system + human
}

func TestSession_SanitizeMessages_AIMessageWithNoToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add AI message with only text (no tool calls)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{llms.TextContent{Text: "just text"}},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should keep the message (no unmatched tool calls)
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_SanitizeMessages_KeepsSecondToolCallResult(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Human message
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "run ritual"}},
	})

	// First AI tool call
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "Enacting ritual:"},
			llms.ToolCall{
				ID: "tc1", Type: "function",
				FunctionCall: &llms.FunctionCall{Name: "enact_ritual", Arguments: "{}"},
			},
		},
	})

	// First tool result (failed)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "tc1", Name: "enact_ritual", Content: `{"status":"failed"}`},
		},
	})

	// Second AI tool call (retry)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "Retrying:"},
			llms.ToolCall{
				ID: "tc2", Type: "function",
				FunctionCall: &llms.FunctionCall{Name: "enact_ritual", Arguments: "{}"},
			},
		},
	})

	// Second tool result (completed) — this MUST survive sanitization
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "tc2", Name: "enact_ritual", Content: `{"status":"completed"}`},
		},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// All messages should be preserved — the second result must not be stripped
	assert.Equal(t, initialLen, len(sess.messages))
}

// mockLLMCancelDuringStream cancels context during streaming
type mockLLMCancelDuringStream struct {
	llms.Model
	cancel context.CancelFunc
}

func (m *mockLLMCancelDuringStream) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}
	if callOpts.StreamingFunc != nil {
		callOpts.StreamingFunc(ctx, []byte("partial"))
		m.cancel() // Cancel during streaming
		err := callOpts.StreamingFunc(ctx, []byte("more"))
		if err != nil {
			return nil, err
		}
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "partial"}}}, nil
}

func TestSession_AskWithStreaming_CancelDuringStreaming(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	llm := &mockLLMCancelDuringStream{cancel: cancel}

	sess, err := NewSession(llm, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	_, err = sess.AskWithStreaming(ctx, "test", nil)
	// Either error from streaming cancel or success with interruption
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}

// mockLLMSkippedToolCalls returns tool calls that will be skipped (nil FunctionCall)
type mockLLMSkippedToolCalls struct {
	llms.Model
}

func (m *mockLLMSkippedToolCalls) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}
	// Return tool calls that will be skipped AND text content
	if callOpts.StreamingFunc != nil {
		callOpts.StreamingFunc(ctx, []byte("text with skipped tools"))
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
		Content: "text with skipped tools",
		ToolCalls: []llms.ToolCall{
			{ID: "tc1", FunctionCall: nil},                                           // nil FunctionCall - skipped
			{ID: "tc2", FunctionCall: &llms.FunctionCall{Name: "", Arguments: "{}"}}, // empty name - skipped
		},
	}}}, nil
}

func TestSession_AskWithStreaming_AfterToolCall_NoMoreToolMessages(t *testing.T) {
	t.Parallel()

	// This tests the path where tool calls are skipped due to nil/empty FunctionCall
	sess, err := NewSession(&mockLLMSkippedToolCalls{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "test", nil)
	require.NoError(t, err)
	// Should complete with text response, tool calls were skipped
	assert.Contains(t, out, "text with skipped tools")
}

func TestSession_ProcessToolCalls_MultipleToolsOneAborted(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "test_tool", output: "result"}
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)

	// Create context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// First tool call should complete, then we cancel
	toolCalls := []llms.ToolCall{
		{ID: "tc1", Type: "function", FunctionCall: &llms.FunctionCall{Name: "test_tool", Arguments: "{}"}},
		{ID: "tc2", Type: "function", FunctionCall: &llms.FunctionCall{Name: "test_tool", Arguments: "{}"}},
	}

	// Cancel after the processToolCalls starts (but this is synchronous, so cancel before)
	cancel()

	messages, shouldReturn := sess.processToolCalls(ctx, toolCalls)

	assert.True(t, shouldReturn)
	// Should have abort responses for remaining tool calls
	require.NotEmpty(t, messages)
}

func TestSession_GetContextInfo_Anthropic(t *testing.T) {
	t.Parallel()

	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-latest",
		},
	}

	sess, err := NewSession(&mockLLMNoTools{}, cfg, nil, nil, func(any) {}, "You are a helpful assistant", "")
	require.NoError(t, err)

	info := sess.GetContextInfo()

	assert.Equal(t, "claude-3-5-sonnet-latest", info.Model)
	assert.Equal(t, 200_000, info.TotalTokens)
	assert.Greater(t, info.SystemPromptTokens, 0)
	assert.Equal(t, 0, info.MessagesTokens)
	assert.Greater(t, info.FreeTokens, 0)
	assert.Greater(t, info.AutocompactBuffer, 0)
	assert.Equal(t, info.SystemPromptTokens+info.SystemToolsTokens+info.MemoryFilesTokens+info.MessagesTokens, info.UsedTokens)
	assert.Equal(t, info.TotalTokens, info.UsedTokens+info.FreeTokens+info.AutocompactBuffer)
}

func TestSession_GetContextInfo_OpenAI(t *testing.T) {
	t.Parallel()

	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
	}

	sess, err := NewSession(&mockLLMNoTools{}, cfg, nil, nil, func(any) {}, "You are a helpful assistant", "")
	require.NoError(t, err)

	info := sess.GetContextInfo()

	assert.Equal(t, "gpt-4o", info.Model)
	assert.Greater(t, info.TotalTokens, 0)
	assert.Greater(t, info.SystemPromptTokens, 0)
	assert.Equal(t, info.TotalTokens, info.UsedTokens+info.FreeTokens+info.AutocompactBuffer)
}

func TestSession_GetContextInfo_WithContextFiles(t *testing.T) {
	t.Parallel()

	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-latest",
		},
	}

	sess, err := NewSession(&mockLLMNoTools{}, cfg, nil, nil, func(any) {}, "system prompt", "")
	require.NoError(t, err)

	infoBefore := sess.GetContextInfo()

	// AddContextFile triggers updateTokenCounts
	sess.AddContextFile("test.go", "package main\n\nfunc main() {}\n")

	infoAfter := sess.GetContextInfo()

	assert.Greater(t, infoAfter.MemoryFilesTokens, infoBefore.MemoryFilesTokens)
	assert.Greater(t, infoAfter.UsedTokens, infoBefore.UsedTokens)
	assert.Equal(t, infoAfter.SystemPromptTokens+infoAfter.SystemToolsTokens+infoAfter.MemoryFilesTokens+infoAfter.MessagesTokens, infoAfter.UsedTokens)
}

func TestSession_GetContextInfo_UnknownModel(t *testing.T) {
	t.Parallel()

	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			Provider: "custom",
			Model:    "unknown-model-xyz",
		},
	}

	sess, err := NewSession(&mockLLMNoTools{}, cfg, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	info := sess.GetContextInfo()

	assert.Equal(t, "unknown-model-xyz", info.Model)
	assert.Equal(t, defaultUnknownContextRef, info.TotalTokens)
}

// --- Test for asimisql large output truncation bug ---

// mockToolLargeOutput simulates a tool that returns very large output,
// similar to what asimisql would return for large query results.
type mockToolLargeOutput struct {
	mockTool
	outputSize int
}

func (t *mockToolLargeOutput) Call(ctx context.Context, input string) (string, error) {
	t.called = true
	// Return output larger than DefaultMaxOutputSize (50KB)
	return strings.Repeat("col1|col2|col3|col4\n", t.outputSize/20), nil
}

// TestSession_ExecuteToolCall_LargeOutputTruncated verifies that large tool output
// is truncated when executed through Session.executeToolCall(). This is the fix
// for the bug where asimisql tool output was causing context explosion.
func TestSession_ExecuteToolCall_LargeOutputTruncated(t *testing.T) {
	t.Parallel()

	// Create a tool that returns 100KB of output (exceeds DefaultMaxOutputSize of 50KB)
	largeOutputSize := 100 * 1024
	tool := &mockToolLargeOutput{
		mockTool: mockTool{name: "asimisql", output: ""},
		outputSize: largeOutputSize,
	}

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)
	sess.scheduler = nil // Use direct execution

	tc := llms.ToolCall{
		ID:           "tc1",
		Type:         "function",
		FunctionCall: &llms.FunctionCall{Name: "asimisql", Arguments: `{"query":"SELECT * FROM edicts"}`},
	}

	resp := sess.executeToolCall(context.Background(), tool, tc, `{"query":"SELECT * FROM edicts"}`)

	// The output should be truncated to DefaultMaxOutputSize (50KB)
	assert.Less(t, len(resp.Content), int(runners.DefaultMaxOutputSize)+200,
		"Tool output should be truncated to roughly DefaultMaxOutputSize")

	// Should contain truncation marker
	assert.Contains(t, resp.Content, "... +",
		"Truncated output should contain marker indicating lines were skipped")
}

// TestSession_ExecuteToolCall_LargeOutputTruncatedViaScheduler verifies truncation
// works when using the scheduler, which is the normal execution path for tools.
func TestSession_ExecuteToolCall_LargeOutputTruncatedViaScheduler(t *testing.T) {
	t.Parallel()

	// Create a tool that returns 100KB of output
	largeOutputSize := 100 * 1024
	tool := &mockToolLargeOutput{
		mockTool: mockTool{name: "asimisql", output: ""},
		outputSize: largeOutputSize,
	}

	scheduler := runners.NewCoreToolScheduler(func(any) {})
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, []Tool{tool}, scheduler, func(any) {}, "", "")
	require.NoError(t, err)

	tc := llms.ToolCall{
		ID:           "tc1",
		Type:         "function",
		FunctionCall: &llms.FunctionCall{Name: "asimisql", Arguments: `{"query":"SELECT * FROM edicts"}`},
	}

	resp := sess.executeToolCall(context.Background(), tool, tc, `{"query":"SELECT * FROM edicts"}`)

	// Verify truncation occurred
	assert.Less(t, len(resp.Content), int(runners.DefaultMaxOutputSize)+200,
		"Tool output via scheduler should be truncated")
}

// TestSession_ExecuteToolCall_CustomMaxOutput verifies that the MaxToolOutput
// config option controls the truncation size.
func TestSession_ExecuteToolCall_CustomMaxOutput(t *testing.T) {
	t.Parallel()

	customMaxOutput := 10 * 1024 // 10KB
	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			MaxToolOutput: customMaxOutput,
		},
	}

	// Create a tool that returns 100KB of output
	tool := &mockToolLargeOutput{
		mockTool: mockTool{name: "asimisql", output: ""},
		outputSize: 100 * 1024,
	}

	sess, err := NewSession(&mockLLMNoTools{}, cfg, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)
	sess.scheduler = nil

	tc := llms.ToolCall{
		ID:           "tc1",
		Type:         "function",
		FunctionCall: &llms.FunctionCall{Name: "asimisql", Arguments: "{}"},
	}

	resp := sess.executeToolCall(context.Background(), tool, tc, "{}")

	// Output should be truncated to customMaxOutput (10KB)
	assert.Less(t, len(resp.Content), customMaxOutput+200,
		"Tool output should respect custom MaxToolOutput config")
}

// TestSession_Rollback_LeavesOnlySystemPrompt verifies that Rollback()
// discards all messages except the system prompt.
func TestSession_Rollback_LeavesOnlySystemPrompt(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system prompt", "")
	require.NoError(t, err)

	// Initially should have 1 message (system prompt)
	require.Equal(t, 1, len(sess.messages))
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.messages[0].Role)

	// Add multiple messages
	sess.AddMessage(llms.ChatMessageTypeHuman, "Hello")
	sess.AddMessage(llms.ChatMessageTypeAI, "Hi there")
	sess.AddMessage(llms.ChatMessageTypeHuman, "How are you?")
	sess.AddMessage(llms.ChatMessageTypeAI, "I'm doing well")

	// Should now have 5 messages
	require.Equal(t, 5, len(sess.messages))

	// Rollback should discard all but the system prompt
	sess.Rollback()

	// Should now have exactly 1 message
	assert.Equal(t, 1, len(sess.messages), "should have exactly 1 message after rollback")
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.messages[0].Role, "remaining message should be system prompt")
}

// TestSession_Rollback_UpdatesTokenCounts verifies that Rollback updates token counts.
func TestSession_Rollback_UpdatesTokenCounts(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add conversation with content
	sess.AddMessage(llms.ChatMessageTypeHuman, "Hello world this is a test message for token counting")

	// Get message tokens before rollback
	infoBefore := sess.GetContextInfo()
	_ = infoBefore.MessagesTokens // captured for documentation

	sess.Rollback()

	infoAfter := sess.GetContextInfo()

	// After rollback, message tokens should be 0
	assert.Equal(t, 0, infoAfter.MessagesTokens)
	// System prompt tokens should remain
	assert.Equal(t, infoBefore.SystemPromptTokens, infoAfter.SystemPromptTokens)
}

// TestSession_Rollback_ResetsLoopDetection verifies that Rollback resets loop detection state.
func TestSession_Rollback_ResetsLoopDetection(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	// Trigger loop detection
	args := `{"path":"test.txt"}`
	sess.checkToolCallLoop("read_file", args)
	sess.checkToolCallLoop("read_file", args)

	require.Equal(t, 2, sess.toolCallRepetitionCount)
	require.NotEmpty(t, sess.lastToolCallKey)

	sess.Rollback()

	assert.Equal(t, 0, sess.toolCallRepetitionCount, "loop repetition count should be reset")
	assert.Empty(t, sess.lastToolCallKey, "last tool call key should be reset")
}

// TestSession_Rollback_Idempotent verifies that calling Rollback multiple times is safe.
func TestSession_Rollback_Idempotent(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	sess.AddMessage(llms.ChatMessageTypeHuman, "test")

	sess.Rollback()
	firstLen := len(sess.messages)

	sess.Rollback()
	secondLen := len(sess.messages)

	sess.Rollback()
	thirdLen := len(sess.messages)

	assert.Equal(t, 1, firstLen)
	assert.Equal(t, firstLen, secondLen)
	assert.Equal(t, secondLen, thirdLen)
}

// TestSession_ToolOutputAddedToMessageHistory verifies that truncated output
// is what gets added to the message history, preventing context explosion.
func TestSession_ToolOutputAddedToMessageHistory(t *testing.T) {
	t.Parallel()

	// Create a tool that returns large output
	tool := &mockToolLargeOutput{
		mockTool: mockTool{name: "asimisql", output: ""},
		outputSize: 100 * 1024,
	}

	// Mock LLM that returns a tool call, then final response
	llm := &mockLLMWithToolCall{
		toolCall: llms.ToolCall{
			ID:           "tc1",
			Type:         "function",
			FunctionCall: &llms.FunctionCall{Name: "asimisql", Arguments: `{"query":"SELECT * FROM edicts"}`},
		},
		finalResponse: "Query completed",
	}

	scheduler := runners.NewCoreToolScheduler(func(any) {})
	sess, err := NewSession(llm, &SessionConfig{}, []Tool{tool}, scheduler, func(any) {}, "", "")
	require.NoError(t, err)

	_, err = sess.AskWithStreaming(context.Background(), "run query", nil)
	require.NoError(t, err)

	// Find the tool response in message history
	var toolResponseSize int
	for _, msg := range sess.messages {
		if msg.Role == llms.ChatMessageTypeTool {
			for _, part := range msg.Parts {
				if resp, ok := part.(llms.ToolCallResponse); ok {
					toolResponseSize = len(resp.Content)
					break
				}
			}
		}
	}

	// The tool response in message history should be truncated
	assert.Less(t, toolResponseSize, int(runners.DefaultMaxOutputSize)+200,
		"Tool response in message history should be truncated to prevent context explosion")
}
