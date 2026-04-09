package shogunate

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// capturingMockLLM captures all messages sent to GenerateContent for later inspection.
type capturingMockLLM struct {
	llms.Model
	response        string
	capturedMessages []llms.MessageContent
	mu              sync.Mutex
}

func (m *capturingMockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return m.response, nil
}

func (m *capturingMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.mu.Lock()
	m.capturedMessages = append(m.capturedMessages, messages...)
	m.mu.Unlock()

	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}
	if callOpts.StreamingFunc != nil {
		callOpts.StreamingFunc(ctx, []byte(m.response))
	}

	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: m.response}},
	}, nil
}

// capturingMinister uses a capturingMockLLM so that executeMinisterStep's
// ephemeral session can actually call the model and we can inspect what was sent.
type capturingMinister struct {
	MinisterBase
	id      string
	tasksCh chan *Task
	result  string
	mockLLM *capturingMockLLM
}

func (m *capturingMinister) ID() string           { return m.id }
func (m *capturingMinister) SystemPrompt() string { return "" }
func (m *capturingMinister) Title() string        { return m.id }
func (m *capturingMinister) Tools() []Tool        { return nil }
func (m *capturingMinister) Tasks() chan<- *Task   { return m.tasksCh }
func (m *capturingMinister) Model() llms.Model     { return m.mockLLM }
func (m *capturingMinister) GetConfig() config.LLMConfig { return config.LLMConfig{} }
func (m *capturingMinister) Run(ctx context.Context) {
	<-ctx.Done()
}

// allMessagesText concatenates all text parts from captured messages.
func (m *capturingMinister) allMessagesText() string {
	m.mockLLM.mu.Lock()
	defer m.mockLLM.mu.Unlock()
	var sb strings.Builder
	for _, msg := range m.mockLLM.capturedMessages {
		for _, part := range msg.Parts {
			if tc, ok := part.(llms.TextContent); ok {
				sb.WriteString(tc.Text)
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
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
		mockLLM:      &capturingMockLLM{response: "battle plan created"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ministers := map[string]Minister{"strategist": strat}
	for _, id := range []string{"forge", "judge", "sage", "chancellor", "marshal"} {
		m := &ritualTestMinister{
			MinisterBase: MinisterBase{logger: slog.Default()},
			id:           id,
			tasksCh:      make(chan *Task, 1),
			result:       "ok",
		}
		ministers[id] = m
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

	// The captured messages should contain the castle-siege Act for strategizing
	allText := strat.allMessagesText()

	assert.Contains(t, allText, "Analyze the edict below and produce a technical Battle Plan",
		"Work prompt must include the ritual Act text")

	// The edict intent should appear in the messages (via work prompt or system prompt)
	assert.Contains(t, allText, "Build a REST API",
		"Messages must include edict intent")
}
