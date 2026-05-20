package wire

import (
	"errors"
	"fmt"
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

// MaxFrameSize caps a single frame at 16 MiB for defensive protection.
const MaxFrameSize = 16 * 1024 * 1024

// ErrFrameTooLarge is returned when a peer announces a frame above MaxFrameSize.
var ErrFrameTooLarge = errors.New(fmt.Sprintf("wire: frame exceeds %d bytes", MaxFrameSize))


// ReadFrame reads one msgpack-RPC envelope from r and decodes it.
// Returns io.EOF on a clean peer close.
func ReadFrame(r io.Reader) (*Frame, error) {
	dec := msgpack.NewDecoder(r)
	// DecodeRaw reads the next complete msgpack value from the stream.
	raw, err := dec.DecodeRaw()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, err
		}
		return nil, fmt.Errorf("wire: read frame: %w", err)
	}
	if len(raw) > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	var elems []msgpack.RawMessage
	if err := msgpack.Unmarshal(raw, &elems); err != nil {
		return nil, fmt.Errorf("wire: unmarshal frame array: %w", err)
	}
	f, err := parseEnvelope(elems)
	if err != nil {
		return nil, fmt.Errorf("wire: parse envelope: %w", err)
	}
	return f, nil
}

// Frame holds the decoded fields of a standard msgpack-RPC envelope.
// It replaces the old bespoke Frame struct while preserving the same
// field names so callers in internal/rpc need minimal changes.
type Frame struct {
	// T is 0=Request, 1=Response, 2=Notification
	T uint8
	// ID is the request/response correlation id (msgid)
	ID uint64
	// M is the method name (requests and notifications)
	M string
	// P is the raw msgpack params (requests and notifications)
	P []byte
	// R is the raw msgpack result (responses)
	R []byte
	// E is the wire error (responses)
	E *Error
}

func parseEnvelope(elems []msgpack.RawMessage) (*Frame, error) {
	if len(elems) < 1 {
		return nil, errors.New("empty envelope")
	}
	var t uint8
	if err := msgpack.Unmarshal(elems[0], &t); err != nil {
		return nil, fmt.Errorf("decode type: %w", err)
	}

	switch t {
	case 0: // Request: [0, msgid, method, params]
		if len(elems) != 4 {
			return nil, fmt.Errorf("request envelope wants 4 elements, got %d", len(elems))
		}
		var id uint64
		if err := msgpack.Unmarshal(elems[1], &id); err != nil {
			return nil, fmt.Errorf("decode msgid: %w", err)
		}
		var m string
		if err := msgpack.Unmarshal(elems[2], &m); err != nil {
			return nil, fmt.Errorf("decode method: %w", err)
		}
		return &Frame{T: 0, ID: id, M: m, P: elems[3]}, nil

	case 1: // Response: [1, msgid, error, result]
		if len(elems) != 4 {
			return nil, fmt.Errorf("response envelope wants 4 elements, got %d", len(elems))
		}
		var id uint64
		if err := msgpack.Unmarshal(elems[1], &id); err != nil {
			return nil, fmt.Errorf("decode msgid: %w", err)
		}
		f := &Frame{T: 1, ID: id}
		// error slot: empty raw means nil (success)
		if len(elems[2]) > 0 {
			var errVal any
			if err := msgpack.Unmarshal(elems[2], &errVal); err != nil {
				return nil, fmt.Errorf("decode error slot: %w", err)
			}
			if errVal != nil {
				// Try to decode as our structured Error first
				var werr Error
				if err := msgpack.Unmarshal(elems[2], &werr); err == nil && werr.Message != "" {
					f.E = &werr
				} else {
					// Fallback: string error
					var s string
					if err := msgpack.Unmarshal(elems[2], &s); err == nil {
						f.E = &Error{Code: 0, Message: s}
					}
				}
			}
		}
		// result slot: empty raw means nil result
		if len(elems[3]) > 0 {
			f.R = elems[3]
		}
		return f, nil

	case 2: // Notification: [2, method, params]
		if len(elems) != 3 {
			return nil, fmt.Errorf("notification envelope wants 3 elements, got %d", len(elems))
		}
		var m string
		if err := msgpack.Unmarshal(elems[1], &m); err != nil {
			return nil, fmt.Errorf("decode method: %w", err)
		}
		return &Frame{T: 2, M: m, P: elems[2]}, nil

	default:
		return nil, fmt.Errorf("unknown envelope type %d", t)
	}
}

// WriteFrame encodes f as a standard msgpack-RPC envelope and writes it
// to w. The caller is responsible for serialising concurrent writes to w.
func WriteFrame(w io.Writer, f *Frame) error {
	var err error
	var raw []byte
	switch f.T {
	case 0: // Request
		var p any
		if f.P != nil {
			p = msgpack.RawMessage(f.P)
		} else {
			p = nil
		}
		raw, err = msgpack.Marshal([]any{0, f.ID, f.M, p})
	case 1: // Response
		var e any
		if f.E != nil {
			e = f.E
		} else {
			e = nil
		}
		var r any
		if f.R != nil {
			r = msgpack.RawMessage(f.R)
		} else {
			r = nil
		}
		raw, err = msgpack.Marshal([]any{1, f.ID, e, r})
	case 2: // Notification
		var p any
		if f.P != nil {
			p = msgpack.RawMessage(f.P)
		} else {
			p = nil
		}
		raw, err = msgpack.Marshal([]any{2, f.M, p})
	default:
		return fmt.Errorf("wire: unknown frame type %d", f.T)
	}
	if err != nil {
		return fmt.Errorf("wire: encode frame: %w", err)
	}
	if len(raw) > MaxFrameSize {
		return ErrFrameTooLarge
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	return nil
}

// Encode marshals v to a byte slice suitable for Frame.P or Frame.R.
// A nil v produces a nil slice.
func Encode(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return msgpack.Marshal(v)
}

// Decode unmarshals raw into v. An empty raw is a no-op and leaves v at
// its zero value.
func Decode(raw []byte, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return msgpack.Unmarshal(raw, v)
}
