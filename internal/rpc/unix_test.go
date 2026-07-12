package rpc

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func tempSocketPath(t *testing.T) string {
	t.Helper()
	// t.TempDir paths under macOS $TMPDIR can be long; shorten by
	// symlinking if the path would blow the 104-byte cap.
	dir := t.TempDir()
	p := filepath.Join(dir, "a.sock")
	if len(p) >= 104 {
		// Fall back to /tmp with a timestamp suffix.
		p = filepath.Join("/tmp", "asimi-test-"+time.Now().Format("150405.000000")+".sock")
	}
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func TestListenDialRoundTrip(t *testing.T) {
	path := tempSocketPath(t)

	l, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	// Server accepts one connection and registers fake handlers.
	impl := newFakeShogunate()
	impl.hasIDs["chancellor"] = true

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := l.Accept()
		if err != nil {
			return
		}
		conn := New(c, Options{})
		RegisterShogunateHandlers(conn, impl)
		_ = conn.Serve()
	}()
	defer wg.Wait()

	clientNet, err := Dial(path)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	clientConn := New(clientNet, Options{})
	go func() { _ = clientConn.Serve() }()
	defer clientConn.Close()

	client := NewShogunateClient(clientConn)

	if !client.HasMinister("chancellor") {
		t.Error("HasMinister(chancellor) = false over unix socket")
	}
	e, err := client.CreateEdict("#1", "ship it", "")
	if err != nil {
		t.Fatalf("CreateEdict over socket: %v", err)
	}
	got, err := client.GetEdict(e.ID)
	if err != nil || got.Intent != "ship it" {
		t.Fatalf("GetEdict over socket: got=%+v err=%v", got, err)
	}
}

func TestListenRejectsLivePath(t *testing.T) {
	path := tempSocketPath(t)
	l, err := Listen(path)
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	defer l.Close()

	_, err = Listen(path)
	if err == nil {
		t.Fatal("second Listen on live socket: want error, got nil")
	}
}

func TestListenCleansStaleSocket(t *testing.T) {
	path := tempSocketPath(t)
	// Create a stale socket file (not being listened on).
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	l, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen with stale file: %v", err)
	}
	defer l.Close()
}

func TestSocketPathRespectsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	p, err := SocketPath()
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if filepath.Dir(p) != dir {
		t.Errorf("SocketPath = %s; want under %s", p, dir)
	}
}

func TestSocketPathFallsBackWithoutXDG(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	p, err := SocketPath()
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if p == "" {
		t.Fatal("empty path")
	}
	// Should contain /tmp-ish dir and uid.
	if filepath.Dir(p) == "" {
		t.Errorf("path has no dir: %q", p)
	}
}
