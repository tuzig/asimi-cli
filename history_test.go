package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHistoryStore_NewHistoryStore(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	store, err := NewHistoryStore()
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NotEmpty(t, store.filePath)
	require.Equal(t, 1000, store.maxSize)

	cwd, _ := os.Getwd()
	expectedSlug := projectSlug(findProjectRoot(cwd))
	if expectedSlug == "" {
		expectedSlug = defaultProjectSlug
	}
	expectedPath := filepath.Join(tempDir, ".local", "share", "asimi", "repo", filepath.FromSlash(expectedSlug), "history.json")
	require.Equal(t, expectedPath, store.filePath)
}

func TestHistoryStore_LoadEmpty(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  1000,
	}

	entries, err := store.Load()
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestHistoryStore_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  1000,
	}

	// Create test entries
	now := time.Now()
	entries := []HistoryEntry{
		{Prompt: "first prompt", Timestamp: now},
		{Prompt: "second prompt", Timestamp: now.Add(time.Minute)},
		{Prompt: "third prompt", Timestamp: now.Add(2 * time.Minute)},
	}

	// Save entries
	err := store.Save(entries)
	require.NoError(t, err)

	// Load entries
	loaded, err := store.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 3)
	require.Equal(t, "first prompt", loaded[0].Prompt)
	require.Equal(t, "second prompt", loaded[1].Prompt)
	require.Equal(t, "third prompt", loaded[2].Prompt)
}

func TestHistoryStore_Append(t *testing.T) {
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  1000,
	}

	// Append first entry
	err := store.Append("first prompt")
	require.NoError(t, err)

	// Append second entry
	err = store.Append("second prompt")
	require.NoError(t, err)

	// Load and verify
	entries, err := store.Load()
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "first prompt", entries[0].Prompt)
	require.Equal(t, "second prompt", entries[1].Prompt)
}

func TestHistoryStore_AppendDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  1000,
	}

	// Append first entry
	err := store.Append("same prompt")
	require.NoError(t, err)

	// Append duplicate (should be ignored)
	err = store.Append("same prompt")
	require.NoError(t, err)

	// Load and verify only one entry exists
	entries, err := store.Load()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "same prompt", entries[0].Prompt)
}

func TestHistoryStore_MaxSize(t *testing.T) {
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  5, // Small max size for testing
	}

	// Create more entries than max size
	now := time.Now()
	entries := []HistoryEntry{
		{Prompt: "prompt 1", Timestamp: now},
		{Prompt: "prompt 2", Timestamp: now},
		{Prompt: "prompt 3", Timestamp: now},
		{Prompt: "prompt 4", Timestamp: now},
		{Prompt: "prompt 5", Timestamp: now},
		{Prompt: "prompt 6", Timestamp: now},
		{Prompt: "prompt 7", Timestamp: now},
	}

	// Save entries
	err := store.Save(entries)
	require.NoError(t, err)

	// Load and verify only last 5 entries are kept
	loaded, err := store.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 5)
	require.Equal(t, "prompt 3", loaded[0].Prompt)
	require.Equal(t, "prompt 7", loaded[4].Prompt)
}

func TestHistoryStore_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  1000,
	}

	// Add some entries
	err := store.Append("first prompt")
	require.NoError(t, err)
	err = store.Append("second prompt")
	require.NoError(t, err)

	// Verify entries exist
	entries, err := store.Load()
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Clear history
	err = store.Clear()
	require.NoError(t, err)

	// Verify file is gone
	_, err = os.Stat(store.filePath)
	require.True(t, os.IsNotExist(err))

	// Load should return empty
	entries, err = store.Load()
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestHistoryStore_LoadCorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  1000,
	}

	// Write corrupted JSON
	err := os.WriteFile(store.filePath, []byte("not valid json {{{"), 0o644)
	require.NoError(t, err)

	// Load should return empty and not error
	entries, err := store.Load()
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestTUIModel_InitHistoryLoadsFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  1000,
	}

	// Save some history
	err := store.Append("first prompt")
	require.NoError(t, err)
	err = store.Append("second prompt")
	require.NoError(t, err)

	// Create a new TUI model with the store
	model, _ := newTestModel(t)
	model.historyStore = store

	// Initialize history
	model.initHistory()

	// Verify history was loaded
	require.Len(t, model.promptHistory, 2)
	require.Equal(t, "first prompt", model.promptHistory[0].Prompt)
	require.Equal(t, "second prompt", model.promptHistory[1].Prompt)
	require.Equal(t, 2, model.historyCursor)
}

func TestTUIModel_HistoryPersistsAcrossSessions(t *testing.T) {
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  1000,
	}

	// First session
	model1, _ := newTestModel(t)
	model1.historyStore = store
	model1.initHistory()

	// Simulate entering prompts
	model1.promptHistory = append(model1.promptHistory, promptHistoryEntry{
		Prompt:          "first prompt",
		SessionSnapshot: 1,
		ChatSnapshot:    0,
	})
	store.Append("first prompt")

	model1.promptHistory = append(model1.promptHistory, promptHistoryEntry{
		Prompt:          "second prompt",
		SessionSnapshot: 2,
		ChatSnapshot:    1,
	})
	store.Append("second prompt")

	// Second session (simulating app restart)
	model2, _ := newTestModel(t)
	model2.historyStore = store
	model2.initHistory()

	// Verify history was loaded
	require.Len(t, model2.promptHistory, 2)
	require.Equal(t, "first prompt", model2.promptHistory[0].Prompt)
	require.Equal(t, "second prompt", model2.promptHistory[1].Prompt)
}

func TestClearHistoryCommand(t *testing.T) {
	tmpDir := t.TempDir()
	store := &HistoryStore{
		filePath: filepath.Join(tmpDir, "history.json"),
		maxSize:  1000,
	}

	model, _ := newTestModel(t)
	model.historyStore = store

	// Add some history
	store.Append("first prompt")
	store.Append("second prompt")
	model.initHistory()

	require.Len(t, model.promptHistory, 2)

	// Execute clear history command
	handleClearHistoryCommand(model, []string{})

	// Verify in-memory history is cleared
	require.Empty(t, model.promptHistory)
	require.Equal(t, 0, model.historyCursor)
	require.False(t, model.historySaved)

	// Verify persistent history is cleared
	entries, err := store.Load()
	require.NoError(t, err)
	require.Empty(t, entries)
}
