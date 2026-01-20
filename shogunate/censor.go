package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// Censor enforces code ethics and maintains precedent law
type Censor struct {
	MinisterBase // embedded base for database access and session creation
	linter       Linter
}

// NewCensor creates a new Censor minister
func NewCensor(db *gorm.DB, linter Linter, logger *slog.Logger) *Censor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Censor{
		MinisterBase: MinisterBase{db: db, ministerID: "censor", logger: logger},
		linter:       linter,
	}
}

// ID returns the minister identifier
func (c *Censor) ID() string {
	return "censor"
}

// Role returns the Censor's role identity text
func (c *Censor) Role() string {
	return `You are the Censor (都察院, Dūcháyuàn). Your domain is Dao (道, the Zen of Python) and institutional memory.

You preside over the censor_precedents table. You review code changes. You can reject a commit or grant a waiver with justification.

Your rulings are queryable precedent, not opinion. No merge passes without your seal.

CRITICAL RULES:
- NO GUESSING: If style rules are ambiguous, invoke Zhengming—do
- Log every violation as a precedent (reject or waive)
- Precedents are permanent and searchable
- Waivers require written justification`

}

// Tools returns the Censor's LLM tools for interactive sessions
func (c *Censor) Tools(notify NotifyFunc) []Tool {
	// TODO: Implement Censor tools (list_quenched_manifests, log_precedent, reject_manifest, query_precedents)
	return []Tool{}
}

// --- Database Methods ---

// GetQuenchedManifests retrieves all quenched manifests ready for ethics review
func (c *Censor) GetQuenchedManifests(edictID string) ([]storage.ForgeManifest, error) {
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
func (c *Censor) NoRejections(edictID string) (bool, error) {
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
func (c *Censor) LogPrecedent(manifestID, principle string, ruling storage.PrecedentRuling, justification string) (string, error) {
	precedentID := GenerateID("precedent", manifestID, principle)

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
func (c *Censor) RejectManifest(manifestID string) error {
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
func (c *Censor) GetPrecedentsForManifest(manifestID string) ([]storage.CensorPrecedent, error) {
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
func (c *Censor) QueryPrecedentsByPrinciple(principle string, limit int) ([]storage.CensorPrecedent, error) {
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

// GetEdictsWithQuenchedManifests returns edicts with quenched manifests needing review
func (c *Censor) GetEdictsWithQuenchedManifests() ([]storage.Edict, error) {
	var edicts []storage.Edict
	err := c.db.Distinct("edicts.*").
		Joins("JOIN forge_manifests ON forge_manifests.edict_id = edicts.edict_id").
		Where("forge_manifests.status = ? AND edicts.current_phase = ?",
			storage.ManifestQuenched, storage.PhaseReview).
		Find(&edicts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get edicts with quenched manifests: %w", err)
	}
	return edicts, nil
}

// --- Execute Logic ---

// Execute runs the Censor's ethics review for an edict
func (c *Censor) Execute(ctx context.Context, edictID string) (bool, error) {
	// Check if there are any rejections
	noRejections, err := c.NoRejections(edictID)
	if err != nil {
		return false, fmt.Errorf("check rejections: %w", err)
	}

	// Get quenched manifests to review
	manifests, err := c.GetQuenchedManifests(edictID)
	if err != nil {
		return false, fmt.Errorf("get quenched manifests: %w", err)
	}

	if len(manifests) == 0 {
		// No manifests to review, phase complete if no rejections
		if noRejections {
			c.logger.Info("censor review complete, no rejections", "edict_id", edictID)
			return true, nil
		}
		c.logger.Info("censor review blocked by rejections", "edict_id", edictID)
		return false, nil
	}

	// Review each manifest
	for _, manifest := range manifests {
		if err := c.reviewManifest(ctx, &manifest); err != nil {
			return false, fmt.Errorf("review manifest %s: %w", manifest.ManifestID, err)
		}
	}

	// Check again for rejections
	noRejections, err = c.NoRejections(edictID)
	if err != nil {
		return false, fmt.Errorf("check rejections after review: %w", err)
	}

	return noRejections, nil
}

// reviewManifest runs ethics checks on a single manifest
func (c *Censor) reviewManifest(ctx context.Context, manifest *storage.ForgeManifest) error {
	if c.linter == nil {
		// No linter - auto-approve with precedent
		_, err := c.LogPrecedent(
			manifest.ManifestID,
			"auto-approval",
			storage.RulingWaive,
			"No linter configured",
		)
		if err != nil {
			return fmt.Errorf("log auto-approval precedent: %w", err)
		}
		c.logger.Info("manifest auto-approved", "manifest_id", manifest.ManifestID)
		return nil
	}

	// Run linter
	violations, err := c.linter.Analyze(ctx, manifest.FilePath)
	if err != nil {
		return fmt.Errorf("linter analyze: %w", err)
	}

	// Log each violation as a precedent
	hasRejection := false
	for _, v := range violations {
		_, err := c.LogPrecedent(
			manifest.ManifestID,
			v.Principle,
			v.Ruling,
			v.Justification,
		)
		if err != nil {
			return fmt.Errorf("log precedent: %w", err)
		}

		if v.Ruling == storage.RulingReject {
			hasRejection = true
		}
	}

	// Reject manifest if any violations were rejected
	if hasRejection {
		if err := c.RejectManifest(manifest.ManifestID); err != nil {
			return fmt.Errorf("reject manifest: %w", err)
		}
		c.logger.Info("manifest rejected by censor",
			"manifest_id", manifest.ManifestID,
			"violations", len(violations))
	} else {
		c.logger.Info("manifest approved by censor",
			"manifest_id", manifest.ManifestID,
			"waivers", len(violations))
	}

	return nil
}

// Run starts the Censor's polling loop for edicts with quenched manifests
func (c *Censor) Run(ctx context.Context, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	c.logger.Info("censor started", "poll_interval", pollInterval)

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("censor stopped")
			return
		case <-ticker.C:
			c.pollAndExecute(ctx)
		}
	}
}

// pollAndExecute checks for edicts needing ethics review and processes them
func (c *Censor) pollAndExecute(ctx context.Context) {
	edicts, err := c.GetEdictsWithQuenchedManifests()
	if err != nil {
		c.logger.Error("failed to poll review edicts", "error", err)
		return
	}

	for _, edict := range edicts {
		// Check for pending zhengming before processing
		pending, err := c.IsZhengmingPending(edict.EdictID)
		if err != nil {
			c.logger.Error("failed to check zhengming", "edict_id", edict.EdictID, "error", err)
			continue
		}
		if pending {
			continue
		}

		sealed, err := c.Execute(ctx, edict.EdictID)
		if err != nil {
			c.logger.Error("failed to execute review", "edict_id", edict.EdictID, "error", err)
			continue
		}
		if sealed {
			c.logger.Info("review phase sealed", "edict_id", edict.EdictID)
		}
	}
}
