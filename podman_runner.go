//go:build !ignore
// +build !ignore

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

	dockerContainer "github.com/docker/docker/api/types/container"
	spec "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/containers/podman/v5/pkg/api/handlers"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/specgen"
)

type PodmanShellRunner struct {
	imageName     string
	containerName string
	allowFallback bool
	mu            sync.Mutex
	conn          context.Context
	// New fields for persistent session
	execSessionID string
	stdinPipe     io.WriteCloser
	stdoutPipe    io.ReadCloser
	stderrPipe    io.ReadCloser
}

func newPodmanShellRunner(allowFallback bool) *PodmanShellRunner {
	return &PodmanShellRunner{
		imageName:     "localhost/asimi-shell:latest",
		containerName: "asimi-shell-workspace",
		allowFallback: allowFallback,
		execSessionID: "",
		stdinPipe:     nil,
		stdoutPipe:    nil,
		stderrPipe:    nil,
	}
}

// ensureConnection ensures we have a connection to podman
func (r *PodmanShellRunner) ensureConnection(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn != nil {
		slog.Debug("podman connection already established")
		return nil
	}

	slog.Debug("attempting to establish podman connection")

	// Get current user for socket paths
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	// Try macOS podman machine socket first
	macOSSocket := filepath.Join(currentUser.HomeDir, ".local/share/containers/podman/machine/podman.sock")
	slog.Debug("trying macOS podman socket", "socket", macOSSocket)
	if _, err := os.Stat(macOSSocket); err == nil {
		conn, err := bindings.NewConnection(ctx, "unix://"+macOSSocket)
		if err == nil {
			slog.Debug("successfully connected via macOS socket")
			r.conn = conn
			return nil
		}
		slog.Debug("failed to connect via macOS socket", "error", err)
	}

	// Try default connection (may work on some Linux setups)
	slog.Debug("trying default podman connection")
	conn, err := bindings.NewConnection(ctx, "")
	if err == nil {
		slog.Debug("successfully connected via default connection")
		r.conn = conn
		return nil
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
			return fmt.Errorf("failed to connect to podman: %w", err)
		}
	}

	slog.Debug("successfully connected via user/system socket")
	r.conn = conn
	return nil
}

// ensureContainer ensures the container is running
func (r *PodmanShellRunner) ensureContainer(ctx context.Context) error {
	slog.Debug("ensuring container is running", "containerName", r.containerName)

	if err := r.ensureConnection(ctx); err != nil {
		return err
	}

	// Check if container exists and is running
	slog.Debug("inspecting container", "containerName", r.containerName)
	inspectData, err := containers.Inspect(r.conn, r.containerName, nil)
	if err == nil {
		// Container exists, check if it's running
		if inspectData.State.Running {
			slog.Debug("container is already running")
			return nil
		}

		// Container exists but not running, start it
		slog.Debug("starting stopped container")
		if err := containers.Start(r.conn, r.containerName, nil); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		slog.Debug("container started successfully")
		return nil
	}

	// Container doesn't exist, create and start it
	slog.Debug("container does not exist, creating new container")
	return r.createContainer(ctx)
}

// ensurePersistentBashSession ensures a persistent interactive bash session is established
func (r *PodmanShellRunner) ensurePersistentBashSession(ctx context.Context) error {
	slog.Debug("ensuring persistent bash session is established")

	// Ensure container is running BEFORE acquiring the mutex
	slog.Debug("ensuring container is running from ensurePersistentBashSession")
	if err := r.ensureContainer(ctx); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.execSessionID != "" {
		// Session already established
		slog.Debug("bash session already established", "sessionID", r.execSessionID)
		return nil
	}

	slog.Debug("creating new bash session")

	// Create exec configuration for an interactive bash session
	slog.Debug("creating exec configuration for interactive bash")
	execConfig := &handlers.ExecCreateConfig{
		ExecOptions: dockerContainer.ExecOptions{
			Cmd:          []string{"bash", "-i"}, // Start interactive bash
			WorkingDir:   "/workspace",
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          true, // Enable TTY for interactive session
		},
	}

	// Create the exec session
	slog.Debug("calling ExecCreate")
	sessionID, err := containers.ExecCreate(r.conn, r.containerName, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create persistent exec session: %w", err)
	}
	slog.Debug("exec session created", "sessionID", sessionID)

	// Create pipes for stdin, stdout, and stderr
	slog.Debug("creating pipes for stdin, stdout, stderr")
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	execStartOptions := new(containers.ExecStartAndAttachOptions)
	execStartOptions.WithInputStream(*bufio.NewReader(stdinReader))
	execStartOptions.WithOutputStream(stdoutWriter)
	execStartOptions.WithErrorStream(stderrWriter)
	execStartOptions.WithAttachInput(true)
	execStartOptions.WithAttachOutput(true)
	execStartOptions.WithAttachError(true)
	// execStartOptions.WithTty(true) // Ensure TTY is enabled for the attachment

	// Start the exec session in a goroutine so it doesn't block
	slog.Debug("starting ExecStartAndAttach goroutine")
	go func() {
		slog.Debug("ExecStartAndAttach goroutine started", "sessionID", sessionID)
		if err := containers.ExecStartAndAttach(r.conn, sessionID, execStartOptions); err != nil {
			slog.Error("error attaching to persistent exec session", "error", err)
			fmt.Fprintf(os.Stderr, "Error attaching to persistent exec session: %v\n", err)
			// Handle error: perhaps close pipes and reset session ID
			stdinReader.Close()
			stdoutWriter.Close()
			stderrWriter.Close()
			r.mu.Lock()
			r.execSessionID = ""
			r.stdinPipe = nil
			r.stdoutPipe = nil
			r.stderrPipe = nil
			r.mu.Unlock()
			slog.Debug("bash session reset after error")
		} else {
			slog.Debug("ExecStartAndAttach completed successfully")
		}
	}()

	r.execSessionID = sessionID
	r.stdinPipe = stdinWriter
	r.stdoutPipe = stdoutReader
	r.stderrPipe = stderrReader

	slog.Debug("bash session pipes configured", "sessionID", sessionID)

	// Initialize shell with clean prompts to avoid output pollution
	initCmd := "export PS1=\"\"; export PS2=\"\"\n"
	slog.Debug("initializing shell with clean prompts")
	if _, err := stdinWriter.Write([]byte(initCmd)); err != nil {
		slog.Error("failed to initialize shell prompts", "error", err)
		return fmt.Errorf("failed to initialize shell prompts: %w", err)
	}

	// Consume the initial welcome message and prompt output
	slog.Debug("consuming initial bash output")
	discardBuf := make([]byte, 4096)
	_, err = stdoutReader.Read(discardBuf)
	if err != nil && err != io.EOF {
		slog.Error("failed to read initial bash output", "error", err)
		return fmt.Errorf("failed to read initial bash output: %w", err)
	}
	slog.Debug("initial bash output consumed")

	return nil
}

// createContainer creates and starts a new container
func (r *PodmanShellRunner) createContainer(ctx context.Context) error {
	slog.Debug("creating new container", "image", r.imageName, "containerName", r.containerName)

	s := specgen.NewSpecGenerator(r.imageName, false)
	s.Name = r.containerName

	// Set up for interactive bash session
	s.Command = []string{"bash"}
	stdin_open := true
	s.Terminal = &stdin_open

	// Mount current directory to /workspace
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	slog.Debug("mounting directory to container", "source", absPath, "destination", "/workspace")

	mount := spec.Mount{
		Type:        "bind",
		Source:      absPath,
		Destination: "/workspace",
	}
	s.Mounts = []spec.Mount{mount}

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

	// Ensure container is running
	slog.Debug("ensuring container is running")
	if err := r.ensureContainer(ctx); err != nil {
		slog.Error("failed to ensure container", "error", err)
		// If podman is not available, fall back to host shell only if allowed
		if r.allowFallback {
			slog.Debug("falling back to host shell")
			return hostShellRunner{}.Run(ctx, params)
		}
		return RunInShellOutput{}, fmt.Errorf("podman unavailable and fallback to host shell is disabled: %w", err)
	}

	// Ensure persistent bash session is established
	slog.Debug("ensuring persistent bash session")
	if err := r.ensurePersistentBashSession(ctx); err != nil {
		slog.Error("failed to establish persistent session", "error", err)
		if r.allowFallback {
			slog.Debug("falling back to host shell")
			return hostShellRunner{}.Run(ctx, params)
		}
		return RunInShellOutput{}, fmt.Errorf("failed to establish persistent session: %w", err)
	}

	// Compose the command to run in the container
	command := composeShellCommand(params.Command) + "\n" // Add newline to execute command
	slog.Debug("composed command", "command", command)

	// Write the command to the persistent session's stdin
	slog.Debug("writing command to stdin")
	_, err := r.stdinPipe.Write([]byte(command))
	if err != nil {
		slog.Error("failed to write to stdin", "error", err)
		return RunInShellOutput{}, fmt.Errorf("failed to write command to persistent session: %w", err)
	}
	slog.Debug("command written to stdin successfully")

	// Read output from stdout and stderr
	output := RunInShellOutput{}

	// Read from stdout with a timeout or until we get the exit code marker
	slog.Debug("reading from stdout")
	outputBytes := make([]byte, 4096)
	n, err := r.stdoutPipe.Read(outputBytes)
	slog.Debug("read from stdout completed", "bytesRead", n, "error", err)
	if err != nil && err != io.EOF {
		slog.Error("failed to read from stdout", "error", err)
		return RunInShellOutput{}, fmt.Errorf("failed to read from stdout: %w", err)
	}

	output.Output = string(outputBytes[:n])
	slog.Debug("output read", "outputLength", len(output.Output))

	// Parse exit code from the output (it's appended by composeShellCommand)
	// The format is "**Exit Code**: <number>"
	lines := strings.Split(output.Output, "\n")
	l := len(lines) - 1
	output.ExitCode = lines[l]
	slog.Debug("exit code extracted", "exitCode", output.ExitCode)
	// Clearing the last line to help the GC
	lines[l] = ""
	lines = lines[:l]
	output.Output = strings.Join(lines, "\n")
	slog.Debug("Run completed successfully")
	return output, nil
}

// Close closes the persistent bash session and its associated pipes.
func (r *PodmanShellRunner) Close(ctx context.Context) error {
	slog.Debug("closing bash session")

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.execSessionID == "" {
		slog.Debug("no active session to close")
		return nil // No session to close
	}

	slog.Debug("closing pipes", "sessionID", r.execSessionID)

	// Close the pipes
	if r.stdinPipe != nil {
		slog.Debug("closing stdin pipe")
		r.stdinPipe.Close()
	}
	if r.stdoutPipe != nil {
		slog.Debug("closing stdout pipe")
		r.stdoutPipe.Close()
	}
	if r.stderrPipe != nil {
		slog.Debug("closing stderr pipe")
		r.stderrPipe.Close()
	}

	// Optionally, stop the exec session in Podman.
	// This might not be strictly necessary as closing pipes should eventually
	// lead to the session terminating, but it's good practice for explicit cleanup.
	// However, there isn't a direct `ExecStop` in the Podman bindings.
	// The session will eventually be garbage collected by Podman when the container
	// is stopped or the exec process exits.

	r.execSessionID = ""
	r.stdinPipe = nil
	r.stdoutPipe = nil
	r.stderrPipe = nil

	slog.Debug("bash session closed successfully")

	return nil
}

func composeShellCommand(userCommand string) string {
	return userCommand + "; echo $?"
}
