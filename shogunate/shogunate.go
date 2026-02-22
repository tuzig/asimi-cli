package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
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

// RitualGuardRunner runs scheduled ritual guard cycles.
type RitualGuardRunner interface {
	Run(ctx context.Context) error
}

// Shogunate coordinates ministers and manages lifecycle.
type Shogunate struct {
	db     *gorm.DB
	logger *slog.Logger
	config *config.ShogunateConfig
	runner runners.Runner

	ministers      map[string]Minister
	ritualGuard    RitualGuardRunner
	ritualRegistry *RitualRegistry
	ritualRunner   *RitualRunner
	eventRegistry  *EventRegistry

	ctx    context.Context
	cancel context.CancelFunc
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
	}
	s.ensureDefaults()

	// Create ritual runner with shell runner for cmd steps
	s.ritualRunner = NewRitualRunner(s.ritualRegistry, s, db, runner, s.logger)

	// Create all ministers — each needs its own base (channels/maps are reference types)
	newBase := func() MinisterBase {
		return NewMinisterBase(db, runner, logger)
	}

	chancellor := NewChancellor(newBase())
	chancellor.SetShogunate(s)
	s.ministers[chancellor.ID()] = chancellor

	s.ministers["strategist"] = NewStrategist(newBase())
	s.ministers["forge"] = NewForge(newBase())
	s.ministers["judge"] = NewJudge(newBase(), nil)
	s.ministers["censor"] = NewCensor(newBase(), nil)
	s.ministers["marshal"] = NewMarshal(newBase(), nil)
	s.ministers["confucius"] = NewConfucius(newBase())

	return s
}

// SetNotify sets the notification callback for all ministers.
func (s *Shogunate) SetNotify(notify internal.NotifyFunc) {
	if s == nil {
		return
	}
	for _, minister := range s.Ministers() {
		if base, ok := minister.(interface{ SetNotify(internal.NotifyFunc) }); ok {
			base.SetNotify(notify)
		}
	}
}

// SetRitualGuard sets the ritual guard runner.
func (s *Shogunate) SetRitualGuard(rg RitualGuardRunner) {
	if s == nil {
		return
	}
	s.ritualGuard = rg
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

	// Load rituals from .agents/rituals/
	if err := s.loadRituals(); err != nil {
		s.logger.Warn("failed to load rituals", "error", err)
	}

	for _, minister := range s.Ministers() {
		go minister.Run(s.ctx)
	}

	if s.ritualGuard != nil {
		go s.runRitualGuard()
	}

	s.logger.Info("shogunate started",
		"poll_interval", s.config.PollInterval,
		"ministers", s.ministerIDs(),
		"rituals", s.ritualRegistry.List())

	// Invoke wakeup ritual if registered
	if s.ritualRunner != nil && s.ritualRegistry.Get("wakeup") != nil {
		go func() {
			inputs := map[string]string{}
			exec, err := s.ritualRunner.Start(s.ctx, "wakeup", "", inputs, nil)
			if err != nil {
				s.logger.Warn("failed to start wakeup ritual", "error", err)
				return
			}
			if err := s.ritualRunner.Run(s.ctx, exec); err != nil {
				s.logger.Warn("wakeup ritual failed", "error", err)
			}
		}()
	}

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

func (s *Shogunate) runRitualGuard() {
	s.ensureDefaults()
	if s.ritualGuard == nil {
		return
	}

	interval := s.config.PollInterval
	if interval <= 0 {
		s.logger.Info("ritual guard polling disabled")
		return
	}

	timeout := s.config.RitualTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.logger.Info("ritual guard started", "poll_interval", interval)

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("ritual guard stopped")
			return
		case <-ticker.C:
			runCtx, runCancel := context.WithTimeout(s.ctx, timeout)
			if err := s.ritualGuard.Run(runCtx); err != nil {
				s.logger.Error("ritual guard cycle failed", "error", err)
			}
			runCancel()
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
	// Load embedded rituals first (swift-strike, grand-campaign)
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
		for _, ritual := range rituals {
			edictID := event.EdictID
			inputs := map[string]string{"edict_id": edictID}
			go func(r *RitualDef) {
				exec, err := s.ritualRunner.Start(context.Background(), r.Name, edictID, inputs, nil)
				if err != nil {
					s.logger.Warn("failed to start event-triggered ritual",
						"ritual", r.Name, "event", event.Type, "error", err)
					return
				}
				if err := s.ritualRunner.Run(context.Background(), exec); err != nil {
					s.logger.Warn("event-triggered ritual failed",
						"ritual", r.Name, "error", err)
				}
			}(ritual)
		}
	}
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
