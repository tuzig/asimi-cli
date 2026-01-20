// Package shogunate implements context tracking for Session.
// This implementation leverages langchaingo's model database and token counting capabilities
// for improved accuracy, particularly for OpenAI models.

package shogunate

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

const (
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
		Model:              s.getModelName(),
		TotalTokens:        s.getModelContextSize(),
		SystemPromptTokens: s.systemPromptTokens,
		SystemToolsTokens:  s.systemToolsTokens,
		MemoryFilesTokens:  s.memoryFilesTokens,
		MessagesTokens:     s.messagesTokens,
	}

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
// This includes the base system prompt template and AGENTS.md content if it exists.
func (s *Session) CountSystemPromptTokens() int {
	if len(s.Messages) == 0 {
		return 0
	}

	if s.Messages[0].Role != llms.ChatMessageTypeSystem {
		return 0
	}

	var content strings.Builder
	for _, part := range s.Messages[0].Parts {
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

// CountMemoryFilesTokens counts tokens in dynamically added context files.
// Context files are managed by TUI and passed to this method for token counting.
func (s *Session) CountMemoryFilesTokens(contextFiles map[string]string) int {
	if len(contextFiles) == 0 {
		return 0
	}

	totalTokens := 0
	for path, content := range contextFiles {
		totalTokens += s.countTokens(path)
		totalTokens += s.countTokens(content)
		totalTokens += memoryFileOverheadTokens
	}

	return totalTokens
}

// CountMessagesTokens counts tokens in conversation history (excluding the system message).
func (s *Session) CountMessagesTokens() int {
	if len(s.Messages) <= 1 {
		return 0
	}

	totalTokens := 0
	for i := 1; i < len(s.Messages); i++ {
		msg := s.Messages[i]
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
