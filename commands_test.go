package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindCommand(t *testing.T) {
	registry := NewCommandRegistry()

	tests := []struct {
		name            string
		input           string
		expectFound     bool
		expectCommand   string
		expectMatches   int
		expectAmbiguous bool
	}{
		{
			name:          "exact match with colon",
			input:         ":quit",
			expectFound:   true,
			expectCommand: "quit",
			expectMatches: 1,
		},
		{
			name:          "partial match single - q",
			input:         ":q",
			expectFound:   true,
			expectCommand: "quit",
			expectMatches: 1,
		},
		{
			name:          "partial match single - qu",
			input:         ":qu",
			expectFound:   true,
			expectCommand: "quit",
			expectMatches: 1,
		},
		{
			name:          "partial match single - qui",
			input:         ":qui",
			expectFound:   true,
			expectCommand: "quit",
			expectMatches: 1,
		},
		{
			name:          "partial match single - h",
			input:         ":h",
			expectFound:   true,
			expectCommand: "help",
			expectMatches: 1,
		},
		{
			name:          "partial match single - n",
			input:         ":n",
			expectFound:   true,
			expectCommand: "new",
			expectMatches: 1,
		},
		{
			name:            "ambiguous match - c",
			input:           ":c",
			expectFound:     false,
			expectMatches:   2, // compact and context
			expectAmbiguous: true,
		},
		{
			name:            "ambiguous match - co",
			input:           ":co",
			expectFound:     false,
			expectMatches:   2, // compact and context
			expectAmbiguous: true,
		},
		{
			name:          "partial disambiguated - com",
			input:         ":com",
			expectFound:   true,
			expectCommand: "compact",
			expectMatches: 1,
		},
		{
			name:          "partial disambiguated - con",
			input:         ":con",
			expectFound:   true,
			expectCommand: "context",
			expectMatches: 1,
		},
		{
			name:          "no match",
			input:         ":xyz",
			expectFound:   false,
			expectMatches: 0,
		},
		{
			name:          "empty input",
			input:         "",
			expectFound:   false,
			expectMatches: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, matches, found := registry.FindCommand(tt.input)

			require.Equal(t, tt.expectFound, found, "found mismatch")
			require.Equal(t, tt.expectMatches, len(matches), "matches count mismatch")

			if tt.expectFound {
				require.Equal(t, tt.expectCommand, cmd.Name, "command name mismatch")
			}

			if tt.expectAmbiguous {
				require.False(t, found, "should not find unique match for ambiguous input")
				require.Greater(t, len(matches), 1, "ambiguous should have multiple matches")
			}
		})
	}
}

func TestNormalizeCommandName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: ":help", expected: "help"},
		{input: ":quit", expected: "quit"},
		{input: "", expected: ""},
		{input: ":new", expected: "new"},
		{input: "help", expected: "help"},
		{input: "quit", expected: "quit"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeCommandName(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleInitCommand(t *testing.T) {
	skipIfNotCI(t)
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() {
		err := os.Chdir(originalWd)
		if err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Setup a mock model with no session (simulates no active session state)
	mockTUI := &TUIModel{}

	t.Run("No session returns error message", func(t *testing.T) {
		cmd := handleInitCommand(mockTUI, []string{})
		msg := cmd()

		// Without a session, we should get a showContextMsg with error content
		sysMsg, ok := msg.(showContextMsg)
		require.True(t, ok, "Expected showContextMsg when no session, got %T", msg)
		require.Contains(t, sysMsg.content, "No model connection")
	})

	t.Run("Clear mode with no session removes files", func(t *testing.T) {
		// Create the files that clearAsimiFiles should remove
		err := os.MkdirAll(".agents", 0755)
		require.NoError(t, err)
		err = os.WriteFile(".agents/test", []byte("test"), 0644)
		require.NoError(t, err)
		err = os.WriteFile("AGENTS.md", []byte("# Agents"), 0644)
		require.NoError(t, err)
		err = os.WriteFile("Justfile", []byte("test:"), 0644)
		require.NoError(t, err)

		cmd := handleInitCommand(mockTUI, []string{"clear"})
		msg := cmd()

		// Should still get the "no session" error after clearing files
		sysMsg, ok := msg.(showContextMsg)
		require.True(t, ok, "Expected showContextMsg when no session, got %T", msg)
		require.Contains(t, sysMsg.content, "No model connection")

		// But files should still be removed
		_, err = os.Stat(".agents")
		require.True(t, os.IsNotExist(err), ".agents should be removed")
		_, err = os.Stat("AGENTS.md")
		require.True(t, os.IsNotExist(err), "AGENTS.md should be removed")
		_, err = os.Stat("Justfile")
		require.True(t, os.IsNotExist(err), "Justfile should be removed")
	})

	// TODO: Tests for actual init workflow require a full shogunate setup with a configured session.
	// These tests are skipped until proper integration test infrastructure is added.
	t.Run("Clean directory - skipped without session", func(t *testing.T) {
		t.Skip("Requires shogunate session setup - see integration tests")
	})

	t.Run("Some files exist - skipped without session", func(t *testing.T) {
		t.Skip("Requires shogunate session setup - see integration tests")

		// Clean up for the next test
		err = os.Remove("Justfile")
		require.NoError(t, err)
		err = os.RemoveAll(".agents")
		require.NoError(t, err)
	})

	t.Run("All files exist - skipped without session", func(t *testing.T) {
		t.Skip("Requires shogunate session setup - see integration tests")
	})

	t.Run("Clear mode - skipped without session", func(t *testing.T) {
		t.Skip("Requires shogunate session setup - see integration tests")
	})
}

func TestClearAsimiFiles(t *testing.T) {
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() {
		err := os.Chdir(originalWd)
		if err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	mockTUI := &TUIModel{}

	t.Run("Clears all infrastructure files", func(t *testing.T) {
		// Create the files that clearAsimiFiles should remove
		err := os.MkdirAll(".agents", 0755)
		require.NoError(t, err)
		err = os.WriteFile(".agents/test", []byte("test"), 0644)
		require.NoError(t, err)
		err = os.WriteFile("AGENTS.md", []byte("# Agents"), 0644)
		require.NoError(t, err)
		err = os.WriteFile("Justfile", []byte("test:"), 0644)
		require.NoError(t, err)

		errors := clearAsimiFiles(mockTUI)
		require.Empty(t, errors, "Expected no errors, got: %v", errors)

		// Verify files are removed
		_, err = os.Stat(".agents")
		require.True(t, os.IsNotExist(err), ".agents should be removed")
		_, err = os.Stat("AGENTS.md")
		require.True(t, os.IsNotExist(err), "AGENTS.md should be removed")
		_, err = os.Stat("Justfile")
		require.True(t, os.IsNotExist(err), "Justfile should be removed")
	})

	t.Run("Handles missing files gracefully", func(t *testing.T) {
		// Ensure no files exist
		os.RemoveAll(".agents")
		os.Remove("AGENTS.md")
		os.Remove("Justfile")

		errors := clearAsimiFiles(mockTUI)
		require.Empty(t, errors, "Expected no errors for missing files, got: %v", errors)
	})
}

func TestRunInitGuardrails(t *testing.T) {
	skipIfNotCI(t)
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer func() {
		err := os.Chdir(originalWd)
		if err != nil {
			t.Logf("Failed to change back to original directory: %v", err)
		}
	}()

	// Setup a mock model
	mockTUI := &TUIModel{}

	t.Run("Missing files", func(t *testing.T) {
		// Run guardrails with no files present
		cmd := verifyInit(mockTUI, nil)
		msg := cmd()

		// When there are errors, runInitGuardrails returns a startConversationMsg to retry
		retryMsg, ok := msg.(startConversationMsg)
		require.True(t, ok, "Expected startConversationMsg for retry, got type: %T", msg)
		require.Contains(t, retryMsg.prompt, "❌ AGENTS.md was not created")
		require.Contains(t, retryMsg.prompt, "❌ Justfile was not created")
		require.Contains(t, retryMsg.prompt, "Issues found verifying initialization")
		require.True(t, retryMsg.RunOnHost, "Expected RunOnHost to be true")
	})

	t.Run("Files present", func(t *testing.T) {
		// Create the required files
		err := os.WriteFile("AGENTS.md", []byte("# Test AGENTS.md"), 0644)
		require.NoError(t, err)
		err = os.WriteFile("Justfile", []byte("default:\n\techo 'hello'"), 0644)
		require.NoError(t, err)

		// Run guardrails
		cmd := verifyInit(mockTUI, nil)
		msg := cmd()

		// When just commands fail (which they will in test), it returns startConversationMsg
		// When all passes, it returns showContextMsg
		switch m := msg.(type) {
		case startConversationMsg:
			// Just commands failed, which is expected in test environment
			require.Contains(t, m.prompt, "AGENTS.md created")
			require.Contains(t, m.prompt, "Justfile created")
			require.True(t, m.RunOnHost, "Expected RunOnHost to be true")
		case showContextMsg:
			// All passed (unlikely in test environment)
			require.Contains(t, m.content, "AGENTS.md created")
			require.Contains(t, m.content, "Justfile created")
		default:
			t.Fatalf("Expected startConversationMsg or showContextMsg, got: %T", msg)
		}

		// Clean up
		os.Remove("AGENTS.md")
		os.Remove("Justfile")
	})
}
