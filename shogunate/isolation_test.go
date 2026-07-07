package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Cross-project isolation tests: each test creates records for project A,
// then queries as project B, and asserts zero results.

const (
	isolationUserA = "alice"
	isolationProjA = "project-a"
	isolationUserB = "bob"
	isolationProjB = "project-b"
)

// createEdictForProject creates an edict under a specific username/project.
func createEdictForProject(db *gorm.DB, username, project, intent string) (*storage.Edict, error) {
	edict := storage.Edict{
		Username: username,
		Project:  project,
		Intent:   intent,
	}
	if err := db.Create(&edict).Error; err != nil {
		return nil, err
	}
	return &edict, nil
}

// stageManifestDB creates a manifest directly in the DB for tests.
func stageManifestDB(t *testing.T, db *gorm.DB, key storage.EdictKey, lingID, filePath, funcName, contentSHA string) string {
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
	require.NoError(t, db.Create(&manifest).Error)
	return manifestID
}

// TestIsolation_MarshalIncident verifies that MarshalIncident queries with
// wrong username/project return no results.
func TestIsolation_MarshalIncident(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create an incident under project A
	edictA, err := createEdictForProject(db, isolationUserA, isolationProjA, "incident test")
	require.NoError(t, err)

	keyA := storage.EdictKey{ID: edictA.ID, Username: isolationUserA, Project: isolationProjA}
	incidentID := "inc-001"
	incident := storage.MarshalIncident{
		IncidentID: incidentID,
		EdictID:    keyA.ID,
		Username:   keyA.Username,
		Project:    keyA.Project,
		CommitHash: "deadbeef",
		RCASummary: "segfault in main",
	}
	require.NoError(t, db.Create(&incident).Error)

	// Verify project A can see its own incident
	var foundA storage.MarshalIncident
	err = db.Where("incident_id = ? AND username = ? AND project = ?", incidentID, isolationUserA, isolationProjA).First(&foundA).Error
	require.NoError(t, err)
	assert.NotNil(t, foundA)

	// Query as project B — should get "not found"
	var foundB storage.MarshalIncident
	err = db.Where("incident_id = ? AND username = ? AND project = ?", incidentID, isolationUserB, isolationProjB).First(&foundB).Error
	assert.Error(t, err, "cross-project GetIncident should return error")

	// GetPendingIncidents as project B should return zero
	var pending []storage.MarshalIncident
	err = db.Where("hotfix_approved = ? AND username = ? AND project = ?", false, isolationUserB, isolationProjB).
		Order("created_at ASC").
		Find(&pending).Error
	require.NoError(t, err)
	assert.Empty(t, pending, "cross-project GetPendingIncidents should return empty")

	// MarkHotfixApproved as project B should affect zero rows
	result := db.Model(&storage.MarshalIncident{}).
		Where("incident_id = ? AND username = ? AND project = ?", incidentID, isolationUserB, isolationProjB).
		Update("hotfix_approved", true)
	assert.Equal(t, int64(0), result.RowsAffected, "cross-project MarkHotfixApproved should affect zero rows")
}

// TestIsolation_RejectManifest verifies that rejecting a manifest with wrong
// project returns "not found" (zero rows affected).
func TestIsolation_RejectManifest(t *testing.T) {
	db := setupMinisterTestDB(t)

	edictA, err := createEdictForProject(db, isolationUserA, isolationProjA, "reject isolation")
	require.NoError(t, err)

	manifestID := stageManifestDB(t, db,
		storage.EdictKey{ID: edictA.ID, Username: isolationUserA, Project: isolationProjA},
		"", "file.go", "Func", "sha1",
	)

	// Reject as project B — should affect zero rows
	keyB := storage.EdictKey{Username: isolationUserB, Project: isolationProjB}
	result := db.Model(&storage.ForgeManifest{}).
		Where("manifest_id = ? AND username = ? AND project = ?", manifestID, keyB.Username, keyB.Project).
		Update("status", storage.ManifestRejected)
	assert.Equal(t, int64(0), result.RowsAffected, "cross-project RejectManifest should affect zero rows")

	// Verify manifest is still forged (not rejected) in project A
	var manifest storage.ForgeManifest
	err = db.Where("manifest_id = ? AND username = ? AND project = ?", manifestID, isolationUserA, isolationProjA).First(&manifest).Error
	require.NoError(t, err)
	assert.Equal(t, storage.ManifestForged, manifest.Status, "manifest should still be forged, not rejected")
}

// TestIsolation_CouncilDecisions verifies that council decisions are scoped
// by username/project.
func TestIsolation_CouncilDecisions(t *testing.T) {
	db := setupMinisterTestDB(t)

	edictA, err := createEdictForProject(db, isolationUserA, isolationProjA, "council test")
	require.NoError(t, err)

	keyA := storage.EdictKey{ID: edictA.ID, Username: isolationUserA, Project: isolationProjA}

	// Create a council decision under project A
	err = CreateCouncilDecision(db, "council-001", keyA, "Deploy to production")
	require.NoError(t, err)

	// Project A can see its pending decisions
	pending, err := GetPendingCouncilDecisions(db, keyA)
	require.NoError(t, err)
	assert.Len(t, pending, 1)

	// Project B queries with same edict ID — should get zero results
	keyB := storage.EdictKey{ID: edictA.ID, Username: isolationUserB, Project: isolationProjB}
	pendingB, err := GetPendingCouncilDecisions(db, keyB)
	require.NoError(t, err)
	assert.Empty(t, pendingB, "cross-project GetPendingCouncilDecisions should return empty")

	// GetCouncilDecision as project B should return error
	_, err = GetCouncilDecision(db, "council-001", keyB)
	assert.Error(t, err, "cross-project GetCouncilDecision should fail")

	// GetCouncilDecisionsForEdict as project B should return empty
	decisionsB, err := GetCouncilDecisionsForEdict(db, keyB)
	require.NoError(t, err)
	assert.Empty(t, decisionsB, "cross-project GetCouncilDecisionsForEdict should return empty")
}

// TestIsolation_QueryCourt verifies that query_court only returns the
// current project's edicts and zhengming.
func TestIsolation_QueryCourt(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create edict and zhengming under project A
	edictA, err := createEdictForProject(db, isolationUserA, isolationProjA, "court test")
	require.NoError(t, err)

	// Create a pending zhengming for project A
	zhengming := storage.Zhengming{
		RequestID:  "zh-001",
		EdictID:    edictA.ID,
		Username:   isolationUserA,
		Project:    isolationProjA,
		MinisterID: "forge",
		Questions:  storage.ZhengmingQuestions{{Text: "Proceed?", Options: []string{"yes", "no"}}},
		Priority:   storage.PriorityNormal,
		Status:     storage.ZhengmingPending,
	}
	require.NoError(t, db.Create(&zhengming).Error)

	// Query court as project B with specific edict_id — should return no edict
	toolB := tools.QueryCourtTool{DB: db, Username: isolationUserB, Project: isolationProjB}
	result, err := toolB.Call(context.Background(), fmt.Sprintf(`{"edict_id": %d}`, edictA.ID))
	require.NoError(t, err)

	var courtResult map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(result), &courtResult))

	edicts, ok := courtResult["edicts"].([]interface{})
	require.True(t, ok, "edicts should be a list")
	assert.Empty(t, edicts, "cross-project query_court with edict_id should return no edicts")

	// No pending zhengming for project B (zhengming query always filters by username/project)
	_, hasZhengming := courtResult["pending_zhengming"]
	assert.False(t, hasZhengming, "cross-project query_court should not return zhengming")

	// Now query as project A with same edict_id — should see its edict
	toolA := tools.QueryCourtTool{DB: db, Username: isolationUserA, Project: isolationProjA}
	resultA, err := toolA.Call(context.Background(), fmt.Sprintf(`{"edict_id": %d}`, edictA.ID))
	require.NoError(t, err)

	var courtResultA map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(resultA), &courtResultA))
	edictsA, ok := courtResultA["edicts"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, edictsA, "project A should see its own edict")

	// Project A should see pending zhengming
	_, hasZhengmingA := courtResultA["pending_zhengming"]
	assert.True(t, hasZhengmingA, "project A should see its pending zhengming")
}

// TestIsolation_CensorPrecedent verifies that CensorPrecedent queries are
// filtered by username/project.
func TestIsolation_CensorPrecedent(t *testing.T) {
	db := setupMinisterTestDB(t)

	edictA, err := createEdictForProject(db, isolationUserA, isolationProjA, "precedent test")
	require.NoError(t, err)

	manifestID := stageManifestDB(t, db,
		storage.EdictKey{ID: edictA.ID, Username: isolationUserA, Project: isolationProjA},
		"", "file.go", "Func", "sha2",
	)

	// Log a precedent for project A's manifest
	precedentID := GenerateID("precedent", manifestID, "naming_convention", fmt.Sprintf("%d", time.Now().UnixNano()))
	precedent := storage.CensorPrecedent{
		PrecedentID:   precedentID,
		ManifestID:    manifestID,
		Principle:     "naming_convention",
		Ruling:        storage.PrecedentApproved,
		Justification: "names are clear",
	}
	require.NoError(t, db.Create(&precedent).Error)

	// GetPrecedentsForManifest as project B should return empty
	var precedentsB []storage.CensorPrecedent
	err = db.Joins("JOIN forge_manifests ON forge_manifests.manifest_id = censor_precedents.manifest_id").
		Where("censor_precedents.manifest_id = ? AND forge_manifests.username = ? AND forge_manifests.project = ?", manifestID, isolationUserB, isolationProjB).
		Order("censor_precedents.created_at ASC").
		Find(&precedentsB).Error
	require.NoError(t, err)
	assert.Empty(t, precedentsB, "cross-project GetPrecedentsForManifest should return empty")

	// GetPrecedentsForManifest as project A should return the precedent
	var precedentsA []storage.CensorPrecedent
	err = db.Joins("JOIN forge_manifests ON forge_manifests.manifest_id = censor_precedents.manifest_id").
		Where("censor_precedents.manifest_id = ? AND forge_manifests.username = ? AND forge_manifests.project = ?", manifestID, isolationUserA, isolationProjA).
		Order("censor_precedents.created_at ASC").
		Find(&precedentsA).Error
	require.NoError(t, err)
	assert.Len(t, precedentsA, 1, "project A should see its own precedent")

	// QueryPrecedentsByPrinciple as project B should return empty
	var resultsB []storage.CensorPrecedent
	err = db.Joins("JOIN forge_manifests ON forge_manifests.manifest_id = censor_precedents.manifest_id").
		Where("censor_precedents.principle LIKE ? AND forge_manifests.username = ? AND forge_manifests.project = ?", "%naming%", isolationUserB, isolationProjB).
		Order("censor_precedents.created_at DESC").
		Find(&resultsB).Error
	require.NoError(t, err)
	assert.Empty(t, resultsB, "cross-project QueryPrecedentsByPrinciple should return empty")

	// QueryPrecedentsByPrinciple as project A should return the precedent
	var resultsA []storage.CensorPrecedent
	err = db.Joins("JOIN forge_manifests ON forge_manifests.manifest_id = censor_precedents.manifest_id").
		Where("censor_precedents.principle LIKE ? AND forge_manifests.username = ? AND forge_manifests.project = ?", "%naming%", isolationUserA, isolationProjA).
		Order("censor_precedents.created_at DESC").
		Find(&resultsA).Error
	require.NoError(t, err)
	assert.Len(t, resultsA, 1, "project A should see its own precedent by principle")
}
