package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// --- Minister ---

// JudgePrompt defines the Judge's identity and capabilities
const JudgePrompt = `You are the Judge (刑部, Xíngbù). Your domain is Tian (天, Heaven)—objective truth.

You preside over the verdicts table. Your CI pipeline is the court; its failure is Tian's voice. When tests pass, you update forge_manifest to 'quenched'. When they fail, you mark 'rejected'.

You are adversarial and data-driven. Your word is final.

# Tools

## Shogunate Tools
- **list_pending_manifests**: Get manifests awaiting CI judgment (status='pending')
- **insert_verdict**: Record a CI verdict (passed/failed) with evidence
- **update_manifest_status**: Update manifest to 'quenched' (passed) or 'rejected' (failed)
- **request_zhengming**: Ask the Ruler for clarification when test criteria are ambiguous

## Standard Tools (execute and read)
- **run_shell_command**: Execute test runners, CI pipelines, build commands
- **read_file**: Read test output, logs, and evidence files

CRITICAL RULES:
- If test criteria are ambiguous, invoke Zhengming—do not guess
- Code is guilty until proven innocent
- Verdicts are immutable once rendered
- Evidence must be preserved in JSON format
- You have read/write on verdicts and forge_manifest.status/verdict_id; execute access on shell`

// Judge evaluates code through CI and renders verdicts
type Judge struct {
	MinisterBase // embedded base for database access and session creation
	ci           CIRunner
}

// NewJudge creates a new Judge minister
func NewJudge(db *gorm.DB, ci CIRunner, logger *slog.Logger) *Judge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Judge{
		MinisterBase: MinisterBase{db: db, ministerID: "judge", logger: logger},
		ci:           ci,
	}
}

// ID returns the minister identifier
func (j *Judge) ID() string {
	return "judge"
}

// Role returns the Judge's role identity text
func (j *Judge) Role() string {
	return JudgePrompt
}

// Tools returns the Judge's LLM tools for interactive sessions
func (j *Judge) Tools(notify NotifyFunc) []Tool {
	// TODO: Implement Judge tools (list_pending_manifests, insert_verdict, update_manifest_status)
	return []Tool{}
}

// --- Database Methods ---

// GetPendingManifests retrieves all pending manifests for an edict
func (j *Judge) GetPendingManifests(edictID string) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := j.db.Where("edict_id = ? AND status = ?", edictID, storage.ManifestPending).
		Order("created_at ASC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending manifests: %w", err)
	}
	return manifests, nil
}

// AllManifestsQuenched checks if all manifests for an edict are quenched
func (j *Judge) AllManifestsQuenched(edictID string) (bool, error) {
	var pendingCount int64
	err := j.db.Model(&storage.ForgeManifest{}).
		Where("edict_id = ? AND status != ?", edictID, storage.ManifestQuenched).
		Count(&pendingCount).Error
	if err != nil {
		return false, fmt.Errorf("failed to check quenched status: %w", err)
	}

	// Also check that at least one manifest exists
	var totalCount int64
	err = j.db.Model(&storage.ForgeManifest{}).
		Where("edict_id = ?", edictID).
		Count(&totalCount).Error
	if err != nil {
		return false, fmt.Errorf("failed to count manifests: %w", err)
	}

	return totalCount > 0 && pendingCount == 0, nil
}

// InsertVerdict records a CI judgment for a manifest
func (j *Judge) InsertVerdict(manifestID, testSuite string, outcome storage.VerdictOutcome, evidence storage.JSON) (string, error) {
	verdictID := GenerateID("verdict", manifestID, testSuite)

	// Get manifest for idempotency key
	var manifest storage.ForgeManifest
	if err := j.db.First(&manifest, "manifest_id = ?", manifestID).Error; err != nil {
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

	if err := j.db.Create(&verdict).Error; err != nil {
		return "", fmt.Errorf("failed to insert verdict: %w", err)
	}
	return verdictID, nil
}

// UpdateManifestStatus updates a manifest's status after judgment
func (j *Judge) UpdateManifestStatus(manifestID string, status storage.ManifestStatus, verdictID string) error {
	result := j.db.Model(&storage.ForgeManifest{}).
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

// GetEdictsWithPendingManifests returns edicts that have pending manifests needing judgment
func (j *Judge) GetEdictsWithPendingManifests() ([]storage.Edict, error) {
	var edicts []storage.Edict
	err := j.db.Distinct("edicts.*").
		Joins("JOIN forge_manifests ON forge_manifests.edict_id = edicts.edict_id").
		Where("forge_manifests.status = ? AND edicts.current_phase = ?",
			storage.ManifestPending, storage.PhaseJudgment).
		Find(&edicts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get edicts with pending manifests: %w", err)
	}
	return edicts, nil
}

// --- Execute Logic ---

// Execute runs the Judge's CI evaluation for an edict
func (j *Judge) Execute(ctx context.Context, edictID string) (bool, error) {
	// Check if all manifests are already quenched
	allQuenched, err := j.AllManifestsQuenched(edictID)
	if err != nil {
		return false, fmt.Errorf("check quenched: %w", err)
	}
	if allQuenched {
		j.logger.Info("all manifests quenched, judgment complete", "edict_id", edictID)
		return true, nil
	}

	// Get pending manifests
	manifests, err := j.GetPendingManifests(edictID)
	if err != nil {
		return false, fmt.Errorf("get pending manifests: %w", err)
	}

	if len(manifests) == 0 {
		j.logger.Info("no pending manifests", "edict_id", edictID)
		return false, nil
	}

	// Judge each manifest
	for _, manifest := range manifests {
		if err := j.judgeManifest(ctx, edictID, &manifest); err != nil {
			return false, fmt.Errorf("judge manifest %s: %w", manifest.ManifestID, err)
		}
	}

	// Check again if all are now quenched
	allQuenched, err = j.AllManifestsQuenched(edictID)
	if err != nil {
		return false, fmt.Errorf("check quenched after judging: %w", err)
	}

	return allQuenched, nil
}

// judgeManifest runs CI for a single manifest
func (j *Judge) judgeManifest(ctx context.Context, edictID string, manifest *storage.ForgeManifest) error {
	if j.ci == nil {
		// No CI runner - auto-pass
		verdictID, err := j.InsertVerdict(
			manifest.ManifestID,
			"auto",
			storage.VerdictPassed,
			storage.JSON(`{"reason":"no CI configured"}`),
		)
		if err != nil {
			return fmt.Errorf("insert auto-pass verdict: %w", err)
		}
		return j.UpdateManifestStatus(manifest.ManifestID, storage.ManifestQuenched, verdictID)
	}

	// Run CI
	outcome, evidence, err := j.ci.Run(ctx, manifest.CommitHash)
	if err != nil {
		return fmt.Errorf("CI run: %w", err)
	}

	// Insert verdict
	verdictID, err := j.InsertVerdict(
		manifest.ManifestID,
		j.ci.GetTestSuite(),
		outcome,
		evidence,
	)
	if err != nil {
		return fmt.Errorf("insert verdict: %w", err)
	}

	// Update manifest status based on verdict
	var newStatus storage.ManifestStatus
	if outcome == storage.VerdictPassed {
		newStatus = storage.ManifestQuenched
	} else {
		newStatus = storage.ManifestRejected
	}

	if err := j.UpdateManifestStatus(manifest.ManifestID, newStatus, verdictID); err != nil {
		return fmt.Errorf("update manifest status: %w", err)
	}

	j.logger.Info("manifest judged",
		"manifest_id", manifest.ManifestID,
		"outcome", outcome,
		"new_status", newStatus)

	return nil
}

// Run starts the Judge's polling loop for edicts with pending manifests
func (j *Judge) Run(ctx context.Context, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	j.logger.Info("judge started", "poll_interval", pollInterval)

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("judge stopped")
			return
		case <-ticker.C:
			j.pollAndExecute(ctx)
		}
	}
}

// pollAndExecute checks for edicts needing judgment and processes them
func (j *Judge) pollAndExecute(ctx context.Context) {
	edicts, err := j.GetEdictsWithPendingManifests()
	if err != nil {
		j.logger.Error("failed to poll judgment edicts", "error", err)
		return
	}

	for _, edict := range edicts {
		// Check for pending zhengming before processing
		pending, err := j.IsZhengmingPending(edict.EdictID)
		if err != nil {
			j.logger.Error("failed to check zhengming", "edict_id", edict.EdictID, "error", err)
			continue
		}
		if pending {
			continue
		}

		sealed, err := j.Execute(ctx, edict.EdictID)
		if err != nil {
			j.logger.Error("failed to execute judgment", "edict_id", edict.EdictID, "error", err)
			continue
		}
		if sealed {
			j.logger.Info("judgment phase sealed", "edict_id", edict.EdictID)
		}
	}
}
