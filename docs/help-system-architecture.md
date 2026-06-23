# Help System Architecture

## Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                         TUIModel                             │
│                                                              │
│  ┌────────────┐  ┌────────────┐  ┌──────────────┐         │
│  │   Status   │  │   Prompt   │  │     Chat     │         │
│  └────────────┘  └────────────┘  └──────────────┘         │
│                                                              │
│  ┌────────────┐  ┌────────────┐  ┌──────────────┐         │
│  │ CommandLine│  │ Completions│  │    Modals    │         │
│  └────────────┘  └────────────┘  └──────────────┘         │
│                                                              │
│  ┌──────────────────────────────────────────────┐          │
│  │           HelpViewer                         │          │
│  │  ┌────────────────────────────────────────┐  │          │
│  │  │         Viewport (scrollable)          │  │          │
│  │  │  ┌──────────────────────────────────┐  │  │          │
│  │  │  │      Help Content                │  │  │          │
│  │  │  │  - index                         │  │  │          │
│  │  │  │  - modes                         │  │  │          │
│  │  │  │  - commands                      │  │  │          │
│  │  │  │  - navigation                    │  │  │          │
│  │  │  │  - editing                       │  │  │          │
│  │  │  │  - files                         │  │  │          │
│  │  │  │  - sessions                      │  │  │          │
│  │  │  │  - context                       │  │  │          │
│  │  │  │  - config                        │  │  │          │
│  │  │  │  - quickref                      │  │  │          │
│  │  │  └──────────────────────────────────┘  │  │          │
│  │  └────────────────────────────────────────┘  │          │
│  └──────────────────────────────────────────────┘          │
└─────────────────────────────────────────────────────────────┘
```

## Message Flow

```
User Input: ":help modes" + Enter
        │
        ▼
┌─────────────────────────┐
│ PromptComponent.Update()│
│                         │
│ Detects Enter key       │
│ Creates SubmitPromptMsg │
└───────────┬─────────────┘
            │
            ▼
┌──────────────────────────┐
│ handleCustomMessages()   │
│                          │
│ Receives SubmitPromptMsg │
│ Parses ":help modes"     │
│ Calls handleHelpCommand()│
└───────────┬──────────────┘
            │
            ▼
┌─────────────────────┐
│ handleHelpCommand() │
│                     │
│ Creates message:    │
│ showHelpMsg{        │
│   topic: "modes"    │
│ }                   │
└──────────┬──────────┘
           │
           ▼
┌──────────────────────────┐
│ handleCustomMessages()   │
│                          │
│ Receives showHelpMsg     │
│ Calls:                   │
│ helpViewer.Show("modes") │
└────────────┬─────────────┘
             │
             ▼
┌────────────────────────────┐
│    HelpViewer.Show()       │
│                            │
│ 1. Sets visible = true     │
│ 2. Sets topic = "modes"    │
│ 3. Renders help content    │
│ 4. Scrolls to top          │
└──────────────┬─────────────┘
               │
               ▼
┌──────────────────────────────┐
│   User navigates with:       │
│   - j/k (line by line)       │
│   - Ctrl+d/u (half page)     │
│   - Ctrl+f/b (full page)     │
│   - g/G (top/bottom)         │
└────────────┬─────────────────┘
             │
             ▼
┌────────────────────────────┐
│  User presses 'q' or ESC   │
└────────────┬───────────────┘
             │
             ▼
┌────────────────────────────┐
│   HelpViewer.Hide()        │
│                            │
│   Sets visible = false     │
└────────────┬───────────────┘
             │
             ▼
┌────────────────────────────┐
│   Focus returns to prompt  │
└────────────────────────────┘
```

## Prompt Submission Architecture

The prompt submission flow has been refactored to use a message-based architecture:

```
┌──────────────────────────────────────────────────────────┐
│                  Prompt Submission Flow                   │
└──────────────────────────────────────────────────────────┘

User presses Enter (Vi insert or normal mode)
        │
        ▼
┌─────────────────────────────────────────────────────────┐
│  PromptComponent.Update(tea.KeyMsg)                     │
│                                                          │
│  1. Detects Enter key (not Alt+Enter)                   │
│  2. Validates prompt is non-empty                        │
│  3. Clears textarea and resets cursor                    │
│  4. Returns SubmitPromptMsg{Prompt: content}            │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  TUIModel.handleCustomMessages(SubmitPromptMsg)         │
│                                                          │
│  1. Clears toasts and refreshes git info                │
│  2. Handles history navigation/rollback if needed       │
│  3. Adds message to chat history                        │
│  4. Auto-compacts if context is low                     │
│  5. Starts streaming response from LLM                  │
│  6. Updates prompt history                              │
│  7. Saves to persistent history                         │
└─────────────────────────────────────────────────────────┘

Benefits of this architecture:
- Separation of concerns: PromptComponent handles UI, TUIModel handles logic
- Consistent behavior across Vi modes
- Proper command propagation from nested components
- Easier to test and maintain
- Prevents empty prompt submission
```

## State Diagram

```
┌─────────────┐
│   Initial   │
│  (hidden)   │
└──────┬──────┘
       │
       │ :help [topic]
       │
       ▼
┌─────────────┐
│   Visible   │◄────────┐
│  (showing   │         │
│   help)     │         │
└──────┬──────┘         │
       │                │
       │ j/k/g/G/       │
       │ Ctrl+d/u/f/b   │
       │                │
       └────────────────┘
       │
       │ q or ESC
       │
       ▼
┌─────────────┐
│   Hidden    │
│  (closed)   │
└─────────────┘
```

## Rendering Pipeline

```
TUIModel.View()
    │
    ├─► renderMainContent()
    │   └─► chat.View() or homeView() or rawView()
    │
    ├─► prompt.View()
    │
    ├─► commandLine.View()
    │
    ├─► composeBaseView()
    │   └─► lipgloss.JoinVertical()
    │
    ├─► overlayCompletionDialog() (if active)
    │
    └─► applyModalOverlays()
        │
        ├─► helpViewer.View() (if visible) ◄── FULL SCREEN
        │   │
        │   ├─► renderHelpContent(topic)
        │   │   │
        │   │   ├─► getHelpTopic(topic)
        │   │   │   └─► Returns help text
        │   │   │
        │   │   └─► Apply styling
        │   │       ├─► Headers (bold yellow)
        │   │       ├─► Subheaders (bold cyan)
        │   │       ├─► Keys (bold magenta)
        │   │       └─► Code blocks (background)
        │   │
        │   ├─► Title bar
        │   ├─► Viewport (scrollable content)
        │   └─► Footer (navigation hints)
        │
        ├─► codeInputModal.Render() (if active)
        ├─► modelSelectionModal.Render() (if active)
        └─► sessionModal.Render() (if active)
```

## Key Interactions

```
┌──────────────────────────────────────────────────────────┐
│                    User Actions                          │
└──────────────────────────────────────────────────────────┘
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
        ▼                  ▼                  ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  :help       │  │  :help modes │  │  ? (NORMAL)  │
│  (index)     │  │  (specific)  │  │  (quick)     │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                  │
       └─────────────────┼──────────────────┘
                         │
                         ▼
              ┌──────────────────┐
              │   Help Viewer    │
              │   (full screen)  │
              └──────────────────┘
                         │
        ┌────────────────┼────────────────┐
        │                │                │
        ▼                ▼                ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  Navigate    │  │  Read        │  │  Close       │
│  j/k/g/G     │  │  Content     │  │  q/ESC       │
└──────────────┘  └──────────────┘  └──────────────┘
```

## Data Flow

```
Help Content (constants in help.go)
    │
    ├─► helpIndex
    ├─► helpModes
    ├─► helpCommands
    ├─► helpNavigation
    ├─► helpEditing
    ├─► helpFiles
    ├─► helpSessions
    ├─► helpContext
    ├─► helpConfig
    └─► helpQuickRef
        │
        ▼
getHelpTopic(topic) ──► Returns raw text
        │
        ▼
renderHelpContent(topic) ──► Applies styling
        │
        ├─► Parse lines
        ├─► Detect headers (#, ##)
        ├─► Detect key bindings (indented with -)
        ├─► Detect commands (:, /)
        └─► Apply lipgloss styles
            │
            ▼
viewport.SetContent(styledContent)
            │
            ▼
viewport.View() ──► Scrollable display
```

## Integration Points

```
┌─────────────────────────────────────────────────────────┐
│                    Asimi Application                     │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  Prompt Component                                        │
│  ├─► Handles Enter key in Vi insert/normal modes        │
│  └─► Emits SubmitPromptMsg with prompt content          │
│                                                          │
│  Command Registry                                        │
│  ├─► /help ──► handleHelpCommand()                      │
│  │                                                       │
│  TUI Model                                               │
│  ├─► helpViewer *HelpViewer                             │
│  ├─► handleKeyMsg() ──► Routes to helpViewer.Update()  │
│  ├─► handleCustomMessages()                             │
│  │   ├─► Receives SubmitPromptMsg                       │
│  │   ├─► Parses commands (e.g., :help)                  │
│  │   └─► Shows help via showHelpMsg                     │
│  ├─► handleWindowSizeMsg() ──► Resizes help viewer      │
│  └─► applyModalOverlays() ──► Renders help viewer       │
│                                                          │
│  Help Viewer                                             │
│  ├─► Show(topic) ──► Display help                       │
│  ├─► Hide() ──► Close help                              │
│  ├─► Update(msg) ──► Handle navigation                  │
│  ├─► View() ──► Render help                             │
│  └─► renderHelpContent(topic) ──► Style content         │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

## Styling System

```
Help Content (plain text)
    │
    ▼
Line-by-line processing
    │
    ├─► "# Header" ──► headerStyle.Render()
    │                  (Bold Yellow)
    │
    ├─► "## Subheader" ──► subheaderStyle.Render()
    │                      (Bold Cyan)
    │
    ├─► "  key - desc" ──► keyStyle.Render(key) + desc
    │                      (Bold Magenta + normal)
    │
    ├─► ":command" ──► codeStyle.Render()
    │                  (Magenta on dark background)
    │
    └─► "normal text" ──► No styling
        │
        ▼
Styled lines joined
    │
    ▼
viewport.SetContent()
```

## Navigation System

```
User Input
    │
    ├─► j/k or ↓/↑ ──► viewport.LineUp/Down(1)
    │
    ├─► Ctrl+d ──► viewport.HalfViewDown()
    │
    ├─► Ctrl+u ──► viewport.HalfViewUp()
    │
    ├─► Ctrl+f ──► viewport.ViewDown()
    │
    ├─► Ctrl+b ──► viewport.ViewUp()
    │
    ├─► g ──► viewport.GotoTop()
    │
    ├─► G ──► viewport.GotoBottom()
    │
    └─► q/ESC ──► helpViewer.Hide()
```

This architecture provides a clean, maintainable, and extensible help system that integrates seamlessly with Asimi's existing UI framework.
