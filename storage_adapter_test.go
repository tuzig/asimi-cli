package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// TestSessionStore_RoundTrip verifies that messages survive a save/load cycle
func TestSessionStore_RoundTrip(t *testing.T) {
	// Create temp DB
	tmpDir, err := os.MkdirTemp("", "adapter_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := repo.RepoInfo{
		ProjectRoot: tmpDir,
		Branch:      "main",
	}

	store, err := NewSessionStore(db, repoInfo, 100, 30)
	require.NoError(t, err)
	defer store.Close()

	// Create session with various message types
	now := time.Now()
	session := &shogunate.Session{
		ID:          "test-roundtrip-1",
		CreatedAt:   now,
		LastUpdated: now,
		FirstPrompt: "What is the meaning of life?",
		Provider:    "openai",
		Model:       "gpt-4",
		WorkingDir:  tmpDir,
		TabType:     "ruling",
	}

	// Add diverse message types
	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "What is the meaning of life?"},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "42"},
			},
		},
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "Analyze this file: /tmp/data.csv"},
			},
		},
		{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: `{"result": "file created at /tmp/test.txt"}`},
			},
		},
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "Now read the file"},
			},
		},
	}
	session.SetMessages(messages)

	// Save session
	err = store.SaveSessionSync(session)
	require.NoError(t, err)

	// Load session
	loaded, err := store.LoadSession(session.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	// Verify messages round-trip correctly
	loadedMessages := loaded.GetMessages()
	require.Equal(t, len(messages), len(loadedMessages), "message count should match")

	for i, expected := range messages {
		actual := loadedMessages[i]
		require.Equal(t, expected.Role, actual.Role, "message %d role should match", i)
		require.Equal(t, len(expected.Parts), len(actual.Parts), "message %d part count should match", i)

		for j, expectedPart := range expected.Parts {
			actualPart := actual.Parts[j]
			// Compare text content (only type used in these tests)
			if et, ok := expectedPart.(llms.TextContent); ok {
				at, ok := actualPart.(llms.TextContent)
				require.True(t, ok, "message %d part %d should be TextContent", i, j)
				require.Equal(t, et.Text, at.Text, "message %d part %d text should match", i, j)
			}
		}
	}

	// Verify metadata
	require.Equal(t, session.ID, loaded.ID)
	require.Equal(t, session.FirstPrompt, loaded.FirstPrompt)
	require.Equal(t, session.Provider, loaded.Provider)
	require.Equal(t, session.Model, loaded.Model)
}

// TestSessionStore_RoundTrip_EmptyMessages verifies sessions with no messages can be saved
func TestSessionStore_RoundTrip_EmptyMessages(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "adapter_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := repo.RepoInfo{
		ProjectRoot: tmpDir,
		Branch:      "main",
	}

	store, err := NewSessionStore(db, repoInfo, 100, 30)
	require.NoError(t, err)
	defer store.Close()

	// Create session with no user/AI messages (should be skipped on save)
	session := &shogunate.Session{
		ID:          "test-empty-1",
		FirstPrompt: "empty session",
		Provider:    "openai",
		Model:       "gpt-4",
		WorkingDir:  tmpDir,
	}
	// No messages - SaveSessionSync should skip
	err = store.SaveSessionSync(session)
	require.NoError(t, err, "empty session should not error on save")

	// Session should not exist (empty sessions are skipped)
	loaded, err := store.LoadSession(session.ID)
	require.Error(t, err, "should get error for non-existent session") // Returns error for non-existent session
	require.Nil(t, loaded)
}

// TestSessionStore_RoundTrip_SystemMessages verifies system messages alone also skip save
func TestSessionStore_RoundTrip_SystemMessages(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "adapter_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := repo.RepoInfo{
		ProjectRoot: tmpDir,
		Branch:      "main",
	}

	store, err := NewSessionStore(db, repoInfo, 100, 30)
	require.NoError(t, err)
	defer store.Close()

	session := &shogunate.Session{
		ID:          "test-system-only",
		FirstPrompt: "system message only",
		Provider:    "openai",
		Model:       "gpt-4",
		WorkingDir:  tmpDir,
	}
	session.SetMessages([]llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: "You are helpful"}}},
	})

	// Save should skip since no human/AI messages
	err = store.SaveSessionSync(session)
	require.NoError(t, err)
}

// TestSessionStore_ListSessions verifies list sessions work without messages
func TestSessionStore_ListSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "adapter_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repoInfo := repo.RepoInfo{
		ProjectRoot: tmpDir,
		Branch:      "main",
	}

	store, err := NewSessionStore(db, repoInfo, 100, 30)
	require.NoError(t, err)
	defer store.Close()

	// Save a session
	session := &shogunate.Session{
		ID:          "test-list-1",
		FirstPrompt: "List test",
		Provider:    "openai",
		Model:       "gpt-4",
		WorkingDir:  tmpDir,
	}
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}}},
		{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextContent{Text: "Hi there!"}}},
	}
	session.SetMessages(messages)

	err = store.SaveSessionSync(session)
	require.NoError(t, err)

	// List sessions
	sessions, err := store.ListSessions(10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(sessions), 1, "should have at least one session")

	// Find our session
	var found *shogunate.Session
	for i := range sessions {
		if sessions[i].ID == session.ID {
			found = &sessions[i]
			break
		}
	}
	require.NotNil(t, found, "session should be in list")
	require.Equal(t, session.FirstPrompt, found.FirstPrompt)
	require.Equal(t, 2, found.MessageCount, "list view should have message count")
}
