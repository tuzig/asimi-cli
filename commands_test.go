package main

import (
	"os"
	"testing"

	"github.com/afittestide/asimi/shogunate"
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

	// Setup a mock model with shogunate
	sg := &shogunate.Shogunate{}
	edictID := sg.SetTestSession("", &shogunate.Session{})
	mockTUI := &TUIModel{
		shogunate:     sg,
		activeEdictID: edictID,
	}

	t.Run("Clean directory", func(t *testing.T) {
		cmd := handleInitCommand(mockTUI, []string{})
		msg := cmd()

		// Check that the message is a startInitWorkflowMsg (new workflow-based implementation)
		initMsg, ok := msg.(startInitWorkflowMsg)
		require.True(t, ok, "Expected startInitWorkflowMsg, got %T", msg)
		require.Equal(t, "AGENTS.md", initMsg.AgentsFile)
		require.False(t, initMsg.ClearMode)

		// Clean up for the next test
		err = os.RemoveAll(".agents")
		require.NoError(t, err)
	})

	t.Run("Some files exist", func(t *testing.T) {
		// Create a dummy Justfile
		err := os.WriteFile("Justfile", []byte("default:\n\techo 'hello'"), 0644)
		require.NoError(t, err)

		cmd := handleInitCommand(mockTUI, []string{})
		msg := cmd()

		// Check that the message is a startInitWorkflowMsg
		initMsg, ok := msg.(startInitWorkflowMsg)
		require.True(t, ok, "Expected startInitWorkflowMsg, got %T", msg)
		require.Equal(t, "AGENTS.md", initMsg.AgentsFile)
		require.False(t, initMsg.ClearMode)

		// Clean up for the next test
		err = os.Remove("Justfile")
		require.NoError(t, err)
		err = os.RemoveAll(".agents")
		require.NoError(t, err)
	})

	t.Run("All files exist", func(t *testing.T) {
		// Create all the files
		err := os.MkdirAll(".agents/sandbox", 0755)
		require.NoError(t, err)
		files := []string{
			"AGENTS.md",
			"Justfile",
			".agents/asimi.conf",
			".agents/sandbox/Dockerfile",
			".agents/sandbox/bashrc",
		}
		for _, file := range files {
			err := os.WriteFile(file, []byte("dummy content"), 0644)
			require.NoError(t, err)
		}

		cmd := handleInitCommand(mockTUI, []string{})
		msg := cmd()

		// Check that the message is a showContextMsg
		contextMsg, ok := msg.(showContextMsg)
		require.True(t, ok, "Expected showContextMsg, got %T", msg)
		require.Contains(t, contextMsg.content, "files already exist")

		// Clean up for the next test
		for _, file := range files {
			err := os.Remove(file)
			require.NoError(t, err)
		}
		err = os.RemoveAll(".agents")
		require.NoError(t, err)
	})

	t.Run("Clear mode", func(t *testing.T) {
		// Create all the files
		err := os.MkdirAll(".agents/sandbox", 0755)
		require.NoError(t, err)
		files := []string{
			"AGENTS.md",
			"Justfile",
			".agents/asimi.conf",
			".agents/sandbox/Dockerfile",
			".agents/sandbox/bashrc",
		}
		originalContent := "original content"
		for _, file := range files {
			err := os.WriteFile(file, []byte(originalContent), 0644)
			require.NoError(t, err)
		}

		cmd := handleInitCommand(mockTUI, []string{"clear"})
		msg := cmd()

		// Check that the message is a startInitWorkflowMsg with clearMode=true
		initMsg, ok := msg.(startInitWorkflowMsg)
		require.True(t, ok, "Expected startInitWorkflowMsg, got %T", msg)
		require.True(t, initMsg.ClearMode, "Expected clearMode to be true")
		require.Equal(t, "AGENTS.md", initMsg.AgentsFile)

		// Clean up
		err = os.RemoveAll(".agents")
		require.NoError(t, err)
		for _, file := range files {
			os.Remove(file) // Ignore errors
		}
	})
}
