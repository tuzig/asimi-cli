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
