package e2e

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// RunShellCommandInput mirrors the main package's RunShellCommandInput
type RunShellCommandInput struct {
	Command        string
	Description    string
	BypassApproval bool
}

// RunShellCommandOutput mirrors the main package's RunShellCommandOutput
type RunShellCommandOutput struct {
	Output   string
	ExitCode string
}

// CommandRecord stores information about a command that was executed
type CommandRecord struct {
	Input  RunShellCommandInput
	Output RunShellCommandOutput
	Error  error
}

// MockRunner provides a configurable mock shell runner for testing.
// It can return pre-configured results for specific commands and records
// all commands for assertions.
type MockRunner struct {
	mu sync.Mutex

	// Type is the runner type returned by RunnerType() ("host" or "podman")
	Type string

	// CommandResults maps command patterns (regex) to their outputs
	CommandResults map[string]RunShellCommandOutput

	// DefaultResult is returned when no pattern matches
	DefaultResult RunShellCommandOutput

	// RecordedCommands stores all commands executed through this runner
	RecordedCommands []CommandRecord

	// ErrorOnCommand causes Run to return an error for commands matching the pattern
	ErrorOnCommand map[string]error

	// SequentialResults returns results in order, ignoring command content
	SequentialResults []RunShellCommandOutput
	sequentialIndex   int

	// FailAfterN causes commands to fail after N successful executions
	FailAfterN    int
	failAfterFunc func(CommandRecord) RunShellCommandOutput

	// AllowFallbackValue tracks the AllowFallback setting
	AllowFallbackValue bool
}

// NewMockRunner creates a new MockRunner with default settings
func NewMockRunner(runnerType string) *MockRunner {
	return &MockRunner{
		Type:           runnerType,
		CommandResults: make(map[string]RunShellCommandOutput),
		ErrorOnCommand: make(map[string]error),
		DefaultResult: RunShellCommandOutput{
			Output:   "",
			ExitCode: "0",
		},
	}
}

// NewMockHostRunner creates a mock runner configured as a host runner
func NewMockHostRunner() *MockRunner {
	return NewMockRunner("host")
}

// NewMockPodmanRunner creates a mock runner configured as a podman runner
func NewMockPodmanRunner() *MockRunner {
	runner := NewMockRunner("podman")
	// Configure default behavior for podman runner
	runner.WithCommandResult("uname", RunShellCommandOutput{Output: "Linux", ExitCode: "0"})
	return runner
}

// WithCommandResult adds a result for commands matching the pattern
func (m *MockRunner) WithCommandResult(pattern string, result RunShellCommandOutput) *MockRunner {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CommandResults[pattern] = result
	return m
}

// WithDefaultResult sets the default result when no pattern matches
func (m *MockRunner) WithDefaultResult(result RunShellCommandOutput) *MockRunner {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DefaultResult = result
	return m
}

// WithSequentialResults sets up results that are returned in order
func (m *MockRunner) WithSequentialResults(results ...RunShellCommandOutput) *MockRunner {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SequentialResults = results
	m.sequentialIndex = 0
	return m
}

// WithError causes the runner to return an error for commands matching the pattern
func (m *MockRunner) WithError(pattern string, err error) *MockRunner {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ErrorOnCommand[pattern] = err
	return m
}

// WithFailAfterN causes commands to fail after N successful executions
func (m *MockRunner) WithFailAfterN(n int, failResult RunShellCommandOutput) *MockRunner {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailAfterN = n
	m.failAfterFunc = func(_ CommandRecord) RunShellCommandOutput { return failResult }
	return m
}

// Run executes a command (mock implementation)
func (m *MockRunner) Run(ctx context.Context, params RunShellCommandInput) (RunShellCommandOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for context cancellation (only if context is not nil)
	if ctx != nil {
		select {
		case <-ctx.Done():
			return RunShellCommandOutput{}, ctx.Err()
		default:
		}
	}

	// Check for errors first
	for pattern, err := range m.ErrorOnCommand {
		matched, _ := regexp.MatchString(pattern, params.Command)
		if matched || strings.Contains(params.Command, pattern) {
			record := CommandRecord{Input: params, Error: err}
			m.RecordedCommands = append(m.RecordedCommands, record)
			return RunShellCommandOutput{}, err
		}
	}

	// Check FailAfterN
	if m.FailAfterN > 0 && len(m.RecordedCommands) >= m.FailAfterN && m.failAfterFunc != nil {
		record := CommandRecord{Input: params}
		result := m.failAfterFunc(record)
		record.Output = result
		m.RecordedCommands = append(m.RecordedCommands, record)
		return result, nil
	}

	var result RunShellCommandOutput

	// Check for sequential results first
	if len(m.SequentialResults) > 0 && m.sequentialIndex < len(m.SequentialResults) {
		result = m.SequentialResults[m.sequentialIndex]
		m.sequentialIndex++
	} else {
		// Look for pattern match
		found := false
		for pattern, res := range m.CommandResults {
			matched, _ := regexp.MatchString(pattern, params.Command)
			if matched || strings.Contains(params.Command, pattern) {
				result = res
				found = true
				break
			}
		}
		if !found {
			result = m.DefaultResult
		}
	}

	record := CommandRecord{Input: params, Output: result}
	m.RecordedCommands = append(m.RecordedCommands, record)

	return result, nil
}

// Restart resets the runner (mock implementation)
func (m *MockRunner) Restart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sequentialIndex = 0
	return nil
}

// Close closes the runner (mock implementation)
func (m *MockRunner) Close(ctx context.Context) error {
	return nil
}

// AllowFallback sets the fallback behavior (mock implementation)
func (m *MockRunner) AllowFallback(allow bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AllowFallbackValue = allow
}

// RunnerType returns the runner type
func (m *MockRunner) RunnerType() string {
	return m.Type
}

// GetCommandHistory returns all recorded commands
func (m *MockRunner) GetCommandHistory() []CommandRecord {
	m.mu.Lock()
	defer m.mu.Unlock()

	history := make([]CommandRecord, len(m.RecordedCommands))
	copy(history, m.RecordedCommands)
	return history
}

// GetCallCount returns the number of commands executed
func (m *MockRunner) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.RecordedCommands)
}

// Reset clears all recorded commands
func (m *MockRunner) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedCommands = nil
	m.sequentialIndex = 0
}

// AssertCommandExecuted checks that a command matching the pattern was executed
func (m *MockRunner) AssertCommandExecuted(pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, record := range m.RecordedCommands {
		matched, _ := regexp.MatchString(pattern, record.Input.Command)
		if matched || strings.Contains(record.Input.Command, pattern) {
			return nil
		}
	}
	return fmt.Errorf("no command matched pattern: %q", pattern)
}

// AssertCommandNotExecuted checks that no command matching the pattern was executed
func (m *MockRunner) AssertCommandNotExecuted(pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, record := range m.RecordedCommands {
		matched, _ := regexp.MatchString(pattern, record.Input.Command)
		if matched || strings.Contains(record.Input.Command, pattern) {
			return fmt.Errorf("command matched pattern: %q (command: %s)", pattern, record.Input.Command)
		}
	}
	return nil
}

// AssertCallCount checks that exactly n commands were executed
func (m *MockRunner) AssertCallCount(expected int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.RecordedCommands) != expected {
		return fmt.Errorf("expected %d commands, got %d", expected, len(m.RecordedCommands))
	}
	return nil
}

// LastCommand returns the most recent command, or empty if none
func (m *MockRunner) LastCommand() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.RecordedCommands) == 0 {
		return ""
	}
	return m.RecordedCommands[len(m.RecordedCommands)-1].Input.Command
}

// CommandAt returns the command at the given index (0-based)
func (m *MockRunner) CommandAt(index int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.RecordedCommands) {
		return "", fmt.Errorf("command index %d out of range (have %d commands)", index, len(m.RecordedCommands))
	}
	return m.RecordedCommands[index].Input.Command, nil
}

// CountCommandsMatching returns the number of commands matching the pattern
func (m *MockRunner) CountCommandsMatching(pattern string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, record := range m.RecordedCommands {
		matched, _ := regexp.MatchString(pattern, record.Input.Command)
		if matched || strings.Contains(record.Input.Command, pattern) {
			count++
		}
	}
	return count
}

// TestSuccessResult creates a successful result with the given output
func TestSuccessResult(output string) RunShellCommandOutput {
	return RunShellCommandOutput{Output: output, ExitCode: "0"}
}

// TestFailResult creates a failed result with the given output
func TestFailResult(output string) RunShellCommandOutput {
	return RunShellCommandOutput{Output: output, ExitCode: "1"}
}

// TestExitResult creates a result with the given exit code
func TestExitResult(output string, exitCode string) RunShellCommandOutput {
	return RunShellCommandOutput{Output: output, ExitCode: exitCode}
}
