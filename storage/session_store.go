package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
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

	// Update last updated timestamp
	session.LastUpdated = time.Now()

	// Insert or replace session metadata
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO sessions
		(id, branch_id, created_at, last_updated, first_prompt, provider, model, working_dir, tab_type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID,
		branchID,
		session.CreatedAt.Unix(),
		session.LastUpdated.Unix(),
		session.FirstPrompt,
		session.Provider,
		session.Model,
		session.WorkingDir,
		session.TabType,
	)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	// Delete existing messages for this session
	_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", session.ID)
	if err != nil {
		return fmt.Errorf("failed to delete old messages: %w", err)
	}

	// Insert messages
	for i, msg := range session.Messages {
		// Serialize entire message to JSON (not just Parts, to preserve type info)
		contentJSON, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal message content: %w", err)
		}

		_, err = tx.Exec(`
			INSERT INTO messages (session_id, sequence, role, content, created_at)
			VALUES (?, ?, ?, ?, ?)`,
			session.ID,
			i,
			string(msg.Role),
			string(contentJSON),
			time.Now().Unix(),
		)
		if err != nil {
			return fmt.Errorf("failed to insert message %d: %w", i, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.Debug("Session saved", "id", session.ID, "messages", len(session.Messages))
	return nil
}

// LoadSession loads a session by ID with all its messages
// Returns: (session, host, org, project, branch, error)
func (s *SessionStore) LoadSession(sessionID string) (*SessionData, string, string, string, string, error) {
	// Query session metadata with repository and branch info
	var session SessionData
	var host, org, project, branch string
	var createdAt, lastUpdated int64

	err := s.db.conn.QueryRow(`
		SELECT s.id, s.created_at, s.last_updated, s.first_prompt,
		       s.provider, s.model, s.working_dir, s.tab_type,
		       r.host, r.org, r.project, b.name
		FROM sessions s
		JOIN branches b ON s.branch_id = b.id
		JOIN repositories r ON b.repository_id = r.id
		WHERE s.id = ?`,
		sessionID,
	).Scan(
		&session.ID,
		&createdAt,
		&lastUpdated,
		&session.FirstPrompt,
		&session.Provider,
		&session.Model,
		&session.WorkingDir,
		&session.TabType,
		&host,
		&org,
		&project,
		&branch,
	)

	if err == sql.ErrNoRows {
		return nil, "", "", "", "", fmt.Errorf("session not found: %s", sessionID)
	}
	if err != nil {
		return nil, "", "", "", "", fmt.Errorf("failed to load session: %w", err)
	}

	// Convert Unix timestamps to time.Time
	session.CreatedAt = time.Unix(createdAt, 0)
	session.LastUpdated = time.Unix(lastUpdated, 0)
	session.ProjectSlug = fmt.Sprintf("%s/%s/%s", host, org, project)
	session.Messages = []llms.MessageContent{}     // Initialize empty slice
	session.ContextFiles = make(map[string]string) // Initialize empty map

	// Load messages
	rows, err := s.db.conn.Query(`
		SELECT role, content
		FROM messages
		WHERE session_id = ?
		ORDER BY sequence`,
		sessionID,
	)
	if err != nil {
		return nil, "", "", "", "", fmt.Errorf("failed to load messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var role string
		var contentJSON string

		if err := rows.Scan(&role, &contentJSON); err != nil {
			return nil, "", "", "", "", fmt.Errorf("failed to scan message: %w", err)
		}

		// Deserialize entire message from JSON
		var msg llms.MessageContent
		if err := json.Unmarshal([]byte(contentJSON), &msg); err != nil {
			return nil, "", "", "", "", fmt.Errorf("failed to unmarshal message content: %w", err)
		}

		session.Messages = append(session.Messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, "", "", "", "", fmt.Errorf("error iterating messages: %w", err)
	}

	slog.Debug("Session loaded", "id", sessionID, "messages", len(session.Messages))
	return &session, host, org, project, branch, nil
}

// ListSessions lists sessions for a given host/org/project/branch
func (s *SessionStore) ListSessions(host, org, project, branch string, limit int) ([]SessionData, error) {
	query := `
		SELECT s.id, s.created_at, s.last_updated, s.first_prompt,
		       s.provider, s.model, s.working_dir, s.tab_type,
		       COUNT(m.id) as message_count
		FROM sessions s
		JOIN branches b ON s.branch_id = b.id
		JOIN repositories r ON b.repository_id = r.id
		LEFT JOIN messages m ON s.id = m.session_id
		WHERE r.host = ? AND r.org = ? AND r.project = ? AND b.name = ?
		GROUP BY s.id, s.created_at, s.last_updated, s.first_prompt,
		         s.provider, s.model, s.working_dir, s.tab_type
		ORDER BY s.last_updated DESC`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.conn.Query(query, host, org, project, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []SessionData
	for rows.Next() {
		var session SessionData
		var createdAt, lastUpdated int64
		var messageCount int

		err := rows.Scan(
			&session.ID,
			&createdAt,
			&lastUpdated,
			&session.FirstPrompt,
			&session.Provider,
			&session.Model,
			&session.WorkingDir,
			&session.TabType,
			&messageCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		session.CreatedAt = time.Unix(createdAt, 0)
		session.LastUpdated = time.Unix(lastUpdated, 0)
		session.ProjectSlug = fmt.Sprintf("%s/%s/%s", host, org, project)
		session.MessageCount = messageCount
		session.Messages = []llms.MessageContent{} // Empty for list view
		session.ContextFiles = make(map[string]string)

		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return sessions, nil
}

// ListAllSessions lists all sessions across all repositories
func (s *SessionStore) ListAllSessions(limit int) ([]SessionData, error) {
	query := `
		SELECT s.id, s.created_at, s.last_updated, s.first_prompt,
		       s.provider, s.model, s.working_dir, s.tab_type, r.host, r.org, r.project,
		       COUNT(m.id) as message_count
		FROM sessions s
		JOIN branches b ON s.branch_id = b.id
		JOIN repositories r ON b.repository_id = r.id
		LEFT JOIN messages m ON s.id = m.session_id
		GROUP BY s.id, s.created_at, s.last_updated, s.first_prompt,
		         s.provider, s.model, s.working_dir, s.tab_type, r.host, r.org, r.project
		ORDER BY s.last_updated DESC`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list all sessions: %w", err)
	}
	defer rows.Close()

	var sessions []SessionData
	for rows.Next() {
		var session SessionData
		var createdAt, lastUpdated int64
		var host, org, project string
		var messageCount int

		err := rows.Scan(
			&session.ID,
			&createdAt,
			&lastUpdated,
			&session.FirstPrompt,
			&session.Provider,
			&session.Model,
			&session.WorkingDir,
			&session.TabType,
			&host,
			&org,
			&project,
			&messageCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		session.CreatedAt = time.Unix(createdAt, 0)
		session.LastUpdated = time.Unix(lastUpdated, 0)
		session.ProjectSlug = fmt.Sprintf("%s/%s/%s", host, org, project)
		session.MessageCount = messageCount
		session.Messages = []llms.MessageContent{} // Empty for list view
		session.ContextFiles = make(map[string]string)

		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return sessions, nil
}

// ClearSession deletes a session and its messages from storage.
// This explicitly deletes messages first, then the session, ensuring
// cleanup happens atomically in a transaction.
func (s *SessionStore) ClearSession(sessionID string) error {
	tx, err := s.db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete messages: %w", err)
	}
	_, err = tx.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return tx.Commit()
}

// DeleteSession deletes a session (messages are cascade deleted by DB)
func (s *SessionStore) DeleteSession(sessionID string) error {
	result, err := s.db.conn.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
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
		result, err := s.db.conn.Exec(
			"DELETE FROM sessions WHERE last_updated < ?",
			cutoffTime,
		)
		if err != nil {
			return fmt.Errorf("failed to delete old sessions: %w", err)
		}

		if deleted, _ := result.RowsAffected(); deleted > 0 {
			slog.Info("Deleted old sessions", "count", deleted, "max_age_days", s.cfg.MaxAgeDays)
		}
	}

	// Keep only the most recent maxSessions
	if s.cfg.MaxSessions > 0 {
		// For each branch, delete sessions beyond the limit
		_, err := s.db.conn.Exec(`
			DELETE FROM sessions
			WHERE id NOT IN (
				SELECT id FROM sessions
				ORDER BY last_updated DESC
				LIMIT ?
			)`,
			s.cfg.MaxSessions,
		)
		if err != nil {
			return fmt.Errorf("failed to limit session count: %w", err)
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

	// Query all messages (we'll filter in Go since SQLite regexp is limited)
	query := `
		SELECT m.session_id, m.sequence, m.role, m.content,
		       s.first_prompt, s.working_dir,
		       r.org, r.project, b.name
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		JOIN branches b ON s.branch_id = b.id
		JOIN repositories r ON b.repository_id = r.id
		ORDER BY m.created_at DESC`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit*10) // Get more, filter, then limit
	}

	rows, err := s.db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to search messages: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sessionID, role, contentJSON, firstPrompt, workingDir, org, project, branch string
		var sequence int

		err := rows.Scan(
			&sessionID, &sequence, &role, &contentJSON,
			&firstPrompt, &workingDir, &org, &project, &branch,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}

		// Check if content matches regex
		if re.MatchString(contentJSON) {
			// Extract matched text snippet
			matches := re.FindStringSubmatch(contentJSON)
			snippet := contentJSON
			if len(matches) > 0 {
				// Get context around match
				idx := strings.Index(contentJSON, matches[0])
				start := max(0, idx-100)
				end := min(len(contentJSON), idx+len(matches[0])+100)
				snippet = contentJSON[start:end]
			}

			results = append(results, SearchResult{
				SessionID:   sessionID,
				Sequence:    sequence,
				Role:        role,
				Snippet:     snippet,
				FirstPrompt: firstPrompt,
				WorkingDir:  workingDir,
				Org:         org,
				Project:     project,
				Branch:      branch,
			})

			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating messages: %w", err)
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

func (s *SessionStore) getOrCreateRepositoryTx(tx *sql.Tx, host, org, project string) (int64, error) {
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

func (s *SessionStore) getOrCreateBranchTx(tx *sql.Tx, repositoryID int64, name string) (int64, error) {
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
