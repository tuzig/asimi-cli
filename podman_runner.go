package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"al.essio.dev/pkg/shellescape"
	spec "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/specgen"
)

type PodmanShellRunner struct {
	imageName        string
	containerName    string
	allowFallback    bool
	noCleanup        bool // Skip container removal on exit (for debugging)
	config           *Config
	repoInfo         RepoInfo
	mu               sync.Mutex
	conn             context.Context
	containerStarted bool // tracks if container has been successfully started
	// Fields for container attachment
	stdinPipe  io.WriteCloser
	stdoutPipe io.ReadCloser
	// Command output storage
	outputs       map[int]*commandOutput
	outputsMu     sync.Mutex
	nextCommandID int
}

type commandOutput struct {
	output     string
	exitCode   string
	ready      chan struct{} // closed when both stdout and stderr are complete
	outputDone bool
}

func newPodmanShellRunner(allowFallback bool, config *Config, repoInfo RepoInfo) *PodmanShellRunner {
	pid := os.Getpid()
	noCleanup := false
	imageName := fmt.Sprintf("localhost/asimi-sandbox-%s:latest", repoInfo.Slug)

	if config != nil {
		if config.RunInShell.NoCleanup {
			noCleanup = true
		}
		if config.RunInShell.ImageName != "" {
			imageName = config.RunInShell.ImageName
		}
	} else {
		slog.Debug("Config is nil, using image: ", "name", imageName)
	}

	ret := &PodmanShellRunner{
		imageName:     imageName,
		containerName: fmt.Sprintf("asimi-shell-%d", pid),
		allowFallback: allowFallback,
		noCleanup:     noCleanup,
		config:        config,
		repoInfo:      repoInfo,
		stdinPipe:     nil,
		stdoutPipe:    nil,
		outputs:       make(map[int]*commandOutput),
		nextCommandID: 1,
	}
	slog.Debug("New podman shell runner", "object", ret)
	return ret
}

// initialize sets up everything needed to run commands: connection, container, and bash session
func (r *PodmanShellRunner) initialize(ctx context.Context) error {
	slog.Debug("initializing podman shell runner")

	// Step 1: Establish connection to podman if needed
	r.mu.Lock()
	hasConnection := r.conn != nil
	r.mu.Unlock()

	if !hasConnection {
		slog.Debug("establishing podman connection")
		conn, err := r.establishConnection(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}
		r.mu.Lock()
		r.conn = conn
		r.mu.Unlock()
		slog.Debug("podman connection established")
	}

	// Step 2: Ensure container is running (if not already started)
	r.mu.Lock()
	if !r.containerStarted {
		r.mu.Unlock()
		slog.Debug("ensuring container for this instance", "containerName", r.containerName)

		// Check if container already exists
		inspectData, err := containers.Inspect(r.conn, r.containerName, nil)
		if err == nil {
			// Container exists, check if it's running
			if inspectData.State.Running {
				slog.Debug("container already running", "containerName", r.containerName)
				r.mu.Lock()
				r.containerStarted = true
				r.mu.Unlock()
			} else {
				// Container exists but not running, start it
				slog.Debug("starting existing container", "containerName", r.containerName)
				if err := containers.Start(r.conn, r.containerName, nil); err != nil {
					return fmt.Errorf("failed to start existing container: %w", err)
				}
				slog.Debug("existing container started", "containerName", r.containerName)
				r.mu.Lock()
				r.containerStarted = true
				r.mu.Unlock()
			}
		} else {
			// Container doesn't exist, create it
			slog.Debug("container doesn't exist, creating new one", "containerName", r.containerName)
			if err := r.createContainer(ctx); err != nil {
				return err
			}
			r.mu.Lock()
			r.containerStarted = true
			r.mu.Unlock()

			// Notify user that container was launched
			if program != nil {
				program.Send(containerLaunchMsg{message: "🐳 Container launched"})
			}
		}
	} else {
		r.mu.Unlock()
		slog.Debug("container already started, skipping checks", "containerName", r.containerName)
	}

	// Step 3: Attach to container if needed
	r.mu.Lock()
	hasAttachment := r.stdinPipe != nil
	r.mu.Unlock()

	if !hasAttachment {
		slog.Debug("attaching to container")

		// Create pipes for stdin, stdout, and stderr
		slog.Debug("creating pipes for stdin, stdout, stderr")
		stdinReader, stdinWriter := io.Pipe()
		stdoutReader, stdoutWriter := io.Pipe()

		// Attach to the container in a goroutine so it doesn't block
		slog.Debug("starting Attach goroutine")
		go func() {
			slog.Debug("Attach goroutine started", "containerName", r.containerName)
			if err := containers.Attach(r.conn, r.containerName, stdinReader, stdoutWriter, nil, nil, nil); err != nil {
				slog.Error("error attaching to container", "error", err)
				// Handle error: close pipes and reset
				stdinReader.Close()
				stdoutWriter.Close()
				r.mu.Lock()
				r.stdinPipe = nil
				r.stdoutPipe = nil
				r.mu.Unlock()
				slog.Debug("container attachment reset after error")
			} else {
				slog.Debug("Attach completed successfully")
			}
		}()

		r.mu.Lock()
		r.stdinPipe = stdinWriter
		r.stdoutPipe = stdoutReader
		r.mu.Unlock()

		slog.Debug("container pipes configured")

		// Start persistent reader loops for stdout and stderr
		slog.Debug("starting persistent reader loops")
		go r.readStream(stdoutReader)

		slog.Debug("container attachment established", "repoInfo", r.repoInfo)

		var rc strings.Builder
		// Navigate to worktree if we're in one
		rc.WriteString("git config --global core.pager cat\n")
		if r.repoInfo.WorktreePath != "" {
			rc.WriteString(fmt.Sprintf("cd %s/%s\n", r.repoInfo.ProjectRoot, r.repoInfo.WorktreePath))
		} else {
			rc.WriteString(fmt.Sprintf("cd %s\n", r.repoInfo.ProjectRoot))
		}
		slog.Debug("navigating to path in the container", "path", r.repoInfo.WorktreePath)
		if _, err := r.stdinPipe.Write([]byte(rc.String())); err != nil {
			slog.Error("failed to navigate to worktree", "error", err)
		}
	}

	slog.Debug("initialization complete")
	return nil
}

// establishConnection creates a connection to podman
func (r *PodmanShellRunner) establishConnection(ctx context.Context) (context.Context, error) {
	slog.Debug("attempting to establish podman connection")

	// Get current user for socket paths
	currentUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	// Try macOS podman machine socket first
	macOSSocket := filepath.Join(currentUser.HomeDir, ".local/share/containers/podman/machine/podman.sock")
	slog.Debug("trying macOS podman socket", "socket", macOSSocket)
	if _, err := os.Stat(macOSSocket); err == nil {
		conn, err := bindings.NewConnection(ctx, "unix://"+macOSSocket)
		if err == nil {
			slog.Debug("successfully connected via macOS socket")
			return conn, nil
		}
		slog.Debug("failed to connect via macOS socket", "error", err)
	}

	// Try default connection (may work on some Linux setups)
	slog.Debug("trying default podman connection")
	conn, err := bindings.NewConnection(ctx, "")
	if err == nil {
		slog.Debug("successfully connected via default connection")
		return conn, nil
	}
	slog.Debug("failed to connect via default connection", "error", err)

	// Try user socket (rootless podman on Linux)
	userSocket := fmt.Sprintf("unix:///run/user/%s/podman/podman.sock", currentUser.Uid)
	slog.Debug("trying user socket", "socket", userSocket)
	conn, err = bindings.NewConnection(ctx, userSocket)
	if err != nil {
		// Try system socket (root podman on Linux)
		slog.Debug("trying system socket")
		conn, err = bindings.NewConnection(ctx, "unix:///var/run/podman/podman.sock")
		if err != nil {
			slog.Debug("failed to connect via system socket", "error", err)
			return nil, err
		}
	}

	slog.Debug("successfully connected via user/system socket")
	return conn, nil
}

// readStream continuously reads from a stream looking for command markers
// and populates the outputs map when complete command outputs are found
func (r *PodmanShellRunner) readStream(reader io.Reader) {
	slog.Debug("stream reader started")

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 1024*1024) // 1MB buffer
	scanner.Buffer(buf, 1024*1024)

	var currentID int
	var output strings.Builder
	inCommand := false

	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		slog.Debug("stream reader line", "line", line)

		// Check for start marker (format: __ASIMI_STDOUT_START:123)
		if strings.Contains(line, "__ASIMI_STDOUT_START:") {
			// Extract ID from marker by splitting on ':'
			// Format is "__ASIMI_STDOUT_START:ID"
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				if _, err := fmt.Sscanf(parts[1], "%d", &currentID); err == nil {
					inCommand = true
					output.Reset()
					slog.Debug("found start marker", "id", currentID)
					continue
				}
			}
		}

		// Check for end marker (format: __ASIMI_STDOUT_END:123:0)
		if inCommand && strings.HasPrefix(line, "__ASIMI_STDOUT_END:") {
			// Extract ID and exit code from marker by splitting on ':'
			// Format is "__ASIMI_STDOUT_END:ID:exitcode"
			parts := strings.Split(line, ":")
			var exitCode string
			if len(parts) >= 3 {
				exitCode = parts[2]
			}
			slog.Debug("found end marker", "id", currentID, "exitCode", exitCode)

			// Store output
			r.outputsMu.Lock()
			if cmd, exists := r.outputs[currentID]; exists {
				cmd.output = output.String()
				cmd.exitCode = exitCode
				cmd.outputDone = true
				close(cmd.ready)
				slog.Debug("command output complete", "id", currentID)
			}
			r.outputsMu.Unlock()

			inCommand = false
			currentID = 0
			output.Reset()
			continue
		}

		// Accumulate output
		if inCommand {
			if output.Len() > 0 {
				output.WriteString("\n")
			}
			output.WriteString(line)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error("stream reader error", "error", err)
	}

	// Clean up any pending commands when reader exits
	r.outputsMu.Lock()
	for id, cmd := range r.outputs {
		if !cmd.outputDone {
			cmd.outputDone = true
			slog.Debug("marking output done due to reader exit", "id", id)
			select {
			case <-cmd.ready:
				// Already closed
			default:
				close(cmd.ready)
				slog.Debug("closed ready channel due to reader exit", "id", id)
			}
		}
	}
	r.outputsMu.Unlock()

	slog.Debug("stream reader exited")
}

// createContainer creates and starts a new container
func (r *PodmanShellRunner) createContainer(ctx context.Context) error {
	slog.Debug("creating new container", "image", r.imageName, "containerName", r.containerName, "noCleanup", r.noCleanup)

	s := specgen.NewSpecGenerator(r.imageName, false)
	s.Name = r.containerName
	// Only auto-remove if not in no-cleanup mode
	autoRemove := !r.noCleanup
	s.Remove = &autoRemove
	if r.noCleanup {
		slog.Info("Container will NOT be auto-removed on exit (--no-cleanup flag set)")
	}

	// Enable TTY to support a pty, which merges stdout and stderr
	terminal := true
	s.Terminal = &terminal
	s.Env = map[string]string{"TERM": "dumb"}

	// Set up bash to read from stdin
	s.Command = []string{"bash", "-i"}
	stdinOpen := true
	s.Stdin = &stdinOpen

	// Mount project root at the same absolute path as on host
	absPath, err := filepath.Abs(r.repoInfo.ProjectRoot)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	slog.Debug("mounting directory to container", "source", absPath, "destination", absPath)

	mounts := []spec.Mount{
		{
			Type:        "bind",
			Source:      absPath,
			Destination: absPath,
		},
	}
	// Add additional mounts from config if available
	if r.config != nil {
		for _, m := range r.config.Container.AdditionalMounts {
			slog.Debug("adding additional mount", "source", m.Source, "destination", m.Destination)
			mounts = append(mounts, spec.Mount{
				Type:        "bind",
				Source:      m.Source,
				Destination: m.Destination,
			})
		}
	}

	s.Mounts = mounts

	// Create the container
	slog.Debug("calling CreateWithSpec")
	createResponse, err := containers.CreateWithSpec(r.conn, s, nil)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}
	slog.Debug("container created", "containerID", createResponse.ID)

	// Start the container
	slog.Debug("starting container", "containerID", createResponse.ID)
	if err := containers.Start(r.conn, createResponse.ID, nil); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	slog.Debug("container started successfully", "containerID", createResponse.ID)

	return nil
}

func (r *PodmanShellRunner) Run(ctx context.Context, params RunInShellInput) (RunInShellOutput, error) {
	slog.Debug("Run called", "command", params.Command)

	// Initialize if needed (connection, container, bash session)
	if err := r.initialize(ctx); err != nil {
		slog.Error("failed to initialize", "error", err)
		// If podman is not available, fall back to host shell only if allowed
		if r.allowFallback {
			slog.Debug("falling back to host shell")
			return hostRun(ctx, params)
		}
		// TODO: return a more general error for errors not matching:
		// "failed to create container: no such image: localhost/asimi-shell:latest: image not known"
		return RunInShellOutput{}, fmt.Errorf("Sandbox container image is missing. Did you run `:init` ?")
	}

	// Get next command ID
	r.outputsMu.Lock()
	id := r.nextCommandID
	r.nextCommandID++
	r.outputsMu.Unlock()

	slog.Debug("generated command ID", "id", id)

	// Register command in outputs map
	cmd := &commandOutput{
		ready: make(chan struct{}),
	}
	r.outputsMu.Lock()
	r.outputs[id] = cmd
	r.outputsMu.Unlock()

	// Quote the command using shellescape to preserve newlines (for heredocs) while escaping special chars
	// This prevents redirects from being parsed as function redirects
	command := fmt.Sprintf("__asimi_run %d %s\n", id, shellescape.Quote(params.Command))
	slog.Debug("wrapped command", "command", command)

	// Write the command to the persistent session's stdin
	slog.Debug("writing command to stdin")
	_, err := r.stdinPipe.Write([]byte(command))
	if err != nil {
		slog.Error("failed to write to stdin", "error", err)
		// Clean up map entry
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		return RunInShellOutput{}, fmt.Errorf("failed to write command to persistent session: %w", err)
	}
	slog.Debug("command written to stdin successfully")

	// Get timeout from config or use default of 2 minutes
	// TODO: move the default to config.go
	timeoutMinutes := 2
	if r.config != nil && r.config.RunInShell.TimeoutMinutes > 0 {
		timeoutMinutes = r.config.RunInShell.TimeoutMinutes
	}
	timeout := time.Duration(timeoutMinutes) * time.Minute
	slog.Debug("using timeout", "timeout", timeout)

	// Wait for output to be ready with timeout
	select {
	case <-cmd.ready:
		slog.Debug("command output ready", "id", id)
	case <-time.After(timeout):
		slog.Warn("timeout waiting for command output", "id", id, "cmd", params.Command, "timeout", timeout)
		// Clean up map entry
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()

		// Return timeout as command output, not as a harness error
		// This allows the LLM to see the timeout and handle it appropriately
		return RunInShellOutput{
			Output:   fmt.Sprintf("Command timed out after %v", timeout),
			ExitCode: "124", // Standard timeout exit code
		}, nil
	}

	// Retrieve output from map
	r.outputsMu.Lock()
	output := RunInShellOutput{
		Output:   cmd.output,
		ExitCode: cmd.exitCode,
	}
	// Clean up map entry
	delete(r.outputs, id)
	r.outputsMu.Unlock()

	slog.Debug("Run completed successfully")
	return output, nil
}

// Restart resets the container attachment to recover from connection errors.
// The container keeps running, we just close and clear the pipes so they'll be
// re-established on the next command.
func (r *PodmanShellRunner) Restart(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	slog.Info("restarting container attachment", "containerName", r.containerName)

	// Close existing pipes if they exist
	if r.stdinPipe != nil {
		r.stdinPipe.Close()
		r.stdinPipe = nil
	}
	if r.stdoutPipe != nil {
		r.stdoutPipe.Close()
		r.stdoutPipe = nil
	}

	// Clear any pending outputs
	r.outputsMu.Lock()
	for id, cmd := range r.outputs {
		select {
		case <-cmd.ready:
			// Already closed
		default:
			close(cmd.ready)
		}
		delete(r.outputs, id)
		slog.Debug("cleared pending command during restart", "id", id)
	}
	r.outputsMu.Unlock()

	slog.Info("container attachment restarted - will reconnect on next command")
	return nil
}

// Close closes the container attachment, stops and optionally removes the container.
func (r *PodmanShellRunner) Close(ctx context.Context) error {
	slog.Debug("closing podman shell runner", "noCleanup", r.noCleanup)

	r.mu.Lock()
	defer r.mu.Unlock()

	// Send exit command to bash before closing pipes
	if r.stdinPipe != nil {
		slog.Debug("sending exit command to bash")
		// Ignore errors here as the pipe might already be broken
		r.stdinPipe.Write([]byte("exit\n"))
		slog.Debug("closing stdin pipe")
		// TODO: try first exit the shell to solve the dangling containers bug
		r.stdinPipe.Close()
	}
	if r.stdoutPipe != nil {
		slog.Debug("closing stdout pipe")
		r.stdoutPipe.Close()
	}

	r.stdinPipe = nil
	r.stdoutPipe = nil

	// Stop and optionally remove the container if we have a connection
	if r.conn != nil && r.containerStarted {
		// Use a very short timeout since we sent exit to bash
		// If bash doesn't exit quickly, force kill the container
		timeout := uint(1)
		slog.Debug("stopping container", "containerName", r.containerName, "timeout", timeout)
		if err := containers.Stop(r.conn, r.containerName, &containers.StopOptions{Timeout: &timeout}); err != nil {
			slog.Debug("stop returned error (may already be stopped)", "error", err)
			// Don't return error, continue to try removal
		}

		// Only remove the container if not in no-cleanup mode
		if !r.noCleanup {
			slog.Debug("removing container", "containerName", r.containerName)
			force := true
			volumes := true
			if _, err := containers.Remove(r.conn, r.containerName, &containers.RemoveOptions{
				Force:   &force,
				Volumes: &volumes,
			}); err != nil {
				slog.Debug("remove returned error", "error", err)
				// Don't return error, just log it
			} else {
				slog.Debug("container removed successfully", "containerName", r.containerName)
			}
		} else {
			slog.Info("Container NOT removed (--no-cleanup flag set)", "containerName", r.containerName)
			slog.Info("To manually remove the container later, run:", "command", fmt.Sprintf("podman rm -f %s", r.containerName))
		}
	}

	// Reset containerStarted so the runner can be reused after Close()
	// This allows the container to be recreated on the next Run() call
	r.containerStarted = false

	slog.Debug("podman shell runner closed successfully")
	return nil
}
func (r *PodmanShellRunner) AllowFallback(allow bool) {
	r.allowFallback = allow
}

func (r *PodmanShellRunner) RunnerType() string {
	return "podman"
}

// ContainerID returns the container name if the container has been started
func (r *PodmanShellRunner) ContainerID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.containerStarted {
		return r.containerName
	}
	return ""
}
