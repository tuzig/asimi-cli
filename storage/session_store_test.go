package storage

import (
	"encoding/json"
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

// TestListAllSessions_HostOrgProjectFilter tests that host/org/project filters work
func TestListAllSessions_HostOrgProjectFilter(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "session_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := InitDB(tmpFile.Name())
	require.NoError(t, err)
	defer db.Close()

	store := NewSessionStore(db, &SessionConfig{Enabled: true})

	// Create sessions across two different repos
	sessionSpecs := []struct {
		id      string
		host    string
		org     string
		project string
	}{
		{"s1", "github.com", "acme", "alpha"},
		{"s2", "github.com", "acme", "beta"},
		{"s3", "github.com", "other", "alpha"},
		{"s4", "gitlab.com", "acme", "alpha"},
	}

	for _, spec := range sessionSpecs {
		session := &SessionData{
			ID:          spec.id,
			FirstPrompt: "test",
			Model:       "m",
			Provider:    "p",
			WorkingDir:  "/tmp",
			TabType:     "ruling",
		}
		err := store.SaveSession(session, spec.host, spec.org, spec.project, "main")
		require.NoError(t, err)
	}

	// No filters — all sessions
	all, err := store.ListAllSessions(0, "", "", "")
	require.NoError(t, err)
	require.Len(t, all, 4)

	// Filter by org
	acmeOnly, err := store.ListAllSessions(0, "", "acme", "")
	require.NoError(t, err)
	require.Len(t, acmeOnly, 3)
	for _, s := range acmeOnly {
		require.Contains(t, s.ProjectSlug, "acme")
	}

	// Filter by host
	ghOnly, err := store.ListAllSessions(0, "github.com", "", "")
	require.NoError(t, err)
	require.Len(t, ghOnly, 3)

	// Filter by project
	alphaOnly, err := store.ListAllSessions(0, "", "", "alpha")
	require.NoError(t, err)
	require.Len(t, alphaOnly, 3)
	for _, s := range alphaOnly {
		require.Contains(t, s.ProjectSlug, "alpha")
	}

	// Filter by host+org+project
	specific, err := store.ListAllSessions(0, "github.com", "acme", "alpha")
	require.NoError(t, err)
	require.Len(t, specific, 1)
	require.Equal(t, "github.com/acme/alpha", specific[0].ProjectSlug)

	// With limit
	limited, err := store.ListAllSessions(2, "", "", "")
	require.NoError(t, err)
	require.Len(t, limited, 2)
}

// TestSearchMessages_HostOrgProjectFilter tests that search can be scoped
func TestSearchMessages_HostOrgProjectFilter(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "session_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := InitDB(tmpFile.Name())
	require.NoError(t, err)
	defer db.Close()

	store := NewSessionStore(db, &SessionConfig{Enabled: true})

	// Save sessions with messages in two different repos.
	// Messages are stored as JSON in the content column, so we need
	// to provide actual JSON message data for SearchMessages to find them.
	alphaMessages := json.RawMessage(`[{"role":"user","content":"hello from alpha"}]`)
	betaMessages := json.RawMessage(`[{"role":"user","content":"hello from beta"}]`)

	alphaSession := &SessionData{
		ID:          "search-alpha",
		FirstPrompt: "hello from alpha",
		Model:       "m",
		Provider:    "p",
		WorkingDir:  "/tmp",
		TabType:     "ruling",
		Messages:    alphaMessages,
	}
	err = store.SaveSession(alphaSession, "github.com", "acme", "alpha", "main")
	require.NoError(t, err)

	betaSession := &SessionData{
		ID:          "search-beta",
		FirstPrompt: "hello from beta",
		Model:       "m",
		Provider:    "p",
		WorkingDir:  "/tmp",
		TabType:     "ruling",
		Messages:    betaMessages,
	}
	err = store.SaveSession(betaSession, "github.com", "acme", "beta", "main")
	require.NoError(t, err)

	// Search across all repos
	allResults, err := store.SearchMessages("hello", 10, "", "", "")
	require.NoError(t, err)
	require.True(t, len(allResults) >= 2, "expected at least 2 results, got %d", len(allResults))

	// Scope to "alpha" project only
	alphaResults, err := store.SearchMessages("hello", 10, "", "", "alpha")
	require.NoError(t, err)
	require.Len(t, alphaResults, 1)
	require.Equal(t, "alpha", alphaResults[0].Project)

	// Scope by org
	acmeResults, err := store.SearchMessages("hello", 10, "", "acme", "")
	require.NoError(t, err)
	require.Len(t, acmeResults, 2)

	// Verify Host field is populated
	for _, r := range acmeResults {
		require.Equal(t, "github.com", r.Host)
	}
}

// TestCleanupOldSessions_BranchScoping tests that cleanup can be scoped to a branch
func TestCleanupOldSessions_BranchScoping(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "session_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := InitDB(tmpFile.Name())
	require.NoError(t, err)
	defer db.Close()

	// Use a large MaxSessions so the constructor auto-cleanup doesn't delete anything
	cfg := &SessionConfig{Enabled: true, MaxSessions: 100}
	store := NewSessionStore(db, cfg)

	// Create 3 sessions on "main" and 3 on "feature"
	for i := 0; i < 3; i++ {
		session := &SessionData{
			ID:          fmt.Sprintf("main-s%d", i),
			FirstPrompt: fmt.Sprintf("main prompt %d", i),
			Model:       "m",
			Provider:    "p",
			WorkingDir:  "/tmp",
			TabType:     "ruling",
		}
		err := store.SaveSession(session, "github.com", "acme", "repo", "main")
		require.NoError(t, err)

		session2 := &SessionData{
			ID:          fmt.Sprintf("feature-s%d", i),
			FirstPrompt: fmt.Sprintf("feature prompt %d", i),
			Model:       "m",
			Provider:    "p",
			WorkingDir:  "/tmp",
			TabType:     "ruling",
		}
		err = store.SaveSession(session2, "github.com", "acme", "repo", "feature")
		require.NoError(t, err)
	}

	// Look up the branch_id for "main"
	var mainBranchID int64
	err = db.conn.QueryRow(
		"SELECT b.id FROM branches b JOIN repositories r ON b.repository_id = r.id WHERE r.host = ? AND r.org = ? AND r.project = ? AND b.name = ?",
		"github.com", "acme", "repo", "main",
	).Scan(&mainBranchID)
	require.NoError(t, err)

	// Now reduce MaxSessions and cleanup scoped to main branch
	cfg.MaxSessions = 2
	err = store.CleanupOldSessions(mainBranchID)
	require.NoError(t, err)

	// Verify main has 2 sessions
	mainSessions, err := store.ListSessions("github.com", "acme", "repo", "main", "", 0)
	require.NoError(t, err)
	require.Len(t, mainSessions, 2)

	// Verify feature is untouched (still 3)
	featureSessions, err := store.ListSessions("github.com", "acme", "repo", "feature", "", 0)
	require.NoError(t, err)
	require.Len(t, featureSessions, 3)

	// Cleanup with branchID=0 — global, keeps only 2 across all branches
	cfg.MaxSessions = 2
	err = store.CleanupOldSessions(0)
	require.NoError(t, err)

	allSessions, err := store.ListAllSessions(0, "", "", "")
	require.NoError(t, err)
	require.Len(t, allSessions, 2)
}
