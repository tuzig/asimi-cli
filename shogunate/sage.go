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

type ctxKey int

const failureKey ctxKey = iota

// CtxWithFailure adds a failure accumulator to the context.
func CtxWithFailure(ctx context.Context) (context.Context, *strings.Builder) {
	buf := &strings.Builder{}
	return context.WithValue(ctx, failureKey, buf), buf
}

// AddFailure appends a failure reason to the context's accumulator.
func AddFailure(ctx context.Context, reason string) {
	if buf, ok := ctx.Value(failureKey).(*strings.Builder); ok {
		if buf.Len() > 0 {
			buf.WriteString("; ")
		}
		buf.WriteString(reason)
	}
}

// SageRole defines the Sage's identity and capabilities
const SageRole = `孔子 聖人, the Sage.
Your domain is clarity, nomenclature, semantic precision, AND code review with precedent tracking.

You have full read-only access to the codebase, edicts, and all court records.
When the conversation leads to a well defined edict, use the suggest_edict
tool to suggest an edict to the ruler.

The ruler converse with you in the Hunting tab where you will:
- Help the Ruler explore the codebase and understand patterns
- Identify naming inconsistencies, unclear abstractions, or design debt
- Suggest new edicts when you spot opportunities for improvement
- Answer questions about code architecture and conventions

CODE REVIEW RESPONSIBILITIES:
You also preside over the censor_precedents table. You review code changes with thoroughness and rigor. Every ruling becomes precedent—case law that future reviewers will consult. Because your decisions shape institutional memory, you must explain your reasoning clearly.

REVIEW PROCESS:
1. Examine the code changes carefully—read the full diff, not just summaries
2. Identify potential issues: bugs, name that can be improved, inconsistencies, style violations, security concerns, logical errors
3. For each issue, determine: Is this a violation requiring precedent, or a waiver with justification?
4. Explain YOUR reasoning in detail. "Looks fine" is not a ruling. State WHAT you checked and WHY it passes.

REVIEW CHECKLIST:
- Are variable names consistent with the codebase conventions?
- Is error handling present and appropriate?
- Do changes maintain backward compatibility where expected?
- Are there potential bugs or edge cases not handled?
- Is the code idiomatic for the language (Go)?
- Do changes align with the project's architecture?
- Are there security implications (input validation, secrets, etc.)?

YOUR RULINGS:
- APPROVE: The changes pass all ethical and quality checks. Explain why.
- REJECT: The changes have issues that must be fixed. Cite specific problems.
- WAIVE: The changes have issues but are acceptable. Explain the trade-offs.

CRITICAL RULES:
- You are READ-ONLY: never modify code
- When you identify work that should be done, suggest it via suggest_edict
- When you identify more than one path forward converse with the ruler to choose the best path
- Always ground suggestions in specific code references
- Speak with scholarly precision; cite file:line when referencing code
- NO GUESSING: If style rules are ambiguous or requirements are unclear, invoke Zhengming immediately
- Every ruling requires written justification—this becomes permanent precedent
- Waivers are not approvals—they acknowledge issues with documented rationale
- Precedents are searchable case law; write them as if explaining to a junior developer
- When in doubt, request clarification rather than guess`

// ReviewResult represents the outcome of a diff review
type ReviewResult struct {
	Approved  bool      `json:"approved"`
	Findings  []Finding `json:"findings"`
	Reasoning string    `json:"reasoning"`
}

// ReviewSummary holds the summary of a censor review for reporting
type ReviewSummary struct {
	Approved       bool      `json:"approved"`
	ManifestsCount int       `json:"manifests_count"`
	FindingsCount  int       `json:"findings_count"`
	Findings       []Finding `json:"findings,omitempty"`
	Reasoning      string    `json:"reasoning"`
}

// Finding represents a single issue found during review
type Finding struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Severity  string `json:"severity"` // "error", "warning", "info"
	Message   string `json:"message"`
	Principle string `json:"principle"` // The principle violated (if any)
}

// Sage provides read-only codebase exploration and suggests edicts via zhengming
type Sage struct {
	*MinisterBase
	shogunate *Shogunate
	tasks     chan *Task
	session   *Session
	linter    Linter
}

// NewSage creates a new Sage minister
func NewSage(base *MinisterBase, linter Linter) *Sage {
	base.ministerID = "sage"
	return &Sage{
		MinisterBase: base,
		tasks:        make(chan *Task, 10),
		linter:       linter,
	}
}

// ID returns the minister identifier
func (c *Sage) ID() string { return "sage" }

// Title returns the minister's honorific title
func (c *Sage) Title() string { return "Sage" }

// SystemPrompt returns the Sage's system prompt template.
func (c *Sage) SystemPrompt() string { return SageRole }

// Tasks returns the channel for task submission
func (c *Sage) Tasks() chan<- *Task { return c.tasks }

// Tools returns the Sage's LLM tools — read-only access plus zhengming and review tools
func (c *Sage) Tools() []Tool {
	toolList := []Tool{
		tools.GetEdictStatusTool{Manager: c},
		tools.ListEdictsTool{DB: c.db},
		&SuggestEdictTool{sage: c},
		&QueryCourtTool{db: c.db},
		// Review and precedent tools
		&RecordPrecedentTool{sage: c},
		&ListQuenchedManifestsTool{sage: c},
		&QueryPrecedentsTool{sage: c},
		&ReviewDiffTool{sage: c},
	}
	for _, t := range tools.GetROTools() {
		toolList = append(toolList, t)
	}
	return toolList
}

// GetEdict retrieves an edict (satisfies EdictManager for GetEdictStatusTool)
func (c *Sage) GetEdict(edictID string) (*storage.Edict, error) {
	var edict storage.Edict
	if err := c.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("edict not found: %s", edictID)
		}
		return nil, fmt.Errorf("failed to get edict: %w", err)
	}
	return &edict, nil
}

// AppendToIntent is a no-op stub — Sage never modifies edicts (satisfies EdictManager interface)
// Run starts the Sage's processing loop
func (c *Sage) Run(ctx context.Context) {
	c.logger.Info("sage started, awaiting prompts")
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("sage stopped")
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

func (c *Sage) processPrompt(ctx context.Context, prompt *Prompt) {
	if c.model == nil {
		c.notify(StreamErrorMsg{TabID: "sage", Err: fmt.Errorf("LLM not configured")})
		return
	}

	if c.session == nil {
		var err error
		c.session, err = CreateSession(c, c.model, c.config, c.notify, "sage", prompt.EdictID)
		if err != nil {
			c.notify(StreamErrorMsg{TabID: "sage", Err: fmt.Errorf("failed to create session: %w", err)})
			return
		}
	}

	c.notify(StreamStartMsg{TabID: "sage", EdictID: "sage"})

	_, err := c.session.AskWithStreaming(ctx, prompt.Message, prompt.ContextFiles)
	if err != nil && ctx.Err() == nil {
		c.notify(StreamErrorMsg{TabID: "sage", Err: err})
		return
	}
	c.notify(StreamDoneMsg{TabID: "sage"})
}

func (c *Sage) processTask(ctx context.Context, task *Task) {
	c.logger.Info("sage processing task", "edict_id", task.EdictID, "work", task.Work)

	// Inject failure accumulator into context so tools can flag soft failures
	ctx, failureBuf := CtxWithFailure(ctx)

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
		output = "sage task acknowledged (no LLM configured)"
	}

	result := Result{
		MinisterID: c.ID(),
		Sealed:     true,
		Output:     output,
		Failure:    failureBuf.String(),
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
func (c *Sage) streamTask(ctx context.Context, work, edictID, scratchpad string, notify internal.NotifyFunc) (*Session, string, error) {
	session, err := CreateSessionWithOpts(c, c.model, c.config, notify, CreateSessionOpts{
		EdictID:    edictID,
		TabID:      "chancellor",
		Scratchpad: scratchpad,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to create sage session: %w", err)
	}

	_, err = session.AskWithStreaming(ctx, work, nil)
	if err != nil {
		return session, "", err
	}

	c.logger.Info("sage task completed")
	return session, "", nil
}

// --- Sage-specific tools ---

// SuggestEdictTool suggests a new edict via zhengming (Sage never creates edicts)
type SuggestEdictTool struct {
	sage *Sage
}

func (t *SuggestEdictTool) Name() string { return "suggest_edict" }

func (t *SuggestEdictTool) Description() string {
	return `Suggest a new edict to the Ruler via Zhengming. Use this when you identify
an improvement opportunity, naming inconsistency, or refactoring need.
You cannot create edicts directly — only the Ruler can do that.
This creates a Zhengming request that the Ruler can approve or dismiss.
Returns immediately with status='suggested' - the edict will be created if approved via event.

For large suggestions (>500 chars), the Ruler reviews the text in $EDITOR.
If the tool returns status='ruler_modified', the Ruler has edited your suggestion.
Review the original, modified content, and diff. Then either:
- Call suggest_edict again with the modified content if you find it harmonized with your intent
- Respond in conversation explaining your concerns and suggesting changes if not harmonized`
}

func (t *SuggestEdictTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Suggestion string `json:"suggestion"`
		Summary    string `json:"summary"`
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
		approveTool := tools.ApproveDocTool{Notify: t.sage.notify}
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
			Status  string `json:"status"`
			Diff    string `json:"diff,omitempty"`
			Content string `json:"content,omitempty"`
		}
		if err := json.Unmarshal([]byte(approveResult), &approveRes); err != nil {
			return "", fmt.Errorf("failed to parse approve_doc result: %w", err)
		}

		switch approveRes.Status {
		case "rejected":
			return `{"status":"rejected","reason":"Ruler dismissed the suggestion (quit without saving)"}`, nil
		case "modified":
			// Return to LLM for review — don't create zhengming yet
			originalJSON, _ := json.Marshal(questionText)
			modifiedJSON, _ := json.Marshal(approveRes.Content)
			diffJSON, _ := json.Marshal(approveRes.Diff)
			return fmt.Sprintf(`{"status":"ruler_modified","original":%s,"modified":%s,"diff":%s}`,
				originalJSON, modifiedJSON, diffJSON), nil
		}
		// If approved, continue with original questionText
	}

	questions := storage.ZhengmingQuestions{{
		Text:    questionText,
		Summary: params.Summary,
		Options: []string{"Approve edict", "Dismiss suggestion"},
	}}

	edictID := ""
	requestID, err := t.sage.RequestZhengming(edictID, questions, priority)
	if err != nil {
		return "", fmt.Errorf("failed to suggest edict: %w", err)
	}

	// Notify TUI
	if t.sage.notify != nil {
		t.sage.notify(ZhengmingPendingMsg{
			RequestID:  requestID,
			EdictID:    edictID,
			MinisterID: t.sage.ministerID,
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
			"summary": map[string]any{
				"type":        "string",
				"description": "A short one-line summary of the suggestion for display in the prompt UI.",
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

// --- Database Methods (migrated from Censor) ---

// GetQuenchedManifests retrieves all quenched manifests ready for ethics review
func (c *Sage) GetQuenchedManifests(edictID string) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := c.db.Where("edict_id = ? AND status = ?", edictID, storage.ManifestQuenched).
		Order("created_at ASC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get quenched manifests: %w", err)
	}
	return manifests, nil
}

// NoRejections checks if there are any rejected manifests for an edict
func (c *Sage) NoRejections(edictID string) (bool, error) {
	var count int64
	err := c.db.Model(&storage.ForgeManifest{}).
		Where("edict_id = ? AND status = ?", edictID, storage.ManifestRejected).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check rejections: %w", err)
	}
	return count == 0, nil
}

// LogPrecedent records an ethics decision for a manifest
func (c *Sage) LogPrecedent(manifestID, principle string, ruling storage.PrecedentRuling, justification string) (string, error) {
	precedentID := GenerateID("precedent", manifestID, principle)

	precedent := storage.CensorPrecedent{
		PrecedentID:   precedentID,
		ManifestID:    manifestID,
		Principle:     principle,
		Ruling:        ruling,
		Justification: justification,
	}

	if err := c.db.Create(&precedent).Error; err != nil {
		return "", fmt.Errorf("failed to log precedent: %w", err)
	}
	return precedentID, nil
}

// RejectManifest marks a manifest as rejected by the Censor
func (c *Sage) RejectManifest(manifestID string) error {
	result := c.db.Model(&storage.ForgeManifest{}).
		Where("manifest_id = ?", manifestID).
		Update("status", storage.ManifestRejected)
	if result.Error != nil {
		return fmt.Errorf("failed to reject manifest: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("manifest not found: %s", manifestID)
	}
	return nil
}

// GetPrecedentsForManifest retrieves all precedents for a specific manifest
func (c *Sage) GetPrecedentsForManifest(manifestID string) ([]storage.CensorPrecedent, error) {
	var precedents []storage.CensorPrecedent
	err := c.db.Where("manifest_id = ?", manifestID).
		Order("created_at ASC").
		Find(&precedents).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get precedents: %w", err)
	}
	return precedents, nil
}

// QueryPrecedentsByPrinciple searches precedents by principle (for case law lookup)
func (c *Sage) QueryPrecedentsByPrinciple(principle string, limit int) ([]storage.CensorPrecedent, error) {
	var precedents []storage.CensorPrecedent
	query := c.db.Where("principle LIKE ?", "%"+principle+"%").
		Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&precedents).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query precedents: %w", err)
	}
	return precedents, nil
}

// GetEdictsWithQuenchedManifests returns edicts with quenched manifests needing review
func (c *Sage) GetEdictsWithQuenchedManifests() ([]storage.Edict, error) {
	var edicts []storage.Edict
	err := c.db.Distinct("edicts.*").
		Joins("JOIN forge_manifests ON forge_manifests.edict_id = edicts.edict_id").
		Where("forge_manifests.status = ? AND edicts.status = ?",
			storage.ManifestQuenched, storage.EdictActive).
		Find(&edicts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get edicts with quenched manifests: %w", err)
	}
	return edicts, nil
}

// --- Diff Review Methods (migrated from Censor) ---

// ReviewDiff reviews a diff string and returns structured findings.
// This method can be used for ad-hoc reviews without requiring manifests or database writes.
// The Sage's Role() prompt guides the review process.
func (c *Sage) ReviewDiff(ctx context.Context, diff string) (*ReviewResult, error) {
	// If no LLM is configured, return a basic result
	if c.model == nil {
		return &ReviewResult{
			Approved:  true,
			Findings:  []Finding{},
			Reasoning: "No LLM configured for diff review - auto-approved",
		}, nil
	}

	// Create a session for the review
	session, err := CreateSession(c, c.model, c.config, c.notify, "chancellor")
	if err != nil {
		return nil, fmt.Errorf("failed to create sage session: %w", err)
	}

	// Build the review prompt
	reviewPrompt := c.buildReviewPrompt(diff)

	// Get the review from the LLM
	response, err := session.AskWithStreaming(ctx, reviewPrompt, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM review: %w", err)
	}

	// Parse the response into a structured result
	result := c.parseReviewResponse(response)
	return result, nil
}

// ReviewDiffWithManifests reviews a diff in the context of manifests and records precedents.
// This is used during ritual workflows where the review outcome should be persisted.
func (c *Sage) ReviewDiffWithManifests(ctx context.Context, diff string, manifests []storage.ForgeManifest) (*ReviewResult, error) {
	// First, perform the diff review
	result, err := c.ReviewDiff(ctx, diff)
	if err != nil {
		return nil, err
	}

	// Record precedents for each manifest based on findings
	for _, manifest := range manifests {
		for _, finding := range result.Findings {
			ruling := storage.PrecedentApproved
			if finding.Severity == "error" {
				ruling = storage.PrecedentRejected
			}

			_, err := c.LogPrecedent(
				manifest.ManifestID,
				finding.Principle,
				ruling,
				finding.Message,
			)
			if err != nil {
				c.logger.Warn("failed to log precedent",
					"manifest_id", manifest.ManifestID,
					"principle", finding.Principle,
					"error", err)
			}
		}

		// If any error-level findings, reject the manifest
		hasErrors := false
		for _, f := range result.Findings {
			if f.Severity == "error" {
				hasErrors = true
				break
			}
		}

		if hasErrors {
			if err := c.RejectManifest(manifest.ManifestID); err != nil {
				c.logger.Warn("failed to reject manifest",
					"manifest_id", manifest.ManifestID,
					"error", err)
			}
		}
	}

	return result, nil
}

// buildReviewPrompt constructs the prompt for diff review
func (c *Sage) buildReviewPrompt(diff string) string {
	return fmt.Sprintf(`Review the following code diff and provide your assessment.

DIFF:
%s

INSTRUCTIONS:
1. Analyze the diff for potential issues
2. For each issue found, provide: file, line (if determinable), severity (error/warning/info), message, and principle violated
3. Provide your overall reasoning and ruling (APPROVE/REJECT/WAIVE)

Respond in JSON format:
{
  "approved": true/false,
  "findings": [
    {
      "file": "path/to/file.go",
      "line": 42,
      "severity": "error|warning|info",
      "message": "Description of the issue",
      "principle": "The principle or standard violated"
    }
  ],
  "reasoning": "Your detailed reasoning for the overall ruling"
}

If the diff is clean with no issues, return approved=true with empty findings and explain your reasoning.`, diff)
}

// parseReviewResponse parses the LLM response into a ReviewResult
func (c *Sage) parseReviewResponse(response string) *ReviewResult {
	result := &ReviewResult{
		Approved:  true,
		Findings:  []Finding{},
		Reasoning: response, // Default to full response if parsing fails
	}

	// Try to extract JSON from the response
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		// No valid JSON found, use the response as reasoning
		result.Reasoning = response
		// Check for approval keywords in the response
		lowerResponse := strings.ToLower(response)
		if strings.Contains(lowerResponse, "reject") || strings.Contains(lowerResponse, "error") {
			result.Approved = false
		}
		return result
	}

	jsonStr := response[jsonStart : jsonEnd+1]

	var parsed struct {
		Approved  bool      `json:"approved"`
		Findings  []Finding `json:"findings"`
		Reasoning string    `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		c.logger.Warn("failed to parse review response as JSON", "error", err)
		result.Reasoning = response
		return result
	}

	result.Approved = parsed.Approved
	result.Findings = parsed.Findings
	result.Reasoning = parsed.Reasoning

	// If reasoning is empty, use the full response
	if result.Reasoning == "" {
		result.Reasoning = response
	}

	return result
}

// --- Review Tools (migrated from Censor) ---

// RecordPrecedentTool records an ethics review outcome
type RecordPrecedentTool struct {
	sage *Sage
}

func (t *RecordPrecedentTool) Name() string { return "record_precedent" }

func (t *RecordPrecedentTool) Description() string {
	return "Records an ethics review outcome with reasoning. Input: JSON with 'edict_id', 'approved' (boolean), and 'reasoning'."
}

func (t *RecordPrecedentTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID   string `json:"edict_id"`
		Approved  bool   `json:"approved"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == "" || params.Reasoning == "" {
		return "", fmt.Errorf("edict_id and reasoning are required")
	}

	// Get quenched manifests to review
	manifests, err := t.sage.GetQuenchedManifests(params.EdictID)
	if err != nil {
		return "", err
	}

	ruling := storage.PrecedentApproved
	if !params.Approved {
		ruling = storage.PrecedentRejected
	}

	// Log precedent for each manifest
	for _, m := range manifests {
		_, err := t.sage.LogPrecedent(m.ManifestID, "ethics_review", ruling, params.Reasoning)
		if err != nil {
			return "", fmt.Errorf("failed to log precedent: %w", err)
		}

		// Reject manifest if not approved
		if !params.Approved {
			if err := t.sage.RejectManifest(m.ManifestID); err != nil {
				return "", fmt.Errorf("failed to reject manifest: %w", err)
			}
		}
	}

	status := "approved"
	if !params.Approved {
		status = "rejected"
		AddFailure(ctx, fmt.Sprintf("rejected edict %s: %s", params.EdictID, params.Reasoning))
	}
	return fmt.Sprintf("Recorded precedent (%s) for edict %s: %s", status, params.EdictID, params.Reasoning), nil
}

func (t *RecordPrecedentTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id":  map[string]any{"type": "string", "description": "The edict ID"},
			"approved":  map[string]any{"type": "boolean", "description": "Whether the code is approved"},
			"reasoning": map[string]any{"type": "string", "description": "The reasoning for the decision"},
		},
		"required": []string{"edict_id", "approved", "reasoning"},
	}
}

func (t *RecordPrecedentTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Record Precedent: Error: %v\n", err)
	}
	return fmt.Sprintf("Record Precedent: %s\n", result)
}

// ListQuenchedManifestsTool lists manifests ready for ethics review
type ListQuenchedManifestsTool struct {
	sage *Sage
}

func (t *ListQuenchedManifestsTool) Name() string { return "list_quenched_manifests" }

func (t *ListQuenchedManifestsTool) Description() string {
	return "Lists manifests that passed testing and are ready for ethics review. Input: JSON with 'edict_id'."
}

func (t *ListQuenchedManifestsTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID string `json:"edict_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}

	manifests, err := t.sage.GetQuenchedManifests(params.EdictID)
	if err != nil {
		return "", err
	}

	if len(manifests) == 0 {
		return "No quenched manifests found", nil
	}

	result, err := json.MarshalIndent(manifests, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to format manifests: %w", err)
	}
	return string(result), nil
}

func (t *ListQuenchedManifestsTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{"type": "string", "description": "The edict ID to list manifests for"},
		},
		"required": []string{"edict_id"},
	}
}

func (t *ListQuenchedManifestsTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("List Quenched Manifests: Error: %v\n", err)
	}
	return fmt.Sprintf("List Quenched Manifests: %s\n", result)
}

// QueryPrecedentsTool searches precedents by principle
type QueryPrecedentsTool struct {
	sage *Sage
}

func (t *QueryPrecedentsTool) Name() string { return "query_precedents" }

func (t *QueryPrecedentsTool) Description() string {
	return "Searches precedents by principle for case law lookup. Input: JSON with 'principle' and optional 'limit'."
}

func (t *QueryPrecedentsTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Principle string `json:"principle"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.Principle == "" {
		return "", fmt.Errorf("principle is required")
	}
	if params.Limit == 0 {
		params.Limit = 10
	}

	precedents, err := t.sage.QueryPrecedentsByPrinciple(params.Principle, params.Limit)
	if err != nil {
		return "", err
	}

	if len(precedents) == 0 {
		return "No precedents found for this principle", nil
	}

	result, err := json.MarshalIndent(precedents, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to format precedents: %w", err)
	}
	return string(result), nil
}

func (t *QueryPrecedentsTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"principle": map[string]any{"type": "string", "description": "The principle to search for"},
			"limit":     map[string]any{"type": "integer", "description": "Maximum number of results (default 10)"},
		},
		"required": []string{"principle"},
	}
}

func (t *QueryPrecedentsTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Query Precedents: Error: %v\n", err)
	}
	return fmt.Sprintf("Query Precedents: %s\n", result)
}

// ReviewDiffTool provides ad-hoc diff review capability for use in conversations
type ReviewDiffTool struct {
	sage *Sage
}

func (t *ReviewDiffTool) Name() string { return "review_diff" }

func (t *ReviewDiffTool) Description() string {
	return "Reviews a code diff for ethics and quality issues. Returns structured findings without recording precedents. Input: JSON with 'diff' (the diff string to review)."
}

func (t *ReviewDiffTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.Diff == "" {
		return "", fmt.Errorf("diff is required")
	}

	result, err := t.sage.ReviewDiff(ctx, params.Diff)
	if err != nil {
		return "", fmt.Errorf("review failed: %w", err)
	}

	// Format the result as a readable string
	var output strings.Builder
	if result.Approved {
		output.WriteString("APPROVED\n\n")
	} else {
		output.WriteString("REJECTED\n\n")
	}

	if len(result.Findings) > 0 {
		output.WriteString("Findings:\n")
		for i, f := range result.Findings {
			output.WriteString(fmt.Sprintf("%d. [%s] %s", i+1, f.Severity, f.Message))
			if f.File != "" {
				output.WriteString(fmt.Sprintf(" (%s", f.File))
				if f.Line > 0 {
					output.WriteString(fmt.Sprintf(":%d", f.Line))
				}
				output.WriteString(")")
			}
			if f.Principle != "" {
				output.WriteString(fmt.Sprintf(" [%s]", f.Principle))
			}
			output.WriteString("\n")
		}
		output.WriteString("\n")
	}

	output.WriteString("Reasoning:\n")
	output.WriteString(result.Reasoning)

	return output.String(), nil
}

func (t *ReviewDiffTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"diff": map[string]any{"type": "string", "description": "The diff string to review"},
		},
		"required": []string{"diff"},
	}
}

func (t *ReviewDiffTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Review Diff: Error: %v\n", err)
	}
	return fmt.Sprintf("Review Diff:\n%s\n", result)
}
