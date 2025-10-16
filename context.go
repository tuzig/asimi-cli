// Package main implements the /context command for displaying context usage information.
// This implementation leverages langchaingo's model database and token counting capabilities
// for improved accuracy, particularly for OpenAI models.

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

const (
	contextBarWidth          = 10
	autocompactBufferRatio   = 0.225
	memoryFileOverheadTokens = 20
	defaultUnknownContextRef = 8192
)

// extendedModelContextSizes contains context sizes for models not covered by langchaingo.
// langchaingo already covers OpenAI models comprehensively, so we only need to maintain
// Anthropic and Google models here.
var extendedModelContextSizes = map[string]int{
	// Anthropic Claude models (not in langchaingo)
	"claude-3-5-sonnet-latest":   200_000,
	"claude-3-5-sonnet":          200_000,
	"claude-3-opus-20240229":     200_000,
	"claude-3-sonnet-20240229":   200_000,
	"claude-3-5-haiku-latest":    200_000,
	"claude-3-haiku-20240307":    200_000,
	"claude-sonnet-4-5-20250929": 200_000,

	// Google Gemini models (not in langchaingo)
	"gemini-1.5-flash":        1_000_000,
	"gemini-1.5-flash-latest": 1_000_000,
	"gemini-1.5-pro":          2_000_000,
	"gemini-1.5-pro-latest":   2_000_000,
	"gemini-pro":              1_000_000,
	"gemini-2.0-flash":        1_000_000,
}

// ContextInfo holds information about context usage.
type ContextInfo struct {
	Model              string
	TotalTokens        int
	UsedTokens         int
	SystemPromptTokens int
	SystemToolsTokens  int
	MemoryFilesTokens  int
	MessagesTokens     int
	FreeTokens         int
	AutocompactBuffer  int
}

// GetContextInfo returns detailed information about context usage.
func (s *Session) GetContextInfo() ContextInfo {
	info := ContextInfo{
		Model:       s.getModelName(),
		TotalTokens: s.getModelContextSize(),
	}

	info.SystemPromptTokens = s.CountSystemPromptTokens()
	info.SystemToolsTokens = s.CountSystemToolsTokens()
	info.MemoryFilesTokens = s.CountMemoryFilesTokens()
	info.MessagesTokens = s.CountMessagesTokens()
	info.UsedTokens = info.SystemPromptTokens + info.SystemToolsTokens + info.MemoryFilesTokens + info.MessagesTokens

	buffer := int(math.Round(float64(info.TotalTokens) * autocompactBufferRatio))
	maxBuffer := info.TotalTokens - info.UsedTokens
	if maxBuffer < 0 {
		maxBuffer = 0
	}
	if buffer > maxBuffer {
		buffer = maxBuffer
	}
	info.AutocompactBuffer = buffer

	free := info.TotalTokens - info.UsedTokens - info.AutocompactBuffer
	if free < 0 {
		free = 0
	}
	info.FreeTokens = free

	return info
}

// getModelName returns the configured model name when available, falling back to provider defaults.
func (s *Session) getModelName() string {
	if s.config != nil && s.config.Model != "" {
		return s.config.Model
	}
	// TODO: Add a log Info message
	return "Unknown"

}

// getModelContextSize returns the context window size for the current model.
// First checks langchaingo's database (covers OpenAI models), then falls back to our extended list.
func (s *Session) getModelContextSize() int {
	modelName := s.getModelName()

	// First, try langchaingo's database (covers OpenAI models comprehensively)
	if size := llms.GetModelContextSize(modelName); size > 2048 { // 2048 is langchaingo's default for unknown models
		return size
	}

	// Fall back to our extended database for non-OpenAI models
	if size, ok := extendedModelContextSizes[strings.ToLower(modelName)]; ok && size > 0 {
		return size
	}

	// Provider-based fallbacks
	if s.config != nil {
		switch strings.ToLower(s.config.Provider) {
		case "anthropic":
			return 200_000
		case "openai":
			return 128_000 // Modern OpenAI default
		case "googleai":
			return 1_000_000
		}
	}

	return defaultUnknownContextRef
}

// CountSystemPromptTokens counts tokens in the system prompt.
// This includes the base system prompt template (AGENTS.md is now in Memory files).
func (s *Session) CountSystemPromptTokens() int {
	if len(s.messages) == 0 {
		return 0
	}

	if s.messages[0].Role != llms.ChatMessageTypeSystem {
		return 0
	}

	var content strings.Builder
	for _, part := range s.messages[0].Parts {
		if textPart, ok := part.(llms.TextContent); ok {
			content.WriteString(textPart.Text)
		}
	}

	return s.countTokens(content.String())
}

// CountSystemToolsTokens counts tokens in tool definitions.
func (s *Session) CountSystemToolsTokens() int {
	if len(s.toolDefs) == 0 {
		return 0
	}

	toolsJSON, err := json.Marshal(s.toolDefs)
	if err != nil {
		return 0
	}

	return s.countTokens(string(toolsJSON))
}

// CountMemoryFilesTokens counts tokens in context files.
// This includes AGENTS.md and any files dynamically added via AddContextFile().
func (s *Session) CountMemoryFilesTokens() int {
	if len(s.contextFiles) == 0 {
		return 0
	}

	totalTokens := 0
	for path, content := range s.contextFiles {
		totalTokens += s.countTokens(path)
		totalTokens += s.countTokens(content)
		totalTokens += memoryFileOverheadTokens
	}

	return totalTokens
}

// CountMessagesTokens counts tokens in conversation history (excluding the system message).
func (s *Session) CountMessagesTokens() int {
	if len(s.messages) <= 1 {
		return 0
	}

	totalTokens := 0
	for i := 1; i < len(s.messages); i++ {
		msg := s.messages[i]
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llms.TextContent:
				totalTokens += s.countTokens(p.Text)
			case llms.ToolCall:
				if p.FunctionCall != nil {
					totalTokens += s.countTokens(p.FunctionCall.Name)
					totalTokens += s.countTokens(p.FunctionCall.Arguments)
				}
			case llms.ToolCallResponse:
				totalTokens += s.countTokens(p.Name)
				totalTokens += s.countTokens(p.Content)
			}
		}
	}

	return totalTokens
}

// countTokens provides token counting with langchaingo for OpenAI models,
// falling back to estimation for other providers.
func (s *Session) countTokens(text string) int {
	if text == "" {
		return 0
	}

	modelName := s.getModelName()

	return llms.CountTokens(modelName, text)
}

// renderContextInfo renders the context information as a formatted string.
func renderContextInfo(info ContextInfo) string {
	var b strings.Builder
	total := info.TotalTokens
	if total <= 0 {
		total = info.UsedTokens + info.AutocompactBuffer + info.FreeTokens
	}
	if total <= 0 {
		total = 1
	}

	usedPercent := percentage(clampInt(info.UsedTokens, 0, total), total)
	systemPromptPercent := percentage(info.SystemPromptTokens, total)
	systemToolsPercent := percentage(info.SystemToolsTokens, total)
	memoryFilesPercent := percentage(info.MemoryFilesTokens, total)
	messagesPercent := percentage(info.MessagesTokens, total)
	freePercent := percentage(info.FreeTokens, total)
	autocompactPercent := percentage(info.AutocompactBuffer, total)

	b.WriteString("  ⎿  Context Usage\n")
	b.WriteString(fmt.Sprintf("     %s   %s · %s/%s tokens (%.1f%%)\n",
		renderContextBar(info),
		info.Model,
		formatTokenCount(info.UsedTokens),
		formatTokenCount(info.TotalTokens),
		usedPercent,
	))

	b.WriteString(formatContextLine("System prompt", info.SystemPromptTokens, total, "⛁", systemPromptPercent))
	b.WriteString(formatContextLine("System tools", info.SystemToolsTokens, total, "⛁", systemToolsPercent))
	b.WriteString(formatContextLine("Memory files", info.MemoryFilesTokens, total, "⛁", memoryFilesPercent))
	b.WriteString(formatContextLine("Messages", info.MessagesTokens, total, "⛁", messagesPercent))
	b.WriteString(formatContextLine("Free space", info.FreeTokens, total, "⛶", freePercent))
	b.WriteString(formatContextLine("Autocompact buffer", info.AutocompactBuffer, total, "⛝", autocompactPercent))

	return b.String()
}

// renderContextBar creates a visual bar representation of context usage.
func renderContextBar(info ContextInfo) string {
	total := info.TotalTokens
	if total <= 0 {
		total = info.UsedTokens + info.AutocompactBuffer + info.FreeTokens
	}
	if total <= 0 {
		total = 1
	}

	usedTokens := clampInt(info.UsedTokens, 0, total)
	bufferTokens := clampInt(info.AutocompactBuffer, 0, total-usedTokens)
	freeTokens := total - usedTokens - bufferTokens
	if freeTokens < 0 {
		freeTokens = 0
	}

	segments := make([]string, 0, contextBarWidth)
	remaining := contextBarWidth

	addSegments := func(tokens int, fill, partial string) {
		if remaining == 0 || tokens <= 0 {
			return
		}
		percentage := float64(tokens) / float64(total) * 100
		fullSegments, partialSegment := calculateBarSegments(percentage)
		if fullSegments > remaining {
			fullSegments = remaining
			partialSegment = false
		}
		for i := 0; i < fullSegments && remaining > 0; i++ {
			segments = append(segments, fill)
			remaining--
		}
		if partialSegment && remaining > 0 {
			if partial == "" {
				partial = fill
			}
			segments = append(segments, partial)
			remaining--
		}
	}

	addSegments(usedTokens, "⛁", "⛀")
	addSegments(freeTokens, "⛶", "")
	addSegments(bufferTokens, "⛝", "")

	for len(segments) < contextBarWidth {
		segments = append(segments, "⛶")
	}

	return strings.Join(segments, " ")
}

// formatContextLine builds a formatted line for a specific category.
func formatContextLine(label string, tokens, total int, symbol string, percent float64) string {
	bar := renderCategoryBar(tokens, total, symbol)
	return fmt.Sprintf("     %s   %s %s: %s tokens (%.1f%%)\n",
		bar,
		symbol,
		label,
		formatTokenCount(tokens),
		percent,
	)
}

// renderCategoryBar returns a bar showing the share of a category.
func renderCategoryBar(tokens, total int, symbol string) string {
	percentage := 0.0
	if total > 0 {
		percentage = float64(tokens) / float64(total) * 100
	}
	fullSegments, partialSegment := calculateBarSegments(percentage)

	segments := make([]string, 0, contextBarWidth)
	for i := 0; i < fullSegments && len(segments) < contextBarWidth; i++ {
		segments = append(segments, symbol)
	}
	if partialSegment && len(segments) < contextBarWidth {
		if symbol == "⛁" {
			segments = append(segments, "⛀")
		} else {
			segments = append(segments, symbol)
		}
	}
	for len(segments) < contextBarWidth {
		segments = append(segments, "⛶")
	}
	return strings.Join(segments, " ")
}

// calculateBarSegments converts a percentage into bar segments (full segments and a flag for a partial segment).
func calculateBarSegments(percentage float64) (int, bool) {
	if percentage <= 0 {
		return 0, false
	}
	fullSegments := int(percentage / 10)
	if fullSegments >= contextBarWidth {
		return contextBarWidth, false
	}
	remainder := percentage - float64(fullSegments*10)
	return fullSegments, remainder > 0
}

// formatTokenCount formats a token count with appropriate units.
func formatTokenCount(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func percentage(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round((float64(part)/float64(total))*1000) / 10
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
