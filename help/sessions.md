# Session Management

Asimi can save and resume your coding sessions, preserving the entire
conversation history and context.

## Starting a New Session

  :new             - Start a fresh conversation
                     Clears chat history and context

## Resuming Sessions

  :resume          - Show list of recent sessions
                     Select one to resume

The session list shows:
  - First prompt from each session
  - Time since last update
  - Project/directory

Navigation in session list:
  ↓/↑              - Navigate sessions
  Enter            - Resume selected session
  ESC              - Cancel

## Auto-Save

Sessions are automatically saved when:
  - You send a message
  - You quit Asimi
  - You start a new session

## Session Configuration

Configure session behavior in ~/.config/asimi/asimi.conf:

  [session]
  enabled = true           # Enable session persistence
  auto_save = true         # Auto-save after each message
  max_sessions = 50        # Maximum sessions to keep
  max_age_days = 30        # Delete sessions older than this
  list_limit = 20          # Number of sessions to show in :resume

## Session Storage

Sessions are stored in:
  ~/.local/share/asimi/sessions/

Each session includes:
  - Full conversation history
  - Context files
  - Model and provider information
  - Timestamps

## Exporting Sessions

  :export              - Export conversation to file
  :export conversation - Export just the conversation
  :export full         - Export with full context

The exported file opens in your $EDITOR for review or sharing.
