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
		db:            db,
		logger:        slog.Default(),
		ministers:     make(map[string]Minister),
		eventRegistry: NewEventRegistry(),
		eventCh:       make(chan Event, 256),
	}
	base := NewMinisterBase(db, nil, slog.Default())
	rg := NewRitualGuard(base, nil, s)
	s.ritualGuard = rg
	return s
}

func TestChannelDelivery(t *testing.T) {
	db := setupEventTestDB(t)
	s := newTestShogunate(t, db)

	// Subscribe to the event
	var received []Event
	var mu sync.Mutex
	s.eventRegistry.Subscribe(storage.EventEdictCreated, func(e Event) {
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
	s.PublishEvent("edict-1", storage.EventEdictCreated, storage.JSON{"foo": "bar"})

	// Wait briefly for the event to be dispatched
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(received) != 1 {
		t.Fatalf("expected 1 dispatched event, got %d", len(received))
	}
	if received[0].EdictID != "edict-1" {
		t.Errorf("expected EdictID 'edict-1', got %q", received[0].EdictID)
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
	s.PublishEvent("edict-2", "edict_created", storage.JSON{"key": "val"})

	// Verify it's in the database
	var events []storage.TianEvent
	if err := db.Find(&events).Error; err != nil {
		t.Fatalf("failed to query events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 DB event, got %d", len(events))
	}
	if events[0].EdictID != "edict-2" {
		t.Errorf("expected EdictID 'edict-2', got %q", events[0].EdictID)
	}
	if events[0].EventType != "edict_created" {
		t.Errorf("expected EventType 'edict_created', got %q", events[0].EventType)
	}

	// Also verify it arrived on the channel
	select {
	case ev := <-s.eventCh:
		if ev.EdictID != "edict-2" {
			t.Errorf("channel event EdictID: expected 'edict-2', got %q", ev.EdictID)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestBackpressure(t *testing.T) {
	db := setupEventTestDB(t)
	// Use a tiny channel to force backpressure
	s := &Shogunate{
		db:            db,
		logger:        slog.Default(),
		ministers:     make(map[string]Minister),
		eventRegistry: NewEventRegistry(),
		eventCh:       make(chan Event, 2),
	}

	// Fill the channel
	s.PublishEvent("e1", "edict_created", storage.JSON{})
	s.PublishEvent("e2", "edict_created", storage.JSON{})

	// This one should overflow — persists to DB but doesn't block
	s.PublishEvent("e3", "edict_created", storage.JSON{})

	// Verify all 3 persisted to DB
	var count int64
	db.Model(&storage.TianEvent{}).Count(&count)
	if count != 3 {
		t.Errorf("expected 3 DB events, got %d", count)
	}

	// Verify channel has exactly 2 (capacity)
	if len(s.eventCh) != 2 {
		t.Errorf("expected 2 events in channel, got %d", len(s.eventCh))
	}
}

func TestDrainUnprocessedEvents(t *testing.T) {
	db := setupEventTestDB(t)
	s := newTestShogunate(t, db)

	// Create a ritual guard for checkpoint methods
	base := NewMinisterBase(db, nil, slog.Default())
	rg := NewRitualGuard(base, nil, s)
	s.ritualGuard = rg

	// Insert 5 events directly into DB (simulating crash scenario)
	for i := 0; i < 5; i++ {
		db.Create(&storage.TianEvent{
			EdictID:   "drain-edict",
			EventType: "step_completed",
			Payload:   storage.JSON{"i": i},
		})
	}

	// Set checkpoint to event ID 2 (simulating partial processing before crash)
	rg.SaveCheckpoint(2)

	// Track dispatched events
	var dispatched []Event
	var mu sync.Mutex
	s.eventRegistry.Subscribe("step_completed", func(e Event) {
		mu.Lock()
		dispatched = append(dispatched, e)
		mu.Unlock()
	})

	// Drain should replay events 3, 4, 5
	s.drainUnprocessedEvents()

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

	base := NewMinisterBase(db, nil, slog.Default())
	base.publish = s.PublishEvent

	err := base.EmitEvent("edict-pub", "edict_assigned", storage.JSON{"from": "minister"})
	if err != nil {
		t.Fatalf("EmitEvent error: %v", err)
	}

	// Verify DB
	var count int64
	db.Model(&storage.TianEvent{}).Where("edict_id = ?", "edict-pub").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 DB event, got %d", count)
	}

	// Verify channel
	select {
	case ev := <-s.eventCh:
		if ev.EdictID != "edict-pub" {
			t.Errorf("expected EdictID 'edict-pub', got %q", ev.EdictID)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestMinisterBaseEmitEvent_Fallback(t *testing.T) {
	db := setupEventTestDB(t)

	base := NewMinisterBase(db, nil, slog.Default())
	// No publish set — should fall back to DB-only

	err := base.EmitEvent("edict-fb", "edict_created", storage.JSON{"from": "fallback"})
	if err != nil {
		t.Fatalf("EmitEvent error: %v", err)
	}

	var count int64
	db.Model(&storage.TianEvent{}).Where("edict_id = ?", "edict-fb").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 DB event, got %d", count)
	}
}
