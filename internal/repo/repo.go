package repo

// RepoInfo contains information about the git repository and worktree.
// This type is shared between main and shogunate packages.
type RepoInfo struct {
	ProjectRoot  string
	WorktreePath string
	Branch       string
	IsWorktree   bool
	IsMain       bool
	Slug         string // Project slug (e.g., "owner/repo")
	LinesAdded   int    // Lines added in working directory
	LinesDeleted int    // Lines deleted in working directory
}
