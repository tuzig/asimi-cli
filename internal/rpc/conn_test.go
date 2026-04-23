package rpc

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/wire"
)

// newPair returns two Conns wired back-to-back over net.Pipe. Both Serve
// loops are running; the returned cleanup closes them.
func newPair(t *testing.T) (a, b *Conn, cleanup func()) {
	t.Helper()
	pa, pb := net.Pipe()
	a = New(pa, Options{})
	b = New(pb, Options{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = a.Serve() }()
	go func() { defer wg.Done(); _ = b.Serve() }()
	return a, b, func() {
		_ = a.Close()
		_ = b.Close()
		wg.Wait()
	}
}

type addReq struct {
	A int `msgpack:"a"`
	B int `msgpack:"b"`
}

type addResp struct {
	Sum int `msgpack:"sum"`
}

func TestCallRoundTrip(t *testing.T) {
	a, b, cleanup := newPair(t)
	defer cleanup()

	b.Handle("Add", func(ctx context.Context, params []byte) ([]byte, error) {
		var in addReq
		if err := wire.Decode(params, &in); err != nil {
			return nil, err
		}
		out, err := wire.Encode(addResp{Sum: in.A + in.B})
		return out, err
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := a.Call(ctx, "Add", addReq{A: 2, B: 3})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var got addResp
	if err := wire.Decode(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Sum != 5 {
		t.Fatalf("sum = %d", got.Sum)
	}
}

func TestBidirectionalCall(t *testing.T) {
	// Both sides register handlers; both sides Call.
	a, b, cleanup := newPair(t)
	defer cleanup()

	a.Handle("Ping", func(ctx context.Context, _ []byte) ([]byte, error) {
		return wire.Encode("pong-from-a")
	})
	b.Handle("Ping", func(ctx context.Context, _ []byte) ([]byte, error) {
		return wire.Encode("pong-from-b")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	raw, err := a.Call(ctx, "Ping", nil)
	if err != nil {
		t.Fatalf("a→b Call: %v", err)
	}
	var s string
	_ = wire.Decode(raw, &s)
	if s != "pong-from-b" {
		t.Fatalf("a→b got %q", s)
	}

	raw, err = b.Call(ctx, "Ping", nil)
	if err != nil {
		t.Fatalf("b→a Call: %v", err)
	}
	_ = wire.Decode(raw, &s)
	if s != "pong-from-a" {
		t.Fatalf("b→a got %q", s)
	}
}

func TestCallUnknownMethod(t *testing.T) {
	a, _, cleanup := newPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := a.Call(ctx, "Nope", nil)
	we := &wire.Error{}
	if !errors.As(err, &we) {
		t.Fatalf("expected *wire.Error, got %T: %v", err, err)
	}
	if we.Code != wire.CodeUnknownMethod {
		t.Fatalf("code = %d, want CodeUnknownMethod", we.Code)
	}
}

func TestHandlerReturnsError(t *testing.T) {
	a, b, cleanup := newPair(t)
	defer cleanup()

	b.Handle("Fail", func(ctx context.Context, _ []byte) ([]byte, error) {
		return nil, wire.NewError(1042, "boom")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := a.Call(ctx, "Fail", nil)
	we := &wire.Error{}
	if !errors.As(err, &we) || we.Code != 1042 || we.Message != "boom" {
		t.Fatalf("unexpected err: %+v", err)
	}
}

func TestHandlerPlainErrorBecomesCodeZero(t *testing.T) {
	a, b, cleanup := newPair(t)
	defer cleanup()

	b.Handle("Fail", func(ctx context.Context, _ []byte) ([]byte, error) {
		return nil, errors.New("io uring melted")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := a.Call(ctx, "Fail", nil)
	we := &wire.Error{}
	if !errors.As(err, &we) || we.Code != 0 || we.Message != "io uring melted" {
		t.Fatalf("unexpected err: %+v", err)
	}
}

func TestCallContextCancel(t *testing.T) {
	a, b, cleanup := newPair(t)
	defer cleanup()

	block := make(chan struct{})
	b.Handle("Block", func(ctx context.Context, _ []byte) ([]byte, error) {
		<-block
		return nil, nil
	})
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := a.Call(ctx, "Block", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestCallFailsAfterPeerDisconnect(t *testing.T) {
	a, b, cleanup := newPair(t)
	defer cleanup()

	block := make(chan struct{})
	b.Handle("Block", func(ctx context.Context, _ []byte) ([]byte, error) {
		<-block
		return nil, nil
	})
	defer close(block)

	errc := make(chan error, 1)
	go func() {
		_, err := a.Call(context.Background(), "Block", nil)
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	_ = b.Close()

	select {
	case err := <-errc:
		if !errors.Is(err, ErrPeerDisconnected) && !errors.Is(err, io.EOF) {
			t.Fatalf("want ErrPeerDisconnected, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after peer disconnect")
	}
}

func TestNotifyDelivered(t *testing.T) {
	a, b, cleanup := newPair(t)
	defer cleanup()

	got := make(chan string, 1)
	b.HandleNotify("greet", func(ctx context.Context, params []byte) {
		var s string
		_ = wire.Decode(params, &s)
		got <- s
	})

	if err := a.Notify("greet", "hi"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	select {
	case s := <-got:
		if s != "hi" {
			t.Fatalf("got %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification not delivered")
	}
}

func TestNotifyUnknownMethodIsIgnored(t *testing.T) {
	a, _, cleanup := newPair(t)
	defer cleanup()
	// Must not panic, must not break the connection for subsequent traffic.
	if err := a.Notify("no.such", 42); err != nil {
		t.Fatalf("Notify: %v", err)
	}
}

func TestConcurrentCallsMultiplexCorrectly(t *testing.T) {
	a, b, cleanup := newPair(t)
	defer cleanup()

	b.Handle("Echo", func(ctx context.Context, params []byte) ([]byte, error) {
		var n int
		_ = wire.Decode(params, &n)
		// simulate work so responses arrive out of order
		time.Sleep(time.Duration(50-n%10) * time.Millisecond)
		return wire.Encode(n * 2)
	})

	const N = 20
	var wg sync.WaitGroup
	var ok atomic.Int32
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			raw, err := a.Call(ctx, "Echo", i)
			if err != nil {
				t.Errorf("Call %d: %v", i, err)
				return
			}
			var got int
			_ = wire.Decode(raw, &got)
			if got == i*2 {
				ok.Add(1)
			} else {
				t.Errorf("Call %d got %d", i, got)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != N {
		t.Fatalf("only %d/%d calls matched", ok.Load(), N)
	}
}
