package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Tool defines a tool that can be invoked by ministers
type Tool interface {
	Name() string
	Description() string
	Call(ctx context.Context, input string) (string, error)
	Format(input, result string, err error) string
	ParameterSchema() map[string]any
}

// ValidatePathWithinProject checks if a file path is within the current working directory.
// It prevents path traversal attacks and ensures files are only modified within the current directory tree.
func ValidatePathWithinProject(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Get the current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Convert both paths to absolute paths
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("failed to resolve current directory: %w", err)
	}

	// Clean the paths to resolve any .. or . components
	absPath = filepath.Clean(absPath)
	absCwd = filepath.Clean(absCwd)

	// Evaluate symlinks to get the real path
	// This prevents writing through symlinks to locations outside the current directory
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If the file doesn't exist yet, check the parent directory
		parentDir := filepath.Dir(absPath)
		realParentPath, evalErr := filepath.EvalSymlinks(parentDir)
		if evalErr != nil {
			// Parent doesn't exist either, use the cleaned absolute path
			realPath = absPath
		} else {
			// Reconstruct the path with the real parent
			realPath = filepath.Join(realParentPath, filepath.Base(absPath))
		}
	}

	realCwd, err := filepath.EvalSymlinks(absCwd)
	if err != nil {
		// If cwd symlink evaluation fails, use the cleaned path
		realCwd = absCwd
	}

	// Check if the file path is within the current working directory
	relPath, err := filepath.Rel(realCwd, realPath)
	if err != nil {
		return fmt.Errorf("failed to determine relative path: %w", err)
	}

	// If the relative path starts with "..", it's outside the current directory
	if strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("access denied: path '%s' is outside the current working directory '%s'", path, cwd)
	}

	return nil
}

// GetFileTools returns the list of file-based tools for use by ministers.
func GetFileTools() []Tool {
	return []Tool{
		ReadFileTool{},
		WriteFileTool{},
		ListDirectoryTool{},
		ReplaceTextTool{},
		ReadManyFilesTool{},
		GrepTool{},
	}
}

// GetROTools returns read-only tools for exploration and research.
func GetROTools() []Tool {
	return []Tool{
		ListDirectoryTool{},
		ReadFileTool{},
		ReadManyFilesTool{},
		GrepTool{},
	}
}

// GetEditTools returns read/write tools for code editing (no shell).
func GetEditTools() []Tool {
	return []Tool{
		ReadFileTool{},
		WriteFileTool{},
		ReplaceTextTool{},
		ListDirectoryTool{},
		ReadManyFilesTool{},
		GrepTool{},
	}
}

