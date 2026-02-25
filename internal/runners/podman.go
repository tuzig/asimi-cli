package runners

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

	"github.com/afittestide/asimi/internal/repo"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/specgen"
)

// PodmanRunner executes shell commands in a podman container
type PodmanRunner struct {
	imageName        string
	containerName    string
	allowFallback    bool
	noCleanup        bool
	config           *Config
	repoInfo         repo.RepoInfo
	msgChan          chan<- Msg
	fallback         Runner
	mu               sync.Mutex
	conn             context.Context
	containerStarted bool
	stdinPipe        io.WriteCloser
	stdoutPipe       io.ReadCloser
	outputs          map[int]*commandOutput
	outputsMu        sync.Mutex
	nextCommandID    int
}

type commandOutput struct {
	output     string
	exitCode   string
	ready      chan struct{}
	outputDone bool
}

// NewPodmanRunner creates a new PodmanRunner
func NewPodmanRunner(cfg *Config, repoInfo repo.RepoInfo, fallback Runner) *PodmanRunner {
	pid := os.Getpid()

	imageName := cfg.ImageName
	if imageName == "" {
		imageName = fmt.Sprintf("localhost/asimi-sandbox-%s:latest", repoInfo.Slug)
	}

	return &PodmanRunner{
		imageName:     imageName,
		containerName: fmt.Sprintf("asimi-shell-%d", pid),
		allowFallback: cfg.AllowHostFallback,
		noCleanup:     cfg.NoCleanup,
		config:        cfg,
		repoInfo:      repoInfo,
		fallback:      fallback,
		outputs:       make(map[int]*commandOutput),
		nextCommandID: 1,
	}
}

func (r *PodmanRunner) SetMessageChannel(msgChan chan<- Msg) {
	r.msgChan = msgChan
}

func (r *PodmanRunner) initialize(ctx context.Context) error {
	slog.Debug("initializing podman shell runner")

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

	r.mu.Lock()
	if !r.containerStarted {
		r.mu.Unlock()
		slog.Debug("ensuring container for this instance", "containerName", r.containerName)

		inspectData, err := containers.Inspect(r.conn, r.containerName, nil)
		if err == nil {
			if inspectData.State.Running {
				slog.Debug("container already running", "containerName", r.containerName)
				r.mu.Lock()
				r.containerStarted = true
				r.mu.Unlock()
			} else {
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
			slog.Debug("container doesn't exist, creating new one", "containerName", r.containerName)
			if err := r.createContainer(ctx); err != nil {
				return err
			}
			r.mu.Lock()
			r.containerStarted = true
			r.mu.Unlock()

			// Notify that container was launched
			if r.msgChan != nil {
				// TODO: add the process ID to the message
				r.msgChan <- ContainerLaunchedMsg{Message: "🐳 Container launched"}
			}
		}
	} else {
		r.mu.Unlock()
		slog.Debug("container already started, skipping checks", "containerName", r.containerName)
	}

	r.mu.Lock()
	hasAttachment := r.stdinPipe != nil
	r.mu.Unlock()

	if !hasAttachment {
		slog.Debug("attaching to container")

		stdinReader, stdinWriter := io.Pipe()
		stdoutReader, stdoutWriter := io.Pipe()

		go func() {
			slog.Debug("Attach goroutine started", "containerName", r.containerName)
			if err := containers.Attach(r.conn, r.containerName, stdinReader, stdoutWriter, nil, nil, nil); err != nil {
				slog.Error("error attaching to container", "error", err)
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

		go r.readStream(stdoutReader)

		slog.Debug("container attachment established", "repoInfo", r.repoInfo)

		var rc strings.Builder
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

func (r *PodmanRunner) establishConnection(ctx context.Context) (context.Context, error) {
	slog.Debug("attempting to establish podman connection")

	currentUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

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

	slog.Debug("trying default podman connection")
	conn, err := bindings.NewConnection(ctx, "")
	if err == nil {
		slog.Debug("successfully connected via default connection")
		return conn, nil
	}
	slog.Debug("failed to connect via default connection", "error", err)

	userSocket := fmt.Sprintf("unix:///run/user/%s/podman/podman.sock", currentUser.Uid)
	slog.Debug("trying user socket", "socket", userSocket)
	conn, err = bindings.NewConnection(ctx, userSocket)
	if err != nil {
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

func (r *PodmanRunner) readStream(reader io.Reader) {
	slog.Debug("stream reader started")

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	var currentID int
	var output strings.Builder
	inCommand := false

	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		slog.Debug("stream reader line", "line", line)

		if strings.Contains(line, "__ASIMI_STDOUT_START:") {
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

		if inCommand && strings.HasPrefix(line, "__ASIMI_STDOUT_END:") {
			parts := strings.Split(line, ":")
			var exitCode string
			if len(parts) >= 3 {
				exitCode = parts[2]
			}
			slog.Debug("found end marker", "id", currentID, "exitCode", exitCode)

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

	r.outputsMu.Lock()
	for id, cmd := range r.outputs {
		if !cmd.outputDone {
			cmd.outputDone = true
			slog.Debug("marking output done due to reader exit", "id", id)
			select {
			case <-cmd.ready:
			default:
				close(cmd.ready)
				slog.Debug("closed ready channel due to reader exit", "id", id)
			}
		}
	}
	r.outputsMu.Unlock()

	slog.Debug("stream reader exited")
}

func (r *PodmanRunner) createContainer(ctx context.Context) error {
	slog.Debug("creating new container", "image", r.imageName, "containerName", r.containerName, "noCleanup", r.noCleanup)

	s := specgen.NewSpecGenerator(r.imageName, false)
	s.Name = r.containerName
	autoRemove := !r.noCleanup
	s.Remove = &autoRemove
	if r.noCleanup {
		slog.Info("Container will NOT be auto-removed on exit (--no-cleanup flag set)")
	}

	terminal := true
	s.Terminal = &terminal
	s.Env = map[string]string{"TERM": "dumb"}

	s.Command = []string{"bash", "-i"}
	stdinOpen := true
	s.Stdin = &stdinOpen

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

	for _, m := range r.config.AdditionalMounts {
		slog.Debug("adding additional mount", "source", m.Source, "destination", m.Destination)
		mounts = append(mounts, spec.Mount{
			Type:        "bind",
			Source:      m.Source,
			Destination: m.Destination,
		})
	}

	s.Mounts = mounts

	slog.Debug("calling CreateWithSpec")
	createResponse, err := containers.CreateWithSpec(r.conn, s, nil)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}
	slog.Debug("container created", "containerID", createResponse.ID)

	slog.Debug("starting container", "containerID", createResponse.ID)
	if err := containers.Start(r.conn, createResponse.ID, nil); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	slog.Debug("container started successfully", "containerID", createResponse.ID)

	return nil
}

func (r *PodmanRunner) Run(ctx context.Context, input Input) (Output, error) {
	slog.Debug("Run called", "command", input.Command)

	if err := r.initialize(ctx); err != nil {
		slog.Error("failed to initialize", "error", err)
		if r.allowFallback && r.fallback != nil {
			slog.Debug("falling back to host shell")
			return r.fallback.Run(ctx, input)
		}
		return Output{}, SandboxMissingError{}
	}

	r.outputsMu.Lock()
	id := r.nextCommandID
	r.nextCommandID++
	r.outputsMu.Unlock()

	slog.Debug("generated command ID", "id", id)

	cmd := &commandOutput{
		ready: make(chan struct{}),
	}
	r.outputsMu.Lock()
	r.outputs[id] = cmd
	r.outputsMu.Unlock()

	command := fmt.Sprintf("__asimi_run %d %s\n", id, shellescape.Quote(input.Command))
	slog.Debug("wrapped command", "command", command)

	slog.Debug("writing command to stdin")
	_, err := r.stdinPipe.Write([]byte(command))
	if err != nil {
		slog.Error("failed to write to stdin", "error", err)
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		return Output{}, fmt.Errorf("failed to write command to persistent session: %w", err)
	}
	slog.Debug("command written to stdin successfully")

	timeoutMinutes := 2
	if r.config.TimeoutMinutes > 0 {
		timeoutMinutes = r.config.TimeoutMinutes
	}
	timeout := time.Duration(timeoutMinutes) * time.Minute
	slog.Debug("using timeout", "timeout", timeout)

	select {
	case <-cmd.ready:
		slog.Debug("command output ready", "id", id)
	case <-time.After(timeout):
		slog.Warn("timeout waiting for command output", "id", id, "cmd", input.Command, "timeout", timeout)
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()

		return Output{
			Output:   fmt.Sprintf("Command timed out after %v", timeout),
			ExitCode: "124",
		}, nil
	}

	r.outputsMu.Lock()
	output := Output{
		Output:   cmd.output,
		ExitCode: cmd.exitCode,
	}
	delete(r.outputs, id)
	r.outputsMu.Unlock()

	slog.Debug("Run completed successfully")
	return output, nil
}

func (r *PodmanRunner) Restart(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	slog.Info("restarting container attachment", "containerName", r.containerName)

	if r.stdinPipe != nil {
		r.stdinPipe.Close()
		r.stdinPipe = nil
	}
	if r.stdoutPipe != nil {
		r.stdoutPipe.Close()
		r.stdoutPipe = nil
	}

	r.outputsMu.Lock()
	for id, cmd := range r.outputs {
		select {
		case <-cmd.ready:
		default:
			close(cmd.ready)
		}
		delete(r.outputs, id)
		slog.Debug("cleared pending command during restart", "id", id)
	}
	r.outputsMu.Unlock()

	// Request scheduler clear via message channel
	if r.msgChan != nil {
		resultChan := make(chan int, 1)
		r.msgChan <- ClearSchedulerMsg{ResultChan: resultChan}
		abortedCount := <-resultChan
		if abortedCount > 0 {
			slog.Info("aborted pending tool calls during restart", "count", abortedCount)
		}
	}

	slog.Info("container attachment restarted - will reconnect on next command")
	return nil
}

func (r *PodmanRunner) Close(ctx context.Context) error {
	slog.Debug("closing podman shell runner", "noCleanup", r.noCleanup)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stdinPipe != nil {
		slog.Debug("sending exit command to bash")
		r.stdinPipe.Write([]byte("exit\n"))
		slog.Debug("closing stdin pipe")
		r.stdinPipe.Close()
	}
	if r.stdoutPipe != nil {
		slog.Debug("closing stdout pipe")
		r.stdoutPipe.Close()
	}

	r.stdinPipe = nil
	r.stdoutPipe = nil

	if r.conn != nil && r.containerStarted {
		timeout := uint(1)
		slog.Debug("stopping container", "containerName", r.containerName, "timeout", timeout)
		if err := containers.Stop(r.conn, r.containerName, &containers.StopOptions{Timeout: &timeout}); err != nil {
			slog.Debug("stop returned error (may already be stopped)", "error", err)
		}

		if !r.noCleanup {
			slog.Debug("removing container", "containerName", r.containerName)
			force := true
			volumes := true
			if _, err := containers.Remove(r.conn, r.containerName, &containers.RemoveOptions{
				Force:   &force,
				Volumes: &volumes,
			}); err != nil {
				slog.Debug("remove returned error", "error", err)
			} else {
				slog.Debug("container removed successfully", "containerName", r.containerName)
			}
		} else {
			slog.Info("Container NOT removed (--no-cleanup flag set)", "containerName", r.containerName)
			slog.Info("To manually remove the container later, run:", "command", fmt.Sprintf("podman rm -f %s", r.containerName))
		}
	}

	r.containerStarted = false

	slog.Debug("podman shell runner closed successfully")
	return nil
}

func (r *PodmanRunner) RunnerType() string {
	return "podman"
}

// ContainerID returns the container name if the container has been started
func (r *PodmanRunner) ContainerID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.containerStarted {
		return r.containerName
	}
	return ""
}

// AllowFallback enables or disables fallback to host runner
func (r *PodmanRunner) AllowFallback(allow bool) {
	r.allowFallback = allow
}
