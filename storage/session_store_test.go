package storage

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// DEPRECATED: TestClearSession tests removed - ClearSession function was removed
// to preserve user history for :resume functionality.

// TestListSessions_TabTypeFilter tests filtering sessions by tabType
func TestListSessions_TabTypeFilter(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "session_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := InitDB(tmpFile.Name())
	require.NoError(t, err)
	defer db.Close()

	store := NewSessionStore(db, &SessionConfig{Enabled: true})

	// Create sessions with different tab types
	sessionSpecs := []struct {
		id      string
		tabType string
	}{
		{"ruling-1", "ruling"},
		{"ruling-2", "ruling"},
		{"hunting-1", "hunting"},
		{"sandbox-1", "sandbox"},
	}

	for _, spec := range sessionSpecs {
		session := &SessionData{
			ID:          spec.id,
			FirstPrompt: "test prompt",
			Model:       "test-model",
			Provider:    "test-provider",
			WorkingDir:  "/tmp",
			TabType:     spec.tabType,
		}
		err := store.SaveSession(session, "github.com", "testuser", "testrepo", "main")
		require.NoError(t, err)
	}

	// Filter by "ruling" tabType — returns only ruling sessions
	rulingSessions, err := store.ListSessions("github.com", "testuser", "testrepo", "main", "ruling", 0)
	require.NoError(t, err)
	require.Len(t, rulingSessions, 2)
	for _, s := range rulingSessions {
		require.Equal(t, "ruling", s.TabType)
	}

	// Filter by "hunting" tabType — returns only hunting session
	huntingSessions, err := store.ListSessions("github.com", "testuser", "testrepo", "main", "hunting", 0)
	require.NoError(t, err)
	require.Len(t, huntingSessions, 1)
	require.Equal(t, "hunting", huntingSessions[0].TabType)

	// Filter by non-existent tabType — returns empty
	emptySessions, err := store.ListSessions("github.com", "testuser", "testrepo", "main", "nonexistent", 0)
	require.NoError(t, err)
	require.Len(t, emptySessions, 0)

	// Empty tabType — returns all sessions
	allSessions, err := store.ListSessions("github.com", "testuser", "testrepo", "main", "", 0)
	require.NoError(t, err)
	require.Len(t, allSessions, 4)
}

// TestListSessions_TabTypeFilterWithLimit tests that limit works correctly with tabType filter
func TestListSessions_TabTypeFilterWithLimit(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "session_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := InitDB(tmpFile.Name())
	require.NoError(t, err)
	defer db.Close()

	store := NewSessionStore(db, &SessionConfig{Enabled: true})

	// Create multiple sessions with the same tab type
	for i := 0; i < 5; i++ {
		session := &SessionData{
			ID:          fmt.Sprintf("session-ruling-%d", i),
			FirstPrompt: "test prompt",
			Model:       "test-model",
			Provider:    "test-provider",
			WorkingDir:  "/tmp",
			TabType:     "ruling",
		}
		err := store.SaveSession(session, "github.com", "user", "repo", "main")
		require.NoError(t, err)
	}

	// Filter with limit
	sessions, err := store.ListSessions("github.com", "user", "repo", "main", "ruling", 3)
	require.NoError(t, err)
	require.Len(t, sessions, 3)
	for _, s := range sessions {
		require.Equal(t, "ruling", s.TabType)
	}
}

// TestListSessions_TabTypeFilterAcrossBranches tests tabType filter works correctly across branches
func TestListSessions_TabTypeFilterAcrossBranches(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "session_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := InitDB(tmpFile.Name())
	require.NoError(t, err)
	defer db.Close()

	store := NewSessionStore(db, &SessionConfig{Enabled: true})

	// Create sessions on different branches with different tab types
	sessionSpecs := []struct {
		id      string
		tabType string
		branch  string
	}{
		{"ruling-main", "ruling", "main"},
		{"hunting-main", "hunting", "main"},
		{"ruling-feature", "ruling", "feature"},
		{"sandbox-feature", "sandbox", "feature"},
	}

	for _, spec := range sessionSpecs {
		session := &SessionData{
			ID:          spec.id,
			FirstPrompt: "test prompt",
			Model:       "test-model",
			Provider:    "test-provider",
			WorkingDir:  "/tmp",
			TabType:     spec.tabType,
		}
		err := store.SaveSession(session, "github.com", "user", "repo", spec.branch)
		require.NoError(t, err)
	}

	// ruling sessions on main branch only
	rulingMain, err := store.ListSessions("github.com", "user", "repo", "main", "ruling", 0)
	require.NoError(t, err)
	require.Len(t, rulingMain, 1)
	require.Equal(t, "ruling", rulingMain[0].TabType)

	// ruling sessions on feature branch only
	rulingFeature, err := store.ListSessions("github.com", "user", "repo", "feature", "ruling", 0)
	require.NoError(t, err)
	require.Len(t, rulingFeature, 1)
	require.Equal(t, "ruling", rulingFeature[0].TabType)

	// all sessions on feature branch
	allFeature, err := store.ListSessions("github.com", "user", "repo", "feature", "", 0)
	require.NoError(t, err)
	require.Len(t, allFeature, 2)
}
