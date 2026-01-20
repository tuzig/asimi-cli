package shogunate

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
	lctools "github.com/tmc/langchaingo/tools"
)

const sandboxOS = "debian"

// Version is the application version, set by main package
var Version = "dev"

// SessionConfig holds configuration needed by Session.
type SessionConfig struct {
	LLM        internalconfig.LLMConfig
	AgentsFile string
}

// SessionStore is a placeholder interface for session persistence.
type SessionStore interface {
	SaveSession(session *Session)
	SaveSessionSync(session *Session) error
	LoadSession(id string) (*Session, error)
	ListSessions(limit int) ([]Session, error)
	Close()
	Flush()
}

// TokenRefreshFunc is called when OAuth token needs refresh
type TokenRefreshFunc func(provider string) (string, error)

// ModelClientFunc creates a new LLM client with the given config
type ModelClientFunc func(config *internalconfig.LLMConfig) (llms.Model, error)

// ToolsBuilderFunc builds tools from configuration
type ToolsBuilderFunc func(config *SessionConfig) ([]llms.Tool, map[string]interface{})

// Tool input types used by Session for context tracking

// ReadFileInput represents input for read_file tool
type ReadFileInput struct {
	Path string `json:"path"`
}

// ReadManyFilesInput represents input for read_many_files tool
type ReadManyFilesInput struct {
	Paths []string `json:"paths"`
}

// WriteFileInput represents input for write_file tool
type WriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Stream notification message types

// StreamChunkMsg contains a streaming text chunk from the LLM
type StreamChunkMsg string

// StreamReasoningChunkMsg contains a reasoning/thinking chunk from the LLM
type StreamReasoningChunkMsg string

// StreamStartMsg signals that streaming has begun
type StreamStartMsg struct{}

// StreamCompleteMsg signals that streaming has completed successfully
type StreamCompleteMsg struct{}

// StreamInterruptedMsg signals that streaming was interrupted (e.g., by user)
type StreamInterruptedMsg struct{ PartialContent string }

// StreamErrorMsg signals an error during streaming
type StreamErrorMsg struct{ Err error }

// StreamMaxTurnsExceededMsg signals that the max turns limit was reached
type StreamMaxTurnsExceededMsg struct{ MaxTurns int }

// StreamMaxTokensReachedMsg signals that the response was truncated due to token limit
type StreamMaxTokensReachedMsg struct{ Content string }

// ContainerLaunchMsg signals container lifecycle events
type ContainerLaunchMsg struct{ Message string }

// PromptReply carries responses back to the TUI via channel
type PromptReply struct {
	Type    PromptReplyType
	Content string
	Error   error
	Data    any // For Zhengming requests, tool results, etc.
}

// PromptReplyType identifies the type of reply
type PromptReplyType int

const (
	ReplyStreamStart PromptReplyType = iota
	ReplyStreamChunk
	ReplyStreamComplete
	ReplyToolCall
	ReplyToolResult
	ReplyZhengming // Chancellor needs clarification
	ReplyError
)

// Session is a lightweight chat loop that uses llms.Model directly
// and native provider tool/function-calling. It executes tools via the
// existing CoreToolScheduler and keeps conversation state locally.
type Session struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	LastUpdated time.Time `json:"last_updated"`
	FirstPrompt string    `json:"first_prompt"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	WorkingDir  string    `json:"working_dir"`
	ProjectSlug string    `json:"project_slug,omitempty"`

	Messages     []llms.MessageContent `json:"messages"`
	MessageCount int                   `json:"message_count,omitempty"` // For list views, avoids loading full messages

	// Track files read during session for write protection
	filesRead map[string]bool `json:"-"`

	llm                     llms.Model                `json:"-"`
	toolCatalog             map[string]lctools.Tool   `json:"-"`
	toolDefs                []llms.Tool               `json:"-"`
	lastToolCallKey         string                    `json:"-"`
	toolCallRepetitionCount int                       `json:"-"`
	scheduler               *CoreToolScheduler        `json:"-"`
	notify                  NotifyFunc                `json:"-"`
	accumulatedContent      strings.Builder           `json:"-"`
	config                  *internalconfig.LLMConfig `json:"-"`
	startTime               time.Time                 `json:"-"`

	// Shogunate Forge for envelope-based tool execution
	forge *Forge `json:"-"`

	// Token counts - updated when messages/context changes
	systemPromptTokens int `json:"-"`
	systemToolsTokens  int `json:"-"`
	memoryFilesTokens  int `json:"-"`
	messagesTokens     int `json:"-"`

	// Persistence (injected by TUI after session creation)
	store      SessionStore `json:"-"`
	autoSaveOn bool         `json:"-"`

	// Callbacks for OAuth and model client recreation (injected by main)
	refreshToken   TokenRefreshFunc `json:"-"`
	recreateClient ModelClientFunc  `json:"-"`
}

// SetOAuthCallbacks sets the callbacks for OAuth token refresh and model client recreation.
func (s *Session) SetOAuthCallbacks(refresh TokenRefreshFunc, recreate ModelClientFunc) {
	s.refreshToken = refresh
	s.recreateClient = recreate
}

// SetStore sets the session store for persistence operations.
func (s *Session) SetStore(store SessionStore, autoSave bool) {
	s.store = store
	s.autoSaveOn = autoSave
}

// Save persists the current session state asynchronously.
func (s *Session) Save() {
	if s.store == nil || !s.autoSaveOn {
		return
	}
	s.store.SaveSession(s)
	slog.Debug("session save queued")
}

// CloseStore gracefully closes the session store.
func (s *Session) CloseStore() {
	if s.store != nil {
		s.store.Close()
	}
}

// No syncMessages method needed anymore - we only use Messages

// resetStreamBuffer safely resets the accumulated content buffer
func (s *Session) resetStreamBuffer() {
	s.accumulatedContent.Reset()
}

// getStreamBuffer returns the current accumulated content
func (s *Session) getStreamBuffer() string {
	return s.accumulatedContent.String()
}

// notification messages
// Message types are defined in session_msgs.go

// Local copies of prompt partials and template used by the session, to decouple from agent.go.
var sessPromptPartials = map[string]any{
	"SandboxStatus": "none",
	"UserMemory":    "",
	"Env":           "",
	"ReadFile":      "read_file",
	"WriteFile":     "write_file",
	"Grep":          "grep",
	"Glob":          "glob",
	"Edit":          "replace_text",
	"Shell":         "run_shell_command",
	"ReadManyFiles": "read_many_files",
	"Memory":        "",
	"LS":            "list_files",
	"history":       "",
}

//go:embed prompts/system_prompt.tmpl
var sessSystemPromptTemplate string

// NewSession creates a new Session instance with a system prompt and tools.
// If systemPrompt is empty, it uses the default template from prompts/system_prompt.tmpl.
// If systemPrompt is provided, it uses that directly (for Shogunate ministers, etc.).
func NewSession(llm llms.Model, cfg *SessionConfig, repoInfo repo.RepoInfo, tools []Tool, scheduler *CoreToolScheduler, toolNotify NotifyFunc, role string) (*Session, error) {
	now := time.Now()
	workingDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	s := &Session{
		ID:          GenerateSessionID(),
		CreatedAt:   now,
		LastUpdated: now,
		WorkingDir:  workingDir,
		llm:         llm,
		toolCatalog: map[string]lctools.Tool{},
		notify:      toolNotify,
		filesRead:   make(map[string]bool),
	}
	if cfg != nil {
		s.config = &cfg.LLM
		s.Provider = cfg.LLM.Provider
		s.Model = cfg.LLM.Model
		// Set default maxTurns if not configured
	} else {
		// Create default config if none provided
		s.config = &internalconfig.LLMConfig{}
	}
	if s.config.MaxTurns <= 0 {
		s.config.MaxTurns = 999
	}

	// Build system prompt from the existing template and partials (legacy mode)
	partials := map[string]any{
		"SandboxStatus": "none",
		"Env":           sessBuildEnvBlock(repoInfo),
		"Role":          role,
		"ProjectName":   repoInfo.Slug,
	}

	pt := prompts.PromptTemplate{
		Template:         sessSystemPromptTemplate,
		TemplateFormat:   prompts.TemplateFormatGoTemplate,
		InputVariables:   []string{"input", "agent_scratchpad"},
		PartialVariables: partials,
	}

	// Render with empty input/scratchpad since this is a system message.
	sys, err := pt.Format(map[string]any{"input": "", "agent_scratchpad": ""})
	if err != nil {
		return nil, fmt.Errorf("formatting system prompt: %w", err)
	}
	parts := []llms.ContentPart{llms.TextPart(sys)}

	// Add agents file (AGENTS.md or CLAUDE.md) to system message if it exists
	agentsFile := "AGENTS.md"
	if cfg != nil && cfg.AgentsFile != "" {
		agentsFile = cfg.AgentsFile
	}
	projectContext := readProjectContext(agentsFile)
	if projectContext != "" {
		parts = append(parts, llms.TextPart(fmt.Sprintf("\n--- Project specific directions from: %s ---\n%s\n--- End of Directions from: %s ---", agentsFile, projectContext, agentsFile)))

	}
	// For Ollama, consolidate all parts into a single text
	if s.config != nil && s.config.Provider == "ollama" {
		var builder strings.Builder
		for _, part := range parts {
			if textPart, ok := part.(llms.TextContent); ok {
				if builder.Len() > 0 {
					builder.WriteString("\n\n")
				}
				builder.WriteString(textPart.Text)
			}
		}
		parts = []llms.ContentPart{llms.TextPart(builder.String())}
	}

	s.Messages = append(s.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeSystem,
		Parts: parts,
	})

	// Build tool schema for the model and execution catalog for the scheduler.
	s.toolDefs, s.toolCatalog = buildLLMTools(tools)
	// Use provided scheduler or create a new one for non-interactive mode
	if scheduler != nil {
		s.scheduler = scheduler
		s.scheduler.SetNotify(s.notify)
	} else {
		s.scheduler = NewCoreToolScheduler(s.notify)
	}
	s.startTime = time.Now()
	s.UpdateTokenCounts(nil)
	return s, nil
}

// MarkFileAsRead records that a file has been read during this session
func (s *Session) MarkFileAsRead(path string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		// If we can't get absolute path, use the path as-is
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
			// File doesn't exist - allowed to write
			slog.Debug("Files does not exist write file allowed", "path", absPath)
			return true, ""
		}
		// Some other error - be conservative and deny
		return false, fmt.Sprintf("cannot check file status: %v", err)
	}

	// File exists - check if it was read in this session
	if s.HasFileBeenRead(absPath) {
		return true, ""
	}

	// File exists but wasn't read - deny write
	return false, fmt.Sprintf("file '%s' already exists and was not read in this session. Use read_file first to review the existing content", filepath.Base(path))
}

// ClearHistory clears the conversation history but keeps the system message
// TODO: rename to ClearMessages
func (s *Session) ClearHistory() {
	// Keep only the system message (first message)
	if len(s.Messages) > 0 && s.Messages[0].Role == llms.ChatMessageTypeSystem {
		s.Messages = s.Messages[:1]
	} else {
		s.Messages = []llms.MessageContent{}
	}

	// Reset tool call tracking
	s.lastToolCallKey = ""
	s.toolCallRepetitionCount = 0

	// Invalidate context cache since messages changed
	s.UpdateTokenCounts(nil)

	// Reset session start time
	s.startTime = time.Now()
}

// InjectSystemPromptSuffix appends additional content to the system prompt.
// This is used to add role-specific identity (e.g., Chancellor prompt) to the session.
func (s *Session) InjectSystemPromptSuffix(suffix string) {
	if len(s.Messages) == 0 || s.Messages[0].Role != llms.ChatMessageTypeSystem {
		slog.Warn("cannot inject suffix: no system message found")
		return
	}

	// Append the suffix as a new text part to the system message
	s.Messages[0].Parts = append(s.Messages[0].Parts, llms.TextPart(suffix))
	s.UpdateTokenCounts(nil)
}

// SetSystemPrompt sets the system prompt
func (s *Session) SetSystemPrompt(prompt string) {
	s.ReplaceSystemPrompt(prompt)
}

// ReplaceSystemPrompt replaces the entire system prompt with new content.
// This is used for minister sessions that need a completely different identity.
func (s *Session) ReplaceSystemPrompt(newPrompt string) {
	systemMsg := llms.MessageContent{
		Role:  llms.ChatMessageTypeSystem,
		Parts: []llms.ContentPart{llms.TextPart(newPrompt)},
	}

	if len(s.Messages) > 0 && s.Messages[0].Role == llms.ChatMessageTypeSystem {
		s.Messages[0] = systemMsg
	} else {
		// Prepend system message
		s.Messages = append([]llms.MessageContent{systemMsg}, s.Messages...)
	}
	s.UpdateTokenCounts(nil)
}

// RegisterShogunateTools adds shogunate-specific tools to the session's tool catalog.
// The tools are added to both the execution catalog and the LLM definitions.
func (s *Session) RegisterShogunateTools(tools []Tool) {
	for _, tool := range tools {
		// Add to execution catalog - Tool is compatible with lctools.Tool
		s.toolCatalog[tool.Name()] = tool

		// Add to LLM tool definitions
		s.toolDefs = append(s.toolDefs, llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.ParameterSchema(),
			},
		})
	}
	s.UpdateTokenCounts(nil)
}

// AddTools adds shogunate tools to the session
func (s *Session) AddTools(tools []Tool) {
	slog.Info("adding shogunate tools to session", "count", len(tools))
	s.RegisterShogunateTools(tools)
	slog.Info("session tools after adding", "toolDefs", len(s.toolDefs), "toolCatalog", len(s.toolCatalog))
}

// GetNotify returns the session's notify function for use by sub-components.
func (s *Session) GetNotify() NotifyFunc {
	return s.notify
}

// GetScheduler returns the session's tool scheduler.
func (s *Session) GetScheduler() *CoreToolScheduler {
	return s.scheduler
}

// SetForge sets the Shogunate Forge for envelope-based tool execution.
// It also passes the Session's tools to the Forge for execution.
func (s *Session) SetForge(forge *Forge) {
	s.forge = forge

	// Pass Session's tools to the Forge
	// The tools in toolCatalog are concrete types that implement Tool
	if forge != nil && len(s.toolCatalog) > 0 {
		forgeTools := make(map[string]Tool)
		for name, tool := range s.toolCatalog {
			if st, ok := tool.(Tool); ok {
				forgeTools[name] = st
			}
		}
		if len(forgeTools) > 0 {
			forge.SetTools(forgeTools)
		}
	}
}

// GetForge returns the Shogunate Forge if set.
func (s *Session) GetForge() *Forge {
	return s.forge
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

// getToolCallKey generates a unique key for a tool call based on name and arguments
func (s *Session) getToolCallKey(name, argsJSON string) string {
	keyString := fmt.Sprintf("%s:%s", name, argsJSON)
	hash := sha256.Sum256([]byte(keyString))
	return hex.EncodeToString(hash[:])
}

// checkToolCallLoop detects if the same tool call is being repeated
func (s *Session) checkToolCallLoop(name, argsJSON string) bool {
	const toolCallLoopThreshold = 3 // More conservative than gemini-cli's 5

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

// SanitizeMessages removes any trailing assistant messages with tool calls
// that don't have corresponding tool responses. This prevents errors when the agent
// is interrupted mid-execution. Can be disabled via config.
func (s *Session) SanitizeMessages() {
	// Check if sanitization is disabled
	if s.config != nil && s.config.DisableContextSanitization {
		return
	}

	if len(s.Messages) == 0 {
		return
	}

	for len(s.Messages) > 0 {
		lastIdx := len(s.Messages) - 1
		lastMsg := s.Messages[lastIdx]

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
				s.Messages = s.Messages[:lastIdx]
				continue
			}
		}

		if lastMsg.Role == llms.ChatMessageTypeTool {
			if lastIdx == 0 {
				slog.Debug("removing tool result without prior messages")
				s.Messages = s.Messages[:lastIdx]
				continue
			}

			// Look backwards past other tool messages to find the AI message with tool calls
			var aiMsg *llms.MessageContent
			for i := lastIdx - 1; i >= 0; i-- {
				if s.Messages[i].Role == llms.ChatMessageTypeAI {
					aiMsg = &s.Messages[i]
					break
				}
				// Stop if we encounter a non-tool message that isn't AI
				if s.Messages[i].Role != llms.ChatMessageTypeTool {
					break
				}
			}

			if aiMsg == nil {
				slog.Debug("removing tool result without prior AI message")
				s.Messages = s.Messages[:lastIdx]
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
				s.Messages = s.Messages[:lastIdx]
				continue
			}
		}

		return
	}
}

// prepareUserMessage builds the prompt with context and adds it to the message history
func (s *Session) prepareUserMessage(prompt string, contextFiles map[string]string) {
	// Before adding a new user message, check for and remove any unmatched tool calls
	s.SanitizeMessages()

	fullPrompt := buildPromptWithContext(prompt, contextFiles)
	s.Messages = append(s.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart(fullPrompt)},
	})
	// Invalidate context cache since messages changed
	s.UpdateTokenCounts(nil)
}

// isOAuthTokenExpiredError checks if an error is due to an expired or revoked OAuth token
func isOAuthTokenExpiredError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	// Check for OAuth-related expiration or revocation errors
	return (strings.Contains(errStr, "oauth") || strings.Contains(errStr, "401") || strings.Contains(errStr, "403")) &&
		(strings.Contains(errStr, "expire") || strings.Contains(errStr, "revoke"))
}

func (s *Session) generateLLMResponse(ctx context.Context, streamingFunc func(ctx context.Context, chunk []byte) error) (*llms.ContentChoice, error) {
	// Build call options; try with explicit tool choice first, then without, then no tools.
	var callOptsWithChoice []llms.CallOption
	var callOptsNoChoice []llms.CallOption
	if len(s.toolDefs) > 0 {
		callOptsNoChoice = []llms.CallOption{llms.WithTools(s.toolDefs), llms.WithMaxTokens(64000)}
		callOptsWithChoice = append([]llms.CallOption{}, callOptsNoChoice...)
		callOptsWithChoice = append(callOptsWithChoice, llms.WithToolChoice("auto"))
	}

	// Add streaming option if requested
	if streamingFunc != nil {
		// TODO: find a way to controll the thinking mode
		callOptsWithChoice = append(callOptsWithChoice, llms.WithStreamingFunc(streamingFunc),
			llms.WithThinkingMode(llms.ThinkingModeMedium),
			llms.WithTemperature(1))

		// Add reasoning callback for models that support it (#38)
		reasoningFunc := func(ctx context.Context, reasoningChunk, chunk []byte) error {
			// Check for cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// Send reasoning chunk to UI
			if len(reasoningChunk) > 0 && s.notify != nil {
				s.notify(StreamReasoningChunkMsg(string(reasoningChunk)))
			}
			return nil
		}
		callOptsWithChoice = append(callOptsWithChoice, llms.WithStreamingReasoningFunc(reasoningFunc))
	}

	// Remove any unmatched tool calls from context before sending to API
	s.SanitizeMessages()

	// Attempt with explicit tool choice first
	resp, err := s.llm.GenerateContent(ctx, s.Messages, callOptsWithChoice...)
	if err != nil {
		// Check if this is an OAuth token expiration error
		if isOAuthTokenExpiredError(err) && s.refreshToken != nil && s.recreateClient != nil {
			slog.Info("OAuth token expired, attempting to force refresh and retry", "error", err)

			// Force refresh the token (ignoring local expiry time since server rejected it)
			newToken, refreshErr := s.refreshToken(s.config.Provider)
			if refreshErr != nil {
				slog.Error("Failed to force refresh OAuth token", "error", refreshErr)
				return nil, fmt.Errorf("OAuth token expired and refresh failed: %w (original error: %v)", refreshErr, err)
			}

			// Update the session config with the new token
			s.config.AuthToken = newToken

			// Recreate the LLM client with the new token
			newLLM, clientErr := s.recreateClient(s.config)
			if clientErr != nil {
				slog.Error("Failed to recreate LLM client after token refresh", "error", clientErr)
				return nil, fmt.Errorf("failed to recreate LLM client after token refresh: %w", clientErr)
			}
			s.llm = newLLM

			// Retry the request with the new client
			slog.Info("Retrying request with refreshed OAuth token")
			resp, err = s.llm.GenerateContent(ctx, s.Messages, callOptsWithChoice...)
			if err != nil {
				return nil, fmt.Errorf("request failed after OAuth token refresh: %w", err)
			}
		} else if isOAuthTokenExpiredError(err) {
			// OAuth error but no callbacks set
			return nil, fmt.Errorf("OAuth token expired but refresh callbacks not set: %w", err)
		} else {
			// Not an OAuth error, return as-is
			return nil, err
		}
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response choices")
	}
	return resp.Choices[0], nil
}

// appendMessage adds LLM response content, thinking content, and tool calls to the message history.
// When thinking is enabled, the thinking content must be included to satisfy Anthropic's API requirement
// that assistant messages start with a thinking block before any tool_use blocks.
func (s *Session) appendMessage(choice *llms.ContentChoice) {
	if choice == nil {
		return
	}

	// Extract thinking signature from generation info
	var thinkingSignature string
	if choice.GenerationInfo != nil {
		if sig, ok := choice.GenerationInfo["ThinkingSignature"].(string); ok {
			thinkingSignature = sig
		}
	}

	// Build the assistant message parts
	var parts []llms.ContentPart

	slog.Debug("appending message", "signature", thinkingSignature)
	// Add thinking content first if present (must come before tool_use blocks per Anthropic API)
	if strings.TrimSpace(choice.ReasoningContent) != "" {
		parts = append(parts, llms.ThinkingPartWithSignature(choice.ReasoningContent, thinkingSignature))
	}

	// Add text content if present
	if strings.TrimSpace(choice.Content) != "" {
		parts = append(parts, llms.TextPart(choice.Content))
	}

	// Add tool calls if present
	for _, toolCall := range choice.ToolCalls {
		parts = append(parts, llms.ToolCall{
			ID:           toolCall.ID,
			Type:         toolCall.Type,
			FunctionCall: toolCall.FunctionCall,
		})
	}

	// Only add the assistant message if we have content or tool calls
	if len(parts) > 0 {
		s.Messages = append(s.Messages, llms.MessageContent{
			Role:  llms.ChatMessageTypeAI,
			Parts: parts,
		})
		// Invalidate context cache since messages changed
		s.UpdateTokenCounts(nil)
	}
}

// executeToolCall executes a single tool call and returns the response content
func (s *Session) executeToolCall(ctx context.Context, tool lctools.Tool, tc llms.ToolCall, argsJSON string) llms.ToolCallResponse {
	var out string
	var callErr error

	switch tc.FunctionCall.Name {
	case "read_file", "read_many_files":
		s.trackFileReads(tc.FunctionCall.Name, argsJSON)
	case "write_file":
		allowed, reason := s.checkWritePermission(argsJSON)
		if !allowed {
			slog.Debug("write_file was rejected", "reason", reason)
			return llms.ToolCallResponse{
				ToolCallID: tc.ID,
				Name:       tc.FunctionCall.Name,
				Content:    fmt.Sprintf("Error: %s", reason),
			}
		}
	}

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

// trackFileReads extracts file paths from read tool calls and marks them as read
func (s *Session) trackFileReads(toolName string, argsJSON string) {
	switch toolName {
	case "read_file":
		var params ReadFileInput
		if err := json.Unmarshal([]byte(argsJSON), &params); err == nil {
			s.MarkFileAsRead(params.Path)
		}
	case "read_many_files":
		var params ReadManyFilesInput
		if err := json.Unmarshal([]byte(argsJSON), &params); err == nil {
			for _, path := range params.Paths {
				s.MarkFileAsRead(path)
			}
		}
	}
}

// checkWritePermission validates if a write_file operation is allowed
func (s *Session) checkWritePermission(argsJSON string) (bool, string) {
	var params WriteFileInput
	if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
		return false, fmt.Sprintf("invalid write_file parameters: %v", err)
	}

	return s.CanWriteFile(params.Path)
}

// GetMessageSnapshot returns the current size of the message history for rollback purposes
func (s *Session) GetMessageSnapshot() int {
	return len(s.Messages)
}

// RollbackTo truncates the message history back to the provided snapshot index
func (s *Session) RollbackTo(snapshot int) {
	if snapshot < 1 {
		snapshot = 1 // always preserve the system prompt
	}
	if snapshot > len(s.Messages) {
		snapshot = len(s.Messages)
	}
	if snapshot < len(s.Messages) {
		s.Messages = s.Messages[:snapshot]
		// Invalidate context cache since messages changed
		s.UpdateTokenCounts(nil)
	}

	// Reset tool loop detection state when rolling back
	s.lastToolCallKey = ""
	s.toolCallRepetitionCount = 0
}

// hasToolCallResponse checks if toolMessages already contains a response for the given tool call ID
// TODO: test to ensure we need this and the loops that use it
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

// processToolCalls handles executing tool calls and building response messages.
// When Forge is set, uses the envelope pattern for tool execution with audit trail.
// Otherwise falls back to direct scheduler execution.
func (s *Session) processToolCalls(ctx context.Context, toolCalls []llms.ToolCall) ([]llms.MessageContent, bool) {
	// Use Forge envelope pattern if available
	if s.forge != nil {
		return s.processToolCallsViaForge(ctx, toolCalls)
	}

	// Fallback to direct execution
	return s.processToolCallsDirect(ctx, toolCalls)
}

// forgeReplyTimeout is the maximum time to wait for a single tool execution reply.
const forgeReplyTimeout = 5 * time.Minute

// processToolCallsViaForge executes tool calls using the Forge envelope pattern.
func (s *Session) processToolCallsViaForge(ctx context.Context, toolCalls []llms.ToolCall) ([]llms.MessageContent, bool) {
	// Create reply channel for this batch
	replyChan := make(chan *LingResult, len(toolCalls))

	// Track valid tool calls sent
	sentCount := 0
	toolCallIDs := make(map[int]string) // index -> tool call ID for timeout error messages

	for _, tc := range toolCalls {
		if tc.FunctionCall == nil {
			continue
		}
		name := tc.FunctionCall.Name
		argsJSON := tc.FunctionCall.Arguments

		// Check for tool call loops
		if s.checkToolCallLoop(name, argsJSON) {
			// Still need to send something so we can collect all results
			slog.Warn("tool call loop detected", "tool", name, "count", s.toolCallRepetitionCount)
		}

		// Generate a unique Ling ID
		lingID := GenerateID("ling", s.ID, tc.ID, name)

		slog.Debug("sending envelope to forge", "tool", name, "ling_id", lingID)

		env := &LingEnvelope{
			Ling: &storage.Ling{
				LingID:     lingID,
				ToolName:   name,
				ToolInput:  storage.JSON(argsJSON),
				ToolCallID: tc.ID,
			},
			ReplyChan: replyChan,
		}
		s.forge.AddLing() <- env
		toolCallIDs[sentCount] = tc.ID
		sentCount++
	}

	// Collect results (blocks until all received or timeout)
	toolMessages := make([]llms.MessageContent, 0, sentCount)
	for i := 0; i < sentCount; i++ {
		select {
		case <-ctx.Done():
			// Context cancelled - add abort responses for remaining
			slog.Debug("context cancelled during forge execution", "received", i, "total", sentCount)
			for j := i; j < sentCount; j++ {
				// Drain any remaining results
				select {
				case result := <-replyChan:
					var content string
					if result.Error != nil {
						content = fmt.Sprintf("error: %v (aborted)", result.Error)
					} else {
						content = result.Output + " (session aborted)"
					}
					toolMessages = append(toolMessages, llms.MessageContent{
						Role: llms.ChatMessageTypeTool,
						Parts: []llms.ContentPart{llms.ToolCallResponse{
							ToolCallID: result.Ling.ToolCallID,
							Name:       result.Ling.ToolName,
							Content:    content,
						}},
					})
				default:
					// No more results available
				}
			}
			return toolMessages, true

		case <-time.After(forgeReplyTimeout):
			// Timeout waiting for Forge reply - likely Forge crashed or deadlocked
			slog.Error("forge reply timeout", "received", i, "total", sentCount, "timeout", forgeReplyTimeout)
			// Add timeout error for remaining tool calls
			for j := i; j < sentCount; j++ {
				toolMessages = append(toolMessages, llms.MessageContent{
					Role: llms.ChatMessageTypeTool,
					Parts: []llms.ContentPart{llms.ToolCallResponse{
						ToolCallID: toolCallIDs[j],
						Name:       "unknown",
						Content:    fmt.Sprintf("error: forge reply timeout after %v", forgeReplyTimeout),
					}},
				})
			}
			return toolMessages, true

		case result := <-replyChan:
			var content string
			if result.Error != nil {
				content = fmt.Sprintf("error: %v", result.Error)
			} else {
				content = result.Output
			}
			toolMessages = append(toolMessages, llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{llms.ToolCallResponse{
					ToolCallID: result.Ling.ToolCallID,
					Name:       result.Ling.ToolName,
					Content:    content,
				}},
			})
		}
	}

	return toolMessages, false
}

// processToolCallsDirect executes tool calls directly via scheduler (legacy path).
func (s *Session) processToolCallsDirect(ctx context.Context, toolCalls []llms.ToolCall) ([]llms.MessageContent, bool) {
	toolMessages := make([]llms.MessageContent, 0, len(toolCalls))

	for i, tc := range toolCalls {
		if tc.FunctionCall == nil {
			continue
		}
		name := tc.FunctionCall.Name
		argsJSON := tc.FunctionCall.Arguments

		// Check for context cancellation before processing each tool call
		select {
		case <-ctx.Done():
			// Context was cancelled - provide "session aborted" responses for remaining tool calls
			slog.Debug("context cancelled during tool execution, aborting remaining tool calls", "completed", i, "total", len(toolCalls))

			// Add abort responses for all remaining tool calls (including current one)
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

			return toolMessages, true // shouldReturn = true
		default:
			// Continue with normal processing
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
			return toolMessages, true // shouldReturn = true
		}

		tool, ok := s.toolCatalog[name]
		if !ok {
			// If the model requested an unknown tool, feed an error response back.
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

		// Execute tool and add response
		response := s.executeToolCall(ctx, tool, tc, argsJSON)
		slog.Debug("Called a tool", "tool", name, "args", argsJSON)
		toolMessages = append(toolMessages, llms.MessageContent{
			Role:  llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{response},
		})
	}

	return toolMessages, false // shouldReturn = false
}

// Ask sends a user prompt through the native loop. It returns the final assistant text.
// It handles provider-native tool calls by executing them and feeding results back.
// contextFiles contains files loaded via @ references that should be included in the prompt.
func (s *Session) Ask(ctx context.Context, prompt string, contextFiles map[string]string) (string, error) {
	// Build prompt with context if available and add to messages
	s.prepareUserMessage(prompt, contextFiles)

	// A simple loop: generate -> maybe tool calls -> tool responses -> generate.
	var finalText string
	var lastAssistant string
	var hadAnyToolCall bool
	var i int
	maxTurns := s.config.MaxTurns
	for i = 0; i < maxTurns; i++ {
		choice, err := s.generateLLMResponse(ctx, nil)
		if err != nil {
			return "", err
		}

		// Check if response was truncated due to max tokens
		if choice.StopReason == "max_tokens" {
			return choice.Content + "\n\n[Response truncated due to length limit]", nil
		}

		// Build response with reasoning content if available for display
		responseText := choice.Content
		if choice.ReasoningContent != "" {
			responseText = "<thinking>\n" + choice.ReasoningContent + "\n</thinking>\n\n" + choice.Content
		}

		// Record assistant response in message history
		if strings.TrimSpace(responseText) != "" {
			finalText = responseText
		}

		// Store thinking content properly for API compatibility
		s.appendMessage(choice)

		// Handle tool calls, if any.
		if len(choice.ToolCalls) == 0 {
			// Give the model another turn to issue tool calls if it only planned.
			// Stop if it repeats the same assistant content.
			if hadAnyToolCall || strings.TrimSpace(choice.Content) == strings.TrimSpace(lastAssistant) {
				break
			}
			lastAssistant = choice.Content
			continue
		}
		hadAnyToolCall = true

		// Process tool calls and add responses
		toolMessages, shouldReturn := s.processToolCalls(ctx, choice.ToolCalls)
		if len(toolMessages) > 0 {
			s.Messages = append(s.Messages, toolMessages...)
			// Invalidate context cache since messages changed
			s.UpdateTokenCounts(nil)
		}

		if shouldReturn {
			return finalText, nil
		}

		// Continue to next iteration to let the model incorporate tool results.
		if len(toolMessages) > 0 {
			continue
		}

		// No tool responses to send; break.
		break
	}
	if i < maxTurns {
		return finalText, nil
	}
	return fmt.Sprintf("%s\n\nEnded after %d interation", finalText, maxTurns), nil
}

// AskWithStreaming sends a user prompt and streams the response to the UI while blocking.
// Unlike Ask, it streams chunks to the UI via the notify callback.
// Unlike AskStream, it blocks until completion and returns the final response.
// This is useful for workflows that need to show progress but also wait for completion.
// contextFiles contains files loaded via @ references that should be included in the prompt.
func (s *Session) AskWithStreaming(ctx context.Context, prompt string, contextFiles map[string]string) (string, error) {
	// Build prompt with context if available and add to messages
	s.prepareUserMessage(prompt, contextFiles)

	// Notify UI that streaming has started
	if s.notify != nil {
		s.notify(StreamStartMsg{})
	}

	// A simple loop: generate -> maybe tool calls -> tool responses -> generate.
	var finalText string
	var lastAssistant string
	var hadAnyToolCall bool
	var i int
	maxTurns := s.config.MaxTurns
	for i = 0; i < maxTurns; i++ {
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

		// Create streaming function that accumulates content and notifies UI
		streamingFunc := func(ctx context.Context, chunk []byte) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			chunkStr := string(chunk)
			s.accumulatedContent.WriteString(chunkStr)
			if s.notify != nil {
				s.notify(StreamChunkMsg(chunkStr))
			}
			return nil
		}

		choice, err := s.generateLLMResponse(ctx, streamingFunc)
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

		// Use accumulated content as the response
		responseContent := s.getStreamBuffer()

		// Check if response was truncated due to max tokens
		if choice.StopReason == "max_tokens" {
			if s.notify != nil {
				s.notify(StreamMaxTokensReachedMsg{Content: responseContent})
			}
			s.appendMessage(choice)
			return responseContent + "\n\n[Response truncated due to length limit]", nil
		}

		// Record assistant response in message history with thinking content for API compatibility
		if strings.TrimSpace(responseContent) != "" {
			finalText = responseContent
		}
		s.appendMessage(choice)

		// Handle tool calls, if any.
		if len(choice.ToolCalls) == 0 {
			if hadAnyToolCall || strings.TrimSpace(responseContent) == strings.TrimSpace(lastAssistant) {
				break
			}
			lastAssistant = responseContent
			continue
		}
		hadAnyToolCall = true

		// Process tool calls and add responses
		toolMessages, shouldReturn := s.processToolCalls(ctx, choice.ToolCalls)
		if len(toolMessages) > 0 {
			s.Messages = append(s.Messages, toolMessages...)
			s.UpdateTokenCounts(nil)
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

	// Notify completion
	if s.notify != nil {
		if i >= maxTurns {
			s.notify(StreamMaxTurnsExceededMsg{MaxTurns: maxTurns})
		} else {
			s.notify(StreamCompleteMsg{})
		}
	}

	if i < maxTurns {
		return finalText, nil
	}
	return fmt.Sprintf("%s\n\nEnded after %d iterations", finalText, maxTurns), nil
}

// AskStream sends a user prompt through the native loop with streaming support.
// It launches the streaming process in a goroutine and returns immediately.
// Sends PromptReply messages directly to the reply channel and closes it when done.
// Supports cancellation via the provided context.
// contextFiles contains files loaded via @ references that should be included in the prompt.
func (s *Session) AskStream(ctx context.Context, prompt string, reply chan<- PromptReply, edictID string, contextFiles map[string]string) {
	// Launch streaming in a goroutine to avoid blocking the UI
	go func() {
		// Ensure channel close on exit
		defer close(reply)

		// Build prompt with context if available and add to messages
		s.prepareUserMessage(prompt, contextFiles)

		// Signal streaming has started
		reply <- PromptReply{Type: ReplyStreamStart, Data: edictID}

		// A simple loop: generate -> maybe tool calls -> tool responses -> generate.
		// Cap at a few iterations to avoid infinite loops.
		var i int
		maxTurns := s.config.MaxTurns
		for i = 0; i < maxTurns; i++ {
			s.resetStreamBuffer()

			// Check for cancellation
			select {
			case <-ctx.Done():
				// Streaming was cancelled - add any accumulated content to message history
				accumulatedText := s.getStreamBuffer()
				if strings.TrimSpace(accumulatedText) != "" {
					s.appendMessage(&llms.ContentChoice{Content: accumulatedText})
				}
				reply <- PromptReply{Type: ReplyStreamComplete}
				return
			default:
				// Continue with streaming
			}

			// Create streaming function that accumulates content and sends to reply channel
			streamingFunc := func(ctx context.Context, chunk []byte) error {
				// Check for cancellation in streaming callback
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				chunkStr := string(chunk)
				s.accumulatedContent.WriteString(chunkStr)
				reply <- PromptReply{Type: ReplyStreamChunk, Content: chunkStr}
				return nil
			}

			choice, err := s.generateLLMResponse(ctx, streamingFunc)
			if err != nil {
				// Check if this was a cancellation
				if ctx.Err() != nil {
					accumulatedText := s.getStreamBuffer()
					if strings.TrimSpace(accumulatedText) != "" {
						s.appendMessage(&llms.ContentChoice{Content: accumulatedText})
					}
					reply <- PromptReply{Type: ReplyStreamComplete}
					return
				}

				// Regular error
				reply <- PromptReply{Type: ReplyError, Error: err}
				return
			}

			// Use accumulated content as the response
			responseContent := s.getStreamBuffer()

			// Check if response was truncated due to max tokens
			if choice.StopReason == "max_tokens" {
				// Notify via legacy callback if set (for non-Shogunate paths)
				if s.notify != nil {
					s.notify(StreamMaxTokensReachedMsg{Content: responseContent})
				}
				s.appendMessage(choice)
				break
			}

			// Add the assistant message with content, thinking, and tool calls to message history
			s.appendMessage(choice)

			// Handle tool calls, if any.
			if len(choice.ToolCalls) == 0 {
				// No tool calls - streaming is complete
				break
			}

			// Process tool calls and add responses
			toolMessages, shouldReturn := s.processToolCalls(ctx, choice.ToolCalls)
			if len(toolMessages) > 0 {
				s.Messages = append(s.Messages, toolMessages...)
				// Invalidate context cache since messages changed
				s.UpdateTokenCounts(nil)
			}

			if shouldReturn {
				break
			}

			// Continue to next iteration to let the model incorporate tool results.
			if len(toolMessages) > 0 {
				continue
			}

			// No tool responses to send; break.
			break
		}

		// Send completion
		reply <- PromptReply{Type: ReplyStreamComplete}
	}()
}

// sessBuildEnvBlock constructs a markdown summary of the OS, shell, and key paths.
func sessBuildEnvBlock(repoInfo repo.RepoInfo) string {
	var env strings.Builder

	env.WriteString(fmt.Sprintf("- **OS:** %s\n", sandboxOS))
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		env.WriteString(fmt.Sprintf("- **Working copy path** %s\n", cwd))
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	env.WriteString(fmt.Sprintf("- **Shell:** %s\n", shell))

	if repoInfo.Branch != "" {
		env.WriteString(fmt.Sprintf("- **Branch:** %s\n", repoInfo.Branch))
	}

	if repoInfo.IsWorktree && repoInfo.Branch != "dev" {
		env.WriteString(
			`\n\n**IMPORTANT:** Working on worktree so commits will be quashed.
Feel free to commit whenever you can summarize the changes in a meaningful commit message.`)
	}

	return env.String()
}

// readProjectContext reads the contents of the agents file (AGENTS.md or CLAUDE.md) from the current working directory.
func readProjectContext(agentsFile string) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	path := filepath.Join(wd, agentsFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// buildLLMTools returns the LLM tool/function definitions and a catalog by name for execution.
// Tools are passed in rather than built here to avoid dependency on main package.
func buildLLMTools(tools []Tool) ([]llms.Tool, map[string]lctools.Tool) {
	// Map our concrete tools by name for execution.
	execCatalog := map[string]lctools.Tool{}
	defs := make([]llms.Tool, 0, len(tools))

	for i := range tools {
		tool := tools[i]
		//nolint:typecheck // Tool interface is correctly defined in tools.go
		execCatalog[tool.Name()] = tool

		// Automatically generate the LLM tool definition from the tool's metadata
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

// GetSessionDuration returns the duration since the session started
func (s *Session) GetSessionDuration() time.Duration {
	return time.Since(s.startTime)
}

// UpdateTokenCounts recalculates and stores token counts for all context components.
// contextFiles is optional - pass nil when no context files are loaded.
func (s *Session) UpdateTokenCounts(contextFiles map[string]string) {
	s.systemPromptTokens = s.CountSystemPromptTokens()
	s.systemToolsTokens = s.CountSystemToolsTokens()
	// Only update memory files tokens when context files are explicitly provided
	if contextFiles != nil {
		s.memoryFilesTokens = s.CountMemoryFilesTokens(contextFiles)
	}
	s.messagesTokens = s.CountMessagesTokens()
}

// GetContextUsagePercent returns the percentage of context used (0-100)
func (s *Session) GetContextUsagePercent() float64 {
	info := s.GetContextInfo()
	if info.TotalTokens <= 0 {
		return 0
	}
	return (float64(info.UsedTokens) / float64(info.TotalTokens)) * 100
}

// CompactHistory summarizes the conversation history to reduce context usage
// It uses the high-end model to create a comprehensive summary that includes:
// - All diffs/changes made to files
// - Key decisions and outcomes
// - Important technical details
// The summary replaces the conversation history while preserving the system message
func (s *Session) CompactHistory(ctx context.Context, compactPrompt string) (string, error) {
	if len(s.Messages) <= 2 {
		return "", fmt.Errorf("not enough conversation history to compact")
	}

	// Build the content to summarize
	var contentBuilder strings.Builder

	// Collect all diffs and file changes
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

	// Collect conversation messages (excluding tool calls)
	contentBuilder.WriteString("## Conversation History\n\n")
	for i := 1; i < len(s.Messages); i++ {
		msg := s.Messages[i]

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
			// Only include text content, skip tool calls
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					contentBuilder.WriteString(textPart.Text)
					contentBuilder.WriteString("\n\n")
				}
			}
		}
	}

	// Build the compaction request
	fullPrompt := fmt.Sprintf("%s\n\n---\n\n%s", compactPrompt, contentBuilder.String())

	// Save the current messages
	originalMessages := s.Messages
	systemMessage := s.Messages[0]

	// Create a temporary message history with just the system message and compaction request
	s.Messages = []llms.MessageContent{
		systemMessage,
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart(fullPrompt)},
		},
	}

	// Generate the summary using the LLM
	choice, err := s.generateLLMResponse(ctx, nil)
	if err != nil {
		// Restore original messages on error
		s.Messages = originalMessages
		s.UpdateTokenCounts(nil)
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	summary := choice.Content
	if choice.ReasoningContent != "" {
		summary = choice.ReasoningContent + "\n\n" + choice.Content
	}

	// Replace the conversation history with the summary
	s.Messages = []llms.MessageContent{
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

	// Reset tool call tracking
	s.lastToolCallKey = ""
	s.toolCallRepetitionCount = 0

	// Invalidate context cache since messages changed
	s.UpdateTokenCounts(nil)

	return summary, nil
}

// extractFileChanges extracts all file changes from tool call responses
func (s *Session) extractFileChanges() map[string][]string {
	changes := make(map[string][]string)

	for _, msg := range s.Messages {
		if msg.Role != llms.ChatMessageTypeTool {
			continue
		}

		for _, part := range msg.Parts {
			if toolResp, ok := part.(llms.ToolCallResponse); ok {
				// Track write_file and replace_text operations
				if toolResp.Name == "write_file" || toolResp.Name == "replace_text" {
					// Try to extract the file path from the response
					// The response format varies, but we can try to parse it
					content := toolResp.Content
					if strings.Contains(content, "Successfully") || strings.Contains(content, "wrote") {
						// Extract file path - this is a simple heuristic
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

type SessionIndex struct {
	Sessions []Session `json:"sessions"`
}

// GenerateSessionID creates a unique session ID with timestamp prefix.
func GenerateSessionID() string {
	timestamp := time.Now().Format("2006-01-02-150405")

	randomBytes := make([]byte, 4)
	crand.Read(randomBytes)
	suffix := hex.EncodeToString(randomBytes)

	return fmt.Sprintf("%s-%s", timestamp, suffix)
}

// --- Shogunate Integration ---

// SessionLLMClient implements LLMClient using a Session.
// This allows Shogunate ministers to use the Session's LLM capabilities.
type SessionLLMClient struct {
	session *Session
}

// NewSessionLLMClient creates an LLM client wrapper around a Session.
func NewSessionLLMClient(session *Session) *SessionLLMClient {
	return &SessionLLMClient{session: session}
}

// Generate produces a response using the Session's LLM.
func (c *SessionLLMClient) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if systemPrompt != "" {
		c.session.ReplaceSystemPrompt(systemPrompt)
	}

	response, err := c.session.Ask(ctx, userPrompt, nil)
	if err != nil {
		return "", fmt.Errorf("session ask: %w", err)
	}

	return response, nil
}
