package shogunate

import (
	"context"
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// --- Minister ---

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
	MinisterBase // embedded base for database access and session creation
	rca          RCAAnalyzer
	tasks        chan *TaskEnvelope
}

// NewMarshal creates a new Marshal minister
func NewMarshal(base MinisterBase, rca RCAAnalyzer) *Marshal {
	base.ministerID = "marshal"
	return &Marshal{
		MinisterBase: base,
		rca:          rca,
		tasks:        make(chan *TaskEnvelope, 10),
	}
}

// Tasks returns the channel for task submission
func (m *Marshal) Tasks() chan<- *TaskEnvelope {
	return m.tasks
}

// ID returns the minister identifier
func (m *Marshal) ID() string {
	return "marshal"
}

// Role returns the Marshal's role identity text
func (m *Marshal) Role() string {
	return MarshalPrompt
}

// Tools returns the Marshal's LLM tools for interactive sessions
func (m *Marshal) Tools(notify NotifyFunc) []Tool {
	// TODO: Implement Marshal tools (log_incident, get_incident, approve_hotfix, get_manifest_by_commit)
	return []Tool{}
}

// --- Database Methods ---

// GetManifestByCommit finds a manifest by its git commit hash
func (m *Marshal) GetManifestByCommit(commitHash string) (*storage.ForgeManifest, error) {
	var manifest storage.ForgeManifest
	if err := m.db.First(&manifest, "commit_hash = ?", commitHash).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("manifest not found for commit: %s", commitHash)
		}
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}
	return &manifest, nil
}

// LogIncident records a production crash incident
func (m *Marshal) LogIncident(incidentID, edictID, commitHash, rcaSummary string) error {
	incident := storage.MarshalIncident{
		IncidentID: incidentID,
		EdictID:    edictID,
		CommitHash: commitHash,
		RCASummary: rcaSummary,
	}

	if err := m.db.Create(&incident).Error; err != nil {
		return fmt.Errorf("failed to log incident: %w", err)
	}
	return nil
}

// GetIncident retrieves an incident by ID
func (m *Marshal) GetIncident(incidentID string) (*storage.MarshalIncident, error) {
	var incident storage.MarshalIncident
	if err := m.db.First(&incident, "incident_id = ?", incidentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("incident not found: %s", incidentID)
		}
		return nil, fmt.Errorf("failed to get incident: %w", err)
	}
	return &incident, nil
}

// MarkHotfixApproved approves a hotfix for an incident
func (m *Marshal) MarkHotfixApproved(incidentID string) error {
	result := m.db.Model(&storage.MarshalIncident{}).
		Where("incident_id = ?", incidentID).
		Update("hotfix_approved", true)
	if result.Error != nil {
		return fmt.Errorf("failed to approve hotfix: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("incident not found: %s", incidentID)
	}
	return nil
}

// GetPendingIncidents returns incidents that need processing (not yet approved)
func (m *Marshal) GetPendingIncidents() ([]storage.MarshalIncident, error) {
	var incidents []storage.MarshalIncident
	err := m.db.Where("hotfix_approved = ?", false).
		Order("created_at ASC").
		Find(&incidents).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending incidents: %w", err)
	}
	return incidents, nil
}

// --- Execute Logic ---

// execute runs the Marshal's production monitoring (internal method)
// Note: Marshal doesn't participate in normal edict flow, but handles incidents
func (m *Marshal) execute(ctx context.Context, edictID string) (bool, error) {
	// Marshal's Execute is called for 'assassination' type edicts (hotfixes)
	edict, err := m.GetEdict(edictID)
	if err != nil {
		return false, fmt.Errorf("get edict: %w", err)
	}

	// Check if this is a hotfix edict created by an incident
	if edict.CurrentPhase == storage.PhasePlanning {
		// Hotfix needs expedited processing - auto-seal planning phase
		m.logger.Info("marshal expediting hotfix", "edict_id", edictID)
		return true, nil
	}

	return true, nil
}

// OnIncident handles a production incident
func (m *Marshal) OnIncident(ctx context.Context, incidentID, commitHash string) error {
	// Perform RCA if analyzer available
	var rcaSummary string
	var edictID string

	if m.rca != nil {
		report, err := m.rca.Analyze(ctx, incidentID)
		if err != nil {
			m.logger.Warn("RCA failed", "incident_id", incidentID, "error", err)
			rcaSummary = fmt.Sprintf("RCA failed: %v", err)
		} else {
			rcaSummary = report.Summary
			edictID = report.EdictID
		}
	} else {
		rcaSummary = "No RCA analyzer configured"
	}

	// Try to find the manifest by commit
	if edictID == "" {
		manifest, err := m.GetManifestByCommit(commitHash)
		if err == nil && manifest != nil {
			edictID = manifest.EdictID
		}
	}

	// Log the incident
	if err := m.LogIncident(incidentID, edictID, commitHash, rcaSummary); err != nil {
		return fmt.Errorf("log incident: %w", err)
	}

	// Request Zhengming for hotfix approval
	if edictID != "" {
		_, err := m.RequestZhengming(edictID,
			fmt.Sprintf("Production incident %s requires hotfix approval.\n\nRCA: %s", incidentID, rcaSummary),
			storage.PriorityUrgent)
		if err != nil {
			return fmt.Errorf("request zhengming: %w", err)
		}
	}

	m.logger.Info("incident logged",
		"incident_id", incidentID,
		"commit_hash", commitHash,
		"edict_id", edictID)

	return nil
}

// Run starts the Marshal's task processing loop
func (m *Marshal) Run(ctx context.Context) {
	m.logger.Info("marshal started, awaiting tasks")

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("marshal stopped")
			return
		case env := <-m.tasks:
			m.processTask(ctx, env)
		}
	}
}

// processTask handles a single task envelope
func (m *Marshal) processTask(ctx context.Context, env *TaskEnvelope) {
	m.logger.Info("marshal processing task",
		"edict_id", env.EdictID,
		"task", env.Task)

	// Execute the marshal logic
	sealed, err := m.execute(ctx, env.EdictID)

	// Send reply back to Chancellor
	reply := &TaskReply{
		EdictID:    env.EdictID,
		MinisterID: m.ID(),
		Task:       env.Task,
		Sealed:     sealed,
		Error:      err,
	}

	if sealed {
		reply.Output = "marshal task complete"
	}

	// Send reply (non-blocking)
	select {
	case env.ReplyChan <- reply:
	default:
		m.logger.Warn("reply channel full, dropping reply", "edict_id", env.EdictID)
	}
}
