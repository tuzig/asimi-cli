package shogunate

import (
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// forgeConn implements ForgeConn - creates code manifests
type forgeConn struct {
	baseConn
}

// NewForgeConn creates a new Forge connection
func NewForgeConn(db *gorm.DB) ForgeConn {
	return &forgeConn{
		baseConn: baseConn{db: db, ministerID: "forge"},
	}
}

// GetEdict retrieves an edict by ID
func (c *forgeConn) GetEdict(edictID string) (*storage.Edict, error) {
	return c.getEdict(edictID)
}

// GetPendingLing retrieves all pending ling for an edict
func (c *forgeConn) GetPendingLing(edictID string) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := c.db.Where("edict_id = ? AND status = ?", edictID, storage.LingPending).
		Order("created_at ASC").
		Find(&ling).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending ling: %w", err)
	}
	return ling, nil
}

// MarkLingCompleted marks a ling as completed
func (c *forgeConn) MarkLingCompleted(lingID string) error {
	result := c.db.Model(&storage.Ling{}).
		Where("ling_id = ?", lingID).
		Update("status", storage.LingCompleted)
	if result.Error != nil {
		return fmt.Errorf("failed to mark ling completed: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("ling not found: %s", lingID)
	}
	return nil
}

// StageManifest creates a staged manifest (not yet committed to git)
func (c *forgeConn) StageManifest(edictID, lingID, filePath, qualifiedName, patchHash string) (string, error) {
	manifestID := generateID("manifest", edictID, lingID, filePath)
	idempotencyKey := generateIdempotencyKey(edictID, lingID, filePath, patchHash)

	manifest := storage.ForgeManifest{
		ManifestID:     manifestID,
		EdictID:        edictID,
		LingID:         lingID,
		FilePath:       filePath,
		QualifiedName:  qualifiedName,
		Status:         storage.ManifestStaging,
		IdempotencyKey: idempotencyKey,
	}

	if err := c.db.Create(&manifest).Error; err != nil {
		return "", fmt.Errorf("failed to stage manifest: %w", err)
	}
	return manifestID, nil
}

// ActivateManifest transitions a staged manifest to pending after git commit
func (c *forgeConn) ActivateManifest(manifestID, commitHash string) error {
	result := c.db.Model(&storage.ForgeManifest{}).
		Where("manifest_id = ? AND status = ?", manifestID, storage.ManifestStaging).
		Updates(map[string]interface{}{
			"commit_hash": commitHash,
			"status":      storage.ManifestPending,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to activate manifest: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("staged manifest not found: %s", manifestID)
	}
	return nil
}

// DeleteStagedManifest removes a staged manifest (git commit failed)
func (c *forgeConn) DeleteStagedManifest(manifestID string) error {
	result := c.db.Where("manifest_id = ? AND status = ?", manifestID, storage.ManifestStaging).
		Delete(&storage.ForgeManifest{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete staged manifest: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("staged manifest not found: %s", manifestID)
	}
	return nil
}

// GetRejectedManifests retrieves all rejected manifests for an edict
func (c *forgeConn) GetRejectedManifests(edictID string) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := c.db.Where("edict_id = ? AND status = ?", edictID, storage.ManifestRejected).
		Order("created_at DESC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get rejected manifests: %w", err)
	}
	return manifests, nil
}
