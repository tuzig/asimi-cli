package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPodmanRunnerStub(t *testing.T) {
	// Test that the stub implementation works
	runner := newPodmanShellRunner(true)
	require.NotNil(t, runner)
	require.Equal(t, "localhost/asimi-shell:latest", runner.imageName)
	require.True(t, runner.allowFallback)
}
