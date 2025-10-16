package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

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
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// getGitStatus returns a shortened git status string
func getGitStatus() string {
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return "" // Clean working directory
	}

	var status strings.Builder
	status.WriteString("[")

	// Count different types of changes
	var modified, added, deleted, untracked, renamed int
	for _, line := range lines {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'M', 'T': // Modified or type changed
			modified++
		case 'A': // Added
			added++
		case 'D': // Deleted
			deleted++
		case 'R': // Renamed
			renamed++
		case '?': // Untracked
			untracked++
		}
		switch line[1] {
		case 'M', 'T': // Modified or type changed in working tree
			modified++
		case 'D': // Deleted in working tree
			deleted++
		}
	}

	// Build status string
	if modified > 0 {
		status.WriteString("!")
	}
	if added > 0 {
		status.WriteString("+")
	}
	if deleted > 0 {
		status.WriteString("-")
	}
	if renamed > 0 {
		status.WriteString("→")
	}
	if untracked > 0 {
		status.WriteString("?")
	}

	status.WriteString("]")
	return status.String()
}

// isGitRepository checks if the current directory is a git repository
func isGitRepository() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	err := cmd.Run()
	return err == nil
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
	if strings.Contains(strings.ToLower(model), "claude") {
		if strings.Contains(model, "3.5") {
			modelShort = "3.5"
		} else if strings.Contains(model, "3-5") {
			modelShort = "3.5"
		} else if strings.Contains(model, "4") {
			modelShort = "4"
		} else if strings.Contains(model, "3") {
			modelShort = "3"
		}
	} else if strings.Contains(strings.ToLower(model), "gpt") {
		if strings.Contains(model, "4") {
			if strings.Contains(model, "turbo") {
				modelShort = "4T"
			} else {
				modelShort = "4"
			}
		} else if strings.Contains(model, "3.5") {
			modelShort = "3.5"
		}
	} else if strings.Contains(strings.ToLower(model), "gemini") {
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
