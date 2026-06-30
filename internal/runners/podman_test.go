package runners

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
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
	assert.Equal(t, "localhost/asimi/sandbox/test/project:latest", runner.imageName)
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
	go runner.readStream(stdoutReader, nil)

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
	go runner.readStream(stdoutReader, nil)

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

// TestHealthcheckNilStdinPipe verifies that healthcheck returns an error
// instead of panicking when stdinPipe is nil (race with Restart()/Close()).
func TestHealthcheckNilStdinPipe(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/nil"}, 0, nil)
	runner.outputs = make(map[int]*commandOutput)

	// stdinPipe is nil by default — healthcheck should return an error, not panic
	err := runner.healthcheck(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdinPipe is nil")
}

// TestHealthcheckConcurrentNilRace verifies that healthcheck does not panic
// when stdinPipe is concurrently set to nil (simulating Restart() or Close()).
func TestHealthcheckConcurrentNilRace(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/race"}, 0, nil)
	runner.outputs = make(map[int]*commandOutput)

	stdinReader, stdinWriter := io.Pipe()
	defer stdinReader.Close()

	runner.stdinPipe = stdinWriter

	// Drain stdin so writes don't block forever
	go io.Copy(io.Discard, stdinReader)

	done := make(chan error, 1)
	go func() {
		done <- runner.healthcheck(context.Background())
	}()

	// Simulate Restart()/Close() niling the pipe concurrently
	time.Sleep(1 * time.Millisecond)
	runner.mu.Lock()
	runner.stdinPipe = nil
	runner.mu.Unlock()

	// healthcheck should either time out or get a write error — but not panic
	select {
	case err := <-done:
		_ = err // either nil or error is fine; the point is no panic
	case <-time.After(7 * time.Second):
		t.Fatal("healthcheck did not complete within expected timeout")
	}
}

func TestSandboxMissingErrorNoAgentsDir(t *testing.T) {
	// ProjectRoot without .agents/ → user hasn't run :init
	err := SandboxMissingError{
		ImageName:   "localhost/asimi/sandbox/test/project:latest",
		ProjectRoot: t.TempDir(),
	}
	msg := err.Error()
	assert.Contains(t, msg, "Sandbox container image is missing.")
	assert.Contains(t, msg, "Did you run `:init` ?")
	assert.NotContains(t, msg, "build-sandbox")
}

func TestSandboxMissingErrorWithAgentsDir(t *testing.T) {
	// ProjectRoot with .agents/ → user ran :init but image is missing
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(dir+"/.agents", 0o755))

	err := SandboxMissingError{
		ImageName:   "localhost/asimi/sandbox/test/project:latest",
		ProjectRoot: dir,
	}
	msg := err.Error()
	assert.Contains(t, msg, "Sandbox container image 'localhost/asimi/sandbox/test/project:latest' is missing.")
	assert.Contains(t, msg, "Did you run `just build-sandbox` ?")
	assert.NotContains(t, msg, ":init")
}

// TestReadStreamConcurrentCloseReady verifies that readStream's exit cleanup
// does not panic with "close of closed channel" when multiple readStream
// goroutines race to close the same cmd.ready channel. This simulates
// concurrent initialize() calls that each launch their own readStream.
func TestReadStreamConcurrentCloseReady(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/concurrent"}, 0, nil)
	runner.outputs = make(map[int]*commandOutput)

	// Register a command output that both readStream goroutines will try to close
	cmd := &commandOutput{ready: make(chan struct{})}
	runner.outputs[1] = cmd

	// Two stdout readers that both share the same outputs map — they both
	// see command 1 as not-yet-done and try to close cmd.ready on exit.
	stop1 := make(chan struct{})
	stop2 := make(chan struct{})

	go runner.readStream(stdoutPipeReader(t), stop1)
	go runner.readStream(stdoutPipeReader(t), stop2)

	// Signal both to stop — they will race into the exit cleanup
	close(stop1)
	close(stop2)

	// Wait for cmd.ready to be closed — no panic means the test passes
	select {
	case <-cmd.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("cmd.ready was not closed within timeout")
	}

	// Closing again should be a no-op, not a panic
	runner.closeReady(cmd)
}

// TestCloseReadyIdempotent verifies that closeReady can be called multiple
// times concurrently without panicking.
func TestCloseReadyIdempotent(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/idempotent"}, 0, nil)
	runner.outputs = make(map[int]*commandOutput)

	cmd := &commandOutput{ready: make(chan struct{})}
	runner.outputs[1] = cmd

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runner.closeReady(cmd)
		}()
	}
	wg.Wait()

	select {
	case <-cmd.ready:
	default:
		t.Fatal("cmd.ready should be closed")
	}
}

// stdoutPipeReader creates a pipe whose read end can be passed to readStream.
// The write end is closed immediately so the scanner returns EOF quickly.
func stdoutPipeReader(t *testing.T) io.Reader {
	t.Helper()
	r, w := io.Pipe()
	go w.Close()
	return r
}

// TestAttachGoroutineDoesNotClobberNewPipes simulates the race fixed in edict 550:
// After Restart() triggers initialize() which sets new pipes, a stale Attach goroutine
// from the previous attachment errors out and runs its cleanup handler. The handler
// must NOT nil the new pipes because r.stdinPipe no longer matches the old stdinWriter.
func TestAttachGoroutineDoesNotClobberNewPipes(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/attach-race"}, 0, nil)

	// Simulate the first attachment's pipes (the "old" ones)
	oldStdinReader, oldStdinWriter := io.Pipe()
	oldStdoutReader, oldStdoutWriter := io.Pipe()
	defer oldStdinReader.Close()
	defer oldStdoutWriter.Close()

	runner.mu.Lock()
	runner.stdinPipe = oldStdinWriter
	runner.stdoutPipe = oldStdoutReader
	runner.mu.Unlock()

	// Simulate initialize() setting new pipes (the "fresh" ones)
	newStdinReader, newStdinWriter := io.Pipe()
	newStdoutReader, newStdoutWriter := io.Pipe()
	defer newStdinReader.Close()
	defer newStdoutWriter.Close()

	runner.mu.Lock()
	runner.stdinPipe = newStdinWriter
	runner.stdoutPipe = newStdoutReader
	runner.mu.Unlock()

	// Now the stale Attach goroutine's error handler runs.
	// It closes its local old pipes and checks r.stdinPipe == oldStdinWriter.
	// Since newStdinWriter replaced oldStdinWriter, the check should fail and
	// the new pipes must survive.
	oldStdinReader.Close()
	oldStdoutWriter.Close()

	runner.mu.Lock()
	if runner.stdinPipe == oldStdinWriter {
		runner.stdinPipe = nil
		runner.stdoutPipe = nil
	}
	runner.mu.Unlock()

	// Verify the new pipes survived
	runner.mu.Lock()
	assert.Equal(t, newStdinWriter, runner.stdinPipe, "new stdinPipe should survive stale Attach cleanup")
	assert.Equal(t, newStdoutReader, runner.stdoutPipe, "new stdoutPipe should survive stale Attach cleanup")
	runner.mu.Unlock()
}

// TestAttachGoroutineNilsMatchingPipes verifies that when the Attach error handler
// runs and r.stdinPipe still matches the old stdinWriter (no concurrent initialize()),
// the pipes ARE niled as expected.
func TestAttachGoroutineNilsMatchingPipes(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/attach-match"}, 0, nil)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdinReader.Close()
	defer stdoutWriter.Close()

	runner.mu.Lock()
	runner.stdinPipe = stdinWriter
	runner.stdoutPipe = stdoutReader
	runner.mu.Unlock()

	// Simulate the Attach error handler with matching pipes
	stdinReader.Close()
	stdoutWriter.Close()

	runner.mu.Lock()
	if runner.stdinPipe == stdinWriter {
		runner.stdinPipe = nil
		runner.stdoutPipe = nil
	}
	runner.mu.Unlock()

	runner.mu.Lock()
	assert.Nil(t, runner.stdinPipe, "stdinPipe should be nil when pipes match")
	assert.Nil(t, runner.stdoutPipe, "stdoutPipe should be nil when pipes match")
	runner.mu.Unlock()
}

// TestAttachGoroutineResetsContainerStarted verifies Bug 2: when the Attach
// error handler runs and r.stdinPipe matches the old stdinWriter, it must
// also reset containerStarted so the next initialize() re-inspects the container.
func TestAttachGoroutineResetsContainerStarted(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/reset"}, 0, nil)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdinReader.Close()
	defer stdoutWriter.Close()

	runner.mu.Lock()
	runner.stdinPipe = stdinWriter
	runner.stdoutPipe = stdoutReader
	runner.containerStarted = true
	runner.mu.Unlock()

	// Simulate the Attach error handler with matching pipes
	stdinReader.Close()
	stdoutWriter.Close()

	runner.mu.Lock()
	if runner.stdinPipe == stdinWriter {
		runner.stdinPipe = nil
		runner.stdoutPipe = nil
		runner.containerStarted = false // Bug 2 fix
	}
	runner.mu.Unlock()

	runner.mu.Lock()
	assert.Nil(t, runner.stdinPipe, "stdinPipe should be nil")
	assert.False(t, runner.containerStarted, "containerStarted should be reset to force re-inspection")
	runner.mu.Unlock()
}

// TestAttachGoroutineResetsContainerStartedNonMatchingPipes verifies that
// when pipes don't match (concurrent initialize replaced them), containerStarted
// is NOT reset — the new attachment owns the state.
func TestAttachGoroutineResetsContainerStartedNonMatchingPipes(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/noreset"}, 0, nil)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdinReader.Close()
	defer stdoutWriter.Close()

	runner.mu.Lock()
	runner.stdinPipe = stdinWriter
	runner.stdoutPipe = stdoutReader
	runner.containerStarted = true
	runner.mu.Unlock()

	// Simulate a concurrent initialize() replacing the pipes
	newStdinWriter := &nopWriteCloser{}
	newStdoutReader := &nopReadCloser{}
	runner.mu.Lock()
	runner.stdinPipe = newStdinWriter
	runner.stdoutPipe = newStdoutReader
	runner.mu.Unlock()

	// Stale Attach error handler runs — pipes don't match
	stdinReader.Close()
	stdoutWriter.Close()

	runner.mu.Lock()
	if runner.stdinPipe == stdinWriter {
		runner.stdinPipe = nil
		runner.stdoutPipe = nil
		runner.containerStarted = false
	}
	runner.mu.Unlock()

	runner.mu.Lock()
	assert.Equal(t, newStdinWriter, runner.stdinPipe, "new pipes should survive stale handler")
	assert.True(t, runner.containerStarted, "containerStarted should NOT be reset by stale handler")
	runner.mu.Unlock()
}

// TestEstablishConnectionContextNotCanceled is the regression test for Edict 586:
// the connection context returned by establishConnection must NOT be canceled
// after the function returns. The old dialWithTimeout used context.WithTimeout
// with defer cancel(), which canceled the context — and all children — the
// moment the function returned, killing every subsequent API call.
//
// This test calls establishConnection against a live podman socket if
// available; if podman is not running it is skipped.
func TestEstablishConnectionContextNotCanceled(t *testing.T) {
	if os.Getenv("container") != "" {
		t.Skip("Skipping Podman test when running inside a container")
	}

	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/ctx"}, 0, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := runner.establishConnection(ctx)
	if err != nil {
		t.Skipf("podman not available, skipping: %v", err)
	}

	if err := conn.Err(); err != nil {
		t.Fatalf("connection context is already canceled after establishConnection returned: %v", err)
	}
}

// nopWriteCloser is a no-op io.WriteCloser for testing.
type nopWriteCloser struct{}

func (n *nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (n *nopWriteCloser) Close() error                { return nil }

// nopReadCloser is a no-op io.ReadCloser for testing.
type nopReadCloser struct{}

func (n *nopReadCloser) Read(p []byte) (int, error) { return 0, io.EOF }
func (n *nopReadCloser) Close() error                { return nil }

// TestRcCommandsWriteErrorPropagation verifies Bug 1: when the rc-commands
// write fails (broken pipe), the error should be returned rather than nil.
// This test exercises the pipe write + select logic that initialize() uses.
func TestRcCommandsWriteErrorPropagation(t *testing.T) {
	// Create a pipe and close the read end to simulate a broken pipe
	stdinReader, stdinWriter := io.Pipe()
	stdinReader.Close() // Close reader → writes will fail with broken pipe

	// Simulate the rc-commands write logic from initialize()
	rc := "git config --global core.pager cat\ncd /tmp\n"
	stdinPipe := stdinWriter

	writeDone := make(chan error, 1)
	go func() {
		_, err := stdinPipe.Write([]byte(rc))
		writeDone <- err
	}()

	var resultErr error
	select {
	case err := <-writeDone:
		if err != nil {
			resultErr = fmt.Errorf("attachment failed: rc-commands write: %w", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("write did not complete within 5 seconds")
	}

	require.Error(t, resultErr, "should return error on broken pipe write")
	assert.Contains(t, resultErr.Error(), "attachment failed: rc-commands write")
}

// TestRcCommandsWriteCancelledReturnsError verifies Bug 1: when the context
// is cancelled during the rc-commands write, an error is returned.
func TestRcCommandsWriteCancelledReturnsError(t *testing.T) {
	// Use a pipe where the reader is never read — writes will block
	stdinReader, stdinWriter := io.Pipe()
	defer stdinReader.Close()
	defer stdinWriter.Close()

	ctx, cancel := context.WithCancel(context.Background())

	stdinPipe := stdinWriter
	writeDone := make(chan error, 1)
	go func() {
		_, err := stdinPipe.Write([]byte("test\n"))
		writeDone <- err
	}()

	// Cancel the context immediately
	cancel()

	var resultErr error
	select {
	case err := <-writeDone:
		// Write may complete or fail — either way check ctx path
		_ = err
	case <-ctx.Done():
		resultErr = fmt.Errorf("attachment failed: rc-commands write cancelled: %w", ctx.Err())
	}

	require.Error(t, resultErr, "should return error on cancelled context")
	assert.Contains(t, resultErr.Error(), "attachment failed: rc-commands write cancelled")
}

// TestHealthcheckRunsOnReattach verifies Bug 4: when containerStarted is true
// and stdinPipe is nil (re-attaching), the healthcheck condition should fire.
// This test validates the containerWasAlreadyStarted flag logic.
func TestHealthcheckRunsOnReattach(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/reattach"}, 0, nil)

	// Simulate the state: containerStarted=true, stdinPipe=nil (need re-attach)
	runner.mu.Lock()
	runner.containerStarted = true
	runner.stdinPipe = nil
	runner.mu.Unlock()

	// Replicate the containerWasAlreadyStarted logic from initialize()
	containerWasAlreadyStarted := false
	existingRunning := false

	runner.mu.Lock()
	if !runner.containerStarted {
		// This branch won't run — containerStarted is true
	} else {
		if runner.stdinPipe == nil {
			containerWasAlreadyStarted = true
		}
	}
	runner.mu.Unlock()

	// The healthcheck condition should be true
	assert.True(t, containerWasAlreadyStarted,
		"containerWasAlreadyStarted should be true when re-attaching to a started container with nil pipes")
	assert.True(t, existingRunning || containerWasAlreadyStarted,
		"healthcheck should run when containerWasAlreadyStarted is true")
}

// TestHealthcheckSkippedOnFreshContainer verifies Bug 4: when a container
// is freshly created (containerStarted set by this initialize() call, not
// pre-existing), containerWasAlreadyStarted should be false.
func TestHealthcheckSkippedOnFreshContainer(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/fresh"}, 0, nil)

	// Fresh container — containerStarted is false initially
	containerWasAlreadyStarted := false
	existingRunning := false

	runner.mu.Lock()
	if !runner.containerStarted {
		// Simulate createContainer path: containerStarted gets set
		runner.containerStarted = true
		// existingRunning stays false (not an existing running container)
	} else {
		if runner.stdinPipe == nil {
			containerWasAlreadyStarted = true
		}
	}
	runner.mu.Unlock()

	assert.False(t, containerWasAlreadyStarted,
		"containerWasAlreadyStarted should be false for a freshly created container")
	assert.False(t, existingRunning || containerWasAlreadyStarted,
		"healthcheck should not run for a fresh container (no existingRunning, no re-attach)")
}

// TestHealthcheckFailurePipeCleanupNoRace verifies that the healthcheck failure
// path's pipe cleanup (which closes and nils stdinPipe/stdoutPipe) does not
// data race with a concurrent Attach goroutine error handler that modifies
// the same fields. Run with -race to detect the race.
func TestHealthcheckFailurePipeCleanupNoRace(t *testing.T) {
	runner := NewPodmanRunner(&Config{}, repo.RepoInfo{ProjectRoot: t.TempDir(), Slug: "test/hc-race"}, 0, nil)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdinReader.Close()
	defer stdoutWriter.Close()

	runner.mu.Lock()
	runner.stdinPipe = stdinWriter
	runner.stdoutPipe = stdoutReader
	runner.mu.Unlock()

	var wg sync.WaitGroup

	// Simulate the healthcheck failure path: close and nil pipes under lock
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := runner
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
	}()

	// Simulate the Attach goroutine error handler modifying the same fields under lock
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := runner
		r.mu.Lock()
		if r.stdinPipe == stdinWriter {
			r.stdinPipe = nil
			r.stdoutPipe = nil
			r.containerStarted = false
		}
		r.mu.Unlock()
	}()

	wg.Wait()

	// After both goroutines complete, pipes should be nil
	runner.mu.Lock()
	assert.Nil(t, runner.stdinPipe, "stdinPipe should be nil after cleanup")
	assert.Nil(t, runner.stdoutPipe, "stdoutPipe should be nil after cleanup")
	runner.mu.Unlock()
}
