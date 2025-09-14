package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// MockStreamingLLM is a mock LLM that supports streaming
type MockStreamingLLM struct {
	response   string
	shouldFail bool
}

func (m *MockStreamingLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// Parse options to find streaming function
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

func (m *MockStreamingLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
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

func TestSession_AskStream(t *testing.T) {
	// Create mock LLM
	mockLLM := &MockStreamingLLM{
		response: "Hello this is a streaming response",
	}

	// Track notifications
	var notifications []interface{}
	notify := func(msg any) {
		notifications = append(notifications, msg)
	}

	// Create session
	session, err := NewSession(mockLLM, nil, notify)
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
	chat := NewChatComponent(80, 20)

	// Chat starts with a welcome message, so append to it first
	chat.AppendToLastMessage(" Additional text")
	assert.Equal(t, 1, len(chat.Messages))
	assert.Contains(t, chat.Messages[0], "Additional text")

	// Test appending more to existing message
	chat.AppendToLastMessage(" More text")
	assert.Equal(t, 1, len(chat.Messages))
	assert.Contains(t, chat.Messages[0], "Additional text More text")

	// Add a new message and append to it
	chat.AddMessage("AI: ")
	chat.AppendToLastMessage("This is streaming")
	assert.Equal(t, 2, len(chat.Messages))
	assert.Equal(t, "AI: This is streaming", chat.Messages[1])
}
