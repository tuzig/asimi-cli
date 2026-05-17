package rpc

import (
	"context"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/shogunateapi"
	"github.com/afittestide/asimi/shogunate"
)

// LoopbackShogunate composes a *ShogunateClient with a direct reference
// to a local shogunateapi.Client implementation. Wire-safe methods are
// served through the Conn; non-wire-safe methods (GetMinister,
// ConfigureModel) go to Local.
//
// This shape is a stepping stone — it lets the TUI talk through a real
// msgpack codec for almost everything while keeping the last few
// hold-outs working in-process. Once those methods land over the wire
// (or are refactored away), LoopbackShogunate collapses to the plain
// *ShogunateClient.
type LoopbackShogunate struct {
	*ShogunateClient
	Local shogunateapi.Client
}

// NewLoopbackShogunate wires a Conn-backed client that falls back to
// local for in-process-only methods.
func NewLoopbackShogunate(conn *Conn, local shogunateapi.Client) *LoopbackShogunate {
	return &LoopbackShogunate{
		ShogunateClient: NewShogunateClient(conn),
		Local:           local,
	}
}

// GetMinister delegates to the local shogunate.
// Ministers carry non-wire-safe interface methods.
func (l *LoopbackShogunate) GetMinister(id string) shogunate.Minister {
	if l.Local == nil {
		return nil
	}
	return l.Local.GetMinister(id)
}

// ConfigureModel delegates to the local shogunate. The LLM provider
// cannot cross a wire; this method stays in-process until bifrost
// initialisation moves to the daemon side.
func (l *LoopbackShogunate) ConfigureModel(client shogunate.LLMProvider, config *shogunate.SessionConfig, repoInfo repo.RepoInfo) {
	if l.Local == nil {
		return
	}
	l.Local.ConfigureModel(client, config, repoInfo)
}

// CancellableStreamCtx is server-side machinery; the TUI never calls
// it through the LoopbackShogunate. Delegate to Local so the interface
// is satisfied without minting orphan contexts on the client side.
func (l *LoopbackShogunate) CancellableStreamCtx(channelID string) context.Context {
	if l.Local == nil {
		return context.Background()
	}
	return l.Local.CancellableStreamCtx(channelID)
}

var _ shogunateapi.Client = (*LoopbackShogunate)(nil)
