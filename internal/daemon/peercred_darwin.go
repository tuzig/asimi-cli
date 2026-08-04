package daemon

import (
	"net"
	"os/user"
	"strconv"

	"golang.org/x/sys/unix"
)

// unixPeerUsername extracts the verified username from a Unix socket
// peer credential (getsockopt with SOL_LOCAL/LOCAL_PEERCRED on macOS).
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

	xucred, err := unix.GetsockoptXucred(int(f.Fd()), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return ""
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(xucred.Uid), 10))
	if err != nil {
		return ""
	}
	return u.Username
}
