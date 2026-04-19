package mocks

import (
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

// MockProvider is a mock implementation of LLMProvider for testing.
// It allows tests to simulate LLM responses without requiring actual API keys.
// Note: This mock implements the shogunate.LLMProvider interface by method signature.
// The compile-time interface check is in shogunate/mocks_test.go to avoid import cycles.
type MockProvider struct {
	StreamingChunks []StreamingChunk
	StaticResponse  *schemas.BifrostChatResponse
	Error           *schemas.BifrostError
	DelayBetweenChunks time.Duration
}

// NewLLMProviderWithChunks creates a mock with specific streaming chunks.
func NewLLMProviderWithChunks(chunks []StreamingChunk) *MockProvider {
	return &MockProvider{StreamingChunks: chunks}
}

// NewLLMProvider creates a new MockLLMBridge with default streaming chunks.
func NewLLMProvider() *MockProvider {
	return NewLLMProviderWithChunks([]StreamingChunk{
		{Content: "This "},
		{Content: "is "},
		{Content: "a "},
		{Content: "mock "},
		{Content: "streaming "},
		{Content: "response", FinishReason: "stop"},
	})
}

// StreamingChunk represents a single chunk in a streaming response.
type StreamingChunk struct {
	// Content is the text content for this chunk (optional)
	Content string
	// Reasoning is the reasoning content for this chunk (optional)
	Reasoning string
	// FinishReason is set on the last chunk (optional)
	FinishReason string
	// Usage is set on the last chunk (optional)
	Usage *schemas.BifrostLLMUsage
}

// ChatCompletionRequest implements the LLMBridge interface.
func (m *MockProvider) ChatCompletionRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	if m.Error != nil {
		return nil, m.Error
	}

	if m.StaticResponse != nil {
		return m.StaticResponse, nil
	}

	// Return a default response if none set
	content := "This is a mock response"
	return &schemas.BifrostChatResponse{
		ID: "mock-chatcmpl-123",
		Choices: []schemas.BifrostResponseChoice{
			{
				Index: 0,
				ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
					Delta: &schemas.ChatStreamResponseChoiceDelta{
						Content: &content,
					},
				},
				FinishReason: strPtr("stop"),
			},
		},
	}, nil
}

// ChatCompletionStreamRequest implements the LLMBridge interface.
func (m *MockProvider) ChatCompletionStreamRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	ch := make(chan *schemas.BifrostStreamChunk)

	go func() {
		defer close(ch)

		if m.Error != nil {
			ch <- &schemas.BifrostStreamChunk{BifrostError: m.Error}
			return
		}

		chunks := m.StreamingChunks
		if len(chunks) == 0 {
			chunks = []StreamingChunk{
				{Content: "This "},
				{Content: "is "},
				{Content: "a "},
				{Content: "mock "},
				{Content: "streaming "},
				{Content: "response", FinishReason: "stop"},
			}
		}

		for i, chunk := range chunks {
			if m.DelayBetweenChunks > 0 {
				time.Sleep(m.DelayBetweenChunks)
			}

			delta := &schemas.ChatStreamResponseChoiceDelta{}
			if chunk.Content != "" {
				delta.Content = &chunk.Content
			}
			if chunk.Reasoning != "" {
				delta.Reasoning = &chunk.Reasoning
			}

			isLast := i == len(chunks)-1

			resp := &schemas.BifrostChatResponse{
				ID: "mock-chatcmpl-123",
				Choices: []schemas.BifrostResponseChoice{
					{
						Index: i,
						ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
							Delta: delta,
						},
						FinishReason: func() *string {
							if chunk.FinishReason != "" || isLast {
								r := chunk.FinishReason
								if r == "" {
									r = "stop"
								}
								return &r
							}
							return nil
						}(),
					},
				},
			}

			// Add usage on last chunk
			if isLast && chunk.Usage != nil {
				resp.Usage = chunk.Usage
			} else if isLast && chunk.FinishReason != "" {
				resp.Usage = &schemas.BifrostLLMUsage{
					PromptTokens:     100,
					CompletionTokens: 50,
					TotalTokens:      150,
				}
			}

			ch <- &schemas.BifrostStreamChunk{BifrostChatResponse: resp}
		}
	}()

	return ch, nil
}

// SetStreamingChunks configures the chunks returned by ChatCompletionStreamRequest.
func (m *MockProvider) SetStreamingChunks(chunks []StreamingChunk) {
	m.StreamingChunks = chunks
}

// SetStaticResponse configures the response returned by ChatCompletionRequest.
func (m *MockProvider) SetStaticResponse(resp *schemas.BifrostChatResponse) {
	m.StaticResponse = resp
}

// SetError configures an error to return for all requests.
func (m *MockProvider) SetError(err *schemas.BifrostError) {
	m.Error = err
}

// Helper functions

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
