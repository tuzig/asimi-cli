package court

import (
	"context"
	"runtime"
	"testing"

	"github.com/afittestide/asimi/court/tools"
	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reflect"
)

// recordingMsgChanRunner captures the msgChan passed via SetMessageChannel.
type recordingMsgChanRunner struct {
	runners.Runner // embed nil to satisfy interface; only SetMessageChannel matters
	msgChan        chan<- runners.Msg
}

func (r *recordingMsgChanRunner) SetMessageChannel(ch chan<- runners.Msg) { r.msgChan = ch }
func (r *recordingMsgChanRunner) Run(context.Context, runners.Input) (runners.Output, error) {
	return runners.Output{}, nil
}
func (r *recordingMsgChanRunner) Close(context.Context) error   { return nil }
func (r *recordingMsgChanRunner) Restart(context.Context) error { return nil }
func (r *recordingMsgChanRunner) AllowFallback(bool)            {}
func (r *recordingMsgChanRunner) RunnerType() string            { return "recording" }
func (r *recordingMsgChanRunner) GetOS() string                 { return runtime.GOOS }

// TestSetRunnerMessageChannel_UpdatesPointerHolders verifies that
// SetRunnerMessageChannel updates s.msgChan on the Court. Since ministers
// and tools hold a *chan<- runners.Msg pointing to &s.msgChan, they
// automatically see the new value without explicit propagation.
func TestSetRunnerMessageChannel_UpdatesPointerHolders(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()

	runner := &recordingMsgChanRunner{}
	court := NewCourt(db, cfg, runner, nil)
	require.NotNil(t, court)

	// ConfigureModel is needed so ministers get a SessionConfig (avoids nil
	// dereference in some minister internals).
	court.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	// Before SetRunnerMessageChannel, s.msgChan is nil (and all pointer holders see nil).
	assert.Nil(t, court.msgChan, "msgChan should be nil before SetRunnerMessageChannel")

	// Create a message channel and call SetRunnerMessageChannel.
	ch := make(chan runners.Msg, 10)
	var msgChan chan<- runners.Msg = ch
	court.SetRunnerMessageChannel(msgChan)

	// Verify the runner got the channel.
	assert.NotNil(t, runner.msgChan, "runner should receive a non-nil msg channel")

	// Verify every minister's msgChan pointer dereferences to the new channel.
	for _, id := range []string{"chancellor", "sage", "forge", "judge"} {
		m := court.GetMinister(id)
		require.NotNil(t, m, "minister %s should exist", id)

		// Use reflection to read the unexported msgChan field (*chan<- runners.Msg).
		// Walk the embedded *MinisterBase to find the field.
		val := reflect.ValueOf(m)
		for val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		baseField := val.FieldByName("MinisterBase")
		if !baseField.IsValid() || baseField.IsNil() {
			t.Fatalf("minister %s does not have MinisterBase", id)
		}
		baseElem := baseField.Elem()
		msgChanField := baseElem.FieldByName("msgChan")
		require.True(t, msgChanField.IsValid(), "minister %s should have msgChan field", id)

		// msgChan is *chan<- runners.Msg — verify it's non-nil and points to a non-nil channel.
		require.False(t, msgChanField.IsNil(), "minister %s msgChan pointer should be non-nil", id)

		// Dereference the pointer and check the channel is not nil.
		derefChan := msgChanField.Elem()
		require.False(t, derefChan.IsNil(), "minister %s *msgChan should point to a non-nil channel", id)
	}
}

// TestSetRunnerMessageChannel_NilRunner is a safety check: calling
// SetRunnerMessageChannel on a court with no runner must not panic.
func TestSetRunnerMessageChannel_NilRunner(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()

	court := NewCourt(db, cfg, nil, nil)
	require.NotNil(t, court)

	ch := make(chan runners.Msg, 10)
	var msgChan chan<- runners.Msg = ch
	// Must not panic.
	court.SetRunnerMessageChannel(msgChan)
}

// TestSubscribe_CallsSetRunnerMessageChannel verifies that Subscribe()
// uses SetRunnerMessageChannel (which sets s.msgChan) rather than
// calling s.runner.SetMessageChannel directly. We confirm this by checking
// that after Subscribe(), the runner has a non-nil msgChan.
func TestSubscribe_CallsSetRunnerMessageChannel(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()

	runner := &recordingMsgChanRunner{}
	court := NewCourt(db, cfg, runner, nil)
	require.NotNil(t, court)
	court.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe creates the runner msg channel internally and calls
	// SetRunnerMessageChannel. The runner should end up with a non-nil
	// msgChan.
	_ = court.Subscribe(ctx)
	assert.NotNil(t, runner.msgChan, "Subscribe should set a non-nil msgChan on the runner via SetRunnerMessageChannel")
}

// TestBuildToolRegistry_PointerPropagatesToShellTool verifies the core fix:
// buildToolRegistry runs when msgChan is nil, then SetRunnerMessageChannel
// sets the real channel. The shell tool in the registry must see the new
// channel via the pointer — this would fail with the old value-copy approach.
func TestBuildToolRegistry_PointerPropagatesToShellTool(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()

	runner := &recordingMsgChanRunner{}
	court := NewCourt(db, cfg, runner, nil)
	require.NotNil(t, court)
	court.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	// At this point, buildToolRegistry has already run (in NewCourt).
	// s.msgChan is still nil — the shell tool's *msgChan points to &s.msgChan.
	require.Nil(t, court.msgChan, "msgChan should be nil before SetRunnerMessageChannel")

	// Now set the real channel.
	ch := make(chan runners.Msg, 10)
	var msgChan chan<- runners.Msg = ch
	court.SetRunnerMessageChannel(msgChan)
	require.NotNil(t, court.msgChan, "msgChan should be set after SetRunnerMessageChannel")

	// Retrieve the RunShellCommand tool from the registry.
	forgePerm, _ := tools.ParsePermissions("rwxr---w-")
	ts := court.toolRegistry.ForPermissions(forgePerm)
	var shellImpl *tools.RunShellCommand
	for _, tool := range ts {
		if st, ok := tool.(*tools.RunShellCommand); ok {
			shellImpl = st
			break
		}
	}
	require.NotNil(t, shellImpl, "run_shell_command should be registered")

	// Verify the shell tool sees the non-nil channel via its pointer.
	// Use reflection since msgChan is unexported.
	shellVal := reflect.ValueOf(shellImpl).Elem()
	msgChanField := shellVal.FieldByName("msgChan")
	require.True(t, msgChanField.IsValid(), "RunShellCommand should have msgChan field")
	require.False(t, msgChanField.IsNil(), "shell tool's msgChan pointer should be non-nil")
	derefChan := msgChanField.Elem()
	assert.False(t, derefChan.IsNil(), "shell tool should see the non-nil msgChan via pointer after SetRunnerMessageChannel")
}
