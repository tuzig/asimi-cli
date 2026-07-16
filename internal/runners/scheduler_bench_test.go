package runners

import (
	"context"
	"testing"
	"time"
)

// BenchmarkSchedulerSerial100SlowCommands measures dispatch overhead with
// tools that sleep 1ms each. Under the serial scheduler 100 calls take ~100ms;
// a concurrent implementation with maxConcurrency=4 should finish in ~25ms.
// This makes the dispatch model visible in the benchmark, unlike no-op tools
// which only measure channel/map/UUID overhead.
//
// Run with:
//
//	go test -bench BenchmarkScheduler -benchmem ./internal/runners/
func BenchmarkSchedulerSerial100SlowCommands(b *testing.B) {
	slowTool := &mockTool{
		name:        "slow",
		description: "1ms tool",
		callFunc: func(ctx context.Context, input string) (string, error) {
			time.Sleep(1 * time.Millisecond)
			return "ok", nil
		},
	}

	for n := 0; n < b.N; n++ {
		b.StopTimer()
		scheduler := NewCoreToolScheduler(nil)
		chans := make([]<-chan ToolCallResult, 100)
		b.StartTimer()

		for i := 0; i < 100; i++ {
			chans[i] = scheduler.Schedule(context.Background(), slowTool, "x")
		}
		for i := 0; i < 100; i++ {
			<-chans[i]
		}
	}
}

// TestSchedulerConcurrencyGuardrail fails if the scheduler cannot dispatch
// 8 tool calls (each sleeping 50ms) within a deadline that only concurrent
// dispatch can meet. Under serial dispatch the floor is 8×50ms = 400ms, so a
// 2s deadline gives comfortable headroom while catching deadlocks or broken
// dispatch. When e657 lands concurrent dispatch, tighten the deadline to
// assert the speedup (e.g. 600ms for maxConcurrency=4).
func TestSchedulerConcurrencyGuardrail(t *testing.T) {
	slowTool := &mockTool{
		name:        "slow",
		description: "50ms tool",
		callFunc: func(ctx context.Context, input string) (string, error) {
			time.Sleep(50 * time.Millisecond)
			return "ok", nil
		},
	}

	scheduler := NewCoreToolScheduler(nil)
	chans := make([]<-chan ToolCallResult, 8)
	for i := range chans {
		chans[i] = scheduler.Schedule(context.Background(), slowTool, "x")
	}

	deadline := 2 * time.Second
	for i, ch := range chans {
		select {
		case <-ch:
		case <-time.After(deadline):
			t.Fatalf("call %d did not complete within %v", i, deadline)
		}
	}
}
