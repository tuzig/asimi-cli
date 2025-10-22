package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// HistoryEntry represents a single prompt in the history with metadata
type HistoryEntry struct {
	Prompt    string    `json:"prompt"`
	Timestamp time.Time `json:"timestamp"`
	// We don't persist SessionSnapshot and ChatSnapshot as they're session-specific
}

// HistoryStore manages persistent storage of prompt history
type HistoryStore struct {
	filePath string
	maxSize  int // Maximum number of entries to keep
}

// NewHistoryStore creates a new history store
func NewHistoryStore() (*HistoryStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}
	projectRoot := findProjectRoot(cwd)
	slug := projectSlug(projectRoot)
	if slug == "" {
		slug = defaultProjectSlug
	}

	repoBase := filepath.Join(homeDir, ".local", "share", "asimi", "repo")
	projectDir := filepath.Join(repoBase, filepath.FromSlash(slug))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	store := &HistoryStore{
		filePath: filepath.Join(projectDir, "history.json"),
		maxSize:  1000, // Keep last 1000 prompts
	}
	store.migrateLegacyHistory(filepath.Join(homeDir, ".local", "share", "asimi", "history.json"))
	return store, nil
}

func (h *HistoryStore) migrateLegacyHistory(legacyPath string) {
	if _, err := os.Stat(h.filePath); err == nil {
		return
	}

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return
	}

	_ = os.WriteFile(h.filePath, data, 0o644)
}

// Load reads the history from disk
func (h *HistoryStore) Load() ([]HistoryEntry, error) {
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No history file yet, return empty slice
			return []HistoryEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read history file: %w", err)
	}

	var entries []HistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// If we can't parse the history, log the error and start fresh
		slog.Warn("failed to parse history file, starting fresh", "error", err)
		return []HistoryEntry{}, nil
	}

	return entries, nil
}

// Save writes the history to disk
func (h *HistoryStore) Save(entries []HistoryEntry) error {
	// Trim to max size if needed
	if len(entries) > h.maxSize {
		entries = entries[len(entries)-h.maxSize:]
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	// Write to a temporary file first, then rename for atomicity
	tmpPath := h.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write history file: %w", err)
	}

	if err := os.Rename(tmpPath, h.filePath); err != nil {
		return fmt.Errorf("failed to rename history file: %w", err)
	}

	return nil
}

// Append adds a new entry to the history and saves it
func (h *HistoryStore) Append(prompt string) error {
	entries, err := h.Load()
	if err != nil {
		return err
	}

	// Don't add duplicate consecutive entries
	if len(entries) > 0 && entries[len(entries)-1].Prompt == prompt {
		return nil
	}

	entries = append(entries, HistoryEntry{
		Prompt:    prompt,
		Timestamp: time.Now(),
	})

	return h.Save(entries)
}

// Clear removes all history
func (h *HistoryStore) Clear() error {
	if err := os.Remove(h.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear history: %w", err)
	}
	return nil
}
