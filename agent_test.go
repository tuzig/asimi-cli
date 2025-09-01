package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/llms/fake"
	"github.com/tmc/langchaingo/memory"
	"github.com/tmc/langchaingo/tools"
)

func TestAgentWithFakeLLM(t *testing.T) {
	// Create the agent
	agent, err := NewAgent(&Config{
		LLM: LLMConfig{
			Provider: "fake",
		},
	})
	require.NoError(t, err)

	// Execute the agent with a test prompt
	ctx := context.Background()
	result, err := agent.executor.Call(ctx, map[string]any{
		"input": "Read the test file and create an output file with 'Hello, World!'",
	})
	require.NoError(t, err)

	// Check the result
	output, ok := result["output"]
	require.True(t, ok)
	require.Equal(t, "I am a large language model, trained by Google.", output)
}

type mockReadFileTool struct {
	called bool
	path   string
}

func (t *mockReadFileTool) Name() string {
	return "read_file"
}

func (t *mockReadFileTool) Description() string {
	return "Reads a file and returns its content. The input should be a JSON object with a 'path' field."
}

func (t *mockReadFileTool) Call(ctx context.Context, input string) (string, error) {
	t.called = true
	t.path = input
	return "file content", nil
}

var _ tools.Tool = &mockReadFileTool{}

func TestAgentReadFileTool(t *testing.T) {
	// Create a fake LLM that will trigger the read_file tool and then give a final answer
	llm := fake.NewFakeLLM([]string{
		`Action: read_file
Action Input: {"path": "test.txt"}`,
		`AI:file content`,
	})

	// Create the mock tool
	mockTool := &mockReadFileTool{}

	// Create the agent
	agent := agents.NewConversationalAgent(llm, []tools.Tool{mockTool})
	executor := agents.NewExecutor(
		agent,
		agents.WithMemory(memory.NewConversationBuffer()),
	)

	// Execute the agent with a test prompt
	result, err := executor.Call(context.Background(), map[string]any{
		"input": "read the file 'test.txt'",
	})
	require.NoError(t, err)

	// Check if the mock tool was called
	require.True(t, mockTool.called)
	require.Equal(t, `{"path": "test.txt"}`, mockTool.path)

	// Check the final output
	output, ok := result["output"]
	require.True(t, ok)
	require.Equal(t, "file content", output)
}
