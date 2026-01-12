package shogunate

import (
	"fmt"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// chancellorConn implements ChancellorConn - full access to all domains
type chancellorConn struct {
	baseConn
}

// NewChancellorConn creates a new Chancellor connection
func NewChancellorConn(db *gorm.DB) ChancellorConn {
	return &chancellorConn{
		baseConn: baseConn{db: db, ministerID: "chancellor"},
	}
}

// GetEdict retrieves an edict by ID
func (c *chancellorConn) GetEdict(edictID string) (*storage.Edict, error) {
	return c.getEdict(edictID)
}

// CreateEdict creates a new edict from a GitHub issue
func (c *chancellorConn) CreateEdict(edictID, renIntent string) error {
	edict := storage.Edict{
		EdictID:      edictID,
		RenIntent:    renIntent,
		CurrentPhase: storage.PhasePlanning,
	}
	if err := c.db.Create(&edict).Error; err != nil {
		return fmt.Errorf("failed to create edict: %w", err)
	}
	return nil
}

// UpdatePhase transitions an edict to a new phase
func (c *chancellorConn) UpdatePhase(edictID string, phase storage.EdictPhase) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("current_phase", phase)
	if result.Error != nil {
		return fmt.Errorf("failed to update phase: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// SetChancellorSeal sets or clears the Chancellor's seal on an edict
func (c *chancellorConn) SetChancellorSeal(edictID string, sealed bool) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("chancellor_seal", sealed)
	if result.Error != nil {
		return fmt.Errorf("failed to set chancellor seal: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// SetCensorSeal sets or clears the Censor's seal on an edict
func (c *chancellorConn) SetCensorSeal(edictID string, sealed bool) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("censor_seal", sealed)
	if result.Error != nil {
		return fmt.Errorf("failed to set censor seal: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// CancelEdict marks an edict as cancelled
func (c *chancellorConn) CancelEdict(edictID, cancelledBy, reason string) error {
	now := time.Now()
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Updates(map[string]interface{}{
			"current_phase":       storage.PhaseCancelled,
			"cancelled_at":        &now,
			"cancelled_by":        cancelledBy,
			"cancellation_reason": reason,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to cancel edict: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// GetPendingZhengming retrieves all pending clarification requests for an edict
func (c *chancellorConn) GetPendingZhengming(edictID string) ([]storage.ZhengmingRequest, error) {
	var requests []storage.ZhengmingRequest
	err := c.db.Where("edict_id = ? AND status = ?", edictID, storage.ZhengmingPending).
		Order("created_at ASC").
		Find(&requests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending zhengming: %w", err)
	}
	return requests, nil
}

// AnswerZhengming marks a clarification request as answered
func (c *chancellorConn) AnswerZhengming(requestID, answer string) error {
	now := time.Now()
	result := c.db.Model(&storage.ZhengmingRequest{}).
		Where("request_id = ?", requestID).
		Updates(map[string]interface{}{
			"answer":      answer,
			"status":      storage.ZhengmingAnswered,
			"answered_at": &now,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to answer zhengming: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("zhengming request not found: %s", requestID)
	}
	return nil
}

// AppendToRenIntent appends clarification to the edict's intent and increments version
func (c *chancellorConn) AppendToRenIntent(edictID, clarification string) error {
	var edict storage.Edict
	if err := c.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		return fmt.Errorf("failed to get edict: %w", err)
	}

	newIntent := edict.RenIntent + "\n\n---\n**Clarification (v" +
		fmt.Sprintf("%d", edict.RenIntentVersion+1) + "):**\n" + clarification

	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Updates(map[string]interface{}{
			"ren_intent":         newIntent,
			"ren_intent_version": edict.RenIntentVersion + 1,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to append to ren intent: %w", result.Error)
	}
	return nil
}

// GetAllManifestsForEdict retrieves all manifests for an edict (Chancellor privilege)
func (c *chancellorConn) GetAllManifestsForEdict(edictID string) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := c.db.Where("edict_id = ?", edictID).
		Order("created_at ASC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get manifests: %w", err)
	}
	return manifests, nil
}

// GetAllLingForEdict retrieves all ling for an edict (Chancellor privilege)
func (c *chancellorConn) GetAllLingForEdict(edictID string) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := c.db.Where("edict_id = ?", edictID).
		Order("created_at ASC").
		Find(&ling).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get ling: %w", err)
	}
	return ling, nil
}

// ResetLingStatus resets a ling's status (for regression handling)
func (c *chancellorConn) ResetLingStatus(lingID string, status storage.LingStatus) error {
	result := c.db.Model(&storage.Ling{}).
		Where("ling_id = ?", lingID).
		Update("status", status)
	if result.Error != nil {
		return fmt.Errorf("failed to reset ling status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("ling not found: %s", lingID)
	}
	return nil
}
