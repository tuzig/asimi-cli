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
//
// Every method below is designed so its parameters and return values can
// travel over a MessagePack wire unchanged. GetMinister and ConfigureModel
// are the remaining exceptions: they hand back or consume non-marshallable
// types (Minister interface, bifrost.LLMProvider) and are kept in-process
// only pending a session/model refactor of their own.
type Client interface {
	// Scope.
	EdictKey(edictID uint) storage.EdictKey
	CourtEdictKey() storage.EdictKey

	// Minister presence and reset. Fully wire-safe.
	HasMinister(id string) bool
	ResetMinisterSession(id string)

	// In-process only; needed by getCurrentSession(). Scheduled for a
	// future session-narrowing phase.
	GetMinister(id string) shogunate.Minister

	// Edicts and events.
	CreateEdict(issueRef, intent string) (*storage.Edict, error)
	CreateEdictSilent(issueRef, intent string) (*storage.Edict, error)
	GetEdict(edictID uint) (*storage.Edict, error)
	PublishEvent(key storage.EdictKey, eventType storage.ShogunateEvent, payload storage.JSON) uint

	// Seals.
	GrantRulerSeal(edictID uint, notes string) error
	GetEdictSeals(key storage.EdictKey) ([]storage.Seal, error)
	ListActiveEdicts() ([]storage.ActiveEdict, error)

	// Prompts and sessions.
	SubmitPrompt(targetID string, p *shogunate.Prompt) error
	RestoreMinisterSession(tabType string, msgs []schemas.ChatMessage) error

	// Session state (wire-safe bulk read + targeted mutators).
	SessionState(tabTarget string) shogunate.SessionState
	AddSessionContextFile(tabTarget, path, content string) error
	AddSessionMessage(tabTarget, role, content string) error
	ClearSessionHistory(tabTarget string) error
	RollbackSession(tabTarget string, snapshot int) error
	CompactSession(ctx context.Context, tabTarget, prompt string) (string, error)

	// Zhengming.
	HandleZhengmingResponse(ctx context.Context, requestID, answer string) error
	CancelZhengming(requestID string)

	// Shell runner.
	AllowRunnerFallback(allow bool)
	RunShellCommand(ctx context.Context, input runners.Input) (runners.Output, error)

	// In-process only; LLM client setup. Scheduled for a model-layer
	// refactor that builds the client daemon-side.
	ConfigureModel(client shogunate.LLMProvider, config *shogunate.SessionConfig, repoInfo repo.RepoInfo)

	// Snapshots for the shogunate debug view.
	TakeSnapshot() shogunate.Snapshot

	// SetRulingCtx hands the Shogunate a function that returns the currently-
	// focused tab's context. Cancelling the tab cancels rituals. In the split
	// world this becomes an explicit Cancel RPC.
	SetRulingCtx(fn func() context.Context)

	// Subscribe returns a channel that carries every TUI-bound notification:
	// streaming chunks, shogunate events, and runner messages.
	Subscribe(ctx context.Context) <-chan any
}

var _ Client = (*shogunate.Shogunate)(nil)
