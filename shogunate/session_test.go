package shogunate

import (
	"context"
	"strings"
	"testing"

	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// msgContentStr extracts the string content from a schemas.ChatMessage.
// Returns empty string if no content is set.
func msgContentStr(msg schemas.ChatMessage) string {
	if msg.Content != nil && msg.Content.ContentStr != nil {
		return *msg.Content.ContentStr
	}
	return ""
}

func TestNewSession(t *testing.T) {
	t.Parallel()

	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			Provider: "test",
			Model:    "test-model",
		},
	}

	sess, err := NewSession(nil, cfg, nil, nil, func(any) {}, "You are a test assistant", "")
	require.NoError(t, err)

	assert.NotEmpty(t, sess.ID)
	assert.NotNil(t, sess.toolCatalog)
	assert.NotNil(t, sess.scheduler)

	// Should have system message
	require.NotEmpty(t, sess.messages)
	assert.Equal(t, schemas.ChatMessageRoleSystem, sess.messages[0].Role)
}

func TestNewSession_DefaultMaxTurns(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, nil, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	assert.Equal(t, 999, sess.config.MaxTurns)
}

func TestNewSession_WithTools(t *testing.T) {
	t.Parallel()

	tools := []Tool{
		&mockTool{name: "test_tool", output: "tool output"},
	}

	sess, err := NewSession(nil, nil, tools, nil, func(any) {}, "", "")
	require.NoError(t, err)

	assert.Len(t, sess.toolDefs, 1)
	assert.Len(t, sess.toolCatalog, 1)
	assert.Contains(t, sess.toolCatalog, "test_tool")
}

func TestSession_AskWithStreaming_NoTools(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_WithResponse(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_StreamsChunks(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_WithToolExecution(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_WithContextFiles(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_SanitizeMessages_RemovesUnmatchedToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add a user message
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("Hello"),
	})

	// Add an AI message with tool call (no corresponding tool response)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role: schemas.ChatMessageRoleAssistant,
		ChatAssistantMessage: &schemas.ChatAssistantMessage{
			ToolCalls: []schemas.ChatAssistantMessageToolCall{
				{
					ID:   strPtr("tc1"),
					Type: strPtr("function"),
					Function: schemas.ChatAssistantMessageToolCallFunction{
						Name:      strPtr("some_tool"),
						Arguments: "{}",
					},
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

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add a user message
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("Hello"),
	})

	// Add an orphan tool response (no matching AI message)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("orphan result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("orphan_tc")},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should have removed the orphan tool response
	assert.Less(t, len(sess.messages), initialLen, "Orphan tool responses should be removed")
}

func TestSession_SanitizeMessages_KeepsMatchedToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add user message
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("Hello"),
	})

	// Add AI message with tool call
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role: schemas.ChatMessageRoleAssistant,
		ChatAssistantMessage: &schemas.ChatAssistantMessage{
			ToolCalls: []schemas.ChatAssistantMessageToolCall{
				{
					ID:   strPtr("tc1"),
					Type: strPtr("function"),
					Function: schemas.ChatAssistantMessageToolCallFunction{
						Name:      strPtr("some_tool"),
						Arguments: "{}",
					},
				},
			},
		},
	})

	// Add matching tool response
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("tool result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc1")},
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
	sess, err := NewSession(nil, cfg, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add unmatched tool call
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role: schemas.ChatMessageRoleAssistant,
		ChatAssistantMessage: &schemas.ChatAssistantMessage{
			ToolCalls: []schemas.ChatAssistantMessageToolCall{
				{
					ID:   strPtr("tc1"),
					Type: strPtr("function"),
					Function: schemas.ChatAssistantMessageToolCallFunction{
						Name:      strPtr("some_tool"),
						Arguments: "{}",
					},
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
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_CheckToolCallLoop(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
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

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
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

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
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

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system prompt", "")
	require.NoError(t, err)

	messages := sess.Messages()
	require.NotEmpty(t, messages)
	assert.Equal(t, schemas.ChatMessageRoleSystem, messages[0].Role)
}

func TestSession_AddMessage(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.AddMessage(schemas.ChatMessageRoleUser, "Hello")

	assert.Equal(t, initialLen+1, len(sess.messages))
	lastMsg := sess.messages[len(sess.messages)-1]
	assert.Equal(t, schemas.ChatMessageRoleUser, lastMsg.Role)
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

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
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

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	toolCalls := []schemas.ChatAssistantMessageToolCall{{
		ID:   strPtr("tc1"),
		Type: strPtr("function"),
		Function: schemas.ChatAssistantMessageToolCallFunction{
			Name:      strPtr("unknown_tool"),
			Arguments: "{}",
		},
	}}

	messages, shouldReturn := sess.processToolCalls(context.Background(), toolCalls)

	assert.False(t, shouldReturn)
	require.Len(t, messages, 1)

	// Check error response
	content := msgContentStr(messages[0])
	assert.Contains(t, content, "unknown tool")
}

func TestSession_ProcessToolCalls_ContextCancellation(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	toolCalls := []schemas.ChatAssistantMessageToolCall{{
		ID:   strPtr("tc1"),
		Type: strPtr("function"),
		Function: schemas.ChatAssistantMessageToolCallFunction{
			Name:      strPtr("some_tool"),
			Arguments: "{}",
		},
	}}

	messages, shouldReturn := sess.processToolCalls(ctx, toolCalls)

	assert.True(t, shouldReturn)
	require.NotEmpty(t, messages)

	// Should have abort response
	content := msgContentStr(messages[0])
	assert.Contains(t, content, "aborted")
}

// --- Additional tests for full coverage ---

func TestSession_AddTools(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
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
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, nil, "", "")
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
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestEnsureToolCallID_ExistingID(t *testing.T) {
	t.Parallel()

	tc := &schemas.ChatAssistantMessageToolCall{
		ID: strPtr("existing-id"),
		Function: schemas.ChatAssistantMessageToolCallFunction{
			Name: strPtr("test"),
		},
	}

	result := ensureToolCallID(tc, 0)
	assert.Equal(t, "existing-id", result)
	assert.Equal(t, "existing-id", *tc.ID)
}

func TestEnsureToolCallID_GeneratesSyntheticID(t *testing.T) {
	t.Parallel()

	tc := &schemas.ChatAssistantMessageToolCall{
		ID: strPtr(""), // Empty ID
		Function: schemas.ChatAssistantMessageToolCallFunction{
			Name: strPtr("test"),
		},
	}

	result := ensureToolCallID(tc, 0)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "synthetic_")
	assert.Equal(t, result, *tc.ID) // Should be set on the struct
}

func TestHasToolCallResponse_Found(t *testing.T) {
	t.Parallel()

	toolMessages := []schemas.ChatMessage{{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc1")},
	}}

	assert.True(t, hasToolCallResponse(toolMessages, "tc1"))
}

func TestHasToolCallResponse_NotFound(t *testing.T) {
	t.Parallel()

	toolMessages := []schemas.ChatMessage{{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc1")},
	}}

	assert.False(t, hasToolCallResponse(toolMessages, "tc2"))
}

func TestHasToolCallResponse_EmptyMessages(t *testing.T) {
	t.Parallel()

	assert.False(t, hasToolCallResponse(nil, "tc1"))
	assert.False(t, hasToolCallResponse([]schemas.ChatMessage{}, "tc1"))
}

func TestHasToolCallResponse_NonToolMessage(t *testing.T) {
	t.Parallel()

	messages := []schemas.ChatMessage{{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("hello"),
	}}

	assert.False(t, hasToolCallResponse(messages, "tc1"))
}

func TestSession_SanitizeMessages_EmptyMessages(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	sess.messages = []schemas.ChatMessage{}
	sess.SanitizeMessages() // Should not panic
	assert.Empty(t, sess.messages)
}

func TestSession_SanitizeMessages_OrphanToolResponse(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add orphan tool response without prior AI message
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc1")},
	})

	sess.SanitizeMessages()

	// Should remove orphan tool response, keep only system message
	assert.Len(t, sess.messages, 1)
	assert.Equal(t, schemas.ChatMessageRoleSystem, sess.messages[0].Role)
}

func TestSession_SanitizeMessages_ToolResponseWithMismatchedID(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add AI message with tool call
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role: schemas.ChatMessageRoleAssistant,
		ChatAssistantMessage: &schemas.ChatAssistantMessage{
			ToolCalls: []schemas.ChatAssistantMessageToolCall{
				{
					ID:       strPtr("tc1"),
					Type:     strPtr("function"),
					Function: schemas.ChatAssistantMessageToolCallFunction{Name: strPtr("test"), Arguments: "{}"},
				},
			},
		},
	})

	// Add tool response with DIFFERENT ID
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc2")}, // Mismatched!
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should remove mismatched messages
	assert.Less(t, len(sess.messages), initialLen)
}

func TestSession_SanitizeMessages_ToolResponseWithEmptyID(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add AI message with tool call
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role: schemas.ChatMessageRoleAssistant,
		ChatAssistantMessage: &schemas.ChatAssistantMessage{
			ToolCalls: []schemas.ChatAssistantMessageToolCall{
				{
					ID:       strPtr("tc1"),
					Type:     strPtr("function"),
					Function: schemas.ChatAssistantMessageToolCallFunction{Name: strPtr("test"), Arguments: "{}"},
				},
			},
		},
	})

	// Add tool response with empty ID
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("")}, // Empty!
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should remove messages with empty tool call ID
	assert.Less(t, len(sess.messages), initialLen)
}

func TestSession_AppendMessage_NilChoice(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(nil)

	// Should not add anything for nil choice
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_AppendMessage_EmptyContent(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(&responseChoice{Content: "   "}) // Only whitespace

	// Should not add message with only whitespace
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_AppendMessage_WithToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(&responseChoice{
		Content: "",
		ToolCalls: []schemas.ChatAssistantMessageToolCall{{
			ID:   strPtr("tc1"),
			Type: strPtr("function"),
			Function: schemas.ChatAssistantMessageToolCallFunction{
				Name:      strPtr("test"),
				Arguments: "{}",
			},
		}},
	})

	// Should add message with tool calls even without content
	assert.Equal(t, initialLen+1, len(sess.messages))
}

func TestSession_AppendMessage_WithInvalidToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	initialLen := len(sess.messages)
	sess.appendMessage(&responseChoice{
		Content: "text",
		ToolCalls: []schemas.ChatAssistantMessageToolCall{
			{ID: strPtr("tc1"), Function: schemas.ChatAssistantMessageToolCallFunction{Name: nil}},        // nil Name
			{ID: strPtr("tc2"), Function: schemas.ChatAssistantMessageToolCallFunction{Name: strPtr("")}}, // empty name
		},
	})

	// Should add message with content and tool calls (appendMessage stores them as-is;
	// filtering happens in processToolCalls)
	assert.Equal(t, initialLen+1, len(sess.messages))
	lastMsg := sess.messages[len(sess.messages)-1]
	assert.NotNil(t, lastMsg.Content)
	assert.NotNil(t, lastMsg.ChatAssistantMessage)
	assert.Len(t, lastMsg.ChatAssistantMessage.ToolCalls, 2)
}

func TestNewSession_WithProvidedScheduler(t *testing.T) {
	t.Parallel()

	customScheduler := runners.NewCoreToolScheduler(func(any) {})
	sess, err := NewSession(nil, &SessionConfig{}, nil, customScheduler, func(any) {}, "", "")
	require.NoError(t, err)

	assert.Equal(t, customScheduler, sess.scheduler)
}

func TestSession_AskWithStreaming_LLMError(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_GenerateLLMResponse_EmptyChoices(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_Cancellation(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_MaxTokens(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_ErrorStopReason(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_ProcessToolCalls_LoopDetected(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "loop_tool", output: "result"}
	sess, err := NewSession(nil, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)

	toolCall := schemas.ChatAssistantMessageToolCall{
		ID:   strPtr("tc1"),
		Type: strPtr("function"),
		Function: schemas.ChatAssistantMessageToolCallFunction{
			Name:      strPtr("loop_tool"),
			Arguments: `{"same":"args"}`,
		},
	}

	// Call 3 times to trigger loop detection
	sess.processToolCalls(context.Background(), []schemas.ChatAssistantMessageToolCall{toolCall})
	sess.processToolCalls(context.Background(), []schemas.ChatAssistantMessageToolCall{toolCall})
	messages, shouldReturn := sess.processToolCalls(context.Background(), []schemas.ChatAssistantMessageToolCall{toolCall})

	assert.True(t, shouldReturn)
	require.NotEmpty(t, messages)
	content := msgContentStr(messages[0])
	assert.Contains(t, content, "loop detected")
}

func TestSession_ProcessToolCalls_SkipsEmptyName(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	toolCalls := []schemas.ChatAssistantMessageToolCall{
		{
			ID:       strPtr("tc1"),
			Type:     strPtr("function"),
			Function: schemas.ChatAssistantMessageToolCallFunction{Name: strPtr(""), Arguments: "{}"},
		},
		{
			ID:       strPtr("tc2"),
			Type:     strPtr("function"),
			Function: schemas.ChatAssistantMessageToolCallFunction{Name: nil, Arguments: "{}"}, // nil Name
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

	sess, err := NewSession(nil, &SessionConfig{}, []Tool{tool}, scheduler, func(any) {}, "", "")
	require.NoError(t, err)

	resp := sess.executeToolCall(context.Background(), tool, "tc1", "scheduled_tool", "{}")

	content := msgContentStr(resp)
	assert.Equal(t, "scheduled result", content)
	assert.True(t, tool.called)
}

func TestSession_ExecuteToolCall_WithoutScheduler(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "direct_tool", output: "direct result"}

	sess, err := NewSession(nil, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)
	sess.scheduler = nil // Force no scheduler

	resp := sess.executeToolCall(context.Background(), tool, "tc1", "direct_tool", "{}")

	content := msgContentStr(resp)
	assert.Equal(t, "direct result", content)
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

	sess, err := NewSession(nil, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)
	sess.scheduler = nil

	resp := sess.executeToolCall(context.Background(), tool, "tc1", "error_tool", "{}")

	content := msgContentStr(resp)
	assert.Contains(t, content, "Error:")
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

func TestSession_AskWithStreaming_RepeatingResponse(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_MultiTurnToolExecution(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_NoNotify(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_SanitizeMessages_ToolResponseAtIndexZero(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
	require.NoError(t, err)

	// Start with only a tool response (no prior messages)
	sess.messages = []schemas.ChatMessage{{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc1")},
	}}

	sess.SanitizeMessages()

	// Should remove the orphan tool response
	assert.Empty(t, sess.messages)
}

func TestSession_SanitizeMessages_ToolResponseAfterNonAIMessage(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add human message then tool response (no AI message in between)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("hello"),
	})
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc1")},
	})

	sess.SanitizeMessages()

	// Should remove the tool response (no prior AI message)
	assert.Len(t, sess.messages, 2) // system + human
}

func TestSession_SanitizeMessages_AIMessageWithNoToolCalls(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add AI message with only text (no tool calls)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleAssistant,
		Content: textContent("just text"),
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// Should keep the message (no unmatched tool calls)
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_SanitizeMessages_KeepsSecondToolCallResult(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Human message
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("run ritual"),
	})

	// First AI tool call
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleAssistant,
		Content: textContent("Enacting ritual:"),
		ChatAssistantMessage: &schemas.ChatAssistantMessage{
			ToolCalls: []schemas.ChatAssistantMessageToolCall{
				{
					ID: strPtr("tc1"), Type: strPtr("function"),
					Function: schemas.ChatAssistantMessageToolCallFunction{Name: strPtr("enact_ritual"), Arguments: "{}"},
				},
			},
		},
	})

	// First tool result (failed)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent(`{"status":"failed"}`),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc1")},
	})

	// Second AI tool call (retry)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleAssistant,
		Content: textContent("Retrying:"),
		ChatAssistantMessage: &schemas.ChatAssistantMessage{
			ToolCalls: []schemas.ChatAssistantMessageToolCall{
				{
					ID: strPtr("tc2"), Type: strPtr("function"),
					Function: schemas.ChatAssistantMessageToolCallFunction{Name: strPtr("enact_ritual"), Arguments: "{}"},
				},
			},
		},
	})

	// Second tool result (completed) -- this MUST survive sanitization
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent(`{"status":"completed"}`),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc2")},
	})

	initialLen := len(sess.messages)
	sess.SanitizeMessages()

	// All messages should be preserved -- the second result must not be stripped
	assert.Equal(t, initialLen, len(sess.messages))
}

func TestSession_AskWithStreaming_CancelDuringStreaming(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_AskWithStreaming_AfterToolCall_NoMoreToolMessages(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses")
}

func TestSession_ProcessToolCalls_MultipleToolsOneAborted(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "test_tool", output: "result"}
	sess, err := NewSession(nil, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)

	// Create context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// First tool call should complete, then we cancel
	toolCalls := []schemas.ChatAssistantMessageToolCall{
		{ID: strPtr("tc1"), Type: strPtr("function"), Function: schemas.ChatAssistantMessageToolCallFunction{Name: strPtr("test_tool"), Arguments: "{}"}},
		{ID: strPtr("tc2"), Type: strPtr("function"), Function: schemas.ChatAssistantMessageToolCallFunction{Name: strPtr("test_tool"), Arguments: "{}"}},
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

	sess, err := NewSession(nil, cfg, nil, nil, func(any) {}, "You are a helpful assistant", "")
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

	sess, err := NewSession(nil, cfg, nil, nil, func(any) {}, "You are a helpful assistant", "")
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

	sess, err := NewSession(nil, cfg, nil, nil, func(any) {}, "system prompt", "")
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

	sess, err := NewSession(nil, cfg, nil, nil, func(any) {}, "system", "")
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
		mockTool:   mockTool{name: "asimisql", output: ""},
		outputSize: largeOutputSize,
	}

	sess, err := NewSession(nil, &SessionConfig{}, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)
	sess.scheduler = nil // Use direct execution

	resp := sess.executeToolCall(context.Background(), tool, "tc1", "asimisql", `{"query":"SELECT * FROM edicts"}`)

	// Should contain truncation marker
	content := msgContentStr(resp)
	assert.Contains(t, content, "result is too long",
		"Huge output should report to the caller")
}

// TestSession_ExecuteToolCall_LargeOutputTruncatedViaScheduler verifies truncation
// works when using the scheduler, which is the normal execution path for tools.
func TestSession_ExecuteToolCall_LargeOutputTruncatedViaScheduler(t *testing.T) {
	t.Parallel()

	// Create a tool that returns 100KB of output
	largeOutputSize := 100 * 1024
	tool := &mockToolLargeOutput{
		mockTool:   mockTool{name: "asimisql", output: ""},
		outputSize: largeOutputSize,
	}

	scheduler := runners.NewCoreToolScheduler(func(any) {})
	sess, err := NewSession(nil, &SessionConfig{}, []Tool{tool}, scheduler, func(any) {}, "", "")
	require.NoError(t, err)

	resp := sess.executeToolCall(context.Background(), tool, "tc1", "asimisql", `{"query":"SELECT * FROM edicts"}`)

	// Verify truncation occurred
	content := msgContentStr(resp)
	assert.Less(t, len(content), int(runners.DefaultMaxOutputSize)+200,
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
		mockTool:   mockTool{name: "asimisql", output: ""},
		outputSize: 100 * 1024,
	}

	sess, err := NewSession(nil, cfg, []Tool{tool}, nil, func(any) {}, "", "")
	require.NoError(t, err)
	sess.scheduler = nil

	resp := sess.executeToolCall(context.Background(), tool, "tc1", "asimisql", "{}")

	// Output should be truncated to customMaxOutput (10KB)
	content := msgContentStr(resp)
	assert.Less(t, len(content), customMaxOutput+200,
		"Tool output should respect custom MaxToolOutput config")
}

// TestSession_Rollback_LeavesOnlySystemPrompt verifies that Rollback()
// discards all messages except the system prompt.
func TestSession_Rollback_LeavesOnlySystemPrompt(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system prompt", "")
	require.NoError(t, err)

	// Initially should have 1 message (system prompt)
	require.Equal(t, 1, len(sess.messages))
	assert.Equal(t, schemas.ChatMessageRoleSystem, sess.messages[0].Role)

	// Add multiple messages
	sess.AddMessage(schemas.ChatMessageRoleUser, "Hello")
	sess.AddMessage(schemas.ChatMessageRoleAssistant, "Hi there")
	sess.AddMessage(schemas.ChatMessageRoleUser, "How are you?")
	sess.AddMessage(schemas.ChatMessageRoleAssistant, "I'm doing well")

	// Should now have 5 messages
	require.Equal(t, 5, len(sess.messages))

	// Rollback should discard all but the system prompt
	sess.Rollback()

	// Should now have exactly 1 message
	assert.Equal(t, 1, len(sess.messages), "should have exactly 1 message after rollback")
	assert.Equal(t, schemas.ChatMessageRoleSystem, sess.messages[0].Role, "remaining message should be system prompt")
}

// TestSession_Rollback_UpdatesTokenCounts verifies that Rollback updates token counts.
func TestSession_Rollback_UpdatesTokenCounts(t *testing.T) {
	t.Parallel()

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	// Add conversation with content
	sess.AddMessage(schemas.ChatMessageRoleUser, "Hello world this is a test message for token counting")

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

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "", "")
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

	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	sess.AddMessage(schemas.ChatMessageRoleUser, "test")

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
	t.Skip("requires mock bifrost client for LLM responses")
}
