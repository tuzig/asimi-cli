package shogunate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "modernc.org/sqlite" // SQLite driver
)

func setupMinisterTestDB(t *testing.T) *gorm.DB {
	db, _ := setupMinisterTestDBWithPath(t)
	return db
}

func setupMinisterTestDBWithPath(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "minister_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := tmpDir + "/test.db"

	// Open database using database/sql with modernc.org/sqlite driver
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Use gorm with the existing connection
	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to initialize gorm: %v", err)
	}

	// Run migrations
	err = db.AutoMigrate(
		&storage.Edict{},
		&storage.Zhengming{},
		&storage.TianEvent{},
		&storage.TianEventDLQ{},
		&storage.Ling{},
		&storage.ForgeManifest{},
		&storage.JudgeVerdict{},
		&storage.CensorPrecedent{},
		&storage.MarshalIncident{},
		&storage.RulerCouncil{},
		&storage.RitualGuardCheckpoint{},
	)
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Create checkpoint table
	db.Exec(`CREATE TABLE IF NOT EXISTS ritual_guard_checkpoint (id INTEGER PRIMARY KEY, event_id INTEGER NOT NULL, updated_at DATETIME)`)

	return db, dbPath
}

func TestChancellor_EdictLifecycle(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create chancellor
	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	// Create an edict (starts in brewing phase)
	edict, err := CreateEdict(db, "test/repo#1", "Add a simple hello world function")
	assert.NoError(t, err)
	assert.NotNil(t, edict)

	// Verify edict was created with active status
	edict2, err := chancellor.GetEdict("test/repo#1")
	assert.NoError(t, err)
	assert.Equal(t, edict2.Status, storage.EdictActive)
}

func TestStrategist_DecomposeEdict(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Create edict
	base := NewMinisterBase(db, nil, nil)
	edict, err := CreateEdict(db, "test/repo#2", "Implement user authentication with login and logout")
	assert.NoError(t, err)
	assert.NotNil(t, edict)

	// Create strategist
	strategist := NewStrategist(base)

	// Execute planning (internal method)
	sealed, err := strategist.execute(ctx, "test/repo#2")
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	if !sealed {
		t.Error("Expected sealed after decomposition")
	}

	// Check ling was created
	ling, err := strategist.GetLingForEdict("test/repo#2")
	if err != nil {
		t.Fatalf("Failed to get ling: %v", err)
	}
	if len(ling) == 0 {
		t.Error("Expected at least one ling")
	}
}

func TestStrategist_AmbiguousIntent(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Create edict with ambiguous intent
	base := NewMinisterBase(db, nil, nil)
	edict, err := CreateEdict(db, "test/repo#3", "Fix it")
	assert.NoError(t, err)
	assert.NotNil(t, edict)

	// Create strategist
	strategist := NewStrategist(base)

	// Capture notifications to get the requestID
	var notifiedMsg ZhengmingPendingMsg
	var notifyMu sync.Mutex
	strategist.SetNotify(func(msg any) {
		notifyMu.Lock()
		defer notifyMu.Unlock()
		if m, ok := msg.(ZhengmingPendingMsg); ok {
			notifiedMsg = m
			// Answer via the full DB path (HandleZhengmingResponse)
			go strategist.HandleZhengmingResponse(ctx, m.RequestID, "Let me expand the requirements")
		}
	})

	// Execute - should request zhengming and block until resolved
	sealed, err := strategist.execute(ctx, "test/repo#3")
	require.NoError(t, err)
	assert.False(t, sealed, "expected not sealed for ambiguous intent")

	// Verify notification was sent with structured questions
	notifyMu.Lock()
	defer notifyMu.Unlock()
	assert.NotEmpty(t, notifiedMsg.RequestID, "expected zhengming notification")
	assert.NotEmpty(t, notifiedMsg.Questions, "expected structured questions")

	// Verify the DB was updated with answer and answered_at
	var req storage.Zhengming
	require.NoError(t, db.First(&req, "request_id = ?", notifiedMsg.RequestID).Error)
	assert.Equal(t, storage.ZhengmingAnswered, req.Status)
	assert.NotNil(t, req.AnsweredAt, "answered_at should be set")
}

func TestJudge_VerdictFlow(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Setup: create edict and manifest
	base := NewMinisterBase(db, nil, nil)
	edict, err := CreateEdict(db, "test/repo#4", "Test feature")
	assert.NoError(t, err)
	assert.NotNil(t, edict)

	forge := NewForge(base)
	manifestID, err := forge.StageManifest("test/repo#4", "", "test.go", "TestFunc", "hash1")
	assert.NoError(t, err)
	assert.NotEmpty(t, manifestID)

	// Create judge (no CI runner - will auto-pass, picks up "forged" manifests)
	judge := NewJudge(base, nil)

	// Execute judgment (internal method)
	sealed, err := judge.execute(ctx, "test/repo#4")
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	if !sealed {
		t.Error("Expected sealed after judgment")
	}

	// Check manifest is quenched
	allQuenched, _ := judge.AllManifestsQuenched("test/repo#4")
	if !allQuenched {
		t.Error("Expected all manifests quenched")
	}
}

func TestCensor_ReviewFlow(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Setup: create quenched manifest
	base := NewMinisterBase(db, nil, nil)
	edict, err := CreateEdict(db, "test/repo#5", "Review feature")
	assert.NoError(t, err)
	assert.NotNil(t, edict)

	forge := NewForge(base)
	manifestID, err := forge.StageManifest("test/repo#5", "", "review.go", "ReviewFunc", "hash2")
	assert.NoError(t, err)
	assert.NotEmpty(t, manifestID)

	judge := NewJudge(base, nil)
	verdictID, _ := judge.InsertVerdict(manifestID, "tests", storage.VerdictPassed, nil)
	judge.UpdateManifestStatus(manifestID, storage.ManifestQuenched, verdictID)

	// Create censor (no linter - will auto-approve)
	censor := NewCensor(base, nil)

	// Execute review (internal method)
	sealed, err := censor.execute(ctx, "test/repo#5")
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	if !sealed {
		t.Error("Expected sealed after review")
	}

	// Check no rejections
	noReject, _ := censor.NoRejections("test/repo#5")
	if !noReject {
		t.Error("Expected no rejections")
	}
}

func TestMarshal_IncidentFlow(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Setup: create edict and manifest
	base := NewMinisterBase(db, nil, nil)
	edict, err := CreateEdict(db, "test/repo#6", "Production feature")
	assert.NoError(t, err)
	assert.NotNil(t, edict)

	forge := NewForge(base)
	manifestID, err := forge.StageManifest("test/repo#6", "", "prod.go", "ProdFunc", "hash3")
	assert.NoError(t, err)
	assert.NotEmpty(t, manifestID)
	// Set commit_hash directly for marshal incident lookup
	db.Model(&storage.ForgeManifest{}).Where("edict_id = ?", "test/repo#6").
		Update("commit_hash", "prodcommit789")

	// Create marshal
	marshal := NewMarshal(base, nil)

	// Report incident
	err = marshal.OnIncident(ctx, "sentry-456", "prodcommit789")
	if err != nil {
		t.Fatalf("Failed to handle incident: %v", err)
	}

	// Check incident was logged
	incident, err := marshal.GetIncident("sentry-456")
	if err != nil {
		t.Fatalf("Failed to get incident: %v", err)
	}
	if incident == nil {
		t.Error("Expected incident to be logged")
	}
}

func TestChancellor_CancelEdict(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	edict, err := CreateEdict(db, "test/repo#7", "Feature to cancel")
	assert.NoError(t, err)
	assert.NotNil(t, edict)
	err = chancellor.CancelEdictWithContext(ctx, "test/repo#7", "@user", "No longer needed")
	if err != nil {
		t.Fatalf("Failed to cancel: %v", err)
	}

	// Check cancelled
	edict, _ = chancellor.GetEdict("test/repo#7")
	if edict.Status != storage.EdictCancelled {
		t.Errorf("Expected status cancelled, got %s", edict.Status)
	}
}

func TestStrategist_CircularDependencyDetection(t *testing.T) {
	strategist := &Strategist{}

	// Create ling with circular dependency
	ling := []storage.Ling{
		{LingID: "a", Dependencies: storage.StringArray{"b"}},
		{LingID: "b", Dependencies: storage.StringArray{"c"}},
		{LingID: "c", Dependencies: storage.StringArray{"a"}}, // Circular!
	}

	err := strategist.validateDependencies(ling)
	if err == nil {
		t.Error("Expected error for circular dependency")
	}
}

func TestStrategist_ValidDependencies(t *testing.T) {
	strategist := &Strategist{}

	// Create ling with valid DAG
	ling := []storage.Ling{
		{LingID: "a", Dependencies: storage.StringArray{}},
		{LingID: "b", Dependencies: storage.StringArray{"a"}},
		{LingID: "c", Dependencies: storage.StringArray{"a", "b"}},
	}

	err := strategist.validateDependencies(ling)
	if err != nil {
		t.Errorf("Expected no error for valid dependencies: %v", err)
	}
}

// TestHappyFlowE2E tests the complete Task flow:
// Chancellor -> invoke_minister tool -> Minister receives Task -> Minister sends Result (synchronous)
func TestHappyFlowE2E(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create Chancellor and Shogunate
	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	// Create a simple Shogunate with just Forge for this test
	forge := NewForge(base)
	shogunate := &Shogunate{
		db: db,
		ministers: map[string]Minister{
			chancellor.ID(): chancellor,
			forge.ID():      forge,
		},
	}
	chancellor.SetShogunate(shogunate)

	// Create an edict for the test
	edictID := "test-e2e-edict"
	edict, err := CreateEdict(db, edictID, "E2E test edict")
	assert.NoError(t, err)
	assert.NotNil(t, edict)
	// Start the Forge's Run loop in a goroutine
	go forge.Run(ctx)

	// Create the InvokeMinisterTool
	tool := InvokeMinisterTool{chancellor: chancellor}

	// Invoke the Forge minister with a trivial task
	// With synchronous blocking, this call blocks until minister replies
	taskInput := `{"minister_id": "forge", "edict_id": "test-e2e-edict", "task": "please reply with 'hello world'"}`
	result, err := tool.Call(ctx, taskInput)
	if err != nil {
		t.Fatalf("Failed to invoke minister: %v", err)
	}

	// Verify the tool returned success with completed status
	if result == "" {
		t.Error("Expected non-empty result from invoke_minister")
	}
	t.Logf("invoke_minister result: %s", result)

	// Parse the result to verify it contains completion info
	var response struct {
		MinisterID string `json:"minister_id"`
		EdictID    string `json:"edict_id"`
		Status     string `json:"status"`
		Sealed     bool   `json:"sealed"`
		Output     string `json:"output"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if response.Status != "completed" {
		t.Errorf("Expected status 'completed', got %s", response.Status)
	}
	if response.MinisterID != "forge" {
		t.Errorf("Expected MinisterID 'forge', got %s", response.MinisterID)
	}
	if response.EdictID != edictID {
		t.Errorf("Expected EdictID %s, got %s", edictID, response.EdictID)
	}
	if !response.Sealed {
		t.Error("Expected Sealed=true from Forge")
	}
	t.Logf("Received response: minister=%s, edict=%s, sealed=%v, output=%s",
		response.MinisterID, response.EdictID, response.Sealed, response.Output)
}

// TestInvokeMinisterTool_InvalidMinister tests error handling for unknown ministers
func TestInvokeMinisterTool_InvalidMinister(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)
	shogunate := &Shogunate{
		db: db,
		ministers: map[string]Minister{
			chancellor.ID(): chancellor,
		},
	}
	chancellor.SetShogunate(shogunate)

	tool := InvokeMinisterTool{chancellor: chancellor}

	// Try to invoke a non-existent minister
	taskInput := `{"minister_id": "unknown", "edict_id": "test", "task": "hello"}`
	_, err := tool.Call(ctx, taskInput)
	if err == nil {
		t.Error("Expected error for unknown minister")
	}
}

// TestInvokeMinisterTool_MissingTask tests error handling for missing task parameter
func TestInvokeMinisterTool_MissingTask(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	tool := InvokeMinisterTool{chancellor: chancellor}

	// Missing task parameter
	taskInput := `{"minister_id": "forge", "edict_id": "test"}`
	_, err := tool.Call(ctx, taskInput)
	if err == nil {
		t.Error("Expected error for missing task parameter")
	}
}

// TestInvokeMinisterTool_InvalidJSON tests error handling for malformed JSON input
func TestInvokeMinisterTool_InvalidJSON(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil)
	chancellor := NewChancellor(base)
	tool := InvokeMinisterTool{chancellor: chancellor}

	_, err := tool.Call(context.Background(), `not json`)
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid input") {
		t.Errorf("Expected 'invalid input' error, got: %v", err)
	}
}

// TestInvokeMinisterTool_MissingMinisterID tests error handling for missing minister_id
func TestInvokeMinisterTool_MissingMinisterID(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil)
	chancellor := NewChancellor(base)
	tool := InvokeMinisterTool{chancellor: chancellor}

	_, err := tool.Call(context.Background(), `{"edict_id": "e1", "task": "do something"}`)
	if err == nil {
		t.Fatal("Expected error for missing minister_id")
	}
	if err.Error() != "minister_id is required" {
		t.Errorf("Expected 'minister_id is required', got: %v", err)
	}
}

// TestInvokeMinisterTool_MissingEdictID tests error handling for missing edict_id
func TestInvokeMinisterTool_MissingEdictID(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil)
	chancellor := NewChancellor(base)
	tool := InvokeMinisterTool{chancellor: chancellor}

	_, err := tool.Call(context.Background(), `{"minister_id": "forge", "task": "do something"}`)
	if err == nil {
		t.Fatal("Expected error for missing edict_id")
	}
	if err.Error() != "edict_id is required" {
		t.Errorf("Expected 'edict_id is required', got: %v", err)
	}
}

// TestInvokeMinisterTool_MinisterReturnsError tests that a Result with Err is propagated
func TestInvokeMinisterTool_MinisterReturnsError(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	// Create a fake minister that returns an error in Result
	fake := &fakeMinister{id: "failing", tasks: make(chan *Task, 1)}
	shogunate := &Shogunate{
		db:        db,
		ministers: map[string]Minister{"failing": fake},
	}
	chancellor.SetShogunate(shogunate)

	// Start the fake minister: reads task, sends error result
	go func() {
		task := <-fake.tasks
		task.Done <- Result{
			MinisterID: "failing",
			Err:        errors.New("something broke"),
		}
	}()

	tool := InvokeMinisterTool{chancellor: chancellor}
	_, err := tool.Call(ctx, `{"minister_id": "failing", "edict_id": "e1", "task": "break"}`)
	if err == nil {
		t.Fatal("Expected error when minister returns Result.Err")
	}
	if !strings.Contains(err.Error(), "something broke") {
		t.Errorf("Expected 'something broke' in error, got: %v", err)
	}
}

// TestInvokeMinisterTool_ContextCancelledDuringSend tests cancellation while sending task
func TestInvokeMinisterTool_ContextCancelledDuringSend(t *testing.T) {
	db := setupMinisterTestDB(t)

	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	// Create a fake minister with a full task channel (buffer 0, no reader)
	fake := &fakeMinister{id: "blocked", tasks: make(chan *Task)} // unbuffered, no goroutine reading
	shogunate := &Shogunate{
		db:        db,
		ministers: map[string]Minister{"blocked": fake},
	}
	chancellor.SetShogunate(shogunate)

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tool := InvokeMinisterTool{chancellor: chancellor}
	_, err := tool.Call(ctx, `{"minister_id": "blocked", "edict_id": "e1", "task": "go"}`)
	if err == nil {
		t.Fatal("Expected error when context is cancelled during send")
	}
	if !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("Expected 'context cancelled' in error, got: %v", err)
	}
}

// TestInvokeMinisterTool_ContextCancelledDuringWait tests cancellation while waiting for result
func TestInvokeMinisterTool_ContextCancelledDuringWait(t *testing.T) {
	db := setupMinisterTestDB(t)

	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	// Create a fake minister that accepts but never replies
	fake := &fakeMinister{id: "slow", tasks: make(chan *Task, 1)}
	shogunate := &Shogunate{
		db:        db,
		ministers: map[string]Minister{"slow": fake},
	}
	chancellor.SetShogunate(shogunate)

	// Drain the task channel so the send succeeds, but never reply
	go func() { <-fake.tasks }()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay to let the send succeed
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	tool := InvokeMinisterTool{chancellor: chancellor}
	_, err := tool.Call(ctx, `{"minister_id": "slow", "edict_id": "e1", "task": "wait"}`)
	if err == nil {
		t.Fatal("Expected error when context is cancelled during wait")
	}
	
	assert.Equal(t, err.Error, "context canceled")
}

// TestInvokeMinisterTool_Notifications verifies MinisterInvokingMsg and MinisterCompletedMsg are sent
func TestInvokeMinisterTool_Notifications(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	// Collect notifications
	var mu sync.Mutex
	var notifications []any
	chancellor.SetNotify(internal.NotifyFunc(func(msg any) {
		mu.Lock()
		defer mu.Unlock()
		notifications = append(notifications, msg)
	}))

	// Create a fake minister that succeeds
	fake := &fakeMinister{id: "notifier", tasks: make(chan *Task, 1)}
	shogunate := &Shogunate{
		db:        db,
		ministers: map[string]Minister{"notifier": fake},
	}
	chancellor.SetShogunate(shogunate)

	go func() {
		task := <-fake.tasks
		task.Done <- Result{MinisterID: "notifier", Sealed: true, Output: "done"}
	}()

	tool := InvokeMinisterTool{chancellor: chancellor}
	_, err := tool.Call(ctx, `{"minister_id": "notifier", "edict_id": "e1", "task": "notify me"}`)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(notifications) != 2 {
		t.Fatalf("Expected 2 notifications, got %d", len(notifications))
	}

	// First: MinisterInvokingMsg
	invoking, ok := notifications[0].(MinisterInvokingMsg)
	if !ok {
		t.Fatalf("Expected MinisterInvokingMsg, got %T", notifications[0])
	}
	if invoking.MinisterID != "notifier" || invoking.EdictID != "e1" || invoking.Task != "notify me" {
		t.Errorf("Unexpected invoking msg: %+v", invoking)
	}

	// Second: MinisterCompletedMsg
	completed, ok := notifications[1].(MinisterCompletedMsg)
	if !ok {
		t.Fatalf("Expected MinisterCompletedMsg, got %T", notifications[1])
	}
	if completed.MinisterID != "notifier" || completed.EdictID != "e1" || completed.Error != nil {
		t.Errorf("Unexpected completed msg: %+v", completed)
	}
	if !completed.Sealed {
		t.Error("Expected Sealed=true in completed notification")
	}
}

// TestInvokeMinisterTool_NotificationsOnError verifies error notifications are sent
func TestInvokeMinisterTool_NotificationsOnError(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	var mu sync.Mutex
	var notifications []any
	chancellor.SetNotify(internal.NotifyFunc(func(msg any) {
		mu.Lock()
		defer mu.Unlock()
		notifications = append(notifications, msg)
	}))

	// No ministers registered -> unknown minister error
	shogunate := &Shogunate{
		db:        db,
		ministers: map[string]Minister{},
	}
	chancellor.SetShogunate(shogunate)

	tool := InvokeMinisterTool{chancellor: chancellor}
	_, _ = tool.Call(ctx, `{"minister_id": "ghost", "edict_id": "e1", "task": "haunt"}`)

	mu.Lock()
	defer mu.Unlock()

	// Should have invoking + completed-with-error
	if len(notifications) != 2 {
		t.Fatalf("Expected 2 notifications, got %d", len(notifications))
	}

	completed, ok := notifications[1].(MinisterCompletedMsg)
	if !ok {
		t.Fatalf("Expected MinisterCompletedMsg, got %T", notifications[1])
	}
	if completed.Error == nil {
		t.Error("Expected error in completed notification for unknown minister")
	}
}

// TestInvokeMinisterTool_Format tests the Format method
func TestInvokeMinisterTool_Format(t *testing.T) {
	tool := InvokeMinisterTool{}

	// Normal case
	output := tool.Format(`{"minister_id": "forge", "task": "build it"}`, `{"status":"ok"}`, nil)
	if !strings.Contains(output, "InvokeMinister") {
		t.Errorf("Expected 'InvokeMinister' in output, got: %s", output)
	}
	if !strings.Contains(output, "forge") {
		t.Errorf("Expected 'forge' in output, got: %s", output)
	}
	if !strings.Contains(output, "[build it]") {
		t.Errorf("Expected '[build it]' in output, got: %s", output)
	}

	// Error case
	output = tool.Format(`{"minister_id": "forge", "task": "x"}`, "", errors.New("boom"))
	if !strings.Contains(output, "Error: boom") {
		t.Errorf("Expected 'Error: boom' in output, got: %s", output)
	}

	// Long task truncation
	longTask := strings.Repeat("a", 50)
	output = tool.Format(`{"minister_id": "forge", "task": "`+longTask+`"}`, `{}`, nil)
	if !strings.Contains(output, "...") {
		t.Errorf("Expected truncation with '...' for long task, got: %s", output)
	}
}

// TestBuildSystemPrompt_EdictID verifies the edict ID is injected into the scratchpad
func TestBuildSystemPrompt_EdictID(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil)

	fake := &fakeMinister{MinisterBase: base, id: "test"}

	// With edict ID — should appear in system prompt alongside Realm and role text
	prompt := buildSystemPrompt(fake, nil, "edict-123456")
	if !strings.Contains(prompt, "Current Edict: edict-123456") {
		t.Errorf("Expected edict ID in system prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "You are a test minister.") {
		t.Errorf("Expected role text in system prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "幕府") {
		t.Errorf("Expected Realm (幕府) in system prompt, got:\n%s", prompt)
	}

	// Without edict ID — should not contain "Current Edict"
	prompt = buildSystemPrompt(fake, nil, "")
	if strings.Contains(prompt, "Current Edict") {
		t.Errorf("Expected no edict ID in system prompt, got:\n%s", prompt)
	}
}

// TestBuildSystemPrompt_Scratchpad verifies scratchpad is included with minister ID heading
func TestBuildSystemPrompt_Scratchpad(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil)

	fake := &fakeMinisterWithScratchpad{
		fakeMinister: fakeMinister{MinisterBase: base, id: "strategist"},
		scratchpad:   "# Available Rituals\n- implement: Run implementation",
	}

	prompt := buildSystemPrompt(fake, nil, "")
	if !regexp.MustCompile(`--- .* Scratchpad ---`).MatchString(prompt) {
		t.Errorf("Expected scratchpad heading matching '--- .* Scratchpad ---', got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Available Rituals") {
		t.Errorf("Expected scratchpad content in system prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "--- End of scratchpad ---") {
		t.Errorf("Expected scratchpad end marker, got:\n%s", prompt)
	}
}

// fakeMinisterWithScratchpad extends fakeMinister with a non-empty scratchpad
type fakeMinisterWithScratchpad struct {
	fakeMinister
	scratchpad string
}

func (f *fakeMinisterWithScratchpad) Scratchpad() string { return f.scratchpad }

// fakeMinister is a minimal Minister implementation for testing
type fakeMinister struct {
	MinisterBase
	id    string
	tasks chan *Task
}

func (f *fakeMinister) ID() string              { return f.id }
func (f *fakeMinister) SystemPrompt() string    { return "You are a test minister." }
func (f *fakeMinister) Title() string           { return "Fake" }
func (f *fakeMinister) Tools() []Tool           { return nil }
func (f *fakeMinister) Tasks() chan<- *Task     { return f.tasks }
func (f *fakeMinister) Run(ctx context.Context) {}

// TestChancellor_ScratchpadIncludesRituals verifies the Chancellor's system prompt
// contains ritual names and descriptions from the registry.
func TestChancellor_ScratchpadIncludesRituals(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	registry := NewRitualRegistry()
	registry.Register(&RitualDef{Name: "swift-strike", Description: "The Swift Strike (S)"})
	registry.Register(&RitualDef{Name: "grand-campaign", Description: "The Grand Campaign (L)"})

	shogunate := &Shogunate{
		db:             db,
		ministers:      map[string]Minister{chancellor.ID(): chancellor},
		ritualRegistry: registry,
	}
	chancellor.SetShogunate(shogunate)

	prompt := buildSystemPrompt(chancellor, nil, "")

	if !strings.Contains(prompt, "swift-strike") {
		t.Errorf("Expected ritual name 'swift-strike' in system prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "The Swift Strike (S)") {
		t.Errorf("Expected ritual description in system prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "grand-campaign") {
		t.Errorf("Expected ritual name 'grand-campaign' in system prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "The Grand Campaign (L)") {
		t.Errorf("Expected ritual description in system prompt, got:\n%s", prompt)
	}
}

// TestChancellor_GetDBPath tests that getDBPath correctly extracts the database path from gorm.DB
func TestChancellor_GetDBPath(t *testing.T) {
	db, expectedPath := setupMinisterTestDBWithPath(t)

	base := NewMinisterBase(db, nil, nil)
	chancellor := NewChancellor(base)

	// Call getDBPath and verify it returns the correct path
	gotPath := chancellor.getDBPath()

	// Resolve symlinks for comparison (e.g., /tmp -> /private/tmp on macOS)
	expectedResolved, _ := filepath.EvalSymlinks(expectedPath)
	gotResolved, _ := filepath.EvalSymlinks(gotPath)

	if gotResolved != expectedResolved {
		t.Errorf("getDBPath() = %q, want %q", gotPath, expectedPath)
	}
}

// TestChancellor_GetDBPath_NilDB tests getDBPath returns empty string when db is nil
func TestChancellor_GetDBPath_NilDB(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil)
	chancellor := NewChancellor(base)

	gotPath := chancellor.getDBPath()
	if gotPath != "" {
		t.Errorf("getDBPath() with nil db = %q, want empty string", gotPath)
	}
}

func TestZhengmingWaitForAnswer(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil)

	// Resolve from another goroutine
	go func() {
		time.Sleep(10 * time.Millisecond)
		ok := base.ResolveZhengmingWaiter("req-1", "Yes")
		assert.True(t, ok, "ResolveZhengmingWaiter should return true")
	}()

	answer, err := base.WaitForAnswer(context.Background(), "req-1")
	require.NoError(t, err)
	assert.Equal(t, "Yes", answer)
}

func TestZhengmingWaitForAnswer_ContextCancelled(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := base.WaitForAnswer(ctx, "req-2")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Verify waiter was cleaned up
	base.zhengmingMu.Lock()
	_, exists := base.zhengmingWaiters["req-2"]
	base.zhengmingMu.Unlock()
	assert.False(t, exists, "waiter should be cleaned up after context cancellation")
}

func TestZhengmingMultipleQuestions(t *testing.T) {
	questions := []storage.ZhengmingQuestion{
		{Text: "What should it do?", Options: []string{"Option A", "Option B"}},
		{Text: "How urgent?", Options: []string{"Now", "Later", "Never"}},
	}

	data, err := json.Marshal(questions)
	require.NoError(t, err)

	var parsed []storage.ZhengmingQuestion
	require.NoError(t, json.Unmarshal(data, &parsed))

	require.Len(t, parsed, 2)
	assert.Equal(t, "What should it do?", parsed[0].Text)
	assert.Len(t, parsed[0].Options, 2)
	assert.Equal(t, "How urgent?", parsed[1].Text)
	assert.Len(t, parsed[1].Options, 3)
}

// TestFormatGivenContext verifies edict details are rendered as readable markdown
func TestFormatGivenContext(t *testing.T) {
	ctx := map[string]interface{}{
		"edict": map[string]interface{}{
			"edict_id": "edict-abc123",
			"intent":   "Add user authentication with OAuth2",
			"status":   "active",
		},
		"manifests": []map[string]interface{}{
			{"manifest_id": "m-1", "file_path": "auth.go", "status": "staged"},
		},
	}

	result := formatScratchpad(ctx)

	// Should contain both section headings (sorted alphabetically)
	if !strings.Contains(result, "## edict") {
		t.Errorf("Expected '## edict' heading, got:\n%s", result)
	}
	if !strings.Contains(result, "## manifests") {
		t.Errorf("Expected '## manifests' heading, got:\n%s", result)
	}
	// Should contain the edict intent
	if !strings.Contains(result, "Add user authentication with OAuth2") {
		t.Errorf("Expected edict intent in output, got:\n%s", result)
	}
	// Should contain JSON formatting
	if !strings.Contains(result, "```json") {
		t.Errorf("Expected JSON code block, got:\n%s", result)
	}
	// Nil/empty context should return empty string
	if formatScratchpad(nil) != "" {
		t.Error("Expected empty string for nil context")
	}
	if formatScratchpad(map[string]interface{}{}) != "" {
		t.Error("Expected empty string for empty context")
	}
}

// TestFormatGivenContext_StringValues verifies plain strings are rendered without JSON wrapping
func TestFormatGivenContext_StringValues(t *testing.T) {
	ctx := map[string]interface{}{
		"notes": "some plain text output",
	}
	result := formatScratchpad(ctx)
	if !strings.Contains(result, "some plain text output") {
		t.Errorf("Expected plain text, got:\n%s", result)
	}
	if strings.Contains(result, "```json") {
		t.Errorf("Plain strings should not be wrapped in JSON code blocks, got:\n%s", result)
	}
}

// TestBuildSystemPrompt_GivenContext verifies that pre-formatted given context
// from a ritual is included in the system prompt when passed to buildSystemPrompt.
func TestBuildSystemPrompt_GivenContext(t *testing.T) {
	base := NewMinisterBase(nil, nil, nil)
	fake := &fakeMinister{MinisterBase: base, id: "forge"}

	// Simulate what the ritual does: format given context map → markdown string
	scratchpad := formatScratchpad(map[string]interface{}{
		"edict": map[string]interface{}{
			"edict_id": "edict-xyz",
			"intent":   "Implement dark mode for the dashboard",
			"status":   "active",
		},
	})

	// With scratchpad — edict intent should appear in system prompt
	prompt := buildSystemPrompt(fake, nil, "edict-xyz", scratchpad)
	if !strings.Contains(prompt, "Implement dark mode for the dashboard") {
		t.Errorf("Expected edict intent in system prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "# Given Context") {
		t.Errorf("Expected '# Given Context' heading, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Current Edict: edict-xyz") {
		t.Errorf("Expected edict ID in system prompt, got:\n%s", prompt)
	}

	// Without given context — should not contain "Given Context"
	prompt = buildSystemPrompt(fake, nil, "edict-xyz")
	if strings.Contains(prompt, "# Given Context") {
		t.Errorf("Expected no scratchpad without scratchpad param, got:\n%s", prompt)
	}
}
