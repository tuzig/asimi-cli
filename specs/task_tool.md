# Task Tool Specification

## Overview
The `task` tool allows the AI agent to delegate complex subtasks to sub-agents with separate context windows. This enables handling larger projects by breaking them down into manageable pieces while keeping the main context window focused and compact.

## Status
**Planning** - Moving from "To be Planned" to implementation phase

## Motivation
Users working on complex, multi-file projects often hit context window limits. The task tool addresses this by:
- Allowing the agent to spawn focused sub-agents for specific subtasks
- Keeping the main conversation context clean and manageable
- Enabling work on larger codebases that wouldn't fit in a single context
- Providing better separation of concerns between different aspects of a task
- Allowing optional model selection for cost optimization (e.g., using faster/cheaper models for simple tasks)

## Requirements

### Functional Requirements

1. **Task Delegation**
   - Accept a task description as primary input
   - Support optional `model` parameter to specify which LLM model to use for the subtask
   - Create a new sub-agent with its own context window
   - Execute the task independently from the parent context
   - Return results to the main agent

2. **Sub-Agent Configuration**
   - Sub-agents should have access to the same tools as the main agent
   - Sub-agents should inherit relevant context files (optional)
   - Sub-agents should have their own system prompt focused on the delegated task
   - Support model override (e.g., use Claude Haiku for simple tasks, Sonnet for complex ones)

3. **Result Handling**
   - Return a summary of work completed
   - Include key findings, file changes, or errors encountered
   - Provide enough detail for the main agent to continue work
   - Limit result size to avoid overwhelming main context

4. **Error Handling**
   - Handle sub-agent failures gracefully
   - Report errors back to main agent
   - Provide context about what failed and why

### Non-Functional Requirements

1. **Performance**
   - Sub-agents should execute efficiently
   - Avoid unnecessary API calls
   - Timeout protection for runaway sub-agents

2. **Resource Management**
   - Limit maximum depth of sub-agent nesting (prevent infinite recursion)
   - Track token usage for cost awareness
   - Clean up sub-agent resources after completion

3. **Observability**
   - Log sub-agent creation and completion
   - Provide visibility into sub-agent actions (optional verbose mode)
   - Track which tasks were delegated

## Design

### Tool Interface

```go
// TaskToolInput is the input for the TaskTool
type TaskToolInput struct {
    Task  string `json:"task"`  // Required: description of the task to delegate
    Model string `json:"model,omitempty"` // Optional: model to use (e.g., "claude-3-5-haiku-20241022")
}

// TaskTool delegates work to a sub-agent
type TaskTool struct {
    parentSession *Session
    config        *Config
}

func (t TaskTool) Name() string {
    return "task"
}

func (t TaskTool) Description() string {
    return `Delegates a complex subtask to a focused sub-agent with its own context window. 
Use this to break down large tasks, work on specific files without cluttering the main context, 
or use different models for different complexity levels.

Input should be a JSON object with:
- 'task' (required): detailed description of what the sub-agent should do
- 'model' (optional): model name to use (e.g., "claude-3-5-haiku-20241022" for simple tasks)

The sub-agent will have access to all tools and can read/write files, run commands, etc.
Results will be summarized and returned to you.

Example use cases:
- "Refactor the authentication module in auth.go to use the new token system"
- "Analyze all test files and create a summary of test coverage"
- "Fix all lint errors in the frontend/ directory"
`
}

func (t TaskTool) Call(ctx context.Context, input string) (string, error) {
    // Implementation details below
}
```

### Implementation Plan

#### Phase 1: Core Sub-Agent Infrastructure
1. **Create SubAgent Type**
   - Wrap existing Session with task-specific configuration
   - Add parent-child relationship tracking
   - Implement depth limiting (max 2-3 levels)

2. **Task Tool Implementation**
   - Add TaskTool to `tools.go`
   - Implement input parsing and validation
   - Create sub-agent with appropriate context
   - Execute task and collect results

3. **Model Selection**
   - Parse optional `model` parameter
   - Validate model is available for current provider
   - Create LLM client with specified model
   - Fall back to parent model if not specified

#### Phase 2: Context Management
1. **Sub-Agent System Prompt**
   - Create focused system prompt template for sub-agents
   - Include task description prominently
   - Clarify that results should be concise
   - Add instructions about tool usage

2. **Context Inheritance**
   - Determine which context files to pass to sub-agent
   - Implement selective context copying
   - Avoid duplicating large contexts unnecessarily

3. **Result Summarization**
   - Define result format (JSON or structured text)
   - Include: success status, files modified, key findings, errors
   - Limit result length (e.g., max 2000 tokens)
   - Compress or truncate verbose output

#### Phase 3: Execution & Safety
1. **Execution Flow**
   ```go
   func (t TaskTool) Call(ctx context.Context, input string) (string, error) {
       // 1. Parse input
       var params TaskToolInput
       if err := json.Unmarshal([]byte(input), &params); err != nil {
           return "", err
       }
       
       // 2. Check recursion depth
       if t.parentSession.GetDepth() >= maxSubAgentDepth {
           return "", fmt.Errorf("maximum sub-agent depth exceeded")
       }
       
       // 3. Create sub-agent
       subAgent, err := t.createSubAgent(params.Task, params.Model)
       if err != nil {
           return "", err
       }
       
       // 4. Execute task
       result, err := subAgent.Execute(ctx, params.Task)
       if err != nil {
           return formatError(err), nil
       }
       
       // 5. Format and return results
       return formatResults(result), nil
   }
   ```

2. **Safety Mechanisms**
   - Timeout for sub-agent execution (configurable, default 5 minutes)
   - Token limit for sub-agent (configurable)
   - Prevent access to dangerous operations if needed
   - Rate limiting to prevent abuse

3. **Error Handling**
   - Graceful handling of sub-agent failures
   - Clear error messages for debugging
   - Preserve parent session state on sub-agent failure

#### Phase 4: Observability & Testing
1. **Logging**
   - Log sub-agent creation with task description and model
   - Log sub-agent completion with success/failure status
   - Track token usage for cost monitoring
   - Optional verbose mode to see sub-agent actions

2. **UI Integration**
   - Display sub-agent activity in TUI (e.g., "○ Delegating task to sub-agent...")
   - Show completion status
   - Optionally show sub-agent tool calls in collapsed/expandable format

3. **Testing**
   - Unit tests for tool input parsing
   - Unit tests for depth limiting
   - Integration tests with mock LLM
   - End-to-end test with real task delegation

#### Phase 5: Documentation & Polish
1. Update system prompt to mention task tool
2. Add examples to help documentation
3. Add configuration options to config file:
   ```toml
   [task_tool]
   max_depth = 2
   default_timeout_seconds = 300
   max_tokens = 50000
   enable_verbose_logging = false
   ```
4. Add usage examples to README

### Example Usage

**User prompt:**
```
Please refactor all the tool implementations to follow a consistent pattern
```

**Agent response:**
```
I'll delegate this to a sub-agent to keep our context clean.

[Calls task tool with:]
{
  "task": "Review all tool implementations in tools.go and refactor them to follow a consistent pattern. Ensure all Format() methods have the same structure, error handling is consistent, and documentation is complete.",
  "model": "claude-3-5-sonnet-20241022"
}

[Sub-agent executes, returns:]
{
  "success": true,
  "files_modified": ["tools.go"],
  "summary": "Refactored 6 tool implementations. Standardized Format() methods to use consistent two-line output format. Added missing error handling to ReadManyFilesTool. Updated all tool descriptions to follow same documentation style.",
  "tokens_used": 12453
}
```

## Configuration

Add to `config.go`:
```go
type TaskToolConfig struct {
    Enabled              bool   `koanf:"enabled"`
    MaxDepth             int    `koanf:"max_depth"`
    DefaultTimeoutSec    int    `koanf:"default_timeout_seconds"`
    MaxTokens            int    `koanf:"max_tokens"`
    EnableVerboseLogging bool   `koanf:"enable_verbose_logging"`
    AllowModelOverride   bool   `koanf:"allow_model_override"`
}
```

Add to default config:
```toml
[task_tool]
enabled = true
max_depth = 2
default_timeout_seconds = 300
max_tokens = 50000
enable_verbose_logging = false
allow_model_override = true
```

## Testing Strategy

### Unit Tests
1. Input parsing and validation
2. Depth limiting logic
3. Model selection fallback
4. Result formatting

### Integration Tests
1. Sub-agent creation and execution
2. Context passing
3. Error propagation
4. Timeout handling

### Manual Tests
1. Simple task delegation (e.g., "read and summarize config.go")
2. Complex task with multiple file operations
3. Nested task delegation (sub-agent delegates to another sub-agent)
4. Task delegation with model override
5. Error scenarios (invalid model, timeout, etc.)

## Dependencies
- Existing Session infrastructure
- Tool execution framework
- LLM client creation (getLLMClient)
- Token counting infrastructure
- Configuration system

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Runaway costs from nested sub-agents | High | Strict depth limits, timeouts, token limits |
| Context confusion between agents | Medium | Clear result formatting, limited context sharing |
| Sub-agent errors breaking parent | Medium | Robust error handling, state isolation |
| Poor results from wrong model choice | Low | Good default, allow override, document model selection |
| Performance issues | Medium | Timeouts, async execution where possible |

## Success Criteria
- [ ] Tool successfully executes simple tasks
- [ ] Depth limiting prevents infinite recursion
- [ ] Model override works correctly
- [ ] Results are concise and useful
- [ ] Error handling is robust
- [ ] Token usage is tracked
- [ ] Documentation is complete
- [ ] Unit test coverage >80%
- [ ] Manual testing shows practical value

## Future Enhancements
1. **Parallel Task Execution**: Allow multiple sub-agents to run concurrently
2. **Task Templates**: Pre-defined task patterns (e.g., "test file", "refactor module")
3. **Result Caching**: Cache results of identical tasks
4. **Inter-Agent Communication**: Allow sub-agents to share findings
5. **Progress Reporting**: Stream sub-agent progress back to parent
6. **Cost Optimization**: Automatic model selection based on task complexity

## References
- Existing tool implementations in `tools.go`
- Session management in `session.go`
- Context command spec: `specs/context_command.md`
- LangChain multi-agent patterns
- Anthropic documentation on tool use
