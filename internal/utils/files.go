package utils

import (
	"os"
	"path/filepath"
	"sort"
)

func GetFileTree(root string) ([]string, error) {
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
