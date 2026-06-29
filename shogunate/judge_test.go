package shogunate

import (
	"context"
	"fmt"
	"testing"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
)

// TestAllManifestsQuenched_NoManifests_EdictLevelVerdict tests that when no manifests exist,
// AllManifestsQuenched checks for edict-level verdicts as a completion signal.
func TestAllManifestsQuenched_NoManifests_EdictLevelVerdict(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	judge := NewJudge(base, nil)

	// Create edict without any manifests
	edict, err := CreateEdictForTest(db, "Project init edict")
	assert.NoError(t, err)

	// Use edict's actual key (may have empty username/project from CreateEdictForTest)
	key := edict.Key()

	// Initially no manifests and no verdicts -> not quenched
	quenched, err := judge.AllManifestsQuenched(key)
	assert.NoError(t, err)
	assert.False(t, quenched, "Should not be quenched without manifests or edict-level verdict")

	// Record an edict-level verdict (ManifestID = "")
	verdictID := GenerateID("verdict", fmt.Sprintf("%d", edict.ID), "edict", "direct")
	verdict := storage.JudgeVerdict{
		VerdictID:  verdictID,
		ManifestID: "", // empty = edict-level verdict
		Username:   key.Username,
		Project:    key.Project,
		TestSuite:  "edict",
		Outcome:    storage.VerdictPassed,
		Evidence:   storage.JSON{"details": "project-init complete"},
	}
	err = db.Create(&verdict).Error
	assert.NoError(t, err)

	// Now should be quenched
	quenched, err = judge.AllManifestsQuenched(key)
	assert.NoError(t, err)
	assert.True(t, quenched, "Should be quenched with edict-level verdict")
}

// TestRecordVerdictTool_EdictLevelVerdict tests that RecordVerdictTool creates edict-level
// verdicts when no manifests exist for an edict.
func TestRecordVerdictTool_EdictLevelVerdict(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	judge := NewJudge(base, nil)

	// Create edict without manifests
	edict, err := CreateEdictForTest(db, "Init ritual")
	assert.NoError(t, err)

	// Create the tool
	tool := RecordVerdictTool{judge: judge}

	// Call with passed=true for edict with no manifests
	// Use the edict's actual key values for the call
	input := fmt.Sprintf(`{"edict_id": %d, "passed": true, "details": "init complete"}`, edict.ID)
	result, err := tool.Call(ctx, input)
	assert.NoError(t, err)
	assert.Contains(t, result, "passed=true")
	assert.Contains(t, result, "sealed=true")

	// Check verdict was created at edict level
	var verdict storage.JudgeVerdict
	err = db.Where("manifest_id = '' AND test_suite = 'edict'").First(&verdict).Error
	assert.NoError(t, err, "Edict-level verdict should be created when no manifests exist")
	assert.Equal(t, storage.VerdictPassed, verdict.Outcome)
	assert.Equal(t, "init complete", verdict.Evidence["details"])
}

// TestRecordVerdictTool_ManifestVerdicts tests that RecordVerdictTool still works correctly
// when manifests exist (existing behavior not weakened).
func TestRecordVerdictTool_ManifestVerdicts(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	judge := NewJudge(base, nil)
	forge := NewForge(base)

	// Create edict with manifest
	edict, err := CreateEdictForTest(db, "Feature with tests")
	assert.NoError(t, err)

	// Stage manifest using the judge's key (with username/project)
	// This ensures the manifest matches the key used by RecordVerdictTool
	judgeKey := storage.EdictKey{ID: edict.ID, Username: base.username, Project: base.project}
	manifestID, err := forge.StageManifest(judgeKey, "", "feature.go", "TestFeature", "hash123")
	assert.NoError(t, err)

	// Create the tool
	tool := RecordVerdictTool{judge: judge}

	// Call with passed=true using the edict ID (tool will use judge's username/project)
	input := fmt.Sprintf(`{"edict_id": %d, "passed": true}`, edict.ID)
	result, err := tool.Call(ctx, input)
	assert.NoError(t, err)
	assert.Contains(t, result, "passed=true")

	// Check manifest is quenched
	var manifest storage.ForgeManifest
	err = db.Where("manifest_id = ?", manifestID).First(&manifest).Error
	assert.NoError(t, err)
	assert.Equal(t, storage.ManifestQuenched, manifest.Status)
}

// TestRecordVerdictTool_ManifestVerdictsFailed tests that RecordVerdictTool correctly
// marks manifests as rejected when passed=false.
func TestRecordVerdictTool_ManifestVerdictsFailed(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	judge := NewJudge(base, nil)
	forge := NewForge(base)

	// Create edict with manifest
	edict, err := CreateEdictForTest(db, "Feature with failing tests")
	assert.NoError(t, err)

	// Stage manifest using the judge's key
	judgeKey := storage.EdictKey{ID: edict.ID, Username: base.username, Project: base.project}
	manifestID, err := forge.StageManifest(judgeKey, "", "feature.go", "TestFeature", "hash456")
	assert.NoError(t, err)

	// Create the tool
	tool := RecordVerdictTool{judge: judge}

	// Call with passed=false
	input := fmt.Sprintf(`{"edict_id": %d, "passed": false, "details": "tests failed"}`, edict.ID)
	result, err := tool.Call(ctx, input)
	assert.NoError(t, err)
	assert.Contains(t, result, "passed=false")

	// Check manifest is rejected
	var manifest storage.ForgeManifest
	err = db.Where("manifest_id = ?", manifestID).First(&manifest).Error
	assert.NoError(t, err)
	assert.Equal(t, storage.ManifestRejected, manifest.Status)
}

// TestRecordVerdictTool_EdictLevelFailed tests that RecordVerdictTool correctly
// creates a failed edict-level verdict when no manifests exist.
func TestRecordVerdictTool_EdictLevelFailed(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	judge := NewJudge(base, nil)

	// Create edict without manifests
	edict, err := CreateEdictForTest(db, "Init ritual failing")
	assert.NoError(t, err)

	// Create the tool
	tool := RecordVerdictTool{judge: judge}

	// Call with passed=false
	input := fmt.Sprintf(`{"edict_id": %d, "passed": false, "details": "init failed"}`, edict.ID)
	result, err := tool.Call(ctx, input)
	assert.NoError(t, err)
	assert.Contains(t, result, "passed=false")

	// Check verdict was created at edict level with failed outcome
	var verdict storage.JudgeVerdict
	err = db.Where("manifest_id = '' AND test_suite = 'edict'").First(&verdict).Error
	assert.NoError(t, err, "Edict-level verdict should be created")
	assert.Equal(t, storage.VerdictFailed, verdict.Outcome)
}

// TestListPendingManifestsTool_Format tests the Format method of ListPendingManifestsTool
// for the three cases: error, no manifests, and N manifests.
func TestListPendingManifestsTool_Format(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	judge := NewJudge(base, nil)
	tool := &ListPendingManifestsTool{judge: judge}

	// Error case
	out := tool.Format(`{"edict_id":1}`, "", fmt.Errorf("db error"))
	assert.Contains(t, out, "Error")
	assert.Contains(t, out, "db error")

	// No manifests — Call returns "No pending manifests found" (not JSON);
	// Format logs an error and returns the full text for debugging.
	out = tool.Format(`{"edict_id":1}`, "No pending manifests found", nil)
	assert.Equal(t, "No pending manifests found\n", out)

	// One manifest — Call returns a JSON array
	jsonResult := `[{"manifest_id":"m1"}]`
	out = tool.Format(`{"edict_id":1}`, jsonResult, nil)
	assert.Equal(t, "Listed 1 pending manifests\n", out)

	// Multiple manifests
	jsonResult = `[{"manifest_id":"m1"},{"manifest_id":"m2"},{"manifest_id":"m3"}]`
	out = tool.Format(`{"edict_id":1}`, jsonResult, nil)
	assert.Equal(t, "Listed 3 pending manifests\n", out)
}
