package shogunate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
)

// StrategistRole defines the Strategist's identity and capabilities
const StrategistRole = `You are the Strategist (兵部, Bīngbù) and the planner of the shogunate.
Your domain is strategy and sequence.

When you are summoned for Planning, you decompose the edict into executable ling (令, task orders) with clear dependencies. You enforce temporal order: no forging until planning is complete.

Speak in milestones and dependency graphs.
Use the insert_ling tool repeatedly to break a large task into mutiple small ones.

CRITICAL RULES:
- If the Ruler's intent is ambiguous, invoke Zhengming—do not guess
- Each ling must be atomic, clear, and testable
- Dependencies must form a directed acyclic graph (no cycles)
- You have read/write on ling; read-only on edicts and filesystem
- Break complex tasks ito multiple lings when possible`

// Strategist decomposes edicts into executable ling (令, task orders)
type Strategist struct {
	MinisterBase // embedded base for database access and session creation
	tasks        chan *Task
}

// NewStrategist creates a new Strategist minister
func NewStrategist(base MinisterBase) *Strategist {
	base.ministerID = "strategist"
	return &Strategist{
		MinisterBase: base,
		tasks:        make(chan *Task, 10),
	}
}

// Tasks returns the channel for task submission
func (s *Strategist) Tasks() chan<- *Task {
	return s.tasks
}

// ID returns the minister identifier
func (s *Strategist) ID() string {
	return "strategist"
}

// SystemPrompt returns the Strategist's system prompt template.
func (s *Strategist) SystemPrompt() string {
	return StrategistRole
}

// Tools returns the Strategist's LLM tools for interactive sessions
func (s *Strategist) Tools() []Tool {
	toolList := []Tool{
		// Ling management tools
		&InsertLingTool{strategist: s},
		&ListLingTool{strategist: s},
		&UpdateLingStatusTool{strategist: s},
	}
	// Add read-only file tools
	for _, t := range tools.GetROTools() {
		toolList = append(toolList, t)
	}
	return toolList
}

// --- Database Methods ---

// InsertLing creates a new task order for an edict
func (s *Strategist) InsertLing(ling *storage.Ling) error {
	// Generate ling ID if not set
	if ling.LingID == "" {
		ling.LingID = GenerateID("ling", ling.EdictID, ling.Description)
	}

	if err := s.db.Create(ling).Error; err != nil {
		return fmt.Errorf("failed to insert ling: %w", err)
	}
	return nil
}

// GetLingForEdict retrieves all ling for an edict
func (s *Strategist) GetLingForEdict(edictID string) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := s.db.Where("edict_id = ?", edictID).
		Order("created_at ASC").
		Find(&ling).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get ling: %w", err)
	}
	return ling, nil
}

// LingExistsForEdict checks if any ling exists for an edict
func (s *Strategist) LingExistsForEdict(edictID string) (bool, error) {
	var count int64
	err := s.db.Model(&storage.Ling{}).
		Where("edict_id = ?", edictID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check ling existence: %w", err)
	}
	return count > 0, nil
}

// GetEdictsInPlanningPhase returns edicts that need planning
func (s *Strategist) GetEdictsInPlanningPhase() ([]storage.Edict, error) {
	var edicts []storage.Edict
	err := s.db.Where("current_phase = ?", storage.PhasePlanning).
		Order("created_at ASC").
		Find(&edicts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get planning edicts: %w", err)
	}
	return edicts, nil
}

// --- Execute Logic ---

// execute runs the Strategist's planning logic for an edict (internal method)
func (s *Strategist) execute(ctx context.Context, edictID string) (bool, error) {
	// Check if ling already exist (idempotency)
	exists, err := s.LingExistsForEdict(edictID)
	if err != nil {
		return false, fmt.Errorf("check existing ling: %w", err)
	}
	if exists {
		s.logger.Info("ling already exist, phase sealed", "edict_id", edictID)
		return true, nil
	}

	// Get the edict
	edict, err := s.GetEdict(edictID)
	if err != nil {
		return false, fmt.Errorf("get edict: %w", err)
	}

	// Check for ambiguity
	if s.isAmbiguous(edict.Intent) {
		_, err := s.RequestZhengming(edictID,
			"The requirements are ambiguous. Please clarify the expected behavior.",
			storage.PriorityUrgent)
		if err != nil {
			return false, fmt.Errorf("request zhengming: %w", err)
		}
		return false, nil
	}

	// Decompose into ling
	lingList, err := s.decompose(ctx, edict)
	if err != nil {
		return false, fmt.Errorf("decompose: %w", err)
	}

	// Validate dependencies form a DAG
	if err := s.validateDependencies(lingList); err != nil {
		return false, fmt.Errorf("invalid dependencies: %w", err)
	}

	// Insert ling
	for _, ling := range lingList {
		if err := s.InsertLing(&ling); err != nil {
			return false, fmt.Errorf("insert ling: %w", err)
		}
	}

	s.logger.Info("planning complete", "edict_id", edictID, "ling_count", len(lingList))
	return true, nil
}

// isAmbiguous checks if the intent has obvious ambiguity markers
func (s *Strategist) isAmbiguous(intent string) bool {
	// Simple heuristic: very short intents are likely ambiguous
	return len(intent) < 20
}

// decompose breaks down an edict into executable ling
func (s *Strategist) decompose(ctx context.Context, edict *storage.Edict) ([]storage.Ling, error) {
	if s.model == nil {
		// Fallback: create a single ling for the whole edict
		return []storage.Ling{{
			EdictID:     edict.EdictID,
			Description: edict.Intent,
			Status:      storage.LingPending,
		}}, nil
	}

	// Use LLM to decompose
	prompt := fmt.Sprintf("Decompose this task into 3-7 atomic, testable steps:\n\n%s", edict.Intent)
	messages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, StrategistRole),
		llms.TextParts(llms.ChatMessageTypeHuman, prompt),
	}
	resp, err := s.model.GenerateContent(ctx, messages)
	if err != nil {
		s.logger.Warn("LLM decomposition failed, using fallback", "error", err)
		return []storage.Ling{{
			EdictID:     edict.EdictID,
			Description: edict.Intent,
			Status:      storage.LingPending,
		}}, nil
	}
	response := resp.Choices[0].Content

	// Parse LLM response into ling
	// For now, treat response as a single ling
	return []storage.Ling{{
		EdictID:     edict.EdictID,
		Description: response,
		Status:      storage.LingPending,
	}}, nil
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
	s.logger.Info("strategist started, awaiting tasks")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("strategist stopped")
			return
		case task := <-s.tasks:
			s.processTask(ctx, task)
		}
	}
}

// processTask handles a single task
func (s *Strategist) processTask(ctx context.Context, task *Task) {
	s.logger.Info("strategist processing task",
		"edict_id", task.EdictID,
		"work", task.Work)

	// Execute the planning logic
	sealed, err := s.execute(ctx, task.EdictID)

	// Send result back to Chancellor
	result := Result{
		MinisterID: s.ID(),
		Sealed:     sealed,
		Err:        err,
	}

	if sealed {
		result.Output = "planning complete"
		// Transition to forging phase
		if phaseErr := s.UpdatePhase(task.EdictID, storage.PhaseForging); phaseErr != nil {
			s.logger.Error("failed to transition to forging", "edict_id", task.EdictID, "error", phaseErr)
		}
	}

	// Send result (non-blocking)
	select {
	case task.Done <- result:
	default:
		s.logger.Warn("done channel full, dropping result", "edict_id", task.EdictID)
	}
}

// --- Strategist Tools ---

// InsertLingTool creates a new ling (task order) for an edict
type InsertLingTool struct {
	strategist *Strategist
}

func (t *InsertLingTool) Name() string { return "insert_ling" }

func (t *InsertLingTool) Description() string {
	return "Creates a new ling (task order) for an edict. The input should be a JSON object with 'edict_id', 'description', and optionally 'dependencies' (array of ling IDs that must complete first)."
}

func (t *InsertLingTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID      string   `json:"edict_id"`
		Description  string   `json:"description"`
		Dependencies []string `json:"dependencies,omitempty"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Description == "" {
		return "", fmt.Errorf("description is required")
	}

	ling := &storage.Ling{
		EdictID:      params.EdictID,
		Description:  params.Description,
		Dependencies: storage.StringArray(params.Dependencies),
		Status:       storage.LingPending,
	}

	if err := t.strategist.InsertLing(ling); err != nil {
		return "", err
	}

	return fmt.Sprintf("Created ling %s for edict %s", ling.LingID, params.EdictID), nil
}

func (t *InsertLingTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "string",
				"description": "The edict ID this ling belongs to",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Clear, atomic description of the task",
			},
			"dependencies": map[string]any{
				"type":        "array",
				"description": "Array of ling IDs that must complete before this one",
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
		EdictID string `json:"edict_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}

	lingList, err := t.strategist.GetLingForEdict(params.EdictID)
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
				"type":        "string",
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
