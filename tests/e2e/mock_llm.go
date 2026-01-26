package e2e

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/tmc/langchaingo/llms"
)

// MockLLM provides a configurable mock LLM provider for testing.
// It can return pre-recorded responses and records prompts for assertions.
type MockLLM struct {
	mu sync.Mutex

	// Responses maps prompt patterns to responses.
	// If a prompt contains the key string, the corresponding value is returned.
	Responses map[string]string

	// DefaultResponse is returned when no pattern matches
	DefaultResponse string

	// RecordedPrompts stores all prompts sent to the mock
	RecordedPrompts []PromptRecord

	// CallCount tracks the number of times SendPrompt was called
	CallCount int

	// ResponseDelay adds artificial delay to responses (for timing tests)
	ResponseDelay int // milliseconds

	// ErrorOnPrompt causes SendPrompt to return an error for specific prompts
	ErrorOnPrompt map[string]error

	// SequentialResponses returns responses in order, ignoring prompt content
	// Useful when you need specific responses for each call
	SequentialResponses []string
	sequentialIndex     int
}

// PromptRecord stores information about a prompt that was sent
type PromptRecord struct {
	Prompt   string
	Response string
	Error    error
}

// NewMockLLM creates a new MockLLM with default settings
func NewMockLLM() *MockLLM {
	return &MockLLM{
		Responses:       make(map[string]string),
		ErrorOnPrompt:   make(map[string]error),
		DefaultResponse: "OK",
	}
}

// WithResponse adds a pattern-based response
func (m *MockLLM) WithResponse(pattern, response string) *MockLLM {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses[pattern] = response
	return m
}

// WithDefaultResponse sets the default response when no pattern matches
func (m *MockLLM) WithDefaultResponse(response string) *MockLLM {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DefaultResponse = response
	return m
}

// WithSequentialResponses sets up responses that are returned in order
func (m *MockLLM) WithSequentialResponses(responses ...string) *MockLLM {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SequentialResponses = responses
	m.sequentialIndex = 0
	return m
}

// WithError causes the mock to return an error for prompts containing the pattern
func (m *MockLLM) WithError(pattern string, err error) *MockLLM {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ErrorOnPrompt[pattern] = err
	return m
}

// SendPrompt implements the prompt sending interface used by workflows.
// It returns a channel that will receive the response.
func (m *MockLLM) SendPrompt(ctx context.Context, prompt string) <-chan string {
	responseChan := make(chan string, 1)

	go func() {
		defer close(responseChan)

		m.mu.Lock()
		m.CallCount++
		callNum := m.CallCount

		// Check for errors first
		var responseErr error
		for pattern, err := range m.ErrorOnPrompt {
			if strings.Contains(prompt, pattern) {
				responseErr = err
				break
			}
		}

		var response string
		if responseErr != nil {
			m.RecordedPrompts = append(m.RecordedPrompts, PromptRecord{
				Prompt: prompt,
				Error:  responseErr,
			})
			m.mu.Unlock()
			responseChan <- ""
			return
		}

		// Check for sequential responses first
		if len(m.SequentialResponses) > 0 && m.sequentialIndex < len(m.SequentialResponses) {
			response = m.SequentialResponses[m.sequentialIndex]
			m.sequentialIndex++
		} else {
			// Look for pattern match
			found := false
			for pattern, resp := range m.Responses {
				if strings.Contains(prompt, pattern) {
					response = resp
					found = true
					break
				}
			}
			if !found {
				response = m.DefaultResponse
			}
		}

		m.RecordedPrompts = append(m.RecordedPrompts, PromptRecord{
			Prompt:   prompt,
			Response: response,
		})
		m.mu.Unlock()

		// Check context cancellation (only if context is not nil)
		if ctx != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}

		_ = callNum // Suppress unused variable warning
		responseChan <- response
	}()

	return responseChan
}

// GetPromptHistory returns all recorded prompts
func (m *MockLLM) GetPromptHistory() []PromptRecord {
	m.mu.Lock()
	defer m.mu.Unlock()

	history := make([]PromptRecord, len(m.RecordedPrompts))
	copy(history, m.RecordedPrompts)
	return history
}

// GetCallCount returns the number of times SendPrompt was called
func (m *MockLLM) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.CallCount
}

// Reset clears all recorded prompts and resets the call count
func (m *MockLLM) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedPrompts = nil
	m.CallCount = 0
	m.sequentialIndex = 0
}

// AssertPromptContains checks that at least one prompt contained the given string
func (m *MockLLM) AssertPromptContains(pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, record := range m.RecordedPrompts {
		if strings.Contains(record.Prompt, pattern) {
			return nil
		}
	}
	return fmt.Errorf("no prompt contained pattern: %q", pattern)
}

// AssertCallCount checks that SendPrompt was called exactly n times
func (m *MockLLM) AssertCallCount(expected int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CallCount != expected {
		return fmt.Errorf("expected %d calls, got %d", expected, m.CallCount)
	}
	return nil
}

// LastPrompt returns the most recent prompt, or empty string if none
func (m *MockLLM) LastPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.RecordedPrompts) == 0 {
		return ""
	}
	return m.RecordedPrompts[len(m.RecordedPrompts)-1].Prompt
}

// PromptAt returns the prompt at the given index (0-based)
func (m *MockLLM) PromptAt(index int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.RecordedPrompts) {
		return "", fmt.Errorf("prompt index %d out of range (have %d prompts)", index, len(m.RecordedPrompts))
	}
	return m.RecordedPrompts[index].Prompt, nil
}

// FileCreatingResponse generates a mock response that simulates the LLM creating files.
// This is useful for testing the init workflow which expects the LLM to create files.
func FileCreatingResponse(agentsFile string) string {
	return fmt.Sprintf(`I'll analyze the project and create the necessary files.

<tool_call>
{"name": "write_file", "arguments": {"path": "%s", "content": "# Project Guidelines\n\nThis is an auto-generated agents file.\n"}}
</tool_call>

<tool_call>
{"name": "write_file", "arguments": {"path": ".agents/sandbox/Dockerfile", "content": "FROM node:20-slim\nWORKDIR /app\nRUN npm install -g npm@latest\n"}}
</tool_call>

Files have been created successfully.`, agentsFile)
}

// FixDockerfileResponse generates a mock response for fixing a Dockerfile build error
func FixDockerfileResponse() string {
	return `I see the issue. Let me fix the Dockerfile.

<tool_call>
{"name": "write_file", "arguments": {"path": ".agents/sandbox/Dockerfile", "content": "FROM node:20-slim\nWORKDIR /app\nRUN apt-get update && apt-get install -y git\n"}}
</tool_call>

The Dockerfile has been updated with the necessary fixes.`
}

// FixTestsResponse generates a mock response for fixing test failures
func FixTestsResponse() string {
	return `I'll fix the failing tests.

<tool_call>
{"name": "write_file", "arguments": {"path": "fix_applied.txt", "content": "Tests fixed"}}
</tool_call>

The tests should pass now.`
}

// MockLLMModel implements llms.Model for e2e testing with tool calls.
// Unlike MockLLM which uses channels, this implements the langchaingo interface
// directly and can return structured tool calls.
type MockLLMModel struct {
	llms.Model // Embed to satisfy interface for methods we don't implement
	mu         sync.Mutex

	// Responses is a sequence of responses to return
	// Each response can be text-only or include tool calls
	Responses []MockLLMResponse

	// responseIndex tracks which response to return next
	responseIndex int

	// RecordedMessages stores all messages sent to the mock
	RecordedMessages [][]llms.MessageContent
}

// MockLLMResponse represents a single response from the mock LLM
type MockLLMResponse struct {
	// Content is the text response (if any)
	Content string

	// ToolCalls are the tool calls to return (if any)
	ToolCalls []llms.ToolCall

	// StopReason indicates why the response ended
	StopReason string
}

// NewMockLLMModel creates a new mock LLM model for e2e testing
func NewMockLLMModel() *MockLLMModel {
	return &MockLLMModel{
		Responses: []MockLLMResponse{},
	}
}

// WithTextResponse adds a simple text response
func (m *MockLLMModel) WithTextResponse(content string) *MockLLMModel {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses = append(m.Responses, MockLLMResponse{
		Content:    content,
		StopReason: "end_turn",
	})
	return m
}

// WithToolCall adds a response that includes a tool call
func (m *MockLLMModel) WithToolCall(toolName, toolID, arguments string) *MockLLMModel {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses = append(m.Responses, MockLLMResponse{
		ToolCalls: []llms.ToolCall{
			{
				ID:   toolID,
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      toolName,
					Arguments: arguments,
				},
			},
		},
		StopReason: "tool_use",
	})
	return m
}

// WithWriteFileToolCall is a convenience method that adds a write_file tool call
func (m *MockLLMModel) WithWriteFileToolCall(path, content string) *MockLLMModel {
	args := fmt.Sprintf(`{"path":%q,"content":%q}`, path, content)
	return m.WithToolCall("write_file", "tc-write-"+path, args)
}

// Call implements the llms.Model interface for simple prompts
func (m *MockLLMModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
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

// GenerateContent implements the llms.Model interface
func (m *MockLLMModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record the messages
	messagesCopy := make([]llms.MessageContent, len(messages))
	copy(messagesCopy, messages)
	m.RecordedMessages = append(m.RecordedMessages, messagesCopy)

	// Process call options for streaming
	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}

	// Get the next response
	var response MockLLMResponse
	if m.responseIndex < len(m.Responses) {
		response = m.Responses[m.responseIndex]
		m.responseIndex++
	} else {
		// Default response when we run out
		response = MockLLMResponse{
			Content:    "Task completed.",
			StopReason: "end_turn",
		}
	}

	// Handle streaming if requested
	if callOpts.StreamingFunc != nil && response.Content != "" {
		// Stream the content word by word
		words := strings.Split(response.Content, " ")
		for i, word := range words {
			chunk := word
			if i < len(words)-1 {
				chunk += " "
			}
			if err := callOpts.StreamingFunc(ctx, []byte(chunk)); err != nil {
				return nil, err
			}
		}
	}

	// Build the response
	choice := &llms.ContentChoice{
		Content:    response.Content,
		StopReason: response.StopReason,
		ToolCalls:  response.ToolCalls,
	}

	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{choice},
	}, nil
}

// GetRecordedMessages returns all messages that were sent to the mock
func (m *MockLLMModel) GetRecordedMessages() [][]llms.MessageContent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]llms.MessageContent, len(m.RecordedMessages))
	copy(result, m.RecordedMessages)
	return result
}

// Reset clears recorded messages and resets the response index
func (m *MockLLMModel) ResetModel() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedMessages = nil
	m.responseIndex = 0
}
