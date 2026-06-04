package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

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

// TestIsolation_MarshalIncident verifies that MarshalIncident queries with
// wrong username/project return no results.
func TestIsolation_MarshalIncident(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create an incident under project A
	baseA := NewMinisterBase(db, nil, nil, isolationUserA, isolationProjA)
	marshalA := NewMarshal(baseA, nil)
	edictA, err := createEdictForProject(db, isolationUserA, isolationProjA, "incident test")
	require.NoError(t, err)

	keyA := storage.EdictKey{ID: edictA.ID, Username: isolationUserA, Project: isolationProjA}
	err = marshalA.LogIncident("inc-001", keyA, "deadbeef", "segfault in main")
	require.NoError(t, err)

	// Verify project A can see its own incident
	incident, err := marshalA.GetIncident("inc-001", isolationUserA, isolationProjA)
	require.NoError(t, err)
	assert.NotNil(t, incident)

	// Query as project B — should get "not found"
	_, err = marshalA.GetIncident("inc-001", isolationUserB, isolationProjB)
	assert.Error(t, err, "cross-project GetIncident should return error")
	assert.Contains(t, err.Error(), "not found")

	// GetPendingIncidents as project B should return zero
	pending, err := marshalA.GetPendingIncidents(isolationUserB, isolationProjB)
	require.NoError(t, err)
	assert.Empty(t, pending, "cross-project GetPendingIncidents should return empty")

	// MarkHotfixApproved as project B should affect zero rows
	err = marshalA.MarkHotfixApproved("inc-001", isolationUserB, isolationProjB)
	assert.Error(t, err, "cross-project MarkHotfixApproved should return error")
	assert.Contains(t, err.Error(), "not found")
}

// TestIsolation_RejectManifest verifies that RejectManifest with wrong
// project returns "not found".
func TestIsolation_RejectManifest(t *testing.T) {
	db := setupMinisterTestDB(t)

	baseA := NewMinisterBase(db, nil, nil, isolationUserA, isolationProjA)
	sageA := NewSage(baseA, nil)
	edictA, err := createEdictForProject(db, isolationUserA, isolationProjA, "reject isolation")
	require.NoError(t, err)

	forgeA := NewForge(baseA)
	manifestID, err := forgeA.StageManifest(
		storage.EdictKey{ID: edictA.ID, Username: isolationUserA, Project: isolationProjA},
		"", "file.go", "Func", "sha1",
	)
	require.NoError(t, err)

	// Reject as project B — should return "not found"
	keyB := storage.EdictKey{Username: isolationUserB, Project: isolationProjB}
	err = sageA.RejectManifest(keyB, manifestID)
	assert.Error(t, err, "cross-project RejectManifest should fail")
	assert.Contains(t, err.Error(), "not found")

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

	baseA := NewMinisterBase(db, nil, nil, isolationUserA, isolationProjA)
	sageA := NewSage(baseA, nil)

	edictA, err := createEdictForProject(db, isolationUserA, isolationProjA, "precedent test")
	require.NoError(t, err)

	forgeA := NewForge(baseA)
	manifestID, err := forgeA.StageManifest(
		storage.EdictKey{ID: edictA.ID, Username: isolationUserA, Project: isolationProjA},
		"", "file.go", "Func", "sha2",
	)
	require.NoError(t, err)

	// Log a precedent for project A's manifest
	_, err = sageA.LogPrecedent(manifestID, "naming_convention", storage.PrecedentApproved, "names are clear")
	require.NoError(t, err)

	// GetPrecedentsForManifest as project B should return empty
	precedentsB, err := sageA.GetPrecedentsForManifest(isolationUserB, isolationProjB, manifestID)
	require.NoError(t, err)
	assert.Empty(t, precedentsB, "cross-project GetPrecedentsForManifest should return empty")

	// GetPrecedentsForManifest as project A should return the precedent
	precedentsA, err := sageA.GetPrecedentsForManifest(isolationUserA, isolationProjA, manifestID)
	require.NoError(t, err)
	assert.Len(t, precedentsA, 1, "project A should see its own precedent")

	// QueryPrecedentsByPrinciple as project B should return empty
	resultsB, err := sageA.QueryPrecedentsByPrinciple(isolationUserB, isolationProjB, "naming", 10)
	require.NoError(t, err)
	assert.Empty(t, resultsB, "cross-project QueryPrecedentsByPrinciple should return empty")

	// QueryPrecedentsByPrinciple as project A should return the precedent
	resultsA, err := sageA.QueryPrecedentsByPrinciple(isolationUserA, isolationProjA, "naming", 10)
	require.NoError(t, err)
	assert.Len(t, resultsA, 1, "project A should see its own precedent by principle")
}
