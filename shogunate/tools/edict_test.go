package tools

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupEdictTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Migrate all required tables
	if err := db.AutoMigrate(&storage.Edict{}, &storage.Seal{}, &storage.Zhengming{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db
}

func TestTransitionEdictTool_Cancel(t *testing.T) {
	db := setupEdictTestDB(t)

	// Create an edict
	edict := storage.Edict{
		EdictID: 1,
		Intent:  "Test edict",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	tool := TransitionEdictTool{DB: db}

	// Test cancelling
	result, err := tool.Call(context.Background(), fmt.Sprintf(`{"edict_id": %d, "status": "cancelled", "reason": "no longer needed"}`, edict.EdictID))
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify result
	if result == "" {
		t.Fatal("Expected non-empty result")
	}

	// Verify edict was cancelled
	var updated storage.Edict
	if err := db.First(&updated, "edict_id = ?", edict.EdictID).Error; err != nil {
		t.Fatalf("failed to get updated edict: %v", err)
	}
	if updated.CancelledAt == nil {
		t.Errorf("Expected cancelled_at to be set")
	}

	// Verify derived status is cancelled
	sealService := storage.NewSealService(db)
	status, err := sealService.GetEdictStatus(edict.Key())
	if err != nil {
		t.Fatalf("Failed to get edict status: %v", err)
	}
	if status != storage.EdictCancelled {
		t.Errorf("Expected status to be cancelled, got: %s", status)
	}
}

func TestTransitionEdictTool_InvalidStatus(t *testing.T) {
	db := setupEdictTestDB(t)

	// Create an edict
	edict := storage.Edict{
		EdictID: 1,
		Intent:  "Test edict",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	tool := TransitionEdictTool{DB: db}

	// Test invalid status
	_, err := tool.Call(context.Background(), fmt.Sprintf(`{"edict_id": %d, "status": "invalid_status"}`, edict.EdictID))
	if err == nil {
		t.Fatal("Expected error for invalid status")
	}
}

func TestTransitionEdictTool_BlockedToSealed(t *testing.T) {
	db := setupEdictTestDB(t)

	// Create an edict (not blocked)
	edict := storage.Edict{
		EdictID: 1,
		Intent:  "Test edict",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	tool := TransitionEdictTool{DB: db}

	// Test that we can seal a non-blocked edict
	result, err := tool.Call(context.Background(), fmt.Sprintf(`{"edict_id": %d, "status": "sealed"}`, edict.EdictID))
	if err != nil {
		t.Fatalf("Expected no error when sealing edict, got: %v", err)
	}
	if result == "" {
		t.Fatal("Expected non-empty result")
	}

	// Verify edict is now sealed
	sealService := storage.NewSealService(db)
	status, err := sealService.GetEdictStatus(edict.Key())
	if err != nil {
		t.Fatalf("Failed to get edict status: %v", err)
	}
	if status != storage.EdictSealed {
		t.Errorf("Expected status to be sealed, got: %s", status)
	}
}

func TestTransitionEdictTool_NonExistent(t *testing.T) {
	db := setupEdictTestDB(t)

	tool := TransitionEdictTool{DB: db}

	// Test non-existent edict
	_, err := tool.Call(context.Background(), `{"edict_id": 999, "status": "active"}`)
	if err == nil {
		t.Fatal("Expected error for non-existent edict")
	}
}

func TestTransitionEdictTool_MissingFields(t *testing.T) {
	db := setupEdictTestDB(t)

	tool := TransitionEdictTool{DB: db}

	// Test missing edict_id
	_, err := tool.Call(context.Background(), `{"status": "active"}`)
	if err == nil {
		t.Fatal("Expected error for missing edict_id")
	}

	// Test missing status
	_, err = tool.Call(context.Background(), `{"edict_id": 1}`)
	if err == nil {
		t.Fatal("Expected error for missing status")
	}
}

func TestListEdictsTool_FilterByStatus(t *testing.T) {
	db := setupEdictTestDB(t)

	// Create edicts
	edicts := []storage.Edict{
		{EdictID: 1, Intent: "Active edict"},
		{EdictID: 2, Intent: "Blocked edict"},
		{EdictID: 3, Intent: "Another blocked"},
		{EdictID: 4, Intent: "Sealed edict"},
	}
	for i := range edicts {
		if err := db.Create(&edicts[i]).Error; err != nil {
			t.Fatalf("failed to create edict: %v", err)
		}
	}

	// Create a pending zhengming for the second edict to make it blocked
	zhengming := storage.Zhengming{
		RequestID:  "test-blocked",
		EdictID:    edicts[1].EdictID,
		MinisterID: "forge",
		Status:     storage.ZhengmingPending,
		TimeoutAt:  time.Now().Add(time.Hour),
	}
	if err := db.Create(&zhengming).Error; err != nil {
		t.Fatalf("failed to create zhengming: %v", err)
	}

	// Seal the fourth edict
	sealService := storage.NewSealService(db)
	if err := sealService.GrantSeal(edicts[3].Key(), "judge", storage.JSON{}); err != nil {
		t.Fatalf("failed to grant judge seal: %v", err)
	}
	if err := sealService.GrantSeal(edicts[3].Key(), "sage", storage.JSON{}); err != nil {
		t.Fatalf("failed to grant sage seal: %v", err)
	}
	if err := sealService.GrantSeal(edicts[3].Key(), "ruler", storage.JSON{}); err != nil {
		t.Fatalf("failed to grant ruler seal: %v", err)
	}

	tool := ListEdictsTool{DB: db}

	// Test filtering by blocked status
	result, err := tool.Call(context.Background(), `{"status": "blocked"}`)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify result contains only blocked edicts
	if result == "" {
		t.Fatal("Expected non-empty result")
	}
	// Should contain the blocked edict
	if !containsSubstring(result, "Blocked edict") {
		t.Errorf("Expected result to contain blocked edict, got: %s", result)
	}
	// Should not contain active or sealed edicts
	if containsSubstring(result, "Active edict") || containsSubstring(result, "Sealed edict") {
		t.Errorf("Result should not contain non-blocked edicts, got: %s", result)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
