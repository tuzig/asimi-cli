package storage

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

func TestShogunateSchema_AutoMigrate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "shogunate_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := tmpDir + "/test.db"

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	// Verify all Shogunate tables exist
	tables := []string{
		"edicts",
		"zhengming_requests",
		"tian_events",
		"tian_events_dlq",
		"ling",
		"forge_manifests",
		"judge_verdicts",
		"censor_precedents",
		"marshal_incidents",
		"ruler_council",
		"ritual_guard_checkpoint",
	}

	for _, table := range tables {
		var count int64
		if err := db.Conn().Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count).Error; err != nil {
			t.Errorf("Failed to check table %s: %v", table, err)
		}
		if count == 0 {
			t.Errorf("Table %s does not exist", table)
		}
	}

	// Verify schema version matches current
	var record SchemaVersionRecord
	if err := db.Conn().First(&record).Error; err != nil {
		t.Fatalf("Failed to read schema version: %v", err)
	}
	if record.Version != SchemaVersion {
		t.Errorf("Expected schema version %d, got %d", SchemaVersion, record.Version)
	}
}

func TestShogunateSchema_EdictLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "shogunate_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := tmpDir + "/test.db"

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	// Create an edict
	edict := Edict{
		EdictID:      "test/repo#123",
		RenIntent:    "Fix the bug in the login system",
		CurrentPhase: PhasePlanning,
	}
	if err := db.Conn().Create(&edict).Error; err != nil {
		t.Fatalf("Failed to create edict: %v", err)
	}

	// Verify edict was created
	var loadedEdict Edict
	if err := db.Conn().First(&loadedEdict, "edict_id = ?", edict.EdictID).Error; err != nil {
		t.Fatalf("Failed to load edict: %v", err)
	}

	if loadedEdict.EdictID != edict.EdictID {
		t.Errorf("Expected edict_id %s, got %s", edict.EdictID, loadedEdict.EdictID)
	}
	if loadedEdict.CurrentPhase != PhasePlanning {
		t.Errorf("Expected phase %s, got %s", PhasePlanning, loadedEdict.CurrentPhase)
	}

	// Create a zhengming request
	zhreq := ZhengmingRequest{
		RequestID:  "zhreq-1",
		EdictID:    edict.EdictID,
		MinisterID: "strategist",
		Question:   "What is the expected behavior?",
		Priority:   PriorityNormal,
		Status:     ZhengmingPending,
		TimeoutAt:  time.Now().Add(24 * time.Hour),
	}
	if err := db.Conn().Create(&zhreq).Error; err != nil {
		t.Fatalf("Failed to create zhengming request: %v", err)
	}

	// Create ling for the edict
	ling := Ling{
		LingID:         "ling-1",
		EdictID:        edict.EdictID,
		Description:    "Implement login fix",
		Dependencies:   StringArray{},
		Status:         LingPending,
		IdempotencyKey: "hash-123",
	}
	if err := db.Conn().Create(&ling).Error; err != nil {
		t.Fatalf("Failed to create ling: %v", err)
	}

	// Create a forge manifest
	manifest := ForgeManifest{
		ManifestID:     "manifest-1",
		EdictID:        edict.EdictID,
		LingID:         ling.LingID,
		CommitHash:     "abc123",
		FilePath:       "login.go",
		QualifiedName:  "LoginService",
		Status:         ManifestQuenched,
		IdempotencyKey: "hash-456",
	}
	if err := db.Conn().Create(&manifest).Error; err != nil {
		t.Fatalf("Failed to create manifest: %v", err)
	}

	// Create a judge verdict
	verdict := JudgeVerdict{
		VerdictID:      "verdict-1",
		ManifestID:     manifest.ManifestID,
		TestSuite:      "unit-tests",
		Outcome:        VerdictPassed,
		Evidence:       JSON([]byte(`{"passed": 10, "failed": 0}`)),
		IdempotencyKey: "hash-789",
	}
	if err := db.Conn().Create(&verdict).Error; err != nil {
		t.Fatalf("Failed to create verdict: %v", err)
	}

	// Create a censor precedent
	precedent := CensorPrecedent{
		PrecedentID:    "precedent-1",
		ManifestID:     manifest.ManifestID,
		Principle:      "golangci:govet",
		Ruling:         RulingWaive,
		Justification:  "False positive",
		IdempotencyKey: "hash-101",
	}
	if err := db.Conn().Create(&precedent).Error; err != nil {
		t.Fatalf("Failed to create precedent: %v", err)
	}

	// Create a marshal incident
	incident := MarshalIncident{
		IncidentID:  "incident-1",
		EdictID:     edict.EdictID,
		CommitHash:  "abc123",
		RCASummary:  "Panic in login.go",
	}
	if err := db.Conn().Create(&incident).Error; err != nil {
		t.Fatalf("Failed to create incident: %v", err)
	}

	// Create a ruler council member
	council := RulerCouncil{
		Username:        "@test-lead",
		IsActive:        true,
		CanOverride:     true,
		EscalationOrder: 1,
	}
	if err := db.Conn().Create(&council).Error; err != nil {
		t.Fatalf("Failed to create council member: %v", err)
	}

	// Create a tian event
	event := TianEvent{
		EdictID:   edict.EdictID,
		EventType: "edict_assigned",
		Payload:   JSON([]byte(`{"test": "data"}`)),
	}
	if err := db.Conn().Create(&event).Error; err != nil {
		t.Fatalf("Failed to create tian event: %v", err)
	}

	t.Logf("Successfully created and verified all Shogunate entities")
}

func TestShogunateSchema_StringArray(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "shogunate_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := tmpDir + "/test.db"

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	// Create edict with ling that has dependencies
	edict := Edict{
		EdictID:      "test/repo#456",
		RenIntent:    "Multi-task edict",
		CurrentPhase: PhasePlanning,
	}
	if err := db.Conn().Create(&edict).Error; err != nil {
		t.Fatalf("Failed to create edict: %v", err)
	}

	// Create ling with dependencies
	ling := Ling{
		LingID:         "ling-2",
		EdictID:        edict.EdictID,
		Description:    "Dependent task",
		Dependencies:   StringArray{"ling-1", "ling-3"},
		Status:         LingPending,
		IdempotencyKey: "hash-dep",
	}
	if err := db.Conn().Create(&ling).Error; err != nil {
		t.Fatalf("Failed to create ling: %v", err)
	}

	// Load and verify dependencies
	var loadedLing Ling
	if err := db.Conn().First(&loadedLing, "ling_id = ?", ling.LingID).Error; err != nil {
		t.Fatalf("Failed to load ling: %v", err)
	}

	if len(loadedLing.Dependencies) != 2 {
		t.Errorf("Expected 2 dependencies, got %d", len(loadedLing.Dependencies))
	}
	if loadedLing.Dependencies[0] != "ling-1" || loadedLing.Dependencies[1] != "ling-3" {
		t.Errorf("Dependencies not preserved: %v", loadedLing.Dependencies)
	}

	t.Logf("StringArray serialization works correctly: %v", loadedLing.Dependencies)
}

func TestShogunateSchema_JSONType(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "shogunate_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := tmpDir + "/test.db"

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	// Create edict and manifest
	edict := Edict{
		EdictID:      "test/repo#789",
		RenIntent:    "JSON test",
		CurrentPhase: PhaseForging,
	}
	if err := db.Conn().Create(&edict).Error; err != nil {
		t.Fatalf("Failed to create edict: %v", err)
	}

	// Create manifest with JSON evidence
	evidence := map[string]interface{}{
		"test_suite": "integration",
		"duration_ms": 1234,
		"passed":      true,
	}
	evidenceJSON, _ := json.Marshal(evidence)

	manifest := ForgeManifest{
		ManifestID:     "manifest-2",
		EdictID:        edict.EdictID,
		CommitHash:     "xyz789",
		FilePath:       "api.go",
		QualifiedName:  "APIHandler",
		Status:         ManifestPending,
		IdempotencyKey: "hash-json",
	}
	if err := db.Conn().Create(&manifest).Error; err != nil {
		t.Fatalf("Failed to create manifest: %v", err)
	}

	verdict := JudgeVerdict{
		VerdictID:      "verdict-2",
		ManifestID:     manifest.ManifestID,
		TestSuite:      "integration-tests",
		Outcome:        VerdictFailed,
		Evidence:       JSON(evidenceJSON),
		IdempotencyKey: "hash-json-2",
	}
	if err := db.Conn().Create(&verdict).Error; err != nil {
		t.Fatalf("Failed to create verdict: %v", err)
	}

	// Load and verify JSON
	var loadedVerdict JudgeVerdict
	if err := db.Conn().First(&loadedVerdict, "verdict_id = ?", verdict.VerdictID).Error; err != nil {
		t.Fatalf("Failed to load verdict: %v", err)
	}

	// Parse the JSON evidence
	var parsedEvidence map[string]interface{}
	if err := json.Unmarshal(loadedVerdict.Evidence, &parsedEvidence); err != nil {
		t.Fatalf("Failed to unmarshal evidence: %v", err)
	}

	if parsedEvidence["duration_ms"].(float64) != 1234 {
		t.Errorf("Evidence not preserved correctly")
	}

	t.Logf("JSON type serialization works correctly: %v", parsedEvidence)
}
