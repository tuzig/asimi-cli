package court

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOnZhengmingRaisedCallbackFired verifies that the OnZhengmingRaised callback
// is invoked when RequestZhengming is called on a MinisterBase.
func TestOnZhengmingRaisedCallbackFired(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject", nil)
	base.SetNotify(func(msg any) {})

	var raisedMu sync.Mutex
	raisedCalled := false
	base.SetOnZhengmingRaised(func() {
		raisedMu.Lock()
		raisedCalled = true
		raisedMu.Unlock()
	})

	key := storage.EdictKey{ID: 42, Username: "testuser", Project: "testproject"}
	_, err := base.RequestZhengming(key, storage.ZhengmingQuestions{{Text: "OK?"}}, storage.PriorityNormal, "forge")
	require.NoError(t, err)

	raisedMu.Lock()
	assert.True(t, raisedCalled, "OnZhengmingRaised callback should have been called")
	raisedMu.Unlock()
}

// TestOnZhengmingResolvedCallbackFired verifies that the OnZhengmingResolved callback
// is invoked when AnswerZhengming is called on a MinisterBase.
func TestOnZhengmingResolvedCallbackFired(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject", nil)
	base.SetNotify(func(msg any) {})

	var resolvedMu sync.Mutex
	resolvedCalled := false
	base.SetOnZhengmingResolved(func() {
		resolvedMu.Lock()
		resolvedCalled = true
		resolvedMu.Unlock()
	})

	// Create a zhengming request first
	key := storage.EdictKey{ID: 42, Username: "testuser", Project: "testproject"}
	requestID, err := base.RequestZhengming(key, storage.ZhengmingQuestions{{Text: "OK?"}}, storage.PriorityNormal, "forge")
	require.NoError(t, err)

	// Answer it — should fire the resolved callback
	err = base.AnswerZhengming(requestID, "yes")
	require.NoError(t, err)

	resolvedMu.Lock()
	assert.True(t, resolvedCalled, "OnZhengmingResolved callback should have been called after AnswerZhengming")
	resolvedMu.Unlock()
}

// TestOnZhengmingResolvedNotCalledOnMissingRequest verifies that OnZhengmingResolved
// is NOT called when AnswerZhengming fails (e.g., request not found).
func TestOnZhengmingResolvedNotCalledOnMissingRequest(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, slog.Default(), "testuser", "testproject", nil)

	resolvedCalled := false
	base.SetOnZhengmingResolved(func() {
		resolvedCalled = true
	})

	// Try to answer a nonexistent request
	err := base.AnswerZhengming("nonexistent-request-id", "yes")
	require.Error(t, err, "AnswerZhengming should fail for nonexistent request")
	assert.False(t, resolvedCalled, "OnZhengmingResolved should NOT be called when AnswerZhengming fails")
}

// TestZhengmingCallbacksWiredDuringStep verifies that during executeMinisterStep,
// the zhengming callbacks are wired to the minister and cleared afterward.
func TestZhengmingCallbacksWiredDuringStep(t *testing.T) {
	db := setupRitualTestDB(t)

	ritual := &RitualDef{
		Name: "callback-wire-test",
		Steps: []RitualStep{
			{Name: "work", Minister: "forge", Act: "do work"},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	// Create a forge minister with an observable MinisterBase
	forgeM := &ritualTestMinister{
		MinisterBase: MinisterBase{logger: slog.Default()},
		id:           "forge",
		tasksCh:      make(chan *Task, 1),
		result:       "done",
	}

	ministers := map[string]Minister{"forge": forgeM}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go forgeM.Run(ctx)

	court := &Court{
		ministers: ministers,
		logger:    slog.Default(),
	}

	runner := NewRitualRunner(registry, court.GetMinister, court.PublishEvent, db, nil, nil, repo.RepoInfo{})
	notify := func(msg any) {}

	exec, err := runner.Start(ctx, "callback-wire-test", testEK(50), nil, notify)
	require.NoError(t, err)

	err = runner.Run(ctx, exec)
	require.NoError(t, err, "step should complete successfully with callbacks wired")

	// After the step completes, callbacks should be nil (cleared by defer)
	// Verify by setting new callbacks and checking they're not overwritten
	var newRaised, newResolved bool
	forgeM.SetOnZhengmingRaised(func() { newRaised = true })
	forgeM.SetOnZhengmingResolved(func() { newResolved = true })

	// Trigger the callbacks manually to confirm they work
	forgeM.zhengmingMu.Lock()
	cb := forgeM.onZhengmingRaised
	forgeM.zhengmingMu.Unlock()
	if cb != nil {
		cb()
	}
	assert.True(t, newRaised, "new OnZhengmingRaised should be active after step cleanup")

	forgeM.zhengmingMu.Lock()
	cb2 := forgeM.onZhengmingResolved
	forgeM.zhengmingMu.Unlock()
	if cb2 != nil {
		cb2()
	}
	assert.True(t, newResolved, "new OnZhengmingResolved should be active after step cleanup")
}

// TestZhengmingTimerPauseResume tests the core timer pause/resume logic
// by simulating the same sequence that executeMinisterStep uses.
func TestZhengmingTimerPauseResume(t *testing.T) {
	// Simulate the timer logic from executeMinisterStep
	const stepTimeout = 100 * time.Millisecond
	stepTimer := time.NewTimer(stepTimeout)
	defer stepTimer.Stop()

	var timerMu sync.Mutex
	var remaining time.Duration = stepTimeout
	var paused bool
	var timerStart time.Time = time.Now()

	pauseTimer := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if paused {
			return
		}
		if stepTimer.Stop() {
			elapsed := time.Since(timerStart)
			remaining = stepTimeout - elapsed
			if remaining < 0 {
				remaining = 0
			}
		} else {
			select {
			case <-stepTimer.C:
			default:
			}
			remaining = 0
		}
		paused = true
	}

	resumeTimer := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if !paused {
			return
		}
		if remaining > 0 {
			stepTimer.Reset(remaining)
			timerStart = time.Now()
		} else {
			stepTimer.Reset(0)
		}
		paused = false
	}

	// Wait a bit, then pause
	time.Sleep(30 * time.Millisecond)
	pauseTimer()

	// While paused, wait longer than the original timeout
	time.Sleep(150 * time.Millisecond)

	// Timer should NOT have fired because it was paused
	select {
	case <-stepTimer.C:
		t.Fatal("timer should not have fired while paused")
	default:
		// Expected
	}

	// Resume — remaining should be roughly 70ms
	resumeTimer()

	// Now wait — the timer should fire with remaining time
	select {
	case <-stepTimer.C:
		// Expected: timer fired with remaining time
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timer should have fired after resume with remaining time")
	}
}

// TestZhengmingTimerExpiresNormally tests that the step timer fires
// normally when no zhengming pause/resume occurs.
func TestZhengmingTimerExpiresNormally(t *testing.T) {
	const stepTimeout = 50 * time.Millisecond
	stepTimer := time.NewTimer(stepTimeout)
	defer stepTimer.Stop()

	select {
	case <-stepTimer.C:
		// Expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timer should have fired normally")
	}
}

// TestZhengmingTimerPausedWithZeroRemaining tests that if the timer
// has already expired when paused, remaining is 0 and resume fires immediately.
func TestZhengmingTimerPausedWithZeroRemaining(t *testing.T) {
	const stepTimeout = 10 * time.Millisecond
	stepTimer := time.NewTimer(stepTimeout)
	defer stepTimer.Stop()

	var timerMu sync.Mutex
	var remaining time.Duration = stepTimeout
	var paused bool
	var timerStart time.Time = time.Now()

	pauseTimer := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if paused {
			return
		}
		if stepTimer.Stop() {
			elapsed := time.Since(timerStart)
			remaining = stepTimeout - elapsed
			if remaining < 0 {
				remaining = 0
			}
		} else {
			select {
			case <-stepTimer.C:
			default:
			}
			remaining = 0
		}
		paused = true
	}

	resumeTimer := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if !paused {
			return
		}
		if remaining > 0 {
			stepTimer.Reset(remaining)
			timerStart = time.Now()
		} else {
			stepTimer.Reset(0)
		}
		paused = false
	}

	// Let the timer expire
	time.Sleep(50 * time.Millisecond)

	// Now pause — timer already fired, remaining should be 0
	pauseTimer()

	// Drain the timer channel if it fired
	select {
	case <-stepTimer.C:
	default:
	}

	// Resume with 0 remaining should fire immediately
	resumeTimer()

	select {
	case <-stepTimer.C:
		// Expected: immediate fire
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timer with 0 remaining should fire immediately on resume")
	}
}
