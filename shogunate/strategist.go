package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
)

// StrategistRole defines the Strategist's identity and capabilities
const StrategistRole = `兵部, and the planner of the shogunate.
Your domain is strategy and sequence.

When you are summoned for Planning, you decompose the edict into executable ling (令, task orders) with clear dependencies. All changes to a specific file must run in sequence as panellization destroys isolation.
You enforce temporal order for large efforts.

Speak in milestones and dependency graphs.
Use the insert_ling tool repeatedly to break a large task into mutiple small ones.

CRITICAL RULES:
- If the Ruler's intent is ambiguous, invoke Zhengming—do not guess
- Each ling must be atomic, clear, and testable
- Dependencies must form a directed acyclic graph (no cycles)
- Dependencies must use exact ling_id values (e.g. '74183c66ba0507ba') as returned by insert_ling. Never use shorthand like '470-1' — the DAG resolver only matches full ling_ids.
- You have read/write on ling; read-only on edicts and filesystem
- Break complex tasks ito multiple lings when possible`

// Strategist decomposes edicts into executable ling (令, task orders)
type Strategist struct {
	*MinisterBase // embedded base for database access and session creation
}

// NewStrategist creates a new Strategist minister
func NewStrategist(base *MinisterBase) *Strategist {
	base.ministerID = "strategist"
	s := &Strategist{
		MinisterBase: base,
	}
	s.self = s
	s.SetPostTaskHook(s.validateDependenciesHook)
	return s
}

// SystemPrompt returns the Strategist's system prompt template.
func (s *Strategist) SystemPrompt() string {
	return StrategistRole
}

// Tools returns the Strategist's LLM tools for interactive sessions
func (s *Strategist) Tools() []Tool {
	if s.toolRegistry != nil {
		perm, _ := tools.ParsePermissions("r-----rwx")
		registered := s.toolRegistry.ForPermissions("strategist", perm)
		result := make([]Tool, len(registered))
		for i, t := range registered {
			result[i] = t
		}
		return result
	}
	// Fallback: legacy tool list when registry is not yet wired
	toolList := []Tool{
		&InsertLingTool{strategist: s},
		&ListLingTool{strategist: s},
		&UpdateLingStatusTool{strategist: s},
		tools.RequestZhengmingTool{MinisterID: s.ministerID, Requester: s, WaitForAnswer: s.WaitForZhengming, Username: s.Username(), Project: s.Project()},
	}
	for _, t := range tools.GetROTools(s.config.LLM, s.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	return toolList
}

// --- Database Methods ---

// InsertLing creates a new task order for an edict
func (s *Strategist) InsertLing(ling *storage.Ling) error {
	// Generate ling ID with random component to ensure uniqueness
	if ling.LingID == "" {
		ling.LingID = GenerateID("ling", fmt.Sprintf("%d", ling.EdictID), ling.Username, ling.Project,
			ling.Description, time.Now().String(), fmt.Sprintf("%d", rand.Int63()))
	}

	if err := s.db.Create(ling).Error; err != nil {
		return fmt.Errorf("failed to insert ling: %w", err)
	}
	return nil
}

// GetLingForEdict retrieves all ling for an edict
func (s *Strategist) GetLingForEdict(key storage.EdictKey) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := s.db.Where("edict_id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).
		Order("created_at ASC").
		Find(&ling).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get ling: %w", err)
	}
	return ling, nil
}

// LingExistsForEdict checks if any ling exists for an edict
func (s *Strategist) LingExistsForEdict(key storage.EdictKey) (bool, error) {
	var count int64
	err := s.db.Model(&storage.Ling{}).
		Where("edict_id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check ling existence: %w", err)
	}
	return count > 0, nil
}

// validateDependenciesHook is the PostTaskHook wrapper for validateDependencies.
func (s *Strategist) validateDependenciesHook(ctx context.Context, task *Task, session *Session, output string) (string, error) {
	lingList, err := s.GetLingForEdict(task.EdictKey)
	if err == nil && len(lingList) > 0 {
		if err := s.validateDependencies(lingList); err != nil {
			return "", fmt.Errorf("invalid dependencies: %w", err)
		}
	}
	return "", nil
}

// validateDependencies ensures ling form a DAG (no cycles)
func (s *Strategist) validateDependencies(lingList []storage.Ling) error {
	// Build adjacency map
	deps := make(map[string][]string)
	for _, ling := range lingList {
		deps[ling.LingID] = ling.Dependencies
	}

	// Check for cycles using DFS
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var hasCycle func(id string) bool
	hasCycle = func(id string) bool {
		visited[id] = true
		inStack[id] = true

		for _, dep := range deps[id] {
			if !visited[dep] {
				if hasCycle(dep) {
					return true
				}
			} else if inStack[dep] {
				return true
			}
		}

		inStack[id] = false
		return false
	}

	for _, ling := range lingList {
		if !visited[ling.LingID] {
			if hasCycle(ling.LingID) {
				return fmt.Errorf("circular dependency detected involving ling %s", ling.LingID)
			}
		}
	}

	return nil
}

// Run starts the Strategist's task processing loop
func (s *Strategist) Run(ctx context.Context) {
	s.RunLoop(ctx, s, nil, s.MinisterBase.processTask)
}

// --- Strategist Tools ---

// InsertLingTool creates a new ling (task order) for an edict
type InsertLingTool struct {
	strategist *Strategist
}

func (t *InsertLingTool) Name() string { return "insert_ling" }

func (t *InsertLingTool) Description() string {
	return "Creates a new ling (task order) for an edict. The input should be a JSON object with 'edict_id', 'description', and optionally 'dependencies' (array of FULL ling IDs, e.g. '74183c66ba0507ba', that must complete first — never use shorthand aliases like '470-1')."
}

func (t *InsertLingTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID      uint     `json:"edict_id"`
		Description  string   `json:"description"`
		Dependencies []string `json:"dependencies,omitempty"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Description == "" {
		return "", fmt.Errorf("description is required")
	}

	ling := &storage.Ling{
		EdictID:      params.EdictID,
		Username:     t.strategist.Username(),
		Project:      t.strategist.Project(),
		Description:  params.Description,
		Dependencies: storage.StringArray(params.Dependencies),
		Status:       storage.LingPending,
	}

	if err := t.strategist.InsertLing(ling); err != nil {
		return "", err
	}

	return fmt.Sprintf("Created ling %s for edict %d", ling.LingID, params.EdictID), nil
}

func (t *InsertLingTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "integer",
				"description": "The edict ID this ling belongs to",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Clear, atomic description of the task",
			},
			"dependencies": map[string]any{
				"type":        "array",
				"description": "Array of FULL ling IDs (e.g. '74183c66ba0507ba') that must complete before this one. Use the exact ling_id returned by insert_ling — never invent shorthand aliases like '470-1'.",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"edict_id", "description"},
	}
}

func (t *InsertLingTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Insert Ling: Error: %v\n", err)
	}
	return fmt.Sprintf("Insert Ling: %s\n", result)
}

// ListLingTool lists all ling for an edict
type ListLingTool struct {
	strategist *Strategist
}

func (t *ListLingTool) Name() string { return "list_ling" }

func (t *ListLingTool) Description() string {
	return "Lists all ling (task orders) for an edict. The input should be a JSON object with 'edict_id'."
}

func (t *ListLingTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID uint `json:"edict_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}

	key := storage.EdictKey{ID: params.EdictID, Username: t.strategist.Username(), Project: t.strategist.Project()}
	lingList, err := t.strategist.GetLingForEdict(key)
	if err != nil {
		return "", err
	}

	if len(lingList) == 0 {
		return "No ling found for this edict", nil
	}

	result, err := json.MarshalIndent(lingList, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to format ling list: %w", err)
	}
	return string(result), nil
}

func (t *ListLingTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "integer",
				"description": "The edict ID to list ling for",
			},
		},
		"required": []string{"edict_id"},
	}
}

func (t *ListLingTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("List Ling: Error: %v\n", err)
	}
	return fmt.Sprintf("List Ling: %s\n", result)
}

// UpdateLingStatusTool updates the status of a ling
type UpdateLingStatusTool struct {
	strategist *Strategist
}

func (t *UpdateLingStatusTool) Name() string { return "update_ling_status" }

func (t *UpdateLingStatusTool) Description() string {
	return "Updates the status of a ling. Valid statuses: pending, in_progress, completed, blocked. The input should be a JSON object with 'ling_id' and 'status'."
}

func (t *UpdateLingStatusTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		LingID string `json:"ling_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.LingID == "" {
		return "", fmt.Errorf("ling_id is required")
	}
	if params.Status == "" {
		return "", fmt.Errorf("status is required")
	}

	// Validate status
	validStatuses := map[string]storage.LingStatus{
		"pending":     storage.LingPending,
		"in_progress": storage.LingInProgress,
		"completed":   storage.LingDone,
		"blocked":     storage.LingBlocked,
	}
	newStatus, ok := validStatuses[params.Status]
	if !ok {
		return "", fmt.Errorf("invalid status: %s (valid: pending, in_progress, completed, blocked)", params.Status)
	}

	result := t.strategist.db.Model(&storage.Ling{}).
		Where("ling_id = ?", params.LingID).
		Update("status", newStatus)
	if result.Error != nil {
		return "", fmt.Errorf("failed to update ling status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("ling not found: %s", params.LingID)
	}

	return fmt.Sprintf("Updated ling %s status to %s", params.LingID, params.Status), nil
}

func (t *UpdateLingStatusTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ling_id": map[string]any{
				"type":        "string",
				"description": "The ling ID to update",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "New status: pending, in_progress, completed, or blocked",
				"enum":        []string{"pending", "in_progress", "completed", "blocked"},
			},
		},
		"required": []string{"ling_id", "status"},
	}
}

func (t *UpdateLingStatusTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Update Ling Status: Error: %v\n", err)
	}
	return fmt.Sprintf("Update Ling Status: %s\n", result)
}
