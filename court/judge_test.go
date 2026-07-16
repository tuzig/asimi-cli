package court

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/court/tools"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// stageManifestForTest is a test helper that creates a manifest directly in the DB.
func stageManifestForTest(t *testing.T, db *gorm.DB, key storage.EdictKey, lingID, filePath, funcName, contentSHA string) string {
	t.Helper()
	manifestID := GenerateID("manifest", fmt.Sprintf("%d", key.ID), lingID, filePath, fmt.Sprintf("%d", time.Now().UnixNano()))
	manifest := storage.ForgeManifest{
		ManifestID: manifestID,
		EdictID:    key.ID,
		Username:   key.Username,
		Project:    key.Project,
		LingID:     lingID,
		FilePath:   filePath,
		FuncName:   funcName,
		ContentSHA: contentSHA,
		Status:     storage.ManifestForged,
	}
	if err := db.Create(&manifest).Error; err != nil {
		t.Fatalf("failed to stage manifest: %v", err)
	}
	return manifestID
}

// toolContextForTest builds a tools.ToolContext from a MinisterBase for testing.
func toolContextForTest(base *MinisterBase) tools.ToolContext {
	tc := tools.ToolContext{
		RepoInfo:   &repo.RepoInfo{},
		MinisterID: base.ministerID,
		Username:   base.username,
		Project:    base.project,
		DB:         base.db,
	}
	*tc.RepoInfo = base.RepoInfo()
	return tc
}

// TestAllManifestsQuenched_NoManifests_EdictLevelVerdict tests that when no manifests exist,
// the tools' RecordVerdictTool.sealIfComplete checks for edict-level verdicts as a completion signal.
func TestAllManifestsQuenched_NoManifests_EdictLevelVerdict(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)

	// Create edict without any manifests
	edict, err := CreateEdictForTest(db, "Project init edict")
	assert.NoError(t, err)

	key := storage.EdictKey{ID: edict.ID, Username: base.username, Project: base.project}

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

	// Use the tools RecordVerdictTool to check sealing
	tool := tools.RecordVerdictTool{Ctx: toolContextForTest(base)}
	input := fmt.Sprintf(`{"edict_id": %d, "passed": true, "details": "project-init complete"}`, edict.ID)
	result, err := tool.Call(context.Background(), input)
	assert.NoError(t, err)
	assert.Contains(t, result, "sealed=true")
}

// TestRecordVerdictTool_EdictLevelVerdict tests that RecordVerdictTool creates edict-level
// verdicts when no manifests exist for an edict.
func TestRecordVerdictTool_EdictLevelVerdict(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)

	// Create edict without manifests
	edict, err := CreateEdictForTest(db, "Init ritual")
	assert.NoError(t, err)

	// Create the tool
	tool := tools.RecordVerdictTool{Ctx: toolContextForTest(base)}

	// Call with passed=true for edict with no manifests
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
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)

	// Create edict with manifest
	edict, err := CreateEdictForTest(db, "Feature with tests")
	assert.NoError(t, err)

	// Stage manifest using the base's key (with username/project)
	judgeKey := storage.EdictKey{ID: edict.ID, Username: base.username, Project: base.project}
	manifestID := stageManifestForTest(t, db, judgeKey, "", "feature.go", "TestFeature", "hash123")

	// Create the tool
	tool := tools.RecordVerdictTool{Ctx: toolContextForTest(base)}

	// Call with passed=true using the edict ID
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
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)

	// Create edict with manifest
	edict, err := CreateEdictForTest(db, "Feature with failing tests")
	assert.NoError(t, err)

	// Stage manifest using the base's key
	judgeKey := storage.EdictKey{ID: edict.ID, Username: base.username, Project: base.project}
	manifestID := stageManifestForTest(t, db, judgeKey, "", "feature.go", "TestFeature", "hash456")

	// Create the tool
	tool := tools.RecordVerdictTool{Ctx: toolContextForTest(base)}

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
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)

	// Create edict without manifests
	edict, err := CreateEdictForTest(db, "Init ritual failing")
	assert.NoError(t, err)

	// Create the tool
	tool := tools.RecordVerdictTool{Ctx: toolContextForTest(base)}

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

// TestRecordVerdictTool_EdictLevelFailedNotSealed tests that a failed
// edict-level verdict does NOT result in sealing.
func TestRecordVerdictTool_EdictLevelFailedNotSealed(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)

	edict, err := CreateEdictForTest(db, "Failed init edict")
	assert.NoError(t, err)

	// Record a failed edict-level verdict via the tool
	tool := tools.RecordVerdictTool{Ctx: toolContextForTest(base)}
	input := fmt.Sprintf(`{"edict_id": %d, "passed": false, "details": "init failed"}`, edict.ID)
	result, err := tool.Call(context.Background(), input)
	assert.NoError(t, err)
	assert.Contains(t, result, "sealed=false")
}

// TestRecordVerdictTool_EdictLevelPassedThenFailed tests latest-wins:
// a failed verdict after a passed one should not be sealed.
func TestRecordVerdictTool_EdictLevelPassedThenFailed(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)

	edict, err := CreateEdictForTest(db, "Retried init edict")
	assert.NoError(t, err)

	// First: passed verdict
	tool := tools.RecordVerdictTool{Ctx: toolContextForTest(base)}
	input := fmt.Sprintf(`{"edict_id": %d, "passed": true, "details": "first pass"}`, edict.ID)
	result, err := tool.Call(context.Background(), input)
	assert.NoError(t, err)
	assert.Contains(t, result, "sealed=true")

	// Later: failed verdict (supersedes the passed one)
	time.Sleep(10 * time.Millisecond)
	inputFail := fmt.Sprintf(`{"edict_id": %d, "passed": false, "details": "fail"}`, edict.ID)
	result, err = tool.Call(context.Background(), inputFail)
	assert.NoError(t, err)
	assert.Contains(t, result, "sealed=false")
}

// TestListPendingManifestsTool_Format tests the Format method of ListPendingManifestsTool
// for the three cases: error, no manifests, and N manifests.
func TestListPendingManifestsTool_Format(t *testing.T) {
	tool := tools.ListPendingManifestsTool{Ctx: ToolContextForTest()}

	// Error case
	out := tool.Format(`{"edict_id":1}`, "", fmt.Errorf("db error"))
	assert.Contains(t, out, "Error")
	assert.Contains(t, out, "db error")

	// No manifests — Call returns "No pending manifests found" (not JSON);
	// Format returns the full text.
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

// ToolContextForTest returns a ToolContext with a nil DB for Format-only tests.
func ToolContextForTest() tools.ToolContext {
	return tools.ToolContext{
		RepoInfo:   &repo.RepoInfo{},
		MinisterID: "test",
		Username:   "testuser",
		Project:    "testproject",
	}
}
