package shogunate

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "modernc.org/sqlite"
)

// --- Test helpers for promptForAbortedRituals tests ---

// setupRecoveryTestDB creates a test database with all tables needed for recovery tests.
func setupRecoveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := tmpDir + "/recovery_test.db"

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to initialize gorm: %v", err)
	}

	err = db.AutoMigrate(
		&RitualExecution{},
		&RitualStepState{},
		&storage.Edict{},
		&storage.Seal{},
		&storage.Zhengming{},
	)
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	return db
}

// mockChancellorMinister implements Minister plus the zhengming interfaces
// for use in promptForAbortedRituals tests.
type mockChancellorMinister struct {
	requestZhengmingFn func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error)
	waitForZhengmingFn func(ctx context.Context, requestID string) (string, error)
	MinisterBase
	id string
}

// newMockChancellor creates a mock chancellor with optional zhengming function overrides.
func newMockChancellor(requestFn func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error), waitFn func(ctx context.Context, requestID string) (string, error)) *mockChancellorMinister {
	return &mockChancellorMinister{
		MinisterBase:       MinisterBase{logger: slog.Default()},
		id:                 "chancellor",
		requestZhengmingFn: requestFn,
		waitForZhengmingFn: waitFn,
	}
}

func (m *mockChancellorMinister) ID() string              { return m.id }
func (m *mockChancellorMinister) SystemPrompt() string    { return "" }
func (m *mockChancellorMinister) Tools() []Tool           { return nil }
func (m *mockChancellorMinister) Run(ctx context.Context) { <-ctx.Done() }

func (m *mockChancellorMinister) RequestZhengming(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
	if m.requestZhengmingFn != nil {
		return m.requestZhengmingFn(key, questions, priority)
	}
	return "req-1", nil
}

func (m *mockChancellorMinister) WaitForZhengming(ctx context.Context, requestID string) (string, error) {
	if m.waitForZhengmingFn != nil {
		return m.waitForZhengmingFn(ctx, requestID)
	}
	return "", nil
}

// newTestRitualGuard creates a RitualGuard wired for promptForAbortedRituals tests.
func newTestRitualGuard(t *testing.T, db *gorm.DB, getMinister func(id string) Minister) *RitualGuard {
	t.Helper()
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	rg := NewRitualGuard(RitualGuardOpts{
		Base:         base,
		GetMinister:  getMinister,
		StreamingCtx: func() context.Context { return context.Background() },
	})
	return rg
}

// --- Recovery detection tests (RitualRunner.Start) ---

// TestRitualRecoveryDetection tests that aborted executions are detected for recovery
func TestRitualRecoveryDetection(t *testing.T) {
	db := setupRitualTestDB(t)

	// Migrate edict table (not done by setupRitualTestDB)
	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate edict table: %v", err)
	}

	// Create test edict
	edict := storage.Edict{
		Intent:   "Test recovery",
		Username: "testuser",
		Project:  "testproject",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := edict.ID
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
		Username:    "testuser",
		Project:     "testproject",
		CurrentStep: 1,
		State:       RitualStateAborted,
		Data: storage.JSON{
			"inputs":       map[string]interface{}{"edict_id": edictID},
			"step1_result": "completed",
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
		repo.RepoInfo{},
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

	// Verify step1_result is preserved
	if exec.Data["step1_result"] != "completed" {
		t.Errorf("expected step1_result to be preserved, got %v", exec.Data["step1_result"])
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
		Intent:   "Test fresh start",
		Username: "testuser",
		Project:  "testproject",
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
		repo.RepoInfo{},
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
		Intent:   "Test all complete",
		Username: "testuser",
		Project:  "testproject",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := edict.ID
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
		Username:    "testuser",
		Project:     "testproject",
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
		repo.RepoInfo{},
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
		Intent:   "Test retry",
		Username: "testuser",
		Project:  "testproject",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := edict.ID
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
		Username:    "testuser",
		Project:     "testproject",
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
		repo.RepoInfo{},
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
		Intent:   "Test log",
		Username: "testuser",
		Project:  "testproject",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := edict.ID
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
		Username:    "testuser",
		Project:     "testproject",
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
		repo.RepoInfo{},
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

func TestFindFirstIncompleteStep(t *testing.T) {
	tests := []struct {
		name       string
		states     []RitualStepState
		totalSteps int
		want       int
	}{
		{
			name:       "all steps complete returns -1",
			states:     []RitualStepState{{Message: "ok"}, {Message: "done"}},
			totalSteps: 2,
			want:       -1,
		},
		{
			name:       "empty message on step 0",
			states:     []RitualStepState{{Message: ""}, {Message: "done"}},
			totalSteps: 2,
			want:       0,
		},
		{
			name:       "empty message on step 1",
			states:     []RitualStepState{{Message: "ok"}, {Message: ""}},
			totalSteps: 2,
			want:       1,
		},
		{
			name:       "retry count > 0 on step 0",
			states:     []RitualStepState{{Message: "failed", RetryCount: 1}, {Message: "ok"}},
			totalSteps: 2,
			want:       0,
		},
		{
			name:       "retry count > 0 on step 2",
			states:     []RitualStepState{{Message: "ok"}, {Message: "ok"}, {Message: "failed", RetryCount: 3}},
			totalSteps: 3,
			want:       2,
		},
		{
			name:       "context canceled message",
			states:     []RitualStepState{{Message: "ok"}, {Message: "context canceled"}},
			totalSteps: 2,
			want:       1,
		},
		{
			name:       "context canceled embedded in longer message",
			states:     []RitualStepState{{Message: "step failed: context canceled by upstream"}},
			totalSteps: 1,
			want:       0,
		},
		{
			name:       "timeout message",
			states:     []RitualStepState{{Message: "ok"}, {Message: "timeout waiting for response"}},
			totalSteps: 2,
			want:       1,
		},
		{
			name:       "timeout as standalone message",
			states:     []RitualStepState{{Message: "timeout"}},
			totalSteps: 2,
			want:       0,
		},
		{
			name:       "aborted message",
			states:     []RitualStepState{{Message: "ok"}, {Message: "step was aborted mid-execution"}},
			totalSteps: 3,
			want:       1,
		},
		{
			name:       "aborted as standalone message",
			states:     []RitualStepState{{Message: "aborted"}},
			totalSteps: 1,
			want:       0,
		},
		{
			name:       "step never reached when totalSteps exceeds len stepStates",
			states:     []RitualStepState{{Message: "ok"}},
			totalSteps: 4,
			want:       1,
		},
		{
			name:       "all steps never reached with empty states",
			states:     []RitualStepState{},
			totalSteps: 3,
			want:       0,
		},
		{
			name:       "zero total steps returns -1",
			states:     []RitualStepState{},
			totalSteps: 0,
			want:       -1,
		},
		{
			name:       "mixed: step0 ok, step1 retry, step2 empty",
			states:     []RitualStepState{{Message: "ok"}, {Message: "failed", RetryCount: 2}, {Message: ""}},
			totalSteps: 3,
			want:       1,
		},
		{
			name:       "mixed: step0 ok, step1 context canceled, step2 never reached",
			states:     []RitualStepState{{Message: "ok"}, {Message: "context canceled"}},
			totalSteps: 3,
			want:       1,
		},
		{
			name:       "retry count takes priority over non-empty message",
			states:     []RitualStepState{{Message: "completed", RetryCount: 1}},
			totalSteps: 1,
			want:       0,
		},
		{
			name:       "empty message takes priority over later error patterns",
			states:     []RitualStepState{{Message: ""}, {Message: "context canceled"}},
			totalSteps: 2,
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findFirstIncompleteStep(tt.states, tt.totalSteps)
			if got != tt.want {
				t.Errorf("findFirstIncompleteStep() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestRitualRecoveryRecoveringMarksCompleted verifies that when a previous execution
// has state "recovering", Start() marks it as completed in the database.
// This is the zombie "recovering" fix: the recovering execution should not linger.
func TestRitualRecoveryRecoveringMarksCompleted(t *testing.T) {
	db := setupRitualTestDB(t)

	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate edict table: %v", err)
	}

	edict := storage.Edict{
		Intent:   "Test recovering completion",
		Username: "testuser",
		Project:  "testproject",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictID := edict.ID
	edictKey := edict.Key()

	ritual := &RitualDef{
		Name:        "test-recovering-complete",
		Description: "Test recovering marks completed",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "Do step 1"},
			{Name: "step2", Minister: "judge", Act: "Do step 2"},
		},
	}

	// Create a previous execution in "recovering" state with step1 completed and step2 incomplete
	recoveringExecID := "ritual-recovering-xyz"
	err := db.Create(&RitualExecution{
		ID:          recoveringExecID,
		RitualName:  "test-recovering-complete",
		EdictID:     edictID,
		Username:    "testuser",
		Project:     "testproject",
		CurrentStep: 1,
		State:       RitualStateRecovering,
		Data:        storage.JSON{"inputs": map[string]interface{}{"edict_id": edictID}, "step1_result": "done"},
	}).Error
	if err != nil {
		t.Fatalf("failed to create recovering execution: %v", err)
	}

	db.Create(&RitualStepState{
		ExecutionID: recoveringExecID,
		StepIndex:   0,
		Name:        "step1",
		Message:     "Step 1 completed",
		RetryCount:  0,
	})
	db.Create(&RitualStepState{
		ExecutionID: recoveringExecID,
		StepIndex:   1,
		Name:        "step2",
		Message:     "",
		RetryCount:  0,
	})

	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("failed to register ritual: %v", err)
	}

	runner := NewRitualRunner(
		registry,
		nil, // getMinister - nil, no zhengming needed for recovering state
		nil, // publishEvent
		db,
		nil, // runner
		slog.Default(),
		repo.RepoInfo{},
	)

	ctx := context.Background()
	exec, err := runner.Start(ctx, "test-recovering-complete", edictKey, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("failed to start ritual: %v", err)
	}

	// Verify recovery mode is active
	if !exec.RecoveryMode {
		t.Error("expected RecoveryMode to be true")
	}

	// The key assertion: the previous "recovering" execution must now be "completed" in the database
	var previousExec RitualExecution
	if err := db.First(&previousExec, "id = ?", recoveringExecID).Error; err != nil {
		t.Fatalf("failed to query previous execution: %v", err)
	}
	if previousExec.State != RitualStateCompleted {
		t.Errorf("expected previous execution state to be %q, got %q", RitualStateCompleted, previousExec.State)
	}
}

// --- skipZhengmingPrompt / RitualStateRecovering tests ---

// TestSkipZhengmingPrompt_RecoveringState verifies that when a previous execution
// has state "recovering", the ritual runner skips the zhengming prompt and
// enters recovery mode directly.
func TestSkipZhengmingPrompt_RecoveringState(t *testing.T) {
	db := setupRitualTestDB(t)

	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate edict table: %v", err)
	}

	edict := storage.Edict{Intent: "Test skip zhengming", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictKey := edict.Key()

	ritual := &RitualDef{
		Name:        "test-skip-zhengming",
		Description: "Test skip zhengming ritual",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "Do step 1"},
			{Name: "step2", Minister: "judge", Act: "Do step 2"},
			{Name: "step3", Minister: "censor", Act: "Do step 3"},
		},
	}

	recoveringExecID := "ritual-recovering-123"
	err := db.Create(&RitualExecution{
		ID: recoveringExecID, RitualName: "test-skip-zhengming", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", CurrentStep: 1,
		State: RitualStateRecovering,
		Data:  storage.JSON{"inputs": map[string]interface{}{"edict_id": edict.ID}, "step1_result": "completed"},
	}).Error
	if err != nil {
		t.Fatalf("failed to create recovering execution: %v", err)
	}

	db.Create(&RitualStepState{ExecutionID: recoveringExecID, StepIndex: 0, Name: "step1", Message: "Step 1 completed successfully", RetryCount: 0})
	db.Create(&RitualStepState{ExecutionID: recoveringExecID, StepIndex: 1, Name: "step2", Message: "", RetryCount: 0})

	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("failed to register ritual: %v", err)
	}

	zhengmingCalled := false
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			zhengmingCalled = true
			return "req-should-not-be-called", nil
		},
		nil,
	)

	runner := NewRitualRunner(
		registry,
		func(id string) Minister {
			if id == "chancellor" {
				return chancellor
			}
			return nil
		},
		nil,
		db,
		nil,
		slog.Default(),
		repo.RepoInfo{},
	)

	ctx := context.Background()
	exec, err := runner.Start(ctx, "test-skip-zhengming", edictKey, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("failed to start ritual: %v", err)
	}

	if !exec.RecoveryMode {
		t.Error("expected RecoveryMode to be true for recovering state")
	}
	if exec.CurrentStep != 1 {
		t.Errorf("expected CurrentStep to be 1, got %d", exec.CurrentStep)
	}
	if zhengmingCalled {
		t.Error("expected RequestZhengming NOT to be called when previous state is 'recovering'")
	}
}

// TestSkipZhengmingPrompt_AbortedStateTriggersZhengming verifies that when
// a previous execution has state "aborted" (not "recovering"), the ritual
// runner DOES request zhengming confirmation.
func TestSkipZhengmingPrompt_AbortedStateTriggersZhengming(t *testing.T) {
	db := setupRitualTestDB(t)

	if err := db.AutoMigrate(&storage.Edict{}); err != nil {
		t.Fatalf("failed to migrate edict table: %v", err)
	}

	edict := storage.Edict{Intent: "Test aborted triggers zhengming", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}
	edictKey := edict.Key()

	ritual := &RitualDef{
		Name:        "test-aborted-zhengming",
		Description: "Test aborted zhengming ritual",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "Do step 1"},
			{Name: "step2", Minister: "judge", Act: "Do step 2"},
		},
	}

	abortedExecID := "ritual-aborted-zhengming-123"
	err := db.Create(&RitualExecution{
		ID: abortedExecID, RitualName: "test-aborted-zhengming", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", CurrentStep: 1,
		State: RitualStateAborted,
		Data:  storage.JSON{"inputs": map[string]interface{}{"edict_id": edict.ID}, "step1_result": "completed"},
	}).Error
	if err != nil {
		t.Fatalf("failed to create aborted execution: %v", err)
	}

	db.Create(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 0, Name: "step1", Message: "Step 1 completed", RetryCount: 0})
	db.Create(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 1, Name: "step2", Message: "", RetryCount: 0})

	registry := NewRitualRegistry()
	if err := registry.Register(ritual); err != nil {
		t.Fatalf("failed to register ritual: %v", err)
	}

	zhengmingCalled := false
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			zhengmingCalled = true
			return "req-aborted", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Recover from step 1", nil
		},
	)

	runner := NewRitualRunner(
		registry,
		func(id string) Minister {
			if id == "chancellor" {
				return chancellor
			}
			return nil
		},
		nil,
		db,
		nil,
		slog.Default(),
		repo.RepoInfo{},
	)

	ctx := context.Background()
	_, err = runner.Start(ctx, "test-aborted-zhengming", edictKey, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("failed to start ritual: %v", err)
	}

	if !zhengmingCalled {
		t.Error("expected RequestZhengming to be called when previous state is 'aborted'")
	}
}

// TestRitualStateRecovering_ConstantValue verifies that RitualStateRecovering
// has the expected string value.
func TestRitualStateRecovering_ConstantValue(t *testing.T) {
	if RitualStateRecovering != "recovering" {
		t.Errorf("expected RitualStateRecovering to be %q, got %q", "recovering", RitualStateRecovering)
	}
}

// --- promptForAbortedRituals tests ---

// TestPromptForAbortedRituals_NoAbortedRituals verifies that promptForAbortedRituals
// returns immediately when there are no aborted rituals.
func TestPromptForAbortedRituals_NoAbortedRituals(t *testing.T) {
	db := setupRecoveryTestDB(t)
	rg := newTestRitualGuard(t, db, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	rg.recoveryMu.RLock()
	if !rg.recoveryComplete {
		t.Error("expected recoveryComplete to be true after promptForAbortedRituals returns")
	}
	rg.recoveryMu.RUnlock()
}

// TestPromptForAbortedRituals_NilDB verifies that promptForAbortedRituals
// returns safely when db is nil.
func TestPromptForAbortedRituals_NilDB(t *testing.T) {
	base := NewMinisterBase(nil, nil, slog.Default(), "testuser", "testproject")
	rg := &RitualGuard{
		MinisterBase: base,
		getMinister:  nil,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	rg.recoveryMu.RLock()
	if !rg.recoveryComplete {
		t.Error("expected recoveryComplete to be true even with nil db")
	}
	rg.recoveryMu.RUnlock()
}

// TestPromptForAbortedRituals_NilGetMinister verifies that promptForAbortedRituals
// returns safely when getMinister is nil.
func TestPromptForAbortedRituals_NilGetMinister(t *testing.T) {
	db := setupRecoveryTestDB(t)
	rg := newTestRitualGuard(t, db, nil)

	abortedExec := &RitualExecution{
		ID:          "aborted-nil-minister",
		RitualName:  "test-ritual",
		EdictID:     1,
		Username:    "testuser",
		Project:     "testproject",
		State:       RitualStateAborted,
		CurrentStep: 0,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	rg.recoveryMu.RLock()
	if !rg.recoveryComplete {
		t.Error("expected recoveryComplete to be true even with nil getMinister")
	}
	rg.recoveryMu.RUnlock()
}

// TestPromptForAbortedRituals_AutoCompleteSealedEdict verifies that aborted
// rituals for sealed edicts are auto-completed without prompting.
func TestPromptForAbortedRituals_AutoCompleteSealedEdict(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test sealed edict", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	seal := storage.Seal{
		SealID: "seal-1", EdictID: edict.ID, Username: "testuser",
		Project: "testproject", MinisterID: "ruler", SealedAt: time.Now(),
	}
	if err := db.Create(&seal).Error; err != nil {
		t.Fatalf("failed to create seal: %v", err)
	}

	abortedExec := &RitualExecution{
		ID: "aborted-sealed-edict", RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	// Even though we don't need the chancellor for auto-completion,
	// getMinister must be non-nil for promptForAbortedRituals to proceed past the nil check.
	rg := newTestRitualGuard(t, db, func(id string) Minister { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", "aborted-sealed-edict").Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateCompleted {
		t.Errorf("expected state %s, got %s", RitualStateCompleted, exec.State)
	}
}

// TestPromptForAbortedRituals_AutoCompleteCancelledEdict verifies that aborted
// rituals for cancelled edicts are auto-completed without prompting.
func TestPromptForAbortedRituals_AutoCompleteCancelledEdict(t *testing.T) {
	db := setupRecoveryTestDB(t)

	now := time.Now()
	edict := storage.Edict{Intent: "Test cancelled edict", Username: "testuser", Project: "testproject", CancelledAt: &now}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExec := &RitualExecution{
		ID: "aborted-cancelled-edict", RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateFailed, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	rg := newTestRitualGuard(t, db, func(id string) Minister { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", "aborted-cancelled-edict").Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateCompleted {
		t.Errorf("expected state %s, got %s", RitualStateCompleted, exec.State)
	}
}

// TestPromptForAbortedRituals_RecoverAnswer verifies that answering "Recover from step N"
// sets the ritual state to "recovering" and updates CurrentStep.
func TestPromptForAbortedRituals_RecoverAnswer(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test recover answer", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-recover-answer"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 2,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 0, Name: "step0", Message: "completed"})
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 1, Name: "step1", Message: ""})

	var requestedQuestions storage.ZhengmingQuestions
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			requestedQuestions = questions
			return "req-recover", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Recover from step 1", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	if len(requestedQuestions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(requestedQuestions))
	}
	if !strings.Contains(requestedQuestions[0].Text, "aborted at step 1") {
		t.Errorf("expected question text to mention step 1, got %q", requestedQuestions[0].Text)
	}

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", abortedExecID).Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateRecovering {
		t.Errorf("expected state %s, got %s", RitualStateRecovering, exec.State)
	}
	if exec.CurrentStep != 1 {
		t.Errorf("expected CurrentStep to be 1, got %d", exec.CurrentStep)
	}
}

// TestPromptForAbortedRituals_MarkAsCompletedAnswer verifies that answering
// "Mark as completed" sets the ritual state to completed.
func TestPromptForAbortedRituals_MarkAsCompletedAnswer(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test mark completed", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-mark-completed"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 0, Name: "step0", Message: "ok"})
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 1, Name: "step1", Message: ""})

	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			return "req-mark", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Mark as completed", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", abortedExecID).Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateCompleted {
		t.Errorf("expected state %s, got %s", RitualStateCompleted, exec.State)
	}
}

// TestPromptForAbortedRituals_PassAnswer verifies that answering "Pass"
// leaves the ritual in its current aborted state.
func TestPromptForAbortedRituals_PassAnswer(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test pass answer", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-pass-answer"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 0, Name: "step0", Message: "ok"})
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 1, Name: "step1", Message: ""})

	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			return "req-pass", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Pass", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", abortedExecID).Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateAborted {
		t.Errorf("expected state %s, got %s", RitualStateAborted, exec.State)
	}
}

// TestPromptForAbortedRituals_ChancellorNil verifies that
// promptForAbortedRituals returns gracefully when the chancellor is nil.
func TestPromptForAbortedRituals_ChancellorNil(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test nil chancellor", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-nil-chancellor"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		return nil // chancellor not available
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", abortedExecID).Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateAborted {
		t.Errorf("expected state %s, got %s", RitualStateAborted, exec.State)
	}
}

// TestPromptForAbortedRituals_RequestZhengmingFails verifies that
// promptForAbortedRituals continues when RequestZhengming returns an error.
func TestPromptForAbortedRituals_RequestZhengmingFails(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test req fail", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-req-fail"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			return "", fmt.Errorf("zhengming unavailable")
		},
		nil,
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", abortedExecID).Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateAborted {
		t.Errorf("expected state %s, got %s", RitualStateAborted, exec.State)
	}
}

// TestPromptForAbortedRituals_ContextCancelled verifies that
// promptForAbortedRituals respects context cancellation.
func TestPromptForAbortedRituals_ContextCancelled(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test ctx cancel", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	aborted1 := &RitualExecution{
		ID: "aborted-ctx-1", RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	aborted2 := &RitualExecution{
		ID: "aborted-ctx-2", RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	db.Save(aborted1)
	db.Save(aborted2)

	var mu sync.Mutex
	chancellorCallCount := 0
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			mu.Lock()
			chancellorCallCount++
			mu.Unlock()
			return "req-1", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		rg.promptForAbortedRituals(ctx)
		close(done)
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("promptForAbortedRituals did not return after context cancellation")
	}

	mu.Lock()
	if chancellorCallCount > 1 {
		t.Errorf("expected at most 1 zhengming call before cancellation, got %d", chancellorCallCount)
	}
	mu.Unlock()
}

// TestPromptForAbortedRituals_IncompleteStepDetection verifies that
// promptForAbortedRituals correctly identifies the first incomplete step
// based on step states (empty message, retries, or error messages).
func TestPromptForAbortedRituals_IncompleteStepDetection(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test step detection", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-step-detect"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 3,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	// Step 0: completed. Step 1: has retry (incomplete). Step 2: context canceled (incomplete).
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 0, Name: "step0", Message: "ok"})
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 1, Name: "step1", Message: "failed", RetryCount: 2})
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 2, Name: "step2", Message: "context canceled"})

	var capturedStepIdx int
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			for _, opt := range questions[0].Options {
				if strings.HasPrefix(opt, "Recover from step ") {
					if _, err := fmt.Sscanf(opt, "Recover from step %d", &capturedStepIdx); err != nil {
						continue
					}
				}
			}
			return "req-step", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return fmt.Sprintf("Recover from step %d", capturedStepIdx), nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	if capturedStepIdx != 1 {
		t.Errorf("expected incomplete step at index 1 (retry count), got %d", capturedStepIdx)
	}
}

// TestPromptForAbortedRituals_EdictDescriptionTruncation verifies that
// long edict descriptions are truncated to 60 chars in the zhengming prompt.
func TestPromptForAbortedRituals_EdictDescriptionTruncation(t *testing.T) {
	db := setupRecoveryTestDB(t)

	longIntent := strings.Repeat("a", 100)
	edict := storage.Edict{Intent: longIntent, Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-desc-trunc"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 0, Name: "step0", Message: ""})

	var capturedText string
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			capturedText = questions[0].Text
			return "req-trunc", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Pass", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	if !strings.Contains(capturedText, "...") {
		t.Errorf("expected truncated description with '...' in question text, got %q", capturedText)
	}
}

// TestPromptForAbortedRituals_UsesSummaryOverIntent verifies that
// promptForAbortedRituals prefers edict.Summary over edict.Intent.
func TestPromptForAbortedRituals_UsesSummaryOverIntent(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{
		Intent: "This is the long intent text that should be ignored", Summary: "Short summary",
		Username: "testuser", Project: "testproject",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-summary-preferred"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 0, Name: "step0", Message: ""})

	var capturedText string
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			capturedText = questions[0].Text
			return "req-summary", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Pass", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	if !strings.Contains(capturedText, "Short summary") {
		t.Errorf("expected question to contain edict Summary 'Short summary', got %q", capturedText)
	}
}

// TestPromptForAbortedRituals_StoppedStateIncluded verifies that stopped rituals
// are also included in the aborted rituals query.
func TestPromptForAbortedRituals_StoppedStateIncluded(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test stopped state", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	stoppedExec := &RitualExecution{
		ID: "stopped-ritual-1", RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateStopped, CurrentStep: 1,
	}
	if err := db.Save(stoppedExec).Error; err != nil {
		t.Fatalf("failed to create stopped ritual: %v", err)
	}

	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			return "req-stopped", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Mark as completed", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", "stopped-ritual-1").Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateCompleted {
		t.Errorf("expected state %s, got %s", RitualStateCompleted, exec.State)
	}
}

// TestPromptForAbortedRituals_FailedStateIncluded verifies that failed rituals
// are also included in the aborted rituals query.
func TestPromptForAbortedRituals_FailedStateIncluded(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test failed state", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	failedExec := &RitualExecution{
		ID: "failed-ritual-1", RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateFailed, CurrentStep: 1,
	}
	if err := db.Save(failedExec).Error; err != nil {
		t.Fatalf("failed to create failed ritual: %v", err)
	}

	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			return "req-failed", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Mark as completed", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", "failed-ritual-1").Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateCompleted {
		t.Errorf("expected state %s, got %s", RitualStateCompleted, exec.State)
	}
}

// TestPromptForAbortedRituals_LimitFive verifies that at most 5 aborted
// rituals are prompted at a time.
func TestPromptForAbortedRituals_LimitFive(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test limit 5", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	for i := 0; i < 7; i++ {
		exec := &RitualExecution{
			ID: fmt.Sprintf("aborted-limit-%d", i), RitualName: fmt.Sprintf("test-ritual-%d", i),
			EdictID: edict.ID, Username: "testuser", Project: "testproject",
			State: RitualStateAborted, CurrentStep: 1,
		}
		if err := db.Save(exec).Error; err != nil {
			t.Fatalf("failed to create aborted ritual %d: %v", i, err)
		}
	}

	var mu sync.Mutex
	promptCount := 0
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			mu.Lock()
			promptCount++
			mu.Unlock()
			return fmt.Sprintf("req-%d", promptCount), nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Pass", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	mu.Lock()
	if promptCount > 5 {
		t.Errorf("expected at most 5 zhengming prompts, got %d", promptCount)
	}
	mu.Unlock()
}

// TestPromptForAbortedRituals_RecoveryCompleteOnReturn verifies that
// recoveryComplete is always set to true after promptForAbortedRituals returns.
func TestPromptForAbortedRituals_RecoveryCompleteOnReturn(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test recovery complete", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExec := &RitualExecution{
		ID: "aborted-recovery-complete", RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			return "req-1", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Pass", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	rg.recoveryMu.RLock()
	if !rg.recoveryComplete {
		t.Error("expected recoveryComplete to be true after promptForAbortedRituals returns")
	}
	rg.recoveryMu.RUnlock()
}

// TestPromptForAbortedRituals_ZeroEdictIDExcluded verifies that rituals
// with edict_id = 0 are excluded from the recovery prompt.
func TestPromptForAbortedRituals_ZeroEdictIDExcluded(t *testing.T) {
	db := setupRecoveryTestDB(t)

	abortedExec := &RitualExecution{
		ID: "aborted-zero-edict", RitualName: "test-ritual", EdictID: 0,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	zhengmingCalled := false
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			zhengmingCalled = true
			return "req-zero", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Pass", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	if zhengmingCalled {
		t.Error("expected RequestZhengming NOT to be called for ritual with edict_id=0")
	}
}

// TestPromptForAbortedRituals_WaitForZhengmingFails verifies that
// promptForAbortedRituals continues when WaitForZhengming returns an error.
func TestPromptForAbortedRituals_WaitForZhengmingFails(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test wait fail", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-wait-fail"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			return "req-wait-fail", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "", fmt.Errorf("zhengming wait failed")
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	var exec RitualExecution
	if err := db.First(&exec, "id = ?", abortedExecID).Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateAborted {
		t.Errorf("expected state %s, got %s", RitualStateAborted, exec.State)
	}
}

// TestPromptForAbortedRituals_EmptyMessageStepIncomplete verifies that
// steps with empty messages are detected as incomplete.
func TestPromptForAbortedRituals_EmptyMessageStepIncomplete(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test empty msg", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-empty-msg"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 3,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	// Step 0 and 1 completed, step 2 has empty message (never executed)
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 0, Name: "step0", Message: "ok"})
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 1, Name: "step1", Message: "ok"})
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 2, Name: "step2", Message: ""})

	var capturedStepIdx int
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			for _, opt := range questions[0].Options {
				if strings.HasPrefix(opt, "Recover from step ") {
					if _, err := fmt.Sscanf(opt, "Recover from step %d", &capturedStepIdx); err != nil {
						continue
					}
				}
			}
			return "req-empty", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Pass", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	if capturedStepIdx != 2 {
		t.Errorf("expected incomplete step at index 2 (empty message), got %d", capturedStepIdx)
	}
}

// TestPromptForAbortedRituals_AbortedMessageStepIncomplete verifies that
// steps with "aborted" in their message are detected as incomplete.
func TestPromptForAbortedRituals_AbortedMessageStepIncomplete(t *testing.T) {
	db := setupRecoveryTestDB(t)

	edict := storage.Edict{Intent: "Test aborted msg", Username: "testuser", Project: "testproject"}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	abortedExecID := "aborted-aborted-msg"
	abortedExec := &RitualExecution{
		ID: abortedExecID, RitualName: "test-ritual", EdictID: edict.ID,
		Username: "testuser", Project: "testproject", State: RitualStateAborted, CurrentStep: 1,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	// Step 0 completed, step 1 has "aborted" message
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 0, Name: "step0", Message: "ok"})
	db.Save(&RitualStepState{ExecutionID: abortedExecID, StepIndex: 1, Name: "step1", Message: "step was aborted mid-execution"})

	var capturedStepIdx int
	chancellor := newMockChancellor(
		func(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
			for _, opt := range questions[0].Options {
				if strings.HasPrefix(opt, "Recover from step ") {
					if _, err := fmt.Sscanf(opt, "Recover from step %d", &capturedStepIdx); err != nil {
						continue
					}
				}
			}
			return "req-abort-msg", nil
		},
		func(ctx context.Context, requestID string) (string, error) {
			return "Pass", nil
		},
	)

	rg := newTestRitualGuard(t, db, func(id string) Minister {
		if id == "chancellor" {
			return chancellor
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rg.promptForAbortedRituals(ctx)

	if capturedStepIdx != 1 {
		t.Errorf("expected incomplete step at index 1 (aborted message), got %d", capturedStepIdx)
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
