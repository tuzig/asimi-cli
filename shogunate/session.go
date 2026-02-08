package shogunate

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal"
	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/tmc/langchaingo/llms"
)

// --- Stream notification message types ---

// StreamChunkMsg contains a streaming text chunk from the LLM
type StreamChunkMsg struct {
	Text string
}

// StreamReasoningChunkMsg contains a reasoning/thinking chunk from the LLM
type StreamReasoningChunkMsg struct {
	Text string
}

// StreamStartMsg signals that streaming has begun
type StreamStartMsg struct {
	EdictID string
}

// StreamCompleteMsg signals that streaming has completed successfully
type StreamCompleteMsg struct{}

// StreamInterruptedMsg signals that streaming was interrupted
type StreamInterruptedMsg struct{ PartialContent string }

// StreamErrorMsg signals an error during streaming
type StreamErrorMsg struct{ Err error }

// StreamMaxTokensReachedMsg signals that the response was truncated due to token limit
type StreamMaxTokensReachedMsg struct{ Content string }

// SessionConfig holds configuration for minister sessions
type SessionConfig struct {
	LLM        internalconfig.LLMConfig
	AgentsFile string
}

// Session represents a chat session for a minister
type Session struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	LastUpdated time.Time `json:"last_updated"`
	FirstPrompt string    `json:"first_prompt"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	WorkingDir  string    `json:"working_dir"`
	ProjectSlug string    `json:"project_slug,omitempty"`

	model        llms.Model
	config       *internalconfig.LLMConfig
	repoInfo     repo.RepoInfo
	tools        []Tool
	messages     []llms.MessageContent
	notify       internal.NotifyFunc
	systemPrompt string

	// Tool execution
	toolCatalog map[string]Tool
	toolDefs    []llms.Tool
	scheduler   *runners.CoreToolScheduler

	// Streaming
	accumulatedContent strings.Builder

	// Loop detection
	lastToolCallKey         string
	toolCallRepetitionCount int

	// MessageCount is the number of messages (used for display in session listings)
	MessageCount int `json:"message_count,omitempty"`

	// Context files - dynamically added via @ references
	ContextFiles map[string]string `json:"context_files"`

	// Write protection - track files read during session
	filesRead map[string]bool

	// Token counts - updated when messages/context changes
	systemPromptTokens int
	systemToolsTokens  int
	memoryFilesTokens  int
	messagesTokens     int

	// Session timing
	startTime time.Time
}

// NewSession creates a new minister session
func NewSession(
	model llms.Model,
	cfg *SessionConfig,
	repoInfo repo.RepoInfo,
	tools []Tool,
	scheduler *runners.CoreToolScheduler,
	notify internal.NotifyFunc,
	systemPrompt string,
) (*Session, error) {
	now := time.Now()
	workingDir, _ := os.Getwd()

	session := &Session{
		ID:           GenerateID("session", now.String()),
		CreatedAt:    now,
		LastUpdated:  now,
		WorkingDir:   workingDir,
		model:        model,
		repoInfo:     repoInfo,
		tools:        tools,
		messages:     []llms.MessageContent{},
		notify:       notify,
		systemPrompt: systemPrompt,
		toolCatalog:  make(map[string]Tool),
		ContextFiles: make(map[string]string),
		filesRead:    make(map[string]bool),
		startTime:    now,
	}

	// Set config
	if cfg != nil {
		session.config = &cfg.LLM
		session.Provider = cfg.LLM.Provider
		session.Model = cfg.LLM.Model
	} else {
		session.config = &internalconfig.LLMConfig{}
	}
	if session.config.MaxTurns <= 0 {
		session.config.MaxTurns = 999
	}

	// Add system prompt as first message
	if systemPrompt != "" {
		session.messages = append(session.messages, llms.MessageContent{
			Role: llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: systemPrompt},
			},
		})
	}

	// Build tool catalog and definitions
	session.toolDefs, session.toolCatalog = buildLLMTools(tools)

	// Use provided scheduler or create a new one
	if scheduler != nil {
		session.scheduler = scheduler
		session.scheduler.SetNotify(notify)
	} else {
		session.scheduler = runners.NewCoreToolScheduler(notify)
	}

	// Initialize token counts
	session.updateTokenCounts()

	return session, nil
}

// GetModel returns the LLM model for this session
func (s *Session) GetModel() llms.Model {
	return s.model
}

// Messages returns the session messages
func (s *Session) Messages() []llms.MessageContent {
	return s.messages
}

// SetMessages replaces the session messages (used when loading from storage)
func (s *Session) SetMessages(msgs []llms.MessageContent) {
	s.messages = msgs
}

// SetNotify updates the session's notify function and the scheduler's notify
func (s *Session) SetNotify(notify internal.NotifyFunc) {
	s.notify = notify
	if s.scheduler != nil {
		s.scheduler.SetNotify(notify)
	}
}

// AddMessage adds a message to the session
func (s *Session) AddMessage(role llms.ChatMessageType, content string) {
	s.messages = append(s.messages, llms.MessageContent{
		Role: role,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: content},
		},
	})
	s.LastUpdated = time.Now()
}

// RegisterShogunateTools adds shogunate-specific tools to the session's tool catalog.
func (s *Session) RegisterShogunateTools(tools []Tool) {
	for _, tool := range tools {
		s.toolCatalog[tool.Name()] = tool
		s.toolDefs = append(s.toolDefs, llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.ParameterSchema(),
			},
		})
	}
}

// AddTools adds shogunate tools to the session
func (s *Session) AddTools(tools []Tool) {
	slog.Info("adding shogunate tools to session", "count", len(tools))
	s.RegisterShogunateTools(tools)
	slog.Info("session tools after adding", "toolDefs", len(s.toolDefs), "toolCatalog", len(s.toolCatalog))
}

// --- Context Files Management ---

// AddContextFile adds file content to the context for the next prompt
func (s *Session) AddContextFile(path, content string) {
	s.ContextFiles[path] = content
	s.updateTokenCounts()
}

// ClearContext removes all dynamically added file content from the context
func (s *Session) ClearContext() {
	s.ContextFiles = make(map[string]string)
	s.updateTokenCounts()
}

// HasContextFiles returns true if there are files in the context
func (s *Session) HasContextFiles() bool {
	return len(s.ContextFiles) > 0
}

// GetContextFiles returns a copy of the context files map
func (s *Session) GetContextFiles() map[string]string {
	result := make(map[string]string)
	for k, v := range s.ContextFiles {
		result[k] = v
	}
	return result
}

// --- Write Protection ---

// MarkFileAsRead records that a file has been read during this session
func (s *Session) MarkFileAsRead(path string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	s.filesRead[absPath] = true
	slog.Debug("file marked as read", "path", absPath)
}

// HasFileBeenRead checks if a file has been read during this session
func (s *Session) HasFileBeenRead(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	return s.filesRead[absPath]
}

// CanWriteFile checks if a file can be written based on session rules:
// - File does not exist (new file)
// - OR file was read earlier in this session
func (s *Session) CanWriteFile(path string) (bool, string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Sprintf("cannot resolve path: %v", err)
	}

	// Check if file exists
	_, err = os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, ""
		}
		return false, fmt.Sprintf("cannot check file status: %v", err)
	}

	// File exists - check if it was read in this session
	if s.HasFileBeenRead(absPath) {
		return true, ""
	}

	return false, fmt.Sprintf("file '%s' already exists and was not read in this session. Use read_file first to review the existing content", filepath.Base(path))
}

// --- Rollback Support ---

// GetMessageSnapshot returns the current size of the message history for rollback purposes
func (s *Session) GetMessageSnapshot() int {
	return len(s.messages)
}

// RollbackTo truncates the message history back to the provided snapshot index
func (s *Session) RollbackTo(snapshot int) {
	if snapshot < 1 {
		snapshot = 1 // always preserve the system prompt
	}
	if snapshot > len(s.messages) {
		snapshot = len(s.messages)
	}
	if snapshot < len(s.messages) {
		s.messages = s.messages[:snapshot]
		s.updateTokenCounts()
	}

	// Reset tool loop detection state when rolling back
	s.lastToolCallKey = ""
	s.toolCallRepetitionCount = 0
}

// ClearHistory clears the conversation history but keeps the system message
func (s *Session) ClearHistory() {
	if len(s.messages) > 0 && s.messages[0].Role == llms.ChatMessageTypeSystem {
		s.messages = s.messages[:1]
	} else {
		s.messages = []llms.MessageContent{}
	}
	s.lastToolCallKey = ""
	s.toolCallRepetitionCount = 0
	s.updateTokenCounts()
	s.startTime = time.Now()
	s.ClearContext()
}

// --- Token Counting ---

const (
	contextBarWidth          = 10
	autocompactBufferRatio   = 0.225
	memoryFileOverheadTokens = 20
	defaultUnknownContextRef = 8192
)

// extendedModelContextSizes contains context sizes for models not covered by langchaingo.
var extendedModelContextSizes = map[string]int{
	"claude-3-5-sonnet-latest":   200_000,
	"claude-3-5-sonnet":          200_000,
	"claude-3-opus-20240229":     200_000,
	"claude-3-sonnet-20240229":   200_000,
	"claude-3-5-haiku-latest":    200_000,
	"claude-3-haiku-20240307":    200_000,
	"claude-sonnet-4-5-20250929": 200_000,
	"gemini-1.5-flash":           1_000_000,
	"gemini-1.5-flash-latest":    1_000_000,
	"gemini-1.5-pro":             2_000_000,
	"gemini-1.5-pro-latest":      2_000_000,
	"gemini-pro":                 1_000_000,
	"gemini-2.0-flash":           1_000_000,
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

// getModelName returns the configured model name when available.
func (s *Session) getModelName() string {
	if s.config != nil && s.config.Model != "" {
		return s.config.Model
	}
	return "Unknown"
}

// getModelContextSize returns the context window size for the current model.
func (s *Session) getModelContextSize() int {
	modelName := s.getModelName()

	if size := llms.GetModelContextSize(modelName); size > 2048 {
		return size
	}

	if size, ok := extendedModelContextSizes[strings.ToLower(modelName)]; ok && size > 0 {
		return size
	}

	if s.config != nil {
		switch strings.ToLower(s.config.Provider) {
		case "anthropic":
			return 200_000
		case "openai":
			return 128_000
		case "googleai":
			return 1_000_000
		}
	}

	return defaultUnknownContextRef
}

// updateTokenCounts recalculates and stores token counts for all context components
func (s *Session) updateTokenCounts() {
	s.systemPromptTokens = s.countSystemPromptTokens()
	s.systemToolsTokens = s.countSystemToolsTokens()
	s.memoryFilesTokens = s.countMemoryFilesTokens()
	s.messagesTokens = s.countMessagesTokens()
}

// countSystemPromptTokens counts tokens in the system prompt.
func (s *Session) countSystemPromptTokens() int {
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

// countSystemToolsTokens counts tokens in tool definitions.
func (s *Session) countSystemToolsTokens() int {
	if len(s.toolDefs) == 0 {
		return 0
	}
	toolsJSON, err := json.Marshal(s.toolDefs)
	if err != nil {
		return 0
	}
	return s.countTokens(string(toolsJSON))
}

// countMemoryFilesTokens counts tokens in dynamically added context files.
func (s *Session) countMemoryFilesTokens() int {
	if len(s.ContextFiles) == 0 {
		return 0
	}
	totalTokens := 0
	for path, content := range s.ContextFiles {
		totalTokens += s.countTokens(path)
		totalTokens += s.countTokens(content)
		totalTokens += memoryFileOverheadTokens
	}
	return totalTokens
}

// countMessagesTokens counts tokens in conversation history (excluding the system message).
func (s *Session) countMessagesTokens() int {
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

// countTokens provides token counting using langchaingo.
func (s *Session) countTokens(text string) int {
	if text == "" {
		return 0
	}
	return llms.CountTokens(s.getModelName(), text)
}

// GetContextUsagePercent returns the percentage of context used (0-100)
func (s *Session) GetContextUsagePercent() float64 {
	info := s.GetContextInfo()
	if info.TotalTokens <= 0 {
		return 0
	}
	return (float64(info.UsedTokens) / float64(info.TotalTokens)) * 100
}

// --- History Compaction ---

// CompactHistory summarizes the conversation history to reduce context usage
func (s *Session) CompactHistory(ctx context.Context, compactPrompt string) (string, error) {
	if len(s.messages) <= 2 {
		return "", fmt.Errorf("not enough conversation history to compact")
	}

	// Build the content to summarize
	var contentBuilder strings.Builder

	contentBuilder.WriteString("## File Changes and Diffs\n\n")
	fileChanges := s.extractFileChanges()
	if len(fileChanges) > 0 {
		for path, changes := range fileChanges {
			contentBuilder.WriteString(fmt.Sprintf("### %s\n\n", path))
			for _, change := range changes {
				contentBuilder.WriteString(change)
				contentBuilder.WriteString("\n\n")
			}
		}
	} else {
		contentBuilder.WriteString("No file changes recorded.\n\n")
	}

	contentBuilder.WriteString("## Conversation History\n\n")
	for i := 1; i < len(s.messages); i++ {
		msg := s.messages[i]
		switch msg.Role {
		case llms.ChatMessageTypeHuman:
			contentBuilder.WriteString("**User:**\n")
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					contentBuilder.WriteString(textPart.Text)
					contentBuilder.WriteString("\n\n")
				}
			}
		case llms.ChatMessageTypeAI:
			contentBuilder.WriteString("**Assistant:**\n")
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					contentBuilder.WriteString(textPart.Text)
					contentBuilder.WriteString("\n\n")
				}
			}
		}
	}

	fullPrompt := fmt.Sprintf("%s\n\n---\n\n%s", compactPrompt, contentBuilder.String())

	originalMessages := s.messages
	systemMessage := s.messages[0]

	s.messages = []llms.MessageContent{
		systemMessage,
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart(fullPrompt)},
		},
	}

	choice, err := s.generateLLMResponse(ctx, nil, nil)
	if err != nil {
		s.messages = originalMessages
		s.updateTokenCounts()
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	summary := choice.Content
	if choice.ReasoningContent != "" {
		summary = choice.ReasoningContent + "\n\n" + choice.Content
	}

	s.messages = []llms.MessageContent{
		systemMessage,
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Previous conversation summary:\n\n" + summary)},
		},
		{
			Role:  llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{llms.TextPart("I understand. I have the context from the previous conversation and am ready to continue.")},
		},
	}

	s.lastToolCallKey = ""
	s.toolCallRepetitionCount = 0
	s.updateTokenCounts()

	return summary, nil
}

// extractFileChanges extracts all file changes from tool call responses
func (s *Session) extractFileChanges() map[string][]string {
	changes := make(map[string][]string)

	for _, msg := range s.messages {
		if msg.Role != llms.ChatMessageTypeTool {
			continue
		}

		for _, part := range msg.Parts {
			if toolResp, ok := part.(llms.ToolCallResponse); ok {
				if toolResp.Name == "write_file" || toolResp.Name == "replace_text" {
					content := toolResp.Content
					if strings.Contains(content, "Successfully") || strings.Contains(content, "wrote") {
						lines := strings.Split(content, "\n")
						for _, line := range lines {
							if strings.Contains(line, "Successfully") || strings.Contains(line, "wrote") {
								changes["file-changes"] = append(changes["file-changes"], content)
								break
							}
						}
					}
				}
			}
		}
	}

	return changes
}

// --- Session Duration ---

// GetSessionDuration returns the duration since the session started
func (s *Session) GetSessionDuration() time.Duration {
	return time.Since(s.startTime)
}

// --- Export Support ---

// GetID returns the session ID (for ExportableSession interface)
func (s *Session) GetID() string {
	return s.ID
}

// GetMessages returns the session messages (for ExportableSession interface)
func (s *Session) GetMessages() []llms.MessageContent {
	return s.messages
}

// FormatMetadata returns formatted metadata for export (for ExportableSession interface)
// Note: exportType is a string here to avoid circular import with main package
func (s *Session) FormatMetadata(exportType, exportedAt string) string {
	var b strings.Builder
	exported := exportedAt

	b.WriteString(fmt.Sprintf("**Export Type:** %s\n", exportType))
	b.WriteString(fmt.Sprintf("**Session ID:** %s | **Working Directory:** %s\n", s.ID, s.WorkingDir))
	b.WriteString(fmt.Sprintf("**Provider:** %s | **Model:** %s\n", s.Provider, s.Model))
	b.WriteString(fmt.Sprintf("**Created:** %s | **Last Updated:** %s | **Exported:** %s\n",
		s.CreatedAt.Format("2006-01-02 15:04:05"),
		s.LastUpdated.Format("2006-01-02 15:04:05"),
		exported))
	if s.ProjectSlug != "" {
		b.WriteString(fmt.Sprintf("**Project:** %s\n", s.ProjectSlug))
	}

	return b.String()
}

// --- Streaming Support ---

// resetStreamBuffer safely resets the accumulated content buffer
func (s *Session) resetStreamBuffer() {
	s.accumulatedContent.Reset()
}

// getStreamBuffer returns the current accumulated content
func (s *Session) getStreamBuffer() string {
	return s.accumulatedContent.String()
}

// --- Message Preparation ---

// prepareUserMessage builds the prompt with context and adds it to the message history
func (s *Session) prepareUserMessage(prompt string, contextFiles map[string]string) {
	s.SanitizeMessages()

	fullPrompt := buildPromptWithContext(prompt, contextFiles)
	s.messages = append(s.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart(fullPrompt)},
	})
}

// buildPromptWithContext builds a prompt that includes all file content
func buildPromptWithContext(userPrompt string, contextFiles map[string]string) string {
	if len(contextFiles) == 0 {
		return userPrompt
	}

	var fileContents []string
	for path, content := range contextFiles {
		fileContents = append(fileContents, fmt.Sprintf("--- Context from: %s ---\n%s\n--- End of Context from: %s ---", path, content, path))
	}

	return strings.Join(fileContents, "\n\n") + "\n" + userPrompt
}

// --- LLM Response Generation ---

// generateLLMResponse calls the LLM and returns the response
func (s *Session) generateLLMResponse(ctx context.Context, streamingFunc func(ctx context.Context, chunk []byte) error, reasoningFunc func(ctx context.Context, reasoningChunk, chunk []byte) error) (*llms.ContentChoice, error) {
	var callOpts []llms.CallOption
	if len(s.toolDefs) > 0 {
		callOpts = append(callOpts, llms.WithTools(s.toolDefs), llms.WithMaxTokens(64000))
		callOpts = append(callOpts, llms.WithToolChoice("auto"))
	}

	if streamingFunc != nil {
		callOpts = append(callOpts, llms.WithStreamingFunc(streamingFunc))
	}

	if reasoningFunc != nil {
		callOpts = append(callOpts, llms.WithStreamingReasoningFunc(reasoningFunc))
	}

	s.SanitizeMessages()
	resp, err := s.model.GenerateContent(ctx, s.messages, callOpts...)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response choices")
	}
	return resp.Choices[0], nil
}

// appendMessage adds LLM response content and tool calls to the message history
func (s *Session) appendMessage(choice *llms.ContentChoice) {
	if choice == nil {
		return
	}

	var parts []llms.ContentPart

	// Add text content if present
	if strings.TrimSpace(choice.Content) != "" {
		parts = append(parts, llms.TextPart(choice.Content))
	}

	// Add tool calls if present
	for _, toolCall := range choice.ToolCalls {
		if toolCall.FunctionCall == nil || toolCall.FunctionCall.Name == "" {
			continue
		}
		parts = append(parts, llms.ToolCall{
			ID:           toolCall.ID,
			Type:         toolCall.Type,
			FunctionCall: toolCall.FunctionCall,
		})
	}

	if len(parts) > 0 {
		s.messages = append(s.messages, llms.MessageContent{
			Role:  llms.ChatMessageTypeAI,
			Parts: parts,
		})
	}
}

// --- Tool Call Loop Detection ---

// getToolCallKey generates a unique key for a tool call based on name and arguments
func (s *Session) getToolCallKey(name, argsJSON string) string {
	keyString := fmt.Sprintf("%s:%s", name, argsJSON)
	hash := sha256.Sum256([]byte(keyString))
	return hex.EncodeToString(hash[:])
}

// checkToolCallLoop detects if the same tool call is being repeated
func (s *Session) checkToolCallLoop(name, argsJSON string) bool {
	const toolCallLoopThreshold = 3

	key := s.getToolCallKey(name, argsJSON)
	if s.lastToolCallKey == key {
		s.toolCallRepetitionCount++
	} else {
		s.lastToolCallKey = key
		s.toolCallRepetitionCount = 1
	}

	if s.toolCallRepetitionCount >= toolCallLoopThreshold {
		slog.Warn("tool call loop detected", "tool", name, "count", s.toolCallRepetitionCount)
		return true
	}

	return false
}

// --- Message Sanitization ---

// SanitizeMessages removes any trailing assistant messages with tool calls
// that don't have corresponding tool responses.
func (s *Session) SanitizeMessages() {
	if s.config != nil && s.config.DisableContextSanitization {
		return
	}

	if len(s.messages) == 0 {
		return
	}

	for len(s.messages) > 0 {
		lastIdx := len(s.messages) - 1
		lastMsg := s.messages[lastIdx]

		if lastMsg.Role == llms.ChatMessageTypeAI {
			hasToolCalls := false
			for _, part := range lastMsg.Parts {
				if _, ok := part.(llms.ToolCall); ok {
					hasToolCalls = true
					break
				}
			}

			if hasToolCalls {
				slog.Debug("removing unmatched tool call from context")
				s.messages = s.messages[:lastIdx]
				continue
			}
		}

		if lastMsg.Role == llms.ChatMessageTypeTool {
			if lastIdx == 0 {
				slog.Debug("removing tool result without prior messages")
				s.messages = s.messages[:lastIdx]
				continue
			}

			var aiMsg *llms.MessageContent
			for i := lastIdx - 1; i >= 0; i-- {
				if s.messages[i].Role == llms.ChatMessageTypeAI {
					aiMsg = &s.messages[i]
					break
				}
				if s.messages[i].Role != llms.ChatMessageTypeTool {
					break
				}
			}

			if aiMsg == nil {
				slog.Debug("removing tool result without prior AI message")
				s.messages = s.messages[:lastIdx]
				continue
			}

			toolCallIDs := make(map[string]struct{})
			for _, part := range aiMsg.Parts {
				if tc, ok := part.(llms.ToolCall); ok && tc.ID != "" {
					toolCallIDs[tc.ID] = struct{}{}
				}
			}

			valid := len(toolCallIDs) > 0
			for _, part := range lastMsg.Parts {
				if resp, ok := part.(llms.ToolCallResponse); ok {
					if _, exists := toolCallIDs[resp.ToolCallID]; !exists || resp.ToolCallID == "" {
						valid = false
						break
					}
				}
			}

			if !valid {
				slog.Debug("removing dangling tool result from context")
				s.messages = s.messages[:lastIdx]
				continue
			}
		}

		return
	}
}

// --- Tool Execution ---

// toolCallIDCounter is used to generate unique tool call IDs when provider doesn't supply one
var toolCallIDCounter int64

// ensureToolCallID returns the tool call ID if valid, or generates a synthetic one.
func ensureToolCallID(tc *llms.ToolCall, index int) string {
	if tc.ID != "" {
		return tc.ID
	}
	toolCallIDCounter++
	syntheticID := fmt.Sprintf("synthetic_%d_%d", time.Now().UnixNano(), toolCallIDCounter)
	tc.ID = syntheticID
	slog.Warn("provider returned empty tool_call_id, using synthetic ID",
		"index", index,
		"tool", tc.FunctionCall.Name,
		"synthetic_id", syntheticID)
	return syntheticID
}

// hasToolCallResponse checks if toolMessages already contains a response for the given tool call ID
func hasToolCallResponse(toolMessages []llms.MessageContent, toolCallID string) bool {
	for _, msg := range toolMessages {
		if msg.Role != llms.ChatMessageTypeTool {
			continue
		}
		for _, part := range msg.Parts {
			if resp, ok := part.(llms.ToolCallResponse); ok && resp.ToolCallID == toolCallID {
				return true
			}
		}
	}
	return false
}

// executeToolCall executes a single tool call and returns the response content
func (s *Session) executeToolCall(ctx context.Context, tool Tool, tc llms.ToolCall, argsJSON string) llms.ToolCallResponse {
	var out string
	var callErr error

	if s.scheduler != nil {
		ch := s.scheduler.Schedule(tool, argsJSON)
		res := <-ch
		out, callErr = res.Output, res.Error
	} else {
		out, callErr = tool.Call(ctx, argsJSON)
	}

	if callErr != nil {
		return llms.ToolCallResponse{
			ToolCallID: tc.ID,
			Name:       tc.FunctionCall.Name,
			Content:    fmt.Sprintf("Error: %v", callErr),
		}
	}

	return llms.ToolCallResponse{
		ToolCallID: tc.ID,
		Name:       tc.FunctionCall.Name,
		Content:    out,
	}
}

// processToolCalls handles executing tool calls and building response messages.
func (s *Session) processToolCalls(ctx context.Context, toolCalls []llms.ToolCall) ([]llms.MessageContent, bool) {
	toolMessages := make([]llms.MessageContent, 0, len(toolCalls))

	for i := range toolCalls {
		tc := &toolCalls[i]
		if tc.FunctionCall == nil {
			continue
		}

		name := tc.FunctionCall.Name
		if name == "" {
			slog.Debug("skipping tool call with empty name", "index", i)
			continue
		}

		ensureToolCallID(tc, i)
		argsJSON := tc.FunctionCall.Arguments

		// Check for context cancellation
		select {
		case <-ctx.Done():
			slog.Debug("context cancelled during tool execution", "completed", i, "total", len(toolCalls))

			for _, remainingTC := range toolCalls {
				if remainingTC.FunctionCall == nil {
					continue
				}
				if !hasToolCallResponse(toolMessages, remainingTC.ID) {
					toolMessages = append(toolMessages, llms.MessageContent{
						Role: llms.ChatMessageTypeTool,
						Parts: []llms.ContentPart{llms.ToolCallResponse{
							ToolCallID: remainingTC.ID,
							Name:       remainingTC.FunctionCall.Name,
							Content:    "error: session aborted by user",
						}},
					})
				}
			}

			return toolMessages, true
		default:
		}

		// Check for tool call loops
		if s.checkToolCallLoop(name, argsJSON) {
			toolMessages = append(toolMessages, llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{llms.ToolCallResponse{
					ToolCallID: tc.ID,
					Name:       name,
					Content:    fmt.Sprintf("error: tool call loop detected after %d attempts, please try a different approach", s.toolCallRepetitionCount),
				}},
			})
			return toolMessages, true
		}

		tool, ok := s.toolCatalog[name]
		if !ok {
			toolMessages = append(toolMessages, llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{llms.ToolCallResponse{
					ToolCallID: tc.ID,
					Name:       name,
					Content:    fmt.Sprintf("error: unknown tool %q", name),
				}},
			})
			continue
		}

		response := s.executeToolCall(ctx, tool, *tc, argsJSON)
		slog.Debug("Called a tool", "tool", name, "args", argsJSON)
		toolMessages = append(toolMessages, llms.MessageContent{
			Role:  llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{response},
		})
	}

	return toolMessages, false
}

// --- Main API Methods ---

// AskWithStreaming sends a user prompt and streams the response while blocking.
// It streams chunks to the UI via the notify callback.
func (s *Session) AskWithStreaming(ctx context.Context, prompt string, contextFiles map[string]string) (string, error) {
	s.prepareUserMessage(prompt, contextFiles)

	if s.notify != nil {
		s.notify(StreamStartMsg{})
	}

	var finalText string
	maxTurns := s.config.MaxTurns

	for i := 0; i < maxTurns; i++ {
		s.resetStreamBuffer()

		// Check for cancellation
		select {
		case <-ctx.Done():
			accumulatedText := s.getStreamBuffer()
			if s.notify != nil {
				s.notify(StreamInterruptedMsg{PartialContent: accumulatedText})
			}
			return accumulatedText, ctx.Err()
		default:
		}

		// Create streaming function for content
		streamingFunc := func(ctx context.Context, chunk []byte) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			chunkStr := string(chunk)
			s.accumulatedContent.WriteString(chunkStr)
			if s.notify != nil {
				s.notify(StreamChunkMsg{Text: chunkStr})
			}
			return nil
		}

		// Create streaming function for reasoning/thinking content
		reasoningFunc := func(ctx context.Context, reasoningChunk, chunk []byte) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if len(reasoningChunk) > 0 && s.notify != nil {
				s.notify(StreamReasoningChunkMsg{Text: string(reasoningChunk)})
			}
			return nil
		}

		choice, err := s.generateLLMResponse(ctx, streamingFunc, reasoningFunc)
		if err != nil {
			if ctx.Err() != nil {
				accumulatedText := s.getStreamBuffer()
				if s.notify != nil {
					s.notify(StreamInterruptedMsg{PartialContent: accumulatedText})
				}
				return accumulatedText, ctx.Err()
			}
			if s.notify != nil {
				s.notify(StreamErrorMsg{Err: err})
			}
			return "", err
		}

		responseContent := s.getStreamBuffer()

		// Check if response was truncated
		if choice.StopReason == "max_tokens" {
			if s.notify != nil {
				s.notify(StreamMaxTokensReachedMsg{Content: responseContent})
			}
			s.appendMessage(choice)
			return responseContent + "\n\n[Response truncated due to length limit]", nil
		}

		if strings.TrimSpace(responseContent) != "" {
			finalText = responseContent
		}
		s.appendMessage(choice)

		// Handle tool calls - if no tool calls, we're done
		if len(choice.ToolCalls) == 0 {
			break
		}

		// Process tool calls
		toolMessages, shouldReturn := s.processToolCalls(ctx, choice.ToolCalls)
		if len(toolMessages) > 0 {
			s.messages = append(s.messages, toolMessages...)
		}

		if shouldReturn {
			if s.notify != nil {
				s.notify(StreamCompleteMsg{})
			}
			return finalText, nil
		}

		if len(toolMessages) > 0 {
			continue
		}

		break
	}

	if s.notify != nil {
		s.notify(StreamCompleteMsg{})
	}

	return finalText, nil
}

// --- Helper Functions ---

// buildLLMTools returns the LLM tool definitions and a catalog by name for execution.
func buildLLMTools(tools []Tool) ([]llms.Tool, map[string]Tool) {
	execCatalog := map[string]Tool{}
	defs := make([]llms.Tool, 0, len(tools))

	for i := range tools {
		tool := tools[i]
		execCatalog[tool.Name()] = tool

		defs = append(defs, llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.ParameterSchema(),
			},
		})
	}

	return defs, execCatalog
}

// --- Tool Input Types (for JSON parsing) ---

// ReadFileInput represents the input for read_file tool
type ReadFileInput struct {
	Path string `json:"path"`
}

// ReadManyFilesInput represents the input for read_many_files tool
type ReadManyFilesInput struct {
	Paths []string `json:"paths"`
}

// WriteFileInput represents the input for write_file tool
type WriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ToolResult represents a tool execution result
type ToolResult struct {
	Output string
	Error  error
}

// --- JSON Helper ---

// JSON is a map for storing arbitrary JSON data
type JSON = map[string]any

func GenerateSessionID() string {
	timestamp := time.Now().Format("2006-01-02-150405")

	randomBytes := make([]byte, 4)
	crand.Read(randomBytes)
	suffix := hex.EncodeToString(randomBytes)

	return fmt.Sprintf("%s-%s", timestamp, suffix)
}
