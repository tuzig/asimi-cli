package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/rpc"
	tea "github.com/charmbracelet/bubbletea"
)

// autostartReadyTimeout bounds how long the TUI waits for a spawned
// daemon to bind its listener. Generous; podman init on a cold macOS
// machine can be slow, but much past this and something's wrong.
const autostartReadyTimeout = 10 * time.Second

// connectOrStartDaemon resolves the default socket path and returns a
// live, serving rpc.Conn — reusing a running daemon only if it proves
// responsive, otherwise spawning a fresh one (self-exec "asimi daemon").
//
// A bare unix-socket Dial is not proof of life: the kernel completes the
// connection into the listen backlog even when the daemon process is
// frozen (SIGSTOP) or otherwise wedged. So a successful Dial is followed
// by a liveness probe, and a daemon that fails it is evicted and
// replaced. Errors from the spawn path are annotated so the user knows
// whether the bind, the dial, or the readiness timeout failed.
func connectOrStartDaemon(ctx context.Context) (*rpc.Conn, string, error) {
	path, err := rpc.SocketPath()
	if err != nil {
		return nil, "", fmt.Errorf("resolve socket path: %w", err)
	}

	// Fast path: a running daemon that actually answers a request.
	if c, err := rpc.Dial(path); err == nil {
		conn := rpc.New(c, rpc.Options{})
		go func() {
			if err := conn.Serve(); err != nil {
				slog.Warn("autostart: conn.Serve error (fast path)", "err", err)
			}
		}()
		if daemonResponds(ctx, conn) {
			return conn, path, nil
		}
		// Dialed but unresponsive — frozen or wedged. Evict and respawn.
		_ = conn.Close()
		evictWedgedDaemon(path)
	} else if !isSocketAbsent(err) {
		// Stale socket file (nothing listening) gives ECONNREFUSED;
		// fall through and spawn. Any other dial error is fatal.
		if !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, path, fmt.Errorf("dial %s: %w", path, err)
		}
	}

	if err := spawnDaemonAndWait(ctx, path); err != nil {
		return nil, path, err
	}

	c, err := rpc.Dial(path)
	if err != nil {
		return nil, path, fmt.Errorf("dial after spawn: %w", err)
	}
	conn := rpc.New(c, rpc.Options{})
	go func() {
		if err := conn.Serve(); err != nil {
			slog.Warn("autostart: conn.Serve error (after spawn)", "err", err)
		}
	}()
	return conn, path, nil
}

// isSocketAbsent reports whether err indicates the socket file isn't
// there yet (vs. there but not accepting).
func isSocketAbsent(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT)
}

// daemonProbeTimeout bounds the liveness probe against a daemon we
// dialed successfully. CourtEdictKey is pure — no LLM, DB, or I/O — so a
// healthy daemon answers well within this; exceeding it means the daemon
// is frozen or wedged.
const daemonProbeTimeout = 2 * time.Second

// daemonResponds reports whether the daemon behind conn answers a cheap
// request before daemonProbeTimeout elapses.
func daemonResponds(ctx context.Context, conn *rpc.Conn) bool {
	probeCtx, cancel := context.WithTimeout(ctx, daemonProbeTimeout)
	defer cancel()
	_, err := conn.Call(probeCtx, rpc.MethodCourtEdictKey, nil)
	return err == nil
}

// evictWedgedDaemon clears a daemon that owns the socket at path but
// stopped answering, so a freshly spawned one can bind. It SIGKILLs the
// PID from the daemon's pidfile — SIGKILL reaches even SIGSTOP'd
// processes, unlike the SIGTERM `pkill` sends — and removes the socket
// file, since rpc.Listen's own stale-socket check is fooled by the same
// backlog behaviour that fools Dial.
func evictWedgedDaemon(path string) {
	pidPath := path + ".pid"
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	_ = os.Remove(path)
	_ = os.Remove(pidPath)
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

	args := []string{"daemon"}
	if cli.Debug {
		args = append(args, "--debug")
	}
	cmd := exec.CommandContext(ctx, selfPath, args...)
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

	conn, _, err := connectOrStartDaemon(ctx)
	if err != nil {
		return nil, err
	}

	// Send the SetContext handshake required before any other RPC call.
	// The daemon disconnects clients that fail to handshake within 30s.
	repoInfo := repo.GetRepoInfo()
	username := "guest"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	if err := rpc.NewShogunateClient(conn).SetContext(ctx, rpc.SetContextParams{
		Project:      repoInfo.Slug,
		Username:     username,
		ProjectRoot:  repoInfo.ProjectRoot,
		WorktreePath: repoInfo.WorktreePath,
		Branch:       repoInfo.Branch,
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("installDaemonAutostart: handshake failed: %w", err)
	}

	local := model.shogunate
	model.shogunate = rpc.NewLoopbackShogunate(conn, local)

	return func(p *tea.Program) {
		rpc.RegisterApprovalHandler(conn, teaSender{p})
		rpc.RegisterEditorHandler(conn, teaSender{p})
	}, nil
}
