package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

func TestSessionStore_SaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &SessionConfig{
		Enabled:     true,
		MaxSessions: 100,
		MaxAgeDays:  30,
	}
	store := NewSessionStore(db, cfg)

	// Create a session with messages
	session := &SessionData{
		ID:          "test-session-1",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Hello world",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp/test",
		Messages: []llms.MessageContent{
			{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}},
			},
			{
				Role:  llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{llms.TextContent{Text: "Hi there!"}},
			},
		},
		ContextFiles: map[string]string{"file.go": "content"},
	}

	err = store.SaveSession(session, "github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Load and verify
	loaded, host, org, project, branch, err := store.LoadSession("test-session-1")
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	if loaded.ID != session.ID {
		t.Errorf("Expected ID %s, got %s", session.ID, loaded.ID)
	}
	if loaded.FirstPrompt != session.FirstPrompt {
		t.Errorf("Expected FirstPrompt %s, got %s", session.FirstPrompt, loaded.FirstPrompt)
	}
	if len(loaded.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(loaded.Messages))
	}
	if host != "github.com" || org != "test" || project != "project" || branch != "main" {
		t.Errorf("Unexpected repo info: %s/%s/%s@%s", host, org, project, branch)
	}
}

func TestSessionStore_LoadNonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_nonexistent_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	_, _, _, _, _, err = store.LoadSession("nonexistent")
	if err == nil {
		t.Error("Expected error when loading non-existent session")
	}
}

func TestSessionStore_ListSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_list_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	// Create sessions
	for i := 0; i < 5; i++ {
		session := &SessionData{
			ID:          "session-" + string(rune('A'+i)),
			CreatedAt:   time.Now(),
			LastUpdated: time.Now(),
			FirstPrompt: "Prompt " + string(rune('A'+i)),
			Provider:    "anthropic",
			Model:       "claude-3",
			WorkingDir:  "/tmp/test",
		}
		store.SaveSession(session, "github.com", "test", "project", "main")
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// List all
	sessions, err := store.ListSessions("github.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	if len(sessions) != 5 {
		t.Errorf("Expected 5 sessions, got %d", len(sessions))
	}

	// List with limit
	sessions, err = store.ListSessions("github.com", "test", "project", "main", 3)
	if err != nil {
		t.Fatalf("Failed to list sessions with limit: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("Expected 3 sessions, got %d", len(sessions))
	}
}

func TestSessionStore_ListAllSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_listall_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	// Create sessions in different repos
	session1 := &SessionData{
		ID:          "session-1",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Prompt 1",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp/test1",
	}
	store.SaveSession(session1, "github.com", "org1", "project1", "main")

	session2 := &SessionData{
		ID:          "session-2",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Prompt 2",
		Provider:    "openai",
		Model:       "gpt-4",
		WorkingDir:  "/tmp/test2",
	}
	store.SaveSession(session2, "github.com", "org2", "project2", "main")

	session3 := &SessionData{
		ID:          "session-3",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Prompt 3",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp/test3",
	}
	store.SaveSession(session3, "gitlab.com", "org3", "project3", "develop")

	// List all sessions across repos
	sessions, err := store.ListAllSessions(0)
	if err != nil {
		t.Fatalf("Failed to list all sessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("Expected 3 sessions, got %d", len(sessions))
	}

	// List with limit
	sessions, err = store.ListAllSessions(2)
	if err != nil {
		t.Fatalf("Failed to list all sessions with limit: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(sessions))
	}
}

func TestSessionStore_DeleteSession(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_delete_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	// Create session
	session := &SessionData{
		ID:          "session-to-delete",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Test",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp/test",
	}
	store.SaveSession(session, "github.com", "test", "project", "main")

	// Delete it
	err = store.DeleteSession("session-to-delete")
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	// Verify it's gone
	_, _, _, _, _, err = store.LoadSession("session-to-delete")
	if err == nil {
		t.Error("Expected error when loading deleted session")
	}

	// Try to delete non-existent
	err = store.DeleteSession("nonexistent")
	if err == nil {
		t.Error("Expected error when deleting non-existent session")
	}
}

func TestSessionStore_CleanupOldSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_cleanup_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &SessionConfig{
		Enabled:     true,
		MaxSessions: 100,
		MaxAgeDays:  1, // 1 day
	}
	store := NewSessionStore(db, cfg)

	// Get branch ID
	repoID, _ := db.GetOrCreateRepository("github.com", "test", "project")
	branchID, _ := db.GetOrCreateBranch(repoID, "main")

	// Create old session directly
	oldTimestamp := time.Now().AddDate(0, 0, -2).Unix()
	db.conn.Create(&DBSession{
		ID:          "old-session",
		BranchID:    branchID,
		CreatedAt:   oldTimestamp,
		LastUpdated: oldTimestamp,
		FirstPrompt: "Old",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp",
	})

	// Create recent session
	session := &SessionData{
		ID:          "recent-session",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Recent",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp",
	}
	store.SaveSession(session, "github.com", "test", "project", "main")

	// Run cleanup
	err = store.CleanupOldSessions()
	if err != nil {
		t.Fatalf("Failed to cleanup old sessions: %v", err)
	}

	// Verify old session is gone
	sessions, _ := store.ListSessions("github.com", "test", "project", "main", 0)
	if len(sessions) != 1 {
		t.Errorf("Expected 1 session after cleanup, got %d", len(sessions))
	}
}

func TestSessionStore_CleanupOldSessions_MaxSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_maxsessions_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	cfg := &SessionConfig{
		Enabled:     true,
		MaxSessions: 3,
		MaxAgeDays:  0, // Don't cleanup by age
	}
	store := NewSessionStore(db, cfg)

	// Create sessions
	for i := 0; i < 5; i++ {
		session := &SessionData{
			ID:          "session-" + string(rune('A'+i)),
			CreatedAt:   time.Now(),
			LastUpdated: time.Now(),
			FirstPrompt: "Prompt",
			Provider:    "anthropic",
			Model:       "claude-3",
			WorkingDir:  "/tmp",
		}
		store.SaveSession(session, "github.com", "test", "project", "main")
		time.Sleep(10 * time.Millisecond)
	}

	// Run cleanup
	err = store.CleanupOldSessions()
	if err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}

	// Should only have 3 most recent
	sessions, _ := store.ListAllSessions(0)
	if len(sessions) != 3 {
		t.Errorf("Expected 3 sessions after max limit cleanup, got %d", len(sessions))
	}
}

func TestSessionStore_CleanupOldSessions_NilConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_nilconfig_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	// Should not error
	err = store.CleanupOldSessions()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSessionStore_SearchMessages(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_search_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	// Create session with messages
	session := &SessionData{
		ID:          "search-session",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Test search",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp/test",
		Messages: []llms.MessageContent{
			{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextContent{Text: "Find the foo function"}},
			},
			{
				Role:  llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{llms.TextContent{Text: "Here is the bar function"}},
			},
		},
	}
	store.SaveSession(session, "github.com", "test", "project", "main")

	// Search for pattern
	results, err := store.SearchMessages("foo", 10)
	if err != nil {
		t.Fatalf("Failed to search messages: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'foo', got %d", len(results))
	}

	// Search for another pattern
	results, err = store.SearchMessages("bar", 10)
	if err != nil {
		t.Fatalf("Failed to search messages: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'bar', got %d", len(results))
	}

	// Search with no matches
	results, err = store.SearchMessages("nonexistent", 10)
	if err != nil {
		t.Fatalf("Failed to search messages: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results for 'nonexistent', got %d", len(results))
	}

	// Invalid regex
	_, err = store.SearchMessages("[invalid", 10)
	if err == nil {
		t.Error("Expected error for invalid regex")
	}
}

func TestSessionStore_SearchMessages_WithLimit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_search_limit_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	// Create sessions with matching messages
	for i := 0; i < 5; i++ {
		session := &SessionData{
			ID:          "session-" + string(rune('A'+i)),
			CreatedAt:   time.Now(),
			LastUpdated: time.Now(),
			FirstPrompt: "Test",
			Provider:    "anthropic",
			Model:       "claude-3",
			WorkingDir:  "/tmp",
			Messages: []llms.MessageContent{
				{
					Role:  llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{llms.TextContent{Text: "keyword match"}},
				},
			},
		}
		store.SaveSession(session, "github.com", "test", "project", "main")
	}

	// Search with limit
	results, err := store.SearchMessages("keyword", 2)
	if err != nil {
		t.Fatalf("Failed to search messages: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results with limit, got %d", len(results))
	}
}

func TestMinMax(t *testing.T) {
	// Test max
	if max(5, 3) != 5 {
		t.Error("max(5, 3) should be 5")
	}
	if max(3, 5) != 5 {
		t.Error("max(3, 5) should be 5")
	}
	if max(5, 5) != 5 {
		t.Error("max(5, 5) should be 5")
	}

	// Test min
	if min(5, 3) != 3 {
		t.Error("min(5, 3) should be 3")
	}
	if min(3, 5) != 3 {
		t.Error("min(3, 5) should be 3")
	}
	if min(5, 5) != 5 {
		t.Error("min(5, 5) should be 5")
	}
}

func TestSessionStore_SaveWithMessages(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_messages_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	// Create session with multiple messages of different types
	session := &SessionData{
		ID:          "msg-session",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Test",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp",
		Messages: []llms.MessageContent{
			{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: "You are helpful"}}},
			{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "Hello"}}},
			{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextContent{Text: "Hi there!"}}},
			{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "How are you?"}}},
			{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextContent{Text: "I'm doing well!"}}},
		},
	}

	err = store.SaveSession(session, "github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Load and verify all messages
	loaded, _, _, _, _, err := store.LoadSession("msg-session")
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	if len(loaded.Messages) != 5 {
		t.Errorf("Expected 5 messages, got %d", len(loaded.Messages))
	}

	// Verify message types preserved
	if loaded.Messages[0].Role != llms.ChatMessageTypeSystem {
		t.Error("First message should be system")
	}
	if loaded.Messages[1].Role != llms.ChatMessageTypeHuman {
		t.Error("Second message should be human")
	}

	// Update session (save again with different messages)
	session.Messages = append(session.Messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: "New message"}},
	})

	err = store.SaveSession(session, "github.com", "test", "project", "main")
	if err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	// Verify update worked
	loaded, _, _, _, _, err = store.LoadSession("msg-session")
	if err != nil {
		t.Fatalf("Failed to reload session: %v", err)
	}
	if len(loaded.Messages) != 6 {
		t.Errorf("Expected 6 messages after update, got %d", len(loaded.Messages))
	}
}

func TestSessionStore_ListWithMessageCount(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session_count_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	// Create session with messages
	session := &SessionData{
		ID:          "count-session",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Test",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp",
		Messages: []llms.MessageContent{
			{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "1"}}},
			{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextContent{Text: "2"}}},
			{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "3"}}},
		},
	}

	store.SaveSession(session, "github.com", "test", "project", "main")

	// List should include message count
	sessions, err := store.ListSessions("github.com", "test", "project", "main", 0)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}

	if sessions[0].MessageCount != 3 {
		t.Errorf("Expected message count 3, got %d", sessions[0].MessageCount)
	}
}

func TestSessionStore_SearchWithSnippet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "search_snippet_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	store := NewSessionStore(db, nil)

	// Create session with long message
	longText := "This is a very long message that contains the keyword FINDME somewhere in the middle of the text. " +
		"The search should return a snippet around this keyword rather than the entire text. " +
		"This helps users quickly find what they're looking for."

	session := &SessionData{
		ID:          "snippet-session",
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		FirstPrompt: "Test",
		Provider:    "anthropic",
		Model:       "claude-3",
		WorkingDir:  "/tmp",
		Messages: []llms.MessageContent{
			{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: longText}}},
		},
	}

	store.SaveSession(session, "github.com", "test", "project", "main")

	// Search
	results, err := store.SearchMessages("FINDME", 10)
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	// Snippet should contain the keyword
	if !strings.Contains(results[0].Snippet, "FINDME") {
		t.Error("Snippet should contain the keyword")
	}
}
