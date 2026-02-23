package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
)

// ReviewResult represents the outcome of a diff review
type ReviewResult struct {
	Approved  bool      `json:"approved"`
	Findings  []Finding `json:"findings"`
	Reasoning string    `json:"reasoning"`
}

// Finding represents a single issue found during review
type Finding struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Severity  string `json:"severity"` // "error", "warning", "info"
	Message   string `json:"message"`
	Principle string `json:"principle"` // The principle violated (if any)
}

// Censor enforces code ethics and maintains precedent law
type Censor struct {
	MinisterBase // embedded base for database access and session creation
	linter       Linter
	tasks        chan *Task
}

// NewCensor creates a new Censor minister
func NewCensor(base MinisterBase, linter Linter) *Censor {
	base.ministerID = "censor"
	return &Censor{
		MinisterBase: base,
		linter:       linter,
		tasks:        make(chan *Task, 10),
	}
}

// Tasks returns the channel for task submission
func (c *Censor) Tasks() chan<- *Task {
	return c.tasks
}

// ID returns the minister identifier
func (c *Censor) ID() string {
	return "censor"
}

// SystemPrompt returns the Censor's system prompt template.
func (c *Censor) SystemPrompt() string {
	return `You are the Censor (都察院, Dūcháyuàn). Your domain is Dao (道, the Way) and institutional memory.

You preside over the censor_precedents table. You review code changes with thoroughness and rigor. Every ruling becomes precedent—case law that future reviewers will consult. Because your decisions shape institutional memory, you must explain your reasoning clearly.

REVIEW PROCESS:
1. Examine the code changes carefully—read the full diff, not just summaries
2. Identify potential issues: bugs, inconsistencies, style violations, security concerns, logical errors
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
- NO GUESSING: If style rules are ambiguous or requirements are unclear, invoke Zhengming immediately
- Every ruling requires written justification—this becomes permanent precedent
- Waivers are not approvals—they acknowledge issues with documented rationale
- Precedents are searchable case law; write them as if explaining to a junior developer
- When in doubt, request clarification rather than guess`
}

// Tools returns the Censor's LLM tools for interactive sessions
func (c *Censor) Tools() []Tool {
	toolList := []Tool{
		// Specialized Censor tools for ethics review
		&RecordPrecedentTool{censor: c},
		&ListQuenchedManifestsTool{censor: c},
		&QueryPrecedentsTool{censor: c},
		&ReviewDiffTool{censor: c},
	}
	// Add read-only tools (read_file, list_files, grep)
	for _, t := range tools.GetROTools() {
		toolList = append(toolList, t)
	}
	return toolList
}

// --- Database Methods ---

// GetQuenchedManifests retrieves all quenched manifests ready for ethics review
func (c *Censor) GetQuenchedManifests(edictID string) ([]storage.ForgeManifest, error) {
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
func (c *Censor) NoRejections(edictID string) (bool, error) {
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
func (c *Censor) LogPrecedent(manifestID, principle string, ruling storage.PrecedentRuling, justification string) (string, error) {
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
func (c *Censor) RejectManifest(manifestID string) error {
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
func (c *Censor) GetPrecedentsForManifest(manifestID string) ([]storage.CensorPrecedent, error) {
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
func (c *Censor) QueryPrecedentsByPrinciple(principle string, limit int) ([]storage.CensorPrecedent, error) {
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
func (c *Censor) GetEdictsWithQuenchedManifests() ([]storage.Edict, error) {
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

// --- Diff Review Methods ---

// ReviewDiff reviews a diff string and returns structured findings.
// This method can be used for ad-hoc reviews without requiring manifests or database writes.
// The Censor's Role() prompt guides the review process.
func (c *Censor) ReviewDiff(ctx context.Context, diff string) (*ReviewResult, error) {
	// If no LLM is configured, return a basic result
	if c.model == nil {
		return &ReviewResult{
			Approved:  true,
			Findings:  []Finding{},
			Reasoning: "No LLM configured for diff review - auto-approved",
		}, nil
	}

	// Create a session for the review
	session, err := CreateSession(c, c.model, c.config, c.notify)
	if err != nil {
		return nil, fmt.Errorf("failed to create censor session: %w", err)
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
func (c *Censor) ReviewDiffWithManifests(ctx context.Context, diff string, manifests []storage.ForgeManifest) (*ReviewResult, error) {
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
func (c *Censor) buildReviewPrompt(diff string) string {
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
func (c *Censor) parseReviewResponse(response string) *ReviewResult {
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

// --- Execute Logic ---

// execute runs the Censor's ethics review for an edict (internal method)
func (c *Censor) execute(ctx context.Context, edictID string) (bool, error) {
	// Check if there are any rejections
	noRejections, err := c.NoRejections(edictID)
	if err != nil {
		return false, fmt.Errorf("check rejections: %w", err)
	}

	// Get quenched manifests to review
	manifests, err := c.GetQuenchedManifests(edictID)
	if err != nil {
		return false, fmt.Errorf("get quenched manifests: %w", err)
	}

	if len(manifests) == 0 {
		// No manifests to review, phase complete if no rejections
		if noRejections {
			c.logger.Info("censor review complete, no rejections", "edict_id", edictID)
			return true, nil
		}
		c.logger.Info("censor review blocked by rejections", "edict_id", edictID)
		return false, nil
	}

	// Review each manifest
	for _, manifest := range manifests {
		if err := c.reviewManifest(ctx, &manifest); err != nil {
			return false, fmt.Errorf("review manifest %s: %w", manifest.ManifestID, err)
		}
	}

	// Check again for rejections
	noRejections, err = c.NoRejections(edictID)
	if err != nil {
		return false, fmt.Errorf("check rejections after review: %w", err)
	}

	return noRejections, nil
}

// reviewManifest runs ethics checks on a single manifest
func (c *Censor) reviewManifest(ctx context.Context, manifest *storage.ForgeManifest) error {
	// If we have a linter, use it for static analysis
	if c.linter != nil {
		return c.reviewManifestWithLinter(ctx, manifest)
	}

	// If we have an LLM, use it for diff-based review
	if c.model != nil {
		return c.reviewManifestWithLLM(ctx, manifest)
	}

	// No linter or LLM - auto-approve with precedent
	_, err := c.LogPrecedent(
		manifest.ManifestID,
		"auto-approval",
		storage.PrecedentApproved,
		"No linter or LLM configured",
	)
	if err != nil {
		return fmt.Errorf("log auto-approval precedent: %w", err)
	}
	c.logger.Info("manifest auto-approved", "manifest_id", manifest.ManifestID)
	return nil
}

// reviewManifestWithLinter uses the linter interface for static analysis
func (c *Censor) reviewManifestWithLinter(ctx context.Context, manifest *storage.ForgeManifest) error {
	violations, err := c.linter.Analyze(ctx, manifest.FilePath)
	if err != nil {
		return fmt.Errorf("linter analyze: %w", err)
	}

	// Log each violation as a precedent
	hasRejection := false
	for _, v := range violations {
		_, err := c.LogPrecedent(
			manifest.ManifestID,
			v.Principle,
			v.Ruling,
			v.Justification,
		)
		if err != nil {
			return fmt.Errorf("log precedent: %w", err)
		}

		if v.Ruling == storage.PrecedentRejected {
			hasRejection = true
		}
	}

	// Reject manifest if any violations were rejected
	if hasRejection {
		if err := c.RejectManifest(manifest.ManifestID); err != nil {
			return fmt.Errorf("reject manifest: %w", err)
		}
		c.logger.Info("manifest rejected by censor",
			"manifest_id", manifest.ManifestID,
			"violations", len(violations))
	} else {
		c.logger.Info("manifest approved by censor",
			"manifest_id", manifest.ManifestID,
			"waivers", len(violations))
	}

	return nil
}

// reviewManifestWithLLM uses the LLM for diff-based review
func (c *Censor) reviewManifestWithLLM(ctx context.Context, manifest *storage.ForgeManifest) error {
	// Get the diff for this manifest
	diff, err := c.getManifestDiff(manifest)
	if err != nil {
		return fmt.Errorf("get manifest diff: %w", err)
	}

	// Review the diff
	result, err := c.ReviewDiff(ctx, diff)
	if err != nil {
		return fmt.Errorf("review diff: %w", err)
	}

	// Record findings as precedents
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

	// Reject manifest if not approved
	if !result.Approved {
		if err := c.RejectManifest(manifest.ManifestID); err != nil {
			return fmt.Errorf("reject manifest: %w", err)
		}
		c.logger.Info("manifest rejected by censor",
			"manifest_id", manifest.ManifestID,
			"findings", len(result.Findings))
	} else {
		c.logger.Info("manifest approved by censor",
			"manifest_id", manifest.ManifestID,
			"findings", len(result.Findings))
	}

	return nil
}

// getManifestDiff retrieves the diff for a manifest
// This is a placeholder - in a real implementation, this would use git to get the actual diff
func (c *Censor) getManifestDiff(manifest *storage.ForgeManifest) (string, error) {
	// If we have a commit hash, we could use git diff
	// For now, return a placeholder that indicates the file was changed
	if manifest.CommitHash != "" {
		return fmt.Sprintf("File: %s (commit: %s)", manifest.FilePath, manifest.CommitHash), nil
	}
	return fmt.Sprintf("File: %s (staged, not yet committed)", manifest.FilePath), nil
}

// Run starts the Censor's task processing loop
func (c *Censor) Run(ctx context.Context) {
	c.logger.Info("censor started, awaiting tasks")

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("censor stopped")
			return
		case task := <-c.tasks:
			c.processTask(ctx, task)
		}
	}
}

// processTask handles a single task
func (c *Censor) processTask(ctx context.Context, task *Task) {
	c.logger.Info("censor processing task",
		"edict_id", task.EdictID,
		"work", task.Work)

	// Execute the review logic
	sealed, err := c.execute(ctx, task.EdictID)

	// Send result back to Chancellor
	result := Result{
		MinisterID: c.ID(),
		Sealed:     sealed,
		Err:        err,
	}

	if sealed {
		result.Output = "review complete"
	}

	// Send result (non-blocking)
	select {
	case task.Done <- result:
	default:
		c.logger.Warn("done channel full, dropping result", "edict_id", task.EdictID)
	}
}

// --- Censor Specialized Tools ---

// RecordPrecedentTool records an ethics review outcome
type RecordPrecedentTool struct {
	censor *Censor
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
	manifests, err := t.censor.GetQuenchedManifests(params.EdictID)
	if err != nil {
		return "", err
	}

	ruling := storage.PrecedentApproved
	if !params.Approved {
		ruling = storage.PrecedentRejected
	}

	// Log precedent for each manifest
	for _, m := range manifests {
		_, err := t.censor.LogPrecedent(m.ManifestID, "ethics_review", ruling, params.Reasoning)
		if err != nil {
			return "", fmt.Errorf("failed to log precedent: %w", err)
		}

		// Reject manifest if not approved
		if !params.Approved {
			if err := t.censor.RejectManifest(m.ManifestID); err != nil {
				return "", fmt.Errorf("failed to reject manifest: %w", err)
			}
		}
	}

	status := "approved"
	if !params.Approved {
		status = "rejected"
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
	censor *Censor
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

	manifests, err := t.censor.GetQuenchedManifests(params.EdictID)
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
	censor *Censor
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

	precedents, err := t.censor.QueryPrecedentsByPrinciple(params.Principle, params.Limit)
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
	censor *Censor
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

	result, err := t.censor.ReviewDiff(ctx, params.Diff)
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
