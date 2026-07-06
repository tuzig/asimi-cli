package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/afittestide/asimi/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupPrecedentTestDB creates an in-memory SQLite DB with the right schema.
func setupPrecedentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	if err := db.AutoMigrate(&storage.ForgeManifest{}, &storage.CensorPrecedent{}, &storage.Seal{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func TestRecordPrecedentTool_NoReasoningEcho(t *testing.T) {
	db := setupPrecedentTestDB(t)
	// Insert a quenched manifest
	db.Create(&storage.ForgeManifest{
		ManifestID: "abc123",
		EdictID:    5,
		Username:   "testuser",
		Project:    "testproject",
		Status:     storage.ManifestQuenched,
	})

	tool := RecordPrecedentTool{
		Ctx: ToolContext{
			Username: "testuser",
			Project:  "testproject",
			DB:       db,
		},
	}

	longReasoning := "This is a very long reasoning that should NOT appear in the tool output because the Sage already wrote it as conversational text."
	result, err := tool.Call(context.Background(),
		`{"edict_id": 5, "approved": true, "reasoning": "`+longReasoning+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result, longReasoning) {
		t.Errorf("result should not echo reasoning, got: %s", result)
	}

	want := "Recorded precedent (approved) for edict 5"
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
	}
}

func TestRecordPrecedentTool_RejectedNoReasoningEcho(t *testing.T) {
	db := setupPrecedentTestDB(t)
	db.Create(&storage.ForgeManifest{
		ManifestID: "abc123",
		EdictID:    5,
		Username:   "testuser",
		Project:    "testproject",
		Status:     storage.ManifestQuenched,
	})

	tool := RecordPrecedentTool{
		Ctx: ToolContext{
			Username: "testuser",
			Project:  "testproject",
			DB:       db,
		},
	}

	longReasoning := "Code has issues that need addressing."
	result, err := tool.Call(context.Background(),
		`{"edict_id": 5, "approved": false, "reasoning": "`+longReasoning+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result, longReasoning) {
		t.Errorf("result should not echo reasoning, got: %s", result)
	}

	want := "Recorded precedent (rejected) for edict 5"
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
	}
}

func TestRecordPrecedentTool_Format(t *testing.T) {
	tool := RecordPrecedentTool{}
	formatted := tool.Format("", "Recorded precedent (approved) for edict 5", nil)
	want := "Record Precedent: Recorded precedent (approved) for edict 5\n"
	if formatted != want {
		t.Errorf("Format() = %q, want %q", formatted, want)
	}
}

func TestRecordPrecedentTool_GrantsSageSealOnApproval(t *testing.T) {
	db := setupPrecedentTestDB(t)
	db.Create(&storage.ForgeManifest{
		ManifestID: "m1",
		EdictID:    7,
		Username:   "testuser",
		Project:    "testproject",
		Status:     storage.ManifestQuenched,
	})

	tool := RecordPrecedentTool{
		Ctx: ToolContext{
			Username: "testuser",
			Project:  "testproject",
			DB:       db,
		},
	}

	_, err := tool.Call(context.Background(),
		`{"edict_id": 7, "approved": true, "reasoning": "LGTM"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify sage seal was created
	var seal storage.Seal
	if err := db.Where("edict_id = ? AND minister_id = ?", 7, "sage").First(&seal).Error; err != nil {
		t.Errorf("expected sage seal to be granted: %v", err)
	}
}

func TestRecordPrecedentTool_RejectsManifestOnRejection(t *testing.T) {
	db := setupPrecedentTestDB(t)
	db.Create(&storage.ForgeManifest{
		ManifestID: "m1",
		EdictID:    9,
		Username:   "testuser",
		Project:    "testproject",
		Status:     storage.ManifestQuenched,
	})

	tool := RecordPrecedentTool{
		Ctx: ToolContext{
			Username: "testuser",
			Project:  "testproject",
			DB:       db,
		},
	}

	_, err := tool.Call(context.Background(),
		`{"edict_id": 9, "approved": false, "reasoning": "bad code"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify manifest was rejected
	var manifest storage.ForgeManifest
	db.Where("manifest_id = ?", "m1").First(&manifest)
	if manifest.Status != storage.ManifestRejected {
		t.Errorf("expected manifest status rejected, got %s", manifest.Status)
	}
}
