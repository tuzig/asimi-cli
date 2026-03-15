package shogunate

import (
	"context"
	"fmt"
	"time"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/runners"
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

// RitualGuard processes events and owns ritual/event infrastructure
type RitualGuard struct {
	*MinisterBase // embedded base for database access and session creation
	chancellor    *Chancellor
	ritualRegistry *RitualRegistry
	ritualRunner   *RitualRunner
	eventRegistry  *EventRegistry
	eventCh        chan Event
	maxRetries     int
	batchSize      int
	flatlineAge    time.Duration
	// Dependency injection (replaces *Shogunate back-reference)
	getMinister  func(id string) Minister
	streamingCtx func() context.Context
}

// RitualGuardOpts configures a new RitualGuard.
type RitualGuardOpts struct {
	Base         *MinisterBase
	// TODO: remove chancellor as we have GetMinister("chancellor")
	Chancellor   *Chancellor
	Runner       runners.Runner
	GetMinister  func(id string) Minister
	StreamingCtx func() context.Context
}

// NewRitualGuard creates a new Ritual Guard that owns all ritual/event infrastructure.
func NewRitualGuard(opts RitualGuardOpts) *RitualGuard {
	opts.Base.ministerID = "ritual_guard"
	registry := NewRitualRegistry()
	eventRegistry := NewEventRegistry()
	eventCh := make(chan Event, 256)

	rg := &RitualGuard{
		MinisterBase:   opts.Base,
		chancellor:     opts.Chancellor,
		ritualRegistry: registry,
		eventRegistry:  eventRegistry,
		eventCh:        eventCh,
		maxRetries:     3,
		batchSize:      100,
		flatlineAge:    5 * time.Minute,
		getMinister:    opts.GetMinister,
		streamingCtx:   opts.StreamingCtx,
	}

	// Create ritual runner with injected functions
	rg.ritualRunner = NewRitualRunner(
		registry,
		opts.GetMinister,
		rg.PublishEvent,
		opts.Base.db,
		opts.Runner,
		opts.Base.logger,
	)

	return rg
}

// ID returns the minister identifier (not technically a minister)
func (rg *RitualGuard) ID() string {
	return "ritual_guard"
}

// SystemPrompt returns the RitualGuard's system prompt template.
func (rg *RitualGuard) SystemPrompt() string {
	return RitualGuardPrompt
}

// Tools returns the RitualGuard's LLM tools for interactive sessions
// RitualGuard doesn't have LLM tools - it's an event processor, not an agent
func (rg *RitualGuard) Tools() []Tool {
	return []Tool{}
}

// Tasks returns a no-op channel (RitualGuard doesn't receive tasks)
func (rg *RitualGuard) Tasks() chan<- *Task {
	return make(chan *Task)
}

// --- Event lifecycle ---

// PublishEvent persists an event to the DB and sends it to the event channel.
func (rg *RitualGuard) PublishEvent(edictID string, eventType storage.ShogunateEvent, payload storage.JSON) string {
	if rg.db != nil {
		dbEvent := storage.TianEvent{
			EdictID:   edictID,
			EventType: eventType,
			Payload:   payload,
		}
		if err := rg.db.Create(&dbEvent).Error; err != nil {
			rg.logger.Error("failed to persist event", "type", eventType, "error", err)
		}
	}
	select {
	case rg.eventCh <- Event{Type: eventType, EdictID: edictID, Payload: map[string]interface{}(payload)}:
	default:
		rg.logger.Warn("event channel full, persisted to DB only", "type", eventType)
	}
	return edictID
}

// DispatchEvent dispatches an event to all subscribers and triggers event-driven rituals.
func (rg *RitualGuard) DispatchEvent(event Event) {
	if rg.eventRegistry == nil {
		return
	}
	rg.eventRegistry.Dispatch(event)

	// Trigger event-driven rituals
	if rg.ritualRegistry != nil && rg.ritualRunner != nil && rg.streamingCtx != nil {
		rituals := rg.ritualRegistry.GetByEvent(string(event.Type))
		sourceRitual, _ := event.Payload["ritual"].(string)
		for _, ritual := range rituals {
			if ritual.Name == sourceRitual {
				rg.logger.Debug("skipping self-triggered ritual",
					"ritual", ritual.Name, "event", event.Type)
				continue
			}
			edictID := event.EdictID
			inputs := map[string]string{"edict_id": edictID}
			go func(r *RitualDef) {
				ctx := rg.streamingCtx()
				exec, err := rg.ritualRunner.Start(ctx, r.Name, edictID, inputs, rg.notify)
				if err != nil {
					rg.logger.Warn("failed to start event-triggered ritual",
						"ritual", r.Name, "event", event.Type, "error", err)
					return
				}
				if err := rg.ritualRunner.Run(ctx, exec); err != nil {
					rg.logger.Warn("event-triggered ritual failed",
						"ritual", r.Name, "error", err)
				}
			}(ritual)
		}
	}
}

// Subscribe registers a handler for an event type.
func (rg *RitualGuard) Subscribe(eventType storage.ShogunateEvent, handler EventHandler) {
	rg.eventRegistry.Subscribe(eventType, handler)
}

// --- Ritual management ---

// LoadRituals loads embedded rituals and project rituals from .agents/rituals/
func (rg *RitualGuard) LoadRituals() error {
	embedded, err := LoadEmbeddedRituals()
	if err != nil {
		return fmt.Errorf("failed to load embedded rituals: %w", err)
	}

	for _, ritual := range embedded {
		if err := rg.ritualRegistry.Register(ritual); err != nil {
			rg.logger.Warn("failed to register embedded ritual",
				"ritual", ritual.Name,
				"error", err)
			continue
		}
		rg.logger.Debug("loaded embedded ritual", "name", ritual.Name)
	}

	projectRituals, err := LoadRitualsFromDir(".agents/rituals")
	if err != nil {
		rg.logger.Warn("failed to load project rituals", "error", err)
		return nil
	}

	for _, ritual := range projectRituals {
		if err := rg.ritualRegistry.Register(ritual); err != nil {
			rg.logger.Warn("failed to register project ritual",
				"ritual", ritual.Name,
				"error", err)
			continue
		}
		rg.logger.Debug("loaded project ritual", "name", ritual.Name)
	}

	return nil
}

// RitualRegistry returns the ritual registry.
func (rg *RitualGuard) RitualRegistry() *RitualRegistry {
	return rg.ritualRegistry
}

// RitualRunner returns the ritual runner.
func (rg *RitualGuard) RitualRunner() *RitualRunner {
	return rg.ritualRunner
}

// EventRegistry returns the event registry.
func (rg *RitualGuard) EventRegistry() *EventRegistry {
	return rg.eventRegistry
}

// SetNotify sets the notification callback.
func (rg *RitualGuard) SetNotify(notify internal.NotifyFunc) {
	rg.notify = notify
}

// DrainUnprocessedEvents replays events persisted to DB but never dispatched (crash recovery).
func (rg *RitualGuard) DrainUnprocessedEvents() []DrainedEvent {
	lastEventID, err := rg.GetLastAcknowledgedEvent()
	if err != nil {
		rg.logger.Warn("drain: failed to get last checkpoint", "error", err)
		return nil
	}

	events, err := rg.GetEventsFrom(lastEventID, 0)
	if err != nil {
		rg.logger.Warn("drain: failed to get unprocessed events", "error", err)
		return nil
	}

	if len(events) == 0 {
		return nil
	}

	rg.logger.Info("draining unprocessed events", "count", len(events), "from_id", lastEventID)
	var drained []DrainedEvent
	for _, event := range events {
		t := event.EventType
		rg.DispatchEvent(Event{
			Type:    t,
			EdictID: event.EdictID,
			Payload: map[string]interface{}(event.Payload),
		})
		drained = append(drained, DrainedEvent{
			EventType: t,
			EdictID:   event.EdictID,
			Payload:   map[string]interface{}(event.Payload),
		})
		if err := rg.SaveCheckpoint(event.ID); err != nil {
			rg.logger.Warn("drain: failed to save checkpoint", "error", err)
		}
	}
	return drained
}

// --- Database Methods ---

// GetEventsFrom retrieves events starting from a given event ID
func (rg *RitualGuard) GetEventsFrom(fromEventID uint, limit int) ([]storage.TianEvent, error) {
	var events []storage.TianEvent
	query := rg.db.Where("id > ?", fromEventID).
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
func (rg *RitualGuard) AcknowledgeEvent(eventID uint) error {
	return nil
}

// GetLastAcknowledgedEvent returns the last processed event ID
func (rg *RitualGuard) GetLastAcknowledgedEvent() (uint, error) {
	return rg.LoadCheckpoint()
}

// SaveCheckpoint persists the last processed event ID for crash recovery
func (rg *RitualGuard) SaveCheckpoint(eventID uint) error {
	result := rg.db.Exec(`
		INSERT OR REPLACE INTO ritual_guard_checkpoint (id, event_id, updated_at)
		VALUES (1, ?, datetime('now'))
	`, eventID)
	if result.Error != nil {
		return fmt.Errorf("failed to save checkpoint: %w", result.Error)
	}
	return nil
}

// LoadCheckpoint retrieves the last processed event ID
func (rg *RitualGuard) LoadCheckpoint() (uint, error) {
	var eventID uint
	err := rg.db.Raw(`SELECT COALESCE(event_id, 0) FROM ritual_guard_checkpoint WHERE id = 1`).
		Scan(&eventID).Error
	if err != nil {
		return 0, nil
	}
	return eventID, nil
}

// MoveToDLQ moves a failed event to the dead letter queue
func (rg *RitualGuard) MoveToDLQ(event storage.TianEvent, errMsg string, retryCount int) error {
	dlqEntry := storage.TianEventDLQ{
		OriginalID:   event.ID,
		EdictID:      event.EdictID,
		EventType:    event.EventType,
		Payload:      event.Payload,
		ErrorMessage: errMsg,
		RetryCount:   retryCount,
	}

	if err := rg.db.Create(&dlqEntry).Error; err != nil {
		return fmt.Errorf("failed to move to DLQ: %w", err)
	}
	return nil
}

// Run consumes events from the event channel and dispatches them.
func (rg *RitualGuard) Run(ctx context.Context) {
	rg.logger.Info("ritual guard started (channel mode)")
	for {
		select {
		case <-ctx.Done():
			rg.logger.Info("ritual guard stopped")
			return
		case event := <-rg.eventCh:
			rg.DispatchEvent(event)
		}
	}
}
