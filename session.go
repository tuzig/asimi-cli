package main

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
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
	lctools "github.com/tmc/langchaingo/tools"
)

// NotifyFunc is a function that handles notifications
type NotifyFunc func(any)

// Session is a lightweight chat loop that uses llms.Model directly
// and native provider tool/function-calling. It executes tools via the
// existing CoreToolScheduler and keeps conversation state locally.
type Session struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	LastUpdated  time.Time `json:"last_updated"`
	FirstPrompt  string    `json:"first_prompt"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	WorkingDir   string    `json:"working_dir"`

	Messages     []llms.MessageContent `json:"messages"`
	ContextFiles map[string]string     `json:"context_files"`

	llm                     llms.Model            `json:"-"`
	toolCatalog             map[string]lctools.Tool `json:"-"`
	toolDefs                []llms.Tool           `json:"-"`
	lastToolCallKey         string                `json:"-"`
	toolCallRepetitionCount int                   `json:"-"`
	scheduler               *CoreToolScheduler    `json:"-"`
	notify                  NotifyFunc            `json:"-"`
	accumulatedContent      strings.Builder       `json:"-"`
	config                  *LLMConfig            `json:"-"`
}

// resetStreamBuffer safely resets the accumulated content buffer
func (s *Session) resetStreamBuffer() {
	s.accumulatedContent.Reset()
}

// getStreamBuffer returns the current accumulated content and optionally resets it
func (s *Session) getStreamBuffer(reset bool) string {
	content := s.accumulatedContent.String()
	if reset {
		s.accumulatedContent.Reset()
	}
	return content
}

// notification messages
type streamChunkMsg string
type streamStartMsg struct{}
type streamCompleteMsg struct{}
type streamInterruptedMsg struct{ partialContent string }
type streamErrorMsg struct{ err error }
type streamMaxTurnsExceededMsg struct{ maxTurns int }
type streamMaxTokensReachedMsg struct{ content string }

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
func NewSession(llm llms.Model, cfg *Config, toolNotify NotifyFunc) (*Session, error) {
	now := time.Now()
	workingDir, _ := os.Getwd()
	
	s := &Session{
		ID:          generateSessionID(),
		CreatedAt:   now,
		LastUpdated: now,
		WorkingDir:  workingDir,
		llm:         llm,
		toolCatalog: map[string]lctools.Tool{},
		notify:      toolNotify,
	}
	if cfg != nil {
		s.config = &cfg.LLM
		s.Provider = cfg.LLM.Provider
		s.Model = cfg.LLM.Model
		// Set default maxTurns if not configured
		if s.config.MaxTurns <= 0 {
			s.config.MaxTurns = 50
		}
	} else {
		// Create default config if none provided
		s.config = &LLMConfig{
			MaxTurns: 50,
		}
	}

	// Build system prompt from the existing template and partials, same as the agent.
	partials := make(map[string]any, len(sessPromptPartials))
	for k, v := range sessPromptPartials {
		partials[k] = v
	}
	partials["Env"] = sessBuildEnvBlock()

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
	var parts []llms.ContentPart
	if s.config != nil && s.config.Provider == "anthropic" {
		parts = append(parts, llms.TextPart("You are Claude Code, Anthropic's official CLI for Claude."))
	}
	parts = append(parts, llms.TextPart(sys))

	s.Messages = append(s.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeSystem,
		Parts: parts,
	})

	// Build tool schema for the model and execution catalog for the scheduler.
	s.toolDefs, s.toolCatalog = buildLLMTools()
	s.scheduler = NewCoreToolScheduler(s.notify)
	s.ContextFiles = make(map[string]string)

	// Add AGENTS.md as a persistent context file if it exists
	projectContext := readProjectContext()
	if projectContext != "" {
		s.ContextFiles["AGENTS.md"] = projectContext
	}
	return s, nil
}

// AddContextFile adds file content to the context for the next prompt
func (s *Session) AddContextFile(path, content string) {
	s.ContextFiles[path] = content
}

// ClearContext removes all file content from the context except AGENTS.md
func (s *Session) ClearContext() {
	// Preserve AGENTS.md if it exists
	agentsContent, hasAgents := s.ContextFiles["AGENTS.md"]
	s.ContextFiles = make(map[string]string)
	if hasAgents {
		s.ContextFiles["AGENTS.md"] = agentsContent
	}
}

// ClearHistory clears the conversation history but keeps the system message and AGENTS.md
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

	s.ClearContext()
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

// buildPromptWithContext builds a prompt that includes all file content
func (s *Session) buildPromptWithContext(userPrompt string) string {
	if len(s.ContextFiles) == 0 {
		return userPrompt
	}

	var fileContents []string
	for path, content := range s.ContextFiles {
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

// prepareUserMessage builds the prompt with context and adds it to the message history
func (s *Session) prepareUserMessage(prompt string) {
	fullPrompt := s.buildPromptWithContext(prompt)
	s.Messages = append(s.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart(fullPrompt)},
	})
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
		callOptsWithChoice = append(callOptsWithChoice, llms.WithStreamingFunc(streamingFunc))
	}
	// Attempt with explicit tool choice first.
	resp, err := s.llm.GenerateContent(ctx, s.Messages, callOptsWithChoice...)
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response choices")
	}
	return resp.Choices[0], nil
}

// appendMessages adds LLM response content and tool calls to the message history
func (s *Session) appendMessages(content string, toolCalls []llms.ToolCall) {
	// Build the assistant message parts
	var parts []llms.ContentPart

	// Add text content if present
	if strings.TrimSpace(content) != "" {
		parts = append(parts, llms.TextPart(content))
	}

	// Add tool calls if present
	for _, toolCall := range toolCalls {
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
	}
}

// executeToolCall executes a single tool call and adds the response to the message
func (s *Session) executeToolCall(ctx context.Context, tool lctools.Tool, tc llms.ToolCall, argsJSON string, toolResponseMsg *llms.MessageContent) {
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
		toolResponseMsg.Parts = append(toolResponseMsg.Parts, llms.ToolCallResponse{
			ToolCallID: tc.ID,
			Name:       tc.FunctionCall.Name,
			Content:    fmt.Sprintf("Error: %v", callErr),
		})
	} else {
		toolResponseMsg.Parts = append(toolResponseMsg.Parts, llms.ToolCallResponse{
			ToolCallID: tc.ID,
			Name:       tc.FunctionCall.Name,
			Content:    out,
		})
	}
}

// processToolCalls handles executing tool calls and building response messages
func (s *Session) processToolCalls(ctx context.Context, toolCalls []llms.ToolCall, finalText string) (llms.MessageContent, bool) {
	toolResponseMsg := llms.MessageContent{Role: llms.ChatMessageTypeTool}

	for _, tc := range toolCalls {
		if tc.FunctionCall == nil {
			continue
		}
		name := tc.FunctionCall.Name
		argsJSON := tc.FunctionCall.Arguments

		// Check for tool call loops
		if s.checkToolCallLoop(name, argsJSON) {
			return toolResponseMsg, true // shouldReturn = true
		}

		tool, ok := s.toolCatalog[name]
		if !ok {
			// If the model requested an unknown tool, feed an error response back.
			toolResponseMsg.Parts = append(toolResponseMsg.Parts, llms.ToolCallResponse{
				ToolCallID: tc.ID,
				Name:       name,
				Content:    fmt.Sprintf("error: unknown tool %q", name),
			})
			continue
		}

		// Execute tool and add response
		s.executeToolCall(ctx, tool, tc, argsJSON, &toolResponseMsg)
	}

	return toolResponseMsg, false // shouldReturn = false
}

// Ask sends a user prompt through the native loop. It returns the final assistant text.
// It handles provider-native tool calls by executing them and feeding results back.
func (s *Session) Ask(ctx context.Context, prompt string) (string, error) {
	// Build prompt with context if available and add to messages
	s.prepareUserMessage(prompt)
	// Clear context after building the prompt
	defer s.ClearContext()

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

		// Build response with reasoning content if available
		responseText := choice.Content
		if choice.ReasoningContent != "" {
			responseText = "<thinking>\n" + choice.ReasoningContent + "\n</thinking>\n\n" + choice.Content
		}

		// Record assistant response in message history
		if strings.TrimSpace(responseText) != "" {
			finalText = responseText
		}
		s.appendMessages(responseText, choice.ToolCalls)

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
		toolResponseMsg, shouldReturn := s.processToolCalls(ctx, choice.ToolCalls, finalText)
		if shouldReturn {
			return finalText, nil
		}

		// Append the aggregated tool responses and continue the loop.
		if len(toolResponseMsg.Parts) > 0 {
			s.Messages = append(s.Messages, toolResponseMsg)
			// Continue to next iteration to let the model incorporate tool results.
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

// AskStream sends a user prompt through the native loop with streaming support.
// It launches the streaming process in a goroutine and returns immediately.
// Uses the notify callback to send streaming chunks as they arrive.
// Supports cancellation via the provided context.
func (s *Session) AskStream(ctx context.Context, prompt string) {
	// Launch streaming in a goroutine to avoid blocking the UI
	go func() {
		// Ensure cleanup on exit
		defer func() {
			s.ClearContext()
		}()

		// Build prompt with context if available and add to messages
		s.prepareUserMessage(prompt)

		// Notify UI that streaming has started
		if s.notify != nil {
			s.notify(streamStartMsg{})
		}

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
				accumulatedText := s.getStreamBuffer(false)
				if strings.TrimSpace(accumulatedText) != "" {
					s.appendMessages(accumulatedText, nil)
				}
				if s.notify != nil {
					s.notify(streamInterruptedMsg{partialContent: accumulatedText})
				}
				return
			default:
				// Continue with streaming
			}

			// Create streaming function that accumulates content and notifies UI
			streamingFunc := func(ctx context.Context, chunk []byte) error {
				// Check for cancellation in streaming callback
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				chunkStr := string(chunk)
				s.accumulatedContent.WriteString(chunkStr)
				if s.notify != nil {
					s.notify(streamChunkMsg(chunkStr))
				}
				return nil
			}

			choice, err := s.generateLLMResponse(ctx, streamingFunc)
			if err != nil {
				// Check if this was a cancellation
				if ctx.Err() != nil {
					accumulatedText := s.getStreamBuffer(false)
					if strings.TrimSpace(accumulatedText) != "" {
						s.appendMessages(accumulatedText, nil)
					}
					if s.notify != nil {
						s.notify(streamInterruptedMsg{partialContent: accumulatedText})
					}
					return
				}

				// Regular error
				if s.notify != nil {
					s.notify(streamErrorMsg{err: err})
				}
				return
			}

			// Use accumulated content as the response
			responseContent := s.getStreamBuffer(false)

			// Check if response was truncated due to max tokens
			if choice.StopReason == "max_tokens" {
				if s.notify != nil {
					s.notify(streamMaxTokensReachedMsg{content: responseContent})
				}
				s.appendMessages(responseContent, choice.ToolCalls)
				break
			}

			// Add reasoning content if available (for models like deepseek-reasoner)
			if choice.ReasoningContent != "" && s.notify != nil {
				s.notify(streamChunkMsg("\n\n<thinking>\n" + choice.ReasoningContent + "\n</thinking>\n\n"))
			}

			// Add the assistant message with content and tool calls to message history
			s.appendMessages(responseContent, choice.ToolCalls)

			// Handle tool calls, if any.
			if len(choice.ToolCalls) == 0 {
				// No tool calls - streaming is complete
				break
			}

			// Process tool calls and add responses
			toolResponseMsg, shouldReturn := s.processToolCalls(ctx, choice.ToolCalls, responseContent)
			if shouldReturn {
				break
			}

			// Append the aggregated tool responses and continue the loop.
			if len(toolResponseMsg.Parts) > 0 {
				s.Messages = append(s.Messages, toolResponseMsg)
				// Continue to next iteration to let the model incorporate tool results.
				continue
			}

			// No tool responses to send; break.
			break
		}

		// Check if we exceeded max turns and send appropriate notification
		if s.notify != nil {
			if i >= maxTurns {
				s.notify(streamMaxTurnsExceededMsg{maxTurns: maxTurns})
			} else {
				s.notify(streamCompleteMsg{})
			}
		}
	}()
}

// parseReActAction extracts a tool name and JSON arguments from text containing lines like:
// "Action: tool_name" and "Action Input: { ... }".
func parseReActAction(text string) (name string, argsJSON string, ok bool) {
	if text == "" {
		return "", "", false
	}
	var tool string
	var args string
	lines := strings.Split(text, "\n")
	for _, ln := range lines {
		l := strings.TrimSpace(ln)
		if strings.HasPrefix(strings.ToLower(l), "action:") {
			tool = strings.TrimSpace(l[len("Action:"):])
		} else if strings.HasPrefix(strings.ToLower(l), "action input:") {
			args = strings.TrimSpace(l[len("Action Input:"):])
		}
	}
	if tool == "" || args == "" {
		return "", "", false
	}
	return tool, args, true
}

// sessBuildEnvBlock constructs a minimal <env> XML snippet containing OS and paths.
func sessBuildEnvBlock() string {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	root := findProjectRoot(cwd)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	return fmt.Sprintf(`
 <os>%s</os>
 <paths>
  <cwd>%s</cwd>
  <project_root>%s</project_root>
  <home>%s</home>
 </paths>`, runtime.GOOS, shell, root, home)
}

// readProjectContext reads the contents of AGENTS.md from the current working directory.
func readProjectContext() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	path := filepath.Join(wd, "AGENTS.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// buildLLMTools returns the LLM tool/function definitions and a catalog by name for execution.
func buildLLMTools() ([]llms.Tool, map[string]lctools.Tool) {
	// Map our concrete tools by name for execution.
	execCatalog := map[string]lctools.Tool{}
	for _, t := range availableTools {
		execCatalog[t.Name()] = t
	}

	// Helper to produce a basic JSON schema for function parameters.
	obj := func(props map[string]any, required []string) map[string]any {
		m := map[string]any{
			"type":       "object",
			"properties": props,
		}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}

	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	defs := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "read_file",
				Description: "Reads a file and returns its content.",
				Parameters: obj(map[string]any{
					"path": str("Absolute or relative path to the file"),
				}, []string{"path"}),
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "write_file",
				Description: "Writes content to a file, creating or overwriting it.",
				Parameters: obj(map[string]any{
					"path":    str("Target file path"),
					"content": str("File contents to write"),
				}, []string{"path", "content"}),
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "list_files",
				Description: "Lists the contents of a directory.",
				Parameters: obj(map[string]any{
					"path": str("Directory path (defaults to '.')"),
				}, nil),
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "replace_text",
				Description: "Replaces all occurrences of a string in a file with another string.",
				Parameters: obj(map[string]any{
					"path":     str("File path"),
					"old_text": str("Text to replace"),
					"new_text": str("Replacement text"),
				}, []string{"path", "old_text", "new_text"}),
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "run_shell_command",
				Description: "Executes a shell command in an optional directory.",
				Parameters: obj(map[string]any{
					"command":     str("Shell command to run"),
					"description": str("Short description of the command"),
					"path":        str("Working directory for the command"),
				}, []string{"command"}),
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "read_many_files",
				Description: "Reads content from multiple files specified by wildcard paths.",
				Parameters: obj(map[string]any{
					"paths": map[string]any{
						"type":        "array",
						"description": "Array of file paths or glob patterns to read",
						"items": map[string]any{
							"type":        "string",
							"description": "A file path or glob pattern",
						},
					},
				}, []string{"paths"}),
			},
		},
	}

	return defs, execCatalog
}

// Utility to pretty-print any struct for debug (unused but handy during dev).
func toJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

type SessionIndex struct {
	Sessions []Session `json:"sessions"`
}

type SessionStore struct {
	storageDir  string
	maxSessions int
	maxAgeDays  int
	saveChan    chan *Session
	stopChan    chan struct{}
}

func generateSessionID() string {
	timestamp := time.Now().Format("2006-01-02-150405")
	
	randomBytes := make([]byte, 4)
	crand.Read(randomBytes)
	suffix := hex.EncodeToString(randomBytes)
	
	return fmt.Sprintf("%s-%s", timestamp, suffix)
}

func NewSessionStore(maxSessions, maxAgeDays int) (*SessionStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	storageDir := filepath.Join(homeDir, ".local", "share", "asimi", "sessions")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session storage directory: %w", err)
	}

	store := &SessionStore{
		storageDir:  storageDir,
		maxSessions: maxSessions,
		maxAgeDays:  maxAgeDays,
		saveChan:    make(chan *Session, 100),
		stopChan:    make(chan struct{}),
	}

	if err := store.CleanupOldSessions(); err != nil {
		fmt.Printf("Warning: failed to cleanup old sessions: %v\n", err)
	}

	go store.saveWorker()

	return store, nil
}

func (store *SessionStore) saveWorker() {
	for {
		select {
		case session := <-store.saveChan:
			if err := store.saveSessionSync(session); err != nil {
				fmt.Printf("Warning: failed to save session: %v\n", err)
			}
		case <-store.stopChan:
			for len(store.saveChan) > 0 {
				session := <-store.saveChan
				if err := store.saveSessionSync(session); err != nil {
					fmt.Printf("Warning: failed to save session: %v\n", err)
				}
			}
			return
		}
	}
}

func (store *SessionStore) SaveSession(session *Session) {
	if session != nil {
		select {
		case store.saveChan <- session:
		default:
			fmt.Printf("Warning: save channel full, skipping save\n")
		}
	}
}

func (store *SessionStore) Close() {
	close(store.stopChan)
}

func (store *SessionStore) saveSessionSync(session *Session) error {
	if session == nil {
		return fmt.Errorf("cannot save nil session")
	}

	hasUserMessage := false
	for _, msg := range session.Messages {
		if msg.Role == llms.ChatMessageTypeHuman {
			hasUserMessage = true
			break
		}
	}
	if !hasUserMessage {
		return nil
	}

	if session.ID == "" {
		session.ID = generateSessionID()
		session.CreatedAt = time.Now()
		workingDir, _ := os.Getwd()
		session.WorkingDir = workingDir
	}

	if session.FirstPrompt == "" {
		for _, msg := range session.Messages {
			if msg.Role == llms.ChatMessageTypeHuman {
				for _, part := range msg.Parts {
					if textPart, ok := part.(llms.TextContent); ok {
						session.FirstPrompt = textPart.Text
						if len(session.FirstPrompt) > 60 {
							session.FirstPrompt = session.FirstPrompt[:57] + "..."
						}
						break
					}
				}
				if session.FirstPrompt != "" {
					break
				}
			}
		}
	}

	session.LastUpdated = time.Now()

	sessionDir := filepath.Join(store.storageDir, "session-"+session.ID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	sessionFile := filepath.Join(sessionDir, "session.json")
	sessionJSON, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}
	if err := os.WriteFile(sessionFile, sessionJSON, 0644); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	if err := store.updateIndex(session); err != nil {
		return fmt.Errorf("failed to update index: %w", err)
	}

	return nil
}

func (store *SessionStore) LoadSession(id string) (*Session, error) {
	sessionDir := filepath.Join(store.storageDir, "session-"+id)
	sessionFile := filepath.Join(sessionDir, "session.json")

	data, err := os.ReadFile(sessionFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	return &session, nil
}

func (store *SessionStore) ListSessions(limit int) ([]Session, error) {
	index, err := store.loadIndex()
	if err != nil {
		return nil, err
	}

	sort.Slice(index.Sessions, func(i, j int) bool {
		return index.Sessions[i].LastUpdated.After(index.Sessions[j].LastUpdated)
	})

	if limit > 0 && len(index.Sessions) > limit {
		return index.Sessions[:limit], nil
	}

	return index.Sessions, nil
}

func (store *SessionStore) CleanupOldSessions() error {
	index, err := store.loadIndex()
	if err != nil {
		return err
	}

	sort.Slice(index.Sessions, func(i, j int) bool {
		return index.Sessions[i].LastUpdated.After(index.Sessions[j].LastUpdated)
	})

	var sessionsToKeep []Session
	cutoffTime := time.Now().AddDate(0, 0, -store.maxAgeDays)

	for i, session := range index.Sessions {
		if i < store.maxSessions && session.LastUpdated.After(cutoffTime) {
			sessionsToKeep = append(sessionsToKeep, session)
		} else {
			sessionDir := filepath.Join(store.storageDir, "session-"+session.ID)
			if err := os.RemoveAll(sessionDir); err != nil {
				fmt.Printf("Warning: failed to remove old session %s: %v\n", session.ID, err)
			}
		}
	}

	index.Sessions = sessionsToKeep
	return store.saveIndex(index)
}

func (store *SessionStore) loadIndex() (*SessionIndex, error) {
	indexFile := filepath.Join(store.storageDir, "index.json")
	
	if _, err := os.Stat(indexFile); os.IsNotExist(err) {
		return &SessionIndex{Sessions: []Session{}}, nil
	}

	data, err := os.ReadFile(indexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	var index SessionIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to unmarshal index: %w", err)
	}

	return &index, nil
}

func (store *SessionStore) saveIndex(index *SessionIndex) error {
	indexFile := filepath.Join(store.storageDir, "index.json")
	
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	if err := os.WriteFile(indexFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	return nil
}

func (store *SessionStore) updateIndex(session *Session) error {
	index, err := store.loadIndex()
	if err != nil {
		return err
	}

	found := false
	for i, s := range index.Sessions {
		if s.ID == session.ID {
			index.Sessions[i] = *session
			found = true
			break
		}
	}

	if !found {
		index.Sessions = append(index.Sessions, *session)
	}

	return store.saveIndex(index)
}

func formatRelativeTime(t time.Time) string {
	now := time.Now()
	
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return fmt.Sprintf("Today %s", t.Format("15:04"))
	}
	
	yesterday := now.AddDate(0, 0, -1)
	if t.Year() == yesterday.Year() && t.YearDay() == yesterday.YearDay() {
		return fmt.Sprintf("Yesterday %s", t.Format("15:04"))
	}
	
	if t.Year() == now.Year() {
		return t.Format("Jan 2, 15:04")
	}
	
	return t.Format("Jan 2 2006, 15:04")
}

func FormatSessionList(sessions []Session) string {
	if len(sessions) == 0 {
		return "No previous sessions found. Start chatting to create a new session!"
	}

	var b strings.Builder
	b.WriteString("Recent Sessions:\n\n")

	for i, session := range sessions {
		messageCount := len(session.Messages)
		b.WriteString(fmt.Sprintf("%2d. [%s] %s\n", i+1, formatRelativeTime(session.LastUpdated), session.FirstPrompt))
		b.WriteString(fmt.Sprintf("    %d messages • %s", messageCount, session.Model))
		
		currentDir, _ := os.Getwd()
		if session.WorkingDir != "" && session.WorkingDir != currentDir {
			shortPath := session.WorkingDir
			homeDir, _ := os.UserHomeDir()
			if homeDir != "" {
				shortPath = strings.Replace(shortPath, homeDir, "~", 1)
			}
			b.WriteString(fmt.Sprintf(" • %s", shortPath))
		}
		
		b.WriteString("\n")
		if i < len(sessions)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (store *SessionStore) Flush() {
	for len(store.saveChan) > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
}
