package main

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/shogunate"
)

// messageSender abstracts the send side of Bubble Tea's program.
// In production this wraps *tea.Program; in tests a mock implements it.
type messageSender interface {
	Send(msg any)
}

// programSender wraps a *tea.Program to implement messageSender.
type programSender struct {
	program *tea.Program
}

func (s programSender) Send(msg any) {
	s.program.Send(msg)
}

// notificationDispatcher is an async prioritized dispatcher that replaces the
// synchronous events → Send goroutine in main.go. It has two priority lanes:
//
//   - High: control/lifecycle messages — always delivered with a blocking send.
//   - Chunk: StreamChunkMsg — non-blocking
//     try-send; dropped if the TUI is busy.
//
// A single internal goroutine reads from the buffered `in` channel and routes
// each message based on a type-switch. Dropped counters are tracked for
// telemetry and exposed via DroppedCounts().
type notificationDispatcher struct {
	sender   messageSender
	sendFunc func(msg any) // if non-nil, overrides sender (used in tests)
	in       chan any

	cancel context.CancelFunc
	done   chan struct{} // closed when the dispatch goroutine exits

	streamChunkSem chan struct{}

	mediumDropped atomic.Int64
}

// newNotificationDispatcher creates a dispatcher and starts its internal
// goroutine. Call close() to stop it.
func newNotificationDispatcher(program *tea.Program) *notificationDispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	d := &notificationDispatcher{
		sender:         programSender{program: program},
		in:             make(chan any, 512),
		cancel:         cancel,
		done:           make(chan struct{}),
		streamChunkSem: make(chan struct{}, 1),
	}
	go d.run(ctx)
	return d
}

// notify enqueues a message for dispatch. It never blocks the caller because
// the `in` channel is buffered (cap 512). If the buffer is full the message is
// silently dropped — the producer (Subscribe loop) must not stall.
func (d *notificationDispatcher) notify(msg any) {
	select {
	case d.in <- msg:
	default:
		slog.Warn("notificationDispatcher: input buffer full, dropping message",
			"msg_type", typeName(msg))
	}
}

// DroppedCounts returns the cumulative count of dropped chunk messages.
// The `low` return value is always 0 — it is kept for API compatibility.
func (d *notificationDispatcher) DroppedCounts() (medium int64, low int64) {
	return d.mediumDropped.Load(), 0
}

// close signals the dispatch goroutine to stop and waits for it to finish.
func (d *notificationDispatcher) close() {
	d.cancel()
	<-d.done
}

// run is the main dispatch loop.
func (d *notificationDispatcher) run(ctx context.Context) {
	defer close(d.done)

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-d.in:
			d.dispatch(msg)
		}
	}
}

// dispatch routes a single message based on its type.
func (d *notificationDispatcher) dispatch(msg any) {
	switch msg.(type) {
	// ── High priority: blocking send (guaranteed delivery) ────────────
	case shogunate.StreamStartMsg,
		shogunate.StreamCompleteMsg,
		shogunate.StreamInterruptedMsg,
		shogunate.StreamErrorMsg,
		shogunate.StreamMaxTokensReachedMsg,
		shogunate.StreamDoneMsg,
		shogunate.MinisterInvokingMsg,
		shogunate.MinisterCompletedMsg,
		shogunate.EventNotificationMsg,
		shogunate.ZhengmingPendingMsg,
		shogunate.ZhengmingAnsweredMsg,
		shogunate.RitualStepMsg,
		runners.ToolCallScheduledMsg,
		runners.ToolCallExecutingMsg,
		runners.ToolCallSuccessMsg,
		runners.ToolCallErrorMsg,
		runners.ToolCallAbortedMsg,
		runners.ToolCallWaitingForApprovalMsg,
		runners.ContainerLaunchedMsg,
		shogunate.EventsDrainedMsg,
		errMsg,
		llmInitSuccessMsg,
		llmInitErrorMsg,
		compactCompleteMsg,
		compactErrorMsg,
		updateAvailableMsg:
		d.send(msg)

	case shogunate.StreamChunkMsg:
		d.trySend(msg, d.streamChunkSem, &d.mediumDropped)

	default:
		// Unknown type — send blocking to avoid silent loss of new message types.
		d.send(msg)
	}
}

// send dispatches to sendFunc if set (tests), otherwise to the real sender.
func (d *notificationDispatcher) send(msg any) {
	if d.sendFunc != nil {
		d.sendFunc(msg)
	} else {
		d.sender.Send(msg)
	}
}

// trySend performs a non-blocking send. It acquires the 1-slot semaphore; if
// it can't (a previous send of this priority is still pending), the message is
// dropped and the counter incremented. The semaphore is released by the
// sending goroutine after the send completes.
func (d *notificationDispatcher) trySend(msg any, sem chan struct{}, counter *atomic.Int64) {
	select {
	case sem <- struct{}{}:
		go func() {
			d.send(msg)
			<-sem // release semaphore
		}()
	default:
		counter.Add(1)
	}
}

// typeName returns a short human-readable name for a message's type,
// e.g. "shogunate.StreamStartMsg" instead of the full package path.
func typeName(msg any) string {
	if msg == nil {
		return "<nil>"
	}
	t := reflect.TypeOf(msg)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	pkg := t.PkgPath()
	name := t.Name()
	// Trim to just the last segment of the package path.
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}

// Compile-time sanity check.
var _ = fmt.Sprintf
