package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
)

type SessionMetadata struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	LastUpdated  time.Time `json:"last_updated"`
	FirstPrompt  string    `json:"first_prompt"`
	MessageCount int       `json:"message_count"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	WorkingDir   string    `json:"working_dir"`
}

type SessionData struct {
	Metadata     SessionMetadata       `json:"metadata"`
	Messages     []llms.MessageContent `json:"messages"`
	ContextFiles map[string]string     `json:"context_files"`
	Provider     string                `json:"provider"`
	Model        string                `json:"model"`
}

type SessionIndex struct {
	Sessions []SessionMetadata `json:"sessions"`
}

type SessionStore struct {
	storageDir  string
	maxSessions int
	maxAgeDays  int
}

func NewSessionStore(maxSessions, maxAgeDays int) (*SessionStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	storageDir := filepath.Join(homeDir, ".local", "share", "asimi", "sessions")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session storage directory: %w", err)
	}

	store := &SessionStore{
		storageDir:  storageDir,
		maxSessions: maxSessions,
		maxAgeDays:  maxAgeDays,
	}

	if err := store.CleanupOldSessions(); err != nil {
		fmt.Printf("Warning: failed to cleanup old sessions: %v\n", err)
	}

	return store, nil
}

func generateSessionID() string {
	timestamp := time.Now().Format("2006-01-02-150405")
	
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	suffix := hex.EncodeToString(randomBytes)
	
	return fmt.Sprintf("%s-%s", timestamp, suffix)
}

func (s *SessionStore) SaveSession(session *Session, provider, model string) error {
	if session == nil {
		return fmt.Errorf("cannot save nil session")
	}

	hasUserMessage := false
	for _, msg := range session.messages {
		if msg.Role == llms.ChatMessageTypeHuman {
			hasUserMessage = true
			break
		}
	}
	if !hasUserMessage {
		return nil // Silently skip empty sessions
	}

	sessionID := generateSessionID()
	
	firstPrompt := ""
	for _, msg := range session.messages {
		if msg.Role == llms.ChatMessageTypeHuman {
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					firstPrompt = string(textPart)
					break
				}
			}
			if firstPrompt != "" {
				break
			}
		}
	}
	
	if len(firstPrompt) > 60 {
		firstPrompt = firstPrompt[:57] + "..."
	}

	workingDir, _ := os.Getwd()

	now := time.Now()
	metadata := SessionMetadata{
		ID:           sessionID,
		CreatedAt:    now,
		LastUpdated:  now,
		FirstPrompt:  firstPrompt,
		MessageCount: len(session.messages),
		Model:        model,
		Provider:     provider,
		WorkingDir:   workingDir,
	}

	sessionData := SessionData{
		Metadata:     metadata,
		Messages:     session.messages,
		ContextFiles: session.contextFiles,
		Provider:     provider,
		Model:        model,
	}

	sessionDir := filepath.Join(s.storageDir, "session-"+sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	sessionFile := filepath.Join(sessionDir, "session.json")
	sessionJSON, err := json.MarshalIndent(sessionData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}
	if err := os.WriteFile(sessionFile, sessionJSON, 0644); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	metadataFile := filepath.Join(sessionDir, "metadata.json")
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := os.WriteFile(metadataFile, metadataJSON, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	if err := s.updateIndex(metadata); err != nil {
		return fmt.Errorf("failed to update index: %w", err)
	}

	return nil
}

func (s *SessionStore) LoadSession(id string) (*SessionData, error) {
	sessionDir := filepath.Join(s.storageDir, "session-"+id)
	sessionFile := filepath.Join(sessionDir, "session.json")

	data, err := os.ReadFile(sessionFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var sessionData SessionData
	if err := json.Unmarshal(data, &sessionData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	return &sessionData, nil
}

func (s *SessionStore) ListSessions(limit int) ([]SessionMetadata, error) {
	index, err := s.loadIndex()
	if err != nil {
		return nil, err
	}

	sort.Slice(index.Sessions, func(i, j int) bool {
		return index.Sessions[i].LastUpdated.After(index.Sessions[j].LastUpdated)
	})

	if limit > 0 && len(index.Sessions) > limit {
		return index.Sessions[:limit], nil
	}

	return index.Sessions, nil
}

func (s *SessionStore) CleanupOldSessions() error {
	index, err := s.loadIndex()
	if err != nil {
		return err
	}

	sort.Slice(index.Sessions, func(i, j int) bool {
		return index.Sessions[i].LastUpdated.After(index.Sessions[j].LastUpdated)
	})

	var sessionsToKeep []SessionMetadata
	cutoffTime := time.Now().AddDate(0, 0, -s.maxAgeDays)

	for i, session := range index.Sessions {
		if i < s.maxSessions && session.LastUpdated.After(cutoffTime) {
			sessionsToKeep = append(sessionsToKeep, session)
		} else {
			sessionDir := filepath.Join(s.storageDir, "session-"+session.ID)
			if err := os.RemoveAll(sessionDir); err != nil {
				fmt.Printf("Warning: failed to remove old session %s: %v\n", session.ID, err)
			}
		}
	}

	index.Sessions = sessionsToKeep
	return s.saveIndex(index)
}

func (s *SessionStore) loadIndex() (*SessionIndex, error) {
	indexFile := filepath.Join(s.storageDir, "index.json")
	
	if _, err := os.Stat(indexFile); os.IsNotExist(err) {
		return &SessionIndex{Sessions: []SessionMetadata{}}, nil
	}

	data, err := os.ReadFile(indexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	var index SessionIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to unmarshal index: %w", err)
	}

	return &index, nil
}

func (s *SessionStore) saveIndex(index *SessionIndex) error {
	indexFile := filepath.Join(s.storageDir, "index.json")
	
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	if err := os.WriteFile(indexFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	return nil
}

func (s *SessionStore) updateIndex(metadata SessionMetadata) error {
	index, err := s.loadIndex()
	if err != nil {
		return err
	}

	found := false
	for i, session := range index.Sessions {
		if session.ID == metadata.ID {
			index.Sessions[i] = metadata
			found = true
			break
		}
	}

	if !found {
		index.Sessions = append(index.Sessions, metadata)
	}

	return s.saveIndex(index)
}

func formatRelativeTime(t time.Time) string {
	now := time.Now()
	
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return fmt.Sprintf("Today %s", t.Format("15:04"))
	}
	
	yesterday := now.AddDate(0, 0, -1)
	if t.Year() == yesterday.Year() && t.YearDay() == yesterday.YearDay() {
		return fmt.Sprintf("Yesterday %s", t.Format("15:04"))
	}
	
	if t.Year() == now.Year() {
		return t.Format("Jan 2, 15:04")
	}
	
	return t.Format("Jan 2 2006, 15:04")
}

func FormatSessionList(sessions []SessionMetadata) string {
	if len(sessions) == 0 {
		return "No previous sessions found. Start chatting to create a new session!"
	}

	var b strings.Builder
	b.WriteString("Recent Sessions:\n\n")

	for i, session := range sessions {
		b.WriteString(fmt.Sprintf("%2d. [%s] %s\n", i+1, formatRelativeTime(session.LastUpdated), session.FirstPrompt))
		b.WriteString(fmt.Sprintf("    %d messages • %s", session.MessageCount, session.Model))
		
		currentDir, _ := os.Getwd()
		if session.WorkingDir != "" && session.WorkingDir != currentDir {
			shortPath := session.WorkingDir
			homeDir, _ := os.UserHomeDir()
			if homeDir != "" {
				shortPath = strings.Replace(shortPath, homeDir, "~", 1)
			}
			b.WriteString(fmt.Sprintf(" • %s", shortPath))
		}
		
		b.WriteString("\n")
		if i < len(sessions)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}
