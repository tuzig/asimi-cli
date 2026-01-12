package shogunate

import (
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// judgeConn implements JudgeConn - judges code through CI
type judgeConn struct {
	baseConn
}

// NewJudgeConn creates a new Judge connection
func NewJudgeConn(db *gorm.DB) JudgeConn {
	return &judgeConn{
		baseConn: baseConn{db: db, ministerID: "judge"},
	}
}

// GetPendingManifests retrieves all pending manifests for an edict
func (c *judgeConn) GetPendingManifests(edictID string) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := c.db.Where("edict_id = ? AND status = ?", edictID, storage.ManifestPending).
		Order("created_at ASC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending manifests: %w", err)
	}
	return manifests, nil
}

// AllManifestsQuenched checks if all manifests for an edict are quenched
func (c *judgeConn) AllManifestsQuenched(edictID string) (bool, error) {
	var pendingCount int64
	err := c.db.Model(&storage.ForgeManifest{}).
		Where("edict_id = ? AND status != ?", edictID, storage.ManifestQuenched).
		Count(&pendingCount).Error
	if err != nil {
		return false, fmt.Errorf("failed to check quenched status: %w", err)
	}

	// Also check that at least one manifest exists
	var totalCount int64
	err = c.db.Model(&storage.ForgeManifest{}).
		Where("edict_id = ?", edictID).
		Count(&totalCount).Error
	if err != nil {
		return false, fmt.Errorf("failed to count manifests: %w", err)
	}

	return totalCount > 0 && pendingCount == 0, nil
}

// InsertVerdict records a CI judgment for a manifest
func (c *judgeConn) InsertVerdict(manifestID, testSuite string, outcome storage.VerdictOutcome, evidence storage.JSON) (string, error) {
	verdictID := generateID("verdict", manifestID, testSuite)

	// Get manifest for idempotency key
	var manifest storage.ForgeManifest
	if err := c.db.First(&manifest, "manifest_id = ?", manifestID).Error; err != nil {
		return "", fmt.Errorf("failed to get manifest: %w", err)
	}

	idempotencyKey := generateIdempotencyKey(manifestID, testSuite, manifest.CommitHash)

	verdict := storage.JudgeVerdict{
		VerdictID:      verdictID,
		ManifestID:     manifestID,
		TestSuite:      testSuite,
		Outcome:        outcome,
		Evidence:       evidence,
		IdempotencyKey: idempotencyKey,
	}

	if err := c.db.Create(&verdict).Error; err != nil {
		return "", fmt.Errorf("failed to insert verdict: %w", err)
	}
	return verdictID, nil
}

// UpdateManifestStatus updates a manifest's status after judgment
func (c *judgeConn) UpdateManifestStatus(manifestID string, status storage.ManifestStatus, verdictID string) error {
	result := c.db.Model(&storage.ForgeManifest{}).
		Where("manifest_id = ?", manifestID).
		Updates(map[string]interface{}{
			"status":     status,
			"verdict_id": verdictID,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to update manifest status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("manifest not found: %s", manifestID)
	}
	return nil
}
