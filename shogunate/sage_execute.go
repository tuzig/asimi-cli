package shogunate

import (
	"context"
	"fmt"
	"strings"

	"github.com/afittestide/asimi/storage"
)

// --- Execute Logic (migrated from Censor) ---

// execute runs the Sage's ethics review for an edict (internal method)
// Returns: sealed (phase complete), review summary, error
func (c *Sage) execute(ctx context.Context, key storage.EdictKey) (bool, *ReviewSummary, error) {
	// Check if there are any rejections
	noRejections, err := c.NoRejections(key)
	if err != nil {
		return false, nil, fmt.Errorf("check rejections: %w", err)
	}

	// Get quenched manifests to review
	manifests, err := c.GetQuenchedManifests(key)
	if err != nil {
		return false, nil, fmt.Errorf("get quenched manifests: %w", err)
	}

	if len(manifests) == 0 {
		// No manifests to review, phase complete if no rejections
		if noRejections {
			c.logger.Info("sage review complete, no rejections", "edict_id", key.ID)
			return true, &ReviewSummary{
				Approved:       true,
				ManifestsCount: 0,
				Reasoning:      "No manifests to review",
			}, nil
		}
		c.logger.Info("sage review blocked by rejections", "edict_id", key.ID)
		return false, &ReviewSummary{
			Approved:  false,
			Reasoning: "Review blocked by previous rejections",
		}, nil
	}

	// Collect all findings from manifest reviews
	var allFindings []Finding
	var reviewReasoning strings.Builder
	reviewReasoning.WriteString(fmt.Sprintf("Reviewed %d manifest(s)\n\n", len(manifests)))

	// Review each manifest
	for _, manifest := range manifests {
		findings, err := c.reviewManifest(ctx, &manifest)
		if err != nil {
			return false, nil, fmt.Errorf("review manifest %s: %w", manifest.ManifestID, err)
		}
		allFindings = append(allFindings, findings...)
	}

	// Check again for rejections
	noRejections, err = c.NoRejections(key)
	if err != nil {
		return false, nil, fmt.Errorf("check rejections after review: %w", err)
	}

	// Build reasoning summary
	if len(allFindings) == 0 {
		reviewReasoning.WriteString("No issues found. All manifests approved.")
	} else {
		reviewReasoning.WriteString(fmt.Sprintf("Found %d issue(s):\n", len(allFindings)))
		for i, f := range allFindings {
			reviewReasoning.WriteString(fmt.Sprintf("%d. [%s] %s", i+1, f.Severity, f.Message))
			if f.File != "" {
				reviewReasoning.WriteString(fmt.Sprintf(" (%s", f.File))
				if f.Line > 0 {
					reviewReasoning.WriteString(fmt.Sprintf(":%d", f.Line))
				}
				reviewReasoning.WriteString(")")
			}
			reviewReasoning.WriteString("\n")
		}
	}

	summary := &ReviewSummary{
		Approved:       noRejections,
		ManifestsCount: len(manifests),
		FindingsCount:  len(allFindings),
		Findings:       allFindings,
		Reasoning:      reviewReasoning.String(),
	}

	return noRejections, summary, nil
}

// reviewManifest runs ethics checks on a single manifest and returns any findings
func (c *Sage) reviewManifest(ctx context.Context, manifest *storage.ForgeManifest) ([]Finding, error) {
	// If we have a linter, use it for static analysis
	if c.linter != nil {
		return c.reviewManifestWithLinter(ctx, manifest)
	}

	// If we have an LLM, use it for diff-based review
	if c.client != nil {
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
		return nil, fmt.Errorf("log auto-approval precedent: %w", err)
	}
	c.logger.Info("manifest auto-approved", "manifest_id", manifest.ManifestID)
	return []Finding{}, nil
}

// reviewManifestWithLinter uses the linter interface for static analysis
func (c *Sage) reviewManifestWithLinter(ctx context.Context, manifest *storage.ForgeManifest) ([]Finding, error) {
	violations, err := c.linter.Analyze(ctx, manifest.FilePath)
	if err != nil {
		return nil, fmt.Errorf("linter analyze: %w", err)
	}

	var findings []Finding
	hasRejection := false
	for _, v := range violations {
		_, err := c.LogPrecedent(
			manifest.ManifestID,
			v.Principle,
			v.Ruling,
			v.Justification,
		)
		if err != nil {
			return nil, fmt.Errorf("log precedent: %w", err)
		}

		severity := "warning"
		if v.Ruling == storage.PrecedentRejected {
			severity = "error"
			hasRejection = true
		}

		findings = append(findings, Finding{
			File:      manifest.FilePath,
			Severity:  severity,
			Message:   v.Justification,
			Principle: v.Principle,
		})
	}

	// Reject manifest if any violations were rejected
	if hasRejection {
		if err := c.RejectManifest(manifest.ManifestID); err != nil {
			return nil, fmt.Errorf("reject manifest: %w", err)
		}
		c.logger.Info("manifest rejected by sage",
			"manifest_id", manifest.ManifestID,
			"violations", len(violations))
	} else {
		c.logger.Info("manifest approved by sage",
			"manifest_id", manifest.ManifestID,
			"waivers", len(violations))
	}

	return findings, nil
}

// reviewManifestWithLLM uses the LLM for diff-based review
func (c *Sage) reviewManifestWithLLM(ctx context.Context, manifest *storage.ForgeManifest) ([]Finding, error) {
	// Get the diff for this manifest
	diff, err := c.getManifestDiff(manifest)
	if err != nil {
		return nil, fmt.Errorf("get manifest diff: %w", err)
	}

	// Review the diff
	result, err := c.ReviewDiff(ctx, diff)
	if err != nil {
		return nil, fmt.Errorf("review diff: %w", err)
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
			return nil, fmt.Errorf("reject manifest: %w", err)
		}
		c.logger.Info("manifest rejected by sage",
			"manifest_id", manifest.ManifestID,
			"findings", len(result.Findings))
	} else {
		c.logger.Info("manifest approved by sage",
			"manifest_id", manifest.ManifestID,
			"findings", len(result.Findings))
	}

	return result.Findings, nil
}

// getManifestDiff retrieves the diff for a manifest
// This is a placeholder - in a real implementation, this would use git to get the actual diff
func (c *Sage) getManifestDiff(manifest *storage.ForgeManifest) (string, error) {
	// If we have a commit hash, we could use git diff
	// For now, return a placeholder that indicates the file was changed
	if manifest.CommitHash != "" {
		return fmt.Sprintf("File: %s (commit: %s)", manifest.FilePath, manifest.CommitHash), nil
	}
	return fmt.Sprintf("File: %s (staged, not yet committed)", manifest.FilePath), nil
}
