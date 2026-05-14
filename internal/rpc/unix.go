package rpc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// DefaultSocketName is the file name used under the socket directory.
const DefaultSocketName = "asimi.sock"

// SocketPath returns the path the user-scoped daemon listens on. The
// first location that can host a path under the macOS 104-byte sun_path
// cap wins:
//
//  1. $XDG_RUNTIME_DIR/asimi.sock         (Linux, modern systemds)
//  2. /tmp/asimi-$UID.sock                (macOS fallback)
//
// The containing directory is created with mode 0700 if we end up
// using XDG_RUNTIME_DIR and it doesn't already exist.
func SocketPath() (string, error) {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		p := filepath.Join(xdg, DefaultSocketName)
		if len(p) < 104 {
			return p, nil
		}
	}
	uid := os.Getuid()
	p := filepath.Join(os.TempDir(), "asimi-"+strconv.Itoa(uid)+".sock")
	if len(p) >= 104 {
		return "", fmt.Errorf("rpc: socket path too long (%d bytes): %s", len(p), p)
	}
	return p, nil
}

// Listen opens a unix-domain listener at path, removing any stale
// socket file first. The socket and (where possible) its containing
// directory are created with owner-only permissions.
func Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("rpc: mkdir socket dir: %w", err)
	}
	// Remove stale socket if it exists but nothing is listening.
	if _, err := os.Stat(path); err == nil {
		if isListening(path) {
			return nil, fmt.Errorf("rpc: socket %s is already in use", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("rpc: remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("rpc: stat socket: %w", err)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = l.Close()
		return nil, fmt.Errorf("rpc: chmod socket: %w", err)
	}
	return l, nil
}

// Dial opens a unix-domain connection to the listener at path.
func Dial(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

// isListening probes a path: reports true if someone accepts connections.
// Used to distinguish "live daemon" from "stale socket file".
func isListening(path string) bool {
	c, err := net.Dial("unix", path)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
