package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitDB(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "db_test")
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

	// Test Path()
	if db.Path() != dbPath {
		t.Errorf("Expected path %s, got %s", dbPath, db.Path())
	}

	// Test Conn()
	if db.Conn() == nil {
		t.Error("Expected non-nil connection")
	}

	// Test Vacuum()
	if err := db.Vacuum(); err != nil {
		t.Errorf("Vacuum failed: %v", err)
	}
}

func TestGetOrCreateRepository(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo_test")
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

	// Create a repository
	id1, err := db.GetOrCreateRepository("github.com", "test", "project1")
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	if id1 == 0 {
		t.Error("Expected non-zero repository ID")
	}

	// Get the same repository (should return same ID)
	id2, err := db.GetOrCreateRepository("github.com", "test", "project1")
	if err != nil {
		t.Fatalf("Failed to get repository: %v", err)
	}
	if id1 != id2 {
		t.Errorf("Expected same ID %d, got %d", id1, id2)
	}

	// Create a different repository
	id3, err := db.GetOrCreateRepository("github.com", "test", "project2")
	if err != nil {
		t.Fatalf("Failed to create second repository: %v", err)
	}
	if id3 == id1 {
		t.Error("Expected different ID for different repository")
	}
}

func TestGetOrCreateBranch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "branch_test")
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

	// Create a repository first
	repoID, err := db.GetOrCreateRepository("github.com", "test", "project")
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}

	// Create a branch
	branchID1, err := db.GetOrCreateBranch(repoID, "main")
	if err != nil {
		t.Fatalf("Failed to create branch: %v", err)
	}
	if branchID1 == 0 {
		t.Error("Expected non-zero branch ID")
	}

	// Get the same branch (should return same ID)
	branchID2, err := db.GetOrCreateBranch(repoID, "main")
	if err != nil {
		t.Fatalf("Failed to get branch: %v", err)
	}
	if branchID1 != branchID2 {
		t.Errorf("Expected same ID %d, got %d", branchID1, branchID2)
	}

	// Create a different branch
	branchID3, err := db.GetOrCreateBranch(repoID, "develop")
	if err != nil {
		t.Fatalf("Failed to create second branch: %v", err)
	}
	if branchID3 == branchID1 {
		t.Error("Expected different ID for different branch")
	}
}

func TestListRepositories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "list_repos_test")
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

	// Initially empty
	repos, err := db.ListRepositories()
	if err != nil {
		t.Fatalf("Failed to list repositories: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("Expected 0 repositories, got %d", len(repos))
	}

	// Add repositories
	db.GetOrCreateRepository("github.com", "org1", "project1")
	db.GetOrCreateRepository("github.com", "org2", "project2")
	db.GetOrCreateRepository("gitlab.com", "org3", "project3")

	repos, err = db.ListRepositories()
	if err != nil {
		t.Fatalf("Failed to list repositories: %v", err)
	}
	if len(repos) != 3 {
		t.Errorf("Expected 3 repositories, got %d", len(repos))
	}
}

func TestListBranches(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "list_branches_test")
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

	// Create a repository
	repoID, _ := db.GetOrCreateRepository("github.com", "test", "project")

	// Initially empty
	branches, err := db.ListBranches(repoID)
	if err != nil {
		t.Fatalf("Failed to list branches: %v", err)
	}
	if len(branches) != 0 {
		t.Errorf("Expected 0 branches, got %d", len(branches))
	}

	// Add branches
	db.GetOrCreateBranch(repoID, "main")
	db.GetOrCreateBranch(repoID, "develop")
	db.GetOrCreateBranch(repoID, "feature/test")

	branches, err = db.ListBranches(repoID)
	if err != nil {
		t.Fatalf("Failed to list branches: %v", err)
	}
	if len(branches) != 3 {
		t.Errorf("Expected 3 branches, got %d", len(branches))
	}
}

func TestDeleteRepository(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "delete_repo_test")
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

	// Create a repository
	repoID, _ := db.GetOrCreateRepository("github.com", "test", "project")

	// Delete it
	err = db.DeleteRepository(repoID)
	if err != nil {
		t.Fatalf("Failed to delete repository: %v", err)
	}

	// Verify it's gone
	repo, err := db.GetRepository("github.com", "test", "project")
	if err != nil {
		t.Fatalf("Failed to get repository: %v", err)
	}
	if repo != nil {
		t.Error("Expected repository to be deleted")
	}

	// Try to delete non-existent repository
	err = db.DeleteRepository(99999)
	if err == nil {
		t.Error("Expected error when deleting non-existent repository")
	}
}

func TestDeleteBranch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "delete_branch_test")
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

	// Create a repository and branch
	repoID, _ := db.GetOrCreateRepository("github.com", "test", "project")
	branchID, _ := db.GetOrCreateBranch(repoID, "main")

	// Delete the branch
	err = db.DeleteBranch(branchID)
	if err != nil {
		t.Fatalf("Failed to delete branch: %v", err)
	}

	// Verify it's gone
	branch, err := db.GetBranch(repoID, "main")
	if err != nil {
		t.Fatalf("Failed to get branch: %v", err)
	}
	if branch != nil {
		t.Error("Expected branch to be deleted")
	}

	// Try to delete non-existent branch
	err = db.DeleteBranch(99999)
	if err == nil {
		t.Error("Expected error when deleting non-existent branch")
	}
}

func TestGetRepository_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "get_repo_test")
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

	// Try to get non-existent repository
	repo, err := db.GetRepository("github.com", "nonexistent", "project")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if repo != nil {
		t.Error("Expected nil for non-existent repository")
	}
}

func TestGetBranch_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "get_branch_test")
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

	// Create a repository
	repoID, _ := db.GetOrCreateRepository("github.com", "test", "project")

	// Try to get non-existent branch
	branch, err := db.GetBranch(repoID, "nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if branch != nil {
		t.Error("Expected nil for non-existent branch")
	}
}

func TestSchemaVersion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "schema_test")
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

	version, err := db.getSchemaVersion()
	if err != nil {
		t.Fatalf("Failed to get schema version: %v", err)
	}
	if version != SchemaVersion {
		t.Errorf("Expected schema version %d, got %d", SchemaVersion, version)
	}
}

func TestDB_ReopenExisting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reopen_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create and close
	db1, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB first time: %v", err)
	}
	db1.GetOrCreateRepository("github.com", "test", "project")
	db1.Close()

	// Reopen
	db2, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB second time: %v", err)
	}
	defer db2.Close()

	// Verify data persisted
	repo, err := db2.GetRepository("github.com", "test", "project")
	if err != nil {
		t.Fatalf("Failed to get repository: %v", err)
	}
	if repo == nil {
		t.Error("Expected repository to persist across reopens")
	}
}

func TestDB_StatsWithData(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stats_data_test")
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

	// Add some data
	repoID, _ := db.GetOrCreateRepository("github.com", "test", "project")
	db.GetOrCreateBranch(repoID, "main")

	cfg := &HistoryConfig{Enabled: true}
	histStore := NewHistoryStore(db, cfg)
	histStore.AppendPrompt("github.com", "test", "project", "main", "test")
	histStore.AppendCommand("github.com", "test", "project", "main", "/help")

	// Get stats
	stats, err := db.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats["repositories"] != 1 {
		t.Errorf("Expected 1 repository, got %d", stats["repositories"])
	}
	if stats["branches"] != 1 {
		t.Errorf("Expected 1 branch, got %d", stats["branches"])
	}
	if stats["prompt_history"] != 1 {
		t.Errorf("Expected 1 prompt, got %d", stats["prompt_history"])
	}
	if stats["command_history"] != 1 {
		t.Errorf("Expected 1 command, got %d", stats["command_history"])
	}
}
