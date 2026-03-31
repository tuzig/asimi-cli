package shogunate

import (
	"context"
	"database/sql"
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
	s.PublishEvent(storage.EdictKey{EdictID: 1}, storage.EventEdictCreated, storage.JSON{"foo": "bar"})

	// Wait briefly for the event to be dispatched
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(received) != 1 {
		t.Fatalf("expected 1 dispatched event, got %d", len(received))
	}
	if received[0].EdictKey.EdictID != 1 {
		t.Errorf("expected EdictID 1, got %d", received[0].EdictKey.EdictID)
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
	s.PublishEvent(storage.EdictKey{EdictID: 2}, "edict_created", storage.JSON{"key": "val"})

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
		if ev.EdictKey.EdictID != 2 {
			t.Errorf("channel event EdictID: expected 2, got %d", ev.EdictKey.EdictID)
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
	s.PublishEvent(storage.EdictKey{EdictID: 1}, "edict_created", storage.JSON{})
	s.PublishEvent(storage.EdictKey{EdictID: 2}, "edict_created", storage.JSON{})

	// This one should overflow — persists to DB but doesn't block
	s.PublishEvent(storage.EdictKey{EdictID: 3}, "edict_created", storage.JSON{})

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

	err := base.EmitEvent(storage.EdictKey{EdictID: 10}, "edict_assigned", storage.JSON{"from": "minister"})
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
		if ev.EdictKey.EdictID != 10 {
			t.Errorf("expected EdictID 10, got %d", ev.EdictKey.EdictID)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestMinisterBaseEmitEvent_Fallback(t *testing.T) {
	db := setupEventTestDB(t)

	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	// No publish set — should fall back to DB-only

	err := base.EmitEvent(storage.EdictKey{EdictID: 20}, "edict_created", storage.JSON{"from": "fallback"})
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
		EdictKey: storage.EdictKey{EdictID: 1},
		Payload:  map[string]interface{}{"intent": "Add new feature"},
	})

	time.Sleep(10 * time.Millisecond) // Allow async processing

	mu.Lock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].EdictKey.EdictID != 1 {
		t.Errorf("expected EdictID 1, got %d", notifications[0].EdictKey.EdictID)
	}
	if notifications[0].EventType != storage.EventEdictCreated {
		t.Errorf("expected EventType EventEdictCreated, got %s", notifications[0].EventType)
	}
	if notifications[0].Message != "Edict 1 created: Add new feature" {
		t.Errorf("unexpected message: %s", notifications[0].Message)
	}
	mu.Unlock()

	// Test EventSealGranted notification
	notifications = nil
	rg.DispatchEvent(Event{
		Type:     storage.EventSealGranted,
		EdictKey: storage.EdictKey{EdictID: 2},
		Payload:  map[string]interface{}{"minister_id": "judge"},
	})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].Message != "Minister judge sealed edict 2" {
		t.Errorf("unexpected message: %s", notifications[0].Message)
	}
	mu.Unlock()

	// Test EventEdictSealed notification
	notifications = nil
	rg.DispatchEvent(Event{
		Type:     storage.EventEdictSealed,
		EdictKey: storage.EdictKey{EdictID: 3},
		Payload:  map[string]interface{}{},
	})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].Message != "Edict 3 sealed and ascended to Heaven" {
		t.Errorf("unexpected message: %s", notifications[0].Message)
	}
	mu.Unlock()

	// Test that non-notifiable events don't trigger notifications
	notifications = nil
	rg.DispatchEvent(Event{
		Type:     storage.EventStepCompleted,
		EdictKey: storage.EdictKey{EdictID: 4},
		Payload:  map[string]interface{}{},
	})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(notifications) != 0 {
		t.Fatalf("expected 0 notifications for non-notifiable event, got %d", len(notifications))
	}
	mu.Unlock()
}

// TestRitualGuard_BuildEventNotification tests the message building for different event types
func TestRitualGuard_BuildEventNotification(t *testing.T) {
	db := setupEventTestDB(t)
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject")
	rg := NewRitualGuard(RitualGuardOpts{Base: base})

	tests := []struct {
		name        string
		event       Event
		expectMsg   string
		expectTabID string
	}{
		{
			name: "edict_created",
			event: Event{
				Type:     storage.EventEdictCreated,
				EdictKey: storage.EdictKey{EdictID: 1},
				Payload:  map[string]interface{}{"intent": "Test intent"},
			},
			expectMsg:   "Edict 1 created: Test intent",
			expectTabID: "chancellor",
		},
		{
			name: "edict_sealed",
			event: Event{
				Type:     storage.EventEdictSealed,
				EdictKey: storage.EdictKey{EdictID: 2},
				Payload:  map[string]interface{}{},
			},
			expectMsg:   "Edict 2 sealed and ascended to Heaven",
			expectTabID: "chancellor",
		},
		{
			name: "seal_granted",
			event: Event{
				Type:     storage.EventSealGranted,
				EdictKey: storage.EdictKey{EdictID: 3},
				Payload:  map[string]interface{}{"minister_id": "sage"},
			},
			expectMsg:   "Minister sage sealed edict 3",
			expectTabID: "chancellor",
		},
		{
			name: "zhengming_needed",
			event: Event{
				Type:     storage.EventZhengmingNeeded,
				EdictKey: storage.EdictKey{EdictID: 4},
				Payload:  map[string]interface{}{"summary": "Need clarification"},
			},
			expectMsg:   "Zhengming requested for edict 4: Need clarification",
			expectTabID: "chancellor",
		},
		{
			name: "zhengming_answered",
			event: Event{
				Type:     storage.EventZhengmingAnswered,
				EdictKey: storage.EdictKey{EdictID: 5},
				Payload:  map[string]interface{}{},
			},
			expectMsg:   "Zhengming answered for edict 5",
			expectTabID: "chancellor",
		},
		{
			name: "edict_cancelled",
			event: Event{
				Type:     storage.EventEdictCancelled,
				EdictKey: storage.EdictKey{EdictID: 6},
				Payload:  map[string]interface{}{},
			},
			expectMsg:   "Edict 6 cancelled",
			expectTabID: "chancellor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := rg.buildEventNotification(tt.event)
			if msg.Message != tt.expectMsg {
				t.Errorf("expected message %q, got %q", tt.expectMsg, msg.Message)
			}
			if msg.TabID != tt.expectTabID {
				t.Errorf("expected TabID %q, got %q", tt.expectTabID, msg.TabID)
			}
			if msg.EdictKey.EdictID != tt.event.EdictKey.EdictID {
				t.Errorf("expected EdictID %d, got %d", tt.event.EdictKey.EdictID, msg.EdictKey.EdictID)
			}
			if msg.EventType != tt.event.Type {
				t.Errorf("expected EventType %s, got %s", tt.event.Type, msg.EventType)
			}
		})
	}
}
