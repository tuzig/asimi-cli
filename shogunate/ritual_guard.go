package shogunate

import (
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// ritualGuardConn implements RitualGuardConn - event processing
type ritualGuardConn struct {
	db *gorm.DB
}

// NewRitualGuardConn creates a new Ritual Guard connection
func NewRitualGuardConn(db *gorm.DB) RitualGuardConn {
	return &ritualGuardConn{db: db}
}

// GetEventsFrom retrieves events starting from a given event ID
func (c *ritualGuardConn) GetEventsFrom(fromEventID int64, limit int) ([]storage.TianEvent, error) {
	var events []storage.TianEvent
	query := c.db.Where("event_id > ?", fromEventID).
		Order("event_id ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	return events, nil
}

// AcknowledgeEvent marks an event as processed (no-op for SQLite, used for checkpointing)
func (c *ritualGuardConn) AcknowledgeEvent(eventID int64) error {
	// For SQLite, we just track via checkpoint
	// For PostgreSQL, this could update a processed flag or commit offset
	return nil
}

// GetLastAcknowledgedEvent returns the last processed event ID
func (c *ritualGuardConn) GetLastAcknowledgedEvent() (int64, error) {
	return c.LoadCheckpoint()
}

// SaveCheckpoint persists the last processed event ID for crash recovery
func (c *ritualGuardConn) SaveCheckpoint(eventID int64) error {
	// Use a simple key-value approach in ruler_council table (repurposed)
	// In production, this would be a dedicated checkpoint table
	result := c.db.Exec(`
		INSERT OR REPLACE INTO ritual_guard_checkpoint (id, event_id, updated_at)
		VALUES (1, ?, datetime('now'))
	`, eventID)
	if result.Error != nil {
		return fmt.Errorf("failed to save checkpoint: %w", result.Error)
	}
	return nil
}

// LoadCheckpoint retrieves the last processed event ID
func (c *ritualGuardConn) LoadCheckpoint() (int64, error) {
	var eventID int64
	err := c.db.Raw(`SELECT COALESCE(event_id, 0) FROM ritual_guard_checkpoint WHERE id = 1`).
		Scan(&eventID).Error
	if err != nil {
		// Table might not exist or be empty, return 0
		return 0, nil
	}
	return eventID, nil
}

// MoveToDLQ moves a failed event to the dead letter queue
func (c *ritualGuardConn) MoveToDLQ(event storage.TianEvent, errMsg string, retryCount int) error {
	dlqEntry := storage.TianEventDLQ{
		EventID:           event.EventID,
		EdictID:           event.EdictID,
		EventType:         event.EventType,
		Payload:           event.Payload,
		ErrorMessage:      errMsg,
		RetryCount:        retryCount,
		OriginalCreatedAt: event.CreatedAt,
	}

	if err := c.db.Create(&dlqEntry).Error; err != nil {
		return fmt.Errorf("failed to move to DLQ: %w", err)
	}
	return nil
}
