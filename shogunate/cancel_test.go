package shogunate

import (
	"context"
	"testing"
	"time"
)

func TestCancelTabCancelsRegisteredContext(t *testing.T) {
	s := &Shogunate{}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	defer s.cancel()

	ctx := s.CancellableStreamCtx("ruling")
	select {
	case <-ctx.Done():
		t.Fatal("ctx cancelled before CancelTab")
	default:
	}

	s.CancelTab("ruling")
	select {
	case <-ctx.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("ctx not cancelled after CancelTab")
	}
}

func TestCancelTabUnknownChannelIsNoOp(t *testing.T) {
	s := &Shogunate{}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	defer s.cancel()
	// Should not panic.
	s.CancelTab("unknown")
}

func TestCancellableStreamCtxReplacesPriorCancel(t *testing.T) {
	s := &Shogunate{}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	defer s.cancel()

	first := s.CancellableStreamCtx("ruling")
	// Register a second for same channel: should cancel the first.
	second := s.CancellableStreamCtx("ruling")

	select {
	case <-first.Done():
		// expected — first was replaced
	case <-time.After(time.Second):
		t.Fatal("first ctx not cancelled when replaced")
	}
	select {
	case <-second.Done():
		t.Fatal("second ctx cancelled prematurely")
	default:
	}

	s.CancelTab("ruling")
	select {
	case <-second.Done():
	case <-time.After(time.Second):
		t.Fatal("second ctx not cancelled")
	}
}

func TestCancellableStreamCtxChildOfRootCtx(t *testing.T) {
	// When the shogunate's root ctx cancels, stream ctxes cancel too.
	s := &Shogunate{}
	s.ctx, s.cancel = context.WithCancel(context.Background())

	ctx := s.CancellableStreamCtx("ruling")
	s.cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stream ctx not cancelled when root cancelled")
	}
}
