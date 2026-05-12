package rpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/shogunateapi"
	"github.com/afittestide/asimi/shogunate"
)

// cancellingFake stands in for the server-side Shogunate just enough to
// exercise the SubmitPrompt → CancelTab cancellation path. SubmitPrompt
// captures the prompt's Ctx; CancellableStreamCtx mints a per-channel
// cancellable ctx; CancelTab fires the registered cancel.
//
// Everything else panics via the nil embedded interface — keeps the
// fake honest about what's actually exercised.
type cancellingFake struct {
	shogunateapi.Client

	mu             sync.Mutex
	capturedCtx    chan context.Context
	channelCancels map[string]context.CancelFunc
}

func newCancellingFake() *cancellingFake {
	return &cancellingFake{
		capturedCtx:    make(chan context.Context, 1),
		channelCancels: map[string]context.CancelFunc{},
	}
}

func (f *cancellingFake) SubmitPrompt(_ string, p *shogunate.Prompt) error {
	select {
	case f.capturedCtx <- p.Ctx:
	default:
	}
	return nil
}

func (f *cancellingFake) CancellableStreamCtx(channelID string) context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	if prev, ok := f.channelCancels[channelID]; ok {
		prev()
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.channelCancels[channelID] = cancel
	return ctx
}

func (f *cancellingFake) CancelTab(channelID string) {
	f.mu.Lock()
	cancel, ok := f.channelCancels[channelID]
	if ok {
		delete(f.channelCancels, channelID)
	}
	f.mu.Unlock()
	if ok {
		cancel()
	}
}

// TestRPCSubmitPromptCtxCancelledByCancelTab is the daemon-mode
// CTRL-C contract: the TUI sends SubmitPrompt over the wire, then a
// CancelTab for the same channelID must cancel the prompt's Ctx as
// seen by the server-side impl.
//
// Today the server hands the prompt the RPC handler's connection ctx,
// which is unrelated to any per-channel cancel registry — so this
// test fails until the server is fixed to mint the prompt's Ctx via
// impl.CancellableStreamCtx(channelID).
func TestRPCSubmitPromptCtxCancelledByCancelTab(t *testing.T) {
	impl := newCancellingFake()

	pa, pb := net.Pipe()
	server := New(pa, Options{})
	clientConn := New(pb, Options{})

	RegisterShogunateHandlers(server, impl)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = server.Serve() }()
	go func() { defer wg.Done(); _ = clientConn.Serve() }()
	defer func() {
		_ = clientConn.Close()
		_ = server.Close()
		wg.Wait()
	}()

	client := NewShogunateClient(clientConn)

	if err := client.SubmitPrompt("chancellor", &shogunate.Prompt{
		Message:   "hello",
		ChannelID: "ruling",
	}); err != nil {
		t.Fatalf("SubmitPrompt: %v", err)
	}

	var promptCtx context.Context
	select {
	case promptCtx = <-impl.capturedCtx:
	case <-time.After(2 * time.Second):
		t.Fatal("SubmitPrompt never reached impl")
	}

	select {
	case <-promptCtx.Done():
		t.Fatal("prompt ctx cancelled before CancelTab — server handed a ctx unrelated to the channel")
	case <-time.After(50 * time.Millisecond):
	}

	client.CancelTab("ruling")

	select {
	case <-promptCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("prompt ctx not cancelled after CancelTab — daemon-mode CTRL-C is broken")
	}
}
