package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

// toolResult holds the content of a tool response for export formatting
type toolResult struct {
	Content string
}

// ExportType represents the type of export to generate
type ExportType string

const (
	ExportTypeFull         ExportType = "full"
	ExportTypeConversation ExportType = "conversation"
)

// ExportableSession is an interface for that can be exported.
// Implemented by shogunate.Session.
type ExportableSession interface {
	// GetID returns the session ID
	GetID() string
	// GetMessages returns the conversation messages
	GetMessages() []schemas.ChatMessage
	// GetContextFiles returns the context files map
	GetContextFiles() map[string]string
	// FormatMetadata returns formatted metadata for export
	FormatMetadata(exportType string, exportedAt string) string
}

// exportSession exports the current session to a markdown file and returns the filepath
func exportSession(session ExportableSession, exportType ExportType) (string, error) {
	if session == nil {
		return "", fmt.Errorf("no session to export")
	}

	// Generate export content based on type
	var content string
	switch exportType {
	case ExportTypeFull:
		content = generateFullExportContent(session)
	case ExportTypeConversation:
		content = generateConversationExportContent(session)
	default:
		return "", fmt.Errorf("unknown export type: %s", exportType)
	}

	// Create temporary file
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("asimi-export-%s-%s-%s.md", string(exportType), session.GetID(), timestamp)
	filepath := filepath.Join(os.TempDir(), filename)

	// Write content to file
	if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write export file: %w", err)
	}

	return filepath, nil
}

// generateFullExportContent generates the full markdown content for the export
// including system prompt, context files, and conversation
func generateFullExportContent(session ExportableSession) string {
	var b strings.Builder
	messages := session.GetMessages()
	contextFiles := session.GetContextFiles()

	// Header with full metadata in 4 lines
	b.WriteString("# Asimi Conversation Export\n\n")
	b.WriteString(session.FormatMetadata(string(ExportTypeFull), time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString("\n---\n\n")

	// System Prompt
	if len(messages) > 0 && messages[0].Role == schemas.ChatMessageRoleSystem {
		b.WriteString("## System Prompt\n\n")
		if messages[0].Content != nil && messages[0].Content.ContentStr != nil {
			b.WriteString(*messages[0].Content.ContentStr)
			b.WriteString("\n")
		}
		b.WriteString("\n---\n\n")
	}

	// Context Files
	if len(contextFiles) > 0 {
		b.WriteString("## Context Files\n\n")
		for path, content := range contextFiles {
			b.WriteString(fmt.Sprintf("### %s\n\n", path))
			b.WriteString("```\n")
			b.WriteString(content)
			b.WriteString("\n```\n\n")
		}
		b.WriteString("---\n\n")
	}

	// Conversation
	b.WriteString("## Conversation\n\n")

	// Skip system message (already shown above)
	startIdx := 0
	if len(messages) > 0 && messages[0].Role == schemas.ChatMessageRoleSystem {
		startIdx = 1
	}

	formatMessages(&b, messages[startIdx:], true, true) // true = full mode, true = include message numbers

	return b.String()
}

// generateConversationExportContent generates a slimmer export with just the conversation
// including tool calls but with limited output (no stdout)
func generateConversationExportContent(session ExportableSession) string {
	var b strings.Builder
	messages := session.GetMessages()

	// Minimal header
	b.WriteString("# Asimi Conversation\n\n")
	b.WriteString(session.FormatMetadata(string(ExportTypeConversation), time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString("\n---\n\n")

	// Skip system message
	startIdx := 0
	if len(messages) > 0 && messages[0].Role == schemas.ChatMessageRoleSystem {
		startIdx = 1
	}

	formatMessages(&b, messages[startIdx:], false, false) // false = conversation mode, false = no message numbers

	return b.String()
}

// formatMessages formats a slice of messages, pairing tool calls with their results
func formatMessages(b *strings.Builder, messages []schemas.ChatMessage, fullMode bool, includeMessageNumbers bool) {
	// Build a map of tool call IDs to their results for quick lookup
	toolResults := make(map[string]toolResult)
	for _, msg := range messages {
		if msg.Role == schemas.ChatMessageRoleTool {
			if msg.ChatToolMessage != nil && msg.ChatToolMessage.ToolCallID != nil {
				content := ""
				if msg.Content != nil && msg.Content.ContentStr != nil {
					content = *msg.Content.ContentStr
				}
				toolResults[*msg.ChatToolMessage.ToolCallID] = toolResult{Content: content}
			}
		}
	}

	messageNum := 1
	for _, msg := range messages {
		switch msg.Role {
		case schemas.ChatMessageRoleUser:
			if includeMessageNumbers {
				fmt.Fprintf(b, "### User (Message %d)\n\n", messageNum)
			} else {
				b.WriteString("### User\n\n")
			}
			if msg.Content != nil && msg.Content.ContentStr != nil {
				b.WriteString(*msg.Content.ContentStr)
				b.WriteString("\n\n")
			}
			messageNum++

		case schemas.ChatMessageRoleAssistant:
			if includeMessageNumbers {
				fmt.Fprintf(b, "### Assistant (Message %d)\n\n", messageNum)
			} else {
				b.WriteString("### Assistant\n\n")
			}
			// Text content
			if msg.Content != nil && msg.Content.ContentStr != nil {
				b.WriteString(*msg.Content.ContentStr)
				b.WriteString("\n\n")
			}
			// Tool calls
			if msg.ChatAssistantMessage != nil {
				for _, tc := range msg.ChatAssistantMessage.ToolCalls {
					formatToolCallWithResult(b, tc, toolResults, fullMode)
				}
			}
			messageNum++

		case schemas.ChatMessageRoleTool:
			// Tool results are handled inline with tool calls, skip standalone tool messages
		}
	}
}

// formatToolCallWithResult formats a tool call and its result together
func formatToolCallWithResult(b *strings.Builder, toolCall schemas.ChatAssistantMessageToolCall, toolResults map[string]toolResult, fullMode bool) {
	if toolCall.Function.Name == nil {
		return
	}

	fmt.Fprintf(b, "**Tool Call:** %s\n\n", *toolCall.Function.Name)
	b.WriteString("**Input:**\n```json\n")

	// Try to pretty-print JSON
	var jsonData interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &jsonData); err == nil {
		if prettyJSON, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
			b.WriteString(string(prettyJSON))
		} else {
			b.WriteString(toolCall.Function.Arguments)
		}
	} else {
		b.WriteString(toolCall.Function.Arguments)
	}

	b.WriteString("\n```\n")

	// Find and format the corresponding tool result
	if toolCall.ID != nil {
		if toolResp, ok := toolResults[*toolCall.ID]; ok {
			formatToolOutput(b, toolResp, fullMode)
		}
	}

	b.WriteString("\n")
}

// formatToolOutput formats the tool output based on mode
// In full mode: shows complete output
// In conversation mode: shows output if ≤128 chars, otherwise shows exit code and character count
func formatToolOutput(b *strings.Builder, toolResp toolResult, fullMode bool) {
	b.WriteString("**Output:**")

	// Try to parse as shell command JSON output
	var output map[string]interface{}
	if err := json.Unmarshal([]byte(toolResp.Content), &output); err == nil {
		if _, hasExitCode := output["exitCode"]; hasExitCode {
			exitCode := "0"
			if ec, ok := output["exitCode"].(string); ok {
				exitCode = ec
			}

			stdout := ""
			if s, ok := output["stdout"].(string); ok {
				stdout = s
			}

			stderr := ""
			if s, ok := output["stderr"].(string); ok {
				stderr = s
			}

			totalLength := len(stdout) + len(stderr)

			if fullMode || totalLength <= 128 {
				b.WriteString("\n```\n")
				fmt.Fprintf(b, "Exit Code: %s\n", exitCode)

				if stdout != "" {
					b.WriteString("\n")
					b.WriteString(stdout)
				}

				if stderr != "" {
					b.WriteString("\nStderr:\n")
					b.WriteString(stderr)
				}

				b.WriteString("\n```")
			} else {
				fmt.Fprintf(b, " Exit code %s, %d characters", exitCode, totalLength)
			}
			return
		}
	}

	// For other tools or non-JSON content
	if fullMode || len(toolResp.Content) <= 128 {
		b.WriteString("\n```\n")
		b.WriteString(toolResp.Content)
		b.WriteString("\n```")
	} else {
		fmt.Fprintf(b, " %d characters", len(toolResp.Content))
	}
}

// openInEditor creates a command to open the specified file in the user's preferred editor
func openInEditor(filepath string) *exec.Cmd {
	// Get editor from environment
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi" // Fallback to vi
	}

	// Create command
	cmd := exec.Command(editor, filepath)
	return cmd
}
