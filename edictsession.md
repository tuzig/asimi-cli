Remove Chancellor.edictSessions and unify all conversation into a single interactiveSession. Edict state is tracked via database queries, not per-edict LLM sessions. Ritual results flow into the interactive session.

## Architecture Change

**Before**: Chancellor maintains `map[string]*Session` for per-edict conversations + one rulingSession
**After**: Chancellor maintains only `interactiveSession` — all conversation, all edicts, one memory

## Changes Required

### 1. shogunate/chancellor.go:28-32 — Remove edictSessions field
```go
type Chancellor struct {
	*MinisterBase
	shogunate        *Shogunate
	taskChan         chan *Task
	eventChan        chan Event
	interactiveSession *Session  // Renamed from rulingSession, ONLY session
	// REMOVED: edictSessions map[string]*Session
}
```

### 2. shogunate/chancellor.go:35-42 — Update NewChancellor
```go
func NewChancellor(base *MinisterBase) *Chancellor {
	base.ministerID = "chancellor"
	return &Chancellor{
		MinisterBase: base,
		taskChan:     make(chan *Task, 10),
		eventChan:    make(chan Event, 256),
		// REMOVED: edictSessions: make(map[string]*Session),
	}
}
```

### 3. shogunate/chancellor.go:550-560 — Simplify GetSession
```go
// GetSession returns the Chancellor's interactive session.
// The edictID parameter is ignored — all conversation uses the same session.
func (c *Chancellor) GetSession(edictID string) *Session {
	return c.interactiveSession
}
```

### 4. shogunate/chancellor.go:560-580 — Rename and simplify session methods
```go
// GetInteractiveSession returns the Chancellor's interactive session
func (c *Chancellor) GetInteractiveSession() *Session {
	return c.interactiveSession
}

// ResetInteractiveSession nils out the interactive session
func (c *Chancellor) ResetInteractiveSession() {
	c.interactiveSession = nil
}

// RestoreInteractiveSession creates a fully-wired interactive session and injects loaded history
func (c *Chancellor) RestoreInteractiveSession(msgs []llms.MessageContent) error {
	sess, err := CreateSession(c, c.model, c.config, c.notify, "chancellor")
	if err != nil {
		return err
	}
	sess.SetMessages(msgs)
	sess.TabType = "interactive"  // Changed from "ruling"
	c.interactiveSession = sess
	return nil
}
```

### 5. shogunate/chancellor.go:730-780 — Simplify brewWithStreaming
```go
func (c *Chancellor) brewWithStreaming(ctx context.Context, edictID, prompt string, contextFiles map[string]string) {
	if c.model == nil {
		c.notify(StreamErrorMsg{TabID: "chancellor", Err: fmt.Errorf("LLM not configured")})
		return
	}

	// Always use interactiveSession — no per-edict sessions
	if c.interactiveSession == nil {
		var err error
		c.interactiveSession, err = CreateSession(c, c.model, c.config, c.notify, "chancellor")
		if err != nil {
			c.notify(StreamErrorMsg{TabID: "chancellor", Err: fmt.Errorf("failed to create session: %w", err)})
			return
		}
		c.interactiveSession.TabType = "interactive"
		c.logger.Info("chancellor created interactive session")
	}

	c.logger.Debug("context files received from TUI", "count", len(contextFiles))

	// Pass edictID in prompt context, not session
	fullPrompt := prompt
	if edictID != "" {
		fullPrompt = fmt.Sprintf("[Context: edict %s]\n\n%s", edictID, prompt)
	}

	_, err := c.interactiveSession.AskWithStreaming(ctx, fullPrompt, contextFiles)
	if err != nil && ctx.Err() == nil {
		c.notify(StreamErrorMsg{TabID: "chancellor", Err: err})
		return
	}
	c.notify(StreamDoneMsg{TabID: "chancellor"})
}
```

### 6. shogunate/chancellor.go:835-875 — Update handleEdictCreated
```go
func (c *Chancellor) handleEdictCreated(ctx context.Context, edictID string) {
	c.logger.Info("handling edict created", "edict_id", edictID)

	// REMOVED: Check for existing edict session — no per-edict sessions anymore

	if len(c.shogunate.GetRitualRegistry().List()) == 0 {
		c.logger.Warn("no rituals available", "edict_id", edictID)
		return
	}

	edict, err := c.GetEdict(edictID)
	if err != nil {
		c.logger.Error("failed to get edict", "edict_id", edictID, "error", err)
		return
	}

	// Include edict context in the work prompt
	work := fmt.Sprintf("New edict %s: %s\n\nChoose the appropriate ritual and enact it.", edictID, edict.Intent)
	
	// Use interactiveSession for multi-turn if needed
	task := &Task{
		Ctx:     ctx,
		EdictID: edictID,
		Work:    work,
		Done:    make(chan Result, 1),
		// REMOVED: Session field — rituals manage their own sessions
	}

	select {
	case c.taskChan <- task:
	default:
		c.logger.Warn("chancellor task channel full", "edict_id", edictID)
	}
}
```

### 7. shogunate/chancellor.go:790-830 — Update processTask
```go
func (c *Chancellor) processTask(ctx context.Context, task *Task) {
	c.logger.Info("chancellor processing task",
		"edict_id", task.EdictID,
		"work", truncateString(task.Work, 50))

	var output string
	var taskErr error

	if c.model != nil {
		// Always use interactiveSession for task conversation
		if c.interactiveSession == nil {
			var err error
			c.interactiveSession, err = CreateSession(c, c.model, c.config, c.notify, "chancellor")
			if err != nil {
				taskErr = fmt.Errorf("failed to create session: %w", err)
			} else {
				c.interactiveSession.TabType = "interactive"
			}
		}
		
		if taskErr == nil {
			_, taskErr = c.interactiveSession.AskWithStreaming(ctx, task.Work, nil)
		}
	} else {
		output = "chancellor task acknowledged (no LLM configured)"
	}

	result := Result{
		MinisterID: c.ID(),
		Sealed:     true,
		Output:     output,
		// REMOVED: Session field — caller uses interactiveSession directly
		Err: taskErr,
	}

	if task.Done != nil {
		select {
		case task.Done <- result:
		default:
			c.logger.Warn("done channel full, dropping result", "edict_id", task.EdictID)
		}
	}
}
```

### 8. Update all references to rulingSession → interactiveSession
- Line 553, 560, 565, 576, 738, 740, 745, 748

### 9. Update shogunate/shogunate.go — Update method calls
- `GetRulingSession()` → `GetInteractiveSession()`
- `ResetRulingSession()` → `ResetInteractiveSession()`
- `RestoreRulingSession()` → `RestoreInteractiveSession()`

### 10. Update tui.go — Update method calls
- Same method renames as above

## Benefits

1. **Simplified architecture** — One session to manage, not N+1
2. **Natural conversation flow** — LLM tracks edict context from dialogue, like a human PM
3. **Reduced memory** — No per-edict session overhead
4. **Easier CTRL-C handling** — One interactive session per tab, clear cancellation
5. **Database as source of truth** — Edict state from DB, not LLM memory

## Migration Notes

- Existing edictSessions content is NOT migrated — it becomes part of conversation history naturally
- LLM must use tools (`get_edict_status`, `list_edicts`) to fetch edict state
- Ritual results should be posted to interactive session for visibility
- Context compaction becomes more important — summarize old edict work periodically

Evidence: shogunate/chancellor.go:28-32 (edictSessions field), shogunate/chancellor.go:555 (GetSession returns edictSessions[edictID]), shogunate/chancellor.go:752-761 (brewWithStreaming creates edict sessions), shogunate/chancellor.go:844 (handleEdictCreated checks edictSessions), shogunate/chancellor.go:524-530 (Run loop context merging)
