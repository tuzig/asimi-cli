package main

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sync"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/shogunate"
)

var (
	shellRunnerMu       sync.RWMutex
	currentPodmanRunner *runners.PodmanRunner
	currentHostRunner   *runners.HostRunner
	shellRunnerOnce     sync.Once
	// toolScheduler holds the scheduler for clearing the queue on restart
	toolScheduler *shogunate.CoreToolScheduler
	// runnerMsgChan is the channel for runner messages
	runnerMsgChan chan runners.Msg
	// runnerMsgDone signals when the message handler goroutine is done
	runnerMsgDone chan struct{}
	// testPodmanAvailable is used for testing to simulate podman availability
	testPodmanAvailable *bool
)

// ShellRunnerInfo contains information about the current shell runner
type ShellRunnerInfo struct {
	Type        string // "podman" or "host"
	ContainerID string // Container ID if using podman, empty otherwise
}

// GetShellRunnerInfo returns information about the current shell runner
func GetShellRunnerInfo() ShellRunnerInfo {
	shellRunnerMu.RLock()
	defer shellRunnerMu.RUnlock()

	if currentPodmanRunner != nil {
		return ShellRunnerInfo{
			Type:        "podman",
			ContainerID: currentPodmanRunner.ContainerID(),
		}
	}

	return ShellRunnerInfo{Type: "host", ContainerID: ""}
}

// getImageName extracts the image name from config and repo info
func getImageName(config *Config, repoInfo repo.RepoInfo) string {
	imageName := fmt.Sprintf("localhost/asimi-sandbox-%s:latest", repoInfo.Slug)
	if config != nil && config.RunShellCommand.ImageName != "" {
		imageName = config.RunShellCommand.ImageName
	}
	return imageName
}

// startRunnerMessageHandler starts a goroutine that handles messages from runners
func startRunnerMessageHandler() {
	runnerMsgChan = make(chan runners.Msg, 10)
	runnerMsgDone = make(chan struct{})

	go func() {
		defer close(runnerMsgDone)
		for msg := range runnerMsgChan {
			switch m := msg.(type) {
			case runners.ContainerLaunchedMsg:
				// Notify TUI about container launch
				if program != nil {
					program.Send(containerLaunchMsg{message: m.Message})
				}
			case runners.ApprovalRequestMsg:
				// Handle approval request by forwarding to TUI
				go handleApprovalRequest(m)
			case runners.ClearSchedulerMsg:
				// Clear the scheduler queue and respond
				count := 0
				if toolScheduler != nil {
					count = toolScheduler.ClearQueue()
				}
				m.ResultChan <- count
			}
		}
	}()
}

// handleApprovalRequest forwards the approval request to the TUI
func handleApprovalRequest(req runners.ApprovalRequestMsg) {
	if hostCommandApprovalChan == nil {
		slog.Warn("Approval requested but no approval channel configured", "command", req.Command)
		req.ResponseChan <- false
		return
	}

	// Forward to the existing approval mechanism
	responseChan := make(chan bool, 1)
	approvalReq := HostCommandApprovalRequest{
		Command:      req.Command,
		ResponseChan: responseChan,
	}

	select {
	case hostCommandApprovalChan <- approvalReq:
		// Wait for response
		approved := <-responseChan
		req.ResponseChan <- approved
	default:
		// Channel full or blocked
		req.ResponseChan <- false
	}
}

// stopRunnerMessageHandler stops the message handler goroutine
func stopRunnerMessageHandler() {
	if runnerMsgChan != nil {
		close(runnerMsgChan)
		<-runnerMsgDone
	}
}

func initShellRunner(config *Config, scheduler *shogunate.CoreToolScheduler) {
	shellRunnerMu.Lock()
	defer shellRunnerMu.Unlock()

	toolScheduler = scheduler

	// Start the message handler
	startRunnerMessageHandler()

	repoInfo := GetRepoInfo()

	// Always create a host runner
	currentHostRunner = runners.NewHostRunner(runnerMsgChan)

	// Create podman runner if available
	imageName := getImageName(config, repoInfo.RepoInfo)
	if runners.IsPodmanAvailable(imageName) {
		slog.Info("using podman shell runner")
		runnerConfig := runners.Config{
			ImageName:      config.RunShellCommand.ImageName,
			TimeoutMinutes: config.RunShellCommand.TimeoutMinutes,
			NoCleanup:      config.RunShellCommand.NoCleanup,
			AllowFallback:  config.RunShellCommand.AllowHostFallback,
		}
		// Add additional mounts
		for _, m := range config.Container.AdditionalMounts {
			runnerConfig.AdditionalMounts = append(runnerConfig.AdditionalMounts, runners.Mount{
				Source:      m.Source,
				Destination: m.Destination,
			})
		}
		currentPodmanRunner = runners.NewPodmanRunner(runnerConfig, repoInfo.RepoInfo, runnerMsgChan, currentHostRunner)
	} else {
		slog.Info("using host shell runner (podman not available or image missing)")
	}
}

// tryUpgradeToSandbox checks if we're currently on the host runner and a sandbox
// image has become available. If so, it upgrades to the podman runner.
func tryUpgradeToSandbox(config *Config) bool {
	shellRunnerMu.Lock()
	defer shellRunnerMu.Unlock()

	if currentPodmanRunner != nil {
		slog.Debug("not upgrading shell runner", "reason", "already using podman")
		return false
	}

	repoInfo := GetRepoInfo()
	imageName := getImageName(config, repoInfo.RepoInfo)

	if runners.IsPodmanAvailable(imageName) {
		slog.Info("sandbox image now available, upgrading from host to podman shell runner")
		runnerConfig := runners.Config{
			ImageName:      config.RunShellCommand.ImageName,
			TimeoutMinutes: config.RunShellCommand.TimeoutMinutes,
			NoCleanup:      config.RunShellCommand.NoCleanup,
			AllowFallback:  config.RunShellCommand.AllowHostFallback,
		}
		for _, m := range config.Container.AdditionalMounts {
			runnerConfig.AdditionalMounts = append(runnerConfig.AdditionalMounts, runners.Mount{
				Source:      m.Source,
				Destination: m.Destination,
			})
		}
		currentPodmanRunner = runners.NewPodmanRunner(runnerConfig, repoInfo.RepoInfo, runnerMsgChan, currentHostRunner)
		return true
	}

	slog.Debug("sandbox image still not available, staying on host runner")
	return false
}

// shouldRunOnHost checks if a command matches any of the run_on_host patterns
// and whether it requires user approval (i.e., not in safe_run_on_host patterns).
func shouldRunOnHost(config *Config, command string) (runOnHost, requiresApproval bool) {
	runOnHost = false
	requiresApproval = true
	if config == nil {
		return
	}

	shellRunnerMu.RLock()
	hasPodman := currentPodmanRunner != nil
	// For testing: override the podman availability check
	if testPodmanAvailable != nil {
		hasPodman = *testPodmanAvailable
	}
	shellRunnerMu.RUnlock()

	if hasPodman {
		// Check if command matches any run_on_host pattern
		for _, pattern := range config.RunShellCommand.RunOnHost {
			matched, _ := regexp.MatchString(pattern, command)
			if matched {
				goto onHost
			}
		}
		requiresApproval = false
		return
	}

onHost:
	runOnHost = true

	if len(config.RunShellCommand.SafeRunOnHost) == 0 {
		return
	}

	for _, pattern := range config.RunShellCommand.SafeRunOnHost {
		matched, err := regexp.MatchString(pattern, command)
		if err != nil {
			continue
		}
		if matched {
			requiresApproval = false
		}
	}

	return
}

// getAvailableTools returns the list of tools available to ministers
func getAvailableTools(config *Config) []shogunate.Tool {
	fileTools := shogunate.GetFileTools()

	// Create the run shell command tool using the new architecture
	shellRunnerMu.RLock()
	podman := currentPodmanRunner
	host := currentHostRunner
	shellRunnerMu.RUnlock()

	// Create adapters that match the Runner interface expected by shogunate.RunShellCommand
	var runner shogunate.Runner
	var hostRunner shogunate.Runner

	if podman != nil {
		runner = runners.NewShogunateAdapter(podman)
	}
	if host != nil {
		hostRunner = runners.NewShogunateAdapter(host)
	}

	runShellCmd := shogunate.NewRunShellCommand(
		runner,
		hostRunner,
		func(cmd string) (bool, bool) {
			return shouldRunOnHost(config, cmd)
		},
	)

	return append(fileTools, runShellCmd)
}

// HostCommandApprovalRequest represents a request for user approval to run a host command
type HostCommandApprovalRequest struct {
	Command      string
	ResponseChan chan bool
}

// hostCommandApprovalChan is used to send approval requests to the TUI
var hostCommandApprovalChan chan HostCommandApprovalRequest

// SetHostCommandApprovalChannel sets the channel used for host command approval requests
func SetHostCommandApprovalChannel(ch chan HostCommandApprovalRequest) {
	hostCommandApprovalChan = ch
}

// availableTools is a package-level variable for backward compatibility
var availableTools = getAvailableTools(nil)

// closeShellRunner closes the current shell runners
func closeShellRunner(ctx context.Context) {
	shellRunnerMu.Lock()
	defer shellRunnerMu.Unlock()

	if currentPodmanRunner != nil {
		currentPodmanRunner.Close(ctx)
		currentPodmanRunner = nil
	}

	stopRunnerMessageHandler()
}

// GetRunner returns the current shell runner (podman if available, otherwise host).
// This is the primary runner used for command execution.
func GetRunner() runners.Runner {
	shellRunnerMu.RLock()
	defer shellRunnerMu.RUnlock()

	if currentPodmanRunner != nil {
		return currentPodmanRunner
	}
	if currentHostRunner != nil {
		return currentHostRunner
	}
	return nil
}

// GetHostRunner returns the host runner for direct host access.
// Use this when you explicitly need to run commands on the host.
func GetHostRunner() runners.Runner {
	shellRunnerMu.RLock()
	defer shellRunnerMu.RUnlock()

	return currentHostRunner
}

// GetPodmanRunner returns the podman runner if available (nil otherwise).
// Use this when you need podman-specific functionality like AllowFallback.
func GetPodmanRunner() *runners.PodmanRunner {
	shellRunnerMu.RLock()
	defer shellRunnerMu.RUnlock()

	return currentPodmanRunner
}

// requestHostCommandApproval sends an approval request via the approval channel and waits for response
func requestHostCommandApproval(ctx context.Context, command string) (bool, error) {
	if hostCommandApprovalChan == nil {
		return false, fmt.Errorf("no approval mechanism configured")
	}

	responseChan := make(chan bool, 1)
	request := HostCommandApprovalRequest{
		Command:      command,
		ResponseChan: responseChan,
	}

	select {
	case hostCommandApprovalChan <- request:
		// Request sent successfully
	case <-ctx.Done():
		return false, ctx.Err()
	}

	select {
	case approved := <-responseChan:
		return approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// setTestPodmanAvailable sets whether podman is available for testing purposes.
// Pass nil to use the real check, or a pointer to a bool to override.
func setTestPodmanAvailable(available *bool) {
	shellRunnerMu.Lock()
	testPodmanAvailable = available
	shellRunnerMu.Unlock()
}
