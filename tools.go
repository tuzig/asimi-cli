package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tmc/langchaingo/tools"
	"github.com/yargevad/filepathx"
)

// validatePathWithinProject checks if a file path is within the current working directory.
// It prevents path traversal attacks and ensures files are only modified within the current directory tree.
func validatePathWithinProject(path string) error {
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

	if err := validatePathWithinProject(params.Path); err != nil {
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

// String formats a read_file tool call for display
func (t ReadFileTool) Format(input, result string, err error) string {
	// Parse input JSON to extract path
	var params ReadFileInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if params.Path != "" {
		paramStr = fmt.Sprintf(" %s", params.Path)
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("Read File%s", paramStr)

	// Second line: result summary
	var secondLine string
	if err != nil {
		secondLine = fmt.Sprintf("%sError: %v", treeFinalPrefix, err)
	} else {
		lines := strings.Count(result, "\n") + 1
		if result == "" {
			lines = 0
		}
		secondLine = fmt.Sprintf("%sRead %d lines", treeFinalPrefix, lines)
	}

	return firstLine + "\n" + secondLine + "\n"
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
	if err := validatePathWithinProject(params.Path); err != nil {
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

// String formats a write_file tool call for display
func (t WriteFileTool) Format(input, result string, err error) string {
	// Parse input JSON to extract path
	var params WriteFileInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if params.Path != "" {
		paramStr = fmt.Sprintf(" %s", params.Path)
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("Write File%s", paramStr)

	// Second line: result summary
	var secondLine string
	if err != nil {
		secondLine = fmt.Sprintf("%sError: %v", treeFinalPrefix, err)
	} else {
		secondLine = fmt.Sprintf("%sFile written successfully", treeFinalPrefix)
	}

	return firstLine + "\n" + secondLine + "\n"
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

	if err := validatePathWithinProject(params.Path); err != nil {
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

// String formats a list_files tool call for display
func (t ListDirectoryTool) Format(input, result string, err error) string {
	// Parse input JSON to extract path
	var params ListDirectoryInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if params.Path != "" {
		paramStr = params.Path
	} else {
		paramStr = "."
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("List Files %s", paramStr)

	// Second line: result summary
	secondLine := treeFinalPrefix
	if err != nil {
		secondLine += fmt.Sprintf("Error: %v", err)
	} else {
		files := strings.Split(strings.TrimSpace(result), "\n")
		if result == "" {
			files = []string{}
		}
		secondLine += fmt.Sprintf("Found %d items", len(files))
	}
	return firstLine + "\n" + secondLine + "\n"
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
	if err := validatePathWithinProject(params.Path); err != nil {
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

// String formats a replace_text tool call for display
func (t ReplaceTextTool) Format(input, result string, err error) string {
	// Parse input JSON to extract path
	var params ReplaceTextInput
	json.Unmarshal([]byte(input), &params)

	paramStr := ""
	if params.Path != "" {
		paramStr = fmt.Sprintf(" %s", params.Path)
	}

	// First line: tool name and parameters
	firstLine := fmt.Sprintf("Replace Text %s", paramStr)

	// Second line: result summary
	secondLine := treeFinalPrefix
	if err != nil {
		secondLine += fmt.Sprintf("Error: %v", err)
	} else {
		if strings.Contains(result, "No occurrences") {
			secondLine += "No matches found"
		} else if strings.Contains(result, "No changes") {
			secondLine += "No changes needed"
		} else {
			secondLine += "Text replaced successfully"
		}
	}

	return firstLine + "\n" + secondLine
}

// RunInShell is a tool for running shell commands in a persistent shell
type RunInShell struct {
	config *Config
}

// RunInShellInput is the input for the RunInShell tool
type RunInShellInput struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	// RequestApproval is an internal field (not included in JSON schema) that indicates
	// whether this command requires user approval before execution on the host.
	// This is set by the tool based on config patterns, not by the LLM.
	RequestApproval bool `json:"-"`
}

// RunInShellOutput is the output of the RunInShell tool
type RunInShellOutput struct {
	Output   string `json:"stdout"`
	ExitCode string `json:"exitCode"`
}

type shellRunner interface {
	Run(context.Context, RunInShellInput) (RunInShellOutput, error)
	Restart(context.Context) error
	Close(context.Context) error
	AllowFallback(bool)
	RunnerType() string // Returns "podman" or "host"
}

// HostShellRunner runs commands directly on the host system
type HostShellRunner struct {
	config *Config
}

func newHostShellRunner(config *Config) *HostShellRunner {
	return &HostShellRunner{config: config}
}

func (r *HostShellRunner) Run(ctx context.Context, params RunInShellInput) (RunInShellOutput, error) {
	return hostRun(ctx, params)
}

func (r *HostShellRunner) Restart(ctx context.Context) error {
	// No-op for host shell runner
	return nil
}

func (r *HostShellRunner) Close(ctx context.Context) error {
	// No-op for host shell runner
	return nil
}

func (r *HostShellRunner) AllowFallback(allow bool) {
	// No-op for host shell runner
}

func (r *HostShellRunner) RunnerType() string {
	return "host"
}

// ShellRunnerInfo contains information about the current shell runner
type ShellRunnerInfo struct {
	Type        string // "podman" or "host"
	ContainerID string // Container ID if using podman, empty otherwise
}

// getShellRunnerInfo returns information about the current shell runner
func getShellRunnerInfo() ShellRunnerInfo {
	shellRunnerMu.RLock()
	defer shellRunnerMu.RUnlock()

	if currentShellRunner == nil {
		return ShellRunnerInfo{Type: "host", ContainerID: ""}
	}

	info := ShellRunnerInfo{
		Type: currentShellRunner.RunnerType(),
	}

	// If it's a podman runner, try to get the container ID
	if podmanRunner, ok := currentShellRunner.(*PodmanShellRunner); ok {
		info.ContainerID = podmanRunner.ContainerID()
	}

	return info
}

var (
	shellRunnerMu      sync.RWMutex
	currentShellRunner shellRunner
	shellRunnerOnce    sync.Once
)

func setShellRunnerForTesting(r shellRunner) func() {
	shellRunnerMu.Lock()
	prev := currentShellRunner
	currentShellRunner = r
	shellRunnerMu.Unlock()
	return func() {
		shellRunnerMu.Lock()
		currentShellRunner = prev
		shellRunnerMu.Unlock()
	}
}

// isPodmanAvailable checks if podman daemon is running AND the sandbox image exists
func isPodmanAvailable(config *Config, repoInfo RepoInfo) bool {
	// Try to run a simple podman command to check if daemon is available
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Determine the image name
	imageName := fmt.Sprintf("localhost/asimi-sandbox-%s:latest", repoInfo.Slug)
	if config != nil && config.RunInShell.ImageName != "" {
		imageName = config.RunInShell.ImageName
	}

	// Check if podman is available and the image exists using podman CLI
	// This is simpler and more reliable than using the bindings for a quick check
	cmd := exec.CommandContext(ctx, "podman", "image", "exists", imageName)
	err := cmd.Run()
	if err != nil {
		slog.Debug("podman not available or image missing", "image", imageName, "error", err)
		return false
	}

	slog.Debug("podman available with image", "image", imageName)
	return true
}

func initShellRunner(config *Config) {
	shellRunnerMu.Lock()
	defer shellRunnerMu.Unlock()

	repoInfo := GetRepoInfo()

	// Auto-detect and assign shell runner
	if isPodmanAvailable(config, repoInfo) {
		slog.Info("using podman shell runner")
		currentShellRunner = newPodmanShellRunner(config.RunInShell.AllowHostFallback, config, repoInfo)
	} else {
		slog.Info("using host shell runner (podman not available or image missing)")
		currentShellRunner = newHostShellRunner(config)
	}
}

func getShellRunner() shellRunner {
	shellRunnerOnce.Do(func() {
		repoInfo := GetRepoInfo()
		shellRunnerMu.Lock()
		if currentShellRunner == nil {
			// Default to podman runner with fallback disabled and nil config
			currentShellRunner = newPodmanShellRunner(false, nil, repoInfo)
		}
		shellRunnerMu.Unlock()
	})
	shellRunnerMu.RLock()
	defer shellRunnerMu.RUnlock()
	return currentShellRunner
}

// shouldRunOnHost checks if a command matches any of the run_on_host patterns
// and whether it requires user approval (i.e., not in safe_run_on_host patterns).
func (t RunInShell) shouldRunOnHost(command string) (runOnHost, requiresApproval bool) {
	if t.config == nil || len(t.config.RunInShell.RunOnHost) == 0 {
		return
	}
	// First check if command matches any safe_run_on_host pattern (no approval needed)
	for _, pattern := range t.config.RunInShell.SafeRunOnHost {
		matched, err := regexp.MatchString(pattern, command)
		if err != nil {
			// Log warning but continue checking other patterns
			continue
		}
		if matched {
			runOnHost = true
			requiresApproval = false
			return
		}
	}

	// Check if command matches any run_on_host pattern
	for _, pattern := range t.config.RunInShell.RunOnHost {
		matched, _ := regexp.MatchString(pattern, command)
		if matched {
			runOnHost = true
			requiresApproval = true
			return
		}
	}

	runOnHost = false
	requiresApproval = false
	return
}

func (t RunInShell) Name() string {
	return "run_in_shell"
}

func (t RunInShell) Description() string {
	return "Executes a shell command in a persistent shell session inside a container. The project root is mounted at `/workspace`, and when in a worktree, the shell automatically navigates to the worktree directory. Current working directory is maintained between commands. The input should be a JSON object with 'command' and optional 'description' fields.\n\nIMPORTANT: Each command runs in an isolated subshell for stability and predictability. This means:\n- Environment variables set with 'export' do NOT persist between commands\n- Directory changes with 'cd' do NOT persist between commands\n- Each command starts fresh in the project/worktree root directory\n- To perform multi-step operations, combine them in a single command using && or ; (e.g., 'cd dir && make && cd ..')\n- Redirects and heredocs work correctly within each command"
}

func (t RunInShell) Call(ctx context.Context, input string) (string, error) {
	var params RunInShellInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	var output RunInShellOutput
	var runErr error

	// Check if command should run on host based on config patterns
	runOnHost, requiresApproval := t.shouldRunOnHost(params.Command)
	if runOnHost {
		slog.Info("Executing safe command on HOST", "needs approval", requiresApproval, "command", params.Command)

		// Set the approval flag based on config patterns
		params.RequestApproval = requiresApproval

		// Run directly on host using hostRun (which handles approval internally)
		output, runErr = hostRun(ctx, params)
	} else {
		runner := getShellRunner()
		output, runErr = runner.Run(ctx, params)

		// If we got a harness error, try to restart and retry once
		if runErr != nil {
			slog.Warn("Shell runner failed", "error", runErr)

			// Try to restart the container connection
			if restartErr := runner.Restart(ctx); restartErr != nil {
				// If restart fails, return the original error
				return "", fmt.Errorf("command failed and restart failed: %w (restart error: %v)", runErr, restartErr)
			}

			// Retry the command once after restart
			output, runErr = runner.Run(ctx, params)
			slog.Info("bash: Command Finished", "error", runErr)
		}
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

func (t RunInShell) ParameterSchema() map[string]any {
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

// String formats a run_in_shell tool call for display
func (t RunInShell) Format(input, result string, err error) string {
	var params RunInShellInput
	var ec string
	json.Unmarshal([]byte(input), &params)
	line3 := ""
	if err != nil {
		line3 = fmt.Sprintf("%sERROR: %v\n", treeFinalPrefix, err)
	} else if result != "" {
		var output map[string]interface{}
		err := json.Unmarshal([]byte(result), &output)
		if err == nil {
			ec = output["exitCode"].(string)
			if ec != "0" {
				// TODO: Format with warning color
				line3 = fmt.Sprintf("%s%s\n", treeFinalPrefix, ec)
			}
		} else {
			line3 = fmt.Sprintf("%sERROR: %s\n", treeFinalPrefix, err)
		}
	}

	var ret strings.Builder
	ret.WriteString(params.Description + "\n")
	if line3 == "" {
		ret.WriteString(fmt.Sprintf("%s$ %s\n", treeFinalPrefix, params.Command))
	} else {
		ret.WriteString(fmt.Sprintf("%s$ %s\n", treeMidPrefix, params.Command))
	}
	ret.WriteString(line3)
	return ret.String()
}

func hostRun(ctx context.Context, params RunInShellInput) (RunInShellOutput, error) {
	var output RunInShellOutput

	// Check if approval is required
	if params.RequestApproval {
		approved, err := requestHostCommandApproval(ctx, params.Command)
		if err != nil {
			output.Output = fmt.Sprintf("Error requesting approval: %v", err)
			output.ExitCode = "1"
			return output, err
		}

		if !approved {
			output.Output = "Command execution denied by user"
			output.ExitCode = "1"
			return output, fmt.Errorf("command denied by user: %s", params.Command)
		}
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", params.Command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", params.Command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// Populate stdout and stderr separately
	output.Output = stdout.String() + "\n" + stderr.String()

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			output.ExitCode = fmt.Sprintf("%d", exitErr.ExitCode())
		} else {
			output.ExitCode = "-1"
		}
	} else {
		if cmd.ProcessState != nil {
			output.ExitCode = fmt.Sprintf("%d", cmd.ProcessState.ExitCode())
		} else {
			output.ExitCode = "0"
		}
	}

	return output, nil
}

// HostCommandApprovalRequest represents a request for user approval to run a host command
type HostCommandApprovalRequest struct {
	Command      string
	ResponseChan chan bool
}

// hostCommandApprovalChan is used to send approval requests to the TUI
var hostCommandApprovalChan chan HostCommandApprovalRequest

// SetHostCommandApprovalChannel sets the channel used for host command approval requests
// This should be called by the TUI during initialization
func SetHostCommandApprovalChannel(ch chan HostCommandApprovalRequest) {
	hostCommandApprovalChan = ch
}

// requestHostCommandApproval sends an approval request to the TUI and waits for a response
func requestHostCommandApproval(ctx context.Context, command string) (bool, error) {
	if hostCommandApprovalChan == nil {
		// No approval channel configured - deny by default for safety
		slog.Warn("Host command approval requested but no approval channel configured", "command", command)
		return false, fmt.Errorf("no approval mechanism configured")
	}

	responseChan := make(chan bool, 1)
	request := HostCommandApprovalRequest{
		Command:      command,
		ResponseChan: responseChan,
	}

	// Send the approval request
	select {
	case hostCommandApprovalChan <- request:
		// Request sent successfully
	case <-ctx.Done():
		return false, ctx.Err()
	}

	// Wait for the response
	select {
	case approved := <-responseChan:
		return approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

type PodmanUnavailableError struct {
	reason string
}

func (e PodmanUnavailableError) Error() string {
	return e.reason
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
		if err := validatePathWithinProject(path); err != nil {
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
	secondLine := treeFinalPrefix
	if err != nil {
		secondLine += fmt.Sprintf("Error: %v", err)
	} else {
		// Count files by counting "---\t" markers
		fileCount := strings.Count(result, "---\t")
		secondLine += fmt.Sprintf("Read %d files", fileCount)
	}

	return firstLine + "\n" + secondLine + "\n"
}

type Tool interface {
	tools.Tool
	Format(input, result string, err error) string
	// ParameterSchema returns the JSON schema for the tool's parameters
	ParameterSchema() map[string]any
}

func getAvailableTools(config *Config) []Tool {
	return []Tool{
		ReadFileTool{},
		WriteFileTool{},
		ListDirectoryTool{},
		ReplaceTextTool{},
		RunInShell{config: config},
		ReadManyFilesTool{},
	}
}

// availableTools is a package-level variable for backward compatibility
// It will be initialized with nil config by default
var availableTools = getAvailableTools(nil)
