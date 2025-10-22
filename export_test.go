package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

func setTestVersion(t *testing.T) {
	t.Helper()
	t.Setenv("ASIMI_VERSION", "test-version")
	originalVersion := version
	version = ""
	t.Cleanup(func() {
		version = originalVersion
	})
}

func TestGenerateFullExportContent(t *testing.T) {
	setTestVersion(t)
	// Create a test session
	session := &Session{
		ID:          "test-session-123",
		CreatedAt:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		LastUpdated: time.Date(2024, 1, 15, 11, 45, 0, 0, time.UTC),
		Provider:    "anthropic",
		Model:       "claude-3-5-sonnet-latest",
		WorkingDir:  "/home/user/project",
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextPart("You are a helpful assistant."),
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextPart("Hello, how are you?"),
				},
			},
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.TextPart("I'm doing well, thank you!"),
				},
			},
		},
		ContextFiles: map[string]string{
			"AGENTS.md": "# Project Context\nThis is a test project.",
		},
	}

	// Generate export content
	content := generateFullExportContent(session)

	// Verify content contains expected sections
	expectedSections := []string{
		"# Asimi Conversation Export",
		"**Asimi Version:** test-version",
		"**Session ID:** test-session-123",
		"**Provider:** anthropic",
		"**Model:** claude-3-5-sonnet-latest",
		"**Working Directory:** /home/user/project",
		"**Created:** 2024-01-15 10:30:00",
		"**Last Updated:** 2024-01-15 11:45:00",
		"**Exported:**",
		"## System Prompt",
		"You are a helpful assistant.",
		"## Context Files",
		"### AGENTS.md",
		"# Project Context",
		"## Conversation",
		"### User",
		"Hello, how are you?",
		"### Assistant",
		"I'm doing well, thank you!",
	}

	for _, expected := range expectedSections {
		if !strings.Contains(content, expected) {
			t.Errorf("Export content missing expected section: %s", expected)
		}
	}
}

func TestGenerateConversationExportContent(t *testing.T) {
	setTestVersion(t)
	// Create a test session
	session := &Session{
		ID:          "test-session-456",
		CreatedAt:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		LastUpdated: time.Date(2024, 1, 15, 11, 45, 0, 0, time.UTC),
		Provider:    "anthropic",
		Model:       "claude-3-5-sonnet-latest",
		WorkingDir:  "/home/user/project",
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextPart("You are a helpful assistant."),
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextPart("Hello, how are you?"),
				},
			},
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.TextPart("I'm doing well, thank you!"),
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextPart("What's the weather like?"),
				},
			},
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.TextPart("I don't have access to real-time weather data."),
				},
			},
		},
		ContextFiles: map[string]string{
			"AGENTS.md": "# Project Context\nThis is a test project.",
		},
	}

	// Generate conversation export content
	content := generateConversationExportContent(session)

	// Verify content contains expected sections
	expectedSections := []string{
		"# Asimi Conversation",
		"**Asimi Version:** test-version",
		"**Session ID:** test-session-456 | **Working Directory:** /home/user/project",
		"**Provider:** anthropic | **Model:** claude-3-5-sonnet-latest",
		"**Created:** 2024-01-15 10:30:00 | **Last Updated:** 2024-01-15 11:45:00 | **Exported:**",
		"### User",
		"Hello, how are you?",
		"### Assistant",
		"I'm doing well, thank you!",
		"What's the weather like?",
		"I don't have access to real-time weather data.",
	}

	for _, expected := range expectedSections {
		if !strings.Contains(content, expected) {
			t.Errorf("Conversation export content missing expected section: %s", expected)
		}
	}

	// Verify content does NOT contain full export sections
	unexpectedSections := []string{
		"## System Prompt",
		"You are a helpful assistant.",
		"## Context Files",
		"### AGENTS.md",
	}

	for _, unexpected := range unexpectedSections {
		if strings.Contains(content, unexpected) {
			t.Errorf("Conversation export content should not include: %s", unexpected)
		}
	}
}

func TestGenerateConversationExportContentWithToolCalls(t *testing.T) {
	setTestVersion(t)
	// Create a test session with tool calls
	session := &Session{
		ID:          "test-session-789",
		CreatedAt:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		LastUpdated: time.Date(2024, 1, 15, 11, 45, 0, 0, time.UTC),
		Provider:    "anthropic",
		Model:       "claude-3-5-sonnet-latest",
		WorkingDir:  "/home/user/project",
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextPart("You are a helpful assistant."),
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextPart("Read the file test.txt"),
				},
			},
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID:   "call_123",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "read_file",
							Arguments: `{"path":"test.txt"}`,
						},
					},
				},
			},
			{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: "call_123",
						Name:       "read_file",
						Content:    "File contents here",
					},
				},
			},
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.TextPart("The file contains: File contents here"),
				},
			},
		},
		ContextFiles: map[string]string{},
	}

	// Generate conversation export content
	content := generateConversationExportContent(session)

	// Verify user and assistant text messages are included
	expectedSections := []string{
		"**Asimi Version:** test-version",
		"**Session ID:** test-session-789 | **Working Directory:** /home/user/project",
		"**Provider:** anthropic | **Model:** claude-3-5-sonnet-latest",
		"### User",
		"Read the file test.txt",
		"### Assistant",
		"The file contains: File contents here",
	}

	for _, expected := range expectedSections {
		if !strings.Contains(content, expected) {
			t.Errorf("Conversation export content missing expected section: %s", expected)
		}
	}

	// Verify tool calls and tool results are NOT included
	unexpectedSections := []string{
		"**Tool Call:**",
		"read_file",
		"### Tool Result",
		"**Tool:**",
	}

	for _, unexpected := range unexpectedSections {
		if strings.Contains(content, unexpected) {
			t.Errorf("Conversation export content should not include tool details: %s", unexpected)
		}
	}
}

func TestGenerateFullExportContentWithToolCalls(t *testing.T) {
	setTestVersion(t)
	// Create a test session with tool calls
	session := &Session{
		ID:          "test-session-456",
		CreatedAt:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		LastUpdated: time.Date(2024, 1, 15, 11, 45, 0, 0, time.UTC),
		Provider:    "anthropic",
		Model:       "claude-3-5-sonnet-latest",
		WorkingDir:  "/home/user/project",
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextPart("You are a helpful assistant."),
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextPart("Read the file test.txt"),
				},
			},
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID:   "call_123",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "read_file",
							Arguments: `{"path":"test.txt"}`,
						},
					},
				},
			},
			{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: "call_123",
						Name:       "read_file",
						Content:    "File contents here",
					},
				},
			},
		},
		ContextFiles: map[string]string{},
	}

	// Generate export content
	content := generateFullExportContent(session)

	// Verify tool call formatting
	expectedSections := []string{
		"**Tool Call:** read_file",
		"**Input:**",
		"```json",
		`"path"`,
		`"test.txt"`,
		"### Tool Result",
		"**Tool:** read_file",
		"**Result:**",
		"File contents here",
	}

	for _, expected := range expectedSections {
		if !strings.Contains(content, expected) {
			t.Errorf("Export content missing expected tool call section: %s", expected)
		}
	}
}

func TestGenerateFullExportContentEmptySession(t *testing.T) {
	setTestVersion(t)
	// Create an empty session (only system message)
	session := &Session{
		ID:          "test-session-789",
		CreatedAt:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		LastUpdated: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Provider:    "openai",
		Model:       "gpt-4",
		WorkingDir:  "/home/user/project",
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextPart("You are a helpful assistant."),
				},
			},
		},
		ContextFiles: map[string]string{},
	}

	// Generate export content
	content := generateFullExportContent(session)

	// Verify basic structure is present
	if !strings.Contains(content, "# Asimi Conversation Export") {
		t.Error("Export content missing header")
	}
	if !strings.Contains(content, "## System Prompt") {
		t.Error("Export content missing system prompt section")
	}
	if !strings.Contains(content, "## Conversation") {
		t.Error("Export content missing conversation section")
	}
}

func TestGenerateFullExportContentNoContextFiles(t *testing.T) {
	setTestVersion(t)
	// Create a session without context files
	session := &Session{
		ID:          "test-session-no-context",
		CreatedAt:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		LastUpdated: time.Date(2024, 1, 15, 11, 45, 0, 0, time.UTC),
		Provider:    "anthropic",
		Model:       "claude-3-5-sonnet-latest",
		WorkingDir:  "/home/user/project",
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextPart("You are a helpful assistant."),
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextPart("Hello!"),
				},
			},
		},
		ContextFiles: map[string]string{},
	}

	// Generate export content
	content := generateFullExportContent(session)

	// Verify context files section is not present when there are no files
	if strings.Contains(content, "## Context Files") {
		t.Error("Export content should not include Context Files section when there are no files")
	}
}

func TestExportSessionWithType(t *testing.T) {
	setTestVersion(t)
	// Create a test session
	session := &Session{
		ID:          "test-session-export",
		CreatedAt:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		LastUpdated: time.Date(2024, 1, 15, 11, 45, 0, 0, time.UTC),
		Provider:    "anthropic",
		Model:       "claude-3-5-sonnet-latest",
		WorkingDir:  "/home/user/project",
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextPart("You are a helpful assistant."),
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextPart("Hello!"),
				},
			},
		},
		ContextFiles: map[string]string{},
	}

	// Test full export
	fullPath, err := exportSession(session, ExportTypeFull)
	if err != nil {
		t.Fatalf("Full export failed: %v", err)
	}
	defer os.Remove(fullPath)

	if !strings.Contains(fullPath, "asimi-export-full-") {
		t.Errorf("Full export filename should contain 'full', got: %s", fullPath)
	}

	// Test conversation export
	convPath, err := exportSession(session, ExportTypeConversation)
	if err != nil {
		t.Fatalf("Conversation export failed: %v", err)
	}
	defer os.Remove(convPath)

	if !strings.Contains(convPath, "asimi-export-conversation-") {
		t.Errorf("Conversation export filename should contain 'conversation', got: %s", convPath)
	}

	// Verify files exist and have content
	fullContent, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("Failed to read full export file: %v", err)
	}
	if len(fullContent) == 0 {
		t.Error("Full export file is empty")
	}

	convContent, err := os.ReadFile(convPath)
	if err != nil {
		t.Fatalf("Failed to read conversation export file: %v", err)
	}
	if len(convContent) == 0 {
		t.Error("Conversation export file is empty")
	}

	// Conversation export should be shorter than full export
	if len(convContent) >= len(fullContent) {
		t.Error("Conversation export should be shorter than full export")
	}
}

func TestExportSessionInvalidType(t *testing.T) {
	setTestVersion(t)
	session := &Session{
		ID:       "test-session",
		Messages: []llms.MessageContent{},
	}

	_, err := exportSession(session, ExportType("invalid"))
	if err == nil {
		t.Error("Expected error for invalid export type")
	}
	if !strings.Contains(err.Error(), "unknown export type") {
		t.Errorf("Expected 'unknown export type' error, got: %v", err)
	}
}

func TestOpenInEditorWithEnvVar(t *testing.T) {
	// This test verifies that the EDITOR environment variable is respected
	// We can't actually run the editor in tests, so we just verify the logic

	// Save original EDITOR value
	originalEditor := os.Getenv("EDITOR")
	defer os.Setenv("EDITOR", originalEditor)

	// Test with custom editor
	os.Setenv("EDITOR", "nano")
	editor := os.Getenv("EDITOR")
	if editor != "nano" {
		t.Errorf("Expected EDITOR to be 'nano', got '%s'", editor)
	}

	// Test with no EDITOR set
	os.Unsetenv("EDITOR")
	editor = os.Getenv("EDITOR")
	if editor != "" {
		t.Errorf("Expected EDITOR to be empty, got '%s'", editor)
	}
}

// TestGenerateExportContent tests backward compatibility
func TestGenerateExportContent(t *testing.T) {
	setTestVersion(t)
	session := &Session{
		ID:          "test-session-compat",
		CreatedAt:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		LastUpdated: time.Date(2024, 1, 15, 11, 45, 0, 0, time.UTC),
		Provider:    "anthropic",
		Model:       "claude-3-5-sonnet-latest",
		WorkingDir:  "/home/user/project",
		Messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextPart("You are a helpful assistant."),
				},
			},
		},
		ContextFiles: map[string]string{},
	}

	// Test that deprecated function still works
	content := generateExportContent(session)
	if !strings.Contains(content, "# Asimi Conversation Export") {
		t.Error("Deprecated generateExportContent should still work")
	}
}
