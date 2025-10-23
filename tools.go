package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

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

// RunInShell is a tool for running shell commands in a persistent shell
type RunInShell struct{}

// RunInShellInput is the input for the RunInShell tool
type RunInShellInput struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// RunInShellOutput is the output of the RunInShell tool
type RunInShellOutput struct {
	Output   string `json:"output"`
	ExitCode string `json:"exitCode"`
}

type shellRunner interface {
	Run(context.Context, RunInShellInput) (RunInShellOutput, error)
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

func initShellRunner(config *Config) {
	shellRunnerMu.Lock()
	defer shellRunnerMu.Unlock()

	// Initialize podman shell runner with config
	currentShellRunner = newPodmanShellRunner(config.LLM.PodmanAllowHostFallback)
}

func getShellRunner() shellRunner {
	shellRunnerOnce.Do(func() {
		shellRunnerMu.Lock()
		if currentShellRunner == nil {
			// Default to podman runner with fallback disabled
			currentShellRunner = newPodmanShellRunner(false)
		}
		shellRunnerMu.Unlock()
	})
	shellRunnerMu.RLock()
	defer shellRunnerMu.RUnlock()
	return currentShellRunner
}

func (t RunInShell) Name() string {
	return "run_in_shell"
}

func (t RunInShell) Description() string {
	return "Executes a shell command in a persistent shell session. The input should be a JSON object with 'command' and optional 'description' fields."
}

func (t RunInShell) Call(ctx context.Context, input string) (string, error) {
	var params RunInShellInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	runner := getShellRunner()
	output, runErr := runner.Run(ctx, params)
	if runErr != nil {
		return "", runErr
	}

	outputBytes, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	return string(outputBytes), nil
}

// String formats a run_in_shell tool call for display
func (t RunInShell) Format(input, result string, err error) string {
	// Parse input JSON to extract command
	var params RunInShellInput
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
	firstLine := fmt.Sprintf("Run In Shell%s", paramStr)

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

type hostShellRunner struct{}

func (hostShellRunner) Run(ctx context.Context, params RunInShellInput) (RunInShellOutput, error) {
	var output RunInShellOutput

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

	// Combine stdout and stderr into a single output field
	output.Output = stdout.String()
	if stderr.Len() > 0 {
		if output.Output != "" {
			output.Output += "\n"
		}
		output.Output += stderr.String()
	}

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

// MergeToolInput defines the parameters expected by the merge tool.
type MergeToolInput struct {
	WorktreePath  string `json:"worktree_path"`
	Branch        string `json:"branch"`
	MainBranch    string `json:"main_branch"`
	Push          bool   `json:"push,omitempty"`
	AutoApprove   bool   `json:"auto_approve,omitempty"`
	CommitMessage string `json:"commit_message,omitempty"`
	SkipReview    bool   `json:"skip_review,omitempty"`
}

// MergeTool orchestrates squashing and merging a worktree-backed branch.
type MergeTool struct{}

func (t MergeTool) Name() string {
	return "merge"
}

func (t MergeTool) Description() string {
	return "Squashes the commits from a worktree-backed branch onto the main branch after a review in lazygit, then removes the worktree and deletes the branch."
}

func (t MergeTool) Call(ctx context.Context, input string) (string, error) {
	var params MergeToolInput
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	worktreePath := strings.TrimSpace(params.WorktreePath)
	if worktreePath == "" {
		return "", errors.New("worktree_path is required")
	}
	absWorktree, err := filepath.Abs(worktreePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve worktree path: %w", err)
	}

	branch := strings.TrimSpace(params.Branch)
	if branch == "" {
		return "", errors.New("branch is required")
	}

	mainBranch := strings.TrimSpace(params.MainBranch)
	if mainBranch == "" {
		mainBranch = "main"
	}

	commitMsg := strings.TrimSpace(params.CommitMessage)

	if _, err := os.Stat(absWorktree); err != nil {
		return "", fmt.Errorf("invalid worktree_path: %w", err)
	}

	if !params.SkipReview {
		lazygitCmd := strings.TrimSpace(os.Getenv("ASIMI_LAZYGIT_CMD"))
		if lazygitCmd == "" {
			lazygitCmd = "lazygit"
		}
		if _, err := exec.LookPath(lazygitCmd); err != nil {
			return "", fmt.Errorf("unable to locate lazygit command: %w", err)
		}

		cmd := exec.CommandContext(ctx, lazygitCmd)
		cmd.Dir = absWorktree
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("lazygit exited with an error: %w", err)
		}
	}

	readerNeeded := (!params.AutoApprove) || commitMsg == ""
	var reader *bufio.Reader
	if readerNeeded {
		reader = bufio.NewReader(os.Stdin)
	}

	approved := params.AutoApprove
	if !approved {
		fmt.Printf("Proceed with squash merge of %s onto %s? (y/N): ", branch, mainBranch)
		ans, err := readYesNo(reader)
		if err != nil {
			return "", fmt.Errorf("failed to read approval: %w", err)
		}
		approved = ans
	}

	if !approved {
		return "Merge cancelled by user", nil
	}

	if commitMsg == "" {
		if params.AutoApprove {
			return "", errors.New("commit_message is required when auto_approve is true")
		}
		fmt.Print("Enter squash commit message: ")
		line, err := readLine(reader)
		if err != nil {
			return "", fmt.Errorf("failed to read commit message: %w", err)
		}
		if line == "" {
			return "", errors.New("commit message cannot be empty")
		}
		commitMsg = line
	}

	var log bytes.Buffer
	log.WriteString("Starting merge process...\n")

	baseRef := fmt.Sprintf("origin/%s", mainBranch)
	if err := runGitCommand(ctx, absWorktree, &log, "fetch", "origin", mainBranch); err != nil {
		log.WriteString(fmt.Sprintf("git fetch origin %s failed: %v\n", mainBranch, err))
		baseRef = mainBranch
	}

	if err := runGitCommand(ctx, absWorktree, &log, "rebase", baseRef); err != nil {
		runGitCommand(ctx, absWorktree, &log, "rebase", "--abort")
		return "", fmt.Errorf("git rebase failed: %w\n%s", err, log.String())
	}

	if err := runGitCommand(ctx, absWorktree, &log, "reset", "--soft", baseRef); err != nil {
		return "", fmt.Errorf("git reset failed: %w\n%s", err, log.String())
	}

	if err := runGitCommand(ctx, absWorktree, &log, "add", "-A"); err != nil {
		return "", fmt.Errorf("git add failed: %w\n%s", err, log.String())
	}

	if err := runGitCommand(ctx, absWorktree, &log, "commit", "-m", commitMsg); err != nil {
		return "", fmt.Errorf("git commit failed: %w\n%s", err, log.String())
	}

	repoRoot, err := resolveRepoRoot(ctx, absWorktree)
	if err != nil {
		return "", fmt.Errorf("failed to resolve repository root: %w", err)
	}

	if err := runGitCommand(ctx, repoRoot, &log, "checkout", mainBranch); err != nil {
		return "", fmt.Errorf("git checkout %s failed: %w\n%s", mainBranch, err, log.String())
	}

	if err := runGitCommand(ctx, repoRoot, &log, "pull", "--ff-only", "origin", mainBranch); err != nil {
		log.WriteString(fmt.Sprintf("git pull origin %s failed: %v (continuing without remote update)\n", mainBranch, err))
	}

	if err := runGitCommand(ctx, repoRoot, &log, "merge", "--ff-only", branch); err != nil {
		return "", fmt.Errorf("git merge failed: %w\n%s", err, log.String())
	}

	if params.Push {
		if err := runGitCommand(ctx, repoRoot, &log, "push", "origin", mainBranch); err != nil {
			return "", fmt.Errorf("git push failed: %w\n%s", err, log.String())
		}
	}

	if err := runGitCommand(ctx, repoRoot, &log, "worktree", "remove", "--force", absWorktree); err != nil {
		return "", fmt.Errorf("git worktree remove failed: %w\n%s", err, log.String())
	}

	if err := runGitCommand(ctx, repoRoot, &log, "branch", "-D", branch); err != nil {
		return "", fmt.Errorf("git branch -D failed: %w\n%s", err, log.String())
	}

	log.WriteString("Merge completed successfully.\n")
	return log.String(), nil
}

func (t MergeTool) Format(input, result string, err error) string {
	var params MergeToolInput
	_ = json.Unmarshal([]byte(input), &params)

	branch := params.Branch
	if branch == "" {
		branch = "(unknown)"
	}
	mainBranch := params.MainBranch
	if mainBranch == "" {
		mainBranch = "main"
	}

	firstLine := fmt.Sprintf("Merge (%s -> %s)", branch, mainBranch)
	var secondLine string
	if err != nil {
		secondLine = fmt.Sprintf("  ⎿  Error: %v", err)
	} else {
		secondLine = "  ⎿  Merge completed"
	}

	return firstLine + "\n" + secondLine
}

func runGitCommand(ctx context.Context, dir string, log *bytes.Buffer, args ...string) error {
	if log != nil {
		log.WriteString(fmt.Sprintf("$ git %s\n", strings.Join(args, " ")))
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitCommandEnv()
	if log != nil {
		cmd.Stdout = log
		cmd.Stderr = log
	}

	return cmd.Run()
}

func resolveRepoRoot(ctx context.Context, worktreePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "rev-parse", "--git-common-dir")
	cmd.Env = gitCommandEnv()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir failed: %w (%s)", err, out.String())
	}

	commonDir := strings.TrimSpace(out.String())
	if commonDir == "" {
		return "", errors.New("git common dir not found")
	}

	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}

	return filepath.Dir(commonDir), nil
}

func readYesNo(reader *bufio.Reader) (bool, error) {
	response, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func gitCommandEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, value := range env {
		if strings.HasPrefix(value, "GIT_") {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
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
	RunInShell{},
	ReadManyFilesTool{},
	MergeTool{},
}
