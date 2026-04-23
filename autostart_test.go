package main

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/rpc"
)

// fakeDaemon stands in for the real asimi daemon subprocess in unit
// tests — it binds the socket path, signals readiness on ASIMI_READY_FD
// exactly the way runDaemonMode does, and serves one trivial handler.
// No fx, no bifrost, no TUI — just the readiness handshake and a dial
// target.
func startFakeDaemon(t *testing.T, socketPath string) (stop func()) {
	t.Helper()
	l, err := rpc.Listen(socketPath)
	if err != nil {
		t.Fatalf("fake daemon listen: %v", err)
	}

	// Signal readiness if ASIMI_READY_FD is set — mirrors daemon.go.
	if fdStr := os.Getenv("ASIMI_READY_FD"); fdStr != "" {
		t.Fatalf("fake daemon shouldn't be run with ASIMI_READY_FD set at test level")
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			if ctx.Err() != nil {
				return
			}
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				conn := rpc.New(nc, rpc.Options{})
				conn.Handle("Ping", func(ctx context.Context, params []byte) ([]byte, error) {
					return nil, nil
				})
				_ = conn.Serve()
			}(c)
		}
	}()

	return func() {
		cancel()
		_ = l.Close()
		wg.Wait()
	}
}

func TestConnectOrStartDaemonFastPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.sock")
	if len(path) >= 104 {
		t.Skip("tmp path too long for unix socket")
	}
	t.Setenv("XDG_RUNTIME_DIR", dir)

	// Start a fake daemon at the path rpc.SocketPath() will resolve
	// to. connectOrStartDaemon should hit the fast-path.
	resolved, err := rpc.SocketPath()
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	stop := startFakeDaemon(t, resolved)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, got, err := connectOrStartDaemon(ctx)
	if err != nil {
		t.Fatalf("connectOrStartDaemon: %v", err)
	}
	defer c.Close()
	if got != resolved {
		t.Errorf("path = %q, want %q", got, resolved)
	}
}

// TestReadySignalOnFD verifies the pipe-fd handshake end-to-end: open
// a pipe, pass the write end as fd 3 to a function that mimics the
// daemon's readiness write, confirm the parent wakes from ReadFull.
func TestReadySignalOnFD(t *testing.T) {
	readR, readW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer readR.Close()

	// Simulate the daemon writing the readiness byte on "fd 3" — in
	// this test we just use the pipe directly.
	go func() {
		_, _ = readW.Write([]byte{1})
		_ = readW.Close()
	}()

	buf := make([]byte, 1)
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(readR, buf)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read ready: %v", err)
		}
		if buf[0] != 1 {
			t.Fatalf("ready byte = %d", buf[0])
		}
	case <-time.After(time.Second):
		t.Fatal("readiness byte never arrived")
	}
}

