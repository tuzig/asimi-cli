package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// sessionMockLLM simulates provider-native function/tool calling behavior.
type sessionMockLLM struct{ llms.Model }

// Call is unused in these tests but required by the interface.
func (m *sessionMockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}

// GenerateContent returns a tool call on first round and a final content after tool response.
func (m *sessionMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
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
	sess, err := NewSession(llm, &Config{}, func(any) {})
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
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	out, err := sess.Ask(context.Background(), "say hi")
	assert.NoError(t, err)
	assert.Equal(t, "Hello world", out)
}

// sessionMockLLMReAct returns textual ReAct-style tool usage; Session must parse and execute.
type sessionMockLLMReAct struct{ llms.Model }

func (m *sessionMockLLMReAct) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}
func (m *sessionMockLLMReAct) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	last := messages[len(messages)-1]
	switch last.Role {
	case llms.ChatMessageTypeHuman:
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
			Content: "Action: read_file\nAction Input: { \"path\": \"testdata/test.txt\" }",
		}}}, nil
	case llms.ChatMessageTypeTool:
		// Return final answer echoing the tool output
		var out string
		for i := len(last.Parts) - 1; i >= 0; i-- {
			if tr, ok := last.Parts[i].(llms.ToolCallResponse); ok {
				out = tr.Content
				break
			}
		}
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: out}}}, nil
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}, nil
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

	// Create a temp file path
	tmp := t.TempDir()
	path := tmp + "/wr_test.txt"

	// We encode the path into the system message content via the template; to avoid
	// changing the template, we pass it through the first system message text part.
	// The mock reads that value back.
	sess, err := NewSession(&sessionMockLLMWriteRead{}, &Config{}, func(any) {})
	assert.NoError(t, err)
	// Overwrite the first system message text with the temp path as a simple channel to the mock
	sess.messages[0].Parts = []llms.ContentPart{llms.TextPart(path)}

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
	sess, err := NewSession(llm, &Config{}, func(any) {})
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
	sess, err := NewSession(llm, &Config{}, func(any) {})
	assert.NoError(t, err)

	// Test HasContextFiles - should be true if AGENTS.md exists, false otherwise
	initialHasContext := sess.HasContextFiles()
	initialFiles := sess.GetContextFiles()
	initialCount := len(initialFiles)
	// AGENTS.md may or may not be present depending on whether the file exists

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

	// Verify dynamically added context was cleared after Ask, but AGENTS.md persists
	contextFiles = sess.GetContextFiles()
	assert.Len(t, contextFiles, initialCount)
	assert.Equal(t, initialHasContext, sess.HasContextFiles())

	// Test ClearContext explicitly - should preserve AGENTS.md
	sess.AddContextFile("test.txt", "test content")
	assert.True(t, sess.HasContextFiles())
	sess.ClearContext()
	assert.Equal(t, initialHasContext, sess.HasContextFiles())
	contextFiles = sess.GetContextFiles()
	assert.Len(t, contextFiles, initialCount)
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
	sess, err := NewSession(llm, &Config{}, func(any) {})
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
		for _, part := range last.Parts {
			if tr, ok := part.(llms.ToolCallResponse); ok {
				contents = append(contents, strings.TrimSpace(tr.Content))
			}
		}
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
			Content: "FILES:" + strings.Join(contents, "|"),
		}}}, nil
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}, nil
}
