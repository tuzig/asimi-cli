package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/afittestide/asimi/internal"
	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// ConfuciusRole defines Confucius's identity and capabilities
const ConfuciusRole = `孔子,the Sage.
Your domain is clarity, nomenclature, and semantic precision.

You have full read-only access to the codebase, edicts, and all court records.
When the conversation leads to a well defined edict, use the suggest_edict
tool to suggest an edict to the ruler.

The ruler converse with you in the Hunting tab where you will:
- Help the Ruler explore the codebase and understand patterns
- Identify naming inconsistencies, unclear abstractions, or design debt
- Suggest new edicts when you spot opportunities for improvement
- Answer questions about code architecture and conventions

CRITICAL RULES:
- You are READ-ONLY: never modify code
- When you identify work that should be done, suggest it via suggest_edict
- When you identify more than one path forward converse with the ruler to choose the best path
- Always ground suggestions in specific code references
- Speak with scholarly precision; cite file:line when referencing code`

// Confucius provides read-only codebase exploration and suggests edicts via zhengming
type Confucius struct {
	*MinisterBase
	shogunate *Shogunate
	tasks     chan *Task
	session   *Session
}

// NewConfucius creates a new Confucius minister
func NewConfucius(base *MinisterBase) *Confucius {
	base.ministerID = "confucius"
	return &Confucius{
		MinisterBase: base,
		tasks:        make(chan *Task, 10),
	}
}

// ID returns the minister identifier
func (c *Confucius) ID() string { return "confucius" }

// Title returns the minister's honorific title
func (c *Confucius) Title() string { return "Confucius" }

// SystemPrompt returns Confucius's system prompt template.
func (c *Confucius) SystemPrompt() string { return ConfuciusRole }

// Tasks returns the channel for task submission
func (c *Confucius) Tasks() chan<- *Task { return c.tasks }

// Tools returns Confucius's LLM tools — read-only access plus zhengming
func (c *Confucius) Tools() []Tool {
	toolList := []Tool{
		tools.GetEdictStatusTool{Manager: c},
		tools.ListEdictsTool{DB: c.db},
		&SuggestEdictTool{confucius: c},
		&QueryCourtTool{db: c.db},
	}
	for _, t := range tools.GetROTools() {
		toolList = append(toolList, t)
	}
	return toolList
}

// GetEdict retrieves an edict (satisfies EdictManager for GetEdictStatusTool)
func (c *Confucius) GetEdict(edictID string) (*storage.Edict, error) {
	var edict storage.Edict
	if err := c.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("edict not found: %s", edictID)
		}
		return nil, fmt.Errorf("failed to get edict: %w", err)
	}
	return &edict, nil
}

// AppendToIntent is a no-op stub — Confucius never modifies edicts (satisfies EdictManager interface)
// Run starts Confucius's processing loop
func (c *Confucius) Run(ctx context.Context) {
	c.logger.Info("confucius started, awaiting prompts")
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("confucius stopped")
			return
		case prompt := <-c.PromptsChan():
			// Merge lifecycle ctx (shutdown) with per-prompt ctx (CTRL-C):
			// cancel when either fires.
			merged, mergedCancel := context.WithCancel(ctx)
			if prompt.Ctx != nil {
				context.AfterFunc(prompt.Ctx, func() { mergedCancel() })
			}
			c.processPrompt(merged, prompt)
			mergedCancel()
		case task := <-c.tasks:
			merged, mergedCancel := context.WithCancel(ctx)
			if task.Ctx != nil {
				context.AfterFunc(task.Ctx, func() { mergedCancel() })
			}
			c.processTask(merged, task)
			mergedCancel()
		}
	}
}

func (c *Confucius) processPrompt(ctx context.Context, prompt *Prompt) {
	if c.model == nil {
		c.notify(StreamErrorMsg{TabID: "confucius", Err: fmt.Errorf("LLM not configured")})
		return
	}

	if c.session == nil {
		var err error
		c.session, err = CreateSession(c, c.model, c.config, c.notify, "confucius", prompt.EdictID)
		if err != nil {
			c.notify(StreamErrorMsg{TabID: "confucius", Err: fmt.Errorf("failed to create session: %w", err)})
			return
		}
	}

	c.notify(StreamStartMsg{TabID: "confucius", EdictID: "confucius"})

	_, err := c.session.AskWithStreaming(ctx, prompt.Message, prompt.ContextFiles)
	if err != nil && ctx.Err() == nil {
		c.notify(StreamErrorMsg{TabID: "confucius", Err: err})
		return
	}
	c.notify(StreamDoneMsg{TabID: "confucius"})
}

func (c *Confucius) processTask(ctx context.Context, task *Task) {
	c.logger.Info("confucius processing task", "edict_id", task.EdictID, "work", task.Work)

	// Use task-level notify override for routing (e.g., ritual → Ruling tab)
	notify := c.notify
	if task.Notify != nil {
		notify = task.Notify
	}

	var output string
	var taskErr error
	var session *Session

	if c.model != nil {
		if task.Session != nil {
			// Multi-turn: continue existing session
			session = task.Session
			session.SetNotify(notify)
			_, taskErr = session.AskWithStreaming(ctx, task.Work, nil)
		} else {
			// First invocation: create new session
			session, output, taskErr = c.streamTask(ctx, task.Work, task.EdictID, task.Scratchpad, notify)
		}
	} else {
		output = "confucius task acknowledged (no LLM configured)"
	}

	result := Result{
		MinisterID: c.ID(),
		Sealed:     true,
		Output:     output,
		Session:    session,
		Err:        taskErr,
	}

	select {
	case task.Done <- result:
	default:
		c.logger.Warn("done channel full", "edict_id", task.EdictID)
	}
}

// streamTask creates a session and streams the task through the LLM.
// Returns the session for potential reuse in multi-turn conversations.
func (c *Confucius) streamTask(ctx context.Context, work, edictID, scratchpad string, notify internal.NotifyFunc) (*Session, string, error) {
	session, err := CreateSessionWithOpts(c, c.model, c.config, notify, CreateSessionOpts{
		EdictID:    edictID,
		TabID:      "chancellor",
		Scratchpad: scratchpad,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to create confucius session: %w", err)
	}

	_, err = session.AskWithStreaming(ctx, work, nil)
	if err != nil {
		return session, "", err
	}

	c.logger.Info("confucius task completed")
	return session, "", nil
}

// --- Confucius-specific tools ---

// SuggestEdictTool suggests a new edict via zhengming (Confucius never creates edicts)
type SuggestEdictTool struct {
	confucius *Confucius
}

func (t *SuggestEdictTool) Name() string { return "suggest_edict" }

func (t *SuggestEdictTool) Description() string {
	return `Suggest a new edict to the Ruler via Zhengming. Use this when you identify
an improvement opportunity, naming inconsistency, or refactoring need.
You cannot create edicts directly — only the Ruler can do that.
This creates a Zhengming request that the Ruler can approve or dismiss.
Returns immediately with status='suggested' - the edict will be created if approved via event.`
}

func (t *SuggestEdictTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Suggestion string `json:"suggestion"`
		Priority   string `json:"priority"`
		Evidence   string `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.Suggestion == "" {
		return "", fmt.Errorf("suggestion is required")
	}

	priority := storage.PriorityNormal
	if params.Priority == "urgent" {
		priority = storage.PriorityUrgent
	}

	// Build structured question with options
	questionText := params.Suggestion
	if params.Evidence != "" {
		questionText = fmt.Sprintf("%s\n\nEvidence: %s", params.Suggestion, params.Evidence)
	}

	// For large suggestions (>500 chars), use approve_doc for external review
	if len(questionText) > 500 {
		approveTool := tools.ApproveDocTool{}
		approveInput := map[string]any{
			"content":     questionText,
			"description": "Review edict suggestion before submission",
		}
		approveInputJSON, _ := json.Marshal(approveInput)
		approveResult, err := approveTool.Call(ctx, string(approveInputJSON))
		if err != nil {
			return "", fmt.Errorf("approve_doc failed: %w", err)
		}

		// Check if user approved or modified
		var approveRes struct {
			Status string `json:"status"`
			Diff   string `json:"diff,omitempty"`
		}
		if err := json.Unmarshal([]byte(approveResult), &approveRes); err != nil {
			return "", fmt.Errorf("failed to parse approve_doc result: %w", err)
		}

		if approveRes.Status == "modified" {
			// User modified the suggestion, use the modified version
			// Extract the modified content from the diff (simplified: assume user edited the whole thing)
			questionText = params.Suggestion + approveRes.Diff
		}
		// If approved, continue with original questionText
	}

	questions := storage.ZhengmingQuestions{{
		Text:    questionText,
		Options: []string{"Approve edict", "Dismiss suggestion"},
	}}

	edictID := ""
	requestID, err := t.confucius.RequestZhengming(edictID, questions, priority)
	if err != nil {
		return "", fmt.Errorf("failed to suggest edict: %w", err)
	}

	// Notify TUI
	if t.confucius.notify != nil {
		t.confucius.notify(ZhengmingPendingMsg{
			RequestID:  requestID,
			EdictID:    edictID,
			MinisterID: t.confucius.ministerID,
			Questions:  questions,
			Priority:   priority,
		})
	}

	// Return immediately - edict creation happens via event handler when user answers
	return fmt.Sprintf(`{"status":"suggested","request_id":"%s"}`, requestID), nil
}

func (t *SuggestEdictTool) Format(input, result string, err error) string {
	msg := utils.NewMsgBlockBuilder("SuggestEdict")
	msg.WriteLn()
	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		var params struct {
			Suggestion string `json:"suggestion"`
		}
		json.Unmarshal([]byte(input), &params)
		preview := params.Suggestion
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		msg.Writef("Suggested: %s", preview)
	}
	return msg.String() + "\n"
}

func (t *SuggestEdictTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"suggestion": map[string]any{
				"type":        "string",
				"description": "What edict should the Ruler consider? Be specific about the change.",
			},
			"priority": map[string]any{
				"type":        "string",
				"enum":        []string{"normal", "urgent"},
				"description": "Priority level (default: normal)",
			},
			"evidence": map[string]any{
				"type":        "string",
				"description": "Supporting evidence: file:line references, patterns found, etc.",
			},
		},
		"required": []string{"suggestion"},
	}
}

// QueryCourtTool queries the court's state (edicts, manifests, verdicts, precedents)
type QueryCourtTool struct {
	db *gorm.DB
}

func (t *QueryCourtTool) Name() string { return "query_court" }

func (t *QueryCourtTool) Description() string {
	return `Query the current state of the court. Returns active edicts, their phases,
recent manifests, verdicts, and precedents. Use this for a broad overview
of what's happening in the Shogunate.`
}

func (t *QueryCourtTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID string `json:"edict_id"`
		Scope   string `json:"scope"` // "active", "all", or specific edict_id
	}
	json.Unmarshal([]byte(input), &params)

	result := make(map[string]interface{})

	// Get edicts
	var edicts []storage.Edict
	query := t.db.Order("created_at DESC").Limit(20)
	if params.EdictID != "" {
		query = query.Where("edict_id = ?", params.EdictID)
	} else if params.Scope != "all" {
		query = query.Where("status NOT IN ?", []string{"sealed", "cancelled"})
	}
	query.Find(&edicts)

	edictSummaries := make([]map[string]interface{}, len(edicts))
	for i, e := range edicts {
		edictSummaries[i] = map[string]interface{}{
			"edict_id": e.EdictID,
			"status":   string(e.Status),
			"intent":   truncateForCourt(e.Intent, 120),
		}
	}
	result["edicts"] = edictSummaries

	// Get recent zhengming
	var zhengming []storage.Zhengming
	t.db.Where("status = ?", storage.ZhengmingPending).
		Order("created_at DESC").Limit(10).Find(&zhengming)
	if len(zhengming) > 0 {
		zhSummaries := make([]map[string]interface{}, len(zhengming))
		for i, z := range zhengming {
			zhSummaries[i] = map[string]interface{}{
				"request_id":  z.RequestID,
				"edict_id":    z.EdictID,
				"minister_id": z.MinisterID,
				"questions":   z.Questions,
				"priority":    string(z.Priority),
			}
		}
		result["pending_zhengming"] = zhSummaries
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

func (t *QueryCourtTool) Format(input, result string, err error) string {
	msg := utils.NewMsgBlockBuilder("QueryCourt")
	msg.WriteLn()
	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		// Count edicts in result
		var res map[string]interface{}
		json.Unmarshal([]byte(result), &res)
		if edicts, ok := res["edicts"].([]interface{}); ok {
			msg.Writef("Found %d edicts", len(edicts))
		}
	}
	return msg.String() + "\n"
}

func (t *QueryCourtTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "string",
				"description": "Optional: focus on a specific edict",
			},
			"scope": map[string]any{
				"type":        "string",
				"enum":        []string{"active", "all"},
				"description": "Scope of query: 'active' (default) or 'all'",
			},
		},
	}
}

func truncateForCourt(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
