package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
)

// TestExecuteForkStep_Parallel tests parallel fork execution
func TestExecuteForkStep_Parallel(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fork-parallel-test",
		Steps: []RitualStep{
			{
				Name: "prepare",
				Minister: "strategist",
				Task: "prepare work units",
			},
			{
				Name: "process-all",
				Fork: &ForkDef{
					Over:      "work_units",
					BatchSize: 2,
				},
				Work: []RitualStep{
					{Name: "process", Minister: "forge", Act: "process {{ .item }}"},
				},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	// Track forge invocations
	var mu sync.Mutex
	forgeInvocations := []interface{}{}

	forgeCh := make(chan *Task, 10)
	strategistCh := make(chan *Task, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Strategist returns work units
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-strategistCh:
				task.Done <- Result{Output: "work units prepared"}
			}
		}
	}()

	// Forge processes each item
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				mu.Lock()
				forgeInvocations = append(forgeInvocations, task.Work)
				mu.Unlock()
				task.Done <- Result{Output: fmt.Sprintf("processed %v", task.Work)}
			}
		}
	}()

	strategistM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "strategist",
		tasksCh:      strategistCh,
		result:       "work units prepared",
	}
	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}

	shog := &Shogunate{
		ministers: map[string]Minister{"strategist": strategistM, "forge": forgeM},
		logger:   slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "fork-parallel-test", "edict-fork-1", nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	// Inject work units into context
	exec.Data = storage.JSON{
		"given_context": map[string]interface{}{
			"work_units": []interface{}{"file1.go", "file2.go", "file3.go"},
		},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	if exec.State != RitualStateCompleted {
		t.Errorf("Expected state 'completed', got %s", exec.State)
	}

	mu.Lock()
	defer mu.Unlock()

	// Verify all items were processed
	if len(forgeInvocations) != 3 {
		t.Errorf("Expected 3 forge invocations, got %d", len(forgeInvocations))
	}
}

// TestExecuteForkStep_Sequential tests sequential fork execution
func TestExecuteForkStep_Sequential(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fork-sequential-test",
		Steps: []RitualStep{
			{
				Name: "process-all",
				Fork: &ForkDef{
					Over:      "items",
					BatchSize: 1,
				},
				Work: []RitualStep{
					{Name: "process", Minister: "forge", Act: "process {{ .item }}"},
				},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	var mu sync.Mutex
	processOrder := []string{}

	forgeCh := make(chan *Task, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				mu.Lock()
				processOrder = append(processOrder, fmt.Sprintf("%v", task.Work))
				mu.Unlock()
				task.Done <- Result{Output: "done"}
			}
		}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}

	shog := &Shogunate{
		ministers: map[string]Minister{"forge": forgeM},
		logger:   slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "fork-sequential-test", "edict-fork-2", nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"given_context": map[string]interface{}{
			"items": []interface{}{"first", "second", "third"},
		},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Verify sequential order - Work contains full prompt with item
	expected := []string{"first", "second", "third"}
	if len(processOrder) != len(expected) {
		t.Errorf("Expected %d items processed, got %d", len(expected), len(processOrder))
	}
	for i, exp := range expected {
		if i < len(processOrder) && !strings.Contains(processOrder[i], exp) {
			t.Errorf("Expected item %d to contain %q, got %q", i, exp, processOrder[i])
		}
	}
}

// TestExecuteForkStep_WithLimit tests fork execution with limit
func TestExecuteForkStep_WithLimit(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fork-limit-test",
		Steps: []RitualStep{
			{
				Name: "process-all",
				Fork: &ForkDef{
					Over:      "items",
					BatchSize: 1,
					Limit:     "2",
				},
				Work: []RitualStep{
					{Name: "process", Minister: "forge", Act: "process {{ .item }}"},
				},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	var mu sync.Mutex
	processedCount := 0

	forgeCh := make(chan *Task, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				mu.Lock()
				processedCount++
				mu.Unlock()
				task.Done <- Result{Output: "done"}
			}
		}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}

	shog := &Shogunate{
		ministers: map[string]Minister{"forge": forgeM},
		logger:   slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "fork-limit-test", "edict-fork-3", nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"given_context": map[string]interface{}{
			"items": []interface{}{"one", "two", "three", "four", "five"},
		},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Verify only 2 items were processed (limit)
	if processedCount != 2 {
		t.Errorf("Expected 2 items processed (limit), got %d", processedCount)
	}
}

// TestGetForkWorkUnits tests retrieving work units from context
func TestGetForkWorkUnits(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, nil)

	tests := []struct {
		name      string
		execData  storage.JSON
		over      string
		wantCount int
		wantErr   bool
	}{
		{
			name: "from given_context",
			execData: storage.JSON{
				"given_context": map[string]interface{}{
					"files": []interface{}{"a.go", "b.go"},
				},
			},
			over:      "files",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "from step_results",
			execData: storage.JSON{
				"step_results": map[string]interface{}{
					"output": []interface{}{"x", "y", "z"},
				},
			},
			over:      "output",
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "key not found",
			execData:  storage.JSON{},
			over:      "missing",
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &RitualExecution{
				ID:         "test-exec",
				RitualName: "test",
				Data:       tt.execData,
			}

			units, err := runner.getForkWorkUnits(exec, tt.over)
			if (err != nil) != tt.wantErr {
				t.Errorf("getForkWorkUnits() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(units) != tt.wantCount {
				t.Errorf("expected %d work units, got %d", tt.wantCount, len(units))
			}
		})
	}
}

// TestToInterfaceSlice tests conversion of various slice types
func TestToInterfaceSlice(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, nil)

	tests := []struct {
		name    string
		input   interface{}
		wantLen int
		wantErr bool
	}{
		{
			name:    "interface slice",
			input:   []interface{}{"a", "b", "c"},
			wantLen: 3,
			wantErr: false,
		},
		{
			name:    "map slice",
			input:   []map[string]interface{}{{"key": "val1"}, {"key": "val2"}},
			wantLen: 2,
			wantErr: false,
		},
		{
			name:    "string slice via json",
			input:   []string{"x", "y"},
			wantLen: 2,
			wantErr: false,
		},
		{
			name:    "int slice via json",
			input:   []int{1, 2, 3, 4},
			wantLen: 4,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := runner.toInterfaceSlice(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("toInterfaceSlice() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(result) != tt.wantLen {
				t.Errorf("expected %d items, got %d", tt.wantLen, len(result))
			}
		})
	}
}

// TestExecuteForkItem tests single fork item execution
func TestExecuteForkItem(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fork-item-test",
		Steps: []RitualStep{
			{
				Name: "process",
				Fork: &ForkDef{
					Over:      "items",
					BatchSize: 1,
				},
				Work: []RitualStep{
					{Name: "step1", Minister: "forge", Act: "work on {{ .item }}"},
					{Name: "step2", Minister: "judge", Act: "verify {{ .item }}"},
				},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	forgeCh := make(chan *Task, 1)
	judgeCh := make(chan *Task, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				task.Done <- Result{Output: "forge done"}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-judgeCh:
				task.Done <- Result{Output: "judge done"}
			}
		}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}
	judgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "judge",
		tasksCh:      judgeCh,
	}

	shog := &Shogunate{
		ministers: map[string]Minister{"forge": forgeM, "judge": judgeM},
		logger:   slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "fork-item-test", "edict-fork-4", nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"given_context": map[string]interface{}{
			"items": []interface{}{"item1"},
		},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	// Verify fork results stored
	forkData, ok := exec.Data["fork"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fork data in exec.Data")
	}

	out, ok := forkData["out"].([]ForkResult)
	if !ok {
		t.Fatal("expected fork out results")
	}

	if len(out) != 1 {
		t.Errorf("expected 1 successful fork result, got %d", len(out))
	}
}

// TestExecuteForkStep_FailureHandling tests fork execution with failures
func TestExecuteForkStep_FailureHandling(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fork-failure-test",
		Steps: []RitualStep{
			{
				Name: "process-all",
				Fork: &ForkDef{
					Over:      "items",
					BatchSize: 1,
				},
				Work: []RitualStep{
					{Name: "process", Minister: "forge", Act: "process {{ .item }}"},
				},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	forgeCh := make(chan *Task, 10)
	callCount := 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				callCount++
				// Fail on second item
				if callCount == 2 {
					task.Done <- Result{Output: "partial", Err: fmt.Errorf("processing failed")}
				} else {
					task.Done <- Result{Output: "success"}
				}
			}
		}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}

	shog := &Shogunate{
		ministers: map[string]Minister{"forge": forgeM},
		logger:   slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "fork-failure-test", "edict-fork-5", nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"given_context": map[string]interface{}{
			"items": []interface{}{"item1", "item2", "item3"},
		},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	// Verify fork results include both success and failure
	forkData, ok := exec.Data["fork"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fork data in exec.Data")
	}

	out, ok := forkData["out"].([]ForkResult)
	if !ok {
		t.Fatal("expected fork out results")
	}

	errs, ok := forkData["err"].([]ForkResult)
	if !ok {
		t.Fatal("expected fork err results")
	}

	// Should have 2 successful (item1, item3) and 1 failed (item2)
	if len(out) != 2 {
		t.Errorf("expected 2 successful results, got %d", len(out))
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 failed result, got %d", len(errs))
	}
}

// TestExecuteForkStep_Notification tests that fork steps send proper notifications
func TestExecuteForkStep_Notification(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fork-notify-test",
		Steps: []RitualStep{
			{
				Name: "process-all",
				Fork: &ForkDef{
					Over:      "items",
					BatchSize: 1,
				},
				Work: []RitualStep{
					{Name: "process", Minister: "forge", Act: "process {{ .item }}"},
				},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	forgeCh := make(chan *Task, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				task.Done <- Result{Output: "done"}
			}
		}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}

	shog := &Shogunate{
		ministers: map[string]Minister{"forge": forgeM},
		logger:   slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	var messages []RitualStepMsg
	notify := func(msg any) {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			messages = append(messages, stepMsg)
		}
	}

	exec, err := runner.Start(ctx, "fork-notify-test", "edict-fork-6", nil, notify)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"given_context": map[string]interface{}{
			"items": []interface{}{"item1", "item2"},
		},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	// Verify fork notifications
	var forkStarted, forkCompleted int
	for _, msg := range messages {
		if msg.StepName == "process-all" {
			switch msg.Status {
			case "started":
				forkStarted++
				if !strings.Contains(msg.Message, "fork over") {
					t.Error("expected fork started message to mention 'fork over'")
				}
			case "completed":
				forkCompleted++
				if !strings.Contains(msg.Message, "Fork completed") {
					t.Error("expected fork completed message to contain summary")
				}
			}
		}
	}

	if forkStarted != 1 {
		t.Errorf("expected 1 fork started notification, got %d", forkStarted)
	}
	// Allow for 1 or 2 completed notifications (one per step execution context)
	if forkCompleted < 1 {
		t.Errorf("expected at least 1 fork completed notification, got %d", forkCompleted)
	}
}

// TestExecuteForkStep_TemplateExpansion tests template expansion in fork work steps
func TestExecuteForkStep_TemplateExpansion(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fork-template-test",
		Steps: []RitualStep{
			{
				Name: "process-all",
				Fork: &ForkDef{
					Over:      "files",
					BatchSize: 1,
				},
				Work: []RitualStep{
					{Name: "fix", Minister: "forge", Act: "Fix {{ .item }}"},
				},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	var mu sync.Mutex
	works := []string{}

	forgeCh := make(chan *Task, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				mu.Lock()
				works = append(works, task.Work)
				mu.Unlock()
				task.Done <- Result{Output: "fixed"}
			}
		}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}

	shog := &Shogunate{
		ministers: map[string]Minister{"forge": forgeM},
		logger:   slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "fork-template-test", "edict-fork-7", nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"given_context": map[string]interface{}{
			"files": []interface{}{"main.go", "utils.go"},
		},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Verify template was expanded with item
	if len(works) != 2 {
		t.Errorf("expected 2 work items, got %d", len(works))
	}

	foundMain := false
	foundUtils := false
	for _, w := range works {
		if strings.Contains(w, "main.go") {
			foundMain = true
		}
		if strings.Contains(w, "utils.go") {
			foundUtils = true
		}
	}

	if !foundMain {
		t.Error("expected template expansion for main.go")
	}
	if !foundUtils {
		t.Error("expected template expansion for utils.go")
	}
}

// TestExecuteForkStep_Cancelation tests fork execution cancellation
func TestExecuteForkStep_Cancelation(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fork-cancel-test",
		Steps: []RitualStep{
			{
				Name: "process-all",
				Fork: &ForkDef{
					Over:      "items",
					BatchSize: 1,
				},
				Work: []RitualStep{
					{Name: "process", Minister: "forge", Act: "process {{ .item }}"},
				},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	forgeCh := make(chan *Task, 10)
	processedCount := 0

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-forgeCh:
				processedCount++
				// Cancel after first item
				if processedCount == 1 {
					cancel()
				}
				// Simulate slow processing
				time.Sleep(100 * time.Millisecond)
				task.Done <- Result{Output: "done"}
			}
		}
	}()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      forgeCh,
	}

	shog := &Shogunate{
		ministers: map[string]Minister{"forge": forgeM},
		logger:   slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil)

	exec, err := runner.Start(ctx, "fork-cancel-test", "edict-fork-8", nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"given_context": map[string]interface{}{
			"items": []interface{}{"item1", "item2", "item3", "item4", "item5"},
		},
	}

	// Run will be cancelled
	_ = runner.Run(ctx, exec)

	// Verify not all items were processed
	if processedCount >= 5 {
		t.Errorf("expected cancellation to stop processing, but all %d items were processed", processedCount)
	}
}

// mockCallCountRunner implements runners.Runner for fork tests
type mockForkRunner struct {
	results []runners.Output
	idx     int
}

func (m *mockForkRunner) Run(ctx context.Context, input runners.Input) (runners.Output, error) {
	if m.idx >= len(m.results) {
		return runners.Output{Output: "", ExitCode: "0"}, nil
	}
	result := m.results[m.idx]
	m.idx++
	return result, nil
}

func (m *mockForkRunner) Restart(ctx context.Context) error    { return nil }
func (m *mockForkRunner) Close(ctx context.Context) error      { return nil }
func (m *mockForkRunner) AllowFallback(bool)                   {}
func (m *mockForkRunner) RunnerType() string                   { return "mock" }
func (m *mockForkRunner) SetMessageChannel(chan<- runners.Msg) {}
