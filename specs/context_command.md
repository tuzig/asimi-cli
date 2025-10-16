# Context Command Specification

## Overview
The `/context` command provides detailed information about the current context usage, including token consumption breakdown and visual representation of context allocation.

## Status
**Planning** - Moving from "To be Planned" to implementation phase

## Motivation
Users need visibility into how their context window is being utilized to:
- Understand when they're approaching context limits
- Identify what's consuming the most tokens
- Make informed decisions about when to compact or start a new session
- Debug context-related issues

## Requirements

### Functional Requirements

1. **Display Context Breakdown**
   - Show current model and total context window size
   - Display token usage by category:
     - System prompt tokens
     - System tools tokens
     - Memory files tokens
     - Messages tokens
     - Free space
     - Autocompact buffer
   - Show both absolute token counts and percentages

2. **Visual Representation**
   - ASCII-based bar chart showing proportional usage
   - Different symbols for different states:
     - `⛁` - Used space
     - `⛀` - Partially used
     - `⛶` - Free space
     - `⛝` - Autocompact buffer
   - 10-column width for easy reading

3. **Command Interface**
   - Command: `/context`
   - No arguments required
   - Output displayed in the chat interface

### Non-Functional Requirements

1. **Performance**
   - Command should execute quickly (<100ms)
   - Token counting should be efficient
   - No API calls required

2. **Accuracy**
   - Token counts should match actual API usage
   - Percentages should be accurate to 1 decimal place

3. **Usability**
   - Output should be clear and easy to read
   - Visual representation should be intuitive
   - Should work with all supported models

## Design

### Data Structure

```go
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
```

### Implementation Plan

#### Phase 1: Token Counting Infrastructure
1. Add token counting methods to session:
   - `CountSystemPromptTokens()` - Count tokens in system prompt
   - `CountSystemToolsTokens()` - Count tokens in tool definitions
   - `CountMemoryFilesTokens()` - Count tokens in memory files
   - `CountMessagesTokens()` - Count tokens in conversation history
   - `GetContextInfo()` - Aggregate all counts

2. Integrate with existing token estimation:
   - Use the same tokenization method as API calls
   - Ensure consistency with actual API token usage
   - Cache counts where appropriate for performance

#### Phase 2: Visual Rendering
1. Create rendering functions:
   - `renderContextBar(info ContextInfo)` - Generate ASCII bar chart
   - `formatContextLine(label, tokens, total, symbol)` - Format individual lines
   - `calculateBarSegments(percentage)` - Convert percentage to bar segments

2. Symbol mapping:
   - 0-70% used: `⛁` (filled)
   - 70-80% used: `⛀` (warning)
   - Free space: `⛶` (empty)
   - Autocompact buffer: `⛝` (reserved)

#### Phase 3: Command Integration
1. Add `/context` to command parser in `commands.go`
2. Implement command handler:
   ```go
   func (s *Session) handleContextCommand() error {
       info := s.GetContextInfo()
       output := renderContextInfo(info)
       s.displayMessage(output)
       return nil
   }
   ```

3. Add tests:
   - Unit tests for token counting
   - Unit tests for rendering
   - Integration test for full command

#### Phase 4: Polish & Documentation
1. Add help text for `/context` command
2. Update user documentation
3. Add examples to README
4. Consider adding to welcome message or help output

### Example Output

```
  ⎿  Context Usage
     ⛁ ⛁ ⛁ ⛁ ⛁ ⛁ ⛁ ⛀ ⛀ ⛶   claude-sonnet-4-5-20250929 · 60k/200k tokens (30%)
     ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ 
     ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶   ⛁ System prompt: 2.3k tokens (1.1%)
     ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶   ⛁ System tools: 11.9k tokens (5.9%)
     ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶   ⛁ Memory files: 208 tokens (0.1%)
     ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶   ⛁ Messages: 259 tokens (0.1%)
     ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶   ⛶ Free space: 140k (70.2%)
     ⛶ ⛶ ⛶ ⛶ ⛶ ⛝ ⛝ ⛝ ⛝ ⛝   ⛝ Autocompact buffer: 45.0k tokens (22.5%)
     ⛝ ⛝ ⛝ ⛝ ⛝ ⛝ ⛝ ⛝ ⛝ ⛝ 
     ⛝ ⛝ ⛝ ⛝ ⛝ ⛝ ⛝ ⛝ ⛝ ⛝
```

## Testing Strategy

### Unit Tests
1. Token counting accuracy
2. Bar rendering with various percentages
3. Edge cases (0%, 100%, >100%)
4. Different model context sizes

### Integration Tests
1. Full command execution
2. Output formatting
3. Multiple models

### Manual Testing
1. Test with real sessions at various context levels
2. Verify visual representation is clear
3. Test with different terminal widths

## Dependencies
- Existing token counting infrastructure
- Session state management
- Command parser
- TUI rendering system

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Token count inaccuracy | High | Use same tokenizer as API, add validation tests |
| Performance issues | Medium | Cache counts, optimize calculations |
| Visual rendering issues | Low | Test with various terminal emulators |
| Model compatibility | Medium | Test with all supported models |


## Success Criteria
- [ ] Command executes in <100ms
- [ ] Token counts accurate within 5%
- [ ] Visual representation renders correctly in all supported terminals
- [ ] Works with all supported models
- [ ] Unit test coverage >80%
- [ ] Documentation complete
- [ ] User feedback positive

## References
- [Keep a Changelog](https://keepachangelog.com/)
- Existing command implementations in `commands.go`
- Token counting in `session.go`
- TUI rendering in `tui.go`
