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
	"github.com/maximhq/bifrost/core/schemas"
	"gorm.io/gorm"
)

// Event is a dispatched event carrying type, edict key, and payload.
type Event struct {
	Type     storage.ShogunateEvent
	EdictKey storage.EdictKey
	Payload  map[string]interface{}
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

// Dispatch sends an event to all registered handlers asynchronously.
func (r *EventRegistry) Dispatch(event Event) {
	r.mu.RLock()
	handlers := r.subscribers[event.Type]
	r.mu.RUnlock()

	for _, handler := range handlers {
		go handler(event)
	}
}

// DrainedEvent describes a single event recovered from the DB at startup.
type DrainedEvent struct {
	EventType storage.ShogunateEvent
	EdictKey  storage.EdictKey
	Payload   map[string]interface{}
}

// EventsDrainedMsg notifies the TUI that crash-recovery drained events from the DB.
type EventsDrainedMsg struct {
	ChannelID string
	Events    []DrainedEvent
}

// Shogunate coordinates ministers and manages lifecycle.
type Shogunate struct {
	db     *gorm.DB
	logger *slog.Logger
	config *config.ShogunateConfig
	runner runners.Runner

	ministers   map[string]Minister
	ritualGuard *RitualGuard

	notify        internal.NotifyFunc
	drainedEvents []DrainedEvent // events recovered from DB at startup

	ctx    context.Context
	cancel context.CancelFunc

	rulingCtx func() context.Context
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

	// Reserve edict 1 for Court Infrastructure operations
	s.ensureCourtInfrastructureEdict()

	// Create all ministers — each needs its own base (channels/maps are reference types).
	// publish uses a closure so it works even before ritualGuard is assigned.
	newBase := func() *MinisterBase {
		base := NewMinisterBase(db, runner, logger, s.config.Username, s.config.Project)
		base.publish = func(key storage.EdictKey, eventType storage.ShogunateEvent, payload storage.JSON) uint {
			return s.PublishEvent(key, eventType, payload)
		}
		return base
	}

	chancellor := NewChancellor(newBase())
	chancellor.SetShogunate(s)
	s.ministers[chancellor.ID()] = chancellor

	// Wire up the ritual guard — it owns all ritual/event infrastructure
	s.ritualGuard = NewRitualGuard(RitualGuardOpts{
		Base:            newBase(),
		Chancellor:      chancellor,
		Runner:          runner,
		GetMinister:     s.GetMinister,
		OnRunnerUpgrade: s.SetRunner,
		StreamingCtx: func() context.Context {
			if s.rulingCtx != nil {
				return s.rulingCtx()
			}
			return s.ctx
		},
	})

	// Subscribe Chancellor to events via RitualGuard
	chancellor.SubscribeToEvents(s.ritualGuard)

	// Handle zhengming_answered events: merged handler for ritual delivery, edict creation, and legacy path
	s.ritualGuard.Subscribe(storage.EventZhengmingAnswered, func(e Event) {
		requestID, _ := e.Payload["request_id"].(string)
		answer, _ := e.Payload["answer"].(string)
		key := e.EdictKey

		// 1. Try to deliver to a waiting minister (tool or ritual via chancellor)
		if requestID != "" {
			zhAnswer := ZhengmingAnswer{RequestID: requestID, Answer: answer, EdictID: key.ID}
			for id, m := range s.ministers {
				if mb, ok := m.(interface{ DeliverZhengmingAnswer(ZhengmingAnswer) bool }); ok {
					if mb.DeliverZhengmingAnswer(zhAnswer) {
						s.logger.Info("zhengming answer delivered to minister",
							"minister", id, "request_id", requestID, "edict_id", key.ID)
						return
					}
				}
			}
		}

		// 2. Handle "Approve edict" for suggestion-based edict creation
		if answer == "Approve edict" {
			var req storage.Zhengming
			if err := s.db.First(&req, "request_id = ?", requestID).Error; err == nil {
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
			return
		}

		// 2b. Handle rejection — user dismissed the suggestion
		if answer == "Reject" || answer == "Dismiss suggestion" {
			s.logger.Info("zhengming suggestion rejected", "request_id", requestID)
			return
		}

		// 3. Handle system ritual path (e.g., wakeup) — no edict, user chose a path forward
		if key.ID == 0 && answer != "" {
			if edict, err := s.CreateEdict("", answer); err != nil {
				s.logger.Warn("failed to create edict from zhengming answer", "error", err)
			} else {
				s.logger.Info("created edict from zhengming answer", "edict_id", edict.ID, "answer", answer)
			}
			return
		}

		// 4. Forward to chancellor as fallback for legacy path
		s.logger.Info("Default forwarding zhengming answer to chancellot")
		go chancellor.handleZhengmingAnswered(s.ctx, key, e.Payload)
	})

	s.ministers["strategist"] = NewStrategist(newBase())
	s.ministers["forge"] = NewForge(newBase())
	s.ministers["judge"] = NewJudge(newBase(), nil)
	s.ministers["marshal"] = NewMarshal(newBase(), nil)
	sage := NewSage(newBase(), nil)
	sage.shogunate = s
	s.ministers["sage"] = sage

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

// ConfigureModel sets the LLM client for all ministers.
// This should be called once the LLM client is initialized.
func (s *Shogunate) ConfigureModel(client LLMProvider, config *SessionConfig, repoInfo repo.RepoInfo) {
	if s == nil {
		return
	}
	for _, minister := range s.Ministers() {
		if base, ok := minister.(interface {
			SetMinisterConfig(LLMProvider, *SessionConfig, repo.RepoInfo)
		}); ok {
			base.SetMinisterConfig(client, config, repoInfo)
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

// EdictKey returns the current context as an EdictKey with the given edict ID.
func (s *Shogunate) EdictKey(edictID uint) storage.EdictKey {
	return storage.EdictKey{ID: edictID, Username: s.config.Username, Project: s.config.Project}
}

// CourtEdictKey returns the Court Infrastructure edict key (edict 1).
// This is used for system-level operations like startup events.
func (s *Shogunate) CourtEdictKey() storage.EdictKey {
	return storage.EdictKey{ID: 0, Username: s.config.Username, Project: s.config.Project}
}

// nextEdictID returns the next available edict ID (MAX+1).
func (s *Shogunate) nextEdictID() uint {
	var maxID uint
	s.db.Model(&storage.Edict{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID)
	return maxID + 1
}

// CreateEdict creates a new active edict record in the database and publishes storage.EventEdictCreated.
// TODO: Add a "summary" parameter which is already in the Edict
func (s *Shogunate) CreateEdict(issueRef, intent string) (*storage.Edict, error) {
	edict, err := s.createEdict(issueRef, intent)
	if err != nil {
		return nil, err
	}
	s.PublishEvent(edict.Key(), storage.EventEdictCreated, storage.JSON{
		"intent": intent,
		"id":     edict.ID,
	})
	return edict, nil
}

// CreateEdictSilent creates an edict without publishing EventEdictCreated.
// Use this when the caller already knows which ritual to run and will
// dispatch it directly, so routing through the chancellor LLM would be
// redundant and would double-start the ritual.
func (s *Shogunate) CreateEdictSilent(issueRef, intent string) (*storage.Edict, error) {
	return s.createEdict(issueRef, intent)
}

func (s *Shogunate) createEdict(issueRef, intent string) (*storage.Edict, error) {
	edict := storage.Edict{
		ID:       s.nextEdictID(),
		Username: s.config.Username,
		Project:  s.config.Project,
		IssueRef: issueRef,
		Intent:   intent,
	}
	if err := s.db.Create(&edict).Error; err != nil {
		return nil, fmt.Errorf("failed to create edict: %w", err)
	}
	return &edict, nil
}

// CreateEdictForTest creates an edict without publishing events (for unit tests).
func CreateEdictForTest(db *gorm.DB, intent string) (*storage.Edict, error) {
	var maxID uint
	db.Model(&storage.Edict{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID)
	edict := storage.Edict{
		ID:     maxID + 1,
		Intent: intent,
	}
	if err := db.Create(&edict).Error; err != nil {
		return nil, fmt.Errorf("failed to create edict: %w", err)
	}
	return &edict, nil
}

// PublishEvent delegates to RitualGuard.
func (s *Shogunate) PublishEvent(key storage.EdictKey, eventType storage.ShogunateEvent, payload storage.JSON) uint {
	if s == nil || s.ritualGuard == nil {
		return key.ID
	}
	return s.ritualGuard.PublishEvent(key, eventType, payload)
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

// ensureCourtInfrastructureEdict creates edict 1 if it doesn't exist.
// Edict 1 is reserved for Court Infrastructure operations (init, bootstrap, etc.)
func (s *Shogunate) ensureCourtInfrastructureEdict() {
	var edict storage.Edict
	if err := s.db.First(&edict, "id = ? AND username = ? AND project = ?", 1, s.config.Username, s.config.Project).Error; err == nil {
		return
	}

	courtEdict := storage.Edict{
		ID:       1,
		Username: s.config.Username,
		Project:  s.config.Project,
		Intent:   "Court Infrastructure - reserved for system-level operations (init, bootstrap, etc.)",
		Summary:  "Court Infrastructure",
		IssueRef: "court-infra",
	}
	if err := s.db.Create(&courtEdict).Error; err != nil {
		s.logger.Warn("failed to create court infrastructure edict", "error", err)
		return
	}

	s.logger.Info("court infrastructure edict created", "edict_id", 1)
}

// GetRunner returns the shell runner
func (s *Shogunate) GetRunner() runners.Runner {
	if s == nil {
		return nil
	}
	return s.runner
}

// SetRunner updates the shogunate's shell runner (e.g. after sandbox comes up)
// and propagates the change to all ministers so their shell tools run in the container.
func (s *Shogunate) SetRunner(r runners.Runner) {
	if s == nil {
		return
	}
	s.runner = r
	// Set globally so runners package can be used directly
	runners.SetRunner(r)
	// Propagate to all ministers that implement RunnerSetter
	for _, m := range s.ministers {
		if setter, ok := m.(interface{ SetRunner(runners.Runner) }); ok {
			setter.SetRunner(r)
		}
	}
}

// SetRunnerMessageChannel sets the message channel on the runner for approval requests
func (s *Shogunate) SetRunnerMessageChannel(msgChan chan<- runners.Msg) {
	if s == nil || s.runner == nil {
		return
	}
	s.runner.SetMessageChannel(msgChan)
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

// SetRulingCtx sets the function that returns the ruling tab's context.
// Rituals use this context, so cancelling the ruling tab cancels rituals.
func (s *Shogunate) SetRulingCtx(fn func() context.Context) {
	if s == nil {
		return
	}
	s.rulingCtx = fn
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

// RestoreMinisterSession creates a fully-wired session and injects loaded history.
// Routes to chancellor or sage based on tabType.
func (s *Shogunate) RestoreMinisterSession(tabType string, msgs []schemas.ChatMessage) error {
	if s == nil {
		return fmt.Errorf("shogunate not initialized")
	}
	switch tabType {
	case "ruling":
		if ch, ok := s.GetMinister("chancellor").(*Chancellor); ok {
			return ch.RestoreSession(msgs)
		}
		return fmt.Errorf("chancellor not found")
	case "hunting":
		if sage, ok := s.GetMinister("sage").(*Sage); ok {
			return sage.RestoreSession(msgs)
		}
		return fmt.Errorf("sage not found")
	default:
		return fmt.Errorf("unknown tab type: %s", tabType)
	}
}

// ResetRulling resets the rulling session
func (s *Shogunate) ResetRuling() {
	if s == nil {
		return
	}
	if ch, ok := s.GetMinister("chancellor").(*Chancellor); ok {
		ch.ResetSession()
	}
}

// ResetHunting resets the hunting session
func (s *Shogunate) ResetHunting() {
	if s == nil {
		return
	}
	if sage, ok := s.GetMinister("sage").(*Sage); ok {
		sage.ResetSession()
	}
}

// GetSealService returns the seal service
func (s *Shogunate) GetSealService() *storage.SealService {
	if s == nil || s.db == nil {
		return nil
	}
	return storage.NewSealService(s.db)
}
