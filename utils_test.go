package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

func TestGitHelpersReturnRepositoryState(t *testing.T) {
	tempDir := t.TempDir()

	repo, worktree := initTempRepo(t, tempDir)

	// Switch to the temporary repository directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalDir))
	})
	require.NoError(t, os.Chdir(tempDir))

	// Reset the git info manager so it loads this repository
	t.Cleanup(func() {
		defaultGitInfoManager = newGitInfoManager()
	})
	defaultGitInfoManager = newGitInfoManager()

	require.True(t, isGitRepository(), "expected temporary directory to be detected as git repository")

	expectedBranch := currentBranchName(t, repo)
	require.Equal(t, expectedBranch, getCurrentGitBranch())

	require.Empty(t, getGitStatus(), "freshly committed repository should report clean status")

	// Create an untracked and a modified file to trigger status updates
	untrackedFile := filepath.Join(tempDir, "untracked.txt")
	require.NoError(t, os.WriteFile(untrackedFile, []byte("hello"), 0o644))

	trackedFile := filepath.Join(tempDir, "tracked.txt")
	require.NoError(t, os.WriteFile(trackedFile, []byte("first\n"), 0o644))
	_, err = worktree.Add("tracked.txt")
	require.NoError(t, err)
	_, err = worktree.Commit("add tracked file", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(trackedFile, []byte("first\nsecond\n"), 0o644))

	// Explicitly refresh git info to pick up the changes
	refreshGitInfo()

	// Wait for the async refresh to complete
	require.Eventually(t, func() bool {
		return getGitStatus() == "[!?]"
	}, 2*time.Second, 50*time.Millisecond, "status should reflect modified tracked file and untracked file")
}

func initTempRepo(t *testing.T, dir string) (*gogit.Repository, *gogit.Worktree) {
	repo, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	worktree, err := repo.Worktree()
	require.NoError(t, err)

	initialFile := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(initialFile, []byte("temp repo\n"), 0o644))

	_, err = worktree.Add("README.md")
	require.NoError(t, err)

	_, err = worktree.Commit("initial commit", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})
	require.NoError(t, err)

	// Create and checkout a "main" branch so our helpers see a familiar name
	require.NoError(t, worktree.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("main"),
		Create: true,
	}))

	return repo, worktree
}

func currentBranchName(t *testing.T, repo *gogit.Repository) string {
	head, err := repo.Head()
	require.NoError(t, err)
	return head.Name().Short()
}

func TestShortenProviderModel(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		expected string
	}{
		{
			name:     "Claude Haiku 4.5",
			provider: "anthropic",
			model:    "Claude-Haiku-4.5",
			expected: "Claude-Haiku-4.5",
		},
		{
			name:     "Claude 3.5 Sonnet",
			provider: "anthropic",
			model:    "Claude 3.5 Sonnet",
			expected: "Claude-3.5-Sonnet",
		},
		{
			name:     "claude-3-5-sonnet-20241022",
			provider: "anthropic",
			model:    "claude-3-5-sonnet-20241022",
			expected: "Claude-3.5-Sonnet",
		},
		{
			name:     "claude-3-haiku-20240307",
			provider: "anthropic",
			model:    "claude-3-haiku-20240307",
			expected: "Claude-3-Haiku",
		},
		{
			name:     "claude-3-5-haiku-latest",
			provider: "anthropic",
			model:    "claude-3-5-haiku-latest",
			expected: "Claude-3.5-Haiku",
		},
		{
			name:     "GPT-4 Turbo",
			provider: "openai",
			model:    "gpt-4-turbo",
			expected: "GPT-4T",
		},
		{
			name:     "GPT-3.5",
			provider: "openai",
			model:    "gpt-3.5-turbo",
			expected: "GPT-3.5",
		},
		{
			name:     "Gemini Pro",
			provider: "google",
			model:    "gemini-pro",
			expected: "Gemini-Pro",
		},
		{
			name:     "Gemini Flash",
			provider: "googleai",
			model:    "gemini-1.5-flash",
			expected: "Gemini-Flash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortenProviderModel(tt.provider, tt.model)
			require.Equal(t, tt.expected, result)
		})
	}
}
