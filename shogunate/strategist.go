package shogunate

import (
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// strategistConn implements StrategistConn - decomposes edicts into ling
type strategistConn struct {
	baseConn
}

// NewStrategistConn creates a new Strategist connection
func NewStrategistConn(db *gorm.DB) StrategistConn {
	return &strategistConn{
		baseConn: baseConn{db: db, ministerID: "strategist"},
	}
}

// GetEdict retrieves an edict by ID
func (c *strategistConn) GetEdict(edictID string) (*storage.Edict, error) {
	return c.getEdict(edictID)
}

// InsertLing creates a new task order for an edict
func (c *strategistConn) InsertLing(ling *storage.Ling) error {
	// Generate idempotency key if not set
	if ling.IdempotencyKey == "" {
		var edict storage.Edict
		if err := c.db.First(&edict, "edict_id = ?", ling.EdictID).Error; err != nil {
			return fmt.Errorf("failed to get edict for idempotency key: %w", err)
		}
		ling.IdempotencyKey = generateIdempotencyKey(
			ling.EdictID,
			fmt.Sprintf("%d", edict.RenIntentVersion),
			ling.Description,
		)
	}

	// Generate ling ID if not set
	if ling.LingID == "" {
		ling.LingID = generateID("ling", ling.EdictID, ling.Description)
	}

	if err := c.db.Create(ling).Error; err != nil {
		return fmt.Errorf("failed to insert ling: %w", err)
	}
	return nil
}

// GetLingForEdict retrieves all ling for an edict
func (c *strategistConn) GetLingForEdict(edictID string) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := c.db.Where("edict_id = ?", edictID).
		Order("created_at ASC").
		Find(&ling).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get ling: %w", err)
	}
	return ling, nil
}

// LingExistsForEdict checks if any ling exists for an edict
func (c *strategistConn) LingExistsForEdict(edictID string) (bool, error) {
	var count int64
	err := c.db.Model(&storage.Ling{}).
		Where("edict_id = ?", edictID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check ling existence: %w", err)
	}
	return count > 0, nil
}
