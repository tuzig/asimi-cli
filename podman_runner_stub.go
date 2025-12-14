//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
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

func newPodmanShellRunner(allowFallback bool, config *Config, repoInfo RepoInfo) *PodmanShellRunner {
	pid := os.Getpid()
	noCleanup := false
	if config != nil && config.LLM.PodmanNoCleanup {
		noCleanup = true
	}
	return &PodmanShellRunner{
		imageName:     "localhost/asimi-shell:latest",
		containerName: fmt.Sprintf("asimi-shell-%d", pid),
		allowFallback: allowFallback,
		noCleanup:     noCleanup,
		config:        config,
		repoInfo:      repoInfo,
		outputs:       make(map[int]*commandOutput),
		nextCommandID: 1,
	}
}

func (r *PodmanShellRunner) Run(ctx context.Context, params RunShellCommandInput) (RunShellCommandOutput, error) {
	// In non-podman build, always fall back to host shell
	return hostRun(ctx, params)
}

func (r *PodmanShellRunner) ensureConnection(ctx context.Context) error {
	return fmt.Errorf("podman not available in this build")
}

func (r *PodmanShellRunner) ensureContainer(ctx context.Context) error {
	return fmt.Errorf("podman not available in this build")
}

func (r *PodmanShellRunner) createContainer(ctx context.Context) error {
	return fmt.Errorf("podman not available in this build")
}
