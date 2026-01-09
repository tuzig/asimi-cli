package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

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
	return s.db.conn.Transaction(func(tx *gorm.DB) error {
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
		workflow.UpdatedAt = time.Now().Unix()

		if workflow.CreatedAt == 0 {
			workflow.CreatedAt = time.Now().Unix()
		}

		// Use Save to insert or update
		if err := tx.Save(workflow).Error; err != nil {
			return fmt.Errorf("failed to save workflow: %w", err)
		}

		return nil
	})
}

// SaveWorkflowStep saves or updates a workflow step
func (s *WorkflowStore) SaveWorkflowStep(step *WorkflowStepData) error {
	// Try to find existing step
	var existing WorkflowStepData
	result := s.db.conn.Where("workflow_id = ? AND step_index = ?", step.WorkflowID, step.StepIndex).First(&existing)

	if result.Error == nil {
		// Update existing
		step.ID = existing.ID
		return s.db.conn.Save(step).Error
	}

	if result.Error == gorm.ErrRecordNotFound {
		// Create new
		return s.db.conn.Create(step).Error
	}

	return fmt.Errorf("failed to save workflow step: %w", result.Error)
}

// LoadWorkflow loads a workflow by ID
func (s *WorkflowStore) LoadWorkflow(workflowID string) (*WorkflowData, error) {
	var workflow WorkflowData
	result := s.db.conn.Where("id = ?", workflowID).First(&workflow)

	if result.Error == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if result.Error != nil {
		return nil, fmt.Errorf("failed to load workflow: %w", result.Error)
	}

	return &workflow, nil
}

// LoadWorkflowSteps loads all steps for a workflow
func (s *WorkflowStore) LoadWorkflowSteps(workflowID string) ([]WorkflowStepData, error) {
	var steps []WorkflowStepData
	result := s.db.conn.Where("workflow_id = ?", workflowID).
		Order("step_index").
		Find(&steps)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to load workflow steps: %w", result.Error)
	}

	return steps, nil
}

// ListWorkflows lists workflows for a given branch
func (s *WorkflowStore) ListWorkflows(host, org, project, branch string, limit int) ([]WorkflowData, error) {
	var workflows []WorkflowData
	query := s.db.conn.Model(&WorkflowData{}).
		Joins("JOIN branches ON workflows.branch_id = branches.id").
		Joins("JOIN repositories ON branches.repository_id = repositories.id").
		Where("repositories.host = ? AND repositories.org = ? AND repositories.project = ? AND branches.name = ?",
			host, org, project, branch).
		Order("workflows.updated_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&workflows).Error; err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	return workflows, nil
}

// ListActiveWorkflows lists workflows that are pending or running
func (s *WorkflowStore) ListActiveWorkflows(host, org, project, branch string) ([]WorkflowData, error) {
	var workflows []WorkflowData
	err := s.db.conn.Model(&WorkflowData{}).
		Joins("JOIN branches ON workflows.branch_id = branches.id").
		Joins("JOIN repositories ON branches.repository_id = repositories.id").
		Where("repositories.host = ? AND repositories.org = ? AND repositories.project = ? AND branches.name = ?",
			host, org, project, branch).
		Where("workflows.state IN ?", []string{string(WorkflowStatePending), string(WorkflowStateRunning)}).
		Order("workflows.updated_at DESC").
		Find(&workflows).Error

	if err != nil {
		return nil, fmt.Errorf("failed to list active workflows: %w", err)
	}

	return workflows, nil
}

// DeleteWorkflow deletes a workflow and all its steps
func (s *WorkflowStore) DeleteWorkflow(workflowID string) error {
	result := s.db.conn.Delete(&WorkflowData{}, "id = ?", workflowID)
	if result.Error != nil {
		return fmt.Errorf("failed to delete workflow: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("workflow not found: %s", workflowID)
	}
	return nil
}

// UpdateWorkflowState updates just the state of a workflow
func (s *WorkflowStore) UpdateWorkflowState(workflowID string, state WorkflowState) error {
	result := s.db.conn.Model(&WorkflowData{}).
		Where("id = ?", workflowID).
		Updates(map[string]interface{}{
			"state":      string(state),
			"updated_at": time.Now().Unix(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update workflow state: %w", result.Error)
	}
	return nil
}

// UpdateWorkflowCurrentStep updates the current step of a workflow
func (s *WorkflowStore) UpdateWorkflowCurrentStep(workflowID string, currentStep int) error {
	result := s.db.conn.Model(&WorkflowData{}).
		Where("id = ?", workflowID).
		Updates(map[string]interface{}{
			"current_step": currentStep,
			"updated_at":   time.Now().Unix(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update workflow current step: %w", result.Error)
	}
	return nil
}

// UpdateWorkflowData updates the data field of a workflow
func (s *WorkflowStore) UpdateWorkflowData(workflowID string, data map[string]string) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal workflow data: %w", err)
	}

	result := s.db.conn.Model(&WorkflowData{}).
		Where("id = ?", workflowID).
		Updates(map[string]interface{}{
			"data":       string(dataJSON),
			"updated_at": time.Now().Unix(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update workflow data: %w", result.Error)
	}
	return nil
}

// UpdateStepStatus updates the status and message of a step
func (s *WorkflowStore) UpdateStepStatus(workflowID string, stepIndex int, status StepStatus, message string) error {
	result := s.db.conn.Model(&WorkflowStepData{}).
		Where("workflow_id = ? AND step_index = ?", workflowID, stepIndex).
		Updates(map[string]interface{}{
			"status":  string(status),
			"message": message,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update step status: %w", result.Error)
	}
	return nil
}

// IncrementStepRetryCount increments the retry count for a step
func (s *WorkflowStore) IncrementStepRetryCount(workflowID string, stepIndex int) error {
	result := s.db.conn.Model(&WorkflowStepData{}).
		Where("workflow_id = ? AND step_index = ?", workflowID, stepIndex).
		Update("retry_count", gorm.Expr("retry_count + ?", 1))

	if result.Error != nil {
		return fmt.Errorf("failed to increment step retry count: %w", result.Error)
	}
	return nil
}

// Helper functions for transactions

func (s *WorkflowStore) getOrCreateRepositoryTx(tx *gorm.DB, host, org, project string) (int64, error) {
	var repo Repository
	result := tx.Where("host = ? AND org = ? AND project = ?", host, org, project).First(&repo)

	if result.Error == nil {
		return repo.ID, nil
	}

	if result.Error != gorm.ErrRecordNotFound {
		return 0, fmt.Errorf("failed to query repository: %w", result.Error)
	}

	repo = Repository{
		Host:    host,
		Org:     org,
		Project: project,
	}
	if err := tx.Create(&repo).Error; err != nil {
		return 0, fmt.Errorf("failed to create repository: %w", err)
	}

	return repo.ID, nil
}

func (s *WorkflowStore) getOrCreateBranchTx(tx *gorm.DB, repositoryID int64, name string) (int64, error) {
	var branch Branch
	result := tx.Where("repository_id = ? AND name = ?", repositoryID, name).First(&branch)

	if result.Error == nil {
		return branch.ID, nil
	}

	if result.Error != gorm.ErrRecordNotFound {
		return 0, fmt.Errorf("failed to query branch: %w", result.Error)
	}

	branch = Branch{
		RepositoryID: repositoryID,
		Name:         name,
	}
	if err := tx.Create(&branch).Error; err != nil {
		return 0, fmt.Errorf("failed to create branch: %w", err)
	}

	return branch.ID, nil
}
