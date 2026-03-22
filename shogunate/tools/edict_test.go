package tools

import (
	"context"
	"fmt"
	"testing"

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

	// Migrate the Edict table
	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db
}

func TestTransitionEdictTool_Unblock(t *testing.T) {
	db := setupEdictTestDB(t)

	// Create a blocked edict
	edict := storage.Edict{
		Intent: "Test edict",
		Status: storage.EdictBlocked,
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	tool := TransitionEdictTool{DB: db}

	// Test unblocking
	result, err := tool.Call(context.Background(), fmt.Sprintf(`{"edict_id": %d, "status": "active", "reason": "reviewed and approved"}`, edict.EdictID))
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify result
	if result == "" {
		t.Fatal("Expected non-empty result")
	}

	// Verify edict status was updated
	var updated storage.Edict
	if err := db.First(&updated, "edict_id = ?", edict.EdictID).Error; err != nil {
		t.Fatalf("failed to get updated edict: %v", err)
	}
	if updated.Status != storage.EdictActive {
		t.Errorf("Expected status to be active, got: %s", updated.Status)
	}
}

func TestTransitionEdictTool_Reject(t *testing.T) {
	db := setupEdictTestDB(t)

	// Create a blocked edict
	edict := storage.Edict{
		Intent: "Test edict to reject",
		Status: storage.EdictBlocked,
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	tool := TransitionEdictTool{DB: db}

	// Test rejecting (cancelling)
	result, err := tool.Call(context.Background(), fmt.Sprintf(`{"edict_id": %d, "status": "cancelled", "reason": "no longer needed"}`, edict.EdictID))
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify result
	if result == "" {
		t.Fatal("Expected non-empty result")
	}

	// Verify edict status was updated
	var updated storage.Edict
	if err := db.First(&updated, "edict_id = ?", edict.EdictID).Error; err != nil {
		t.Fatalf("failed to get updated edict: %v", err)
	}
	if updated.Status != storage.EdictCancelled {
		t.Errorf("Expected status to be cancelled, got: %s", updated.Status)
	}
}

func TestTransitionEdictTool_InvalidStatus(t *testing.T) {
	db := setupEdictTestDB(t)

	// Create an edict
	edict := storage.Edict{
		Intent: "Test edict",
		Status: storage.EdictActive,
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

	// Create a blocked edict
	edict := storage.Edict{
		Intent: "Test edict",
		Status: storage.EdictBlocked,
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	tool := TransitionEdictTool{DB: db}

	// Test that blocked edicts can only go to active or cancelled
	_, err := tool.Call(context.Background(), fmt.Sprintf(`{"edict_id": %d, "status": "sealed"}`, edict.EdictID))
	if err == nil {
		t.Fatal("Expected error when transitioning blocked edict to sealed")
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

	// Create edicts with different statuses
	edicts := []storage.Edict{
		{Intent: "Active edict", Status: storage.EdictActive},
		{Intent: "Blocked edict", Status: storage.EdictBlocked},
		{Intent: "Another blocked", Status: storage.EdictBlocked},
		{Intent: "Sealed edict", Status: storage.EdictSealed},
	}
	for i := range edicts {
		if err := db.Create(&edicts[i]).Error; err != nil {
			t.Fatalf("failed to create edict: %v", err)
		}
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
	// Should contain 2 blocked edicts (by intent)
	if !containsSubstring(result, "Blocked edict") || !containsSubstring(result, "Another blocked") {
		t.Errorf("Expected result to contain blocked edicts, got: %s", result)
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
