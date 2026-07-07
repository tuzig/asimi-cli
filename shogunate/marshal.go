package shogunate

import (
	"context"
	"fmt"

	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
)

// MarshalPrompt defines the Marshal's identity and capabilities
const MarshalPrompt = `You are the Marshal (锦衣卫, Jǐnyīwèi). Your jurisdiction is production runtime.

When a crash occurs, you perform RCA (Root Cause Analysis), link it to an edict and a commit, then invoke Zhengming to request hotfix approval.

You report directly to the Chancellor.

# Tools

## Shogunate Tools
- **log_incident**: Record a production incident with commit hash and RCA summary
- **get_incident**: Get details of an incident by ID
- **approve_hotfix**: Approve a hotfix for expedited deployment
- **get_manifest_by_commit**: Find the manifest associated with a crash-causing commit
- **request_zhengming**: Ask the Ruler for hotfix approval (defaults to urgent priority)

## Standard Tools (diagnose)
- **read_file**: Read crash logs, stack traces, error reports
- **run_shell_command**: Run diagnostic commands, query monitoring systems

CRITICAL RULES:
- You never deploy extrajudicially
- Create a new edict of type 'assassination' for hotfixes
- Let the full Shogunate execute emergency fixes
- Log all incidents with full traceability
- You have read/write on marshal_incidents; read-only on edicts, forge_manifest, and filesystem`

// Marshal monitors production and handles incidents
type Marshal struct {
	*MinisterBase // embedded base for database access and session creation
	rca           RCAAnalyzer
}

// NewMarshal creates a new Marshal minister
func NewMarshal(base *MinisterBase, rca RCAAnalyzer) *Marshal {
	base.ministerID = "marshal"
	m := &Marshal{
		MinisterBase: base,
		rca:          rca,
	}
	m.self = m
	return m
}

// SystemPrompt returns the Marshal's system prompt template.
func (m *Marshal) SystemPrompt() string {
	return MarshalPrompt
}

// Tools returns the Marshal's LLM tools for interactive sessions
func (m *Marshal) Tools() []Tool {
	if m.toolRegistry != nil {
		perm, _ := tools.ParsePermissions("r-xr--rw-")
		registered := m.toolRegistry.ForPermissions("marshal", perm)
		result := make([]Tool, len(registered))
		for i, t := range registered {
			result[i] = t
		}
		return result
	}
	// Fallback: read-only tools only (no DB-dependent tools without registry)
	var toolList []Tool
	for _, t := range tools.GetROTools(m.config.LLM, m.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	if m.runner != nil {
		toolList = append(toolList, tools.NewRunShellCommand(m.CheckHostCommand, m.runner, m.msgChan, m.RepoInfo().ProjectRoot))
	}
	return toolList
}

// OnIncident handles a production incident
func (m *Marshal) OnIncident(ctx context.Context, incidentID, commitHash string) error {
	// Perform RCA if analyzer available
	var rcaSummary string
	var key storage.EdictKey

	if m.rca != nil {
		report, err := m.rca.Analyze(ctx, incidentID)
		if err != nil {
			m.logger.Warn("RCA failed", "incident_id", incidentID, "error", err)
			rcaSummary = fmt.Sprintf("RCA failed: %v", err)
		} else {
			rcaSummary = report.Summary
			key = report.EdictKey
		}
	} else {
		rcaSummary = "No RCA analyzer configured"
	}

	// Try to find the manifest by commit
	if key.ID == 0 {
		var manifest storage.ForgeManifest
		if err := m.db.Where("commit_hash = ? AND username = ? AND project = ?", commitHash, m.username, m.project).
			First(&manifest).Error; err == nil {
			key = storage.EdictKey{ID: manifest.EdictID, Username: manifest.Username, Project: manifest.Project}
		}
	}

	// Log the incident
	username := key.Username
	if username == "" {
		username = m.username
	}
	project := key.Project
	if project == "" {
		project = m.project
	}
	incident := storage.MarshalIncident{
		IncidentID: incidentID,
		EdictID:    key.ID,
		Username:   username,
		Project:    project,
		CommitHash: commitHash,
		RCASummary: rcaSummary,
	}
	if err := m.db.Create(&incident).Error; err != nil {
		return fmt.Errorf("log incident: %w", err)
	}

	// Request Zhengming for hotfix approval
	if key.ID != 0 {
		_, err := m.RequestZhengming(key,
			storage.ZhengmingQuestions{{
				Text:    fmt.Sprintf("Production incident %s requires hotfix approval.\n\nRCA: %s", incidentID, rcaSummary),
				Options: []string{"Approve hotfix", "Reject hotfix"},
			}},
			storage.PriorityUrgent,
			m.ministerID)
		if err != nil {
			return fmt.Errorf("request zhengming: %w", err)
		}
	}

	m.logger.Info("incident logged",
		"incident_id", incidentID,
		"commit_hash", commitHash,
		"edict_id", key.ID)

	return nil
}

// Run starts the Marshal's task processing loop
func (m *Marshal) Run(ctx context.Context) {
	m.RunLoop(ctx, m, nil, m.MinisterBase.processTask)
}
