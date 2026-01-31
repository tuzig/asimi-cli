package shogunate

import (
	"context"
	"log/slog"
	"time"

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

// ShogunateConfig holds configuration for the Shogunate.
type ShogunateConfig struct {
	PollInterval  time.Duration
	RitualTimeout time.Duration
}

// DefaultShogunateConfig returns the default configuration.
func DefaultShogunateConfig() *ShogunateConfig {
	return &ShogunateConfig{
		PollInterval:  5 * time.Second,
		RitualTimeout: 30 * time.Second,
	}
}

// RitualGuardRunner runs scheduled ritual guard cycles.
type RitualGuardRunner interface {
	Run(ctx context.Context) error
}

// Shogunate coordinates ministers and manages lifecycle.
type Shogunate struct {
	db     *gorm.DB
	logger *slog.Logger
	config *ShogunateConfig

	Chancellor Minister
	Strategist Minister
	Forge      Minister
	Judge      Minister
	Censor     Minister
	Marshal    Minister

	RitualGuard RitualGuardRunner

	ctx    context.Context
	cancel context.CancelFunc
}

// NewShogunate creates a new Shogunate coordinator.
func NewShogunate(db *gorm.DB, config *ShogunateConfig, logger *slog.Logger) *Shogunate {
	s := &Shogunate{
		db:     db,
		logger: logger,
		config: config,
	}
	s.ensureDefaults()
	return s
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

	// TODO: restore minister construction once constructors are migrated from legacy.
	// s.Chancellor = NewChancellor(...)
	// s.Strategist = NewStrategist(...)
	// s.Forge = NewForge(...)
	// s.Judge = NewJudge(...)
	// s.Censor = NewCensor(...)
	// s.Marshal = NewMarshal(...)

	for _, minister := range s.Ministers() {
		go minister.Run(s.ctx)
	}

	if s.RitualGuard != nil {
		go s.runRitualGuard()
	}

	s.logger.Info("shogunate started",
		"poll_interval", s.config.PollInterval,
		"ministers", s.ministerIDs())

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

// Ministers returns the active ministers.
func (s *Shogunate) Ministers() []Minister {
	if s == nil {
		return nil
	}

	ministers := []Minister{
		s.Chancellor,
		s.Strategist,
		s.Forge,
		s.Judge,
		s.Censor,
		s.Marshal,
	}

	active := make([]Minister, 0, len(ministers))
	for _, minister := range ministers {
		if minister != nil {
			active = append(active, minister)
		}
	}
	return active
}

func (s *Shogunate) runRitualGuard() {
	s.ensureDefaults()
	if s.RitualGuard == nil {
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
			if err := s.RitualGuard.Run(runCtx); err != nil {
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
		s.config = DefaultShogunateConfig()
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
