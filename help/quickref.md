# Quick Reference

## Modes

  ESC      - NORMAL mode (from INSERT/COMMAND-LINE)
  i        - INSERT mode at cursor
  a        - INSERT mode after cursor
  o        - INSERT mode on new line below
  :        - COMMAND-LINE mode
  #        - LEARNING mode

## Navigation (NORMAL mode)

  h j k l  - Left, down, up, right
  w b      - Word forward/backward
  0 $      - Line start/end
  gg G     - Document start/end
  ↑ ↓      - History navigation

## Tabs

  Tab          - Next tab
  Shift+Tab    - Previous tab
  gt           - Next tab (NORMAL mode)
  gT           - Previous tab (NORMAL mode)
  :tabnew      - Open a new tab
  :tabclose    - Close current tab

## Editing (NORMAL mode)

  x        - Delete character
  dw dd D  - Delete word/line/to-end
  p        - Paste
  u Ctrl+r - Undo/redo

## Input (INSERT mode)

  Enter        - Submit prompt
  Alt+Enter    - Insert newline (Option+Enter on Mac)

## Commands (type : then command)

  :help [topic]    - Show help
  :new             - New session
  :resume          - Resume session
  :quit / :q     - Close current tab (quit app if last tab)
  :qa / :quitall - Quit the application (all tabs)
  :update          - Check for updates
  :models          - Login and select the model 
  :login           - Authenticate with a provider
  :context         - Show context info
  :export          - Export conversation
  :init            - Initialize project

## Special Features

  @filename        - Reference file (triggers completion)
  #note            - Add note to AGENTS.md
  Ctrl+C (2x)      - Quit (press twice quickly)
  Ctrl+Z           - Background Asimi
  Ctrl+O           - Toggle raw session view
  ?                - Quick help (in NORMAL mode)

## File Completion

  @        - Start file reference
  ↓↑       - Navigate files
  Enter    - Select file
  ESC      - Cancel

## Help Navigation

  j k      - Scroll line by line
  Ctrl+d u - Half page down/up
  Ctrl+f b - Full page down/up
  g G      - Top/bottom
  q ESC    - Close help

## Tips

  - Start in INSERT mode, press ESC for NORMAL mode
  - Use : for commands, @ for files, # for learning
  - Check :context to monitor token usage
  - Use :export to save conversations
  - Press ? in NORMAL mode for quick help
