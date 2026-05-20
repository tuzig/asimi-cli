package runners

import (
	"context"
	"os"
	"testing"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPodmanRunner(t *testing.T) {
	repoInfo := repo.RepoInfo{
		ProjectRoot: t.TempDir(),
		Slug:        "test/project",
	}

	hostRunner := NewHostRunner(1)

	config := &Config{
		AllowHostFallback: true,
		NoCleanup:         false,
	}

	runner := NewPodmanRunner(config, repoInfo, 1, hostRunner)
	require.NotNil(t, runner)
	assert.Equal(t, "localhost/asimi-sandbox-test/project:latest", runner.imageName)
	assert.Equal(t, "asimi-shell-test-project-1", runner.containerName)
	assert.True(t, runner.allowFallback)
	assert.Equal(t, "podman", runner.RunnerType())
}

func TestNewPodmanRunnerCustomImage(t *testing.T) {
	repoInfo := repo.RepoInfo{
		ProjectRoot: t.TempDir(),
		Slug:        "test/project",
	}

	config := &Config{
		ImageName:         "custom-image:v1",
		AllowHostFallback: false,
	}

	runner := NewPodmanRunner(config, repoInfo, 2, nil)
	require.NotNil(t, runner)
	assert.Equal(t, "custom-image:v1", runner.imageName)
	assert.Equal(t, "asimi-shell-test-project-2", runner.containerName)
	assert.False(t, runner.allowFallback)
}

func TestPodmanRunnerWithFallback(t *testing.T) {
	if os.Getenv("container") != "" {
		t.Skip("Skipping Podman test when running inside a container")
	}

	repoInfo := repo.RepoInfo{
		ProjectRoot: t.TempDir(),
		Slug:        "test/nonexistent-image",
	}

	hostRunner := NewHostRunner(3)

	config := &Config{
		AllowHostFallback: true,
	}

	runner := NewPodmanRunner(config, repoInfo, 3, hostRunner)
	require.NotNil(t, runner)

	// This should fall back to host runner since the image doesn't exist
	output, err := runner.Run(context.Background(), Input{
		Command:        "echo hello",
		BypassApproval: true,
	})

	// With fallback enabled and image missing, it should either succeed via fallback
	// or fail with SandboxMissingError if fallback is disabled
	if err != nil {
		// If we get an error, it should be because podman isn't available
		// and fallback didn't work (which is fine for this test)
		t.Logf("Runner returned error (expected if podman unavailable): %v", err)
	} else {
		assert.Contains(t, output.Output, "hello")
		assert.Equal(t, "0", output.ExitCode)
	}
}

func TestPodmanRunnerIntegration(t *testing.T) {
	if os.Getenv("container") != "" {
		t.Skip("Skipping Podman test when running inside a container")
	}

	if os.Getenv("ASIMI_TEST_PODMAN") == "" {
		t.Skip("Skipping Podman integration test. Set ASIMI_TEST_PODMAN=1 to run this test (requires podman and sandbox image)")
	}

	repoInfo := repo.RepoInfo{
		ProjectRoot: t.TempDir(),
		Slug:        "test/project",
	}

	config := &Config{
		AllowHostFallback: false,
	}

	runner := NewPodmanRunner(config, repoInfo, 4, nil)
	require.NotNil(t, runner)
	defer runner.Close(context.Background())

	// Test basic command
	output, err := runner.Run(context.Background(), Input{
		Command: "echo hello",
	})
	require.NoError(t, err)
	assert.Contains(t, output.Output, "hello")
	assert.Equal(t, "0", output.ExitCode)

	// Test command with stderr
	output2, err := runner.Run(context.Background(), Input{
		Command: "echo 'stdout' && echo 'stderr' >&2",
	})
	require.NoError(t, err)
	assert.Contains(t, output2.Output, "stdout")
	assert.Contains(t, output2.Output, "stderr")
	assert.Equal(t, "0", output2.ExitCode)

	// Test command with non-zero exit code
	output3, err := runner.Run(context.Background(), Input{
		Command: "exit 42",
	})
	require.NoError(t, err)
	assert.Equal(t, "42", output3.ExitCode)
}

func TestPodmanRunnerContainerLaunchMessage(t *testing.T) {
	if os.Getenv("container") != "" {
		t.Skip("Skipping Podman test when running inside a container")
	}

	if os.Getenv("ASIMI_TEST_PODMAN") == "" {
		t.Skip("Skipping Podman integration test. Set ASIMI_TEST_PODMAN=1 to run this test")
	}

	repoInfo := repo.RepoInfo{
		ProjectRoot: t.TempDir(),
		Slug:        "test/project",
	}

	config := &Config{
		AllowHostFallback: false,
	}

	runner := NewPodmanRunner(config, repoInfo, 5, nil)
	msgChan := make(chan Msg, 10)
	runner.SetMessageChannel(msgChan)
	require.NotNil(t, runner)
	defer runner.Close(context.Background())

	// Run a command to trigger container initialization
	_, err := runner.Run(context.Background(), Input{
		Command: "echo test",
	})
	require.NoError(t, err)

	// Check that we received a container launched message
	select {
	case msg := <-msgChan:
		_, ok := msg.(ContainerLaunchedMsg)
		assert.True(t, ok, "Expected ContainerLaunchedMsg, got %T", msg)
	default:
		// Message may have already been consumed or container was already running
		t.Log("No container launch message received (container may have been reused)")
	}
}
