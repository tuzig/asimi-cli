# Resume Command Specification

## Overview
The `/resume` command allows users to list and resume previous chat sessions, enabling continuity across sessions and the ability to pick up where they left off.

## Status
**Planning** - Moving from "To be Planned" to implementation phase

## Motivation
Users frequently need to:
- Continue work from a previous session after closing the application
- Switch between multiple ongoing projects/conversations
- Review past interactions and their outcomes
- Recover from accidental session termination
- Reference solutions or discussions from earlier sessions

The `/resume` command provides a user-friendly way to manage session history without manually tracking session files.

## Requirements

### Functional Requirements

1. **Session Persistence**
   - Automatically save session state when user creates meaningful interactions
   - Store conversation history (messages)
   - Store context files used in the session
   - Store session metadata (timestamp, model used, project directory)
   - Use unique session IDs for identification

2. **Session Listing**
   - Display last X sessions (X configurable in config file, default: 10)
   - Show relevant metadata for each session:
     - Session number/ID
     - Date/time of last activity
     - First user prompt (truncated to ~60 chars for display)
     - Message count
     - Model used
     - Project directory (if different from current)
   - Order by most recent first

3. **Session Selection**
   - Interactive selection UI (numbered list)
   - Keyboard navigation (up/down arrows, numbers for direct selection)
   - Cancel option (ESC or q)
   - Visual feedback for selected session

4. **Session Restoration**
   - Load complete conversation history
   - Restore context files
   - Restore session configuration (model, provider)
   - Continue from where user left off
   - Display confirmation of loaded session

### Non-Functional Requirements

1. **Performance**
   - Session listing should be fast (<200ms)
   - Session loading should complete in <1s
   - Minimal disk space usage (compress old sessions if needed)

2. **Storage**
   - Store sessions in `~/.local/share/asimi/sessions/`
   - Use JSON or TOML format for easy inspection
   - Implement session cleanup (auto-delete sessions older than N days, configurable)
   - Limit total number of stored sessions (configurable)

3. **Usability**
   - Clear, intuitive UI
   - Helpful error messages
   - Show when no sessions are available
   - Graceful handling of corrupted session files

## Design

### Data Structures

```go
// SessionMetadata stores information about a saved session
type SessionMetadata struct {
    ID            string    `json:"id"`
    CreatedAt     time.Time `json:"created_at"`
    LastUpdated   time.Time `json:"last_updated"`
    FirstPrompt   string    `json:"first_prompt"`
    MessageCount  int       `json:"message_count"`
    Model         string    `json:"model"`
    Provider      string    `json:"provider"`
    WorkingDir    string    `json:"working_dir"`
}

// SessionData stores the full session state
type SessionData struct {
    Metadata     SessionMetadata           `json:"metadata"`
    Messages     []llms.MessageContent     `json:"messages"`
    ContextFiles map[string]string         `json:"context_files"`
    Config       LLMConfig                 `json:"config"`
}

// SessionStore manages session persistence
type SessionStore struct {
    storageDir string
    maxSessions int
    maxAgeDays int
}
```

### File Storage Structure

```
~/.local/share/asimi/sessions/
├── index.json                          # Quick metadata index
├── session-2024-10-16-143022-abc123/
│   ├── metadata.json
│   └── session.json
├── session-2024-10-16-120301-def456/
│   ├── metadata.json
│   └── session.json
└── ...
```

**index.json** (for fast listing):
```json
{
  "sessions": [
    {
      "id": "2024-10-16-143022-abc123",
      "created_at": "2024-10-16T14:30:22Z",
      "last_updated": "2024-10-16T15:45:10Z",
      "first_prompt": "Help me refactor the authentication module",
      "message_count": 24,
      "model": "claude-sonnet-4-5-20250929",
      "provider": "anthropic",
      "working_dir": "/home/user/projects/myapp"
    }
  ]
}
```

### Implementation Plan

#### Phase 1: Session Storage Infrastructure
1. **Create SessionStore**
   - Implement `NewSessionStore()` constructor
   - Create storage directory if needed
   - Load/create index file

2. **Session Saving**
   - Implement `SaveSession(session *Session) error`
   - Generate unique session ID (timestamp + random suffix)
   - Serialize session data to JSON
   - Update index file
   - Trigger save on meaningful interactions (user prompts, not every message)

3. **Session Loading**
   - Implement `LoadSession(id string) (*SessionData, error)`
   - Parse session JSON
   - Validate data integrity
   - Handle missing or corrupted files

4. **Session Listing**
   - Implement `ListSessions(limit int) ([]SessionMetadata, error)`
   - Read from index file for speed
   - Sort by last_updated descending
   - Apply limit

#### Phase 2: Command Implementation
1. **Add `/resume` Command**
   - Register in `commands.go`
   - Create handler function

2. **Session Selection Modal**
   ```go
   type SessionSelectionModal struct {
       sessions     []SessionMetadata
       selectedIdx  int
       scrollOffset int
   }
   
   func (m SessionSelectionModal) Update(msg tea.Msg) (tea.Model, tea.Cmd)
   func (m SessionSelectionModal) View() string
   ```

3. **Selection UI**
   - Display sessions in a scrollable list
   - Show metadata in readable format
   - Highlight selected session
   - Support keyboard navigation
   - Handle selection confirmation

4. **Session Restoration**
   - Load selected session data
   - Replace current session messages
   - Restore context files
   - Update session configuration
   - Display success message

#### Phase 3: Auto-Save Integration
1. **Save Triggers**
   - Save after each user prompt (or every N messages)
   - Save on application exit
   - Save on `/new` command (before clearing)
   - Don't save empty sessions

2. **Background Saving**
   - Implement async save to avoid blocking UI
   - Queue saves to prevent race conditions
   - Handle save errors gracefully

#### Phase 4: Session Management
1. **Cleanup**
   - Implement `CleanupOldSessions(maxAge int, maxCount int) error`
   - Run on application startup
   - Delete sessions older than maxAge days
   - Keep only maxCount most recent sessions
   - Update index file

2. **Session Deletion**
   - Optional: Add `/delete-session` command
   - Remove from index and filesystem
   - Confirmation prompt for safety

#### Phase 5: Testing & Documentation
1. **Tests**
   - Unit tests for SessionStore operations
   - Test save/load roundtrip
   - Test cleanup logic
   - Test edge cases (empty sessions, corrupted data)

2. **Documentation**
   - Update help text
   - Add examples to user guide
   - Document configuration options

### Configuration

Add to `config.go`:
```go
type SessionConfig struct {
    Enabled            bool   `koanf:"enabled"`
    MaxSessions        int    `koanf:"max_sessions"`
    MaxAgeDays         int    `koanf:"max_age_days"`
    ListLimit          int    `koanf:"list_limit"`
    AutoSave           bool   `koanf:"auto_save"`
    SaveInterval       int    `koanf:"save_interval"` // Save every N messages
}
```

Add to default config:
```toml
[session]
enabled = true
max_sessions = 50
max_age_days = 30
list_limit = 10
auto_save = true
save_interval = 2  # Save after every 2 user messages
```

### User Flow

1. **User types `/resume`**
   ```
   ┌─────────────────────────────────────────────────────────────┐
   │ Resume Session                                              │
   │                                                             │
   │ > 1. [Today 14:30] Help me refactor the authentication ... │
   │      24 messages • claude-sonnet-4-5 • /home/user/myapp    │
   │                                                             │
   │   2. [Today 12:03] Fix the build errors in frontend/        │
   │      12 messages • claude-sonnet-4-5 • /home/user/myapp    │
   │                                                             │
   │   3. [Yesterday 16:45] Create API documentation             │
   │      8 messages • claude-haiku-4 • /home/user/docs         │
   │                                                             │
   │ ↑/↓: Navigate • Enter: Select • Esc: Cancel                │
   └─────────────────────────────────────────────────────────────┘
   ```

2. **User selects session**
   - Press Enter or number key
   - Modal closes
   - Session loads

3. **Confirmation message**
   ```
   ○ Resumed session from Today 14:30
     24 messages loaded • claude-sonnet-4-5
     
   [Previous conversation appears in chat]
   ```

### Example Output

**No sessions available:**
```
No previous sessions found. Start chatting to create a new session!
```

**Session list:**
```
Recent Sessions (use ↑/↓ to select, Enter to load, Esc to cancel):

 1. [Oct 16, 14:30] Help me refactor the authentication module
    24 messages • claude-sonnet-4-5 • ~/projects/myapp

 2. [Oct 16, 12:03] Fix the build errors in frontend/ directory  
    12 messages • claude-sonnet-4-5 • ~/projects/myapp

 3. [Oct 15, 16:45] Create API documentation for user endpoints
    8 messages • claude-haiku-4 • ~/projects/docs

 4. [Oct 15, 10:20] Debug the memory leak in scheduler.go
    31 messages • claude-sonnet-4-5 • ~/projects/asimi-cli

 5. [Oct 14, 15:12] Add OAuth authentication flow
    19 messages • claude-sonnet-4-5 • ~/projects/myapp
```

## Testing Strategy

### Unit Tests
1. SessionStore creation and initialization
2. Save/load roundtrip with various session sizes
3. Index file management
4. Cleanup logic (age and count limits)
5. Corrupted data handling
6. Empty session handling

### Integration Tests
1. Full command flow (list → select → restore)
2. Auto-save on user prompts
3. Session restoration with context files
4. Multiple session saves and loads

### Manual Tests
1. Create several sessions and resume each
2. Test with empty history
3. Test with very long conversations
4. Test session cleanup on startup
5. Test across application restarts
6. Test canceling session selection

## Dependencies
- Session state management (`session.go`)
- File I/O and JSON serialization
- TUI modal system (similar to model selection)
- Command registry (`commands.go`)
- Configuration system

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Disk space growth from many sessions | Medium | Auto-cleanup, configurable limits, compression |
| Privacy concerns with saved conversations | High | Clear documentation, allow disable, secure permissions |
| Corrupted session files breaking load | Medium | Validate on load, skip corrupted files, backup index |
| Performance with large sessions | Low | Lazy loading, pagination, limit message count |
| Confusion with multiple similar sessions | Low | Better metadata, preview first few messages |

## Success Criteria
- [ ] Sessions save automatically on user prompts
- [ ] `/resume` command lists recent sessions
- [ ] Session selection works smoothly
- [ ] Loaded sessions restore full conversation
- [ ] Auto-cleanup prevents disk bloat
- [ ] Works reliably across restarts
- [ ] Unit test coverage >80%
- [ ] Documentation complete
- [ ] User feedback positive

## Future Enhancements
1. **Search Sessions**: Search by content, date, or tags
2. **Session Tags**: User-defined tags for organization
3. **Export Sessions**: Export to markdown or JSON
4. **Share Sessions**: Export sanitized sessions for sharing
5. **Session Stats**: Show token usage, duration, etc.
6. **Cloud Sync**: Optional cloud backup of sessions
7. **Session Merge**: Combine related sessions
8. **Session Notes**: Add notes/annotations to sessions

## References
- TUI modal implementations (`login.go`, `models.go`)
- Command system (`commands.go`)
- Session management (`session.go`)
- Configuration system (`config.go`)
- File storage patterns (existing keyring/config storage)
