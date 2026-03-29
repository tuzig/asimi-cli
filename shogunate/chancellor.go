package shogunate

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
	"gorm.io/gorm"
)

//go:embed context/chancellor.md
var role string

// Chancellor harmonizes all ministers and manages edict lifecycle
type Chancellor struct {
	*MinisterBase // embedded base provides db, llm, config, repoInfo, logger
	shogunate     *Shogunate
	taskChan      chan *Task

	// Run() loop fields
	RulingSession *Session // Persistent edict-free session for direct chat
}

// NewChancellor creates a new Chancellor minister
func NewChancellor(base *MinisterBase) *Chancellor {
	base.ministerID = "chancellor"
	return &Chancellor{
		MinisterBase: base,
		taskChan:     make(chan *Task, 10),
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
	b.WriteString("- Use swift-strike for small, focused changes\n")
	b.WriteString("- Use castle-siege for medium sized changes that require planning\n")
	b.WriteString("- Use invoke_minister for ad-hoc tasks not covered by rituals\n")
	b.WriteString("- When ambiguity threatens progress, invoke Zhengming immediately\n")
	b.WriteString("- Never guess at requirements—always clarify\n")

	return b.String()
}

// Tasks returns the channel for submitting Tasks
func (c *Chancellor) Tasks() chan<- *Task { return c.taskChan }

// --- InvokeMinisterTool ---

// InvokeMinisterTool allows the Chancellor to invoke any registered minister for an edict.
type InvokeMinisterTool struct {
	chancellor *Chancellor
}

// MinisterInvokingMsg notifies the user that a minister is being invoked
type MinisterInvokingMsg struct {
	TabID      string
	MinisterID string
	EdictID    uint
	Task       string
}

// MinisterCompletedMsg notifies the user that a minister completed its task
type MinisterCompletedMsg struct {
	TabID      string
	MinisterID string
	EdictID    uint
	Output     string
	Sealed     bool
	Error      error
}

func (t InvokeMinisterTool) Name() string {
	return "invoke_minister"
}

func (t InvokeMinisterTool) Description() string {
	return `Invoke a minister by ID to execute its logic for an edict.
	Ministers process edicts through their specialized phase logic
	(e.g., strategist for planning, forge for code generation, judge for testing and verification, censor for review, marshal for deployment).
	Provide specific task instructions for what the minister should do.`
}

func (t InvokeMinisterTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		MinisterID string `json:"minister_id"`
		EdictID    uint   `json:"edict_id"`
		Work       string `json:"task"` // JSON field is "task" for backwards compatibility
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.MinisterID == "" {
		return "", fmt.Errorf("minister_id is required")
	}
	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Work == "" {
		return "", fmt.Errorf("task is required")
	}

	logger := t.chancellor.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Notify: invoking
	if t.chancellor.notify != nil {
		t.chancellor.notify(MinisterInvokingMsg{
			TabID:      "chancellor",
			MinisterID: params.MinisterID,
			EdictID:    params.EdictID,
			Task:       params.Work,
		})
	}

	// Get minister via Shogunate
	minister := t.chancellor.shogunate.GetMinister(params.MinisterID)
	if minister == nil {
		err := fmt.Errorf("minister not found: %s", params.MinisterID)
		if t.chancellor.notify != nil {
			t.chancellor.notify(MinisterCompletedMsg{
				TabID:      "chancellor",
				MinisterID: params.MinisterID,
				EdictID:    params.EdictID,
				Error:      err,
			})
		}
		return "", fmt.Errorf("minister %s failed: %w", params.MinisterID, err)
	}

	// Create per-call done channel (synchronous blocking pattern)
	doneChan := make(chan Result, 1)

	// Create Task with per-call done channel
	task := &Task{
		EdictID: params.EdictID,
		Work:    params.Work,
		Done:    doneChan,
	}

	// Send task to minister
	timeout := 5 * time.Minute
	select {
	case minister.Tasks() <- task:
		logger.Info("task sent to minister",
			"minister", params.MinisterID,
			"edict_id", params.EdictID,
			"work", truncateString(params.Work, 50))
	case <-ctx.Done():
		return "", fmt.Errorf("minister %s failed: context cancelled while sending task to %s", params.MinisterID, params.MinisterID)
	}

	// Block until minister replies (only blocks this session's goroutine)
	var result Result
	select {
	case result = <-doneChan:
	case <-time.After(timeout):
		err := fmt.Errorf("minister %s timeout after %v", params.MinisterID, timeout)
		if t.chancellor.notify != nil {
			t.chancellor.notify(MinisterCompletedMsg{
				TabID:      "chancellor",
				MinisterID: params.MinisterID,
				EdictID:    params.EdictID,
				Error:      err,
			})
		}
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}

	if result.Err != nil {
		// Notify: failed
		if t.chancellor.notify != nil {
			t.chancellor.notify(MinisterCompletedMsg{
				TabID:      "chancellor",
				MinisterID: params.MinisterID,
				EdictID:    params.EdictID,
				Error:      result.Err,
			})
		}
		logger.Error("task returned error",
			"minister", params.MinisterID,
			"edict_id", params.EdictID,
			"error", result.Err)
		return "", fmt.Errorf("minister %s failed: %w", params.MinisterID, result.Err)
	}

	// Notify: completed
	if t.chancellor.notify != nil {
		t.chancellor.notify(MinisterCompletedMsg{
			TabID:      "chancellor",
			MinisterID: params.MinisterID,
			EdictID:    params.EdictID,
			Output:     params.Work,
			Sealed:     true,
		})
	}

	logger.Info("task completed",
		"minister", params.MinisterID,
		"edict_id", params.EdictID,
		"sealed", result.Sealed,
		"output_len", len(result.Output))

	resultMap := map[string]any{
		"minister_id": params.MinisterID,
		"edict_id":    params.EdictID,
		"status":      "completed",
		"sealed":      result.Sealed,
		"output":      result.Output,
	}
	resultJSON, _ := json.Marshal(resultMap)
	return string(resultJSON), nil
}

func (t InvokeMinisterTool) Format(input, result string, err error) string {
	var params struct {
		MinisterID string `json:"minister_id"`
		Task       string `json:"task"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("InvokeMinister")
	msg.Writef(" %s", params.MinisterID)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		taskPreview := params.Task
		if len(taskPreview) > 30 {
			taskPreview = taskPreview[:27] + "..."
		}
		msg.Writef("[%s]", taskPreview)
	}

	return msg.String() + "\n"
}

func (t InvokeMinisterTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"minister_id": map[string]any{
				"type":        "string",
				"description": "The minister to invoke (strategist, forge, judge, censor or marshal)",
			},
			"edict_id": map[string]any{
				"type":        "integer",
				"description": "The edict ID to process",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Specific instructions for the minister to execute",
			},
		},
		"required": []string{"minister_id", "edict_id", "task"},
	}
}

// InvokeRitualTool starts a YAML-defined ritual workflow
type InvokeRitualTool struct {
	chancellor *Chancellor
}

func (t InvokeRitualTool) Name() string {
	return "enact_ritual"
}

func (t InvokeRitualTool) Description() string {
	return `Execute a YAML-defined ritual workflow for an existing edict. Blocks until the ritual completes or fails.
Rituals are predefined workflows that orchestrate ministers and commands through a series of steps.`
}

func (t InvokeRitualTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		RitualName string            `json:"ritual_name"`
		EdictID    uint              `json:"edict_id"`
		Inputs     map[string]string `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.RitualName == "" {
		return "", fmt.Errorf("ritual_name is required")
	}

	if params.Inputs == nil {
		params.Inputs = make(map[string]string)
	}
	// Add edict_id to inputs for template expansion
	params.Inputs["edict_id"] = fmt.Sprintf("%d", params.EdictID)

	logger := t.chancellor.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Publish EventRitualEnacted for RitualGuard to handle asynchronously
	// Convert inputs to map[string]interface{} so the type is consistent
	// whether the event travels via in-memory channel or DB round-trip.
	inputsPayload := make(map[string]interface{}, len(params.Inputs))
	for k, v := range params.Inputs {
		inputsPayload[k] = v
	}
	payload := storage.JSON{
		"ritual_name": params.RitualName,
		"inputs":      inputsPayload,
	}
	if err := t.chancellor.EmitEvent(params.EdictID, storage.EventRitualEnacted, payload); err != nil {
		logger.Warn("failed to emit ritual_enacted event", "error", err)
	}

	logger.Info("ritual enacted",
		"ritual", params.RitualName,
		"edict_id", params.EdictID)

	result := map[string]any{
		"status":      "enacted and reported. stay quiet and trust the ritual to wake you up when done",
		"ritual_name": params.RitualName,
		"edict_id":    params.EdictID,
	}
	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

func (t InvokeRitualTool) Format(input, result string, err error) string {
	var params struct {
		RitualName string `json:"ritual_name"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("Ritual")
	msg.Writef(" %s", params.RitualName)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		var res struct {
			Status      string `json:"status"`
			ExecutionID string `json:"execution_id"`
		}
		json.Unmarshal([]byte(result), &res)
		execID := res.ExecutionID
		if len(execID) > 8 {
			execID = execID[:8]
		}
		switch res.Status {
		case "completed":
			msg.Writef("Completed [%s]", execID)
		case "failed":
			msg.Writef("Failed [%s]", execID)
		default:
			msg.Writef("%s [%s]", res.Status, execID)
		}
	}

	return msg.String() + "\n"
}

func (t InvokeRitualTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ritual_name": map[string]any{
				"type":        "string",
				"description": "Name of the ritual to invoke (e.g., 'implement', 'fix', 'refactor')",
			},
			"edict_id": map[string]any{
				"type":        "integer",
				"description": "The edict ID this ritual is processing (optional for unbound rituals, like reviews)",
			},
			"inputs": map[string]any{
				"type":        "object",
				"description": "Optional inputs for the ritual (key-value pairs)",
			},
		},
		"required": []string{"ritual_name"},
	}
}

// Tools returns the Chancellor's LLM tools for interactive sessions
func (c *Chancellor) Tools() []Tool {
	// Create zhengming notify wrapper
	var zhengmingNotify tools.ZhengmingNotifyFunc
	zhengmingNotify = func(requestID string, edictID uint, ministerID string, questions []storage.ZhengmingQuestion, priority storage.ZhengmingPriority) {
		c.notify(ZhengmingPendingMsg{
			RequestID:  requestID,
			EdictID:    edictID,
			MinisterID: ministerID,
			Questions:  questions,
			Priority:   priority,
		})
	}

	toolList := []Tool{
		tools.AsimiSQLTool{DBPath: c.getDBPath()},
		tools.UpdateEdictTool{Manager: c},
		tools.RequestZhengmingTool{MinisterID: c.ministerID, Requester: c, Notify: zhengmingNotify},
		tools.GetEdictStatusTool{Manager: c, DB: c.db},
		tools.ListEdictsTool{DB: c.db},
		tools.TransitionEdictTool{DB: c.db},
		InvokeMinisterTool{chancellor: c},
	}
	// Add read-only file tools
	for _, t := range tools.GetROTools(c.config.LLM) {
		toolList = append(toolList, t)
	}
	// Add InvokeRitualTool if ritual runner is available
	if c.shogunate != nil && c.shogunate.GetRitualRunner() != nil {
		toolList = append(toolList, InvokeRitualTool{chancellor: c})
	}
	if c.runner != nil {
		toolList = append(toolList, tools.NewRunShellCommand(c.runner, c.runner, nil))
	}
	return toolList
}

// getLastStepOutput returns the full output of the last step that produced a message.
func getLastStepOutput(exec *RitualExecution) string {
	for i := len(exec.stepStates) - 1; i >= 0; i-- {
		if exec.stepStates[i].Message != "" {
			return exec.stepStates[i].Message
		}
	}
	return ""
}

// --- Interface implementations for tools package ---

// RunRitual runs a ritual synchronously, blocking until completion or failure.
// Uses the caller's ctx so CTRL-C propagates properly.
func (c *Chancellor) RunRitual(ctx context.Context, ritualName string, edictID uint, inputs map[string]string) (*RitualExecution, error) {
	if c.shogunate == nil || c.shogunate.GetRitualRunner() == nil {
		return nil, fmt.Errorf("ritual runner not available")
	}

	exec, err := c.shogunate.GetRitualRunner().Start(ctx, ritualName, edictID, inputs, c.notify)
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
	c.logger.Info("chancellor started, awaiting prompts and events")
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("chancellor stopped")
			return
		case prompt := <-c.PromptsChan():
			c.logger.Debug("Processing prompt", "prompt", prompt)
			// Merge lifecycle ctx (shutdown) with per-prompt ctx (CTRL-C):
			// cancel when either fires.
			merged, mergedCancel := context.WithCancel(ctx)
			if prompt.Ctx != nil {
				context.AfterFunc(prompt.Ctx, func() { mergedCancel() })
			}
			c.processPrompt(merged, prompt)
			mergedCancel()
		case task := <-c.taskChan:
			merged, mergedCancel := context.WithCancel(ctx)
			if task.Ctx != nil {
				context.AfterFunc(task.Ctx, func() { mergedCancel() })
			}
			c.processTask(merged, task)
			mergedCancel()
		}
	}
}

// SetShogunate sets the Shogunate reference for minister access
func (c *Chancellor) SetShogunate(s *Shogunate) {
	c.shogunate = s
}

// SubscribeToEvents registers the Chancellor's event handlers directly with the RitualGuard.
func (c *Chancellor) SubscribeToEvents(rg *RitualGuard) {
	rg.Subscribe(storage.EventEdictCreated, func(e Event) {
		c.handleEdictCreated(c.shogunate.ctx, e.EdictID)
	})
	rg.Subscribe(storage.EventRitualCompleted, func(e Event) {
		c.handleRitualCompleted(c.shogunate.ctx, e.EdictID, e.Payload)
	})
	rg.Subscribe(storage.EventRitualFailed, func(e Event) {
		c.handleRitualFailed(c.shogunate.ctx, e.EdictID, e.Payload)
	})
}

// ResetSession nils out the interactive session
func (c *Chancellor) ResetSession() {
	c.RulingSession = nil
}

// RestoreSession creates a fully-wired interactive session and injects loaded history
func (c *Chancellor) RestoreSession(msgs []llms.MessageContent) error {
	sess, err := CreateSession(c, c.model, c.config, c.notify, "chancellor")
	if err != nil {
		return err
	}
	sess.SetMessages(msgs)
	sess.TabType = "interactive"
	c.RulingSession = sess
	return nil
}

// --- Edict Management ---

// GetEdict retrieves an edict by ID
func (c *Chancellor) GetEdict(edictID uint) (*storage.Edict, error) {
	var edict storage.Edict
	if err := c.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("edict not found: %d", edictID)
		}
		return nil, fmt.Errorf("failed to get edict: %w", err)
	}
	return &edict, nil
}

// SetChancellorSeal sets or clears the Chancellor's seal on an edict
func (c *Chancellor) SetChancellorSeal(edictID uint, sealed bool) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("chancellor_seal", sealed)
	if result.Error != nil {
		return fmt.Errorf("failed to set chancellor seal: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %d", edictID)
	}
	return nil
}

// SetCensorSeal sets or clears the Censor's seal on an edict
func (c *Chancellor) SetCensorSeal(edictID uint, sealed bool) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("censor_seal", sealed)
	if result.Error != nil {
		return fmt.Errorf("failed to set censor seal: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %d", edictID)
	}
	return nil
}

// CancelEdict marks an edict as cancelled
func (c *Chancellor) CancelEdict(edictID uint, cancelledBy, reason string) error {
	now := time.Now()
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("cancelled_at", now)
	if result.Error != nil {
		return fmt.Errorf("failed to cancel edict: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %d", edictID)
	}
	return nil
}

// --- Zhengming (Clarification) Management ---

// GetPendingZhengming retrieves all pending clarification requests for an edict
func (c *Chancellor) GetPendingZhengming(edictID uint) ([]storage.Zhengming, error) {
	var requests []storage.Zhengming
	query := c.db.Where("status = ?", storage.ZhengmingPending).Order("created_at ASC")
	if edictID != 0 {
		query = query.Where("edict_id = ?", edictID)
	}
	if err := query.Find(&requests).Error; err != nil {
		return nil, fmt.Errorf("failed to get pending zhengming: %w", err)
	}
	return requests, nil
}

// --- Manifest and Ling Management ---

// GetAllManifestsForEdict retrieves all manifests for an edict (Chancellor privilege)
func (c *Chancellor) GetAllManifestsForEdict(edictID uint) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := c.db.Where("edict_id = ?", edictID).
		Order("created_at ASC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get manifests: %w", err)
	}
	return manifests, nil
}

// GetAllLingForEdict retrieves all ling for an edict (Chancellor privilege)
func (c *Chancellor) GetAllLingForEdict(edictID uint) ([]storage.Ling, error) {
	var ling []storage.Ling
	err := c.db.Where("edict_id = ?", edictID).
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
func (c *Chancellor) CancelEdictWithContext(ctx context.Context, edictID uint, cancelledBy, reason string) error {
	if err := c.CancelEdict(edictID, cancelledBy, reason); err != nil {
		return err
	}

	c.EmitEvent(edictID, "edict_cancelled", storage.JSON{
		"cancelled_by": cancelledBy,
		"reason":       reason,
	})

	c.logger.Info("edict cancelled", "edict_id", edictID, "by", cancelledBy)
	return nil
}

// --- Prompt Processing ---

// processPrompt handles a single prompt from the Ruler
func (c *Chancellor) processPrompt(ctx context.Context, prompt *Prompt) {
	edictID := prompt.EdictID
	if edictID != 0 {
		// Edict-bound prompt — append to intent
		if err := c.AppendToIntent(edictID, prompt.Message); err != nil {
			c.logger.Warn("failed to append to intent", "edict_id", edictID, "error", err)
		}
	}
	// When edictID == 0, this is a ruling session (edict-free chat).
	// No edict is created — the Chancellor can create one on-demand via tools.

	// Notify TUI of edict ID before streaming begins
	c.notify(StreamStartMsg{TabID: "chancellor", EdictID: edictID})

	// Call LLM with streaming
	c.brewWithStreaming(ctx, edictID, prompt.Message, prompt.ContextFiles)
}

// brewWithStreaming delegates to Session for LLM interaction
func (c *Chancellor) brewWithStreaming(ctx context.Context, edictID uint, prompt string, contextFiles map[string]string) {
	if c.model == nil {
		c.notify(StreamErrorMsg{TabID: "chancellor", Err: fmt.Errorf("LLM not configured")})
		return
	}

	// Always use RulingSession — no per-edict sessions
	if c.RulingSession == nil {
		var err error
		c.RulingSession, err = CreateSession(c, c.model, c.config, c.notify, "chancellor")
		if err != nil {
			c.notify(StreamErrorMsg{TabID: "chancellor", Err: fmt.Errorf("failed to create session: %w", err)})
			return
		}
		c.RulingSession.TabType = "interactive"
		c.logger.Info("chancellor created interactive session")
	}

	c.logger.Debug("context files received from TUI", "count", len(contextFiles))

	// Pass edictID in prompt context, not session
	fullPrompt := prompt
	if edictID != 0 {
		fullPrompt = fmt.Sprintf("[Context: edict %d]\n\n%s", edictID, prompt)
	}

	_, err := c.RulingSession.AskWithStreaming(ctx, fullPrompt, contextFiles)
	if err != nil && ctx.Err() == nil {
		c.notify(StreamErrorMsg{TabID: "chancellor", Err: err})
		return
	}
	c.notify(StreamDoneMsg{TabID: "chancellor"})
}

// processTask handles a task from the ritual runner or other ministers.
func (c *Chancellor) processTask(ctx context.Context, task *Task) {
	c.logger.Info("chancellor processing task",
		"edict_id", task.EdictID,
		"work", truncateString(task.Work, 50))

	var output string
	var taskErr error

	if c.model != nil {
		// Always use RulingSession for task conversation
		if c.RulingSession == nil {
			var err error
			c.RulingSession, err = CreateSession(c, c.model, c.config, c.notify, "chancellor")
			if err != nil {
				taskErr = fmt.Errorf("failed to create session: %w", err)
			} else {
				c.RulingSession.TabType = "interactive"
			}
		}

		if taskErr == nil {
			_, taskErr = c.RulingSession.AskWithStreaming(ctx, task.Work, nil)
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
			c.logger.Warn("done channel full, dropping result", "edict_id", task.EdictID)
		}
	}
}

// handleEdictCreated sends the new edict to the chancellor LLM to choose and enact the appropriate ritual.
func (c *Chancellor) handleEdictCreated(ctx context.Context, edictID uint) {
	c.logger.Info("handling edict created", "edict_id", edictID)

	edict, err := c.GetEdict(edictID)
	if err != nil {
		c.logger.Error("failed to get edict", "edict_id", edictID, "error", err)
		return
	}

	work := fmt.Sprintf("New edict %d: %s\n\nChoose the appropriate ritual and enact it.", edictID, edict.Intent)
	task := &Task{
		Ctx:     ctx,
		EdictID: edictID,
		Work:    work,
		Done:    make(chan Result, 1),
	}

	select {
	case c.taskChan <- task:
	default:
		c.logger.Warn("chancellor task channel full", "edict_id", edictID)
	}
}

// handleRitualCompleted processes a completed ritual event
func (c *Chancellor) handleRitualCompleted(ctx context.Context, edictID uint, payload map[string]interface{}) {
	c.logger.Info("handling ritual completed", "edict_id", edictID, "payload", payload)

	// Extract last_step_output from payload and append to RulingSession
	if lastStepOutput, ok := payload["last_step_output"].(string); ok && lastStepOutput != "" {
		if c.RulingSession != nil {
			c.RulingSession.AddMessage(llms.ChatMessageTypeAI, fmt.Sprintf("Ritual completed. Last step output:\n%s", lastStepOutput))
			c.logger.Debug("appended ritual completion to RulingSession", "edict_id", edictID)
		}
	}

	c.logger.Debug("ritual completed - edict may need synthesis", "edict_id", edictID)
}

// handleZhengmingAnswered resumes the chancellor's work on an edict after clarification
func (c *Chancellor) handleZhengmingAnswered(ctx context.Context, edictID uint, payload map[string]interface{}) {
	answer, _ := payload["answer"].(string)
	if edictID == 0 || answer == "" {
		return
	}
	c.logger.Info("handling zhengming answered", "edict_id", edictID, "answer", answer)

	edict, err := c.GetEdict(edictID)
	if err != nil {
		c.logger.Error("failed to get edict for zhengming resumption", "edict_id", edictID, "error", err)
		return
	}

	work := fmt.Sprintf("Resume edict %d: %s\n\nThe clarification has been answered. Continue from where you left off.", edictID, edict.Intent)
	task := &Task{
		Ctx:     ctx,
		EdictID: edictID,
		Work:    work,
		Done:    make(chan Result, 1),
	}

	select {
	case c.taskChan <- task:
	default:
		c.logger.Warn("chancellor task channel full", "edict_id", edictID)
	}
}

// handleRitualFailed processes a failed ritual event
func (c *Chancellor) handleRitualFailed(ctx context.Context, edictID uint, payload map[string]interface{}) {
	c.logger.Error("handling ritual failed", "edict_id", edictID, "payload", payload)

	// Extract last_step_output and error from payload and append to RulingSession
	if lastStepOutput, ok := payload["last_step_output"].(string); ok && lastStepOutput != "" {
		if c.RulingSession != nil {
			errMsg, _ := payload["error"].(string)
			c.RulingSession.AddMessage(llms.ChatMessageTypeAI, fmt.Sprintf("Ritual failed. Error: %s\nLast step output:\n%s", errMsg, lastStepOutput))
			c.logger.Debug("appended ritual failure to RulingSession", "edict_id", edictID)
		}
	}

	c.logger.Debug("ritual failed - may need zhengming or retry", "edict_id", edictID)
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
