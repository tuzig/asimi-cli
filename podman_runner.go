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
}

func newPodmanShellRunner(allowFallback bool) *PodmanShellRunner {
	return &PodmanShellRunner{
		imageName:     "localhost/asimi-shell:latest",
		containerName: "asimi-shell-workspace",
		allowFallback: allowFallback,
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

// createContainer creates and starts a new container
func (r *PodmanShellRunner) createContainer(ctx context.Context) error {
	s := specgen.NewSpecGenerator(r.imageName, false)
	s.Name = r.containerName

	// Keep container running by using a long-running command
	s.Command = []string{"sleep", "infinity"}

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
	command := composeShellCommand(params.Command)

	// Create exec configuration
	execConfig := &handlers.ExecCreateConfig{
		ExecOptions: dockerContainer.ExecOptions{
			Cmd:          []string{"bash", "-c", command},
			WorkingDir:   "/workspace",
			AttachStdout: true,
			AttachStderr: true,
		},
	}

	// Create the exec session
	sessionID, err := containers.ExecCreate(r.conn, r.containerName, execConfig)
	if err != nil {
		return RunShellCommandOutput{}, fmt.Errorf("failed to create exec session: %w", err)
	}

	// Prepare buffers for output
	var stdout, stderr bytes.Buffer

	// Start and attach to the exec session
	execStartOptions := new(containers.ExecStartAndAttachOptions)
	execStartOptions.WithOutputStream(&stdout)
	execStartOptions.WithErrorStream(&stderr)
	execStartOptions.WithAttachOutput(true)
	execStartOptions.WithAttachError(true)

	if err := containers.ExecStartAndAttach(r.conn, sessionID, execStartOptions); err != nil {
		return RunShellCommandOutput{}, fmt.Errorf("exec failed: %w", err)
	}

	// Get exec inspect to get exit code
	inspectData, err := containers.ExecInspect(r.conn, sessionID, nil)
	if err != nil {
		return RunShellCommandOutput{}, fmt.Errorf("failed to inspect exec: %w", err)
	}

	return RunShellCommandOutput{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: inspectData.ExitCode,
		PID:      inspectData.Pid,
	}, nil
}

func composeShellCommand(userCommand string) string {
	return "cd /workspace && just bootstrap && " + userCommand
}
