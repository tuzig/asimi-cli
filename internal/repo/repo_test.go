package repo

import (
	"os"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/require"
)

func TestIsMainBranch(t *testing.T) {
	tests := []struct {
		name     string
		branch   string
		expected bool
	}{
		{"main branch", "main", true},
		{"master branch", "master", true},
		{"feature branch", "feature/test", false},
		{"develop branch", "develop", false},
		{"empty branch", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMainBranch(tt.branch)
			require.Equal(t, tt.expected, result, "isMainBranch(%q)", tt.branch)
		})
	}
}

func TestSummarizeStatus(t *testing.T) {
	cases := []struct {
		name     string
		status   gogit.Status
		expected string
	}{
		{
			name:     "empty status",
			status:   gogit.Status{},
			expected: "",
		},
		{
			name: "mixed indicators",
			status: gogit.Status{
				"modified.go": &gogit.FileStatus{
					Staging:  gogit.Modified,
					Worktree: gogit.Unmodified,
				},
				"staged_added.go": &gogit.FileStatus{
					Staging:  gogit.Added,
					Worktree: gogit.Unmodified,
				},
				"deleted.txt": &gogit.FileStatus{
					Staging:  gogit.Deleted,
					Worktree: gogit.Unmodified,
				},
				"renamed.txt": &gogit.FileStatus{
					Staging:  gogit.Renamed,
					Worktree: gogit.Unmodified,
				},
				"untracked.md": &gogit.FileStatus{
					Staging:  gogit.Untracked,
					Worktree: gogit.Untracked,
				},
				"worktree_modified.go": &gogit.FileStatus{
					Staging:  gogit.Unmodified,
					Worktree: gogit.Modified,
				},
			},
			expected: "[!+-→?]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, summarizeStatus(tc.status))
		})
	}
}

func TestDiffLines(t *testing.T) {
	tests := []struct {
		name            string
		original        []string
		current         []string
		expectedAdded   int
		expectedDeleted int
	}{
		{
			name:            "no changes",
			original:        []string{"line1", "line2", "line3"},
			current:         []string{"line1", "line2", "line3"},
			expectedAdded:   0,
			expectedDeleted: 0,
		},
		{
			name:            "only additions",
			original:        []string{"line1", "line2"},
			current:         []string{"line1", "line2", "line3", "line4"},
			expectedAdded:   2,
			expectedDeleted: 0,
		},
		{
			name:            "only deletions",
			original:        []string{"line1", "line2", "line3", "line4"},
			current:         []string{"line1", "line2"},
			expectedAdded:   0,
			expectedDeleted: 2,
		},
		{
			name:            "balanced changes (should be modifications)",
			original:        []string{"line1", "line2", "line3"},
			current:         []string{"line1", "newline2", "newline3"},
			expectedAdded:   2,
			expectedDeleted: 2,
		},
		{
			name:            "more additions than deletions",
			original:        []string{"line1", "line2"},
			current:         []string{"line1", "newline2", "line3", "line4"},
			expectedAdded:   3,
			expectedDeleted: 1,
		},
		{
			name:            "more deletions than additions",
			original:        []string{"line1", "line2", "line3", "line4"},
			current:         []string{"line1", "newline2"},
			expectedAdded:   1,
			expectedDeleted: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, deleted := diffLines(tt.original, tt.current)
			require.Equal(t, tt.expectedAdded, added, "added lines mismatch")
			require.Equal(t, tt.expectedDeleted, deleted, "deleted lines mismatch")
		})
	}
}

func TestParseGitNumstat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         string
		expectedAdded int
		expectedDel   int
	}{
		{
			name: "mixed changes",
			input: "1\t1\tmain.go\n" +
				"34\t0\ttui.go\n",
			expectedAdded: 35,
			expectedDel:   1,
		},
		{
			name:          "binary files ignored",
			input:         "-\t-\timage.png\n",
			expectedAdded: 0,
			expectedDel:   0,
		},
		{
			name:          "unbalanced changes stay separate",
			input:         "5\t2\treport.md\n",
			expectedAdded: 5,
			expectedDel:   2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			added, deleted := parseGitNumstat([]byte(tt.input))
			require.Equal(t, tt.expectedAdded, added)
			require.Equal(t, tt.expectedDel, deleted)
		})
	}
}
func TestGetRepoInfoForRoot(t *testing.T) {
	// Use the current project directory as a real git repo
	root, err := os.Getwd()
	require.NoError(t, err)

	t.Setenv("ASIMI_SKIP_GIT_STATUS", "1")

	info := GetRepoInfoForRoot(root)
	require.NotEmpty(t, info.ProjectRoot, "ProjectRoot should be set")
	require.NotEmpty(t, info.Branch, "Branch should be detected")
	require.False(t, info.IsWorktree, "project root should not be a worktree")
	require.Contains(t, []string{"main", "master"}, info.Branch, "should detect a main branch")
	require.Equal(t, root, info.ProjectRoot, "ProjectRoot should match the provided root")
}

func TestGetRepoInfoForRoot_NonExistentDir(t *testing.T) {
	t.Setenv("ASIMI_SKIP_GIT_STATUS", "1")

	info := GetRepoInfoForRoot("/nonexistent/path/that/does/not/exist")
	// When .git doesn't exist, isWorktree=false, so projectRoot=root (not a git repo)
	require.Equal(t, "/nonexistent/path/that/does/not/exist", info.ProjectRoot)
	require.Empty(t, info.Branch, "Branch should be empty for nonexistent dir")
	require.False(t, info.IsWorktree)
	require.Empty(t, info.Slug, "Slug should be empty when no git remote")
}

func TestSanitizeSegment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"owner/repo slug", "owner/repo", "owner-repo"},
		{"multiple slashes", "a/b/c", "a-b-c"},
		{"empty string", "", ""},
		{"single char", "x", "x"},
		{"uppercase", "MyRepo", "myrepo"},
		{"leading slash", "/leading", "leading"},
		{"trailing slash", "trailing/", "trailing"},
		{"special chars", "hello@world!", "hello-world"},
		{"consecutive specials", "a///b", "a-b"},
		{"dots", "v1.2.3", "v1-2-3"},
		{"underscores", "my_repo", "my-repo"},
		{"spaces", "hello world", "hello-world"},
		{"numbers", "repo123", "repo123"},
		{"mixed", "Owner/Repo.v2", "owner-repo-v2"},
		{"only specials", "///", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeSegment(tt.input)
			require.Equal(t, tt.expected, result, "SanitizeSegment(%q)", tt.input)
		})
	}
}
