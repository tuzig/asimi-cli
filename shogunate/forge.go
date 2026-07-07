package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
)

// Forge receives Tasks from the Chancellor via the tasks channel.
// When an LLM is configured, it creates sessions to process tasks through tool execution.
type Forge struct {
	*MinisterBase // embedded base for database access and session creation
}

// NewForge creates a new Forge that processes tasks via the Task pattern.
func NewForge(base *MinisterBase) *Forge {
	base.ministerID = "forge"
	f := &Forge{
		MinisterBase: base,
	}
	f.self = f
	f.SetPreTaskHook(f.handleFailedVerdictsHook)
	return f
}

// SystemPrompt returns the Forge's system prompt template.
func (f *Forge) SystemPrompt() string {
	return `工部. Your domain is 地—simple, clear code forged into existence.

Your ledger is the forge_manifest table. You create manifests with status='forged' and leave them for the Judge to review. You do NOT commit code—commits happen after Judge and Censor approve. When status='rejected', you reforge.

CRITICAL RULES:
- If requirements are unclear, invoke Zhengming—do not guess
- When work is done, create a manifest to record the change (status will be 'forged')
- Generate idiomatic, clear code
- Write tests to verify your code will always work
- Run only the tests that cover your code as the 刑部 will run the complete testing suite
- Do NOT commit to git - other members of the shogunate need to approve your work`
}

// Tools returns the Forge's LLM tools for interactive sessions.
func (f *Forge) Tools() []Tool {
	toolList := []Tool{
		// Specialized Forge tools for manifest tracking
		&CreateManifestTool{forge: f},
	}
	// Add file-based tools
	for _, t := range tools.GetFileTools(f.config.LLM, f.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	// Add shell command tool if runner is available
	if f.runner != nil {
		toolList = append(toolList, tools.NewRunShellCommand(f.CheckHostCommand, f.runner, f.msgChan, f.RepoInfo().ProjectRoot))
	}
	return toolList
}

// GetFailedVerdicts retrieves all failed verdicts for an edict that need fixing.
// It joins with forge_manifests to get context about what failed.
func (f *Forge) GetFailedVerdicts(key storage.EdictKey) ([]storage.JudgeVerdict, error) {
	var verdicts []storage.JudgeVerdict
	err := f.db.Table("judge_verdicts").
		Joins("JOIN forge_manifests ON forge_manifests.manifest_id = judge_verdicts.manifest_id").
		Where("forge_manifests.edict_id = ? AND forge_manifests.username = ? AND forge_manifests.project = ?",
			key.ID, key.Username, key.Project).
		Where("judge_verdicts.outcome = ?", storage.VerdictFailed).
		Order("judge_verdicts.created_at ASC").
		Find(&verdicts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get failed verdicts: %w", err)
	}
	return verdicts, nil
}

// handleFailedVerdictsHook is the PreTaskHook wrapper for HandleFailedVerdicts.
// Returns (handled=true, result) when failed verdicts were found and processed.
func (f *Forge) handleFailedVerdictsHook(ctx context.Context, task *Task, notify internal.NotifyFunc) (bool, *Result) {
	failedVerdicts, err := f.GetFailedVerdicts(task.EdictKey)
	if err != nil {
		f.logger.Error("failed to query failed verdicts", "error", err)
		return true, &Result{Sealed: true, Session: task.Session, Err: err}
	}
	if len(failedVerdicts) == 0 {
		return false, nil
	}

	f.logger.Info("found failed verdicts, fixing instead of act",
		"count", len(failedVerdicts),
		"edict_id", task.EdictKey.ID)

	for _, verdict := range failedVerdicts {
		f.logger.Info("forge working on verdict",
			"verdict_id", verdict.VerdictID,
			"manifest_id", verdict.ManifestID,
			"edict_id", task.EdictKey.ID)
		fixWork := f.buildFixPrompt(verdict)
		session, _, taskErr := f.streamTask(ctx, fixWork, task.EdictKey, task.Scratchpad, notify, task.Session, task.ChannelID)
		task.Session = session
		if taskErr != nil {
			f.logger.Error("failed to fix verdict", "verdict_id", verdict.VerdictID, "error", taskErr)
			return true, &Result{Sealed: true, Session: task.Session, Err: taskErr}
		}
	}
	f.logger.Info("finished fixing verdicts")
	return true, &Result{Sealed: true, Session: task.Session}
}

// buildFixPrompt creates a fix prompt from a failed verdict's evidence
func (f *Forge) buildFixPrompt(verdict storage.JudgeVerdict) string {
	evidenceJSON, _ := json.Marshal(verdict.Evidence)
	return fmt.Sprintf(`A Judge has recorded a failed verdict for manifest %s.
The verdict contains evidence of what failed: %s

Focus on minimal, targeted changes to fulfill the intent.
Do not repeat work that already passed judgment.`, verdict.ManifestID, string(evidenceJSON))
}

// StageManifest creates a staged manifest (not yet committed to git)
func (f *Forge) StageManifest(key storage.EdictKey, lingID, filePath, funcName, contentSHA string) (string, error) {
	// Add timestamp to ensure uniqueness across retries/loops
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano())
	manifestID := GenerateID("manifest", fmt.Sprintf("%d", key.ID), lingID, filePath, timestamp)

	manifest := storage.ForgeManifest{
		ManifestID: manifestID,
		EdictID:    key.ID,
		Username:   key.Username,
		Project:    key.Project,
		LingID:     lingID,
		FilePath:   filePath,
		FuncName:   funcName,
		ContentSHA: contentSHA,
		Status:     storage.ManifestForged,
	}

	if err := f.db.Create(&manifest).Error; err != nil {
		return "", fmt.Errorf("failed to stage manifest: %w", err)
	}
	return manifestID, nil
}

// DeleteForgedManifest removes a forged manifest
func (f *Forge) DeleteForgedManifest(manifestID string, key storage.EdictKey) error {
	result := f.db.Where("manifest_id = ? AND username = ? AND project = ? AND status = ?",
		manifestID, key.Username, key.Project, storage.ManifestForged).
		Delete(&storage.ForgeManifest{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete forged manifest: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("forged manifest not found: %s", manifestID)
	}
	return nil
}

// GetRejectedManifests retrieves all rejected manifests for an edict
func (f *Forge) GetRejectedManifests(key storage.EdictKey) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := f.db.Where("edict_id = ? AND username = ? AND project = ? AND status = ?", key.ID, key.Username, key.Project, storage.ManifestRejected).
		Order("created_at DESC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get rejected manifests: %w", err)
	}
	return manifests, nil
}

// --- Execute Logic ---

// Run starts the Forge's processing loop
func (f *Forge) Run(ctx context.Context) {
	f.RunLoop(ctx, f, nil, f.MinisterBase.processTask)
}

// --- Forge Specialized Tools ---

// CreateManifestTool records a new file change in the forge manifest
type CreateManifestTool struct {
	forge *Forge
}

func (t *CreateManifestTool) Name() string { return "create_manifest" }

func (t *CreateManifestTool) Description() string {
	return "Records a new file change in the forge manifest. Input: JSON with 'edict_id', 'ling_id', 'file_path', 'func_name' (optional), and 'content_sha'."
}

func (t *CreateManifestTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID    uint   `json:"edict_id"`
		LingID     string `json:"ling_id"`
		FilePath   string `json:"file_path"`
		FuncName   string `json:"func_name"`
		ContentSHA string `json:"content_sha"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == 0 || params.FilePath == "" {
		return "", fmt.Errorf("edict_id and file_path are required")
	}

	key := storage.EdictKey{ID: params.EdictID, Username: t.forge.username, Project: t.forge.project}

	// Auto-populate ling_id if not provided.
	// In the fork-based ritual pattern, ling_id comes from the fork item context
	// ({{ .item.ling_id }}) via template expansion. When the LLM doesn't pass it
	// explicitly, we skip auto-population rather than querying pending lings,
	// since the ling iteration is now handled by the ritual engine's fork.
	if params.LingID == "" {
		t.forge.logger.Info("ling_id not provided by LLM, leaving empty",
			"file_path", params.FilePath)
	}

	manifestID, err := t.forge.StageManifest(key, params.LingID, params.FilePath, params.FuncName, params.ContentSHA)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created manifest %s for file %s (ling: %s)", manifestID, params.FilePath, params.LingID), nil
}

func (t *CreateManifestTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id":    map[string]any{"type": "integer", "description": "The edict ID this manifest belongs to"},
			"ling_id":     map[string]any{"type": "string", "description": "The ling ID this manifest implements"},
			"file_path":   map[string]any{"type": "string", "description": "Path to the modified file"},
			"func_name":   map[string]any{"type": "string", "description": "Name of the function being modified (optional)"},
			"content_sha": map[string]any{"type": "string", "description": "SHA of the file content"},
		},
		"required": []string{"edict_id", "file_path"},
	}
}

func (t *CreateManifestTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Create Manifest: Error: %v\n", err)
	}
	return fmt.Sprintf("Create Manifest: %s\n", result)
}
