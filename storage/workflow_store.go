package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// WorkflowState represents the overall state of a workflow
type WorkflowState string

const (
	WorkflowStatePending   WorkflowState = "pending"
	WorkflowStateRunning   WorkflowState = "running"
	WorkflowStateCompleted WorkflowState = "completed"
	WorkflowStateAborted   WorkflowState = "aborted"
	WorkflowStateFailed    WorkflowState = "failed"
)

// WorkflowData represents a workflow stored in the database
type WorkflowData struct {
	ID          string        `db:"id"`
	BranchID    int64         `db:"branch_id"`
	Name        string        `db:"name"`
	CurrentStep int           `db:"current_step"`
	State       WorkflowState `db:"state"`
	MaxRetries  int           `db:"max_retries"`
	Data        string        `db:"data"` // JSON-encoded map[string]string
	CreatedAt   time.Time     `db:"created_at"`
	UpdatedAt   time.Time     `db:"updated_at"`
}

// WorkflowStepData represents a workflow step stored in the database
type WorkflowStepData struct {
	ID             int64  `db:"id"`
	WorkflowID     string `db:"workflow_id"`
	StepIndex      int    `db:"step_index"`
	Name           string `db:"name"`
	RetryCount     int    `db:"retry_count"`
	Message        string `db:"message"`
	PromptTemplate string `db:"prompt_template"`
	PrepareData    string `db:"prepare_data"` // JSON-encoded map[string]string
}

// WorkflowStore handles workflow persistence
type WorkflowStore struct {
	db *DB
}

// NewWorkflowStore creates a new workflow store
func NewWorkflowStore(db *DB) *WorkflowStore {
	return &WorkflowStore{db: db}
}

// SaveWorkflow saves or updates a workflow
func (s *WorkflowStore) SaveWorkflow(workflow *WorkflowData, host, org, project, branch string) error {
	// Start transaction
	tx, err := s.db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get or create repository
	repoID, err := s.getOrCreateRepositoryTx(tx, host, org, project)
	if err != nil {
		return err
	}

	// Get or create branch
	branchID, err := s.getOrCreateBranchTx(tx, repoID, branch)
	if err != nil {
		return err
	}

	workflow.BranchID = branchID
	workflow.UpdatedAt = time.Now()

	// Insert or replace workflow
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO workflows
		(id, branch_id, name, current_step, state, max_retries, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		workflow.ID,
		workflow.BranchID,
		workflow.Name,
		workflow.CurrentStep,
		string(workflow.State),
		workflow.MaxRetries,
		workflow.Data,
		workflow.CreatedAt.Unix(),
		workflow.UpdatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to save workflow: %w", err)
	}

	return tx.Commit()
}

// SaveWorkflowStep saves or updates a workflow step
func (s *WorkflowStore) SaveWorkflowStep(step *WorkflowStepData) error {
	_, err := s.db.conn.Exec(`
		INSERT OR REPLACE INTO workflow_steps
		(workflow_id, step_index, name, retry_count, message, prompt_template, prepare_data)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workflow_id, step_index) DO UPDATE SET
			name = excluded.name,
			retry_count = excluded.retry_count,
			message = excluded.message,
			prompt_template = excluded.prompt_template,
			prepare_data = excluded.prepare_data`,
		step.WorkflowID,
		step.StepIndex,
		step.Name,
		step.RetryCount,
		step.Message,
		step.PromptTemplate,
		step.PrepareData,
	)
	if err != nil {
		return fmt.Errorf("failed to save workflow step: %w", err)
	}
	return nil
}

// LoadWorkflow loads a workflow by ID
func (s *WorkflowStore) LoadWorkflow(workflowID string) (*WorkflowData, error) {
	var workflow WorkflowData
	var createdAt, updatedAt int64
	var state string

	err := s.db.conn.QueryRow(`
		SELECT id, branch_id, name, current_step, state, max_retries, data, created_at, updated_at
		FROM workflows
		WHERE id = ?`,
		workflowID,
	).Scan(
		&workflow.ID,
		&workflow.BranchID,
		&workflow.Name,
		&workflow.CurrentStep,
		&state,
		&workflow.MaxRetries,
		&workflow.Data,
		&createdAt,
		&updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow: %w", err)
	}

	workflow.State = WorkflowState(state)
	workflow.CreatedAt = time.Unix(createdAt, 0)
	workflow.UpdatedAt = time.Unix(updatedAt, 0)

	return &workflow, nil
}

// LoadWorkflowSteps loads all steps for a workflow
func (s *WorkflowStore) LoadWorkflowSteps(workflowID string) ([]WorkflowStepData, error) {
	rows, err := s.db.conn.Query(`
		SELECT id, workflow_id, step_index, name, retry_count, message, prompt_template, prepare_data
		FROM workflow_steps
		WHERE workflow_id = ?
		ORDER BY step_index`,
		workflowID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow steps: %w", err)
	}
	defer rows.Close()

	var steps []WorkflowStepData
	for rows.Next() {
		var step WorkflowStepData

		err := rows.Scan(
			&step.ID,
			&step.WorkflowID,
			&step.StepIndex,
			&step.Name,
			&step.RetryCount,
			&step.Message,
			&step.PromptTemplate,
			&step.PrepareData,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow step: %w", err)
		}

		steps = append(steps, step)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating workflow steps: %w", err)
	}

	return steps, nil
}

// ListWorkflows lists workflows for a given branch
func (s *WorkflowStore) ListWorkflows(host, org, project, branch string, limit int) ([]WorkflowData, error) {
	query := `
		SELECT w.id, w.branch_id, w.name, w.current_step, w.state, w.max_retries, w.data, w.created_at, w.updated_at
		FROM workflows w
		JOIN branches b ON w.branch_id = b.id
		JOIN repositories r ON b.repository_id = r.id
		WHERE r.host = ? AND r.org = ? AND r.project = ? AND b.name = ?
		ORDER BY w.updated_at DESC`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.conn.Query(query, host, org, project, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}
	defer rows.Close()

	var workflows []WorkflowData
	for rows.Next() {
		var workflow WorkflowData
		var createdAt, updatedAt int64
		var state string

		err := rows.Scan(
			&workflow.ID,
			&workflow.BranchID,
			&workflow.Name,
			&workflow.CurrentStep,
			&state,
			&workflow.MaxRetries,
			&workflow.Data,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow: %w", err)
		}

		workflow.State = WorkflowState(state)
		workflow.CreatedAt = time.Unix(createdAt, 0)
		workflow.UpdatedAt = time.Unix(updatedAt, 0)
		workflows = append(workflows, workflow)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating workflows: %w", err)
	}

	return workflows, nil
}

// ListActiveWorkflows lists workflows that are pending or running
func (s *WorkflowStore) ListActiveWorkflows(host, org, project, branch string) ([]WorkflowData, error) {
	query := `
		SELECT w.id, w.branch_id, w.name, w.current_step, w.state, w.max_retries, w.data, w.created_at, w.updated_at
		FROM workflows w
		JOIN branches b ON w.branch_id = b.id
		JOIN repositories r ON b.repository_id = r.id
		WHERE r.host = ? AND r.org = ? AND r.project = ? AND b.name = ?
		AND w.state IN ('pending', 'running')
		ORDER BY w.updated_at DESC`

	rows, err := s.db.conn.Query(query, host, org, project, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to list active workflows: %w", err)
	}
	defer rows.Close()

	var workflows []WorkflowData
	for rows.Next() {
		var workflow WorkflowData
		var createdAt, updatedAt int64
		var state string

		err := rows.Scan(
			&workflow.ID,
			&workflow.BranchID,
			&workflow.Name,
			&workflow.CurrentStep,
			&state,
			&workflow.MaxRetries,
			&workflow.Data,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow: %w", err)
		}

		workflow.State = WorkflowState(state)
		workflow.CreatedAt = time.Unix(createdAt, 0)
		workflow.UpdatedAt = time.Unix(updatedAt, 0)
		workflows = append(workflows, workflow)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating workflows: %w", err)
	}

	return workflows, nil
}

// DeleteWorkflow deletes a workflow and all its steps
func (s *WorkflowStore) DeleteWorkflow(workflowID string) error {
	result, err := s.db.conn.Exec("DELETE FROM workflows WHERE id = ?", workflowID)
	if err != nil {
		return fmt.Errorf("failed to delete workflow: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("workflow not found: %s", workflowID)
	}

	return nil
}

// UpdateWorkflowState updates just the state of a workflow
func (s *WorkflowStore) UpdateWorkflowState(workflowID string, state WorkflowState) error {
	_, err := s.db.conn.Exec(`
		UPDATE workflows
		SET state = ?, updated_at = ?
		WHERE id = ?`,
		string(state),
		time.Now().Unix(),
		workflowID,
	)
	if err != nil {
		return fmt.Errorf("failed to update workflow state: %w", err)
	}
	return nil
}

// UpdateWorkflowCurrentStep updates the current step of a workflow
func (s *WorkflowStore) UpdateWorkflowCurrentStep(workflowID string, currentStep int) error {
	_, err := s.db.conn.Exec(`
		UPDATE workflows
		SET current_step = ?, updated_at = ?
		WHERE id = ?`,
		currentStep,
		time.Now().Unix(),
		workflowID,
	)
	if err != nil {
		return fmt.Errorf("failed to update workflow current step: %w", err)
	}
	return nil
}

// UpdateWorkflowData updates the data field of a workflow
func (s *WorkflowStore) UpdateWorkflowData(workflowID string, data map[string]string) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal workflow data: %w", err)
	}

	_, err = s.db.conn.Exec(`
		UPDATE workflows
		SET data = ?, updated_at = ?
		WHERE id = ?`,
		string(dataJSON),
		time.Now().Unix(),
		workflowID,
	)
	if err != nil {
		return fmt.Errorf("failed to update workflow data: %w", err)
	}
	return nil
}

// IncrementStepRetryCount increments the retry count for a step
func (s *WorkflowStore) IncrementStepRetryCount(workflowID string, stepIndex int) error {
	_, err := s.db.conn.Exec(`
		UPDATE workflow_steps
		SET retry_count = retry_count + 1
		WHERE workflow_id = ? AND step_index = ?`,
		workflowID,
		stepIndex,
	)
	if err != nil {
		return fmt.Errorf("failed to increment step retry count: %w", err)
	}
	return nil
}

// Helper functions for transactions

func (s *WorkflowStore) getOrCreateRepositoryTx(tx *sql.Tx, host, org, project string) (int64, error) {
	var id int64
	err := tx.QueryRow(
		"SELECT id FROM repositories WHERE host = ? AND org = ? AND project = ?",
		host, org, project,
	).Scan(&id)

	if err == nil {
		return id, nil
	}

	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to query repository: %w", err)
	}

	result, err := tx.Exec(
		"INSERT INTO repositories (host, org, project) VALUES (?, ?, ?)",
		host, org, project,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create repository: %w", err)
	}

	return result.LastInsertId()
}

func (s *WorkflowStore) getOrCreateBranchTx(tx *sql.Tx, repositoryID int64, name string) (int64, error) {
	var id int64
	err := tx.QueryRow(
		"SELECT id FROM branches WHERE repository_id = ? AND name = ?",
		repositoryID, name,
	).Scan(&id)

	if err == nil {
		return id, nil
	}

	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to query branch: %w", err)
	}

	result, err := tx.Exec(
		"INSERT INTO branches (repository_id, name) VALUES (?, ?)",
		repositoryID, name,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create branch: %w", err)
	}

	return result.LastInsertId()
}
