package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

func TestMigrationFromOldSchema(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "migration_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create old-style database with integer timestamps using raw SQL
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Create old schema tables one by one
	tables := []string{
		"CREATE TABLE repositories (id INTEGER PRIMARY KEY AUTOINCREMENT, host TEXT NOT NULL, org TEXT NOT NULL, project TEXT NOT NULL, UNIQUE(host, org, project))",
		"CREATE TABLE branches (id INTEGER PRIMARY KEY AUTOINCREMENT, repository_id INTEGER NOT NULL, name TEXT NOT NULL, UNIQUE(repository_id, name), FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE)",
		"CREATE TABLE sessions (id TEXT PRIMARY KEY, branch_id INTEGER NOT NULL, created_at INTEGER NOT NULL, last_updated INTEGER NOT NULL, first_prompt TEXT NOT NULL, provider TEXT NOT NULL, model TEXT NOT NULL, working_dir TEXT NOT NULL, FOREIGN KEY (branch_id) REFERENCES branches(id) ON DELETE CASCADE)",
		"CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, sequence INTEGER NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, created_at INTEGER NOT NULL, FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE)",
		"CREATE TABLE workflows (id TEXT PRIMARY KEY, branch_id INTEGER NOT NULL, name TEXT NOT NULL, current_step INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL DEFAULT 'pending', max_retries INTEGER NOT NULL DEFAULT 3, data TEXT NOT NULL DEFAULT '{}', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, FOREIGN KEY (branch_id) REFERENCES branches(id) ON DELETE CASCADE)",
		"CREATE TABLE workflow_steps (id INTEGER PRIMARY KEY AUTOINCREMENT, workflow_id TEXT NOT NULL, step_index INTEGER NOT NULL, name TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', retry_count INTEGER NOT NULL DEFAULT 0, message TEXT NOT NULL DEFAULT '', prompt_template TEXT NOT NULL DEFAULT '', prepare_data TEXT NOT NULL DEFAULT '{}', UNIQUE(workflow_id, step_index), FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE)",
		"CREATE TABLE prompt_history (id INTEGER PRIMARY KEY AUTOINCREMENT, branch_id INTEGER NOT NULL, prompt TEXT NOT NULL, timestamp INTEGER NOT NULL, FOREIGN KEY (branch_id) REFERENCES branches(id) ON DELETE CASCADE)",
		"CREATE TABLE command_history (id INTEGER PRIMARY KEY AUTOINCREMENT, branch_id INTEGER NOT NULL, command TEXT NOT NULL, timestamp INTEGER NOT NULL, FOREIGN KEY (branch_id) REFERENCES branches(id) ON DELETE CASCADE)",
		"CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)",
	}

	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Failed to create table: %v\nSQL: %s", err, stmt)
		}
	}

	// Insert test data with integer timestamps (Unix epoch: 1704067200 = 2024-01-01 00:00:00 UTC)
	inserts := []string{
		"INSERT INTO repositories (id, host, org, project) VALUES (1, 'github.com', 'test', 'project')",
		"INSERT INTO branches (id, repository_id, name) VALUES (1, 1, 'main')",
		"INSERT INTO workflows (id, branch_id, name, created_at, updated_at) VALUES ('wf-1', 1, 'test-workflow', 1704067200, 1704153600)",
		"INSERT INTO workflow_steps (workflow_id, step_index, name) VALUES ('wf-1', 0, 'step-1')",
		"INSERT INTO schema_version (version, applied_at) VALUES (2, 1704067200)",
	}

	for _, stmt := range inserts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Failed to insert data: %v\nSQL: %s", err, stmt)
		}
	}

	db.Close()

	// Now try to open with GORM (this is where it might crash)
	t.Log("Opening old database with GORM...")
	gormDB, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database with GORM: %v", err)
	}
	defer gormDB.Close()

	// Try to load the workflow with old integer timestamps
	t.Log("Loading workflow with old integer timestamps...")
	store := NewWorkflowStore(gormDB)
	workflow, err := store.LoadWorkflow("wf-1")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}

	if workflow == nil {
		t.Fatal("Workflow not found")
	}

	t.Logf("Loaded workflow: ID=%s, Name=%s, CreatedAt=%v, UpdatedAt=%v",
		workflow.ID, workflow.Name, workflow.CreatedAt, workflow.UpdatedAt)

	// Check if timestamps are reasonable (should be 1704067200 = 2024-01-01 00:00:00 UTC)
	if workflow.CreatedAt != 1704067200 {
		t.Errorf("CreatedAt wrong: got %d, expected 1704067200", workflow.CreatedAt)
	}
	if workflow.UpdatedAt != 1704153600 {
		t.Errorf("UpdatedAt wrong: got %d, expected 1704153600", workflow.UpdatedAt)
	}
}
