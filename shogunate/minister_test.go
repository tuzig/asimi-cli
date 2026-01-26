package shogunate

import (
	"context"
	"encoding/json"
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
	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)

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

	// Chancellor doesn't have an Execute method anymore - it receives tasks
	// The test now just verifies edict creation and phase transitions
}

func TestStrategist_DecomposeEdict(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Create edict
	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)
	chancellor.CreateEdict("test/repo#2", "Implement user authentication with login and logout")

	// Create strategist
	strategist := NewStrategist(base)

	// Execute planning (internal method)
	sealed, err := strategist.execute(ctx, "test/repo#2")
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
	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)
	chancellor.CreateEdict("test/repo#3", "Fix it")

	// Create strategist
	strategist := NewStrategist(base)

	// Execute - should request zhengming (internal method)
	sealed, err := strategist.execute(ctx, "test/repo#3")
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
	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)
	chancellor.CreateEdict("test/repo#4", "Test feature")

	forge := NewForge(base)
	manifestID, _ := forge.StageManifest("test/repo#4", "", "test.go", "TestFunc", "hash1")
	forge.ActivateManifest(manifestID, "commit123")

	// Create judge (no CI runner - will auto-pass)
	judge := NewJudge(base, nil)

	// Execute judgment (internal method)
	sealed, err := judge.execute(ctx, "test/repo#4")
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
	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)
	chancellor.CreateEdict("test/repo#5", "Review feature")

	forge := NewForge(base)
	manifestID, _ := forge.StageManifest("test/repo#5", "", "review.go", "ReviewFunc", "hash2")
	forge.ActivateManifest(manifestID, "commit456")

	judge := NewJudge(base, nil)
	verdictID, _ := judge.InsertVerdict(manifestID, "tests", storage.VerdictPassed, nil)
	judge.UpdateManifestStatus(manifestID, storage.ManifestQuenched, verdictID)

	// Create censor (no linter - will auto-approve)
	censor := NewCensor(base, nil)

	// Execute review (internal method)
	sealed, err := censor.execute(ctx, "test/repo#5")
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
	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)
	chancellor.CreateEdict("test/repo#6", "Production feature")

	forge := NewForge(base)
	manifestID, _ := forge.StageManifest("test/repo#6", "", "prod.go", "ProdFunc", "hash3")
	forge.ActivateManifest(manifestID, "prodcommit789")

	// Create marshal
	marshal := NewMarshal(base, nil)

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

	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)

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

// TestHappyFlowE2E tests the complete TaskEnvelope flow:
// Chancellor -> invoke_minister tool -> Minister receives task -> Minister replies (synchronous)
func TestHappyFlowE2E(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create Chancellor and Shogunate
	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)

	// Create a simple Shogunate with just Forge for this test
	shogunate := &Shogunate{
		db:         db,
		Chancellor: chancellor,
		Forge:      NewForge(base),
	}
	chancellor.SetShogunate(shogunate)

	// Create an edict for the test
	edictID := "test-e2e-edict"
	err := chancellor.CreateEdict(edictID, "E2E test edict")
	if err != nil {
		t.Fatalf("Failed to create edict: %v", err)
	}

	// Start the Forge's Run loop in a goroutine
	go shogunate.Forge.Run(ctx)

	// Create the InvokeMinisterTool
	tool := InvokeMinisterTool{chancellor: chancellor}

	// Invoke the Forge minister with a trivial task
	// With synchronous blocking, this call blocks until minister replies
	taskInput := `{"minister_id": "forge", "edict_id": "test-e2e-edict", "task": "please reply with 'hello world'"}`
	result, err := tool.Call(ctx, taskInput)
	if err != nil {
		t.Fatalf("Failed to invoke minister: %v", err)
	}

	// Verify the tool returned success with completed status
	if result == "" {
		t.Error("Expected non-empty result from invoke_minister")
	}
	t.Logf("invoke_minister result: %s", result)

	// Parse the result to verify it contains completion info
	var response struct {
		MinisterID string `json:"minister_id"`
		EdictID    string `json:"edict_id"`
		Status     string `json:"status"`
		Sealed     bool   `json:"sealed"`
		Output     string `json:"output"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if response.Status != "completed" {
		t.Errorf("Expected status 'completed', got %s", response.Status)
	}
	if response.MinisterID != "forge" {
		t.Errorf("Expected MinisterID 'forge', got %s", response.MinisterID)
	}
	if response.EdictID != edictID {
		t.Errorf("Expected EdictID %s, got %s", edictID, response.EdictID)
	}
	if !response.Sealed {
		t.Error("Expected Sealed=true from Forge")
	}
	t.Logf("Received response: minister=%s, edict=%s, sealed=%v, output=%s",
		response.MinisterID, response.EdictID, response.Sealed, response.Output)
}

// TestInvokeMinisterTool_InvalidMinister tests error handling for unknown ministers
func TestInvokeMinisterTool_InvalidMinister(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)
	shogunate := &Shogunate{
		db:         db,
		Chancellor: chancellor,
	}
	chancellor.SetShogunate(shogunate)

	tool := InvokeMinisterTool{chancellor: chancellor}

	// Try to invoke a non-existent minister
	taskInput := `{"minister_id": "unknown", "edict_id": "test", "task": "hello"}`
	_, err := tool.Call(ctx, taskInput)
	if err == nil {
		t.Error("Expected error for unknown minister")
	}
}

// TestInvokeMinisterTool_MissingTask tests error handling for missing task parameter
func TestInvokeMinisterTool_MissingTask(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	base := NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
	chancellor := NewChancellor(base)

	tool := InvokeMinisterTool{chancellor: chancellor}

	// Missing task parameter
	taskInput := `{"minister_id": "forge", "edict_id": "test"}`
	_, err := tool.Call(ctx, taskInput)
	if err == nil {
		t.Error("Expected error for missing task parameter")
	}
}
