package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// EdictEnvelope carries the Ruler's prompt to the Chancellor
type EdictEnvelope struct {
	Prompt       string             // The Ruler's words
	EdictID      string             // Empty = new edict, set = continue existing
	ContextFiles map[string]string  // Files loaded via @ references
	ReplyChan    chan<- PromptReply // Return channel for streaming responses
}

// TODO replace with the current podmanShellRunner
// execCommand wraps exec.Command for testability
var execCommand = exec.Command

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

// CreateEdict creates a new edict in the brewing phase
func (c *Chancellor) CreateEdict(edictID, renIntent string) error {
	edict := storage.Edict{
		EdictID:      edictID,
		RenIntent:    renIntent,
		CurrentPhase: storage.PhaseBrewing,
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
	now := time.Now()
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Updates(map[string]interface{}{
			"current_phase":       storage.PhaseCancelled,
			"cancelled_at":        &now,
			"cancelled_by":        cancelledBy,
			"cancellation_reason": reason,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to cancel edict: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// GetPendingZhengming retrieves all pending clarification requests for an edict
func (c *Chancellor) GetPendingZhengming(edictID string) ([]storage.ZhengmingRequest, error) {
	var requests []storage.ZhengmingRequest
	err := c.db.Where("edict_id = ? AND status = ?", edictID, storage.ZhengmingPending).
		Order("created_at ASC").
		Find(&requests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending zhengming: %w", err)
	}
	return requests, nil
}

// AnswerZhengming marks a clarification request as answered
func (c *Chancellor) AnswerZhengming(requestID, answer string) error {
	now := time.Now()
	result := c.db.Model(&storage.ZhengmingRequest{}).
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

// AppendToRenIntent appends clarification to the edict's intent and increments version
func (c *Chancellor) AppendToRenIntent(edictID, clarification string) error {
	var edict storage.Edict
	if err := c.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		return fmt.Errorf("failed to get edict: %w", err)
	}

	newIntent := edict.RenIntent + "\n\n---\n**Clarification (v" +
		fmt.Sprintf("%d", edict.RenIntentVersion+1) + "):**\n" + clarification

	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Updates(map[string]interface{}{
			"ren_intent":         newIntent,
			"ren_intent_version": edict.RenIntentVersion + 1,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to append to ren intent: %w", result.Error)
	}
	return nil
}

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

// UpdatePhase transitions an edict to a new phase
func (c *Chancellor) UpdatePhase(edictID string, phase storage.EdictPhase) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("current_phase", phase)
	if result.Error != nil {
		return fmt.Errorf("failed to update phase: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// RequestZhengming creates a clarification request
func (c *Chancellor) RequestZhengming(edictID, question string, priority storage.ZhengmingPriority) (string, error) {
	requestID := GenerateID("zhengming", edictID, "chancellor", question, time.Now().String())

	req := storage.ZhengmingRequest{
		RequestID:  requestID,
		EdictID:    edictID,
		MinisterID: "chancellor",
		Question:   question,
		Priority:   priority,
		Status:     storage.ZhengmingPending,
		TimeoutAt:  time.Now().Add(24 * time.Hour),
	}

	if priority == storage.PriorityUrgent {
		req.TimeoutAt = time.Now().Add(1 * time.Hour)
	}

	if err := c.db.Create(&req).Error; err != nil {
		return "", fmt.Errorf("failed to create zhengming request: %w", err)
	}
	return requestID, nil
}

// IsZhengmingPending checks if there are pending clarification requests for an edict
func (c *Chancellor) IsZhengmingPending(edictID string) (bool, error) {
	var count int64
	err := c.db.Model(&storage.ZhengmingRequest{}).
		Where("edict_id = ? AND status = ?", edictID, storage.ZhengmingPending).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check pending zhengming: %w", err)
	}
	return count > 0, nil
}

// EmitEvent records an event in the Tian ledger
func (c *Chancellor) EmitEvent(edictID, eventType string, payload storage.JSON) error {
	event := storage.TianEvent{
		EdictID:   edictID,
		EventType: eventType,
		Payload:   payload,
	}
	if err := c.db.Create(&event).Error; err != nil {
		return fmt.Errorf("failed to emit event: %w", err)
	}
	return nil
}

// Chancellor harmonizes all ministers and manages edict lifecycle
type Chancellor struct {
	MinisterBase            // embedded base provides db, llm, config, repoInfo, logger
	dbPath       string     // Path to database for tools
	shogunate    *Shogunate // Reference to Shogunate for minister access

	// Run() loop fields
	Edicts        chan *EdictEnvelope // Ruler speaks here
	tasks         chan *TaskEnvelope  // For Minister interface (Chancellor doesn't receive tasks)
	edictSessions map[string]*Session // Per-edict sessions (edictID -> session)
	toolCatalog   map[string]Tool     // Chancellor's own tools for direct execution
}

// NewChancellor creates a new Chancellor minister
func NewChancellor(base MinisterBase) *Chancellor {
	base.ministerID = "chancellor"
	return &Chancellor{
		MinisterBase:  base,
		Edicts:        make(chan *EdictEnvelope),
		tasks:         make(chan *TaskEnvelope), // Chancellor doesn't process tasks, but interface requires it
		edictSessions: make(map[string]*Session),
	}
}

// Tasks returns the channel for task submission (Chancellor doesn't receive tasks)
func (c *Chancellor) Tasks() chan<- *TaskEnvelope {
	return c.tasks
}

// GetSession returns the session for the specified edict ID
func (c *Chancellor) GetSession(edictID string) *Session {
	if edictID == "" {
		return nil
	}
	return c.edictSessions[edictID]
}

// SetShogunate sets the Shogunate reference for minister access
func (c *Chancellor) SetShogunate(s *Shogunate) {
	c.shogunate = s
}

// SetDBPath sets the database path for tools that need file access
func (c *Chancellor) SetDBPath(dbPath string) {
	c.dbPath = dbPath
}

// SetToolCatalog sets the Chancellor's tool catalog for direct execution
func (c *Chancellor) SetToolCatalog(catalog map[string]Tool) {
	c.toolCatalog = catalog
}

// Run listens for prompts from the Ruler
func (c *Chancellor) Run(ctx context.Context) {
	c.logger.Info("chancellor started, awaiting ruler's edicts")
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("chancellor stopped")
			return
		case env := <-c.Edicts:
			c.processPrompt(ctx, env)
		}
	}
}

// processPrompt handles a single prompt from the Ruler
func (c *Chancellor) processPrompt(ctx context.Context, env *EdictEnvelope) {
	// Determine: new edict or continue existing?
	edictID := env.EdictID
	if edictID == "" {
		edictID = generateEdictID()
		if err := c.CreateEdict(edictID, env.Prompt); err != nil {
			env.ReplyChan <- PromptReply{Type: ReplyError, Error: fmt.Errorf("create edict: %w", err)}
			close(env.ReplyChan)
			return
		}
		c.logger.Info("new edict created", "edict_id", edictID)
	} else {
		if err := c.AppendToRenIntent(edictID, env.Prompt); err != nil {
			c.logger.Warn("failed to append to ren intent", "edict_id", edictID, "error", err)
		}
	}

	// Brew the edict (call LLM with streaming)
	c.brewWithStreaming(ctx, edictID, env.Prompt, env.ContextFiles, env.ReplyChan)
}

// brewWithStreaming delegates to Session's agentic loop for LLM interaction
func (c *Chancellor) brewWithStreaming(ctx context.Context, edictID, prompt string, contextFiles map[string]string, reply chan<- PromptReply) {
	// Check if LLM is configured before proceeding
	if c.llm == nil {
		reply <- PromptReply{Type: ReplyError, Error: fmt.Errorf("LLM not configured - please wait for model to connect")}
		close(reply)
		return
	}

	// Get or create session for this edict
	sess, exists := c.edictSessions[edictID]
	if !exists {
		var err error
		sess, err = c.CreateSession(c, nil)
		if err != nil {
			reply <- PromptReply{Type: ReplyError, Error: fmt.Errorf("failed to create session: %w", err)}
			close(reply)
			return
		}
		c.edictSessions[edictID] = sess
		c.logger.Info("chancellor created session for edict", "edict_id", edictID)
	}

	c.logger.Debug("context files received from TUI", "count", len(contextFiles))

	sess.AskStream(ctx, prompt, reply, edictID, contextFiles)
}

// generateEdictID creates a unique edict ID
func generateEdictID() string {
	return fmt.Sprintf("edict-%d", time.Now().UnixNano())
}

// ID returns the minister identifier
func (c *Chancellor) ID() string {
	return "chancellor"
}

// Role returns the Chancellor's role identity text
func (c *Chancellor) Role() string {
	return `You are the Chancellor (宰相, Zǎixiàng).
You hamronize all rituals by invoking ministers using the invoke_minister tool. You wield Zhengming (正名) when ambiguity threatens: post the question, halt the edict, await the Ruler's word.
Your decisions are bound by Dao (道, the Way). Command the ministries; they report to you, not the Ruler.
# Critical Rules

- You have full read/write access to all domains
- You are the ONLY role that can read cross-minister data
- When ambiguity threatens progress, invoke Zhengming immediately via request_zhengming
- Never guess at requirements—always clarify`
}

// Tools returns the Chancellor's LLM tools for interactive sessions
func (c *Chancellor) Tools(notify NotifyFunc) []Tool {
	return []Tool{
		AsimiSQLTool{dbPath: c.dbPath},
		CreateEdictTool{chancellor: c},
		RequestZhengmingTool{chancellor: c, notify: notify},
		GetEdictStatusTool{chancellor: c},
		ListEdictsTool{db: c.db},
		InvokeMinisterTool{chancellor: c},
	}
}

// Execute is a stub to satisfy the Minister interface.
// Chancellor orchestration is now handled via the invoke_minister tool.
func (c *Chancellor) Execute(ctx context.Context, edictID string) (bool, error) {
	c.logger.Debug("chancellor execute called", "edict_id", edictID)
	return false, nil
}

// HandleZhengmingResponse processes a clarification response
func (c *Chancellor) HandleZhengmingResponse(ctx context.Context, requestID, answer string) error {
	// Answer the zhengming
	if err := c.AnswerZhengming(requestID, answer); err != nil {
		return fmt.Errorf("answer zhengming: %w", err)
	}

	// Get the request to find the edict
	requests, err := c.GetPendingZhengming("")
	if err != nil {
		return fmt.Errorf("get requests: %w", err)
	}

	// Find the edict ID from the answered request
	// TODO: look up the request directly
	for _, req := range requests {
		if req.RequestID == requestID {
			// Append clarification to edict
			if err := c.AppendToRenIntent(req.EdictID, answer); err != nil {
				return fmt.Errorf("append clarification: %w", err)
			}
			break
		}
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

// CreateEdictFromIssue creates a new edict from a GitHub issue
func (c *Chancellor) CreateEdictFromIssue(ctx context.Context, edictID, issueBody string) error {
	if err := c.CreateEdict(edictID, issueBody); err != nil {
		return fmt.Errorf("create edict: %w", err)
	}

	// Emit edict created event
	c.EmitEvent(edictID, "edict_assigned", storage.JSON(`{"source":"github_issue"}`))

	c.logger.Info("edict created", "edict_id", edictID)
	return nil
}

// CancelEdictWithContext cancels an edict (context-aware variant)
func (c *Chancellor) CancelEdictWithContext(ctx context.Context, edictID, cancelledBy, reason string) error {
	if err := c.CancelEdict(edictID, cancelledBy, reason); err != nil {
		return err
	}

	c.EmitEvent(edictID, "edict_cancelled", storage.JSON(
		fmt.Sprintf(`{"cancelled_by":"%s","reason":"%s"}`, cancelledBy, reason)))

	c.logger.Info("edict cancelled", "edict_id", edictID, "by", cancelledBy)
	return nil
}

// --- Ritual Selection ---

// SelectRitualType determines the ritual type for an edict.
// Returns empty string if the type is unknown and Zhengming should be requested.
func (c *Chancellor) SelectRitualType(ctx context.Context, edictID string) (RitualType, error) {
	edict, err := c.GetEdict(edictID)
	if err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	// If type is set and valid, use it
	if edict.Type != "" {
		ritualType := RitualType(edict.Type)
		if ritualType.IsEdictLevel() {
			return ritualType, nil
		}
	}

	// Unknown type - needs Zhengming
	return "", nil
}

// RequestRitualTypeZhengming requests clarification on the edict type from the Ruler.
// Call this when SelectRitualType returns empty string.
func (c *Chancellor) RequestRitualTypeZhengming(ctx context.Context, edictID string, notify NotifyFunc) (string, error) {
	edict, err := c.GetEdict(edictID)
	if err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	question := fmt.Sprintf("What type of work is this edict?\n\nIntent: %s\n\nOptions: feature, bugfix, hotfix, chore", edict.RenIntent)

	requestID, err := c.RequestZhengming(edictID, question, storage.PriorityNormal)
	if err != nil {
		return "", fmt.Errorf("request zhengming: %w", err)
	}

	// Notify UI
	if notify != nil {
		notify(ZhengmingPendingMsg{
			RequestID:  requestID,
			EdictID:    edictID,
			MinisterID: "chancellor",
			Question:   question,
			Priority:   storage.PriorityNormal,
		})
	}

	return requestID, nil
}

// UpdateEdictType sets the edict's ritual type.
// Call this after Zhengming response clarifies the type.
func (c *Chancellor) UpdateEdictType(edictID string, edictType RitualType) error {
	result := c.db.Model(&storage.Edict{}).
		Where("edict_id = ?", edictID).
		Update("type", string(edictType))
	if result.Error != nil {
		return fmt.Errorf("failed to update edict type: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("edict not found: %s", edictID)
	}
	return nil
}

// GetDB returns the database connection (for ritual building)
func (c *Chancellor) GetDB() *gorm.DB {
	return c.db
}

// --- Chancellor Tools ---

// CreateEdictTool creates a new edict from the user's request.
type CreateEdictTool struct {
	chancellor *Chancellor
}

func (t CreateEdictTool) Name() string {
	return "create_edict"
}

func (t CreateEdictTool) Description() string {
	return "Creates a new edict (work order) from the user's request. Use this when the user asks you to implement a feature, fix a bug, or make changes to the codebase. The edict_id should be a unique identifier like 'issue-123' or 'feature-user-auth'."
}

func (t CreateEdictTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID string `json:"edict_id"`
		Intent  string `json:"intent"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Intent == "" {
		return "", fmt.Errorf("intent is required")
	}

	if err := t.chancellor.CreateEdict(params.EdictID, params.Intent); err != nil {
		return "", fmt.Errorf("create edict: %w", err)
	}

	// Emit event for edict assignment
	t.chancellor.EmitEvent(params.EdictID, "edict_assigned", storage.JSON(`{"source":"chancellor"}`))

	return fmt.Sprintf(`{"status":"created","edict_id":"%s"}`, params.EdictID), nil
}

func (t CreateEdictTool) Format(input, result string, err error) string {
	var params struct {
		EdictID string `json:"edict_id"`
	}
	json.Unmarshal([]byte(input), &params)

	if err != nil {
		return fmt.Sprintf("Create Edict %s Error: %v\n", params.EdictID, err)
	}
	return fmt.Sprintf("Create Edict %s Created\n", params.EdictID)
}

func (t CreateEdictTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "string",
				"description": "Unique identifier for the edict (e.g., 'issue-123', 'feature-auth')",
			},
			"intent": map[string]any{
				"type":        "string",
				"description": "The user's intent - what they want to accomplish",
			},
		},
		"required": []string{"edict_id", "intent"},
	}
}

// RequestZhengmingTool requests clarification from the user.
type RequestZhengmingTool struct {
	chancellor *Chancellor
	notify     NotifyFunc
}

func (t RequestZhengmingTool) Name() string {
	return "request_zhengming"
}

func (t RequestZhengmingTool) Description() string {
	return "Request clarification from the user (Zhengming - 正名) when requirements are ambiguous. Use this when you need more information before proceeding with an edict. The edict will be halted until the user responds."
}

func (t RequestZhengmingTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID  string `json:"edict_id"`
		Question string `json:"question"`
		Priority string `json:"priority"` // "low", "normal", "urgent"
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Question == "" {
		return "", fmt.Errorf("question is required")
	}

	// Default priority
	priority := storage.PriorityNormal
	if params.Priority != "" {
		priority = storage.ZhengmingPriority(params.Priority)
	}

	requestID, err := t.chancellor.RequestZhengming(params.EdictID, params.Question, priority)
	if err != nil {
		return "", fmt.Errorf("request zhengming: %w", err)
	}

	// Notify TUI if callback is set
	if t.notify != nil {
		t.notify(ZhengmingPendingMsg{
			RequestID:  requestID,
			EdictID:    params.EdictID,
			MinisterID: "chancellor",
			Question:   params.Question,
			Priority:   priority,
		})
	}

	return fmt.Sprintf(`{"status":"pending","request_id":"%s"}`, requestID), nil
}

func (t RequestZhengmingTool) Format(input, result string, err error) string {
	var params struct {
		Question string `json:"question"`
	}
	json.Unmarshal([]byte(input), &params)

	if err != nil {
		return fmt.Sprintf("Zhengming Error: %v\n", err)
	}
	// Truncate question if too long
	q := params.Question
	if len(q) > 50 {
		q = q[:47] + "..."
	}
	return fmt.Sprintf("Zhengming %s\n", q)
}

func (t RequestZhengmingTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "string",
				"description": "The edict ID this question relates to",
			},
			"question": map[string]any{
				"type":        "string",
				"description": "The clarification question to ask the user",
			},
			"priority": map[string]any{
				"type":        "string",
				"enum":        []string{"low", "normal", "urgent"},
				"description": "Priority of the clarification request",
			},
		},
		"required": []string{"edict_id", "question"},
	}
}

// GetEdictStatusTool retrieves the status of an edict.
type GetEdictStatusTool struct {
	chancellor *Chancellor
}

func (t GetEdictStatusTool) Name() string {
	return "get_edict_status"
}

func (t GetEdictStatusTool) Description() string {
	return "Gets the current status and phase of an edict. Use this to check progress on a work order."
}

func (t GetEdictStatusTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID string `json:"edict_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}

	edict, err := t.chancellor.GetEdict(params.EdictID)
	if err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	result := map[string]any{
		"edict_id":           edict.EdictID,
		"phase":              string(edict.CurrentPhase),
		"chancellor_seal":    edict.ChancellorSeal,
		"censor_seal":        edict.CensorSeal,
		"ren_intent_version": edict.RenIntentVersion,
	}

	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

func (t GetEdictStatusTool) Format(input, result string, err error) string {
	var params struct {
		EdictID string `json:"edict_id"`
	}
	json.Unmarshal([]byte(input), &params)

	if err != nil {
		return fmt.Sprintf("Edict Status %s Error: %v\n", params.EdictID, err)
	}
	var res struct {
		Phase string `json:"phase"`
	}
	json.Unmarshal([]byte(result), &res)
	return fmt.Sprintf("Edict Status %s [%s]\n", params.EdictID, res.Phase)
}

func (t GetEdictStatusTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "string",
				"description": "The edict ID to check status for",
			},
		},
		"required": []string{"edict_id"},
	}
}

// ListEdictsTool lists all edicts with optional filtering.
type ListEdictsTool struct {
	db *gorm.DB
}

func (t ListEdictsTool) Name() string {
	return "list_edicts"
}

func (t ListEdictsTool) Description() string {
	return "Lists all edicts, optionally filtered by phase. Use this to see what work orders exist."
}

func (t ListEdictsTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Phase string `json:"phase"`
		Limit int    `json:"limit"`
	}
	json.Unmarshal([]byte(input), &params)

	if params.Limit <= 0 {
		params.Limit = 20
	}

	var edicts []storage.Edict
	query := t.db.Order("created_at DESC").Limit(params.Limit)
	if params.Phase != "" {
		query = query.Where("current_phase = ?", params.Phase)
	}

	if err := query.Find(&edicts).Error; err != nil {
		return "", fmt.Errorf("list edicts: %w", err)
	}

	var results []map[string]any
	for _, e := range edicts {
		results = append(results, map[string]any{
			"edict_id": e.EdictID,
			"phase":    string(e.CurrentPhase),
			"intent":   truncateString(e.RenIntent, 100),
		})
	}

	resultJSON, _ := json.Marshal(results)
	return string(resultJSON), nil
}

func (t ListEdictsTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("List Edicts Error: %v\n", err)
	}
	var edicts []map[string]any
	json.Unmarshal([]byte(result), &edicts)
	return fmt.Sprintf("List Edicts Found %d edicts\n", len(edicts))
}

func (t ListEdictsTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"phase": map[string]any{
				"type":        "string",
				"enum":        []string{"planning", "forging", "judgment", "review", "merged", "cancelled"},
				"description": "Filter by phase (optional)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of edicts to return (default: 20)",
			},
		},
	}
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// AsimiSQLTool executes SQL queries against the Shogunate database.
type AsimiSQLTool struct {
	dbPath string
}

func (t AsimiSQLTool) Name() string {
	return "asimisql"
}

func (t AsimiSQLTool) Description() string {
	return `Execute SQL against the Shogunate database. Use for edict phase transitions:
- UPDATE edicts SET current_phase = 'merged' WHERE edict_id = '...';
- UPDATE edicts SET current_phase = 'planning' WHERE edict_id = '...';
Phases: brewing, planning, forging, judgment, review, merged, cancelled`
}

func (t AsimiSQLTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	// Execute via sqlite3 command
	// #nosec G204 - dbPath is controlled by the application
	cmd := execCommand("sqlite3", t.dbPath, params.Query)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sqlite3 error: %w: %s", err, string(output))
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return `{"status":"ok"}`, nil
	}
	return result, nil
}

func (t AsimiSQLTool) Format(input, result string, err error) string {
	var params struct {
		Query string `json:"query"`
	}
	json.Unmarshal([]byte(input), &params)

	q := params.Query
	if len(q) > 40 {
		q = q[:37] + "..."
	}

	if err != nil {
		return fmt.Sprintf("SQL Error: %v\n", err)
	}
	return fmt.Sprintf("SQL %s\n", q)
}

func (t AsimiSQLTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "SQL query to execute",
			},
		},
		"required": []string{"query"},
	}
}

// InvokeMinisterTool allows the Chancellor to invoke any registered minister for an edict.
type InvokeMinisterTool struct {
	chancellor *Chancellor
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
		Task       string `json:"task"`
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
	if params.Task == "" {
		return "", fmt.Errorf("task is required")
	}

	// Get minister via Shogunate
	minister := t.chancellor.shogunate.GetMinister(params.MinisterID)
	if minister == nil {
		return "", fmt.Errorf("minister not found: %s", params.MinisterID)
	}

	// Create per-call reply channel (synchronous blocking pattern)
	replyChan := make(chan *TaskReply, 1)

	// Create TaskEnvelope with per-call reply channel
	env := &TaskEnvelope{
		EdictID:   params.EdictID,
		Task:      params.Task,
		ReplyChan: replyChan,
	}

	// Send task to minister
	select {
	case minister.Tasks() <- env:
		t.chancellor.logger.Info("task sent to minister",
			"minister", params.MinisterID,
			"edict_id", params.EdictID,
			"task", truncateString(params.Task, 50))
	case <-ctx.Done():
		return "", fmt.Errorf("context cancelled while sending task to %s", params.MinisterID)
	}

	// Block until minister replies (only blocks this session's goroutine)
	select {
	case reply := <-replyChan:
		if reply.Error != nil {
			t.chancellor.logger.Error("task failed",
				"minister", params.MinisterID,
				"edict_id", params.EdictID,
				"error", reply.Error)
			return "", fmt.Errorf("minister %s failed: %w", params.MinisterID, reply.Error)
		}
		// TODO: update the edicts table

		t.chancellor.logger.Info("task completed",
			"minister", params.MinisterID,
			"edict_id", params.EdictID,
			"sealed", reply.Sealed,
			"output_len", len(reply.Output))

		result := map[string]any{
			"minister_id": params.MinisterID,
			"edict_id":    params.EdictID,
			"status":      "completed",
			"sealed":      reply.Sealed,
			"output":      reply.Output,
		}
		resultJSON, _ := json.Marshal(result)
		return string(resultJSON), nil

	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("minister %s timeout after 5 minutes", params.MinisterID)

	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (t InvokeMinisterTool) Format(input, result string, err error) string {
	var params struct {
		MinisterID string `json:"minister_id"`
		EdictID    string `json:"edict_id"`
		Task       string `json:"task"`
	}
	json.Unmarshal([]byte(input), &params)

	if err != nil {
		return fmt.Sprintf("Invoke %s Error: %v\n", params.MinisterID, err)
	}

	taskPreview := truncateString(params.Task, 30)
	return fmt.Sprintf("Invoke %s [%s]\n", params.MinisterID, taskPreview)
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
