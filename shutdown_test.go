package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
)

// TestSessionStoreCloseWithTimeout verifies that Close() waits for pending saves
func TestSessionStoreCloseWithTimeout(t *testing.T) {
	// Create a temporary home directory for the test
	tmpHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage
	dbPath := filepath.Join(tmpHome, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	repoInfo := MakeRepoInfo(tmpHome, "")
	store, err := NewSessionStore(db, repoInfo, 10, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	// Create a test session
	session := &shogunate.Session{
		ID:           "test-session-123",
		CreatedAt:    time.Now(),
		LastUpdated:  time.Now(),
		FirstPrompt:  "Test prompt",
		Provider:     "test",
		Model:        "test-model",
		WorkingDir:   repoInfo.ProjectRoot,
	}

	// Add a user message so the session will be saved
	session.Messages = append(session.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart("test message")},
	})

	// Queue a save
	store.SaveSession(session)

	// Close the store (should wait for the save to complete)
	start := time.Now()
	store.Close()
	duration := time.Since(start)

	// Verify the close completed within a reasonable time (should be < 2 seconds timeout)
	if duration > 3*time.Second {
		t.Errorf("Close() took too long: %v", duration)
	}

	// Verify the session was saved to the database
	loadedSession, err := store.LoadSession(session.ID)
	if err != nil {
		t.Errorf("Session was not saved to database: %v", err)
	}
	if loadedSession.ID != session.ID {
		t.Errorf("Expected session ID %s, got %s", session.ID, loadedSession.ID)
	}
}

// TestSessionSaveOnQuit verifies that store.SaveSession() persists the session
// Note: SessionStore closing is handled by fx lifecycle
func TestSessionSaveOnQuit(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Initialize storage
	dbPath := filepath.Join(tmpDir, ".local", "share", "asimi", "asimi.sqlite")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Close()

	// Create a session store using NewSessionStore
	repoInfo := MakeRepoInfo(tmpDir, "")
	store, err := NewSessionStore(db, repoInfo, 10, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}
	defer store.Close() // Clean up after test

	// Create a test session with a message so it will be saved
	session := &shogunate.Session{
		ID:           "test-save-session",
		CreatedAt:    time.Now(),
		LastUpdated:  time.Now(),
		FirstPrompt:  "Test prompt",
		Provider:     "test",
		Model:        "test-model",
		WorkingDir:   repoInfo.ProjectRoot,
	}
	session.Messages = append(session.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart("test message")},
	})

	// Save session directly via store (as quit handlers do in production code)
	store.SaveSession(session)

	// Give async save a moment to complete
	time.Sleep(100 * time.Millisecond)

	// Verify session was saved by loading it
	loadedSession, err := store.LoadSession(session.ID)
	if err != nil {
		t.Errorf("Session was not saved: %v", err)
	}
	if loadedSession != nil && loadedSession.ID != session.ID {
		t.Errorf("Expected session ID %s, got %s", session.ID, loadedSession.ID)
	}
}
