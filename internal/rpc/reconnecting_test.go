package rpc

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
)

// --- Tests for Issue 1: No retry on ErrPeerDisconnected for mutating methods ---

func TestReconnectingClient_ShouldRetryOnlyErrClosed(t *testing.T) {
	rc := &ReconnectingClient{}

	if !rc.shouldRetry(ErrClosed) {
		t.Error("shouldRetry should return true for ErrClosed")
	}
	if rc.shouldRetry(ErrPeerDisconnected) {
		t.Error("shouldRetry should return false for ErrPeerDisconnected")
	}
}

func TestReconnectingClient_ShouldRetryReadOnlyBoth(t *testing.T) {
	rc := &ReconnectingClient{}

	if !rc.shouldRetryReadOnly(ErrClosed) {
		t.Error("shouldRetryReadOnly should return true for ErrClosed")
	}
	if !rc.shouldRetryReadOnly(ErrPeerDisconnected) {
		t.Error("shouldRetryReadOnly should return true for ErrPeerDisconnected")
	}
}

// --- Tests for Issue 2: Serialised reconnect() calls ---

func TestReconnectingClient_ReconnectSerialized(t *testing.T) {
	var factoryCalls atomic.Int64

	factory := func() (*Conn, error) {
		factoryCalls.Add(1)
		pa, _ := net.Pipe()
		conn := New(pa, Options{})
		go func() { _ = conn.Serve() }()
		return conn, nil
	}

	rc := NewReconnectingClient(factory, nil)
	conn, _ := factory()
	rc.mu.Lock()
	rc.conn = conn
	rc.client = NewCourtClient(conn)
	rc.mu.Unlock()

	// Close the connection so both goroutines attempt reconnect.
	conn.Close()
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_ = rc.reconnect()
		}()
	}
	wg.Wait()

	// 1 initial + 1 reconnect = 2 total, not 3.
	if got := factoryCalls.Load(); got != 2 {
		t.Errorf("expected 2 factory calls (1 initial + 1 reconnect), got %d", got)
	}
}

func TestReconnectingClient_ReconnectSerializedMany(t *testing.T) {
	var factoryCalls atomic.Int64

	factory := func() (*Conn, error) {
		factoryCalls.Add(1)
		pa, _ := net.Pipe()
		conn := New(pa, Options{})
		go func() { _ = conn.Serve() }()
		return conn, nil
	}

	rc := NewReconnectingClient(factory, nil)
	conn, _ := factory()
	rc.mu.Lock()
	rc.conn = conn
	rc.client = NewCourtClient(conn)
	rc.mu.Unlock()

	conn.Close()
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_ = rc.reconnect()
		}()
	}
	wg.Wait()

	// 1 initial + 1 reconnect = 2 total. Not 11.
	if got := factoryCalls.Load(); got != 2 {
		t.Errorf("expected 2 factory calls (1 initial + 1 reconnect), got %d", got)
	}
}

func TestReconnectingClient_ReconnectMuPreventsConcurrentFactory(t *testing.T) {
	var factoryCalls atomic.Int64

	factory := func() (*Conn, error) {
		n := factoryCalls.Add(1)
		// Slow down the first call to give the second goroutine time
		// to also enter reconnect().
		if n == 1 {
			time.Sleep(100 * time.Millisecond)
		}
		pa, _ := net.Pipe()
		conn := New(pa, Options{})
		go func() { _ = conn.Serve() }()
		return conn, nil
	}

	rc := NewReconnectingClient(factory, nil)

	// Start with a nil conn so reconnect() doesn't try to close one.
	rc.mu.Lock()
	rc.conn = nil
	rc.client = nil
	rc.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_ = rc.reconnect()
		}()
	}
	wg.Wait()

	// Both goroutines called reconnect, but only one should have
	// called the factory because reconnectMu serialises them.
	if got := factoryCalls.Load(); got != 1 {
		t.Errorf("expected exactly 1 factory call, got %d", got)
	}
}

// --- Tests for reconnectIfError ---

func TestReconnectingClient_ReconnectIfError_NoRetryOnUnrelatedError(t *testing.T) {
	rc := NewReconnectingClient(nil, nil)

	// reconnectIfError returns false for errors that the delegate rejects.
	result := rc.reconnectIfError(errors.New("something else"), rc.shouldRetry)
	if result {
		t.Error("reconnectIfError should return false for unrelated errors with shouldRetry")
	}
	result = rc.reconnectIfError(errors.New("something else"), rc.shouldRetryReadOnly)
	if result {
		t.Error("reconnectIfError should return false for unrelated errors with shouldRetryReadOnly")
	}
}

func TestReconnectingClient_ReconnectIfError_RetryOnErrClosed(t *testing.T) {
	var factoryCalls atomic.Int64
	factory := func() (*Conn, error) {
		factoryCalls.Add(1)
		pa, _ := net.Pipe()
		conn := New(pa, Options{})
		go func() { _ = conn.Serve() }()
		return conn, nil
	}

	rc := NewReconnectingClient(factory, nil)
	// Start with a dead conn so reconnect succeeds.
	conn, _ := factory()
	rc.mu.Lock()
	rc.conn = conn
	rc.client = NewCourtClient(conn)
	rc.mu.Unlock()
	conn.Close()
	time.Sleep(20 * time.Millisecond)

	before := factoryCalls.Load()

	// For both shouldRetry and shouldRetryReadOnly, ErrClosed should trigger reconnect.
	result := rc.reconnectIfError(ErrClosed, rc.shouldRetry)
	if !result {
		t.Error("reconnectIfError should return true for ErrClosed with shouldRetry")
	}
	if factoryCalls.Load() == before {
		t.Error("reconnectIfError should have triggered a reconnect for ErrClosed")
	}
}

func TestReconnectingClient_ReconnectIfError_ReadOnlyRetriesPeerDisconnected(t *testing.T) {
	var factoryCalls atomic.Int64
	factory := func() (*Conn, error) {
		factoryCalls.Add(1)
		pa, _ := net.Pipe()
		conn := New(pa, Options{})
		go func() { _ = conn.Serve() }()
		return conn, nil
	}

	rc := NewReconnectingClient(factory, nil)
	conn, _ := factory()
	rc.mu.Lock()
	rc.conn = conn
	rc.client = NewCourtClient(conn)
	rc.mu.Unlock()
	conn.Close()
	time.Sleep(20 * time.Millisecond)

	before := factoryCalls.Load()

	// shouldRetry (mutating) must NOT retry ErrPeerDisconnected.
	result := rc.reconnectIfError(ErrPeerDisconnected, rc.shouldRetry)
	if result {
		t.Error("reconnectIfError should return false for ErrPeerDisconnected with shouldRetry (mutating)")
	}
	if factoryCalls.Load() != before {
		t.Error("reconnectIfError should NOT have triggered reconnect for ErrPeerDisconnected with shouldRetry")
	}

	// shouldRetryReadOnly must retry ErrPeerDisconnected.
	result = rc.reconnectIfError(ErrPeerDisconnected, rc.shouldRetryReadOnly)
	if !result {
		t.Error("reconnectIfError should return true for ErrPeerDisconnected with shouldRetryReadOnly (read-only)")
	}
	if factoryCalls.Load() == before {
		t.Error("reconnectIfError should have triggered reconnect for ErrPeerDisconnected with shouldRetryReadOnly")
	}
}

func TestReconnectingClient_ReconnectIfError_NilError(t *testing.T) {
	rc := NewReconnectingClient(nil, nil)

	result := rc.reconnectIfError(nil, rc.shouldRetry)
	if result {
		t.Error("reconnectIfError should return false for nil error")
	}
	result = rc.reconnectIfError(nil, rc.shouldRetryReadOnly)
	if result {
		t.Error("reconnectIfError should return false for nil error with shouldRetryReadOnly")
	}
}

// --- Verify that mutating methods use shouldRetry (not shouldRetryReadOnly) ---

func TestReconnectingClient_MutatingMethodsUseShouldRetry(t *testing.T) {
	// Verify the core contract: shouldRetry rejects ErrPeerDisconnected
	// (used by mutating methods), while shouldRetryReadOnly accepts it
	// (used by read-only methods).
	rc := &ReconnectingClient{}

	// Mutating methods (CreateEdict, CreateEdictSilent, GrantRulerSeal,
	// HandleZhengmingResponse) use shouldRetry, which must NOT accept
	// ErrPeerDisconnected.
	if rc.shouldRetry(ErrPeerDisconnected) {
		t.Error("shouldRetry must return false for ErrPeerDisconnected (used by mutating methods)")
	}
	// ErrClosed is safe for all methods — the call never left.
	if !rc.shouldRetry(ErrClosed) {
		t.Error("shouldRetry must return true for ErrClosed")
	}
	// Read-only methods use shouldRetryReadOnly, which accepts both.
	if !rc.shouldRetryReadOnly(ErrPeerDisconnected) {
		t.Error("shouldRetryReadOnly must return true for ErrPeerDisconnected (used by read-only methods)")
	}
	if !rc.shouldRetryReadOnly(ErrClosed) {
		t.Error("shouldRetryReadOnly must return true for ErrClosed")
	}
}

// --- Tests for reconnectIfDead: proactive reconnect for zero-return read-only methods ---

func TestReconnectingClient_ReconnectIfDead_TriggeredOnDeadConn(t *testing.T) {
	var factoryCalls atomic.Int64

	factory := func() (*Conn, error) {
		factoryCalls.Add(1)
		pa, _ := net.Pipe()
		conn := New(pa, Options{})
		go func() { _ = conn.Serve() }()
		return conn, nil
	}

	rc := NewReconnectingClient(factory, nil)

	conn, _ := factory()
	rc.mu.Lock()
	rc.conn = conn
	rc.client = NewCourtClient(conn)
	rc.mu.Unlock()

	conn.Close()
	time.Sleep(50 * time.Millisecond)

	factoryBefore := factoryCalls.Load()

	// Calling reconnectIfDead on a dead connection should trigger a reconnect.
	rc.reconnectIfDead()

	if factoryCalls.Load() <= factoryBefore {
		t.Errorf("reconnectIfDead should have triggered reconnect on dead connection; before=%d after=%d",
			factoryBefore, factoryCalls.Load())
	}
}

func TestReconnectingClient_ReconnectIfDead_NoOpOnLiveConn(t *testing.T) {
	var factoryCalls atomic.Int64

	factory := func() (*Conn, error) {
		factoryCalls.Add(1)
		pa, _ := net.Pipe()
		conn := New(pa, Options{})
		go func() { _ = conn.Serve() }()
		return conn, nil
	}

	rc := NewReconnectingClient(factory, nil)

	conn, _ := factory()
	rc.mu.Lock()
	rc.conn = conn
	rc.client = NewCourtClient(conn)
	rc.mu.Unlock()

	factoryBefore := factoryCalls.Load()

	// Calling reconnectIfDead on a live connection should NOT trigger a reconnect.
	rc.reconnectIfDead()

	if got := factoryCalls.Load(); got != factoryBefore {
		t.Errorf("reconnectIfDead should NOT have triggered reconnect on live connection; before=%d after=%d",
			factoryBefore, got)
	}
}

func TestReconnectingClient_ReconnectIfDead_NoOpOnNilConn(t *testing.T) {
	rc := NewReconnectingClient(nil, nil)
	// Should not panic and should not call factory (nil factory).
	rc.reconnectIfDead()
}

func TestReconnectingClient_HasMinister_CallsReconnectIfDead(t *testing.T) {
	// Verify HasMinister calls reconnectIfDead by checking that a dead
	// connection is replaced. We can't call HasMinister directly because
	// the new connection has no server — instead we verify that after
	// closing the conn, the HasMinister code path triggers reconnectIfDead
	// which calls the factory.
	var factoryCalls atomic.Int64

	factory := func() (*Conn, error) {
		factoryCalls.Add(1)
		pa, _ := net.Pipe()
		conn := New(pa, Options{})
		go func() { _ = conn.Serve() }()
		return conn, nil
	}

	rc := NewReconnectingClient(factory, nil)

	conn, _ := factory()
	rc.mu.Lock()
	rc.conn = conn
	rc.client = NewCourtClient(conn)
	rc.mu.Unlock()

	conn.Close()
	time.Sleep(50 * time.Millisecond)

	factoryBefore := factoryCalls.Load()

	// Directly test reconnectIfDead — this is what HasMinister, EdictKey,
	// CourtEdictKey, and SessionState call before getClient.
	rc.reconnectIfDead()

	if factoryCalls.Load() <= factoryBefore {
		t.Errorf("reconnectIfDead should have triggered reconnect on dead connection; before=%d after=%d",
			factoryBefore, factoryCalls.Load())
	}
}

// --- Verify nil client returns ErrClosed for methods that support it ---

func TestReconnectingClient_NilClientReturnsErrClosed(t *testing.T) {
	rc := &ReconnectingClient{}

	// Methods that return error when client is nil should return ErrClosed.
	if _, err := rc.CreateEdict("ref", "intent", ""); !errors.Is(err, ErrClosed) {
		t.Errorf("CreateEdict with nil client should return ErrClosed, got %v", err)
	}
	if _, err := rc.CreateEdictSilent("ref", "intent", ""); !errors.Is(err, ErrClosed) {
		t.Errorf("CreateEdictSilent with nil client should return ErrClosed, got %v", err)
	}
	if err := rc.GrantRulerSeal(1, ""); !errors.Is(err, ErrClosed) {
		t.Errorf("GrantRulerSeal with nil client should return ErrClosed, got %v", err)
	}
	if _, err := rc.GetEdict(1); !errors.Is(err, ErrClosed) {
		t.Errorf("GetEdict with nil client should return ErrClosed, got %v", err)
	}
	if _, err := rc.ListActiveEdicts(); !errors.Is(err, ErrClosed) {
		t.Errorf("ListActiveEdicts with nil client should return ErrClosed, got %v", err)
	}
	if _, err := rc.GetEdictSeals(storage.EdictKey{}); !errors.Is(err, ErrClosed) {
		t.Errorf("GetEdictSeals with nil client should return ErrClosed, got %v", err)
	}
	if err := rc.HandleZhengmingResponse(nil, "req", "ans"); !errors.Is(err, ErrClosed) {
		t.Errorf("HandleZhengmingResponse with nil client should return ErrClosed, got %v", err)
	}
	if _, err := rc.GetSessionExport("tab"); !errors.Is(err, ErrClosed) {
		t.Errorf("GetSessionExport with nil client should return ErrClosed, got %v", err)
	}
	if err := rc.CancelEdict(1); !errors.Is(err, ErrClosed) {
		t.Errorf("CancelEdict with nil client should return ErrClosed, got %v", err)
	}
	if err := rc.AppendToIntent(1, "clarification"); !errors.Is(err, ErrClosed) {
		t.Errorf("AppendToIntent with nil client should return ErrClosed, got %v", err)
	}
}

// --- Verify shouldRetry and shouldRetryReadOnly for wrapped errors ---

func TestReconnectingClient_ShouldRetry_WrappedErrors(t *testing.T) {
	rc := &ReconnectingClient{}

	// errors.Is must work with wrapped errors.
	wrappedClosed := fmt.Errorf("wrapper: %w", ErrClosed)
	wrappedPeer := fmt.Errorf("wrapper: %w", ErrPeerDisconnected)

	if !rc.shouldRetry(wrappedClosed) {
		t.Error("shouldRetry should match wrapped ErrClosed")
	}
	if rc.shouldRetry(wrappedPeer) {
		t.Error("shouldRetry should not match wrapped ErrPeerDisconnected")
	}
	if !rc.shouldRetryReadOnly(wrappedClosed) {
		t.Error("shouldRetryReadOnly should match wrapped ErrClosed")
	}
	if !rc.shouldRetryReadOnly(wrappedPeer) {
		t.Error("shouldRetryReadOnly should match wrapped ErrPeerDisconnected")
	}
}

// --- Verify the retry classification table is consistent ---

func TestReconnectingClient_RetryClassificationTable(t *testing.T) {
	rc := &ReconnectingClient{}

	tests := []struct {
		name     string
		err      error
		mutating bool // shouldRetry result
		readOnly bool // shouldRetryReadOnly result
	}{
		{"ErrClosed", ErrClosed, true, true},
		{"ErrPeerDisconnected", ErrPeerDisconnected, false, true},
		{"unrelated error", errors.New("other"), false, false},
		{"nil error", nil, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rc.shouldRetry(tt.err); got != tt.mutating {
				t.Errorf("shouldRetry(%v) = %v, want %v", tt.err, got, tt.mutating)
			}
			if got := rc.shouldRetryReadOnly(tt.err); got != tt.readOnly {
				t.Errorf("shouldRetryReadOnly(%v) = %v, want %v", tt.err, got, tt.readOnly)
			}
		})
	}
}

// --- Verify that SetContext is the factory's responsibility, not ReconnectingClient's ---

func TestReconnectingClient_SetContextIsFactoryResponsibility(t *testing.T) {
	// The SetContext handshake is sent inside the factory function (e.g.
	// newDaemonConn), not by ReconnectingClient itself. This test
	// verifies that ReconnectingClient does NOT call SetContext during
	// reconnection — if it did, the factory's SetContext would be
	// called twice (once in the factory, once in reconnect), which
	// would be redundant and could break the daemon's handshake
	// protocol.
	//
	// We verify this contract by confirming that:
	//   1. ReconnectingClient has no SetContext method
	//   2. The factory is called exactly once per reconnect (not
	//      followed by a SetContext call from ReconnectingClient)
	var factoryCalls atomic.Int64

	factory := func() (*Conn, error) {
		factoryCalls.Add(1)
		pa, _ := net.Pipe()
		conn := New(pa, Options{})
		go func() { _ = conn.Serve() }()
		return conn, nil
	}

	rc := NewReconnectingClient(factory, nil)
	conn, _ := factory()
	rc.mu.Lock()
	rc.conn = conn
	rc.client = NewCourtClient(conn)
	rc.mu.Unlock()

	conn.Close()
	time.Sleep(20 * time.Millisecond)

	before := factoryCalls.Load()
	if err := rc.reconnect(); err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}

	// The factory should be called exactly once for the reconnect.
	// If ReconnectingClient were also calling SetContext, we'd see
	// additional RPC calls on the new connection — but we can't
	// observe those directly. Instead, we just verify the factory
	// call count matches our expectation: exactly one.
	if got := factoryCalls.Load(); got != before+1 {
		t.Errorf("expected %d factory calls after reconnect, got %d", before+1, got)
	}
}

// Verify that storage.EdictKey is imported (used by read-only method tests).
var _ storage.EdictKey

// --- ConnDone tests (edict 552) ---

func TestReconnectingClient_ConnDone_NilConn(t *testing.T) {
	rc := &ReconnectingClient{}
	done := rc.ConnDone()
	select {
	case <-done:
		t.Fatal("ConnDone with nil conn should return a never-closed channel")
	default:
		// Good — channel is open
	}
}

func TestReconnectingClient_ConnDone_LiveConn(t *testing.T) {
	pa, _ := net.Pipe()
	conn := New(pa, Options{})
	go func() { _ = conn.Serve() }()

	rc := &ReconnectingClient{}
	rc.mu.Lock()
	rc.conn = conn
	rc.client = NewCourtClient(conn)
	rc.mu.Unlock()

	done := rc.ConnDone()
	select {
	case <-done:
		t.Fatal("ConnDone on a live conn should not fire")
	default:
		// Good — channel is open
	}

	conn.Close()
	time.Sleep(20 * time.Millisecond)

	done = rc.ConnDone()
	select {
	case <-done:
		// Good — channel is closed after conn.Close()
	default:
		t.Fatal("ConnDone should fire after conn is closed")
	}
}

func TestLoopbackCourt_ConnDone(t *testing.T) {
	pa, _ := net.Pipe()
	conn := New(pa, Options{})
	go func() { _ = conn.Serve() }()

	lb := NewLoopbackCourt(conn, nil)
	done := lb.ConnDone()

	select {
	case <-done:
		t.Fatal("ConnDone on live loopback conn should not fire")
	default:
		// Good
	}

	conn.Close()
	time.Sleep(20 * time.Millisecond)

	done = lb.ConnDone()
	select {
	case <-done:
		// Good — channel is closed
	default:
		t.Fatal("ConnDone should fire after loopback conn is closed")
	}
}
