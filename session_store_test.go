package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

func TestSessionStore_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	
	store, err := NewSessionStore(50, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	session := &Session{
		messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{
					llms.TextContent("Hello, world!"),
				},
			},
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.TextContent("Hi there!"),
				},
			},
		},
		contextFiles: map[string]string{
			"test.go": "package main",
		},
	}

	err = store.SaveSession(session, "anthropic", "claude-sonnet-4")
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	sessions, err := store.ListSessions(10)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}

	sessionData, err := store.LoadSession(sessions[0].ID)
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	if len(sessionData.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(sessionData.Messages))
	}

	if sessionData.Provider != "anthropic" {
		t.Fatalf("Expected provider 'anthropic', got '%s'", sessionData.Provider)
	}

	if sessionData.Model != "claude-sonnet-4" {
		t.Fatalf("Expected model 'claude-sonnet-4', got '%s'", sessionData.Model)
	}
}

func TestSessionStore_EmptySession(t *testing.T) {
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	
	store, err := NewSessionStore(50, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	session := &Session{
		messages: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextContent("System prompt"),
				},
			},
		},
		contextFiles: map[string]string{},
	}

	err = store.SaveSession(session, "anthropic", "claude-sonnet-4")
	if err != nil {
		t.Fatalf("SaveSession should not error on empty session: %v", err)
	}

	sessions, err := store.ListSessions(10)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 0 {
		t.Fatalf("Expected 0 sessions (empty session should be skipped), got %d", len(sessions))
	}
}

func TestSessionStore_Cleanup(t *testing.T) {
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	
	store, err := NewSessionStore(2, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	for i := 0; i < 5; i++ {
		session := &Session{
			messages: []llms.MessageContent{
				{
					Role: llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{
						llms.TextContent("Message " + string(rune('0'+i))),
					},
				},
			},
			contextFiles: map[string]string{},
		}

		err = store.SaveSession(session, "anthropic", "claude-sonnet-4")
		if err != nil {
			t.Fatalf("Failed to save session %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	err = store.CleanupOldSessions()
	if err != nil {
		t.Fatalf("Failed to cleanup sessions: %v", err)
	}

	sessions, err := store.ListSessions(10)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("Expected 2 sessions after cleanup (maxSessions=2), got %d", len(sessions))
	}
}

func TestSessionStore_ListSessionsLimit(t *testing.T) {
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	
	store, err := NewSessionStore(50, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	for i := 0; i < 10; i++ {
		session := &Session{
			messages: []llms.MessageContent{
				{
					Role: llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{
						llms.TextContent("Message " + string(rune('0'+i))),
					},
				},
			},
			contextFiles: map[string]string{},
		}

		err = store.SaveSession(session, "anthropic", "claude-sonnet-4")
		if err != nil {
			t.Fatalf("Failed to save session %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	sessions, err := store.ListSessions(5)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 5 {
		t.Fatalf("Expected 5 sessions (limit=5), got %d", len(sessions))
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Now()

	today := formatRelativeTime(now)
	if today[:5] != "Today" {
		t.Errorf("Expected 'Today...', got '%s'", today)
	}

	yesterday := formatRelativeTime(now.AddDate(0, 0, -1))
	if yesterday[:9] != "Yesterday" {
		t.Errorf("Expected 'Yesterday...', got '%s'", yesterday)
	}

	thisYear := formatRelativeTime(now.AddDate(0, -2, 0))
	if thisYear[:3] == "Today" || thisYear[:9] == "Yesterday" {
		t.Errorf("Expected date format, got '%s'", thisYear)
	}
}

func TestSessionStore_DirectoryCreation(t *testing.T) {
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)
	
	store, err := NewSessionStore(50, 30)
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}

	expectedDir := filepath.Join(tempDir, ".local", "share", "asimi", "sessions")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Fatalf("Session directory was not created: %s", expectedDir)
	}

	if store.storageDir != expectedDir {
		t.Fatalf("Expected storageDir '%s', got '%s'", expectedDir, store.storageDir)
	}
}
