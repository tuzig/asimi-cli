package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"github.com/tmc/langchaingo/tools"
	"github.com/yargevad/filepathx"
)

// ReadFileInput is the input for the ReadFileTool
type ReadFileInput struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
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

	// Clean up the path to remove any surrounding quotes
	params.Path = strings.Trim(params.Path, `"'`)

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

// String formats a read_file tool call for display
func (t ReadFileTool) Format(input, result string, err error) string {
	// Parse input JSON to extract path
	var params ReadFileInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if params.Path != "" {
		paramStr = fmt.Sprintf("(%s)", params.Path)
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("Read File%s", paramStr)

	// Second line: result summary
	var secondLine string
	if err != nil {
		secondLine = fmt.Sprintf("  ⎿  Error: %v", err)
	} else {
		lines := strings.Count(result, "\n") + 1
		if result == "" {
			lines = 0
		}
		secondLine = fmt.Sprintf("  ⎿  Read %d lines", lines)
	}

	return firstLine + "\n" + secondLine
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
	return "Writes content to a file. The input should be a JSON object with 'path' and 'content' fields."
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

	err = os.WriteFile(params.Path, []byte(params.Content), 0644)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully wrote to %s", params.Path), nil
}

// String formats a write_file tool call for display
func (t WriteFileTool) Format(input, result string, err error) string {
	// Parse input JSON to extract path
	var params WriteFileInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if params.Path != "" {
		paramStr = fmt.Sprintf("(%s)", params.Path)
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("Write File%s", paramStr)

	// Second line: result summary
	var secondLine string
	if err != nil {
		secondLine = fmt.Sprintf("  ⎿  Error: %v", err)
	} else {
		secondLine = "  ⎿  File written successfully"
	}

	return firstLine + "\n" + secondLine
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

// String formats a list_files tool call for display
func (t ListDirectoryTool) Format(input, result string, err error) string {
	// Parse input JSON to extract path
	var params ListDirectoryInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if params.Path != "" {
		paramStr = fmt.Sprintf("(%s)", params.Path)
	} else {
		paramStr = "(.)"
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("List Files%s", paramStr)

	// Second line: result summary
	var secondLine string
	if err != nil {
		secondLine = fmt.Sprintf("  ⎿  Error: %v", err)
	} else {
		files := strings.Split(strings.TrimSpace(result), "\n")
		if result == "" {
			files = []string{}
		}
		secondLine = fmt.Sprintf("  ⎿  Found %d items", len(files))
	}

	return firstLine + "\n" + secondLine
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
	return "Replaces all occurrences of a string in a file with another string. The input should be a JSON object with 'path', 'old_text', and 'new_text' fields."
}

func (t ReplaceTextTool) Call(ctx context.Context, input string) (string, error) {
	var params ReplaceTextInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w. The input should be a JSON object with 'path', 'old_text', and 'new_text' fields", err)
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

// String formats a replace_text tool call for display
func (t ReplaceTextTool) Format(input, result string, err error) string {
	// Parse input JSON to extract path
	var params ReplaceTextInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if params.Path != "" {
		paramStr = fmt.Sprintf("(%s)", params.Path)
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("Replace Text%s", paramStr)

	// Second line: result summary
	var secondLine string
	if err != nil {
		secondLine = fmt.Sprintf("  ⎿  Error: %v", err)
	} else {
		if strings.Contains(result, "No occurrences") {
			secondLine = "  ⎿  No matches found"
		} else if strings.Contains(result, "No changes") {
			secondLine = "  ⎿  No changes needed"
		} else {
			secondLine = "  ⎿  Text replaced successfully"
		}
	}

	return firstLine + "\n" + secondLine
}

// RunShellCommand is a tool for running shell commands
type RunShellCommand struct{}

// RunShellCommandInput is the input for the RunShellCommand tool
type RunShellCommandInput struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

// RunShellCommandOutput is the output of the RunShellCommand tool
type RunShellCommandOutput struct {
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	ExitCode       int    `json:"exitCode"`
	Signal         int    `json:"signal,omitempty"`
	Error          string `json:"error,omitempty"`
	PID            int    `json:"pid,omitempty"`
	BackgroundPids []int  `json:"backgroundPids,omitempty"`
}

func (t RunShellCommand) Name() string {
	return "run_shell_command"
}

func (t RunShellCommand) Description() string {
	return "Executes a shell command. The input should be a JSON object with 'command', 'description', and 'directory' fields."
}

func (t RunShellCommand) Call(ctx context.Context, input string) (string, error) {
	var params RunShellCommandInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	output := RunShellCommandOutput{}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", params.Command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", params.Command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = params.Path

	runErr := cmd.Run()

	output.Stdout = stdout.String()
	output.Stderr = stderr.String()

	if cmd.Process != nil {
		output.PID = cmd.Process.Pid
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			output.ExitCode = exitErr.ExitCode()
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					output.Signal = int(status.Signal())
				}
			}
		} else {
			output.Error = runErr.Error()
			output.ExitCode = -1
		}
	} else {
		if cmd.ProcessState != nil {
			output.ExitCode = cmd.ProcessState.ExitCode()
		} else {
			output.ExitCode = 0
		}
	}

	outputBytes, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	return string(outputBytes), nil
}

// String formats a run_shell_command tool call for display
func (t RunShellCommand) Format(input, result string, err error) string {
	// Parse input JSON to extract command
	var params RunShellCommandInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if params.Command != "" {
		cmd := params.Command
		// Truncate long commands
		if len(cmd) > 50 {
			cmd = cmd[:47] + "..."
		}
		paramStr = fmt.Sprintf("(%s)", cmd)
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("Run Shell Command%s", paramStr)

	// Second line: result summary
	var secondLine string
	if err != nil {
		secondLine = fmt.Sprintf("  ⎿  Error: %v", err)
	} else {
		// Parse JSON output to get exit code
		var output map[string]interface{}
		if json.Unmarshal([]byte(result), &output) == nil {
			if exitCode, ok := output["exitCode"].(float64); ok {
				if exitCode == 0 {
					secondLine = "  ⎿  Command completed successfully"
				} else {
					secondLine = fmt.Sprintf("  ⎿  Command failed (exit code %d)", int(exitCode))
				}
			} else {
				secondLine = "  ⎿  Command executed"
			}
		} else {
			secondLine = "  ⎿  Command executed"
		}
	}

	return firstLine + "\n" + secondLine
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

// String formats a read_many_files tool call for display
func (t ReadManyFilesTool) Format(input, result string, err error) string {
	// Parse input JSON to extract paths
	var params ReadManyFilesInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if len(params.Paths) == 1 {
		paramStr = fmt.Sprintf("(%v)", params.Paths[0])
	} else if len(params.Paths) > 1 {
		paramStr = fmt.Sprintf("(%d files)", len(params.Paths))
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("Read Many Files%s", paramStr)

	// Second line: result summary
	var secondLine string
	if err != nil {
		secondLine = fmt.Sprintf("  ⎿  Error: %v", err)
	} else {
		// Count files by counting "---\t" markers
		fileCount := strings.Count(result, "---\t")
		secondLine = fmt.Sprintf("  ⎿  Read %d files", fileCount)
	}

	return firstLine + "\n" + secondLine
}

type Tool interface {
	tools.Tool
	Format(input, result string, err error) string
}

var availableTools = []Tool{
	ReadFileTool{},
	WriteFileTool{},
	ListDirectoryTool{},
	ReplaceTextTool{},
	RunShellCommand{},
	ReadManyFilesTool{},
}
