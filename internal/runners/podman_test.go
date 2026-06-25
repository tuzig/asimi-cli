package runners

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPodmanRunner(t *testing.T) {
	repoInfo := repo.RepoInfo{
		ProjectRoot: t.TempDir(),
		Slug:        "test/project",
	}

	hostRunner := NewHostRunner(1, t.TempDir())

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

	hostRunner := NewHostRunner(3, t.TempDir())

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

func TestHealthcheckTimeout(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/hc"}, 0, nil)
	runner.outputs = make(map[int]*commandOutput)

	// Create pipe pairs: stdin (runner writes) and stdout (nobody writes, readStream reads)
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	runner.stdinPipe = stdinWriter
	runner.stdoutPipe = stdoutReader

	// Drain stdin in the background — io.Pipe is synchronous so writes block
	// until someone reads. Without this, healthcheck's stdinPipe.Write hangs
	// and the timeout path is never reached.
	go io.Copy(io.Discard, stdinReader)

	// Start readStream — it will block reading from stdoutReader since nobody writes
	go runner.readStream(stdoutReader)

	// Close pipes when done to avoid goroutine leaks
	defer stdinReader.Close()
	defer stdinWriter.Close()
	defer stdoutWriter.Close()

	// healthcheck should time out since no response is written to stdoutWriter
	done := make(chan error, 1)
	go func() {
		done <- runner.healthcheck(context.Background())
	}()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "healthcheck")
	case <-time.After(6 * time.Second):
		t.Fatal("healthcheck did not complete within expected timeout")
	}
}

func TestHealthcheckSuccess(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/hc"}, 0, nil)
	runner.outputs = make(map[int]*commandOutput)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	runner.stdinPipe = stdinWriter
	runner.stdoutPipe = stdoutReader

	// Start readStream to parse the response we'll write
	go runner.readStream(stdoutReader)

	defer stdinReader.Close()
	defer stdoutWriter.Close()

	// When healthcheck writes a command to stdinWriter, read it from stdinReader
	// to unblock the pipe, then write a valid response to stdoutWriter
	go func() {
		// Read and discard the healthcheck command from stdin
		buf := make([]byte, 4096)
		n, _ := stdinReader.Read(buf)
		_ = n

		// Write a valid __asimi_run response for command ID 1
		fmt.Fprintf(stdoutWriter, "__ASIMI_STDOUT_START:1\n__ASIMI_STDOUT_END:1:0\n")
	}()

	err := runner.healthcheck(context.Background())
	require.NoError(t, err)
}

func TestPodmanRunnerHealthcheckPass(t *testing.T) {
	if os.Getenv("container") != "" {
		t.Skip("Skipping Podman test when running inside a container")
	}

	if os.Getenv("ASIMI_TEST_PODMAN") == "" {
		t.Skip("Skipping Podman integration test. Set ASIMI_TEST_PODMAN=1 to run this test")
	}

	repoInfo := repo.RepoInfo{
		ProjectRoot: t.TempDir(),
		Slug:        "test/hc-pass",
	}

	config := &Config{
		AllowHostFallback: false,
	}

	// First runner: create and verify container is healthy
	runner1 := NewPodmanRunner(config, repoInfo, 100, nil)
	msgChan1 := make(chan Msg, 10)
	runner1.SetMessageChannel(msgChan1)
	require.NotNil(t, runner1)
	defer runner1.Close(context.Background())

	output, err := runner1.Run(context.Background(), Input{Command: "echo alive"})
	require.NoError(t, err)
	assert.Contains(t, output.Output, "alive")

	// Second runner with same containerName (same slug + connID) reuses the running container
	runner2 := NewPodmanRunner(config, repoInfo, 100, nil)
	msgChan2 := make(chan Msg, 10)
	runner2.SetMessageChannel(msgChan2)
	require.NotNil(t, runner2)
	defer runner2.Close(context.Background())

	output2, err := runner2.Run(context.Background(), Input{Command: "echo still-alive"})
	require.NoError(t, err)
	assert.Contains(t, output2.Output, "still-alive")

	// No SandboxUnhealthyMsg should have been sent — container is healthy
	select {
	case msg := <-msgChan2:
		_, ok := msg.(SandboxUnhealthyMsg)
		assert.False(t, ok, "unexpected SandboxUnhealthyMsg for healthy container, got %T: %v", msg, msg)
	default:
		// No message — expected for a healthy container
	}
}

func TestPodmanRunnerHealthcheckRecovery(t *testing.T) {
	if os.Getenv("container") != "" {
		t.Skip("Skipping Podman test when running inside a container")
	}

	if os.Getenv("ASIMI_TEST_PODMAN") == "" {
		t.Skip("Skipping Podman integration test. Set ASIMI_TEST_PODMAN=1 to run this test")
	}

	repoInfo := repo.RepoInfo{
		ProjectRoot: t.TempDir(),
		Slug:        "test/hc-recover",
	}

	config := &Config{
		AllowHostFallback: false,
	}

	// First runner: create the container and verify it works
	runner1 := NewPodmanRunner(config, repoInfo, 200, nil)
	require.NotNil(t, runner1)

	output, err := runner1.Run(context.Background(), Input{Command: "echo before-wedge"})
	require.NoError(t, err)
	assert.Contains(t, output.Output, "before-wedge")

	containerName := runner1.containerName
	t.Logf("container name: %s", containerName)

	// Wedge the shell: kill bash PID 1 inside the container so the shell stops responding
	_, err = runner1.Run(context.Background(), Input{Command: "kill -9 1"})
	// The kill may cause the command to fail or the container to exit — either is fine
	t.Logf("kill result: %v", err)

	// Close the first runner (pipes are likely broken now)
	_ = runner1.Close(context.Background())

	// Second runner reuses the same container (same slug + connID)
	runner2 := NewPodmanRunner(config, repoInfo, 200, nil)
	msgChan := make(chan Msg, 10)
	runner2.SetMessageChannel(msgChan)
	require.NotNil(t, runner2)
	defer runner2.Close(context.Background())

	// Run a command — healthcheck should detect the stale container, recreate, and succeed
	output2, err := runner2.Run(context.Background(), Input{Command: "echo recovered"})
	require.NoError(t, err, "command should succeed after healthcheck recovery")
	assert.Contains(t, output2.Output, "recovered")

	// Check that SandboxUnhealthyMsg was sent
	select {
	case msg := <-msgChan:
		unhealthy, ok := msg.(SandboxUnhealthyMsg)
		if ok {
			assert.Equal(t, containerName, unhealthy.ContainerName)
			t.Logf("received expected SandboxUnhealthyMsg: %s", unhealthy.Message)
		} else {
			t.Logf("got %T instead of SandboxUnhealthyMsg (container may have been stopped, not running)", msg)
		}
	default:
		// If the container was stopped (not running), initialize() skips the healthcheck
		// and just starts/recreates it — no SandboxUnhealthyMsg in that case
		t.Log("no SandboxUnhealthyMsg received (container may have been stopped rather than wedged)")
	}
}
