// Package wire defines the MessagePack RPC frame format used between the
// shogunate daemon and the TUI over a unix socket.
//
// Every frame is a single MessagePack-encoded Frame struct preceded by a
// 4-byte big-endian length prefix. A frame is one of:
//
//	request      t=FrameRequest,  id, m, p       → expects a response
//	response     t=FrameResponse, id,    r | e   → replies to a request
//	notification t=FrameNotify,       m, p       → fire-and-forget
//
// The envelope is symmetric: either side may originate a request, which
// lets the daemon prompt the TUI for approval using the same machinery
// the TUI uses to call the daemon.
package wire

// FrameType identifies the role of a frame.
type FrameType uint8

const (
	FrameRequest  FrameType = 0
	FrameResponse FrameType = 1
	FrameNotify   FrameType = 2
)

// MaxFrameSize caps a single frame at 16 MiB. The reader rejects larger
// frames to protect against runaway peers.
const MaxFrameSize = 16 * 1024 * 1024

// Frame is the wire envelope. Params and result are carried as raw bytes
// so the dispatcher can route a frame to the right handler before the
// handler decodes its typed payload.
type Frame struct {
	T  FrameType `msgpack:"t"`
	ID uint64    `msgpack:"id,omitempty"`
	M  string    `msgpack:"m,omitempty"`
	P  []byte    `msgpack:"p,omitempty"`
	R  []byte    `msgpack:"r,omitempty"`
	E  *Error    `msgpack:"e,omitempty"`
}

// Error is the wire representation of a handler failure.
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

// NewError builds a transport *Error.
func NewError(code int32, msg string) *Error {
	return &Error{Code: code, Message: msg}
}
