package shogunate

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "modernc.org/sqlite"
)

// TestRitualAbortAndRestart_Integration tests the full recovery flow:
// 1. Start ritual and let first step complete
// 2. Abort mid-execution (cancel context)
// 3. Restart ritual - should recover from incomplete step
// 4. Verify completed steps are SKIPPED (forge not invoked again)
// 5. Verify remaining steps complete successfully
func TestRitualAbortAndRestart_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(tmpDir+"/test.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	db.AutoMigrate(&RitualExecution{}, &RitualStepState{}, &storage.Edict{}, &storage.TianEvent{})

	edictVar := storage.Edict{Intent: "Test", Status: storage.EdictActive}
	db.Create(&edictVar)
	edictID := edictVar.EdictID

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

	// Track minister invocations to verify skip behavior
	var mu sync.Mutex
	forgeCount, judgeCount, sageCount := 0, 0, 0

	forgeCh := make(chan *Task, 1)
	judgeCh := make(chan *Task, 1)
	sageCh := make(chan *Task, 1)

	judgeDoneCh := make(chan struct{})
	sageDoneCh := make(chan struct{})

	// Use a background context for ministers that persists across recovery
	ministerCtx, ministerCancel := context.WithCancel(context.Background())
	defer ministerCancel()

	go func() {
		for {
			select {
			case <-ministerCtx.Done():
				return
			case task := <-forgeCh:
				mu.Lock()
				forgeCount++
				mu.Unlock()
				task.Done <- Result{Output: "forge done"}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ministerCtx.Done():
				return
			case task := <-judgeCh:
				mu.Lock()
				judgeCount++
				mu.Unlock()
				<-judgeDoneCh
				task.Done <- Result{Output: "judge done"}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ministerCtx.Done():
				return
			case task := <-sageCh:
				mu.Lock()
				sageCount++
				mu.Unlock()
				task.Done <- Result{Output: "sage done"}
				close(sageDoneCh)
			}
		}
	}()

	shog := &Shogunate{
		ministers: map[string]Minister{
			"forge": &ritualTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "forge", tasksCh: forgeCh},
			"judge": &ritualTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "judge", tasksCh: judgeCh},
			"sage":  &ritualTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "sage", tasksCh: sageCh},
		},
		logger: slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	// === PHASE 1: START AND ABORT ===
	// Start ritual, let forge complete, then abort
	ctx1, cancel1 := context.WithCancel(context.Background())
	exec1, _ := runner.Start(ctx1, "test", edictID, nil, nil)
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- runner.Run(ctx1, exec1) }()

	time.Sleep(100 * time.Millisecond) // Let forge complete step1
	cancel1()                          // Abort mid-execution
	<-runErrCh
	time.Sleep(50 * time.Millisecond)

	// Verify forge was invoked once
	mu.Lock()
	if forgeCount != 1 {
		mu.Unlock()
		t.Fatalf("Expected forge=1 after phase 1, got %d", forgeCount)
	}
	mu.Unlock()

	// === PHASE 2: RESTART AND RECOVER ===
	// Restart ritual - should detect aborted execution and recover
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	exec2, _ := runner.Start(ctx2, "test", edictID, nil, nil)

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

	// Wait for judge to be invoked (step2)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	if judgeCount == 0 {
		mu.Unlock()
		t.Fatal("Judge not invoked in recovery")
	}
	mu.Unlock()

	// Complete judge
	close(judgeDoneCh)

	// Wait for sage to complete (step3) with timeout
	select {
	case <-sageDoneCh:
		// Sage completed successfully
	case <-time.After(5 * time.Second):
		mu.Lock()
		t.Logf("Counts: forge=%d, judge=%d, sage=%d", forgeCount, judgeCount, sageCount)
		mu.Unlock()
		var exec RitualExecution
		db.First(&exec, "id = ?", exec2.ID)
		t.Logf("Exec: CurrentStep=%d, State=%s", exec.CurrentStep, exec.State)
		var steps []RitualStepState
		db.Where("execution_id = ?", exec2.ID).Order("step_index").Find(&steps)
		for i, s := range steps {
			t.Logf("Step %d: %s - %q", i, s.Name, s.Message)
		}
		t.Fatal("Sage timeout")
	}

	// Wait for run to complete
	select {
	case err := <-runErrCh2:
		if err != nil {
			t.Errorf("Run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not finish")
	}

	// === PHASE 3: VERIFY SKIP BEHAVIOR ===
	// Verify final state
	var finalExec RitualExecution
	db.First(&finalExec, "id = ?", exec2.ID)
	if finalExec.State != RitualStateCompleted {
		t.Errorf("Expected completed, got %s", finalExec.State)
	}

	mu.Lock()
	fi, ji, si := forgeCount, judgeCount, sageCount
	mu.Unlock()

	// CRITICAL: Verify forge was NOT invoked again (step was skipped)
	if fi != 1 {
		t.Errorf("Expected forge=1 (skipped on recovery), got %d", fi)
	}

	// Judge was invoked twice: once in initial run, once in recovery
	if ji != 2 {
		t.Errorf("Expected judge=2 (initial + recovery), got %d", ji)
	}

	// Sage was invoked once (only in recovery)
	if si != 1 {
		t.Errorf("Expected sage=1, got %d", si)
	}

	t.Log("Recovery flow verified: completed steps were skipped")
}

// TestRitualAbortMidStep_VerifySkipExplicit tests that when a ritual is aborted
// mid-step (step started but not completed), the recovery correctly identifies
// the incomplete step and skips only the completed ones.
func TestRitualAbortMidStep_VerifySkipExplicit(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(tmpDir+"/test.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	db.AutoMigrate(&RitualExecution{}, &RitualStepState{}, &storage.Edict{}, &storage.TianEvent{})

	edictVar := storage.Edict{Intent: "Test mid-step abort", Status: storage.EdictActive}
	db.Create(&edictVar)
	edictID := edictVar.EdictID

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

	// Track invocations with detailed logging
	var mu sync.Mutex
	invocations := []string{}

	forgeCh := make(chan *Task, 1)
	judgeCh := make(chan *Task, 1)
	sageCh := make(chan *Task, 1)

	judgeBlockCh := make(chan struct{})

	ministerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ministerCtx.Done():
				return
			case task := <-forgeCh:
				mu.Lock()
				invocations = append(invocations, "forge:setup")
				mu.Unlock()
				task.Done <- Result{Output: "setup complete"}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ministerCtx.Done():
				return
			case task := <-judgeCh:
				mu.Lock()
				invocations = append(invocations, "judge:process")
				mu.Unlock()
				<-judgeBlockCh // Block until we're ready
				task.Done <- Result{Output: "process complete"}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ministerCtx.Done():
				return
			case task := <-sageCh:
				mu.Lock()
				invocations = append(invocations, "sage:finalize")
				mu.Unlock()
				task.Done <- Result{Output: "finalize complete"}
			}
		}
	}()

	shog := &Shogunate{
		ministers: map[string]Minister{
			"forge": &ritualTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "forge", tasksCh: forgeCh},
			"judge": &ritualTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "judge", tasksCh: judgeCh},
			"sage":  &ritualTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "sage", tasksCh: sageCh},
		},
		logger: slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	// === PHASE 1: Start and abort during judge step ===
	ctx1, cancel1 := context.WithCancel(context.Background())
	exec1, _ := runner.Start(ctx1, "test-midstep", edictID, nil, nil)
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- runner.Run(ctx1, exec1) }()

	// Wait for forge to complete and judge to start
	time.Sleep(150 * time.Millisecond)

	// Verify we're in judge step
	mu.Lock()
	if len(invocations) < 2 || invocations[1] != "judge:process" {
		mu.Unlock()
		t.Fatalf("Expected judge to be invoked, got invocations: %v", invocations)
	}
	mu.Unlock()

	// Abort during judge step
	cancel1()
	<-runErrCh
	time.Sleep(50 * time.Millisecond)

	// === PHASE 2: Restart and verify skip ===
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	exec2, _ := runner.Start(ctx2, "test-midstep", edictID, nil, nil)

	// Verify recovery mode
	if !exec2.RecoveryMode {
		t.Fatal("Expected RecoveryMode=true")
	}

	// Should recover from judge step (step 1) since it was incomplete
	if exec2.CurrentStep != 1 {
		t.Fatalf("Expected CurrentStep=1 (recover from judge), got %d", exec2.CurrentStep)
	}

	// Release judge and let it complete
	close(judgeBlockCh)

	// Run recovery
	runErrCh2 := make(chan error, 1)
	go func() { runErrCh2 <- runner.Run(ctx2, exec2) }()

	// Wait for completion
	select {
	case err := <-runErrCh2:
		if err != nil {
			t.Errorf("Recovery run failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Recovery run did not finish")
	}

	// === PHASE 3: Verify skip behavior ===
	mu.Lock()
	defer mu.Unlock()

	// Count invocations per minister
	forgeCount := 0
	judgeCount := 0
	sageCount := 0
	for _, inv := range invocations {
		switch {
		case inv == "forge:setup":
			forgeCount++
		case inv == "judge:process":
			judgeCount++
		case inv == "sage:finalize":
			sageCount++
		}
	}

	// Forge should be invoked only once (skipped on recovery)
	if forgeCount != 1 {
		t.Errorf("Expected forge=1 (skipped on recovery), got %d. Invocations: %v", forgeCount, invocations)
	}

	// Judge should be invoked twice (initial + recovery)
	if judgeCount != 2 {
		t.Errorf("Expected judge=2 (initial + recovery), got %d. Invocations: %v", judgeCount, invocations)
	}

	// Sage should be invoked once (only in recovery)
	if sageCount != 1 {
		t.Errorf("Expected sage=1, got %d. Invocations: %v", sageCount, invocations)
	}

	t.Logf("Skip behavior verified: invocations=%v", invocations)
}
