package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/afittestide/asimi/internal/rpc"
	tea "github.com/charmbracelet/bubbletea"
)

// autostartReadyTimeout bounds how long the TUI waits for a spawned
// daemon to bind its listener. Generous; podman init on a cold macOS
// machine can be slow, but much past this and something's wrong.
const autostartReadyTimeout = 10 * time.Second

// connectOrStartDaemon resolves the default socket path and either
// connects to a running daemon or spawns one (self-exec "asimi
// daemon") and waits for it to be ready. Returns the live net.Conn
// and the resolved socket path.
//
// Errors from the spawn path are annotated so the user knows whether
// the bind, the dial, or the readiness timeout failed.
func connectOrStartDaemon(ctx context.Context) (net.Conn, string, error) {
	path, err := rpc.SocketPath()
	if err != nil {
		return nil, "", fmt.Errorf("resolve socket path: %w", err)
	}

	// Fast path: socket is live.
	if c, err := rpc.Dial(path); err == nil {
		return c, path, nil
	} else if !isSocketAbsent(err) {
		// Stale socket file (nothing listening). Dial gives us
		// ECONNREFUSED in that case; other errors are fatal.
		if !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, path, fmt.Errorf("dial %s: %w", path, err)
		}
		// Fall through and spawn; Listen on the daemon side will
		// clean up the stale file.
	}

	if err := spawnDaemonAndWait(ctx, path); err != nil {
		return nil, path, err
	}

	c, err := rpc.Dial(path)
	if err != nil {
		return nil, path, fmt.Errorf("dial after spawn: %w", err)
	}
	return c, path, nil
}

// isSocketAbsent reports whether err indicates the socket file isn't
// there yet (vs. there but not accepting).
func isSocketAbsent(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT)
}

// spawnDaemonAndWait starts `asimi daemon` as a child process, hands
// it a readiness pipe via the ASIMI_READY_FD env var, and blocks until
// the child writes a byte (up to autostartReadyTimeout).
func spawnDaemonAndWait(ctx context.Context, socketPath string) error {
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self exec: %w", err)
	}

	readR, readW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}
	defer readR.Close()

	cmd := exec.CommandContext(ctx, selfPath, "daemon")
	cmd.Env = append(os.Environ(), "ASIMI_READY_FD=3")
	cmd.ExtraFiles = []*os.File{readW}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = readW.Close()
		return fmt.Errorf("spawn daemon: %w", err)
	}
	// Parent closes its copy of the write end so the child's close on
	// that fd actually hits EOF in the reader.
	_ = readW.Close()

	// Detach so the daemon outlives this TUI. Go's os/exec will
	// otherwise reap the child process via Wait, which we don't want
	// to do from the TUI.
	go func() { _ = cmd.Wait() }()

	ready := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := io.ReadFull(readR, buf)
		ready <- err
	}()

	select {
	case err := <-ready:
		if err != nil {
			return fmt.Errorf("daemon never signalled ready: %w", err)
		}
		return nil
	case <-time.After(autostartReadyTimeout):
		return fmt.Errorf("daemon did not become ready within %s", autostartReadyTimeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// installDaemonAutostart wires the TUI to a daemon, spawning one if
// no live socket is found. Symmetrical to installDaemonSocket but
// responsible for starting the daemon.
//
// Opt-in via ASIMI_DAEMON=1.
func installDaemonAutostart(ctx context.Context, model *TUIModel) (func(*tea.Program), error) {
	if model == nil || model.shogunate == nil {
		return nil, fmt.Errorf("installDaemonAutostart: tui model or shogunate is nil")
	}

	c, _, err := connectOrStartDaemon(ctx)
	if err != nil {
		return nil, err
	}
	client := rpc.New(c, rpc.Options{})
	go func() { _ = client.Serve() }()

	local := model.shogunate
	model.shogunate = rpc.NewLoopbackShogunate(client, local)

	return func(p *tea.Program) {
		rpc.RegisterApprovalHandler(client, teaSender{p})
		rpc.RegisterEditorHandler(client, teaSender{p})
	}, nil
}
