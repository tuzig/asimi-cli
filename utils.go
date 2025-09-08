package main

import (
	"os"
	"path/filepath"
	"sort"
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
