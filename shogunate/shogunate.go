package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
	"gorm.io/gorm"
)

// Event is a dispatched event carrying type, edict, and payload.
type Event struct {
	Type    storage.ShogunateEvent
	EdictID string
	Payload map[string]interface{}
}

// EventHandler handles dispatched events.
type EventHandler func(event Event)

// EventRegistry manages event subscriptions and dispatch.
type EventRegistry struct {
	mu          sync.RWMutex
	subscribers map[storage.ShogunateEvent][]EventHandler
}

// NewEventRegistry creates a new event registry.
func NewEventRegistry() *EventRegistry {
	return &EventRegistry{
		subscribers: make(map[storage.ShogunateEvent][]EventHandler),
	}
}

// Subscribe registers a handler for an event type.
func (r *EventRegistry) Subscribe(eventType storage.ShogunateEvent, handler EventHandler) {
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

// DrainedEvent describes a single event recovered from the DB at startup.
type DrainedEvent struct {
	EventType storage.ShogunateEvent
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

	ministers    map[string]Minister
	ritualGuard *RitualGuard

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
		db:        db,
		logger:    logger,
		config:    cfg,
		runner:    runner,
		ministers: make(map[string]Minister),
	}
	s.ensureDefaults()

	// Create all ministers — each needs its own base (channels/maps are reference types).
	// publish uses a closure so it works even before ritualGuard is assigned.
	newBase := func() *MinisterBase {
		base := NewMinisterBase(db, runner, logger)
		base.publish = func(edictID string, eventType storage.ShogunateEvent, payload storage.JSON) string {
			return s.PublishEvent(edictID, eventType, payload)
		}
		return base
	}

	chancellor := NewChancellor(newBase())
	chancellor.SetShogunate(s)
	s.ministers[chancellor.ID()] = chancellor

	// Wire up the ritual guard — it owns all ritual/event infrastructure
	s.ritualGuard = NewRitualGuard(RitualGuardOpts{
		Base:       newBase(),
		Chancellor: chancellor,
		Runner:     runner,
		GetMinister: s.GetMinister,
		StreamingCtx: func() context.Context {
			return s.streamingCtx
		},
	})

	// Subscribe Chancellor to events via RitualGuard
	s.ritualGuard.Subscribe(storage.EventEdictCreated, func(e Event) {
		select {
		case chancellor.eventChan <- e:
		default:
			s.logger.Warn("chancellor event channel full", "event", e.Type)
		}
	})

	s.ritualGuard.Subscribe(storage.EventRitualCompleted, func(e Event) {
		select {
		case chancellor.eventChan <- e:
		default:
			s.logger.Warn("chancellor event channel full", "event", e.Type)
		}
	})

	s.ritualGuard.Subscribe(storage.EventRitualFailed, func(e Event) {
		select {
		case chancellor.eventChan <- e:
		default:
			s.logger.Warn("chancellor event channel full", "event", e.Type)
		}
	})

	// Handle zhengming_answered events for edict creation from suggestions
	s.ritualGuard.Subscribe(storage.EventZhengmingAnswered, func(e Event) {
		answer, _ := e.Payload["answer"].(string)
		edictID, _ := e.Payload["edict_id"].(string)
		s.logger.Debug("zhengming_answered handler", "answer", answer, "edict_id", edictID, "payload", e.Payload)

		if answer == "Approve edict" {
			// Extract suggestion from the zhengming request
			var req storage.Zhengming
			if err := s.db.First(&req, "request_id = ?", e.Payload["request_id"]).Error; err == nil {
				if len(req.Questions) > 0 {
					suggestion := req.Questions[0].Text
					summary := req.Questions[0].Summary
					// Remove "Evidence:" part if present
					if idx := strings.Index(suggestion, "\n\nEvidence:"); idx != -1 {
						suggestion = suggestion[:idx]
					}
					edict, err := s.CreateEdict("", suggestion)
					if err != nil {
						s.logger.Warn("failed to create edict from zhengming approval", "error", err)
					} else if summary != "" {
						edict.Summary = summary
						s.db.Save(edict)
					}
				}
			}
		} else if edictID == "" && answer != "" {
			// System ritual (e.g., wakeup) — no edict, user chose a path forward
			if edict, err := s.CreateEdict("", answer); err != nil {
				s.logger.Warn("failed to create edict from zhengming answer", "error", err)
			} else {
				s.logger.Info("created edict from zhengming answer", "edict_id", edict.EdictID, "answer", answer)
			}
		}
	})

	s.ministers["strategist"] = NewStrategist(newBase())
	s.ministers["forge"] = NewForge(newBase())
	s.ministers["judge"] = NewJudge(newBase(), nil)
	s.ministers["marshal"] = NewMarshal(newBase(), nil)
	confucius := NewConfucius(newBase(), nil)
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
	s.ritualGuard.SetNotify(notify)
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

	// Load rituals
	if err := s.ritualGuard.LoadRituals(); err != nil {
		s.logger.Warn("failed to load rituals", "error", err)
	}

	for _, minister := range s.Ministers() {
		go minister.Run(s.ctx)
	}

	if s.ritualGuard != nil {
		go s.ritualGuard.Run(s.ctx)
	}

	s.logger.Info("shogunate started",
		"poll_interval", s.config.PollInterval,
		"ministers", s.ministerIDs(),
		"rituals", s.ritualGuard.RitualRegistry().List())

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

// CreateEdict creates a new active edict record in the database and publishes storage.EventEdictCreated.
func (s *Shogunate) CreateEdict(edictID, intent string) (*storage.Edict, error) {
	if edictID == "" {
		edictID = generateEdictID()
	}
	edict := storage.Edict{
		EdictID: edictID,
		Intent:  intent,
		Status:  storage.EdictActive,
	}
	if err := s.db.Create(&edict).Error; err != nil {
		return nil, fmt.Errorf("failed to create edict: %w", err)
	}
	s.PublishEvent(edictID, storage.EventEdictCreated, storage.JSON{"intent": intent})
	return &edict, nil
}

// CreateEdictForTest creates an edict without publishing events (for unit tests).
func CreateEdictForTest(db *gorm.DB, edictID, intent string) (*storage.Edict, error) {
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

// PublishEvent delegates to RitualGuard.
func (s *Shogunate) PublishEvent(edictID string, eventType storage.ShogunateEvent, payload storage.JSON) string {
	if s == nil || s.ritualGuard == nil {
		return edictID
	}
	return s.ritualGuard.PublishEvent(edictID, eventType, payload)
}

// DispatchEvent delegates to RitualGuard.
func (s *Shogunate) DispatchEvent(event Event) {
	if s == nil || s.ritualGuard == nil {
		return
	}
	s.ritualGuard.DispatchEvent(event)
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

// GetRunner returns the shell runner
func (s *Shogunate) GetRunner() runners.Runner {
	if s == nil {
		return nil
	}
	return s.runner
}

// GetRitualRegistry returns the ritual registry
func (s *Shogunate) GetRitualRegistry() *RitualRegistry {
	if s == nil || s.ritualGuard == nil {
		return nil
	}
	return s.ritualGuard.RitualRegistry()
}

// GetRitualRunner returns the ritual runner
func (s *Shogunate) GetRitualRunner() *RitualRunner {
	if s == nil || s.ritualGuard == nil {
		return nil
	}
	return s.ritualGuard.RitualRunner()
}

// GetEventRegistry returns the event registry
func (s *Shogunate) GetEventRegistry() *EventRegistry {
	if s == nil || s.ritualGuard == nil {
		return nil
	}
	return s.ritualGuard.EventRegistry()
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
