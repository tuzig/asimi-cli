package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	internalrepo "github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/shogunate"
	gogit "github.com/go-git/go-git/v5"
)

// RepoInfo contains information about the git repository and worktree.
// It embeds the shared internal config type and adds private fields for git operations.
type RepoInfo struct {
	internalrepo.RepoInfo
	status string            // Cached git status
	repo   *gogit.Repository // Git repository handle for operations
}

// MakeRepoInfo creates a RepoInfo with the given fields.
// This is a convenience function for creating RepoInfo structs,
// especially useful in tests where the embedded struct syntax would be verbose.
func MakeRepoInfo(projectRoot, branch string) RepoInfo {
	return RepoInfo{
		RepoInfo: internalrepo.RepoInfo{
			ProjectRoot: projectRoot,
			Branch:      branch,
		},
	}
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
		RepoInfo: internalrepo.RepoInfo{
			ProjectRoot:  projectRoot,
			WorktreePath: worktreePath,
			Branch:       branch,
			IsWorktree:   isWorktree,
			IsMain:       isMain,
			Slug:         projectSlug(projectRoot),
		},
		status: status,
		repo:   repo,
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
			// Skip files/directories we can't access
			return nil
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

// TruncateMiddle truncates a string by keeping the beginning and end portions
// and replacing the middle with an ellipsis. The maxWidth parameter specifies
// the maximum total width of the result. The beginning portion takes 1/3 of
// the available space, and the end portion takes the remaining 2/3.
// If the message fits within maxWidth, it is returned unchanged.
func TruncateMiddle(message string, maxWidth int) string {
	// Handle edge cases
	if maxWidth <= 0 {
		return ""
	}

	// Count runes for proper Unicode handling
	runes := []rune(message)
	msgLen := len(runes)

	// If message fits, return as-is
	if msgLen <= maxWidth {
		return message
	}

	// Need at least 4 characters for truncation to make sense (1 + ellipsis + 1 + 1)
	if maxWidth < 4 {
		return string(runes[:maxWidth])
	}

	// Calculate portions: beginning gets 1/3, end gets 2/3 (minus ellipsis)
	ellipsisLen := 1 // Single rune
	availableWidth := maxWidth - ellipsisLen

	beginLen := availableWidth / 3
	endLen := availableWidth - beginLen

	// Ensure we have at least 1 character on each side
	if beginLen < 1 {
		beginLen = 1
		endLen = availableWidth - 1
	}
	if endLen < 1 {
		endLen = 1
		beginLen = availableWidth - 1
	}

	// Build truncated string
	beginning := string(runes[:beginLen])
	end := string(runes[msgLen-endLen:])

	return beginning + "…" + end
}

// containerLaunchMsg is sent when the container is launching.
type containerLaunchMsg struct{ message string }

// generateSessionID creates a unique session ID with timestamp prefix.
func generateSessionID() string {
	timestamp := time.Now().Format("2006-01-02-150405")
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	suffix := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("%s-%s", timestamp, suffix)
}

// findProjectRoot walks up from start until it finds a .git directory.
func findProjectRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == "/" || parent == dir {
			return start
		}
		dir = parent
	}
}

const (
	contextBarWidth              = 10
	contextCategorySymbol        = "⛁"
	contextCategoryPartialSymbol = "⛀"
	contextFreeSymbol            = "⛶"
)

// Git and slug utilities

func branchSlugOrDefault(branch string) string {
	slug := sanitizeSegment(branch)
	if slug == "" {
		return "main"
	}
	return slug
}

func sanitizeSegment(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	prevHyphen := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func gitRemoteOriginURL(workingDir string) (string, error) {
	cmd := exec.Command("git", "-C", workingDir, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func parseGitRemote(remote string) (owner, repo string) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	if remote == "" {
		return "", ""
	}

	if strings.Contains(remote, "://") {
		if u, err := url.Parse(remote); err == nil {
			segments := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(segments) >= 2 {
				owner = segments[len(segments)-2]
				repo = segments[len(segments)-1]
			}
			return owner, repo
		}
	}

	if strings.Contains(remote, ":") {
		parts := strings.SplitN(remote, ":", 2)
		if len(parts) == 2 {
			path := strings.Trim(parts[1], "/")
			segments := strings.Split(path, "/")
			if len(segments) >= 2 {
				owner = segments[len(segments)-2]
				repo = segments[len(segments)-1]
			}
		}
	}

	return owner, repo
}

// Context display utilities

func renderContextInfo(info shogunate.ContextInfo) string {
	var b strings.Builder
	total := info.TotalTokens
	if total <= 0 {
		total = info.UsedTokens + info.AutocompactBuffer + info.FreeTokens
	}
	if total <= 0 {
		total = 1
	}

	usedPercent := percentage(clampInt(info.UsedTokens, 0, total), total)
	systemPromptPercent := percentage(info.SystemPromptTokens, total)
	systemToolsPercent := percentage(info.SystemToolsTokens, total)
	memoryFilesPercent := percentage(info.MemoryFilesTokens, total)
	messagesPercent := percentage(info.MessagesTokens, total)

	b.WriteString("  ⎿  Context Usage\n")
	b.WriteString(fmt.Sprintf("     %s   %s · %s/%s tokens (%.1f%%)\n",
		renderContextBar(info),
		info.Model,
		formatTokenCount(info.UsedTokens),
		formatTokenCount(info.TotalTokens),
		usedPercent,
	))

	b.WriteString(formatContextLine("System prompt", info.SystemPromptTokens, total, systemPromptPercent))
	b.WriteString(formatContextLine("System tools", info.SystemToolsTokens, total, systemToolsPercent))
	b.WriteString(formatContextLine("Memory files", info.MemoryFilesTokens, total, memoryFilesPercent))
	b.WriteString(formatContextLine("Messages", info.MessagesTokens, total, messagesPercent))
	b.WriteString(formatFreeSpaceLine(info, total))

	return b.String()
}

func renderContextBar(info shogunate.ContextInfo) string {
	total := info.TotalTokens
	if total <= 0 {
		total = info.UsedTokens + info.AutocompactBuffer + info.FreeTokens
	}
	if total <= 0 {
		total = 1
	}

	usedTokens := clampInt(info.UsedTokens, 0, total)
	bufferTokens := clampInt(info.AutocompactBuffer, 0, total-usedTokens)
	freeTokens := total - usedTokens - bufferTokens
	if freeTokens < 0 {
		freeTokens = 0
	}

	segments := make([]string, 0, contextBarWidth)
	remaining := contextBarWidth

	addSegments := func(tokens int, fill, partial string) {
		if remaining == 0 || tokens <= 0 {
			return
		}
		percentage := float64(tokens) / float64(total) * 100
		fullSegments, partialSegment := calculateBarSegments(percentage)
		if fullSegments > remaining {
			fullSegments = remaining
			partialSegment = false
		}
		for i := 0; i < fullSegments && remaining > 0; i++ {
			segments = append(segments, fill)
			remaining--
		}
		if partialSegment && remaining > 0 {
			if partial == "" {
				partial = fill
			}
			segments = append(segments, partial)
			remaining--
		}
	}

	addSegments(usedTokens, "⛁", "⛀")
	addSegments(freeTokens, "⛶", "")
	addSegments(bufferTokens, "⛝", "")

	for len(segments) < contextBarWidth {
		segments = append(segments, "⛶")
	}

	return strings.Join(segments, " ")
}

func formatContextLine(label string, tokens, total int, percent float64) string {
	bar := renderCategoryBar(tokens, total)
	return fmt.Sprintf("     %s   %s %s: %s tokens (%.1f%%)\n",
		bar,
		contextCategorySymbol,
		label,
		formatTokenCount(tokens),
		percent,
	)
}

func formatFreeSpaceLine(info shogunate.ContextInfo, total int) string {
	bar := renderFreeSpaceBar(info, total)
	totalFreeSpace := info.FreeTokens + info.AutocompactBuffer
	return fmt.Sprintf("     %s   ⛶ Free space: %s tokens (%.1f%%)\n",
		bar,
		formatTokenCount(totalFreeSpace),
		percentage(totalFreeSpace, total),
	)
}

func renderCategoryBar(tokens, total int) string {
	percentage := 0.0
	if total > 0 {
		percentage = float64(tokens) / float64(total) * 100
	}
	fullSegments, partialSegment := calculateBarSegments(percentage)

	segments := make([]string, 0, contextBarWidth)
	for i := 0; i < fullSegments && len(segments) < contextBarWidth; i++ {
		segments = append(segments, contextCategorySymbol)
	}
	if partialSegment && len(segments) < contextBarWidth {
		segments = append(segments, contextCategoryPartialSymbol)
	}
	for len(segments) < contextBarWidth {
		segments = append(segments, contextFreeSymbol)
	}
	return strings.Join(segments, " ")
}

func renderFreeSpaceBar(info shogunate.ContextInfo, total int) string {
	freePercentage := 0.0
	bufferPercentage := 0.0
	if total > 0 {
		freePercentage = float64(info.FreeTokens) / float64(total) * 100
		bufferPercentage = float64(info.AutocompactBuffer) / float64(total) * 100
	}

	freeSegments, freePartial := calculateBarSegments(freePercentage)
	bufferSegments, _ := calculateBarSegments(bufferPercentage)

	segments := make([]string, 0, contextBarWidth)

	for i := 0; i < freeSegments && len(segments) < contextBarWidth; i++ {
		segments = append(segments, "⛶")
	}
	if freePartial && len(segments) < contextBarWidth {
		segments = append(segments, "⛶")
	}

	if len(segments) < contextBarWidth {
		segments = append(segments, "↓")
	}

	for i := 0; i < bufferSegments && len(segments) < contextBarWidth; i++ {
		segments = append(segments, "⛶")
	}

	for len(segments) < contextBarWidth {
		segments = append(segments, "⛶")
	}

	return strings.Join(segments, " ")
}

func calculateBarSegments(percentage float64) (int, bool) {
	if percentage <= 0 {
		return 0, false
	}
	fullSegments := int(percentage / 10)
	if fullSegments >= contextBarWidth {
		return contextBarWidth, false
	}
	remainder := percentage - float64(fullSegments*10)
	return fullSegments, remainder > 0
}

func formatTokenCount(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func percentage(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round((float64(part)/float64(total))*1000) / 10
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
