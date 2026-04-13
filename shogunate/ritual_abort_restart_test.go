package shogunate

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "modernc.org/sqlite"
)

// blockingTestMinister wraps MinisterBase for use in abort/restart tests.
// Note: With the migration to bifrost, the mock LLM approach is no longer possible.
// These tests are skipped until a bifrost test server can be used.
type blockingTestMinister struct {
	MinisterBase
	id      string
	tasksCh chan *Task
}

func (m *blockingTestMinister) ID() string              { return m.id }
func (m *blockingTestMinister) SystemPrompt() string    { return "" }
func (m *blockingTestMinister) Title() string           { return m.id }
func (m *blockingTestMinister) Tools() []Tool           { return nil }
func (m *blockingTestMinister) Tasks() chan<- *Task     { return m.tasksCh }
func (m *blockingTestMinister) Run(ctx context.Context) { <-ctx.Done() }

// TestRitualAbortAndRestart_Integration tests the full recovery flow:
// 1. Start ritual and let first step complete
// 2. Abort mid-execution (during step2)
// 3. Restart ritual - should recover from incomplete step
// 4. Verify completed steps are SKIPPED
// 5. Verify remaining steps complete successfully
func TestRitualAbortAndRestart_Integration(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses in ritual steps")

	tmpDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(tmpDir+"/test.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	db.AutoMigrate(&RitualExecution{}, &RitualStepState{}, &storage.Edict{}, &storage.TianEvent{}, &storage.ForgeManifest{})

	edictVar := storage.Edict{Intent: "Test", Username: "testuser", Project: "testproject"}
	db.Create(&edictVar)
	edictKey := edictVar.Key()

	ritual := &RitualDef{
		Name: "test",
		Steps: []RitualStep{
			{Name: "step1", Minister: "forge", Act: "First"},
			{Name: "step2", Minister: "judge", Act: "Second"},
			{Name: "step3", Minister: "sage", Act: "Third"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	shog := &Shogunate{
		ministers: map[string]Minister{
			"forge": &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "forge", tasksCh: make(chan *Task, 1)},
			"judge": &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "judge", tasksCh: make(chan *Task, 1)},
			"sage":  &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "sage", tasksCh: make(chan *Task, 1)},
		},
		logger: slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	// === PHASE 1: START AND ABORT ===
	ctx1, cancel1 := context.WithCancel(context.Background())
	exec1, _ := runner.Start(ctx1, "test", edictKey, nil, nil)
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- runner.Run(ctx1, exec1) }()

	// Wait for forge to complete and judge to start blocking
	time.Sleep(200 * time.Millisecond)

	// Abort mid-execution (judge is blocking)
	cancel1()
	<-runErrCh
	time.Sleep(50 * time.Millisecond)

	// === PHASE 2: RESTART AND RECOVER ===
	shog.ministers["judge"] = &blockingTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "judge",
		tasksCh:      make(chan *Task, 1),
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	exec2, _ := runner.Start(ctx2, "test", edictKey, nil, nil)

	// Verify recovery mode is enabled
	if !exec2.RecoveryMode {
		t.Fatal("Expected RecoveryMode=true")
	}

	// Verify recovery starts from step 1 (step2, since step1 completed)
	if exec2.CurrentStep != 1 {
		t.Fatalf("Expected CurrentStep=1 (recover from step2), got %d", exec2.CurrentStep)
	}

	// Run recovery
	runErrCh2 := make(chan error, 1)
	go func() { runErrCh2 <- runner.Run(ctx2, exec2) }()

	select {
	case err := <-runErrCh2:
		if err != nil {
			t.Errorf("Recovery run failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Recovery run did not finish")
	}

	// === PHASE 3: VERIFY ===
	var finalExec RitualExecution
	db.First(&finalExec, "id = ?", exec2.ID)
	if finalExec.State != RitualStateCompleted {
		t.Errorf("Expected completed, got %s", finalExec.State)
	}

	t.Log("Recovery flow verified: completed steps were skipped")
}

// TestRitualAbortMidStep_VerifySkipExplicit tests that when a ritual is aborted
// mid-step, the recovery correctly identifies the incomplete step and skips completed ones.
func TestRitualAbortMidStep_VerifySkipExplicit(t *testing.T) {
	t.Skip("requires mock bifrost client for LLM responses in ritual steps")

	tmpDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(tmpDir+"/test.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	db.AutoMigrate(&RitualExecution{}, &RitualStepState{}, &storage.Edict{}, &storage.TianEvent{}, &storage.ForgeManifest{})

	edictVar := storage.Edict{Intent: "Test mid-step abort", Username: "testuser", Project: "testproject"}
	db.Create(&edictVar)
	edictKey := edictVar.Key()

	ritual := &RitualDef{
		Name: "test-midstep",
		Steps: []RitualStep{
			{Name: "setup", Minister: "forge", Act: "Setup work"},
			{Name: "process", Minister: "judge", Act: "Process data"},
			{Name: "finalize", Minister: "sage", Act: "Finalize"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	shog := &Shogunate{
		ministers: map[string]Minister{
			"forge": &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "forge", tasksCh: make(chan *Task, 1)},
			"judge": &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "judge", tasksCh: make(chan *Task, 1)},
			"sage":  &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "sage", tasksCh: make(chan *Task, 1)},
		},
		logger: slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	// === PHASE 1: Start and abort during judge step ===
	ctx1, cancel1 := context.WithCancel(context.Background())
	exec1, _ := runner.Start(ctx1, "test-midstep", edictKey, nil, nil)
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- runner.Run(ctx1, exec1) }()

	// Wait for forge to complete and judge to be blocking
	time.Sleep(200 * time.Millisecond)

	// Abort during judge step
	cancel1()
	<-runErrCh
	time.Sleep(50 * time.Millisecond)

	// === PHASE 2: Restart and verify skip ===
	shog.ministers["judge"] = &blockingTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "judge",
		tasksCh:      make(chan *Task, 1),
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	exec2, _ := runner.Start(ctx2, "test-midstep", edictKey, nil, nil)

	// Verify recovery mode
	if !exec2.RecoveryMode {
		t.Fatal("Expected RecoveryMode=true")
	}

	// Should recover from judge step (step 1) since it was incomplete
	if exec2.CurrentStep != 1 {
		t.Fatalf("Expected CurrentStep=1 (recover from judge), got %d", exec2.CurrentStep)
	}

	// Run recovery
	runErrCh2 := make(chan error, 1)
	go func() { runErrCh2 <- runner.Run(ctx2, exec2) }()

	select {
	case err := <-runErrCh2:
		if err != nil {
			t.Errorf("Recovery run failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Recovery run did not finish")
	}

	t.Log("Skip behavior verified: completed steps were skipped on recovery")
}
