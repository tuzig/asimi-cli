package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// ExportType represents the type of export to generate
type ExportType string

const (
	ExportTypeFull         ExportType = "full"
	ExportTypeConversation ExportType = "conversation"
)

// exportSession exports the current session to a markdown file and returns the filepath
func exportSession(session *Session, exportType ExportType) (string, error) {
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
	filename := fmt.Sprintf("asimi-export-%s-%s-%s.md", string(exportType), session.ID, timestamp)
	filepath := filepath.Join(os.TempDir(), filename)

	// Write content to file
	if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write export file: %w", err)
	}

	return filepath, nil
}

// generateFullExportContent generates the full markdown content for the export
// including system prompt, context files, and conversation
func generateFullExportContent(session *Session) string {
	var b strings.Builder

	// Header with full metadata in 4 lines
	b.WriteString("# Asimi Conversation Export\n\n")
	b.WriteString(session.formatMetadata(ExportTypeFull, time.Now()))
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

	// Context Files
	if len(session.ContextFiles) > 0 {
		b.WriteString("## Context Files\n\n")
		for path, content := range session.ContextFiles {
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
	if len(session.Messages) > 0 && session.Messages[0].Role == llms.ChatMessageTypeSystem {
		startIdx = 1
	}

	for i := startIdx; i < len(session.Messages); i++ {
		msg := session.Messages[i]
		formatMessage(&b, msg, i)
	}

	return b.String()
}

// generateConversationExportContent generates a slimmer export with just the conversation
// excluding system prompt, context files, and tool calls
func generateConversationExportContent(session *Session) string {
	var b strings.Builder

	// Minimal header
	b.WriteString("# Asimi Conversation\n\n")
	b.WriteString(session.formatMetadata(ExportTypeConversation, time.Now()))
	b.WriteString("\n---\n\n")

	// Skip system message and only show user/assistant exchanges
	for i := 0; i < len(session.Messages); i++ {
		msg := session.Messages[i]

		// Skip system and tool messages
		if msg.Role == llms.ChatMessageTypeSystem || msg.Role == llms.ChatMessageTypeTool {
			continue
		}

		formatConversationMessage(&b, msg)
	}

	return b.String()
}

// formatMessage formats a single message for full export
func formatMessage(b *strings.Builder, msg llms.MessageContent, index int) {
	switch msg.Role {
	case llms.ChatMessageTypeHuman:
		b.WriteString(fmt.Sprintf("### User (Message %d)\n\n", index))
		for _, part := range msg.Parts {
			if textPart, ok := part.(llms.TextContent); ok {
				b.WriteString(textPart.Text)
				b.WriteString("\n\n")
			}
		}

	case llms.ChatMessageTypeAI:
		b.WriteString(fmt.Sprintf("### Assistant (Message %d)\n\n", index))
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llms.TextContent:
				b.WriteString(p.Text)
				b.WriteString("\n\n")
			case llms.ToolCall:
				formatToolCallExport(b, p)
			}
		}

	case llms.ChatMessageTypeTool:
		b.WriteString(fmt.Sprintf("### Tool Result (Message %d)\n\n", index))
		for _, part := range msg.Parts {
			if toolResp, ok := part.(llms.ToolCallResponse); ok {
				b.WriteString(fmt.Sprintf("**Tool:** %s\n\n", toolResp.Name))
				b.WriteString("**Result:**\n\n")
				b.WriteString("```\n")
				b.WriteString(toolResp.Content)
				b.WriteString("\n```\n\n")
			}
		}
	}
}

// formatConversationMessage formats a message for conversation-only export
func formatConversationMessage(b *strings.Builder, msg llms.MessageContent) {
	switch msg.Role {
	case llms.ChatMessageTypeHuman:
		b.WriteString("### User\n\n")
		for _, part := range msg.Parts {
			if textPart, ok := part.(llms.TextContent); ok {
				b.WriteString(textPart.Text)
				b.WriteString("\n\n")
			}
		}

	case llms.ChatMessageTypeAI:
		b.WriteString("### Assistant\n\n")
		// Only include text content, skip tool calls
		for _, part := range msg.Parts {
			if textPart, ok := part.(llms.TextContent); ok {
				b.WriteString(textPart.Text)
				b.WriteString("\n\n")
			}
		}
	}
}

// formatToolCallExport formats a tool call for export
func formatToolCallExport(b *strings.Builder, toolCall llms.ToolCall) {
	if toolCall.FunctionCall == nil {
		return
	}

	b.WriteString(fmt.Sprintf("**Tool Call:** %s\n\n", toolCall.FunctionCall.Name))
	b.WriteString("**Input:**\n\n")
	b.WriteString("```json\n")

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

	b.WriteString("\n```\n\n")
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

// Deprecated: use generateFullExportContent instead
// generateExportContent is kept for backward compatibility
func generateExportContent(session *Session) string {
	return generateFullExportContent(session)
}
