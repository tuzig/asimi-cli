package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/rpc"
	"github.com/afittestide/asimi/internal/shogunateapi"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
)

// daemonFake is a minimal shogunateapi.Client stub for daemon tests.
// It satisfies the interface via embedded nil Client; only the methods
// the test actually calls are implemented.
type daemonFake struct {
	shogunateapi.Client
	mu      sync.Mutex
	created int
}

func (d *daemonFake) HasMinister(id string) bool { return id == "chancellor" }
func (d *daemonFake) CreateEdict(issueRef, intent string) (*storage.Edict, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.created++
	return &storage.Edict{ID: uint(d.created), IssueRef: issueRef, Intent: intent}, nil
}
func (d *daemonFake) Subscribe(context.Context) <-chan any {
	ch := make(chan any)
	// Daemon won't emit anything in this test — return an open chan;
	// caller will stop reading when ctx cancels.
	return ch
}

// TestDaemonServesClientsEndToEnd wires serveClients over a real unix
// socket, dials as a client, exercises HasMinister + CreateEdict, then
// cancels and expects a clean shutdown.
func TestDaemonServesClientsEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.sock")
	if len(path) >= 104 {
		path = filepath.Join("/tmp", "asimi-d-"+time.Now().Format("150405.000000")+".sock")
		t.Cleanup(func() { _ = os.Remove(path) })
	}

	listener, err := rpc.Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	impl := &daemonFake{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() { serveDone <- serveClients(ctx, listener, impl) }()

	// Connect as a client.
	c, err := rpc.Dial(path)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	clientConn := rpc.New(c, rpc.Options{})
	go func() { _ = clientConn.Serve() }()
	defer clientConn.Close()

	client := rpc.NewShogunateClient(clientConn)

	if !client.HasMinister("chancellor") {
		t.Error("HasMinister failed through daemon socket")
	}
	e, err := client.CreateEdict("#1", "via socket")
	if err != nil {
		t.Fatalf("CreateEdict: %v", err)
	}
	if e.Intent != "via socket" {
		t.Errorf("edict = %+v", e)
	}

	// Stop the daemon; Accept should return, serveClients should exit.
	cancel()
	_ = listener.Close()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Errorf("serveClients exited with: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveClients didn't shut down")
	}
}

var _ shogunate.Snapshot // keep the shogunate import warm for the test file
