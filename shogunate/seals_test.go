package shogunate

import (
	"testing"

	"github.com/afittestide/asimi/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Migrate the Seal table
	if err := db.AutoMigrate(&storage.Seal{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db
}

func TestSealService_GrantSeal(t *testing.T) {
	db := setupTestDB(t)
	service := NewSealService(db)

	var edictID uint = 1
	ministerID := "judge"
	metadata := storage.JSON{"test": "data"}

	err := service.GrantSeal(edictID, ministerID, metadata)
	if err != nil {
		t.Fatalf("GrantSeal failed: %v", err)
	}

	// Verify seal was created
	hasSeal, err := service.HasSeal(edictID, ministerID)
	if err != nil {
		t.Fatalf("HasSeal failed: %v", err)
	}
	if !hasSeal {
		t.Error("Expected seal to exist")
	}
}

func TestSealService_GetSeals(t *testing.T) {
	db := setupTestDB(t)
	service := NewSealService(db)

	var edictID uint = 2

	// Grant multiple seals
	service.GrantSeal(edictID, "judge", storage.JSON{"verdict": "passed"})
	service.GrantSeal(edictID, "sage", storage.JSON{"review": "approved"})

	seals, err := service.GetSeals(edictID)
	if err != nil {
		t.Fatalf("GetSeals failed: %v", err)
	}

	if len(seals) != 2 {
		t.Errorf("Expected 2 seals, got %d", len(seals))
	}
}

func TestSealService_GetMissingSeals(t *testing.T) {
	db := setupTestDB(t)
	service := NewSealService(db)

	var edictID uint = 3

	// Grant only judge seal
	service.GrantSeal(edictID, "judge", storage.JSON{})

	missing, err := service.GetMissingSeals(edictID)
	if err != nil {
		t.Fatalf("GetMissingSeals failed: %v", err)
	}

	if len(missing) != 2 {
		t.Errorf("Expected 2 missing seals, got %d", len(missing))
	}

	// Check that sage and ruler are missing
	expectedMissing := map[string]bool{"sage": true, "ruler": true}
	for _, m := range missing {
		if !expectedMissing[m] {
			t.Errorf("Unexpected missing seal: %s", m)
		}
	}
}

func TestSealService_IsPendingAscension(t *testing.T) {
	db := setupTestDB(t)
	service := NewSealService(db)

	var edictID uint = 4

	// Initially not pending
	pending, err := service.IsPendingAscension(edictID)
	if err != nil {
		t.Fatalf("IsPendingAscension failed: %v", err)
	}
	if pending {
		t.Error("Expected edict to not be pending ascension initially")
	}

	// Grant judge and sage seals
	service.GrantSeal(edictID, "judge", storage.JSON{})
	service.GrantSeal(edictID, "sage", storage.JSON{})

	// Now should be pending
	pending, err = service.IsPendingAscension(edictID)
	if err != nil {
		t.Fatalf("IsPendingAscension failed: %v", err)
	}
	if !pending {
		t.Error("Expected edict to be pending ascension after judge and sage seals")
	}

	// Grant ruler seal
	service.GrantSeal(edictID, "ruler", storage.JSON{})

	// Should no longer be pending
	pending, err = service.IsPendingAscension(edictID)
	if err != nil {
		t.Fatalf("IsPendingAscension failed: %v", err)
	}
	if pending {
		t.Error("Expected edict to not be pending after ruler seal")
	}
}

func TestSealService_GetSealStatus(t *testing.T) {
	db := setupTestDB(t)
	service := NewSealService(db)

	var edictID uint = 5

	// Grant judge seal only
	service.GrantSeal(edictID, "judge", storage.JSON{})

	status, err := service.GetSealStatus(edictID)
	if err != nil {
		t.Fatalf("GetSealStatus failed: %v", err)
	}

	if !status["judge"] {
		t.Error("Expected judge seal to be present")
	}
	if status["sage"] {
		t.Error("Expected sage seal to be absent")
	}
	if status["ruler"] {
		t.Error("Expected ruler seal to be absent")
	}
}
