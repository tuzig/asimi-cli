// Package rpc implements the symmetric MessagePack RPC used between the
// court daemon and the TUI.
//
// A Conn wraps any io.ReadWriteCloser (a unix socket, a net.Pipe, a pair
// of os.Pipe, etc.) and supports three frame kinds:
//
//	Call          — outbound request, waits for a response
//	Notify        — outbound fire-and-forget notification
//	Handle(m, h)  — register an inbound request handler
//	HandleNotify  — register an inbound notification handler
//
// Both peers run the same Conn; either end may Call the other. This is
// how the daemon asks the TUI for approval using the same machinery the
// TUI uses to submit prompts.
package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/afittestide/asimi/internal/wire"
	"github.com/vmihailenco/msgpack/v5"
)

// Handler services an inbound request. It receives the raw params and
// returns the raw result (or an *wire.Error for a structured failure).
type Handler func(ctx context.Context, params []byte) ([]byte, error)

// NotifyHandler services an inbound notification.
type NotifyHandler func(ctx context.Context, params []byte)

// ErrPeerDisconnected is returned from Call when the Conn closes before
// a response arrives.
var ErrPeerDisconnected = errors.New("rpc: peer disconnected")

// ErrClosed is returned when Call is invoked on a closed Conn.
var ErrClosed = errors.New("rpc: conn closed")

// Conn is a bidirectional RPC connection.
type Conn struct {
	rw     io.ReadWriteCloser
	dec    *msgpack.Decoder // reused across ReadFrame calls to preserve buffer
	logger *slog.Logger

	// writes serialises all frame writes to rw.
	writes chan *wire.Frame

	// notifyQueue carries inbound notifications to a single dispatch
	// goroutine so handlers run serially in arrival order.
	notifyQueue chan *wire.Frame

	// pending holds in-flight outbound requests awaiting a response.
	pending sync.Map // uint64 → chan *wire.Frame

	mu             sync.RWMutex
	handlers       map[string]Handler
	notifyHandlers map[string]NotifyHandler

	nextID atomic.Uint64

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  atomic.Pointer[error]

	// ctx is cancelled on Close; handlers receive a child of it.
	ctx    context.Context
	cancel context.CancelFunc
}

// Options tune Conn behaviour. A zero Options is fine.
type Options struct {
	// WriteBuffer bounds the outbound frame channel. Default 256.
	WriteBuffer int
	Logger      *slog.Logger
}

// New wraps rw as a Conn. Register handlers via Handle / HandleNotify
// before calling Serve, which runs the reader and writer goroutines
// until the Conn is closed or rw returns an error.
func New(rw io.ReadWriteCloser, opts Options) *Conn {
	if opts.WriteBuffer <= 0 {
		opts.WriteBuffer = 256
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		rw:             rw,
		dec:            msgpack.NewDecoder(rw),
		logger:         opts.Logger,
		writes:         make(chan *wire.Frame, opts.WriteBuffer),
		notifyQueue:    make(chan *wire.Frame, opts.WriteBuffer),
		handlers:       make(map[string]Handler),
		notifyHandlers: make(map[string]NotifyHandler),
		closed:         make(chan struct{}),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Handle registers a request handler for method m. Safe to call after Serve.
func (c *Conn) Handle(m string, h Handler) {
	c.mu.Lock()
	c.handlers[m] = h
	c.mu.Unlock()
}

// HandleNotify registers a notification handler for method m. Safe to call after Serve.
func (c *Conn) HandleNotify(m string, h NotifyHandler) {
	c.mu.Lock()
	c.notifyHandlers[m] = h
	c.mu.Unlock()
}

// Serve runs the reader, writer, and notification-dispatch goroutines
// until the Conn closes. Blocks the caller; typically invoked in its
// own goroutine.
func (c *Conn) Serve() error {
	writeDone := make(chan struct{})
	notifyDone := make(chan struct{})

	go func() {
		defer close(writeDone)
		for {
			select {
			case <-c.closed:
				return
			case f, ok := <-c.writes:
				if !ok {
					return
				}
				if err := wire.WriteFrame(c.rw, f); err != nil {
					c.setCloseErr(err)
					c.Close()
					return
				}
			}
		}
	}()

	go func() {
		defer close(notifyDone)
		for {
			select {
			case <-c.closed:
				return
			case f, ok := <-c.notifyQueue:
				if !ok {
					return
				}
				c.mu.RLock()
				h, ok := c.notifyHandlers[f.M]
				c.mu.RUnlock()
				if !ok {
					c.logger.Debug("rpc: no handler for notification", "method", f.M)
					continue
				}
				// Handlers run in this goroutine so order is preserved.
				h(c.ctx, f.P)
			}
		}
	}()

	readErr := c.readLoop()
	if readErr != nil {
		c.logger.Debug("readLoop exited with error", "err", readErr)
	}
	c.setCloseErr(readErr)
	c.Close()
	<-writeDone
	<-notifyDone

	// Fail all pending callers.
	c.pending.Range(func(key, value any) bool {
		ch := value.(chan *wire.Frame)
		close(ch)
		return true
	})
	return readErr
}

func (c *Conn) readLoop() error {
	for {
		f, err := wire.ReadFrame(c.dec)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		c.dispatch(f)
	}
}

func (c *Conn) dispatch(f *wire.Frame) {
	switch f.T {
	case wire.FrameResponse:
		if v, ok := c.pending.LoadAndDelete(f.ID); ok {
			ch := v.(chan *wire.Frame)
			// Non-blocking; the caller created the chan with cap 1.
			select {
			case ch <- f:
			default:
			}
		} else {
			c.logger.Warn("rpc: orphan response", "id", f.ID)
		}
	case wire.FrameRequest:
		c.mu.RLock()
		h, ok := c.handlers[f.M]
		c.mu.RUnlock()
		if !ok {
			c.sendResponse(f.ID, nil, wire.NewError(wire.CodeUnknownMethod, "unknown method: "+f.M))
			return
		}
		go func() {
			result, err := h(c.ctx, f.P)
			var werr *wire.Error
			if err != nil {
				if e, ok := err.(*wire.Error); ok {
					werr = e
				} else {
					werr = wire.NewError(0, err.Error())
				}
			}
			c.sendResponse(f.ID, result, werr)
		}()
	case wire.FrameNotify:
		// Enqueue for serial dispatch. Drops (with a log) only if the
		// queue is full — i.e. handlers can't keep up with the stream.
		select {
		case c.notifyQueue <- f:
		case <-c.closed:
		default:
			c.logger.Warn("rpc: notification queue full, dropping", "method", f.M)
		}
	default:
		c.logger.Warn("rpc: unknown frame type", "t", f.T)
	}
}

// Call sends a request and waits for a response. Cancellable via ctx.
// Returns the raw result; use wire.Decode to unmarshal into a typed struct.
func (c *Conn) Call(ctx context.Context, method string, params any) ([]byte, error) {
	raw, err := wire.Encode(params)
	if err != nil {
		return nil, fmt.Errorf("rpc: encode params: %w", err)
	}
	id := c.nextID.Add(1)
	ch := make(chan *wire.Frame, 1)
	c.pending.Store(id, ch)
	defer c.pending.Delete(id)

	f := &wire.Frame{T: wire.FrameRequest, ID: id, M: method, P: raw}
	if err := c.enqueue(f); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, ErrPeerDisconnected
	case resp, ok := <-ch:
		if !ok {
			return nil, ErrPeerDisconnected
		}
		if resp.E != nil {
			return nil, resp.E
		}
		return resp.R, nil
	}
}

// Notify sends a fire-and-forget notification. Returns immediately after
// the frame is queued for writing; returns ErrClosed if the Conn is down.
func (c *Conn) Notify(method string, params any) error {
	raw, err := wire.Encode(params)
	if err != nil {
		return fmt.Errorf("rpc: encode params: %w", err)
	}
	return c.enqueue(&wire.Frame{T: wire.FrameNotify, M: method, P: raw})
}

func (c *Conn) enqueue(f *wire.Frame) error {
	select {
	case <-c.closed:
		return ErrClosed
	case c.writes <- f:
		return nil
	}
}

func (c *Conn) sendResponse(id uint64, result []byte, werr *wire.Error) {
	_ = c.enqueue(&wire.Frame{T: wire.FrameResponse, ID: id, R: result, E: werr})
}

// Close shuts the Conn down. Idempotent. Safe from multiple goroutines.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.cancel()
		_ = c.rw.Close()
	})
	return nil
}

// Done returns a channel that is closed when the Conn has shut down.
func (c *Conn) Done() <-chan struct{} { return c.closed }

// Err returns the error that caused the Conn to close, if any.
func (c *Conn) Err() error {
	if p := c.closeErr.Load(); p != nil {
		return *p
	}
	return nil
}

func (c *Conn) setCloseErr(err error) {
	if err == nil {
		return
	}
	// Use a local copy so every Store provides a consistent pointer type.
	e := err
	c.closeErr.CompareAndSwap(nil, &e)
}
