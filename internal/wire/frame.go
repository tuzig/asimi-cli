// Package wire defines helpers for the standard msgpack-RPC wire format
// used between the court daemon and the TUI over a unix socket.
//
// Standard msgpack-RPC uses array envelopes:
//
//	Request:      [0, msgid, method, params]
//	Response:     [1, msgid, error, result]
//	Notification: [2, method, params]
//
// The envelope is symmetric: either side may originate a request, which
// lets the daemon prompt the TUI for approval using the same machinery
// the TUI uses to call the daemon.
package wire

// Error is the wire representation of a handler failure, placed in the
// error slot (index 2) of a standard msgpack-RPC response.
type Error struct {
	Code    int32  `msgpack:"c"`
	Message string `msgpack:"m"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Error codes used by the transport itself. Handler-specific codes live
// above 1000 to avoid collisions.
const (
	CodeUnknownMethod    int32 = 1
	CodePeerDisconnected int32 = 2
	CodeFrameTooLarge    int32 = 3
	CodeDecodeFailed     int32 = 4
	CodeNotReady         int32 = 5
)

const (
	FrameRequest  uint8 = 0
	FrameResponse uint8 = 1
	FrameNotify   uint8 = 2
)

// NewError builds a transport *Error.
func NewError(code int32, msg string) *Error {
	return &Error{Code: code, Message: msg}
}
