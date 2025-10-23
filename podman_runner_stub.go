//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
)

type PodmanShellRunner struct {
	imageName     string
	allowFallback bool
}

func newPodmanShellRunner(allowFallback bool) *PodmanShellRunner {
	return &PodmanShellRunner{
		imageName:     "localhost/asimi-shell:latest",
		allowFallback: allowFallback,
	}
}

func (r *PodmanShellRunner) Run(ctx context.Context, params RunInShellInput) (RunInShellOutput, error) {
	// In non-podman build, always fall back to host shell
	return hostShellRunner{}.Run(ctx, params)
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

func composeShellCommand(userCommand string) string {
	return "cd /workspace && just bootstrap && " + userCommand
}
