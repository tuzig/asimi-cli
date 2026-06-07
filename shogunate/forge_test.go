package shogunate

import (
	"testing"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	t.Skip("requires bifrost mock")
}

// TestForge_ProcessTask_SkipsFixLoopWhenNoFailedVerdicts verifies that when there
// are no failed verdicts, processTask executes the normal ling work.
func TestForge_ProcessTask_SkipsFixLoopWhenNoFailedVerdicts(t *testing.T) {
	t.Skip("requires bifrost mock")
}
