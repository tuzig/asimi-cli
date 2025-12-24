# TUI Architecture Documentation

This document describes the architecture of the Terminal User Interface (TUI) for Claude Code, focusing on three major refactorings that established clean component boundaries and message-passing patterns.

## Table of Contents

1. [Overview](#overview)
2. [Component Architecture](#component-architecture)
3. [Mode Management System](#mode-management-system)
   - [SELECT Mode Unification](#select-mode-unification)
4. [Command Line Component](#command-line-component)
5. [Mouse Event Handling](#mouse-event-handling)
6. [Dynamic Prompt Height](#dynamic-prompt-height)
7. [Message Flow Patterns](#message-flow-patterns)
8. [Future Improvements](#future-improvements)

---

## Overview

### Screen Layout

```
┌─────────────────────────────────────────┐
│                                         │
│         CONTENT AREA                    │  ← Mouse wheel works here
│         (Chat/Help/Models/Resume)       │
│                                         │
│         Height = screen - prompt - 4    │  ← Dynamic based on prompt height
│                                         │
├─────────────────────────────────────────┤
│         EMPTY LINE                      │
├─────────────────────────────────────────┤
│  ┌───────────────────────────────────┐  │
│  │    PROMPT AREA                    │  │  ← Mouse wheel ignored here
│  │    (2-N lines, configurable)      │  │  ← Grows when multiline
│  └───────────────────────────────────┘  │
├─────────────────────────────────────────┤
│  STATUS LINE                            │
├─────────────────────────────────────────┤
│  COMMAND LINE                           │
└─────────────────────────────────────────┘
```

**Dynamic Content Height Calculation:**
```go
commandLineHeight := 1
statusHeight := 1
promptWithBorder := m.prompt.Height + 2
contentHeight := m.height - commandLineHeight - statusHeight - promptWithBorder + 1 - modalHeight
```

The TUI follows the [Bubbletea](https://github.com/charmbracelet/bubbletea) architecture pattern:
- **Model**: `TUIModel` holds application state
- **Update**: Message handlers update state and return commands
- **View**: Renders the current state (no logic, just rendering)

### Core Principles

1. **Single Source of Truth**: State lives in one place
2. **Message Passing**: Components communicate via messages, not direct calls
3. **Separation of Concerns**: Components handle their own input and state
4. **No Polling**: View() renders state without checking component internals
5. **Centralized Coordination**: TUI coordinates, components execute

---

## Component Architecture

### TUIModel (tui.go)

The main model that coordinates all components:

```go
type TUIModel struct {
    Mode           string              // Current UI mode (single source of truth)
    prompt         *PromptComponent    // User input
    content        *ContentComponent   // Chat/Help/Models/Resume views
    commandLine    *CommandLineComponent // Command line input
    status         *StatusComponent    // Status bar
    completion     *CompletionComponent // Completion dialog
    // ... other fields
}
```

**Responsibilities:**
- Coordinate component interactions
- Handle high-level key bindings
- Route messages to appropriate handlers
- Maintain global state (mode, focus, etc.)

### Components

Each component is self-contained and follows these patterns:

1. **Handles its own input** via `HandleKey()` or `Update()`
2. **Returns messages** to communicate state changes
3. **Exposes minimal API** for coordination
4. **Doesn't know about other components**

---

## Mode Management System

### Problem

Previously, `TUIModel.View()` had to poll multiple components to determine what mode to display:

```go
// OLD - Scattered, coupled logic
if m.commandLine.IsInCommandMode() {
    m.status.SetViMode(viEnabled, "COMMAND", "")
} else if activeView != ViewChat {
    m.status.SetViMode(viEnabled, viewName, "")
} else {
    m.status.SetViMode(viEnabled, viMode, viPending)
}
```

This violated separation of concerns and created tight coupling.

### Solution: Centralized Mode Management

**Core Concept:**
- **Single Source of Truth**: `TUIModel.Mode string` field
- **Single Message**: `ChangeModeMsg{NewMode: "insert"}` for ALL mode changes
- **Centralized Handler**: Updates `m.Mode` and status component in one place
- **Components Send Messages**: When they change state
- **View() Just Renders**: No polling, no logic

### The Message

```go
type ChangeModeMsg struct {
    NewMode string // "insert", "normal", "visual", "command", "help", "select", "scroll"
}
```

### Centralized Handler

```go
case ChangeModeMsg:
    wasScroll := m.Mode == "scroll"
    m.Mode = msg.NewMode
    isScroll := m.Mode == "scroll"
    if wasScroll && !isScroll {
        m.content.Chat.SetScrollLock(false)
    } else if !wasScroll && isScroll {
        m.content.Chat.SetScrollLock(true)
    }
    m.status.SetMode(m.Mode)
    if m.Mode == "select" {
        m.commandLine.AddToast(" :quit to close | j/k to navigate | Enter to select ", "success", 3000)
        m.prompt.TextArea.Placeholder = "j/k to navigate | Enter to select | :quit to close"
    } else if m.Mode == "insert" {
        m.prompt.TextArea.Placeholder = PlaceholderDefault
    }
    return m, nil
```

**Special Modes:**

- `scroll` - Dedicated chat-navigation mode entered with `Ctrl-B`. It locks the viewport in place (no auto-scroll), minimizes the prompt to 2 lines to maximize content visibility, and provides vi-style paging:
  - `Ctrl-F` / `Ctrl-B` - Page down/up
  - `Ctrl-D` / `Ctrl-U` - Half page down/up
  - `j` / `k` / `↓` / `↑` - Scroll one line down/up
  - `G` - Jump to bottom
  - `:` - Enter command mode without snapping back
  - `Esc` / `i` - Return to insert mode
  
  The placeholder text in scroll mode shows: "j/k to scroll | CTRL-f/b & d/u as in vi | i/:/ESC to exit"

- `select` - Unified list selection mode used for both models and resume views. Provides consistent navigation:
  - `j` / `k` / `↓` / `↑` - Navigate up/down
  - `Ctrl-D` / `Ctrl-U` - Half page down/up
  - `g` / `G` - Jump to top/bottom
  - `Enter` - Select item
  - `:quit` / `Esc` - Return to chat

### Component Integration

Components return `ChangeModeMsg` when their state changes:

```go
// CommandLineComponent
func (cl *CommandLineComponent) EnterCommandMode(initialText string) tea.Cmd {
    cl.mode = CommandLineCommand
    // ... setup ...
    return func() tea.Msg {
        return ChangeModeMsg{NewMode: "command"}
    }
}

// ContentComponent
func (c *ContentComponent) ShowHelp(topic string) tea.Cmd {
    c.activeView = ViewHelp
    // ... setup ...
    return func() tea.Msg {
        return ChangeModeMsg{NewMode: "help"}
    }
}

func (c *ContentComponent) ShowModels(models []AnthropicModel, currentModel string) tea.Cmd {
    c.activeView = ViewModels
    // ... setup ...
    return func() tea.Msg {
        return ChangeModeMsg{NewMode: "select"}
    }
}

func (c *ContentComponent) ShowResume(sessions []Session) tea.Cmd {
    c.activeView = ViewResume
    // ... setup ...
    return func() tea.Msg {
        return ChangeModeMsg{NewMode: "select"}
    }
}

// TUI vi mode changes
case "i":
    m.prompt.EnterViInsertMode()
    return m, func() tea.Msg { return ChangeModeMsg{NewMode: "insert"} }
```

### Message Flow Example

```
User presses ':'
  ↓
TUI calls m.commandLine.EnterCommandMode("")
  ↓
Returns ChangeModeMsg{NewMode: "command"}
  ↓
TUI receives message in handleCustomMessages
  ↓
Sets m.Mode = "command"
  ↓
Maps to displayMode = "COMMAND"
  ↓
Calls m.status.SetViMode(viEnabled, "COMMAND", viPending)
  ↓
View() renders with updated status
```

### Supported Modes

- `"insert"` → `<INSERT>`
- `"normal"` → `<NORMAL>`
- `"visual"` → `<VISUAL>`
- `"command"` → `<COMMAND>`
- `"help"` → `<HELP>`
- `"select"` → `<SELECT>` (used for both models and resume views)
- `"scroll"` → `<SCROLL>`
- `"learning"` → (maps to itself)

### Benefits

✅ **Single Source of Truth** - `m.Mode` is the only place tracking mode  
✅ **Single Message** - `ChangeModeMsg` for ALL mode changes  
✅ **Centralized Logic** - One handler updates everything  
✅ **No Polling** - View() doesn't check component state  
✅ **Flexible** - Easy to add modes (just strings)  
✅ **Testable** - Verify mode changes via messages  
✅ **Decoupled** - Components don't know about status  
✅ **Consistent** - Same pattern everywhere  

### SELECT Mode Unification

#### Problem

Previously, the TUI had separate `MODELS` and `RESUME` modes for list selection interfaces. Both modes were essentially identical:
- Same navigation patterns (j/k, Ctrl-D/U, g/G)
- Same selection behavior (Enter to select)
- Same exit behavior (Esc/:quit to return to chat)
- Same visual presentation (title bar + scrollable list)

Having two separate modes added unnecessary complexity to mode management logic, status bar display, and documentation.

#### Solution: Unified SELECT Mode

Both modes were merged into a single `SELECT` mode that handles all list selection interfaces.

**Implementation Changes:**

```go
// content.go - Both methods now return the same mode
func (c *ContentComponent) ShowModels(...) tea.Cmd {
    c.activeView = ViewModels
    // ... setup ...
    return func() tea.Msg {
        return ChangeModeMsg{NewMode: "select"}
    }
}

func (c *ContentComponent) ShowResume(...) tea.Cmd {
    c.activeView = ViewResume
    // ... setup ...
    return func() tea.Msg {
        return ChangeModeMsg{NewMode: "select"}
    }
}
```

**Mode Handler:**

```go
case ChangeModeMsg:
    // ...
    if m.Mode == "select" {
        m.commandLine.AddToast(" :quit to close | j/k to navigate | Enter to select ", "success", 3000)
        m.prompt.TextArea.Placeholder = "j/k to navigate | Enter to select | :quit to close"
    }
    // ...
```

**View Differentiation:**

The content component uses `c.activeView` (ViewModels/ViewResume) to distinguish between different selection views when needed. The title bar still shows context-specific information:
- "Select Model" for model selection
- "Choose a session to resume [1/10]:" for session selection

#### Benefits

✅ **Simpler Mode Management** - One mode instead of two  
✅ **Consistent User Experience** - Same behavior for all list selections  
✅ **Less Code** - Fewer conditional branches  
✅ **Easier to Extend** - Adding new selection views (e.g., "select provider", "select workspace") is trivial  
✅ **Better Documentation** - Clearer mental model for users  
✅ **Maintainability** - Less code to maintain and test  

#### Future Selection Interfaces

This pattern makes it easy to add new selection interfaces:

1. **Provider Selection** - `:provider` command to switch between Anthropic/OpenAI/etc.
2. **Workspace Selection** - `:workspace` command to switch between different project contexts
3. **Theme Selection** - `:theme` command to choose color schemes
4. **History Search** - `:search` command to search through conversation history

All would use the same `SELECT` mode with consistent navigation.

---

## Command Line Component

### Problem

Previously, `TUIModel.handleCommandLineInput()` handled all keyboard input for the command line (266 lines), which violated separation of concerns.

### Solution: Component Self-Management

The `CommandLineComponent` now handles its own input and communicates via messages.

### Component Messages

```go
type (
    commandReadyMsg       struct{ command string }  // Command ready to execute
    commandCancelledMsg   struct{}                   // User pressed ESC
    commandTextChangedMsg struct{}                   // Text changed, update completions
    navigateCompletionMsg struct{ direction int }    // Navigate completion list
    navigateHistoryMsg    struct{ direction int }    // Navigate history
    acceptCompletionMsg   struct{}                   // Accept selected completion
)
```

### HandleKey Method

The component handles its own keyboard input:

```go
func (cl *CommandLineComponent) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool)
```

**Handles:**
- Basic editing (backspace, delete, cursor movement, space, typing)
- Enter (returns `commandReadyMsg`)
- ESC (returns `commandCancelledMsg` + `ChangeModeMsg`)
- Up/Down/Tab (returns navigation messages)

**Returns:**
- `tea.Cmd` - Command to execute (message for TUI)
- `bool` - Whether the key was handled

### TUI Integration

```go
if m.commandLine.IsInCommandMode() {
    cmd, handled := m.commandLine.HandleKey(msg)
    if handled {
        return m, cmd  // Process returned message
    }
    // Fallback (shouldn't happen)
}
```

### Message Handlers

**`commandReadyMsg`**:
- Saves to persistent history
- Parses and executes command using `FindCommand()` (vim-style partial matching)
- Hides completions
- Restores focus

**`commandCancelledMsg`**:
- Hides completions
- Restores focus to prompt

**`commandTextChangedMsg`**:
- Updates command line completions via `updateCommandLineCompletions()`

**`navigateCompletionMsg`**:
- Navigates completion dialog up/down

**`acceptCompletionMsg`**:
- Accepts selected completion

### Architecture Evolution

#### Before (Monolithic)
```
TUIModel.handleKeyMsg()
    └── TUIModel.handleCommandLineInput()  [handles everything - 266 lines]
            ├── Editing (backspace, delete, cursor)
            ├── History navigation
            ├── Completion navigation
            └── Command execution
```

#### After (Component-Based)
```
TUIModel.handleKeyMsg()
    └── CommandLineComponent.HandleKey()  [handles ALL keys]
            ├── Returns commandReadyMsg → TUIModel.handleCustomMessages()
            ├── Returns commandCancelledMsg → TUIModel.handleCustomMessages()
            ├── Returns commandTextChangedMsg → TUIModel.handleCustomMessages()
            ├── Returns navigateCompletionMsg → TUIModel.handleCustomMessages()
            ├── Returns navigateHistoryMsg → TUIModel.handleCustomMessages()
            └── Returns acceptCompletionMsg → TUIModel.handleCustomMessages()
```

### Benefits

✅ **Complete Separation** - Component handles ALL its own input  
✅ **Clean Message Passing** - Uses bubbletea pattern throughout  
✅ **Reduced Code** - Removed 266 lines from TUI  
✅ **Better Encapsulation** - All command line logic in one place  
✅ **Easier Testing** - Component can be tested independently  
✅ **Maintainability** - Clear responsibilities  

---

## Mouse Event Handling

### Problem

Mouse wheel events were being processed twice, causing duplicate scrolling and potentially affecting areas outside the content window:

1. **Duplicate Processing**: Events were handled by both `content.Update(msg)` and `handleMouseMsg()`
2. **No Position Checking**: Events were processed regardless of mouse cursor position
3. **Unintended Side Effects**: Mouse wheel could affect prompt history navigation

### Solution: Position-Aware Single Processing

The mouse event handling now follows these principles:

1. **Position Checking**: Only process events within the content area
2. **Single Handler**: Events are processed exactly once
3. **Component Delegation**: Content component handles its own scrolling

### Implementation

**Position Checking** (`tui.go`):
```go
case tea.MouseMsg:
    // Only handle mouse events if they're within the content area
    // Content area is from top of screen to just above the prompt
    contentHeight := m.height - 6 // Subtract prompt, status, command line, etc.
    if msg.Y < contentHeight {
        // Mouse is in content area - let content component handle it
        var contentCmd tea.Cmd
        m.content, contentCmd = m.content.Update(msg)
        return m, contentCmd
    }
    // Mouse is outside content area - ignore it
    return m, nil
```

**Content Height Calculation**:
```go
contentHeight := m.height - 6

// Breakdown:
// - Command line: 1 line
// - Status line: 1 line
// - Prompt (with borders): ~2-3 lines
// - Empty line: 1 line
// = 6 lines reserved for UI chrome
```

### Event Flow

```
Mouse Wheel Event (Y position)
       ↓
   TUI Update()
       ↓
   Check: msg.Y < contentHeight?
       ↓
       ├─→ YES (in content area)
       │        ↓
       │   content.Update(msg)
       │        ↓
       │   Chat.Update(msg)
       │        ↓
       │   Viewport.ScrollUp/Down()  ← Single scroll ✓
       │
       └─→ NO (outside content area)
                ↓
           Ignore event  ← No scrolling ✓
```

### Component Responsibilities

**TUIModel** (`tui.go`):
- Checks mouse event position
- Routes events to content component only if in content area
- Ignores events outside content area

**ContentComponent** (`content.go`):
- Receives mouse events from TUI
- Delegates to active view (chat, help, models, resume)
- Handles view-specific mouse behavior

**ChatComponent** (`chat.go`):
- Handles mouse wheel scrolling
- Updates viewport position
- Tracks user scrolling vs auto-scrolling
- Supports touch gestures (drag scrolling)

**PromptComponent** (`prompt.go`):
- Does NOT handle mouse events
- Only responds to keyboard input
- History navigation via up/down arrow keys

### Supported Mouse Events

**In Content Area**:
- `MouseWheelUp` - Scroll content up
- `MouseWheelDown` - Scroll content down
- `MouseLeft` + `MouseActionPress` - Start touch drag
- `MouseMotion` - Touch drag scrolling
- `MouseLeft` + `MouseActionRelease` - End touch drag

**Outside Content Area**:
- All mouse events are ignored

### Benefits

✅ **No Duplicate Scrolling** - Each mouse event processed exactly once  
✅ **Position Awareness** - Events only affect area under cursor  
✅ **Clean Separation** - Each component handles its own input  
✅ **Predictable Behavior** - Users get expected scrolling  
✅ **No Side Effects** - Prompt history navigation unaffected  
✅ **Touch Support** - Drag scrolling works in content area  

### Testing Scenarios

**Scenario 1: Scroll in Chat Area**
```
User: Scrolls mouse wheel over chat messages
Expected: Chat scrolls smoothly
Result: ✓ Works correctly
```

**Scenario 2: Scroll over Prompt**
```
User: Scrolls mouse wheel over prompt input
Expected: Nothing happens
Result: ✓ Works correctly (event ignored)
```

**Scenario 3: Keyboard History Navigation**
```
User: Presses up/down arrows in prompt
Expected: Navigate through prompt history
Result: ✓ Works correctly (unaffected by mouse fix)
```

**Scenario 4: Touch Drag Scrolling**
```
User: Drags finger/mouse in chat area
Expected: Chat scrolls based on drag distance
Result: ✓ Works correctly (handled by chat component)
```

---

## Dynamic Prompt Height

### Problem

The prompt input area had a fixed height, which wasted vertical space for single-line inputs and didn't provide enough room for multiline inputs. Users composing longer messages or code snippets couldn't see their full input without scrolling within the prompt.

### Solution: Dynamic Height Based on Content

The prompt now dynamically adjusts its height based on content:

- **Minimum (2 lines)**: Empty prompt or single-line content
- **Expanded (PromptExpandedHeight, default 10)**: When content spans multiple lines (including wrapped text)
- **Maximum (50% of screen)**: Hard cap to ensure content area remains usable

### Height Calculation Logic

```go
func (p *PromptComponent) CalculateDesiredHeight() int {
    value := p.TextArea.Value()

    // Return to minimum height when:
    // 1. Prompt is cleared (empty)
    // 2. In scroll mode (maximize content visibility)
    if value == "" || p.ViCurrentMode == ViModeScroll {
        return 2
    }

    // Calculate visual lines (accounting for word wrap)
    textWidth := p.Width - 4 // Account for borders and cursor
    visualLines := 0
    for _, line := range strings.Split(value, "\n") {
        if len(line) == 0 {
            visualLines++
        } else {
            visualLines += (len(line) + textWidth - 1) / textWidth
        }
    }

    // Single visual line: minimum height
    if visualLines <= 1 {
        return 2
    }

    // Multiline: expand to configured height (capped at MaxHeight)
    expandedHeight := p.ExpandedHeight
    if p.MaxHeight > 0 && expandedHeight > p.MaxHeight {
        return p.MaxHeight
    }
    return expandedHeight
}
```

### Configuration

The expanded height can be customized via configuration:

```go
type UIConfig struct {
    MarkdownEnabled      bool          `koanf:"markdown_enabled"`
    CtrlCDebounceTime    time.Duration `koanf:"ctrl_c_debounce_time"`
    CtrlCWindowTime      time.Duration `koanf:"ctrl_c_window_time"`
    PromptExpandedHeight int           `koanf:"prompt_expanded_height"` // Default: 10
}
```

**Usage in config file:**
```yaml
ui:
  prompt_expanded_height: 15  # Grow to 15 lines instead of default 10
```

### Integration with View()

The `View()` method recalculates prompt height before each render:

```go
func (m TUIModel) View() string {
    // Update prompt dimensions based on content
    m.prompt.SetScreenHeight(m.height)
    promptHeight := m.prompt.CalculateDesiredHeight()
    m.prompt.SetHeight(promptHeight)

    // Recalculate content height based on new prompt height
    commandLineHeight := 1
    statusHeight := 1
    promptWithBorder := promptHeight + 2
    contentHeight := m.height - commandLineHeight - statusHeight - promptWithBorder + 1
    m.content.SetSize(m.width-2, contentHeight)
    
    // ... rest of rendering
}
```

### Scroll Mode Optimization

When entering scroll mode (`Ctrl-B`), the prompt automatically shrinks to 2 lines to maximize the content viewing area. This is particularly useful when reviewing long chat histories:

```go
if p.ViCurrentMode == ViModeScroll {
    return 2  // Minimize prompt in scroll mode
}
```

### Height Transitions

| Content State | Visual Lines | Resulting Height |
|--------------|--------------|------------------|
| Empty | 0 | 2 lines |
| Single line "hello" | 1 | 2 lines |
| "line1\nline2" | 2 | PromptExpandedHeight |
| Long wrapped text | 2+ | PromptExpandedHeight |
| Any content in scroll mode | N/A | 2 lines |
| Multiline (small screen) | 2+ | MaxHeight (50% of screen) |

### Benefits

✅ **Space Efficiency** - Minimal height for simple inputs  
✅ **Multiline Editing** - Comfortable editing for longer inputs  
✅ **Word Wrap Awareness** - Accounts for wrapped lines, not just explicit newlines  
✅ **Scroll Mode Integration** - Maximizes content area when browsing  
✅ **Configurable** - Users can customize expanded height  
✅ **Screen-Aware** - Respects maximum height constraints  
✅ **Dynamic Content Area** - Content area automatically adjusts  

### Testing

```go
func TestPromptHeightGrowsTo10LinesForMultilineInput(t *testing.T) {
    prompt := NewPromptComponent(80, 5)
    prompt.SetScreenHeight(40)

    tests := []struct {
        name           string
        value          string
        mode           string
        expectedHeight int
    }{
        {"empty prompt returns 2 lines", "", ViModeInsert, 2},
        {"single line returns 2 lines", "Hello", ViModeInsert, 2},
        {"two lines grows to 10", "Line 1\nLine 2", ViModeInsert, 10},
        {"scroll mode returns 2 lines", "Line 1\nLine 2", ViModeScroll, 2},
        {"wrapped text grows to 10", "Very long line...", ViModeInsert, 10},
    }
    // ...
}
```

---

## Message Flow Patterns

### Pattern 1: Component State Change

```
User action
  ↓
Component method called
  ↓
Component updates internal state
  ↓
Returns ChangeModeMsg (or other message)
  ↓
TUI receives in handleCustomMessages
  ↓
TUI updates global state
  ↓
View() renders
```

### Pattern 2: Component Input Handling

```
User presses key
  ↓
TUI.handleKeyMsg() checks active component
  ↓
Calls Component.HandleKey(msg)
  ↓
Component processes key
  ↓
Returns (tea.Cmd, bool)
  ↓
TUI executes command (if any)
  ↓
Message flows back to handleCustomMessages
```

### Pattern 3: Batched Messages

Components can return multiple messages at once:

```go
case "esc":
    exitCmd := cl.ExitCommandMode()  // ChangeModeMsg{NewMode: "insert"}
    return tea.Batch(
        exitCmd,
        func() tea.Msg { return commandCancelledMsg{} },
    ), true
```

### Pattern 4: Command Chaining

Methods return commands that can be chained:

```go
// Before
m.content.ShowHelp(msg.topic)
return m, nil

// After
return m, m.content.ShowHelp(msg.topic)  // Returns ChangeModeMsg
```

---

### Pattern 5: Streaming AI Responses

The TUI handles streaming responses from the AI through specialized message types. This includes both regular content and "extended thinking" (reasoning) content.

**Message Types:**

- `streamChunkMsg` - Regular AI response content chunks
- `streamReasoningChunkMsg` - Thinking/reasoning content (from models with extended thinking)
- `streamCompleteMsg` - Marks the end of a streaming response

**Message Flow:**

```
AI starts response
       ↓
   streamReasoningChunkMsg (if thinking enabled)
       ↓
   <thinking> tags wrap reasoning content
       ↓
   streamChunkMsg (regular response content)
       ↓
   "Asimi: " prefix added to message
       ↓
   streamCompleteMsg
       ↓
   FinalizeLastAIMessage() - adds SUCCESS/FAILURE prefix
```

**Optimizations:**

1. **Empty Reasoning Skip**: Empty reasoning chunks (whitespace only) are skipped to avoid unnecessary message creation and rendering.

2. **Empty Content Skip**: When rendering messages with `<thinking>` tags, if both the thinking content and regular content are empty, the message is skipped entirely.

**Rendering:**

The `ChatComponent.UpdateContent()` method handles rendering of thinking blocks:

- Content wrapped in `<thinking>...</thinking>` tags is extracted and rendered with a special "thinking" style
- Regular content following thinking tags is rendered normally with markdown
- Messages with the `Asimi:` prefix receive appropriate styling (success/failure/normal)

---

## Future Improvements

### Mode Management
- Add mode transition validation (e.g., can't go from help → command directly)
- Track mode history for undo/debugging
- Add mode change hooks for logging/analytics
- Make mode changes async if needed
- Add mode-specific state (e.g., help topic, selected model)

### Command Line Component
- Extract completion handling to a separate component
- Make history navigation a reusable component
- Add more unit tests for `CommandLineComponent.HandleKey()`

### Prompt Component
- Animate height transitions for smoother UX
- Add user preference for "always expanded" mode
- Consider different expanded heights for different content types

### General Architecture
- Consider extracting more components (e.g., StatusComponent could handle its own updates)
- Add component lifecycle hooks (Init, Cleanup)
- Implement component-level error handling
- Add telemetry/metrics for component interactions

---

## Files Reference

### Core Files
- `tui.go` - Main TUI model and coordination logic
- `commandline.go` - Command line component
- `content.go` - Content view component (chat/help/models/resume)
- `prompt.go` - User input prompt component (with dynamic height)
- `status.go` - Status bar component
- `config.go` - Configuration including `UIConfig.PromptExpandedHeight`

### Key Changes Made

**prompt.go**:
- Added `ExpandedHeight` field (configurable, default 10)
- Added `SetExpandedHeight()` method
- Updated `CalculateDesiredHeight()` for dynamic sizing:
  - Returns 2 for empty/single-line/scroll-mode
  - Returns `ExpandedHeight` for multiline (capped at `MaxHeight`)
  - Accounts for word-wrapped lines

**config.go**:
- Added `PromptExpandedHeight int` to `UIConfig`
- Default value: 10 (configurable via `PromptExpandedHeight`)

**tui.go**:
- Added `Mode` field to `TUIModel`
- Added centralized `ChangeModeMsg` handler
- Updated `View()` to recalculate prompt height dynamically
- Updated `renderMainContent()` for dynamic content height
- Applies config `PromptExpandedHeight` on initialization

**commandline.go**:
- Added message types (`ChangeModeMsg`, `commandReadyMsg`, etc.)
- Added `HandleKey()` method
- Updated `EnterCommandMode()` and `ExitCommandMode()` to return messages

**content.go**:
- Updated `ShowChat()`, `ShowHelp()`, `ShowModels()`, `ShowResume()` to return `ChangeModeMsg`
- Updated exit handlers to return commands
- Updated selection handlers to batch commands

---

## Testing

All refactorings maintain backward compatibility:

✅ All existing tests pass  
✅ No behavior changes for users  
✅ Mode display works correctly for all states  
✅ Completion dialog works in command line mode  
✅ History navigation works correctly  
✅ Vim-style partial command matching integrated  
✅ Prompt height grows to PromptExpandedHeight for multiline input  
✅ Prompt shrinks when cleared or in scroll mode  
✅ Dynamic content height adjusts with prompt size  

---

## Summary

These refactorings established a clean, maintainable architecture:

1. **Mode Management**: Single source of truth with centralized message handling
2. **SELECT Mode Unification**: Merged MODELS and RESUME modes into a single, extensible SELECT mode
3. **Component Boundaries**: Each component handles its own input and state
4. **Message Passing**: Clean communication via bubbletea messages
5. **No Polling**: View() just renders, no logic
6. **Dynamic Prompt Height**: Prompt grows for multiline input, shrinks in scroll mode
7. **Testability**: Components can be tested in isolation

The result is a more maintainable, extensible, and testable codebase that follows bubbletea best practices.
