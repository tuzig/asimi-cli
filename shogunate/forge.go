package shogunate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// It receives LingEnvelopes, executes the requested tools, and replies directly.
type Forge struct {
	MinisterBase // embedded base for database access and session creation
	addLing      chan *LingEnvelope
	tools        map[string]Tool
}

// NewForge creates a new Forge that processes tool calls via envelopes.
func NewForge(db *gorm.DB, tools map[string]Tool, logger *slog.Logger) *Forge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Forge{
		MinisterBase: MinisterBase{db: db, ministerID: "forge", logger: logger},
		addLing:      make(chan *LingEnvelope, 100),
		tools:        tools,
	}
}

// AddLing returns the channel for sending LingEnvelopes.
func (f *Forge) AddLing() chan<- *LingEnvelope {
	return f.addLing
}

// SetTools updates the tool registry. This is called when a Session connects
// to provide the Forge with the Session's available tools.
func (f *Forge) SetTools(tools map[string]Tool) {
	f.tools = tools
	f.logger.Info("forge tools updated", "count", len(tools))
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
	return []Tool{}
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

// Run processes incoming LingEnvelopes until context is cancelled.
// Each envelope is executed and replied to directly via its reply channel.
func (f *Forge) Run(ctx context.Context) {
	f.logger.Info("forge started, processing envelopes")
	for {
		select {
		case <-ctx.Done():
			f.logger.Info("forge stopped")
			return
		case env := <-f.addLing:
			f.processEnvelope(ctx, env)
		}
	}
}

// processEnvelope executes a single envelope and replies.
func (f *Forge) processEnvelope(ctx context.Context, env *LingEnvelope) {
	tool, ok := f.tools[env.Ling.ToolName]
	if !ok {
		f.logger.Warn("unknown tool", "tool", env.Ling.ToolName)
		env.ReplyChan <- &LingResult{
			Ling:   env.Ling,
			Output: "",
			Error:  fmt.Errorf("unknown tool: %s", env.Ling.ToolName),
		}
		return
	}

	f.logger.Debug("executing tool", "tool", env.Ling.ToolName, "ling_id", env.Ling.LingID)
	output, err := tool.Call(ctx, string(env.Ling.ToolInput))

	// Async persist (audit trail) - fire and forget
	go f.persist(env.Ling, output, err)

	// Reply directly to the envelope's channel
	env.ReplyChan <- &LingResult{
		Ling:   env.Ling,
		Output: output,
		Error:  err,
	}
}

// persist saves the Ling result to the database asynchronously.
func (f *Forge) persist(ling *storage.Ling, output string, err error) {
	// First, insert the Ling if it doesn't exist
	if insertErr := f.db.Create(ling).Error; insertErr != nil {
		// Ling may already exist (idempotency), that's fine
		f.logger.Debug("ling insert (may exist)", "ling_id", ling.LingID, "error", insertErr)
	}

	// Update with result
	updates := map[string]interface{}{
		"tool_result": output,
		"status":      storage.LingCompleted,
	}
	if err != nil {
		updates["tool_result"] = fmt.Sprintf("error: %v", err)
	}

	if updateErr := f.db.Model(&storage.Ling{}).
		Where("ling_id = ?", ling.LingID).
		Updates(updates).Error; updateErr != nil {
		f.logger.Error("failed to persist ling result", "ling_id", ling.LingID, "error", updateErr)
	}
}

// Execute is a no-op for the envelope-based Forge.
// Tool execution happens via the envelope pattern in Run().
func (f *Forge) Execute(ctx context.Context, edictID string) (bool, error) {
	// The envelope-based Forge doesn't use Execute for tool calls.
	// This is kept for Minister interface compatibility.
	return true, nil
}
