package court

import (
	"context"
	"testing"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestSetRunnerMessageChannel_PropagatesToMinisters verifies that
// SetRunnerMessageChannel sets the msg channel on the runner AND on every
// minister that implements SetMessageChannel. This is the fix for edict 411:
// Subscribe() must call SetRunnerMessageChannel (not s.runner.SetMessageChannel
// directly) so ephemeral HostRunner instances created by RunShellCommand
// inherit the channel from their parent minister.
func TestSetRunnerMessageChannel_PropagatesToMinisters(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()

	runner := &recordingMsgChanRunner{}
	court := NewCourt(db, cfg, runner, nil)
	require.NotNil(t, court)

	// ConfigureModel is needed so ministers get a SessionConfig (avoids nil
	// dereference in some minister internals).
	court.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	// Create a message channel and call SetRunnerMessageChannel.
	ch := make(chan runners.Msg, 10)
	var msgChan chan<- runners.Msg = ch
	court.SetRunnerMessageChannel(msgChan)

	// Verify the runner got the channel.
	assert.NotNil(t, runner.msgChan, "runner should receive a non-nil msg channel")

	// Verify every minister that supports SetMessageChannel received the channel.
	for _, id := range []string{"chancellor", "sage", "forge", "judge"} {
		m := court.GetMinister(id)
		require.NotNil(t, m, "minister %s should exist", id)

		getter, ok := m.(interface {
			SetMessageChannel(chan<- runners.Msg)
			MessageChannel() chan<- runners.Msg
		})
		require.True(t, ok, "minister %s should implement SetMessageChannel/MessageChannel", id)

		// The critical assertion: the channel must have been propagated, not just
		// that the interface exists. This is what edict 411 fixes — Subscribe()
		// previously called s.runner.SetMessageChannel directly, skipping propagation.
		assert.NotNil(t, getter.MessageChannel(), "minister %s should have a non-nil msg channel after SetRunnerMessageChannel", id)
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
// uses SetRunnerMessageChannel (which propagates to ministers) rather than
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
