package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

func TestExportShowsToolCalls(t *testing.T) {
	// Create a test session with tool calls
	session := &Session{
		ID:          "test-session",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Test prompt",
		Provider:    "test",
		Model:       "test-model",
		WorkingDir:  "/test",
		Messages: []llms.MessageContent{
			// System message
			{
				Role:  llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{llms.TextPart("System prompt")},
			},
			// User message
			{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextPart("Run a test command")},
			},
			// Assistant message with tool call
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.TextPart("I'll run that command for you."),
					llms.ToolCall{
						ID:   "call_123",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "run_shell_command",
							Arguments: `{"command":"echo 'test output'","description":"Test command"}`,
						},
					},
				},
			},
			// Tool result
			{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: "call_123",
						Name:       "run_shell_command",
						Content:    `{"stdout":"test output\n","stderr":"","exitCode":"0"}`,
					},
				},
			},
		},
		ContextFiles: make(map[string]string),
	}

	t.Run("Full export includes tool calls with stdout", func(t *testing.T) {
		content := generateFullExportContent(session)

		// Check that tool call is present
		if !strings.Contains(content, "**Tool Call:** run_shell_command") {
			t.Error("Full export should contain tool call")
		}

		// Check that tool input is present
		if !strings.Contains(content, "echo 'test output'") {
			t.Error("Full export should contain tool call input")
		}

		// Check that tool output is present
		if !strings.Contains(content, "**Output:**") {
			t.Error("Full export should contain output section")
		}

		// Check that exit code is present
		if !strings.Contains(content, "Exit Code: 0") {
			t.Error("Full export should contain exit code")
		}

		// Check that stdout is present in full mode
		if !strings.Contains(content, "test output") {
			t.Error("Full export should contain stdout content")
		}
	})

	t.Run("Conversation export includes tool calls without stdout", func(t *testing.T) {
		content := generateConversationExportContent(session)

		// Check that tool call is present
		if !strings.Contains(content, "**Tool Call:** run_shell_command") {
			t.Error("Conversation export should contain tool call")
		}

		// Check that tool input is present
		if !strings.Contains(content, "echo 'test output'") {
			t.Error("Conversation export should contain tool call input")
		}

		// Check that tool output is present
		if !strings.Contains(content, "**Output:**") {
			t.Error("Conversation export should contain output section")
		}

		// Check that exit code is present
		if !strings.Contains(content, "Exit Code: 0") {
			t.Error("Conversation export should contain exit code")
		}

		// In conversation mode with short output (≤128 chars), stdout is still shown
		// This is expected behavior - only long output is truncated
		if !strings.Contains(content, "test output") {
			t.Error("Conversation export should contain short stdout content")
		}
	})

	t.Run("Conversation export does not skip tool messages", func(t *testing.T) {
		content := generateConversationExportContent(session)

		// Check that tool call is present
		if !strings.Contains(content, "**Tool Call:**") {
			t.Error("Conversation export should include tool calls")
		}

		// Check that tool output is present
		if !strings.Contains(content, "**Output:**") {
			t.Error("Conversation export should include tool outputs")
		}

		// Check that the tool was actually executed (exit code present)
		if !strings.Contains(content, "Exit Code:") {
			t.Error("Conversation export should include tool execution results")
		}
	})
}

func TestFormatMessagesNumberingSkipsToolMessages(t *testing.T) {
	var b strings.Builder
	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Hello")},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.TextPart("Running a command"),
				llms.ToolCall{
					ID:   "call_123",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      "run_shell_command",
						Arguments: `{"command":"echo test"}`,
					},
				},
			},
		},
		{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{
					ToolCallID: "call_123",
					Name:       "run_shell_command",
					Content:    `{"stdout":"test","stderr":"","exitCode":"0"}`,
				},
			},
		},
	}

	formatMessages(&b, messages, true, true)
	output := b.String()

	if !strings.Contains(output, "### User (Message 1)") {
		t.Fatalf("expected first heading to be User (Message 1), got:\n%s", output)
	}
	if !strings.Contains(output, "### Assistant (Message 2)") {
		t.Fatalf("expected Assistant heading to be Message 2, got:\n%s", output)
	}
	if strings.Contains(output, "Message 3") {
		t.Fatalf("numbering should not skip due to hidden tool messages:\n%s", output)
	}
}

func TestExportToolResultWithStderr(t *testing.T) {
	// Create a test session with a command that has stderr
	session := &Session{
		ID:          "test-session",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Test prompt",
		Provider:    "test",
		Model:       "test-model",
		WorkingDir:  "/test",
		Messages: []llms.MessageContent{
			// System message
			{
				Role:  llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{llms.TextPart("System prompt")},
			},
			// User message
			{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextPart("Run a command with error")},
			},
			// Assistant message with tool call
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID:   "call_456",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "run_shell_command",
							Arguments: `{"command":"ls /nonexistent","description":"Test error"}`,
						},
					},
				},
			},
			// Tool result with error
			{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: "call_456",
						Name:       "run_shell_command",
						Content:    `{"stdout":"","stderr":"ls: cannot access '/nonexistent': No such file or directory\n","exitCode":"2"}`,
					},
				},
			},
		},
		ContextFiles: make(map[string]string),
	}

	t.Run("Full export shows stderr", func(t *testing.T) {
		content := generateFullExportContent(session)

		// Check that stderr is present
		if !strings.Contains(content, "Stderr:") {
			t.Error("Full export should contain stderr section")
		}
		if !strings.Contains(content, "No such file or directory") {
			t.Error("Full export should contain stderr content")
		}

		// Check exit code
		if !strings.Contains(content, "Exit Code: 2") {
			t.Error("Full export should contain non-zero exit code")
		}
	})

	t.Run("Conversation export shows stderr but not stdout", func(t *testing.T) {
		content := generateConversationExportContent(session)

		// Check that stderr is present
		if !strings.Contains(content, "Stderr:") {
			t.Error("Conversation export should contain stderr section")
		}
		if !strings.Contains(content, "No such file or directory") {
			t.Error("Conversation export should contain stderr content")
		}

		// Check that stdout section is not present
		if strings.Contains(content, "Stdout:") {
			t.Error("Conversation export should NOT contain stdout section")
		}

		// Check exit code
		if !strings.Contains(content, "Exit Code: 2") {
			t.Error("Conversation export should contain non-zero exit code")
		}
	})
}

func TestExportNonShellToolCalls(t *testing.T) {
	// Create a test session with non-shell tool calls
	session := &Session{
		ID:          "test-session",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Test prompt",
		Provider:    "test",
		Model:       "test-model",
		WorkingDir:  "/test",
		Messages: []llms.MessageContent{
			// System message
			{
				Role:  llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{llms.TextPart("System prompt")},
			},
			// User message
			{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextPart("Read a file")},
			},
			// Assistant message with tool call
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID:   "call_789",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "read_file",
							Arguments: `{"path":"test.txt"}`,
						},
					},
				},
			},
			// Tool result
			{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: "call_789",
						Name:       "read_file",
						Content:    "File content here",
					},
				},
			},
		},
		ContextFiles: make(map[string]string),
	}

	t.Run("Non-shell tools show full result in both modes", func(t *testing.T) {
		fullContent := generateFullExportContent(session)
		convContent := generateConversationExportContent(session)

		// Both should contain the tool call
		if !strings.Contains(fullContent, "**Tool Call:** read_file") {
			t.Error("Full export should contain read_file tool call")
		}
		if !strings.Contains(convContent, "**Tool Call:** read_file") {
			t.Error("Conversation export should contain read_file tool call")
		}

		// Both should contain the full result (not shell-specific)
		if !strings.Contains(fullContent, "File content here") {
			t.Error("Full export should contain file content")
		}
		if !strings.Contains(convContent, "File content here") {
			t.Error("Conversation export should contain file content")
		}
	})
}

func TestFormatToolOutput(t *testing.T) {
	t.Run("Shell command with stdout in full mode", func(t *testing.T) {
		var b strings.Builder
		toolResp := llms.ToolCallResponse{
			Name:    "run_shell_command",
			Content: `{"stdout":"output line 1\noutput line 2","stderr":"","exitCode":"0"}`,
		}

		formatToolOutput(&b, toolResp, true)
		result := b.String()

		if !strings.Contains(result, "Exit Code: 0") {
			t.Error("Should contain exit code")
		}
		if !strings.Contains(result, "output line 1") {
			t.Error("Should contain stdout content")
		}
	})

	t.Run("Shell command with long output in conversation mode", func(t *testing.T) {
		var b strings.Builder
		// Create output longer than 128 characters
		longOutput := strings.Repeat("x", 150)
		toolResp := llms.ToolCallResponse{
			Name:    "run_shell_command",
			Content: fmt.Sprintf(`{"stdout":"%s","stderr":"","exitCode":"0"}`, longOutput),
		}

		formatToolOutput(&b, toolResp, false)
		result := b.String()

		if !strings.Contains(result, "Exit code 0") {
			t.Error("Should contain exit code")
		}
		if !strings.Contains(result, "150 characters") {
			t.Error("Conversation mode should show character count for long output")
		}
		if strings.Contains(result, longOutput) {
			t.Error("Conversation mode should NOT contain full output for long content")
		}
	})

	t.Run("Shell command with short output in conversation mode", func(t *testing.T) {
		var b strings.Builder
		toolResp := llms.ToolCallResponse{
			Name:    "run_shell_command",
			Content: `{"stdout":"short","stderr":"","exitCode":"0"}`,
		}

		formatToolOutput(&b, toolResp, false)
		result := b.String()

		if !strings.Contains(result, "Exit Code: 0") {
			t.Error("Should contain exit code")
		}
		if !strings.Contains(result, "short") {
			t.Error("Conversation mode should show short output")
		}
	})

	t.Run("Shell command with stderr", func(t *testing.T) {
		var b strings.Builder
		toolResp := llms.ToolCallResponse{
			Name:    "run_shell_command",
			Content: `{"stdout":"","stderr":"error message","exitCode":"1"}`,
		}

		formatToolOutput(&b, toolResp, false)
		result := b.String()

		if !strings.Contains(result, "Exit Code: 1") {
			t.Error("Should contain exit code")
		}
		if !strings.Contains(result, "Stderr:") {
			t.Error("Should contain stderr section")
		}
		if !strings.Contains(result, "error message") {
			t.Error("Should contain stderr content")
		}
	})

	t.Run("Non-JSON tool result", func(t *testing.T) {
		var b strings.Builder
		toolResp := llms.ToolCallResponse{
			Name:    "read_file",
			Content: "Plain text file content",
		}

		formatToolOutput(&b, toolResp, false)
		result := b.String()

		if !strings.Contains(result, "Plain text file content") {
			t.Error("Should contain raw content for non-JSON results")
		}
	})
}

func TestFormatToolCallWithResult(t *testing.T) {
	t.Run("Valid JSON arguments", func(t *testing.T) {
		var b strings.Builder
		toolCall := llms.ToolCall{
			ID:   "call_123",
			Type: "function",
			FunctionCall: &llms.FunctionCall{
				Name:      "run_shell_command",
				Arguments: `{"command":"echo test","description":"Test"}`,
			},
		}
		toolResults := make(map[string]llms.ToolCallResponse)

		formatToolCallWithResult(&b, toolCall, toolResults, true)
		result := b.String()

		if !strings.Contains(result, "**Tool Call:** run_shell_command") {
			t.Error("Should contain tool name")
		}
		if !strings.Contains(result, "**Input:**") {
			t.Error("Should contain input section")
		}
		// Should be pretty-printed JSON
		if !strings.Contains(result, "\"command\"") {
			t.Error("Should contain formatted JSON")
		}
	})

	t.Run("Invalid JSON arguments", func(t *testing.T) {
		var b strings.Builder
		toolCall := llms.ToolCall{
			ID:   "call_456",
			Type: "function",
			FunctionCall: &llms.FunctionCall{
				Name:      "test_tool",
				Arguments: `not valid json`,
			},
		}
		toolResults := make(map[string]llms.ToolCallResponse)

		formatToolCallWithResult(&b, toolCall, toolResults, true)
		result := b.String()

		if !strings.Contains(result, "not valid json") {
			t.Error("Should contain raw arguments when JSON parsing fails")
		}
	})
}

func TestExportMetadata(t *testing.T) {
	session := &Session{
		ID:          "test-123",
		CreatedAt:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		LastUpdated: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC),
		FirstPrompt: "Test prompt",
		Provider:    "anthropic",
		Model:       "claude-3-5-sonnet",
		WorkingDir:  "/home/user/project",
		ProjectSlug: "user/project",
		Messages: []llms.MessageContent{
			{
				Role:  llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{llms.TextPart("System")},
			},
		},
		ContextFiles: make(map[string]string),
	}

	t.Run("Full export includes metadata", func(t *testing.T) {
		content := generateFullExportContent(session)

		if !strings.Contains(content, "**Session ID:** test-123") {
			t.Error("Should contain session ID")
		}
		if !strings.Contains(content, "**Provider:** anthropic") {
			t.Error("Should contain provider")
		}
		if !strings.Contains(content, "**Model:** claude-3-5-sonnet") {
			t.Error("Should contain model")
		}
		if !strings.Contains(content, "**Working Directory:** /home/user/project") {
			t.Error("Should contain working directory")
		}
	})

	t.Run("Conversation export includes metadata", func(t *testing.T) {
		content := generateConversationExportContent(session)

		if !strings.Contains(content, "**Session ID:** test-123") {
			t.Error("Should contain session ID")
		}
		if !strings.Contains(content, "**Provider:** anthropic") {
			t.Error("Should contain provider")
		}
	})
}
