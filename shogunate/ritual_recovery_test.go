package shogunate

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/afittestide/asimi/storage"
)

// TestRitualRecoveryDetection tests that aborted executions are detected for recovery
func TestRitualRecoveryDetection(t *testing.T) {
	db := setupRitualTestDB(t)

	// Migrate edict table (not done by setupRitualTestDB)
	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate edict table: %v", err)
	}

	// Create test edict
	edict := storage.Edict{
		Intent: "Test recovery",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := edict.EdictID
	edictKey := edict.Key()

	// Create ritual definition
	ritual := &RitualDef{
		Name:        "test-recovery",
		Description: "Test recovery ritual",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "Do step 1"},
			{Name: "step2", Minister: "judge", Act: "Do step 2"},
			{Name: "step3", Minister: "censor", Act: "Do step 3"},
		},
	}

	// Create aborted execution with one completed step
	abortedExecID := "ritual-aborted-123"
	err := db.Create(&RitualExecution{
		ID:          abortedExecID,
		RitualName:  "test-recovery",
		EdictID:     edictID,
		CurrentStep: 1,
		State:       RitualStateAborted,
		Data: storage.JSON{
			"inputs":        map[string]interface{}{"edict_id": edictID},
			"given_context": map[string]interface{}{"step1_result": "completed"},
		},
	}).Error
	if err != nil {
		t.Fatalf("failed to create aborted execution: %v", err)
	}

	// Create step states - first step completed, second step incomplete
	err = db.Create(&RitualStepState{
		ExecutionID: abortedExecID,
		StepIndex:   0,
		Name:        "step1",
		Message:     "Step 1 completed successfully",
		RetryCount:  0,
	}).Error
	if err != nil {
		t.Fatalf("failed to create step state 1: %v", err)
	}

	err = db.Create(&RitualStepState{
		ExecutionID: abortedExecID,
		StepIndex:   1,
		Name:        "step2",
		Message:     "", // Incomplete
		RetryCount:  0,
	}).Error
	if err != nil {
		t.Fatalf("failed to create step state 2: %v", err)
	}

	// Setup ritual runner
	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("failed to register ritual: %v", err)
	}

	runner := NewRitualRunner(
		registry,
		nil, // getMinister - nil means no zhengming will be requested
		nil, // publishEvent
		db,
		nil, // runner
		slog.Default(),
	)

	// Start ritual - should detect aborted execution
	ctx := context.Background()
	exec, err := runner.Start(ctx, "test-recovery", edictKey, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("failed to start ritual: %v", err)
	}

	// Verify recovery mode is enabled (even without zhengming, recovery is prepared)
	if !exec.RecoveryMode {
		t.Error("expected RecoveryMode to be true")
	}

	// Verify it's resuming from step 1 (first incomplete step)
	if exec.CurrentStep != 1 {
		t.Errorf("expected CurrentStep to be 1, got %d", exec.CurrentStep)
	}

	// Verify previous execution ID is tracked
	if exec.PreviousExecutionID != abortedExecID {
		t.Errorf("expected PreviousExecutionID to be %q, got %q", abortedExecID, exec.PreviousExecutionID)
	}

	// Verify given_context is preserved
	givenContext, ok := exec.Data["given_context"].(map[string]interface{})
	if !ok {
		t.Fatal("expected given_context to be preserved")
	}
	if givenContext["step1_result"] != "completed" {
		t.Errorf("expected step1_result to be preserved, got %v", givenContext["step1_result"])
	}
}

// TestRitualRecoveryNoAbortedExecution tests that fresh start occurs when no aborted execution exists
func TestRitualRecoveryNoAbortedExecution(t *testing.T) {
	db := setupRitualTestDB(t)

	// Migrate edict table
	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate edict table: %v", err)
	}

	// Create test edict
	edict := storage.Edict{
		Intent: "Test fresh start",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictKey := edict.Key()

	// Create ritual definition
	ritual := &RitualDef{
		Name:        "test-fresh",
		Description: "Test fresh start ritual",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "Do step 1"},
			{Name: "step2", Minister: "judge", Act: "Do step 2"},
		},
	}

	// Setup ritual runner
	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("failed to register ritual: %v", err)
	}

	runner := NewRitualRunner(
		registry,
		nil,
		nil,
		db,
		nil,
		slog.Default(),
	)

	// Start ritual - should start fresh (no aborted execution)
	ctx := context.Background()
	exec, err := runner.Start(ctx, "test-fresh", edictKey, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("failed to start ritual: %v", err)
	}

	// Verify NOT in recovery mode
	if exec.RecoveryMode {
		t.Error("expected RecoveryMode to be false for fresh start")
	}

	// Verify it starts from step 0
	if exec.CurrentStep != 0 {
		t.Errorf("expected CurrentStep to be 0, got %d", exec.CurrentStep)
	}

	// Verify no previous execution ID
	if exec.PreviousExecutionID != "" {
		t.Errorf("expected PreviousExecutionID to be empty, got %q", exec.PreviousExecutionID)
	}
}

// TestRitualRecoveryAllStepsComplete tests that recovery is skipped if all steps completed
func TestRitualRecoveryAllStepsComplete(t *testing.T) {
	db := setupRitualTestDB(t)

	// Migrate edict table
	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate edict table: %v", err)
	}

	// Create test edict
	edict := storage.Edict{
		Intent: "Test all complete",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := edict.EdictID
	edictKey := edict.Key()

	// Create ritual definition
	ritual := &RitualDef{
		Name:        "test-all-complete",
		Description: "Test all complete ritual",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "Do step 1"},
			{Name: "step2", Minister: "judge", Act: "Do step 2"},
		},
	}

	// Create aborted execution with ALL steps completed
	abortedExecID := "ritual-aborted-complete-123"
	err := db.Create(&RitualExecution{
		ID:          abortedExecID,
		RitualName:  "test-all-complete",
		EdictID:     edictID,
		CurrentStep: 2,
		State:       RitualStateAborted,
		Data:        storage.JSON{"inputs": map[string]interface{}{"edict_id": edictID}},
	}).Error
	if err != nil {
		t.Fatalf("failed to create aborted execution: %v", err)
	}

	// Create step states - ALL steps completed
	err = db.Create(&RitualStepState{
		ExecutionID: abortedExecID,
		StepIndex:   0,
		Name:        "step1",
		Message:     "Step 1 completed",
		RetryCount:  0,
	}).Error
	if err != nil {
		t.Fatalf("failed to create step state 1: %v", err)
	}

	err = db.Create(&RitualStepState{
		ExecutionID: abortedExecID,
		StepIndex:   1,
		Name:        "step2",
		Message:     "Step 2 completed",
		RetryCount:  0,
	}).Error
	if err != nil {
		t.Fatalf("failed to create step state 2: %v", err)
	}

	// Setup ritual runner
	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("failed to register ritual: %v", err)
	}

	runner := NewRitualRunner(
		registry,
		nil,
		nil,
		db,
		nil,
		slog.Default(),
	)

	// Start ritual - should start fresh since all steps completed
	ctx := context.Background()
	exec, err := runner.Start(ctx, "test-all-complete", edictKey, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("failed to start ritual: %v", err)
	}

	// Verify NOT in recovery mode (all steps were complete)
	if exec.RecoveryMode {
		t.Error("expected RecoveryMode to be false when all steps completed")
	}

	// Verify it starts from step 0
	if exec.CurrentStep != 0 {
		t.Errorf("expected CurrentStep to be 0, got %d", exec.CurrentStep)
	}
}

// TestRitualRecoveryWithRetry tests that steps with retries are considered incomplete
func TestRitualRecoveryWithRetry(t *testing.T) {
	db := setupRitualTestDB(t)

	// Migrate edict table
	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate edict table: %v", err)
	}

	// Create test edict
	edict := storage.Edict{
		Intent: "Test retry",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := edict.EdictID
	edictKey := edict.Key()

	// Create ritual definition
	ritual := &RitualDef{
		Name:        "test-retry",
		Description: "Test retry ritual",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "Do step 1"},
			{Name: "step2", Minister: "judge", Act: "Do step 2"},
			{Name: "step3", Minister: "censor", Act: "Do step 3"},
		},
	}

	// Create aborted execution
	abortedExecID := "ritual-aborted-retry-123"
	err := db.Create(&RitualExecution{
		ID:          abortedExecID,
		RitualName:  "test-retry",
		EdictID:     edictID,
		CurrentStep: 2,
		State:       RitualStateAborted,
		Data:        storage.JSON{"inputs": map[string]interface{}{"edict_id": edictID}},
	}).Error
	if err != nil {
		t.Fatalf("failed to create aborted execution: %v", err)
	}

	// Create step states - step1 completed, step2 has retries (incomplete), step3 not reached
	err = db.Create(&RitualStepState{
		ExecutionID: abortedExecID,
		StepIndex:   0,
		Name:        "step1",
		Message:     "Step 1 completed",
		RetryCount:  0,
	}).Error
	if err != nil {
		t.Fatalf("failed to create step state 1: %v", err)
	}

	err = db.Create(&RitualStepState{
		ExecutionID: abortedExecID,
		StepIndex:   1,
		Name:        "step2",
		Message:     "Step 2 failed",
		RetryCount:  2, // Has retries - considered incomplete
	}).Error
	if err != nil {
		t.Fatalf("failed to create step state 2: %v", err)
	}

	// Setup ritual runner
	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("failed to register ritual: %v", err)
	}

	runner := NewRitualRunner(
		registry,
		nil,
		nil,
		db,
		nil,
		slog.Default(),
	)

	// Start ritual - should recover from step 2 (has retries)
	ctx := context.Background()
	exec, err := runner.Start(ctx, "test-retry", edictKey, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("failed to start ritual: %v", err)
	}

	// Verify recovery mode is enabled
	if !exec.RecoveryMode {
		t.Error("expected RecoveryMode to be true")
	}

	// Verify it's resuming from step 2 (step with retries)
	if exec.CurrentStep != 1 {
		t.Errorf("expected CurrentStep to be 1 (step with retries), got %d", exec.CurrentStep)
	}
}

// TestRitualRecoveryLogMessage tests that recovery logs appropriate messages
func TestRitualRecoveryLogMessage(t *testing.T) {
	db := setupRitualTestDB(t)

	// Migrate edict table
	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate edict table: %v", err)
	}

	// Create test edict
	edict := storage.Edict{
		Intent: "Test log",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := edict.EdictID
	edictKey := edict.Key()

	// Create ritual definition
	ritual := &RitualDef{
		Name:        "test-log",
		Description: "Test log ritual",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "Do step 1"},
			{Name: "step2", Minister: "judge", Act: "Do step 2"},
		},
	}

	// Create aborted execution
	abortedExecID := "ritual-aborted-log-123"
	err := db.Create(&RitualExecution{
		ID:          abortedExecID,
		RitualName:  "test-log",
		EdictID:     edictID,
		CurrentStep: 1,
		State:       RitualStateAborted,
		Data:        storage.JSON{"inputs": map[string]interface{}{"edict_id": edictID}},
	}).Error
	if err != nil {
		t.Fatalf("failed to create aborted execution: %v", err)
	}

	// Create step state - first step completed
	err = db.Create(&RitualStepState{
		ExecutionID: abortedExecID,
		StepIndex:   0,
		Name:        "step1",
		Message:     "Step 1 completed",
		RetryCount:  0,
	}).Error
	if err != nil {
		t.Fatalf("failed to create step state: %v", err)
	}

	// Setup ritual runner
	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("failed to register ritual: %v", err)
	}

	// Use a test logger to capture log messages
	var logMessages []string
	testLogger := slog.New(slog.NewTextHandler(&testWriter{messages: &logMessages}, nil))

	runner := NewRitualRunner(
		registry,
		nil,
		nil,
		db,
		nil,
		testLogger,
	)

	// Start ritual
	ctx := context.Background()
	_, err = runner.Start(ctx, "test-log", edictKey, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("failed to start ritual: %v", err)
	}

	// Verify recovery log messages
	foundRecoveryLog := false
	for _, msg := range logMessages {
		if strings.Contains(msg, "recovery") && strings.Contains(msg, "aborted") {
			foundRecoveryLog = true
			break
		}
	}
	if !foundRecoveryLog {
		t.Error("expected log message about recovery from aborted execution")
	}
}

// testWriter is a simple io.Writer that captures log messages
type testWriter struct {
	messages *[]string
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	*w.messages = append(*w.messages, string(p))
	return len(p), nil
}
