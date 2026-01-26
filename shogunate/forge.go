package shogunate

import (
	"context"
	"fmt"

	"github.com/afittestide/asimi/storage"
)

// Forge receives TaskEnvelopes from the Chancellor via the tasks channel.
// When an LLM is configured, it creates sessions to process tasks through tool execution.
type Forge struct {
	MinisterBase // embedded base for database access and session creation
	tasks        chan *TaskEnvelope
}

// NewForge creates a new Forge that processes tasks via the TaskEnvelope pattern.
func NewForge(base MinisterBase) *Forge {
	base.ministerID = "forge"
	return &Forge{
		MinisterBase: base,
		tasks:        make(chan *TaskEnvelope, 10),
	}
}

// Tasks returns the channel for task submission from Chancellor
func (f *Forge) Tasks() chan<- *TaskEnvelope {
	return f.tasks
}

// ID returns the minister identifier.
func (f *Forge) ID() string {
	return "forge"
}

// Role returns the Forge's role identity text.
func (f *Forge) Role() string {
	return `You are the Forge (工部, Gōngbù). Your domain is Di (地, Earth)—raw code forged into existence.

Your ledger is the forge_manifest table. You stage commits with status='staging' and await Judge's verdict. When status='quenched', you are done. When status='rejected', you reforge.

CRITICAL RULES:
- If requirements are unclear, invoke Zhengming—do not guess
- Stage manifests before git commits for crash recovery
- One manifest per file change
- Generate idiomatic, well-tested code
- You have read/write on forge_manifest, ling.status, and filesystem; read-only on edicts`

}

// Tools returns the Forge's LLM tools for interactive sessions.
func (f *Forge) Tools(notify NotifyFunc) []Tool {
	return GetFileTools()
}

// --- Database Methods ---

// GetPendingLing retrieves all pending ling for an edict
func (f *Forge) GetPendingLing(edictID string) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := f.db.Where("edict_id = ? AND status = ?", edictID, storage.LingPending).
		Order("created_at ASC").
		Find(&ling).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending ling: %w", err)
	}
	return ling, nil
}

// MarkLingCompleted marks a ling as completed
func (f *Forge) MarkLingCompleted(lingID string) error {
	result := f.db.Model(&storage.Ling{}).
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
func (f *Forge) StageManifest(edictID, lingID, filePath, qualifiedName, patchHash string) (string, error) {
	manifestID := GenerateID("manifest", edictID, lingID, filePath)
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

	if err := f.db.Create(&manifest).Error; err != nil {
		return "", fmt.Errorf("failed to stage manifest: %w", err)
	}
	return manifestID, nil
}

// ActivateManifest transitions a staged manifest to pending after git commit
func (f *Forge) ActivateManifest(manifestID, commitHash string) error {
	result := f.db.Model(&storage.ForgeManifest{}).
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
func (f *Forge) DeleteStagedManifest(manifestID string) error {
	result := f.db.Where("manifest_id = ? AND status = ?", manifestID, storage.ManifestStaging).
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
func (f *Forge) GetRejectedManifests(edictID string) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := f.db.Where("edict_id = ? AND status = ?", edictID, storage.ManifestRejected).
		Order("created_at DESC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get rejected manifests: %w", err)
	}
	return manifests, nil
}

// SaveLingResult persists the tool execution result to the database
func (f *Forge) SaveLingResult(ling *storage.Ling, output string, err error) error {
	updates := map[string]interface{}{
		"tool_result": output,
		"status":      storage.LingCompleted,
	}
	if err != nil {
		updates["tool_result"] = fmt.Sprintf("error: %v", err)
	}
	result := f.db.Model(&storage.Ling{}).
		Where("ling_id = ?", ling.LingID).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("failed to save ling result: %w", result.Error)
	}
	return nil
}

// InsertLing creates a new Ling record
func (f *Forge) InsertLing(ling *storage.Ling) error {
	if err := f.db.Create(ling).Error; err != nil {
		return fmt.Errorf("failed to insert ling: %w", err)
	}
	return nil
}

// --- Execute Logic ---

// Run processes incoming TaskEnvelopes until context is cancelled.
// Each task is executed and replied to directly via its reply channel.
func (f *Forge) Run(ctx context.Context) {
	f.logger.Info("forge started, processing tasks")
	for {
		select {
		case <-ctx.Done():
			f.logger.Info("forge stopped")
			return
		case taskEnv := <-f.tasks:
			f.processTaskEnvelope(ctx, taskEnv)
		}
	}
}

// processTaskEnvelope handles a task from the Chancellor.
// If an LLM is configured, it creates a session to process the task through the LLM,
// which may generate tool calls that the Forge executes.
func (f *Forge) processTaskEnvelope(ctx context.Context, env *TaskEnvelope) {
	f.logger.Info("forge processing task",
		"edict_id", env.EdictID,
		"task", env.Task)

	var output string
	var taskErr error

	// If LLM is configured, use a session to process the task
	if f.llm != nil {
		output, taskErr = f.executeTaskWithSession(ctx, env.Task)
	} else {
		// No LLM configured - just acknowledge
		output = "forge task acknowledged (no LLM configured)"
	}

	reply := &TaskReply{
		EdictID:    env.EdictID,
		MinisterID: f.ID(),
		Task:       env.Task,
		Sealed:     true,
		Output:     output,
		Error:      taskErr,
	}

	select {
	case env.ReplyChan <- reply:
	default:
		f.logger.Warn("reply channel full, dropping reply", "edict_id", env.EdictID)
	}
}

// executeTaskWithSession creates a session and processes the task through the LLM.
func (f *Forge) executeTaskWithSession(ctx context.Context, task string) (string, error) {
	// Create a notify function that logs tool execution
	notify := func(msg any) {
		f.logger.Debug("forge session notification", "msg", fmt.Sprintf("%T", msg))
	}

	// Create a session for this task
	session, err := f.CreateSession(f, notify)
	if err != nil {
		return "", fmt.Errorf("failed to create forge session: %w", err)
	}

	// Ask the LLM to process the task
	response, err := session.Ask(ctx, task, nil)
	if err != nil {
		return "", fmt.Errorf("forge session error: %w", err)
	}

	f.logger.Info("forge task completed", "response_length", len(response))
	return response, nil
}

