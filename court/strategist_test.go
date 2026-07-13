package court

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	asimitools "github.com/afittestide/asimi/court/tools"
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
func (m *capturingMinister) Model() LLMProvider          { return nil }
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

// TestStrategist_ZhengmingRoutesToStrategist verifies that when the Strategist
// raises a Zhengming request, the question is routed to the Strategist's tab.
// Before edict 489, all zhengming routed to "chancellor" regardless of caller.
// Now each minister's request_zhengming carries its own MinisterID so the
// ZhengmingPendingMsg routes to the correct tab.
func TestStrategist_ZhengmingRoutesToStrategist(t *testing.T) {
	db := setupRitualTestDB(t)
	require.NoError(t, db.AutoMigrate(&storage.Edict{}))

	cfg := config.DefaultCourtConfig()
	court := NewCourt(db, cfg, nil, nil)
	require.NotNil(t, court)
	court.ConfigureModel(nil, &SessionConfig{LLM: config.LLMConfig{Provider: "test", Model: "test"}}, repo.RepoInfo{})

	strategist := court.GetMinister("strategist")
	require.NotNil(t, strategist)

	tools := strategist.Tools()
	var zhengmingTool asimitools.RequestZhengmingTool
	for _, t := range tools {
		if zt, ok := t.(asimitools.RequestZhengmingTool); ok {
			zhengmingTool = zt
			break
		}
	}

	// The MinisterID must be "strategist" so the question appears in the Strategist's tab
	assert.Equal(t, "strategist", zhengmingTool.MinisterID,
		"Strategist's Zhengming must route to Strategist's tab, not Chancellor's")

	// Requester should be the strategist itself (for answer delivery via DeliverZhengming)
	assert.NotNil(t, zhengmingTool.Requester, "Requester must be set")
}

// TestStrategist_InsertLingTool_DescriptionMentionsFullLingID verifies that
// InsertLingTool.Description() explicitly instructs the LLM to use full ling IDs
// and to avoid shorthand aliases. This prevents the DAG resolver from failing
// when the strategist invents abbreviated dependency references.
func TestStrategist_InsertLingTool_DescriptionMentionsFullLingID(t *testing.T) {
	tool := asimitools.InsertLingTool{}

	desc := tool.Description()

	// Must mention "FULL ling IDs" to emphasize the constraint
	assert.Contains(t, desc, "FULL ling IDs",
		"Description must emphasize FULL ling IDs to prevent shorthand usage")

	// Must include an example of a real ling_id format so the LLM knows what to expect
	assert.Contains(t, desc, "'74183c66ba0507ba'",
		"Description must include a concrete full ling_id example")

	// Must explicitly forbid shorthand aliases
	assert.Contains(t, desc, "never use shorthand",
		"Description must warn against shorthand aliases")
}

// TestStrategist_InsertLingTool_ParameterSchemaWarnsAboutShorthand verifies that
// the dependencies parameter in ParameterSchema explicitly warns against
// shorthand aliases and instructs use of full ling IDs.
func TestStrategist_InsertLingTool_ParameterSchemaWarnsAboutShorthand(t *testing.T) {
	tool := asimitools.InsertLingTool{}

	schema := tool.ParameterSchema()

	props := schema["properties"].(map[string]any)
	deps := props["dependencies"].(map[string]any)
	desc := deps["description"].(string)

	// Must mention "FULL ling IDs" to emphasize the constraint
	assert.Contains(t, desc, "FULL ling IDs",
		"ParameterSchema dependencies description must emphasize FULL ling IDs")

	// Must include a concrete example
	assert.Contains(t, desc, "'74183c66ba0507ba'",
		"ParameterSchema dependencies description must include a concrete full ling_id example")

	// Must warn against shorthand aliases
	assert.Contains(t, desc, "never invent shorthand",
		"ParameterSchema dependencies description must warn against inventing shorthand aliases")
}

// TestStrategist_StrategistRoleMentionsFullLingID verifies that the StrategistRole
// system prompt includes the critical rule about using exact ling_id values.
func TestStrategist_StrategistRoleMentionsFullLingID(t *testing.T) {
	// Must mention the rule about exact ling_id values
	assert.Contains(t, StrategistRole, "exact ling_id",
		"StrategistRole must instruct use of exact ling_id values for dependencies")

	// Must include an example of a real ling_id format
	assert.Contains(t, StrategistRole, "'74183c66ba0507ba'",
		"StrategistRole must include a concrete full ling_id example")

	// Must warn against shorthand
	assert.Contains(t, StrategistRole, "Never use shorthand",
		"StrategistRole must warn against shorthand dependency references")
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
	for _, id := range []string{"forge", "judge", "sage", "chancellor"} {
		m := &ritualTestMinister{
			MinisterBase: MinisterBase{logger: slog.Default()},
			id:           id,
			tasksCh:      make(chan *Task, 1),
			result:       "ok",
		}
		ministers[id] = m
		go m.Run(ctx)
	}

	court := &Court{ministers: ministers, logger: slog.Default()}
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	court.ritualGuard = NewRitualGuard(RitualGuardOpts{
		Base:        base,
		GetMinister: court.GetMinister,
	})

	// Load the builtin rituals so castle-siege is available
	registry := court.GetRitualRegistry()
	builtins, err := LoadEmbeddedRituals()
	require.NoError(t, err)
	for _, r := range builtins {
		registry.Register(r)
	}

	runner := court.GetRitualRunner()
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
