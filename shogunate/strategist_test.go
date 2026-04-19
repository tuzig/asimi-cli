package shogunate

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	asimitools "github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturingMinister captures task Work prompts dispatched to it via the task channel.
type capturingMinister struct {
	MinisterBase
	id           string
	tasksCh      chan *Task
	result       string
	mu           sync.Mutex
	capturedWork []string
}

func (m *capturingMinister) ID() string                  { return m.id }
func (m *capturingMinister) SystemPrompt() string        { return "" }
func (m *capturingMinister) Title() string               { return m.id }
func (m *capturingMinister) Tools() []Tool               { return nil }
func (m *capturingMinister) Tasks() chan<- *Task         { return m.tasksCh }
func (m *capturingMinister) Model() LLMProvider     { return nil }
func (m *capturingMinister) GetConfig() config.LLMConfig { return config.LLMConfig{} }
func (m *capturingMinister) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-m.tasksCh:
			m.mu.Lock()
			m.capturedWork = append(m.capturedWork, t.Work)
			if t.Scratchpad != "" {
				m.capturedWork = append(m.capturedWork, t.Scratchpad)
			}
			m.mu.Unlock()
			t.Done <- Result{Output: m.result}
		}
	}
}

// allCapturedText concatenates all captured work prompts.
func (m *capturingMinister) allCapturedText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sb strings.Builder
	for _, w := range m.capturedWork {
		sb.WriteString(w)
		sb.WriteString("\n")
	}
	return sb.String()
}

// TestStrategist_ZhengmingRoutesToChancellor verifies that when the Strategist
// raises a Zhengming request, the question is routed to the Chancellor's ruling tab.
// This is critical for UX: the Strategist handles planning but clarification questions
// should appear in the Chancellor's UI where the Ruler is already engaged.
func TestStrategist_ZhengmingRoutesToChancellor(t *testing.T) {
	db := setupRitualTestDB(t)
	require.NoError(t, db.AutoMigrate(&storage.Edict{}))

	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	strategist := NewStrategist(base)

	// Configure with empty LLM config to avoid nil pointer in GetROTools
	llmConfig := config.LLMConfig{Provider: "test", Model: "test"}
	strategist.SetMinisterConfig(nil, &SessionConfig{LLM: llmConfig}, repo.RepoInfo{})

	tools := strategist.Tools()
	var zhengmingTool asimitools.RequestZhengmingTool
	for _, t := range tools {
		if zt, ok := t.(asimitools.RequestZhengmingTool); ok {
			zhengmingTool = zt
			break
		}
	}

	// The MinisterID must be "chancellor" so the question appears in the Chancellor's tab
	assert.Equal(t, "chancellor", zhengmingTool.MinisterID,
		"Strategist's Zhengming must route to Chancellor's tab, not Strategist's")

	// Requester should be the strategist itself (for answer delivery via DeliverZhengming)
	assert.NotNil(t, zhengmingTool.Requester, "Requester must be set")
}

// TestCastleSiege_StrategistTaskCarriesContext verifies the ritual builds
// the correct Work prompt before dispatching to the strategist.
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

	// Use a timeout so test doesn't hang if later steps block
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
	defer timeoutCancel()

	exec, err := runner.Start(timeoutCtx, "castle-siege", edict.Key(),
		map[string]string{"edict_id": "1"}, nil)
	require.NoError(t, err)

	// Run the ritual — strategist step will complete, subsequent steps may fail
	_ = runner.Run(timeoutCtx, exec)

	// The captured work should contain the castle-siege Act for strategizing
	allText := strat.allCapturedText()

	assert.Contains(t, allText, "Analyze the edict below and produce a technical Battle Plan",
		"Work prompt must include the ritual Act text")

	// The edict intent should appear in the messages (via work prompt or scratchpad)
	assert.Contains(t, allText, "Build a REST API",
		"Messages must include edict intent")
}
