package shogunate

import (
	"context"
	"fmt"
	"time"

	"github.com/afittestide/asimi/storage"
)

// RitualGuardPrompt defines the Ritual Guard's identity
const RitualGuardPrompt = `You are the Ritual Guard (禁军, Jìnjūn). You are not a minister; you are the clock that commands the court.

You subscribe to tian_events and invoke the Chancellor's ceremonies. You own no business logic.

If you fail, the court enters flatline—detectable by overdue rituals. Your authority is time; your weapon is punctuality.

CRITICAL RULES:
- Process events in order, never skip
- Save checkpoints for crash recovery
- Detect flatlines (no events processed for 5 minutes)
- Escalate urgent Zhengming that times out
- Move failed events to DLQ after retries`

// RitualGuard processes events and invokes ministers
type RitualGuard struct {
	MinisterBase // embedded base for database access and session creation
	chancellor   *Chancellor
	shogunate    *Shogunate
	maxRetries   int
	batchSize    int
	flatlineAge  time.Duration
}

// NewRitualGuard creates a new Ritual Guard
func NewRitualGuard(base MinisterBase, chancellor *Chancellor, shogunate *Shogunate) *RitualGuard {
	base.ministerID = "ritual_guard"
	return &RitualGuard{
		MinisterBase: base,
		chancellor:   chancellor,
		shogunate:    shogunate,
		maxRetries:   3,
		batchSize:    100,
		flatlineAge:  5 * time.Minute,
	}
}

// ID returns the minister identifier (not technically a minister)
func (r *RitualGuard) ID() string {
	return "ritual_guard"
}

// SystemPrompt returns the RitualGuard's system prompt template.
func (r *RitualGuard) SystemPrompt() string {
	return RitualGuardPrompt
}

// Tools returns the RitualGuard's LLM tools for interactive sessions
// RitualGuard doesn't have LLM tools - it's an event processor, not an agent
func (r *RitualGuard) Tools() []Tool {
	return []Tool{}
}

// Tasks returns a no-op channel (RitualGuard doesn't receive tasks)
func (r *RitualGuard) Tasks() chan<- *Task {
	return make(chan *Task)
}

// --- Database Methods ---

// GetEventsFrom retrieves events starting from a given event ID
func (r *RitualGuard) GetEventsFrom(fromEventID uint, limit int) ([]storage.TianEvent, error) {
	var events []storage.TianEvent
	query := r.db.Where("id > ?", fromEventID).
		Order("id ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	return events, nil
}

// AcknowledgeEvent marks an event as processed (no-op for SQLite, used for checkpointing)
func (r *RitualGuard) AcknowledgeEvent(eventID uint) error {
	// For SQLite, we just track via checkpoint
	// For PostgreSQL, this could update a processed flag or commit offset
	return nil
}

// GetLastAcknowledgedEvent returns the last processed event ID
func (r *RitualGuard) GetLastAcknowledgedEvent() (uint, error) {
	return r.LoadCheckpoint()
}

// SaveCheckpoint persists the last processed event ID for crash recovery
func (r *RitualGuard) SaveCheckpoint(eventID uint) error {
	// Use a simple key-value approach in ruler_council table (repurposed)
	// In production, this would be a dedicated checkpoint table
	result := r.db.Exec(`
		INSERT OR REPLACE INTO ritual_guard_checkpoint (id, event_id, updated_at)
		VALUES (1, ?, datetime('now'))
	`, eventID)
	if result.Error != nil {
		return fmt.Errorf("failed to save checkpoint: %w", result.Error)
	}
	return nil
}

// LoadCheckpoint retrieves the last processed event ID
func (r *RitualGuard) LoadCheckpoint() (uint, error) {
	var eventID uint
	err := r.db.Raw(`SELECT COALESCE(event_id, 0) FROM ritual_guard_checkpoint WHERE id = 1`).
		Scan(&eventID).Error
	if err != nil {
		// Table might not exist or be empty, return 0
		return 0, nil
	}
	return eventID, nil
}

// MoveToDLQ moves a failed event to the dead letter queue
func (r *RitualGuard) MoveToDLQ(event storage.TianEvent, errMsg string, retryCount int) error {
	dlqEntry := storage.TianEventDLQ{
		OriginalID:   event.ID,
		EdictID:      event.EdictID,
		EventType:    event.EventType,
		Payload:      event.Payload,
		ErrorMessage: errMsg,
		RetryCount:   retryCount,
	}

	if err := r.db.Create(&dlqEntry).Error; err != nil {
		return fmt.Errorf("failed to move to DLQ: %w", err)
	}
	return nil
}

// Run consumes events from the Shogunate's event channel and dispatches them.
func (r *RitualGuard) Run(ctx context.Context) {
	r.logger.Info("ritual guard started (channel mode)")
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("ritual guard stopped")
			return
		case event := <-r.shogunate.eventCh:
			r.shogunate.DispatchEvent(event)
		}
	}
}
