package shogunate

import (
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSession_HasUserContent_EmptySession(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	assert.False(t, sess.HasUserContent(), "Empty session should have no user content")
}

func TestSession_HasUserContent_SystemOnly(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system only prompt", "")
	require.NoError(t, err)

	assert.False(t, sess.HasUserContent(), "System-only session should have no user content")
}

func TestSession_HasUserContent_WithHumanMessage(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("Hello"),
	})

	assert.True(t, sess.HasUserContent(), "Session with Human message should have user content")
}

func TestSession_HasUserContent_WithAIMessage(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleAssistant,
		Content: textContent("Hello"),
	})

	assert.True(t, sess.HasUserContent(), "Session with AI message should have user content")
}

func TestSession_HasUserContent_WithToolMessageOnly(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:            schemas.ChatMessageRoleTool,
		Content:         textContent("result"),
		ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: strPtr("tc1")},
	})

	assert.False(t, sess.HasUserContent(), "Tool-only session should have no user content")
}

func TestSession_ExtractFirstPrompt_EmptySession(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)

	assert.Equal(t, "", sess.ExtractFirstPrompt(), "Empty session should return empty string")
}

func TestSession_ExtractFirstPrompt_SystemOnly(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system only prompt", "")
	require.NoError(t, err)

	assert.Equal(t, "", sess.ExtractFirstPrompt(), "System-only session should return empty string")
}

func TestSession_ExtractFirstPrompt_WithHumanMessage(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("Hello, world!"),
	})

	assert.Equal(t, "Hello, world!", sess.ExtractFirstPrompt())
}

func TestSession_ExtractFirstPrompt_SkipsNonHumanMessages(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleAssistant,
		Content: textContent("AI response"),
	})
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("Human prompt"),
	})

	assert.Equal(t, "Human prompt", sess.ExtractFirstPrompt())
}

func TestSession_ExtractFirstPrompt_ReturnsFullText(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	longText := strings.Repeat("x", 150)
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent(longText),
	})

	prompt := sess.ExtractFirstPrompt()
	assert.Equal(t, 150, len(prompt), "Full prompt should be returned without truncation")
	assert.Equal(t, longText, prompt)
}

func TestSession_ExtractFirstPrompt_UserMessageWithContent(t *testing.T) {
	t.Parallel()
	sess, err := NewSession(nil, &SessionConfig{}, nil, nil, func(any) {}, "system", "")
	require.NoError(t, err)
	// A user message with text content
	sess.messages = append(sess.messages, schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleUser,
		Content: textContent("The actual prompt"),
	})

	assert.Equal(t, "The actual prompt", sess.ExtractFirstPrompt())
}
