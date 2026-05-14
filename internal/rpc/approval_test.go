package rpc

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/runners"
)

// fakeProgram captures any tea.Msg equivalents the approval handler
// sends. In the real TUI this would be *tea.Program.Send.
type fakeProgram struct {
	ch chan any
}

func (f *fakeProgram) Send(msg any) { f.ch <- msg }

func TestApprovalRequestRoundTrip(t *testing.T) {
	pa, pb := net.Pipe()
	daemon := New(pa, Options{})
	tui := New(pb, Options{})

	fake := &fakeProgram{ch: make(chan any, 1)}
	RegisterApprovalHandler(tui, fake)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = daemon.Serve() }()
	go func() { defer wg.Done(); _ = tui.Serve() }()
	defer func() {
		_ = daemon.Close()
		_ = tui.Close()
		wg.Wait()
	}()

	// Goroutine on the "user" side of the fake program: receives the
	// tea.Msg-equivalent, writes the answer back through ResponseChan.
	go func() {
		msg := <-fake.ch
		req, ok := msg.(runners.ApprovalRequestMsg)
		if !ok {
			t.Errorf("unexpected msg type: %T", msg)
			return
		}
		req.ResponseChan <- true
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	approved, err := RequestApproval(ctx, daemon, "rm -rf /")
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if !approved {
		t.Fatalf("approved = false")
	}
}

func TestApprovalRequestDeniedRoundTrip(t *testing.T) {
	pa, pb := net.Pipe()
	daemon := New(pa, Options{})
	tui := New(pb, Options{})

	fake := &fakeProgram{ch: make(chan any, 1)}
	RegisterApprovalHandler(tui, fake)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = daemon.Serve() }()
	go func() { defer wg.Done(); _ = tui.Serve() }()
	defer func() {
		_ = daemon.Close()
		_ = tui.Close()
		wg.Wait()
	}()

	go func() {
		msg := <-fake.ch
		req := msg.(runners.ApprovalRequestMsg)
		req.ResponseChan <- false
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	approved, err := RequestApproval(ctx, daemon, "dangerous cmd")
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if approved {
		t.Fatalf("approved = true; wanted false")
	}
}

func TestApprovalRequestContextTimeout(t *testing.T) {
	pa, pb := net.Pipe()
	daemon := New(pa, Options{})
	tui := New(pb, Options{})

	// TUI registers a handler but the fake "user" never replies.
	fake := &fakeProgram{ch: make(chan any, 1)}
	RegisterApprovalHandler(tui, fake)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = daemon.Serve() }()
	go func() { defer wg.Done(); _ = tui.Serve() }()
	defer func() {
		_ = daemon.Close()
		_ = tui.Close()
		wg.Wait()
	}()

	// Drain the handler-side tea.Msg so the handler is blocked on
	// ResponseChan; then let ctx time out.
	go func() { <-fake.ch }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := RequestApproval(ctx, daemon, "slow")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}

func TestPumpShogunateEventsInterceptsApproval(t *testing.T) {
	pa, pb := net.Pipe()
	daemon := New(pa, Options{})
	tui := New(pb, Options{})

	fake := &fakeProgram{ch: make(chan any, 1)}
	RegisterApprovalHandler(tui, fake)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = daemon.Serve() }()
	go func() { defer wg.Done(); _ = tui.Serve() }()
	defer func() {
		_ = daemon.Close()
		_ = tui.Close()
		wg.Wait()
	}()

	events := make(chan any, 4)
	pumpCtx, cancelPump := context.WithCancel(context.Background())
	go PumpShogunateEvents(pumpCtx, daemon, events)
	defer cancelPump()

	// Have the fake user answer "yes".
	go func() {
		msg := <-fake.ch
		req := msg.(runners.ApprovalRequestMsg)
		req.ResponseChan <- true
	}()

	respCh := make(chan bool, 1)
	events <- runners.ApprovalRequestMsg{Command: "ls", ResponseChan: respCh}

	select {
	case got := <-respCh:
		if !got {
			t.Fatalf("want true, got false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("response never delivered")
	}
}
