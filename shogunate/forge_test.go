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

// TestForge_StageManifest_UniqueIDs verifies that StageManifest generates
// unique IDs even when called multiple times with the same inputs.
// This prevents UNIQUE constraint violations when the ritual retries.
func TestForge_StageManifest_UniqueIDs(t *testing.T) {
	db := setupMinisterTestDB(t)

	edict := &storage.Edict{SessionID: "test-session", Intent: "Build REST API", Username: "testuser", Project: "testproject"}
	require.NoError(t, db.Create(edict).Error)

	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	forge := NewForge(base)

	key := edict.Key()
	lingID := "ling-1"
	filePath := "internal/model/user.go"
	funcName := "User"
	contentSHA := "abc123"

	// Stage the same manifest multiple times with identical inputs
	id1, err := forge.StageManifest(key, lingID, filePath, funcName, contentSHA)
	require.NoError(t, err)

	id2, err := forge.StageManifest(key, lingID, filePath, funcName, contentSHA)
	require.NoError(t, err)

	id3, err := forge.StageManifest(key, lingID, filePath, funcName, contentSHA)
	require.NoError(t, err)

	// All IDs should be unique
	assert.NotEqual(t, id1, id2, "second call should produce different ID")
	assert.NotEqual(t, id2, id3, "third call should produce different ID")
	assert.NotEqual(t, id1, id3, "first and third call should produce different ID")

	// All should be stored in the database
	var manifests []storage.ForgeManifest
	require.NoError(t, db.Where("edict_id = ?", edict.ID).Find(&manifests).Error)
	assert.Len(t, manifests, 3, "all three manifests should be in the database")
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

// TestForge_GetFailedVerdicts_QueriesCorrectVerdicts verifies that GetFailedVerdicts
// returns only failed verdicts for the specified edict, joined with forge_manifests.
func TestForge_GetFailedVerdicts_QueriesCorrectVerdicts(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create edict
	edict := &storage.Edict{SessionID: "test-session", Intent: "Build REST API", Username: "testuser", Project: "testproject"}
	require.NoError(t, db.Create(edict).Error)

	// Create manifest for the edict
	manifest := &storage.ForgeManifest{
		ManifestID: "manifest-1",
		EdictID:    edict.ID,
		Username:   "testuser",
		Project:    "testproject",
		LingID:     "ling-1",
		FilePath:   "internal/model/user.go",
		Status:     storage.ManifestForged,
	}
	require.NoError(t, db.Create(manifest).Error)

	// Create failed verdict
	failedVerdict := &storage.JudgeVerdict{
		VerdictID:  "verdict-failed-1",
		ManifestID: "manifest-1",
		TestSuite:  "unit",
		Outcome:    storage.VerdictFailed,
		Evidence:   storage.JSON{"error": "test failed", "details": "missing assertion"},
	}
	require.NoError(t, db.Create(failedVerdict).Error)

	// Create another manifest with a PASSED verdict
	manifest2 := &storage.ForgeManifest{
		ManifestID: "manifest-2",
		EdictID:    edict.ID,
		Username:   "testuser",
		Project:    "testproject",
		LingID:     "ling-2",
		FilePath:   "internal/model/user_test.go",
		Status:     storage.ManifestQuenched,
	}
	require.NoError(t, db.Create(manifest2).Error)

	passedVerdict := &storage.JudgeVerdict{
		VerdictID:  "verdict-passed-1",
		ManifestID: "manifest-2",
		TestSuite:  "unit",
		Outcome:    storage.VerdictPassed,
		Evidence:   storage.JSON{},
	}
	require.NoError(t, db.Create(passedVerdict).Error)

	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	forge := NewForge(base)

	// Get failed verdicts
	verdicts, err := forge.GetFailedVerdicts(edict.Key())
	require.NoError(t, err)

	// Should only return the failed verdict, not the passed one
	require.Len(t, verdicts, 1)
	assert.Equal(t, "verdict-failed-1", verdicts[0].VerdictID)
	assert.Equal(t, storage.VerdictFailed, verdicts[0].Outcome)
}

// TestForge_GetFailedVerdicts_ReturnsEmptyWhenNoFailures verifies the happy path
// when there are no failed verdicts.
func TestForge_GetFailedVerdicts_ReturnsEmptyWhenNoFailures(t *testing.T) {
	db := setupMinisterTestDB(t)

	edict := &storage.Edict{SessionID: "test-session", Intent: "Build REST API", Username: "testuser", Project: "testproject"}
	require.NoError(t, db.Create(edict).Error)

	manifest := &storage.ForgeManifest{
		ManifestID: "manifest-1",
		EdictID:    edict.ID,
		Username:   "testuser",
		Project:    "testproject",
		Status:     storage.ManifestQuenched,
	}
	require.NoError(t, db.Create(manifest).Error)

	passedVerdict := &storage.JudgeVerdict{
		VerdictID:  "verdict-passed-1",
		ManifestID: "manifest-1",
		Outcome:    storage.VerdictPassed,
	}
	require.NoError(t, db.Create(passedVerdict).Error)

	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	forge := NewForge(base)

	verdicts, err := forge.GetFailedVerdicts(edict.Key())
	require.NoError(t, err)
	assert.Empty(t, verdicts)
}

// TestForge_buildFixPrompt_FormatsEvidenceCorrectly verifies that buildFixPrompt
// properly formats the verdict evidence into a fix instruction.
func TestForge_buildFixPrompt_FormatsEvidenceCorrectly(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil, "u", "p")
	forge := NewForge(base)

	verdict := storage.JudgeVerdict{
		VerdictID:  "verdict-123",
		ManifestID: "manifest-456",
		Evidence:   storage.JSON{"error": "missing return statement", "file": "user.go", "line": 42},
	}

	prompt := forge.buildFixPrompt(verdict)

	// Should reference the manifest ID
	assert.Contains(t, prompt, "manifest-456")
	// Should contain the evidence
	assert.Contains(t, prompt, "missing return statement")
	assert.Contains(t, prompt, "user.go")
	// Should have fix instruction
	assert.Contains(t, prompt, "Focus on minimal")
}

// TestForge_ProcessTask_FixesFailedVerdictsFirst verifies that when an edict has
// failed verdicts, processTask calls the LLM with fix prompts instead of
// executing the normal ling work.
func TestForge_ProcessTask_FixesFailedVerdictsFirst(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create edict with pending ling
	edict := &storage.Edict{SessionID: "test-session", Intent: "Build REST API", Username: "testuser", Project: "testproject"}
	require.NoError(t, db.Create(edict).Error)

	ling := &storage.Ling{
		LingID:       "ling-1",
		EdictID:      edict.ID,
		Username:     "testuser",
		Project:      "testproject",
		Description:  "Create user model",
		Dependencies: storage.StringArray{},
		Status:       storage.LingPending,
	}
	require.NoError(t, db.Create(ling).Error)

	// Create manifest with failed verdict
	manifest := &storage.ForgeManifest{
		ManifestID: "manifest-1",
		EdictID:    edict.ID,
		Username:   "testuser",
		Project:    "testproject",
		LingID:     "ling-1",
		FilePath:   "user.go",
		Status:     storage.ManifestForged,
	}
	require.NoError(t, db.Create(manifest).Error)

	failedVerdict := &storage.JudgeVerdict{
		VerdictID:  "verdict-1",
		ManifestID: "manifest-1",
		Outcome:    storage.VerdictFailed,
		Evidence:   storage.JSON{"error": "test failed"},
	}
	require.NoError(t, db.Create(failedVerdict).Error)

	// Create capturing mock LLM
	mock := &capturingForgeLLM{
		mockLLM: mockLLM{response: "fixed the issue"},
	}

	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	forge := NewForge(base)

	llmConfig := config.LLMConfig{Provider: "test", Model: "test-model"}
	forge.SetMinisterConfig(mock, &SessionConfig{LLM: llmConfig}, repo.RepoInfo{})

	doneCh := make(chan Result, 1)
	task := &Task{
		Ctx:      context.Background(),
		EdictKey: edict.Key(),
		Work:     "Execute normal work",
		Done:     doneCh,
	}

	forge.processTask(context.Background(), task)

	result := <-doneCh
	require.NoError(t, result.Err)

	// Verify that the LLM was called with a fix prompt (containing "test failed")
	mock.mu.Lock()
	prompts := make([]string, len(mock.capturedPrompts))
	copy(prompts, mock.capturedPrompts)
	mock.mu.Unlock()

	require.NotEmpty(t, prompts, "LLM should have been called")

	// The first prompt should contain the fix instruction (evidence from failed verdict)
	firstPrompt := prompts[0]
	assert.Contains(t, firstPrompt, "test failed", "fix prompt should contain verdict evidence")
	assert.Contains(t, firstPrompt, "manifest-1", "fix prompt should reference manifest")
}

// TestForge_ProcessTask_SkipsFixLoopWhenNoFailedVerdicts verifies that when there
// are no failed verdicts, processTask executes the normal ling work.
func TestForge_ProcessTask_SkipsFixLoopWhenNoFailedVerdicts(t *testing.T) {
	db := setupMinisterTestDB(t)

	edict := &storage.Edict{SessionID: "test-session", Intent: "Build REST API", Username: "testuser", Project: "testproject"}
	require.NoError(t, db.Create(edict).Error)

	ling := &storage.Ling{
		LingID:       "ling-1",
		EdictID:      edict.ID,
		Username:     "testuser",
		Project:      "testproject",
		Description:  "Create user model",
		Dependencies: storage.StringArray{},
		Status:       storage.LingPending,
	}
	require.NoError(t, db.Create(ling).Error)

	// Create manifest with PASSED verdict (not failed)
	manifest := &storage.ForgeManifest{
		ManifestID: "manifest-1",
		EdictID:    edict.ID,
		Username:   "testuser",
		Project:    "testproject",
		LingID:     "ling-1",
		FilePath:   "user.go",
		Status:     storage.ManifestQuenched,
	}
	require.NoError(t, db.Create(manifest).Error)

	passedVerdict := &storage.JudgeVerdict{
		VerdictID:  "verdict-1",
		ManifestID: "manifest-1",
		Outcome:    storage.VerdictPassed,
	}
	require.NoError(t, db.Create(passedVerdict).Error)

	mock := &capturingForgeLLM{
		mockLLM: mockLLM{response: "ling executed"},
	}

	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	forge := NewForge(base)

	llmConfig := config.LLMConfig{Provider: "test", Model: "test-model"}
	forge.SetMinisterConfig(mock, &SessionConfig{LLM: llmConfig}, repo.RepoInfo{})

	doneCh := make(chan Result, 1)
	task := &Task{
		Ctx:      context.Background(),
		EdictKey: edict.Key(),
		Work:     "Execute normal work",
		Done:     doneCh,
	}

	forge.processTask(context.Background(), task)

	result := <-doneCh
	require.NoError(t, result.Err)

	// Verify ling was executed (not fix loop)
	mock.mu.Lock()
	prompts := make([]string, len(mock.capturedPrompts))
	copy(prompts, mock.capturedPrompts)
	mock.mu.Unlock()

	require.NotEmpty(t, prompts)
	// Should contain ling description, not "test failed"
	lastPrompt := prompts[len(prompts)-1]
	assert.Contains(t, lastPrompt, "Create user model")
	assert.NotContains(t, lastPrompt, "test failed")
}
