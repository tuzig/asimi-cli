package daemon

import (
	"net"
	"os/user"
	"strconv"
	"syscall"
)

// unixPeerUsername extracts the verified username from a Unix socket
// peer credential (SO_PEERCRED on Linux).
func unixPeerUsername(conn net.Conn) string {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return ""
	}
	f, err := uc.File()
	if err != nil {
		return ""
	}
	defer f.Close()

	ucred, err := syscall.GetsockoptUcred(int(f.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return ""
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(ucred.Uid), 10))
	if err != nil {
		return ""
	}
	return u.Username
}
