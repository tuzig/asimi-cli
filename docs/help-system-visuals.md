# Help System Visual Examples

## Main Help Index

```
╭─────────────────────────────────────────────────────────────────────────╮
│                          Help: index                                     │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Asimi Help Index                                                        │
│                                                                          │
│  Welcome to Asimi - A safe, opinionated coding agent with vim-like      │
│  interface.                                                              │
│                                                                          │
│  Getting Started                                                         │
│                                                                          │
│  Asimi uses vi-style editing by default. You start in INSERT mode       │
│  where you can type normally. Press ESC to enter NORMAL mode for        │
│  navigation and commands.                                                │
│                                                                          │
│  Quick Start                                                             │
│                                                                          │
│    1. Type your question or request in INSERT mode                      │
│    2. Press Enter to send                                                │
│    3. Use @ to reference files (e.g., @main.go)                         │
│    4. Press : in NORMAL mode to enter commands                          │
│    5. Press ? in NORMAL mode for quick help                             │
│                                                                          │
│  Help Topics                                                             │
│                                                                          │
│    :help modes       - Vi modes (INSERT, NORMAL, VISUAL, COMMAND-LINE)  │
│    :help commands    - Available commands (:help, :new, :quit, etc.)    │
│    :help navigation  - Navigation keys (h, j, k, l, w, b, etc.)        │
│    :help editing     - Editing commands (i, a, o, d, y, p, etc.)       │
│    :help files       - File operations and @ references                  │
│    :help sessions    - Session management and resume                     │
│    :help context     - Context and token usage                          │
│    :help config      - Configuration options                            │
│    :help quickref    - Quick reference guide                            │
│                                                                          │
├─────────────────────────────────────────────────────────────────────────┤
│ q/ESC: close | j/k: scroll | g/G: top/bottom | Ctrl+d/u: half page     │
╰─────────────────────────────────────────────────────────────────────────╯
```

## Vi Modes Help

```
╭─────────────────────────────────────────────────────────────────────────╮
│                          Help: modes                                     │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Vi Modes                                                                │
│                                                                          │
│  Asimi uses vi-style modal editing. Each mode has a different purpose   │
│  and different key bindings.                                             │
│                                                                          │
│  INSERT Mode (Default)                                                   │
│                                                                          │
│  This is the mode you start in. Type normally to compose your message.  │
│                                                                          │
│    Status: -- INSERT --                                                  │
│    Border: Green (#00FF00)                                               │
│                                                                          │
│    Enter INSERT mode from NORMAL mode:                                   │
│      i    - Insert at cursor                                             │
│      I    - Insert at beginning of line                                  │
│      a    - Append after cursor                                          │
│      A    - Append at end of line                                        │
│      o    - Open new line below                                          │
│      O    - Open new line above                                          │
│                                                                          │
│    Exit INSERT mode:                                                     │
│      ESC  - Return to NORMAL mode                                        │
│                                                                          │
│  NORMAL Mode                                                             │
│                                                                          │
│  Navigation and command mode. Use this to move around and execute        │
│  commands.                                                               │
│                                                                          │
│    Status: -- NORMAL --                                                  │
│    Border: Yellow (#F4DB53)                                              │
│                                                                          │
│    Enter NORMAL mode:                                                    │
│      ESC  - From INSERT, VISUAL, or COMMAND-LINE mode                   │
│                                                                          │
├─────────────────────────────────────────────────────────────────────────┤
│ q/ESC: close | j/k: scroll | g/G: top/bottom | Ctrl+d/u: half page     │
╰─────────────────────────────────────────────────────────────────────────╯
```

## Quick Reference

```
╭─────────────────────────────────────────────────────────────────────────╮
│                        Help: quickref                                    │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Quick Reference                                                         │
│                                                                          │
│  Modes                                                                   │
│                                                                          │
│    ESC      - NORMAL mode (from INSERT/VISUAL/COMMAND-LINE)             │
│    i        - INSERT mode at cursor                                      │
│    a        - INSERT mode after cursor                                   │
│    o        - INSERT mode on new line below                              │
│    v        - VISUAL mode                                                │
│    :        - COMMAND-LINE mode                                          │
│    #        - LEARNING mode                                              │
│                                                                          │
│  Navigation (NORMAL mode)                                                │
│                                                                          │
│    h j k l  - Left, down, up, right                                      │
│    w b      - Word forward/backward                                      │
│    0 $      - Line start/end                                             │
│    gg G     - Document start/end                                         │
│    ↑ ↓      - History navigation                                         │
│                                                                          │
│  Editing (NORMAL mode)                                                   │
│                                                                          │
│    x        - Delete character                                           │
│    dw dd D  - Delete word/line/to-end                                    │
│    y p      - Yank (copy) and paste                                      │
│    u Ctrl+r - Undo/redo                                                  │
│                                                                          │
│  Commands (type : then command)                                          │
│                                                                          │
│    :help [topic]    - Show help                                          │
│    :new             - New session                                        │
│    :resume          - Resume session                                     │
│    :quit            - Quit                                               │
│    :models          - Select model / login                                │
│    :context         - Show context info                                  │
│    :export          - Export conversation                                │
│    :init            - Initialize project                                 │
│                                                                          │
├─────────────────────────────────────────────────────────────────────────┤
│ q/ESC: close | j/k: scroll | g/G: top/bottom | Ctrl+d/u: half page     │
╰─────────────────────────────────────────────────────────────────────────╯
```

## Commands Help

```
╭─────────────────────────────────────────────────────────────────────────╮
│                        Help: commands                                    │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Commands                                                                │
│                                                                          │
│  Commands are executed from COMMAND-LINE mode. Press : in NORMAL mode   │
│  to enter COMMAND-LINE mode, then type the command and press Enter.     │
│                                                                          │
│  Session Management                                                      │
│                                                                          │
│    :new              - Start a new conversation                          │
│    :resume           - Resume a previous session                         │
│    :quit             - Quit Asimi (also saves session)                   │
│                                                                          │
│  Configuration                                                           │
│                                                                          │
│    :models           - Select AI model / login                            │
│                                                                          │
│  Information                                                             │
│                                                                          │
│    :help [topic]     - Show help (optionally for a specific topic)      │
│    :context          - Show context usage and token information          │
│                                                                          │
│  History                                                                 │
│                                                                          │
│    :clear-history    - Clear all prompt history                          │
│                                                                          │
│  Export                                                                  │
│                                                                          │
│    :export [type]    - Export conversation to file and open in $EDITOR  │
│                        Types: conversation (default), full               │
│                                                                          │
│  Project Initialization                                                  │
│                                                                          │
│    :init             - Initialize project with infrastructure files      │
│                        Creates: AGENTS.md, Justfile, .agents/Sandbox  │
│    :init force       - Force regenerate all infrastructure files         │
│                                                                          │
├─────────────────────────────────────────────────────────────────────────┤
│ q/ESC: close | j/k: scroll | g/G: top/bottom | Ctrl+d/u: half page     │
╰─────────────────────────────────────────────────────────────────────────╯
```

## Color Scheme

The help viewer uses Asimi's color scheme:

- **Title Bar**: Magenta (#F952F9) on Black - Bold
- **Headers**: Yellow (#F4DB53) - Bold
- **Subheaders**: Cyan (#01FAFA) - Bold
- **Key Bindings**: Magenta (#F952F9) - Bold
- **Commands**: Magenta (#F952F9) on Dark Gray (#1a1a1a)
- **Footer**: Cyan (#01FAFA) on Black
- **Border**: Magenta (#F952F9)
- **Normal Text**: Default terminal color

## Navigation Hints

The footer always shows available navigation options:

```
┌─────────────────────────────────────────────────────────────────────────┐
│ q/ESC: close | j/k: scroll | g/G: top/bottom | Ctrl+d/u: half page     │
└─────────────────────────────────────────────────────────────────────────┘
```

## Quick Help Modal (? in NORMAL mode)

```
╭─────────────────────────────────────────────────────────────────────────╮
│                        Shortcuts Help                                    │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│                                                                          │
│    j/↓     - Next history                                                │
│    k/↑     - Previous history                                            │
│    :       - Enter command mode                                          │
│    i       - Insert mode at cursor                                       │
│    I       - Insert mode at line start                                   │
│    a       - Insert mode after cursor                                    │
│    A       - Insert mode at line end                                     │
│    o       - Open new line below                                         │
│    O       - Open new line above                                         │
│    v       - Enter visual mode                                           │
│    V       - Enter visual line mode                                      │
│    ?       - Show this help                                              │
│                                                                          │
│                                                                          │
╰─────────────────────────────────────────────────────────────────────────╯
```

## User Experience Flow

1. **User types `:help`**
   - Command line shows: `:help`
   - Press Enter

2. **Help viewer opens (full screen)**
   - Shows main help index
   - Title bar: "Help: index"
   - Content: Scrollable help text
   - Footer: Navigation hints

3. **User navigates**
   - Press `j` to scroll down
   - Press `k` to scroll up
   - Press `Ctrl+d` for half page down
   - Press `g` to go to top
   - Press `G` to go to bottom

4. **User closes help**
   - Press `q` or `ESC`
   - Returns to normal Asimi interface
   - Focus returns to prompt

5. **User opens specific topic**
   - Type `:help modes`
   - Shows help for vi modes
   - Same navigation available

## Responsive Design

The help viewer adapts to terminal size:

- **Small terminals** (< 80 cols): Content wraps appropriately
- **Medium terminals** (80-120 cols): Optimal viewing
- **Large terminals** (> 120 cols): Content centered with padding

## Accessibility

- **Keyboard-only navigation**: No mouse required
- **Clear visual hierarchy**: Headers, subheaders, content
- **Consistent styling**: Predictable color usage
- **Navigation hints**: Always visible in footer
- **Vim-familiar**: Uses standard vim navigation keys
