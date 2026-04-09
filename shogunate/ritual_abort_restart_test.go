package shogunate

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "modernc.org/sqlite"
)

// blockingMockLLM blocks GenerateContent until the context is cancelled or a signal is sent.
type blockingMockLLM struct {
	llms.Model
	response  string
	unblockCh chan struct{} // close to unblock
	callCount int
}

func (m *blockingMockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return m.response, nil
}

func (m *blockingMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	m.callCount++

	// Block until unblocked or context cancelled
	select {
	case <-m.unblockCh:
		// Proceed normally
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	callOpts := &llms.CallOptions{}
	for _, opt := range options {
		opt(callOpts)
	}
	if callOpts.StreamingFunc != nil {
		callOpts.StreamingFunc(ctx, []byte(m.response))
	}
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: m.response}},
	}, nil
}

// blockingTestMinister wraps a blockingMockLLM for use in abort/restart tests.
type blockingTestMinister struct {
	MinisterBase
	id      string
	tasksCh chan *Task
	mockLLM *blockingMockLLM
}

func (m *blockingTestMinister) ID() string                        { return m.id }
func (m *blockingTestMinister) SystemPrompt() string              { return "" }
func (m *blockingTestMinister) Title() string                     { return m.id }
func (m *blockingTestMinister) Tools() []Tool                     { return nil }
func (m *blockingTestMinister) Tasks() chan<- *Task                { return m.tasksCh }
func (m *blockingTestMinister) Model() llms.Model                 { return m.mockLLM }
func (m *blockingTestMinister) Run(ctx context.Context)           { <-ctx.Done() }

// TestRitualAbortAndRestart_Integration tests the full recovery flow:
// 1. Start ritual and let first step complete
// 2. Abort mid-execution (during step2)
// 3. Restart ritual - should recover from incomplete step
// 4. Verify completed steps are SKIPPED
// 5. Verify remaining steps complete successfully
func TestRitualAbortAndRestart_Integration(t *testing.T) {
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

	// forge: completes immediately
	forgeUnblock := make(chan struct{})
	close(forgeUnblock) // never blocks
	forgeLLM := &blockingMockLLM{response: "forge done", unblockCh: forgeUnblock}

	// judge: blocks until we unblock it
	judgeUnblock := make(chan struct{})
	judgeLLM := &blockingMockLLM{response: "judge done", unblockCh: judgeUnblock}

	// sage: completes immediately
	sageUnblock := make(chan struct{})
	close(sageUnblock)
	sageLLM := &blockingMockLLM{response: "sage done", unblockCh: sageUnblock}

	shog := &Shogunate{
		ministers: map[string]Minister{
			"forge": &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "forge", tasksCh: make(chan *Task, 1), mockLLM: forgeLLM},
			"judge": &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "judge", tasksCh: make(chan *Task, 1), mockLLM: judgeLLM},
			"sage":  &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "sage", tasksCh: make(chan *Task, 1), mockLLM: sageLLM},
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

	// Verify forge was called
	if forgeLLM.callCount < 1 {
		t.Fatalf("Expected forge to be called at least once, got %d", forgeLLM.callCount)
	}

	// Abort mid-execution (judge is blocking)
	cancel1()
	<-runErrCh
	time.Sleep(50 * time.Millisecond)

	// === PHASE 2: RESTART AND RECOVER ===
	// Create fresh judge unblock channel for recovery
	judgeUnblock2 := make(chan struct{})
	close(judgeUnblock2) // Complete immediately on recovery
	judgeLLM2 := &blockingMockLLM{response: "judge done", unblockCh: judgeUnblock2}

	// Replace judge minister with one that completes immediately
	shog.ministers["judge"] = &blockingTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "judge",
		tasksCh:      make(chan *Task, 1),
		mockLLM:      judgeLLM2,
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

	// Forge was called once in phase 1, NOT called again in recovery (skipped)
	if forgeLLM.callCount != 1 {
		t.Errorf("Expected forge=1 (skipped on recovery), got %d", forgeLLM.callCount)
	}

	// Judge was called in recovery
	if judgeLLM2.callCount != 1 {
		t.Errorf("Expected judge recovery calls=1, got %d", judgeLLM2.callCount)
	}

	// Sage was called in recovery
	if sageLLM.callCount != 1 {
		t.Errorf("Expected sage=1, got %d", sageLLM.callCount)
	}

	t.Log("Recovery flow verified: completed steps were skipped")
}

// TestRitualAbortMidStep_VerifySkipExplicit tests that when a ritual is aborted
// mid-step, the recovery correctly identifies the incomplete step and skips completed ones.
func TestRitualAbortMidStep_VerifySkipExplicit(t *testing.T) {
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

	// forge: completes immediately
	forgeUnblock := make(chan struct{})
	close(forgeUnblock)
	forgeLLM := &blockingMockLLM{response: "setup complete", unblockCh: forgeUnblock}

	// judge: blocks on first call, completes on second
	judgeUnblock := make(chan struct{})
	judgeLLM := &blockingMockLLM{response: "process complete", unblockCh: judgeUnblock}

	// sage: completes immediately
	sageUnblock := make(chan struct{})
	close(sageUnblock)
	sageLLM := &blockingMockLLM{response: "finalize complete", unblockCh: sageUnblock}

	shog := &Shogunate{
		ministers: map[string]Minister{
			"forge": &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "forge", tasksCh: make(chan *Task, 1), mockLLM: forgeLLM},
			"judge": &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "judge", tasksCh: make(chan *Task, 1), mockLLM: judgeLLM},
			"sage":  &blockingTestMinister{MinisterBase: MinisterBase{logger: slog.Default()}, id: "sage", tasksCh: make(chan *Task, 1), mockLLM: sageLLM},
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

	// Verify judge was called (blocking)
	if judgeLLM.callCount < 1 {
		t.Fatalf("Expected judge to be invoked, got %d calls", judgeLLM.callCount)
	}

	// Abort during judge step
	cancel1()
	<-runErrCh
	time.Sleep(50 * time.Millisecond)

	// === PHASE 2: Restart and verify skip ===
	// Replace judge with one that completes immediately
	judgeUnblock2 := make(chan struct{})
	close(judgeUnblock2)
	judgeLLM2 := &blockingMockLLM{response: "process complete", unblockCh: judgeUnblock2}
	shog.ministers["judge"] = &blockingTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "judge",
		tasksCh:      make(chan *Task, 1),
		mockLLM:      judgeLLM2,
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

	// === PHASE 3: Verify skip behavior ===
	// Forge should be invoked only once (skipped on recovery)
	if forgeLLM.callCount != 1 {
		t.Errorf("Expected forge=1 (skipped on recovery), got %d", forgeLLM.callCount)
	}

	// Judge was invoked once in recovery (fresh LLM)
	if judgeLLM2.callCount != 1 {
		t.Errorf("Expected judge recovery=1, got %d", judgeLLM2.callCount)
	}

	// Sage should be invoked once (only in recovery)
	if sageLLM.callCount != 1 {
		t.Errorf("Expected sage=1, got %d", sageLLM.callCount)
	}

	t.Log("Skip behavior verified: completed steps were skipped on recovery")
}
