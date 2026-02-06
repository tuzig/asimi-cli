package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/tmc/langchaingo/llms"
	"gorm.io/gorm"
)

// ShogunateEvent represents an event emitted by the Shogunate lifecycle.
type ShogunateEvent string

const (
	EventEdictAssigned     ShogunateEvent = "edict_assigned"
	EventEdictCreated      ShogunateEvent = "edict_created"
	EventPhaseChanged      ShogunateEvent = "phase_changed"
	EventForgeCommitted    ShogunateEvent = "forge_committed"
	EventRitualStarted     ShogunateEvent = "ritual_started"
	EventRitualCompleted   ShogunateEvent = "ritual_completed"
	EventRitualFailed      ShogunateEvent = "ritual_failed"
	EventStepCompleted     ShogunateEvent = "step_completed"
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
	}
	s.ensureDefaults()

	// Create ritual runner with shell runner for cmd steps
	s.ritualRunner = NewRitualRunner(s.ritualRegistry, s, db, runner, s.logger)

	// Create all ministers with minimal base (model/config/repoInfo can be set later)
	// Runner is passed so ministers that need shell access get it for free
	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, runner, s.logger)

	chancellor := NewChancellor(base)
	chancellor.SetShogunate(s)
	s.ministers[chancellor.ID()] = chancellor

	s.ministers["strategist"] = NewStrategist(base)
	s.ministers["forge"] = NewForge(base)
	s.ministers["judge"] = NewJudge(base, nil)
	s.ministers["censor"] = NewCensor(base, nil)
	s.ministers["marshal"] = NewMarshal(base, nil)

	return s
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
		if base, ok := minister.(interface{ SetMinisterConfig(llms.Model, *SessionConfig, repo.RepoInfo) }); ok {
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
		return nil
	}
	return ch.GetSession(edictID)
}
