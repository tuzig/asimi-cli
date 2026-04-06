package shogunate

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturingMinister is like ritualTestMinister but captures the Task's Work and Scratchpad.
type capturingMinister struct {
	MinisterBase
	id           string
	tasksCh      chan *Task
	result       string
	capturedWork string
	capturedPad  string
	mu           sync.Mutex
}

func (m *capturingMinister) ID() string           { return m.id }
func (m *capturingMinister) SystemPrompt() string { return "" }
func (m *capturingMinister) Title() string        { return m.id }
func (m *capturingMinister) Tools() []Tool        { return nil }
func (m *capturingMinister) Tasks() chan<- *Task  { return m.tasksCh }
func (m *capturingMinister) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-m.tasksCh:
			m.mu.Lock()
			m.capturedWork = t.Work
			m.capturedPad = t.Scratchpad
			m.mu.Unlock()
			t.Done <- Result{Output: m.result, Sealed: true}
		}
	}
}

// TestCastleSiege_StrategistTaskCarriesContext verifies the ritual builds
// the correct Work and Scratchpad before dispatching to the strategist.
func TestCastleSiege_StrategistTaskCarriesContext(t *testing.T) {
	db := setupRitualTestDB(t)
	// Additional tables needed for edict background step
	require.NoError(t, db.AutoMigrate(&storage.Edict{}, &storage.Zhengming{}, &storage.Ling{}, &storage.Seal{}))

	// Create an edict so the background "the edict details" step succeeds
	edict := &storage.Edict{Intent: "Build a REST API for user management with CRUD endpoints"}
	require.NoError(t, db.Create(edict).Error)

	// Set up capturing strategist; other ministers are plain auto-completing
	strat := &capturingMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "strategist",
		tasksCh:      make(chan *Task, 1),
		result:       "battle plan created",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go strat.Run(ctx)

	ministers := map[string]Minister{"strategist": strat}
	for _, id := range []string{"forge", "judge", "sage", "chancellor", "marshal"} {
		m := &ritualTestMinister{
			MinisterBase: MinisterBase{logger: slog.Default()},
			id:           id,
			tasksCh:      make(chan *Task, 1),
			result:       "ok",
		}
		ministers[id] = m
		go m.Run(ctx)
	}

	shog := &Shogunate{ministers: ministers, logger: slog.Default()}
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	shog.ritualGuard = NewRitualGuard(RitualGuardOpts{
		Base:        base,
		GetMinister: shog.GetMinister,
	})

	// Load the builtin rituals so castle-siege is available
	registry := shog.GetRitualRegistry()
	builtins, err := LoadEmbeddedRituals()
	require.NoError(t, err)
	for _, r := range builtins {
		registry.Register(r)
	}

	runner := shog.GetRitualRunner()
	require.NotNil(t, runner)

	exec, err := runner.Start(ctx, "castle-siege", edict.Key(),
		map[string]string{"edict_id": "1"}, nil)
	require.NoError(t, err)

	// Run the ritual — it will stop after strategist (forge not seeded, but we only care about strat)
	_ = runner.Run(ctx, exec)

	strat.mu.Lock()
	defer strat.mu.Unlock()

	// Work should contain the castle-siege Act for strategizing
	assert.Contains(t, strat.capturedWork, "Analyze the edict below and produce a technical Battle Plan",
		"Work prompt must include the ritual Act text")

	// Work should contain given_context from background "the edict details"
	assert.Contains(t, strat.capturedWork, "Given Context",
		"Work prompt must include the Given Context section from background steps")

	// Scratchpad should contain ritual context
	assert.Contains(t, strat.capturedPad, "castle-siege",
		"Scratchpad must include ritual name")
	assert.Contains(t, strat.capturedPad, "Build a REST API",
		"Scratchpad must include edict intent")
}
