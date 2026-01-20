package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/afittestide/asimi/shogunate"
	"github.com/tmc/langchaingo/llms"
)

// Export type constants
const (
	ExportTypeFull         = "full"
	ExportTypeConversation = "conversation"
)

// exportSession exports the current session to a markdown file and returns the filepath
func exportSession(session *shogunate.Session, exportType string) (string, error) {
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
	filename := fmt.Sprintf("asimi-export-%s-%s-%s.md", exportType, session.ID, timestamp)
	filepath := filepath.Join(os.TempDir(), filename)

	// Write content to file
	if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write export file: %w", err)
	}

	return filepath, nil
}

// formatSessionMetadata formats session metadata for export
func formatSessionMetadata(session *shogunate.Session, exportType string, exportedAt time.Time) string {
	var b strings.Builder
	exported := exportedAt.Format("2006-01-02 15:04:05")

	b.WriteString(fmt.Sprintf("**Asimi Version:** %s \n", version))
	b.WriteString(fmt.Sprintf("**Export Type:** %s\n", exportType))
	b.WriteString(fmt.Sprintf("**Session ID:** %s | **Working Directory:** %s\n", session.ID, session.WorkingDir))
	b.WriteString(fmt.Sprintf("**Provider:** %s | **Model:** %s\n", session.Provider, session.Model))
	b.WriteString(fmt.Sprintf("**Created:** %s | **Last Updated:** %s | **Exported:** %s\n",
		session.CreatedAt.Format("2006-01-02 15:04:05"),
		session.LastUpdated.Format("2006-01-02 15:04:05"),
		exported))
	if session.ProjectSlug != "" {
		b.WriteString(fmt.Sprintf("**Project:** %s\n", session.ProjectSlug))
	}

	return b.String()
}

// generateFullExportContent generates the full markdown content for the export
// including system prompt, context files, and conversation
func generateFullExportContent(session *shogunate.Session) string {
	var b strings.Builder

	// Header with full metadata in 4 lines
	b.WriteString("# Asimi Conversation Export\n\n")
	b.WriteString(formatSessionMetadata(session, ExportTypeFull, time.Now()))
	b.WriteString("\n---\n\n")

	// System Prompt
	if len(session.Messages) > 0 && session.Messages[0].Role == llms.ChatMessageTypeSystem {
		b.WriteString("## System Prompt\n\n")
		for _, part := range session.Messages[0].Parts {
			if textPart, ok := part.(llms.TextContent); ok {
				b.WriteString(textPart.Text)
				b.WriteString("\n")
			}
		}
		b.WriteString("\n---\n\n")
	}

	// Conversation
	b.WriteString("## Conversation\n\n")

	// Skip system message (already shown above)
	startIdx := 0
	if len(session.Messages) > 0 && session.Messages[0].Role == llms.ChatMessageTypeSystem {
		startIdx = 1
	}

	formatMessages(&b, session.Messages[startIdx:], true, true) // true = full mode, true = include message numbers

	return b.String()
}

// generateConversationExportContent generates a slimmer export with just the conversation
// including tool calls but with limited output (no stdout)
func generateConversationExportContent(session *shogunate.Session) string {
	var b strings.Builder

	// Minimal header
	b.WriteString("# Asimi Conversation\n\n")
	b.WriteString(formatSessionMetadata(session, ExportTypeConversation, time.Now()))
	b.WriteString("\n---\n\n")

	// Skip system message
	startIdx := 0
	if len(session.Messages) > 0 && session.Messages[0].Role == llms.ChatMessageTypeSystem {
		startIdx = 1
	}

	formatMessages(&b, session.Messages[startIdx:], false, false) // false = conversation mode, false = no message numbers

	return b.String()
}

// formatMessages formats a slice of messages, pairing tool calls with their results
func formatMessages(b *strings.Builder, messages []llms.MessageContent, fullMode bool, includeMessageNumbers bool) {
	// Build a map of tool call IDs to their results for quick lookup
	toolResults := make(map[string]llms.ToolCallResponse)
	for _, msg := range messages {
		if msg.Role == llms.ChatMessageTypeTool {
			for _, part := range msg.Parts {
				if toolResp, ok := part.(llms.ToolCallResponse); ok {
					toolResults[toolResp.ToolCallID] = toolResp
				}
			}
		}
	}

	messageNum := 1
	for _, msg := range messages {
		switch msg.Role {
		case llms.ChatMessageTypeHuman:
			if includeMessageNumbers {
				fmt.Fprintf(b, "### User (Message %d)\n\n", messageNum)
			} else {
				b.WriteString("### User\n\n")
			}
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					b.WriteString(textPart.Text)
					b.WriteString("\n\n")
				}
			}
			messageNum++

		case llms.ChatMessageTypeAI:
			if includeMessageNumbers {
				fmt.Fprintf(b, "### Assistant (Message %d)\n\n", messageNum)
			} else {
				b.WriteString("### Assistant\n\n")
			}
			for _, part := range msg.Parts {
				switch p := part.(type) {
				case llms.TextContent:
					b.WriteString(p.Text)
					b.WriteString("\n\n")
				case llms.ToolCall:
					formatToolCallWithResult(b, p, toolResults, fullMode)
				}
			}
			messageNum++

		case llms.ChatMessageTypeTool:
			// Tool results are handled inline with tool calls, skip standalone tool messages
		}
	}
}

// formatToolCallWithResult formats a tool call and its result together
func formatToolCallWithResult(b *strings.Builder, toolCall llms.ToolCall, toolResults map[string]llms.ToolCallResponse, fullMode bool) {
	if toolCall.FunctionCall == nil {
		return
	}

	fmt.Fprintf(b, "**Tool Call:** %s\n\n", toolCall.FunctionCall.Name)
	b.WriteString("**Input:**\n```json\n")

	// Try to pretty-print JSON
	var jsonData interface{}
	if err := json.Unmarshal([]byte(toolCall.FunctionCall.Arguments), &jsonData); err == nil {
		if prettyJSON, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
			b.WriteString(string(prettyJSON))
		} else {
			b.WriteString(toolCall.FunctionCall.Arguments)
		}
	} else {
		b.WriteString(toolCall.FunctionCall.Arguments)
	}

	b.WriteString("\n```\n")

	// Find and format the corresponding tool result
	if toolResp, ok := toolResults[toolCall.ID]; ok {
		formatToolOutput(b, toolResp, fullMode)
	}

	b.WriteString("\n")
}

// formatToolOutput formats the tool output based on mode
// In full mode: shows complete output
// In conversation mode: shows output if ≤128 chars, otherwise shows exit code and character count
func formatToolOutput(b *strings.Builder, toolResp llms.ToolCallResponse, fullMode bool) {
	b.WriteString("**Output:**")

	// For run_shell_command, parse the JSON output and format accordingly
	if toolResp.Name == "run_shell_command" {
		var output map[string]interface{}
		if err := json.Unmarshal([]byte(toolResp.Content), &output); err == nil {
			// Successfully parsed as JSON - format the shell output
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

			// Show full output if in full mode OR if output is short (≤128 chars)
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
				// Conversation mode with long output: show only exit code and character count
				fmt.Fprintf(b, " Exit code %s, %d characters", exitCode, totalLength)
			}
		} else {
			// Not JSON or parsing failed - show raw content
			if fullMode || len(toolResp.Content) <= 128 {
				b.WriteString("\n```\n")
				b.WriteString(toolResp.Content)
				b.WriteString("\n```")
			} else {
				fmt.Fprintf(b, " %d characters", len(toolResp.Content))
			}
		}
	} else {
		// For other tools
		if fullMode || len(toolResp.Content) <= 128 {
			b.WriteString("\n```\n")
			b.WriteString(toolResp.Content)
			b.WriteString("\n```")
		} else {
			fmt.Fprintf(b, " %d characters", len(toolResp.Content))
		}
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
