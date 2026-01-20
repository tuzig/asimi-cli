package shogunate

import (
	"context"
	"os"
	"testing"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupMinisterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "minister_test")
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

	// Create checkpoint table
	db.Exec(`CREATE TABLE IF NOT EXISTS ritual_guard_checkpoint (id INTEGER PRIMARY KEY, event_id INTEGER NOT NULL, updated_at DATETIME)`)

	return db
}

func TestChancellor_EdictLifecycle(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Create chancellor
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)

	// Create an edict (starts in brewing phase)
	err := chancellor.CreateEdictFromIssue(ctx, "test/repo#1", "Add a simple hello world function")
	if err != nil {
		t.Fatalf("Failed to create edict: %v", err)
	}

	// Verify edict was created in brewing phase
	edict, err := chancellor.GetEdict("test/repo#1")
	if err != nil {
		t.Fatalf("Failed to get edict: %v", err)
	}
	if edict.CurrentPhase != storage.PhaseBrewing {
		t.Errorf("Expected phase brewing, got %s", edict.CurrentPhase)
	}

	// Transition to planning
	err = chancellor.UpdatePhase("test/repo#1", storage.PhasePlanning)
	if err != nil {
		t.Fatalf("Failed to update phase to planning: %v", err)
	}

	// Verify phase transition
	edict, _ = chancellor.GetEdict("test/repo#1")
	if edict.CurrentPhase != storage.PhasePlanning {
		t.Errorf("Expected phase planning, got %s", edict.CurrentPhase)
	}

	// Execute stub returns (false, nil)
	sealed, err := chancellor.Execute(ctx, "test/repo#1")
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	if sealed {
		t.Error("Expected not sealed from stub Execute")
	}
}

func TestStrategist_DecomposeEdict(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Create edict
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#2", "Implement user authentication with login and logout")

	// Create strategist
	strategist := NewStrategist(db, nil, nil)

	// Execute planning
	sealed, err := strategist.Execute(ctx, "test/repo#2")
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	if !sealed {
		t.Error("Expected sealed after decomposition")
	}

	// Check ling was created
	ling, err := strategist.GetLingForEdict("test/repo#2")
	if err != nil {
		t.Fatalf("Failed to get ling: %v", err)
	}
	if len(ling) == 0 {
		t.Error("Expected at least one ling")
	}
}

func TestStrategist_AmbiguousIntent(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Create edict with ambiguous intent
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#3", "Fix it")

	// Create strategist
	strategist := NewStrategist(db, nil, nil)

	// Execute - should request zhengming
	sealed, err := strategist.Execute(ctx, "test/repo#3")
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	if sealed {
		t.Error("Expected not sealed for ambiguous intent")
	}

	// Check zhengming was requested
	pending, _ := strategist.IsZhengmingPending("test/repo#3")
	if !pending {
		t.Error("Expected pending zhengming")
	}
}

func TestJudge_VerdictFlow(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Setup: create edict and manifest
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#4", "Test feature")

	forge := NewForge(db, nil, nil)
	manifestID, _ := forge.StageManifest("test/repo#4", "", "test.go", "TestFunc", "hash1")
	forge.ActivateManifest(manifestID, "commit123")

	// Create judge (no CI runner - will auto-pass)
	judge := NewJudge(db, nil, nil)

	// Execute judgment
	sealed, err := judge.Execute(ctx, "test/repo#4")
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	if !sealed {
		t.Error("Expected sealed after judgment")
	}

	// Check manifest is quenched
	allQuenched, _ := judge.AllManifestsQuenched("test/repo#4")
	if !allQuenched {
		t.Error("Expected all manifests quenched")
	}
}

func TestCensor_ReviewFlow(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Setup: create quenched manifest
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#5", "Review feature")

	forge := NewForge(db, nil, nil)
	manifestID, _ := forge.StageManifest("test/repo#5", "", "review.go", "ReviewFunc", "hash2")
	forge.ActivateManifest(manifestID, "commit456")

	judge := NewJudge(db, nil, nil)
	verdictID, _ := judge.InsertVerdict(manifestID, "tests", storage.VerdictPassed, nil)
	judge.UpdateManifestStatus(manifestID, storage.ManifestQuenched, verdictID)

	// Create censor (no linter - will auto-approve)
	censor := NewCensor(db, nil, nil)

	// Execute review
	sealed, err := censor.Execute(ctx, "test/repo#5")
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	if !sealed {
		t.Error("Expected sealed after review")
	}

	// Check no rejections
	noReject, _ := censor.NoRejections("test/repo#5")
	if !noReject {
		t.Error("Expected no rejections")
	}
}

func TestMarshal_IncidentFlow(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Setup: create edict and manifest
	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor.CreateEdict("test/repo#6", "Production feature")

	forge := NewForge(db, nil, nil)
	manifestID, _ := forge.StageManifest("test/repo#6", "", "prod.go", "ProdFunc", "hash3")
	forge.ActivateManifest(manifestID, "prodcommit789")

	// Create marshal
	marshal := NewMarshal(db, nil, nil)

	// Report incident
	err := marshal.OnIncident(ctx, "sentry-456", "prodcommit789")
	if err != nil {
		t.Fatalf("Failed to handle incident: %v", err)
	}

	// Check incident was logged
	incident, err := marshal.GetIncident("sentry-456")
	if err != nil {
		t.Fatalf("Failed to get incident: %v", err)
	}
	if incident == nil {
		t.Error("Expected incident to be logged")
	}
}

func TestChancellor_CancelEdict(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	chancellor := NewChancellor(db, nil, nil, repo.RepoInfo{}, nil)

	// Create and cancel edict
	chancellor.CreateEdictFromIssue(ctx, "test/repo#7", "Feature to cancel")
	err := chancellor.CancelEdictWithContext(ctx, "test/repo#7", "@user", "No longer needed")
	if err != nil {
		t.Fatalf("Failed to cancel: %v", err)
	}

	// Check cancelled
	edict, _ := chancellor.GetEdict("test/repo#7")
	if edict.CurrentPhase != storage.PhaseCancelled {
		t.Errorf("Expected phase cancelled, got %s", edict.CurrentPhase)
	}
}

func TestStrategist_CircularDependencyDetection(t *testing.T) {
	strategist := &Strategist{}

	// Create ling with circular dependency
	ling := []storage.Ling{
		{LingID: "a", Dependencies: storage.StringArray{"b"}},
		{LingID: "b", Dependencies: storage.StringArray{"c"}},
		{LingID: "c", Dependencies: storage.StringArray{"a"}}, // Circular!
	}

	err := strategist.validateDependencies(ling)
	if err == nil {
		t.Error("Expected error for circular dependency")
	}
}

func TestStrategist_ValidDependencies(t *testing.T) {
	strategist := &Strategist{}

	// Create ling with valid DAG
	ling := []storage.Ling{
		{LingID: "a", Dependencies: storage.StringArray{}},
		{LingID: "b", Dependencies: storage.StringArray{"a"}},
		{LingID: "c", Dependencies: storage.StringArray{"a", "b"}},
	}

	err := strategist.validateDependencies(ling)
	if err != nil {
		t.Errorf("Expected no error for valid dependencies: %v", err)
	}
}
