package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkflowStore(t *testing.T) {
	// Create a temporary database
	tmpDir, err := os.MkdirTemp("", "workflow_test")
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

	store := NewWorkflowStore(db)

	// Test SaveWorkflow
	now := time.Now().Unix()
	workflow := &WorkflowData{
		ID:          "test-workflow-1",
		Name:        "test-workflow",
		CurrentStep: 0,
		State:       WorkflowStatePending,
		MaxRetries:  3,
		Data:        `{"key": "value"}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = store.SaveWorkflow(workflow, "github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to save workflow: %v", err)
	}

	// Test LoadWorkflow
	loaded, err := store.LoadWorkflow("test-workflow-1")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}

	if loaded == nil {
		t.Fatal("Loaded workflow is nil")
	}

	if loaded.ID != workflow.ID {
		t.Errorf("Expected ID %s, got %s", workflow.ID, loaded.ID)
	}

	if loaded.Name != workflow.Name {
		t.Errorf("Expected Name %s, got %s", workflow.Name, loaded.Name)
	}

	if loaded.State != workflow.State {
		t.Errorf("Expected State %s, got %s", workflow.State, loaded.State)
	}

	// Test SaveWorkflowStep
	step := &WorkflowStepData{
		WorkflowID:     "test-workflow-1",
		StepIndex:      0,
		Name:           "step-1",
		Status:         StepStatusPending,
		RetryCount:     0,
		Message:        "",
		PromptTemplate: "Test prompt",
		PrepareData:    "{}",
	}

	err = store.SaveWorkflowStep(step)
	if err != nil {
		t.Fatalf("Failed to save workflow step: %v", err)
	}

	// Test LoadWorkflowSteps
	steps, err := store.LoadWorkflowSteps("test-workflow-1")
	if err != nil {
		t.Fatalf("Failed to load workflow steps: %v", err)
	}

	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	if steps[0].Name != "step-1" {
		t.Errorf("Expected step name 'step-1', got '%s'", steps[0].Name)
	}

	// Test UpdateWorkflowState
	err = store.UpdateWorkflowState("test-workflow-1", WorkflowStateRunning)
	if err != nil {
		t.Fatalf("Failed to update workflow state: %v", err)
	}

	loaded, err = store.LoadWorkflow("test-workflow-1")
	if err != nil {
		t.Fatalf("Failed to load workflow after state update: %v", err)
	}

	if loaded.State != WorkflowStateRunning {
		t.Errorf("Expected state Running, got %s", loaded.State)
	}

	// Test UpdateStepStatus
	err = store.UpdateStepStatus("test-workflow-1", 0, StepStatusCompleted, "done")
	if err != nil {
		t.Fatalf("Failed to update step status: %v", err)
	}

	steps, err = store.LoadWorkflowSteps("test-workflow-1")
	if err != nil {
		t.Fatalf("Failed to load steps after status update: %v", err)
	}

	if steps[0].Status != StepStatusCompleted {
		t.Errorf("Expected step status Completed, got %s", steps[0].Status)
	}

	if steps[0].Message != "done" {
		t.Errorf("Expected step message 'done', got '%s'", steps[0].Message)
	}

	// Test IncrementStepRetryCount
	err = store.IncrementStepRetryCount("test-workflow-1", 0)
	if err != nil {
		t.Fatalf("Failed to increment retry count: %v", err)
	}

	steps, err = store.LoadWorkflowSteps("test-workflow-1")
	if err != nil {
		t.Fatalf("Failed to load steps after retry increment: %v", err)
	}

	if steps[0].RetryCount != 1 {
		t.Errorf("Expected retry count 1, got %d", steps[0].RetryCount)
	}

	// Test ListWorkflows
	workflows, err := store.ListWorkflows("github.com", "test", "project", "main", 10)
	if err != nil {
		t.Fatalf("Failed to list workflows: %v", err)
	}

	if len(workflows) != 1 {
		t.Errorf("Expected 1 workflow, got %d", len(workflows))
	}

	// Test ListActiveWorkflows
	activeWorkflows, err := store.ListActiveWorkflows("github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to list active workflows: %v", err)
	}

	if len(activeWorkflows) != 1 {
		t.Errorf("Expected 1 active workflow, got %d", len(activeWorkflows))
	}

	// Complete the workflow and check it's no longer active
	err = store.UpdateWorkflowState("test-workflow-1", WorkflowStateCompleted)
	if err != nil {
		t.Fatalf("Failed to complete workflow: %v", err)
	}

	activeWorkflows, err = store.ListActiveWorkflows("github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to list active workflows after completion: %v", err)
	}

	if len(activeWorkflows) != 0 {
		t.Errorf("Expected 0 active workflows after completion, got %d", len(activeWorkflows))
	}

	// Test DeleteWorkflow
	err = store.DeleteWorkflow("test-workflow-1")
	if err != nil {
		t.Fatalf("Failed to delete workflow: %v", err)
	}

	loaded, err = store.LoadWorkflow("test-workflow-1")
	if err != nil {
		t.Fatalf("Error loading deleted workflow: %v", err)
	}

	if loaded != nil {
		t.Error("Expected workflow to be deleted")
	}

	// Steps should also be deleted (CASCADE)
	steps, err = store.LoadWorkflowSteps("test-workflow-1")
	if err != nil {
		t.Fatalf("Error loading steps of deleted workflow: %v", err)
	}

	if len(steps) != 0 {
		t.Errorf("Expected 0 steps after workflow deletion, got %d", len(steps))
	}
}

func TestWorkflowStoreUpdateData(t *testing.T) {
	// Create a temporary database
	tmpDir, err := os.MkdirTemp("", "workflow_data_test")
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

	store := NewWorkflowStore(db)

	// Create workflow
	now := time.Now().Unix()
	workflow := &WorkflowData{
		ID:          "test-workflow-data",
		Name:        "test-workflow",
		CurrentStep: 0,
		State:       WorkflowStatePending,
		MaxRetries:  3,
		Data:        `{}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = store.SaveWorkflow(workflow, "github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to save workflow: %v", err)
	}

	// Update data
	newData := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	err = store.UpdateWorkflowData("test-workflow-data", newData)
	if err != nil {
		t.Fatalf("Failed to update workflow data: %v", err)
	}

	// Load and verify
	loaded, err := store.LoadWorkflow("test-workflow-data")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}

	if loaded.Data != `{"key1":"value1","key2":"value2"}` {
		t.Errorf("Unexpected data: %s", loaded.Data)
	}
}

func TestSchemaMigration(t *testing.T) {
	// Create a temporary database
	tmpDir, err := os.MkdirTemp("", "migration_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize DB - this should create schema v2
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}

	// Verify schema version
	version, err := db.getSchemaVersion()
	if err != nil {
		t.Fatalf("Failed to get schema version: %v", err)
	}

	if version != SchemaVersion {
		t.Errorf("Expected schema version %d, got %d", SchemaVersion, version)
	}

	// Verify workflow tables exist using GORM
	var count int64
	err = db.conn.Model(&WorkflowData{}).Count(&count).Error
	if err != nil {
		t.Errorf("workflows table should exist: %v", err)
	}

	err = db.conn.Model(&WorkflowStepData{}).Count(&count).Error
	if err != nil {
		t.Errorf("workflow_steps table should exist: %v", err)
	}

	db.Close()
}

func TestHistoryStore(t *testing.T) {
	// Create a temporary database
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

	if entries[0].Content != "test prompt 1" {
		t.Errorf("Expected 'test prompt 1', got '%s'", entries[0].Content)
	}

	// Test AppendCommand
	err = store.AppendCommand("github.com", "test", "project", "main", "/help")
	if err != nil {
		t.Fatalf("Failed to append command: %v", err)
	}

	// Test LoadCommandHistory
	cmdEntries, err := store.LoadCommandHistory("github.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Failed to load command history: %v", err)
	}

	if len(cmdEntries) != 1 {
		t.Errorf("Expected 1 command, got %d", len(cmdEntries))
	}

	// Test ClearPromptHistory
	err = store.ClearPromptHistory("github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to clear prompt history: %v", err)
	}

	entries, err = store.LoadPromptHistory("github.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Failed to load prompt history after clear: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("Expected 0 prompts after clear, got %d", len(entries))
	}
}

func TestSessionStore(t *testing.T) {
	// Create a temporary database
	tmpDir, err := os.MkdirTemp("", "session_test")
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

	cfg := &SessionConfig{
		Enabled:     true,
		MaxSessions: 100,
		MaxAgeDays:  30,
	}
	store := NewSessionStore(db, cfg)

	// Test SaveSession
	session := &SessionData{
		ID:           "test-session-1",
		CreatedAt:    time.Now(),
		LastUpdated:  time.Now(),
		FirstPrompt:  "Hello world",
		Provider:     "anthropic",
		Model:        "claude-3",
		WorkingDir:   "/tmp/test",
		ProjectSlug:  "github.com/test/project",
		Messages:     nil,
		ContextFiles: make(map[string]string),
	}

	err = store.SaveSession(session, "github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Test LoadSession
	loaded, host, org, project, branch, err := store.LoadSession("test-session-1")
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	if loaded.ID != session.ID {
		t.Errorf("Expected ID %s, got %s", session.ID, loaded.ID)
	}

	if loaded.FirstPrompt != session.FirstPrompt {
		t.Errorf("Expected FirstPrompt %s, got %s", session.FirstPrompt, loaded.FirstPrompt)
	}

	if host != "github.com" || org != "test" || project != "project" || branch != "main" {
		t.Errorf("Unexpected repo info: %s/%s/%s@%s", host, org, project, branch)
	}

	// Test ListSessions
	sessions, err := store.ListSessions("github.com", "test", "project", "main", 10)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(sessions))
	}

	// Test DeleteSession
	err = store.DeleteSession("test-session-1")
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	_, _, _, _, _, err = store.LoadSession("test-session-1")
	if err == nil {
		t.Error("Expected error loading deleted session")
	}
}

func TestDBStats(t *testing.T) {
	// Create a temporary database
	tmpDir, err := os.MkdirTemp("", "stats_test")
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

	// Get stats
	stats, err := db.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	// Verify all expected keys exist
	expectedKeys := []string{"repositories", "branches", "sessions", "messages", "prompt_history", "command_history", "workflows", "workflow_steps"}
	for _, key := range expectedKeys {
		if _, ok := stats[key]; !ok {
			t.Errorf("Expected key %s in stats", key)
		}
	}
}

func TestWorkflowStore_UpdateCurrentStep(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "workflow_step_test")
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

	store := NewWorkflowStore(db)

	// Create workflow
	now := time.Now().Unix()
	workflow := &WorkflowData{
		ID:          "test-current-step",
		Name:        "test-workflow",
		CurrentStep: 0,
		State:       WorkflowStatePending,
		MaxRetries:  3,
		Data:        `{}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = store.SaveWorkflow(workflow, "github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to save workflow: %v", err)
	}

	// Update current step
	err = store.UpdateWorkflowCurrentStep("test-current-step", 2)
	if err != nil {
		t.Fatalf("Failed to update current step: %v", err)
	}

	// Verify
	loaded, err := store.LoadWorkflow("test-current-step")
	if err != nil {
		t.Fatalf("Failed to load workflow: %v", err)
	}

	if loaded.CurrentStep != 2 {
		t.Errorf("Expected current step 2, got %d", loaded.CurrentStep)
	}
}

func TestWorkflowStore_LoadNonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "workflow_nonexistent_test")
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

	store := NewWorkflowStore(db)

	// Load non-existent workflow
	loaded, err := store.LoadWorkflow("nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if loaded != nil {
		t.Error("Expected nil for non-existent workflow")
	}
}

func TestWorkflowStore_DeleteNonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "workflow_delete_nonexistent_test")
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

	store := NewWorkflowStore(db)

	// Try to delete non-existent workflow
	err = store.DeleteWorkflow("nonexistent")
	if err == nil {
		t.Error("Expected error when deleting non-existent workflow")
	}
}

func TestWorkflowStore_ListWorkflowsEmpty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "workflow_listempty_test")
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

	store := NewWorkflowStore(db)

	// List from non-existent repo
	workflows, err := store.ListWorkflows("nonexistent.com", "test", "project", "main", 10)
	if err != nil {
		t.Fatalf("Failed to list workflows: %v", err)
	}
	if len(workflows) != 0 {
		t.Errorf("Expected 0 workflows, got %d", len(workflows))
	}
}

func TestWorkflowStore_ListActiveWorkflowsEmpty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "workflow_listactive_empty_test")
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

	store := NewWorkflowStore(db)

	// List from non-existent repo
	workflows, err := store.ListActiveWorkflows("nonexistent.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to list active workflows: %v", err)
	}
	if len(workflows) != 0 {
		t.Errorf("Expected 0 workflows, got %d", len(workflows))
	}
}

func TestWorkflowStore_SaveStepUpdate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "workflow_savstep_test")
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

	store := NewWorkflowStore(db)

	// Create workflow
	now := time.Now().Unix()
	workflow := &WorkflowData{
		ID:          "test-step-update",
		Name:        "test-workflow",
		CurrentStep: 0,
		State:       WorkflowStatePending,
		MaxRetries:  3,
		Data:        `{}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	store.SaveWorkflow(workflow, "github.com", "test", "project", "main")

	// Create step
	step := &WorkflowStepData{
		WorkflowID:     "test-step-update",
		StepIndex:      0,
		Name:           "step-1",
		Status:         StepStatusPending,
		RetryCount:     0,
		Message:        "",
		PromptTemplate: "Initial",
		PrepareData:    "{}",
	}
	err = store.SaveWorkflowStep(step)
	if err != nil {
		t.Fatalf("Failed to save step: %v", err)
	}

	// Update the same step
	step.Status = StepStatusCompleted
	step.Message = "Done"
	step.PromptTemplate = "Updated"
	err = store.SaveWorkflowStep(step)
	if err != nil {
		t.Fatalf("Failed to update step: %v", err)
	}

	// Verify
	steps, err := store.LoadWorkflowSteps("test-step-update")
	if err != nil {
		t.Fatalf("Failed to load steps: %v", err)
	}
	if len(steps) != 1 {
		t.Errorf("Expected 1 step, got %d", len(steps))
	}
	if steps[0].Status != StepStatusCompleted {
		t.Errorf("Expected status Completed, got %s", steps[0].Status)
	}
	if steps[0].PromptTemplate != "Updated" {
		t.Errorf("Expected template 'Updated', got '%s'", steps[0].PromptTemplate)
	}
}

func TestWorkflowStore_LoadStepsEmpty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "workflow_loadsteps_empty_test")
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

	store := NewWorkflowStore(db)

	// Load steps for non-existent workflow
	steps, err := store.LoadWorkflowSteps("nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("Expected 0 steps, got %d", len(steps))
	}
}

func TestWorkflowStore_ListWithLimit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "workflow_listlimit_test")
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

	store := NewWorkflowStore(db)

	// Create multiple workflows
	for i := 0; i < 5; i++ {
		now := time.Now().Unix()
		workflow := &WorkflowData{
			ID:          "workflow-" + string(rune('A'+i)),
			Name:        "test-workflow",
			CurrentStep: 0,
			State:       WorkflowStatePending,
			MaxRetries:  3,
			Data:        `{}`,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		store.SaveWorkflow(workflow, "github.com", "test", "project", "main")
		time.Sleep(10 * time.Millisecond)
	}

	// List with limit
	workflows, err := store.ListWorkflows("github.com", "test", "project", "main", 3)
	if err != nil {
		t.Fatalf("Failed to list workflows: %v", err)
	}
	if len(workflows) != 3 {
		t.Errorf("Expected 3 workflows, got %d", len(workflows))
	}

	// List without limit
	workflows, err = store.ListWorkflows("github.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Failed to list workflows: %v", err)
	}
	if len(workflows) != 5 {
		t.Errorf("Expected 5 workflows, got %d", len(workflows))
	}
}
