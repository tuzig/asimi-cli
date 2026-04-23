// Package shogunateapi defines the interface the TUI uses to talk to the
// Shogunate coordinator. Today *shogunate.Shogunate satisfies it directly in
// the same process. In later phases an RPC client will satisfy it and the TUI
// will be unaware of the transport.
package shogunateapi

import (
	"context"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	"github.com/maximhq/bifrost/core/schemas"
)

// Client is everything the TUI needs from the Shogunate.
type Client interface {
	// Ministers and scope.
	GetMinister(id string) shogunate.Minister
	Ministers() []shogunate.Minister
	EdictKey(edictID uint) storage.EdictKey
	CourtEdictKey() storage.EdictKey

	// Edicts and events.
	CreateEdict(issueRef, intent string) (*storage.Edict, error)
	CreateEdictSilent(issueRef, intent string) (*storage.Edict, error)
	PublishEvent(key storage.EdictKey, eventType storage.ShogunateEvent, payload storage.JSON) uint
	GetSealService() *storage.SealService

	// Prompts.
	SubmitPrompt(targetID string, p *shogunate.Prompt) error
	RestoreMinisterSession(tabType string, msgs []schemas.ChatMessage) error

	// Model configuration.
	ConfigureModel(client shogunate.LLMProvider, config *shogunate.SessionConfig, repoInfo repo.RepoInfo)

	// Runner access. Post-split, the TUI only uses this to toggle AllowFallback;
	// keep it here so P1 is behaviour-preserving.
	GetRunner() runners.Runner

	// Snapshots for the shogunate debug view.
	TakeSnapshot() shogunate.Snapshot

	// SetRulingCtx hands the Shogunate a function that returns the currently-
	// focused tab's context. Rituals use this context, so cancelling the tab
	// cancels rituals. In later phases this is replaced by an explicit Cancel
	// RPC; kept here for P1 behaviour parity.
	SetRulingCtx(fn func() context.Context)

	// Subscribe returns a channel that carries every TUI-bound notification:
	// streaming chunks, shogunate events, and runner messages (container
	// launched, approval requests). The channel stays open for the client's
	// lifetime; callers drain it until ctx is done.
	Subscribe(ctx context.Context) <-chan any
}

var _ Client = (*shogunate.Shogunate)(nil)
