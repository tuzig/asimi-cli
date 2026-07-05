package shogunate

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
	"github.com/maximhq/bifrost/core/schemas"
	"gorm.io/gorm"
)

//go:embed context/chancellor.md
var role string

// Chancellor harmonizes all ministers and manages edict lifecycle
type Chancellor struct {
	*MinisterBase // embedded base provides db, llm, config, repoInfo, logger, session
	shogunate     *Shogunate
}

// NewChancellor creates a new Chancellor minister
func NewChancellor(base *MinisterBase) *Chancellor {
	base.ministerID = "chancellor"
	return &Chancellor{
		MinisterBase: base,
	}
}

// ID returns the minister identifier
func (c *Chancellor) ID() string {
	return "chancellor"
}

// Title returns the minister's honorific title
func (c *Chancellor) Title() string { return "Chancellor" }

// SystemPrompt returns the Chancellor's system prompt template.
func (c *Chancellor) SystemPrompt() string {
	return role
}

// Scratchpad returns dynamic context for the Chancellor including available rituals and rules
func (c *Chancellor) Scratchpad() string {
	var b strings.Builder

	b.WriteString("# Available Rituals\n")
	if c.shogunate == nil || c.shogunate.GetRitualRegistry() == nil {
		b.WriteString("None loaded\n")
	} else {
		names := c.shogunate.GetRitualRegistry().List()
		if len(names) == 0 {
			b.WriteString("None loaded\n")
		} else {
			for _, name := range names {
				ritual := c.shogunate.GetRitualRegistry().Get(name)
				if ritual != nil {
					b.WriteString(fmt.Sprintf("- %s: %s\n", name, ritual.Description))
				}
			}
		}
	}

	b.WriteString("\n# Critical Rules\n")
	b.WriteString("- Size the edict (S, M, L, XL) and invoke the appropriate ritual\n")
	b.WriteString("- Use swift-strike for all code changes\n")
	b.WriteString("- castle-siege is reserved for explicit ruler invocation only — do not auto-select it\n")
	b.WriteString("- Use invoke_minister for ad-hoc tasks not covered by rituals\n")
	b.WriteString("- When ambiguity threatens progress, invoke Zhengming immediately\n")
	b.WriteString("- Never guess at requirements—always clarify\n")

	return b.String()
}

// Tools returns the Chancellor's LLM tools for interactive sessions
func (c *Chancellor) Tools() []Tool {
	tc := tools.ToolContext{
		Username: c.username,
		Project:  c.project,
	}
	toolList := []Tool{
		tools.AsimiSQLTool{DBPath: c.getDBPath(), ProjectRoot: c.RepoInfo().ProjectRoot},
		tools.UpdateEdictTool{Manager: c, Username: c.username, Project: c.project},
		tools.RequestZhengmingTool{MinisterID: c.ministerID, Requester: c, WaitForAnswer: c.WaitForZhengming, Username: c.username, Project: c.project},
		tools.GetEdictStatusTool{Manager: c, DB: c.db, Username: c.username, Project: c.project},
		tools.ListEdictsTool{DB: c.db, Username: c.username, Project: c.project},
		tools.TransitionEdictTool{DB: c.db, Username: c.username, Project: c.project},
		tools.InvokeMinisterTool{Ctx: tc, Invoker: c},
	}
	// Add read-only file tools
	for _, t := range tools.GetROTools(c.config.LLM, c.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	// Add InvokeRitualTool if ritual runner is available
	if c.shogunate != nil && c.shogunate.GetRitualRunner() != nil {
		toolList = append(toolList, tools.InvokeRitualTool{Ctx: tc, Launcher: c})
	}
	toolList = append(toolList, tools.NewRunShellCommand(c.CheckHostCommand, c.runner, c.msgChan, c.RepoInfo().ProjectRoot))
	return toolList
}

// getLastStepOutput returns the full output of the last step that produced a message.
func getLastStepOutput(exec *RitualExecution) string {
	if out, ok := exec.Data["act_result"].(string); ok {
		return out
	}
	slog.Debug("act_result is not a string", "act_result", fmt.Sprintf("%v", exec.Data["act_result"]))
	return ""
}

// --- Interface implementations for tools package ---

// RunRitual runs a ritual synchronously, blocking until completion or failure.
// Uses the caller's ctx so CTRL-C propagates properly.
func (c *Chancellor) RunRitual(ctx context.Context, ritualName string, key storage.EdictKey, inputs map[string]string) (*RitualExecution, error) {
	if c.shogunate == nil || c.shogunate.GetRitualRunner() == nil {
		return nil, fmt.Errorf("ritual runner not available")
	}

	exec, err := c.shogunate.GetRitualRunner().Start(ctx, ritualName, key, inputs, c.notify)
	if err != nil {
		return nil, err
	}

	// Run synchronously — blocks until completion/failure/cancellation
	if err := c.shogunate.GetRitualRunner().Run(ctx, exec); err != nil {
		return exec, fmt.Errorf("ritual %s failed: %w", ritualName, err)
	}
	return exec, nil
}

// getDBPath extracts the database file path from gorm.DB using PRAGMA database_list
func (c *Chancellor) getDBPath() string {
	if c.db == nil {
		return ""
	}
	var file string
	// PRAGMA database_list returns: seq, name, file
	row := c.db.Raw("PRAGMA database_list").Row()
	var seq int
	var name string
	if err := row.Scan(&seq, &name, &file); err != nil {
		c.logger.Warn("failed to get database path", "error", err)
		return ""
	}
	return file
}

// Run listens for prompts from the Ruler, tasks from ministers, and events from the Shogunate
func (c *Chancellor) Run(ctx context.Context) {
	c.RunLoop(ctx, c, c.processPrompt, c.processTask)
}

// SetShogunate sets the Shogunate reference for minister access
func (c *Chancellor) SetShogunate(s *Shogunate) {
	c.shogunate = s
}

// SubscribeToEvents registers the Chancellor's event handlers directly with the RitualGuard.
func (c *Chancellor) SubscribeToEvents(rg *RitualGuard) {
	rg.Subscribe(storage.EventRitualCompleted, func(e Event) {
		c.handleRitualCompleted(c.shogunate.ctx, e.EdictKey, e.Payload)
	})
	rg.Subscribe(storage.EventRitualFailed, func(e Event) {
		c.handleRitualFailed(c.shogunate.ctx, e.EdictKey, e.Payload)
	})
}

// ResetSession clears the Chancellor's session (delegates to MinisterBase)
func (c *Chancellor) ResetSession() {
	c.MinisterBase.ResetSession()
}

// RestoreSession creates a fully-wired interactive session and injects loaded history
func (c *Chancellor) RestoreSession(minister Minister, msgs []schemas.ChatMessage) error {
	return c.MinisterBase.restoreSession(minister, msgs)
}

// --- Edict Management ---

// GetEdict retrieves an edict by ID
func (c *Chancellor) GetEdict(key storage.EdictKey) (*storage.Edict, error) {
	var edict storage.Edict
	if err := c.db.First(&edict, "id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).Error; err != nil {
		c.logger.Warn("Edict not found", "key", key)
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("edict %d not found", key.ID)
		}
		return nil, fmt.Errorf("failed to get edict: %w", err)
	}
	return &edict, nil
}

// SetChancellorSeal sets or clears the Chancellor's seal on an edict
func (c *Chancellor) SetChancellorSeal(key storage.EdictKey, sealed bool) error {
	result := c.db.Model(&storage.Edict{}).
		Where("id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).
		Update("chancellor_seal", sealed)
	if result.Error != nil {
		return fmt.Errorf("failed to set chancellor seal: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %d", key.ID)
	}
	return nil
}

// SetCensorSeal sets or clears the Censor's seal on an edict
func (c *Chancellor) SetCensorSeal(key storage.EdictKey, sealed bool) error {
	result := c.db.Model(&storage.Edict{}).
		Where("id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).
		Update("censor_seal", sealed)
	if result.Error != nil {
		return fmt.Errorf("failed to set censor seal: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %d", key.ID)
	}
	return nil
}

// CancelEdict marks an edict as cancelled
func (c *Chancellor) CancelEdict(key storage.EdictKey, cancelledBy, reason string) error {
	now := time.Now()
	result := c.db.Model(&storage.Edict{}).
		Where("id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).
		Update("cancelled_at", now)
	if result.Error != nil {
		return fmt.Errorf("failed to cancel edict: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %d", key.ID)
	}
	return nil
}

// --- Zhengming (Clarification) Management ---

// GetPendingZhengming retrieves all pending clarification requests for an edict
func (c *Chancellor) GetPendingZhengming(key storage.EdictKey) ([]storage.Zhengming, error) {
	var requests []storage.Zhengming
	query := c.db.Where("status = ?", storage.ZhengmingPending).Order("created_at ASC")
	if key.ID != 0 {
		query = query.Where("edict_id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project)
	}
	if err := query.Find(&requests).Error; err != nil {
		return nil, fmt.Errorf("failed to get pending zhengming: %w", err)
	}
	return requests, nil
}

// --- Manifest and Ling Management ---

// GetAllManifestsForEdict retrieves all manifests for an edict (Chancellor privilege)
func (c *Chancellor) GetAllManifestsForEdict(key storage.EdictKey) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := c.db.Where("edict_id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).
		Order("created_at ASC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get manifests: %w", err)
	}
	return manifests, nil
}

// GetAllLingForEdict retrieves all ling for an edict (Chancellor privilege)
func (c *Chancellor) GetAllLingForEdict(key storage.EdictKey) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := c.db.Where("edict_id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).
		Order("created_at ASC").
		Find(&ling).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get ling: %w", err)
	}
	return ling, nil
}

// ResetLingStatus resets a ling's status (for regression handling)
func (c *Chancellor) ResetLingStatus(lingID string, status storage.LingStatus) error {
	result := c.db.Model(&storage.Ling{}).
		Where("ling_id = ?", lingID).
		Update("status", status)
	if result.Error != nil {
		return fmt.Errorf("failed to reset ling status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("ling not found: %s", lingID)
	}
	return nil
}

// CancelEdictWithContext cancels an edict (context-aware variant)
func (c *Chancellor) CancelEdictWithContext(ctx context.Context, key storage.EdictKey, cancelledBy, reason string) error {
	if err := c.CancelEdict(key, cancelledBy, reason); err != nil {
		return err
	}

	c.EmitEvent(key, "edict_cancelled", storage.JSON{
		"cancelled_by": cancelledBy,
		"reason":       reason,
	})

	c.logger.Info("edict cancelled", "edict_id", key.ID, "by", cancelledBy)
	return nil
}

// CheckSandboxHealth verifies the sandbox container is healthy by running uname
// and checking that "Linux" appears in the output. Returns nil if healthy.
func (c *Chancellor) CheckSandboxHealth(ctx context.Context) error {
	if c.shogunate == nil {
		return fmt.Errorf("shogunate not configured")
	}

	runner := c.shogunate.GetRunner()
	if runner == nil {
		return fmt.Errorf("shell runner not available")
	}

	result, err := runner.Run(ctx, runners.Input{
		Command:     "uname",
		Description: "sandbox health check",
	})
	if err != nil {
		return fmt.Errorf("sandbox health check failed: %w", err)
	}
	if result.ExitCode != "0" {
		return fmt.Errorf("sandbox health check failed: uname exited with %s", result.ExitCode)
	}
	if !strings.Contains(result.Output, "Linux") {
		return fmt.Errorf("sandbox health check failed: expected Linux in output, got: %s", result.Output)
	}
	return nil
}

// --- Prompt Processing ---

// processPrompt handles a single prompt from the Ruler
func (c *Chancellor) processPrompt(ctx context.Context, prompt *Prompt) {
	key := prompt.EdictKey
	if key.ID != 0 {
		// Edict-bound prompt — append to intent
		if err := c.AppendToIntent(key, prompt.Message); err != nil {
			c.logger.Warn("failed to append to intent", "edict_id", key.ID, "error", err)
		}
	}
	// When ID == 0, this is a ruling session (edict-free chat).
	// No edict is created — the Chancellor can create one on-demand via tools.

	// Notify TUI of edict ID before streaming begins
	c.notify(StreamStartMsg{ChannelID: "chancellor", EdictID: key.ID})

	// Call LLM with streaming
	c.brewWithStreaming(ctx, key, prompt.Message, prompt.ContextFiles)
}

// brewWithStreaming delegates to Session for LLM interaction
func (c *Chancellor) brewWithStreaming(ctx context.Context, key storage.EdictKey, prompt string, contextFiles map[string]string) {
	if c.client == nil {
		c.notify(StreamErrorMsg{ChannelID: "chancellor", Err: fmt.Errorf("LLM not configured")})
		return
	}

	// Always use session — no per-edict sessions
	if c.session == nil {
		var err error
		c.session, err = CreateSession(c, c.client, c.config, c.notify, "chancellor")
		if err != nil {
			c.notify(StreamErrorMsg{ChannelID: "chancellor", Err: fmt.Errorf("failed to create session: %w", err)})
			return
		}
		c.session.TabType = "chancellor"
		c.session.SetPersister(c.Persister())
		c.logger.Info("chancellor created interactive session")
	}

	c.logger.Debug("context files received from TUI", "count", len(contextFiles))

	// Pass edictID in prompt context, not session
	fullPrompt := prompt
	if key.ID != 0 {
		fullPrompt = fmt.Sprintf("[Context: edict %d]\n\n%s", key.ID, prompt)
	}

	_, err := c.session.AskWithStreaming(ctx, fullPrompt, contextFiles)
	if err != nil && ctx.Err() == nil {
		c.notify(StreamErrorMsg{ChannelID: "chancellor", Err: err})
		return
	}
	c.notify(StreamDoneMsg{ChannelID: "chancellor"})
}

// processTask handles a task from the ritual runner or other ministers.
func (c *Chancellor) processTask(ctx context.Context, task *Task) {
	c.logger.Info("chancellor processing task",
		"edict_id", task.EdictKey.ID,
		"work", truncateString(task.Work, 50))

	var output string
	var taskErr error

	if c.client != nil {
		// Always use session for task conversation
		if c.session == nil {
			var err error
			c.session, err = CreateSession(c, c.client, c.config, c.notify, "chancellor")
			if err != nil {
				taskErr = fmt.Errorf("failed to create session: %w", err)
			} else {
				c.session.TabType = "chancellor"
				c.session.SetPersister(c.Persister())
			}
		}

		if taskErr == nil {
			output, taskErr = c.session.AskWithStreaming(ctx, task.Work, nil)
		}
	} else {
		output = "chancellor task acknowledged (no LLM configured)"
	}

	result := Result{
		MinisterID: c.ID(),
		Sealed:     true,
		Output:     output,
		Err:        taskErr,
	}

	if task.Done != nil {
		select {
		case task.Done <- result:
		default:
			c.logger.Warn("done channel full, dropping result", "edict_id", task.EdictKey.ID)
		}
	}
}

// handleRitualCompleted processes a completed ritual event
func (c *Chancellor) handleRitualCompleted(ctx context.Context, key storage.EdictKey, payload map[string]interface{}) {
	c.logger.Info("handling ritual completed", "edict_id", key.ID, "payload", payload)
	c.logger.Debug("ritual completed - edict may need synthesis", "edict_id", key.ID)
}

// handleZhengmingAnswered resumes the chancellor's work on an edict after clarification
func (c *Chancellor) handleZhengmingAnswered(ctx context.Context, key storage.EdictKey, payload map[string]interface{}) {
	answer, _ := payload["answer"].(string)
	if key.ID == 0 || answer == "" {
		return
	}
	c.logger.Info("handling zhengming answered", "edict_id", key.ID, "answer", answer)

	edict, err := c.GetEdict(key)
	if err != nil {
		c.logger.Error("failed to get edict for zhengming resumption", "edict_id", key.ID, "error", err)
		return
	}

	work := fmt.Sprintf("Resume edict %d: %s\n\nThe clarification has been answered. Continue from where you left off.", key.ID, edict.Intent)
	task := &Task{
		Ctx:      ctx,
		EdictKey: key,
		Work:     work,
		Done:     make(chan Result, 1),
	}

	select {
	case c.tasks <- task:
	default:
		c.logger.Warn("chancellor task channel full", "edict_id", key.ID)
	}
}

// handleRitualFailed processes a failed ritual event
func (c *Chancellor) handleRitualFailed(ctx context.Context, key storage.EdictKey, payload map[string]interface{}) {
	c.logger.Error("handling ritual failed", "edict_id", key.ID, "payload", payload)
	c.logger.Debug("ritual failed - may need zhengming or retry", "edict_id", key.ID)
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
