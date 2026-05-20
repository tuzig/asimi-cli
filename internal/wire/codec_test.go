package wire

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
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

func TestFrameRoundTripResponseWithResult(t *testing.T) {
	raw, err := Encode(map[string]int{"sum": 5})
	if err != nil {
		t.Fatalf("encode result: %v", err)
	}
	in := &Frame{T: FrameResponse, ID: 3, R: raw}
	var buf bytes.Buffer
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.T != FrameResponse || out.ID != 3 {
		t.Fatalf("header mismatch: %+v", out)
	}
	if out.E != nil {
		t.Fatalf("unexpected error: %+v", out.E)
	}
	var got map[string]int
	if err := Decode(out.R, &got); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if got["sum"] != 5 {
		t.Fatalf("sum = %v", got["sum"])
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

func TestReadFrameRejectsOversizedPayload(t *testing.T) {
	// Build a valid msgpack-RPC request array larger than MaxFrameSize.
	big := make([]byte, MaxFrameSize+1)
	arr, _ := msgpack.Marshal([]any{0, uint64(1), "x", big})
	_, err := ReadFrame(bytes.NewReader(arr))
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

func TestReadFrameEOFOnEmptyStream(t *testing.T) {
	var buf bytes.Buffer
	_, err := ReadFrame(&buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestReadFrameTruncatedValue(t *testing.T) {
	// Start of a 4-element array but truncated mid-stream.
	// With standard msgpack-RPC streaming, this returns EOF.
	buf := bytes.NewReader([]byte{0x94, 0x00, 0x01}) // [0, 0, ...
	_, err := ReadFrame(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
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

func TestStandardEnvelopeTypes(t *testing.T) {
	tests := []struct {
		name   string
		frame  *Frame
		expect []any
	}{
		{
			name:   "request",
			frame:  &Frame{T: FrameRequest, ID: 5, M: "Foo", P: []byte{0xc0}},
			expect: []any{uint8(0), uint64(5), "Foo", msgpack.RawMessage{0xc0}},
		},
		{
			name:   "response",
			frame:  &Frame{T: FrameResponse, ID: 5, R: []byte{0xc0}},
			expect: []any{uint8(1), uint64(5), nil, msgpack.RawMessage{0xc0}},
		},
		{
			name:   "response_with_error",
			frame:  &Frame{T: FrameResponse, ID: 5, E: &Error{Code: 1, Message: "bad"}},
			expect: []any{uint8(1), uint64(5), &Error{Code: 1, Message: "bad"}, nil},
		},
		{
			name:   "notification",
			frame:  &Frame{T: FrameNotify, M: "bar", P: []byte{0xc0}},
			expect: []any{uint8(2), "bar", msgpack.RawMessage{0xc0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, tt.frame); err != nil {
				t.Fatalf("write: %v", err)
			}
			var got []msgpack.RawMessage
			if err := msgpack.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(got) != len(tt.expect) {
				t.Fatalf("want %d elements, got %d", len(tt.expect), len(got))
			}
		})
	}
}

// Defensive-input tests for parseEnvelope error branches.

func TestReadFrameEmptyEnvelope(t *testing.T) {
	// empty msgpack array
	data, _ := msgpack.Marshal([]any{})
	_, err := ReadFrame(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for empty envelope")
	}
}

func TestReadFrameUnknownType(t *testing.T) {
	// type 99 is not a valid envelope type
	data, _ := msgpack.Marshal([]any{99, "whatever"})
	_, err := ReadFrame(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for unknown envelope type")
	}
}

func TestReadFrameMalformedRequestMsgid(t *testing.T) {
	// [0, "not-a-number", "method", nil]
	data, _ := msgpack.Marshal([]any{0, "not-a-number", "method", nil})
	_, err := ReadFrame(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed request msgid")
	}
}

func TestReadFrameMalformedRequestMethod(t *testing.T) {
	// [0, 1, 42, nil] — method slot is an int, not a string
	data, _ := msgpack.Marshal([]any{0, 1, 42, nil})
	_, err := ReadFrame(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed request method")
	}
}

func TestReadFrameMalformedResponseMsgid(t *testing.T) {
	// [1, "not-a-number", nil, nil]
	data, _ := msgpack.Marshal([]any{1, "not-a-number", nil, nil})
	_, err := ReadFrame(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed response msgid")
	}
}

func TestReadFrameMalformedNotificationMethod(t *testing.T) {
	// [2, 42, nil] — method slot is an int, not a string
	data, _ := msgpack.Marshal([]any{2, 42, nil})
	_, err := ReadFrame(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for malformed notification method")
	}
}

func TestReadFrameResponseStringErrorFallback(t *testing.T) {
	// [1, 7, "plain string error", nil]
	// This exercises the fallback path where the error slot is a
	// plain string rather than a structured *wire.Error.
	data, _ := msgpack.Marshal([]any{1, uint64(7), "plain string error", nil})
	f, err := ReadFrame(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.E == nil {
		t.Fatal("expected error to be decoded")
	}
	if f.E.Code != 0 {
		t.Fatalf("expected code 0 for string fallback, got %d", f.E.Code)
	}
	if f.E.Message != "plain string error" {
		t.Fatalf("unexpected message: %q", f.E.Message)
	}
}

func TestReadFrameRequestWrongLength(t *testing.T) {
	// [0, 1] — too short for a request
	data, _ := msgpack.Marshal([]any{0, uint64(1)})
	_, err := ReadFrame(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for request with wrong element count")
	}
}

func TestReadFrameResponseWrongLength(t *testing.T) {
	// [1, 1] — too short for a response
	data, _ := msgpack.Marshal([]any{1, uint64(1)})
	_, err := ReadFrame(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for response with wrong element count")
	}
}

func TestReadFrameNotificationWrongLength(t *testing.T) {
	// [2, "m"] — too short for a notification
	data, _ := msgpack.Marshal([]any{2, "m"})
	_, err := ReadFrame(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for notification with wrong element count")
	}
}

func TestWriteFrameUnknownType(t *testing.T) {
	in := &Frame{T: 99}
	err := WriteFrame(io.Discard, in)
	if err == nil {
		t.Fatal("expected error for unknown frame type")
	}
}
