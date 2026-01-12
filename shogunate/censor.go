package shogunate

import (
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// censorConn implements CensorConn - enforces code ethics
type censorConn struct {
	baseConn
}

// NewCensorConn creates a new Censor connection
func NewCensorConn(db *gorm.DB) CensorConn {
	return &censorConn{
		baseConn: baseConn{db: db, ministerID: "censor"},
	}
}

// GetQuenchedManifests retrieves all quenched manifests ready for ethics review
func (c *censorConn) GetQuenchedManifests(edictID string) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := c.db.Where("edict_id = ? AND status = ?", edictID, storage.ManifestQuenched).
		Order("created_at ASC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get quenched manifests: %w", err)
	}
	return manifests, nil
}

// NoRejections checks if there are any rejected manifests for an edict
func (c *censorConn) NoRejections(edictID string) (bool, error) {
	var count int64
	err := c.db.Model(&storage.ForgeManifest{}).
		Where("edict_id = ? AND status = ?", edictID, storage.ManifestRejected).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check rejections: %w", err)
	}
	return count == 0, nil
}

// LogPrecedent records an ethics decision for a manifest
func (c *censorConn) LogPrecedent(manifestID, principle string, ruling storage.PrecedentRuling, justification string) (string, error) {
	precedentID := generateID("precedent", manifestID, principle)

	// Get manifest for idempotency key
	var manifest storage.ForgeManifest
	if err := c.db.First(&manifest, "manifest_id = ?", manifestID).Error; err != nil {
		return "", fmt.Errorf("failed to get manifest: %w", err)
	}

	idempotencyKey := generateIdempotencyKey(manifestID, principle, manifest.CommitHash)

	precedent := storage.CensorPrecedent{
		PrecedentID:    precedentID,
		ManifestID:     manifestID,
		Principle:      principle,
		Ruling:         ruling,
		Justification:  justification,
		IdempotencyKey: idempotencyKey,
	}

	if err := c.db.Create(&precedent).Error; err != nil {
		return "", fmt.Errorf("failed to log precedent: %w", err)
	}
	return precedentID, nil
}

// RejectManifest marks a manifest as rejected by the Censor
func (c *censorConn) RejectManifest(manifestID string) error {
	result := c.db.Model(&storage.ForgeManifest{}).
		Where("manifest_id = ?", manifestID).
		Update("status", storage.ManifestRejected)
	if result.Error != nil {
		return fmt.Errorf("failed to reject manifest: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("manifest not found: %s", manifestID)
	}
	return nil
}

// GetPrecedentsForManifest retrieves all precedents for a specific manifest
func (c *censorConn) GetPrecedentsForManifest(manifestID string) ([]storage.CensorPrecedent, error) {
	var precedents []storage.CensorPrecedent
	err := c.db.Where("manifest_id = ?", manifestID).
		Order("created_at ASC").
		Find(&precedents).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get precedents: %w", err)
	}
	return precedents, nil
}

// QueryPrecedentsByPrinciple searches precedents by principle (for case law lookup)
func (c *censorConn) QueryPrecedentsByPrinciple(principle string, limit int) ([]storage.CensorPrecedent, error) {
	var precedents []storage.CensorPrecedent
	query := c.db.Where("principle LIKE ?", "%"+principle+"%").
		Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&precedents).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query precedents: %w", err)
	}
	return precedents, nil
}
