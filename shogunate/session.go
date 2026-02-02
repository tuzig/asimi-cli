package shogunate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/tmc/langchaingo/llms"
)

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

	model        llms.Model
	config       *internalconfig.LLMConfig
	repoInfo     repo.RepoInfo
	tools        []Tool
	messages     []llms.MessageContent
	notify       NotifyFunc
	systemPrompt string

	// Tool execution
	toolCatalog map[string]Tool
	toolDefs    []llms.Tool
	scheduler   *CoreToolScheduler

	// Streaming
	accumulatedContent strings.Builder

	// Loop detection
	lastToolCallKey         string
	toolCallRepetitionCount int
}

// NewSession creates a new minister session
func NewSession(
	model llms.Model,
	cfg *SessionConfig,
	repoInfo repo.RepoInfo,
	tools []Tool,
	scheduler *CoreToolScheduler,
	notify NotifyFunc,
	systemPrompt string,
) (*Session, error) {
	session := &Session{
		ID:           GenerateID("session", time.Now().String()),
		CreatedAt:    time.Now(),
		LastUpdated:  time.Now(),
		model:        model,
		repoInfo:     repoInfo,
		tools:        tools,
		messages:     []llms.MessageContent{},
		notify:       notify,
		systemPrompt: systemPrompt,
		toolCatalog:  make(map[string]Tool),
	}

	// Set config
	if cfg != nil {
		session.config = &cfg.LLM
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
		session.scheduler = NewCoreToolScheduler(notify)
	}

	return session, nil
}

// Messages returns the session messages
func (s *Session) Messages() []llms.MessageContent {
	return s.messages
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
func (s *Session) generateLLMResponse(ctx context.Context, streamingFunc func(ctx context.Context, chunk []byte) error) (*llms.ContentChoice, error) {
	var callOpts []llms.CallOption
	if len(s.toolDefs) > 0 {
		callOpts = append(callOpts, llms.WithTools(s.toolDefs), llms.WithMaxTokens(64000))
		callOpts = append(callOpts, llms.WithToolChoice("auto"))
	}

	if streamingFunc != nil {
		callOpts = append(callOpts, llms.WithStreamingFunc(streamingFunc))
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

		// Create streaming function
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

// --- Stream notification message types ---

// StreamChunkMsg contains a streaming text chunk from the LLM
type StreamChunkMsg string

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

// CoreToolScheduler handles tool execution with rate limiting
type CoreToolScheduler struct {
	notify NotifyFunc
}

// NewCoreToolScheduler creates a new tool scheduler
func NewCoreToolScheduler(notify NotifyFunc) *CoreToolScheduler {
	return &CoreToolScheduler{notify: notify}
}

// SetNotify sets the notification callback
func (s *CoreToolScheduler) SetNotify(notify NotifyFunc) {
	s.notify = notify
}

// Schedule schedules a tool for execution and returns a result channel
func (s *CoreToolScheduler) Schedule(tool Tool, argsJSON string) <-chan ToolResult {
	ch := make(chan ToolResult, 1)

	go func() {
		// Notify that tool is being executed
		if s.notify != nil {
			s.notify(ToolCallExecutingMsg{
				Name:   tool.Name(),
				Input:  argsJSON,
				Format: tool.Format,
			})
		}

		out, err := tool.Call(context.Background(), argsJSON)

		// Notify result
		if s.notify != nil {
			if err != nil {
				s.notify(ToolCallErrorMsg{
					Name:   tool.Name(),
					Input:  argsJSON,
					Error:  err,
					Format: tool.Format,
				})
			} else {
				s.notify(ToolCallSuccessMsg{
					Name:   tool.Name(),
					Input:  argsJSON,
					Output: out,
					Format: tool.Format,
				})
			}
		}

		ch <- ToolResult{Output: out, Error: err}
		close(ch)
	}()

	return ch
}

// Tool notification message types
type (
	ToolCallScheduledMsg struct {
		Name   string
		Input  string
		Format func(input, result string, err error) string
	}
	ToolCallExecutingMsg struct {
		Name   string
		Input  string
		Format func(input, result string, err error) string
	}
	ToolCallSuccessMsg struct {
		Name   string
		Input  string
		Output string
		Format func(input, result string, err error) string
	}
	ToolCallErrorMsg struct {
		Name   string
		Input  string
		Error  error
		Format func(input, result string, err error) string
	}
	ToolCallAbortedMsg struct {
		Name  string
		Input string
	}
)

// --- JSON Helper ---

// JSON is a map for storing arbitrary JSON data
type JSON = map[string]any

// MustMarshalJSON marshals a value to JSON, panicking on error
func MustMarshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}
