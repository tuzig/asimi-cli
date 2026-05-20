package runners

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
)

var (
	globalRunner Runner
	runnerMu     sync.RWMutex
)

// SetRunner sets the global runner (thread-safe)
func SetRunner(r Runner) {
	runnerMu.Lock()
	globalRunner = r
	runnerMu.Unlock()
}

// GetRunner returns the global runner (thread-safe)
func GetRunner() Runner {
	runnerMu.RLock()
	r := globalRunner
	runnerMu.RUnlock()
	return r
}

// DefaultMaxOutputSize is the fallback when no config is provided.
const DefaultMaxOutputSize = 51200 // 50KB

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
	AllowFallback(bool)
	RunnerType() string // Returns "podman" or "host"
	SetMessageChannel(msgChan chan<- Msg)
}

// Type aliases - use types from internal/config as the single source of truth
type (
	Config = config.SandboxConfig
	Mount  = config.Mount
)

// Messages sent by runners (bubbletea pattern)

// Msg is the interface for runner messages
type Msg interface{}

// ContainerLaunchedMsg is sent when a container is launched
type ContainerLaunchedMsg struct {
	Message     string `msgpack:"message,omitempty"`
	ContainerID string `msgpack:"container_id,omitempty"`
}

// ApprovalRequestMsg is sent when a command needs user approval.
// ResponseChan is an in-process field only; over the RPC wire this
// message becomes a daemon→TUI request that expects a reply.
type ApprovalRequestMsg struct {
	Command      string    `msgpack:"command"`
	ResponseChan chan bool `msgpack:"-"`
}

// ClearSchedulerMsg is sent when the runner needs to clear the scheduler queue.
// Always in-process only.
type ClearSchedulerMsg struct {
	ResultChan chan int `msgpack:"-"`
}

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

func InitShellRunner(config *Config, repoInfo repo.RepoInfo) Runner {
	var runner Runner
	runner = NewHostRunner(0)

	// Resolve image name using same default as NewPodmanRunner
	imageName := config.ImageName
	if imageName == "" {
		imageName = fmt.Sprintf("localhost/asimi-sandbox-%s:latest", repoInfo.Slug)
	}

	// Auto-detect and assign shell runner
	if IsPodmanAvailable(imageName) {
		slog.Info("using podman shell runner", "image", imageName)
		runner = NewPodmanRunner(config, repoInfo, uint64(os.Getpid()), runner)
	} else {
		slog.Info("using host shell runner (podman not available or image missing)", "image", imageName)
	}
	SetRunner(runner)
	return runner
}
func HostRun(ctx context.Context, in Input) (Output, error) {
	runner := NewHostRunner(0)
	out, err := runner.Run(ctx, in)
	slog.Debug("Run a host command", "cmd", in.Command, "err", err, "out", out)
	return out, err
}

func lineBytes(lines []string) int {
	n := 0
	for _, l := range lines {
		n += len(l) + 1 // +1 for newline
	}
	return n
}
