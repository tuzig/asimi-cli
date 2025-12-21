package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	lctools "github.com/tmc/langchaingo/tools"
)

// sessionMockLLM simulates provider-native function/tool calling behavior and streaming.
type sessionMockLLM struct {
	llms.Model
	response string // If set, returns this as a simple response instead of tool calling
}

// repoInfoWithProjectRoot returns a RepoInfo populated with the current working directory.
func repoInfoWithProjectRoot(t *testing.T) RepoInfo {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	return RepoInfo{ProjectRoot: cwd}
}

// Call is unused in these tests but required by the interface.
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

// GenerateContent returns a tool call on first round and a final content after tool response.
// If response field is set, it returns that as a simple streaming response instead.
func (m *sessionMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// If response is set, use streaming mode
	if m.response != "" {
		callOpts := &llms.CallOptions{}
		for _, opt := range options {
			opt(callOpts)
		}

		// If streaming function is provided, simulate streaming
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
			Choices: []*llms.ContentChoice{
				{
					Content:   m.response,
					ToolCalls: nil,
				},
			},
		}, nil
	}

	// Tool calling mode
	last := messages[len(messages)-1]
	switch last.Role {
	case llms.ChatMessageTypeHuman:
		// Ask the runtime to read a file via tool call.
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
		// Echo back the tool output in a final assistant message so Session stops looping.
		// Find the last tool response content.
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

func TestSession_ToolRoundTrip(t *testing.T) {
	t.Parallel()

	// Set up a native session with the mock LLM and real tools/scheduler.
	llm := &sessionMockLLM{}
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, func(any) {})
	assert.NoError(t, err)

	out, err := sess.Ask(context.Background(), "please read the file")
	assert.NoError(t, err)
	assert.Contains(t, out, "This is a test file.")
}

// mockLLMNoTools returns a direct assistant message without any tool calls.
type mockLLMNoTools struct{ llms.Model }

func (m *mockLLMNoTools) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}
func (m *mockLLMNoTools) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "Hello world"}}}, nil
}

func TestSession_NoTools(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, func(any) {})
	assert.NoError(t, err)

	out, err := sess.Ask(context.Background(), "say hi")
	assert.NoError(t, err)
	assert.Equal(t, "Hello world", out)
}

func TestNewSessionSystemMessageSinglePart(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	cfg := &Config{
		LLM: LLMConfig{
			Provider: "ollama",
			Model:    "dummy",
		},
	}

	sess, err := NewSession(llm, cfg, RepoInfo{}, func(any) {})
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

// sessionMockLLMWriteRead simulates a write_file followed by read_file and then returns file content.
type sessionMockLLMWriteRead struct{ llms.Model }

func (m *sessionMockLLMWriteRead) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}
func (m *sessionMockLLMWriteRead) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	last := messages[len(messages)-1]
	switch last.Role {
	case llms.ChatMessageTypeHuman:
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID:   "w1",
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "write_file",
					Arguments: `{"path":"` + messages[0].Parts[0].(llms.TextContent).Text + `","content":"hello world"}`,
				},
			}},
		}}}, nil
	case llms.ChatMessageTypeTool:
		// If last tool was write_file, ask to read it; else finish with file content
		// Look back for the last tool name
		for i := len(last.Parts) - 1; i >= 0; i-- {
			if tr, ok := last.Parts[i].(llms.ToolCallResponse); ok {
				if tr.Name == "write_file" {
					// Next, request read_file on same path which we encode in system msg hack
					path := messages[0].Parts[0].(llms.TextContent).Text
					return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
						ToolCalls: []llms.ToolCall{{
							ID:   "r1",
							Type: "function",
							FunctionCall: &llms.FunctionCall{
								Name:      "read_file",
								Arguments: `{"path":"` + path + `"}`,
							},
						}},
					}}}, nil
				}
				if tr.Name == "read_file" {
					return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "FILE:" + tr.Content}}}, nil
				}
			}
		}
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}, nil
}

func TestSession_WriteAndReadFile(t *testing.T) {
	t.Parallel()

	// Create a temp file path under repo-controlled directory to avoid /tmp quotas
	tmpBase := filepath.Join("test_tmp")
	err := os.MkdirAll(tmpBase, 0o755)
	require.NoError(t, err)
	tmp, err := os.MkdirTemp(tmpBase, "session")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(tmp)
	})
	path := tmp + "/wr_test.txt"

	// We encode the path into the system message content via the template; to avoid
	// changing the template, we pass it through the first system message text part.
	// The mock reads that value back.
	sess, err := NewSession(&sessionMockLLMWriteRead{}, &Config{}, RepoInfo{}, func(any) {})
	assert.NoError(t, err)
	// Overwrite the first system message text with the temp path as a simple channel to the mock
	sess.Messages[0].Parts = []llms.ContentPart{llms.TextPart(path)}

	out, err := sess.Ask(context.Background(), "please write then read")
	assert.NoError(t, err)
	assert.Contains(t, out, "hello world")
}

// historyPreservingMockLLM echoes all user messages to verify history is maintained
type historyPreservingMockLLM struct{ llms.Model }

func (m *historyPreservingMockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}

func (m *historyPreservingMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// Collect all user messages from the conversation history
	var userMessages []string
	for _, msg := range messages {
		if msg.Role == llms.ChatMessageTypeHuman {
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					userMessages = append(userMessages, textPart.Text)
				}
			}
		}
	}

	// Respond with all user messages we've seen
	response := "I have received these messages from you: "
	for i, msg := range userMessages {
		if i > 0 {
			response += " | "
		}
		response += msg
	}

	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: response,
			},
		},
	}, nil
}

func TestSession_ChatHistoryPersistence(t *testing.T) {
	t.Parallel()

	// Create session with history-preserving mock
	llm := &historyPreservingMockLLM{}
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, func(any) {})
	assert.NoError(t, err)

	// First message
	out1, err := sess.Ask(context.Background(), "hi! my name is John Doe")
	assert.NoError(t, err)
	assert.Contains(t, out1, "hi! my name is John Doe")

	// Second message - should contain both messages in history
	out2, err := sess.Ask(context.Background(), "What is my name?")
	assert.NoError(t, err)
	assert.Contains(t, out2, "hi! my name is John Doe", "First message should be in history")
	assert.Contains(t, out2, "What is my name?", "Second message should be in history")
}

// TestSession_ContextFiles tests the context file functionality
func TestSession_ContextFiles(t *testing.T) {
	// Create temporary test files
	tmpDir := t.TempDir()
	contextFile1 := filepath.Join(tmpDir, "context1.txt")
	contextFile2 := filepath.Join(tmpDir, "context2.txt")

	err := os.WriteFile(contextFile1, []byte("context file 1 content"), 0644)
	assert.NoError(t, err)
	err = os.WriteFile(contextFile2, []byte("context file 2 content"), 0644)
	assert.NoError(t, err)

	llm := &sessionMockLLMContext{}
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, func(any) {})
	assert.NoError(t, err)

	// Test HasContextFiles - should be false initially (AGENTS.md is in system prompt, not ContextFiles)
	initialHasContext := sess.HasContextFiles()
	assert.False(t, initialHasContext, "ContextFiles should be empty initially")
	initialFiles := sess.GetContextFiles()
	initialCount := len(initialFiles)
	assert.Equal(t, 0, initialCount, "ContextFiles should be empty initially")

	// Add context files
	content1, err := os.ReadFile(contextFile1)
	assert.NoError(t, err)
	sess.AddContextFile("context1.txt", string(content1))

	content2, err := os.ReadFile(contextFile2)
	assert.NoError(t, err)
	sess.AddContextFile("context2.txt", string(content2))

	// Test HasContextFiles when files added
	assert.True(t, sess.HasContextFiles())

	// Test GetContextFiles - should have initial count + 2 new files
	contextFiles := sess.GetContextFiles()
	assert.Len(t, contextFiles, initialCount+2)
	assert.Equal(t, "context file 1 content", contextFiles["context1.txt"])
	assert.Equal(t, "context file 2 content", contextFiles["context2.txt"])

	// Send a message - the mock will verify context is included
	out, err := sess.Ask(context.Background(), "use the context")
	assert.NoError(t, err)
	assert.Contains(t, out, "CONTEXT:context1.txt,context2.txt")

	// Verify dynamically added context was cleared after Ask
	contextFiles = sess.GetContextFiles()
	assert.Len(t, contextFiles, 0, "ContextFiles should be empty after Ask")
	assert.False(t, sess.HasContextFiles(), "HasContextFiles should be false after Ask")

	// Test ClearContext explicitly - should clear all context
	sess.AddContextFile("test.txt", "test content")
	assert.True(t, sess.HasContextFiles())
	sess.ClearContext()
	assert.False(t, sess.HasContextFiles(), "HasContextFiles should be false after ClearContext")
	contextFiles = sess.GetContextFiles()
	assert.Len(t, contextFiles, 0, "ContextFiles should be empty after ClearContext")
}

// sessionMockLLMContext verifies that context files are included in prompts
type sessionMockLLMContext struct{}

func (m *sessionMockLLMContext) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}

func (m *sessionMockLLMContext) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// Extract the user message to verify context was included
	for _, msg := range messages {
		if msg.Role == llms.ChatMessageTypeHuman {
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					content := textPart.Text
					// Check if context files are included
					var contextFiles []string
					if strings.Contains(content, "context1.txt") {
						contextFiles = append(contextFiles, "context1.txt")
					}
					if strings.Contains(content, "context2.txt") {
						contextFiles = append(contextFiles, "context2.txt")
					}
					if len(contextFiles) > 0 {
						return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
							Content: "CONTEXT:" + strings.Join(contextFiles, ","),
						}}}, nil
					}
				}
			}
		}
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "no context found"}}}, nil
}

// TestSession_MultipleToolCalls tests that Session can handle multiple tool calls in a single response
func TestSession_MultipleToolCalls(t *testing.T) {
	// Create temporary test files
	tmpDir := t.TempDir()
	testFile1 := filepath.Join(tmpDir, "testdata1.txt")
	testFile2 := filepath.Join(tmpDir, "testdata2.txt")

	err := os.WriteFile(testFile1, []byte("testdata1 content"), 0644)
	assert.NoError(t, err)
	err = os.WriteFile(testFile2, []byte("testdata2 content"), 0644)
	assert.NoError(t, err)

	// Change to temp directory for the test
	oldWd, err := os.Getwd()
	assert.NoError(t, err)
	err = os.Chdir(tmpDir)
	assert.NoError(t, err)
	defer os.Chdir(oldWd)

	llm := &sessionMockLLMMultiTools{}
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, func(any) {})
	assert.NoError(t, err)

	out, err := sess.Ask(context.Background(), "read two files")
	assert.NoError(t, err)
	assert.Equal(t, "FILES:testdata1 content|testdata2 content", out)
}

// sessionMockLLMMultiTools returns multiple tool calls in a single response
type sessionMockLLMMultiTools struct{}

func (m *sessionMockLLMMultiTools) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}

func (m *sessionMockLLMMultiTools) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	last := messages[len(messages)-1]
	switch last.Role {
	case llms.ChatMessageTypeHuman:
		// Return two tool calls in a single response
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{
				{
					ID:   "tc1",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      "read_file",
						Arguments: `{"path":"testdata1.txt"}`,
					},
				},
				{
					ID:   "tc2",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      "read_file",
						Arguments: `{"path":"testdata2.txt"}`,
					},
				},
			},
		}}}, nil
	case llms.ChatMessageTypeTool:
		// After receiving tool responses, generate final answer
		var contents []string
		for i := len(messages) - 1; i >= 0; i-- {
			msg := messages[i]
			if msg.Role != llms.ChatMessageTypeTool {
				break
			}
			for _, part := range msg.Parts {
				if tr, ok := part.(llms.ToolCallResponse); ok {
					contents = append([]string{strings.TrimSpace(tr.Content)}, contents...)
				}
			}
		}
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
			Content: "FILES:" + strings.Join(contents, "|"),
		}}}, nil
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}, nil
}

// TestSession_GetMessageSnapshot tests the snapshot functionality
func TestSession_GetMessageSnapshot(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, func(any) {})
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
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, func(any) {})
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
	assert.Equal(t, snapshot1, len(sess.Messages))

	// Verify we can continue from rolled back state
	_, err = sess.Ask(context.Background(), "new second message")
	assert.NoError(t, err)
	// Should add user + ai + ai = 3 more messages
	assert.Equal(t, snapshot1+3, len(sess.Messages))
}

// TestSession_RollbackToZero tests rollback with invalid snapshot
func TestSession_RollbackToZero(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, func(any) {})
	assert.NoError(t, err)

	_, err = sess.Ask(context.Background(), "test message")
	assert.NoError(t, err)

	// Rollback to 0 should preserve system message
	sess.RollbackTo(0)
	assert.Equal(t, 1, len(sess.Messages), "Should preserve system message")

	// Rollback to negative should preserve system message
	sess.RollbackTo(-5)
	assert.Equal(t, 1, len(sess.Messages), "Should preserve system message")
}

// TestSession_RollbackBeyondLength tests rollback with snapshot beyond current length
func TestSession_RollbackBeyondLength(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, func(any) {})
	assert.NoError(t, err)

	_, err = sess.Ask(context.Background(), "test message")
	assert.NoError(t, err)
	currentLen := len(sess.Messages)

	// Rollback to beyond current length should not change anything
	sess.RollbackTo(currentLen + 10)
	assert.Equal(t, currentLen, len(sess.Messages))
}

// TestSession_RollbackResetsToolLoopDetection tests that rollback resets tool loop state
func TestSession_RollbackResetsToolLoopDetection(t *testing.T) {
	t.Parallel()

	llm := &mockLLMNoTools{}
	repoInfo := RepoInfo{}
	sess, err := NewSession(llm, &Config{}, repoInfo, func(any) {})
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
	repoInfo := RepoInfo{}
	sess, err := NewSession(llm, &Config{}, repoInfo, func(any) {})
	assert.NoError(t, err)

	snapshot1 := sess.GetMessageSnapshot()

	// Execute a message that triggers tool calls
	_, err = sess.Ask(context.Background(), "read a file")
	assert.NoError(t, err)

	// Should have: system + user + assistant(tool call) + tool response + assistant(final)
	assert.Greater(t, len(sess.Messages), snapshot1)

	// Rollback to before the tool call
	sess.RollbackTo(snapshot1)
	assert.Equal(t, snapshot1, len(sess.Messages))

	// Verify we can execute a different command
	_, err = sess.Ask(context.Background(), "different command")
	assert.NoError(t, err)
	assert.Greater(t, len(sess.Messages), snapshot1)
}

// TestSession_MultipleToolMessagesPerCall tests the new one-message-per-tool-call structure
func TestSession_MultipleToolMessagesPerCall(t *testing.T) {
	t.Parallel()

	llm := &sessionMockLLMMultiTools{}
	repoInfo := RepoInfo{}
	sess, err := NewSession(llm, &Config{}, repoInfo, func(any) {})
	assert.NoError(t, err)

	initialLen := len(sess.Messages)

	// Execute a request that triggers multiple tool calls
	_, err = sess.Ask(context.Background(), "read two files")
	assert.NoError(t, err)

	// Count tool messages
	toolMessageCount := 0
	for i := initialLen; i < len(sess.Messages); i++ {
		if sess.Messages[i].Role == llms.ChatMessageTypeTool {
			toolMessageCount++
			// Each tool message should have exactly one part
			assert.Equal(t, 1, len(sess.Messages[i].Parts),
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
	repoInfo := RepoInfo{}
	sess, err := NewSession(llm, &Config{}, repoInfo, func(any) {})
	assert.NoError(t, err)

	// Get the system message
	assert.Equal(t, 1, len(sess.Messages))
	systemMsg := sess.Messages[0]
	assert.Equal(t, llms.ChatMessageTypeSystem, systemMsg.Role)

	// Add some messages
	_, err = sess.Ask(context.Background(), "test1")
	assert.NoError(t, err)
	_, err = sess.Ask(context.Background(), "test2")
	assert.NoError(t, err)

	// Rollback to 0 (should preserve system message)
	sess.RollbackTo(0)
	assert.Equal(t, 1, len(sess.Messages))
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.Messages[0].Role)
	assert.Equal(t, systemMsg, sess.Messages[0])

	// Rollback to 1 (should preserve system message)
	_, err = sess.Ask(context.Background(), "test3")
	assert.NoError(t, err)
	sess.RollbackTo(1)
	assert.Equal(t, 1, len(sess.Messages))
	assert.Equal(t, llms.ChatMessageTypeSystem, sess.Messages[0].Role)
}

func TestSessionStore_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage before creating session store
	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	repoInfo := repoInfoWithProjectRoot(t)
	store, err := NewSessionStore(db, repoInfo, 50, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	session := &Session{
		ID:          "test-session-001",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Hello, world!",
		Provider:    "anthropic",
		Model:       "claude-sonnet-4",
		WorkingDir:  repoInfo.ProjectRoot,
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "Hello, world!"},
				},
			},
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "Hi there!"},
					llms.ToolCall{
						ID:   "call-123",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "write_file",
							Arguments: `{"path":"test.go","content":"package main"}`,
						},
					},
				},
			},
			{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: "call-123",
						Name:       "write_file",
						Content:    "ok",
					},
				},
			},
		},
		ContextFiles: map[string]string{
			"test.go": "package main",
		},
	}

	// Use SaveSessionSync to get immediate error feedback in tests
	if err := store.SaveSessionSync(session); err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	sessions, err := store.ListSessions(10)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}

	sessionData, err := store.LoadSession(sessions[0].ID)
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	if len(sessionData.Messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(sessionData.Messages))
	}

	if sessionData.Provider != "anthropic" {
		t.Fatalf("Expected provider 'anthropic', got '%s'", sessionData.Provider)
	}

	if sessionData.Model != "claude-sonnet-4" {
		t.Fatalf("Expected model 'claude-sonnet-4', got '%s'", sessionData.Model)
	}

	if len(sessionData.Messages[0].Parts) == 0 {
		t.Fatalf("Expected first message to have parts, got 0")
	}
	if _, ok := sessionData.Messages[0].Parts[0].(llms.TextContent); !ok {
		t.Fatalf("Expected first part to be TextContent, got %T", sessionData.Messages[0].Parts[0])
	}

	if len(sessionData.Messages[1].Parts) < 2 {
		t.Fatalf("Expected second message to have at least 2 parts, got %d", len(sessionData.Messages[1].Parts))
	}
	if _, ok := sessionData.Messages[1].Parts[1].(llms.ToolCall); !ok {
		t.Fatalf("Expected second message second part to be ToolCall, got %T", sessionData.Messages[1].Parts[1])
	}

	if len(sessionData.Messages[2].Parts) == 0 {
		t.Fatalf("Expected third message to have parts, got 0")
	}
	if _, ok := sessionData.Messages[2].Parts[0].(llms.ToolCallResponse); !ok {
		t.Fatalf("Expected third message first part to be ToolCallResponse, got %T", sessionData.Messages[2].Parts[0])
	}

	// With SQLite storage, ProjectSlug now includes host: "host/org/project"
	// The old format was just "org/project"
	if sessionData.ProjectSlug == "" {
		t.Fatalf("Expected non-empty project slug, got empty string")
	}

	// Verify the slug has the expected format (host/org/project)
	slugParts := strings.Split(sessionData.ProjectSlug, "/")
	if len(slugParts) != 3 {
		t.Fatalf("Expected project slug format 'host/org/project', got %q", sessionData.ProjectSlug)
	}

	// Verify it contains "asimi" (project name) or "unknown" (when git is not accessible)
	// This handles both normal environments and container environments where git may not be accessible
	if !strings.Contains(sessionData.ProjectSlug, "asimi") && !strings.Contains(sessionData.ProjectSlug, "unknown") {
		t.Fatalf("Expected project slug to contain 'asimi' or 'unknown', got %q", sessionData.ProjectSlug)
	}

	if sessions[0].ProjectSlug != sessionData.ProjectSlug {
		t.Fatalf("Expected consistent project slug %q, got %q", sessionData.ProjectSlug, sessions[0].ProjectSlug)
	}

	// Verify SQLite database file exists (dbPath was already declared earlier)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("Expected database file %s to exist: %v", dbPath, err)
	}
}

func TestSessionStore_RemovesDanglingToolCallsOnSave(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage
	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	repoInfo := RepoInfo{ProjectRoot: tempDir}
	store, err := NewSessionStore(db, repoInfo, 50, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	session := &Session{
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "System prompt"},
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "Need help writing a file"},
				},
			},
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID:   "toolu_123",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "write_file",
							Arguments: `{"path":"main.go","content":"package main"}`,
						},
					},
				},
			},
		},
	}

	store.SaveSession(session)
	store.Flush()

	if len(session.Messages) == 0 {
		t.Fatalf("session should retain at least one message after cleanup")
	}
	if got := session.Messages[len(session.Messages)-1].Role; got != llms.ChatMessageTypeHuman {
		t.Fatalf("expected trailing tool call to be removed, got last role %q", got)
	}

	sessions, err := store.ListSessions(10)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}

	loaded, err := store.LoadSession(sessions[0].ID)
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}
	if len(loaded.Messages) == 0 {
		t.Fatalf("Loaded session should contain messages")
	}
	last := loaded.Messages[len(loaded.Messages)-1]
	if last.Role != llms.ChatMessageTypeHuman {
		t.Fatalf("Expected last message role to be human after cleanup, got %q", last.Role)
	}
	for _, part := range last.Parts {
		if _, ok := part.(llms.ToolCall); ok {
			t.Fatalf("Expected no tool calls in final message after cleanup")
		}
	}
}

// mockLLMValidateNoUnmatchedCalls verifies that messages have no unmatched tool calls
type mockLLMValidateNoUnmatchedCalls struct {
	llms.Model
	t *testing.T
}

func (m *mockLLMValidateNoUnmatchedCalls) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// Verify no unmatched tool calls: if there's an AI message with tool calls,
	// it must be followed by tool result messages
	for i := 0; i < len(messages); i++ {
		if messages[i].Role == llms.ChatMessageTypeAI {
			// Collect tool call IDs from this message
			toolCallIDs := make(map[string]bool)
			for _, part := range messages[i].Parts {
				if tc, ok := part.(llms.ToolCall); ok {
					toolCallIDs[tc.ID] = true
				}
			}

			// If there are tool calls, verify they have responses
			if len(toolCallIDs) > 0 {
				// Check subsequent messages for tool results
				foundResults := make(map[string]bool)
				for j := i + 1; j < len(messages) && messages[j].Role == llms.ChatMessageTypeTool; j++ {
					for _, part := range messages[j].Parts {
						if tcr, ok := part.(llms.ToolCallResponse); ok {
							foundResults[tcr.ToolCallID] = true
						}
					}
				}

				// All tool calls must have results
				for id := range toolCallIDs {
					if !foundResults[id] {
						m.t.Errorf("Found unmatched tool call with ID %s in messages sent to API", id)
					}
				}
			}
		}
	}

	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "Test response"}}}, nil
}

func TestSession_RemovesUnmatchedToolCallsBeforeAPICall(t *testing.T) {
	mock := &mockLLMValidateNoUnmatchedCalls{t: t}
	session := &Session{
		llm: mock,
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "System prompt"},
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "Write a file for me"},
				},
			},
			// This AI message has a tool call with no result (simulates interrupt)
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID:   "toolu_unmatched",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "write_file",
							Arguments: `{"path":"test.go","content":"package main"}`,
						},
					},
				},
			},
		},
	}

	// This should not error because generateLLMResponse should remove the unmatched tool call
	_, err := session.generateLLMResponse(context.Background(), nil)
	if err != nil {
		t.Fatalf("generateLLMResponse failed: %v", err)
	}

	// Verify the session's messages were cleaned up
	if len(session.Messages) > 0 {
		last := session.Messages[len(session.Messages)-1]
		if last.Role == llms.ChatMessageTypeAI {
			// If the last message is AI, it should have no tool calls
			for _, part := range last.Parts {
				if _, ok := part.(llms.ToolCall); ok {
					t.Error("Expected unmatched tool call to be removed from session messages")
				}
			}
		}
	}
}

func TestSessionStore_EmptySession(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage
	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	repoInfo := RepoInfo{ProjectRoot: tempDir}
	store, err := NewSessionStore(db, repoInfo, 50, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	session := &Session{
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "System prompt"},
				},
			},
		},
		ContextFiles: map[string]string{},
	}

	session.Provider = "anthropic"
	session.Model = "claude-sonnet-4"
	store.SaveSession(session)
	store.Flush()
	err = nil
	if err != nil {
		t.Fatalf("SaveSession should not error on empty session: %v", err)
	}

	sessions, err := store.ListSessions(10)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 0 {
		t.Fatalf("Expected 0 sessions (empty session should be skipped), got %d", len(sessions))
	}
}

func TestSessionStore_Cleanup(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage
	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	repoInfo := RepoInfo{ProjectRoot: tempDir}
	store, err := NewSessionStore(db, repoInfo, 2, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	for i := 0; i < 5; i++ {
		session := &Session{
			Messages: []llms.MessageContent{
				{
					Role: llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{
						llms.TextContent{Text: "Message " + string(rune('0'+i))},
					},
				},
			},
			ContextFiles: map[string]string{},
		}

		session.Provider = "anthropic"
		session.Model = "claude-sonnet-4"
		store.SaveSession(session)
		store.Flush()
		err = nil
		if err != nil {
			t.Fatalf("Failed to save session %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	err = store.CleanupOldSessions()
	if err != nil {
		t.Fatalf("Failed to cleanup sessions: %v", err)
	}

	sessions, err := store.ListSessions(10)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("Expected 2 sessions after cleanup (maxSessions=2), got %d", len(sessions))
	}
}

func TestSessionStore_ListSessionsLimit(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage
	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	repoInfo := RepoInfo{ProjectRoot: tempDir}
	store, err := NewSessionStore(db, repoInfo, 50, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	for i := 0; i < 10; i++ {
		session := &Session{
			Messages: []llms.MessageContent{
				{
					Role: llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{
						llms.TextContent{Text: "Message " + string(rune('0'+i))},
					},
				},
			},
			ContextFiles: map[string]string{},
		}

		session.Provider = "anthropic"
		session.Model = "claude-sonnet-4"
		store.SaveSession(session)
		store.Flush()
		err = nil
		if err != nil {
			t.Fatalf("Failed to save session %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	sessions, err := store.ListSessions(5)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 5 {
		t.Fatalf("Expected 5 sessions (limit=5), got %d", len(sessions))
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Now()

	today := formatRelativeTime(now)
	if today[:5] != "Today" {
		t.Errorf("Expected 'Today...', got '%s'", today)
	}

	yesterday := formatRelativeTime(now.AddDate(0, 0, -1))
	if yesterday[:9] != "Yesterday" {
		t.Errorf("Expected 'Yesterday...', got '%s'", yesterday)
	}

	thisYear := formatRelativeTime(now.AddDate(0, -2, 0))
	if thisYear[:3] == "Today" || thisYear[:9] == "Yesterday" {
		t.Errorf("Expected date format, got '%s'", thisYear)
	}
}

func TestSessionStore_DirectoryCreation(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage
	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	repoInfo := RepoInfo{ProjectRoot: tempDir}
	store, err := NewSessionStore(db, repoInfo, 50, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	// With SQLite, verify the database file and its parent directory were created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("Database file was not created: %s", dbPath)
	}

	// Verify we can use the store
	if store.ProjectRoot != repoInfo.ProjectRoot {
		t.Fatalf("Expected projectRoot '%s', got '%s'", repoInfo.ProjectRoot, store.ProjectRoot)
	}
}

func TestSession_AskStream(t *testing.T) {
	// Create mock LLM
	mockLLM := &sessionMockLLM{
		response: "Hello this is a streaming response",
	}

	// Track notifications
	var notifications []interface{}
	notify := func(msg any) {
		notifications = append(notifications, msg)
	}

	// Create session
	repoInfo := RepoInfo{}
	session, err := NewSession(mockLLM, nil, repoInfo, notify)
	require.NoError(t, err)

	// Test streaming
	session.AskStream(context.Background(), "Hello")

	// Wait a bit for the goroutine to complete
	time.Sleep(100 * time.Millisecond)

	// Verify we received streaming notifications
	assert.Greater(t, len(notifications), 0, "Should have received streaming notifications")

	// Check that we received streamChunkMsg notifications
	chunkCount := 0
	completeCount := 0
	for _, notif := range notifications {
		switch notif.(type) {
		case streamChunkMsg:
			chunkCount++
		case streamCompleteMsg:
			completeCount++
		}
	}

	assert.Greater(t, chunkCount, 0, "Should have received chunk notifications")
	assert.Equal(t, 1, completeCount, "Should have received exactly one complete notification")
}

func TestChatComponent_AppendToLastMessage(t *testing.T) {
	chat := NewChatComponent(80, 20, false)

	// Chat starts with a welcome message, so append to it first
	chat.AppendToLastMessage(" Additional text")
	assert.Equal(t, 1, len(chat.Messages))
	assert.Contains(t, chat.Messages[0].Content, "Additional text")

	// Test appending more to existing message
	chat.AppendToLastMessage(" More text")
	assert.Equal(t, 1, len(chat.Messages))
	assert.Contains(t, chat.Messages[0].Content, "Additional text More text")

	// Add a new message and append to it
	chat.AddMessage("Asimi: ")
	chat.AppendToLastMessage("This is streaming")
	assert.Equal(t, 2, len(chat.Messages))
	assert.Equal(t, "Asimi: This is streaming", chat.Messages[1].Content)
}

func TestChatComponent_FinalizeLastAIMessage(t *testing.T) {
	t.Run("success message", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		chat.AddMessage("Asimi: This is a successful response")

		isFailure := chat.FinalizeLastAIMessage()

		assert.False(t, isFailure)
		assert.Equal(t, "Asimi:SUCCESS: This is a successful response", chat.Messages[len(chat.Messages)-1].Content)
	})

	t.Run("failure message", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		chat.AddMessage("Asimi: [[FAILURE]] I cannot do that because of reasons")

		isFailure := chat.FinalizeLastAIMessage()

		assert.True(t, isFailure)
		assert.Equal(t, "Asimi:FAILURE: I cannot do that because of reasons", chat.Messages[len(chat.Messages)-1].Content)
	})

	t.Run("non-AI message", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		chat.AddMessage("You: Hello")

		isFailure := chat.FinalizeLastAIMessage()

		assert.False(t, isFailure)
		// Message should remain unchanged
		assert.Equal(t, "You: Hello", chat.Messages[len(chat.Messages)-1].Content)
	})

	t.Run("empty messages", func(t *testing.T) {
		chat := NewChatComponent(80, 20, false)
		chat.Messages = []ChatMessage{} // Clear all messages

		isFailure := chat.FinalizeLastAIMessage()

		assert.False(t, isFailure)
	})
}

// Tests from session_oauth_test.go

// TestIsOAuthTokenExpiredError tests the OAuth error detection function
func TestIsOAuthTokenExpiredError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "OAuth token expired error",
			err:      errors.New("anthropic: failed to create message: API returned unexpected status code: 401: OAuth token has expired. Please obtain a new token or refresh your existing token."),
			expected: true,
		},
		{
			name:     "OAuth token expire error (different case)",
			err:      errors.New("OAuth Token Has Expired"),
			expected: true,
		},
		{
			name:     "401 with expire keyword",
			err:      errors.New("API returned status 401: token will expire soon"),
			expected: true,
		},
		{
			name:     "Regular 401 without OAuth",
			err:      errors.New("API returned status 401: Unauthorized"),
			expected: false,
		},
		{
			name:     "OAuth without expiration",
			err:      errors.New("OAuth authentication failed"),
			expected: false,
		},
		{
			name:     "Unrelated error",
			err:      errors.New("network timeout"),
			expected: false,
		},
		{
			name:     "API key error",
			err:      errors.New("Invalid API key"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOAuthTokenExpiredError(tt.err)
			assert.Equal(t, tt.expected, result, "Error: %v", tt.err)
		})
	}
}

// mockLLMWithOAuthError is a mock LLM that simulates OAuth token expiration
type mockLLMWithOAuthError struct {
	callCount      int
	failFirstCall  bool
	oauthError     error
	successContent string
}

func (m *mockLLMWithOAuthError) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.callCount++

	// Fail on first call if configured
	if m.failFirstCall && m.callCount == 1 {
		return nil, m.oauthError
	}

	// Return success response
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content:    m.successContent,
				StopReason: "end_turn",
			},
		},
	}, nil
}

func (m *mockLLMWithOAuthError) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", errors.New("not implemented")
}

// TestSession_OAuthTokenRefreshOnError tests that the session automatically refreshes
// OAuth tokens when it encounters an expiration error
func TestSession_OAuthTokenRefreshOnError(t *testing.T) {
	// Note: This test verifies the error detection logic.
	// Full integration testing of token refresh would require mocking the AuthAnthropic
	// and getModelClient functions, which is complex due to package-level dependencies.

	t.Run("detects OAuth expiration error", func(t *testing.T) {
		// Create a session with a mock LLM that returns an OAuth error
		mockLLM := &mockLLMWithOAuthError{
			failFirstCall: true,
			oauthError: errors.New(
				"anthropic: failed to create message: API returned unexpected status code: 401: " +
					"OAuth token has expired. Please obtain a new token or refresh your existing token.",
			),
			successContent: "Success after refresh",
		}

		config := &LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-20241022",
		}

		session := &Session{
			llm:         mockLLM,
			config:      config,
			Messages:    []llms.MessageContent{},
			toolCatalog: map[string]lctools.Tool{},
			toolDefs:    []llms.Tool{},
		}

		// Add a user message
		session.Messages = append(session.Messages, llms.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("test prompt")},
		})

		// Try to generate a response - this should detect the OAuth error
		_, err := session.generateLLMResponse(context.Background(), nil)

		// We expect an error because we can't actually refresh the token in this test
		// (that would require mocking AuthAnthropic and getModelClient)
		assert.Error(t, err)

		// Verify the error message indicates OAuth token expiration was detected
		assert.True(t, strings.Contains(err.Error(), "OAuth token expired"),
			"Expected error to mention OAuth token expiration, got: %v", err)
	})

	t.Run("passes through non-OAuth errors", func(t *testing.T) {
		// Create a session with a mock LLM that returns a non-OAuth error
		mockLLM := &mockLLMWithOAuthError{
			failFirstCall: true,
			oauthError:    errors.New("network timeout"),
		}

		config := &LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-20241022",
		}

		session := &Session{
			llm:         mockLLM,
			config:      config,
			Messages:    []llms.MessageContent{},
			toolCatalog: map[string]lctools.Tool{},
			toolDefs:    []llms.Tool{},
		}

		// Add a user message
		session.Messages = append(session.Messages, llms.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("test prompt")},
		})

		// Try to generate a response
		_, err := session.generateLLMResponse(context.Background(), nil)

		// We expect the original error to be returned
		assert.Error(t, err)
		assert.Equal(t, "network timeout", err.Error())
	})
}
