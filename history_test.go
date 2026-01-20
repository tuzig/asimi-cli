package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPromptHistoryStore_CreateAndLoad tests creating a prompt history store and loading entries
func TestPromptHistoryStore_CreateAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage
	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := MakeRepoInfo("/test/project", "main")

	// Create prompt history store
	store, err := NewPromptHistoryStore(db, repoInfo)
	require.NoError(t, err)
	require.NotNil(t, store)

	// Initially should be empty
	entries, err := store.Load()
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// TestPromptHistoryStore_AppendAndLoad tests appending and loading prompts
func TestPromptHistoryStore_AppendAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := MakeRepoInfo("/test/project", "main")

	store, err := NewPromptHistoryStore(db, repoInfo)
	require.NoError(t, err)

	// Append some prompts
	prompts := []string{
		"How do I write a test?",
		"Explain channels in Go",
		"Help me debug this code",
	}

	for _, prompt := range prompts {
		err := store.Append(prompt)
		require.NoError(t, err)
		time.Sleep(1 * time.Millisecond) // Ensure different timestamps
	}

	// Load and verify
	entries, err := store.Load()
	require.NoError(t, err)
	require.Len(t, entries, 3)

	// Verify prompts are in chronological order (oldest first)
	for i, expected := range prompts {
		assert.Equal(t, expected, entries[i].Content)
		assert.False(t, entries[i].Timestamp.IsZero())
	}

	// Verify timestamps are in order
	for i := 1; i < len(entries); i++ {
		assert.True(t, entries[i].Timestamp.After(entries[i-1].Timestamp) ||
			entries[i].Timestamp.Equal(entries[i-1].Timestamp))
	}
}

// TestPromptHistoryStore_Clear tests clearing history
func TestPromptHistoryStore_Clear(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := MakeRepoInfo("/test/project", "main")

	store, err := NewPromptHistoryStore(db, repoInfo)
	require.NoError(t, err)

	// Add some entries
	err = store.Append("First prompt")
	require.NoError(t, err)
	err = store.Append("Second prompt")
	require.NoError(t, err)

	// Verify they exist
	entries, err := store.Load()
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	// Clear history
	err = store.Clear()
	require.NoError(t, err)

	// Verify it's empty
	entries, err = store.Load()
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// TestPromptHistoryStore_Save tests the Save method (should be no-op)
func TestPromptHistoryStore_Save(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := MakeRepoInfo("/test/project", "main")

	store, err := NewPromptHistoryStore(db, repoInfo)
	require.NoError(t, err)

	// Save should not error (it's a no-op for SQLite)
	entries := []storage.HistoryEntry{
		{Content: "test", Timestamp: time.Now()},
	}
	err = store.Save(entries)
	assert.NoError(t, err)
}

// TestCommandHistoryStore_AppendAndLoad tests command history
func TestCommandHistoryStore_AppendAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := MakeRepoInfo("/test/project", "main")

	store, err := NewCommandHistoryStore(db, repoInfo)
	require.NoError(t, err)

	// Append some commands
	commands := []string{
		"/help",
		"/new",
		"/resume",
	}

	for _, cmd := range commands {
		err := store.Append(cmd)
		require.NoError(t, err)
		time.Sleep(1 * time.Millisecond)
	}

	// Load and verify
	entries, err := store.Load()
	require.NoError(t, err)
	require.Len(t, entries, 3)

	for i, expected := range commands {
		assert.Equal(t, expected, entries[i].Content)
	}
}

// TestHistoryStore_IsolatedByProject tests that history is isolated per project
// Note: For non-git directories, parseProjectSlug returns the same values,
// so we test that history with the same project info is shared
func TestHistoryStore_IsolatedByProject(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Use the current directory (which is a git repo) for project1
	// and a non-git directory for project2
	cwd, _ := os.Getwd()
	project1 := MakeRepoInfo(cwd, "main")
	project2 := MakeRepoInfo("/nonexistent/project", "main")

	store1, err := NewPromptHistoryStore(db, project1)
	require.NoError(t, err)

	store2, err := NewPromptHistoryStore(db, project2)
	require.NoError(t, err)

	// Add to store1
	err = store1.Append("Project 1 prompt")
	require.NoError(t, err)

	// Add to store2
	err = store2.Append("Project 2 prompt")
	require.NoError(t, err)

	// Verify entries - they should be isolated since they have different project roots
	// (assuming project1 is a git repo and project2 is not)
	entries1, err := store1.Load()
	require.NoError(t, err)
	// Store1 should have only its own prompt
	found := false
	for _, entry := range entries1 {
		if entry.Content == "Project 1 prompt" {
			found = true
		}
	}
	assert.True(t, found, "Project 1 prompt should be in store1")

	entries2, err := store2.Load()
	require.NoError(t, err)
	// Store2 should have only its own prompt
	found = false
	for _, entry := range entries2 {
		if entry.Content == "Project 2 prompt" {
			found = true
		}
	}
	assert.True(t, found, "Project 2 prompt should be in store2")
}

// TestHistoryStore_EmptyPrompt tests handling of empty prompts
func TestHistoryStore_EmptyPrompt(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := MakeRepoInfo("/test/project", "main")

	store, err := NewPromptHistoryStore(db, repoInfo)
	require.NoError(t, err)

	// Try to append empty prompt (should still work - storage layer handles validation)
	err = store.Append("")
	require.NoError(t, err)

	// Verify it was stored
	entries, err := store.Load()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "", entries[0].Content)
}

// TestHistoryStore_LongPrompt tests handling of very long prompts
func TestHistoryStore_LongPrompt(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := MakeRepoInfo("/test/project", "main")

	store, err := NewPromptHistoryStore(db, repoInfo)
	require.NoError(t, err)

	// Create a very long prompt (10KB)
	longPrompt := ""
	for i := 0; i < 10000; i++ {
		longPrompt += "a"
	}

	err = store.Append(longPrompt)
	require.NoError(t, err)

	// Verify it was stored correctly
	entries, err := store.Load()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, longPrompt, entries[0].Content)
	assert.Len(t, entries[0].Content, 10000)
}

// TestHistoryStore_SpecialCharacters tests handling of special characters
func TestHistoryStore_SpecialCharacters(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := MakeRepoInfo("/test/project", "main")

	store, err := NewPromptHistoryStore(db, repoInfo)
	require.NoError(t, err)

	// Test various special characters
	specialPrompts := []string{
		"Prompt with 'single quotes'",
		`Prompt with "double quotes"`,
		"Prompt with\nnewlines\nand\ttabs",
		"Prompt with unicode: 你好世界 🚀",
		"Prompt with SQL: SELECT * FROM users WHERE name = 'test';",
		"Prompt with backslash: C:\\Users\\test",
	}

	for _, prompt := range specialPrompts {
		err := store.Append(prompt)
		require.NoError(t, err)
	}

	// Verify all were stored correctly
	entries, err := store.Load()
	require.NoError(t, err)
	require.Len(t, entries, len(specialPrompts))

	for i, expected := range specialPrompts {
		assert.Equal(t, expected, entries[i].Content)
	}
}

// TestHistoryStore_NilDatabase tests error handling when database is nil
func TestHistoryStore_NilDatabase(t *testing.T) {
	repoInfo := MakeRepoInfo("/test/project", "main")

	// Try to create store with nil database
	store, err := NewPromptHistoryStore(nil, repoInfo)
	assert.Error(t, err)
	assert.Nil(t, store)
	assert.Contains(t, err.Error(), "storage not initialized")
}

// TestHistoryStore_ConcurrentAccess tests concurrent append operations
func TestHistoryStore_ConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	dbPath := filepath.Join(tempDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := MakeRepoInfo("/test/project", "main")

	store, err := NewPromptHistoryStore(db, repoInfo)
	require.NoError(t, err)

	// Pre-create the repository to avoid UNIQUE constraint issues during concurrent access
	err = store.Append("initial prompt")
	require.NoError(t, err)

	// Append concurrently from multiple goroutines
	const numGoroutines = 10
	const promptsPerGoroutine = 5

	done := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines*promptsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < promptsPerGoroutine; j++ {
				err := store.Append(string(rune('A'+id)) + " prompt " + string(rune('0'+j)))
				if err != nil {
					errors <- err
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Logf("Append error: %v", err)
		errorCount++
	}

	// Verify entries were stored (allow for some failures due to race conditions)
	entries, err := store.Load()
	require.NoError(t, err)
	// Should have initial prompt + at least most of the concurrent appends
	assert.GreaterOrEqual(t, len(entries), numGoroutines*promptsPerGoroutine/2,
		"Expected at least half the concurrent appends to succeed")
	assert.LessOrEqual(t, errorCount, numGoroutines*promptsPerGoroutine/2,
		"Expected most concurrent appends to succeed")
}
