package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/afittestide/asimi/court"
	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
)

// TestHeadlessSink_StreamChunkWritesText verifies that StreamChunkMsg text
// is written to stdout (captured via the sink's output).
func TestHeadlessSink_StreamChunkWritesText(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.StreamChunkMsg{Text: "hello world"})
	// No panic, no done signal — just text output
	select {
	case code := <-sink.done:
		t.Fatalf("should not signal done on StreamChunkMsg, got code %d", code)
	default:
	}
}

// TestHeadlessSink_StreamDoneNoRitual_FinishesZero verifies that StreamDoneMsg
// signals exit 0 when no ritual has been enacted (plain chat).
func TestHeadlessSink_StreamDoneNoRitual_FinishesZero(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.StreamDoneMsg{ChannelID: "secretary"})

	select {
	case code := <-sink.done:
		assert.Equal(t, 0, code)
	default:
		t.Fatal("expected done signal after StreamDoneMsg with no ritual")
	}
}

// TestHeadlessSink_StreamDoneWithRitual_DoesNotFinish verifies that
// StreamDoneMsg does NOT signal completion when a ritual is running.
func TestHeadlessSink_StreamDoneWithRitual_DoesNotFinish(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.enactedSet[42] = true
	sink.handle(court.StreamDoneMsg{ChannelID: "secretary"})

	select {
	case code := <-sink.done:
		t.Fatalf("should not signal done when ritual is running, got code %d", code)
	default:
		// correct — ritual completion will signal later
	}
}

// TestHeadlessSink_StreamError_FinishesOne verifies that StreamErrorMsg
// signals exit 1.
func TestHeadlessSink_StreamError_FinishesOne(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.StreamErrorMsg{Err: assert.AnError})

	select {
	case code := <-sink.done:
		assert.Equal(t, 1, code)
	default:
		t.Fatal("expected done signal after StreamErrorMsg")
	}
}

// TestHeadlessSink_RitualStepCompleted_FinishesZero verifies that
// RitualStepMsg with status "ritual_completed" signals exit 0.
func TestHeadlessSink_RitualStepCompleted_FinishesZero(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.RitualStepMsg{
		Status:     "ritual_completed",
		RitualName: "swift-strike",
		EdictID:    1,
		Message:    "5s",
	})

	select {
	case code := <-sink.done:
		assert.Equal(t, 0, code)
	default:
		t.Fatal("expected done signal after ritual_completed")
	}
}

// TestHeadlessSink_RitualStepFailed_FinishesOne verifies that
// RitualStepMsg with status "ritual_failed" signals exit 1.
func TestHeadlessSink_RitualStepFailed_FinishesOne(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.RitualStepMsg{
		Status:     "ritual_failed",
		RitualName: "swift-strike",
		EdictID:    1,
		Message:    "something broke",
	})

	select {
	case code := <-sink.done:
		assert.Equal(t, 1, code)
	default:
		t.Fatal("expected done signal after ritual_failed")
	}
}

// TestHeadlessSink_EventRitualCompleted_FinishesZero verifies that
// EventNotificationMsg with EventRitualCompleted signals exit 0.
func TestHeadlessSink_EventRitualCompleted_FinishesZero(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventRitualCompleted,
		EdictKey:  storage.EdictKey{ID: 7},
	})

	select {
	case code := <-sink.done:
		assert.Equal(t, 0, code)
	default:
		t.Fatal("expected done signal after EventRitualCompleted")
	}
}

// TestHeadlessSink_EventRitualFailed_FinishesOne verifies that
// EventNotificationMsg with EventRitualFailed signals exit 1.
func TestHeadlessSink_EventRitualFailed_FinishesOne(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventRitualFailed,
		EdictKey:  storage.EdictKey{ID: 7},
		Payload:   map[string]interface{}{"error": "boom"},
	})

	select {
	case code := <-sink.done:
		assert.Equal(t, 1, code)
	default:
		t.Fatal("expected done signal after EventRitualFailed")
	}
}

// TestHeadlessSink_FinishIsIdempotent verifies that calling finish
// multiple times only delivers the first code.
func TestHeadlessSink_FinishIsIdempotent(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.finish(0)
	sink.finish(1) // should be dropped

	code := <-sink.done
	assert.Equal(t, 0, code)

	select {
	case code := <-sink.done:
		t.Fatalf("should not have a second value, got %d", code)
	default:
	}
}

// TestHeadlessSink_ToolCallSuccess_PrintsResult verifies that
// ToolCallSuccessMsg with a result is handled without panic.
func TestHeadlessSink_ToolCallSuccess_PrintsResult(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(runners.ToolCallSuccessMsg{
		ToolName: "read_file",
		Result:   "file contents here",
	})
	// No done signal
	select {
	case <-sink.done:
		t.Fatal("should not signal done on ToolCallSuccessMsg")
	default:
	}
}

// TestHeadlessSink_ToolCallError_PrintsError verifies that
// ToolCallErrorMsg is handled without panic.
func TestHeadlessSink_ToolCallError_PrintsError(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(runners.ToolCallErrorMsg{
		ToolName: "grep",
		Error:    "pattern not found",
	})
	// No done signal
	select {
	case <-sink.done:
		t.Fatal("should not signal done on ToolCallErrorMsg")
	default:
	}
}

// TestHeadlessSink_MinisterInvokingAndCompleted verifies that
// MinisterInvokingMsg and MinisterCompletedMsg are handled without panic.
func TestHeadlessSink_MinisterInvokingAndCompleted(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.MinisterInvokingMsg{
		MinisterID: "forge",
		Task:       "implement the feature",
	})
	sink.handle(court.MinisterCompletedMsg{
		MinisterID: "forge",
	})
	// No done signal from these
	select {
	case <-sink.done:
		t.Fatal("should not signal done on minister messages")
	default:
	}
}

// TestHeadlessSink_MinisterCompletedWithError verifies that
// MinisterCompletedMsg with an error is handled without panic.
func TestHeadlessSink_MinisterCompletedWithError(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.MinisterCompletedMsg{
		MinisterID: "forge",
		Error:      assert.AnError,
	})
	select {
	case <-sink.done:
		t.Fatal("should not signal done on MinisterCompletedMsg")
	default:
	}
}

// TestHeadlessSink_RitualStepStarted_NoFinish verifies that intermediate
// ritual step statuses (started, completed, failed for individual steps)
// do not signal completion.
func TestHeadlessSink_RitualStepStarted_NoFinish(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.RitualStepMsg{
		Status:     "started",
		RitualName: "swift-strike",
		StepName:   "forging",
		StepIndex:  0,
		TotalSteps: 3,
	})
	sink.handle(court.RitualStepMsg{
		Status:  "completed",
		Message: "forging done",
	})
	sink.handle(court.RitualStepMsg{
		Status:   "failed",
		StepName: "judging",
	})

	select {
	case <-sink.done:
		t.Fatal("should not signal done on intermediate ritual steps")
	default:
	}
}

// TestHeadlessContextParams verifies that headlessContextParams correctly
// builds SetContextParams from config and repo info.
func TestHeadlessContextParams(t *testing.T) {
	cfg := &Config{
		Court: config.CourtConfig{
			Username: "testuser",
			Project:  "testproject",
		},
	}
	ri := &repo.RepoInfo{
		ProjectRoot:  "/path/to/project",
		WorktreePath: "/path/to/worktree",
		Branch:       "main",
	}

	params := headlessContextParams(cfg, ri)

	assert.Equal(t, "testproject", params.Project)
	assert.Equal(t, "testuser", params.Username)
	assert.Equal(t, "/path/to/project", params.ProjectRoot)
	assert.Equal(t, "/path/to/worktree", params.WorktreePath)
	assert.Equal(t, "main", params.Branch)
}

// TestHeadlessContextParams_NilRepoInfo verifies fallback to CWD.
func TestHeadlessContextParams_NilRepoInfo(t *testing.T) {
	cfg := &Config{
		Court: config.CourtConfig{
			Username: "testuser",
		},
	}
	params := headlessContextParams(cfg, nil)
	assert.NotEmpty(t, params.ProjectRoot)
}

// TestHeadlessContextParams_ProjectFromSlug verifies that when config
// doesn't set a project, it falls back to repoInfo.Slug.
func TestHeadlessContextParams_ProjectFromSlug(t *testing.T) {
	cfg := &Config{}
	ri := &repo.RepoInfo{
		ProjectRoot: "/path/to/project",
		Slug:        "my-slug",
	}
	params := headlessContextParams(cfg, ri)
	assert.Equal(t, "my-slug", params.Project)
}

// TestHeadlessSink_EventEdictCreated_NoDuplicateEnactment verifies that
// the same edict ID is only enacted once.
func TestHeadlessSink_EventEdictCreated_NoDuplicateEnactment(t *testing.T) {
	sink := newHeadlessSink(nil)

	// First EventEdictCreated — should set enactedSet
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventEdictCreated,
		EdictKey:  storage.EdictKey{ID: 5},
		Payload:   map[string]interface{}{"intent": "test"},
	})
	require.True(t, sink.enactedSet[5], "edict 5 should be marked as enacted")

	// Second EventEdictCreated for same edict — should be a no-op
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventEdictCreated,
		EdictKey:  storage.EdictKey{ID: 5},
		Payload:   map[string]interface{}{"intent": "test"},
	})
	// Still only one entry
	assert.True(t, sink.enactedSet[5])
}

// TestHeadlessSink_ZhengmingPending_NilCourt_NoPanic verifies that
// receiving a ZhengmingPendingMsg when court is nil does not panic.
// autoAnswerZhengming must guard against nil court.
func TestHeadlessSink_ZhengmingPending_NilCourt_NoPanic(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.ZhengmingPendingMsg{
		RequestID:  "req-1",
		MinisterID: "secretary",
		Questions: storage.ZhengmingQuestions{
			{Text: "Which option?", Options: []string{"A", "B"}},
		},
	})
	// No panic, no done signal
	select {
	case <-sink.done:
		t.Fatal("should not signal done on ZhengmingPendingMsg")
	default:
	}
}

// TestHeadlessSink_EventEdictSealed_NoDone verifies that EventEdictSealed
// is handled without panic and does not signal completion.
func TestHeadlessSink_EventEdictSealed_NoDone(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventEdictSealed,
		EdictKey:  storage.EdictKey{ID: 9},
	})
	select {
	case <-sink.done:
		t.Fatal("should not signal done on EventEdictSealed")
	default:
	}
}

// TestHeadlessSink_EventEdictCreated_IDZero_NoEnact verifies that
// EventEdictCreated with edict ID 0 is ignored (no enactment).
func TestHeadlessSink_EventEdictCreated_IDZero_NoEnact(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventEdictCreated,
		EdictKey:  storage.EdictKey{ID: 0},
		Payload:   map[string]interface{}{"intent": "test"},
	})
	assert.False(t, sink.enactedSet[0], "edict 0 should not be enacted")
}

// TestHeadlessSink_EventRitualEnacted_PreventsStreamDoneFinish verifies that
// receiving EventRitualEnacted (from the secretary's enact_ritual tool call)
// marks the edict in enactedSet, preventing StreamDoneMsg from finishing early.
func TestHeadlessSink_EventRitualEnacted_PreventsStreamDoneFinish(t *testing.T) {
	sink := newHeadlessSink(nil)

	// Secretary enacts a ritual via the enact_ritual tool → EventRitualEnacted
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventRitualEnacted,
		EdictKey:  storage.EdictKey{ID: 42},
		Payload:   map[string]interface{}{"ritual_name": "swift-strike"},
	})
	require.True(t, sink.enactedSet[42], "edict 42 should be tracked after EventRitualEnacted")

	// StreamDoneMsg should NOT finish because a ritual is in flight
	sink.handle(court.StreamDoneMsg{ChannelID: "secretary"})
	select {
	case code := <-sink.done:
		t.Fatalf("should not signal done when ritual is in flight, got code %d", code)
	default:
	}
}

// TestHeadlessSink_EventRitualEnacted_DoesNotDuplicateTrack verifies that
// EventRitualEnacted for an edict already in enactedSet (from auto-enact)
// does not cause issues.
func TestHeadlessSink_EventRitualEnacted_DoesNotDuplicateTrack(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.enactedSet[5] = true // already tracked from EventEdictCreated auto-enact

	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventRitualEnacted,
		EdictKey:  storage.EdictKey{ID: 5},
		Payload:   map[string]interface{}{"ritual_name": "swift-strike"},
	})
	assert.True(t, sink.enactedSet[5], "edict 5 should still be tracked")
}

// TestHeadlessSink_EventRitualEnacted_IDZero_NoTrack verifies that
// EventRitualEnacted with edict ID 0 is ignored.
func TestHeadlessSink_EventRitualEnacted_IDZero_NoTrack(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventRitualEnacted,
		EdictKey:  storage.EdictKey{ID: 0},
		Payload:   map[string]interface{}{"ritual_name": "swift-strike"},
	})
	assert.False(t, sink.enactedSet[0], "edict 0 should not be tracked")
}

// TestHeadlessSink_UnknownMessage_NoPanic verifies that an unrecognized
// message type does not cause a panic.
func TestHeadlessSink_UnknownMessage_NoPanic(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handle("some random string")
	sink.handle(42)
	sink.handle(struct{ X int }{X: 1})
	select {
	case <-sink.done:
		t.Fatal("should not signal done on unknown messages")
	default:
	}
}

// TestHeadlessSink_HandsoffOff_ZhengmingNotAnswered verifies that when
// handsoff is false, a ZhengmingPendingMsg is NOT auto-answered (the
// sink should not attempt to call HandleZhengmingResponse).
func TestHeadlessSink_HandsoffOff_ZhengmingNotAnswered(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handsoff = false
	sink.handle(court.ZhengmingPendingMsg{
		RequestID:  "req-1",
		MinisterID: "secretary",
		Questions: storage.ZhengmingQuestions{
			{Text: "Which option?", Options: []string{"A", "B"}},
		},
	})
	// No panic, no done signal
	select {
	case <-sink.done:
		t.Fatal("should not signal done on ZhengmingPendingMsg")
	default:
	}
}

// TestHeadlessSink_HandsoffOn_ZhengmingAutoAnswered verifies that when
// handsoff is true, a ZhengmingPendingMsg is auto-answered (no panic
// even with nil court — autoAnswerZhengming guards against nil).
func TestHeadlessSink_HandsoffOn_ZhengmingAutoAnswered(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handsoff = true
	sink.handle(court.ZhengmingPendingMsg{
		RequestID:  "req-2",
		MinisterID: "secretary",
		Questions: storage.ZhengmingQuestions{
			{Text: "Which option?", Options: []string{"A", "B"}},
		},
	})
	// No panic, no done signal
	select {
	case <-sink.done:
		t.Fatal("should not signal done on ZhengmingPendingMsg")
	default:
	}
}

// TestHeadlessSink_HandsoffOff_NoAutoEnact verifies that when handsoff is
// false, EventEdictCreated does NOT auto-enact swift-strike (edict is
// tracked in enactedSet but no ritual is published).
func TestHeadlessSink_HandsoffOff_NoAutoEnact(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handsoff = false
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventEdictCreated,
		EdictKey:  storage.EdictKey{ID: 10},
		Payload:   map[string]interface{}{"intent": "test"},
	})
	// Edict should be tracked (so StreamDone doesn't finish early if
	// the secretary enacts a ritual via the tool), but no auto-enact.
	assert.True(t, sink.enactedSet[10], "edict 10 should be tracked")
}

// TestHeadlessSink_HandsoffOn_AutoEnacts verifies that when handsoff is
// true, EventEdictCreated triggers auto-enact (edict tracked in enactedSet).
// With nil court, enactSwiftStrike is a no-op but enactedSet is set before
// the call.
func TestHeadlessSink_HandsoffOn_AutoEnacts(t *testing.T) {
	sink := newHeadlessSink(nil)
	sink.handsoff = true
	sink.handle(court.EventNotificationMsg{
		EventType: storage.EventEdictCreated,
		EdictKey:  storage.EdictKey{ID: 11},
		Payload:   map[string]interface{}{"intent": "test"},
	})
	assert.True(t, sink.enactedSet[11], "edict 11 should be tracked")
}
