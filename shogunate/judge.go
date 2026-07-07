package shogunate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// JudgePrompt defines the Judge's identity and capabilities
const JudgePrompt = `刑部. Your domain is 天—test results and testing code.

You preside over the verdicts table. You review 'forged' manifests against the working tree. When tests pass, you update forge_manifest to 'quenched'. When they fail, you mark 'rejected'.

You are adversarial and data-driven. Your word is final.

CRITICAL RULES:
- If test criteria are ambiguous, invoke Zhengming—do not guess
- Code is guilty until proven innocent
- Verdicts are immutable once rendered
- Evidence must be preserved in JSON format
- You have read/write on verdicts and forge_manifest.status/verdict_id; execute access on shell`

// Judge evaluates code through CI and renders verdicts
type Judge struct {
	*MinisterBase // embedded base for database access and session creation
	ci            CIRunner
}

// NewJudge creates a new Judge minister
func NewJudge(base *MinisterBase, ci CIRunner) *Judge {
	base.ministerID = "judge"
	j := &Judge{
		MinisterBase: base,
		ci:           ci,
	}
	j.self = j
	j.SetPostTaskHook(j.sealIfCompleteHook)
	j.SetTaskFallback(j.executeFallback)
	return j
}

// SystemPrompt returns the Judge's system prompt template.
func (j *Judge) SystemPrompt() string {
	return JudgePrompt
}

// Tools returns the Judge's LLM tools for interactive sessions
func (j *Judge) Tools() []Tool {
	toolList := []Tool{
		// Specialized Judge tools for verdict management
		&RecordVerdictTool{judge: j},
		&ListPendingManifestsTool{judge: j},
		&UpdateManifestStatusTool{judge: j},
		tools.RequestZhengmingTool{MinisterID: j.ministerID, Requester: j, WaitForAnswer: j.WaitForZhengming, Username: j.username, Project: j.project},
	}
	// Add edit tools (read, write, edit, list, grep)
	for _, t := range tools.GetEditTools(j.config.LLM, j.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	// Add shell command tool if runner is available
	if j.runner != nil {
		toolList = append(toolList, tools.NewRunShellCommand(j.CheckHostCommand, j.runner, j.msgChan, j.RepoInfo().ProjectRoot))
	}
	return toolList
}

// --- Database Methods ---

// GetPendingManifests retrieves all pending manifests for an edict
func (j *Judge) GetPendingManifests(key storage.EdictKey) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := j.db.Where("edict_id = ? AND username = ? AND project = ? AND status = ?", key.ID, key.Username, key.Project, storage.ManifestForged).
		Order("created_at ASC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending manifests: %w", err)
	}
	return manifests, nil
}

// AllManifestsQuenched checks if all manifests for an edict are quenched.
// For rituals with no manifests (e.g., project-init), it also checks for
// edict-level verdicts as an alternative completion signal.
func (j *Judge) AllManifestsQuenched(key storage.EdictKey) (bool, error) {
	// Only count the latest manifest per file_path; superseded manifests should
	// not inflate the pending count after reforging.
	var pendingCount int64
	err := j.db.Raw(`
		SELECT COUNT(*) FROM forge_manifests fm
		WHERE fm.edict_id = ? AND fm.username = ? AND fm.project = ?
		  AND fm.status != ?
		  AND fm.created_at = (
		    SELECT MAX(fm2.created_at) FROM forge_manifests fm2
		    WHERE fm2.edict_id = fm.edict_id
		      AND fm2.username = fm.username
		      AND fm2.project = fm.project
		      AND fm2.file_path = fm.file_path
		  )`,
		key.ID, key.Username, key.Project, storage.ManifestQuenched).
		Scan(&pendingCount).Error
	if err != nil {
		return false, fmt.Errorf("failed to check quenched status: %w", err)
	}

	// Also check that at least one manifest exists (latest per file_path only)
	var totalCount int64
	err = j.db.Raw(`
		SELECT COUNT(*) FROM forge_manifests fm
		WHERE fm.edict_id = ? AND fm.username = ? AND fm.project = ?
		  AND fm.created_at = (
		    SELECT MAX(fm2.created_at) FROM forge_manifests fm2
		    WHERE fm2.edict_id = fm.edict_id
		      AND fm2.username = fm.username
		      AND fm2.project = fm.project
		      AND fm2.file_path = fm.file_path
		  )`,
		key.ID, key.Username, key.Project).
		Scan(&totalCount).Error
	if err != nil {
		return false, fmt.Errorf("failed to count manifests: %w", err)
	}

	if totalCount > 0 {
		// Has manifests: all must be quenched
		return pendingCount == 0, nil
	}

	// No manifests: check the outcome of the latest edict-level verdict
	var latestVerdict storage.JudgeVerdict
	err = j.db.Where("manifest_id = '' AND test_suite = 'edict' AND username = ? AND project = ?", key.Username, key.Project).
		Order("created_at DESC").
		First(&latestVerdict).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil // no verdict at all
		}
		return false, fmt.Errorf("failed to check edict-level verdicts: %w", err)
	}
	return latestVerdict.Outcome == storage.VerdictPassed, nil
}

// sealIfCompleteHook is the PostTaskHook wrapper for sealIfComplete.
func (j *Judge) sealIfCompleteHook(ctx context.Context, task *Task, session *Session, output string) (string, error) {
	j.sealIfComplete(task.EdictKey)
	return "", nil
}

// executeFallback is the TaskFallback for deterministic CI execution.
func (j *Judge) executeFallback(ctx context.Context, task *Task) (bool, error) {
	return j.execute(ctx, task.EdictKey)
}

// sealIfComplete checks whether all manifests for the edict are quenched
// and, if so, grants the Judge's seal. Returns true when sealed.
func (j *Judge) sealIfComplete(key storage.EdictKey) bool {
	allQuenched, err := j.AllManifestsQuenched(key)
	if err != nil {
		j.logger.Warn("failed to check quenched status", "error", err)
		return false
	}
	if !allQuenched {
		return false
	}
	if err := j.grantSeal(key, storage.JSON{"type": "judgment_complete"}); err != nil {
		j.logger.Warn("failed to grant judge seal", "edict_id", key.ID, "error", err)
		return false
	}
	return true
}

// InsertVerdict records a CI judgment for a manifest
func (j *Judge) InsertVerdict(manifestID, testSuite string, outcome storage.VerdictOutcome, evidence storage.JSON, key storage.EdictKey) (string, error) {
	verdictID := GenerateID("verdict", manifestID, testSuite)

	verdict := storage.JudgeVerdict{
		VerdictID:  verdictID,
		ManifestID: manifestID,
		Username:   key.Username,
		Project:    key.Project,
		TestSuite:  testSuite,
		Outcome:    outcome,
		Evidence:   evidence,
	}

	if err := j.db.Create(&verdict).Error; err != nil {
		return "", fmt.Errorf("failed to insert verdict: %w", err)
	}
	return verdictID, nil
}

// UpdateManifestStatus updates a manifest's status after judgment.
// It uses the EdictKey to filter by username/project for defense-in-depth,
// ensuring a manifest can only be updated within its own project scope.
func (j *Judge) UpdateManifestStatus(manifestID string, status storage.ManifestStatus, verdictID string, key storage.EdictKey) error {
	result := j.db.Model(&storage.ForgeManifest{}).
		Where("manifest_id = ? AND username = ? AND project = ?", manifestID, key.Username, key.Project).
		Updates(map[string]interface{}{
			"status":     status,
			"verdict_id": verdictID,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to update manifest status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("manifest not found: %s (username=%s, project=%s)", manifestID, key.Username, key.Project)
	}
	return nil
}

// GetEdictsWithPendingManifests returns edicts that have pending manifests needing judgment
func (j *Judge) GetEdictsWithPendingManifests() ([]storage.Edict, error) {
	var edicts []storage.Edict
	err := j.db.Distinct("edicts.*").
		Joins("JOIN forge_manifests ON forge_manifests.edict_id = edicts.id AND forge_manifests.username = edicts.username AND forge_manifests.project = edicts.project").
		Where("forge_manifests.status = ?", storage.ManifestForged).
		Find(&edicts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get edicts with pending manifests: %w", err)
	}

	// Filter out sealed/cancelled edicts using derived status
	sealService := storage.NewSealService(j.db)
	var activeEdicts []storage.Edict
	for _, e := range edicts {
		status, err := sealService.GetEdictStatus(storage.EdictKey{ID: e.ID, Username: e.Username, Project: e.Project})
		if err != nil {
			continue
		}
		if status == storage.EdictActive || status == storage.EdictBlocked {
			activeEdicts = append(activeEdicts, e)
		}
	}
	return activeEdicts, nil
}

// --- Execute Logic ---

// execute runs the Judge's CI evaluation for an edict (internal method)
func (j *Judge) execute(ctx context.Context, key storage.EdictKey) (bool, error) {
	// Check if all manifests are already quenched
	allQuenched, err := j.AllManifestsQuenched(key)
	if err != nil {
		return false, fmt.Errorf("check quenched: %w", err)
	}
	if allQuenched {
		j.logger.Info("all manifests quenched, judgment complete", "edict_id", key.ID)
		return true, nil
	}

	// Get pending manifests
	manifests, err := j.GetPendingManifests(key)
	if err != nil {
		return false, fmt.Errorf("get pending manifests: %w", err)
	}

	if len(manifests) == 0 {
		j.logger.Info("no pending manifests", "edict_id", key.ID)
		return false, nil
	}

	// Judge each manifest
	for _, manifest := range manifests {
		if err := j.judgeManifest(ctx, key, &manifest); err != nil {
			return false, fmt.Errorf("judge manifest %s: %w", manifest.ManifestID, err)
		}
	}

	return j.sealIfComplete(key), nil
}

// judgeManifest runs CI for a single manifest
func (j *Judge) judgeManifest(ctx context.Context, key storage.EdictKey, manifest *storage.ForgeManifest) error {
	if j.ci == nil {
		// No CI runner - auto-pass
		verdictID, err := j.InsertVerdict(
			manifest.ManifestID,
			"auto",
			storage.VerdictPassed,
			storage.JSON{"reason": "no CI configured"},
			key,
		)
		if err != nil {
			return fmt.Errorf("insert auto-pass verdict: %w", err)
		}
		return j.UpdateManifestStatus(manifest.ManifestID, storage.ManifestQuenched, verdictID, key)
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
		key,
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

	if err := j.UpdateManifestStatus(manifest.ManifestID, newStatus, verdictID, key); err != nil {
		return fmt.Errorf("update manifest status: %w", err)
	}

	j.logger.Info("manifest judged",
		"manifest_id", manifest.ManifestID,
		"outcome", outcome,
		"new_status", newStatus)

	return nil
}

// Run starts the Judge's task processing loop
func (j *Judge) Run(ctx context.Context) {
	j.RunLoop(ctx, j, nil, j.MinisterBase.processTask)
}

// --- Judge Specialized Tools ---

// RecordVerdictTool records test results for an edict or specific Ling
type RecordVerdictTool struct {
	judge *Judge
}

func (t *RecordVerdictTool) Name() string { return "record_verdict" }

func (t *RecordVerdictTool) Description() string {
	return "Records test results for an edict or specific Ling. Input: JSON with 'edict_id', 'ling_id' (optional), 'passed' (boolean), and 'details' (optional)."
}

func (t *RecordVerdictTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID  uint   `json:"edict_id"`
		Username string `json:"username"`
		Project  string `json:"project"`
		LingID   string `json:"ling_id"`
		Passed   bool   `json:"passed"`
		Details  string `json:"details"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}

	key := storage.EdictKey{ID: params.EdictID, Username: t.judge.username, Project: t.judge.project}

	// Get manifests for this edict
	manifests, err := t.judge.GetPendingManifests(key)
	if err != nil {
		return "", err
	}

	outcome := storage.VerdictPassed
	if !params.Passed {
		outcome = storage.VerdictFailed
	}

	recordedCount := 0

	// Record verdict for each manifest
	for _, m := range manifests {
		evidence := storage.JSON{"details": params.Details}
		if params.LingID != "" && m.LingID != params.LingID {
			continue
		}

		verdictID, err := t.judge.InsertVerdict(m.ManifestID, "test", outcome, evidence, key)
		if err != nil {
			return "", fmt.Errorf("failed to record verdict: %w", err)
		}

		// Update manifest status
		status := storage.ManifestQuenched
		if !params.Passed {
			status = storage.ManifestRejected
		}
		if err := t.judge.UpdateManifestStatus(m.ManifestID, status, verdictID, key); err != nil {
			return "", fmt.Errorf("failed to update manifest status: %w", err)
		}
		recordedCount++
	}

	// If no manifests, record verdict for edict directly (for rituals like project-init)
	if recordedCount == 0 {
		verdictID := GenerateID("verdict", fmt.Sprintf("%d", params.EdictID), "edict", "direct")
		verdict := storage.JudgeVerdict{
			VerdictID:  verdictID,
			ManifestID: "", // empty = edict-level verdict
			Username:   key.Username,
			Project:    key.Project,
			TestSuite:  "edict",
			Outcome:    outcome,
			Evidence:   storage.JSON{"details": params.Details},
		}
		if err := t.judge.db.Create(&verdict).Error; err != nil {
			return "", fmt.Errorf("failed to record edict verdict: %w", err)
		}
		recordedCount++
	}

	sealed := t.judge.sealIfComplete(key)
	return fmt.Sprintf("Recorded verdict (passed=%v) for edict %d (sealed=%v)", params.Passed, params.EdictID, sealed), nil
}

func (t *RecordVerdictTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{"type": "integer", "description": "The edict ID"},
			"ling_id":  map[string]any{"type": "string", "description": "Optional Ling ID to record verdict for"},
			"passed":   map[string]any{"type": "boolean", "description": "Whether the tests passed"},
			"details":  map[string]any{"type": "string", "description": "Additional details about the verdict"},
		},
		"required": []string{"edict_id", "passed"},
	}
}

func (t *RecordVerdictTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Record Verdict: Error: %v\n", err)
	}
	return fmt.Sprintf("Record Verdict: %s\n", result)
}

// ListPendingManifestsTool lists manifests awaiting judgment
type ListPendingManifestsTool struct {
	judge *Judge
}

func (t *ListPendingManifestsTool) Name() string { return "list_pending_manifests" }

func (t *ListPendingManifestsTool) Description() string {
	return "Lists manifests awaiting CI judgment. Input: JSON with 'edict_id'."
}

func (t *ListPendingManifestsTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID uint `json:"edict_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}

	key := storage.EdictKey{ID: params.EdictID, Username: t.judge.username, Project: t.judge.project}
	manifests, err := t.judge.GetPendingManifests(key)
	if err != nil {
		return "", err
	}

	if len(manifests) == 0 {
		return "No pending manifests found", nil
	}

	result, err := json.MarshalIndent(manifests, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to format manifests: %w", err)
	}
	return string(result), nil
}

func (t *ListPendingManifestsTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{"type": "integer", "description": "The edict ID to list manifests for"},
		},
		"required": []string{"edict_id"},
	}
}

func (t *ListPendingManifestsTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("List Pending Manifests: Error: %v\n", err)
	}
	var manifests []json.RawMessage
	if json.Unmarshal([]byte(result), &manifests) == nil {
		return fmt.Sprintf("Listed %d pending manifests\n", len(manifests))
	}
	t.judge.logger.Error("list_pending_manifests: expected JSON array from Call, got non-JSON result", "result", result)
	return result + "\n"
}

// UpdateManifestStatusTool updates a manifest's status after judgment
type UpdateManifestStatusTool struct {
	judge *Judge
}

func (t *UpdateManifestStatusTool) Name() string { return "update_manifest_status" }

func (t *UpdateManifestStatusTool) Description() string {
	return "Updates a manifest's status to 'quenched' (passed) or 'rejected' (failed). Input: JSON with 'manifest_id', 'status', and 'verdict_id'."
}

func (t *UpdateManifestStatusTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		ManifestID string `json:"manifest_id"`
		Status     string `json:"status"`
		VerdictID  string `json:"verdict_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.ManifestID == "" || params.Status == "" {
		return "", fmt.Errorf("manifest_id and status are required")
	}

	// Map string status to enum
	statusMap := map[string]storage.ManifestStatus{
		"quenched": storage.ManifestQuenched,
		"rejected": storage.ManifestRejected,
	}
	status, ok := statusMap[params.Status]
	if !ok {
		return "", fmt.Errorf("invalid status: %s (use 'quenched' or 'rejected')", params.Status)
	}

	if err := t.judge.UpdateManifestStatus(params.ManifestID, status, params.VerdictID, storage.EdictKey{Username: t.judge.username, Project: t.judge.project}); err != nil {
		return "", err
	}

	return fmt.Sprintf("Updated manifest %s status to %s", params.ManifestID, params.Status), nil
}

func (t *UpdateManifestStatusTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"manifest_id": map[string]any{"type": "string", "description": "The manifest ID to update"},
			"status":      map[string]any{"type": "string", "description": "New status: 'quenched' or 'rejected'"},
			"verdict_id":  map[string]any{"type": "string", "description": "The verdict ID that determined this status"},
		},
		"required": []string{"manifest_id", "status"},
	}
}

func (t *UpdateManifestStatusTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Update Manifest Status: Error: %v\n", err)
	}
	return fmt.Sprintf("Update Manifest Status: %s\n", result)
}
