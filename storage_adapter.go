package main

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/afittestide/asimi/storage"
	"github.com/tmc/langchaingo/llms"
)

// parseProjectSlug parses a project slug into host, org and project
// Format: "org/project" (from git remote)
// We need to extract host from the Git remote URL
func parseProjectSlug(workingDir string) (host, org, project string) {
	// Try to get the Git remote URL to extract the host
	remote, err := gitRemoteOriginURL(workingDir)
	if err != nil || remote == "" {
		// Fallback: no git remote
		return "local", "local", "unknown"
	}

	// Parse the remote URL to extract host
	host = "github.com" // default
	if strings.Contains(remote, "://") {
		// HTTP(S) URL format
		if u, err := url.Parse(remote); err == nil {
			host = u.Host
		}
	} else if strings.Contains(remote, ":") {
		// SSH format: git@github.com:owner/repo.git
		parts := strings.SplitN(remote, "@", 2)
		if len(parts) == 2 {
			hostPart := strings.SplitN(parts[1], ":", 2)
			if len(hostPart) >= 1 {
				host = hostPart[0]
			}
		}
	}

	// Parse org and project
	owner, repo := parseGitRemote(remote)
	if owner == "" || repo == "" {
		return host, "unknown", "unknown"
	}

	return host, sanitizeSegment(owner), sanitizeSegment(repo)
}

// baseHistory contains common fields and logic for history stores
type baseHistory struct {
	store   *storage.HistoryStore
	host    string
	org     string
	project string
	branch  string
}

// PromptHistory handles prompt history persistence
type PromptHistory struct {
	baseHistory
}

// NewPromptHistoryStore creates a new prompt history store using SQLite
func NewPromptHistoryStore(db *storage.DB, repoInfo RepoInfo) (*PromptHistory, error) {
	if db == nil {
		return nil, fmt.Errorf("storage not initialized")
	}

	host, org, project := parseProjectSlug(repoInfo.ProjectRoot)
	branch := branchSlugOrDefault(repoInfo.Branch)

	// Create history store with config (use defaults if not available)
	histCfg := &storage.HistoryConfig{
		Enabled:     true,
		MaxSessions: 1000,
		MaxAgeDays:  90,
	}

	return &PromptHistory{
		baseHistory: baseHistory{
			store:   storage.NewHistoryStore(db, histCfg),
			host:    host,
			org:     org,
			project: project,
			branch:  branch,
		},
	}, nil
}

// Load reads the prompt history from storage
func (h *PromptHistory) Load() ([]storage.HistoryEntry, error) {
	return h.store.LoadPromptHistory(h.host, h.org, h.project, h.branch, 0)
}

// Save is a no-op for SQLite (entries are saved immediately on Append)
func (h *PromptHistory) Save(entries []storage.HistoryEntry) error {
	return nil // No-op, SQLite saves on append
}

// Append adds a new entry to the prompt history
func (h *PromptHistory) Append(prompt string) error {
	return h.store.AppendPrompt(h.host, h.org, h.project, h.branch, prompt)
}

// Clear removes all prompt history
func (h *PromptHistory) Clear() error {
	return h.store.ClearPromptHistory(h.host, h.org, h.project, h.branch)
}

// CommandHistory handles command history persistence
type CommandHistory struct {
	baseHistory
}

// NewCommandHistoryStore creates a new command history store using SQLite
func NewCommandHistoryStore(db *storage.DB, repoInfo RepoInfo) (*CommandHistory, error) {
	if db == nil {
		return nil, fmt.Errorf("storage not initialized")
	}

	host, org, project := parseProjectSlug(repoInfo.ProjectRoot)
	branch := branchSlugOrDefault(repoInfo.Branch)

	// Create history store with config (use defaults if not available)
	histCfg := &storage.HistoryConfig{
		Enabled:     true,
		MaxSessions: 1000,
		MaxAgeDays:  90,
	}

	return &CommandHistory{
		baseHistory: baseHistory{
			store:   storage.NewHistoryStore(db, histCfg),
			host:    host,
			org:     org,
			project: project,
			branch:  branch,
		},
	}, nil
}

// Load reads the command history from storage
func (h *CommandHistory) Load() ([]storage.HistoryEntry, error) {
	return h.store.LoadCommandHistory(h.host, h.org, h.project, h.branch, 0)
}

// Save is a no-op for SQLite (entries are saved immediately on Append)
func (h *CommandHistory) Save(entries []storage.HistoryEntry) error {
	return nil // No-op, SQLite saves on append
}

// Append adds a new entry to the command history
func (h *CommandHistory) Append(command string) error {
	return h.store.AppendCommand(h.host, h.org, h.project, h.branch, command)
}

// Clear removes all command history
func (h *CommandHistory) Clear() error {
	return h.store.ClearCommandHistory(h.host, h.org, h.project, h.branch)
}

// SessionStore adapter wraps the SQLite session store with the old interface
type SessionStore struct {
	store       *storage.SessionStore
	Host        string
	Org         string
	Project     string
	Branch      string
	ProjectRoot string
	saveChan    chan *Session
	stopChan    chan struct{}
	closeOnce   sync.Once
	wg          sync.WaitGroup // Track in-flight saves
}

// NewSessionStore creates a new session store using SQLite
func NewSessionStore(db *storage.DB, repoInfo RepoInfo, maxSessions, maxAgeDays int) (*SessionStore, error) {
	if db == nil {
		return nil, fmt.Errorf("storage not initialized")
	}

	host, org, project := parseProjectSlug(repoInfo.ProjectRoot)
	branch := branchSlugOrDefault(repoInfo.Branch)

	sessCfg := &storage.SessionConfig{
		Enabled:     true,
		MaxSessions: maxSessions,
		MaxAgeDays:  maxAgeDays,
	}

	store := &SessionStore{
		store:       storage.NewSessionStore(db, sessCfg),
		Host:        host,
		Org:         org,
		Project:     project,
		Branch:      branch,
		ProjectRoot: repoInfo.ProjectRoot,
		saveChan:    make(chan *Session, 100),
		stopChan:    make(chan struct{}),
	}

	// Start async save worker
	go store.saveWorker()

	// Cleanup old sessions
	if err := store.CleanupOldSessions(); err != nil {
		slog.Warn("failed to cleanup old sessions", "error", err)
	}

	return store, nil
}

func (s *SessionStore) saveWorker() {
	for {
		select {
		case session := <-s.saveChan:
			if err := s.saveSessionSync(session); err != nil {
				slog.Warn("failed to save session", "error", err)
			}
			s.wg.Done() // Mark this save as complete
		case <-s.stopChan:
			// Drain remaining saves
			for len(s.saveChan) > 0 {
				session := <-s.saveChan
				if err := s.saveSessionSync(session); err != nil {
					slog.Warn("failed to save session", "error", err)
				}
				s.wg.Done()
			}
			return
		}
	}
}

// SaveSession saves a session asynchronously
func (s *SessionStore) SaveSession(session *Session) {
	if session != nil {
		s.wg.Add(1) // Track this save
		select {
		case s.saveChan <- session:
		default:
			s.wg.Done() // Don't track if we couldn't queue it
			slog.Warn("save channel full, skipping save")
		}
	}
}

// SaveSessionSync saves a session synchronously
func (s *SessionStore) SaveSessionSync(session *Session) error {
	return s.saveSessionSync(session)
}

func (s *SessionStore) saveSessionSync(session *Session) error {
	if session == nil {
		return fmt.Errorf("cannot save nil session")
	}

	// Remove unmatched tool calls before saving
	session.sanitizeMessages()

	// Don't save empty sessions (only system messages)
	hasUserContent := false
	for _, msg := range session.Messages {
		if msg.Role == llms.ChatMessageTypeHuman || msg.Role == llms.ChatMessageTypeAI {
			hasUserContent = true
			break
		}
	}
	if !hasUserContent {
		return nil // Skip saving empty sessions
	}

	// Generate ID and timestamps for new sessions
	if session.ID == "" {
		session.ID = generateSessionID()
	}
	now := time.Now()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.LastUpdated = now

	// Set FirstPrompt if not set
	if session.FirstPrompt == "" && len(session.Messages) > 0 {
		for _, msg := range session.Messages {
			if msg.Role == llms.ChatMessageTypeHuman {
				for _, part := range msg.Parts {
					if textPart, ok := part.(llms.TextContent); ok {
						session.FirstPrompt = textPart.Text
						if len(session.FirstPrompt) > 100 {
							session.FirstPrompt = session.FirstPrompt[:100] + "..."
						}
						break
					}
				}
				if session.FirstPrompt != "" {
					break
				}
			}
		}
	}

	// Convert main.Session to storage.SessionData
	storageSession := &storage.SessionData{
		ID:           session.ID,
		CreatedAt:    session.CreatedAt,
		LastUpdated:  session.LastUpdated,
		FirstPrompt:  session.FirstPrompt,
		Provider:     session.Provider,
		Model:        session.Model,
		WorkingDir:   session.WorkingDir,
		ProjectSlug:  session.ProjectSlug,
		Messages:     session.Messages,
		ContextFiles: session.ContextFiles,
	}

	return s.store.SaveSession(storageSession, s.Host, s.Org, s.Project, s.Branch)
}

// LoadSession loads a session by ID
func (s *SessionStore) LoadSession(id string) (*Session, error) {
	storageSession, host, org, project, branch, err := s.store.LoadSession(id)
	if err != nil {
		return nil, err
	}

	// Verify it's from the same repo/branch (optional check)
	_ = host
	_ = org
	_ = project
	_ = branch

	// Convert storage.SessionData to main.Session
	session := &Session{
		ID:           storageSession.ID,
		CreatedAt:    storageSession.CreatedAt,
		LastUpdated:  storageSession.LastUpdated,
		FirstPrompt:  storageSession.FirstPrompt,
		Provider:     storageSession.Provider,
		Model:        storageSession.Model,
		WorkingDir:   storageSession.WorkingDir,
		ProjectSlug:  storageSession.ProjectSlug,
		Messages:     storageSession.Messages,
		ContextFiles: storageSession.ContextFiles,
	}

	return session, nil
}

// ListSessions lists sessions for the current branch
func (s *SessionStore) ListSessions(limit int) ([]Session, error) {
	storageSessions, err := s.store.ListSessions(s.Host, s.Org, s.Project, s.Branch, limit)
	if err != nil {
		return nil, err
	}

	// Convert []storage.SessionData to []main.Session
	sessions := make([]Session, len(storageSessions))
	for i, ss := range storageSessions {
		sessions[i] = Session{
			ID:           ss.ID,
			CreatedAt:    ss.CreatedAt,
			LastUpdated:  ss.LastUpdated,
			FirstPrompt:  ss.FirstPrompt,
			Provider:     ss.Provider,
			Model:        ss.Model,
			WorkingDir:   ss.WorkingDir,
			ProjectSlug:  ss.ProjectSlug,
			Messages:     ss.Messages,
			ContextFiles: ss.ContextFiles,
			MessageCount: ss.MessageCount,
		}
	}

	return sessions, nil
}

// CleanupOldSessions removes old sessions
func (s *SessionStore) CleanupOldSessions() error {
	return s.store.CleanupOldSessions()
}

// Close closes the session store gracefully, waiting for pending saves to complete
func (s *SessionStore) Close() {
	s.closeOnce.Do(func() {
		close(s.stopChan)
		if waitWithTimeout(&s.wg, 2*time.Second) {
			slog.Debug("session store closed gracefully")
		} else {
			slog.Warn("session store close timed out, some saves may be lost")
		}
	})
}

// waitWithTimeout waits for a WaitGroup with a timeout, returning true if completed
func waitWithTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Flush waits for all pending saves to complete
func (s *SessionStore) Flush() {
	s.wg.Wait()
}
