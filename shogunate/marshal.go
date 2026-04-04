package shogunate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/afittestide/asimi/shogunate/tools"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
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
	tasks         chan *Task
}

// NewMarshal creates a new Marshal minister
func NewMarshal(base *MinisterBase, rca RCAAnalyzer) *Marshal {
	base.ministerID = "marshal"
	return &Marshal{
		MinisterBase: base,
		rca:          rca,
		tasks:        make(chan *Task, 10),
	}
}

// Tasks returns the channel for task submission
func (m *Marshal) Tasks() chan<- *Task {
	return m.tasks
}

// ID returns the minister identifier
func (m *Marshal) ID() string {
	return "marshal"
}

// SystemPrompt returns the Marshal's system prompt template.
func (m *Marshal) SystemPrompt() string {
	return MarshalPrompt
}

// Tools returns the Marshal's LLM tools for interactive sessions
func (m *Marshal) Tools() []Tool {
	toolList := []Tool{
		// Specialized Marshal tools for incident management
		&CreateIncidentTool{marshal: m},
		&ResolveIncidentTool{marshal: m},
		&GetIncidentTool{marshal: m},
		&GetManifestByCommitTool{marshal: m},
	}
	// Add read-only tools (read_file, list_files, grep)
	for _, t := range tools.GetROTools(m.config.LLM) {
		toolList = append(toolList, t)
	}
	// Add shell command tool if runner is available
	if m.runner != nil {
		toolList = append(toolList, tools.NewRunShellCommand(m.runner, m.runner, nil))
	}
	return toolList
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
func (m *Marshal) LogIncident(incidentID string, key storage.EdictKey, commitHash, rcaSummary string) error {
	incident := storage.MarshalIncident{
		IncidentID: incidentID,
		EdictID:    key.ID,
		Username:   key.Username,
		Project:    key.Project,
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
func (m *Marshal) execute(ctx context.Context, key storage.EdictKey) (bool, error) {
	// Marshal's Execute is called for 'assassination' type edicts (hotfixes)
	// Check if this is a hotfix edict created by an incident
	sealService := storage.NewSealService(m.db)
	status, err := sealService.GetEdictStatus(key)
	if err != nil {
		return false, fmt.Errorf("get edict status: %w", err)
	}

	if status == storage.EdictActive {
		// Hotfix needs expedited processing
		m.logger.Info("marshal expediting hotfix", "edict_id", key.ID)
		return true, nil
	}

	return true, nil
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
		manifest, err := m.GetManifestByCommit(commitHash)
		if err == nil && manifest != nil {
			key = storage.EdictKey{ID: manifest.EdictID, Username: manifest.Username, Project: manifest.Project}
		}
	}

	// Log the incident
	if err := m.LogIncident(incidentID, key, commitHash, rcaSummary); err != nil {
		return fmt.Errorf("log incident: %w", err)
	}

	// Request Zhengming for hotfix approval
	if key.ID != 0 {
		_, err := m.RequestZhengming(key,
			storage.ZhengmingQuestions{{
				Text:    fmt.Sprintf("Production incident %s requires hotfix approval.\n\nRCA: %s", incidentID, rcaSummary),
				Options: []string{"Approve hotfix", "Reject hotfix"},
			}},
			storage.PriorityUrgent)
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
	m.logger.Info("marshal started, awaiting tasks")

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("marshal stopped")
			return
		case task := <-m.tasks:
			merged, mergedCancel := context.WithCancel(ctx)
			if task.Ctx != nil {
				context.AfterFunc(task.Ctx, func() { mergedCancel() })
			}
			m.processTask(merged, task)
			mergedCancel()
		}
	}
}

// processTask handles a single task
func (m *Marshal) processTask(ctx context.Context, task *Task) {
	m.logger.Info("marshal processing task",
		"edict_id", task.EdictKey.ID,
		"work", task.Work)

	// Execute the marshal logic
	sealed, err := m.execute(ctx, task.EdictKey)

	// Send result back to Chancellor
	result := Result{
		MinisterID: m.ID(),
		Sealed:     sealed,
		Err:        err,
	}

	if sealed {
		result.Output = "marshal task complete"
	}

	// Send result (non-blocking)
	select {
	case task.Done <- result:
	default:
		m.logger.Warn("done channel full, dropping result", "edict_id", task.EdictKey.ID)
	}
}

// --- Marshal Specialized Tools ---

// CreateIncidentTool creates a new production incident
type CreateIncidentTool struct {
	marshal *Marshal
}

func (t *CreateIncidentTool) Name() string { return "create_incident" }

func (t *CreateIncidentTool) Description() string {
	return "Creates a new production incident. Input: JSON with 'description', 'severity', and optional 'edict_id'."
}

func (t *CreateIncidentTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Description string `json:"description"`
		Severity    string `json:"severity"`
		EdictID     uint   `json:"edict_id"`
		Username    string `json:"username"`
		Project     string `json:"project"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.Description == "" || params.Severity == "" {
		return "", fmt.Errorf("description and severity are required")
	}

	incidentID := GenerateID("incident", params.Description, params.Severity)
	key := storage.EdictKey{ID: params.EdictID, Username: params.Username, Project: params.Project}

	if err := t.marshal.LogIncident(incidentID, key, "", params.Description); err != nil {
		return "", err
	}

	return fmt.Sprintf("Created incident %s (severity: %s)", incidentID, params.Severity), nil
}

func (t *CreateIncidentTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"description": map[string]any{"type": "string", "description": "Description of the incident"},
			"severity":    map[string]any{"type": "string", "description": "Severity level (critical, high, medium, low)"},
			"edict_id":    map[string]any{"type": "integer", "description": "Optional edict ID linked to this incident"},
			"username":    map[string]any{"type": "string", "description": "The username"},
			"project":     map[string]any{"type": "string", "description": "The project name"},
		},
		"required": []string{"description", "severity"},
	}
}

func (t *CreateIncidentTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Create Incident: Error: %v\n", err)
	}
	return fmt.Sprintf("Create Incident: %s\n", result)
}

// ResolveIncidentTool marks an incident as resolved
type ResolveIncidentTool struct {
	marshal *Marshal
}

func (t *ResolveIncidentTool) Name() string { return "resolve_incident" }

func (t *ResolveIncidentTool) Description() string {
	return "Marks an incident as resolved with details. Input: JSON with 'incident_id', 'resolution', and optional 'root_cause'."
}

func (t *ResolveIncidentTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		IncidentID string `json:"incident_id"`
		Resolution string `json:"resolution"`
		RootCause  string `json:"root_cause"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.IncidentID == "" || params.Resolution == "" {
		return "", fmt.Errorf("incident_id and resolution are required")
	}

	// Update the incident with resolution
	result := t.marshal.db.Model(&storage.MarshalIncident{}).
		Where("incident_id = ?", params.IncidentID).
		Updates(map[string]interface{}{
			"rca_summary":     params.Resolution + "\n\nRoot Cause: " + params.RootCause,
			"hotfix_approved": true,
		})
	if result.Error != nil {
		return "", fmt.Errorf("failed to resolve incident: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("incident not found: %s", params.IncidentID)
	}

	return fmt.Sprintf("Resolved incident %s", params.IncidentID), nil
}

func (t *ResolveIncidentTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"incident_id": map[string]any{"type": "string", "description": "The incident ID to resolve"},
			"resolution":  map[string]any{"type": "string", "description": "Description of how the incident was resolved"},
			"root_cause":  map[string]any{"type": "string", "description": "Root cause analysis (optional)"},
		},
		"required": []string{"incident_id", "resolution"},
	}
}

func (t *ResolveIncidentTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Resolve Incident: Error: %v\n", err)
	}
	return fmt.Sprintf("Resolve Incident: %s\n", result)
}

// GetIncidentTool retrieves an incident by ID
type GetIncidentTool struct {
	marshal *Marshal
}

func (t *GetIncidentTool) Name() string { return "get_incident" }

func (t *GetIncidentTool) Description() string {
	return "Gets details of an incident by ID. Input: JSON with 'incident_id'."
}

func (t *GetIncidentTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		IncidentID string `json:"incident_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.IncidentID == "" {
		return "", fmt.Errorf("incident_id is required")
	}

	incident, err := t.marshal.GetIncident(params.IncidentID)
	if err != nil {
		return "", err
	}

	result, err := json.MarshalIndent(incident, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to format incident: %w", err)
	}
	return string(result), nil
}

func (t *GetIncidentTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"incident_id": map[string]any{"type": "string", "description": "The incident ID to retrieve"},
		},
		"required": []string{"incident_id"},
	}
}

func (t *GetIncidentTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Get Incident: Error: %v\n", err)
	}
	return fmt.Sprintf("Get Incident: %s\n", result)
}

// GetManifestByCommitTool finds a manifest by its commit hash
type GetManifestByCommitTool struct {
	marshal *Marshal
}

func (t *GetManifestByCommitTool) Name() string { return "get_manifest_by_commit" }

func (t *GetManifestByCommitTool) Description() string {
	return "Finds the manifest associated with a commit hash. Input: JSON with 'commit_hash'."
}

func (t *GetManifestByCommitTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		CommitHash string `json:"commit_hash"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.CommitHash == "" {
		return "", fmt.Errorf("commit_hash is required")
	}

	manifest, err := t.marshal.GetManifestByCommit(params.CommitHash)
	if err != nil {
		return "", err
	}

	result, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to format manifest: %w", err)
	}
	return string(result), nil
}

func (t *GetManifestByCommitTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"commit_hash": map[string]any{"type": "string", "description": "The git commit hash to look up"},
		},
		"required": []string{"commit_hash"},
	}
}

func (t *GetManifestByCommitTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Get Manifest By Commit: Error: %v\n", err)
	}
	return fmt.Sprintf("Get Manifest By Commit: %s\n", result)
}
