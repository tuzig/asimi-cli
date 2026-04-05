package shogunate

import (
	"context"
	"sync"
	"testing"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// capturingForgeLLM wraps mockLLM and records all prompts sent to it.
type capturingForgeLLM struct {
	mockLLM
	mu              sync.Mutex
	capturedPrompts []string
}

func (m *capturingForgeLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Capture the prompt from the last user message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llms.ChatMessageTypeHuman {
			for _, part := range messages[i].Parts {
				if tc, ok := part.(llms.TextContent); ok {
					m.capturedPrompts = append(m.capturedPrompts, tc.Text)
					return m.mockLLM.GenerateContent(ctx, messages, options...)
				}
			}
		}
	}

	return m.mockLLM.GenerateContent(ctx, messages, options...)
}

// TestForge_ExecutesLingNotRawWork verifies that when an edict has pending lings,
// the forge executes them via executeLings() rather than executing the raw task.Work.
func TestForge_ExecutesLingNotRawWork(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create edict with pending ling (simulating strategist output)
	edict := &storage.Edict{SessionID: "test-session", Intent: "Build REST API", Username: "testuser", Project: "testproject"}
	require.NoError(t, db.Create(edict).Error)

	ling := &storage.Ling{
		LingID:       "ling-1",
		EdictID:      edict.ID,
		Username:     "testuser",
		Project:      "testproject",
		Description:  "Create user model with CRUD fields",
		Dependencies: storage.StringArray{},
		Status:       storage.LingPending,
	}
	require.NoError(t, db.Create(ling).Error)

	// Create capturing mock LLM
	mock := &capturingForgeLLM{
		mockLLM: mockLLM{response: "ling executed successfully"},
	}

	// Create forge with the mock LLM
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	forge := NewForge(base)

	// Set up model and config via SetMinisterConfig (proper initialization)
	llmConfig := config.LLMConfig{Provider: "test", Model: "test-model"}
	sessionConfig := &SessionConfig{LLM: llmConfig}
	forge.SetMinisterConfig(mock, sessionConfig, repo.RepoInfo{})

	// Create done channel and task
	doneCh := make(chan Result, 1)
	rawWork := "Analyze the edict below and produce a technical Battle Plan..."
	scratchpad := "# Ritual: swift-strike\n\nForging step"

	task := &Task{
		Ctx:        context.Background(),
		EdictKey:   edict.Key(),
		Work:       rawWork,
		Scratchpad: scratchpad,
		Done:       doneCh,
	}

	// Process the task
	forge.processTask(context.Background(), task)

	// Get result
	result := <-doneCh
	require.NoError(t, result.Err, "processTask should not return an error")

	// Assert: captured prompt should contain ling description, NOT raw work
	mock.mu.Lock()
	prompts := make([]string, len(mock.capturedPrompts))
	copy(prompts, mock.capturedPrompts)
	mock.mu.Unlock()

	require.NotEmpty(t, prompts, "at least one prompt should have been captured")

	// The last captured prompt should contain the ling description
	lastPrompt := prompts[len(prompts)-1]

	// Should contain the ling description
	assert.Contains(t, lastPrompt, "Create user model with CRUD fields",
		"should contain the ling description")

	// Should NOT contain the raw strategist work
	assert.NotContains(t, lastPrompt, rawWork,
		"should NOT contain the raw task.Work from strategist")

	// Verify the ling was marked as done
	var updatedLing storage.Ling
	require.NoError(t, db.First(&updatedLing, "ling_id = ?", "ling-1").Error)
	assert.Equal(t, storage.LingDone, updatedLing.Status,
		"ling should be marked as done after execution")
}

// TestForge_TopologicalSort_MissingDepsTreatedAsDone verifies that when some
// lings in a DAG reference dependency IDs that are not in the current batch
// (e.g. because they were completed in a previous forge attempt and filtered
// out by GetPendingLing's status=pending predicate), topologicalSort treats
// those deps as already-satisfied rather than reporting a false circular
// dependency.
func TestForge_TopologicalSort_MissingDepsTreatedAsDone(t *testing.T) {
	// Mirrors the user's castle-siege failure:
	//   d69816 (Phase 1) — completed in a previous attempt, NOT in slice
	//   165d4c (Phase 2) — blocked by d69816
	//   c8fc40 (Phase 3) — blocked by d69816
	//   9df8b9 (Phase 4) — blocked by 165d4c, c8fc40
	//   0a45db (Phase 5) — blocked by d69816, 165d4c
	lings := []storage.Ling{
		{LingID: "165d4c", Dependencies: storage.StringArray{"d69816"}},
		{LingID: "c8fc40", Dependencies: storage.StringArray{"d69816"}},
		{LingID: "9df8b9", Dependencies: storage.StringArray{"165d4c", "c8fc40"}},
		{LingID: "0a45db", Dependencies: storage.StringArray{"d69816", "165d4c"}},
	}

	base := NewMinisterBase(nil, nil, nil, "u", "p")
	forge := NewForge(base)

	sorted, err := forge.topologicalSort(lings)
	require.NoError(t, err, "missing deps should be treated as completed, not as a cycle")
	require.Len(t, sorted, len(lings))

	// Verify ordering: every ling appears after all of its deps that ARE in the batch.
	pos := make(map[string]int, len(sorted))
	for i, l := range sorted {
		pos[l.LingID] = i
	}
	for _, l := range lings {
		for _, dep := range l.Dependencies {
			if depPos, ok := pos[dep]; ok {
				assert.Less(t, depPos, pos[l.LingID],
					"ling %s should come after its dep %s", l.LingID, dep)
			}
		}
	}
}
