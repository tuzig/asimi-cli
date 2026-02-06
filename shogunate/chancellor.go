package shogunate

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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
	Edicts           chan *Edict         // Ruler speaks here
	edictSessions    map[string]*Session // Per-edict sessions (edictID -> session)
	activeReplyChans sync.Map            // edictID -> chan<- Reply (thread-safe)
}

// NewChancellor creates a new Chancellor minister
func NewChancellor(base MinisterBase) *Chancellor {
	base.ministerID = "chancellor"
	return &Chancellor{
		MinisterBase:  base,
		taskChan:      make(chan *Task, 10),
		Edicts:        make(chan *Edict),
		edictSessions: make(map[string]*Session),
		// activeReplyChans is a sync.Map, zero-value ready
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

// Tools returns the Chancellor's LLM tools for interactive sessions
func (c *Chancellor) Tools(notify NotifyFunc) []Tool {
	// Create zhengming notify wrapper
	// TODO: Simplify the zhendming notifications
	var zhengmingNotify tools.ZhengmingNotifyFunc
	if notify != nil {
		zhengmingNotify = func(requestID, edictID, ministerID, question string, priority storage.ZhengmingPriority) {
			notify(ZhengmingPendingMsg{
				RequestID:  requestID,
				EdictID:    edictID,
				MinisterID: ministerID,
				Question:   question,
				Priority:   priority,
			})
		}
	}

	// Create minister notify wrapper
	var ministerNotify tools.MinisterNotifyFunc
	if notify != nil {
		ministerNotify = func(ministerID, edictID, task, status string) {
			switch status {
			case "invoking":
				notify(MinisterInvokingMsg{
					MinisterID: ministerID,
					EdictID:    edictID,
					Task:       task,
				})
			case "completed", "failed":
				//TODO: There has to be a difference
				notify(MinisterCompletedMsg{
					MinisterID: ministerID,
					EdictID:    edictID,
					Output:     "",
					Sealed:     status == "completed",
					Error:      nil,
				})
			}
		}
	}

	// Create ritual notify wrapper
	var ritualNotify tools.RitualNotifyFunc
	if notify != nil {
		ritualNotify = func(ritualName, executionID, edictID, status string) {
			notify(RitualStepMsg{
				RitualName:  ritualName,
				ExecutionID: executionID,
				EdictID:     edictID,
				Status:      status,
			})
		}
	}

	toolList := []Tool{
		tools.AsimiSQLTool{DBPath: c.getDBPath()},
		tools.CreateEdictTool{Manager: c},
		tools.RequestZhengmingTool{Requester: c, Notify: zhengmingNotify},
		tools.GetEdictStatusTool{Manager: c},
		tools.ListEdictsTool{DB: c.db},
		// TODO: rename to InviteMinisterTool to join the chat
		tools.InvokeMinisterTool{Invoker: c, Logger: c.logger, Notify: ministerNotify},
	}
	// Add read-only file tools
	for _, t := range tools.GetROTools() {
		toolList = append(toolList, t)
	}
	// Add InvokeRitualTool if ritual runner is available
	if c.shogunate != nil && c.shogunate.ritualRunner != nil {
		toolList = append(toolList, tools.InvokeRitualTool{
			Starter: c,
			Logger:  c.logger,
			Notify:  ritualNotify,
		})
	}
	return toolList
}

// --- Interface implementations for tools package ---

// InvokeMinister implements tools.MinisterInvoker
func (c *Chancellor) InvokeMinister(ctx context.Context, ministerID, edictID, work string, timeout time.Duration) (tools.MinisterResult, error) {
	// Get minister via Shogunate
	minister := c.shogunate.GetMinister(ministerID)
	if minister == nil {
		return nil, fmt.Errorf("minister not found: %s", ministerID)
	}

	// Create per-call done channel (synchronous blocking pattern)
	doneChan := make(chan Result, 1)

	// Get streaming channel if available (thread-safe lookup)
	var streamChan StreamChan
	if val, ok := c.activeReplyChans.Load(edictID); ok {
		streamChan = val.(StreamChan)
	}
	c.logger.Debug("invoke_minister looking up stream channel",
		"edict_id", edictID,
		"found", streamChan != nil)

	// Create Task with per-call done channel and streaming channel
	t := &Task{
		EdictID: edictID,
		Work:    work,
		Stream:  streamChan,
		Done:    doneChan,
	}

	// Send task to minister
	select {
	case minister.Tasks() <- t:
		c.logger.Info("task sent to minister",
			"minister", ministerID,
			"edict_id", edictID,
			"work", truncateString(work, 50))
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled while sending task to %s", ministerID)
	}

	// Block until minister replies (only blocks this session's goroutine)
	select {
	case result := <-doneChan:
		return &result, nil

	case <-time.After(timeout):
		return nil, fmt.Errorf("minister %s timeout after %v", ministerID, timeout)

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// StartRitual implements tools.RitualStarter
func (c *Chancellor) StartRitual(ctx context.Context, ritualName, edictID string, inputs map[string]string) (string, error) {
	if c.shogunate == nil || c.shogunate.ritualRunner == nil {
		return "", fmt.Errorf("ritual runner not available")
	}

	// Get streaming channel if available
	var notify NotifyFunc
	if val, ok := c.activeReplyChans.Load(edictID); ok {
		streamChan := val.(StreamChan)
		notify = func(msg any) {
			// Forward typed messages directly to the stream
			streamChan <- msg
		}
	}

	// Start the ritual
	exec, err := c.shogunate.ritualRunner.Start(ctx, ritualName, edictID, inputs, notify)
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
	c.logger.Info("chancellor started, awaiting ruler's edicts")
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("chancellor stopped")
			return
		case edict := <-c.Edicts:
			c.processPrompt(ctx, edict)
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
func (c *Chancellor) processPrompt(ctx context.Context, edict *Edict) {
	// Determine: new edict or continue existing?
	edictID := edict.EdictID
	if edictID == "" {
		edictID = generateEdictID()
		if err := c.CreateEdict(edictID, edict.Prompt); err != nil {
			edict.Stream <- StreamErrorMsg{Err: fmt.Errorf("create edict: %w", err)}
			close(edict.Stream)
			return
		}
		c.logger.Info("new edict created", "edict_id", edictID)
	} else {
		if err := c.AppendToIntent(edictID, edict.Prompt); err != nil {
			c.logger.Warn("failed to append to intent", "edict_id", edictID, "error", err)
		}
	}

	// Brew the edict (call LLM with streaming)
	c.brewWithStreaming(ctx, edictID, edict.Prompt, edict.ContextFiles, edict.Stream)
}

// brewWithStreaming delegates to Session for LLM interaction
func (c *Chancellor) brewWithStreaming(ctx context.Context, edictID, prompt string, contextFiles map[string]string, stream StreamChan) {
	// Check if LLM is configured before proceeding
	if c.model == nil {
		stream <- StreamErrorMsg{Err: fmt.Errorf("LLM not configured - please wait for model to connect")}
		close(stream)
		return
	}

	// Get or create session for this edict
	sess, exists := c.edictSessions[edictID]
	if !exists {
		var err error
		sess, err = c.CreateSession(c, nil)
		if err != nil {
			stream <- StreamErrorMsg{Err: fmt.Errorf("failed to create session: %w", err)}
			close(stream)
			return
		}
		c.edictSessions[edictID] = sess
		c.logger.Info("chancellor created session for edict", "edict_id", edictID)
	}

	c.logger.Debug("context files received from TUI", "count", len(contextFiles))

	// Track the stream channel so ministers can stream to the TUI.
	c.activeReplyChans.Store(edictID, stream)
	c.logger.Debug("stored stream channel for streaming", "edict_id", edictID)

	// Forward all typed messages directly to stream channel - no conversion
	sess.SetNotify(func(msg any) {
		switch msg.(type) {
		case StreamStartMsg, StreamCompleteMsg:
			// Internal signals, don't forward
		default:
			stream <- msg
		}
	})

	// Use AskWithStreaming for tool execution and streaming
	_, err := sess.AskWithStreaming(ctx, prompt, contextFiles)
	if err != nil && ctx.Err() == nil {
		stream <- StreamErrorMsg{Err: err}
	}
	stream <- StreamDoneMsg{}
	close(stream)
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
