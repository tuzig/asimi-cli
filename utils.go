package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
)

var claudeVersionPattern = regexp.MustCompile(`\d+(\.\d+)?`)

func getFileTree(root string) ([]string, error) {
	var files []string
	// Directories to ignore at any level
	ignoreDirs := map[string]bool{
		".git":    true,
		"vendor":  true,
		".asimi":  true,
		"archive": true,
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
func findProjectRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == "/" {
			return start
		}
		dir = parent
	}
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

const gitInfoRefreshInterval = 1 * time.Second

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
		return ""
	}

	if ref.Name().IsBranch() {
		return ref.Name().Short()
	}

	return ref.Hash().String()[:7]
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

// shortenProviderModel shortens provider and model names for display
func shortenProviderModel(provider, model string) string {
	// Shorten common provider names
	switch strings.ToLower(provider) {
	case "anthropic":
		provider = "Claude"
	case "openai":
		provider = "GPT"
	case "google", "googleai":
		provider = "Gemini"
	case "ollama":
		provider = "Ollama"
	}

	// Shorten common model names
	modelShort := model
	lowerModel := strings.ToLower(model)
	if strings.Contains(lowerModel, "claude") {
		normalized := strings.ReplaceAll(lowerModel, "-", ".")
		normalized = strings.ReplaceAll(normalized, "_", ".")
		normalized = strings.ReplaceAll(normalized, " ", ".")
		if match := claudeVersionPattern.FindString(normalized); match != "" {
			modelShort = match
		} else if strings.Contains(lowerModel, "instant") {
			modelShort = "Instant"
		}
	} else if strings.Contains(lowerModel, "gpt") {
		if strings.Contains(model, "4") {
			if strings.Contains(model, "turbo") {
				modelShort = "4T"
			} else {
				modelShort = "4"
			}
		} else if strings.Contains(model, "3.5") {
			modelShort = "3.5"
		}
	} else if strings.Contains(lowerModel, "gemini") {
		if strings.Contains(model, "pro") {
			modelShort = "Pro"
		} else if strings.Contains(model, "flash") {
			modelShort = "Flash"
		}
	}

	return fmt.Sprintf("%s-%s", provider, modelShort)
}

// getProviderStatusIcon returns an icon for the provider status
func getProviderStatusIcon(connected bool) string {
	if connected {
		return "✅"
	}
	return "🔌"
}
