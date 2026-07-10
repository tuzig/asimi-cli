package shogunate

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
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
	c := &Chancellor{
		MinisterBase: base,
	}
	// Set the prompt preprocessor so ProcessPrompt handles edict-specific prep
	c.promptPreprocessor = c.preprocessPrompt
	return c
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
		RepoInfo:   &repo.RepoInfo{},
		MinisterID: c.ministerID,
		Username:   c.username,
		Project:    c.project,
		DB:         c.db,
	}
	*tc.RepoInfo = c.RepoInfo()
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

// Run listens for prompts from the Ruler, tasks from ministers, and events from the Shogunate.
// processTask is nil so RunLoop uses MinisterBase.processTask as the default.
func (c *Chancellor) Run(ctx context.Context) {
	c.RunLoop(ctx, c, c.processPrompt, nil)
}

// SetShogunate sets the Shogunate reference for minister access
func (c *Chancellor) SetShogunate(s *Shogunate) {
	c.shogunate = s
}

// --- Edict Management ---

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

// preprocessPrompt is the Chancellor's PromptPreprocessor hook.
// For edict-bound prompts (key.ID != 0) it appends the message to the edict
// intent and prepends a [Context: edict N] prefix. For ruling-session prompts
// (key.ID == 0) it returns the message unchanged.
func (c *Chancellor) preprocessPrompt(key storage.EdictKey, message string) string {
	if key.ID != 0 {
		if err := c.AppendToIntent(key, message); err != nil {
			c.logger.Warn("failed to append to intent", "edict_id", key.ID, "error", err)
		}
		return fmt.Sprintf("[Context: edict %d]\n\n%s", key.ID, message)
	}
	return message
}

// processPrompt handles a single prompt from the Ruler.
// The preprocessor hook handles edict-specific preprocessing; the actual
// session creation and streaming is delegated to MinisterBase.ProcessPrompt.
func (c *Chancellor) processPrompt(ctx context.Context, prompt *Prompt) {
	c.ProcessPrompt(ctx, c, prompt)
}

// handleZhengmingAnswered resumes the chancellor's work on an edict after clarification
func (c *Chancellor) handleZhengmingAnswered(ctx context.Context, key storage.EdictKey, payload map[string]interface{}) {
	answer, _ := payload["answer"].(string)
	if key.ID == 0 || answer == "" {
		return
	}
	c.logger.Info("handling zhengming answered", "edict_id", key.ID, "answer", answer)

	edict, err := c.MinisterBase.GetEdict(key)
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
