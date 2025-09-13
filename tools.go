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
	Path string `json:"path"`
}

// ReadFileTool is a tool for reading files
type ReadFileTool struct{}

func (t ReadFileTool) Name() string {
	return "read_file"
}

func (t ReadFileTool) Description() string {
	return "Reads a file and returns its content. The input should be a JSON object with a 'path' field."
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
	return string(content), nil
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

var availableTools = []tools.Tool{
	ReadFileTool{},
	WriteFileTool{},
	ListDirectoryTool{},
	ReplaceTextTool{},
	RunShellCommand{},
	ReadManyFilesTool{},
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
