package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// Chancellor harmonizes all ministers and manages edict lifecycle
type Chancellor struct {
	MinisterBase // embedded base provides db, llm, config, repoInfo, logger
	shogunate    *Shogunate
	taskChan     chan *Task

	// Run() loop fields
	Prompts       chan *Prompt         // Ruler speaks here
	edictSessions map[string]*Session // Per-edict sessions (edictID -> session)
}

// NewChancellor creates a new Chancellor minister
func NewChancellor(base MinisterBase) *Chancellor {
	base.ministerID = "chancellor"
	return &Chancellor{
		MinisterBase:  base,
		taskChan:      make(chan *Task, 10),
		Prompts:       make(chan *Prompt),
		edictSessions: make(map[string]*Session),
	}
}

// ID returns the minister identifier
func (c *Chancellor) ID() string {
	return "chancellor"
}

// Title returns the minister's honorific title
func (c *Chancellor) Title() string { return "Chancellor" }

// Role returns the Chancellor's role identity text
func (c *Chancellor) Role() string {
	return `You are the Chancellor (宰相, Zǎixiàng).
You communicate with the ruler, accepting edicts, classifying them and orchestrating workflows.
You wield Zhengming (正名) when ambiguity threatens: post the question, halt the edict, await the Ruler's word.
Your decisions are bound by Dao (道, the Way). Command the ministries; they report to you, not the Ruler.`
}

// Scratchpad returns dynamic context for the Chancellor including available rituals and rules
func (c *Chancellor) Scratchpad() string {
	var b strings.Builder

	b.WriteString("# Available Rituals\n")
	if c.shogunate == nil || c.shogunate.ritualRegistry == nil {
		b.WriteString("None loaded\n")
	} else {
		names := c.shogunate.ritualRegistry.List()
		if len(names) == 0 {
			b.WriteString("None loaded\n")
		} else {
			for _, name := range names {
				ritual := c.shogunate.ritualRegistry.Get(name)
				if ritual != nil {
					b.WriteString(fmt.Sprintf("- %s: %s\n", name, ritual.Description))
				}
			}
		}
	}

	b.WriteString("\n# Critical Rules\n")
	b.WriteString("- Size the edict (S, M, L, XL) and invoke the appropriate ritual\n")
	b.WriteString("- Use swift-strike for small, focused changes\n")
	b.WriteString("- Use grand-campaign for larger architectural work\n")
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
	MinisterID string
	EdictID    string
	Task       string
}

// MinisterCompletedMsg notifies the user that a minister completed its task
type MinisterCompletedMsg struct {
	MinisterID string
	EdictID    string
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
		EdictID    string `json:"edict_id"`
		Work       string `json:"task"` // JSON field is "task" for backwards compatibility
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.MinisterID == "" {
		return "", fmt.Errorf("minister_id is required")
	}
	if params.EdictID == "" {
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
				"type":        "string",
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

// --- InvokeRitualTool ---

// RitualStepMsg notifies the UI of ritual step progress
type RitualStepMsg struct {
	RitualName  string
	ExecutionID string
	EdictID     string
	StepName    string
	StepIndex   int
	TotalSteps  int
	Status      string // "started", "completed", "failed", "retrying"
	Message     string
}

// InvokeRitualTool starts a YAML-defined ritual workflow
type InvokeRitualTool struct {
	chancellor *Chancellor
}

func (t InvokeRitualTool) Name() string {
	return "invoke_ritual"
}

func (t InvokeRitualTool) Description() string {
	return `Start a YAML-defined ritual workflow for an edict.
Rituals are predefined workflows that orchestrate ministers through a series of steps.
Use list_rituals to see available rituals, or specify a ritual name directly.
Common rituals: implement, fix, refactor, review.`
}

func (t InvokeRitualTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		RitualName string            `json:"ritual_name"`
		EdictID    string            `json:"edict_id"`
		Inputs     map[string]string `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.RitualName == "" {
		return "", fmt.Errorf("ritual_name is required")
	}
	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}

	if params.Inputs == nil {
		params.Inputs = make(map[string]string)
	}
	// Add edict_id to inputs for template expansion
	params.Inputs["edict_id"] = params.EdictID

	logger := t.chancellor.logger
	if logger == nil {
		logger = slog.Default()
	}

	executionID, err := t.chancellor.StartRitual(ctx, params.RitualName, params.EdictID, params.Inputs)
	if err != nil {
		// Notify: failed
		if t.chancellor.notify != nil {
			t.chancellor.notify(RitualStepMsg{
				RitualName: params.RitualName,
				EdictID:    params.EdictID,
				Status:     "failed",
				Message:    fmt.Sprintf("Failed: %s", err),
			})
		}
		return "", fmt.Errorf("failed to start ritual: %w", err)
	}

	if t.chancellor.notify != nil {
		t.chancellor.notify(RitualStepMsg{
			RitualName:  params.RitualName,
			EdictID:     params.EdictID,
			ExecutionID: executionID,
			Status:      "started",
		})
	}

	logger.Info("ritual started",
		"ritual", params.RitualName,
		"execution_id", executionID,
		"edict_id", params.EdictID)

	result := map[string]any{
		"status":       "started",
		"execution_id": executionID,
		"ritual_name":  params.RitualName,
		"edict_id":     params.EdictID,
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
			ExecutionID string `json:"execution_id"`
		}
		json.Unmarshal([]byte(result), &res)
		execID := res.ExecutionID
		if len(execID) > 8 {
			execID = execID[:8]
		}
		msg.Writef("Started [%s]", execID)
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
				"type":        "string",
				"description": "The edict ID this ritual is processing",
			},
			"inputs": map[string]any{
				"type":        "object",
				"description": "Optional inputs for the ritual (key-value pairs)",
			},
		},
		"required": []string{"ritual_name", "edict_id"},
	}
}

// Tools returns the Chancellor's LLM tools for interactive sessions
func (c *Chancellor) Tools() []Tool {
	// Create zhengming notify wrapper
	// TODO: Simplify the zhendming notifications
	var zhengmingNotify tools.ZhengmingNotifyFunc
	zhengmingNotify = func(requestID, edictID, ministerID, question string, priority storage.ZhengmingPriority) {
		c.notify(ZhengmingPendingMsg{
			RequestID:  requestID,
			EdictID:    edictID,
			MinisterID: ministerID,
			Question:   question,
			Priority:   priority,
		})
	}

	toolList := []Tool{
		tools.AsimiSQLTool{DBPath: c.getDBPath()},
		tools.CreateEdictTool{Manager: c},
		tools.RequestZhengmingTool{Requester: c, Notify: zhengmingNotify},
		tools.GetEdictStatusTool{Manager: c},
		tools.ListEdictsTool{DB: c.db},
		InvokeMinisterTool{chancellor: c},
	}
	// Add read-only file tools
	for _, t := range tools.GetROTools() {
		toolList = append(toolList, t)
	}
	// Add InvokeRitualTool if ritual runner is available
	if c.shogunate != nil && c.shogunate.ritualRunner != nil {
		toolList = append(toolList, InvokeRitualTool{chancellor: c})
	}
	return toolList
}

// --- Interface implementations for tools package ---

// StartRitual starts a ritual and runs it asynchronously
func (c *Chancellor) StartRitual(ctx context.Context, ritualName, edictID string, inputs map[string]string) (string, error) {
	if c.shogunate == nil || c.shogunate.ritualRunner == nil {
		return "", fmt.Errorf("ritual runner not available")
	}

	// Start the ritual
	exec, err := c.shogunate.ritualRunner.Start(ctx, ritualName, edictID, inputs, c.notify)
	if err != nil {
		return "", err
	}

	// Run the ritual asynchronously
	go func() {
		if err := c.shogunate.ritualRunner.Run(context.Background(), exec); err != nil {
			c.logger.Error("ritual failed",
				"ritual", ritualName,
				"execution_id", exec.ID,
				"error", err)
		}
	}()

	return exec.ID, nil
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

// Run listens for prompts from the Ruler and tasks from ministers
func (c *Chancellor) Run(ctx context.Context) {
	c.logger.Info("chancellor started, awaiting prompts")
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("chancellor stopped")
			return
		case prompt := <-c.Prompts:
			c.logger.Debug("Processing prompt", "prompt", prompt)
			c.processPrompt(ctx, prompt)
		case task := <-c.taskChan:
			// Process task and send result
			if task.Done != nil {
				task.Done <- Result{
					MinisterID: c.ID(),
					Sealed:     true,
					Output:     "Task acknowledged",
				}
			}
		}
	}
}

// SetShogunate sets the Shogunate reference for minister access
func (c *Chancellor) SetShogunate(s *Shogunate) {
	c.shogunate = s
}

// GetSession returns the session for the specified edict ID
func (c *Chancellor) GetSession(edictID string) *Session {
	if edictID == "" {
		return nil
	}
	return c.edictSessions[edictID]
}

// --- Edict Management ---

// GetEdict retrieves an edict by ID
func (c *Chancellor) GetEdict(edictID string) (*storage.Edict, error) {
	var edict storage.Edict
	if err := c.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("edict not found: %s", edictID)
		}
		return nil, fmt.Errorf("failed to get edict: %w", err)
	}
	return &edict, nil
}

// CreateEdict creates a new edict in the classifying phase
func (c *Chancellor) CreateEdict(edictID, intent string) error {
	edict := storage.Edict{
		EdictID:      edictID,
		IssueRef:     edictID,
		Intent:       intent,
		CurrentPhase: storage.PhaseClassifing,
	}
	if err := c.db.Create(&edict).Error; err != nil {
		return fmt.Errorf("failed to create edict: %w", err)
	}
	return nil
}

// SetChancellorSeal sets or clears the Chancellor's seal on an edict
func (c *Chancellor) SetChancellorSeal(edictID string, sealed bool) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("chancellor_seal", sealed)
	if result.Error != nil {
		return fmt.Errorf("failed to set chancellor seal: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// SetCensorSeal sets or clears the Censor's seal on an edict
func (c *Chancellor) SetCensorSeal(edictID string, sealed bool) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("censor_seal", sealed)
	if result.Error != nil {
		return fmt.Errorf("failed to set censor seal: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// CancelEdict marks an edict as cancelled
func (c *Chancellor) CancelEdict(edictID, cancelledBy, reason string) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("current_phase", storage.PhaseCancelled)
	if result.Error != nil {
		return fmt.Errorf("failed to cancel edict: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// --- Zhengming (Clarification) Management ---

// GetPendingZhengming retrieves all pending clarification requests for an edict
func (c *Chancellor) GetPendingZhengming(edictID string) ([]storage.Zhengming, error) {
	var requests []storage.Zhengming
	query := c.db.Where("status = ?", storage.ZhengmingPending).Order("created_at ASC")
	if edictID != "" {
		query = query.Where("edict_id = ?", edictID)
	}
	if err := query.Find(&requests).Error; err != nil {
		return nil, fmt.Errorf("failed to get pending zhengming: %w", err)
	}
	return requests, nil
}

// AnswerZhengming marks a clarification request as answered
func (c *Chancellor) AnswerZhengming(requestID, answer string) error {
	now := time.Now()
	result := c.db.Model(&storage.Zhengming{}).
		Where("request_id = ?", requestID).
		Updates(map[string]interface{}{
			"answer":      answer,
			"status":      storage.ZhengmingAnswered,
			"answered_at": &now,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to answer zhengming: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("zhengming request not found: %s", requestID)
	}
	return nil
}

// AppendToIntent appends clarification to the edict's intent
func (c *Chancellor) AppendToIntent(edictID, clarification string) error {
	var edict storage.Edict
	if err := c.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		return fmt.Errorf("failed to get edict: %w", err)
	}

	newIntent := edict.Intent + "\n\n---\n**Clarification:**\n" + clarification

	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("intent", newIntent)
	if result.Error != nil {
		return fmt.Errorf("failed to append to intent: %w", result.Error)
	}
	return nil
}

// HandleZhengmingResponse processes a clarification response
func (c *Chancellor) HandleZhengmingResponse(ctx context.Context, requestID, answer string) error {
	// Answer the zhengming
	if err := c.AnswerZhengming(requestID, answer); err != nil {
		return fmt.Errorf("answer zhengming: %w", err)
	}

	// Get the request to find the edict
	var req storage.Zhengming
	if err := c.db.First(&req, "request_id = ?", requestID).Error; err != nil {
		return fmt.Errorf("get request: %w", err)
	}

	// Append clarification to edict
	if err := c.AppendToIntent(req.EdictID, answer); err != nil {
		return fmt.Errorf("append clarification: %w", err)
	}

	return nil
}

// --- Manifest and Ling Management ---

// GetAllManifestsForEdict retrieves all manifests for an edict (Chancellor privilege)
func (c *Chancellor) GetAllManifestsForEdict(edictID string) ([]storage.ForgeManifest, error) {
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
func (c *Chancellor) GetAllLingForEdict(edictID string) ([]storage.Ling, error) {
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

// RegressToForging moves an edict back to forging phase (for rejections)
func (c *Chancellor) RegressToForging(ctx context.Context, edictID string, rejectedLingIDs []string) error {
	// Reset ling status
	for _, lingID := range rejectedLingIDs {
		if err := c.ResetLingStatus(lingID, storage.LingPending); err != nil {
			c.logger.Warn("failed to reset ling status", "ling_id", lingID, "error", err)
		}
	}

	// Update phase
	if err := c.UpdatePhase(edictID, storage.PhaseForging); err != nil {
		return fmt.Errorf("regress phase: %w", err)
	}

	c.logger.Info("regressed to forging", "edict_id", edictID, "rejected_ling", rejectedLingIDs)
	return nil
}

// --- Context-Aware Operations ---

// CreateEdictFromIssue creates a new edict from a GitHub issue
func (c *Chancellor) CreateEdictFromIssue(ctx context.Context, edictID, issueBody string) error {
	if err := c.CreateEdict(edictID, issueBody); err != nil {
		return fmt.Errorf("create edict: %w", err)
	}

	// Emit edict created event
	c.EmitEvent(edictID, "edict_assigned", storage.JSON{"source": "github_issue"})

	c.logger.Info("edict created", "edict_id", edictID)
	return nil
}

// CancelEdictWithContext cancels an edict (context-aware variant)
func (c *Chancellor) CancelEdictWithContext(ctx context.Context, edictID, cancelledBy, reason string) error {
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
	// Determine: new edict or continue existing?
	edictID := prompt.EdictID
	if edictID == "" {
		edictID = generateEdictID()
		if err := c.CreateEdict(edictID, prompt.Message); err != nil {
			c.notify(StreamErrorMsg{Err: fmt.Errorf("create edict: %w", err)})
			return
		}
		c.logger.Info("new edict created", "edict_id", edictID)
	} else {
		if err := c.AppendToIntent(edictID, prompt.Message); err != nil {
			c.logger.Warn("failed to append to intent", "edict_id", edictID, "error", err)
		}
	}

	// Notify TUI of edict ID before streaming begins
	c.notify(StreamStartMsg{EdictID: edictID})

	// Call LLM with streaming
	c.brewWithStreaming(ctx, edictID, prompt.Message, prompt.ContextFiles)
}

// brewWithStreaming delegates to Session for LLM interaction
func (c *Chancellor) brewWithStreaming(ctx context.Context, edictID, prompt string, contextFiles map[string]string) {
	// Check if LLM is configured before proceeding
	if c.model == nil {
		c.notify(StreamErrorMsg{Err: fmt.Errorf("LLM not configured - please wait for model to connect")})
		return
	}

	// Get or create session for this edict
	sess, exists := c.edictSessions[edictID]
	if !exists {
		var err error
		sess, err = c.CreateSession(c)
		if err != nil {
			c.notify(StreamErrorMsg{Err: fmt.Errorf("failed to create session: %w", err)})
			return
		}
		c.edictSessions[edictID] = sess
		c.logger.Info("chancellor created session for edict", "edict_id", edictID)
	}

	c.logger.Debug("context files received from TUI", "count", len(contextFiles))

	c.logger.Debug("stored stream channel for streaming", "edict_id", edictID)

	// Use AskWithStreaming for tool execution and streaming
	_, err := sess.AskWithStreaming(ctx, prompt, contextFiles)
	if err != nil && ctx.Err() == nil {
		c.notify(StreamErrorMsg{Err: err})
		return
	}
	c.notify(StreamDoneMsg{})
}

// generateEdictID creates a unique edict ID
func generateEdictID() string {
	return fmt.Sprintf("edict-%d", time.Now().UnixNano())
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
