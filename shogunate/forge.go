package shogunate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
)

// Forge receives Tasks from the Chancellor via the tasks channel.
// When an LLM is configured, it creates sessions to process tasks through tool execution.
type Forge struct {
	*MinisterBase // embedded base for database access and session creation
	tasks         chan *Task
}

// NewForge creates a new Forge that processes tasks via the Task pattern.
func NewForge(base *MinisterBase) *Forge {
	base.ministerID = "forge"
	return &Forge{
		MinisterBase: base,
		tasks:        make(chan *Task, 10),
	}
}

// Tasks returns the channel for task submission from Chancellor
func (f *Forge) Tasks() chan<- *Task {
	return f.tasks
}

// ID returns the minister identifier.
func (f *Forge) ID() string {
	return "forge"
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
	for _, t := range tools.GetFileTools() {
		toolList = append(toolList, t)
	}
	// Add shell command tool if runner is available
	if f.runner != nil {
		toolList = append(toolList, tools.NewRunShellCommand(f.runner, f.runner, nil))
	}
	return toolList
}

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
		Update("status", storage.LingDone)
	if result.Error != nil {
		return fmt.Errorf("failed to mark ling completed: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("ling not found: %s", lingID)
	}
	return nil
}

// StageManifest creates a staged manifest (not yet committed to git)
func (f *Forge) StageManifest(edictID, lingID, filePath, funcName, contentSHA string) (string, error) {
	manifestID := GenerateID("manifest", edictID, lingID, filePath)

	manifest := storage.ForgeManifest{
		ManifestID: manifestID,
		EdictID:    edictID,
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
func (f *Forge) DeleteForgedManifest(manifestID string) error {
	result := f.db.Where("manifest_id = ? AND status = ?", manifestID, storage.ManifestForged).
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
	status := storage.LingDone
	description := ling.Description
	if err != nil {
		description = fmt.Sprintf("%s\nerror: %v", ling.Description, err)
	} else {
		description = fmt.Sprintf("%s\nresult: %s", ling.Description, output)
	}
	result := f.db.Model(&storage.Ling{}).
		Where("ling_id = ?", ling.LingID).
		Updates(map[string]interface{}{
			"description": description,
			"status":      status,
		})
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

// Run processes incoming Tasks until context is cancelled.
// Each task is executed and replied to directly via its done channel.
func (f *Forge) Run(ctx context.Context) {
	f.logger.Info("forge started, processing tasks")
	for {
		select {
		case <-ctx.Done():
			f.logger.Info("forge stopped")
			return
		case task := <-f.tasks:
			merged, mergedCancel := context.WithCancel(ctx)
			if task.Ctx != nil {
				context.AfterFunc(task.Ctx, func() { mergedCancel() })
			}
			f.processTask(merged, task)
			mergedCancel()
		}
	}
}

// processTask handles a task from the Chancellor.
// If an LLM is configured, it creates a session to process the task through the LLM,
// which may generate tool calls that the Forge executes.
func (f *Forge) processTask(ctx context.Context, task *Task) {
	f.logger.Info("forge processing task",
		"edict_id", task.EdictID,
		"work", task.Work)

	// Use task-level notify override for routing (e.g., ritual → Ruling tab)
	notify := f.notify
	if task.Notify != nil {
		notify = task.Notify
	}

	var output string
	var taskErr error
	var session *Session

	if f.model != nil {
		if task.Session != nil {
			// Multi-turn: continue existing session
			session = task.Session
			session.SetNotify(notify)
			_, taskErr = session.AskWithStreaming(ctx, task.Work, nil)
		} else {
			// First invocation: create new session
			session, output, taskErr = f.streamTask(ctx, task.Work, task.EdictID, task.Scratchpad, notify)
		}
	} else {
		output = "forge task acknowledged (no LLM configured)"
	}

	result := Result{
		MinisterID: f.ID(),
		Sealed:     true,
		Output:     output,
		Session:    session,
		Err:        taskErr,
	}

	select {
	case task.Done <- result:
	default:
		f.logger.Warn("done channel full, dropping result", "edict_id", task.EdictID)
	}
}

// streamTask creates a session and streams the task through the LLM.
// Returns the session for potential reuse in multi-turn conversations.
func (f *Forge) streamTask(ctx context.Context, work, edictID, scratchpad string, notify internal.NotifyFunc) (*Session, string, error) {
	session, err := CreateSessionWithOpts(f, f.model, f.config, notify, CreateSessionOpts{
		EdictID:    edictID,
		TabID:      "chancellor",
		Scratchpad: scratchpad,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to create forge session: %w", err)
	}

	_, err = session.AskWithStreaming(ctx, work, nil)
	if err != nil {
		return session, "", err
	}

	f.logger.Info("forge task completed")
	return session, "", nil
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
		EdictID    string `json:"edict_id"`
		LingID     string `json:"ling_id"`
		FilePath   string `json:"file_path"`
		FuncName   string `json:"func_name"`
		ContentSHA string `json:"content_sha"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == "" || params.FilePath == "" {
		return "", fmt.Errorf("edict_id and file_path are required")
	}

	manifestID, err := t.forge.StageManifest(params.EdictID, params.LingID, params.FilePath, params.FuncName, params.ContentSHA)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created manifest %s for file %s", manifestID, params.FilePath), nil
}

func (t *CreateManifestTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id":    map[string]any{"type": "string", "description": "The edict ID this manifest belongs to"},
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
