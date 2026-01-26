package runners

import (
	"context"
	"fmt"
)

// Input is the input for shell command execution
type Input struct {
	Command        string `json:"command"`
	Description    string `json:"description"`
	BypassApproval bool   `json:"-"` // Internal field, not included in JSON
}

// Output is the output of shell command execution
type Output struct {
	Output   string `json:"stdout"`
	ExitCode string `json:"exitCode"`
}

// Runner is the interface for shell command execution backends
type Runner interface {
	Run(ctx context.Context, input Input) (Output, error)
	Restart(ctx context.Context) error
	Close(ctx context.Context) error
	RunnerType() string // Returns "podman" or "host"
}

// Config holds configuration for runners
type Config struct {
	ImageName        string
	TimeoutMinutes   int
	NoCleanup        bool
	AllowFallback    bool
	AdditionalMounts []Mount
}

// Mount represents a volume mount for containers
type Mount struct {
	Source      string
	Destination string
}

// Messages sent by runners (bubbletea pattern)

// Msg is the interface for runner messages
type Msg interface{ runnerMsg() }

// ContainerLaunchedMsg is sent when a container is launched
type ContainerLaunchedMsg struct{ Message string }

func (ContainerLaunchedMsg) runnerMsg() {}

// ApprovalRequestMsg is sent when a command needs user approval
type ApprovalRequestMsg struct {
	Command      string
	ResponseChan chan bool
}

func (ApprovalRequestMsg) runnerMsg() {}

// ClearSchedulerMsg is sent when the runner needs to clear the scheduler queue
type ClearSchedulerMsg struct {
	ResultChan chan int
}

func (ClearSchedulerMsg) runnerMsg() {}

// CommandDeniedError is returned when a user denies a host command approval request
type CommandDeniedError struct {
	Command string
}

func (e CommandDeniedError) Error() string {
	return fmt.Sprintf("command denied by user: `%s`", e.Command)
}

// PodmanUnavailableError is returned when podman is not available
type PodmanUnavailableError struct {
	Reason string
}

func (e PodmanUnavailableError) Error() string {
	return e.Reason
}

// SandboxMissingError is returned when the sandbox image is missing
type SandboxMissingError struct{}

func (e SandboxMissingError) Error() string {
	return "Sandbox container image is missing. Did you run `:init` ?"
}

