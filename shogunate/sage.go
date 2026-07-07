package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// SageRole defines the Sage's identity and capabilities
const SageRole = `孔子 聖人, the Sage.
Your domain is clarity, nomenclature, semantic precision, AND code review with precedent tracking.

You have full read-only access to the codebase, edicts, and all court records.
When the conversation leads to a well defined edict, use the suggest_edict
tool to suggest an edict to the ruler.

Ruler Sessions:
- Help the Ruler explore the codebase and understand patterns
- Identify naming inconsistencies, unclear abstractions, or design debt
- Suggest new edicts when you spot opportunities for improvement
- Answer questions about code architecture and conventions

CODE REVIEW RESPONSIBILITIES:
You review code changes with thoroughness and rigor. You record every ruling using record_precedent  for future reviewers. Because your decisions shape institutional memory, you must explain your reasoning clearly.

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
- ZHENGMING CHECKPOINT: When suggest_edict returns status='ruler_modified', you MUST:
  1. Read the diff carefully - compare original suggestion vs. Ruler's modifications
  2. Make explicit determination in conversation - state whether changes harmonize with original intent
  3. Take appropriate action:
     - If harmonized: Call suggest_edict again with the modified content to re-submit
     - If not harmonized: Explain concerns in conversation and propose alternative wording
  This preserves Zhengming (semantic alignment) and maintains audit trail (Sage approves final form).
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
	linter    Linter
}

// NewSage creates a new Sage minister
func NewSage(base *MinisterBase, linter Linter) *Sage {
	base.ministerID = "sage"
	c := &Sage{
		MinisterBase: base,
		linter:       linter,
	}
	c.self = c
	return c
}

// Title returns the minister's honorific title
func (c *Sage) Title() string { return "Sage" }

// SystemPrompt returns the Sage's system prompt template.
func (c *Sage) SystemPrompt() string { return SageRole }

// Tools returns the Sage's LLM tools — read-only access plus review tools
func (c *Sage) Tools() []Tool {
	if c.toolRegistry != nil {
		perm, _ := tools.ParsePermissions("r--r--rwx")
		registered := c.toolRegistry.ForPermissions("sage", perm)
		result := make([]Tool, len(registered))
		for i, t := range registered {
			result[i] = t
		}
		return result
	}
	// Fallback: legacy tool list when registry is not yet wired
	tc := tools.ToolContext{
		RepoInfo:   &repo.RepoInfo{},
		MinisterID: c.ministerID,
		Username:   c.username,
		Project:    c.project,
		DB:         c.db,
	}
	*tc.RepoInfo = c.RepoInfo()
	toolList := []Tool{
		tools.RequestZhengmingTool{MinisterID: c.ministerID, Requester: c, WaitForAnswer: c.WaitForZhengming, Username: c.username, Project: c.project},
		tools.GetEdictStatusTool{Manager: c, DB: c.db, Username: c.username, Project: c.project},
		tools.ListEdictsTool{DB: c.db, Username: c.username, Project: c.project},
		tools.SuggestEdictTool{
			Ctx:       tc,
			Requester: c,
			NotifyFn:  func() func(any) { return c.notify },
		},
		tools.QueryCourtTool{DB: c.db, Username: c.username, Project: c.project},
		tools.RecordPrecedentTool{
			Ctx: tc,
		},
		tools.ListQuenchedManifestsTool{Ctx: tc},
		tools.QueryPrecedentsTool{Ctx: tc},
	}
	for _, t := range tools.GetROTools(c.config.LLM, c.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	return toolList
}

// GrantSeal exposes MinisterBase's seal-granting method so the Sage can
// grant seals directly (e.g., during ritual workflows) without importing
// shogunate-internal helpers.
func (c *Sage) GrantSeal(key storage.EdictKey, metadata storage.JSON) error {
	return c.grantSeal(key, metadata)
}

// ResetSession clears the Sage's session (delegates to MinisterBase)
func (c *Sage) ResetSession() {
	c.MinisterBase.ResetSession()
}

// GetSession returns the Sage's session (from MinisterBase)
func (c *Sage) GetSession() *Session {
	return c.MinisterBase.Session()
}

// GetEdict retrieves an edict (satisfies EdictManager for GetEdictStatusTool)
func (c *Sage) GetEdict(key storage.EdictKey) (*storage.Edict, error) {
	var edict storage.Edict
	if err := c.db.First(&edict, "id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("edict not found: %d", key.ID)
		}
		return nil, fmt.Errorf("failed to get edict: %w", err)
	}
	return &edict, nil
}

// AppendToIntent is a no-op stub — Sage never modifies edicts (satisfies EdictManager interface)

// Run starts the Sage's processing loop
func (c *Sage) Run(ctx context.Context) {
	c.RunLoop(ctx, c, c.processPrompt, c.MinisterBase.processTask)
}

func (c *Sage) processPrompt(ctx context.Context, prompt *Prompt) {
	if c.client == nil {
		c.notify(StreamErrorMsg{ChannelID: "sage", Err: fmt.Errorf("LLM not configured")})
		return
	}

	if c.MinisterBase.Session() == nil {
		var err error
		sess, err := CreateSession(c, c.client, c.config, c.notify, "sage")
		if err != nil {
			c.notify(StreamErrorMsg{ChannelID: "sage", Err: fmt.Errorf("failed to create session: %w", err)})
			return
		}
		sess.TabType = "sage"
		sess.SetPersister(c.Persister())
		c.MinisterBase.SetSession(sess)
	}

	c.notify(StreamStartMsg{ChannelID: "sage", EdictID: 0})

	_, err := c.MinisterBase.Session().AskWithStreaming(ctx, prompt.Message, prompt.ContextFiles)
	if err != nil && ctx.Err() == nil {
		c.notify(StreamErrorMsg{ChannelID: "sage", Err: err})
		return
	}
	c.notify(StreamDoneMsg{ChannelID: "sage"})
}

// --- Database Methods (migrated from Censor) ---

// GetQuenchedManifests retrieves all quenched manifests ready for ethics review
func (c *Sage) GetQuenchedManifests(key storage.EdictKey) ([]storage.ForgeManifest, error) {
	var manifests []storage.ForgeManifest
	err := c.db.Where("edict_id = ? AND username = ? AND project = ? AND status = ?", key.ID, key.Username, key.Project, storage.ManifestQuenched).
		Order("created_at ASC").
		Find(&manifests).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get quenched manifests: %w", err)
	}
	return manifests, nil
}

// NoRejections checks if there are any rejected manifests for an edict.
// Only the latest manifest per file_path is considered; superseded rejected
// manifests from prior forging rounds do not count.
func (c *Sage) NoRejections(key storage.EdictKey) (bool, error) {
	var count int64
	err := c.db.Raw(`
		SELECT COUNT(*) FROM forge_manifests fm
		WHERE fm.edict_id = ? AND fm.username = ? AND fm.project = ?
		  AND fm.status = ?
		  AND fm.created_at = (
		    SELECT MAX(fm2.created_at) FROM forge_manifests fm2
		    WHERE fm2.edict_id = fm.edict_id
		      AND fm2.username = fm.username
		      AND fm2.project = fm.project
		      AND fm2.file_path = fm.file_path
		  )`,
		key.ID, key.Username, key.Project, storage.ManifestRejected).
		Scan(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check rejections: %w", err)
	}
	return count == 0, nil
}

// LogPrecedent records an ethics decision for a manifest.
// Precedents are an append-only audit log, so the ID includes a nanosecond
// timestamp to keep repeated decisions on the same (manifest, principle) unique.
func (c *Sage) LogPrecedent(manifestID, principle string, ruling storage.PrecedentRuling, justification string) (string, error) {
	precedentID := GenerateID("precedent", manifestID, principle, fmt.Sprintf("%d", time.Now().UnixNano()))

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
func (c *Sage) RejectManifest(key storage.EdictKey, manifestID string) error {
	result := c.db.Model(&storage.ForgeManifest{}).
		Where("manifest_id = ? AND username = ? AND project = ?", manifestID, key.Username, key.Project).
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
func (c *Sage) GetPrecedentsForManifest(username, project, manifestID string) ([]storage.CensorPrecedent, error) {
	var precedents []storage.CensorPrecedent
	err := c.db.Joins("JOIN forge_manifests ON forge_manifests.manifest_id = censor_precedents.manifest_id").
		Where("censor_precedents.manifest_id = ? AND forge_manifests.username = ? AND forge_manifests.project = ?", manifestID, username, project).
		Order("censor_precedents.created_at ASC").
		Find(&precedents).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get precedents: %w", err)
	}
	return precedents, nil
}

// QueryPrecedentsByPrinciple searches precedents by principle (for case law lookup)
func (c *Sage) QueryPrecedentsByPrinciple(username, project, principle string, limit int) ([]storage.CensorPrecedent, error) {
	var precedents []storage.CensorPrecedent
	query := c.db.Joins("JOIN forge_manifests ON forge_manifests.manifest_id = censor_precedents.manifest_id").
		Where("censor_precedents.principle LIKE ? AND forge_manifests.username = ? AND forge_manifests.project = ?", "%"+principle+"%", username, project).
		Order("censor_precedents.created_at DESC")
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
func (c *Sage) GetEdictsWithQuenchedManifests(username, project string) ([]storage.Edict, error) {
	var edicts []storage.Edict
	err := c.db.Distinct("edicts.*").
		Joins("JOIN forge_manifests ON forge_manifests.edict_id = edicts.id AND forge_manifests.username = edicts.username AND forge_manifests.project = edicts.project").
		Where("forge_manifests.status = ? AND edicts.username = ? AND edicts.project = ?", storage.ManifestQuenched, username, project).
		Find(&edicts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get edicts with quenched manifests: %w", err)
	}

	// Filter out sealed/cancelled edicts using derived status
	sealService := storage.NewSealService(c.db)
	var activeEdicts []storage.Edict
	for _, e := range edicts {
		status, err := sealService.GetEdictStatus(storage.EdictKey{ID: e.ID, Username: e.Username, Project: e.Project})
		if err != nil {
			continue
		}
		if status == storage.EdictActive || status == storage.EdictBlocked {
			activeEdicts = append(activeEdicts, e)
		}
	}
	return activeEdicts, nil
}

// --- Diff Review Methods (migrated from Censor) ---

// ReviewDiff reviews a diff string and returns structured findings.
// This method can be used for ad-hoc reviews without requiring manifests or database writes.
// The Sage's Role() prompt guides the review process.
func (c *Sage) ReviewDiff(ctx context.Context, diff string) (*ReviewResult, error) {
	// If no LLM is configured, return a basic result
	if c.client == nil {
		return &ReviewResult{
			Approved:  true,
			Findings:  []Finding{},
			Reasoning: "No LLM configured for diff review - auto-approved",
		}, nil
	}

	// Create or reuse session for the review
	var sess *Session
	var err error
	if c.MinisterBase.Session() != nil {
		sess = c.MinisterBase.Session()
	} else {
		sess, err = CreateSession(c, c.client, c.config, c.notify, "sage")
		if err != nil {
			return nil, fmt.Errorf("failed to create sage session: %w", err)
		}
		c.MinisterBase.SetSession(sess)
	}

	// Build the review prompt
	reviewPrompt := c.buildReviewPrompt(diff)

	// Get the review from the LLM
	response, err := sess.AskWithStreaming(ctx, reviewPrompt, nil)
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
			if err := c.RejectManifest(storage.EdictKey{Username: manifest.Username, Project: manifest.Project}, manifest.ManifestID); err != nil {
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
