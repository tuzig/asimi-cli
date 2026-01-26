// Package e2e provides end-to-end testing infrastructure for asimi commands
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestProject represents an isolated copy of a test project
type TestProject struct {
	T        *testing.T
	Dir      string // Temporary directory containing the project copy
	Original string // Original project path (e.g., tests/demo1)
}

// SetupTestProject creates an isolated copy of a demo project for testing.
// It copies the source directory to a temp location, initializes git, and
// creates an initial commit so hasUncommittedChanges() returns false.
//
// Usage:
//
//	project := e2e.SetupTestProject(t, "tests/demo1")
//	// project.Dir contains the isolated copy
//	// The cleanup is automatic via t.Cleanup()
func SetupTestProject(t *testing.T, sourceDir string) *TestProject {
	t.Helper()

	// Get absolute path of source directory
	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		t.Fatalf("Failed to get absolute path of source: %v", err)
	}

	// Verify source exists
	if _, err := os.Stat(absSource); os.IsNotExist(err) {
		t.Fatalf("Source directory does not exist: %s", absSource)
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "asimi-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Resolve symlinks to get the real path (important for macOS where /tmp -> /private/tmp)
	realTmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		// Fall back to the original path if symlink resolution fails
		realTmpDir = tmpDir
	}

	project := &TestProject{
		T:        t,
		Dir:      realTmpDir,
		Original: absSource,
	}

	// Register cleanup
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Warning: Failed to clean up temp directory %s: %v", tmpDir, err)
		}
	})

	// Copy the source directory to temp (excluding .git, node_modules, .next)
	if err := copyDir(absSource, tmpDir, []string{".git", "node_modules", ".next", "__pycache__", ".pytest_cache"}); err != nil {
		t.Fatalf("Failed to copy source directory: %v", err)
	}

	// Initialize git repository
	if err := initGitRepo(tmpDir); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	return project
}

// copyDir recursively copies a directory, excluding specified directories
func copyDir(src, dst string, exclude []string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		// Check if this entry should be excluded
		shouldExclude := false
		for _, ex := range exclude {
			if entry.Name() == ex {
				shouldExclude = true
				break
			}
		}
		if shouldExclude {
			continue
		}

		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			if err := copyDir(srcPath, dstPath, exclude); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// Preserve the file mode from source
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}

// cleanGitEnv returns a copy of os.Environ() with git hook variables removed.
// This is necessary when running git commands from within a git hook (e.g., pre-commit),
// where GIT_DIR, GIT_INDEX_FILE, etc. would interfere with creating a new repo.
func cleanGitEnv() []string {
	gitVars := map[string]bool{
		"GIT_DIR":                          true,
		"GIT_WORK_TREE":                    true,
		"GIT_INDEX_FILE":                   true,
		"GIT_OBJECT_DIRECTORY":             true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
		"GIT_QUARANTINE_PATH":              true,
	}

	var env []string
	for _, e := range os.Environ() {
		name := e
		if idx := strings.Index(e, "="); idx != -1 {
			name = e[:idx]
		}
		if !gitVars[name] {
			env = append(env, e)
		}
	}
	return env
}

// initGitRepo initializes a git repository and creates an initial commit
func initGitRepo(dir string) error {
	// Clean environment to remove git hook variables (GIT_DIR, etc.)
	// that would make git commands operate on the parent repo instead of dir.
	// This only affects subprocesses - the current process env is unchanged.
	cleanEnv := cleanGitEnv()

	// First, run git init separately so we can verify it worked
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	cmd.Env = cleanEnv
	if output, err := cmd.CombinedOutput(); err != nil {
		return &GitError{Command: []string{"git", "init"}, Output: string(output), Err: err}
	}

	// Verify .git directory was created (safety check)
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return &GitError{
			Command: []string{"git", "init"},
			Output:  "git init did not create .git directory",
			Err:     err,
		}
	}

	// Now run the rest of the git commands with clean env
	cmds := [][]string{
		{"git", "config", "--local", "user.email", "test@asimi.dev"},
		{"git", "config", "--local", "user.name", "Asimi Test"},
		{"git", "add", "-A"},
		{"git", "commit", "-m", "Initial commit for testing"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(cleanEnv, "GIT_AUTHOR_DATE=2024-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2024-01-01T00:00:00Z")
		if output, err := cmd.CombinedOutput(); err != nil {
			return &GitError{Command: args, Output: string(output), Err: err}
		}
	}

	return nil
}

// GitError represents an error from a git command
type GitError struct {
	Command []string
	Output  string
	Err     error
}

func (e *GitError) Error() string {
	return "git command failed: " + e.Err.Error() + "\nOutput: " + e.Output
}

// Chdir changes to the project directory and returns a cleanup function
// that restores the original directory
func (p *TestProject) Chdir() func() {
	p.T.Helper()

	originalDir, err := os.Getwd()
	if err != nil {
		p.T.Fatalf("Failed to get current directory: %v", err)
	}

	if err := os.Chdir(p.Dir); err != nil {
		p.T.Fatalf("Failed to change to project directory: %v", err)
	}

	return func() {
		if err := os.Chdir(originalDir); err != nil {
			p.T.Errorf("Failed to restore original directory: %v", err)
		}
	}
}

// FileExists checks if a file exists in the project directory
func (p *TestProject) FileExists(path string) bool {
	fullPath := filepath.Join(p.Dir, path)
	_, err := os.Stat(fullPath)
	return err == nil
}

// ReadFile reads a file from the project directory
func (p *TestProject) ReadFile(path string) (string, error) {
	fullPath := filepath.Join(p.Dir, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFile writes content to a file in the project directory
func (p *TestProject) WriteFile(path, content string) error {
	fullPath := filepath.Join(p.Dir, path)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, []byte(content), 0644)
}

// RemoveFile removes a file from the project directory
func (p *TestProject) RemoveFile(path string) error {
	fullPath := filepath.Join(p.Dir, path)
	return os.Remove(fullPath)
}

// CreateUncommittedChange creates an uncommitted change in the git repo
// (useful for testing that :init fails on dirty repos)
func (p *TestProject) CreateUncommittedChange() error {
	return p.WriteFile("uncommitted_test_file.txt", "test content")
}

// CommitAll stages and commits all changes
func (p *TestProject) CommitAll(message string) error {
	cleanEnv := cleanGitEnv()
	cmds := [][]string{
		{"git", "add", "-A"},
		{"git", "commit", "-m", message},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = p.Dir
		cmd.Env = cleanEnv
		if output, err := cmd.CombinedOutput(); err != nil {
			return &GitError{Command: args, Output: string(output), Err: err}
		}
	}

	return nil
}

// HasUncommittedChanges checks if there are uncommitted changes in the repo
func (p *TestProject) HasUncommittedChanges() bool {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = p.Dir
	cmd.Env = cleanGitEnv()
	output, err := cmd.Output()
	if err != nil {
		return true // Assume dirty on error
	}
	return len(output) > 0
}

// RemoveInfraFiles removes all asimi infrastructure files
func (p *TestProject) RemoveInfraFiles() error {
	files := []string{
		"AGENTS.md",
		"CLAUDE.md",
		"Justfile",
		".agents",
	}

	for _, file := range files {
		fullPath := filepath.Join(p.Dir, file)
		if err := os.RemoveAll(fullPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

// GetInfraFileStatus returns a map of infrastructure file paths to their existence status
func (p *TestProject) GetInfraFileStatus() map[string]bool {
	files := []string{
		"AGENTS.md",
		"CLAUDE.md",
		"Justfile",
		".agents/asimi.conf",
		".agents/sandbox/Dockerfile",
		".agents/sandbox/bashrc",
	}

	status := make(map[string]bool)
	for _, file := range files {
		status[file] = p.FileExists(file)
	}

	return status
}
