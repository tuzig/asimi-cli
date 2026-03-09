package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
	"gorm.io/gorm"
)

// ShogunateEvent represents an event type emitted by the Shogunate lifecycle.
type ShogunateEvent string

// Event is a dispatched event carrying type, edict, and payload.
type Event struct {
	Type    ShogunateEvent
	EdictID string
	Payload map[string]interface{}
}

// EventHandler handles dispatched events.
type EventHandler func(event Event)

// EventRegistry manages event subscriptions and dispatch.
type EventRegistry struct {
	mu          sync.RWMutex
	subscribers map[ShogunateEvent][]EventHandler
}

// NewEventRegistry creates a new event registry.
func NewEventRegistry() *EventRegistry {
	return &EventRegistry{
		subscribers: make(map[ShogunateEvent][]EventHandler),
	}
}

// Subscribe registers a handler for an event type.
func (r *EventRegistry) Subscribe(eventType ShogunateEvent, handler EventHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subscribers[eventType] = append(r.subscribers[eventType], handler)
}

// Dispatch sends an event to all registered handlers.
func (r *EventRegistry) Dispatch(event Event) {
	r.mu.RLock()
	handlers := r.subscribers[event.Type]
	r.mu.RUnlock()

	for _, handler := range handlers {
		handler(event)
	}
}

const (
	EventEdictAssigned     ShogunateEvent = "edict_assigned"
	EventEdictCreated      ShogunateEvent = "edict_created"
	EventPhaseChanged      ShogunateEvent = "phase_changed"
	EventForgeCommitted    ShogunateEvent = "forge_committed"
	EventManifestCommitted ShogunateEvent = "manifest_committed"
	EventManifestRejected  ShogunateEvent = "manifest_rejected"
	EventRitualStarted     ShogunateEvent = "ritual_started"
	EventRitualCompleted   ShogunateEvent = "ritual_completed"
	EventRitualFailed      ShogunateEvent = "ritual_failed"
	EventStepStarted       ShogunateEvent = "step_started"
	EventStepCompleted     ShogunateEvent = "step_completed"
	EventStepFailed        ShogunateEvent = "step_failed"
	EventLingCreated       ShogunateEvent = "ling_created"
	EventZhengmingNeeded   ShogunateEvent = "zhengming_needed"
	EventZhengmingAnswered ShogunateEvent = "zhengming_answered"
	EventEdictCancelled    ShogunateEvent = "edict_cancelled"
)

// DrainedEvent describes a single event recovered from the DB at startup.
type DrainedEvent struct {
	EventType string
	EdictID   string
	Payload   map[string]interface{}
}

// EventsDrainedMsg notifies the TUI that crash-recovery drained events from the DB.
type EventsDrainedMsg struct {
	TabID  string
	Events []DrainedEvent
}

// Shogunate coordinates ministers and manages lifecycle.
type Shogunate struct {
	db     *gorm.DB
	logger *slog.Logger
	config *config.ShogunateConfig
	runner runners.Runner

	ministers      map[string]Minister
	ritualGuard    *RitualGuard
	ritualRegistry *RitualRegistry
	ritualRunner   *RitualRunner
	eventRegistry  *EventRegistry
	eventCh        chan Event

	notify        internal.NotifyFunc
	drainedEvents []DrainedEvent // events recovered from DB at startup

	ctx    context.Context
	cancel context.CancelFunc

	streamingCtx    context.Context
	streamingCancel context.CancelFunc
}

// NewShogunate creates a new Shogunate coordinator.
func NewShogunate(db *gorm.DB, cfg *config.ShogunateConfig, runner runners.Runner, logger *slog.Logger) *Shogunate {
	s := &Shogunate{
		db:             db,
		logger:         logger,
		config:         cfg,
		runner:         runner,
		ministers:      make(map[string]Minister),
		ritualRegistry: NewRitualRegistry(),
		eventRegistry:  NewEventRegistry(),
		eventCh:        make(chan Event, 256),
	}
	s.ensureDefaults()

	// Create ritual runner with shell runner for cmd steps
	s.ritualRunner = NewRitualRunner(s.ritualRegistry, s, db, runner, s.logger)

	// Create all ministers — each needs its own base (channels/maps are reference types)
	newBase := func() *MinisterBase {
		base := NewMinisterBase(db, runner, logger)
		base.publish = s.PublishEvent
		return base
	}

	chancellor := NewChancellor(newBase())
	chancellor.SetShogunate(s)
	s.ministers[chancellor.ID()] = chancellor

	// Wire up the ritual guard
	s.ritualGuard = NewRitualGuard(newBase(), chancellor, s)

	s.ministers["strategist"] = NewStrategist(newBase())
	s.ministers["forge"] = NewForge(newBase())
	s.ministers["judge"] = NewJudge(newBase(), nil)
	s.ministers["censor"] = NewCensor(newBase(), nil)
	s.ministers["marshal"] = NewMarshal(newBase(), nil)
	confucius := NewConfucius(newBase())
	confucius.shogunate = s
	s.ministers["confucius"] = confucius

	return s
}

// SetNotify sets the notification callback for all ministers.
func (s *Shogunate) SetNotify(notify internal.NotifyFunc) {
	if s == nil {
		return
	}
	s.notify = notify
	for _, minister := range s.Ministers() {
		if base, ok := minister.(interface{ SetNotify(internal.NotifyFunc) }); ok {
			base.SetNotify(notify)
		}
	}
	// Send deferred drain notification if events were recovered at startup.
	// Use goroutine because notify may call program.Send before program.Run.
	if len(s.drainedEvents) > 0 {
		events := s.drainedEvents
		go func() { notify(EventsDrainedMsg{Events: events}) }()
	}
}

// Start initializes the Shogunate lifecycle.
func (s *Shogunate) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.ensureDefaults()
	if ctx == nil {
		ctx = context.Background()
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.streamingCtx, s.streamingCancel = context.WithCancel(s.ctx)

	// Load rituals from .agents/rituals/
	if err := s.loadRituals(); err != nil {
		s.logger.Warn("failed to load rituals", "error", err)
	}

	for _, minister := range s.Ministers() {
		go minister.Run(s.ctx)
	}

	// Drain unprocessed DB events synchronously before starting the channel consumer.
	// Running on the main goroutine avoids opening a second SQLite connection
	// for in-memory databases where each connection is a separate database.
	// s.drainUnprocessedEvents()

	if s.ritualGuard != nil {
		go s.ritualGuard.Run(s.ctx)
	}

	s.logger.Info("shogunate started",
		"poll_interval", s.config.PollInterval,
		"ministers", s.ministerIDs(),
		"rituals", s.ritualRegistry.List())

	return nil
}

// Stop gracefully shuts down the Shogunate.
func (s *Shogunate) Stop() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.logger.Info("shogunate stopped")
	return nil
}

// GetMinister returns a minister by ID.
func (s *Shogunate) GetMinister(id string) Minister {
	if s == nil || id == "" {
		return nil
	}
	for _, minister := range s.Ministers() {
		if minister.ID() == id {
			return minister
		}
	}
	return nil
}

// ConfigureModel sets the LLM model for all ministers.
// This should be called once the LLM client is initialized.
func (s *Shogunate) ConfigureModel(model llms.Model, config *SessionConfig, repoInfo repo.RepoInfo) {
	if s == nil {
		return
	}
	for _, minister := range s.Ministers() {
		if base, ok := minister.(interface {
			SetMinisterConfig(llms.Model, *SessionConfig, repo.RepoInfo)
		}); ok {
			base.SetMinisterConfig(model, config, repoInfo)
		}
	}
	s.logger.Info("shogunate model configured", "ministers", s.ministerIDs())
}

// Ministers returns the active ministers.
func (s *Shogunate) Ministers() []Minister {
	if s == nil || s.ministers == nil {
		return nil
	}

	active := make([]Minister, 0, len(s.ministers))
	for _, minister := range s.ministers {
		if minister != nil {
			active = append(active, minister)
		}
	}
	return active
}

// CreateEdict creates a new active edict record in the database.
func CreateEdict(db *gorm.DB, edictID, intent string) (*storage.Edict, error) {
	if edictID == "" {
		edictID = generateEdictID()
	}
	edict := storage.Edict{
		EdictID: edictID,
		Intent:  intent,
		Status:  storage.EdictActive,
	}
	if err := db.Create(&edict).Error; err != nil {
		return nil, fmt.Errorf("failed to create edict: %w", err)
	}
	return &edict, nil
}

// PublishEvent persists an event to the DB and sends it to the event channel.
func (s *Shogunate) PublishEvent(edictID, eventType string, payload storage.JSON) string {
	if s.db != nil {
		dbEvent := storage.TianEvent{
			EdictID:   edictID,
			EventType: eventType,
			Payload:   payload,
		}
		if err := s.db.Create(&dbEvent).Error; err != nil {
			s.logger.Error("failed to persist event", "type", eventType, "error", err)
		}
	}
	select {
	case s.eventCh <- Event{Type: ShogunateEvent(eventType), EdictID: edictID, Payload: map[string]interface{}(payload)}:
	default:
		s.logger.Warn("event channel full, persisted to DB only", "type", eventType)
	}
	return edictID
}

// drainUnprocessedEvents replays events persisted to DB but never dispatched (crash recovery).
func (s *Shogunate) drainUnprocessedEvents() {
	if s.ritualGuard == nil {
		return
	}

	lastEventID, err := s.ritualGuard.GetLastAcknowledgedEvent()
	if err != nil {
		s.logger.Warn("drain: failed to get last checkpoint", "error", err)
		return
	}

	events, err := s.ritualGuard.GetEventsFrom(lastEventID, 0)
	if err != nil {
		s.logger.Warn("drain: failed to get unprocessed events", "error", err)
		return
	}

	if len(events) == 0 {
		return
	}

	s.logger.Info("draining unprocessed events", "count", len(events), "from_id", lastEventID)
	for _, event := range events {
		s.DispatchEvent(Event{
			Type:    ShogunateEvent(event.EventType),
			EdictID: event.EdictID,
			Payload: map[string]interface{}(event.Payload),
		})
		s.drainedEvents = append(s.drainedEvents, DrainedEvent{
			EventType: event.EventType,
			EdictID:   event.EdictID,
			Payload:   map[string]interface{}(event.Payload),
		})
		if err := s.ritualGuard.SaveCheckpoint(event.ID); err != nil {
			s.logger.Warn("drain: failed to save checkpoint", "error", err)
		}
	}
}

func (s *Shogunate) ensureDefaults() {
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if s.config == nil {
		s.config = config.DefaultShogunateConfig()
	}
}

func (s *Shogunate) ministerIDs() []string {
	ministers := s.Ministers()
	ids := make([]string, 0, len(ministers))
	for _, minister := range ministers {
		ids = append(ids, minister.ID())
	}
	return ids
}

// loadRituals loads embedded rituals and project rituals from .agents/rituals/
func (s *Shogunate) loadRituals() error {
	// Load embedded rituals first
	embedded, err := LoadEmbeddedRituals()
	if err != nil {
		return fmt.Errorf("failed to load embedded rituals: %w", err)
	}

	for _, ritual := range embedded {
		if err := s.ritualRegistry.Register(ritual); err != nil {
			s.logger.Warn("failed to register embedded ritual",
				"ritual", ritual.Name,
				"error", err)
			continue
		}
		s.logger.Debug("loaded embedded ritual", "name", ritual.Name)
	}

	// Load project-specific rituals (can override embedded)
	projectRituals, err := LoadRitualsFromDir(".agents/rituals")
	if err != nil {
		s.logger.Warn("failed to load project rituals", "error", err)
		return nil // Don't fail - embedded rituals are still available
	}

	for _, ritual := range projectRituals {
		if err := s.ritualRegistry.Register(ritual); err != nil {
			s.logger.Warn("failed to register project ritual",
				"ritual", ritual.Name,
				"error", err)
			continue
		}
		s.logger.Debug("loaded project ritual", "name", ritual.Name)
	}

	return nil
}

// GetRunner returns the shell runner
func (s *Shogunate) GetRunner() runners.Runner {
	if s == nil {
		return nil
	}
	return s.runner
}

// GetRitualRegistry returns the ritual registry
func (s *Shogunate) GetRitualRegistry() *RitualRegistry {
	if s == nil {
		return nil
	}
	return s.ritualRegistry
}

// GetRitualRunner returns the ritual runner
func (s *Shogunate) GetRitualRunner() *RitualRunner {
	if s == nil {
		return nil
	}
	return s.ritualRunner
}

// GetEventRegistry returns the event registry
func (s *Shogunate) GetEventRegistry() *EventRegistry {
	if s == nil {
		return nil
	}
	return s.eventRegistry
}

// DispatchEvent dispatches an event to all subscribers and triggers event-driven rituals.
func (s *Shogunate) DispatchEvent(event Event) {
	if s == nil || s.eventRegistry == nil {
		return
	}
	s.eventRegistry.Dispatch(event)

	// Trigger event-driven rituals
	if s.ritualRegistry != nil && s.ritualRunner != nil {
		rituals := s.ritualRegistry.GetByEvent(string(event.Type))
		// Extract source ritual name from payload to prevent cascading loops
		// (e.g. report_failure failing should not trigger report_failure again)
		sourceRitual, _ := event.Payload["ritual"].(string)
		for _, ritual := range rituals {
			if ritual.Name == sourceRitual {
				s.logger.Debug("skipping self-triggered ritual",
					"ritual", ritual.Name, "event", event.Type)
				continue
			}
			edictID := event.EdictID
			inputs := map[string]string{"edict_id": edictID}
			go func(r *RitualDef) {
				exec, err := s.ritualRunner.Start(s.streamingCtx, r.Name, edictID, inputs, s.notify)
				if err != nil {
					s.logger.Warn("failed to start event-triggered ritual",
						"ritual", r.Name, "event", event.Type, "error", err)
					return
				}
				if err := s.ritualRunner.Run(s.streamingCtx, exec); err != nil {
					s.logger.Warn("event-triggered ritual failed",
						"ritual", r.Name, "error", err)
				}
			}(ritual)
		}
	}
}

// Interrupt cancels the streaming context, stopping event-triggered rituals,
// then creates a fresh streaming context for subsequent work.
func (s *Shogunate) Interrupt() {
	if s == nil || s.streamingCancel == nil {
		return
	}
	s.streamingCancel()
	s.streamingCtx, s.streamingCancel = context.WithCancel(s.ctx)
}

// SubmitPrompt routes a prompt to the specified minister by ID.
func (s *Shogunate) SubmitPrompt(targetID string, p *Prompt) error {
	if s == nil {
		return fmt.Errorf("shogunate not initialized")
	}
	minister := s.GetMinister(targetID)
	if minister == nil {
		return fmt.Errorf("minister not found: %s", targetID)
	}
	go minister.SubmitPrompt(p)
	return nil
}

// GetCurrentSession returns the session for the specified edict ID.
// If edictID is empty, returns nil.
func (s *Shogunate) GetCurrentSession(edictID string) *Session {
	if s == nil || edictID == "" {
		return nil
	}
	chancellor := s.GetMinister("chancellor")
	if chancellor == nil {
		return nil
	}
	ch, ok := chancellor.(*Chancellor)
	if !ok {
		s.logger.Warn("Failed to get the chancellor in Shogunate.GetCurrentSession")
		return nil
	}
	return ch.GetSession(edictID)
}
