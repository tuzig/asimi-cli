package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// mockLLM is a mock implementation of the llms.Model interface for testing.
type agentMockLLM struct {
	llms.Model
	lastPrompt string
}

// GenerateContent implements the llms.Model interface.
func (m *agentMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// Capture the prompt from messages
	var b strings.Builder
	for _, msg := range messages {
		for _, part := range msg.Parts {
			if textPart, ok := part.(llms.TextContent); ok {
				b.WriteString(textPart.Text)
			}
		}
	}
	m.lastPrompt = b.String()

	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "Final Answer: This is a test final answer.",
			},
		},
	}, nil
}

// Call implements the llms.Model interface.
func (m *agentMockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	m.lastPrompt = prompt
	return "Action: read_file\nAction Input: { \"path\": \"test.txt\" }", nil
}

func TestSendPromptToAgent(t *testing.T) {
	// Create a mock LLM to intercept the prompt.
	mockLLM := &agentMockLLM{}

	// Create a test config.
	config := &Config{
		LLM: LLMConfig{
			Provider: "fake",
		},
	}

	// Create a test prompt.
	prompt := "test prompt"

	// Create a new agent with the mock LLM.
	agent, err := NewAgentWithLLM(config, mockLLM)
	assert.NoError(t, err)

	// Call the function that sends the prompt to the agent.
	_, callErr := agent.executor.Call(context.Background(), map[string]any{
		"input": prompt,
	})

	// Check if the last prompt passed to the mock LLM contains the test prompt.
	re := regexp.MustCompile(`\s+`)
	normalizedLastPrompt := re.ReplaceAllString(mockLLM.lastPrompt, " ")
	normalizedPrompt := re.ReplaceAllString(prompt, " ")
	normalizedSystemSnippet := re.ReplaceAllString("You are an interactive CLI agent", " ")

	t.Logf("Normalized Last Prompt: %s", normalizedLastPrompt)
	t.Logf("Normalized Prompt: %s", normalizedPrompt)
	t.Logf("Normalized System Snippet: %s", normalizedSystemSnippet)

	assert.True(t, strings.Contains(normalizedLastPrompt, normalizedPrompt), "The prompt should contain the user input")

	// Check if the last prompt contains some text from the system prompt.
	assert.True(t, strings.Contains(normalizedLastPrompt, normalizedSystemSnippet), "The prompt should contain the system prompt")

	// Check that the environment block is included via system partials
	assert.Contains(t, normalizedLastPrompt, "<env>")
	assert.Contains(t, normalizedLastPrompt, "<os>")
	assert.Contains(t, normalizedLastPrompt, "<paths>")
	assert.Contains(t, normalizedLastPrompt, "</env>")

	// Only assert NoError if the error is not related to agent output parsing.
	if callErr != nil && !strings.Contains(callErr.Error(), "unable to parse agent output") {
		assert.NoError(t, callErr)
	}
}

func TestAgentIncludesAgentsMDInUserMemory(t *testing.T) {
	// Prepare a temporary working directory with an AGENTS.md
	tmp := t.TempDir()
	content := "TEST_AGENTS_GUIDE_123: Ensure AGENTS.md is injected into the system prompt."
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed writing temp AGENTS.md: %v", err)
	}

	// Switch CWD to the temporary directory
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// Create a mock LLM to intercept the prompt.
	mockLLM := &agentMockLLM{}

	// Minimal config using fake model
	config := &Config{LLM: LLMConfig{Provider: "fake"}}

	agent, err := NewAgentWithLLM(config, mockLLM)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Trigger a call to build the prompt
	_, _ = agent.executor.Call(context.Background(), map[string]any{"input": "hello"})

	// Normalize whitespace for robust matching
	re := regexp.MustCompile(`\s+`)
	normalizedLastPrompt := re.ReplaceAllString(mockLLM.lastPrompt, " ")
	normalizedAgents := re.ReplaceAllString(content, " ")

	if !strings.Contains(normalizedLastPrompt, normalizedAgents) {
		t.Fatalf("expected system prompt to contain AGENTS.md content; got: %q", normalizedLastPrompt)
	}
}
