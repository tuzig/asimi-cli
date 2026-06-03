package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/types"
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
	EventType storage.ShogunateEvent `msgpack:"event_type"`
	EdictKey  storage.EdictKey       `msgpack:"edict_key"`
	Payload   map[string]interface{} `msgpack:"payload,omitempty"`
}

// EventsDrainedMsg notifies the TUI that crash-recovery drained events from the DB.
type EventsDrainedMsg struct {
	ChannelID string         `msgpack:"channel_id,omitempty"`
	Events    []DrainedEvent `msgpack:"events"`
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

	// persister, if set, is attached to interactive sessions when they
	// are created so messages flow into durable storage in near-real time.
	persister SessionPersister

	ctx    context.Context
	cancel context.CancelFunc

	// Per-channel cancel registry; CancelTab(channelID) invokes the
	// corresponding cancel func. Populated by CancellableStreamCtx.
	tabCancelsMu sync.Mutex
	tabCancels   map[string]context.CancelFunc
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
		// Each ritual startup gets a fresh cancellable ctx registered
		// under the chancellor channel. A subsequent ritual on the
		// same channel replaces it; an explicit CancelTab("chancellor")
		// from the TUI stops the current one.
		StreamingCtx: func() context.Context {
			return s.CancellableStreamCtx("chancellor")
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

// SetSessionPersister wires a persister into the shogunate. Sessions
// created afterwards (and any currently held by interactive ministers)
// receive it; sessions without a TabType are silently skipped at save
// time. Idempotent — safe to call again with a different persister.
func (s *Shogunate) SetSessionPersister(p SessionPersister) {
	if s == nil {
		return
	}
	s.persister = p
	for _, minister := range s.Ministers() {
		if setter, ok := minister.(interface{ SetSessionPersister(SessionPersister) }); ok {
			setter.SetSessionPersister(p)
		}
	}
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

// SetRepoInfo sets the repo info on the shogunate and all ministers.
// This must be called before Start() to ensure rituals load from the
// correct project root. ConfigureModel will also set repoInfo, but
// that happens after Start() in the normal flow.
func (s *Shogunate) SetRepoInfo(repoInfo repo.RepoInfo) {
	if s == nil {
		return
	}
	for _, minister := range s.Ministers() {
		if base, ok := minister.(interface{ SetRepoInfo(repo.RepoInfo) }); ok {
			base.SetRepoInfo(repoInfo)
		}
	}
	if s.ritualGuard != nil {
		s.ritualGuard.repoInfo = repoInfo
	}
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
	// Propagate sandbox config, project slug, and repoInfo to the
	// RitualRunner so it never needs to reload config from disk.
	// repoInfo is especially important because the runner is created
	// with an empty ProjectRoot at daemon
	// startup and only receives the real root via SetContext/ConfigureModel.
	if s.ritualGuard != nil && s.ritualGuard.RitualRunner() != nil {
		sandboxCfg := &config.Sandbox
		projectSlug := ""
		if s.config != nil {
			projectSlug = s.config.Project
		}
		s.ritualGuard.RitualRunner().SetConfig(sandboxCfg, projectSlug, repoInfo)
	}
	// Update repoInfo on the RitualGuard (not covered by the Ministers
	// loop since RitualGuard is stored separately) and reload rituals
	// when the project root becomes available for the first time.
	// Start() skips loading if repoInfo.ProjectRoot was empty, so we
	// retry here once SetContext/ConfigureModel has the real root.
	// We only reload when the registry is still empty (i.e., no one
	// has manually registered rituals since Start).
	if s.ritualGuard != nil && repoInfo.ProjectRoot != "" {
		wasEmpty := s.ritualGuard.repoInfo.ProjectRoot == ""
		s.ritualGuard.repoInfo = repoInfo
		if wasEmpty && len(s.ritualGuard.RitualRegistry().List()) == 0 {
			if err := s.ritualGuard.LoadRituals(); err != nil {
				s.logger.Warn("failed to load rituals after ConfigureModel", "error", err)
			}
		}
	}
	s.logger.Info("shogunate model configured", "ministers", s.ministerIDs())
}

// SetContext reconfigures the Shogunate with the given credentials and
// project context. In single-process mode this initialises Bifrost
// inline using the APIKeys from the params. It is idempotent — each
// call re-initialises the LLM client.
func (s *Shogunate) SetContext(ctx context.Context, params types.SetContextParams) error {
	if s == nil {
		return fmt.Errorf("shogunate not initialised")
	}

	// Load project config from ProjectRoot.
	projectRoot := params.ProjectRoot
	if projectRoot == "" {
		projectRoot = "."
	}
	// Validate that the path exists and is a directory before loading config.
	if info, err := os.Stat(projectRoot); err != nil {
		return fmt.Errorf("invalid project_root %q: %w", projectRoot, err)
	} else if !info.IsDir() {
		return fmt.Errorf("invalid project_root %q: not a directory", projectRoot)
	}

	projectCfg, err := config.LoadProjectConfig(projectRoot, false)
	if err != nil {
		return fmt.Errorf("load project config: %w", err)
	}

	repoInfo := repo.RepoInfo{
		ProjectRoot:  params.ProjectRoot,
		WorktreePath: params.WorktreePath,
		Branch:       params.Branch,
		Slug:         params.Project,
	}

	// Init Bifrost with the APIKeys provided by the client.
	bifrostClient, err := InitBifrost(
		ctx,
		projectCfg.LLM.RequestTimeoutSeconds,
		projectCfg.LLM.StreamIdleTimeoutSeconds,
		projectCfg.LLM.MaxRetries,
		projectCfg.LLM.BaseURL,
		params.APIKeys,
	)
	if err != nil {
		return fmt.Errorf("init bifrost: %w", err)
	}

	sessionCfg := &SessionConfig{
		LLM:        projectCfg.LLM,
		Sandbox:    projectCfg.Sandbox,
		AgentsFile: projectCfg.Session.AgentsFile,
		WorkingDir: params.ProjectRoot,
	}

	s.ConfigureModel(bifrostClient, sessionCfg, repoInfo)
	return nil
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
// and propagates it to all ministers so ephemeral HostRunner instances can use it too.
func (s *Shogunate) SetRunnerMessageChannel(msgChan chan<- runners.Msg) {
	if s == nil || s.runner == nil {
		return
	}
	s.runner.SetMessageChannel(msgChan)
	// Propagate to all ministers so their shell tools can pass msgChan
	// to ephemeral HostRunner instances
	for _, m := range s.ministers {
		if setter, ok := m.(interface{ SetMessageChannel(chan<- runners.Msg) }); ok {
			setter.SetMessageChannel(msgChan)
		}
	}
}

// Subscribe returns a channel carrying every TUI-bound notification produced
// by the shogunate: streaming chunks, events, and runner messages. It installs
// the underlying SetNotify callback and the runner message channel on the
// caller's behalf, so callers no longer touch either directly. The returned
// channel stays open for the Shogunate's lifetime; the caller drains it until
// ctx is cancelled.
func (s *Shogunate) Subscribe(ctx context.Context) <-chan any {
	if s == nil {
		closed := make(chan any)
		close(closed)
		return closed
	}
	out := make(chan any, 256)

	s.SetNotify(func(msg any) {
		select {
		case out <- msg:
		case <-ctx.Done():
		}
	})

	if s.runner != nil {
		runnerMsgChan := make(chan runners.Msg, 10)
		s.SetRunnerMessageChannel(runnerMsgChan)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-runnerMsgChan:
					if !ok {
						return
					}
					select {
					case out <- msg:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	return out
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

// CancellableStreamCtx returns a context derived from the Shogunate's
// root ctx and registers its cancel func under channelID. Any previous
// cancel for the same channelID is invoked first, so callers don't
// leak. Use CancelTab(channelID) to trigger cancellation externally.
func (s *Shogunate) CancellableStreamCtx(channelID string) context.Context {
	if s == nil {
		return context.Background()
	}
	s.tabCancelsMu.Lock()
	defer s.tabCancelsMu.Unlock()
	if s.tabCancels == nil {
		s.tabCancels = make(map[string]context.CancelFunc)
	}
	if prev, ok := s.tabCancels[channelID]; ok {
		prev()
	}
	parent := s.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	s.tabCancels[channelID] = cancel
	return ctx
}

// CancelTab invokes (and forgets) the cancel func registered under
// channelID. Idempotent; calling with an unknown channelID is a no-op.
func (s *Shogunate) CancelTab(channelID string) {
	if s == nil {
		return
	}
	s.tabCancelsMu.Lock()
	cancel, ok := s.tabCancels[channelID]
	if ok {
		delete(s.tabCancels, channelID)
	}
	s.tabCancelsMu.Unlock()
	if ok {
		cancel()
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

// RestoreMinisterSession rebuilds the session of the minister identified
// by tabType (matches the saved Session.TabType, which is the minister id)
// and seeds it with msgs. Works for any minister that implements
// RestoreSession — currently chancellor, sage, forge, judge.
func (s *Shogunate) RestoreMinisterSession(tabType string, msgs []schemas.ChatMessage) error {
	if s == nil {
		return fmt.Errorf("shogunate not initialized")
	}
	minister := s.GetMinister(tabType)
	if minister == nil {
		return fmt.Errorf("minister not found: %s", tabType)
	}
	return minister.RestoreSession(minister, msgs)
}

// ResetChancellor resets the chancellor session
func (s *Shogunate) ResetChancellor() {
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

// HasMinister reports whether a minister with the given id is registered.
func (s *Shogunate) HasMinister(id string) bool {
	return s != nil && s.GetMinister(id) != nil
}

// ResetMinisterSession resets the session of the minister with the given id.
// No-op if the minister doesn't exist or doesn't expose ResetSession.
func (s *Shogunate) ResetMinisterSession(id string) {
	if s == nil {
		return
	}
	m := s.GetMinister(id)
	if m == nil {
		return
	}
	if rs, ok := m.(interface{ ResetSession() }); ok {
		rs.ResetSession()
	}
}

// GetEdict looks up an edict in the current shogunate scope.
func (s *Shogunate) GetEdict(edictID uint) (*storage.Edict, error) {
	if s == nil {
		return nil, fmt.Errorf("shogunate not initialized")
	}
	ch, ok := s.GetMinister("chancellor").(*Chancellor)
	if !ok {
		return nil, fmt.Errorf("chancellor not found")
	}
	return ch.GetEdict(s.EdictKey(edictID))
}

// GrantRulerSeal stamps the Ruler's seal on an edict and publishes EventSealGranted.
func (s *Shogunate) GrantRulerSeal(edictID uint, notes string) error {
	if s == nil {
		return fmt.Errorf("shogunate not initialized")
	}
	sealer := s.GetSealService()
	if sealer == nil {
		return fmt.Errorf("seal service not available")
	}
	key := s.EdictKey(edictID)
	timestamp := time.Now().Format(time.RFC3339)
	if err := sealer.GrantSeal(key, "ruler", storage.JSON{
		"notes":     notes,
		"timestamp": timestamp,
	}); err != nil {
		return err
	}
	s.PublishEvent(key, storage.EventSealGranted, storage.JSON{
		"minister_id": "ruler",
		"notes":       notes,
		"timestamp":   timestamp,
	})
	return nil
}

// GetEdictSeals returns the seal chain for an edict.
func (s *Shogunate) GetEdictSeals(key storage.EdictKey) ([]storage.Seal, error) {
	if s == nil {
		return nil, fmt.Errorf("shogunate not initialized")
	}
	sealer := s.GetSealService()
	if sealer == nil {
		return nil, fmt.Errorf("seal service not available")
	}
	return sealer.GetSeals(key)
}

// ListActiveEdicts returns edicts in the current scope that aren't cancelled or sealed.
func (s *Shogunate) ListActiveEdicts() ([]storage.ActiveEdict, error) {
	if s == nil {
		return nil, fmt.Errorf("shogunate not initialized")
	}
	sealer := s.GetSealService()
	if sealer == nil {
		return nil, fmt.Errorf("seal service not available")
	}
	return sealer.ListActiveEdicts(s.config.Username, s.config.Project)
}

// AllowRunnerFallback toggles the host-fallback behaviour on the active runner.
func (s *Shogunate) AllowRunnerFallback(allow bool) {
	if s == nil || s.runner == nil {
		return
	}
	s.runner.AllowFallback(allow)
}

// RunShellCommand executes a shell command on the active runner.
func (s *Shogunate) RunShellCommand(ctx context.Context, input runners.Input) (runners.Output, error) {
	if s == nil || s.runner == nil {
		return runners.Output{}, fmt.Errorf("runner not initialized")
	}
	return s.runner.Run(ctx, input)
}

// HandleZhengmingResponse dispatches a user's zhengming answer to the first
// minister that knows how to handle one.
func (s *Shogunate) HandleZhengmingResponse(ctx context.Context, requestID, answer string) error {
	if s == nil {
		return fmt.Errorf("shogunate not initialized")
	}
	type zhengmingHandler interface {
		HandleZhengmingResponse(ctx context.Context, requestID, answer string) error
	}
	for _, m := range s.Ministers() {
		if h, ok := m.(zhengmingHandler); ok {
			return h.HandleZhengmingResponse(ctx, requestID, answer)
		}
	}
	return fmt.Errorf("no minister accepted zhengming response")
}

// CancelZhengming cancels a pending zhengming request on whichever minister owns it.
func (s *Shogunate) CancelZhengming(requestID string) {
	if s == nil {
		return
	}
	for _, m := range s.Ministers() {
		if base, ok := m.(interface{ CancelZhengming(string) }); ok {
			base.CancelZhengming(requestID)
			return
		}
	}
}

// SessionState is a wire-safe snapshot of a minister's conversation state,
// aggregated in one call for cheap TUI-side access. Exists=false means no
// session exists for that tab.
type SessionState struct {
	Exists              bool
	ChannelID           string
	MessageCount        int
	MessageSnapshot     int
	ContextInfo         ContextInfo
	ContextUsagePercent float64
	ContextFiles        map[string]string
}

func (s *Shogunate) sessionForTab(tabTarget string) *Session {
	if s == nil {
		return nil
	}
	m := s.GetMinister(tabTarget)
	if m == nil {
		return nil
	}
	return m.GetSession()
}

// SessionState returns a snapshot of the session attached to the given tab.
func (s *Shogunate) SessionState(tabTarget string) SessionState {
	sess := s.sessionForTab(tabTarget)
	if sess == nil {
		return SessionState{}
	}
	return SessionState{
		Exists:              true,
		ChannelID:           sess.ChannelID(),
		MessageCount:        len(sess.GetMessages()),
		MessageSnapshot:     sess.GetMessageSnapshot(),
		ContextInfo:         sess.GetContextInfo(),
		ContextUsagePercent: sess.GetContextUsagePercent(),
		ContextFiles:        sess.GetContextFiles(),
	}
}

// AddSessionContextFile attaches a file's contents to the given tab's session.
func (s *Shogunate) AddSessionContextFile(tabTarget, path, content string) error {
	sess := s.sessionForTab(tabTarget)
	if sess == nil {
		return fmt.Errorf("no session for tab %q", tabTarget)
	}
	sess.AddContextFile(path, content)
	return nil
}

// AddSessionMessage appends a message to the tab's conversation.
func (s *Shogunate) AddSessionMessage(tabTarget, role, content string) error {
	sess := s.sessionForTab(tabTarget)
	if sess == nil {
		return fmt.Errorf("no session for tab %q", tabTarget)
	}
	sess.AddMessage(schemas.ChatMessageRole(role), content)
	return nil
}

// ClearSessionHistory resets the tab's conversation to an empty state.
func (s *Shogunate) ClearSessionHistory(tabTarget string) error {
	sess := s.sessionForTab(tabTarget)
	if sess == nil {
		return fmt.Errorf("no session for tab %q", tabTarget)
	}
	sess.ClearHistory()
	return nil
}

// RollbackSession rewinds the conversation to the given message snapshot.
func (s *Shogunate) RollbackSession(tabTarget string, snapshot int) error {
	sess := s.sessionForTab(tabTarget)
	if sess == nil {
		return fmt.Errorf("no session for tab %q", tabTarget)
	}
	sess.RollbackTo(snapshot)
	return nil
}

// CompactSession runs a summarisation pass and returns the summary, replacing
// older messages in the conversation with it.
func (s *Shogunate) CompactSession(ctx context.Context, tabTarget, prompt string) (string, error) {
	sess := s.sessionForTab(tabTarget)
	if sess == nil {
		return "", fmt.Errorf("no session for tab %q", tabTarget)
	}
	return sess.CompactHistory(ctx, prompt)
}

// SessionExport is a wire-safe copy of everything an exporter needs:
// the session ID, its full message history, and the metadata fields
// FormatMetadata renders. It satisfies the ExportableSession interface
// used by export.go without the TUI ever touching the live *Session.
type SessionExport struct {
	ID           string                `msgpack:"id"`
	Messages     []schemas.ChatMessage `msgpack:"messages"`
	ContextFiles map[string]string     `msgpack:"context_files,omitempty"`
	Provider     string                `msgpack:"provider,omitempty"`
	Model        string                `msgpack:"model,omitempty"`
	WorkingDir   string                `msgpack:"working_dir,omitempty"`
	ProjectSlug  string                `msgpack:"project_slug,omitempty"`
	CreatedAt    time.Time             `msgpack:"created_at"`
	LastUpdated  time.Time             `msgpack:"last_updated"`
}

// GetID satisfies ExportableSession.
func (e *SessionExport) GetID() string { return e.ID }

// GetMessages satisfies ExportableSession.
func (e *SessionExport) GetMessages() []schemas.ChatMessage { return e.Messages }

// GetContextFiles satisfies ExportableSession.
func (e *SessionExport) GetContextFiles() map[string]string { return e.ContextFiles }

// FormatMetadata satisfies ExportableSession; mirrors Session.FormatMetadata
// so exports look identical whether sourced live or over the wire.
func (e *SessionExport) FormatMetadata(exportType, exportedAt string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**Export Type:** %s\n", exportType))
	b.WriteString(fmt.Sprintf("**Session ID:** %s | **Working Directory:** %s\n", e.ID, e.WorkingDir))
	b.WriteString(fmt.Sprintf("**Provider:** %s | **Model:** %s\n", e.Provider, e.Model))
	b.WriteString(fmt.Sprintf("**Created:** %s | **Last Updated:** %s | **Exported:** %s\n",
		e.CreatedAt.Format("2006-01-02 15:04:05"),
		e.LastUpdated.Format("2006-01-02 15:04:05"),
		exportedAt))
	if e.ProjectSlug != "" {
		b.WriteString(fmt.Sprintf("**Project:** %s\n", e.ProjectSlug))
	}
	return b.String()
}

// GetSessionExport returns a wire-safe snapshot of the tab's session
// suitable for feeding to exportSession. Nil if no session exists.
func (s *Shogunate) GetSessionExport(tabTarget string) (*SessionExport, error) {
	sess := s.sessionForTab(tabTarget)
	if sess == nil {
		return nil, fmt.Errorf("no session for tab %q", tabTarget)
	}
	msgs := make([]schemas.ChatMessage, len(sess.Messages()))
	copy(msgs, sess.Messages())
	return &SessionExport{
		ID:           sess.ID,
		Messages:     msgs,
		ContextFiles: sess.GetContextFiles(),
		Provider:     sess.Provider,
		Model:        sess.Model,
		WorkingDir:   sess.WorkingDir,
		ProjectSlug:  sess.ProjectSlug,
		CreatedAt:    sess.CreatedAt,
		LastUpdated:  sess.LastUpdated,
	}, nil
}
