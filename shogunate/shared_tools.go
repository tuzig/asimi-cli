package shogunate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/yargevad/filepathx"
)

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

// ReadFileInput is the input for the ReadFileTool
type ReadFileInput struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// readFileInputRaw is used to handle string values for numeric fields (workaround for Claude Code CLI)
type readFileInputRaw struct {
	Path   string `json:"path"`
	Offset any    `json:"offset,omitempty"`
	Limit  any    `json:"limit,omitempty"`
}

// ReadFileTool is a tool for reading files
type ReadFileTool struct{}

func (t ReadFileTool) Name() string {
	return "read_file"
}

func (t ReadFileTool) Description() string {
	return "Reads a file and returns its content. The input should be a JSON object with a 'path' field. Optionally specify 'offset' (line number to start from, 1-based) and 'limit' (number of lines to read)."
}

func (t ReadFileTool) Call(ctx context.Context, input string) (string, error) {
	var params ReadFileInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		// If unmarshalling fails, assume the input is a raw path
		params.Path = input
	}

	// Workaround for Claude Code CLI bug: numeric params come as strings
	// If offset/limit are zero but the input contains them, try flexible parsing
	if (params.Offset == 0 && strings.Contains(input, `"offset"`)) ||
		(params.Limit == 0 && strings.Contains(input, `"limit"`)) {
		var rawParams readFileInputRaw
		if json.Unmarshal([]byte(input), &rawParams) == nil {
			params.Path = rawParams.Path
			if s, ok := rawParams.Offset.(string); ok && s != "" {
				fmt.Sscanf(s, "%d", &params.Offset)
			}
			if s, ok := rawParams.Limit.(string); ok && s != "" {
				fmt.Sscanf(s, "%d", &params.Limit)
			}
		}
	}

	// Clean up the path to remove any surrounding quotes
	params.Path = strings.Trim(params.Path, `"'`)

	if err := ValidatePathWithinProject(params.Path); err != nil {
		return "", err
	}

	content, err := os.ReadFile(params.Path)
	if err != nil {
		return "", err
	}

	contentStr := string(content)

	// If no offset or limit specified, return full content
	if params.Offset == 0 && params.Limit == 0 {
		return contentStr, nil
	}

	lines := strings.Split(contentStr, "\n")
	totalLines := len(lines)

	// Handle offset (1-based, convert to 0-based)
	startLine := 0
	if params.Offset > 0 {
		startLine = params.Offset - 1
		if startLine >= totalLines {
			return "", nil // Offset beyond file end
		}
	}

	// Handle limit
	endLine := totalLines
	if params.Limit > 0 {
		endLine = startLine + params.Limit
		if endLine > totalLines {
			endLine = totalLines
		}
	}

	selectedLines := lines[startLine:endLine]
	return strings.Join(selectedLines, "\n"), nil
}

func (t ReadFileTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file",
			},
		},
		"required": []string{"path"},
	}
}

// Format formats a read_file tool call for display
func (t ReadFileTool) Format(input, result string, err error) string {
	var params ReadFileInput
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("Read File")
	if params.Path != "" {
		msg.Writef(" %s", params.Path)
	}
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		lines := strings.Count(result, "\n") + 1
		if result == "" {
			lines = 0
		}
		msg.Writef("Read %d lines", lines)
	}

	return msg.String() + "\n"
}

// WriteFileInput is the input for the WriteFileTool
type WriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteFileTool is a tool for writing to files
type WriteFileTool struct{}

func (t WriteFileTool) Name() string {
	return "write_file"
}

func (t WriteFileTool) Description() string {
	return "Writes content to a file, creating or overwriting it. The input should be a JSON object with 'path' and 'content' fields. The path must be within the current working directory."
}

func (t WriteFileTool) Call(ctx context.Context, input string) (string, error) {
	var params WriteFileInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w. The input should be a JSON object with 'path' and 'content' fields", err)
	}

	// Clean up path and content
	params.Path = strings.Trim(params.Path, `"'`)
	params.Content = strings.Trim(params.Content, `"'`)

	// Validate that the path is within the project root
	if err := ValidatePathWithinProject(params.Path); err != nil {
		return "", err
	}

	// Create parent directory if it doesn't exist
	dir := filepath.Dir(params.Path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
	}

	err = os.WriteFile(params.Path, []byte(params.Content), 0644)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully wrote to %s", params.Path), nil
}

func (t WriteFileTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Target file path",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "File contents to write",
			},
		},
		"required": []string{"path", "content"},
	}
}

// Format formats a write_file tool call for display
func (t WriteFileTool) Format(input, result string, err error) string {
	var params WriteFileInput
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("Write File")
	if params.Path != "" {
		msg.Writef(" %s", params.Path)
	}
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		msg.WriteString("File written successfully")
	}

	return msg.String() + "\n"
}

// ListDirectoryInput is the input for the ListDirectoryTool
type ListDirectoryInput struct {
	Path string `json:"path"`
}

// ListDirectoryTool is a tool for listing directory contents
type ListDirectoryTool struct{}

func (t ListDirectoryTool) Name() string {
	return "list_files"
}

func (t ListDirectoryTool) Description() string {
	return "Lists the contents of a directory. The input should be a JSON object with a 'path' field."
}

func (t ListDirectoryTool) Call(ctx context.Context, input string) (string, error) {
	var params ListDirectoryInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		// If unmarshalling fails, assume the input is a raw path
		params.Path = input
	}

	// Clean up the path to remove any surrounding quotes
	params.Path = strings.Trim(params.Path, `"'`) // Corrected: escaped the double quote within the backticks

	// If the path is empty, use the current directory
	if params.Path == "" {
		params.Path = "."
	}

	if err := ValidatePathWithinProject(params.Path); err != nil {
		return "", err
	}

	files, err := os.ReadDir(params.Path)
	if err != nil {
		return "", err
	}

	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name())
	}
	return strings.Join(fileNames, "\n"), nil
}

func (t ListDirectoryTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path (defaults to '.')",
			},
		},
	}
}

// Format formats a list_files tool call for display
func (t ListDirectoryTool) Format(input, result string, err error) string {
	var params ListDirectoryInput
	json.Unmarshal([]byte(input), &params)

	path := params.Path
	if path == "" {
		path = "."
	}

	msg := utils.NewMsgBlockBuilder("List Files ")
	msg.WriteLn(path)

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		files := strings.Split(strings.TrimSpace(result), "\n")
		if result == "" {
			files = []string{}
		}
		msg.Writef("Found %d items", len(files))
	}

	return msg.String() + "\n"
}

// ReplaceTextInput is the input for the ReplaceTextTool
type ReplaceTextInput struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// ReplaceTextTool is a tool for replacing text in a file
type ReplaceTextTool struct{}

func (t ReplaceTextTool) Name() string {
	return "replace_text"
}

func (t ReplaceTextTool) Description() string {
	return "Replaces all occurrences of a string in a file with another string. The input should be a JSON object with 'path', 'old_text', and 'new_text' fields. The path must be within the current working directory."
}

func (t ReplaceTextTool) Call(ctx context.Context, input string) (string, error) {
	var params ReplaceTextInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w. The input should be a JSON object with 'path', 'old_text', and 'new_text' fields", err)
	}

	// Validate that the path is within the project root
	if err := ValidatePathWithinProject(params.Path); err != nil {
		return "", err
	}

	content, err := os.ReadFile(params.Path)
	if err != nil {
		return "", err
	}

	oldContent := string(content)

	// Check if old_string and new_string are identical
	if params.OldText == params.NewText {
		return fmt.Sprintf("No changes to apply. The old_string and new_string are identical in file: %s", params.Path), nil
	}

	newContent := strings.ReplaceAll(oldContent, params.OldText, params.NewText)

	// Count how many replacements were made
	occurrences := strings.Count(oldContent, params.OldText)

	if occurrences == 0 {
		return fmt.Sprintf("No occurrences of '%s' found in %s", params.OldText, params.Path), nil
	}

	err = os.WriteFile(params.Path, []byte(newContent), 0644)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Successfully modified file: %s (%d replacements)", params.Path, occurrences), nil
}

func (t ReplaceTextTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "Text to replace",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "Replacement text",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

// Format formats a replace_text tool call for display
func (t ReplaceTextTool) Format(input, result string, err error) string {
	var params ReplaceTextInput
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("Replace Text")
	if params.Path != "" {
		msg.Writef(" %s", params.Path)
	}
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else if strings.Contains(result, "No occurrences") {
		msg.WriteString("No matches found")
	} else if strings.Contains(result, "No changes") {
		msg.WriteString("No changes needed")
	} else {
		msg.WriteString("Text replaced successfully")
	}

	return msg.String()
}

// ReadManyFilesInput is the input for the ReadManyFilesTool.
type ReadManyFilesInput struct {
	Paths []string `json:"paths"`
}

// ReadManyFilesTool is a tool for reading multiple files using glob patterns.
type ReadManyFilesTool struct{}

func (t ReadManyFilesTool) Name() string {
	return "read_many_files"
}

func (t ReadManyFilesTool) Description() string {
	return "Reads content from multiple files specified by wildcard paths. The input should be a JSON object with a 'paths' field, which is an array of strings."
}

func (t ReadManyFilesTool) Call(ctx context.Context, input string) (string, error) {
	var params ReadManyFilesInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w. The input should be a JSON object with a 'paths' field", err)
	}

	var contentBuilder strings.Builder
	var allMatches []string

	for _, pattern := range params.Paths {
		matches, err := filepathx.Glob(pattern)
		if err != nil {
			// Silently ignore glob errors for now, or maybe log them.
			// For now, just continue.
			continue
		}
		allMatches = append(allMatches, matches...)
	}

	// Create a map to track unique matches
	uniqueMatchesMap := make(map[string]bool)
	var uniqueMatches []string
	for _, match := range allMatches {
		if !uniqueMatchesMap[match] {
			uniqueMatchesMap[match] = true
			uniqueMatches = append(uniqueMatches, match)
		}
	}

	for _, path := range uniqueMatches {
		if err := ValidatePathWithinProject(path); err != nil {
			// Skip files outside the project directory
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			// If we can't read a file, we can skip it and continue.
			continue
		}
		contentBuilder.WriteString(fmt.Sprintf("---\t%s---\n", path))
		contentBuilder.Write(content)
		contentBuilder.WriteString("\n")
	}

	return contentBuilder.String(), nil
}

func (t ReadManyFilesTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paths": map[string]any{
				"type":        "array",
				"description": "Array of file paths or glob patterns to read",
				"items": map[string]any{
					"type":        "string",
					"description": "A file path or glob pattern",
				},
			},
		},
		"required": []string{"paths"},
	}
}

// Format formats a read_many_files tool call for display
func (t ReadManyFilesTool) Format(input, result string, err error) string {
	var params ReadManyFilesInput
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("Read Many Files")
	if len(params.Paths) == 1 {
		msg.Writef("(%v)", params.Paths[0])
	} else if len(params.Paths) > 1 {
		msg.Writef("(%d files)", len(params.Paths))
	}
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		// Count files by counting "---\t" markers
		fileCount := strings.Count(result, "---\t")
		msg.Writef("Read %d files", fileCount)
	}

	return msg.String() + "\n"
}

// GrepInput is the input for the GrepTool
type GrepInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

// GrepTool searches for patterns in files
type GrepTool struct{}

func (t GrepTool) Name() string {
	return "grep"
}

func (t GrepTool) Description() string {
	return "Searches for a pattern in files. The input should be a JSON object with 'pattern' (regex) and optional 'path' (file or directory, defaults to '.'). Returns matching lines with file:line: prefix."
}

func (t GrepTool) Call(ctx context.Context, input string) (string, error) {
	var params GrepInput
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if params.Path == "" {
		params.Path = "."
	}

	if err := ValidatePathWithinProject(params.Path); err != nil {
		return "", err
	}

	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	var results []string
	maxResults := 100 // Limit results to avoid overwhelming output

	err = filepath.Walk(params.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}
		if info.IsDir() {
			// Skip hidden directories and common non-code directories
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip binary and hidden files
		if strings.HasPrefix(info.Name(), ".") || info.Size() > 1<<20 { // Skip files > 1MB
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip files we can't read
		}

		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d:%s", path, lineNum+1, line))
				if len(results) >= maxResults {
					return fmt.Errorf("max results reached")
				}
			}
		}
		return nil
	})

	if err != nil && err.Error() != "max results reached" {
		return "", err
	}

	if len(results) == 0 {
		return "No matches found", nil
	}

	output := strings.Join(results, "\n")
	if len(results) >= maxResults {
		output += fmt.Sprintf("\n... (truncated at %d results)", maxResults)
	}
	return output, nil
}

func (t GrepTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern to search for",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File or directory to search in (defaults to '.')",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t GrepTool) Format(input, result string, err error) string {
	var params GrepInput
	json.Unmarshal([]byte(input), &params)

	path := params.Path
	if path == "" {
		path = "."
	}

	msg := utils.NewMsgBlockBuilder("Grep")
	msg.Writef(" /%s/ in %s", params.Pattern, path)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else if result == "No matches found" {
		msg.WriteString("No matches found")
	} else {
		matches := strings.Count(result, "\n") + 1
		if strings.Contains(result, "truncated") {
			matches = 100
		}
		msg.Writef("Found %d matches", matches)
	}

	return msg.String() + "\n"
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

// RunShellCommand is a tool for running shell commands in a persistent shell.
// It uses the runners package for actual execution.
type RunShellCommand struct {
	runner          Runner
	hostRunner      Runner
	shouldRunOnHost func(cmd string) (runOnHost, needsApproval bool)
}

// NewRunShellCommand creates a new RunShellCommand tool
func NewRunShellCommand(
	runner Runner,
	hostRunner Runner,
	hostChecker func(string) (bool, bool),
) *RunShellCommand {
	return &RunShellCommand{
		runner:          runner,
		hostRunner:      hostRunner,
		shouldRunOnHost: hostChecker,
	}
}

func (t *RunShellCommand) Name() string {
	return "run_shell_command"
}

func (t *RunShellCommand) Description() string {
	return "Executes a shell command in a persistent shell session inside a container. The project root is mounted at `/workspace`, and when in a worktree, the shell automatically navigates to the worktree directory. Current working directory is maintained between commands. The input should be a JSON object with 'command' and optional 'description' fields.\n\nIMPORTANT: Each command runs in an isolated subshell for stability and predictability. This means:\n- Environment variables set with 'export' do NOT persist between commands\n- Directory changes with 'cd' do NOT persist between commands\n- Each command starts fresh in the project/worktree root directory\n- To perform multi-step operations, combine them in a single command using && or ; (e.g., 'cd dir && make && cd ..')\n- Redirects and heredocs work correctly within each command"
}

func (t *RunShellCommand) Call(ctx context.Context, input string) (string, error) {
	var params RunShellCommandInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	var output RunShellCommandOutput
	var runErr error

	runnerInput := RunnerInput{
		Command:        params.Command,
		Description:    params.Description,
		BypassApproval: params.BypassApproval,
	}

	// Check if command should run on host based on config patterns
	runOnHost, requiresApproval := false, true
	if t.shouldRunOnHost != nil {
		runOnHost, requiresApproval = t.shouldRunOnHost(params.Command)
	}

	if runOnHost && t.hostRunner != nil {
		// Set the approval flag based on config patterns
		runnerInput.BypassApproval = !requiresApproval

		// Run directly on host
		runnerOutput, err := t.hostRunner.Run(ctx, runnerInput)
		output.Output = runnerOutput.Output
		output.ExitCode = runnerOutput.ExitCode
		runErr = err
	} else if t.runner != nil {
		runnerOutput, err := t.runner.Run(ctx, runnerInput)
		output.Output = runnerOutput.Output
		output.ExitCode = runnerOutput.ExitCode
		runErr = err

		// If we got a harness error, try to restart and retry once
		if runErr != nil {
			if restartErr := t.runner.Restart(ctx); restartErr != nil {
				return "", fmt.Errorf("command failed and restart failed: %w (restart error: %v)", runErr, restartErr)
			}

			runnerOutput, err = t.runner.Run(ctx, runnerInput)
			output.Output = runnerOutput.Output
			output.ExitCode = runnerOutput.ExitCode
			runErr = err
		}
	} else {
		return "", fmt.Errorf("no runner configured")
	}

	if runErr != nil {
		return "", runErr
	}

	outputBytes, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	return string(outputBytes), nil
}

func (t *RunShellCommand) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to run",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Why we run this command, will be displayed to the user",
			},
		},
		"required": []string{"command"},
	}
}

// Format formats a run_shell_command tool call for display
func (t *RunShellCommand) Format(input, result string, err error) string {
	var params RunShellCommandInput
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("")
	msg.WriteLn(params.Description)
	msg.Writef("$ %s", params.Command)

	if err != nil {
		msg.WriteLn()
		msg.Writef("ERROR: %v", err)
	} else if result != "" {
		var output map[string]interface{}
		if json.Unmarshal([]byte(result), &output) == nil {
			if ec, ok := output["exitCode"].(string); ok && ec != "0" {
				msg.WriteLn()
				msg.WriteString(ec)
			}
		} else {
			msg.WriteLn()
			msg.Writef("ERROR: %v", err)
		}
	}

	return msg.String() + "\n"
}

// RunShellCommandInput is the input for the RunShellCommand tool
type RunShellCommandInput struct {
	Command        string `json:"command"`
	Description    string `json:"description"`
	BypassApproval bool   `json:"-"`
}

// RunShellCommandOutput is the output of the RunShellCommand tool
type RunShellCommandOutput struct {
	Output   string `json:"stdout"`
	ExitCode string `json:"exitCode"`
}

// Runner is the interface for shell command execution backends
type Runner interface {
	Run(ctx context.Context, input RunnerInput) (RunnerOutput, error)
	Restart(ctx context.Context) error
	Close(ctx context.Context) error
	RunnerType() string
}

// RunnerInput is the input for runner execution
type RunnerInput struct {
	Command        string
	Description    string
	BypassApproval bool
}

// RunnerOutput is the output from runner execution
type RunnerOutput struct {
	Output   string
	ExitCode string
}
