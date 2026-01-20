package shogunate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// strategistConn implements StrategistConn - decomposes edicts into ling
type strategistConn struct {
	baseConn
}

// NewStrategistConn creates a new Strategist connection
func NewStrategistConn(db *gorm.DB) StrategistConn {
	return &strategistConn{
		baseConn: baseConn{db: db, ministerID: "strategist"},
	}
}

// GetEdict retrieves an edict by ID
func (c *strategistConn) GetEdict(edictID string) (*storage.Edict, error) {
	return c.getEdict(edictID)
}

// InsertLing creates a new task order for an edict
func (c *strategistConn) InsertLing(ling *storage.Ling) error {
	// Generate idempotency key if not set
	if ling.IdempotencyKey == "" {
		var edict storage.Edict
		if err := c.db.First(&edict, "edict_id = ?", ling.EdictID).Error; err != nil {
			return fmt.Errorf("failed to get edict for idempotency key: %w", err)
		}
		ling.IdempotencyKey = generateIdempotencyKey(
			ling.EdictID,
			fmt.Sprintf("%d", edict.RenIntentVersion),
			ling.Description,
		)
	}

	// Generate ling ID if not set
	if ling.LingID == "" {
		ling.LingID = GenerateID("ling", ling.EdictID, ling.Description)
	}

	if err := c.db.Create(ling).Error; err != nil {
		return fmt.Errorf("failed to insert ling: %w", err)
	}
	return nil
}

// GetLingForEdict retrieves all ling for an edict
func (c *strategistConn) GetLingForEdict(edictID string) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := c.db.Where("edict_id = ?", edictID).
		Order("created_at ASC").
		Find(&ling).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get ling: %w", err)
	}
	return ling, nil
}

// LingExistsForEdict checks if any ling exists for an edict
func (c *strategistConn) LingExistsForEdict(edictID string) (bool, error) {
	var count int64
	err := c.db.Model(&storage.Ling{}).
		Where("edict_id = ?", edictID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check ling existence: %w", err)
	}
	return count > 0, nil
}

// GetEdictsInPlanningPhase returns edicts that need planning
func (c *strategistConn) GetEdictsInPlanningPhase() ([]storage.Edict, error) {
	var edicts []storage.Edict
	err := c.db.Where("current_phase = ?", storage.PhasePlanning).
		Order("created_at ASC").
		Find(&edicts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get planning edicts: %w", err)
	}
	return edicts, nil
}

// --- Minister ---

// StrategistPrompt defines the Strategist's identity and capabilities
const StrategistPrompt = `You are the Strategist (兵部, Bīngbù). Your domain is strategy and sequence.

When the Ritual Guard summons you for Planning, you decompose the edict into executable ling (令, task orders) with clear dependencies. You enforce temporal order: no forging until planning is complete.

Speak in milestones and dependency graphs. You are the planner of the court.

# Tools

## Shogunate Tools
- **insert_ling**: Create a new task order (ling) with description and dependencies
- **list_ling**: List all ling for an edict to see current decomposition
- **update_ling_status**: Update ling status (pending, in_progress, completed, blocked)
- **request_zhengming**: Ask the Ruler for clarification when requirements are ambiguous

## Standard Tools (read-only access)
- **read_file**: Read file contents to understand existing code
- **list_directory**: Explore project structure
- **read_many_files**: Read multiple files at once for context

CRITICAL RULES:
- If the Ruler's intent is ambiguous, invoke Zhengming—do not guess
- Each ling must be atomic, clear, and testable
- Dependencies must form a directed acyclic graph (no cycles)
- You have read/write on ling; read-only on edicts and filesystem
- Break complex tasks into 3-7 ling when possible`

// Strategist decomposes edicts into executable ling (令, task orders)
type Strategist struct {
	MinisterBase          // embedded base for session creation
	conn         StrategistConn
	llmClient    LLMClient // local LLM client for planning (distinct from MinisterBase.llm)
}

// NewStrategist creates a new Strategist minister
func NewStrategist(conn StrategistConn, llm LLMClient, logger *slog.Logger) *Strategist {
	if logger == nil {
		logger = slog.Default()
	}
	return &Strategist{
		MinisterBase: MinisterBase{logger: logger},
		conn:         conn,
		llmClient:    llm,
	}
}

// ID returns the minister identifier
func (s *Strategist) ID() string {
	return "strategist"
}

// Role returns the Strategist's role identity text
func (s *Strategist) Role() string {
	return StrategistPrompt
}

// Tools returns the Strategist's LLM tools for interactive sessions
func (s *Strategist) Tools(notify NotifyFunc) []Tool {
	// TODO: Implement Strategist tools (insert_ling, list_ling, update_ling_status)
	return []Tool{}
}

// Execute runs the Strategist's planning logic for an edict
func (s *Strategist) Execute(ctx context.Context, edictID string) (bool, error) {
	// Check if ling already exist (idempotency)
	exists, err := s.conn.LingExistsForEdict(edictID)
	if err != nil {
		return false, fmt.Errorf("check existing ling: %w", err)
	}
	if exists {
		s.logger.Info("ling already exist, phase sealed", "edict_id", edictID)
		return true, nil
	}

	// Get the edict
	edict, err := s.conn.GetEdict(edictID)
	if err != nil {
		return false, fmt.Errorf("get edict: %w", err)
	}

	// Check for ambiguity
	if s.isAmbiguous(edict.RenIntent) {
		_, err := s.conn.RequestZhengming(edictID,
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
		if err := s.conn.InsertLing(&ling); err != nil {
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
	if s.llm == nil {
		// Fallback: create a single ling for the whole edict
		return []storage.Ling{{
			EdictID:     edict.EdictID,
			Description: edict.RenIntent,
			Status:      storage.LingPending,
		}}, nil
	}

	// Use LLM to decompose
	prompt := fmt.Sprintf("Decompose this task into 3-7 atomic, testable steps:\n\n%s", edict.RenIntent)
	response, err := s.llmClient.Generate(ctx, StrategistPrompt, prompt)
	if err != nil {
		s.logger.Warn("LLM decomposition failed, using fallback", "error", err)
		return []storage.Ling{{
			EdictID:     edict.EdictID,
			Description: edict.RenIntent,
			Status:      storage.LingPending,
		}}, nil
	}

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

// Run starts the Strategist's polling loop for edicts in planning phase
func (s *Strategist) Run(ctx context.Context, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	s.logger.Info("strategist started", "poll_interval", pollInterval)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("strategist stopped")
			return
		case <-ticker.C:
			s.pollAndExecute(ctx)
		}
	}
}

// pollAndExecute checks for edicts needing planning and processes them
func (s *Strategist) pollAndExecute(ctx context.Context) {
	edicts, err := s.conn.GetEdictsInPlanningPhase()
	if err != nil {
		s.logger.Error("failed to poll planning edicts", "error", err)
		return
	}

	for _, edict := range edicts {
		// Check for pending zhengming before processing
		pending, err := s.conn.IsZhengmingPending(edict.EdictID)
		if err != nil {
			s.logger.Error("failed to check zhengming", "edict_id", edict.EdictID, "error", err)
			continue
		}
		if pending {
			continue
		}

		sealed, err := s.Execute(ctx, edict.EdictID)
		if err != nil {
			s.logger.Error("failed to execute planning", "edict_id", edict.EdictID, "error", err)
			continue
		}
		if sealed {
			// Transition to forging phase
			if err := s.conn.UpdatePhase(edict.EdictID, storage.PhaseForging); err != nil {
				s.logger.Error("failed to transition to forging", "edict_id", edict.EdictID, "error", err)
				continue
			}
			s.logger.Info("planning phase sealed, transitioning to forging", "edict_id", edict.EdictID)
		}
	}
}
