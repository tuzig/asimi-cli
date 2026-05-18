package main

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/shogunate"
)

// ── Test dispatcher factory ──────────────────────────────────────────────────

// newTestDispatcher creates a dispatcher whose sends go through sendFunc
// instead of a real *tea.Program. The caller must call shutdown().
func newTestDispatcher(sendFunc func(msg any)) *notificationDispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	d := &notificationDispatcher{
		sendFunc:       sendFunc,
		in:             make(chan any, 512),
		cancel:         cancel,
		done:           make(chan struct{}),
		streamChunkSem: make(chan struct{}, 1),
	}
	go d.run(ctx)
	return d
}

// newDispatcherWithSender creates a dispatcher that routes to the given sender
// instead of a real *tea.Program. The caller must call shutdown().
// Kept for backward compatibility with existing integration tests.
func newDispatcherWithSender(sender messageSender) *notificationDispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	d := &notificationDispatcher{
		sender:         sender,
		in:             make(chan any, 512),
		cancel:         cancel,
		done:           make(chan struct{}),
		streamChunkSem: make(chan struct{}, 1),
	}
	go d.run(ctx)
	return d
}

// ── Mock sendFunc helpers ───────────────────────────────────────────────────

// recordSendFunc returns a sendFunc that appends every message to msgs.
// It is safe for concurrent use.
func recordSendFunc(msgs *[]any) func(any) {
	var mu sync.Mutex
	return func(msg any) {
		mu.Lock()
		*msgs = append(*msgs, msg)
		mu.Unlock()
	}
}

// blockingSendFunc returns a sendFunc that blocks until unblock is closed,
// then records the message. The returned counter tracks how many messages
// have completed their send (after unblock).
func blockingSendFunc(unblock <-chan struct{}, counter *atomic.Int64) func(any) {
	return func(msg any) {
		<-unblock // block until caller signals
		counter.Add(1)
	}
}

// blockingRecordSendFunc blocks like blockingSendFunc but also records.
func blockingRecordSendFunc(unblock <-chan struct{}, msgs *[]any, mu *sync.Mutex, counter *atomic.Int64) func(any) {
	return func(msg any) {
		<-unblock
		mu.Lock()
		*msgs = append(*msgs, msg)
		mu.Unlock()
		counter.Add(1)
	}
}

// ── Test 1: High-priority message is never dropped ──────────────────────────

// TestHighPriorityNeverDropped verifies that high-priority messages are always
// delivered via blocking send, even when the sendFunc is temporarily blocked.
// All high-priority messages must eventually arrive—none are dropped.
func TestHighPriorityNeverDropped(t *testing.T) {
	var (
		mu      sync.Mutex
		msgs    []any
		counter atomic.Int64
	)
	unblock := make(chan struct{})

	sendFunc := blockingRecordSendFunc(unblock, &msgs, &mu, &counter)

	d := newTestDispatcher(sendFunc)
	defer d.close()

	const count = 20
	// Enqueue high-priority messages while the sendFunc is blocked.
	// Because dispatch calls sendFunc directly for high-priority types,
	// the first message blocks the dispatch loop; subsequent messages
	// accumulate in the `in` buffer.
	for i := 0; i < count; i++ {
		d.notify(shogunate.StreamStartMsg{})
	}

	// Give the dispatcher time to pick up the first message and block.
	time.Sleep(50 * time.Millisecond)

	// Unblock — all messages should now drain through.
	close(unblock)
	waitForCount(t, &counter, count, 3*time.Second)

	mu.Lock()
	delivered := len(msgs)
	mu.Unlock()
	if delivered != count {
		t.Errorf("expected exactly %d high-priority messages, got %d", count, delivered)
	}

	med, low := d.DroppedCounts()
	if med != 0 || low != 0 {
		t.Errorf("expected zero drops for high-priority, got medium=%d low=%d", med, low)
	}
}

// ── Test 2: StreamChunkMsg is dropped when congested ────────────────────────

// TestMediumPriorityDroppedWhenCongested verifies that StreamChunkMsg
// uses a non-blocking try-send. When the sendFunc is busy (semaphore
// occupied), additional chunk messages are dropped.
func TestMediumPriorityDroppedWhenCongested(t *testing.T) {
	var counter atomic.Int64
	unblock := make(chan struct{})

	sendFunc := blockingSendFunc(unblock, &counter)

	d := newTestDispatcher(sendFunc)
	defer d.close()

	// First message acquires the semaphore and blocks in the sendFunc.
	d.notify(shogunate.StreamChunkMsg{})
	time.Sleep(100 * time.Millisecond) // let dispatch loop process it

	// Semaphore is now occupied. These chunk messages should be dropped.
	const extra = 30
	for i := 0; i < extra; i++ {
		d.notify(shogunate.StreamChunkMsg{})
	}
	time.Sleep(200 * time.Millisecond)

	// Unblock and allow the first message to finish.
	close(unblock)
	time.Sleep(100 * time.Millisecond)

	med, low := d.DroppedCounts()
	if med == 0 {
		t.Error("expected medium-priority drops, got 0")
	}
	if low != 0 {
		t.Errorf("expected zero low drops, got %d", low)
	}
}

// ── Test 3: Unknown message types are treated as high-priority by default ──

// TestUnknownTypeHighPriority verifies that messages not matching any known
// type case fall through to the default branch, which does a blocking send.
func TestUnknownTypeHighPriority(t *testing.T) {
	type novelMsg struct{ id int }
	type anotherMsg struct{}

	var (
		mu   sync.Mutex
		msgs []any
	)
	sendFunc := recordSendFunc(&msgs)

	d := newTestDispatcher(sendFunc)
	defer d.close()

	const count = 15
	for i := 0; i < count; i++ {
		d.notify(novelMsg{id: i})
	}
	d.notify(anotherMsg{})

	// Blocking send completes immediately with uncongested sendFunc,
	// so all messages should arrive promptly.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	delivered := len(msgs)
	mu.Unlock()
	if delivered != count+1 {
		t.Errorf("expected %d unknown-type messages delivered, got %d", count+1, delivered)
	}

	med, low := d.DroppedCounts()
	if med != 0 || low != 0 {
		t.Errorf("expected zero drops for unknown types, got medium=%d low=%d", med, low)
	}
}

func TestDropCountersIncrement(t *testing.T) {
	const flood = 40

	// Single chunk lane (StreamChunkMsg only — reasoning was merged into same type).
	{
		var counter atomic.Int64
		unblock := make(chan struct{})
		d := newTestDispatcher(blockingSendFunc(unblock, &counter))
		defer d.close()

		d.notify(shogunate.StreamChunkMsg{})
		time.Sleep(50 * time.Millisecond)
		for i := 0; i < flood; i++ {
			d.notify(shogunate.StreamChunkMsg{})
		}
		time.Sleep(200 * time.Millisecond)
		close(unblock)
		time.Sleep(50 * time.Millisecond)

		med, low := d.DroppedCounts()
		if med != flood {
			t.Errorf("chunk flood: expected medium drops = %d, got %d", flood, med)
		}
		if low != 0 {
			t.Errorf("chunk flood: expected low drops = 0, got %d", low)
		}
	}
}

// ── Test 5: Context cancellation shuts down the dispatcher cleanly ──────────

// TestContextCancellationShutdown verifies that cancelling the dispatcher's
// context causes the dispatch goroutine to exit cleanly, and that subsequent
// shutdown() returns promptly without deadlock.
func TestContextCancellationShutdown(t *testing.T) {
	var counter atomic.Int64
	sendFunc := func(msg any) {
		counter.Add(1)
	}

	d := newTestDispatcher(sendFunc)

	// Enqueue some messages before cancellation.
	for i := 0; i < 10; i++ {
		d.notify(shogunate.StreamStartMsg{})
	}

	// Cancel the context directly (simulates external cancellation).
	d.cancel()

	// shutdown() should still complete — it waits on <-d.done.
	done := make(chan struct{})
	go func() {
		d.close()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown — test passes.
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown timed out after context cancellation — possible deadlock")
	}
}

// ── Test 6: Dispatcher drains from `in` channel and sends to mock program ───

// TestDispatcherDrainsInChannel verifies that the dispatcher actually reads
// messages from its `in` channel and delivers them via sendFunc. We use
// high-priority messages exclusively so the semaphore-based trySend path
// doesn't introduce non-deterministic drops.
func TestDispatcherDrainsInChannel(t *testing.T) {
	var (
		mu      sync.Mutex
		msgs    []any
		counter atomic.Int64
	)

	// sendFunc records and counts
	sendFunc := func(msg any) {
		counter.Add(1)
		mu.Lock()
		msgs = append(msgs, msg)
		mu.Unlock()
	}

	d := newTestDispatcher(sendFunc)
	defer d.close()

	const count = 20
	for i := 0; i < count; i++ {
		d.notify(shogunate.StreamCompleteMsg{})
	}

	waitForCount(t, &counter, count, 3*time.Second)

	mu.Lock()
	delivered := len(msgs)
	mu.Unlock()
	if delivered != count {
		t.Errorf("expected %d messages drained and sent, got %d", count, delivered)
	}

	// Verify `in` channel is empty — everything was drained.
	select {
	case m := <-d.in:
		t.Errorf("in channel should be empty, but got: %v", m)
	default:
		// Channel is empty as expected.
	}
}

// ── TypeName coverage ──────────────────────────────────────────────────────

// TestTypeName covers the typeName helper.
func TestTypeName(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{shogunate.StreamStartMsg{}, "shogunate.StreamStartMsg"},
		{errMsg{}, "asimi.errMsg"},
		{nil, "<nil>"},
	}
	for _, tt := range tests {
		got := typeName(tt.input)
		if got != tt.want {
			t.Errorf("typeName(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── All high-priority types coverage ────────────────────────────────────────

// TestAllHighPriorityTypes verifies that every declared high-priority type
// is routed through the blocking send path and delivered.
func TestAllHighPriorityTypes(t *testing.T) {
	highPriorityMsgs := []any{
		shogunate.StreamStartMsg{},
		shogunate.StreamCompleteMsg{},
		shogunate.StreamInterruptedMsg{},
		shogunate.StreamErrorMsg{},
		shogunate.StreamMaxTokensReachedMsg{},
		shogunate.StreamDoneMsg{},
		shogunate.MinisterInvokingMsg{},
		shogunate.MinisterCompletedMsg{},
		shogunate.EventNotificationMsg{},
		shogunate.ZhengmingPendingMsg{},
		shogunate.ZhengmingAnsweredMsg{},
		shogunate.RitualStepMsg{},
		runners.ToolCallScheduledMsg{},
		runners.ToolCallExecutingMsg{},
		runners.ToolCallSuccessMsg{},
		runners.ToolCallErrorMsg{},
		runners.ToolCallAbortedMsg{},
		runners.ToolCallWaitingForApprovalMsg{},
		runners.ContainerLaunchedMsg{},
		shogunate.EventsDrainedMsg{},
		errMsg{},
		llmInitSuccessMsg{},
		llmInitErrorMsg{},
		compactCompleteMsg{},
		compactErrorMsg{},
		updateAvailableMsg{},
	}

	count := len(highPriorityMsgs)

	var (
		mu      sync.Mutex
		msgs    []any
		counter atomic.Int64
	)
	sendFunc := func(msg any) {
		counter.Add(1)
		mu.Lock()
		msgs = append(msgs, msg)
		mu.Unlock()
	}

	d := newTestDispatcher(sendFunc)
	defer d.close()

	for _, msg := range highPriorityMsgs {
		d.notify(msg)
	}

	waitForCount(t, &counter, int64(count), 2*time.Second)

	mu.Lock()
	delivered := len(msgs)
	mu.Unlock()
	if delivered < count {
		t.Errorf("expected %d high-priority messages delivered, got %d", count, delivered)
	}
}

// ── Chunk ordering: all chunks share a single lane ──────────────────────────

// TestChunkOrderingOutputAndReasoning verifies that StreamChunkMsg chunks
// (both content and reasoning, now the same type) share a single 1-slot
// semaphore. The dispatch loop's submission order must be preserved by the
// sender. Interleaved chunk messages must arrive in prefix-ordered subset
// of what was submitted.
func TestChunkOrderingOutputAndReasoning(t *testing.T) {
	var (
		mu       sync.Mutex
		received []shogunate.StreamChunkMsg
	)
	sendFunc := func(msg any) {
		mu.Lock()
		defer mu.Unlock()
		if v, ok := msg.(shogunate.StreamChunkMsg); ok {
			received = append(received, v)
		}
	}

	d := newTestDispatcher(sendFunc)
	defer d.close()

	// Submit an interleaved sequence of content and reasoning chunks,
	// both as StreamChunkMsg. The seq number embedded in Text must
	// arrive in monotonically increasing order at the sender (allowing for
	// drops — a drop just skips a number, never inverts the order).
	const total = 200
	for i := 0; i < total; i++ {
		text := fmt.Sprintf("%04d", i)
		// Alternate ChannelID to simulate interleaved content/reasoning,
		// but both are the same message type.
		chID := "o" // output
		if i%2 != 0 {
			chID = "r" // reasoning
		}
		d.notify(shogunate.StreamChunkMsg{ChannelID: chID, Text: text})
	}

	// Wait for the dispatcher to drain.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	got := append([]shogunate.StreamChunkMsg{}, received...)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("no chunks delivered")
	}

	// Each received Text must be lexicographically greater than the previous —
	// i.e. the dispatcher preserved submission order.
	for i := 1; i < len(got); i++ {
		if got[i].Text <= got[i-1].Text {
			t.Fatalf("ordering violation at %d: prev=%q curr=%q (full sequence: %v)",
				i, got[i-1].Text, got[i].Text, textsOnly(got))
		}
	}
}

func textsOnly(msgs []shogunate.StreamChunkMsg) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Text
	}
	return out
}

// ── helpers ────────────────────────────────────────────────────────────────

// waitForCount polls the atomic counter until it reaches want or times out.
func waitForCount(t *testing.T, counter *atomic.Int64, want int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if counter.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for count %d, got %d", want, counter.Load())
}

// Compile-time reference checks for typeName.
var _ = fmt.Sprintf
var _ = reflect.TypeOf

// ────────────────────────────────────────────────────────────────────────────
// messageSender-layer integration tests
//
// The tests above exercise the dispatcher through its sendFunc seam. The tests
// below go one layer up and drive it through the messageSender interface used
// in production. They verify the same priority/drop guarantees end-to-end.
// ────────────────────────────────────────────────────────────────────────────

// countingSender implements messageSender and counts how many times Send is
// called. If block is non-nil, every Send blocks until block is closed.
type countingSender struct {
	mu        sync.Mutex
	msgs      []any
	block     chan struct{} // if non-nil, Send blocks until closed
	sendCount atomic.Int64
}

func (s *countingSender) Send(msg any) {
	s.sendCount.Add(1)
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	s.msgs = append(s.msgs, msg)
	s.mu.Unlock()
}

func (s *countingSender) messages() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]any, len(s.msgs))
	copy(cp, s.msgs)
	return cp
}

// TestBackpressure_DispatcherDoesNotBlockProducer verifies that calling
// dispatcher.notify() never blocks, even when the TUI sender is congested.
// The producer (Subscribe loop) must not stall — this is the core backpressure
// relief guarantee.
func TestBackpressure_DispatcherDoesNotBlockProducer(t *testing.T) {
	block := make(chan struct{})
	sender := &countingSender{block: block}
	d := newDispatcherWithSender(sender)
	defer d.close()

	// Block the sender so all dispatched messages will be stuck.
	// Enqueue a flood of messages from the producer side.
	const flood = 200
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < flood; i++ {
			d.notify(shogunate.StreamChunkMsg{ChannelID: "forge", Text: "chunk"})
		}
	}()

	// The producer goroutine should complete quickly — it never blocks
	select {
	case <-done:
		// Producer completed without blocking — backpressure relief works
	case <-time.After(2 * time.Second):
		t.Fatal("producer blocked — backpressure relief not working")
	}

	// Unblock the sender and let everything drain
	close(block)
	waitForCount(t, &sender.sendCount, 1, 3*time.Second)
}

// TestBackpressure_ReasoningChunksDroppedWhenCongested verifies that
// StreamChunkMsg messages (including reasoning) are dropped when the TUI
// sender is congested. Since reasoning and content share the same type and
// semaphore lane, both use the medium drop counter.
func TestBackpressure_ReasoningChunksDroppedWhenCongested(t *testing.T) {
	block := make(chan struct{})
	sender := &countingSender{block: block}
	d := newDispatcherWithSender(sender)
	defer d.close()

	// First chunk acquires semaphore and blocks
	d.notify(shogunate.StreamChunkMsg{ChannelID: "sage", Text: "thinking"})
	time.Sleep(50 * time.Millisecond)

	// Flood with more chunks — semaphore is occupied, these should drop
	const extra = 50
	for i := 0; i < extra; i++ {
		d.notify(shogunate.StreamChunkMsg{ChannelID: "sage", Text: "more thinking"})
	}
	time.Sleep(200 * time.Millisecond)

	close(block)
	time.Sleep(100 * time.Millisecond)

	med, low := d.DroppedCounts()
	assert.Greater(t, med, int64(0), "chunk messages should be dropped when congested")
	assert.Equal(t, int64(0), low, "low drops always zero")

	// At least the first message should have been delivered
	msgs := sender.messages()
	chunkDelivered := 0
	for _, m := range msgs {
		if _, ok := m.(shogunate.StreamChunkMsg); ok {
			chunkDelivered++
		}
	}
	assert.GreaterOrEqual(t, chunkDelivered, 1, "at least one chunk should be delivered")
}

// TestBackpressure_StreamChunksDroppedWhenCongested verifies that
// StreamChunkMsg messages use non-blocking try-send and are dropped when
// the TUI sender is congested.
func TestBackpressure_StreamChunksDroppedWhenCongested(t *testing.T) {
	block := make(chan struct{})
	sender := &countingSender{block: block}
	d := newDispatcherWithSender(sender)
	defer d.close()

	// First message acquires semaphore and blocks
	d.notify(shogunate.StreamChunkMsg{ChannelID: "forge", Text: "first chunk"})
	time.Sleep(50 * time.Millisecond)

	// Flood with more messages — should be dropped
	const extra = 50
	for i := 0; i < extra; i++ {
		d.notify(shogunate.StreamChunkMsg{ChannelID: "forge", Text: "extra chunk"})
	}
	time.Sleep(200 * time.Millisecond)

	close(block)
	time.Sleep(100 * time.Millisecond)

	med, low := d.DroppedCounts()
	assert.Greater(t, med, int64(0), "chunk messages should be dropped when congested")
	assert.Equal(t, int64(0), low, "low drops always zero")

	// The first message should have been delivered
	msgs := sender.messages()
	medDelivered := 0
	for _, m := range msgs {
		if _, ok := m.(shogunate.StreamChunkMsg); ok {
			medDelivered++
		}
	}
	assert.GreaterOrEqual(t, medDelivered, 1, "at least one chunk should be delivered")
}

// TestBackpressure_ControlMessagesNeverDropped verifies that high-priority
// control messages (StreamCompleteMsg, StreamStartMsg, etc.) are always
// delivered via blocking send, even when the TUI sender is congested.
func TestBackpressure_ControlMessagesNeverDropped(t *testing.T) {
	block := make(chan struct{})
	sender := &countingSender{block: block}
	d := newDispatcherWithSender(sender)
	defer d.close()

	// Send a mix of priorities while sender is blocked
	const highCount = 10
	const chunkCount = 40

	for i := 0; i < highCount; i++ {
		d.notify(shogunate.StreamCompleteMsg{ChannelID: "forge"})
	}
	for i := 0; i < chunkCount; i++ {
		d.notify(shogunate.StreamChunkMsg{ChannelID: "forge", Text: "chunk"})
	}

	// Wait for the dispatch loop to pick up messages
	time.Sleep(200 * time.Millisecond)

	// Unblock the sender — all high-priority must come through
	close(block)
	waitForCount(t, &sender.sendCount, highCount, 5*time.Second)

	msgs := sender.messages()
	highDelivered := 0
	for _, m := range msgs {
		switch m.(type) {
		case shogunate.StreamCompleteMsg:
			highDelivered++
		}
	}

	assert.Equal(t, highCount, highDelivered,
		"all high-priority StreamCompleteMsg must be delivered, never dropped")

	med, low := d.DroppedCounts()
	_ = med // chunk drops are expected
	assert.Equal(t, int64(0), low, "low drops always zero")
}

// TestBackpressure_CongestedSenderPriorityGuarantee verifies that under
// heavy congestion (sender always busy), only high-priority messages are
// guaranteed delivery. Chunk messages are subject to backpressure relief
// (dropping).
func TestBackpressure_CongestedSenderPriorityGuarantee(t *testing.T) {
	// Use a sender that blocks for a long time on each Send
	block := make(chan struct{})
	sender := &countingSender{block: block}
	d := newDispatcherWithSender(sender)
	defer d.close()

	// Flood with high-priority and chunk messages (reasoning merged into chunks)
	const highCount = 5
	for i := 0; i < highCount; i++ {
		d.notify(shogunate.StreamCompleteMsg{ChannelID: "forge"})
	}
	for i := 0; i < 200; i++ {
		d.notify(shogunate.StreamChunkMsg{ChannelID: "forge", Text: "data"})
	}

	// Give dispatcher time to process and block on first high-priority send
	time.Sleep(200 * time.Millisecond)

	// Unblock and let everything drain
	close(block)
	waitForCount(t, &sender.sendCount, highCount, 5*time.Second)

	msgs := sender.messages()

	// Count delivered by type
	highDelivered := 0
	for _, m := range msgs {
		switch m.(type) {
		case shogunate.StreamCompleteMsg:
			highDelivered++
		}
	}

	// High-priority: ALL must be delivered
	assert.Equal(t, highCount, highDelivered,
		"all high-priority StreamCompleteMsg must be delivered even under congestion")

	// Chunks: some may be delivered, but most should be dropped
	med, low := d.DroppedCounts()
	assert.Greater(t, med, int64(0),
		"chunk messages should have drops under heavy congestion")
	assert.Equal(t, int64(0), low, "low drops always zero")
}
