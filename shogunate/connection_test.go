package shogunate

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "shogunate_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := tmpDir + "/test.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Run migrations
	err = db.AutoMigrate(
		&storage.Edict{},
		&storage.ZhengmingRequest{},
		&storage.TianEvent{},
		&storage.TianEventDLQ{},
		&storage.Ling{},
		&storage.ForgeManifest{},
		&storage.JudgeVerdict{},
		&storage.CensorPrecedent{},
		&storage.MarshalIncident{},
		&storage.RulerCouncil{},
		&storage.RitualGuardCheckpoint{},
	)
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	return db
}

func TestChancellor_EdictCreationAndSeals(t *testing.T) {
	db := setupTestDB(t)
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)

	// Create edict
	err := chancellor.CreateEdict("test/repo#1", "Fix the login bug")
	if err != nil {
		t.Fatalf("Failed to create edict: %v", err)
	}

	// Get edict
	edict, err := chancellor.GetEdict("test/repo#1")
	if err != nil {
		t.Fatalf("Failed to get edict: %v", err)
	}
	if edict.CurrentPhase != storage.PhaseBrewing {
		t.Errorf("Expected phase brewing, got %s", edict.CurrentPhase)
	}

	// Update phase
	err = chancellor.UpdatePhase("test/repo#1", storage.PhaseForging)
	if err != nil {
		t.Fatalf("Failed to update phase: %v", err)
	}

	edict, _ = chancellor.GetEdict("test/repo#1")
	if edict.CurrentPhase != storage.PhaseForging {
		t.Errorf("Expected phase forging, got %s", edict.CurrentPhase)
	}

	// Set seals
	err = chancellor.SetChancellorSeal("test/repo#1", true)
	if err != nil {
		t.Fatalf("Failed to set chancellor seal: %v", err)
	}

	err = chancellor.SetCensorSeal("test/repo#1", true)
	if err != nil {
		t.Fatalf("Failed to set censor seal: %v", err)
	}

	edict, _ = chancellor.GetEdict("test/repo#1")
	if !edict.ChancellorSeal || !edict.CensorSeal {
		t.Error("Seals not set correctly")
	}

	// Cancel edict
	err = chancellor.CancelEdict("test/repo#1", "@admin", "User requested cancellation")
	if err != nil {
		t.Fatalf("Failed to cancel edict: %v", err)
	}

	edict, _ = chancellor.GetEdict("test/repo#1")
	if edict.CurrentPhase != storage.PhaseCancelled {
		t.Errorf("Expected phase cancelled, got %s", edict.CurrentPhase)
	}
}

func TestChancellor_Zhengming(t *testing.T) {
	db := setupTestDB(t)
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)

	// Create edict first
	chancellor.CreateEdict("test/repo#2", "Add feature X")

	// Request zhengming
	requestID, err := chancellor.RequestZhengming("test/repo#2", "What should X do?", storage.PriorityNormal)
	if err != nil {
		t.Fatalf("Failed to request zhengming: %v", err)
	}

	// Check pending
	pending, err := chancellor.IsZhengmingPending("test/repo#2")
	if err != nil {
		t.Fatalf("Failed to check pending: %v", err)
	}
	if !pending {
		t.Error("Expected pending zhengming")
	}

	// Get pending requests
	requests, err := chancellor.GetPendingZhengming("test/repo#2")
	if err != nil {
		t.Fatalf("Failed to get pending zhengming: %v", err)
	}
	if len(requests) != 1 {
		t.Errorf("Expected 1 pending request, got %d", len(requests))
	}

	// Answer zhengming
	err = chancellor.AnswerZhengming(requestID, "X should do Y")
	if err != nil {
		t.Fatalf("Failed to answer zhengming: %v", err)
	}

	// Check no longer pending
	pending, _ = chancellor.IsZhengmingPending("test/repo#2")
	if pending {
		t.Error("Expected no pending zhengming after answering")
	}

	// Append to ren intent
	err = chancellor.AppendToRenIntent("test/repo#2", "X should do Y")
	if err != nil {
		t.Fatalf("Failed to append to ren intent: %v", err)
	}

	edict, _ := chancellor.GetEdict("test/repo#2")
	if edict.RenIntentVersion != 2 {
		t.Errorf("Expected version 2, got %d", edict.RenIntentVersion)
	}
}

func TestStrategistConn_Ling(t *testing.T) {
	db := setupTestDB(t)

	// Create edict via chancellor
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#3", "Build feature Y")

	// Use strategist to create ling
	strategist := NewStrategistConn(db)

	ling := &storage.Ling{
		EdictID:      "test/repo#3",
		Description:  "Create database schema",
		Dependencies: storage.StringArray{},
		Status:       storage.LingPending,
	}
	err := strategist.InsertLing(ling)
	if err != nil {
		t.Fatalf("Failed to insert ling: %v", err)
	}

	// Check ling exists
	exists, err := strategist.LingExistsForEdict("test/repo#3")
	if err != nil {
		t.Fatalf("Failed to check ling existence: %v", err)
	}
	if !exists {
		t.Error("Expected ling to exist")
	}

	// Get ling
	lingList, err := strategist.GetLingForEdict("test/repo#3")
	if err != nil {
		t.Fatalf("Failed to get ling: %v", err)
	}
	if len(lingList) != 1 {
		t.Errorf("Expected 1 ling, got %d", len(lingList))
	}
}

func TestForgeConn_Manifests(t *testing.T) {
	db := setupTestDB(t)

	// Setup
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#4", "Build feature Z")

	strategist := NewStrategistConn(db)
	ling := &storage.Ling{
		EdictID:     "test/repo#4",
		Description: "Implement handler",
		Status:      storage.LingPending,
	}
	strategist.InsertLing(ling)
	lingList, _ := strategist.GetLingForEdict("test/repo#4")
	lingID := lingList[0].LingID

	// Forge operations
	forge := NewForgeConn(db)

	// Get pending ling
	pendingLing, err := forge.GetPendingLing("test/repo#4")
	if err != nil {
		t.Fatalf("Failed to get pending ling: %v", err)
	}
	if len(pendingLing) != 1 {
		t.Errorf("Expected 1 pending ling, got %d", len(pendingLing))
	}

	// Stage manifest
	manifestID, err := forge.StageManifest("test/repo#4", lingID, "handler.go", "Handler", "patchhash123")
	if err != nil {
		t.Fatalf("Failed to stage manifest: %v", err)
	}

	// Activate manifest
	err = forge.ActivateManifest(manifestID, "abc123def")
	if err != nil {
		t.Fatalf("Failed to activate manifest: %v", err)
	}

	// Mark ling completed
	err = forge.MarkLingCompleted(lingID)
	if err != nil {
		t.Fatalf("Failed to mark ling completed: %v", err)
	}

	// Verify no pending ling
	pendingLing, _ = forge.GetPendingLing("test/repo#4")
	if len(pendingLing) != 0 {
		t.Errorf("Expected 0 pending ling, got %d", len(pendingLing))
	}
}

func TestJudgeConn_Verdicts(t *testing.T) {
	db := setupTestDB(t)

	// Setup: create manifest
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#5", "Test feature")

	forge := NewForgeConn(db)
	manifestID, _ := forge.StageManifest("test/repo#5", "", "test.go", "Test", "hash1")
	forge.ActivateManifest(manifestID, "commit123")

	// Judge operations
	judge := NewJudgeConn(db)

	// Get pending manifests
	pending, err := judge.GetPendingManifests("test/repo#5")
	if err != nil {
		t.Fatalf("Failed to get pending manifests: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("Expected 1 pending manifest, got %d", len(pending))
	}

	// Insert verdict
	evidence, _ := json.Marshal(map[string]interface{}{"tests": 10, "passed": 10})
	verdictID, err := judge.InsertVerdict(manifestID, "unit-tests", storage.VerdictPassed, storage.JSON(evidence))
	if err != nil {
		t.Fatalf("Failed to insert verdict: %v", err)
	}

	// Update manifest status
	err = judge.UpdateManifestStatus(manifestID, storage.ManifestQuenched, verdictID)
	if err != nil {
		t.Fatalf("Failed to update manifest status: %v", err)
	}

	// Check all quenched
	allQuenched, err := judge.AllManifestsQuenched("test/repo#5")
	if err != nil {
		t.Fatalf("Failed to check quenched status: %v", err)
	}
	if !allQuenched {
		t.Error("Expected all manifests to be quenched")
	}
}

func TestCensorConn_Precedents(t *testing.T) {
	db := setupTestDB(t)

	// Setup: create quenched manifest
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#6", "Ethics test")

	forge := NewForgeConn(db)
	manifestID, _ := forge.StageManifest("test/repo#6", "", "ethics.go", "Ethics", "hash2")
	forge.ActivateManifest(manifestID, "commit456")

	judge := NewJudgeConn(db)
	verdictID, _ := judge.InsertVerdict(manifestID, "tests", storage.VerdictPassed, nil)
	judge.UpdateManifestStatus(manifestID, storage.ManifestQuenched, verdictID)

	// Censor operations
	censor := NewCensorConn(db)

	// Get quenched manifests
	quenched, err := censor.GetQuenchedManifests("test/repo#6")
	if err != nil {
		t.Fatalf("Failed to get quenched manifests: %v", err)
	}
	if len(quenched) != 1 {
		t.Errorf("Expected 1 quenched manifest, got %d", len(quenched))
	}

	// Log precedent (waive)
	precedentID, err := censor.LogPrecedent(manifestID, "golangci:govet", storage.RulingWaive, "False positive")
	if err != nil {
		t.Fatalf("Failed to log precedent: %v", err)
	}

	// Get precedents
	precedents, err := censor.GetPrecedentsForManifest(manifestID)
	if err != nil {
		t.Fatalf("Failed to get precedents: %v", err)
	}
	if len(precedents) != 1 || precedents[0].PrecedentID != precedentID {
		t.Error("Precedent not found correctly")
	}

	// Query by principle
	found, err := censor.QueryPrecedentsByPrinciple("govet", 10)
	if err != nil {
		t.Fatalf("Failed to query precedents: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("Expected 1 precedent, got %d", len(found))
	}

	// Check no rejections
	noReject, _ := censor.NoRejections("test/repo#6")
	if !noReject {
		t.Error("Expected no rejections")
	}

	// Reject manifest
	err = censor.RejectManifest(manifestID)
	if err != nil {
		t.Fatalf("Failed to reject manifest: %v", err)
	}

	noReject, _ = censor.NoRejections("test/repo#6")
	if noReject {
		t.Error("Expected rejections after reject")
	}
}

func TestMarshalConn_Incidents(t *testing.T) {
	db := setupTestDB(t)

	// Setup
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#7", "Production issue")

	forge := NewForgeConn(db)
	manifestID, _ := forge.StageManifest("test/repo#7", "", "prod.go", "Prod", "hash3")
	forge.ActivateManifest(manifestID, "prodcommit789")

	// Marshal operations
	marshal := NewMarshalConn(db)

	// Get manifest by commit
	manifest, err := marshal.GetManifestByCommit("prodcommit789")
	if err != nil {
		t.Fatalf("Failed to get manifest by commit: %v", err)
	}
	if manifest.ManifestID != manifestID {
		t.Error("Wrong manifest returned")
	}

	// Log incident
	err = marshal.LogIncident("sentry-123", "test/repo#7", "prodcommit789", "Null pointer exception in handler")
	if err != nil {
		t.Fatalf("Failed to log incident: %v", err)
	}

	// Get incident
	incident, err := marshal.GetIncident("sentry-123")
	if err != nil {
		t.Fatalf("Failed to get incident: %v", err)
	}
	if incident.CommitHash != "prodcommit789" {
		t.Error("Wrong incident data")
	}

	// Approve hotfix
	err = marshal.MarkHotfixApproved("sentry-123")
	if err != nil {
		t.Fatalf("Failed to approve hotfix: %v", err)
	}

	incident, _ = marshal.GetIncident("sentry-123")
	if !incident.HotfixApproved {
		t.Error("Hotfix not approved")
	}
}

func TestRitualGuardConn_Events(t *testing.T) {
	db := setupTestDB(t)

	// Create checkpoint table manually for this test
	db.Exec(`CREATE TABLE IF NOT EXISTS ritual_guard_checkpoint (id INTEGER PRIMARY KEY, event_id INTEGER NOT NULL, updated_at DATETIME)`)

	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#8", "Event test")

	// Emit events
	payload, _ := json.Marshal(map[string]string{"action": "created"})
	chancellor.EmitEvent("test/repo#8", "edict_created", storage.JSON(payload))
	chancellor.EmitEvent("test/repo#8", "phase_changed", storage.JSON(payload))

	// Ritual Guard operations
	guard := NewRitualGuardConn(db)

	// Get events from start
	events, err := guard.GetEventsFrom(0, 10)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	// Save checkpoint
	err = guard.SaveCheckpoint(events[0].EventID)
	if err != nil {
		t.Fatalf("Failed to save checkpoint: %v", err)
	}

	// Load checkpoint
	checkpoint, err := guard.LoadCheckpoint()
	if err != nil {
		t.Fatalf("Failed to load checkpoint: %v", err)
	}
	if checkpoint != events[0].EventID {
		t.Errorf("Expected checkpoint %d, got %d", events[0].EventID, checkpoint)
	}

	// Move to DLQ
	err = guard.MoveToDLQ(events[1], "processing failed", 1)
	if err != nil {
		t.Fatalf("Failed to move to DLQ: %v", err)
	}

	// Verify DLQ entry
	var dlqCount int64
	db.Model(&storage.TianEventDLQ{}).Count(&dlqCount)
	if dlqCount != 1 {
		t.Errorf("Expected 1 DLQ entry, got %d", dlqCount)
	}
}

func TestCouncil_SeedAndQuery(t *testing.T) {
	db := setupTestDB(t)

	// Seed council
	members := []CouncilMember{
		{Username: "@lead", CanOverride: true, EscalationOrder: 1},
		{Username: "@senior", CanOverride: true, EscalationOrder: 2},
		{Username: "@oncall", CanOverride: false, EscalationOrder: 3},
	}
	err := SeedRulerCouncil(db, members)
	if err != nil {
		t.Fatalf("Failed to seed council: %v", err)
	}

	// Get active members
	active, err := GetActiveCouncilMembers(db)
	if err != nil {
		t.Fatalf("Failed to get active members: %v", err)
	}
	if len(active) != 3 {
		t.Errorf("Expected 3 active members, got %d", len(active))
	}

	// Get override members
	overriders, err := GetOverrideMembers(db)
	if err != nil {
		t.Fatalf("Failed to get override members: %v", err)
	}
	if len(overriders) != 2 {
		t.Errorf("Expected 2 override members, got %d", len(overriders))
	}

	// Deactivate member
	err = DeactivateCouncilMember(db, "@oncall")
	if err != nil {
		t.Fatalf("Failed to deactivate member: %v", err)
	}

	active, _ = GetActiveCouncilMembers(db)
	if len(active) != 2 {
		t.Errorf("Expected 2 active members after deactivation, got %d", len(active))
	}
}
