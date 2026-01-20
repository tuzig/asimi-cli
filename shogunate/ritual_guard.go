package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// --- Minister ---

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
	MinisterBase            // embedded base for database access and session creation
	chancellor   *Chancellor
	maxRetries   int
	batchSize    int
	flatlineAge  time.Duration
}

// NewRitualGuard creates a new Ritual Guard
func NewRitualGuard(db *gorm.DB, chancellor *Chancellor, logger *slog.Logger) *RitualGuard {
	if logger == nil {
		logger = slog.Default()
	}
	return &RitualGuard{
		MinisterBase: MinisterBase{db: db, ministerID: "ritual_guard", logger: logger},
		chancellor:   chancellor,
		maxRetries:   3,
		batchSize:    100,
		flatlineAge:  5 * time.Minute,
	}
}

// ID returns the minister identifier (not technically a minister)
func (r *RitualGuard) ID() string {
	return "ritual_guard"
}

// Prompt returns the RitualGuard's system prompt
func (r *RitualGuard) Role() string {
	return RitualGuardPrompt
}

// Tools returns the RitualGuard's LLM tools for interactive sessions
// RitualGuard doesn't have LLM tools - it's an event processor, not an agent
func (r *RitualGuard) Tools(notify NotifyFunc) []Tool {
	return []Tool{}
}

// Execute is not used by RitualGuard - it runs via Run() instead
func (r *RitualGuard) Execute(ctx context.Context, edictID string) (bool, error) {
	return true, nil
}

// --- Database Methods ---

// GetEventsFrom retrieves events starting from a given event ID
func (r *RitualGuard) GetEventsFrom(fromEventID int64, limit int) ([]storage.TianEvent, error) {
	var events []storage.TianEvent
	query := r.db.Where("event_id > ?", fromEventID).
		Order("event_id ASC")
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
func (r *RitualGuard) AcknowledgeEvent(eventID int64) error {
	// For SQLite, we just track via checkpoint
	// For PostgreSQL, this could update a processed flag or commit offset
	return nil
}

// GetLastAcknowledgedEvent returns the last processed event ID
func (r *RitualGuard) GetLastAcknowledgedEvent() (int64, error) {
	return r.LoadCheckpoint()
}

// SaveCheckpoint persists the last processed event ID for crash recovery
func (r *RitualGuard) SaveCheckpoint(eventID int64) error {
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
func (r *RitualGuard) LoadCheckpoint() (int64, error) {
	var eventID int64
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
		EventID:           event.EventID,
		EdictID:           event.EdictID,
		EventType:         event.EventType,
		Payload:           event.Payload,
		ErrorMessage:      errMsg,
		RetryCount:        retryCount,
		OriginalCreatedAt: event.CreatedAt,
	}

	if err := r.db.Create(&dlqEntry).Error; err != nil {
		return fmt.Errorf("failed to move to DLQ: %w", err)
	}
	return nil
}

// --- Execute Logic ---

// Run processes events from the Tian ledger
func (r *RitualGuard) Run(ctx context.Context) error {
	// Get last acknowledged event
	lastEventID, err := r.GetLastAcknowledgedEvent()
	if err != nil {
		return fmt.Errorf("get last acknowledged: %w", err)
	}

	// Get events to process
	events, err := r.GetEventsFrom(lastEventID, r.batchSize)
	if err != nil {
		return fmt.Errorf("get events: %w", err)
	}

	if len(events) == 0 {
		r.logger.Debug("no events to process")
		return nil
	}

	// Process each event
	for _, event := range events {
		if err := r.processEvent(ctx, event); err != nil {
			r.logger.Error("event processing failed",
				"event_id", event.EventID,
				"error", err)
			// Continue processing other events
		}

		// Acknowledge event
		if err := r.AcknowledgeEvent(event.EventID); err != nil {
			r.logger.Error("failed to acknowledge event",
				"event_id", event.EventID,
				"error", err)
		}

		// Save checkpoint periodically
		if err := r.SaveCheckpoint(event.EventID); err != nil {
			r.logger.Warn("failed to save checkpoint", "error", err)
		}
	}

	r.logger.Info("event batch processed", "count", len(events))
	return nil
}

// processEvent handles a single event
func (r *RitualGuard) processEvent(ctx context.Context, event storage.TianEvent) error {
	r.logger.Debug("processing event",
		"event_id", event.EventID,
		"type", event.EventType,
		"edict_id", event.EdictID)

	// Route event based on type
	switch event.EventType {
	// Edict lifecycle events
	case "edict_assigned", "edict_created":
		// Invoke Chancellor for new edict
		if r.chancellor != nil {
			_, err := r.chancellor.Execute(ctx, event.EdictID)
			if err != nil {
				return fmt.Errorf("chancellor execute: %w", err)
			}
		}

	case "phase_changed", "forge_committed":
		// Continue edict processing after phase change
		if r.chancellor != nil {
			_, err := r.chancellor.Execute(ctx, event.EdictID)
			if err != nil {
				return fmt.Errorf("chancellor execute: %w", err)
			}
		}

	// Ritual/Workflow events
	case "ritual_started":
		r.logger.Info("ritual started", "edict_id", event.EdictID)

	case "ritual_completed":
		r.logger.Info("ritual completed", "edict_id", event.EdictID)
		// Could trigger post-completion actions here

	case "ritual_failed":
		r.logger.Error("ritual failed", "edict_id", event.EdictID, "payload", string(event.Payload))
		// Could trigger error handling/notification here

	case "step_completed":
		r.logger.Debug("step completed", "edict_id", event.EdictID, "payload", string(event.Payload))
		// Continue workflow execution if needed

	// Ling events
	case "ling_created":
		r.logger.Debug("ling created", "edict_id", event.EdictID, "payload", string(event.Payload))
		// Could wake up Forge to process new ling

	// Zhengming events
	case "zhengming_needed":
		r.logger.Info("zhengming needed", "edict_id", event.EdictID)
		// Workflow will pause waiting for answer

	case "zhengming_answered":
		// Handle clarification response - resume paused workflow
		if r.chancellor != nil {
			r.logger.Info("zhengming answered, resuming", "edict_id", event.EdictID)
			// Re-execute to continue workflow
			_, err := r.chancellor.Execute(ctx, event.EdictID)
			if err != nil {
				return fmt.Errorf("chancellor execute after zhengming: %w", err)
			}
		}

	case "edict_cancelled":
		r.logger.Info("edict cancelled", "edict_id", event.EdictID)
		// No action needed - workflow will check state

	default:
		r.logger.Debug("unknown event type, skipping", "type", event.EventType)
	}

	return nil
}

