package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

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
	for _, t := range tools.GetFileTools(f.config.LLM) {
		toolList = append(toolList, t)
	}
	// Add shell command tool if runner is available
	if f.runner != nil {
		toolList = append(toolList, tools.NewRunShellCommand(f.runner, f.runner, nil))
	}
	return toolList
}

// GetPendingLing retrieves all pending ling for an edict
func (f *Forge) GetPendingLing(key storage.EdictKey) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := f.db.Where("edict_id = ? AND username = ? AND project = ? AND status = ?", key.ID, key.Username, key.Project, storage.LingPending).
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
func (f *Forge) StageManifest(key storage.EdictKey, lingID, filePath, funcName, contentSHA string) (string, error) {
	manifestID := GenerateID("manifest", fmt.Sprintf("%d", key.ID), lingID, filePath)

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
			// TODO: This looks off, too much context management
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
		"edict_id", task.EdictKey.ID,
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
		// Get pending lings for this edict
		pendingLings, err := f.GetPendingLing(task.EdictKey)
		if err != nil {
			f.logger.Error("failed to get pending lings", "error", err)
			taskErr = err
		} else if len(pendingLings) > 0 {
			// Execute lings in dependency order
			output, taskErr = f.executeLings(ctx, task, pendingLings, notify)
		} else {
			// Fallback: no lings, execute task.Work directly (backward compatibility)
			session, output, taskErr = f.streamTask(ctx, task.Work, task.EdictKey, task.Scratchpad, notify, task.Session)
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
		f.logger.Warn("done channel full, dropping result", "edict_id", task.EdictKey.ID)
	}
}

// streamTask creates a session (or reuses existing) and streams the task through the LLM.
// Returns the session for potential reuse in multi-turn conversations.
func (f *Forge) streamTask(ctx context.Context, work string, key storage.EdictKey, scratchpad string, notify internal.NotifyFunc, existingSession *Session) (*Session, string, error) {
	var session *Session
	var err error

	if existingSession != nil {
		// Reuse existing session for multi-turn conversation
		session = existingSession
		session.SetNotify(notify)
		_, err = session.AskWithStreaming(ctx, work, nil)
		if err != nil {
			return session, "", err
		}
	} else {
		// Create new session for first invocation
		session, err = CreateSessionWithOpts(f, f.model, f.config, notify, CreateSessionOpts{
			EdictKey:   key,
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
	}

	f.logger.Info("forge task completed")
	return session, "", nil
}

// executeLings processes lings in dependency order
func (f *Forge) executeLings(ctx context.Context, task *Task, lings []storage.Ling, notify internal.NotifyFunc) (string, error) {
	// Build dependency graph and sort topologically
	sortedLings, err := f.topologicalSort(lings)
	if err != nil {
		return "", fmt.Errorf("failed to sort lings: %w", err)
	}

	var results []string
	completedLingIDs := make(map[string]bool)
	inBatch := make(map[string]bool, len(lings))
	for _, l := range lings {
		inBatch[l.LingID] = true
	}

	for _, ling := range sortedLings {
		// Check if dependencies are satisfied
		if !f.dependenciesSatisfied(ling, completedLingIDs, inBatch) {
			f.logger.Warn("ling dependencies not satisfied, skipping",
				"ling_id", ling.LingID,
				"dependencies", ling.Dependencies)
			continue
		}

		// Execute this ling
		f.logger.Info("executing ling",
			"ling_id", ling.LingID,
			"description", ling.Description)

		// Build work prompt for this specific ling
		lingWork := ling.Description

		// Create or reuse session for this ling
		var session *Session
		var output string
		var lingErr error

		if task.Session != nil {
			session = task.Session
			session.SetNotify(notify)
			_, lingErr = session.AskWithStreaming(ctx, lingWork, nil)
		} else {
			session, output, lingErr = f.streamTask(ctx, lingWork, task.EdictKey, task.Scratchpad, notify, nil)
		}

		// Update task.Session for multi-ling continuity
		task.Session = session

		if lingErr != nil {
			// Mark ling as blocked/failed
			f.SaveLingResult(&ling, output, lingErr)
			return strings.Join(results, "\n"), fmt.Errorf("ling %s failed: %w", ling.LingID, lingErr)
		}

		// Mark ling as completed
		if err := f.MarkLingCompleted(ling.LingID); err != nil {
			f.logger.Error("failed to mark ling completed",
				"ling_id", ling.LingID,
				"error", err)
		}

		// Save result
		if err := f.SaveLingResult(&ling, output, nil); err != nil {
			f.logger.Error("failed to save ling result",
				"ling_id", ling.LingID,
				"error", err)
		}

		results = append(results, fmt.Sprintf("✓ Ling %s: %s", ling.LingID, ling.Description))
		completedLingIDs[ling.LingID] = true
	}

	return strings.Join(results, "\n"), nil
}

// topologicalSort sorts lings by dependencies (DAG) using Kahn's algorithm
func (f *Forge) topologicalSort(lings []storage.Ling) ([]storage.Ling, error) {
	// Build adjacency map
	lingMap := make(map[string]storage.Ling)
	inDegree := make(map[string]int)
	graph := make(map[string][]string)

	for _, ling := range lings {
		lingMap[ling.LingID] = ling
		inDegree[ling.LingID] = 0
	}

	// Build graph: dependency → dependent.
	// Deps not in lingMap are already completed (filtered out by GetPendingLing's
	// status=pending predicate on retry) — treat them as satisfied, not as a cycle.
	for _, ling := range lings {
		for _, dep := range ling.Dependencies {
			if _, ok := lingMap[dep]; !ok {
				continue
			}
			graph[dep] = append(graph[dep], ling.LingID)
			inDegree[ling.LingID]++
		}
	}

	// Kahn's algorithm
	var queue []string
	for _, ling := range lings {
		if inDegree[ling.LingID] == 0 {
			queue = append(queue, ling.LingID)
		}
	}

	var sorted []storage.Ling
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, lingMap[current])

		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(sorted) != len(lings) {
		return nil, fmt.Errorf("circular dependency detected in lings")
	}

	return sorted, nil
}

// dependenciesSatisfied checks if all dependencies are completed.
// Deps not present in inBatch were completed in a previous forge attempt
// (filtered out by GetPendingLing) and are treated as already satisfied.
func (f *Forge) dependenciesSatisfied(ling storage.Ling, completed map[string]bool, inBatch map[string]bool) bool {
	for _, dep := range ling.Dependencies {
		if !inBatch[dep] {
			continue
		}
		if !completed[dep] {
			return false
		}
	}
	return true
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
		Username   string `json:"username"`
		Project    string `json:"project"`
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

	key := storage.EdictKey{ID: params.EdictID, Username: params.Username, Project: params.Project}

	// Auto-populate ling_id if not provided (use most recent pending ling)
	if params.LingID == "" {
		pendingLings, err := t.forge.GetPendingLing(key)
		if err != nil {
			return "", fmt.Errorf("failed to get pending lings for auto-population: %w", err)
		}
		slog.Info("Forge got a list of lings per edict", "key", key, "lings", pendingLings)
		if len(pendingLings) > 0 {
			params.LingID = pendingLings[0].LingID
			t.forge.logger.Info("auto-populated ling_id",
				"ling_id", params.LingID,
				"file_path", params.FilePath)
		}
	} else {
		slog.Info("Forge is working on ling", "params", params)
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
			"username":    map[string]any{"type": "string", "description": "The username"},
			"project":     map[string]any{"type": "string", "description": "The project name"},
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
