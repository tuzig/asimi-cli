package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestCalculateBarSegments(t *testing.T) {
	tests := []struct {
		name       string
		percentage float64
		full       int
		partial    bool
	}{
		{"zero", 0, 0, false},
		{"small remainder", 5, 0, true},
		{"two segments", 20, 2, false},
		{"over capacity", 130, contextBarWidth, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			full, partial := calculateBarSegments(tc.percentage)
			if full != tc.full || partial != tc.partial {
				t.Fatalf("expected (%d,%t) got (%d,%t)", tc.full, tc.partial, full, partial)
			}
		})
	}
}

func TestRenderContextBar(t *testing.T) {
	info := ContextInfo{
		TotalTokens:       100,
		UsedTokens:        55,
		AutocompactBuffer: 20,
		FreeTokens:        25,
	}

	bar := renderContextBar(info)
	expected := "⛁ ⛁ ⛁ ⛁ ⛁ ⛀ ⛶ ⛶ ⛶ ⛝"
	if bar != expected {
		t.Fatalf("expected bar %q got %q", expected, bar)
	}

	if strings.Count(bar, " ")+1 != contextBarWidth {
		t.Fatalf("expected %d segments got %d", contextBarWidth, strings.Count(bar, " ")+1)
	}
}

func TestGetContextInfo(t *testing.T) {
	system := llms.MessageContent{
		Role:  llms.ChatMessageTypeSystem,
		Parts: []llms.ContentPart{llms.TextPart("abcdabcd")},
	}
	user := llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart("abcd")},
	}

	cfg := &Config{
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet-latest",
		},
	}
	session, err := NewSession(&sessionMockLLMContext{}, cfg, RepoInfo{}, nil, func(any) {})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	session.Messages = []llms.MessageContent{system, user}
	session.ContextFiles = map[string]string{"file.txt": "abcd"}
	session.toolDefs = nil
	session.updateTokenCounts()

	info := session.GetContextInfo()

	if info.Model != "claude-3-5-sonnet-latest" {
		t.Fatalf("expected model claude-3-5-sonnet-latest got %s", info.Model)
	}
	if info.TotalTokens != 200_000 {
		t.Fatalf("expected total tokens 200000 got %d", info.TotalTokens)
	}
	if info.SystemPromptTokens != 2 {
		t.Fatalf("expected 2 system prompt tokens got %d", info.SystemPromptTokens)
	}
	if info.MemoryFilesTokens != 23 {
		t.Fatalf("expected 23 memory tokens got %d", info.MemoryFilesTokens)
	}
	if info.MessagesTokens != 1 {
		t.Fatalf("expected 1 message token got %d", info.MessagesTokens)
	}
	if info.UsedTokens != 26 {
		t.Fatalf("expected used tokens 26 got %d", info.UsedTokens)
	}
	if info.AutocompactBuffer != 45_000 {
		t.Fatalf("expected autocompact buffer 45000 got %d", info.AutocompactBuffer)
	}
	if info.FreeTokens != 154_974 {
		t.Fatalf("expected free tokens 154974 got %d", info.FreeTokens)
	}
}

func TestGetContextInfoWithOpenAI(t *testing.T) {
	system := llms.MessageContent{
		Role:  llms.ChatMessageTypeSystem,
		Parts: []llms.ContentPart{llms.TextPart("test system prompt")},
	}
	user := llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart("hello")},
	}

	cfg := &Config{
		LLM: LLMConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
	}
	session, err := NewSession(&sessionMockLLMContext{}, cfg, RepoInfo{}, nil, func(any) {})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("expected system message in session")
	}
	session.Messages[0] = system
	session.Messages = append(session.Messages, user)
	session.updateTokenCounts()

	info := session.GetContextInfo()

	if info.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o got %s", info.Model)
	}
	// Should use langchaingo's context size for gpt-4o (128,000)
	if info.TotalTokens != 128_000 {
		t.Fatalf("expected total tokens 128000 got %d", info.TotalTokens)
	}
	// For OpenAI models, token counting should be more accurate via langchaingo
	if info.SystemPromptTokens <= 0 {
		t.Fatalf("expected positive system prompt tokens got %d", info.SystemPromptTokens)
	}
	if info.MessagesTokens <= 0 {
		t.Fatalf("expected positive message tokens got %d", info.MessagesTokens)
	}
}

func TestRenderContextInfoIncludesSections(t *testing.T) {
	info := ContextInfo{
		Model:              "claude-3-5-sonnet-latest",
		TotalTokens:        200_000,
		UsedTokens:         40_000,
		SystemPromptTokens: 2_000,
		SystemToolsTokens:  10_000,
		MemoryFilesTokens:  500,
		MessagesTokens:     1_500,
		FreeTokens:         95_000,
		AutocompactBuffer:  45_000,
	}

	output := renderContextInfo(info)

	expectedSnippets := []string{
		"Context Usage",
		"claude-3-5-sonnet-latest",
		"System prompt",
		"System tools",
		"Memory files",
		"Messages",
		"Free space",
		"↓",
	}

	for _, snippet := range expectedSnippets {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected output to contain %q\n%s", snippet, output)
		}
	}

	// Should NOT contain the old "Autocompact buffer" line
	if strings.Contains(output, "Autocompact buffer") {
		t.Fatalf("output should not contain 'Autocompact buffer' line\n%s", output)
	}
}

func TestHandleContextCommand(t *testing.T) {
	t.Run("no session", func(t *testing.T) {
		model := &TUIModel{}
		cmd := handleContextCommand(model, nil)
		msg := cmd()
		contextMsg, ok := msg.(showContextMsg)
		if !ok {
			t.Fatalf("expected showContextMsg got %T", msg)
		}
		if !strings.Contains(contextMsg.content, "No active session") {
			t.Fatalf("unexpected content: %s", contextMsg.content)
		}
	})

	// TODO: Whay about with shogunate
	t.Run("with session - skipped without shogunate", func(t *testing.T) {
		// This test requires a shogunate session setup.
		// The context command now uses shogunate.Session.GetContextInfo()
		// instead of the legacy Session.
		t.Skip("Requires shogunate session setup - see integration tests")
	})
}

// TestAGENTSmdInSystemPrompt verifies that AGENTS.md content is included in the system prompt
func TestAGENTSmdInSystemPrompt(t *testing.T) {
	llm := &sessionMockLLMContext{}
	sess, err := NewSession(llm, &Config{}, RepoInfo{}, nil, func(any) {})
	assert.NoError(t, err)

	info := sess.GetContextInfo()

	// AGENTS.md should NOT be in ContextFiles anymore
	contextFiles := sess.GetContextFiles()
	_, hasAgents := contextFiles["AGENTS.md"]
	assert.False(t, hasAgents, "AGENTS.md should not be in ContextFiles (it's in the system prompt)")

	// System prompt should include AGENTS.md content if it exists
	// Check if AGENTS.md exists by trying to read it
	projectContext := readProjectContext("AGENTS.md")
	if projectContext != "" {
		t.Logf("AGENTS.md found with %d characters", len(projectContext))

		// System prompt tokens should include AGENTS.md
		assert.Greater(t, info.SystemPromptTokens, 0, "System prompt should have tokens including AGENTS.md")

		t.Logf("Context breakdown:")
		t.Logf("  System prompt: %d tokens (includes AGENTS.md)", info.SystemPromptTokens)
		t.Logf("  System tools: %d tokens", info.SystemToolsTokens)
		t.Logf("  Memory files: %d tokens", info.MemoryFilesTokens)
		t.Logf("  Messages: %d tokens", info.MessagesTokens)
	} else {
		t.Log("AGENTS.md not found in project directory")
	}
}
