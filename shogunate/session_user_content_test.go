package shogunate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func TestSession_HasUserContent_EmptySession(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	assert.False(t, sess.HasUserContent(), "Empty session should have no user content")
}

func TestSession_HasUserContent_SystemOnly(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system only prompt", "")
	require.NoError(t, err)

	assert.False(t, sess.HasUserContent(), "System-only session should have no user content")
}

func TestSession_HasUserContent_WithHumanMessage(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})

	assert.True(t, sess.HasUserContent(), "Session with Human message should have user content")
}

func TestSession_HasUserContent_WithAIMessage(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})

	assert.True(t, sess.HasUserContent(), "Session with AI message should have user content")
}

func TestSession_HasUserContent_WithToolMessageOnly(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "tc1", Name: "some_tool", Content: "result"},
		},
	})

	assert.False(t, sess.HasUserContent(), "Tool-only session should have no user content")
}

func TestSession_ExtractFirstPrompt_EmptySession(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	assert.Equal(t, "", sess.ExtractFirstPrompt(), "Empty session should return empty string")
}

func TestSession_ExtractFirstPrompt_SystemOnly(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system only prompt", "")
	require.NoError(t, err)

	assert.Equal(t, "", sess.ExtractFirstPrompt(), "System-only session should return empty string")
}

func TestSession_ExtractFirstPrompt_WithHumanMessage(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello, world!"}},
	})

	assert.Equal(t, "Hello, world!", sess.ExtractFirstPrompt())
}

func TestSession_ExtractFirstPrompt_SkipsNonHumanMessages(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{llms.TextContent{Text: "AI response"}},
	})
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Human prompt"}},
	})

	assert.Equal(t, "Human prompt", sess.ExtractFirstPrompt())
}

func TestSession_ExtractFirstPrompt_ReturnsFullText(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	longText := strings.Repeat("x", 150)
	sess.messages = append(sess.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: longText}},
	})

	prompt := sess.ExtractFirstPrompt()
	assert.Equal(t, 150, len(prompt), "Full prompt should be returned without truncation")
	assert.Equal(t, longText, prompt)
}

func TestSession_ExtractFirstPrompt_SkipsNonTextParts(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	// Add a Human message with a tool call part (non-text) followed by text part
	sess.messages = append(sess.messages, llms.MessageContent{
		Role: llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{
			llms.ToolCall{ID: "call1", Type: "function", FunctionCall: &llms.FunctionCall{Name: "some_func", Arguments: "{}"}},
			llms.TextContent{Text: "The actual prompt"},
		},
	})

	assert.Equal(t, "The actual prompt", sess.ExtractFirstPrompt())
}
