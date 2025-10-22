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
	debug "runtime/debug"
	"sort"
	"strings"
	"sync"
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
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	LastUpdated time.Time `json:"last_updated"`
	FirstPrompt string    `json:"first_prompt"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	WorkingDir  string    `json:"working_dir"`
	ProjectSlug string    `json:"project_slug,omitempty"`

	Messages     []llms.MessageContent `json:"messages"`
	ContextFiles map[string]string     `json:"context_files"`
	messages     []llms.MessageContent `json:"-"`

	llm                     llms.Model              `json:"-"`
	toolCatalog             map[string]lctools.Tool `json:"-"`
	toolDefs                []llms.Tool             `json:"-"`
	lastToolCallKey         string                  `json:"-"`
	toolCallRepetitionCount int                     `json:"-"`
	scheduler               *CoreToolScheduler      `json:"-"`
	notify                  NotifyFunc              `json:"-"`
	accumulatedContent      strings.Builder         `json:"-"`
	config                  *LLMConfig              `json:"-"`
	startTime               time.Time               `json:"-"`
}

// formatMetadata returns the metadata header used by export helpers.
func (s *Session) formatMetadata(exportType ExportType, exportedAt time.Time) string {
	var b strings.Builder
	exported := exportedAt.Format("2006-01-02 15:04:05")
	version := asimiVersion()

	b.WriteString(fmt.Sprintf("**Asimi Version:** %s \n", version))
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

// syncMessages keeps the exported and internal message slices referencing the same data.
func (s *Session) syncMessages() {
	if s.messages == nil && len(s.Messages) > 0 {
		s.messages = s.Messages
	}
	if s.messages == nil {
		s.messages = make([]llms.MessageContent, 0)
	}
	s.Messages = s.messages
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
	} else {
		// Create default config if none provided
		s.config = &LLMConfig{}
	}
	if s.config.MaxTurns <= 0 {
		s.config.MaxTurns = 999
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

	s.messages = append(s.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeSystem,
		Parts: parts,
	})
	s.syncMessages()

	// Build tool schema for the model and execution catalog for the scheduler.
	s.toolDefs, s.toolCatalog = buildLLMTools()
	s.scheduler = NewCoreToolScheduler(s.notify)
	s.ContextFiles = make(map[string]string)
	s.startTime = time.Now()

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
	if len(s.messages) > 0 && s.messages[0].Role == llms.ChatMessageTypeSystem {
		s.messages = s.messages[:1]
	} else {
		s.messages = []llms.MessageContent{}
	}
	s.syncMessages()

	// Reset tool call tracking
	s.lastToolCallKey = ""
	s.toolCallRepetitionCount = 0

	// Reset session start time
	s.startTime = time.Now()

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
	s.messages = append(s.messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart(fullPrompt)},
	})
	s.syncMessages()
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
	resp, err := s.llm.GenerateContent(ctx, s.messages, callOptsWithChoice...)
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
		s.messages = append(s.messages, llms.MessageContent{
			Role:  llms.ChatMessageTypeAI,
			Parts: parts,
		})
		s.syncMessages()
	}
}

// executeToolCall executes a single tool call and returns the response content
func (s *Session) executeToolCall(ctx context.Context, tool lctools.Tool, tc llms.ToolCall, argsJSON string) llms.ToolCallResponse {
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
		s.syncMessages()
	}

	// Reset tool loop detection state when rolling back
	s.lastToolCallKey = ""
	s.toolCallRepetitionCount = 0
}

// processToolCalls handles executing tool calls and building response messages
func (s *Session) processToolCalls(ctx context.Context, toolCalls []llms.ToolCall) ([]llms.MessageContent, bool) {
	toolMessages := make([]llms.MessageContent, 0, len(toolCalls))

	for _, tc := range toolCalls {
		if tc.FunctionCall == nil {
			continue
		}
		name := tc.FunctionCall.Name
		argsJSON := tc.FunctionCall.Arguments

		// Check for tool call loops
		if s.checkToolCallLoop(name, argsJSON) {
			toolMessages = append(toolMessages, llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{llms.ToolCallResponse{
					ToolCallID: tc.ID,
					Name:       name,
					Content:    fmt.Sprintf("error: tool call loop detected after %d attempts", s.toolCallRepetitionCount),
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
		toolMessages = append(toolMessages, llms.MessageContent{
			Role:  llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{response},
		})
	}

	return toolMessages, false // shouldReturn = false
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
		toolMessages, shouldReturn := s.processToolCalls(ctx, choice.ToolCalls)
		if len(toolMessages) > 0 {
			s.messages = append(s.messages, toolMessages...)
			s.syncMessages()
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
			toolMessages, shouldReturn := s.processToolCalls(ctx, choice.ToolCalls)
			if len(toolMessages) > 0 {
				s.messages = append(s.messages, toolMessages...)
				s.syncMessages()
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

// sessBuildEnvBlock constructs a markdown summary of the OS, shell, and key paths.
func sessBuildEnvBlock() string {
	cwd, _ := os.Getwd()
	if cwd == "" {
		cwd = "(unknown)"
	}

	home, _ := os.UserHomeDir()
	if home == "" {
		home = "(unknown)"
	}

	root := findProjectRoot(cwd)
	if root == "" {
		root = "(unknown)"
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}

	return fmt.Sprintf(`- **OS:** %s
- **Shell:** %s
- **Paths:**
  - **cwd:** %s
  - **project root:** %s
  - **home:** %s`,
		runtime.GOOS,
		shell,
		cwd,
		root,
		home)
}

func asimiVersion() string {
	if strings.TrimSpace(version) != "" {
		return strings.TrimSpace(version)
	}

	if v := os.Getenv("ASIMI_VERSION"); v != "" {
		return v
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		if normalized := normalizeBuildVersion(info.Main.Version); normalized != "" {
			return normalized
		}

		var revision string
		var modified bool
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}

		if revision != "" {
			shortRev := revision
			if len(shortRev) > 7 {
				shortRev = shortRev[:7]
			}
			if modified {
				return fmt.Sprintf("dev-%s-dirty", shortRev)
			}
			return fmt.Sprintf("dev-%s", shortRev)
		}
	}

	return "dev"
}

func normalizeBuildVersion(v string) string {
	if v == "" || v == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
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
	for i := range availableTools {
		tool := availableTools[i]
		//nolint:typecheck // Tool interface is correctly defined in tools.go
		execCatalog[tool.Name()] = tool
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

// GetSessionDuration returns the duration since the session started
func (s *Session) GetSessionDuration() time.Duration {
	return time.Since(s.startTime)
}

// GetContextUsagePercent returns the percentage of context used (0-100)
func (s *Session) GetContextUsagePercent() float64 {
	info := s.GetContextInfo()
	if info.TotalTokens <= 0 {
		return 0
	}
	return (float64(info.UsedTokens) / float64(info.TotalTokens)) * 100
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
	closeOnce   sync.Once
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

// SaveSessionSync saves a session synchronously and returns any error
func (store *SessionStore) SaveSessionSync(session *Session) error {
	return store.saveSessionSync(session)
}

func (store *SessionStore) Close() {
	// Use sync.Once to ensure we only close the channel once
	store.closeOnce.Do(func() {
		close(store.stopChan)

		// Wait for worker to finish with timeout
		// The worker will drain the queue when it receives the stop signal
		done := make(chan struct{})
		go func() {
			// Give the worker time to process remaining items
			time.Sleep(100 * time.Millisecond)
			close(done)
		}()

		select {
		case <-done:
			slog.Debug("session store closed gracefully")
		case <-time.After(2 * time.Second):
			slog.Warn("session store close timed out, some saves may be lost")
		}
	})
}

func (store *SessionStore) saveSessionSync(session *Session) error {
	if session == nil {
		return fmt.Errorf("cannot save nil session")
	}

	session.syncMessages()

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

	if session.ProjectSlug == "" {
		session.ProjectSlug = projectSlug(session.WorkingDir)
	}
	if session.ProjectSlug == "" {
		session.ProjectSlug = defaultProjectSlug
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

	sessionDir := filepath.Join(store.storageDir, session.ProjectSlug, "session-"+session.ID)
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

const defaultProjectSlug = "project-unknown"

func projectSlug(workingDir string) string {
	cleaned := filepath.Clean(workingDir)
	if cleaned == "" || cleaned == "." {
		cleaned = workingDir
	}

	base := strings.ToLower(filepath.Base(cleaned))
	if base == "." || base == string(os.PathSeparator) || base == "" {
		base = "project"
	}

	var b strings.Builder
	prevHyphen := false
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	slugBase := strings.Trim(b.String(), "-")
	if slugBase == "" {
		slugBase = "project"
	}

	hash := sha256.Sum256([]byte(cleaned))
	return fmt.Sprintf("%s-%s", slugBase, hex.EncodeToString(hash[:])[:6])
}

type persistedSession struct {
	ID           string            `json:"id"`
	CreatedAt    time.Time         `json:"created_at"`
	LastUpdated  time.Time         `json:"last_updated"`
	FirstPrompt  string            `json:"first_prompt"`
	Provider     string            `json:"provider"`
	Model        string            `json:"model"`
	WorkingDir   string            `json:"working_dir"`
	ProjectSlug  string            `json:"project_slug,omitempty"`
	Messages     []json.RawMessage `json:"messages"`
	ContextFiles map[string]string `json:"context_files"`
}

func (store *SessionStore) LoadSession(id string) (*Session, error) {
	index, err := store.loadIndex()
	if err != nil {
		return nil, err
	}

	var slug string
	var recorded Session
	found := false
	for _, entry := range index.Sessions {
		if entry.ID == id {
			recorded = entry
			slug = entry.ProjectSlug
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("session %s not found", id)
	}

	if slug == "" {
		slug = projectSlug(recorded.WorkingDir)
	}
	if slug == "" {
		slug = defaultProjectSlug
	}

	sessionDir := filepath.Join(store.storageDir, slug, "session-"+id)
	sessionFile := filepath.Join(sessionDir, "session.json")

	data, readErr := os.ReadFile(sessionFile)
	if readErr != nil {
		legacyFile := filepath.Join(store.storageDir, "session-"+id, "session.json")
		var legacyErr error
		data, legacyErr = os.ReadFile(legacyFile)
		if legacyErr != nil {
			return nil, fmt.Errorf("failed to read session file: %w", readErr)
		}
	}

	var persisted persistedSession
	if err := json.Unmarshal(data, &persisted); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	session := &Session{
		ID:           persisted.ID,
		CreatedAt:    persisted.CreatedAt,
		LastUpdated:  persisted.LastUpdated,
		FirstPrompt:  persisted.FirstPrompt,
		Provider:     persisted.Provider,
		Model:        persisted.Model,
		WorkingDir:   persisted.WorkingDir,
		ProjectSlug:  persisted.ProjectSlug,
		ContextFiles: persisted.ContextFiles,
	}

	if session.ProjectSlug == "" {
		session.ProjectSlug = slug
	}

	if session.ContextFiles == nil {
		session.ContextFiles = make(map[string]string)
	}

	for _, rawMsg := range persisted.Messages {
		var restored llms.MessageContent
		if err := json.Unmarshal(rawMsg, &restored); err != nil {
			return nil, fmt.Errorf("restore message: %w", err)
		}
		session.Messages = append(session.Messages, restored)
	}
	session.messages = session.Messages
	session.syncMessages()

	return session, nil
}

func (store *SessionStore) ListSessions(limit int) ([]Session, error) {
	index, err := store.loadIndex()
	if err != nil {
		return nil, err
	}

	currentDir, _ := os.Getwd()
	targetSlug := projectSlug(currentDir)
	if targetSlug == "" {
		targetSlug = defaultProjectSlug
	}

	var filtered []Session
	for _, session := range index.Sessions {
		if session.ProjectSlug == "" {
			session.ProjectSlug = projectSlug(session.WorkingDir)
		}
		if session.ProjectSlug == "" {
			session.ProjectSlug = defaultProjectSlug
		}
		if session.ProjectSlug == targetSlug {
			filtered = append(filtered, session)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].LastUpdated.After(filtered[j].LastUpdated)
	})

	if limit > 0 && len(filtered) > limit {
		return filtered[:limit], nil
	}

	return filtered, nil
}

func (store *SessionStore) CleanupOldSessions() error {
	index, err := store.loadIndex()
	if err != nil {
		return err
	}

	var sessionsToKeep []Session
	cutoffTime := time.Now().AddDate(0, 0, -store.maxAgeDays)
	maxAgeEnabled := store.maxAgeDays > 0

	grouped := make(map[string][]Session)
	for _, session := range index.Sessions {
		if session.ProjectSlug == "" {
			session.ProjectSlug = projectSlug(session.WorkingDir)
		}
		if session.ProjectSlug == "" {
			session.ProjectSlug = defaultProjectSlug
		}
		grouped[session.ProjectSlug] = append(grouped[session.ProjectSlug], session)
	}

	for slug, sessions := range grouped {
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].LastUpdated.After(sessions[j].LastUpdated)
		})

		kept := 0
		for _, session := range sessions {
			if maxAgeEnabled && session.LastUpdated.Before(cutoffTime) {
				store.removeSessionDir(slug, session.ID)
				continue
			}

			if store.maxSessions > 0 && kept >= store.maxSessions {
				store.removeSessionDir(slug, session.ID)
				continue
			}

			session.ProjectSlug = slug
			sessionsToKeep = append(sessionsToKeep, session)
			kept++
		}
	}

	sort.Slice(sessionsToKeep, func(i, j int) bool {
		return sessionsToKeep[i].LastUpdated.After(sessionsToKeep[j].LastUpdated)
	})

	index.Sessions = sessionsToKeep
	return store.saveIndex(index)
}

func (store *SessionStore) removeSessionDir(slug, id string) {
	var paths []string
	if slug != "" {
		paths = append(paths, filepath.Join(store.storageDir, slug, "session-"+id))
	}
	paths = append(paths, filepath.Join(store.storageDir, "session-"+id))

	seen := make(map[string]struct{})
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			fmt.Printf("Warning: failed to remove session %s at %s: %v\n", id, path, err)
		}
	}
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

	for i := range index.Sessions {
		if index.Sessions[i].ProjectSlug == "" {
			index.Sessions[i].ProjectSlug = projectSlug(index.Sessions[i].WorkingDir)
		}
		if index.Sessions[i].ProjectSlug == "" {
			index.Sessions[i].ProjectSlug = defaultProjectSlug
		}
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

	if session.ProjectSlug == "" {
		session.ProjectSlug = projectSlug(session.WorkingDir)
	}
	if session.ProjectSlug == "" {
		session.ProjectSlug = defaultProjectSlug
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
