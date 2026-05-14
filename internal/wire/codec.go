package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

// ErrFrameTooLarge is returned when a peer announces a frame above MaxFrameSize.
var ErrFrameTooLarge = errors.New("wire: frame exceeds MaxFrameSize")

// WriteFrame encodes f and writes it to w with a 4-byte big-endian length
// prefix. The caller is responsible for serialising concurrent writes to w.
func WriteFrame(w io.Writer, f *Frame) error {
	payload, err := msgpack.Marshal(f)
	if err != nil {
		return fmt.Errorf("wire: encode frame: %w", err)
	}
	if len(payload) > MaxFrameSize {
		return ErrFrameTooLarge
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return nil
}

// ReadFrame reads one length-prefixed frame from r and decodes it.
// Returns io.EOF on a clean peer close before the next length header.
func ReadFrame(r io.Reader) (*Frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("wire: read payload: %w", err)
	}
	var f Frame
	if err := msgpack.Unmarshal(buf, &f); err != nil {
		return nil, fmt.Errorf("wire: decode frame: %w", err)
	}
	return &f, nil
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
