package storage

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestClearSession tests that ClearSession properly deletes both messages and session
func TestClearSession(t *testing.T) {
	// Create a temporary database
	tmpFile, err := os.CreateTemp("", "session_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := InitDB(tmpFile.Name())
	require.NoError(t, err)
	defer db.Close()

	store := NewSessionStore(db, &SessionConfig{Enabled: true})

	// Create a session with minimal data
	session := &SessionData{
		ID:          "test-session-123",
		FirstPrompt: "test prompt",
		Model:       "test-model",
		Provider:    "test-provider",
		WorkingDir:  "/tmp",
		TabType:     "ruling",
		Messages:    nil, // empty for this test
	}

	// Save the session
	err = store.SaveSession(session, "github.com", "testuser", "testrepo", "main")
	require.NoError(t, err)

	// Verify session exists
	loaded, host, org, project, branch, err := store.LoadSession(session.ID)
	require.NoError(t, err)
	require.Equal(t, "test-session-123", loaded.ID)
	require.Equal(t, "github.com", host)
	require.Equal(t, "testuser", org)
	require.Equal(t, "testrepo", project)
	require.Equal(t, "main", branch)

	// Clear the session
	err = store.ClearSession(session.ID)
	require.NoError(t, err)

	// Verify session no longer exists
	_, _, _, _, _, err = store.LoadSession(session.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "session not found")
}

// TestClearSession_OnlyDeletesTargetSession tests that ClearSession only deletes the specified session
func TestClearSession_OnlyDeletesTargetSession(t *testing.T) {
	// Create a temporary database
	tmpFile, err := os.CreateTemp("", "session_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := InitDB(tmpFile.Name())
	require.NoError(t, err)
	defer db.Close()

	store := NewSessionStore(db, &SessionConfig{Enabled: true})

	// Create two sessions
	session1 := &SessionData{
		ID:          "session-1",
		FirstPrompt: "prompt 1",
		Model:       "test-model",
		Provider:    "test-provider",
		WorkingDir:  "/tmp",
		TabType:     "ruling",
	}
	session2 := &SessionData{
		ID:          "session-2",
		FirstPrompt: "prompt 2",
		Model:       "test-model",
		Provider:    "test-provider",
		WorkingDir:  "/tmp",
		TabType:     "hunting",
	}

	err = store.SaveSession(session1, "github.com", "user", "repo", "main")
	require.NoError(t, err)
	err = store.SaveSession(session2, "github.com", "user", "repo", "main")
	require.NoError(t, err)

	// Clear only session 1
	err = store.ClearSession("session-1")
	require.NoError(t, err)

	// Session 1 should be gone
	_, _, _, _, _, err = store.LoadSession("session-1")
	require.Error(t, err)

	// Session 2 should still exist
	loaded, _, _, _, _, err := store.LoadSession("session-2")
	require.NoError(t, err)
	require.Equal(t, "session-2", loaded.ID)
}

// TestClearSession_Idempotent tests that calling ClearSession twice is safe
func TestClearSession_Idempotent(t *testing.T) {
	// Create a temporary database
	tmpFile, err := os.CreateTemp("", "session_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := InitDB(tmpFile.Name())
	require.NoError(t, err)
	defer db.Close()

	store := NewSessionStore(db, &SessionConfig{Enabled: true})

	// Create a session
	session := &SessionData{
		ID:          "idempotent-session",
		FirstPrompt: "test",
		Model:       "test-model",
		Provider:    "test-provider",
		WorkingDir:  "/tmp",
		TabType:     "ruling",
	}

	err = store.SaveSession(session, "github.com", "user", "repo", "main")
	require.NoError(t, err)

	// Clear once - should succeed
	err = store.ClearSession(session.ID)
	require.NoError(t, err)

	// Clear again - should also succeed (idempotent)
	err = store.ClearSession(session.ID)
	require.NoError(t, err) // No error even though session doesn't exist
}
