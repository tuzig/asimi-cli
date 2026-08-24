package daemon

import (
	"net"
	"os/user"
	"path/filepath"
	"testing"
)

// TestUnixPeerUsername_NonUnixConn verifies that unixPeerUsername
// returns "" for a non-Unix connection (e.g. net.Pipe).
func TestUnixPeerUsername_NonUnixConn(t *testing.T) {
	client, _ := net.Pipe()
	defer client.Close()

	if got := unixPeerUsername(client); got != "" {
		t.Errorf("unixPeerUsername(net.Pipe) = %q, want %q", got, "")
	}
}

// TestUnixPeerUsername_RealSocket verifies that unixPeerUsername
// returns the current OS user when connecting over a real Unix socket.
func TestUnixPeerUsername_RealSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "s.sock")

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		serverConn, err := listener.Accept()
		if err != nil {
			t.Logf("Accept: %v", err)
			return
		}
		defer serverConn.Close()

		got := unixPeerUsername(serverConn)
		currentUser, err := user.Current()
		if err != nil {
			t.Logf("user.Current: %v", err)
			// Just check it's non-empty when we can't verify the exact value.
			if got == "" {
				t.Error("unixPeerUsername returned empty on real socket")
			}
			return
		}
		if got != currentUser.Username {
			t.Errorf("unixPeerUsername = %q, want %q", got, currentUser.Username)
		}
	}()

	client, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	<-done
}

// TestUnixPeerUsername_ClosedSocket verifies graceful handling when
// File() fails (e.g. connection already closed).
func TestUnixPeerUsername_ClosedSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "s.sock")

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		serverConn, err := listener.Accept()
		if err != nil {
			t.Logf("Accept: %v", err)
			return
		}
		// Close immediately so File() fails on the server side.
		serverConn.Close()

		got := unixPeerUsername(serverConn)
		if got != "" {
			t.Errorf("unixPeerUsername on closed conn = %q, want %q", got, "")
		}
	}()

	client, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	client.Close()

	<-done
}
