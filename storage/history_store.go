package storage

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// HistoryStore handles prompt and command history persistence
type HistoryStore struct {
	db  *DB
	cfg *HistoryConfig
}

// NewHistoryStore creates a new history store
func NewHistoryStore(db *DB, cfg *HistoryConfig) *HistoryStore {
	return &HistoryStore{
		db:  db,
		cfg: cfg,
	}
}

// AppendPrompt adds a prompt to the history for a given host/org/project/branch
func (h *HistoryStore) AppendPrompt(host, org, project, branch, prompt string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*10) * time.Millisecond)
		}

		err := h.appendPromptOnce(host, org, project, branch, prompt)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// appendPromptOnce performs a single attempt to append a prompt
func (h *HistoryStore) appendPromptOnce(host, org, project, branch, prompt string) error {
	return h.db.conn.Transaction(func(tx *gorm.DB) error {
		// Get or create repository within transaction
		var repo Repository
		if err := tx.Where(Repository{Host: host, Org: org, Project: project}).
			FirstOrCreate(&repo).Error; err != nil {
			return err
		}

		// Get or create branch within transaction
		var branchRec Branch
		if err := tx.Where(Branch{RepositoryID: repo.ID, Name: branch}).
			FirstOrCreate(&branchRec).Error; err != nil {
			return err
		}

		// Insert prompt
		entry := PromptHistory{
			BranchID:  branchRec.ID,
			Prompt:    prompt,
			Timestamp: time.Now().Unix(),
		}
		if err := tx.Create(&entry).Error; err != nil {
			return fmt.Errorf("failed to append prompt: %w", err)
		}

		// Apply limit if configured
		if h.cfg != nil && h.cfg.MaxSessions > 0 {
			var keepIDs []int64
			if err := tx.Model(&PromptHistory{}).
				Where("branch_id = ?", branchRec.ID).
				Order("timestamp DESC").
				Limit(h.cfg.MaxSessions).
				Pluck("id", &keepIDs).Error; err != nil {
				return err
			}

			if len(keepIDs) > 0 {
				if err := tx.Where("branch_id = ? AND id NOT IN ?", branchRec.ID, keepIDs).
					Delete(&PromptHistory{}).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
}

// AppendCommand adds a command to the history for a given host/org/project/branch
func (h *HistoryStore) AppendCommand(host, org, project, branch, command string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*10) * time.Millisecond)
		}

		err := h.appendCommandOnce(host, org, project, branch, command)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// appendCommandOnce performs a single attempt to append a command
func (h *HistoryStore) appendCommandOnce(host, org, project, branch, command string) error {
	return h.db.conn.Transaction(func(tx *gorm.DB) error {
		// Get or create repository within transaction
		var repo Repository
		if err := tx.Where(Repository{Host: host, Org: org, Project: project}).
			FirstOrCreate(&repo).Error; err != nil {
			return err
		}

		// Get or create branch within transaction
		var branchRec Branch
		if err := tx.Where(Branch{RepositoryID: repo.ID, Name: branch}).
			FirstOrCreate(&branchRec).Error; err != nil {
			return err
		}

		// Insert command
		entry := CommandHistory{
			BranchID:  branchRec.ID,
			Command:   command,
			Timestamp: time.Now().Unix(),
		}
		if err := tx.Create(&entry).Error; err != nil {
			return fmt.Errorf("failed to append command: %w", err)
		}

		// Apply limit if configured
		if h.cfg != nil && h.cfg.MaxSessions > 0 {
			var keepIDs []int64
			if err := tx.Model(&CommandHistory{}).
				Where("branch_id = ?", branchRec.ID).
				Order("timestamp DESC").
				Limit(h.cfg.MaxSessions).
				Pluck("id", &keepIDs).Error; err != nil {
				return err
			}

			if len(keepIDs) > 0 {
				if err := tx.Where("branch_id = ? AND id NOT IN ?", branchRec.ID, keepIDs).
					Delete(&CommandHistory{}).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
}

// LoadPromptHistory loads prompt history for a given host/org/project/branch
func (h *HistoryStore) LoadPromptHistory(host, org, project, branch string, limit int) ([]HistoryEntry, error) {
	// Get repository
	repo, err := h.db.GetRepository(host, org, project)
	if err != nil {
		return nil, err
	}
	if repo == nil {
		return []HistoryEntry{}, nil
	}

	// Get branch
	branchRecord, err := h.db.GetBranch(repo.ID, branch)
	if err != nil {
		return nil, err
	}
	if branchRecord == nil {
		return []HistoryEntry{}, nil
	}

	// Query prompts in chronological order (oldest first)
	var prompts []PromptHistory
	query := h.db.conn.Where("branch_id = ?", branchRecord.ID).
		Order("timestamp ASC, id ASC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&prompts).Error; err != nil {
		return nil, fmt.Errorf("failed to load prompt history: %w", err)
	}

	entries := make([]HistoryEntry, len(prompts))
	for i, p := range prompts {
		entries[i] = HistoryEntry{
			Content:   p.Prompt,
			Timestamp: time.Unix(p.Timestamp, 0),
		}
	}

	return entries, nil
}

// LoadCommandHistory loads command history for a given host/org/project/branch
func (h *HistoryStore) LoadCommandHistory(host, org, project, branch string, limit int) ([]HistoryEntry, error) {
	// Get repository
	repo, err := h.db.GetRepository(host, org, project)
	if err != nil {
		return nil, err
	}
	if repo == nil {
		return []HistoryEntry{}, nil
	}

	// Get branch
	branchRecord, err := h.db.GetBranch(repo.ID, branch)
	if err != nil {
		return nil, err
	}
	if branchRecord == nil {
		return []HistoryEntry{}, nil
	}

	// Query commands in chronological order (oldest first)
	var commands []CommandHistory
	query := h.db.conn.Where("branch_id = ?", branchRecord.ID).
		Order("timestamp ASC, id ASC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&commands).Error; err != nil {
		return nil, fmt.Errorf("failed to load command history: %w", err)
	}

	entries := make([]HistoryEntry, len(commands))
	for i, c := range commands {
		entries[i] = HistoryEntry{
			Content:   c.Command,
			Timestamp: time.Unix(c.Timestamp, 0),
		}
	}

	return entries, nil
}

// ClearPromptHistory clears all prompt history for a given host/org/project/branch
func (h *HistoryStore) ClearPromptHistory(host, org, project, branch string) error {
	// Get repository
	repo, err := h.db.GetRepository(host, org, project)
	if err != nil {
		return err
	}
	if repo == nil {
		return nil
	}

	// Get branch
	branchRecord, err := h.db.GetBranch(repo.ID, branch)
	if err != nil {
		return err
	}
	if branchRecord == nil {
		return nil
	}

	if err := h.db.conn.Where("branch_id = ?", branchRecord.ID).Delete(&PromptHistory{}).Error; err != nil {
		return fmt.Errorf("failed to clear prompt history: %w", err)
	}

	return nil
}

// ClearCommandHistory clears all command history for a given host/org/project/branch
func (h *HistoryStore) ClearCommandHistory(host, org, project, branch string) error {
	// Get repository
	repo, err := h.db.GetRepository(host, org, project)
	if err != nil {
		return err
	}
	if repo == nil {
		return nil
	}

	// Get branch
	branchRecord, err := h.db.GetBranch(repo.ID, branch)
	if err != nil {
		return err
	}
	if branchRecord == nil {
		return nil
	}

	if err := h.db.conn.Where("branch_id = ?", branchRecord.ID).Delete(&CommandHistory{}).Error; err != nil {
		return fmt.Errorf("failed to clear command history: %w", err)
	}

	return nil
}

// CleanupOldHistory removes history entries older than configured age
func (h *HistoryStore) CleanupOldHistory() error {
	if h.cfg == nil || h.cfg.MaxAgeDays <= 0 {
		return nil
	}

	cutoffTime := time.Now().AddDate(0, 0, -h.cfg.MaxAgeDays).Unix()

	// Clean prompt history
	if err := h.db.conn.Where("timestamp < ?", cutoffTime).Delete(&PromptHistory{}).Error; err != nil {
		return fmt.Errorf("failed to cleanup old prompt history: %w", err)
	}

	// Clean command history
	if err := h.db.conn.Where("timestamp < ?", cutoffTime).Delete(&CommandHistory{}).Error; err != nil {
		return fmt.Errorf("failed to cleanup old command history: %w", err)
	}

	return nil
}
