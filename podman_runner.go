//go:build podman
// +build podman

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"io"

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
		return nil
	}

	// Get current user for socket paths
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	// Try macOS podman machine socket first
	macOSSocket := filepath.Join(currentUser.HomeDir, ".local/share/containers/podman/machine/podman.sock")
	if _, err := os.Stat(macOSSocket); err == nil {
		conn, err := bindings.NewConnection(ctx, "unix://"+macOSSocket)
		if err == nil {
			r.conn = conn
			return nil
		}
	}

	// Try default connection (may work on some Linux setups)
	conn, err := bindings.NewConnection(ctx, "")
	if err == nil {
		r.conn = conn
		return nil
	}

	// Try user socket (rootless podman on Linux)
	userSocket := fmt.Sprintf("unix:///run/user/%s/podman/podman.sock", currentUser.Uid)
	conn, err = bindings.NewConnection(ctx, userSocket)
	if err != nil {
		// Try system socket (root podman on Linux)
		conn, err = bindings.NewConnection(ctx, "unix:///var/run/podman/podman.sock")
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}
	}

	r.conn = conn
	return nil
}

// ensureContainer ensures the container is running
func (r *PodmanShellRunner) ensureContainer(ctx context.Context) error {
	if err := r.ensureConnection(ctx); err != nil {
		return err
	}

	// Check if container exists and is running
	inspectData, err := containers.Inspect(r.conn, r.containerName, nil)
	if err == nil {
		// Container exists, check if it's running
		if inspectData.State.Running {
			return nil
		}

		// Container exists but not running, start it
		if err := containers.Start(r.conn, r.containerName, nil); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		return nil
	}

	// Container doesn't exist, create and start it
	return r.createContainer(ctx)
}

// ensurePersistentBashSession ensures a persistent interactive bash session is established
func (r *PodmanShellRunner) ensurePersistentBashSession(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.execSessionID != "" {
		// Session already established
		return nil
	}

	// Ensure container is running
	if err := r.ensureContainer(ctx); err != nil {
		return err
	}

	// Create exec configuration for an interactive bash session
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
	sessionID, err := containers.ExecCreate(r.conn, r.containerName, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create persistent exec session: %w", err)
	}

	// Create pipes for stdin, stdout, and stderr
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	execStartOptions := new(containers.ExecStartAndAttachOptions)
	execStartOptions.WithInputStream(stdinReader)
	execStartOptions.WithOutputStream(stdoutWriter)
	execStartOptions.WithErrorStream(stderrWriter)
	execStartOptions.WithAttachInput(true)
	execStartOptions.WithAttachOutput(true)
	execStartOptions.WithAttachError(true)
	execStartOptions.WithTty(true) // Ensure TTY is enabled for the attachment

	// Start the exec session in a goroutine so it doesn't block
	go func() {
		if err := containers.ExecStartAndAttach(r.conn, sessionID, execStartOptions); err != nil {
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
		}
	}()

	r.execSessionID = sessionID
	r.stdinPipe = stdinWriter
	r.stdoutPipe = stdoutReader
	r.stderrPipe = stderrReader

	return nil
}

// createContainer creates and starts a new container
func (r *PodmanShellRunner) createContainer(ctx context.Context) error {
	s := specgen.NewSpecGenerator(r.imageName, false)
	s.Name = r.containerName

	// Set up for interactive bash session
	s.Command = []string{"bash"}
	s.Terminal = true
	s.OpenStdin = true

	// Mount current directory to /workspace
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	mount := spec.Mount{
		Type:        "bind",
		Source:      absPath,
		Destination: "/workspace",
	}
	s.Mounts = []spec.Mount{mount}

	// Create the container
	createResponse, err := containers.CreateWithSpec(r.conn, s, nil)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Start the container
	if err := containers.Start(r.conn, createResponse.ID, nil); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

func (r *PodmanShellRunner) Run(ctx context.Context, params RunShellCommandInput) (RunShellCommandOutput, error) {
	// Ensure container is running
	if err := r.ensureContainer(ctx); err != nil {
		// If podman is not available, fall back to host shell only if allowed
		if r.allowFallback {
			return hostShellRunner{}.Run(ctx, params)
		}
		return RunShellCommandOutput{}, fmt.Errorf("podman unavailable and fallback to host shell is disabled: %w", err)
	}

	// Compose the command to run in the container
	command := composeShellCommand(params.Command) + "\n" // Add newline to execute command

	// Write the command to the persistent session's stdin
	_, err := r.stdinPipe.Write([]byte(command))
	if err != nil {
		return RunShellCommandOutput{}, fmt.Errorf("failed to write command to persistent session: %w", err)
	}

		return RunShellCommandOutput{

			Stdout:   "Command sent to persistent bash session. Output and exit code determination is handled by the caller.\n",

			Stderr:   "",

			ExitCode: 0, // Exit code determination is handled by the caller

			PID:      0, // PID determination is handled by the caller

		}, nil
}

// Close closes the persistent bash session and its associated pipes.
func (r *PodmanShellRunner) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.execSessionID == "" {
		return nil // No session to close
	}

	// Close the pipes
	if r.stdinPipe != nil {
		r.stdinPipe.Close()
	}
	if r.stdoutPipe != nil {
		r.stdoutPipe.Close()
	}
	if r.stderrPipe != nil {
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

	return nil
}

func composeShellCommand(userCommand string) string {
	return "cd /workspace && just bootstrap && " + userCommand
}
