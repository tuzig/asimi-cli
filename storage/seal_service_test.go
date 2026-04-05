package storage

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSealServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	if err := db.AutoMigrate(&Edict{}, &Seal{}, &Zhengming{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db
}

func TestListActiveEdicts_ORSemantics(t *testing.T) {
	db := setupSealServiceTestDB(t)
	svc := NewSealService(db)

	// Create test edicts
	edicts := []Edict{
		{ID: 1, Username: "testuser", Project: "testproject", Intent: "Active edict (un-cancelled, unsealed)"},
		{ID: 2, Username: "testuser", Project: "testproject", Intent: "Cancelled but unsealed (NEW: should be included)"},
		{ID: 3, Username: "testuser", Project: "testproject", Intent: "Sealed but un-cancelled (NEW: should be included)"},
		{ID: 4, Username: "testuser", Project: "testproject", Intent: "Sealed and cancelled (should NOT be included)"},
	}
	for i := range edicts {
		if err := db.Create(&edicts[i]).Error; err != nil {
			t.Fatalf("failed to create edict: %v", err)
		}
	}

	// Cancel edict 2
	now := time.Now()
	db.Model(&Edict{}).Where("id = ?", 2).Update("cancelled_at", &now)

	// Seal edict 3 with all three seals (judge, sage, ruler)
	for _, ministerID := range []string{"judge", "sage", "ruler"} {
		if err := svc.GrantSeal(edicts[2].Key(), ministerID, JSON{}); err != nil {
			t.Fatalf("failed to grant seal: %v", err)
		}
	}

	// Seal edict 4 with all three seals
	for _, ministerID := range []string{"judge", "sage", "ruler"} {
		if err := svc.GrantSeal(edicts[3].Key(), ministerID, JSON{}); err != nil {
			t.Fatalf("failed to grant seal: %v", err)
		}
	}
	// Then cancel edict 4
	db.Model(&Edict{}).Where("id = ?", 4).Update("cancelled_at", &now)

	// Test ListActiveEdicts - should return edicts 1, 2, 3 (not 4)
	activeEdicts, err := svc.ListActiveEdicts("testuser", "testproject")
	if err != nil {
		t.Fatalf("ListActiveEdicts failed: %v", err)
	}

	if len(activeEdicts) != 3 {
		t.Errorf("Expected 3 active edicts, got %d", len(activeEdicts))
	}

	// Build a map of returned edict IDs for easy checking
	returnedIDs := make(map[uint]bool)
	for _, e := range activeEdicts {
		returnedIDs[e.ID] = true
	}

	// Edict 1 should be included (un-cancelled, unsealed)
	if !returnedIDs[1] {
		t.Error("Expected edict 1 (active) to be included")
	}

	// Edict 2 should be included (cancelled but unsealed) - NEW OR BEHAVIOR
	if !returnedIDs[2] {
		t.Error("Expected edict 2 (cancelled, unsealed) to be included due to OR semantics")
	}

	// Edict 3 should be included (un-cancelled but sealed) - NEW OR BEHAVIOR
	if !returnedIDs[3] {
		t.Error("Expected edict 3 (sealed, un-cancelled) to be included due to OR semantics")
	}

	// Edict 4 should NOT be included (cancelled AND sealed)
	if returnedIDs[4] {
		t.Error("Edict 4 (cancelled AND sealed) should NOT be included")
	}
}

func TestListActiveEdicts_Empty(t *testing.T) {
	db := setupSealServiceTestDB(t)
	svc := NewSealService(db)

	activeEdicts, err := svc.ListActiveEdicts("testuser", "testproject")
	if err != nil {
		t.Fatalf("ListActiveEdicts failed: %v", err)
	}

	if len(activeEdicts) != 0 {
		t.Errorf("Expected 0 edicts, got %d", len(activeEdicts))
	}
}

func TestListActiveEdicts_SealStatusPopulated(t *testing.T) {
	db := setupSealServiceTestDB(t)
	svc := NewSealService(db)

	// Create an edict
	edict := Edict{ID: 1, Username: "testuser", Project: "testproject", Intent: "Test"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	// Grant only judge seal
	if err := svc.GrantSeal(edict.Key(), "judge", JSON{}); err != nil {
		t.Fatalf("failed to grant seal: %v", err)
	}

	activeEdicts, err := svc.ListActiveEdicts("testuser", "testproject")
	if err != nil {
		t.Fatalf("ListActiveEdicts failed: %v", err)
	}

	if len(activeEdicts) != 1 {
		t.Fatalf("Expected 1 edict, got %d", len(activeEdicts))
	}

	if !activeEdicts[0].HasJudgeSeal {
		t.Error("Expected HasJudgeSeal to be true")
	}
	if activeEdicts[0].HasSageSeal {
		t.Error("Expected HasSageSeal to be false")
	}
}
