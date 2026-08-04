//go:build !linux && !darwin

package daemon

import (
	"net"
)

// unixPeerUsername extracts the verified username from a Unix socket
// peer credential. This fallback returns "" on unsupported platforms.
func unixPeerUsername(conn net.Conn) string {
	if _, ok := conn.(*net.UnixConn); !ok {
		return ""
	}
	return ""
}
