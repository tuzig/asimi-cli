package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestFrameRoundTripRequest(t *testing.T) {
	raw, err := Encode(map[string]any{"foo": "bar", "n": 42})
	if err != nil {
		t.Fatalf("encode params: %v", err)
	}
	in := &Frame{T: FrameRequest, ID: 7, M: "DoThing", P: raw}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.T != FrameRequest || out.ID != 7 || out.M != "DoThing" {
		t.Fatalf("header mismatch: %+v", out)
	}
	var got map[string]any
	if err := Decode(out.P, &got); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if got["foo"] != "bar" {
		t.Fatalf("foo = %v", got["foo"])
	}
}

func TestFrameRoundTripResponseWithError(t *testing.T) {
	in := &Frame{T: FrameResponse, ID: 9, E: NewError(1234, "boom")}
	var buf bytes.Buffer
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.E == nil || out.E.Code != 1234 || out.E.Message != "boom" {
		t.Fatalf("error not preserved: %+v", out.E)
	}
	if len(out.R) != 0 {
		t.Fatalf("unexpected result payload: %x", out.R)
	}
}

func TestFrameRoundTripNotification(t *testing.T) {
	raw, err := Encode(struct {
		ChannelID string `msgpack:"channel_id"`
		Text      string `msgpack:"text"`
	}{ChannelID: "ruling", Text: "hello"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	in := &Frame{T: FrameNotify, M: "stream.chunk", P: raw}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.ID != 0 {
		t.Fatalf("notification must have ID=0, got %d", out.ID)
	}
	if out.M != "stream.chunk" {
		t.Fatalf("method: %q", out.M)
	}
	var got struct {
		ChannelID string `msgpack:"channel_id"`
		Text      string `msgpack:"text"`
	}
	if err := Decode(out.P, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ChannelID != "ruling" || got.Text != "hello" {
		t.Fatalf("payload mismatch: %+v", got)
	}
}

func TestMultipleFramesOnStream(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 5; i++ {
		raw, _ := Encode(map[string]int{"i": i})
		if err := WriteFrame(&buf, &Frame{T: FrameNotify, M: "tick", P: raw}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	for i := 0; i < 5; i++ {
		f, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		var got map[string]int
		if err := Decode(f.P, &got); err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		if got["i"] != i {
			t.Fatalf("frame %d: i=%d", i, got["i"])
		}
	}
	if _, err := ReadFrame(&buf); !errors.Is(err, io.EOF) {
		t.Fatalf("trailing read: want EOF, got %v", err)
	}
}

func TestReadFrameRejectsOversizedHeader(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], MaxFrameSize+1)
	buf.Write(hdr[:])
	_, err := ReadFrame(&buf)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	big := make([]byte, MaxFrameSize+1)
	in := &Frame{T: FrameRequest, ID: 1, M: "x", P: big}
	err := WriteFrame(io.Discard, in)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}

func TestReadFrameEOFBeforeHeader(t *testing.T) {
	var buf bytes.Buffer
	_, err := ReadFrame(&buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestReadFrameTruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 100) // promise 100 bytes
	buf.Write(hdr[:])
	buf.Write([]byte{0x80}) // deliver 1
	_, err := ReadFrame(&buf)
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("want non-EOF read error, got %v", err)
	}
}

func TestEncodeNilLeavesRawEmpty(t *testing.T) {
	raw, err := Encode(nil)
	if err != nil {
		t.Fatalf("encode nil: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("want empty raw, got %x", raw)
	}
	var dest map[string]any
	if err := Decode(raw, &dest); err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if dest != nil {
		t.Fatalf("dest unexpectedly populated: %v", dest)
	}
}
