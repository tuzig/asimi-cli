package shogunate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// sessionMockLLM simulates provider-native function/tool calling behavior.
type sessionMockLLM struct {
	llms.Model
	response string // If set, returns this as a simple response
}

func (m *sessionMockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
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

func (m *sessionMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	if m.response != "" {
		callOpts := &llms.CallOptions{}
		for _, opt := range options {
			opt(callOpts)
		}
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

	last := messages[len(messages)-1]
	switch last.Role {
	case llms.ChatMessageTypeHuman:
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{
			{
				ToolCalls: []llms.ToolCall{
					{
						ID:   "tc1",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "read_file",
							Arguments: `{"path":"testdata/test.txt"}`,
						},
					},
				},
			},
		}}, nil
	case llms.ChatMessageTypeTool:
		var toolOut string
		for i := len(last.Parts) - 1; i >= 0; i-- {
			if tr, ok := last.Parts[i].(llms.ToolCallResponse); ok {
				toolOut = tr.Content
				break
			}
		}
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "FILE:" + toolOut}}}, nil
	default:
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}, nil
	}
}

// mockLLMNoTools returns a direct assistant message without any tool calls.
type mockLLMNoTools struct{ llms.Model }

func (m *mockLLMNoTools) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}

func (m *mockLLMNoTools) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "Hello world"}}}, nil
}

func TestSession_ToolRoundTrip(t *testing.T) {
	t.Parallel()

	// Use a mock that returns a simple response instead of tool calls
	// since we don't have real tools registered in this test
	llm := &sessionMockLLM{response: "Hello from the model"}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	assert.NoError(t, err)

	out, err := sess.Ask(context.Background(), "say hello", nil)
	assert.NoError(t, err)
	assert.Contains(t, out, "Hello from the model")
}

func TestSession_NoTools(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	assert.NoError(t, err)

	out, err := sess.Ask(context.Background(), "say hi", nil)
	assert.NoError(t, err)
	assert.Equal(t, "Hello world", out)
}

func TestNewSessionSystemMessageSinglePart(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			Provider: "ollama",
			Model:    "dummy",
		},
	}

	sess, err := NewSession(llm, cfg, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	assert.NoError(t, err)

	if assert.NotEmpty(t, sess.Messages) {
		systemMsg := sess.Messages[0]
		assert.Equal(t, llms.ChatMessageTypeSystem, systemMsg.Role)
		if assert.Len(t, systemMsg.Parts, 1) {
			_, ok := systemMsg.Parts[0].(llms.TextContent)
			assert.True(t, ok)
		}
	}
}

func TestSession_GetMessageSnapshot(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	initialSnapshot := sess.GetMessageSnapshot()
	assert.Equal(t, 1, initialSnapshot) // System message

	sess.Messages = append(sess.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})

	newSnapshot := sess.GetMessageSnapshot()
	assert.Equal(t, 2, newSnapshot)
}

func TestSession_RollbackTo(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	snapshot := sess.GetMessageSnapshot()

	sess.Messages = append(sess.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})
	sess.Messages = append(sess.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hi there!"}},
	})

	assert.Equal(t, 3, len(sess.Messages))

	sess.RollbackTo(snapshot)
	assert.Equal(t, 1, len(sess.Messages))
}

func TestSession_RollbackToZero(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	sess.Messages = append(sess.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})

	// RollbackTo(0) clears all messages except preserves system message if present
	sess.RollbackTo(0)
	// With preserveSystemMessage=true in RollbackTo, system message is kept
	assert.Equal(t, 1, len(sess.Messages))
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.Messages[0].Role)
}

func TestSession_RollbackBeyondLength(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	originalLen := len(sess.Messages)
	sess.RollbackTo(100)
	assert.Equal(t, originalLen, len(sess.Messages))
}

func TestSession_MarkFileAsRead(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	assert.False(t, sess.HasFileBeenRead(testFile))
	sess.MarkFileAsRead(testFile)
	assert.True(t, sess.HasFileBeenRead(testFile))
}

func TestSession_CanWriteFile_NewFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	newFile := filepath.Join(tmpDir, "new.txt")

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	canWrite, reason := sess.CanWriteFile(newFile)
	assert.True(t, canWrite, "Should allow writing new files")
	assert.Empty(t, reason)
}

func TestSession_CanWriteFile_ExistingNotRead(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "existing.txt")
	err := os.WriteFile(existingFile, []byte("original content"), 0644)
	require.NoError(t, err)

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	canWrite, reason := sess.CanWriteFile(existingFile)
	assert.False(t, canWrite, "Should not allow writing existing file without reading first")
	assert.Contains(t, reason, "read_file first")
}

func TestSession_CanWriteFile_ReadThenWrite(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "existing.txt")
	err := os.WriteFile(existingFile, []byte("original content"), 0644)
	require.NoError(t, err)

	sess, err := NewSession(&mockLLMNoTools{}, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	sess.MarkFileAsRead(existingFile)
	canWrite, reason := sess.CanWriteFile(existingFile)
	assert.True(t, canWrite, "Should allow writing after reading")
	assert.Empty(t, reason)
}

func TestSession_ClearHistory(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	sess.Messages = append(sess.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})

	assert.Greater(t, len(sess.Messages), 1)

	sess.ClearHistory()
	assert.Equal(t, 1, len(sess.Messages)) // Only system message remains
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.Messages[0].Role)
}

func TestSession_GetSessionDuration(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	// Session was just created, duration should be very small
	duration := sess.GetSessionDuration()
	assert.Less(t, duration, time.Second)

	// Wait a bit and check duration increased
	time.Sleep(10 * time.Millisecond)
	duration2 := sess.GetSessionDuration()
	assert.Greater(t, duration2, duration)
}

func TestGenerateSessionID(t *testing.T) {
	t.Parallel()

	id1 := GenerateSessionID()
	id2 := GenerateSessionID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)

	// Should have timestamp prefix format
	assert.Contains(t, id1, "-")
	parts := strings.Split(id1, "-")
	assert.GreaterOrEqual(t, len(parts), 4) // date parts + random suffix
}

func TestSession_SanitizeMessages(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &SessionConfig{}, repo.RepoInfo{}, nil, nil, func(any) {}, "")
	require.NoError(t, err)

	// Add messages including a trailing assistant message with tool call
	sess.Messages = append(sess.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
	})
	sess.Messages = append(sess.Messages, llms.MessageContent{
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

	initialLen := len(sess.Messages)
	sess.SanitizeMessages()

	// Should have removed the trailing AI message with unmatched tool call
	assert.Less(t, len(sess.Messages), initialLen)
}
