package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
)

// RepoInfo contains information about the git repository and worktree
type RepoInfo struct {
	ProjectRoot  string
	WorktreePath string
	Branch       string
	IsWorktree   bool
	IsMain       bool
	Slug         string // Project slug (e.g., "owner/repo")
	status       string // Cached git status
	LinesAdded   int    // Lines added in working directory
	LinesDeleted int    // Lines deleted in working directory
	repo         *gogit.Repository
}

// isMainBranch checks if the given branch name is considered a main branch.
// Currently checks for "main" and "master".
func isMainBranch(branch string) bool {
	return branch == "main" || branch == "master"
}

// projectSlug returns the project slug (e.g., "owner/repo") from the git remote origin URL.
// Returns an empty string if the project root is not a git repository or has no remote.
func projectSlug(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}

	remoteURL, err := gitRemoteOriginURL(projectRoot)
	if err != nil || remoteURL == "" {
		return ""
	}

	owner, repo := parseGitRemote(remoteURL)
	if owner == "" || repo == "" {
		return ""
	}

	return owner + "-" + repo
}

// GetStatus returns a short git status string (e.g., "[!+]")
// This is cached at the time RepoInfo is created and does not update dynamically
func (r *RepoInfo) GetStatus() string {
	return r.status
}

// RefreshDiff recalculates diff statistics using gogit
func (r *RepoInfo) RefreshDiff() {
	if r.repo == nil {
		return
	}

	// TODO: add support for non-worktree branches
	worktree, err := r.repo.Worktree()
	if err != nil {
		return
	}

	repoPath := worktree.Filesystem.Root()
	if repoPath == "" {
		repoPath = r.ProjectRoot
	}
	if repoPath == "" {
		return
	}

	headExists := true
	if _, err := r.repo.Head(); err != nil {
		headExists = false
	}

	added := 0
	deleted := 0

	if headExists {
		added, deleted = collectDiffFromGit(repoPath, []string{"--numstat", "HEAD"})
	} else {
		slog.Debug("Getting diff for initial commit")
		a, d := collectDiffFromGit(repoPath, []string{"--numstat", "--cached"})
		added += a
		deleted += d

		a, d = collectDiffFromGit(repoPath, []string{"--numstat"})
		added += a
		deleted += d
	}

	r.LinesAdded = added
	r.LinesDeleted = deleted
	slog.Debug("Refreshed git diff", "+", added, "-", deleted)
}

func collectDiffFromGit(repoPath string, opts []string) (int, int) {

	args := []string{"diff"}
	args = append(args, opts...)
	output, err := runGitCommand(repoPath, args...)
	if err != nil {
		slog.Debug("git diff failed", "args", strings.Join(args, " "), "err", err, "output", strings.TrimSpace(string(output)))
		return 0, 0
	}

	return parseGitNumstat(output)
}

func runGitCommand(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

func parseGitNumstat(data []byte) (int, int) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	buf := make([]byte, 1024)
	scanner.Buffer(buf, 1024*1024)

	added := 0
	deleted := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		added += parseNumStatValue(parts[0])
		deleted += parseNumStatValue(parts[1])
	}

	if err := scanner.Err(); err != nil {
		slog.Debug("failed to parse git numstat output", "err", err)
	}

	return added, deleted
}

func parseNumStatValue(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "-" {
		return 0
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return val
}

// diffLines performs a simple line-based diff
func diffLines(original, current []string) (added int, deleted int) {
	// Create maps for quick lookup
	origMap := make(map[string]int)
	currMap := make(map[string]int)

	for _, line := range original {
		origMap[line]++
	}

	for _, line := range current {
		currMap[line]++
	}

	// Count added lines (in current but not in original)
	for line, count := range currMap {
		origCount := origMap[line]
		if count > origCount {
			added += count - origCount
		}
	}

	// Count deleted lines (in original but not in current)
	for line, count := range origMap {
		currCount := currMap[line]
		if count > currCount {
			deleted += count - currCount
		}
	}

	return added, deleted
}

// GetRepoInfo returns information about the current git repository and worktree
func GetRepoInfo() RepoInfo {

	cwd, err := os.Getwd()
	if err != nil {
		return RepoInfo{}
	}

	// Detect if we're in a worktree by checking if .git is a file vs directory
	gitPath := filepath.Join(cwd, ".git")
	info, err := os.Stat(gitPath)
	isWorktree := err == nil && !info.IsDir()

	// Find project root - for worktrees, find the main repo root
	var projectRoot string
	var worktreePath string

	if isWorktree {
		// Read .git file to find the main repository
		mainRepoRoot, err := findMainRepoRoot(cwd)
		if err == nil && mainRepoRoot != "" {
			projectRoot = mainRepoRoot
			// Calculate worktree path relative to main repo
			relPath, err := filepath.Rel(projectRoot, cwd)
			if err == nil && relPath != "." {
				worktreePath = relPath
			}
		} else {
			// Fallback to current directory if we can't find main repo
			projectRoot = cwd
		}
	} else {
		// Not a worktree, use standard project root finding
		projectRoot = findProjectRoot(cwd)
	}

	// Get current branch and status using go-git
	branch := ""
	status := ""
	repo, err := gogit.PlainOpenWithOptions(cwd, &gogit.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err == nil {
		ref, err := repo.Head()
		if err == nil {
			if ref.Name().IsBranch() {
				branch = ref.Name().Short()
			} else {
				branch = ref.Hash().String()[:7]
			}
		} else if isWorktree {
			// go-git doesn't fully support worktrees, try reading HEAD directly
			branch = readBranchFromWorktree()
		}
		// Get git status - only read once at startup
		// Skip in tests to avoid slow git operations
		if os.Getenv("ASIMI_SKIP_GIT_STATUS") == "" {
			status = readShortStatus(repo)
		}
	} else if isWorktree {
		// go-git failed, try reading branch directly from worktree
		branch = readBranchFromWorktree()
	}

	// Detect if branch is a main branch
	isMain := isMainBranch(branch)

	repoInfo := RepoInfo{
		ProjectRoot:  projectRoot,
		WorktreePath: worktreePath,
		Branch:       branch,
		IsWorktree:   isWorktree,
		IsMain:       isMain,
		Slug:         projectSlug(projectRoot),
		status:       status,
		repo:         repo,
	}

	// Calculate diff stats if we have a repo and not skipping git status
	if repo != nil && os.Getenv("ASIMI_SKIP_GIT_STATUS") == "" {
		// TODO: this should run in the background and update the status when done
		repoInfo.RefreshDiff()
	}

	return repoInfo
}

func getFileTree(root string) ([]string, error) {
	var files []string
	// Directories to ignore at any level
	ignoreDirs := map[string]bool{
		".git":      true,
		"vendor":    true,
		"worktrees": true,
		"archive":   true,
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if ignoreDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// We only want files.
		// Let's make sure the path is relative to the root.
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, relPath)
		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

// findProjectRoot returns the nearest ancestor directory (including start)
// that contains a project marker like .git or go.mod. Falls back to start.
// findMainRepoRoot finds the main repository root when in a worktree
// by reading the .git file and extracting the main repo path from the gitdir
func findMainRepoRoot(worktreeDir string) (string, error) {
	gitPath := filepath.Join(worktreeDir, ".git")

	// Read the .git file
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}

	// Parse gitdir: path
	// Example: gitdir: /Users/daonb/src/asimi-cli/.git/worktrees/GH33-fix-worktrees
	gitdirLine := strings.TrimSpace(string(content))
	if !strings.HasPrefix(gitdirLine, "gitdir: ") {
		return "", fmt.Errorf("invalid .git file format")
	}

	gitdir := strings.TrimPrefix(gitdirLine, "gitdir: ")

	// The gitdir points to: <main-repo>/.git/worktrees/<worktree-name>
	// We need to extract <main-repo> from this path
	// Split by "/.git/worktrees/" to get the main repo path
	parts := strings.Split(gitdir, "/.git/worktrees/")
	if len(parts) < 2 {
		return "", fmt.Errorf("unexpected gitdir format: %s", gitdir)
	}

	mainRepoRoot := parts[0]
	return mainRepoRoot, nil
}

// getCurrentGitBranch returns the current git branch name
func getCurrentGitBranch() string {
	return defaultGitInfoManager.CurrentBranch()
}

// getGitStatus returns a shortened git status string
func getGitStatus() string {
	return defaultGitInfoManager.ShortStatus()
}

// isGitRepository checks if the current directory is a git repository
func isGitRepository() bool {
	return defaultGitInfoManager.IsRepository()
}

// hasUncommittedChanges checks if there are uncommitted changes in the git repository
// Returns true if there are staged or unstaged changes (excluding untracked files)
func hasUncommittedChanges() bool {
	return defaultGitInfoManager.HasUncommittedChanges()
}

var defaultGitInfoManager = newGitInfoManager()

type gitInfoManager struct {
	mu         sync.RWMutex
	branch     string
	status     string
	repo       *gogit.Repository
	repoPath   string
	isRepo     bool
	lastUpdate time.Time
	updateCh   chan struct{}
	startOnce  sync.Once
}

func newGitInfoManager() *gitInfoManager {
	return &gitInfoManager{
		updateCh: make(chan struct{}, 1),
	}
}

func (m *gitInfoManager) start() {
	m.startOnce.Do(func() {
		m.refresh()
		go m.loop()
	})
}

func (m *gitInfoManager) loop() {
	for range m.updateCh {
		m.refresh()
	}
}

func (m *gitInfoManager) refresh() {
	branch, status, repo, repoPath, err := m.readRepositoryState()
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		m.branch = ""
		m.status = ""
		m.isRepo = false
		m.repo = nil
		m.repoPath = ""
		m.lastUpdate = now
		return
	}

	m.branch = branch
	m.status = status
	m.isRepo = true
	m.repo = repo
	m.repoPath = repoPath
	m.lastUpdate = now
}

func (m *gitInfoManager) readRepositoryState() (string, string, *gogit.Repository, string, error) {
	repo, repoPath, err := m.ensureRepository()
	if err != nil {
		return "", "", nil, "", err
	}

	branch := readCurrentBranch(repo)
	status := readShortStatus(repo)

	return branch, status, repo, repoPath, nil
}

func (m *gitInfoManager) ensureRepository() (*gogit.Repository, string, error) {
	// Get current working directory to find project root
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	root := findProjectRoot(cwd)

	m.mu.RLock()
	repo := m.repo
	repoPath := m.repoPath
	m.mu.RUnlock()

	if repo != nil && repoPath == root {
		return repo, repoPath, nil
	}

	repo, err = gogit.PlainOpenWithOptions(root, &gogit.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, "", err
	}

	m.mu.Lock()
	m.repo = repo
	m.repoPath = root
	m.mu.Unlock()

	return repo, root, nil
}

func (m *gitInfoManager) requestRefresh() {
	select {
	case m.updateCh <- struct{}{}:
	default:
	}
}

func (m *gitInfoManager) CurrentBranch() string {
	m.start()

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.branch
}

func (m *gitInfoManager) ShortStatus() string {
	m.start()

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *gitInfoManager) IsRepository() bool {
	m.start()

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isRepo
}

// HasUncommittedChanges checks if there are uncommitted changes (staged or unstaged)
// excluding untracked files
func (m *gitInfoManager) HasUncommittedChanges() bool {
	m.start()

	m.mu.RLock()
	repo := m.repo
	m.mu.RUnlock()

	if repo == nil {
		return false
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return false
	}

	status, err := worktree.Status()
	if err != nil {
		return false
	}

	// Check for any staged or unstaged changes (excluding untracked)
	for _, entry := range status {
		// Check staging area
		if entry.Staging != gogit.Unmodified && entry.Staging != gogit.Untracked {
			return true
		}
		// Check worktree (unstaged changes)
		if entry.Worktree != gogit.Unmodified && entry.Worktree != gogit.Untracked {
			return true
		}
	}

	return false
}

func refreshGitInfo() {
	defaultGitInfoManager.start()
	defaultGitInfoManager.requestRefresh()
}

func readCurrentBranch(repo *gogit.Repository) string {
	if repo == nil {
		return ""
	}

	ref, err := repo.Head()
	if err != nil {
		// go-git doesn't fully support worktrees, try reading HEAD directly
		branch := readBranchFromWorktree()
		if branch != "" {
			return branch
		}
		return ""
	}

	if ref.Name().IsBranch() {
		return ref.Name().Short()
	}

	return ref.Hash().String()[:7]
}

// readBranchFromWorktree reads the branch name directly from a git worktree
func readBranchFromWorktree() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	gitPath := filepath.Join(cwd, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}

	// If .git is a directory, not a worktree
	if info.IsDir() {
		return ""
	}

	// Read the .git file to get the actual git directory
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return ""
	}

	// Parse gitdir: path
	gitdirLine := strings.TrimSpace(string(content))
	if !strings.HasPrefix(gitdirLine, "gitdir: ") {
		return ""
	}

	gitdir := strings.TrimPrefix(gitdirLine, "gitdir: ")

	// Read HEAD from the worktree git directory
	headPath := filepath.Join(gitdir, "HEAD")
	headContent, err := os.ReadFile(headPath)
	if err != nil {
		return ""
	}

	// Parse ref: refs/heads/branch
	headLine := strings.TrimSpace(string(headContent))
	if strings.HasPrefix(headLine, "ref: ") {
		ref := strings.TrimPrefix(headLine, "ref: ")
		if strings.HasPrefix(ref, "refs/heads/") {
			return strings.TrimPrefix(ref, "refs/heads/")
		}
	}

	// If HEAD is detached, return the short hash
	if len(headLine) >= 7 {
		return headLine[:7]
	}

	return ""
}

func readShortStatus(repo *gogit.Repository) string {
	if repo == nil {
		return ""
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return ""
	}

	status, err := worktree.Status()
	if err != nil {
		return ""
	}

	return summarizeStatus(status)
}

func summarizeStatus(status gogit.Status) string {
	if len(status) == 0 {
		return ""
	}

	var modified, added, deleted, untracked, renamed int
	for _, entry := range status {
		switch entry.Staging {
		case gogit.Modified, gogit.UpdatedButUnmerged:
			modified++
		case gogit.Added, gogit.Copied:
			added++
		case gogit.Deleted:
			deleted++
		case gogit.Renamed:
			renamed++
		case gogit.Untracked:
			untracked++
		}
		switch entry.Worktree {
		case gogit.Modified, gogit.UpdatedButUnmerged:
			modified++
		case gogit.Added, gogit.Copied:
			added++
		case gogit.Deleted:
			deleted++
		case gogit.Renamed:
			renamed++
		case gogit.Untracked:
			untracked++
		}
	}

	var builder strings.Builder
	builder.WriteString("[")
	if modified > 0 {
		builder.WriteString("!")
	}
	if added > 0 {
		builder.WriteString("+")
	}
	if deleted > 0 {
		builder.WriteString("-")
	}
	if renamed > 0 {
		builder.WriteString("→")
	}
	if untracked > 0 {
		builder.WriteString("?")
	}
	builder.WriteString("]")

	result := builder.String()
	if result == "[]" {
		return ""
	}
	return result
}
