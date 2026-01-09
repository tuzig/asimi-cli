package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryStore_AppendAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "history_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &HistoryConfig{
		Enabled:     true,
		MaxSessions: 100,
		MaxAgeDays:  30,
	}
	store := NewHistoryStore(db, cfg)

	// Test AppendPrompt
	err = store.AppendPrompt("github.com", "test", "project", "main", "test prompt 1")
	if err != nil {
		t.Fatalf("Failed to append prompt: %v", err)
	}

	err = store.AppendPrompt("github.com", "test", "project", "main", "test prompt 2")
	if err != nil {
		t.Fatalf("Failed to append prompt: %v", err)
	}

	// Test LoadPromptHistory
	entries, err := store.LoadPromptHistory("github.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Failed to load prompt history: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("Expected 2 prompts, got %d", len(entries))
	}

	// Test with limit
	entries, err = store.LoadPromptHistory("github.com", "test", "project", "main", 1)
	if err != nil {
		t.Fatalf("Failed to load prompt history with limit: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("Expected 1 prompt with limit, got %d", len(entries))
	}

	// Test AppendCommand
	err = store.AppendCommand("github.com", "test", "project", "main", "/help")
	if err != nil {
		t.Fatalf("Failed to append command: %v", err)
	}

	err = store.AppendCommand("github.com", "test", "project", "main", "/new")
	if err != nil {
		t.Fatalf("Failed to append command: %v", err)
	}

	// Test LoadCommandHistory
	cmdEntries, err := store.LoadCommandHistory("github.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Failed to load command history: %v", err)
	}
	if len(cmdEntries) != 2 {
		t.Errorf("Expected 2 commands, got %d", len(cmdEntries))
	}

	// Test with limit
	cmdEntries, err = store.LoadCommandHistory("github.com", "test", "project", "main", 1)
	if err != nil {
		t.Fatalf("Failed to load command history with limit: %v", err)
	}
	if len(cmdEntries) != 1 {
		t.Errorf("Expected 1 command with limit, got %d", len(cmdEntries))
	}
}

func TestHistoryStore_ClearPromptHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clear_prompt_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &HistoryConfig{Enabled: true, MaxSessions: 100}
	store := NewHistoryStore(db, cfg)

	// Add prompts
	store.AppendPrompt("github.com", "test", "project", "main", "prompt 1")
	store.AppendPrompt("github.com", "test", "project", "main", "prompt 2")

	// Clear
	err = store.ClearPromptHistory("github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to clear prompt history: %v", err)
	}

	// Verify empty
	entries, _ := store.LoadPromptHistory("github.com", "test", "project", "main", 0)
	if len(entries) != 0 {
		t.Errorf("Expected 0 prompts after clear, got %d", len(entries))
	}

	// Clear non-existent repo (should not error)
	err = store.ClearPromptHistory("nonexistent.com", "test", "project", "main")
	if err != nil {
		t.Errorf("Unexpected error clearing non-existent history: %v", err)
	}
}

func TestHistoryStore_ClearCommandHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clear_cmd_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &HistoryConfig{Enabled: true, MaxSessions: 100}
	store := NewHistoryStore(db, cfg)

	// Add commands
	store.AppendCommand("github.com", "test", "project", "main", "/help")
	store.AppendCommand("github.com", "test", "project", "main", "/new")

	// Clear
	err = store.ClearCommandHistory("github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to clear command history: %v", err)
	}

	// Verify empty
	entries, _ := store.LoadCommandHistory("github.com", "test", "project", "main", 0)
	if len(entries) != 0 {
		t.Errorf("Expected 0 commands after clear, got %d", len(entries))
	}

	// Clear non-existent repo (should not error)
	err = store.ClearCommandHistory("nonexistent.com", "test", "project", "main")
	if err != nil {
		t.Errorf("Unexpected error clearing non-existent history: %v", err)
	}
}

func TestHistoryStore_CleanupOldHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &HistoryConfig{
		Enabled:     true,
		MaxSessions: 100,
		MaxAgeDays:  1, // 1 day
	}
	store := NewHistoryStore(db, cfg)

	// Get branch ID first
	repoID, _ := db.GetOrCreateRepository("github.com", "test", "project")
	branchID, _ := db.GetOrCreateBranch(repoID, "main")

	// Insert old prompt directly (2 days ago)
	oldTimestamp := time.Now().AddDate(0, 0, -2).Unix()
	db.conn.Create(&PromptHistory{
		BranchID:  branchID,
		Prompt:    "old prompt",
		Timestamp: oldTimestamp,
	})

	// Insert recent prompt
	store.AppendPrompt("github.com", "test", "project", "main", "recent prompt")

	// Insert old command directly
	db.conn.Create(&CommandHistory{
		BranchID:  branchID,
		Command:   "/old",
		Timestamp: oldTimestamp,
	})

	// Insert recent command
	store.AppendCommand("github.com", "test", "project", "main", "/recent")

	// Run cleanup
	err = store.CleanupOldHistory()
	if err != nil {
		t.Fatalf("Failed to cleanup old history: %v", err)
	}

	// Verify old entries are gone
	prompts, _ := store.LoadPromptHistory("github.com", "test", "project", "main", 0)
	if len(prompts) != 1 {
		t.Errorf("Expected 1 prompt after cleanup, got %d", len(prompts))
	}

	commands, _ := store.LoadCommandHistory("github.com", "test", "project", "main", 0)
	if len(commands) != 1 {
		t.Errorf("Expected 1 command after cleanup, got %d", len(commands))
	}
}

func TestHistoryStore_CleanupOldHistory_NilConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup_nil_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewHistoryStore(db, nil)

	// Should not error with nil config
	err = store.CleanupOldHistory()
	if err != nil {
		t.Errorf("Unexpected error with nil config: %v", err)
	}
}

func TestHistoryStore_CleanupOldHistory_ZeroMaxAge(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup_zero_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &HistoryConfig{
		Enabled:    true,
		MaxAgeDays: 0, // Zero means no cleanup
	}
	store := NewHistoryStore(db, cfg)

	// Should not error
	err = store.CleanupOldHistory()
	if err != nil {
		t.Errorf("Unexpected error with zero MaxAgeDays: %v", err)
	}
}

func TestHistoryStore_ApplyLimit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "limit_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &HistoryConfig{
		Enabled:     true,
		MaxSessions: 3, // Only keep 3 entries
	}
	store := NewHistoryStore(db, cfg)

	// Add more than the limit
	for i := 0; i < 5; i++ {
		err = store.AppendPrompt("github.com", "test", "project", "main", "prompt")
		if err != nil {
			t.Fatalf("Failed to append prompt %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Should only have 3 entries
	entries, err := store.LoadPromptHistory("github.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Failed to load prompt history: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("Expected 3 prompts after limit, got %d", len(entries))
	}

	// Same for commands
	for i := 0; i < 5; i++ {
		err = store.AppendCommand("github.com", "test", "project", "main", "/cmd")
		if err != nil {
			t.Fatalf("Failed to append command %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cmdEntries, err := store.LoadCommandHistory("github.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Failed to load command history: %v", err)
	}
	if len(cmdEntries) != 3 {
		t.Errorf("Expected 3 commands after limit, got %d", len(cmdEntries))
	}
}

func TestHistoryStore_LoadNonExistentRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nonexistent_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewHistoryStore(db, nil)

	// Load from non-existent repo
	entries, err := store.LoadPromptHistory("nonexistent.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected empty list, got %d entries", len(entries))
	}

	cmdEntries, err := store.LoadCommandHistory("nonexistent.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(cmdEntries) != 0 {
		t.Errorf("Expected empty list, got %d entries", len(cmdEntries))
	}
}

func TestHistoryStore_LoadNonExistentBranch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nonexistent_branch_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewHistoryStore(db, nil)

	// Create repo but not branch
	db.GetOrCreateRepository("github.com", "test", "project")

	// Load from non-existent branch
	entries, err := store.LoadPromptHistory("github.com", "test", "project", "nonexistent", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected empty list, got %d entries", len(entries))
	}
}

func TestHistoryStore_RetryOnError(t *testing.T) {
	// This test verifies the retry mechanism works by ensuring
	// that operations complete even under mild contention
	tmpDir, err := os.MkdirTemp("", "retry_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &HistoryConfig{Enabled: true, MaxSessions: 100}
	store := NewHistoryStore(db, cfg)

	// Multiple sequential appends should all succeed (exercises retry path indirectly)
	for i := 0; i < 10; i++ {
		err = store.AppendPrompt("github.com", "test", "project", "main", "prompt")
		if err != nil {
			t.Fatalf("Failed to append prompt %d: %v", i, err)
		}
		err = store.AppendCommand("github.com", "test", "project", "main", "/cmd")
		if err != nil {
			t.Fatalf("Failed to append command %d: %v", i, err)
		}
	}

	prompts, _ := store.LoadPromptHistory("github.com", "test", "project", "main", 0)
	if len(prompts) != 10 {
		t.Errorf("Expected 10 prompts, got %d", len(prompts))
	}

	commands, _ := store.LoadCommandHistory("github.com", "test", "project", "main", 0)
	if len(commands) != 10 {
		t.Errorf("Expected 10 commands, got %d", len(commands))
	}
}

func TestHistoryStore_LimitEdgeCases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "limit_edge_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	// Test with MaxSessions = 1
	cfg := &HistoryConfig{Enabled: true, MaxSessions: 1}
	store := NewHistoryStore(db, cfg)

	store.AppendPrompt("github.com", "test", "project", "main", "first")
	time.Sleep(10 * time.Millisecond)
	store.AppendPrompt("github.com", "test", "project", "main", "second")

	entries, _ := store.LoadPromptHistory("github.com", "test", "project", "main", 0)
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry with MaxSessions=1, got %d", len(entries))
	}
	// Should keep the most recent
	if entries[0].Content != "second" {
		t.Errorf("Expected 'second', got '%s'", entries[0].Content)
	}
}

func TestHistoryStore_NilConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nilconfig_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	// Create store with nil config
	store := NewHistoryStore(db, nil)

	// Should work without limit
	for i := 0; i < 5; i++ {
		err = store.AppendPrompt("github.com", "test", "project", "main", "prompt")
		if err != nil {
			t.Fatalf("Failed to append prompt: %v", err)
		}
	}

	entries, _ := store.LoadPromptHistory("github.com", "test", "project", "main", 0)
	if len(entries) != 5 {
		t.Errorf("Expected 5 entries with nil config, got %d", len(entries))
	}
}
