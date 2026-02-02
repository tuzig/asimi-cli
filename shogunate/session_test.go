package shogunate

import (
	"context"
	"strings"
	"testing"

	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// mockLLM simulates provider-native function/tool calling behavior.
type mockLLM struct {
	llms.Model
	response  string      // If set, returns this as a simple response
	toolCalls []llms.ToolCall // If set, returns these tool calls
	callCount int         // Track number of GenerateContent calls
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

	sess, err := NewSession(llm, cfg, repo.RepoInfo{}, nil, nil, func(any) {}, "You are a test assistant")
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

	sess, err := NewSession(&mockLLMNoTools{}, nil, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	assert.Equal(t, 999, sess.config.MaxTurns)
}

func TestNewSession_WithTools(t *testing.T) {
	t.Parallel()

	tools := []Tool{
		&mockTool{name: "test_tool", output: "tool output"},
	}

	sess, err := NewSession(&mockLLMNoTools{}, nil, repo.RepoInfo{}, tools, nil, func(any) {}, "")
	require.NoError(t, err)

	assert.Len(t, sess.toolDefs, 1)
	assert.Len(t, sess.toolCatalog, 1)
	assert.Contains(t, sess.toolCatalog, "test_tool")
}

func TestSession_AskWithStreaming_NoTools(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "say hi", nil)
	require.NoError(t, err)
	assert.Equal(t, "Hello world", out)
}

func TestSession_AskWithStreaming_WithResponse(t *testing.T) {
	t.Parallel()

	llm := &mockLLM{response: "Hello from the model"}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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
			chunks = append(chunks, string(chunk))
		}
	}

	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, notify, "")
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

	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, []Tool{tool}, nil, func(any) {}, "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "read test.txt", nil)
	require.NoError(t, err)

	assert.True(t, tool.called, "Tool should have been called")
	assert.Contains(t, out, "file contents")
}

func TestSession_AskWithStreaming_WithContextFiles(t *testing.T) {
	t.Parallel()

	llm := &mockLLM{response: "I see the context"}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
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

func TestSession_SanitizeMessages_KeepsMatchedToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
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
	sess, err := NewSession(&mockLLMNoTools{}, cfg, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
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

func TestSession_CheckToolCallLoop(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system prompt")
	require.NoError(t, err)

	messages := sess.Messages()
	require.NotEmpty(t, messages)
	assert.Equal(t, llms.ChatMessageTypeSystem, messages[0].Role)
}

func TestSession_AddMessage(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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

	scheduler := NewCoreToolScheduler(notify)
	tool := &mockTool{name: "test", output: "result"}

	ch := scheduler.Schedule(tool, "{}")
	result := <-ch

	assert.NoError(t, result.Error)
	assert.Equal(t, "result", result.Output)
	assert.True(t, tool.called)
	assert.True(t, notified)
}

func TestSession_StreamBufferMethods(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	tools := []Tool{&mockTool{name: "added_tool"}}
	sess.AddTools(tools)

	assert.Contains(t, sess.toolCatalog, "added_tool")
}

func TestCoreToolScheduler_SetNotify(t *testing.T) {
	t.Parallel()

	scheduler := NewCoreToolScheduler(nil)
	assert.Nil(t, scheduler.notify)

	scheduler.SetNotify(func(any) {})
	assert.NotNil(t, scheduler.notify)
}

func TestMustMarshalJSON(t *testing.T) {
	t.Parallel()

	result := MustMarshalJSON(map[string]string{"key": "value"})
	assert.Contains(t, result, "key")
	assert.Contains(t, result, "value")
}

func TestMustMarshalJSON_Panic(t *testing.T) {
	t.Parallel()

	// Functions cannot be marshaled to JSON
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for unmarshalable value")
		}
	}()

	MustMarshalJSON(func() {})
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	sess.messages = []llms.MessageContent{}
	sess.SanitizeMessages() // Should not panic
	assert.Empty(t, sess.messages)
}

func TestSession_SanitizeMessages_OrphanToolResponse(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(nil)

	// Should not add anything for nil choice
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_AppendMessage_EmptyContent(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(&llms.ContentChoice{Content: "   "}) // Only whitespace

	// Should not add message with only whitespace
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_AppendMessage_WithToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(&llms.ContentChoice{
		Content: "text",
		ToolCalls: []llms.ToolCall{
			{ID: "tc1", FunctionCall: nil}, // nil FunctionCall
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

	customScheduler := NewCoreToolScheduler(func(any) {})
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, customScheduler, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMError{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, notify, "")
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

	sess, err := NewSession(&mockLLMEmpty{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
	require.NoError(t, err)

	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "test"}},
	})

	_, err = sess.generateLLMResponse(context.Background(), nil)
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

	sess, err := NewSession(&mockLLM{response: "long response"}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, notify, "")
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

	sess, err := NewSession(&mockLLMMaxTokens{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, notify, "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "test", nil)
	require.NoError(t, err)
	assert.Contains(t, out, "truncated")
	assert.True(t, maxTokensNotified)
}

func TestSession_ProcessToolCalls_LoopDetected(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "loop_tool", output: "result"}
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, []Tool{tool}, nil, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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
	scheduler := NewCoreToolScheduler(func(any) {})

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, []Tool{tool}, scheduler, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, []Tool{tool}, nil, func(any) {}, "")
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

func (t *mockToolError) Name() string                              { return t.name }
func (t *mockToolError) Description() string                       { return "error tool" }
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, []Tool{tool}, nil, func(any) {}, "")
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
		if _, ok := msg.(ToolCallErrorMsg); ok {
			errorNotified = true
		}
	}

	scheduler := NewCoreToolScheduler(notify)
	tool := &mockToolError{name: "error_tool"}

	ch := scheduler.Schedule(tool, "{}")
	result := <-ch

	assert.Error(t, result.Error)
	assert.True(t, errorNotified)
}

func TestCoreToolScheduler_Schedule_NoNotify(t *testing.T) {
	t.Parallel()

	scheduler := NewCoreToolScheduler(nil) // No notify function
	tool := &mockTool{name: "test", output: "result"}

	ch := scheduler.Schedule(tool, "{}")
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
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, []Tool{tool}, nil, func(any) {}, "")
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
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, nil, "") // nil notify
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "test", nil)
	require.NoError(t, err)
	assert.Contains(t, out, "response")
}

func TestSession_SanitizeMessages_ToolResponseAtIndexZero(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
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

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "system")
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

	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
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
			{ID: "tc1", FunctionCall: nil},                                      // nil FunctionCall - skipped
			{ID: "tc2", FunctionCall: &llms.FunctionCall{Name: "", Arguments: "{}"}}, // empty name - skipped
		},
	}}}, nil
}

func TestSession_AskWithStreaming_AfterToolCall_NoMoreToolMessages(t *testing.T) {
	t.Parallel()

	// This tests the path where tool calls are skipped due to nil/empty FunctionCall
	sess, err := NewSession(&mockLLMSkippedToolCalls{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	out, err := sess.AskWithStreaming(context.Background(), "test", nil)
	require.NoError(t, err)
	// Should complete with text response, tool calls were skipped
	assert.Contains(t, out, "text with skipped tools")
}

func TestSession_ProcessToolCalls_MultipleToolsOneAborted(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "test_tool", output: "result"}
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, []Tool{tool}, nil, func(any) {}, "")
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
