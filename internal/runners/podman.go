package runners

import (
	"bufio"
	"context"
	"crypto/md5"
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
	"go.podman.io/podman/v6/pkg/bindings"
	"go.podman.io/podman/v6/pkg/bindings/containers"
	"go.podman.io/podman/v6/pkg/specgen"
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
	establishConn    func(ctx context.Context) (context.Context, error)
	checkImage       func(ctx context.Context) error
	containerStarted bool
	stdinPipe        io.WriteCloser
	stdoutPipe       io.ReadCloser
	outputs          map[int]*commandOutput
	outputsMu        sync.Mutex
	nextCommandID    int
	readStreamStop   chan struct{}
}

type commandOutput struct {
	output     string
	exitCode   string
	ready      chan struct{}
	outputDone bool
	closed     bool
}

// healthcheckTimeout is the maximum time to wait for a healthcheck response.
const healthcheckTimeout = 5 * time.Second

// closeReady safely closes cmd.ready exactly once under outputsMu.
// It replaces all bare close(cmd.ready) and select-guarded close patterns.
func (r *PodmanRunner) closeReady(cmd *commandOutput) {
	r.outputsMu.Lock()
	defer r.outputsMu.Unlock()
	if cmd.closed {
		return
	}
	cmd.closed = true
	close(cmd.ready)
}

// NewPodmanRunner creates a new PodmanRunner
func NewPodmanRunner(cfg *Config, repoInfo repo.RepoInfo, connID uint64, fallback Runner) *PodmanRunner {
	imageName := cfg.ImageName
	if imageName == "" {
		imageName = fmt.Sprintf("localhost/asimi/sandbox/%s:latest", repoInfo.Slug)
	}

	containerName := fmt.Sprintf("asimi-shell-%s-%d", repo.SanitizeSegment(repoInfo.Slug), connID)
	slog.Debug("NewPodmanRunner", "containerName", containerName, "imageName", imageName, "slug", repoInfo.Slug)

	r := &PodmanRunner{
		imageName:     imageName,
		containerName: containerName,
		allowFallback: cfg.AllowHostFallback,
		noCleanup:     cfg.NoCleanup,
		config:        cfg,
		repoInfo:      repoInfo,
		fallback:      fallback,
		outputs:       make(map[int]*commandOutput),
		nextCommandID: 1,
	}
	r.establishConn = r.establishConnection
	r.checkImage = r.defaultCheckImage
	return r
}

func (r *PodmanRunner) SetMessageChannel(msgChan chan<- Msg) {
	r.msgChan = msgChan
}

// teardownAttachment closes and nils stdinPipe/stdoutPipe, stops the
// readStream goroutine, and resets containerStarted — everything needed
// so initialize() can re-attach from a clean state on recursion.
func (r *PodmanRunner) teardownAttachment() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.readStreamStop != nil {
		close(r.readStreamStop)
		r.readStreamStop = nil
	}
	if r.stdinPipe != nil {
		r.stdinPipe.Close()
		r.stdinPipe = nil
	}
	if r.stdoutPipe != nil {
		r.stdoutPipe.Close()
		r.stdoutPipe = nil
	}
	r.containerStarted = false
}

func (r *PodmanRunner) initialize(ctx context.Context) error {
	slog.Debug("initializing podman shell runner")

	r.mu.Lock()
	hasConnection := r.conn != nil
	containerStarted := r.containerStarted
	r.mu.Unlock()

	if !hasConnection || !containerStarted {
		if err := r.preflightSandbox(ctx); err != nil {
			return err
		}
	}

	if !hasConnection {
		if err := r.preflightSandbox(ctx); err != nil {
			return err
		}

		slog.Debug("establishing podman connection")
		conn, err := r.establishConn(context.Background())
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}
		r.mu.Lock()
		r.conn = conn
		r.mu.Unlock()
		slog.Debug("podman connection established")
	} else if r.conn.Err() != nil {
		slog.Info("podman connection cancelled, re-establishing", "error", r.conn.Err())
		r.mu.Lock()
		r.conn = nil
		r.mu.Unlock()
		hasConnection = false

		conn, err := r.establishConn(context.Background())
		if err != nil {
			return fmt.Errorf("failed to reconnect to podman: %w", err)
		}
		r.mu.Lock()
		r.conn = conn
		r.mu.Unlock()
		slog.Debug("podman connection re-established")
	}

	existingRunning := false

	// Track this so we run a healthcheck to verify the container is actually alive.
	containerWasAlreadyStarted := false

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
				existingRunning = true
				r.mu.Unlock()
				r.sendContainerLaunched(fmt.Sprintf("%d", inspectData.State.Pid))
			} else {
				slog.Debug("starting existing container", "containerName", r.containerName)
				if err := containers.Start(r.conn, r.containerName, nil); err != nil {
					return fmt.Errorf("failed to start existing container: %w", err)
				}
				slog.Debug("existing container started", "containerName", r.containerName)
				// Re-inspect after Start to get the actual PID — the pre-Start
				// inspectData has Pid=0 because the container was stopped.
				startedInspect, err := containers.Inspect(r.conn, r.containerName, nil)
				if err != nil {
					return fmt.Errorf("failed to inspect started container: %w", err)
				}
				r.mu.Lock()
				r.containerStarted = true
				r.mu.Unlock()
				r.sendContainerLaunched(fmt.Sprintf("%d", startedInspect.State.Pid))
			}
		} else {
			slog.Debug("container doesn't exist, creating new one", "containerName", r.containerName)
			containerID, err := r.createContainer(ctx)
			if err != nil {
				return err
			}
			r.mu.Lock()
			r.containerStarted = true
			r.mu.Unlock()

			r.sendContainerLaunched(containerID)
		}
	} else {
		// Even if we have an active attachment, the container may have been
		// stopped externally (e.g., podman stop). Verify it's still running.
		r.mu.Unlock()

		inspectData, err := containers.Inspect(r.conn, r.containerName, nil)
		if err != nil {
			slog.Info("container inspect failed on fast path, resetting", "error", err)
			r.teardownAttachment()
			return r.initialize(ctx)
		}
		if !inspectData.State.Running {
			slog.Info("container stopped externally, resetting for re-creation", "containerName", r.containerName)
			r.teardownAttachment()
			return r.initialize(ctx)
		}

		// Container is running. Check if we need to re-attach.
		r.mu.Lock()
		if r.stdinPipe == nil {
			containerWasAlreadyStarted = true
		}
		r.mu.Unlock()
		slog.Debug("container already started, skipping checks", "containerName", r.containerName)
	}

	r.mu.Lock()
	hasAttachment := r.stdinPipe != nil
	r.mu.Unlock()

	if !hasAttachment {
		slog.Debug("attaching to container")

		// Signal any prior readStream goroutine to stop
		r.mu.Lock()
		if r.readStreamStop != nil {
			close(r.readStreamStop)
			r.readStreamStop = nil
		}
		stopChan := make(chan struct{})
		r.readStreamStop = stopChan
		r.mu.Unlock()

		stdinReader, stdinWriter := io.Pipe()
		stdoutReader, stdoutWriter := io.Pipe()

		go func() {
			slog.Debug("Attach goroutine started", "containerName", r.containerName)
			if err := containers.Attach(r.conn, r.containerName, stdinReader, stdoutWriter, nil, nil, nil); err != nil {
				slog.Error("error attaching to container", "error", err)
				stdinReader.Close()
				stdoutWriter.Close()
				r.mu.Lock()
				if r.stdinPipe == stdinWriter {
					r.stdinPipe = nil
					r.stdoutPipe = nil
					r.containerStarted = false // Force re-inspection on next initialize()
				}
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

		go r.readStream(stdoutReader, stopChan)

		slog.Debug("container attachment established", "repoInfo", r.repoInfo)

		// Healthcheck: when reusing an already-running container OR re-attaching
		// to a container we believe was already started (but pipes were nil).
		if existingRunning || containerWasAlreadyStarted {
			if err := r.healthcheck(ctx); err != nil {
				slog.Info("container unhealthy, force-killing and recreating", "containerName", r.containerName, "error", err)

				// Force-remove the stale container
				forceTrue := true
				volumesTrue := true
				if _, rmErr := containers.Remove(r.conn, r.containerName, &containers.RemoveOptions{Force: &forceTrue, Volumes: &volumesTrue}); rmErr != nil {
					return fmt.Errorf("failed to remove unhealthy container: %w", rmErr)
				}

				// Close and nil out pipes under lock to avoid data race
				// with the Attach goroutine's error handler
				r.mu.Lock()
				if r.stdinPipe != nil {
					r.stdinPipe.Close()
					r.stdinPipe = nil
				}
				if r.stdoutPipe != nil {
					r.stdoutPipe.Close()
					r.stdoutPipe = nil
				}
				r.containerStarted = false
				r.mu.Unlock()

				// Notify TUI
				if r.msgChan != nil {
					r.msgChan <- SandboxUnhealthyMsg{
						Message:       "🔄 Stale container detected and recreated",
						ContainerName: r.containerName,
					}
				}

				// Re-initialize: will fall into the createContainer path
				return r.initialize(ctx)
			}
			slog.Info("existing container is healthy", "containerName", r.containerName)
		}

		var rc strings.Builder
		rc.WriteString("git config --global core.pager cat\n")
		if r.repoInfo.WorktreePath != "" {
			rc.WriteString(fmt.Sprintf("cd %s/%s\n", r.repoInfo.ProjectRoot, r.repoInfo.WorktreePath))
		} else {
			rc.WriteString(fmt.Sprintf("cd %s\n", r.repoInfo.ProjectRoot))
		}
		slog.Debug("navigating to path in the container", "path", r.repoInfo.WorktreePath)

		// Capture stdinPipe under lock so the write goroutine uses a stable reference
		r.mu.Lock()
		stdinPipe := r.stdinPipe
		r.mu.Unlock()

		if stdinPipe == nil {
			slog.Error("stdinPipe is nil, cannot send rc-commands")
			return fmt.Errorf("stdinPipe is nil during initialization")
		}

		// Protect the rc-commands write with context — the io.Pipe write can block
		// if the containers.Attach goroutine is hung
		writeDone := make(chan error, 1)
		go func() {
			_, err := stdinPipe.Write([]byte(rc.String()))
			writeDone <- err
		}()
		select {
		case err := <-writeDone:
			if err != nil {
				slog.Error("failed to navigate to worktree", "error", err)
				return fmt.Errorf("attachment failed: rc-commands write: %w", err)
			}
		case <-ctx.Done():
			slog.Error("context cancelled during rc-commands write", "error", ctx.Err())
			return fmt.Errorf("attachment failed: rc-commands write cancelled: %w", ctx.Err())
		}
	}

	slog.Debug("initialization complete")
	return nil
}

func (r *PodmanRunner) preflightSandbox(ctx context.Context) error {
	if err := r.checkSandboxFiles(); err != nil {
		return err
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return r.checkImage(probeCtx)
}

// defaultCheckImage checks that the sandbox image exists in podman.
func (r *PodmanRunner) defaultCheckImage(ctx context.Context) error {
	return CheckSandboxImageAvailable(ctx, r.imageName, r.repoInfo.ProjectRoot)
}

func (r *PodmanRunner) checkSandboxFiles() error {
	root := r.projectWorkingRoot()
	if root == "" {
		return nil
	}

	required := []string{
		".agents/sandbox",
		".agents/sandbox/Dockerfile",
		".agents/sandbox/bashrc",
	}
	for _, rel := range required {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			return SandboxSetupMissingError{}
		}
	}

	return nil
}

func (r *PodmanRunner) projectWorkingRoot() string {
	if r.repoInfo.ProjectRoot == "" {
		return ""
	}
	if r.repoInfo.WorktreePath != "" {
		return filepath.Join(r.repoInfo.ProjectRoot, r.repoInfo.WorktreePath)
	}
	return r.repoInfo.ProjectRoot
}

func (r *PodmanRunner) healthcheck(ctx context.Context) error {
	r.outputsMu.Lock()
	id := r.nextCommandID
	r.nextCommandID++
	r.outputsMu.Unlock()

	cmd := &commandOutput{
		ready: make(chan struct{}),
	}
	r.outputsMu.Lock()
	r.outputs[id] = cmd
	r.outputsMu.Unlock()

	command := fmt.Sprintf("__asimi_run %d 'echo __ASIMI_HEALTHY'\n", id)

	r.mu.Lock()
	stdinPipe := r.stdinPipe
	r.mu.Unlock()

	if stdinPipe == nil {
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		return fmt.Errorf("healthcheck: stdinPipe is nil")
	}

	writeDone := make(chan error, 1)
	go func() {
		_, err := stdinPipe.Write([]byte(command))
		writeDone <- err
	}()

	select {
	case err := <-writeDone:
		if err != nil {
			r.outputsMu.Lock()
			delete(r.outputs, id)
			r.outputsMu.Unlock()
			return fmt.Errorf("healthcheck: failed to write probe: %w", err)
		}
	case <-ctx.Done():
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		return fmt.Errorf("healthcheck: context cancelled during write: %w", ctx.Err())
	case <-time.After(healthcheckTimeout):
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		return fmt.Errorf("healthcheck: write timeout after %v", healthcheckTimeout)
	}

	select {
	case <-cmd.ready:
		r.outputsMu.Lock()
		exitCode := cmd.exitCode
		delete(r.outputs, id)
		r.outputsMu.Unlock()

		if exitCode != "0" {
			return fmt.Errorf("healthcheck: probe exited with code %s", exitCode)
		}
		slog.Info("healthcheck passed")
		return nil
	case <-ctx.Done():
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		return fmt.Errorf("healthcheck: context cancelled: %w", ctx.Err())
	case <-time.After(healthcheckTimeout):
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		slog.Info("healthcheck failed: timeout")
		return fmt.Errorf("healthcheck: container shell not responding after %v", healthcheckTimeout)
	}
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
		conn, err := bindings.NewConnection(context.Background(), "unix://"+macOSSocket)
		if err == nil {
			slog.Debug("successfully connected via macOS socket")
			return conn, nil
		}
		slog.Debug("failed to connect via macOS socket", "error", err)
	}

	slog.Debug("trying default podman connection")
	if conn, err := bindings.NewConnection(context.Background(), ""); err == nil {
		slog.Debug("successfully connected via default connection")
		return conn, nil
	} else {
		slog.Debug("failed to connect via default connection", "error", err)
	}

	userSocket := fmt.Sprintf("unix:///run/user/%s/podman/podman.sock", currentUser.Uid)
	slog.Debug("trying user socket", "socket", userSocket)
	if conn, err := bindings.NewConnection(context.Background(), userSocket); err == nil {
		slog.Debug("successfully connected via user socket")
		return conn, nil
	}

	slog.Debug("trying system socket")
	conn, err := bindings.NewConnection(context.Background(), "unix:///var/run/podman/podman.sock")
	if err != nil {
		slog.Debug("failed to connect via system socket", "error", err)
		return nil, err
	}

	slog.Debug("successfully connected via system socket")
	return conn, nil
}

func (r *PodmanRunner) readStream(reader io.Reader, stopChan <-chan struct{}) {
	slog.Debug("stream reader started")

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	var currentID int
	var output strings.Builder
	inCommand := false

	scanner.Split(bufio.ScanLines)

	// Scan loop: break on stop signal or scanner exhaustion
	stopped := false
	for !stopped {
		select {
		case <-stopChan:
			slog.Debug("stream reader received stop signal")
			stopped = true
			continue
		default:
		}

		if !scanner.Scan() {
			break
		}

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

		if inCommand && strings.Contains(line, "__ASIMI_STDOUT_END:") {
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
				if !cmd.closed {
					cmd.closed = true
					close(cmd.ready)
				}
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
			if !cmd.closed {
				cmd.closed = true
				close(cmd.ready)
				slog.Debug("closed ready channel due to reader exit", "id", id)
			}
		}
	}
	r.outputsMu.Unlock()

	slog.Debug("stream reader exited")
}

func (r *PodmanRunner) createContainer(ctx context.Context) (string, error) {
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
	for _, name := range r.config.PassthroughEnv {
		if val, ok := os.LookupEnv(name); ok {
			s.Env[name] = val
		}
	}
	// This provides access to all the host's ports
	s.NetNS = specgen.Namespace{NSMode: specgen.Host}
	s.Command = []string{"bash", "-i"}
	stdinOpen := true
	s.Stdin = &stdinOpen

	absPath, err := filepath.Abs(r.repoInfo.ProjectRoot)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
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

	for _, relPath := range r.config.PlatformOverlays {
		overlayDest := filepath.Join(absPath, relPath)
		info, err := os.Stat(overlayDest)
		if err != nil {
			slog.Warn("platform overlay host path does not exist", "path", overlayDest, "overlay", relPath)
			continue
		}
		if info.IsDir() {
			volumeName := fmt.Sprintf("asimi-overlay-%s-%s", md5Hash(absPath), sanitizePath(relPath))
			slog.Debug("adding directory overlay as named volume", "volume", volumeName, "destination", overlayDest)
			s.Volumes = append(s.Volumes, &specgen.NamedVolume{
				Name: volumeName,
				Dest: overlayDest,
			})
		} else {
			overlayDataDir, err := overlayFileDir(absPath)
			if err != nil {
				return "", fmt.Errorf("failed to create overlay data directory: %w", err)
			}
			overlayFilePath := filepath.Join(overlayDataDir, sanitizePath(relPath))
			if _, err := os.Stat(overlayFilePath); err != nil {
				if err := os.WriteFile(overlayFilePath, nil, 0644); err != nil {
					return "", fmt.Errorf("failed to create overlay file: %w", err)
				}
			}
			slog.Debug("adding file overlay as bind mount", "source", overlayFilePath, "destination", overlayDest)
			mounts = append(mounts, spec.Mount{
				Type:        "bind",
				Source:      overlayFilePath,
				Destination: overlayDest,
			})
		}
	}

	s.Mounts = mounts

	slog.Debug("calling CreateWithSpec")
	createResponse, err := containers.CreateWithSpec(r.conn, s, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}
	slog.Debug("container created", "containerID", createResponse.ID)

	slog.Debug("starting container", "containerID", createResponse.ID)
	if err := containers.Start(r.conn, createResponse.ID, nil); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}
	slog.Debug("container started successfully", "containerID", createResponse.ID)

	return createResponse.ID, nil
}

// sendContainerLaunched notifies the TUI that a container is running,
// including the container's PID or ID for the status bar indicator.
func (r *PodmanRunner) sendContainerLaunched(containerID string) {
	if r.msgChan != nil {
		r.msgChan <- ContainerLaunchedMsg{
			Message:     "🐳 Container launched",
			ContainerID: containerID,
		}
	}
}

func (r *PodmanRunner) Run(ctx context.Context, input Input) (Output, error) {
	slog.Debug("Run called", "command", input.Command)

	if err := r.initialize(ctx); err != nil {
		slog.Error("failed to initialize", "error", err)
		if r.allowFallback && r.fallback != nil {
			slog.Warn("sandbox unavailable, falling back to host shell", "command", input.Command)
			out, fallbackErr := r.fallback.Run(ctx, input)
			return out, SandboxFallbackError{Err: err, FallbackErr: fallbackErr}
		}
		return Output{}, err
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

	// Capture stdinPipe under lock so the write goroutine uses a stable reference
	r.mu.Lock()
	stdinPipe := r.stdinPipe
	r.mu.Unlock()

	if stdinPipe == nil {
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		return Output{}, fmt.Errorf("failed to write command: stdinPipe is nil")
	}

	writeDone := make(chan error, 1)
	go func() {
		_, err := stdinPipe.Write([]byte(command))
		writeDone <- err
	}()

	timeoutMinutes := 10
	if r.config.TimeoutMinutes > 0 {
		timeoutMinutes = r.config.TimeoutMinutes
	}
	timeout := time.Duration(timeoutMinutes) * time.Minute
	slog.Debug("using timeout", "timeout", timeout)

	// Wait for write to complete (or timeout/context cancellation)
	select {
	case err := <-writeDone:
		if err != nil {
			slog.Error("failed to write to stdin", "error", err)
			r.outputsMu.Lock()
			delete(r.outputs, id)
			r.outputsMu.Unlock()
			return Output{}, fmt.Errorf("failed to write command to persistent session: %w", err)
		}
		slog.Debug("command written to stdin successfully")
	case <-ctx.Done():
		slog.Debug("context cancelled during write", "id", id, "cmd", input.Command)
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		return Output{
			Output:   "Command cancelled",
			ExitCode: "130",
		}, ctx.Err()
	case <-time.After(timeout):
		slog.Warn("timeout during write", "id", id, "cmd", input.Command, "timeout", timeout)
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()
		return Output{
			Output:   fmt.Sprintf("Command timed out after %v", timeout),
			ExitCode: "124",
		}, nil
	}

	// Wait for command output
	select {
	case <-cmd.ready:
		slog.Debug("command output ready", "id", id)
	case <-ctx.Done():
		slog.Debug("context cancelled waiting for command output", "id", id, "cmd", input.Command)
		r.outputsMu.Lock()
		delete(r.outputs, id)
		r.outputsMu.Unlock()

		return Output{
			Output:   "Command cancelled",
			ExitCode: "130",
		}, ctx.Err()
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

	// Signal the readStream goroutine to stop
	if r.readStreamStop != nil {
		close(r.readStreamStop)
		r.readStreamStop = nil
	}

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
		if !cmd.closed {
			cmd.closed = true
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

	// Signal the readStream goroutine to stop
	if r.readStreamStop != nil {
		close(r.readStreamStop)
		r.readStreamStop = nil
	}

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

func (r *PodmanRunner) GetOS() string {
	return "linux"
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

// GetImageName returns the sandbox image name
func (r *PodmanRunner) GetImageName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.imageName
}

// AllowFallback enables or disables fallback to host runner
func (r *PodmanRunner) AllowFallback(allow bool) {
	r.allowFallback = allow
}

// HealthCheck verifies the sandbox container is healthy by running uname
// and checking that "Linux" appears in the output.
func (r *PodmanRunner) HealthCheck(ctx context.Context) error {
	result, err := r.Run(ctx, Input{
		Command:     "uname",
		Description: "sandbox health check",
	})
	if err != nil {
		return fmt.Errorf("sandbox health check failed: %w", err)
	}
	if result.ExitCode != "0" {
		return fmt.Errorf("sandbox health check failed: uname exited with %s", result.ExitCode)
	}
	if !strings.Contains(result.Output, "Linux") {
		return fmt.Errorf("sandbox health check failed: expected Linux in output, got: %s", result.Output)
	}
	return nil
}

// md5Hash returns the hex MD5 hash of s.
func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

// overlayFileDir computes and creates the per-platform data directory for file overlays.
func overlayFileDir(absPath string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	dir := filepath.Join(homeDir, ".local", "share", "asimi", "overlays", md5Hash(absPath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create overlay data directory: %w", err)
	}
	return dir, nil
}

// sanitizePath replaces non-alphanumeric characters with underscores.
func sanitizePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
