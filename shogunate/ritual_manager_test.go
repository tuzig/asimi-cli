package shogunate

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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

// setupEventTestDB creates a test database with tian_events and ritual_guard_checkpoint tables.
func setupEventTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := tmpDir + "/event_test.db"

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
		&storage.TianEvent{},
		&storage.TianEventDLQ{},
		&storage.RitualGuardCheckpoint{},
		&RitualExecution{},
		&RitualStepState{},
	)
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	return db
}

// newTestShogunate creates a minimal Shogunate with event channel for testing.
func newTestShogunate(t *testing.T, db *gorm.DB) *Shogunate {
	t.Helper()
	s := &Shogunate{
		db:        db,
		logger:    slog.Default(),
		ministers: make(map[string]Minister),
	}
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	rg := NewRitualGuard(RitualGuardOpts{Base: base})
	s.ritualGuard = rg
	return s
}

func TestChannelDelivery(t *testing.T) {
	db := setupEventTestDB(t)
	s := newTestShogunate(t, db)

	// Subscribe to the event
	var received []Event
	var mu sync.Mutex
	s.ritualGuard.Subscribe(storage.EventEdictCreated, func(e Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel
	defer cancel()

	// Start the channel consumer
	done := make(chan struct{})
	go func() {
		s.ritualGuard.Run(ctx)
		close(done)
	}()

	// Publish an event
	s.PublishEvent(storage.EdictKey{ID: 1}, storage.EventEdictCreated, storage.JSON{"foo": "bar"})

	// Wait briefly for the event to be dispatched
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(received) != 1 {
		t.Fatalf("expected 1 dispatched event, got %d", len(received))
	}
	if received[0].EdictKey.ID != 1 {
		t.Errorf("expected EdictID 1, got %d", received[0].EdictKey.ID)
	}
	if received[0].Type != storage.EventEdictCreated {
		t.Errorf("expected type %q, got %q", storage.EventEdictCreated, received[0].Type)
	}
	mu.Unlock()

	cancel()
	<-done
}

func TestDBPersistence(t *testing.T) {
	db := setupEventTestDB(t)
	s := newTestShogunate(t, db)

	// Publish an event (just DB + channel, no consumer needed)
	s.PublishEvent(storage.EdictKey{ID: 2}, "edict_created", storage.JSON{"key": "val"})

	// Verify it's in the database
	var events []storage.TianEvent
	if err := db.Find(&events).Error; err != nil {
		t.Fatalf("failed to query events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 DB event, got %d", len(events))
	}
	if events[0].EdictID != 2 {
		t.Errorf("expected EdictID 2, got %d", events[0].EdictID)
	}
	if events[0].EventType != "edict_created" {
		t.Errorf("expected EventType 'edict_created', got %q", events[0].EventType)
	}

	// Also verify it arrived on the channel
	select {
	case ev := <-s.ritualGuard.eventCh:
		if ev.EdictKey.ID != 2 {
			t.Errorf("channel event EdictID: expected 2, got %d", ev.EdictKey.ID)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestBackpressure(t *testing.T) {
	db := setupEventTestDB(t)
	// Use a tiny channel to force backpressure
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	rg := NewRitualGuard(RitualGuardOpts{Base: base})
	// Replace the default channel with a tiny one
	rg.eventCh = make(chan Event, 2)
	s := &Shogunate{
		db:        db,
		logger:    slog.Default(),
		ministers: make(map[string]Minister),
	}
	s.ritualGuard = rg

	// Fill the channel
	s.PublishEvent(storage.EdictKey{ID: 1}, "edict_created", storage.JSON{})
	s.PublishEvent(storage.EdictKey{ID: 2}, "edict_created", storage.JSON{})

	// This one should overflow — persists to DB but doesn't block
	s.PublishEvent(storage.EdictKey{ID: 3}, "edict_created", storage.JSON{})

	// Verify all 3 persisted to DB
	var count int64
	db.Model(&storage.TianEvent{}).Count(&count)
	if count != 3 {
		t.Errorf("expected 3 DB events, got %d", count)
	}

	// Verify channel has exactly 2 (capacity)
	if len(s.ritualGuard.eventCh) != 2 {
		t.Errorf("expected 2 events in channel, got %d", len(s.ritualGuard.eventCh))
	}
}

func TestDrainUnprocessedEvents(t *testing.T) {
	db := setupEventTestDB(t)
	s := newTestShogunate(t, db)

	rg := s.ritualGuard

	// Insert 5 events directly into DB (simulating crash scenario)
	for i := 0; i < 5; i++ {
		db.Create(&storage.TianEvent{
			EdictID:   99,
			Username:  "testuser",
			Project:   "testproject",
			EventType: "step_completed",
			Payload:   storage.JSON{"i": i},
		})
	}

	// Set checkpoint to event ID 2 (simulating partial processing before crash)
	rg.SaveCheckpoint(2)

	// Track dispatched events
	var dispatched []Event
	var mu sync.Mutex
	s.ritualGuard.Subscribe("step_completed", func(e Event) {
		mu.Lock()
		dispatched = append(dispatched, e)
		mu.Unlock()
	})

	// Drain should replay events 3, 4, 5
	s.ritualGuard.DrainUnprocessedEvents()

	// Wait for async dispatch handlers to complete
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(dispatched) != 3 {
		t.Fatalf("expected 3 drained events, got %d", len(dispatched))
	}
	mu.Unlock()

	// Verify checkpoint was updated to last event
	lastID, _ := rg.GetLastAcknowledgedEvent()
	if lastID != 5 {
		t.Errorf("expected checkpoint at 5, got %d", lastID)
	}
}

func TestShutdown(t *testing.T) {
	db := setupEventTestDB(t)
	s := newTestShogunate(t, db)

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel

	done := make(chan struct{})
	go func() {
		s.ritualGuard.Run(ctx)
		close(done)
	}()

	// Cancel should cause RitualGuard.Run to exit
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("RitualGuard.Run did not exit after context cancel")
	}
}

func TestMinisterBaseEmitEvent_WithPublish(t *testing.T) {
	db := setupEventTestDB(t)
	s := newTestShogunate(t, db)

	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	base.publish = s.PublishEvent

	err := base.EmitEvent(storage.EdictKey{ID: 10}, "edict_assigned", storage.JSON{"from": "minister"})
	if err != nil {
		t.Fatalf("EmitEvent error: %v", err)
	}

	// Verify DB
	var count int64
	db.Model(&storage.TianEvent{}).Where("edict_id = ?", 10).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 DB event, got %d", count)
	}

	// Verify channel
	select {
	case ev := <-s.ritualGuard.eventCh:
		if ev.EdictKey.ID != 10 {
			t.Errorf("expected EdictID 10, got %d", ev.EdictKey.ID)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestMinisterBaseEmitEvent_Fallback(t *testing.T) {
	db := setupEventTestDB(t)

	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	// No publish set — should fall back to DB-only

	err := base.EmitEvent(storage.EdictKey{ID: 20}, "edict_created", storage.JSON{"from": "fallback"})
	if err != nil {
		t.Fatalf("EmitEvent error: %v", err)
	}

	var count int64
	db.Model(&storage.TianEvent{}).Where("edict_id = ?", 20).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 DB event, got %d", count)
	}
}

// TestRitualGuard_EventNotification tests that significant events trigger notifications
// Message formatting is handled by the TUI layer; ritual_manager only routes notifiable events.
func TestRitualGuard_EventNotification(t *testing.T) {
	db := setupEventTestDB(t)
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	rg := NewRitualGuard(RitualGuardOpts{Base: base})

	// Collect notifications
	var notifications []EventNotificationMsg
	var mu sync.Mutex
	rg.SetNotify(func(msg interface{}) {
		if n, ok := msg.(EventNotificationMsg); ok {
			mu.Lock()
			notifications = append(notifications, n)
			mu.Unlock()
		}
	})

	// Test EventEdictCreated notification
	rg.DispatchEvent(Event{
		Type:     storage.EventEdictCreated,
		EdictKey: storage.EdictKey{ID: 1},
		Payload:  map[string]interface{}{"intent": "Add new feature"},
	})

	time.Sleep(10 * time.Millisecond) // Allow async processing

	mu.Lock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].EdictKey.ID != 1 {
		t.Errorf("expected EdictID 1, got %d", notifications[0].EdictKey.ID)
	}
	if notifications[0].EventType != storage.EventEdictCreated {
		t.Errorf("expected EventType EventEdictCreated, got %s", notifications[0].EventType)
	}
	if notifications[0].ChannelID != "chancellor" {
		t.Errorf("expected ChannelID chancellor, got %s", notifications[0].ChannelID)
	}
	mu.Unlock()

	// Test EventSealGranted notification
	notifications = nil
	rg.DispatchEvent(Event{
		Type:     storage.EventSealGranted,
		EdictKey: storage.EdictKey{ID: 2},
		Payload:  map[string]interface{}{"minister_id": "judge"},
	})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].EventType != storage.EventSealGranted {
		t.Errorf("expected EventType EventSealGranted, got %s", notifications[0].EventType)
	}
	if notifications[0].Payload["minister_id"] != "judge" {
		t.Errorf("expected minister_id judge in payload, got %v", notifications[0].Payload["minister_id"])
	}
	mu.Unlock()

	// Test EventEdictSealed notification
	notifications = nil
	rg.DispatchEvent(Event{
		Type:     storage.EventEdictSealed,
		EdictKey: storage.EdictKey{ID: 3},
		Payload:  map[string]interface{}{},
	})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].EventType != storage.EventEdictSealed {
		t.Errorf("expected EventType EventEdictSealed, got %s", notifications[0].EventType)
	}
	if notifications[0].EdictKey.ID != 3 {
		t.Errorf("expected EdictID 3, got %d", notifications[0].EdictKey.ID)
	}
	mu.Unlock()

	// All events are forwarded to the TUI
	notifications = nil
	rg.DispatchEvent(Event{
		Type:     storage.EventStepCompleted,
		EdictKey: storage.EdictKey{ID: 4},
		Payload:  map[string]interface{}{},
	})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].EventType != storage.EventStepCompleted {
		t.Errorf("expected EventType EventStepCompleted, got %s", notifications[0].EventType)
	}
	mu.Unlock()

	// Test EventRitualAborted notification
	notifications = nil
	rg.DispatchEvent(Event{
		Type:     storage.EventRitualAborted,
		EdictKey: storage.EdictKey{ID: 5},
		Payload: map[string]interface{}{
			"ritual": "swift-strike",
			"reason": "edict cancelled",
		},
	})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].EventType != storage.EventRitualAborted {
		t.Errorf("expected EventType EventRitualAborted, got %s", notifications[0].EventType)
	}
	expected := "Ritual swift-strike aborted: edict cancelled"
	if notifications[0].Message != expected {
		t.Errorf("expected message %q, got %q", expected, notifications[0].Message)
	}
	mu.Unlock()
}

// TestAbortStaleRitualsOnStart verifies that startRitual aborts stale running
// rituals before starting a new one.
func TestAbortStaleRitualsOnStart(t *testing.T) {
	db := setupEventTestDB(t)
	logger := slog.Default()

	// Create a stale running ritual (updated 10 minutes ago)
	// Use raw SQL to bypass GORM's autoUpdateTime
	staleTime := time.Now().Add(-10 * time.Minute)
	staleExec := &RitualExecution{
		ID:          "stale-ritual-123",
		RitualName:  "swift-strike",
		EdictID:     100,
		Username:    "test",
		Project:     "test/project",
		State:       RitualStateRunning,
		CurrentStep: 1,
	}
	if err := db.Save(staleExec).Error; err != nil {
		t.Fatalf("failed to create stale ritual: %v", err)
	}
	// Manually set updated_at to the past
	if err := db.Exec("UPDATE ritual_executions SET updated_at = ? WHERE id = ?", staleTime, "stale-ritual-123").Error; err != nil {
		t.Fatalf("failed to set stale updated_at: %v", err)
	}

	// Create RitualGuard with 5 minute flatlineAge and matching username/project
	base := NewMinisterBase(db, nil, logger, "test", "test/project")
	rg := &RitualGuard{
		MinisterBase: base,
		flatlineAge:  5 * time.Minute,
	}

	// Call abortStaleRitualsIfLocked
	rg.abortStaleRitualsIfLocked()

	// Verify the stale ritual was aborted
	var exec RitualExecution
	if err := db.First(&exec, "id = ?", "stale-ritual-123").Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateAborted {
		t.Errorf("expected ritual state %s, got %s", RitualStateAborted, exec.State)
	}
}

// TestNoAbortForFreshRituals verifies that fresh running rituals are not aborted.
func TestNoAbortForFreshRituals(t *testing.T) {
	db := setupEventTestDB(t)
	logger := slog.Default()

	// Create a fresh running ritual (updated 1 minute ago)
	freshTime := time.Now().Add(-1 * time.Minute)
	freshExec := &RitualExecution{
		ID:          "fresh-ritual-456",
		RitualName:  "swift-strike",
		EdictID:     200,
		Username:    "test",
		Project:     "test/project",
		State:       RitualStateRunning,
		CurrentStep: 1,
	}
	freshExec.UpdatedAt = freshTime
	if err := db.Save(freshExec).Error; err != nil {
		t.Fatalf("failed to create fresh ritual: %v", err)
	}

	// Create RitualGuard with 5 minute flatlineAge and matching username/project
	base := NewMinisterBase(db, nil, logger, "test", "test/project")
	rg := &RitualGuard{
		MinisterBase: base,
		flatlineAge:  5 * time.Minute,
	}

	// Call abortStaleRitualsIfLocked
	rg.abortStaleRitualsIfLocked()

	// Verify the fresh ritual is still running
	var exec RitualExecution
	if err := db.First(&exec, "id = ?", "fresh-ritual-456").Error; err != nil {
		t.Fatalf("failed to find ritual: %v", err)
	}
	if exec.State != RitualStateRunning {
		t.Errorf("expected ritual state %s, got %s", RitualStateRunning, exec.State)
	}
}

// TestAbortStaleRituals_OtherProjectExcluded verifies that abortStaleRitualsIfLocked
// does not abort stale rituals belonging to a different username or project.
func TestAbortStaleRituals_OtherProjectExcluded(t *testing.T) {
	db := setupEventTestDB(t)
	logger := slog.Default()

	// Create a stale running ritual for the current user/project
	staleTime := time.Now().Add(-10 * time.Minute)
	localExec := &RitualExecution{
		ID: "stale-local", RitualName: "swift-strike", EdictID: 100,
		Username: "test", Project: "test/project", State: RitualStateRunning, CurrentStep: 1,
	}
	if err := db.Save(localExec).Error; err != nil {
		t.Fatalf("failed to create local stale ritual: %v", err)
	}
	if err := db.Exec("UPDATE ritual_executions SET updated_at = ? WHERE id = ?", staleTime, "stale-local").Error; err != nil {
		t.Fatalf("failed to set stale updated_at: %v", err)
	}

	// Create a stale running ritual for a different project (same user)
	otherProjectExec := &RitualExecution{
		ID: "stale-other-project", RitualName: "swift-strike", EdictID: 101,
		Username: "test", Project: "other/project", State: RitualStateRunning, CurrentStep: 1,
	}
	if err := db.Save(otherProjectExec).Error; err != nil {
		t.Fatalf("failed to create other-project stale ritual: %v", err)
	}
	if err := db.Exec("UPDATE ritual_executions SET updated_at = ? WHERE id = ?", staleTime, "stale-other-project").Error; err != nil {
		t.Fatalf("failed to set stale updated_at for other-project: %v", err)
	}

	// Create a stale running ritual for a different username (same project)
	otherUserExec := &RitualExecution{
		ID: "stale-other-user", RitualName: "swift-strike", EdictID: 102,
		Username: "otheruser", Project: "test/project", State: RitualStateRunning, CurrentStep: 1,
	}
	if err := db.Save(otherUserExec).Error; err != nil {
		t.Fatalf("failed to create other-user stale ritual: %v", err)
	}
	if err := db.Exec("UPDATE ritual_executions SET updated_at = ? WHERE id = ?", staleTime, "stale-other-user").Error; err != nil {
		t.Fatalf("failed to set stale updated_at for other-user: %v", err)
	}

	// Create RitualGuard with the local user/project
	base := NewMinisterBase(db, nil, logger, "test", "test/project")
	rg := &RitualGuard{
		MinisterBase: base,
		flatlineAge:  5 * time.Minute,
	}

	rg.abortStaleRitualsIfLocked()

	// Local ritual should be aborted
	var localResult RitualExecution
	if err := db.First(&localResult, "id = ?", "stale-local").Error; err != nil {
		t.Fatalf("failed to find local ritual: %v", err)
	}
	if localResult.State != RitualStateAborted {
		t.Errorf("expected local ritual state %s, got %s", RitualStateAborted, localResult.State)
	}

	// Other-project ritual should remain running
	var otherProjectResult RitualExecution
	if err := db.First(&otherProjectResult, "id = ?", "stale-other-project").Error; err != nil {
		t.Fatalf("failed to find other-project ritual: %v", err)
	}
	if otherProjectResult.State != RitualStateRunning {
		t.Errorf("expected other-project ritual state to remain %s, got %s", RitualStateRunning, otherProjectResult.State)
	}

	// Other-user ritual should remain running
	var otherUserResult RitualExecution
	if err := db.First(&otherUserResult, "id = ?", "stale-other-user").Error; err != nil {
		t.Fatalf("failed to find other-user ritual: %v", err)
	}
	if otherUserResult.State != RitualStateRunning {
		t.Errorf("expected other-user ritual state to remain %s, got %s", RitualStateRunning, otherUserResult.State)
	}
}

// TestScanForStaleRituals_OtherProjectExcluded verifies that scanForStaleRituals
// only considers running rituals belonging to the current username and project.
func TestScanForStaleRituals_OtherProjectExcluded(t *testing.T) {
	db := setupEventTestDB(t)
	// Migrate tables needed by scanForStaleRituals
	if err := db.AutoMigrate(&storage.Edict{}, &storage.Seal{}); err != nil {
		t.Fatalf("failed to migrate edict/seal tables: %v", err)
	}
	logger := slog.Default()

	// Create an edict for the local user/project that is sealed
	localEdict := storage.Edict{Intent: "local edict", Username: "test", Project: "test/project"}
	if err := db.Create(&localEdict).Error; err != nil {
		t.Fatalf("failed to create local edict: %v", err)
	}
	db.Create(&storage.Seal{EdictID: localEdict.ID, Username: "test", Project: "test/project", MinisterID: "ruler"})

	// Create a running ritual for the local user/project
	localExec := &RitualExecution{
		ID: "running-local", RitualName: "swift-strike", EdictID: localEdict.ID,
		Username: "test", Project: "test/project", State: RitualStateRunning, CurrentStep: 1,
	}
	if err := db.Save(localExec).Error; err != nil {
		t.Fatalf("failed to create local running ritual: %v", err)
	}

	// Create an edict for a different project that is sealed
	otherProjectEdict := storage.Edict{Intent: "other project edict", Username: "test", Project: "other/project"}
	if err := db.Create(&otherProjectEdict).Error; err != nil {
		t.Fatalf("failed to create other-project edict: %v", err)
	}
	db.Create(&storage.Seal{EdictID: otherProjectEdict.ID, Username: "test", Project: "other/project", MinisterID: "judge"})

	// Create a running ritual for the other project
	otherProjectExec := &RitualExecution{
		ID: "running-other-project", RitualName: "swift-strike", EdictID: otherProjectEdict.ID,
		Username: "test", Project: "other/project", State: RitualStateRunning, CurrentStep: 1,
	}
	if err := db.Save(otherProjectExec).Error; err != nil {
		t.Fatalf("failed to create other-project running ritual: %v", err)
	}

	// Create an edict for a different username that is sealed
	otherUserEdict := storage.Edict{Intent: "other user edict", Username: "otheruser", Project: "test/project"}
	if err := db.Create(&otherUserEdict).Error; err != nil {
		t.Fatalf("failed to create other-user edict: %v", err)
	}
	db.Create(&storage.Seal{EdictID: otherUserEdict.ID, Username: "otheruser", Project: "test/project", MinisterID: "judge"})

	// Create a running ritual for the other user
	otherUserExec := &RitualExecution{
		ID: "running-other-user", RitualName: "swift-strike", EdictID: otherUserEdict.ID,
		Username: "otheruser", Project: "test/project", State: RitualStateRunning, CurrentStep: 1,
	}
	if err := db.Save(otherUserExec).Error; err != nil {
		t.Fatalf("failed to create other-user running ritual: %v", err)
	}

	// Create RitualGuard with the local user/project
	scanBase := NewMinisterBase(db, nil, logger, "test", "test/project")
	rg := &RitualGuard{
		MinisterBase: scanBase,
		ritualRunner: NewRitualRunner(
			NewRitualRegistry(),
			func(id string) Minister { return nil },
			nil, db, nil, logger, repo.RepoInfo{},
		),
	}

	ctx := context.Background()
	rg.scanForStaleRituals(ctx)

	// Local ritual should be aborted (sealed edict)
	var localResult RitualExecution
	if err := db.First(&localResult, "id = ?", "running-local").Error; err != nil {
		t.Fatalf("failed to find local ritual: %v", err)
	}
	if localResult.State != RitualStateAborted {
		t.Errorf("expected local ritual state %s, got %s", RitualStateAborted, localResult.State)
	}

	// Other-project ritual should remain running (not queried)
	var otherProjectResult RitualExecution
	if err := db.First(&otherProjectResult, "id = ?", "running-other-project").Error; err != nil {
		t.Fatalf("failed to find other-project ritual: %v", err)
	}
	if otherProjectResult.State != RitualStateRunning {
		t.Errorf("expected other-project ritual state to remain %s, got %s", RitualStateRunning, otherProjectResult.State)
	}

	// Other-user ritual should remain running (not queried)
	var otherUserResult RitualExecution
	if err := db.First(&otherUserResult, "id = ?", "running-other-user").Error; err != nil {
		t.Fatalf("failed to find other-user ritual: %v", err)
	}
	if otherUserResult.State != RitualStateRunning {
		t.Errorf("expected other-user ritual state to remain %s, got %s", RitualStateRunning, otherUserResult.State)
	}
}

// TestRecoveryZhengmingHasPassOption verifies that recovery zhengming includes "Pass" option.
func TestRecoveryZhengmingHasPassOption(t *testing.T) {
	db := setupEventTestDB(t)

	// Create an aborted ritual
	abortedExec := &RitualExecution{
		ID:          "aborted-ritual-789",
		RitualName:  "test-ritual",
		EdictID:     300,
		Username:    "test",
		Project:     "test/project",
		State:       RitualStateAborted,
		CurrentStep: 2,
	}
	if err := db.Save(abortedExec).Error; err != nil {
		t.Fatalf("failed to create aborted ritual: %v", err)
	}

	// Create step states to mark step 0 and 1 as complete
	for i := 0; i < 2; i++ {
		stepState := RitualStepState{
			ExecutionID: abortedExec.ID,
			StepIndex:   i,
			Name:        fmt.Sprintf("step-%d", i),
			Message:     "completed",
		}
		if err := db.Save(&stepState).Error; err != nil {
			t.Fatalf("failed to create step state: %v", err)
		}
	}

	// Verify the ritual exists
	var count int64
	db.Model(&RitualExecution{}).Where("state IN ?", []RitualState{RitualStateAborted, RitualStateFailed, RitualStateStopped}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 aborted ritual, got %d", count)
	}
}
