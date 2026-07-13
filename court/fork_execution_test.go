package court

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
)

func ek(id uint) storage.EdictKey {
	return storage.EdictKey{ID: id, Username: "testuser", Project: "testproject"}
}

// TestExecuteForkStep_Parallel tests parallel fork execution
func TestExecuteForkStep_Parallel(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "fork-parallel-test",
		Steps: []RitualStep{
			{
				Name:     "prepare",
				Minister: "strategist",
				Task:     "prepare work units",
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	strategistM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "strategist",
		tasksCh:      make(chan *Task, 1),
		result:       "work units prepared",
	}
	go strategistM.Run(ctx)
	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 10),
		result:       "processed",
	}
	go forgeM.Run(ctx)

	shog := &Court{
		ministers: map[string]Minister{"strategist": strategistM, "forge": forgeM},
		logger:    slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil, repo.RepoInfo{})

	exec, err := runner.Start(ctx, "fork-parallel-test", ek(1), nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	// Inject work units into context
	exec.Data = storage.JSON{
		"work_units": []interface{}{"file1.go", "file2.go", "file3.go"},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	if exec.State != RitualStateCompleted {
		t.Errorf("Expected state 'completed', got %s", exec.State)
	}

	// Verify all items were processed via fork results
	forkData, ok := exec.Data["fork"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fork data in exec.Data")
	}
	out, ok := forkData["out"].([]ForkResult)
	if !ok {
		t.Fatal("expected fork out results")
	}
	if len(out) != 3 {
		t.Errorf("Expected 3 fork results, got %d", len(out))
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 10),
		result:       "done",
	}
	go forgeM.Run(ctx)

	shog := &Court{
		ministers: map[string]Minister{"forge": forgeM},
		logger:    slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil, repo.RepoInfo{})

	exec, err := runner.Start(ctx, "fork-sequential-test", ek(2), nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"items": []interface{}{"first", "second", "third"},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	// Verify all 3 items were processed via fork results
	forkData, ok := exec.Data["fork"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fork data in exec.Data")
	}
	out, ok := forkData["out"].([]ForkResult)
	if !ok {
		t.Fatal("expected fork out results")
	}
	if len(out) != 3 {
		t.Errorf("Expected 3 items processed, got %d", len(out))
	}

	// Verify sequential: with BatchSize=1, items run one at a time
	// Each result should have the correct item
	items := map[string]bool{}
	for _, r := range out {
		items[fmt.Sprintf("%v", r.Item)] = true
	}
	for _, expected := range []string{"first", "second", "third"} {
		if !items[expected] {
			t.Errorf("Expected item %q in results", expected)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 10),
		result:       "done",
	}
	go forgeM.Run(ctx)

	shog := &Court{
		ministers: map[string]Minister{"forge": forgeM},
		logger:    slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil, repo.RepoInfo{})

	exec, err := runner.Start(ctx, "fork-limit-test", ek(3), nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"items": []interface{}{"one", "two", "three", "four", "five"},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	// Verify only 2 items were processed (limit)
	forkData, ok := exec.Data["fork"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fork data in exec.Data")
	}
	out, ok := forkData["out"].([]ForkResult)
	if !ok {
		t.Fatal("expected fork out results")
	}
	if len(out) != 2 {
		t.Errorf("Expected 2 items processed (limit), got %d", len(out))
	}
}

// TestGetForkWorkUnits tests retrieving work units from context
func TestGetForkWorkUnits(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, nil, repo.RepoInfo{})

	tests := []struct {
		name      string
		execData  storage.JSON
		over      string
		wantCount int
		wantErr   bool
	}{
		{
			name: "from exec.Data",
			execData: storage.JSON{
				"files": []interface{}{"a.go", "b.go"},
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
	runner := NewRitualRunner(registry, nil, nil, db, nil, nil, repo.RepoInfo{})

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

// TestExecuteForkItem tests single fork item execution with multi-step work
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 1),
		result:       "forge done",
	}
	go forgeM.Run(ctx)
	judgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "judge",
		tasksCh:      make(chan *Task, 1),
		result:       "judge done",
	}
	go judgeM.Run(ctx)

	shog := &Court{
		ministers: map[string]Minister{"forge": forgeM, "judge": judgeM},
		logger:    slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil, repo.RepoInfo{})

	exec, err := runner.Start(ctx, "fork-item-test", ek(4), nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"items": []interface{}{"item1"},
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

	// Minister that fails with an error
	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 10),
		result:       "success",
		err:          fmt.Errorf("processing failed"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	go forgeM.Run(ctx)
	defer cancel()

	shog := &Court{
		ministers: map[string]Minister{"forge": forgeM},
		logger:    slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil, repo.RepoInfo{})

	exec, err := runner.Start(ctx, "fork-failure-test", ek(5), nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"items": []interface{}{"item1", "item2", "item3"},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	// Verify fork results include failures (all items fail because the model returns an error)
	forkData, ok := exec.Data["fork"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fork data in exec.Data")
	}

	errs, ok := forkData["err"].([]ForkResult)
	if !ok {
		t.Fatal("expected fork err results")
	}

	if len(errs) != 3 {
		t.Errorf("expected 3 failed results, got %d", len(errs))
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 10),
		result:       "done",
	}
	go forgeM.Run(ctx)

	shog := &Court{
		ministers: map[string]Minister{"forge": forgeM},
		logger:    slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil, repo.RepoInfo{})

	var messages []RitualStepMsg
	notify := func(msg any) {
		if stepMsg, ok := msg.(RitualStepMsg); ok {
			messages = append(messages, stepMsg)
		}
	}

	exec, err := runner.Start(ctx, "fork-notify-test", ek(6), nil, notify)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"items": []interface{}{"item1", "item2"},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	// Verify fork notifications
	var forkStarted, forkCompleted int
	var forkItemMsgs []RitualStepMsg
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
		if msg.ForkItem != "" {
			forkItemMsgs = append(forkItemMsgs, msg)
		}
	}

	if forkStarted != 1 {
		t.Errorf("expected 1 fork started notification, got %d", forkStarted)
	}
	// Allow for 1 or 2 completed notifications (one per step execution context)
	if forkCompleted < 1 {
		t.Errorf("expected at least 1 fork completed notification, got %d", forkCompleted)
	}

	// Verify ForkItem is populated on fork work step messages and that
	// StepIndex/TotalSteps reflect the work step's position within the fork.
	if len(forkItemMsgs) == 0 {
		t.Error("expected at least one notification with ForkItem set")
	}
	for _, msg := range forkItemMsgs {
		if msg.ForkItem != "1/2" && msg.ForkItem != "2/2" {
			t.Errorf("expected ForkItem to be '1/2' or '2/2', got %q", msg.ForkItem)
		}
		if msg.TotalSteps != 1 {
			t.Errorf("expected TotalSteps to be 1 within single-work-step fork, got %d", msg.TotalSteps)
		}
		if msg.StepIndex != 0 {
			t.Errorf("expected StepIndex to be 0 within single-work-step fork, got %d", msg.StepIndex)
		}
	}
	// Verify all expected ForkItem values are present
	seen := map[string]bool{}
	for _, msg := range forkItemMsgs {
		seen[msg.ForkItem] = true
	}
	if !seen["1/2"] {
		t.Error("expected ForkItem '1/2' in notifications")
	}
	if !seen["2/2"] {
		t.Error("expected ForkItem '2/2' in notifications")
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 10),
		result:       "fixed",
	}
	go forgeM.Run(ctx)

	shog := &Court{
		ministers: map[string]Minister{"forge": forgeM},
		logger:    slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil, repo.RepoInfo{})

	exec, err := runner.Start(ctx, "fork-template-test", ek(7), nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"files": []interface{}{"main.go", "utils.go"},
	}

	err = runner.Run(ctx, exec)
	if err != nil {
		t.Fatalf("Ritual run failed: %v", err)
	}

	// Verify all items processed
	forkData, ok := exec.Data["fork"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fork data in exec.Data")
	}
	out, ok := forkData["out"].([]ForkResult)
	if !ok {
		t.Fatal("expected fork out results")
	}

	if len(out) != 2 {
		t.Errorf("expected 2 work items, got %d", len(out))
	}

	// Verify each item was processed (items are in the fork results)
	items := map[string]bool{}
	for _, r := range out {
		items[fmt.Sprintf("%v", r.Item)] = true
	}
	if !items["main.go"] {
		t.Error("expected template expansion for main.go")
	}
	if !items["utils.go"] {
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

	ctx, cancel := context.WithCancel(context.Background())

	// Minister that returns context.Canceled error
	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 10),
		result:       "done",
		err:          context.Canceled,
	}
	go forgeM.Run(ctx)

	shog := &Court{
		ministers: map[string]Minister{"forge": forgeM},
		logger:    slog.Default(),
	}

	runner := NewRitualRunner(registry, shog.GetMinister, shog.PublishEvent, db, nil, nil, repo.RepoInfo{})

	exec, err := runner.Start(ctx, "fork-cancel-test", ek(8), nil, nil)
	if err != nil {
		t.Fatalf("Failed to start ritual: %v", err)
	}

	exec.Data = storage.JSON{
		"items": []interface{}{"item1", "item2", "item3", "item4", "item5"},
	}

	// Cancel immediately
	cancel()

	// Run will be cancelled
	_ = runner.Run(ctx, exec)

	// Verify fork results show errors (cancelled)
	forkData, ok := exec.Data["fork"].(map[string]interface{})
	if !ok {
		// Fork may not have started at all if cancelled fast enough
		return
	}
	out, _ := forkData["out"].([]ForkResult)
	if len(out) >= 5 {
		t.Errorf("expected cancellation to stop processing, but all %d items succeeded", len(out))
	}
}

// mockCallCountRunner implements runners.Runner for fork tests
