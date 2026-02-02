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
	workflow := &WorkflowData{
		ID:          "test-workflow-1",
		Name:        "test-workflow",
		CurrentStep: 0,
		State:       WorkflowStatePending,
		MaxRetries:  3,
		Data:        `{"key": "value"}`,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
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
	workflow := &WorkflowData{
		ID:          "test-workflow-data",
		Name:        "test-workflow",
		CurrentStep: 0,
		State:       WorkflowStatePending,
		MaxRetries:  3,
		Data:        `{}`,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
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

	// Verify workflow tables exist
	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM workflows").Scan(&count)
	if err != nil {
		t.Errorf("workflows table should exist: %v", err)
	}

	err = db.conn.QueryRow("SELECT COUNT(*) FROM workflow_steps").Scan(&count)
	if err != nil {
		t.Errorf("workflow_steps table should exist: %v", err)
	}

	db.Close()
}
