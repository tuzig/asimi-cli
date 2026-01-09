package storage

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
	"gorm.io/gorm"
)

// SessionStore handles session persistence
type SessionStore struct {
	db  *DB
	cfg *SessionConfig
}

// NewSessionStore creates a new session store
func NewSessionStore(db *DB, cfg *SessionConfig) *SessionStore {
	return &SessionStore{
		db:  db,
		cfg: cfg,
	}
}

// SaveSession saves or updates a session with all its messages
func (s *SessionStore) SaveSession(session *SessionData, host, org, project, branch string) error {
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

		// Update last updated timestamp
		session.LastUpdated = time.Now()

		// Create or update session
		dbSession := DBSession{
			ID:          session.ID,
			BranchID:    branchID,
			CreatedAt:   session.CreatedAt.Unix(),
			LastUpdated: session.LastUpdated.Unix(),
			FirstPrompt: session.FirstPrompt,
			Provider:    session.Provider,
			Model:       session.Model,
			WorkingDir:  session.WorkingDir,
		}

		// Use Save to insert or update
		if err := tx.Save(&dbSession).Error; err != nil {
			return fmt.Errorf("failed to save session: %w", err)
		}

		// Delete existing messages for this session
		if err := tx.Where("session_id = ?", session.ID).Delete(&Message{}).Error; err != nil {
			return fmt.Errorf("failed to delete old messages: %w", err)
		}

		// Insert messages
		for i, msg := range session.Messages {
			contentJSON, err := json.Marshal(msg)
			if err != nil {
				return fmt.Errorf("failed to marshal message content: %w", err)
			}

			message := Message{
				SessionID: session.ID,
				Sequence:  i,
				Role:      string(msg.Role),
				Content:   string(contentJSON),
				CreatedAt: time.Now().Unix(),
			}

			if err := tx.Create(&message).Error; err != nil {
				return fmt.Errorf("failed to insert message %d: %w", i, err)
			}
		}

		slog.Debug("Session saved", "id", session.ID, "messages", len(session.Messages))
		return nil
	})
}

// LoadSession loads a session by ID with all its messages
func (s *SessionStore) LoadSession(sessionID string) (*SessionData, string, string, string, string, error) {
	var dbSession DBSession

	// Query session with preloaded Branch and Repository
	result := s.db.conn.
		Preload("Branch.Repository").
		Where("id = ?", sessionID).
		First(&dbSession)

	if result.Error == gorm.ErrRecordNotFound {
		return nil, "", "", "", "", fmt.Errorf("session not found: %s", sessionID)
	}
	if result.Error != nil {
		return nil, "", "", "", "", fmt.Errorf("failed to load session: %w", result.Error)
	}

	// Extract repository and branch info from preloaded relations
	host := dbSession.Branch.Repository.Host
	org := dbSession.Branch.Repository.Org
	project := dbSession.Branch.Repository.Project
	branch := dbSession.Branch.Name

	// Load messages
	var messages []Message
	if err := s.db.conn.Where("session_id = ?", sessionID).
		Order("sequence").
		Find(&messages).Error; err != nil {
		return nil, "", "", "", "", fmt.Errorf("failed to load messages: %w", err)
	}

	// Convert to SessionData
	session := &SessionData{
		ID:           dbSession.ID,
		CreatedAt:    time.Unix(dbSession.CreatedAt, 0),
		LastUpdated:  time.Unix(dbSession.LastUpdated, 0),
		FirstPrompt:  dbSession.FirstPrompt,
		Provider:     dbSession.Provider,
		Model:        dbSession.Model,
		WorkingDir:   dbSession.WorkingDir,
		ProjectSlug:  fmt.Sprintf("%s/%s/%s", host, org, project),
		Messages:     make([]llms.MessageContent, 0, len(messages)),
		ContextFiles: make(map[string]string),
	}

	for _, msg := range messages {
		var llmMsg llms.MessageContent
		if err := json.Unmarshal([]byte(msg.Content), &llmMsg); err != nil {
			return nil, "", "", "", "", fmt.Errorf("failed to unmarshal message content: %w", err)
		}
		session.Messages = append(session.Messages, llmMsg)
	}

	slog.Debug("Session loaded", "id", sessionID, "messages", len(session.Messages))
	return session, host, org, project, branch, nil
}

// ListSessions lists sessions for a given host/org/project/branch
func (s *SessionStore) ListSessions(host, org, project, branch string, limit int) ([]SessionData, error) {
	type sessionWithCount struct {
		DBSession
		MessageCount int64
	}

	var results []sessionWithCount
	query := s.db.conn.Model(&DBSession{}).
		Select("sessions.*, COUNT(messages.id) as message_count").
		Joins("JOIN branches ON sessions.branch_id = branches.id").
		Joins("JOIN repositories ON branches.repository_id = repositories.id").
		Joins("LEFT JOIN messages ON sessions.id = messages.session_id").
		Where("repositories.host = ? AND repositories.org = ? AND repositories.project = ? AND branches.name = ?",
			host, org, project, branch).
		Group("sessions.id").
		Order("sessions.last_updated DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	sessions := make([]SessionData, len(results))
	for i, r := range results {
		sessions[i] = SessionData{
			ID:           r.ID,
			CreatedAt:    time.Unix(r.CreatedAt, 0),
			LastUpdated:  time.Unix(r.LastUpdated, 0),
			FirstPrompt:  r.FirstPrompt,
			Provider:     r.Provider,
			Model:        r.Model,
			WorkingDir:   r.WorkingDir,
			ProjectSlug:  fmt.Sprintf("%s/%s/%s", host, org, project),
			MessageCount: int(r.MessageCount),
			Messages:     []llms.MessageContent{},
			ContextFiles: make(map[string]string),
		}
	}

	return sessions, nil
}

// ListAllSessions lists all sessions across all repositories
func (s *SessionStore) ListAllSessions(limit int) ([]SessionData, error) {
	type sessionWithInfo struct {
		ID           string
		CreatedAt    int64
		LastUpdated  int64
		FirstPrompt  string
		Provider     string
		Model        string
		WorkingDir   string
		Host         string
		Org          string
		Project      string
		MessageCount int64
	}

	var results []sessionWithInfo
	query := s.db.conn.Model(&DBSession{}).
		Select("sessions.id, sessions.created_at, sessions.last_updated, sessions.first_prompt, "+
			"sessions.provider, sessions.model, sessions.working_dir, "+
			"repositories.host, repositories.org, repositories.project, "+
			"COUNT(messages.id) as message_count").
		Joins("JOIN branches ON sessions.branch_id = branches.id").
		Joins("JOIN repositories ON branches.repository_id = repositories.id").
		Joins("LEFT JOIN messages ON sessions.id = messages.session_id").
		Group("sessions.id").
		Order("sessions.last_updated DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to list all sessions: %w", err)
	}

	sessions := make([]SessionData, len(results))
	for i, r := range results {
		sessions[i] = SessionData{
			ID:           r.ID,
			CreatedAt:    time.Unix(r.CreatedAt, 0),
			LastUpdated:  time.Unix(r.LastUpdated, 0),
			FirstPrompt:  r.FirstPrompt,
			Provider:     r.Provider,
			Model:        r.Model,
			WorkingDir:   r.WorkingDir,
			ProjectSlug:  fmt.Sprintf("%s/%s/%s", r.Host, r.Org, r.Project),
			MessageCount: int(r.MessageCount),
			Messages:     []llms.MessageContent{},
			ContextFiles: make(map[string]string),
		}
	}

	return sessions, nil
}

// DeleteSession deletes a session and all its messages
func (s *SessionStore) DeleteSession(sessionID string) error {
	result := s.db.conn.Delete(&DBSession{}, "id = ?", sessionID)
	if result.Error != nil {
		return fmt.Errorf("failed to delete session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	return nil
}

// CleanupOldSessions deletes sessions older than maxAgeDays or exceeding maxSessions count
func (s *SessionStore) CleanupOldSessions() error {
	if s.cfg == nil {
		return nil
	}

	// Delete sessions older than maxAgeDays
	if s.cfg.MaxAgeDays > 0 {
		cutoffTime := time.Now().AddDate(0, 0, -s.cfg.MaxAgeDays).Unix()
		result := s.db.conn.Where("last_updated < ?", cutoffTime).Delete(&DBSession{})
		if result.Error != nil {
			return fmt.Errorf("failed to delete old sessions: %w", result.Error)
		}
		if result.RowsAffected > 0 {
			slog.Info("Deleted old sessions", "count", result.RowsAffected, "max_age_days", s.cfg.MaxAgeDays)
		}
	}

	// Keep only the most recent maxSessions
	if s.cfg.MaxSessions > 0 {
		// Get IDs to keep
		var keepIDs []string
		if err := s.db.conn.Model(&DBSession{}).
			Order("last_updated DESC").
			Limit(s.cfg.MaxSessions).
			Pluck("id", &keepIDs).Error; err != nil {
			return fmt.Errorf("failed to get sessions to keep: %w", err)
		}

		if len(keepIDs) > 0 {
			if err := s.db.conn.Where("id NOT IN ?", keepIDs).Delete(&DBSession{}).Error; err != nil {
				return fmt.Errorf("failed to limit session count: %w", err)
			}
		}
	}

	return nil
}

// SearchMessages searches for messages matching a regex pattern
func (s *SessionStore) SearchMessages(pattern string, limit int) ([]SearchResult, error) {
	// Compile regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// Query all messages (filter in Go since SQLite regexp is limited)
	type messageWithInfo struct {
		SessionID   string
		Sequence    int
		Role        string
		Content     string
		FirstPrompt string
		WorkingDir  string
		Org         string
		Project     string
		BranchName  string
	}

	var messages []messageWithInfo
	queryLimit := limit * 10
	if limit <= 0 {
		queryLimit = 1000
	}

	err = s.db.conn.Model(&Message{}).
		Select("messages.session_id, messages.sequence, messages.role, messages.content, "+
			"sessions.first_prompt, sessions.working_dir, "+
			"repositories.org, repositories.project, branches.name as branch_name").
		Joins("JOIN sessions ON messages.session_id = sessions.id").
		Joins("JOIN branches ON sessions.branch_id = branches.id").
		Joins("JOIN repositories ON branches.repository_id = repositories.id").
		Order("messages.created_at DESC").
		Limit(queryLimit).
		Find(&messages).Error

	if err != nil {
		return nil, fmt.Errorf("failed to search messages: %w", err)
	}

	var results []SearchResult
	for _, m := range messages {
		if re.MatchString(m.Content) {
			// Extract matched text snippet
			matches := re.FindStringSubmatch(m.Content)
			snippet := m.Content
			if len(matches) > 0 {
				idx := strings.Index(m.Content, matches[0])
				start := max(0, idx-100)
				end := min(len(m.Content), idx+len(matches[0])+100)
				snippet = m.Content[start:end]
			}

			results = append(results, SearchResult{
				SessionID:   m.SessionID,
				Sequence:    m.Sequence,
				Role:        m.Role,
				Snippet:     snippet,
				FirstPrompt: m.FirstPrompt,
				WorkingDir:  m.WorkingDir,
				Org:         m.Org,
				Project:     m.Project,
				Branch:      m.BranchName,
			})

			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}

	return results, nil
}

// SearchResult represents a search result
type SearchResult struct {
	SessionID   string
	Sequence    int
	Role        string
	Snippet     string
	FirstPrompt string
	WorkingDir  string
	Org         string
	Project     string
	Branch      string
}

// Helper functions for transactions

func (s *SessionStore) getOrCreateRepositoryTx(tx *gorm.DB, host, org, project string) (int64, error) {
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

func (s *SessionStore) getOrCreateBranchTx(tx *gorm.DB, repositoryID int64, name string) (int64, error) {
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

// Helper function for min/max (Go 1.21+)
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
